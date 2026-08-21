package providerauth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubHandle is a DeviceAuthHandle stand-in. It records cancels and lets a test
// release Wait at a chosen moment.
type stubHandle struct {
	release chan struct{}
	err     error

	mu        sync.Mutex
	cancels   int
	waits     int
	updates   chan string
	panicWait bool
}

func newStubHandle(err error) *stubHandle {
	return &stubHandle{release: make(chan struct{}), err: err, updates: make(chan string, 1)}
}

func (h *stubHandle) Wait(ctx context.Context) error {
	h.mu.Lock()
	h.waits++
	shouldPanic := h.panicWait
	h.mu.Unlock()
	if shouldPanic {
		panic("handle exploded")
	}
	select {
	case <-h.release:
		return h.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *stubHandle) Cancel() {
	h.mu.Lock()
	h.cancels++
	h.mu.Unlock()
	select {
	case <-h.release:
	default:
		close(h.release)
	}
}

func (h *stubHandle) Updates() <-chan string { return h.updates }

func (h *stubHandle) cancelCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cancels
}

func (h *stubHandle) waitCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.waits
}

func (h *stubHandle) finish() {
	select {
	case <-h.release:
	default:
		close(h.release)
	}
}

// TestReserveOwnsCapacityBeforeAnySideEffect proves admission is decided before
// a provider process can exist. The pre-transaction server started the child
// first and rejected afterwards, which orphaned it (MADR 0074 F3, D27).
func TestReserveOwnsCapacityBeforeAnySideEffect(t *testing.T) {
	r := NewRegistry()
	var reservations []*Reservation
	for i := range MaxFlowsPerDevice {
		res, err := r.Reserve(context.Background(), "dev-1", "codex", "openai")
		if err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
		reservations = append(reservations, res)
	}
	if _, err := r.Reserve(context.Background(), "dev-1", "codex", "openai"); !errors.Is(err, ErrTooManyFlows) {
		t.Fatalf("err = %v, want ErrTooManyFlows", err)
	}
	// A rejected reservation owns nothing, so nothing needs cleaning up.
	for _, res := range reservations {
		res.Cancel()
	}
	if n := r.Len(); n != 0 {
		t.Fatalf("live flows = %d after cancelling every reservation", n)
	}
}

// TestAbandonedReservationReleasesCapacity proves a reservation that never
// attaches a handle still frees its slot.
func TestAbandonedReservationReleasesCapacity(t *testing.T) {
	r := NewRegistry()
	res, err := r.Reserve(context.Background(), "dev-1", "codex", "openai")
	if err != nil {
		t.Fatal(err)
	}
	res.Cancel()
	res.Cancel() // idempotent

	for range MaxFlowsPerDevice {
		if _, err := r.Reserve(context.Background(), "dev-1", "codex", "openai"); err != nil {
			t.Fatalf("capacity was not released: %v", err)
		}
	}
}

// TestAttachAcceptsExactlyOneHandle proves a second attach fails and cancels
// the handle it was given, so a caller cannot leak a started child by racing.
func TestAttachAcceptsExactlyOneHandle(t *testing.T) {
	r := NewRegistry()
	res, err := r.Reserve(context.Background(), "dev-1", "codex", "openai")
	if err != nil {
		t.Fatal(err)
	}
	first := newStubHandle(nil)
	second := newStubHandle(nil)

	if err := res.Attach(first, DeviceFlowInfo{UserCode: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := res.Attach(second, DeviceFlowInfo{UserCode: "B"}); err == nil {
		t.Fatal("a second attach was accepted")
	}
	if second.cancelCount() == 0 {
		t.Fatal("the rejected handle was leaked instead of cancelled")
	}
	res.Cancel()
	<-res.Done()
}

// TestOwnerCallsWaitOnceAndFinishesOnce proves one terminal result however many
// observers there are (MADR 0074 D27, P20 step 2).
func TestOwnerCallsWaitOnceAndFinishesOnce(t *testing.T) {
	r := NewRegistry()
	res, err := r.Reserve(context.Background(), "dev-1", "codex", "openai")
	if err != nil {
		t.Fatal(err)
	}
	h := newStubHandle(nil)
	if err := res.Attach(h, DeviceFlowInfo{UserCode: "A"}); err != nil {
		t.Fatal(err)
	}
	h.finish()

	var wg sync.WaitGroup
	results := make([]error, 4)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-res.Done()
			results[i] = res.Result()
		}()
	}
	wg.Wait()

	if h.waitCount() != 1 {
		t.Fatalf("Wait called %d times, want exactly 1", h.waitCount())
	}
	for i, got := range results {
		if got != results[0] {
			t.Fatalf("observer %d saw %v, want the shared result %v", i, got, results[0])
		}
	}
	if r.Len() != 0 {
		t.Fatal("a finished flow was not removed from the registry")
	}
}

// TestOwnerRecoversAPanicAsAFailedResult proves a panicking handle cannot take
// the daemon down or strand capacity.
func TestOwnerRecoversAPanicAsAFailedResult(t *testing.T) {
	r := NewRegistry()
	res, err := r.Reserve(context.Background(), "dev-1", "codex", "openai")
	if err != nil {
		t.Fatal(err)
	}
	h := newStubHandle(nil)
	h.panicWait = true
	if err := res.Attach(h, DeviceFlowInfo{UserCode: "A"}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-res.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("a panicking handle never produced a terminal result")
	}
	if res.Result() == nil {
		t.Fatal("a panic was reported as success")
	}
	if r.Len() != 0 {
		t.Fatal("a panicking flow stranded its registry slot")
	}
}

// TestReadyToActivateIsNonTerminal proves a deferred activation keeps its
// ownership and admission rather than looking finished (MADR 0074 D28).
func TestReadyToActivateIsNonTerminal(t *testing.T) {
	r := NewRegistry()
	res, err := r.Reserve(context.Background(), "dev-1", "codex", "openai")
	if err != nil {
		t.Fatal(err)
	}
	h := newStubHandle(nil)
	if err := res.Attach(h, DeviceFlowInfo{UserCode: "A"}); err != nil {
		t.Fatal(err)
	}

	var seen atomic.Value
	go func() {
		for s := range res.Updates() {
			seen.Store(s)
		}
	}()
	h.updates <- "ready_to_activate"

	if !waitFor(t, 5*time.Second, func() bool { return seen.Load() != nil }) {
		t.Fatal("the non-terminal update was never forwarded")
	}
	select {
	case <-res.Done():
		t.Fatal("ready_to_activate was treated as terminal")
	default:
	}
	if r.Len() != 1 {
		t.Fatal("a deferred flow released its admission slot")
	}
	res.Cancel()
	<-res.Done()
}

// TestCancelDoesNotReleaseCapacityBeforeCleanup proves the slot is held until
// the handle's terminal cleanup actually completes.
func TestCancelDoesNotReleaseCapacityBeforeCleanup(t *testing.T) {
	r := NewRegistry()
	res, err := r.Reserve(context.Background(), "dev-1", "codex", "openai")
	if err != nil {
		t.Fatal(err)
	}
	// A handle whose Cancel does not immediately end Wait.
	h := &slowHandle{released: make(chan struct{})}
	if err := res.Attach(h, DeviceFlowInfo{UserCode: "A"}); err != nil {
		t.Fatal(err)
	}
	res.Cancel()

	if r.Len() != 1 {
		t.Fatal("capacity was released before terminal cleanup finished")
	}
	close(h.released)
	<-res.Done()
	if r.Len() != 0 {
		t.Fatal("capacity was not released after cleanup")
	}
}

type slowHandle struct {
	released chan struct{}
	once     sync.Once
}

func (h *slowHandle) Wait(context.Context) error { <-h.released; return ErrFlowCancelled }
func (h *slowHandle) Cancel()                    { h.once.Do(func() {}) }

// TestCancelAllDrainsEveryFlow proves shutdown cancels and reaps before the
// process exits and destroys in-memory ownership (MADR 0074 F4, D27).
func TestCancelAllDrainsEveryFlow(t *testing.T) {
	r := NewRegistry()
	handles := make([]*stubHandle, 0, 4)
	for i := range 4 {
		dev := "dev-a"
		if i%2 == 1 {
			dev = "dev-b"
		}
		res, err := r.Reserve(context.Background(), dev, "codex", "openai")
		if err != nil {
			t.Fatal(err)
		}
		h := newStubHandle(nil)
		if err := res.Attach(h, DeviceFlowInfo{UserCode: "A"}); err != nil {
			t.Fatal(err)
		}
		handles = append(handles, h)
	}

	r.CancelAll()
	if err := r.WaitAll(context.Background(), DrainTimeout); err != nil {
		t.Fatalf("drain: %v", err)
	}
	for i, h := range handles {
		if h.cancelCount() == 0 {
			t.Errorf("flow %d was never cancelled at shutdown", i)
		}
	}
	if r.Len() != 0 {
		t.Fatal("flows survived the drain")
	}
}

// TestWaitAllReportsRetainedOwnership proves a stuck flow is reported rather
// than silently forced, and its state is preserved.
func TestWaitAllReportsRetainedOwnership(t *testing.T) {
	r := NewRegistry()
	res, err := r.Reserve(context.Background(), "dev-1", "codex", "openai")
	if err != nil {
		t.Fatal(err)
	}
	h := &slowHandle{released: make(chan struct{})}
	if err := res.Attach(h, DeviceFlowInfo{UserCode: "A"}); err != nil {
		t.Fatal(err)
	}
	r.CancelAll()

	err = r.WaitAll(context.Background(), 200*time.Millisecond)
	if err == nil {
		t.Fatal("a stuck flow was reported as drained")
	}
	if r.Len() != 1 {
		t.Fatal("a stuck flow's state was discarded")
	}
	close(h.released)
	<-res.Done()
}

// TestDetachAndResume proves a transient disconnect keeps the flow alive for
// the resume window and a same-device resume reattaches it (P20 step 5).
func TestDetachAndResume(t *testing.T) {
	r := NewRegistry()
	res, err := r.Reserve(context.Background(), "dev-1", "codex", "openai")
	if err != nil {
		t.Fatal(err)
	}
	h := newStubHandle(nil)
	if err := res.Attach(h, DeviceFlowInfo{UserCode: "A"}); err != nil {
		t.Fatal(err)
	}

	if n := r.DetachDevice("dev-1", time.Minute); n != 1 {
		t.Fatalf("detached %d flows, want 1", n)
	}
	if h.cancelCount() != 0 {
		t.Fatal("a transient disconnect cancelled the flow immediately")
	}
	resumed := r.ResumeDevice("dev-1")
	if len(resumed) != 1 || resumed[0].ID() != res.ID() {
		t.Fatalf("resume returned %v, want the detached flow", resumed)
	}
	if h.cancelCount() != 0 {
		t.Fatal("a resumed flow was cancelled")
	}
	res.Cancel()
	<-res.Done()
}

// TestDetachExpiryCancels proves an abandoned flow does not outlive its resume
// window.
func TestDetachExpiryCancels(t *testing.T) {
	r := NewRegistry()
	res, err := r.Reserve(context.Background(), "dev-1", "codex", "openai")
	if err != nil {
		t.Fatal(err)
	}
	h := newStubHandle(nil)
	if err := res.Attach(h, DeviceFlowInfo{UserCode: "A"}); err != nil {
		t.Fatal(err)
	}

	r.DetachDevice("dev-1", 50*time.Millisecond)
	select {
	case <-res.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("an abandoned flow outlived its resume window")
	}
	if h.cancelCount() == 0 {
		t.Fatal("the expired flow was not cancelled")
	}
	if r.Len() != 0 {
		t.Fatal("the expired flow was not reaped")
	}
}

// TestTerminalResultSurvivesDisconnect proves the outcome is retained for a
// reconnecting device rather than being written to a dead socket, and that it
// carries no child output, device code, or credential metadata (P20 step 7).
func TestTerminalResultSurvivesDisconnect(t *testing.T) {
	r := NewRegistry()
	res, err := r.Reserve(context.Background(), "dev-1", "codex", "openai")
	if err != nil {
		t.Fatal(err)
	}
	h := newStubHandle(errors.New("sign-in failed"))
	if err := res.Attach(h, DeviceFlowInfo{UserCode: "SECRET-CODE"}); err != nil {
		t.Fatal(err)
	}
	r.DetachDevice("dev-1", time.Minute)
	h.finish()
	<-res.Done()

	pending := r.PendingResults("dev-1")
	if len(pending) != 1 {
		t.Fatalf("pending results = %d, want 1", len(pending))
	}
	got := pending[0]
	if got.FlowID != res.ID() || got.ProviderID != "codex" {
		t.Fatalf("result = %+v, want it to identify the flow", got)
	}
	if got.State == "" {
		t.Fatal("no terminal state was retained")
	}
	if got.UserCode != "" {
		t.Fatal("the retained result carried the device code")
	}
	// Draining is once-only so a reconnect cannot replay it forever.
	if again := r.PendingResults("dev-1"); len(again) != 0 {
		t.Fatal("terminal results were replayed after being drained")
	}
}

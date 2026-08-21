package ws

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// ownedStub is a provider.DeviceAuthHandle whose lifetime a test controls.
type ownedStub struct {
	mu      sync.Mutex
	cancels int
	release chan struct{}
	err     error
	updates chan provider.DeviceAuthState
}

func newOwnedStub(err error) *ownedStub {
	return &ownedStub{release: make(chan struct{}), err: err, updates: make(chan provider.DeviceAuthState, 1)}
}

func (h *ownedStub) Flow() provider.DeviceFlow {
	return provider.DeviceFlow{VerificationURI: "https://example.test/device", UserCode: "CODE-1"}
}

func (h *ownedStub) Wait(ctx context.Context) error {
	select {
	case <-h.release:
		return h.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *ownedStub) Cancel() {
	h.mu.Lock()
	h.cancels++
	h.mu.Unlock()
	select {
	case <-h.release:
	default:
		close(h.release)
	}
}

func (h *ownedStub) Updates() <-chan provider.DeviceAuthState { return h.updates }

func (h *ownedStub) cancelCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cancels
}

// TestOwnedFlowIsAlwaysOwnedBeforeFramesAreWritten is the F3 regression: no
// server branch may start a provider child without first installing the owner
// that can clean it up.
func TestOwnedFlowIsAlwaysOwnedBeforeFramesAreWritten(t *testing.T) {
	reg := providerauth.NewRegistry()

	// Fill the device's admission quota, then prove the next reserve is
	// refused before any handle could have been created.
	var started atomic.Int64
	makeHandle := func() provider.DeviceAuthHandle {
		started.Add(1)
		return newOwnedStub(nil)
	}

	for range providerauth.MaxFlowsPerDevice {
		res, err := reg.Reserve(context.Background(), "dev-1", "codex", "openai")
		if err != nil {
			t.Fatal(err)
		}
		if err := res.Attach(makeHandle(), providerauth.DeviceFlowInfo{}); err != nil {
			t.Fatal(err)
		}
	}
	before := started.Load()
	if _, err := reg.Reserve(context.Background(), "dev-1", "codex", "openai"); !errors.Is(err, providerauth.ErrTooManyFlows) {
		t.Fatalf("err = %v, want ErrTooManyFlows", err)
	}
	if started.Load() != before {
		t.Fatal("a rejected admission still created a provider handle")
	}

	reg.CancelAll()
	if err := reg.WaitAll(context.Background(), providerauth.DrainTimeout); err != nil {
		t.Fatal(err)
	}
}

// TestShutdownCancelsAndReapsEveryFlow is the F4 regression: a restart must not
// destroy in-memory ownership while a child is still running.
func TestShutdownCancelsAndReapsEveryFlow(t *testing.T) {
	reg := providerauth.NewRegistry()
	stubs := make([]*ownedStub, 0, 3)
	// Distinct devices: the per-device cap is deliberately small, and this
	// test is about shutdown reaching every flow, not about admission.
	for i := range 3 {
		res, err := reg.Reserve(context.Background(), "dev-"+string(rune('a'+i)), "codex", "openai")
		if err != nil {
			t.Fatal(err)
		}
		h := newOwnedStub(nil)
		if err := res.Attach(h, providerauth.DeviceFlowInfo{}); err != nil {
			t.Fatal(err)
		}
		stubs = append(stubs, h)
	}

	reg.CancelAll()
	if err := reg.WaitAll(context.Background(), providerauth.DrainTimeout); err != nil {
		t.Fatalf("shutdown did not drain: %v", err)
	}
	for i, h := range stubs {
		if h.cancelCount() == 0 {
			t.Errorf("flow %d survived shutdown uncancelled", i)
		}
	}
	if reg.Len() != 0 {
		t.Fatal("flows survived shutdown")
	}
}

// TestDuplicateCancelAndFinishAreIdempotent proves exactly one cleanup however
// many times a phone retries.
func TestDuplicateCancelAndFinishAreIdempotent(t *testing.T) {
	reg := providerauth.NewRegistry()
	res, err := reg.Reserve(context.Background(), "dev-1", "codex", "openai")
	if err != nil {
		t.Fatal(err)
	}
	h := newOwnedStub(nil)
	if err := res.Attach(h, providerauth.DeviceFlowInfo{}); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		res.Cancel()
	}
	<-res.Done()
	if got := h.cancelCount(); got != 1 {
		t.Fatalf("handle cancelled %d times, want exactly 1", got)
	}
	if reg.Len() != 0 {
		t.Fatal("repeated cancels stranded the flow")
	}
}

// TestTerminalStateClassification pins the additive result vocabulary so a
// client can switch on it exhaustively (P20 step 9).
func TestTerminalStateClassification(t *testing.T) {
	cases := []struct {
		err       error
		want      string
		retryable bool
	}{
		{nil, providerauth.ResultCompleted, false},
		{providerauth.ErrFlowCancelled, providerauth.ResultCancelled, true},
		{providerauth.ErrActivationExpired, providerauth.ResultExpired, true},
		{providerauth.ErrConflict, providerauth.ResultConflict, true},
		{providerauth.ErrUnsupportedBackend, providerauth.ResultUnsupportedBackend, false},
		{providerauth.ErrRecoveryRequired, providerauth.ResultRecoveryRequired, false},
		{errors.New("boom"), providerauth.ResultFailed, true},
	}
	for _, tc := range cases {
		reg := providerauth.NewRegistry()
		res, err := reg.Reserve(context.Background(), "dev-1", "codex", "openai")
		if err != nil {
			t.Fatal(err)
		}
		h := newOwnedStub(tc.err)
		if err := res.Attach(h, providerauth.DeviceFlowInfo{}); err != nil {
			t.Fatal(err)
		}
		close(h.release)
		<-res.Done()

		if got := res.State(); got != tc.want {
			t.Errorf("state for %v = %q, want %q", tc.err, got, tc.want)
		}
		if got := providerauth.Retryable(res.State()); got != tc.retryable {
			t.Errorf("retryable for %v = %v, want %v", tc.err, got, tc.retryable)
		}
	}
	// ready_to_activate is never retryable: it is not terminal.
	if providerauth.Retryable(providerauth.ResultReadyToActivate) {
		t.Error("ready_to_activate must not be retryable")
	}
}

// TestDisconnectResumeAndExpiry covers the P20 step 5 lifecycle.
func TestDisconnectResumeAndExpiry(t *testing.T) {
	t.Run("resume keeps the flow", func(t *testing.T) {
		reg := providerauth.NewRegistry()
		res, err := reg.Reserve(context.Background(), "dev-1", "codex", "openai")
		if err != nil {
			t.Fatal(err)
		}
		h := newOwnedStub(nil)
		if err := res.Attach(h, providerauth.DeviceFlowInfo{}); err != nil {
			t.Fatal(err)
		}
		reg.DetachDevice("dev-1", time.Minute)
		reg.ResumeDevice("dev-1")
		time.Sleep(200 * time.Millisecond)
		if h.cancelCount() != 0 {
			t.Fatal("a resumed flow was cancelled")
		}
		res.Cancel()
		<-res.Done()
	})

	t.Run("expiry cancels and cleans up", func(t *testing.T) {
		reg := providerauth.NewRegistry()
		res, err := reg.Reserve(context.Background(), "dev-1", "codex", "openai")
		if err != nil {
			t.Fatal(err)
		}
		h := newOwnedStub(nil)
		if err := res.Attach(h, providerauth.DeviceFlowInfo{}); err != nil {
			t.Fatal(err)
		}
		reg.DetachDevice("dev-1", 50*time.Millisecond)
		select {
		case <-res.Done():
		case <-time.After(10 * time.Second):
			t.Fatal("an abandoned flow never expired")
		}
		if h.cancelCount() == 0 {
			t.Fatal("the expired flow was not cancelled")
		}
	})
}

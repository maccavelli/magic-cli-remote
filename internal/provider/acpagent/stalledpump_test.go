package acpagent

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// sdkNotificationQueueDepth is the SDK's notification queue capacity
// (acp-go-sdk@v0.13.5 connection.go:19, defaultMaxQueuedNotifications).
//
// Written down here because the failure this file guards is entirely about that
// number: the queue does not drop on overflow, it closes the whole connection
// (connection.go:432-447, errNotificationQueueOverflow → shutdownReceive). A
// client handler that blocks forever therefore does not merely stall its own
// session — it takes the engine's transport down with it (MADR 0138 F5).
//
// And a client handler that blocks for no time at all can lose it too. The queue
// is between the SDK's reader and its single consumer, both of which are the
// SDK's; when the reader wins that race the connection closes whatever we do.
// Not blocking is still worth doing — it keeps us from being the cause — but it
// is not a guarantee, which is why this file no longer asserts one
// (MADR 0166 F6/F12).
const sdkNotificationQueueDepth = 1024

// toolCallFrame is one ACP `session/update` carrying a tool_call.
//
// tool_call and not a chunk: chunks are coalesced and delivered with the
// non-blocking send, so they cannot exercise the blocking control path. Not
// tool_call_update either — that is an in-place update, and since MADR 0138
// Phase 5 grok's tool lane holds those rather than delivering them. A tool_call
// is a boundary the lane never holds, so it takes the control path every time.
//
// The shape is taken from the SDK's own ToolCall type (types_gen.go) and
// matches the frames in internal/provider/grok/testdata/wire/1.0.13.
func toolCallFrame(sessionID string, i int) []byte {
	b, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": sessionID,
			"update": map[string]any{
				"sessionUpdate": "tool_call",
				"toolCallId":    fmt.Sprintf("tool-%d", i),
				"title":         fmt.Sprintf("call %d", i),
				"kind":          "execute",
				"status":        "pending",
			},
		},
	})
	if err != nil {
		panic(err)
	}
	return append(b, '\n')
}

// stalledSession builds a session wired to a real ClientSideConnection, with a
// full event channel and no consumer — the stalled pump. opts are the
// connection options; pass clientConnOptions() to get what production builds.
func stalledSession(t *testing.T, opts ...acp.ConnectionOption) (*session, *acp.ClientSideConnection, *io.PipeWriter) {
	t.Helper()

	s := &session{
		localID:  "local-1",
		agentID:  "agent-1",
		log:      slog.New(slog.DiscardHandler),
		events:   make(chan event.Event, 1),
		done:     make(chan struct{}),
		attached: true,
	}
	// Full, and nothing reads it. deliver's non-blocking attempt fails and it
	// falls through to the bounded wait.
	s.events <- event.Event{Type: event.TypeSessionStatus}

	agentOut, agentIn := io.Pipe()
	conn := acp.NewClientSideConnection(s, io.Discard, agentOut, opts...)
	s.conn = conn
	// Wire the containment path production wires (acpagent.go:655). Without it a
	// dead transport produced no teardown here at all, so a test asserting
	// containment would have been asserting something this fixture could not do
	// (MADR 0166 F11, PLAN 0166 A10).
	go s.watchConnClose(conn)
	t.Cleanup(func() { _ = agentIn.Close() })
	return s, conn, agentIn
}

// TestAStalledPumpIsContainedNotHung is MADR 0166 D2. It replaces
// TestACPConnectionSurvivesAStalledPump, whose name was its claim.
//
// History worth keeping: this file began as MADR 0138 Phase 7's G2. 7.2 bounded
// the control send with a 30-second timer so a stalled consumer could not pin the
// SDK's single notification-consumer goroutine; driven for the first time here,
// that guard turned out to protect nothing, because with the consumer blocked the
// SDK tore the connection down in 7.16 ms. It was replaced by the parked overflow,
// which never blocks the consumer at all — and that mechanism is correct and
// unchanged.
//
// What changed is the claim made for it. **This test no longer asserts that the
// ACP connection survives**, because no client-side code can deliver that at
// acp-go-sdk v0.13.5. Measured under GOMAXPROCS=1, 5 trials: a handler that does
// NOTHING — discarding every session/update without touching deliver — still loses
// the transport 3 times in 5, with our stall detector never firing. The SDK's
// reader outpaces its single consumer, fills a 1024-deep queue that is an
// unexported constant, and closes the connection; its reader never blocks
// (connection.go:19, :108, :432, :446-447). We sit downstream of that queue, so
// there is nothing of ours left to make faster (MADR 0166 F6/F9/F10/F12).
//
// Asserting an unachievable property leaves a permanently red test, which teaches
// readers to ignore red tests. So this asserts the two things that ARE true and
// ARE ours: deliver absorbs up to the overflow cap without blocking, and the
// session ends up contained rather than hung or zombied.
//
// Do not reinstate the conn.Done() assertion without first re-measuring the
// null-handler case above. If upstream ever makes the reader block or the depth
// configurable, this test becomes an under-assertion — that is the moment to
// revisit it (PLAN 0166, Deferred).
//
// Revisited in MADR 0167: PLAN P4 re-measured the null-handler case against the
// overflow policy (20/20 survival), and the connection-survival property is now
// asserted by TestACPConnectionSurvivesAStalledPump below, with the options
// production builds. This test keeps asserting containment, which still matters:
// the policy is what keeps the transport up, containment is what ends a session
// whose consumer will never drain.
func TestAStalledPumpIsContainedNotHung(t *testing.T) {
	s, _, agentIn := stalledSession(t)

	// Comfortably past the SDK's queue depth.
	const frames = sdkNotificationQueueDepth + 200

	var written atomic.Int64
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for i := range frames {
			if _, err := agentIn.Write(toolCallFrame(s.agentID, i)); err != nil {
				return
			}
			written.Add(1)
		}
	}()

	// The writer is ALLOWED to end up blocked, and that is the key difference from
	// the assertion this replaces. agentIn is an unbuffered io.Pipe: once the SDK
	// stops reading, a Write blocks rather than erroring. If the transport dies,
	// a stuck writer is the consequence, not the defect. stalledSession's cleanup
	// closes the pipe, so the goroutine cannot outlive the test.
	//
	// Note what is NOT done here: the pipe is not closed when the session faults.
	// Closing it hands the SDK EOF and CAUSES a teardown, which is how an earlier
	// attempt at this test turned green for the wrong reason (MADR 0166 F5).
	//
	// 5s rather than something larger because waiting longer buys nothing. Once the
	// transport dies the writer is blocked for good, and the only thing the wait has
	// to outlast is the absorb threshold below — reached at 513 frames, while the
	// writer gets ~1025 in before it can block at all. An earlier 15s ceiling made
	// 20 starved runs take 285s instead of 100s, all of it sleeping.
	select {
	case <-writerDone:
	case <-time.After(5 * time.Second):
	}

	// (1) The guarantee that is genuinely ours: deliver PARKS what it cannot hand
	// over, rather than blocking on a consumer that will never drain (MADR 0166 D3).
	//
	// Measured on the overflow slice, not on frames written. Counting writes proves
	// nothing about deliver: the SDK's own 1024-deep queue accepts frames whether or
	// not our handler is making progress, so a blocking deliver still lets ~1025
	// frames reach the pipe. That version of this assertion passed with deliver
	// deliberately blocked — it was measuring the SDK's buffer, not our code.
	s.overflowMu.Lock()
	parked := len(s.overflow)
	s.overflowMu.Unlock()

	faulted := false
	select {
	case <-s.done:
		faulted = true
	default:
	}

	switch {
	case parked == 0:
		t.Errorf("deliver parked nothing (overflow is empty) after %d frames reached the pipe; "+
			"it blocked on the stalled consumer instead of parking", written.Load())
	case faulted && parked < controlOverflowCap:
		// The stall detector only fires at the cap, so if it fired, the cap was
		// reached — and the parked events must still be there to show it.
		t.Errorf("the session faulted but only %d events are parked, want at least %d (controlOverflowCap); "+
			"the absorb path and the stall detector disagree about what happened", parked, controlOverflowCap)
	}

	// (2) Containment: the session must end up on a teardown path, by either
	// route. The stall detector faulting it and a dead transport being turned into
	// a disconnect are both correct outcomes; what is forbidden is a session that
	// is still nominally alive after this storm.
	if !waitForContainment(s, 15*time.Second) {
		t.Error("the session was neither faulted nor disconnected after a stalled-pump storm: " +
			"it is still live with a consumer that will never drain, which is the zombie " +
			"this containment is supposed to prevent")
	}
}

// waitForContainment reports whether the session reached a teardown path.
//
// Two routes, both correct: markClosedAndKill closes done when the overflow cap is
// hit, and watchConnClose sets disconnected when the transport dies first. Which
// one fires depends on a race this test deliberately no longer cares about.
func waitForContainment(s *session, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for {
		select {
		case <-s.done:
			return true
		default:
		}
		s.mu.Lock()
		contained := s.disconnected || s.closed
		s.mu.Unlock()
		if contained {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestACPConnectionSurvivesAStalledPump is MADR 0167 D19: the assertion MADR 0166
// D1 retired, reinstated because it is now achievable.
//
// With the SDK's default policy a full notification queue closes the whole
// connection, taking every session on the engine with it; 0166 measured that
// even a handler doing nothing loses it 3 times in 5 under GOMAXPROCS=1. With
// OverflowDropNewest the reader drops the arriving notification and keeps
// reading, so the transport survives however far the consumer falls behind.
//
// Built from clientConnOptions(), the same options spawnAgent uses: removing the
// policy from production turns this test red, which is the point of sharing them.
// Seen red before it was relied on: with the option removed it failed under
// GOMAXPROCS=1 (PLAN 0167 P12, execution record).
func TestACPConnectionSurvivesAStalledPump(t *testing.T) {
	_, conn, agentIn := stalledSession(t, clientConnOptions()...)
	const frames = sdkNotificationQueueDepth + 200

	var written atomic.Int64
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for i := range frames {
			if _, err := agentIn.Write(toolCallFrame("agent-1", i)); err != nil {
				return
			}
			written.Add(1)
		}
	}()

	// With the policy the reader never stops, so every frame is read. A writer
	// still blocked here means the connection stopped reading: it closed.
	select {
	case <-writerDone:
	case <-time.After(10 * time.Second):
		t.Fatalf("only %d of %d frames were read: the connection stopped reading (closed=%v)",
			written.Load(), frames, isClosed(conn))
	}
	if n := written.Load(); n != frames {
		t.Fatalf("the writer stopped after %d of %d frames: the connection closed", n, frames)
	}

	// And it is still open once the storm is over, not merely slow to close.
	select {
	case <-conn.Done():
		t.Fatalf("the ACP connection closed under a stalled pump (%d notifications dropped); "+
			"the overflow policy is not in effect", conn.DroppedNotifications())
	case <-time.After(500 * time.Millisecond):
	}
}

func isClosed(conn *acp.ClientSideConnection) bool {
	select {
	case <-conn.Done():
		return true
	default:
		return false
	}
}

// TestStalledPumpFaultsTheSessionRatherThanDroppingTheEvent pins the half of
// 7.2 that is a choice rather than a mechanism.
//
// The bound could have dropped the event and carried on. It does not: a
// transcript missing a control event with no explanation is the failure MADR
// 0138 exists to fix, so the session is closed loudly instead.
func TestStalledPumpFaultsTheSessionRatherThanDroppingTheEvent(t *testing.T) {
	s := &session{
		localID:  "local-1",
		log:      slog.New(slog.DiscardHandler),
		events:   make(chan event.Event, 1),
		done:     make(chan struct{}),
		attached: true,
	}
	s.events <- event.Event{Type: event.TypeSessionStatus}

	// One past the cap: the parked queue fills, then the session faults.
	go func() {
		for i := range controlOverflowCap + 1 {
			s.deliver(event.Event{Type: event.TypeToolCall, ToolID: fmt.Sprintf("t%d", i)}, true)
		}
	}()

	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the overflow filled without faulting the session")
	}

	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if !closed {
		t.Fatal("done was closed but the session is not marked closed; a later deliver would still block")
	}
}

// TestControlDeliveryIsNotDelayedWhenTheConsumerIsHealthy is the other side of
// the bound: it must cost nothing on the path that matters.
func TestControlDeliveryIsNotDelayedWhenTheConsumerIsHealthy(t *testing.T) {
	s := &session{
		localID:  "local-1",
		log:      slog.New(slog.DiscardHandler),
		events:   make(chan event.Event, 4),
		done:     make(chan struct{}),
		attached: true,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 4 {
			s.deliver(event.Event{Type: event.TypeToolCall, ToolID: "t"}, true)
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a healthy consumer did not get its events straight through")
	}
	if len(s.events) != 4 {
		t.Fatalf("delivered %d of 4 events", len(s.events))
	}
	s.overflowMu.Lock()
	parked, spawned := len(s.overflow), s.overflowWake != nil
	s.overflowMu.Unlock()
	if parked != 0 {
		t.Fatalf("%d events were parked on a healthy session", parked)
	}
	if spawned {
		t.Fatal("a healthy session spawned a drainer goroutine; the overflow must stay cold")
	}
	select {
	case <-s.done:
		t.Fatal("a healthy session was faulted")
	default:
	}
}

// TestParkedControlEventsKeepTheirOrder is the correctness property option B
// rests on, and the one it could plausibly get wrong.
//
// Once anything is parked, a later event must not take the fast path and
// overtake it. The drainer therefore keeps the event it is delivering at the
// head of the queue until the send completes, so "overflow is empty" means
// "nothing is in flight" — not "nothing is waiting".
func TestParkedControlEventsKeepTheirOrder(t *testing.T) {
	const n = 200

	s := &session{
		localID:  "local-1",
		log:      slog.New(slog.DiscardHandler),
		events:   make(chan event.Event, 1),
		done:     make(chan struct{}),
		attached: true,
	}
	// Occupy the single slot so the first delivery has to park.
	s.events <- event.Event{Type: event.TypeSessionStatus, ToolID: "seed"}

	go func() {
		for i := range n {
			s.deliver(event.Event{Type: event.TypeToolCall, ToolID: fmt.Sprintf("t%d", i)}, true)
		}
	}()

	// Drain, skipping the seed, and check the sequence.
	deadline := time.After(15 * time.Second)
	got := make([]string, 0, n)
	for len(got) < n {
		select {
		case ev := <-s.events:
			if ev.ToolID == "seed" {
				continue
			}
			got = append(got, ev.ToolID)
		case <-deadline:
			t.Fatalf("only %d of %d events arrived", len(got), n)
		}
	}
	for i, id := range got {
		if want := fmt.Sprintf("t%d", i); id != want {
			t.Fatalf("event %d is %s, want %s — a parked event was overtaken", i, id, want)
		}
	}
}

// TestOverflowDrainerExitsWithTheSession pins that the goroutine deliver spawns
// does not outlive the session it serves.
func TestOverflowDrainerExitsWithTheSession(t *testing.T) {
	s := &session{
		localID:  "local-1",
		log:      slog.New(slog.DiscardHandler),
		events:   make(chan event.Event, 1),
		done:     make(chan struct{}),
		attached: true,
	}
	s.events <- event.Event{Type: event.TypeSessionStatus}
	s.deliver(event.Event{Type: event.TypeToolCall, ToolID: "t0"}, true)

	s.overflowMu.Lock()
	spawned := s.overflowWake != nil
	s.overflowMu.Unlock()
	if !spawned {
		t.Fatal("parking an event did not start the drainer")
	}

	before := runtime.NumGoroutine()
	s.markClosedAndKill()

	// The drainer selects on s.done from both of its waits, so it returns
	// promptly once the session ends.
	for range 200 {
		if runtime.NumGoroutine() < before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the drainer did not exit when the session closed")
}

package acpagent

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"runtime"
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
// full event channel and no consumer — the stalled pump.
func stalledSession(t *testing.T) (*session, *acp.ClientSideConnection, *io.PipeWriter) {
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
	conn := acp.NewClientSideConnection(s, io.Discard, agentOut)
	s.conn = conn
	t.Cleanup(func() { _ = agentIn.Close() })
	return s, conn, agentIn
}

// TestACPConnectionSurvivesAStalledPump is MADR 0138 Phase 7's G2 — the
// fail-first check that phase recorded as *not run*, and this file is it.
//
// 7.2 bounded the control send with a 30-second timer so a stalled consumer
// could not pin the SDK's single notification-consumer goroutine. Driven for
// the first time here, that guard turned out to protect nothing: with the
// consumer blocked the SDK tears the connection down in 7.16 ms, so the timer
// lost the race by three orders of magnitude. It is replaced by the parked
// overflow, which never blocks the consumer at all.
//
// The assertion is the same either way — the connection must survive — which is
// why this test is the one that told the difference.
func TestACPConnectionSurvivesAStalledPump(t *testing.T) {
	s, conn, agentIn := stalledSession(t)

	// Comfortably past the queue depth: if the guard does not fire, the SDK
	// closes the connection somewhere around frame 1024.
	const frames = sdkNotificationQueueDepth + 200

	writeErr := make(chan error, 1)
	go func() {
		for i := range frames {
			if _, err := agentIn.Write(toolCallFrame(s.agentID, i)); err != nil {
				writeErr <- err
				return
			}
		}
		writeErr <- nil
	}()

	// Every frame must be accepted. deliver parks what it cannot hand over and
	// returns immediately, so the SDK's reader is never starved and its queue
	// never fills.
	select {
	case err := <-writeErr:
		if err != nil {
			t.Fatalf("the SDK stopped reading after %d frames: %v", frames, err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the writer never finished; the SDK stopped reading, which means the connection went away")
	}

	// Past the overflow cap the session is faulted rather than growing without
	// bound — that is the stall detector, and it is the correct outcome here.
	select {
	case <-s.done:
	case <-time.After(10 * time.Second):
		t.Fatal("a permanently stalled consumer never faulted the session")
	}

	select {
	case <-conn.Done():
		t.Fatal("the ACP connection was torn down: a stalled pump took the engine's transport with it " +
			"(acp-go-sdk errNotificationQueueOverflow — MADR 0138 F5)")
	default:
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

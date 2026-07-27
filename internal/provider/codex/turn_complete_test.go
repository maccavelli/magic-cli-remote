package codex

import (
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// drain reads every event currently buffered in s.events without blocking.
func drain(s *session) []event.Event {
	out := []event.Event{}
	for {
		select {
		case ev := <-s.events:
			out = append(out, ev)
		default:
			return out
		}
	}
}

// TestCodexStopReasonMapping pins the MADR 0035 D5 mapping:
//
//	completed   -> end_turn
//	interrupted -> cancelled
//	failed      -> error
//
// Unknown values pass through (the reducer treats anything not in its set
// as an unknown stop reason worth surfacing as a system message).
func TestCodexStopReasonMapping(t *testing.T) {
	cases := map[string]string{
		"completed":   "end_turn",
		"interrupted": "cancelled",
		"failed":      "error",
		"inProgress":  "inProgress",
		"":            "",
		"unknown":     "unknown",
	}
	for in, want := range cases {
		if got := codexStopReason(in); got != want {
			t.Errorf("codexStopReason(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestEmitTurnCompleteCancelledEmitsCancelledReason pins the cancel path:
// "cancelled" is the canonical reason the mobile reducer turns into
// "Turn cancelled" exactly once.
func TestEmitTurnCompleteCancelledEmitsCancelledReason(t *testing.T) {
	s := &session{
		events: make(chan event.Event, 8),
		done:   make(chan struct{}),
		log:    silentLogger(),
	}
	defer close(s.done)

	s.emitTurnComplete("cancelled", "")

	events := drain(s)
	// Expect: turn_complete, session_status: idle (no TypeError on cancel).
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Type != event.TypeTurnComplete {
		t.Errorf("events[0] type = %s, want turn_complete", events[0].Type)
	}
	if events[0].StopReason != "cancelled" {
		t.Errorf("StopReason = %q, want cancelled", events[0].StopReason)
	}
	if events[1].Type != event.TypeSessionStatus || events[1].Status != "idle" {
		t.Errorf("events[1] = %+v, want session_status: idle", events[1])
	}
}

// TestEmitTurnCompleteErrorEmitsTypeError: when the turn failed and the
// engine surfaced a message, the message travels as a TypeError event
// between turn_complete and session_status: idle.
func TestEmitTurnCompleteErrorEmitsTypeError(t *testing.T) {
	s := &session{
		events: make(chan event.Event, 8),
		done:   make(chan struct{}),
		log:    silentLogger(),
	}
	defer close(s.done)

	s.emitTurnComplete("error", "model returned 500")

	events := drain(s)
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3 (turn_complete, error, session_status)", len(events))
	}
	if events[0].Type != event.TypeTurnComplete || events[0].StopReason != "error" {
		t.Errorf("events[0] = %+v", events[0])
	}
	if events[1].Type != event.TypeError {
		t.Errorf("events[1] type = %s, want error", events[1].Type)
	}
	if events[1].Error != "model returned 500" {
		t.Errorf("events[1].Error = %q", events[1].Error)
	}
	if events[2].Type != event.TypeSessionStatus {
		t.Errorf("events[2] type = %s, want session_status", events[2].Type)
	}
}

// TestEmitTurnCompleteErrorNoMessageStillEmitsTypeError pins MADR 0036 D1:
// an "error" stop is ALWAYS paired with a TypeError, even when the engine
// gave no message — a generic one is substituted.
//
// This reverses an earlier decision (skip the event, "no noise on empty
// errors"), which rested on the mobile reducer rendering a canonical
// "Turn ended (error)" line. It never did: the reducer had no 'error' arm, so
// that path printed a contentless "Turn ended (error)" and nothing else. The
// reducer now suppresses the stop line entirely and relies on this event, so
// skipping it here would make the failure completely silent.
func TestEmitTurnCompleteErrorNoMessageStillEmitsTypeError(t *testing.T) {
	s := &session{
		events: make(chan event.Event, 8),
		done:   make(chan struct{}),
		log:    silentLogger(),
	}
	defer close(s.done)

	s.emitTurnComplete("error", "")

	events := drain(s)
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3 (turn_complete, error, session_status)", len(events))
	}
	if events[0].Type != event.TypeTurnComplete || events[0].StopReason != "error" {
		t.Errorf("events[0] = %+v", events[0])
	}
	if events[1].Type != event.TypeError {
		t.Fatalf("events[1] type = %s, want error", events[1].Type)
	}
	if events[1].Error != genericTurnError {
		t.Errorf("events[1].Error = %q, want the generic fallback %q",
			events[1].Error, genericTurnError)
	}
	if events[2].Type != event.TypeSessionStatus {
		t.Errorf("events[2] type = %s", events[2].Type)
	}
}

// TestEmitTurnCompleteNonErrorStopKeepsNoErrorEvent: the D1 pairing is scoped
// to the "error" stop reason — a cancelled or normal stop with no message must
// not gain a spurious error event.
func TestEmitTurnCompleteNonErrorStopKeepsNoErrorEvent(t *testing.T) {
	for _, stop := range []string{"end_turn", "cancelled"} {
		s := &session{
			events: make(chan event.Event, 8),
			done:   make(chan struct{}),
			log:    silentLogger(),
		}
		s.emitTurnComplete(stop, "")
		events := drain(s)
		close(s.done)
		for _, ev := range events {
			if ev.Type == event.TypeError {
				t.Errorf("stop %q emitted a TypeError: %+v", stop, ev)
			}
		}
	}
}

// TestEmitTurnCompleteCompletedIsSilent: a normal completion is two events
// (turn_complete + session_status: idle) and nothing else. The mobile
// reducer treats StopReason=end_turn as a no-op for the transcript, so
// the user sees a clean turn end.
func TestEmitTurnCompleteCompletedIsSilent(t *testing.T) {
	s := &session{
		events: make(chan event.Event, 8),
		done:   make(chan struct{}),
		log:    silentLogger(),
	}
	defer close(s.done)

	s.emitTurnComplete("end_turn", "")

	events := drain(s)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn", events[0].StopReason)
	}
	if events[1].Status != "idle" {
		t.Errorf("events[1].Status = %q, want idle", events[1].Status)
	}
}

// TestTurnCompletedNotificationRoutesThroughEmitter: pins the
// notification handler in handleNotification routing through
// emitTurnComplete (MADR 0035 D5 — the single-emitter requirement).
// This is a wire-level test: the JSON shape comes from MADR 0028 §16.3
// and the live probe (docs/codex-spike-0.145.0/item-stream.json).
func TestTurnCompletedNotificationRoutesThroughEmitter(t *testing.T) {
	s := &session{
		events:  make(chan event.Event, 8),
		done:    make(chan struct{}),
		agentID: "thread-x",
		log:     silentLogger(),
	}
	defer close(s.done)

	// Drive the handler directly. params mirrors the live 0.145.0 wire shape.
	completed := `{
		"threadId": "thread-x",
		"turn": {
			"id": "turn-1",
			"items": [],
			"itemsView": "notLoaded",
			"status": "completed",
			"error": null,
			"startedAt": 1700000000,
			"completedAt": 1700000001,
			"durationMs": 1000
		}
	}`
	s.handleNotification("turn/completed", []byte(completed))

	events := drain(s)
	// turn_complete + session_status: idle; no TypeError.
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn (engine 'completed' must map to 'end_turn')", events[0].StopReason)
	}
}

// TestTurnCompletedFailedEmitsTypeError: on `failed`, the engine's
// turn.error.message is captured and emitted as a TypeError. This is
// the D5 fix for the bug where a `failed` turn reported nothing.
func TestTurnCompletedFailedEmitsTypeError(t *testing.T) {
	s := &session{
		events:  make(chan event.Event, 8),
		done:    make(chan struct{}),
		agentID: "thread-x",
		log:     silentLogger(),
	}
	defer close(s.done)

	failed := `{
		"threadId": "thread-x",
		"turn": {
			"id": "turn-1",
			"items": [],
			"itemsView": "notLoaded",
			"status": "failed",
			"error": {
				"message": "context window exceeded"
			},
			"startedAt": 1700000000,
			"completedAt": 1700000001,
			"durationMs": 1000
		}
	}`
	s.handleNotification("turn/completed", []byte(failed))

	events := drain(s)
	// turn_complete + TypeError + session_status: idle.
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	if events[0].StopReason != "error" {
		t.Errorf("StopReason = %q, want error", events[0].StopReason)
	}
	if events[1].Type != event.TypeError {
		t.Errorf("events[1] type = %s, want error", events[1].Type)
	}
	if events[1].Error != "context window exceeded" {
		t.Errorf("Error = %q, want 'context window exceeded'", events[1].Error)
	}
}

// TestTurnCompletedInterruptedEmitsCancelled: the engine's `interrupted`
// status maps to the daemon's `cancelled` stop reason. The mobile
// reducer then appends "Turn cancelled" exactly once.
func TestTurnCompletedInterruptedEmitsCancelled(t *testing.T) {
	s := &session{
		events:  make(chan event.Event, 8),
		done:    make(chan struct{}),
		agentID: "thread-x",
		log:     silentLogger(),
	}
	defer close(s.done)

	interrupted := `{
		"threadId": "thread-x",
		"turn": {
			"id": "turn-1",
			"items": [],
			"itemsView": "notLoaded",
			"status": "interrupted",
			"error": null,
			"startedAt": 1700000000,
			"completedAt": 1700000001,
			"durationMs": 1000
		}
	}`
	s.handleNotification("turn/completed", []byte(interrupted))

	events := drain(s)
	if events[0].StopReason != "cancelled" {
		t.Errorf("StopReason = %q, want cancelled", events[0].StopReason)
	}
}

// keep imports alive
var _ = time.Second

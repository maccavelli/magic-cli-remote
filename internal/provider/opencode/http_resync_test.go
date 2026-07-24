package opencode

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// resyncHarness wires a captureHost whose API serves a canned message-log
// response for GET /session/{id}/message. Tree/status/permission GETs return
// empty success so PR5 resync preflight does not fail the 0014 cases.
func resyncHarness(t *testing.T, messagesJSON string) (*captureHost, *httpSession) {
	t.Helper()
	h := &captureHost{}
	h.api = func(_ context.Context, method, path string, _, out any) error {
		if method != "GET" {
			t.Fatalf("unexpected API call %s %s", method, path)
		}
		switch {
		case strings.Contains(path, "/message"):
			return json.Unmarshal([]byte(messagesJSON), out)
		case strings.Contains(path, "/children"):
			return json.Unmarshal([]byte(`[]`), out)
		case strings.Contains(path, "/session/status"):
			return json.Unmarshal([]byte(`{}`), out)
		case strings.Contains(path, "/permission"):
			return json.Unmarshal([]byte(`[]`), out)
		default:
			return nil
		}
	}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	return h, s
}

func (h *captureHost) eventTypes() []event.Type {
	h.mu.Lock()
	defer h.mu.Unlock()
	types := make([]event.Type, 0, len(h.events))
	for _, e := range h.events {
		types = append(types, e.Type)
	}
	return types
}

// nowMS returns a unix-milli timestamp n seconds from now, for building
// engine message-log fixtures around a turnStartedAt.
func nowMS(offset time.Duration) int64 {
	return time.Now().Add(offset).UnixMilli()
}

// H4 core case: the SSE gap swallowed the whole tail of the turn — final text
// and session.idle. Resync must emit the missed text, end the turn, and emit
// the turn-end events, exactly in stream order.
func TestResyncRecoversMissedTurnEnd(t *testing.T) {
	completed := nowMS(0)
	h, s := resyncHarness(t, `[
		{"info":{"id":"msg_u","role":"user"},"parts":[{"id":"p0","type":"text","text":"hi"}]},
		{"info":{"id":"msg_a","role":"assistant","time":{"created":`+jsonInt(completed-500)+`,"completed":`+jsonInt(completed)+`}},
		 "parts":[{"id":"prt_t","type":"text","text":"Hello world"}]}
	]`)

	// The stream delivered "Hello" before dropping.
	s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"msg_a","role":"assistant"}}`))
	s.HandleEvent("message.part.updated", json.RawMessage(`{
		"part":{"id":"prt_t","messageID":"msg_a","type":"text","text":"Hello"}
	}`))

	s.Resync(context.Background(), time.UnixMilli(completed).Add(-time.Minute))

	if got := h.texts(event.TypeAssistantChunk); got != "Hello world" {
		t.Fatalf("assistant text=%q want %q", got, "Hello world")
	}
	if h.endTurnCount() != 1 {
		t.Fatalf("EndTurn calls = %d, want 1", h.endTurnCount())
	}
	types := h.eventTypes()
	n := len(types)
	if n < 3 || types[n-2] != event.TypeTurnComplete || types[n-1] != event.TypeSessionStatus {
		t.Fatalf("tail events = %v, want …, turn_complete, session_status", types)
	}
	last := func(typ event.Type) event.Event {
		h.mu.Lock()
		defer h.mu.Unlock()
		for i := len(h.events) - 1; i >= 0; i-- {
			if h.events[i].Type == typ {
				return h.events[i]
			}
		}
		return event.Event{}
	}
	if ev := last(event.TypeTurnComplete); ev.StopReason != "end_turn" {
		t.Fatalf("stop_reason=%q want end_turn", ev.StopReason)
	}
	if ev := last(event.TypeSessionStatus); ev.Status != "idle" {
		t.Fatalf("status=%q want idle", ev.Status)
	}
}

// A turn the engine still reports as streaming must be left alone: in-stream
// snapshots heal live turns, and acting on a moving turn risks duplicates.
func TestResyncLeavesLiveTurnAlone(t *testing.T) {
	h, s := resyncHarness(t, `[
		{"info":{"id":"msg_a","role":"assistant","time":{"created":`+jsonInt(nowMS(0))+`}},
		 "parts":[{"id":"prt_t","type":"text","text":"partial text so far"}]}
	]`)
	s.Resync(context.Background(), time.Now().Add(-time.Minute))
	if h.endTurnCount() != 0 || len(h.eventTypes()) != 0 {
		t.Fatalf("live turn was touched: endTurns=%d events=%v", h.endTurnCount(), h.eventTypes())
	}
}

// Evidence from a PREVIOUS turn (finished before turnStartedAt) must be
// ignored — otherwise a resync racing a just-accepted prompt would end the
// new turn with the old turn's completion.
func TestResyncIgnoresStaleEvidence(t *testing.T) {
	completed := nowMS(-time.Hour)
	h, s := resyncHarness(t, `[
		{"info":{"id":"msg_a","role":"assistant","time":{"created":`+jsonInt(completed-500)+`,"completed":`+jsonInt(completed)+`}},
		 "parts":[{"id":"prt_t","type":"text","text":"old turn"}]}
	]`)
	s.Resync(context.Background(), time.Now())
	if h.endTurnCount() != 0 || len(h.eventTypes()) != 0 {
		t.Fatalf("stale evidence acted on: endTurns=%d events=%v", h.endTurnCount(), h.eventTypes())
	}
}

// Last message still the user's: the reply has not started, nothing to do.
func TestResyncIgnoresPendingUserMessage(t *testing.T) {
	h, s := resyncHarness(t, `[
		{"info":{"id":"msg_u","role":"user","time":{"created":`+jsonInt(nowMS(0))+`}},
		 "parts":[{"id":"p0","type":"text","text":"hi"}]}
	]`)
	s.Resync(context.Background(), time.Now().Add(-time.Minute))
	if h.endTurnCount() != 0 || len(h.eventTypes()) != 0 {
		t.Fatalf("pending user message acted on: endTurns=%d events=%v", h.endTurnCount(), h.eventTypes())
	}
}

// A turn aborted during the gap (Stop raced the drop) ends as cancelled, like
// the lost session.error MessageAbortedError frame would have.
func TestResyncRecoversAbortedTurn(t *testing.T) {
	h, s := resyncHarness(t, `[
		{"info":{"id":"msg_a","role":"assistant",
		  "time":{"created":`+jsonInt(nowMS(0))+`},
		  "error":{"name":"MessageAbortedError"}},
		 "parts":[]}
	]`)
	s.Resync(context.Background(), time.Now().Add(-time.Minute))
	if h.endTurnCount() != 1 {
		t.Fatalf("EndTurn calls = %d, want 1", h.endTurnCount())
	}
	types := h.eventTypes()
	if len(types) != 2 || types[0] != event.TypeTurnComplete || types[1] != event.TypeSessionStatus {
		t.Fatalf("events = %v, want turn_complete, session_status", types)
	}
	h.mu.Lock()
	stop := h.events[0].StopReason
	h.mu.Unlock()
	if stop != "cancelled" {
		t.Fatalf("stop_reason=%q want cancelled", stop)
	}
}

// A turn that errored during the gap surfaces the error, like the lost
// session.error frame would have.
func TestResyncRecoversErroredTurn(t *testing.T) {
	h, s := resyncHarness(t, `[
		{"info":{"id":"msg_a","role":"assistant",
		  "time":{"created":`+jsonInt(nowMS(0))+`},
		  "error":{"name":"UnknownError","data":{"message":"model exploded"}}},
		 "parts":[]}
	]`)
	s.Resync(context.Background(), time.Now().Add(-time.Minute))
	types := h.eventTypes()
	if len(types) != 2 || types[0] != event.TypeError || types[1] != event.TypeSessionStatus {
		t.Fatalf("events = %v, want error, session_status", types)
	}
	h.mu.Lock()
	errText, status := h.events[0].Error, h.events[1].Status
	h.mu.Unlock()
	if errText != "model exploded" {
		t.Fatalf("error=%q", errText)
	}
	if status != "error" {
		t.Fatalf("status=%q want error", status)
	}
}

// Text already fully streamed before the gap must not be re-emitted when only
// the idle frame was lost.
func TestResyncDoesNotDuplicateStreamedText(t *testing.T) {
	completed := nowMS(0)
	h, s := resyncHarness(t, `[
		{"info":{"id":"msg_a","role":"assistant","time":{"created":`+jsonInt(completed-500)+`,"completed":`+jsonInt(completed)+`}},
		 "parts":[{"id":"prt_t","type":"text","text":"Hello"}]}
	]`)
	s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"msg_a","role":"assistant"}}`))
	s.HandleEvent("message.part.updated", json.RawMessage(`{
		"part":{"id":"prt_t","messageID":"msg_a","type":"text","text":"Hello"}
	}`))
	streamed := h.texts(event.TypeAssistantChunk)

	s.Resync(context.Background(), time.UnixMilli(completed).Add(-time.Minute))
	if got := h.texts(event.TypeAssistantChunk); got != streamed {
		t.Fatalf("resync duplicated text: %q -> %q", streamed, got)
	}
}

func jsonInt(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

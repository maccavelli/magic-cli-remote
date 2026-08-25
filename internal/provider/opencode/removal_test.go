package opencode

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// removalSession builds a dialect session over a capture host.
func removalSession(t *testing.T) (*captureHost, *httpSession) {
	t.Helper()
	h := &captureHost{}
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	return h, d.NewSession(h).(*httpSession)
}

// capturedEvents copies what the host recorded.
func capturedEvents(h *captureHost) []event.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]event.Event, len(h.events))
	copy(out, h.events)
	return out
}

// TestMessageRemovedEmitsTombstone proves a retracted message becomes one
// message-scoped tombstone (MADR 0112 A3).
func TestMessageRemovedEmitsTombstone(t *testing.T) {
	h, s := removalSession(t)
	s.HandleEvent("message.removed", json.RawMessage(`{"sessionID":"ses_1","messageID":"msg_a"}`))
	evs := capturedEvents(h)
	if len(evs) != 1 {
		t.Fatalf("events = %+v, want one tombstone", evs)
	}
	got := evs[0]
	if got.Type != event.TypeTranscriptRemove || got.NativeMessageID != "msg_a" || got.NativePartID != "" {
		t.Fatalf("tombstone = %+v", got)
	}
}

// TestPartRemovedEmitsPartScopedTombstone proves a part removal names its part,
// so the rest of the message survives.
func TestPartRemovedEmitsPartScopedTombstone(t *testing.T) {
	h, s := removalSession(t)
	s.HandleEvent("message.part.removed",
		json.RawMessage(`{"sessionID":"ses_1","messageID":"msg_a","partID":"prt_1"}`))
	evs := capturedEvents(h)
	if len(evs) != 1 {
		t.Fatalf("events = %+v", evs)
	}
	if evs[0].NativeMessageID != "msg_a" || evs[0].NativePartID != "prt_1" {
		t.Fatalf("tombstone = %+v", evs[0])
	}
}

// TestRemovalIgnoresIncompleteFrames proves a frame without the ids it needs
// emits nothing rather than a tombstone that would match everything.
func TestRemovalIgnoresIncompleteFrames(t *testing.T) {
	for _, frame := range []struct{ typ, body string }{
		{"message.removed", `{"sessionID":"ses_1"}`},
		{"message.removed", `{"messageID":""}`},
		{"message.part.removed", `{"sessionID":"ses_1","messageID":"msg_a"}`},
		{"message.part.removed", `{"sessionID":"ses_1","partID":"prt_1"}`},
		{"message.removed", `not json`},
	} {
		h, s := removalSession(t)
		s.HandleEvent(frame.typ, json.RawMessage(frame.body))
		if evs := capturedEvents(h); len(evs) != 0 {
			t.Fatalf("%s %s emitted %+v", frame.typ, frame.body, evs)
		}
	}
}

// TestPartRemovedForgetsAccumulatedText proves a removed part's streamed text
// is dropped, so a later snapshot for a reused id is not compared against text
// that no longer exists.
func TestPartRemovedForgetsAccumulatedText(t *testing.T) {
	h, s := removalSession(t)
	s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"msg_a","role":"assistant"}}`))
	s.HandleEvent("message.part.delta",
		json.RawMessage(`{"messageID":"msg_a","partID":"prt_1","field":"text","delta":"Hello"}`))
	s.HandleEvent("message.part.removed",
		json.RawMessage(`{"sessionID":"ses_1","messageID":"msg_a","partID":"prt_1"}`))
	s.mu.Lock()
	_, stillTracked := s.partText["prt_1"]
	s.mu.Unlock()
	if stillTracked {
		t.Fatal("removed part kept its accumulated text")
	}
	_ = h
}

// TestCompactionEmitsNoticeAndNoReplay is the anti-growth rule: compaction must
// not re-fetch history, or resume appends the conversation again (A3).
func TestCompactionEmitsNoticeAndNoReplay(t *testing.T) {
	h := newRecorder()
	s := newOpsSession(h)
	s.HandleEvent("session.compacted", json.RawMessage(`{"sessionID":"ses_1"}`))

	var notices int
	for _, ev := range h.events {
		switch ev.Type {
		case event.TypeNotice:
			notices++
		case event.TypeUserMessage, event.TypeAssistantChunk, event.TypeThoughtChunk, event.TypeToolCall:
			t.Fatalf("compaction produced transcript content: %+v", ev)
		}
	}
	if notices != 1 {
		t.Fatalf("notices = %d, want exactly one bounded notice", notices)
	}
	for _, c := range h.calls {
		if c.method == "GET" {
			t.Fatalf("compaction fetched %s — it must perform no history fetch", c.path)
		}
	}
}

// TestTextSnapshotSupersedesDeltas proves a full part.updated frame arrives as
// an authoritative replacement carrying the whole text, not a computed delta.
func TestTextSnapshotSupersedesDeltas(t *testing.T) {
	h, s := removalSession(t)
	s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"msg_a","role":"assistant"}}`))
	s.HandleEvent("message.part.delta",
		json.RawMessage(`{"messageID":"msg_a","partID":"prt_1","field":"text","delta":"Hel"}`))
	s.HandleEvent("message.part.updated", json.RawMessage(`{
		"part":{"id":"prt_1","messageID":"msg_a","type":"text","text":"Hello world"}}`))

	evs := capturedEvents(h)
	var delta, snapshot *event.Event
	for i := range evs {
		if evs[i].Type != event.TypeAssistantChunk {
			continue
		}
		if evs[i].Replace {
			snapshot = &evs[i]
		} else {
			delta = &evs[i]
		}
	}
	if delta == nil || delta.Text != "Hel" || delta.NativePartID != "prt_1" {
		t.Fatalf("delta = %+v", delta)
	}
	if snapshot == nil || snapshot.Text != "Hello world" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.NativeMessageID != "msg_a" || snapshot.NativePartID != "prt_1" {
		t.Fatalf("snapshot lost identity: %+v", snapshot)
	}
}

// TestStaleSnapshotDoesNotShortenVisibleText proves a snapshot lagging the
// deltas already streamed is dropped rather than truncating the reply.
func TestStaleSnapshotDoesNotShortenVisibleText(t *testing.T) {
	h, s := removalSession(t)
	s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"msg_a","role":"assistant"}}`))
	s.HandleEvent("message.part.delta",
		json.RawMessage(`{"messageID":"msg_a","partID":"prt_1","field":"text","delta":"Hello world"}`))
	s.HandleEvent("message.part.updated", json.RawMessage(`{
		"part":{"id":"prt_1","messageID":"msg_a","type":"text","text":"Hello"}}`))

	for _, ev := range capturedEvents(h) {
		if ev.Replace && ev.Text == "Hello" {
			t.Fatal("a stale snapshot was emitted and would truncate the reply")
		}
	}
}

// TestIdenticalSnapshotEmitsNothing proves a repeated snapshot is not re-sent.
func TestIdenticalSnapshotEmitsNothing(t *testing.T) {
	h, s := removalSession(t)
	s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"msg_a","role":"assistant"}}`))
	frame := json.RawMessage(`{"part":{"id":"prt_1","messageID":"msg_a","type":"text","text":"Hi"}}`)
	s.HandleEvent("message.part.updated", frame)
	s.HandleEvent("message.part.updated", frame)
	s.HandleEvent("message.part.updated", frame)

	n := 0
	for _, ev := range capturedEvents(h) {
		if ev.Type == event.TypeAssistantChunk {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("identical snapshots emitted %d times, want 1", n)
	}
}

// TestBlankSnapshotIsNotEmitted proves a whitespace-only snapshot does not open
// an empty bubble.
func TestBlankSnapshotIsNotEmitted(t *testing.T) {
	h, s := removalSession(t)
	s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"msg_a","role":"assistant"}}`))
	s.HandleEvent("message.part.updated",
		json.RawMessage(`{"part":{"id":"prt_1","messageID":"msg_a","type":"text","text":"   "}}`))
	for _, ev := range capturedEvents(h) {
		if ev.Type == event.TypeAssistantChunk {
			t.Fatalf("a blank snapshot opened a bubble: %+v", ev)
		}
	}
}

// TestReplayFallsBackToMessageInfoID proves a part without its own messageID
// still gets the identity of the message it belongs to, so replay rows are
// never anonymous.
func TestReplayFallsBackToMessageInfoID(t *testing.T) {
	const log = `[
		{"info":{"id":"msg_a","role":"assistant"},"parts":[
			{"id":"prt_1","type":"text","text":"hello"}
		]}
	]`
	h := newRecorder(route{"/message", log})
	s := newOpsSession(h)
	s.Replay(context.Background())

	found := false
	for _, ev := range h.events {
		if ev.Type == event.TypeAssistantChunk {
			found = true
			if ev.NativeMessageID != "msg_a" || ev.NativePartID != "prt_1" {
				t.Fatalf("replayed row identity = %+v", ev)
			}
			if !ev.Replace {
				t.Fatal("a replayed row was not marked authoritative")
			}
		}
	}
	if !found {
		t.Fatal("replay produced no assistant row")
	}
}

// TestResumeAdoptsUpstreamModel proves resume takes the engine's persisted
// model when the catalog still carries it.
func TestResumeAdoptsUpstreamModel(t *testing.T) {
	const info = `{"id":"ses_1","model":{"providerID":"opencode","id":"m","variant":"high"}}`
	h := newRecorder(route{"/session/", info})
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	d.surfaces.replace(map[string]modelSurface{
		"opencode/m": {Levels: []picker.ThinkingLevel{{ID: "high"}}},
	})
	s := d.NewSession(h).(*httpSession)
	if _, err := s.Resume(context.Background(), "ses_1"); err != nil {
		t.Fatal(err)
	}
	if got := h.Model(); got != "opencode/m" {
		t.Fatalf("model after resume = %q, want opencode/m", got)
	}
	if got := s.ThinkingLevel(); got != "high" {
		t.Fatalf("upstream variant not adopted: %q", got)
	}
}

// TestResumeKeepsActiveModelWhenUpstreamIsUnknown proves an upstream model the
// catalog dropped does not pin the session to an id the engine would reject.
func TestResumeKeepsActiveModelWhenUpstreamIsUnknown(t *testing.T) {
	const info = `{"id":"ses_1","model":{"providerID":"opencode","id":"vanished"}}`
	h := newRecorder(route{"/session/", info})
	h.model = "opencode/known"
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	d.surfaces.replace(map[string]modelSurface{"opencode/known": {}})
	s := d.NewSession(h).(*httpSession)
	if _, err := s.Resume(context.Background(), "ses_1"); err != nil {
		t.Fatal(err)
	}
	if got := h.Model(); got != "opencode/known" {
		t.Fatalf("model after resume = %q, want the active model retained", got)
	}
}

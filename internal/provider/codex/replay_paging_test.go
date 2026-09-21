package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// replayedEvent takes the next buffered replay event, or fails.
//
// Deliberately non-blocking. emitThreadReplayPage emits synchronously before it
// returns, so anything it produced is already queued and there is nothing to wait
// for. A bare `<-s.events` would instead block forever when a regression stops
// emitting — which is how the first version of this file behaved: a mutation test
// that should have failed in milliseconds hung until the package timeout, and
// reported a deadlock panic rather than the assertion that actually broke.
func replayedEvent(t *testing.T, s *session, what string) event.Event {
	t.Helper()
	select {
	case ev := <-s.events:
		return ev
	default:
		t.Fatalf("no %s replay event was emitted", what)
		return event.Event{}
	}
}

// TestReplayPageUnwrapsItemEntries is acceptance criterion A25 of PLAN 0163.
//
// thread/items/list returns ThreadItemEntry values — {turnId, item} — not bare
// items. The failure this guards is silent rather than loud: decoding an entry as
// though it were an item succeeds, leaves every field zero, and matches no case in
// the render switch, so the transcript comes back empty with no error anywhere.
func TestReplayPageUnwrapsItemEntries(t *testing.T) {
	s := modeTestSession(t, Config{})
	raw := json.RawMessage(`{"data":[
		{"turnId":"turn-1","item":{"type":"userMessage","id":"u1","content":[{"type":"text","text":"hello"}]}},
		{"turnId":"turn-1","item":{"type":"agentMessage","id":"a1","text":"world"}}
	],"nextCursor":"cursor-2"}`)

	next, err := s.emitThreadReplayPage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if next != "cursor-2" {
		t.Errorf("nextCursor = %q, want cursor-2", next)
	}

	first := replayedEvent(t, s, "user message")
	second := replayedEvent(t, s, "agent message")
	// Order is the wire order, because replay asks for sortDirection asc and a
	// transcript printed backwards is worse than one not printed at all.
	if first.Text != "hello" || second.Text != "world" {
		t.Errorf("replay text = %q then %q, want hello then world", first.Text, second.Text)
	}
	if !first.Replay || !second.Replay {
		t.Errorf("replay events must be marked Replay so they are persisted but not broadcast: %+v %+v", first, second)
	}
}

// TestReplayPageParamsAskForOldestFirst is acceptance criterion A26 of PLAN 0163.
//
// Ordering is asserted here rather than in the decoder tests because the decoder
// never sees it. thread/items/list happens to default to ascending, but
// thread/turns/list defaults to DESCENDING, so the moment anyone swaps one RPC for
// the other an implicit default silently reverses the transcript.
func TestReplayPageParamsAskForOldestFirst(t *testing.T) {
	first := replayPageParams("thread-1", "")
	if first["sortDirection"] != "asc" {
		t.Errorf("sortDirection = %v, want asc; replay must emit oldest first", first["sortDirection"])
	}
	if first["threadId"] != "thread-1" {
		t.Errorf("threadId = %v, want thread-1", first["threadId"])
	}
	if first["limit"] != uint32(replayPageSize) {
		t.Errorf("limit = %v (%T), want uint32(%d)", first["limit"], first["limit"], replayPageSize)
	}
	// A cursor must be absent on the first call, not empty: Codex treats an empty
	// cursor as a value rather than as "from the beginning".
	if _, present := first["cursor"]; present {
		t.Errorf("first page sent a cursor: %v", first["cursor"])
	}
	if next := replayPageParams("thread-1", "cursor-2"); next["cursor"] != "cursor-2" {
		t.Errorf("cursor = %v, want cursor-2 threaded into the next page", next["cursor"])
	}
}

// TestResumeCapturesBackwardsCursors is acceptance criterion A14 of PLAN 0163.
//
// Both cursors are non-experimental fields of ThreadResumeResponse (:450-461) and
// resume is the only place Codex offers them, so failing to read them means a
// second round trip later or no backwards hydration at all.
func TestResumeCapturesBackwardsCursors(t *testing.T) {
	s := modeTestSession(t, Config{})

	s.noteBackwardsCursors("turns-cursor", "items-cursor")
	turns, items := s.backwardsCursors()
	if turns != "turns-cursor" || items != "items-cursor" {
		t.Errorf("cursors = (%q, %q), want (turns-cursor, items-cursor)", turns, items)
	}

	// A thread with no paginated history sends neither; that must read as absent
	// rather than as a cursor, or the next request pages from a bogus anchor.
	s.noteBackwardsCursors("", "")
	if turns, items := s.backwardsCursors(); turns != "" || items != "" {
		t.Errorf("cursors = (%q, %q), want both empty", turns, items)
	}

	// Engine-supplied strings end up in our own request params, so they are bounded
	// like every other opaque cursor this package accepts.
	s.noteBackwardsCursors(strings.Repeat("t", 4096), strings.Repeat("i", 4096))
	if turns, _ := s.backwardsCursors(); len(turns) > 1024 {
		t.Errorf("turns cursor kept %d bytes, want it bounded to 1024", len(turns))
	}
}

// TestReplayPageStopsOnNullCursor proves the paging loop terminates. A null
// nextCursor is how Codex says "no more items"; treating it as a value would page
// forever against the same cursor.
func TestReplayPageStopsOnNullCursor(t *testing.T) {
	s := modeTestSession(t, Config{})
	for _, body := range []string{
		`{"data":[{"turnId":"t","item":{"type":"agentMessage","id":"a","text":"last"}}],"nextCursor":null}`,
		`{"data":[],"nextCursor":null}`,
	} {
		next, err := s.emitThreadReplayPage(json.RawMessage(body))
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if next != "" {
			t.Errorf("%s: nextCursor = %q, want empty so the loop ends", body, next)
		}
	}
}

// TestReplayPageRejectsOversizedPage keeps the bound that every other history
// decoder in this file applies, so a hostile or buggy engine cannot make replay
// allocate without limit.
func TestReplayPageRejectsOversizedPage(t *testing.T) {
	s := modeTestSession(t, Config{})
	entries := make([]json.RawMessage, 0, maxNativeThreadHistoryPage+1)
	for i := 0; i <= maxNativeThreadHistoryPage; i++ {
		entries = append(entries, json.RawMessage(`{"turnId":"t","item":{"type":"agentMessage","id":"a","text":"x"}}`))
	}
	body, err := json.Marshal(map[string]any{"data": entries, "nextCursor": nil})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.emitThreadReplayPage(body); err == nil {
		t.Errorf("a page of %d entries was accepted; the bound is %d", len(entries), maxNativeThreadHistoryPage)
	}
}

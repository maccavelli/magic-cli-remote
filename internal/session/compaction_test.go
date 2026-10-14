package session

import (
	"context"
	"reflect"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func compaction(id, status, summary string) event.Event {
	return event.Event{Type: event.TypeCompaction, CompactionID: id, CompactionStatus: status, CompactionSummary: summary}
}

func compactionsInHistory(mgr *Manager, id string) []string {
	var out []string
	for _, ev := range mgr.History(id) {
		if ev.Type == event.TypeCompaction {
			out = append(out, ev.CompactionID+"|"+ev.CompactionStatus+"|"+ev.CompactionSummary)
		}
	}
	return out
}

// TestCompactionIsKeptAndDeduplicatedPerID is PLAN 0190 K1 step 4: a
// compaction's states are kept in history for a client that reconnects, and a
// state identical to the previous one for the same ID is dropped, even with
// another compaction's state in between (MADR 0190 D2).
func TestCompactionIsKeptAndDeduplicatedPerID(t *testing.T) {
	mgr, p := newNameManager(t, nil)
	meta, err := mgr.Create(context.Background(), "metadata", provider.StartOptions{}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	sendEvent(p, compaction("c1", "in_progress", ""))
	sendEvent(p, compaction("c1", "in_progress", "")) // repeat: dropped
	sendEvent(p, compaction("c2", "in_progress", "")) // another ID: kept
	sendEvent(p, compaction("c1", "in_progress", "")) // c1 unchanged since its last: dropped
	sendEvent(p, compaction("c1", "completed", "Kept the parser fix."))
	sendEvent(p, compaction("c1", "completed", "Kept the parser fix."))              // repeat: dropped
	sendEvent(p, compaction("c1", "completed", "Kept the parser fix and the test.")) // new summary: kept

	got := compactionsInHistory(mgr, meta.ID)
	want := []string{
		"c1|in_progress|",
		"c2|in_progress|",
		"c1|completed|Kept the parser fix.",
		"c1|completed|Kept the parser fix and the test.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("compactions in history = %v\nwant %v", got, want)
	}
}

// TestCompactionDedupeIsBounded: the IDs come from the agent, so the per-ID
// memory is capped; past the cap it starts again, which can only let a
// repeat through, never drop a change.
func TestCompactionDedupeIsBounded(t *testing.T) {
	e := &entry{}
	for i := range maxCompactionStates * 3 {
		ev := compaction("id", "in_progress", "")
		ev.CompactionID = string(rune('a'+i%26)) + string(rune('a'+i/26))
		e.compactionChangedLocked(&ev)
	}
	if n := len(e.compactionSeen); n > maxCompactionStates {
		t.Fatalf("remembering %d compaction IDs, want at most %d", n, maxCompactionStates)
	}
}

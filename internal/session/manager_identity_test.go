package session

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// hist reduces a ring to a comparable shape.
func hist(e *entry) []string {
	out := make([]string, 0, len(e.history))
	for _, h := range e.history {
		out = append(out, string(h.Type)+":"+h.NativeMessageID+"/"+h.NativePartID+":"+h.Text)
	}
	return out
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSnapshotReplacesPriorDeltas is the anti-duplication rule: replay repeats
// text already streamed, so a snapshot must supersede the deltas rather than
// append after them (MADR 0112 A3, PLAN P4 step 7).
func TestSnapshotReplacesPriorDeltas(t *testing.T) {
	e := &entry{}
	for _, delta := range []string{"Hel", "lo ", "world"} {
		ev := event.Event{Type: event.TypeAssistantChunk, Text: delta,
			NativeMessageID: "m1", NativePartID: "p1"}
		e.appendHistoryLocked(&ev)
	}
	if len(e.history) != 3 {
		t.Fatalf("deltas = %v", hist(e))
	}
	snap := event.Event{Type: event.TypeAssistantChunk, Text: "Hello world",
		NativeMessageID: "m1", NativePartID: "p1", Replace: true}
	e.appendHistoryLocked(&snap)

	want := []string{"assistant_message_chunk:m1/p1:Hello world"}
	if got := hist(e); !eqStrings(got, want) {
		t.Fatalf("history = %v, want %v", got, want)
	}
	// Sequence numbers keep advancing; gaps left by deletion are valid.
	if e.history[0].Seq != 4 {
		t.Fatalf("snapshot seq = %d, want 4", e.history[0].Seq)
	}
}

// TestSnapshotOnlyTouchesItsOwnPart proves reconciliation is scoped: another
// part of the same message, and another message, are untouched.
func TestSnapshotOnlyTouchesItsOwnPart(t *testing.T) {
	e := &entry{}
	for _, ev := range []event.Event{
		{Type: event.TypeAssistantChunk, Text: "a", NativeMessageID: "m1", NativePartID: "p1"},
		{Type: event.TypeAssistantChunk, Text: "b", NativeMessageID: "m1", NativePartID: "p2"},
		{Type: event.TypeAssistantChunk, Text: "c", NativeMessageID: "m2", NativePartID: "p1"},
	} {
		ev := ev
		e.appendHistoryLocked(&ev)
	}
	snap := event.Event{Type: event.TypeAssistantChunk, Text: "A",
		NativeMessageID: "m1", NativePartID: "p1", Replace: true}
	e.appendHistoryLocked(&snap)

	want := []string{
		"assistant_message_chunk:m1/p2:b",
		"assistant_message_chunk:m2/p1:c",
		"assistant_message_chunk:m1/p1:A",
	}
	if got := hist(e); !eqStrings(got, want) {
		t.Fatalf("history = %v, want %v", got, want)
	}
}

// TestAuthoritativeUserPartReplacesOptimisticRow proves the optimistic row the
// daemon wrote before submission is superseded by the agent's own user part —
// the reason the message id is preassigned at all.
func TestAuthoritativeUserPartReplacesOptimisticRow(t *testing.T) {
	e := &entry{}
	optimistic := event.Event{Type: event.TypeUserMessage, Text: "hi", NativeMessageID: "m1"}
	e.appendHistoryLocked(&optimistic)
	authoritative := event.Event{Type: event.TypeUserMessage, Text: "hi",
		NativeMessageID: "m1", NativePartID: "p1", Replace: true}
	e.appendHistoryLocked(&authoritative)

	want := []string{"user_message:m1/p1:hi"}
	if got := hist(e); !eqStrings(got, want) {
		t.Fatalf("history = %v, want %v", got, want)
	}
}

// TestTombstoneRemovesAndIsRetained proves a removal deletes the content it
// names and keeps the tombstone, so a reconnecting client learns of it.
func TestTombstoneRemovesAndIsRetained(t *testing.T) {
	e := &entry{}
	for _, ev := range []event.Event{
		{Type: event.TypeAssistantChunk, Text: "gone", NativeMessageID: "m1", NativePartID: "p1"},
		{Type: event.TypeAssistantChunk, Text: "stays", NativeMessageID: "m1", NativePartID: "p2"},
	} {
		ev := ev
		e.appendHistoryLocked(&ev)
	}
	tomb := event.Event{Type: event.TypeTranscriptRemove, NativeMessageID: "m1", NativePartID: "p1"}
	e.appendHistoryLocked(&tomb)

	want := []string{
		"assistant_message_chunk:m1/p2:stays",
		"transcript_remove:m1/p1:",
	}
	if got := hist(e); !eqStrings(got, want) {
		t.Fatalf("history = %v, want %v", got, want)
	}
}

// TestMessageTombstoneRemovesEveryPart proves a message-scoped removal takes
// all its parts.
func TestMessageTombstoneRemovesEveryPart(t *testing.T) {
	e := &entry{}
	for _, ev := range []event.Event{
		{Type: event.TypeAssistantChunk, Text: "a", NativeMessageID: "m1", NativePartID: "p1"},
		{Type: event.TypeToolCall, Text: "b", NativeMessageID: "m1", NativePartID: "p2"},
		{Type: event.TypeAssistantChunk, Text: "keep", NativeMessageID: "m2", NativePartID: "p1"},
	} {
		ev := ev
		e.appendHistoryLocked(&ev)
	}
	tomb := event.Event{Type: event.TypeTranscriptRemove, NativeMessageID: "m1"}
	e.appendHistoryLocked(&tomb)

	want := []string{
		"assistant_message_chunk:m2/p1:keep",
		"transcript_remove:m1/:",
	}
	if got := hist(e); !eqStrings(got, want) {
		t.Fatalf("history = %v, want %v", got, want)
	}
}

// TestRemovalIsIdempotentForUnknownIDs proves a duplicate or late tombstone is
// harmless.
func TestRemovalIsIdempotentForUnknownIDs(t *testing.T) {
	e := &entry{}
	keep := event.Event{Type: event.TypeAssistantChunk, Text: "keep",
		NativeMessageID: "m1", NativePartID: "p1"}
	e.appendHistoryLocked(&keep)
	for i := 0; i < 3; i++ {
		tomb := event.Event{Type: event.TypeTranscriptRemove, NativeMessageID: "unknown", NativePartID: "nope"}
		e.appendHistoryLocked(&tomb)
	}
	if len(e.history) != 4 {
		t.Fatalf("history = %v", hist(e))
	}
	if e.history[0].Text != "keep" {
		t.Fatalf("unknown-id removal deleted real content: %v", hist(e))
	}
}

// TestLegacyRowsWithoutIdentityAreNeverMatched is the safety rule: every
// provider but OpenCode emits rows with no native ids, and they must never be
// removed or replaced by inference.
func TestLegacyRowsWithoutIdentityAreNeverMatched(t *testing.T) {
	e := &entry{}
	for _, ev := range []event.Event{
		{Type: event.TypeAssistantChunk, Text: "legacy-1"},
		{Type: event.TypeAssistantChunk, Text: "legacy-2"},
	} {
		ev := ev
		e.appendHistoryLocked(&ev)
	}
	// A tombstone with an empty message id must be inert, not a wildcard.
	tomb := event.Event{Type: event.TypeTranscriptRemove}
	e.appendHistoryLocked(&tomb)
	// And a real removal must not sweep up id-less rows.
	tomb2 := event.Event{Type: event.TypeTranscriptRemove, NativeMessageID: "m1"}
	e.appendHistoryLocked(&tomb2)

	kept := 0
	for _, h := range e.history {
		if h.Type == event.TypeAssistantChunk {
			kept++
		}
	}
	if kept != 2 {
		t.Fatalf("legacy rows were removed: %v", hist(e))
	}
}

// TestSnapshotWithoutPartIDDoesNotReconcile proves reconciliation needs both
// ids: a message-level snapshot must not delete the message's parts.
func TestSnapshotWithoutPartIDDoesNotReconcile(t *testing.T) {
	e := &entry{}
	part := event.Event{Type: event.TypeAssistantChunk, Text: "part",
		NativeMessageID: "m1", NativePartID: "p1"}
	e.appendHistoryLocked(&part)
	loose := event.Event{Type: event.TypeAssistantChunk, Text: "loose",
		NativeMessageID: "m1", Replace: true}
	e.appendHistoryLocked(&loose)
	if len(e.history) != 2 {
		t.Fatalf("history = %v, want both rows", hist(e))
	}
}

// TestRemovalHelpersRefuseAnEmptyMessageID covers the guards directly.
//
// appendHistoryLocked already checks the id before delegating, so these
// branches are unreachable through it. They are kept because the helpers are
// package-level and an empty id would otherwise be a wildcard that deletes
// every legacy row — the one failure mode identity-based removal must not have.
func TestRemovalHelpersRefuseAnEmptyMessageID(t *testing.T) {
	e := &entry{}
	for _, ev := range []event.Event{
		{Type: event.TypeAssistantChunk, Text: "legacy"},
		{Type: event.TypeUserMessage, Text: "optimistic", NativeMessageID: "m1"},
		{Type: event.TypeAssistantChunk, Text: "identified", NativeMessageID: "m1", NativePartID: "p1"},
	} {
		ev := ev
		e.appendHistoryLocked(&ev)
	}
	before := len(e.history)

	e.removeNativeLocked("", "")
	e.removeNativeLocked("", "p1")
	e.removeOptimisticUserLocked("")

	if len(e.history) != before {
		t.Fatalf("an empty message id deleted rows: %v", hist(e))
	}
}

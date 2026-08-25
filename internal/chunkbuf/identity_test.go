package chunkbuf

import (
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// idChunk is a streaming chunk carrying native identity.
func idChunk(text, msgID, partID string) event.Event {
	return event.Event{Type: event.TypeAssistantChunk, SessionID: "s1", Text: text,
		NativeMessageID: msgID, NativePartID: partID}
}

// collect folds events through a buffer and returns everything emitted,
// including the final flush.
func collect(b *Buffer, evs ...event.Event) []event.Event {
	var out []event.Event
	for _, ev := range evs {
		got, _, _ := b.Add(ev)
		out = append(out, got...)
	}
	if ev, ok := b.Drain(); ok {
		out = append(out, ev)
	}
	return out
}

// TestDeltasCoalesceWithinOnePart proves the coalescer still does its job when
// identity matches.
func TestDeltasCoalesceWithinOnePart(t *testing.T) {
	b := New(50*time.Millisecond, 1<<20)
	got := collect(b, idChunk("Hel", "m1", "p1"), idChunk("lo", "m1", "p1"), idChunk("!", "m1", "p1"))
	var text string
	for _, ev := range got {
		text += ev.Text
	}
	if text != "Hello!" {
		t.Fatalf("text = %q", text)
	}
	if len(got) > 2 {
		t.Fatalf("no coalescing happened: %d events", len(got))
	}
}

// TestDeltasDoNotCoalesceAcrossParts proves two parts of one message stay
// separate transcript components (PLAN P4 step 6).
func TestDeltasDoNotCoalesceAcrossParts(t *testing.T) {
	b := New(50*time.Millisecond, 1<<20)
	got := collect(b, idChunk("a", "m1", "p1"), idChunk("b", "m1", "p2"))
	for _, ev := range got {
		if ev.Text == "ab" {
			t.Fatal("text from two native parts was merged into one run")
		}
	}
}

// TestDeltasDoNotCoalesceAcrossMessages proves the same for two messages.
func TestDeltasDoNotCoalesceAcrossMessages(t *testing.T) {
	b := New(50*time.Millisecond, 1<<20)
	got := collect(b, idChunk("a", "m1", "p1"), idChunk("b", "m2", "p1"))
	for _, ev := range got {
		if ev.Text == "ab" {
			t.Fatal("text from two messages was merged")
		}
	}
}

// TestSnapshotIsNeverCoalesced is the correctness rule: a snapshot is the full
// text of a part, so concatenating it with deltas invents content.
func TestSnapshotIsNeverCoalesced(t *testing.T) {
	b := New(50*time.Millisecond, 1<<20)
	snap := idChunk("Hello world", "m1", "p1")
	snap.Replace = true
	got := collect(b, idChunk("Hel", "m1", "p1"), snap, idChunk("!", "m1", "p1"))

	for _, ev := range got {
		if ev.Replace && ev.Text != "Hello world" {
			t.Fatalf("a snapshot was merged: %q", ev.Text)
		}
		if !ev.Replace && ev.Text == "Hello world!" {
			t.Fatalf("a delta was appended onto a snapshot: %q", ev.Text)
		}
	}
	var sawSnapshot bool
	for _, ev := range got {
		if ev.Replace {
			sawSnapshot = true
		}
	}
	if !sawSnapshot {
		t.Fatalf("the snapshot was lost: %+v", got)
	}
}

// TestLegacyChunksWithoutIdentityStillCoalesce proves providers that emit no
// native ids keep the previous behaviour.
func TestLegacyChunksWithoutIdentityStillCoalesce(t *testing.T) {
	b := New(50*time.Millisecond, 1<<20)
	got := collect(b, idChunk("a", "", ""), idChunk("b", "", ""), idChunk("c", "", ""))
	var text string
	for _, ev := range got {
		text += ev.Text
	}
	if text != "abc" {
		t.Fatalf("text = %q", text)
	}
	if len(got) > 2 {
		t.Fatalf("legacy chunks stopped coalescing: %d events", len(got))
	}
}

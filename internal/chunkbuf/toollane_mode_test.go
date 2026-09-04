package chunkbuf

import (
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func appendBuf() *Buffer { return New(testWin, testMaxBytes, WithToolLaneAppend()) }

func TestAppendLaneKeepsEveryDelta(t *testing.T) {
	b := appendBuf()

	// Codex sends the *next* chunk each time. Replacing them loses a line of
	// the agent's command output (MADR 0138 F2).
	b.Add(toolUpdate("t1", event.ToolStatusRunning, "line 1\n", 0))
	b.Add(toolUpdate("t1", event.ToolStatusRunning, "line 2\n", time.Millisecond))
	b.Add(toolUpdate("t1", event.ToolStatusRunning, "line 3\n", 2*time.Millisecond))

	drained := b.DrainTools()
	if len(drained) != 1 {
		t.Fatalf("want 1 coalesced update, got %d", len(drained))
	}
	if got, want := drained[0].Text, "line 1\nline 2\nline 3\n"; got != want {
		t.Fatalf("text = %q, want %q — every delta must survive", got, want)
	}
}

func TestReplaceLaneStillSupersedes(t *testing.T) {
	// The default must not change: kilo, opencode and goose send the current
	// whole, and concatenating those would duplicate the output instead of
	// losing it.
	b := toolBuf()

	b.Add(toolUpdate("t1", event.ToolStatusRunning, "the whole output so far", 0))
	b.Add(toolUpdate("t1", event.ToolStatusRunning, "the whole output, longer", time.Millisecond))

	drained := b.DrainTools()
	if len(drained) != 1 {
		t.Fatalf("want 1 coalesced update, got %d", len(drained))
	}
	if got, want := drained[0].Text, "the whole output, longer"; got != want {
		t.Fatalf("text = %q, want %q — a snapshot provider must still supersede", got, want)
	}
}

func TestAppendLaneCarriesTextIntoATerminalUpdate(t *testing.T) {
	b := appendBuf()
	b.Add(toolUpdate("t1", event.ToolStatusRunning, "building...\n", 0))

	out, _, _ := b.Add(toolUpdate("t1", event.ToolStatusCompleted, "done\n", time.Millisecond))
	if len(out) != 1 {
		t.Fatalf("a terminal status must emit at once, got %d", len(out))
	}
	if got, want := out[0].Text, "building...\ndone\n"; got != want {
		t.Fatalf("text = %q, want %q — the terminal delta appends, it does not replace", got, want)
	}
}

func TestAppendLaneFlushesBeforeTheTextGrowsUnbounded(t *testing.T) {
	b := appendBuf()

	// In append mode the held text accumulates, so it must flush at the byte
	// cap rather than growing into an unbounded frame. Replace mode cannot
	// grow, which is why only this mode needs the guard.
	chunk := strings.Repeat("x", testMaxBytes/2+1)
	if out, _, _ := b.Add(toolUpdate("t1", event.ToolStatusRunning, chunk, 0)); len(out) != 0 {
		t.Fatalf("the first chunk is under the cap and must be held, got %d events", len(out))
	}
	out, _, blocking := b.Add(toolUpdate("t1", event.ToolStatusRunning, chunk, time.Millisecond))
	if len(out) != 1 {
		t.Fatalf("crossing the byte cap must flush, got %d events", len(out))
	}
	if !blocking {
		t.Fatal("tool events are control events and must be delivered blocking")
	}
	if got := len(out[0].Text); got != 2*len(chunk) {
		t.Fatalf("flushed text is %d bytes, want %d — the flush must not duplicate or drop", got, 2*len(chunk))
	}
	if held := b.DrainTools(); len(held) != 0 {
		t.Fatalf("the flushed hold must be gone, got %d", len(held))
	}
}

func TestAppendLaneKeepsDistinctToolsApart(t *testing.T) {
	b := appendBuf()
	b.Add(toolUpdate("t1", event.ToolStatusRunning, "a", 0))
	b.Add(toolUpdate("t2", event.ToolStatusRunning, "b", time.Millisecond))
	b.Add(toolUpdate("t1", event.ToolStatusRunning, "c", 2*time.Millisecond))

	drained := b.DrainTools()
	if len(drained) != 2 {
		t.Fatalf("want 2 updates, got %d", len(drained))
	}
	byID := map[string]string{}
	for _, ev := range drained {
		byID[ev.ToolID] = ev.Text
	}
	if byID["t1"] != "ac" {
		t.Fatalf("t1 text = %q, want %q", byID["t1"], "ac")
	}
	if byID["t2"] != "b" {
		t.Fatalf("t2 text = %q, want %q", byID["t2"], "b")
	}
}

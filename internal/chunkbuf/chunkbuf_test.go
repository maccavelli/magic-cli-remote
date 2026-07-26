package chunkbuf

import (
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

const (
	testWin      = 80 * time.Millisecond
	testMaxBytes = 64
)

var base = time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

func chunk(text string, at time.Duration) event.Event {
	return event.Event{
		Type:      event.TypeAssistantChunk,
		SessionID: "s1",
		Timestamp: base.Add(at),
		Text:      text,
	}
}

// texts concatenates the text of every event of typ, in order.
func texts(evs []event.Event, typ event.Type) string {
	var b strings.Builder
	for _, e := range evs {
		if e.Type == typ {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

func newBuf() *Buffer { return New(testWin, testMaxBytes) }

func TestLeadingEdgeEmitsVerbatim(t *testing.T) {
	b := newBuf()
	out, deadline, blocking := b.Add(chunk("He", 0))
	if len(out) != 1 || out[0].Text != "He" {
		t.Fatalf("first chunk must pass through verbatim, got %+v", out)
	}
	if blocking {
		t.Error("chunk delivery must not block")
	}
	if !deadline.IsZero() {
		t.Errorf("nothing buffered after the leading edge, got deadline %v", deadline)
	}
	if b.Pending() != 0 {
		t.Errorf("Pending = %d, want 0", b.Pending())
	}

	// The second chunk buffers and arms the window from its own arrival.
	out, deadline, _ = b.Add(chunk("llo", 10*time.Millisecond))
	if len(out) != 0 {
		t.Fatalf("second chunk must buffer, got %+v", out)
	}
	if want := base.Add(10*time.Millisecond + testWin); !deadline.Equal(want) {
		t.Errorf("deadline = %v, want %v", deadline, want)
	}
}

func TestAccumulationConcatenatesExactlyAndStampsRunStart(t *testing.T) {
	b := newBuf()
	b.Add(chunk("A", 0)) // leading edge, emitted
	for i, part := range []string{"B", "C", "D"} {
		if out, _, _ := b.Add(chunk(part, time.Duration(i+1)*time.Millisecond)); len(out) != 0 {
			t.Fatalf("part %q must buffer, got %+v", part, out)
		}
	}
	ev, ok := b.Drain()
	if !ok {
		t.Fatal("Drain reported nothing pending")
	}
	if ev.Text != "BCD" {
		t.Errorf("Text = %q, want %q", ev.Text, "BCD")
	}
	if want := base.Add(1 * time.Millisecond); !ev.Timestamp.Equal(want) {
		t.Errorf("Timestamp = %v, want the run start %v", ev.Timestamp, want)
	}
	if ev.SessionID != "s1" || ev.Type != event.TypeAssistantChunk {
		t.Errorf("merged event lost its identity: %+v", ev)
	}
	if _, ok := b.Drain(); ok {
		t.Error("Drain must empty the run")
	}
}

func TestSizeTriggerFlushesFromAdd(t *testing.T) {
	b := newBuf()
	b.Add(chunk("x", 0)) // leading edge

	var got []event.Event
	for range 20 {
		out, _, _ := b.Add(chunk(strings.Repeat("y", 8), time.Millisecond))
		got = append(got, out...)
		if len(got) > 0 {
			break
		}
	}
	if len(got) != 1 {
		t.Fatalf("crossing maxBytes must flush exactly once, got %d events", len(got))
	}
	if len(got[0].Text) < testMaxBytes {
		t.Errorf("flushed %d bytes, want >= maxBytes (%d)", len(got[0].Text), testMaxBytes)
	}
	if b.Pending() != 0 {
		t.Errorf("Pending = %d after a size flush, want 0", b.Pending())
	}
}

func TestBoundaryFlushesTailAheadOfItselfAndBlocks(t *testing.T) {
	b := newBuf()
	b.Add(chunk("Hi", 0)) // leading edge
	b.Add(chunk(" there", time.Millisecond))

	done := event.Event{Type: event.TypeTurnComplete, SessionID: "s1", StopReason: "end_turn"}
	out, deadline, blocking := b.Add(done)

	if len(out) != 2 {
		t.Fatalf("want [pending text, turn_complete], got %+v", out)
	}
	if out[0].Type != event.TypeAssistantChunk || out[0].Text != " there" {
		t.Errorf("out[0] = %+v, want the pending tail", out[0])
	}
	if out[1].Type != event.TypeTurnComplete {
		t.Errorf("out[1] = %+v, want turn_complete last", out[1])
	}
	if !blocking {
		t.Error("a boundary flush must be delivered with control guarantees")
	}
	if !deadline.IsZero() {
		t.Errorf("deadline = %v, want zero after a boundary", deadline)
	}

	// The boundary restores the leading edge: the next chunk goes out at once.
	out, _, _ = b.Add(chunk("next", 2*time.Millisecond))
	if len(out) != 1 || out[0].Text != "next" {
		t.Fatalf("boundary must restore the leading edge, got %+v", out)
	}
}

// TestTelemetryPassesThroughWithoutFragmentingTheRun pins the rule that makes
// the whole design work: usage_update arrives at chunk cadence on OpenCode, so
// treating it as a boundary would defeat coalescing entirely (MADR 0024 §1.1).
func TestTelemetryPassesThroughWithoutFragmentingTheRun(t *testing.T) {
	b := newBuf()
	b.Add(chunk("one", 0)) // leading edge
	b.Add(chunk(" two", time.Millisecond))

	usage := event.Event{
		Type:      event.TypeUsage,
		SessionID: "s1",
		Usage:     &event.Usage{Used: 10, Size: 100},
	}
	out, deadline, blocking := b.Add(usage)

	if len(out) != 1 || out[0].Type != event.TypeUsage {
		t.Fatalf("usage must pass straight through, got %+v", out)
	}
	if blocking {
		t.Error("usage_update is droppable telemetry; it must not block")
	}
	if b.Pending() != len(" two") {
		t.Errorf("Pending = %d, want the run left intact (%d)", b.Pending(), len(" two"))
	}
	if want := base.Add(time.Millisecond + testWin); !deadline.Equal(want) {
		t.Errorf("deadline = %v, want the run's original deadline %v", deadline, want)
	}

	b.Add(chunk(" three", 2*time.Millisecond))
	ev, _ := b.Drain()
	if ev.Text != " two three" {
		t.Errorf("Text = %q, want the run unbroken by the telemetry event", ev.Text)
	}
}

func TestTypeSwitchNeverReorders(t *testing.T) {
	b := newBuf()
	thought := func(text string, at time.Duration) event.Event {
		e := chunk(text, at)
		e.Type = event.TypeThoughtChunk
		return e
	}

	var out []event.Event
	add := func(ev event.Event) {
		got, _, _ := b.Add(ev)
		out = append(out, got...)
	}

	add(chunk("A1", 0))                    // leading edge -> emitted
	add(chunk("A2", time.Millisecond))     // buffers
	add(thought("T1", 2*time.Millisecond)) // type switch -> flushes A2, buffers T1
	add(thought("T2", 3*time.Millisecond))
	add(chunk("A3", 4*time.Millisecond)) // type switch -> flushes T1T2, buffers A3
	if ev, ok := b.Drain(); ok {
		out = append(out, ev)
	}

	want := []struct {
		typ  event.Type
		text string
	}{
		{event.TypeAssistantChunk, "A1"},
		{event.TypeAssistantChunk, "A2"},
		{event.TypeThoughtChunk, "T1"},
		{event.TypeThoughtChunk, "T2"},
		{event.TypeAssistantChunk, "A3"},
	}
	if len(out) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(out), len(want), out)
	}
	for i, w := range want {
		if out[i].Type != w.typ || out[i].Text != w.text {
			t.Errorf("out[%d] = (%s, %q), want (%s, %q)", i, out[i].Type, out[i].Text, w.typ, w.text)
		}
	}
	if got := texts(out, event.TypeAssistantChunk); got != "A1A2A3" {
		t.Errorf("assistant text = %q, want %q", got, "A1A2A3")
	}
	if got := texts(out, event.TypeThoughtChunk); got != "T1T2" {
		t.Errorf("thought text = %q, want %q", got, "T1T2")
	}
}

func TestMergeIncompatibilityForcesAFlush(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mutef func(*event.Event)
	}{
		{"agent session id", func(e *event.Event) { e.AgentSessionID = "other" }},
		{"replay flag", func(e *event.Event) { e.Replay = true }},
		{"session id", func(e *event.Event) { e.SessionID = "s2" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBuf()
			b.Add(chunk("lead", 0))
			b.Add(chunk("pending", time.Millisecond))

			next := chunk("fresh", 2*time.Millisecond)
			tc.mutef(&next)
			out, _, _ := b.Add(next)

			if len(out) != 2 || out[0].Text != "pending" || out[1].Text != "fresh" {
				t.Fatalf("incompatible: want [pending, fresh], got %+v", out)
			}
			if b.Pending() != 0 {
				t.Errorf("Pending = %d, want 0 (incompatible chunk was emitted immediately)", b.Pending())
			}
		})
	}
}

func TestUnflushRoundTripsInOrder(t *testing.T) {
	b := newBuf()
	b.Add(chunk("lead", 0))
	b.Add(chunk("one", time.Millisecond))

	first, _ := b.Drain()
	if dropped := b.Unflush(first); dropped != 0 {
		t.Errorf("dropped = %d, want 0", dropped)
	}
	if b.Pending() != len("one") {
		t.Fatalf("Pending = %d, want %d", b.Pending(), len("one"))
	}
	if got, _ := b.Drain(); got.Timestamp != first.Timestamp {
		t.Errorf("Timestamp = %v, want the run start restored (%v)", got.Timestamp, first.Timestamp)
	}

	// Two failed sends in a row must still reassemble in arrival order.
	b = newBuf()
	b.Add(chunk("lead", 0))
	b.Add(chunk("one", time.Millisecond))
	a, _ := b.Drain()
	b.Unflush(a)
	b.Add(chunk("two", 2*time.Millisecond))
	c, _ := b.Drain()
	b.Unflush(c)
	b.Add(chunk("three", 3*time.Millisecond))
	final, _ := b.Drain()
	if final.Text != "onetwothree" {
		t.Errorf("Text = %q, want %q", final.Text, "onetwothree")
	}
}

func TestUnflushGrowthGuardDropsOldestOnRuneBoundary(t *testing.T) {
	b := newBuf()
	limit := growthFactor * testMaxBytes

	// "é" is two bytes; a naive byte cut would land mid-rune.
	stuck := chunk(strings.Repeat("é", limit), 0)
	dropped := b.Unflush(stuck)
	if dropped == 0 {
		t.Fatal("the growth guard must fire well above the limit")
	}
	if b.Pending() > limit {
		t.Errorf("Pending = %d, want <= %d", b.Pending(), limit)
	}
	ev, _ := b.Drain()
	if !strings.HasPrefix(ev.Text, "é") {
		t.Errorf("guard cut mid-rune: text starts %q", ev.Text[:2])
	}
}

func TestDisabledReproducesPreChangeBehaviour(t *testing.T) {
	b := New(0, testMaxBytes)
	if b.Enabled() {
		t.Fatal("a zero window must disable coalescing")
	}
	for _, ev := range []event.Event{
		chunk("a", 0),
		chunk("b", time.Millisecond),
		{Type: event.TypeTurnComplete, SessionID: "s1"},
	} {
		out, deadline, blocking := b.Add(ev)
		if len(out) != 1 || out[0].Text != ev.Text || out[0].Type != ev.Type {
			t.Fatalf("disabled buffer must pass %+v through unchanged, got %+v", ev, out)
		}
		if !deadline.IsZero() {
			t.Error("a disabled buffer never has a deadline")
		}
		if blocking != event.IsControl(ev.Type) {
			t.Errorf("blocking = %v for %s, want IsControl", blocking, ev.Type)
		}
	}
	if b.Pending() != 0 {
		t.Errorf("Pending = %d, want 0", b.Pending())
	}
	if _, ok := b.Drain(); ok {
		t.Error("a disabled buffer holds nothing to drain")
	}
}

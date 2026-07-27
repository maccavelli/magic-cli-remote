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

func TestInPlaceUpdateNonBoundary(t *testing.T) {
	b := newBuf()
	b.Add(chunk("lead", 0))                        // leading edge, emitted
	b.Add(chunk("pending text", time.Millisecond)) // buffers

	updateWithID := event.Event{
		Type:      event.TypeToolUpdate,
		SessionID: "s1",
		ToolID:    "tool_123",
		Text:      "running",
	}

	out, _, blocking := b.Add(updateWithID)
	if len(out) != 1 || out[0].Type != event.TypeToolUpdate || out[0].ToolID != "tool_123" {
		t.Fatalf("in-place update must pass straight through without draining text run, got %+v", out)
	}
	if !blocking {
		t.Error("in-place update must still report blocking = true for control delivery guarantee")
	}
	if b.Pending() != len("pending text") {
		t.Errorf("Pending = %d, want pending text run untouched (%d)", b.Pending(), len("pending text"))
	}

	// Next chunk should buffer (leading stays false).
	out2, _, _ := b.Add(chunk(" more text", 2*time.Millisecond))
	if len(out2) != 0 {
		t.Fatalf("next chunk must buffer (leading stayed false), got %+v", out2)
	}

	ev, _ := b.Drain()
	if ev.Text != "pending text more text" {
		t.Errorf("drained text = %q, want unbroken run %q", ev.Text, "pending text more text")
	}

	// Update WITHOUT tool ID must preserve boundary behavior (drain + leading = true).
	b = newBuf()
	b.Add(chunk("lead", 0))
	b.Add(chunk("pending", time.Millisecond))
	updateNoID := event.Event{
		Type:      event.TypeToolUpdate,
		SessionID: "s1",
		ToolID:    "",
		Text:      "update",
	}
	out3, _, blocking3 := b.Add(updateNoID)
	if len(out3) != 2 || out3[0].Text != "pending" || out3[1].Type != event.TypeToolUpdate {
		t.Fatalf("id-less update must drain tail first: want [pending, update], got %+v", out3)
	}
	if !blocking3 {
		t.Error("id-less update must be blocking")
	}

	// Tool call (TypeToolCall) must ALWAYS be a boundary.
	b = newBuf()
	b.Add(chunk("lead", 0))
	b.Add(chunk("pending", time.Millisecond))
	toolCall := event.Event{
		Type:      event.TypeToolCall,
		SessionID: "s1",
		ToolID:    "tool_123",
	}
	out4, _, blocking4 := b.Add(toolCall)
	if len(out4) != 2 || out4[0].Text != "pending" || out4[1].Type != event.TypeToolCall {
		t.Fatalf("tool_call must be a boundary: want [pending, tool_call], got %+v", out4)
	}
	if !blocking4 {
		t.Error("tool_call must be blocking")
	}
}

// TestIsControlToolUpdate asserts IsControl(TypeToolUpdate) remains true so a
// future refactor cannot quietly make tool updates droppable (MADR 0034 §2.3,
// codex delta hazard).
func TestIsControlToolUpdate(t *testing.T) {
	if !event.IsControl(event.TypeToolUpdate) {
		t.Fatal("IsControl(TypeToolUpdate) MUST remain true to protect delta-emitting providers like Codex")
	}
}

// ── Tool lane (MADR 0042 D4) ────────────────────────────────────────────────

func toolBuf() *Buffer { return New(testWin, testMaxBytes, WithToolLane()) }

func toolUpdate(id, status, text string, at time.Duration) event.Event {
	return event.Event{
		Type:      event.TypeToolUpdate,
		SessionID: "s1",
		Timestamp: base.Add(at),
		ToolID:    id,
		Status:    status,
		Text:      text,
	}
}

func TestToolLaneHoldsNonTerminalUpdates(t *testing.T) {
	b := toolBuf()

	out, deadline, _ := b.Add(toolUpdate("t1", event.ToolStatusRunning, "line 1", 0))
	if len(out) != 0 {
		t.Fatalf("a non-terminal update must be held, got %d events", len(out))
	}
	if deadline.IsZero() {
		t.Fatal("holding an update must arm the flush deadline")
	}

	out, _, _ = b.Add(toolUpdate("t1", event.ToolStatusRunning, "line 2", time.Millisecond))
	if len(out) != 0 {
		t.Fatalf("the newer update supersedes rather than emits, got %d", len(out))
	}

	drained := b.DrainTools()
	if len(drained) != 1 {
		t.Fatalf("want 1 coalesced update, got %d", len(drained))
	}
	if drained[0].Text != "line 2" {
		t.Fatalf("want the latest payload, got %q", drained[0].Text)
	}
}

func TestToolLaneNeverHoldsATerminalStatus(t *testing.T) {
	b := toolBuf()
	b.Add(toolUpdate("t1", event.ToolStatusRunning, "building", 0))

	out, _, blocking := b.Add(toolUpdate("t1", event.ToolStatusCompleted, "done", time.Millisecond))
	if len(out) != 1 {
		t.Fatalf("a terminal status must emit at once, got %d events", len(out))
	}
	if out[0].Status != event.ToolStatusCompleted || out[0].Text != "done" {
		t.Fatalf("unexpected terminal event: %+v", out[0])
	}
	if !blocking {
		t.Fatal("tool events are control events and must be delivered blocking")
	}
	if got := b.DrainTools(); len(got) != 0 {
		t.Fatalf("the superseded hold must be gone, got %d", len(got))
	}
}

func TestToolLaneMergesFieldsForward(t *testing.T) {
	b := toolBuf()
	held := toolUpdate("t1", event.ToolStatusRunning, "partial", 0)
	held.ToolName = "npm test"
	held.ToolKind = "execute"
	b.Add(held)

	// A terminal update that omits the title/kind must not lose them: the
	// client keeps a field only when the incoming one is empty, so dropping the
	// superseded event outright would drop them for good.
	out, _, _ := b.Add(toolUpdate("t1", event.ToolStatusCompleted, "", time.Millisecond))
	if len(out) != 1 {
		t.Fatalf("want 1 event, got %d", len(out))
	}
	if out[0].ToolName != "npm test" || out[0].ToolKind != "execute" {
		t.Fatalf("fields not carried forward: %+v", out[0])
	}
	if out[0].Text != "partial" {
		t.Fatalf("an empty text must not erase the held one, got %q", out[0].Text)
	}
}

func TestToolLaneNeverHoldsAToolCall(t *testing.T) {
	b := toolBuf()
	call := event.Event{
		Type:      event.TypeToolCall,
		SessionID: "s1",
		Timestamp: base,
		ToolID:    "t1",
		Status:    event.ToolStatusPending,
	}
	out, _, blocking := b.Add(call)
	if len(out) != 1 || out[0].Type != event.TypeToolCall {
		t.Fatalf("a tool_call positions the item and must pass straight through, got %+v", out)
	}
	if !blocking {
		t.Fatal("tool_call is a boundary and must block")
	}
}

func TestToolLaneKeepsDistinctToolsApart(t *testing.T) {
	b := toolBuf()
	b.Add(toolUpdate("t1", event.ToolStatusRunning, "a", 0))
	b.Add(toolUpdate("t2", event.ToolStatusRunning, "b", time.Millisecond))
	b.Add(toolUpdate("t1", event.ToolStatusRunning, "a2", 2*time.Millisecond))

	drained := b.DrainTools()
	if len(drained) != 2 {
		t.Fatalf("want one pending state per tool, got %d", len(drained))
	}
	if drained[0].ToolID != "t1" || drained[0].Text != "a2" {
		t.Fatalf("t1 wrong: %+v", drained[0])
	}
	if drained[1].ToolID != "t2" || drained[1].Text != "b" {
		t.Fatalf("t2 wrong: %+v", drained[1])
	}
}

func TestToolLaneDrainsBeforeABoundary(t *testing.T) {
	b := toolBuf()
	b.Add(chunk("reply ", 0))
	b.Add(chunk("text", time.Millisecond))
	b.Add(toolUpdate("t1", event.ToolStatusRunning, "working", 2*time.Millisecond))

	done := event.Event{
		Type:      event.TypeTurnComplete,
		SessionID: "s1",
		Timestamp: base.Add(3 * time.Millisecond),
	}
	out, _, blocking := b.Add(done)
	if !blocking {
		t.Fatal("a boundary must block")
	}
	if len(out) == 0 || out[len(out)-1].Type != event.TypeTurnComplete {
		t.Fatalf("turn_complete must land last, got %+v", out)
	}
	var sawTool bool
	for _, e := range out[:len(out)-1] {
		if e.Type == event.TypeToolUpdate {
			sawTool = true
		}
	}
	if !sawTool {
		t.Fatal("a held tool update must land before turn_complete, not after it")
	}
	if got := b.DrainTools(); len(got) != 0 {
		t.Fatalf("the lane must be empty after a boundary, got %d", len(got))
	}
}

func TestToolLaneOffByDefault(t *testing.T) {
	b := newBuf()
	out, _, _ := b.Add(toolUpdate("t1", event.ToolStatusRunning, "x", 0))
	if len(out) != 1 {
		t.Fatalf("without the option an in-place update passes straight through, got %d", len(out))
	}
	if got := b.DrainTools(); got != nil {
		t.Fatalf("a disabled lane holds nothing, got %d", len(got))
	}
}

func TestToolLaneDoesNotDelayText(t *testing.T) {
	b := toolBuf()
	// The lane must not consume the text run's leading edge.
	out, _, _ := b.Add(toolUpdate("t1", event.ToolStatusRunning, "x", 0))
	if len(out) != 0 {
		t.Fatalf("held, got %d", len(out))
	}
	out, _, _ = b.Add(chunk("first token", time.Millisecond))
	if len(out) != 1 || out[0].Type != event.TypeAssistantChunk {
		t.Fatalf("first token after a boundary must still emit at once, got %+v", out)
	}
}

package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// replayEvent builds a distinct replayed event. Distinct matters: a real ACP
// session/load replays a whole conversation of *different* events into an empty
// ring, so nothing short-circuits the duplicate check. A first attempt at
// measuring this used repeated events, every one of which matched at index 0,
// and reported linear growth for a quadratic scan (MADR 0138 F15).
func replayEvent(i int) event.Event {
	return event.Event{
		Type:           event.TypeAssistantChunk,
		Text:           fmt.Sprintf("line %d %s", i, strings.Repeat("x", 40)),
		AgentSessionID: "agent-1",
		Replay:         true,
	}
}

func TestReplayDedupeIsNotQuadratic(t *testing.T) {
	// Small enough to stay inside the byte budget: an eviction would drop
	// events and make the "a second replay adds nothing" assertion measure the
	// budget instead of the dedupe. Eviction has its own test.
	const n = 700

	e := &entry{}
	for i := range n {
		ev := replayEvent(i)
		e.appendHistoryLocked(&ev)
	}
	if len(e.history) != n {
		t.Fatalf("distinct replayed events must all be kept: got %d, want %d", len(e.history), n)
	}
	// Replay the identical conversation again, which is what a reconnect does.
	for i := range n {
		ev := replayEvent(i)
		e.appendHistoryLocked(&ev)
	}
	if len(e.history) != n {
		t.Fatalf("a second replay must add nothing: got %d, want %d", len(e.history), n)
	}

	// With the index each event costs at most a constant number of field
	// comparisons. The linear scan this replaced cost n²/2 — 2,000,000 here,
	// and 185,966,000 at the 20,000 events measured in MADR 0138 F15.
	if max := uint64(4 * n); e.replayCompares > max {
		t.Fatalf("replay dedupe is scanning: %d field comparisons for %d events, want <= %d",
			e.replayCompares, 2*n, max)
	}
}

// TestReplayDedupeConfirmsCollisionsByField plants a wrong position under a new
// event's hash, standing in for a 64-bit collision that cannot be constructed on
// demand. The event must survive: dropping it would be the exact defect this
// work exists to fix — a user's message silently discarded during a replay.
func TestReplayDedupeConfirmsCollisionsByField(t *testing.T) {
	e := &entry{}

	first := event.Event{Type: event.TypeUserMessage, Text: "the stored one", Replay: true}
	e.appendHistoryLocked(&first)

	incoming := event.Event{Type: event.TypeUserMessage, Text: "a different message", Replay: true}
	// Point the incoming event's key at the stored, non-matching event.
	e.replayIndex[replayKey(&incoming)] = []int{0}

	e.appendHistoryLocked(&incoming)

	if len(e.history) != 2 {
		t.Fatalf("a hash hit that does not match on fields must not suppress the event: history has %d entries, want 2", len(e.history))
	}
	if e.history[1].Text != "a different message" {
		t.Fatalf("wrong event retained: %q", e.history[1].Text)
	}
	if e.replayCompares == 0 {
		t.Fatal("the collision was never confirmed by field comparison")
	}
}

func TestReplayDedupeSuppressesAGenuineDuplicate(t *testing.T) {
	e := &entry{}
	ev := event.Event{Type: event.TypeUserMessage, Text: "same", ToolID: "t", AgentSessionID: "a", Replay: true}
	dup := ev

	e.appendHistoryLocked(&ev)
	e.appendHistoryLocked(&dup)

	if len(e.history) != 1 {
		t.Fatalf("a genuine replayed duplicate must be suppressed: history has %d entries, want 1", len(e.history))
	}
}

func TestReplayIndexSurvivesATrim(t *testing.T) {
	e := &entry{}
	// Overflow the byte budget so an eviction shifts every remaining position.
	// Each event carries 64 KiB, so 800 of them are ~50 MiB against a 32 MiB
	// budget. A fixed count, not a loop on historyBytes: eviction always brings
	// the total back under budget, so that loop would never end.
	const big = 64 << 10
	const emitted = 800
	for i := range emitted {
		ev := event.Event{
			Type:      event.TypeToolUpdate,
			ToolID:    "exec-1",
			Text:      fmt.Sprintf("%06d|%s", i, strings.Repeat("z", big)),
			Timestamp: time.Unix(1788000000+int64(i), 0).UTC(),
		}
		e.appendHistoryLocked(&ev)
	}
	if e.historyBytes > historyBudgetBytes {
		t.Fatalf("ring is %d bytes, over the %d budget", e.historyBytes, historyBudgetBytes)
	}
	if len(e.history) >= emitted {
		t.Fatalf("nothing was evicted: %d of %d events retained at %d bytes",
			len(e.history), emitted, e.historyBytes)
	}

	// Every retained event must still be found by the index, at its new
	// position. A stale index is a wrong answer, not a slow one.
	for i := range e.history {
		probe := e.history[i]
		probe.Replay = true
		if !e.hasReplayDuplicateLocked(&probe) {
			t.Fatalf("event at position %d (seq %d) is not indexed after the trim", i, e.history[i].Seq)
		}
	}
}

func TestReplayIndexSurvivesANativeRemoval(t *testing.T) {
	e := &entry{}
	keep := event.Event{Type: event.TypeAssistantChunk, Text: "keep me", NativeMessageID: "m2"}
	e.appendHistoryLocked(&keep)
	doomed := event.Event{Type: event.TypeAssistantChunk, Text: "replace me", NativeMessageID: "m1", NativePartID: "p1"}
	e.appendHistoryLocked(&doomed)

	repl := event.Event{
		Type: event.TypeAssistantChunk, Text: "the replacement",
		NativeMessageID: "m1", NativePartID: "p1", Replace: true,
	}
	e.appendHistoryLocked(&repl)

	for i := range e.history {
		probe := e.history[i]
		probe.Replay = true
		if !e.hasReplayDuplicateLocked(&probe) {
			t.Fatalf("event at position %d is not indexed after a native removal", i)
		}
	}
	stale := doomed
	stale.Replay = true
	if e.hasReplayDuplicateLocked(&stale) {
		t.Fatal("a removed event is still reported as present")
	}
}

func TestSeededRingIsIndexed(t *testing.T) {
	// A restart seeds the ring from disk before any append. Without an index
	// built from that seed, a session/load would re-append the whole durable
	// transcript as new content.
	prior := []event.Event{
		{Type: event.TypeUserMessage, Text: "from disk", Seq: 1},
		{Type: event.TypeAssistantChunk, Text: "also from disk", Seq: 2},
	}
	e := &entry{history: prior, seq: 2}
	e.reindexHistoryLocked()

	again := event.Event{Type: event.TypeUserMessage, Text: "from disk", Replay: true}
	e.appendHistoryLocked(&again)

	if len(e.history) != 2 {
		t.Fatalf("a replay of seeded content must be suppressed: history has %d entries, want 2", len(e.history))
	}
}

// bigEvent is sized so a page of them exceeds historyMaxResponseBytes, which is
// what makes the byte budget shorten the page at all.
func bigEvent(i int) event.Event {
	return event.Event{
		Type:      event.TypeAssistantChunk,
		SessionID: "s1",
		Seq:       uint64(i + 1),
		Text:      strings.Repeat("y", 4096),
	}
}

func TestHistoryPageMarshalsEachEventOnce(t *testing.T) {
	const ring = 800

	hist := make([]event.Event, 0, ring)
	for i := range ring {
		hist = append(hist, bigEvent(i))
	}
	m := &Manager{sessions: map[string]*entry{"s1": {history: hist}}}

	calls := 0
	orig := historyMarshal
	historyMarshal = func(ev *event.Event) ([]byte, error) {
		calls++
		return json.Marshal(ev)
	}
	t.Cleanup(func() { historyMarshal = orig })

	page, truncated, next := m.HistoryPage("s1", 0, historyMaxPage)

	if len(page) == 0 {
		t.Fatal("the page is empty")
	}
	if !truncated {
		t.Fatal("a ring larger than the byte budget must report truncated")
	}
	if next != page[len(page)-1].Seq {
		t.Fatalf("next_since_seq %d does not match the last event %d", next, page[len(page)-1].Seq)
	}
	// One encode per candidate the budget considered — at most the window it
	// was handed. The shrink loop this replaced re-encoded a shrinking slice on
	// every step: 505 encodes producing 635 MB to return 102 events on the
	// operator's own data (MADR 0138 F14).
	if calls > ring {
		t.Fatalf("the byte budget encoded %d times for a %d-event window; want at most one pass", calls, ring)
	}
	if calls < len(page) {
		t.Fatalf("the byte budget encoded %d times but returned %d events", calls, len(page))
	}
}

func TestHistoryPageStaysUnderTheFrameBudget(t *testing.T) {
	hist := make([]event.Event, 0, 800)
	for i := range 800 {
		hist = append(hist, bigEvent(i))
	}
	m := &Manager{sessions: map[string]*entry{"s1": {history: hist}}}

	page, _, _ := m.HistoryPage("s1", 0, historyMaxPage)
	b, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal page: %v", err)
	}
	if len(b) > historyMaxResponseBytes {
		t.Fatalf("page is %d bytes, over the %d budget", len(b), historyMaxResponseBytes)
	}
	if len(page) < 2 {
		t.Fatalf("the budget shortened the page to %d events; it should fit many 4 KiB events in 512 KiB", len(page))
	}
}

func TestHistoryPageAlwaysMakesProgress(t *testing.T) {
	// One event larger than the whole frame budget must still be returned, or
	// the pager wedges at that seq forever.
	huge := event.Event{Type: event.TypeAssistantChunk, SessionID: "s1", Seq: 1,
		Text: strings.Repeat("z", historyMaxResponseBytes+4096)}
	m := &Manager{sessions: map[string]*entry{"s1": {history: []event.Event{huge, bigEvent(1)}}}}

	page, _, next := m.HistoryPage("s1", 0, historyMaxPage)
	if len(page) != 1 {
		t.Fatalf("want exactly the one oversized event, got %d", len(page))
	}
	if next != 1 {
		t.Fatalf("next_since_seq = %d, want 1 so the next request moves past it", next)
	}
}

func TestHistoryPagePagesFromASeqInsideAGap(t *testing.T) {
	// Deletions leave gaps in the sequence, so the start is "first seq greater
	// than since", not an equality match.
	hist := []event.Event{
		{Type: event.TypeUserMessage, Text: "a", Seq: 1},
		{Type: event.TypeUserMessage, Text: "b", Seq: 5},
		{Type: event.TypeUserMessage, Text: "c", Seq: 9},
	}
	m := &Manager{sessions: map[string]*entry{"s1": {history: hist}}}

	page, truncated, next := m.HistoryPage("s1", 3, 10)
	if len(page) != 2 || page[0].Seq != 5 || page[1].Seq != 9 {
		t.Fatalf("want seqs [5 9], got %v", seqsOf(page))
	}
	if truncated {
		t.Fatal("the whole remainder fit; truncated must be false")
	}
	if next != 9 {
		t.Fatalf("next_since_seq = %d, want 9", next)
	}
}

func seqsOf(evs []event.Event) []uint64 {
	out := make([]uint64, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Seq)
	}
	return out
}

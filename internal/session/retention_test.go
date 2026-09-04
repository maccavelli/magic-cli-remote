package session

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// toolFlood reproduces the shape of the operator's codex session 5e360a4e:
// 695 of its 723 retained events were tool_call_update from a single exec tool,
// each one a line of a `flutter test` run's stdout, and they evicted 2,814
// events including the prompt that asked for the run (MADR 0138 F1).
func toolFlood(t *testing.T, prompts int, floodPerPrompt int, floodBytes int) []event.Event {
	t.Helper()
	out := make([]event.Event, 0, prompts*(floodPerPrompt+1))
	seq := 0
	for p := range prompts {
		seq++
		out = append(out, event.Event{
			Type:      event.TypeUserMessage,
			SessionID: "s1",
			Text:      fmt.Sprintf("prompt %d", p),
			Timestamp: time.Unix(1788000000+int64(seq), 0).UTC(),
		})
		for range floodPerPrompt {
			seq++
			out = append(out, event.Event{
				Type:      event.TypeToolUpdate,
				SessionID: "s1",
				ToolID:    "exec-1",
				Status:    "running",
				Text:      strings.Repeat("o", floodBytes),
				Timestamp: time.Unix(1788000000+int64(seq), 0).UTC(),
			})
		}
	}
	return out
}

func TestAnchorsSurviveATelemetryFlood(t *testing.T) {
	// 20 prompts, each followed by 300 lines of 64 KiB tool output: roughly
	// 390 MiB offered into a 32 MiB budget, so more than 90% must be evicted.
	const prompts = 20
	evs := toolFlood(t, prompts, 300, 64<<10)

	e := &entry{}
	for i := range evs {
		ev := evs[i]
		e.appendHistoryLocked(&ev)
	}

	if e.historyBytes > historyBudgetBytes {
		t.Fatalf("ring is %d bytes, over the %d budget", e.historyBytes, historyBudgetBytes)
	}

	users := 0
	for i := range e.history {
		if e.history[i].Type == event.TypeUserMessage {
			users++
		}
	}
	if users != prompts {
		t.Fatalf("retained %d of %d user messages; anchors must outlive telemetry", users, prompts)
	}
	// The point of the budget is that most of the flood is gone.
	if len(e.history) >= len(evs)/2 {
		t.Fatalf("retained %d of %d events; the flood was not evicted", len(e.history), len(evs))
	}
	t.Logf("offered=%d retained=%d bytes=%d user_messages=%d/%d",
		len(evs), len(e.history), e.historyBytes, users, prompts)
}

func TestAnchorsSurviveEvenWhenTheyAloneExceedTheBudget(t *testing.T) {
	// Every event is an anchor, and together they are over budget. The rule
	// says anchors are evicted only when nothing else remains, so eviction
	// must still make progress rather than loop or give up silently.
	e := &entry{}
	const n = 700
	for i := range n {
		ev := event.Event{
			Type:      event.TypeUserMessage,
			SessionID: "s1",
			Text:      strings.Repeat("a", 64<<10),
			Timestamp: time.Unix(1788000000+int64(i), 0).UTC(),
		}
		e.appendHistoryLocked(&ev)
	}
	if e.historyBytes > historyBudgetBytes {
		t.Fatalf("ring is %d bytes, over the %d budget even after evicting anchors", e.historyBytes, historyBudgetBytes)
	}
	if len(e.history) == 0 {
		t.Fatal("everything was evicted; the newest event must always survive")
	}
	if got := e.history[len(e.history)-1].Text[:1]; got != "a" {
		t.Fatalf("newest event lost: %q", got)
	}
}

func TestEvictionPrefersTheLowestClassPresent(t *testing.T) {
	// Content must not be touched while telemetry is still there to give.
	e := &entry{}
	const big = 64 << 10
	for i := range 600 {
		typ := event.TypeRemoteCommands
		if i%3 == 0 {
			typ = event.TypeAssistantChunk
		}
		ev := event.Event{
			Type:      typ,
			SessionID: "s1",
			Text:      strings.Repeat("c", big),
			Timestamp: time.Unix(1788000000+int64(i), 0).UTC(),
		}
		e.appendHistoryLocked(&ev)
	}

	telemetry, content := 0, 0
	for i := range e.history {
		switch event.ClassOf(e.history[i].Type) {
		case event.ClassTelemetry:
			telemetry++
		case event.ClassContent:
			content++
		}
	}
	if telemetry > 0 && content < 200 {
		t.Fatalf("evicted content (%d of 200 left) while %d telemetry events remain", content, telemetry)
	}
}

func TestGlobalBudgetEvictsColdestSessionFirst(t *testing.T) {
	// A small global budget so the cross-session rule can be exercised without
	// allocating the 384 MiB the production constant implies. The rule under
	// test is the ordering, not the number.
	const budget = 24 << 20

	// Sized so that trimming exactly one session clears the overage. That is
	// what makes this a test of the ordering: if more than one session had to
	// be trimmed, every ordering would trim them all and the assertion below
	// would hold no matter what order they were visited in. An earlier version
	// of this test did exactly that and passed with the comparison reversed.
	const perSession = 10 << 20 // cold and warm
	const activeSize = 6 << 20  // total 26 MiB, over by 2 MiB

	m := &Manager{sessions: map[string]*entry{}, globalBudget: budget}
	now := time.Now()

	fill := func(id string, target int, last time.Time) *entry {
		e := &entry{lastEventAt: last}
		for e.historyBytes < target {
			ev := event.Event{
				Type:      event.TypeToolUpdate,
				SessionID: id,
				ToolID:    "exec",
				Text:      strings.Repeat("g", 256<<10),
				Timestamp: now,
			}
			e.appendHistoryLocked(&ev)
		}
		m.sessions[id] = e
		return e
	}
	cold := fill("cold", perSession, now.Add(-2*time.Hour))
	warm := fill("warm", perSession, now.Add(-1*time.Minute))
	active := fill("active", activeSize, now)

	coldBefore, warmBefore, activeBefore := cold.historyBytes, warm.historyBytes, active.historyBytes
	if coldBefore+warmBefore+activeBefore <= budget {
		t.Fatalf("setup is not over budget: %d bytes against %d", coldBefore+warmBefore+activeBefore, budget)
	}

	trimmed := m.enforceGlobalBudgetLocked("active")

	if cold.historyBytes >= coldBefore {
		t.Fatalf("the coldest session was not trimmed: %d bytes, was %d", cold.historyBytes, coldBefore)
	}
	if warm.historyBytes != warmBefore {
		t.Fatalf("a warmer session was trimmed (%d, was %d) while the coldest still had bytes to give",
			warm.historyBytes, warmBefore)
	}
	if active.historyBytes != activeBefore {
		t.Fatalf("the active session was trimmed while colder sessions had bytes to give")
	}
	if len(trimmed) != 1 || trimmed[0] != "cold" {
		t.Fatalf("global trim reported %v, want exactly [cold] so the right operator is told", trimmed)
	}

	// Reported once per session, not once per event.
	for _, id := range m.enforceGlobalBudgetLocked("active") {
		if id == "cold" {
			t.Fatal("the same session was reported twice; the notice would repeat on every event")
		}
	}
}

func TestDurableHistoryUsesTheClassRule(t *testing.T) {
	evs := toolFlood(t, 5, 200, 64<<10)
	out := boundHistoryByClass(evs, historyFileBudgetBytes)

	total := 0
	users := 0
	for i := range out {
		total += event.Bytes(&out[i])
		if out[i].Type == event.TypeUserMessage {
			users++
		}
	}
	if total > historyFileBudgetBytes {
		t.Fatalf("bounded transcript is %d bytes, over the %d budget", total, historyFileBudgetBytes)
	}
	if users != 5 {
		t.Fatalf("kept %d of 5 user messages on disk; the file must use the same rule as the ring", users)
	}
	if len(out) >= len(evs) {
		t.Fatalf("nothing was dropped: %d of %d", len(out), len(evs))
	}
	if out[len(out)-1].Timestamp != evs[len(evs)-1].Timestamp {
		t.Fatal("the newest event must survive")
	}
}

func TestBoundHistoryByClassLeavesASmallTranscriptAlone(t *testing.T) {
	evs := toolFlood(t, 2, 3, 128)
	out := boundHistoryByClass(evs, historyFileBudgetBytes)
	if len(out) != len(evs) {
		t.Fatalf("a transcript inside the budget must be written whole: %d of %d", len(out), len(evs))
	}
	if boundHistoryByClass(nil, historyFileBudgetBytes) == nil {
		t.Fatal("nil must become an empty slice so the file is never null-JSON")
	}
}

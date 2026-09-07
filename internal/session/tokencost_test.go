package session

import (
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func withUsage(used, size int) *entry {
	return &entry{lastUsage: &event.Usage{Used: used, Size: size}}
}

func TestContextPressureNoticeAtSeventyFive(t *testing.T) {
	e := withUsage(75_000, 100_000)
	got := e.contextPressureLocked()
	if got == "" {
		t.Fatal("75% of the context window must be reported")
	}
	if !strings.Contains(got, "75%") || !strings.Contains(got, "75,000") {
		t.Fatalf("notice does not carry the numbers: %q", got)
	}
	if !strings.Contains(got, "/compact") {
		t.Fatalf("notice offers no remedy: %q", got)
	}
}

func TestContextPressureIsReportedOncePerThreshold(t *testing.T) {
	e := withUsage(76_000, 100_000)
	if e.contextPressureLocked() == "" {
		t.Fatal("the first crossing must be reported")
	}
	// Session 1b3742ba on the operator's host went 99,385 -> 1,526,598 tokens
	// across two turns and the daemon said nothing (MADR 0138 T1). It must say
	// something — but not on every turn afterwards.
	e.lastUsage = &event.Usage{Used: 80_000, Size: 100_000}
	if got := e.contextPressureLocked(); got != "" {
		t.Fatalf("the same threshold was reported twice: %q", got)
	}
	e.lastUsage = &event.Usage{Used: 91_000, Size: 100_000}
	got := e.contextPressureLocked()
	if got == "" {
		t.Fatal("crossing the higher threshold must be reported")
	}
	if !strings.Contains(got, "may not fit") {
		t.Fatalf("the 90%% notice must say what is at stake: %q", got)
	}
	e.lastUsage = &event.Usage{Used: 95_000, Size: 100_000}
	if got := e.contextPressureLocked(); got != "" {
		t.Fatalf("90%% was reported twice: %q", got)
	}
}

func TestContextPressureReArmsAfterCompaction(t *testing.T) {
	e := withUsage(92_000, 100_000)
	if e.contextPressureLocked() == "" {
		t.Fatal("setup: the crossing must be reported")
	}
	// A /compact drops usage back down.
	e.lastUsage = &event.Usage{Used: 20_000, Size: 100_000}
	if got := e.contextPressureLocked(); got != "" {
		t.Fatalf("dropping below a threshold is not itself news: %q", got)
	}
	// Climbing again is.
	e.lastUsage = &event.Usage{Used: 78_000, Size: 100_000}
	if e.contextPressureLocked() == "" {
		t.Fatal("after a compaction the next climb must be reported again")
	}
}

func TestNoPressureNoticeWithoutAContextWindow(t *testing.T) {
	// A percentage of an unknown denominator is exactly the confidently wrong
	// number this record's predecessor was corrected for. Providers that report
	// no window get no notice rather than a guess.
	for _, u := range []*event.Usage{
		nil,
		{Used: 900_000, Size: 0},
		{Used: 0, Size: 100_000},
		{Used: 900_000, Size: -1},
	} {
		e := &entry{lastUsage: u}
		if got := e.contextPressureLocked(); got != "" {
			t.Fatalf("usage %+v produced a notice: %q", u, got)
		}
	}
}

func TestNoPressureNoticeBelowTheFirstThreshold(t *testing.T) {
	e := withUsage(74_999, 100_000)
	if got := e.contextPressureLocked(); got != "" {
		t.Fatalf("74%% must be quiet: %q", got)
	}
}

func TestTurnCostAccrues(t *testing.T) {
	e := &entry{meta: Meta{ID: "s1"}}

	e.lastUsage = &event.Usage{Input: 13_317, Output: 9, CacheRead: 2_227}
	m := e.accrueTurnCostLocked()
	if m == nil {
		t.Fatal("a turn with usage must produce metadata to persist")
	}
	// The operator's kilo session 10fe2896 reported byte-identical accounting
	// on two consecutive turns: 13,317 input against 2,227 cached, twice. The
	// totals are what make that visible (MADR 0138 T2).
	e.lastUsage = &event.Usage{Input: 13_317, Output: 9, CacheRead: 2_227}
	e.accrueTurnCostLocked()

	if e.meta.Turns != 2 {
		t.Fatalf("turns = %d, want 2", e.meta.Turns)
	}
	if e.meta.InputTokens != 26_634 {
		t.Fatalf("input = %d, want 26634", e.meta.InputTokens)
	}
	if e.meta.CachedTokens != 4_454 {
		t.Fatalf("cached = %d, want 4454", e.meta.CachedTokens)
	}
	if e.meta.OutputTokens != 18 {
		t.Fatalf("output = %d, want 18", e.meta.OutputTokens)
	}
}

func TestTurnCostIgnoresAnEmptyReport(t *testing.T) {
	e := &entry{meta: Meta{ID: "s1"}}
	if m := e.accrueTurnCostLocked(); m != nil {
		t.Fatal("no usage at all must not count as a turn")
	}
	e.lastUsage = &event.Usage{Used: 1200, Size: 128_000}
	if m := e.accrueTurnCostLocked(); m != nil {
		t.Fatal("a context-only report carries no per-turn tokens and must not count as a turn")
	}
	if e.meta.Turns != 0 {
		t.Fatalf("turns = %d, want 0", e.meta.Turns)
	}
}

func TestHumanCount(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{0, "0"}, {7, "7"}, {999, "999"}, {1000, "1,000"},
		{27_474, "27,474"}, {1_526_598, "1,526,598"}, {-5, "0"},
	} {
		if got := humanCount(tc.in); got != tc.want {
			t.Errorf("humanCount(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

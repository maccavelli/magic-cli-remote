package session

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// The shape of a real transcript, measured over all 7,004 events in the
// operator's 34 stored sessions (MADR 0138 F16). Two types carry 73% of the
// bytes; the actual conversation carries 11.7%. A retention or paging test run
// against a uniform synthetic ring measures neither of those facts, which is
// why the generator below reproduces the distribution rather than inventing
// one.
//
// Real transcripts are deliberately not committed: this repository is public
// and its fixtures would publish the operator's conversations permanently
// (0138-PLAN, deviation 2026-09-03). TestHistoryPageAgainstRealHistory reads
// them from a local data dir instead, on demand.
type fixtureKind struct {
	typ   event.Type
	share float64 // fraction of events
	bytes int     // mean encoded size
}

var fixtureMix = []fixtureKind{
	{event.TypeToolUpdate, 0.270, 861},
	{event.TypeAssistantChunk, 0.245, 216},
	{event.TypeToolCall, 0.159, 383},
	{event.TypeThoughtChunk, 0.138, 261},
	{event.TypeRemoteCommands, 0.069, 5120}, // stands in for available_commands
	{event.TypeSessionStatus, 0.030, 180},
	{event.TypeNotice, 0.015, 254},
	{event.TypeTurnComplete, 0.013, 208},
	{event.TypeUserMessage, 0.013, 202},
	{event.TypeUsage, 0.014, 179},
	{event.TypePlan, 0.034, 716},
}

// syntheticHistory builds n events with the measured type mix and per-type
// sizes. Deterministic: the same n always yields the same ring, so a failure is
// reproducible and a byte assertion can be exact.
func syntheticHistory(n int) []event.Event {
	rng := rand.New(rand.NewSource(0x0138))
	out := make([]event.Event, 0, n)
	cum := make([]float64, len(fixtureMix))
	total := 0.0
	for i, k := range fixtureMix {
		total += k.share
		cum[i] = total
	}
	for i := range n {
		r := rng.Float64() * total
		pick := fixtureMix[len(fixtureMix)-1]
		for j, c := range cum {
			if r <= c {
				pick = fixtureMix[j]
				break
			}
		}
		// Subtract the fixed JSON scaffolding so the encoded event lands near
		// the measured mean rather than that mean plus overhead.
		const scaffold = 120
		fill := pick.bytes - scaffold
		if fill < 8 {
			fill = 8
		}
		ev := event.Event{
			Type:      pick.typ,
			SessionID: "s1",
			Seq:       uint64(i + 1),
			Timestamp: time.Unix(1788000000+int64(i), 0).UTC(),
			Text:      fmt.Sprintf("%06d|%s", i, strings.Repeat("m", fill)),
		}
		if pick.typ == event.TypeToolUpdate || pick.typ == event.TypeToolCall {
			ev.ToolID = fmt.Sprintf("tool-%d", i%7)
			ev.Status = "running"
		}
		out = append(out, ev)
	}
	return out
}

func TestSyntheticHistoryMatchesTheMeasuredShape(t *testing.T) {
	// Pins the generator itself. A fixture that silently drifts away from the
	// distribution it claims to model is a test that stops testing what its
	// name says, and nothing else in the suite would notice.
	const n = 7004
	hist := syntheticHistory(n)
	if len(hist) != n {
		t.Fatalf("got %d events, want %d", len(hist), n)
	}

	counts := map[event.Type]int{}
	bytes := map[event.Type]int{}
	totalBytes := 0
	for i := range hist {
		b, err := json.Marshal(&hist[i])
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		counts[hist[i].Type]++
		bytes[hist[i].Type] += len(b)
		totalBytes += len(b)
	}

	for _, k := range fixtureMix {
		got := float64(counts[k.typ]) / float64(n)
		if diff := got - k.share; diff > 0.02 || diff < -0.02 {
			t.Errorf("%s is %.1f%% of events, want %.1f%% (±2pp)", k.typ, 100*got, 100*k.share)
		}
		if counts[k.typ] == 0 {
			continue
		}
		mean := bytes[k.typ] / counts[k.typ]
		if lo, hi := k.bytes*3/4, k.bytes*5/4; mean < lo || mean > hi {
			t.Errorf("%s mean is %d B, want %d B (±25%%)", k.typ, mean, k.bytes)
		}
	}

	// The measured overall mean across the operator's real sessions is 806 B.
	if mean := totalBytes / n; mean < 600 || mean > 1100 {
		t.Errorf("overall mean is %d B/event, want near the measured 806", mean)
	}
}

func TestHistoryPageOnARealisticRingEncodesOncePerEvent(t *testing.T) {
	hist := syntheticHistory(800)
	m := &Manager{sessions: map[string]*entry{"s1": {history: hist}}}

	calls := 0
	orig := historyMarshal
	historyMarshal = func(ev *event.Event) ([]byte, error) { calls++; return json.Marshal(ev) }
	t.Cleanup(func() { historyMarshal = orig })

	page, _, _ := m.HistoryPage("s1", 0, historyMaxPage)
	if len(page) == 0 {
		t.Fatal("empty page")
	}
	if calls > len(hist) {
		t.Fatalf("encoded %d times over a %d-event window; want at most one pass", calls, len(hist))
	}
	b, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal page: %v", err)
	}
	if len(b) > historyMaxResponseBytes {
		t.Fatalf("page is %d bytes, over the %d budget", len(b), historyMaxResponseBytes)
	}
}

// TestHistoryPageAgainstRealHistory reproduces the acceptance measurement
// recorded in 0138-PLAN Phase 1 against real transcripts. It reads them from
// MCREMOTE_HISTORY_FIXTURE_DIR — normally ~/.local/share/mcremote/sessions —
// and skips when that is unset, so real conversations stay out of this
// repository while the measurement stays reproducible on the operator's host:
//
//	MCREMOTE_HISTORY_FIXTURE_DIR=~/.local/share/mcremote/sessions \
//	  go test ./internal/session/ -run RealHistory -v
func TestHistoryPageAgainstRealHistory(t *testing.T) {
	dir := strings.TrimSpace(os.Getenv("MCREMOTE_HISTORY_FIXTURE_DIR"))
	if dir == "" {
		t.Skip("set MCREMOTE_HISTORY_FIXTURE_DIR to a mcremote sessions directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read fixture dir: %v", err)
	}

	seen := 0
	for _, de := range entries {
		if !de.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, de.Name(), "history.json"))
		if err != nil {
			continue
		}
		var hf struct {
			Events []event.Event `json:"events"`
		}
		if json.Unmarshal(b, &hf) != nil || len(hf.Events) == 0 {
			continue
		}
		seen++

		m := &Manager{sessions: map[string]*entry{"s": {history: hf.Events}}}
		calls := 0
		orig := historyMarshal
		historyMarshal = func(ev *event.Event) ([]byte, error) { calls++; return json.Marshal(ev) }

		var m0, m1 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m0)
		start := time.Now()
		page, truncated, _ := m.HistoryPage("s", 0, historyMaxPage)
		elapsed := time.Since(start)
		runtime.ReadMemStats(&m1)
		historyMarshal = orig

		allocMB := float64(m1.TotalAlloc-m0.TotalAlloc) / 1e6
		t.Logf("%s ring=%d returned=%d truncated=%v encodes=%d alloc=%.1fMB elapsed=%.1fms",
			de.Name()[:8], len(hf.Events), len(page), truncated, calls,
			allocMB, float64(elapsed.Microseconds())/1000)

		if calls > len(hf.Events) {
			t.Errorf("%s: encoded %d times over a %d-event ring; want at most one pass",
				de.Name()[:8], calls, len(hf.Events))
		}
		if allocMB > 10 {
			t.Errorf("%s: allocated %.1f MB, want under 10 MB", de.Name()[:8], allocMB)
		}
	}
	if seen == 0 {
		t.Skipf("no history.json under %s", dir)
	}
}

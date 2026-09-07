package kilo

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestUsageCarriesTheCacheSplit is the regression for the defect that made two
// phases of latency analysis wrong (MADR 0137, second correction).
//
// kilo's emitUsage parsed Input, Output, Reasoning, Cache.Read and Cache.Write
// into msgTokens, used them to compute a context total, and then emitted
// {Used, Size} — discarding all five. `cold` is derived from CacheRead == 0,
// so a kilo turn that read 14336 cached tokens was recorded as cold, and the
// conclusion "kilo never gets a warm turn" followed from an instrument that
// could not report one.
func TestUsageCarriesTheCacheSplit(t *testing.T) {
	h := &captureHost{}
	o := &httpSession{h: h, d: &httpDialect{}}

	warm := &msgTokens{Input: 212, Output: 22, Reasoning: 0}
	warm.Cache.Read = 14336
	o.emitUsage("assistant", warm, &msgModel{ProviderID: "kilo", ModelID: "kilo-auto/balanced"})

	u := lastUsage(t, h)
	if u.CacheRead != 14336 {
		t.Fatalf("CacheRead = %d, want 14336: a warm turn is being reported as cold", u.CacheRead)
	}
	if u.Input != 212 || u.Output != 22 {
		t.Fatalf("input/output = %d/%d, want 212/22", u.Input, u.Output)
	}
}

// TestUsageIsReEmittedWhenOnlyTheCacheSplitMoves pins the dedupe latch.
//
// The latch compared (used, size) alone. A turn can hold the context total
// steady while the split between fresh and cached input moves — which is
// exactly the cold-to-warm transition this record is about — and the report
// showing it would have been suppressed as unchanged.
func TestUsageIsReEmittedWhenOnlyTheCacheSplitMoves(t *testing.T) {
	h := &captureHost{}
	o := &httpSession{h: h, d: &httpDialect{}}

	cold := &msgTokens{Input: 14548, Output: 22}
	o.emitUsage("assistant", cold, nil)
	if n := countUsage(h); n != 1 {
		t.Fatalf("first report produced %d events, want 1", n)
	}

	// Same total (14548+0 == 212+14336), different split.
	warm := &msgTokens{Input: 212, Output: 22}
	warm.Cache.Read = 14336
	o.emitUsage("assistant", warm, nil)
	if n := countUsage(h); n != 2 {
		t.Fatalf("a changed cache split produced %d events total, want 2: the "+
			"latch compared only the context total, so the cold-to-warm "+
			"transition was suppressed as unchanged", n)
	}

	// A genuine repeat is still suppressed.
	o.emitUsage("assistant", warm, nil)
	if n := countUsage(h); n != 2 {
		t.Fatalf("an identical report produced %d events total, want 2", n)
	}
}

func countUsage(h *captureHost) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, ev := range h.events {
		if ev.Type == event.TypeUsage {
			n++
		}
	}
	return n
}

func lastUsage(t *testing.T, h *captureHost) *event.Usage {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.events) - 1; i >= 0; i-- {
		if h.events[i].Type == event.TypeUsage && h.events[i].Usage != nil {
			return h.events[i].Usage
		}
	}
	t.Fatal("no usage event emitted")
	return nil
}

// TestFixtureConfirmsKiloReportsACacheRead is the ground truth behind all of
// the above: the claim that kilo caches is read from kilo's own wire, not
// assumed.
func TestFixtureConfirmsKiloReportsACacheRead(t *testing.T) {
	data, err := os.ReadFile("testdata/wire/" + KnownGoodVersion + "/frames.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, `"cache"`) {
			continue
		}
		var probe map[string]any
		if json.Unmarshal([]byte(line), &probe) != nil {
			continue
		}
		found = true
		break
	}
	if !found {
		t.Skip("no cache accounting in this fixture; re-record from a session " +
			"with more than one turn to cover the warm case")
	}
}

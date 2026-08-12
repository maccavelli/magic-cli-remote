package picker

import "testing"

// The two payloads below are copied from live probes, not invented: grok
// 0.2.114 reports its rungs expensive-first, codex 0.145.0 cheapest-first. The
// whole point of NormalizeThinkingLevels is that clients never have to know
// which provider they are looking at (MADR 0052 D2).
func grokLevels() []ThinkingLevel {
	return []ThinkingLevel{
		{ID: "high", Label: "High Effort", Description: "Highest implementation quality with extensive reasoning", Default: true},
		{ID: "medium", Label: "Medium Effort", Description: "Balanced effort with standard implementation and testing"},
		{ID: "low", Label: "Low Effort", Description: "Quick, fast implementations"},
	}
}

func codexLevels() []ThinkingLevel {
	return []ThinkingLevel{
		{ID: "low", Description: "Fast responses with lighter reasoning"},
		{ID: "medium", Description: "Balances speed and reasoning depth for everyday tasks", Default: true},
		{ID: "high", Description: "Greater reasoning depth for complex problems"},
		{ID: "xhigh", Description: "Extra high reasoning depth for complex problems"},
		{ID: "max", Description: "Maximum reasoning depth for the hardest problems"},
		{ID: "ultra", Description: "Maximum reasoning with automatic task delegation"},
	}
}

func ids(levels []ThinkingLevel) []string {
	out := make([]string, 0, len(levels))
	for _, l := range levels {
		out = append(out, l.ID)
	}
	return out
}

func eq(a, b []string) bool {
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

func TestNormalizeReversesGrokOrder(t *testing.T) {
	got := ids(NormalizeThinkingLevels(grokLevels()))
	want := []string{"low", "medium", "high"}
	if !eq(got, want) {
		t.Fatalf("got %v, want %v — grok reports expensive-first", got, want)
	}
}

func TestNormalizeLeavesCodexOrderAlone(t *testing.T) {
	got := ids(NormalizeThinkingLevels(codexLevels()))
	want := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	if !eq(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Codex types the effort value as an open string, so a name the daemon has
// never seen is expected. Dropping it would silently hide a rung a provider
// added — the failure that matters when the vocabulary is not closed.
func TestNormalizeKeepsUnknownRungs(t *testing.T) {
	in := []ThinkingLevel{
		{ID: "high"},
		{ID: "hyper"},
		{ID: "low"},
		{ID: "cosmic"},
	}
	got := ids(NormalizeThinkingLevels(in))
	want := []string{"low", "high", "hyper", "cosmic"}
	if !eq(got, want) {
		t.Fatalf("got %v, want %v — unknown rungs sort last, in input order", got, want)
	}
}

// T-E3: grok-4.6 marks both xhigh and high default; cheapest-first keeps high
// (MADR 0081 P1.4 / 0052 D3).
func TestNormalizeGrok46DualDefaultKeepsHigh(t *testing.T) {
	in := []ThinkingLevel{
		{ID: "xhigh", Default: true},
		{ID: "high", Default: true},
		{ID: "medium"},
		{ID: "low"},
	}
	got := NormalizeThinkingLevels(in)
	if DefaultThinkingLevel(got) != "high" {
		t.Fatalf("default=%q, want high (cheapest-first first-default)", DefaultThinkingLevel(got))
	}
	var defaults int
	for _, l := range got {
		if l.Default {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("defaults=%d, want 1", defaults)
	}
}

func TestNormalizeKeepsOneDefault(t *testing.T) {
	in := []ThinkingLevel{
		{ID: "low", Default: true},
		{ID: "medium", Default: true},
		{ID: "high", Default: true},
	}
	got := NormalizeThinkingLevels(in)
	var n int
	for _, l := range got {
		if l.Default {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d defaults survived, want exactly 1", n)
	}
	if got[0].ID != "low" || !got[0].Default {
		t.Fatalf("kept %+v, want the first in cheapest-first order", got[0])
	}
}

func TestNormalizeDoesNotMutateInput(t *testing.T) {
	in := grokLevels()
	before := ids(in)
	_ = NormalizeThinkingLevels(in)
	if !eq(ids(in), before) {
		t.Fatalf("input reordered: %v", ids(in))
	}
}

func TestNormalizeEmptyIsNil(t *testing.T) {
	if got := NormalizeThinkingLevels(nil); got != nil {
		t.Fatalf("got %v, want nil so omitempty drops the key", got)
	}
	if got := NormalizeThinkingLevels([]ThinkingLevel{}); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestDefaultThinkingLevel(t *testing.T) {
	if got := DefaultThinkingLevel(NormalizeThinkingLevels(grokLevels())); got != "high" {
		t.Errorf("grok default = %q, want high", got)
	}
	if got := DefaultThinkingLevel(NormalizeThinkingLevels(codexLevels())); got != "medium" {
		t.Errorf("codex default = %q, want medium", got)
	}
	if got := DefaultThinkingLevel([]ThinkingLevel{{ID: "low"}}); got != "" {
		t.Errorf("no marked default should yield %q, got %q", "", got)
	}
}

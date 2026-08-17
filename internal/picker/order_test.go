package picker_test

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
)

func ids(opts []picker.Option) []string {
	out := make([]string, len(opts))
	for i, o := range opts {
		out[i] = o.ID
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func dated(id, date string) picker.Option {
	return picker.Option{ID: id, Meta: map[string]string{picker.MetaReleaseDate: date}}
}

// TestOrderModelsNewestFirst pins the ordering contract MADR 0043 D3 promises:
// current first, then newest by release date, deprecated last regardless.
func TestOrderModelsNewestFirst(t *testing.T) {
	in := []picker.Option{
		dated("old", "2025-01-01"),
		dated("new", "2026-07-16"),
		{ID: "gone", Meta: map[string]string{
			picker.MetaReleaseDate: "2026-12-01", picker.MetaStatus: picker.StatusDeprecated,
		}},
		dated("mid", "2026-01-01"),
	}
	eq(t, ids(picker.OrderModels(in, "mid")), []string{"mid", "new", "old", "gone"})
	// Without a current id the newest simply leads.
	eq(t, ids(picker.OrderModels(in, "")), []string{"new", "mid", "old", "gone"})
}

// TestOrderModelsKeepsEngineOrderWithoutDates is the honesty guard: goose and
// grok report no dates, and inventing an order for them (alphabetical, or a
// version-name heuristic) would be a claim the data does not support. The
// engine's own order must survive untouched.
func TestOrderModelsKeepsEngineOrderWithoutDates(t *testing.T) {
	in := []picker.Option{{ID: "zeta"}, {ID: "alpha"}, {ID: "mike"}}
	eq(t, ids(picker.OrderModels(in, "")), []string{"zeta", "alpha", "mike"})
}

// TestOrderModelsDatedBeatsUndated covers the mixed case: a provider that dates
// some models and not others must not have its undated ones float to the top,
// and their relative order must still be the engine's.
func TestOrderModelsDatedBeatsUndated(t *testing.T) {
	in := []picker.Option{
		{ID: "undated-first"},
		dated("dated-old", "2024-01-01"),
		{ID: "undated-second"},
		dated("dated-new", "2026-01-01"),
	}
	eq(t, ids(picker.OrderModels(in, "")),
		[]string{"dated-new", "dated-old", "undated-first", "undated-second"})
}

// TestOrderModelsMonthPrecision covers the 172 opencode models dated YYYY-MM:
// lexical comparison must still be chronological across the two forms.
func TestOrderModelsMonthPrecision(t *testing.T) {
	in := []picker.Option{
		dated("jan-day", "2026-01-30"),
		dated("feb-month", "2026-02"),
		dated("dec-prev", "2025-12-31"),
	}
	eq(t, ids(picker.OrderModels(in, "")), []string{"feb-month", "jan-day", "dec-prev"})
}

// TestOrderModelsDoesNotMutateInput guards the shared-slice hazard: providers
// hand OrderModels their package-level static catalogs.
func TestOrderModelsDoesNotMutateInput(t *testing.T) {
	in := []picker.Option{dated("a", "2024-01-01"), dated("b", "2026-01-01")}
	_ = picker.OrderModels(in, "")
	eq(t, ids(in), []string{"a", "b"})
}

// TestOrderModelsRecommendedIndexBeatsDate pins MADR 0096 D5: an engine that
// says "pick this one" outranks recency, and a model with no recommendation is
// not treated as recommendation zero.
func TestOrderModelsRecommendedIndexBeatsDate(t *testing.T) {
	rec := func(id, idx, date string) picker.Option {
		m := map[string]string{picker.MetaReleaseDate: date}
		if idx != "" {
			m[picker.MetaRecommendedIndex] = idx
		}
		return picker.Option{ID: id, Meta: m}
	}
	got := picker.OrderModels([]picker.Option{
		rec("newest-but-unrecommended", "", "2026-08-16"),
		rec("free", "3", "2026-01-01"),
		rec("frontier", "0", "2026-01-01"),
		rec("older-unrecommended", "", "2025-01-01"),
		rec("balanced", "1", "2026-01-01"),
	}, "")
	want := []string{"frontier", "balanced", "free", "newest-but-unrecommended", "older-unrecommended"}
	for i, w := range want {
		if got[i].ID != w {
			t.Fatalf("position %d = %s, want %s (full: %v)", i, got[i].ID, w, ids(got))
		}
	}
}

// A non-integer index must degrade to date ordering rather than silently
// ranking first: the engine is free to change the field's type.
func TestOrderModelsIgnoresUnparsableRecommendedIndex(t *testing.T) {
	got := picker.OrderModels([]picker.Option{
		{ID: "junk", Meta: map[string]string{picker.MetaRecommendedIndex: "top", picker.MetaReleaseDate: "2020-01-01"}},
		{ID: "recent", Meta: map[string]string{picker.MetaReleaseDate: "2026-01-01"}},
	}, "")
	if got[0].ID != "recent" {
		t.Fatalf("order = %v, want the dated model first", ids(got))
	}
}

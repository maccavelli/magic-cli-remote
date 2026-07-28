package picker

import (
	"sort"
	"strings"
)

// Well-known [Option.Meta] keys for model catalogs. Providers populate what
// their engine actually reports; [OrderModels] is the only place that
// interprets them, so "newest first" is defined once rather than per provider.
const (
	// MetaReleaseDate is the model's release date as "YYYY-MM-DD" or
	// "YYYY-MM". Lexical comparison is chronological for both forms, and a
	// shorter (month-only) date sorts before any day in that month — which is
	// the correct "we only know the month" answer.
	MetaReleaseDate = "release_date"
	// MetaStatus is the engine's lifecycle status; the value StatusDeprecated
	// sinks the option to the bottom of the list.
	MetaStatus = "status"
	// MetaContext is a human-readable context window ("200K").
	MetaContext = "context"
	// MetaConnected is "true"/"false" on a model-provider option: whether the
	// engine reports credentials configured for it.
	MetaConnected = "connected"
	// MetaModelCount is the number of models a model-provider option covers.
	MetaModelCount = "model_count"
	// MetaDefaultModel is the engine's default model id for a model-provider
	// option, when it reports one.
	MetaDefaultModel = "default_model"
)

// StatusDeprecated is the [MetaStatus] value that ranks an option last.
const StatusDeprecated = "deprecated"

// OrderModels returns opts ordered for a model picker:
//
//  1. currentID first, when present;
//  2. then by MetaReleaseDate descending, newest first;
//  3. then in source order — the engine's own order, which is the only honest
//     answer for a provider that reports no dates (goose, grok, codex);
//  4. MetaStatus == StatusDeprecated last regardless of date.
//
// The sort is stable, so options without a date keep their relative engine
// order rather than being shuffled into an arbitrary one. The input is not
// modified.
func OrderModels(opts []Option, currentID string) []Option {
	out := make([]Option, len(opts))
	copy(out, opts)
	// rank groups the list into bands; ordering *within* a band is decided
	// below. Bands, not one comparison chain, because "current first" and
	// "deprecated last" must beat the date comparison rather than participate
	// in it.
	rank := func(o Option) int {
		switch {
		case currentID != "" && o.ID == currentID:
			return 0
		case strings.EqualFold(o.Meta[MetaStatus], StatusDeprecated):
			return 2
		default:
			return 1
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		if ri != rj {
			return ri < rj
		}
		di, dj := out[i].Meta[MetaReleaseDate], out[j].Meta[MetaReleaseDate]
		if di == dj {
			return false // stable: keep source order
		}
		// A missing date must not win the comparison: it sorts after every
		// dated option, but ties with other undated options so their source
		// order survives.
		if di == "" {
			return false
		}
		if dj == "" {
			return true
		}
		return di > dj
	})
	return out
}

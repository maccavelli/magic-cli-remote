package picker

import (
	"sort"
	"strconv"
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
	// MetaRecommendedIndex is the engine's own recommendation rank for a
	// model, lowest first ("0" is the top recommendation). Kilo reports it on
	// its auto-routers (frontier 0, balanced 1, efficient 2, free 3); a model
	// the engine does not recommend carries no key at all, which is why the
	// value is a rank and not a boolean.
	MetaRecommendedIndex = "recommended_index"
)

// StatusDeprecated is the [MetaStatus] value that ranks an option last.
const StatusDeprecated = "deprecated"

// OrderModels returns opts ordered for a model picker:
//
//  1. currentID first, when present;
//  2. then by MetaRecommendedIndex ascending — the engine's own "pick this
//     one" ranking, which beats recency because a router the engine
//     recommends is the answer regardless of when it shipped;
//  3. then by MetaReleaseDate descending, newest first;
//  4. then in source order — the engine's own order, which is the only honest
//     answer for a provider that reports no dates (goose, grok, codex);
//  5. MetaStatus == StatusDeprecated last regardless of date.
//
// The sort is stable, so options without a date keep their relative engine
// order rather than being shuffled into an arbitrary one. That makes the
// caller's input order part of the result: a caller whose source is a Go map
// must sort before calling, or the answer changes between two calls with the
// same data (MADR 0096 D4). The input is not modified.
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
		// The engine's own recommendation, when it reports one. An option
		// without an index is not "index 0" and not last overall — it simply
		// loses to every recommended option and then falls through to the
		// date comparison against its unrecommended peers.
		xi, oki := recommendedIndex(out[i])
		xj, okj := recommendedIndex(out[j])
		if oki != okj {
			return oki
		}
		if oki && xi != xj {
			return xi < xj
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

// recommendedIndex reads MetaRecommendedIndex. A missing or unparsable value
// reports ok=false: an engine that starts sending something other than an
// integer must degrade to date ordering, not to rank zero.
func recommendedIndex(o Option) (int, bool) {
	v, ok := o.Meta[MetaRecommendedIndex]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, false
	}
	return n, true
}

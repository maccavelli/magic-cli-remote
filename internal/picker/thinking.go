package picker

import "sort"

// thinkingRank orders the rung names the provider vocabularies share, cheapest
// first. Providers disagree on the direction they report — codex returns
// cheapest-first, grok returns high, medium, low — and clients render a ladder,
// so the daemon fixes the direction once here rather than in each dialect
// (MADR 0052 D2).
//
// This is an ordering aid, not an allowlist: codex types the effort value as an
// open string, so a name absent from this table is expected rather than
// exceptional and is kept (see [NormalizeThinkingLevels]).
var thinkingRank = map[string]int{
	"none":    0,
	"minimal": 1,
	"low":     2,
	"medium":  3,
	"high":    4,
	"xhigh":   5,
	"max":     6,
	"ultra":   7,
}

// rankUnknown sorts any name missing from thinkingRank after every known one.
const rankUnknown = 1 << 20

// NormalizeThinkingLevels returns levels ordered cheapest-first, with at most
// one Default.
//
// Unknown rung names are kept, ranked after every known one and holding their
// relative input order — dropping them would silently hide a rung a provider
// added, which is the failure mode that matters when the vocabulary is open.
// A provider marking several defaults keeps the first; marking none is left
// alone, and callers fall back to the model's own behaviour.
//
// The input is not modified.
func NormalizeThinkingLevels(in []ThinkingLevel) []ThinkingLevel {
	if len(in) == 0 {
		return nil
	}
	out := make([]ThinkingLevel, len(in))
	copy(out, in)

	// Stable: two unknown names (or two known ones that somehow tie) keep the
	// order the provider reported them in.
	sort.SliceStable(out, func(i, j int) bool {
		return rankOf(out[i].ID) < rankOf(out[j].ID)
	})

	seenDefault := false
	for i := range out {
		if !out[i].Default {
			continue
		}
		if seenDefault {
			out[i].Default = false
			continue
		}
		seenDefault = true
	}
	return out
}

func rankOf(id string) int {
	if r, ok := thinkingRank[id]; ok {
		return r
	}
	return rankUnknown
}

// DefaultThinkingLevel returns the id of the rung the provider marked as its
// own default, or "" when it marked none.
func DefaultThinkingLevel(levels []ThinkingLevel) string {
	for _, l := range levels {
		if l.Default {
			return l.ID
		}
	}
	return ""
}

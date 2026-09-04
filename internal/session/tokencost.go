package session

import "fmt"

// Context-pressure thresholds, as a percentage of the model's context window.
//
// Two of them, not a continuum: a bar that creeps up is ignored, and a warning
// on every turn is noise. 75% is "plan a /compact", 90% is "the next long turn
// may not fit".
var contextPressureThresholds = []int{75, 90}

// contextPressureLocked returns the notice to emit for this turn's context
// usage, or "" when there is nothing to say.
//
// It fires on a *crossing*, once per threshold, and re-arms when usage drops
// back below — a /compact or /clear should make the next climb worth reporting
// again.
//
// Nothing is reported without a known window. A session on a provider that does
// not report `size` would otherwise get a percentage of an unknown denominator,
// which is the kind of confidently wrong number this record's predecessor was
// corrected for twice (MADR 0137).
//
// Caller holds m.mu.
func (e *entry) contextPressureLocked() string {
	u := e.lastUsage
	if u == nil || u.Size <= 0 || u.Used <= 0 {
		return ""
	}
	pct := u.Used * 100 / u.Size

	// Re-arm on the way down, so a compacted session reports again later.
	if pct < e.ctxPressureNoticed {
		e.ctxPressureNoticed = 0
		for _, t := range contextPressureThresholds {
			if pct >= t {
				e.ctxPressureNoticed = t
			}
		}
		return ""
	}

	crossed := 0
	for _, t := range contextPressureThresholds {
		if pct >= t && t > e.ctxPressureNoticed {
			crossed = t
		}
	}
	if crossed == 0 {
		return ""
	}
	e.ctxPressureNoticed = crossed

	advice := "`/compact` summarises the conversation to reclaim room; `/new` starts fresh."
	if crossed >= 90 {
		advice = "The next long turn may not fit. " + advice
	}
	return fmt.Sprintf(
		"This session is using %s of %s context tokens (%d%%). %s",
		humanCount(int64(u.Used)), humanCount(int64(u.Size)), pct, advice)
}

// accrueTurnCostLocked folds this turn's reported usage into the session's
// running totals and returns the metadata to persist, or nil when there is
// nothing new to record.
//
// Cumulative, unlike event.Usage, which is explicitly per-turn ("labelling a
// per-turn figure as a session total is the specific error that split exists to
// avoid"). Both are wanted: the turn figure says what the last question cost,
// the total says what the session has cost so far. Across the operator's own 15
// recorded turns that was 2,157,605 input tokens, 21% of them uncached, and
// nothing in the daemon added it up (MADR 0138 T1-T3).
//
// Caller holds m.mu.
func (e *entry) accrueTurnCostLocked() *Meta {
	u := e.lastUsage
	if u == nil {
		return nil
	}
	if u.Input <= 0 && u.Output <= 0 && u.CacheRead <= 0 {
		return nil
	}
	e.meta.Turns++
	e.meta.InputTokens += u.Input
	e.meta.OutputTokens += u.Output
	e.meta.CachedTokens += u.CacheRead
	meta := e.meta
	return &meta
}

// humanCount renders a token count with thousands separators. A seven-digit
// context figure is unreadable without them, and 1,526,598 is a real number
// from a real session on the operator's host.
func humanCount(n int64) string {
	if n < 0 {
		return "0"
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

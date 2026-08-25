package opencode

import (
	"math"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// msgTokens is the token block OpenCode puts on an assistant message.
type msgTokens struct {
	Input     int `json:"input"`
	Output    int `json:"output"`
	Reasoning int `json:"reasoning"`
	Cache     struct {
		Read  int `json:"read"`
		Write int `json:"write"`
	} `json:"cache"`
}

// detail projects the raw counts onto the wire's latest-turn accounting.
//
// A negative count is dropped rather than clamped to zero: clamping would
// present a fabricated number as fact, while omitting one is an honest "the
// agent did not report this" (MADR 0112 A4).
func (t msgTokens) detail() (in, out, reasoning, cacheRead, cacheWrite int64) {
	nonNeg := func(v int) int64 {
		if v < 0 {
			return 0
		}
		return int64(v)
	}
	return nonNeg(t.Input), nonNeg(t.Output), nonNeg(t.Reasoning),
		nonNeg(t.Cache.Read), nonNeg(t.Cache.Write)
}

// validCost returns the reportable cost for a raw upstream value.
//
// nil in means the agent reported none. A non-finite or negative figure is
// treated the same way, because "unknown" is the truthful rendering of a value
// that cannot be a price. A finite zero survives: a known-free turn and a turn
// with no accounting must stay distinguishable.
func validCost(in *float64) *float64 {
	if in == nil {
		return nil
	}
	v := *in
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return nil
	}
	return &v
}

// inContext is what the turn left occupying the context window: everything the
// model read (fresh plus cache-read input) and everything it wrote. Cache
// *writes* are excluded — they are the same content counted a second time as it
// is stored, and adding them double-counts the prompt.
func (t msgTokens) inContext() int {
	return t.Input + t.Cache.Read + t.Output + t.Reasoning
}

// msgModel is the model reference on an OpenCode message. The field name varies
// by endpoint ("modelID" on messages, "id" on model refs), so both are read.
type msgModel struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
	ID         string `json:"id"`
}

func (m msgModel) key() string {
	id := m.ModelID
	if id == "" {
		id = m.ID
	}
	if m.ProviderID == "" || id == "" {
		return ""
	}
	return m.ProviderID + "/" + id
}

// emitUsage reports context usage from an assistant message's token counts.
// OpenCode has no usage stream of its own, so this is where the daemon's
// usage_update (and therefore /context) comes from on this provider. User
// messages carry no tokens and are ignored.
//
// The engine re-sends message.updated many times across a turn, mostly with
// identical token counts. Each repeat used to cost a WebSocket frame and — as
// usage_update is not batchable on the phone — an immediate transcript commit,
// which defeated the client's own coalescing window (MADR 0024 §1.1). Only a
// changed report is emitted now.
func (o *httpSession) emitUsage(role string, tokens *msgTokens, model *msgModel, cost *float64) {
	if role != "assistant" || tokens == nil {
		return
	}
	used := tokens.inContext()
	if used <= 0 {
		return
	}
	size := 0
	if model != nil {
		size = o.d.contextLimit(model.key())
	}
	if size == 0 {
		size = o.d.contextLimit(o.h.Model())
	}
	in, out, reasoning, cacheRead, cacheWrite := tokens.detail()
	usd := validCost(cost)
	if !o.usageChanged(used, size, in, out, reasoning, cacheRead, cacheWrite, usd) {
		return
	}
	o.h.Emit(event.Event{Type: event.TypeUsage, Usage: &event.Usage{
		Used: used, Size: size,
		Input: in, Output: out, Reasoning: reasoning,
		CacheRead: cacheRead, CacheWrite: cacheWrite,
		CostUSD: usd,
	}})
}

// usageChanged reports whether (used, size) differs from the last report
// actually emitted, recording it when it does. turnCleanup clears the latch so
// every turn reports at least once even when the numbers have not moved.
func (o *httpSession) usageChanged(used, size int, in, out, reasoning, cacheRead, cacheWrite int64, cost *float64) bool {
	// The detail participates in the latch: a turn whose context total is
	// unchanged can still have moved its buckets or its cost, and suppressing
	// that would leave the breakdown stale while the headline looked current.
	key := usageKey{used, size, in, out, reasoning, cacheRead, cacheWrite, cost == nil, 0}
	if cost != nil {
		key.cost = *cost
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.usageSent && o.lastUsage == key {
		return false
	}
	o.usageSent, o.lastUsage = true, key
	o.lastUsed, o.lastSize = used, size
	return true
}

// usageKey is the comparable identity of one usage report.
type usageKey struct {
	used, size                                int
	in, out, reasoning, cacheRead, cacheWrite int64
	costAbsent                                bool
	cost                                      float64
}

// contextLimit is the window size for a "providerID/modelID" key, or 0 when the
// catalog never reported one — clients render a bare token count then.
func (d *httpDialect) contextLimit(key string) int {
	if key == "" {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.contextLimits[key]
}

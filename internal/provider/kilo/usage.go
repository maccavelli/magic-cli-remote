package kilo

import "github.com/maccavelli/magic-cli-remote/internal/event"

// msgTokens is the token block Kilo puts on an assistant message.
type msgTokens struct {
	Input     int `json:"input"`
	Output    int `json:"output"`
	Reasoning int `json:"reasoning"`
	Cache     struct {
		Read  int `json:"read"`
		Write int `json:"write"`
	} `json:"cache"`
}

// inContext is what the turn left occupying the context window: everything the
// model read (fresh plus cache-read input) and everything it wrote. Cache
// *writes* are excluded — they are the same content counted a second time as it
// is stored, and adding them double-counts the prompt.
func (t msgTokens) inContext() int {
	return t.Input + t.Cache.Read + t.Output + t.Reasoning
}

// msgModel is the model reference on a Kilo message. The field name varies
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
// Kilo has no usage stream of its own, so this is where the daemon's
// usage_update (and therefore /context) comes from on this provider. User
// messages carry no tokens and are ignored.
//
// The engine re-sends message.updated many times across a turn, mostly with
// identical token counts. Each repeat used to cost a WebSocket frame and — as
// usage_update is not batchable on the phone — an immediate transcript commit,
// which defeated the client's own coalescing window (MADR 0024 §1.1). Only a
// changed report is emitted now.
func (o *httpSession) emitUsage(role string, tokens *msgTokens, model *msgModel) {
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
	// Every field kilo already reports is passed through (MADR 0137, second
	// correction). They were parsed into msgTokens, used to compute `used`,
	// and then discarded — so `cold`, which is derived from CacheRead == 0,
	// could only ever be true for kilo. A warm turn reading 14336 cached
	// tokens was recorded as cold, and two phases of analysis were drawn from
	// that.
	if !o.usageChanged(used, size, tokens) {
		return
	}
	o.h.Emit(event.Event{Type: event.TypeUsage, Usage: &event.Usage{
		Used: used, Size: size,
		Input:      int64(tokens.Input),
		Output:     int64(tokens.Output),
		Reasoning:  int64(tokens.Reasoning),
		CacheRead:  int64(tokens.Cache.Read),
		CacheWrite: int64(tokens.Cache.Write),
	}})
}

// usageChanged reports whether (used, size) differs from the last report
// actually emitted, recording it when it does. turnCleanup clears the latch so
// every turn reports at least once even when the numbers have not moved.
func (o *httpSession) usageChanged(used, size int, tokens *msgTokens) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	// The cache counts are part of the comparison, not just the totals. A turn
	// can hold `used` steady while the split between fresh and cached input
	// moves — which is exactly the transition this record cares about — and
	// latching on the total alone would suppress the report that shows it.
	if o.usageSent && o.lastUsed == used && o.lastSize == size &&
		o.lastTokens == *tokens {
		return false
	}
	o.usageSent, o.lastUsed, o.lastSize, o.lastTokens = true, used, size, *tokens
	return true
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

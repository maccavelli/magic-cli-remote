package session

import (
	"log/slog"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// turnLatency is one turn's timing and token cost, logged once when the turn
// ends (MADR 0137 Phase 2).
//
// It exists because the daemon measured the fast part and never the slow one:
// providers log how long it took to hand a prompt to an engine (tens of
// milliseconds) and then nothing until the turn is over, so a regression from
// ~1s to 5-18s in the number the user actually feels produced no signal at all.
// It was only recoverable at all because session history happens to carry
// per-event timestamps.
type turnLatency struct {
	sessionID string
	provider  string
	model     string
	// sess is held so the model can be asked for outside m.mu. Calling into
	// provider code under the manager lock would put a third-party mutex
	// inside it for nothing.
	sess     provider.Session
	ttft     time.Duration
	haveTTFT bool
	turn     time.Duration
	usage    *event.Usage
	failed   bool
}

// isTurnOutput reports whether an event is the first thing a user would see
// come back from a turn. Reasoning counts: a thought chunk is the moment the
// screen stops being empty, which is what "time to first token" means to the
// person waiting.
func isTurnOutput(t event.Type) bool {
	switch t {
	case event.TypeAssistantChunk, event.TypeThoughtChunk, event.TypeToolCall:
		return true
	}
	return false
}

// newTurnLatency snapshots a finished turn. Caller holds m.mu.
func newTurnLatency(e *entry, ev event.Event) *turnLatency {
	end := ev.Timestamp
	if end.IsZero() {
		end = time.Now().UTC()
	}
	r := &turnLatency{
		sessionID: e.meta.ID,
		provider:  string(e.meta.Provider),
		model:     e.meta.Model,
		sess:      e.sess,
		turn:      end.Sub(e.promptAt),
		usage:     e.lastUsage,
		failed:    ev.Type == event.TypeError,
	}
	if !e.firstOutputAt.IsZero() {
		r.ttft = e.firstOutputAt.Sub(e.promptAt)
		r.haveTTFT = true
	}
	return r
}

// log emits exactly one line per turn.
//
// ttft_ms is omitted rather than reported as zero when a turn produced no
// output: a turn that answered instantly and a turn that answered never are
// not the same event, and a zero would merge them.
func (r *turnLatency) log(l *slog.Logger) {
	if r == nil || l == nil {
		return
	}
	// Meta.Model is what the CLIENT asked for, so it is empty whenever the
	// session runs on the provider's own default — which MADR 0137 makes the
	// norm. Ask the session what it is actually running on (MADR 0137, eighth
	// amendment). Outside m.mu, so a provider mutex never nests inside it.
	model := r.model
	if model == "" {
		if mr, ok := r.sess.(provider.ModelReporter); ok {
			model = mr.CurrentModel()
		}
	}
	attrs := []any{
		slog.String("session_id", r.sessionID),
		slog.String("provider", r.provider),
		slog.Int64("turn_ms", r.turn.Milliseconds()),
	}
	if model != "" {
		attrs = append(attrs, slog.String("model", model))
	}
	if r.haveTTFT {
		attrs = append(attrs, slog.Int64("ttft_ms", r.ttft.Milliseconds()))
	}
	if r.failed {
		attrs = append(attrs, slog.Bool("failed", true))
	}
	if u := r.usage; u != nil {
		// cold says whether this turn paid a full uncached prefill. It is the
		// variable that separates a sub-second answer from a multi-second one
		// (MADR 0137 correction), so a latency number without it is not
		// evidence.
		//
		// It is reported ONLY when the provider reports cache accounting at
		// all. goose reports none, and a bare `CacheRead == 0` there says
		// "this turn was cold" when the truth is "nobody knows" — which is how
		// four providers' warm turns were recorded as cold for two phases
		// (MADR 0137, second correction). An engine that cannot answer must
		// leave the field absent rather than answer wrongly.
		if u.CacheRead > 0 || u.CacheWrite > 0 || u.Input > 0 {
			attrs = append(attrs, slog.Bool("cold", u.CacheRead == 0))
		}
		attrs = append(attrs, slog.Int("context_used", u.Used))
		if u.Input > 0 {
			attrs = append(attrs, slog.Int64("input_tokens", u.Input))
		}
		if u.Output > 0 {
			attrs = append(attrs, slog.Int64("output_tokens", u.Output))
		}
		if u.Reasoning > 0 {
			attrs = append(attrs, slog.Int64("reasoning_tokens", u.Reasoning))
		}
		if u.CacheRead > 0 {
			attrs = append(attrs, slog.Int64("cache_read", u.CacheRead))
		}
		if u.CacheWrite > 0 {
			attrs = append(attrs, slog.Int64("cache_write", u.CacheWrite))
		}
	}
	l.Info("turn latency", attrs...)
}

package acpagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// xaiUsage is grok's token accounting, carried on the `turn_completed` variant
// of `_x.ai/session_notification`.
//
// It exists because ACP's standard `usage_update` is `{used, size, cost}`, a
// context gauge with no per-turn split or cache fields. grok reports a cached
// input share — 11776 and 5888 tokens on observed turns — and with only the
// standard update mapped, every grok turn was recorded as having paid a full
// uncached prefill (MADR 0137, second correction). The unstable
// `PromptResponse.usage` does carry a turn's split, cache counts included; grok
// 1.0.13 sends it only under its answer's `_meta`, so this stays grok's
// channel, and a turn it has reported is not counted again (emitTurnUsage;
// MADR 0207 amendment H, D25).
type xaiUsage struct {
	InputTokens         int `json:"inputTokens"`
	OutputTokens        int `json:"outputTokens"`
	TotalTokens         int `json:"totalTokens"`
	CachedReadTokens    int `json:"cachedReadTokens"`
	CacheCreationTokens int `json:"cacheCreationTokens"`
	ReasoningTokens     int `json:"reasoningTokens"`
	// CostUsdTicks is this one turn's cost, in grok's 1e10 ticks to the
	// dollar. A pointer because absent is not zero (runtime.go's promptUsage).
	CostUsdTicks *int64 `json:"costUsdTicks"`
	// CostIsPartial means "do not trust this turn's cost".
	CostIsPartial bool `json:"costIsPartial"`
}

// GrokSessionState is grok's own state for one session (Spec.NewSessionState;
// MADR 0208, amendment of 2026-11-01).
//
// The cost ledger is the session's cost so far, summed from every turn's
// `costUsdTicks`, replayed or live. prompts holds the prompt_ids already
// counted, and unknown is set once a turn's cost is missing or partial, after
// which no total is reported (MADR 0199 E1, E3).
//
// The compaction counter numbers the compactions grok reports on its own
// channel, which carry no ID, and open is the one grok has started and not yet
// ended (MADR 0208, P4b deviation 1). Touched only from the notification
// handler goroutine.
type GrokSessionState struct {
	mu      sync.Mutex
	ticks   int64
	prompts map[string]bool
	unknown bool

	compactions    int
	compactionOpen string
}

// NewGrokSessionState is grok's Spec.NewSessionState.
func NewGrokSessionState() any { return &GrokSessionState{} }

// grokState returns h's grok state. A session built without grok's Spec has
// none, and gets a fresh one that is not kept.
func grokState(h Host) *GrokSessionState {
	if st, ok := h.SpecState().(*GrokSessionState); ok {
		return st
	}
	return &GrokSessionState{}
}

// addTurnCost folds one turn's cost into the session total (MADR 0199 E1, E3).
// A prompt_id already counted adds nothing; a turn without one is counted,
// because nothing could match it. A missing or partial cost makes the total
// unknowable, and it stays so for the life of this agent process.
func (st *GrokSessionState) addTurnCost(promptID string, u *xaiUsage) {
	if u == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if u.CostUsdTicks == nil || u.CostIsPartial {
		st.unknown = true
		return
	}
	if promptID != "" {
		if st.prompts[promptID] {
			return
		}
		if st.prompts == nil {
			st.prompts = make(map[string]bool)
		}
		st.prompts[promptID] = true
	}
	st.ticks += *u.CostUsdTicks
}

// sessionCost is the total so far, or nil once it is unknowable.
func (st *GrokSessionState) sessionCost() *event.SessionCost {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.unknown {
		return nil
	}
	return &event.SessionCost{Amount: float64(st.ticks) / usdTicksPerUSD, Currency: "USD"}
}

// emitXAIUsage publishes grok's token split, and on a live turn the session's
// cost so far (MADR 0199 E2).
//
// Size is left at whatever the standard usage_update reported, which is 0 here:
// grok's turn_completed carries no context-window size, and inventing one from
// the model catalog would report a limit this message never stated. A client
// renders a bare token count when size is 0, which is the honest rendering.
//
// Input is grok's `inputTokens` as sent, with `cachedReadTokens` alongside as
// the cached share of it. The two are not added — that would double-count the
// cached prefix.
//
// A replayed turn reports no session cost: the daemon replaces its stored total
// with any figure it is sent (MADR 0189 D2), so a sum taken partway through a
// replay would briefly undercut it.
func emitXAIUsage(h Host, u *xaiUsage, replay bool) {
	if u == nil || u.TotalTokens <= 0 {
		return
	}
	usage := &event.Usage{
		Used:       u.TotalTokens,
		Input:      int64(u.InputTokens),
		Output:     int64(u.OutputTokens),
		Reasoning:  int64(u.ReasoningTokens),
		CacheRead:  int64(u.CachedReadTokens),
		CacheWrite: int64(u.CacheCreationTokens),
	}
	if !replay && !h.Loading() {
		usage.SessionCost = grokState(h).sessionCost()
	}
	h.ReportUsage(usage, replay)
}

// HandleXAISessionUpdate handles grok's `_x.ai/session/update`, the channel a
// session/load replays grok's own turn records on (MADR 0199, amendment). Live
// turns arrive on `_x.ai/session_notification` instead.
//
// Only `turn_completed` is read, and only for its cost: a relaunch starts a new
// grok process whose ledger is empty, and these replayed costs are what
// rebuild the session's total. Nothing is emitted from here — the history the
// replay restores is the standard `session/update` stream's to deliver. Every
// other variant is declined by name.
func HandleXAISessionUpdate(_ context.Context, h Host, params json.RawMessage) {
	var env struct {
		SessionID string `json:"sessionId"`
		Update    struct {
			SessionUpdate string    `json:"sessionUpdate"`
			PromptID      string    `json:"prompt_id"`
			Usage         *xaiUsage `json:"usage"`
		} `json:"update"`
	}
	if err := json.Unmarshal(params, &env); err != nil {
		if log := h.Log(); log != nil {
			log.Debug("x.ai session/update: parse failed", slog.String("err", err.Error()))
		}
		return
	}
	// The same origin rule as the live channel: a child session's records are
	// not this conversation's spend.
	if id := env.SessionID; id != "" {
		if live := h.AgentSessionID(); live != "" && id != live {
			return
		}
	}
	if env.Update.SessionUpdate == "turn_completed" {
		grokState(h).addTurnCost(env.Update.PromptID, env.Update.Usage)
		return
	}
	h.Decline("_x.ai/session/update." + env.Update.SessionUpdate)
}

// beginTurnUsage starts a turn's usage accounting: nothing reported yet.
func (s *session) beginTurnUsage() {
	s.mu.Lock()
	s.turnUsageReported = false
	s.mu.Unlock()
}

// emitTurnUsage publishes the token split an agent's prompt answer carries, the
// unstable `PromptResponse.usage`, unless the Spec reported this turn on its
// own channel first (Host.ReportUsage), as grok does before the answer (frames
// 82 and 85 of grok 1.0.13's wire capture), so a turn is never counted twice
// (MADR 0207 amendment H, D25). The accounting of MADR 0189 and 0199 is
// unchanged: no cost travels here.
//
// The phone replaces its usage with each report's, so this report carries the
// context gauge the last usage_update set. Before any, Used is the turn's total
// and Size 0, as emitXAIUsage reports it.
func (s *session) emitTurnUsage(resp acp.PromptResponse) {
	//lint:ignore SA1019 the unstable ACP schema's turn usage (MADR 0207 D25); 0207 amendment H
	u := resp.Usage
	if u == nil {
		return
	}
	s.mu.Lock()
	reported, used, size := s.turnUsageReported, s.gaugeUsed, s.gaugeSize
	s.mu.Unlock()
	if reported {
		return
	}
	if used == 0 && size == 0 {
		used = saturatingInt(u.TotalTokens)
	}
	usage := &event.Usage{
		Used:   used,
		Size:   size,
		Input:  int64(saturatingInt(u.InputTokens)),
		Output: int64(saturatingInt(u.OutputTokens)),
	}
	if u.ThoughtTokens != nil {
		usage.Reasoning = int64(saturatingInt(*u.ThoughtTokens))
	}
	if u.CachedReadTokens != nil {
		usage.CacheRead = int64(saturatingInt(*u.CachedReadTokens))
	}
	if u.CachedWriteTokens != nil {
		usage.CacheWrite = int64(saturatingInt(*u.CachedWriteTokens))
	}
	s.emit(event.Event{
		Type:      event.TypeUsage,
		SessionID: s.localID,
		Usage:     usage,
	})
}

package grok

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// grok's extension notifications on acpagent.Host: its session notification, its
// replayed turn records, and its MCP and model catalog updates (MADR 0208 D2).

// handleSessionNotification handles grok's `_x.ai/session_notification`.
//
// grok 0.2.114 reports a full sub-agent lifecycle on this channel, addressed to
// the *parent* session — richer than anything the other three providers offer,
// including a terminal status. Measured payloads (MADR 0051 §10.2):
//
//	subagent_spawned  {subagent_id, subagent_type, description, child_session_id,
//	                   parent_session_id, role, model, capability_mode, …}
//	subagent_progress {subagent_id, duration_ms, turn_count, tokens_used, …}
//	subagent_finished {subagent_id, status, tool_calls, turns, output, …}
//
// Every other variant on this channel is ignored: `tool_call_delta_chunk` also
// arrives here, including for the child, and it is not status this session
// should publish. `turn_completed` is now read for its token accounting only
// (MADR 0137, second correction) — never for status, which the ACP turn result
// already owns.
//
// `subagent_finished.output` is deliberately dropped — it is the sub-agent's
// full report, i.e. exactly the content MADR 0051 keeps out of the transcript.
// The parent relays the conclusion itself.
func handleSessionNotification(ctx context.Context, h acpagent.Host, params json.RawMessage) {
	var env struct {
		SessionID string `json:"sessionId"`
		Update    struct {
			SessionUpdate string `json:"sessionUpdate"`
			SubagentID    string `json:"subagent_id"`
			SubagentType  string `json:"subagent_type"`
			Description   string `json:"description"`
			Status        string `json:"status"`
			// retry_state fields (captured live from grok 1.0.0, MADR 0073
			// follow-up): {"sessionUpdate":"retry_state","type":"failed",
			// "error_type":"auth","message":"Unauthorized (401) from …"}.
			Type    string `json:"type"`
			Message string `json:"message"`
			// turn_completed carries grok's token accounting, including the
			// cached share of the input. ACP's own SessionUsageUpdate has no
			// cache fields at all (acp-go-sdk types_gen.go:5243), so this
			// vendor channel is the ONLY place grok's cold/warm signal exists
			// (MADR 0137, second correction).
			Usage *xaiUsage `json:"usage"`
			// PromptID names the turn, so its cost is counted once (MADR 0199 E1).
			PromptID string `json:"prompt_id"`
			// The auto_compact_* variants' own text: a failure's error, and a
			// completion's summary preview (MADR 0208, P4b deviation 1).
			Error          string  `json:"error"`
			SummaryPreview *string `json:"summary_preview"`
		} `json:"update"`
		Meta struct {
			// IsReplay marks a turn grok re-sends from history (MADR 0199 E2).
			IsReplay bool `json:"isReplay"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(params, &env); err != nil {
		if log := h.Log(); log != nil {
			log.Debug("x.ai session_notification: parse failed",
				slog.String("err", err.Error()))
		}
		return
	}
	// Same origin rule as SessionUpdate: the child's own frames come down this
	// channel too, and they are not this conversation.
	if id := env.SessionID; id != "" {
		if live := h.AgentSessionID(); live != "" && id != live {
			return
		}
	}

	u := env.Update
	// retry_state is grok's structured channel for provider-API trouble
	// mid-turn — the analog of stderr backoff lines (MADR 0073). A terminal
	// "failed" is skipped on purpose: the session/prompt JSON-RPC error that
	// carries the same cause (in its `data` field) arrives immediately after
	// and owns the turn's error emission — handling both would double-post.
	// Non-terminal states (silent retry sleeps) go through the same
	// limit-abort machinery as stderr scraping, which dedupes via
	// limitNotified and only ever acts on classified limit text.
	if u.SessionUpdate == "turn_completed" && u.Usage != nil {
		stateOf(h).addTurnCost(u.PromptID, u.Usage)
		emitXAIUsage(h, u.Usage, env.Meta.IsReplay)
		return
	}
	if u.SessionUpdate == "retry_state" {
		if u.Type != "failed" && u.Message != "" {
			h.NoteLimitText(u.Message)
		}
		return
	}
	if _, ok := xaiCompactionStatus[u.SessionUpdate]; ok {
		onXAICompaction(h, u.SessionUpdate, u.Error, u.SummaryPreview)
		return
	}
	if u.SubagentID == "" {
		return
	}
	switch u.SessionUpdate {
	case "subagent_spawned":
		h.NoteSubagent(u.SubagentID, u.SubagentType, u.Description,
			event.SubagentStatusRunning)
	case "subagent_progress":
		// Refresh only; a progress frame for an agent we never saw spawn is
		// still evidence it is running.
		h.NoteSubagent(u.SubagentID, u.SubagentType, u.Description,
			event.SubagentStatusRunning)
	case "subagent_finished":
		status := event.SubagentStatusFailed
		if u.Status == "completed" {
			status = event.SubagentStatusCompleted
		}
		h.NoteSubagent(u.SubagentID, u.SubagentType, u.Description, status)
	default:
		// Not sub-agent status; nothing to publish.
	}
}

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

// sessionState is grok's own state for one session (Spec.NewSessionState;
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
type sessionState struct {
	mu      sync.Mutex
	ticks   int64
	prompts map[string]bool
	unknown bool

	compactions    int
	compactionOpen string
}

// newSessionState is grok's Spec.NewSessionState.
func newSessionState() any { return &sessionState{} }

// stateOf returns h's grok state. A session built without grok's Spec has
// none, and gets a fresh one that is not kept.
func stateOf(h acpagent.Host) *sessionState {
	if st, ok := h.SpecState().(*sessionState); ok {
		return st
	}
	return &sessionState{}
}

// addTurnCost folds one turn's cost into the session total (MADR 0199 E1, E3).
// A prompt_id already counted adds nothing; a turn without one is counted,
// because nothing could match it. A missing or partial cost makes the total
// unknowable, and it stays so for the life of this agent process.
func (st *sessionState) addTurnCost(promptID string, u *xaiUsage) {
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
func (st *sessionState) sessionCost() *event.SessionCost {
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
func emitXAIUsage(h acpagent.Host, u *xaiUsage, replay bool) {
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
		usage.SessionCost = stateOf(h).sessionCost()
	}
	h.ReportUsage(usage, replay)
}

// handleSessionUpdate handles grok's `_x.ai/session/update`, the channel a
// session/load replays grok's own turn records on (MADR 0199, amendment). Live
// turns arrive on `_x.ai/session_notification` instead.
//
// Only `turn_completed` is read, and only for its cost: a relaunch starts a new
// grok process whose ledger is empty, and these replayed costs are what
// rebuild the session's total. Nothing is emitted from here — the history the
// replay restores is the standard `session/update` stream's to deliver. Every
// other variant is declined by name.
func handleSessionUpdate(_ context.Context, h acpagent.Host, params json.RawMessage) {
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
		stateOf(h).addTurnCost(env.Update.PromptID, env.Update.Usage)
		return
	}
	h.Decline("_x.ai/session/update." + env.Update.SessionUpdate)
}

// xaiCompactionStatus maps grok's four compaction variants on
// `_x.ai/session_notification` to ACP's compaction status (MADR 0208, P4b
// deviation 1). grok compacts on `/compact` and on a full context window, and
// reports it only here: the standard compaction_update is never sent.
var xaiCompactionStatus = map[string]acp.CompactionStatus{
	"auto_compact_started":   acp.CompactionStatusInProgress,
	"auto_compact_completed": acp.CompactionStatusCompleted,
	"auto_compact_failed":    acp.CompactionStatusFailed,
	"auto_compact_cancelled": acp.CompactionStatusCancelled,
}

// onXAICompaction folds one of grok's compaction variants into the same state
// ACP's compaction_update produces, and emits it (MADR 0190 D1's shape).
//
// grok's variants carry no ID, so Magic numbers them. `started` opens one, and
// the next terminal variant closes it. A terminal variant with none open is a
// whole compaction on its own: grok's `/compact` sends `auto_compact_completed`
// alone, with no `started` before it.
//
// The summary is grok's `summary_preview` and the error is grok's `error`, as
// sent. Nothing of Magic's own wording is added (MADR 0208 D11).
func onXAICompaction(h acpagent.Host, variant, errText string, preview *string) {
	status, ok := xaiCompactionStatus[variant]
	if !ok {
		return
	}
	st := stateOf(h)
	id := st.compactionOpen
	if id == "" {
		st.compactions++
		id = fmt.Sprintf("xai-%d", st.compactions)
	}
	if status == acp.CompactionStatusInProgress {
		st.compactionOpen = id
	} else {
		st.compactionOpen = ""
	}
	h.UpdateCompaction(id, status, preview, errText)
}

// handleMCPServersUpdated handles grok's `_x.ai/mcp/servers_updated`.
//
// It carries the engine's full current MCP membership — name, transport and
// URL per server — which is the list `_x.ai/mcp/server_status` reports state
// against. Unrouted, mcremote's server list came only from its own config, so
// a server grok added or dropped at runtime was invisible in /status and any
// status frame naming it created a phantom entry (MADR 0137 F11).
//
// Membership only; state is not invented here. A server appearing in this list
// with no status frame yet is recorded as "connecting", not "ready" — claiming
// a server is up because it was mentioned is exactly the guess this daemon's
// status output must not make.
func handleMCPServersUpdated(_ context.Context, h acpagent.Host, params json.RawMessage) {
	var p struct {
		MCPServers []struct {
			Name string `json:"name"`
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		if log := h.Log(); log != nil {
			log.Debug("x.ai mcp/servers_updated: parse failed",
				slog.String("err", err.Error()))
		}
		return
	}
	if len(p.MCPServers) == 0 {
		return
	}
	h.UpdateMCPStatus(func(cur []provider.MCPServerStatus) []provider.MCPServerStatus {
		known := make(map[string]struct{}, len(cur))
		for _, st := range cur {
			known[st.Name] = struct{}{}
		}
		for _, srv := range p.MCPServers {
			if srv.Name == "" {
				continue
			}
			if _, ok := known[srv.Name]; ok {
				continue
			}
			cur = append(cur, provider.MCPServerStatus{
				Name:  srv.Name,
				State: "connecting",
			})
		}
		return cur
	})
}

// handleMCPInitProgress handles grok's `_x.ai/mcp/init_progress`.
//
// `{total, connected, sessionId}` — how many MCP servers the engine is waiting
// on before it can serve a turn. This bears directly on first-token latency:
// MCP startup is work that happens between the prompt and the first token, and
// unrouted it was invisible, so a session waiting on a slow MCP server looked
// identical to a slow model.
//
// Logged, not emitted as a transcript event. A progress counter is diagnostic
// detail — it belongs where an operator investigating latency will find it, not
// in a conversation. Promoting it to a phone-visible event is a protocol and UI
// decision that needs its own record.
func handleMCPInitProgress(_ context.Context, h acpagent.Host, params json.RawMessage) {
	var p struct {
		Total     int    `json:"total"`
		Connected int    `json:"connected"`
		SessionID string `json:"sessionId"`
	}
	log := h.Log()
	if err := json.Unmarshal(params, &p); err != nil || log == nil {
		return
	}
	// Origin rule, as everywhere else on this channel: a child session's
	// progress is not this conversation's.
	if p.SessionID != "" {
		if live := h.AgentSessionID(); live != "" && p.SessionID != live {
			return
		}
	}
	log.Info("grok mcp startup progress",
		slog.Int("connected", p.Connected),
		slog.Int("total", p.Total))
}

// handleModelsUpdate handles _x.ai/models/update (grok 1.0.3) and the
// historical _x.ai/models_update spelling.
func handleModelsUpdate(ctx context.Context, h acpagent.Host, params json.RawMessage) {
	var p struct {
		AvailableModels []availableModel `json:"availableModels"`
		CurrentModelID  string           `json:"currentModelId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		if log := h.Log(); log != nil {
			log.Debug("models_update: parse failed", slog.String("err", err.Error()))
		}
		return
	}
	if len(p.AvailableModels) == 0 {
		return
	}
	h.SetModelCatalog(modelsToCatalog(p.CurrentModelID, p.AvailableModels))
}

// handleMCPStatus handles _x.ai/mcp/server_status notifications.
func handleMCPStatus(ctx context.Context, h acpagent.Host, params json.RawMessage) {
	var p struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	if p.Name == "" {
		return
	}
	h.UpdateMCPStatus(func(cur []provider.MCPServerStatus) []provider.MCPServerStatus {
		for i, st := range cur {
			if st.Name == p.Name {
				cur[i].State = p.Status
				return cur
			}
		}
		return append(cur, provider.MCPServerStatus{
			Name:  p.Name,
			State: p.Status,
		})
	})
}

// handleMCPInit handles _x.ai/mcp_initialized notifications.
func handleMCPInit(ctx context.Context, h acpagent.Host, params json.RawMessage) {
	servers := h.Config().McpServers
	h.UpdateMCPStatus(func(cur []provider.MCPServerStatus) []provider.MCPServerStatus {
		if len(cur) == 0 && len(servers) > 0 {
			for _, srv := range servers {
				cur = append(cur, provider.MCPServerStatus{
					Name:  srv.Name,
					State: "ready",
				})
			}
		}
		return cur
	})
}

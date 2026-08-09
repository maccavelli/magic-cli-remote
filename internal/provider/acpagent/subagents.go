package acpagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// maxSubagentField caps a name or task so one long description cannot bloat
// every subsequent set snapshot.
const maxSubagentField = 300

// subagentState is what this session knows about one child agent.
type subagentState struct {
	name   string
	task   string
	status string
}

// HandleXAISessionNotification handles grok's `_x.ai/session_notification`.
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
// Every other variant on this channel is ignored: `tool_call_delta_chunk` and
// `turn_completed` also arrive here, including for the child, and none of them
// is status this session should publish.
//
// `subagent_finished.output` is deliberately dropped — it is the sub-agent's
// full report, i.e. exactly the content MADR 0051 keeps out of the transcript.
// The parent relays the conclusion itself.
func HandleXAISessionNotification(ctx context.Context, s *session, params json.RawMessage) {
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
		} `json:"update"`
	}
	if err := json.Unmarshal(params, &env); err != nil {
		if s.log != nil {
			s.log.Debug("x.ai session_notification: parse failed",
				slog.String("err", err.Error()))
		}
		return
	}
	// Same origin rule as SessionUpdate: the child's own frames come down this
	// channel too, and they are not this conversation.
	if id := env.SessionID; id != "" {
		if live := s.AgentSessionID(); live != "" && id != live {
			return
		}
	}

	u := env.Update
	// retry_state is grok's structured channel for provider-API trouble
	// mid-turn — the analog of goose's stderr backoff lines. A terminal
	// "failed" is skipped on purpose: the session/prompt JSON-RPC error that
	// carries the same cause (in its `data` field) arrives immediately after
	// and owns the turn's error emission — handling both would double-post.
	// Non-terminal states (silent retry sleeps) go through the same
	// limit-abort machinery as stderr scraping, which dedupes via
	// limitNotified and only ever acts on classified limit text.
	if u.SessionUpdate == "retry_state" {
		if u.Type != "failed" && u.Message != "" {
			s.noteEngineLogLine(u.Message)
		}
		return
	}
	if u.SubagentID == "" {
		return
	}
	switch u.SessionUpdate {
	case "subagent_spawned":
		s.noteSubagent(u.SubagentID, u.SubagentType, u.Description,
			event.SubagentStatusRunning)
	case "subagent_progress":
		// Refresh only; a progress frame for an agent we never saw spawn is
		// still evidence it is running.
		s.noteSubagent(u.SubagentID, u.SubagentType, u.Description,
			event.SubagentStatusRunning)
	case "subagent_finished":
		status := event.SubagentStatusFailed
		if u.Status == "completed" {
			status = event.SubagentStatusCompleted
		}
		s.noteSubagent(u.SubagentID, u.SubagentType, u.Description, status)
	default:
		// Not sub-agent status; nothing to publish.
	}
}

// noteSubagent records or updates one sub-agent and republishes the set.
//
// A finished entry never reverts to running: a late progress frame for an agent
// that already reported finished must not reopen it.
func (s *session) noteSubagent(id, name, task, status string) {
	s.mu.Lock()
	if s.subagents == nil {
		s.subagents = make(map[string]subagentState)
	}
	st, existed := s.subagents[id]
	if existed && st.status != event.SubagentStatusRunning &&
		status == event.SubagentStatusRunning {
		s.mu.Unlock()
		return
	}
	if name != "" {
		st.name = truncateRunes(name, maxSubagentField)
	}
	if st.name == "" {
		st.name = "subagent"
	}
	if task != "" {
		st.task = truncateRunes(task, maxSubagentField)
	}
	st.status = status
	s.subagents[id] = st
	s.mu.Unlock()
	s.emitSubagents()
}

// emitSubagents publishes the whole current set (replace semantics, exactly
// like TypePlan). Never call it holding s.mu.
func (s *session) emitSubagents() {
	s.mu.Lock()
	ids := make([]string, 0, len(s.subagents))
	for id := range s.subagents {
		ids = append(ids, id)
	}
	// Map iteration is random; the panel must not reshuffle between updates.
	slices.Sort(ids)
	out := make([]event.SubagentInfo, 0, len(ids))
	for _, id := range ids {
		st := s.subagents[id]
		out = append(out, event.SubagentInfo{
			ID:     id,
			Name:   st.name,
			Task:   st.task,
			Status: st.status,
		})
	}
	s.subagentsPublished = true
	localID := s.localID
	s.mu.Unlock()

	s.emit(event.Event{
		Type:      event.TypeSubagents,
		SessionID: localID,
		Timestamp: time.Now().UTC(),
		Subagents: out,
	})
}

// clearSubagents drops the set at turn end and tells the client to hide the
// panel.
//
// A no-op unless a set was actually published: the overwhelming majority of
// turns spawn no sub-agent, and a clear for them would put a control event on
// the wire at every turn end to undo something that never happened.
func (s *session) clearSubagents() {
	s.mu.Lock()
	published := s.subagentsPublished
	s.subagentsPublished = false
	s.subagents = nil
	localID := s.localID
	s.mu.Unlock()
	if !published {
		return
	}
	s.emit(event.Event{
		Type:      event.TypeSubagents,
		SessionID: localID,
		Timestamp: time.Now().UTC(),
	})
}

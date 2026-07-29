package codex

import (
	"encoding/json"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// maxSubagentField caps a name or task so one long prompt cannot bloat every
// subsequent set snapshot.
const maxSubagentField = 300

// subagentState is what this session knows about one sub-agent thread.
type subagentState struct {
	name   string
	task   string
	status string
}

// subAgentActivityItem is codex's `subAgentActivity` thread item.
//
// These are the fields the 0.145.0 schema actually declares as required —
// `id, type, kind, agentThreadId, agentPath`. The daemon used to parse
// `agentName`, `goal` and `instructions`, none of which exist, so every card
// rendered as the literal string "sub-agent" with an empty detail
// (MADR 0051 §10.3).
type subAgentActivityItem struct {
	AgentThreadID string `json:"agentThreadId"`
	AgentPath     string `json:"agentPath"`
	// Kind is started | interacted | interrupted. Note the absence of any
	// terminal value: nothing on this item ever says a sub-agent finished, so
	// completion comes from collabAgentToolCall.agentsStates or from turn end.
	Kind string `json:"kind"`
}

// collabAgentToolCallItem is codex's `collabAgentToolCall` thread item.
//
// `agentsStates` is the authoritative per-agent status map and the only
// trustworthy source of entries: measured twice live, codex emits this item
// with tool="wait", empty receiverThreadIds and empty agentsStates even when
// nothing was spawned, so deriving an entry from the item's mere existence
// invents a sub-agent on every wait (MADR 0051 §10.3).
type collabAgentToolCallItem struct {
	Tool              string   `json:"tool"`
	ReceiverThreadIDs []string `json:"receiverThreadIds"`
	Prompt            string   `json:"prompt"`
	AgentsStates      map[string]struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"agentsStates"`
}

// collabAgentStatus maps codex's CollabAgentStatus vocabulary onto the
// daemon's. The enum is pendingInit | running | interrupted | completed |
// errored | shutdown | notFound.
func collabAgentStatus(s string) string {
	switch s {
	case "pendingInit", "running":
		return event.SubagentStatusRunning
	case "completed", "shutdown":
		return event.SubagentStatusCompleted
	case "interrupted", "errored", "notFound":
		return event.SubagentStatusFailed
	}
	// An unrecognised status is more likely a new in-flight state than a
	// terminal one; reading it as running keeps the panel honest about work
	// that may still be happening.
	return event.SubagentStatusRunning
}

// agentDisplayName turns codex's agentPath into something worth showing —
// "…/agents/explore.md" becomes "explore".
func agentDisplayName(agentPath string) string {
	base := path.Base(strings.TrimSpace(agentPath))
	if base == "." || base == "/" || base == "" {
		return ""
	}
	return strings.TrimSuffix(base, path.Ext(base))
}

// noteSubagentItem routes the two sub-agent item types into the set and
// reports whether it handled the item, so the caller skips tool-card rendering
// for them.
func (s *session) noteSubagentItem(itemType string, item json.RawMessage) bool {
	switch itemType {
	case "subAgentActivity":
		s.noteSubAgentActivity(item)
		return true
	case "collabAgentToolCall":
		s.noteCollabToolCall(item)
		return true
	}
	return false
}

// noteSubAgentActivity records a subAgentActivity item.
func (s *session) noteSubAgentActivity(item json.RawMessage) {
	var p subAgentActivityItem
	if json.Unmarshal(item, &p) != nil || p.AgentThreadID == "" {
		return
	}
	// No terminal kind exists, so every activity frame means "still there".
	s.noteSubagent(p.AgentThreadID, agentDisplayName(p.AgentPath), "",
		event.SubagentStatusRunning)
}

// noteCollabToolCall records whatever a collabAgentToolCall says about the
// agents it names. Entries come from agentsStates only.
func (s *session) noteCollabToolCall(item json.RawMessage) {
	var p collabAgentToolCallItem
	if json.Unmarshal(item, &p) != nil {
		return
	}
	for id, st := range p.AgentsStates {
		if id == "" {
			continue
		}
		s.noteSubagent(id, "", p.Prompt, collabAgentStatus(st.Status))
	}
}

// noteSubagent records or updates one sub-agent and republishes the set.
//
// A finished entry never reverts to running: a late activity frame for an agent
// that already reported terminal must not reopen it.
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
		st.name = "sub-agent"
	}
	if task != "" {
		st.task = truncateRunes(task, maxSubagentField)
	}
	if existed && st == s.subagents[id] && st.status == status {
		// Nothing observable changed; do not re-publish the set.
		s.mu.Unlock()
		return
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
	s.mu.Unlock()

	s.emit(event.Event{
		Type:           event.TypeSubagents,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		Subagents:      out,
		AgentSessionID: s.agentID,
	})
}

// clearSubagents drops the set at turn end and hides the panel.
//
// This is also the only completion signal for entries seeded from
// subAgentActivity, whose `kind` enum has no terminal value (MADR 0051 §10.3).
//
// A no-op unless a set was actually published: nearly every turn spawns no
// sub-agent, and a clear for them would put a control event on the wire at
// every turn end to undo something that never happened.
func (s *session) clearSubagents() {
	s.mu.Lock()
	published := s.subagentsPublished
	s.subagentsPublished = false
	s.subagents = nil
	s.mu.Unlock()
	if !published {
		return
	}
	s.emit(event.Event{
		Type:           event.TypeSubagents,
		SessionID:      s.localID,
		Timestamp:      time.Now().UTC(),
		AgentSessionID: s.agentID,
	})
}

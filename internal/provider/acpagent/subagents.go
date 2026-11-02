package acpagent

import (
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

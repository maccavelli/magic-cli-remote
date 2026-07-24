package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

const subagentToolPrefix = "subagent:"

// handleSessionLifecycle processes session.created / session.updated (MADR 0020 PR2).
func (o *httpSession) handleSessionLifecycle(props json.RawMessage) {
	var p struct {
		SessionID string `json:"sessionID"`
		Info      struct {
			ID       string `json:"id"`
			ParentID string `json:"parentID"`
			Title    string `json:"title"`
		} `json:"info"`
	}
	if json.Unmarshal(props, &p) != nil {
		return
	}
	id := firstNonEmpty(p.Info.ID, p.SessionID, sessionIDOf(props))
	if id == "" {
		return
	}
	parent := o.h.AgentSessionID()
	if id == parent {
		// Parent session metadata update — no tree bind.
		return
	}
	// Child or grandchild for this local session (demux already routed here).
	o.h.BindChildAlias(id)
	o.h.NoteNodeStatus(id, httpagent.NodeBusy)
	title := firstNonEmpty(p.Info.Title, "subagent")
	o.emitSubagentCard(id, title, "running")
}

// handleSessionDeleted processes session.deleted.
func (o *httpSession) handleSessionDeleted(props json.RawMessage) {
	id := firstNonEmpty(sessionIDOf(props), o.h.EventAgentSessionID())
	if id == "" || id == o.h.AgentSessionID() {
		return
	}
	o.h.UnbindChildAlias(id)
	o.h.NoteNodeStatus(id, httpagent.NodeIdle)
	o.completeSubagentCard(id)
}

// handleSessionStatus processes session.status (busy/idle/retry).
func (o *httpSession) handleSessionStatus(props json.RawMessage) {
	var p struct {
		SessionID string `json:"sessionID"`
		Status    struct {
			Type    string `json:"type"`
			Attempt int    `json:"attempt"`
			Message string `json:"message"`
			Next    int    `json:"next"`
		} `json:"status"`
	}
	if json.Unmarshal(props, &p) != nil {
		return
	}
	sid := firstNonEmpty(p.SessionID, sessionIDOf(props), o.h.EventAgentSessionID(), o.h.AgentSessionID())
	switch strings.ToLower(p.Status.Type) {
	case "busy":
		o.h.NoteNodeStatus(sid, httpagent.NodeBusy)
		if sid == o.h.AgentSessionID() {
			o.h.Emit(event.Event{Type: event.TypeSessionStatus, Status: "running"})
		}
	case "retry":
		o.h.NoteNodeStatus(sid, httpagent.NodeRetry)
		msg := firstNonEmpty(p.Status.Message, "agent retrying")
		o.h.Emit(event.Event{
			Type: event.TypeNotice,
			Text: clip(fmt.Sprintf("Retry (attempt %d): %s", p.Status.Attempt, msg), 300),
		})
	case "idle":
		o.h.NoteNodeStatus(sid, httpagent.NodeIdle)
		o.tryTreeEndTurn()
	}
}

// tryTreeEndTurn ends the phone turn when the whole tree is idle and completes
// synthetic subagent cards.
func (o *httpSession) tryTreeEndTurn() {
	if !o.h.TryEndTurnIfTreeIdle() {
		return
	}
	o.completeAllSubagentCards()
	o.turnCleanup()
	o.h.Emit(event.Event{Type: event.TypeTurnComplete, Status: "end_turn", StopReason: "end_turn"})
	o.h.Emit(event.Event{Type: event.TypeSessionStatus, Status: "idle"})
}

// ConfirmTreeIdle implements [httpagent.TreeIdleConfirmer] (MADR 0020 §6.3.1).
// Status map is global — only keys in treeIDs are considered.
func (o *httpSession) ConfirmTreeIdle(ctx context.Context, parentID string, knownTreeIDs []string) (stillBusy, discovered []string, err error) {
	if parentID == "" {
		return nil, nil, nil
	}
	// Discover children of this parent (bounded BFS for nested trees).
	treeSet := make(map[string]struct{}, len(knownTreeIDs)+4)
	treeSet[parentID] = struct{}{}
	for _, id := range knownTreeIDs {
		if id != "" {
			treeSet[id] = struct{}{}
		}
	}
	queue := []string{parentID}
	seenWalk := map[string]struct{}{parentID: {}}
	const maxDepth = 4
	for depth := 0; depth < maxDepth && len(queue) > 0; depth++ {
		next := queue
		queue = nil
		for _, id := range next {
			kids, kerr := o.fetchChildren(ctx, id)
			if kerr != nil {
				// Children probe failed — still try status with known set.
				o.h.Log().Debug("idle-confirm children fetch failed",
					slog.String("session", id), slog.String("err", kerr.Error()))
				continue
			}
			for _, c := range kids {
				if c == "" {
					continue
				}
				if _, ok := treeSet[c]; !ok {
					discovered = append(discovered, c)
					treeSet[c] = struct{}{}
				}
				if _, ok := seenWalk[c]; !ok {
					seenWalk[c] = struct{}{}
					queue = append(queue, c)
				}
			}
		}
	}

	statusMap, serr := o.fetchSessionStatus(ctx)
	if serr != nil {
		// Soft-fail: local treeNodes already looked idle. A hard error would
		// pin the phone on "running" forever when /session/status is down.
		o.h.Log().Warn("idle-confirm status fetch failed; using local tree only",
			slog.String("err", serr.Error()))
		return nil, discovered, nil
	}
	for id := range treeSet {
		st, ok := statusMap[id]
		if !ok {
			continue // missing → treat idle
		}
		switch strings.ToLower(st) {
		case "busy", "retry":
			stillBusy = append(stillBusy, id)
		}
	}
	return stillBusy, discovered, nil
}

func (o *httpSession) fetchChildren(ctx context.Context, sessionID string) ([]string, error) {
	var out []struct {
		ID string `json:"id"`
	}
	if err := o.h.API()(ctx, "GET", "/session/"+sessionID+"/children"+o.dir(), nil, &out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out))
	for _, s := range out {
		if s.ID != "" {
			ids = append(ids, s.ID)
		}
	}
	return ids, nil
}

// fetchSessionStatus returns map sessionID → status type string (idle|busy|retry).
func (o *httpSession) fetchSessionStatus(ctx context.Context) (map[string]string, error) {
	// OpenAPI: { [sessionID]: SessionStatus } where SessionStatus is a tagged union.
	var raw map[string]json.RawMessage
	if err := o.h.API()(ctx, "GET", "/session/status"+o.dir(), nil, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(raw))
	for id, blob := range raw {
		var st struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(blob, &st) == nil && st.Type != "" {
			out[id] = st.Type
			continue
		}
		// Some engines may return a bare string.
		var s string
		if json.Unmarshal(blob, &s) == nil && s != "" {
			out[id] = s
		}
	}
	return out, nil
}

func (o *httpSession) emitSubagentCard(agentID, title, status string) {
	toolID := subagentToolPrefix + agentID
	o.mu.Lock()
	if o.subagents == nil {
		o.subagents = make(map[string]struct{})
	}
	_, seen := o.subagents[agentID]
	if status == "running" {
		o.subagents[agentID] = struct{}{}
	}
	o.mu.Unlock()
	typ := event.TypeToolUpdate
	if !seen && status == "running" {
		typ = event.TypeToolCall
	}
	o.h.Emit(event.Event{
		Type:     typ,
		ToolID:   toolID,
		ToolName: clip(title, 300),
		ToolKind: "other",
		Status:   status,
		Text:     clip(title, 300),
	})
}

func (o *httpSession) completeSubagentCard(agentID string) {
	o.mu.Lock()
	if o.subagents == nil {
		o.mu.Unlock()
		return
	}
	if _, ok := o.subagents[agentID]; !ok {
		o.mu.Unlock()
		return
	}
	delete(o.subagents, agentID)
	o.mu.Unlock()
	o.h.Emit(event.Event{
		Type:     event.TypeToolUpdate,
		ToolID:   subagentToolPrefix + agentID,
		ToolName: "subagent",
		ToolKind: "other",
		Status:   "completed",
	})
}

func (o *httpSession) completeAllSubagentCards() {
	o.mu.Lock()
	ids := make([]string, 0, len(o.subagents))
	for id := range o.subagents {
		ids = append(ids, id)
	}
	o.subagents = nil
	o.mu.Unlock()
	for _, id := range ids {
		o.h.Emit(event.Event{
			Type:     event.TypeToolUpdate,
			ToolID:   subagentToolPrefix + id,
			ToolName: "subagent",
			ToolKind: "other",
			Status:   "completed",
		})
	}
}

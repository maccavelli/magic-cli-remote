package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

const subagentToolPrefix = "subagent:"

// Card states for the synthetic subagent tool cards tracked in httpSession.subagents.
const (
	cardRunning   = "running"
	cardCompleted = "completed"
)

// handleSessionLifecycle processes session.created / session.updated (MADR 0020 PR2).
//
// created distinguishes the two: only session.created is evidence that a child
// started working. OpenCode also emits session.updated for a child AFTER it
// goes idle (final token counts, summary, title). Treating that as "busy" —
// as this did before — permanently wedged the parent turn: the tree never
// looked idle again, no further frame arrived for the child, and the phone sat
// on "running" until the stall watchdog resynced ~2 minutes later.
func (o *httpSession) handleSessionLifecycle(props json.RawMessage, created bool) {
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
		// Parent session metadata update: forward the title so the client
		// sees the latest conversation topic (OpenCode updates it after the
		// first turn completes). No tree bind needed.
		if title := strings.TrimSpace(p.Info.Title); title != "" {
			o.h.Emit(event.Event{
				Type: event.TypeNotice,
				Text: "Session title: " + clip(title, 200),
			})
		}
		return
	}
	// Child or grandchild for this local session (demux already routed here).
	// Bind either way so the child's own frames keep routing to this transcript.
	o.h.BindChildAlias(id)
	title := firstNonEmpty(p.Info.Title, "subagent")
	if created {
		o.h.NoteNodeStatus(id, httpagent.NodeBusy)
		o.emitSubagentCard(id, title, "running")
		return
	}
	// Metadata update. Refresh the card only while it is still open; a closed
	// card must not reopen, and an unknown child must not be seeded busy —
	// BindChildAlias defaults unknown children to busy for the live-child case,
	// so undo that here. Real work always announces itself via session.created
	// or session.status=busy, both of which mark the node busy explicitly.
	if !o.refreshSubagentCard(id, title) {
		o.h.NoteNodeStatus(id, httpagent.NodeIdle)
	}
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
			o.emitStatus("running")
		}
	case "retry":
		o.h.NoteNodeStatus(sid, httpagent.NodeRetry)
		msg := firstNonEmpty(p.Status.Message, "agent retrying")
		text := fmt.Sprintf("Retry (attempt %d): %s", p.Status.Attempt, msg)
		if p.Status.Next > 0 {
			// next is ms until next try in OpenCode session.status retry.
			secs := (p.Status.Next + 999) / 1000
			if secs < 1 {
				secs = 1
			}
			text = fmt.Sprintf("Retry (attempt %d) in %ds: %s", p.Status.Attempt, secs, msg)
		}
		o.h.Emit(event.Event{
			Type: event.TypeNotice,
			Text: clip(text, 300),
		})
	case "idle":
		o.noteNodeIdle(sid)
		o.tryTreeEndTurn()
	}
}

// noteNodeIdle marks a tree node idle and, for a child, closes its subagent
// card right away instead of leaving it spinning until the parent's turn ends.
func (o *httpSession) noteNodeIdle(sid string) {
	o.h.NoteNodeStatus(sid, httpagent.NodeIdle)
	if sid != "" && sid != o.h.AgentSessionID() {
		o.completeSubagentCard(sid)
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
	o.emitStatus("idle")
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
		// Soft-fail: local treeNodes already looked idle, so nodes we already
		// knew about stay idle — a hard error would pin the phone on "running"
		// forever when /session/status is down. Children we only just
		// discovered are the exception: with no liveness oracle we have no
		// evidence either way, so report them busy. That keeps the turn active
		// (and therefore keeps 0014 resync armed) rather than ending it on a
		// tree we could not verify.
		o.h.Log().Warn("idle-confirm status fetch failed; treating newly discovered children as busy",
			slog.String("err", serr.Error()),
			slog.Int("discovered", len(discovered)))
		return append([]string(nil), discovered...), discovered, nil
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

// emitSubagentCard opens (or re-renders) the synthetic tool card for a child
// agent session. A card that already completed this turn is never reopened —
// OpenCode keeps emitting session.updated for a finished child.
func (o *httpSession) emitSubagentCard(agentID, title, status string) {
	o.mu.Lock()
	if o.subagents == nil {
		o.subagents = make(map[string]string)
	}
	prev, seen := o.subagents[agentID]
	if prev == cardCompleted {
		o.mu.Unlock()
		return
	}
	o.subagents[agentID] = cardRunning
	o.mu.Unlock()
	typ := event.TypeToolUpdate
	if !seen {
		typ = event.TypeToolCall
	}
	o.h.Emit(event.Event{
		Type:     typ,
		ToolID:   subagentToolPrefix + agentID,
		ToolName: clip(title, 300),
		ToolKind: "other",
		Status:   status,
		Text:     clip(title, 300),
	})
}

// refreshSubagentCard re-renders an already-open card (title/summary changed)
// and reports whether one was open. It never opens or reopens a card.
func (o *httpSession) refreshSubagentCard(agentID, title string) bool {
	o.mu.Lock()
	if o.subagents[agentID] != cardRunning {
		o.mu.Unlock()
		return false
	}
	o.mu.Unlock()
	o.h.Emit(event.Event{
		Type:     event.TypeToolUpdate,
		ToolID:   subagentToolPrefix + agentID,
		ToolName: clip(title, 300),
		ToolKind: "other",
		Status:   "running",
		Text:     clip(title, 300),
	})
	return true
}

// completeSubagentCard closes an open card. The id is kept (marked completed)
// rather than deleted so a later session.updated cannot reopen it; the whole
// map is dropped by turnCleanup.
func (o *httpSession) completeSubagentCard(agentID string) {
	o.mu.Lock()
	if o.subagents[agentID] != cardRunning {
		o.mu.Unlock()
		return
	}
	o.subagents[agentID] = cardCompleted
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
	for id, st := range o.subagents {
		if st == cardRunning {
			ids = append(ids, id)
			o.subagents[id] = cardCompleted
		}
	}
	o.mu.Unlock()
	// Deterministic order so a turn with several open subagent cards closes them
	// the same way every run (map iteration is random).
	slices.Sort(ids)
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

package kilo

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// subagentState is what this session knows about one child agent session.
//
// name/task come straight off the child's session info: opencode 1.18.7 sends
// `agent` ("general", "explore") and `title` ("Read a.txt and b.txt (@general
// subagent)") on session.created. Only `title` used to be read, so the agent's
// identity was discarded.
type subagentState struct {
	name   string
	task   string
	status string
}

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
			// Agent is the child's role — "general", "explore" — sent by
			// opencode 1.18.7 on a subagent's session.created and previously
			// ignored, so the panel had only the task to show.
			Agent string `json:"agent"`
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
	// Bind either way so the child's own frames keep routing to this transcript.
	o.h.BindChildAlias(id)
	name := firstNonEmpty(p.Info.Agent, "subagent")
	task := p.Info.Title
	if created {
		o.h.NoteNodeStatus(id, httpagent.NodeBusy)
		o.noteSubagentStarted(id, name, task)
		return
	}
	// Metadata update. Refresh only while the child is still running; a
	// finished one must not reopen, and an unknown child must not be seeded
	// busy — BindChildAlias defaults unknown children to busy for the
	// live-child case, so undo that here. Real work always announces itself via
	// session.created or session.status=busy, both of which mark the node busy
	// explicitly.
	if !o.refreshSubagent(id, p.Info.Agent, task) {
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
	o.finishSubagent(id, event.SubagentStatusCompleted)
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
		// next is ms until next try in OpenCode session.status retry.
		next := time.Duration(p.Status.Next) * time.Millisecond
		// Hard limit / long silent wait: surface a limit card and end the
		// turn so the phone is not stuck "running" for minutes/hours while
		// OpenCode sleeps (parity with goose/codex/grok — MADR 0073).
		// Short transient retries keep the existing notice-only path.
		cls := agenterr.Present(msg, time.Now())
		hard := cls.Kind == agenterr.KindQuota ||
			next >= agenterr.LongBackoffMin ||
			agenterr.LooksLikeLongBackoff(msg)
		if hard && (sid == "" || sid == o.h.AgentSessionID()) {
			if next > 0 && cls.ResetAt.IsZero() {
				cls.ResetAt = time.Now().Add(next)
			}
			out := cls.Message
			if out == "" {
				out = clip(msg, 400)
			}
			active := o.h.EndTurn()
			if active {
				o.finishAllSubagents()
				o.turnCleanup()
				o.clearSubagents()
				o.finishApprovals()
				o.h.Emit(event.Event{
					Type:       event.TypeTurnComplete,
					Status:     "error",
					StopReason: "error",
				})
				o.h.Emit(event.Event{
					Type:      event.TypeError,
					Error:     out,
					ErrorKind: string(cls.Kind),
					RetryAt:   cls.ResetAt,
				})
				o.emitStatus("error")
			}
			return
		}
		text := fmt.Sprintf("Retry (attempt %d): %s", p.Status.Attempt, msg)
		if p.Status.Next > 0 {
			secs := (p.Status.Next + 999) / 1000
			if secs < 1 {
				secs = 1
			}
			text = fmt.Sprintf("Retry (attempt %d) in %ds: %s", p.Status.Attempt, secs, msg)
		}
		// Soft rate-limit retries keep the turn alive; prefer Present copy
		// when the message is a known limit so the notice is actionable.
		if cls.Kind == agenterr.KindRateLimit || cls.Kind == agenterr.KindQuota {
			if cls.Message != "" {
				text = fmt.Sprintf("Retry (attempt %d): %s", p.Status.Attempt, cls.Message)
				if p.Status.Next > 0 {
					secs := (p.Status.Next + 999) / 1000
					if secs < 1 {
						secs = 1
					}
					text = fmt.Sprintf("Retry (attempt %d) in %ds: %s", p.Status.Attempt, secs, cls.Message)
				}
			}
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
		o.finishSubagent(sid, event.SubagentStatusCompleted)
	}
}

// tryTreeEndTurn ends the phone turn when the whole tree is idle and completes
// synthetic subagent cards.
func (o *httpSession) tryTreeEndTurn() {
	if !o.h.TryEndTurnIfTreeIdle() {
		return
	}
	o.finishAllSubagents()
	o.turnCleanup()
	// turnCleanup dropped the map; tell the phone to drop the panel.
	o.clearSubagents()
	// Before the turn boundary, so the card is marked done ahead of it.
	o.finishApprovals()
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

// maxSubagentField caps a name or task so one long child title cannot bloat
// every subsequent set snapshot.
const maxSubagentField = 300

// noteSubagentStarted records a child agent session as running and republishes
// the set. A child that already completed this turn is never reopened —
// OpenCode keeps emitting session.updated for a finished child.
func (o *httpSession) noteSubagentStarted(agentID, name, task string) {
	o.mu.Lock()
	if o.subagents == nil {
		o.subagents = make(map[string]subagentState)
	}
	if o.subagents[agentID].status == event.SubagentStatusCompleted {
		o.mu.Unlock()
		return
	}
	o.subagents[agentID] = subagentState{
		name:   clip(name, maxSubagentField),
		task:   clip(task, maxSubagentField),
		status: event.SubagentStatusRunning,
	}
	o.mu.Unlock()
	o.emitSubagents()
}

// refreshSubagent updates the task of an already-running child and reports
// whether one was open. It never opens or reopens an entry.
func (o *httpSession) refreshSubagent(agentID, name, task string) bool {
	o.mu.Lock()
	st, ok := o.subagents[agentID]
	if !ok || st.status != event.SubagentStatusRunning {
		o.mu.Unlock()
		return false
	}
	if name != "" {
		st.name = clip(name, maxSubagentField)
	}
	if task != "" {
		st.task = clip(task, maxSubagentField)
	}
	o.subagents[agentID] = st
	o.mu.Unlock()
	o.emitSubagents()
	return true
}

// finishSubagent moves a running child to a terminal status. The id is kept
// (not deleted) so a later session.updated cannot reopen it; the whole map is
// dropped by turnCleanup.
func (o *httpSession) finishSubagent(agentID, status string) {
	o.mu.Lock()
	st, ok := o.subagents[agentID]
	if !ok || st.status != event.SubagentStatusRunning {
		o.mu.Unlock()
		return
	}
	st.status = status
	o.subagents[agentID] = st
	o.mu.Unlock()
	o.emitSubagents()
}

// finishAllSubagents closes every still-running child at turn end.
func (o *httpSession) finishAllSubagents() {
	o.mu.Lock()
	var changed bool
	for id, st := range o.subagents {
		if st.status == event.SubagentStatusRunning {
			st.status = event.SubagentStatusCompleted
			o.subagents[id] = st
			changed = true
		}
	}
	o.mu.Unlock()
	if changed {
		o.emitSubagents()
	}
}

// emitSubagents publishes the whole current set (replace semantics, exactly
// like TypePlan). Never call it holding o.mu: Emit of a control event blocks
// until consumed.
func (o *httpSession) emitSubagents() {
	o.mu.Lock()
	ids := make([]string, 0, len(o.subagents))
	for id := range o.subagents {
		ids = append(ids, id)
	}
	// Map iteration is random; the panel must not reshuffle between updates.
	slices.Sort(ids)
	out := make([]event.SubagentInfo, 0, len(ids))
	for _, id := range ids {
		st := o.subagents[id]
		out = append(out, event.SubagentInfo{
			ID:     id,
			Name:   st.name,
			Task:   st.task,
			Status: st.status,
		})
	}
	o.subagentsPublished = true
	o.mu.Unlock()
	o.h.Emit(event.Event{Type: event.TypeSubagents, Subagents: out})
}

// clearSubagents publishes an empty set so the panel disappears. Called after
// turnCleanup has dropped the map, and outside o.mu.
//
// A no-op unless this session actually published a set: the overwhelming
// majority of turns spawn no sub-agent, and emitting a clear for them would put
// a control event on the wire at every single turn end to undo something that
// never happened.
func (o *httpSession) clearSubagents() {
	o.mu.Lock()
	published := o.subagentsPublished
	o.subagentsPublished = false
	o.mu.Unlock()
	if !published {
		return
	}
	o.h.Emit(event.Event{Type: event.TypeSubagents})
}

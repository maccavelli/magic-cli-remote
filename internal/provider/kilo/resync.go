package kilo

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// Resync reconciles this session against engine state after an SSE gap
// (stream reconnect, stall watchdog) — H4 extended by MADR 0020 PR5.
//
// Order:
//  1. Discover/bind children for THIS parent; apply tree-scoped /session/status
//  2. Re-emit pending permissions / questions for treeIDs
//  3. Refresh parent todos → plan (even while tree busy — MADR 0020 Sprint 2)
//  4. If any tree node is busy/retry, keep the turn active (do not EndTurn)
//  5. Otherwise heal parent message log and EndTurn when the parent turn finished
//
// Gates (unchanged from 0014): transport only calls this while turn-active and
// not promptInFlight; stale-evidence and finished-only rules still apply to the
// parent message path.
func (o *httpSession) Resync(ctx context.Context, turnStartedAt time.Time) {
	parent := o.h.AgentSessionID()
	if parent == "" {
		return
	}

	treeIDs, treeBusy := o.resyncTreeState(ctx, parent)
	o.resyncPendingPermissions(ctx, parent, treeIDs)
	o.resyncPendingQuestions(ctx, parent, treeIDs)
	o.resyncTodos(ctx, parent)

	if treeBusy {
		o.h.Log().Debug("sse resync: tree still busy; keeping turn active",
			slog.String("agent_session_id", parent))
		return
	}

	o.resyncParentMessageTurn(ctx, turnStartedAt)
}

// resyncTreeState binds discovered children and applies status for treeIDs only.
// Returns the tree set and whether any node is busy/retry.
//
// Every treeID gets an explicit status, including the ones /session/status
// omits: the engine drops idle sessions from that map, and BindChildAlias
// seeds an unknown child as busy. Leaving such a child busy would outlive this
// resync and wedge the next tree-idle EndTurn — the same failure the
// session.updated regression caused.
func (o *httpSession) resyncTreeState(ctx context.Context, parent string) (treeIDs map[string]struct{}, busy bool) {
	treeIDs = map[string]struct{}{parent: {}}
	children := 0
	for _, id := range o.discoverTreeChildren(ctx, parent) {
		if id == "" || id == parent {
			continue
		}
		treeIDs[id] = struct{}{}
		children++
		o.h.BindChildAlias(id)
	}

	statusMap, err := o.fetchSessionStatus(ctx)
	if err != nil {
		o.h.Log().Debug("sse resync: status fetch failed",
			slog.String("err", err.Error()), slog.Int("children", children))
		// With children bound but unverifiable, report busy: their seeded-busy
		// marks stand, so ending the turn here would leave the tree inconsistent
		// for the next EndTurn. With no children there is nothing to verify and
		// the 0014 parent message-log recovery should still run.
		return treeIDs, children > 0
	}
	for id := range treeIDs {
		switch strings.ToLower(statusMap[id]) {
		case "busy":
			o.h.NoteNodeStatus(id, httpagent.NodeBusy)
			busy = true
		case "retry":
			o.h.NoteNodeStatus(id, httpagent.NodeRetry)
			busy = true
		default:
			// "idle" or absent from the map — the engine only lists non-idle
			// sessions, so a missing key is authoritative evidence of idle.
			o.h.NoteNodeStatus(id, httpagent.NodeIdle)
			if id != parent {
				o.finishSubagent(id, event.SubagentStatusCompleted)
			}
		}
	}
	return treeIDs, busy
}

// discoverTreeChildren returns all descendant session ids under parent (BFS).
func (o *httpSession) discoverTreeChildren(ctx context.Context, parent string) []string {
	var out []string
	queue := []string{parent}
	seen := map[string]struct{}{parent: {}}
	const maxDepth = 4
	for depth := 0; depth < maxDepth && len(queue) > 0; depth++ {
		next := queue
		queue = nil
		for _, id := range next {
			kids, err := o.fetchChildren(ctx, id)
			if err != nil {
				continue
			}
			for _, c := range kids {
				if c == "" {
					continue
				}
				if _, ok := seen[c]; ok {
					continue
				}
				seen[c] = struct{}{}
				out = append(out, c)
				queue = append(queue, c)
			}
		}
	}
	return out
}

// resyncPendingPermissions re-emits open permission sheets for this tree only.
func (o *httpSession) resyncPendingPermissions(ctx context.Context, parent string, treeIDs map[string]struct{}) {
	var list []struct {
		ID         string          `json:"id"`
		SessionID  string          `json:"sessionID"`
		Permission string          `json:"permission"`
		Patterns   []string        `json:"patterns"`
		Always     []string        `json:"always"`
		Metadata   json.RawMessage `json:"metadata"`
		// updated shape
		Type    string          `json:"type"`
		Pattern json.RawMessage `json:"pattern"`
		Title   string          `json:"title"`
	}
	if err := o.h.API()(ctx, "GET", "/permission"+o.dir(), nil, &list); err != nil {
		o.h.Log().Debug("sse resync: permission list failed", slog.String("err", err.Error()))
		return
	}
	for _, raw := range list {
		sid := raw.SessionID
		if sid != "" {
			if _, ok := treeIDs[sid]; !ok {
				continue // other session on shared engine
			}
		}
		// Prefer dual-shape normalize via JSON round-trip.
		b, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		p, ok := normalizePermissionAsk(b)
		if !ok {
			// Fall back to explicit fields.
			p = permAsk{
				ID:        raw.ID,
				SessionID: firstNonEmpty(raw.SessionID, parent),
				Name:      firstNonEmpty(raw.Permission, raw.Type),
				Patterns:  raw.Patterns,
				Always:    raw.Always,
			}
			if len(p.Patterns) == 0 && len(raw.Pattern) > 0 {
				p.Patterns = coerceStringOrArray(raw.Pattern)
			}
			p.Detail = strings.Join(p.Patterns, "\n")
			if p.Detail == "" {
				p.Detail = firstNonEmpty(raw.Title, shortJSON(raw.Metadata, 300))
			}
			if p.ID == "" {
				continue
			}
		}
		if p.SessionID == "" {
			p.SessionID = parent
		}
		o.emitPermissionAsk(p)
	}
}

// resyncParentMessageTurn is the original 0014 parent message-log recovery path.
func (o *httpSession) resyncParentMessageTurn(ctx context.Context, turnStartedAt time.Time) {
	var msgs []struct {
		Info struct {
			ID   string `json:"id"`
			Role string `json:"role"`
			Time struct {
				Created   float64 `json:"created"`
				Completed float64 `json:"completed"`
			} `json:"time"`
			Error *struct {
				Name string `json:"name"`
				Data struct {
					Message string `json:"message"`
				} `json:"data"`
			} `json:"error"`
		} `json:"info"`
		Parts []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err := o.h.API()(ctx, "GET", "/session/"+o.h.AgentSessionID()+"/message"+o.dir(), nil, &msgs); err != nil {
		o.h.Log().Warn("sse resync: message fetch failed", slog.String("err", err.Error()))
		return
	}
	if len(msgs) == 0 {
		return
	}
	last := msgs[len(msgs)-1]
	// Last message still the user's: the assistant reply has not started, so
	// the turn cannot have finished — nothing to recover.
	if last.Info.Role != "assistant" {
		return
	}
	if last.Info.Time.Completed == 0 && last.Info.Error == nil {
		return // turn still streaming engine-side
	}
	// Stale-evidence guard: a finish stamped before this turn began belongs to
	// a previous turn (the transport already excludes the prompt-submit
	// window, but the fetch can still race a just-accepted prompt). Engine and
	// daemon share the host clock, so a direct comparison is sound.
	ts := last.Info.Time.Completed
	if ts == 0 {
		ts = last.Info.Time.Created
	}
	if ts > 0 && time.UnixMilli(int64(ts)).Before(turnStartedAt) {
		return
	}
	// The turn finished while the stream was down. Heal the text first so the
	// tail lands before the turn-end events, as it would have on the stream.
	// emitTextCatchUp holds o.mu across the comparison, so it is safe against
	// a concurrently running SSE pump (part.delta handler also serializes on
	// o.mu). If the SSE pump's part.delta already added text since the
	// snapshot was fetched, the prefix comparison handles it correctly: the
	// authoritative snapshot is always the full text, so a stale prev with
	// extra text just means no delta is emitted (the delta handler already
	// streamed it).
	for _, part := range last.Parts {
		if part.Type == "text" || part.Type == "reasoning" {
			o.emitTextCatchUp(part.ID, part.Type, part.Text)
		}
	}
	// Mark parent idle so tree state matches finished message log.
	o.h.NoteNodeStatus(o.h.AgentSessionID(), httpagent.NodeIdle)
	if !o.h.EndTurn() {
		return // the live stream delivered the turn-end while we were fetching
	}
	o.finishAllSubagents()
	o.turnCleanup()
	o.clearSubagents()
	o.h.Log().Info("sse resync: recovered missed turn-end",
		slog.String("agent_session_id", o.h.AgentSessionID()),
		slog.Bool("errored", last.Info.Error != nil))
	if last.Info.Error != nil && last.Info.Error.Name != "MessageAbortedError" {
		msg := firstNonEmpty(last.Info.Error.Data.Message, last.Info.Error.Name, "agent error")
		cls := agenterr.Present(msg, time.Now())
		out := cls.Message
		if out == "" {
			out = clip(msg, 400)
		}
		o.h.Emit(event.Event{
			Type:      event.TypeError,
			Error:     out,
			ErrorKind: string(cls.Kind),
			RetryAt:   cls.ResetAt,
		})
		o.emitStatus("error")
		return
	}
	status := "end_turn"
	if last.Info.Error != nil {
		status = "cancelled" // MessageAbortedError: the lost frame was a cancel
	}
	o.h.Emit(event.Event{Type: event.TypeTurnComplete, Status: status, StopReason: status})
	o.emitStatus("idle")
}

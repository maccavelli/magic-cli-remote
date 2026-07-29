package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// permAsk is the normalized permission request after dual-shape mapping (MADR 0020 PR3).
type permAsk struct {
	ID        string
	SessionID string
	Name      string
	Detail    string
	Patterns  []string
	Always    []string
}

// normalizePermissionAsk maps permission.asked and permission.updated field shapes
// onto one struct. Field names differ: asked uses permission/patterns; updated uses
// type/pattern/title.
func normalizePermissionAsk(props json.RawMessage) (permAsk, bool) {
	var loose struct {
		ID         string          `json:"id"`
		SessionID  string          `json:"sessionID"`
		Permission string          `json:"permission"`
		Type       string          `json:"type"`
		Patterns   []string        `json:"patterns"`
		Pattern    json.RawMessage `json:"pattern"`
		Always     []string        `json:"always"`
		Title      string          `json:"title"`
		Metadata   json.RawMessage `json:"metadata"`
	}
	if json.Unmarshal(props, &loose) != nil || loose.ID == "" {
		return permAsk{}, false
	}
	patterns := loose.Patterns
	if len(patterns) == 0 && len(loose.Pattern) > 0 {
		patterns = coerceStringOrArray(loose.Pattern)
	}
	name := firstNonEmpty(loose.Permission, loose.Type)
	detail := strings.Join(patterns, "\n")
	if detail == "" {
		detail = firstNonEmpty(loose.Title, shortJSON(loose.Metadata, 300))
	}
	return permAsk{
		ID:        loose.ID,
		SessionID: loose.SessionID,
		Name:      name,
		Detail:    detail,
		Patterns:  patterns,
		Always:    loose.Always,
	}, true
}

// normalizePermissionV2Ask maps permission.v2.asked.
func normalizePermissionV2Ask(props json.RawMessage) (permAsk, bool) {
	var p struct {
		ID        string          `json:"id"`
		SessionID string          `json:"sessionID"`
		Action    string          `json:"action"`
		Resources []string        `json:"resources"`
		Save      []string        `json:"save"`
		Metadata  json.RawMessage `json:"metadata"`
	}
	if json.Unmarshal(props, &p) != nil || p.ID == "" {
		return permAsk{}, false
	}
	detail := strings.Join(p.Resources, "\n")
	if detail == "" {
		detail = shortJSON(p.Metadata, 300)
	}
	return permAsk{
		ID:        p.ID,
		SessionID: p.SessionID,
		Name:      p.Action,
		Detail:    detail,
		Patterns:  p.Resources,
		Always:    p.Save,
	}, true
}

func coerceStringOrArray(raw json.RawMessage) []string {
	var one string
	if json.Unmarshal(raw, &one) == nil && one != "" {
		return []string{one}
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return many
	}
	return nil
}

// emitPermissionAsk is the single funnel for every permission the engine can
// raise — permission.asked, permission.updated, permission.v2.asked and the
// resync replay all land here — so the auto-approve gate lives here and no
// future permission shape can route around it (MADR 0044 D3).
func (o *httpSession) emitPermissionAsk(p permAsk) {
	if p.ID == "" {
		return
	}
	origin := firstNonEmpty(p.SessionID, o.h.EventAgentSessionID(), o.h.AgentSessionID())

	auto := o.h.Config().AlwaysApprove || o.h.AutoApprove()
	if auto && o.alreadyAutoHandled(p.ID) {
		// A second frame for an id we already answered. Falling through would
		// re-Track it — resurrecting an id the first reply already claimed —
		// and send a duplicate reply.
		return
	}

	// Track origin and pending *before* deciding how to answer. The
	// session-scoped reply fallback needs the origin even on the auto path
	// (child-session permissions reply to a different REST path), and
	// TrackPermission is what makes a duplicated asked/updated pair for one id
	// collapse to a single reply — autoApprove claims the id, so the loser of
	// that race gets ErrPermissionNotPending and stops.
	o.h.TrackPermissionOrigin(p.ID, origin)
	o.h.TrackPermission(p.ID)

	if auto {
		o.autoApprove(p)
		return
	}
	o.emitPermissionSheet(p)
}

// maxAutoHandled bounds autoHandled so a long session cannot grow it without
// limit. Generous: it only has to outlive the window in which duplicate frames
// for one permission can arrive.
const maxAutoHandled = 512

// alreadyAutoHandled reports whether this id has already been routed to the
// auto-approve path, recording it if not. The claim is atomic so two SSE frames
// for one id cannot both proceed.
func (o *httpSession) alreadyAutoHandled(id string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.autoHandled == nil {
		o.autoHandled = make(map[string]struct{})
	}
	if _, ok := o.autoHandled[id]; ok {
		return true
	}
	o.autoHandled[id] = struct{}{}
	o.autoOrder = append(o.autoOrder, id)
	if len(o.autoOrder) > maxAutoHandled {
		drop := o.autoOrder[0]
		o.autoOrder = o.autoOrder[1:]
		delete(o.autoHandled, drop)
	}
	return false
}

// forgetAutoHandled releases an id whose auto-approval ultimately failed, so a
// later frame for it can be surfaced or retried rather than silently dropped.
func (o *httpSession) forgetAutoHandled(id string) {
	o.mu.Lock()
	delete(o.autoHandled, id)
	o.mu.Unlock()
}

// emitPermissionSheet surfaces one permission to the phone. Split out of
// emitPermissionAsk so the auto-approve fail-safe can reuse it verbatim rather
// than rebuilding the option list.
func (o *httpSession) emitPermissionSheet(p permAsk) {
	opts := []event.PermissionOption{
		{OptionID: "once", Name: "Allow once", Kind: "allow_once"},
	}
	if len(p.Always) > 0 {
		opts = append(opts, event.PermissionOption{OptionID: "always", Name: "Allow always", Kind: "allow_always"})
	}
	opts = append(opts, event.PermissionOption{OptionID: "reject", Name: "Reject", Kind: "reject_once"})
	o.h.Emit(event.Event{
		Type:         event.TypePermission,
		PermissionID: p.ID,
		ToolName:     firstNonEmpty(p.Name, "permission"),
		Text:         p.Detail,
		Options:      opts,
		Status:       "pending",
	})
}

// autoApproveAttempts bounds the retry. A permission that cannot be answered
// must end up in front of the user, not in a log line: the agent stays blocked
// until someone answers it (MADR 0044 D4.1).
const autoApproveAttempts = 3

// autoApproveBackoff is the delay before retry attempt N (1-indexed). Short on
// purpose: the engine is on loopback and the agent is blocked meanwhile.
func autoApproveBackoff(attempt int) time.Duration {
	return time.Duration(attempt) * 250 * time.Millisecond
}

// autoApprove answers one permission on the daemon's behalf.
//
// Owner: this session. Exit conditions: the reply is accepted, the id was
// claimed by someone else, the attempts are exhausted, or the session shuts
// down. It runs on its own goroutine because Emit of a control event blocks
// until consumed, and this is called from the SSE handler.
func (o *httpSession) autoApprove(p permAsk) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				o.h.Log().Error("auto-approve permission panic",
					slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()

		var err error
		for attempt := range autoApproveAttempts {
			if attempt > 0 {
				select {
				case <-o.h.Done():
					return
				case <-time.After(autoApproveBackoff(attempt)):
				}
			}
			// The transport's RespondPermission, not the dialect's: it claims
			// the id (dedupe, and it disarms the PermissionTimeout fail-safe),
			// clears the origin, emits permission_resolved and drains the
			// queue. Replying to the engine directly here would leave the
			// expiry armed to later cancel this very permission (D4.2).
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err = o.h.RespondPermission(ctx, p.ID, "once", false)
			cancel()
			if err == nil {
				o.h.Log().Info("auto-approved permission",
					slog.String("permission", firstNonEmpty(p.Name, "permission")),
					slog.String("patterns", strings.Join(p.Patterns, ",")),
					slog.String("agent_session_id",
						firstNonEmpty(p.SessionID, o.h.PermissionOrigin(p.ID))))
				o.emitApprovalSummary(p)
				return
			}
			if errors.Is(err, httpagent.ErrPermissionNotPending) {
				// Claimed elsewhere — the phone, a resync replay, or the expiry
				// fail-safe. Not an error, and retrying cannot help.
				return
			}
		}

		// Release the dedup claim: this id was not answered, so a later frame
		// for it should be free to try again rather than be dropped as a
		// duplicate.
		o.forgetAutoHandled(p.ID)
		o.h.Log().Warn("auto-approve failed; surfacing permission",
			slog.String("permission_id", p.ID), slog.String("err", err.Error()))
		o.h.Emit(event.Event{
			Type: event.TypeNotice,
			Text: "Auto-approve failed for this request — please answer it.",
		})
		o.emitPermissionSheet(p)
	}()
}

// approvalGroupID is the stable client-side upsert key for this session's
// auto-approval card. One card per turn: the client replaces rather than
// appends, so the whole turn's approvals collapse into a single expandable
// item instead of one line each (MADR 0051 Part I).
const approvalGroupID = "auto-approvals"

// maxApprovalItems bounds the per-turn audit list. Same order as
// maxAutoHandled: it only has to outlive one turn's tool executions.
const maxApprovalItems = 512

// emitApprovalSummary records one auto-approval and re-publishes the whole
// list for this turn.
//
// The entire function runs under approvalMu because autoApprove gives each
// permission its own goroutine: without it, two concurrent approvals can emit
// their snapshots out of order and the shorter one — arriving last — replaces
// the longer, dropping an approval from the audit permanently.
func (o *httpSession) emitApprovalSummary(p permAsk) {
	o.approvalMu.Lock()
	defer o.approvalMu.Unlock()

	now := time.Now().UTC()
	o.autoApprovals = append(o.autoApprovals, event.ApprovalItem{
		ToolName: firstNonEmpty(p.Name, "permission"),
		Detail:   permissionDetail(p),
		Time:     now,
	})
	if n := len(o.autoApprovals); n > maxApprovalItems {
		o.autoApprovals = append([]event.ApprovalItem(nil), o.autoApprovals[n-maxApprovalItems:]...)
	}
	o.h.Emit(event.Event{
		Type:            event.TypeApprovalSummary,
		Timestamp:       now,
		ApprovalGroupID: approvalGroupID,
		// Clone: the event is marshalled later, on the writer goroutine, while
		// the next append may reallocate or overwrite this backing array.
		Approvals: slices.Clone(o.autoApprovals),
		Status:    event.ApprovalStatusRunning,
		Text:      approvalFallbackText(len(o.autoApprovals)),
	})
}

// finishApprovals publishes the terminal summary for a turn and clears the
// list. Called when the turn ends and when the mode leaves auto.
//
// Never call it from turnCleanup: that holds o.mu, and Emit of a control event
// blocks until consumed.
func (o *httpSession) finishApprovals() {
	o.approvalMu.Lock()
	items := slices.Clone(o.autoApprovals)
	o.autoApprovals = nil
	o.approvalMu.Unlock()
	if len(items) == 0 {
		return
	}
	o.h.Emit(event.Event{
		Type:            event.TypeApprovalSummary,
		Timestamp:       time.Now().UTC(),
		ApprovalGroupID: approvalGroupID,
		Approvals:       items,
		Status:          event.ApprovalStatusCompleted,
		Text:            approvalFallbackText(len(items)),
	})
}

// approvalFallbackText is what a client that does not understand
// approval_summary renders instead: one system line, not twenty.
func approvalFallbackText(n int) string {
	return "Auto-approved (" + strconv.Itoa(n) + ")"
}

// maxPermissionSummary caps the audit line so one permission carrying a long
// pattern list cannot flood the transcript.
const maxPermissionSummary = 120

// permissionDetail renders a permission's patterns for the audit item. It
// carries the patterns only — never prompt text — and the tool name travels
// separately in ApprovalItem.ToolName.
//
// This replaces the old permissionSummary, which folded both into one
// "bash (git status)" string for the per-approval notice. Nothing needs that
// shape now: the structured item lets the client decide how to render, and the
// slog audit line builds its own fields.
func permissionDetail(p permAsk) string {
	pats := p.Patterns
	if len(pats) > 2 {
		pats = pats[:2]
	}
	return truncateRunes(strings.Join(pats, ", "), maxPermissionSummary)
}

// truncateRunes shortens s to at most maxRunes runes, appending an ellipsis
// when it cuts. It counts runes rather than bytes so a multi-byte character in
// a path or pattern is never split down the middle, and so the ellipsis itself
// (3 bytes) cannot push the result past the cap.
func truncateRunes(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	if maxRunes <= 1 {
		return "…"
	}
	return string(r[:maxRunes-1]) + "…"
}

// respondPermissionEngine prefers global /permission/{id}/reply, then session-scoped
// path using the permission's origin agent session (child or parent).
func (o *httpSession) respondPermissionEngine(ctx context.Context, permissionID, response string) error {
	// Global reply (OpenCode 1.18+ / v2 SDK).
	err := o.h.API()(ctx, "POST", "/permission/"+permissionID+"/reply"+o.dir(),
		map[string]string{"reply": response}, nil)
	if err == nil {
		return nil
	}
	origin := o.h.PermissionOrigin(permissionID)
	if origin == "" {
		origin = o.h.AgentSessionID()
	}
	return o.h.API()(ctx, "POST", "/session/"+origin+"/permissions/"+permissionID+o.dir(),
		map[string]string{"response": response}, nil)
}

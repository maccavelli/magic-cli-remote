package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"runtime/debug"
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

	// Track origin and pending *before* deciding how to answer. The
	// session-scoped reply fallback needs the origin even on the auto path
	// (child-session permissions reply to a different REST path), and
	// TrackPermission is what makes a duplicated asked/updated pair for one id
	// collapse to a single reply — autoApprove claims the id, so the loser of
	// that race gets ErrPermissionNotPending and stops.
	o.h.TrackPermissionOrigin(p.ID, origin)
	o.h.TrackPermission(p.ID)

	if o.h.Config().AlwaysApprove || o.h.AutoApprove() {
		o.autoApprove(p)
		return
	}
	o.emitPermissionSheet(p)
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
				o.h.Emit(event.Event{
					Type: event.TypeNotice,
					Text: "Auto-approved: " + permissionSummary(p),
				})
				return
			}
			if errors.Is(err, httpagent.ErrPermissionNotPending) {
				// Claimed elsewhere — the phone, a resync replay, or the expiry
				// fail-safe. Not an error, and retrying cannot help.
				return
			}
		}

		o.h.Log().Warn("auto-approve failed; surfacing permission",
			slog.String("permission_id", p.ID), slog.String("err", err.Error()))
		o.h.Emit(event.Event{
			Type: event.TypeNotice,
			Text: "Auto-approve failed for this request — please answer it.",
		})
		o.emitPermissionSheet(p)
	}()
}

// maxPermissionSummary caps the audit line so one permission carrying a long
// pattern list cannot flood the transcript.
const maxPermissionSummary = 120

// permissionSummary renders one permission as "bash (git status)" for the audit
// notice. It carries the tool name and its patterns only — never prompt text.
func permissionSummary(p permAsk) string {
	name := firstNonEmpty(p.Name, "permission")
	pats := p.Patterns
	if len(pats) > 2 {
		pats = pats[:2]
	}
	out := name
	if len(pats) > 0 {
		out = name + " (" + strings.Join(pats, ", ") + ")"
	}
	if len(out) > maxPermissionSummary {
		out = out[:maxPermissionSummary-1] + "…"
	}
	return out
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

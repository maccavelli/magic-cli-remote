package opencode

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
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

// emitPermissionAsk tracks origin and emits permission_request (or auto-approves).
// TakePending/TrackPermission already dedupe double sheets for the same id.
func (o *httpSession) emitPermissionAsk(p permAsk) {
	if p.ID == "" {
		return
	}
	origin := firstNonEmpty(p.SessionID, o.h.EventAgentSessionID(), o.h.AgentSessionID())

	if o.h.Config().AlwaysApprove {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = o.RespondPermission(ctx, p.ID, "once", false)
		}()
		return
	}
	o.h.TrackPermissionOrigin(p.ID, origin)
	o.h.TrackPermission(p.ID)
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

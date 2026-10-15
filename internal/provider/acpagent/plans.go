package acpagent

import (
	"encoding/json"
	"time"
	"unicode/utf8"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpcommon"
)

// maxPlanContent caps a Markdown plan's text. The agent chooses its length,
// and the phone shows it collapsed (MADR 0192 D1).
const maxPlanContent = 32 << 10

// onPlanUpdate maps an identified plan (ACP plan_update, unstable) onto an
// identified_plan event carrying its ID and shape. A file plan's URI is passed
// on as sent and never read. An unknown shape is declined by name (MADR 0192
// D1).
func (s *session) onPlanUpdate(u *acp.SessionPlanUpdate, now time.Time) {
	ev := event.Event{Type: event.TypeIdentifiedPlan, SessionID: s.localID, Timestamp: now}
	var meta map[string]any
	switch c := u.Plan; {
	case c.Items != nil:
		ev.PlanID, ev.PlanKind, meta = string(c.Items.PlanId), event.PlanKindItems, c.Items.Meta
		ev.Entries = acpcommon.PlanEntries(c.Items.Entries)
	case c.Markdown != nil:
		ev.PlanID, ev.PlanKind, meta = string(c.Markdown.PlanId), event.PlanKindMarkdown, c.Markdown.Meta
		ev.PlanContent, ev.PlanTruncated = capPlanContent(c.Markdown.Content)
	case c.File != nil:
		ev.PlanID, ev.PlanKind, meta = string(c.File.PlanId), event.PlanKindFile, c.File.Meta
		ev.PlanURI = c.File.Uri
	default:
		s.declineUpdate("plan_update." + unknownPlanKind(c.Unknown))
		return
	}
	if len(u.Meta) > 0 || len(meta) > 0 {
		s.declineUpdate("plan_update._meta")
	}
	s.emit(ev)
}

// onPlanRemoved removes the one identified plan it names. It never touches
// the stable plan, which the 2026-07-21 mapping cleared (MADR 0192 D2).
func (s *session) onPlanRemoved(u *acp.SessionUpdatePlanRemoved, now time.Time) {
	if len(u.Meta) > 0 {
		s.declineUpdate("plan_removed._meta")
	}
	s.emit(event.Event{
		Type:        event.TypeIdentifiedPlan,
		SessionID:   s.localID,
		Timestamp:   now,
		PlanID:      string(u.PlanId),
		PlanRemoved: true,
	})
}

// capPlanContent bounds a Markdown plan at maxPlanContent, cutting on a rune
// boundary, and reports whether anything was lost.
func capPlanContent(s string) (string, bool) {
	if len(s) <= maxPlanContent {
		return s, false
	}
	cut := maxPlanContent
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// unknownPlanKind names an unknown plan shape by its "type", for the decline.
func unknownPlanKind(raw json.RawMessage) string {
	var v struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Type == "" {
		return "unknown"
	}
	return v.Type
}

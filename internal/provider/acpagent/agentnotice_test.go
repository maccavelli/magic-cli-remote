package acpagent

import (
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestANoticeArrivesOverTheWire is PLAN 0188 N3: once Magic advertises
// session.notices, an agent's notice sent over a real connection, through the
// SDK's capability gate as production configures it, reaches the session as
// an agent_notice. Without the advertisement the SDK refuses it before the
// handler runs (MADR 0188 D4).
func TestANoticeArrivesOverTheWire(t *testing.T) {
	s, a := newWireSession(t, `{}`)
	a.send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": "agent-1",
			"update": map[string]any{
				"sessionUpdate": "notice", "severity": "warning", "title": "gopls unavailable",
			},
		},
	})
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeAgentNotice {
				if ev.NoticeSeverity != "warning" || ev.NoticeTitle != "gopls unavailable" {
					t.Fatalf("agent_notice = %q/%q", ev.NoticeSeverity, ev.NoticeTitle)
				}
				return
			}
		case <-deadline:
			t.Fatal("no agent_notice within 5s: the notice did not get past the SDK's capability gate")
		}
	}
}

// TestNoticeBecomesAnAgentNotice is PLAN 0188 N1 step 1: an ACP `notice` of
// each severity, with and without a description, becomes an agent_notice
// event carrying the same fields; `_meta` is declined by name (MADR 0188 D1).
func TestNoticeBecomesAnAgentNotice(t *testing.T) {
	for _, tc := range []struct {
		update, severity, title, description string
		declines                             []string
	}{
		{update: `{"sessionUpdate":"notice","severity":"info","title":"Indexed 120 files"}`,
			severity: "info", title: "Indexed 120 files"},
		{update: `{"sessionUpdate":"notice","severity":"warning","title":"MCP server gopls is unavailable","description":"Code navigation is limited."}`,
			severity: "warning", title: "MCP server gopls is unavailable", description: "Code navigation is limited."},
		{update: `{"sessionUpdate":"notice","severity":"error","title":"Rate limited","_meta":{"x":1}}`,
			severity: "error", title: "Rate limited", declines: []string{"notice._meta"}},
		// ACP's severities are an open string type: an unknown one is passed
		// through as sent, and the phone draws it as info.
		{update: `{"sessionUpdate":"notice","severity":"critical","title":"Future severity"}`,
			severity: "critical", title: "Future severity"},
	} {
		s, h := newUpdateSession()
		deliver(t, s, tc.update)
		var got []event.Event
		for len(s.events) > 0 {
			if ev := <-s.events; ev.Type == event.TypeAgentNotice {
				got = append(got, ev)
			}
		}
		if len(got) != 1 {
			t.Fatalf("%s: agent_notice events = %d, want 1", tc.title, len(got))
		}
		ev := got[0]
		if ev.NoticeSeverity != tc.severity || ev.NoticeTitle != tc.title || ev.NoticeDescription != tc.description {
			t.Errorf("%s: got %q/%q/%q, want %q/%q/%q", tc.title,
				ev.NoticeSeverity, ev.NoticeTitle, ev.NoticeDescription, tc.severity, tc.title, tc.description)
		}
		declined, silent := h.declined()
		if silent != 0 || len(declined) != len(tc.declines) || (len(declined) == 1 && declined[0] != tc.declines[0]) {
			t.Errorf("%s: declined %v (silent %d), want %v", tc.title, declined, silent, tc.declines)
		}
	}
}

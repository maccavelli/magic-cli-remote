package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// Golden SSE fixtures under testdata/sse exercise DecodeFrame + HandleEvent
// shapes captured/normalized for OpenCode 1.18.x (MADR 0020 Sprint 4 / PR8).

func loadSSEFixture(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join("testdata", "sse", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestFixtureDecodeGlobalEventSessionCreated(t *testing.T) {
	d := &httpDialect{}
	data := loadSSEFixture(t, "global_event_session_created.json")
	typ, props, sid, ok := d.DecodeFrame(data)
	if !ok {
		t.Fatal("DecodeFrame ok=false")
	}
	if typ != "session.created" {
		t.Fatalf("type=%q", typ)
	}
	if sid != "ses_child1" {
		t.Fatalf("sid=%q want ses_child1 (info.id)", sid)
	}
	if parentIDOf(props) != "ses_parent1" {
		t.Fatalf("parentID=%q", parentIDOf(props))
	}
}

func TestFixtureDecodeBareSessionIdle(t *testing.T) {
	d := &httpDialect{}
	typ, props, sid, ok := d.DecodeFrame(loadSSEFixture(t, "bare_session_idle.json"))
	if !ok || typ != "session.idle" {
		t.Fatalf("typ=%q ok=%v", typ, ok)
	}
	if sid != "ses_parent1" {
		t.Fatalf("sid=%q", sid)
	}
	var probe struct {
		SessionID string `json:"sessionID"`
	}
	if err := json.Unmarshal(props, &probe); err != nil || probe.SessionID != "ses_parent1" {
		t.Fatalf("props=%s err=%v", props, err)
	}
}

func TestFixtureTodoUpdatedMapsPlan(t *testing.T) {
	h := newTreeHost()
	d := &httpDialect{}
	s := d.NewSession(h).(*httpSession)

	data := loadSSEFixture(t, "todo_updated.json")
	typ, props, _, ok := d.DecodeFrame(data)
	if !ok || typ != "todo.updated" {
		t.Fatalf("typ=%q ok=%v", typ, ok)
	}
	s.HandleEvent(typ, props)

	var plan *event.Event
	for _, ev := range h.events {
		if ev.Type == event.TypePlan {
			e := ev
			plan = &e
			break
		}
	}
	if plan == nil {
		t.Fatalf("no plan event; events=%v", h.eventTypes())
	}
	if len(plan.Entries) != 3 {
		t.Fatalf("entries=%d want 3: %+v", len(plan.Entries), plan.Entries)
	}
	// cancelled → pending + "(cancelled) " prefix (Q4)
	found := false
	for _, e := range plan.Entries {
		if e.Status == event.PlanStatusPending && strings.HasPrefix(e.Content, "(cancelled) ") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("cancelled todo mapping missing: %+v", plan.Entries)
	}
}

func TestFixturePermissionUpdatedChild(t *testing.T) {
	h := newTreeHost()
	d := &httpDialect{}
	s := d.NewSession(h).(*httpSession)

	data := loadSSEFixture(t, "permission_updated_child.json")
	typ, props, sid, ok := d.DecodeFrame(data)
	if !ok || typ != "permission.updated" {
		t.Fatalf("typ=%q ok=%v", typ, ok)
	}
	if sid != "ses_child1" {
		t.Fatalf("sid=%q want child", sid)
	}
	// EventAgentSessionID is set by transport before HandleEvent; set via host.
	// treeHost embeds captureHost which returns "" for EventAgentSessionID.
	// Permission handler uses properties.sessionID + origin tracking.
	s.HandleEvent(typ, props)

	var sawPerm bool
	for _, ev := range h.events {
		if ev.Type == event.TypePermission && ev.PermissionID == "perm_child_1" {
			sawPerm = true
			if ev.ToolName == "" && ev.Text == "" {
				t.Fatal("dual-shape permission must populate tool/text")
			}
		}
	}
	if !sawPerm {
		t.Fatalf("no permission event; types=%v", h.eventTypes())
	}
}

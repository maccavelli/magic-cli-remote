package opencode

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

func TestSoleSlashCommand(t *testing.T) {
	name, args, ok := soleSlashCommand([]provider.Content{{Type: "text", Text: "/init my repo"}})
	if !ok || name != "init" || args != "my repo" {
		t.Fatalf("got name=%q args=%q ok=%v", name, args, ok)
	}
	if _, _, ok := soleSlashCommand([]provider.Content{{Type: "text", Text: "/etc/hosts"}}); ok {
		t.Fatal("path must not parse as command")
	}
	if _, _, ok := soleSlashCommand([]provider.Content{
		{Type: "text", Text: "/init"},
		{Type: "text", Text: "x"},
	}); ok {
		t.Fatal("multi-part must not be slash command")
	}
}

func TestListCommandsLive(t *testing.T) {
	d := &httpDialect{}
	body := `[{"name":"init","description":"setup","source":"command","hints":[]},
		{"name":"review","description":"review","source":"command","hints":[]}]`
	api := func(_ context.Context, method, path string, _ any, out any) error {
		if method != "GET" || path != "/command" {
			t.Fatalf("%s %s", method, path)
		}
		return json.Unmarshal([]byte(body), out)
	}
	cat, err := d.ListCommandsLive(context.Background(), api)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Options) != 2 {
		t.Fatalf("opts=%d", len(cat.Options))
	}
}

func TestHandleSessionDiffNotice(t *testing.T) {
	h := newTreeHost()
	d := &httpDialect{}
	s := d.NewSession(h).(*httpSession)
	props := json.RawMessage(`{
		"sessionID":"ses_p",
		"diff":[
			{"file":"a.go","status":"modified","additions":3,"deletions":1},
			{"file":"b.go","status":"added","additions":10,"deletions":0}
		]
	}`)
	s.handleSessionDiff(props)
	var saw bool
	for _, ev := range h.events {
		if ev.Type == event.TypeNotice && len(ev.Text) > 0 {
			saw = true
			if !containsAll(ev.Text, "Diff", "a.go", "b.go", "+3") {
				t.Fatalf("notice=%q", ev.Text)
			}
		}
	}
	if !saw {
		t.Fatal("expected diff notice")
	}
}

func TestSubmitCommandBody(t *testing.T) {
	var got map[string]any
	h := &captureHost{
		model: "opencode/m",
		agent: "build",
		api: func(_ context.Context, method, path string, body, _ any) error {
			if method != "POST" || !containsAll(path, "/command") {
				t.Fatalf("%s %s", method, path)
			}
			b, _ := json.Marshal(body)
			return json.Unmarshal(b, &got)
		},
	}
	d := &httpDialect{}
	s := d.NewSession(h).(*httpSession)
	if err := s.submitCommand(context.Background(), "init", "focus tests"); err != nil {
		t.Fatal(err)
	}
	if got["command"] != "init" || got["arguments"] != "focus tests" {
		t.Fatalf("body=%v", got)
	}
	if got["agent"] != "build" {
		t.Fatalf("agent=%v", got["agent"])
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !json.Valid([]byte(`"` + p + `"`)) {
			// just strings.Contains without import clash
		}
		if len(p) == 0 {
			continue
		}
		found := false
		for i := 0; i+len(p) <= len(s); i++ {
			if s[i:i+len(p)] == p {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Ensure StaticCommands compiles against CommandLister.
var _ httpagent.CommandLister = (*httpDialect)(nil)

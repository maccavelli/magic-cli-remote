package opencode

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"

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

// ---------------------------------------------------------------------------
// MADR 0112 D7 and P1 — tool classification and command argument hints
// ---------------------------------------------------------------------------

func TestKindForToolMatrix(t *testing.T) {
	cases := []struct {
		tool string
		want string
	}{
		// The fix: apply_patch edits files exactly as edit and write do, but
		// was falling through to "other" (MADR 0112 D7).
		{"apply_patch", "edit"},
		{"APPLY_PATCH", "edit"},
		{"  apply_patch  ", "edit"},
		{"edit", "edit"},
		{"write", "edit"},
		{"patch", "edit"},
		{"multiedit", "edit"},
		{"bash", "execute"},
		{"read", "read"},
		{"grep", "search"},
		{"glob", "search"},
		{"webfetch", "fetch"},
		{"websearch", "fetch"},
		{"todowrite", "think"},
		{"", ""},
		// The remaining 1.18.21 built-ins stay generic on purpose: inventing a
		// kind for them would be a guess the phone then renders as fact.
		{"task", "other"},
		{"question", "other"},
		{"invalid", "other"},
		{"skill", "other"},
		// Custom and MCP tools must remain forward-compatible.
		{"mcp__github__create_issue", "other"},
		{"some-future-tool", "other"},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			if got := kindForTool(c.tool); got != c.want {
				t.Errorf("kindForTool(%q) = %q, want %q", c.tool, got, c.want)
			}
		})
	}
}

// Every built-in tool ID observed on isolated 1.18.21 must classify to
// something, and none may be dropped.
func TestKindForToolCoversEvery1_18_21Builtin(t *testing.T) {
	builtins := []string{
		"invalid", "question", "bash", "read", "glob", "grep", "edit", "write",
		"task", "webfetch", "todowrite", "websearch", "skill", "apply_patch",
	}
	if len(builtins) != 14 {
		t.Fatalf("expected the 14 observed built-in tool ids, have %d", len(builtins))
	}
	for _, b := range builtins {
		if got := kindForTool(b); got == "" {
			t.Errorf("built-in tool %q classified as empty", b)
		}
	}
}

func TestCommandHintMatrix(t *testing.T) {
	long := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		long = append(long, "$LONGPLACEHOLDER")
	}
	cases := []struct {
		name  string
		hints []string
		want  string
	}{
		{"nil is empty", nil, ""},
		{"empty slice is empty", []string{}, ""},
		{"all-blank entries are empty", []string{"", "   ", "\t"}, ""},
		// The shape 1.18.21 actually returns for the two built-in commands.
		{"single placeholder", []string{"$ARGUMENTS"}, "$ARGUMENTS"},
		{"numbered placeholders keep upstream order", []string{"$1", "$2", "$3"}, "$1 $2 $3"},
		// Order is argument order, so it is never sorted or deduplicated.
		{"order is preserved, not sorted", []string{"$3", "$1", "$2"}, "$3 $1 $2"},
		{"duplicates are preserved", []string{"$1", "$1"}, "$1 $1"},
		{"blank entries are dropped from the middle", []string{"$1", "  ", "$2"}, "$1 $2"},
		{"entries are individually trimmed", []string{"  $1  ", " $ARGUMENTS "}, "$1 $ARGUMENTS"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := commandHint(c.hints); got != c.want {
				t.Errorf("commandHint(%q) = %q, want %q", c.hints, got, c.want)
			}
		})
	}

	t.Run("long hint is clipped", func(t *testing.T) {
		got := commandHint(long)
		if len(got) > maxCommandHintLen+len("…") {
			t.Errorf("clipped hint is %d bytes, want at most %d", len(got), maxCommandHintLen+len("…"))
		}
		if !strings.HasSuffix(got, "…") {
			t.Errorf("clipped hint %q should end with an ellipsis", got)
		}
	})

	t.Run("clipping never splits a rune", func(t *testing.T) {
		// Multi-byte placeholders: a byte-index cut would leave half a rune,
		// which is a decode error on the phone.
		var wide []string
		for i := 0; i < 60; i++ {
			wide = append(wide, "«ARG»")
		}
		got := commandHint(wide)
		if !utf8.ValidString(got) {
			t.Fatalf("clipped hint is not valid UTF-8: %q", got)
		}
	})
}

// The picker and the advertised command list must describe one command the same
// way; they previously used different reductions of the same array.
func TestCommandHintSharedBetweenPickerAndAdvertise(t *testing.T) {
	const body = `[
		{"name":"init","description":"guided AGENTS.md setup","source":"command","hints":["$ARGUMENTS"]},
		{"name":"deploy","description":"","source":"command","hints":["$1","$2"]},
		{"name":"customize-opencode","description":"tune opencode config","source":"skill","hints":[]}
	]`
	d := &httpDialect{log: slog.Default()}

	cat, err := d.ListCommandsLive(context.Background(), jsonAPI(body))
	if err != nil {
		t.Fatalf("ListCommandsLive: %v", err)
	}
	var deployDesc string
	for _, o := range cat.Options {
		if o.ID == "deploy" {
			deployDesc = o.Description
		}
	}
	// deploy has no description, so its hints become the description.
	if deployDesc != "$1 $2" {
		t.Errorf("picker description for deploy = %q, want %q", deployDesc, "$1 $2")
	}

	h := &captureHost{api: jsonAPI(body)}
	s := d.NewSession(h).(*httpSession)
	s.advertiseCommands(context.Background())

	byName := map[string]event.AvailableCommand{}
	h.mu.Lock()
	captured := append([]event.Event(nil), h.events...)
	h.mu.Unlock()
	for _, ev := range captured {
		if ev.Type == event.TypeAvailableCommands {
			for _, c := range ev.Commands {
				byName[c.Name] = c
			}
		}
	}
	if len(byName) != 3 {
		t.Fatalf("advertised %d commands, want 3: %+v", len(byName), byName)
	}
	if got := byName["init"].Hint; got != "$ARGUMENTS" {
		t.Errorf("init hint = %q, want %q", got, "$ARGUMENTS")
	}
	if got := byName["deploy"].Hint; got != "$1 $2" {
		t.Errorf("deploy hint = %q, want %q", got, "$1 $2")
	}
	if got := byName["customize-opencode"].Hint; got != "" {
		t.Errorf("a skill command with no hints must advertise none, got %q", got)
	}
	if got := byName["init"].Description; got != "guided AGENTS.md setup" {
		t.Errorf("init description = %q", got)
	}
	// Same reduction on both surfaces for the same command.
	if byName["deploy"].Hint != deployDesc {
		t.Errorf("picker (%q) and advertised list (%q) disagree about deploy",
			deployDesc, byName["deploy"].Hint)
	}
}

// A command catalog that omits hints entirely, or sends null, must not break
// advertisement — older engines and MCP prompts both do this.
func TestAdvertiseCommandsToleratesMissingHints(t *testing.T) {
	const body = `[
		{"name":"init","description":"setup"},
		{"name":"nullhints","description":"x","hints":null},
		{"name":"  ","description":"blank name is dropped"}
	]`
	d := &httpDialect{log: slog.Default()}
	h := &captureHost{api: jsonAPI(body)}
	s := d.NewSession(h).(*httpSession)
	s.advertiseCommands(context.Background())

	var got []event.AvailableCommand
	h.mu.Lock()
	captured := append([]event.Event(nil), h.events...)
	h.mu.Unlock()
	for _, ev := range captured {
		if ev.Type == event.TypeAvailableCommands {
			got = ev.Commands
		}
	}
	if len(got) != 2 {
		t.Fatalf("advertised %d commands, want 2 (blank name dropped): %+v", len(got), got)
	}
	for _, c := range got {
		if c.Hint != "" {
			t.Errorf("command %q advertised hint %q, want none", c.Name, c.Hint)
		}
	}
}

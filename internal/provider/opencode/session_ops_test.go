package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// apiCall is one recorded engine request.
type apiCall struct {
	method string
	path   string
	body   map[string]any
}

// recorder is a captureHost whose API records every call and optionally
// answers with canned JSON keyed by path fragment (first match wins).
type recorder struct {
	captureHost
	agentID string
	calls   []apiCall
	routes  []route
}

func (r *recorder) AgentSessionID() string { return r.agentID }

func newRecorder(routes ...route) *recorder {
	r := &recorder{agentID: "ses_test", routes: routes}
	r.api = func(_ context.Context, method, path string, body, out any) error {
		var m map[string]any
		if body != nil {
			b, _ := json.Marshal(body)
			_ = json.Unmarshal(b, &m)
		}
		r.calls = append(r.calls, apiCall{method, path, m})
		if out == nil {
			return nil
		}
		for _, rt := range r.routes {
			if strings.Contains(path, rt.frag) {
				return json.Unmarshal([]byte(rt.body), out)
			}
		}
		return nil
	}
	return r
}

func (r *recorder) paths() []string {
	out := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		out = append(out, c.method+" "+strings.Split(c.path, "?")[0])
	}
	return out
}

func (r *recorder) find(t *testing.T, method, frag string) apiCall {
	t.Helper()
	for _, c := range r.calls {
		if c.method == method && strings.Contains(c.path, frag) {
			return c
		}
	}
	t.Fatalf("no %s call containing %q; calls=%v", method, frag, r.paths())
	return apiCall{}
}

func newOpsSession(h *recorder) *httpSession {
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	return d.NewSession(h).(*httpSession)
}

// Every request must carry ?directory=, or the engine scopes it to the wrong
// project and silently operates on someone else's session store.
func TestSessionOpsCarryDirectoryScope(t *testing.T) {
	h := newRecorder(route{"/fork", `{"id":"ses_forked"}`}, route{"/diff", `[]`})
	s := newOpsSession(h)
	ctx := context.Background()

	if _, err := s.Fork(ctx, "msg_1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Revert(ctx, "msg_1", "prt_1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Unrevert(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Diff(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx); err != nil {
		t.Fatal(err)
	}

	if len(h.calls) == 0 {
		t.Fatal("no calls recorded")
	}
	for _, c := range h.calls {
		if !strings.Contains(c.path, "directory=") {
			t.Errorf("%s %s missing directory scope", c.method, c.path)
		}
	}
}

func TestForkPostsMessageIDAndReturnsNewSession(t *testing.T) {
	h := newRecorder(route{"/fork", `{"id":"ses_forked"}`})
	s := newOpsSession(h)

	id, err := s.Fork(context.Background(), "msg_1")
	if err != nil {
		t.Fatal(err)
	}
	if id != "ses_forked" {
		t.Fatalf("fork id=%q want ses_forked", id)
	}
	c := h.find(t, "POST", "/fork")
	if c.body["messageID"] != "msg_1" {
		t.Fatalf("fork body=%+v", c.body)
	}
	if !strings.Contains(c.path, "/session/ses_test/fork") {
		t.Fatalf("fork path=%q", c.path)
	}
}

// An empty response id is an engine bug, not a usable session — surface it.
func TestForkRejectsEmptyID(t *testing.T) {
	h := newRecorder(route{"/fork", `{}`})
	s := newOpsSession(h)
	if _, err := s.Fork(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty fork id")
	}
}

func TestRevertRequiresMessageID(t *testing.T) {
	h := newRecorder()
	s := newOpsSession(h)
	if err := s.Revert(context.Background(), "", "prt_1"); err == nil {
		t.Fatal("expected error when messageID is empty")
	}
	if len(h.calls) != 0 {
		t.Fatalf("must not call the engine: %v", h.paths())
	}
}

func TestRevertOmitsEmptyPartID(t *testing.T) {
	h := newRecorder()
	s := newOpsSession(h)
	if err := s.Revert(context.Background(), "msg_1", ""); err != nil {
		t.Fatal(err)
	}
	c := h.find(t, "POST", "/revert")
	if _, ok := c.body["partID"]; ok {
		t.Fatalf("empty partID must be omitted: %+v", c.body)
	}
}

// dir() already appends "?directory=", so a messageID filter must join with &.
func TestDiffAppendsMessageIDWithAmpersand(t *testing.T) {
	h := newRecorder(route{"/diff", `[{"file":"a.go","status":"modified","additions":2,"deletions":1}]`})
	s := newOpsSession(h)

	out, err := s.Diff(context.Background(), "msg_9")
	if err != nil {
		t.Fatal(err)
	}
	c := h.find(t, "GET", "/diff")
	if !strings.Contains(c.path, "&messageID=msg_9") {
		t.Fatalf("diff path=%q want &messageID=", c.path)
	}
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "+2") {
		t.Fatalf("diff summary=%q", out)
	}
}

func TestDiffEmptyIsNotAnError(t *testing.T) {
	h := newRecorder(route{"/diff", `[]`})
	s := newOpsSession(h)
	out, err := s.Diff(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "No file changes" {
		t.Fatalf("diff=%q", out)
	}
}

// Abort must cancel the parent AND cascade to every descendant, or a subagent
// keeps burning tokens after the user pressed Stop (MADR 0020 §6.9).
func TestAbortCascadesToChildren(t *testing.T) {
	h := newRecorder()
	h.api = func(_ context.Context, method, path string, body, out any) error {
		var m map[string]any
		if body != nil {
			b, _ := json.Marshal(body)
			_ = json.Unmarshal(b, &m)
		}
		h.calls = append(h.calls, apiCall{method, path, m})
		if out == nil || !strings.Contains(path, "/children") {
			return nil
		}
		switch {
		case strings.Contains(path, "/session/ses_test/children"):
			return json.Unmarshal([]byte(`[{"id":"c1"}]`), out)
		case strings.Contains(path, "/session/c1/children"):
			return json.Unmarshal([]byte(`[{"id":"g1"}]`), out)
		}
		return json.Unmarshal([]byte(`[]`), out)
	}
	s := newOpsSession(h)

	if err := s.Abort(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/session/ses_test/abort", "/session/c1/abort", "/session/g1/abort"} {
		found := false
		for _, c := range h.calls {
			if c.method == "POST" && strings.Contains(c.path, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing abort for %s; calls=%v", want, h.paths())
		}
	}
}

// A failed child abort must not mask the parent abort result, and vice versa:
// the parent's error is what the caller acts on.
func TestAbortReturnsParentError(t *testing.T) {
	h := newRecorder()
	h.api = func(_ context.Context, method, path string, _, out any) error {
		h.calls = append(h.calls, apiCall{method, path, nil})
		if strings.Contains(path, "/children") {
			if out != nil {
				return json.Unmarshal([]byte(`[]`), out)
			}
			return nil
		}
		return fmt.Errorf("abort refused")
	}
	s := newOpsSession(h)
	if err := s.Abort(context.Background()); err == nil {
		t.Fatal("expected parent abort error")
	}
}

func TestAbortNoopWithoutAgentSession(t *testing.T) {
	h := newRecorder()
	h.agentID = ""
	s := newOpsSession(h)
	if err := s.Abort(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.calls) != 0 {
		t.Fatalf("must not call engine with no session: %v", h.paths())
	}
}

func TestDeleteNoopWithoutAgentSession(t *testing.T) {
	h := newRecorder()
	h.agentID = ""
	s := newOpsSession(h)
	if err := s.Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.calls) != 0 {
		t.Fatalf("must not DELETE with no session: %v", h.paths())
	}
}

// Replay rebuilds the history ring after a resume: user text, assistant text,
// reasoning and tool cards each map to their own event type, and blank text
// parts are skipped so the transcript is not padded with empty bubbles.
func TestReplayMapsMessageLog(t *testing.T) {
	const log = `[
		{"info":{"role":"user"},"parts":[{"type":"text","text":"hi"}]},
		{"info":{"role":"assistant"},"parts":[
			{"type":"reasoning","text":"thinking"},
			{"type":"text","text":"hello"},
			{"type":"text","text":"   "},
			{"type":"tool","tool":"bash","callID":"call1","state":{"status":"completed","title":"ls"}},
			{"type":"step-start"}
		]}
	]`
	h := newRecorder(route{"/message", log})
	s := newOpsSession(h)
	s.Replay(context.Background())

	type got struct {
		typ  event.Type
		text string
	}
	var seen []got
	for _, ev := range h.events {
		seen = append(seen, got{ev.Type, ev.Text})
	}
	want := []got{
		{event.TypeUserMessage, "hi"},
		{event.TypeThoughtChunk, "thinking"},
		{event.TypeAssistantChunk, "hello"},
		{event.TypeToolCall, ""},
	}
	if len(seen) != len(want) {
		t.Fatalf("replay events=%+v want %+v", seen, want)
	}
	for i := range want {
		if seen[i].typ != want[i].typ || seen[i].text != want[i].text {
			t.Fatalf("replay events=%+v want %+v", seen, want)
		}
	}
	tool := h.events[3]
	if tool.ToolName != "ls" || tool.ToolKind != "execute" || tool.Status != "completed" {
		t.Fatalf("tool card=%+v", tool)
	}
}

// A resume whose message log is unreadable must degrade to an empty history,
// not fail the resume.
func TestReplaySurvivesFetchError(t *testing.T) {
	h := newRecorder()
	h.api = func(context.Context, string, string, any, any) error {
		return fmt.Errorf("engine down")
	}
	s := newOpsSession(h)
	s.Replay(context.Background())
	if len(h.events) != 0 {
		t.Fatalf("expected no events, got %+v", h.events)
	}
}

func TestResumeVerifiesAndAdvertisesCommands(t *testing.T) {
	h := newRecorder(
		route{"/command", `[{"name":"init","description":"setup"}]`},
		route{"/session/", `{"id":"ses_test"}`},
	)
	s := newOpsSession(h)

	id, err := s.Resume(context.Background(), "ses_test")
	if err != nil {
		t.Fatal(err)
	}
	if id != "ses_test" {
		t.Fatalf("resume id=%q", id)
	}
	var advertised bool
	for _, ev := range h.events {
		if ev.Type == event.TypeAvailableCommands {
			advertised = true
			if len(ev.Commands) != 1 || ev.Commands[0].Name != "init" {
				t.Fatalf("commands=%+v", ev.Commands)
			}
		}
	}
	if !advertised {
		t.Fatal("resume must re-advertise slash commands for autocomplete")
	}
}

// With the catalog down, /init must still route as an agent command rather than
// being sent to the model as literal text.
func TestAdvertiseCommandsFallsBackWhenCatalogFails(t *testing.T) {
	h := newRecorder()
	h.api = func(context.Context, string, string, any, any) error {
		return fmt.Errorf("catalog down")
	}
	s := newOpsSession(h)
	s.advertiseCommands(context.Background())

	for _, ev := range h.events {
		if ev.Type == event.TypeAvailableCommands && len(ev.Commands) > 0 {
			return
		}
	}
	t.Fatalf("expected static command fallback; events=%+v", h.events)
}

func TestCommandExecutedNotice(t *testing.T) {
	h := newRecorder()
	s := newOpsSession(h)
	s.HandleEvent("command.executed", json.RawMessage(`{"name":"init","arguments":"my repo"}`))
	if len(h.events) != 1 || h.events[0].Type != event.TypeNotice ||
		!strings.Contains(h.events[0].Text, "/init") {
		t.Fatalf("events=%+v", h.events)
	}
	// A nameless frame is not a command.
	h.events = nil
	s.HandleEvent("command.executed", json.RawMessage(`{"arguments":"x"}`))
	if len(h.events) != 0 {
		t.Fatalf("events=%+v", h.events)
	}
}

// ToolKind is the cross-transport vocabulary clients group actions by
// ("Ran N commands", "Edited N files"); a silent mismatch degrades every UI.
func TestKindForTool(t *testing.T) {
	cases := map[string]string{
		"bash": "execute", "Bash": "execute",
		"edit": "edit", "write": "edit", "patch": "edit", "multiedit": "edit",
		"read": "read",
		"grep": "search", "glob": "search", "list": "search", "ls": "search",
		"webfetch": "fetch", "websearch": "fetch",
		"todowrite": "think", "todoread": "think",
		"":     "",
		"task": "other",
	}
	for in, want := range cases {
		if got := kindForTool(in); got != want {
			t.Errorf("kindForTool(%q)=%q want %q", in, got, want)
		}
	}
}

func TestMapToolStatus(t *testing.T) {
	cases := map[string]string{
		"completed": "completed",
		"error":     "failed",
		"running":   "running",
		"pending":   "pending",
		"weird":     "weird",
	}
	for in, want := range cases {
		if got := mapToolStatus(in); got != want {
			t.Errorf("mapToolStatus(%q)=%q want %q", in, got, want)
		}
	}
}

// The engine contract: argv, health path and event path are what the transport
// spawns and polls. Changing one silently breaks every OpenCode session.
func TestEngineContract(t *testing.T) {
	d := &httpDialect{log: slog.Default()}
	args := d.ServeArgs(4321)
	want := []string{"serve", "--hostname", "127.0.0.1", "--port", "4321"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("ServeArgs=%v want %v", args, want)
	}
	if d.HealthPath() != "/global/health" {
		t.Fatalf("HealthPath=%q", d.HealthPath())
	}
	if d.EventsPath() != "/global/event" {
		t.Fatalf("EventsPath=%q", d.EventsPath())
	}
	if d.DefaultBin() != "opencode" {
		t.Fatalf("DefaultBin=%q", d.DefaultBin())
	}
	if d.ID() != provider.IDOpencode {
		t.Fatalf("ID=%q", d.ID())
	}
}

package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent/acptest"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode/control"
)

// surfaceEngine is a control plane that answers by route, recording every
// request it was sent.
type surfaceEngine struct {
	mu     sync.Mutex
	routes map[string]func(control.Request) control.Response
	sent   []control.Request
}

func (e *surfaceEngine) Do(_ context.Context, req control.Request) (control.Response, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sent = append(e.sent, req)
	if h, ok := e.routes[req.Path]; ok {
		return h(req), nil
	}
	return control.Response{Status: http.StatusNotFound, Body: []byte(`{"_tag":"NotFound"}`)}, nil
}

func (e *surfaceEngine) paths() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, r := range e.sent {
		out = append(out, r.Path)
	}
	return out
}

func (e *surfaceEngine) requests() []control.Request {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]control.Request(nil), e.sent...)
}

// jsonAnswer is a 200 with v encoded.
func jsonAnswer(v any) func(control.Request) control.Response {
	b, _ := json.Marshal(v)
	return func(control.Request) control.Response {
		return control.Response{Status: http.StatusOK, Body: b, ContentType: "application/json"}
	}
}

// surfaceHost is a session as the surfaces use it: its directory, its agent
// id and its log. Any other Host method panics, which would show a surface
// reaching for more than it should.
type surfaceHost struct {
	acpagent.Host
	cwd, sid string
}

func (h surfaceHost) CWD() string            { return h.cwd }
func (h surfaceHost) AgentSessionID() string { return h.sid }
func (h surfaceHost) Log() *slog.Logger      { return slog.New(slog.DiscardHandler) }

func newSurfaces(t *testing.T, routes map[string]func(control.Request) control.Response) (*acpSurfaces, *surfaceEngine, surfaceHost) {
	t.Helper()
	e := &surfaceEngine{routes: routes}
	return &acpSurfaces{plane: e}, e, surfaceHost{cwd: t.TempDir(), sid: "ses_1"}
}

// TestACPWorkspaceRefusesPathsOutsideTheRoot: fs.list lists the directory
// above the project when given an absolute path or "..", so those, and a path
// through a symlink, are refused before any request (PLAN P6d Deviation 1).
func TestACPWorkspaceRefusesPathsOutsideTheRoot(t *testing.T) {
	s, e, h := newSurfaces(t, nil)
	if err := os.Symlink(t.TempDir(), filepath.Join(h.cwd, "link")); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, p := range []string{"/etc", "..", "src/../../up", "link", "link/file.txt"} {
		if _, err := s.listWorkspace(ctx, h, p); err == nil {
			t.Errorf("list %q: no error", p)
		}
		if _, err := s.readWorkspace(ctx, h, p); err == nil {
			t.Errorf("read %q: no error", p)
		}
	}
	if sent := e.paths(); len(sent) != 0 {
		t.Errorf("sent %v; want nothing sent for a refused path", sent)
	}
}

// TestACPWorkspaceListKeepsOnlyPathsInside: a listing is asked for in the
// session's directory, and an entry that leaves it is dropped.
func TestACPWorkspaceListKeepsOnlyPathsInside(t *testing.T) {
	s, e, h := newSurfaces(t, map[string]func(control.Request) control.Response{
		"/api/fs/list": jsonAnswer(map[string]any{"location": map[string]any{}, "data": []map[string]string{
			{"path": "README.md", "type": "file"},
			{"path": "src/", "type": "directory"},
			{"path": "../opencode.db", "type": "file"},
			{"path": "/etc/passwd", "type": "file"},
		}}),
	})
	got, err := s.listWorkspace(context.Background(), h, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []provider.WorkspaceEntry{{Name: "src", Path: "src", Dir: true}, {Name: "README.md", Path: "README.md"}}
	if !slices.Equal(got, want) {
		t.Errorf("entries %+v, want %+v", got, want)
	}
	req := e.requests()[0]
	if req.Directory != h.cwd || req.Query.Get("path") != "." {
		t.Errorf("request %+v, want path . in the session's directory", req)
	}
}

// TestACPWorkspaceReadIsRawAndBounded: fs.read is a raw request bounded at the
// 1.x file cap; a longer file is refused whole, and bytes that are not text
// are refused.
func TestACPWorkspaceReadIsRawAndBounded(t *testing.T) {
	answer := control.Response{Status: http.StatusOK, Body: []byte("hello\n"), ContentType: "text/markdown"}
	s, e, h := newSurfaces(t, map[string]func(control.Request) control.Response{
		"/api/fs/read/docs/my%20notes.md": func(control.Request) control.Response { return answer },
	})
	ctx := context.Background()
	got, err := s.readWorkspace(ctx, h, "docs/my notes.md")
	if err != nil || got.Text != "hello\n" || got.Path != "docs/my notes.md" || got.Bytes != 6 {
		t.Fatalf("read = %+v, %v", got, err)
	}
	req := e.requests()[0]
	if !req.Raw || req.MaxBytes != maxWorkspaceFileBytes || req.Directory != h.cwd {
		t.Errorf("request %+v, want raw, bounded at %d, in the session's directory", req, maxWorkspaceFileBytes)
	}
	for name, a := range map[string]control.Response{
		"result_too_large": {Status: http.StatusOK, Body: []byte("x"), Truncated: true},
		"binary_content":   {Status: http.StatusOK, Body: []byte("a\x00b")},
		"binary_content ":  {Status: http.StatusOK, Body: []byte{0xff, 0xfe}},
	} {
		answer = a
		if _, err := s.readWorkspace(ctx, h, "docs/my notes.md"); err == nil || !strings.Contains(err.Error(), strings.TrimSpace(name)) {
			t.Errorf("%+v: err %v, want %s", a, err, strings.TrimSpace(name))
		}
	}
}

// TestACPWorkspaceErrorsCarryNoEngineText: a missing file is invalid_path, and
// no engine message, which can name a host path, reaches the error.
func TestACPWorkspaceErrorsCarryNoEngineText(t *testing.T) {
	s, _, h := newSurfaces(t, map[string]func(control.Request) control.Response{
		"/api/fs/read/gone.txt": func(control.Request) control.Response {
			return control.Response{Status: http.StatusNotFound,
				Body: []byte(`{"_tag":"FileNotFoundError","message":"ENOENT /Users/secret/project/gone.txt"}`)}
		},
		"/api/fs/list": func(control.Request) control.Response {
			return control.Response{Status: http.StatusInternalServerError,
				Body: []byte(`{"_tag":"UnknownError","message":"failed at /Users/secret/project"}`)}
		},
	})
	ctx := context.Background()
	_, err := s.readWorkspace(ctx, h, "gone.txt")
	if err == nil || !errors.Is(err, errWorkspaceInvalidPath) || strings.Contains(err.Error(), "/Users/secret") {
		t.Errorf("missing file: %v, want invalid_path and no engine message", err)
	}
	_, err = s.listWorkspace(ctx, h, "")
	if err == nil || strings.Contains(err.Error(), "/Users/secret") || !strings.Contains(err.Error(), "UnknownError") {
		t.Errorf("engine failure: %v, want the tag and no engine message", err)
	}
}

// TestACPTextSearchNamesTheIssue: full-text search answers that v2 has no
// route, naming #34, and sends nothing.
func TestACPTextSearchNamesTheIssue(t *testing.T) {
	s, e, h := newSurfaces(t, nil)
	_, err := s.searchWorkspace(context.Background(), h, provider.WorkspaceSearchText, "TODO")
	if err == nil || !strings.Contains(err.Error(), "#34") {
		t.Errorf("text search: %v, want an answer naming #34", err)
	}
	if sent := e.paths(); len(sent) != 0 {
		t.Errorf("sent %v", sent)
	}
}

// TestACPFileSearch: fs.find's files, inside the root, in order; a full page
// is marked as possibly more.
func TestACPFileSearch(t *testing.T) {
	rows := []map[string]string{{"path": "src/b.go", "type": "file"}, {"path": "src/", "type": "directory"},
		{"path": "a.go", "type": "file"}, {"path": "../x.go", "type": "file"}}
	s, e, h := newSurfaces(t, map[string]func(control.Request) control.Response{
		"/api/fs/find": func(control.Request) control.Response {
			b, _ := json.Marshal(map[string]any{"data": rows})
			return control.Response{Status: http.StatusOK, Body: b}
		},
	})
	ctx := context.Background()
	got, err := s.searchWorkspace(ctx, h, provider.WorkspaceSearchFile, " go ")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, m := range got.Matches {
		paths = append(paths, m.Path)
	}
	if !slices.Equal(paths, []string{"a.go", "src/b.go"}) || got.Truncated || got.Cap != fileSearchLimit {
		t.Errorf("search %+v, want a.go and src/b.go, not truncated", got)
	}
	req := e.requests()[0]
	if req.Query.Get("query") != "go" || req.Query.Get("limit") != fmt.Sprint(fileSearchLimit) || req.Directory != h.cwd {
		t.Errorf("request %+v", req)
	}
	rows = nil
	for i := range fileSearchLimit {
		rows = append(rows, map[string]string{"path": fmt.Sprintf("f%03d.go", i), "type": "file"})
	}
	if got, _ := s.searchWorkspace(ctx, h, provider.WorkspaceSearchFile, "f"); !got.Truncated {
		t.Error("a full page was not marked truncated")
	}
}

// diagnosticRoutes answers the routes diagnostics reads. The skill rows carry
// a path and content, which must not be decoded.
func diagnosticRoutes() map[string]func(control.Request) control.Response {
	return map[string]func(control.Request) control.Response{
		"/api/vcs": jsonAnswer(map[string]any{"data": map[string]any{"provider": "git",
			"branch": map[string]string{"current": "feature", "default": "main"}}}),
		"/api/vcs/status": jsonAnswer(map[string]any{"data": []map[string]any{
			{"file": "/Users/secret/project/a.go", "status": "modified", "additions": 3, "deletions": 1},
			{"file": "b.go", "status": "added", "additions": 5, "deletions": 0},
		}}),
		"/api/integration": jsonAnswer(map[string]any{"data": []any{}}),
		"/api/skill": jsonAnswer(map[string]any{"data": []map[string]string{
			{"id": "report", "name": "Report", "description": "Report a bug", "path": "/Users/secret/s/SKILL.md", "content": "secret body"},
			{"id": "browser", "name": "Browser", "description": "Use the browser", "path": "/Users/secret/b/SKILL.md", "content": "secret body"},
		}}),
	}
}

// TestACPDiagnostics: branch, an aggregate VCS summary, and skills by name and
// description, read in the session's directory, the skills after plugin
// activation. MCP, plugins, LSP and formatters are not read.
func TestACPDiagnostics(t *testing.T) {
	s, e, h := newSurfaces(t, diagnosticRoutes())
	d, err := s.diagnostics(context.Background(), h)
	if err != nil {
		t.Fatal(err)
	}
	if d.Branch != "feature" || d.DefaultBranch != "main" {
		t.Errorf("branch %q, default %q", d.Branch, d.DefaultBranch)
	}
	if d.VCS == nil || *d.VCS != (provider.VCSStatusSummary{Added: 1, Modified: 1, Additions: 8, Deletions: 1}) {
		t.Errorf("vcs %+v", d.VCS)
	}
	want := []provider.SkillInfo{{Name: "Browser", Description: "Use the browser"}, {Name: "Report", Description: "Report a bug"}}
	if !slices.Equal(d.Skills, want) {
		t.Errorf("skills %+v, want %+v", d.Skills, want)
	}
	if len(d.MCP) != 0 || len(d.LSP) != 0 || len(d.Formatters) != 0 {
		t.Errorf("mcp %v, lsp %v, formatters %v; want none from the control plane", d.MCP, d.LSP, d.Formatters)
	}
	if b, _ := json.Marshal(d); strings.Contains(string(b), "secret") {
		t.Errorf("diagnostics carry a path or content: %s", b)
	}
	sent := e.paths()
	if i, j := slices.Index(sent, "/api/integration"), slices.Index(sent, "/api/skill"); i < 0 || j < i {
		t.Errorf("sent %v; want the integration list before the skill list", sent)
	}
	for _, p := range sent {
		if strings.HasPrefix(p, "/api/mcp") || strings.HasPrefix(p, "/api/plugin") || p == "/api/lsp" || p == "/api/formatter" {
			t.Errorf("read %s", p)
		}
	}
	for _, r := range e.requests() {
		if r.Directory != h.cwd {
			t.Errorf("%s in %q, want the session's directory", r.Path, r.Directory)
		}
	}
}

// TestACPDiagnosticsWithoutActivation: when plugins cannot be activated, the
// skills section is left out, unread, and the rest still reports.
func TestACPDiagnosticsWithoutActivation(t *testing.T) {
	routes := diagnosticRoutes()
	routes["/api/integration"] = func(control.Request) control.Response {
		return control.Response{Status: http.StatusServiceUnavailable, Body: []byte(`{"_tag":"ServiceUnavailableError"}`)}
	}
	s, e, h := newSurfaces(t, routes)
	d, err := s.diagnostics(context.Background(), h)
	if err != nil || d.Branch != "feature" || d.Skills != nil {
		t.Errorf("diagnostics %+v (%v), want the branch and no skills", d, err)
	}
	if slices.Contains(e.paths(), "/api/skill") {
		t.Error("the skill list was read before activation")
	}
}

// diffRoutes answers the user message list by order, and the diff.
func diffRoutes(users []string, files int) map[string]func(control.Request) control.Response {
	var rows []map[string]any
	for i := range files {
		rows = append(rows, map[string]any{"file": fmt.Sprintf("f%02d.go", i), "status": "modified",
			"additions": 2, "deletions": 1, "patch": "secret patch"})
	}
	return map[string]func(control.Request) control.Response{
		"/api/session/ses_1/message": func(r control.Request) control.Response {
			var data []map[string]string
			if len(users) > 0 {
				id := users[0]
				if r.Query.Get("order") == "desc" {
					id = users[len(users)-1]
				}
				data = []map[string]string{{"id": id, "type": "user", "text": "a prompt"}}
			}
			b, _ := json.Marshal(map[string]any{"data": data, "cursor": map[string]any{}})
			return control.Response{Status: http.StatusOK, Body: b}
		},
		"/api/session/ses_1/diff": jsonAnswer(map[string]any{"data": rows}),
	}
}

// TestACPDiffIsTheWholeSession: with no message id, the diff runs from the
// first user message to the newest, as /diff means on 1.x.
func TestACPDiffIsTheWholeSession(t *testing.T) {
	s, e, h := newSurfaces(t, diffRoutes([]string{"msg_1", "msg_2", "msg_3"}, 2))
	got, err := s.diff(context.Background(), h, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "Diff · 2 file(s)\n  modified  +2 −1  f00.go\n  modified  +2 −1  f01.go" || got.Truncated {
		t.Errorf("summary %q, truncated %v", got.Summary, got.Truncated)
	}
	var diffQ url.Values
	var orders []string
	for _, r := range e.requests() {
		switch r.Path {
		case "/api/session/ses_1/diff":
			diffQ = r.Query
		case "/api/session/ses_1/message":
			if r.Query.Get("type") != "user" || r.Query.Get("limit") != "1" {
				t.Errorf("message list query %v", r.Query)
			}
			orders = append(orders, r.Query.Get("order"))
		}
	}
	if diffQ.Get("from") != "msg_1" || diffQ.Get("to") != "msg_3" {
		t.Errorf("diff query %v, want from msg_1 to msg_3", diffQ)
	}
	if !slices.Equal(orders, []string{"asc", "desc"}) {
		t.Errorf("message list orders %v", orders)
	}
}

// TestACPDiffBounds: past 20 files the summary says how many more, and the
// result is marked truncated; a session with no prompt has no changes, and
// asks for no diff.
func TestACPDiffBounds(t *testing.T) {
	s, _, h := newSurfaces(t, diffRoutes([]string{"msg_1"}, 23))
	got, err := s.diff(context.Background(), h, "")
	if err != nil || !got.Truncated || !strings.HasSuffix(got.Summary, "…and 3 more") || strings.Contains(got.Summary, "f20.go") {
		t.Errorf("23 files: %q, truncated %v (%v)", got.Summary, got.Truncated, err)
	}
	s, e, h := newSurfaces(t, diffRoutes(nil, 1))
	got, err = s.diff(context.Background(), h, "")
	if err != nil || got.Summary != "No file changes" || slices.Contains(e.paths(), "/api/session/ses_1/diff") {
		t.Errorf("no prompt: %+v (%v), sent %v", got, err, e.paths())
	}
}

// TestACPDiffOfOneMessage: a message id is the turn to diff, and the user
// messages are not read.
func TestACPDiffOfOneMessage(t *testing.T) {
	s, e, h := newSurfaces(t, diffRoutes([]string{"msg_1", "msg_2"}, 1))
	if _, err := s.diff(context.Background(), h, "msg_2"); err != nil {
		t.Fatal(err)
	}
	reqs := e.requests()
	if len(reqs) != 1 || reqs[0].Query.Get("from") != "msg_2" || reqs[0].Query.Has("to") {
		t.Errorf("sent %+v, want one diff from msg_2", reqs)
	}
}

// TestACPProviderOffersTheSurfaces: newACP declares workspace and diff, sets no
// skill refresh (0229), and a session's diagnostics come from the control
// plane in its directory.
func TestACPProviderOffersTheSurfaces(t *testing.T) {
	e := &surfaceEngine{routes: diagnosticRoutes()}
	p := newACP(acpagent.Config{}, nil, e)
	s, _ := acptest.Start(t, p, acptest.Options{Replies: map[string]string{
		"initialize":  `{"protocolVersion":1,"agentInfo":{"name":"OpenCode","version":"2.2.3"},"agentCapabilities":{"loadSession":true}}`,
		"session/new": `{"sessionId":"ses_1"}`,
	}})
	caps := s.Capabilities()
	if !caps.Has(provider.CapWorkspace) || !caps.Has(provider.CapDiff) || caps.Has(provider.CapSkillRefresh) {
		t.Errorf("capabilities %+v, want workspace and diff, and no skill refresh", caps)
	}
	ds, ok := s.(provider.DiagnosticsSession)
	if !ok {
		t.Fatal("the session reports no diagnostics")
	}
	d, err := ds.Diagnostics(context.Background())
	if err != nil || d.Branch != "feature" || len(d.Skills) != 2 {
		t.Errorf("diagnostics %+v (%v), want the control plane's", d, err)
	}
}

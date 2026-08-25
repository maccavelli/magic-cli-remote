package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// workspaceSession builds a dialect session rooted at a real temp directory, so
// symlink checks exercise the actual filesystem rather than a stub.
func workspaceSession(t *testing.T, routes ...route) (*recorder, *httpSession, string) {
	t.Helper()
	root := t.TempDir()
	h := newRecorder(routes...)
	h.cwd = root
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	return h, d.NewSession(h).(*httpSession), root
}

func TestNormalizeWorkspacePath(t *testing.T) {
	nul := "a" + string(rune(0)) + "b"
	for _, tc := range []struct {
		name, in, want string
		err            error
	}{
		{name: "empty is the root", in: "", want: ""},
		{name: "dot is the root", in: ".", want: ""},
		{name: "dot slash is the root", in: "./", want: ""},
		{name: "plain relative", in: "a/b.txt", want: "a/b.txt"},
		{name: "backslashes are normalised", in: `a\b.txt`, want: "a/b.txt"},
		{name: "redundant segments collapse", in: "a/./b/../b.txt", want: "a/b.txt"},
		{name: "inner traversal that stays inside is fine", in: "a/b/../c", want: "a/c"},
		{name: "absolute is refused", in: "/etc/passwd", err: errWorkspaceInvalidPath},
		{name: "windows-style absolute is refused", in: `\etc\passwd`, err: errWorkspaceInvalidPath},
		{name: "traversal is refused", in: "../secrets", err: errWorkspacePathEscape},
		{name: "deep traversal is refused", in: "a/../../secrets", err: errWorkspacePathEscape},
		{name: "bare parent is refused", in: "..", err: errWorkspacePathEscape},
		{name: "NUL is refused", in: nul, err: errWorkspaceInvalidPath},
		{name: "a file URI is refused", in: "file:///etc/passwd", err: errWorkspaceInvalidPath},
		{name: "an https URI is refused", in: "https://example.com/a", err: errWorkspaceInvalidPath},
		{name: "over-long is refused", in: strings.Repeat("a", maxWorkspacePathBytes+1), err: errWorkspaceInvalidPath},
		{name: "invalid utf-8 is refused", in: "a" + string([]byte{0xff}) + "b", err: errWorkspaceInvalidPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeWorkspacePath(tc.in)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err = %v, want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSymlinkComponentsAreRejectedNotResolved is the confinement rule: a
// resolved link is trusted, and what it points at can change afterwards.
func TestSymlinkComponentsAreRejectedNotResolved(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "real", "ok.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "real", "ok.txt"), filepath.Join(root, "inside-link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := checkNoSymlinkComponents(root, "real/ok.txt"); err != nil {
		t.Fatalf("a plain path was rejected: %v", err)
	}
	for _, bad := range []string{"escape", "escape/secret.txt", "inside-link.txt"} {
		if err := checkNoSymlinkComponents(root, bad); !errors.Is(err, errWorkspacePathSymlink) {
			t.Fatalf("checkNoSymlinkComponents(%q) = %v, want a symlink refusal", bad, err)
		}
	}
	// A link that points inside the root is refused too: the rule is about the
	// link, not about where it happens to point today.
	if err := checkNoSymlinkComponents(root, "does/not/exist"); err != nil {
		t.Fatalf("a missing path must not be a symlink error: %v", err)
	}
}

// TestListStripsAbsolutePathsAndSorts proves host layout never reaches the
// phone and that the order is deterministic.
func TestListStripsAbsolutePathsAndSorts(t *testing.T) {
	const body = `[
		{"name":"README.md","path":"README.md","absolute":"/host/secret/README.md","type":"file"},
		{"name":".git","path":".git/","absolute":"/host/secret/.git","type":"directory"},
		{"name":"go.mod","path":"go.mod","absolute":"/host/secret/go.mod","type":"file","ignored":true},
		{"name":"apps","path":"apps/","absolute":"/host/secret/apps","type":"directory"}
	]`
	h, s, _ := workspaceSession(t, route{"/file", body})
	got, err := s.ListWorkspace(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{".git", "apps", "README.md", "go.mod"}
	if len(got) != len(wantPaths) {
		t.Fatalf("entries = %+v", got)
	}
	for i, w := range wantPaths {
		if got[i].Path != w {
			t.Fatalf("entry %d = %q, want %q (dirs first, then lexical)", i, got[i].Path, w)
		}
	}
	if !got[0].Dir || got[2].Dir {
		t.Fatalf("dir flags wrong: %+v", got)
	}
	if !got[3].Ignored {
		t.Fatal("ignored flag lost")
	}
	blob, _ := json.Marshal(got)
	if strings.Contains(string(blob), "/host/secret") {
		t.Fatalf("an absolute host path reached the wire: %s", blob)
	}
	call := h.find(t, "GET", "/file")
	if !strings.Contains(call.path, "directory=") {
		t.Fatalf("no directory scope on %q", call.path)
	}
}

// TestListRejectsEscapingUpstreamPaths proves upstream is not trusted either.
func TestListRejectsEscapingUpstreamPaths(t *testing.T) {
	const body = `[
		{"name":"ok","path":"ok.txt","type":"file"},
		{"name":"esc","path":"../../etc/passwd","type":"file"},
		{"name":"abs","path":"/etc/shadow","type":"file"}
	]`
	_, s, _ := workspaceSession(t, route{"/file", body})
	got, err := s.ListWorkspace(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "ok.txt" {
		t.Fatalf("escaping upstream rows survived: %+v", got)
	}
}

// TestListIsCapped proves the row budget is enforced.
func TestListIsCapped(t *testing.T) {
	var rows []string
	for i := 0; i < maxWorkspaceEntries+50; i++ {
		rows = append(rows, fmt.Sprintf(`{"name":"f%04d","path":"f%04d.txt","type":"file"}`, i, i))
	}
	_, s, _ := workspaceSession(t, route{"/file", "[" + strings.Join(rows, ",") + "]"})
	got, err := s.ListWorkspace(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != maxWorkspaceEntries {
		t.Fatalf("entries = %d, want %d", len(got), maxWorkspaceEntries)
	}
}

// TestReadRefusesNonText proves binary is a refusal, never a garbled view.
func TestReadRefusesNonText(t *testing.T) {
	nulBody, _ := json.Marshal(map[string]string{"type": "text", "content": "a" + string(rune(0)) + "b"})
	for _, body := range []string{
		`{"type":"binary","content":"AAEC"}`,
		string(nulBody),
	} {
		_, s, _ := workspaceSession(t, route{"/file/content", body})
		_, err := s.ReadWorkspace(context.Background(), "a.bin")
		if !errors.Is(err, errWorkspaceBinary) {
			t.Fatalf("body %s gave err %v, want a binary refusal", body, err)
		}
	}
}

// TestReadRefusesOversizeRatherThanTruncating is the "never a partial view"
// rule: a truncated file read as complete hides the part that mattered.
func TestReadRefusesOversizeRatherThanTruncating(t *testing.T) {
	big, _ := json.Marshal(map[string]string{
		"type": "text", "content": strings.Repeat("a", maxWorkspaceFileBytes+1),
	})
	_, s, _ := workspaceSession(t, route{"/file/content", string(big)})
	got, err := s.ReadWorkspace(context.Background(), "big.txt")
	if !errors.Is(err, errWorkspaceTooLarge) {
		t.Fatalf("err = %v, want result_too_large", err)
	}
	if got.Text != "" {
		t.Fatal("an oversize read returned partial content")
	}

	atLimit, _ := json.Marshal(map[string]string{
		"type": "text", "content": strings.Repeat("a", maxWorkspaceFileBytes),
	})
	_, s2, _ := workspaceSession(t, route{"/file/content", string(atLimit)})
	ok, err := s2.ReadWorkspace(context.Background(), "big.txt")
	if err != nil {
		t.Fatalf("a file of exactly the limit was refused: %v", err)
	}
	if ok.Bytes != maxWorkspaceFileBytes {
		t.Fatalf("bytes = %d", ok.Bytes)
	}
}

// TestReadRefusesTheRoot proves a directory is not a file.
func TestReadRefusesTheRoot(t *testing.T) {
	_, s, _ := workspaceSession(t)
	if _, err := s.ReadWorkspace(context.Background(), ""); !errors.Is(err, errWorkspaceInvalidPath) {
		t.Fatalf("err = %v", err)
	}
}

// TestSearchQueryValidation covers every rejected query shape, including the
// symbol kind this surface deliberately never advertises.
func TestSearchQueryValidation(t *testing.T) {
	_, s, _ := workspaceSession(t)
	for _, tc := range []struct{ kind, query string }{
		{provider.WorkspaceSearchText, ""},
		{provider.WorkspaceSearchText, "   "},
		{provider.WorkspaceSearchText, strings.Repeat("q", maxWorkspaceQueryLen+1)},
		{provider.WorkspaceSearchText, "a" + string(rune(0)) + "b"},
		{"symbol", "Thing"},
		{"", "x"},
	} {
		if _, err := s.SearchWorkspace(context.Background(), tc.kind, tc.query); !errors.Is(err, errWorkspaceInvalidQuery) {
			t.Fatalf("kind=%q query=%q gave %v, want invalid_query", tc.kind, tc.query, err)
		}
	}
}

// TestTextSearchReportsTheUpstreamCap pins the asymmetry: text search is capped
// upstream at ten with no parameter to raise it, so reporting the row budget
// would tell a client the search was broader than it was.
func TestTextSearchReportsTheUpstreamCap(t *testing.T) {
	const body = `[
		{"path":{"text":"b.txt"},"lines":{"text":"hit\n"},"line_number":3,
		 "submatches":[{"match":{"text":"hit"},"start":4,"end":7}]},
		{"path":{"text":"a.txt"},"lines":{"text":"hit\n"},"line_number":1,
		 "submatches":[{"match":{"text":"hit"},"start":0,"end":3}]}
	]`
	h, s, _ := workspaceSession(t, route{"/find", body})
	got, err := s.SearchWorkspace(context.Background(), provider.WorkspaceSearchText, "hit")
	if err != nil {
		t.Fatal(err)
	}
	if got.Cap != textSearchCap {
		t.Fatalf("cap = %d, want the upstream hard cap %d", got.Cap, textSearchCap)
	}
	if got.Kind != provider.WorkspaceSearchText {
		t.Fatalf("kind = %q", got.Kind)
	}
	if got.Matches[0].Path != "a.txt" || got.Matches[1].Path != "b.txt" {
		t.Fatalf("unsorted: %+v", got.Matches)
	}
	if got.Matches[0].Line != 1 || got.Matches[0].Column != 1 {
		t.Fatalf("position = %+v", got.Matches[0])
	}
	if got.Matches[1].Column != 5 {
		t.Fatalf("column is not 1-based: %+v", got.Matches[1])
	}
	call := h.find(t, "GET", "/find")
	if !strings.Contains(call.path, "pattern=hit") {
		t.Fatalf("text search used the wrong parameter name: %q", call.path)
	}
}

// TestFileSearchAsksForTheDocumentedLimit pins the request shape: the handler
// defaults to ten, so the limit must be sent explicitly.
func TestFileSearchAsksForTheDocumentedLimit(t *testing.T) {
	h, s, _ := workspaceSession(t, route{"/find/file", `["b/x.go","a/y.go"]`})
	got, err := s.SearchWorkspace(context.Background(), provider.WorkspaceSearchFile, "x")
	if err != nil {
		t.Fatal(err)
	}
	if got.Cap != fileSearchLimit {
		t.Fatalf("cap = %d, want %d", got.Cap, fileSearchLimit)
	}
	if got.Matches[0].Path != "a/y.go" {
		t.Fatalf("unsorted: %+v", got.Matches)
	}
	call := h.find(t, "GET", "/find/file")
	for _, want := range []string{"query=x", "limit=100"} {
		if !strings.Contains(call.path, want) {
			t.Fatalf("file search missing %q in %q", want, call.path)
		}
	}
}

// TestWorkspaceNeverCallsExcludedRoutes is the route spy the acceptance
// requires: no write/apply/config endpoint, and none of the three excluded read
// routes, is reachable from this surface.
func TestWorkspaceNeverCallsExcludedRoutes(t *testing.T) {
	h, s, _ := workspaceSession(t,
		route{"/file/content", `{"type":"text","content":"x"}`},
		route{"/find/file", `[]`},
		route{"/find", `[]`},
		route{"/file", `[]`},
	)
	ctx := context.Background()
	_, _ = s.ListWorkspace(ctx, "")
	_, _ = s.ReadWorkspace(ctx, "a.txt")
	_, _ = s.SearchWorkspace(ctx, provider.WorkspaceSearchText, "q")
	_, _ = s.SearchWorkspace(ctx, provider.WorkspaceSearchFile, "q")

	forbidden := []string{
		"/vcs/status", "/file/status", "/find/symbol",
		"/apply", "/config", "/session/", "/mcp", "/instance",
	}
	for _, c := range h.calls {
		if c.method != "GET" {
			t.Fatalf("workspace issued a %s request to %q; this surface is read-only", c.method, c.path)
		}
		for _, bad := range forbidden {
			if strings.Contains(c.path, bad) {
				t.Fatalf("workspace called excluded route %q", c.path)
			}
		}
	}
	if len(h.calls) != 4 {
		t.Fatalf("expected exactly four reads, got %d: %v", len(h.calls), h.paths())
	}
}

// TestWorkspaceRequestsCarrySessionDirectory proves a client can never choose
// the directory: the daemon supplies the approved CWD on every request.
func TestWorkspaceRequestsCarrySessionDirectory(t *testing.T) {
	h, s, _ := workspaceSession(t,
		route{"/file/content", `{"type":"text","content":"x"}`},
		route{"/find", `[]`},
		route{"/file", `[]`},
	)
	ctx := context.Background()
	_, _ = s.ListWorkspace(ctx, "")
	_, _ = s.ReadWorkspace(ctx, "a.txt")
	_, _ = s.SearchWorkspace(ctx, provider.WorkspaceSearchText, "q")
	for _, c := range h.calls {
		if !strings.Contains(c.path, "directory=") {
			t.Fatalf("request %q carried no session directory", c.path)
		}
	}
}

// TestBoundedJSONReaderDetectsOverflowBeforeDecode proves the envelope guard
// fails outright rather than producing partial JSON.
func TestBoundedJSONReaderDetectsOverflowBeforeDecode(t *testing.T) {
	ok := strings.NewReader(strings.Repeat("a", maxWorkspaceEnvelope))
	if _, err := boundedJSONReader(ok); err != nil {
		t.Fatalf("a body of exactly the envelope was refused: %v", err)
	}
	over := strings.NewReader(strings.Repeat("a", maxWorkspaceEnvelope+1))
	if _, err := boundedJSONReader(over); !errors.Is(err, errWorkspaceTooLarge) {
		t.Fatalf("err = %v, want result_too_large", err)
	}
}

// TestWorkspaceCancellationPropagates proves a cancelled context stops the
// request rather than completing it.
func TestWorkspaceCancellationPropagates(t *testing.T) {
	h := newRecorder()
	h.cwd = t.TempDir()
	h.api = func(ctx context.Context, _, _ string, _, _ any) error { return ctx.Err() }
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.ListWorkspace(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("list err = %v, want context.Canceled", err)
	}
	if _, err := s.ReadWorkspace(ctx, "a.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("read err = %v, want context.Canceled", err)
	}
	if _, err := s.SearchWorkspace(ctx, provider.WorkspaceSearchText, "q"); !errors.Is(err, context.Canceled) {
		t.Fatalf("search err = %v, want context.Canceled", err)
	}
}

// TestFixtureWorkspaceShapesDecode pins the committed 1.18.21 evidence corpus.
func TestFixtureWorkspaceShapesDecode(t *testing.T) {
	var fx struct {
		FileList struct {
			Response json.RawMessage `json:"response"`
		} `json:"file_list"`
		FileContentText struct {
			Response json.RawMessage `json:"response"`
		} `json:"file_content_text"`
		FindText struct {
			Response json.RawMessage `json:"response"`
		} `json:"find_text"`
		FindFile struct {
			Response json.RawMessage `json:"response"`
		} `json:"find_file"`
	}
	readSurfaceFixture(t, "workspace-endpoints.json", &fx)

	_, ls, _ := workspaceSession(t, route{"/file", string(fx.FileList.Response)})
	entries, err := ls.ListWorkspace(context.Background(), "")
	if err != nil || len(entries) == 0 {
		t.Fatalf("fixture listing failed: %v (%d entries)", err, len(entries))
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Path, "/") || strings.Contains(e.Path, "PROJECT") {
			t.Fatalf("fixture listing leaked an absolute path: %+v", e)
		}
	}

	_, rs, _ := workspaceSession(t, route{"/file/content", string(fx.FileContentText.Response)})
	content, err := rs.ReadWorkspace(context.Background(), "README.md")
	if err != nil || content.Text != "hello" {
		t.Fatalf("fixture content = %+v, err %v", content, err)
	}

	_, ts, _ := workspaceSession(t, route{"/find", string(fx.FindText.Response)})
	text, err := ts.SearchWorkspace(context.Background(), provider.WorkspaceSearchText, "hello")
	if err != nil || len(text.Matches) != 1 || text.Matches[0].Path != "README.md" {
		t.Fatalf("fixture text search = %+v, err %v", text, err)
	}

	_, fsx, _ := workspaceSession(t, route{"/find/file", string(fx.FindFile.Response)})
	files, err := fsx.SearchWorkspace(context.Background(), provider.WorkspaceSearchFile, "README")
	if err != nil || len(files.Matches) != 1 {
		t.Fatalf("fixture file search = %+v, err %v", files, err)
	}
}

// TestWorkspaceUpstreamFailuresPropagate proves an engine error surfaces rather
// than becoming an empty listing, which would read as "this project has no
// files" — a different and misleading claim.
func TestWorkspaceUpstreamFailuresPropagate(t *testing.T) {
	want := errors.New("engine unreachable")
	h := newRecorder()
	h.cwd = t.TempDir()
	h.api = func(context.Context, string, string, any, any) error { return want }
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	ctx := context.Background()

	if _, err := s.ListWorkspace(ctx, ""); !errors.Is(err, want) {
		t.Fatalf("list err = %v", err)
	}
	if _, err := s.ReadWorkspace(ctx, "a.txt"); !errors.Is(err, want) {
		t.Fatalf("read err = %v", err)
	}
	if _, err := s.SearchWorkspace(ctx, provider.WorkspaceSearchText, "q"); !errors.Is(err, want) {
		t.Fatalf("text search err = %v", err)
	}
	if _, err := s.SearchWorkspace(ctx, provider.WorkspaceSearchFile, "q"); !errors.Is(err, want) {
		t.Fatalf("file search err = %v", err)
	}
}

// TestWorkspaceRejectsUndecodableResponses proves a malformed body is a failure
// rather than a silently empty result.
func TestWorkspaceRejectsUndecodableResponses(t *testing.T) {
	_, s, _ := workspaceSession(t, route{"/file", `{"not":"an array"}`})
	if _, err := s.ListWorkspace(context.Background(), ""); err == nil {
		t.Fatal("a malformed listing decoded successfully")
	}
	_, s2, _ := workspaceSession(t, route{"/file/content", `["not","an object"]`})
	if _, err := s2.ReadWorkspace(context.Background(), "a.txt"); err == nil {
		t.Fatal("a malformed content body decoded successfully")
	}
}

// TestWorkspacePathValidationRunsBeforeAnyRequest proves a bad path never
// reaches the engine at all.
func TestWorkspacePathValidationRunsBeforeAnyRequest(t *testing.T) {
	h, s, _ := workspaceSession(t, route{"/file", `[]`}, route{"/file/content", `{}`})
	ctx := context.Background()
	for _, bad := range []string{"/etc/passwd", "../up", "a" + string(rune(0)) + "b"} {
		if _, err := s.ListWorkspace(ctx, bad); err == nil {
			t.Fatalf("list(%q) was allowed", bad)
		}
		if _, err := s.ReadWorkspace(ctx, bad); err == nil {
			t.Fatalf("read(%q) was allowed", bad)
		}
	}
	if len(h.calls) != 0 {
		t.Fatalf("a rejected path still reached the engine: %v", h.paths())
	}
}

// TestSymlinkCheckSurfacesInspectionFailures proves an unreadable component is
// a refusal rather than a silent pass.
func TestSymlinkCheckSurfacesInspectionFailures(t *testing.T) {
	root := t.TempDir()
	// A file where a directory is expected makes Lstat of a child fail with a
	// non-NotExist error.
	if err := os.WriteFile(filepath.Join(root, "afile"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := checkNoSymlinkComponents(root, "afile/child/leaf")
	if err != nil && !errors.Is(err, errWorkspaceInvalidPath) {
		t.Fatalf("err = %v, want an invalid-path refusal or nil", err)
	}
}

// TestListSkipsRowsWithNoUsableName proves a nameless row still gets one from
// its path rather than rendering blank.
func TestListSkipsRowsWithNoUsableName(t *testing.T) {
	const body = `[{"path":"deep/nested/file.txt","type":"file"}]`
	_, s, _ := workspaceSession(t, route{"/file", body})
	got, err := s.ListWorkspace(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "file.txt" {
		t.Fatalf("entries = %+v", got)
	}
}

// TestSearchCapsAreEnforcedOnTheRowBudget proves the 100-row budget applies on
// top of whatever the engine returned.
func TestSearchCapsAreEnforcedOnTheRowBudget(t *testing.T) {
	var rows []string
	for i := 0; i < maxWorkspaceMatches+25; i++ {
		rows = append(rows, fmt.Sprintf(`"dir/f%04d.go"`, i))
	}
	_, s, _ := workspaceSession(t, route{"/find/file", "[" + strings.Join(rows, ",") + "]"})
	got, err := s.SearchWorkspace(context.Background(), provider.WorkspaceSearchFile, "f")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Matches) != maxWorkspaceMatches {
		t.Fatalf("matches = %d, want %d", len(got.Matches), maxWorkspaceMatches)
	}
	if !got.Truncated {
		t.Fatal("a capped result did not report truncation")
	}
}

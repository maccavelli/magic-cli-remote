package control

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/wirecap"
)

// TestMain doubles as `opencode serve --stdio --port 0` for this package's
// tests, which re-run this binary as the control plane's child.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_OPENCODE_SERVE") == "1" {
		runFakeServe()
		return
	}
	// The live MCP pin's servers (live_mcp_test.go): append the tag and exit.
	if mark := os.Getenv("FAKE_MCP_MARKER"); mark != "" {
		tag := "?"
		if len(os.Args) > 1 {
			tag = os.Args[1]
		}
		if f, err := os.OpenFile(mark, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			_, _ = f.WriteString(tag + "\n")
			_ = f.Close()
		}
		return
	}
	os.Exit(m.Run())
}

// fakeLog is one line the fake writes to FAKE_SERVE_LOG.
type fakeLog struct {
	Kind     string          `json:"kind"` // "launch" or "request"
	Argv     []string        `json:"argv,omitempty"`
	Password string          `json:"password,omitempty"`
	Method   string          `json:"method,omitempty"`
	Path     string          `json:"path,omitempty"`
	Location string          `json:"location,omitempty"`
	DirHead  string          `json:"dirHeader,omitempty"`
	AuthOK   bool            `json:"authOK,omitempty"`
	Body     json.RawMessage `json:"body,omitempty"`
	// Config, Content and ConfigFile are the launch's OPENCODE_CONFIG,
	// OPENCODE_CONFIG_CONTENT and the named file's content (0230 MADR).
	Config     string `json:"config,omitempty"`
	Content    string `json:"content,omitempty"`
	ConfigFile string `json:"configFile,omitempty"`
}

// runFakeServe is a minimal serve --stdio: one {"url"} ready line, the routes
// the tests use behind HTTP Basic, an exit on stdin EOF, and 130 on SIGTERM.
// FAKE_SERVE_* variables bend it.
func runFakeServe() {
	password := os.Getenv(passwordEnv)
	var logMu sync.Mutex
	logLine := func(l fakeLog) {
		path := os.Getenv("FAKE_SERVE_LOG")
		if path == "" {
			return
		}
		logMu.Lock()
		defer logMu.Unlock()
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		b, _ := json.Marshal(l)
		_, _ = f.Write(append(b, '\n'))
		_ = f.Close()
	}
	launch := fakeLog{Kind: "launch", Argv: os.Args[1:], Password: password,
		Config: os.Getenv("OPENCODE_CONFIG"), Content: os.Getenv("OPENCODE_CONFIG_CONTENT")}
	if b, err := os.ReadFile(launch.Config); err == nil {
		launch.ConfigFile = string(b)
	}
	logLine(launch)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, os.Interrupt)
	go func() {
		<-sigs
		if os.Getenv("FAKE_SERVE_IGNORE_TERM") != "1" {
			os.Exit(130)
		}
	}()
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		if os.Getenv("FAKE_SERVE_IGNORE_LEASE") != "1" {
			os.Exit(0)
		}
	}()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(2)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(basicUser+":"+password))
	var agentCalls int
	var mu sync.Mutex
	jsonOut := func(w http.ResponseWriter, status int, body string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		entry := fakeLog{Kind: "request", Method: r.Method, Path: r.URL.Path,
			Location: r.URL.Query().Get("location[directory]"), DirHead: r.Header.Get("x-opencode-directory"),
			AuthOK: r.Header.Get("Authorization") == want}
		if json.Valid(body) {
			entry.Body = body
		}
		logLine(entry)
		if !entry.AuthOK {
			jsonOut(w, http.StatusUnauthorized, `{"error":"unauthorized"}`)
			return
		}
		switch r.URL.Path {
		case "/api/info":
			switch os.Getenv("FAKE_SERVE_INFO") {
			case "html":
				w.Header().Set("Content-Type", "text/html")
				_, _ = io.WriteString(w, "<!doctype html><html></html>")
			case "noversion":
				jsonOut(w, http.StatusOK, `{"data":{}}`)
			case "old":
				jsonOut(w, http.StatusOK, `{"data":{"version":"1.18.26"}}`)
			default:
				jsonOut(w, http.StatusOK, `{"data":{"version":"2.2.3"}}`)
			}
		case "/api/chatty":
			// More than a pipe holds: it blocks unless stdout is drained.
			_, _ = os.Stdout.Write(bytes.Repeat([]byte("log line\n"), 1<<17))
			jsonOut(w, http.StatusOK, `{"data":{}}`)
		case "/api/agent":
			mu.Lock()
			agentCalls++
			n := agentCalls
			mu.Unlock()
			var busy int
			_, _ = fmt.Sscan(os.Getenv("FAKE_SERVE_AGENT_503"), &busy)
			if n <= busy {
				jsonOut(w, http.StatusServiceUnavailable, `{"error":"ServiceUnavailableError"}`)
				return
			}
			jsonOut(w, http.StatusOK, `{"data":[{"name":"build"}]}`)
		case "/api/html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html>v2 web shell</html>")
		case "/api/slow":
			// Answers only after 400 ms, as session.shell answers when its
			// command ends.
			time.Sleep(400 * time.Millisecond)
			jsonOut(w, http.StatusOK, `{"data":{}}`)
		case "/api/fs/read/notes.md":
			// fs.read answers the file's bytes in its own type (0214 PLAN P6d).
			w.Header().Set("Content-Type", "text/markdown")
			_, _ = io.WriteString(w, rawFile)
		case "/api/exit":
			jsonOut(w, http.StatusOK, `{}`)
			go func() { time.Sleep(50 * time.Millisecond); os.Exit(3) }()
		default:
			jsonOut(w, http.StatusOK, `{"data":{"ok":true}}`)
		}
	})}
	if os.Getenv("FAKE_SERVE_READY") == "bad" {
		fmt.Println("Listening on a port")
	} else {
		fmt.Printf("{\"url\":\"http://%s\"}\n", l.Addr().String())
	}
	_ = srv.Serve(l)
}

// fake is one control plane on the fake engine, with its log and the plane's
// own log lines.
type fake struct {
	t     *testing.T
	p     *Plane
	path  string
	lines *syncBuffer
}

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// newFake returns a plane whose child is the fake, bent by env.
func newFake(t *testing.T, cfg Config, env ...string) *fake {
	t.Helper()
	f := &fake{t: t, path: filepath.Join(t.TempDir(), "serve.log"), lines: &syncBuffer{}}
	cfg.Bin = os.Args[0]
	cfg.Env = append([]string{"FAKE_OPENCODE_SERVE=1", "FAKE_SERVE_LOG=" + f.path}, env...)
	cfg.Log = slog.New(slog.NewTextHandler(f.lines, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if cfg.MinVersion == "" {
		cfg.MinVersion = "2.1.0"
		cfg.UpgradeURL = "https://opencode.invalid/install"
	}
	f.p = New(cfg)
	t.Cleanup(f.p.Close)
	return f
}

// log returns what the fake wrote, in order.
func (f *fake) log() []fakeLog {
	f.t.Helper()
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	var out []fakeLog
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		var l fakeLog
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, l)
	}
	return out
}

func (f *fake) launches() []fakeLog { return f.kind("launch") }
func (f *fake) requests() []fakeLog { return f.kind("request") }

func (f *fake) kind(k string) []fakeLog {
	var out []fakeLog
	for _, l := range f.log() {
		if l.Kind == k {
			out = append(out, l)
		}
	}
	return out
}

// do sends req and fails the test on a transport error.
func (f *fake) do(req Request) Response {
	f.t.Helper()
	res, err := f.p.Do(context.Background(), req)
	if err != nil {
		f.t.Fatalf("%s %s: %v", req.Method, req.Path, err)
	}
	return res
}

// waitFor polls cond for up to 10 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !cond(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// TestStartsOnFirstUse pins D2's launch: nothing runs until a request, then one
// `serve --stdio --port 0`, ready on its url line and a 2.x /api/info.
func TestStartsOnFirstUse(t *testing.T) {
	f := newFake(t, Config{})
	if n := len(f.launches()); n != 0 {
		t.Fatalf("%d launch(es) before any request", n)
	}
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	ls := f.launches()
	if len(ls) != 1 {
		t.Fatalf("%d launches, want 1", len(ls))
	}
	if got := strings.Join(ls[0].Argv, " "); got != "serve --stdio --port 0" {
		t.Errorf("argv = %q, want serve --stdio --port 0", got)
	}
	if v := f.p.Version(); v != "2.2.3" {
		t.Errorf("version = %q, want 2.2.3", v)
	}
}

// TestRequestsCarryTheirPasswordAndLocation pins D2's requests: HTTP Basic with
// the launch's password, and a directory as location[directory] and
// x-opencode-directory; a create body carries location.directory.
func TestRequestsCarryTheirPasswordAndLocation(t *testing.T) {
	f := newFake(t, Config{})
	dir := t.TempDir()
	f.do(Request{Method: http.MethodPost, Path: "/api/session", Directory: dir,
		Body: WithLocation(dir, map[string]any{"title": "t"})})
	var create *fakeLog
	for _, r := range f.requests() {
		if !r.AuthOK {
			t.Errorf("%s %s went without the launch's password", r.Method, r.Path)
		}
		if r.Path == "/api/session" {
			r := r
			create = &r
		}
	}
	if create == nil {
		t.Fatal("the create never reached the engine")
	}
	if create.Location != dir || create.DirHead != dir {
		t.Errorf("create location %q, header %q; want %q for both", create.Location, create.DirHead, dir)
	}
	var body struct {
		Title    string `json:"title"`
		Location struct {
			Directory string `json:"directory"`
		} `json:"location"`
	}
	if err := json.Unmarshal(create.Body, &body); err != nil || body.Location.Directory != dir || body.Title != "t" {
		t.Errorf("create body %s, want title t and location.directory %q", create.Body, dir)
	}
}

// TestStdoutIsDrainedAfterReady pins D2: stdout keeps being read after the
// ready line, as standalone.ts does, so a child that writes to it later does
// not stall.
func TestStdoutIsDrainedAfterReady(t *testing.T) {
	f := newFake(t, Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := f.p.Do(ctx, Request{Method: http.MethodGet, Path: "/api/chatty"}); err != nil {
		t.Fatalf("a child that wrote 1 MiB to stdout stalled: %v", err)
	}
}

// TestNeverReadsCredentials pins D2: GET /api/credential is refused before it
// is sent, and starts nothing.
func TestNeverReadsCredentials(t *testing.T) {
	f := newFake(t, Config{})
	for _, path := range []string{"/api/credential", "/api/credential/abc"} {
		if _, err := f.p.Do(context.Background(), Request{Method: http.MethodGet, Path: path}); !errors.Is(err, ErrCredentialRead) {
			t.Errorf("GET %s: err %v, want ErrCredentialRead", path, err)
		}
	}
	if n := len(f.launches()); n != 0 {
		t.Errorf("refusing a credential read started %d engine(s)", n)
	}
}

// TestNeverCallsProviderRoutes pins 0214 PLAN P6c Deviation 1: /api/provider
// and /api/model, which return configured keys, are refused before they are
// sent, by any method, and start nothing. A route that only shares the prefix
// is not refused.
func TestNeverCallsProviderRoutes(t *testing.T) {
	f := newFake(t, Config{})
	for _, req := range []Request{
		{Method: http.MethodGet, Path: "/api/provider"},
		{Method: http.MethodGet, Path: "/api/provider/mistral"},
		{Method: http.MethodGet, Path: "/api/model"},
		{Method: http.MethodGet, Path: "/api/model/default"},
		{Method: http.MethodPatch, Path: "/api/provider/mistral"},
	} {
		if _, err := f.p.Do(context.Background(), req); !errors.Is(err, ErrProviderRoute) {
			t.Errorf("%s %s: err %v, want ErrProviderRoute", req.Method, req.Path, err)
		}
	}
	if n := len(f.launches()); n != 0 {
		t.Errorf("refusing a provider route started %d engine(s)", n)
	}
	if !underRoute("/api/provider", "/api/provider") || underRoute("/api/providers", "/api/provider") ||
		underRoute("/api/modelx", "/api/model") {
		t.Error("underRoute matches a route that only shares the prefix")
	}
}

// TestAgentsRetry503 pins A3: GET /api/agent is retried while it answers 503,
// a bounded number of times.
func TestAgentsRetry503(t *testing.T) {
	first := agentRetryFirst
	agentRetryFirst = time.Millisecond
	t.Cleanup(func() { agentRetryFirst = first })

	f := newFake(t, Config{}, "FAKE_SERVE_AGENT_503=2")
	if res := f.do(Request{Method: http.MethodGet, Path: "/api/agent"}); res.Status != http.StatusOK {
		t.Errorf("after two 503s: %d, want 200", res.Status)
	}
	if n := len(f.kind("request")) - 1; n != 3 { // less the start's /api/info
		t.Errorf("%d agent requests, want 3", n)
	}

	g := newFake(t, Config{}, "FAKE_SERVE_AGENT_503=100")
	if res := g.do(Request{Method: http.MethodGet, Path: "/api/agent"}); res.Status != http.StatusServiceUnavailable {
		t.Errorf("always 503: %d, want the 503 after the last try", res.Status)
	}
	if n := len(g.kind("request")) - 1; n != agentRetries {
		t.Errorf("%d agent requests, want %d", n, agentRetries)
	}
}

// TestStartRefusesWhatIsNotTheEngine pins D2's readiness and D3's floor: a
// ready line that is not {"url"}, an /api/info that is not JSON, and a 1.x or
// unnamed version each fail the start, saying which.
func TestStartRefusesWhatIsNotTheEngine(t *testing.T) {
	for _, c := range []struct {
		env  string
		want []string
	}{
		{"FAKE_SERVE_READY=bad", []string{"ready line", "Listening on a port"}},
		{"FAKE_SERVE_INFO=html", []string{"text/html", "<!doctype html>"}},
		{"FAKE_SERVE_INFO=old", []string{"1.18.26", "2.1.0", "https://opencode.invalid/install"}},
		{"FAKE_SERVE_INFO=noversion", []string{"no version", "2.1.0"}},
	} {
		f := newFake(t, Config{StartTimeout: 10 * time.Second}, c.env)
		_, err := f.p.Do(context.Background(), Request{Method: http.MethodGet, Path: "/api/agent"})
		if err == nil {
			t.Errorf("%s: the start succeeded", c.env)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: error %q does not name %q", c.env, err, w)
			}
		}
	}
}

// TestAnswerMustBeJSON pins D12 on the control plane: a 200 that is not JSON
// is an error naming what came instead.
func TestAnswerMustBeJSON(t *testing.T) {
	f := newFake(t, Config{})
	_, err := f.p.Do(context.Background(), Request{Method: http.MethodGet, Path: "/api/html"})
	if err == nil || !strings.Contains(err.Error(), "text/html") || !strings.Contains(err.Error(), "v2 web shell") {
		t.Errorf("err = %v, want one naming text/html and the body", err)
	}
}

// rawFile is what the fake's fs.read route answers. Its marker must never
// reach a capture.
const rawFile = "# notes\nraw-file-marker-7f3a\n"

// TestRawAnswerSkipsTheJSONCheck: a raw request takes a file's bytes in their
// own content type, where any other request refuses them (D12).
func TestRawAnswerSkipsTheJSONCheck(t *testing.T) {
	f := newFake(t, Config{})
	res := f.do(Request{Method: http.MethodGet, Path: "/api/fs/read/notes.md", Raw: true, MaxBytes: 1 << 10})
	if string(res.Body) != rawFile || res.ContentType != "text/markdown" || res.Truncated {
		t.Errorf("raw answer = %q, %q, truncated %v; want the file in text/markdown", res.Body, res.ContentType, res.Truncated)
	}
	if _, err := f.p.Do(context.Background(), Request{Method: http.MethodGet, Path: "/api/fs/read/notes.md"}); err == nil {
		t.Error("the same answer taken as JSON was accepted")
	}
}

// TestRawAnswerIsBounded: a raw answer is read to MaxBytes, and one longer is
// cut there and marked, never passed whole.
func TestRawAnswerIsBounded(t *testing.T) {
	f := newFake(t, Config{})
	res := f.do(Request{Method: http.MethodGet, Path: "/api/fs/read/notes.md", Raw: true, MaxBytes: 7})
	if string(res.Body) != rawFile[:7] || !res.Truncated {
		t.Errorf("MaxBytes 7: %q, truncated %v; want the first 7 bytes, truncated", res.Body, res.Truncated)
	}
	res = f.do(Request{Method: http.MethodGet, Path: "/api/fs/read/notes.md", Raw: true, MaxBytes: len(rawFile)})
	if string(res.Body) != rawFile || res.Truncated {
		t.Errorf("MaxBytes = its length: %q, truncated %v; want it whole", res.Body, res.Truncated)
	}
}

// TestRawNeedsABound: a raw request without MaxBytes is refused before
// anything starts.
func TestRawNeedsABound(t *testing.T) {
	f := newFake(t, Config{})
	if _, err := f.p.Do(context.Background(), Request{Method: http.MethodGet, Path: "/api/fs/read/notes.md", Raw: true}); err == nil {
		t.Error("a raw request with no MaxBytes was sent")
	}
	if n := len(f.launches()); n != 0 {
		t.Errorf("%d launch(es); want none", n)
	}
}

// TestRawBodyIsNotCaptured: with capture on, a raw answer is recorded by its
// size, and its bytes are not (0214 MADR, added at P6d).
func TestRawBodyIsNotCaptured(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(wirecap.EnvDir, dir)
	f := newFake(t, Config{})
	f.do(Request{Method: http.MethodGet, Path: "/api/fs/read/notes.md", Raw: true, MaxBytes: 1 << 10})
	f.p.Close()
	var all strings.Builder
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			b, _ := os.ReadFile(path)
			all.Write(b)
		}
		return nil
	})
	if !strings.Contains(all.String(), `"bytes":`+fmt.Sprint(len(rawFile))) {
		t.Fatalf("no size line was captured; the test proves nothing: %q", all.String())
	}
	if strings.Contains(all.String(), "raw-file-marker-7f3a") {
		t.Error("a capture carries the raw file's bytes")
	}
}

// TestRequestTimeoutBoundsOneRequest: a request's own Timeout bounds it, and a
// request without one takes the default, which a slow answer is well within
// (0214 MADR, added at P6e).
func TestRequestTimeoutBoundsOneRequest(t *testing.T) {
	f := newFake(t, Config{})
	start := time.Now()
	_, err := f.p.Do(context.Background(), Request{Method: http.MethodGet, Path: "/api/slow", Timeout: 50 * time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Errorf("a 50 ms bound on a 400 ms answer: %v after %s; want the deadline", err, time.Since(start))
	}
	if res := f.do(Request{Method: http.MethodGet, Path: "/api/slow"}); res.Status != http.StatusOK {
		t.Errorf("without a Timeout: %d, want the answer", res.Status)
	}
}

// clearOperatorConfig unsets the two variables for the test, whatever the
// developer's environment holds.
func clearOperatorConfig(t *testing.T) {
	t.Helper()
	t.Setenv("OPENCODE_CONFIG", "")
	t.Setenv("OPENCODE_CONFIG_CONTENT", "")
}

// policies decodes a config document's policies.
func policies(t *testing.T, doc string) []map[string]any {
	t.Helper()
	var d struct {
		Policies []map[string]any `json:"policies"`
	}
	if err := json.Unmarshal([]byte(doc), &d); err != nil {
		t.Fatalf("%q: %v", doc, err)
	}
	return d.Policies
}

// isDenyAllMCP reports whether a policy statement denies every MCP server.
func isDenyAllMCP(p map[string]any) bool {
	return p["action"] == "mcp.use" && p["resource"] == "*" && p["effect"] == "deny"
}

// TestControlPlaneDeniesMCP: by default the child is started with
// OPENCODE_CONFIG naming a file that holds exactly the deny-all MCP policy,
// and no inline config (0230 MADR D1, D2).
func TestControlPlaneDeniesMCP(t *testing.T) {
	clearOperatorConfig(t)
	f := newFake(t, Config{})
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	l := f.launches()[0]
	if l.Config == "" || l.ConfigFile != mcpDenyPolicy || l.Content != "" {
		t.Errorf("launch config %q (file %q), content %q; want the policy file alone", l.Config, l.ConfigFile, l.Content)
	}
}

// TestControlPlaneKeepsTheOperatorsConfig: an operator's OPENCODE_CONFIG is
// kept and the policy goes inline; an operator's inline JSON object keeps
// every key and gains the policy last, where it wins (0230 MADR D2).
func TestControlPlaneKeepsTheOperatorsConfig(t *testing.T) {
	clearOperatorConfig(t)
	op := filepath.Join(t.TempDir(), "operator.json")
	t.Setenv("OPENCODE_CONFIG", op)
	f := newFake(t, Config{})
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	l := f.launches()[0]
	if ps := policies(t, l.Content); l.Config != op || len(ps) != 1 || !isDenyAllMCP(ps[0]) {
		t.Errorf("config %q, content %q; want the operator's file and the policy inline", l.Config, l.Content)
	}

	t.Setenv("OPENCODE_CONFIG_CONTENT",
		`{"model":"opencode/x","policies":[{"action":"mcp.use","resource":"docs","effect":"allow"}],"n":1.50}`)
	f = newFake(t, Config{})
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	l = f.launches()[0]
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(l.Content), &doc); err != nil {
		t.Fatal(err)
	}
	ps := policies(t, l.Content)
	if string(doc["model"]) != `"opencode/x"` || string(doc["n"]) != "1.50" || len(ps) != 2 ||
		ps[0]["resource"] != "docs" || !isDenyAllMCP(ps[1]) {
		t.Errorf("content %q; want the operator's keys kept and the policy appended", l.Content)
	}
}

// TestControlPlaneWarnsWhenItCannotDeny: inline config that is not a JSON
// object is passed on unchanged, and the plane says the MCP policy is not set.
func TestControlPlaneWarnsWhenItCannotDeny(t *testing.T) {
	clearOperatorConfig(t)
	t.Setenv("OPENCODE_CONFIG", filepath.Join(t.TempDir(), "operator.json"))
	const jsonc = "// the operator's\n{\"model\": \"opencode/x\",}"
	t.Setenv("OPENCODE_CONFIG_CONTENT", jsonc)
	f := newFake(t, Config{})
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	if l := f.launches()[0]; l.Content != jsonc {
		t.Errorf("content %q; want the operator's, unchanged", l.Content)
	}
	if !strings.Contains(f.lines.String(), "the MCP policy is not set") || !strings.Contains(f.lines.String(), "0230") {
		t.Errorf("no warning logged:\n%s", f.lines.String())
	}
}

// TestDenyMCPMatchesNamesAsTheOSDoes: on Windows a variable's name matches in
// any case; elsewhere only exactly.
func TestDenyMCPMatchesNamesAsTheOSDoes(t *testing.T) {
	env := []string{"opencode_config=/op.json"}
	if got, _ := denyMCP(env, "/policy.json", true); got[len(got)-1] != "OPENCODE_CONFIG_CONTENT="+mcpDenyPolicy {
		t.Errorf("folded: %v; want the operator's file kept and the policy inline", got)
	}
	if got, _ := denyMCP(env, "/policy.json", false); got[len(got)-1] != "OPENCODE_CONFIG=/policy.json" {
		t.Errorf("exact: %v; want the policy file, the lower-case name being another variable", got)
	}
}

// TestPolicyFileIsRemovedOnClose: the policy's directory goes with the plane.
func TestPolicyFileIsRemovedOnClose(t *testing.T) {
	clearOperatorConfig(t)
	f := newFake(t, Config{})
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	file := f.launches()[0].Config
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the policy file is not there while the plane runs: %v", err)
	}
	f.p.Close()
	if _, err := os.Stat(filepath.Dir(file)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("after Close, the policy directory: %v; want it gone", err)
	}
}

// TestOnlyTheControlPlaneSetsOpencodeConfig: no other production file in the
// provider tree names the variables, so the engine's environment never
// carries the policy (0230 MADR: the engine is untouched).
func TestOnlyTheControlPlaneSetsOpencodeConfig(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		if filepath.Base(filepath.Dir(path)) == "control" && filepath.Base(filepath.Dir(filepath.Dir(path))) == "opencode" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "OPENCODE_CONFIG") {
			t.Errorf("%s names OPENCODE_CONFIG; only the control plane sets it", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// stopLine returns the plane's log line about how the child ended.
func stopLine(f *fake) string {
	for _, l := range strings.Split(f.lines.String(), "\n") {
		if strings.Contains(l, "control plane stopped") || strings.Contains(l, "control plane exited") {
			return l
		}
	}
	return ""
}

// TestLeaseCloseIsOrderly pins D2's lifetime: Close ends the lease, and the
// child exits 0, which is orderly.
func TestLeaseCloseIsOrderly(t *testing.T) {
	f := newFake(t, Config{})
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	f.p.Close()
	waitFor(t, "the exit line", func() bool { return stopLine(f) != "" })
	if l := stopLine(f); !strings.Contains(l, "control plane stopped") || !strings.Contains(l, "exit_code=0") {
		t.Errorf("exit line %q, want an orderly stop with 0", l)
	}
}

// TestIgnoredLeaseIsTerminated: a child that does not end on its lease gets
// SIGTERM, and its 130 is orderly on POSIX; on Windows the forced stop exits 1
// and is not (0214 MADR amendment B, D2).
func TestIgnoredLeaseIsTerminated(t *testing.T) {
	f := newFake(t, Config{LeaseWait: 100 * time.Millisecond}, "FAKE_SERVE_IGNORE_LEASE=1")
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	f.p.Close()
	waitFor(t, "the exit line", func() bool { return stopLine(f) != "" })
	l := stopLine(f)
	if runtime.GOOS == "windows" {
		if !strings.Contains(l, "control plane exited") {
			t.Errorf("exit line %q, want a forced stop that is not orderly", l)
		}
		return
	}
	if !strings.Contains(l, "control plane stopped") || !strings.Contains(l, "exit_code=130") {
		t.Errorf("exit line %q, want an orderly stop with 130", l)
	}
}

// TestIgnoredTerminateIsKilled: a child that ignores its lease and SIGTERM is
// killed after the bound, and Close returns.
func TestIgnoredTerminateIsKilled(t *testing.T) {
	f := newFake(t, Config{LeaseWait: 100 * time.Millisecond, TerminateWait: 100 * time.Millisecond},
		"FAKE_SERVE_IGNORE_LEASE=1", "FAKE_SERVE_IGNORE_TERM=1")
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	done := make(chan struct{})
	go func() { f.p.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return: the child was not killed")
	}
}

// TestIdleStopsAndNextUseRestarts pins D2's on-demand lifetime.
func TestIdleStopsAndNextUseRestarts(t *testing.T) {
	f := newFake(t, Config{IdleTimeout: 200 * time.Millisecond})
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	waitFor(t, "the idle stop", func() bool { return stopLine(f) != "" })
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	if n := len(f.launches()); n != 2 {
		t.Errorf("%d launches, want a second after the idle stop", n)
	}
}

// TestRestartsAfterTheChildDies: a child that exits on its own is replaced on
// the next use.
func TestRestartsAfterTheChildDies(t *testing.T) {
	f := newFake(t, Config{})
	f.do(Request{Method: http.MethodGet, Path: "/api/exit"})
	waitFor(t, "the child's exit", func() bool { return stopLine(f) != "" })
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	if n := len(f.launches()); n != 2 {
		t.Errorf("%d launches, want a second after the child died", n)
	}
}

// password returns the password the fake's first launch was given.
func (f *fake) password() string {
	f.t.Helper()
	ls := f.launches()
	if len(ls) == 0 || ls[0].Password == "" {
		f.t.Fatal("the fake was launched without a password")
	}
	return ls[0].Password
}

// TestPasswordIsNotInArgv pins C5: the password reaches the child only through
// its environment.
func TestPasswordIsNotInArgv(t *testing.T) {
	f := newFake(t, Config{})
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	pw := f.password()
	for _, a := range f.launches()[0].Argv {
		if strings.Contains(a, pw) {
			t.Errorf("argv %q carries the password", f.launches()[0].Argv)
		}
	}
}

// TestPasswordIsNotLogged pins C5 across a start, requests, a refusal, an
// error answer, a death, a restart and a stop, at Debug.
func TestPasswordIsNotLogged(t *testing.T) {
	f := newFake(t, Config{})
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	_, _ = f.p.Do(context.Background(), Request{Method: http.MethodGet, Path: "/api/html"})
	_, _ = f.p.Do(context.Background(), Request{Method: http.MethodGet, Path: "/api/credential"})
	pw := f.password()
	f.do(Request{Method: http.MethodGet, Path: "/api/exit"})
	waitFor(t, "the child's exit", func() bool { return stopLine(f) != "" })
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	f.p.Close()
	logs := f.lines.String()
	if !strings.Contains(logs, "control plane request") {
		t.Fatalf("the plane logged no request at Debug; the test proves nothing:\n%s", logs)
	}
	for _, l := range f.launches() {
		if strings.Contains(logs, l.Password) {
			t.Errorf("the log carries a launch's password")
		}
	}
	if strings.Contains(logs, base64.StdEncoding.EncodeToString([]byte(basicUser+":"+pw))) {
		t.Error("the log carries the Authorization header")
	}
}

// TestPasswordIsNotCaptured pins C5 on wire captures: with capture on, the
// control plane's frames are written, and none carries the password.
func TestPasswordIsNotCaptured(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(wirecap.EnvDir, dir)
	f := newFake(t, Config{})
	f.do(Request{Method: http.MethodGet, Path: "/api/agent"})
	f.do(Request{Method: http.MethodPost, Path: "/api/session", Body: map[string]any{"title": "t"}})
	pw := f.password()
	f.p.Close()
	var all strings.Builder
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			b, _ := os.ReadFile(path)
			all.Write(b)
		}
		return nil
	})
	if !strings.Contains(all.String(), "2.2.3") {
		t.Fatalf("nothing was captured; the test proves nothing")
	}
	if strings.Contains(all.String(), pw) || strings.Contains(all.String(), base64.StdEncoding.EncodeToString([]byte(basicUser+":"+pw))) {
		t.Error("a capture carries the password")
	}
}

// TestOrderly pins which exits are orderly on this host (amendment B, D2).
func TestOrderly(t *testing.T) {
	if !Orderly(0) || Orderly(1) || Orderly(-1) {
		t.Error("0 alone of 0, 1 and -1 is orderly everywhere")
	}
	if want := runtime.GOOS != "windows"; Orderly(130) != want {
		t.Errorf("Orderly(130) = %v on %s, want %v", Orderly(130), runtime.GOOS, want)
	}
}

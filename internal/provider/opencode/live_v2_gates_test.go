//go:build live_opencode

package opencode_test

// The facts MADR 0214's gates established against opencode v2, pinned (PLAN
// P3, amendment B). Each test drives opencode directly: `opencode acp` over
// JSON-RPC and `opencode serve --stdio` over HTTP, in an isolated home with
// every credential-shaped variable removed (PLAN C4 as amended). None needs a
// credential. G2's load and G5 send free-model turns; the rest send no prompt.
//
// Run with: go test -tags live_opencode ./internal/provider/opencode/ -run LiveV2 -count=1 -v

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// v2FreeModel is the model G1, G2 and G5 ran on.
const v2FreeModel = "opencode/deepseek-v4.1-flash-free"

var v2Credential = regexp.MustCompile(`(?i)(_API_KEY|_TOKEN|_SECRET|_PASSWORD)$`)

// v2Home is an isolated home: its environment and a project directory in it.
type v2Home struct {
	env     []string
	home    string
	project string
}

// newV2Home builds the environment PLAN C4 (as amended) requires and checks
// the engine is v2.
func newV2Home(t *testing.T) *v2Home {
	t.Helper()
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not on PATH")
	}
	home := t.TempDir()
	set := map[string]string{
		"HOME":                         home,
		"XDG_CONFIG_HOME":              filepath.Join(home, ".config"),
		"XDG_DATA_HOME":                filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME":               filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME":               filepath.Join(home, ".cache"),
		"OPENCODE_CONFIG_DIR":          filepath.Join(home, ".config", "opencode"),
		"OPENCODE_DB":                  filepath.Join(home, "opencode.db"),
		"OPENCODE_DISABLE_AUTOUPDATE":  "1",
		"OPENCODE_DISABLE_FILEWATCHER": "1",
	}
	if runtime.GOOS == "windows" {
		set["USERPROFILE"] = home
		set["APPDATA"] = filepath.Join(home, "AppData", "Roaming")
		set["LOCALAPPDATA"] = filepath.Join(home, "AppData", "Local")
	}
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if _, overridden := set[strings.ToUpper(name)]; overridden {
			continue
		}
		if v2Credential.MatchString(name) || strings.EqualFold(name, "BASH_ENV") {
			continue
		}
		env = append(env, kv)
	}
	for k, v := range set {
		env = append(env, k+"="+v)
	}
	h := &v2Home{env: env, home: home, project: filepath.Join(home, "project")}
	if err := os.MkdirAll(h.project, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := h.command(context.Background(), "--version").Output()
	if err != nil {
		t.Fatalf("opencode --version: %v", err)
	}
	if v := strings.TrimSpace(string(out)); !strings.HasPrefix(strings.TrimPrefix(v, "opencode "), "v2.") {
		t.Fatalf("opencode --version = %q; these tests pin opencode v2 (MADR 0214)", v)
	}
	return h
}

func (h *v2Home) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Env = h.env
	cmd.Dir = h.project
	return cmd
}

// v2Wait waits up to d for cmd to exit and returns its exit code, or fails.
func v2Wait(t *testing.T, cmd *exec.Cmd, d time.Duration) int {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		if err != nil {
			t.Fatalf("wait: %v", err)
		}
		return 0
	case <-time.After(d):
		_ = cmd.Process.Kill()
		t.Fatalf("the process did not exit within %s", d)
		return -1
	}
}

// v2Msg is one JSON-RPC message from `opencode acp`.
type v2Msg struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// update returns a session/update's kind, or "".
func (m v2Msg) update() (kind string, u map[string]any) {
	if m.Method != "session/update" {
		return "", nil
	}
	var p struct {
		Update map[string]any `json:"update"`
	}
	if json.Unmarshal(m.Params, &p) != nil {
		return "", nil
	}
	k, _ := p.Update["sessionUpdate"].(string)
	return k, p.Update
}

// v2Acp is an `opencode acp` process spoken to over its stdio.
type v2Acp struct {
	t    *testing.T
	cmd  *exec.Cmd
	in   io.WriteCloser
	msgs chan v2Msg
	wmu  sync.Mutex
	next int
}

func startV2Acp(t *testing.T, h *v2Home) *v2Acp {
	t.Helper()
	cmd := h.command(context.Background(), "acp")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	a := &v2Acp{t: t, cmd: cmd, in: in, msgs: make(chan v2Msg, 4096)}
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
		for sc.Scan() {
			var m v2Msg
			if json.Unmarshal(sc.Bytes(), &m) == nil {
				a.msgs <- m
			}
		}
		close(a.msgs)
	}()
	t.Cleanup(func() {
		_ = in.Close()
		_ = cmd.Process.Kill()
	})
	if _, err := a.call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}, 3*time.Minute); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return a
}

func (a *v2Acp) write(v any) {
	a.t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		a.t.Fatal(err)
	}
	a.wmu.Lock()
	defer a.wmu.Unlock()
	if _, err := a.in.Write(append(b, '\n')); err != nil {
		a.t.Fatalf("write: %v", err)
	}
}

// v2RPCError is an error answer.
type v2RPCError struct {
	Code    int
	Message string
}

func (e *v2RPCError) Error() string { return e.Message }

// call sends a request and returns its result.
func (a *v2Acp) call(method string, params any, d time.Duration) (json.RawMessage, error) {
	res, _, err := a.callSeen(method, params, d)
	return res, err
}

// callSeen is call that also returns every other message the engine sent
// before the answer. It answers a permission request with allow_once.
func (a *v2Acp) callSeen(method string, params any, d time.Duration) (json.RawMessage, []v2Msg, error) {
	a.t.Helper()
	a.next++
	id := a.next
	a.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	var seen []v2Msg
	deadline := time.After(d)
	for {
		select {
		case m, ok := <-a.msgs:
			if !ok {
				a.t.Fatalf("%s: the engine closed its stdout", method)
			}
			if m.Method == "" && string(bytes.TrimSpace(m.ID)) == itoa(id) {
				if m.Error != nil {
					return nil, seen, &v2RPCError{Code: m.Error.Code, Message: m.Error.Message}
				}
				return m.Result, seen, nil
			}
			if m.Method == "session/request_permission" && len(m.ID) > 0 {
				a.allowOnce(m)
				continue
			}
			seen = append(seen, m)
		case <-deadline:
			a.t.Fatalf("%s: no answer within %s", method, d)
		}
	}
}

func (a *v2Acp) allowOnce(m v2Msg) {
	var p struct {
		Options []struct {
			OptionID string `json:"optionId"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	_ = json.Unmarshal(m.Params, &p)
	pick := ""
	for _, o := range p.Options {
		if o.Kind == "allow_once" {
			pick = o.OptionID
		}
	}
	if pick == "" && len(p.Options) > 0 {
		pick = p.Options[0].OptionID
	}
	a.write(map[string]any{"jsonrpc": "2.0", "id": m.ID,
		"result": map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": pick}}})
}

// quiet returns every message the engine sends within d.
func (a *v2Acp) quiet(d time.Duration) []v2Msg {
	var out []v2Msg
	deadline := time.After(d)
	for {
		select {
		case m, ok := <-a.msgs:
			if !ok {
				return out
			}
			out = append(out, m)
		case <-deadline:
			return out
		}
	}
}

// close ends the lease and returns the exit code.
func (a *v2Acp) close() int {
	a.t.Helper()
	_ = a.in.Close()
	return v2Wait(a.t, a.cmd, 15*time.Second)
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// v2Session is session/new's answer: the id and the model option.
type v2Session struct {
	SessionID     string `json:"sessionId"`
	ConfigOptions []struct {
		ID       string `json:"id"`
		Category string `json:"category"`
		Options  []struct {
			Value string `json:"value"`
		} `json:"options"`
	} `json:"configOptions"`
}

func (s v2Session) models() (configID string, ids []string) {
	for _, o := range s.ConfigOptions {
		if o.Category == "model" {
			for _, v := range o.Options {
				ids = append(ids, v.Value)
			}
			return o.ID, ids
		}
	}
	return "", nil
}

func (a *v2Acp) newSession(cwd string) v2Session {
	a.t.Helper()
	res, err := a.call("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}}, 3*time.Minute)
	if err != nil {
		a.t.Fatalf("session/new: %v", err)
	}
	var s v2Session
	if err := json.Unmarshal(res, &s); err != nil || s.SessionID == "" {
		a.t.Fatalf("session/new answer %s: %v", res, err)
	}
	return s
}

func (a *v2Acp) setModel(sid, configID, model string) error {
	_, err := a.call("session/set_config_option", map[string]any{"sessionId": sid, "configId": configID, "value": model}, time.Minute)
	return err
}

// prompt runs one turn and returns its stop reason.
func (a *v2Acp) prompt(sid, text string) string {
	a.t.Helper()
	res, err := a.call("session/prompt", map[string]any{"sessionId": sid,
		"prompt": []any{map[string]any{"type": "text", "text": text}}}, 4*time.Minute)
	if err != nil {
		a.t.Fatalf("prompt: %v", err)
	}
	var r struct {
		StopReason string `json:"stopReason"`
	}
	_ = json.Unmarshal(res, &r)
	return r.StopReason
}

// v2Control is an `opencode serve --stdio --port 0` control plane.
type v2Control struct {
	t        *testing.T
	cmd      *exec.Cmd
	in       io.WriteCloser
	url      string
	ready    map[string]any
	password string
	dir      string
}

func startV2Control(t *testing.T, h *v2Home) *v2Control {
	t.Helper()
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	c := &v2Control{t: t, password: hex.EncodeToString(b), dir: h.project}
	cmd := h.command(context.Background(), "serve", "--stdio", "--port", "0")
	cmd.Env = append(append([]string(nil), h.env...), "OPENCODE_PASSWORD="+c.password)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c.cmd, c.in = cmd, in
	t.Cleanup(func() {
		_ = in.Close()
		_ = cmd.Process.Kill()
	})
	r := bufio.NewReader(out)
	line, err := r.ReadBytes('\n')
	if err != nil {
		t.Fatalf("ready line: %v", err)
	}
	if err := json.Unmarshal(line, &c.ready); err != nil {
		t.Fatalf("ready line %q is not JSON: %v", line, err)
	}
	c.url, _ = c.ready["url"].(string)
	// Drained after readiness, as standalone.ts does.
	go func() { _, _ = io.Copy(io.Discard, r) }()
	return c
}

func (c *v2Control) do(method, path string, body any, auth bool) (int, string, []byte) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.url+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	if auth {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("opencode:"+c.password)))
	}
	req.Header.Set("x-opencode-directory", c.dir)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Content-Type"), raw
}

// data returns v's "data" member when it has one.
func data(raw []byte) any {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		if d, ok := m["data"]; ok {
			return d
		}
	}
	return v
}

func (c *v2Control) close() int {
	c.t.Helper()
	_ = c.in.Close()
	return v2Wait(c.t, c.cmd, 15*time.Second)
}

// messages returns the session's messages as role-or-type and id, oldest first.
func (c *v2Control) messages(sid string) [][2]string {
	c.t.Helper()
	st, _, raw := c.do("GET", "/api/session/"+sid+"/message", nil, true)
	if st != http.StatusOK {
		c.t.Fatalf("GET messages: %d %s", st, raw)
	}
	items, _ := data(raw).([]any)
	if m, ok := data(raw).(map[string]any); ok {
		items, _ = m["items"].([]any)
	}
	var out [][2]string
	for _, it := range items {
		m, _ := it.(map[string]any)
		if info, ok := m["info"].(map[string]any); ok {
			m = info
		}
		kind, _ := m["role"].(string)
		if kind == "" {
			kind, _ = m["type"].(string)
		}
		id, _ := m["id"].(string)
		out = append(out, [2]string{kind, id})
	}
	sort.Slice(out, func(i, j int) bool { return out[i][1] < out[j][1] })
	return out
}

// TestLiveV2ServeStdioLease is G3 (MADR 0214 B5; D2, C5).
func TestLiveV2ServeStdioLease(t *testing.T) {
	h := newV2Home(t)
	c := startV2Control(t, h)
	if len(c.ready) != 1 || !strings.HasPrefix(c.url, "http://127.0.0.1:") {
		t.Fatalf("ready line = %v, want only a loopback url", c.ready)
	}
	st, ct, raw := c.do("GET", "/api/info", nil, true)
	info, _ := data(raw).(map[string]any)
	if v, _ := info["version"].(string); st != http.StatusOK || !strings.HasPrefix(ct, "application/json") || !strings.HasPrefix(v, "2.") {
		t.Fatalf("/api/info = %d %q version %q, want 200 JSON naming 2.x", st, ct, info["version"])
	}
	if st, _, _ := c.do("GET", "/api/info", nil, false); st != http.StatusUnauthorized {
		t.Fatalf("/api/info without the password = %d, want 401", st)
	}
	st, _, raw = c.do("POST", "/api/session", map[string]any{"location": map[string]any{"directory": h.project}}, true)
	sess, _ := data(raw).(map[string]any)
	sid, _ := sess["id"].(string)
	if st != http.StatusOK || sid == "" {
		t.Fatalf("POST /api/session = %d %s", st, raw)
	}
	if st, _, raw := c.do("POST", "/api/session/"+sid+"/shell", map[string]any{"command": "env > env.txt"}, true); st != http.StatusNoContent {
		t.Fatalf("shell = %d %s", st, raw)
	}
	var env []byte
	for i := 0; i < 40 && len(env) == 0; i++ {
		env, _ = os.ReadFile(filepath.Join(h.project, "env.txt"))
		time.Sleep(250 * time.Millisecond)
	}
	if len(env) == 0 {
		t.Fatal("the shell wrote no env.txt")
	}
	for _, leak := range []string{c.password, "OPENCODE_PASSWORD", "OPENCODE_SERVER_PASSWORD"} {
		if bytes.Contains(env, []byte(leak)) {
			t.Errorf("a shell the server ran sees %s", map[bool]string{true: "the password", false: leak}[leak == c.password])
		}
	}
	if code := c.close(); code != 0 {
		t.Errorf("lease close: exit %d, want 0", code)
	}
	if runtime.GOOS == "windows" {
		t.Log("SIGTERM is POSIX only; a Windows forced stop exits 1 (MADR 0214 B5)")
		return
	}
	c = startV2Control(t, h)
	if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if code := v2Wait(t, c.cmd, 15*time.Second); code != 130 {
		t.Errorf("SIGTERM: exit %d, want 130", code)
	}
}

// TestLiveV2AcpLease is G3 for `opencode acp` (B5).
func TestLiveV2AcpLease(t *testing.T) {
	h := newV2Home(t)
	a := startV2Acp(t, h)
	a.newSession(h.project)
	if code := a.close(); code != 0 {
		t.Errorf("lease close: exit %d, want 0", code)
	}
}

// TestLiveV2RemovedCredentialStaysInRunningEngine is G4 (B6), the reason for
// D8's restart: after the control plane removes a credential, the running ACP
// engine still accepts the removed provider's model, and a fresh engine does
// not list it. When this fails, opencode has fixed the staleness and D8's
// restart can be retired.
func TestLiveV2RemovedCredentialStaysInRunningEngine(t *testing.T) {
	h := newV2Home(t)
	running := startV2Acp(t, h)
	// The catalog loads, then settles once the engine's own provider
	// discovery has finished (catalog.ts:128 at b44b7164ba).
	running.newSession(h.project)
	time.Sleep(20 * time.Second)
	c := startV2Control(t, h)
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	st, _, raw := c.do("POST", "/api/credential", map[string]any{"integrationID": "openrouter", "label": "pin-throwaway",
		"value": map[string]any{"type": "key", "key": "sk-or-v1-" + hex.EncodeToString(b)}, "activate": true}, true)
	cred, _ := data(raw).(map[string]any)
	credID, _ := cred["id"].(string)
	if st != http.StatusOK || credID == "" {
		t.Fatalf("POST /api/credential = %d, id %q", st, credID)
	}
	// An engine started after the add lists the provider; take a model from it.
	added := startV2Acp(t, h)
	_, ids := added.newSession(h.project).models()
	model := ""
	for _, id := range ids {
		if strings.HasPrefix(id, "openrouter/") {
			model = id
			break
		}
	}
	if model == "" {
		t.Fatalf("an engine started after the add lists no openrouter model (%d models)", len(ids))
	}
	added.close()
	// No credential event crosses processes, so a settled engine does not
	// list the added provider. Selecting its model misses the cache, which
	// reloads it (service.ts:80-85).
	s := running.newSession(h.project)
	configID, listed := s.models()
	if slices.Contains(listed, model) {
		t.Fatalf("the settled running engine lists %s right after the add: the add now reaches it, "+
			"so D8's restart after an add may be retired (MADR 0214 B6)", model)
	}
	if err := running.setModel(s.SessionID, configID, model); err != nil {
		t.Fatalf("the running engine refused %s after the add: %v", model, err)
	}

	if st, _, _ := c.do("DELETE", "/api/credential/"+credID, nil, true); st != http.StatusNoContent {
		t.Fatalf("DELETE credential = %d", st)
	}
	time.Sleep(10 * time.Second)
	s = running.newSession(h.project)
	if err := running.setModel(s.SessionID, configID, model); err != nil {
		t.Fatalf("the running engine refused %s after the removal: opencode now clears a removed provider, "+
			"so D8's restart can be retired (MADR 0214 B6): %v", model, err)
	}
	fresh := startV2Acp(t, h)
	_, ids = fresh.newSession(h.project).models()
	for _, id := range ids {
		if strings.HasPrefix(id, "openrouter/") {
			t.Fatalf("a fresh engine still lists %s after the removal", id)
		}
	}
}

// TestLiveV2ResumeReplaysNoHistory is G2 for session/resume (B4): the answer
// carries configOptions, no history is replayed (the commands and config
// option updates a new session also gets may follow), and another cwd is
// refused.
func TestLiveV2ResumeReplaysNoHistory(t *testing.T) {
	h := newV2Home(t)
	a := startV2Acp(t, h)
	s := a.newSession(h.project)
	a.close()

	b := startV2Acp(t, h)
	res, seen, err := b.callSeen("session/resume", map[string]any{"sessionId": s.SessionID, "cwd": h.project, "mcpServers": []any{}}, 3*time.Minute)
	if err != nil {
		t.Fatalf("session/resume: %v", err)
	}
	var r map[string]json.RawMessage
	_ = json.Unmarshal(res, &r)
	if _, ok := r["configOptions"]; !ok {
		t.Errorf("session/resume answered %s, want configOptions", res)
	}
	for _, m := range append(seen, b.quiet(3*time.Second)...) {
		switch k, _ := m.update(); k {
		case "", "available_commands_update", "config_option_update", "session_info_update":
		default:
			t.Errorf("session/resume replayed a %s update", k)
		}
	}
	_, err = b.call("session/resume", map[string]any{"sessionId": s.SessionID, "cwd": h.home, "mcpServers": []any{}}, time.Minute)
	var re *v2RPCError
	if !errors.As(err, &re) || re.Code != -32602 {
		t.Errorf("resume with another cwd: %v, want -32602 (A5)", err)
	}
}

// TestLiveV2LoadReplaysPromptsAndNoUsage is G2 for session/load (B2, B3): a
// fresh engine replays the prompt as user_message_chunk, and no usage_update.
// One free-model turn.
func TestLiveV2LoadReplaysPromptsAndNoUsage(t *testing.T) {
	h := newV2Home(t)
	a := startV2Acp(t, h)
	s := a.newSession(h.project)
	configID, _ := s.models()
	if err := a.setModel(s.SessionID, configID, v2FreeModel); err != nil {
		t.Fatalf("set the free model: %v", err)
	}
	const prompt = "Reply with exactly one word: apple"
	if stop := a.prompt(s.SessionID, prompt); stop != "end_turn" {
		t.Fatalf("turn stopped with %q", stop)
	}
	a.close()

	b := startV2Acp(t, h)
	_, seen, err := b.callSeen("session/load", map[string]any{"sessionId": s.SessionID, "cwd": h.project, "mcpServers": []any{}}, 3*time.Minute)
	if err != nil {
		t.Fatalf("session/load: %v", err)
	}
	var user, agent, usage int
	for _, m := range seen {
		k, u := m.update()
		switch k {
		case "user_message_chunk":
			if c, _ := u["content"].(map[string]any); c["text"] == prompt {
				user++
			}
		case "agent_message_chunk":
			agent++
		case "usage_update":
			usage++
		}
	}
	if user != 1 || agent == 0 {
		t.Errorf("load replayed %d user_message_chunk with the prompt and %d agent chunks, want 1 and some", user, agent)
	}
	if usage != 0 {
		t.Errorf("load replayed %d usage_update(s); MADR 0214 D6 relies on none", usage)
	}
}

// TestLiveV2RevertAndShellFromControlPlane is G5 (B7): on an idle session the
// ACP engine owns, the control plane's revert/stage and revert/commit drop a
// turn's messages and session.shell runs its command, and the engine sends no
// update for either. Two free-model turns.
func TestLiveV2RevertAndShellFromControlPlane(t *testing.T) {
	h := newV2Home(t)
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = h.project
		cmd.Env = append(append([]string(nil), h.env...), "GIT_AUTHOR_NAME=pin", "GIT_AUTHOR_EMAIL=pin@example.invalid",
			"GIT_COMMITTER_NAME=pin", "GIT_COMMITTER_EMAIL=pin@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(h.project, "README.md"), []byte("# pin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("init", "-q")
	git("add", "README.md")
	git("commit", "-q", "-m", "init")

	a := startV2Acp(t, h)
	s := a.newSession(h.project)
	configID, _ := s.models()
	if err := a.setModel(s.SessionID, configID, v2FreeModel); err != nil {
		t.Fatalf("set the free model: %v", err)
	}
	for _, w := range []string{"apple", "banana"} {
		if stop := a.prompt(s.SessionID, "Reply with exactly one word: "+w); stop != "end_turn" {
			t.Fatalf("turn %s stopped with %q", w, stop)
		}
	}
	a.quiet(2 * time.Second)

	c := startV2Control(t, h)
	var users []string
	for _, m := range c.messages(s.SessionID) {
		if m[0] == "user" {
			users = append(users, m[1])
		}
	}
	if len(users) != 2 {
		t.Fatalf("%d user messages, want 2", len(users))
	}
	noUpdates := func(step string) {
		for _, m := range a.quiet(2 * time.Second) {
			if k, _ := m.update(); k != "" {
				t.Errorf("%s: the ACP engine sent a %s update", step, k)
			}
		}
	}
	if st, _, raw := c.do("POST", "/api/session/"+s.SessionID+"/revert/stage", map[string]any{"messageID": users[1], "files": true}, true); st != http.StatusOK {
		t.Fatalf("revert/stage = %d %s", st, raw)
	}
	noUpdates("revert/stage")
	if st, _, raw := c.do("POST", "/api/session/"+s.SessionID+"/revert/commit", nil, true); st != http.StatusNoContent {
		t.Fatalf("revert/commit = %d %s", st, raw)
	}
	noUpdates("revert/commit")
	kept := map[string]bool{}
	for _, m := range c.messages(s.SessionID) {
		kept[m[1]] = true
	}
	if !kept[users[0]] || kept[users[1]] {
		t.Errorf("after revert/commit the first turn kept %v, the second kept %v; want true, false", kept[users[0]], kept[users[1]])
	}
	if st, _, raw := c.do("POST", "/api/session/"+s.SessionID+"/shell", map[string]any{"command": "echo pinned > shell.txt"}, true); st != http.StatusNoContent {
		t.Fatalf("shell = %d %s", st, raw)
	}
	var out []byte
	for i := 0; i < 40 && len(out) == 0; i++ {
		out, _ = os.ReadFile(filepath.Join(h.project, "shell.txt"))
		time.Sleep(250 * time.Millisecond)
	}
	if strings.TrimSpace(string(out)) != "pinned" {
		t.Errorf("shell.txt = %q, want pinned", out)
	}
	noUpdates("session.shell")
	shell := false
	for _, m := range c.messages(s.SessionID) {
		shell = shell || m[0] == "shell"
	}
	if !shell {
		t.Error("session.shell added no shell message")
	}
}

//go:build live_kilo

package kilo_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	kilo7423Version = "7.4.23"
	probeTailBytes  = 64 << 10
)

var (
	ansiCSI    = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	listenLine = regexp.MustCompile(
		`^kilo server listening on (http://127\.0\.0\.1:[0-9]+)$`,
	)
)

type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > probeTailBytes {
		b.data = append([]byte(nil), b.data[len(b.data)-probeTailBytes:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data...)
}

func (b *tailBuffer) String() string {
	return string(b.bytes())
}

type isolatedKilo struct {
	root string
	env  []string
}

func newIsolatedKilo(t *testing.T) isolatedKilo {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"home", "config", "data", "state", "cache"} {
		if err := os.MkdirAll(pathJoin(root, dir), 0o700); err != nil {
			t.Fatalf("create isolated %s: %v", dir, err)
		}
	}

	remove := map[string]bool{
		"HOME":                        true,
		"XDG_CONFIG_HOME":             true,
		"XDG_DATA_HOME":               true,
		"XDG_STATE_HOME":              true,
		"XDG_CACHE_HOME":              true,
		"KILO_TEST_HOME":              true,
		"KILO_CONFIG":                 true,
		"KILO_CONFIG_DIR":             true,
		"KILO_CONFIG_CONTENT":         true,
		"KILO_AUTH_CONTENT":           true,
		"KILO_SERVER_USERNAME":        true,
		"KILO_SERVER_PASSWORD":        true,
		"KILO_DISABLE_PROJECT_CONFIG": true,
		"KILO_DISABLE_AUTOUPDATE":     true,
		"KILO_DISABLE_AUTOCOMPACT":    true,
		"KILO_DISABLE_MODELS_FETCH":   true,
		"KILO_PURE":                   true,
	}
	env := make([]string, 0, len(os.Environ())+12)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if !remove[key] {
			env = append(env, item)
		}
	}
	env = append(env,
		"HOME="+pathJoin(root, "home"),
		"XDG_CONFIG_HOME="+pathJoin(root, "config"),
		"XDG_DATA_HOME="+pathJoin(root, "data"),
		"XDG_STATE_HOME="+pathJoin(root, "state"),
		"XDG_CACHE_HOME="+pathJoin(root, "cache"),
		`KILO_CONFIG_CONTENT={"permission":{"*":"allow"}}`,
		"KILO_AUTH_CONTENT={}",
		"KILO_DISABLE_PROJECT_CONFIG=1",
		"KILO_DISABLE_AUTOUPDATE=1",
		"KILO_DISABLE_AUTOCOMPACT=1",
		"KILO_DISABLE_MODELS_FETCH=1",
		"KILO_PURE=1",
	)
	return isolatedKilo{root: root, env: env}
}

func pathJoin(base, elem string) string {
	return filepath.Join(base, elem)
}

func requireKilo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("kilo"); err != nil {
		t.Skip("kilo not in PATH")
	}
}

func (k isolatedKilo) command(args ...string) *exec.Cmd {
	cmd := exec.Command("kilo", args...)
	cmd.Dir = k.root
	cmd.Env = append([]string(nil), k.env...)
	return cmd
}

func (k isolatedKilo) run(t *testing.T, timeout time.Duration, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kilo", args...)
	cmd.Dir = k.root
	cmd.Env = append([]string(nil), k.env...)
	var stdout, stderr tailBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("kilo %s timed out after %s\nstdout:\n%s\nstderr:\n%s",
			strings.Join(args, " "), timeout, stdout.String(), stderr.String())
	}
	if err != nil {
		t.Fatalf("kilo %s: %v\nstdout:\n%s\nstderr:\n%s",
			strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.bytes()
}

type runningKiloServer struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdout tailBuffer
	stderr tailBuffer
	done   chan struct{}
	mu     sync.Mutex
	wait   error
	stop   sync.Once
}

func (k isolatedKilo) startServer(t *testing.T) (string, *runningKiloServer) {
	t.Helper()
	cmd := k.command("serve", "--hostname", "127.0.0.1", "--port", "0", "--pure")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("kilo serve stdout: %v", err)
	}
	s := &runningKiloServer{t: t, cmd: cmd, done: make(chan struct{})}
	cmd.Stderr = &s.stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start kilo serve: %v", err)
	}
	t.Cleanup(s.shutdown)

	urlCh := make(chan string, 1)
	scanDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			_, _ = s.stdout.Write(append([]byte(line), '\n'))
			clean := strings.TrimSuffix(ansiCSI.ReplaceAllString(line, ""), "\r")
			if m := listenLine.FindStringSubmatch(clean); m != nil {
				select {
				case urlCh <- m[1]:
				default:
				}
			}
		}
		scanDone <- scanner.Err()
	}()
	go func() {
		err := cmd.Wait()
		s.mu.Lock()
		s.wait = err
		s.mu.Unlock()
		close(s.done)
	}()

	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case url := <-urlCh:
		return url, s
	case err := <-scanDone:
		t.Fatalf("kilo serve ended before its listening line: %v\nstdout:\n%s\nstderr:\n%s",
			err, s.stdout.String(), s.stderr.String())
	case <-s.done:
		s.mu.Lock()
		waitErr := s.wait
		s.mu.Unlock()
		t.Fatalf("kilo serve exited before its listening line: %v\nstdout:\n%s\nstderr:\n%s",
			waitErr, s.stdout.String(), s.stderr.String())
	case <-timer.C:
		t.Fatalf("kilo serve produced no listening line in 30s\nstdout:\n%s\nstderr:\n%s",
			s.stdout.String(), s.stderr.String())
	}
	return "", s
}

func (s *runningKiloServer) shutdown() {
	s.stop.Do(func() {
		select {
		case <-s.done:
			return
		default:
		}
		_ = s.cmd.Process.Signal(os.Interrupt)
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-s.done:
			return
		case <-timer.C:
			_ = s.cmd.Process.Kill()
			<-s.done
			s.t.Errorf("kilo serve did not stop within 5s of interrupt\nstdout:\n%s\nstderr:\n%s",
				s.stdout.String(), s.stderr.String())
		}
	})
}

func getJSON(t *testing.T, baseURL, endpoint string, dst any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+endpoint, nil)
	if err != nil {
		t.Fatalf("build GET %s: %v", endpoint, err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", endpoint, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		t.Fatalf("GET %s: status %d: %s", endpoint, res.StatusCode, body)
	}
	dec := json.NewDecoder(io.LimitReader(res.Body, 32<<20))
	if err := dec.Decode(dst); err != nil {
		t.Fatalf("decode GET %s: %v", endpoint, err)
	}
}

type openAPIDocument struct {
	Paths      map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]json.RawMessage `json:"schemas"`
	} `json:"components"`
}

func canonicalEventTypes(t *testing.T, doc openAPIDocument) []string {
	t.Helper()
	var event struct {
		AnyOf []struct {
			Ref string `json:"$ref"`
		} `json:"anyOf"`
	}
	if err := json.Unmarshal(doc.Components.Schemas["Event"], &event); err != nil {
		t.Fatalf("decode Event schema: %v", err)
	}
	types := make(map[string]bool)
	for _, member := range event.AnyOf {
		name := member.Ref[strings.LastIndex(member.Ref, "/")+1:]
		raw, ok := doc.Components.Schemas[name]
		if !ok {
			t.Fatalf("Event member %q not found", name)
		}
		var schema struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("decode Event member %q: %v", name, err)
		}
		values := schema.Properties["type"].Enum
		if len(values) != 1 {
			t.Fatalf("Event member %q has %d top-level type values, want 1", name, len(values))
		}
		types[values[0]] = true
	}
	out := make([]string, 0, len(types))
	for typ := range types {
		out = append(out, typ)
	}
	sort.Strings(out)
	return out
}

func TestLiveKilo7423Surface(t *testing.T) {
	requireKilo(t)
	k := newIsolatedKilo(t)
	if got := strings.TrimSpace(string(k.run(t, 30*time.Second, "--version"))); got != kilo7423Version {
		t.Fatalf("kilo --version = %q, want %q", got, kilo7423Version)
	}

	baseURL, server := k.startServer(t)
	defer server.shutdown()
	var health struct {
		Healthy bool   `json:"healthy"`
		Version string `json:"version"`
	}
	getJSON(t, baseURL, "/global/health", &health)
	if !health.Healthy || health.Version != kilo7423Version {
		t.Fatalf("health = %+v, want healthy 7.4.23", health)
	}

	var doc openAPIDocument
	getJSON(t, baseURL, "/doc", &doc)
	events := canonicalEventTypes(t, doc)
	if len(doc.Paths) != 255 || len(doc.Components.Schemas) != 680 || len(events) != 119 {
		t.Fatalf("OpenAPI paths/schemas/events = %d/%d/%d, want 255/680/119",
			len(doc.Paths), len(doc.Components.Schemas), len(events))
	}
	if _, ok := doc.Paths["/kilocode/agent/requirements"]; ok {
		t.Error("removed /kilocode/agent/requirements path is still present")
	}
	if _, ok := doc.Components.Schemas["AgentRequirementResult"]; ok {
		t.Error("removed AgentRequirementResult schema is still present")
	}

	eventSet := make(map[string]bool, len(events))
	for _, typ := range events {
		eventSet[typ] = true
	}
	required := []string{
		"message.updated", "message.part.delta", "message.part.updated", "message.part.removed",
		"permission.asked", "permission.v2.asked", "permission.replied", "permission.v2.replied",
		"question.asked", "question.v2.asked", "question.replied", "question.v2.replied",
		"question.rejected", "question.v2.rejected", "session.diff", "session.created",
		"session.updated", "session.deleted", "session.status", "session.idle",
		"session.turn.open", "session.turn.close", "session.error", "session.next.prompted",
		"session.next.prompt.admitted",
	}
	for _, typ := range required {
		if !eventSet[typ] {
			t.Errorf("required Event discriminator %q is absent", typ)
		}
	}
}

type acpMatch struct {
	result json.RawMessage
	err    error
}

func (k isolatedKilo) probeACP(t *testing.T) json.RawMessage {
	t.Helper()
	cmd := k.command("acp", "--hostname", "127.0.0.1", "--port", "0", "--cwd", k.root)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("kilo acp stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("kilo acp stdout: %v", err)
	}
	var stdoutTail, stderrTail tailBuffer
	cmd.Stderr = &stderrTail
	if err := cmd.Start(); err != nil {
		t.Fatalf("start kilo acp: %v", err)
	}
	request := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{},"clientInfo":{"name":"mcremote-0108-probe","version":"1"}}}`
	if _, err := io.WriteString(stdin, request+"\n"); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("write kilo acp initialize: %v", err)
	}
	if err := stdin.Close(); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("close kilo acp stdin: %v", err)
	}

	matches := make(chan acpMatch, 4)
	scanDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Bytes()
			_, _ = stdoutTail.Write(append(append([]byte(nil), line...), '\n'))
			var obj map[string]json.RawMessage
			if json.Unmarshal(line, &obj) != nil {
				continue
			}
			if string(obj["jsonrpc"]) != `"2.0"` || string(obj["id"]) != "1" {
				continue
			}
			match := acpMatch{result: append(json.RawMessage(nil), obj["result"]...)}
			switch {
			case len(obj["error"]) > 0 && string(obj["error"]) != "null":
				match.err = fmt.Errorf("ACP initialize error: %s", obj["error"])
			case len(match.result) == 0 || string(match.result) == "null":
				match.err = fmt.Errorf("ACP initialize response has no result")
			}
			matches <- match
		}
		scanDone <- scanner.Err()
	}()
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	responseTimer := time.NewTimer(30 * time.Second)
	defer responseTimer.Stop()
	var found []acpMatch
	var processErr, scannerErr error
	processDone, scannerDone := false, false
	var exitTimer *time.Timer
	for !processDone || !scannerDone {
		var exitC <-chan time.Time
		if exitTimer != nil {
			exitC = exitTimer.C
		}
		select {
		case match := <-matches:
			found = append(found, match)
			if len(found) == 1 {
				exitTimer = time.NewTimer(5 * time.Second)
			}
		case processErr = <-waitDone:
			processDone = true
		case scannerErr = <-scanDone:
			scannerDone = true
		case <-responseTimer.C:
			_ = cmd.Process.Kill()
			if !processDone {
				processErr = <-waitDone
			}
			t.Fatalf("no ACP initialize response in 30s: %v\nstdout:\n%s\nstderr:\n%s",
				processErr, stdoutTail.String(), stderrTail.String())
		case <-exitC:
			_ = cmd.Process.Kill()
			if !processDone {
				processErr = <-waitDone
			}
			t.Fatalf("kilo acp did not exit within 5s of its response: %v\nstdout:\n%s\nstderr:\n%s",
				processErr, stdoutTail.String(), stderrTail.String())
		}
		if processDone && scannerDone && len(found) == 0 {
			break
		}
	}
	if exitTimer != nil {
		exitTimer.Stop()
	}
	if scannerErr != nil {
		t.Fatalf("scan kilo acp stdout: %v", scannerErr)
	}
	if processErr != nil {
		t.Fatalf("kilo acp exited non-zero: %v\nstdout:\n%s\nstderr:\n%s",
			processErr, stdoutTail.String(), stderrTail.String())
	}
	if len(found) != 1 {
		t.Fatalf("matching ACP initialize responses = %d, want 1\nstdout:\n%s",
			len(found), stdoutTail.String())
	}
	if found[0].err != nil {
		t.Fatal(found[0].err)
	}
	return found[0].result
}

func TestLiveKilo7423ACPInitialize(t *testing.T) {
	requireKilo(t)
	result := newIsolatedKilo(t).probeACP(t)
	var got struct {
		ProtocolVersion   int `json:"protocolVersion"`
		AgentCapabilities struct {
			LoadSession     bool `json:"loadSession"`
			MCPCapabilities struct {
				HTTP bool `json:"http"`
				SSE  bool `json:"sse"`
			} `json:"mcpCapabilities"`
			PromptCapabilities struct {
				EmbeddedContext bool `json:"embeddedContext"`
				Image           bool `json:"image"`
			} `json:"promptCapabilities"`
			SessionCapabilities map[string]json.RawMessage `json:"sessionCapabilities"`
		} `json:"agentCapabilities"`
		AuthMethods []struct {
			ID string `json:"id"`
		} `json:"authMethods"`
		AgentInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"agentInfo"`
	}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("decode ACP result: %v", err)
	}
	if got.ProtocolVersion != 1 || !got.AgentCapabilities.LoadSession ||
		!got.AgentCapabilities.MCPCapabilities.HTTP || !got.AgentCapabilities.MCPCapabilities.SSE ||
		!got.AgentCapabilities.PromptCapabilities.EmbeddedContext ||
		!got.AgentCapabilities.PromptCapabilities.Image {
		t.Fatalf("unexpected ACP capabilities: %+v", got.AgentCapabilities)
	}
	for _, capability := range []string{"close", "fork", "list", "resume"} {
		if _, ok := got.AgentCapabilities.SessionCapabilities[capability]; !ok {
			t.Errorf("ACP session capability %q absent", capability)
		}
	}
	if len(got.AuthMethods) != 1 || got.AuthMethods[0].ID != "kilo-login" {
		t.Errorf("ACP auth method ids = %+v, want kilo-login", got.AuthMethods)
	}
	if got.AgentInfo.Name != "Kilo" || got.AgentInfo.Version != kilo7423Version {
		t.Errorf("ACP agent info = %+v, want Kilo 7.4.23", got.AgentInfo)
	}
}

type permissionRule struct {
	Permission string `json:"permission"`
	Pattern    string `json:"pattern"`
	Action     string `json:"action"`
}

type debugAgent struct {
	Name       string           `json:"name"`
	Mode       string           `json:"mode"`
	Native     bool             `json:"native"`
	Hidden     *bool            `json:"hidden"`
	Tools      map[string]bool  `json:"tools"`
	Permission []permissionRule `json:"permission"`
}

func (k isolatedKilo) debugAgent(t *testing.T, name string) debugAgent {
	t.Helper()
	var agent debugAgent
	if err := json.Unmarshal(k.run(t, 30*time.Second, "debug", "agent", name), &agent); err != nil {
		t.Fatalf("decode kilo debug agent %s: %v", name, err)
	}
	return agent
}

func effectivePermission(t *testing.T, rules []permissionRule, permission, value string) string {
	t.Helper()
	action := ""
	for _, rule := range rules {
		if rule.Permission != "*" && rule.Permission != permission {
			continue
		}
		matched, err := path.Match(rule.Pattern, value)
		if err != nil {
			t.Fatalf("invalid permission pattern %q: %v", rule.Pattern, err)
		}
		if matched {
			action = rule.Action
		}
	}
	return action
}

func assertPermission(t *testing.T, agent debugAgent, permission, value, want string) {
	t.Helper()
	if got := effectivePermission(t, agent.Permission, permission, value); got != want {
		t.Errorf("%s permission %s(%q) = %q, want %q", agent.Name, permission, value, got, want)
	}
}

func assertTools(t *testing.T, agent debugAgent, want bool, names ...string) {
	t.Helper()
	for _, name := range names {
		if got := agent.Tools[name]; got != want {
			t.Errorf("%s tool %q enabled = %v, want %v", agent.Name, name, got, want)
		}
	}
}

func TestLiveKilo7423ReadOnlyAgentBoundaries(t *testing.T) {
	requireKilo(t)
	k := newIsolatedKilo(t)
	code := k.debugAgent(t, "code")
	ask := k.debugAgent(t, "ask")
	plan := k.debugAgent(t, "plan")

	assertTools(t, code, true, "read", "bash", "edit", "write", "task")
	assertPermission(t, code, "read", "README.md", "allow")
	assertPermission(t, code, "bash", "python mutate.py", "allow")
	assertPermission(t, code, "edit", "main.go", "allow")
	assertPermission(t, code, "write", "notes.txt", "allow")
	assertPermission(t, code, "task", "explore", "allow")

	assertTools(t, ask, true, "read")
	assertTools(t, ask, false, "edit", "write", "task", "interactive_terminal")
	assertPermission(t, ask, "read", "README.md", "allow")
	assertPermission(t, ask, "bash", "python mutate.py", "deny")
	assertPermission(t, ask, "write", "notes.txt", "deny")
	assertPermission(t, ask, "edit", "main.go", "deny")
	assertPermission(t, ask, "task", "explore", "deny")
	assertPermission(t, ask, "interactive_terminal", "shell", "deny")

	// Plan keeps mutation tools registered so plan-file edits can be allowed,
	// while its ordered rules deny arbitrary edits and every write.
	assertTools(t, plan, true, "read", "edit", "write", "task")
	assertTools(t, plan, false, "interactive_terminal")
	assertPermission(t, plan, "read", "README.md", "allow")
	assertPermission(t, plan, "bash", "python mutate.py", "deny")
	assertPermission(t, plan, "write", "notes.txt", "deny")
	assertPermission(t, plan, "edit", "main.go", "deny")
	assertPermission(t, plan, "edit", "plans/implementation.md", "allow")
	assertPermission(t, plan, "task", "explore", "allow")
	assertPermission(t, plan, "task", "general", "deny")
	assertPermission(t, plan, "interactive_terminal", "shell", "deny")
}

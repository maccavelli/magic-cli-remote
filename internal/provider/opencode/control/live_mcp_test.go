//go:build live_opencode

package control

// The 0230 MADR pin, against opencode v2 in an isolated home: the control
// plane starts none of the user's MCP servers, whether a config file or a
// plugin added them, while a plain `opencode serve` starts both. No prompt is
// sent and nothing leaves the host.
//
// Run with: go test -tags live_opencode ./internal/provider/opencode/control/ -run LiveV2ControlPlane -count=1 -v

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// liveCredential matches variables kept out of the isolated home.
var liveCredential = regexp.MustCompile(`(?i)(_API_KEY|_TOKEN|_SECRET|_PASSWORD)$`)

// liveHome is an isolated OpenCode home: its environment, its project, and
// the file the fake MCP servers write to.
type liveHome struct {
	env     []string
	home    string
	project string
	marker  string
}

func newLiveHome(t *testing.T) liveHome {
	t.Helper()
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not on PATH")
	}
	home := t.TempDir()
	h := liveHome{home: home, project: filepath.Join(home, "project"), marker: filepath.Join(home, "mcp-started.txt")}
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
		"OPENCODE_CONFIG":              "",
		"OPENCODE_CONFIG_CONTENT":      "",
	}
	if runtime.GOOS == "windows" {
		set["USERPROFILE"] = home
		set["APPDATA"] = filepath.Join(home, "AppData", "Roaming")
		set["LOCALAPPDATA"] = filepath.Join(home, "AppData", "Local")
	}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if _, overridden := set[strings.ToUpper(name)]; overridden {
			continue
		}
		if liveCredential.MatchString(name) || strings.EqualFold(name, "BASH_ENV") {
			continue
		}
		h.env = append(h.env, kv)
	}
	for k, v := range set {
		h.env = append(h.env, k+"="+v)
	}
	// The plane starts its child in the user's home, so the test's home is
	// made the one this process sees too: the real home is never a location.
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}

	cfg := filepath.Join(home, ".config", "opencode")
	for _, dir := range []string{h.project, filepath.Join(cfg, "plugins")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	server := func(tag string) map[string]any {
		return map[string]any{"type": "local", "command": []string{os.Args[0], tag},
			"environment": map[string]string{"FAKE_MCP_MARKER": h.marker}}
	}
	conf, _ := json.Marshal(map[string]any{"mcp": map[string]any{"servers": map[string]any{"marker": server("config")}}})
	if err := os.WriteFile(filepath.Join(cfg, "opencode.json"), conf, 0o600); err != nil {
		t.Fatal(err)
	}
	added, _ := json.Marshal(server("plugin"))
	plugin := "export default { id: 'probe.mcp', async setup(ctx) { await ctx.mcp.transform((editor) => {" +
		" if (!editor.get('plugin-marker')) editor.set('plugin-marker', " + string(added) + ") }) } }\n"
	if err := os.WriteFile(filepath.Join(cfg, "plugins", "probe-mcp.js"), []byte(plugin), 0o600); err != nil {
		t.Fatal(err)
	}
	return h
}

// started is what the fake MCP servers wrote, as a set of tags.
func (h liveHome) started() map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(h.marker)
	if err != nil {
		return out
	}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		out[strings.TrimSpace(sc.Text())] = true
	}
	return out
}

// TestLiveV2ControlPlaneStartsNoMCPServers is the 0230 MADR D4 pin.
func TestLiveV2ControlPlaneStartsNoMCPServers(t *testing.T) {
	h := newLiveHome(t)

	// The control: a plain serve, given the call P6b makes first, starts both
	// servers. Without this the case below could pass by seeing nothing.
	password, err := newPassword()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("opencode", "serve", "--stdio", "--port", "0")
	cmd.Dir = h.home
	cmd.Env = append(append([]string(nil), h.env...), passwordEnv+"="+password)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var ready struct {
		URL string `json:"url"`
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || json.Unmarshal([]byte(line), &ready) != nil {
		t.Fatalf("plain serve: no ready line: %q %v", line, err)
	}
	req, _ := http.NewRequest(http.MethodGet, strings.TrimRight(ready.URL, "/")+"/api/integration", nil)
	req.Header.Set("Authorization", basicAuth(password))
	if res, err := http.DefaultClient.Do(req); err != nil {
		t.Fatalf("plain serve: %v", err)
	} else {
		_ = res.Body.Close()
	}
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		if s := h.started(); s["config"] && s["plugin"] {
			break
		}
	}
	_ = stdin.Close()
	_ = cmd.Wait()
	if s := h.started(); !s["config"] || !s["plugin"] {
		t.Fatalf("a plain serve started %v; want both servers, or this test proves nothing", s)
	}
	if err := os.Remove(h.marker); err != nil {
		t.Fatal(err)
	}

	// The case: the control plane, through the production path, at the routes
	// P6b and P6d call.
	p := New(Config{Bin: "opencode", MinVersion: "2.1.0", UpgradeURL: "https://opencode.invalid/install", Env: h.env})
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	calls := []Request{
		{Method: http.MethodGet, Path: "/api/integration"},
		{Method: http.MethodGet, Path: "/api/integration", Directory: h.project},
		{Method: http.MethodGet, Path: "/api/skill", Directory: h.project},
		{Method: http.MethodGet, Path: "/api/plugin", Directory: h.project},
	}
	for _, c := range calls {
		res, err := p.Do(ctx, c)
		if err != nil || res.Status != http.StatusOK {
			t.Fatalf("%s in %q: %d %v", c.Path, c.Directory, res.Status, err)
		}
	}
	// The plugin that adds a server is loaded: otherwise its server's absence
	// would prove nothing.
	res, err := p.Do(ctx, Request{Method: http.MethodGet, Path: "/api/plugin", Directory: h.project})
	if err != nil || !strings.Contains(string(res.Body), `"probe.mcp"`) {
		t.Fatalf("the probe plugin is not listed: %v %s", err, res.Body)
	}
	time.Sleep(5 * time.Second)
	if s := h.started(); len(s) != 0 {
		t.Errorf("the control plane started %v; want none of the user's MCP servers", s)
	}
}

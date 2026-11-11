//go:build live_gobble

package gobble

// Live gobble suite (MADR 0179 D12's live list). It runs the installed gobble
// in a scratch HOME and GOBBLE_HOME and spends quota, so it runs only on the
// owner's word (0179 PLAN C10), with the model the owner names:
//
//	MCREMOTE_LIVE_GOBBLE_MODEL=<upstream>/<model>  the model, and the upstream it belongs to
//	MCREMOTE_LIVE_GOBBLE_API_KEY=<key>             that upstream's key; never printed or logged
//	MCREMOTE_LIVE_GOBBLE_BIN=<path>                optional; gobble on PATH by default
//
// Run with: make live-gobble
//
// The scratch gobble's settings.yaml sets the named model as gobble's default,
// so no turn reaches another model before the switch, and a small
// compaction.keepRecentTokens, so /compact has something to compact.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
	"github.com/maccavelli/magic-cli-remote/internal/wirecap"
)

// liveEnv is one scratch gobble: its home, the work directory a session runs
// in, a wrapper that logs each argv before running the real gobble, and the
// owner's model and key.
type liveEnv struct {
	upstream, model, key string
	bin, argvLog, work   string
	// captureHost is the wirecap tag a capture must record: this host's, kept
	// through the HOME swap, or "" when this host has no key.
	captureHost string
}

// newLiveEnv builds the scratch gobble, or skips when the owner named no
// model or key.
func newLiveEnv(t *testing.T) liveEnv {
	t.Helper()
	model, key := os.Getenv("MCREMOTE_LIVE_GOBBLE_MODEL"), os.Getenv("MCREMOTE_LIVE_GOBBLE_API_KEY")
	upstream, name, ok := strings.Cut(model, "/")
	if !ok || upstream == "" || name == "" || key == "" {
		t.Skip("set MCREMOTE_LIVE_GOBBLE_MODEL=<upstream>/<model> and MCREMOTE_LIVE_GOBBLE_API_KEY; this suite spends quota")
	}
	real := os.Getenv("MCREMOTE_LIVE_GOBBLE_BIN")
	if real == "" {
		real = "gobble"
	}
	real, err := exec.LookPath(real)
	if err != nil {
		t.Fatalf("gobble: %v", err)
	}
	home := t.TempDir()
	gobbleHome := filepath.Join(home, ".gobble")
	if err := os.MkdirAll(gobbleHome, 0o700); err != nil {
		t.Fatal(err)
	}
	settings := "defaultProvider: " + upstream + "\ndefaultModel: " + name + "\ncompaction:\n  keepRecentTokens: 1000\n"
	if err := os.WriteFile(filepath.Join(gobbleHome, "settings.yaml"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(home, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "notes.txt"), []byte("live gobble notes\nsecond line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Before HOME moves, so a capture still records this host (MADR 0151 D12):
	// the 0.4.0 fixture, taken without this, named no capture host and failed
	// on a test host whose name is an ordinary word (0179 PLAN, P3 deviation of
	// 2026-11-10).
	captureHost := keepWirecapHostKey(t)
	t.Setenv("HOME", home)
	t.Setenv("GOBBLE_HOME", gobbleHome)
	argvLog := filepath.Join(home, "argv.log")
	bin := testexec.WriteShellStub(t, filepath.Join(home, "bin", "gobble"),
		`printf '%s\n' "$*" >> '`+argvLog+`'`+"\nexec '"+real+`' "$@"`)
	return liveEnv{upstream: upstream, model: model, key: key, bin: bin, argvLog: argvLog, work: work,
		captureHost: captureHost}
}

// argv is every gobble command line the wrapper saw.
func (e liveEnv) argv(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(e.argvLog)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// upstreamStatus is env's upstream's status in p's auth state.
func (e liveEnv) upstreamStatus(t *testing.T, p *acpagent.Provider) string {
	t.Helper()
	st, err := p.AuthStatus(context.Background())
	if err != nil {
		t.Fatalf("AuthStatus: %v", err)
	}
	for _, u := range st.Upstreams {
		if u.ID == e.upstream {
			return u.Status
		}
	}
	t.Fatalf("AuthStatus lists no %s", e.upstream)
	return ""
}

// writeKey stores env's key as the phone does: provider.set_credential,
// through gobble's own CLI with the key on stdin (MADR 0179 D3).
func (e liveEnv) writeKey(t *testing.T, p *acpagent.Provider) {
	t.Helper()
	if got := e.upstreamStatus(t, p); got == provider.AuthConfigured {
		t.Fatalf("%s is already configured before the phone's write; unset its environment variable so the write is what makes it ready", e.upstream)
	}
	if err := p.SetCredential(context.Background(), e.upstream, e.upstream+methodAPI, e.key, nil); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
	if got := e.upstreamStatus(t, p); got != provider.AuthConfigured {
		t.Fatalf("%s status after the phone's write = %q, want %q", e.upstream, got, provider.AuthConfigured)
	}
}

// lockedBuffer is a log sink the provider's goroutines can share.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// D12: a phone key write makes the upstream ready and opens no session: the
// only gobble commands it runs are auth commands, and none carries the key.
func TestLiveGobblePhoneKeyWriteOpensNoSession(t *testing.T) {
	e := newLiveEnv(t)
	p := New(acpagent.Config{Bin: e.bin}, slog.New(slog.NewTextHandler(&lockedBuffer{}, nil)))
	e.writeKey(t, p)
	for _, line := range e.argv(t) {
		if strings.Contains(line, e.key) {
			t.Fatal("a gobble command line carried the key")
		}
		if args := strings.Fields(line); len(args) > 0 && args[0] != "auth" {
			t.Errorf("the key write ran `gobble %s`; want auth commands only, and no session", line)
		}
	}
}

// liveTurn waits for s's turn to end, approving any permission request, and
// returns the events it saw.
func liveTurn(t *testing.T, s provider.Session, name string) []event.Event {
	t.Helper()
	var evs []event.Event
	deadline := time.After(4 * time.Minute)
	for {
		select {
		case ev := <-s.Events():
			evs = append(evs, ev)
			switch ev.Type {
			case event.TypePermission:
				opt := ""
				for _, o := range ev.Options {
					if o.Kind == "allow_once" || opt == "" && o.Kind == "allow_always" {
						opt = o.OptionID
					}
				}
				if err := s.(provider.PermissionSession).RespondPermission(context.Background(), ev.PermissionID, opt, opt == "", "live"); err != nil {
					t.Errorf("%s: permission: %v", name, err)
				}
			case event.TypeError:
				t.Errorf("%s: error event: %s", name, ev.Error)
			case event.TypeTurnComplete:
				return evs
			}
		case <-deadline:
			t.Fatalf("%s: no turn_complete in 4 minutes", name)
		}
	}
}

// livePrompt sends text and waits for its turn.
func livePrompt(t *testing.T, s provider.Session, name, text string) []event.Event {
	t.Helper()
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: text}}); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return liveTurn(t, s, name)
}

// compactedNotice is the A2 handler's notice for a live completed compaction.
var compactedNotice = regexp.MustCompile(`^Context compacted \(manual\), from \d+ tokens\.$`)

// D12's live list on one session: authenticate after a stored key; the model
// and thinking switches by session/set_config_option, and the typed /model
// and /thinking passed through; the grouped catalog; a
// turn with a tool call; a phone rename and fork; /compact passed through and
// shown as a compaction with its notice; and a recorded connection with no
// fs/* or terminal/* request. The recorded connection's methods are logged for
// the PLAN's record.
func TestLiveGobbleSession(t *testing.T) {
	e := newLiveEnv(t)
	capDir := t.TempDir()
	t.Setenv(wirecap.EnvDir, capDir)
	logs := &lockedBuffer{}
	p := New(acpagent.Config{Bin: e.bin}, slog.New(slog.NewTextHandler(logs, nil)))
	e.writeKey(t, p)
	ctx := context.Background()

	s, err := p.Start(ctx, provider.StartOptions{CWD: e.work})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = s.Close(ctx)
		}
	}()
	if !strings.Contains(logs.String(), "acp authenticated") || !strings.Contains(logs.String(), "method_id="+e.upstream) {
		t.Fatalf("the handshake did not authenticate %s after the stored key; log:\n%s", e.upstream, logs.String())
	}

	if err := s.(provider.ModelSession).SetModel(ctx, e.model); err != nil {
		t.Fatalf("SetModel(%s): %v; stopping before any prompt", e.model, err)
	}
	if err := s.(provider.ThinkingSession).SetThinkingLevel(ctx, "low"); err != nil {
		t.Errorf("SetThinkingLevel(low): %v", err)
	}
	// The typed commands pass through to gobble as written (D9).
	livePrompt(t, s, "/model", "/model "+e.model)
	livePrompt(t, s, "/thinking", "/thinking low")

	cat, err := p.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	found := false
	for _, o := range cat.Options {
		if o.Group == "" {
			t.Errorf("catalog option %q has no group; gobble groups every model", o.ID)
		}
		found = found || o.ID == e.model
	}
	if !found {
		t.Errorf("the catalog does not list %s", e.model)
	}

	evs := livePrompt(t, s, "tool turn", "Use your file read tool to read notes.txt in the current directory, then reply with its first line only.")
	if !slices.ContainsFunc(evs, func(ev event.Event) bool { return ev.Type == event.TypeToolCall }) {
		t.Error("the tool turn produced no tool_call")
	}

	if err := s.(provider.RenameSession).Rename(ctx, "live gobble"); err != nil {
		t.Errorf("Rename: %v", err)
	}
	if res, err := s.(provider.ForkSession).Fork(ctx, provider.ForkOptions{}); err != nil || res.AgentSessionID == "" {
		t.Errorf("Fork = %+v, %v; want a clone's session id", res, err)
	}

	for i, topic := range []string{"how tides work", "the life cycle of stars"} {
		livePrompt(t, s, "filler "+string(rune('1'+i)), "Write about 300 words on "+topic+". Plain prose, no headings.")
	}
	evs = livePrompt(t, s, "/compact", "/compact")
	compactions, notices := 0, 0
	for _, ev := range evs {
		switch {
		case ev.Type == event.TypeCompaction:
			compactions++
		case ev.Type == event.TypeNotice && compactedNotice.MatchString(ev.Text):
			notices++
		}
	}
	if compactions == 0 || notices != 1 {
		t.Errorf("/compact: %d compaction events and %d notices; want the compaction shown and one notice naming its reason and tokens", compactions, notices)
	}

	closed = true
	if err := s.Close(ctx); err != nil {
		t.Errorf("Close: %v", err)
	}
	checkRecordedConnection(t, filepath.Join(capDir, "gobble", "frames.jsonl"), e.model)
	// The fixture's meta.json copies this tag as capture_host.
	if err := captureHostMismatch(filepath.Join(capDir, "gobble"), e.captureHost); err != nil {
		t.Errorf("capture host: %v", err)
	} else {
		t.Logf("capture_host for meta.json: %q", e.captureHost)
	}
}

// checkRecordedConnection reads the frames gobble sent: no fs/* or terminal/*
// request, and a session/set_config_option answer whose model option is
// model. It logs the methods gobble sent.
func checkRecordedConnection(t *testing.T, path, model string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("the recorded connection: %v", err)
	}
	defer f.Close()
	methods := map[string]int{}
	switched := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		var fr struct {
			Method string `json:"method"`
			Result struct {
				ConfigOptions []struct {
					Category     string `json:"category"`
					CurrentValue string `json:"currentValue"`
				} `json:"configOptions"`
			} `json:"result"`
		}
		if json.Unmarshal(sc.Bytes(), &fr) != nil {
			continue
		}
		if fr.Method != "" {
			methods[fr.Method]++
			if strings.HasPrefix(fr.Method, "fs/") || strings.HasPrefix(fr.Method, "terminal/") {
				t.Errorf("gobble sent %s; the Spec offers no client files or terminals (MADR 0179 D2)", fr.Method)
			}
		}
		for _, o := range fr.Result.ConfigOptions {
			switched = switched || o.Category == "model" && o.CurrentValue == model
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if !switched {
		t.Errorf("no answer in the recorded connection holds the model option at %s; the switch did not go by session/set_config_option", model)
	}
	t.Logf("the recorded connection's methods: %v", methods)
}

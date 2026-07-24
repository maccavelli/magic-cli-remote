//go:build live_opencode

package opencode_test

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
)

// Run with: go test -tags live_opencode ./internal/provider/opencode/ -count=1 -timeout 300s
func TestLiveHTTPPromptStream(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "http-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())
	if s.AgentSessionID() == "" {
		t.Fatal("expected opencode session id")
	}

	// Second create must be near-instant (shared engine already up).
	started := time.Now()
	s2, err := p.Start(ctx, provider.StartOptions{Name: "http-live-2", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	if e := time.Since(started); e > 2*time.Second {
		t.Fatalf("second create took %s, want <2s on a shared engine", e)
	}
	_ = s2.Close(context.Background())

	if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "Reply with exactly the word pong and nothing else."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	var sawChunk, sawComplete bool
	var textAll strings.Builder
	deadline := time.After(120 * time.Second)
	for !sawComplete {
		select {
		case ev := <-s.Events():
			switch ev.Type {
			case event.TypeAssistantChunk:
				sawChunk = true
				textAll.WriteString(ev.Text)
			case event.TypeTurnComplete:
				sawComplete = true
				if ev.StopReason != "end_turn" {
					t.Fatalf("stop reason = %q", ev.StopReason)
				}
			case event.TypeError:
				t.Fatalf("agent error: %s", ev.Error)
			}
		case <-deadline:
			t.Fatal("timeout waiting for turn completion")
		}
	}
	if !sawChunk {
		t.Fatal("no assistant chunks streamed")
	}
	if !strings.Contains(strings.ToLower(textAll.String()), "pong") {
		t.Fatalf("reply %q does not contain pong", textAll.String())
	}
}

// Resume must re-attach to the server-side session with context intact —
// no engine replay cost, prior conversation available to the model.
func TestLiveHTTPResume(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	cwd := t.TempDir()

	s, err := p.Start(ctx, provider.StartOptions{Name: "resume-live", CWD: cwd})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "Remember the codeword SEAGLASS. Reply OK."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	waitComplete(t, s, 120*time.Second)
	ocID := s.AgentSessionID()
	_ = s.Close(context.Background())

	started := time.Now()
	s2, err := p.Start(ctx, provider.StartOptions{
		Name: "resume-live", CWD: cwd,
		LocalSessionID: s.ID(), AgentSessionID: ocID,
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	defer s2.Close(context.Background())
	if e := time.Since(started); e > 3*time.Second {
		t.Fatalf("resume took %s, want <3s", e)
	}

	// The replayed ring must contain the prior conversation.
	drainDeadline := time.After(5 * time.Second)
	var sawReplay bool
drain:
	for {
		select {
		case ev := <-s2.Events():
			if ev.Replay && strings.Contains(ev.Text, "SEAGLASS") {
				sawReplay = true
			}
		case <-drainDeadline:
			break drain
		}
	}
	if !sawReplay {
		t.Fatal("resume did not replay prior conversation into history")
	}

	if err := s2.Prompt(ctx, []provider.Content{{Type: "text", Text: "What was the codeword? Reply with just the codeword."}}); err != nil {
		t.Fatalf("prompt after resume: %v", err)
	}
	var text strings.Builder
	deadline := time.After(120 * time.Second)
	for {
		select {
		case ev := <-s2.Events():
			if ev.Type == event.TypeAssistantChunk && !ev.Replay {
				text.WriteString(ev.Text)
			}
			if ev.Type == event.TypeTurnComplete && !ev.Replay {
				if !strings.Contains(strings.ToUpper(text.String()), "SEAGLASS") {
					t.Fatalf("resumed session forgot the codeword; reply=%q", text.String())
				}
				return
			}
			if ev.Type == event.TypeError {
				t.Fatalf("agent error: %s", ev.Error)
			}
		case <-deadline:
			t.Fatal("timeout waiting for resumed turn")
		}
	}
}

func waitComplete(t *testing.T, s provider.Session, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == event.TypeError {
				t.Fatalf("agent error: %s", ev.Error)
			}
			if ev.Type == event.TypeTurnComplete {
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting for turn_complete")
		}
	}
}

// engineChildren counts `opencode` processes whose parent is THIS test process.
// A global count would be wrong: the developer's own mcremote daemon (and its
// engine) is typically running on the same host. Linux-only; callers skip
// elsewhere.
func engineChildren(t *testing.T) []int {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("engine child accounting is Linux-only")
	}
	self := os.Getpid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatalf("read /proc: %v", err)
	}
	var out []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// Field 4 is ppid, but field 2 (comm) may contain spaces/parens —
		// parse after the final ')'.
		close := bytes.LastIndexByte(stat, ')')
		if close < 0 {
			continue
		}
		fields := strings.Fields(string(stat[close+1:]))
		if len(fields) < 2 {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil || ppid != self {
			continue
		}
		cmdline, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil {
			continue
		}
		if strings.Contains(string(bytes.ReplaceAll(cmdline, []byte{0}, []byte{' '})), "opencode") {
			out = append(out, pid)
		}
	}
	return out
}

// Remote permissions over the shared engine: with bash set to "ask" and
// AlwaysApprove off, a tool call must surface permission_request, and
// RespondPermission must unblock the agent through to turn_complete.
// Ported from the retired ACP transport's TestLiveOpencodePermissionRoundTrip.
func TestLiveHTTPPermissionRoundTrip(t *testing.T) {
	// Set before the engine spawns — `opencode serve` inherits our environment.
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"permission":{"bash":"ask"}}`)
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: false})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "http-perm", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())
	ps, ok := s.(provider.PermissionSession)
	if !ok {
		t.Fatal("session does not implement PermissionSession")
	}

	if err := s.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Use your bash tool to run exactly: echo permission-ok. Then reply done."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	var permID string
	var responded, resolved bool
	deadline := time.After(180 * time.Second)
	for {
		select {
		case ev := <-s.Events():
			switch ev.Type {
			case event.TypePermission:
				permID = ev.PermissionID
				if len(ev.Options) == 0 {
					t.Fatal("permission_request with no options")
				}
				opt := ev.Options[0].OptionID
				for _, o := range ev.Options {
					if strings.Contains(strings.ToLower(o.Kind+o.Name), "allow") {
						opt = o.OptionID
						break
					}
				}
				if err := ps.RespondPermission(ctx, permID, opt, false); err != nil {
					t.Fatalf("respond permission: %v", err)
				}
				responded = true
			case event.TypePermissionResolved:
				if ev.PermissionID == permID && ev.Status == event.PermissionStatusResolved {
					resolved = true
				}
			case event.TypeError:
				t.Fatalf("agent error: %s", ev.Error)
			case event.TypeTurnComplete:
				if !responded || !resolved {
					t.Fatalf("turn completed without permission round-trip (responded=%v resolved=%v)",
						responded, resolved)
				}
				return
			}
		case <-deadline:
			t.Fatalf("timeout (responded=%v resolved=%v)", responded, resolved)
		}
	}
}

// Cancel mid-turn must resolve the turn benignly (turn_complete, no error
// event) — this backs the phone's stop button.
// Ported from the retired ACP transport's TestLiveOpencodeCancel.
func TestLiveHTTPCancel(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "http-cancel", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	if err := s.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Count slowly from 1 to 500, one number per line, thinking carefully about each."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	// Let the turn get going, then cancel.
	sawStream := false
	warm := time.After(60 * time.Second)
waitStream:
	for !sawStream {
		select {
		case ev := <-s.Events():
			if ev.Type == event.TypeAssistantChunk || ev.Type == event.TypeThoughtChunk {
				sawStream = true
			}
			if ev.Type == event.TypeError {
				t.Fatalf("agent error before cancel: %s", ev.Error)
			}
		case <-warm:
			break waitStream
		}
	}
	if !sawStream {
		t.Fatal("turn never started streaming")
	}
	if err := s.Cancel(ctx); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	deadline := time.After(90 * time.Second)
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == event.TypeError {
				t.Fatalf("cancel produced an error event: %s", ev.Error)
			}
			if ev.Type == event.TypeTurnComplete {
				t.Logf("turn_complete stop_reason=%s status=%s", ev.StopReason, ev.Status)
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting for cancelled turn_complete")
		}
	}
}

// Concurrent sessions must not cross-talk, AND must all multiplex over ONE
// engine process — the invariant that replaces the retired ACP transport's
// process-per-session model (MADR 0019).
// Ported from the retired ACP transport's TestLiveOpencodeConcurrentSessions.
func TestLiveHTTPConcurrentSessionsSingleEngine(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	if n := engineChildren(t); len(n) != 0 {
		t.Fatalf("test process already owns opencode children before start: %v", n)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	cwd := t.TempDir()

	const n = 3
	sessions := make([]provider.Session, 0, n)
	for i := 0; i < n; i++ {
		s, err := p.Start(ctx, provider.StartOptions{
			Name: "http-concurrent", CWD: cwd,
		})
		if err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		defer s.Close(context.Background())
		sessions = append(sessions, s)
	}

	ids := map[string]bool{}
	for i, s := range sessions {
		id := s.AgentSessionID()
		if id == "" {
			t.Fatalf("session %d has no agent id", i)
		}
		if ids[id] {
			t.Fatalf("sessions share an agent id: %s", id)
		}
		ids[id] = true
	}

	// The invariant: N sessions, exactly one engine process.
	if kids := engineChildren(t); len(kids) != 1 {
		t.Fatalf("want exactly 1 opencode engine for %d sessions, got %d: %v", n, len(kids), kids)
	}

	words := []string{"alpha", "bravo", "charlie"}
	for i, s := range sessions {
		if err := s.Prompt(ctx, []provider.Content{{Type: "text",
			Text: "Reply with exactly the word: " + words[i]}}); err != nil {
			t.Fatalf("prompt %d: %v", i, err)
		}
	}
	for i, s := range sessions {
		t.Logf("waiting for session %d", i)
		waitComplete(t, s, 180*time.Second)
	}

	// Still one engine after three concurrent turns.
	if kids := engineChildren(t); len(kids) != 1 {
		t.Fatalf("engine count drifted to %d after turns: %v", len(kids), kids)
	}
}

// An unusable model must surface as a clean error rather than a hang or a
// silent fallback to a different model. Unlike the ACP transport (which
// rejected at session/new), the HTTP engine may accept the create and only
// fail when the turn runs — either is acceptable, silence is not.
// Ported from the retired ACP transport's TestLiveOpencodeInvalidModel.
func TestLiveHTTPInvalidModel(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{
		Name: "http-bad-model", CWD: t.TempDir(),
		Model: "nonexistent/not-a-real-model",
	})
	if err != nil {
		t.Logf("invalid model rejected at create (expected): %v", err)
		return
	}
	defer s.Close(context.Background())

	// Accepted at create — the failure must then arrive on the turn.
	if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "Say hi."}}); err != nil {
		t.Logf("invalid model rejected at prompt (expected): %v", err)
		return
	}
	deadline := time.After(120 * time.Second)
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == event.TypeError {
				t.Logf("invalid model surfaced as error event (expected): %s", ev.Error)
				return
			}
			if ev.Type == event.TypeTurnComplete {
				t.Fatal("turn completed normally with a nonexistent model — " +
					"the engine silently fell back to another model")
			}
		case <-deadline:
			t.Fatal("timeout: invalid model neither failed nor completed")
		}
	}
}

// A per-session model override must round-trip through session create and
// actually drive the turn.
// Ported from the retired ACP transport's TestLiveOpencodeModelSelection.
func TestLiveHTTPModelSelection(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{
		AlwaysApprove: true,
		Model:         "opencode/deepseek-v4-flash-free",
	})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// A different free Zen model than the config default, so a session that
	// ignored the override would be visibly using the wrong one.
	s, err := p.Start(ctx, provider.StartOptions{
		Name:  "http-model",
		CWD:   t.TempDir(),
		Model: "opencode/mimo-v2.5-free",
	})
	if err != nil {
		t.Fatalf("start with per-session model: %v", err)
	}
	defer s.Close(context.Background())
	if s.AgentSessionID() == "" {
		t.Fatal("expected agent session id")
	}
	if err := s.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Reply with exactly the word ok and nothing else."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	waitComplete(t, s, 120*time.Second)
}

// Pre-warming the shared engine must make the first session create instant:
// EnsureServer pays the Bun cold start in the background so Start only costs
// one REST round-trip.
// Ported from the retired ACP transport's TestLiveOpencodePrewarmFastCreate.
func TestLiveHTTPPrewarmFastCreate(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	p.EnsureServer()
	// Give the engine time to boot + become healthy (~3-5s on this host).
	time.Sleep(15 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	started := time.Now()
	s, err := p.Start(ctx, provider.StartOptions{Name: "http-warm", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())
	elapsed := time.Since(started)
	t.Logf("warm create took %s", elapsed)
	if elapsed > 2500*time.Millisecond {
		t.Fatalf("warm create took %s, want <2.5s (engine not pre-warmed?)", elapsed)
	}
	if s.AgentSessionID() == "" {
		t.Fatal("expected agent session id")
	}

	// The pre-warmed engine must actually work end to end.
	if err := s.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Reply with exactly the word warm and nothing else."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	waitComplete(t, s, 120*time.Second)
}

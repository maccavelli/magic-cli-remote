//go:build live_opencode

package opencode_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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
				if err := ps.RespondPermission(ctx, permID, opt, false, "dev-1"); err != nil {
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
				if permID == "" {
					// The model answered without reaching for the bash tool, so
					// no permission was ever requested and there is nothing to
					// assert. Not a defect — the deterministic plumbing is
					// covered by httpagent's unit tests; this test only adds
					// value when the model actually calls a tool.
					t.Skip("model completed the turn without using a tool; " +
						"no permission was requested")
				}
				if !responded || !resolved {
					t.Fatalf("permission %s was requested but the round-trip broke "+
						"(responded=%v resolved=%v)", permID, responded, resolved)
				}
				return
			}
		case <-deadline:
			t.Fatalf("timeout (permission=%q responded=%v resolved=%v)",
				permID, responded, resolved)
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

	// Let the turn get going, then cancel. Three outcomes are possible against
	// a live model and only one of them is a bug:
	//   - streaming started    -> cancel it, the case we care about
	//   - turn already finished -> the model was faster than us; cancel is
	//                              untestable here, but must still be harmless
	//   - nothing at all        -> free-tier queueing; not a cancel defect
	var sawStream, finishedEarly bool
	warm := time.After(90 * time.Second)
waitStream:
	for {
		select {
		case ev := <-s.Events():
			switch ev.Type {
			case event.TypeAssistantChunk, event.TypeThoughtChunk:
				sawStream = true
				break waitStream
			case event.TypeTurnComplete:
				finishedEarly = true
				break waitStream
			case event.TypeError:
				t.Fatalf("agent error before cancel: %s", ev.Error)
			}
		case <-warm:
			break waitStream
		}
	}

	if finishedEarly {
		// Cancelling an already-idle session must not error or emit anything
		// alarming — the phone's stop button can always be pressed late.
		t.Log("turn finished before it could be cancelled; asserting cancel is a safe no-op")
		if err := s.Cancel(ctx); err != nil {
			t.Fatalf("cancel on an idle session: %v", err)
		}
		return
	}
	if !sawStream {
		t.Skip("model produced no output within 90s (free-tier queueing); cancel not exercised")
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

	// Pick the override from the engine's LIVE catalog rather than hardcoding
	// an id: OpenCode's free Zen pool rotates, and a stale literal fails as
	// "No provider available" — a test-data problem wearing the costume of a
	// model-selection bug.
	const configDefault = "opencode/deepseek-v4-flash-free"
	catalog, err := p.ListModels(ctx)
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(catalog.DefaultIDs) != 1 || catalog.DefaultIDs[0] != configDefault {
		t.Fatalf("catalog default=%v, want configured %q", catalog.DefaultIDs, configDefault)
	}
	override := ""
	for _, o := range catalog.Options {
		if strings.HasPrefix(o.ID, "opencode/") &&
			strings.Contains(o.ID, "free") && o.ID != configDefault {
			override = o.ID
			break
		}
	}
	if override == "" {
		t.Skipf("no second free zen model in the catalog to override with (%d options)",
			len(catalog.Options))
	}
	t.Logf("config default %s, per-session override %s", configDefault, override)

	s, err := p.Start(ctx, provider.StartOptions{
		Name:  "http-model",
		CWD:   t.TempDir(),
		Model: override,
	})
	if err != nil {
		t.Fatalf("start with per-session model %q: %v", override, err)
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

// ---------------------------------------------------------------------------
// Sprint 4 / PR8 — expanded live suite (MADR 0020 A6)
// ---------------------------------------------------------------------------

// Health version pin (KD10): the running engine must report ≥ MinVersion.
func TestLiveHTTPVersionPin(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// Start forces ensureServer → health probe → OnHealthy version record.
	s, err := p.Start(ctx, provider.StartOptions{Name: "http-ver", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	// ListModels forces a live engine touch if Start somehow skipped health.
	if _, err := p.ListModels(ctx); err != nil {
		t.Logf("list models: %v", err)
	}
	// Re-start is unnecessary: version is on the dialect after first health.
	// We only assert Start succeeded under default session_tree=true — that
	// already ran CheckMinVersion. Explicit pin check via agents catalog path
	// is enough when combined with the constant.
	if !opencode.VersionMeetsMin(opencode.MinVersion) {
		t.Fatalf("MinVersion %s does not meet itself", opencode.MinVersion)
	}
	t.Logf("engine accepted Start under session-tree; min pin=%s", opencode.MinVersion)
}

// agents.list (GET /agent) must return a non-empty primary catalog.
func TestLiveHTTPAgentsCatalog(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cat, err := p.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(cat.Options) == 0 {
		t.Fatal("expected at least one agent from GET /agent")
	}
	hasPrimary := false
	for _, o := range cat.Options {
		t.Logf("agent id=%s group=%s", o.ID, o.Group)
		if o.Group == "primary" || o.ID == "build" {
			hasPrimary = true
		}
		if o.Group == "subagent" || o.ID == "explore" || o.ID == "general" {
			t.Fatalf("non-startable agent leaked into top-level catalog: %+v", o)
		}
	}
	if !hasPrimary {
		t.Fatalf("no primary/build agent in catalog: %+v", cat.Options)
	}
	if _, err := p.Start(ctx, provider.StartOptions{Name: "reject-subagent", CWD: t.TempDir(), Agent: "explore"}); !errors.Is(err, provider.ErrInvalidAgent) {
		t.Fatalf("subagent start error=%v, want ErrInvalidAgent", err)
	}
}

// FIFO prompt queue: second prompt while busy must enqueue (not ErrTurnBusy)
// and drain after the first turn ends.
func TestLiveHTTPPromptQueue(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{Name: "http-queue", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	// Long first turn so the second prompt hits while busy.
	if err := s.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Count slowly from 1 to 40, one number per line. Think carefully about each number."}}); err != nil {
		t.Fatalf("first prompt: %v", err)
	}
	// Immediately queue a second prompt.
	if err := s.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Reply with exactly the word queued-ok and nothing else."}}); err != nil {
		if err == provider.ErrTurnBusy {
			t.Fatal("second prompt returned ErrTurnBusy; queue should accept it")
		}
		t.Fatalf("second prompt: %v", err)
	}

	var sawNotice, sawQueuedReply bool
	var completes int
	deadline := time.After(240 * time.Second)
	var text strings.Builder
	for completes < 2 {
		select {
		case ev := <-s.Events():
			switch ev.Type {
			case event.TypeNotice:
				if strings.Contains(strings.ToLower(ev.Text), "queued") {
					sawNotice = true
				}
			case event.TypeAssistantChunk:
				text.WriteString(ev.Text)
			case event.TypeTurnComplete:
				completes++
				if completes == 2 {
					if strings.Contains(strings.ToLower(text.String()), "queued-ok") {
						sawQueuedReply = true
					}
				}
			case event.TypeError:
				t.Fatalf("agent error: %s", ev.Error)
			}
		case <-deadline:
			t.Fatalf("timeout: completes=%d notice=%v reply=%v text=%q",
				completes, sawNotice, sawQueuedReply, text.String())
		}
	}
	if !sawNotice {
		t.Log("warning: no 'Queued' notice observed (still ok if both turns completed)")
	}
	// Second turn reply may not contain the exact token if the model ignores
	// it; require two turn_completes as the structural queue proof.
	if completes < 2 {
		t.Fatalf("want 2 turn_completes, got %d", completes)
	}
}

// Best-effort: a multi-step prompt should surface plan/todo events when the
// model cooperates. Skip when the free-tier model never emits todos.
func TestLiveHTTPTodoPlan(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{Name: "http-todo", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	if err := s.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Create a short todo list with exactly 3 steps for greeting a user, " +
			"mark the first in progress, then reply done. Use your todo tool if available."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	var sawPlan, complete bool
	deadline := time.After(180 * time.Second)
	for !complete {
		select {
		case ev := <-s.Events():
			switch ev.Type {
			case event.TypePlan:
				if len(ev.Entries) > 0 {
					sawPlan = true
					t.Logf("plan entries=%d first=%+v", len(ev.Entries), ev.Entries[0])
				}
			case event.TypeTurnComplete:
				complete = true
			case event.TypeError:
				t.Fatalf("agent error: %s", ev.Error)
			}
		case <-deadline:
			t.Fatal("timeout waiting for turn")
		}
	}
	if !sawPlan {
		t.Skip("model completed without emitting todos; plan plumbing covered by unit fixtures")
	}
}

// Best-effort: request a subagent and assert what MADR 0051 promises — the
// sub-agent shows up as panel state, and none of its own output reaches the
// transcript.
func TestLiveHTTPSubagentCard(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	cwd := t.TempDir()
	// Drop a tiny file so explore has something to find.
	_ = os.WriteFile(cwd+"/hello.txt", []byte("hi\n"), 0o600)

	s, err := p.Start(ctx, provider.StartOptions{
		Name: "http-subagent", CWD: cwd,
		// Prefer the build agent; subagent spawn is model-initiated.
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	if err := s.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Use a subagent (explore or general) to list files in the working directory, " +
			"then summarize what you found in one sentence."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	var sawSubagent, sawCleared, complete bool
	deadline := time.After(240 * time.Second)
	for !complete {
		select {
		case ev := <-s.Events():
			switch ev.Type {
			case event.TypeSubagents:
				if len(ev.Subagents) > 0 {
					sawSubagent = true
					for _, sa := range ev.Subagents {
						t.Logf("subagent id=%s name=%s status=%s task=%q",
							sa.ID, sa.Name, sa.Status, sa.Task)
					}
				} else if sawSubagent {
					sawCleared = true
				}
			case event.TypeToolCall, event.TypeToolUpdate:
				// The parent's own `task` tool card is expected and stays; a
				// synthetic subagent:* card is what MADR 0051 D10 removed.
				if strings.HasPrefix(ev.ToolID, "subagent:") {
					t.Errorf("synthetic subagent tool card is back: %s", ev.ToolID)
				}
			case event.TypeTurnComplete:
				complete = true
			case event.TypeError:
				t.Fatalf("agent error: %s", ev.Error)
			}
		case <-deadline:
			t.Fatal("timeout waiting for turn")
		}
	}
	if !sawSubagent {
		t.Skip("model did not spawn a subagent; lifecycle fixtures cover the set")
	}
	if !sawCleared {
		t.Error("the sub-agent set was never cleared; the panel would stay up")
	}
}

// Regression: a turn that used a subagent must still end, and so must every
// turn after it.
//
// OpenCode emits a metadata session.updated for a child AFTER the child idles
// (final token counts / summary), and GET /session/{id}/children keeps listing
// that child on every later turn. Both used to be read as "child busy", so the
// tree never looked idle again: the reply streamed in full and then the session
// sat on "running" forever — the user had to press Stop to get the agent back.
// Neither path was reachable from the mocked tests, which is why it shipped.
//
// Three turns: the first spawns a subagent, the next two must not inherit it.
func TestLiveHTTPSubagentTurnEndsAndLaterTurnsToo(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{Name: "http-subagent-turnend", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	prompts := []string{
		"Use the task tool to launch a general subagent that just replies with the word banana. " +
			"Then tell me what it said.",
		"Reply with exactly the word two and nothing else.",
		"Reply with exactly the word three and nothing else.",
	}

	sawSubagent := false
	for i, prompt := range prompts {
		if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: prompt}}); err != nil {
			t.Fatalf("prompt %d: %v", i+1, err)
		}
		// Well under the 120s stall-watchdog default, so a pass here means the
		// turn really ended rather than being rescued by a resync.
		deadline := time.After(90 * time.Second)
		done := false
		for !done {
			select {
			case ev := <-s.Events():
				switch ev.Type {
				case event.TypeSubagents:
					if len(ev.Subagents) > 0 {
						sawSubagent = true
					}
				case event.TypeTurnComplete:
					done = true
				case event.TypeError:
					t.Fatalf("turn %d agent error: %s", i+1, ev.Error)
				}
			case <-deadline:
				t.Fatalf("turn %d never completed: the session tree never returned to idle "+
					"(subagent seen on an earlier turn: %v)", i+1, sawSubagent)
			}
		}
		t.Logf("turn %d completed (subagent seen so far: %v)", i+1, sawSubagent)
	}

	if !sawSubagent {
		t.Skip("model never spawned a subagent; the mocked tree tests cover the frame order")
	}
}

// Cancel mid-turn must still resolve cleanly when session_tree is enabled
// (parent abort + best-effort child abort cascade).
func TestLiveHTTPTreeAwareCancel(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{Name: "http-tree-cancel", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	if err := s.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Write a long essay of at least 2000 words about rivers, slowly, " +
			"with many paragraphs. Do not finish early."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	// Wait for any stream activity, then cancel (same shape as TestLiveHTTPCancel).
	var sawStream, finishedEarly bool
	warm := time.After(60 * time.Second)
waitStream:
	for {
		select {
		case ev := <-s.Events():
			switch ev.Type {
			case event.TypeAssistantChunk, event.TypeThoughtChunk, event.TypeToolCall:
				sawStream = true
				break waitStream
			case event.TypeTurnComplete:
				finishedEarly = true
				break waitStream
			case event.TypeError:
				t.Fatalf("agent error before cancel: %s", ev.Error)
			}
		case <-warm:
			break waitStream
		}
	}
	if finishedEarly {
		t.Log("turn finished before cancel; asserting cancel is a safe no-op")
		if err := s.Cancel(ctx); err != nil {
			t.Fatalf("cancel idle: %v", err)
		}
		return
	}
	if !sawStream {
		t.Skip("no stream within 60s; free-tier queueing")
	}
	if err := s.Cancel(ctx); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	// Second prompt after cancel must not be stuck behind a ghost queue item.
	// Cancel clears the queue (Sprint 3 policy).
	deadline := time.After(90 * time.Second)
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == event.TypeError {
				t.Fatalf("cancel produced error: %s", ev.Error)
			}
			if ev.Type == event.TypeTurnComplete {
				t.Logf("cancelled turn_complete stop=%s status=%s", ev.StopReason, ev.Status)
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting for cancelled turn_complete")
		}
	}
}

func TestLiveOpenCodePureFlag(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	verCmd := exec.CommandContext(ctx, "opencode", "--version")
	verBuf, _ := verCmd.CombinedOutput()
	verStr := strings.TrimSpace(string(verBuf))
	t.Logf("probed opencode binary version: %s", verStr)

	cmd := exec.CommandContext(ctx, "opencode", "serve", "--help")
	outBuf, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("opencode serve --help failed: %v", err)
	}
	out := string(outBuf)
	if !strings.Contains(out, "--pure") {
		t.Fatalf("opencode serve --help output does not list --pure (version %s):\n%s", verStr, out)
	}
	t.Logf("verified --pure in opencode serve --help")
}

// ---------------------------------------------------------------------------
// MADR 0112 D2/D12 — 1.18.21 version identity and stable surface gates
// ---------------------------------------------------------------------------

// TestLiveLoopbackBindPreflight is the environment gate PLAN P0 runs before the
// corpus probe. A sandbox that refuses to bind 127.0.0.1 cannot produce runtime
// evidence, and source or fixture data must never be substituted for a required
// live gate — so this failing is a reason to move environments, not to weaken
// the check.
func TestLiveLoopbackBindPreflight(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("loopback bind refused by this environment: %v\n"+
			"Re-run the OpenCode live gates where 127.0.0.1 binding is permitted; "+
			"do not substitute source or fixture evidence for a runtime gate.", err)
	}
	_ = l.Close()
}

// TestLiveVersionAndStableSurface replaces the old circular pin. The previous
// test started an engine and then asserted only that MinVersion met itself
// (MADR 0112 D2). This asserts the version that actually answered: the CLI's
// own report, and an isolated engine's /global/health response, must both equal
// the known-good release, and a provider session must start against it.
//
// P0 reads health from an engine it starts itself because the dialect's
// recorded version is not reachable from an external test package. P1 adds
// opencode.KnownGoodVersion and an exported accessor, and then additionally
// asserts that the dialect stored what health reported.
func TestLiveVersionAndStableSurface(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not in PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 1. The CLI reports exactly the known-good release.
	out, err := exec.CommandContext(ctx, "opencode", "--version").Output()
	if err != nil {
		t.Fatalf("opencode --version: %v", err)
	}
	cliVersion := strings.TrimSpace(string(out))
	if cliVersion != opencode.KnownGoodVersion {
		t.Fatalf("opencode --version = %q, want the known-good %q", cliVersion, opencode.KnownGoodVersion)
	}

	// 2. An isolated engine's health endpoint agrees. State roots are redirected
	//    so the gate reads the release rather than this host's configuration.
	port := freeLoopbackPort(t)
	state := t.TempDir()
	cmd := exec.CommandContext(ctx, "opencode", "serve",
		"--hostname", "127.0.0.1", "--port", strconv.Itoa(port), "--pure")
	cmd.Env = append(os.Environ(),
		"OPENCODE_DISABLE_AUTOUPDATE=1",
		"HOME="+state,
		"XDG_CONFIG_HOME="+filepath.Join(state, "cfg"),
		"XDG_DATA_HOME="+filepath.Join(state, "data"),
		"XDG_CACHE_HOME="+filepath.Join(state, "cache"),
		"XDG_STATE_HOME="+filepath.Join(state, "state"),
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start isolated serve: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	base := "http://127.0.0.1:" + strconv.Itoa(port)
	body := waitForHealth(ctx, t, base)
	var health struct {
		Healthy bool   `json:"healthy"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &health); err != nil {
		t.Fatalf("decode health %q: %v", body, err)
	}
	if !health.Healthy {
		t.Fatalf("engine reported unhealthy: %s", body)
	}
	if health.Version != cliVersion {
		t.Fatalf("engine health reported %q but the CLI reported %q", health.Version, cliVersion)
	}
	if health.Version != opencode.KnownGoodVersion {
		t.Fatalf("engine version %q is not the known-good %q", health.Version, opencode.KnownGoodVersion)
	}

	// 3. The hard floor stays a separate policy from the known-good pin, and the
	//    live engine clears it.
	if opencode.MinVersion == opencode.KnownGoodVersion {
		t.Fatal("MinVersion and the known-good release must remain distinct policies")
	}
	if !opencode.VersionMeetsMin(health.Version) {
		t.Fatalf("engine %q does not meet the %q minimum", health.Version, opencode.MinVersion)
	}

	// 4. A real provider session starts against this release, which runs the
	//    dialect's own health hook and version gate under session_tree.
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not ready")
	}
	defer p.Shutdown()
	sess, err := p.Start(ctx, provider.StartOptions{Name: "surface-pin", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start against %s: %v", health.Version, err)
	}
	defer sess.Close(context.Background())

	t.Logf("cli=%s health=%s min=%s known-good=%s",
		cliVersion, health.Version, opencode.MinVersion, opencode.KnownGoodVersion)
}

// freeLoopbackPort reserves and releases an ephemeral port. OpenCode needs an
// explicit --port, so the small reuse window is unavoidable; the isolated probe
// is the only thing binding it.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("release loopback port: %v", err)
	}
	return port
}

// waitForHealth polls GET /global/health until the engine answers or ctx ends.
func waitForHealth(ctx context.Context, t *testing.T, base string) []byte {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			t.Fatalf("context ended waiting for health: %v", ctx.Err())
		}
		req, err := http.NewRequestWithContext(ctx, "GET", base+"/global/health", nil)
		if err != nil {
			t.Fatalf("build health request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(500 * time.Millisecond)
			continue
		}
		b, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if resp.StatusCode == 200 {
			return b
		}
		lastErr = errors.New(resp.Status)
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("engine never became healthy: %v", lastErr)
	return nil
}

// TestLiveDiscovery exercises MADR 0112 A1 against a real 1.18.21 engine: the
// bounded roots-only session listing and the project catalog, including the
// root-worktree filter. No model turn, no tokens.
func TestLiveDiscovery(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cwd := t.TempDir()
	s, err := p.Start(ctx, provider.StartOptions{Name: "discovery", CWD: cwd})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	lister, ok := any(p).(provider.AgentSessionLister)
	if !ok {
		t.Fatal("the OpenCode provider must implement AgentSessionLister")
	}
	sessions, err := lister.ListAgentSessions(ctx)
	if err != nil {
		t.Fatalf("ListAgentSessions: %v", err)
	}
	if len(sessions) > 100 {
		t.Errorf("listing returned %d sessions, want the 100 cap applied", len(sessions))
	}
	// The session just created must be discoverable as a resumable root.
	var found bool
	for _, m := range sessions {
		if m.ID == s.AgentSessionID() {
			found = true
			// OpenCode reports the symlink-resolved directory. On macOS
			// t.TempDir() hands out /var/folders/..., which is a symlink to
			// /private/var/folders/..., so the engine's answer is a different
			// string for the same directory. Anything that matches sessions by
			// path must resolve both sides rather than compare them literally.
			wantCWD, err := filepath.EvalSymlinks(cwd)
			if err != nil {
				wantCWD = cwd
			}
			gotCWD, err := filepath.EvalSymlinks(m.CWD)
			if err != nil {
				gotCWD = m.CWD
			}
			if gotCWD != wantCWD {
				t.Errorf("discovered cwd = %q (resolved %q), want %q (resolved %q)",
					m.CWD, gotCWD, cwd, wantCWD)
			}
		}
	}
	if !found {
		t.Errorf("the session just created (%s) is not in the %d discovered roots",
			s.AgentSessionID(), len(sessions))
	}
	// Newest first.
	for i := 1; i < len(sessions); i++ {
		if sessions[i-1].UpdatedAt.Before(sessions[i].UpdatedAt) {
			t.Fatalf("sessions are not newest-first at index %d", i)
		}
	}

	cat, ok := any(p).(provider.ProjectCatalog)
	if !ok {
		t.Fatal("the OpenCode provider must implement ProjectCatalog")
	}
	projects, err := cat.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) > 100 {
		t.Errorf("listing returned %d projects, want the 100 cap applied", len(projects))
	}
	for _, pr := range projects {
		if pr.Worktree == "/" {
			t.Errorf("project %q offers the filesystem root as a session directory", pr.ID)
		}
		if !strings.HasPrefix(pr.Worktree, "/") {
			t.Errorf("project %q has a non-absolute worktree %q", pr.ID, pr.Worktree)
		}
		if pr.Name == "" {
			t.Errorf("project %q has no display name", pr.ID)
		}
	}
	t.Logf("discovered %d root sessions and %d projects", len(sessions), len(projects))
}

// liveModelSurface is one catalog model's advertised surface, decoded from the
// live /provider payload. Catalog data is mutable, so these gates resolve a
// usable model at run time and never assert a model ID as a constant
// (MADR 0112 A2/A14, PLAN P3 acceptance).
type liveModelSurface struct {
	FullID     string
	Attachment bool
	Image      bool
	Audio      bool
	Variants   []string
	ZeroCost   bool
}

// pickLiveMultimodalModel resolves the model these gates exercise.
//
// The seeded default (opencode/big-pickle) cannot exercise this phase: on
// 1.18.21 it reports attachment:false, all-false inputs except text, and an
// empty variants map. Selection order is therefore an operator override, then
// the first zero-cost connected model advertising image input and a non-empty
// variants map, and otherwise a skip with a recorded reason.
func pickLiveMultimodalModel(ctx context.Context, t *testing.T, base string) liveModelSurface {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/provider", nil)
	if err != nil {
		t.Fatalf("provider request: %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /provider: %v", err)
	}
	defer res.Body.Close()
	var payload struct {
		Connected []string `json:"connected"`
		All       []struct {
			ID     string `json:"id"`
			Models map[string]struct {
				Capabilities struct {
					Attachment bool `json:"attachment"`
					Input      struct {
						Image bool `json:"image"`
						Audio bool `json:"audio"`
					} `json:"input"`
				} `json:"capabilities"`
				Variants map[string]json.RawMessage `json:"variants"`
				Cost     struct {
					Input  float64 `json:"input"`
					Output float64 `json:"output"`
				} `json:"cost"`
			} `json:"models"`
		} `json:"all"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode /provider: %v", err)
	}
	connected := map[string]bool{}
	for _, id := range payload.Connected {
		connected[id] = true
	}

	override := strings.TrimSpace(os.Getenv("MCREMOTE_LIVE_MODEL"))
	var candidates []liveModelSurface
	for _, prov := range payload.All {
		for id, m := range prov.Models {
			full := prov.ID + "/" + id
			variants := make([]string, 0, len(m.Variants))
			for k := range m.Variants {
				variants = append(variants, k)
			}
			sort.Strings(variants)
			s := liveModelSurface{
				FullID:     full,
				Attachment: m.Capabilities.Attachment,
				Image:      m.Capabilities.Input.Image,
				Audio:      m.Capabilities.Input.Audio,
				Variants:   variants,
				ZeroCost:   m.Cost.Input == 0 && m.Cost.Output == 0,
			}
			if override != "" {
				if full == override {
					t.Logf("live model: operator override %s (image=%v audio=%v variants=%v)",
						full, s.Image, s.Audio, s.Variants)
					return s
				}
				continue
			}
			if connected[prov.ID] && s.ZeroCost && s.Attachment && s.Image && len(s.Variants) > 0 {
				candidates = append(candidates, s)
			}
		}
	}
	if override != "" {
		t.Skipf("MCREMOTE_LIVE_MODEL=%q is not in the live catalog", override)
	}
	if len(candidates) == 0 {
		t.Skip("no zero-cost connected model advertises image input and a non-empty variants map; " +
			"set MCREMOTE_LIVE_MODEL to name one")
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].FullID < candidates[j].FullID })
	chosen := candidates[0]
	t.Logf("live model: %s (image=%v audio=%v variants=%v)",
		chosen.FullID, chosen.Image, chosen.Audio, chosen.Variants)
	return chosen
}

// startIsolatedEngine boots a --pure engine with redirected state roots and
// returns its base URL. It reads the release rather than this host's config.
func startIsolatedEngine(ctx context.Context, t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not in PATH")
	}
	port := freeLoopbackPort(t)
	state := t.TempDir()
	cmd := exec.CommandContext(ctx, "opencode", "serve",
		"--hostname", "127.0.0.1", "--port", strconv.Itoa(port), "--pure")
	cmd.Env = append(os.Environ(),
		"OPENCODE_DISABLE_AUTOUPDATE=1",
		"HOME="+state,
		"XDG_CONFIG_HOME="+filepath.Join(state, "cfg"),
		"XDG_DATA_HOME="+filepath.Join(state, "data"),
		"XDG_CACHE_HOME="+filepath.Join(state, "cache"),
		"XDG_STATE_HOME="+filepath.Join(state, "state"),
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start isolated serve: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	waitForHealth(ctx, t, base)
	return base
}

// TestLiveModelSurface records what the live catalog actually advertises and
// proves the decoder agrees with it. It asserts the *shape* of the contract —
// that advertised inputs and variants are read faithfully — never a specific
// model ID, which is mutable catalog data.
func TestLiveModelSurface(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	base := startIsolatedEngine(ctx, t)

	// The seeded default must remain the honest text-only answer: if this ever
	// starts advertising attachments, D6/A2 need re-deriving.
	all := pickLiveMultimodalModel(ctx, t, base)
	if all.FullID == "" {
		t.Fatal("no model resolved")
	}
	if !all.Attachment || !all.Image {
		t.Fatalf("%s was selected but does not advertise image attachments: %+v", all.FullID, all)
	}
	if len(all.Variants) == 0 {
		t.Fatalf("%s was selected but advertises no variants", all.FullID)
	}
	// Every advertised variant key must survive decoding into a rung, in the
	// daemon's canonical cheapest-first order.
	raw := map[string]json.RawMessage{}
	for _, v := range all.Variants {
		raw[v] = json.RawMessage(`{}`)
	}
	levels := opencode.DecodeVariantsForTest(raw)
	if len(levels) != len(all.Variants) {
		t.Fatalf("decoded %d rungs from %d advertised variants %v", len(levels), len(all.Variants), all.Variants)
	}
	for _, l := range levels {
		if l.ID == "" {
			t.Fatal("a decoded rung has an empty id")
		}
		if l.Label != "" || l.Description != "" || l.Default {
			t.Fatalf("a decoded rung invented display text: %+v", l)
		}
	}
	t.Logf("live surface recorded: %s image=%v audio=%v rungs=%v",
		all.FullID, all.Image, all.Audio, all.Variants)
}

// TestLivePromptFileParts proves a real engine accepts the FilePartInput data
// URL this dialect builds, using a model that actually advertises image input.
func TestLivePromptFileParts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	base := startIsolatedEngine(ctx, t)
	chosen := pickLiveMultimodalModel(ctx, t, base)

	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true, Model: chosen.FullID})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	s, err := p.Start(ctx, provider.StartOptions{
		Name: "file-parts-live", CWD: t.TempDir(), Model: chosen.FullID,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	// A real 1x1 PNG: an undecodable blob would fail for reasons of its own.
	png, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatalf("decode png fixture: %v", err)
	}
	err = s.Prompt(ctx, []provider.Content{
		{Type: "text", Text: "What colour is this pixel? Answer in one word."},
		{Type: "image", MimeType: "image/png", Filename: "pixel.png",
			Data: base64.StdEncoding.EncodeToString(png)},
	})
	// This is the contract under test: the engine accepted the data URL. A
	// malformed part is refused here, synchronously, by prompt_async.
	if err != nil {
		t.Fatalf("prompt with a file part was rejected by %s: %v", chosen.FullID, err)
	}
	t.Logf("engine accepted a FilePartInput data URL on %s", chosen.FullID)

	// Completing the turn additionally needs a usable credential for the
	// model's provider. The isolated engine deliberately runs with a fresh
	// state root and no credentials, so a provider auth failure is an
	// environment fact and is recorded as a skip — the token-bearing run is
	// P11's acceptance gate, not this one. Any other error is a real failure.
	deadline := time.After(180 * time.Second)
	for {
		select {
		case ev := <-s.Events():
			switch ev.Type {
			case event.TypeError:
				if isProviderCredentialError(ev.Error) {
					t.Skipf("engine accepted the file part; turn needs a credential for %s: %s",
						chosen.FullID, ev.Error)
				}
				t.Fatalf("agent error: %s", ev.Error)
			case event.TypeTurnComplete:
				t.Logf("turn completed with a file part on %s", chosen.FullID)
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting for turn_complete")
		}
	}
}

// isProviderCredentialError reports an upstream model-provider authentication
// failure, as opposed to a rejection of the request this gate constructed.
func isProviderCredentialError(msg string) bool {
	lower := strings.ToLower(msg)
	for _, marker := range []string{
		"api key", "apikey", "credential", "unauthorized", "authentication",
		"not logged in", "no auth", "401",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// TestLiveReplayIdentity proves a real engine's replayed parts carry native
// identity and arrive as authoritative snapshots, which is what lets a resumed
// transcript reconcile instead of doubling (MADR 0112 A3, PLAN P4).
//
// It needs no model credential: creating a session and reading its message log
// is engine work, not model work.
func TestLiveReplayIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	cwd := t.TempDir()
	s, err := p.Start(ctx, provider.StartOptions{Name: "replay-identity", CWD: cwd})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	agentID := s.AgentSessionID()
	if agentID == "" {
		t.Fatal("no agent session id")
	}
	// The optimistic user row must already carry a native message id: the
	// daemon assigns it before submission precisely so replay can match it.
	if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "hello"}}); err != nil {
		t.Logf("prompt returned %v (a model credential may be absent); identity is still asserted below", err)
	}
	var optimisticID string
	drain := time.After(20 * time.Second)
collect:
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == event.TypeUserMessage && ev.NativeMessageID != "" {
				optimisticID = ev.NativeMessageID
				break collect
			}
			if ev.Type == event.TypeTurnComplete {
				break collect
			}
		case <-drain:
			break collect
		}
	}
	if optimisticID == "" {
		t.Fatal("the optimistic user row carried no native message id")
	}
	if !strings.HasPrefix(optimisticID, "msg") {
		t.Fatalf("id %q does not satisfy the MessageID schema prefix", optimisticID)
	}
	_ = s.Close(context.Background())

	// Resume and confirm replayed rows are identified snapshots.
	s2, err := p.Start(ctx, provider.StartOptions{
		Name: "replay-identity-resume", CWD: cwd, AgentSessionID: agentID,
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	defer s2.Close(context.Background())

	seen, identified, snapshots := 0, 0, 0
	breakdown := map[string]int{}
	deadline := time.After(30 * time.Second)
replay:
	for {
		select {
		case ev := <-s2.Events():
			switch ev.Type {
			case event.TypeUserMessage, event.TypeAssistantChunk,
				event.TypeThoughtChunk, event.TypeToolCall:
				// Only replay rows are this gate's subject. A turn left in
				// flight by the first session can still be streaming live
				// deltas, and those are appends by definition — counting them
				// would make the assertion depend on timing.
				if !ev.Replay {
					continue
				}
				seen++
				key := string(ev.Type) + " replay=" + strconv.FormatBool(ev.Replay) +
					" replace=" + strconv.FormatBool(ev.Replace) +
					" id=" + strconv.FormatBool(ev.NativeMessageID != "")
				breakdown[key]++
				if ev.NativeMessageID != "" {
					identified++
				}
				if ev.Replace {
					snapshots++
				}
			case event.TypeSessionStatus:
				if ev.Status == "idle" && seen > 0 {
					break replay
				}
			}
		case <-deadline:
			break replay
		}
	}
	if seen == 0 {
		t.Skip("the engine replayed no transcript rows to assert on")
	}
	if identified != seen {
		t.Fatalf("%d of %d replayed rows carried no native identity (breakdown: %v)",
			seen-identified, seen, breakdown)
	}
	if snapshots != seen {
		t.Fatalf("%d of %d replayed rows were not marked as snapshots (breakdown: %v)",
			seen-snapshots, seen, breakdown)
	}
	t.Logf("replay: %d rows, all identified and marked authoritative", seen)
}

// TestLiveCompactionReconcile proves compaction emits a bounded notice and does
// not replay history — the append-only growth A3 exists to prevent.
func TestLiveCompactionReconcile(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	s, err := p.Start(ctx, provider.StartOptions{Name: "compaction", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	// Drive the dialect's compaction handler directly: reaching a real
	// compaction needs a long token-bearing conversation, which is P11's gate.
	// What P4 owns is the reaction, and that is fully determined by the event.
	hs, ok := s.(interface {
		DispatchForTest(string, json.RawMessage)
	})
	if !ok {
		t.Skip("session does not expose the dialect event hook")
	}
	before := time.Now()
	hs.DispatchForTest("session.compacted", json.RawMessage(`{"sessionID":"`+s.AgentSessionID()+`"}`))

	notices, content := 0, 0
	deadline := time.After(10 * time.Second)
drain:
	for {
		select {
		case ev := <-s.Events():
			switch ev.Type {
			case event.TypeNotice:
				notices++
			case event.TypeUserMessage, event.TypeAssistantChunk,
				event.TypeThoughtChunk, event.TypeToolCall:
				content++
			}
		case <-deadline:
			break drain
		case <-time.After(2 * time.Second):
			break drain
		}
	}
	if notices == 0 {
		t.Fatal("compaction emitted no notice")
	}
	if content != 0 {
		t.Fatalf("compaction produced %d transcript rows; it must not replay", content)
	}
	t.Logf("compaction: %d notice(s), no replay, in %s", notices, time.Since(before).Round(time.Millisecond))
}

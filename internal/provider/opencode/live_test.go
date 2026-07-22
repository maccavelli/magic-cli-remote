//go:build live_opencode

package opencode_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
)

// Run with: go test -tags live_opencode ./internal/provider/opencode/ -count=1 -timeout 90s
// Needs `opencode` in PATH; works without provider auth via OpenCode's free
// Zen models (default opencode/big-pickle).
func TestLiveOpencodePrompt(t *testing.T) {
	p := opencode.New(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	if s.AgentSessionID() == "" {
		t.Fatal("expected agent session id")
	}

	if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "Reply with exactly the word pong and nothing else."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	deadline := time.Now().Add(55 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed early")
			}
			t.Logf("event type=%s status=%s text=%q err=%q", ev.Type, ev.Status, ev.Text, ev.Error)
			if ev.Type == event.TypeTurnComplete {
				return
			}
			if ev.Type == event.TypeError {
				t.Fatalf("agent error: %s", ev.Error)
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("timeout waiting for turn_complete")
}

// Resume: a closed session must reload via ACP session/load with its
// provider-native id and retain conversational context.
func TestLiveOpencodeResume(t *testing.T) {
	p := opencode.New(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cwd := t.TempDir()

	s1, err := p.Start(ctx, provider.StartOptions{Name: "live-resume", CWD: cwd})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	agentID := s1.AgentSessionID()
	if agentID == "" {
		t.Fatal("expected agent session id")
	}
	if err := s1.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Remember this codeword: BLUEFISH. Reply with exactly: ok"}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	waitTurnComplete(t, s1, 55*time.Second)
	_ = s1.Close(context.Background())

	// New process, same agent session id → ACP session/load.
	s2, err := p.Start(ctx, provider.StartOptions{
		Name: "live-resume", CWD: cwd, AgentSessionID: agentID,
	})
	if err != nil {
		t.Fatalf("resume start: %v", err)
	}
	defer s2.Close(context.Background())
	if s2.AgentSessionID() != agentID {
		t.Fatalf("resumed agent id = %q, want %q", s2.AgentSessionID(), agentID)
	}
	if err := s2.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "What was the codeword? Reply with just the codeword."}}); err != nil {
		t.Fatalf("resume prompt: %v", err)
	}
	var reply strings.Builder
	deadline := time.Now().Add(55 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev, ok := <-s2.Events():
			if !ok {
				t.Fatal("events closed early")
			}
			if ev.Type == event.TypeAssistantChunk {
				reply.WriteString(ev.Text)
			}
			if ev.Type == event.TypeError {
				t.Fatalf("agent error: %s", ev.Error)
			}
			if ev.Type == event.TypeTurnComplete {
				if !strings.Contains(strings.ToUpper(reply.String()), "BLUEFISH") {
					t.Fatalf("resumed session lost context; reply = %q", reply.String())
				}
				return
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("timeout waiting for resumed turn")
}

// Remote permissions: with OpenCode configured to ask for bash and
// AlwaysApprove off, a tool call must surface a permission_request event, and
// RespondPermission must unblock the agent through to turn_complete.
func TestLiveOpencodePermissionRoundTrip(t *testing.T) {
	// Inline config forces an ask so the test does not depend on host config.
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"permission":{"bash":"ask"}}`)
	p := opencode.New(opencode.Config{AlwaysApprove: false})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "live-perm", CWD: t.TempDir()})
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
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed early")
			}
			t.Logf("event type=%s status=%s perm=%s text=%q err=%q",
				ev.Type, ev.Status, ev.PermissionID, ev.Text, ev.Error)
			switch ev.Type {
			case event.TypePermission:
				permID = ev.PermissionID
				if len(ev.Options) == 0 {
					t.Fatal("permission_request with no options")
				}
				// Pick an allow-style option like the phone would.
				opt := ev.Options[0].OptionID
				for _, o := range ev.Options {
					k := strings.ToLower(o.Kind + o.Name)
					if strings.Contains(k, "allow") || strings.Contains(k, "once") {
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
					t.Fatalf("turn completed without permission round-trip (responded=%v resolved=%v)", responded, resolved)
				}
				return
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatalf("timeout (responded=%v resolved=%v)", responded, resolved)
}

// Invalid model: Start must fail fast with a clear error, not hang or
// silently fall back to a different model.
func TestLiveOpencodeInvalidModel(t *testing.T) {
	p := opencode.New(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{
		Name: "live-bad-model", CWD: t.TempDir(),
		Model: "nonexistent/not-a-real-model",
	})
	if err == nil {
		defer s.Close(context.Background())
		t.Fatalf("expected error for invalid model, got session %s", s.AgentSessionID())
	}
	t.Logf("invalid model error (expected): %v", err)
	if !strings.Contains(err.Error(), "model") {
		t.Fatalf("error should mention the model: %v", err)
	}
}

// Cancel mid-turn must resolve the prompt as cancelled (benign turn_complete,
// no scary error event) — this backs the phone's stop button.
func TestLiveOpencodeCancel(t *testing.T) {
	p := opencode.New(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "live-cancel", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	if err := s.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Count slowly from 1 to 500, one number per line, thinking carefully about each."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	// Let the turn get going, then cancel.
	sawRunning := false
	warm := time.Now().Add(20 * time.Second)
	for time.Now().Before(warm) && !sawRunning {
		select {
		case ev := <-s.Events():
			if ev.Type == event.TypeAssistantChunk || ev.Type == event.TypeThoughtChunk {
				sawRunning = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !sawRunning {
		t.Fatal("turn never started streaming")
	}
	if err := s.Cancel(ctx); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed early")
			}
			if ev.Type == event.TypeError {
				t.Fatalf("cancel produced an error event: %s", ev.Error)
			}
			if ev.Type == event.TypeTurnComplete {
				t.Logf("turn_complete stop_reason=%s status=%s", ev.StopReason, ev.Status)
				return
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("timeout waiting for cancelled turn_complete")
}

// Two concurrent sessions in the same cwd must not corrupt or deadlock each
// other (OpenCode shares per-project storage between processes).
func TestLiveOpencodeConcurrentSessions(t *testing.T) {
	p := opencode.New(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cwd := t.TempDir()

	s1, err := p.Start(ctx, provider.StartOptions{Name: "live-c1", CWD: cwd})
	if err != nil {
		t.Fatalf("start s1: %v", err)
	}
	defer s1.Close(context.Background())
	s2, err := p.Start(ctx, provider.StartOptions{Name: "live-c2", CWD: cwd})
	if err != nil {
		t.Fatalf("start s2: %v", err)
	}
	defer s2.Close(context.Background())
	if s1.AgentSessionID() == s2.AgentSessionID() {
		t.Fatalf("sessions share an agent id: %s", s1.AgentSessionID())
	}

	if err := s1.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Reply with exactly the word: alpha"}}); err != nil {
		t.Fatalf("prompt s1: %v", err)
	}
	if err := s2.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Reply with exactly the word: bravo"}}); err != nil {
		t.Fatalf("prompt s2: %v", err)
	}
	waitTurnComplete(t, s1, 90*time.Second)
	waitTurnComplete(t, s2, 90*time.Second)
}

func waitTurnComplete(t *testing.T, s provider.Session, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed early")
			}
			if ev.Type == event.TypeError {
				t.Fatalf("agent error: %s", ev.Error)
			}
			if ev.Type == event.TypeTurnComplete {
				return
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("timeout waiting for turn_complete")
}

// The per-session model must round-trip through OpenCode's ACP "model"
// session config option without failing Start.
func TestLiveOpencodeModelSelection(t *testing.T) {
	p := opencode.New(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// A free Zen model that needs no provider auth (not the default).
	s, err := p.Start(ctx, provider.StartOptions{
		Name:  "live-model",
		CWD:   t.TempDir(),
		Model: "opencode/mimo-v2.5-free",
	})
	if err != nil {
		t.Fatalf("start with model: %v", err)
	}
	defer s.Close(context.Background())
	if s.AgentSessionID() == "" {
		t.Fatal("expected agent session id")
	}
}

// The prewarm pool must make a session create skip the engine cold start:
// with a spare armed, Start pays only session/new (~1s) instead of Bun boot +
// initialize + session/new (~4s measured on this host).
func TestLiveOpencodePrewarmFastCreate(t *testing.T) {
	p := opencode.New(opencode.Config{AlwaysApprove: true, Prewarm: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	p.EnsureWarm()
	// Give the spare time to spawn + initialize (~3s on this host).
	time.Sleep(8 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	started := time.Now()
	s, err := p.Start(ctx, provider.StartOptions{Name: "warm", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())
	elapsed := time.Since(started)
	t.Logf("warm create took %s", elapsed)
	if elapsed > 2500*time.Millisecond {
		t.Fatalf("warm create took %s, want <2.5s (spare not claimed?)", elapsed)
	}
	if s.AgentSessionID() == "" {
		t.Fatal("expected agent session id")
	}

	// The claimed session must actually work end to end.
	if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "Reply with exactly the word warm and nothing else."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	waitTurnComplete(t, s, 45*time.Second)
}

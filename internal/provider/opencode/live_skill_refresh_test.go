//go:build live_opencode

package opencode_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
)

// TestLiveDiagnosticsSurface proves the sanitized report works against a real
// engine and that nothing sensitive appears in it (MADR 0112 A6).
func TestLiveDiagnosticsSurface(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	cwd := t.TempDir()
	s, err := p.Start(ctx, provider.StartOptions{Name: "diagnostics-live", CWD: cwd})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	ds, ok := s.(provider.DiagnosticsSession)
	if !ok {
		t.Fatal("an OpenCode session does not implement DiagnosticsSession")
	}
	got, err := ds.Diagnostics(ctx)
	if err != nil {
		t.Fatalf("diagnostics: %v", err)
	}
	blob, _ := json.Marshal(got)
	for _, forbidden := range []string{cwd, "Bearer", "Authorization", "oauth", "/Users/"} {
		if strings.Contains(string(blob), forbidden) {
			t.Fatalf("live diagnostics leaked %q:\n%s", forbidden, blob)
		}
	}
	// Every MCP state must be from the closed vocabulary.
	for _, m := range got.MCP {
		switch m.State {
		case provider.MCPStateConnected, provider.MCPStateDisabled,
			provider.MCPStateFailed, provider.MCPStateNeedsAuth,
			provider.MCPStateNeedsRegistration, provider.MCPStateUnknown:
		default:
			t.Fatalf("unnormalized MCP state %q", m.State)
		}
	}
	t.Logf("live diagnostics: %d skill(s), %d lsp, %d formatter(s), %d mcp",
		len(got.Skills), len(got.LSP), len(got.Formatters), len(got.MCP))
}

// TestLiveSkillDiscoveryRefresh proves an idle refresh recycles the instance
// and makes a newly written skill discoverable, without losing session history.
//
// The test writes the skill file directly. That writer exists only here, inside
// a disposable temporary project, to establish cache state: production code and
// the phone never receive one (MADR 0112 A10, PLAN P7 step 5).
func TestLiveSkillDiscoveryRefresh(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	cwd := t.TempDir()
	s, err := p.Start(ctx, provider.StartOptions{Name: "skill-refresh-live", CWD: cwd})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	agentID := s.AgentSessionID()
	ds, _ := s.(provider.DiagnosticsSession)
	before, err := ds.Diagnostics(ctx)
	if err != nil {
		t.Fatalf("diagnostics before: %v", err)
	}

	// Establish cache state, then write a skill the engine has not seen.
	const skillName = "loopback-probe"
	dir := filepath.Join(cwd, ".opencode", "skills", skillName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + skillName + "\ndescription: A probe skill\n---\n\nProbe body.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	rs, ok := s.(provider.SkillRefreshSession)
	if !ok {
		t.Fatal("an OpenCode session does not implement SkillRefreshSession")
	}
	if err := rs.RefreshSkills(ctx); err != nil {
		t.Fatalf("refresh on an idle instance: %v", err)
	}

	after, err := ds.Diagnostics(ctx)
	if err != nil {
		t.Fatalf("diagnostics after: %v", err)
	}
	found := false
	for _, sk := range after.Skills {
		if sk.Name == skillName {
			found = true
		}
	}
	if !found {
		t.Fatalf("the new skill was not discovered after refresh (before=%d after=%d)",
			len(before.Skills), len(after.Skills))
	}
	// The native session survives the recycle.
	if s.AgentSessionID() != agentID {
		t.Fatalf("the session id changed across a refresh: %q -> %q", agentID, s.AgentSessionID())
	}
	t.Logf("refresh discovered %q; session %s preserved", skillName, agentID)
}

// TestLiveDirectShell proves the documented shell route works end to end and
// that its output arrives as ordinary tool events (MADR 0112 A9).
//
// It runs only `printf mcremote-shell-ok` in a temporary project. No model is
// invoked and no tokens are spent.
func TestLiveDirectShell(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	// Policy must be explicitly enabled, exactly as an operator would.
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true, AllowRemoteShell: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	cwd := t.TempDir()
	s, err := p.Start(ctx, provider.StartOptions{Name: "shell-live", CWD: cwd})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	sh, ok := s.(provider.ShellSession)
	if !ok {
		t.Fatal("an OpenCode session does not implement ShellSession")
	}

	// Drain events in the background so the tool states can be inspected.
	type toolEv struct{ status, text string }
	seen := make(chan toolEv, 64)
	stop := make(chan struct{})
	var once sync.Once
	stopDrain := func() { once.Do(func() { close(stop) }) }
	defer stopDrain()
	go func() {
		for {
			select {
			case ev := <-s.Events():
				if ev.Type == event.TypeToolCall || ev.Type == event.TypeToolUpdate {
					select {
					case seen <- toolEv{ev.Status, ev.Text}:
					default:
					}
				}
			case <-ctx.Done():
				return
			case <-stop:
				return
			}
		}
	}()

	if err := sh.Shell(ctx, "printf mcremote-shell-ok"); err != nil {
		t.Fatalf("shell: %v", err)
	}

	deadline := time.After(60 * time.Second)
	var sawCompleted bool
	var output string
collect:
	for {
		select {
		case ev := <-seen:
			if ev.status == "completed" {
				sawCompleted = true
				output = ev.text
				break collect
			}
		case <-deadline:
			break collect
		}
	}
	stopDrain()

	if !sawCompleted {
		t.Fatal("no completed tool state was observed for the shell command")
	}
	if !strings.Contains(output, "mcremote-shell-ok") {
		t.Fatalf("tool output = %q, want the command's stdout", output)
	}
	t.Logf("direct shell produced a completed tool state with the expected output")
}

// TestLiveDirectShellRefusedWhenDisabled proves the default daemon spawns
// nothing at all.
func TestLiveDirectShellRefusedWhenDisabled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// No AllowRemoteShell: the default.
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	marker := filepath.Join(t.TempDir(), "should-not-exist")
	s, err := p.Start(ctx, provider.StartOptions{Name: "shell-disabled-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	sh, ok := s.(provider.ShellSession)
	if !ok {
		t.Fatal("an OpenCode session does not implement ShellSession")
	}
	if err := sh.Shell(ctx, "touch "+marker); err == nil {
		t.Fatal("a disabled host ran a command")
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("a disabled host actually created the file")
	}
}

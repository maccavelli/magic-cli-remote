//go:build live_goose

package goose_test

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acphttp"
	"github.com/maccavelli/magic-cli-remote/internal/provider/goose"
)

// TestLiveGooseACPContract pins the non-token-bearing portion of the Goose
// v1.44.0 contract. Run this deliberately when upgrading Goose:
// go test -tags live_goose ./internal/provider/goose/ -run TestLiveGooseACPContract -count=1 -v
func TestLiveGooseACPContract(t *testing.T) {
	if _, err := exec.LookPath("goose"); err != nil {
		t.Skip("goose not in PATH")
	}
	t.Log("probing Goose ACP contract; expected Goose v1.44.0")
	p := goose.New(goose.Config{Config: acphttp.Config{AlwaysApprove: true}})
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cwd := t.TempDir()
	s, err := p.Start(ctx, provider.StartOptions{
		CWD:  cwd,
		Name: fmt.Sprintf("mcremote-live-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("start Goose v1.44.0 contract session: %v", err)
	}
	defer s.Close(context.Background())

	var caps *event.Capabilities
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for caps == nil {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed before session capabilities")
			}
			if ev.Type == event.TypeSessionCapabilities {
				caps = ev.Capabilities
			}
		case <-deadline.C:
			t.Fatal("timed out waiting for session capabilities")
		}
	}
	if caps == nil || !caps.Image || caps.Audio || !caps.LoadSession || !caps.EmbeddedContext ||
		!caps.ListSessions || !caps.CloseSession || !caps.MCPHTTP || caps.MCPSSE || caps.MCPACP {
		t.Fatalf("Goose v1.44.0 capabilities = %#v", caps)
	}

	lister, ok := any(p).(provider.AgentSessionLister)
	if !ok {
		t.Fatal("Goose provider does not implement AgentSessionLister")
	}
	listed, err := lister.ListAgentSessions(ctx)
	if err != nil {
		t.Fatalf("session/list: %v", err)
	}
	for _, entry := range listed {
		if entry.ID == "" {
			t.Fatalf("session/list returned an entry without an id: %#v", entry)
		}
	}
	// Goose 1.44.0 lists persisted conversations only: the just-created,
	// never-prompted session is correctly absent at this point. The prompted
	// disposable session in TestLiveGoosePrompt verifies list/load after
	// persistence without touching a user-owned historical session.
	for _, entry := range listed {
		if entry.ID == s.AgentSessionID() {
			t.Fatalf("Goose v1.44.0 unexpectedly listed never-prompted session %q", entry.ID)
		}
	}
}

// Run with: go test -tags live_goose ./internal/provider/goose/ -count=1 -timeout 90s
func TestLiveGoosePrompt(t *testing.T) {
	if _, err := exec.LookPath("goose"); err != nil {
		t.Skip("goose not in PATH")
	}
	p := goose.New(goose.Config{
		Config: acphttp.Config{
			AlwaysApprove: true,
		},
	})
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cwd := t.TempDir()

	s, err := p.Start(ctx, provider.StartOptions{
		CWD: cwd,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer s.Close(context.Background())

	if err := s.Prompt(ctx, []provider.Content{{
		Type: "text",
		Text: "Reply with exactly the word pong and nothing else.",
	}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	deadline := time.Now().Add(55 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events channel closed early")
			}
			t.Logf("event type=%s status=%s text=%q tool=%s err=%q",
				ev.Type, ev.Status, ev.Text, ev.ToolName, ev.Error)
			if ev.Type == event.TypeTurnComplete {
				goto verifyLoad
			}
			if ev.Type == event.TypeError {
				t.Fatalf("agent error: %s", ev.Error)
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("timeout waiting for turn_complete")

verifyLoad:
	lister, ok := any(p).(provider.AgentSessionLister)
	if !ok {
		t.Fatal("Goose provider does not implement AgentSessionLister")
	}
	listed, err := lister.ListAgentSessions(ctx)
	if err != nil {
		t.Fatalf("session/list after prompt: %v", err)
	}
	found := false
	for _, entry := range listed {
		if entry.ID == s.AgentSessionID() {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("session/list did not include prompted session %q", s.AgentSessionID())
	}
	loaded, err := p.Start(ctx, provider.StartOptions{
		CWD:            cwd,
		AgentSessionID: s.AgentSessionID(),
	})
	if err != nil {
		t.Fatalf("session/load %q: %v", s.AgentSessionID(), err)
	}
	if err := loaded.Close(context.Background()); err != nil {
		t.Fatalf("close loaded session: %v", err)
	}
}

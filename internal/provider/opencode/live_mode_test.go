//go:build live_opencode

package opencode_test

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
)

// Run with: go test -tags live_opencode ./internal/provider/opencode/ -run Mode -count=1
//
// Pins the two facts the mode mapping rests on, against a real engine: the
// primary agents are advertised as modes (hidden internals excluded), and the
// session accepts a switch to plan.
func TestLiveModesAdvertisedAndSwitchable(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "mode-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	var modes []event.SessionMode
	var current string
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && modes == nil {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed early")
			}
			if ev.Type == event.TypeMode && len(ev.Modes) > 0 {
				modes, current = ev.Modes, ev.CurrentModeID
			}
		case <-time.After(200 * time.Millisecond):
		}
	}
	if modes == nil {
		t.Fatal("no session_mode event with a mode list")
	}
	t.Logf("modes=%+v current=%q", modes, current)

	has := func(id string) bool {
		for _, m := range modes {
			if m.ID == id {
				return true
			}
		}
		return false
	}
	if !has("build") || !has("plan") {
		t.Fatalf("expected the built-in build/plan agents, got %+v", modes)
	}
	for _, internal := range []string{"compaction", "summary", "title", "explore"} {
		if has(internal) {
			t.Fatalf("hidden or subagent %q offered as a mode: %+v", internal, modes)
		}
	}
	if current != "build" {
		t.Fatalf("current = %q, want build (no agent requested at create)", current)
	}

	ms, ok := s.(provider.ModeSession)
	if !ok {
		t.Fatal("opencode session must implement provider.ModeSession")
	}
	if err := ms.SetMode(ctx, "plan"); err != nil {
		t.Fatalf("set plan mode: %v", err)
	}
	if err := ms.SetMode(ctx, "nope-not-a-mode"); err == nil {
		t.Error("unknown mode accepted")
	}
}

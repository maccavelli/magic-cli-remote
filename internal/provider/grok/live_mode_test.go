//go:build live_grok

package grok_test

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// Run with: go test -tags live_grok ./internal/provider/grok/ -run Mode -count=1
//
// Pins the assumption the static mode list rests on: grok advertises no modes of
// its own, yet honors session/set_mode and confirms the switch with a
// current_mode_update. If a future grok starts advertising modes, the fallback
// stops being used and this test's first assertion is what will notice.
func TestLiveGrokPlanModeSwitch(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "mode-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	// The advertised list at create (ours, since grok declares none).
	var modes []event.SessionMode
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && modes == nil {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed early")
			}
			if ev.Type == event.TypeMode && len(ev.Modes) > 0 {
				modes = ev.Modes
			}
		case <-time.After(200 * time.Millisecond):
		}
	}
	if len(modes) != 2 || modes[0].ID != "default" || modes[1].ID != "plan" {
		t.Fatalf("modes = %+v, want default+plan", modes)
	}

	ms, ok := s.(provider.ModeSession)
	if !ok {
		t.Fatal("grok session must implement provider.ModeSession")
	}
	if err := ms.SetMode(ctx, "plan"); err != nil {
		t.Fatalf("set plan mode: %v", err)
	}
	// Grok echoes the active mode; without that echo the daemon (and /plan)
	// could not track state.
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed early")
			}
			if ev.Type == event.TypeMode && ev.CurrentModeID == "plan" {
				// Ids outside our list must not reach grok: it accepts anything.
				if err := ms.SetMode(ctx, "bogus-xyz"); err == nil {
					t.Error("unknown mode accepted")
				}
				return
			}
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("no current_mode_update confirming plan mode")
}

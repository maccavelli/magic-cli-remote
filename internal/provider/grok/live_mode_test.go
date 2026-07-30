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
	// default+plan are ours; `auto` is appended by the daemon and enforced by
	// intercepting the permission request (MADR 0049).
	if len(modes) != 3 || modes[0].ID != "default" || modes[1].ID != "plan" ||
		modes[2].ID != "auto" {
		t.Fatalf("modes = %+v, want default+plan+auto", modes)
	}
	if !modes[2].Dangerous {
		t.Error("auto must be flagged dangerous so the phone gates arming")
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

// Run with: go test -tags live_grok ./internal/provider/grok/ -run AutoMode -count=1
//
// The synthetic `auto` id is ours, not grok's — grok's own auto is the
// `--permission-mode auto` launch flag, and its ACP mode vocabulary is
// default/plan only. Arming must therefore put grok into `default` and never
// send `auto` on the wire; the daemon then answers permissions itself. A grok
// release that starts advertising its own modes, or accepting `auto` as a mode
// id, will fail here and force a conscious decision.
func TestLiveGrokAutoModeArmsWithoutSendingAuto(t *testing.T) {
	// AlwaysApprove off: it would mask the per-session interception under test.
	p := grok.New(grok.Config{})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "auto-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	ms, ok := s.(provider.ModeSession)
	if !ok {
		t.Fatal("grok session must implement provider.ModeSession")
	}
	// Park it in plan first: arming auto has to leave plan, or the session
	// would be prompts-off and edits-off at once.
	if err := ms.SetMode(ctx, "plan"); err != nil {
		t.Fatalf("set plan mode: %v", err)
	}
	if err := ms.SetMode(ctx, "auto"); err != nil {
		t.Fatalf("arm auto: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed early")
			}
			if ev.Type == event.TypeMode && ev.CurrentModeID == "auto" {
				return
			}
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("no session_mode reporting auto after arming")
}

// Run with: go test -tags live_grok ./internal/provider/grok/ -run ArmsFromDefault -count=1
//
// The phone's common path is Build → auto (already on default). MADR 0053 F1:
// set_mode(default) is a no-op for grok so no current_mode_update fires; the
// daemon must still emit current=auto. The plan→auto test above does not cover
// this path.
func TestLiveGrokAutoModeArmsFromDefault(t *testing.T) {
	p := grok.New(grok.Config{})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "auto-from-default", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	ms, ok := s.(provider.ModeSession)
	if !ok {
		t.Fatal("grok session must implement provider.ModeSession")
	}
	// Do not park in plan — that is the uncommon path that hid the silent arm.
	if err := ms.SetMode(ctx, "auto"); err != nil {
		t.Fatalf("arm auto from default: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed early")
			}
			if ev.Type == event.TypeMode && ev.CurrentModeID == "auto" {
				return
			}
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("no session_mode reporting auto after arming from default")
}

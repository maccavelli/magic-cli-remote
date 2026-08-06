//go:build live_kilo

package kilo_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/kilo"
)

// permCWD prepares a session cwd whose project config forces bash to ask.
// Kilo reads project ./kilo.json (MADR 0075 §2.1); the env override
// KILO_CONFIG_CONTENT is also set by callers in case the fork honors it like
// OpenCode's OPENCODE_CONFIG_CONTENT.
func permCWD(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfg := map[string]any{"permission": map[string]string{"bash": "ask"}}
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "kilo.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestLivePermissionRoundTrip is the PD6 fixture: with bash set to "ask" and
// AlwaysApprove off, a tool call must surface permission_request over SSE, and
// RespondPermission(once) must unblock the agent through to turn_complete.
// Closes MADR 0075 Q10.
func TestLivePermissionRoundTrip(t *testing.T) {
	t.Setenv("KILO_CONFIG_CONTENT", `{"permission":{"bash":"ask"}}`)
	p := kilo.NewHTTP(kilo.Config{AlwaysApprove: false, Model: liveModel()})
	if !p.Ready() {
		t.Skip("kilo not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{Name: "kilo-perm", CWD: permCWD(t)})
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
				t.Logf("permission ask: id=%s title=%q options=%+v", permID, ev.Text, ev.Options)
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
				if permID == "" {
					t.Skip("model completed the turn without using a tool; " +
						"no permission was requested")
				}
				if !responded || !resolved {
					t.Fatalf("permission %s round-trip broke (responded=%v resolved=%v)",
						permID, responded, resolved)
				}
				return
			}
		case <-deadline:
			t.Fatalf("timeout (permission=%q responded=%v resolved=%v)",
				permID, responded, resolved)
		}
	}
}

// TestLivePermissionReject: rejecting the ask must close the turn without the
// tool's effect (the file the agent was told to create must not exist).
func TestLivePermissionReject(t *testing.T) {
	t.Setenv("KILO_CONFIG_CONTENT", `{"permission":{"bash":"ask"}}`)
	p := kilo.NewHTTP(kilo.Config{AlwaysApprove: false, Model: liveModel()})
	if !p.Ready() {
		t.Skip("kilo not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	cwd := permCWD(t)
	s, err := p.Start(ctx, provider.StartOptions{Name: "kilo-perm-reject", CWD: cwd})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())
	ps := s.(provider.PermissionSession)

	if err := s.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Use your bash tool to run exactly: touch rejected-probe.txt. Then reply done."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	var permID string
	deadline := time.After(180 * time.Second)
	for {
		select {
		case ev := <-s.Events():
			switch ev.Type {
			case event.TypePermission:
				permID = ev.PermissionID
				if err := ps.RespondPermission(ctx, permID, "", true); err != nil {
					t.Fatalf("reject permission: %v", err)
				}
			case event.TypeTurnComplete:
				if permID == "" {
					t.Skip("model completed the turn without using a tool")
				}
				if _, err := os.Stat(filepath.Join(cwd, "rejected-probe.txt")); err == nil {
					t.Fatal("rejected tool call still created the file")
				}
				return
			}
		case <-deadline:
			t.Fatalf("timeout (permission=%q)", permID)
		}
	}
}

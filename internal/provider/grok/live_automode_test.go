//go:build live_grok

package grok_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// Run with: go test -tags live_grok ./internal/provider/grok/ -run AutoDiscrimination
//
// The pair that proves MADR 0049's daemon-side auto mode actually works, rather
// than merely being plumbed. Both halves are load-bearing:
//
//	unarmed  must prompt        — otherwise the armed half proves nothing
//	armed    must not prompt,
//	         and must still write — a session that is silently blocked also
//	                                emits no prompt, and would pass a
//	                                one-sided check for the wrong reason
//
// `permission_mode: default` is required: grok only routes approvals through
// ACP session/request_permission in a prompting mode. This test was impossible
// before MADR 0050 Phase A, because the flag was placed where grok rejects it,
// so grok always ran in its host default (permissive here) and an earlier
// investigation wrongly concluded auto was a no-op for grok.
func TestLiveGrokAutoDiscriminationPair(t *testing.T) {
	for _, arm := range []bool{false, true} {
		name := "unarmed"
		if arm {
			name = "armed"
		}
		t.Run(name, func(t *testing.T) {
			p := grok.New(grok.Config{PermissionMode: "default"})
			if !p.Ready() {
				t.Skip("grok not in PATH")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			dir := t.TempDir()
			s, err := p.Start(ctx, provider.StartOptions{Name: "pair", CWD: dir})
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			defer s.Close(context.Background())
			if arm {
				if err := s.(provider.ModeSession).SetMode(ctx, "auto"); err != nil {
					t.Fatalf("arm: %v", err)
				}
			}
			if err := s.Prompt(ctx, []provider.Content{{Type: "text",
				Text: "Create a file named probe.txt containing OK. Do it now, then stop."}}); err != nil {
				t.Fatalf("prompt: %v", err)
			}
			prompts := 0
			deadline := time.Now().Add(90 * time.Second)
			done := false
			for !done && time.Now().Before(deadline) {
				select {
				case ev, ok := <-s.Events():
					if !ok {
						done = true
						break
					}
					switch ev.Type {
					case event.TypePermission, event.TypeQuestion:
						prompts++
					case event.TypeTurnComplete:
						done = true
					}
				case <-time.After(300 * time.Millisecond):
				}
			}
			wrote := false
			if _, err := os.Stat(filepath.Join(dir, "probe.txt")); err == nil {
				wrote = true
			}
			t.Logf("arm=%-5v prompts=%d wrote=%v turnDone=%v", arm, prompts, wrote, done)
			if arm {
				if prompts != 0 {
					t.Errorf("armed auto must not prompt; got %d", prompts)
				}
				if !wrote {
					t.Error("armed auto must let the agent complete the write")
				}
			} else if prompts == 0 {
				t.Error("unarmed must prompt, else the pair proves nothing")
			}
		})
	}
}

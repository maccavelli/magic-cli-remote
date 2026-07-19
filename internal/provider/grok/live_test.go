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

// Run with: go test -tags live_grok ./internal/provider/grok/ -count=1 -timeout 90s
func TestLiveGrokPrompt(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

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

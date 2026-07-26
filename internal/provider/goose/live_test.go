//go:build live_goose

package goose_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/goose"
)

// Run with: go test -tags live_goose ./internal/provider/goose/ -count=1 -timeout 90s
func TestLiveGoosePrompt(t *testing.T) {
	if _, err := exec.LookPath("goose"); err != nil {
		t.Skip("goose not in PATH")
	}
	p := goose.New(goose.Config{
		AlwaysApprove: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{
		CWD: t.TempDir(),
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

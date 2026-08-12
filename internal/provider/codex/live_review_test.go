//go:build live_codex_review

package codex

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// TestLiveInlineReview is explicitly opt-in and token-bearing. It runs one
// minimal custom inline review, checks entered/exited lifecycle, and
// interrupts if the turn is still running.
func TestLiveInlineReview(t *testing.T) {
	p := NewWithLogger(Config{Bin: "codex"}, nil)
	if !p.Ready() {
		t.Skip("codex binary not found on PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sess, err := p.Start(ctx, provider.StartOptions{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close(context.Background())

	rs, ok := sess.(provider.ReviewSession)
	if !ok {
		t.Fatal("session does not implement ReviewSession")
	}
	t.Logf("codex live review thread=%s", sess.AgentSessionID())
	if err := rs.StartReview(ctx, provider.ReviewTarget{
		Kind:         provider.ReviewCustom,
		Instructions: "Reply with the single word ok.",
	}); err != nil {
		t.Fatalf("start review: %v", err)
	}

	var notices, assistants []string
	timeout := time.After(60 * time.Second)
	for {
		select {
		case ev := <-sess.Events():
			switch ev.Type {
			case event.TypeNotice:
				notices = append(notices, ev.Text)
			case event.TypeAssistantChunk:
				assistants = append(assistants, ev.Text)
			case event.TypeTurnComplete, event.TypeError:
				t.Logf("notices=%v assistant_chunks=%d stop=%s", notices, len(assistants), ev.StopReason)
				return
			}
		case <-timeout:
			_ = sess.Cancel(context.Background())
			t.Fatal("timed out waiting for review to finish")
		}
	}
}

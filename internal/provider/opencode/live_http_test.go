//go:build live_opencode

package opencode_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
)

// Run with: go test -tags live_opencode ./internal/provider/opencode/ -count=1 -timeout 300s
func TestLiveHTTPPromptStream(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "http-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())
	if s.AgentSessionID() == "" {
		t.Fatal("expected opencode session id")
	}

	// Second create must be near-instant (shared engine already up).
	started := time.Now()
	s2, err := p.Start(ctx, provider.StartOptions{Name: "http-live-2", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	if e := time.Since(started); e > 2*time.Second {
		t.Fatalf("second create took %s, want <2s on a shared engine", e)
	}
	_ = s2.Close(context.Background())

	if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "Reply with exactly the word pong and nothing else."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	var sawChunk, sawComplete bool
	var textAll strings.Builder
	deadline := time.After(120 * time.Second)
	for !sawComplete {
		select {
		case ev := <-s.Events():
			switch ev.Type {
			case event.TypeAssistantChunk:
				sawChunk = true
				textAll.WriteString(ev.Text)
			case event.TypeTurnComplete:
				sawComplete = true
				if ev.StopReason != "end_turn" {
					t.Fatalf("stop reason = %q", ev.StopReason)
				}
			case event.TypeError:
				t.Fatalf("agent error: %s", ev.Error)
			}
		case <-deadline:
			t.Fatal("timeout waiting for turn completion")
		}
	}
	if !sawChunk {
		t.Fatal("no assistant chunks streamed")
	}
	if !strings.Contains(strings.ToLower(textAll.String()), "pong") {
		t.Fatalf("reply %q does not contain pong", textAll.String())
	}
}

// Resume must re-attach to the server-side session with context intact —
// no engine replay cost, prior conversation available to the model.
func TestLiveHTTPResume(t *testing.T) {
	p := opencode.NewHTTP(opencode.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("opencode not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	cwd := t.TempDir()

	s, err := p.Start(ctx, provider.StartOptions{Name: "resume-live", CWD: cwd})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "Remember the codeword SEAGLASS. Reply OK."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	waitComplete(t, s, 120*time.Second)
	ocID := s.AgentSessionID()
	_ = s.Close(context.Background())

	started := time.Now()
	s2, err := p.Start(ctx, provider.StartOptions{
		Name: "resume-live", CWD: cwd,
		LocalSessionID: s.ID(), AgentSessionID: ocID,
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	defer s2.Close(context.Background())
	if e := time.Since(started); e > 3*time.Second {
		t.Fatalf("resume took %s, want <3s", e)
	}

	// The replayed ring must contain the prior conversation.
	drainDeadline := time.After(5 * time.Second)
	var sawReplay bool
drain:
	for {
		select {
		case ev := <-s2.Events():
			if ev.Replay && strings.Contains(ev.Text, "SEAGLASS") {
				sawReplay = true
			}
		case <-drainDeadline:
			break drain
		}
	}
	if !sawReplay {
		t.Fatal("resume did not replay prior conversation into history")
	}

	if err := s2.Prompt(ctx, []provider.Content{{Type: "text", Text: "What was the codeword? Reply with just the codeword."}}); err != nil {
		t.Fatalf("prompt after resume: %v", err)
	}
	var text strings.Builder
	deadline := time.After(120 * time.Second)
	for {
		select {
		case ev := <-s2.Events():
			if ev.Type == event.TypeAssistantChunk && !ev.Replay {
				text.WriteString(ev.Text)
			}
			if ev.Type == event.TypeTurnComplete && !ev.Replay {
				if !strings.Contains(strings.ToUpper(text.String()), "SEAGLASS") {
					t.Fatalf("resumed session forgot the codeword; reply=%q", text.String())
				}
				return
			}
			if ev.Type == event.TypeError {
				t.Fatalf("agent error: %s", ev.Error)
			}
		case <-deadline:
			t.Fatal("timeout waiting for resumed turn")
		}
	}
}

func waitComplete(t *testing.T, s provider.Session, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == event.TypeError {
				t.Fatalf("agent error: %s", ev.Error)
			}
			if ev.Type == event.TypeTurnComplete {
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting for turn_complete")
		}
	}
}

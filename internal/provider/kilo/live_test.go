//go:build live_kilo

package kilo_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/kilo"
)

// liveModel is the model live turns run with. openrouter/free is the proven
// spike model on this host (env OPENROUTER_API_KEY); override with
// MCREMOTE_LIVE_KILO_MODEL to exercise the Gateway path (e.g. empty string
// uses the engine default — kilo-auto/balanced when Gateway-authenticated).
func liveModel() string {
	if v, ok := os.LookupEnv("MCREMOTE_LIVE_KILO_MODEL"); ok {
		return v
	}
	return "openrouter/openrouter/free"
}

// Run with: go test -tags live_kilo ./internal/provider/kilo/ -count=1 -timeout 600s
func TestLivePromptStream(t *testing.T) {
	p := kilo.NewHTTP(kilo.Config{AlwaysApprove: true, Model: liveModel()})
	if !p.Ready() {
		t.Skip("kilo not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "kilo-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())
	if s.AgentSessionID() == "" {
		t.Fatal("expected kilo session id")
	}

	// Second create must be near-instant (shared engine already up, MADR 0075 D2).
	started := time.Now()
	s2, err := p.Start(ctx, provider.StartOptions{Name: "kilo-live-2", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	if e := time.Since(started); e > 2*time.Second {
		t.Fatalf("second create took %s, want <2s on a shared engine", e)
	}
	_ = s2.Close(context.Background())

	if err := s.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Reply with exactly the word PONG and nothing else."}}); err != nil {
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

// Resume must re-attach to the server-side session (engine keeps state).
func TestLiveResumeReplay(t *testing.T) {
	p := kilo.NewHTTP(kilo.Config{AlwaysApprove: true, Model: liveModel()})
	if !p.Ready() {
		t.Skip("kilo not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	cwd := t.TempDir()
	s, err := p.Start(ctx, provider.StartOptions{Name: "kilo-resume", CWD: cwd})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	agentID := s.AgentSessionID()
	localID := s.ID()
	_ = s.Close(context.Background())

	s2, err := p.Start(ctx, provider.StartOptions{
		Name: "kilo-resume", CWD: cwd,
		LocalSessionID: localID, AgentSessionID: agentID,
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	defer s2.Close(context.Background())
	if s2.AgentSessionID() != agentID {
		t.Fatalf("resumed id %q != %q", s2.AgentSessionID(), agentID)
	}
}

// Cancel mid-turn must resolve the turn benignly (turn_complete, no error).
func TestLiveCancel(t *testing.T) {
	p := kilo.NewHTTP(kilo.Config{AlwaysApprove: true, Model: liveModel()})
	if !p.Ready() {
		t.Skip("kilo not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{Name: "kilo-cancel", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	if err := s.Prompt(ctx, []provider.Content{{Type: "text",
		Text: "Count from 1 to 500 slowly, one number per line."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	// Let the turn get going before cancelling.
	time.Sleep(3 * time.Second)
	if err := s.Cancel(ctx); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	deadline := time.After(90 * time.Second)
	for {
		select {
		case ev := <-s.Events():
			switch ev.Type {
			case event.TypeTurnComplete:
				return // cancelled or end_turn both acceptable turn closures
			case event.TypeError:
				t.Fatalf("cancel produced error event: %s", ev.Error)
			}
		case <-deadline:
			t.Fatal("timeout waiting for cancelled turn to close")
		}
	}
}

//go:build live_codex

package codex

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// TestLiveInitializeConnect verifies the engine starts over stdio,
// completes initialize/initialized, and the daemon can shut it down
// cleanly. Requires a working `codex` binary on PATH.
func TestLiveInitializeConnect(t *testing.T) {
	cfg := Config{Bin: "codex"}
	p := NewWithLogger(cfg, nil)
	if !p.Ready() {
		t.Skip("codex binary not found on PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fr, err := p.ensureEngine(ctx)
	if err != nil {
		t.Fatalf("engine start: %v", err)
	}
	_ = fr

	p.Shutdown()
	t.Log("engine started and shut down cleanly")
}

// TestLiveStartSession verifies a fresh Codex thread can be created without
// sending a model turn. It is the live counterpart to the phone's New session
// action and requires a working `codex` binary on PATH.
func TestLiveStartSession(t *testing.T) {
	p := NewWithLogger(Config{Bin: "codex"}, nil)
	if !p.Ready() {
		t.Skip("codex binary not found on PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sess, err := p.Start(ctx, provider.StartOptions{})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer sess.Close(context.Background())

	if sess.AgentSessionID() == "" {
		t.Fatal("agent session id is empty after start")
	}
}

// TestLiveModelList verifies model/list returns models from a live engine.
func TestLiveModelList(t *testing.T) {
	cfg := Config{Bin: "codex"}
	p := NewWithLogger(cfg, nil)
	if !p.Ready() {
		t.Skip("codex binary not found on PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	catalog, err := p.ListModels(ctx)
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(catalog.Options) == 0 {
		t.Error("expected at least one model")
	}
	for _, opt := range catalog.Options {
		t.Logf("model: id=%s label=%s defaults=%v", opt.ID, opt.Label, catalog.DefaultIDs)
	}
	p.Shutdown()
}

// TestLiveThreadLifecycle creates a thread, runs a cheap turn, and cleans up.
func TestLiveThreadLifecycle(t *testing.T) {
	cfg := Config{Bin: "codex"}
	p := NewWithLogger(cfg, nil)
	if !p.Ready() {
		t.Skip("codex binary not found on PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sess, err := p.Start(ctx, provider.StartOptions{})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer sess.Close(context.Background())

	cs, ok := sess.(*session)
	if !ok {
		t.Fatal("session is not *session")
	}
	if cs.agentID == "" {
		t.Fatal("agent session id is empty after start")
	}
	t.Logf("thread id: %s", cs.agentID)

	if err := sess.Prompt(ctx, []provider.Content{{Text: "Reply with exactly 'hello' and nothing else."}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	timeout := time.After(30 * time.Second)
	for {
		select {
		case ev := <-sess.Events():
			if ev.Type == event.TypeTurnComplete {
				t.Logf("turn completed: status=%s", ev.Status)
				goto done
			}
			if ev.Type == event.TypeError {
				t.Logf("turn error: %s", ev.Error)
				goto done
			}
			if ev.Type == event.TypeAssistantChunk {
				t.Logf("assistant chunk: %s", ev.Text)
			}
		case <-timeout:
			t.Fatal("timed out waiting for turn completion")
		}
	}
done:
	p.Shutdown()
}

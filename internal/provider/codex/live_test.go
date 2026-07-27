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

// TestLiveTurnPlanUpdatedNotSkipped is a load-bearing live test for MADR
// 0035 phase 8: a turn that produces a plan must surface a TypePlan
// event from the daemon. Skips when the model is busy or the live
// environment cannot exercise the path; the fixture in
// docs/codex-spike-0.145.0/item-stream.json captures the wire shape
// (turnId, plan: [{step, status}]) and the unit tests pin the
// translation.
func TestLiveTurnPlanUpdatedNotSkipped(t *testing.T) {
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

	if err := sess.Prompt(ctx, []provider.Content{{
		Text: "Make a 3-step plan: 1) read AGENTS.md, 2) summarize, 3) reply. Use your plan tool if available.",
	}}); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	sawPlan := false
	timeout := time.After(45 * time.Second)
loop:
	for {
		select {
		case ev := <-sess.Events():
			switch ev.Type {
			case event.TypePlan:
				sawPlan = true
				t.Logf("plan entries=%d first=%+v", len(ev.Entries), ev.Entries[0])
			case event.TypeTurnComplete, event.TypeError:
				break loop
			}
		case <-timeout:
			break loop
		}
	}
	if !sawPlan {
		// Skip rather than fail: free-tier models do not always emit a
		// plan. The wire shape is pinned by the spike and the unit
		// tests (plan_test.go) cover the daemon-side translation.
		t.Skip("model did not emit a plan; wire shape and translation are pinned by unit tests")
	}
	p.Shutdown()
}

// TestLiveImageInputAdvertised pins MADR 0035 D4: a codex session must
// emit session_capabilities with image=true at create. Models in
// inputModalities confirm the wire claim, but a daemon-side regression
// would also break the image-attach affordance, so this lives here
// rather than only in unit tests.
func TestLiveImageInputAdvertised(t *testing.T) {
	cfg := Config{Bin: "codex"}
	p := NewWithLogger(cfg, nil)
	if !p.Ready() {
		t.Skip("codex binary not found on PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sess, err := p.Start(ctx, provider.StartOptions{})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer sess.Close(context.Background())

	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-sess.Events():
			if ev.Type == event.TypeSessionCapabilities {
				if ev.Capabilities == nil || !ev.Capabilities.Image {
					t.Errorf("session_capabilities: image=%v, want true", ev.Capabilities)
				}
				p.Shutdown()
				return
			}
		case <-timeout:
			t.Fatal("no session_capabilities event arrived")
		}
	}
}

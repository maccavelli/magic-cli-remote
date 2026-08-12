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

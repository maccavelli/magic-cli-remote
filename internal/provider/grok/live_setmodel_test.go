//go:build live_grok

package grok_test

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// Run with: go test -tags live_grok ./internal/provider/grok/ -run SetModel -count=1
//
// Pins session/set_model against grok 1.0.3 (1a29d5bc12d4): accepts grok-4.6
// and grok-4.5; rejects grok-code-fast-1, grok-build, and unknown ids
// (MADR 0081 T-D2).
func TestLiveGrokSetModelWireContract(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "setmodel-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	ms, ok := s.(provider.ModelSession)
	if !ok {
		t.Fatal("grok session must implement provider.ModelSession")
	}

	if err := ms.SetModel(ctx, "grok-4.6"); err != nil {
		t.Fatalf("SetModel(grok-4.6) failed: %v", err)
	}
	if err := ms.SetModel(ctx, "grok-4.5"); err != nil {
		t.Fatalf("SetModel(grok-4.5) failed: %v", err)
	}

	for _, id := range []string{"grok-nonexistent-999", "grok-code-fast-1", "grok-build"} {
		err = ms.SetModel(ctx, id)
		if err == nil {
			t.Fatalf("SetModel(%s) succeeded, want unknown model id error", id)
		}
		t.Logf("SetModel(%s) expectedly failed: %v", id, err)
	}
}

// T-D4: existing CloseSession + process kill still succeeds on 1.0.3.
func TestLiveGrokCloseSucceeds(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "close-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

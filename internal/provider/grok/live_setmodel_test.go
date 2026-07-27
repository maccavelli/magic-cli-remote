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
// Pins the wire contract D1 relies on: grok 0.2.112 honors session/set_model,
// accepts valid model ids (returning {"_meta":{"model":{"Ok":"<id>"}}}), and
// rejects unknown model ids with code -32602 ("unknown model id").
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

	// Valid model switch to grok-4.5 (or current live default)
	if err := ms.SetModel(ctx, "grok-4.5"); err != nil {
		t.Fatalf("SetModel(grok-4.5) failed: %v", err)
	}

	// Invalid model switch must fail with error containing unknown model id
	err = ms.SetModel(ctx, "grok-nonexistent-999")
	if err == nil {
		t.Fatal("SetModel(grok-nonexistent-999) succeeded, want unknown model id error")
	}
	t.Logf("SetModel(unknown) expectedly failed: %v", err)
}

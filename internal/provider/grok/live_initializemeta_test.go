//go:build live_grok

package grok_test

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// Run with: go test -tags live_grok ./internal/provider/grok/ -run InitializeMeta -count=1
//
// Pins the wire contract D2 relies on: grok 0.2.112 initialize response contains
// _meta.modelState with currentModelId and availableModels.
func TestLiveGrokInitializeMetaWireContract(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "initmeta-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	// ListModels on provider should now leverage live catalog once session initialized
	cat, err := p.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	t.Logf("ListModels returned %d options, default=%v, source=%v", len(cat.Options), cat.DefaultIDs, cat.Source)
	if len(cat.Options) == 0 {
		t.Fatal("ListModels returned empty options")
	}
	if cat.Source != picker.SourceLive && cat.Source != picker.SourceMerged {
		t.Errorf("ListModels source = %v, want SourceLive or SourceMerged", cat.Source)
	}
}

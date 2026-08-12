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
// Pins initialize _meta.modelState against grok 1.0.3 (1a29d5bc12d4):
// live catalog includes grok-4.6; dual-default effort collapses off xhigh
// (MADR 0081 T-D3).
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

	var grok46 *picker.Option
	for i := range cat.Options {
		if cat.Options[i].ID == "grok-4.6" {
			grok46 = &cat.Options[i]
			break
		}
	}
	if grok46 == nil {
		t.Fatal("live catalog missing grok-4.6")
	}
	if len(grok46.ThinkingLevels) == 0 {
		t.Log("grok-4.6 has no ThinkingLevels (supportsReasoningEffort=false); skip xhigh pin")
		return
	}
	var hasXHigh bool
	for _, l := range grok46.ThinkingLevels {
		if l.ID == "xhigh" {
			hasXHigh = true
			break
		}
	}
	if !hasXHigh {
		t.Error("grok-4.6 ThinkingLevels missing xhigh")
	}
	if d := picker.DefaultThinkingLevel(grok46.ThinkingLevels); d == "xhigh" {
		t.Errorf("default thinking = xhigh; NormalizeThinkingLevels must keep high")
	}
}

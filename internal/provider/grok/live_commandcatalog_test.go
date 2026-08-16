//go:build live_grok

package grok_test

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// Run with: go test -tags live_grok ./internal/provider/grok/ -run CommandCatalog -count=1
//
// Pins D3 against grok 1.0.4: asserts that grok advertises deep-research,
// workflow, and goal.
func TestLiveGrokCommandCatalogContainsDeepResearchAndWorkflow(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{Name: "cmdcat-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	advertised := waitForCommands(t, s, 30*time.Second)
	t.Logf("grok advertised commands: %v", advertised)

	for _, req := range []string{"deep-research", "workflow", "goal", "review", "loop"} {
		if !slicesContainsFold(advertised, req) {
			t.Errorf("grok advertised commands missing %q", req)
		}
	}
}

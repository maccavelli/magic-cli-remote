//go:build live_grok

package grok_test

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// TestLiveGrokColdCatalog is the acceptance test for MADR 0043 D7: the
// create-session picker must show the agent's real models before any session
// exists, not the hardcoded floor.
//
// Run: go test -tags live_grok ./internal/provider/grok/ -run TestLiveGrokColdCatalog -count=1 -v
func TestLiveGrokColdCatalog(t *testing.T) {
	p := grok.New(grok.Config{AlwaysApprove: true})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	start := time.Now()
	cat, err := p.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	first := time.Since(start)
	t.Logf("cold catalog: source=%s n=%d defaults=%v took=%s",
		cat.Source, len(cat.Options), cat.DefaultIDs, first.Round(time.Millisecond))
	for _, o := range cat.Options {
		t.Logf("  %s | %s", o.ID, o.Label)
	}

	if cat.Source == picker.SourceStatic {
		t.Fatal("cold catalog is the static floor: the initialize harvest did not run " +
			"(no session had populated the cache, so this is the path D7 added)")
	}
	if len(cat.Options) == 0 {
		t.Fatal("cold catalog is empty")
	}
	if len(cat.DefaultIDs) == 0 {
		t.Error("no default model: the agent's currentModelId was not carried through")
	}
	if !cat.AllowCustom {
		t.Error("allow_custom must stay on so an older install's ids remain typeable")
	}

	// The live catalog must not be padded with the static floor: grok's own
	// availableModels is short, and the floor names models it no longer accepts.
	if len(cat.Options) > 3 {
		t.Errorf("cold catalog has %d options; the live list is being merged with "+
			"the static floor, which offers models the agent refuses", len(cat.Options))
	}

	// Second call is cached.
	start = time.Now()
	if _, err := p.ListModels(ctx); err != nil {
		t.Fatalf("second ListModels: %v", err)
	}
	second := time.Since(start)
	t.Logf("second open: %s (cached)", second.Round(time.Microsecond))
	if second > 100*time.Millisecond {
		t.Errorf("second open took %s: the catalog cache is not engaging", second)
	}
}

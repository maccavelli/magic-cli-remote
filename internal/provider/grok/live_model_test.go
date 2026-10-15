//go:build live_grok

package grok_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
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

	// The live catalog must be grok's own list, not padded with the static
	// floor. A count could not tell once the floor fell inside grok's list, so
	// compare with what a session advertises, as sets
	// (0194-MADR-check-the-grok-catalog-against-what-grok-advertises.md, D2).
	// The unit test TestLiveCatalogIsNotPaddedWithTheFloor guards the rule
	// without an agent.
	catalogIDs := make([]string, 0, len(cat.Options))
	for _, o := range cat.Options {
		catalogIDs = append(catalogIDs, o.ID)
	}
	if sessionIDs, ok := sessionModelValues(ctx, t, p); ok {
		slices.Sort(catalogIDs)
		slices.Sort(sessionIDs)
		if !slices.Equal(catalogIDs, sessionIDs) {
			t.Errorf("cold catalog %v differs from the session's model option %v: "+
				"the catalog is not grok's own list", catalogIDs, sessionIDs)
		}
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

// sessionModelValues starts a session without a prompt, so it spends no
// tokens, and returns the values of the `model` config option its
// session_config event advertises. grok 1.0.50 sends one at session start; an
// older grok without config options reports false, and the caller skips the
// comparison.
func sessionModelValues(ctx context.Context, t *testing.T, p *grok.Provider) ([]string, bool) {
	t.Helper()
	s, err := p.Start(ctx, provider.StartOptions{Name: "catalog-live", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start a session to read its model option: %v", err)
	}
	defer s.Close(context.Background())

	wait := time.After(30 * time.Second)
	for {
		select {
		case ev := <-s.Events():
			if ev.Type != event.TypeSessionConfig {
				continue
			}
			for _, o := range ev.ConfigOptions {
				if o.ID != "model" {
					continue
				}
				ids := make([]string, 0, len(o.Values))
				for _, v := range o.Values {
					ids = append(ids, v.ID)
				}
				t.Logf("session model option: %v", ids)
				return ids, true
			}
		case <-wait:
			t.Log("no session_config event with a model option within 30s; " +
				"skipping the comparison with the session's model list")
			return nil, false
		}
	}
}

//go:build live_kilo

package kilo_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/kilo"
)

// The catalog defects behind MADR 0096 were all invisible to the unit suite:
// each one depended on the engine's real provider order, its real per-model
// metadata, or Go map iteration over a 295-entry map. Fixtures can pin the
// logic; only a live engine pins the assumptions the logic rests on.
//
// Run with: go test -tags live_kilo ./internal/provider/kilo/ -count=1 -timeout 600s

func liveCatalogProvider(t *testing.T) *httpagent.Provider {
	t.Helper()
	p := kilo.NewHTTP(kilo.Config{})
	if !p.Ready() {
		t.Skip("kilo not in PATH")
	}
	return p
}

func liveIDs(cat picker.Catalog) []string {
	out := make([]string, 0, len(cat.Options))
	for _, o := range cat.Options {
		out = append(out, o.ID)
	}
	return out
}

// TestLiveDefaultCatalogOffersGatewayRouters pins MADR 0096 D2. The engine
// lists Kilo Gateway last, so before the fix the option cap cut all 295 of its
// models: the default catalog held exactly one Kilo row and no auto-router the
// user could choose.
func TestLiveDefaultCatalogOffersGatewayRouters(t *testing.T) {
	p := liveCatalogProvider(t)
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cat, err := p.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	routers := 0
	for _, id := range liveIDs(cat) {
		if strings.HasPrefix(id, "kilo/kilo-auto/") {
			routers++
		}
	}
	t.Logf("default catalog: %d options, %d kilo-auto routers, truncated=%v",
		len(cat.Options), routers, cat.Truncated)
	if routers == 0 {
		t.Error("default catalog offers no kilo-auto router; Gateway models were capped out again")
	}
}

// TestLiveKiloCatalogIsDeterministic pins MADR 0096 D4. Kilo stamps every
// Gateway model with the same release_date, so a stable sort resolves ties by
// source order — and the source is a Go map. Three consecutive calls once
// differed in 293+ of 295 positions.
func TestLiveKiloCatalogIsDeterministic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// A fresh provider per run, because one provider memoizes the catalog and
	// would answer from cache — which proves nothing about the ordering. Each
	// provider builds the list from its own decode of the engine's response,
	// which is where the map-iteration nondeterminism lived.
	var first []string
	for i := 0; i < 3; i++ {
		p := liveCatalogProvider(t)
		cat, err := p.ListModelsFor(ctx, "kilo")
		if err != nil {
			p.Shutdown()
			t.Fatalf("run %d: %v", i, err)
		}
		ids := liveIDs(cat)
		p.Shutdown()

		if len(ids) == 0 {
			t.Skip("engine reports no kilo models (logged out?)")
		}
		if i == 0 {
			first = ids
			t.Logf("kilo catalog: %d options, first 5: %v", len(ids), ids[:min(5, len(ids))])
			continue
		}
		if strings.Join(ids, ",") != strings.Join(first, ",") {
			diff := 0
			for n := range ids {
				if n < len(first) && ids[n] != first[n] {
					diff++
				}
			}
			t.Fatalf("run %d differs from run 0 in %d of %d positions", i, diff, len(ids))
		}
	}
}

// TestLiveRecommendedRoutersRankFirst pins MADR 0096 D5, including the field
// it depends on: a Kilo release that stops sending recommendedIndex fails here
// rather than quietly scattering the routers through 295 same-dated models.
func TestLiveRecommendedRoutersRankFirst(t *testing.T) {
	p := liveCatalogProvider(t)
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cat, err := p.ListModelsFor(ctx, "kilo")
	if err != nil {
		t.Fatalf("ListModelsFor: %v", err)
	}
	if len(cat.Options) == 0 {
		t.Skip("engine reports no kilo models (logged out?)")
	}
	seenRecommended := false
	for i, o := range cat.Options {
		_, ok := o.Meta[picker.MetaRecommendedIndex]
		if ok {
			seenRecommended = true
			continue
		}
		// The first unrecommended option must come after every recommended
		// one; anything recommended below it is a ranking regression.
		for _, rest := range cat.Options[i+1:] {
			if _, isRec := rest.Meta[picker.MetaRecommendedIndex]; isRec {
				t.Fatalf("recommended %s ranks below unrecommended %s", rest.ID, o.ID)
			}
		}
		break
	}
	if !seenRecommended {
		t.Error("no kilo model carries recommendedIndex; the engine dropped the field (MADR 0096 D5)")
	}
	if !hasID(cat, "kilo/kilo-auto/frontier") {
		t.Error("kilo/kilo-auto/frontier missing from the kilo catalog")
	}
}

// TestLiveSessionScopedCatalog pins MADR 0096 D1 end to end: the catalog a
// session reports for itself is its own vendor's, and it contains the model
// the user could not previously select.
func TestLiveSessionScopedCatalog(t *testing.T) {
	p := kilo.NewHTTP(kilo.Config{Model: "kilo/kilo-auto/frontier"})
	if !p.Ready() {
		t.Skip("kilo not in PATH")
	}
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{
		Name: "kilo-catalog", CWD: t.TempDir(), Model: "kilo/kilo-auto/frontier",
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	mc, ok := s.(provider.ModelCatalogSession)
	if !ok {
		t.Fatal("kilo session does not implement provider.ModelCatalogSession")
	}
	cat, err := mc.ModelCatalog(ctx, provider.CatalogScopeModels)
	if err != nil {
		t.Fatalf("ModelCatalog: %v", err)
	}
	if len(cat.Options) == 0 {
		t.Skip("engine reports no kilo models (logged out?)")
	}
	for _, o := range cat.Options {
		if o.Group != "kilo" {
			t.Fatalf("session catalog leaked model provider %q (option %s)", o.Group, o.ID)
		}
	}
	if !hasID(cat, "kilo/kilo-auto/frontier") {
		t.Error("session-scoped catalog omits the session's own model")
	}
	if len(cat.DefaultIDs) == 0 || cat.DefaultIDs[0] != "kilo/kilo-auto/frontier" {
		t.Errorf("default_ids = %v, want the session's own model preselected", cat.DefaultIDs)
	}
	t.Logf("session catalog: %d options, default=%v", len(cat.Options), cat.DefaultIDs)
}

func hasID(cat picker.Catalog, id string) bool {
	for _, o := range cat.Options {
		if o.ID == id {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

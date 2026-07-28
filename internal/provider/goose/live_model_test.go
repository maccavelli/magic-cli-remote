//go:build live_goose

package goose_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acphttp"
	"github.com/maccavelli/magic-cli-remote/internal/provider/goose"
)

// TestLiveGooseModelCatalog is the acceptance test for MADR 0043 D6 against a
// real engine. Before it, goose's model picker was empty — its spec declared no
// static models and nothing read the catalog goose publishes as ACP session
// config options.
//
// Run: go test -tags live_goose ./internal/provider/goose/ -run TestLiveGooseModelCatalog -count=1 -v
func TestLiveGooseModelCatalog(t *testing.T) {
	if _, err := exec.LookPath("goose"); err != nil {
		t.Skip("goose not in PATH")
	}
	p := goose.New(goose.Config{Config: acphttp.Config{AlwaysApprove: true}})
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	mpc, ok := any(p).(provider.ModelProviderCatalog)
	if !ok {
		t.Fatal("goose provider does not implement ModelProviderCatalog")
	}

	// Pre-session: nothing has populated the cache, so this opens a short-lived
	// probe session. That is the whole point — there is no goose HTTP endpoint
	// for models, and the catalog only exists on a session.
	start := time.Now()
	models, err := p.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	t.Logf("pre-session catalog: %d models, source=%s, defaults=%v, took %s",
		len(models.Options), models.Source, models.DefaultIDs,
		time.Since(start).Round(time.Millisecond))
	if models.Source == picker.SourceStatic {
		t.Skip("no goose provider configured on this host; nothing to assert")
	}
	if len(models.Options) == 0 {
		t.Fatal("pre-session catalog is empty — the whole point of D6")
	}
	if len(models.DefaultIDs) == 0 {
		t.Error("no current model reported; the picker cannot pre-select")
	}

	providers, err := mpc.ListModelProviders(ctx)
	if err != nil {
		t.Fatalf("ListModelProviders: %v", err)
	}
	t.Logf("model providers: %d", len(providers.Options))
	if len(providers.Options) < 2 {
		t.Fatalf("providers = %d, want goose's full list", len(providers.Options))
	}
	connected := ""
	for _, o := range providers.Options {
		if o.Meta[picker.MetaConnected] == "true" {
			if connected != "" {
				t.Errorf("more than one provider marked connected: %q and %q", connected, o.ID)
			}
			connected = o.ID
		}
	}
	if connected == "" {
		t.Fatal("no provider marked connected; the current one must be")
	}
	t.Logf("connected provider: %s", connected)

	// Every model id is qualified with that provider, so a create-session
	// choice cannot be applied against the wrong one.
	for _, o := range models.Options {
		if !strings.HasPrefix(o.ID, connected+"/") {
			t.Errorf("model %q is not scoped to the current provider %q", o.ID, connected)
		}
	}

	// The second request is served from the provider cache — a probe session
	// per picker open would be unacceptable.
	start = time.Now()
	if _, err := p.ListModels(ctx); err != nil {
		t.Fatalf("second ListModels: %v", err)
	}
	second := time.Since(start)
	t.Logf("second open: %s (cached)", second.Round(time.Microsecond))
	if second > 250*time.Millisecond {
		t.Errorf("second open took %s: the catalog cache is not engaging", second)
	}
}

// TestLiveGooseSessionModelCatalog covers the in-session /model path: a live
// session must report its own provider's models with the current one selected.
func TestLiveGooseSessionModelCatalog(t *testing.T) {
	if _, err := exec.LookPath("goose"); err != nil {
		t.Skip("goose not in PATH")
	}
	p := goose.New(goose.Config{Config: acphttp.Config{AlwaysApprove: true}})
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	s, err := p.Start(ctx, provider.StartOptions{CWD: t.TempDir(), Name: "live-model-catalog"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	mcs, ok := s.(provider.ModelCatalogSession)
	if !ok {
		t.Fatal("goose session does not implement ModelCatalogSession")
	}
	cat, err := mcs.ModelCatalog(ctx, provider.CatalogScopeModels)
	if err != nil {
		t.Fatalf("session ModelCatalog: %v", err)
	}
	t.Logf("session catalog: %d models, default=%v", len(cat.Options), cat.DefaultIDs)
	if len(cat.Options) == 0 {
		t.Fatal("session catalog is empty")
	}
	if len(cat.DefaultIDs) != 1 {
		t.Fatalf("default = %v, want exactly the session's current model", cat.DefaultIDs)
	}
	// The default must be one of the options, or the picker pre-selects nothing.
	found := false
	for _, o := range cat.Options {
		if o.ID == cat.DefaultIDs[0] {
			found = true
		}
	}
	if !found {
		t.Errorf("default %q is not in the catalog", cat.DefaultIDs[0])
	}

	provCat, err := mcs.ModelCatalog(ctx, provider.CatalogScopeProviders)
	if err != nil {
		t.Fatalf("session provider catalog: %v", err)
	}
	if len(provCat.Options) < 2 {
		t.Errorf("session provider catalog = %d options", len(provCat.Options))
	}

	// In-place switch: the same model must round-trip through SetModel without
	// relaunching anything.
	ms, ok := s.(provider.ModelSession)
	if !ok {
		t.Fatal("goose session does not implement ModelSession — /model would relaunch")
	}
	if err := ms.SetModel(ctx, cat.DefaultIDs[0]); err != nil {
		t.Fatalf("SetModel(%q): %v", cat.DefaultIDs[0], err)
	}

	// And the same qualified id applied at create time: this is the value the
	// create-session picker stores, so it must survive session/new rather than
	// being sent verbatim as a model id the engine will not recognise.
	qualified := cat.DefaultIDs[0]
	s2, err := p.Start(ctx, provider.StartOptions{
		CWD:   t.TempDir(),
		Name:  "live-model-create",
		Model: qualified,
	})
	if err != nil {
		t.Fatalf("start with model %q: %v", qualified, err)
	}
	defer s2.Close(context.Background())
	cat2, err := s2.(provider.ModelCatalogSession).ModelCatalog(ctx, provider.CatalogScopeModels)
	if err != nil {
		t.Fatalf("second session catalog: %v", err)
	}
	if len(cat2.DefaultIDs) != 1 || cat2.DefaultIDs[0] != qualified {
		t.Errorf("session created with %q reports %v", qualified, cat2.DefaultIDs)
	}
}

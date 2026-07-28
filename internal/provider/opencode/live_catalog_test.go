//go:build live_opencode

package opencode_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
)

// TestLiveOpenCodeCatalogIsScoped is the acceptance test for MADR 0043 D2/D8
// against a real engine. It asserts the three properties the unscoped catalog
// violated: the default set is the user's configured providers, the reply fits
// comfortably inside the relay's 1 MiB frame, and the full provider list is
// still reachable for the search path.
//
// Run: make live-opencode, or
// go test -tags live_opencode ./internal/provider/opencode/ -run TestLiveOpenCodeCatalog -count=1 -v
func TestLiveOpenCodeCatalogIsScoped(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not in PATH")
	}
	p := opencode.NewHTTP(opencode.Config{})
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	mpc, ok := any(p).(provider.ModelProviderCatalog)
	if !ok {
		t.Fatal("opencode provider does not implement ModelProviderCatalog")
	}

	start := time.Now()
	def, err := p.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	firstFetch := time.Since(start)
	body, err := json.Marshal(protocol.ModelsResultFromCatalog("opencode", def))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("default catalog: %d options, %d bytes, %s (was 5,788 / 531,554 bytes / ~0.9s)",
		len(def.Options), len(body), firstFetch.Round(time.Millisecond))
	if def.Source == picker.SourceStatic {
		t.Skip("engine catalog unavailable (offline?); nothing to assert")
	}
	if len(def.Options) == 0 {
		t.Fatal("default catalog is empty")
	}
	const budget = 64 << 10
	if len(body) > budget {
		t.Errorf("default reply is %d bytes, over the %d-byte budget", len(body), budget)
	}

	// Every model in the default set must belong to a connected provider.
	providers, err := mpc.ListModelProviders(ctx)
	if err != nil {
		t.Fatalf("ListModelProviders: %v", err)
	}
	connected := map[string]bool{}
	for _, o := range providers.Options {
		if o.Meta[picker.MetaConnected] == "true" {
			connected[o.ID] = true
		}
	}
	t.Logf("model providers: %d total, %d connected", len(providers.Options), len(connected))
	if len(providers.Options) <= len(connected) {
		t.Error("the full provider list is not reachable; the search path has nothing to search")
	}
	for _, o := range def.Options {
		mp, _, found := strings.Cut(o.ID, "/")
		if !found {
			t.Errorf("model id %q is not provider-qualified", o.ID)
			continue
		}
		if !connected[mp] {
			t.Errorf("default catalog contains %q from unconfigured provider %q", o.ID, mp)
		}
	}

	// Ordering: newest first *within each provider group*. The default catalog
	// is a concatenation of per-provider lists and the picker renders it with a
	// header per provider, so ordering is a within-group property; a global
	// sort would interleave providers under their own headers.
	// Each group's first row is that provider's own default model, pinned there
	// regardless of age, so the monotonic check starts from the second.
	prev := map[string]string{}
	seenInGroup := map[string]int{}
	for _, o := range def.Options {
		seenInGroup[o.Group]++
		d := o.Meta[picker.MetaReleaseDate]
		if d == "" || o.Meta[picker.MetaStatus] == picker.StatusDeprecated || seenInGroup[o.Group] == 1 {
			continue
		}
		if last, seen := prev[o.Group]; seen && d > last {
			t.Errorf("provider %q is not newest-first: %q (%s) follows a %s model",
				o.Group, o.ID, d, last)
			break
		}
		prev[o.Group] = d
	}

	// The cache must make a second open free.
	start = time.Now()
	if _, err := p.ListModels(ctx); err != nil {
		t.Fatalf("second ListModels: %v", err)
	}
	second := time.Since(start)
	t.Logf("second open: %s (cached)", second.Round(time.Microsecond))
	if second > firstFetch/2 && firstFetch > 50*time.Millisecond {
		t.Errorf("second open took %s vs %s: the catalog cache is not engaging", second, firstFetch)
	}

	// One named provider's models.
	for _, o := range providers.Options {
		if o.Meta[picker.MetaConnected] != "true" {
			continue
		}
		models, err := mpc.ListModelsFor(ctx, o.ID)
		if err != nil {
			t.Fatalf("ListModelsFor(%s): %v", o.ID, err)
		}
		t.Logf("provider %s (%s): %d models", o.ID, o.Label, len(models.Options))
		for _, m := range models.Options {
			if !strings.HasPrefix(m.ID, o.ID+"/") {
				t.Errorf("model %q is not scoped to provider %q", m.ID, o.ID)
			}
		}
		break
	}
}

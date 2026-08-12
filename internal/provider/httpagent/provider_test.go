package httpagent

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestWithConfiguredDefaultOverridesLiveDefault(t *testing.T) {
	cat := picker.SingleCatalog(picker.SourceLive, []picker.Option{{ID: "engine/default"}}, "engine/default", true)
	got := withConfiguredDefault(cat, "opencode-go/deepseek-v4-flash")
	if len(got.DefaultIDs) != 1 || got.DefaultIDs[0] != "opencode-go/deepseek-v4-flash" {
		t.Fatalf("defaults=%v", got.DefaultIDs)
	}
	if unchanged := withConfiguredDefault(cat, ""); unchanged.DefaultIDs[0] != "engine/default" {
		t.Fatalf("empty config changed defaults=%v", unchanged.DefaultIDs)
	}
}

// An engine that dies instantly (bad binary, immediate crash) must fail startup
// at once, not spin the full serverStartTimeout probing a corpse on
// connection-refused. `false` exits non-zero immediately, so the health poll
// never connects; the fix watches cmd.Wait and bails as soon as it exits.
func TestStartServerBailsWhenEngineExitsImmediately(t *testing.T) {
	if _, err := exec.LookPath("false"); err != nil {
		t.Skip("no 'false' binary on PATH")
	}
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)

	start := time.Now()
	_, err := p.startServer(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when engine exits immediately")
	}
	// Must return promptly — well under serverStartTimeout, which the buggy code
	// would spin in full before failing.
	if elapsed > 5*time.Second {
		t.Fatalf("startServer took %s; expected prompt failure well under serverStartTimeout (%s)",
			elapsed, serverStartTimeout)
	}
	if !strings.Contains(err.Error(), "exited during startup") {
		t.Fatalf("error=%q, want it to mention 'exited during startup'", err)
	}
}

// MADR 0074 D16: the vendor catalog is a multi-megabyte read on the real
// engines (4.7 MB from opencode 1.18.16), and the phone pages through it and
// searches it, so it is cached. A credential write must drop that cache,
// because the catalog carries the per-vendor status the user just changed.
func TestAuthCatalogCacheHitsThenInvalidates(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)

	if _, ok := p.cachedCatalog(); ok {
		t.Fatal("a fresh provider reported a cached catalog")
	}
	p.storeCatalog(provider.AuthCatalog{
		Upstreams: []provider.UpstreamAuth{{ID: "togetherai"}},
		Source:    provider.AuthCatalogSourceEngine,
	})
	got, ok := p.cachedCatalog()
	if !ok || len(got.Upstreams) != 1 || got.Upstreams[0].ID != "togetherai" {
		t.Fatalf("cache miss or wrong contents: ok=%v got=%+v", ok, got)
	}

	p.InvalidateAuthCatalog()
	if _, ok := p.cachedCatalog(); ok {
		t.Fatal("catalog survived invalidation; a stale status would be shown after a write")
	}
}

// An expired entry is a miss, so a vendor list that changed under a long-lived
// daemon is picked up without a restart.
func TestAuthCatalogCacheExpires(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{Bin: "false"}, nil)
	p.storeCatalog(provider.AuthCatalog{Upstreams: []provider.UpstreamAuth{{ID: "x"}}})
	p.authCatalogMu.Lock()
	p.authCatalogExpiry = time.Now().Add(-time.Second)
	p.authCatalogMu.Unlock()
	if _, ok := p.cachedCatalog(); ok {
		t.Fatal("an expired catalog was served from cache")
	}
}

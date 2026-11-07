//go:build live_grok

package grok_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/picker/saved"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

func catalogIDs(c picker.Catalog) []string {
	out := make([]string, 0, len(c.Options))
	for _, o := range c.Options {
		out = append(out, o.ID)
	}
	return out
}

// TestLiveSavedCatalogStandsInForAMissingGrok is MADR 0235's live
// confirmation at the provider: the real grok's catalog is saved, a grok that
// cannot start serves it, and with nothing saved the SDK floor is served with
// grok-4.7 preselected. grok's home is isolated and no key is set, so grok
// answers initialize signed out and nothing is spent (0235 PLAN C6).
func TestLiveSavedCatalogStandsInForAMissingGrok(t *testing.T) {
	isolateGrokHome(t)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "catalogs")
	missing := filepath.Join(t.TempDir(), "no-grok")

	store := saved.Open(dir, nil)
	a, err := grok.New(grok.Config{SavedCatalogs: store}).ListModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if a.Source != picker.SourceLive || len(a.Options) == 0 {
		t.Fatalf("the real grok listed %q %v; want a non-empty live catalog", a.Source, catalogIDs(a))
	}
	t.Logf("grok's live catalog: %v, default %v", catalogIDs(a), a.DefaultIDs)

	store.Flush()
	fi, err := os.Stat(filepath.Join(dir, "grok.json"))
	if err != nil {
		t.Fatalf("no saved catalog after the live answer: %v", err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("saved catalog mode = %o, want 600", fi.Mode().Perm())
	}

	b, err := grok.New(grok.Config{Bin: missing, SavedCatalogs: saved.Open(dir, nil)}).ListModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if b.Source != picker.SourceSaved || !slices.Equal(catalogIDs(b), catalogIDs(a)) {
		t.Fatalf("a missing grok listed %q %v; want saved %v", b.Source, catalogIDs(b), catalogIDs(a))
	}

	if err := os.Remove(filepath.Join(dir, "grok.json")); err != nil {
		t.Fatal(err)
	}
	c, err := grok.New(grok.Config{Bin: missing, SavedCatalogs: saved.Open(dir, nil)}).ListModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	floor := []string{"grok-4.7", "grok-4.6", "grok-4.5", "grok-4.3", "grok-build-0.1"}
	if c.Source != picker.SourceStatic || !slices.Equal(catalogIDs(c), floor) ||
		!slices.Equal(c.DefaultIDs, []string{"grok-4.7"}) {
		t.Fatalf("with nothing saved: %q %v default %v; want static %v, default [grok-4.7]",
			c.Source, catalogIDs(c), c.DefaultIDs, floor)
	}
}

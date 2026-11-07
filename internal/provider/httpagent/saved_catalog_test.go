package httpagent

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/picker/saved"
)

// listingDialect is a dialect with a default model catalog and no model
// providers, so ListModels takes its merge path when the engine answers.
type listingDialect struct {
	*fakeDialect
	live   picker.Catalog
	err    error
	static picker.Catalog
}

func (d *listingDialect) StaticModels(Config) picker.Catalog { return d.static }

func (d *listingDialect) ListModelsLive(context.Context, API) (picker.Catalog, error) {
	return d.live, d.err
}

// listingProvider is a provider over d whose engine is already up, so the
// dialect's fetch is the only thing between ListModels and its answer.
func listingProvider(t *testing.T, d *listingDialect, store *saved.Store, model string) *Provider {
	t.Helper()
	p := NewWithLogger(d, Config{Bin: testBin(t), Model: model, SavedCatalogs: store}, nil)
	p.eng = &engine{url: "http://127.0.0.1:1", dead: make(chan struct{})}
	return p
}

func catalogIDs(c picker.Catalog) []string {
	out := make([]string, 0, len(c.Options))
	for _, o := range c.Options {
		out = append(out, o.ID)
	}
	return out
}

// TestALiveListIsSavedBeforeTheMerge: what is saved is the engine's own list,
// never the static rows a merge adds (MADR 0235 D1).
func TestALiveListIsSavedBeforeTheMerge(t *testing.T) {
	dir := t.TempDir()
	store := saved.Open(dir, nil)
	d := &listingDialect{fakeDialect: &fakeDialect{id: "vendor"},
		live:   picker.SingleCatalog(picker.SourceLive, []picker.Option{{ID: "live-one"}}, "live-one", true),
		static: picker.SingleCatalog(picker.SourceStatic, []picker.Option{{ID: "static-one"}}, "", true)}

	got, err := listingProvider(t, d, store, "").ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(catalogIDs(got), []string{"live-one", "static-one"}) {
		t.Fatalf("ListModels = %v, want the merge [live-one static-one]", catalogIDs(got))
	}
	store.Flush()
	kept, ok := saved.Open(dir, nil).Load("vendor")
	if !ok || !slices.Equal(catalogIDs(kept), []string{"live-one"}) {
		t.Fatalf("saved = %v, %v; want only the live [live-one]", catalogIDs(kept), ok)
	}
}

// TestAFailedListServesTheSavedCatalog: an engine that cannot answer is
// stood in for by its last answer, with the configured model still the
// default (MADR 0235 D1).
func TestAFailedListServesTheSavedCatalog(t *testing.T) {
	dir := t.TempDir()
	seed := saved.Open(dir, nil)
	seed.Save("vendor", "", picker.SingleCatalog(picker.SourceLive,
		[]picker.Option{{ID: "kept-one"}, {ID: "kept-two"}}, "kept-one", true))
	seed.Flush()
	d := &listingDialect{fakeDialect: &fakeDialect{id: "vendor"}, err: errors.New("engine is down"),
		static: picker.SingleCatalog(picker.SourceStatic, []picker.Option{{ID: "static-one"}}, "", true)}

	got, err := listingProvider(t, d, saved.Open(dir, nil), "kept-two").ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != picker.SourceSaved || !slices.Equal(catalogIDs(got), []string{"kept-one", "kept-two"}) {
		t.Fatalf("ListModels = %q %v; want saved [kept-one kept-two]", got.Source, catalogIDs(got))
	}
	if !slices.Equal(got.DefaultIDs, []string{"kept-two"}) {
		t.Errorf("default = %v, want the configured [kept-two]", got.DefaultIDs)
	}
}

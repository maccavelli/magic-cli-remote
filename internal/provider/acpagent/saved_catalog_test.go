package acpagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"

	acp "github.com/maccavelli/acp-go-sdk2"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/picker/saved"
)

// The saved layer (MADR 0235 D1): an agent's live catalog is kept on disk, and
// served where the static list was when the agent cannot answer.

// savedCatalog loads id's catalog from a fresh store on dir, as a restarted
// daemon would.
func savedCatalog(t *testing.T, dir, id string) (picker.Catalog, bool) {
	t.Helper()
	return saved.Open(dir, nil).Load(id)
}

// seedSaved leaves a live catalog of ids saved for provider "vendor" in dir.
func seedSaved(t *testing.T, dir string, opts ...picker.Option) {
	t.Helper()
	s := saved.Open(dir, nil)
	s.Save("vendor", "", picker.SingleCatalog(picker.SourceLive, opts, opts[0].ID, true))
	s.Flush()
	if _, ok := savedCatalog(t, dir, "vendor"); !ok {
		t.Fatal("seed: nothing saved")
	}
}

// missingAgent is a provider whose binary does not exist, so it cannot
// start an agent to ask.
func missingAgent(t *testing.T, store *saved.Store, model string) *Provider {
	t.Helper()
	return New(Spec{ID: "vendor", StaticModels: staticModel,
		ModelProviders: func(_ context.Context, models []picker.Option) (picker.Catalog, error) {
			return picker.SingleCatalog(picker.SourceLive, nil, "", false), nil
		}},
		Config{Bin: filepath.Join(t.TempDir(), "no-agent"), Model: model, SavedCatalogs: store})
}

func TestAnInitializeCatalogIsSaved(t *testing.T) {
	dir := t.TempDir()
	store := saved.Open(dir, nil)
	var resp acp.InitializeResponse
	if err := json.Unmarshal([]byte(`{"protocolVersion":1,"_meta":{}}`), &resp); err != nil {
		t.Fatal(err)
	}
	catalog := picker.SingleCatalog(picker.SourceLive,
		[]picker.Option{{ID: "vendor-a"}, {ID: "vendor-b"}}, "vendor-a", true)
	p := &Provider{log: slog.Default(), cfg: Config{SavedCatalogs: store}, spec: Spec{ID: "vendor",
		InitializeMeta: func(map[string]any) InitializeInfo {
			return InitializeInfo{Version: "9.9.9", Models: &catalog}
		}}}

	p.applyInitializeResponse(&session{log: slog.Default()}, resp)
	store.Flush()

	got, ok := savedCatalog(t, dir, "vendor")
	if !ok || !slices.Equal(ids(got), []string{"vendor-a", "vendor-b"}) {
		t.Fatalf("saved after initialize: %v, %v; want [vendor-a vendor-b]", ids(got), ok)
	}
}

func TestASessionModelOptionIsSaved(t *testing.T) {
	dir := t.TempDir()
	store := saved.Open(dir, nil)
	p := New(Spec{ID: "vendor", StaticModels: staticModel, ListModels: noHarvest, ModelsFromOption: byPrefix},
		Config{SavedCatalogs: store})
	s := loadingSession()
	s.provider = p
	report(t, s, modelOptionJSON("a/two", "a/one"))
	store.Flush()

	got, ok := savedCatalog(t, dir, "vendor")
	if !ok || !slices.Equal(ids(got), []string{"a/two", "a/one"}) || got.Options[0].Group != "a" {
		t.Fatalf("saved after a session's model option: %+v, %v; want a/two, a/one, grouped", got, ok)
	}
}

func TestAMissingAgentServesTheSavedCatalog(t *testing.T) {
	dir := t.TempDir()
	seedSaved(t, dir, picker.Option{ID: "vendor-a"}, picker.Option{ID: "vendor-b"})

	got, err := missingAgent(t, saved.Open(dir, nil), "").ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != picker.SourceSaved || !slices.Equal(ids(got), []string{"vendor-a", "vendor-b"}) {
		t.Fatalf("missing agent lists %q %v; want saved [vendor-a vendor-b]", got.Source, ids(got))
	}
	if !slices.Equal(got.DefaultIDs, []string{"vendor-a"}) {
		t.Errorf("default = %v, want the saved one, [vendor-a]", got.DefaultIDs)
	}

	configured, err := missingAgent(t, saved.Open(dir, nil), "vendor-b").ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(configured.DefaultIDs, []string{"vendor-b"}) {
		t.Errorf("with a configured model, default = %v, want [vendor-b]", configured.DefaultIDs)
	}
}

func TestNoSavedCatalogServesTheStaticOne(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "catalogs")
	store := saved.Open(dir, nil)
	got, err := missingAgent(t, store, "").ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	store.Flush()
	if got.Source != picker.SourceStatic || !slices.Equal(ids(got), []string{"static/one"}) {
		t.Fatalf("no saved catalog: %q %v; want static [static/one]", got.Source, ids(got))
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the static fallback wrote to the store: %v", err)
	}
}

func TestModelsForVendorUsesTheSavedCatalog(t *testing.T) {
	dir := t.TempDir()
	seedSaved(t, dir, picker.Option{ID: "g1/a", Group: "g1"}, picker.Option{ID: "g2/b", Group: "g2"},
		picker.Option{ID: "g1/c", Group: "g1"})

	got, err := missingAgent(t, saved.Open(dir, nil), "").ListModelsFor(context.Background(), "g1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != picker.SourceSaved || !slices.Equal(ids(got), []string{"g1/a", "g1/c"}) {
		t.Fatalf("models for g1: %q %v; want saved [g1/a g1/c]", got.Source, ids(got))
	}
}

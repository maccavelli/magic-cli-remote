package codex

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/picker/saved"
)

// The saved layer (MADR 0235 D1): codex's last live model list stands in for
// its empty fallback when codex cannot answer.

func seedCodexCatalog(t *testing.T, dir string, ids ...string) {
	t.Helper()
	opts := make([]picker.Option, 0, len(ids))
	for _, id := range ids {
		opts = append(opts, picker.Option{ID: id})
	}
	s := saved.Open(dir, nil)
	s.Save("codex", "", picker.SingleCatalog(picker.SourceLive, opts, ids[0], true))
	s.Flush()
}

// unavailable is a codex provider whose binary does not exist, so its engine
// cannot start.
func unavailable(t *testing.T, store *saved.Store, model string) *Provider {
	t.Helper()
	return NewWithLogger(Config{Bin: filepath.Join(t.TempDir(), "no-codex"), Model: model, SavedCatalogs: store},
		silentLogger())
}

func TestAnUnavailableEngineServesTheSavedCatalog(t *testing.T) {
	dir := t.TempDir()
	seedCodexCatalog(t, dir, "gpt-a", "gpt-b")

	got, err := unavailable(t, saved.Open(dir, nil), "gpt-b").ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != picker.SourceSaved || !slices.Equal(optionIDs(got), []string{"gpt-a", "gpt-b"}) {
		t.Fatalf("ListModels = %q %v; want saved [gpt-a gpt-b]", got.Source, optionIDs(got))
	}
	if !slices.Equal(got.DefaultIDs, []string{"gpt-b"}) {
		t.Errorf("default = %v, want the configured [gpt-b]", got.DefaultIDs)
	}

	empty, err := unavailable(t, saved.Open(t.TempDir(), nil), "").ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if empty.Source != picker.SourceStatic || len(empty.Options) != 0 || !empty.AllowCustom {
		t.Errorf("with nothing saved: %q %v custom=%v; want today's empty static list", empty.Source,
			optionIDs(empty), empty.AllowCustom)
	}
}

// TestALiveModelListIsSaved runs codex's answer through collectModels, as
// ListModels does, and then through the step ListModels takes with it.
func TestALiveModelListIsSaved(t *testing.T) {
	dir := t.TempDir()
	store := saved.Open(dir, nil)
	p := unavailable(t, store, "")

	cat, _, err := collectModels(context.Background(), codexLike(onePage), "", silentLogger())
	if err != nil {
		t.Fatal(err)
	}
	if got := p.withSavedCatalog(cat); got.Source != picker.SourceLive {
		t.Fatalf("a live answer came back as %q", got.Source)
	}
	store.Flush()
	kept, ok := saved.Open(dir, nil).Load("codex")
	if !ok || !slices.Equal(optionIDs(kept), []string{"gpt-5.6-sol", "gpt-5.6-terra"}) {
		t.Fatalf("saved = %v, %v; want [gpt-5.6-sol gpt-5.6-terra]", optionIDs(kept), ok)
	}

	failed, _, _ := collectModels(context.Background(), func(context.Context, string, any) (json.RawMessage, error) {
		return nil, errors.New("model/list failed")
	}, "", silentLogger())
	if got := p.withSavedCatalog(failed); got.Source != picker.SourceSaved {
		t.Errorf("a failed model/list came back as %q, want the saved list", got.Source)
	}
}

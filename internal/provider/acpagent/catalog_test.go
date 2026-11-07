package acpagent

import (
	"context"
	"log/slog"
	"slices"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
)

// TestLiveCatalogIsNotPaddedWithTheFloor pins ListModels' rule that a live
// catalog is authoritative: the static floor is the fallback for having no
// agent, never a supplement to having one (MADR 0043 D7).
//
// The floor shares one ID with the live catalog and adds a sentinel no agent
// lists, so padding shows whatever the agent's list is. A live test's option
// count could not see it once an agent's floor fell inside its own list (MADR
// 0194 D1).
func TestLiveCatalogIsNotPaddedWithTheFloor(t *testing.T) {
	p := &Provider{
		log: slog.Default(),
		spec: Spec{StaticModels: []picker.Option{
			{ID: "m-shared", Label: "Shared"},
			{ID: "m-floor-sentinel", Label: "Floor only"},
		}},
		catalogCache: picker.SingleCatalog(picker.SourceLive, []picker.Option{
			{ID: "m-live-1", Label: "Live one"},
			{ID: "m-shared", Label: "Shared"},
		}, "m-live-1", true),
		catalogHas: true,
	}

	cat, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if cat.Source != picker.SourceLive {
		t.Errorf("source = %q, want %q", cat.Source, picker.SourceLive)
	}
	ids := make([]string, 0, len(cat.Options))
	for _, o := range cat.Options {
		ids = append(ids, o.ID)
	}
	slices.Sort(ids)
	if want := []string{"m-live-1", "m-shared"}; !slices.Equal(ids, want) {
		t.Errorf("catalog IDs = %v, want %v: the live catalog was padded with the static floor", ids, want)
	}
}

// TestTheStaticDefaultModelIsPreselected: a Spec's StaticDefaultModel is the
// static catalog's default, so the phone preselects it, and a configured
// model still wins (MADR 0235 D4 and its correction).
func TestTheStaticDefaultModelIsPreselected(t *testing.T) {
	spec := Spec{StaticModels: []picker.Option{{ID: "vendor-a"}, {ID: "vendor-b"}}, StaticDefaultModel: "vendor-b"}
	if got := (&Provider{spec: spec}).staticModelCatalog().DefaultIDs; !slices.Equal(got, []string{"vendor-b"}) {
		t.Errorf("static default = %v, want [vendor-b]", got)
	}
	configured := &Provider{spec: spec, cfg: Config{Model: "vendor-a"}}
	if got := configured.staticModelCatalog().DefaultIDs; !slices.Equal(got, []string{"vendor-a"}) {
		t.Errorf("with a configured model, static default = %v, want [vendor-a]", got)
	}
}

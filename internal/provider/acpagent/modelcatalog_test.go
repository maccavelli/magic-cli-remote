package acpagent

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// modelOptionJSON is a `model` select option whose values are ids, current
// first.
func modelOptionJSON(ids ...string) string {
	var vals []string
	for _, id := range ids {
		vals = append(vals, `{"value":"`+id+`","name":"Name of `+id+`"}`)
	}
	return `[{"type":"select","id":"model","name":"Model","category":"model","currentValue":"` + ids[0] +
		`","options":[` + strings.Join(vals, ",") + `]}]`
}

// byPrefix groups an id "<group>/<model>" under its group, as a Spec's
// ModelsFromOption might.
func byPrefix(opts []picker.Option) []picker.Option {
	out := slices.Clone(opts)
	for i := range out {
		out[i].Group, _, _ = strings.Cut(out[i].ID, "/")
	}
	return out
}

// noHarvest keeps ListModels from spawning an agent.
func noHarvest(context.Context, Config) (picker.Catalog, error) { return picker.Catalog{}, nil }

var staticModel = []picker.Option{{ID: "static/one", Label: "Static"}}

// report has s record its agent's config options, as a session/new answer
// or a config_option_update does.
func report(t *testing.T, s *session, raw string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordConfigOptions(parseConfigOptions(t, raw))
}

func ids(c picker.Catalog) []string {
	var out []string
	for _, o := range c.Options {
		out = append(out, o.ID)
	}
	return out
}

// TestModelOptionBecomesTheCatalog pins 0214 PLAN P6c Deviation 1 ("Latest
// session's option"): with ModelsFromOption, the latest session's model option
// is the provider's catalog, through the hook, and a later report replaces it;
// before any report, and without the hook, the static list stays.
func TestModelOptionBecomesTheCatalog(t *testing.T) {
	p := New(Spec{ID: "vendor", StaticModels: staticModel, ListModels: noHarvest, ModelsFromOption: byPrefix}, Config{})
	if got, _ := p.ListModels(context.Background()); !slices.Equal(ids(got), []string{"static/one"}) {
		t.Fatalf("before any session: %v, want the static list", ids(got))
	}
	s := loadingSession()
	s.provider = p
	report(t, s, modelOptionJSON("a/two", "a/one", "b/three"))
	got, _ := p.ListModels(context.Background())
	if !slices.Equal(ids(got), []string{"a/two", "a/one", "b/three"}) || got.Options[2].Group != "b" ||
		!slices.Equal(got.DefaultIDs, []string{"a/two"}) {
		t.Errorf("after a report: %+v, want the option's values, grouped by the hook, its current value the default", got)
	}
	report(t, s, modelOptionJSON("c/four"))
	if got, _ := p.ListModels(context.Background()); !slices.Equal(ids(got), []string{"c/four"}) {
		t.Errorf("after a second report: %v, want it to replace the first", ids(got))
	}

	plain := New(Spec{ID: "vendor", StaticModels: staticModel, ListModels: noHarvest}, Config{})
	s2 := loadingSession()
	s2.provider = plain
	report(t, s2, modelOptionJSON("a/two"))
	if got, _ := plain.ListModels(context.Background()); !slices.Equal(ids(got), []string{"static/one"}) {
		t.Errorf("without the hook: %v, want the static list unchanged", ids(got))
	}
}

// TestModelOptionsKeepTheAgentsGroups: a grouped option's values carry their
// group id.
func TestModelOptionsKeepTheAgentsGroups(t *testing.T) {
	opts := parseConfigOptions(t, `[{"type":"select","id":"model","name":"Model","category":"model","currentValue":"x",
	  "options":[{"group":"g1","name":"Group 1","options":[{"value":"x","name":"X"}]},
	             {"group":"g2","name":"Group 2","options":[{"value":"y","name":"Y"}]}]}]`)
	got, current, ok := modelOptions(opts)
	if !ok || current != "x" || len(got) != 2 || got[0].Group != "g1" || got[1].Group != "g2" || got[1].Label != "Y" {
		t.Errorf("modelOptions = %+v, %q, %v", got, current, ok)
	}
}

// TestModelProvidersDrillDown: with ModelProviders, the vendor step is the
// hook's answer from the provider's models, one vendor's models are those of
// its group, and a session's own are its current model's vendor's.
func TestModelProvidersDrillDown(t *testing.T) {
	var seen []string
	p := New(Spec{ID: "vendor", ListModels: noHarvest, ModelsFromOption: byPrefix,
		ModelProviders: func(_ context.Context, models []picker.Option) (picker.Catalog, error) {
			for _, m := range models {
				seen = append(seen, m.ID)
			}
			return picker.SingleCatalog(picker.SourceLive, []picker.Option{{ID: "a"}, {ID: "b"}}, "a", false), nil
		}}, Config{})
	s := loadingSession()
	s.provider = p
	report(t, s, modelOptionJSON("b/three", "a/one", "a/two"))
	ctx := context.Background()

	vendors, err := p.ListModelProviders(ctx)
	if err != nil || !slices.Equal(ids(vendors), []string{"a", "b"}) || !slices.Equal(seen, []string{"b/three", "a/one", "a/two"}) {
		t.Errorf("vendors %v (%v); the hook saw %v", ids(vendors), err, seen)
	}
	if a, _ := p.ListModelsFor(ctx, "a"); !slices.Equal(ids(a), []string{"a/one", "a/two"}) || len(a.DefaultIDs) != 0 {
		t.Errorf("vendor a: %v default %v, want its two models and no default from another vendor", ids(a), a.DefaultIDs)
	}
	if b, _ := p.ListModelsFor(ctx, "b"); !slices.Equal(b.DefaultIDs, []string{"b/three"}) {
		t.Errorf("vendor b's default %v, want the current model", b.DefaultIDs)
	}
	if none, _ := p.ListModelsFor(ctx, "gone"); len(none.Options) != 0 {
		t.Errorf("an unknown vendor lists %v", ids(none))
	}

	own, err := s.ModelCatalog(ctx, provider.CatalogScopeModels)
	if err != nil || !slices.Equal(ids(own), []string{"b/three"}) || !slices.Equal(own.DefaultIDs, []string{"b/three"}) {
		t.Errorf("the session's models %v (%v), want its current vendor's, its model the default", ids(own), err)
	}
	if v, err := s.ModelCatalog(ctx, provider.CatalogScopeProviders); err != nil || !slices.Equal(ids(v), []string{"a", "b"}) {
		t.Errorf("the session's vendor step %v (%v)", ids(v), err)
	}
}

// TestNoModelProvidersKeepsOneVendor: a Spec without the hook answers as the
// daemon did before acpagent offered the vendor step.
func TestNoModelProvidersKeepsOneVendor(t *testing.T) {
	p := New(Spec{ID: "vendor", StaticModels: staticModel, ListModels: noHarvest}, Config{})
	ctx := context.Background()
	v, err := p.ListModelProviders(ctx)
	if err != nil || !slices.Equal(ids(v), []string{"vendor"}) || v.Options[0].Meta[picker.MetaConnected] != "true" ||
		!slices.Equal(v.DefaultIDs, []string{"vendor"}) {
		t.Errorf("vendor step %+v (%v), want the Spec's own ID, connected", v, err)
	}
	if own, _ := p.ListModelsFor(ctx, "vendor"); !slices.Equal(ids(own), []string{"static/one"}) {
		t.Errorf("the one vendor's models %v, want the provider's", ids(own))
	}
	if other, _ := p.ListModelsFor(ctx, "other"); len(other.Options) != 0 || !other.AllowCustom {
		t.Errorf("another vendor: %+v, want empty with free text", other)
	}
	s := loadingSession()
	s.provider = p
	if _, err := s.ModelCatalog(ctx, provider.CatalogScopeModels); !errors.Is(err, provider.ErrNotImplemented) {
		t.Errorf("the session's own catalog: %v, want none, so the daemon answers the provider's", err)
	}
}

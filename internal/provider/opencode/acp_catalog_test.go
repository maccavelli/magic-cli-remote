package opencode

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent/acptest"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode/control"
)

func catalogIDs(c picker.Catalog) []string {
	var out []string
	for _, o := range c.Options {
		out = append(out, o.ID)
	}
	return out
}

// TestACPModelsFromOptionGroupsByVendor: a value's vendor is its group, and
// the label drops the vendor the name repeats.
func TestACPModelsFromOptionGroupsByVendor(t *testing.T) {
	got := acpModelsFromOption([]picker.Option{
		{ID: "anthropic/claude-fable-5", Label: "anthropic/Claude Fable 5"},
		{ID: "openrouter/particle-ai/particle-1", Label: "openrouter/Particle 1"},
		{ID: "bare", Label: "Bare"},
	})
	if got[0].Group != "anthropic" || got[0].Label != "Claude Fable 5" || got[1].Group != "openrouter" ||
		got[1].Label != "Particle 1" || got[2].Group != "" || got[2].Label != "Bare" {
		t.Errorf("options %+v", got)
	}
}

// TestACPModelProvidersGroupsConnected pins D7 as amended at P6c: a vendor
// whose models the option lists is connected, with its name from the
// integration list; every other integration but an MCP server's is not
// configured; and /api/provider is never asked.
func TestACPModelProvidersGroupsConnected(t *testing.T) {
	a, f, _ := newFakeAuth(t)
	f.add("zeta", "Zeta", keyMethod)
	f.add("openrouter", "OpenRouter", keyMethod)
	f.add("alpha", "Alpha", keyMethod)
	f.add("mistral", "Mistral", keyMethod)
	f.add("mcp", "MCP", oauthDevice).info.Metadata = map[string]any{"source": "mcp"}
	models := acpModelsFromOption([]picker.Option{
		{ID: "openrouter/a"}, {ID: "openrouter/b"}, {ID: "mistral/c"}, {ID: "opencode/d"},
	})
	cat, err := a.modelProviders(context.Background(), models)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"openrouter", "mistral", "opencode", "alpha", "zeta"}; !slices.Equal(catalogIDs(cat), want) {
		t.Fatalf("vendors %v, want %v", catalogIDs(cat), want)
	}
	or, oc, al := cat.Options[0], cat.Options[2], cat.Options[3]
	if or.Label != "OpenRouter" || or.Group != "Connected" || or.Meta[picker.MetaConnected] != "true" ||
		or.Meta[picker.MetaModelCount] != "2" || or.Description != "2 models" {
		t.Errorf("openrouter = %+v", or)
	}
	if oc.Label != "opencode" || oc.Group != "Connected" || oc.Description != "1 model" {
		t.Errorf("a vendor with no integration = %+v, want connected under its id", oc)
	}
	if al.Group != "Not configured" || al.Meta[picker.MetaConnected] != "false" || al.Meta[picker.MetaModelCount] != "" {
		t.Errorf("alpha = %+v, want not configured with no count", al)
	}
	if !slices.Equal(cat.DefaultIDs, []string{"openrouter"}) {
		t.Errorf("default %v, want the first connected vendor", cat.DefaultIDs)
	}
	for _, r := range f.sent() {
		if strings.Contains(r, "/api/provider") || strings.Contains(r, "/api/model") {
			t.Errorf("sent %s", r)
		}
	}
}

// failingPlane answers nothing.
type failingPlane struct{}

func (failingPlane) Do(context.Context, control.Request) (control.Response, error) {
	return control.Response{}, errors.New("no engine")
}

// TestACPModelProvidersWithoutTheEngine: the connected vendors are still
// listed, by id.
func TestACPModelProvidersWithoutTheEngine(t *testing.T) {
	a, _, _ := newFakeAuth(t)
	a.plane = failingPlane{}
	cat, err := a.modelProviders(context.Background(), acpModelsFromOption([]picker.Option{{ID: "openrouter/a"}}))
	if err != nil || !slices.Equal(catalogIDs(cat), []string{"openrouter"}) || cat.Options[0].Label != "openrouter" {
		t.Errorf("vendors %+v (%v), want openrouter alone", cat, err)
	}
}

// TestACPProviderCatalog: the provider NewACP builds shows the static list
// before a session, then the session's model option, grouped, with the vendor
// step and the session's own vendor's models.
func TestACPProviderCatalog(t *testing.T) {
	f := &fakeEngine{t: t}
	f.add("openrouter", "OpenRouter", keyMethod)
	f.add("alpha", "Alpha", keyMethod)
	p := newACP(acpagent.Config{}, nil, f)
	ctx := context.Background()
	before, _ := p.ListModels(ctx)
	if len(before.Options) != len(acpStaticModels) || before.Options[0].ID != acpStaticModels[0].ID {
		t.Errorf("before a session: %v, want the static list", catalogIDs(before))
	}

	s, _ := acptest.Start(t, p, acptest.Options{Replies: map[string]string{
		"initialize": `{"protocolVersion":1,"agentInfo":{"name":"OpenCode","version":"2.2.3"},"agentCapabilities":{"loadSession":true}}`,
		"session/new": `{"sessionId":"agent-1","configOptions":[{"type":"select","id":"model","name":"Model","category":"model",
		  "currentValue":"openrouter/b","options":[{"value":"opencode/a","name":"opencode/A"},
		  {"value":"openrouter/b","name":"openrouter/B"},{"value":"openrouter/c","name":"openrouter/C"}]}]}`,
	}})
	after, _ := p.ListModels(ctx)
	if !slices.Equal(catalogIDs(after), []string{"opencode/a", "openrouter/b", "openrouter/c"}) || after.Options[1].Group != "openrouter" ||
		after.Options[1].Label != "B" {
		t.Errorf("after a session: %+v, want its model option, grouped", after.Options)
	}
	vendors, err := p.ListModelProviders(ctx)
	if err != nil || !slices.Equal(catalogIDs(vendors), []string{"opencode", "openrouter", "alpha"}) {
		t.Errorf("vendors %v (%v)", catalogIDs(vendors), err)
	}
	mc, ok := s.(provider.ModelCatalogSession)
	if !ok {
		t.Fatal("the session reports no model catalog")
	}
	own, err := mc.ModelCatalog(ctx, provider.CatalogScopeModels)
	if err != nil || !slices.Equal(catalogIDs(own), []string{"openrouter/b", "openrouter/c"}) {
		t.Errorf("the session's own models %v (%v), want its vendor's", catalogIDs(own), err)
	}
}

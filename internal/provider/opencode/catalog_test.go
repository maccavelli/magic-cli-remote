package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// jsonAPI serves one canned body for every request.
func jsonAPI(body string) httpagent.API {
	return func(_ context.Context, _, _ string, _, out any) error {
		if out == nil {
			return nil
		}
		return json.Unmarshal([]byte(body), out)
	}
}

// catDefault is the single pre-selected id, or "".
func catDefault(c picker.Catalog) string {
	if len(c.DefaultIDs) == 0 {
		return ""
	}
	return c.DefaultIDs[0]
}

func optionIDs(c picker.Catalog) []string {
	out := make([]string, 0, len(c.Options))
	for _, o := range c.Options {
		out = append(out, o.ID)
	}
	return out
}

const providerCatalog = `{
	"default": {"opencode": "big-pickle"},
	"all": [
		{"id":"opencode","models":{"north-mini-code-free":{},"big-pickle":{}}},
		{"id":"anthropic","models":{"claude-sonnet-4-5":{}}}
	]
}`

// AfterBoot prefers the lowest-latency free model still in the catalog over the
// engine's own default (often a slower big model) — see zenFallbackModels.
func TestAfterBootPrefersFallbackOrderOverEngineDefault(t *testing.T) {
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	d.AfterBoot(context.Background(), jsonAPI(providerCatalog))

	mp, mid := d.fallbackModel()
	// deepseek-v4-flash-free is absent from this catalog, so the next entry wins.
	if mp != "opencode" || mid != "north-mini-code-free" {
		t.Fatalf("fallback=%s/%s want opencode/north-mini-code-free", mp, mid)
	}
}

// With no zenFallbackModels entry available, the engine's declared default wins.
func TestAfterBootFallsBackToEngineDefault(t *testing.T) {
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	d.AfterBoot(context.Background(), jsonAPI(`{
		"default": {"opencode": "some-other-model"},
		"all": [{"id":"opencode","models":{"some-other-model":{}}}]
	}`))
	if _, mid := d.fallbackModel(); mid != "some-other-model" {
		t.Fatalf("fallback model=%q", mid)
	}
}

// A failed catalog fetch must leave the seeded default intact: sessions still
// need a pinned model or the engine 400s on its own broken zen default.
func TestAfterBootKeepsSeedOnFetchError(t *testing.T) {
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	d.AfterBoot(context.Background(), func(context.Context, string, string, any, any) error {
		return fmt.Errorf("catalog down")
	})
	if mp, mid := d.fallbackModel(); mp != "opencode" || mid != zenDefaultModel {
		t.Fatalf("fallback=%s/%s want the seeded default", mp, mid)
	}
}

func TestListModelsLiveFlattensAndSorts(t *testing.T) {
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	cat, err := d.ListModelsLive(context.Background(), jsonAPI(providerCatalog))
	if err != nil {
		t.Fatal(err)
	}
	ids := optionIDs(cat)
	want := []string{"anthropic/claude-sonnet-4-5", "opencode/big-pickle", "opencode/north-mini-code-free"}
	if !slices.Equal(ids, want) {
		t.Fatalf("options=%v want %v (map order must be normalized)", ids, want)
	}
	if catDefault(cat) != "opencode/big-pickle" {
		t.Fatalf("default=%q want the engine's declared default", catDefault(cat))
	}
	if cat.Source != picker.SourceLive {
		t.Fatalf("source=%q", cat.Source)
	}
}

// No engine default → fall back to the dialect's resolved model so the picker
// always has something selected.
func TestListModelsLiveUsesDialectFallbackDefault(t *testing.T) {
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: "seeded"}
	cat, err := d.ListModelsLive(context.Background(), jsonAPI(`{
		"default": {}, "all": [{"id":"opencode","models":{"m1":{}}}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if catDefault(cat) != "opencode/seeded" {
		t.Fatalf("default=%q want opencode/seeded", catDefault(cat))
	}
}

func TestListAgentsLiveSortsPrimaryFirst(t *testing.T) {
	d := &httpDialect{log: slog.Default()}
	cat, err := d.ListAgentsLive(context.Background(), jsonAPI(`[
		{"name":"explore","mode":"subagent","description":"read only"},
		{"name":"build","mode":"primary","description":"default"},
		{"name":"","mode":"primary"},
		{"name":"plan","mode":"primary"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	ids := optionIDs(cat)
	want := []string{"build", "plan", "explore"}
	if !slices.Equal(ids, want) {
		t.Fatalf("agents=%v want %v (primary first, nameless dropped)", ids, want)
	}
	if catDefault(cat) != "build" {
		t.Fatalf("default=%q want build", catDefault(cat))
	}
	// A missing description falls back to the mode so the picker row is not blank.
	for _, o := range cat.Options {
		if o.Description == "" {
			t.Fatalf("agent %q has no description", o.ID)
		}
	}
}

// Without a "build" agent, the first primary-mode agent becomes the default.
func TestListAgentsLiveDefaultsToFirstPrimary(t *testing.T) {
	d := &httpDialect{log: slog.Default()}
	cat, err := d.ListAgentsLive(context.Background(), jsonAPI(`[
		{"name":"zeta","mode":"subagent"},
		{"name":"custom","mode":"primary"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if catDefault(cat) != "custom" {
		t.Fatalf("default=%q want custom", catDefault(cat))
	}
}

func TestListCommandsLiveSortsAndUsesHints(t *testing.T) {
	d := &httpDialect{log: slog.Default()}
	cat, err := d.ListCommandsLive(context.Background(), jsonAPI(`[
		{"name":"review","source":"builtin"},
		{"name":"deploy","source":"project","hints":["stage","prod"]},
		{"name":""}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	ids := optionIDs(cat)
	want := []string{"review", "deploy"} // builtin < project by group
	if !slices.Equal(ids, want) {
		t.Fatalf("commands=%v want %v", ids, want)
	}
	for _, o := range cat.Options {
		if o.ID == "deploy" && o.Description != "stage, prod" {
			t.Fatalf("hints not used as description: %q", o.Description)
		}
	}
}

// The offline catalogs are what the picker shows before (or without) an engine.
func TestStaticCatalogs(t *testing.T) {
	d := &httpDialect{log: slog.Default()}

	models := d.StaticModels(httpagent.Config{})
	if catDefault(models) != "opencode/"+zenDefaultModel {
		t.Fatalf("static model default=%q", catDefault(models))
	}
	if !slices.Contains(optionIDs(models), "opencode/"+zenDefaultModel) {
		t.Fatalf("static models missing the seeded default: %v", optionIDs(models))
	}
	// A configured model overrides the seed.
	if got := d.StaticModels(httpagent.Config{Model: "anthropic/claude-haiku-4-5"}); catDefault(got) != "anthropic/claude-haiku-4-5" {
		t.Fatalf("configured model ignored: %q", catDefault(got))
	}

	agents := d.StaticAgents(httpagent.Config{})
	if catDefault(agents) != "build" || !slices.Contains(optionIDs(agents), "plan") {
		t.Fatalf("static agents=%v default=%q", optionIDs(agents), catDefault(agents))
	}

	cmds := d.StaticCommands(httpagent.Config{})
	if !slices.Contains(optionIDs(cmds), "init") {
		t.Fatalf("static commands=%v", optionIDs(cmds))
	}
}

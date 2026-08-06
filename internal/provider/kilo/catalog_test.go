package kilo

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
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

// routedAPI serves a different body per path. The dialect deliberately
// prefers /config/providers (small, connected only) over /provider (multi-MB,
// everything), so tests must be able to tell which one it asked for.
func routedAPI(bodies map[string]string) httpagent.API {
	return func(_ context.Context, _, path string, _, out any) error {
		body, ok := bodies[path]
		if !ok {
			return fmt.Errorf("no canned body for %s", path)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal([]byte(body), out)
	}
}

// connectedCatalog mirrors GET /config/providers as the engine really returns
// it (MADR 0075 §2.4, §2.6) — including the plaintext `key` field, which must
// never survive the decode.
const connectedCatalog = `{
	"default": {"kilo": "kilo-auto/balanced", "openrouter": "openrouter/free"},
	"providers": [
		{"id":"kilo","name":"Kilo Gateway","key":"SENTINEL-NOT-A-REAL-KEY","models":{
			"kilo-auto/free":{"id":"kilo-auto/free","name":"Kilo Auto (free)","release_date":"2025-10-17","status":"active","limit":{"context":200000}},
			"kilo-auto/balanced":{"id":"kilo-auto/balanced","name":"Kilo Auto (balanced)","release_date":"2026-03-02","status":"active","limit":{"context":1000000}},
			"~anthropic/claude-sonnet-4-5":{"id":"~anthropic/claude-sonnet-4-5","name":"Claude Sonnet 4.5","release_date":"2025-09-29","status":"active","limit":{"context":200000}},
			"retired-one":{"id":"retired-one","name":"Retired","release_date":"2026-06-01","status":"deprecated"}
		}},
		{"id":"openrouter","name":"OpenRouter","key":"SENTINEL-NOT-A-REAL-KEY","models":{
			"openrouter/free":{"id":"openrouter/free","name":"OpenRouter free","release_date":"2025-08-01","status":"active","limit":{"context":32000}}
		}}
	]
}`

// providerCatalog mirrors GET /provider — every provider the engine knows,
// `connected` naming the ones the user actually has credentials for.
const providerCatalog = `{
	"default": {"kilo": "kilo-auto/balanced"},
	"connected": ["kilo", "openrouter"],
	"all": [
		{"id":"kilo","name":"Kilo Gateway","models":{"kilo-auto/free":{},"kilo-auto/balanced":{}}},
		{"id":"openrouter","name":"OpenRouter","models":{"openrouter/free":{}}},
		{"id":"302ai","name":"302.AI","models":{"some-model":{},"other-model":{}}}
	]
}`

// catalogAPI is the pair of endpoints the dialect actually uses.
func catalogAPI() httpagent.API {
	return routedAPI(map[string]string{
		"/config/providers": connectedCatalog,
		"/provider":         providerCatalog,
	})
}

func newDialect() *httpDialect {
	return &httpDialect{log: slog.Default(), defaultModelProvider: "kilo", defaultModelID: defaultModelID}
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

// TestConnectedCatalogDropsAPIKey is the security guard for MADR 0043 D4,
// ported for Kilo's own connectedProvidersResponse (MADR 0076 M2 — the
// comment at catalog_live.go:124 promised this test existed here; it did
// not, until now). GET /config/providers returns the user's API key in
// plaintext; the decode struct has no such field so encoding/json discards
// it. Nothing derived from that response may carry it onto the wire.
func TestConnectedCatalogDropsAPIKey(t *testing.T) {
	d := newDialect()

	// The decode itself must not retain the key. Re-marshalling what we
	// decoded is what catches someone "completing" connectedProvidersResponse
	// against the engine's response shape — the catalog assertions below
	// would not, because a struct field only leaks once something copies it
	// forward.
	decoded, err := d.connectedProviders(context.Background(), catalogAPI())
	if err != nil {
		t.Fatal(err)
	}
	if b, err := json.Marshal(decoded); err != nil {
		t.Fatal(err)
	} else if strings.Contains(string(b), "SENTINEL") {
		t.Fatalf("connectedProvidersResponse retained the API key: %s", b)
	}

	for name, fetch := range map[string]func() (picker.Catalog, error){
		"models": func() (picker.Catalog, error) {
			return d.ListModelsLive(context.Background(), catalogAPI())
		},
		"providers": func() (picker.Catalog, error) {
			return d.ListModelProvidersLive(context.Background(), catalogAPI())
		},
		"models-for": func() (picker.Catalog, error) {
			return d.ListModelsForLive(context.Background(), catalogAPI(), "kilo")
		},
	} {
		cat, err := fetch()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, err := json.Marshal(cat)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "SENTINEL") {
			t.Fatalf("%s catalog carries the provider API key: %s", name, b)
		}
	}
}

// AfterBoot's fallback-preference logic is opencode-specific (zenFallbackModels
// — a curated list of low-latency free models preferred over the engine's own
// declared default). Kilo's AfterBoot has no equivalent concept (dialect.go)
// — it either takes the Gateway's own default or the first connected
// provider's default, never a static preference list — so
// TestAfterBootPrefersFallbackOrderOverEngineDefault has no kilo counterpart
// (MADR 0076 plan P6, D3 N/A note).

// TestAfterBootUsesKiloGatewayDefault: when the engine reports a Gateway
// default (conn.Default["kilo"]), AfterBoot adopts it verbatim — this is the
// auth-state-dependent default MADR 0075 §2.6 / PD4 says must come from the
// engine, never be hard-coded.
func TestAfterBootUsesKiloGatewayDefault(t *testing.T) {
	d := newDialect()
	d.AfterBoot(context.Background(), jsonAPI(connectedCatalog))
	mp, mid := d.fallbackModel()
	if mp != "kilo" || mid != "kilo-auto/balanced" {
		t.Fatalf("fallback=%s/%s want kilo/kilo-auto/balanced", mp, mid)
	}
}

// TestAfterBootFallsBackToFirstConnectedProviderDefault: no Gateway default
// (e.g. logged out) but another connected provider has one — AfterBoot must
// still land on a usable model rather than leaving the seed (dialect.go
// AfterBoot's "no Gateway default" branch).
func TestAfterBootFallsBackToFirstConnectedProviderDefault(t *testing.T) {
	d := newDialect()
	d.AfterBoot(context.Background(), jsonAPI(`{
		"default": {"openrouter": "openrouter/free"},
		"providers": [{"id":"openrouter","name":"OpenRouter","models":{"openrouter/free":{}}}]
	}`))
	mp, mid := d.fallbackModel()
	if mp != "openrouter" || mid != "openrouter/free" {
		t.Fatalf("fallback=%s/%s want openrouter/openrouter/free", mp, mid)
	}
}

// A failed catalog fetch must leave the seeded default intact: sessions still
// need a pinned model or the engine 400s on an unresolved prompt.
func TestAfterBootKeepsSeedOnFetchError(t *testing.T) {
	d := newDialect()
	d.AfterBoot(context.Background(), func(context.Context, string, string, any, any) error {
		return fmt.Errorf("catalog down")
	})
	if mp, mid := d.fallbackModel(); mp != "kilo" || mid != defaultModelID {
		t.Fatalf("fallback=%s/%s want the seeded default", mp, mid)
	}
}

// TestListModelsLiveIsConnectedOnly is the core of MADR 0043 D2 / 0075 PD5.
// The default catalog must be the user's configured providers and nothing
// else — kilo's real /provider is 179-181 providers and thousands of models
// (MADR 0076 M4 #3), so this guard matters even more here than on opencode.
func TestListModelsLiveIsConnectedOnly(t *testing.T) {
	cat, err := newDialect().ListModelsLive(context.Background(), catalogAPI())
	if err != nil {
		t.Fatal(err)
	}
	ids := optionIDs(cat)
	for _, id := range ids {
		if strings.HasPrefix(id, "302ai/") {
			t.Fatalf("unconfigured provider leaked into the default catalog: %v", ids)
		}
	}
	if !slices.Contains(ids, "kilo/kilo-auto/balanced") || !slices.Contains(ids, "openrouter/openrouter/free") {
		t.Fatalf("connected models missing: %v", ids)
	}
	if catDefault(cat) != "kilo/kilo-auto/balanced" {
		t.Fatalf("default=%q want the engine's declared default", catDefault(cat))
	}
	if cat.Source != picker.SourceLive {
		t.Fatalf("source=%q", cat.Source)
	}
}

// TestListModelsLiveOrdersNewestFirst: Kilo dates its models like OpenCode
// does, so the picker must lead with the default, then newest first, and
// sink the deprecated to the end regardless of date (PD3/PD4).
func TestListModelsLiveOrdersNewestFirst(t *testing.T) {
	cat, err := newDialect().ListModelsForLive(context.Background(), catalogAPI(), "kilo")
	if err != nil {
		t.Fatal(err)
	}
	ids := optionIDs(cat)
	want := []string{
		"kilo/kilo-auto/balanced", // engine default leads
		"kilo/kilo-auto/free",     // then by release date descending
		"kilo/~anthropic/claude-sonnet-4-5",
		"kilo/retired-one", // deprecated sinks last whatever its date
	}
	if !slices.Equal(ids, want) {
		t.Fatalf("options=%v want %v", ids, want)
	}
}

// TestListModelProvidersLive: connected first, engine display names, counts,
// and the long tail still reachable.
func TestListModelProvidersLive(t *testing.T) {
	cat, err := newDialect().ListModelProvidersLive(context.Background(), catalogAPI())
	if err != nil {
		t.Fatal(err)
	}
	ids := optionIDs(cat)
	if len(ids) < 3 {
		t.Fatalf("providers=%v want connected plus the tail", ids)
	}
	if ids[0] != "kilo" && ids[1] != "kilo" {
		t.Fatalf("connected providers are not first: %v", ids)
	}
	byID := map[string]picker.Option{}
	for _, o := range cat.Options {
		byID[o.ID] = o
	}
	if got := byID["kilo"].Label; got != "Kilo Gateway" {
		t.Errorf("kilo label=%q want the engine's display name", got)
	}
	if got := byID["kilo"].Meta[picker.MetaConnected]; got != "true" {
		t.Errorf("kilo connected=%q", got)
	}
	if got := byID["302ai"].Meta[picker.MetaConnected]; got != "false" {
		t.Errorf("302ai connected=%q want false", got)
	}
	if got := byID["302ai"].Group; got != "All providers" {
		t.Errorf("302ai group=%q", got)
	}
	if got := byID["kilo"].Meta[picker.MetaDefaultModel]; got != "kilo/kilo-auto/balanced" {
		t.Errorf("kilo default model=%q", got)
	}
	if cat.AllowCustom {
		t.Error("a free-typed model provider id is meaningless; allow_custom must be off")
	}
}

// TestListModelsForLiveUnknownProviderIsEmpty: the client may ask about a
// provider that has since left the engine's list; that is not an error.
func TestListModelsForLiveUnknownProviderIsEmpty(t *testing.T) {
	cat, err := newDialect().ListModelsForLive(context.Background(), catalogAPI(), "nope")
	if err != nil {
		t.Fatalf("unknown provider must not error: %v", err)
	}
	if len(cat.Options) != 0 {
		t.Fatalf("options=%v want empty", optionIDs(cat))
	}
}

// TestListModelsForLiveFallsThroughToFullCatalog: a provider the user has not
// configured is not in the cheap endpoint, so the expensive one must answer.
func TestListModelsForLiveFallsThroughToFullCatalog(t *testing.T) {
	cat, err := newDialect().ListModelsForLive(context.Background(), catalogAPI(), "302ai")
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Options) != 2 {
		t.Fatalf("options=%v want the two 302ai models", optionIDs(cat))
	}
}

// TestListModelsLiveSkipsTheExpensiveEndpoint: the whole point of the cheap
// endpoint is that the common path never downloads the 4.7 MB /provider dump.
func TestListModelsLiveSkipsTheExpensiveEndpoint(t *testing.T) {
	var paths []string
	api := func(_ context.Context, _, path string, _, out any) error {
		paths = append(paths, path)
		if path != "/config/providers" {
			return fmt.Errorf("unexpected fetch of %s", path)
		}
		return json.Unmarshal([]byte(connectedCatalog), out)
	}
	if _, err := newDialect().ListModelsLive(context.Background(), api); err != nil {
		t.Fatalf("default catalog fetched %v: %v", paths, err)
	}
	if len(paths) != 1 || paths[0] != "/config/providers" {
		t.Fatalf("fetched %v, want only /config/providers", paths)
	}
}

// No engine default → fall back to the dialect's resolved model so the picker
// always has something selected.
func TestListModelsLiveUsesDialectFallbackDefault(t *testing.T) {
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "kilo", defaultModelID: "seeded"}
	cat, err := d.ListModelsLive(context.Background(), routedAPI(map[string]string{
		"/config/providers": `{"default": {}, "providers": [{"id":"kilo","models":{"m1":{}}}]}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if catDefault(cat) != "kilo/seeded" {
		t.Fatalf("default=%q want kilo/seeded", catDefault(cat))
	}
}

func TestListAgentsLiveSortsPrimaryFirst(t *testing.T) {
	d := &httpDialect{log: slog.Default()}
	cat, err := d.ListAgentsLive(context.Background(), jsonAPI(`[
		{"name":"explore","mode":"subagent","description":"read only"},
		{"name":"code","mode":"primary","description":"default"},
		{"name":"","mode":"primary"},
		{"name":"plan","mode":"primary"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	ids := optionIDs(cat)
	want := []string{"code", "plan"}
	if !slices.Equal(ids, want) {
		t.Fatalf("agents=%v want %v (primary first, nameless dropped)", ids, want)
	}
	if catDefault(cat) != "code" {
		t.Fatalf("default=%q want code", catDefault(cat))
	}
	// A missing description falls back to the mode so the picker row is not blank.
	for _, o := range cat.Options {
		if o.Description == "" {
			t.Fatalf("agent %q has no description", o.ID)
		}
	}
}

// Without a "code" agent, the first primary-mode agent becomes the default.
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

// TestStaticCommandsCatalog covers the one static catalog not already tested
// by dialect_test.go (TestStaticModelsDefaultsToKiloAutoFree,
// TestStaticModelsHonorsConfigModel, TestStaticAgentsDefaultCode already
// cover StaticModels/StaticAgents — porting opencode's combined
// TestStaticCatalogs here would just re-assert them, MADR 0076 plan P6 D3).
func TestStaticCommandsCatalog(t *testing.T) {
	d := &httpDialect{log: slog.Default()}
	cmds := d.StaticCommands(httpagent.Config{})
	if !slices.Contains(optionIDs(cmds), "init") {
		t.Fatalf("static commands=%v", optionIDs(cmds))
	}
}

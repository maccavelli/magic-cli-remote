package opencode

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

// routedAPI serves a different body per path. The catalog code deliberately
// prefers /config/providers (99 KB, connected only) over /provider (4.3 MB,
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
// it (probed against opencode 1.18.7, MADR 0043 §2.1) — including the
// plaintext `key` field, which must never survive the decode.
const connectedCatalog = `{
	"default": {"opencode": "big-pickle", "anthropic": "claude-sonnet-4-5"},
	"providers": [
		{"id":"opencode","name":"OpenCode Zen","source":"api","key":"SENTINEL-NOT-A-REAL-KEY","models":{
			"big-pickle":{"id":"big-pickle","name":"Big Pickle","release_date":"2025-10-17","status":"active","limit":{"context":200000}},
			"north-mini-code-free":{"id":"north-mini-code-free","name":"North Mini Code","release_date":"2026-03-02","status":"active","limit":{"context":1000000}},
			"retired-one":{"id":"retired-one","name":"Retired","release_date":"2026-06-01","status":"deprecated"}
		}},
		{"id":"anthropic","name":"Anthropic","source":"custom","key":"SENTINEL-NOT-A-REAL-KEY","models":{
			"claude-sonnet-4-5":{"id":"claude-sonnet-4-5","name":"Claude Sonnet 4.5","release_date":"2025-09-29","status":"active","limit":{"context":200000}}
		}}
	]
}`

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
	"connected": ["opencode", "anthropic"],
	"all": [
		{"id":"opencode","name":"OpenCode Zen","models":{"north-mini-code-free":{},"big-pickle":{}}},
		{"id":"anthropic","name":"Anthropic","models":{"claude-sonnet-4-5":{}}},
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

// AfterBoot takes the engine's own declared default, even when the catalog also
// carries free models mcremote used to rank ahead of it.
//
// This reverses the previous policy deliberately. The old order preferred a
// hand-maintained latency ranking (deepseek-v4-flash-free, then
// north-mini-code-free, then big-pickle); by 1.18.21 the first two had been
// removed from the OpenCode Zen catalog entirely, so the ranking was steering
// toward models that no longer existed. Whatever the engine reports is current
// by construction (MADR 0112 D6).
func TestAfterBootPrefersEngineDefault(t *testing.T) {
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	d.AfterBoot(context.Background(), jsonAPI(providerCatalog))

	mp, mid := d.fallbackModel()
	// This catalog declares big-pickle as the engine default and also carries
	// north-mini-code-free, which the old ranking would have taken instead.
	if mp != "opencode" || mid != "big-pickle" {
		t.Fatalf("fallback=%s/%s want the engine default opencode/big-pickle", mp, mid)
	}
}

// When the engine declares a default the catalog does list, it wins outright.
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

func newDialect() *httpDialect {
	return &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
}

// TestListModelsLiveIsConnectedOnly is the core of MADR 0043 D2. The default
// catalog used to be every model of every provider models.dev knows — 5,788
// options in a 532 KB frame, opening on a provider called `302ai`. It must now
// be the user's configured providers and nothing else.
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
	if !slices.Contains(ids, "opencode/big-pickle") || !slices.Contains(ids, "anthropic/claude-sonnet-4-5") {
		t.Fatalf("connected models missing: %v", ids)
	}
	if catDefault(cat) != "opencode/big-pickle" {
		t.Fatalf("default=%q want the engine's declared default", catDefault(cat))
	}
	if cat.Source != picker.SourceLive {
		t.Fatalf("source=%q", cat.Source)
	}
}

// TestListModelsLiveOrdersNewestFirst: OpenCode dates 100% of its models, so
// the picker must lead with the newest and sink the deprecated.
func TestListModelsLiveOrdersNewestFirst(t *testing.T) {
	cat, err := newDialect().ListModelsForLive(context.Background(), catalogAPI(), "opencode")
	if err != nil {
		t.Fatal(err)
	}
	ids := optionIDs(cat)
	// big-pickle is the engine default so it leads; then by release date
	// descending; the deprecated one is last whatever its date.
	want := []string{"opencode/big-pickle", "opencode/north-mini-code-free", "opencode/retired-one"}
	if !slices.Equal(ids, want) {
		t.Fatalf("options=%v want %v", ids, want)
	}
}

// TestConnectedCatalogDropsAPIKey is the security guard for MADR 0043 D4.
// GET /config/providers returns the user's API key in plaintext; the decode
// struct has no such field so encoding/json discards it. Nothing derived from
// that response may carry it onto the wire.
func TestConnectedCatalogDropsAPIKey(t *testing.T) {
	d := newDialect()

	// The decode itself must not retain the key. Re-marshalling what we decoded
	// is what catches someone "completing" connectedProvidersResponse against
	// the engine's response shape — the catalog assertions below would not,
	// because a struct field only leaks once something copies it forward.
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
			return d.ListModelsForLive(context.Background(), catalogAPI(), "opencode")
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
	if ids[0] != "opencode" && ids[1] != "opencode" {
		t.Fatalf("connected providers are not first: %v", ids)
	}
	byID := map[string]picker.Option{}
	for _, o := range cat.Options {
		byID[o.ID] = o
	}
	if got := byID["opencode"].Label; got != "OpenCode Zen" {
		t.Errorf("opencode label=%q want the engine's display name", got)
	}
	if got := byID["opencode"].Meta[picker.MetaConnected]; got != "true" {
		t.Errorf("opencode connected=%q", got)
	}
	if got := byID["302ai"].Meta[picker.MetaConnected]; got != "false" {
		t.Errorf("302ai connected=%q want false", got)
	}
	if got := byID["302ai"].Group; got != "All providers" {
		t.Errorf("302ai group=%q", got)
	}
	if got := byID["opencode"].Meta[picker.MetaDefaultModel]; got != "opencode/big-pickle" {
		t.Errorf("opencode default model=%q", got)
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
// endpoint is that the common path never downloads 4.3 MB.
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
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: "seeded"}
	cat, err := d.ListModelsLive(context.Background(), routedAPI(map[string]string{
		"/config/providers": `{"default": {}, "providers": [{"id":"opencode","models":{"m1":{}}}]}`,
	}))
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
	want := []string{"build", "plan"}
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
	// A command with no description falls back to its argument hints, reduced
	// exactly as the advertised command list reduces them: upstream order,
	// single-space joined, so the picker and the phone agree (MADR 0112 P1).
	for _, o := range cat.Options {
		if o.ID == "deploy" && o.Description != "stage prod" {
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

// ---------------------------------------------------------------------------
// MADR 0112 D6 — the engine's own default replaces a hand-ranked model list
// ---------------------------------------------------------------------------

// providerCatalog builds a GET /provider body with the given OpenCode Zen model
// IDs and engine default.
func zenCatalogBody(engineDefault string, zenModels ...string) string {
	models := make([]string, 0, len(zenModels))
	for _, id := range zenModels {
		models = append(models, fmt.Sprintf(
			`%q:{"id":%q,"name":%q,"limit":{"context":200000}}`, id, id, id))
	}
	def := "{}"
	if engineDefault != "" {
		def = fmt.Sprintf(`{"opencode":%q}`, engineDefault)
	}
	return fmt.Sprintf(`{"default":%s,"all":[{"id":"opencode","models":{%s}}]}`,
		def, strings.Join(models, ","))
}

func TestCatalogKnownGoodDefaultMatrix(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
		// wantWarn is true when the engine default and the seed are both gone
		// and the dialect had to fall back to an arbitrary catalog entry.
		wantWarn bool
	}{
		{
			name: "engine default wins when the catalog lists it",
			body: zenCatalogBody("big-pickle", "big-pickle", "other-model"),
			want: "big-pickle",
		},
		{
			// This is the case the old latency ranking got wrong: it would have
			// taken deepseek-v4-flash-free over the engine's own answer.
			name: "engine default wins over any other free model",
			body: zenCatalogBody("mimo-v2.5-free", "mimo-v2.5-free", "big-pickle", "x-preview-f-free"),
			want: "mimo-v2.5-free",
		},
		{
			name: "seed is used when the engine reports no default",
			body: zenCatalogBody("", "big-pickle", "other-model"),
			want: "big-pickle",
		},
		{
			name: "seed is used when the engine default is absent from the catalog",
			body: zenCatalogBody("ghost-model", "big-pickle", "other-model"),
			want: "big-pickle",
		},
		{
			name:     "an arbitrary catalog entry is taken when default and seed are both gone",
			body:     zenCatalogBody("ghost-model", "aardvark", "zebra"),
			want:     "aardvark", // lowest ID, for determinism
			wantWarn: true,
		},
		{
			name: "blank engine default is ignored",
			body: zenCatalogBody("   ", "big-pickle"),
			want: "big-pickle",
		},
		{
			// No OpenCode provider at all: keep the seed rather than adopting
			// some other vendor's model as an OpenCode Zen ID.
			name: "seed survives a catalog with no opencode provider",
			body: `{"default":{"anthropic":"claude-sonnet-4-5"},"all":[{"id":"anthropic","models":{"claude-sonnet-4-5":{"id":"claude-sonnet-4-5"}}}]}`,
			want: "big-pickle",
		},
		{
			name: "seed survives an empty catalog",
			body: `{"default":{},"all":[]}`,
			want: "big-pickle",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			log, recs := captureLogs(t)
			d := &httpDialect{
				log:                  log,
				defaultModelProvider: "opencode",
				defaultModelID:       zenDefaultModel,
			}
			d.AfterBoot(context.Background(), jsonAPI(c.body))
			gotProvider, gotID := d.fallbackModel()
			if gotProvider != "opencode" {
				t.Errorf("provider = %q, want opencode", gotProvider)
			}
			if gotID != c.want {
				t.Errorf("default model = %q, want %q", gotID, c.want)
			}
			warned := countMsg(*recs, "opencode default and seed model both absent from the catalog")
			if c.wantWarn && warned != 1 {
				t.Errorf("expected one fallback warning, got %d", warned)
			}
			if !c.wantWarn && warned != 0 {
				t.Errorf("unexpected fallback warning (%d)", warned)
			}
		})
	}
}

// A failed or slow catalog fetch must leave the seed in place. Session create
// does not wait on AfterBoot, so the seed is what the first prompt uses.
func TestCatalogDefaultSurvivesFetchFailure(t *testing.T) {
	for _, c := range []struct {
		name string
		api  httpagent.API
	}{
		{"transport error", func(context.Context, string, string, any, any) error {
			return fmt.Errorf("connection refused")
		}},
		{"malformed body", jsonAPI(`{"default":`)},
		{"cancelled context", func(ctx context.Context, _, _ string, _, _ any) error {
			return context.Canceled
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := &httpDialect{
				log:                  slog.Default(),
				defaultModelProvider: "opencode",
				defaultModelID:       zenDefaultModel,
			}
			d.AfterBoot(context.Background(), c.api)
			p, id := d.fallbackModel()
			if p != "opencode" || id != zenDefaultModel {
				t.Fatalf("fallback = %s/%s, want opencode/%s after a failed fetch", p, id, zenDefaultModel)
			}
		})
	}
}

// AfterBoot harvests context limits from the same fetch. A model with no limit
// must not land in the map as zero, which would render as "0 tokens of window".
func TestCatalogAfterBootHarvestsContextLimits(t *testing.T) {
	body := `{"default":{"opencode":"big-pickle"},"all":[
		{"id":"opencode","models":{
			"big-pickle":{"id":"big-pickle","limit":{"context":200000}},
			"no-limit":{"id":"no-limit"}
		}},
		{"id":"anthropic","models":{"claude-sonnet-4-5":{"id":"claude-sonnet-4-5","limit":{"context":1000000}}}}
	]}`
	d := &httpDialect{
		log:                  slog.Default(),
		defaultModelProvider: "opencode",
		defaultModelID:       zenDefaultModel,
	}
	d.AfterBoot(context.Background(), jsonAPI(body))
	d.mu.Lock()
	limits := d.contextLimits
	d.mu.Unlock()

	if got := limits["opencode/big-pickle"]; got != 200000 {
		t.Errorf("opencode/big-pickle limit = %d, want 200000", got)
	}
	if got := limits["anthropic/claude-sonnet-4-5"]; got != 1000000 {
		t.Errorf("anthropic/claude-sonnet-4-5 limit = %d, want 1000000", got)
	}
	if _, ok := limits["opencode/no-limit"]; ok {
		t.Error("a model without a context limit must be absent, not recorded as zero")
	}
}

// Explicit operator and session models still win: AfterBoot only decides what
// an *unspecified* model resolves to.
func TestCatalogDefaultDoesNotOverrideExplicitModel(t *testing.T) {
	d := &httpDialect{
		log:                  slog.Default(),
		defaultModelProvider: "opencode",
		defaultModelID:       zenDefaultModel,
	}
	d.AfterBoot(context.Background(), jsonAPI(zenCatalogBody("big-pickle", "big-pickle")))

	h := &captureHost{model: "anthropic/claude-sonnet-4-5"}
	s := d.NewSession(h).(*httpSession)
	p, id := s.resolveModel()
	if p != "anthropic" || id != "claude-sonnet-4-5" {
		t.Fatalf("resolveModel = %s/%s, want the configured anthropic/claude-sonnet-4-5", p, id)
	}
}

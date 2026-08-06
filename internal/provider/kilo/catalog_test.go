package kilo

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
			"~anthropic/claude-sonnet-4-5":{"id":"~anthropic/claude-sonnet-4-5","name":"Claude Sonnet 4.5","release_date":"2025-09-29","status":"active","limit":{"context":200000}}
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

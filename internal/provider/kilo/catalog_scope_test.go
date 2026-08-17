package kilo

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// gatewayLastCatalog reproduces the shape that made kilo-auto/frontier
// unreachable on kilo 7.4.22: the engine lists Kilo Gateway *last*, behind a
// vendor large enough to consume the whole option cap on its own. `kilo`
// carries the auto-routers and their recommendedIndex; `bulk` is the vendor
// standing in for openrouter's 351 models (MADR 0096 D2).
func gatewayLastCatalog(bulk int) string {
	var b strings.Builder
	b.WriteString(`{"default":{"kilo":"kilo-auto/balanced"},"providers":[`)
	fmt.Fprintf(&b, `{"id":"bulk","name":"Bulk Vendor","models":{`)
	for i := 0; i < bulk; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b,
			`"bulk-model-%03d":{"id":"bulk-model-%03d","name":"Bulk %03d","release_date":"2026-01-0%d","status":"active","limit":{"context":200000}}`,
			i, i, i, i%9+1)
	}
	b.WriteString(`}},`)
	// Every Gateway model shares one release_date, exactly as the engine
	// reports it — this is what makes ordering fall through to source order.
	b.WriteString(`{"id":"kilo","name":"Kilo Gateway","models":{
		"kilo-auto/frontier":{"id":"kilo-auto/frontier","name":"Auto Frontier","release_date":"2026-08-16","status":"active","recommendedIndex":0,"limit":{"context":1000000}},
		"kilo-auto/balanced":{"id":"kilo-auto/balanced","name":"Auto Balanced","release_date":"2026-08-16","status":"active","recommendedIndex":1,"limit":{"context":1000000}},
		"kilo-auto/efficient":{"id":"kilo-auto/efficient","name":"Auto Efficient","release_date":"2026-08-16","status":"active","recommendedIndex":2,"limit":{"context":1000000}},
		"kilo-auto/free":{"id":"kilo-auto/free","name":"Auto Free","release_date":"2026-08-16","status":"active","recommendedIndex":3,"limit":{"context":256000}},
		"anthropic/claude-sonnet-5":{"id":"anthropic/claude-sonnet-5","name":"Claude Sonnet 5","release_date":"2026-08-16","status":"active","limit":{"context":1000000}},
		"zzz/last-alphabetically":{"id":"zzz/last-alphabetically","name":"ZZZ","release_date":"2026-08-16","status":"active","limit":{"context":200000}}
	}}]}`)
	return b.String()
}

// TestDefaultCatalogLeadsWithEngineDefaultProvider is the regression guard for
// the reported bug: on the real 7.4.22 host the default catalog held 150
// options of which exactly one was a Kilo model, and kilo-auto/frontier was
// not among them (MADR 0096 D2).
func TestDefaultCatalogLeadsWithEngineDefaultProvider(t *testing.T) {
	api := routedAPI(map[string]string{
		"/config/providers": gatewayLastCatalog(maxDefaultCatalogModels * 2),
	})
	cat, err := newDialect().ListModelsLive(context.Background(), api)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Options) != maxDefaultCatalogModels {
		t.Fatalf("options = %d, want the cap %d", len(cat.Options), maxDefaultCatalogModels)
	}
	for _, want := range []string{
		"kilo/kilo-auto/frontier",
		"kilo/kilo-auto/balanced",
		"kilo/kilo-auto/efficient",
		"kilo/kilo-auto/free",
	} {
		if !hasOptionID(cat.Options, want) {
			t.Errorf("default catalog is missing %s", want)
		}
	}
	if g := cat.Options[0].Group; g != "kilo" {
		t.Errorf("first option group = %q, want the engine's default provider %q", g, "kilo")
	}
	// The cap still bites — the point is which rows survive it, and that the
	// reply admits the loss rather than reading as a complete list.
	if !cat.Truncated {
		t.Error("catalog dropped rows but reports Truncated = false")
	}
}

// TestDefaultCatalogNotTruncatedWhenItFits guards the other half of D3: a
// catalog that lost nothing must not claim it did.
func TestDefaultCatalogNotTruncatedWhenItFits(t *testing.T) {
	api := routedAPI(map[string]string{"/config/providers": connectedCatalog})
	cat, err := newDialect().ListModelsLive(context.Background(), api)
	if err != nil {
		t.Fatal(err)
	}
	if cat.Truncated {
		t.Errorf("catalog of %d options claims truncation", len(cat.Options))
	}
}

// TestModelsForIsDeterministic pins MADR 0096 D4. Before the sort in modelsOf,
// three consecutive live calls differed in 293+ of 295 positions, because
// every model shares one release_date and picker.OrderModels is a stable sort
// over a Go map's iteration order.
func TestModelsForIsDeterministic(t *testing.T) {
	api := routedAPI(map[string]string{"/config/providers": gatewayLastCatalog(4)})
	d := newDialect()
	var first []string
	for i := 0; i < 8; i++ {
		cat, err := d.ListModelsForLive(context.Background(), api, "kilo")
		if err != nil {
			t.Fatal(err)
		}
		ids := optionIDs(cat)
		if i == 0 {
			first = ids
			continue
		}
		if strings.Join(ids, ",") != strings.Join(first, ",") {
			t.Fatalf("run %d order differs:\n first: %v\n  this: %v", i, first, ids)
		}
	}
}

// TestRecommendedModelsRankFirst pins MADR 0096 D5: the engine's own
// recommendation order for the auto-routers, ahead of models it does not
// recommend, and unrecommended models still ordered deterministically.
func TestRecommendedModelsRankFirst(t *testing.T) {
	api := routedAPI(map[string]string{"/config/providers": gatewayLastCatalog(4)})
	cat, err := newDialect().ListModelsForLive(context.Background(), api, "kilo")
	if err != nil {
		t.Fatal(err)
	}
	ids := optionIDs(cat)
	// The session/engine default leads (picker's current-first band), then the
	// remaining recommendations by index, then everything else.
	want := []string{
		"kilo/kilo-auto/balanced",  // engine default for `kilo`
		"kilo/kilo-auto/frontier",  // recommendedIndex 0
		"kilo/kilo-auto/efficient", // recommendedIndex 2
		"kilo/kilo-auto/free",      // recommendedIndex 3
	}
	if len(ids) < len(want) {
		t.Fatalf("got %d options, want at least %d", len(ids), len(want))
	}
	for i, w := range want {
		if ids[i] != w {
			t.Errorf("option %d = %s, want %s (full order: %v)", i, ids[i], w, ids)
		}
	}
}

// TestOrderProvidersByLead covers the promotion in isolation, including the
// case that must not reorder anything: an engine default naming a provider it
// did not list.
func TestOrderProvidersByLead(t *testing.T) {
	ps := []providerEntry{{ID: "a"}, {ID: "b"}, {ID: "kilo"}, {ID: "c"}}
	ids := func(in []providerEntry) string {
		out := make([]string, 0, len(in))
		for _, p := range in {
			out = append(out, p.ID)
		}
		return strings.Join(out, ",")
	}
	if got := ids(orderProvidersByLead(ps, "kilo")); got != "kilo,a,b,c" {
		t.Errorf("lead kilo = %s, want kilo,a,b,c", got)
	}
	if got := ids(orderProvidersByLead(ps, "nope")); got != "a,b,kilo,c" {
		t.Errorf("unknown lead reordered the list: %s", got)
	}
	if got := ids(orderProvidersByLead(ps, "")); got != "a,b,kilo,c" {
		t.Errorf("empty lead reordered the list: %s", got)
	}
	if got := ids(ps); got != "a,b,kilo,c" {
		t.Errorf("input was mutated: %s", got)
	}
}

// TestRecommendedIndexReachesTheOption is the decode guard: the engine sends
// recommendedIndex as a bare JSON number, and 0 (the top recommendation) must
// not be indistinguishable from absent.
func TestRecommendedIndexReachesTheOption(t *testing.T) {
	zero := 0
	opt := modelOption("kilo", providerModel{
		ID: "kilo-auto/frontier", Name: "Auto Frontier", RecommendedIndex: &zero,
	}, "kilo-auto/frontier")
	if got := opt.Meta[picker.MetaRecommendedIndex]; got != "0" {
		t.Errorf("meta[%s] = %q, want %q", picker.MetaRecommendedIndex, got, "0")
	}
	plain := modelOption("kilo", providerModel{ID: "x/y", Name: "Y"}, "x/y")
	if _, ok := plain.Meta[picker.MetaRecommendedIndex]; ok {
		t.Error("unrecommended model carries a recommended_index key")
	}
}

// TestSoleSlashCommandAcceptsMCPNamespacedNames guards the routing of
// MCP-sourced commands. kilo 7.4.22's GET /command advertises them namespaced
// (`magictools:pipeline-start`, source "mcp"); rejecting the colon here sent
// them to the model as prompt text while still offering them in autocomplete.
func TestSoleSlashCommandAcceptsMCPNamespacedNames(t *testing.T) {
	for _, tc := range []struct {
		in       string
		wantName string
		wantArgs string
		wantOK   bool
	}{
		{"/magictools:pipeline-start", "magictools:pipeline-start", "", true},
		{"/magictools:pipeline-start go", "magictools:pipeline-start", "go", true},
		{"/resume-claude", "resume-claude", "", true},
		{"/init", "init", "", true},
		// A leading colon is not a namespace, and a path is still not a command.
		{"/:nope", "", "", false},
		{"/etc/hosts", "", "", false},
	} {
		name, args, ok := soleSlashCommand([]provider.Content{{Type: "text", Text: tc.in}})
		if ok != tc.wantOK || name != tc.wantName || args != tc.wantArgs {
			t.Errorf("%q -> (%q, %q, %v), want (%q, %q, %v)",
				tc.in, name, args, ok, tc.wantName, tc.wantArgs, tc.wantOK)
		}
	}
}

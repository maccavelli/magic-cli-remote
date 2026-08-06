package kilo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

// The shape measured against kilo 7.4.20 on 2026-08-06, spike day
// (docs/kilo-spike-7.4.20/provider-summary.json: provider_count 179, sum of
// per-provider model_count 6,006). This is Kilo's OWN measured scale, not a
// copy of opencode's (172 providers / 5,788 models) — kilo's real catalog is
// larger on both axes (MADR 0076 M4 #3). The later plan P3 acceptance run
// recorded 181 live providers but no paired model count was captured, so the
// spike-day pair is used here as the documented, reproducible source (MADR
// 0076 plan P6 step 2).
//
// realConnectedCount/realConnectedModels come from the same spike artifact's
// three connected providers (openrouter 337, huggingface 57, kilo 283 = 677)
// — also larger than opencode's measured 113, which makes this frame-budget
// guard a stronger regression net here than it was on opencode.
//
// The fixtures below are synthesized to these proportions rather than checked
// in: the real /config/providers response contains the user's API keys, and
// a multi-MB /provider dump does not belong in the repo.
const (
	realProviderCount   = 179
	realModelCount      = 6006
	realConnectedCount  = 3
	realConnectedModels = 677
)

// syntheticFull builds a /provider body with the measured provider and model
// counts and realistic id lengths.
func syntheticFull() string {
	var b strings.Builder
	b.WriteString(`{"default":{"kilo":"kilo-auto/balanced"},"connected":["p000","p001","p002"],"all":[`)
	perProvider := realModelCount / realProviderCount
	for p := 0; p < realProviderCount; p++ {
		if p > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":"p%03d","name":"Provider %03d","models":{`, p, p)
		for m := 0; m < perProvider; m++ {
			if m > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b,
				`"some-fairly-long-model-identifier-%03d":{"id":"some-fairly-long-model-identifier-%03d","name":"Some Model Display Name %03d","release_date":"2026-0%d-1%d","status":"active","limit":{"context":200000}}`,
				m, m, m, m%9+1, m%9)
		}
		b.WriteString("}}")
	}
	b.WriteString("]}")
	return b.String()
}

// syntheticConnected builds a /config/providers body with the measured
// connected-provider and model counts.
func syntheticConnected() string {
	var b strings.Builder
	b.WriteString(`{"default":{"p000":"some-fairly-long-model-identifier-000"},"providers":[`)
	perProvider := realConnectedModels / realConnectedCount
	for p := 0; p < realConnectedCount; p++ {
		if p > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":"p%03d","name":"Provider %03d","key":"SENTINEL","models":{`, p, p)
		for m := 0; m < perProvider; m++ {
			if m > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b,
				`"some-fairly-long-model-identifier-%03d":{"id":"some-fairly-long-model-identifier-%03d","name":"Some Model Display Name %03d","release_date":"2026-0%d-1%d","status":"active","limit":{"context":1000000}}`,
				m, m, m, m%9+1, m%9)
		}
		b.WriteString("}}")
	}
	b.WriteString("]}")
	return b.String()
}

func TestCapDefaultCatalogModelsKeepsDefault(t *testing.T) {
	opts := make([]picker.Option, 0, 10)
	for i := 0; i < 10; i++ {
		opts = append(opts, picker.Option{ID: fmt.Sprintf("p/m%02d", i)})
	}
	// Default is past the cap; it must be forced into the kept window.
	got := capDefaultCatalogModels(opts, "p/m09", 5)
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5", len(got))
	}
	found := false
	for _, o := range got {
		if o.ID == "p/m09" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("default p/m09 missing from capped list: %+v", got)
	}
	// Under the cap, unchanged.
	small := opts[:3]
	if out := capDefaultCatalogModels(small, "p/m00", 5); len(out) != 3 {
		t.Fatalf("under-cap len = %d", len(out))
	}
}

// TestDefaultCatalogFitsTheFrame is the regression guard for the reason this
// work exists, at Kilo's own measured scale — larger than opencode's, so this
// is a stronger guard here than the one it is ported from (MADR 0076 M4 #3).
func TestDefaultCatalogFitsTheFrame(t *testing.T) {
	api := routedAPI(map[string]string{
		"/config/providers": syntheticConnected(),
		"/provider":         syntheticFull(),
	})
	cat, err := newDialect().ListModelsLive(context.Background(), api)
	if err != nil {
		t.Fatal(err)
	}
	body := protocol.ModelsResultFromCatalog("kilo", cat)
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("default catalog: %d options, %d bytes (real shape: %d options / connected models)",
		len(cat.Options), len(b), realConnectedModels)
	if len(cat.Options) > 200 {
		t.Errorf("default catalog has %d options; the connected set is ~%d", len(cat.Options), realConnectedModels)
	}
	const budget = 32 << 10
	if len(b) > budget {
		t.Errorf("default reply is %d bytes, over the %d-byte budget", len(b), budget)
	}
}

// TestProviderCatalogFitsTheFrame: the provider step enumerates all real
// providers, and that list must stay small too — it is one row each, not one
// row per model.
func TestProviderCatalogFitsTheFrame(t *testing.T) {
	api := routedAPI(map[string]string{
		"/config/providers": syntheticConnected(),
		"/provider":         syntheticFull(),
	})
	cat, err := newDialect().ListModelProvidersLive(context.Background(), api)
	if err != nil {
		t.Fatal(err)
	}
	body := protocol.ModelsResultFromCatalog("kilo", cat)
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("provider catalog: %d options, %d bytes", len(cat.Options), len(b))
	if len(cat.Options) != realProviderCount {
		t.Errorf("provider catalog has %d options, want all %d", len(cat.Options), realProviderCount)
	}
	const budget = 64 << 10
	if len(b) > budget {
		t.Errorf("provider reply is %d bytes, over the %d-byte budget", len(b), budget)
	}
}

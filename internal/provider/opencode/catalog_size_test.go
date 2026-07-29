package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/protocol"
)

// The shape measured against opencode 1.18.7 on 2026-07-28 (MADR 0043 §2.1).
// The fixtures below are synthesized to these proportions rather than checked
// in: the real /config/providers response contains the user's API keys, and a
// 4.3 MB /provider dump does not belong in the repo.
const (
	realProviderCount   = 172
	realModelCount      = 5788
	realConnectedCount  = 3
	realConnectedModels = 113
)

// syntheticFull builds a /provider body with the measured provider and model
// counts and realistic id lengths.
func syntheticFull() string {
	var b strings.Builder
	b.WriteString(`{"default":{"opencode":"big-pickle"},"connected":["p000","p001","p002"],"all":[`)
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
// work exists. The unscoped catalog serialized to 531,554 bytes — 51% of the
// relay's 1 MiB per-message cap — and grew with models.dev. The default reply
// must now be a small fraction of that.
func TestDefaultCatalogFitsTheFrame(t *testing.T) {
	api := routedAPI(map[string]string{
		"/config/providers": syntheticConnected(),
		"/provider":         syntheticFull(),
	})
	cat, err := newDialect().ListModelsLive(context.Background(), api)
	if err != nil {
		t.Fatal(err)
	}
	body := protocol.ModelsResultFromCatalog("opencode", cat)
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("default catalog: %d options, %d bytes (was 5,788 options / 531,554 bytes)",
		len(cat.Options), len(b))
	if len(cat.Options) > 200 {
		t.Errorf("default catalog has %d options; the connected set is ~113", len(cat.Options))
	}
	const budget = 32 << 10
	if len(b) > budget {
		t.Errorf("default reply is %d bytes, over the %d-byte budget", len(b), budget)
	}
}

// TestProviderCatalogFitsTheFrame: the provider step enumerates all 172
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
	body := protocol.ModelsResultFromCatalog("opencode", cat)
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

//go:build live_kilo

package kilo

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveProviderUsageAnswersTheShapeWeParse pins MADR 0138 F9's premise: that
// kilo answers plan usage structurally, so the daemon does not have to
// reconstruct it from English in engine log lines.
//
// A decision that rests on how an external CLI behaves needs a live test, or it
// is an assumption with a future bug report attached (AGENTS.md). This one
// spends **no model tokens**: it is a single GET against an endpoint documented
// as "cache-aware, secret-free provider plan usage and personal billing
// status", and it invokes nothing.
//
// Run with:
//
//	MCREMOTE_KILO_ENGINE=127.0.0.1:PORT \
//	  go test -tags live_kilo ./internal/provider/kilo/ -run LiveProviderUsage -count=1
//
// The port is whatever `kilo serve` is listening on; `mcremote engines` reports
// the one the daemon started.
func TestLiveProviderUsageAnswersTheShapeWeParse(t *testing.T) {
	addr := strings.TrimSpace(os.Getenv("MCREMOTE_KILO_ENGINE"))
	if addr == "" {
		t.Skip("set MCREMOTE_KILO_ENGINE to a running `kilo serve` host:port")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+addr+"/kilocode/provider-usage", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("engine unreachable at %s: %v", addr, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /kilocode/provider-usage -> %d, want 200", res.StatusCode)
	}

	// Decode into the exact struct the production path uses. Strict, so a field
	// this code names and the engine has renamed is a failure rather than a
	// silent zero.
	dec := json.NewDecoder(res.Body)
	dec.DisallowUnknownFields()
	var usage providerUsage
	if err := dec.Decode(&usage); err != nil {
		t.Fatalf("provider-usage no longer decodes into providerUsage: %v\n"+
			"re-read components.schemas.ProviderUsage* from the engine's GET /doc", err)
	}
	if strings.TrimSpace(usage.GeneratedAt) == "" {
		t.Fatal("generatedAt is empty; the response shape has changed")
	}

	// The summary must not invent a limit. An account with nothing exhausted —
	// which is this host's, returning {"items":[],...} — must summarise to "".
	summary := usage.exhaustedWindows()
	t.Logf("plans=%d generatedAt=%s exhausted=%q", len(usage.Items), usage.GeneratedAt, summary)
	for _, item := range usage.Items {
		anyExhausted := false
		for _, w := range item.Windows {
			if w.State == "exhausted" {
				anyExhausted = true
			}
		}
		if !anyExhausted && strings.Contains(summary, item.ProviderID) {
			t.Fatalf("summary names %q but none of its windows is exhausted", item.ProviderID)
		}
	}
	if len(usage.Items) == 0 && summary != "" {
		t.Fatalf("no plans reported but summary = %q", summary)
	}
}

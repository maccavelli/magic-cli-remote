//go:build live_kilo

package kilo_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/kilo"
)

// MADR 0074 D15: pin the real engine's auth surface. mcremote writes to and
// reads from three third-party formats; a vendor change must fail here rather
// than in the field. If this breaks, re-probe the engine and update both the
// testdata fixture and MADR 0074 §5.1 before touching the parser.
func TestLiveKiloAuthCatalogShape(t *testing.T) {
	p := kilo.NewHTTP(kilo.Config{})
	if !p.Ready() {
		t.Skip("kilo not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// AuthStatus needs a booted engine for the catalog half; Start is the
	// supported way to get one.
	s, err := p.Start(ctx, provider.StartOptions{Name: "kilo-auth-probe", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	st, err := p.AuthStatus(ctx)
	if err != nil {
		t.Fatalf("AuthStatus: %v", err)
	}
	if len(st.Upstreams) == 0 {
		t.Fatal("live catalog returned no upstreams")
	}

	var withInputs, device, browser, apiKey int
	for _, up := range st.Upstreams {
		for _, m := range up.Methods {
			switch m.Type {
			case provider.AuthMethodOAuthDevice:
				device++
			case provider.AuthMethodOAuthBrowser:
				browser++
			case provider.AuthMethodAPIKey:
				apiKey++
			}
			if len(m.Inputs) > 0 {
				withInputs++
			}
			for _, in := range m.Inputs {
				if in.Key == "" {
					t.Errorf("%s/%s has an input with no key", up.ID, m.ID)
				}
				if in.Type == provider.AuthInputSelect && len(in.Options) == 0 {
					t.Errorf("%s/%s select input %q has no options", up.ID, m.ID, in.Key)
				}
			}
		}
	}
	if apiKey == 0 {
		t.Error("no api_key methods in the live catalog")
	}
	if device == 0 && browser == 0 {
		t.Error("no oauth methods in the live catalog")
	}
	// 8 of 13 declared inputs when this was written. Any change is a real
	// catalog change worth a human look, so assert the floor rather than
	// the exact number.
	if withInputs == 0 {
		t.Error("no method declares inputs; MADR 0074 D5 was written because 8 did")
	}
	t.Logf("live catalog: %d upstreams, api=%d device=%d browser=%d, %d methods with inputs",
		len(st.Upstreams), apiKey, device, browser, withInputs)
}

// The catalog must never carry key material, even though the engine's
// /config/providers response does (MADR 0074 D2).
func TestLiveKiloAuthStatusHasNoKeys(t *testing.T) {
	p := kilo.NewHTTP(kilo.Config{})
	if !p.Ready() {
		t.Skip("kilo not in PATH")
	}
	defer p.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	s, err := p.Start(ctx, provider.StartOptions{Name: "kilo-auth-secret-probe", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	st, err := p.AuthStatus(ctx)
	if err != nil {
		t.Fatalf("AuthStatus: %v", err)
	}
	for _, up := range st.Upstreams {
		blob := up.ID + " " + up.Label + " " + up.Status
		for _, m := range up.Methods {
			blob += " " + m.ID + " " + m.Label
		}
		if strings.Contains(blob, "sk-") {
			t.Fatalf("live auth state carried something key-shaped: %s", blob)
		}
	}
}

// TestLiveKiloAuthCatalog pins the D16 surface against a real kilo engine.
//
// Run: go test -tags live_kilo ./internal/provider/kilo/ -run TestLiveKiloAuthCatalog -count=1 -v
func TestLiveKiloAuthCatalog(t *testing.T) {
	p := kilo.NewHTTP(kilo.Config{})
	if !p.Ready() {
		t.Skip("kilo not in PATH")
	}
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// The catalog is an engine read, so boot one the supported way.
	s, err := p.Start(ctx, provider.StartOptions{Name: "kilo-catalog-probe", CWD: t.TempDir()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close(context.Background())

	cataloger, ok := any(p).(provider.AuthCataloger)
	if !ok {
		t.Fatal("kilo provider does not implement AuthCataloger")
	}
	cat, err := cataloger.AuthCatalogList(ctx)
	if err != nil {
		t.Fatalf("AuthCatalogList: %v", err)
	}
	t.Logf("catalog: %d upstreams (185 on kilo 7.4.21)", len(cat.Upstreams))
	if len(cat.Upstreams) < 50 {
		t.Fatalf("catalog has only %d upstreams; the vendor list did not load", len(cat.Upstreams))
	}
	byID := map[string]provider.UpstreamAuth{}
	for _, up := range cat.Upstreams {
		byID[up.ID] = up
	}
	// The key-only vendors D16 exists to reach, plus one whose auth carries
	// declared inputs from GET /provider/auth.
	for _, id := range []string{"togetherai", "deepseek", "anthropic", "github-copilot"} {
		if up, ok := byID[id]; !ok || len(up.Methods) == 0 {
			t.Errorf("live catalog is missing usable methods for %q", id)
		}
	}
}

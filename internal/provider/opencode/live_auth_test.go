//go:build live_opencode

package opencode_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
)

// TestLiveOpenCodeAuthCatalog pins the surface MADR 0074 D16 is built on
// (D15: every asserted auth surface has a live test).
//
// It is read-only. The write half is TestLiveOpenCodeCredentialRoundTrip
// below, which is opt-in because it touches the host's real auth.json.
//
// Run: go test -tags live_opencode ./internal/provider/opencode/ -run TestLiveOpenCodeAuth -count=1 -v
func TestLiveOpenCodeAuthCatalog(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not in PATH")
	}
	p := opencode.NewHTTP(opencode.Config{})
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cataloger, ok := any(p).(provider.AuthCataloger)
	if !ok {
		t.Fatal("opencode provider does not implement AuthCataloger")
	}
	cat, err := cataloger.AuthCatalogList(ctx)
	if err != nil {
		t.Fatalf("AuthCatalogList: %v", err)
	}
	if cat.Source != provider.AuthCatalogSourceEngine {
		t.Errorf("source = %q, want engine", cat.Source)
	}
	t.Logf("catalog: %d upstreams (184 on opencode 1.18.16)", len(cat.Upstreams))
	if len(cat.Upstreams) < 50 {
		t.Fatalf("catalog has only %d upstreams; the vendor list did not load", len(cat.Upstreams))
	}

	byID := map[string]provider.UpstreamAuth{}
	for _, up := range cat.Upstreams {
		byID[up.ID] = up
	}
	// The classes D16 exists to cover: a plain key-only vendor, and one whose
	// auth needs declared inputs. If either disappears from the live catalog,
	// the phone's setup sheet is describing a surface that no longer exists.
	for _, id := range []string{"togetherai", "deepseek", "anthropic", "github-copilot"} {
		up, ok := byID[id]
		if !ok {
			t.Errorf("live catalog is missing %q", id)
			continue
		}
		if len(up.Methods) == 0 {
			t.Errorf("%s has no methods", id)
		}
	}

	// One page must fit a phone frame comfortably; that is why the wire form
	// pages at 100 (MADR 0074 D16).
	page := cat.Upstreams
	if len(page) > 100 {
		page = page[:100]
	}
	ups := make([]protocol.UpstreamAuthPayload, 0, len(page))
	for _, up := range page {
		ups = append(ups, protocol.UpstreamAuthPayload{ID: up.ID, Label: up.Label, Status: up.Status})
	}
	body, err := json.Marshal(protocol.AuthCatalogPayload{ProviderID: "opencode", Upstreams: ups})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("one page of %d upstreams is %d bytes", len(page), len(body))
	const budget = 64 << 10
	if len(body) > budget {
		t.Errorf("a catalog page is %d bytes, over the %d-byte budget", len(body), budget)
	}

	// Status is the small block; it must not have become the catalog.
	auth, ok := any(p).(provider.Auth)
	if !ok {
		t.Fatal("opencode provider does not implement Auth")
	}
	st, err := auth.AuthStatus(ctx)
	if err != nil {
		t.Fatalf("AuthStatus: %v", err)
	}
	t.Logf("status: %d upstreams, active %q", len(st.Upstreams), st.ActiveUpstream)
	if len(st.Upstreams) >= len(cat.Upstreams) {
		t.Errorf("status carries %d upstreams against a %d-entry catalog; the two have collapsed",
			len(st.Upstreams), len(cat.Upstreams))
	}
}

// TestLiveOpenCodeCredentialRoundTrip proves the write path against a real
// engine: PUT then DELETE for a vendor the host does not use.
//
// Opt-in via MCREMOTE_LIVE_AUTH_WRITE=1 because it writes the host's real
// ~/.local/share/opencode/auth.json. It picks a scratch vendor and removes it
// again, but the standing rule from MADR 0074 applies: snapshot the store
// before running credential probes.
func TestLiveOpenCodeCredentialRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not in PATH")
	}
	if os.Getenv("MCREMOTE_LIVE_AUTH_WRITE") != "1" {
		t.Skip("set MCREMOTE_LIVE_AUTH_WRITE=1 to run the write round trip")
	}
	p := opencode.NewHTTP(opencode.Config{})
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	w, ok := any(p).(provider.AuthWriter)
	if !ok {
		t.Fatal("opencode provider does not implement AuthWriter")
	}
	const scratch = "togetherai"
	const key = "mcremote-live-probe-not-a-real-key"

	if err := w.SetCredential(ctx, scratch, scratch+":api", key, nil); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
	cataloger := any(p).(provider.AuthCataloger)
	cat, err := cataloger.AuthCatalogList(ctx)
	if err != nil {
		t.Fatalf("AuthCatalogList: %v", err)
	}
	configured := false
	for _, up := range cat.Upstreams {
		if up.ID == scratch && up.Status == provider.AuthConfigured {
			configured = true
		}
	}
	if !configured {
		t.Errorf("%s did not read back as configured after a write", scratch)
	}
	if err := w.ClearCredential(ctx, scratch); err != nil {
		t.Fatalf("ClearCredential: %v", err)
	}

	// MADR 0083 D2: metadata round-trip — the engine must accept the ApiAuth
	// metadata field its OpenAPI declares. cloudflare-workers-ai declares an
	// accountId prompt in the live catalog.
	const typed = "cloudflare-workers-ai"
	if err := w.SetCredential(ctx, typed, typed+":0", key,
		map[string]string{"accountId": "0123456789abcdef"}); err != nil {
		t.Fatalf("SetCredential with metadata: %v", err)
	}
	if err := w.ClearCredential(ctx, typed); err != nil {
		t.Fatalf("ClearCredential typed: %v", err)
	}
}

// TestLiveOpenCodeDeviceFlowStarts proves the MADR 0083 D3 wiring against the
// real engine: an oauth-typed vendor either starts a device flow (URL + code
// come back) or is refused as a browser flow after a real authorize round
// trip — never the bare "unsupported" the missing dialect used to produce.
// No flow is completed: pending vendor device codes simply expire.
func TestLiveOpenCodeDeviceFlowStarts(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not in PATH")
	}
	p := opencode.NewHTTP(opencode.Config{})
	defer p.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cat, err := any(p).(provider.AuthCataloger).AuthCatalogList(ctx)
	if err != nil {
		t.Fatalf("AuthCatalogList: %v", err)
	}
	da, ok := any(p).(provider.DeviceAuth)
	if !ok {
		t.Fatal("opencode provider does not implement DeviceAuth")
	}

	tried, started, browser := 0, 0, 0
	for _, up := range cat.Upstreams {
		for _, m := range up.Methods {
			if m.Type != provider.AuthMethodOAuthDevice {
				continue
			}
			tried++
			flow, _, derr := da.StartDeviceAuth(ctx, up.ID, m.ID, nil, false)
			switch {
			case derr == nil && flow.UserCode != "":
				// The wait func is simply abandoned; the vendor-side code
				// expires on its own.
				started++
			case derr != nil && strings.Contains(derr.Error(), "browser"):
				browser++
			default:
				t.Logf("%s %s: %v", up.ID, m.ID, derr)
			}
			if tried >= 4 || started > 0 {
				break
			}
		}
		if tried >= 4 || started > 0 {
			break
		}
	}
	t.Logf("device-flow probe: tried %d, started %d, browser-refused %d", tried, started, browser)
	if tried == 0 {
		t.Skip("live catalog advertises no oauth_device methods")
	}
	if started == 0 && browser == 0 {
		t.Fatal("no oauth vendor started a device flow or was refused as browser — the dialect looks unwired")
	}
}

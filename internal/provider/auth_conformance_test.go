package provider_test

import (
	"context"
	"errors"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/codex"
	"github.com/maccavelli/magic-cli-remote/internal/provider/goose"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
	"github.com/maccavelli/magic-cli-remote/internal/provider/kilo"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
)

// The auth surface is a set of optional interfaces, so a provider can silently
// stop offering one by losing a method — nothing fails to compile, the
// affordance just disappears from the phone. This test is the guard.
//
// It asserts the *type* side only. Two of the transports (acpagent, httpagent)
// declare every auth method on the shared Provider and delegate to a dialect
// or spec hook that may be absent, so a type assertion succeeds for them
// regardless; what a provider actually supports is decided at runtime with
// ErrAuthUnsupported. TestSingleVendorAgentsHaveNoCatalog below covers that
// half for D19.
func TestProviderAuthInterfaceConformance(t *testing.T) {
	cases := []struct {
		name            string
		p               provider.Provider
		wantStatus      bool
		wantWriter      bool
		wantCatalog     bool
		wantSwitcher    bool
		wantDeviceOAuth bool
	}{
		{
			name: "opencode", p: opencode.NewHTTP(opencode.Config{}),
			wantStatus: true, wantWriter: true, wantCatalog: true,
			wantSwitcher: true, wantDeviceOAuth: true,
		},
		{
			name: "kilo", p: kilo.NewHTTP(kilo.Config{}),
			wantStatus: true, wantWriter: true, wantCatalog: true,
			wantSwitcher: true, wantDeviceOAuth: true,
		},
		{
			name: "goose", p: goose.New(goose.Config{}),
			wantStatus: true, wantWriter: true, wantCatalog: true,
			// No device flow: the acphttp transport goose rides carries no
			// StartDeviceAuth at all, and goose's own OAuth is loopback (W3).
			wantSwitcher: true, wantDeviceOAuth: false,
		},
		{
			name: "codex", p: codex.New(codex.Config{}),
			wantStatus: true, wantWriter: true, wantCatalog: false,
			wantDeviceOAuth: true,
		},
		{
			name: "grok", p: grok.New(grok.Config{}),
			wantStatus: true, wantWriter: true, wantCatalog: true,
			wantSwitcher: true, wantDeviceOAuth: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := tc.p.(provider.Auth); ok != tc.wantStatus {
				t.Errorf("Auth: got %v, want %v", ok, tc.wantStatus)
			}
			if _, ok := tc.p.(provider.AuthWriter); ok != tc.wantWriter {
				t.Errorf("AuthWriter: got %v, want %v", ok, tc.wantWriter)
			}
			if _, ok := tc.p.(provider.AuthCataloger); ok != tc.wantCatalog {
				t.Errorf("AuthCataloger: got %v, want %v (MADR 0074 D16/D19)", ok, tc.wantCatalog)
			}
			if _, ok := tc.p.(provider.UpstreamSwitcher); ok != tc.wantSwitcher {
				t.Errorf("UpstreamSwitcher: got %v, want %v", ok, tc.wantSwitcher)
			}
			if _, ok := tc.p.(provider.DeviceAuth); ok != tc.wantDeviceOAuth {
				t.Errorf("DeviceAuth: got %v, want %v", ok, tc.wantDeviceOAuth)
			}
		})
	}
}

// D19: codex and grok each talk to exactly one vendor, so browsing a catalog
// is meaningless for them and must fail cleanly rather than return an empty
// list the phone would render as "this agent supports nothing".
func TestSingleVendorAgentsHaveNoCatalog(t *testing.T) {
	for name, p := range map[string]provider.Provider{
		"codex": codex.New(codex.Config{}),
		"grok":  grok.New(grok.Config{}),
	} {
		t.Run(name, func(t *testing.T) {
			c, ok := p.(provider.AuthCataloger)
			if !ok {
				return // no method at all is also a correct answer
			}
			if _, err := c.AuthCatalogList(context.Background()); !errors.Is(err, provider.ErrAuthUnsupported) {
				t.Fatalf("AuthCatalogList err = %v, want ErrAuthUnsupported (MADR 0074 D19)", err)
			}
		})
	}
}

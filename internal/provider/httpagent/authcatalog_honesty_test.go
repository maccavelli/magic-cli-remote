package httpagent

import (
	"context"
	"errors"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/protocol"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestBuildCatalogDoesNotSynthesiseKiloAPIKey(t *testing.T) {
	got := BuildCatalog(
		[]VendorEntry{{ID: "kilo", Name: "Kilo Gateway"}},
		nil,
		nil,
	)
	if len(got) != 1 || len(got[0].Methods) != 1 {
		t.Fatalf("got %+v", got)
	}
	m := got[0].Methods[0]
	if m.Type == provider.AuthMethodAPIKey || m.ID == "kilo:api" {
		t.Fatalf("synthesised kilo API key: %+v", m)
	}
	if !m.Unavailable || m.Reason != protocol.AuthReasonHostOAuth {
		t.Fatalf("kilo method = %+v, want unavailable host_oauth", m)
	}
}

func TestBuildCatalogStillSynthesisesTogether(t *testing.T) {
	got := BuildCatalog(
		[]VendorEntry{{ID: "togetherai", Name: "Together AI"}},
		nil,
		nil,
	)
	if len(got) != 1 || len(got[0].Methods) != 1 {
		t.Fatalf("got %+v", got)
	}
	m := got[0].Methods[0]
	if m.ID != "togetherai:api" || m.Type != provider.AuthMethodAPIKey || m.Unavailable {
		t.Fatalf("togetherai method = %+v", m)
	}
}

func TestSuperGrokSubscriptionClassifiesBrowser(t *testing.T) {
	got := ClassifyCatalogMethod("oauth", "xAI Grok OAuth (SuperGrok Subscription)")
	if got != provider.AuthMethodOAuthBrowser {
		t.Fatalf("classify = %q, want oauth_browser (P0 loopback)", got)
	}
}

func TestHeadlessVPSClassifiesDevice(t *testing.T) {
	got := ClassifyCatalogMethod("oauth", "xAI Grok OAuth (Headless / Remote / VPS)")
	if got != provider.AuthMethodOAuthDevice {
		t.Fatalf("classify = %q, want oauth_device (P0 RFC 8628)", got)
	}
}

func TestVerifyAPIKeyMethodRefusesKiloAPI(t *testing.T) {
	err := VerifyAPIKeyMethod(context.Background(), nil, "kilo", "kilo:api")
	if !errors.Is(err, provider.ErrAuthMethodUnsupported) {
		t.Fatalf("err=%v, want ErrAuthMethodUnsupported", err)
	}
	if err := VerifyAPIKeyMethod(context.Background(), nil, "togetherai", "togetherai:api"); err != nil {
		t.Fatalf("togetherai:api: %v", err)
	}
}

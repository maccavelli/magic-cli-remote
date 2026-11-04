package daemon

import (
	"crypto/tls"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/provider/fake"
)

func TestEventHubBroadcastNilServerNoPanic(t *testing.T) {
	hub := &eventHub{}
	hub.Broadcast(event.Event{Type: event.TypeSessionStatus})
}

func TestRequestClientCertSetsRequestClientCert(t *testing.T) {
	cfg := &tls.Config{}
	got := requestClientCert(cfg)
	if got.ClientAuth != tls.RequestClientCert {
		t.Fatalf("ClientAuth = %v, want RequestClientCert", got.ClientAuth)
	}
}

func TestRequestClientCertNilInput(t *testing.T) {
	got := requestClientCert(nil)
	if got != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestRequestClientCertPreservesOtherFields(t *testing.T) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "test"}
	got := requestClientCert(cfg)
	if got.MinVersion != tls.VersionTLS13 || got.ServerName != "test" {
		t.Fatalf("fields not preserved: %+v", got)
	}
}

func TestIdentityCloseNil(t *testing.T) {
	var id *Identity
	id.Close()
}

func TestIdentityCloseNilACME(t *testing.T) {
	id := &Identity{Mode: config.TLSModeSelfSigned}
	id.Close()
}

// TestADescribedProviderRegistersWhenItsSectionIsEnabled is 0208 PLAN P8
// step 3: the daemon builds a provider from the registration list and its
// section, and a Config without the section takes the section's defaults.
func TestADescribedProviderRegistersWhenItsSectionIsEnabled(t *testing.T) {
	cfg := config.Defaults()
	if _, err := InspectProviders(cfg, "test", nil).Get(provider.IDFake); err == nil {
		t.Error("fake is registered from its defaults, which disable it")
	}
	cfg.Providers.Sections = config.ProviderSections{"fake": &fake.Section{Enabled: true}}
	p, err := InspectProviders(cfg, "test", nil).Get(provider.IDFake)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*fake.Provider); !ok {
		t.Fatalf("fake is registered as %T, want *fake.Provider", p)
	}
}

// TestBuildBaseCarriesTheRuntimeDirAndVersion: codex generates its engine's
// secret material into the daemon's runtime directory, so every described
// provider is built with it (0208 PLAN P8, codex commit).
func TestBuildBaseCarriesTheRuntimeDirAndVersion(t *testing.T) {
	cfg := config.Defaults()
	cfg.Paths.RuntimeDir = "/run/mcremote-under-test"
	b := buildBase(cfg, "9.9.9", nil)
	if b.RuntimeDir != "/run/mcremote-under-test" || b.Version != "9.9.9" {
		t.Fatalf("build base = %+v", b)
	}
}

// TestOpencodeRegistersTheV2Spec: the daemon's opencode provider is the ACP
// one, not the retired 1.x dialect (0214 PLAN P7, step 1). Its defaults
// enable it.
func TestOpencodeRegistersTheV2Spec(t *testing.T) {
	cfg := config.Defaults()
	p, err := InspectProviders(cfg, "test", nil).Get(provider.IDOpencode)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*acpagent.Provider); !ok {
		t.Fatalf("opencode is registered as %T, want *acpagent.Provider", p)
	}
}

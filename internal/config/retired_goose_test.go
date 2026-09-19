package config_test

import (
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/config"
)

// Every case loads an explicit file under t.TempDir() (MADR 0160 C8): a bare
// Load(LoadOptions{}) reads the host's live config on Windows. Diagnostics are
// found by code, never by count, because temp-dir configs on Windows also draw
// config_not_owner_only.

const retiredGooseDiag = "retired_provider_goose"

func findDiag(cfg config.Config, code string) (appdirs.Diagnostic, bool) {
	for _, d := range cfg.Diagnostics {
		if d.Code == code {
			return d, true
		}
	}
	return appdirs.Diagnostic{}, false
}

func loadRetiredGoose(t *testing.T, body string) config.Config {
	t.Helper()
	cfg, err := config.Load(config.LoadOptions{ConfigFile: writeConfig(t, body)})
	if err != nil {
		t.Fatalf("Load must not fail on a leftover goose setting (MADR 0160 D3): %v", err)
	}
	return cfg
}

func TestRetiredGoosePopulatedBlockWarns(t *testing.T) {
	cfg := loadRetiredGoose(t, "providers:\n  goose:\n    enabled: true\n    bin: goose\n")
	d, ok := findDiag(cfg, retiredGooseDiag)
	if !ok {
		t.Fatalf("no %s diagnostic; got %+v", retiredGooseDiag, cfg.Diagnostics)
	}
	if !strings.Contains(d.Message, "0160") {
		t.Fatalf("message should cite MADR 0160: %q", d.Message)
	}
}

func TestRetiredGooseDisabledBlockWarns(t *testing.T) {
	cfg := loadRetiredGoose(t, "providers:\n  goose:\n    enabled: false\n")
	if _, ok := findDiag(cfg, retiredGooseDiag); !ok {
		t.Fatalf("no %s diagnostic; got %+v", retiredGooseDiag, cfg.Diagnostics)
	}
}

func TestRetiredGooseEmptyMapWarns(t *testing.T) {
	cfg := loadRetiredGoose(t, "providers:\n  goose: {}\n")
	if _, ok := findDiag(cfg, retiredGooseDiag); !ok {
		t.Fatalf("no %s diagnostic; got %+v", retiredGooseDiag, cfg.Diagnostics)
	}
}

// A bare `goose:` is null to viper: inert and undetectable (MADR 0160 F17).
func TestRetiredGooseNullKeyIsSilent(t *testing.T) {
	cfg := loadRetiredGoose(t, "providers:\n  goose:\n")
	if d, ok := findDiag(cfg, retiredGooseDiag); ok {
		t.Fatalf("unexpected diagnostic for a null key: %+v", d)
	}
}

func TestRetiredGooseEnvWarns(t *testing.T) {
	t.Setenv("MCREMOTE_PROVIDERS_GOOSE_ENABLED", "true")
	cfg := loadRetiredGoose(t, "providers:\n  grok:\n    enabled: true\n")
	d, ok := findDiag(cfg, retiredGooseDiag)
	if !ok {
		t.Fatalf("no %s diagnostic; got %+v", retiredGooseDiag, cfg.Diagnostics)
	}
	if !strings.Contains(d.Message, "MCREMOTE_PROVIDERS_GOOSE_ENABLED") {
		t.Fatalf("message should name the variable: %q", d.Message)
	}
}

func TestRetiredGooseAbsentIsSilent(t *testing.T) {
	cfg := loadRetiredGoose(t, "providers:\n  grok:\n    enabled: true\n")
	if d, ok := findDiag(cfg, retiredGooseDiag); ok {
		t.Fatalf("unexpected diagnostic with no goose setting: %+v", d)
	}
}

// Regression for MADR 0160 F16: setup-service seeded this exact block into
// every config it created. It is pasted, not read, because P1 removed it from
// the template.
func TestRetiredGooseSeededTemplateLoads(t *testing.T) {
	cfg := loadRetiredGoose(t, `providers:
  grok:
    enabled: true
  goose:
    enabled: true
    bin: "goose"
    always_approve: false
    default_cwd: ""
    model: ""
    permission_timeout_seconds: 120
    prewarm: false
    turn_stall_notice_seconds: 120
    stream_coalesce_ms: 80
    auth_method_id: ""
    keyring_disabled: true
    with_builtins: []
    mcp_servers: []
`)
	if _, ok := findDiag(cfg, retiredGooseDiag); !ok {
		t.Fatalf("no %s diagnostic; got %+v", retiredGooseDiag, cfg.Diagnostics)
	}
	if !cfg.Providers.Grok.Enabled {
		t.Fatal("a leftover goose block must not disturb the other providers")
	}
}

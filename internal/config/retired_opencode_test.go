package config_test

import (
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
)

// The 1.x dialect's keys are ignored with a warning, whatever their value
// (0214 PLAN P7 Deviation 2). Every case loads an explicit file under
// t.TempDir(), and diagnostics are found by code, as the goose cases are.

const retiredOpencodeDiag = "retired_opencode_1x_keys"

func loadRetiredOpencode(t *testing.T, body string) config.Config {
	t.Helper()
	cfg, err := config.Load(config.LoadOptions{ConfigFile: writeConfig(t, body)})
	if err != nil {
		t.Fatalf("Load must not fail on a leftover 1.x opencode key (0214 PLAN P7 Deviation 2): %v", err)
	}
	return cfg
}

func TestRetiredOpencodeKeyWarnsWhateverItsValue(t *testing.T) {
	for _, line := range []string{
		"pure: true", "pure: false",
		"session_tree: true", "session_tree: false",
		"stream_coalesce_ms: 80", "stream_coalesce_ms: 0", "stream_coalesce_ms: 200",
	} {
		t.Run(line, func(t *testing.T) {
			cfg := loadRetiredOpencode(t, "providers:\n  opencode:\n    "+line+"\n")
			d, ok := findDiag(cfg, retiredOpencodeDiag)
			if !ok {
				t.Fatalf("no %s diagnostic; got %+v", retiredOpencodeDiag, cfg.Diagnostics)
			}
			key, _, _ := strings.Cut(line, ":")
			if !strings.Contains(d.Message, "providers.opencode."+key) || !strings.Contains(d.Message, "0214") {
				t.Fatalf("message should name the key and 0214: %q", d.Message)
			}
		})
	}
}

func TestRetiredOpencodeEnvWarns(t *testing.T) {
	t.Setenv("MCREMOTE_PROVIDERS_OPENCODE_SESSION_TREE", "false")
	cfg := loadRetiredOpencode(t, "providers:\n  opencode:\n    enabled: true\n")
	d, ok := findDiag(cfg, retiredOpencodeDiag)
	if !ok {
		t.Fatalf("no %s diagnostic; got %+v", retiredOpencodeDiag, cfg.Diagnostics)
	}
	if !strings.Contains(d.Message, "MCREMOTE_PROVIDERS_OPENCODE_SESSION_TREE") {
		t.Fatalf("message should name the variable: %q", d.Message)
	}
}

func TestRetiredOpencodeAbsentIsSilent(t *testing.T) {
	cfg := loadRetiredOpencode(t, "providers:\n  opencode:\n    enabled: true\n    allow_remote_shell: true\n")
	if d, ok := findDiag(cfg, retiredOpencodeDiag); ok {
		t.Fatalf("unexpected diagnostic with no 1.x key: %+v", d)
	}
}

// Regression for 0214 PLAN P7 Deviation 2: setup-service seeded this exact
// block into every config it created before P7. It is pasted, not read,
// because P7 removed the keys from the template.
func TestRetiredOpencodeSeededTemplateLoads(t *testing.T) {
	cfg := loadRetiredOpencode(t, `providers:
  opencode:
    enabled: true
    bin: "opencode"
    always_approve: false
    default_cwd: ""
    model: ""
    permission_timeout_seconds: 120
    prewarm: false
    turn_stall_notice_seconds: 120
    stream_coalesce_ms: 80
    session_tree: true
    pure: false
    allow_remote_share: false
    allow_remote_shell: false
`)
	d, ok := findDiag(cfg, retiredOpencodeDiag)
	if !ok {
		t.Fatalf("no %s diagnostic; got %+v", retiredOpencodeDiag, cfg.Diagnostics)
	}
	for _, key := range []string{"pure", "session_tree", "stream_coalesce_ms"} {
		if !strings.Contains(d.Message, "providers.opencode."+key) {
			t.Errorf("message should name %s: %q", key, d.Message)
		}
	}
	if !cfg.Providers.Opencode.Enabled || cfg.Providers.Opencode.Bin != "opencode" {
		t.Fatalf("the shared ACP keys must still load: %+v", cfg.Providers.Opencode)
	}
}

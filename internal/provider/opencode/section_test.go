package opencode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
)

// clearMcremoteEnv unsets every MCREMOTE_* variable for the rest of t, so the
// host's own settings take no part. Call it before the test's own t.Setenv.
func clearMcremoteEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "MCREMOTE_") {
			t.Setenv(name, "")
			_ = os.Unsetenv(name)
		}
	}
}

// loadSection loads body as the config file with opencode's section
// registered.
func loadSection(t *testing.T, body string) (*Section, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.LoadOptions{ConfigFile: path,
		Providers: []config.ProviderSectionSpec{Descriptor().SectionSpec()}})
	if err != nil {
		return nil, err
	}
	return cfg.Providers.Sections["opencode"].(*Section), nil
}

func TestSectionDefaults(t *testing.T) {
	s := defaultSection()
	if !s.Enabled || s.Bin != "opencode" || s.PermissionTimeoutSeconds != 120 || s.TurnStallNoticeSeconds != 120 {
		t.Errorf("defaults = %+v", s)
	}
	if s.Prewarm {
		t.Error("prewarm should default false (MADR 0089 D5)")
	}
	// The shell runs a command on this host, so its default must be off
	// (MADR 0112 A9).
	if s.AllowRemoteShell {
		t.Error("allow_remote_shell should default false")
	}
}

// TestSectionLoadsThroughConfig: providers.opencode decodes from its defaults,
// the file and its variables, including the timeout keys that once had no
// viper default and so ignored their variables.
func TestSectionLoadsThroughConfig(t *testing.T) {
	clearMcremoteEnv(t)
	t.Setenv("MCREMOTE_PROVIDERS_OPENCODE_MODEL", "anthropic/claude-sonnet-4-5")
	t.Setenv("MCREMOTE_PROVIDERS_OPENCODE_PERMISSION_TIMEOUT_SECONDS", "60")
	s, err := loadSection(t, "providers:\n  opencode:\n    enabled: true\n    default_cwd: work\n")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Enabled || s.Model != "anthropic/claude-sonnet-4-5" || s.PermissionTimeoutSeconds != 60 {
		t.Errorf("file and variables not applied: %+v", s)
	}
	if s.Bin != "opencode" || s.Prewarm {
		t.Errorf("untouched defaults lost: %+v", s)
	}
	if !filepath.IsAbs(s.DefaultCWD) || filepath.Base(s.DefaultCWD) != "work" {
		t.Errorf("default_cwd = %q, want it resolved against the config file", s.DefaultCWD)
	}
}

// TestSectionRefusesTheRetiredKeys is 0208 MADR D6 as amended on 2026-11-04:
// the 1.x dialect's keys, its share policy and the pre-0019 transport were
// ignored with a warning, or refused by a guard of their own. Now none is a
// key, so each fails the load and is named, in the file or in a variable.
func TestSectionRefusesTheRetiredKeys(t *testing.T) {
	for _, key := range []string{"pure", "session_tree", "stream_coalesce_ms", "allow_remote_share", "transport"} {
		t.Run(key, func(t *testing.T) {
			clearMcremoteEnv(t)
			_, err := loadSection(t, "providers:\n  opencode:\n    "+key+": true\n")
			if err == nil || !strings.Contains(err.Error(), "providers.opencode has no key "+key) {
				t.Fatalf("Load error = %v, want providers.opencode's %s refused", err, key)
			}
		})
	}
	for _, name := range []string{"MCREMOTE_PROVIDERS_OPENCODE_TRANSPORT", "MCREMOTE_PROVIDERS_OPENCODE_PURE"} {
		t.Run(name, func(t *testing.T) {
			clearMcremoteEnv(t)
			t.Setenv(name, "acp")
			_, err := loadSection(t, "")
			if err == nil || !strings.Contains(err.Error(), name+" names no providers.opencode key") {
				t.Fatalf("Load error = %v, want %s refused", err, name)
			}
		})
	}
}

// TestRemoteShellPolicy: a fresh load leaves the shell off, the file can turn
// it on, and the environment wins over the file, including turning it back off
// (MADR 0112 A9).
func TestRemoteShellPolicy(t *testing.T) {
	clearMcremoteEnv(t)
	s, err := loadSection(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if s.AllowRemoteShell {
		t.Fatal("a fresh load enabled the remote shell")
	}
	const on = "providers:\n  opencode:\n    allow_remote_shell: true\n"
	if s, err = loadSection(t, on); err != nil || !s.AllowRemoteShell {
		t.Fatalf("YAML did not enable the shell: %+v, %v", s, err)
	}
	t.Setenv("MCREMOTE_PROVIDERS_OPENCODE_ALLOW_REMOTE_SHELL", "false")
	if s, err = loadSection(t, on); err != nil || s.AllowRemoteShell {
		t.Fatalf("the environment did not override the file: %+v, %v", s, err)
	}
}

func TestSectionValidate(t *testing.T) {
	for name, mod := range map[string]func(*Section){
		"providers.opencode.turn_stall_notice_seconds":  func(s *Section) { s.TurnStallNoticeSeconds = -5 },
		"providers.opencode.permission_timeout_seconds": func(s *Section) { s.PermissionTimeoutSeconds = -1 },
		"providers.opencode.mcp_servers[0].url": func(s *Section) {
			s.MCPServers = []config.MCPServerConfig{{Name: "x", Transport: "sse"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := defaultSection()
			mod(s)
			if err := s.Validate(); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("Validate = %v, want an error naming %s", err, name)
			}
		})
	}
	if err := defaultSection().Validate(); err != nil {
		t.Fatalf("defaults rejected: %v", err)
	}
}

// TestSectionReachesTheProvider proves the operator's shell decision reaches
// the constructed provider (MADR 0112 A9), and the shared ACP keys with it
// (0214 PLAN P7). Without this the flag would be settable, documented and
// tested at the config layer while silently never arriving.
func TestSectionReachesTheProvider(t *testing.T) {
	for _, shell := range []bool{false, true} {
		s := defaultSection()
		s.AllowRemoteShell = shell
		s.Bin = "/opt/opencode/bin/opencode"
		s.Prewarm = true
		got := s.providerConfig()
		if got.AllowRemoteShell != shell {
			t.Fatalf("shell=%v reached the provider as %v", shell, got.AllowRemoteShell)
		}
		if got.Bin != s.Bin || !got.Prewarm {
			t.Fatalf("the shared ACP keys did not reach the provider: %+v", got)
		}
	}
}

// TestDescriptorCarriesTheVersionFloor: doctor asks an engine its version
// only through a descriptor's floor.
func TestDescriptorCarriesTheVersionFloor(t *testing.T) {
	f := Descriptor().Floor
	if f == nil || f.Min != MinVersion || f.UpgradeURL != UpgradeURL {
		t.Fatalf("floor = %+v, want %s and %s", f, MinVersion, UpgradeURL)
	}
}

package grok

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
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

// loadSection loads body as the config file with grok's section registered.
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
	return cfg.Providers.Sections["grok"].(*Section), nil
}

// TestSectionDefaults pins providers.grok's defaults.
func TestSectionDefaults(t *testing.T) {
	s := defaultSection()
	// grok's permission mode is pinned rather than inherited: with it empty the
	// daemon cannot know the session's approval posture (grok resolves it from
	// its own config, project config, or fleet-wide remote config), yet it
	// advertises modes and a `dangerous` flag as if it did (MADR 0050 D3).
	if s.PermissionMode != "default" {
		t.Errorf("permission_mode default = %q, want default", s.PermissionMode)
	}
	if s.Prewarm {
		t.Error("prewarm should default false (MADR 0089 D5)")
	}
	if !s.Enabled || s.Bin != "grok" || s.StreamCoalesceMs != 80 || s.PermissionTimeoutSeconds != 120 ||
		s.TurnStallNoticeSeconds != 120 {
		t.Errorf("defaults = %+v", s)
	}
}

// TestSectionLoadsThroughConfig is 0208 PLAN P8: providers.grok decodes from
// its defaults, the file and its variables.
func TestSectionLoadsThroughConfig(t *testing.T) {
	clearMcremoteEnv(t)
	t.Setenv("MCREMOTE_PROVIDERS_GROK_PERMISSION_TIMEOUT_SECONDS", "60")
	s, err := loadSection(t, `
providers:
  grok:
    permission_mode: acceptEdits
    no_subagents: true
    disable_web_search: true
    sandbox: workspace
`)
	if err != nil {
		t.Fatal(err)
	}
	if s.PermissionMode != "acceptEdits" || !s.NoSubagents || !s.DisableWebSearch || s.Sandbox != "workspace" {
		t.Errorf("file keys not applied: %+v", s)
	}
	if s.PermissionTimeoutSeconds != 60 {
		t.Errorf("permission_timeout_seconds = %d, want 60 from the variable", s.PermissionTimeoutSeconds)
	}
	if s.StreamCoalesceMs != 80 || s.Bin != "grok" {
		t.Errorf("defaults lost: stream_coalesce_ms %d, bin %q", s.StreamCoalesceMs, s.Bin)
	}
}

// TestSectionRefusesTheRemovedFilterKeys is 0208 PLAN P8: the four tool and
// rule filters were measured with no effect over ACP and removed, so a config
// still carrying one fails the load and names it.
func TestSectionRefusesTheRemovedFilterKeys(t *testing.T) {
	for _, key := range []string{"allowed_tools", "disallowed_tools", "allow_rules", "deny_rules"} {
		t.Run(key, func(t *testing.T) {
			clearMcremoteEnv(t)
			_, err := loadSection(t, "providers:\n  grok:\n    "+key+": [Write]\n")
			if err == nil || !strings.Contains(err.Error(), "providers.grok has no key "+key) {
				t.Fatalf("Load error = %v, want providers.grok's %s refused", err, key)
			}
		})
	}
}

func TestSectionValidate(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*Section)
		want string
	}{
		{"negative stall", func(s *Section) { s.TurnStallNoticeSeconds = -1 },
			"providers.grok.turn_stall_notice_seconds"},
		{"negative permission timeout", func(s *Section) { s.PermissionTimeoutSeconds = -1 },
			"providers.grok.permission_timeout_seconds"},
		{"stream coalesce too long", func(s *Section) { s.StreamCoalesceMs = config.MaxStreamCoalesceMs + 1 },
			"providers.grok.stream_coalesce_ms"},
		{"whitespace reasoning effort", func(s *Section) { s.ReasoningEffort = "  " },
			"providers.grok.reasoning_effort"},
		{"unknown permission mode", func(s *Section) { s.PermissionMode = "Default" },
			"providers.grok.permission_mode"},
		{"an MCP server without a url", func(s *Section) {
			s.MCPServers = []config.MCPServerConfig{{Name: "x", Transport: "sse"}}
		}, "providers.grok.mcp_servers[0].url"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := defaultSection()
			tc.mod(s)
			if err := s.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate = %v, want an error naming %s", err, tc.want)
			}
		})
	}
	for _, ok := range []string{"", "default", "acceptEdits", "auto", "dontAsk", "bypassPermissions", "plan"} {
		s := defaultSection()
		s.PermissionMode = ok
		s.ReasoningEffort = "high"
		if err := s.Validate(); err != nil {
			t.Errorf("permission_mode %q must be accepted: %v", ok, err)
		}
	}
}

// TestSectionReachesTheProvider: every key of the section is copied into the
// provider's config. A key that is settable and documented but never copied
// would be a setting in name only (MADR 0112 A9).
func TestSectionReachesTheProvider(t *testing.T) {
	s := defaultSection()
	s.Bin = "/opt/grok"
	s.Model = "grok-4.6"
	s.AuthMethodID = "xai.api_key"
	s.ReasoningEffort = "high"
	s.PermissionMode = "plan"
	s.Sandbox = "workspace"
	s.NoSubagents = true
	s.DisableWebSearch = true
	s.StreamCoalesceMs = 0
	c := s.providerConfig("9.9.9")
	if c.Bin != "/opt/grok" || c.Model != "grok-4.6" || c.AuthMethodID != "xai.api_key" ||
		c.ReasoningEffort != "high" || c.PermissionMode != "plan" || c.Sandbox != "workspace" ||
		!c.NoSubagents || !c.DisableWebSearch || c.ClientVersion != "9.9.9" ||
		c.PermissionTimeout != 120*time.Second {
		t.Errorf("provider config = %+v", c)
	}
	// An explicit 0 is "one event per token", not the transport default.
	if c.StreamCoalesce == nil || *c.StreamCoalesce != 0 {
		t.Errorf("stream coalesce = %v, want an explicit 0", c.StreamCoalesce)
	}
}

// TestDescriptorCoordinatesWhenGivenACoordinator: with a credential
// coordinator the descriptor builds the coordinated provider, which owns the
// transactional device-auth contract (MADR 0074 D21); without one, the plain
// provider.
func TestDescriptorCoordinatesWhenGivenACoordinator(t *testing.T) {
	d := Descriptor()
	plain := d.New(provider.Build{Section: defaultSection()})
	if _, ok := plain.(*Provider); !ok {
		t.Fatalf("without a coordinator: %T, want *Provider", plain)
	}
	testexec.SkipIfNoPOSIXModes(t)
	coord, err := providerauth.NewCoordinator(t.TempDir(), d.Credentials(defaultSection()),
		providerauth.CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	coordinated := d.New(provider.Build{Section: defaultSection(), Credentials: coord, Busy: func() int { return 0 }})
	if _, ok := coordinated.(*CoordinatedProvider); !ok {
		t.Fatalf("with a coordinator: %T, want *CoordinatedProvider", coordinated)
	}
}

// TestSectionDefaultCWDIsResolvedAgainstTheConfigFile: a relative default_cwd
// in a described agent's section is resolved against the config file's
// directory, as it was when config held the section.
func TestSectionDefaultCWDIsResolvedAgainstTheConfigFile(t *testing.T) {
	clearMcremoteEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("providers:\n  grok:\n    default_cwd: work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.LoadOptions{ConfigFile: path,
		Providers: []config.ProviderSectionSpec{Descriptor().SectionSpec()}})
	if err != nil {
		t.Fatal(err)
	}
	// The file sits in a temp dir, which may be reached through a symlink.
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Providers.Sections["grok"].(*Section).DefaultCWD
	if got != filepath.Join(dir, "work") && got != filepath.Join(want, "work") {
		t.Fatalf("default_cwd = %q, want it under %q", got, dir)
	}
}

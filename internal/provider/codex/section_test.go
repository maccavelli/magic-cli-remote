package codex

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

// loadSection loads body as the config file with codex's section registered.
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
	return cfg.Providers.Sections["codex"].(*Section), nil
}

func TestSectionDefaults(t *testing.T) {
	s := defaultSection()
	if !s.Enabled || s.Bin != "codex" || s.PermissionTimeoutSeconds != 900 || s.TurnStallNoticeSeconds != 120 ||
		s.StreamCoalesceMs != 80 {
		t.Errorf("defaults = %+v", s)
	}
	if s.Transport != "stdio" || s.ReconnectAttempts != 3 {
		t.Errorf("transport defaults = %q/%d, want stdio/3", s.Transport, s.ReconnectAttempts)
	}
	// Off (MADR 0089 D5), and full access is opt-in (MADR 0044 D5).
	if s.Prewarm || s.AllowFullAccess {
		t.Errorf("prewarm %v, allow_full_access %v; want both false", s.Prewarm, s.AllowFullAccess)
	}
}

// TestSectionLoadsThroughConfig: providers.codex decodes from its defaults,
// the file and its variables. sandbox_broken_policy had no viper default once,
// so its variable was ignored (MADR 0201 D23).
func TestSectionLoadsThroughConfig(t *testing.T) {
	clearMcremoteEnv(t)
	t.Setenv("MCREMOTE_PROVIDERS_CODEX_SANDBOX_BROKEN_POLICY", "refuse")
	t.Setenv("MCREMOTE_PROVIDERS_CODEX_STANDALONE_PROCESS_ENV_ALLOWLIST", "TERM,LANG")
	s, err := loadSection(t, "providers:\n  codex:\n    approval_policy: on-request\n    reconnect_attempts: 1\n")
	if err != nil {
		t.Fatal(err)
	}
	if s.SandboxBrokenPolicy != "refuse" || s.ApprovalPolicy != "on-request" || s.ReconnectAttempts != 1 {
		t.Errorf("file and variables not applied: %+v", s)
	}
	if len(s.StandaloneProcessEnvAllowlist) != 2 || s.StandaloneProcessEnvAllowlist[1] != "LANG" {
		t.Errorf("standalone_process_env_allowlist = %v, want [TERM LANG] from the variable", s.StandaloneProcessEnvAllowlist)
	}
	if s.Bin != "codex" || s.PermissionTimeoutSeconds != 900 {
		t.Errorf("defaults lost: %+v", s)
	}
}

// TestSectionEnvironmentsAreConfigFileOnly: the execution catalog is a list
// of tables, so a variable for it fails the load and is named.
func TestSectionEnvironmentsAreConfigFileOnly(t *testing.T) {
	clearMcremoteEnv(t)
	t.Setenv("MCREMOTE_PROVIDERS_CODEX_ENVIRONMENTS", "x")
	_, err := loadSection(t, "")
	if err == nil || !strings.Contains(err.Error(), "MCREMOTE_PROVIDERS_CODEX_ENVIRONMENTS sets providers.codex.environments, which is config-file only") {
		t.Fatalf("Load error = %v, want the variable refused", err)
	}
}

func TestSectionTransportValidation(t *testing.T) {
	for name, mod := range map[string]func(*Section){
		"wildcard": func(s *Section) {
			s.Transport = "ws"
			s.ListenAddress = "0.0.0.0:1234"
		},
		"inline-auth-on-stdio": func(s *Section) { s.WSAuthMode = "capability_token" },
		"too-many-reconnects":  func(s *Section) { s.ReconnectAttempts = 4 },
		"unknown-transport":    func(s *Section) { s.Transport = "pipe" },
	} {
		t.Run(name, func(t *testing.T) {
			s := defaultSection()
			mod(s)
			if err := s.Validate(); err == nil {
				t.Fatal("invalid Codex transport configuration accepted")
			}
		})
	}
	s := defaultSection()
	s.Transport = "ws"
	s.ListenAddress = "127.0.0.1:4500"
	s.WSAuthMode = "signed_bearer"
	if err := s.Validate(); err != nil {
		t.Fatalf("a loopback ws transport was rejected: %v", err)
	}
}

func TestSectionValidate(t *testing.T) {
	for want, mod := range map[string]func(*Section){
		"providers.codex.turn_stall_notice_seconds":  func(s *Section) { s.TurnStallNoticeSeconds = -1 },
		"providers.codex.permission_timeout_seconds": func(s *Section) { s.PermissionTimeoutSeconds = -1 },
		"providers.codex.stream_coalesce_ms":         func(s *Section) { s.StreamCoalesceMs = config.MaxStreamCoalesceMs + 1 },
		"providers.codex.approval_policy":            func(s *Section) { s.ApprovalPolicy = "always" },
		"providers.codex.sandbox_mode":               func(s *Section) { s.SandboxMode = "none" },
		"providers.codex.sandbox_broken_policy":      func(s *Section) { s.SandboxBrokenPolicy = "ignore" },
	} {
		t.Run(want, func(t *testing.T) {
			s := defaultSection()
			mod(s)
			if err := s.Validate(); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Validate = %v, want an error naming %s", err, want)
			}
		})
	}
	if err := defaultSection().Validate(); err != nil {
		t.Fatalf("defaults rejected: %v", err)
	}
}

// TestSectionReachesTheProvider: every key of the section is copied into the
// provider's config, with the daemon's runtime directory.
func TestSectionReachesTheProvider(t *testing.T) {
	s := defaultSection()
	s.Bin = "/opt/codex"
	s.ApprovalPolicy = "never"
	s.SandboxMode = "workspace-write"
	s.AllowFullAccess = true
	s.SandboxBrokenPolicy = "refuse"
	s.Transport = "ws"
	s.ListenAddress = "127.0.0.1:4500"
	s.WSAuthMode = "signed_bearer"
	s.ReconnectAttempts = 2
	s.StandaloneProcessesEnabled = true
	s.StandaloneProcessEnvAllowlist = []string{"TERM"}
	s.Environments = []EnvironmentSection{{ID: "e", ExecServerURL: "wss://x/y", ConnectTimeoutMS: 250,
		RuntimeWorkspaceRoots: []string{"/w"}}}
	c := s.providerConfig("/run/mcremote")
	if c.Bin != "/opt/codex" || c.ApprovalPolicy != "never" || c.SandboxMode != "workspace-write" || !c.AllowFullAccess ||
		c.SandboxBrokenPolicy != "refuse" || c.Transport != TransportMode("ws") || c.ListenAddress != "127.0.0.1:4500" ||
		c.WSAuthMode != WSAuthMode("signed_bearer") || c.ReconnectAttempts != 2 || !c.ReconnectAttemptsConfigured ||
		c.RuntimeDir != "/run/mcremote" || !c.StandaloneProcessesEnabled || len(c.StandaloneProcessEnvAllowlist) != 1 ||
		c.PermissionTimeout != 900*time.Second {
		t.Errorf("provider config = %+v", c)
	}
	if len(c.Environments) != 1 || c.Environments[0].ConnectTimeout != 250*time.Millisecond {
		t.Errorf("environments = %+v", c.Environments)
	}
}

// TestDescriptorBuildsTheAdapterAndTheCoordinatedProvider: the credential
// adapter carries the configured binary (MADR 0134), and a coordinator makes
// the coordinated provider (MADR 0074 D21).
func TestDescriptorBuildsTheAdapterAndTheCoordinatedProvider(t *testing.T) {
	d := Descriptor()
	s := defaultSection()
	s.Bin = "/opt/codex-under-test"
	ad, ok := d.Credentials(s).(*CredentialAdapter)
	if !ok || ad.bin != "/opt/codex-under-test" {
		t.Fatalf("adapter = %+v, want codex's with the configured binary", ad)
	}
	plain := d.New(provider.Build{Section: s}).(*Provider)
	if plain.coord != nil {
		t.Fatal("built without a coordinator, the provider has one")
	}
	testexec.SkipIfNoPOSIXModes(t)
	coord, err := providerauth.NewCoordinator(t.TempDir(), ad, providerauth.CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	coordinated := d.New(provider.Build{Section: s, Credentials: coord, Busy: func() int { return 0 }}).(*Provider)
	if coordinated.coord != coord {
		t.Fatal("given a coordinator, the provider is not the coordinated one")
	}
}

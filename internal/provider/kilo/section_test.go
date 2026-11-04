package kilo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// Kilo defaults per MADR 0075: enabled since the acceptance flip (2026-08-10),
// session_tree off until the child-SSE fixtures land (PD2), prewarm off (MADR
// 0089 D5), and no model, because the engine default is auth-state-dependent.
func TestSectionDefaults(t *testing.T) {
	s := defaultSection()
	if !s.Enabled || s.Bin != "kilo" {
		t.Fatalf("enabled %v, bin %q; want true, kilo", s.Enabled, s.Bin)
	}
	if s.SessionTree || s.Prewarm || s.Pure {
		t.Fatalf("session_tree %v, prewarm %v, pure %v; want all false", s.SessionTree, s.Prewarm, s.Pure)
	}
	if s.PermissionTimeoutSeconds != 120 || s.TurnStallNoticeSeconds != 120 || s.StreamCoalesceMs != 80 {
		t.Fatalf("timeouts %d/%d, stream_coalesce_ms %d; want 120/120 and 80",
			s.PermissionTimeoutSeconds, s.TurnStallNoticeSeconds, s.StreamCoalesceMs)
	}
	if s.Model != "" {
		t.Fatalf("model = %q, want empty", s.Model)
	}
}

// TestSectionLoadsThroughConfig: every key of providers.kilo resolves from
// its variable, and the defaults survive.
func TestSectionLoadsThroughConfig(t *testing.T) {
	clearMcremoteEnv(t)
	t.Setenv("MCREMOTE_PROVIDERS_KILO_MODEL", "kilo/~anthropic/claude-sonnet")
	t.Setenv("MCREMOTE_PROVIDERS_KILO_PERMISSION_TIMEOUT_SECONDS", "60")
	t.Setenv("MCREMOTE_PROVIDERS_KILO_SESSION_TREE", "true")
	t.Setenv("MCREMOTE_PROVIDERS_KILO_PURE", "true")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.LoadOptions{ConfigFile: path,
		Providers: []config.ProviderSectionSpec{Descriptor().SectionSpec()}})
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Providers.Sections["kilo"].(*Section)
	if s.Model != "kilo/~anthropic/claude-sonnet" || s.PermissionTimeoutSeconds != 60 || !s.SessionTree || !s.Pure {
		t.Fatalf("variables not applied: %+v", s)
	}
	if s.Bin != "kilo" || !s.Enabled {
		t.Fatalf("defaults lost: %+v", s)
	}
}

func TestSectionValidate(t *testing.T) {
	for want, mod := range map[string]func(*Section){
		"providers.kilo.permission_timeout_seconds": func(s *Section) { s.PermissionTimeoutSeconds = -1 },
		"providers.kilo.turn_stall_notice_seconds":  func(s *Section) { s.TurnStallNoticeSeconds = -5 },
		"providers.kilo.stream_coalesce_ms":         func(s *Section) { s.StreamCoalesceMs = 5000 },
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

// TestSectionReachesTheProvider: every key is copied into the provider's
// config, and the two pointers keep an explicit false and an explicit 0.
func TestSectionReachesTheProvider(t *testing.T) {
	s := defaultSection()
	s.Bin = "/opt/kilo"
	s.Model = "kilo/~anthropic/x"
	s.AlwaysApprove = true
	s.Pure = true
	s.StreamCoalesceMs = 0
	c := s.providerConfig()
	if c.Bin != "/opt/kilo" || c.Model != "kilo/~anthropic/x" || !c.AlwaysApprove || !c.Pure ||
		c.PermissionTimeout != 120*time.Second || c.TurnStallNotice != 120*time.Second {
		t.Fatalf("provider config = %+v", c)
	}
	if c.SessionTree == nil || *c.SessionTree {
		t.Fatalf("session tree = %v, want an explicit false", c.SessionTree)
	}
	if c.StreamCoalesce == nil || *c.StreamCoalesce != 0 {
		t.Fatalf("stream coalesce = %v, want an explicit 0", c.StreamCoalesce)
	}
}

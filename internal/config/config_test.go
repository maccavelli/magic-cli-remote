package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/tailnet"
	"github.com/spf13/pflag"
)

func TestDefaults(t *testing.T) {
	d := config.Defaults()
	if d.Listen.Port != 7531 {
		t.Fatalf("port=%d", d.Listen.Port)
	}
	if d.Listen.Host != "127.0.0.1" {
		t.Fatalf("host=%s", d.Listen.Host)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	// Client-key enforcement ships default-on (D7 / ADR 0005).
	if !d.Auth.RequireClientKey {
		t.Fatal("auth.require_client_key should default to true")
	}
	// Signed receipts ship opt-in, off by default (MADR 0077 D4).
	if d.Receipts.Enabled {
		t.Fatal("receipts.enabled should default to false")
	}
	if len(d.Receipts.AllowPatterns) != 0 || len(d.Receipts.DenyPatterns) != 0 {
		t.Fatalf("receipts patterns should default to empty, got allow=%v deny=%v",
			d.Receipts.AllowPatterns, d.Receipts.DenyPatterns)
	}
	// Handoff attestation defaults on *when* receipts are enabled (MADR 0078 D6).
	if !d.Receipts.Handoffs {
		t.Fatal("receipts.handoffs should default to true")
	}
}

// receipts.* must bind from the environment, matching every other config key
// that reads through a SetDefault call — without one, the key is absent from
// viper's key set and AutomaticEnv silently ignores the env var (the same
// gap TestRoute53MaxRetriesEnvOverride guards for route53.max_retries).
func TestReceiptsEnvOverride(t *testing.T) {
	t.Setenv("MCREMOTE_RECEIPTS_ENABLED", "true")
	t.Setenv("MCREMOTE_RECEIPTS_ALLOW_PATTERNS", "*rm -rf*,*push --force*")
	cfg, err := config.Load(config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Receipts.Enabled {
		t.Fatal("MCREMOTE_RECEIPTS_ENABLED=true should enable receipts")
	}
	if len(cfg.Receipts.AllowPatterns) != 2 {
		t.Fatalf("receipts.allow_patterns env override not applied: %v", cfg.Receipts.AllowPatterns)
	}
}

func TestRequireClientKeyEnvOverride(t *testing.T) {
	t.Setenv("MCREMOTE_AUTH_REQUIRE_CLIENT_KEY", "false")
	cfg, err := config.Load(config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.RequireClientKey {
		t.Fatal("MCREMOTE_AUTH_REQUIRE_CLIENT_KEY=false should disable enforcement")
	}
}

// Provider env overrides must work for every provider key, including the
// timeout keys that previously had no viper default (AutomaticEnv only
// resolves known keys, so a missing default silently ignored the env var).
func TestProviderEnvOverrides(t *testing.T) {
	t.Setenv("MCREMOTE_PROVIDERS_OPENCODE_ENABLED", "true")
	t.Setenv("MCREMOTE_PROVIDERS_OPENCODE_MODEL", "anthropic/claude-sonnet-4-5")
	t.Setenv("MCREMOTE_PROVIDERS_OPENCODE_PERMISSION_TIMEOUT_SECONDS", "120")
	t.Setenv("MCREMOTE_PROVIDERS_GROK_PERMISSION_TIMEOUT_SECONDS", "60")
	cfg, err := config.Load(config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	oc := cfg.Providers.Opencode
	if !oc.Enabled || oc.Model != "anthropic/claude-sonnet-4-5" || oc.PermissionTimeoutSeconds != 120 {
		t.Fatalf("opencode env overrides not applied: %+v", oc)
	}
	if cfg.Providers.Grok.PermissionTimeoutSeconds != 60 {
		t.Fatalf("grok timeout env override not applied: %d",
			cfg.Providers.Grok.PermissionTimeoutSeconds)
	}
	if oc.Bin != "opencode" {
		t.Fatalf("untouched opencode defaults should survive: %+v", oc)
	}
}

// route53.max_retries must bind from the environment. Without a viper default
// the key is absent from the key set and AutomaticEnv silently ignores the env
// var.
func TestRoute53MaxRetriesEnvOverride(t *testing.T) {
	t.Setenv("MCREMOTE_TLS_LETSENCRYPT_ROUTE53_MAX_RETRIES", "7")
	cfg, err := config.Load(config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.TLS.LetsEncrypt.Route53.MaxRetries; got != 7 {
		t.Fatalf("route53.max_retries=%d want 7 (env override ignored)", got)
	}
}

// pair.advertise_host must load from the file, be overridable by env (both the
// canonical name and the legacy alias), and reject a scheme/path.
func TestPairAdvertiseHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("pair:\n  advertise_host: \"devbox.local:7531\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(config.LoadOptions{ConfigFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Pair.AdvertiseHost != "devbox.local:7531" {
		t.Fatalf("from file: advertise_host=%q", cfg.Pair.AdvertiseHost)
	}

	// Canonical env overrides the file.
	t.Setenv("MCREMOTE_PAIR_ADVERTISE_HOST", "100.64.0.1:7531")
	cfg, err = config.Load(config.LoadOptions{ConfigFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Pair.AdvertiseHost != "100.64.0.1:7531" {
		t.Fatalf("canonical env: advertise_host=%q", cfg.Pair.AdvertiseHost)
	}

	// Legacy alias binds to the same key.
	t.Setenv("MCREMOTE_PAIR_ADVERTISE_HOST", "")
	t.Setenv("MCREMOTE_PAIR_HOST", "10.0.0.5:7531")
	cfg, err = config.Load(config.LoadOptions{ConfigFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Pair.AdvertiseHost != "10.0.0.5:7531" {
		t.Fatalf("legacy alias: advertise_host=%q", cfg.Pair.AdvertiseHost)
	}

	// A scheme is rejected: it would be baked into the pair URI verbatim.
	c := config.Defaults()
	c.Pair.AdvertiseHost = "wss://devbox.local:7531"
	if err := c.Validate(); err == nil {
		t.Fatal("expected validation error for advertise_host with a scheme")
	}
}

func TestLoadFileAndEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
listen:
  host: "0.0.0.0"
  port: 9000
log:
  level: debug
  format: json
auth:
  require_device_token: true
providers:
  fake:
    enabled: false
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MCREMOTE_LISTEN_PORT", "9001")

	cfg, err := config.Load(config.LoadOptions{ConfigFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen.Host != "0.0.0.0" {
		t.Fatalf("host=%s", cfg.Listen.Host)
	}
	// Env should override file for port.
	if cfg.Listen.Port != 9001 {
		t.Fatalf("port=%d want 9001", cfg.Listen.Port)
	}
	if cfg.Log.Format != "json" {
		t.Fatalf("format=%s", cfg.Log.Format)
	}
	if cfg.Providers.Fake.Enabled {
		t.Fatal("fake should be disabled by file")
	}
}

func TestTLSModeResolution(t *testing.T) {
	base := func() config.TLSConfig { return config.Defaults().TLS }

	t.Run("defaults to selfsigned", func(t *testing.T) {
		if got := base().ResolvedMode(); got != config.TLSModeSelfSigned {
			t.Fatalf("mode=%s", got)
		}
	})

	t.Run("letsencrypt when domain and email are set", func(t *testing.T) {
		tc := base()
		tc.LetsEncrypt.Domains = []string{"devbox.ts.lallygag.net"}
		tc.LetsEncrypt.Email = "ops@lallygag.net"
		if got := tc.ResolvedMode(); got != config.TLSModeLetsEncrypt {
			t.Fatalf("mode=%s", got)
		}
		if !tc.Active() || tc.Pinned() {
			t.Fatalf("active=%v pinned=%v", tc.Active(), tc.Pinned())
		}
		if tc.Scheme() != "wss" || tc.HTTPScheme() != "https" {
			t.Fatalf("scheme=%s/%s", tc.Scheme(), tc.HTTPScheme())
		}
	})

	t.Run("domain without email stays selfsigned", func(t *testing.T) {
		tc := base()
		tc.LetsEncrypt.Domains = []string{"devbox.ts.lallygag.net"}
		if got := tc.ResolvedMode(); got != config.TLSModeSelfSigned {
			t.Fatalf("mode=%s", got)
		}
		if !tc.Pinned() {
			t.Fatal("selfsigned must be pinned")
		}
	})

	t.Run("legacy enabled=false means off", func(t *testing.T) {
		tc := base()
		tc.LetsEncrypt.Domains = []string{"devbox.ts.lallygag.net"}
		tc.LetsEncrypt.Email = "ops@lallygag.net"
		tc.Enabled = false
		if got := tc.ResolvedMode(); got != config.TLSModeOff {
			t.Fatalf("mode=%s", got)
		}
		if tc.Active() || tc.Scheme() != "ws" {
			t.Fatalf("active=%v scheme=%s", tc.Active(), tc.Scheme())
		}
	})

	t.Run("explicit mode wins over auto-detection", func(t *testing.T) {
		tc := base()
		tc.Mode = config.TLSModeSelfSigned
		tc.LetsEncrypt.Domains = []string{"devbox.ts.lallygag.net"}
		tc.LetsEncrypt.Email = "ops@lallygag.net"
		if got := tc.ResolvedMode(); got != config.TLSModeSelfSigned {
			t.Fatalf("mode=%s", got)
		}
	})

	t.Run("WithEnabled toggles off and back", func(t *testing.T) {
		tc := base()
		tc.LetsEncrypt.Domains = []string{"devbox.ts.lallygag.net"}
		tc.LetsEncrypt.Email = "ops@lallygag.net"
		off := tc.WithEnabled(false)
		if off.Mode != config.TLSModeOff {
			t.Fatalf("mode=%s", off.Mode)
		}
		back := off.WithEnabled(true)
		if back.Mode != config.TLSModeLetsEncrypt {
			t.Fatalf("mode=%s", back.Mode)
		}
	})

	t.Run("Normalized syncs Enabled", func(t *testing.T) {
		tc := base()
		tc.Mode = config.TLSModeOff
		n := tc.Normalized()
		if n.Enabled {
			t.Fatal("enabled should follow mode off")
		}
	})
}

func TestLetsEncryptDirectory(t *testing.T) {
	le := config.LetsEncryptConfig{}
	if le.Directory() != "" {
		t.Fatalf("directory=%q want empty (CA default)", le.Directory())
	}
	le.Staging = true
	if le.Directory() != config.LetsEncryptStagingDirectory {
		t.Fatalf("directory=%q", le.Directory())
	}
	le.DirectoryURL = "https://acme.example.com/dir"
	if le.Directory() != "https://acme.example.com/dir" {
		t.Fatalf("explicit directory_url must win, got %q", le.Directory())
	}
}

func TestTLSValidate(t *testing.T) {
	withTLS := func(mut func(*config.TLSConfig)) config.Config {
		c := config.Defaults()
		mut(&c.TLS)
		return c
	}
	cases := []struct {
		name    string
		cfg     config.Config
		wantErr string
	}{
		{
			name:    "unknown mode",
			cfg:     withTLS(func(t *config.TLSConfig) { t.Mode = "acme" }),
			wantErr: "tls.mode must be",
		},
		{
			name: "letsencrypt without domains",
			cfg: withTLS(func(t *config.TLSConfig) {
				t.Mode = config.TLSModeLetsEncrypt
				t.LetsEncrypt.Email = "ops@lallygag.net"
			}),
			wantErr: "domains is empty",
		},
		{
			name: "letsencrypt without email",
			cfg: withTLS(func(t *config.TLSConfig) {
				t.Mode = config.TLSModeLetsEncrypt
				t.LetsEncrypt.Domains = []string{"devbox.ts.lallygag.net"}
			}),
			wantErr: "email is empty",
		},
		{
			name: "letsencrypt with an IP domain",
			cfg: withTLS(func(t *config.TLSConfig) {
				t.LetsEncrypt.Domains = []string{"100.64.0.1"}
				t.LetsEncrypt.Email = "ops@lallygag.net"
			}),
			wantErr: "is an IP address",
		},
		{
			name: "letsencrypt with a host:port domain",
			cfg: withTLS(func(t *config.TLSConfig) {
				t.LetsEncrypt.Domains = []string{"devbox.ts.lallygag.net:7531"}
				t.LetsEncrypt.Email = "ops@lallygag.net"
			}),
			wantErr: "bare hostname",
		},
		{
			name: "bad directory url",
			cfg: withTLS(func(t *config.TLSConfig) {
				t.LetsEncrypt.Domains = []string{"devbox.ts.lallygag.net"}
				t.LetsEncrypt.Email = "ops@lallygag.net"
				t.LetsEncrypt.DirectoryURL = "acme.example.com"
			}),
			wantErr: "http(s) URL",
		},
		{
			name: "enabled false contradicts explicit mode",
			cfg: withTLS(func(t *config.TLSConfig) {
				t.Enabled = false
				t.Mode = config.TLSModeSelfSigned
			}),
			wantErr: "tls.enabled is false but tls.mode",
		},
		{
			name:    "cert without key",
			cfg:     withTLS(func(t *config.TLSConfig) { t.CertFile = "/tmp/tls.crt" }),
			wantErr: "must be set together",
		},
		{
			name: "cert files with letsencrypt",
			cfg: withTLS(func(t *config.TLSConfig) {
				t.CertFile = "/tmp/tls.crt"
				t.KeyFile = "/tmp/tls.key"
				t.LetsEncrypt.Domains = []string{"devbox.ts.lallygag.net"}
				t.LetsEncrypt.Email = "ops@lallygag.net"
			}),
			wantErr: "only valid with tls.mode",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q missing %q", err, tc.wantErr)
			}
		})
	}
}

func TestTLSValidateAccepts(t *testing.T) {
	cfg := config.Defaults()
	cfg.TLS.Mode = config.TLSModeLetsEncrypt
	cfg.TLS.LetsEncrypt.Domains = []string{"devbox.ts.lallygag.net", "alt.ts.lallygag.net"}
	cfg.TLS.LetsEncrypt.Email = "ops@lallygag.net"
	cfg.TLS.LetsEncrypt.Staging = true
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := cfg.TLS.LetsEncrypt.PrimaryDomain(); got != "devbox.ts.lallygag.net" {
		t.Fatalf("primary=%q", got)
	}

	// Legacy config with only tls.enabled: false still validates.
	legacy := config.Defaults()
	legacy.TLS.Enabled = false
	if err := legacy.Validate(); err != nil {
		t.Fatal(err)
	}
}

// A YAML file carrying only the pre-mode tls.enabled key must keep working.
func TestLoadLegacyTLSEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("tls:\n  enabled: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.LoadOptions{ConfigFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLS.Mode != config.TLSModeOff || cfg.TLS.Enabled {
		t.Fatalf("mode=%s enabled=%v", cfg.TLS.Mode, cfg.TLS.Enabled)
	}
}

func TestLoadLetsEncryptFromFileAndEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
tls:
  letsencrypt:
    domains:
      - devbox.ts.lallygag.net
    email: ops@lallygag.net
    route53:
      hosted_zone_id: Z123456
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MCREMOTE_TLS_ACME_STAGING", "true")

	cfg, err := config.Load(config.LoadOptions{ConfigFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLS.Mode != config.TLSModeLetsEncrypt {
		t.Fatalf("mode=%s want letsencrypt (default when domain+email are set)", cfg.TLS.Mode)
	}
	if cfg.TLS.LetsEncrypt.Route53.HostedZoneID != "Z123456" {
		t.Fatalf("zone=%q", cfg.TLS.LetsEncrypt.Route53.HostedZoneID)
	}
	if cfg.TLS.LetsEncrypt.Directory() != config.LetsEncryptStagingDirectory {
		t.Fatalf("directory=%q want staging (from env)", cfg.TLS.LetsEncrypt.Directory())
	}
	if cfg.ACMECacheDir() != filepath.Join(cfg.DataDir, "acme") {
		t.Fatalf("cache dir=%q", cfg.ACMECacheDir())
	}
}

func TestInvalidPort(t *testing.T) {
	cfg := config.Defaults()
	cfg.Listen.Port = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error")
	}
}

func TestRelayValidateAdvertiseWithoutSecret(t *testing.T) {
	// url + host_id alone are valid so `mcremote pair` can put relay=/hid= on
	// the QR without the registration secret in the operator shell.
	cfg := config.Defaults()
	cfg.Relay.URL = "wss://relay.example.com:8443"
	cfg.Relay.HostID = "macos-laptop"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("url+host_id without secret should load: %v", err)
	}
	if !cfg.Relay.Enabled() {
		t.Fatal("expected Enabled")
	}
	if cfg.Relay.CanRegister() {
		t.Fatal("empty secret must not CanRegister")
	}

	cfg.Relay.Secret = "sixteen-chars-min"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("full relay config: %v", err)
	}
	if !cfg.Relay.CanRegister() {
		t.Fatal("expected CanRegister with secret")
	}

	// url without host_id rejected
	bad := config.Defaults()
	bad.Relay.URL = "wss://relay.example.com"
	if err := bad.Validate(); err == nil {
		t.Fatal("expected error for url without host_id")
	}

	// secret alone rejected
	bad = config.Defaults()
	bad.Relay.Secret = "sixteen-chars-min"
	if err := bad.Validate(); err == nil {
		t.Fatal("expected error for secret without url/host_id")
	}
}

func TestValidateRejectsNegativeStallSeconds(t *testing.T) {
	cfg := config.Defaults()
	cfg.Providers.Grok.TurnStallNoticeSeconds = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for negative grok stall")
	}
	cfg = config.Defaults()
	cfg.Providers.Opencode.TurnStallNoticeSeconds = -5
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for negative opencode stall")
	}
}

func TestDefaultsGrokPrewarm(t *testing.T) {
	if config.Defaults().Providers.Grok.Prewarm {
		t.Fatal("grok prewarm should default false (MADR 0089 D5)")
	}
}

func TestDefaultsAllPrewarmFalse(t *testing.T) {
	d := config.Defaults()
	for _, row := range []struct {
		id      string
		prewarm bool
	}{
		{"grok", d.Providers.Grok.Prewarm},
		{"goose", d.Providers.Goose.Prewarm},
		{"opencode", d.Providers.Opencode.Prewarm},
		{"codex", d.Providers.Codex.Prewarm},
		{"kilo", d.Providers.Kilo.Prewarm},
	} {
		if row.prewarm {
			t.Errorf("%s prewarm defaulted true; want false (MADR 0089 D5)", row.id)
		}
	}
}

// Kilo defaults per MADR 0075: enabled since the acceptance flip (2026-08-10),
// session_tree off until the child-SSE fixtures land (PD2), everything else
// mirrors OpenCode.
func TestDefaultsKilo(t *testing.T) {
	k := config.Defaults().Providers.Kilo
	if !k.Enabled {
		t.Fatal("kilo defaults on since the MADR 0075 acceptance flip (2026-08-10)")
	}
	if k.Bin != "kilo" {
		t.Fatalf("kilo bin = %q", k.Bin)
	}
	if k.SessionTree {
		t.Fatal("kilo session_tree must default false (plan PD2)")
	}
	if k.Prewarm {
		t.Fatal("kilo prewarm should default false (MADR 0089 D5)")
	}
	if k.PermissionTimeoutSeconds != 120 || k.TurnStallNoticeSeconds != 120 {
		t.Fatalf("kilo timeouts = %d/%d, want 120/120",
			k.PermissionTimeoutSeconds, k.TurnStallNoticeSeconds)
	}
	if k.StreamCoalesceMs != 80 {
		t.Fatalf("kilo stream_coalesce_ms = %d, want 80", k.StreamCoalesceMs)
	}
	if k.Model != "" {
		t.Fatalf("kilo model must default empty (engine default is auth-state-dependent), got %q", k.Model)
	}
}

// Kilo env overrides must resolve via AutomaticEnv — every key needs a viper
// default or MCREMOTE_PROVIDERS_KILO_* is silently ignored.
func TestKiloEnvOverrides(t *testing.T) {
	t.Setenv("MCREMOTE_PROVIDERS_KILO_ENABLED", "true")
	t.Setenv("MCREMOTE_PROVIDERS_KILO_MODEL", "kilo/~anthropic/claude-sonnet")
	t.Setenv("MCREMOTE_PROVIDERS_KILO_PERMISSION_TIMEOUT_SECONDS", "60")
	t.Setenv("MCREMOTE_PROVIDERS_KILO_SESSION_TREE", "true")
	t.Setenv("MCREMOTE_PROVIDERS_KILO_PURE", "true")
	cfg, err := config.Load(config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	k := cfg.Providers.Kilo
	if !k.Enabled || k.Model != "kilo/~anthropic/claude-sonnet" || k.PermissionTimeoutSeconds != 60 {
		t.Fatalf("kilo env overrides not applied: %+v", k)
	}
	if !k.SessionTree || !k.Pure {
		t.Fatalf("kilo bool env overrides not applied: %+v", k)
	}
	if k.Bin != "kilo" {
		t.Fatalf("untouched kilo defaults should survive: %+v", k)
	}
}

func TestValidateRejectsBadKiloRanges(t *testing.T) {
	cfg := config.Defaults()
	cfg.Providers.Kilo.PermissionTimeoutSeconds = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for negative kilo permission timeout")
	}
	cfg = config.Defaults()
	cfg.Providers.Kilo.TurnStallNoticeSeconds = -5
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for negative kilo stall")
	}
	cfg = config.Defaults()
	cfg.Providers.Kilo.StreamCoalesceMs = 5000
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for out-of-range kilo stream_coalesce_ms")
	}
}

func TestValidateGrokReasoningEffort(t *testing.T) {
	cfg := config.Defaults()
	cfg.Providers.Grok.ReasoningEffort = "  "
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "providers.grok.reasoning_effort") {
		t.Fatalf("expected error naming providers.grok.reasoning_effort for whitespace value, got %v", err)
	}

	cfg.Providers.Grok.ReasoningEffort = "high"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid reasoning_effort rejected: %v", err)
	}
}

func TestResolveListenHostTailscaleSentinel(t *testing.T) {
	orig := tailnet.IPv4
	t.Cleanup(func() { tailnet.IPv4 = orig })

	t.Run("resolves to the tailnet IPv4", func(t *testing.T) {
		tailnet.IPv4 = func() string { return "100.64.0.7" }
		cfg := config.Defaults()
		cfg.Listen.Host = "tailscale"
		if err := cfg.ResolveListenHost(); err != nil {
			t.Fatal(err)
		}
		if cfg.Listen.Host != "100.64.0.7" {
			t.Fatalf("host=%s", cfg.Listen.Host)
		}
		if cfg.Addr() != "100.64.0.7:7531" {
			t.Fatalf("addr=%s", cfg.Addr())
		}
	})

	t.Run("fails closed with no tailnet address", func(t *testing.T) {
		tailnet.IPv4 = func() string { return "" }
		cfg := config.Defaults()
		cfg.Listen.Host = "tailscale"
		err := cfg.ResolveListenHost()
		if err == nil {
			t.Fatal("want error when no Tailscale IPv4 exists")
		}
		if !strings.Contains(err.Error(), "tailscale ip -4") {
			t.Fatalf("error is not actionable: %v", err)
		}
		// Never silently widens.
		if cfg.Listen.Host != "tailscale" {
			t.Fatalf("host mutated to %s", cfg.Listen.Host)
		}
	})

	t.Run("leaves explicit hosts alone", func(t *testing.T) {
		tailnet.IPv4 = func() string { return "100.64.0.7" }
		for _, host := range []string{"0.0.0.0", "127.0.0.1", "192.168.1.5"} {
			cfg := config.Defaults()
			cfg.Listen.Host = host
			if err := cfg.ResolveListenHost(); err != nil {
				t.Fatal(err)
			}
			if cfg.Listen.Host != host {
				t.Fatalf("host %s rewritten to %s", host, cfg.Listen.Host)
			}
		}
	})

	t.Run("sentinel survives load and validation", func(t *testing.T) {
		tailnet.IPv4 = func() string { return "100.64.0.7" }
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(path, []byte("listen:\n  host: \"tailscale\"\n  port: 7531\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(config.LoadOptions{ConfigFile: path})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Listen.Host != config.ListenHostTailscale {
			t.Fatalf("host=%s (Load must not resolve; the daemon does)", cfg.Listen.Host)
		}
		if err := cfg.ResolveListenHost(); err != nil {
			t.Fatal(err)
		}
		if cfg.Listen.Host != "100.64.0.7" {
			t.Fatalf("host=%s", cfg.Listen.Host)
		}
	})
}

// Pinned expresses client policy; AdvertisesFingerprint decides whether the
// pair URI carries fp= at all. Conflating them is what made the Let's Encrypt
// fallback unreachable, so they are asserted apart.
func TestFingerprintAdvertisedInBothTLSModes(t *testing.T) {
	cases := []struct {
		mode       string
		advertises bool
		pinnedOnly bool
	}{
		{config.TLSModeSelfSigned, true, true},
		{config.TLSModeLetsEncrypt, true, false},
		{config.TLSModeOff, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			tls := config.TLSConfig{Mode: tc.mode, Enabled: tc.mode != config.TLSModeOff}
			if got := tls.AdvertisesFingerprint(); got != tc.advertises {
				t.Errorf("AdvertisesFingerprint=%v want %v", got, tc.advertises)
			}
			if got := tls.Pinned(); got != tc.pinnedOnly {
				t.Errorf("Pinned=%v want %v", got, tc.pinnedOnly)
			}
		})
	}
}

// `serve --tls=false` must not hard-error when the config selects a tls.mode:
// serve owns the legacy switch and applies it after Load via TLS.WithEnabled.
// Load previously bound --tls into tls.enabled, so Validate saw enabled=false
// beside mode=selfsigned and rejected the combination before serve could act.
func TestLoadTLSDisableFlagWithConfiguredMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("tls:\n  mode: selfsigned\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Mirror serve's --tls bool flag, set false on the command line.
	fs := pflag.NewFlagSet("serve", pflag.ContinueOnError)
	fs.Bool("tls", true, "")
	if err := fs.Parse([]string{"--tls=false"}); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(config.LoadOptions{ConfigFile: path, Flags: fs})
	if err != nil {
		t.Fatalf("Load with --tls=false and tls.mode=selfsigned should succeed, got: %v", err)
	}
	// Load leaves the resolved mode intact; the switch is serve's to apply.
	if cfg.TLS.ResolvedMode() != config.TLSModeSelfSigned {
		t.Fatalf("mode=%s want selfsigned before the override", cfg.TLS.ResolvedMode())
	}
	// And the switch, once applied the way serve does, still disables TLS.
	if off := cfg.TLS.WithEnabled(false); off.ResolvedMode() != config.TLSModeOff {
		t.Fatalf("after --tls=false override mode=%s want off", off.ResolvedMode())
	}
}

// providers.opencode.transport was retired in MADR 0019. Because viper ignores
// unknown keys, a config that still pins it would otherwise be silently
// upgraded to the shared-engine transport — so loading must fail loudly, and
// the error must name the offending value.
func TestLoadRejectsRetiredOpencodeTransport(t *testing.T) {
	for _, value := range []string{"acp", "http"} {
		t.Run(value, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			content := "providers:\n  opencode:\n    transport: \"" + value + "\"\n"
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := config.Load(config.LoadOptions{ConfigFile: path})
			if err == nil {
				t.Fatal("expected an error for a config pinning the retired transport key")
			}
			if !strings.Contains(err.Error(), "transport is no longer supported") {
				t.Fatalf("error should explain the retirement, got: %v", err)
			}
			if !strings.Contains(err.Error(), value) {
				t.Fatalf("error should name the offending value %q, got: %v", value, err)
			}
		})
	}
}

// A config with no transport key at all is the post-0019 shape and must load.
func TestLoadWithoutTransportKeySucceeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "providers:\n  opencode:\n    enabled: true\n    model: \"opencode/x\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.LoadOptions{ConfigFile: path})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Providers.Opencode.Enabled || cfg.Providers.Opencode.Model != "opencode/x" {
		t.Fatalf("opencode config not applied: %+v", cfg.Providers.Opencode)
	}
	// Prewarm omitted in YAML must follow Defaults() (MADR 0089 D5).
	if cfg.Providers.Opencode.Prewarm {
		t.Fatal("prewarm should default to false")
	}
	// Session tree demux defaults on (MADR 0020 KD11 / Q5).
	if !cfg.Providers.Opencode.SessionTree {
		t.Fatal("session_tree should default to true")
	}
	// Streaming text is coalesced by default (MADR 0024); a zero here would
	// silently restore one WebSocket frame per model token.
	if got := cfg.Providers.Opencode.StreamCoalesceMs; got != 80 {
		t.Fatalf("stream_coalesce_ms = %d, want the 80ms default", got)
	}
	if got := cfg.Providers.Grok.StreamCoalesceMs; got != 80 {
		t.Fatalf("providers.grok.stream_coalesce_ms = %d, want the 80ms default", got)
	}
}

func TestLoadStreamCoalesce(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("override applies", func(t *testing.T) {
		path := write(t, "providers:\n  opencode:\n    stream_coalesce_ms: 200\n")
		cfg, err := config.Load(config.LoadOptions{ConfigFile: path})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got := cfg.Providers.Opencode.StreamCoalesceMs; got != 200 {
			t.Fatalf("stream_coalesce_ms = %d, want 200", got)
		}
	})

	t.Run("zero is a valid kill switch", func(t *testing.T) {
		path := write(t, "providers:\n  opencode:\n    stream_coalesce_ms: 0\n")
		cfg, err := config.Load(config.LoadOptions{ConfigFile: path})
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if got := cfg.Providers.Opencode.StreamCoalesceMs; got != 0 {
			t.Fatalf("stream_coalesce_ms = %d, want 0 to disable coalescing", got)
		}
	})

	for _, bad := range []string{"-1", "1001"} {
		t.Run("rejects "+bad, func(t *testing.T) {
			path := write(t, "providers:\n  opencode:\n    stream_coalesce_ms: "+bad+"\n")
			if _, err := config.Load(config.LoadOptions{ConfigFile: path}); err == nil {
				t.Fatalf("stream_coalesce_ms: %s must be rejected", bad)
			}
		})
	}
}

func TestLoadSessionTreeKillSwitch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "providers:\n  opencode:\n    session_tree: false\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.LoadOptions{ConfigFile: path})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Providers.Opencode.SessionTree {
		t.Fatal("session_tree: false must apply")
	}
}

// The retired env override must be caught too, not just the file key.
func TestLoadRejectsRetiredOpencodeTransportFromEnv(t *testing.T) {
	t.Setenv("MCREMOTE_PROVIDERS_OPENCODE_TRANSPORT", "acp")
	if _, err := config.Load(config.LoadOptions{}); err == nil {
		t.Fatal("expected an error for MCREMOTE_PROVIDERS_OPENCODE_TRANSPORT")
	} else if !strings.Contains(err.Error(), "transport is no longer supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// grok's permission mode is pinned rather than inherited: with it empty the
// daemon cannot know the session's approval posture (grok resolves it from its
// own config, project config, or fleet-wide remote config), yet it advertises
// modes and a `dangerous` flag as if it did (MADR 0050 D3).
func TestGrokPermissionModeDefaultsToDefault(t *testing.T) {
	if got := config.Defaults().Providers.Grok.PermissionMode; got != "default" {
		t.Fatalf("providers.grok.permission_mode default = %q, want \"default\"", got)
	}
}

func TestGrokPermissionModeValidation(t *testing.T) {
	for _, ok := range []string{"", "default", "acceptEdits", "auto", "dontAsk", "bypassPermissions", "plan"} {
		c := config.Defaults()
		c.Providers.Grok.PermissionMode = ok
		if err := c.Validate(); err != nil {
			t.Errorf("%q must be accepted: %v", ok, err)
		}
	}
	// Rejected at load, not at session start: grok exits with
	// `error: unexpected argument` on an unknown value, which would surface as
	// a provider that never becomes ready.
	for _, bad := range []string{"nonsense", "Default", "yolo"} {
		c := config.Defaults()
		c.Providers.Grok.PermissionMode = bad
		if err := c.Validate(); err == nil {
			t.Errorf("%q must fail config validation", bad)
		}
	}
}

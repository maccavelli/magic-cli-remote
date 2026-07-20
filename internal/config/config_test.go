package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/tailnet"
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

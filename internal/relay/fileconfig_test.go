package relay

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
)

func TestLoadFromYAMLAndAllow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `
listen:
  host: 127.0.0.1
  port: 9443
tls:
  mode: off
log:
  level: debug
  format: json
hosts:
  - id: from-file
    secret: sixteen-chars-min-1
limits:
  max_hosts: 4
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(LoadOptions{
		ConfigFile: path,
		AllowExtra: []string{"from-flag:sixteen-chars-min-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen.Host != "127.0.0.1" || cfg.Listen.Port != 9443 {
		t.Fatalf("listen %+v", cfg.Listen)
	}
	if cfg.Log.Level != "debug" || cfg.Log.Format != "json" {
		t.Fatalf("log %+v", cfg.Log)
	}
	if cfg.Limits.MaxHosts != 4 {
		t.Fatalf("limits %+v", cfg.Limits)
	}
	if len(cfg.Hosts) != 2 {
		t.Fatalf("hosts %d %+v", len(cfg.Hosts), cfg.Hosts)
	}
	srv := cfg.ToServerConfig()
	if srv.ListenAddr != "127.0.0.1:9443" {
		t.Fatalf("addr %s", srv.ListenAddr)
	}
	if len(srv.Allow) != 2 {
		t.Fatalf("allow %d", len(srv.Allow))
	}
}

func TestLoadHostsEnv(t *testing.T) {
	t.Setenv("MCRELAY_HOSTS", "env-host:sixteen-chars-min-e")
	// No file: empty config paths.
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// Load without config file — defaults + env.
	cfg, err := Load(LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range cfg.Hosts {
		if h.ID == "env-host" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected env host in %+v", cfg.Hosts)
	}
}

func TestLoadRejectsShortSecret(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
hosts:
  - id: x
    secret: short
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(LoadOptions{ConfigFile: path}); err == nil {
		t.Fatal("expected short secret error")
	}
}

func TestTLSNormalizedLetsEncrypt(t *testing.T) {
	tls := TLSConfig{
		LetsEncrypt: LetsEncryptConfig{
			Domains: []string{"relay.example.com"},
			Email:   "ops@example.com"},
	}.Normalized()
	if tls.Mode != TLSModeLetsEncrypt {
		t.Fatalf("mode=%s", tls.Mode)
	}
	tls2 := TLSConfig{CertFile: "c.pem", KeyFile: "k.pem"}.Normalized()
	if tls2.Mode != TLSModeFiles {
		t.Fatalf("mode=%s", tls2.Mode)
	}
}

func TestValidateLetsEncryptRequiresEmail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
tls:
  mode: letsencrypt
  letsencrypt:
    domains: [relay.example.com]
hosts:
  - id: h
    secret: sixteen-chars-min-h
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(LoadOptions{ConfigFile: path}); err == nil {
		t.Fatal("expected email required")
	}
}

func TestValidateRejectsLimitCeilings(t *testing.T) {
	cfg := DefaultsFile()
	cfg.Hosts = []HostEntry{{ID: "h1", Secret: "sixteen-chars-min-1"}}
	cfg.Limits.MaxMessageBytes = MaxLimitMessageBytes + 1
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected max_message_bytes ceiling reject")
	}
	cfg.Limits.MaxMessageBytes = MaxLimitMessageBytes
	cfg.Limits.MaxHosts = MaxLimitHosts + 1
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected max_hosts ceiling reject")
	}
	cfg.Limits.MaxHosts = 32
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestToServerConfigLegacyFlag(t *testing.T) {
	cfg := DefaultsFile()
	cfg.Hosts = []HostEntry{{ID: "h1", Secret: "sixteen-chars-min-1"}}
	cfg.AllowLegacyTunnelSecret = true
	srv := cfg.ToServerConfig()
	if !srv.AllowLegacyTunnelSecret {
		t.Fatal("expected legacy flag")
	}
}

func TestLoadFlagOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
listen:
  host: 0.0.0.0
  port: 8443
hosts:
  - id: h
    secret: sixteen-chars-min-h
`), 0o600); err != nil {
		t.Fatal(err)
	}
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("listen-host", "", "")
	fs.Int("listen-port", 0, "")
	_ = fs.Set("listen-host", "127.0.0.1")
	_ = fs.Set("listen-port", "9001")
	cfg, err := Load(LoadOptions{ConfigFile: path, Flags: fs})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen.Host != "127.0.0.1" || cfg.Listen.Port != 9001 {
		t.Fatalf("got %+v", cfg.Listen)
	}
}

// TestAllowEntryTrailingWhitespaceSecret pins 0115 F3: one parse for --allow
// entries. Pre-fix, ParseAllowFlag validated a TrimSpace'd copy while
// hostEntryFromAllow re-sliced the raw string, so a trailing space (easy in a
// systemd Environment= line) was hashed into the stored secret and the host
// could never authenticate.
func TestAllowEntryTrailingWhitespaceSecret(t *testing.T) {
	const raw = "h1:0123456789abcdef "
	ent, err := hostEntryFromAllow(raw)
	if err != nil {
		t.Fatal(err)
	}
	if ent.Secret != "0123456789abcdef" {
		t.Fatalf("stored secret %q; trailing whitespace must be trimmed with the entry", ent.Secret)
	}
	cred, err := ParseAllowFlag(raw)
	if err != nil {
		t.Fatal(err)
	}
	if HashSecret(ent.Secret) != cred.SecretHash {
		t.Fatal("hostEntryFromAllow and ParseAllowFlag disagree on the same entry")
	}
}

// TestServeFlagsSingleMechanism guards the 0115 F15 deletion of newServeCmd's
// manual overrides: values must reach FileConfig through bindRelayFlags+Load
// alone for representative flag types (int, string, bool).
func TestServeFlagsSingleMechanism(t *testing.T) {
	// Hermetic: never pick up a real ~/.config/mcrelay/config.yaml.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fs := pflag.NewFlagSet("serve", pflag.ContinueOnError)
	fs.Int("listen-port", 0, "")
	fs.String("tls-mode", "", "")
	fs.Bool("allow-legacy-tunnel-secret", false, "")
	if err := fs.Parse([]string{
		"--listen-port", "9443",
		"--tls-mode", "off",
		"--allow-legacy-tunnel-secret",
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(LoadOptions{
		Flags:      fs,
		AllowExtra: []string{"h1:0123456789abcdef"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen.Port != 9443 {
		t.Fatalf("listen.port=%d, want 9443 via bindRelayFlags alone", cfg.Listen.Port)
	}
	if cfg.TLS.Mode != TLSModeOff {
		t.Fatalf("tls.mode=%q, want off", cfg.TLS.Mode)
	}
	if !cfg.AllowLegacyTunnelSecret {
		t.Fatal("allow_legacy_tunnel_secret should be true via bindRelayFlags alone")
	}
}

package relay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

func TestLoadFromYAMLAndAllow(t *testing.T) {
	dir := privateFixtureDir(t)
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

func TestAddrDefaultHost(t *testing.T) {
	fc := FileConfig{Listen: ListenConfig{Port: 8443}}
	if got := fc.Addr(); got != "0.0.0.0:8443" {
		t.Fatalf("Addr()=%q", got)
	}
}

func TestVersionStringNonEmpty(t *testing.T) {
	if VersionString() == "" {
		t.Fatal("VersionString empty")
	}
}

func TestCheckSecretFilesSkipsNonFilesMode(t *testing.T) {
	if err := checkSecretFiles(FileConfig{TLS: TLSConfig{Mode: TLSModeOff}}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSecretFilesEmptyPaths(t *testing.T) {
	if err := checkSecretFiles(FileConfig{TLS: TLSConfig{Mode: TLSModeFiles}}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSecretFilesMissingPEM(t *testing.T) {
	err := checkSecretFiles(FileConfig{TLS: TLSConfig{
		Mode:     TLSModeFiles,
		CertFile: "/no/such/mcrelay-cert.pem",
		KeyFile:  "/no/such/mcrelay-key.pem",
	}})
	if err == nil {
		t.Fatal("want error for missing PEM")
	}
}

func TestCheckSecretFilesRejectsWorldReadablePEM(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	owner, err := appdirs.FileIsOwnerOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	if owner {
		t.Skip("0644 still owner-only on this OS")
	}
	err = checkSecretFiles(FileConfig{TLS: TLSConfig{Mode: TLSModeFiles, CertFile: p, KeyFile: p}})
	if err == nil || !strings.Contains(err.Error(), "chmod 0600") {
		t.Fatalf("err=%v; want chmod 0600", err)
	}
}

func TestCheckSecretFilesAcceptsOwnerOnlyPEM(t *testing.T) {
	dir := privateFixtureDir(t)
	p := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkSecretFiles(FileConfig{TLS: TLSConfig{Mode: TLSModeFiles, CertFile: p, KeyFile: p}}); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsWorldReadableConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `
listen:
  host: 127.0.0.1
  port: 8443
tls:
  mode: off
hosts:
  - id: h1
    secret: sixteen-chars-min-1
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	owner, err := appdirs.FileIsOwnerOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	if owner {
		t.Skip("config mode 0644 is still owner-only on this OS")
	}
	_, err = Load(LoadOptions{ConfigFile: path})
	if err == nil || !strings.Contains(err.Error(), "chmod 0600") {
		t.Fatalf("err=%v; want chmod 0600", err)
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

func TestToServerConfigBlanksSecrets(t *testing.T) {
	const secret = "sixteen-chars-min-1"
	cfg := DefaultsFile()
	cfg.Hosts = []HostEntry{{ID: "h1", Secret: secret}}
	srv := cfg.ToServerConfig()
	if cfg.Hosts[0].Secret != "" {
		t.Fatalf("Hosts[0].Secret = %q, want empty after hash", cfg.Hosts[0].Secret)
	}
	if srv.Allow[0].SecretHash != HashSecret(secret) {
		t.Fatal("SecretHash does not match HashSecret of the original")
	}
}

func TestParseAllowPartsErrorOmitsSecret(t *testing.T) {
	const leak = "sixteen-chars-min-secret"
	_, _, err := parseAllowParts("not-a-pair-" + leak)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), leak) {
		t.Fatalf("error quotes secret material: %v", err)
	}
}

func TestLoadFlagOverride(t *testing.T) {
	dir := privateFixtureDir(t)
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

func privateFixtureDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private")
	if err := appdirs.EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	return dir
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

// 0115 P8: helper coverage — hints, data-dir creation, path recompute, list
// expansion, and the limits-config ceiling branches.

func TestPathHintsNonEmpty(t *testing.T) {
	if ConfigPathHint() == "" {
		t.Fatal("ConfigPathHint empty")
	}
	if DataDirHint() == "" {
		t.Fatal("DataDirHint empty")
	}
}

func TestEnsureDataDirCreatesPrivate(t *testing.T) {
	testexec.SkipIfNoPOSIXModes(t)
	dir := filepath.Join(t.TempDir(), "nested", "data")
	if err := EnsureDataDir(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("perm=%o, want 0700", perm)
	}
}

func TestRecomputePaths(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := FileConfig{DataDir: filepath.Join(t.TempDir(), "d")}
	if err := cfg.RecomputePaths(); err != nil {
		t.Fatal(err)
	}
	if cfg.Paths.DataDir != cfg.DataDir {
		t.Fatalf("paths.DataDir=%q, want %q", cfg.Paths.DataDir, cfg.DataDir)
	}
	// Second call takes the WithDataDir branch (RuntimeBase already set).
	cfg.DataDir = filepath.Join(t.TempDir(), "e")
	if err := cfg.RecomputePaths(); err != nil {
		t.Fatal(err)
	}
	if cfg.Paths.DataDir != cfg.DataDir {
		t.Fatalf("recompute: %q vs %q", cfg.Paths.DataDir, cfg.DataDir)
	}
}

func TestExpandStringListSplitsAndTrims(t *testing.T) {
	got := expandStringList([]string{"a, b", "  c  ", "", ",,"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%v", got)
		}
	}
	if out := expandStringList(nil); out != nil {
		t.Fatalf("nil in, %v out", out)
	}
}

func TestValidateLimitsCeilings(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*LimitsConfig)
	}{
		{"max_hosts", func(l *LimitsConfig) { l.MaxHosts = MaxLimitHosts + 1 }},
		{"max_phones_per_host", func(l *LimitsConfig) { l.MaxPhonesPerHost = MaxLimitPhonesPerHost + 1 }},
		{"max_message_bytes", func(l *LimitsConfig) { l.MaxMessageBytes = MaxLimitMessageBytes + 1 }},
		{"max_concurrent_join", func(l *LimitsConfig) { l.MaxConcurrentJoin = MaxLimitConcurrentJoin + 1 }},
		{"max_conns", func(l *LimitsConfig) { l.MaxConns = MaxLimitConns + 1 }},
		{"accept_per_minute", func(l *LimitsConfig) { l.AcceptPerMinute = MaxLimitPerMinute + 1 }},
		{"join_per_minute", func(l *LimitsConfig) { l.JoinPerMinute = MaxLimitPerMinute + 1 }},
		{"register_per_minute", func(l *LimitsConfig) { l.RegisterPerMinute = MaxLimitPerMinute + 1 }},
		{"join_per_host_per_minute", func(l *LimitsConfig) { l.JoinPerHostPerMinute = MaxLimitPerMinute + 1 }},
		{"tunnel_wait_seconds", func(l *LimitsConfig) { l.TunnelWaitSeconds = MaxLimitDurationSeconds + 1 }},
		{"register_idle_seconds", func(l *LimitsConfig) { l.RegisterIdleSeconds = MaxLimitDurationSeconds + 1 }},
		{"splice_idle_seconds", func(l *LimitsConfig) { l.SpliceIdleSeconds = MaxLimitDurationSeconds + 1 }},
		{"splice_max_seconds", func(l *LimitsConfig) { l.SpliceMaxSeconds = MaxLimitDurationSeconds + 1 }},
	}
	for _, c := range cases {
		var l LimitsConfig
		c.mut(&l)
		err := validateLimitsConfig(l)
		if err == nil || !strings.Contains(err.Error(), c.name) {
			t.Errorf("%s: err=%v; want ceiling rejection naming the field", c.name, err)
		}
	}
	// Negative splice knobs mean "disabled" and pass.
	if err := validateLimitsConfig(LimitsConfig{SpliceIdleSeconds: -1, SpliceMaxSeconds: -1}); err != nil {
		t.Fatalf("negative splice knobs: %v", err)
	}
}

func TestValidateMaxConnsCeiling(t *testing.T) {
	err := validateLimitsConfig(LimitsConfig{MaxConns: MaxLimitConns + 1})
	if err == nil || !strings.Contains(err.Error(), "max_conns") {
		t.Fatalf("err=%v; want limits.max_conns ceiling rejection", err)
	}
}

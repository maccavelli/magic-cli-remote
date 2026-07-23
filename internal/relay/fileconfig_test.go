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

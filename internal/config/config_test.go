package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
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

func TestInvalidPort(t *testing.T) {
	cfg := config.Defaults()
	cfg.Listen.Port = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error")
	}
}

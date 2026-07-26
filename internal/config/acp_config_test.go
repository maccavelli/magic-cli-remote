package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The shared ACP options (auth method + MCP servers) parse for grok, ready to
// be reused by goose/codex which embed the same ACPProviderConfig.
func TestGrokACPConfigParse(t *testing.T) {
	path := writeConfig(t, `
providers:
  grok:
    enabled: true
    auth_method_id: "oauth"
    mcp_servers:
      - name: docs
        transport: http
        url: https://mcp.example.com/sse
        headers:
          Authorization: "Bearer x"
`)
	cfg, err := config.Load(config.LoadOptions{ConfigFile: path})
	if err != nil {
		t.Fatal(err)
	}
	g := cfg.Providers.Grok
	if g.AuthMethodID != "oauth" {
		t.Fatalf("auth_method_id = %q, want oauth", g.AuthMethodID)
	}
	if len(g.MCPServers) != 1 {
		t.Fatalf("mcp_servers = %+v, want 1", g.MCPServers)
	}
	m := g.MCPServers[0]
	// viper lowercases map keys; HTTP header names are case-insensitive so this
	// is harmless, but assert the actual (lowercased) key.
	if m.Name != "docs" || m.Transport != "http" || m.URL == "" || m.Headers["authorization"] != "Bearer x" {
		t.Fatalf("mcp_servers[0] = %+v", m)
	}
	// Existing squashed fields still parse via embedding.
	if !g.Enabled {
		t.Fatal("grok.enabled lost through squash embedding")
	}
}

func TestGrokMCPTransportValidation(t *testing.T) {
	path := writeConfig(t, `
providers:
  grok:
    mcp_servers:
      - name: bad
        transport: stdio
        url: x
`)
	_, err := config.Load(config.LoadOptions{ConfigFile: path})
	if err == nil || !strings.Contains(err.Error(), "transport must be http|sse") {
		t.Fatalf("want transport validation error, got %v", err)
	}
}

func TestGrokMCPMissingURLValidation(t *testing.T) {
	path := writeConfig(t, `
providers:
  grok:
    mcp_servers:
      - name: nourl
        transport: sse
`)
	_, err := config.Load(config.LoadOptions{ConfigFile: path})
	if err == nil || !strings.Contains(err.Error(), "url must not be empty") {
		t.Fatalf("want url validation error, got %v", err)
	}
}

func TestGrokAuthMethodEnvOverride(t *testing.T) {
	t.Setenv("MCREMOTE_PROVIDERS_GROK_AUTH_METHOD_ID", "envauth")
	cfg, err := config.Load(config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers.Grok.AuthMethodID != "envauth" {
		t.Fatalf("auth_method_id env override = %q, want envauth", cfg.Providers.Grok.AuthMethodID)
	}
}

func TestGooseWithBuiltinsParseAndValidate(t *testing.T) {
	path := writeConfig(t, `
providers:
  goose:
    with_builtins: [developer, computercontroller]
`)
	cfg, err := config.Load(config.LoadOptions{ConfigFile: path})
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Providers.Goose.WithBuiltins
	if len(got) != 2 || got[0] != "developer" || got[1] != "computercontroller" {
		t.Fatalf("with_builtins = %#v", got)
	}
}

func TestGooseWithBuiltinsRejectsEmptyAndDuplicate(t *testing.T) {
	for name, builtins := range map[string]string{
		"empty":     "[developer, '']",
		"duplicate": "[developer, developer]",
	} {
		t.Run(name, func(t *testing.T) {
			path := writeConfig(t, "providers:\n  goose:\n    with_builtins: "+builtins+"\n")
			_, err := config.Load(config.LoadOptions{ConfigFile: path})
			if err == nil || !strings.Contains(err.Error(), "with_builtins") {
				t.Fatalf("want with_builtins validation error, got %v", err)
			}
		})
	}
}

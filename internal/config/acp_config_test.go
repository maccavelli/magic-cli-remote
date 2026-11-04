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

// acpSection is a section holding only the shared ACP keys, as an ACP agent's
// section embeds them (0208 MADR D6).
type acpSection struct {
	config.ACPProviderConfig `mapstructure:",squash"`
}

func (s *acpSection) Validate() error { return config.ValidateACPProvider("acp", s.ACPProviderConfig) }

var acpSpecs = []config.ProviderSectionSpec{{
	ID:  "acp",
	New: func() config.ProviderSection { return &acpSection{} },
}}

func loadACP(t *testing.T, body string) (*acpSection, error) {
	t.Helper()
	cfg, err := config.Load(config.LoadOptions{ConfigFile: writeConfig(t, body), Providers: acpSpecs})
	if err != nil {
		return nil, err
	}
	return cfg.Providers.Sections["acp"].(*acpSection), nil
}

// The shared ACP options (auth method + MCP servers) parse for any provider
// whose section embeds ACPProviderConfig.
func TestACPConfigParse(t *testing.T) {
	s, err := loadACP(t, `
providers:
  acp:
    enabled: true
    auth_method_id: "oauth"
    mcp_servers:
      - name: docs
        transport: http
        url: https://mcp.example.com/sse
        headers:
          Authorization: "Bearer x"
`)
	if err != nil {
		t.Fatal(err)
	}
	if s.AuthMethodID != "oauth" {
		t.Fatalf("auth_method_id = %q, want oauth", s.AuthMethodID)
	}
	if len(s.MCPServers) != 1 {
		t.Fatalf("mcp_servers = %+v, want 1", s.MCPServers)
	}
	m := s.MCPServers[0]
	// viper lowercases map keys; HTTP header names are case-insensitive so this
	// is harmless, but assert the actual (lowercased) key.
	if m.Name != "docs" || m.Transport != "http" || m.URL == "" || m.Headers["authorization"] != "Bearer x" {
		t.Fatalf("mcp_servers[0] = %+v", m)
	}
	// The keys every agent has parse through two levels of squash embedding.
	if !s.Enabled {
		t.Fatal("enabled lost through squash embedding")
	}
}

func TestACPMCPTransportValidation(t *testing.T) {
	_, err := loadACP(t, `
providers:
  acp:
    mcp_servers:
      - name: bad
        transport: stdio
        url: x
`)
	if err == nil || !strings.Contains(err.Error(), "providers.acp.mcp_servers[0].transport must be http|sse") {
		t.Fatalf("want transport validation error, got %v", err)
	}
}

func TestACPMCPMissingURLValidation(t *testing.T) {
	_, err := loadACP(t, `
providers:
  acp:
    mcp_servers:
      - name: nourl
        transport: sse
`)
	if err == nil || !strings.Contains(err.Error(), "url must not be empty") {
		t.Fatalf("want url validation error, got %v", err)
	}
}

func TestACPAuthMethodEnvOverride(t *testing.T) {
	isolateConfig(t)
	t.Setenv("MCREMOTE_PROVIDERS_ACP_AUTH_METHOD_ID", "envauth")
	cfg, err := config.Load(config.LoadOptions{Providers: acpSpecs})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Providers.Sections["acp"].(*acpSection).AuthMethodID; got != "envauth" {
		t.Fatalf("auth_method_id env override = %q, want envauth", got)
	}
}

// The shared list keys are config-file only: a variable would split an entry
// on its commas.
func TestACPListKeysAreConfigFileOnly(t *testing.T) {
	for _, name := range []string{"MCREMOTE_PROVIDERS_ACP_ARGS", "MCREMOTE_PROVIDERS_ACP_FS_ROOTS",
		"MCREMOTE_PROVIDERS_ACP_MCP_SERVERS"} {
		t.Run(name, func(t *testing.T) {
			isolateConfig(t)
			t.Setenv(name, "x")
			_, err := config.Load(config.LoadOptions{Providers: acpSpecs})
			if err == nil || !strings.Contains(err.Error(), "config-file only") {
				t.Fatalf("Load error = %v, want %s refused as config-file only", err, name)
			}
		})
	}
}

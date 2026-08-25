package config

import (
	"strings"
	"testing"
)

func TestCodexExecutionEnvironmentAndStandaloneConfigValidation(t *testing.T) {
	validEnvironment := CodexExecutionEnvironmentConfig{
		ID: "loop", ExecServerURL: "ws://127.0.0.1:9000", ConnectTimeoutMS: 5000,
		RuntimeWorkspaceRoots: []string{"/workspace"},
	}
	tests := []struct {
		name      string
		configure func(*Config)
		want      string
	}{
		{"empty environment id", func(c *Config) {
			c.Providers.Codex.Environments = []CodexExecutionEnvironmentConfig{{ExecServerURL: "wss://host/x", ConnectTimeoutMS: 1, RuntimeWorkspaceRoots: []string{"/x"}}}
		}, "environment id"},
		{"duplicate environment id", func(c *Config) {
			c.Providers.Codex.Environments = []CodexExecutionEnvironmentConfig{validEnvironment, validEnvironment}
		}, "duplicate environment id"},
		{"non websocket URL", func(c *Config) {
			e := validEnvironment
			e.ExecServerURL = "https://host"
			c.Providers.Codex.Environments = []CodexExecutionEnvironmentConfig{e}
		}, "exec_server_url"},
		{"remote plaintext websocket", func(c *Config) {
			e := validEnvironment
			e.ExecServerURL = "ws://example.com/x"
			c.Providers.Codex.Environments = []CodexExecutionEnvironmentConfig{e}
		}, "loopback"},
		{"timeout zero", func(c *Config) {
			e := validEnvironment
			e.ConnectTimeoutMS = 0
			c.Providers.Codex.Environments = []CodexExecutionEnvironmentConfig{e}
		}, "connect_timeout_ms"},
		{"timeout over minute", func(c *Config) {
			e := validEnvironment
			e.ConnectTimeoutMS = 60001
			c.Providers.Codex.Environments = []CodexExecutionEnvironmentConfig{e}
		}, "connect_timeout_ms"},
		{"relative root", func(c *Config) {
			e := validEnvironment
			e.RuntimeWorkspaceRoots = []string{"workspace"}
			c.Providers.Codex.Environments = []CodexExecutionEnvironmentConfig{e}
		}, "runtime_workspace_roots"},
		{"duplicate root", func(c *Config) {
			e := validEnvironment
			e.RuntimeWorkspaceRoots = []string{"/workspace", "/workspace/."}
			c.Providers.Codex.Environments = []CodexExecutionEnvironmentConfig{e}
		}, "duplicate"},
		{"empty env allowlist", func(c *Config) { c.Providers.Codex.StandaloneProcessEnvAllowlist = []string{""} }, "allowlist"},
		{"duplicate env allowlist", func(c *Config) { c.Providers.Codex.StandaloneProcessEnvAllowlist = []string{"TERM", "TERM"} }, "duplicate"},
		{"invalid env allowlist", func(c *Config) { c.Providers.Codex.StandaloneProcessEnvAllowlist = []string{"BAD-NAME"} }, "environment name"},
		{"secret env allowlist", func(c *Config) { c.Providers.Codex.StandaloneProcessEnvAllowlist = []string{"API_TOKEN"} }, "secret"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := Defaults()
			test.configure(&cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.want)) {
				t.Fatalf("error=%v want substring %q", err, test.want)
			}
		})
	}
	cfg := Defaults()
	cfg.Providers.Codex.Environments = []CodexExecutionEnvironmentConfig{
		validEnvironment,
		{ID: "remote", ExecServerURL: "wss://exec.example.com/socket", ConnectTimeoutMS: 60000, RuntimeWorkspaceRoots: []string{"/srv/work"}},
	}
	cfg.Providers.Codex.StandaloneProcessesEnabled = true
	cfg.Providers.Codex.StandaloneProcessEnvAllowlist = []string{"TERM", "LANG"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}
}

func TestCodexStandaloneProcessesDefaultOff(t *testing.T) {
	cfg := Defaults()
	if cfg.Providers.Codex.StandaloneProcessesEnabled || len(cfg.Providers.Codex.StandaloneProcessEnvAllowlist) != 0 || len(cfg.Providers.Codex.Environments) != 0 {
		t.Fatalf("unsafe defaults: %+v", cfg.Providers.Codex)
	}
}

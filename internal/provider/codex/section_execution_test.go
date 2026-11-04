package codex

import (
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

func TestCodexExecutionEnvironmentAndStandaloneConfigValidation(t *testing.T) {
	testexec.SkipIfNoPOSIXPaths(t)
	validEnvironment := EnvironmentSection{
		ID: "loop", ExecServerURL: "ws://127.0.0.1:9000", ConnectTimeoutMS: 5000,
		RuntimeWorkspaceRoots: []string{"/workspace"},
	}
	tests := []struct {
		name      string
		configure func(*Section)
		want      string
	}{
		{"empty environment id", func(c *Section) {
			c.Environments = []EnvironmentSection{{ExecServerURL: "wss://host/x", ConnectTimeoutMS: 1, RuntimeWorkspaceRoots: []string{"/x"}}}
		}, "environment id"},
		{"duplicate environment id", func(c *Section) {
			c.Environments = []EnvironmentSection{validEnvironment, validEnvironment}
		}, "duplicate environment id"},
		{"non websocket URL", func(c *Section) {
			e := validEnvironment
			e.ExecServerURL = "https://host"
			c.Environments = []EnvironmentSection{e}
		}, "exec_server_url"},
		{"remote plaintext websocket", func(c *Section) {
			e := validEnvironment
			e.ExecServerURL = "ws://example.com/x"
			c.Environments = []EnvironmentSection{e}
		}, "loopback"},
		{"timeout zero", func(c *Section) {
			e := validEnvironment
			e.ConnectTimeoutMS = 0
			c.Environments = []EnvironmentSection{e}
		}, "connect_timeout_ms"},
		{"timeout over minute", func(c *Section) {
			e := validEnvironment
			e.ConnectTimeoutMS = 60001
			c.Environments = []EnvironmentSection{e}
		}, "connect_timeout_ms"},
		{"relative root", func(c *Section) {
			e := validEnvironment
			e.RuntimeWorkspaceRoots = []string{"workspace"}
			c.Environments = []EnvironmentSection{e}
		}, "runtime_workspace_roots"},
		{"duplicate root", func(c *Section) {
			e := validEnvironment
			e.RuntimeWorkspaceRoots = []string{"/workspace", "/workspace/."}
			c.Environments = []EnvironmentSection{e}
		}, "duplicate"},
		{"empty env allowlist", func(c *Section) { c.StandaloneProcessEnvAllowlist = []string{""} }, "allowlist"},
		{"duplicate env allowlist", func(c *Section) { c.StandaloneProcessEnvAllowlist = []string{"TERM", "TERM"} }, "duplicate"},
		{"invalid env allowlist", func(c *Section) { c.StandaloneProcessEnvAllowlist = []string{"BAD-NAME"} }, "environment name"},
		{"secret env allowlist", func(c *Section) { c.StandaloneProcessEnvAllowlist = []string{"API_TOKEN"} }, "secret"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := defaultSection()
			test.configure(cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.want)) {
				t.Fatalf("error=%v want substring %q", err, test.want)
			}
		})
	}
	cfg := defaultSection()
	cfg.Environments = []EnvironmentSection{
		validEnvironment,
		{ID: "remote", ExecServerURL: "wss://exec.example.com/socket", ConnectTimeoutMS: 60000, RuntimeWorkspaceRoots: []string{"/srv/work"}},
	}
	cfg.StandaloneProcessesEnabled = true
	cfg.StandaloneProcessEnvAllowlist = []string{"TERM", "LANG"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}
}

func TestCodexStandaloneProcessesDefaultOff(t *testing.T) {
	cfg := defaultSection()
	if cfg.StandaloneProcessesEnabled || len(cfg.StandaloneProcessEnvAllowlist) != 0 || len(cfg.Environments) != 0 {
		t.Fatalf("unsafe defaults: %+v", cfg)
	}
}

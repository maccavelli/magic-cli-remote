package acpagent

import (
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/config"
)

func TestConfigFromMinimal(t *testing.T) {
	got := ConfigFrom(config.ACPProviderConfig{AgentCommon: config.AgentCommon{Bin: "/usr/bin/agent"}})
	if got.Bin != "/usr/bin/agent" {
		t.Fatalf("Bin = %q, want /usr/bin/agent", got.Bin)
	}
	if got.PermissionTimeout != 0 {
		t.Fatalf("PermissionTimeout = %v, want 0", got.PermissionTimeout)
	}
	if len(got.McpServers) != 0 {
		t.Fatalf("McpServers = %v, want empty", got.McpServers)
	}
}

func TestConfigFromFull(t *testing.T) {
	cfg := config.ACPProviderConfig{
		AgentCommon: config.AgentCommon{
			Bin:                      "/usr/bin/agent",
			AlwaysApprove:            true,
			DefaultCWD:               "/home/<user>/project",
			Model:                    "model-x",
			PermissionTimeoutSeconds: 120,
			Prewarm:                  true,
			TurnStallNoticeSeconds:   30,
		},
		Args:         []string{"--debug"},
		FSRoots:      []string{"/home/<user>"},
		AuthMethodID: "github",
		MCPServers: []config.MCPServerConfig{
			{Name: "fs", Transport: "stdio", URL: "", Headers: nil},
			{Name: "web", Transport: "sse", URL: "http://localhost:8080", Headers: map[string]string{"Auth": "token"}},
		},
	}
	got := ConfigFrom(cfg)
	if got.Bin != cfg.Bin {
		t.Fatalf("Bin = %q, want %q", got.Bin, cfg.Bin)
	}
	if len(got.Args) != 1 || got.Args[0] != "--debug" {
		t.Fatalf("Args = %v", got.Args)
	}
	if !got.AlwaysApprove {
		t.Fatal("AlwaysApprove should be true")
	}
	if got.DefaultCWD != "/home/<user>/project" {
		t.Fatalf("DefaultCWD = %q", got.DefaultCWD)
	}
	if got.Model != "model-x" {
		t.Fatalf("Model = %q", got.Model)
	}
	if got.PermissionTimeout != 120*time.Second {
		t.Fatalf("PermissionTimeout = %v", got.PermissionTimeout)
	}
	if !got.Prewarm {
		t.Fatal("Prewarm should be true")
	}
	if got.TurnStallNotice != 30*time.Second {
		t.Fatalf("TurnStallNotice = %v", got.TurnStallNotice)
	}
	if len(got.FSRoots) != 1 || got.FSRoots[0] != "/home/<user>" {
		t.Fatalf("FSRoots = %v", got.FSRoots)
	}
	if got.AuthMethodID != "github" {
		t.Fatalf("AuthMethodID = %q", got.AuthMethodID)
	}
	if len(got.McpServers) != 2 {
		t.Fatalf("McpServers count = %d, want 2", len(got.McpServers))
	}
	if got.McpServers[0].Transport != McpTransport("stdio") {
		t.Fatalf("McpServers[0].Transport = %q", got.McpServers[0].Transport)
	}
	if got.McpServers[1].URL != "http://localhost:8080" {
		t.Fatalf("McpServers[1].URL = %q", got.McpServers[1].URL)
	}
}

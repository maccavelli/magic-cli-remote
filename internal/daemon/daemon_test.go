package daemon

import (
	"crypto/tls"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

func TestAcpAgentConfigMinimal(t *testing.T) {
	cfg := config.ACPProviderConfig{
		Bin: "/usr/bin/grok",
	}
	got := acpAgentConfig(cfg)
	if got.Bin != "/usr/bin/grok" {
		t.Fatalf("Bin = %q, want /usr/bin/grok", got.Bin)
	}
	if got.PermissionTimeout != 0 {
		t.Fatalf("PermissionTimeout = %v, want 0", got.PermissionTimeout)
	}
}

func TestAcpAgentConfigFull(t *testing.T) {
	cfg := config.ACPProviderConfig{
		Bin:                      "/usr/bin/agent",
		Args:                     []string{"--debug"},
		AlwaysApprove:            true,
		DefaultCWD:               "/home/user/project",
		Model:                    "gpt-4o",
		PermissionTimeoutSeconds: 120,
		Prewarm:                  true,
		TurnStallNoticeSeconds:   30,
		FSRoots:                  []string{"/home/user"},
		AuthMethodID:             "github",
		MCPServers: []config.MCPServerConfig{
			{Name: "fs", Transport: "stdio", URL: "", Headers: nil},
			{Name: "web", Transport: "sse", URL: "http://localhost:8080", Headers: map[string]string{"Auth": "token"}},
		},
	}
	got := acpAgentConfig(cfg)
	if got.Bin != cfg.Bin {
		t.Fatalf("Bin = %q, want %q", got.Bin, cfg.Bin)
	}
	if len(got.Args) != 1 || got.Args[0] != "--debug" {
		t.Fatalf("Args = %v", got.Args)
	}
	if !got.AlwaysApprove {
		t.Fatal("AlwaysApprove should be true")
	}
	if got.DefaultCWD != "/home/user/project" {
		t.Fatalf("DefaultCWD = %q", got.DefaultCWD)
	}
	if got.Model != "gpt-4o" {
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
	if len(got.FSRoots) != 1 || got.FSRoots[0] != "/home/user" {
		t.Fatalf("FSRoots = %v", got.FSRoots)
	}
	if got.AuthMethodID != "github" {
		t.Fatalf("AuthMethodID = %q", got.AuthMethodID)
	}
	if len(got.McpServers) != 2 {
		t.Fatalf("McpServers count = %d, want 2", len(got.McpServers))
	}
	if got.McpServers[0].Transport != acpagent.McpTransport("stdio") {
		t.Fatalf("McpServers[0].Transport = %q", got.McpServers[0].Transport)
	}
	if got.McpServers[1].URL != "http://localhost:8080" {
		t.Fatalf("McpServers[1].URL = %q", got.McpServers[1].URL)
	}
}

func TestAcpAgentConfigEmptyMcpServers(t *testing.T) {
	cfg := config.ACPProviderConfig{Bin: "agent"}
	got := acpAgentConfig(cfg)
	if len(got.McpServers) != 0 {
		t.Fatalf("McpServers = %v, want empty", got.McpServers)
	}
}

func TestAcpAgentConfigPermissionTimeoutZero(t *testing.T) {
	cfg := config.ACPProviderConfig{Bin: "agent", PermissionTimeoutSeconds: 0}
	got := acpAgentConfig(cfg)
	if got.PermissionTimeout != 0 {
		t.Fatalf("PermissionTimeout = %v, want 0", got.PermissionTimeout)
	}
}

func TestEventHubBroadcastNilServerNoPanic(t *testing.T) {
	hub := &eventHub{}
	hub.Broadcast(event.Event{Type: event.TypeSessionStatus})
}

func TestRequestClientCertSetsRequestClientCert(t *testing.T) {
	cfg := &tls.Config{}
	got := requestClientCert(cfg)
	if got.ClientAuth != tls.RequestClientCert {
		t.Fatalf("ClientAuth = %v, want RequestClientCert", got.ClientAuth)
	}
}

func TestRequestClientCertNilInput(t *testing.T) {
	got := requestClientCert(nil)
	if got != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestRequestClientCertPreservesOtherFields(t *testing.T) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "test"}
	got := requestClientCert(cfg)
	if got.MinVersion != tls.VersionTLS13 || got.ServerName != "test" {
		t.Fatalf("fields not preserved: %+v", got)
	}
}

func TestIdentityCloseNil(t *testing.T) {
	var id *Identity
	id.Close()
}

func TestIdentityCloseNilACME(t *testing.T) {
	id := &Identity{Mode: config.TLSModeSelfSigned}
	id.Close()
}

// TestOpencodeRemotePolicyReachesTheProvider proves the daemon copies the
// operator's decision into the constructed provider (MADR 0112 A8/A9, PLAN P8
// step 2).
//
// Without this the flags would be settable, documented and tested at the config
// layer while silently never arriving — which is the failure mode a policy
// default is least able to survive.
func TestOpencodeRemotePolicyReachesTheProvider(t *testing.T) {
	for _, tc := range []struct{ share, shell bool }{
		{false, false},
		{true, false},
		{false, true},
		{true, true},
	} {
		cfg := config.Defaults()
		cfg.Providers.Opencode.AllowRemoteShare = tc.share
		cfg.Providers.Opencode.AllowRemoteShell = tc.shell

		tree, coalesce := true, time.Duration(0)
		got := opencodeConfigFrom(cfg, &tree, &coalesce)
		if got.AllowRemoteShare != tc.share || got.AllowRemoteShell != tc.shell {
			t.Fatalf("share=%v shell=%v reached the provider as share=%v shell=%v",
				tc.share, tc.shell, got.AllowRemoteShare, got.AllowRemoteShell)
		}
	}
}

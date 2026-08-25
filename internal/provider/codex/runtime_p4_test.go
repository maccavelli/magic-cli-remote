package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/command"
)

func TestRuntimeSnapshotCapturesBoundedProviderState(t *testing.T) {
	p := New(Config{Transport: TransportUnixWS})
	p.generation = 7
	p.version = "0.149.1"
	p.cfg.Model = "gpt-test"
	p.cfg.ApprovalPolicy = "on-request"
	p.cfg.SandboxMode = "workspace-write"
	p.models = []modelRecord{{ID: "gpt-test", InputModalities: []string{"text", "image"}, SupportsPersonality: true,
		SupportedReasoningEfforts: []struct {
			ReasoningEffort string `json:"reasoningEffort"`
			Description     string `json:"description"`
		}{{ReasoningEffort: "high"}}}}
	p.runtime.usage = RuntimeUsage{Tokens: 1200, ContextWindow: 128000}
	p.runtime.workspaceNotice = []string{"workspace policy active"}
	p.runtime.features = map[string]RuntimeFeature{"apps": {ID: "apps", Enabled: true, Source: "config"}}
	p.handleProviderNotification("account/updated", json.RawMessage(`{"account":{"type":"chatgpt","planType":"pro"}}`))
	p.handleProviderNotification("account/rateLimits/updated", json.RawMessage(`{"rateLimits":{"primary":{"usedPercent":73,"windowDurationMins":300,"resetsAt":2000000000}}}`))
	p.handleProviderNotification("mcpServer/startupStatus/updated", json.RawMessage(`{"name":"tools","status":"ready"}`))
	p.handleProviderNotification("mcpServer/startupStatus/updated", json.RawMessage(`{"name":"second","status":"failed","error":"safe summary"}`))

	got := p.RuntimeSnapshot()
	if got.CodexVersion != "0.149.1" || got.Generation != 7 || got.Transport != string(TransportUnixWS) {
		t.Fatalf("identity = %+v", got)
	}
	if got.Account.Plan != "pro" || got.RateLimits.Primary.UsedPercent != 73 {
		t.Fatalf("account/rates = %+v", got)
	}
	if len(got.MCPServers) != 2 {
		t.Fatalf("mcp servers = %+v", got.MCPServers)
	}
	if got.Usage.ContextWindow != 128000 || got.Model.ID != "gpt-test" || got.Model.ContextWindow != 128000 || !got.Model.SupportsPersonality {
		t.Fatalf("usage/model = %+v %+v", got.Usage, got.Model)
	}
	if got.Config.Provenance != "daemon" || got.Config.ApprovalPolicy != "on-request" || len(got.Features) != 1 || len(got.WorkspaceNotice) != 1 {
		t.Fatalf("config/features/messages = %+v %+v %+v", got.Config, got.Features, got.WorkspaceNotice)
	}
	b, err := json.Marshal(got)
	if err != nil || len(b) > MaxRuntimeSnapshotBytes {
		t.Fatalf("snapshot bytes=%d err=%v", len(b), err)
	}
}

func TestRuntimeSnapshotBoundsMCPDiagnostics(t *testing.T) {
	p := New(Config{})
	for i := 0; i < maxRuntimeMCPServers+20; i++ {
		p.handleProviderNotification("mcpServer/startupStatus/updated", json.RawMessage(fmt.Sprintf(`{"name":"server-%03d","status":"ready"}`, i)))
	}
	got := p.RuntimeSnapshot()
	if len(got.MCPServers) != maxRuntimeMCPServers {
		t.Fatalf("MCP count = %d, want %d", len(got.MCPServers), maxRuntimeMCPServers)
	}
}

func TestStatusAndUsageCommandsAreMappedAndReadable(t *testing.T) {
	if commandTable["status"].Op != command.OpStatus || commandTable["usage"].Op != command.OpUsage {
		t.Fatalf("command mappings: status=%+v usage=%+v", commandTable["status"], commandTable["usage"])
	}
	p := New(Config{Transport: TransportWS})
	p.version = "0.149.1"
	p.runtime.usage = RuntimeUsage{Tokens: 10, ContextWindow: 100}
	p.runtime.rates.Primary.UsedPercent = 20
	s := p4Session()
	s.p = p
	status, err := s.RuntimeStatus(context.Background())
	if err != nil || !strings.Contains(status, "0.149.1") || !strings.Contains(status, "transport ws") {
		t.Fatalf("status=%q err=%v", status, err)
	}
	usage, err := s.RuntimeUsage(context.Background())
	if err != nil || !strings.Contains(usage, "10 tokens") || !strings.Contains(usage, "20%") {
		t.Fatalf("usage=%q err=%v", usage, err)
	}
}

func TestRuntimeStatusReadProjections(t *testing.T) {
	p := New(Config{})
	p.applyAccountRead(json.RawMessage(`{"account":{"type":"chatgpt","planType":"team"},"requiresOpenaiAuth":true}`))
	p.applyRateLimitsRead(json.RawMessage(`{"rateLimits":{"primary":{"usedPercent":42,"windowDurationMins":300,"resetsAt":9}},"rateLimitsByLimitId":null}`))
	p.applyAccountUsageRead(json.RawMessage(`{"summary":{"lifetimeTokens":123,"peakDailyTokens":7},"dailyUsageBuckets":null}`))
	p.applyWorkspaceMessagesRead(json.RawMessage(`{"featureEnabled":true,"messages":[{"messageId":"m","messageType":"headline","messageBody":"Maintenance soon","createdAt":null,"archivedAt":null}]}`))
	p.applyFeatureList(json.RawMessage(`{"data":[{"name":"apps","stage":"beta","displayName":"Apps","description":null,"announcement":null,"enabled":true,"defaultEnabled":false}],"nextCursor":null}`))
	p.applyMCPStatusList(json.RawMessage(`{"data":[{"name":"tools","pluginId":null,"serverInfo":null,"tools":{},"resources":[],"resourceTemplates":[],"authStatus":"oauth"}],"nextCursor":null}`))
	got := p.RuntimeSnapshot()
	if got.Account.Plan != "team" || got.RateLimits.Primary.UsedPercent != 42 || got.Usage.Tokens != 123 {
		t.Fatalf("account/rate/usage = %+v %+v %+v", got.Account, got.RateLimits, got.Usage)
	}
	if len(got.WorkspaceNotice) != 1 || len(got.Features) != 1 || len(got.MCPServers) != 1 {
		t.Fatalf("messages/features/mcp = %+v %+v %+v", got.WorkspaceNotice, got.Features, got.MCPServers)
	}
}

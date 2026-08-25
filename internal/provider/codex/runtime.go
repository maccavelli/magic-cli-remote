package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// MaxRuntimeSnapshotBytes is the hard response target for runtime status.
const MaxRuntimeSnapshotBytes = 262144

const maxRuntimeMCPServers = 64

// RuntimeSnapshot is the sanitized provider-global status returned to a
// negotiated Codex surface client.
type RuntimeSnapshot struct {
	CodexVersion    string                       `json:"codex_version,omitempty"`
	Transport       string                       `json:"transport"`
	Generation      int                          `json:"generation"`
	Account         RuntimeAccount               `json:"account"`
	RateLimits      RuntimeRateLimits            `json:"rate_limits"`
	Usage           RuntimeUsage                 `json:"usage"`
	Model           RuntimeModel                 `json:"model"`
	Config          RuntimeConfig                `json:"config"`
	Features        []RuntimeFeature             `json:"features,omitempty"`
	WorkspaceNotice []string                     `json:"workspace_messages,omitempty"`
	MCPServers      []RuntimeMCPServer           `json:"mcp_servers,omitempty"`
	Capabilities    *SanitizedCapabilitySnapshot `json:"capabilities,omitempty"`
}

// RuntimeAccount describes the authenticated account without identity data.
type RuntimeAccount struct {
	Kind string `json:"kind,omitempty"`
	Plan string `json:"plan,omitempty"`
}

// RuntimeWindow describes one bounded rate-limit window.
type RuntimeWindow struct {
	UsedPercent int   `json:"used_percent"`
	ResetsAt    int64 `json:"resets_at,omitempty"`
	WindowMins  int   `json:"window_minutes,omitempty"`
}

// RuntimeRateLimits contains the primary and secondary account windows.
type RuntimeRateLimits struct {
	Primary   RuntimeWindow `json:"primary"`
	Secondary RuntimeWindow `json:"secondary"`
}

// RuntimeUsage contains aggregate token and context-window counts.
type RuntimeUsage struct {
	Tokens        int `json:"tokens,omitempty"`
	ContextWindow int `json:"context_window,omitempty"`
}

// RuntimeModel describes the active model and its advertised constraints.
type RuntimeModel struct {
	ID                  string   `json:"id,omitempty"`
	ContextWindow       int      `json:"context_window,omitempty"`
	ReasoningEfforts    []string `json:"reasoning_efforts,omitempty"`
	InputModalities     []string `json:"input_modalities,omitempty"`
	SupportsPersonality bool     `json:"supports_personality,omitempty"`
}

// RuntimeConfig describes the effective runtime policy and its provenance.
type RuntimeConfig struct {
	ApprovalPolicy string `json:"approval_policy,omitempty"`
	SandboxMode    string `json:"sandbox_mode,omitempty"`
	Provenance     string `json:"provenance"`
}

// RuntimeFeature is one bounded experimental-feature status entry.
type RuntimeFeature struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	Source  string `json:"source,omitempty"`
}

// RuntimeMCPServer is a sanitized MCP server status entry.
type RuntimeMCPServer struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type runtimeState struct {
	account         RuntimeAccount
	rates           RuntimeRateLimits
	usage           RuntimeUsage
	workspaceNotice []string
	mcp             map[string]RuntimeMCPServer
	features        map[string]RuntimeFeature
}

// RuntimeSnapshot returns a deep, deterministic copy of current provider
// state. All lists are sorted and bounded.
func (p *Provider) RuntimeSnapshot() RuntimeSnapshot {
	p.mu.Lock()
	generation := p.generation
	version := p.version
	transport := p.cfg.Transport
	modelID := p.cfg.Model
	models := append([]modelRecord(nil), p.models...)
	var caps *SanitizedCapabilitySnapshot
	if p.eng != nil && p.eng.capabilities != nil {
		snapshot := p.eng.capabilities.Snapshot().Sanitized()
		caps = &snapshot
	}
	p.mu.Unlock()
	if transport == "" {
		transport = TransportStdio
	}

	p.runtimeMu.RLock()
	out := RuntimeSnapshot{
		CodexVersion: version, Transport: string(transport), Generation: generation,
		Account: p.runtime.account, RateLimits: p.runtime.rates, Usage: p.runtime.usage,
		Config:          RuntimeConfig{ApprovalPolicy: p.cfg.ApprovalPolicy, SandboxMode: p.cfg.SandboxMode, Provenance: "daemon"},
		WorkspaceNotice: append([]string(nil), p.runtime.workspaceNotice...), Capabilities: caps,
	}
	for _, server := range p.runtime.mcp {
		out.MCPServers = append(out.MCPServers, server)
	}
	for _, feature := range p.runtime.features {
		out.Features = append(out.Features, feature)
	}
	p.runtimeMu.RUnlock()
	sort.Slice(out.MCPServers, func(i, j int) bool { return out.MCPServers[i].Name < out.MCPServers[j].Name })
	if len(out.MCPServers) > maxRuntimeMCPServers {
		out.MCPServers = out.MCPServers[:maxRuntimeMCPServers]
	}
	sort.Slice(out.Features, func(i, j int) bool { return out.Features[i].ID < out.Features[j].ID })
	if len(out.WorkspaceNotice) > 16 {
		out.WorkspaceNotice = out.WorkspaceNotice[:16]
	}

	for _, model := range models {
		if model.ID != modelID && !(modelID == "" && model.IsDefault) {
			continue
		}
		out.Model.ID = model.ID
		out.Model.ContextWindow = out.Usage.ContextWindow
		out.Model.InputModalities = append([]string(nil), model.InputModalities...)
		out.Model.SupportsPersonality = model.SupportsPersonality
		for _, effort := range model.SupportedReasoningEfforts {
			out.Model.ReasoningEfforts = append(out.Model.ReasoningEfforts, effort.ReasoningEffort)
		}
		break
	}
	return out
}

func (p *Provider) noteRuntimeProviderNotification(method string, params json.RawMessage) {
	p.runtimeMu.Lock()
	defer p.runtimeMu.Unlock()
	if p.runtime.mcp == nil {
		p.runtime.mcp = make(map[string]RuntimeMCPServer)
	}
	if p.runtime.features == nil {
		p.runtime.features = make(map[string]RuntimeFeature)
	}
	switch method {
	case "account/updated":
		var body struct {
			AuthMode string `json:"authMode"`
			PlanType string `json:"planType"`
			Account  struct {
				Type string `json:"type"`
				Plan string `json:"planType"`
			} `json:"account"`
		}
		if json.Unmarshal(params, &body) == nil {
			p.runtime.account = RuntimeAccount{Kind: clipRuntime(firstNonEmpty(body.AuthMode, body.Account.Type)), Plan: clipRuntime(firstNonEmpty(body.PlanType, body.Account.Plan))}
		}
	case "account/rateLimits/updated":
		var body struct {
			RateLimits struct {
				Primary, Secondary *struct {
					UsedPercent int   `json:"usedPercent"`
					ResetsAt    int64 `json:"resetsAt"`
					WindowMins  int   `json:"windowDurationMins"`
				}
			} `json:"rateLimits"`
		}
		if json.Unmarshal(params, &body) == nil {
			if body.RateLimits.Primary != nil {
				p.runtime.rates.Primary = RuntimeWindow{UsedPercent: body.RateLimits.Primary.UsedPercent, ResetsAt: body.RateLimits.Primary.ResetsAt, WindowMins: body.RateLimits.Primary.WindowMins}
			}
			if body.RateLimits.Secondary != nil {
				p.runtime.rates.Secondary = RuntimeWindow{UsedPercent: body.RateLimits.Secondary.UsedPercent, ResetsAt: body.RateLimits.Secondary.ResetsAt, WindowMins: body.RateLimits.Secondary.WindowMins}
			}
		}
	case "mcpServer/startupStatus/updated":
		var body struct{ Name, Status, Error string }
		if json.Unmarshal(params, &body) == nil && strings.TrimSpace(body.Name) != "" {
			p.runtime.mcp[clipRuntime(body.Name)] = RuntimeMCPServer{Name: clipRuntime(body.Name), Status: clipRuntime(body.Status), Error: clipRuntime(body.Error)}
		}
	}
}

// RefreshRuntime reads provider-global catalogs that are not pushed on every
// connection. Each request is independently capability-gated and best effort.
func (p *Provider) RefreshRuntime(ctx context.Context) error {
	if _, err := p.ensureEngine(ctx); err != nil {
		return err
	}
	framer := p.framer()
	if framer == nil {
		return errors.New("Codex engine unavailable")
	}
	read := func(capability CapabilityID, method string, params any, apply func(json.RawMessage)) {
		if !p.supportsCapability(capability) {
			return
		}
		raw, err := framer.sendReadOnlyRequest(ctx, method, params)
		if err == nil {
			apply(raw)
		}
	}
	read(CapabilityAccountRead, "account/read", map[string]any{"refreshToken": false}, p.applyAccountRead)
	read(CapabilityAccountRateLimits, "account/rateLimits/read", map[string]any{}, p.applyRateLimitsRead)
	read(CapabilityAccountUsage, "account/usage/read", map[string]any{}, p.applyAccountUsageRead)
	read(CapabilityWorkspaceMessages, "account/workspaceMessages/read", map[string]any{}, p.applyWorkspaceMessagesRead)
	read(CapabilityExperimentalFeature, "experimentalFeature/list", map[string]any{"limit": 100}, p.applyFeatureList)
	read(CapabilityMCPServerStatus, "mcpServerStatus/list", map[string]any{"limit": maxRuntimeMCPServers, "detail": "toolsAndAuthOnly"}, p.applyMCPStatusList)
	return nil
}

func (p *Provider) applyAccountRead(raw json.RawMessage) {
	var body struct {
		Account *struct {
			Type string `json:"type"`
			Plan string `json:"planType"`
		} `json:"account"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Account == nil {
		return
	}
	p.runtimeMu.Lock()
	p.runtime.account = RuntimeAccount{Kind: clipRuntime(body.Account.Type), Plan: clipRuntime(body.Account.Plan)}
	p.runtimeMu.Unlock()
}

func (p *Provider) applyRateLimitsRead(raw json.RawMessage) {
	var body struct {
		RateLimits struct {
			Primary, Secondary *struct {
				UsedPercent int   `json:"usedPercent"`
				ResetsAt    int64 `json:"resetsAt"`
				WindowMins  int   `json:"windowDurationMins"`
			}
		} `json:"rateLimits"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return
	}
	p.runtimeMu.Lock()
	defer p.runtimeMu.Unlock()
	if body.RateLimits.Primary != nil {
		p.runtime.rates.Primary = RuntimeWindow{UsedPercent: body.RateLimits.Primary.UsedPercent, ResetsAt: body.RateLimits.Primary.ResetsAt, WindowMins: body.RateLimits.Primary.WindowMins}
	}
	if body.RateLimits.Secondary != nil {
		p.runtime.rates.Secondary = RuntimeWindow{UsedPercent: body.RateLimits.Secondary.UsedPercent, ResetsAt: body.RateLimits.Secondary.ResetsAt, WindowMins: body.RateLimits.Secondary.WindowMins}
	}
}

func (p *Provider) applyAccountUsageRead(raw json.RawMessage) {
	var body struct {
		Summary struct {
			LifetimeTokens *int `json:"lifetimeTokens"`
		} `json:"summary"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Summary.LifetimeTokens == nil {
		return
	}
	p.runtimeMu.Lock()
	p.runtime.usage.Tokens = *body.Summary.LifetimeTokens
	p.runtimeMu.Unlock()
}

func (p *Provider) applyWorkspaceMessagesRead(raw json.RawMessage) {
	var body struct {
		Messages []struct {
			Body string `json:"messageBody"`
		} `json:"messages"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return
	}
	messages := make([]string, 0, min(len(body.Messages), 16))
	for _, message := range body.Messages {
		messages = append(messages, clipRuntime(message.Body))
	}
	p.runtimeMu.Lock()
	p.runtime.workspaceNotice = messages
	p.runtimeMu.Unlock()
}

func (p *Provider) applyFeatureList(raw json.RawMessage) {
	var body struct {
		Data []struct {
			Name    string `json:"name"`
			Stage   string `json:"stage"`
			Enabled bool   `json:"enabled"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return
	}
	features := make(map[string]RuntimeFeature)
	for _, feature := range body.Data {
		if len(features) >= 100 {
			break
		}
		id := clipRuntime(feature.Name)
		if id != "" {
			features[id] = RuntimeFeature{ID: id, Enabled: feature.Enabled, Source: clipRuntime(feature.Stage)}
		}
	}
	p.runtimeMu.Lock()
	p.runtime.features = features
	p.runtimeMu.Unlock()
}

func (p *Provider) applyMCPStatusList(raw json.RawMessage) {
	var body struct {
		Data []struct {
			Name       string `json:"name"`
			AuthStatus string `json:"authStatus"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return
	}
	servers := make(map[string]RuntimeMCPServer)
	for _, server := range body.Data {
		if len(servers) >= maxRuntimeMCPServers {
			break
		}
		name := clipRuntime(server.Name)
		if name != "" {
			servers[name] = RuntimeMCPServer{Name: name, Status: clipRuntime(server.AuthStatus)}
		}
	}
	p.runtimeMu.Lock()
	p.runtime.mcp = servers
	p.runtimeMu.Unlock()
}

func (p *Provider) noteRuntimeUsage(tokens, contextWindow int) {
	p.runtimeMu.Lock()
	p.runtime.usage = RuntimeUsage{Tokens: tokens, ContextWindow: contextWindow}
	p.runtimeMu.Unlock()
}

func clipRuntime(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 512 {
		return s[:512]
	}
	return s
}

func (s *session) RuntimeStatus(context.Context) (string, error) {
	if s.p == nil {
		return "Codex runtime status is unavailable.", nil
	}
	runtime := s.p.RuntimeSnapshot()
	message := fmt.Sprintf("Codex %s · transport %s · generation %d", firstNonEmpty(runtime.CodexVersion, "unknown"), runtime.Transport, runtime.Generation)
	if runtime.Account.Plan != "" {
		message += " · plan " + runtime.Account.Plan
	}
	if runtime.Model.ID != "" {
		message += " · model " + runtime.Model.ID
	}
	if len(runtime.MCPServers) > 0 {
		message += fmt.Sprintf(" · MCP %d server(s)", len(runtime.MCPServers))
	}
	return message, nil
}

func (s *session) RuntimeUsage(context.Context) (string, error) {
	if s.p == nil {
		return "Codex usage is unavailable.", nil
	}
	runtime := s.p.RuntimeSnapshot()
	message := fmt.Sprintf("Usage: %d tokens", runtime.Usage.Tokens)
	if runtime.Usage.ContextWindow > 0 {
		message += fmt.Sprintf(" of %d context", runtime.Usage.ContextWindow)
	}
	if used := runtime.RateLimits.Primary.UsedPercent; used > 0 {
		message += fmt.Sprintf(" · primary rate window %d%% used", used)
	}
	if runtime.Account.Plan != "" {
		message += " · plan " + runtime.Account.Plan
	}
	return message, nil
}

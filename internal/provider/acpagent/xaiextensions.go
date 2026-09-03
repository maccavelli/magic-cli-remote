package acpagent

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// HandleXAIMCPServersUpdated handles grok's `_x.ai/mcp/servers_updated`.
//
// It carries the engine's full current MCP membership — name, transport and
// URL per server — which is the list `_x.ai/mcp/server_status` reports state
// against. Unrouted, mcremote's server list came only from its own config, so
// a server grok added or dropped at runtime was invisible in /status and any
// status frame naming it created a phantom entry (MADR 0137 F11).
//
// Membership only; state is not invented here. A server appearing in this list
// with no status frame yet is recorded as "connecting", not "ready" — claiming
// a server is up because it was mentioned is exactly the guess this daemon's
// status output must not make.
func HandleXAIMCPServersUpdated(_ context.Context, s *session, params json.RawMessage) {
	var p struct {
		MCPServers []struct {
			Name string `json:"name"`
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		if s.log != nil {
			s.log.Debug("x.ai mcp/servers_updated: parse failed",
				slog.String("err", err.Error()))
		}
		return
	}
	if len(p.MCPServers) == 0 {
		return
	}
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	known := make(map[string]struct{}, len(s.mcpStatus))
	for _, st := range s.mcpStatus {
		known[st.Name] = struct{}{}
	}
	for _, srv := range p.MCPServers {
		if srv.Name == "" {
			continue
		}
		if _, ok := known[srv.Name]; ok {
			continue
		}
		s.mcpStatus = append(s.mcpStatus, provider.MCPServerStatus{
			Name:  srv.Name,
			State: "connecting",
		})
	}
}

// HandleXAIMCPInitProgress handles grok's `_x.ai/mcp/init_progress`.
//
// `{total, connected, sessionId}` — how many MCP servers the engine is waiting
// on before it can serve a turn. This bears directly on first-token latency:
// MCP startup is work that happens between the prompt and the first token, and
// unrouted it was invisible, so a session waiting on a slow MCP server looked
// identical to a slow model.
//
// Logged, not emitted as a transcript event. A progress counter is diagnostic
// detail — it belongs where an operator investigating latency will find it, not
// in a conversation. Promoting it to a phone-visible event is a protocol and UI
// decision that needs its own record.
func HandleXAIMCPInitProgress(_ context.Context, s *session, params json.RawMessage) {
	var p struct {
		Total     int    `json:"total"`
		Connected int    `json:"connected"`
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(params, &p); err != nil || s.log == nil {
		return
	}
	// Origin rule, as everywhere else on this channel: a child session's
	// progress is not this conversation's.
	if p.SessionID != "" {
		if live := s.AgentSessionID(); live != "" && p.SessionID != live {
			return
		}
	}
	s.log.Info("grok mcp startup progress",
		slog.Int("connected", p.Connected),
		slog.Int("total", p.Total))
}

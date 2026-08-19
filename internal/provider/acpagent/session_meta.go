package acpagent

import (
	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// attachSessionMeta returns meta or nil when empty so callers omit `_meta`.
func attachSessionMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func (p *Provider) sessionOpenMeta(opts provider.StartOptions) map[string]any {
	if p.spec.SessionMeta == nil {
		return nil
	}
	return attachSessionMeta(p.spec.SessionMeta(opts, p.cfg))
}

func sessionOpenNew(cwd string, mcp []acp.McpServer, meta map[string]any) acp.NewSessionRequest {
	return acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: mcp,
		Meta:       attachSessionMeta(meta),
	}
}

// applyHarvestedMeta overwrites currentModelID / thinkingLevel from grok's
// session/new|load `_meta` when those keys are present. Caller holds s.mu.
func applyHarvestedMeta(s *session, meta map[string]any) {
	if model := appliedModelFromMeta(meta); model != "" {
		s.currentModelID = model
	}
	if effort := appliedEffortFromMeta(meta); effort != "" {
		s.thinkingLevel = effort
	}
}

func appliedModelFromMeta(meta map[string]any) string {
	if det := nestedMap(meta, "x.ai/sessionDetail"); det != nil {
		if id, _ := det["currentModelId"].(string); id != "" {
			return id
		}
	}
	return selectedConfigID(meta, "model")
}

func appliedEffortFromMeta(meta map[string]any) string {
	return selectedConfigID(meta, "mode")
}

func selectedConfigID(meta map[string]any, category string) string {
	cfg := nestedMap(meta, "x.ai/sessionConfig")
	if cfg == nil {
		return ""
	}
	opts, _ := cfg["options"].([]any)
	for _, raw := range opts {
		o, _ := raw.(map[string]any)
		if o == nil {
			continue
		}
		cat, _ := o["category"].(string)
		sel, _ := o["selected"].(bool)
		if cat == category && sel {
			id, _ := o["id"].(string)
			if id != "" {
				return id
			}
		}
	}
	return ""
}

func nestedMap(parent map[string]any, key string) map[string]any {
	if parent == nil {
		return nil
	}
	m, _ := parent[key].(map[string]any)
	return m
}

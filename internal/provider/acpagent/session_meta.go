package acpagent

import (
	acp "github.com/maccavelli/acp-go-sdk2"
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

// applySessionState records what an agent reports at session/new or
// session/load: its config options, and from them its current model and
// thinking level. A standard `model` or `thought_level` config option wins;
// only for a setting the agent offers no option for is its `_meta` read,
// through the Spec's HarvestSessionMeta (MADR 0184 D6). Caller holds s.mu.
func (p *Provider) applySessionState(s *session, meta map[string]any, opts []acp.SessionConfigOption) {
	modelOK, effortOK := s.recordConfigOptions(opts)
	if (modelOK && effortOK) || p.spec.HarvestSessionMeta == nil {
		return
	}
	model, effort := p.spec.HarvestSessionMeta(meta)
	if !modelOK && model != "" {
		s.currentModelID = model
	}
	if !effortOK && effort != "" {
		s.thinkingLevel = effort
	}
}

// recordConfigOptions stores the agent's full set of config options and takes
// the current model and thinking level from its standard `model` and
// `thought_level` options, reporting which of the two it found. Caller holds
// s.mu.
func (s *session) recordConfigOptions(opts []acp.SessionConfigOption) (model, effort bool) {
	s.configOptions = opts
	if _, v, ok := selectOption(opts, acp.SessionConfigOptionCategoryModel); ok {
		model = true
		if v != "" {
			s.currentModelID = v
		}
	}
	if _, v, ok := selectOption(opts, acp.SessionConfigOptionCategoryThoughtLevel); ok {
		effort = true
		if v != "" {
			s.thinkingLevel = v
		}
	}
	return model, effort
}

// selectOption returns the id and current value of the first select config
// option in category, and false when opts has none.
func selectOption(opts []acp.SessionConfigOption, category acp.SessionConfigOptionCategory) (id, current string, ok bool) {
	sel, ok := acp.ConfigOptionByCategory(opts, category)
	if !ok {
		return "", "", false
	}
	return string(sel.Id), string(sel.CurrentValue), true
}

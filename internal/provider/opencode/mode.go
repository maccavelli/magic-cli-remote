package opencode

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// OpenCode has no plan *command* and no ACP-style mode list: plan mode is the
// built-in `plan` agent ("Plan mode. Disallows all edit tools."), and the agent
// is chosen per message on prompt_async. So a session's switchable modes are its
// primary agents, and switching one means rewriting the agent the next prompt
// carries (MADR 0022).

// agentInfo is one entry of OpenCode's GET /agent catalog.
type agentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Mode        string `json:"mode"`
	Native      bool   `json:"native"`
	Hidden      bool   `json:"hidden"`
}

// fetchAgents GETs the engine agent catalog. query is an optional
// project-scoping suffix (see httpSession.dir).
func fetchAgents(ctx context.Context, api httpagent.API, query string) ([]agentInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var agents []agentInfo
	if err := api(ctx, "GET", "/agent"+query, nil, &agents); err != nil {
		return nil, err
	}
	return agents, nil
}

// visible drops agents the engine marks hidden — its internal compaction,
// summary and title agents, which are reported as primary and would otherwise
// show up as pickable agents and switchable modes.
func (a agentInfo) visible() bool {
	return !a.Hidden && strings.TrimSpace(a.Name) != ""
}

// modesFromAgents maps primary agents to session modes, preserving catalog
// order. Subagents are excluded: they cannot run a user turn.
func modesFromAgents(agents []agentInfo) []event.SessionMode {
	modes := make([]event.SessionMode, 0, len(agents))
	for _, a := range agents {
		if !a.visible() || (a.Mode != "" && a.Mode != "primary" && a.Mode != "all") {
			continue
		}
		modes = append(modes, event.SessionMode{
			ID:          a.Name,
			Name:        a.Name,
			Description: strings.TrimSpace(a.Description),
		})
	}
	return modes
}

// staticModes mirrors [httpDialect.StaticAgents] for the offline path so the
// mode strip and /plan keep working when the engine catalog is unreachable.
func staticModes() []event.SessionMode {
	return []event.SessionMode{
		{ID: "build", Name: "build", Description: "Default agent"},
		{ID: "plan", Name: "plan", Description: "Plan mode (no edits)"},
	}
}

// sessionModes returns this session's switchable modes and the id currently in
// effect. Live catalog first, static fallback on any engine trouble.
func (o *httpSession) sessionModes(ctx context.Context) (modes []event.SessionMode, currentID string) {
	agents, err := fetchAgents(ctx, o.h.API(), o.dir())
	if err != nil {
		o.h.Log().Debug("list agents for modes failed; static modes",
			slog.String("err", err.Error()))
		modes = staticModes()
	} else if modes = modesFromAgents(agents); len(modes) == 0 {
		modes = staticModes()
	}
	return modes, o.currentMode(modes)
}

// currentMode is the session's agent, or the default the engine would pick when
// no agent was requested ("build", else the first mode).
func (o *httpSession) currentMode(modes []event.SessionMode) string {
	if cur := strings.TrimSpace(o.h.Agent()); cur != "" {
		return cur
	}
	for _, m := range modes {
		if m.ID == "build" {
			return m.ID
		}
	}
	if len(modes) > 0 {
		return modes[0].ID
	}
	return ""
}

// advertiseModes emits the session's mode list so clients can offer the switcher
// and the daemon can route /plan. Called at create and resume alongside
// [httpSession.advertiseCommands].
func (o *httpSession) advertiseModes(ctx context.Context) {
	modes, current := o.sessionModes(ctx)
	o.h.Emit(event.Event{
		Type:          event.TypeMode,
		Modes:         modes,
		CurrentModeID: current,
	})
}

// SetMode implements the httpagent dialect-mode hook: point subsequent prompts
// at the agent named by modeID. The engine binds the agent per message, so no
// server-side call is needed — and none is possible mid-turn either, which is
// why the switch is documented as taking effect on the next prompt.
func (o *httpSession) SetMode(ctx context.Context, modeID string) (string, error) {
	modeID = strings.TrimSpace(modeID)
	if modeID == "" {
		return "", fmt.Errorf("empty mode id")
	}
	modes, _ := o.sessionModes(ctx)
	for _, m := range modes {
		if strings.EqualFold(m.ID, modeID) {
			o.h.SetAgent(m.ID)
			o.h.Log().Info(
				"opencode mode switch",
				slog.String("agent_session_id", o.h.AgentSessionID()),
				slog.String("agent", m.ID),
			)
			return m.ID, nil
		}
	}
	return "", fmt.Errorf("unknown mode %q", modeID)
}

// Compile-time check for the transport's optional dialect-mode hook
// (httpagent.dialectMode is unexported, so mirror its shape here).
var _ interface {
	SetMode(ctx context.Context, modeID string) (string, error)
} = (*httpSession)(nil)

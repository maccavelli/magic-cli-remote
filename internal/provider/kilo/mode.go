package kilo

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// Kilo, like the OpenCode it forks, has no ACP-style mode list: the agent is
// chosen per message on prompt_async, so a session's switchable modes are its
// primary agents (code/ask/debug/plan/orchestrator) and switching one means
// rewriting the agent the next prompt carries (MADR 0022 pattern; MADR 0075
// §2.8 — the "mode plumbing" here IS the agent picker, plan P3.3).

// agentInfo is one entry of Kilo's GET /agent catalog (same shape as OpenCode).
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

// startable reports whether OpenCode permits this agent to receive a
// top-level user prompt. Subagents are delegated engine work, not alternate
// chat modes; hidden agents are engine internals. An absent mode predates the
// explicit catalog classification and is treated as primary.
func (a agentInfo) startable() bool {
	if !a.visible() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(a.Mode)) {
	case "", "primary", "all":
		return true
	default:
		return false
	}
}

// modesFromAgents maps primary agents to session modes, preserving catalog
// order. Subagents are excluded: they cannot run a user turn.
func modesFromAgents(agents []agentInfo) []event.SessionMode {
	modes := make([]event.SessionMode, 0, len(agents))
	for _, a := range agents {
		if !a.startable() {
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

// ValidateStartAgent implements httpagent.StartAgentValidator. This is a
// server-side enforcement point, not merely picker hygiene: a raw
// session.create request must not be able to select an OpenCode subagent.
func (d *httpDialect) ValidateStartAgent(ctx context.Context, api httpagent.API, cwd, agent string) (string, error) {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return "", nil
	}
	query := "?directory=" + url.QueryEscape(cwd)
	agents, err := fetchAgents(ctx, api, query)
	if err != nil {
		return "", fmt.Errorf("validate kilo agent: %w", err)
	}
	for _, candidate := range agents {
		if strings.EqualFold(candidate.Name, agent) {
			if !candidate.startable() {
				return "", fmt.Errorf("%w: %q is not a top-level Kilo agent", provider.ErrInvalidAgent, agent)
			}
			return candidate.Name, nil
		}
	}
	return "", fmt.Errorf("%w: Kilo agent %q was not found", provider.ErrInvalidAgent, agent)
}

// autoModeID is a synthetic mode. OpenCode has no auto-approve agent — its
// own --auto flag is a client-side auto-responder that never reaches the
// server — so this id maps to the normal agent plus daemon-side permission
// auto-approval (MADR 0044 D3).
const autoModeID = "auto"

// autoMode is appended last to every advertised mode list. Ordering is
// load-bearing: session.defaultMode resolves "return to normal" as `code`
// (kilo's default agent, MADR 0075 §2.8), else the first non-plan mode
// advertised, so putting auto anywhere earlier would let `/plan off` drop
// the user into auto-approve.
func autoMode() event.SessionMode {
	return event.SessionMode{
		ID:          autoModeID,
		Name:        autoModeID,
		Description: "Auto-approve — permissions answered automatically (dangerous)",
		Dangerous:   true,
	}
}

// withAutoMode appends the synthetic auto mode to an agent-derived list.
func withAutoMode(modes []event.SessionMode) []event.SessionMode {
	return append(modes, autoMode())
}

// normalAgentID is the agent auto mode runs under: `code` when advertised
// (Kilo's default full agent — it has no `build`), else the first mode that is
// neither plan nor auto. Mirrors the resolution in session.defaultMode so the
// two cannot disagree.
func normalAgentID(modes []event.SessionMode) string {
	for _, m := range modes {
		if strings.EqualFold(m.ID, "code") {
			return m.ID
		}
	}
	for _, m := range modes {
		if !strings.EqualFold(m.ID, "plan") && !strings.EqualFold(m.ID, autoModeID) {
			return m.ID
		}
	}
	return ""
}

// staticModes mirrors [httpDialect.StaticAgents] for the offline path so the
// mode strip and /plan keep working when the engine catalog is unreachable.
func staticModes() []event.SessionMode {
	return withAutoMode([]event.SessionMode{
		{ID: "code", Name: "code", Description: "Default full agent"},
		{ID: "ask", Name: "ask", Description: "Q&A, no edits"},
		{ID: "debug", Name: "debug", Description: "Diagnostics"},
		{ID: "plan", Name: "plan", Description: "Plan files only"},
		{ID: "orchestrator", Name: "orchestrator", Description: "Multi-agent coordination"},
	})
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
	} else {
		modes = withAutoMode(modes)
	}
	return modes, o.currentMode(modes)
}

// currentMode is the mode in effect: auto when armed, else the session's agent,
// else the default the engine would pick when no agent was requested ("build",
// else the first mode).
//
// The auto check comes first and must stay first. Auto-approve is not an agent,
// so reporting the agent while it is armed would leave the phone showing
// "build" on a session that silently answers every permission — the worst
// possible failure for this feature (MADR 0044 D2).
func (o *httpSession) currentMode(modes []event.SessionMode) string {
	if o.h.AutoApprove() {
		return autoModeID
	}
	if cur := strings.TrimSpace(o.h.Agent()); cur != "" {
		return cur
	}
	for _, m := range modes {
		if m.ID == "code" {
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

	// Auto is intercepted *before* the agent loop. It is in the advertised
	// list, so the loop below would otherwise match it and point prompts at an
	// agent named "auto", which does not exist (MADR 0044 D2).
	if strings.EqualFold(modeID, autoModeID) {
		o.h.SetAgent(normalAgentID(modes))
		o.h.SetAutoApprove(true)
		o.h.Log().Warn("kilo auto-approve armed",
			slog.String("agent_session_id", o.h.AgentSessionID()),
			slog.String("agent", o.h.Agent()),
		)
		o.sweepPendingPermissions(ctx)
		return autoModeID, nil
	}

	for _, m := range modes {
		if strings.EqualFold(m.ID, modeID) {
			// Any other mode disarms auto-approve: the menu is single-select
			// and must stay honest about what is running. Close the approval
			// card too — nothing further will join it (MADR 0051).
			o.h.SetAutoApprove(false)
			o.finishApprovals()
			o.h.SetAgent(m.ID)
			o.h.Log().Info(
				"kilo mode switch",
				slog.String("agent_session_id", o.h.AgentSessionID()),
				slog.String("agent", m.ID),
			)
			return m.ID, nil
		}
	}
	return "", fmt.Errorf("unknown mode %q", modeID)
}

// sweepPendingPermissions answers permissions that were already waiting when
// auto-approve was armed. Without it, arming auto to unblock an agent that is
// already stuck — the most likely reason to reach for it — does nothing until
// the agent happens to ask again (MADR 0044 D4.5).
//
// It does not emit permission_resolved: autoApprove answers through the
// transport's RespondPermission, which already emits exactly one per id.
// Emitting here too would double-resolve every swept permission.
func (o *httpSession) sweepPendingPermissions(ctx context.Context) {
	// Prefer the engine's list: it carries the name and patterns the audit
	// notice needs. Fall back to the host's bare id set if the call fails.
	if asks, err := o.listPendingPermissions(ctx); err == nil {
		for _, p := range asks {
			o.autoApprove(p)
		}
		return
	}
	for _, id := range o.h.PendingPermissions() {
		o.autoApprove(permAsk{ID: id})
	}
}

// listPendingPermissions fetches the engine's open permission requests for this
// session tree, normalized through the same dual-shape mapping the SSE path
// uses.
func (o *httpSession) listPendingPermissions(ctx context.Context) ([]permAsk, error) {
	var list []json.RawMessage
	if err := o.h.API()(ctx, "GET", "/permission"+o.dir(), nil, &list); err != nil {
		return nil, err
	}
	tracked := make(map[string]struct{}, len(o.h.PendingPermissions()))
	for _, id := range o.h.PendingPermissions() {
		tracked[id] = struct{}{}
	}
	out := make([]permAsk, 0, len(list))
	for _, raw := range list {
		p, ok := normalizePermissionAsk(raw)
		if !ok {
			continue
		}
		// Only sweep what this session actually surfaced: the engine is shared
		// across sessions, and answering another session's permission would be
		// a cross-session action the user never authorised.
		if _, ours := tracked[p.ID]; !ours {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// Compile-time check for the transport's optional dialect-mode hook
// (httpagent.dialectMode is unexported, so mirror its shape here).
var _ interface {
	SetMode(ctx context.Context, modeID string) (string, error)
} = (*httpSession)(nil)

package session

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// BuiltinCommand describes a daemon-handled slash command. Clients advertise
// these for autocomplete; the daemon interprets them in [Manager.Prompt].
type BuiltinCommand struct {
	Name        string
	Args        string // usage hint, e.g. "<name>"
	Description string
}

// BuiltinCommands is the fixed set the daemon interprets itself. Any /command
// not in this set is forwarded to the agent as a normal prompt, so agent-native
// commands keep working.
var BuiltinCommands = []BuiltinCommand{
	{Name: "model", Args: "[name]", Description: "Show or switch the agent model (restarts the agent)"},
	{Name: "reset", Args: "", Description: "Restart the agent with a fresh context"},
	{Name: "new", Args: "[name]", Description: "Start a new agent session"},
	{Name: "plan", Args: "[off]", Description: "Switch to plan mode; /plan off returns to the default mode"},
	{Name: "mode", Args: "[id]", Description: "Show or switch the agent's operating mode"},
	{Name: "help", Args: "", Description: "List available slash commands"},
}

func isBuiltinCommand(name string) bool {
	return slices.ContainsFunc(BuiltinCommands, func(c BuiltinCommand) bool {
		return c.Name == name
	})
}

// softBuiltins are built-ins an agent may legitimately own: the daemon maps
// them onto session modes, but a provider that advertises a command of the same
// name is the better authority, so for these — and only these — the agent wins
// (see [Manager.Prompt]).
var softBuiltins = []string{"plan", "mode"}

func isSoftBuiltin(name string) bool {
	return slices.Contains(softBuiltins, name)
}

// parseSlashCommand splits "/name the rest" into ("name", "the rest", true).
// Returns ok=false when text is not a slash command. The name is lower-cased so
// matching is case-insensitive; the remainder keeps its original casing.
func parseSlashCommand(text string) (name, rest string, ok bool) {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "/") {
		return "", "", false
	}
	t = strings.TrimPrefix(t, "/")
	if i := strings.IndexAny(t, " \t\n"); i >= 0 {
		return strings.ToLower(t[:i]), strings.TrimSpace(t[i+1:]), true
	}
	return strings.ToLower(t), "", true
}

// runBuiltin dispatches an already-recognised built-in command. The caller has
// authorized deviceID for the session.
func (m *Manager) runBuiltin(ctx context.Context, id, deviceID, name, rest string) error {
	switch name {
	case "help":
		m.emitNotice(id, m.helpText(id))
		return nil
	case "model":
		return m.cmdModel(ctx, id, deviceID, rest)
	case "reset":
		return m.cmdReset(ctx, id, deviceID)
	case "new":
		return m.cmdNew(ctx, id, deviceID, rest)
	case "plan":
		return m.cmdPlan(ctx, id, rest)
	case "mode":
		return m.cmdMode(ctx, id, rest)
	default:
		return nil
	}
}

// isCommandName reports whether s is a plausible slash-command name — a single
// [A-Za-z0-9][A-Za-z0-9_-]* token. Guards against treating a pasted path or
// snippet ("/etc/hosts", "/usr/bin") as a command so it is sent as a prompt.
func isCommandName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case i > 0 && (r == '_' || r == '-'):
		default:
			return false
		}
	}
	return true
}

// agentAdvertises reports whether the session's agent advertised a slash command
// with this name (case-insensitive) via ACP available_commands.
func (m *Manager) agentAdvertises(id, name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.sessions[id]
	if !ok {
		return false
	}
	for _, c := range e.agentCommands {
		if strings.EqualFold(c, name) {
			return true
		}
	}
	return false
}

// helpText renders /help for a session: the daemon built-ins, the agent's own
// modes and commands, and — on grok, whose TUI has many more — a note that its
// terminal-only commands are not reachable over the remote.
func (m *Manager) helpText(id string) string {
	m.mu.RLock()
	var agent []string
	var modes []event.SessionMode
	var current string
	var prov provider.ID
	if e, ok := m.sessions[id]; ok {
		agent = append(agent, e.agentCommands...)
		modes = slices.Clone(e.agentModes)
		current = e.currentModeID
		prov = e.meta.Provider
	}
	m.mu.RUnlock()

	parts := make([]string, 0, len(BuiltinCommands))
	for _, c := range BuiltinCommands {
		// Mode commands are only meaningful where the agent has modes.
		if isSoftBuiltin(c.Name) && len(modes) == 0 {
			continue
		}
		if c.Args != "" {
			parts = append(parts, "/"+c.Name+" "+c.Args)
		} else {
			parts = append(parts, "/"+c.Name)
		}
	}
	msg := "You can run: " + strings.Join(parts, ", ")

	if len(modes) > 0 {
		labels := make([]string, 0, len(modes))
		for _, mode := range modes {
			label := mode.ID
			if strings.EqualFold(mode.ID, current) {
				label += " (current)"
			}
			labels = append(labels, label)
		}
		msg += ". Modes: " + strings.Join(labels, ", ")
	}
	if len(agent) > 0 {
		for i := range agent {
			agent[i] = "/" + agent[i]
		}
		msg += ". From the agent: " + strings.Join(agent, ", ")
	}
	if prov == provider.IDGrok {
		msg += ". Grok's terminal-only commands (/context, /compact, /usage, …) " +
			"can't run over the remote."
	}
	return msg
}

// sessionModes snapshots the modes advertised for a session and the active id.
func (m *Manager) sessionModes(id string) (modes []event.SessionMode, current string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.sessions[id]
	if !ok {
		return nil, ""
	}
	return slices.Clone(e.agentModes), e.currentModeID
}

// findMode resolves a mode id case-insensitively.
func findMode(modes []event.SessionMode, id string) (event.SessionMode, bool) {
	for _, mode := range modes {
		if strings.EqualFold(mode.ID, id) {
			return mode, true
		}
	}
	return event.SessionMode{}, false
}

// defaultMode is the mode to return to when leaving plan mode: the id each
// provider treats as its normal working state (grok "default", OpenCode
// "build"), else the first non-plan mode advertised.
func defaultMode(modes []event.SessionMode) (event.SessionMode, bool) {
	for _, want := range []string{"default", "build"} {
		if mode, ok := findMode(modes, want); ok {
			return mode, true
		}
	}
	for _, mode := range modes {
		if !strings.EqualFold(mode.ID, "plan") {
			return mode, true
		}
	}
	return event.SessionMode{}, false
}

// cmdPlan enters plan mode, or returns to the default mode on "/plan off".
// Plan mode is a session mode on every provider that has one — grok reaches it
// over ACP session/set_mode, OpenCode by running prompts as its `plan` agent —
// so both are served by the same switch (MADR 0022).
func (m *Manager) cmdPlan(ctx context.Context, id, arg string) error {
	modes, current := m.sessionModes(id)
	plan, hasPlan := findMode(modes, "plan")
	if !hasPlan {
		m.emitNotice(id, "This agent doesn't offer a plan mode. "+
			"Type /help for what you can run from here.")
		return nil
	}

	target := plan
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "", "on":
	case "off", "exit", "stop":
		def, ok := defaultMode(modes)
		if !ok {
			m.emitNotice(id, "This agent has no other mode to return to.")
			return nil
		}
		target = def
	default:
		m.emitNotice(id, fmt.Sprintf(
			"Usage: /plan to plan without editing, /plan off to leave it. "+
				"Unrecognised argument %q.", arg,
		))
		return nil
	}

	if strings.EqualFold(target.ID, current) {
		if target.ID == plan.ID {
			m.emitNotice(id, "Already in plan mode. Use /plan off to leave it.")
		} else {
			m.emitNotice(id, fmt.Sprintf("Already in %s mode.", target.ID))
		}
		return nil
	}
	if err := m.setMode(ctx, id, target.ID); err != nil {
		m.emitNotice(id, fmt.Sprintf("Mode switch to %s failed: %v", target.ID, err))
		return err
	}
	if target.ID == plan.ID {
		m.emitNotice(id, "Plan mode on from your next message — the agent will "+
			"research and plan without editing. Use /plan off to leave it.")
		return nil
	}
	m.emitNotice(id, fmt.Sprintf("Plan mode off — back to %s from your next message.", target.ID))
	return nil
}

// cmdMode lists the agent's modes (no arg) or switches to one.
func (m *Manager) cmdMode(ctx context.Context, id, arg string) error {
	modes, current := m.sessionModes(id)
	if len(modes) == 0 {
		m.emitNotice(id, "This agent doesn't expose switchable modes.")
		return nil
	}
	arg = strings.TrimSpace(arg)
	if arg == "" {
		labels := make([]string, 0, len(modes))
		for _, mode := range modes {
			label := mode.ID
			if strings.EqualFold(mode.ID, current) {
				label += " (current)"
			}
			labels = append(labels, label)
		}
		m.emitNotice(id, fmt.Sprintf("Modes: %s · usage: /mode <id>",
			strings.Join(labels, ", ")))
		return nil
	}
	target, ok := findMode(modes, arg)
	if !ok {
		ids := make([]string, 0, len(modes))
		for _, mode := range modes {
			ids = append(ids, mode.ID)
		}
		m.emitNotice(id, fmt.Sprintf("Unknown mode %q. Available: %s.",
			arg, strings.Join(ids, ", ")))
		return nil
	}
	if strings.EqualFold(target.ID, current) {
		m.emitNotice(id, fmt.Sprintf("Already in %s mode.", target.ID))
		return nil
	}
	if err := m.setMode(ctx, id, target.ID); err != nil {
		m.emitNotice(id, fmt.Sprintf("Mode switch to %s failed: %v", target.ID, err))
		return err
	}
	m.emitNotice(id, fmt.Sprintf("Mode is now %s, from your next message.", target.ID))
	return nil
}

// cmdModel shows the current model (no arg) or switches it (with a name) by
// relaunching the session's agent with the new model.
func (m *Manager) cmdModel(ctx context.Context, id, deviceID, arg string) error {
	arg = strings.TrimSpace(arg)
	prov, cwd, name, owner, cur, ok := m.sessionRelaunchInfo(id)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotLive, id)
	}
	if arg == "" {
		label := cur
		if label == "" {
			label = "provider default"
		}
		m.emitNotice(id, fmt.Sprintf("Model: %s · usage: /model <name> to switch", label))
		return nil
	}
	if arg == cur {
		m.emitNotice(id, fmt.Sprintf("Already using model %s.", arg))
		return nil
	}
	m.emitNotice(id, fmt.Sprintf("Switching model to %s…", arg))
	if err := m.relaunch(ctx, id, prov, cwd, name, arg, owner); err != nil {
		// Relaunch is destroy-then-create: the old agent is already gone. Try
		// to bring the session back on the previous model rather than leaving
		// the user with a dead id and a vague notice.
		prevLabel := cur
		if prevLabel == "" {
			prevLabel = "the provider default"
		}
		if rerr := m.relaunch(ctx, id, prov, cwd, name, cur, owner); rerr == nil {
			m.emitNotice(id, fmt.Sprintf(
				"Model switch to %s failed: %v — the agent restarted on %s instead.",
				arg, err, prevLabel))
		} else {
			m.emitNotice(id, fmt.Sprintf(
				"Model switch failed and the session could not be restarted: %v. "+
					"This session is closed — create a new one to continue.", err))
		}
		return err
	}
	m.emitNotice(id, fmt.Sprintf("Model is now %s. The agent restarted with a fresh context.", arg))
	return nil
}

// cmdReset relaunches the agent with the same model, clearing its context.
func (m *Manager) cmdReset(ctx context.Context, id, deviceID string) error {
	prov, cwd, name, owner, cur, ok := m.sessionRelaunchInfo(id)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotLive, id)
	}
	m.emitNotice(id, "Restarting the agent…")
	if err := m.relaunch(ctx, id, prov, cwd, name, cur, owner); err != nil {
		// Relaunch is destroy-then-create: try once more (same as /model
		// recovery) so a transient Start failure does not leave a dead id.
		if rerr := m.relaunch(ctx, id, prov, cwd, name, cur, owner); rerr == nil {
			m.emitNotice(id, fmt.Sprintf(
				"Reset failed once (%v) — agent restarted on retry with a fresh context.", err))
			return nil
		}
		m.emitNotice(id, fmt.Sprintf(
			"Reset failed: %v. This session is closed — create a new one to continue.", err))
		return err
	}
	m.emitNotice(id, "Agent restarted with a fresh context.")
	return nil
}

// cmdNew starts a brand-new session (new id), inheriting the current session's
// provider, working directory, and model. It is owned by the requesting device.
func (m *Manager) cmdNew(ctx context.Context, id, deviceID, arg string) error {
	prov, cwd, _, _, model, ok := m.sessionRelaunchInfo(id)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotLive, id)
	}
	name := strings.TrimSpace(arg)
	meta, err := m.Create(ctx, prov, provider.StartOptions{CWD: cwd, Name: name, Model: model}, deviceID)
	if err != nil {
		m.emitNotice(id, fmt.Sprintf("New session failed: %v", err))
		return err
	}
	label := meta.Name
	if label == "" {
		label = meta.ID
	}
	m.emitNotice(id, fmt.Sprintf("Started new session %q — open it from the sessions list.", label))
	return nil
}

// sessionRelaunchInfo snapshots the fields needed to recreate a session under
// the same id. ok is false if the session is not live.
func (m *Manager) sessionRelaunchInfo(id string) (prov provider.ID, cwd, name, owner, model string, ok bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, live := m.sessions[id]
	if !live || e.dead {
		return "", "", "", "", "", false
	}
	return e.meta.Provider, e.meta.CWD, e.meta.Name, e.meta.OwnerDeviceID, e.meta.Model, true
}

// relaunch replaces the live session id with a fresh agent process, reusing the
// tested close-and-replace path in [Manager.Create]. The client keeps its own
// transcript; the server-side history ring is reset with the new process.
func (m *Manager) relaunch(ctx context.Context, id string, prov provider.ID, cwd, name, model, owner string) error {
	_, err := m.Create(ctx, prov, provider.StartOptions{
		LocalSessionID: id,
		CWD:            cwd,
		Name:           name,
		Model:          model,
	}, owner)
	return err
}

// emitEvent pushes a daemon-originated event to a session's clients and records
// it in the history ring (so a cold replay includes it), mirroring how
// [Manager.pump] handles provider events.
func (m *Manager) emitEvent(id string, ev event.Event) {
	m.mu.Lock()
	e, mine := m.sessions[id]
	if mine {
		e.appendHistoryLocked(&ev)
	}
	m.mu.Unlock()
	// A daemon-origin event that landed in a session's ring must also be marked
	// for durable persistence, exactly as [Manager.pump] does for provider
	// events — otherwise a crash loses /help output, notices, and echoed
	// commands from the on-disk transcript.
	if mine {
		m.scheduleHistoryPersist(id)
	}
	if m.onEvent != nil {
		m.onEvent(ev)
	}
}

// emitNotice pushes a daemon-originated informational line (e.g. command output).
func (m *Manager) emitNotice(id, text string) {
	m.emitEvent(id, event.Event{
		Type:      event.TypeNotice,
		SessionID: id,
		Timestamp: time.Now().UTC(),
		Text:      text,
	})
}

// echoUser mirrors a slash command back into the transcript as the user's own
// message, so a daemon-handled command visibly registers. Agent-forwarded
// prompts are echoed by the agent, so this is only for built-ins/unknowns.
func (m *Manager) echoUser(id, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	m.emitEvent(id, event.Event{
		Type:      event.TypeUserMessage,
		SessionID: id,
		Timestamp: time.Now().UTC(),
		Text:      text,
	})
}

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
	{Name: "help", Args: "", Description: "List available slash commands"},
}

func isBuiltinCommand(name string) bool {
	for _, c := range BuiltinCommands {
		if c.Name == name {
			return true
		}
	}
	return false
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

// helpText renders /help for a session: the daemon built-ins, any commands the
// agent advertised, and a note that grok's terminal-only commands are not
// reachable over the remote.
func (m *Manager) helpText(id string) string {
	parts := make([]string, 0, len(BuiltinCommands))
	for _, c := range BuiltinCommands {
		if c.Args != "" {
			parts = append(parts, "/"+c.Name+" "+c.Args)
		} else {
			parts = append(parts, "/"+c.Name)
		}
	}
	msg := "You can run: " + strings.Join(parts, ", ")

	m.mu.RLock()
	var agent []string
	if e, ok := m.sessions[id]; ok {
		agent = append(agent, e.agentCommands...)
	}
	m.mu.RUnlock()
	if len(agent) > 0 {
		for i := range agent {
			agent[i] = "/" + agent[i]
		}
		msg += ". From the agent: " + strings.Join(agent, ", ")
	}
	msg += ". Grok's terminal-only commands (/context, /compact, /usage, …) " +
		"can't run over the remote."
	return msg
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
		m.emitNotice(id, fmt.Sprintf("Model switch failed: %v", err))
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
		m.emitNotice(id, fmt.Sprintf("Reset failed: %v", err))
		return err
	}
	m.emitNotice(id, "Agent restarted with a fresh context.")
	return nil
}

// cmdNew starts a brand-new session (new id), inheriting the current session's
// provider and working directory. It is owned by the requesting device.
func (m *Manager) cmdNew(ctx context.Context, id, deviceID, arg string) error {
	prov, cwd, _, _, _, ok := m.sessionRelaunchInfo(id)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotLive, id)
	}
	name := strings.TrimSpace(arg)
	meta, err := m.Create(ctx, prov, provider.StartOptions{CWD: cwd, Name: name}, deviceID)
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
	if e, ok := m.sessions[id]; ok {
		e.history = append(e.history, ev)
		if len(e.history) > historyBufferCap {
			e.history = slices.Delete(e.history, 0, len(e.history)-historyBufferCap)
		}
	}
	m.mu.Unlock()
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

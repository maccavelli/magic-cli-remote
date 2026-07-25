package session

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

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

// resolveCommand resolves a typed command name against the canonical
// vocabulary and what this session offers. canonical is false for anything
// outside the vocabulary — those belong to the agent.
func (m *Manager) resolveCommand(id, name string) (res command.Resolution, canonical bool) {
	tbl, state, _ := m.commandContext(id)
	return command.Resolve(name, tbl, state)
}

// commandContext gathers everything command resolution needs for a session: the
// provider's declared table, what the session reports (advertised commands,
// modes, capabilities), and the provider's session-wide /help caveat.
func (m *Manager) commandContext(id string) (command.Table, command.SessionState, string) {
	m.mu.RLock()
	e, ok := m.sessions[id]
	if !ok {
		m.mu.RUnlock()
		return nil, command.SessionState{}, ""
	}
	state := command.SessionState{
		AgentCommands: slices.Clone(e.agentCommands),
		Modes:         slices.Clone(e.agentModes),
		Ops:           map[command.Op]bool{command.OpContext: e.lastUsage != nil},
	}
	sess, prov := e.sess, e.meta.Provider
	m.mu.RUnlock()

	// Capabilities come from the live session's optional interfaces, so a
	// provider cannot claim an op its transport does not implement.
	if sess != nil {
		_, state.Ops[command.OpCompact] = sess.(provider.CompactSession)
		_, state.Ops[command.OpSetModel] = sess.(provider.ModelSession)
		_, state.Ops[command.OpDiff] = sess.(provider.DiffSession)
		_, state.Ops[command.OpUndo] = sess.(provider.UndoSession)
		_, state.Ops[command.OpRedo] = sess.(provider.RevertSession)
	}

	var tbl command.Table
	var caveat string
	if p, err := m.provider(prov); err == nil {
		if t, ok := p.(command.Tabler); ok {
			tbl = t.CommandTable()
		}
		if c, ok := p.(command.Caveater); ok {
			caveat = c.CommandCaveat()
		}
	}
	return tbl, state, caveat
}

// advertiseCommands resolves the canonical vocabulary for a session and emits
// it to clients (remote_commands), so autocomplete and help offer exactly what
// works here. A no-op when the answer has not changed since the last emit —
// resolution is re-run on several triggers (commands advertised, modes arriving,
// the first usage report) that often resolve to the same list.
func (m *Manager) advertiseCommands(id string) {
	// Serialized, and the snapshot is taken inside the lock: see advertiseMu.
	m.advertiseMu.Lock()
	defer m.advertiseMu.Unlock()

	tbl, state, _ := m.commandContext(id)
	resolved := command.ResolveAll(tbl, state)
	list := make([]event.RemoteCommand, 0, len(resolved))
	for _, r := range resolved {
		list = append(list, event.RemoteCommand{
			Name:        r.Spec.Name,
			Hint:        r.Spec.Args,
			Description: r.Spec.Description,
			Available:   r.Available,
			Reason:      r.Reason(),
		})
	}

	m.mu.Lock()
	e, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	if slices.Equal(e.advertised, list) {
		m.mu.Unlock()
		return
	}
	e.advertised = list
	m.mu.Unlock()

	m.emitEvent(id, event.Event{
		Type:           event.TypeRemoteCommands,
		SessionID:      id,
		Timestamp:      time.Now().UTC(),
		RemoteCommands: list,
	})
}

// provider looks up a registered provider, tolerating a manager built without a
// registry (unit tests of the session bookkeeping itself).
func (m *Manager) provider(id provider.ID) (provider.Provider, error) {
	if m.reg == nil {
		return nil, fmt.Errorf("no provider registry")
	}
	return m.reg.Get(id)
}

// CanonicalCommandOptions renders the canonical vocabulary as picker options for
// the slash-command catalog (commands.list). With a live sessionID the options
// reflect that session's resolution; without one only the commands that work on
// every session of prov are enabled, since the rest depend on session state
// (modes, capabilities, whether usage has been reported).
func (m *Manager) CanonicalCommandOptions(sessionID string, prov provider.ID) []picker.Option {
	var tbl command.Table
	var state command.SessionState
	if _, err := m.liveSession(sessionID); err == nil {
		tbl, state, _ = m.commandContext(sessionID)
	} else if p, err := m.provider(prov); err == nil {
		if t, ok := p.(command.Tabler); ok {
			tbl = t.CommandTable()
		}
	}
	out := make([]picker.Option, 0, len(command.Specs))
	for _, r := range command.ResolveAll(tbl, state) {
		enabled := r.Available
		desc := r.Spec.Description
		if !enabled {
			desc = r.Spec.Description + " — " + r.Reason()
		}
		out = append(out, picker.Option{
			ID:          "/" + r.Spec.Name,
			Label:       r.Usage(),
			Description: desc,
			Group:       "remote",
			Enabled:     &enabled,
		})
	}
	return out
}

// runCanonical executes a resolved canonical command. The caller has authorized
// deviceID for the session.
//
// handled is true when the daemon dealt with the command itself. When it is
// false the agent owns this one and forward is the prompt text to send —
// the agent's own command name, which may differ from what the user typed.
func (m *Manager) runCanonical(ctx context.Context, id, deviceID string,
	res command.Resolution, typed, rest string,
) (handled bool, forward string, err error) {
	if !res.Available {
		m.echoUser(id, slashText(typed, rest))
		m.emitNotice(id, fmt.Sprintf("“/%s” isn't available here — %s. "+
			"Type /help for what you can run.", res.Spec.Name, res.Reason()))
		return true, "", nil
	}

	if res.Mapping.Kind == command.KindNative {
		native := res.Mapping.Native
		if strings.EqualFold(native, typed) {
			// The agent owns this exact name: forward it untouched and let it
			// echo the user message, as with any other agent command.
			return false, slashText(typed, rest), nil
		}
		// A translation (/context → grok's /session-info): the daemon owns the
		// interaction, so it echoes what the user actually typed.
		m.echoUser(id, slashText(typed, rest))
		return false, slashText(native, rest), nil
	}

	m.echoUser(id, slashText(typed, rest))
	switch res.Mapping.Kind {
	case command.KindMode:
		switch res.Spec.Name {
		case "plan":
			return true, "", m.cmdPlan(ctx, id, rest)
		case "mode":
			return true, "", m.cmdMode(ctx, id, rest)
		}
	case command.KindOp:
		switch res.Mapping.Op {
		case command.OpSetModel:
			return true, "", m.cmdModel(ctx, id, deviceID, rest, true)
		case command.OpCompact:
			return true, "", m.cmdCompact(ctx, id)
		case command.OpContext:
			return true, "", m.cmdContext(id)
		case command.OpDiff:
			return true, "", m.cmdDiff(ctx, id)
		case command.OpUndo:
			return true, "", m.cmdUndo(ctx, id)
		case command.OpRedo:
			return true, "", m.cmdRedo(ctx, id)
		}
	case command.KindDaemon:
		switch res.Spec.Name {
		case "help":
			m.emitNotice(id, m.helpText(id))
			return true, "", nil
		case "model":
			return true, "", m.cmdModel(ctx, id, deviceID, rest, false)
		case "clear":
			return true, "", m.cmdReset(ctx, id, deviceID)
		case "new":
			return true, "", m.cmdNew(ctx, id, deviceID, rest)
		case "sessions":
			return true, "", m.cmdSessions(id, deviceID)
		}
	}
	// A vocabulary entry with no dispatch arm is a programming error, not a
	// user error: say so plainly instead of silently doing nothing.
	m.emitNotice(id, fmt.Sprintf("“/%s” is not wired up in this build.", res.Spec.Name))
	return true, "", nil
}

// slashText rebuilds the command line for echoing or forwarding.
func slashText(name, rest string) string {
	if rest == "" {
		return "/" + name
	}
	return "/" + name + " " + rest
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

// helpText renders /help for a session from the resolved canonical vocabulary:
// what works here, what does not and why, the agent's modes, its own commands,
// and any provider-wide caveat. Everything comes from resolution, so /help
// cannot claim a command the router would refuse (MADR 0023).
func (m *Manager) helpText(id string) string {
	tbl, state, caveat := m.commandContext(id)
	m.mu.RLock()
	var current string
	if e, ok := m.sessions[id]; ok {
		current = e.currentModeID
	}
	m.mu.RUnlock()

	available := make([]string, 0, len(command.Specs))
	var unavailable []string
	for _, r := range command.ResolveAll(tbl, state) {
		if r.Available {
			available = append(available, r.Usage())
			continue
		}
		unavailable = append(unavailable, fmt.Sprintf("/%s (%s)", r.Spec.Name, r.Reason()))
	}
	msg := "You can run: " + strings.Join(available, ", ")

	if len(state.Modes) > 0 {
		labels := make([]string, 0, len(state.Modes))
		for _, mode := range state.Modes {
			label := mode.ID
			if strings.EqualFold(mode.ID, current) {
				label += " (current)"
			}
			labels = append(labels, label)
		}
		msg += ". Modes: " + strings.Join(labels, ", ")
	}
	if agent := state.AgentCommands; len(agent) > 0 {
		names := make([]string, 0, len(agent))
		for _, c := range agent {
			names = append(names, "/"+c)
		}
		msg += ". From the agent: " + strings.Join(names, ", ")
	}
	if len(unavailable) > 0 {
		msg += ". Not here: " + strings.Join(unavailable, ", ")
	}
	if caveat != "" {
		msg += ". " + caveat
	}
	return msg
}

// cmdSessions lists the sessions this device can see. Provider-independent: the
// daemon owns the session registry, so /sessions works everywhere.
func (m *Manager) cmdSessions(id, deviceID string) error {
	metas := m.ListFor(deviceID)
	if len(metas) == 0 {
		m.emitNotice(id, "No sessions yet. /new starts one.")
		return nil
	}
	lines := make([]string, 0, len(metas)+1)
	lines = append(lines, fmt.Sprintf("Sessions (%d):", len(metas)))
	for _, meta := range metas {
		label := meta.Name
		if label == "" {
			label = meta.ID
		}
		state := meta.Status
		if !meta.Live {
			state = "closed"
		}
		if meta.ID == id {
			label += " ← this one"
		}
		lines = append(lines, fmt.Sprintf("  %s · %s · %s", label, meta.Provider, state))
	}
	m.emitNotice(id, strings.Join(lines, "\n"))
	return nil
}

// cmdCompact asks the provider to summarise the conversation in place. The
// summary itself arrives as agent output, so this only reports the request.
func (m *Manager) cmdCompact(ctx context.Context, id string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	cs, ok := sess.(provider.CompactSession)
	if !ok {
		m.emitNotice(id, "This agent can't compact its conversation.")
		return nil
	}
	m.emitNotice(id, "Compacting the conversation…")
	if err := cs.Compact(ctx); err != nil {
		m.emitNotice(id, fmt.Sprintf("Compaction failed: %v", err))
		return err
	}
	return nil
}

// cmdContext reports context-window usage from the session's last usage report
// (the daemon's own tally), plus the model and mode for orientation.
func (m *Manager) cmdContext(id string) error {
	m.mu.RLock()
	var usage *event.Usage
	var model, mode string
	if e, ok := m.sessions[id]; ok {
		usage = e.lastUsage
		model = e.meta.Model
		mode = e.currentModeID
	}
	m.mu.RUnlock()

	if usage == nil {
		m.emitNotice(id, "No context report yet — send a message first.")
		return nil
	}
	msg := fmt.Sprintf("Context: %s tokens", formatCount(usage.Used))
	if usage.Size > 0 {
		msg += fmt.Sprintf(" of %s (%d%%)", formatCount(usage.Size),
			usage.Used*100/usage.Size)
	}
	if model != "" {
		msg += " · model " + model
	}
	if mode != "" {
		msg += " · mode " + mode
	}
	m.emitNotice(id, msg)
	return nil
}

// cmdDiff shows the file changes made in this session.
func (m *Manager) cmdDiff(ctx context.Context, id string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	ds, ok := sess.(provider.DiffSession)
	if !ok {
		m.emitNotice(id, "This agent can't report file changes.")
		return nil
	}
	summary, err := ds.Diff(ctx, "")
	if err != nil {
		m.emitNotice(id, fmt.Sprintf("Diff failed: %v", err))
		return err
	}
	m.emitNotice(id, summary)
	return nil
}

// cmdUndo reverts the last turn's changes.
func (m *Manager) cmdUndo(ctx context.Context, id string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	us, ok := sess.(provider.UndoSession)
	if !ok {
		m.emitNotice(id, "This agent can't undo a turn.")
		return nil
	}
	summary, err := us.UndoLast(ctx)
	if err != nil {
		m.emitNotice(id, fmt.Sprintf("Undo failed: %v", err))
		return err
	}
	if summary == "" {
		summary = "undone"
	}
	m.emitNotice(id, fmt.Sprintf("Undid the last turn — %s. /redo restores it.", summary))
	return nil
}

// cmdRedo restores the last undone turn.
func (m *Manager) cmdRedo(ctx context.Context, id string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	rs, ok := sess.(provider.RevertSession)
	if !ok {
		m.emitNotice(id, "This agent can't redo a turn.")
		return nil
	}
	if err := rs.Unrevert(ctx); err != nil {
		m.emitNotice(id, fmt.Sprintf("Redo failed: %v", err))
		return err
	}
	m.emitNotice(id, "Restored the undone turn.")
	return nil
}

// formatCount renders a token count with thousands separators.
func formatCount(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	lead := len(s) % 3
	if lead > 0 {
		b.WriteString(s[:lead])
	}
	for i := lead; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
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

// cmdModel shows the current model (no arg) or switches it (with a name).
// inPlace uses the provider's own model switch, which keeps the conversation;
// otherwise the agent is relaunched with the new model, which does not.
func (m *Manager) cmdModel(ctx context.Context, id, deviceID, arg string, inPlace bool) error {
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
	if inPlace {
		return m.setModelInPlace(ctx, id, arg)
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

// setModelInPlace switches the model through the provider's own call, keeping
// the conversation. The session metadata is updated so /context, /compact and a
// later resume agree with the engine. Falls back to a relaunch if the provider
// refuses, since a half-applied switch is worse than a restart.
func (m *Manager) setModelInPlace(ctx context.Context, id, model string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	ms, ok := sess.(provider.ModelSession)
	if !ok {
		m.emitNotice(id, "This agent can't switch model without restarting.")
		return nil
	}
	if err := ms.SetModel(ctx, model); err != nil {
		m.emitNotice(id, fmt.Sprintf("Model switch to %s failed: %v", model, err))
		return err
	}
	m.mu.Lock()
	e, live := m.sessions[id]
	if live {
		e.meta.Model = model
	}
	m.mu.Unlock()
	if live {
		// Debounced: the flush re-reads the live meta, so this cannot revert a
		// newer write.
		m.persist(id)
	}
	m.emitNotice(id, fmt.Sprintf("Model is now %s — the conversation is kept.", model))
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

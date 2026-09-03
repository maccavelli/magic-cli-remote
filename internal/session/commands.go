package session

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
		AgentCommands:              slices.Clone(e.agentCommands),
		Modes:                      slices.Clone(e.agentModes),
		CollaborationModes:         slices.Clone(e.collabModes),
		CurrentCollaborationModeID: e.currentCollabModeID,
		Ops:                        map[command.Op]bool{command.OpContext: e.lastUsage != nil},
	}
	sess, prov := e.sess, e.meta.Provider
	m.mu.RUnlock()

	// Capabilities come from the live session's optional interfaces, so a
	// provider cannot claim an op its transport does not implement.
	if sess != nil {
		_, state.Ops[command.OpCompact] = sess.(provider.CompactSession)
		_, state.Ops[command.OpSetModel] = sess.(provider.ModelSession)
		_, state.Ops[command.OpSetThinkingLevel] = sess.(provider.ThinkingSession)
		_, state.Ops[command.OpDiff] = sess.(provider.DiffSession)
		_, state.Ops[command.OpStatus] = sess.(provider.RuntimeSession)
		_, state.Ops[command.OpUsage] = sess.(provider.RuntimeSession)
		_, state.Ops[command.OpApprovalsReviewer] = sess.(provider.PermissionProfileSession)
		_, state.Ops[command.OpGuardianApprove] = sess.(provider.GuardianApprovalSession)
		_, state.Ops[command.OpUndo] = sess.(provider.UndoSession)
		_, state.Ops[command.OpRedo] = sess.(provider.RevertSession)
		_, state.Ops[command.OpFork] = sess.(provider.ForkSession)
		_, state.Ops[command.OpArchive] = sess.(provider.NativeThreadLifecycleSession)
		_, state.Ops[command.OpDelete] = sess.(provider.NativeThreadLifecycleSession)
		_, state.Ops[command.OpPS] = sess.(provider.ExecutionSession)
		_, state.Ops[command.OpStop] = sess.(provider.ExecutionSession)
		_, state.Ops[command.OpGoal] = sess.(provider.GoalSession)
		_, state.Ops[command.OpReview] = sess.(provider.ReviewSession)
		if ts, ok := sess.(provider.ServiceTierSession); ok {
			state.Ops[command.OpServiceTier] = ts.HasFast()
		}
		if ps, ok := sess.(provider.PersonalitySession); ok {
			state.Ops[command.OpPersonality] = ps.PersonalitySupported()
		}
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
	res command.Resolution, typed, rest string, attachments []provider.Content,
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

	if res.Mapping.Kind == command.KindCollaborationMode && res.Spec.Name == "plan" {
		return true, "", m.cmdCollaborationPlan(ctx, id, rest, attachments)
	}

	m.echoUser(id, slashText(typed, rest))
	switch res.Mapping.Kind {
	case command.KindMode:
		switch res.Spec.Name {
		case "plan":
			return true, "", m.cmdPlan(ctx, id, rest)
		case "mode", "permissions":
			return true, "", m.cmdMode(ctx, id, rest)
		}
	case command.KindOp:
		switch res.Mapping.Op {
		case command.OpSetModel:
			return true, "", m.cmdModel(ctx, id, deviceID, rest, true)
		case command.OpSetThinkingLevel:
			return true, "", m.cmdThinking(ctx, id, rest)
		case command.OpCompact:
			return true, "", m.cmdCompact(ctx, id)
		case command.OpContext:
			return true, "", m.cmdContext(id)
		case command.OpStatus:
			return true, "", m.cmdRuntime(ctx, id, false)
		case command.OpUsage:
			return true, "", m.cmdRuntime(ctx, id, true)
		case command.OpApprovalsReviewer:
			return true, "", m.cmdApprovalsReviewer(ctx, id, rest)
		case command.OpGuardianApprove:
			return true, "", m.cmdGuardianApprove(ctx, id, rest)
		case command.OpDiff:
			return true, "", m.cmdDiff(ctx, id)
		case command.OpFork:
			return true, "", m.cmdFork(ctx, id, rest, deviceID)
		case command.OpArchive:
			return true, "", m.cmdNativeArchive(ctx, id, rest)
		case command.OpDelete:
			return true, "", m.cmdNativeDelete(ctx, id, rest)
		case command.OpPS:
			return true, "", m.cmdTerminals(ctx, id)
		case command.OpStop:
			return true, "", m.cmdStopTerminal(ctx, id, rest)
		case command.OpGoal:
			return true, "", m.cmdGoal(ctx, id, rest)
		case command.OpServiceTier:
			return true, "", m.cmdFast(ctx, id, rest)
		case command.OpPersonality:
			return true, "", m.cmdPersonality(ctx, id, rest)
		case command.OpReview:
			return true, "", m.cmdReview(ctx, id, rest)
		case command.OpUndo:
			return true, "", m.cmdUndo(ctx, id)
		case command.OpRedo:
			return true, "", m.cmdRedo(ctx, id)
		}
	case command.KindDaemon:
		return m.dispatchDaemon(ctx, id, deviceID, res.Spec.Name, rest)
	}
	// A vocabulary entry with no dispatch arm is a programming error, not a
	// user error: say so plainly instead of silently doing nothing.
	m.emitNotice(id, fmt.Sprintf("“/%s” is not wired up in this build.", res.Spec.Name))
	return true, "", nil
}

// daemonCommands is the dispatch table for KindDaemon commands. MADR 0035
// D6: it is a data table the conformance test can read directly, so a
// KindDaemon entry declared by any provider's table is provably wired to
// a handler here. The "not wired up in this build" notice at the
// call-site is the programming-error path this table removes.
var daemonCommands = map[string]func(*Manager, context.Context, string, string, string) error{
	"help": func(m *Manager, _ context.Context, id, _, _ string) error {
		m.emitNotice(id, m.helpText(id))
		return nil
	},
	"model": func(m *Manager, ctx context.Context, id, dev, rest string) error {
		return m.cmdModel(ctx, id, dev, rest, false)
	},
	"clear": func(m *Manager, ctx context.Context, id, dev, _ string) error {
		return m.cmdReset(ctx, id, dev)
	},
	"new": func(m *Manager, ctx context.Context, id, dev, rest string) error {
		return m.cmdNew(ctx, id, dev, rest)
	},
	"sessions": func(m *Manager, _ context.Context, id, dev, _ string) error {
		return m.cmdSessions(id, dev)
	},
}

// dispatchDaemon looks up a KindDaemon name in daemonCommands. The
// (intentionally) programming-error fallback is reported as a notice
// and returned as a non-nil error so the test suite catches missing
// entries, not just the user.
func (m *Manager) dispatchDaemon(ctx context.Context, id, deviceID, name, rest string) (bool, string, error) {
	fn, ok := daemonCommands[name]
	if !ok {
		m.emitNotice(id, fmt.Sprintf("“/%s” is not wired up in this build.", name))
		return true, "", nil
	}
	if err := fn(m, ctx, id, deviceID, rest); err != nil {
		return true, "", err
	}
	return true, "", nil
}

// DaemonCommandNames lists the canonical command names the daemon
// implements itself. Exported for the cross-provider conformance test
// (MADR 0035 D6 / internal/command/conformance_test.go).
func DaemonCommandNames() []string {
	out := make([]string, 0, len(daemonCommands))
	for name := range daemonCommands {
		out = append(out, name)
	}
	return out
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

func (m *Manager) cmdRuntime(ctx context.Context, id string, usage bool) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	runtimeSession, ok := sess.(provider.RuntimeSession)
	if !ok {
		m.emitNotice(id, "This agent exposes no runtime status.")
		return nil
	}
	var message string
	if usage {
		message, err = runtimeSession.RuntimeUsage(ctx)
	} else {
		message, err = runtimeSession.RuntimeStatus(ctx)
	}
	if err != nil {
		return err
	}
	m.emitNotice(id, message)
	return nil
}

func (m *Manager) cmdApprovalsReviewer(ctx context.Context, id, arg string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	permissions, ok := sess.(provider.PermissionProfileSession)
	if !ok {
		m.emitNotice(id, "This agent exposes no separate approval reviewer.")
		return nil
	}
	_, _, current := permissions.PermissionSettings()
	target := strings.TrimSpace(arg)
	if target == "" {
		m.emitNotice(id, "Approval reviewer: "+current+". Choices: user, auto_review.")
		return nil
	}
	if target != provider.ApprovalsReviewerUser && target != provider.ApprovalsReviewerAutoReview {
		m.emitNotice(id, "Usage: /reviewer [user|auto_review]")
		return nil
	}
	if err := permissions.SetApprovalsReviewer(ctx, target); err != nil {
		m.emitNotice(id, fmt.Sprintf("Reviewer switch failed: %v", err))
		return err
	}
	m.mu.Lock()
	if entry := m.sessions[id]; entry != nil {
		entry.meta.ApprovalsReviewer = target
	}
	m.mu.Unlock()
	m.persist(id)
	m.emitNotice(id, "Approval reviewer set to "+target+"; the permission profile and sandbox were unchanged.")
	return nil
}

func (m *Manager) cmdGuardianApprove(ctx context.Context, id, arg string) error {
	if strings.TrimSpace(arg) != "" {
		m.emitNotice(id, "Usage: /approve")
		return nil
	}
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	guardian, ok := sess.(provider.GuardianApprovalSession)
	if !ok {
		m.emitNotice(id, "This agent has no tracked Guardian denial.")
		return nil
	}
	if err := guardian.ApproveGuardianDenied(ctx); err != nil {
		if errors.Is(err, provider.ErrGuardianApprovalUnavailable) {
			m.emitNotice(id, "No current Guardian-denied action is available to retry.")
			return nil
		}
		m.emitNotice(id, "Guardian retry failed.")
		return err
	}
	m.emitNotice(id, "Retried the exact Guardian-denied action once; permissions were unchanged.")
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
	res, err := ds.Diff(ctx, "")
	if err != nil {
		m.emitNotice(id, fmt.Sprintf("Diff failed: %v", err))
		return err
	}
	m.emitNotice(id, res.Summary)
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
// provider treats as its normal working state (grok/codex "default", OpenCode
// "build"), else the first mode that is neither plan nor dangerous.
//
// The dangerous filter matters: leaving plan mode must never land the user in a
// mode that answers permissions for them. Ordering alone is not enough — a
// provider advertising only [plan, auto] would otherwise make `/plan off` arm
// auto-approve (MADR 0044).
func defaultMode(modes []event.SessionMode) (event.SessionMode, bool) {
	for _, want := range []string{"default", "build", "code"} {
		if mode, ok := findMode(modes, want); ok && !mode.Dangerous {
			return mode, true
		}
	}
	for _, mode := range modes {
		if !strings.EqualFold(mode.ID, "plan") && !mode.Dangerous {
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

func (m *Manager) cmdCollaborationPlan(ctx context.Context, id, arg string, attachments []provider.Content) error {
	arg = strings.TrimSpace(arg)
	_, current, err := m.sessionCollaboration(id)
	if err != nil {
		m.emitNotice(id, "This agent doesn't offer a collaboration plan mode.")
		return nil
	}
	switch strings.ToLower(arg) {
	case "", "on":
		if strings.EqualFold(current, "plan") {
			m.emitNotice(id, "Already in plan mode. Use /plan off to leave it.")
			return nil
		}
		if m.sessionHasActiveGoal(id) {
			m.emitNotice(id, "Pause or clear the active goal before entering Plan.")
			return provider.ErrGoalPlanConflict
		}
		if err := m.setCollaborationMode(ctx, id, "plan"); err != nil {
			m.emitNotice(id, fmt.Sprintf("Plan switch failed: %v", err))
			return err
		}
		m.echoUser(id, "/plan")
		m.emitNotice(id, "Plan collaboration on. Use /plan off to leave it.")
		return nil
	case "off", "exit", "stop":
		if strings.EqualFold(current, "default") || current == "" {
			m.emitNotice(id, "Already in default collaboration mode.")
			return nil
		}
		if err := m.setCollaborationMode(ctx, id, "default"); err != nil {
			m.emitNotice(id, fmt.Sprintf("Plan switch failed: %v", err))
			return err
		}
		m.echoUser(id, "/plan "+strings.ToLower(arg))
		m.emitNotice(id, "Plan collaboration off — back to default.")
		return nil
	}
	if strings.HasPrefix(strings.ToLower(arg), "off ") ||
		strings.HasPrefix(strings.ToLower(arg), "exit ") ||
		strings.HasPrefix(strings.ToLower(arg), "stop ") {
		m.emitNotice(id, "Usage: /plan to enter Plan, /plan off to leave it. Extra words after off/exit/stop are not allowed.")
		return nil
	}
	// Inline prompt remainder.
	if !strings.EqualFold(current, "plan") {
		if m.sessionHasActiveGoal(id) {
			m.emitNotice(id, "Pause or clear the active goal before entering Plan.")
			return provider.ErrGoalPlanConflict
		}
		if err := m.setCollaborationMode(ctx, id, "plan"); err != nil {
			m.emitNotice(id, fmt.Sprintf("Plan switch failed: %v", err))
			return err
		}
	}
	return m.submitUserPrompt(ctx, id, arg, attachments)
}

func (m *Manager) sessionCollaboration(id string) (modes []event.CollaborationMode, current string, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.sessions[id]
	if !ok {
		return nil, "", fmt.Errorf("unknown session")
	}
	if len(e.collabModes) == 0 {
		return nil, "", provider.ErrCollaborationUnsupported
	}
	return slices.Clone(e.collabModes), e.currentCollabModeID, nil
}

func (m *Manager) submitUserPrompt(ctx context.Context, id, text string, attachments []provider.Content) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	m.echoUser(id, text)
	parts := make([]provider.Content, 0, 1+len(attachments))
	if text != "" || len(attachments) == 0 {
		parts = append(parts, provider.Content{Type: "text", Text: text})
	}
	parts = append(parts, attachments...)
	// A canonical command that ends in a prompt is still a turn the user
	// waits on, so it is timed like any other (MADR 0137 Phase 2). This path
	// is reached only via runCanonical, which returns handled — so Manager.
	// Prompt never also starts the clock for the same turn.
	m.markPromptStart(id)
	if err := sess.Prompt(ctx, parts); err != nil {
		m.clearPromptStart(id)
		return err
	}
	return nil
}

func (m *Manager) cmdFast(ctx context.Context, id, arg string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	ts, ok := sess.(provider.ServiceTierSession)
	if !ok || !ts.HasFast() {
		m.emitNotice(id, "This agent has no Fast service tier.")
		return nil
	}
	arg = strings.ToLower(strings.TrimSpace(arg))
	on := ts.ServiceTier() != ""
	switch arg {
	case "":
		on = !on
	case "on":
		on = true
	case "off":
		on = false
	default:
		m.emitNotice(id, "Usage: /fast [on|off]")
		return nil
	}
	was := ts.ServiceTier() != ""
	if was == on {
		if on {
			m.emitNotice(id, "Fast is already on.")
		} else {
			m.emitNotice(id, "Fast is already off.")
		}
		return nil
	}
	err = ts.SetServiceTier(ctx, on)
	if err != nil && !errors.Is(err, provider.ErrAppliesNextTurn) {
		m.emitNotice(id, fmt.Sprintf("Fast switch failed: %v", err))
		return err
	}
	m.mu.Lock()
	if e, live := m.sessions[id]; live {
		e.meta.ServiceTier = ts.ServiceTier()
	}
	m.mu.Unlock()
	m.persist(id)
	if on {
		if errors.Is(err, provider.ErrAppliesNextTurn) {
			m.emitNotice(id, "Fast on — applies next turn.")
		} else {
			m.emitNotice(id, "Fast is on.")
		}
	} else if errors.Is(err, provider.ErrAppliesNextTurn) {
		m.emitNotice(id, "Fast off — applies next turn.")
	} else {
		m.emitNotice(id, "Fast is off.")
	}
	return nil
}

func (m *Manager) cmdPersonality(ctx context.Context, id, arg string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	ps, ok := sess.(provider.PersonalitySession)
	if !ok || !ps.PersonalitySupported() {
		m.emitNotice(id, "This agent has no personality setting.")
		return nil
	}
	arg = strings.ToLower(strings.TrimSpace(arg))
	if arg == "" {
		cur := ps.Personality()
		if cur == "" {
			cur = "provider default"
		}
		m.emitNotice(id, fmt.Sprintf("Personality: %s · usage: /personality friendly|pragmatic|none", cur))
		return nil
	}
	if arg == "default" {
		m.emitNotice(id, "Usage: /personality friendly|pragmatic|none")
		return nil
	}
	if ps.Personality() == arg {
		m.emitNotice(id, fmt.Sprintf("Already using personality %s.", arg))
		return nil
	}
	err = ps.SetPersonality(ctx, arg)
	if err != nil && !errors.Is(err, provider.ErrAppliesNextTurn) {
		if errors.Is(err, provider.ErrPersonalityInvalid) {
			m.emitNotice(id, "Usage: /personality friendly|pragmatic|none")
			return nil
		}
		m.emitNotice(id, fmt.Sprintf("Personality switch failed: %v", err))
		return err
	}
	m.mu.Lock()
	if e, live := m.sessions[id]; live {
		e.meta.Personality = ps.Personality()
	}
	m.mu.Unlock()
	m.persist(id)
	if errors.Is(err, provider.ErrAppliesNextTurn) {
		m.emitNotice(id, fmt.Sprintf("Personality %s — applies next turn.", arg))
	} else {
		m.emitNotice(id, fmt.Sprintf("Personality is now %s.", arg))
	}
	return nil
}

func (m *Manager) sessionHasActiveGoal(id string) bool {
	sess, err := m.liveSession(id)
	if err != nil {
		return false
	}
	gs, ok := sess.(provider.GoalSession)
	if !ok {
		return false
	}
	g, present := gs.CurrentGoal()
	return provider.GoalIsActive(g, present)
}

func (m *Manager) cmdGoal(ctx context.Context, id, arg string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	gs, ok := sess.(provider.GoalSession)
	if !ok {
		m.emitNotice(id, "This agent has no goal loop.")
		return nil
	}
	mut, err := parseManagerGoal(arg)
	if err != nil {
		m.emitNotice(id, "Usage: /goal [edit] <objective> | /goal pause|resume|clear")
		return nil
	}
	g, err := gs.ApplyGoal(ctx, mut)
	if err != nil {
		switch {
		case errors.Is(err, provider.ErrTurnBusy):
			m.emitNotice(id, "Can't change the goal while a turn is running.")
		case errors.Is(err, provider.ErrGoalPlanConflict):
			m.emitNotice(id, "Leave Plan before creating or resuming a goal. Pause or edit a paused goal is still allowed.")
		default:
			m.emitNotice(id, "Goal request failed.")
		}
		return err
	}
	switch mut.Kind {
	case provider.GoalView:
		if g.Status == "" && g.Objective == "" {
			m.emitNotice(id, "No goal is set.")
			return nil
		}
		m.emitNotice(id, formatGoalNotice(g))
	case provider.GoalClear:
		m.emitNotice(id, "Goal cleared.")
	case provider.GoalPause:
		m.emitNotice(id, "Goal paused.")
	case provider.GoalResume:
		m.emitNotice(id, "Goal resumed.")
	default:
		m.emitNotice(id, formatGoalNotice(g))
	}
	return nil
}

func parseManagerGoal(arg string) (provider.GoalMutation, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return provider.GoalMutation{Kind: provider.GoalView}, nil
	}
	fields := strings.Fields(arg)
	verb := strings.ToLower(fields[0])
	switch verb {
	case "pause":
		if len(fields) != 1 {
			return provider.GoalMutation{}, provider.ErrGoalInvalid
		}
		return provider.GoalMutation{Kind: provider.GoalPause}, nil
	case "resume":
		if len(fields) != 1 {
			return provider.GoalMutation{}, provider.ErrGoalInvalid
		}
		return provider.GoalMutation{Kind: provider.GoalResume}, nil
	case "clear":
		if len(fields) != 1 {
			return provider.GoalMutation{}, provider.ErrGoalInvalid
		}
		return provider.GoalMutation{Kind: provider.GoalClear}, nil
	case "edit":
		obj := strings.TrimSpace(strings.TrimPrefix(arg, fields[0]))
		if utf8.RuneCountInString(obj) == 0 || utf8.RuneCountInString(obj) > 4000 {
			return provider.GoalMutation{}, provider.ErrGoalInvalid
		}
		return provider.GoalMutation{Kind: provider.GoalEdit, Objective: obj}, nil
	default:
		if utf8.RuneCountInString(arg) == 0 || utf8.RuneCountInString(arg) > 4000 {
			return provider.GoalMutation{}, provider.ErrGoalInvalid
		}
		return provider.GoalMutation{Kind: provider.GoalReplace, Objective: arg}, nil
	}
}

func formatGoalNotice(g provider.Goal) string {
	obj := g.Objective
	if runes := []rune(obj); len(runes) > 200 {
		obj = string(runes[:200]) + "…"
	}
	msg := fmt.Sprintf("Goal (%s): %s", g.Status, obj)
	if g.TokenBudget > 0 {
		msg += fmt.Sprintf(" · %d/%d tokens", g.TokenUsage, g.TokenBudget)
	}
	return msg
}

func (m *Manager) cmdReview(ctx context.Context, id, arg string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	rs, ok := sess.(provider.ReviewSession)
	if !ok {
		m.emitNotice(id, "This agent has no inline review command.")
		return nil
	}
	target, err := provider.ParseReviewArg(arg)
	if err != nil {
		m.emitNotice(id, "Usage: /review [uncommitted|base <branch>|commit <sha>|custom <text>]")
		return nil
	}
	if err := rs.StartReview(ctx, target); err != nil {
		if errors.Is(err, provider.ErrTurnBusy) {
			m.emitNotice(id, "Can't start a review while a turn is running.")
			return err
		}
		m.emitNotice(id, "Review failed.")
		return err
	}
	return nil
}

func (m *Manager) cmdFork(ctx context.Context, id, arg, deviceID string) error {
	meta, err := m.Fork(ctx, id, strings.TrimSpace(arg), deviceID)
	if err != nil {
		if errors.Is(err, provider.ErrForkNothing) {
			m.emitNotice(id, "Nothing to fork yet — send a message first.")
			return nil
		}
		m.emitNotice(id, fmt.Sprintf("Fork failed: %v", err))
		return err
	}
	m.emitNotice(id, fmt.Sprintf("Forked to session %s (%s).", meta.ID, meta.Name))
	return nil
}

func (m *Manager) cmdNativeArchive(ctx context.Context, id, arg string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	lifecycle, ok := sess.(provider.NativeThreadLifecycleSession)
	if !ok {
		m.emitNotice(id, "This agent has no native archive operation.")
		return nil
	}
	switch strings.TrimSpace(arg) {
	case "archive":
		if err := lifecycle.ArchiveNativeThread(ctx, true); err != nil {
			m.emitNotice(id, "Archive failed.")
			return err
		}
		m.emitNotice(id, "Native thread archived. Use /archive unarchive to restore it.")
	case "unarchive":
		if err := lifecycle.ArchiveNativeThread(ctx, false); err != nil {
			m.emitNotice(id, "Unarchive failed.")
			return err
		}
		m.emitNotice(id, "Native thread restored from the archive.")
	default:
		m.emitNotice(id, "Confirmation required: /archive archive (or /archive unarchive).")
	}
	return nil
}

func (m *Manager) cmdNativeDelete(ctx context.Context, id, arg string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	lifecycle, ok := sess.(provider.NativeThreadLifecycleSession)
	if !ok {
		m.emitNotice(id, "This agent has no native permanent-delete operation.")
		return nil
	}
	preview, err := lifecycle.PreviewNativeDelete(ctx)
	if err != nil {
		m.emitNotice(id, "Delete impact preview failed; nothing was deleted.")
		return err
	}
	if strings.TrimSpace(arg) != "delete permanently" {
		detail := fmt.Sprintf("%d descendant(s)", len(preview.DescendantIDs))
		if preview.HasLoadedDescendants {
			detail += ", including a loaded descendant"
		}
		m.emitNotice(id, "Permanent deletion affects "+detail+". Confirm exactly: /delete delete permanently")
		return nil
	}
	result, err := lifecycle.DeleteNativeThread(ctx)
	if err != nil {
		m.emitNotice(id, "Permanent delete failed; the native thread was reconciled and remains present.")
		return err
	}
	if !result.Deleted {
		m.emitNotice(id, "Permanent delete outcome is unresolved; refresh the thread browser before retrying.")
		return nil
	}
	detail := fmt.Sprintf("%d descendant(s) affected", len(result.DescendantIDs))
	if len(result.FailedDescendantIDs) > 0 {
		detail += fmt.Sprintf(", %d descendant(s) failed", len(result.FailedDescendantIDs))
	}
	if result.Partial {
		detail += "; deletion was partial"
	}
	m.emitNotice(id, "Native thread permanently deleted; "+detail+".")
	return nil
}

func (m *Manager) cmdTerminals(ctx context.Context, id string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	execution, ok := sess.(provider.ExecutionSession)
	if !ok {
		m.emitNotice(id, "This agent has no terminal registry.")
		return nil
	}
	terminals, err := execution.ListTerminals(ctx)
	if err != nil {
		m.emitNotice(id, "Terminal list failed.")
		return err
	}
	if len(terminals) == 0 {
		m.emitNotice(id, "No active terminals.")
		return nil
	}
	lines := make([]string, 0, len(terminals)+1)
	lines = append(lines, "Execution terminals:")
	for _, terminal := range terminals {
		state := "exited"
		if terminal.Running {
			state = "running"
		}
		lines = append(lines, fmt.Sprintf("%s — %s — %s (%s)", terminal.ID, terminal.Label, terminal.Kind, state))
	}
	m.emitNotice(id, strings.Join(lines, "\n"))
	return nil
}

func (m *Manager) cmdStopTerminal(ctx context.Context, id, arg string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	execution, ok := sess.(provider.ExecutionSession)
	if !ok {
		m.emitNotice(id, "This agent has no stoppable terminals.")
		return nil
	}
	fields := strings.Fields(arg)
	if len(fields) != 1 {
		m.emitNotice(id, "Usage: /stop <id> or /stop --all")
		return nil
	}
	if fields[0] == "--all" {
		count, err := execution.StopAllTerminals(ctx)
		if err != nil {
			m.emitNotice(id, "Stopping all terminals failed.")
			return err
		}
		m.emitNotice(id, fmt.Sprintf("Stopped %d terminal(s).", count))
		return nil
	}
	if strings.HasPrefix(fields[0], "-") || len(fields[0]) > 256 {
		m.emitNotice(id, "Usage: /stop <id> or /stop --all")
		return nil
	}
	if err := execution.StopTerminal(ctx, fields[0]); err != nil {
		m.emitNotice(id, fmt.Sprintf("Stop %s failed.", fields[0]))
		return err
	}
	m.emitNotice(id, fmt.Sprintf("Stopped terminal %s.", fields[0]))
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
	if strings.EqualFold(arg, "plan") {
		if _, _, err := m.sessionCollaboration(id); err == nil {
			m.emitNotice(id, "Use /plan to switch collaboration mode. /mode lists permission modes.")
			return nil
		}
	}
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
	prov, cwd, name, owner, cur, thinking, ok := m.sessionRelaunchInfo(id)
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
	if err := m.relaunch(ctx, id, prov, cwd, name, arg, thinking, owner); err != nil {
		// Relaunch is destroy-then-create: the old agent is already gone. Try
		// to bring the session back on the previous model rather than leaving
		// the user with a dead id and a vague notice.
		prevLabel := cur
		if prevLabel == "" {
			prevLabel = "the provider default"
		}
		if rerr := m.relaunch(ctx, id, prov, cwd, name, cur, thinking, owner); rerr == nil {
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

// cmdThinking shows or switches the session's reasoning/thinking effort.
// Empty arg reports the current level; a name is forwarded to ThinkingSession.
// A provider that locks the level at spawn returns ErrThinkingLevelFixed,
// which is rendered as a "new sessions only" notice rather than a hard error.
// None does today — grok stopped in 1.0.5 (MADR 0106) and this comment named
// it for two releases afterwards (MADR 0123 F7). The branch stays as the
// backstop for a session advertising provider.ThinkingMutabilityFixed.
func (m *Manager) cmdThinking(ctx context.Context, id, arg string) error {
	arg = strings.TrimSpace(arg)
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	ts, ok := sess.(provider.ThinkingSession)
	if !ok {
		m.emitNotice(id, "This agent has no selectable thinking level.")
		return nil
	}
	if arg == "" {
		label := ts.ThinkingLevel()
		if label == "" {
			label = "provider default"
		}
		m.emitNotice(id, fmt.Sprintf("Thinking level: %s · usage: /thinking <level> to switch", label))
		return nil
	}
	if arg == ts.ThinkingLevel() {
		m.emitNotice(id, fmt.Sprintf("Already at thinking level %s.", arg))
		return nil
	}
	if err := ts.SetThinkingLevel(ctx, arg); err != nil {
		if errors.Is(err, provider.ErrThinkingLevelFixed) {
			m.emitNotice(id, "This agent applies thinking level at session start; "+
				"it takes effect for new sessions.")
			return nil
		}
		m.emitNotice(id, fmt.Sprintf("Thinking level switch to %s failed: %v", arg, err))
		return err
	}
	m.mu.Lock()
	if e, live := m.sessions[id]; live {
		e.meta.ThinkingLevel = arg
	}
	m.mu.Unlock()
	m.persist(id)
	if _, current, cerr := m.sessionCollaboration(id); cerr == nil && strings.EqualFold(current, "plan") {
		m.emitNotice(id, fmt.Sprintf("Thinking level %s stored — applies when you leave Plan.", arg))
		return nil
	}
	// Codex is next-turn by construction (MADR 0052 D7); the wording matches
	// mode switches so the user is not told the change is instant when it is not.
	m.emitNotice(id, fmt.Sprintf("Thinking level is now %s, from your next message.", arg))
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
		if ts, ok := sess.(provider.ServiceTierSession); ok {
			e.meta.ServiceTier = ts.ServiceTier()
		}
		if ps, ok := sess.(provider.PersonalitySession); ok {
			e.meta.Personality = ps.Personality()
		}
	}
	m.mu.Unlock()
	if live {
		// Debounced: the flush re-reads the live meta, so this cannot revert a
		// newer write.
		m.persist(id)
	}
	m.advertiseCommands(id)
	m.emitNotice(id, fmt.Sprintf("Model is now %s — the conversation is kept.", model))
	return nil
}

// cmdReset relaunches the agent with the same model, clearing its context.
func (m *Manager) cmdReset(ctx context.Context, id, deviceID string) error {
	prov, cwd, name, owner, cur, thinking, ok := m.sessionRelaunchInfo(id)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotLive, id)
	}
	m.emitNotice(id, "Restarting the agent…")
	if err := m.relaunch(ctx, id, prov, cwd, name, cur, thinking, owner); err != nil {
		// Relaunch is destroy-then-create: try once more (same as /model
		// recovery) so a transient Start failure does not leave a dead id.
		if rerr := m.relaunch(ctx, id, prov, cwd, name, cur, thinking, owner); rerr == nil {
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
// provider, working directory, model, and thinking level. It is owned by the
// requesting device.
func (m *Manager) cmdNew(ctx context.Context, id, deviceID, arg string) error {
	prov, cwd, _, _, model, thinking, ok := m.sessionRelaunchInfo(id)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotLive, id)
	}
	name := strings.TrimSpace(arg)
	meta, err := m.Create(ctx, prov, provider.StartOptions{
		CWD: cwd, Name: name, Model: model, ThinkingLevel: thinking,
	}, deviceID)
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
func (m *Manager) sessionRelaunchInfo(id string) (prov provider.ID, cwd, name, owner, model, thinking string, ok bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, live := m.sessions[id]
	if !live || e.dead {
		return "", "", "", "", "", "", false
	}
	return e.meta.Provider, e.meta.CWD, e.meta.Name, e.meta.OwnerDeviceID, e.meta.Model, e.meta.ThinkingLevel, true
}

// relaunch replaces the live session id with a fresh agent process, reusing the
// tested close-and-replace path in [Manager.Create]. The client keeps its own
// transcript; the server-side history ring is reset with the new process.
func (m *Manager) relaunch(ctx context.Context, id string, prov provider.ID, cwd, name, model, thinking, owner string) error {
	_, err := m.Create(ctx, prov, provider.StartOptions{
		LocalSessionID: id,
		CWD:            cwd,
		Name:           name,
		Model:          model,
		ThinkingLevel:  thinking,
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

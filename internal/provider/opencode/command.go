package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// StaticCommands implements [httpagent.CommandLister] offline fallback.
func (d *httpDialect) StaticCommands(cfg httpagent.Config) picker.Catalog {
	_ = cfg
	opts := []picker.Option{
		{ID: "init", Label: "init", Description: "guided AGENTS.md setup", Group: "command"},
		{ID: "review", Label: "review", Description: "code review", Group: "command"},
	}
	return picker.SingleCatalog(picker.SourceStatic, opts, "", true)
}

// ListCommandsLive implements [httpagent.CommandLister] via GET /command.
func (d *httpDialect) ListCommandsLive(ctx context.Context, api httpagent.API) (picker.Catalog, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var cmds []struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Source      string   `json:"source"`
		Hints       []string `json:"hints"`
	}
	if err := api(ctx, "GET", "/command", nil, &cmds); err != nil {
		return picker.Catalog{}, err
	}
	opts := make([]picker.Option, 0, len(cmds))
	for _, c := range cmds {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		group := c.Source
		if group == "" {
			group = "command"
		}
		desc := strings.TrimSpace(c.Description)
		if desc == "" {
			// Same reduction as advertiseCommands, so one command cannot be
			// described one way in the picker and another in the command list.
			desc = commandHint(c.Hints)
		}
		opts = append(opts, picker.Option{
			ID:          name,
			Label:       name,
			Description: desc,
			Group:       group,
		})
	}
	slices.SortFunc(opts, func(a, b picker.Option) int {
		if a.Group != b.Group {
			return strings.Compare(a.Group, b.Group)
		}
		return strings.Compare(a.ID, b.ID)
	})
	return picker.SingleCatalog(picker.SourceLive, opts, "", true), nil
}

// maxCommandHintLen bounds the rendered hint. The picker shows it inline next
// to the command name, so an unbounded template placeholder list would push the
// name off a phone screen.
const maxCommandHintLen = 128

// commandHint reduces OpenCode's `hints` array to the single display string
// event.AvailableCommand.Hint carries.
//
// GET /command returns hints as an *array* of template placeholders — on
// 1.18.21 the built-in `init` and `review` both report ["$ARGUMENTS"] and
// skill-backed commands report [] — while the wire field is one string. The
// reduction is fixed so the picker and the advertised list can never disagree:
// trim each element, drop empties, preserve upstream order without sorting or
// deduplicating (the order is the argument order), join with a single space,
// and clip on a rune boundary.
func commandHint(hints []string) string {
	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		if h = strings.TrimSpace(h); h != "" {
			parts = append(parts, h)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	joined := strings.Join(parts, " ")
	if len(joined) <= maxCommandHintLen {
		return joined
	}
	// Clip on a rune boundary. The shared clip helper cuts by byte index, which
	// is fine for the machine-generated text it normally handles; a custom
	// command's placeholder can be any UTF-8, and half a rune on the wire is a
	// decode error on the phone.
	cut := maxCommandHintLen
	for cut > 0 && !utf8.RuneStart(joined[cut]) {
		cut--
	}
	return strings.TrimRight(joined[:cut], " ") + "…"
}

// advertiseCommands fetches the engine command catalog and emits
// available_commands so the manager/mobile treat OpenCode slash commands as
// advertised (and manager.Prompt will forward them).
func (o *httpSession) advertiseCommands(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var cmds []struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Hints       []string `json:"hints"`
	}
	if err := o.h.API()(ctx, "GET", "/command"+o.dir(), nil, &cmds); err != nil {
		o.h.Log().Debug("list commands for advertise failed", slog.String("err", err.Error()))
		// Static fallback names so /init still routes when catalog is down.
		o.emitCommands([]event.AvailableCommand{
			{Name: "init", Description: "guided AGENTS.md setup"},
		})
		return
	}
	out := make([]event.AvailableCommand, 0, len(cmds))
	for _, c := range cmds {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		out = append(out, event.AvailableCommand{
			Name:        name,
			Description: strings.TrimSpace(c.Description),
			Hint:        commandHint(c.Hints),
		})
	}
	o.emitCommands(out)
}

// soleSlashCommand returns (name, arguments, true) when parts is a single text
// block that is a slash command (e.g. "/init my repo").
func soleSlashCommand(parts []provider.Content) (name, args string, ok bool) {
	if len(parts) != 1 {
		return "", "", false
	}
	p := parts[0]
	if p.Type != "" && p.Type != "text" {
		return "", "", false
	}
	t := strings.TrimSpace(p.Text)
	if !strings.HasPrefix(t, "/") {
		return "", "", false
	}
	t = strings.TrimPrefix(t, "/")
	if t == "" {
		return "", "", false
	}
	if i := strings.IndexAny(t, " \t\n"); i >= 0 {
		name = strings.ToLower(t[:i])
		args = strings.TrimSpace(t[i+1:])
	} else {
		name = strings.ToLower(t)
	}
	// Plausible command token only (not a path like /etc/hosts).
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case i > 0 && (r == '_' || r == '-'):
		default:
			return "", "", false
		}
	}
	return name, args, name != ""
}

// submitCommand POSTs /session/{id}/command (MADR 0020 Sprint 5).
// submitCommand posts a slash command as a turn. messageID is the preassigned
// native identity for the resulting user message, or "" to let the engine mint
// one (MADR 0112 A3).
func (o *httpSession) submitCommand(ctx context.Context, messageID, name, arguments string) error {
	body := map[string]any{
		"command":   name,
		"arguments": arguments,
	}
	if messageID != "" {
		body["messageID"] = messageID
	}
	if agent := strings.TrimSpace(o.h.Agent()); agent != "" {
		body["agent"] = agent
	}
	if mp, mid := o.resolveModel(); mid != "" {
		// Command schema takes a free-form model string (provider/id).
		body["model"] = mp + "/" + mid
	}
	// Same reasoning-effort variant the prompt path sends (MADR 0112 A14).
	if v := o.requestVariant(); v != "" {
		body["variant"] = v
	}
	start := time.Now()
	// Bound wait matching the prompt_async timeout in beginTurn. SSE drives
	// EndTurn either way; a command that does not enqueue within 30s is
	// probably wedged.
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := o.h.API()(callCtx, "POST",
		"/session/"+o.h.AgentSessionID()+"/command"+o.dir(), body, nil)
	o.h.Log().Info("opencode command",
		slog.String("agent_session_id", o.h.AgentSessionID()),
		slog.String("command", name),
		slog.Duration("ms", time.Since(start)),
		slog.Bool("ok", err == nil),
	)
	return err
}

// handleSessionDiff maps session.diff SSE → a compact notice (Sprint 5).
func (o *httpSession) handleSessionDiff(props json.RawMessage) {
	var p struct {
		SessionID string `json:"sessionID"`
		Diff      []struct {
			File      string `json:"file"`
			Path      string `json:"path"`
			Status    string `json:"status"`
			Additions int    `json:"additions"`
			Deletions int    `json:"deletions"`
		} `json:"diff"`
	}
	if json.Unmarshal(props, &p) != nil || len(p.Diff) == 0 {
		return
	}
	_ = p.SessionID
	lines := make([]string, 0, len(p.Diff)+1)
	lines = append(lines, fmt.Sprintf("Diff · %d file(s)", len(p.Diff)))
	const maxFiles = 12
	for i, d := range p.Diff {
		if i >= maxFiles {
			lines = append(lines, fmt.Sprintf("…and %d more", len(p.Diff)-maxFiles))
			break
		}
		path := firstNonEmpty(d.File, d.Path, "?")
		st := d.Status
		if st == "" {
			st = "modified"
		}
		lines = append(lines, fmt.Sprintf("  %s  +%d −%d  %s", st, d.Additions, d.Deletions, path))
	}
	o.h.Emit(event.Event{Type: event.TypeNotice, Text: strings.Join(lines, "\n")})
}

// handleCommandExecuted is a thin notice when the engine reports a slash
// command was applied.
func (o *httpSession) handleCommandExecuted(props json.RawMessage) {
	var p struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	if json.Unmarshal(props, &p) != nil || p.Name == "" {
		return
	}
	msg := "Command /" + p.Name
	if strings.TrimSpace(p.Arguments) != "" {
		msg += " " + clip(p.Arguments, 80)
	}
	o.h.Emit(event.Event{Type: event.TypeNotice, Text: msg})
}

// emitCommands advertises a command list, skipping an advertisement identical
// to the last one sent for this session (MADR 0137 F2).
//
// Engines re-send the full list on turn boundaries whether or not it changed —
// grok managed 22 repeats in a single `hi` turn — and each repeat crosses the
// websocket, lands in session history and re-renders on the phone while
// carrying no news. The first advertisement is always sent, including an empty
// one: "this session offers no commands" is a fact a client needs told once.
func (o *httpSession) emitCommands(cmds []event.AvailableCommand) {
	o.mu.Lock()
	send := o.cmdDedupe.ShouldEmit(cmds)
	o.mu.Unlock()
	if send {
		o.h.Emit(event.Event{Type: event.TypeAvailableCommands, Commands: cmds})
	}
}

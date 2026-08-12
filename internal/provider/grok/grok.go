// Package grok implements the Grok Build ACP provider adapter as a thin Spec
// over the shared internal/provider/acpagent machinery.
package grok

import (
	"log/slog"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// Config configures the Grok Build ACP provider.
type Config = acpagent.Config

// staticModels is the fallback catalog for when grok is not installed, or its
// process cannot be started to read initialize `_meta.modelState`
// (MADR 0039 D2). It is a floor, never a supplement: once a live catalog
// exists it replaces this outright, because this list goes stale and a picker
// that offers models the agent refuses is worse than a short one.
//
// Measured 2026-08-12 against grok 1.0.3: live availableModels are grok-4.6
// (default) and grok-4.5. grok-code-fast-1 and grok-4 were removed because
// session/set_model rejects them. AllowCustom stays the type-in escape hatch
// for an older install (MADR 0081 P1.1).
var staticModels = []picker.Option{
	{ID: "grok-4.6", Label: "Grok 4.6", Group: "xai"},
	{ID: "grok-4.5", Label: "Grok 4.5", Group: "xai"},
}

// staticModes is grok's plan-mode vocabulary. Grok honors ACP
// session/set_mode — "plan" engages plan mode and "default" leaves it, each
// confirmed by a current_mode_update — but returns no modes from session/new,
// so nothing would advertise the switch without this list. The ids match what
// grok's own TUI toggles between (Shift+Tab / its `/plan` command).
//
// `auto` is deliberately absent here: it is appended by the daemon
// (Spec.SynthesizeAutoMode) and enforced by intercepting the permission
// request, because grok's own auto is `--permission-mode auto` — a flag on the
// process, not an ACP mode id, and not switchable per session (MADR 0049).
var staticModes = []event.SessionMode{
	{ID: "default", Name: "Build", Description: "Full tool access; edits allowed"},
	{ID: "plan", Name: "Plan", Description: "Research and plan only; no edits"},
}

// spec describes how to launch grok in ACP-stdio mode.
var spec = acpagent.Spec{
	ID:              provider.IDGrok,
	DefaultBin:      "grok",
	DefaultArgs:     defaultArgs,
	AuthStatus:      authStatus,
	SetCredential:   setCredential,
	ClearCredential: clearCredential,
	StartDeviceAuth: startDeviceAuth,
	// Per-session model override: rebuild the default args with the model
	// flag and reasoning effort (custom Args are intentionally not preserved
	// here — pre-refactor behavior; ReasoningEffort and policy flags are typed and preserved).
	ModelArgs: func(cfg Config, model string) []string {
		return defaultArgs(Config{
			AlwaysApprove:    cfg.AlwaysApprove,
			Model:            model,
			ReasoningEffort:  cfg.ReasoningEffort,
			PermissionMode:   cfg.PermissionMode,
			Sandbox:          cfg.Sandbox,
			AllowedTools:     cfg.AllowedTools,
			DisallowedTools:  cfg.DisallowedTools,
			AllowRules:       cfg.AllowRules,
			DenyRules:        cfg.DenyRules,
			NoSubagents:      cfg.NoSubagents,
			DisableWebSearch: cfg.DisableWebSearch,
		})
	},
	StaticModels:       staticModels,
	StaticModes:        staticModes,
	DefaultModeID:      "default",
	SynthesizeAutoMode: true,
	Commands:           commandTable,
	CommandCaveat:      commandCaveat,
	ExtensionNotifications: map[string]acpagent.ExtensionNotificationHandler{
		// 1.0.3 emits the slash form as a notification (no id). The
		// underscore key is the 0039 name, kept so a spelling revert
		// does not drop the catalog again (MADR 0081 P1.2).
		"_x.ai/models/update":     acpagent.HandleModelsUpdate,
		"_x.ai/models_update":     acpagent.HandleModelsUpdate,
		"_x.ai/mcp/server_status": acpagent.HandleMCPStatus,
		"_x.ai/mcp_initialized":   acpagent.HandleMCPInit,
		// grok reports a full sub-agent lifecycle here — spawned / progress /
		// finished, with a terminal status. Unregistered until MADR 0051, so
		// every sub-agent grok ran was invisible while its output filled the
		// transcript.
		"_x.ai/session_notification": acpagent.HandleXAISessionNotification,
	},
}

// Provider is the Grok Build ACP adapter.
type Provider = acpagent.Provider

// New creates a Grok provider with defaults for empty fields.
func New(cfg Config) *Provider {
	return acpagent.New(spec, cfg)
}

// NewWithLogger is like New but sets a logger.
func NewWithLogger(cfg Config, log *slog.Logger) *Provider {
	return acpagent.NewWithLogger(spec, cfg, log)
}

// defaultArgs builds `grok <globals…> agent --no-leader stdio`.
//
// grok's own flags are global: they belong *before* the `agent` subcommand.
// `grok agent` accepts only a small set (see its --help) and rejects the rest
// outright — placing them after `agent` made seven config options fail session
// start with `error: unexpected argument` (MADR 0050 D1). `--always-approve`,
// `-m` and `--reasoning-effort` are valid in both positions; they live with
// the other globals so there is one rule rather than an exception list.
//
// Verified against grok 1.0.3. If grok relocates a flag again, the live argv
// test is what notices — a unit test asserting this slice cannot, because it
// only checks what we build. --no-auto-update is unconditional and global
// (accepted before agent, rejected after it); official ACP/headless guidance
// so a daemon-spawned grok does not self-update (MADR 0081 P1.3).
func defaultArgs(cfg Config) []string {
	var args []string
	if cfg.Model != "" {
		args = append(args, "-m", cfg.Model)
	}
	if cfg.ReasoningEffort != "" {
		args = append(args, "--reasoning-effort", cfg.ReasoningEffort)
	}
	if cfg.AlwaysApprove {
		args = append(args, "--always-approve")
	}
	if cfg.PermissionMode != "" {
		args = append(args, "--permission-mode", cfg.PermissionMode)
	}
	if cfg.Sandbox != "" {
		args = append(args, "--sandbox", cfg.Sandbox)
	}
	if len(cfg.AllowedTools) > 0 {
		args = append(args, "--tools", strings.Join(cfg.AllowedTools, ","))
	}
	if len(cfg.DisallowedTools) > 0 {
		args = append(args, "--disallowed-tools", strings.Join(cfg.DisallowedTools, ","))
	}
	for _, r := range cfg.AllowRules {
		args = append(args, "--allow", r)
	}
	for _, r := range cfg.DenyRules {
		args = append(args, "--deny", r)
	}
	if cfg.NoSubagents {
		args = append(args, "--no-subagents")
	}
	if cfg.DisableWebSearch {
		args = append(args, "--disable-web-search")
	}
	return append(args, "--no-auto-update", "agent", "--no-leader", "stdio")
}

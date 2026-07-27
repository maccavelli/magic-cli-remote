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

// staticModels is the fallback catalog when no live session has populated the
// provider cache via initialize _meta.modelState (MADR 0039 D2).
var staticModels = []picker.Option{
	{ID: "grok-code-fast-1", Label: "Grok Code Fast 1", Group: "xai"},
	{ID: "grok-4", Label: "Grok 4", Group: "xai"},
	{ID: "grok-3", Label: "Grok 3", Group: "xai"},
	{ID: "grok-3-mini", Label: "Grok 3 Mini", Group: "xai"},
}

// staticModes is grok's plan-mode vocabulary. Grok honors ACP
// session/set_mode — "plan" engages plan mode and "default" leaves it, each
// confirmed by a current_mode_update — but returns no modes from session/new,
// so nothing would advertise the switch without this list. The ids match what
// grok's own TUI toggles between (Shift+Tab / its `/plan` command).
var staticModes = []event.SessionMode{
	{ID: "default", Name: "Build", Description: "Full tool access; edits allowed"},
	{ID: "plan", Name: "Plan", Description: "Research and plan only; no edits"},
}

// spec describes how to launch grok in ACP-stdio mode.
var spec = acpagent.Spec{
	ID:          provider.IDGrok,
	DefaultBin:  "grok",
	DefaultArgs: defaultArgs,
	// Per-session model override: rebuild the default args with the model
	// flag and reasoning effort (custom Args are intentionally not preserved
	// here — pre-refactor behavior; ReasoningEffort and policy flags are typed and preserved).
	ModelArgs: func(cfg Config, model string) []string {
		return defaultArgs(Config{
			AlwaysApprove:    cfg.AlwaysApprove,
			Model:            model,
			ReasoningEffort:  cfg.ReasoningEffort,
			PermissionMode:   cfg.PermissionMode,
			AllowedTools:     cfg.AllowedTools,
			DisallowedTools:  cfg.DisallowedTools,
			AllowRules:       cfg.AllowRules,
			DenyRules:        cfg.DenyRules,
			NoSubagents:      cfg.NoSubagents,
			DisableWebSearch: cfg.DisableWebSearch,
		})
	},
	StaticModels:  staticModels,
	StaticModes:   staticModes,
	DefaultModeID: "default",
	Commands:      commandTable,
	CommandCaveat: commandCaveat,
	ExtensionNotifications: map[string]acpagent.ExtensionNotificationHandler{
		"_x.ai/models_update":     acpagent.HandleModelsUpdate,
		"_x.ai/mcp/server_status": acpagent.HandleMCPStatus,
		"_x.ai/mcp_initialized":   acpagent.HandleMCPInit,
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

func defaultArgs(cfg Config) []string {
	// Global flags before the stdio subcommand.
	args := []string{"agent", "--no-leader"}
	if cfg.AlwaysApprove {
		args = append(args, "--always-approve")
	}
	if cfg.Model != "" {
		args = append(args, "-m", cfg.Model)
	}
	if cfg.ReasoningEffort != "" {
		args = append(args, "--reasoning-effort", cfg.ReasoningEffort)
	}
	if cfg.PermissionMode != "" {
		args = append(args, "--permission-mode", cfg.PermissionMode)
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
	args = append(args, "stdio")
	return args
}

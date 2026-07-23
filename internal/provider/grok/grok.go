// Package grok implements the Grok Build ACP provider adapter as a thin Spec
// over the shared internal/provider/acpagent machinery.
package grok

import (
	"log/slog"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// Config configures the Grok Build ACP provider.
type Config = acpagent.Config

// staticModels is a best-effort catalog for the model picker. Grok Build does
// not expose a stable list API over ACP; AllowCustom lets users type any -m
// value the host grok accepts.
var staticModels = []picker.Option{
	{ID: "grok-code-fast-1", Label: "Grok Code Fast 1", Group: "xai"},
	{ID: "grok-4", Label: "Grok 4", Group: "xai"},
	{ID: "grok-3", Label: "Grok 3", Group: "xai"},
	{ID: "grok-3-mini", Label: "Grok 3 Mini", Group: "xai"},
}

// spec describes how to launch grok in ACP-stdio mode.
var spec = acpagent.Spec{
	ID:          provider.IDGrok,
	DefaultBin:  "grok",
	DefaultArgs: defaultArgs,
	// Per-session model override: rebuild the default args with the model
	// flag (custom Args are intentionally not preserved here — pre-refactor
	// behavior).
	ModelArgs: func(cfg Config, model string) []string {
		return defaultArgs(Config{
			AlwaysApprove: cfg.AlwaysApprove,
			Model:         model,
		})
	},
	StaticModels: staticModels,
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
	args = append(args, "stdio")
	return args
}

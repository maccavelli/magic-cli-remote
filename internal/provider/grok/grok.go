// Package grok implements the Grok Build ACP provider adapter as a thin Spec
// over the shared internal/provider/acpagent machinery.
package grok

import (
	"log/slog"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// Config configures the Grok Build ACP provider.
type Config = acpagent.Config

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

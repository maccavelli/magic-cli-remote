// Package opencode implements the OpenCode ACP provider adapter as a thin
// Spec over the shared internal/provider/acpagent machinery.
//
// OpenCode (opencode.ai) speaks ACP natively via `opencode acp` — JSON-RPC
// over stdio, the same surface as grok. Models are not a CLI flag: OpenCode
// advertises a "model" session config option (select of provider/model ids,
// e.g. "anthropic/claude-sonnet-4-5") that is applied after session/new. See
// docs/0011-opencode-provider-plan.md.
package opencode

import (
	"context"
	"fmt"
	"log/slog"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// Config configures the OpenCode ACP provider.
type Config = acpagent.Config

// staticModelOptions is the offline catalog for ACP and HTTP fallback.
// Live HTTP ListModels merges the engine /provider list over this.
func staticModelOptions() []picker.Option {
	opts := make([]picker.Option, 0, len(zenFallbackModels))
	for _, id := range zenFallbackModels {
		opts = append(opts, picker.Option{
			ID:    "opencode/" + id,
			Label: id,
			Group: "opencode",
		})
	}
	// Common third-party ids users pin in config (still AllowCustom for the rest).
	extras := []picker.Option{
		{ID: "anthropic/claude-sonnet-4-5", Label: "Claude Sonnet 4.5", Group: "anthropic"},
		{ID: "anthropic/claude-haiku-4-5", Label: "Claude Haiku 4.5", Group: "anthropic"},
		{ID: "openai/gpt-5", Label: "GPT-5", Group: "openai"},
	}
	return append(opts, extras...)
}

// spec describes how to launch opencode in ACP-stdio mode.
var spec = acpagent.Spec{
	ID:          provider.IDOpencode,
	DefaultBin:  "opencode",
	DefaultArgs: func(Config) []string { return []string{"acp"} },
	// No ModelArgs: opencode takes no model flag; models are applied via the
	// ACP session config option below.
	ConfigureSession: configureSession,
	StaticModels:     staticModelOptions(),
}

// Provider is the OpenCode ACP adapter.
type Provider = acpagent.Provider

// New creates an OpenCode provider with defaults for empty fields.
func New(cfg Config) *Provider {
	return acpagent.New(spec, cfg)
}

// NewWithLogger is like New but sets a logger.
func NewWithLogger(cfg Config, log *slog.Logger) *Provider {
	return acpagent.NewWithLogger(spec, cfg, log)
}

// modelConfigID is the session config option OpenCode uses for model choice.
const modelConfigID = "model"

// configureSession applies the requested model (per-session override first,
// then config default) through OpenCode's "model" session config option. A
// missing option is a warning, not a failure — the agent default applies; a
// rejected value fails Start so the user is not silently on the wrong model.
func configureSession(
	ctx context.Context,
	conn *acp.ClientSideConnection,
	resp acp.NewSessionResponse,
	opts provider.StartOptions,
	cfg Config,
	log *slog.Logger,
) error {
	model := opts.Model
	if model == "" {
		model = cfg.Model
	}
	if model == "" {
		return nil
	}
	for _, o := range resp.ConfigOptions {
		if o.Select == nil || string(o.Select.Id) != modelConfigID {
			continue
		}
		if string(o.Select.CurrentValue) == model {
			return nil
		}
		_, err := conn.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{
			ValueId: &acp.SetSessionConfigOptionValueId{
				ConfigId:  o.Select.Id,
				SessionId: resp.SessionId,
				Value:     acp.SessionConfigValueId(model),
			},
		})
		if err != nil {
			return fmt.Errorf("set model %q: %w", model, err)
		}
		log.Info("opencode model set", slog.String("model", model))
		return nil
	}
	log.Warn("opencode did not advertise a model config option; agent default applies",
		slog.String("requested", model),
	)
	return nil
}

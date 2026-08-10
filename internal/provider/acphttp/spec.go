package acphttp

import (
	"context"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Spec describes static provider metadata for an ACP-over-HTTP engine.
type Spec struct {
	ID            provider.ID
	DefaultBin    string
	ServeArgs     func(port int) []string
	HealthPath    string
	StaticModels  []picker.Option
	StaticModes   []event.SessionMode
	DefaultModeID string
	Commands      command.Table
	// AuthStatus, when non-nil, reports the agent's upstream credential state
	// (MADR 0074 D3). Nil means the agent contributes no auth block.
	AuthStatus func(ctx context.Context) (provider.AuthState, error)
	// SetCredential, when non-nil, writes an upstream credential to the agent's
	// native store (MADR 0074 D1). Nil means credentials are read-only here.
	SetCredential func(ctx context.Context, upstreamID, methodID, secret string, inputs map[string]string) error
	// ClearCredential removes one. Nil alongside SetCredential.
	ClearCredential func(ctx context.Context, upstreamID string) error
	// SetActiveUpstream repoints the agent at another configured upstream
	// without re-authenticating (MADR 0074 D14). Nil means unsupported.
	SetActiveUpstream func(ctx context.Context, upstreamID string) error
}

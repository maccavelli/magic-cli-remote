package codex

import (
	"context"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
)

// openaiUpstreamID is codex's single upstream.
const openaiUpstreamID = "openai"

// AuthStatus implements [provider.Auth] for Codex (MADR 0074 D3).
//
// Presence of ~/.codex/auth.json is the signal. `codex login status` would be
// more precise — it distinguishes a ChatGPT session from an API key — but it
// spawns a process on every providers.list, and the file answers the question
// the phone actually asks.
//
// The two methods differ in a way that matters operationally: the API key path
// is inert, while the device path DELETES this file the moment it starts, even
// if the user never completes it (observed on codex-cli 0.146.0). That is why
// the device method is marked destructive for the phone and gated by
// MADR 0074 D8 in P9 — a mis-tap must not sign the host out.
func (p *Provider) AuthStatus(ctx context.Context) (provider.AuthState, error) {
	if err := ctx.Err(); err != nil {
		return provider.AuthState{}, err
	}
	status := provider.AuthMissing
	if path, err := credstore.CodexAuthPath(); err == nil && credstore.FileExists(path) {
		status = provider.AuthConfigured
	}
	return provider.AuthState{
		Status:         status,
		ActiveUpstream: openaiUpstreamID,
		Upstreams: []provider.UpstreamAuth{{
			ID:     openaiUpstreamID,
			Label:  "OpenAI / ChatGPT",
			Status: status,
			Methods: []provider.AuthMethod{
				{
					ID:    openaiUpstreamID + ":api",
					Type:  provider.AuthMethodAPIKey,
					Label: "OpenAI API key",
				},
				{
					ID:    openaiUpstreamID + ":device",
					Type:  provider.AuthMethodOAuthDevice,
					Label: "Sign in with ChatGPT (device code)",
				},
			},
		}},
	}, nil
}

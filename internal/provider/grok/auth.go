package grok

import (
	"context"
	"os"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
)

// xaiUpstreamID is grok's single upstream: xAI. Unlike the OpenCode-family
// agents, grok talks to exactly one vendor, so its auth block has one row.
const xaiUpstreamID = "xai"

// AuthStatus implements [provider.Auth] for Grok (MADR 0074 D3).
//
// Two independent credentials satisfy grok, and its documented precedence is
// per-model api_key, then the OAuth session, then XAI_API_KEY. Either being
// present means a turn can run, so either marks the upstream configured.
func authStatus(ctx context.Context) (provider.AuthState, error) {
	if err := ctx.Err(); err != nil {
		return provider.AuthState{}, err
	}
	configured := strings.TrimSpace(os.Getenv("XAI_API_KEY")) != ""
	if !configured {
		if path, err := credstore.GrokAuthPath(); err == nil {
			configured = credstore.FileExists(path)
		}
	}
	status := provider.AuthMissing
	if configured {
		status = provider.AuthConfigured
	}
	return provider.AuthState{
		Status:         status,
		ActiveUpstream: xaiUpstreamID,
		Upstreams: []provider.UpstreamAuth{{
			ID:     xaiUpstreamID,
			Label:  "xAI",
			Status: status,
			Methods: []provider.AuthMethod{
				{
					ID:    xaiUpstreamID + ":api",
					Type:  provider.AuthMethodAPIKey,
					Label: "xAI API key",
				},
				{
					// grok 1.0.0 keeps --device-auth (re-probed after the
					// 0.2.118 -> 1.0.0 bump); wired in P9.
					ID:    xaiUpstreamID + ":device",
					Type:  provider.AuthMethodOAuthDevice,
					Label: "Sign in with xAI (device code)",
				},
			},
		}},
	}, nil
}

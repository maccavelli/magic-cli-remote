package grok

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
)

// xaiUpstreamID is grok's single upstream: xAI. Unlike the OpenCode-family
// agents, grok talks to exactly one vendor, so its auth block has one row.
const xaiUpstreamID = "xai"

// setCredential writes an xAI key into ~/.grok/config.toml (MADR 0074 D1).
//
// The other documented route, XAI_API_KEY in the service environment, would
// need a restart of the daemon by the daemon — so the config file is what
// mcremote writes. Grok's precedence puts the file key first anyway.
func setCredential(ctx context.Context, upstreamID, methodID, secret string, _ map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := credstore.ValidateSecret(secret); err != nil {
		return err
	}
	if upstreamID != "" && upstreamID != xaiUpstreamID {
		return fmt.Errorf("grok has no upstream %q", upstreamID)
	}
	// The key write serves exactly one method (MADR 0083 D2); the device
	// method has its own path (StartDeviceAuth).
	if m := strings.TrimSpace(methodID); m != "" && m != xaiUpstreamID+":api" {
		return fmt.Errorf("grok method %q: %w", m, provider.ErrAuthMethodUnsupported)
	}
	path, err := credstore.GrokConfigPath()
	if err != nil {
		return err
	}
	return credstore.SetGrokModelAPIKey(path, secret)
}

// clearCredential removes the api_key line. The OAuth session in auth.json is
// deliberately left alone: clearing a pasted key should not also sign the host
// out of a browser login it never touched.
func clearCredential(ctx context.Context, upstreamID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if upstreamID != "" && upstreamID != xaiUpstreamID {
		return fmt.Errorf("grok has no upstream %q", upstreamID)
	}
	path, err := credstore.GrokConfigPath()
	if err != nil {
		return err
	}
	return credstore.ClearGrokModelAPIKey(path)
}

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

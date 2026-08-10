package goose

import (
	"context"
	"fmt"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
)

// AuthStatus implements [provider.Auth] for Goose (MADR 0074 D3).
//
// Goose keeps secrets in the OS keyring and per-provider token files, neither
// of which mcremote reads. What config.yaml does give — the configured
// provider set and which one is active — is exactly what the MADR 0073 hang
// needed: the phone could not see that four other upstreams were already
// authenticated and ready to take over from a quota-blocked one.
//
// Every configured upstream is reported as configured. That is an assumption,
// not an observation: goose writes a provider into config.yaml as part of
// authenticating it, but a revoked token still looks configured here. Auth
// failures surface separately through agenterr on the turn itself.
func authStatus(ctx context.Context) (provider.AuthState, error) {
	if err := ctx.Err(); err != nil {
		return provider.AuthState{}, err
	}
	path, err := credstore.GooseConfigPath()
	if err != nil {
		return provider.AuthState{Status: provider.AuthError}, err
	}
	cfg, err := credstore.ReadGooseConfig(path)
	if err != nil {
		return provider.AuthState{Status: provider.AuthError}, err
	}
	state := provider.AuthState{
		ActiveUpstream: cfg.ActiveProvider,
		Upstreams:      make([]provider.UpstreamAuth, 0, len(cfg.Providers)),
	}
	for _, id := range cfg.Providers {
		state.Upstreams = append(state.Upstreams, provider.UpstreamAuth{
			ID:     id,
			Label:  id,
			Status: provider.AuthConfigured,
			Methods: []provider.AuthMethod{{
				ID:    id + ":api",
				Type:  provider.AuthMethodAPIKey,
				Label: "API key",
			}},
		})
	}
	state.Status = provider.AuthMissing
	if len(state.Upstreams) > 0 {
		state.Status = provider.AuthConfigured
	}
	return state, nil
}

// setActiveUpstream repoints goose at another already-configured provider
// (MADR 0074 D14). This is the operational fix for MADR 0073: when the active
// upstream hits its weekly quota, the phone can move to one of the others
// without any credential work and without SSH.
//
// `goose configure` takes no flags at all, so rewriting the scalar is the only
// non-interactive path (probed 2026-08-10).
func setActiveUpstream(ctx context.Context, upstreamID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := credstore.GooseConfigPath()
	if err != nil {
		return err
	}
	cfg, err := credstore.ReadGooseConfig(path)
	if err != nil {
		return err
	}
	// Refuse a switch to an upstream goose has no configuration for: it would
	// look like it worked and then fail every turn.
	known := false
	for _, id := range cfg.Providers {
		if id == upstreamID {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("goose has no configured provider %q", upstreamID)
	}
	return credstore.SetGooseActiveProvider(path, upstreamID)
}

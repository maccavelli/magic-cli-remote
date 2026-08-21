package grok

import (
	"context"
	"fmt"
	"os"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
)

// configuredMethods reports which Grok auth methods currently hold a
// credential this daemon can see and remove (MADR 0074 P18 step 12).
//
// Grok is the reason this is per-method rather than per-upstream: a host can
// hold a config.toml API key and an auth.json OAuth session simultaneously, and
// each clears independently. The XAI_API_KEY environment fallback is
// deliberately excluded — it can make the upstream configured, but the daemon
// does not own its own service environment and cannot remove it, so reporting
// it as a configured method would offer a removal that silently does nothing.
func configuredMethods() (apiKey, device bool) {
	if path, err := credstore.GrokConfigPath(); err == nil {
		apiKey = credstore.HasGrokConfigAPIKey(path)
	}
	if path, err := credstore.GrokAuthPath(); err == nil {
		if b, err := os.ReadFile(path); err == nil { //nolint:gosec // effective grok home
			if _, err := NewCredentialAdapter("grok").Validate(context.Background(), b); err == nil {
				device = true
			}
		}
	}
	return apiKey, device
}

// ClearCredentialMethod implements [provider.AuthMethodClearer] for Grok.
//
// The two methods clear different files and must not disturb each other:
// removing a pasted API key should never sign the host out of a browser login
// it never touched, and signing out should never delete a key (P18 step 10).
func (c *CoordinatedProvider) ClearCredentialMethod(ctx context.Context, upstreamID, methodID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if upstreamID != "" && upstreamID != xaiUpstreamID {
		return fmt.Errorf("grok has no upstream %q", upstreamID)
	}
	switch methodID {
	case xaiUpstreamID + ":api":
		// Delegate to the existing key-clearing path so the model table is
		// resolved exactly as it is for an ordinary clear; only the OAuth
		// session is handled differently here.
		if c.Provider == nil {
			return fmt.Errorf("grok: provider is not constructed")
		}
		return c.Provider.ClearCredential(ctx, upstreamID)
	case xaiUpstreamID + ":device":
		return c.clearOAuthSession(ctx)
	default:
		return fmt.Errorf("grok has no clearable auth method %q", methodID)
	}
}

// clearOAuthSession removes only the OAuth session, under the coordinator so it
// cannot race a concurrent login or refresh.
//
// grok logout clears the local file through AuthManager::clear() and makes no
// revoke request, so unlike Codex this is not a point of no return and the
// retained generations stay usable (MADR 0074 §17.5).
func (c *CoordinatedProvider) clearOAuthSession(ctx context.Context) error {
	if c.coord == nil {
		return fmt.Errorf("grok: provider was not built with a credential coordinator")
	}
	return c.coord.RecordLogout(ctx)
}

var _ provider.AuthMethodClearer = (*CoordinatedProvider)(nil)

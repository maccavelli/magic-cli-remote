package grok

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// authModeOAuth is a cached grok session.
//
// Unlike Codex, `grok logout` clears the local file through
// AuthManager::clear() and makes no revoke request, so a Grok generation stays
// usable after a logout attempt and a clone-and-verify probe is safe
// (MADR 0074 §17.5, verified against grok-build 1.0.5).
const authModeOAuth = "oauth"

// maxCredentialBytes bounds a staged candidate.

// CredentialAdapter is the Grok half of a credential transaction
// (MADR 0074 D21/D22).
type CredentialAdapter struct {
	id string
	// probe is the provider's cached-token initialization check, injected
	// because it needs an ACP transport the adapter does not own.
	probe func(ctx context.Context, home string) error
}

// NewCredentialAdapter builds the Grok adapter.
func NewCredentialAdapter(id string) *CredentialAdapter { return &CredentialAdapter{id: id} }

// ProviderID implements [providerauth.Adapter].
func (a *CredentialAdapter) ProviderID() string { return a.id }

// LivePath is <GrokHome>/auth.json, resolved from GROK_HOME when set.
func (a *CredentialAdapter) LivePath() (string, error) { return credstore.GrokAuthPath() }

// NativeLockPath is the sibling lock grok's own writer already honors, so an
// mcremote publication serializes against a concurrent refresh rather than
// racing it (MADR 0074 F12/D25).
func (a *CredentialAdapter) NativeLockPath() (string, error) { return credstore.GrokAuthLockPath() }

// CandidateName is the credential filename inside a pending home.
func (a *CredentialAdapter) CandidateName() string { return "auth.json" }

// MaxCandidateBytes bounds a staged candidate.
func (a *CredentialAdapter) MaxCandidateBytes() int64 { return providerauth.MaxCredentialBytes }

// PendingEnv points a grok child at an isolated home.
func (a *CredentialAdapter) PendingEnv(home string) []string {
	return []string{credstore.GrokHomeEnv(home)}
}

// grokEntry is the subset of one auth.json entry this adapter reads. Grok keys
// entries by "<oidc issuer>::<client id>".
type grokEntry struct {
	Key          string `json:"key"`
	AuthMode     string `json:"auth_mode"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
}

// Validate parses credential bytes and reports the newest usable entry.
func (a *CredentialAdapter) Validate(_ context.Context, data []byte) (providerauth.CredentialMeta, error) {
	var entries map[string]grokEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		// Deliberately does not echo the input: it is credential material.
		return providerauth.CredentialMeta{}, fmt.Errorf("grok: credential is not valid JSON")
	}

	var best providerauth.CredentialMeta
	var found bool
	for _, e := range entries {
		if strings.TrimSpace(e.Key) == "" && strings.TrimSpace(e.RefreshToken) == "" {
			continue
		}
		meta := providerauth.CredentialMeta{Mode: authModeOAuth}
		if e.ExpiresAt != "" {
			if ts, err := time.Parse(time.RFC3339, e.ExpiresAt); err == nil {
				meta.ExpiresAt = ts
			}
		}
		// Several entries can coexist; the furthest expiry represents the
		// file's freshness, matching grok's own refusal to persist an older
		// expiry over a newer one.
		if !found || meta.ExpiresAt.After(best.ExpiresAt) {
			best = meta
			found = true
		}
	}
	if !found {
		return providerauth.CredentialMeta{}, fmt.Errorf("grok: credential contains no usable auth material")
	}
	return best, nil
}

// Probe validates a staged candidate. Grok's cached-token initialization probe
// needs a running ACP transport, which the provider owns rather than the
// adapter; the provider injects it through ProbeFunc when available.
func (a *CredentialAdapter) Probe(ctx context.Context, home string) error {
	if a.probe == nil {
		return nil
	}
	return a.probe(ctx, home)
}

// SetProbe injects the provider's cached-token initialization probe.
func (a *CredentialAdapter) SetProbe(fn func(ctx context.Context, home string) error) {
	a.probe = fn
}

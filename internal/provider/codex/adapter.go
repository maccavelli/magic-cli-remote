package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// Codex auth modes, named as Codex names them.
const (
	// authModeChatGPT is an OAuth session. Its refresh token is revoked
	// server-side by any Codex logout, including the implicit one at device
	// login start, so a byte backup of it cannot restore access
	// (MADR 0074 F14).
	authModeChatGPT = "chatgpt"
	// authModeAPIKey is a stored API key. Codex never revokes it, so a clone
	// probe against it is safe.
	authModeAPIKey = "api_key"
)

// maxCredentialBytes bounds a staged candidate. A real auth.json is a few
// hundred bytes; anything approaching this is not a credential.

// codexProbeTimeout bounds `codex login status` in a pending home.
const codexProbeTimeout = providerauth.ProbeTimeout

// CredentialAdapter is the Codex half of a credential transaction
// (MADR 0074 D21/D22). It supplies effective paths, the child environment for
// an isolated home, candidate validation, and the post-write probe. It never
// returns credential bytes and never renders them into a string.
type CredentialAdapter struct {
	id  string
	bin string
}

// NewCredentialAdapter builds the adapter for a Codex binary. bin may be empty
// for path/validation-only use; Probe requires it.
func NewCredentialAdapter(id string, bin ...string) *CredentialAdapter {
	a := &CredentialAdapter{id: id}
	if len(bin) > 0 {
		a.bin = bin[0]
	}
	return a
}

// ProviderID implements [providerauth.Adapter].
func (a *CredentialAdapter) ProviderID() string { return a.id }

// LivePath is <CodexHome>/auth.json, resolved from CODEX_HOME when set.
func (a *CredentialAdapter) LivePath() (string, error) { return credstore.CodexAuthPath() }

// NativeLockPath is the sibling lock every mcremote Codex mutation takes.
func (a *CredentialAdapter) NativeLockPath() (string, error) { return credstore.CodexAuthLockPath() }

// CandidateName is the credential filename inside a pending home.
func (a *CredentialAdapter) CandidateName() string { return "auth.json" }

// MaxCandidateBytes bounds a staged candidate.
func (a *CredentialAdapter) MaxCandidateBytes() int64 { return providerauth.MaxCredentialBytes }

// PendingEnv points a Codex child at an isolated home. The home starts empty,
// which is what stops the child's pre-login logout from revoking the live
// grant (MADR 0074 D22/F14).
func (a *CredentialAdapter) PendingEnv(home string) []string {
	return []string{credstore.CodexHomeEnv(home)}
}

// CheckBackend refuses to open a transaction only when no login could ever
// produce a credential this coordinator can protect.
//
// An externally stored credential is deliberately NOT a refusal. That
// distinction is the 2026-08-21 lockout lesson: a credential we cannot see is
// a reason to tell the operator the truth, not a reason to block the sign-in
// that would replace it with one we can protect (MADR 0074 §15.13).
func (a *CredentialAdapter) CheckBackend() error {
	reality, err := ObserveCredentialStore(context.Background(), a.bin)
	if reality == RealityUnsupported {
		return err
	}
	return nil
}

// Reality reports where this provider's credential actually lives.
func (a *CredentialAdapter) Reality(ctx context.Context) (StoreReality, error) {
	return ObserveCredentialStore(ctx, a.bin)
}

// authDotJSON is the subset of Codex's auth.json this adapter reads. No token
// value is ever copied out of it beyond presence checks.
type authDotJSON struct {
	APIKey *string `json:"OPENAI_API_KEY"`
	Tokens *struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	} `json:"tokens"`
	LastRefresh string `json:"last_refresh"`
}

// Validate parses credential bytes and reports Codex's auth mode.
//
// Tokens win when both are present: that is how Codex itself resolves the mode,
// and it decides revocability, which decides whether a logout can be rehearsed.
func (a *CredentialAdapter) Validate(_ context.Context, data []byte) (providerauth.CredentialMeta, error) {
	var v authDotJSON
	if err := json.Unmarshal(data, &v); err != nil {
		// Deliberately does not echo the input: it is credential material.
		return providerauth.CredentialMeta{}, fmt.Errorf("codex: credential is not valid JSON")
	}

	switch {
	case v.Tokens != nil && (v.Tokens.AccessToken != "" || v.Tokens.RefreshToken != ""):
		meta := providerauth.CredentialMeta{Mode: authModeChatGPT, Revocable: true}
		if v.LastRefresh != "" {
			if ts, err := time.Parse(time.RFC3339, v.LastRefresh); err == nil {
				// Codex's own refresh clock orders two OAuth generations.
				meta.ExpiresAt = ts
			}
		}
		return meta, nil
	case v.APIKey != nil && strings.TrimSpace(*v.APIKey) != "":
		return providerauth.CredentialMeta{Mode: authModeAPIKey}, nil
	default:
		return providerauth.CredentialMeta{}, fmt.Errorf("codex: credential contains no usable auth material")
	}
}

// Probe runs `codex login status` inside the pending home. Exit zero from the
// login command alone is not proof of a usable credential, so publication waits
// on this (MADR 0074 D25).
func (a *CredentialAdapter) Probe(ctx context.Context, home string) error {
	if a.bin == "" {
		// Validation-only adapter: nothing to probe against.
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, codexProbeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, a.bin, "login", "status") //nolint:gosec // bin from provider config
	cmd.Env = append(cmd.Environ(), credstore.CodexHomeEnv(home))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	procutil.SetProcessGroup(cmd)
	if err := cmd.Run(); err != nil {
		// Child output can echo account detail, so it is not quoted here.
		return fmt.Errorf("codex login status failed in the isolated home: %w", err)
	}
	return nil
}

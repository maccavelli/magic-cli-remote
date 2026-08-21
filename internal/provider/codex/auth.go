package codex

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
)

// openaiUpstreamID is codex's single upstream.
const openaiUpstreamID = "openai"

// codexLoginTimeout bounds `codex login --with-api-key`. It is a local
// credential write, not a network login, so it should be near-instant.
const codexLoginTimeout = 30 * time.Second

// SetCredential implements [provider.AuthWriter] for Codex (MADR 0074 D1).
//
// Codex is the one agent with a first-class non-interactive key path, so this
// spawns the CLI rather than writing its store. The secret goes on **stdin**,
// never in argv: argv is world-readable through ps for the life of the process.
func (p *Provider) SetCredential(ctx context.Context, upstreamID, methodID, secret string, _ map[string]string) error {
	if err := credstore.ValidateSecret(secret); err != nil {
		return err
	}
	if upstreamID != "" && upstreamID != openaiUpstreamID {
		return fmt.Errorf("codex has no upstream %q", upstreamID)
	}
	// The key write serves exactly one method (MADR 0083 D2); ChatGPT device
	// sign-in has its own path (StartDeviceAuth, D8-guarded).
	if m := strings.TrimSpace(methodID); m != "" && m != openaiUpstreamID+":api" {
		return fmt.Errorf("codex method %q: %w", m, provider.ErrAuthMethodUnsupported)
	}
	ctx, cancel := context.WithTimeout(ctx, codexLoginTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.cfg.Bin, "login", "--with-api-key")
	cmd.Stdin = strings.NewReader(secret)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	procutil.SetProcessGroup(cmd)
	if err := cmd.Run(); err != nil {
		// The CLI echoes prompts, not the key, but clip anyway rather than
		// forward an unbounded child's output into an error string.
		return fmt.Errorf("codex login --with-api-key: %w: %s", err, clipOutput(out.String()))
	}
	path, err := credstore.CodexAuthPath()
	if err != nil || !credstore.FileExists(path) {
		return fmt.Errorf("codex: %w", provider.ErrCredentialNotAccepted)
	}
	return nil
}

// ClearCredential implements [provider.AuthWriter] via `codex logout`.
func (p *Provider) ClearCredential(ctx context.Context, upstreamID string) error {
	if upstreamID != "" && upstreamID != openaiUpstreamID {
		return fmt.Errorf("codex has no upstream %q", upstreamID)
	}
	ctx, cancel := context.WithTimeout(ctx, codexLoginTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.cfg.Bin, "logout")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	procutil.SetProcessGroup(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("codex logout: %w: %s", err, clipOutput(out.String()))
	}
	return nil
}

// clipOutput bounds child output quoted into an error.
func clipOutput(s string) string {
	s = strings.TrimSpace(s)
	const max = 400
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

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
	// Which method wrote the one native credential decides which method is
	// reported configured; file presence alone would mark both (P18 step 12).
	//
	// modeKnown matters as much as the flags. A host whose auth.json is the
	// stub `{}` — Codex keeping the session elsewhere — yields no mode, and
	// reporting both methods as definitively unconfigured there would tell the
	// phone that a working credential does not exist.
	// Configured means a usable credential, not merely a file.
	//
	// File presence alone was the old test, and it reported this host's
	// three-byte `{}` stub as configured — the phone showed Codex green while
	// the daemon refused to sign in (MADR 0074 §15.13). Parsing costs nothing
	// extra: the mode is needed for the per-method flags anyway.
	var apiConfigured, deviceConfigured, modeKnown bool
	if path, err := credstore.CodexAuthPath(); err == nil && credstore.FileExists(path) {
		switch storedAuthMode(path) {
		case authModeAPIKey:
			status = provider.AuthConfigured
			apiConfigured, modeKnown = true, true
		case authModeChatGPT:
			status = provider.AuthConfigured
			deviceConfigured, modeKnown = true, true
		}
	}
	backupState, recoverable := p.backupProjection(ctx)
	return provider.AuthState{
		Status:            status,
		ActiveUpstream:    openaiUpstreamID,
		BackupState:       backupState,
		RecoveryAvailable: recoverable,
		Upstreams: []provider.UpstreamAuth{{
			ID:     openaiUpstreamID,
			Label:  "OpenAI / ChatGPT",
			Status: status,
			Methods: []provider.AuthMethod{
				{
					ID:              openaiUpstreamID + ":api",
					Type:            provider.AuthMethodAPIKey,
					Label:           "OpenAI API key",
					Configured:      apiConfigured,
					ConfiguredKnown: modeKnown,
				},
				{
					ID:              openaiUpstreamID + ":device",
					Type:            provider.AuthMethodOAuthDevice,
					Label:           "Sign in with ChatGPT (device code)",
					Configured:      deviceConfigured,
					ConfiguredKnown: modeKnown,
				},
			},
		}},
	}, nil
}

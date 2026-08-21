package codex

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// storedAuthMode reports which auth mode the native credential holds, or "" if
// there is none or it cannot be parsed. It never returns credential bytes.
func storedAuthMode(path string) string {
	b, err := os.ReadFile(path) //nolint:gosec // effective codex home path
	if err != nil {
		return ""
	}
	meta, err := NewCredentialAdapter("codex").Validate(context.Background(), b)
	if err != nil {
		return ""
	}
	return meta.Mode
}

// ClearCredentialMethod implements [provider.AuthMethodClearer].
//
// Codex keeps one native credential, so both of its method ids are aliases for
// clearing it. That is a real asymmetry with Grok, whose API key and OAuth
// session live in different files and clear independently (P18 step 10).
func (p *Provider) ClearCredentialMethod(ctx context.Context, upstreamID, methodID string) error {
	if upstreamID != "" && upstreamID != openaiUpstreamID {
		return fmt.Errorf("codex has no upstream %q", upstreamID)
	}
	switch methodID {
	case "", openaiUpstreamID + ":api", openaiUpstreamID + ":device":
	default:
		return fmt.Errorf("codex has no auth method %q", methodID)
	}
	return p.ClearCredential(ctx, upstreamID)
}

// CoordinatedLogout performs a verified logout inside the credential
// transaction (MADR 0074 §17.5, P18 step 11).
//
// The split below is the whole point. For a ChatGPT credential, `codex logout`
// revokes the refresh token server-side before deleting anything (F14), and a
// clone carries the same token — so the "rehearse it on a copy first" design
// destroys the grant before any fingerprint comparison could decide to roll
// back. There is no safe rehearsal, only an ordering: make the tombstone
// durable first, so an interrupted logout is finished by startup recovery
// rather than resurrecting a grant that may already be dead.
//
// An API-key credential is not revoked by Codex at all, so the original
// clone-and-verify probe is retained for it. When the mode cannot be
// determined, the revoking path is chosen: assuming a credential is safe to
// rehearse is the failure that matters.
func (p *Provider) CoordinatedLogout(ctx context.Context) error {
	if p.coord == nil {
		return fmt.Errorf("codex: provider was not built with a credential coordinator")
	}
	live, err := p.coord.LivePath()
	if err != nil {
		return err
	}

	mode := storedAuthMode(live)
	revoking := mode != authModeAPIKey // unknown modes take the safe path

	if revoking {
		// Tombstone first, then invoke. RecordLogout syncs the manifest before
		// removing anything, so a crash between the two is completed by
		// recovery instead of leaving a revoked credential looking valid.
		if err := p.coord.MarkRevoked(ctx); err != nil {
			return err
		}
		if err := p.coord.RecordLogout(ctx); err != nil {
			return err
		}
		// The live file is already gone; this revokes the server-side grant.
		if err := p.ClearCredential(ctx, ""); err != nil {
			// The credential is removed locally and recorded as revoked either
			// way; a failed revoke is reported, not silently swallowed.
			return fmt.Errorf("codex: credential removed but revoke did not confirm: %w", err)
		}
		return nil
	}

	// Non-revoking: verify on a clone, then tombstone and remove.
	if err := p.verifyLogoutOnClone(ctx, live); err != nil {
		return err
	}
	return p.coord.RecordLogout(ctx)
}

// verifyLogoutOnClone runs `codex logout` against an isolated copy and requires
// it to remove the isolated credential. Safe only for a non-revoking
// credential, where the clone shares no server-side grant.
func (p *Provider) verifyLogoutOnClone(ctx context.Context, live string) error {
	data, err := os.ReadFile(live) //nolint:gosec // effective codex home path
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to log out of
		}
		return err
	}
	home, err := os.MkdirTemp("", "mcremote-codex-logout-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(home) }()
	if err := os.Chmod(home, 0o700); err != nil {
		return err
	}
	clone := home + string(os.PathSeparator) + "auth.json"
	if err := os.WriteFile(clone, data, 0o600); err != nil {
		return err
	}
	if err := p.runLogoutIn(ctx, home); err != nil {
		return err
	}
	if _, err := os.Stat(clone); !os.IsNotExist(err) {
		return fmt.Errorf("%w: codex logout left the isolated credential in place",
			providerauth.ErrInvalidCandidate)
	}
	return nil
}

// runLogoutIn invokes `codex logout` with CODEX_HOME pointed at home.
func (p *Provider) runLogoutIn(ctx context.Context, home string) error {
	ctx, cancel := context.WithTimeout(ctx, codexLoginTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.cfg.Bin, "logout") //nolint:gosec // bin from provider config
	cmd.Env = append(cmd.Environ(), credstore.CodexHomeEnv(home))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	procutil.SetProcessGroup(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("codex logout: %w: %s", err, clipOutput(out.String()))
	}
	return nil
}

var _ provider.AuthMethodClearer = (*Provider)(nil)

// SetCredentialCoordinated runs `codex login --with-api-key` inside the
// credential transaction (MADR 0074 D21, P18 step 9).
//
// The key crosses only the child's stdin: never argv, never the environment,
// never a log line, never the manifest, and never an error string. Because
// Codex keeps one native auth.json, an API-key rotation shares the same
// CURRENT/PREVIOUS chain as a device login rather than having its own.
func (p *Provider) SetCredentialCoordinated(ctx context.Context, upstreamID, methodID, secret string) error {
	if p.coord == nil {
		return fmt.Errorf("codex: provider was not built with a credential coordinator")
	}
	if upstreamID != "" && upstreamID != openaiUpstreamID {
		return fmt.Errorf("codex has no upstream %q", upstreamID)
	}
	if m := strings.TrimSpace(methodID); m != "" && m != openaiUpstreamID+":api" {
		return fmt.Errorf("codex has no auth method %q", methodID)
	}
	if strings.TrimSpace(secret) == "" {
		return fmt.Errorf("codex: %w", provider.ErrCredentialNotAccepted)
	}
	if err := NewCredentialAdapter(p.coord.ProviderID(), p.cfg.Bin).CheckBackend(); err != nil {
		return err
	}

	txn, err := p.coord.Begin(ctx, providerauth.SourceAPIKey)
	if err != nil {
		return err
	}
	// Any failure past this point abandons the isolated candidate; LIVE is
	// never touched until Commit.
	commit := false
	defer func() {
		if !commit {
			_ = p.coord.Abort(context.WithoutCancel(ctx), txn)
		}
	}()

	if err := p.runAPIKeyLoginIn(ctx, txn.Home(), secret); err != nil {
		return err
	}
	if err := p.coord.StageCandidate(ctx, txn); err != nil {
		return err
	}
	if err := p.coord.ValidateCandidate(ctx, txn); err != nil {
		return err
	}
	if err := p.coord.Commit(ctx, txn); err != nil {
		return err
	}
	commit = true
	return nil
}

// runAPIKeyLoginIn pipes the key to the child's stdin with CODEX_HOME pointed
// at the isolated home.
func (p *Provider) runAPIKeyLoginIn(ctx context.Context, home, secret string) error {
	ctx, cancel := context.WithTimeout(ctx, codexLoginTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.cfg.Bin, "login", "--with-api-key") //nolint:gosec // bin from provider config
	cmd.Env = append(cmd.Environ(), credstore.CodexHomeEnv(home))
	cmd.Stdin = strings.NewReader(secret)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	procutil.SetProcessGroup(cmd)
	if err := cmd.Run(); err != nil {
		// The CLI echoes prompts rather than the key, but clip anyway rather
		// than forward an unbounded child's output.
		return fmt.Errorf("codex login --with-api-key: %w: %s", err, clipOutput(out.String()))
	}
	return nil
}

// backupProjection reports the non-secret credential recovery state. An
// uncoordinated provider reports nothing at all, so an older daemon and one
// without a coordinator read identically (MADR 0074 P19 step 5).
func (p *Provider) backupProjection(ctx context.Context) (string, bool) {
	if p.coord == nil {
		return "", false
	}
	st, err := p.coord.Status(ctx)
	if err != nil {
		return "", false
	}
	return string(st.BackupState), st.RecoveryAvailable
}

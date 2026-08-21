package providerauth

import (
	"context"
	"errors"
	"time"
)

// Transaction-core sentinel errors (MADR 0074 D21/P17 step 10). Callers
// classify with errors.Is; provider.ErrAuthBusy remains the distinct
// live-session quiescence signal and is deliberately not reused here.
var (
	// ErrTransactionBusy means another managed mutation already owns this
	// provider's transaction slot.
	ErrTransactionBusy = errors.New("provider auth: a credential transaction is already in progress")
	// ErrConflict means LIVE changed between Begin and the publication check,
	// so another writer won and this candidate must not overwrite it.
	ErrConflict = errors.New("provider auth: live credential changed during the transaction")
	// ErrUnsupportedBackend means the provider stores credentials somewhere
	// this coordinator cannot observe or protect.
	ErrUnsupportedBackend = errors.New("provider auth: unsupported credential backend")
	// ErrInvalidCandidate means a staged candidate failed a structural,
	// bounds, ownership, or provider validation check.
	ErrInvalidCandidate = errors.New("provider auth: invalid credential candidate")
	// ErrRecoveryRequired means durable state is ambiguous and an explicit
	// operator decision is needed; no automatic mutation is permitted.
	ErrRecoveryRequired = errors.New("provider auth: credential recovery required")
)

// CredentialMeta is what an Adapter may report about credential bytes. It
// carries no token, path, or raw payload: the coordinator makes retention and
// freshness decisions from metadata alone (MADR 0074 D21/D24).
type CredentialMeta struct {
	// Mode is the provider's own auth mode, such as "chatgpt" or "api_key".
	Mode string
	// Sequence orders two credentials of the same mode when the provider
	// exposes a monotonic value (an expiry, a refresh counter). Zero means the
	// provider offers no ordering and the coordinator must not infer one.
	Sequence int64
	// ExpiresAt is the credential's own expiry when the provider states one.
	ExpiresAt time.Time
	// Revocable reports whether invalidating this credential would revoke a
	// server-side grant rather than merely deleting a local file. Codex
	// ChatGPT credentials are revocable; API keys are not (MADR 0074 F14).
	Revocable bool
}

// Fresher reports whether candidate is strictly newer than existing. It is
// deliberately conservative: without a comparable ordering signal the answer is
// false, so an autonomous refresh is never promoted on a guess (D24).
func (m CredentialMeta) Fresher(existing CredentialMeta) bool {
	if m.Mode != existing.Mode {
		return false
	}
	if !m.ExpiresAt.IsZero() && !existing.ExpiresAt.IsZero() {
		return m.ExpiresAt.After(existing.ExpiresAt)
	}
	if m.Sequence != 0 && existing.Sequence != 0 {
		return m.Sequence > existing.Sequence
	}
	return false
}

// Adapter is the provider-specific half of a credential transaction. Every
// method returns metadata, paths the daemon already owns, or errors — never
// credential bytes, and no implementation may render bytes into a string or
// log line (MADR 0074 D21).
type Adapter interface {
	// ProviderID is the stable directory name under <DataDir>/provider-auth.
	ProviderID() string
	// LivePath is the effective native credential file, resolved from the
	// provider's own home variable rather than a hardcoded $HOME (D22).
	LivePath() (string, error)
	// NativeLockPath is the sibling lock the provider's own writer honors, so
	// mcremote serializes against it instead of racing it (D25).
	NativeLockPath() (string, error)
	// CandidateName is the credential's filename inside a pending home.
	CandidateName() string
	// MaxCandidateBytes bounds a staged candidate.
	MaxCandidateBytes() int64
	// PendingEnv is the environment overlay pointing a child at an isolated
	// home. The home always starts empty of credential material (D22/F14).
	PendingEnv(home string) []string
	// Validate parses credential bytes and reports metadata, or an error if
	// the bytes are not a usable credential for this provider.
	Validate(ctx context.Context, data []byte) (CredentialMeta, error)
	// Probe verifies a staged candidate works, using the provider's own
	// status check inside the isolated home. Exit zero alone is not proof of
	// a usable credential (D25).
	Probe(ctx context.Context, home string) error
}

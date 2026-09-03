package codex

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// StoreReality is where Codex's credential actually is, as observed rather
// than as configured (MADR 0074 §15.13).
//
// Reading `cli_auth_credentials_store` and defaulting to `file` answers what
// the config says. That is not the same question. The host that produced the
// 2026-08-21 lockout had no such key — so the config said `file` — while its
// `auth.json` was the stub `{}` and the CLI reported a live ChatGPT session.
// Trusting the default there meant "managing" a file that was not the
// credential.
type StoreReality string

const (
	// RealityFileProtected means auth.json holds usable auth material: the
	// coordinator is protecting the thing Codex actually uses.
	RealityFileProtected StoreReality = "file_protected"
	// RealityLoggedOut means Codex has no credential stored at all. Nothing to
	// protect yet, and a login is exactly the right next step. Unreachable
	// before MADR 0136, because the exit-code probe it depended on could not
	// report "not logged in".
	RealityLoggedOut StoreReality = "logged_out"
	// RealityBroken means the file backend holds a credential Codex itself
	// reports as unusable — incomplete, corrupt, or half-written.
	//
	// It is deliberately NOT "unprotectable": the file is the store, mcremote
	// can protect it, and what is wrong is the credential. This is the case
	// MADR 0133 escalates to recovery_required, and conflating it with an
	// external store is what MADR 0134 shipped by mistake.
	RealityBroken StoreReality = "broken"
	// RealityUnsupported means the configured store is not the file backend,
	// so no login will ever produce a protectable file.
	RealityUnsupported StoreReality = "unsupported"
	// RealityUnknown means the CLI could not be probed. Callers fall back to
	// the configured answer rather than inventing one.
	RealityUnknown StoreReality = "unknown"
)

// ObserveCredentialStore reports where the credential actually lives, from the
// CLI's own structured verdict (MADR 0136).
//
// It asks `codex doctor --json` and reads `checks["auth.credentials"]`, which
// reports the RESOLVED storage backend and whether a usable credential is
// stored, as separate facts.
//
// The probe it replaced ran `codex login status` and tested the exit status.
// That status is always zero — a home with no auth.json at all prints
// "Not logged in" and still exits 0 — so the old check reported "authenticated"
// unconditionally, RealityLoggedOut was unreachable, and any unusable auth.json
// was classified as an external store. On the reporting host that silenced a
// genuinely broken credential.
func ObserveCredentialStore(ctx context.Context, bin string) (StoreReality, error) {
	if bin == "" {
		// Nothing to ask. Fall back to what the config says, which is
		// pessimistic about `auto` and blind to profile and -c overrides, but
		// is better than inventing an answer.
		return detectedReality()
	}

	auth, err := probeDoctorAuth(ctx, bin)
	if err != nil {
		// Unreadable or uninterpretable: never guess. The configured answer is
		// the conservative one, and callers treat Unknown as "no external
		// store", which keeps MADR 0133's escalation in place.
		return detectedReality()
	}

	if auth.StorageMode != storageModeFile {
		// A keyring-backed credential cannot be protected by this coordinator
		// whether or not one is currently stored there.
		return RealityUnsupported, fmt.Errorf(
			"%w: codex resolves its credential store to the %s backend, which mcremote cannot protect",
			providerauth.ErrUnsupportedBackend, auth.StorageMode)
	}

	// The file is the store, so the file decides. `auth env vars present` is
	// deliberately not consulted: environment auth is per-process, and the
	// daemon's environment is not the operator's shell.
	if auth.Usable {
		return RealityFileProtected, nil
	}
	if storesNoCredential(auth) {
		return RealityLoggedOut, nil
	}
	// Present but not usable — corrupt, incomplete, or half-written. This is
	// NOT an unprotectable credential; it is the case MADR 0133 escalates.
	return RealityBroken, nil
}

// storageModeFile is the resolved backend this coordinator can protect, as
// `codex doctor --json` spells it (lowercased by the parser).
const storageModeFile = "file"

// storesNoCredential distinguishes "signed out" from "stored but broken".
//
// Codex omits every `stored *` detail when there is nothing stored at all, and
// emits them when there is something to describe, so their absence is the
// signal. Read as "no evidence of stored material" rather than by matching the
// summary text, which carries no stability contract.
func storesNoCredential(auth authCredentials) bool {
	return !auth.HasStoredMaterialEvidence
}

// detectedReality is the no-probe fallback: what config.toml says.
func detectedReality() (StoreReality, error) {
	if _, err := DetectCredentialStore(); err != nil {
		return RealityUnsupported, err
	}
	return RealityUnknown, nil
}

// probeDoctorAuth runs `codex doctor --json` in the effective home and returns
// the auth check.
//
// stdout is captured because the report IS the answer; it is parsed and
// discarded, and nothing from it is logged. The report contains no token
// material — Codex redacts its own sensitive detail values — but it is treated
// as credential-adjacent regardless and never rendered into an error string.
func probeDoctorAuth(ctx context.Context, bin string) (authCredentials, error) {
	ctx, cancel := context.WithTimeout(ctx, providerauth.ProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "doctor", "--json") //nolint:gosec // bin from provider config
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	procutil.SetProcessGroup(cmd)
	// A non-zero exit is not fatal: doctor exits non-zero when it finds
	// problems, which is exactly when this classification matters. The report
	// is what decides, so only an unparseable one is a failure.
	_ = cmd.Run()
	return parseDoctorAuth(out.Bytes())
}

// describeReality is the operator-facing explanation for a non-protected
// store. It names no path and no account.
func describeReality(r StoreReality) string {
	switch r {
	case RealityBroken:
		return "codex has a stored credential that it cannot use; signing in again " +
			"from here will replace it"
	case RealityUnsupported:
		return "codex is configured to store credentials outside auth.json, which " +
			"mcremote cannot back up or restore"
	default:
		return ""
	}
}

// realityWindow bounds how long an observation is reused.
//
// The probe spawns the CLI, and providers.list refreshes status for every
// provider, so an uncached probe turns a routine list into one process per
// provider per refresh. A short window keeps the answer honest without that
// cost: the states it distinguishes change only when someone signs in or out.
const realityWindow = 30 * time.Second

var realityCache struct {
	mu      sync.Mutex
	at      time.Time
	home    string
	reality StoreReality
	err     error
	// refreshing is set while a non-blocking caller has a background probe in
	// flight, so a burst of providers.list calls spawns one, not one each.
	refreshing bool
}

// ObserveCredentialStoreCached is ObserveCredentialStore with a bounded cache.
//
// The cache is keyed on the effective home as well as time, so a test or a
// reconfigured host never reads another home's answer. Pass window 0 to force
// a fresh observation.
func ObserveCredentialStoreCached(ctx context.Context, bin string, window time.Duration) (StoreReality, error) {
	home, err := credstore.CodexHome()
	if err != nil {
		return RealityUnknown, err
	}

	realityCache.mu.Lock()
	defer realityCache.mu.Unlock()
	if window > 0 && realityCache.home == home && !realityCache.at.IsZero() &&
		time.Since(realityCache.at) < window {
		return realityCache.reality, realityCache.err
	}

	reality, obsErr := ObserveCredentialStore(ctx, bin)
	realityCache.at = time.Now()
	realityCache.home = home
	realityCache.reality = reality
	realityCache.err = obsErr
	return reality, obsErr
}

// ObserveCredentialStoreCachedNonBlocking answers from the cache and never
// waits for a probe (MADR 0137 F1).
//
// ObserveCredentialStoreCached runs `codex doctor --json` on a cold cache — a
// subprocess plus a network-dependent call, measured at ~1.4 s. That was
// reachable from `providers.list`, which the phone issues on every connect, so
// a routine screen refresh could block on codex talking to its backend.
//
// A cold cache returns RealityUnknown immediately and starts at most one
// background refresh, so the answer is right on the next call rather than late
// on this one. RealityUnknown is the correct conservative value: every caller
// already treats it as "no reality override", which is exactly the pre-MADR
// 0134 behaviour.
//
// This is deliberately NOT used on the recovery or CheckBackend paths. Those
// are not phone-triggered, they must not act on a stale or unknown answer, and
// MADR 0136's classification depends on a real observation.
func ObserveCredentialStoreCachedNonBlocking(bin string, window time.Duration) StoreReality {
	home, err := credstore.CodexHome()
	if err != nil {
		return RealityUnknown
	}

	realityCache.mu.Lock()
	if realityCache.home == home && !realityCache.at.IsZero() &&
		(window <= 0 || time.Since(realityCache.at) < window) {
		reality, cachedErr := realityCache.reality, realityCache.err
		realityCache.mu.Unlock()
		if cachedErr != nil {
			return RealityUnknown
		}
		return reality
	}
	if realityCache.refreshing {
		realityCache.mu.Unlock()
		return RealityUnknown
	}
	realityCache.refreshing = true
	realityCache.mu.Unlock()

	go func() {
		defer func() {
			realityCache.mu.Lock()
			realityCache.refreshing = false
			realityCache.mu.Unlock()
		}()
		// Its own timeout, not the caller's: the caller has already returned,
		// and a request-scoped context would be cancelled out from under this.
		ctx, cancel := context.WithTimeout(context.Background(), realityProbeTimeout)
		defer cancel()
		_, _ = ObserveCredentialStoreCached(ctx, bin, 0)
	}()
	return RealityUnknown
}

// realityProbeTimeout bounds a background reality probe. Generous, because
// nothing waits on it and a probe that gives up too early just leaves the
// cache cold for another cycle.
const realityProbeTimeout = 30 * time.Second

// InvalidateRealityCache forces the next observation to re-probe. Every managed
// credential mutation calls it, so a sign-in or logout is reflected at once
// rather than after the window.
func InvalidateRealityCache() {
	realityCache.mu.Lock()
	defer realityCache.mu.Unlock()
	realityCache.at = time.Time{}
}

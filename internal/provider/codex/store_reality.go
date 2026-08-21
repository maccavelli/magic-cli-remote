package codex

import (
	"context"
	"os"
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
	// RealityExternal means the CLI is authenticated but not from the file.
	// The credential exists somewhere this coordinator cannot see, so it
	// cannot be backed up — but a fresh login will produce one it can.
	RealityExternal StoreReality = "external"
	// RealityLoggedOut means there is no credential anywhere. Nothing to
	// protect yet, and a login is exactly the right next step.
	RealityLoggedOut StoreReality = "logged_out"
	// RealityUnsupported means the configured store is not the file backend,
	// so no login will ever produce a protectable file.
	RealityUnsupported StoreReality = "unsupported"
	// RealityUnknown means the CLI could not be probed. Callers fall back to
	// the configured answer rather than inventing one.
	RealityUnknown StoreReality = "unknown"
)

// ObserveCredentialStore reports where the credential actually lives.
//
// The probe is a comparison, not a lookup: does the file this coordinator can
// protect contain what the CLI is authenticating with? Only running the CLI
// can answer that, so this spawns `codex login status` in the effective home.
// It is read-only, spends no tokens, and prints nothing.
//
// A non-file configured store short-circuits: it is already conclusive, and no
// probe would change it.
func ObserveCredentialStore(ctx context.Context, bin string) (StoreReality, error) {
	if _, err := DetectCredentialStore(); err != nil {
		// Configured elsewhere: conclusive without probing.
		return RealityUnsupported, err
	}

	usable := fileHoldsUsableCredential(ctx)
	if usable {
		// The file is the credential. Nothing else to establish.
		return RealityFileProtected, nil
	}
	if bin == "" {
		// Without the CLI the two states below are indistinguishable, and
		// guessing is what caused the lockout.
		return RealityUnknown, nil
	}
	if cliIsAuthenticated(ctx, bin) {
		return RealityExternal, nil
	}
	return RealityLoggedOut, nil
}

// fileHoldsUsableCredential reports whether the live auth.json parses to auth
// material this provider understands.
func fileHoldsUsableCredential(ctx context.Context) bool {
	path, err := credstore.CodexAuthPath()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(path) //nolint:gosec // effective codex home
	if err != nil {
		return false
	}
	_, err = NewCredentialAdapter("codex").Validate(ctx, data)
	return err == nil
}

// cliIsAuthenticated runs `codex login status` in the effective home. Exit zero
// means authenticated; the output is never captured into an error or a log,
// because it names the account.
func cliIsAuthenticated(ctx context.Context, bin string) bool {
	ctx, cancel := context.WithTimeout(ctx, providerauth.ProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "login", "status") //nolint:gosec // bin from provider config
	cmd.Stdout = nil
	cmd.Stderr = nil
	procutil.SetProcessGroup(cmd)
	return cmd.Run() == nil
}

// describeReality is the operator-facing explanation for a non-protected
// store. It names no path and no account.
func describeReality(r StoreReality) string {
	switch r {
	case RealityExternal:
		return "codex is authenticated, but its credential is not in the auth.json " +
			"file mcremote can back up; signing in from here will create one it can"
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

// InvalidateRealityCache forces the next observation to re-probe. Every managed
// credential mutation calls it, so a sign-in or logout is reflected at once
// rather than after the window.
func InvalidateRealityCache() {
	realityCache.mu.Lock()
	defer realityCache.mu.Unlock()
	realityCache.at = time.Time{}
}

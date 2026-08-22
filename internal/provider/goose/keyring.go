package goose

import (
	"errors"
	"fmt"
	"os"

	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
)

// GuardOutcome is what a reconciliation decided (MADR 0110 D1/D2/D10).
type GuardOutcome string

const (
	// OutcomeSwitch means the key was reconciled to the configured value.
	OutcomeSwitch GuardOutcome = "switch"
	// OutcomeHold means the file store holds nothing while goose's config
	// declares providers, so switching would start goose with no credentials.
	OutcomeHold GuardOutcome = "hold"
	// OutcomeHostControls means GOOSE_DISABLE_KEYRING is set in the daemon's
	// environment, which already decides the backend for every goose child.
	OutcomeHostControls GuardOutcome = "host_controls"
	// OutcomeOperatorOwned means the config carries a hand-set key mcremote
	// will not remove.
	OutcomeOperatorOwned GuardOutcome = "operator_owned"
	// OutcomeNoChange means the config already matched.
	OutcomeNoChange GuardOutcome = "no_change"
)

// Result is a reconciliation's outcome plus a reason safe to log and to show
// on the phone. Reason never contains a secret or a filesystem path.
type Result struct {
	Outcome GuardOutcome
	Reason  string
}

// Fixed operator-facing strings, so the log and the phone say the same words.
const (
	reasonHold = "goose secrets are not in secrets.yaml, so switching would " +
		"start goose with no credentials; run: GOOSE_DISABLE_KEYRING=1 goose configure"
	reasonHostControls = "GOOSE_DISABLE_KEYRING is set in the daemon environment; " +
		"goose config not modified"
	reasonOperatorOwned = "goose config has a hand-set GOOSE_DISABLE_KEYRING; " +
		"mcremote is not managing it"
	reasonSwitchDisabled = "goose will read secrets.yaml"
	reasonSwitchEnabled  = "goose will use the OS keyring"
)

// EffectiveKeyringDisabled resolves what the backend should be, and whether the
// host has already decided (MADR 0110 D4).
//
// A GOOSE_DISABLE_KEYRING in the daemon's environment is authoritative because
// goose's environment branch is presence-only: mcremote cannot express
// "enabled" through it, so writing a config key the environment overrides would
// be a setting with no effect.
func EffectiveKeyringDisabled(cfgValue bool) (disabled, hostControls bool) {
	if _, ok := os.LookupEnv("GOOSE_DISABLE_KEYRING"); ok {
		return true, true
	}
	return cfgValue, false
}

// Reconcile brings goose's GOOSE_DISABLE_KEYRING key in line with mcremote's
// providers.goose.keyring_disabled setting.
//
// It reads only file names and provider ids — never a secret value — and runs
// no subprocess and no keyring call, so it can never raise the very prompt this
// work exists to remove.
func Reconcile(cfgValue bool) (Result, error) {
	secretsPath, configPath, err := goosePaths()
	if err != nil {
		return Result{}, err
	}

	disabled, hostControls := EffectiveKeyringDisabled(cfgValue)
	if hostControls {
		return Result{Outcome: OutcomeHostControls, Reason: reasonHostControls}, nil
	}

	if disabled && !fileStoreCanServe(secretsPath, configPath) {
		// Switching now would leave goose with no credentials at all. Holding
		// keeps a working host working; the operator is told how to migrate.
		return Result{Outcome: OutcomeHold, Reason: reasonHold}, nil
	}

	changed, err := credstore.SetGooseKeyringDisabled(configPath, disabled)
	if errors.Is(err, credstore.ErrGooseKeyringOperatorOwned) {
		return Result{Outcome: OutcomeOperatorOwned, Reason: reasonOperatorOwned}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("reconcile goose keyring setting: %w", err)
	}
	if !changed {
		return Result{Outcome: OutcomeNoChange}, nil
	}
	reason := reasonSwitchEnabled
	if disabled {
		reason = reasonSwitchDisabled
	}
	return Result{Outcome: OutcomeSwitch, Reason: reason}, nil
}

// fileStoreCanServe reports whether switching to file storage is safe.
//
// Safe means either the file store already holds a secret, or goose has no
// configured provider whose credentials could be stranded. A cold host is the
// second case: there is nothing to lose, and the first login writes the file.
func fileStoreCanServe(secretsPath, configPath string) bool {
	if names, err := credstore.ReadGooseSecretNames(secretsPath); err == nil && len(names) > 0 {
		return true
	}
	cfg, err := credstore.ReadGooseConfig(configPath)
	if err != nil {
		// Unreadable config: assume something is configured and hold, which is
		// the outcome that cannot break a working host.
		return false
	}
	return len(cfg.Providers) == 0 && cfg.ActiveProvider == ""
}

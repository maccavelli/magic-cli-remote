package daemon

import (
	"log/slog"

	"github.com/maccavelli/magic-cli-remote/internal/provider/goose"
)

// reconcileGooseKeyring brings Goose's GOOSE_DISABLE_KEYRING key in line with
// providers.goose.keyring_disabled, and reports what it decided (MADR 0110).
//
// A failure here is deliberately non-fatal. This writes a preference, not a
// credential: a host that cannot be reconciled should still run Goose on
// whatever backend it already had, rather than losing the provider entirely.
func reconcileGooseKeyring(keyringDisabled bool, log *slog.Logger) goose.Result {
	res, err := goose.Reconcile(keyringDisabled)
	if err != nil {
		log.Warn("could not reconcile goose keyring setting; leaving it as it is",
			slog.String("err", err.Error()))
		return res
	}

	switch res.Outcome {
	case goose.OutcomeHold:
		// The one case the operator must act on, and the one they cannot see
		// from the phone unless it is said plainly.
		log.Warn("goose keyring left in place: "+res.Reason,
			slog.String("provider", "goose"))
	case goose.OutcomeHostControls, goose.OutcomeOperatorOwned:
		log.Info(res.Reason, slog.String("provider", "goose"))
	case goose.OutcomeSwitch:
		log.Info("goose secret backend reconciled: "+res.Reason,
			slog.String("provider", "goose"))
	case goose.OutcomeNoChange:
		// Nothing to say: the config already matched.
	}
	return res
}

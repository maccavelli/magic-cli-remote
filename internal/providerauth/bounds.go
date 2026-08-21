package providerauth

import "time"

// Fixed operational bounds (MADR 0074 plan §17.4). These are package constants
// with tests rather than user configuration: this repair is about making
// credential handling deterministic, and a tunable deadline is one more way for
// two hosts to behave differently under the same failure.
//
// Changing a value here is operational tuning. Changing retention count,
// publication conditions, or recovery outcomes requires a MADR amendment.
const (
	// MaxCredentialBytes bounds any LIVE, candidate, or generation file. It is
	// checked before allocation or copy.
	MaxCredentialBytes int64 = 1 << 20

	// LockTimeout bounds coordinator and provider-native lock acquisition.
	// Exceeding it returns ErrTransactionBusy rather than waiting forever
	// behind a crashed or external writer.
	LockTimeout = 5 * time.Second

	// ProbeTimeout bounds a provider validation or logout probe, after which
	// the isolated process group is killed and reaped.
	ProbeTimeout = 30 * time.Second

	// WatchDebounce coalesces create/write/rename bursts on the watched parent
	// directory into one evaluation.
	WatchDebounce = 250 * time.Millisecond

	// StableReadInterval and StableReadDeadline require two identical
	// validated fingerprints before an observation is trusted; failing that,
	// the observation is classified unstable and changes nothing.
	StableReadInterval = 100 * time.Millisecond
	StableReadDeadline = 2 * time.Second

	// ActivationGrace retains an owned validated PENDING while waiting for the
	// provider to go idle. The original flow deadline still caps it.
	ActivationGrace = 5 * time.Minute

	// DrainTimeout bounds each shutdown stage. On expiry the daemon reports
	// retained ownership and preserves disk evidence rather than forcing.
	DrainTimeout = 10 * time.Second
)

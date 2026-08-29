package service

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// launchd teardown is asynchronous: `launchctl bootout` returns before the job
// has left the domain, and a bootstrap issued into that window fails with 37
// (EALREADY, "job still loaded") or 5 (EIO). Both were observed on a real Mac,
// from two call sites that "handled" the asynchrony differently — one slept
// 300ms, the other did nothing at all (MADR 0125 F1/F2/F3).
//
// This is the one place that models the fact. Everything that tears a launchd
// job down goes through it.

// launchdWaitTimeout bounds the wait for a job to leave the domain.
//
// Matched to the 15s active-wait already in update/swap.go rather than
// inventing a second number: both are "wait for launchd to reach a state, then
// give up and say so", and two different budgets for that would be a question
// no reader could answer (MADR 0125 open question 1).
var launchdWaitTimeout = 15 * time.Second

// launchdWaitInterval is how often the domain is re-checked. Short on purpose:
// the previous design paid a flat 300ms on every host, so a fast Mac now
// returns in roughly one poll instead — the wait got *cheaper* as well as
// correct.
var launchdWaitInterval = 50 * time.Millisecond

// ErrLaunchdStillLoaded is returned when a job is still in the domain after
// launchdWaitTimeout. It is deliberately distinguishable: a caller must be
// able to say "teardown did not finish" rather than reporting whatever the
// next command happened to fail with.
var ErrLaunchdStillLoaded = errors.New("launchd job still loaded after teardown wait")

// Seams. Tests drive these; nothing else may.
var (
	launchdSleep = time.Sleep
	launchdNow   = time.Now
)

// launchdLoaded reports whether the label is present in the user domain.
//
// A non-zero `launchctl print` cannot tell "gone" from "never existed" — both
// exit non-zero with no machine-readable distinction (MADR 0125 open question
// 2). For a teardown wait that ambiguity is harmless and the answer is the same
// either way: the job is not in the domain. It is *not* harmless for
// classifying a bootout failure, which is why callers check this first, while
// the job is still there to observe.
func launchdLoaded(svc string) bool {
	_, err := runLaunchctlCapture("print", svc)
	return err == nil
}

// stopAndWaitDarwin boots the job out and waits until it has actually left the
// user domain.
//
// Returns nil when the job is gone — including when it was never loaded, which
// is the common case for a fresh install and is not an error.
func stopAndWaitDarwin(svc string) error {
	// Checked before bootout, not after: this is the only moment at which
	// "bootout failed because there was nothing to boot out" can be told apart
	// from "bootout failed and the job is still running". Discarding that
	// distinction is what let a real failure proceed into a doomed bootstrap
	// (MADR 0125 D3).
	if !launchdLoaded(svc) {
		return nil
	}

	bootoutErr := runLaunchctl("bootout", svc)

	deadline := launchdNow().Add(launchdWaitTimeout)
	for {
		if !launchdLoaded(svc) {
			return nil
		}
		if !launchdNow().Before(deadline) {
			// The job outlived the wait. Report that, and carry the bootout
			// error when there was one — it is usually the reason.
			if bootoutErr != nil {
				return fmt.Errorf("%w: %s (%s): %v",
					ErrLaunchdStillLoaded, svc, launchdWaitTimeout, bootoutErr)
			}
			return fmt.Errorf("%w: %s (%s)",
				ErrLaunchdStillLoaded, svc, launchdWaitTimeout)
		}
		launchdSleep(launchdWaitInterval)
	}
}

// launchdServiceTarget returns the gui/<uid>/<label> target for a product.
func launchdServiceTarget(product string) string {
	return fmt.Sprintf("gui/%d/%s", os.Getuid(), launchdLabel(product))
}

// launchdJobRunning reports whether the job is loaded *and* running, using the
// same state vocabulary as IsActive.
func launchdJobRunning(svc string) bool {
	out, err := runLaunchctlCapture("print", svc)
	if err != nil {
		return false
	}
	if strings.Contains(out, "state = exited") || strings.Contains(out, "state = not running") {
		return false
	}
	return strings.Contains(out, "state = running") || strings.Contains(out, "state = waiting")
}

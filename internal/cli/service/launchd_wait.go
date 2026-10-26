package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/launchd"
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

// launchdLoaded reports whether the job svc names is in its domain.
//
// It reads the library's Job.Loaded, which tells "not loaded" (launchctl's
// exit 113) from a probe that failed, and returns the failure as an error
// (MADR 0207 D19). A non-zero `launchctl print` could not: every failure read
// as "not loaded" (MADR 0125 open question 2), so a probe that failed let a
// teardown be skipped.
func launchdLoaded(svc string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return launchdLoadedProbe(ctx, svc)
}

// launchdLoadedProbe answers launchdLoaded. Tests replace it through
// OverrideLaunchdLoaded.
var launchdLoadedProbe = jobLoaded

// launchdRunner runs launchctl for the jobs launchdJobFor builds. Nil is the
// library's own runner; tests answer with exit codes.
var launchdRunner libservice.Runner

// jobLoaded is Job.Loaded for svc.
func jobLoaded(ctx context.Context, svc string) (bool, error) {
	j, err := launchdJobFor(svc)
	if err != nil {
		return false, err
	}
	return j.Loaded(ctx, "")
}

// launchdJobFor is the library's job for svc, a service target as
// launchdServiceTarget writes it: gui/<uid>/<label>. Loaded reads no plist, so
// the plist is setup's default path whatever directory setup wrote to.
func launchdJobFor(svc string) (*launchd.Job, error) {
	parts := strings.SplitN(svc, "/", 3)
	if len(parts) != 3 || parts[0] != "gui" {
		return nil, fmt.Errorf("launchd service target %q is not gui/<uid>/<label>", svc)
	}
	uid, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("launchd service target %q: %w", svc, err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}
	return launchd.New(launchd.Options{
		Label:  parts[2],
		Domain: launchd.GUI(uid),
		Plist:  filepath.Join(home, "Library", "LaunchAgents", parts[2]+".plist"),
		Runner: launchdRunner,
	})
}

// OverrideLaunchdLoaded replaces launchdLoaded's probe for tests; nil restores
// Job.Loaded. Restore with the returned func.
//
// A teardown asks it whether there is a job to boot out, and when the job has
// gone (MADR 0125 D1). A test that stubs launchctl must stub this too, or it
// reads the real launchd (MADR 0207 D19).
func OverrideLaunchdLoaded(fn func(ctx context.Context, svc string) (bool, error)) (restore func()) {
	prev := launchdLoadedProbe
	if fn == nil {
		fn = jobLoaded
	}
	launchdLoadedProbe = fn
	return func() { launchdLoadedProbe = prev }
}

// stopAndWaitDarwin boots the job out and waits until it has actually left the
// user domain.
//
// Returns nil when the job is gone — including when it was never loaded, which
// is the common case for a fresh install and is not an error. A probe that
// fails is an error: whether to tear down, and whether the teardown finished,
// would otherwise be a guess (MADR 0207 D19).
func stopAndWaitDarwin(svc string) error {
	// Checked before bootout, not after: this is the only moment at which
	// "bootout failed because there was nothing to boot out" can be told apart
	// from "bootout failed and the job is still running". Discarding that
	// distinction is what let a real failure proceed into a doomed bootstrap
	// (MADR 0125 D3).
	loaded, err := launchdLoaded(svc)
	if err != nil {
		return fmt.Errorf("read whether %s is loaded: %w", svc, err)
	}
	if !loaded {
		return nil
	}

	bootoutErr := runLaunchctl("bootout", svc)

	deadline := launchdNow().Add(launchdWaitTimeout)
	for {
		loaded, err := launchdLoaded(svc)
		if err != nil {
			if bootoutErr != nil {
				return fmt.Errorf("read whether %s left the domain after bootout (%v): %w", svc, bootoutErr, err)
			}
			return fmt.Errorf("read whether %s left the domain: %w", svc, err)
		}
		if !loaded {
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

// launchdServiceTarget is the service target of the LaunchAgent labelled
// label in this user's GUI domain.
func launchdServiceTarget(label string) string {
	return fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
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

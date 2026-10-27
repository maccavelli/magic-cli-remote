package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/launchd"
)

// launchd teardown is asynchronous: `launchctl bootout` returns before the job
// has left the domain, and a bootstrap issued into that window fails with 37
// (EALREADY, "job still loaded") or 5 (EIO). Both were observed on a real Mac
// (MADR 0125 F1/F2/F3). The library's launchd.Job models that fact: its Stop
// waits until the job and its process have gone, and setup, the update and
// --remove all stop through it (MADR 0207 D43). What is left here is the
// probe for whether a job is loaded.

// launchdLoaded reports whether the job svc names is in its domain.
//
// It reads the library's Job.Loaded, which tells "not loaded" (launchctl's
// exit 113) from a probe that failed, and returns the failure as an error
// (MADR 0207 D19). A non-zero `launchctl print` could not: every failure read
// as "not loaded" (MADR 0125 open question 2).
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
// setup --no-start asks it whether a running job keeps its old definition. A
// test that stubs launchctl must stub this too, or it reads the real launchd
// (MADR 0207 D19).
func OverrideLaunchdLoaded(fn func(ctx context.Context, svc string) (bool, error)) (restore func()) {
	prev := launchdLoadedProbe
	if fn == nil {
		fn = jobLoaded
	}
	launchdLoadedProbe = fn
	return func() { launchdLoadedProbe = prev }
}

// launchdServiceTarget is the service target of the LaunchAgent labelled
// label in this user's GUI domain.
func launchdServiceTarget(label string) string {
	return fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
}

// Deliberately NOT build-tagged. The launchd path shells out through the
// runLaunchctl / runLaunchctlCapture seams and touches no Darwin API, so it
// compiles and runs everywhere — the same reasoning schtasks.go gives for
// exercising the Windows branch from a Unix host (MADR 0116 D12).
//
// That matters here specifically: the teardown race this file pins was
// reported from a Mac, and the developer host is Windows. A test that only
// runs on Unix would have been unrunnable by the person fixing it.

package service_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

// scriptedPrint answers `launchctl print` from a queue, so a test can model a
// job that is loaded and then finishes tearing down. The last value repeats.
func scriptedPrint(loaded ...bool) func(args ...string) (string, error) {
	i := 0
	return func(args ...string) (string, error) {
		if len(args) == 0 || args[0] != "print" {
			return "", nil
		}
		v := loaded[len(loaded)-1]
		if i < len(loaded) {
			v = loaded[i]
		}
		i++
		if v {
			return "state = running\npid = 42\n", nil
		}
		return "", errors.New("could not find service")
	}
}

func kindsOf(calls [][]string) []string {
	var out []string
	for _, c := range calls {
		if len(c) > 0 {
			out = append(out, c[0])
		}
	}
	return out
}

func newSetupDir(t *testing.T) (src, plistDir, cfg string) {
	t.Helper()
	dir := t.TempDir()
	// Executability is a mode bit on Unix and an extension on Windows
	// (launch_windows.go:94). The name is irrelevant to Setup — only that the
	// host agrees the file can be run.
	name := "mcremote"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	src = filepath.Join(dir, name)
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return src, filepath.Join(dir, "LaunchAgents"), filepath.Join(dir, "config.yaml")
}

// A loaded job must be torn down and observed gone before bootstrap.
// Bootstrapping into an unfinished teardown is what returned error 5 on a real
// Mac (MADR 0125 F3).
func TestDarwinSetupWaitsForTeardownBeforeBootstrap(t *testing.T) {
	restoreOS := service.OverrideInstallOS("darwin")
	defer restoreOS()

	src, plistDir, cfg := newSetupDir(t)
	if err := os.MkdirAll(plistDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Content differs from what Setup renders, so this is a genuine reload.
	if err := os.WriteFile(filepath.Join(plistDir, "com.magiccliremote.mcremote.plist"),
		[]byte("<plist>stale</plist>"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Loaded when asked, loaded once more mid-wait, then gone.
	restoreCap := service.OverrideRunLaunchctlCapture(scriptedPrint(true, true, false, false))
	defer restoreCap()

	var calls [][]string
	restoreLC := service.OverrideRunLaunchctl(func(args ...string) error {
		calls = append(calls, append([]string{}, args...))
		return nil
	})
	defer restoreLC()

	if _, err := service.Setup(service.Options{
		UnitName: "mcremote", Binary: src, UnitDir: plistDir, ConfigPath: cfg, Force: true,
	}); err != nil {
		t.Fatal(err)
	}

	kinds := kindsOf(calls)
	bo, bs := -1, -1
	for i, k := range kinds {
		if k == "bootout" && bo < 0 {
			bo = i
		}
		if k == "bootstrap" && bs < 0 {
			bs = i
		}
	}
	if bo < 0 {
		t.Fatalf("a loaded job must be booted out: %v", kinds)
	}
	if bs < 0 || bo > bs {
		t.Fatalf("bootout must precede bootstrap: %v", kinds)
	}
}

// D4: an unchanged plist over a running job needs no reload. Skipping the
// bootout/bootstrap cycle removes the race window rather than surviving it,
// and this is the shape `update` takes — the binary changed, the plist did not.
func TestDarwinSetupRestartsInPlaceWhenPlistUnchanged(t *testing.T) {
	restoreOS := service.OverrideInstallOS("darwin")
	defer restoreOS()

	src, plistDir, cfg := newSetupDir(t)

	// First render the plist so the second call sees identical content.
	restoreCap := service.OverrideRunLaunchctlCapture(scriptedPrint(false))
	restoreLC := service.OverrideRunLaunchctl(func(args ...string) error { return nil })
	first, err := service.Setup(service.Options{
		UnitName: "mcremote", Binary: src, UnitDir: plistDir, ConfigPath: cfg, Force: true,
	})
	restoreLC()
	restoreCap()
	if err != nil {
		t.Fatal(err)
	}
	if first.Unchanged {
		t.Fatal("first Setup should have written the plist")
	}

	// Now: same plist, job loaded and running.
	restoreCap = service.OverrideRunLaunchctlCapture(scriptedPrint(true))
	defer restoreCap()
	var calls [][]string
	restoreLC = service.OverrideRunLaunchctl(func(args ...string) error {
		calls = append(calls, append([]string{}, args...))
		return nil
	})
	defer restoreLC()

	res, err := service.Setup(service.Options{
		UnitName: "mcremote", Binary: src, UnitDir: plistDir, ConfigPath: cfg, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Unchanged {
		t.Fatalf("second Setup should see an unchanged plist: %+v", res)
	}
	if !res.Started {
		t.Fatal("an in-place restart still leaves the service started")
	}

	kinds := kindsOf(calls)
	for _, k := range kinds {
		if k == "bootout" || k == "bootstrap" {
			t.Fatalf("unchanged plist must not reload the job: %v", kinds)
		}
	}
	if len(kinds) == 0 || kinds[len(kinds)-1] != "kickstart" {
		t.Fatalf("expected an in-place kickstart: %v", kinds)
	}
}

// A teardown that never completes must be reported as such, not walked into a
// bootstrap that cannot succeed.
func TestDarwinSetupReportsAStuckTeardown(t *testing.T) {
	restoreOS := service.OverrideInstallOS("darwin")
	defer restoreOS()

	src, plistDir, cfg := newSetupDir(t)
	if err := os.MkdirAll(plistDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plistDir, "com.magiccliremote.mcremote.plist"),
		[]byte("<plist>stale</plist>"), 0o600); err != nil {
		t.Fatal(err)
	}

	restoreCap := service.OverrideRunLaunchctlCapture(scriptedPrint(true)) // never leaves
	defer restoreCap()
	// Shrink the wait: this asserts an error message, not fifteen seconds of
	// patience. Production timings are untouched.
	restoreT := service.OverrideLaunchdWaitTimings(20*time.Millisecond, time.Millisecond)
	defer restoreT()
	var calls [][]string
	restoreLC := service.OverrideRunLaunchctl(func(args ...string) error {
		calls = append(calls, append([]string{}, args...))
		return nil
	})
	defer restoreLC()

	_, err := service.Setup(service.Options{
		UnitName: "mcremote", Binary: src, UnitDir: plistDir, ConfigPath: cfg, Force: true,
	})
	if err == nil {
		t.Fatal("want an error when the job never leaves the domain")
	}
	// Any error would satisfy "err != nil" — including a fixture mistake that
	// never reached the launchd path at all. Pin the reason.
	if !strings.Contains(err.Error(), "could not be torn down") {
		t.Fatalf("error must name the teardown, got: %v", err)
	}
	for _, k := range kindsOf(calls) {
		if k == "bootstrap" {
			t.Fatalf("must not bootstrap over a job that is still loaded: %v", kindsOf(calls))
		}
	}
}

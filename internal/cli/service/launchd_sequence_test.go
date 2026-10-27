// Deliberately NOT build-tagged. The launchd path runs launchctl through the
// runLaunchctl seam and stops and starts through the backend setup builds, and
// touches no Darwin API, so it compiles and runs everywhere — the same
// reasoning schtasks.go gives for exercising the Windows branch from a Unix
// host (MADR 0116 D12).
//
// The teardown race MADR 0125 pinned here is the library's to wait out now:
// setup stops with launchd.Job.Stop and starts with Job.Start, as the update
// does (MADR 0207 D43). These tests pin what setup asks of the Job, and in
// what order against its own launchctl calls.

package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

// jobLog records, in one order, what setup asks of the backend ("job stop",
// "job start") and the launchctl commands it runs itself.
type jobLog struct {
	calls   []string
	stopErr error
}

// jobFake is the launchd backend setup builds, answering into log.
type jobFake struct{ log *jobLog }

func (f jobFake) Installed(context.Context, string) (bool, error) { return true, nil }
func (f jobFake) Enabled(context.Context, string) (bool, error)   { return true, nil }
func (f jobFake) Running(context.Context, string) (bool, error)   { return true, nil }
func (f jobFake) Stop(context.Context, string) error {
	f.log.calls = append(f.log.calls, "job stop")
	return f.log.stopErr
}
func (f jobFake) Start(context.Context, string) error {
	f.log.calls = append(f.log.calls, "job start")
	return nil
}
func (f jobFake) WaitHealthy(context.Context, string) error { return nil }

// withJob makes setup build a jobFake, records setup's launchctl commands in
// the same log, and answers launchdLoaded with loaded.
func withJob(t *testing.T, loaded bool) *jobLog {
	t.Helper()
	log := &jobLog{}
	t.Cleanup(service.OverrideSetupBackend(func(kind service.Kind, _, _ string, _ service.HealthOptions) (service.Backend, error) {
		if kind != service.KindLaunchdAgent {
			t.Errorf("setup built a %s backend on macOS", kind)
		}
		return jobFake{log: log}, nil
	}))
	t.Cleanup(service.OverrideRunLaunchctl(func(args ...string) error {
		log.calls = append(log.calls, "launchctl "+strings.Join(args, " "))
		return nil
	}))
	t.Cleanup(service.OverrideLaunchdLoaded(func(context.Context, string) (bool, error) { return loaded, nil }))
	return log
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

// TestDarwinSetupStopsThenStartsThroughTheJob: a changed plist is applied by
// the Job's Stop, which boots the job out and waits until it has gone (MADR
// 0125 D1, in the library), then its Start. Setup runs no bootout, bootstrap
// or kickstart of its own.
func TestDarwinSetupStopsThenStartsThroughTheJob(t *testing.T) {
	defer service.OverrideInstallOS("darwin")()
	src, plistDir, cfg := newSetupDir(t)
	if err := os.MkdirAll(plistDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Content differs from what Setup renders, so this is a genuine reload.
	if err := os.WriteFile(filepath.Join(plistDir, "com.magiccliremote.mcremote.plist"),
		[]byte("<plist>stale</plist>"), 0o600); err != nil {
		t.Fatal(err)
	}
	log := withJob(t, true)
	res, err := service.Setup(service.Options{UnitName: "mcremote", Binary: src, UnitDir: plistDir, ConfigPath: cfg, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"job stop", "job start"}; !slices.Equal(log.calls, want) || !res.Started || !res.Enabled {
		t.Fatalf("calls %q, res %+v; want %q, started and enabled", log.calls, res, want)
	}
}

// TestDarwinSetupRestartsAnUnchangedPlistThroughTheJob is MADR 0207 D43: an
// unchanged plist over a running job is no longer restarted in place with
// kickstart -k (0125 D4). No library call does that, so it goes the same way
// as any other start.
func TestDarwinSetupRestartsAnUnchangedPlistThroughTheJob(t *testing.T) {
	defer service.OverrideInstallOS("darwin")()
	src, plistDir, cfg := newSetupDir(t)
	opts := service.Options{UnitName: "mcremote", Binary: src, UnitDir: plistDir, ConfigPath: cfg, Force: true}
	withJob(t, false)
	if first, err := service.Setup(opts); err != nil || first.Unchanged {
		t.Fatalf("first Setup: %+v, %v; want the plist written", first, err)
	}

	log := withJob(t, true)
	res, err := service.Setup(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Unchanged || !res.Started {
		t.Fatalf("second Setup %+v; want an unchanged plist, started", res)
	}
	if want := []string{"job stop", "job start"}; !slices.Equal(log.calls, want) {
		t.Fatalf("calls %q, want %q and no kickstart -k", log.calls, want)
	}
}

// TestDarwinSetupReportsAFailedStop: a job the Job's Stop could not stop is
// reported as that, and is not started over: bootstrapping into a job still
// there cannot succeed (MADR 0125 D2).
func TestDarwinSetupReportsAFailedStop(t *testing.T) {
	defer service.OverrideInstallOS("darwin")()
	src, plistDir, cfg := newSetupDir(t)
	log := withJob(t, true)
	log.stopErr = errors.New("selfupdate: launchd: bootout gui/501/com.magiccliremote.mcremote: " + libservice.ErrTimeout.Error())
	_, err := service.Setup(service.Options{UnitName: "mcremote", Binary: src, UnitDir: plistDir, ConfigPath: cfg, Force: true})
	if err == nil || !strings.Contains(err.Error(), "could not be stopped") {
		t.Fatalf("err = %v, want the stop named", err)
	}
	if strings.Contains(err.Error(), "log in via the GUI") {
		t.Errorf("a failed stop got the bootstrap hint: %v", err)
	}
	if !slices.Equal(log.calls, []string{"job stop"}) {
		t.Fatalf("calls %q, want the stop alone", log.calls)
	}
}

// TestDarwinRemoveStopsBeforeDisable: Remove stops the job through the same
// Job.Stop, then disables the label, then deletes the plist.
func TestDarwinRemoveStopsBeforeDisable(t *testing.T) {
	defer service.OverrideInstallOS("darwin")()
	src, plistDir, cfg := newSetupDir(t)
	withJob(t, false)
	if _, err := service.Setup(service.Options{UnitName: "mcremote", Binary: src, UnitDir: plistDir, ConfigPath: cfg, Force: true}); err != nil {
		t.Fatal(err)
	}
	log := withJob(t, true)
	res, err := service.Remove(service.Options{UnitDir: plistDir})
	if err != nil || !res.Removed {
		t.Fatalf("Remove: %+v, %v", res, err)
	}
	stop, disable := slices.Index(log.calls, "job stop"), slices.IndexFunc(log.calls, func(c string) bool {
		return strings.HasPrefix(c, "launchctl disable ")
	})
	if stop < 0 || disable < 0 || stop > disable {
		t.Fatalf("calls %q; want the job stopped, then disabled", log.calls)
	}
}

// TestDarwinRemoveFromInsideTheJobRemovesNothing: a stop refused because this
// process runs inside the job leaves the plist, since the job would keep
// running from a definition that is gone (MADR 0207 D4).
func TestDarwinRemoveFromInsideTheJobRemovesNothing(t *testing.T) {
	defer service.OverrideInstallOS("darwin")()
	src, plistDir, cfg := newSetupDir(t)
	withJob(t, false)
	if _, err := service.Setup(service.Options{UnitName: "mcremote", Binary: src, UnitDir: plistDir, ConfigPath: cfg, Force: true}); err != nil {
		t.Fatal(err)
	}
	log := withJob(t, true)
	log.stopErr = libservice.ErrInsideService
	if _, err := service.Remove(service.Options{UnitDir: plistDir}); !errors.Is(err, libservice.ErrInsideService) {
		t.Fatalf("Remove = %v, want the inside refusal", err)
	}
	if _, err := os.Stat(filepath.Join(plistDir, "com.magiccliremote.mcremote.plist")); err != nil {
		t.Fatalf("the plist was removed after a refused stop: %v", err)
	}
}

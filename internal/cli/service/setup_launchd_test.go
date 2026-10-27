//go:build unix

// These tests assert systemd unit text, launchd plist XML and the
// ~/.local/bin discovery that goes with them. Windows runs a Task
// Scheduler task instead (MADR 0116 D12), covered by schtasks_test.go.

package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
)

func TestDarwinSetupAgentOrder(t *testing.T) {
	restore := service.OverrideInstallOS("darwin")
	defer restore()

	var calls [][]string
	restoreLC := service.OverrideRunLaunchctl(func(args ...string) error {
		cp := append([]string{}, args...)
		calls = append(calls, cp)
		return nil
	})
	defer restoreLC()

	// Setup stops and starts through the backend TestMain replaces, so no
	// test reaches the developer's own launchd (MADR 0207 D43). The probes
	// below answer for what setup still reads itself.
	restoreCap := service.OverrideRunLaunchctlCapture(
		func(...string) (string, error) {
			return "", errors.New("launchctl: no such service")
		})
	defer restoreCap()
	defer service.OverrideLaunchdLoaded(func(context.Context, string) (bool, error) { return false, nil })()

	dir := t.TempDir()
	src := filepath.Join(dir, "mcremote")
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plistDir := filepath.Join(dir, "LaunchAgents")
	cfgPath := filepath.Join(dir, "config.yaml")

	res, err := service.Setup(service.Options{
		UnitName:   "mcremote",
		Binary:     src,
		UnitDir:    plistDir,
		ConfigPath: cfgPath,
		Force:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Scope != "launchd-agent" {
		t.Fatalf("Scope=%q", res.Scope)
	}
	if res.Label != "com.magiccliremote.mcremote" {
		t.Fatalf("Label=%q", res.Label)
	}
	if res.UnitPath == "" || !strings.HasSuffix(res.UnitPath, "com.magiccliremote.mcremote.plist") {
		t.Fatalf("UnitPath=%q", res.UnitPath)
	}
	if !res.Enabled || !res.Started {
		t.Fatalf("Enabled=%v Started=%v", res.Enabled, res.Started)
	}
	if res.LingerEnabled {
		t.Fatal("LingerEnabled must be false on darwin")
	}
	if res.LogDir == "" {
		t.Fatal("LogDir empty")
	}

	// enable, bootstrap and kickstart are Job.Start's, and the teardown before
	// them Job.Stop's (MADR 0207 D43; TestDarwinSetupStopsThenStartsThroughTheJob
	// in launchd_sequence_test.go). Setup runs none of them itself, and no
	// command of its own touches the system domain.
	for _, c := range calls {
		joined := strings.Join(c, " ")
		if strings.Contains(joined, "bootstrap system") || strings.Contains(joined, "/Library/LaunchDaemons") {
			t.Fatalf("must not touch system domain: %v", c)
		}
		if len(c) > 0 && (c[0] == "bootout" || c[0] == "bootstrap" || c[0] == "kickstart") {
			t.Fatalf("setup ran launchctl %v itself; the Job does", c)
		}
	}
}

func TestDarwinSetupNeverTouchesSystemDomain(t *testing.T) {
	restore := service.OverrideInstallOS("darwin")
	defer restore()

	var calls []string
	restoreLC := service.OverrideRunLaunchctl(func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return nil
	})
	defer restoreLC()

	// Setup stops and starts through the backend TestMain replaces, so no
	// test reaches the developer's own launchd (MADR 0207 D43). The probes
	// below answer for what setup still reads itself.
	restoreCap := service.OverrideRunLaunchctlCapture(
		func(...string) (string, error) {
			return "", errors.New("launchctl: no such service")
		})
	defer restoreCap()
	defer service.OverrideLaunchdLoaded(func(context.Context, string) (bool, error) { return false, nil })()

	dir := t.TempDir()
	src := filepath.Join(dir, "mcremote")
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := service.Setup(service.Options{
		Binary:   src,
		UnitDir:  filepath.Join(dir, "LA"),
		Force:    true,
		NoEnable: false,
		NoStart:  false,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range calls {
		if strings.Contains(c, "bootstrap system") || strings.Contains(c, "system/") && !strings.Contains(c, "gui/") {
			// allow nothing with pure system domain
			if strings.HasPrefix(c, "bootstrap system") || strings.HasPrefix(c, "enable system/") || strings.HasPrefix(c, "bootout system/") {
				t.Fatalf("system domain used: %q", c)
			}
		}
		if strings.Contains(c, "/Library/LaunchDaemons") {
			t.Fatalf("LaunchDaemons path: %q", c)
		}
	}
}

func TestDarwinRemoveAgent(t *testing.T) {
	restore := service.OverrideInstallOS("darwin")
	defer restore()

	var calls [][]string
	restoreLC := service.OverrideRunLaunchctl(func(args ...string) error {
		calls = append(calls, append([]string{}, args...))
		return nil
	})
	defer restoreLC()

	// Setup stops and starts through the backend TestMain replaces, so no
	// test reaches the developer's own launchd (MADR 0207 D43). The probes
	// below answer for what setup still reads itself.
	restoreCap := service.OverrideRunLaunchctlCapture(
		func(...string) (string, error) {
			return "", errors.New("launchctl: no such service")
		})
	defer restoreCap()
	defer service.OverrideLaunchdLoaded(func(context.Context, string) (bool, error) { return false, nil })()

	dir := t.TempDir()
	src := filepath.Join(dir, "mcremote")
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plistDir := filepath.Join(dir, "LaunchAgents")
	if _, err := service.Setup(service.Options{
		Binary:  src,
		UnitDir: plistDir,
		Force:   true,
	}); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(plistDir, "com.magiccliremote.mcremote.plist")
	if _, err := os.Stat(plist); err != nil {
		t.Fatal(err)
	}
	// The backup a refresh kept goes with the plist (MADR 0210 D9).
	backup := plist + ".prev"
	if err := os.WriteFile(backup, []byte("<plist/>\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := service.Remove(service.Options{UnitDir: plistDir})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Removed {
		t.Fatal("expected Removed")
	}
	if _, err := os.Stat(plist); !os.IsNotExist(err) {
		t.Fatal("plist still present")
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("%s survived Remove: %v", backup, err)
	}
	if res.RemovedBackup != backup {
		t.Errorf("RemovedBackup = %q, want %q", res.RemovedBackup, backup)
	}

	// The stop is Job.Stop's, which boots out only what is loaded and waits
	// for it (MADR 0125 D1, D3, in the library; MADR 0207 D43). Setup runs no
	// bootout itself. `disable` and the plist removal are what this test
	// guards; the stop's order against them is TestDarwinRemoveStopsBeforeDisable
	// in launchd_sequence_test.go, which is untagged and so runs on every host.
	var hasBootout, hasDisable bool
	for _, c := range calls {
		if len(c) == 0 {
			continue
		}
		if c[0] == "bootout" {
			hasBootout = true
		}
		if strings.Contains(strings.Join(c, " "), "system/") {
			t.Fatalf("remove used system domain: %v", c)
		}
		if c[0] == "disable" {
			hasDisable = true
		}
	}
	if hasBootout {
		t.Fatalf("setup ran a bootout itself; the Job does: %v", calls)
	}
	if !hasDisable {
		t.Fatalf("expected disable, got %v", calls)
	}
}

func TestDarwinPrintOnlyPlist(t *testing.T) {
	restore := service.OverrideInstallOS("darwin")
	defer restore()

	res, err := service.Setup(service.Options{
		Binary:    "/usr/local/bin/mcremote",
		PrintOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.UnitBody, "com.magiccliremote.mcremote") {
		t.Fatalf("print-only should render plist:\n%s", res.UnitBody)
	}
	if strings.Contains(res.UnitBody, "[Unit]") {
		t.Fatal("print-only on darwin must not render systemd unit")
	}
	if res.Scope != "launchd-agent" {
		t.Fatalf("Scope=%q", res.Scope)
	}
}

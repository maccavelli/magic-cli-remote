//go:build unix

// These tests assert systemd unit text, launchd plist XML and the
// ~/.local/bin discovery that goes with them. Windows runs a Task
// Scheduler task instead (MADR 0116 D12), covered by schtasks_test.go.

package service_test

import (
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

	// Order: bootout, enable, bootstrap, kickstart
	var kinds []string
	for _, c := range calls {
		if len(c) == 0 {
			continue
		}
		kinds = append(kinds, c[0])
		joined := strings.Join(c, " ")
		if strings.Contains(joined, "bootstrap system") || strings.Contains(joined, "/Library/LaunchDaemons") {
			t.Fatalf("must not touch system domain: %v", c)
		}
	}
	wantSeq := []string{"bootout", "enable", "bootstrap", "kickstart"}
	if len(kinds) < len(wantSeq) {
		t.Fatalf("calls=%v, want at least %v", kinds, wantSeq)
	}
	// Find first occurrence sequence
	i := 0
	for _, k := range kinds {
		if i < len(wantSeq) && k == wantSeq[i] {
			i++
		}
	}
	if i != len(wantSeq) {
		t.Fatalf("call order %v does not contain sequence %v", kinds, wantSeq)
	}

	// enable before bootstrap
	en, bs := -1, -1
	for i, k := range kinds {
		if k == "enable" && en < 0 {
			en = i
		}
		if k == "bootstrap" && bs < 0 {
			bs = i
		}
	}
	if en < 0 || bs < 0 || en > bs {
		t.Fatalf("enable must precede bootstrap: %v", kinds)
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

	var hasBootout, hasDisable bool
	for _, c := range calls {
		if len(c) == 0 {
			continue
		}
		if c[0] == "bootout" {
			hasBootout = true
			if strings.Contains(strings.Join(c, " "), "system/") {
				t.Fatalf("remove used system domain: %v", c)
			}
		}
		if c[0] == "disable" {
			hasDisable = true
		}
	}
	if !hasBootout || !hasDisable {
		t.Fatalf("expected bootout+disable, got %v", calls)
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

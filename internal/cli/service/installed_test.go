//go:build unix

// These tests assert systemd unit text, launchd plist XML and the
// ~/.local/bin discovery that goes with them. Windows runs a Task
// Scheduler task instead (MADR 0116 D12), covered by schtasks_test.go.

package service

// White-box: IsInstalled's LoadState probe is driven through an unexported
// seam (MADR 0100 Phase 4).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stubSystemctlCapture(t *testing.T, out string, err error) *[]string {
	t.Helper()
	var calls []string
	prev := runSystemctlCapture
	runSystemctlCapture = func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return out, err
	}
	t.Cleanup(func() { runSystemctlCapture = prev })
	return &calls
}

func TestIsInstalledLinuxLoadState(t *testing.T) {
	defer OverrideInstallOS("linux")()

	t.Run("loaded", func(t *testing.T) {
		calls := stubSystemctlCapture(t, "loaded\n", nil)
		got, err := IsInstalled("mcremote")
		if err != nil || !got {
			t.Fatalf("got %v, %v; want true", got, err)
		}
		if len(*calls) != 1 || !strings.Contains((*calls)[0], "show -p LoadState --value mcremote.service") {
			t.Fatalf("probe = %v; is-enabled would report a disabled unit as absent", *calls)
		}
	})

	t.Run("masked is not startable", func(t *testing.T) {
		stubSystemctlCapture(t, "masked\n", nil)
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		got, _ := IsInstalled("mcremote")
		if got {
			t.Fatal("a masked unit must not count as installed")
		}
	})

	t.Run("not-found falls back to the unit file", func(t *testing.T) {
		stubSystemctlCapture(t, "not-found\n", nil)
		cfg := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", cfg)
		if got, _ := IsInstalled("mcremote"); got {
			t.Fatal("no unit file and not-found must be false")
		}
		unitDir := filepath.Join(cfg, "systemd", "user")
		if err := os.MkdirAll(unitDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(unitDir, "mcremote.service"), []byte("[Unit]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// A definition written but never reloaded is still installed: the
		// refresh reloads before the start.
		if got, _ := IsInstalled("mcremote"); !got {
			t.Fatal("a unit file on disk must count as installed")
		}
	})
}

func TestIsInstalledDarwinPlistPresent(t *testing.T) {
	defer OverrideInstallOS("darwin")()
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got, _ := IsInstalled("mcrelay"); got {
		t.Fatal("no plist must be false")
	}
	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "com.magiccliremote.mcrelay.plist"), []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := IsInstalled("mcrelay"); !got {
		t.Fatal("plist present must be true")
	}
}

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withApparmor points the AppArmor check at a scratch file holding value, or
// at a missing file when value is empty.
func withApparmor(t *testing.T, value string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "apparmor_restrict_unprivileged_userns")
	if value != "" {
		if err := os.WriteFile(path, []byte(value+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(OverrideApparmorRestrictPath(path))
}

// TestInstallerAdvisoriesAppArmor is install.sh:895-902: the notice on Linux
// when unprivileged user namespaces are restricted, and nothing otherwise
// (MADR 0054 amendment S7d, A1).
func TestInstallerAdvisoriesAppArmor(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{{"1", true}, {"0", false}, {"", false}} {
		withApparmor(t, tc.value)
		got := strings.Join(installerAdvisories("linux", "mcremote", "/opt/bin"), "\n")
		if has := strings.Contains(got, "AppArmor restricts unprivileged user namespaces"); has != tc.want {
			t.Errorf("file %q: notice shown %v, want %v:\n%s", tc.value, has, tc.want, got)
		}
		if tc.want && !strings.Contains(got, "MADR 0048") {
			t.Errorf("the notice does not name the remedy:\n%s", got)
		}
	}
}

// TestInstallerAdvisoriesFullDiskAccess is install.sh:904-913: on macOS the
// note names the binary in the hook's own folder and the LaunchAgent (A2).
func TestInstallerAdvisoriesFullDiskAccess(t *testing.T) {
	withUnitRecord(t, "mcremote", "")
	got := strings.Join(installerAdvisories("darwin", "mcremote", "/Users/user/.local/bin"), "\n")
	for _, want := range []string{
		"Full Disk Access is not granted by this installer.",
		"add /Users/user/.local/bin/mcremote",
		"launchctl kickstart -k gui/$(id -u)/com.magiccliremote.mcremote",
		"ops-macos-tcc.md",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the note lacks %q:\n%s", want, got)
		}
	}
}

// TestInstallerAdvisoriesOnlyForMcremote: mcrelay's hook prints nothing, so an
// install of both products shows each notice once; Windows has none.
func TestInstallerAdvisoriesOnlyForMcremote(t *testing.T) {
	withApparmor(t, "1")
	for _, osName := range []string{"linux", "darwin", "windows"} {
		if got := installerAdvisories(osName, "mcrelay", "/opt/bin"); got != nil {
			t.Errorf("%s: mcrelay got %q", osName, got)
		}
	}
	if got := installerAdvisories("windows", "mcremote", `C:\bin`); got != nil {
		t.Errorf("windows: mcremote got %q", got)
	}
}

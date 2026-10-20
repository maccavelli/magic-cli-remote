package service

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

// The two notices the shipped install.sh printed on every install, whatever
// the service result (scripts/install.sh:895-913), and the library's
// installer does not. setup-service --from-installer prints them for mcremote
// only: the installer runs both products' hooks, and both notices are about
// mcremote (MADR 0054 amendment S7d, A1 and A2; PLAN S7d0). The quarantine
// clearing is not ported (A3): a quarantined binary fails the installer's
// identity check before any hook runs.

// apparmorRestrictPath is the sysctl Ubuntu 24.04 and later set to 1. It is a
// variable for tests.
var apparmorRestrictPath = "/proc/sys/kernel/apparmor_restrict_unprivileged_userns"

// OverrideApparmorRestrictPath points the AppArmor check at path, for tests.
// Restore with the returned func.
func OverrideApparmorRestrictPath(path string) (restore func()) {
	prev := apparmorRestrictPath
	apparmorRestrictPath = path
	return func() { apparmorRestrictPath = prev }
}

// installerAdvisories returns the notice lines for product's install in dir
// on osName, each group led by a blank line.
func installerAdvisories(osName, product, dir string) []string {
	if product != "mcremote" {
		return nil
	}
	var lines []string
	switch osName {
	case "linux":
		if b, err := os.ReadFile(apparmorRestrictPath); err == nil && strings.TrimSpace(string(b)) == "1" {
			// The shipped remedy was scripts/bwrap-apparmor-fix.sh, which an
			// install through curl does not have.
			lines = append(lines, "",
				"note: AppArmor restricts unprivileged user namespaces on this host.",
				"mcremote itself is unaffected, but agent CLI sandboxing (codex /",
				"bubblewrap) will fail. The remedy needs sudo: see MADR 0048 and the",
				"AppArmor note in docs/guides/ops-linux-install.md.")
		}
	case "darwin":
		label := "com.magiccliremote." + product
		if name, err := ResolveUnitName(product); err == nil {
			if l, err := LaunchdLabel(product, name); err == nil {
				label = l
			}
		}
		lines = append(lines, "",
			"Full Disk Access is not granted by this installer.",
			"System Settings → Privacy & Security → Full Disk Access",
			"    → add "+dir+"/"+product+" → restart the LaunchAgent:",
			"    launchctl kickstart -k gui/$(id -u)/"+label,
			"Unsigned upgrades drop that grant. To keep it, rebuild signed:",
			"    make install MC_CODESIGN_IDENTITY='Apple Development: you (TEAMID)'",
			"See docs/guides/ops-macos-tcc.md.")
	}
	return lines
}

// PrintInstallerAdvisories writes product's install notices: those for the
// binary running the command, in its own folder, on this OS.
func PrintInstallerAdvisories(out io.Writer, product string) {
	osName := installOS
	if osName == "" {
		osName = runtime.GOOS
	}
	dir, err := uninstallExeDir()
	if err != nil {
		return
	}
	for _, l := range installerAdvisories(osName, product, dir) {
		fmt.Fprintln(out, l)
	}
}

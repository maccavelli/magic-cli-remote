package ws

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// permissionGuidance appends actionable copy to a permission_denied message
// when the context identifies the cause (MADR 0069 D4.5). Composed here —
// the daemon layer, which knows the platform — so providers stay GOOS-free.
// Suggestive, never assertive: there is no API to confirm a TCC denial.
func permissionGuidance(err error, msg string) string {
	return permissionGuidanceFor(runtime.GOOS, err, msg)
}

func permissionGuidanceFor(goos string, err error, msg string) string {
	var cwdErr *provider.CWDPermissionError
	if goos == "darwin" && errors.As(err, &cwdErr) && underMacProtectedRoot(cwdErr.Path) {
		return msg + " — macOS privacy protection (TCC) is likely blocking " +
			"the daemon. Grant mcremote Full Disk Access in System Settings → " +
			"Privacy & Security, or choose a path outside " +
			"Documents/Desktop/Downloads. See docs/ops-macos-tcc.md."
	}
	return msg
}

// underMacProtectedRoot reports whether path sits inside one of the macOS
// TCC-protected user folders (Documents, Desktop, Downloads, iCloud Drive).
func underMacProtectedRoot(path string) bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	for _, root := range []string{
		"Documents", "Desktop", "Downloads",
		filepath.Join("Library", "Mobile Documents"), // iCloud Drive
	} {
		p := filepath.Join(home, root)
		if path == p || strings.HasPrefix(path, p+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

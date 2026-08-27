//go:build windows

package appdirs

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// maxWindowsPathBudget is the byte length past which a resolved layout is
// reported as risky. Go's fixLongPath (os/path_windows.go) applies the \\?\
// prefix below 248 bytes, so the daemon's own os calls keep working; the
// number below is that same threshold, used as a warning line rather than a
// hard limit.
const maxWindowsPathBudget = 248

// knownFolder is a test seam: production reads the real Known Folder, and
// roots_windows_test.go substitutes a temp dir.
var knownFolder = func(id *windows.KNOWNFOLDERID) (string, error) {
	return windows.KnownFolderPath(id, windows.KF_FLAG_DEFAULT)
}

// systemRoots resolves the Windows layout from Known Folders (MADR 0116 D3).
//
// Roaming vs local is deliberate: config is the only thing a user wants to
// follow them onto another machine, so it lives under RoamingAppData
// (%AppData%). Data, state, cache, logs and the runtime dir are
// machine-specific and must NOT roam — a session store or an admin socket
// replicated to a second machine is at best useless and at worst confusing,
// so they live under LocalAppData. This mirrors what os.UserConfigDir and
// os.UserCacheDir already choose.
//
// No os.Getuid() appears here: it returns -1 on Windows, and the runtime root
// is per-user by construction because FOLDERID_LocalAppData already is.
func systemRoots(product Product) (Roots, []Diagnostic, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Roots{}, nil, fmt.Errorf("home dir: %w", err)
	}
	if !filepath.IsAbs(home) {
		return Roots{}, nil, fmt.Errorf("home dir is not absolute: %q", home)
	}
	roaming, err := knownFolder(windows.FOLDERID_RoamingAppData)
	if err != nil {
		return Roots{}, nil, fmt.Errorf("known folder RoamingAppData: %w", err)
	}
	local, err := knownFolder(windows.FOLDERID_LocalAppData)
	if err != nil {
		return Roots{}, nil, fmt.Errorf("known folder LocalAppData: %w", err)
	}

	base := filepath.Join(local, product.Name)
	r := Roots{
		Home:        filepath.Clean(home),
		ConfigHome:  filepath.Clean(roaming),
		DataHome:    filepath.Clean(local),
		StateHome:   filepath.Join(base, "State"),
		CacheHome:   filepath.Join(base, "Cache"),
		RuntimeHome: filepath.Join(base, "Runtime"),
		Temp:        filepath.Clean(os.TempDir()),
		Logs:        filepath.Join(base, "Logs"),
		// StateHome/CacheHome/RuntimeHome above are already under the product
		// leaf, so Resolve must not append it again (MADR 0116 D3 amendment).
		ProductScoped: true,
	}
	return r, pathLengthDiagnostics(r, product), nil
}

// pathLengthDiagnostics reports a layout deep enough to risk MAX_PATH.
//
// Go's fixLongPath (os/path_windows.go:100-105) transparently applies the
// \\?\ prefix below 248 bytes, so the daemon's own os calls keep working past
// MAX_PATH. Provider CLIs get no such help, which is the real exposure — so
// warn and proceed rather than refuse (MADR 0116 D3). Contrast
// CheckSocketPathLength, which DOES refuse: an over-long sun_path cannot work
// at all, whereas an over-long directory usually can.
//
// The budget is measured against the deepest leaf Resolve will build — the
// instance-keyed runtime dir plus admin.sock — not against the root, or the
// warning would fire too late to be useful.
func pathLengthDiagnostics(r Roots, product Product) []Diagnostic {
	// InstanceKey is always 16 hex chars; SocketName is the longest leaf.
	deepest := filepath.Join(r.RuntimeHome, product.Name, "0123456789abcdef", "admin.sock")
	if len(deepest) <= maxWindowsPathBudget {
		return nil
	}
	return []Diagnostic{{
		Code: "windows_path_length",
		Message: fmt.Sprintf(
			"resolved layout is deep (%d bytes for %s, budget %d); Go handles this "+
				"but an agent CLI launched by the daemon may not — consider a shorter --data-dir",
			len(deepest), deepest, maxWindowsPathBudget),
	}}
}

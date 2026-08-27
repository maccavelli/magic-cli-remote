// Package testexec writes executable fixtures for tests and gates the ones
// that cannot exist on a given platform.
//
// Many suites stand a provider CLI up as a small `#!/bin/sh` script. That is a
// Unix-only construct: Windows resolves executables through PATHEXT and cannot
// run a shebang file at all, so exec'ing one fails with "%1 is not a valid
// Win32 application" or "executable file not found in %PATH%".
//
// The behaviour those suites test — credential flows, process lifecycle,
// journal ordering — is platform-independent Go. It is the *fixture* that is
// unportable, so the fixture is where the skip belongs: one gate, one stated
// reason, and every dependent test inherits it (MADR 0116 D16, Confirmation
// item 3).
//
// This package is imported only from _test.go files and is never linked into a
// shipped binary.
package testexec

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// SkipIfNoPOSIXShell skips t when a `#!/bin/sh` fixture cannot be executed.
//
// Call it from the helper that writes the fixture rather than from each test,
// so the reason is stated once.
func SkipIfNoPOSIXShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture is a POSIX shell script, which Windows cannot execute " +
			"(PATHEXT has no shebang equivalent); the flow under test is " +
			"platform-independent Go — see MADR 0116 P11")
	}
}

// WriteShellStub writes body as an executable POSIX shell script at path and
// returns it. It skips the test on platforms that cannot run one.
func WriteShellStub(t *testing.T, path, body string) string {
	t.Helper()
	SkipIfNoPOSIXShell(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// SkipIfNoPOSIXModes skips t where filesystem permission bits carry no access
// control.
//
// On Windows a file reports 0666 whatever its ACL says, so an assertion on
// Perm() tests nothing. Where the *property* matters rather than the bits,
// prefer appdirs.FileIsOwnerOnly, which is answered per platform (MADR 0116
// D22); this gate is for assertions that are genuinely about POSIX modes.
func SkipIfNoPOSIXModes(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("permission bits carry no access control on Windows (files " +
			"report 0666 regardless of ACL); the equivalent property is " +
			"appdirs.FileIsOwnerOnly — see MADR 0116 D22")
	}
}

// SkipIfNoUnixServiceManager skips t on platforms with neither systemd nor
// launchd. Tests that drive a specific manager should call OverrideInstallOS
// instead; this is for tests that need a real one present.
func SkipIfNoUnixServiceManager(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("systemd/launchd fixture; Windows uses a Task Scheduler task " +
			"(MADR 0116 D12) covered by schtasks_test.go")
	}
}

// SkipIfNoPOSIXPaths skips t where the fixture uses POSIX absolute paths.
//
// "/work/project" is not an absolute path on Windows — filepath.IsAbs wants a
// volume — so config validation that requires absolute paths rejects the
// fixture before the test's real assertion is reached. The logic under test is
// platform-independent; only the literals are not.
func SkipIfNoPOSIXPaths(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX absolute paths, which are not absolute on " +
			"Windows — see MADR 0116 P11")
	}
}

// SkipIfNoXDG skips t where the fixture injects XDG_* to place config.
//
// Windows resolves paths from Known Folders and ignores XDG_CONFIG_HOME
// (MADR 0116 D3), so a hermetic fixture built that way is invisible there.
func SkipIfNoXDG(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture places config via XDG_CONFIG_HOME; Windows resolves " +
			"from Known Folders (MADR 0116 D3)")
	}
}

// SkipIfNoUnlinkOpenFile skips t where an open file cannot be unlinked.
//
// POSIX removes a directory entry regardless of open handles; Windows refuses
// while any handle is open. Tests that assert cleanup after an injected
// close failure depend on the POSIX behaviour.
func SkipIfNoUnlinkOpenFile(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot unlink a file while a handle is open, so a temp " +
			"left by an injected close failure survives — see MADR 0116 P11")
	}
}

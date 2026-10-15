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
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

// errPrivilegeNotHeld is Windows ERROR_PRIVILEGE_NOT_HELD. Declared as a plain
// Errno so this file needs no build tag: syscall.Errno exists on every
// supported platform and this value simply never occurs on Unix.
const errPrivilegeNotHeld = syscall.Errno(1314)

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

// WriteShellStub writes body as an executable POSIX shell script at path, runs
// it once without reaching body (see writeAndWarm), and returns it. It skips
// the test on platforms that cannot run one.
func WriteShellStub(t *testing.T, path, body string) string {
	t.Helper()
	SkipIfNoPOSIXShell(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeAndWarm(t, path, "#!/bin/sh\n"+warmGuard+body+"\n")
	return path
}

// warmEnv names the file a stub's warm-up run creates. Only writeAndWarm sets
// it, and only on the warm-up command; nothing in Magic reads it.
const warmEnv = "MCREMOTE_STUB_WARM"

// warmGuard is a stub's first line after the shebang: during the warm-up it
// creates the named file and exits, so the body never runs.
const warmGuard = `[ -n "$` + warmEnv + `" ] && { : > "$` + warmEnv + `"; exit 0; }` + "\n"

// writeAndWarm writes script and runs it once, before any test can time it.
//
// macOS holds the first run of a newly written executable while Gatekeeper
// scans it: about 150 ms on an idle host, and seconds when other new files
// (go test's own binaries among them) are queued. The hold falls after
// exec.Cmd.Start returns, so every bound around the run counts it, and a test
// that waited 3 s for a stub's effect failed, while one that watched a 250 ms
// window saw nothing and passed when it should not have (MADR 0193 E1–E8). A
// second run takes about 16 ms.
//
// The run must reach the guard, and the guard must create its file; anything
// else fails the test, so a warm-up that did not happen cannot pass silently.
func writeAndWarm(tb testing.TB, path, script string) {
	tb.Helper()
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		tb.Fatalf("write %s: %v", path, err)
		return
	}
	marker := filepath.Join(tb.TempDir(), "warmed")
	cmd := exec.Command(path)
	cmd.Env = append(os.Environ(), warmEnv+"="+marker)
	if out, err := cmd.CombinedOutput(); err != nil {
		tb.Fatalf("warm %s: %v\n%s", path, err, out)
		return
	}
	if _, err := os.Stat(marker); err != nil {
		tb.Fatalf("warm %s: the run did not create %s, so it did not reach the warm-up guard", path, marker)
	}
}

// WarmExecutable runs a freshly built binary once with args, which the caller
// names as harmless, so its first-run scan is not inside the bound of the run
// the test times (MADR 0193 D2). It fails the test if the run fails.
func WarmExecutable(tb testing.TB, path string, args ...string) {
	tb.Helper()
	if out, err := exec.Command(path, args...).CombinedOutput(); err != nil {
		tb.Fatalf("warm %s %v: %v\n%s", path, args, err, out)
	}
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

// SkipIfNoSymlink skips t where the process cannot create a symbolic link.
//
// Unlike the gates above, the predicate is the machine rather than the OS
// (MADR 0118 F3): creating a symlink on Windows needs
// SeCreateSymbolicLinkPrivilege, which an elevated shell or Developer Mode
// grants and an ordinary shell does not. The same windows/amd64 binary
// therefore succeeds on one machine and fails on another, so a runtime.GOOS
// check would discard the coverage on every Windows machine that can actually
// run the test — CI included.
//
// Any probe failure other than the missing privilege is fatal: a blanket skip
// would convert a broken filesystem into silent non-coverage (MADR 0118 D2).
//
// Set MC_REQUIRE_SYMLINK to turn the skip into a failure, as CI does, so a
// runner that loses the privilege breaks the build instead of quietly ceasing
// to verify the invariants under test (MADR 0118 D4).
func SkipIfNoSymlink(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "symlink-probe-target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := os.Symlink(target, filepath.Join(dir, "symlink-probe-link"))
	if err == nil {
		return
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == errPrivilegeNotHeld {
		if os.Getenv("MC_REQUIRE_SYMLINK") != "" {
			t.Fatalf("MC_REQUIRE_SYMLINK is set but the symlink privilege is "+
				"not held: %v", err)
		}
		t.Skip("symlink creation needs SeCreateSymbolicLinkPrivilege " +
			"(Developer Mode or an elevated shell); MADR 0118")
	}
	t.Fatalf("symlink probe failed for an unexpected reason: %v", err)
}

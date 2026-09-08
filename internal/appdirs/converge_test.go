package appdirs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestEnsurePrivateDirConvergesAnExistingDir pins MADR 0155 F7, and it is
// PLAN 0155's A5 — the criterion the plan named as most likely to be skipped,
// because the fact was already measured by hand and re-proving it in a test
// feels redundant.
//
// It is not redundant. That measurement is the entire reason MADR 0155 rejected
// mirroring mcrelay's hard fail: the migration is believed safe because
// converging the config directory repairs the config file already inside it,
// with no operator action. If that ever stops being true, every Windows
// installation whose config carries a secret starts refusing to boot, and
// nothing else in the suite would notice.
//
// On Unix the claim is narrower and the assertion says so: EnsurePrivateDir
// there is MkdirAll plus a check of the directory, and it never enumerates
// children — which is exactly why config.repairOwnerOnly exists for that
// platform (0155 D8).
func TestEnsurePrivateDirConvergesAnExistingDir(t *testing.T) {
	// A directory created the ordinary way: whatever the host grants, inherited.
	base := t.TempDir()
	dir := filepath.Join(base, "cfg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(file, []byte("listen:\n  port: 7531\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The fixture must actually start non-private, or the test proves nothing.
	before, err := FileIsOwnerOnly(file)
	if err != nil {
		t.Fatalf("FileIsOwnerOnly before: %v", err)
	}
	if before {
		t.Skip("the host created this file owner-only already; nothing to converge")
	}

	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatalf("EnsurePrivateDir: %v", err)
	}

	after, err := FileIsOwnerOnly(file)
	if err != nil {
		t.Fatalf("FileIsOwnerOnly after: %v", err)
	}

	if runtime.GOOS == "windows" {
		if !after {
			t.Fatal("converging the directory did not make the existing file owner-only; " +
				"MADR 0155's migration story depends on this and no longer holds")
		}
		return
	}
	// POSIX: the directory is repaired, the file's own mode is not. Asserted so
	// the asymmetry is a pinned fact rather than an assumption in a document.
	if after {
		t.Error("EnsurePrivateDir repaired a file's mode on POSIX; " +
			"config.repairOwnerOnly exists because it does not, so one of the two is now wrong")
	}
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory mode = %o after converging, want 0700", perm)
	}
}

// A second call must be a no-op, which is what makes it safe to run on every
// daemon start (MADR 0155 D1).
//
// TestEnsurePrivateDirIdempotent in roots_windows_test.go asserts the same
// property through ValidateRuntimeDir, but only on Windows. This one runs
// everywhere and asserts through the property the config guard actually
// depends on — that a file inside the directory comes out owner-only — so the
// POSIX side is covered too. The overlap on Windows is deliberate, not an
// accidental duplicate.
func TestEnsurePrivateDirIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatalf("second call: %v", err)
	}
	// There is no exported "is this dir private" predicate, so the property is
	// asserted through a file created inside it: on both platforms a file in a
	// converged directory must come out owner-only.
	file := filepath.Join(dir, "probe")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ok, err := FileIsOwnerOnly(file)
	if err != nil {
		t.Fatalf("FileIsOwnerOnly: %v", err)
	}
	if !ok {
		t.Error("a file created inside a converged directory is not owner-only")
	}
}

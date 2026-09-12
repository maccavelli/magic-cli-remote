package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
)

// TestEnsureDefaultConfigPrivate pins MADR 0155 D1/F4, PLAN 0155 A4: after
// setup writes the default config, the config directory and the file are
// private on both platforms, even when the directory already existed and was
// not private.
//
// Before 0155 P3 the directory was made with MkdirAll(0700) plus a
// chmod-if-loose. Windows ignores that mode, so the result was private on POSIX
// only, while the config inside may hold a registration secret. P3 switched to
// appdirs.EnsurePrivateDir, but no test pinned it; PLAN 0155's whole-plan
// verification found this test missing when closing the plan. It lives in
// package service because ensureDefaultConfig is unexported and setup_test.go
// is Unix-only.
//
// The fixture starts from a directory that is NOT private on either platform:
// mode 0755 on Unix, and an inheritable BUILTIN\Users grant on Windows. That
// grant is added explicitly rather than taken from whatever %TEMP% inherits,
// so the test fails if convergence does nothing (MADR 0155 F5).
func TestEnsureDefaultConfigPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mcremote")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	exposeDir(t, dir)
	if ok, err := appdirs.FileIsOwnerOnly(dir); err != nil || ok {
		t.Fatalf("fixture directory is already private (ok=%v err=%v); the test would assert nothing", ok, err)
	}

	path, created, err := ensureDefaultConfig(Options{
		Product:    "mcremote",
		ConfigPath: filepath.Join(dir, "config.yaml"),
	})
	if err != nil {
		t.Fatalf("ensureDefaultConfig: %v", err)
	}
	if !created {
		t.Fatal("ensureDefaultConfig did not write a default config into an empty directory")
	}

	for _, p := range []string{dir, path} {
		ok, err := appdirs.FileIsOwnerOnly(p)
		if err != nil {
			t.Fatalf("FileIsOwnerOnly(%s): %v", p, err)
		}
		if !ok {
			t.Errorf("%s is not owner-only after setup: %s", p, appdirs.NotOwnerOnlyDetail(p))
		}
	}
}

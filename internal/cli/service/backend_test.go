package service

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/launchd"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/systemd"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/taskscheduler"
)

// TestNewBackendIsTheLibrarysForThisOS: a managed update and doctor drive
// go-selfupdate-lib's backend for this OS, each of which reports whether the
// service is enabled (MADR 0054 D2, D3).
func TestNewBackendIsTheLibrarysForThisOS(t *testing.T) {
	if runtime.GOOS == "linux" {
		// The user manager is reached through XDG_RUNTIME_DIR; construction
		// only needs it set.
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	}
	b, err := NewBackend("mcremote", "")
	var ok bool
	switch runtime.GOOS {
	case "linux":
		_, ok = b.(*systemd.Unit)
	case "darwin":
		_, ok = b.(*launchd.Job)
	case "windows":
		_, ok = b.(*taskscheduler.Task)
	default:
		if err == nil {
			t.Fatalf("NewBackend on %s = %T, want an error", runtime.GOOS, b)
		}
		return
	}
	if err != nil || !ok {
		t.Fatalf("NewBackend on %s = %T, %v; want the library's backend", runtime.GOOS, b, err)
	}
}

// TestEnsureUserRuntimeDirDerivesIt: the systemd backend needs
// XDG_RUNTIME_DIR, and Magic derives it from the user's runtime directory as
// it always has for systemctl (MADR 0054 PLAN S1, Deviation 1).
func TestEnsureUserRuntimeDirDerivesIt(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	prev := userRuntimeRoot
	userRuntimeRoot = root
	t.Cleanup(func() { userRuntimeRoot = prev })

	t.Setenv("XDG_RUNTIME_DIR", "")
	ensureUserRuntimeDir()
	if got := os.Getenv("XDG_RUNTIME_DIR"); got != "" {
		t.Fatalf("XDG_RUNTIME_DIR = %q with no runtime directory, want it left unset", got)
	}

	dir := filepath.Join(root, u.Uid)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ensureUserRuntimeDir()
	if got := os.Getenv("XDG_RUNTIME_DIR"); got != dir {
		t.Fatalf("XDG_RUNTIME_DIR = %q, want the derived %q", got, dir)
	}

	t.Setenv("XDG_RUNTIME_DIR", "/run/given")
	ensureUserRuntimeDir()
	if got := os.Getenv("XDG_RUNTIME_DIR"); got != "/run/given" {
		t.Fatalf("XDG_RUNTIME_DIR = %q, want the one already set", got)
	}
}

package ws

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// MADR 0069 P2 (U5) — guidance composition: FDA copy only for a cwd
// permission error under a macOS-protected root on darwin; everything else
// stays untouched (the wrong remedy is worse than none).
func TestPermissionGuidanceFor(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	protected := filepath.Join(home, "Documents", "proj")
	unprotected := filepath.Join(home, "gitrepos", "proj")
	cwdErr := func(p string) error {
		return &provider.CWDPermissionError{
			Path: p,
			Err:  &os.PathError{Op: "stat", Path: p, Err: syscall.EPERM},
		}
	}

	base := "cwd blocked"
	if got := permissionGuidanceFor("darwin", cwdErr(protected), base); !strings.Contains(got, "Full Disk Access") {
		t.Fatalf("darwin+protected: no FDA guidance in %q", got)
	}
	if got := permissionGuidanceFor("darwin", cwdErr(unprotected), base); got != base {
		t.Fatalf("darwin+unprotected must stay plain, got %q", got)
	}
	if got := permissionGuidanceFor("linux", cwdErr(protected), base); got != base {
		t.Fatalf("linux must stay plain (~/Documents exists there too), got %q", got)
	}
	// A permission error that is not a cwd validation (e.g. spawn failure)
	// carries no path context — no guidance.
	spawn := fmt.Errorf("fork/exec /bin/x: %w", syscall.EPERM)
	if got := permissionGuidanceFor("darwin", spawn, base); got != base {
		t.Fatalf("non-cwd permission error must stay plain, got %q", got)
	}
}

func TestUnderMacProtectedRoot(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	for _, p := range []string{
		filepath.Join(home, "Documents"),
		filepath.Join(home, "Desktop", "x"),
		filepath.Join(home, "Downloads", "a", "b"),
		filepath.Join(home, "Library", "Mobile Documents", "doc"),
	} {
		if !underMacProtectedRoot(p) {
			t.Errorf("%s should be protected", p)
		}
	}
	for _, p := range []string{
		home,
		filepath.Join(home, "gitrepos"),
		filepath.Join(home, "DocumentsBackup"), // prefix, not child
		"/tmp/x",
	} {
		if underMacProtectedRoot(p) {
			t.Errorf("%s should not be protected", p)
		}
	}
}

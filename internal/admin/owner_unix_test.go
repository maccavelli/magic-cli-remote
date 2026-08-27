//go:build unix

package admin

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOwnedByCurrentUser covers the seam extracted in MADR 0116 D7. It used to
// be an inline `if ok && ...` that SKIPPED the ownership check when the stat
// could not be inspected; the extracted form fails closed instead.
func TestOwnedByCurrentUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := ownedByCurrentUser(path, fi)
	if err != nil {
		t.Fatalf("ownedByCurrentUser: %v", err)
	}
	if !owned {
		t.Error("a just-created file reported as not owned by the current user")
	}
}

// TestOwnedByCurrentUserUninspectable proves the fail-closed branch: a
// FileInfo whose Sys() is not a stat must be an error, never a silent "owned".
func TestOwnedByCurrentUserUninspectable(t *testing.T) {
	if _, err := ownedByCurrentUser("/nonexistent", fakeInfo{}); err == nil {
		t.Fatal("expected an error for an uninspectable FileInfo")
	}
}

// TestSocketIdentity covers the inode seam, including the not-ok branch the
// shutdown path treats as "do not remove".
func TestSocketIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := socketIdentity(fi)
	if !ok {
		t.Fatal("socketIdentity reported not-ok for a real file")
	}
	if id == 0 {
		t.Error("socketIdentity returned 0 with ok=true; callers treat 0 as unknown")
	}
	if _, ok := socketIdentity(fakeInfo{}); ok {
		t.Error("socketIdentity reported ok for an uninspectable FileInfo")
	}
}

// TestSecureSocket covers the chmod seam that Windows replaces with a DACL.
func TestSecureSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.sock")
	if err := os.WriteFile(path, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := secureSocket(path); err != nil {
		t.Fatalf("secureSocket: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("mode %04o still allows group/other", perm)
	}
	if err := secureSocket(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("secureSocket accepted a missing path")
	}
}

// TestSocketPath covers the path helper.
func TestSocketPath(t *testing.T) {
	if got, want := SocketPath("/run/mcremote"), filepath.Join("/run/mcremote", SocketName); got != want {
		t.Errorf("SocketPath = %q, want %q", got, want)
	}
}

// fakeInfo is an os.FileInfo whose Sys() carries nothing inspectable.
type fakeInfo struct{ os.FileInfo }

func (fakeInfo) Sys() any { return nil }

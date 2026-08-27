//go:build windows

package admin

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOwnedByCurrentUser proves a file this process just created reports as
// owned by it — the positive half of the MADR 0116 D7 check.
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

// TestOwnedByCurrentUserMissingFileErrors proves the check fails closed: a
// path it cannot inspect must be an error, never a silent "owned".
func TestOwnedByCurrentUserMissingFileErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.sock")
	if _, err := ownedByCurrentUser(path, nil); err == nil {
		t.Fatal("expected an error for an uninspectable path")
	}
}

// TestSocketIdentityStable proves the identity used to guard shutdown removal
// does not change between calls for the same file.
func TestSocketIdentityStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	a, okA := socketIdentity(first)
	b, okB := socketIdentity(second)
	if !okA || !okB {
		t.Skip("file index unavailable on this filesystem")
	}
	if a != b {
		t.Errorf("socketIdentity unstable: %d then %d", a, b)
	}
	if a == 0 {
		t.Error("socketIdentity returned 0 with ok=true; the caller treats 0 as unknown")
	}
}

// TestSecureSocketIsIdempotent proves applying the owner-only DACL twice is
// safe, matching the MADR 0116 D4 convergence contract.
func TestSecureSocketIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := secureSocket(path); err != nil {
		t.Fatalf("secureSocket: %v", err)
	}
	if err := secureSocket(path); err != nil {
		t.Fatalf("second secureSocket: %v", err)
	}
}

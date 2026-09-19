//go:build windows

package admin

import (
	"net"
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
//
// It used to t.Skip when the identity was unavailable, and on Windows it was
// ALWAYS unavailable, so this test could never fail (MADR 0159 F4). A not-ok
// result is now a failure: without an identity the shutdown guard is dead code.
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
	a, okA := socketIdentity(path, first)
	b, okB := socketIdentity(path, second)
	if !okA || !okB {
		t.Fatalf("socketIdentity unavailable (ok=%v,%v): the shutdown guard cannot run", okA, okB)
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

// TestSocketIdentityOfALiveSocket covers the case the guard exists for: a real
// AF_UNIX socket file, which on Windows is a reparse point (MADR 0159 probe 2).
// The identity must be non-zero, stable across calls, and different for a
// different socket, or the "is this still my socket?" check is meaningless.
func TestSocketIdentityOfALiveSocket(t *testing.T) {
	dir := t.TempDir()
	listen := func(name string) string {
		p := filepath.Join(dir, name)
		ln, err := net.Listen("unix", p)
		if err != nil {
			t.Fatalf("listen %s: %v", p, err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		return p
	}
	a, b := listen("a.sock"), listen("b.sock")
	fa, err := os.Lstat(a)
	if err != nil {
		t.Fatal(err)
	}
	id1, ok1 := socketIdentity(a, fa)
	id2, ok2 := socketIdentity(a, fa)
	if !ok1 || !ok2 || id1 == 0 {
		t.Fatalf("live socket identity unavailable: %d/%v %d/%v", id1, ok1, id2, ok2)
	}
	if id1 != id2 {
		t.Errorf("identity unstable: %d then %d", id1, id2)
	}
	fb, err := os.Lstat(b)
	if err != nil {
		t.Fatal(err)
	}
	other, ok := socketIdentity(b, fb)
	if !ok || other == id1 {
		t.Errorf("a different socket must have a different identity: %d (ok=%v) vs %d", other, ok, id1)
	}
}

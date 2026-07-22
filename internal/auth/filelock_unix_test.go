//go:build unix

package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A wedged lock-holder must not pin the caller forever: flockWithTimeout waits
// only up to the timeout, then fails so the caller (and, through Validate, all
// authentication) can recover instead of hanging.
func TestFlockWithTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	holder, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := unix.Flock(int(holder.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	contender, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer contender.Close()

	start := time.Now()
	if err := flockWithTimeout(int(contender.Fd()), 100*time.Millisecond); err == nil {
		t.Fatal("expected a timeout while the lock is held")
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("returned after %s; did not wait the full timeout", elapsed)
	}

	// After the holder releases, acquisition succeeds promptly.
	if err := unix.Flock(int(holder.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := flockWithTimeout(int(contender.Fd()), time.Second); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	_ = unix.Flock(int(contender.Fd()), unix.LOCK_UN)
}

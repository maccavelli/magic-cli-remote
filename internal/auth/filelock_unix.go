//go:build unix

package auth

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// lockTimeout bounds how long an operation waits for the cross-process flock.
// Legitimate holders (a small devices.json rewrite) release in milliseconds; a
// hold this long means a peer process wedged while holding the lock. Blocking
// forever there would stall ALL authentication (Validate takes this lock), so a
// bounded failure the caller can retry is strictly better than an indefinite hang.
const lockTimeout = 5 * time.Second

// withPathLock holds an exclusive advisory flock on path+".lock" for the
// duration of fn. CLI and daemon both open their own Store instances against
// the same devices.json; without this, concurrent read-modify-write can drop
// devices or resurrect revokes (Phase 1.5 / P1-5).
func withPathLock(path string, fn func() error) error {
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open lock %s: %w", lockPath, err)
	}
	defer f.Close()
	if err := flockWithTimeout(int(f.Fd()), lockTimeout); err != nil {
		return fmt.Errorf("flock %s: %w", lockPath, err)
	}
	defer func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }()
	return fn()
}

// flockWithTimeout acquires an exclusive lock, retrying the non-blocking flock
// until it succeeds or the timeout elapses — so a wedged lock-holder cannot pin
// the caller (and, through Validate, all authentication) forever.
func flockWithTimeout(fd int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if err != unix.EWOULDBLOCK {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("lock busy for more than %s: %w", timeout, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

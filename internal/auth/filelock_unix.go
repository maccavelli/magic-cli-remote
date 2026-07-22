//go:build unix

package auth

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

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
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("flock %s: %w", lockPath, err)
	}
	defer func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }()
	return fn()
}

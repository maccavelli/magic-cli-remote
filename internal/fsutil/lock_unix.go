//go:build unix

package fsutil

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// WithLock holds an exclusive advisory flock on path+".lock" for the duration of fn.
func WithLock(path string, timeout time.Duration, fn func() error) error {
	if path == "" {
		return fmt.Errorf("fsutil: empty lock path")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("fsutil: open lock %s: %w", lockPath, err)
	}
	defer f.Close()
	if err := flockWithTimeout(int(f.Fd()), timeout); err != nil {
		return fmt.Errorf("fsutil: flock %s: %w", lockPath, err)
	}
	defer func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }()
	return fn()
}

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

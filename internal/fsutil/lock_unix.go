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
	release, err := Acquire(path, timeout)
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

// Acquire takes the exclusive lock on path+".lock" and returns the function that
// releases it. The caller must call release exactly once.
//
// See the Windows file for why this exists alongside WithLock: two callers need
// the same exclusion with different shapes, and one acquire is what keeps them
// from drifting apart (MADR 0165 D2/D3).
func Acquire(path string, timeout time.Duration) (func(), error) {
	if path == "" {
		return nil, fmt.Errorf("fsutil: empty lock path")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("fsutil: open lock %s: %w", lockPath, err)
	}
	fd := int(f.Fd())

	// Blocking LOCK_EX, not LOCK_NB in a sleep loop: the kernel then knows a
	// waiter exists and grants the lock in turn. Polling never recorded the
	// waiter at all, so a holder that reacquired immediately could starve it for
	// the whole budget (MADR 0165 F1).
	acquired := make(chan error, 1)
	go func() { acquired <- unix.Flock(fd, unix.LOCK_EX) }()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case err := <-acquired:
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("fsutil: flock %s: %w", lockPath, err)
		}
		return func() {
			_ = unix.Flock(fd, unix.LOCK_UN)
			_ = f.Close()
		}, nil

	case <-timer.C:
		// Unix has no timed flock — not on Linux, not on macOS — so the wait
		// cannot be cancelled the way the Windows one can. The caller is bounded
		// regardless: it returns now, and this goroutine cleans up if and when the
		// wedged holder ever releases. It touches nothing the caller can observe
		// and holds no lock of ours afterwards (MADR 0165 D1, Consequences).
		go func() {
			if err := <-acquired; err == nil {
				_ = unix.Flock(fd, unix.LOCK_UN)
			}
			_ = f.Close()
		}()
		return nil, fmt.Errorf("fsutil: flock %s: lock busy for more than %s: %w",
			lockPath, timeout, unix.EWOULDBLOCK)
	}
}

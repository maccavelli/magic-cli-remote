//go:build windows

package fsutil

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// WithLock holds an exclusive byte-range lock on path+".lock" for the duration
// of fn.
//
// LockFileEx is MANDATORY on Windows, unlike flock's advisory semantics on
// Unix, so a holder genuinely blocks a peer's write rather than relying on
// cooperation. The lock is taken over byte range [0,1) of a zero-length file,
// the conventional whole-file lock idiom (MADR 0116 D6).
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
	// Defer order is load-bearing: defers run LIFO, so registering Close first
	// and the unlock second means the unlock runs BEFORE the handle closes.
	defer f.Close()
	h := windows.Handle(f.Fd())
	if err := lockWithTimeout(h, timeout); err != nil {
		return fmt.Errorf("fsutil: lock %s: %w", lockPath, err)
	}
	defer func() {
		var ol windows.Overlapped
		_ = windows.UnlockFileEx(h, 0, 1, 0, &ol)
	}()
	return fn()
}

// lockWithTimeout acquires an exclusive lock, retrying the non-blocking
// LockFileEx until it succeeds or the timeout elapses — so a wedged holder
// cannot pin the caller forever.
func lockWithTimeout(h windows.Handle, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var ol windows.Overlapped
		err := windows.LockFileEx(h,
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0, 1, 0, &ol)
		if err == nil {
			return nil
		}
		if err != windows.ERROR_LOCK_VIOLATION && err != windows.ERROR_IO_PENDING {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("lock busy for more than %s: %w", timeout, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

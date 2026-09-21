//go:build windows

package fsutil

import (
	"fmt"
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
// It exists because two callers need the same exclusion with different shapes:
// WithLock runs a closure under the lock, while a caller that acquires now and
// releases later (cert generation) cannot express itself as a closure. Sharing
// one acquire is what stops the two drifting into locks that exclude
// differently — a divergence a comment can promise but not enforce
// (MADR 0165 D2/D3).
func Acquire(path string, timeout time.Duration) (func(), error) {
	if path == "" {
		return nil, fmt.Errorf("fsutil: empty lock path")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	lockPath := path + ".lock"
	name, err := windows.UTF16PtrFromString(lockPath)
	if err != nil {
		return nil, fmt.Errorf("fsutil: open lock %s: %w", lockPath, err)
	}
	// FILE_FLAG_OVERLAPPED is load-bearing, not decoration: without it LockFileEx
	// completes synchronously, the event is never signalled, and the wait below
	// can neither time out nor be cancelled. os.OpenFile cannot request it, which
	// is why this opens the handle directly (MADR 0165 P1).
	h, err := windows.CreateFile(name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, fmt.Errorf("fsutil: open lock %s: %w", lockPath, err)
	}
	if err := lockWithTimeout(h, timeout); err != nil {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("fsutil: lock %s: %w", lockPath, err)
	}
	return func() {
		var ol windows.Overlapped
		_ = windows.UnlockFileEx(h, 0, 1, 0, &ol)
		_ = windows.CloseHandle(h)
	}, nil
}

// lockWithTimeout waits for the exclusive lock in the KERNEL's queue, bounded by
// timeout.
//
// It used to ask for the lock with LOCKFILE_FAIL_IMMEDIATELY and sleep 20ms
// between attempts. That never told Windows a waiter existed, so there was no
// queue and no fairness: a waiter acquired only if it happened to poll during a
// gap between the holder's release and its next acquire, and against a holder
// that reacquires immediately that gap is effectively zero. A waiter could
// therefore burn its whole budget and fail while the lock was available
// thousands of times (MADR 0165 F1).
//
// Blocking LockFileEx puts the waiter in the kernel's queue instead, so it is
// granted the lock in turn rather than by luck. The bound is kept because a
// wedged holder must not pin authentication forever (MADR 0165 F3).
func lockWithTimeout(h windows.Handle, timeout time.Duration) error {
	// Manual-reset, initially unsignalled: LockFileEx signals it when the lock is
	// granted OR when the pending request is aborted.
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(event) }()

	ol := &windows.Overlapped{HEvent: event}
	err = windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol)
	if err == nil {
		return nil
	}
	if err != windows.ERROR_IO_PENDING {
		return err
	}

	milliseconds := timeout / time.Millisecond
	if milliseconds < 1 {
		milliseconds = 1
	}
	state, err := windows.WaitForSingleObject(event, uint32(milliseconds))
	if err != nil {
		return err
	}
	switch state {
	case windows.WAIT_OBJECT_0:
		return nil
	case uint32(windows.WAIT_TIMEOUT):
		return cancelPendingLock(h, ol, timeout)
	default:
		return fmt.Errorf("unexpected wait state %#x while locking", state)
	}
}

// cancelPendingLock abandons a lock request that outlived its budget, and
// releases the lock if the kernel granted it anyway.
//
// That last clause is the whole point. CancelIoEx races the grant: the lock can
// be handed over between WaitForSingleObject returning WAIT_TIMEOUT and the
// cancel taking effect, and a cancel that "succeeded" does not mean the lock was
// not taken. Returning without checking leaks the lock for the lifetime of the
// process — the handle stays open, so nothing ever releases it, and every later
// caller times out. GetOverlappedResult reports which actually happened
// (MADR 0165 C3).
func cancelPendingLock(h windows.Handle, ol *windows.Overlapped, timeout time.Duration) error {
	_ = windows.CancelIoEx(h, ol)
	var done uint32
	// wait=true: the operation is already finishing one way or the other, so this
	// reaps it rather than waiting on anything new.
	switch err := windows.GetOverlappedResult(h, ol, &done, true); err {
	case nil:
		// Granted after all. Release it so the next caller is not locked out by a
		// request we already reported as failed.
		var uol windows.Overlapped
		_ = windows.UnlockFileEx(h, 0, 1, 0, &uol)
	case windows.ERROR_OPERATION_ABORTED:
		// The ordinary path: the request was cancelled and no lock was taken.
	default:
		// Anything else is unexpected; report the timeout, which is what the
		// caller experienced, rather than inventing a different failure.
	}
	return fmt.Errorf("lock busy for more than %s: %w", timeout, windows.WAIT_TIMEOUT)
}

//go:build !windows

package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestRenameNeverRetriesOffWindows pins MADR 0153 C1 from the other side.
//
// A POSIX rename cannot be blocked by a reader, so there is nothing to wait
// out. The contract is that the retry loop runs exactly once here, and the
// error this feeds it is deliberately the one most likely to tempt a future
// cross-platform predicate: EACCES, which reads like "access denied" and is
// the permanent answer on this platform. Retrying it would turn an immediate
// permission error into a slow one on the system that never had the bug.
func TestRenameNeverRetriesOffWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.json")

	calls := 0
	ops := realOps()
	ops.rename = func(string, string) error {
		calls++
		return &os.LinkError{Op: "rename", Err: syscall.EACCES}
	}
	ops.sleep = func(time.Duration) {
		t.Error("sleep must never be reached off Windows: the retry is a compile-time no-op there")
	}

	err := writeFileAtomic(path, []byte("v2"), AtomicOptions{}, ops)
	if err == nil {
		t.Fatal("expected the rename failure to surface")
	}
	if calls != 1 {
		t.Errorf("rename called %d times, want exactly 1", calls)
	}
	if !errors.Is(err, syscall.EACCES) {
		t.Errorf("err = %v, want it to wrap the original EACCES unchanged", err)
	}
}

// The predicate itself, stated directly. If someone later replaces the two
// build-tagged files with one cross-platform implementation, this fails before
// the behavioural test above does, and says why.
func TestRetryableRenameErrIsAlwaysFalseOffWindows(t *testing.T) {
	for _, err := range []error{
		nil,
		syscall.EACCES,
		syscall.EPERM,
		syscall.EBUSY,
		errors.New("access is denied"),
		&os.LinkError{Op: "rename", Err: syscall.EACCES},
	} {
		if retryableRenameErr(err) {
			t.Errorf("retryableRenameErr(%v) = true; off Windows it must be unconditionally false", err)
		}
	}
}

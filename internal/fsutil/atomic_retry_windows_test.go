//go:build windows

package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// heldOpenErr is what os.Rename returns when something holds the destination:
// a *os.LinkError wrapping ERROR_ACCESS_DENIED. Constructing it here rather
// than asserting on a string keeps these tables honest about what the OS
// produces (MADR 0153 F2).
func heldOpenErr() error {
	return &os.LinkError{Op: "rename", Err: windows.ERROR_ACCESS_DENIED}
}

// retryOps returns ops whose rename fails with err for the first failures
// calls and then succeeds, counting every call and never really sleeping.
func retryOps(t *testing.T, failures int, err error, calls *int) fileOps {
	t.Helper()
	ops := realOps()
	realRename := ops.rename
	ops.rename = func(oldpath, newpath string) error {
		*calls++
		if *calls <= failures {
			return err
		}
		return realRename(oldpath, newpath)
	}
	ops.sleep = func(time.Duration) {}
	return ops
}

func TestRenameRetriesWhileTheDestinationIsHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.json")
	calls := 0
	ops := retryOps(t, 2, heldOpenErr(), &calls)

	if err := writeFileAtomic(path, []byte("v2"), AtomicOptions{}, ops); err != nil {
		t.Fatalf("write should have survived a held destination: %v", err)
	}
	if calls != 3 {
		t.Errorf("rename called %d times, want 3 (two failures then success)", calls)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "v2" {
		t.Errorf("file = %q, %v; want the new bytes", b, err)
	}
}

func TestRenameGivesUpAfterTheBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.json")
	calls := 0
	// More failures than the budget: the retry must stop on its own.
	ops := retryOps(t, 99, heldOpenErr(), &calls)

	err := writeFileAtomic(path, []byte("v2"), AtomicOptions{}, ops)
	if err == nil {
		t.Fatal("a permanently held destination must still fail")
	}
	// The exact count is the assertion. An off-by-one in a retry budget is
	// invisible without it, and "it eventually failed" would pass with any
	// number of attempts including an unbounded one.
	if want := len(renameBackoffs) + 1; calls != want {
		t.Errorf("rename called %d times, want exactly %d (one attempt per backoff, plus the first)", calls, want)
	}
	// C3: the caller sees the original error, not a retry-flavoured one.
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Errorf("err = %v, want it to wrap the original ERROR_ACCESS_DENIED", err)
	}
}

func TestRenameDoesNotRetryAPermanentError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.json")
	calls := 0
	// ERROR_FILE_NOT_FOUND is not a held-open condition; retrying it wastes the
	// budget and changes nothing. This is the case that catches a predicate
	// which returns true for everything — every other table here would pass.
	ops := retryOps(t, 99, &os.LinkError{Op: "rename", Err: windows.ERROR_FILE_NOT_FOUND}, &calls)

	if err := writeFileAtomic(path, []byte("v2"), AtomicOptions{}, ops); err == nil {
		t.Fatal("expected the permanent error to surface")
	}
	if calls != 1 {
		t.Errorf("rename called %d times, want exactly 1: a permanent error must not be retried", calls)
	}
}

// TestWriteFileAtomicSurvivesARealHeldHandle is MADR 0153 A7, and it is the
// only test here that can catch the mistake this record exists to document.
//
// Every table above injects an error, so all of them would pass just as
// happily against a predicate keyed on ERROR_SHARING_VIOLATION — the errno the
// condition is named after and the one Windows does not actually return. This
// one holds the destination with a real handle and lets the operating system
// decide what the failure is.
func TestWriteFileAtomicSurvivesARealHeldHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.json")
	if err := os.WriteFile(path, []byte("v1"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("utf16: %v", err)
	}
	// Sharing reads only — no FILE_SHARE_DELETE, which is what blocks a rename.
	// This is how antivirus, backup agents and most Win32 applications open a
	// file they are reading.
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("hold the destination: %v", err)
	}
	closed := make(chan struct{})
	// Release well inside the ~150ms budget, so a passing run means the retry
	// worked rather than that the hold expired on its own.
	go func() {
		time.Sleep(30 * time.Millisecond)
		windows.CloseHandle(h)
		close(closed)
	}()

	if err := WriteFileAtomic(path, []byte("v2"), AtomicOptions{}); err != nil {
		t.Fatalf("write lost the race against a real reader: %v", err)
	}
	<-closed
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "v2" {
		t.Errorf("file = %q, %v; want the new bytes", b, err)
	}
}

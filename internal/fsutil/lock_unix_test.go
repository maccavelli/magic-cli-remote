//go:build unix

package fsutil

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestWithLockRejectsEmptyPath covers the precondition both platform
// implementations share.
func TestWithLockRejectsEmptyPath(t *testing.T) {
	called := false
	err := WithLock("", time.Second, func() error { called = true; return nil })
	if err == nil {
		t.Fatal("expected an error for an empty lock path")
	}
	if called {
		t.Error("fn ran despite the precondition failing")
	}
}

// TestWithLockSerializes proves the lock actually excludes. It is the Unix
// twin of the Windows LockFileEx test, so both platforms assert the same
// property rather than only one being checked (MADR 0116 D6).
func TestWithLockSerializes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	var mu sync.Mutex
	inside, maxInside := 0, 0

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := WithLock(path, 5*time.Second, func() error {
				mu.Lock()
				inside++
				if inside > maxInside {
					maxInside = inside
				}
				mu.Unlock()

				time.Sleep(20 * time.Millisecond)

				mu.Lock()
				inside--
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Errorf("WithLock: %v", err)
			}
		}()
	}
	wg.Wait()
	if maxInside != 1 {
		t.Errorf("max concurrent holders = %d, want 1", maxInside)
	}
}

// TestWithLockZeroTimeoutGetsDefault covers the timeout-normalising branch.
func TestWithLockZeroTimeoutGetsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	ran := false
	if err := WithLock(path, 0, func() error { ran = true; return nil }); err != nil {
		t.Fatalf("WithLock with a zero timeout: %v", err)
	}
	if !ran {
		t.Error("fn did not run")
	}
}

// TestWithLockPropagatesFnError proves the closure's error reaches the caller
// rather than being masked by the unlock.
func TestWithLockPropagatesFnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	want := errSentinel{}
	if err := WithLock(path, time.Second, func() error { return want }); err != want {
		t.Errorf("err = %v, want the closure's error", err)
	}
}

type errSentinel struct{}

func (errSentinel) Error() string { return "sentinel" }

// TestWithLockTimesOut exercises flockWithTimeout's busy path, which is the
// half that was uncovered.
func TestWithLockTimesOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		_ = WithLock(path, 5*time.Second, func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	start := time.Now()
	err := WithLock(path, 200*time.Millisecond, func() error {
		t.Error("acquired a lock that was already held")
		return nil
	})
	elapsed := time.Since(start)
	close(release)
	<-done

	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed < 150*time.Millisecond {
		t.Errorf("returned after %s, want at least the 200ms timeout", elapsed)
	}
}

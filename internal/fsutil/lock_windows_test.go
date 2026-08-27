//go:build windows

package fsutil

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestWithLockSerializes proves the Windows lock actually excludes, rather
// than being the no-op it replaced (MADR 0116 D6/F12).
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

// TestWithLockTimesOut proves a wedged holder yields a bounded failure the
// caller can retry, not an indefinite hang.
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

// TestWithLockReleasesForNextCaller proves the deferred UnlockFileEx runs
// before the handle closes, so a second sequential call succeeds.
func TestWithLockReleasesForNextCaller(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	for i := range 3 {
		if err := WithLock(path, time.Second, func() error { return nil }); err != nil {
			t.Fatalf("WithLock call %d: %v", i, err)
		}
	}
}

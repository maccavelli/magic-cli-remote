package auth

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// This replaces filelock_unix_test.go's TestFlockWithTimeout, which drove the
// acquire loop this package used to carry. That loop is gone: both platforms now
// delegate to internal/fsutil (MADR 0165 D3), so there is no package-local
// function left to test.
//
// The assertion it made — a wedged holder yields a bounded failure rather than an
// indefinite hang, and the lock is acquirable again after release — is not lost.
// It moved to where the code moved: fsutil's TestWithLockTimesOut asserts the
// bound on both platforms, and TestAcquireReleasesForTheNextCaller asserts the
// release. Testing it here would mean waiting out the real 5s lockTimeout, which
// withPathLock does not parameterise.
//
// What is asserted here instead is the property that is genuinely this package's:
// withPathLock actually excludes. It is also now cross-platform, where the test
// it replaces was Unix-only — Windows had no auth-level lock coverage at all.

// TestWithPathLockExcludes proves devices.json cannot be read-modify-written
// concurrently, which is the whole reason this lock exists: without it the CLI
// and the daemon drop devices or resurrect revokes.
func TestWithPathLockExcludes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")

	var mu sync.Mutex
	inside, maxInside := 0, 0

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := withPathLock(path, func() error {
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
				t.Errorf("withPathLock: %v", err)
			}
		}()
	}
	wg.Wait()

	if maxInside != 1 {
		t.Errorf("max concurrent holders = %d, want 1", maxInside)
	}
}

// TestWithPathLockReleases proves the lock is available to the next caller, so a
// sequence of Store writes cannot deadlock against itself.
func TestWithPathLockReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	for i := range 3 {
		if err := withPathLock(path, func() error { return nil }); err != nil {
			t.Fatalf("withPathLock call %d: %v", i, err)
		}
	}
}

// TestWithPathLockPropagatesError proves the closure's error is not masked by the
// unlock, which is what makes a failed write visible to the caller.
func TestWithPathLockPropagatesError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	want := errLockSentinel{}
	if err := withPathLock(path, func() error { return want }); err != want {
		t.Errorf("err = %v, want the closure's error", err)
	}
}

type errLockSentinel struct{}

func (errLockSentinel) Error() string { return "sentinel" }

package fsutil

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// These are acceptance criteria A1 and A3 of PLAN 0165. They are deliberately
// platform-agnostic: the defect was the same algorithm on both platforms, so
// asserting it on one would leave the other free to regress.

// Holder shape for the fairness test. These numbers are measured, not chosen:
// they are the shape that separates a queued acquire from a polling one while
// leaving the queued path room to be slow on a loaded runner.
//
//	                       queued            polling
//	2 x 100ms, 600ms       10/10 (worst 151ms)   0/10
//	2 x 100ms, 400ms       10/10 (worst 152ms)   0/10
//	2 x 150ms, 900ms       10/10                 1/10   <- polling starts winning
//	3 x 100ms, 900ms       10/10                 3/10   <- and wins more
//	1 x   3ms,   1s        10/10                10/10   <- proves nothing
//
// Two properties are in tension and both matter. The budget must be long enough
// that a queued waiter is never flaky, and short enough that a POLLING waiter
// still cannot stumble into a free window — which is why a longer budget makes
// the test worse rather than better. Do not "tidy" these into round numbers
// without re-running the separation matrix (PLAN 0165 P1).
//
// fairWaitBudget is per-platform and lives in the _windows/_unix files beside
// this one: Windows queues waiters and Linux does not, so the two need different
// budgets for the same assertion (MADR 0165, amendment 2026-09-21).
const (
	fairHolders  = 2
	fairHoldTime = 100 * time.Millisecond
)

// TestAcquireQueuesBehindATightHolder is A1, and the test the whole of MADR 0165
// turns on.
//
// The holders below are the CI runner's accident made deliberate: hold the lock,
// release it, and immediately ask for it again. The free window between one
// holder's release and the next acquire is microseconds wide. A waiter that
// POLLS every 20ms essentially never lands in one, so it burns its whole budget
// and fails while the lock was free hundreds of times. A waiter the kernel has
// QUEUED is handed the lock at the next release, in turn.
//
// The assertion is not "it is faster" — that would be a benchmark pretending to
// be a test — but "it succeeds at all". Verified to discriminate: against the
// polling implementation this test fails 10 times out of 10.
func TestAcquireQueuesBehindATightHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")

	stop := make(chan struct{})
	var holders sync.WaitGroup
	for range fairHolders {
		holders.Add(1)
		go func() {
			defer holders.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				release, err := Acquire(path, 5*time.Second)
				if err != nil {
					// A holder losing the lock is not this test's subject, and
					// failing here would report the wrong thing.
					return
				}
				time.Sleep(fairHoldTime)
				release()
			}
		}()
	}
	t.Cleanup(func() {
		close(stop)
		holders.Wait()
	})

	// Let the contention establish itself, so the waiter arrives mid-storm rather
	// than winning the first uncontended acquire.
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	release, err := Acquire(path, fairWaitBudget)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("a waiter was starved by %d tight reacquire loops for %s: %v\n\n"+
			"This is the flake from MADR 0165 F1: the acquire is not queueing, so the "+
			"waiter only wins by landing in a microsecond-wide gap.", fairHolders, elapsed, err)
	}
	release()
}

// TestAcquireTimeoutLeavesTheLockAvailable is A3, and it guards the subtlest way
// this change can go wrong.
//
// On Windows a timed-out request is cancelled with CancelIoEx, which races the
// grant: the kernel can hand the lock over just as the wait expires. An
// implementation that cancels and returns without checking leaks the lock for the
// life of the process — the handle stays open, nothing releases it, and every
// later caller times out. The symptom looks nothing like the cause, so it is
// asserted directly rather than left to be noticed in production (PLAN 0165 C3).
func TestAcquireTimeoutLeavesTheLockAvailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")

	held, err := Acquire(path, 5*time.Second)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// Must fail: someone else holds it.
	if release, err := Acquire(path, 100*time.Millisecond); err == nil {
		release()
		held()
		t.Fatal("acquired a lock that was already held")
	}

	held()

	// The timed-out attempt above must not have taken the lock on its way out.
	next, err := Acquire(path, time.Second)
	if err != nil {
		t.Fatalf("the lock was not available after a timed-out attempt: %v\n\n"+
			"A cancelled request took the lock and never released it.", err)
	}
	next()
}

// TestAcquireReleasesForTheNextCaller is the sequential companion: release must
// actually release, not merely close a handle.
func TestAcquireReleasesForTheNextCaller(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	for i := range 3 {
		release, err := Acquire(path, time.Second)
		if err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
		release()
	}
}

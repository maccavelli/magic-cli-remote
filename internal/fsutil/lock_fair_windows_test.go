//go:build windows

package fsutil

import "time"

// fairWaitBudget is how long the waiter in TestAcquireQueuesBehindATightHolder
// may wait on Windows.
//
// 600ms, measured. `LockFileEx` without `LOCKFILE_FAIL_IMMEDIATELY` puts the
// waiter in the kernel's queue, so it is granted the lock at the next release:
// across 10 trials of the shared holder shape the worst wait was **151ms**, which
// this budget clears four times over.
//
// It is also short enough to still be a real test. Against the polling
// implementation this budget produces **0 successes in 10 trials**, whereas at
// 900ms polling starts winning (1/10, then 3/10 with three holders) — so raising
// it would quietly turn the test into one that passes either way, which is what
// the first version of it did (PLAN 0165, deviation 2026-09-21).
const fairWaitBudget = 600 * time.Millisecond

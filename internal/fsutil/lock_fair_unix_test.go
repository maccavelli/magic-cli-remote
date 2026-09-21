//go:build unix

package fsutil

import "time"

// fairWaitBudget is how long the waiter in TestAcquireQueuesBehindATightHolder
// may wait on Unix. It is five times the Windows budget, and that is not slack —
// it is the platform difference.
//
// `flock(2)` has no ordering guarantee. A waiter is woken when the lock is
// released, but a holder that immediately re-requests can barge ahead of it, so
// service is eventual rather than in turn. Measured on Linux with the shared
// holder shape, 10 trials per row:
//
//	budget   acquired   worst wait
//	600ms      9/10       600ms (one starved outright)
//	3s        10/10       1.054s
//
// Windows, the same shape: 10/10 with a worst wait of 151ms. So Unix needs ~7×
// the headroom for the same assertion (MADR 0165, amendment 2026-09-21).
//
// One consequence worth stating plainly: at this budget the test does NOT
// discriminate against the polling implementation on Unix — 3s is long enough for
// a poller to stumble into a free window. The discrimination proof is the Windows
// one, which is also where the flake that prompted all this occurred. Here the
// test asserts the weaker property that a waiter is eventually served at all.
const fairWaitBudget = 3 * time.Second

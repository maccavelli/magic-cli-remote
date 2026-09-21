---
status: proposed
date: 2026-09-21
---

<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0165 — Queue the file lock, and stop a test asserting past its own fault point

Implements [0165-MADR-ci-flakes-are-a-starving-lock-and-an-overreaching-assertion.md](0165-MADR-ci-flakes-are-a-starving-lock-and-an-overreaching-assertion.md)
decisions D1–D7, closing findings F1–F8.

## Goal

Observable states, not activities:

1. A waiter for a contended file lock is **queued by the OS**. A test that holds
   the lock in a tight reacquire loop starves a polling waiter and does **not**
   starve a queued one, and that difference is asserted (**F1**, **D4**).
2. `fsutil.WithLock` still fails after its timeout when the holder never releases —
   `TestWithLockTimesOut` passes unchanged on both platforms (**F3**).
3. Exactly **one** implementation of the acquire algorithm exists.
   `internal/auth/filelock_unix.go` no longer contains a copy, and
   `internal/certs` obtains its lock from `fsutil` on both platforms (**F4**, **F5**).
4. `go test ./internal/auth/ -run TestStoreConcurrentCreateTwoHandles -count=20`
   passes, and the 5 s and 10 s budgets are unchanged (**D7**).
5. `TestACPConnectionSurvivesAStalledPump` asserts the fault point as an exact
   number rather than waiting on a race, and passes 20 consecutive runs (**F7**,
   **D5**, **D6**).
6. No new `ci-flakes.tsv` row names either test in the runs after this ships.

## Scope

### In scope (the only files any phase may touch)

```text
P1  internal/fsutil/lock_windows.go            blocking overlapped acquire + Acquire()
    internal/fsutil/lock_unix.go               blocking flock acquire + Acquire()
    internal/fsutil/lock_fair_test.go          (new) the starvation test, both platforms
    internal/fsutil/lock_fair_windows_test.go  (new) the 600ms budget, per the amendment
    internal/fsutil/lock_fair_unix_test.go     (new) the 3s budget, per the amendment
    internal/fsutil/lock_windows_test.go   extend; TestWithLockTimesOut unchanged
    internal/fsutil/lock_unix_test.go      extend; TestWithLockTimesOut unchanged
P2  internal/auth/filelock_unix.go         delete the duplicated loop; delegate
    internal/auth/filelock_windows.go      comment only, if it still claims Windows-only
P3  internal/certs/filelock_windows.go     move onto fsutil.Acquire
    internal/certs/filelock_unix.go        same
P4  internal/provider/acpagent/stalledpump_test.go   bound the writer; assert the fault point
P5  docs/decisions/0165-PLAN-*.md          execution record only
```

### Out of scope

Named so the boundary is not mistaken for an oversight: the 5 s and 10 s budgets
(**D7**); `internal/providerauth`'s `opts.LockTimeout` and its five call sites,
which change behaviour only through `fsutil`; the ACP SDK itself; the other two
ledger entries (`TestResolveBinaryIdentityHelperDoesNotCountAsLaunch` is already
fixed under MADR 0163, and no third test is in the ledger); and any change to what
`deliver` does — **F8** found it correct.

## Stability rule

Every phase ends with, in order:

```bash
make pre-add-check FILES="<the phase's Go files>"
go test ./internal/fsutil/ ./internal/auth/ ./internal/certs/ -count=1
go test -race ./internal/fsutil/ ./internal/auth/ -count=1
make ci-windows
```

Phases touching cross-platform code (P1–P3) additionally run the WSL lane:
`go build ./... && go test ./internal/fsutil/ ./internal/auth/ ./internal/certs/`
under Ubuntu-24.04. P4 is a single test file and needs only its own package plus
`make ci-windows`.

Commit discipline: one phase, one commit, message written by the hook
(`git commit --no-edit`). `git push` and tags need an explicit instruction in the
same turn.

## Cross-cutting contracts

* **C1 — No assertion is weakened.** `TestWithLockTimesOut` keeps its current
  meaning on both platforms, and P4 must leave the stalled-pump test proving that
  the connection survives. Where an assertion changes, the new one is at least as
  strong and the test says why the old one was wrong.
* **C2 — The lock's exclusion semantics do not change.** Byte range `[0,1)` on
  `path + ".lock"`, exactly as MADR 0116 D6 set it. Only the waiting strategy
  changes, so a new binary and an old one still exclude each other.
* **C3 — No lock is ever leaked.** Every acquire path, including the Windows
  cancellation race and the abandoned Unix goroutine, either releases the lock or
  never held it. This is **the contract most at risk**: the tempting Windows
  implementation calls `CancelIoEx` and returns, which silently leaks the lock
  whenever the kernel granted it between the timeout and the cancel.
* **C4 — Budgets are untouched.** No phase edits `lockTimeout` or
  `certLockTimeout`.
* **C5 — Authentication must not be able to hang.** `Validate` takes this lock; a
  regression that blocks forever is worse than the flake being fixed. Every acquire
  returns within its timeout, and P1 asserts it.

## Dependency and delivery order

P1 must land first: P2 and P3 delete their copies in favour of what it adds. P2 and
P3 are independent of each other. P4 is independent of all of them and may land
first if convenient — it shares no file with any other phase.

## Implementation Steps

### P1 — One fair, bounded acquire in `fsutil` (D1, D2; closes F1, F5; proves F1 per D4)

1. Add `Acquire(path string, timeout time.Duration) (release func(), err error)`
   and reduce `WithLock` to `Acquire` + `defer release()` + `fn()`. `WithLock`'s
   signature and error strings stay as they are, so the five production callers and
   the tests that match on `fsutil: lock` text are unaffected.
2. **Windows.** Open the lock file with `windows.CreateFile(..., OPEN_ALWAYS,
   FILE_ATTRIBUTE_NORMAL|FILE_FLAG_OVERLAPPED, 0)` — settle **MADR open question 2**
   by running it; `os.OpenFile` does not request overlapped I/O, so the event is
   never signalled and the wait cannot be cancelled. Create a manual-reset event
   (`windows.CreateEvent`), put it in `OVERLAPPED.HEvent`, and call `LockFileEx`
   with `LOCKFILE_EXCLUSIVE_LOCK` **and without** `LOCKFILE_FAIL_IMMEDIATELY`:
   * `nil` → acquired synchronously.
   * `ERROR_IO_PENDING` → `WaitForSingleObject(event, ms)`.
     `WAIT_OBJECT_0` → acquired. `WAIT_TIMEOUT` → `CancelIoEx(h, &ol)`, then wait on
     the event once more with a zero timeout: **if it is signalled, the lock was
     granted and must be released before returning the timeout error** (**C3**).
   * any other error → return it unchanged.
3. **Unix.** Own the fd inside the acquire helper. Run blocking
   `unix.Flock(fd, LOCK_EX)` in a goroutine reporting to a buffered channel of 1,
   and `select` on it against `time.After(timeout)`. On timeout, return the same
   `lock busy for more than %s` error the polling version produced, and leave the
   goroutine to unlock and close the fd when it finally acquires — it must not
   touch any state the caller can observe. Document the asymmetry against Windows
   in the file, since it is the consequence the MADR calls out.
4. `lock_fair_test.go` (new, no build tag — it drives `Acquire` only): a holder
   goroutine takes and releases the lock in a tight loop for ~2 s while a waiter
   asks for it with a 1 s budget. Assert the waiter **acquires**. Give the test a
   hard ceiling so a regression fails rather than hangs.
5. Extend each platform's existing test file with: `Acquire` releases so the next
   caller succeeds; a lock whose holder never releases still times out (**C5**); and
   on Windows, that a timed-out acquire leaves the lock **available** to the next
   caller — the direct assertion of **C3**.

**Verification (P1).**

```bash
go test ./internal/fsutil/ -run 'WithLock|Acquire|Fair' -count=5 -v
go test -race ./internal/fsutil/ -count=1
# fail-first, per D4: revert ONLY the acquire body to the polling loop and rerun.
# lock_fair_test.go must fail; TestWithLockTimesOut must still pass.
```

The fail-first step is the phase's real deliverable. If the starvation test passes
against the polling implementation, it is not testing what it claims and must be
sharpened — record the ceiling and the loop shape that finally separated them, or
record that it could not be done and what the test therefore establishes.

### P2 — `auth` stops carrying its own copy (D3; closes F4 for auth)

1. Delete `flockWithTimeout` and the hand-rolled open from
   `internal/auth/filelock_unix.go`; `withPathLock` becomes
   `fsutil.WithLock(path, lockTimeout, fn)`, identical to the Windows file.
2. Check whether `internal/auth/filelock_windows.go`'s comment still describes
   reality once both platforms delegate, and correct it if not.
3. `lockTimeout` stays 5 s (**C4**).

**Verification (P2).**

```bash
go test ./internal/auth/ -count=1
go test ./internal/auth/ -run TestStoreConcurrentCreateTwoHandles -count=20
go test -race ./internal/auth/ -count=1
```

Twenty runs because one proves nothing about a race. Also confirm by inspection
that `internal/auth` no longer contains `LOCK_NB`.

### P3 — `certs` uses the primitive, keeping its own shape (D2, D3; closes F4 for certs)

1. Replace the loop in `internal/certs/filelock_windows.go` and
   `internal/certs/filelock_unix.go` with `fsutil.Acquire(dir/certLockName-derived
   path, certLockTimeout)`, returning the `release` func as `lockCertDir`'s unlock.
2. Mind the lock path: `fsutil` appends `".lock"` to what it is given, and `certs`
   already names a lock file. Pass the base so the derived path is the file `certs`
   locks today — a silent change here means two binaries stop excluding each other
   (**C2**).

   This exact mistake has been made before and is regression-tested twice, so read
   those first: `internal/provider/credstore/lockfile_test.go:16` and
   `internal/providerauth/nativelock_test.go:21,42` both exist because a path that
   already ended in `.lock` became `auth.json.lock.lock`, and the daemon then locked
   a file nothing else took. `0133-PLAN-recovery-must-not-wedge-on-a-transient-observation.md:286`
   records the measurement ("the `.lock` suffix is applied twice"). `certs` is the
   third caller with a pre-named lock file, which makes it the third candidate for
   the same bug.
3. Rewrite the comment at `filelock_windows.go:17-20`: it currently claims the
   loops match by inspection, which is exactly what stops being necessary.

**Verification (P3).**

```bash
go test ./internal/certs/ -count=1
go test -race ./internal/certs/ -count=1
# C2: the lock file certs creates must have the same name as before the change.
```

Record the lock filename before and after in the execution record.

### P4 — ~~The stalled-pump test stops racing its own fault~~ **WITHDRAWN 2026-09-21**

**Do not execute this phase.** Reproduction showed flake 2 is an intermittent
failure of MADR 0138 F5 — the ACP transport is genuinely torn down — not the
over-assertion F7/F8 described. Every step below would have produced a test that
tolerates that teardown. See the MADR's 2026-09-21 amendment and **MADR 0166**,
which owns the real problem. The steps are kept unedited so the record shows what
was planned.

~~Original steps:~~

1. Bound the writer by the session: `select` on `s.done` inside the write loop so
   it stops when the session faults, and report how many frames were accepted.
2. Replace "every frame must be accepted" with the arithmetic from **F7**: with a
   consumer that never drains, the session must fault after exactly
   `cap(s.events) + controlOverflowCap` frames. Derive the expected count from those
   constants rather than writing `513`, so the test follows the code if either
   changes.
3. Keep the second assertion — the session must fault — and keep the comment
   explaining that a permanently stalled consumer faulting is the correct outcome.
4. State in the test why the old assertion was wrong (**C1**): `io.Pipe` blocks
   rather than erroring, so once the fault stops the SDK reading, the remaining
   writes hang and the 20 s timeout fires.

**Verification (P4).**

```bash
go test ./internal/provider/acpagent/ -run StalledPump -count=20 -v
go test -race ./internal/provider/acpagent/ -count=1
# fail-first: raise controlOverflowCap's effect by parking one fewer event and the
# expected-count assertion must fail, naming both numbers.
```

### P5 — Record what execution taught (no decisions)

Append an execution record to this plan: which phases ran, the fail-first results
from P1 and P4, the `certs` lock filename before and after, and — most valuable —
anything this plan predicted incorrectly.

## Verification (whole plan)

```bash
make pre-add-check
go test ./... -count=1 && go test -race ./internal/fsutil/ ./internal/auth/ ./internal/certs/ ./internal/provider/acpagent/
make ci-windows
# WSL lane
go build ./... && go test ./internal/fsutil/ ./internal/auth/ ./internal/certs/ -count=1
# and, after the next push, the ledger:
git show origin/master:ci-flakes.tsv | grep -E 'StoreConcurrentCreateTwoHandles|StalledPump'
```

### Acceptance criteria (mapped to the MADR's Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | A waiter against a tight reacquire loop is queued and acquires; the same test fails against the polling implementation | D1, D4 |
| A2 | `TestWithLockTimesOut` passes unchanged on both platforms | F3, C5 |
| A3 | A timed-out Windows acquire leaves the lock available to the next caller | C3 |
| A4 | `fsutil.WithLock`'s signature, error text and byte range are unchanged | C2 |
| A5 | No `LOCK_NB` or `LOCKFILE_FAIL_IMMEDIATELY` remains outside `fsutil` | D3, F4 |
| A6 | `internal/certs` locks the same filename as before | C2 |
| A7 | `TestStoreConcurrentCreateTwoHandles` passes 20 consecutive runs | F1, F2 |
| ~~A8~~ | ~~The stalled-pump test derives its expected frame count from the constants~~ — **withdrawn with P4** | D6, F7 |
| ~~A9~~ | ~~The stalled-pump test still asserts the session faults~~ — **withdrawn with P4**; the test is unchanged and still asserts it | C1 |
| A10 | `lockTimeout` and `certLockTimeout` are untouched | D7, C4 |
| A11 | No new ledger row names either test | — |

**A3 is the criterion most likely to be quietly dropped.** It asserts the absence of
a leak on a cancellation path that is hard to provoke, and the implementation
appears to work without it — right up to the first timeout in production, which
then wedges the lock for the life of the process. **A1 is second**: it is easy to
write a "fairness" test that passes against both implementations and therefore
proves nothing.

## Rollout and Rollback

* **One release, patch.** No wire shape, no protocol, no mobile change. The lock's
  exclusion semantics are unchanged (**C2**), so a mixed fleet is safe.
* The risky phase is **P1**: it touches the lock that authentication takes
  (**C5**). If it misbehaves the symptom is a hung or failing login, not a failing
  test, so it lands alone and `make ci-windows` runs before P2 begins.
* Rollback is per phase: each is one commit touching files no other phase touches,
  so reverting P1 restores the polling acquire and leaves P2–P4 coherent — except
  that P2 and P3 depend on `Acquire` existing, so a P1 revert means reverting those
  too. Revert in reverse order.
* `git push` and tags need an explicit instruction in the same turn.

## Deferred (named, so they are not mistaken for oversights)

* **Reducing `store_test.go`'s contention.** Option C in the MADR. The 40×2
  read-modify-writes are what expose a lost update; making the test gentler would
  hide **F2** rather than fix it. Left as it is deliberately.
* **`internal/providerauth`'s `opts.LockTimeout`.** Its callers inherit the new
  waiting strategy through `fsutil` without a change of their own. Whether their
  timeouts are well chosen is a separate question, and this plan does not answer it.
* **A single cross-process lock abstraction covering `certs`' "acquire now, release
  later" and `fsutil`'s closure form as one type.** P3 gives `certs` the shared
  algorithm, which is the correctness goal; collapsing the two shapes into one API
  is tidying, and tidying an authentication-adjacent lock is not worth doing in the
  same release.
* **The remaining ledger entries.** Only these two were traced. The ledger is the
  place to look for the next one, and the method that worked is in MADR 0163: read
  the full CI log, never the `--- FAIL` line alone.

## Deviation — 2026-09-21 (P1): the fairness test had to be earned twice

Both halves resolved by owner decision the same turn; the MADR carries the
amendment because the first one contradicts D1.

### The first fairness test proved nothing

Written as the plan described — a holder taking and releasing in a tight loop, a
waiter with a 1 s budget — it **passed against the polling implementation too**.
The gap between one `Acquire` returning and the next being requested is wide
enough (each reopens a handle) that a 20 ms poller lands in it within a second.
This is precisely what the plan warned about when it named A1 the second most
likely criterion to be quietly dropped: *"it is easy to write a 'fairness' test
that passes against both implementations and therefore proves nothing"*.

Fixed by measuring instead of guessing. A temporary matrix ran five holder shapes
against both implementations, 10 trials each:

| shape | queued | polling |
| --- | --- | --- |
| 1 × 3 ms, 1 s budget | 10/10 | **10/10** — proves nothing |
| 1 × 25 ms, 200 ms | 10/10 | 3/10 |
| 2 × 25 ms, 200 ms | 10/10 | 2/10 |
| 4 × 100 ms, 400 ms | 10/10 (worst 353 ms) | 0/10 |
| **2 × 100 ms, 600 ms** | **10/10 (worst 151 ms)** | **0/10** |
| 2 × 150 ms, 900 ms | 10/10 | 1/10 — a longer budget helps polling |
| 3 × 100 ms, 900 ms | 10/10 | 3/10 |

`2 × 100 ms / 600 ms` was chosen over `4 × 100 ms / 400 ms` because both separate
perfectly but the former leaves four times the headroom (151 ms against 600 ms)
rather than 47 ms, and a fairness test that is itself flaky is worse than none.
The matrix is recorded in the test file so the numbers cannot be "tidied" without
re-deriving them. Verified after the change: polling **FAIL**, queued **PASS**,
`TestWithLockTimesOut` **PASS** in both.

### Linux needed its own budget

See the MADR amendment. `flock(2)` is not FIFO, so the 600 ms budget that makes
the test discriminating on Windows starves the Linux waiter 1 in 10 runs. The
budget moves into per-platform files; everything else about the test is shared.

**Files added to P1's scope** by this deviation: `lock_fair_windows_test.go` and
`lock_fair_unix_test.go`, each holding only the budget constant and the
measurement that justifies it.

**A1 is narrowed, not dropped.** It now reads: the fairness test discriminates
against the polling implementation **on Windows**, and asserts eventual service on
Unix. The flake it exists for was on `windows/amd64`.

## Execution record — P1, P2, P3 (2026-09-21)

**Ran: P1, P2, P3.** Commits `e7a4997`, `2e7dbda`, `e184118`, with the amendment
`27a97b7`. **P4 withdrawn** — see above and MADR 0166. P5 is this record.

Gates, every phase: `pre-add-check` clean, `gofmt -l` empty, `go vet`, the touched
packages plus every lock user (`fsutil`, `auth`, `certs`, `providerauth`,
`credstore`) green, `-race` green, `make ci-windows` exit 0, and the WSL Linux lane
green. `TestStoreConcurrentCreateTwoHandles` passes **20 consecutive runs on both
platforms** (A7).

### What the plan predicted incorrectly

1. **The fairness test, twice.** Covered in the 2026-09-21 deviation: the first
   version passed against the polling implementation, and Linux then needed its own
   budget because `flock` is not FIFO.
2. **Unifying `certs` would have broken a build.** The plan's per-platform file list
   looked like duplication worth collapsing into one cross-platform file. It is not:
   `internal/certs/filelock_other.go` (`!unix && !windows`) is a deliberate no-op
   for js/wasm and plan9, and `fsutil` has no implementation there. The instinct to
   tidy would have broken those builds. What the files *could* collapse into is one
   `unix || windows` file mirroring `fsutil`'s own coverage exactly, which is what
   landed.
3. **P2 had a file it did not list.** `internal/auth/filelock_unix_test.go` tested
   the acquire loop P2 deletes, so the package stopped compiling on Linux. Rather
   than delete the coverage, it was retargeted at `withPathLock` and made
   cross-platform as `filelock_test.go` — Windows previously had no auth-level lock
   coverage at all. The bounded-timeout assertion it used to make now lives where
   the code lives, in `fsutil.TestWithLockTimesOut`.

### The most valuable result: A6's fail-first

Passing `certLockName` to `fsutil.Acquire` instead of the base — the double-`.lock`
bug this repository has shipped twice — is caught by **only one** of the three certs
lock tests:

```text
mutated: TestLockCertDirLocksTheSameFileAsBefore  FAIL
         TestLockCertDirExcludes                  PASS
         TestLockCertDirReleases                  PASS
```

The exclusion test passes because within a single process, locking the *wrong* file
still excludes perfectly. That is exactly why the bug shipped twice before: the
obvious test cannot see it. A6 is not a formality.

### Status

Goals 1-4 are met. Goal 5 (the stalled-pump test) is withdrawn and moves to
MADR 0166; goal 6 (no new ledger row) applies to
`TestStoreConcurrentCreateTwoHandles` only, since the stalled-pump test is expected
to keep failing under load until 0166 is resolved. The plan stays `in-progress`
until that expectation is either confirmed or 0166 lands.

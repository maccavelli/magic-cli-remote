---
status: proposed
date: 2026-09-21
decision-makers: Project Owner
consulted: none
informed: none
---

<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Two CI flakes: a file lock that starves its waiter, and an assertion that outlived its design

## Context and Problem Statement

`ci-flakes.tsv` — the ledger MADR 0143 added — recorded two fail-then-pass tests
that CI's retry hid:

| run | job | test | date |
| --- | --- | --- | --- |
| `35604693366` | Go (windows/amd64) | `TestStoreConcurrentCreateTwoHandles` | 2026-09-21 |
| `35461886119` | Go (windows/amd64) | `TestACPConnectionSurvivesAStalledPump` | 2026-09-19 |

A retried flake is not a harmless one. Both tests guard a real invariant — no lost
updates under concurrent read-modify-write, and an ACP connection surviving a
stalled consumer — and a test that fails for an unrelated reason can no longer be
trusted to fail for the right one. The first of these had already turned `master`
red outright once (run `35552774193`, the same test class as the identity flake
traced in MADR 0163).

They turned out to have nothing in common. One is a production locking defect; the
other is a test asserting something its own design stopped promising.

### What was measured, not assumed

Every claim below comes from the full CI logs (not a grep of the `--- FAIL` line —
that shortcut produced a wrong diagnosis earlier the same day, recorded in
PLAN 0163's 2026-09-21 deviation), from the source at `84ea811`, and from local
runs.

**Flake 1, the actual failure** — `internal/auth/store_test.go:259`, then `:275` as
a consequence of it:

```text
b create: fsutil: lock C:\Users\<user>\AppData\Local\Temp\TestStoreConcurrent…\devices.json.lock:
  lock busy for more than 5s: The process cannot access the file…
devices = 40, want 80 (lost updates under concurrent RMW)
```

The test took **6.34 s**, consistent with a 5 s budget expiring plus overhead. The
`fsutil:` prefix places it in `internal/fsutil`, which is the path Windows `auth`
takes (`internal/auth/filelock_windows.go:13` delegates there).

**Flake 2, the actual failure** — `internal/provider/acpagent/stalledpump_test.go:120`:

```text
--- FAIL: TestACPConnectionSurvivesAStalledPump (20.00s)
    the writer never finished; the SDK stopped reading, which means the connection went away
```

Exactly **20.00 s**: the `time.After(20 * time.Second)` branch, not a slow pass.
Locally the same test completes in **0.01 s**, five runs out of five. A CI runner is
not 2000× slower, so load is not the explanation — the writer stopped.

**Reproduction attempts for flake 1: 30 local runs, none reproduced.** Default
`GOMAXPROCS`, `GOMAXPROCS=1` (to starve the polling waiter's scheduling), and six
concurrent instances (to load the filesystem). Each run takes ~0.05 s locally
against ~6.34 s on CI. The mechanism below is therefore **inferred from the error
text and the implementation, not reproduced** — closing that gap is P1's first job.

**The lock implementations, counted.** Five polling loops, every one
non-blocking-plus-sleep:

| file:line | platform | primitive |
| --- | --- | --- |
| `internal/fsutil/lock_windows.go:54` | windows | `LockFileEx` + `LOCKFILE_FAIL_IMMEDIATELY` |
| `internal/fsutil/lock_unix.go:37` | unix | `Flock` + `LOCK_NB` |
| `internal/auth/filelock_unix.go:37` | unix | `Flock` + `LOCK_NB` |
| `internal/certs/filelock_windows.go:32` | windows | `LockFileEx` + `LOCKFILE_FAIL_IMMEDIATELY` |
| `internal/certs/filelock_unix.go:26` | unix | `Flock` + `LOCK_NB` |

All five retry every **20 ms**. Budgets: `internal/auth/filelock.go:10`
`lockTimeout = 5 * time.Second`; `internal/certs/filelock.go:8`
`certLockTimeout = 10 * time.Second`; `internal/providerauth` passes its own
`opts.LockTimeout`.

**Production callers of `fsutil.WithLock`**, which bound the blast radius of
changing it: `internal/auth/filelock_windows.go:13`,
`internal/providerauth/reconcile.go:335`, and
`internal/providerauth/transaction.go:138`, `:452`, `:573`.

**The Windows primitives needed for a cancellable blocking acquire exist in the
pinned dependency.** `golang.org/x/sys v0.47.0` (go.mod) exports `CreateFile`,
`CreateEvent`, `LockFileEx`, `WaitForSingleObject` and `CancelIoEx`.

**Flake 2's arithmetic**, all from `stalledpump_test.go` and `session.go`:

| quantity | value | source |
| --- | --- | --- |
| frames the test writes | 1224 (`1024 + 200`) | `stalledpump_test.go:98` |
| `s.events` capacity in the fixture | 1, and pre-filled | `:66`, `:72` |
| `controlOverflowCap` | 512 | `session.go` |
| frames absorbed before the fault | ~513 | 0 into `events` + 512 parked |
| transport between writer and SDK | `io.Pipe` | `:74` |

### Findings

**F1 — The shared lock cannot queue, so it can starve a waiter.** Every
implementation asks the OS for the lock in a way that *refuses to wait*
(`LOCKFILE_FAIL_IMMEDIATELY`, `LOCK_NB`) and then sleeps 20 ms. Neither platform
records that a waiter wants the lock, so there is no queue and no fairness: a
waiter acquires only if it happens to poll during a gap between the holder's
release and its next acquire. Against a tight `for i := 0; i < 40; i++` loop of
read-modify-writes (`store_test.go:246-263`) that gap is vanishingly small, and a
waiter can lose 250 consecutive polls and exhaust a 5 s budget. **[inferred, not
reproduced — see the measurement above]**

**F2 — The failure is user-visible, not merely a test artefact.** The same code
path serves the daemon and the CLI writing `devices.json`, and `Validate` takes
this lock on the authentication path (`internal/auth/filelock.go:5-9` says so).
Sustained concurrent writes can therefore fail after 5 s rather than queueing. The
second assertion in the flake — `devices = 40, want 80` — is exactly the lost-update
symptom the lock exists to prevent, reached because the write *errored out* rather
than waited.

**F3 — The 5 s budget is correct and must survive the fix.** Blocking forever is
worse: a wedged holder would stall all authentication. Both platforms already have
a `TestWithLockTimesOut`, so "bounded" is an asserted property, not an accident.
The defect is *how* the wait is spent, not that it is bounded.

**F4 — There are three copies of one algorithm, and one of them says so.**
`internal/auth/filelock_unix.go:34-49` is character-for-character the same function
as `internal/fsutil/lock_unix.go:34-49`, while `internal/auth/filelock_windows.go`
delegates to `fsutil` — so Unix has two implementations where Windows has one.
`internal/certs/filelock_windows.go:17-20` states outright that it does not
delegate because its contract differs (it returns an unlock func rather than
running a closure), and asserts that "the retry loop and the byte range match
fsutil's so the two cannot exclude differently". Changing `fsutil` alone would make
that comment false.

**F5 — `certs` needs a different shape, not a different algorithm.** Its contract —
acquire now, release later — is the only reason it holds a copy. An acquire
primitive returning a release func satisfies both shapes, which is what lets one
implementation serve all five sites.

**F6 — Flake 2's writer blocks because `io.Pipe` has no buffer and does not error.**
`io.Pipe` blocks a `Write` until a reader consumes it, and returns an error only
if the pipe is closed. So when the SDK's receive loop stops, the remaining writes
block indefinitely; they never surface as the error the test's other branch
(`:117`) is written to catch. That is why the failure appears as a 20 s timeout
rather than a write error.

**F7 — The test asserts more than the design promises, and its two assertions
conflict.** `deliver` absorbs 513 frames and then deliberately calls
`markClosedAndKill()` — the stall detector doing its job, which the test's *second*
assertion (`:125-129`) requires. But its *first* assertion requires all **1224**
writes to complete (`:111-121`, "Every frame must be accepted"). Nothing keeps the
SDK draining the remaining ~711 frames once the session is faulted. Passing locally
is the writer winning a race by microseconds, not a guarantee.

**F8 — No production defect is implicated in flake 2.** `deliver`
(`session.go:1414-1455`) is O(1) on every path: the channel takes the event, the
overflow parks it, or the session is faulted. `markClosedAndKill` closes `s.done`
and kills a process only when `s.cmd != nil`, which it is not in this fixture. The
faulting behaviour is correct; only the assertion around it is wrong.

## Decision Drivers

* A retried flake degrades the guard it hides behind — both tests protect
  invariants worth keeping (lost updates; transport survival).
* A fix must not weaken an assertion. `TestWithLockTimesOut` must still pass, and
  flake 2's fix must not stop the test proving that the connection survives.
* One algorithm in five places is how the comment in `certs` came to assert parity
  it cannot enforce.
* The Windows path is the one that flaked, and it is the one with a genuine OS
  queue available; the Unix path has no timed `flock` and needs a different
  mechanism for the same guarantee.
* Flake 1 is unreproduced locally. A fix that cannot be shown to fix anything is a
  guess, so the plan must manufacture the contention rather than hope for it.

## Considered Options

* **A — Make the acquire fair and bounded, in one shared primitive** (chosen).
* **B — Keep polling; add backoff and jitter.**
* **C — Lower the test's contention.**
* **D — Raise the 5 s budget.**

## Decision Outcome

Chosen: **A**, at the owner's direction.

### The decisions

* **D1 — Wait in the OS, not in a sleep loop.** Replace polling with a blocking
  acquire that the kernel queues, bounded by the caller's existing timeout.
  *(Amended 2026-09-21: "queues" holds on Windows only — see the amendment at the
  end of this record. Unix is woken on release without ordering.)* On
  Windows: `LockFileEx` **without** `LOCKFILE_FAIL_IMMEDIATELY` on an overlapped
  handle, waited with `WaitForSingleObject` and cancelled with `CancelIoEx` on
  expiry. On Unix: blocking `Flock(LOCK_EX)` on a goroutine-owned fd, with the
  caller bounded by a timer. Closes **F1**, keeps **F3**.
* **D2 — One primitive, two shapes.** Add `fsutil.Acquire(path, timeout) (release
  func(), err error)` and express `fsutil.WithLock` in terms of it. Closes **F5**.
* **D3 — Delete the duplicate and adopt the primitive everywhere.** Remove
  `internal/auth/filelock_unix.go`'s copy so `auth` delegates on both platforms,
  and move `internal/certs` onto `Acquire` on both platforms. Closes **F4**; the
  parity claim in the `certs` comment becomes true by construction rather than by
  inspection.
* **D4 — Prove the starvation before fixing it.** Add a test that manufactures the
  contention the CI runner supplied by accident — a holder that reacquires
  immediately in a tight loop — and show a waiter exhausting its budget on the
  polling implementation and succeeding on the queued one. Closes the gap left by
  **F1** being inferred.
* **D5 — Bound flake 2's writer by the session's own lifetime.** The writer stops
  when `s.done` closes, so "the writer finished" means "every frame was accepted
  until the session legitimately faulted". Closes **F6**, **F7**.
* **D6 — Assert the fault point by arithmetic, not by waiting.** With a consumer
  that never drains, the session must fault after exactly `cap(s.events) +
  controlOverflowCap` frames. That is a deterministic number, so the test stops
  depending on who wins a race. Closes **F7**.
* **D7 — Leave the two budgets alone.** `lockTimeout` stays 5 s and
  `certLockTimeout` stays 10 s: **F3** says the bound is right, and changing it
  would mask **F1** rather than fix it.

### Consequences

* Good: a waiter is queued by the OS, so sustained concurrent writes serialise
  instead of one of them failing. That removes a real user-visible failure (**F2**),
  not just a CI annoyance.
* Good: five implementations become one, and `certs`'s parity comment stops being a
  promise a future edit can quietly break.
* Bad: the Unix acquire cannot be cancelled. `flock` has no timed form on Linux or
  macOS, so on expiry the caller returns while a goroutine stays blocked until the
  wedged holder releases, at which point it unlocks and exits immediately. That
  goroutine holds no lock of ours and cannot corrupt state, but it is a real
  difference from Windows and must be documented where it lives.
* Bad: the Windows acquire gains genuine complexity — an overlapped handle, an
  event, and a cancellation path with its own race (the lock can be granted
  between the timeout and the `CancelIoEx`). That race must be handled by
  re-checking the event and releasing, or the lock leaks.
* Neutral: `fsutil.WithLock`'s signature does not change, so the five production
  call sites are untouched.

### Confirmation

```bash
# the fairness property, and the bound it must not lose
go test ./internal/fsutil/ -run 'WithLock|Acquire|Fair' -count=5
go test ./internal/auth/ -run TestStoreConcurrentCreateTwoHandles -count=20
go test ./internal/certs/ -count=1

# flake 2, deterministically
go test ./internal/provider/acpagent/ -run StalledPump -count=20

# the whole tree, both platforms
make ci-windows
go build ./... && go test ./internal/... -count=1   # WSL
```

Additionally: no new row naming either test appears in `ci-flakes.tsv` for the
runs that follow this change.

## Pros and Cons of the Options

### A — Make the acquire fair and bounded, in one shared primitive (chosen)

* Good, because the kernel queues the waiter, which removes starvation as a class
  rather than making it less likely.
* Good, because it fixes the user-visible failure in **F2**, which no test-side
  change can reach.
* Good, because it collapses five implementations into one and retires **F4**.
* Bad, because Windows needs overlapped I/O and a cancellation race handled
  correctly, and Unix needs an uncancellable goroutine. This is the most
  intricate option by some distance.
* Bad, because it touches an authentication-critical path; a mistake here wedges
  logins rather than a test.

### B — Keep polling; add backoff and jitter

* Good, because it is a few lines and cannot deadlock.
* Good, because randomised jitter genuinely reduces lockstep starvation.
* Bad, because it reduces the probability without removing the mechanism: a
  sufficiently tight holder still starves a waiter, so the flake returns under a
  slower runner and the user-visible failure remains possible.
* Bad, because "less likely" is not a property a test can assert, so **D4** would
  have nothing to verify.

### C — Lower the test's contention

* Good, because it is the smallest possible change and the test's job (proving no
  lost updates) survives with fewer iterations.
* Bad, because it fixes the symptom by no longer asking the question. The
  contention was the point: 40 iterations from two handles is what exposes a
  lost update.
* Bad, because **F2** would be left in the product, undetected and now untested.

### D — Raise the 5 s budget

* Good, because it is one constant.
* Bad, because **F3** establishes the bound is deliberate and protects
  authentication from a wedged holder; raising it trades a real protection for a
  quieter CI.
* Bad, because starvation is unbounded in principle, so no budget is large enough.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Both tests recorded fail-then-pass | `ci-flakes.tsv`, runs `35604693366`, `35461886119` |
| Flake 1's error text and 6.34 s | full log of run `35604693366`; `internal/auth/store_test.go:259`, `:275` |
| Flake 2's error text and 20.00 s | full log of run `35461886119`; `stalledpump_test.go:120` |
| Flake 2 takes 0.01 s locally, 5/5 | `go test ./internal/provider/acpagent/ -run …StalledPump -count=5 -v` |
| Flake 1 not reproduced in 30 local runs | default, `GOMAXPROCS=1`, and 6 concurrent instances |
| Five polling loops, 20 ms retry | table under *What was measured* |
| `lockTimeout = 5s`, `certLockTimeout = 10s` | `internal/auth/filelock.go:10`, `internal/certs/filelock.go:8` |
| `fsutil.WithLock` has 5 production callers | `auth/filelock_windows.go:13`, `providerauth/reconcile.go:335`, `providerauth/transaction.go:138,452,573` |
| Windows primitives exist in the pinned dep | `golang.org/x/sys v0.47.0`: `CreateFile`, `CreateEvent`, `LockFileEx`, `WaitForSingleObject`, `CancelIoEx` |
| `auth`'s Unix copy is identical to `fsutil`'s | `internal/auth/filelock_unix.go:34-49` vs `internal/fsutil/lock_unix.go:34-49` |
| `certs` documents non-delegation and parity | `internal/certs/filelock_windows.go:17-20` |
| Flake 2's frame arithmetic | `stalledpump_test.go:66,72,74,98`; `controlOverflowCap` in `session.go` |
| `deliver` is O(1) on every path | `internal/provider/acpagent/session.go:1414-1455` |
| `io.Pipe` blocks and does not error | Go stdlib `io.Pipe` semantics; `stalledpump_test.go:74` |
| Bounded-ness is already asserted | `TestWithLockTimesOut` in `lock_windows_test.go` and `lock_unix_test.go` |

### Related records

* **MADR 0143** — the flake ledger that surfaced both of these.
* **MADR 0116 D6** — established the Windows `LockFileEx` byte-range idiom this
  record changes the *waiting* strategy of, not the range.
* **MADR 0138 F5** — why the stalled-pump test exists: a blocked client handler
  takes the SDK's connection down with it.
* **MADR 0163** — the 2026-09-21 deviation there is the reason this record quotes
  full CI logs: a truncated read produced a confident wrong diagnosis of a
  sibling flake.

### Open questions for the plan

1. Can the starvation be manufactured deterministically enough to fail on demand on
   a fast local SSD (**D4**)? If not, the plan must say what the test actually
   establishes rather than claiming a reproduction.
2. Does `os.OpenFile` suffice on Windows, or must the handle come from
   `windows.CreateFile` with `FILE_FLAG_OVERLAPPED`? P1 must settle this by
   running it, not by reading documentation.
3. Is there any caller that depends on `WithLock` failing *fast* when the lock is
   held — where queueing would change behaviour rather than improve it?

## Amendment — 2026-09-21: Linux `flock` is eventually-served, not FIFO

Found while executing P1, by running the fairness test on both platforms rather
than on the one where the flake happened.

**What D1 assumed.** That a blocking acquire is queued by the kernel on both
platforms, so a waiter "is granted the lock in turn rather than by luck". That
sentence is true of Windows `LockFileEx` and **not** of Linux `flock(2)`, which
makes no ordering guarantee: a waiter woken on release can be barged by a holder
that immediately re-requests.

**Measured**, same holder shape (2 holders, 100 ms holds, immediate reacquire),
10 trials per row:

| platform | budget | waiter acquired | worst wait |
| --- | --- | --- | --- |
| Windows, queued | 600 ms | 10/10 | **151 ms** |
| Windows, polling | 600 ms | 0/10 | — |
| Linux, queued | 600 ms | 9/10 | 600 ms (one starved) |
| Linux, queued | 3 s | 10/10 | **1.054 s** |
| Linux, queued, 1 holder | 3 s | 10/10 | 552 ms |

**What this does and does not change.**

* It does **not** undermine the fix. Linux still stops polling blind every 20 ms
  and is woken when the lock is released, and with a realistic budget the waiter is
  always served — 10/10 at 3 s. **F1**'s defect (a waiter that can burn its entire
  budget while the lock is free hundreds of times) is addressed on both platforms.
* It does change what may be *claimed*. On Unix the property is "the waiter is
  woken on release and eventually served", not "served in turn". Latency is ~7×
  Windows' for the same contention and far more variable.
* It makes the discrimination proof platform-specific. The fairness test separates
  the queued acquire from the polling one **on Windows** (10/10 vs 0/10). On Linux
  the budget needed for reliability is long enough that the polling implementation
  would sometimes succeed too, so there the test asserts the weaker property.
  That is acceptable because the flake being fixed was on `windows/amd64`.

**Decision (owner, 2026-09-21).** Keep one fairness test with a per-platform
budget — 600 ms on Windows, 3 s on Unix — each documented with the measurement
that produced it, and state the Unix caveat where the code lives. Implementing
our own ordering on top of `flock` was rejected: it is a significant piece of
concurrent code on the path `Validate` takes, and a bug there wedges logins rather
than a test.

This supersedes D1's "queues" wording for Unix. It does not change D2–D7.

## Amendment — 2026-09-21: F7 and F8 were wrong; flake 2 is a real transport failure

Found by reproducing the flake instead of reasoning about it, which is what P4
should have started with.

**What F7 and F8 claimed.** That flake 2 was a test asserting past its own design —
`deliver` faults the session at the overflow cap, nothing keeps the SDK draining
afterwards, and an `io.Pipe` write then blocks — and that **no production defect
was implicated** because `deliver` is O(1) on every path.

**What reproduction showed.** Under `GOMAXPROCS=1` the unmodified test fails with
the exact CI message. Instrumented, three runs:

| run | session faulted | `conn.Done()` closed | writer |
| --- | --- | --- | --- |
| 1 | yes | no | finished — passes |
| 2 | yes | no | finished — passes |
| 3 | yes | **yes** | blocked past 15s — the CI failure |

Run 3 is decisive: the **ACP connection was torn down**. That is the failure
MADR 0138 F5 exists to prevent — one stalled session taking the engine's transport
with it — so flake 2 is an intermittent *product* failure, not only an
over-assertion. The test was right to fail.

**F8 was right about `deliver` and wrong about the conclusion.** `deliver` is
O(1), and `drainOverflow` does release `overflowMu` before blocking
(`session.go:1488` then `:1490-1501`), so neither blocks the SDK's consumer. The
gap is elsewhere: `controlOverflowCap` only acts once `deliver` is *called*, and it
cannot stop the SDK's **reader** outpacing the SDK's single **consumer**
goroutine. With no parallelism the reader fills the SDK's 1024-deep notification
queue, `errNotificationQueueOverflow` fires, and the SDK closes the connection
before our guard ever sees those events.

**Consequences for this record.**

* **F7 is narrowed**: the test's "every frame must be accepted" is indeed stronger
  than the design promises, and that is why the failure surfaces as a hang. But
  fixing the assertion would have hidden a real defect, so it is no longer a
  finding this plan acts on.
* **F8 is withdrawn.** A production defect *is* implicated.
* **D5 and D6 are withdrawn**, and with them **P4** and criteria **A8**/**A9**.
  Bounding the writer and asserting the absorb arithmetic would have made a test
  that tolerates the teardown — going quiet on exactly the regression it was
  written for.
* The transport failure gets its own record, **MADR 0166**, because the fix is a
  design question about the ACP receive path rather than a tidy-up of a lock.

**Decision (owner, 2026-09-21).** Withdraw P4, leave the test failing under load
because it is detecting something real, and open 0166. 0165 keeps what it
achieved: the lock is fair, one implementation serves all five sites, and flake 1
is fixed.

Also recorded, because it cost a wrong commit before it was noticed: the obvious
way to unblock the writer — closing the pipe when the session faults — makes the
SDK see EOF and *causes* the teardown the test forbids. The pipe is the transport
here; closing it is not a test-harness detail.

### Note on the citations above

Three files this record cites no longer exist, because executing it removed them:
`internal/certs/filelock_unix.go` and `internal/certs/filelock_windows.go` were
replaced by `internal/certs/filelock_locked.go` (P3, commit `e184118`), and
`internal/auth/filelock_unix.go`'s acquire loop was deleted (P2, commit `2e7dbda`).
Every citation to them describes the tree as it was when the decision was taken,
which is the point of a decision record — they are not stale references to fix. The
`certs` comment quoted in **F4**, the one promising its retry loop "matches
fsutil's", is gone along with the loop it described.

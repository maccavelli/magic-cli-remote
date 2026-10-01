---
status: completed
date: 2026-09-08
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0153 — Survive a Windows reader holding the destination

Implements [0153-MADR-atomic-writes-lose-a-race-any-windows-reader-can-start.md](0153-MADR-atomic-writes-lose-a-race-any-windows-reader-can-start.md)
decisions D1–D6, closing findings F1–F7.

## Goal

1. `WriteFileAtomic` retries a rename that failed because something held the
   destination, and succeeds once the holder lets go.
2. It gives up after a bounded budget and returns the original error unchanged.
3. POSIX behaviour is identical to today, by construction rather than by test.
4. W3's negative result is recorded where it can be found.

## Scope

### In scope (the only files any phase may touch)

| File | Phase | Why |
| --- | --- | --- |
| `internal/fsutil/rename_windows.go` (new) | P1 | the retryable-error predicate (D2, D3) |
| `internal/fsutil/rename_other.go` (new) | P1 | compile-time `false` off Windows (D2) |
| `internal/fsutil/atomic.go` | P2 | the bounded retry (D1, D4) |
| `internal/fsutil/atomic_test.go` | P2 | retry and give-up tables (D5) |

### Out of scope

* **The other seven steps of `writeFileAtomic`** (MADR option B). Only the
  rename was measured to fail, and the temp file has a unique name nothing else
  holds.
* **`ReplaceFile`** (option C). It fails under the same condition; its ACL
  argument is recorded in the MADR for a future record if the DACL ever matters.
* **Any call site.** All eight keep calling `WriteFileAtomic` unchanged.
* **W3.** Closed by MADR F6, not implemented.

## Stability rule

Every phase ends with:

```bash
GOOS=windows go build ./... && GOOS=linux go build ./... && GOOS=darwin go build ./...
go test ./... && go test -race ./...
gofmt -l $(git diff --name-only HEAD | grep '\.go$')
```

The cross-build matters here for the same reason it did in MADR 0150: a
per-platform predicate is a compile error waiting to happen on the two targets
this host does not run.

One commit per phase. **`git push` needs an explicit instruction in the same
turn** — this plan does not authorise it.

## Cross-cutting contracts

**C1 — POSIX behaviour does not change.** `rename_other.go` returns `false`
unconditionally, so the retry loop runs exactly once there. This is a
compile-time guarantee; no test can weaken it.

**C2 — no host-derived behaviour.** No `runtime.GOOS` in `internal/fsutil`.
The split is build tags (MADR 0144, 0153 D2).

**C3 — a permanent failure still fails.** The final error is the error from the
last attempt, unwrapped and unchanged, so callers and their tests see today's
message.

**C4 — no production call site changes.** The retry is internal to
`writeFileAtomic`; the eight callers are untouched.

**The contract most at risk is C1**, and the tempting mistake is specific:
writing the predicate as a single cross-platform function that string-matches
`"Access is denied"` or compares against `syscall.EACCES`. That compiles
everywhere, looks tidier than two files, and would make POSIX retry real
`EACCES` failures — turning an immediate permission error into a 150 ms one on
the platform that has no bug. The build-tag split is what makes that
unavailable rather than merely discouraged.

## Dependency and delivery order

P1 before P2: the retry cannot compile without the predicate, and the predicate
is meaningless without a caller. P1 lands the two files with the predicate
unused, which the cross-build verifies on all three targets.

## Implementation Steps

### P1 — the per-platform predicate (D2, D3; closes F2, F7)

`rename_windows.go`:

```go
//go:build windows

package fsutil

// retryableRenameErr reports whether err is Windows telling us the
// destination is currently held open, rather than a permanent failure.
//
// ERROR_ACCESS_DENIED is what os.Rename actually returns for this — measured,
// MADR 0153 F2 — not ERROR_SHARING_VIOLATION, which is the name the condition
// goes by. Both are matched: the second is what the documentation describes,
// and a different sharing mode or filesystem may produce it.
//
// The ambiguity is deliberate and costed: ERROR_ACCESS_DENIED is also the
// permanent "you may not write here" error, so a real permission failure will
// exhaust the retry budget before failing (0153 F4).
func retryableRenameErr(err error) bool { ... }
```

`rename_other.go` is `//go:build !windows` and returns `false` with a comment
saying why the whole question is Windows-only: a POSIX rename replaces a
directory entry and existing readers keep their inode, so no holder can block
it.

**Verification.** Three-target cross-build. `go vet ./internal/fsutil/` passes
with the function unused on non-Windows (an unused *function* is not an error;
if it were, that is what the cross-build is for).

### P2 — the bounded retry (D1, D4, D5; closes F1, F3, F5)

In `writeFileAtomic`, replace the single `ops.rename` with a loop: attempt,
and while `retryableRenameErr(err)` and attempts remain, sleep the next backoff
and retry. Budget 10, 20, 40, 80 ms across five attempts. On exhaustion return
`fmt.Errorf("fsutil: rename: %w", err)` with the last error — the same string
callers see today (C3).

The sleep goes through a `fileOps` member so the tables below do not spend real
time.

Tests, using the existing seam (D5):

* rename fails twice with a retryable error, then succeeds → `WriteFileAtomic`
  returns nil, and the injected op was called three times;
* rename always fails with a retryable error → returns the original error, and
  was called exactly five times;
* rename fails once with a **non**-retryable error → returns immediately, called
  exactly once (this is the test that would catch a predicate that returns
  `true` for everything);
* on Windows only, an end-to-end case: hold the destination with a real handle,
  start the write, close the handle, and assert the write succeeded. It proves
  the predicate matches what the OS actually returns, which no injected error
  can.

**Verification.**

```bash
go test ./internal/fsutil/ -run TestRename -count=1 -v
go test ./internal/fsutil/ -count=1
```

The always-fails case must report five calls, not four or six: an off-by-one in
a retry budget is invisible without an exact count.

## Verification (whole plan)

```bash
GOOS=windows go build ./... && GOOS=linux go build ./... && GOOS=darwin go build ./...
go test ./... && go test -race ./...
grep -rn 'runtime.GOOS' internal/fsutil/     # nothing
grep -n 'return false' internal/fsutil/rename_other.go
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1
```

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | All three targets build | D2, F7 |
| A2 | `rename_other.go` returns `false` unconditionally | D2, C1 |
| A3 | No `runtime.GOOS` in `internal/fsutil` | D2, C2 |
| A4 | A retryable failure that clears is survived | D1, F1, F3 |
| A5 | A permanent retryable-looking failure gives up after exactly five attempts and returns the original error | D1, C3, F4 |
| A6 | A non-retryable failure returns after exactly one attempt | D3 |
| A7 | The Windows end-to-end case passes against a real held handle | D3, F2 |
| A8 | No call site changed | C4 |

**A7 is the criterion most likely to be skipped**, because it needs a real
`CreateFile` handle with a restrictive sharing mode and the other tests already
pass with injected errors. It is also the only one that can catch the mistake
this record was written about: keying the predicate on
`ERROR_SHARING_VIOLATION`, which every injected-error test would happily
confirm and the operating system would never produce.

**A6 is the one most likely to pass vacuously**, since a predicate that always
returns `true` fails it — but only if the count is asserted exactly.

## Rollout and Rollback

One function's control flow plus two small files. No protocol, no wire format,
no user-visible surface, no call-site change. Reverting P2 restores the single
rename; reverting P1 as well removes the predicate.

The behaviour change is confined to Windows: a write that previously failed now
takes up to ~150 ms longer and usually succeeds. The risk of the change is
therefore latency on a permanent failure, not correctness.

## Deferred (named, so they are not mistaken for oversights)

* **`ReplaceFile` for ACL preservation** (MADR option C). Its argument survives
  rejection: a replaced file inherits the parent's DACL rather than keeping the
  destination's. It does not matter today because MADR 0116 D4 puts the access
  control on the parent directory. Revisit if a file ever needs its own ACL.
* **Retrying the other filesystem steps** (option B). No evidence any of them
  fails; the temp name is unique.
* **A log line on give-up** (MADR open question 2). The error reaches the
  caller either way; a log would make contention visible rather than merely
  survivable. Left out because the right level and rate-limit are a decision of
  their own.
* **Measuring a real antivirus hold** (open question 1). The budget is derived
  from the failure being transient, not from how long a scanner holds a file.
  If a retry is ever seen exhausting its budget on this host, that measurement
  is the next step.
* **W3's extended-length false negative** (MADR F6). `\\?\C:\...` inside a root
  is reported outside it. It cannot grant access, and no client is known to
  send that form; it would matter only if a workspace root ever needed
  extended-length addressing to exceed `MAX_PATH`.

## Execution record — 2026-09-08

Both phases ran: `cf7f427` (P1), `8d39a6d` (P2).

| # | Result |
| --- | --- |
| A1 | met — `CGO_ENABLED=0` builds for windows/linux/darwin at both phases |
| A2 | met — `rename_other.go` is `func retryableRenameErr(error) bool { return false }` |
| A3 | met — no `runtime.GOOS` in `internal/fsutil` |
| A4 | met — two failures then success, three rename calls |
| A5 | met — exactly five calls, and `errors.Is(err, ERROR_ACCESS_DENIED)` still holds on the returned error |
| A6 | met — a non-retryable error returns after exactly one call |
| A7 | met — the real held-handle case passes, and **fails against the wrong predicate** |
| A8 | met — `git diff` over the four caller packages is empty |

### A7 was worth the trouble, and it is now provable

The plan predicted A7 would be the criterion most likely to be skipped, on the
grounds that it is the only one that can catch a predicate keyed on
`ERROR_SHARING_VIOLATION`. That was tested rather than asserted: the predicate
was temporarily reduced to `errors.Is(err, windows.ERROR_SHARING_VIOLATION)`
and the suite run.

```text
--- FAIL: TestRenameRetriesWhileTheDestinationIsHeld
--- FAIL: TestRenameGivesUpAfterTheBudget          rename called 1 times, want exactly 5
--- FAIL: TestWriteFileAtomicSurvivesARealHeldHandle
        write lost the race against a real reader: ... Access is denied.
```

Three tests fail, and the third fails with the operating system's own error
rather than an injected one. Reverted.

### What the plan predicted incorrectly

**The retry tests could not live in `atomic_test.go`.** The scope table put them
there, which assumed the retry behaves the same everywhere. It cannot: C1 makes
`retryableRenameErr` a compile-time `false` off Windows, so "fails twice then
succeeds" is not a POSIX behaviour to assert — POSIX has exactly one attempt by
construction. Splitting the tests by build tag, mirroring the production files,
was the only shape that tests each platform's actual contract instead of
gating a shared table on `runtime.GOOS` — which C2 forbids in production and
which would be no better in a test.

**The scope table also missed `atomic.go`'s new dependency.** Adding an
injectable `sleep` to `fileOps` was implied by D5 but not listed; it is a new
field on an existing struct and a new `time` import. Every existing test builds
its ops from `realOps()`, so none needed changing — which is the reason that
seam was worth using rather than adding a package-level variable.

### Verification at completion

`gofmt` clean; `GOOS=linux` and `GOOS=darwin` `go vet` pass over the package
including its non-Windows test arm; `go test ./...` and `go test -race ./...`
green; the Windows gate reports `ALL SELECTED CHECKS PASSED`.

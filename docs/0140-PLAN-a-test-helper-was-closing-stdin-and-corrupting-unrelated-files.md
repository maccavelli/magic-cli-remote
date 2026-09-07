---
status: complete
date: 2026-09-04
associated-madr: "0140-MADR-a-test-helper-was-closing-stdin-and-corrupting-unrelated-files.md"
---

# Implement: stop `quietLog` closing stdin, and guard the descriptor

Associated MADR:
[0140-MADR-a-test-helper-was-closing-stdin-and-corrupting-unrelated-files.md](0140-MADR-a-test-helper-was-closing-stdin-and-corrupting-unrelated-files.md)

## Goal

Remove the fd-0 corruption from `internal/daemon`, prove it is gone, and leave a
check that fails on the next helper to do the same.

When this is complete:

* `internal/daemon`'s test suite leaves file descriptor 0 open.
* `go test -race -count=25 -shuffle=on ./internal/daemon/` — the command MADR
  0139's plan abandoned — runs clean, repeatedly.
* Poisoning fd 0 no longer makes `goose.Reconcile` fail.
* MADR 0139's records no longer attribute these failures to the host.

## Scope

### In scope

| item | MADR reference |
| --- | --- |
| `quietLog` writes to `io.Discard` | Decision Outcome 1 |
| A package guard that fd 0 survives the suite | Decision Outcome 2 |
| Correcting MADR 0139's host attribution and restoring its verification | Decision Outcome 3 |

### Out of scope

* **Any production change.** `quietLog` is test-only and no non-test file calls
  `os.NewFile`; this fixes a test helper and nothing else.
* **Auditing other packages for descriptor hygiene.** The scan for
  `os.NewFile(` returns exactly one hit repository-wide, and the other discard
  loggers are already correct. There is nothing else of this shape to fix.
* **Revisiting MADR 0139's decision.** Its fix — naming the failure state — is
  what made this reproduction legible and stands unchanged. Only its
  *attribution* of the EBADF failures is corrected.

## Implementation Steps

One phase. The fix and its guard belong in the same commit: shipping the fix
alone would leave the class open, and shipping the guard alone would leave the
suite red.

### 1.1 Fix the helper

`internal/daemon/credentials_test.go:14-16`:

```go
func quietLog() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
```

`slog.DiscardHandler` rather than `slog.NewTextHandler(io.Discard, …)`: it
discards without formatting, and it is what
`internal/provider/acpagent/stalledpump_test.go` already uses. The `os` import
may become unused in that file — check and remove.

### 1.2 Guard the descriptor for the whole package

`internal/daemon` has a `TestMain` already (`main_test.go`), which is the only
place that sees the suite before and after. Extend it: record whether fd 0 is
open before `m.Run()`, force a collection cycle after it, and fail the package
if a descriptor that was open at the start is closed at the end.

Failing the *package* rather than a test is deliberate. The bug's victims are
arbitrary, so attributing it to any single test would be a guess; what can be
said truthfully is "something in this package closed a standard descriptor".

The check must:

* run `runtime.GC()` after `m.Run()` so pending finalizers have fired — the
  whole failure mode is deferred, and a check that runs before collection sees
  nothing;
* report a non-zero exit distinguishable from an ordinary test failure, with a
  message naming the descriptor and pointing at `os.NewFile`;
* not fire when fd 0 was already closed before the suite started, since that is
  the harness's business and not this package's.

### 1.3 Correct MADR 0139

Its plan's deviation entry attributes the EBADF failures to a host condition and
replaces the stress verification because of it. Add a dated correction pointing
at MADR 0140, and restore the original command in its Verification section now
that it passes. The deviation entry itself stays — what it recorded happening is
accurate, and its conclusion is corrected in place rather than deleted.

## Verification

```bash
make pre-add-check FILES="internal/daemon/credentials_test.go internal/daemon/main_test.go"
for os in windows linux darwin; do GOOS=$os go vet ./internal/... ./cmd/...; done
go test -race ./internal/... ./cmd/... -count=1
go test -race -count=25 -shuffle=on ./internal/daemon/
```

The cross-compile loop is not optional: the guard touches platform-specific
syscall surface, and a single-platform vet is what let a Windows build failure
through on the first attempt.

The last one is the point: it is the command MADR 0139's plan could not use.

### Fail-first evidence required

Each against a scratch copy, never the working tree.

* `L1` — restore `os.NewFile(0, os.DevNull)`; the fd-0 probe must show the
  descriptor invalid after `quietLog()` plus `GC`, and valid with the fix.
* `L2` — restore it and run `-count=25 -shuffle=on`; the package must fail with
  `bad file descriptor`, and pass with the fix. This is the end-to-end claim.
* `L3` — close fd 0 deliberately inside a test; `TestMain`'s guard must fail the
  package. Without this, the guard is a check that has only ever been seen to
  pass.

### Acceptance criteria

1. 50 `quietLog()` calls followed by `runtime.GC()` leave fd 0 valid.
2. Poisoning fd 0 no longer makes `goose.Reconcile` return an error outcome —
   the reproduction in MADR 0140 stops reproducing.
3. `go test -race -count=25 -shuffle=on ./internal/daemon/` passes on at least
   three consecutive runs.
4. The guard fails when fd 0 is closed deliberately (L3).
5. MADR 0139's plan carries the correction and its original verification
   command.

## Rollout and Rollback

**Rollout.** One commit. Test-only: no daemon behaviour changes, nothing to
restart, no release needed for correctness. The next CI run exercises it.

**Compatibility.** None to consider — `quietLog` is unexported and test-only,
and its contract ("a logger that produces no output") is unchanged.

**Rollback.** Revert the commit. That reinstates a known descriptor-corrupting
bug, so it should only be done if the guard itself proves unstable — in which
case the guard alone should be reverted and the one-line fix kept.

## Execution Record

### 2026-09-04 — complete

**1.1** `quietLog` returns `slog.New(slog.DiscardHandler)`. The comment at the
site explains what the old line did and why, because the next reader will
otherwise see a one-line change with no visible motivation.

**1.2** `TestMain` records whether fd 0 is open before `m.Run()`, forces two
collection cycles after it, and fails the package if a descriptor that was open
at the start is closed at the end. It fails the *package*, not a test: the
victims are arbitrary, so blaming any single test would be a guess, while
"something in this package closed a standard descriptor" is true.

The two `runtime.GC()` calls are load-bearing. The failure is entirely deferred
— nothing is closed until a wrapper is collected — so a check that runs before
collection sees a healthy process.

**1.3** MADR 0139's plan and record are corrected where they attribute these
failures to the host, and its verification command is restored to
`-count=25 -shuffle=on`. The deviation entry itself is kept: what it recorded
happening is accurate, and the correction sits beneath it rather than replacing
it.

### Fail-first evidence — three breakages

```text
L1. os.NewFile(0, os.DevNull) restored
    FAIL TestL1Probe
         fd 0 was closed by quietLog finalizers

L2. the same, under the stress command this plan restores
    FAIL TestReconcileGooseKeyringSwitches
         write …/.config/goose/config.yaml: bad file descriptor
    FAIL TestCredentialGuardEscalatesABrokenCredential
         fsutil: write temp: write …/provider-auth/codex/.manifest.json-…: bad file descriptor
    FAIL TestGuardWatcherFailureIsNotFatal
         TempDir RemoveAll cleanup: unlinkat …: bad file descriptor
    FAIL TestReconcileReportsAnUnwritableConfigDirectory
         TempDir RemoveAll cleanup: unlinkat …: bad file descriptor

L3. fd 0 closed deliberately inside a test
    FAIL (package) daemon tests: file descriptor 0 was open before this package
         ran and is closed now. … See MADR 0140.
```

**L2 is the whole argument.** With one line reverted, four unrelated tests fail
on four different syscalls — including `TestReconcileGooseKeyringSwitches`, the
test whose failure in CI run 33871538458 started this. With the line fixed, the
same command passes three times consecutively.

L3 matters separately: it is the only thing that shows the guard can fail. A
guard for a defect that no longer exists is otherwise indistinguishable from a
guard that does nothing.

### Acceptance

| # | criterion | result |
| --- | --- | --- |
| 1 | 100 `quietLog()` + GC leave fd 0 valid | **fd 0 still valid** (was: `INVALID: bad file descriptor`) |
| 2 | fd-0 poisoning no longer breaks `goose.Reconcile` | **40 of 40 returned `switch`** under continuous GC pressure |
| 3 | `-count=25 -shuffle=on` passes three times | **58.3s, 45.5s, 50.3s — all ok** |
| 4 | the guard fails when fd 0 is closed | L3 above |
| 5 | 0139 carries the correction and its original command | done |

```text
go test ./internal/daemon/ -count=1                     -> ok
go test -race ./internal/... ./cmd/... -count=1         -> ok (full tree)
go test -race -count=25 -shuffle=on ./internal/daemon/  -> ok ×3
```

### What this cost before it was found

Recorded because the cost is the argument for the guard, not the fix:

* One red release build (CI run 33871538458, tag `v0.16.4`).
* One full investigation (MADR 0139) that could not identify its own trigger.
* One verification command weakened to work around it, in that same record.
* An unknown number of earlier dismissals — the failure has been visible under
  stress for as long as anyone has stressed this package, and every encounter
  attributed it to the environment, including mine.

The line was introduced as a way to silence a logger. It is the kind of defect
that is obvious once seen and invisible until then, which is exactly what the
package guard is for.

## Plan complete

All three steps executed, with the fail-first evidence above.

### Deviation — 2026-09-04: the guard was not portable, and CI caught it

*What was found.* The guard as first written used `syscall.Fstat(0, &st)` with
`syscall.Stat_t`. That type does not exist on Windows, and this repository runs
a Windows job. CI run **33893250444** failed:

```text
vet.exe: internal\daemon\main_test.go:84:17: undefined: syscall.Stat_t
```

Every other job in that run passed, including `Go (test; build on tag)` — the
fix and the Unix behaviour were correct; only the guard's portability was not.

*Why it was not caught locally.* Every verification in this plan ran on macOS.
`go vet ./...` on one platform says nothing about the others, and nothing in the
plan's Verification section cross-compiled.

*Resolution.* The guard now uses `os.Stdin.Stat()` plus `os.SameFile`, which is
portable — and **stronger than what it replaced**:

| state | `syscall.Fstat(0)` | `os.Stdin.Stat()` + `SameFile` |
| --- | --- | --- |
| fd 0 closed | detected | detected (`stat /dev/stdin: bad file descriptor`) |
| fd 0 closed **and reused by another file** | **missed** — fstat succeeds | detected (`SameFile = false`) |

The recycled case is the one that matters: it is the state in which victims
actually fail, and the original check would have passed straight through it.
Verified by probe before adopting, and pinned by a second fail-first:

```text
L3. fd 0 closed deliberately            -> package FAILS
L4. fd 0 closed and recycled by a file  -> package FAILS
```

*Verification section corrected.* Cross-compilation is added, because
single-platform vet is what let this through:

```bash
for os in windows linux darwin; do GOOS=$os go vet ./internal/... ./cmd/...; done
```

All three clean. `go test -race ./internal/... ./cmd/...` and
`go test -race -count=25 -shuffle=on ./internal/daemon/` (50.9s) both pass.


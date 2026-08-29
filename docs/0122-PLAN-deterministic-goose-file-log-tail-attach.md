---
status: in-progress
date: 2026-08-28
associated-madr: "0122-MADR-deterministic-goose-file-log-tail-attach.md"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0122 — Deterministic goose file-log tail attach

Implements [0122-MADR-deterministic-goose-file-log-tail-attach.md](0122-MADR-deterministic-goose-file-log-tail-attach.md)
decisions D1–D6, closing findings F1–F5.

## Goal

`TestTailGooseFileLogsSurfacesQuota` observes attach, then writes, then
asserts quota — on every OS, without a sleep standing in for the tailer.

Finish line:

* the 350 ms attach sleep is gone;
* the tailer fires a nil `Provider` seam after seek-to-EOF;
* the test waits on that seam, `Sync`s the append, and keeps the
  `TypeError` / `quota` assertions;
* `go test -count=50 -run TestTailGooseFileLogsSurfacesQuota ./internal/provider/acphttp/`
  is green on this host.

## Scope

### In scope (the only files any phase may touch)

* `internal/provider/acphttp/provider.go` — the `gooseTailAttached` field
* `internal/provider/acphttp/engine_log_tail.go` — fire it after EOF seek
* `internal/provider/acphttp/engine_log_tail_test.go` — D2/D3 rewrite
* `docs/0122-*` — this pair's status and execution record

### Out of scope

* `.github/workflows/ci.yml` (D6)
* 0119's codex tests
* Changing the 250 ms ticker, switching to fsnotify, or injecting a clock
* A package-wide poll-and-sleep sweep (MADR open question 3)
* Production `Sync` of goose's own log writes (open question 2)

## Stability rule

Every phase ends with:

```bash
gofmt -l internal/provider/acphttp/provider.go \
         internal/provider/acphttp/engine_log_tail.go \
         internal/provider/acphttp/engine_log_tail_test.go
go vet ./internal/provider/acphttp/
go test -count=20 ./internal/provider/acphttp/
```

then **one commit** (`git commit --no-edit`; never `-m`).

`gofmt -l cmd internal` is not usable on this host (CRLF working tree;
0118 deferral). Scope the format check to the files this phase touched.

**`git push` is not required by P1 and must not be assumed.** P2 needs a
push to observe Windows CI and therefore an explicit instruction in the
same turn (AGENTS.md).

## Cross-cutting contracts

**C1 — Seam is inert when nil.** No call through a nil func; no ticker
change; no behaviour change for `startEngineFileLogTail`.

**C2 — No test is weakened to pass.** No skip, no `runtime.GOOS` gate, no
retry, no loosened `ErrorKind`, no `time.Sleep` as attach. MADR D4.

**C3 — The sleep is replaced, not supplemented.** After P1, grep of
`engine_log_tail_test.go` for `Sleep` in `TestTailGooseFileLogsSurfacesQuota`
is empty.

**C4 — The stale-line contract stays.** Pre-attach content must still not
fire. The existing pre-write of a stale quota line remains; the seam is
what makes "pre-attach" a real state rather than a hoped-for one.

## Dependency and delivery order

P1 is the whole repair. P2 observes it on the lane that failed, and is
gated on a push.

## Implementation Steps

### P1 — Attach seam and rewrite the test (D1–D5; closes F1–F4)

`provider.go`, `engine_log_tail.go`, `engine_log_tail_test.go`.

On `Provider`, add:

```go
// gooseTailAttached is a test seam fired after the file tailer seeks to
// EOF on a newly discovered log. Nil in production (MADR 0122 D1/D5).
gooseTailAttached func(path string, offset int64)
```

In `tailGooseFileLogs`, after `offset = fi.Size()` in the
`path != curPath` branch, if the field is non-nil, call it with that
path and offset. Then `continue` as today.

In `TestTailGooseFileLogsSurfacesQuota`:

1. Install the seam to close a channel the first time it sees `logPath`.
   `t.Cleanup` nils the field.
2. Start `tailGooseFileLogs`.
3. Wait on the channel with a timeout; failure names attach, not quota.
4. Delete the 350 ms sleep.
5. Append the goose 429 line, `Sync`, close.
6. Keep `recvType` + `ErrorKind == "quota"` + natural-language checks.
7. Keep the stale line written *before* the tailer starts (C4).

Do not change `recvType`'s 2 s deadline. After attach, one 250 ms tick
is the expected wait; two seconds is slack, not the barrier.

**Verification:**

```bash
gofmt -l internal/provider/acphttp/provider.go \
         internal/provider/acphttp/engine_log_tail.go \
         internal/provider/acphttp/engine_log_tail_test.go
go vet ./internal/provider/acphttp/
go test -count=50 -run TestTailGooseFileLogsSurfacesQuota ./internal/provider/acphttp/
go test -count=20 ./internal/provider/acphttp/
grep -n 'Sleep' internal/provider/acphttp/engine_log_tail_test.go
# → no hit inside TestTailGooseFileLogsSurfacesQuota
```

### P2 — Observe it on windows/amd64 (D6; closes F1 on the lane)

**Needs an explicit push instruction in the same turn. Without one this
phase does not run, and the plan closes at P1 with P2 marked pending.**

After the P1 commit is on `master`, confirm from the Windows job log:

```text
TestTailGooseFileLogsSurfacesQuota → run, pass, not skipped
Go (windows/amd64)                 → green, or red for a different test
```

A red Windows lane on a *different* test is a new record, not a widening
of this one.

## Verification (whole plan)

```bash
gofmt -l internal/provider/acphttp/provider.go \
         internal/provider/acphttp/engine_log_tail.go \
         internal/provider/acphttp/engine_log_tail_test.go
go vet ./internal/provider/acphttp/
go test -count=50 -run TestTailGooseFileLogsSurfacesQuota ./internal/provider/acphttp/
go test -count=20 ./internal/provider/acphttp/
```

```text
TestTailGooseFileLogsSurfacesQuota → no attach Sleep
gooseTailAttached                  → nil-checked, production unset
```

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | Seam fires after EOF seek on new path, not on every poll | D1 |
| A2 | Test waits for attach, then writes, then asserts quota | D2 |
| A3 | Append is `Sync`ed | D3 |
| A4 | No skip / GOOS gate / retry / attach-sleep | D4, C2, C3 |
| A5 | Stale pre-attach line still present in the test | C4 |
| A6 | Production path unchanged when seam is nil | D5, C1 |
| A7 | Windows lane observation recorded, or P2 pending for want of a push | D6 |

A4 is the one to guard. A 2 s attach wait is a timeout on a missing
signal; putting `time.Sleep(350 * time.Millisecond)` back "for safety"
would reopen F2.

## Rollout and Rollback

The seam is nil in production; users do not observe P1. `git revert` of
the P1 commit restores the sleep and the flake.

## Deferred

* **R1 vs R2.** MADR open question 1. Not needed for the repair.
* **Package-wide poll-and-sleep sweep.** Open question 3; own record.
* **`go test -count=1` on native lanes.** Still 0119's deferral.
* **The CRLF working tree.** Still 0118's.

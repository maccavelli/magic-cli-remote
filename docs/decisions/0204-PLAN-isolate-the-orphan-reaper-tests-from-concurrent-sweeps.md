---
status: in-progress
date: 2026-10-20
associated-madr: "0204-MADR-isolate-the-orphan-reaper-tests-from-concurrent-sweeps.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement isolating the orphan-reaper tests from other packages' startup sweeps

Associated MADR: [0204-MADR-isolate-the-orphan-reaper-tests-from-concurrent-sweeps.md](0204-MADR-isolate-the-orphan-reaper-tests-from-concurrent-sweeps.md)

**Approved by the owner on 2026-10-20** ("Approve 0204"). P1 starts once this PLAN is committed.
P2 starts only if P1 reproduces the failure.

## Goal

The reaper tests in `internal/procutil` pass however many daemons other packages start alongside
them, with their assertions unchanged.

## Scope

* `internal/procutil/reap.go`, and a new `internal/procutil/export_test.go`.
* `internal/procutil/reap_test.go`.
* Nothing in `internal/daemon` or any other package.

## Implementation Steps

### P1 — Reproduce first

On the Linux test host, from a scratch clone at `HEAD`:

1. Build the `procutil` test binary.
2. Run a sweeper beside it: a small Go program in the scratch clone that calls
   `procutil.ReapOrphanEngines(nil)` in a loop, every 20 ms.
3. Run `TestReapOrphanEnginesKillsOrphan` 50 times with `-count=1`. Record how many fail with
   "sweep reaped 0 processes".
4. If none fails, stop. Amend the MADR's cause before any change.

### P2 — The keyed sweep (MADR option A)

1. `FindOrphanEngines` and the env-marker half of `ReapOrphanEngines` move into unexported
   functions that take the engine-ID key and the owner key. The exported functions call them with
   `EnvEngineID` and `EnvEngineOwner`. The registry sweep is unchanged.
2. `export_test.go` exports, for the tests only, the keyed functions and a key pair unique to the
   test process, `MCREMOTE_ENGINE_ID_TEST_<pid>` and `MCREMOTE_ENGINE_OWNER_TEST_<pid>`.
3. `spawnMarked` stamps the test keys. The four tests that spawn marked stand-ins sweep with them.
   `TestFindOrphanEnginesTreatsMissingOwnerAsOrphan`'s retry loop stays as it is.
4. A new test pins the production keys: the exported functions sweep with `EnvEngineID` and
   `EnvEngineOwner`, through a seam that records the keys rather than spawning a stand-in.

## Verification

* **The fix:** P1's reproduction, run again against the changed tree, fails no run in 50.
* **The key pin,** seen failing first: on a scratch copy, the exported `FindOrphanEngines` passing
  the test key fails the new test.
* **The isolation,** seen failing first: on a scratch copy, `spawnMarked` stamping `EnvEngineID`
  fails P1's reproduction again.
* `make preflight`; `go test -race ./internal/procutil/...` on the Linux test host; the full CI run
  after the push.

## Rollout and Rollback

Test code and one internal parameter, so no release is needed. Rollback is reverting the commit.
The flake ledger shows whether the test flakes again.

## Execution record

### P1 and P2, 2026-10-20 — staged for the owner's commit; the CI run after the push remains

**Approval.** This PLAN was committed as `d6c5c349`. S10a of 0054 came first, as `34fb3d0d`. The
owner then said "proceed".

**P1, the reproduction,** on the Linux test host, in a scratch copy of the tree at `34fb3d0d`
(`r0204/drive_p1.py`, `r0204/remote_p1.py`). The sweeper is a scratch `main` package, never in the
tree, that calls the production `procutil.ReapOrphanEngines` every 20 ms, as a daemon test in another
package does once at start:

```text
go version go1.27.2 linux/amd64
control, no sweeper: 0 of 50 runs failed
beside a sweeper every 20 ms: 50 of 50 runs failed
  50x reap_test.go:134: sweep reaped 0 processes, want at least the orphan
```

The message is CI's, line for line. The MADR's cause holds, so P2 went ahead.

**P2, what changed:**

- **`internal/procutil/reap.go`.**
  - The env-marker sweep is keyed by a `markerKeys` pair. `FindOrphanEngines` calls
    `findOrphanEngines(engineMarkers)`; `ReapOrphanEngines` keeps its registry sweep and calls
    `reapMarkedOrphans(log, engineMarkers)`. `engineMarkers` is `EnvEngineID` and `EnvEngineOwner`.
  - Three seams, `findByEnv`, `processEnv` and `terminateOrphan`, default to what the sweep called
    before: `FindByEnv`, `ProcessEnv`, and `os.FindProcess` with `TerminateProcessGroup`.
- **`internal/procutil/export_test.go`,** new. It defines `TestEnvEngineID` and `TestEnvEngineOwner`,
  the production names with `_TEST_<pid>` appended, and `FindTestOrphans`, `ReapTestOrphans` and
  `StubSweep`.
- **`internal/procutil/reap_test.go`.**
  - `spawnMarked` strips both key pairs and stamps the test pair. Every sweep in the tests uses
    `FindTestOrphans` or `ReapTestOrphans`.
  - `TestFindOrphanEnginesTreatsMissingOwnerAsOrphan`'s retry loop is unchanged.
  - The new `TestProductionSweepsUseTheEngineMarkers` stubs the three seams. It checks that
    `FindOrphanEngines` reports the engine ID and owner read from `EnvEngineID` and
    `EnvEngineOwner`, that `ReapOrphanEngines` terminates the stubbed pid once, and that both
    scanned for `EnvEngineID`. Nothing is spawned or signalled.

**Verification,** on the Linux test host, each run in its own copy (`r0204/verify_p2.py`):

```text
#### p2-fixed
control, no sweeper: 0 of 50 runs failed
beside a sweeper every 20 ms: 0 of 50 runs failed
  --- PASS: TestFindOrphanEnginesDetectsDeadOwner (0.21s)
  --- PASS: TestFindOrphanEnginesSkipsLiveOwner (0.51s)
  --- PASS: TestFindOrphanEnginesIgnoresUnmarkedProcesses (0.51s)
  --- PASS: TestReapOrphanEnginesKillsOrphan (0.23s)
  --- PASS: TestFindOrphanEnginesTreatsMissingOwnerAsOrphan (0.21s)
  --- PASS: TestProductionSweepsUseTheEngineMarkers (0.00s)
  ok      github.com/maccavelli/magic-cli-remote/internal/procutil    6.648s
package -race beside the sweeper: exit 0
#### p2-plant-plan-stamp
control, no sweeper: 10 of 10 runs failed
  10x reap_test.go:138: sweep reaped 0 processes, want at least the orphan
beside a sweeper every 20 ms: 10 of 10 runs failed
  10x reap_test.go:138: sweep reaped 0 processes, want at least the orphan
#### p2-plant-same-keys
control, no sweeper: 0 of 50 runs failed
beside a sweeper every 20 ms: 50 of 50 runs failed
  50x reap_test.go:138: sweep reaped 0 processes, want at least the orphan
#### p2-plant-pin
      reap_test.go:192: FindOrphanEngines = [pid=4242 engine= owner=], want pid 4242, engine-pinned, owner 999999999:1
  --- FAIL: TestProductionSweepsUseTheEngineMarkers (0.00s)
package -race beside the sweeper: exit 1
```

- **The fix:** no failure in 50 beside the sweeper, and the whole package passes under `-race`
  beside it.
- **The PLAN's isolation plant,** `spawnMarked` stamping `EnvEngineID`: it fails, but alone as well
  as beside the sweeper, because the keyed sweep then finds no stand-in. So it shows that the stamp
  and the sweep must agree, not the isolation itself.
- **A sharper plant, added for that reason,** the test keys set to the production keys: it gives the
  pre-fix result exactly, 0 failures alone and 50 of 50 beside the sweeper. That is the isolation
  seen failing.
- **The key pin:** the exported `FindOrphanEngines` scanning for another key fails the new test.
- **Local:** `make pre-add-check` on the three files, "3 file(s) clean (gofmt, golint,
  govulncheck)"; `go vet` for `linux/amd64` and `windows/amd64` on the package, clean; and
  `make preflight`, "✅ preflight passed", with no `FAIL` line.

**Still to do:** the full CI run after the owner's push. The PLAN is `complete` once it passes.

**Files for this commit:**

- `internal/procutil/reap.go`;
- `internal/procutil/export_test.go`;
- `internal/procutil/reap_test.go`;
- this PLAN and its MADR.

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

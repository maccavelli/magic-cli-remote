---
status: accepted
date: 2026-10-20
decision-makers: Project Owner
consulted: "CI run 39481886172, job 118832253122 (Go, linux/arm64); ci-flakes.tsv; internal/procutil and internal/daemon at 8385b133"
informed: anyone who runs `go test ./...` on Linux, and the flake ledger's readers
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Isolate the orphan-reaper tests from other packages' startup sweeps

## Context and Problem Statement

`TestReapOrphanEnginesKillsOrphan` failed once in CI and passed on the retry. The test asks the
orphan reaper to stop a stand-in engine it spawned. But every daemon that any other test package
starts sweeps the same account for orphans when it starts, and `go test ./...` runs packages at the
same time. The likely cause is that another package's sweep stops the stand-in first. The question
is how to keep the reaper tests' processes out of every other test's sweep, without weakening what
they assert.

### Facts [observed 2026-10-20, read-only]

* **The failure.** Run 39481886172, job `Go (linux/arm64)`, on `8802be5e`:

  ```text
  --- FAIL: TestReapOrphanEnginesKillsOrphan (0.20s)
      reap_test.go:134: sweep reaped 0 processes, want at least the orphan
  ```

  The in-job retry passed it. The ledger row is `ci-flakes.tsv:19`, the test's first.
* **What the test does** (`internal/procutil/reap_test.go:125-141`). `spawnMarked` starts
  `/bin/sh` with `MCREMOTE_ENGINE_ID` and an owner token, `999999999:1`, for a daemon that cannot
  exist, then sleeps 200 ms (`:18-46`). The test then calls `ReapOrphanEngines(nil)` once and wants
  at least one process reaped. 0.20 s is the spawn's sleep alone, so the sweep found nothing.
* **What a sweep matches** (`internal/procutil/reap.go:54-73`, `owner_linux.go:109-129`). Every
  process the account can read whose environment has the key `MCREMOTE_ENGINE_ID`, exactly, and
  whose owner token is empty or names a dead process. Nothing ties it to the process that sweeps.
* **Who sweeps.** `daemon.Run` sweeps at every start, before any engine of its own
  (`internal/daemon/daemon.go:305-317`). Tests start real daemons, for example
  `internal/daemon/shutdown_unix_test.go:65` and `notify_unix_test.go:65`.
* **When.** `go test ./...` runs package test binaries in parallel, up to `GOMAXPROCS` at a time.
  So a daemon test in another package can sweep while the reaper test's stand-in is alive. Its
  owner token is dead to every sweep, so every sweep stops it.
* **The siblings share the exposure.** `TestFindOrphanEnginesDetectsDeadOwner` (`:58-69`) and
  `TestFindOrphanEnginesTreatsMissingOwnerAsOrphan` (`:145-161`) spawn stand-ins every sweep
  matches. The second's retry loop, added for "scheduler delays on a busy host", cannot help when
  the process has been stopped.

### Not yet established

The cross-package sweep is inferred from the code. It has not been reproduced. The PLAN's first
step reproduces it on the Linux test host before anything changes. If it does not reproduce, this
record is amended before any fix.

**Established 2026-10-20** by PLAN P1: alone, the test failed in 0 of 50 runs; beside a process that
ran the production sweep every 20 ms, in 50 of 50, each with CI's message, "sweep reaped 0
processes, want at least the orphan".

## Decision Drivers

* The reaper tests keep asserting what they assert now: the count, and that the orphan is gone.
* Production sweeps keep matching exactly what they match now.
* A new test that starts a daemon, in any package, must not be able to reintroduce the flake.
* No new test-only behaviour in the daemon.

## Considered Options

* **A** — The reaper tests stamp a marker key unique to their own test process
* **B** — Daemons started by tests skip the startup sweep
* **C** — The test accepts a stand-in someone else reaped
* **D** — Serialize the packages

## Decision Outcome

Chosen option: "A", because it is the only one that removes the shared state: no other process
sweeps for the key the reaper tests use, and production sweeps are unchanged. Accepted by the owner
on 2026-10-20 ("Approve 0204"), on the condition the record already states: the cause is
reproduced first.

### Consequences

* Good, because each reaper test sweeps only its own stand-ins, whatever else runs.
* Good, because the assertions are unchanged.
* Neutral, because `procutil` gains an unexported sweep that takes the marker keys. The exported
  functions call it with `EnvEngineID` and `EnvEngineOwner`, as now.
* Bad, because the reaper tests no longer use the literal production key. One test must pin that
  the exported functions pass `EnvEngineID` and `EnvEngineOwner`.

### Confirmation

* The PLAN's reproduction, a concurrent sweeper beside the reaper test, fails the test before the
  change and passes it after.
* The test that pins the production keys fails on a scratch copy with the keys swapped.

## Pros and Cons of the Options

### A — A marker key unique to the test process

The sweep is parameterised by its two keys. An `export_test.go` gives the tests a sweep keyed on,
for example, `MCREMOTE_ENGINE_ID_TEST_<pid>`, and `spawnMarked` stamps that key.

* Good, because the isolation does not depend on timing or on what other packages do.
* Good, because production code changes only by one internal parameter.
* Bad, because the production key's use needs its own small test.

### B — Test daemons skip the startup sweep

`daemon.Options` gains a switch, and every test that starts a daemon sets it.

* Good, because the reaper tests are unchanged.
* Bad, because every present and future caller has to remember the switch, and missing it brings
  the flake back without warning.
* Bad, because the daemon gains test-only behaviour.

### C — Accept a stand-in someone else reaped

The test passes when the count is zero but the stand-in is gone.

* Bad, because it stops asserting the reaper's return value, which is the loosened assertion this
  repository's rules exclude.

### D — Serialize the packages

`go test -p 1`, or a cross-process lock around the sweeps.

* Bad, because it slows every run, and a developer's plain `go test ./...` keeps the flake.
* Bad, because a lock adds a test-only path to production sweeps.

## More Information

* The reaper: [0019-MADR-opencode-process-management-plan.md](0019-MADR-opencode-process-management-plan.md), §5.4.
* The retry and its ledger: [0143-MADR-ci-retries-once-and-records-every-retry.md](0143-MADR-ci-retries-once-and-records-every-retry.md).
* Earlier flake records: [0165-MADR-ci-flakes-are-a-starving-lock-and-an-overreaching-assertion.md](0165-MADR-ci-flakes-are-a-starving-lock-and-an-overreaching-assertion.md), [0173-MADR-fix-two-ci-flakes-hub-lock-close-and-leaked-transcript-saves.md](0173-MADR-fix-two-ci-flakes-hub-lock-close-and-leaked-transcript-saves.md).
* The PLAN: [0204-PLAN-isolate-the-orphan-reaper-tests-from-concurrent-sweeps.md](0204-PLAN-isolate-the-orphan-reaper-tests-from-concurrent-sweeps.md).

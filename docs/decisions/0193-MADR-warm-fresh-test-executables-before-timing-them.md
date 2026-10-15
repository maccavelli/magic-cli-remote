---
status: accepted
date: 2026-10-15
decision-makers: Project Owner
consulted: none
informed: contributors who run the Go suite on macOS
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Warm a test's fresh executables before timing them, because macOS holds their first run

## Context and Problem Statement

Two prewarm tests in `internal/provider/acpagent/prewarm_rearm_test.go` fail intermittently on the
macOS development host with `wanted 1 "prewarm failed", got 0`:

* `TestSpareIsRearmedAtTurnEndNotTurnStart` (`:87`, failing at `:114`);
* `TestAFailedTurnStillRearmsTheSpare` (`:130`, failing at `:141`).

The failures are recorded in
[0189-PLAN-carry-the-agent-session-cost-as-a-session-total.md](0189-PLAN-carry-the-agent-session-cost-as-a-session-total.md)
and
[0192-PLAN-show-an-agents-identified-plans-on-the-phone.md](0192-PLAN-show-an-agents-identified-plans-on-the-phone.md)
(P3), each time put down to load. A third test,
`internal/providerauth/ownedflow_test.go`'s `TestOwnedFlowStartsWithAnEmptyHome`, failed once on
2026-10-14 with `fakelogin.sh printed no device code within 10s`.

On 2026-10-14 the owner asked for both flakes to be investigated together, and for every assertion
to be proven. This record states what was measured, how, and what was not proven.

### What the tests do

Each test writes a new `#!/bin/sh` stub into a fresh `t.TempDir()`, hands it to the code under test
as an agent or CLI binary, and waits a bounded time for the effect of running it:

* The prewarm tests point `Config.Bin` at a stub that runs `exit 0` (`rearmProvider`, `:52`).
  `EnsureWarm` (`acpagent.go:680`) spawns it (`:708`). The stub exits, `conn.Initialize`
  (`:544`) fails, and the goroutine logs "prewarm failed", which the test counts.
  * `waitForCount` (`:63`) waits 3 s for the line (`:65`).
  * `TestSpareIsRearmedAtTurnEndNotTurnStart` also asserts that **no** line appears in the 250 ms
    after the turn starts (`:107-111`). That window is the test's only check that the spare is not
    armed while the user's turn runs, which is the regression it was written for
    ([0137-PLAN-prompt-to-first-token-latency-regression.md](0137-PLAN-prompt-to-first-token-latency-regression.md),
    breakage O).
* `TestOwnedFlowStartsWithAnEmptyHome` passes a 10 s `ScanTimeout` to `StartOwnedFlow`. That reaches
  `StartCLIDeviceFlow` (`providerauth/cli.go:161`), which waits that long for the stub to print a
  device code.

### What was measured

All measurements were taken on 2026-10-14 on the macOS development host (10 cores), with scripts in
the session scratch directory, and never in the working tree.

**E1. The time is spent before the stub has run, not in Magic.** The prewarm path was instrumented
on a scratch copy.

* The turn ends, `EnsureWarm` is called, and its goroutine starts, all in the same millisecond.
* Resolving the binary takes about 15 µs. `cmd.Start()` returns in 2–6 ms.
* `conn.Initialize` then returns `peer disconnected before response` the moment the stub's pipe
  closes. The kill and `cmd.Wait` that follow take under 0.1 ms.
* So the whole wait is the stub process starting and exiting. That took 165 ms in the fastest
  runs, 0.44–2.48 s in ordinary suite runs, and 9.45 s and 4.62 s in a run that also rebuilt with
  the race detector.

**E2. The first run of a newly written executable is slow, and only the first.** Fifteen fresh
stubs, idle host:

| Run | Median | Max |
| --- | --- | --- |
| first run of a new stub, executed | 149 ms | 168 ms |
| second run of the same stub | 16 ms | 19 ms |
| `/bin/sh <new stub>`, the script read as data | 13 ms | 21 ms |
| first run of a fresh copy of an already-run stub | 146 ms | 797 ms |
| `/bin/sh -c 'exit 0'` | 10 ms | 10 ms |

The shell itself is not slow: the cost is executing a file the system has not seen, once per file.
Single first runs of 1,449 ms and 797 ms occurred on an otherwise idle host. New files carry a
`com.apple.provenance` extended attribute.

**E3. Gatekeeper scans the file on its first run.** The unified log was read for the window around
one first run (1,287 ms) and one second run (12 ms) of the same stub.

* For the first run, `syspolicyd` logs `GK performScan` and `GK evaluateScanResult`, finds and
  creates provenance data, and puts the executable "into provenance". `XprotectService` scans it.
* Those entries are absent for the second run. The 209 security lines in that window name other
  files.

**E4. Scans queue behind each other.** K new stubs were run at once, then the same K again:

| K | First runs: median | First runs: max | Same files again: median | Same files again: max |
| --- | --- | --- | --- | --- |
| 1 | 178 ms | 178 ms | 14 ms | 14 ms |
| 5 | 435 ms | 724 ms | 31 ms | 39 ms |
| 20 | 1,694 ms | 2,839 ms | 115 ms | 188 ms |
| 40 | 2,965 ms | 5,569 ms | 216 ms | 392 ms |

**E5. The hold falls after `Start()` returns.** A Go program timed `exec.Cmd.Start()` separately from
the process finishing, under load:

| File | Run | `Start()` | Then until exit |
| --- | --- | --- | --- |
| shell script | first | 1.2 ms | 11.0 s |
| shell script | second | 1.7 ms | 13 ms |
| native Go binary | first | 1.3 ms | 9.0 s |
| native Go binary | second | 1.3 ms | 9 ms |

So every bound around a run includes the hold: a context created before `Start` (as with
`procutil.Command`, which wraps `exec.CommandContext`, `procutil_unix.go:17-18`), and a timer started
after it (`ScanTimeout`). Native binaries are held as well as scripts.

**E6. The test suite fills the queue itself.**

* `go test` links a new test binary for each package on every run, even when the build is cached.
  There are 47 packages.
* A race-built test binary is 14 MB. The first run of a fresh copy took 1.34–3.23 s against
  1.02–1.04 s for a second run. That makes each binary a bigger scan than a 150 ms stub.
* Those binaries share the queue with the tests' stubs. Other work on the host does too: a second
  session's test run was observed doing the same.

**E7. The failures reproduce on demand, and only under queue load.** A load generator ran W workers,
each writing and running new scripts in a loop, while the stub-writing packages ran with `-race`.
The same packages without the generator are the control.

| Run | Result |
| --- | --- |
| six stub packages, `-count=3`, no load | all pass |
| the same, 12 workers | both prewarm tests failed 2 of 3, with the reported messages |
| `codex` and `providerauth`, `-count=3`, 40 workers | `TestNonBlockingProbeRunsNoBinaryOnAColdCache` failed 3 of 3 ("the background refresh never ran"), `TestNonBlockingProbeCoalescesConcurrentCallers` 2 of 2 ("no probe ran at all"); `providerauth` passed |
| full `go test -race ./...`, 12 workers, twice | run 1: `TestSpareIsRearmedAtTurnEndNotTurnStart` and `TestNonBlockingProbeCoalescesConcurrentCallers`; run 2: `TestAFailedTurnStillRearmsTheSpare` |

The two `codex` failures had not been seen before. Both tests are in
`internal/provider/codex/nonblocking_reality_test.go`: a 5 s wait (`:48`) and a fixed 2.5 s sleep
before an assertion (`:82`).

**E8. Under load on macOS the prewarm test misses the bug it was written for.** 0137's breakage O
re-arms the spare at turn start. It was planted on scratch copies, and the two prewarm tests ran 10
times each under 12 workers:

| Host | Tests | `TestSpareIsRearmedAtTurnEndNotTurnStart` with breakage O |
| --- | --- | --- |
| macOS | as they are | **passed 8 of 10**. The other 2 failed only by timing out, never with the ordering message |
| macOS | with the fix below | failed 10 of 10, with "the spare was armed 1 time(s) while the turn was still running" |
| Linux test host | as they are | failed 10 of 10, with the ordering message |

Without the breakage, the fixed tests passed 10 of 10 on both hosts. On macOS the failed spawn
takes longer than the 250 ms window to log, so the early arm is not yet visible when the window
closes.

**E9. Linux is unaffected.** The same generator with 12 workers ran 18,213 fresh scripts in 44 s on
the Linux test host (4 cores), about 414 per second. On the macOS host it ran 1,159 in 253 s, about
4.6 per second.

**E10. Running a stub once before the test times it removes the cost.** Under 12 workers, 15 stubs
each:

| Run | Median | Max |
| --- | --- | --- |
| first run of a fresh stub (today's tests) | 1,968 ms | 3,642 ms |
| a guarded warm-up run that exits before the body | 1,948 ms | 4,078 ms |
| the timed run after the warm-up, with different arguments and environment | 16 ms | 26 ms |

The body ran exactly 30 times, 15 fresh and 15 timed, and no warm-up reached it.

**E11. The warm-up fixes the `codex` tests under the same 40-worker load.**
`TestNonBlockingProbeRunsNoBinaryOnAColdCache` and `TestNonBlockingProbeCoalescesConcurrentCallers`
failed 5 of 5 runs without the warm-up and passed 4 of 4 with it. Both runs of the package hit
`go test`'s 10-minute package limit under that artificial load, so neither completed all three
passes. Across what did run, 1,355 test results passed and 5 failed without the warm-up; 1,037
passed and none failed with it.

**E12. CI cannot see this.**

* CI runs Go on Ubuntu and Windows runners, and deliberately has no macOS Go job (`ci.yml`, MADR
  0120 D11).
* `ci-flakes.tsv` holds 16 retried CI failures, and none of them is one of these tests.
* The prewarm tests are skipped on Windows (`testexec.SkipIfNoPOSIXModes`).

### Which tests are exposed

Every test file that writes a script was read (19 files, plus the one that builds a binary). For each
site that is run, the bound around the run is:

| Site | Bound | Fails when the first run is held longer than |
| --- | --- | --- |
| `acpagent/prewarm_rearm_test.go` (both tests) | 3 s wait; 250 ms negative window | 3 s; the window passes falsely above about 250 ms (E8) |
| `codex/nonblocking_reality_test.go` (both tests) | 5 s wait; 2.5 s fixed sleep | 5 s and 2.5 s (E7) |
| `codex/sandbox_health_test.go`, `TestProbeSandboxHealth_FakeBinOK` and `…Denied` | 8 s context in `sandbox_health.go:115` | 8 s, from the bound; not observed |
| `cli/service/installed_test.go`, `TestExecRefresherParsesJSON` | 10 s `ExecRefresher.Timeout` | 10 s, from the bound; not observed |
| `providerauth/ownedflow_test.go` (seven tests) | 10 s `ScanTimeout` | 10 s; observed once, not reproduced on demand |
| `providerauth/cli_test.go`, the 10 s tests, and `TestScanTimeout` | 10 s; a 15 s stopwatch | 10 s; about 13 s |
| `ws/receipt_e2e_test.go`, `TestReceiptEndToEndOverRealWS` | a freshly built `mcremote` run under a 15 s context | about 15 s, from the bound; not observed |
| `codex/status_truth_test.go`, `TestStatusDoesNotSpawnAProcessPerCall` | none: it counts probes without waiting (`:101`) | ~~never fails; its "at most 1" check passes falsely while probes are still running~~ never fails. Its immediate count catches a probe per call today only because of the 0137 F1 lock bug, and goes blind once that is fixed (Amendment 1) |

The other run sites have only production bounds of 30 s or more:
`codex/{auth,device_auth,apikey,logout,store_reality,owned_device_auth}_test.go`,
`grok/owned_device_auth_test.go`, and `daemon/credentials_test.go`. The `cli/service` fixtures
written as plain `#!/bin/sh` files (`launchd_sequence_test.go`, `setup_launchd_test.go`,
`refresh_test.go`, `setup_test.go`) are never run: `Setup` only checks the executable bit and
renders the path.

### What this is not

* **Not a product bug in what the tests check.** Magic re-arms the spare at the end of the turn,
  probes in the background, and scans for device codes correctly. The production bounds are 30 s
  (`startTimeout`, `deviceCodeScanTimeout`, `ProbeTimeout`) and run installed CLIs, which are scanned
  once after an install or update.
* **Not a CI flake.** See E12.

### The question

How should tests that run executables they have just written stay correct and stable on macOS,
including their negative checks, without loosening any assertion and without changing Magic?

## Decision Drivers

* A test must fail only when the code under test is wrong, and must not pass when it is wrong. E8
  shows the second matters more than the flake.
* No product change: the defect is in the fixtures.
* One mechanism for every exposed site, including those whose arguments are fixed by production code
  (`codex`, `cli/service`) and the native binary in `ws`.
* No assertion loosened and no bound raised: the bounds are right once they time what they mean to.
* A new test must get this right by default, or be stopped by a check.
* The proof must be repeatable by someone else.

## Considered Options

* Run every fixture once before the test times it, in the shared helpers, and route every run
  fixture through them
* Run fixtures as `/bin/sh <script>` instead of executing the script
* Raise the time bounds
* Skip or quarantine the exposed tests on macOS
* Make only the prewarm tests count a failure that happens before any spawn

## Decision Outcome

Chosen option: "Run every fixture once before the test times it, in the shared helpers, and route
every run fixture through them". It is the only option that removes the hold from every timed
window (E10, E11), restores the negative check (E8), and covers fixtures whose arguments the test
cannot choose, without changing Magic.

* **D1. `testexec.WriteShellStub` warms the stub before returning it.**
  * It writes `#!/bin/sh`, then a guard line, then the body. The guard is
    `[ -n "$MCREMOTE_STUB_WARM" ] && { : > "$MCREMOTE_STUB_WARM"; exit 0; }`.
  * It runs the stub once with `MCREMOTE_STUB_WARM` set to a fresh path. It fails the test, with the
    output, if that run fails or the file was not created. So a warm-up that did not go through the
    interpreter cannot pass silently.
  * The guard means the warm-up never reaches the body, so markers, counters and side effects see
    only the runs the test makes (E10: 30 of 30).
  * The variable is test-only. Nothing in Magic reads it, and the code under test never sets it.
  * The Windows skip is unchanged.
* **D2. `testexec.WarmExecutable` warms a native binary.** It runs the binary once with arguments the
  caller names as harmless (for `mcremote`, `version`). `ws/receipt_e2e_test.go` calls it between the
  build and the 15 s run.
* **D3. Every fixture that is run goes through D1 or D2.**
  * Three test files already call `WriteShellStub` and are covered by D1 alone:
    `acpagent/prewarm_rearm_test.go`, `codex/nonblocking_reality_test.go` and
    `daemon/credentials_test.go`.
  * Twelve write run fixtures themselves, and change to call it: eight in `codex`, one in `grok`,
    two in `providerauth` and one in `cli/service` (the PLAN lists each helper). That includes the
    30 s sites, so that a future tighter bound is safe too.
  * Each local helper keeps its signature.
  * Fixtures that are never run stay as they are.
* **D4. Negative checks wait for what they watch.**
  * The prewarm window stays at 250 ms. With the stub warmed, a stray arm logs within it (E8).
  * `TestStatusDoesNotSpawnAProcessPerCall` waits, with a bound, for any background probe to finish
    before it counts. (Amended 2026-10-15: today this guards against the 0137 F1 fix, not against
    the scan. See Amendment 1.)
* **D5. A guard test keeps it that way.** It fails when a `_test.go` file under `internal/` or `cmd/`
  contains a `#!/bin/sh` literal outside `internal/testexec`, unless the file is in its allowlist of
  never-run fixtures, each with its reason.
* **D6. No bound changes.** The 250 ms, 2.5 s, 3 s, 5 s, 8 s, 10 s and 15 s bounds stay.
* **D7. The load generator is committed** as `scripts/gatekeeper_load.py`, so the reproduction in E7,
  E8 and E11 can be rerun. It is a development aid, with no CI or Make target.

### Consequences

* Good, because the tests time what they mean to on every platform, and the reproduction that
  failed them passes (E10, E11).
* Good, because the prewarm test can again catch an arm at turn start on macOS (E8).
* Good, because a new fixture is warm by default, and a hand-written one is caught by D5.
* Good, because Magic is unchanged, and Linux and Windows CI see only one extra run of each stub,
  which costs milliseconds.
* Neutral, because each stub still pays its first-run scan, now in the helper, so a loaded macOS run
  is no faster. It is only correct.
* Bad, because the warm-up must never be able to run the body. A body placed before the guard would
  break that, so D1 owns the file layout and the unit tests check it.
* Bad, because D5 is a text check: it reads source, and its allowlist must be kept current.

### Confirmation

* `testexec` unit tests: the warm-up runs and does not reach the body; a warm-up that fails, or does
  not create its file, fails the test; the variable is not set for later runs; `WarmExecutable`
  fails on a file it cannot run.
* Each changed test is seen failing on a scratch copy with a planted break for what it guards, and
  0137's breakages M, N and O still fail (E8's table).
* The reproduction (PLAN W6) passes:
  * the stub packages, `-count=3`, under 12 and 40 workers, with no test failure;
  * breakage O caught 10 of 10 under 12 workers, on the macOS host and on the Linux test host;
  * two full `go test -race ./...` runs under 12 workers, with no failure in the exposed tests.
* D5 is seen failing on a planted direct `#!/bin/sh` write.
* `make preflight` and `make ci-windows` pass.

## Pros and Cons of the Options

### Run every fixture once before the test times it, in the shared helpers, and route every run fixture through them

* Good, because E10 and E11 show the timed run drops from seconds to about 16 ms under load.
* Good, because it works whatever arguments the code under test passes.
* Good, because one helper owns it, and D5 enforces it.
* Neutral, because every stub runs once more, which costs milliseconds off macOS.
* Bad, because 12 test files change mechanically, beside `testexec` and the `ws` test.

### Run fixtures as `/bin/sh <script>` instead of executing the script

* Good, because `/bin/sh` is never scanned (E2: 13 ms), and E8 shows it restores the prewarm check.
* Bad, because only the sites whose arguments the test chooses can use it: `acpagent`, `providerauth`
  and `grok`. `codex` and `cli/service` pass fixed arguments to the binary, and `ws` runs a real
  binary.
* Bad, because the stub no longer stands in for the agent binary in the way the code under test
  launches it.

### Raise the time bounds

* Good, because it is a few one-line changes.
* Bad, because it does not fix the negative windows: a longer wait cannot make a stray arm visible
  inside 250 ms (E8).
* Bad, because the hold has no upper bound: 9.45 s, 9.0 s and 11.0 s were observed, and a removed
  re-arm would take the whole new bound to fail.

### Skip or quarantine the exposed tests on macOS

* Good, because no flakes.
* Bad, because the macOS development host is the only place darwin runs at all (E12), so the tests
  would never run there.

### Make only the prewarm tests count a failure that happens before any spawn

Point `DefaultCWD` at a missing directory, so `EnsureWarm` logs "prewarm failed" before it spawns.

* Good, because no process runs, so there is nothing to scan.
* Bad, because it fixes two of the exposed tests and none of the others.
* Bad, because those tests would then no longer exercise a spawn.

## More Information

* **Amendment 1, 2026-10-15: what the status test's wait protects against.** Found while executing
  PLAN W4, and measured on scratch copies under 12-worker load, 10 runs each:

  | Planted break | Today's test (immediate count) | With D4's wait |
  | --- | --- | --- |
  | the coalescing flag bypassed | passes 10/10 | passes 10/10 |
  | a forced background probe on every call | caught 10/10 | caught 10/10 |
  | a spawn on every call that bypasses the cache | caught 10/10, seeing 2–3 of 6 | caught 10/10, seeing 6 |
  | the same, with the 0137 F1 lock fixed (simulated) | **passes 10/10, falsely** | **caught 10/10** |

  * The table row's original claim was wrong for today's code. The immediate count works today only
    because the F1 lock makes each status call wait behind the running probe. With that bug fixed,
    the count is read before the spawns finish.
  * The coalescing-flag break does not exercise this test. With a warm stub, the first probe finishes
    before the second call, so the cache answers the rest either way. That case is
    `TestNonBlockingProbeCoalescesConcurrentCallers`'s (0137 S2).
  * The owner chose to keep the wait in 0193, so that it is already in place when the F1 fix lands.
* **Note, 2026-10-15: `testexec` is allowlisted in the A17 spawn guard.** D1 and D2 spawn with
  `exec.Command`. `testexec` cannot use `procutil.Command`, because `procutil` imports `fsutil`,
  whose in-package tests import `testexec`. So `internal/testexec/testexec.go` is in
  `spawnAllowlist`, with that reason and the fact that it is test-only (PLAN Deviations 2 and 3).
  No decision changes.
* **Not proven.**
  * `TestOwnedFlowStartsWithAnEmptyHome` was not reproduced on demand: it passed in six package runs
    under 12 and 40 workers and in two loaded full suites. It needs a hold over 10 s. It shares the
    mechanism (E5) and is fixed by D3, but its failure was seen only once.
  * The `sandbox_health`, `installed_test` and `receipt_e2e` exposures follow from their bounds and
    E5. They were not observed failing.
  * During one run of two suites at once, one suite exited 1 with no failing test recorded. Its raw
    output was not kept, and it has not recurred. PLAN W6 keeps raw output from every run.
  * The host runs an endpoint-management agent (`JamfDaemon`). Agents of that kind can also hold
    program launches. Its share of the latency was not measured.
* **Found, out of scope: a product bug in 0137 F1.**
  * `ObserveCredentialStoreCached` holds `realityCache.mu` while it runs `codex doctor`
    (`store_reality.go:194-207`). `ObserveCredentialStoreCachedNonBlocking`, which 0137 F1 says
    "never waits for a probe", takes the same lock first.
  * So a caller that arrives while a background refresh runs waits for it. On a scratch copy, a
    second non-blocking call made 300 ms into a 2 s probe **waited 1.73 s**, in 3 of 3 runs.
  * `providers.list` on a phone's reconnect takes that path. A real refresh takes about 1.4 s
    (0137's own measurement), and up to the 30 s probe limit.
  * The existing coalescing test does not see it, because its 20 calls arrive within microseconds,
    before the background goroutine takes the lock.
  * By the same-topic rule it belongs to
    [0137-MADR-prompt-to-first-token-latency-regression.md](0137-MADR-prompt-to-first-token-latency-regression.md),
    as an amendment for the owner to approve.
* **Related records.**
  * [0137-PLAN-prompt-to-first-token-latency-regression.md](0137-PLAN-prompt-to-first-token-latency-regression.md)
    wrote the prewarm tests and breakages M, N and O.
  * [0143-MADR-ci-retries-once-and-records-every-retry.md](0143-MADR-ci-retries-once-and-records-every-retry.md)
    owns the flake ledger.
  * [0173-MADR-fix-two-ci-flakes-hub-lock-close-and-leaked-transcript-saves.md](0173-MADR-fix-two-ci-flakes-hub-lock-close-and-leaked-transcript-saves.md)
    is the precedent for a record about flakes.
* The PLAN is
  [0193-PLAN-warm-fresh-test-executables-before-timing-them.md](0193-PLAN-warm-fresh-test-executables-before-timing-them.md).
  ~~Nothing is implemented until the owner accepts an option and approves the PLAN.~~ On 2026-10-15
  the owner accepted the recommended option and approved the PLAN ("accept recommendations and
  proceed").

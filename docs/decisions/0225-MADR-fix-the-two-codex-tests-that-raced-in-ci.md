---
status: accepted
date: 2026-10-31
decision-makers: Project Owner
consulted: "CI run 41248953620 on c12e6e37 (attempts 1 and 2); this repository at c12e6e37 (internal/provider/codex, internal/testexec); the Codex sources at b91cedc080 (2026-10-30), read locally (codex-rs/cli/src/doctor.rs and doctor/, codex-rs/app-server-transport/src/transport/stdio.rs); 0137-MADR; 0224-PLAN deviation 2; scratch probes leftovers/0224_codex_flake.py, 0225_repro.py, 0225_validate.py and 0225_mu_scan.py"
informed: anyone who reads CI on master
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Fix the two codex tests that raced in CI: hold the probe stub open, and read the engine under its lock

## Context and Problem Statement

CI run `41248953620` on `c12e6e37`, the commit of
[0224-PLAN-pause-the-acp-reader-instead-of-dropping-notifications.md](0224-PLAN-pause-the-acp-reader-instead-of-dropping-notifications.md),
failed in two tests of `internal/provider/codex`, a package that commit does not change. 0224's
deviation 2 showed the commit did not cause them, re-ran the failed jobs, and left the two tests to a
record of their own, grounded in the current Codex sources. This is that record.

The question: what is wrong with each test, is anything wrong with the code it tests, and what change
makes each test assert its property without depending on how goroutines are scheduled?

### What failed [observed: CI run `41248953620`, attempt 1]

* **F1 — `TestNonBlockingProbeRunsNoBinaryOnAColdCache`** (`Go (test)`, ubuntu, "Test (race, all
  packages)"), at `nonblocking_reality_test.go:43`, in 0.01 s: "the binary ran before the call
  returned: the probe is still synchronous on the caller's goroutine".
  * The test pins 0137 F1: `ObserveCredentialStoreCachedNonBlocking` must answer a cold cache at once
    and probe in the background.
  * The code keeps that promise. It sets `refreshing`, starts the probe on its own goroutine
    (`store_reality.go:316-327`) and returns `RealityUnknown` (`:328`).
  * The test calls it, then asserts that the stub's marker file does not exist yet (`:43-46`). The
    goroutine the call has just started is free to run the stub before that check. On a fast runner
    it did. The assertion is a race between the code's own background work and the test's next
    statement. It is not a property of the code.
  * The test dates from `a59ea111` (2026-09-03), and those lines have not changed since.
* **F2 — `TestEngineGenerationResetsCollaborationProbe`** (`Go (darwin/arm64)`, "Test (race,
  darwin, cgo-free)"): `WARNING: DATA RACE`.
  * **The write:** `handleUnexpectedEngineExit` sets `p.eng = nil` (`provider.go:1009`) under `p.mu`.
    It runs on the engine's exit goroutine, started at `provider.go:988` and calling at `:995`.
  * **The read:** the test reads `p.eng` at `collaboration_test.go:340`, to kill the engine, without
    `p.mu`. It also reads `p.generation` without the lock at `:339` and `:357-358`, which the
    replacement path writes under `p.mu` (`provider.go:933`).
  * The test dates from `f3698131` (2026-08-12), and those lines have not changed since.
* **F3 — Neither failure is the commit's** (0224 deviation 2).
  * `c12e6e37` changes no codex file.
  * Codex reaches acp-go-sdk2 only through `agentproc` and `internal/proctree`. Between v1.28.0 and
    v1.30.0 those differ only in `agentproc/ties_test.go`.
  * The two tests, `-race -count=50`, at default `GOMAXPROCS` and at 2, in copies of `c12e6e37` and
    its pre-bump ancestor `fd70cfaf` (`leftovers/0224_codex_flake.py`): 200 of 200 passes each, 0
    races.
  * Attempt 2 of the run re-ran the two failed jobs: `Go (darwin/arm64)` passed.

### What reproduces them [2026-10-31, `git archive` copies of `c12e6e37`; the tree was not written]

`leftovers/0225_repro.py`; logs `leftovers/0225-repro-*.log`.

* **E1 — F1 is the test's race, not the code's.** The unchanged test, with a 100 ms pause between the
  call's return and the marker check (the test goroutine preempted), production code unchanged:
  `-race -count=5`, 5 of 5 fail at `nonblocking_reality_test.go:44` with CI's message.
* **E2 — F2 reproduces with CI's stack.** The unchanged test with a 300 ms pause after `Kill`, so the
  exit goroutine writes while the unlocked read is still in the race detector's history:
  * five separate processes, `-race -count=1`: 5 of 5 fail, 5 data races;
  * each race names the write at `provider.go:1009` from `:995`, and the read at
    `collaboration_test.go:340`, as CI did.
  * In one process `-count=5` shows 1 race for 5 runs, because the race detector reports a racing
    pair once per process.

### What the code and the other tests do [observed at `c12e6e37`]

* **F4 — Production reads the engine's fields under `p.mu`.** `leftovers/0225_mu_scan.py` lists
  every access to `p.eng`, `p.generation` and `p.execution` in non-test codex code without a
  preceding `p.mu.Lock()` in the same function. It flags `provider.go:428-429`, `:926-957` and
  `:1009-1011`. Each was read by hand, and each is under `p.mu`: the scan's flag resets at an
  early-return unlock. The scan is line-based, not a proof. The race of F2 is the test's alone.
* **F5 — The pattern the fix needs is already in the package.**
  * `reconnect_p3_test.go:25-27` and `:41-50` take `p.mu` to read `p.eng` before killing the engine
    and while waiting for its replacement. `provider_test.go:202-204` does the same.
  * `collaboration_test.go` reads a live engine's `p.eng` without the lock in three more places:
    `:161`, `:183` and `:275-276`. In each, the engine stays up until the deferred `Shutdown`, so no
    write runs concurrently today, but an engine that died early would race exactly as F2 did.
  * ~~`live_contract_test.go:86-88` does the same under the `live` tag, which runs a real Codex and
    spends quota.~~ *Corrected 2026-10-31 (PLAN deviation 1):* `live_contract_test.go:85-89` takes
    `p.mu` before it reads `p.eng` and releases it after. The grep behind the struck sentence dropped
    every line naming `p.mu`, and the file was not read. The test has no race. It does read `p.eng`
    without a nil check, so an engine that died between `ensureEngine` and the lock would panic the
    test rather than fail it. It builds only under the `live_codex_contract` tag and runs against a
    real Codex, spending quota.
* **F6 — The package already has a stub that waits.** `reality_lock_test.go:15-17`: "Each stub writes
  a marker before it sleeps, so a test knows a probe is running without guessing from a fixed
  delay". Its `waitForFile` (`:19-32`) waits up to 10 s for a file and fails naming it.

### What the Codex sources say [read at `b91cedc080`, 2026-10-30]

* **F7 — `codex doctor` is slow by design, and not by an amount Magic controls.**
  * `build_report` (`codex-rs/cli/src/doctor.rs`) runs its checks concurrently in one `tokio::join!`:
    config, auth, updates, network, WebSocket, MCP, sandbox, terminal, git, state, thread inventory,
    background server and reachability.
  * Their own deadlines:
    * the provider HTTP probes, 8 s (`doctor/network.rs:184`, `:192`);
    * the filesystem probes, 2 s each and 8 s in all (`doctor/filesystem_paths.rs:36-37`);
    * the endpoint-protection query, 5 s (`doctor/security.rs:22`);
    * the update checks, 2 s and 1 s (`doctor/updates.rs:102`, `:327`).
  * So the probe 0137 F1 keeps off the caller's goroutine can take seconds. A stub that waits until
    released is a faithful stand-in for it; a stub that exits at once is not.
* **F8 — Codex tests a bounded probe the same way.** `returns_when_a_probe_process_does_not_exit`
  (`doctor/filesystem_paths_tests.rs:198-211`) starts a fixture process that sleeps 30 s
  (`blocked_probe_fixture`, `:213-217`). It asserts the caller's answer comes back, and that it came
  back within 5 s. The child is blocked on purpose, and the assertion is about the caller, never
  about whether the child has started.
* **F9 — A Codex app-server's exit is asynchronous to its client.**
  * The stdio transport ends on stdin EOF or SIGTERM and arms a 45 s shutdown watchdog before the
    process exits (`codex-rs/app-server-transport/src/transport/stdio.rs:1-4`, `:125-133`,
    `:167-180`).
  * So the moment Magic's exit goroutine observes an engine's end is not ordered with anything else
    a test does. Every read of the engine's state that can overlap it must take the lock it writes
    under, as production does (F4).

### What the proposed fixes do [2026-10-31, `git archive` copies of `c12e6e37`]

`leftovers/0225_validate.py`, the fixes of D1 and D2 applied to copies; logs
`leftovers/0225-val-*.log`.

* **P1 — The probe test, rewritten as D1:**
  * `-race -count=50`: 50 of 50 pass, 0 races;
  * with E1's 100 ms pause after the call returns: 5 of 5 pass;
  * with a plant that runs the probe on the caller's goroutine (`go func()` made `func()` in
    `store_reality.go`): fails, "the call had not returned after 5s with the probe still running:
    it waits for the probe on the caller's goroutine".
* **P2 — The collaboration test, rewritten as D2:**
  * `-race -count=50`: 50 of 50 pass, 0 races;
  * with E2's 300 ms pause after `Kill`, five separate processes: 5 of 5 pass, 0 races. E2 gave 5
    races there.

## Decision Drivers

* **D-1 — A test asserts a property of the code,** not an order between the code's goroutines and
  its own statements.
* **D-2 — No production change for a test defect.** F1's code keeps its promise, and F4 shows
  production locks correctly.
* **D-3 — Seen failing for the right reason.** Each rewritten test fails on a plant that breaks the
  property it names, and passes under the perturbation that broke the old one.
* **D-4 — The whole pattern, not one line.** A fix that leaves the same unlocked read beside it waits
  for the next CI run to find it (F5).
* **D-5 — Follow what exists:** the package's own locked reads (F5) and waiting stubs (F6), and
  Codex's blocked-fixture pattern (F8).

## Considered Options

* **A — Rewrite the probe test to hold its stub open and bound the call. Read every live engine's
  state in `collaboration_test.go` under `p.mu`, through one helper.**
* **B — As A, but change only the failing lines** (`nonblocking_reality_test.go:43-46`;
  `collaboration_test.go:339-340`, `:357-358`).
* **C — Add a test hook to production** (a channel the background refresh signals, or an injectable
  spawner), and assert on it.
* **D — Retry or skip the two tests in CI.**
* **E — Widen timing:** sleep before the marker check, or after `Kill`.

## Decision Outcome

Chosen option: "A", because it makes each test assert its property whatever the scheduling (P1, P2),
changes no production code (D-2), and removes every unlocked read of a live engine in the file that
failed, not only the one CI caught (D-4). B leaves `:161`, `:183` and `:275-276` as they are. C adds
production surface to fix a test, though F1's code needs nothing. D and E hide the defect: E's pauses
are exactly what E1 and E2 used to make the old tests fail or pass at will.

### The decisions

* **D1 — The probe test asserts that the call returns while the probe is still running.**
  * `TestNonBlockingProbeRunsNoBinaryOnAColdCache` becomes
    `TestNonBlockingProbeReturnsWhileTheProbeRuns`, since its old name was the claim that raced.
  * Its stub writes a `started` marker, waits until a `release` file exists (bounded at 30 s, so it
    cannot outlive a run), then writes `finished` and exits 1.
  * The test runs the call on a goroutine and requires it back within 5 s, far above a non-blocking
    call and far below the 30 s `providerauth.ProbeTimeout` a blocking one would wait for. It
    requires `RealityUnknown`, and that `finished` does not yet exist, which only the release can
    cause.
  * Then it waits for `started`, writes `release`, and waits for `finished`, with the package's
    `waitForFile` (F6). That shows the background refresh runs, and runs to its end.
  * A cleanup writes `release` however the test ends.
  * Its comment keeps 0137 F1's reason, says why the stub waits, and cites F1, F7 and F8.
* **D2 — Every read of a live engine's state in `collaboration_test.go` takes `p.mu`.**
  * A helper, `liveEngine(p) (*engine, int)`, returns `p.eng` and `p.generation` under `p.mu`, as
    `reconnect_p3_test.go` reads them inline (F5).
  * `TestEngineGenerationResetsCollaborationProbe` takes the engine and generation through it before
    `Kill`, and the generation through it after the replacement (`:339-340`, `:357-358`).
  * `:161`, `:183` and `:275-276` read the engine through it too.
* **D3 — No production change.** `store_reality.go` and `provider.go` stay as they are (F1, F4).
* ~~**D4 — `live_contract_test.go:86-88` is left as it is.** It runs only under the `live` tag
  against a real Codex, and no live run is in scope. It is named here so the pattern is not lost.~~
* **D4, as amended 2026-10-31 — `live_contract_test.go` reads the engine through `liveEngine` too,
  and fails clearly when there is none.** The owner added it to scope, then, on the corrected F5,
  chose this form. Its locked read (`:85-89`) becomes `eng, _ := liveEngine(p)`, then a `t.Fatal`
  if `eng` is nil, then the same three reads from `eng`. One pattern holds across the package, and a
  dead engine fails the test instead of panicking it. It is compiled and vetted under its tag on
  darwin, linux and windows, and not run: no live run is in scope.
* **D5 — Each fix is seen failing first, in scratch copies:**
  * on the unchanged tree, with E1's and E2's perturbations;
  * the rewritten probe test on the synchronous plant (P1);
  * the rewritten collaboration test with its locked reads undone and E2's pause, which must show
    the race again.
* **D6 — 0224 is not held for this.** 0224 deviation 2 closes 0224 on the re-run of its failed
  jobs, and this record's commit gets its own CI.

### Consequences

* Good, because both tests now hold whatever the scheduling: 5 of 5 under the perturbations that made
  the old ones fail 5 of 5 (P1, P2, E1, E2).
* Good, because the probe test now proves more than the old one: that the call returns while the
  probe runs, and that the probe then finishes. The old one proved only that the call returned before
  the stub started, when the scheduler let it.
* Good, because `collaboration_test.go` has no unlocked read of a live engine left (D-4).
* Neutral, because the probe test takes about 100 ms longer, waiting on the released stub.
* ~~Bad, because `live_contract_test.go` keeps the pattern until a live run is in scope (D4).~~
  Bad, because D4's change to `live_contract_test.go` is compiled but not run until a live run is in
  scope.
* Bad, because CI cannot prove the absence of a race, only fail to find one. P2 and the
  perturbations are the evidence that this one is gone.

### Confirmation

* `TestNonBlockingProbeReturnsWhileTheProbeRuns` and `TestEngineGenerationResetsCollaborationProbe`
  pass `-race -count=50`, and under E1's and E2's perturbations, in copies.
* The synchronous plant fails the probe test. Undoing D2's locked reads, with E2's pause, fails the
  collaboration test with a data race.
* No `p.eng` or `p.generation` read without `p.mu` remains in `collaboration_test.go`.
* `live_contract_test.go` reads the engine through `liveEngine`, fails on a nil engine, and builds
  and vets under `live_codex_contract` on darwin, linux and windows (D4 as amended).
* `make race`, the Linux and Windows test hosts, and CI on the push pass.

## Pros and Cons of the Options

### A — Hold the stub open; lock every live-engine read in the file

* Good, because each test asserts its property and nothing else (D-1, P1, P2).
* Good, because it reuses the package's helpers and patterns (F5, F6) and follows Codex's (F8).
* Bad, because it touches three passing reads that CI has not failed on, for D-4.

### B — Only the failing lines

* Good, because the diff is smallest.
* Bad, because the same race waits at `:161`, `:183` and `:275-276` for an engine that dies early.

### C — A production test hook

* Good, because a test could observe the refresh's start exactly.
* Bad, because it adds production surface where the code is correct (D-2), and the stub of A
  already makes the start observable.

### D — Retry or skip in CI

* Good, because CI turns green at once.
* Bad, because the tests keep asserting a race, and a real regression would be retried away.

### E — Widen timing

* Bad, because a pause cannot order goroutines, only make one order likelier. E1 and E2 used such
  pauses to make the old tests fail or pass at will.

## More Information

### The owner's answers

*Answered by the owner, 2026-10-31:* "approve scope 1" (option A: every live-engine read in
`collaboration_test.go`), "test hosts 2" (the PLAN's S4 runs), "but also add the live contract
test". On the corrected F5, "Use liveEngine, add nil check" (D4 as amended).

### Evidence index

| Claim | Source |
| --- | --- |
| The CI failures | run `41248953620`, attempt 1: jobs `Go (test)` and `Go (darwin/arm64)`; `gh run view --log-failed` |
| The code keeps 0137 F1 | `store_reality.go:293-329` |
| The race's stack | CI's log; `leftovers/0225-repro-e2.log` |
| Not the commit's | 0224-PLAN deviation 2; `leftovers/0224_codex_flake.py` |
| E1, E2 | `leftovers/0225_repro.py`; `leftovers/0225-repro-{e1,e2}.log` |
| Production locks | `leftovers/0225_mu_scan.py`; `provider.go:418-440`, `:920-960`, `:1002-1015` |
| Patterns in the package | `reconnect_p3_test.go:25-27`, `:41-50`; `provider_test.go:202-204`; `reality_lock_test.go:15-32` |
| Codex's doctor and its deadlines | `codex-rs/cli/src/doctor.rs` (`build_report`); `doctor/network.rs:184`, `:192`; `doctor/filesystem_paths.rs:36-37`; `doctor/security.rs:22`; `doctor/updates.rs:102`, `:327` |
| Codex's blocked-fixture test | `codex-rs/cli/src/doctor/filesystem_paths_tests.rs:198-217` |
| Codex's app-server exit | `codex-rs/app-server-transport/src/transport/stdio.rs:1-4`, `:125-133`, `:167-180` |
| P1, P2 | `leftovers/0225_validate.py`; `leftovers/0225-val-*.log` |

### Related records

* [0224-PLAN-pause-the-acp-reader-instead-of-dropping-notifications.md](0224-PLAN-pause-the-acp-reader-instead-of-dropping-notifications.md):
  deviation 2, which found these failures and left them to this record.
* [0137-MADR-prompt-to-first-token-latency-regression.md](0137-MADR-prompt-to-first-token-latency-regression.md):
  F1, the property the probe test pins.

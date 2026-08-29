---
status: accepted
date: 2026-08-28
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Establish causation before repairing the codex tests that fail on linux/arm64

## Context and Problem Statement

`master` is red. Since commit `fb0f361` (the 0118 implementation) the
`Go (linux/arm64)` lane fails in `internal/provider/codex`, and it failed on
both runs taken so far. A **different** test failed each time:

```text
run 33210742346, attempt 1
--- FAIL: TestDiagnosticRunnerExactArgvTimeoutNonzeroAndSingleFlight (0.00s)
    diagnostics_p4_test.go:85: doctor invocations = 2, want one

run 33210742346, attempt 2
--- FAIL: TestUnrelatedInitializeErrorDoesNotRetry (0.01s)
    collaboration_test.go:199: unrelated JSON-RPC must not relaunch
```

Neither test touches symlinks, and neither file was modified by `fb0f361`. The
only change 0118 made to that package is one added line in
`projects_p6_test.go`.

Two readings fit, and they call for opposite work:

1. **Latent flakiness, newly visible.** Both tests contain a real race (below).
   `fb0f361` perturbed timing enough to expose them, or the ARM runner did. The
   repair is to the two tests; 0118 is incidental.
2. **A regression from `fb0f361`.** Something about the commit genuinely breaks
   the package on that lane. The repair is to 0118.

Choosing wrong is expensive in both directions: fixing the races when the cause
is elsewhere leaves `master` red and the real defect hidden, while reverting
0118 when the races are the cause loses a correct fix and does not stop the lane
flaking again.

### What was measured, not assumed

**The lane's history is one-sided.** The arm64 lane on `b92c4e7` — the commit
immediately before `fb0f361` — is green in five runs, four historical and one
re-run deliberately today (2026-08-28) to test whether the green was stale. It
passed again. The lane on `88171bc` is red in two of two.

| Commit | arm64 lane | Runs |
| --- | --- | --- |
| `b92c4e7` (pre-0118) | green | 5 of 5, incl. a re-run today |
| `88171bc` (post-0118) | red | 0 of 2 |

**Both failures are races the source shows plainly.**

`TestDiagnosticRunnerExactArgvTimeoutNonzeroAndSingleFlight` starts two
goroutines and expects singleflight to coalesce them. The main goroutine waits
on `<-entered`, which proves only that the *first* caller reached `doctorRun`.
Nothing holds the second goroutine until it has joined the group, so a
sufficiently slow scheduler lets the first call complete and drop the group
entry before the second arrives — the second then makes a real call and `calls`
reaches 2. The barrier the test needs does not exist.

`TestUnrelatedInitializeErrorDoesNotRetry` counts launches by reading a file
written by a spawned helper subprocess, and `launchCount` returns **0** when the
file does not exist (`collaboration_test.go:350`). If `ensureEngine` fails fast,
the parent can read before the child has written. The assertion is `!= 1`, so a
premature read is indistinguishable from a genuine double launch.

**The tests are not parallel.**
`grep -rn 't.Parallel()' internal/provider/codex/` returns nothing, so
cross-test interference within the package is not the mechanism. Both races are
internal to a single test.

**They do not reproduce on the development host.** `go test -count=10` over both
tests passes on windows/amd64. Consistent with a race whose window this machine
is too fast to open; not evidence of correctness.

**Test-result caching is not hiding the earlier greens.** The lane runs plain
`go test ./...` and `setup-go` sets `cache: true`, so cached results were the
leading explanation for five prior greens. It is wrong: the `b92c4e7` log shows
`ok github.com/maccavelli/magic-cli-remote/internal/provider/codex 1.695s`, a
real execution, not `(cached)`. **This hypothesis is closed.**

**The lane is native, not emulated.** `ubuntu-24.04-arm` (`ci.yml:277`), so QEMU
slowness is not available as an explanation either.

### Findings

**F1 — `master` is red now.** Whatever the cause, the default branch does not
pass CI, and every later push inherits a red baseline that masks new breakage.

**F2 — The causal claim is unproven in both directions.** 5–0 against 0–2 is
suggestive but small. Two runs cannot separate "a change that causes failure"
from "a change that shifted timing in a package that was already going to fail".
No experiment isolating the variable has been run.

**F3 — `fb0f361` has no visible path to either failure.** On Linux
`SkipIfNoSymlink` creates a temp dir, writes a file, creates a symlink, and
returns. It does not touch `ensureEngine`, the doctor singleflight, process
launch, or any shared state. `MC_REQUIRE_SYMLINK` is read only by that helper —
no codex source reads it. If `fb0f361` is the cause, the mechanism is timing,
not logic.

**F4 — Two independent defects exist regardless.** Both races are real and would
eventually fire on any sufficiently loaded runner, whatever the verdict on
`fb0f361`. Fixing them is correct work that no outcome of the investigation
makes unnecessary.

**F5 — A different test failed on each run.** A deterministic regression
reproduces the same way. Two distinct failures point at a package with more than
one latent race being sampled, which favours reading 1 — but see F2.

**F6 — The failure rate is unmeasured.** Whether the lane fails 100% or 40% of
the time changes both the diagnosis and the urgency, and nobody has counted.

## Decision Drivers

* `master` must go green, and be *known* to be green rather than assumed.
* A red lane must not be quieted by a change that merely hides the symptom —
  that is precisely the failure mode 0118 D4 exists to prevent.
* 0118 is a correct fix for a real problem; discarding it needs evidence, not
  suspicion.
* Race repairs must be verified under conditions that actually open the window,
  which the development host does not.
* Guessing is cheaper than measuring here, which is exactly why it must not be
  the method.

## Considered Options

* **A — Fix the two races, assume flakiness.** Skips F2 entirely.
* **B — Revert `fb0f361`, restore green, investigate later.** Treats correlation
  as causation and loses a correct fix.
* **C — Isolate the variable first, then fix what the evidence indicts.**
* **D — Do nothing, mark the lane non-blocking.** Rejected on sight: it converts
  a real red into a permanent blind spot.

## Decision Outcome

**Chosen: C — isolate the variable first.**

The two readings in the problem statement demand different repairs, F2 says the
evidence cannot currently choose between them, and F6 says the basic measurement
is missing. One cheap experiment settles it: run the arm64 lane repeatedly on
the pre-change commit and on the post-change commit, and split `fb0f361` into
its two independent parts — the test-file edits and the `ci.yml` env line — so
each is tested alone.

This is chosen over A because A would land race fixes and declare victory
without ever learning whether they addressed the cause; if `fb0f361` really does
break the lane, a fixed race would simply move the failure. It is chosen over B
because reverting a correct fix on two data points is not a decision, it is a
flinch — and B leaves F4 unaddressed, so the lane stays fragile.

The cost is one investigation phase before any repair. That is the price of
knowing, and it is small: each arm64 run is under a minute.

### The decisions

**D1 — Measure before repairing.** No change to either failing test until the
isolation experiment has reported. The experiment is the first phase and it
gates the rest.

**D2 — Split `fb0f361` into its two variables.** The test-file additions and the
`ci.yml` `MC_REQUIRE_SYMLINK` line are independent. Test each alone on the arm64
lane. F3 predicts neither is causal; if that prediction fails, the failing half
is the finding.

**D3 — Report a rate, not a verdict.** Every arm64 result in this record is
`k of n` with the run IDs. A single green does not clear a commit and a single
red does not convict one. Minimum five runs per arm.

**D4 — Fix both races regardless of the verdict (F4).** They are defects on
their own terms. `TestDiagnosticRunner…` needs a real barrier so the second
caller has provably joined the group before the first is released.
`TestUnrelatedInitializeErrorDoesNotRetry` must not treat "file absent" as a
count of zero — an absent log and a single launch are different states and the
test must distinguish them.

**D5 — No blanket skip, no `testing.Short()` gate, no retry wrapper on the arm64
lane.** Each would turn a measured failure into silence. This is 0118 D2's rule
applied to a different symptom: a test that cannot run must say so for a stated
reason, not disappear.

**D6 — `master` goes green by fixing or by an evidenced revert, not by weakening
the lane.** If the experiment indicts `fb0f361`, reverting it is legitimate —
the evidence, not the inconvenience, is what licenses it.

**D7 — 0118 stays `in-progress` until this record closes.** Its P4 asked whether
the push is green. The Windows half is (A3, A6 confirmed); the run as a whole is
not. Marking 0118 complete now would record a green that does not exist.

### Consequences

* Good: the lane's behaviour becomes a measured rate, so future flakes are
  comparable against a baseline rather than argued about.
* Good: two real races are fixed, and the fix is verified where the window
  actually opens.
* Bad: `master` stays red through the investigation phase. Accepted — a red
  branch that is understood is worth more than a green one that is not, and the
  phase is short.
* Neutral: if the verdict is "pre-existing flake", 0118 is untouched and this
  record is the reason the suspicion was dropped.

### Confirmation

```text
arm64 lane, post-fix commit    → green in at least 5 consecutive runs
arm64 lane, both failing tests → run, do not skip
go test -count=50 ./internal/provider/codex/   → green on the dev host
0118 PLAN                      → status resolved, in either direction
```

## Pros and Cons of the Options

### A — Fix the races, assume flakiness

* Good: fastest to a probably-green lane.
* Good: the fixes are needed anyway (F4).
* Bad: assumes the answer to F2. If wrong, the real cause survives behind a
  now-passing test — the worst outcome available here.

### B — Revert `fb0f361`, investigate later

* Good: green immediately, near-certainly.
* Bad: convicts a commit on correlation (F2, F3) with no mechanism.
* Bad: re-breaks `go test ./...` on unprivileged Windows, the problem 0118
  exists to solve.
* Bad: leaves both races (F4) to fire another day.

### C — Isolate the variable first, then fix (chosen)

* Good: the repair is aimed at a cause that has been demonstrated.
* Good: produces the failure rate F6 says is missing.
* Good: cheap — sub-minute runs, no local reproduction needed.
* Bad: `master` stays red for one phase.

### D — Mark the lane non-blocking

* Good: nothing.
* Bad: discards the signal the lane exists to produce, permanently.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Two different codex tests failed | run 33210742346, attempts 1 and 2 |
| arm64 green 5 of 5 pre-change | runs 33101607297 (+ re-run today), 33100983283, 33099993994, 33084467102 |
| codex genuinely ran, not cached | job 98621856477 log: `ok … codex 1.695s` |
| Singleflight barrier missing | `internal/provider/codex/diagnostics_p4_test.go:49-85` |
| Absent log counts as zero | `internal/provider/codex/collaboration_test.go:346-361` |
| Seven `launchCount` call sites share the window | `collaboration_test.go:169, 198, 216, 238, 239, 323, 324` |
| No parallel tests in package | `grep -rn 't.Parallel()' internal/provider/codex/` → empty |
| Native ARM runner | `.github/workflows/ci.yml:277` (`ubuntu-24.04-arm`) |
| No codex source reads the new env var | `grep -rn 'MC_REQUIRE_SYMLINK' internal/provider/codex/` → empty |
| Passes 10 of 10 locally | `go test -count=10 -run '…' ./internal/provider/codex/` |

### Related records

* [0118-MADR](0118-MADR-symlink-dependent-tests-on-unprivileged-windows.md) —
  the commit under suspicion. Its D2 (never skip on an unknown error) is the
  same principle D5 applies here.
* [0116-PLAN](0116-PLAN-windows-and-linux-arm64-build-targets.md) — introduced
  the arm64 lane (D17): cross-compiling proves a target links, running it proves
  it works. That is the coverage this failure threatens.

### Open questions for the plan

1. Does the arm64 lane fail on `b92c4e7` when run enough times? Five greens say
   probably not, but the arm below is what decides it.
2. Which half of `fb0f361` — if either — moves the rate?
3. Is the correct repair for `TestUnrelatedInitializeErrorDoesNotRetry` a
   synchronisation barrier, or should `launchCount` distinguish "no file" from
   "zero lines" and the test assert on the stronger state?
4. Does the `internal/provider/codex` package hold further races of the same
   shape, and is a sweep in scope or its own record?

## Amendment — 2026-08-28: rate measured; P1 isolation not run

Subsequent `master` runs closed F6 without the four-arm experiment:

| Run | Commit | linux/arm64 | windows/amd64 |
| --- | --- | --- | --- |
| 33101607297 | `b92c4e7` (pre-0118) | green | green |
| 33210742346 | `88171bc` | red `TestUnrelatedInitializeErrorDoesNotRetry` | green |
| 33219191191 | `19661ca` | red `TestDiagnosticRunnerExactArgvTimeoutNonzeroAndSingleFlight` | green |
| 33221074719 | `08d5a2a` (docs) | green | green |
| 33221972891 | `e12e2a8` (docs) | red `TestUnrelatedInitializeErrorDoesNotRetry` | green |
| 33223300684 | `f8a9237` (docs) | green | red, unrelated (0122) |

arm64 after 0118 is **3 red / 2 green**. The same two tests still alternate.
Docs-only commits after `fb0f361` still flake, so `fb0f361` is not a
deterministic cause (F2, F3). Verdict: 0118 is incidental; F4's two races
are the work.

P1 of the PLAN is not run. It needs pushes the owner did not grant in the
execution turn, and the table above already answers the question P1 was
for. D4 still applies: fix both races. D6's 5-of-5 on the arm64 lane stays
the close-out and still needs a push (PLAN P4).

Open question 3 is answered in the PLAN: distinguish a missing launch log
from a zero count, then wait for the child's write. A longer sleep is not
the repair.

Windows run 33223300684 is
[0122](0122-MADR-deterministic-goose-file-log-tail-attach.md), not this
record.

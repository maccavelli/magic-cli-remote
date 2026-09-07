---
status: accepted
date: 2026-09-05
decision-makers: maccavelli
consulted: GitHub Actions run history 2026-08-19 → 2026-09-05 (115 completed runs); MADRs 0111, 0118, 0119, 0133
informed: Anyone who pushes to this repository or cuts a tag
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# CI retries a failed job once and records every retry, instead of repairing flakes one at a time

## Context and Problem Statement

Roughly one CI run in four fails, and almost none of those failures mean the
commit is broken.

Measured over the 115 completed runs between 2026-08-19 and 2026-09-05:

| Metric | Value |
| --- | --- |
| Completed runs | 115 (88 success, 27 failure) |
| Failure rate | **23.5 %** |
| Tag pushes | 6 / 22 failed (27.3 %) |
| Branch pushes | 21 / 93 failed (22.6 %) |
| Distinct test functions implicated | **at least 28** |
| Median run duration | 5.8 min |
| Genuinely broken commits in the window | **1** (the AGP 9 manifest path, `14e55d2`) |

Failures by job, across 27 failed runs (some fail two jobs):

```text
 9  Go (windows/amd64)
 7  Go (test; build on tag)
 7  Go (linux/arm64)
 3  Android APK (release arm64)
 2  Flutter (analyze & test)
 1  Flutter (test on windows)
```

The decision this record forces is **what CI should do about a job that fails
in a way a re-run would pass** — because the current answer, "a human diagnoses
and repairs each instance", is measurably not converging.

### Why "just keep fixing them" is not the null option

That is the status quo, it has been pursued diligently, and the evidence says it
is not working:

* **Four decision records already exist for this class** — 0111 (asynchronous
  receipt teardown), 0118 (symlink privilege on Windows), 0119 (the codex
  arm64 lane), and 0133's Phase 7 (a churn test's racy assertion). Each was
  correct. The rate did not move.
* **15 of the 298 commits in the window (5 %) were test-stability work.**
* **A targeted fix did not hold.** `internal/provider/codex/collaboration_test.go`
  was repaired under 0119 by `2876467` (2026-08-29T01:34Z). Run `33227671357`
  failed `TestUnrelatedInitializeErrorDoesNotRetry` **21 minutes later** with
  that fix present, and runs `33642941779` and `33648231957` failed
  `TestExperimentalInitializeRetriesOnce` on 2026-09-02, also with it present.
  One file, five failures, three of them after it was declared fixed.

The failures are not one defect wearing disguises. Windows file semantics,
arm64 timing, race-detector overhead, and goroutine scheduling are genuinely
different causes, which is why 28 different tests appear and why no single
repair reaches them. Fixing them one at a time is a treadmill whose speed is
set by how many timing-sensitive tests exist, not by how much care each fix
receives.

### What the failures cost

Every red run demands triage: read the log, decide flake or real, re-run or
repair. At 27 failures in 18 days that is a recurring interruption whose main
product is the conclusion "not my commit". Worse, it is corrosive — a lane that
is red a quarter of the time trains its readers to assume red means nothing,
which is precisely the state in which a real regression walks through.

### What is not established

* **Whether any of the 28 are hiding a genuine product race.** This codebase is
  concurrent in `relay`, `session`, `ws`, and `acpagent`, and a real
  intermittent product bug is indistinguishable, from a CI log, from a flaky
  test. Nothing here proves all 27 failures were test defects.
* **Two runs failed 4+ tests at once** (`33084467102`, `33099993994`). That
  shape suggests one environment or toolchain failure rather than independent
  flakes, and it is not diagnosed.

## Decision Drivers

* **Red must mean broken.** The signal is the asset; a 23.5 % false-positive
  rate is spending it.
* **Do not hide a real intermittent defect.** Any mechanism that makes flakes
  invisible also makes a genuine product race invisible. Whatever is adopted
  must leave the rate *measurable*, not merely tolerable.
* **Bounded, known cost.** A remedy must not require diagnosing 28 tests before
  it delivers anything.
* **A genuinely broken commit must still fail.** The one real breakage in the
  window (`14e55d2`) was deterministic; whatever is adopted must still stop it.
* **Proportionate to a solo repository.** No quarantine dashboards or
  infrastructure needing an owner who does not exist.

## Considered Options

1. **Retry the failed job once automatically, and record every retry to a
   ledger.**
2. **Quarantine known-flaky tests into a separate non-blocking lane.**
3. **Keep repairing per instance** (status quo).
4. **Ban the nondeterministic test shapes and convert all 28** — fake clocks,
   explicit synchronisation, no induce-then-assert.

## Decision Outcome

Chosen option: **"Retry the failed job once automatically, and record every
retry to a ledger"**, because it is the only option that meets the knock-out
driver — *do not hide a real intermittent defect* — while delivering
immediately and without triaging 28 tests first.

The retry alone would fail that driver: it is exactly the mechanism for making
flakes invisible. The **ledger is not a nice-to-have, it is the half that makes
the retry admissible.** Every automatic retry appends the run id, job, commit,
and the failing test to a file in the repository. A flake stops interrupting a
human and starts accruing a count instead. That converts an invisible 23.5 %
into a number that can be read, and it gives the thing this record cannot
currently supply: per-test evidence about which of the 28 are worth repairing,
and which are one test masking a real product race.

Option 4 is the correct end state and this record does not argue against it —
it argues against doing it first, blind. After a few weeks of ledger the
conversion work can be aimed at the tests that actually fail, in order, instead
of at all 28 on the assumption that each is equally guilty.

**This decision is `accepted` as of Phase 1 (2026-09-05).** The owner answered
the three open parameters in the plan's Phase 1 execution record: retry the
platform legs carrying most failures (wired on the shared go-native `Test`
step for windows/amd64 + linux/arm64; tag job deferred to Phase 3); whole-step
retry via pinned `nick-fields/retry` v4.0.0; committed `ci-flakes.tsv` appended
by a batch job rather than a commit-per-retry. Phase 2 (2026-09-06) lands the
ledger file, capture/emit/append scripts, and `.github/workflows/ci-flake-ledger.yml`.

### Consequences

* Good, because the false-positive rate a reader sees drops to the probability
  of the *same* flake twice in a row. At the observed per-run rate that turns
  roughly 23.5 % red into low single digits.
* Good, because the flake rate becomes a measured series instead of an
  impression. "One in four" in this record was reconstructed by hand from the
  Actions API; after this it is a file.
* Good, because it needs no per-test decisions to start working, so the 28 tests
  do not have to be triaged before the lane becomes readable.
* Good, because a deterministic breakage still fails: it fails the retry too.
  The one real breakage in the window would have cost two failed jobs and still
  gone red.
* Bad, because a genuine product race that fails ~50 % of the time will pass on
  retry about half the time and be recorded as a flake. The ledger is what makes
  this recoverable rather than permanent — a rising count against one test is
  the signal — but between the race appearing and someone reading the ledger,
  a real defect is being retried away. This is the central cost and it is
  accepted knowingly.
* Bad, because retrying costs runner minutes on every red run, and a job that
  fails deterministically now burns twice before reporting.
* Bad, because a ledger nobody reads is worse than no ledger: it converts a
  loud problem into a quiet one and supplies an alibi. The Confirmation below
  exists because of this, and it is the part most likely to rot.
* Neutral, because no test changes and no production code changes. The 28 stay
  exactly as nondeterministic as they are today; this record changes what CI
  *does* about them, not what they are.

### Confirmation

* **The ledger file exists and grows.** After the first retry, `ci-flakes.tsv`
  (path decided in the plan) contains a row per automatic retry with run id,
  job, commit, and failing test.
* **The rate is re-measured, not assumed.** Thirty days after adoption, recount
  the failure rate over completed runs the same way this record did, and record
  it as an amendment. The number goes down, or this decision is wrong and says
  so in its own record.
* **A per-test threshold promotes a flake to a bug.** Any single test appearing
  in the ledger **3 or more times in 30 days** gets a MADR of its own, on the
  standing suspicion that it is a product race rather than a flaky test. On the
  window measured here, `collaboration_test.go` alone would trip this — which
  is the point.
* **The retry is visible in the run.** A retried job is distinguishable in the
  Actions UI from one that passed first time, so "green" never silently means
  "green on the second go".

## Pros and Cons of the Options

### Retry the failed job once, and record every retry to a ledger

* Good, because it delivers on the first red run, with no per-test triage.
* Good, because the ledger keeps the rate measurable, which is the only reason
  a retry is defensible at all.
* Good, because it is a `ci.yml` change plus a small script — proportionate to
  a solo repository.
* Bad, because it retries real intermittent defects away until someone reads
  the ledger.
* Bad, because it doubles runner cost on genuinely broken commits.

### Quarantine known-flaky tests into a non-blocking lane

* Good, because the blocking lane becomes green and stays green.
* Good, because quarantine is an explicit, reviewable list rather than a blanket
  policy.
* Bad, because it requires deciding, up front, which of 28 tests are flaky —
  the diagnosis this record cannot currently make.
* Bad, because a quarantined test is a test nobody runs. Its coverage is gone
  while its file still exists, which is worse than deleting it: the repository
  claims a guarantee it is no longer checking.
* Bad, because quarantine lists are famously one-way. Without an owner driving
  reinstatement — and this is a solo repository — the list only grows.

### Keep repairing per instance (status quo)

* Good, because every repair is a real improvement and leaves the suite
  genuinely better; 0111, 0118, 0119 and 0133 P7 are all sound work.
* Good, because it never hides anything.
* Neutral, because it is what has been happening, so its cost is known exactly:
  15 commits and 4 decision records in 18 days.
* Bad, because it has not moved the rate, and `collaboration_test.go` re-flaked
  three times after being fixed. Diligence is not the missing ingredient.
* Bad, because it scales with the number of timing-sensitive tests, which grows
  as the product does.

### Ban the nondeterministic shapes and convert all 28

* Good, because it is the actual root cause, and the only option that ends the
  problem rather than managing it.
* Good, because determinism-by-construction stops the *next* such test being
  written, which no other option does.
* Bad, because it is a large, open-ended programme priced before any evidence
  about which tests actually fail — and some of the 28 may have failed once for
  an environmental reason and be perfectly sound.
* Bad, because it delivers nothing until a substantial part of it is done, while
  the lane stays 23.5 % red throughout.
* Neutral, because adopting the retry does not preclude it. The ledger is what
  makes it targetable later, which is the argument for sequencing this option
  second rather than rejecting it.

## More Information

* Prior records for this class, each correct and each insufficient alone:
  [0111-MADR-stabilize-asynchronous-receipt-test-teardown.md](0111-MADR-stabilize-asynchronous-receipt-test-teardown.md),
  [0118-MADR-symlink-dependent-tests-on-unprivileged-windows.md](0118-MADR-symlink-dependent-tests-on-unprivileged-windows.md),
  [0119-MADR-codex-tests-fail-on-the-linux-arm64-lane.md](0119-MADR-codex-tests-fail-on-the-linux-arm64-lane.md).
* The most recent instance, and the shape of a per-instance repair:
  [0133-PLAN-recovery-must-not-wedge-on-a-transient-observation.md](0133-PLAN-recovery-must-not-wedge-on-a-transient-observation.md),
  Phase 7 and the 2026-09-05 deviation.
* Evidence that a targeted fix did not hold: `2876467` (2026-08-29T01:34Z) against
  runs `33227671357`, `33642941779`, `33648231957`.
* The one genuine breakage in the window, for contrast with the 26 others:
  `14e55d2`, AGP 9 renaming `merged_manifests` to `merged_manifest`.
* Workflow under discussion: `.github/workflows/ci.yml`.
* Implementation:
  [0143-PLAN-ci-retries-once-and-records-every-retry.md](0143-PLAN-ci-retries-once-and-records-every-retry.md).

## Amendment — 2026-09-06 (Phase 2 ledger)

Phase 2 implements Confirmation criterion 1's prerequisite: the ledger file
exists at repository root as `ci-flakes.tsv` (header only until the first
retry is ingested). Rows are produced by the go-native retry path and committed
by a separate workflow with `contents: write`, so the test job itself never
pushes. Visibility beyond `warning_on_retry`: job step summary table and a
`::notice` annotation naming `failing_test` and `retry_result`.

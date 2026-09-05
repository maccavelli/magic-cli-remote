---
status: draft
date: 2026-09-05
associated-madr: "0143-MADR-ci-retries-once-and-records-every-retry.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Implement: CI retries a failed job once and records every retry

Associated MADR: [0143-MADR-ci-retries-once-and-records-every-retry.md](0143-MADR-ci-retries-once-and-records-every-retry.md).

## Goal

A job that fails in a way a re-run would pass costs a ledger row instead of a
human triage, and the flake rate stops being an impression and becomes a file
that can be counted.

Done is: a red run means the commit is broken; every automatic retry is
recorded with enough detail to identify the test; and the 30-day recount in the
MADR's Confirmation can be performed from the repository rather than
reconstructed from the Actions API by hand.

## Scope

**In scope.** `.github/workflows/ci.yml`; a retry mechanism for the Go and
Flutter test jobs; a ledger file and the script that appends to it; the
threshold rule that promotes a repeat offender to its own MADR.

**Out of scope, deliberately.**

* **Repairing any of the 28 tests.** This plan changes what CI does about
  nondeterminism, not the nondeterminism. Converting tests to
  determinism-by-construction is the MADR's option 4 and is sequenced after the
  ledger has said which tests to aim at.
* **The two multi-test failures** (`33084467102`, `33099993994`). They look
  like one environment failure rather than independent flakes and are
  undiagnosed; the ledger will characterise them.
* **Quarantine.** Rejected in the MADR.
* **Publish, Smoke, and Android jobs.** They are tag-gated and their three
  failures in the window were one deterministic bug, since fixed. Retrying a
  publish step has different and worse failure modes than retrying a test job,
  and it is not being decided here.

## Prerequisites and Dependencies

* **An owner decision on three open parameters** — see "Decisions still needed"
  below. Phase 1 cannot start without them.
* No new credentials or secrets. The retry runs inside the existing workflow
  with the default `GITHUB_TOKEN`.
* If the ledger is a committed file (open question 3), the workflow needs
  `contents: write` and a push from CI, which the repository does not currently
  do from any workflow.

### Decisions still needed

1. **Scope of the retry.** All test jobs, or only the three platform legs that
   produce 23 of the 27 failures (`windows/amd64`, `linux/arm64`, `test; build
   on tag`)? Narrower is cheaper and covers 85 %.
2. **Granularity.** Retry the whole job, or re-run only the failed Go packages?
   Whole-job is trivial and honest; package-level is faster but needs the
   workflow to parse test output, which is a second thing to get wrong.
3. **Where the ledger lives.** A committed `ci-flakes.tsv` (durable, greppable,
   but CI pushes to `master` and every retry becomes a commit) or a workflow
   artifact aggregated on demand (no commits, but retention-limited and not
   greppable from a checkout). **Recommendation: committed file, appended by a
   scheduled job that batches rows, so a retry does not produce a commit per
   flake.**

## Technical Design

**Retry.** GitHub Actions has no first-class per-job retry. Two mechanisms are
available and the choice belongs to Phase 1:

* `nick-fields/retry` (or equivalent) wrapped around the test *step*. Keeps the
  retry inside one job, so the ledger writer sees both attempts. Adds a
  third-party action to a workflow that currently uses only first-party ones —
  a supply-chain consideration this repository has otherwise avoided.
* A `re-run failed jobs` call via `gh run rerun --failed` from a follow-on job.
  No third-party action, but the re-run is a separate run, so "did this pass on
  retry" has to be reconstructed across two run ids.

The first is simpler to record against; the second is cleaner on dependencies.
This plan does not pick — Phase 1 does, with the trade named in the commit.

**Ledger schema.** One row per automatic retry, tab-separated:

```text
run_id  job_name  commit_sha  first_attempt_result  retry_result  failing_test  timestamp
```

`failing_test` is extracted from the first attempt's output by matching
`^--- FAIL: (\S+)`; when a job fails without one — a build, vet, or
infrastructure failure — the field records the failing step name instead, so
the two multi-test runs and the AGP-style breakages remain distinguishable from
test flakes.

**Threshold.** A test appearing 3+ times in 30 days is promoted to its own MADR
per the Confirmation. Implemented as a query over the ledger, run by the same
scheduled job that batches rows, emitting a warning annotation rather than
failing anything.

## Execution Phases

### Phase 1 — decide the mechanism, retry one job

Answer the three open questions in a short amendment to this plan, then wire
the retry into **one** job (`Go (windows/amd64)` — the largest single bucket at
9 failures). No ledger yet.

*Exit criterion.* A deliberately flaky test, pushed on a scratch branch, is
observed failing once and passing on the automatic retry; and a deliberately
broken test is observed failing both attempts and reporting red.

Both halves are required. The second is what proves the retry has not simply
disabled the job.

### Phase 2 — the ledger

Add the extraction script and the ledger file, still on the one job. Rows must
appear for the Phase 1 scratch-branch flake.

*Exit criterion.* `ci-flakes.tsv` gains a correct row, including
`failing_test`, for an induced flake; and a build-level failure records the
step name rather than an empty field.

### Phase 3 — extend to the remaining test jobs

Roll the mechanism to `Go (linux/arm64)`, `Go (test; build on tag)`, and the
two Flutter jobs.

*Exit criterion.* All five test jobs retry and record. A full green run shows no
ledger rows — the ledger must stay empty when nothing flakes, or it is
recording the wrong thing.

### Phase 4 — the threshold query

Add the 3-in-30-days query and its warning annotation.

*Exit criterion.* Run against the ledger backfilled with this record's measured
window, the query names `internal/provider/codex/collaboration_test.go`. That
file failed 5 times, 3 of them after being fixed, so a threshold rule that does
not surface it is mis-tuned.

### Phase 5 — re-measure, and say so

Thirty days after Phase 3 lands, recount the failure rate over completed runs
by the same method as the MADR, and record it as an amendment to the MADR.

*Exit criterion.* The number is written down whichever way it went. If the rate
has not fallen, the amendment says the decision was wrong; this phase is not
complete merely because it was performed.

## Verification

```bash
# Ledger rows are well-formed: 7 tab-separated fields, no empty failing_test
awk -F'\t' 'NF!=7 || $6=="" {print "malformed: "NR": "$0}' ci-flakes.tsv

# Re-measure the failure rate the same way the MADR did
gh run list --limit 200 --json conclusion,headBranch,createdAt
```

Per-phase, the existing gates still apply and must stay clean: `make
pre-add-check`, `make vet`, `make lint`, `go test ./... -count=1`.

### Acceptance criteria

* A red run means the commit is broken — verified by the Phase 1 exit criterion,
  not by assumption.
* Every automatic retry has a ledger row naming the test or the step.
* The threshold query surfaces `collaboration_test.go` on the backfilled window.
* The 30-day recount is recorded in the MADR, in whichever direction it went.

## Rollback

Per phase, revert the `ci.yml` change; the workflow returns to failing on the
first failure. Nothing in the retry mechanism touches test or production code,
so there is no state to unwind and no migration to reverse. The ledger file can
be left in place — it is inert once nothing appends to it, and its rows remain
the evidence for whatever is decided next.

**Trigger to roll back:** any run where the retry masks a failure that should
have gone red. That is the failure mode the MADR names as its central cost, and
observing it once is grounds to stop and re-decide rather than to tune.

## Task Checklist

**Phase 1 — mechanism**

* [ ] Owner answers open questions 1-3; amend this plan with the answers
* [ ] Choose retry mechanism, naming the dependency trade in the commit message
* [ ] Wire retry into `Go (windows/amd64)`
* [ ] Scratch branch: induced flake passes on retry
* [ ] Scratch branch: deliberately broken test fails both attempts and reports red

**Phase 2 — ledger**

* [ ] Extraction script (`^--- FAIL:` match, step-name fallback)
* [ ] `ci-flakes.tsv` and its append path
* [ ] Row verified correct for an induced flake
* [ ] Build-level failure records the step name

**Phase 3 — extend**

* [ ] `Go (linux/arm64)`
* [ ] `Go (test; build on tag)`
* [ ] `Flutter (analyze & test)`, `Flutter (test on windows)`
* [ ] A fully green run produces no ledger rows

**Phase 4 — threshold**

* [ ] 3-in-30-days query with warning annotation
* [ ] Backfill the measured window; confirm it names `collaboration_test.go`

**Phase 5 — re-measure**

* [ ] Recount the failure rate 30 days after Phase 3
* [ ] Record the result in the MADR as an amendment, whichever way it went

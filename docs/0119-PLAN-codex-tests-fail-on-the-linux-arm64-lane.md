---
status: completed
date: 2026-08-28
associated-madr: "0119-MADR-codex-tests-fail-on-the-linux-arm64-lane.md"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0119 — Establish causation, then repair the codex arm64 failures

Implements [0119-MADR-codex-tests-fail-on-the-linux-arm64-lane.md](0119-MADR-codex-tests-fail-on-the-linux-arm64-lane.md)
decisions D1–D7, closing findings F1–F6.

## Goal

`master` is green on `Go (linux/arm64)`, and the reason it is green is known
rather than assumed.

Finish line:

* the arm64 lane passes five consecutive runs on the fixed `master`;
* both `TestDiagnosticRunnerExactArgvTimeoutNonzeroAndSingleFlight` and
  `TestUnrelatedInitializeErrorDoesNotRetry` run on that lane — neither skipped,
  neither retried, neither gated;
* this record states, with run IDs, whether `fb0f361` was causal;
* 0118's PLAN status is resolved in whichever direction the evidence points.

## Scope

### In scope

* `internal/provider/codex/diagnostics_p4_test.go` — the singleflight barrier
* `internal/provider/codex/collaboration_test.go` — `launchCount` and the
  launch-count assertions that depend on it
* `docs/0118-PLAN-*.md` — status line only, at P5
* Throwaway experiment branches, deleted at P4

### Out of scope

* **Any production code.** As with 0118, if a phase edits a non-`_test.go` file
  it has left scope. If the experiment shows the *product* has a singleflight or
  launch defect rather than the tests having a race, that is a new record, not a
  widening of this one.
* Changing `.github/workflows/ci.yml`, except the temporary experiment branches
  at P1 which are never merged.
* Reverting `fb0f361` — permitted only by D6, only on P1 evidence, and only with
  the owner's approval in the same turn.
* A package-wide race sweep (MADR open question 4).
* The tree-wide CRLF issue 0118 defers. Still unrelated, still needs its own
  record.

## Stability rule

Every phase ends with:

```bash
gofmt -l internal/provider/codex        # must print nothing
go vet ./internal/provider/codex/
go test -count=20 ./internal/provider/codex/
```

then **one commit** (`git commit --no-edit`; never `-m`).

`git push` is required by this plan — P1 and P4 cannot be performed without it,
since the failure only reproduces on a GitHub ARM runner. **Each push needs an
explicit instruction in the same turn** (AGENTS.md). A phase that cannot get one
stops and says so rather than proceeding locally and claiming the result.

## Cross-cutting contracts

**C1 — No production code.** Tests only.

**C2 — No test is weakened to pass.** No skip, no `testing.Short()`, no retry
wrapper, no loosened assertion (`<= 1`, `!= 0`), no lengthened sleep standing in
for a barrier. A fix must make the test *deterministic*, not *lucky*. This is
MADR D5, and it is the contract most at risk once the lane has been red for a
while.

**C3 — Every claim about the lane carries `k of n` and run IDs.** "It passed" is
not a result (MADR D3).

**C4 — The two failing tests keep testing what they test.** The singleflight
test must still prove coalescing; the retry test must still prove a single
launch. A barrier that makes the assertion trivially true is C2 in disguise.

## Implementation Steps

### P1 — The isolation experiment (D1, D2, D3; closes F2, F6)

**This phase writes no fix.** It produces a table.

Four arms, each a branch pushed to trigger the arm64 lane, each run **five
times** (`gh run rerun --job <id>` on the lane, which is under a minute):

| Arm | Branch content | F3 predicts |
| --- | --- | --- |
| A — baseline | `b92c4e7` unmodified | green 5/5 |
| B — tests only | `b92c4e7` + the five non-`ci.yml` files from `fb0f361` (the `testexec` helper plus four test files) | green 5/5 |
| C — CI env only | `b92c4e7` + the `ci.yml` `MC_REQUIRE_SYMLINK` lines | green 5/5 |
| D — current | `88171bc` unmodified | red, rate unknown |

Arm A already has one green from 2026-08-28 and four historical; it still gets
its five so the arms are comparable.

To keep each arm cheap, restrict the lane to the one package for the experiment
only — `go test ./internal/provider/codex/` in place of `go test ./...` on the
experiment branches. This is a change to a throwaway branch, never to `master`.

**Reading the result.**

* D red at a high rate, A/B/C green → `fb0f361` is implicated and the mechanism
  is in B or C; whichever arm reddens is the answer.
* D red, B and C green, A green → the commit as a whole changes something its
  halves do not, which would be surprising. Do not proceed to P2 on a shrug;
  record it and escalate to the owner.
* **All four arms show failures at some rate** → this is pre-existing flakiness
  and the `b92c4e7` greens were sampling luck. F5 predicts this. 0118 is
  cleared, and P2/P3 are the whole of the remaining work.
* D green 5/5 → the two reds were a coincidence and F2 was right to withhold
  judgement. Fix the races anyway (D4) and close.

**Deliverable:** the table above, filled in with rates and run IDs, appended to
this plan. No code fix in this phase.

### P2 — The singleflight barrier (D4; closes half of F4)

`diagnostics_p4_test.go`. The test releases the first caller once `entered`
closes, but nothing proves the second has joined the group. Add the missing
barrier so both callers are provably in flight before either is released.

The shape that satisfies C4: the second goroutine must be observable as having
*joined* — not merely started — before `release` closes. If singleflight offers
no such observation point, the honest fix is to make the probe function block
until it has seen both callers arrive at the group, and fail on timeout rather
than proceeding.

What is **not** acceptable (C2): a `time.Sleep` before `close(release)`, or
relaxing `calls.Load() != 1`.

**Verification:** `go test -count=200 -run TestDiagnosticRunner ./internal/provider/codex/`
green locally; and on a machine under load (`-cpu=1` narrows the scheduler,
which is closer to the failing condition than the default).

### P3 — The launch-count race (D4; closes the rest of F4)

`collaboration_test.go`. Two defects, and they need separating:

1. `launchCount` maps "file does not exist" to `0` (`:350`). An absent log and a
   log with zero lines are different states; conflating them is what lets a
   premature read masquerade as a count.
2. The test reads the count without any guarantee the helper subprocess has
   written it.

Fix 1 first — make the absent case distinguishable — because it is what turns
the race from a silent wrong answer into a legible one. Then fix 2 with a real
wait for the child's write, bounded and failing on timeout.

Check the other `launchCount` call sites while here — there are **seven**
(`collaboration_test.go:169, 198, 216, 238, 239, 323, 324`), and only one of
them is the test that failed. The same premature-read window exists wherever the
count is read after a fast failure, and fixing one caller while leaving six
siblings is how this recurs. Note `:239` and `:324` call `launchCount` a second
time inside the failure message, so a racing count can even be reported
inconsistently with the condition that triggered it.

**Verification:** `go test -count=200 -run 'TestUnrelated|TestInitializeTransport' ./internal/provider/codex/`.

### P4 — Prove it on the lane (D3, D6; closes F1)

**Needs an explicit push instruction in the same turn.**

Push the fixes. Re-run the arm64 lane five times. Green 5/5 closes the plan.

If it is not 5/5, P2/P3 did not address the cause and the plan returns to P1's
table rather than trying a third fix on instinct.

Delete the four experiment branches from P1.

### P5 — Resolve 0118 (D7)

With the lane green, update `docs/0118-PLAN-*.md`: its P4 is satisfied, and the
execution record gains a line saying its arm64 red was 0119's, with the verdict
P1 reached. Status moves off `in-progress`.

If P1 indicted `fb0f361`, this phase instead records that, and 0118's status
reflects a fix that was reverted or amended rather than completed.

## Verification (whole plan)

```bash
gofmt -l internal/provider/codex               # empty
go vet ./internal/provider/codex/
go test -count=50 ./internal/provider/codex/   # green on the dev host
```

```text
arm64 lane on fixed master → 5 of 5 green, run IDs recorded
both named tests           → present in the log as run, not skipped
0118 PLAN                  → status resolved
```

**Not verifiable on this host:** the failure itself. It has never reproduced on
windows/amd64 (10 of 10 green), which is why P1 and P4 use CI and why neither
may be replaced by a local run.

### Acceptance criteria

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | P1's table is filled in with rates and run IDs for all four arms | D1, D2, D3 |
| A2 | The causal verdict on `fb0f361` is stated, either way | D2, F2 |
| A3 | Both races are fixed deterministically, not by timing | D4, C2 |
| A4 | Both tests still prove what they proved before | C4 |
| A5 | arm64 lane green 5 of 5 on fixed `master` | D6, F1 |
| A6 | Neither test is skipped, gated, or retried on any lane | D5, C2 |
| A7 | No production file changed | C1 |
| A8 | 0118's PLAN status resolved | D7 |

A6 is the one to guard. Once a lane has been red for a day, gating the noisy
test becomes very attractive, and it is exactly the move that would make the
next regression invisible.

## Rollout and Rollback

Test-only; nothing a user observes. Each phase reverts independently. P1 leaves
nothing behind once its branches are deleted.

## Deferred (named, so they are not mistaken for oversights)

* **A package-wide race sweep** — MADR open question 4. Two races found by
  accident in one package is weak evidence that there are more, and weak
  evidence is still evidence. Its own record.
* **Whether `go test ./...` on the native lanes should use `-count=1`** — the
  lanes rely on `setup-go` caching, and this record proved caching was *not*
  masking the arm64 greens (job 98621856477). But a flaky package plus cached
  results is a bad combination in general, and it is a CI-policy question rather
  than a test-repair one.
* **The tree-wide CRLF issue** — still deferred, still unrelated, still 0118's
  last bullet.

## Execution record — 2026-08-28

**P1 not run. P4 pending (needs push). P5 pending (needs P4).** Owner
accepted the MADR and ordered execution in the same turn as 0122, without
a push instruction.

P1 is skipped, not silently dropped. The MADR amendment of the same date
fills F6 from later `master` runs (arm64 3 red / 2 green after 0118;
docs-only commits still flake; `fb0f361` not a deterministic cause). That
is the "all four arms fail at some rate / D is not 100% red" reading, which
the PLAN already said sends the remaining work to P2/P3 and clears 0118 of
causation.

A1 is therefore satisfied by the amendment table rather than by throwaway
branches. A2's verdict: **`fb0f361` was not causal.** A5/A8 wait on P4.

P2 and P3 proceed. `gofmt -l internal/provider/codex` is scoped to the
touched files when the working tree is CRLF (0118 deferral), matching how
0118 itself substituted.

### P2 — `0ebe50c`

`TestDiagnosticRunnerExactArgvTimeoutNonzeroAndSingleFlight` starts the
second `RunDoctor` only after the first has entered `doctorRun`, then
waits for two `RunDoctor` stack frames before `close(release)`. A second
`doctorRun` call is itself a failure. `calls == 1` is unchanged.

Verification: `go test -count=200 -run TestDiagnosticRunner ./internal/provider/codex/`
and `-cpu=1 -count=50` of the same, plus `go test -count=20 ./internal/provider/codex/`,
all green. `make pre-add-check FILES="internal/provider/codex/diagnostics_p4_test.go"`
clean (vuln DB unreachable, skipped).

### P3 — `2876467`

`launchCount` no longer maps a missing file to 0. `waitLaunchCount` polls
until the log exists with the wanted line count, and the timeout names
"never appeared" vs a wrong count. All five former call sites (including
the two that read the count twice in a `Fatalf`) go through it.

Verification: `go test -count=200 -run 'TestUnrelated|TestInitializeTransport' ./internal/provider/codex/`
and `go test -count=20 ./internal/provider/codex/`, both green.
`make pre-add-check FILES="internal/provider/codex/collaboration_test.go"`
clean.

P4 still needs an explicit push to observe the arm64 lane 5/5. P5 waits
on that. 0118 PLAN stays `in-progress`.

### P4 — run 33227227673 on `58b0374` (5 of 5)

Owner said `push` on 2026-08-28. `294aee6..58b0374` went to `origin/master`.
CI #390 (`https://github.com/maccavelli/magic-cli-remote/actions/runs/33227227673`)
is the observation. The arm64 job was then rerun in place through attempt 5:

| Attempt | Job ID | Result |
| --- | --- | --- |
| 1 | 99033191160 | success |
| 2 | 99033763726 | success |
| 3 | 99033891030 | success |
| 4 | 99033997648 | success |
| 5 | 99034131156 | success |

**5 of 5 green.** Attempt 1 of the same run also completed the whole workflow
as `success` (Ubuntu Go, Flutter, windows/amd64).

`go test ./...` is not `-v`, so individual test names do not appear. Evidence
the two tests ran rather than skipped: `internal/provider/codex` on attempt 1
is `ok … 1.801s` (not cached), the log has no `SKIP` lines, and neither
test calls `t.Skip`. A skipped-only package would still print `ok`, but
these tests have no skip path.

A5 holds. A6 holds on the evidence above, with the `-v` gap named.

### P5 — 0118 resolved

0118's remaining P4 was Windows CI observation of the symlink tests; that
lane is green on this SHA with `MC_REQUIRE_SYMLINK=1`. Its arm64 redness
after `fb0f361` was this record's, and the verdict is that `fb0f361` was
not causal. 0118 PLAN status moves to `completed`.

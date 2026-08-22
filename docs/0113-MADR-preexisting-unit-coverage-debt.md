---
status: proposed
date: 2026-08-22
decision-makers: Project Owner
consulted: none
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Close pre-existing unit-coverage debt as its own tracked work

## Context and Problem Statement

[0112](./0112-MADR-opencode-1.18.21-surface-parity.md) A13 originally carried
two different obligations under one rule:

1. every production change in the OpenCode parity plan ships with its own unit
   and widget tests, and does not make coverage worse; and
2. the repository's **pre-existing** sub-80% packages and Dart files are raised
   to a hard floor before that parity work begins.

The second obligation is not caused by, and does not depend on, the OpenCode
1.18.21 parity work. It predates it, it touches packages the parity plan barely
edits, and — measured in P0 of that plan — it is larger than several parity
phases combined. Bundling the two meant no parity capability could ship until
roughly a thousand statements of unrelated legacy test debt had been closed.

P0 of 0112 measured the debt exactly and committed the tooling that quantifies
it, so the debt is now a well-specified, independently executable body of work
rather than an estimate. This record asks: **should pre-existing coverage debt
be closed as a precondition of the parity plan, or tracked and executed as its
own change set?**

## Decision Drivers

* Coverage debt that predates a feature is not that feature's blocker. Coupling
  them delays capability without improving the tests that the capability
  actually needs.
* The obligation that genuinely belongs to a feature is that *its own* new and
  changed code is tested, and that it does not degrade what already exists.
* The debt is real and should not be silently dropped. Separating it must make
  it more visible, not less.
* The measurement tooling, floors, and vocabulary already exist and are
  committed; a separate plan should reuse them rather than reinvent them.
* An absolute floor enforced in CI is only credible once the tree actually meets
  it. Adding a failing required job before the debt is closed would either block
  every unrelated change or be disabled and forgotten.

## Considered Options

* Option 1: Track pre-existing debt in its own MADR/PLAN pair, and leave 0112
  responsible only for the tests its own changes require (chosen)
* Option 2: Keep the debt inside 0112 as its blocking P0A phase
* Option 3: Drop the absolute floors and enforce only per-change non-regression

## Decision Outcome

Chosen option: **"Option 1: Track pre-existing debt in its own MADR/PLAN pair,
and leave 0112 responsible only for the tests its own changes require"**,
because it separates two obligations with different causes, different owners and
very different sizes, while keeping both enforced.

### Locked decisions

| ID | Decision |
| --- | --- |
| **E1** | Pre-existing sub-floor coverage is this record's scope. 0112 retains only the obligations created by its own changes: new and changed production code ships with tests in the same commit, every new Dart production file reaches 90.0% line coverage, and no touched target regresses. |
| **E2** | The hard floor is 80.0% statement coverage for each Go package listed below, 80.0% line coverage for each existing Dart production file listed below, and 80.0% for the Flutter application in aggregate. `internal/provider/opencode` additionally reaches 85.0%. New Dart production files created by any plan reach 90.0%; that rule already lives in 0112 and is not duplicated here. |
| **E3** | Close each target to at least 82.0% rather than exactly 80.0%. The two-point margin means an unrelated future change cannot drop a target below the floor by a single statement, which is what makes a required CI job survivable. |
| **E4** | This is test-only work. No production file changes: no new seams, no exported symbols added for testability, no refactors, no dead-code deletion, no build-tag tricks and no coverage exclusions. If a named scenario cannot be reached without a production change, that is a finding to record and get approved — not a licence to change production code opportunistically. |
| **E5** | `make coverage-check` and a required, non-`continue-on-error` CI job enforcing the absolute floors are added by **this** plan, in the change set that first makes the tree satisfy them. 0112 continues to use per-phase `scripts/coverage-delta.sh phase` non-regression checks, which need no absolute floor. |
| **E6** | Reuse 0112 P0's committed tooling and baseline verbatim: `scripts/coverage-snapshot.sh`, `scripts/coverage-delta.sh`, its fixture suite, and `internal/provider/opencode/testdata/surface-1.18.21/unit-coverage-baseline.json`. Do not fork or re-derive them. |
| **E7** | Three packages — `internal/daemon`, `internal/session` and `internal/ws` — have counts that vary by a few concurrency-dependent statements between runs. They are compared same-run (before/after in one session), never against a number quoted in prose. |
| **E8** | Tests assert behavior. A test that raises a counter without asserting an outcome does not satisfy this record, and every target group is verified with `go tool cover -func` or LCOV inspection to confirm the intended functions actually moved. |

### Measured debt

Captured in 0112 P0 on 2026-08-22 under default build tags, before any
production edit. "Add" columns are exact additional covered statements or lines.

| Go package | Covered / total | Exact | Add for 80% | Add for 82% |
| --- | ---: | ---: | ---: | ---: |
| `internal/provider` | 79 / 161 | 49.0683% | 50 | 54 |
| `internal/provider/opencode` | 1,383 / 1,659 | 83.3635% | 0 | 0; E2 requires 85% |
| `internal/provider/httpagent` | 737 / 1,470 | 50.1361% | 439 | 469 |
| `internal/event` | 4 / 6 | 66.6667% | 1 | 1 |
| `internal/picker` | 164 / 191 | 85.8639% | 0 | 0 |
| `internal/protocol` | 11 / 25 | 44.0000% | 9 | 10 |
| `internal/session` | 1,403 / 1,852 | 75.7559% | 79 | 116 |
| `internal/ws` | 1,153 / 1,593 | 72.3792% | 122 | 154 |
| `internal/config` | 483 / 615 | 78.5366% | 9 | 22 |
| `internal/daemon` | 246 / 362 | 67.9558% | 44 | 51 |
| `internal/cli/service` | 876 / 1,268 | 69.0852% | 139 | 164 |

`internal/provider/opencode` needs 28 statements to reach 85.0% at the 1,659
denominator. That denominator moves as 0112 adds production code, so the
requirement is recomputed from raw counts rather than fixed at 28.

Flutter covered 9,061 / 11,789 production lines (76.8598%) over 1,041 passing
tests with 3 skipped: 371 lines to 80%, 606 to the 82% target.

| Existing Dart file | Covered / total | Exact | Add for 80% | Add for 82% |
| --- | ---: | ---: | ---: | ---: |
| `lib/data/ws/mcremote_client.dart` | 825 / 1,308 | 63.0734% | 222 | 248 |
| `lib/features/chat/chat_screen.dart` | 913 / 1,447 | 63.0961% | 245 | 274 |
| `lib/features/sessions/sessions_screen.dart` | 529 / 749 | 70.6275% | 71 | 86 |
| `lib/data/protocol/models.dart` | 447 / 590 | 75.7627% | 25 | 37 |

Other legacy Dart files below 80% are outside this record and are not declared
compliant. They are listed as future work in the companion plan.

## Consequences

* Good, because parity capability ships on its own merits and its own tests,
  instead of waiting on unrelated legacy debt.
* Good, because the debt becomes a named, measured, independently reviewable
  change set rather than an invisible tax on the next feature.
* Good, because a required CI floor is introduced only once the tree meets it,
  so it is credible instead of pre-disabled.
* Good, because the separation is honest about cause: `internal/cli/service`
  and `internal/daemon` coverage has nothing to do with OpenCode 1.18.21.
* Bad, because the tree stays below the absolute floor until this plan runs, so
  during that window only per-change non-regression is enforced.
* Bad, because two plans now touch overlapping test files and must be sequenced
  to avoid conflicts; the companion plan resolves this by rebasing onto whatever
  0112 has landed rather than running concurrently.
* Neutral, because the total work is unchanged — only its sequencing and
  attribution change.

## Pros and Cons of the Options

### Option 1: Separate MADR and PLAN (chosen)

* Good, because each obligation is owned where its cause lies.
* Good, because either can be scheduled, paused or reprioritized alone.
* Bad, because it needs this extra record pair and a sequencing rule.

### Option 2: Keep it inside 0112 as a blocking phase

* Good, because one plan reaches the floor and the parity work in one sweep.
* Bad, because roughly a thousand Go statements and six hundred Dart lines of
  unrelated legacy tests gate every parity capability.
* Bad, because a phase that large is where plans stall, taking the feature with
  it.

### Option 3: Drop the floors, keep only non-regression

* Good, because it requires no additional work at all.
* Bad, because packages at 44% and 49% stay there permanently, and
  non-regression silently blesses whatever the current number happens to be.
* Bad, because it discards a measurement that has already been made.

## Confirmation

* Every Go package and existing Dart file in the debt table, plus the Flutter
  application, reaches at least 82.0% using only deterministic default-tag unit
  and widget tests.
* `internal/provider/opencode` reaches at least 85.0%.
* No production file differs from its pre-plan content; the diff is tests plus
  the Make and CI gate only.
* `make coverage-check` rejects a 79.999% target and accepts exactly 80.0%,
  using the exact-integer comparison already proven by
  `scripts/coverage-delta_test.sh`.
* The CI job is required, is not `continue-on-error`, and cannot be satisfied by
  the existing Go or Flutter jobs passing independently.
* Live-tagged, token-bearing and loopback tests contribute nothing to these
  numbers.

## More Information

The debt figures, the tooling and the run-to-run variance note all come from
0112 P0, committed as
`internal/provider/opencode/testdata/surface-1.18.21/unit-coverage-baseline.json`.
That file is the reference; this record quotes it for readability only.

### Related

* [0112-MADR-opencode-1.18.21-surface-parity.md](./0112-MADR-opencode-1.18.21-surface-parity.md)
* [0113-PLAN-preexisting-unit-coverage-debt.md](./0113-PLAN-preexisting-unit-coverage-debt.md)

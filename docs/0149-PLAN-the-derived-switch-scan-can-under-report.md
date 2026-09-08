---
status: in-progress
date: 2026-09-07
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0149 — The derived switch scan can silently under-report

Implements [0149-MADR-the-derived-switch-scan-can-under-report.md](0149-MADR-the-derived-switch-scan-can-under-report.md)
decisions D1–D7, closing findings F1–F8.

## Goal

1. `switchDispatchedConstants` parses `handleMessage`'s switch with `go/ast`,
   and is correct for every legal `case` label form.
2. An arm is asynchronous because it *calls* `dispatchAsync`, not because a
   comment mentions it.
3. A case clause the scan cannot interpret is a named failure, not a panic and
   not a silent gap.
4. The reported set of async-dispatched types is **40**, differing from today's
   41 by exactly `TypeSessionCancel` — and by nothing else.
5. `session.cancel`'s now-exposed `op_timeouts.json` entry is **reported, not
   deleted**: P3 stops and hands the decision to the owner.

## Scope

### In scope (the only files any phase may touch)

| File | Phase | Why |
| --- | --- | --- |
| `internal/ws/op_timeout_test.go` | P1, P2 | the scan and its tests (D1–D4) |

### Out of scope

* **`internal/ws/server.go`.** The switch is correct Go; the scanner adapts to
  it, never the reverse (D5). Editing the `TypeSessionCancel` comment to dodge
  F8 would be the worst possible fix — it would hide the defect and leave the
  next comment to re-trigger it.
* **`internal/protocol/op_timeouts.json`.** Deleting `session.cancel` is a
  protocol-visible change to a table the phone reads (D7). P3 surfaces it and
  stops.
* **`codexDispatchedConstantsIn` and `internal/event/retention_test.go`.** They
  do not share the defect (D6).
* **The `TestSourceScansSurviveCRLF` guard** added by MADR 0147 P2. It must keep
  passing; an AST parse of a CRLF file is fine, but that is a claim to verify,
  not assume.

## Stability rule

Every phase ends with:

```bash
go build ./...
go test ./internal/ws/ -count=1
go test ./... && go test -race ./...
gofmt -l internal/ws/op_timeout_test.go
```

and on this Windows host, before any push:

```bash
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/ci-windows-local.ps1
```

One commit per phase. **`git push` needs an explicit instruction in the same
turn** — this plan does not authorise it.

## Cross-cutting contracts

**C1 — no production code changes.** Every edit lands in
`internal/ws/op_timeout_test.go`.

**C2 — the scan's output changes by exactly one entry.** Before: 41 types
including `TypeSessionCancel`. After: 40, without it. Any other difference means
the rewrite changed behaviour beyond the defect and must be understood before
proceeding.

**C3 — no test is weakened to reach green.** In particular, the
`TestEveryAsyncDispatchedMethodIsInTheTable` failure that P3 exposes is the
*correct* behaviour and must not be silenced by editing the table, relaxing the
check, or special-casing `session.cancel`.

**C4 — `TestSourceScansSurviveCRLF` keeps passing** (MADR 0147 D5 / 0148 D3).

**The contract most at risk is C3.** P3 ends with a red test and a question for
the owner. The whole momentum of a plan at that point is toward green, and
deleting one line from `op_timeouts.json` would do it in seconds. That deletion
is a protocol change to a table the phone reads, made to tidy a test — exactly
the inverted-error trap F4 describes, arrived at from the other direction.

## Dependency and delivery order

P1 → P2 → P3. P1 must land before P2 so the new tests are written against the
new parser rather than retrofitted to the old one. P3 is last because it is not
a change at all — it is the report of what P1 uncovers.

## Implementation Steps

### P1 — parse the switch as Go (D1, D2, D3; closes F1, F2, F3, F7, F8)

Replace the regexp walk in `switchDispatchedConstants` with an AST walk over the
body it is already given (MADR 0147 P2 made it take a body rather than a path,
which is why this is contained):

* `parser.ParseFile` the source; find the `FuncDecl` named `handleMessage`.
* Find the `SwitchStmt` whose tag is `env.Type`. **Measured: there is exactly
  one `switch` in `handleMessage` today**, so this is unambiguous — but key on
  the tag anyway, and fail loudly if zero or more than one matches, rather than
  taking the first.
* For each `CaseClause`, collect every `protocol.Type…` from `clause.List`
  (a `[]ast.Expr` — all label forms collapse to the same shape here, which is
  what closes F2 and F3).
* Decide asynchrony by walking `clause.Body` with `ast.Inspect` for a
  `CallExpr` whose function is `dispatchAsync`. **Measured: no arm dispatches
  inside a nested block today** — every call sits at the top level of its arm —
  but use `ast.Inspect` so an arm that later dispatches inside an `if` is not
  missed.
* A clause whose `List` yields no `protocol.Type…` selector is an error naming
  the clause and its `token.Position` (D3). Not a panic, not a skip.

The `"\n// asyncHandler is a slow WS op"` delimiter disappears with the text
scan, closing F7.

**Verification.** Print the resulting type list and diff it against the list the
old scan produced. Expect exactly one removal, `TypeSessionCancel`, and 40
remaining (C2). Capture both lists in the commit message — the count alone is
not evidence, because two compensating changes would also produce 40.

### P2 — prove every label form (D4; closes F1–F3 as regressions)

Table-driven tests over synthetic sources, one case per row, asserting the
captured constants:

| Input | Expect |
| --- | --- |
| `case protocol.TypeA:` + `dispatchAsync` | `[TypeA]` |
| `case protocol.TypeA, protocol.TypeB:` + `dispatchAsync` | `[TypeA TypeB]` |
| `case protocol.TypeA,` / `protocol.TypeB:` + `dispatchAsync` | `[TypeA TypeB]` |
| `case protocol.TypeA:` with a *comment* naming `dispatchAsync`, returning `s.handleX` | `[]` |
| `case someLocalConst:` | a named error, no panic |
| arm dispatching inside an `if` | `[TypeA]` |

The fourth row is F8 and the sixth is the `ast.Inspect` requirement; both would
pass trivially against a naive implementation of the other, so include both.

**Verification.** Each row fails against the pre-P1 implementation — check a few
by reverting locally — and passes after. `go test ./internal/ws/ -run
TestSwitchScan -count=1 -v`.

### P3 — report what the fix exposes, and stop (D7)

With F8 fixed, `TestEveryAsyncDispatchedMethodIsInTheTable` will report
`session.cancel` as an entry in `op_timeouts.json` that no longer reaches
`dispatchAsync`. **That report is correct.** `handleSessionCancel` is inline by
deliberate decision (MADR 0137 F4).

Do not resolve it here. Capture the failure output, state the question, and
stop:

> `op_timeouts.json` carries `session.cancel: 30000`. Cancel is handled inline
> (0137 F4), so it is not an async op and the table's own contract says it does
> not belong. But the table is shared with the phone, which must exceed every
> value — removing the entry changes what the phone budgets for a cancel.
> Delete the entry, or keep it and narrow the test's model of what the table
> covers?

**Verification.** The failure is reproduced and quoted; `op_timeouts.json` is
unmodified; `git status` shows no change to it. A green suite at the end of this
phase means C3 was violated.

## Verification (whole plan)

```bash
go test ./internal/ws/ -count=1 -v          # TestSwitchScan rows all pass
go test ./... ; go test -race ./...          # one expected failure: the stale entry
gofmt -l internal/ws/op_timeout_test.go
```

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | All six label/arm forms behave as tabulated | D4, Confirmation |
| A2 | A bare case label errors with its position; no panic | D3, F1 |
| A3 | Type list is 40, differing from the old 41 only by `TypeSessionCancel` | C2, Confirmation |
| A4 | Both lists captured in the commit, not just the counts | C2 |
| A5 | `TestSourceScansSurviveCRLF` still passes | C4 |
| A6 | `op_timeouts.json` unmodified; the exposed failure is reported | C3, D7 |
| A7 | `server.go` unmodified | D5 |
| A8 | No production `.go` file changed | C1 |

**A6 is the criterion this plan is most likely to violate**, for the reason in
C3: it is the only one whose satisfaction looks like failure. A reviewer
skimming for green will read the red test as unfinished work.

**A4 is the one most likely to be done lazily.** "40 types, as expected" is not
evidence; two compensating errors also produce 40. The lists must be compared
element-wise.

## Rollout and Rollback

Test-only, no runtime effect, no release coupling. Each phase reverts cleanly.
P3 changes nothing, so there is nothing to revert.

CI will go red on the exposed stale entry once this is pushed. That is intended
and must be stated in the push, or someone will "fix" it — which is C3 again,
at a distance.

## Deferred (named, so they are not mistaken for oversights)

* **The `session.cancel` decision itself.** D7 hands it to the owner; whichever
  way it goes belongs in its own record or an amendment here, with the phone's
  behaviour considered.
* **The other source-scanning tests.** `codexDispatchedConstantsIn` and
  `internal/event/retention_test.go` are text scans too, and both were checked
  and found not to share these defects (MADR F-section). Converting them to AST
  for consistency alone is not justified by evidence.
* **The `len(out) < 25` sanity threshold.** F5 shows it is too loose to catch a
  single dropped type, but with the parser fixed it is a backstop for a
  different failure (a wholly broken scan) and tightening it is a guess without
  a case to calibrate against.

## Execution record (2026-09-07) — P1 and P2 done, P3 open

P1 and P2 ran and are committed (`82d28fa`). **P3 is deliberately unfinished:**
it ends in a question for the owner, and the suite is red until that question is
answered. `status` stays `in-progress` for that reason, not because work was
abandoned.

| Criterion | Result |
| --- | --- |
| A1 every label/arm form behaves | met — 7 rows, all pass |
| A2 bare label errors with its position, no panic | met |
| A3 list is 40, differing only by `TypeSessionCancel` | met |
| A4 both lists captured, compared element-wise | met — the diff is in the commit |
| A5 `TestSourceScansSurviveCRLF` still passes | met |
| A6 `op_timeouts.json` unmodified, failure reported | met |
| A7 `server.go` unmodified | met |
| A8 no production `.go` changed | met |

### The open question (D7)

```text
op_timeout_test.go:474: op_timeouts.json lists "session.cancel",
which no longer reaches dispatchAsync
```

`session.cancel` is in the table at 30000 ms. `handleSessionCancel` is inline by
deliberate decision (MADR 0137 F4), so by the table's own contract the entry
does not belong. But `op_timeouts.json` is shared with the phone, which must
exceed every value, so deleting the entry changes what the phone budgets for a
cancel.

**Delete the entry, or keep it and narrow the test's model of what the table
covers?** Answering it needs the phone's behaviour in view, which is why the
plan stops here rather than guessing. Whichever way it goes belongs in an
amendment to this record.

### What the plan predicted incorrectly

**One commit per phase did not survive P2.** P2's rows must assert on the
scan's failure paths, and while the scan reported through `*testing.T` the only
way to observe a failure was to let a subtest fail — which fails the parent
too. That forced `scanDispatchSwitch` to be split out as a pure function
returning `(types, problems, err)`, which reshaped P1's code. Committing P1
separately would have meant reconstructing a shape that never existed.

**The lesson is about testability, not about commits:** a helper that reports
through `*testing.T` cannot have its failure paths tested. If a phase plans to
test error behaviour, the interface has to return the error, and that is a
design constraint worth naming in the phase rather than discovering in it.

**The plan under-specified the table.** It listed six rows; ten were needed.
`default:` had to be added (it carries no labels and must not be reported as a
problem), and three rows for structural failures — no `handleMessage`, no
`switch env.Type`, two such switches — because D1 introduced those error paths
and nothing in the plan required proving them.

### What the plan got right

**Requiring an element-wise comparison, not a count.** A3 as written would have
been satisfied by "40, as expected"; A4 forced the diff, and the diff is the
only thing that shows the single removal was `TypeSessionCancel` and not some
other type balanced by a new one.

**Naming C3/A6 as most at risk, and why.** The phase does end with a red suite
and a one-line deletion available that would make it green. Having written down
in advance that the red line is the *correct* outcome is what makes it
straightforward to stop.

**Keying the switch on its tag rather than taking the first.** The plan called
for it on the strength of a measurement (exactly one `switch env.Type` today);
the implementation made a second one an explicit error, and a test row proves
it.

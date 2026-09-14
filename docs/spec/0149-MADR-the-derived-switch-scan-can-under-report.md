---
status: proposed
date: 2026-09-07
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# The derived switch scan can silently under-report, which is the drift it was written to prevent

## Context and Problem Statement

`asyncDispatchedTypes` in `internal/ws/op_timeout_test.go` reads
`handleMessage`'s switch out of `server.go` and returns every message type that
reaches `dispatchAsync`. Its own comment says why it is derived rather than
hand-maintained:

> It used to be a literal list, and the list is what drifted: MADR 0138 F4
> moved four handlers onto the async path and the list did not follow, so the
> table check reported them as *stale entries* — the opposite of the truth. A
> shadow copy of a switch is updated by the same person who forgot to update
> the switch.

The scan can produce that exact failure itself. It parses the switch with two
regular expressions, and on three of the four ways a Go `case` label can be
written it is wrong: twice silently, once by panicking. Those three are latent
— today's `server.go` uses only the fourth form.

**One defect is not latent.** The scan decides an arm is asynchronous by testing
whether any line in it contains the string `dispatchAsync`, which matches
comments. `case protocol.TypeSessionCancel:` is deliberately *inline* (MADR 0137
F4) and its explanatory comment mentions `dispatchAsync`, so the scan credits it
as dispatched. `session.cancel` is consequently required to be in
`op_timeouts.json`, is in it, and the check that would otherwise flag that entry
as stale cannot fire — the two errors conceal each other and the test is green
and wrong today.

This record was carried over from MADR 0147's execution record and 0148's,
deferred twice as "the nil-index panic, unrelated to line endings". The panic
turns out to be the least of it.

### What was measured, not assumed

**Today's switch is uniform, which is why F1-F3 have not fired.** Of `server.go`
lines 706–916 (`handleMessage`, between its signature and the `asyncHandler`
comment the scan uses as a delimiter):

| Property | Count |
| --- | --- |
| `case` lines | 47 |
| carrying no `protocol.Type…` | 0 |
| carrying more than one `protocol.Type…` | 0 |

**The scan's behaviour on each label form**, measured by running its two
regexps and its loop verbatim over synthetic bodies:

```text
single (today's form)    -> captured [TypeAuth]
same-line comma          -> captured [TypeA]          <- TypeB silently dropped
continuation form        -> captured []               <- whole arm silently dropped
label without protocol   -> PANIC: index out of range [0] with length 0
```

The three failing inputs, in Go source terms:

```go
case protocol.TypeA, protocol.TypeB:   // gofmt-idiomatic; keeps only TypeA
case protocol.TypeA,                   // also gofmt-idiomatic; keeps neither
    protocol.TypeB:
case someLocalConst:                   // panics
```

**The dropped-type failure is worse than "a type is missed".** The test that
consumes this list checks both directions:

```go
// every dispatched type must be in the table
// and nothing in the table is stale
```

If a type is silently dropped from the scan while its entry remains in
`op_timeouts.json`, the second check fires and reports the entry as **stale** —
telling the reader to delete a correct timeout. Removing it makes that op
inherit the default deadline, which is the ladder MADR 0095 D7 exists to keep
the phone from racing. That is precisely the inverted failure the scan's
comment describes 0138 F4 as having caused.

**The `len(out) < 25` sanity check does not catch it.** With 47 case lines, the
scan would have to lose more than 22 types before that guard trips. Losing one
— the realistic case — passes it.

**The sibling scans do not share the defect.**
`codexDispatchedConstantsIn` reads map keys (`^\tprotocol\.(Type\w+): \{`),
where a comma form is not expressible, and it iterates matches rather than
indexing `[0]`. `internal/event/retention_test.go` tests with
`strings.Contains(body, name+",")`, which handles the comma form. The defect is
confined to `switchDispatchedConstants`.

### Findings

**F1 — an unchecked index panics on a case label with no `protocol.Type`.**
`constName.FindAllStringSubmatch(m[1], -1)[0][1]`
(`op_timeout_test.go:124`) indexes `[0]` of a possibly-nil result. Measured:
`case someLocalConst:` panics with `index out of range [0] with length 0`.

**F2 — a same-line comma label silently keeps only the first constant.**
`case protocol.TypeA, protocol.TypeB:` yields `[TypeA]`. The regexp asks for
all matches with `-1` and then discards every one but `[0]`, so the intent to
handle several labels is visible in the code and not carried out.

**F3 — a continuation-form label is silently skipped entirely.** The anchor
`^\tcase (.+):$` requires the colon on the same line, so
`case protocol.TypeA,` matches nothing and the arm contributes no types at all.

**F4 — F2 and F3 produce an inverted error, not a missing one.** The consumer
reports a correct `op_timeouts.json` entry as stale, inviting its deletion,
which would drop that op to the default deadline (MADR 0095 D7). This is the
same shape as the drift 0138 F4 caused and that this scan was written to
prevent.

**F5 — the existing sanity check cannot see it.** `len(out) < 25` against 47
case lines tolerates losing up to 22 types. The realistic loss is one.

**F6 — F1, F2 and F3 are latent; F8 is not.** All 47 case lines carry exactly
one `protocol.Type…`, so the panic and the two dropped-label forms cannot fire
against today's `server.go`. Their trigger is ordinary — a future `case` that
groups two message types, written the way `gofmt` leaves it — but it has not
happened. F8 below is happening now.

**F7 — the scan's other fragility is the text delimiter.** It locates the
switch by searching for the literal comment `"\n// asyncHandler is a slow WS
op"`. Renaming or moving that comment silently changes what is scanned. This is
adjacent to F1–F3 and shares a cause: the switch is parsed as text.

**F8 — the scan credits an arm as asynchronous because a *comment* names
`dispatchAsync`, and it is doing so today.** The arm test is
`strings.Contains(line, "dispatchAsync")` over every line, including comments.
`case protocol.TypeSessionCancel:` stays inline deliberately (MADR 0137 F4) and
returns `s.handleSessionCancel`, but its comment reads "…and dispatchAsync is
bounded by maxAsyncPerClient…". Running the scan verbatim over the real file:

```text
OVER-REPORT: comment line credited [TypeSessionCancel] as async:
  // already running, and dispatchAsync is bounded by maxAsyncPerClient:

scan reports 41 async-dispatched types
  includes: TypeSessionCancel
```

**The consequence is a green test concealing a stale contract entry.**
`session.cancel` is present in `op_timeouts.json` at 30000 ms. Because the scan
wrongly reports it as dispatched, the "nothing in the table is stale" check
passes it. Remove the over-report and that entry is correctly flagged. The two
defects have been cancelling each other out, which is why nothing has ever
looked wrong.

## Decision Drivers

* **A test that silently under-reports is worse than no test**, because it is
  trusted. F4 makes it worse still: it does not go quiet, it gives a confident
  wrong answer.
* **The trigger is ordinary, not exotic.** Grouping two message types in one
  `case` is normal Go that `gofmt` will not reformat.
* **This scan's whole justification is not trusting a hand-maintained shadow of
  the switch.** A parser that mis-parses the switch undermines that
  justification.
* **The repository already parses Go as Go elsewhere.**
  `internal/event/retention_test.go` uses `go/ast`, and so does the guard added
  by MADR 0147 P8.

## Considered Options

* **A — Parse the switch with `go/ast` instead of regular expressions.** (chosen)
* **B — Patch the three regexp defects in place.**
* **C — Reject the shapes the scan cannot read.**
* **D — Leave it; document that one label per line is required.**

## Decision Outcome

Chosen: **Option A.** F1, F2, F3 and F7 are four symptoms of one cause — Go
source is being read as text — and an AST walk removes the cause. The
alternative patches three of the four and leaves the fourth.

### The decisions

**D1 — parse `handleMessage`'s switch with `go/ast`.** Locate the `FuncDecl`,
find the `SwitchStmt` on `env.Type`, and for each `CaseClause` collect the
`protocol.Type…` selectors in `clause.List` and determine whether `clause.Body`
reaches a `dispatchAsync` **call**. Comments are not part of the AST, so F8
cannot recur. Closes F1, F2, F3, F7, F8.

**D2 — every constant in a case clause is captured.** `clause.List` is a slice;
all of it counts, in every label form, because the AST does not distinguish
same-line from continuation.

**D3 — a case label the scan cannot interpret fails loudly, naming it.** If a
clause carries no `protocol.Type…` selector, the test must report the clause and
its position rather than panicking (F1) or ignoring it (F3). Silence is the
failure mode this record is about; an unreadable switch must be an error.

**D4 — prove each form with table-driven tests over synthetic sources.** The
four inputs measured above become cases: single, same-line comma, continuation,
and unreadable. This is cheap because MADR 0147 P2 already split the parser to
take a body rather than a path.

**D5 — do not change `server.go` to suit the scanner.** The switch is correct Go
and its formatting is not the test's business. If a future arm groups two types,
that must keep working, which is the point.

**D6 — leave the codex and event scans alone.** They do not share the defect,
and rewriting them would widen this record for symmetry rather than evidence.

**D7 — fixing the scan will expose `session.cancel` as a stale table entry, and
that is a separate decision, not a cleanup.** `op_timeouts.json` is the shared
daemon/phone contract (MADR 0095 D7) and the phone reads it, so deleting an
entry is a protocol-visible change, not a test fix. The plan must surface the
exposed failure and **stop**, rather than deleting the entry to get to green.
Whether `session.cancel` should carry a timeout despite being handled inline is
for the owner to decide with the phone's behaviour in view.

### Consequences

* Good: the scan becomes correct for every way the switch can legally be
  written, and stops depending on a comment's exact text (F7).
* Good: the inverted "stale entry" report (F4) becomes impossible from this
  cause, so the timeout ladder cannot be eroded by following a wrong test.
* Good: an unreadable switch becomes a named failure instead of a panic or a
  silent gap (D3).
* Neutral: the scan grows from two regexps to an AST walk. It is a test helper;
  clarity there costs nothing at runtime.
* Bad: `go/ast` is more code than the two regexps it replaces, and a reader
  skimming the file will find it heavier. The table-driven tests from D4 are
  what make it legible.
* Bad: this fixes the parser, not the design. The switch is still a source of
  truth read out of a file, and a sufficiently unusual rewrite of
  `handleMessage` could still confuse it — D3 makes that loud rather than
  silent, which is the achievable guarantee, not correctness under all edits.

### Confirmation

```bash
# All four label forms behave, including the two that silently failed:
go test ./internal/ws/ -run TestSwitchScan -count=1 -v

# The existing consumers are unchanged in behaviour:
go test ./internal/ws/ -run 'TestEveryAsyncDispatchedMethodIsInTheTable|TestAsyncOpTimeoutMatchesSharedTable|TestSourceScansSurviveCRLF' -count=1

# The count must drop by exactly one, and for the right reason:
#   before: 41 types, wrongly including TypeSessionCancel (F8)
#   after:  40 types, TypeSessionCancel correctly absent
# Every other type must be identical. Compare the two lists, not the counts.
```

## Pros and Cons of the Options

### A — Parse with `go/ast` (chosen)

* Good, because it fixes F1, F2, F3 and F7 by removing their shared cause
  rather than treating them as four bugs.
* Good, because `clause.List` makes D2 automatic — there is no "first match" to
  get wrong.
* Good, because it matches the house idiom already used in
  `internal/event/retention_test.go` and MADR 0147 P8.
* Neutral, because it is a test-only change with no runtime cost.
* Bad, because it is the largest of the four options for a defect that has
  never fired.

### B — Patch the three regexp defects in place

* Good, because it is small and reviewable: check the match before indexing,
  iterate all matches rather than `[0]`, and widen the anchor for continuation
  lines.
* **The strongest argument for it:** the defect is latent (F6), the file is a
  test, and three targeted fixes carry less risk of changing what the scan
  finds than a rewrite does. A rewrite must be proven to return the *same*
  47-line result; a patch obviously does.
* Bad, because the continuation form cannot be fixed by widening one anchor —
  it needs state across lines, which is a parser, written badly.
* Bad, because it leaves F7 entirely.

### C — Reject the shapes the scan cannot read

* Good, because it converts every silent failure into a loud one for very
  little code, and silence is the actual complaint of this record.
* Bad, because it makes the *test* refuse ordinary Go: a future `case
  protocol.TypeA, protocol.TypeB:` would fail the build until someone
  reformatted `server.go` to suit the scanner, which D5 rejects.
* Bad, because "the tool cannot read your correct code" ages into the tool
  being worked around.

### D — Leave it; document the constraint

* Good, because nothing is broken (F6) and a comment costs nothing.
* Bad, because the constraint is invisible at the point it matters — someone
  editing `server.go` is not reading `op_timeout_test.go` — and the penalty for
  violating it is a confident wrong answer (F4), not an error.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| 47 case lines, 0 bare, 0 multi-`Type` | grep over `server.go:706-916` |
| Scan keeps only `[0]` on a comma label | `op_timeout_test.go:124`; measured with the verbatim regexps |
| Continuation form captures nothing | same harness, `captured []` |
| Bare label panics | same harness, `index out of range [0] with length 0` |
| Consumer reports dropped types as stale | `TestEveryAsyncDispatchedMethodIsInTheTable`, second loop |
| `len(out) < 25` cannot catch losing one of 47 | `op_timeout_test.go`, sanity check |
| Delimiter is a literal comment | `op_timeout_test.go:109` |
| Codex scan iterates matches, no `[0]` index | `codexDispatchedConstantsIn` |
| Event scan handles the comma form | `internal/event/retention_test.go`, `Contains(name+",")` |
| Timeout ladder rationale | MADR 0095 D7 |
| The drift this scan replaced | MADR 0138 F4, quoted in the scan's own comment |
| Comment credits TypeSessionCancel as async | scan run verbatim over `server.go`; `server.go:788-795` |
| `session.cancel` is in the table at 30000 ms | `internal/protocol/op_timeouts.json` |
| Cancel is inline on purpose | MADR 0137 F4, cited in the arm's own comment |
| One switch in handleMessage; no nested dispatch | grep over `server.go:706-916` |

### Related records

* **MADR 0138** — F4 is the drift that caused the hand-maintained list to be
  replaced by this scan.
* **MADR 0095** — D7 is the timeout ladder the table protects, and what erodes
  if a correct entry is deleted on a false stale report.
* **MADR 0147** — P2 split this scan to take a body rather than a path, which
  is what makes D4's synthetic tests cheap; its execution record is where this
  defect was first noted and deferred.
* **MADR 0148** — deferred it a second time.

### Open questions for the plan

1. **How is the switch identified in the AST?** `handleMessage` may contain more
   than one `switch`. Keying on the tag expression `env.Type` is the obvious
   discriminator; the plan must confirm it is unambiguous and say what happens
   if it stops being so.
2. **Does `dispatchAsync` detection need to be recursive?** The current text
   scan matches any line in the arm. An AST walk of `clause.Body` should use
   `ast.Inspect` rather than looking only at top-level statements, or an arm
   that dispatches inside an `if` would be missed. **[unverified]** whether any
   arm does that today.
3. **How is "the same result as before" proven?** The safest evidence is the
   list of dispatched types captured before and after the rewrite, compared
   exactly. The plan should record that list rather than trusting the consuming
   test to notice a difference.

## Amendment — 2026-09-07: D7 resolved — the entry is deleted

D7 left the `session.cancel` question open for the owner. Answered: **delete the
entry.** Recorded here with the evidence the decision rested on, because "we
removed a line from the shared protocol table" is exactly the kind of change a
future reader will want justified.

**F9 — `session.cancel` is the outlier among three deliberately inline ops.**
MADR 0137 F4 made three operations inline: `session.cancel`,
`session.pending_asks` and `oauth.cancel`. Two of the three are already absent
from `op_timeouts.json`. Only `session.cancel` is listed — not by a design
decision about it, but because nothing ever caught it (F8 is why).

| Op (MADR 0137 F4, inline) | In `op_timeouts.json` |
| --- | --- |
| `session.cancel` | 30000 |
| `session.pending_asks` | absent |
| `oauth.cancel` | absent |

**F10 — the daemon never reads the value.** `asyncOpTimeout` is called from
exactly one site, `server.go:978`, inside `dispatchAsync` (declared line 925).
An inline op never reaches it. `asyncOpTimeout` also has no `TypeSessionCancel`
case, so even if it were reached it would return the same 30 s default.

**F11 — deleting it changes the phone's timeout by zero.** `opTimeoutFor` in
`apps/mobile/lib/data/ws/mcremote_client.dart` has no `'session.cancel'` case
and falls to `default: 30 s + kOpTimeoutMargin` (10 s) = 40 s. The JSON value is
30000 and `default_ms` is also 30000, so the ladder test's expectation for a
*listed* entry (`daemon + margin` = 40 s) is identical to the fallback it pins
separately for an *unlisted* method (`default_ms + margin` = 40 s). 34 of the 48
entries equal the default, so a value at the default carries no information.
**[Arithmetic from the Dart source and `apps/mobile/test/op_timeout_ladder_test.dart`;
not executed — Flutter is not installed on this host. CI's Flutter lane closes
this gap.]**

**D8 — delete `"session.cancel": 30000` from `internal/protocol/op_timeouts.json`.**
Closes F9, and closes the failure P3 exposed. The test's model — the table lists
async-dispatched ops — is left intact and keeps its teeth.

**The alternatives, and why not.** Relaxing the test to permit entries for
inline ops would remove the only mechanism that can catch a genuinely stale
entry, which is the drift 0138 F4 caused. Completing the table instead — adding
`session.pending_asks` and `oauth.cancel` — was rejected on a harder ground:
inline handlers run under the read loop's context and get no
`context.WithTimeout` at all, so a table value for one would assert an
enforcement the daemon does not perform. Listing a false deadline is worse than
listing none.

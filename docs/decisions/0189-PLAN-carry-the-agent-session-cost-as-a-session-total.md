---
status: in-progress
date: 2026-10-14
associated-madr: "0189-MADR-carry-the-agent-session-cost-as-a-session-total.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement carrying the agent session cost as a session total

Associated MADR: [0189-MADR-carry-the-agent-session-cost-as-a-session-total.md](0189-MADR-carry-the-agent-session-cost-as-a-session-total.md)

~~**Not approved for execution.** The MADR is `proposed`.~~ On 2026-10-14 the owner accepted the
MADR's recommended option and approved C1 and C2 ("accept option 1, do C1 and C2"). This PLAN
implements that option, "Carry the agent's cumulative cost as a separately named session total,
with its currency" (D1–D5). ~~C3 and C4 still wait for approval by name.~~ On 2026-10-14, after C1
and C2 were committed, the owner said "committed, proceed". That approves C3 and C4 step 1. C4
step 2, the grok turn, still waits for the owner's quota.

* A phase starts only after the owner approves it by name.
* Each Go phase ends with `make pre-add-check` on its staged Go files, `go test -race ./...` and the
  0145 gates on a Windows host.
* The Flutter phase ends with `make preflight` (`flutter analyze`, `flutter test`, `dart format`).
* The owner commits (`git commit --no-edit`); an agent stages only.

## Goal

When an ACP agent reports its session's cumulative cost, Magic keeps it as a session total in the
agent's currency, shows it in `/context` and on the phone, and never writes it into the per-turn
`cost_usd`.

## Scope

* `internal/event/event.go`: `SessionCost` (`{Amount, Currency}`) and `Usage.SessionCost`
  (`session_cost`), with the history byte accounting (D1).
* `internal/provider/acpagent/session.go`: the `u.UsageUpdate` case (D1), and the A4 table in
  `sessionupdate_test.go`.
* `internal/session/manager.go`, `tokencost.go` and `commands.go`: the stored session cost, its
  persistence on `Meta`, resume and fork, and `/context` (D2).
* `apps/mobile/lib/data/protocol/models.dart`, `transcript_reducer.dart` and the context chip in
  `features/chat/chat_screen.dart`, with a widget test (D3).
* `docs/guides/protocol-v1.md`: `session_cost` on `usage_update` and on `Meta`.
* `docs/decisions/0179-MADR-gobble-native-acp-provider.md`: the dated note (D5).
* **Out of scope:**
  * a `session_cost` for OpenCode or kilo;
  * showing `Meta`'s totals in the phone's session list;
  * currency conversion, or checking codes against the ISO list;
  * gobble's agent sending a cost, which belongs to gobble-cli's records.

## Implementation Steps

### C1. The event and the acpagent mapping (D1)

1. Add `event.SessionCost` and `Usage.SessionCost`, with no behavior yet. Document `session_cost` in
   `protocol-v1.md` ("Detailed usage" and the `usage_update` section). It is a session total in the
   agent's currency, distinct from the per-turn `cost_usd`. Run the guards `TestBytesTracksStructGrowth`
   and `TestEventTypesAreDocumented`.
2. Write the acpagent test (MADR Confirmation, first item). Show it failing against the current
   decline.
3. Map the case: amount `>= 0`, currency three ASCII letters, upper-cased. Otherwise decline as
   `usage_update.cost`. Decline `cost._meta` as `usage_update.cost._meta`. Never set `CostUSD`.
   Update the A4 table's `UsageUpdate` row.
4. The schema-built fixture: a `usage_update` frame with `cost`, built from the SDK types, committed
   beside the acpagent tests. Its file comment says it is not a capture, and why (D4).

### C2. The daemon's session total (D2)

1. Write the manager tests (MADR Confirmation, second and third items). Show them failing.
2. Keep the latest session cost on the session entry, replaced only by a report that carries one,
   and persist it on `Meta` as `session_cost`.
3. Resume and relaunch keep the stored value. A fork clears it.
4. `/context` appends `· session cost <formatted>`, with the formatting from MADR D3.

### C3. The phone (D3)

1. Write the widget test (MADR Confirmation, fourth item). Show it failing.
2. Parse `session_cost` into `Usage`, include it in `==` and `hashCode`, and keep the last known
   value in the reducer when a later report omits it.
3. Add the tooltip line to `_ContextUsageChip`.
4. `dart format` on the changed files, and `make preflight`.

### C4. Records and probe (D4, D5)

1. Add the dated note to 0179's planned `usage_update.cost` line, pointing to the 0189 MADR.
2. With the owner's quota, after grok's account resets: one short grok turn with
   `MCREMOTE_WIRE_CAPTURE_DIR` set. Record whether grok sends a standard `usage_update` during a
   turn, and if so whether it carries a cost. That decides whether a capture replaces the
   schema-built fixture.

## Verification

* Every new test is seen failing first, on a scratch copy, and passes after.
* `go test -race ./...`, `make pre-add-check`, `make lint`, `make preflight`.
* `make ci-windows` on the Windows test host.
* `python3 scripts/check_records.py --check-all` and `make markdownlint-docs`.

## Rollout and Rollback

* No agent Magic runs sends a cost today, so C1–C3 change nothing a user sees until one does.
* `session_cost` is additive. An older phone ignores it, and an older daemon never sends it.
* `Meta.session_cost` is `omitempty`, so records written before C2 load unchanged. A daemon
  rolled back past C2 ignores the field on read.
* Rollback of any phase is a revert of its commit. No data migration is involved.

## Execution record

### C1 and C2, 2026-10-14: complete (staged, not committed)

**Approval:** the owner on 2026-10-14, "committed, accept option 1, do C1 and C2".

**C1, structure.**

* `event.SessionCost` (`{Amount, Currency}`) and `Usage.SessionCost` (`session_cost`) were added.
* `Bytes` counts the second allocation and its currency string.
  * `TestBytesCountsSessionCost` was written before that line. Against the bare field it **failed**:
    `session cost adds 0 bytes, want 27`.
  * `TestBytesTracksStructGrowth` and `TestEventTypesAreDocumented` pass.
* `protocol-v1.md`: `session_cost` is documented under "Detailed usage", where `usage_update`'s
  additive fields live, and on `Meta`. The `Meta` bullet is C2's, written in the same edit.

**C1, the mapping.**

* `TestUsageCostBecomesASessionCost` (`internal/provider/acpagent/sessioncost_test.go`) covers nine
  cases:
  * mapped: USD, a free zero, a lower-case code, and `_meta`;
  * refused: a negative amount, a two-letter code, a non-letter code, and a non-ASCII code;
  * no cost at all.

  On the old decline it **failed** in the four cases that should map: `session_cost = <nil>`, and
  `declined [usage_update.cost]`.
* `sessionCost` (`convert.go`) does the checks. The `u.UsageUpdate` case sets `SessionCost` and
  declines `usage_update.cost` or `usage_update.cost._meta`. It never sets `CostUSD`.
* The A4 table's `UsageUpdate` row now expects `usage_update` with no decline.
* **Fixture (D4).** `usageFrame` marshals the frame from `acp.SessionUpdate` and
  `acp.SessionUsageUpdate`, in the test file itself. Its comment says it is not a capture, and why.
  The PLAN said "committed beside the acpagent tests"; it is in the test that uses it, not in a
  separate data file.

**C2.**

* `Meta.SessionCost` and `Record.SessionCost` were added first, with no behavior.
* Three tests were then run against them, and **failed**:
  * `TestSessionCostIsTheLatestReportedTotal`: `after a report: <nil>, want 0.10 USD`.
  * `TestContextReportsTheSessionCost`: four cases, for example
    `notice = "Context: 1,200 tokens of 8,000 (15%)", want it to contain " · session cost $0.0234"`.
  * `TestForkStartsWithoutTheParentsSessionCost`: `the parent never recorded its session cost`.
* The implementation:
  * The manager's usage handling replaces `e.meta.SessionCost` when a report carries a different
    one, and persists it. A report without one leaves it.
  * `create` carries the prior cost, from the live entry or the stored record, the same way it
    carries the name source. A fork has a new id and no prior, so it starts without one.
  * `writePersist` and the list-from-disk path copy the field.
  * `/context` appends `· session cost …` through `formatSessionCost`.
* The fork test needed the fork fake to expose its last session. `forkProvider` in
  `diff_fork_test.go` gained a `last` field. That file was not named in Scope.

**Planted breaks, each on a scratch copy of the repository.** Every plant failed its test:

| Plant | Failure |
| --- | --- |
| negative amount accepted | `session_cost = &{Amount:-0.01 Currency:USD}, want <nil>` |
| currency not upper-cased | `session_cost = <nil>, want &{Amount:1.5 Currency:EUR}` |
| cumulative cost written into `cost_usd` | `cost_usd = 0.42: a session total must never become the per-turn figure` |
| `cost._meta` not declined | `declined [], want [usage_update.cost._meta]` |
| reports added up | `after a second report: &{Amount:0.35 Currency:USD}, want 0.25 USD (replaced, not added)` |
| a report without a cost clears it | `a report without a cost changed it: <nil>, want 0.10 USD` |
| not persisted | `stored record: <nil> <nil>; want session_cost 0.25 USD` |
| resume from disk drops it | `resumed from disk: <nil>, want 0.25 USD` |
| every new session gets a cost | `fork session cost = &{Amount:0.5 Currency:USD}, want none` |
| `/context` loses the two-decimal rule | `… session cost $1.5000", want it to end with " · session cost $1.50"` |
| `/context` drops the currency code | `… session cost $0.4200", want it to end with " · session cost 0.4200 EUR"` |
| `Bytes` stops counting the cost | `session cost adds 0 bytes, want 27` |

* The two-decimal plant **passed** at first: `$1.5000` contains `$1.50`, and the test checked
  containment. The test now checks the end of the line, and the plant fails.
* The fork plant first broke the build (`declared and not used: priorCost`), which proves nothing.
  It was rewritten to compile, and then failed as shown.

**Verification.**

* `go test -race ./...`:
  * The first run failed once, in `TestSpareIsRearmedAtTurnEndNotTurnStart`
    (`prewarm_rearm_test.go:114: wanted 1 "prewarm failed", got 0`). That test waits 3 seconds for
    a background spawn and touches no usage code.
  * Alone it passed ten runs with `-race`, and the acpagent package passed three.
  * The second full run passed.
  * 2026-10-15: the cause was found and fixed in
    [0193-MADR-warm-fresh-test-executables-before-timing-them.md](0193-MADR-warm-fresh-test-executables-before-timing-them.md).
    macOS holds a new stub's first run for a Gatekeeper scan, and that hold fell inside the test's
    3 s wait.
* `make pre-add-check` on the 13 changed Go files: clean. `make lint`: clean.
* `make ci-windows` on the Windows test host: `ci-windows-exit=0`, with `internal/event`,
  `internal/provider/acpagent` and `internal/session` all `ok`.

**Found, out of scope.** `Meta`'s token totals (`Turns`, `InputTokens`, `OutputTokens`,
`CachedTokens`) are not on `Record`, so they are not written to disk. This is despite
`0138-PLAN-overhaul-provider-surfaces-and-turn-path.md` 6.3 saying they are "persisted with the
record". They reset when the daemon restarts. `session_cost` is persisted. The token totals are left
as they are, for the owner to decide.

*Update, 2026-10-14:* the owner asked for it to be fixed under its own record. It is fixed as Phase 12
of [0138-PLAN-overhaul-provider-surfaces-and-turn-path.md](0138-PLAN-overhaul-provider-surfaces-and-turn-path.md),
with the rule recorded in the 0138 MADR's amendment of the same date. A relaunch reset them too, not
only a restart.

**Not done:** C3 (the phone) and C4 (the 0179 note, and the grok probe). They wait for approval by
name. The grok probe also waits for the quota.

C1 and C2 were committed as `e6e95205`.

### C3 and C4 step 1, 2026-10-14: complete (staged, not committed)

**Approval:** the owner on 2026-10-14, "committed, proceed".

**C3, structure.** `SessionCost` (`amount`, `currency`, `==`, and a `label` stub returning `''`) and
an unparsed `Usage.sessionCost` were added first.

**C3, the test.** `apps/mobile/test/session_cost_test.dart` has 13 tests:

* parsing: a valid cost, a free zero, a lower-case code, six malformed shapes, and absent;
* the label: USD and another currency;
* the reducer: a change that is only in the cost, keeping the last known cost, and an identical
  report;
* the context chip, pumped in `ChatScreen`: the tooltip with a USD cost, with another currency,
  and with no cost.

Against the bare structure it **failed** 9 of 13, for example:

* `parsing a valid cost is kept with its currency`: `Expected: <Instance of 'SessionCost'>`,
  `Actual: <null>`;
* `label USD is dollars…`: `Expected: '$0.0234'`, `Actual: ''`;
* `reducer a report whose only change is cost reaches the transcript`: `Expected: <0.2>`,
  `Actual: <null>`;
* `context chip the tooltip shows a USD session cost`: `Found 0 widgets`.

The four that passed are the ones that should already pass: the malformed shapes, absent, the
identical report, and the chip with no cost.

**C3, the implementation.**

* `SessionCost.tryParse` refuses a missing, negative or non-finite amount, or a currency that is
  not three ASCII letters, and upper-cases the code. `label` follows MADR D3.
* `Usage` parses `session_cost`, and its `==` and `hashCode` include it. `withSessionCost` copies a
  report with another cost.
* The reducer carries the last known cost onto a report that omits it, then compares.
* `_ContextUsageChip` adds `Session cost <label>` as the tooltip's last line.

**C3, planted breaks, each on a scratch copy of `apps/mobile`.** Every plant failed:

| Plant | Failing test |
| --- | --- |
| equality ignores the cost | `reducer a report whose only change is cost reaches the transcript` |
| a report without a cost clears it | `reducer a report without a cost keeps the last known one` |
| `session_cost` not parsed | five tests, from `parsing a valid cost…` to the reducer pair |
| negative amount accepted | `parsing a cost that cannot be shown honestly is dropped` |
| code not upper-cased | `parsing a lower-case code is upper-cased` |
| every currency shown as dollars | `label any other currency keeps its code`, and the chip's other-currency tooltip |
| the two-decimal rule lost | both label tests, and the chip's other-currency tooltip |
| the tooltip omits the cost | both chip tooltips that carry a cost |

**C4 step 1.** [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md)
has dated notes in three places:

* D5's `usage_update.cost` → `CostUSD` line is struck through and points here.
* F4's "`cost` is dropped" now says it is mapped since C1.
* Its Confirmation item notes that the cost half of the mapping test exists.

**Verification.** `make preflight` passed:

* records and docs links, and markdownlint;
* `gofmt`, `go mod tidy`, `go vet`, `staticcheck`, and `go test -race`;
* the install tests, systemd units and release build;
* the Flutter pin and pub advisories;
* `dart format` (`Formatted 215 files (0 changed)`), `flutter analyze` (`No issues found!`), and
  `flutter test` (`+1437 ~3: All tests passed!`).

No Go file changed in this step.

**Not done.** C4 step 2, the grok turn, waits for the owner's quota. Until then, the fixture stays
built from the schema. The PLAN stays `in-progress`.

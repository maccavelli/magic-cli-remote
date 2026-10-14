---
status: proposed
date: 2026-10-14
decision-makers: Project Owner
consulted: none
informed: operators of ACP providers (grok today; gobble when registered)
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Carry an ACP agent's `usage_update` cost as a session total, never as a per-turn figure

## Context and Problem Statement

[0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md)
D10 takes up ACP capabilities one record at a time. The session title was first
([0187-MADR-show-the-agent-session-title-without-overwriting-a-user-name.md](0187-MADR-show-the-agent-session-title-without-overwriting-a-user-name.md)),
and notices second
([0188-MADR-show-agent-notices-on-the-phone.md](0188-MADR-show-agent-notices-on-the-phone.md)).
`usage_update`'s `cost` is third. D10 holds it until "the accounting rule (per turn or cumulative,
currency) is decided". This record decides that rule.

**What ACP defines** (`acp-go-sdk2@v0.17.1`):

* `usage_update` carries `used` and `size`, which are context-window figures, and an optional `cost`
  (`types_gen.go:12677-12692`).
* `cost` is `{amount, currency}`. `amount` is the "Total cumulative cost for session", and
  `currency` is an "ISO 4217 currency code (e.g., "USD", "EUR")" (`types_gen.go:2744-2756`).
* The SDK does not check the currency. `amount` is a JSON number, so it cannot be NaN or infinite,
  but nothing stops it being negative.
* `usage_update` is a stable variant, and no client capability gates it. The SDK's capability gates
  cover `notice`, the compaction variants and boolean config options, but not usage
  (`types_gen.go:20122-20144`). So there is nothing to advertise. An agent may send `cost` today.

**What Magic does with it today.** acpagent maps `used` and `size` to an `event.Usage`. When `cost`
is present, it declines it by name as `usage_update.cost`, once per session
(`internal/provider/acpagent/session.go:1855-1869`). The A4 table pins that
(`sessionupdate_test.go:85-86`).

**Magic has three places for usage, and none matches ACP's `cost`:**

| Where | What it means | Cost field |
| --- | --- | --- |
| `event.Usage` on `usage_update` (`internal/event/event.go:400-424`) | the **latest turn**; MADR 0112 A4 says that labelling a per-turn figure as a session total is "the specific error this split exists to avoid" | `CostUSD`, per turn, USD only |
| `provider.AgentSessionUsage` on `agent_sessions.list` (`internal/provider/provider.go:1218-1234`) | an agent-native session's whole-session total, read from the agent's own session list | `CostUSD`, total, USD only |
| `session.Meta` (`internal/session/manager.go:149-157`) | Magic's own running token totals, accrued at each turn end (MADR 0138 Phase 6) | none |

* Only OpenCode fills `event.Usage.CostUSD`, from each assistant message's own cost
  (`internal/provider/opencode/usage.go:90-116`). kilo, codex and grok leave it unset.
* The phone parses `costUsd` on `usage_update` but shows it nowhere
  (`apps/mobile/lib/data/protocol/models.dart:2084-2112`). It shows the aggregate's `costUsd` in
  the sessions list (`features/sessions/sessions_screen.dart:106-115`).
* The phone's `Usage` equality compares only `used` and `size` (`models.dart:2116-2121`), and the
  reducer drops a report equal to the current one (`transcript_reducer.dart:59-65`). So a report
  whose only change is cost never reaches the screen.
* `usage_update` is droppable telemetry (`docs/guides/protocol-v1.md`, "`usage_update` event").
  A dropped report is acceptable for a cumulative figure, because the next one restates it. It is
  not acceptable for a per-turn delta, which would be lost.
* `/context` reads the session's last usage report (`internal/session/commands.go:476-506`). That
  report is replaced by every usage event. grok emits two kinds, the standard one and its own from
  `_x.ai` `turn_completed` (`internal/provider/acpagent/xaiusage.go`), and neither carries a cost.

**Who sends a cost.** No agent Magic runs is known to.

* grok sends no standard `usage_update` at all. MADR 0023 recorded this, and its local session logs
  on the owner's Mac contain none (checked 2026-10-14). Its token accounting arrives on `_x.ai`
  `turn_completed`, which has no cost field. The 1.0.50 capture behind 0188's N4 has no
  `usage_update` in its first 12 seconds.
* gobble-cli's agent sends `usage_update` with `used` only, and no `size` or `cost`
  (gobble-cli `acpserver/prompt.go:307`). Its own ACP client does model a `cost`, so gobble is
  aware of the field.
* [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md), which is still
  `proposed`, plans `usage_update.cost` → `event.Usage.CostUSD`. That would put a cumulative
  figure in the per-turn field, which is the error MADR 0112 A4 names.

**The question.** When an agent reports its session's cumulative cost, in some currency, where does
Magic put it, and what does the user see?

## Decision Drivers

* A per-turn figure and a session total never share a field (MADR 0112 A4, MADR 0138 Phase 6).
* A wrong number shown confidently is worse than an absent one (`event.Usage`'s own rule).
* Keep the agent's currency. Magic has no exchange rate and must not invent one.
* `usage_update` is droppable, so the meaning of a report must not depend on having seen the one
  before it.
* A cost that is known stays visible after a later usage report that omits it.
* D10's bar: daemon and phone behavior, a wire fixture, and a live probe. There is no capability to
  advertise here.
* The smallest phone change that shows the figure.

## Considered Options

* Carry the agent's cumulative cost as a separately named session total, with its currency
* Derive a per-turn USD figure from successive cumulative reports, into `cost_usd`
* Put the cumulative amount into `cost_usd` as reported (0179's planned mapping)
* Keep declining until an agent Magic runs sends a cost

## Decision Outcome

Chosen option: "Carry the agent's cumulative cost as a separately named session total, with its
currency", because it is the only option that keeps ACP's meaning (a running session total in a
named currency) without either relabelling it as a per-turn figure or guessing a delta. A dropped
or repeated report cannot corrupt it.

Under the chosen option:

* **D1. acpagent maps `cost` to a new `event.Usage.SessionCost`** (`session_cost` on the wire),
  `{amount, currency}`.
  * `CostUSD` is never set from `cost`. It stays the per-turn figure that OpenCode reports.
  * A `cost` with a negative `amount`, or a `currency` that is not three ASCII letters, is not
    mapped. It is declined by name as `usage_update.cost`, once per session, as today. Valid codes
    are upper-cased; Magic does not check them against the ISO list.
  * `cost._meta` is declined by name as `usage_update.cost._meta`, as 0184's D7 does for `_meta`
    elsewhere.
* **D2. The daemon keeps the latest session cost per session.** A usage report that carries a
  session cost replaces the stored one. A report without one leaves it unchanged. The figure is not
  added up, because the agent's figure is already a total.
  * It is persisted on `Meta` as `session_cost`, so it survives a daemon restart and appears in
    `session.list_result`.
  * Resume and relaunch keep it until the agent reports again. A fork starts without one, because it
    is a different agent session with its own figure.
  * `/context` appends it, for example `· session cost $0.42`.
* **D3. The phone shows the session cost in the context chip's tooltip.**
  * USD is shown as `$` with four decimals below one dollar and two from one dollar up, as the
    engine runtime line already does (`internal/provider/httpagent/runtime.go:169-176`). Any other
    currency is shown as the amount followed by its code, for example
    `0.42 EUR`.
  * The phone keeps the last known session cost when a later `usage_update` omits it, as the daemon
    does in D2.
  * `Usage` equality includes the session cost, so a report whose only change is cost reaches the
    screen.
  * A phone that does not know `session_cost` ignores it.
* **D4. Probe and fixture.** No agent Magic runs sends a cost, so the wire fixture is built from the
  schema, and the record says so.
  * grok's half is a check that grok still sends no standard `usage_update` during a turn. That
    needs model turns, so it waits for the owner's quota.
  * The live confirmation comes when an agent that sends `cost` is registered. gobble's agent is the
    likely first, and that is
    [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md)'s to arrange.
* **D5. 0179's planned mapping is replaced.** The line in
  [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md) that maps
  `usage_update.cost` → `event.Usage.CostUSD` gets a dated note pointing here. That record is still
  `proposed`, so this is a note, not a supersession.

### Consequences

* Good, because an agent's reported spend reaches the user in the agent's own currency, labelled as
  what it is: the session's total.
* Good, because the per-turn `cost_usd` keeps one meaning across every provider.
* Good, because a dropped, repeated or reordered report self-corrects on the next one.
* Neutral, because no agent Magic runs sends a cost yet. As with 0188, this makes Magic ready rather
  than fixing a visible gap.
* Neutral, because OpenCode keeps reporting only per-turn cost. Its whole-session figure still
  comes from `agent_sessions.list`. Giving them a `session_cost` is a separate decision.
* Bad, because the phone gains a field, an equality change and a tooltip line, and `Meta` gains a
  persisted field. Each needs a test.
* Neutral, because `session_cost` on `Meta` reaches `session.list_result`, but the phone does not
  show `Meta`'s token totals today, so its session list shows nothing new. Showing it there is not
  part of this record.
* Bad, because the figure is only as fresh as the agent's last report. A session whose agent stopped
  reporting shows its last known cost.

### Confirmation

* An acpagent test delivers `usage_update` with a valid cost, a lower-case currency, a negative
  amount, a bad currency, and `cost._meta`. It requires `session_cost` on the first two (the code
  upper-cased), none on the others, the declines by name, and `cost_usd` never set.
* A manager test requires the stored session cost to be replaced by a report that carries one, kept
  through a report that omits one, persisted on `Meta`, kept on resume, and absent on a fork.
* A `/context` test requires the cost suffix when known and none when unknown.
* A Flutter widget test draws the tooltip for USD and for another currency, keeps the last known
  cost through a report without one, and shows that a cost-only change rebuilds the chip.
* Each new test is seen failing first, on a scratch copy.
* The schema-built fixture is committed beside the acpagent tests.

## Pros and Cons of the Options

### Carry the agent's cumulative cost as a separately named session total, with its currency

* Good, because it keeps ACP's meaning and currency without conversion or arithmetic.
* Good, because the meaning of each report stands alone, which suits droppable telemetry.
* Neutral, because it adds a second cost field beside `cost_usd`. Their names say which is which.
* Bad, because it needs a field on the event, on `Meta`, and on the phone.

### Derive a per-turn USD figure from successive cumulative reports, into `cost_usd`

* Good, because it reuses an existing field and needs no phone model change.
* Bad, because a dropped report silently merges two turns, and a restarted agent or a resumed session
  makes the difference negative or wrong.
* Bad, because a non-USD cost has to be dropped or converted at a rate Magic does not have.
* Bad, because ACP reports cost per update, not per turn. Which reports bracket a turn is Magic's
  guess.

### Put the cumulative amount into `cost_usd` as reported (0179's planned mapping)

* Good, because it is one line in acpagent.
* Bad, because it labels a session total as a per-turn figure, the error MADR 0112 A4 names.
* Bad, because a non-USD amount would be shown as dollars.

### Keep declining until an agent Magic runs sends a cost

* Good, because nothing changes and no agent sends one today.
* Bad, because the first agent that does gets its spend discarded, and the accounting rule D10 asks
  for stays undecided. The next mapping is then likely to be the convenient one-liner above.

## More Information

* The facts above were read from the code on 2026-10-14, at `a150f577`, and from
  `acp-go-sdk2@v0.17.1`.
* The PLAN is
  [0189-PLAN-carry-the-agent-session-cost-as-a-session-total.md](0189-PLAN-carry-the-agent-session-cost-as-a-session-total.md).
  Nothing is implemented until the owner accepts an option and approves the PLAN.

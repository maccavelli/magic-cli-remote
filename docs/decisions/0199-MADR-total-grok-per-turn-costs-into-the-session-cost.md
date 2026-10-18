---
status: proposed
date: 2026-10-18
decision-makers: Project Owner
consulted: none
informed: contributors who work on usage and cost reporting
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Total grok's per-turn costs into the session cost

## Context and Problem Statement

[0189-MADR-carry-the-agent-session-cost-as-a-session-total.md](0189-MADR-carry-the-agent-session-cost-as-a-session-total.md)
added `event.Usage.SessionCost`: the agent's own running total for the session, in its currency.
The daemon keeps the latest one per session and persists it on `Meta` (D2). The phone shows it in
the context chip's tooltip, and `/context` appends it. Its only source is a standard
`usage_update.cost`, and no agent Magic runs sends one, so today nothing shows.

The 0189 PLAN's live probe on 2026-10-18 found that grok sends a cost anyway, on its own channel.
The 0189 MADR's amendment of that date records it:

* Each `_x.ai/session_notification` `turn_completed` carries `usage.costUsdTicks`, the cost of
  that one turn, in grok's 1e10 ticks to the dollar.
* acpagent already reads that notification for token counts (`xaiusage.go`), but ignores the
  cost.

So a grok session's cost is known turn by turn and never reaches the user. That includes the
phone, the person who is paying.

### What was measured

All probes ran on 2026-10-18, against grok `1.0.50`. The session was the one the 0187 phone check
created: three turns, in a scratchpad working directory.

* **grok persists the total.** The session's `usage.json` in grok's store holds
  `costUsdTicks: 181628000` over `turnCount: 3`, about $0.0182.
* **grok's ledger does not survive a restart.** A fresh grok process loaded the session
  (`session/load`, no prompt). `_x.ai/session/usage`, which `/usage` already calls, then answered
  "0 input + 0 output tokens · 0 turns · cost unavailable". `usage.json` was unchanged. The ledger
  is in-process (`runtime.go` says so), and a Magic resume or relaunch starts a new grok process.
* **A load replays every turn, with its cost.** The same load, captured, held 3 `turn_completed`
  frames. Each carried its original `costUsdTicks` and its own `prompt_id`, under
  `_meta.isReplay: true`. Their costs sum exactly to `usage.json`'s total:
  118,673,600 + 31,320,800 + 31,633,600 = 181,628,000.
* **The daemon applies a replayed report.** The usage branch of `internal/session/manager.go`
  updates `Meta.SessionCost` from any usage event, and does not check `ev.Replay` as the notice
  and compaction branches beside it do. So a figure sent during a replay would replace the stored
  total partway through.

Not measured: whether a parent turn's cost includes the spend of subagents it ran. The ledger's
comment says the ledger folds subagent spend in, but nothing says whether `turn_completed` does.
Finding out needs a turn that runs a subagent, which spends quota.

### The question

How should grok's per-turn costs become the session total that 0189 already carries and shows?

## Decision Drivers

* The figure must match grok's own total, before and after a resume or relaunch.
* A replayed or repeated turn must not be counted twice.
* A turn with no cost must not leave a total that reads as complete. A number that is too low is
  worse than no number (0189, and `runtime.go`'s "a wrong number rather than a missing one").
* 0189's rule, that a report replaces the stored total, should keep one meaning across agents.
* Only Magic's code should change. grok's private files are not an interface.

## Considered Options

* acpagent adds up every `turn_completed` cost it sees, replays included, and reports the sum on
  live turns
* acpagent reports each live turn's cost, and the daemon adds it to the stored total
* After each turn, acpagent reads grok's ledger (`_x.ai/session/usage`) and reports its cost
* Read the total from grok's `usage.json`
* Keep showing nothing for grok

## Decision Outcome

Chosen option: "acpagent adds up every `turn_completed` cost it sees, replays included, and reports
the sum on live turns". A load replays the whole history, so the sum rebuilds itself in every new
grok process, and it matches grok's own total exactly. It keeps 0189's rule that a report replaces
the stored total, and it changes only acpagent.

* **E1. The sum.** acpagent keeps one running total per agent session, in integer ticks.
  * Every `turn_completed` from this session adds its `costUsdTicks`, live or replayed.
  * A `prompt_id` already counted is skipped, so a turn counts once. A turn without a `prompt_id`
    is counted, because nothing can match it.
  * A child session's frames are already filtered out by session id, and stay out.
* **E2. When it is reported.** Only a live turn reports the total, as
  `SessionCost{Amount: ticks / 1e10, Currency: "USD"}` on the usage event that turn already
  emits.
  * A replayed turn, marked `_meta.isReplay` or arriving while `session/load` runs, adds to the
    sum but reports nothing.
  * Until the first live turn after a relaunch, the daemon shows the total it stored before
    (0189 D2).
* **E3. An unknown cost stops the total.** If a turn's usage has no `costUsdTicks`, or says
  `costIsPartial`, acpagent stops reporting a session cost for that agent process. The token
  counts are still reported. The phone keeps its last known total, as 0189 D3 does for any report
  without one.
* **E4. Nothing else changes.**
  * The daemon, the protocol and the phone are unchanged: `session_cost` already flows and shows.
  * `CostUSD` stays unset for grok, as 0189 D1 requires.
  * `/usage` keeps calling the ledger.
* **E5. The subagent question is recorded, not guessed.** The PLAN's live step checks the sum
  against grok's `usage.json` after a plain turn. A turn that runs a subagent waits for the
  owner's quota, and its result is recorded here.

### Consequences

* Good, because a grok session's cost reaches the phone and `/context`, in dollars, labelled as
  the session's total.
* Good, because the figure is right after a relaunch: the replay rebuilds it, and the stored total
  covers the gap until the next live turn.
* Good, because the change is one function in acpagent and its tests.
* Neutral, because a resumed session's total only refreshes on its next live turn.
* Bad, because if grok ever stops replaying `turn_completed` on load, a relaunched session would
  count from zero and report too low a total. The fixture test pins today's replay, and a grok
  version bump re-records it (0184's capture rule).
* Bad, because if parent turns exclude subagent spend, a session that used subagents is
  under-counted until E5's probe decides how to fix it.

### Confirmation

* An acpagent test replays the committed `turn_completed` frames under `isReplay`. It requires no
  session cost during the replay. On the first live turn after it, it requires the sum of all
  four turns.
* The same test requires that a repeated `prompt_id` counts once, and that a turn with no cost, or
  a partial one, stops further session costs while token usage continues.
* Each new test is seen failing first, and planted breaks fail, on a scratch copy.
* Live, with the owner's quota: after one turn on a loaded session, the reported total equals the
  `costUsdTicks` grok's `usage.json` holds.

## Pros and Cons of the Options

### Add up every turn, replays included; report on live turns

* Good, because it matches grok's total exactly (measured: 181,628,000 ticks both ways).
* Good, because it needs no daemon or protocol change, and keeps 0189's replace rule.
* Bad, because it depends on grok replaying `turn_completed` with its cost on load.

### Report each live turn's cost; the daemon adds it up

* Good, because it does not depend on replay.
* Bad, because the daemon's stored total would mean "the sum of what Magic saw". A session used
  from grok's own terminal, or before this change, would be under-counted.
* Bad, because it gives `session_cost` a second meaning: an accumulated delta for grok, and the
  agent's own total elsewhere. 0189 chose against that.

### Read grok's ledger after each turn

* Good, because the ledger is grok's own total, and it includes subagent spend.
* Bad, because it resets to zero in a new grok process (measured). After a relaunch, the first
  report would replace the stored total with a smaller one.
* Bad, because it adds a request per turn.

### Read grok's `usage.json`

* Good, because it holds the full total and survives restarts.
* Bad, because it is a private file in grok's store. Its path and format are not an interface,
  and the daemon may not even run where the file is.

### Keep showing nothing

* Good, because nothing changes.
* Bad, because the cost of every grok session stays hidden although grok reports it.

## More Information

* The probe scripts and captures are in the session scratch directory. They are not committed,
  except the three replayed frames the PLAN adds to the 1.0.50 capture.
* The PLAN is
  [0199-PLAN-total-grok-per-turn-costs-into-the-session-cost.md](0199-PLAN-total-grok-per-turn-costs-into-the-session-cost.md).

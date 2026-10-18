---
status: proposed
date: 2026-10-18
associated-madr: "0199-MADR-total-grok-per-turn-costs-into-the-session-cost.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement totalling grok's per-turn costs into the session cost

Associated MADR: [0199-MADR-total-grok-per-turn-costs-into-the-session-cost.md](0199-MADR-total-grok-per-turn-costs-into-the-session-cost.md)

* The owner accepts an option and approves the PLAN by name.
* Go files pass `make pre-add-check`.
* The owner commits (`git commit --no-edit`). An agent stages only.
* G4's live step spends quota, and waits for the owner's word.

## Goal

A grok session's `session_cost` is grok's own running total, the same before and after a relaunch,
counted once per turn. It stops when a turn's cost is unknown.

## Scope

* `internal/provider/acpagent/xaiusage.go`, and the `turn_completed` branch in `subagents.go`: the
  sum, the replay rule, and the stop.
* `internal/provider/acpagent/xaiusage_test.go`: new tests.
* `internal/provider/grok/testdata/wire/1.0.50/frames.jsonl` and `meta.json`: the three replayed
  `turn_completed` frames from the MADR's load capture.
* Out of scope: the daemon, the protocol, the phone, `/usage`, and subagent spend beyond E5's
  probe.

## Implementation Steps

### G1. Fixture and tests, first (E1–E3)

1. Append the three replayed `turn_completed` frames, verbatim, to the 1.0.50 capture. Update its
   `meta.json` to 19 frames and say what they are. Before appending, check that they carry no path,
   account or e-mail. `TestGrok1050FramesDecodeAsStandardConfigOptions` skips frames that are not
   config options, and must still pass.
2. Write the tests against today's code, and see them fail:
   * `TestReplayedTurnCostsRebuildTheSessionTotal`: replay the three frames while loading. No
     usage event may carry `SessionCost`. Then a live `turn_completed` with 10,000,000 ticks must
     carry `SessionCost{Amount: 0.0191628, Currency: "USD"}`, which is 191,628,000 ticks.
   * `TestARepeatedTurnCountsOnce`: the same `prompt_id` twice adds once.
   * `TestAnUnknownTurnCostStopsTheSessionTotal`: a live turn without `costUsdTicks`, and in a
     second case one with `costIsPartial: true`. That turn and every later one carry no
     `SessionCost`, while their token usage is still emitted.
   * `TestALiveTurnWithoutAPromptIDStillCounts`.

### G2. The sum (E1–E3)

1. `xaiUsage` gains `CostUsdTicks *int64` and `CostIsPartial bool`. The `turn_completed` branch
   also reads `prompt_id` and `_meta.isReplay`.
2. The session keeps `costTicks int64`, a `countedPrompts` set, and `costUnknown bool`, guarded by
   the lock that already guards usage state.
3. On each `turn_completed` from this session:
   * a cost that is absent or partial sets `costUnknown`;
   * otherwise, an uncounted `prompt_id`, or an empty one, adds the cost.
4. A live turn, not `isReplay` and not loading, attaches `SessionCost` to the usage event
   `emitXAIUsage` sends, unless `costUnknown` is set. A replayed turn sends its token usage as
   today, with no `SessionCost`.

### G3. Prove it

1. G1's tests pass.
2. Planted breaks, each on a scratch copy, must each fail a G1 test:
   * the `prompt_id` check removed;
   * `SessionCost` sent on replayed turns;
   * `costUnknown` ignored;
   * ticks converted with the wrong factor (1e9).
3. `make pre-add-check` on the changed Go files, `go vet ./...`, `make lint`, `make staticcheck`,
   `go test -race ./...`, `make ci-windows`, and `check_records.py --check-all`.

### G4. Live (E5), with the owner's quota

1. Zero-token: load the 0187 phone-check session in a fresh grok, with capture. Confirm the replay
   still carries the three costs.
2. One short live turn on that loaded session. The `session_cost` it reports must equal the new
   `costUsdTicks` in grok's `usage.json`, divided by 1e10.
3. Separately, and only if the owner approves it: one turn that runs a subagent. Compare the sum
   with `usage.json` and with `_x.ai/session/usage`, and record the answer in the MADR.

## Verification

The MADR's Confirmation: G1's tests, G3's planted breaks, and G4's live match.

## Rollout and Rollback

* acpagent only. A grok session starts showing its cost in the context chip's tooltip and in
  `/context`, from its next live turn.
* Rollback is reverting the commit. The stored totals on `Meta` stay as they were, and are shown
  until the agent reports again (0189 D2).

## Execution record

Not started.

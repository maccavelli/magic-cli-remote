---
status: complete
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

Approved 2026-10-18 ("committed, proceed").

### Deviation 1, 2026-10-18: replays come on `_x.ai/session/update`

**Found.** In G1 step 1, the append script found no `turn_completed` frames on
`_x.ai/session_notification` in the load capture. All three are on `_x.ai/session/update`, which
grok's table does not register. The script stopped before writing anything, so the tree was
unchanged.

**Decision (owner, 2026-10-18): "Add a replay-only handler".** The MADR's amendment of the same
date records the fact and the decision. This PLAN's changes:

* **Scope** adds `internal/provider/grok/grok.go` (one table entry and its comment) and the new
  handler in `internal/provider/acpagent/xaiusage.go`.
* **G1** appends the three frames under their real method. Its replay test delivers them through
  the new handler, as grok does.
* **G1** adds `TestReplayChannelDeclinesOtherVariantsByName`: a replayed `background_tasks` on
  `_x.ai/session/update` emits nothing and is declined by name once.
* **G3** adds a planted break: the handler emits a usage event for a replayed turn.

### G1, 2026-10-18: fixture and tests

**Step 1.**

* The three replayed `turn_completed` frames, verbatim from the load capture, were appended to
  `testdata/wire/1.0.50/frames.jsonl`. `meta.json` now counts 19 frames and describes them.
* The script asserted, before writing, that the frames are on `_x.ai/session/update`, that each is
  `isReplay` and has a `prompt_id`, and that their costs are 118,673,600, 31,320,800 and
  31,633,600. It also checked that none carries a path, an account or an `@`.
* `TestGrok1050FramesDecodeAsStandardConfigOptions` and the `wirecap` fixture guard still pass.

**Step 2.** The five tests are in `internal/provider/acpagent/xaicost_test.go`:

* the four the PLAN names;
* Deviation 1's `TestReplayChannelDeclinesOtherVariantsByName`.

The replay test also covers a turn that grok marks `isReplay` on the live channel after loading
has ended. Such a turn adds to the sum and reports nothing.

Against today's code the tests do not build, because the handler does not exist. So, on a scratch
copy, they were run against an empty `HandleXAISessionUpdate`. All five failed, each for its own
reason:

```text
--- FAIL: TestReplayedTurnCostsRebuildTheSessionTotal
    xaicost_test.go:117: SessionCost = <nil>, want {Amount:0.0191628005 Currency:USD}: the replayed turns and the live one
--- FAIL: TestARepeatedTurnCountsOnce
    xaicost_test.go:132: SessionCost = <nil>, want {Amount:1e-08 Currency:USD}: a repeated prompt_id counted twice
--- FAIL: TestALiveTurnWithoutAPromptIDStillCounts
    xaicost_test.go:145: SessionCost = <nil>, want {Amount:2e-08 Currency:USD}
--- FAIL: TestAnUnknownTurnCostStopsTheSessionTotal
    xaicost_test.go:165: first turn reported no session cost
--- FAIL: TestReplayChannelDeclinesOtherVariantsByName
    xaicost_test.go:196: background_tasks was not declined by name; declined = map[]
```

The expected total is 191,628,005 ticks: the three replayed turns, plus 5 ticks from the
replay-marked live-channel turn, plus the live turn's 10,000,000.

### G2, 2026-10-18: the sum

`xaiusage.go`:

* **`xaiUsage`** gains `CostUsdTicks *int64` and `CostIsPartial`.
* **`addTurnCost`** keeps the sum in integer ticks under `s.mu`, with:
  * the `prompt_id` set;
  * the `costUnknown` stop.
* **`emitXAIUsage(u, replay)`** attaches `SessionCost{ticks / 1e10, "USD"}` only when the turn is
  not `isReplay`, the session is not loading, and the cost is not unknown.
* **`HandleXAISessionUpdate`**:
  * reads `turn_completed` costs into the sum, with the same origin rule as the live channel;
  * emits nothing;
  * declines every other variant as `_x.ai/session/update.<variant>`.

The live channel's handler (`subagents.go`) reads `prompt_id` and `_meta.isReplay`, and calls
`addTurnCost` before emitting. The three new fields sit beside `loading` in `session.go`. grok's
table in `internal/provider/grok/grok.go` registers `_x.ai/session/update`.

### G3, 2026-10-18: proof

Each planted break ran on a fresh scratch copy. All five failed a new test, and none was a build
failure:

```text
## prompt_id check removed: exit=1
   xaicost_test.go:132: SessionCost = &{Amount:2e-08 Currency:USD}, want {Amount:1e-08 Currency:USD}: a repeated prompt_id counted twice
## session cost sent on replays: exit=1
   xaicost_test.go:110: a replay-marked turn reported session cost {Amount:0.0181628005 Currency:USD}
## costUnknown ignored: exit=1
   xaicost_test.go:169: turn with an unknown cost: SessionCost &{Amount:1e-08 Currency:USD}, Used 10; want none and 10
## wrong tick factor (1e9): exit=1
   xaicost_test.go:117: SessionCost = &{Amount:0.191628005 Currency:USD}, want {Amount:0.0191628005 Currency:USD}: the replayed turns and the live one
## replay channel emits: exit=1
   xaicost_test.go:102: the replay channel emitted 3 events, want none: [{Type:usage_update ... Replay:true ...
```

```text
make pre-add-check (xaiusage.go, subagents.go, session.go, xaicost_test.go, grok.go)
                                            -> 5 file(s) clean (gofmt, golint, govulncheck)
go vet ./..., and go vet -tags live_grok ./internal/provider/grok/ -> exit 0
make lint, make staticcheck                 -> exit 0
staticcheck v0.8.2 -tags live_grok (grok, acpagent) -> exit 0
go test -race -count=1 ./...                -> exit 0, 42 packages ok
make ci-windows (the Windows test host)     -> ci-windows-exit=0, ALL SELECTED CHECKS PASSED;
                                               acpagent and provider/grok ok
```

My scratch harness for the Windows run failed once, before reaching the host, because its scratch
directory did not exist. It was rerun after creating the directory.

### G4 step 1, 2026-10-18: answered by the MADR's load capture

The same day's capture, on grok `1.0.50`, holds the three replayed costs. G1 commits those frames.

~~**Not done.** G4 steps 2 and 3 send turns, and wait for the owner's quota. The PLAN stays `in-progress`.~~ Both ran on 2026-10-18 (below).

### G4 steps 2 and 3, 2026-10-18: complete

The owner approved the quota on 2026-10-18 ("0199 is approved to proceed with quota usage"). A
`live_grok` test ran both steps on grok `1.0.50`, from a scratch copy of the tree. The test was not
committed.

**Step 2.** The 0187 phone-check session was loaded in a fresh grok, and one turn was sent.

```text
usage.json before:  costUsdTicks 181628000, turnCount 3
usage replay=false used=17999 session_cost=0.0300988400 USD
usage.json after:   costUsdTicks 300988400, turnCount 4  -> $0.0300988400
```

The reported total equals grok's own total exactly. It covers the three replayed turns and the
live one.

**Step 3.** A new session was asked to delegate a sum to one subagent. grok called
`spawn_subagent`, and Magic published `subagents` events.

```text
usage replay=false used=48338 session_cost=0.0233253600 USD
ledger: "Usage: 48156 input + 182 output tokens (19200 cached) · 116 reasoning · 2 turns · $0.0233"
parent usage.json: costUsdTicks 233253600, turns 1
child  usage.json: costUsdTicks  81124000, turns 1
```

The ledger folds the subagent's turn in ("2 turns"), and its cost equals the parent's own total
and Magic's sum. So a parent turn's `costUsdTicks` includes the spend of the subagents it ran:
about $0.0081 of the $0.0233 here. The MADR's E5 question is answered, and nothing is
under-counted.

Every criterion is met, so this PLAN is `complete`.

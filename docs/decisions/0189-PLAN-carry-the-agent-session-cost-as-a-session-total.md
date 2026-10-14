---
status: proposed
date: 2026-10-14
associated-madr: "0189-MADR-carry-the-agent-session-cost-as-a-session-total.md"
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Implement carrying the agent session cost as a session total

Associated MADR: [0189-MADR-carry-the-agent-session-cost-as-a-session-total.md](0189-MADR-carry-the-agent-session-cost-as-a-session-total.md)

**Not approved for execution.** The MADR is `proposed`. This PLAN implements its recommended option,
"Carry the agent's cumulative cost as a separately named session total, with its currency" (D1–D5).

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

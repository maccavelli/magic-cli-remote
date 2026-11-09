---
status: proposed
date: 2026-11-09
associated-madr: "0243-MADR-show-exactly-which-usage-limit-an-agent-hit-and-when-it-lifts.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement showing exactly which usage limit an agent hit and when it lifts

Associated MADR: [0243-MADR-show-exactly-which-usage-limit-an-agent-hit-and-when-it-lifts.md](0243-MADR-show-exactly-which-usage-limit-an-agent-hit-and-when-it-lifts.md)

## Goal

When this plan is done:

* gobble's quota error reads as `quota`, with its scope, upstream, code, reset and the provider's own words (MADR D1
  to D4, Confirmation 1);
* an `error` event carries `limit_scope`, `limit_window_minutes`, `limit_provider` and `limit_code` when known (D5);
* codex's two rate-limit windows reach the phone by name (D6);
* the phone's limit card titles the limit by scope and says when it lifts (D8);
* a gobble-cli issue holds the requirements for structured data (D7), and, once a gobble release sends it, the gobble
  Spec reads it (P4).

## Scope

* **Go:** `internal/agenterr/` (D1 to D4); `internal/event/event.go` (D5); `internal/provider/acpagent/` (emitting the
  fields, and the Spec hook of D3); `internal/provider/codex/session.go` (D6); `internal/provider/gobble/` (P4 only).
* **Dart:** `apps/mobile/lib/data/protocol/models.dart`, `lib/data/chat/chat_models.dart` and
  `lib/data/chat/transcript_reducer.dart` (the fields), `lib/features/chat/chat_bubble.dart` (`_LimitNotice`), and
  their tests.
* **Docs:** `docs/guides/protocol-v1.md` (the four fields).
* **Records:** this pair; `docs/README.md`; a note on 0179's PLAN pointing here for its P3 finding.
* **Outside this repository:** one gobble-cli issue and one go-llmprovider-sdk issue (P0 step 3; MADR Q1). Nothing
  in either repository's code or records is written by this plan.
* **Not in scope:** MADR D9's list.

## Conventions

* **C1. Start state.** Each phase starts on a clean `master`, or names what differs.
* **C2. Seen failing first.** Each new test runs against today's code and fails for the reason it exists; planted
  breaks run in a scratch clone, never in the tree.
* **C3. Go gates:** `make pre-add-check FILES=…` on every changed Go file, `make test`, `go test -race ./...`,
  `make lint`, `make staticcheck`, `make vet-tags`, output redirected and the exit status read before any filter.
* **C4. Dart gates** (P3): `dart format --output=none --set-exit-if-changed .` over `apps/mobile`, `flutter analyze`,
  `flutter test`.
* **C5. Staging.** The agent stages; the owner commits with `git commit --no-edit`. The redact preview runs before
  each staging. Nothing is pushed by the agent.
* **C6. Quota.** No phase spends provider quota. Test inputs are captured or recorded strings.
* **C7. Deviations** stop the phase, and are recorded with a date, and in the MADR when a decision or fact changes.

## Implementation Steps

### P0 — The records, and the gobble-cli issue

1. This pair, `proposed`, with the owner's answers to Q1 to Q3 recorded in the MADR when given.
2. `check_records.py --write-index`; `make check-records markdownlint-docs`; the redact preview. **Stage.**
3. After the owner accepts the MADR: open the gobble-cli issue with the MADR's "Requirements for gobble-cli" 1 to 5,
   the captured error of F1 as the example, and this record's name; and the go-llmprovider-sdk issue with
   requirement 6 (MADR Q1), each linking the other. Record both numbers here and in the MADR. Each issue's text is
   shown to the owner before it is posted.

### P1 — `agenterr` (MADR D1 to D4)

1. **Seen failing on today's code** (C2): a test with 0179's captured gobble error, asserting Confirmation 1's
   values. It fails: kind `server`, no reset, the fixed copy.
2. **D1:** `jsonDataText` reads an object `data`'s string `error`, `message` or `reason`.
3. **D2:** `parseReset` reads `retry-after <Go duration>` first; table tests for `1h33m21s`, `2h9m59.87321s`, `45s`,
   `250ms`, and the older formats unchanged.
4. **D3:** `Classification.Limit` (`Scope`, `WindowMinutes`, `Provider`, `Code`, `ProviderMessage`); the code table
   and the window phrases; an exported `Limit` type a Spec hook can return. Table tests, including text that states
   no scope and a long retry-after that must not become a window.
5. **D4:** the wrapper removal; tests with gobble's chain, grok's 401 shape and an OpenCode "insufficient balance"
   message, each keeping the provider's own words.
6. Every existing `agenterr` test passes unchanged; planted breaks for D1 to D4, in a scratch clone. C3. **Stage.**

### P2 — The wire, `acpagent` and codex (MADR D5, D6)

1. **`event.Event`** gains `LimitScope`, `LimitWindowMinutes`, `LimitProvider`, `LimitCode` (JSON `limit_scope`,
   `limit_window_minutes`, `limit_provider`, `limit_code`, each `omitempty`).
2. **`acpagent`**: the prompt-error path (`emitClassifiedTurnErrorWith` and the queued-prompt path) copies the
   classification's limit into the event. A Spec hook, `ErrorLimit func(raw string) (agenterr.Limit, bool)`, is
   consulted first, so an agent's structured data wins (D3 source 1). No Spec sets it in this phase.
3. **Codex (D6):** both windows read; the one at 100% sets the scope and window; a weekly window of 10,080 minutes is
   `weekly`; the 90% notice names its window.
4. **`protocol-v1.md`:** the four fields, beside `error_kind` and `retry_at`, with the scope values and the
   degrade-unknown rule.
5. Tests, each seen failing: an event carries the fields; the hook wins over the text; codex's 300-minute and
   10,080-minute windows. C3. **Stage.**

### P3 — The phone's card (MADR D8)

1. The protocol and chat models read the four fields; an event without them decodes as today.
2. `_LimitNotice`: the title by scope; the upstream when known; the reset as a time and how long from now; `credits`'
   "does not reset by itself"; the full provider message.
3. Widget tests for each scope, the reset phrase, `credits`, no scope (today's titles), and an unknown scope value
   (degrades to today's card), each seen failing first. C4. **Stage.**

### P4 — gobble's structured data (MADR D7)

**Gate:** a gobble-cli release whose acpext carries the issue's structured `ErrorData`. Until then this phase waits,
and P5 may close the rest.

1. Bump `github.com/maccavelli/gobble-cli/acpext` to that tag; `go mod tidy`; nothing else enters the module graph.
2. The gobble Spec sets `ErrorLimit`: decode the error's `data` as acpext's `ErrorData` and map `limit`,
   `retryAfterMs`/`resetAt`, `provider`, `code` and `providerMessage` onto `agenterr.Limit`.
3. Tests from acpext's recorded frames for the new fields, seen failing with the hook unset. C3. **Stage.**

### P5 — Close-out

1. The MADR's Confirmation 1 to 7, each with its evidence; CI on the push.
2. A dated note on 0179's PLAN (its P3 finding) pointing here.
3. This plan `complete` (or P4 recorded as waiting on gobble-cli, with the plan left `in-progress`), the MADR
   `accepted`; `check_records.py --write-index`; `make check-records`. **Stage.**

## Verification

| MADR Confirmation | Phase | Check |
| :--- | :--- | :--- |
| 1 gobble's captured error, end to end | P1 | steps 1 to 5 |
| 2 durations, codes, phrases, wrappers, no inference | P1 | steps 3 to 5 |
| 3 codex's windows | P2 | step 3 |
| 4 the protocol fields, and an older event | P2, P3 | P2 step 4; P3 step 1 |
| 5 the card | P3 | step 3 |
| 6 the gobble-cli and go-llmprovider-sdk issues | P0 | step 3 |
| 7 gates and CI | every phase | C3, C4; P5 step 1 |

## Rollout and Rollback

* **Rollout:** additive on the wire; an older phone shows today's card, with the better `error` and `retry_at` from P1.
  Ships with the next daemon and app releases.
* **Rollback:** revert a phase's commit. P1 alone is safe to keep without P2 or P3.
* **Risk:** low to moderate. P1 changes a classifier every agent passes through; its existing tests stay unchanged and
  must pass.

## Execution record

Nothing executed yet.

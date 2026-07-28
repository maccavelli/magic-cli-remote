# Protocol contract completeness: implementation plan

**Status:** Implemented 2026-07-27
**Date:** 2026-07-27
**Decision:** [MADR 0036](./0036-protocol-contract-completeness.md)
**Evidence:** audit of `docs/protocol-v1.md` against the tree at `92372a9`

## Goal and non-goal

Make `protocol-v1.md` a contract a third-party client could implement from, and
make the two enumerable surfaces (event types, error codes) impossible to
silently drift again. Fix the one live defect the audit surfaced along the way.

**Non-goals.** No new event types. No wire-format changes beyond D1's added
error event. No generated documentation. No attempt to mechanically verify
field-level *accuracy* — the guards prove presence only (MADR 0036 §2.6).

## Dependency order

Phase 1 is the only code-behaviour change and the only user-visible defect, so
it goes first and alone. Phase 3 must precede phase 4's error-code guard (the
guard enumerates the registry phase 3 creates). Phase 2 is independent.

```
Phase 1 (error pairing + render) ──┐
Phase 2 (doc completeness) ────────┼──> Phase 5 (verify)
Phase 3 (error-code registry) ─> Phase 4 (drift guards) ─┘
```

---

## Phase 1 — P0: `error` is paired once and rendered once

**Files:** `internal/provider/codex/session.go`,
`apps/mobile/lib/data/chat/transcript_reducer.dart`,
`internal/provider/codex/turn_complete_test.go`,
`apps/mobile/test/transcript_reducer_test.dart`

### Daemon

1. In `emitTurnComplete(stop, turnErrMsg)`, when `stop == "error"` and
   `turnErrMsg == ""`, emit the `TypeError` anyway with a generic message
   (e.g. `"The agent's turn failed."`). The invariant becomes: an `error` stop
   is always accompanied by an error event.
2. Update the function comment to state the invariant and cite MADR 0036 D1.
3. Leave acphttp alone — it already satisfies the invariant
   (`acphttp/session.go:398-414`, `err` is non-nil by construction).

### Client

4. In `_onTurnComplete`, treat `error` like `end_turn` — append no system item.
   Comment it with the invariant it relies on, so a future reader does not
   "restore" the bubble.

### Tests

5. **Invert** `TestEmitTurnCompleteErrorNoMessageSkipsTypeError` → an `error`
   stop with an empty message still emits three events (turn_complete, error,
   session_status), with a generic error text. Rewrite its comment to record
   why the earlier decision was reversed.
6. Keep the existing non-empty-message test green (real message preserved).
7. Mobile: `stop_reason: "error"` appends **no** system item; an `error` event
   still appends its own item; `cancelled` still appends "Turn cancelled" once.

**Acceptance:** a failed codex turn produces exactly one user-visible line, and
it carries the failure text.

**Rollback:** revert both halves together — the client suppression is only safe
with the daemon pairing.

---

## Phase 2 — P1: close the documentation gaps

**Files:** `docs/protocol-v1.md`

1. **`stop_reason` vocabulary** (MADR 0036 D2) — replace the open "e.g." list
   with the closed table, including `error`'s pairing invariant and each value's
   expected client rendering.
2. **`usage_update` section** — payload with `usage: {used, size}`; state that it
   is advisory telemetry (droppable, per `event.go`'s `IsControl` comment) and
   that a stale count self-corrects on the next report.
3. **`session_config` section** — payload with `config_options[]`:
   `{id, name, description?, kind, current_value?, bool_value?, values?}` and
   `values[] = {id, name}`. Cross-reference `session.set_config_option`, and
   state which `kind` values are legal.
4. **`session_title` section** — add to the `Event type values` list and document
   the `title` field.
5. **`question_request` / `question_resolved`** — add both to the event-type list
   (behaviour is already described under `question.respond`).
6. **`timed_out`** — document on `permission_resolved`, including that it
   accompanies `status: cancelled`, and correct the "carries no options and no
   tool_id" sentence so it is no longer exhaustive-sounding.
7. **Error codes** — document all of them, grouped, including the twelve missing
   and the `writeSessionErr` overrides (`session_forbidden`, `session_not_live`,
   `session_limit`, `shutting_down`, `turn_busy`, `bad_agent`) which can replace
   any per-message fallback code.
8. **Provider enum** — `fake`, `grok`, `opencode`, `goose`, `codex`; extend the
   `model` guidance for codex (per-turn) and goose.
9. **Pass-through** (D5) — record on both `tool_status` and `stop_reason` that an
   unrecognised native value is emitted as-is rather than fabricated, and that
   clients must degrade gracefully.
10. **`attachments`** — document as an event field on `user_message`
    (`{kind, mime_type?}`).

**Acceptance:** every type in the event list has a section or field entry; every
error code the server can emit appears somewhere in the document.

---

## Phase 3 — P1: error codes become a registry

**Files:** `internal/protocol/`, `internal/ws/server.go`

1. Add an error-code block to `internal/protocol` — one named constant per code,
   documented, plus an exported `ErrorCodes()` slice for the phase-4 guard.
2. Refactor `ws/server.go` to use the constants at every `writeError` /
   `writeSessionErr` call site, including the `writeSessionErr` override switch.
3. No behaviour change: the wire strings stay identical.

**Tests:** existing WS tests must pass untouched — they assert on wire strings,
which is exactly the regression guard for a mechanical rename.

**Acceptance:** no string literal error codes remain in `server.go`.

---

## Phase 4 — P1: drift guards

**Files:** `internal/protocol/` (or a small `docs` test package)

1. **Event-type coverage** — read `docs/protocol-v1.md`, locate the
   `Event type values:` line, assert every `event.Type` constant appears in it.
   Failure message names the missing type and the line to update.
2. **Error-code coverage** — assert every `protocol.ErrorCodes()` entry appears
   somewhere in the document.
3. Both tests locate the document relative to the package directory so they work
   from any working directory.

**Acceptance:** deleting `session_title` from the doc fails test 1; adding a new
error code without documenting it fails test 2.

**Note:** these prove presence, not accuracy (MADR 0036 §2.6). Say so in the
test comments so nobody mistakes a green run for a verified spec.

---

## Phase 5 — verification and finalisation

1. `make preflight` (fmt, vet, lint, staticcheck, vulncheck), `make race`,
   `make test-all`, `flutter test`.
2. Set MADR 0036 to Accepted with an implementation record.

---

## Delivery order

| # | Phase | Priority | Effort | Blocks |
|---|---|---|---|---|
| 1 | `error` pairing + single render | P0 | 1 h | — |
| 2 | Documentation completeness | P1 | 1-2 h | phase 4 guards |
| 3 | Error-code registry | P1 | 1 h | phase 4 |
| 4 | Drift guards | P1 | 1 h | — |
| 5 | Verification | P1 | 30 min | — |

## Commit boundaries

One commit per phase. Phase 1 must not be folded into phase 2 — it is the only
behaviour change and the only one whose rollback matters.

## Definition of done

- A failed turn renders exactly one line, carrying the failure text.
- Every event type in the enumeration has a section or field entry.
- Every server error code is documented and constant-backed.
- Removing a documented type or code fails a named test.
- `make preflight`, `make race`, `make test-all`, `flutter test` all green.

# Codex chat UI/UX remediation: implementation plan

**Status:** Proposed
**Date:** 2026-07-27
**Decision:** [MADR 0035](./0035-codex-ui-ux-remediation.md)
**Evidence:** [Report 0032 rev 2](./0032-codex-ui-ux-polish-report.md)
**Verified target:** codex-cli 0.145.0 (`~/.local/bin/codex`, the pinned spike
version). Re-probe phase 0 before accepting a newer minor version.

## Goal and non-goal

Make the codex chat surface tell the truth: render only what is a tool, offer
only commands that work, disclose the capabilities the provider actually has,
and stop losing the last words of a turn.

**Non-goals.** No plan/todo panel until its wire shape is probed (phase 8). No
codex mode switching until the set path is verified. No new event types. No
mobile-side tool-card folding — it already exists (`transcript_rows.dart:47-83`).

## Dependency order

Phase 1 is first because it is the only *destructive* defect — every turn a user
spends on the current build risks losing a conversation to `/model`. Phases 2-5
are independent. Phase 6's conformance tests must land after phase 5 (they
encode the corrected tables) but before any further provider work.

```
Phase 0 (probe) ──────────────────────────────────────> Phase 8 (plan panel, gated)
Phase 1 (/model, destructive) ─┐
Phase 2 (item allowlist) ──────┤
Phase 3 (tool status) ─────────┼─> Phase 6 (guards) ─> Phase 7 (hardening) ─> Phase 9 (docs)
Phase 4 (command table) ───────┤
Phase 5 (caps + turn end) ─────┘
```

Phases 2 and 3 both change what the transcript shows and should land together in
review even though they are separate commits.

---

## Phase 0 — P0: probe the unknowns

Three facts the design needs and the spike does not have. codex 0.145.0 is
installed, so this is a scripted probe.

1. **Item-type inventory per turn.** Run turns that exercise chat, a shell
   command, a file edit, a web search, and an MCP tool. Record every
   `item/started` / `item/completed` `itemType` observed and its `item` payload
   shape. This validates phase 2's allowlist against reality rather than against
   `thread_item_types` alone.
2. **The `plan` item.** Run a turn that produces a plan (a multi-step task).
   Capture the `item/started` payload for `itemType: "plan"` and any update
   notification. Gates phase 8 entirely.
3. **`account/rateLimits/updated` params.** Capture one. Gates phase 7 step 3.

Commit as `docs/codex-spike-0.145.0/item-stream.json` alongside the existing
captures, recording the CLI version. Pin the two load-bearing facts with a
`live_codex`-tagged test: the item types that carry a tool-like payload, and
that `item/completed` arrives for every `item/started`.

**Gate:** phase 2's allowlist is reconciled against this capture before merge.
Phase 8 does not start without item 2.

---

## Phase 1 — P0: stop `/model` destroying conversations

Smallest change with the largest downside removed. Split from phase 4 so it can
ship immediately.

**Files:** `internal/provider/codex/commandtable.go`, `internal/provider/codex/session.go`

1. Change `"model"` to `{Kind: command.KindOp, Op: command.OpSetModel}`.
   This routes `/model X` to `cmdModel(..., inPlace=true)` → `setModelInPlace`
   → `SetModel`, which keeps the thread.
2. **Add validation to `SetModel`** (`session.go:480-486`). Today it accepts any
   string and mutates `s.opts.Model`; the failure surfaces one turn later at
   `turn/start`. Under `KindOp` the daemon reports *"Model is now X — the
   conversation is kept"*, so an unvalidated typo becomes a lie. Validate
   against `p.ListModels(ctx)` (`provider.go:73`, `model/list`) and return an
   error — `setModelInPlace` already surfaces it as
   *"Model switch to X failed: …"* (`commands.go:692-695`).
   - Treat a `model/list` RPC failure as *permit* (log and proceed), not as
     reject: an engine hiccup must not block a legitimate switch.
3. Confirm the notice wording is accurate for codex. The model is per-turn
   (`session.go:310-317`), so the switch lands on the **next** turn. If the
   existing copy reads as immediate, adjust it in `setModelInPlace` or accept
   it — either is fine, but decide deliberately.

**Tests** (`internal/provider/codex/provider_test.go` or a new `model_test.go`):

- `SetModel` with a name in the catalog succeeds and updates `opts.Model`.
- `SetModel` with an unknown name returns an error and leaves `opts.Model`
  unchanged.
- `SetModel` when `model/list` fails permits the change (fail-open) and logs.
- `beginTurn` sends the updated model on the next `turn/start`.
- Table test: `/model` resolves to `KindOp`/`OpSetModel` for codex.

**Acceptance:** `/model <valid>` keeps the conversation; `/model <typo>` reports
a failure and changes nothing.

**Rollback:** revert the table entry to `KindDaemon`; validation is inert but
harmless.

---

## Phase 2 — P0: explicit item-type allowlist

**Files:** `internal/provider/codex/session.go`

1. Add `itemsRenderedAsTools` (MADR 0035 §2.1) near the notification handler,
   reconciled against the phase-0 capture.
2. **`item/started`** (`:592-684`): keep the four typed arms; replace the
   `default:` arm with:
   - member of the set → generic tool card, with a real `ToolKind` where known
     (`mcpToolCall` → `other`, `webSearch` → `fetch`, `dynamicToolCall` →
     `other`) so `classifyTool` groups it correctly;
   - `contextCompaction`, `enteredReviewMode`, `exitedReviewMode` →
     `TypeNotice`, one line each;
   - `plan` → silence for now (phase 8 replaces this);
   - anything else → `s.log.Debug(...)` only.
3. **`item/completed`** (`:685-701`): replace the three-name deny-list with a
   membership test against the **same** set. This is the point of the change —
   one source of truth, so the handlers cannot drift.

**Tests** (new `internal/provider/codex/item_test.go` — there is currently **no**
fixture covering `item/started` at all):

- Table test over all 18 `thread_item_types`: which emit `TypeToolCall`, which
  emit `TypeNotice`, which emit nothing.
- **Lifecycle invariant**: for every item type that emits a `tool_call`, a
  matching `item/completed` emits a `tool_call_update` with a terminal status.
  Assert no card is left non-terminal after a synthetic turn — this is the exact
  bug being fixed.
- `agentMessage`, `userMessage`, `reasoning`, `plan` emit no tool card.
- An unknown future item type emits nothing and does not panic.

**Acceptance:** a synthetic turn containing `agentMessage`, `reasoning`,
`commandExecution` and `plan` items produces exactly one tool card
(the command), and it reaches a terminal status.

**Risk:** an item type that *should* render is now silent. Mitigated by
reconciling with phase 0 and by the debug log.

**Rollback:** restore the `default:` arm.

---

## Phase 3 — P1: tool status vocabulary

**Files:** `internal/provider/codex/session.go`

1. Replace `"in_progress"` with `"running"` at all six sites
   (`:614,631,649,669,680,714`).
2. Grep the package for any remaining `in_progress` to confirm none is a tool
   status (`event.PlanStatusInProgress` is the plan vocabulary and stays).

**Tests:**

- Assert emitted tool events use only `pending|running|completed|failed`.
- Mobile (`apps/mobile/test/transcript_rows_test.dart`): a tool with status
  `running` is **not** grouped (spinner stays visible) — the regression guard
  for the premature-folding symptom.

**Acceptance:** codex tool cards show a spinner while running and do not fold
into "Ran N commands" until finished.

**Rollback:** trivial revert.

---

## Phase 4 — P1: correct the command tables

**Files:** `internal/provider/codex/commandtable.go`, `internal/provider/goose/commandtable.go`

1. Replace the codex table with MADR 0035 §2.3 (phase 1 already moved `model`).
2. Fix goose's `"mode": {Kind: KindDaemon}` → `KindNone` with a reason. Same
   bug, and phase 5's test will fail without it.
3. Re-read every `Note` as user-facing copy — it is displayed verbatim
   (`command.go:143-151`, `commands.go:178`). No "probe required", no "not
   supported"; each note says what the user can do instead.

**Tests** (`internal/provider/codex/` + `internal/command/`):

- `/compact` resolves to `KindOp`/`OpCompact` and reaches `Compact`.
- `/context` is unavailable before the first usage report and available after
  (it is gated on `lastUsage != nil`, `commands.go:53`).
- `/mode`, `/plan`, `/goal`, `/diff`, `/undo`, `/redo` resolve unavailable with
  a non-empty note.
- No note contains "probe", "TODO", or "not supported".
- goose `/mode` resolves unavailable rather than dead-ending.

**Acceptance:** `/help` on a codex session lists only commands that work, and
every unavailable one gives an actionable reason.

**Rollback:** revert the tables.

---

## Phase 5 — P1: restore hidden functionality (D4, D5)

Two independent changes, one commit each.

### 5a — Emit `session_capabilities` at session create (D4)

**Files:** `internal/provider/codex/session.go`, `internal/provider/codex/provider.go`

1. Add `emitCapabilities()` mirroring `acpagent/session.go:1083-1095`, and call
   it once from the session-create path (`provider.go:300` → `newSession`,
   after the agent session id is known so the event carries it).
2. Values, each justified rather than assumed:
   - `Image: true` — the provider implements image input end to end
     (`session.go:26,1183-1217`) and all six models advertise
     `inputModalities: [text,image]` (MADR 0028 §16.3).
   - `Audio: false` — codex advertises no audio modality.
   - `LoadSession: true` — `thread/resume` verified OK (MADR 0028 §16.3).
   - Leave the ACP-specific fields (`EmbeddedContext`, `MCPHTTP`, …) false;
     codex has no equivalent negotiation and claiming them would be a guess.

**Tests:**

- A created session emits exactly one `session_capabilities` with `image: true`.
- A `live_codex` test pinning that the model catalog still reports image input
  — this is a claim about an external CLI, so per AGENTS.md it needs a live test.
- Mobile (`apps/mobile/test/`): a transcript with `capabilities.image == true`
  shows the attach affordance; absent capabilities hides it (guards the
  `?? false` fallback at `chat_screen.dart:1597`).

**Acceptance:** the image-attach button appears on a codex session and an
attached image reaches the agent.

### 5b — One turn-completion emitter with the full status enum (D5)

**Files:** `internal/provider/codex/session.go`

1. Add `codexStopReason` (MADR 0035 §2.5) mapping `completed`→`end_turn`,
   `interrupted`→`cancelled`, `failed`→`error`, default pass-through.
2. Route the `turn/completed` handler (`:518-549`) through `emitTurnComplete`
   instead of duplicating the emit inline, so the cancel/error paths
   (`:330-332`) and the notification path share one implementation. Keep the
   handler's own state reset (`turnBusy`, `turnID`, `steerable`) and
   `tryDrainQueue`.
3. On `failed`, emit `TypeError` carrying `turn.error` — the wire payload has
   the field (spike capture: `"error": null` on a success) and it is currently
   discarded.
4. This phase also removes the redundant `drainChunks()` at `:527` as a
   consequence of consolidating — coordinate with phase 7 step 1 so the deletion
   happens once, in whichever lands first.

**Tests:**

- Table test over all four `turn_status` values asserting the emitted
  `StopReason`.
- `failed` also emits `TypeError` with the engine's message.
- Reducer test (`apps/mobile/test/transcript_reducer_test.dart`): `end_turn`
  appends no `ChatItem`; `cancelled` appends "Turn cancelled" exactly once.
- Only one `turn_complete` is emitted per turn (guards the two-emitter bug).

**Acceptance:** a normal codex turn ends with no system bubble; an interrupted
one shows "Turn cancelled"; a failed one shows the error.

**Rollback:** each half reverts independently.

---

## Phase 6 — P1: guards that would have caught phases 1-5

The most valuable phase: it converts this whole defect class into test failures.

**Files:** `internal/session/commands.go`, `internal/command/conformance_test.go`

1. **Refactor `runCanonical`'s `KindDaemon` switch into a table**
   (`commands.go:220-233`) so dispatch is data the test can read:

   ```go
   // daemonCommands maps canonical names the daemon implements itself to their
   // handlers. Single source of truth for KindDaemon dispatch: the
   // cross-provider conformance test asserts every KindDaemon mapping a
   // provider declares has an entry here (MADR 0035 D6).
   var daemonCommands = map[string]func(*Manager, context.Context, string, string, string) error{
       "help":     func(m *Manager, _ context.Context, id, _, _ string) error { m.emitNotice(id, m.helpText(id)); return nil },
       "model":    func(m *Manager, ctx context.Context, id, dev, rest string) error { return m.cmdModel(ctx, id, dev, rest, false) },
       "clear":    func(m *Manager, ctx context.Context, id, dev, _ string) error { return m.cmdReset(ctx, id, dev) },
       "new":      func(m *Manager, ctx context.Context, id, dev, rest string) error { return m.cmdNew(ctx, id, dev, rest) },
       "sessions": func(m *Manager, _ context.Context, id, dev, _ string) error { return m.cmdSessions(id, dev) },
   }
   ```

   `runCanonical` looks up the map; the "not wired up in this build" fallback
   stays as the programming-error path it was always meant to be. Export
   `DaemonCommandNames() []string` for the test.

2. **Dispatch conformance** — for every registered provider, every `KindDaemon`
   mapping must have a `daemonCommands` entry. This is the test that produced:

   ```
   DEAD-END: codex /compact, codex /mode, codex /context, goose /mode
   ```

3. **Capability conformance** — when a provider's session type implements
   `CompactSession` / `ModelSession` / `DiffSession` / `UndoSession` /
   `RevertSession`, its table must route the corresponding command through
   `KindOp`, not shadow it with `KindDaemon`. This is what would have caught
   `/model`. Resolve the session type via the provider's declared session
   implementation; where that needs a live session, assert on the concrete type
   instead (`var _ provider.ModelSession = (*session)(nil)` style) and have the
   test read a small per-provider capability manifest.

4. Add `codex` to `TestTablesAreKeyedByCanonicalName` (`:90-95`) — the only
   registered provider missing.

5. **Chunkbuf boundary regression** (guards phase 7): assert
   `TypeTurnComplete` still drains the pending run and returns `blocking=true`,
   with a comment pointing at MADR 0035 §2.7 and MADR 0034 D3.

**Acceptance:** reverting any of phases 1-5 makes a test fail with a message
that names the command and the provider.

**Note:** the conformance test is in `package command_test` (external), so it may
import `internal/session` without a cycle — `internal/command` does not import
`internal/session`.

---

## Phase 7 — P2: hardening

**Files:** `internal/provider/codex/session.go`, `internal/provider/httpagent/session.go`

1. **D7 — drop the redundant turn-boundary drains.** Delete `s.drainChunks()`
   at `session.go:527` and `:976`. `emit(TypeTurnComplete)` already drains the
   pending run through the chunkbuf boundary, in order, on the blocking path —
   proven by test (MADR 0035 §2.7). The explicit call only gets there first with
   the non-blocking `trySend`, which is the sole loss path.
   - **Keep** the `Close` drain (`:392`) — no control event follows there. A
     blocking send is unsafe (`close(s.done)` is later, at `:416`), so keep
     `trySend` and log the byte count `Unflush` returns.
   - `httpagent`'s single call (`session.go:731`) is close-path only: it needs
     the logging change, not the deletion.
2. **D8 — stall detection without per-notification timers.** Replace
   `resetStallTimer()` (`:515`, `:1154-1176`) with a `lastActivity atomic.Int64`
   updated on every notification, plus one per-session ticker that emits the
   notice when `now - lastActivity > stallNotice` and `turnBusy`. Stop the
   ticker in `Close`.
3. **D9 — wire the two discarded notifications** (`:739-740`):
   `account/rateLimits/updated` → the existing limit-card contract
   (`error_kind`, `retry_at`); `mcpServer/startupStatus/updated` → a
   `TypeNotice` on failure only. Shape from the phase-0 capture.

**Tests:**

- Turn-complete emits the buffered tail **before** `turn_complete`, in order,
  with a full-ish event channel (the drop scenario).
- `Close` with a full channel logs a non-zero dropped-byte count.
- Stall notice fires once after the configured idle period and not at all while
  deltas keep arriving.
- Rate-limit notification maps to `TypeError` with `error_kind: rate_limit`.

**Acceptance:** `make race` green; no goroutine or timer leak across
create/prompt/close cycles.

---

## Phase 8 — P3: plan/todo panel (gated on phase 0 item 2)

`WorkItemsPanel` already exists and is provider-agnostic; only the daemon side
is missing.

1. Add a `case "plan"` to `item/started` (and to whatever update notification
   the probe reveals) emitting `event.TypePlan`.
2. Normalize to the daemon's fixed vocabulary — statuses
   `pending|in_progress|completed`, priorities `high|medium|low`
   (`event.go:119-131`) — mirroring `acpcommon/plan.go:7-20` and
   `opencode/todo.go:20-28`, both of which coerce unknown values to
   `pending`/`medium` rather than passing them through.
3. Empty plan emits `TypePlan` with an empty **non-nil** slice (the clear path),
   matching opencode.
4. Delete the dead `case "turn/plan/updated"` arm (`:737-738`), or re-target it
   to the same handler with a comment recording that it was never observed on
   0.145.0.

**Tests:** fixture from the phase-0 capture; status/priority normalization table;
empty-clear path; the item no longer produces a tool card (phase 2 regression).

**Do not start without the capture.** The payload shape is unknown, and report
0032 rev 1 already burned a design cycle guessing it.

---

## Phase 9 — documentation and verification

1. **`docs/protocol-v1.md`** — specify the `tool_call` / `tool_call_update`
   `status` vocabulary: `pending | running | completed | failed`. Its absence
   (`:733-746` documents only `tool_kind`) is why phase 3's drift went unnoticed.
   opencode's `mapToolStatus` (`http.go:1092-1105`) is the reference. **Shared
   with MADR 0034 phase 5** — whichever lands first writes it; the other
   cross-references.
2. **MADR 0028** — add an implementation note that the codex provider's
   command table, item handling and capability disclosure were corrected here,
   so a reader of 0028 does not re-derive the old behaviour.
3. **MADR 0035** — set to Accepted with the date; add an implementation record
   noting what phase 0 measured and what phase 8 did or did not unblock.
4. Full verification: `make preflight`, `make race`, `make test-all`,
   `flutter test`, plus the phase-0 live probe re-run against 0.145.0.

---

## Delivery order

| # | Phase | Priority | Effort | Rationale for position |
|---|---|---|---|---|
| 0 | Probe item stream, plan item, rate-limit params | P0 | 2-3 h | Gates 2 and 8 |
| 1 | `/model` in-place + validation | P0 | 1-2 h | Only destructive defect |
| 2 | Item-type allowlist | P0 | 2-3 h | Largest visible noise source |
| 3 | Tool status → `running` | P1 | 30 min | Pairs with 2 in review |
| 4 | Command tables (codex + goose) | P1 | 1 h | Depends on nothing |
| 5 | Capabilities (D4) + turn completion (D5) | P1 | 2-3 h | Restores hidden function |
| 6 | Dispatch + capability conformance | P1 | 2-3 h | Encodes 1-5; blocks future drift |
| 7 | Hardening (D7, D8, D9) | P2 | 2-3 h | No user-visible defect |
| 8 | Plan panel | P3 | 2-3 h | Gated on phase 0 |
| 9 | Docs and verification | P1 | 1 h | Last |

Roughly 15-22 h including the probe, excluding phase 8 if the capture does not
land.

## Commit boundaries

One commit per phase. Phase 6's `daemonCommands` refactor must not be folded
into phase 4 — it touches the shared command router and needs to be revertible
on its own. Phases 2 and 3 are separate commits but one review.

## Definition of done

- No transcript card is ever titled `agentMessage`, `reasoning`, or `plan`.
- Every tool card that opens reaches a terminal status.
- Codex tool cards show a spinner while running and fold only when finished.
- `/model` keeps the conversation and rejects unknown model names.
- `/compact` and `/context` work; every unavailable command gives an actionable
  reason with no internal TODO text.
- The image-attach button appears on codex sessions.
- Turns end with no "Turn ended (…)" bubble; a failed turn reports its error.
- Reverting any of phases 1-5 fails a named conformance test.
- `protocol-v1.md` specifies the tool status vocabulary.
- `make preflight`, `make race`, `make test-all`, `flutter test` all green.
- Phase-0 capture committed and pinned with a live-tagged test.

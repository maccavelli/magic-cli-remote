# Android app remediation: implementation plan

**Status:** Proposed
**Date:** 2026-07-27
**Decision:** [MADR 0042](./0042-android-app-remediation.md)
**Evidence:** [audit 0041](./0041-android-app-debug-audit.md), measured at `66d6f1e`

## Goal and non-goal

Close every finding in audit 0041, fixing the reported OpenCode symptoms first
(janky, bursty, tool calls not counted, collapse-and-redraw, jitter) and the
backlog after.

**Non-goals.** No R8 / resource shrinking (needs ProGuard keep rules and a
device smoke test — its own change). No new event types. No change to the
`session.history` / resync protocol. No daemon change beyond the tool lane in
phase 6.

## Dependency order

Phases 1–3 are the user-visible fixes and are ordered by value-per-risk. Phase 4
**must not ship before phase 3**: phase 3's narrowed memo removes the constant
full rebuilds that currently mask the stale key index, so shipping the memo
change without the index fix would surface remounts on the tool path (MADR 0042
D3, audit O6).

```
Phase 1 (batch tool_call + fold updates) ─┐
Phase 2 (scroll ownership) ───────────────┤
Phase 3 (row model: one live group) ──────┴─> Phase 4 (memo + key index)
                                                      │
Phase 5 (lifecycle, a11y, reducer, platform, cleanup) ┤
Phase 6 (daemon tool lane) ───────────────────────────┴─> Phase 7 (verify)
```

Phases 1, 2, 5 are independent of each other and of 3/4.

---

## Phase 1 — P0: coalesce the tool ingest path

**Closes:** O3, O8 (MADR 0042 D2)
**Files:** `apps/mobile/lib/state/transcripts_notifier.dart`

1. Add `'tool_call'` to `_isBatchableEvent`. Rewrite the block comment: it
   currently claims the opening `tool_call` "gates an affordance or a state
   machine", which is false — record that it only appends an item, that nothing
   outside the reducer reads it, and that the 32 ms delay is deliberate.
2. Extend `_foldChunks` to collapse **consecutive** `tool_call_update` events
   sharing the same non-empty `toolId`, keeping the **last**. Guard rails, each
   worth its own test:
   - never fold across a `tool_call` for the same id;
   - never fold events with an empty `toolId` — `transcript_reducer.dart:462`
     folds those into the most recent tool card by arrival order;
   - keep the last of a run, so a terminal `completed`/`failed` can never be
     dropped;
   - a different `toolId` between two updates breaks the run.
3. Leave `_isNeutralInFold` alone. Tool updates legitimately separate text runs.

**Tests**

- 5 `tool_call` + 5 `tool_call_update` staged in one window commit **once**
  (today: 6). Use `debugOnEventBatch` — `debugOnEvent` flushes per event and
  cannot show coalescing.
- 10 updates to one tool fold to one apply; assert via
  `debugAppendChunkCount`-style counting or by asserting the resulting item.
- A run ending in `completed` still yields `completed`.
- Interleaved ids `[A,B,A]` do **not** fold.
- Empty-`toolId` updates are never folded.
- Ordering: a `permission_request` arriving mid-window still flushes pending
  tool calls before it applies.

**Acceptance:** a parallel tool fan-out costs one commit per window.
**Rollback:** revert both edits; they are independent of every other phase.

---

## Phase 2 — P0: stop cancelling the user's scroll

**Closes:** O5 (MADR 0042 D5)
**Files:** `apps/mobile/lib/features/chat/chat_screen.dart`

1. In `_scrollToEnd`'s post-frame callback, return early when
   `_scroll.position.pixels == 0` (already pinned — the jump is a no-op that
   still calls `goIdle()` and kills any activity) or when `_listScrolling.value`
   is true (the user is dragging or a fling is in flight).
2. Comment it with the mechanism: `jumpTo` → `goIdle()` cancels the current
   `ScrollActivity`, so an unguarded auto-follow yanks the list out from under a
   gesture.
3. Auto-follow needs no re-arming: the next append after the gesture ends jumps
   normally, and `_userNearBottom` already decides whether it should.

**Tests**

- With `_listScrolling` true, an append does not change `pixels`.
- With it false and `pixels > 0` but inside the 120 px band, an append still
  pins to 0 (the follow behaviour is preserved, not disabled).
- A drag started at the live end survives an append mid-gesture — the
  regression test for the reported jitter.

**Acceptance:** the transcript can be scrolled during a tool burst.
**Rollback:** one guard clause.

---

## Phase 3 — P0: one live group per contiguous tool run

**Closes:** O1, O2 (MADR 0042 D1)
**Files:** `apps/mobile/lib/data/chat/transcript_rows.dart`,
`apps/mobile/lib/features/chat/chat_bubble.dart`,
`apps/mobile/lib/features/chat/transcript_pane.dart`

This is the behavioural change. Ship it alone.

1. **`buildTranscriptRows`** — replace the run rule with: every consecutive run
   of `ChatItemKind.tool` items is one `GroupRow`, regardless of `ToolClass` and
   regardless of status. Concretely: delete the `runClass` split, delete the
   `groupable`/`flushRun()` path for live tools, and lower the threshold from
   `run.length >= 2` to `>= 1`. Non-tool items still break the run.
2. **`GroupRow`** — replace the single `toolClass` field with the member list as
   the source of truth, and add:
   - `runningItem` — first member with `toolRunning`, else null;
   - `title` from the histogram in MADR 0042 D1 (single → the tool's own name;
     all commands → `Ran N commands`; all edits → `Edited N files`; mixed with
     commands → `Ran N commands +M more`; mixed without → `Used N tools`);
   - `dominantClass` for icon selection.
3. **`_ToolGroupTile`** —
   - at `n == 1`, render today's single-tool affordance (name as title,
     expandable to detail) so nothing regresses visually;
   - when `runningItem != null`, show its name plus the running indicator beside
     the summary;
   - do **not** surface streaming output in the collapsed head — it would reflow
     the row on every delta (MADR 0042 §3). Sticky `_groupExpanded` already
     covers watching a build.
4. **`_memoTranscriptRows`** — `canFoldIntoGroup` becomes simply
   `newItem.kind == ChatItemKind.tool` (status and class no longer matter), so a
   tool append never takes the append fast path while every non-tool append
   still does.

**Tests** — `:51`, `:56` and `:69` encode the old rule and invert. Rewrite them
with a comment recording that MADR 0042 D1 reversed the decision and why:

- `transcript_rows_test.dart:51` "a lone finished tool stays a single row" →
  **a lone tool is a group of one**, titled with the tool's own name.
- `:56` "a class change breaks the run into separate groups" → **read + grep +
  edit form one group** titled `Used 3 tools`.
- `:69` "a running tool never folds into a group" → **a running tool joins its
  group** and is exposed as `runningItem`.
- `:82` "runs split by the live tool merge once it completes" → delete; that
  split no longer happens.
- `:42` (`Ran 2 commands`) and `chat_render_test.dart:420` **must keep passing
  unchanged** — they are the guard that D1 preserved the label.
- `:122` (`Used 2 tools`) and the `failedCount` test at `:110` keep passing.
- **New, the regression test for the report:** replay
  `read, grep, bash, read, edit, bash` through `pending → running → completed`
  and assert the row count is **constant** and no row key ever changes. Measured
  today as 3→4→3→4→5→6→7 with one re-key per fold; the proposed rule holds at 3
  with zero re-keys.
- **New:** the group count is monotonically non-decreasing across a burst, and
  includes the running member.
- **New:** mixed-run title arms — `Ran 2 commands +4 more`, `Used 5 tools`.

**Acceptance:** the audit's six-tool sequence renders one stable group row whose
count and live head update in place — no collapse, no re-key, no row churn.

**Rollback:** phase-local, but it changes user-visible behaviour — revert the
whole phase rather than parts.

---

## Phase 4 — P1: honest memo, incremental key index

**Closes:** H1, O4, O6 (MADR 0042 D3). **Requires phase 3.**
**Files:** `apps/mobile/lib/features/chat/transcript_pane.dart`

1. Change `_memoTranscriptRows`'s second return value from `sameKeys` to a small
   result describing what happened — unchanged / appended one row / rebuilt —
   so the caller can react correctly instead of being told a falsehood. Update
   the doc comment, which currently states the contract the code violates.
2. On "appended one row", extend the index instead of rebuilding it:
   `_keyIndex = {..._keyIndex, _rowKeyValue(rows.last): rows.length - 1}`.
   Existing entries stay valid — an append does not shift any earlier row's
   forward index, and `findChildIndexCallback` already converts to the reversed
   child index using the current `rows.length`.
3. Narrow the second fast path: replace `newLast.kind != ChatItemKind.tool` with
   a groupability check — same `toolClass`, and same `toolRunning` (which the
   phase-3 live head displays). A `bash` streaming output while running then
   takes the cheap path.

**Tests**

- **The H1 regression test, which does not exist today:** appending assistant
  bubbles one at a time re-parses **exactly one** bubble per append. Measured
  today as 1, 2, 3, 4, 5, 6 — assert `debugMarkdownParseCount == 1` for each of
  six successive appends.
- A no-op rebuild parses 0.
- A tool detail update that does not change groupability does **not** trigger a
  full `buildTranscriptRows` — assert via a temporary counter or by asserting
  row-list identity is preserved.
- A tool update that flips `toolRunning` **does** rebuild.

**Acceptance:** per-append cost is independent of how many rows preceded it.
**Rollback:** revert to a full `_keyIndex` rebuild on every append (correct,
just O(rows)) before reverting phase 3.

---

## Phase 5 — P1: the audit backlog

**Closes:** H3, M1–M8, L1–L7 (MADR 0042 D6–D10). Five independent commits.

### 5a — Staged image lifecycle (H3 / D6)

`transcripts_notifier.dart`: clear `_sentImages` in `clearSession`, `clearAll`,
`onDispose`, and `syncFromMeta`'s eviction sweep — matching the six sibling maps
already pruned there.
**Test:** stage bytes, `clearSession`, assert the queue is empty; and that a
later `user_message` echo for that session attaches nothing.

### 5b — Notification overlay (M2, M3, M7, M8 / D7)

`theme/top_notification.dart`, `theme/celestial.dart`: wrap the body in
`Semantics(container: true, liveRegion: true)`; add a short FIFO so a burst does
not discard all but the last (cap ~3, drop oldest); resolve colour/shape from
`Theme.of(context).snackBarTheme` and type from `textTheme` instead of
hardcoded `fontSize`/radius/elevation.
**Tests:** a `Semantics` node with `liveRegion` is present (the M2 regression
guard); two rapid notifications both appear; no hardcoded `fontSize` remains in
the file.

### 5c — Reducer consistency (M4, M5, M6 / D8)

`data/protocol/models.dart`, `data/chat/transcript_reducer.dart`,
`features/chat/chat_screen.dart`: value equality on `SessionMode` and
`ConfigOption` plus identical-instance returns for no-op `session_mode` /
`session_config`; `_clip` preserves newlines and backs off a low surrogate
(reuse the `chat_bubble.dart:572` technique, and apply it to `clipItemText`
too); `_noteUnreadIfScrolledUp` iterates backwards and breaks.
**Tests:** a repeated identical `session_mode` returns `identical` state (the
pattern `transcript_reducer_test.dart` already uses for tool updates); a
multi-line error keeps its line breaks; a clip landing on a surrogate pair
produces valid UTF-16.

### 5d — Platform wiring (M1, L5, L7 / D9)

`android/app/src/main/AndroidManifest.xml`: add
`android:enableOnBackInvokedCallback="true"` to `<application>`.
`android/app/build.gradle.kts`: remove the stale applicationId TODO; add an
opt-in lint path (a property-gated `checkReleaseBuilds`) so CI can catch
manifest regressions without burdening small build hosts.
**Verification is a device check, not a test:** on Android 13+, a back gesture
from chat must animate predictively. Record the result in the MADR's
implementation note — this is the one item the audit could not verify
statically.

### 5e — Dead code and revert (L1–L4, L6 / D10)

Delete `TopNotificationX` and `McremoteClient.listCommands`. Humanise
`updatedAt` in the resume-session picker (reuse the `chat_bubble.dart:319`
formatter rather than adding a second one). Prune `_groupExpanded` when items
age out of the cap.

**Remove the revert pair** (MADR 0042 D10 — decided, no longer open):

- delete `McremoteClient.revertMessage` (`mcremote_client.dart:1470`);
- delete `McremoteClient.unrevert` (`:1489`), `_unrevert()`
  (`chat_screen.dart:941`), the `'unrevert'` branch (`:1748`) and the
  "Restore reverts" menu item (`:1791`);
- leave the daemon's `session.revert` / `session.unrevert` handlers and their
  protocol rows in place — other clients may use them, and removing wire
  surface is out of scope here.

**Test:** the session-actions menu no longer offers "Restore reverts" for an
OpenCode session. Record in the commit message that revert is unreachable from
this client because `event.Event` carries no message id, so a future reader does
not "restore" the method.

---

## Phase 6 — P2: daemon tool lane

**Closes:** O7 (MADR 0042 D4). Ships last, alone.
**Files:** `internal/chunkbuf/chunkbuf.go`, `internal/provider/httpagent/session.go`

1. Add a per-`ToolID` pending slot to the buffer: a non-terminal `TypeToolUpdate`
   replaces any pending update for the same id and arms the existing coalesce
   deadline.
2. Never hold: a `TypeToolCall`, a terminal status (`completed`, `failed`), or
   any event when a control boundary flushes.
3. Tool events stay **non-droppable** — on a failed send they are handed back via
   the existing `Unflush` path, as chunks are.
4. Preserve relative order between a tool's own events; a held update for tool A
   must not overtake a later flushed event for tool A.

**Tests**

- Two non-terminal updates for one id inside a window emit **one** event,
  carrying the later payload.
- A terminal status flushes immediately and is never held.
- A `TypeToolCall` is never held.
- Updates for different ids do not collapse into each other.
- Interleaving with assistant chunks preserves order across the boundary.
- Live (`live_opencode`): a `bash` producing continuous output still ends with a
  `completed` tool card carrying the final output — the acceptance test for
  "coalescing never loses the terminal state".

**Risk:** this is the only phase that can lose information if wrong — a held
update whose flush never fires pins a card on "running". The live test is the
gate.
**Rollback:** the lane is additive; disabling it restores pass-through.

---

## Phase 7 — verification and finalisation

1. `make preflight`, `make race`, `make test-all`, `flutter test`,
   `flutter analyze`, `dart format --set-exit-if-changed`.
2. Re-run the audit's three measurements and record the new numbers in MADR 0042
   as an implementation note:
   - row trace for `read, grep, bash, read, edit, bash` — expect monotonic rows;
   - commits for 5 parallel tool calls — expect 1, was 6;
   - `buildTranscriptRows` calls for 6 tools — expect ≤ 2, was 12.
3. Device pass on Android 13+: predictive back (5d), a tool-heavy OpenCode turn
   for jitter/collapse, and TalkBack announcing a failed send (5b).
4. Update `docs/chat-performance.md` — its "Row fold memo" and "List element
   reuse" rows describe behaviour phases 3 and 4 change.
5. Set MADR 0042 to Accepted with the implementation record, including the
   device-verified predictive-back result from 5d.

---

## Delivery order

| # | Phase | Priority | Effort | Notes |
|---|---|---|---|---|
| 1 | Coalesce tool ingest | P0 | 2-3 h | Biggest win per line changed |
| 2 | Scroll ownership | P0 | 1 h | Fixes the jitter |
| 3 | One live group per tool run | P0 | 1-1.5 d | Behavioural; inverts 3 grouping tests |
| 4 | Memo + key index | P1 | 3-4 h | **Must follow 3** |
| 5 | Backlog (5a–5e) | P1 | 1 d | Five independent commits |
| 6 | Daemon tool lane | P2 | 4-6 h | Ships alone, live-test gated |
| 7 | Verification | P1 | 3 h | Includes a device pass |

Roughly 3.5–4.5 days.

## Commit boundaries

One commit per phase, and per sub-phase in 5. Phase 3 must not be folded into
any other: it is the only user-visible behaviour change and the only one whose
rollback is a product decision. Phase 6 stays separate — it touches the shared
`chunkbuf` used by every HTTP-transport provider, and its failure mode is a
stuck tool card.

## Definition of done

- The audit's six-tool OpenCode sequence renders **one** stable group row whose
  count and live head update in place: constant row count, zero re-keys, no
  collapse, no remount.
- `Ran N commands` still appears for a command-only run, and now also fires for
  OpenCode, where alternating classes make it unreachable today.
- A parallel tool fan-out costs one commit per 32 ms window.
- Appending a row re-parses exactly one bubble, regardless of transcript length.
- The transcript is scrollable during a tool burst.
- Staged image bytes do not outlive their session.
- A transient notification is announced by TalkBack, and a burst of two is not
  reduced to one.
- No comment in the tree claims `tool_call` gates an affordance, and no memo
  reports `sameKeys` while adding a key.
- Predictive back verified on a device, with the result recorded.
- `revertMessage`, `unrevert` and the "Restore reverts" menu entry are gone; no
  client method remains for a call the wire cannot carry.
- `make preflight`, `make race`, `make test-all`, `flutter test` green.

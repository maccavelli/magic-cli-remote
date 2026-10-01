# MADR 0042: Android app remediation — stable tool rows, coalesced ingest, and the audit backlog

- **Status**: Accepted — all phases implemented 2026-07-27 (see §6)
- **Date**: 2026-07-27
- **Deciders**: Project Owner
- **Related**: [0041](./0041-MADR-android-app-debug-audit.md) (the audit this closes),
  [MADR 0018](./0018-MADR-mobile-chat-performance-action-plan.md) (row memo, key
  index, batch window — D1/D2 amend it),
  [MADR 0024](./0024-MADR-stream-coalescing.md) (chunk coalescing — D4 extends its
  reasoning to tool events),
  [MADR 0034](./0034-MADR-opencode-tool-stream-fidelity.md) (tool dedup + in-place
  ordering, which stays),
  [MADR 0036](./0036-MADR-protocol-contract-completeness.md) (drift-guard pattern
  reused in D8)
- **Evidence**: [audit 0041](./0041-MADR-android-app-debug-audit.md), measured at
  `66d6f1e`
- **Companion plan**:
  [0042-PLAN-android-app-remediation.md](./0042-PLAN-android-app-remediation.md)

---

## 1. Problem

Audit 0041 found 18 issues in the Flutter client. Three are defects, eight are
medium, seven are low, and eight more (§9) explain a specific user report:
OpenCode sessions render janky and bursty, tool calls do not count into the
collapsed row, that row is "expanded then closed and redrawn" on every tool, and
the screen jitters.

The audit measured the causes rather than inferring them. This MADR turns those
measurements into decisions. The full finding→decision map is §4.

### 1.1 The transcript row model does not survive contact with OpenCode

`buildTranscriptRows` folds runs of **consecutive, finished tools of the same
`ToolClass`**. Both qualifiers fail in practice:

- **Same class.** The daemon's `kindForTool` (`opencode/http.go:1144`) spreads
  OpenCode's tools across all three classes — `bash`→`command`,
  `edit`/`write`→`fileEdit`, `read`/`grep`/`glob`/`webfetch`/`todowrite`→`other`.
  Real work alternates, so runs are length 1 and never reach the `>= 2`
  threshold. The fold is effectively dead code.
- **Finished.** A running tool is not groupable *and calls `flushRun()`*, so it
  breaks the run in progress. Every tool therefore renders as a full standalone
  card, then disappears into the adjacent group when it completes — and because
  the row key changes from `ValueKey(seq)` to `ValueKey('grp-$seq')`, that is a
  genuine element remount, not a repaint.

Measured over `read, grep, bash, read, edit, bash`: six tools produced six rows
and exactly one group of two, with the row count oscillating 4→3 at the one
fold. That is precisely the reported "tool calls do not count into the row" and
"expanded then closed and redrawn".

### 1.2 The ingest path coalesces text but not tools

`tool_call` is excluded from the 32 ms batch window
(`transcripts_notifier.dart:47`). Measured, in one window:

| Events staged | Commits | Ideal |
|---|---|---|
| 5 parallel `tool_call` + 5 `tool_call_update` | **6** | 1 |
| 10 `tool_call_update` | 1 | 1 |
| 10 `assistant_message_chunk` | 1 | 1 |

The text path is fully optimised; the tool path is not. The comment justifying
the exclusion claims `tool_call` "gates an affordance or a state machine" — it
does not. In the reducer it appends an item and nothing else; nothing outside
`transcript_reducer.dart` reads it, the composer gates on
`status`/`hasBlockingPrompt`, and the notification layer only handles
`permission_request` and `turn_complete`.

Compounding it, every tool event forces a full row fold plus a full key-index
rebuild, because the memo's second fast path excludes `ChatItemKind.tool`
wholesale. Measured: **12 folds for 6 tools**, 66 items scanned.

### 1.3 The list fights the user's finger

`_scrollToEnd()` calls `jumpTo(0)`, which begins with `goIdle()`
(`scroll_position_with_single_context.dart:198`) and therefore cancels any
in-flight drag or ballistic fling. It fires on every appended row while
`_userNearBottom` is true — a **120 px** band. During a tool burst that is
several times a second, so scrolling back through output is impossible: the list
snaps to the live end. This is the jitter.

### 1.4 A correctness bug hides behind an inefficiency

`_memoTranscriptRows`'s append fast path returns `sameKeys: true` while adding a
new key, contradicting its own documented contract. `_keyIndex` never learns
about rows appended that way, `findChildIndexCallback` returns null for them,
and Flutter leaves those elements on an index that now holds a different row —
destroying and re-inflating them instead of relocating. Measured: markdown
re-parses grow 1, 2, 3, 4, 5, 6 across successive appends.

During tool bursts this is **masked** by §1.2's constant full rebuilds, which
keep `_keyIndex` fresh. The two must be fixed together or fixing the
inefficiency will expose the bug.

---

## 2. Decisions

### D1 — One live group per contiguous tool run

**Closes O1, O2. Amends MADR 0018's row model.**

The open question in audit §9.4 — whether a long-running tool breaks out into
its own row, or stays inside the group with a live head — is decided in favour
of **staying in the group**. The point of the fold is a readable session flow:
one row per burst of agent activity, expandable on demand to see which tools
ran. Breaking any tool out works against that.

1. **Every maximal run of consecutive tool items is one group**, regardless of
   `ToolClass`. `read`, `grep`, `bash`, `edit` and `write` in one burst collapse
   to a single row. Dropping the same-class rule is what makes the fold engage
   at all for OpenCode.
2. **Running and pending tools are admitted immediately.** `flushRun()` on a
   live tool is removed, so a tool never renders as a standalone card and
   therefore never has to collapse into one later. The count is live: it
   includes the tool executing right now.
3. **The threshold drops from 2 to 1.** A run's identity is `grp-<first seq>`
   from its very first tool and never changes, so the row is never re-keyed —
   which is what eliminates the remount, not merely reduces it. At `n == 1` the
   group renders exactly today's single-tool affordance (tool name as the title,
   expandable to its detail), so nothing regresses visually; only the internal
   representation changes.
4. **The row carries a live head**: while any member is running, the collapsed
   row names it and shows the running indicator, so a collapsed group is never
   opaque about work in progress. Expansion is already sticky
   (`_groupExpanded`), so a user who wants to watch a build expands once and
   stays expanded as further tools stream in.
5. **Titles come from a class histogram**, leading with commands because those
   are the actions worth naming:

   | Run composition | Title |
   |---|---|
   | 1 tool | the tool's own name |
   | all commands | `Ran 3 commands` |
   | all edits | `Edited 3 files` |
   | mixed, contains commands | `Ran 2 commands +4 more` |
   | mixed, no commands | `Used 5 tools` |

The decisive property is that **run membership is decidable when a tool
arrives** and nothing is ever folded retroactively. Verified against the audit's
own six-tool sequence: the row count stays **constant at 3** through all 18
events (today: 3→4→3→4→5→6→7) with **zero** key changes on existing rows.

### D2 — `tool_call` joins the batch window; redundant tool updates fold

**Closes O3, O8.**

Add `tool_call` to `_isBatchableEvent`, and extend `_foldChunks` to collapse
**consecutive `tool_call_update`s carrying the same non-empty `toolId`** down to
the last one. Tool updates are replace-semantics — only the final state of a
given tool matters — which is the same argument that already justifies folding
text runs, in the same function.

Cost: up to 32 ms before a new tool card appears. That is below the perceptual
threshold and identical to what streamed text already accepts.

Constraints the fold must respect: never fold across a `tool_call` for the same
id; never fold updates with an empty `toolId` (the reducer's "fold into the last
tool card" path at `transcript_reducer.dart:462` depends on their arrival
order); and keep the last event of a run, so a terminal `completed`/`failed`
status can never be dropped.

### D3 — The row memo stops lying, and stops excluding tools wholesale

**Closes H1, O4, O6.**

1. `_memoTranscriptRows` reports appends honestly, and `_TranscriptPaneState`
   extends `_keyIndex` incrementally rather than rebuilding it — keeping the
   fast path's benefit without the stale-index bug.
2. The second fast path's blanket `newLast.kind != ChatItemKind.tool` exclusion
   narrows to **"groupability did not change"** — under D1 that means the
   tool's `toolClass` and, for the live-head display, its `toolRunning` flag are
   unchanged. A `bash` streaming output while still running (the common case)
   then takes the cheap path exactly as streamed text does.

These land together with D1 because D1 changes what "groupable" means, and
together with each other because 2 removes the masking that currently hides 1.

### D4 — Tool events coalesce on the wire, without becoming droppable

**Closes O7.**

`chunkbuf` coalesces only `IsChunk` (assistant/thought) events, and both
`TypeToolCall` and `TypeToolUpdate` are in `IsControl` — so a `bash` streaming
build output produces one blocking WebSocket frame per SSE delta, each carrying
up to `maxToolOutputChars` (8 000) characters.

Extend the buffer with a **tool lane**: hold at most one pending update per
`ToolID`, replacing on arrival, and flush it on the existing coalesce deadline.
Three invariants keep the control-event contract intact:

- A **terminal** status (`completed`, `failed`) flushes that tool immediately
  and is never held.
- A `TypeToolCall` (the opening event for an id) is never held.
- Tool events remain **non-droppable**: unlike telemetry they are handed back on
  a failed send, exactly as chunks are today via `Unflush`.

Only *superseded* intermediate states are dropped, which is what
"replace-semantics" already means and what `noteToolEmit` already does for
byte-identical repeats.

### D5 — `_scrollToEnd` never cancels a scroll the user is driving

**Closes O5.**

`_scrollToEnd` returns early when the transcript is already pinned
(`position.pixels == 0`, where the jump is a no-op that still cancels activity)
or when the user is actively scrolling. The signal for the latter already exists
and is already wired for the shimmer/pulse animations: `_listScrolling`
(`chat_screen.dart:159`, sensor at `:1952`). Auto-follow resumes on the next
append after the gesture ends.

### D6 — Staged image bytes are released with their session

**Closes H3.**

`_sentImages` is cleared in `clearSession`, `clearAll`, the notifier's
`onDispose`, and by `syncFromMeta`'s eviction sweep — the same rule already
applied to the six sibling maps beside it. Megabytes of image bytes must not
outlive the session they belong to, and must not survive "Clear saved
credentials".

### D7 — The notification overlay regains what the snackbar migration dropped

**Closes M2, M3, M7, M8.**

`TopNotification` gains `Semantics(container: true, liveRegion: true)`, matching
`SnackBar` (`snack_bar.dart:828`) so screen readers announce transient
messages again; a short FIFO so a burst does not discard everything but the
last; and its colours, shape and type resolved from
`Theme.of(context).snackBarTheme` and `textTheme` instead of hardcoded
literals. The now-unreachable `snackBarTheme` block in `celestial.dart` becomes
the single source for both, rather than being deleted.

### D8 — Reducer consistency and the remaining correctness items

**Closes M4, M5, M6.**

- `SessionMode` and `ConfigOption` get value equality, and the reducer returns
  the identical transcript on a no-op `session_mode` / `session_config` — the
  guard every sibling replace-semantics event already has.
- The error clipper preserves paragraph breaks and never cuts mid-surrogate,
  reusing the technique already present at `chat_bubble.dart:572`.
- The unread counter walks backwards from the tail and stops, instead of
  scanning all 800 items per append.

### D9 — Platform wiring

**Closes M1, L5, L7.**

`android:enableOnBackInvokedCallback="true"` is added to `<application>`, so the
`PredictiveBackPageTransitionsBuilder` the theme already selects actually
receives gestures instead of silently falling back. The stale
"Specify your own unique Application ID" template comment is removed. Android
lint stays disabled by default (the small-build-host rationale holds) but gains
an opt-in CI path, so manifest regressions of exactly M1's class are catchable.

### D10 — Dead code and the revert asymmetry

**Closes L1, L2, L3, L4, L6.**

Delete the unused `TopNotificationX` extension and `McremoteClient.listCommands`.
Humanise the `updatedAt` render in the resume-session picker. Prune
`_groupExpanded` alongside the transcript cap.

**`revertMessage` and `unrevert` are both removed**, along with the "Restore
reverts" menu entry. This was raised as a product decision; it is settled by a
fact the audit did not surface:

`session.revert` requires `message_id` — non-optional in the protocol table
(`protocol-v1.md:260`), unlike `session.fork` and `session.diff` where it is
`message_id?`. But `event.Event` carries **no message-id field at all**, so no
event the phone receives contains one, and `ChatItem.seq` is a reducer-assigned
local counter unrelated to any provider id. The client therefore cannot
construct a valid revert call. This is a missing protocol capability, not
unwired UI.

Supplying it would mean adding `MessageID` to `event.Event`, populating it per
provider, documenting it in `protocol-v1.md` with the MADR 0036 drift guard,
parsing it into `SessionEvent`, carrying it on `ChatItem`, and round-tripping it
through `TranscriptCache` — before any UI exists. Revert does not justify that:

- It is **destructive without preview on a phone**. OpenCode's revert rewinds
  file changes on disk and truncates the conversation, and the app cannot even
  show what would be undone — `sessionDiff` is session-scoped for the same
  missing-`message_id` reason.
- Its **recovery path is unverified**: whether `unrevert` restores files as well
  as messages is OpenCode semantics this repo does not document.
- **git does this better** — previewable, safer, already in the workflow, and
  already surfaced by `session.diagnostics`' VCS summary.
- The **non-destructive cousin already ships**: `fork` branches the conversation
  and leaves the session intact.

`unrevert` goes with it. With revert unreachable, "Restore reverts" can only
undo a revert performed by another client against the same OpenCode session — an
undo button for an action this app cannot perform.

**Kept in view, not built:** the same `message_id` plumbing would enable
**fork-at-message** and **diff-at-message**, both non-destructive and both
already accepting the parameter (`forkSession`/`sessionDiff` are called without
it today, so forking only ever branches from the session head). If that plumbing
is ever added, those are the payoff — revert is not.

---

## 3. Alternatives considered

| Option | Why not |
|---|---|
| **Commands break out into permanent standalone rows** | Considered and rejected by the Project Owner: it destroys the `Ran N commands` fold, whose entire purpose is a readable session flow with detail available on expansion. It also produces a wall of rows on a command-heavy turn — the original complaint — and inverts tests that assert the label deliberately. |
| **Promote a long-running tool out of the group on a timer** | Needs a clock in `buildTranscriptRows` (today pure and memoised) plus a ticker, and promotion is *retroactive*: the tool is counted in the group, then removed, so the group's count visibly decreases. Reintroduces the churn D1 exists to remove. |
| **Promote on evidence — first `running` update carrying output** | Pure and clock-free, and correlates well with "long" (a fast `read` completes before streaming). Same retroactive count-decrease as above, and it would not fire at all for providers that do not stream tool output, so behaviour would differ per provider for no user-visible reason. |
| **Keep same-class runs, just admit running tools** | Leaves O1 unfixed: OpenCode alternates classes, so runs stay length 1 and the wall of rows remains. |
| **Keep the `>= 2` threshold** | Cheaper, but the 1→2 transition re-keys the row from `ValueKey(seq)` to `ValueKey('grp-…')`, so one remount per run survives. Threshold 1 costs only an `n == 1` rendering branch and removes remounts entirely. |
| **Show the running tool's streaming output in the collapsed head** | The head would grow and shrink with the output, reflowing the row on every delta — reintroducing jitter for a benefit sticky expansion already provides. |
| **Make tool events droppable so `chunkbuf` can shed them** | A dropped terminal status leaves a tool card pinned on "running" forever. D4 coalesces without ever dropping a terminal state. |
| **Delete `snackBarTheme`** | The theme block is the more carefully specified of the two; D7 makes it load-bearing again instead of discarding the intent. |
| **Enable R8 / resource shrinking in this change** | Needs ProGuard keep rules and a device smoke test of camera, speech and notifications. Real, but its own change — see §5. |

---

## 4. Finding → decision map

Every finding in audit 0041 is accounted for.

| Finding | Severity | Decision |
|---|---|---|
| H1 stale key index | High | D3 |
| H2 composer behind keyboard | High | **Already fixed** (`472ceab`) |
| H3 staged image bytes retained | High | D6 |
| M1 predictive back not enabled | Medium | D9 |
| M2 no screen-reader announcement | Medium | D7 |
| M3 notifications replace, not queue | Medium | D7 |
| M4 `session_mode`/`session_config` no-op guard | Medium | D8 |
| M5 unread counter rescans | Medium | D8 |
| M6 error clipper | Medium | D8 |
| M7 unreachable `snackBarTheme` | Low-Med | D7 |
| M8 `TopNotification` bypasses tokens | Low-Med | D7 |
| L1 dead `TopNotificationX` | Low | D10 |
| L2 dead `listCommands` | Low | D10 |
| L3 `revertMessage` unwired | Low | D10 |
| L4 raw `DateTime` in picker | Low | D10 |
| L5 stale gradle TODO | Low | D9 |
| L6 `_groupExpanded` unpruned | Low | D10 |
| L7 Android lint disabled | Low | D9 |
| O1 grouping never engages | High | D1 |
| O2 retroactive collapse | High | D1 |
| O3 `tool_call` unbatched | High | D2 |
| O4 full fold per tool event | High | D3 |
| O5 scroll fights the user | High | D5 |
| O6 H1 fires on pending appends | Medium | D3 |
| O7 no wire coalescing for tools | Medium | D4 |
| O8 redundant updates unfolded | Medium | D2 |

---

## 5. Consequences

**Good.** The reported symptoms have direct fixes: no collapse artefact (D1), no
burst (D2/D4), no jitter (D5), and per-event cost proportional to what changed
rather than to transcript length (D3). Accessibility parity returns (D7).

**`Ran N commands` is preserved**, and now actually fires: today it is
unreachable for OpenCode because alternating tool classes keep every run at
length 1. `transcript_rows_test.dart:42` and `chat_render_test.dart:420` — the
tests that assert the label — keep passing unchanged.

**Accepted cost — the group title changes wording as a run's composition
changes**, e.g. `read` → `Used 2 tools` → `Ran 1 command +2 more`. The row
itself is stable (same key, same position); only its label updates. That is
inherent to summarising a live run and is a fair trade for one row instead of
six.

**Accepted cost — three grouping tests invert.** `transcript_rows_test.dart:51`
(a lone tool stays a single row), `:56` (a class change breaks the run) and
`:69` (a running tool never folds) all encode the old rule. They are rewritten,
not deleted, with comments recording that MADR 0042 D1 reversed them and why.
`:82` (runs split by the live tool merge once it completes) describes a split
that no longer happens and is removed.

**Accepted cost — an expanded group rebuilds its members when a tool arrives.**
`ExpansionTile` builds children only while expanded (`maintainState` defaults to
false), so this is bounded to the case where the user has opened the group. If a
50-tool expanded run proves slow, the follow-up is a lazy list for the group
body; not built now.

**Accepted cost — up to 32 ms before a new tool card appears** (D2), and up to
one coalesce interval before a non-terminal tool update reaches the phone (D4).
Both are below the perceptual threshold and match what streamed text already
accepts.

**Risk — D4 touches the control-event contract.** It is the only decision that
can lose information if implemented wrongly (a held update whose flush never
fires would pin a card on "running"). It ships last, alone, behind the three
invariants in D4 and a live test.

**Risk — D9's manifest attribute changes back-gesture behaviour** on Android
13+. It is one line and independently revertible, and needs a device check
rather than a unit test.

**Not in scope.** R8/resource shrinking (needs keep rules + device smoke test);
moving reader-facing docs into their own directory (raised by
[0040](./0040-MADR-markdownlint-assessment.md)); any daemon change beyond D4.

---

## 6. Implementation record

All seven phases implemented 2026-07-27. Every finding in §4 is closed.

### Measured outcome

The three measurements audit 0041 §9.2 took, re-run against the finished tree:

| Measurement | Before | Target | After |
|---|---|---|---|
| Rows across the 18-event OpenCode burst | 3→4→3→4→5→6→7 | constant | **3, unchanged throughout** |
| Commits for 5 parallel tool calls in one window | 6 | 1 | **1** |
| `buildTranscriptRows` calls for 6 tools | 12 | ≤ 2 | **0** |
| Markdown re-parses per successive append | 1, 2, 3, 4, 5, 6 | 1 each | **1 each** |

The row-fold count beat its target because phase 4 was extended during
verification: the append fast path now resolves a tool append from the trailing
row alone (extend that group, or start a new one) instead of falling through to
a full fold. Measured at 6 with only the phase-4 changes as planned, then 0.

Each fix was confirmed to fail without its change before being kept — the
auto-follow guard (drag position yanked `40.0 → 0.0`), the key index
(`append #2` re-parsed 2 bubbles), and the row model (the 4→3 collapse).

### Deviations from the plan

1. **`n == 1` group rendering.** Phase 3 initially only retitled a single-tool
   group, which dropped the status suffix a lone tool used to show and broke
   four tests. `_ToolGroupTile` now delegates to the standalone tool card at
   `n == 1`, as the plan's wording specified. The enclosing widget type is
   unchanged, so growing to two members rebuilds the subtree without remounting
   the row.
2. **The tool lane is opt-in.** `chunkbuf` is shared by httpagent, acphttp and
   codex. Enabling the lane for all three would have changed emission for two
   providers whose tool profiles were never measured, and required touching 7
   drain sites. `WithToolLane()` keeps the change to the plan's two files and to
   the transport the audit profiled; the other two keep the old pass-through.
3. **Superseding merges rather than drops.** D4 said only superseded states are
   dropped. Implementing it that way could lose a field: the client's upsert
   keeps a field only when the incoming one is empty, so an intermediate that
   set a tool's title or kind would lose it if a later update omitted both.
   `mergeTool` layers the newer event over the held one instead.
4. **`_humanTimestamp` is new, not reused.** The plan said to reuse
   `limitResetPhrase`; that function is future-facing ("in about 2 h") and lives
   behind `part of chat_screen.dart`, so it is not reachable or applicable from
   the sessions screen. The new helper uses the same `MaterialLocalizations` +
   calendar-day approach.
5. **`event.IsTerminalToolStatus` added.** The tool lane needs the terminal
   vocabulary; it was documented in `protocol-v1.md` but had no constants.

### Not done

**The device pass in phase 7 step 3 has not been run.** Three things need a real
Android 13+ device and are unverified:

- **D9 / M1 predictive back.** The manifest attribute is set and Gradle
  evaluates, but that the gesture actually animates is not confirmed. This was
  flagged as device-only in audit 0041 §8 and remains so.
- **D7 / M2 TalkBack.** A widget test asserts the `liveRegion` semantics node
  exists; that a screen reader announces it in practice is not confirmed.
- **The reported symptoms themselves.** Jank, burstiness and the collapse
  artefact are confirmed fixed by measurement, not by watching a live OpenCode
  turn on a phone.

*(Superseded — see "Live acceptance" below. The device pass above still stands.)*

### Live acceptance (run 2026-07-28)

`make live-opencode` — the full 19-test `live_opencode` suite against opencode
**1.18.7** — passed twice, 283s and 286s, no skips of the tool paths.

The phase-6 acceptance test did not exist and has been added:
`TestLiveToolLaneKeepsTerminalState`. The existing `TestLiveToolStreamDynamics`
asserts on **raw SSE frames**, upstream of `chunkbuf`, so it proves the pipeline
still works end to end but says nothing about coalescing. Nothing in the suite
asserted that an emitted tool card ends terminal.

Result on a `bash` streaming 12 ticks:

```
tool call_e828e1c8…: 16 raw frames -> 3 emitted events,
                     final status "completed" (86 bytes of detail)
verified terminal state survived coalescing for 1 tool(s)
```

**5.3× fewer WebSocket frames for one streaming tool, terminal state and payload
intact** — the D4 risk ("a held update whose flush never fires pins a card on
running") measured rather than argued. The test asserts the safety property and
only *logs* the compression ratio: the model decides how many frames a tool
emits, so pinning a ratio would flake on a quiet turn.

Two incidental findings, not fixed here:

- `TestLiveToolStreamDynamics` writes `docs/opencode-spike-1.18.5/tool-frames.json`
  with `cli_version` hardcoded to `"1.18.5"`, but the installed CLI is 1.18.7 —
  so a re-run files a **mislabelled** capture into a version-named directory.
  The regenerated file was reverted rather than committed. Fixing it means
  deciding whether the spike directory tracks the probed version or is a
  historical record; that is its own change.
- The capture does re-confirm on 1.18.7 that a tool's terminal status is always
  its last raw frame and `state.output` is non-decreasing — the MADR 0034
  assumptions still hold on the newer CLI.

### Commits

`4ed059c` phase 1 · `4fcd359` phase 2 · `7707a2f` phase 3 · `3312950` phase 4 ·
`9ef85a8` 5a · `6fc0772` 5b · `27f0dd8` 5c · `834db67` 5d · `6a666b6` 5e ·
`fe4dbbb` phase 6 · `d5a564a` phase 4 extension · `29d1039` live acceptance test.

Green at each step: `make preflight`, `make race`, `make test-all`,
`flutter analyze`, `dart format --set-exit-if-changed`, and the Flutter suite
(341 → **358** tests).

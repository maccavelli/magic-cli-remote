# MADR 0018: Mobile session chat performance, stability & polish

- **Status**: Accepted (Phases A–D shipped; Phase E follow-ons implemented 2026-07-23)
- **Date**: 2026-07-23
- **Deciders**: Project Owner
- **Locked decisions**: D1b, D2b, D3b, D4b, D5b, D6b, D7b, D8b (after B), D9 → local last-N cache (E1), D10b, D11a
- **Context**: Deep dive of the Flutter Android session chat after the 2026-07
  stability pass (`docs/chat-performance.md`, reverse-list architecture).
  Lenses: **caching / buffers / FPS**, **scroll correctness**, **memory & GC**,
  **robustness under reconnect/history**, **look & feel / polish**.
- **Scope**: `apps/mobile` session chat path primarily:
  - `lib/features/chat/chat_screen.dart`
  - `lib/data/chat/*`
  - `lib/state/transcripts_notifier.dart`
  - `lib/theme/{scroll_activity,widgets}.dart`
  - tests under `apps/mobile/test/*{chat,transcript,stream,history}*`
  - companion docs: `docs/chat-performance.md`, this MADR
- **Out of scope (explicit)**: FCM / remote push channel design (see
  `docs/mobile-ux-assessment.md` P0), mcrelay, daemon protocol redesign,
  iOS, second provider / Antigravity, replacing `flutter_markdown_plus`
  unless Phase B profiling still pins it.
- **Extends**: [0005 Flutter client plan](docs/0005-flutter-android-client-assessment-and-plan.md),
  [chat-performance.md](docs/chat-performance.md),
  [mobile-ux-assessment.md](docs/mobile-ux-assessment.md)
- **Companions**: Host history ring (`internal/session/manager.go`:
  `historyBufferCap=500`, page defaults 200/max 500, ~512 KiB soft response cap)

---

## 1. Executive summary

The session chat already implements **industry-correct scroll architecture** for
streaming agent UIs:

| Pillar | Current implementation | Grade |
|--------|------------------------|-------|
| Sticky live end | `ListView.builder(reverse: true)` | Strong |
| Follow vs read | Near-bottom threshold; jump only on **new row** | Strong |
| Rebuild isolation | Riverpod `.select` shell vs `_TranscriptPane` | Strong |
| Event batching | 32 ms coalesce for chunk/thought/tool_update | Strong |
| Markdown | Cache + size-tier throttle + stream marker close | Good |
| List reuse | `ValueKey(seq)` + O(1) `findChildIndexCallback` map | Strong |
| Raster | Per-row `RepaintBoundary`; scroll-paused shimmer | Strong |
| Caps | Client 800 items; host ring 500 events | Adequate / mismatched |

Residual value is **not** “add a ListView.” It is:

1. **Allocation discipline** on the streaming reducer path (list + string copy).
2. **Bounded work** for markdown re-parse and cold history hydrate.
3. **Byte-aware limits** so one tool dump cannot dominate RAM/layout.
4. **Chat polish** agent users notice (unread FAB, code chrome, clamp huge blocks).
5. **Maintainability** (`chat_screen.dart` ~2.5k lines monolith).

**No P0 security/auth findings** in this pass. Highest residual product risk
outside pure chat FPS remains **async notifications** (separate plan).

**Baseline verification (plan day):**

```bash
cd apps/mobile && flutter test test/chat_render_test.dart \
  test/transcript_reducer_test.dart test/transcript_rows_test.dart \
  test/streaming_markdown_test.dart test/history_replay_test.dart
```

(Run after any implementation tranche; add new unit tests listed per phase.)

---

## 2. Do not redo (already shipped)

Treat as done unless regression tests fail. Do not rewrite these for their own sake.

| Area | Behavior | Where |
|------|----------|--------|
| Reverse transcript | Newest at offset 0; open lands at live end | `_TranscriptPane`, `initState` |
| Follow discipline | Append jump only if `_userNearBottom`; no per-chunk extent chase | `ref.listen` + `_scrollToEnd` |
| Keyboard policy | Unfocus on send/queue; `onDrag` dismiss; FAB unfocus | composer + list |
| Shell/pane split | Shell watches status/plan/commands/pending/hasItems; pane watches items | `ChatScreen` / `_TranscriptPane` |
| Batching | `kTranscriptBatchWindow = 32ms`; discrete events flush immediately | `transcripts_notifier.dart` |
| Row fold memo | Skip full `buildTranscriptRows` when only last non-tool text grew | `_memoTranscriptRows` |
| Key index | Map rebuilt when row list identity changes | `_TranscriptPaneState._keyIndex` |
| Stream MD buffer | Close unclosed `**` / `` ` `` / fences (show content, not hide) | `streaming_markdown.dart` |
| MD throttle tiers | 120 / 200 / 320 ms at 4k / 16k chars | `_AssistantMarkdown` |
| Tool grouping | Finished consecutive same-class tools → `GroupRow` | `transcript_rows.dart` |
| Running tools | Static icon (no per-row spinner) | `_ChatBubble` tool branch |
| Scroll activity | Pause shimmer/pulse while dragging/flinging | `scroll_activity.dart` |
| History races | Live wins; seq-based `resyncHistory` | `TranscriptsNotifier` |
| Cancel recovery | Local announce + 5s status resync | `_cancelTurn` |
| Queue | Mid-turn queue chips; flush post-frame when idle | `_queuedPrompts` |
| Edit & resend | Long-press user → composer prefill | `_userMessageActions` |
| Idle send guard | First message sendable when idle (tested) | `chat_render_test.dart` |
| Empty-state starfield | Off non-empty path | `ChatScreen` body |

**Doc debt:** `docs/chat-performance.md` still mentions 16 ms batch and
`cacheExtent: 400`; code is **32 ms** and **`scrollCacheExtent` 900 px**. Fix in
Phase A (no behavior change).

---

## 3. Findings inventory

### 3.1 Severity model

| P | Meaning |
|---|--------|
| **P0** | Data loss, stuck composer, scroll yank that breaks “read while streaming,” crash/OOM under normal agent sessions |
| **P1** | Sustained jank under heavy stream or cold open; unbounded memory from a single item; clear UX defect on the live path |
| **P2** | Hardening, caps alignment, rebuild hygiene, maintainability |
| **P3** | Polish, a11y, optional durability, deferred product |

### 3.2 Table

| ID | P | Lens | Finding | Where |
|----|---|------|---------|--------|
| **C1** | P1 | GC / FPS | Every assistant/thought chunk does `List.from(items)` + string `+` on the open bubble | `transcript_reducer._appendAssistant` / `_appendThought` |
| **C2** | P1 | FPS | Full markdown re-parse of entire growing reply each throttle tick (O(message length)) | `_AssistantMarkdown._render` |
| **C3** | P1 | FPS / open | History replay/resync applies all events synchronously on UI isolate | `replayHistory`, `resyncHistory` |
| **C4** | P1 | Memory | Cap is **item count** (800), not **bytes**; one huge tool/assistant blob can dominate RAM and expanded layout | `kMaxTranscriptItems`, tool upsert |
| **C5** | P1 | UX / layout | Expanded tool/thought detail is unbounded `SelectableText` — multi-MB dumps spike layout | `_CompactStatusTile` children |
| **C6** | P2 | Correctness | Client item cap 800 vs host event ring 500 + default history page (client sends no `limit`) | reducer cap vs `manager.go` / `sessionHistory` |
| **C7** | P2 | Rebuild | Crossing near-bottom threshold `setState`s entire `ChatScreen` for FAB only | `_onScroll` |
| **C8** | P2 | Maintainability | `chat_screen.dart` ~2460 lines mixes list, MD, plan, permissions, composer, voice | `features/chat/` |
| **C9** | P2 | Docs | `chat-performance.md` out of date vs code | docs |
| **C10** | P3 | UX | Jump FAB has no unread / “N new” badge while scrolled up | FAB in `ChatScreen` |
| **C11** | P3 | UX | No streaming caret / edge pulse on live assistant bubble | `_AssistantMarkdown` / bubble |
| **C12** | P3 | UX | Code fences: no language label, copy button, or highlight | markdown style sheet |
| **C13** | P3 | UX | No “Show more” clamp on finalized huge assistant bubbles | `_ChatBubble` assistant |
| **C14** | P3 | UX | No turn separators / timestamps | transcript list |
| **C15** | P3 | UX | No haptics on send / queue / permission decide | composer / permission sheet |
| **C16** | P3 | Robustness | No phone-side transcript durability (process death → host history only) | `TranscriptsNotifier` |
| **C17** | P3 | A11y | Semantics for stop/queue/send morph and reverse list not audited | composer |

**Not findings (explicit non-issues this pass):**

- Need to abandon `reverse: true` (correct for this product).
- Need to re-introduce per-chunk scroll-to-maxExtent (regresses jitter).
- Need FCM for chat FPS (product gap, separate plan).
- Need to replace markdown engine **before** profiling C1–C3 (premature).
- Host million-message archives (non-goal; caps stay).

---

## 4. Decisions (proposed defaults — lock before implement)

Owner should confirm or override. Recommendations are marked **(rec)**.

| # | Topic | Options | Recommendation |
|---|--------|---------|----------------|
| **D1** | Streaming text storage | (a) Keep `String +` on `ChatItem` (b) **Open-turn `StringBuffer` / chunk rope, materialize on throttle + finalize** (c) Full immutable rope always | **(b) (rec)** — lowest churn for reducer + MD boundary |
| **D2** | Streaming list mutation | (a) Keep `List.from` every chunk (b) **Notifier holds growable list; copy only on non-last / structural change** (c) Freezed/immutable only | **(b) (rec)** — preserve `identical` prefix for row memo |
| **D3** | Heavy-stream markdown | (a) Keep full re-parse throttled (b) **Plain/mono stream above size T; full MD on finalize** (c) Swap engine now | **(b) (rec)** for replies &gt; ~4k or open fence; keep (a) for short replies |
| **D4** | History hydrate | (a) Sync apply (status quo) (b) **Chunk apply across frames (25–50 events)** (c) `compute()` isolate | **(b) (rec)** first; (c) only if still &gt;100 ms after (b) |
| **D5** | Byte budgets | (a) Count-only cap (b) **Soft per-item text cap + transcript soft byte budget with display truncation** (c) Hard drop events | **(b) (rec)** — retain seq/tool identity; truncate detail with expand/copy path |
| **D6** | Cap alignment | (a) Leave 800 / 500 (b) **Document semantics + client history `limit: 500`** (c) Raise host ring to 800 | **(b) (rec)**; host raise deferred unless product needs longer phone history |
| **D7** | FAB / near-bottom | (a) Keep shell setState (b) **`ValueNotifier` + tiny consumer for FAB + unread** | **(b) (rec)** |
| **D8** | File split | (a) Keep monolith (b) **Extract transcript pane, markdown, composer, plan, permissions under `features/chat/`** | **(b) (rec)** in Phase C after A/B land (avoid mega-diff during perf work) |
| **D9** | Local transcript cache | (a) Defer (b) SQLite/Hive last N items | **(a) (rec)** this tranche; track as C16 follow-on |
| **D10** | Polish tranche | (a) Perf only (b) **Perf + unread FAB + tool clamp + code copy** (c) Full polish C10–C15 | **(b) (rec)** — visible UX with perf; rest P3 backlog |
| **D11** | Markdown engine swap | (a) **Defer until profile** (b) Investigate now | **(a) (rec)** |

### Alternatives considered (not chosen by default)

| Rejected | Why |
|----------|-----|
| `ScrollablePositionedList` / third-party chat list | Current reverse `ListView` is correct; migration cost high for little gain |
| `itemExtent` / fixed heights | Incompatible with markdown/variable bubbles |
| Truncate-at-marker streaming (old approach) | Causes “frozen then jump” on code fences; current close-markers is better |
| Phone-side full archive | Host is source of truth; durability is optional cache only |
| Per-frame markdown | Guaranteed jank on long replies |

---

## 5. Implementation plan (phased)

Ship as sequential commits (or stacked PRs). Each phase must leave tests green.

### Phase A — Foundation (docs + measurement + low risk)

**Goals:** Accurate docs; hooks for verification; no risky behavior change.

| Task | Detail |
|------|--------|
| A1 | Rewrite `docs/chat-performance.md`: reverse list, 32 ms batch, 900 px cache, closer-style stream buffer, scroll activity, profiling checklist |
| A2 | Add `docs/0018-mobile-chat-performance-action-plan.md` (this plan, status → Accepted once locked) |
| A3 | Optional debug overlay (kDebugMode only) or comments documenting: batch window, last MD parse ms — only if useful; otherwise DevTools checklist in doc is enough |
| A4 | Confirm `session.history` client: pass `limit: historyMaxPage` (500) if protocol supports it; document byte soft-cap behavior |

**Tests:** Existing suite must pass unchanged.  
**Exit:** Docs match code; history fetch uses explicit limit if available.

---

### Phase B — Performance core (C1–C5, C3)

**Goals:** Lower GC and frame cost on the live stream and cold open.

#### B1 — Streaming allocation (C1, D1, D2)

- Introduce an **open-turn buffer** in the reducer or notifier for the active
  assistant (and thought) text:
  - On first non-empty chunk opening a bubble: create item + buffer.
  - On subsequent chunks: append to buffer; update `ChatItem.text` only when
    notifying UI (already batched at 32 ms) **or** keep text updated but avoid
    full `List.from` by replacing last index in a growable list retained across
    updates.
- Preserve semantics:
  - Whitespace-only must not open a bubble.
  - Mid-message `\n\n` preserved.
  - Tool append after assistant: buffer closes; new tool card; later assistant
    opens new bubble (existing `_appendAssistant` last-kind check).
- Keep `identical(items[i], prev[i])` for all but last where possible so
  `_memoTranscriptRows` still short-circuits.

**Tests:** extend `transcript_reducer_test.dart` for long multi-chunk append;
  identity/prefix stability if exposed; no behavior change for tool coalesce.

#### B2 — Byte / display budgets (C4, C5, D5)

Constants (tunable, documented in chat-performance.md):

| Constant | Suggested default | Applies to |
|----------|-------------------|------------|
| `kMaxItemTextChars` | 100_000 | Stored text per assistant/thought/tool detail (hard store clip with marker) |
| `kMaxExpandedDetailChars` | 8_000 | Expanded tool/thought UI before “Show full / Copy” |
| `kMaxStreamingMarkdownChars` | 4_000 | Below → full MD; above → stream strategy (B3) |

- Tool upsert: clip `detail` on store with a clear suffix (`… [truncated]`).
- Expanded tile: `ConstrainedBox(maxHeight: …)` + scroll + **Copy** action.
- Prefer not to break `seq` / tool index on clip.

**Tests:** reducer clips; widget test expanded detail does not throw with huge string.

#### B3 — Streaming markdown mode (C2, D3)

- While `streaming && data.length > kMaxStreamingMarkdownChars` **or** open fence
  longer than threshold:
  - Option **preferred:** keep `bufferStreamingMarkdown` + throttle, but render
    **MarkdownBody only for finalized prefix** is complex — simpler approach:
    - **Short path:** existing MD throttle for `length ≤ 4k`.
    - **Long path:** show `SelectableText`/`Text` with monospace for the growing
      tail **or** full plain text with buffer closers applied as plain; on
      `streaming → false`, one full `MarkdownBody` parse.
- Keep short replies fully live-markdown (current quality).
- Selectable remains off while streaming.

**Tests:** streaming_markdown still unit-tested; widget test that long streaming
reply does not rebuild markdown every 32 ms (timer/throttle spy or parse counter
test hook `@visibleForTesting`).

#### B4 — Chunked history apply (C3, D4)

- `replayHistory` / `resyncHistory`: apply in batches of **N=40** events,
  yielding with `await Future<void>.delayed(Duration.zero)` or
  `SchedulerBinding.instance.scheduleTask` so first frames can paint.
- UI: optional subtle “Loading history…” only if first batch empty and more
  pending (avoid banner flash for small histories).
- Preserve race rules: if live populates mid-hydrate, abort remaining batches
  and/or switch to resync rules already defined.

**Tests:** `history_replay_test.dart` — large synthetic list still ends correct;
  live race still drops/resyncs correctly.

#### B5 — Near-bottom / FAB notifier (C7, D7)

- Replace `_userNearBottom` shell `setState` with `ValueNotifier<bool>` (and
  optionally `ValueNotifier<int>` unread count scaffolding for Phase D).
- FAB as `ValueListenableBuilder` so stream chunks never rebuild shell via scroll.

**Tests:** existing scroll tests if any; add pump that toggles scroll position
without requiring full shell rebuild assertions (optional).

**Phase B exit criteria:**

- Multi-KB stream: fewer full-list allocations (profiler / observatory or unit
  identity checks).
- Cold open of max history: no multi-second UI freeze (manual + optional timing).
- All chat-related flutter tests green.

---

### Phase C — Structure (C8, D8)

**Goals:** Safer future changes; same UX.

Split under `apps/mobile/lib/features/chat/`:

| File | Contents |
|------|----------|
| `chat_screen.dart` | Shell: app bar, banners, listen/resync, composition of children |
| `transcript_pane.dart` | `_TranscriptPane`, row memo, list |
| `chat_bubble.dart` | `_ChatBubble`, `_ToolGroupTile`, `_LimitNotice`, compact tiles |
| `assistant_markdown.dart` | `_AssistantMarkdown`, stream mode |
| `composer_bar.dart` | Text field, send/queue/stop, voice, queue chips, slash list |
| `plan_panel.dart` | `_PlanPanel` |
| `permission_sheet.dart` | Permission presentation helpers + sheet UI |
| `chat_helpers.dart` | `isAllowOption`, `prunePresentedPermissionIds` (already tested) |

Rules:

- No behavior change in C (pure move + exports).
- Keep `@visibleForTesting` helpers exported for existing tests.
- Update imports in tests once.

**Exit:** `flutter test` green; file sizes reviewable (&lt;~600 lines preferred).

---

### Phase D — Polish in-scope (D10: C10–C12, C5 UI)

**Goals:** Noticeable agent-chat UX without new backends.

| Task | Detail |
|------|--------|
| D1-unread | While `!nearBottom`, count items with `seq > seqAtLeaveBottom` (or last visible seq); badge on jump FAB (`N` / `9+`) |
| D2-clamp-ui | Expanded tool/thought: max height ~240, internal scroll, **Copy** icon |
| D3-code-copy | Custom markdown `code`/`codeblock` builders: monospaced block + trailing **Copy** IconButton; language from fence if present |
| D4-haptics | Light impact on send/queue; selection click on permission allow (respect reduce-motion / platform) |

**Defer from this tranche (backlog):** C11 streaming caret, C13 show-more on
assistant, C14 timestamps, C16 local cache, C17 full a11y audit, markdown engine
swap, FCM.

**Tests:** badge appears after append while scrolled up; copy actions smoke;
  code block builder renders.

---

### Phase E — Optional follow-ons

| Task | Status |
|------|--------|
| E1 Local transcript cache (C16) | **Done** — last 150 items / 12 sessions via SharedPreferences; host history still wins |
| E2 Streaming caret (C11) | **Done** — blinking edge pulse on live assistant bubble |
| E3 Show more on assistant (C13) | **Done** — clamp at 6k chars with Show more / Show less |
| E4 Host ring raise / pagination UX | **Done** — ring + max page 800; client auto-pages `truncated` history |
| E5 Markdown engine evaluation | **Deferred** — stay on `flutter_markdown_plus` until profile pins parse cost |
| E6 Notifications / background | **Out of scope here** — see `mobile-ux-assessment.md` |

---

## 6. Testing strategy

### Automated (CI / local)

```bash
cd apps/mobile
flutter test test/chat_render_test.dart \
  test/transcript_reducer_test.dart \
  test/transcript_rows_test.dart \
  test/streaming_markdown_test.dart \
  test/history_replay_test.dart
# After Phase C, full suite:
flutter test
```

| Area | New / extended tests |
|------|----------------------|
| Reducer | Long chunk streams; clip budgets; tool detail clip |
| History | Large N batch apply completeness; race mid-hydrate |
| Markdown | Throttle / long-stream mode test hook |
| UI | FAB unread; expand clamp; idle send still green |

### Manual profiling checklist (physical device, profile mode)

1. Cold open session with near-full host history — land at live end; no multi-second freeze.
2. Stream multi-KB reply with nested code fences while following bottom.
3. Scroll up mid-stream — list must not yank; FAB shows unread as rows append.
4. Expand tool with large detail — scroll inside tile; copy works; list stays usable.
5. Submit / queue — keyboard policy unchanged; haptics if Phase D.
6. Kill app mid-session, reopen — history still via host (no regression).
7. Reconnect during stream — resync + composer not stuck running.

Use Flutter DevTools Performance: frame build times, rebuild counts on
`_TranscriptPane` vs shell, GC during stream.

---

## 7. Risk register

| Risk | Mitigation |
|------|------------|
| Growable list breaks Riverpod equality / listeners | Always assign new `SessionTranscript` / new `items` reference on notify; only skip **internal** full copies between batch applies if careful — prefer: one new list per batch flush, mutate last element only within flush |
| Stream plain-text mode feels “worse” | Only engage above threshold; finalize always full MD |
| Chunked history flashes empty | Keep prior items during resync rebuild; for empty open, short loading state only if &gt;1 batch |
| File split breaks test imports | Single commit; run full `flutter test` |
| Byte clip loses copy fidelity | Copy uses stored (clipped) text; optional later: re-fetch not in scope |
| Unread count wrong with reverse list | Define clearly: seqs appended after user left bottom; reset on jumpTo(0) |

---

## 8. Success criteria

| Metric | Target |
|--------|--------|
| Multi-KB follow-stream jank | Subjective smooth; profile build p95 closer to &lt;16 ms on mid-tier after B |
| Cold history open | First interactive frame without multi-second stall for max ring |
| Scroll-up mid-stream | Zero forced jumps (regression guard) |
| Memory | Single item cannot retain &gt; `kMaxItemTextChars`; expand UI bounded |
| Tests | All chat-related + full suite green |
| Docs | `chat-performance.md` + MADR 0018 match code |
| Polish | Unread FAB + tool clamp + code copy shipped if D10=(b) |

---

## 9. Suggested commit sequence

1. `docs(mobile): MADR 0018 chat performance plan + sync chat-performance.md`
2. `perf(mobile): streaming transcript append without full-list thrash`
3. `perf(mobile): byte budgets and expanded tool detail clamp`
4. `perf(mobile): long-stream markdown mode + history chunk apply`
5. `refactor(mobile): split chat_screen into feature modules`
6. `feat(mobile): jump-to-latest unread badge, code copy, haptics`

(Commits 2–4 are Phase B; can squash locally if preferred, but keep tests green per commit.)

---

## 10. Work estimate (rough)

| Phase | Effort | Depends |
|-------|--------|---------|
| A Docs + history limit | 0.5 d | — |
| B1–B2 Alloc + budgets | 1–1.5 d | A |
| B3 Stream MD mode | 0.5–1 d | B1 |
| B4 History chunking | 0.5 d | — |
| B5 FAB notifier | 0.25 d | — |
| C Split files | 0.5–1 d | B preferred |
| D Polish | 1 d | B5 + C optional |
| **Default scope A+B+D (C optional parallel)** | **~4–5 d** | |

---

## 11. Open questions for owner (lock these)

1. **D3 long-stream markdown:** plain/mono while long-streaming, or always
   throttled full MD (status quo + B1 only)?
2. **D5 clip thresholds:** accept 100k / 8k defaults or tighter for low-end phones?
3. **D8 split:** do structure refactor in this tranche or only after perf lands?
4. **D10 polish set:** unread+clamp+code copy+haptics, or perf-only?
5. **D9 local cache:** confirm deferred.
6. **Host history:** any desire to raise ring above 500 in the same tranche? (default no)

**Recommended lock package:** D1b, D2b, D3b, D4b, D5b, D6b, D7b, D8b (after B),
D9a, D10b, D11a.

---

## 12. Implementation readiness

Once decisions are locked:

1. Write `docs/0018-mobile-chat-performance-action-plan.md` into the repo (copy of
   this plan with **Status: Accepted** + locked table filled).
2. Execute Phase A → B → (C) → D in order.
3. Do **not** push unless asked (match mcrelay commit policy).

This plan is ready for decision lock and implementation.

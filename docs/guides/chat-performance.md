# Chat session performance & keyboard UX

Notes for the Android Flutter chat screen. Keep this in sync with
[MADR 0018](../../apps/mobile/docs/decisions/0018-MADR-mobile-chat-performance-action-plan.md) and the code under
`apps/mobile/lib/{features/chat,data/chat,state}`.

**How to measure:** run the app in Flutter **profile** mode and use DevTools —
see [mobile-profiling.md](../../apps/mobile/docs/guides/mobile-profiling.md) (`make profile`, `make profile-apk`).

## Keyboard policy

- Soft keyboard shows when the composer is focused.
- **Submit (send or queue) always unfocuses** the composer so the transcript can use full height.
- Tap outside the field (`TextField.onTapOutside`) and drag-scroll on the transcript (`keyboardDismissBehavior: onDrag`) also dismiss the keyboard.
- Jump-to-latest FAB dismisses the keyboard as well.

## Scroll architecture

- Transcript uses a **`reverse: true` `ListView.builder`**: newest content is at offset `0` (visual bottom).
- Growing the live assistant bubble does **not** chase `maxScrollExtent` every chunk (that was the prior jitter source). Append jumps only when the user is near the live end.
- Near-bottom detection: `pixels < 120`. Jump-to-latest: `jumpTo(0)`.
- Auto-follow **never runs while the user is scrolling**, and is skipped when already pinned at `0`. `jumpTo` begins with `goIdle()`, which cancels the current `ScrollActivity` — so an unguarded jump
  yanked the list out from under a drag or fling. Inside the 120 px band during a tool burst that fired several times a second and made the transcript unscrollable ([MADR
  0042](../../apps/mobile/docs/decisions/0042-MADR-android-app-remediation.md) D5). The gate is `_listScrolling`, the same notifier the shimmer/pulse animations use.
- `scrollCacheExtent: ScrollCacheExtent.pixels(900)` for offscreen row pre-render.
- Near-bottom / FAB visibility uses a `ValueNotifier` so scroll threshold crossings do not rebuild the whole chat shell.

## Caching & buffers

| Layer | Behavior |
|---|---|
| Rebuild isolation | Chat shell uses Riverpod `.select` on status / plan / commands / pending / hasItems; only the transcript pane watches `items` during stream chunks |
| Row fold memo | Skip the full `buildTranscriptRows` when the change is resolvable from the tail alone: the last item's text grew, the newest tool's status/output changed, or one item was appended (a tool extends the trailing group, anything else becomes a new row). A 6-tool OpenCode burst folds **0** times, was 12 ([MADR 0042](../../apps/mobile/docs/decisions/0042-MADR-android-app-remediation.md) D3) |
| Markdown widget cache | Assistant markdown keeps the parsed subtree; re-parses only when shown text changes |
| Markdown throttle | At most one re-parse per frame while streaming; above `kStreamMdTier1Chars` (4k) at least 120 ms apart, above `kStreamMdTier2Chars` (16k) at least 200 ms |
| One markdown engine | Stream and finalize both render `MarkdownBody`; there is no plain-text path for long replies ([mobile MADR 0056](../../apps/mobile/docs/decisions/0056-MADR-mcremote-android-protocol-stack-audit.md), M-9…M-12, Phase 7) |
| Style sheet cache | Per-brightness `MarkdownStyleSheet` reused across re-renders |
| Streaming marker buffer | `bufferStreamingMarkdown` **closes** unclosed `**` / `` ` `` / fences (show content, not hide) while streaming |
| Host stream coalescing | Daemon holds assistant/thought text ~**80 ms** (`stream_coalesce_ms` on grok, codex and kilo; fixed for OpenCode) so the phone receives ~12 updates/s instead of one frame per token; first chunk and end-of-turn tail are never delayed ([MADR 0024](../decisions/0024-MADR-stream-coalescing.md)) |
| Host dedup | `usage_update` only on change; `session_status: running` only on transition — both bypass the 32 ms window client-side, so repeats used to force a commit each |
| Notifier batching | `assistant_message_chunk`, `thought_chunk`, `tool_call`, `tool_call_update`, `usage_update`, `plan`, `available_commands`, `remote_commands` coalesce to **32 ms** windows; discrete events flush immediately. `tool_call` joined the set in MADR 0042 D2 — it gates no affordance, and excluding it cost one synchronous commit per tool on OpenCode's parallel fan-out (5 tools = 6 commits, now 1) |
| Chunk fold | `_foldChunks` merges adjacent same-type text in a window into one apply, so `_appendChunk`'s whole-reply copy runs once per run, not once per chunk (`debugAppendChunkCount` bounds it). It also collapses consecutive `tool_call_update`s for one tool id to the latest — replace-semantics, so only the last state is observable — never across a `tool_call`, never for an id-less update, and always keeping the last so a terminal status cannot be folded away |
| Streaming list | Growable list ownership within a batch: last-index replace without full `List.from` per chunk |
| Selectable text | Off while streaming; on when the turn finalizes (long-press copy always available) |
| List element reuse | `findChildIndexCallback` maps `ValueKey(seq)` / group keys under `reverse: true`. The index is maintained **incrementally**: an append adds its own key rather than rebuilding the map, and the memo reports which happened. Reporting "keys unchanged" while adding a key left the index stale and remounted every row appended since the last full rebuild — re-parse cost grew 1, 2, 3, … per append (audit 0041 H1) |
| History hydrate | `session.history` with `limit: 200` (`kHistoryFetchLimit`); **auto-page** while `truncated`, at most `kHistoryMaxPages` (32) pages per walk; apply in batches of 40 events (`kHistoryApplyBatchSize`) with frame yields |
| Local cache | Last 150 items / 12 sessions in SharedPreferences (process-death polish; host still wins) |
| Streaming caret | Blinking edge pulse on live assistant bubble |
| Show more | Finalized assistant replies &gt; 6k chars clamp with expand control |

## Caps & byte budgets

| Constant | Default | Role |
|---|---|---|
| `kMaxTranscriptItems` | 4_000 | Client FIFO item count cap |
| Host `historyBudgetBytes` | 32 MiB | Daemon retention per session, by bytes (advertised as `caps.history_budget_bytes`) |
| Host `historyMaxPage` | 800 | The largest `session.history` page a client may ask for |
| `kHistoryFetchLimit` | 200 | The page the client asks for (it pages until complete) |
| `kHistoryMaxPages` | 32 | Pages per backward walk, so a daemon that never advances cannot spin the client |
| `kMaxItemTextChars` | 100_000 | Hard store clip per assistant/thought/tool detail (`… [truncated]`) |
| `kMaxExpandedDetailChars` | 8_000 | Expanded tool/thought UI before internal scroll + copy |
| `kStreamMdTier1Chars` / `kStreamMdTier2Chars` | 4_000 / 16_000 | Streaming re-parse throttle tiers (120 ms / 200 ms) |
| `kAssistantShowMoreChars` | 6_000 | Finalized assistant "Show more" clamp |

The two budgets are independent ([MADR 0138](../decisions/0138-MADR-overhaul-provider-surfaces-and-turn-path.md)).
The host keeps up to 32 MiB per session, roughly 21,000 events, and the client keeps 4,000 items, so the phone is
the tighter cap. The client's page of 200 is deliberately far below the host's 800: a page is a screen's worth plus
lookahead, and asking for 800 made the host re-encode a shrinking slice many times over (MADR 0138 F14).

## Graphics

- Per-row `RepaintBoundary` isolates raster of growing bubbles.
- Bubble max widths come from a list-level `LayoutBuilder` (not per-bubble `MediaQuery`) so keyboard insets do not thrash every row.
- Empty-state starfield is off the non-empty chat path.
- **`ChatScrollActivity` / `ChatScrollActivitySensor`**: shimmer + status pulse pause while the transcript is dragging/flinging.
- Running tools use a **static** `Icons.autorenew` (no per-row `CircularProgressIndicator`); the app-bar chip remains the live “working” cue.
- Expanded tool/thought detail: max height 200, internal scroll, **Copy** action.

## Non-goals

- Antigravity / second-provider chat work.
- Replacing `flutter_markdown_plus` unless profiling after Phase B still pins it (MADR 0018 D11).
- Host-side million-message archives.
- FCM / background push (see `0178-REPORT-mobile-ux-assessment.md`).
- Full phone-side archive (only last-N cache; host remains source of truth).
- Markdown engine swap (E5 — re-evaluate only if profiles pin parse cost).
- FCM / remote push (E6 — see `0178-REPORT-mobile-ux-assessment.md`).

## Profiling checklist (physical device, profile mode)

1. Cold open a session with near-full host history — land at live end; no multi-second freeze.
2. Stream a multi-KB reply with nested code fences while following the live end.
3. Scroll up mid-stream — list must not yank; FAB shows unread as rows append.
4. Expand tool with large detail — scroll inside tile; copy works; list stays usable.
5. Submit / queue — keyboard drops; light haptics when enabled.
6. Kill app mid-session, reopen — history still via host (no regression).
7. Reconnect during stream — resync + composer not stuck running.

Use Flutter DevTools Performance: frame build times, rebuild counts on the transcript pane vs shell, GC during stream.
Note batch window (**32 ms**) and the markdown throttle tier (below 4k, above 4k, above 16k chars) when interpreting
parse cost.

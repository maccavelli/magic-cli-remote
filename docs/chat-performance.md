# Chat session performance & keyboard UX

Notes for the Android Flutter chat screen. Keep this in sync with
[MADR 0018](0018-mobile-chat-performance-action-plan.md) and the code under
`apps/mobile/lib/{features/chat,data/chat,state}`.

**How to measure:** run the app in Flutter **profile** mode and use DevTools —
see [mobile-profiling.md](mobile-profiling.md) (`make profile`, `make profile-apk`).

## Keyboard policy

- Soft keyboard shows when the composer is focused.
- **Submit (send or queue) always unfocuses** the composer so the transcript can use full height.
- Tap outside the field (`TextField.onTapOutside`) and drag-scroll on the transcript (`keyboardDismissBehavior: onDrag`) also dismiss the keyboard.
- Jump-to-latest FAB dismisses the keyboard as well.

## Scroll architecture

- Transcript uses a **`reverse: true` `ListView.builder`**: newest content is at offset `0` (visual bottom).
- Growing the live assistant bubble does **not** chase `maxScrollExtent` every chunk (that was the prior jitter source). Append jumps only when the user is near the live end.
- Near-bottom detection: `pixels < 120`. Jump-to-latest: `jumpTo(0)`.
- `scrollCacheExtent: ScrollCacheExtent.pixels(900)` for offscreen row pre-render.
- Near-bottom / FAB visibility uses a `ValueNotifier` so scroll threshold crossings do not rebuild the whole chat shell.

## Caching & buffers

| Layer | Behavior |
|---|---|
| Rebuild isolation | Chat shell uses Riverpod `.select` on status / plan / commands / pending / hasItems; only the transcript pane watches `items` during stream chunks |
| Row fold memo | Skip full `buildTranscriptRows` when only the last non-tool item’s text grew |
| Markdown widget cache | Assistant markdown keeps the parsed subtree; re-parses only when shown text changes |
| Markdown throttle | 120 / 200 / 320 ms at 4k / 16k chars while short-stream path is active |
| Long-stream MD | Above `kMaxStreamingMarkdownChars` (4k) while streaming: plain/mono text (buffer closers applied as plain); full `MarkdownBody` once finalized |
| Style sheet cache | Per-brightness `MarkdownStyleSheet` reused across re-renders |
| Streaming marker buffer | `bufferStreamingMarkdown` **closes** unclosed `**` / `` ` `` / fences (show content, not hide) while streaming |
| Notifier batching | `assistant_message_chunk`, `thought_chunk`, `tool_call_update` coalesce to **32 ms** windows; discrete events flush immediately |
| Streaming list | Growable list ownership within a batch: last-index replace without full `List.from` per chunk |
| Selectable text | Off while streaming; on when the turn finalizes (long-press copy always available) |
| List element reuse | `findChildIndexCallback` maps `ValueKey(seq)` / group keys under `reverse: true` |
| History hydrate | `session.history` with `limit: 800` (`historyMaxPage`); **auto-page** while `truncated`; apply in batches of 40 events with frame yields |
| Local cache | Last 150 items / 12 sessions in SharedPreferences (process-death polish; host still wins) |
| Streaming caret | Blinking edge pulse on live assistant bubble |
| Show more | Finalized assistant replies &gt; 6k chars clamp with expand control |

## Caps & byte budgets

| Constant | Default | Role |
|---|---|---|
| `kMaxTranscriptItems` | 800 | Client FIFO item count cap |
| Host `historyBufferCap` | 800 | Daemon event ring |
| Host `historyMaxPage` / client history `limit` | 800 | One history fetch page (client pages until complete) |
| `kMaxItemTextChars` | 100_000 | Hard store clip per assistant/thought/tool detail (`… [truncated]`) |
| `kMaxExpandedDetailChars` | 8_000 | Expanded tool/thought UI before internal scroll + copy |
| `kMaxStreamingMarkdownChars` | 4_000 | Full MD while streaming below; plain path above |
| `kAssistantShowMoreChars` | 6_000 | Finalized assistant "Show more" clamp |

Client item cap (800) can exceed the host ring (500): phone may retain more from a live session than cold history can fully rebuild. Prefer explicit `limit: 500` on history so wire pages match the ring.

## Graphics

- Per-row `RepaintBoundary` isolates raster of growing bubbles.
- Bubble max widths come from a list-level `LayoutBuilder` (not per-bubble `MediaQuery`) so keyboard insets do not thrash every row.
- Empty-state starfield is off the non-empty chat path.
- **`ChatScrollActivity` / `ChatScrollActivitySensor`**: shimmer + status pulse pause while the transcript is dragging/flinging.
- Running tools use a **static** `Icons.autorenew` (no per-row `CircularProgressIndicator`); the app-bar chip remains the live “working” cue.
- Expanded tool/thought detail: max height ~240, internal scroll, **Copy** action.

## Non-goals

- Antigravity / second-provider chat work.
- Replacing `flutter_markdown_plus` unless profiling after Phase B still pins it (MADR 0018 D11).
- Host-side million-message archives.
- FCM / background push (see `mobile-ux-assessment.md`).
- Full phone-side archive (only last-N cache; host remains source of truth).
- Markdown engine swap (E5 — re-evaluate only if profiles pin parse cost).
- FCM / remote push (E6 — see `mobile-ux-assessment.md`).

## Profiling checklist (physical device, profile mode)

1. Cold open a session with near-full host history — land at live end; no multi-second freeze.
2. Stream a multi-KB reply with nested code fences while following the live end.
3. Scroll up mid-stream — list must not yank; FAB shows unread as rows append.
4. Expand tool with large detail — scroll inside tile; copy works; list stays usable.
5. Submit / queue — keyboard drops; light haptics when enabled.
6. Kill app mid-session, reopen — history still via host (no regression).
7. Reconnect during stream — resync + composer not stuck running.

Use Flutter DevTools Performance: frame build times, rebuild counts on the transcript pane vs shell, GC during stream. Note batch window (**32 ms**) and markdown path (short vs long-stream) when interpreting parse cost.

# Chat session performance & keyboard UX

Notes for the Android Flutter chat screen after the 2026-07 stability pass.

## Keyboard policy

- Soft keyboard shows when the composer is focused.
- **Submit (send or queue) always unfocuses** the composer so the transcript can use full height.
- Tap outside the field (`TextField.onTapOutside`) and drag-scroll on the transcript (`keyboardDismissBehavior: onDrag`) also dismiss the keyboard.
- Jump-to-latest FAB dismisses the keyboard as well.

## Scroll architecture

- Transcript uses a **`reverse: true` `ListView.builder`**: newest content is at offset `0` (visual bottom).
- Growing the live assistant bubble no longer requires chasing `maxScrollExtent` every frame (the previous jitter source).
- Near-bottom detection: `pixels < 120`. Jump-to-latest: `jumpTo(0)`.
- `cacheExtent: 400` for modest pre-render of offscreen rows.

## Caching & buffers

| Layer | Behavior |
|---|---|
| Row fold memo | Skip full `buildTranscriptRows` when only the last non-tool item’s text grew |
| Markdown widget cache | `_AssistantMarkdown` keeps the parsed subtree; re-parses only when shown text changes |
| Markdown throttle | 120 ms default; 200 ms when text &gt; 4k chars |
| Style sheet cache | Per-brightness `MarkdownStyleSheet` reused across re-renders |
| Streaming marker buffer | `bufferStreamingMarkdown` hides unclosed `**` / `` ` `` / fences while streaming |
| Notifier batching | `assistant_message_chunk`, `thought_chunk`, `tool_call_update` coalesce to 16 ms windows; discrete events flush immediately |
| Selectable text | Off while streaming; on when the turn finalizes (long-press copy always available) |

## Graphics

- Per-row `RepaintBoundary` isolates raster of growing bubbles.
- Bubble max widths come from a list-level `LayoutBuilder` (not per-bubble `MediaQuery`) so keyboard insets do not thrash every row.
- Empty-state starfield is off the non-empty chat path.

## Non-goals

- Antigravity / second-provider chat work.
- Replacing `flutter_markdown_plus` unless profiling still shows it as the bottleneck after the above.
- Host-side million-message archives (client cap remains 800 items).

## Profiling checklist (physical device)

1. Cold open a session with long history — land at live end without multi-second jank.
2. Stream a multi-KB reply with code fences while following the live end.
3. Scroll up mid-stream — list must not yank back to bottom.
4. Submit a prompt — keyboard must drop; re-tap composer to type again.
5. Queue while agent is running — keyboard drops; chip appears.

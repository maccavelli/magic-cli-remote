# MADR 0027: OpenCode Chat Streaming & Rendering Hardening

- **Status**: Proposed
- **Date**: 2026-07-26
- **Deciders**: Project Owner, Implementer
- **Related**:
  - [MADR 0018](./0018-MADR-mobile-chat-performance-action-plan.md) — mobile chat performance (foundational optimizations, now done)
  - [MADR 0024](./0024-MADR-stream-coalescing.md) — daemon-side chunkbuf coalescer
  - [MADR 0014](./0014-opencode-session-resync.md) — session resync (history replay path)
  - [MADR 0020](./0020-MADR-opencode-session-tree.md) — session-tree model
  - [MADR 0012](./0012-opencode-engine-management.md) — engine lifecycle

---

## 1. Context

The OpenCode chat UX has two persistent rough edges:

1. **Raw markdown leaks into chat.** During streaming, trailing not-yet-closed
   markdown constructs (`*italic*`, `~~strikethrough~~`, `[links](url)`, lists,
   tables) render as literal syntax characters. The `bufferStreamingMarkdown`
   function only closes `**`, `` ` ``, and ``` fences — leaving the rest raw.

2. **Jank and jitter on the chat screen.** The scrolling is jerky, the screen
   feels unstable, and the transition from streaming to finalized is abrupt
   (the long-stream path swaps from monospace plain-text to styled markdown).

The user has stated that real-time streaming is less important than smoothness,
graphical richness, and polished rendering. This MADR evaluates the full
pipeline from SSE stream to Flutter widget and proposes hardening measures.

A third issue was identified during deeper evaluation:

3. **Tool-group expansion flash and re-layout.** When a new tool call fires, the
   entire transcript row list is rebuilt via `buildTranscriptRows` — an O(n)
   scan over all items. Every `GroupRow` object is recreated, every
   `_ToolGroupTile` rebuilds, and the `ListView` relayouts every item. On a
   200-item transcript with 20 tool calls, this means 20 full O(n) scans and
   20 full-list relayouts. The `ExpansionTile` inside each `_ToolGroupTile`
   receives new widget instances and recalculates its internal `AnimatedSize`,
   causing a visible "flash" even though the expansion state (collapsed) is
   logically preserved.

---

## 2. Current Pipeline

```
opencode serve (SSE)
     │  ~50 events/sec (message.part.delta, etc.)
     ▼
httpagent DialectSession.HandleEvent()
     │  accumulates deltas, emits event.Event
     ▼
chunkbuf coalescer (80ms window, 8KB cap)
     │  folds same-type chunks, flushes on boundaries
     ▼
session.Manager pump → history ring
     │  stamps seq, broadcasts to all WS clients
     ▼
WebSocket (JSON Envelope per event)
     │  ~15-30 frames/sec after coalescing
     ▼
transcripts_notifier.dart (_onEvent)
     │  classifies batchable vs immediate
     │  _foldChunks merges adjacent same-type text events
     │  32ms batch window → _flushSession → applySessionEvent
     ▼
transcript_reducer.dart
     │  _appendChunk: StringBuffer concat for >256 bytes
     │  mutable list, replace-last-index
     ▼
_TranscriptPane (ConsumerStatefulWidget)
     │  watches items.select + status.select
     │  rows memo: swap last row when only tail text changed
     ▼
_AssistantMarkdown (StatefulWidget)
     │  widget cache (_built), throttle timer (120/200/320ms)
     │  streaming: bufferStreamingMarkdown + MarkdownBody
     │  long-stream (>4K chars): SelectableText monospace
     │  finalized: full MarkdownBody once
     ▼
Flutter rendering pipeline
```

### 2.1 Where raw markdown leaks

`bufferStreamingMarkdown` (`streaming_markdown.dart:18-74`) scans for three
constructs:

| Construct | Handled? |
|-----------|----------|
| `**bold**` | Yes |
| `` `code` `` | Yes |
| ` ```fence``` ` | Yes |
| `*italic*` / `_italic_` | **No** |
| `~~strikethrough~~` | **No** |
| `[text](url)` | **No** |
| `- list items` | **No** |
| `1. numbered list` | **No** |
| `# headings` | **No** |
| `> blockquotes` | **No** |
| `| tables |` | **No** |

The function was intentionally scoped to the three most-common constructs in
code-heavy agent output. Expanding it to all GFM constructs would require a
full markdown parser (essentially running the same parse `MarkdownBody` does),
which defeats its purpose as a lightweight pre-render step.

### 2.2 Where jank originates

Five sources of jitter have been identified by profiling and code inspection:

1. **Non-frame-aligned throttle.** `_AssistantMarkdownState._timer` fires on
   wall-clock ticks (120/200/320ms), not display vsync. A timer fire that
   lands mid-frame forces a partial-build that competes with rasterization.

2. **O(n) re-parse on every effective throttle tick.** `MarkdownBody` receives
   the *entire* growing text and re-parses from character 0. For a 10KB reply
   streaming at 30 tokens/sec, this is 30 full parses per second at the
   120ms throttle (each parse walks the full 10KB AST).

3. **ListView scroll correction.** `reverse:true` keeps offset 0 at the
   visual bottom. When a multi-line MarkdownBody grows (e.g. a code block
   expands from 2→8 lines), the layout shift pushes the visual origin up.
   The ListView corrects by adjusting scroll offset, which reads as a
   micro-jump. The code already avoids `maxScrollExtent` chasing per chunk
   (`chat_screen.dart:1642`), but the intrinsic layout shift remains.

4. **The plain-text bailout transition.** At character 4,001, the UI swaps
   from `MarkdownBody` (styled, proportional font) to `SelectableText`
   (monospace, flat). Widget identity changes → old subtree torn down, new
   one built → frame drop. When streaming stops, it swaps back. Two
   transition frames that hitch.

5. **State change frequency.** The 32ms phone-side batch window sits on top
   of the 80ms daemon-side coalescer. Together they deliver 12-15 state
   mutations per second, each triggering `notifyListeners()` → full widget
   tree diff. On lower-end devices, even the diff alone can miss vsync.

6. **Tool-group re-layout on every tool event.** When a `tool_call` event
   fires, `_memoTranscriptRows` cannot take its fast path because the
   items list grew (length +1). It falls through to `buildTranscriptRows`,
   an O(n) scan over every `ChatItem`. All `GroupRow` and `SingleRow`
   objects are recreated. The `ListView` relayouts every item. The
   `ExpansionTile` inside each `_ToolGroupTile` receives a new parent
   widget instance, and its internal `AnimatedSize` recalculates — causing
   a visible "expansion flash" even though the logical expansion state is
   preserved. On a 200-item transcript where the agent runs 20 tools,
   this means 20 full O(n) scans and 20 full-list relayouts for zero
   visual benefit: the tool group's rendered output is identical.

---

## 3. Evaluation of Solutions

### 3.1 Markdown rendering

#### A: Expand `bufferStreamingMarkdown` to full GFM

- **Effort**: Medium (re-implement the `markdown` package's inline parser)
- **Risk**: High. The streaming buffer must be compatible with the parser
  `MarkdownBody` uses; any divergence between "what the buffer closed" and
  "what the parser expects" creates visual glitches worse than raw text.
- **Recommendation**: Not worth the complexity. Better to fix the parser
  path itself.

#### B: Incremental markdown parse (parse only new characters)

- **Effort**: High. The `markdown` package is not incremental — it always
  parses the full document. An incremental fork would need to maintain
  parser state (block context, link reference definitions, list nesting)
  across partial inputs.
- **Risk**: Medium (correctness edge cases around nested constructs)
- **Recommendation**: High potential, but significant engineering. Worth
  prototyping as a separate Dart package.

#### C: Parse markdown on a background isolate

- **Effort**: Medium. `Isolate.run(() => markdownToHtml(text))` + pass
  HTML to `flutter_widget_from_html` or keep `MarkdownBody` with a
  pre-parsed AST.
- **Risk**: Low. The `markdown` package AST is plain Dart objects
  (serializable). Transfer cost is proportional to tree size (~2× text for
  typical agent output).
- **Recommendation**: **Do this first.** Keeps the main isolate free for
  layout and raster. The throttle timer arms the isolate; the result is
  applied in `addPostFrameCallback`. For short messages (<500 chars), skip
  the isolate (transfer overhead dominates).

#### D: Pre-render to a `Widget` tree on isolate (impossible)

- **Effort**: N/A. Widgets are not `Sendable`.
- **Recommendation**: Not possible.

#### E: Replace `flutter_markdown_plus` with a custom canvas renderer

- **Effort**: Very high (re-implement TextPainter layout, inline styles,
  code blocks, blockquotes, tables, links, selection)
- **Risk**: High (maintenance burden, accessibility regression)
- **Recommendation**: Only if profiling proves `MarkdownBody` layout is
  the dominant frame cost after options C and G are implemented.

### 3.2 Jank reduction

#### F: Frame-aligned throttle via `addPostFrameCallback`

- **Effort**: Low. Replace `Timer(_throttle, ...)` with a flag set in
  `didUpdateWidget` and cleared in `addPostFrameCallback`.
- **Risk**: Low. Removes wall-clock jitter. The effective frame rate drops
  to display refresh (60/120Hz), which is the maximum the user can see.
- **Recommendation**: **Do this.** Strictly better than wall-clock timers.

Flow change:
```dart
// Before (current)
void didUpdateWidget(old) {
  if (_upToDate) return;
  _timer ??= Timer(_throttle, () { setState(() => _built = ...); });
}

// After
bool _dirty = false;
void didUpdateWidget(old) {
  if (_upToDate) return;
  if (!_dirty) {
    _dirty = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      _dirty = false;
      setState(() => _built = _render(context, widget.data, true));
    });
  }
}
```

This guarantees at most one re-render per frame. On a 60Hz display,
that's a maximum of 16ms between updates (vs. 120ms worst case with
the timer).

#### G: Virtual scrolling with extent estimation

- **Effort**: Medium. Replace `ListView.builder` with `SuperSliverList` or
  similar virtual list that estimates off-screen item extents.
- **Risk**: Medium. `SuperSliverList` is a third-party package; its layout
  algorithm differs from Flutter's built-in and may interact unexpectedly
  with `reverse:true` and `findChildIndexCallback`.
- **Recommendation**: **Prototype.** The current `ListView.builder` with
  `scrollCacheExtent` of 900px works adequately for 20-50 item chats.
  Worth evaluating on pathological cases (200+ items, mixed tool groups)
  before committing.

#### H: Item extent estimation for MarkdownBody

- **Effort**: Medium. Provide a `prototypeItem` or `itemExtent` to the
  list delegate. For chat, this means estimating bubble height from text
  length (e.g., ~1px per char at 13px font, 90% width).
- **Risk**: Low. Over-estimation wastes a tiny amount of pre-build work;
  under-estimation causes the same layout-shift jitter we have now.
- **Recommendation**: **Do this.** Simple heuristic: `max(48.0, text.length * 0.11)` for assistant bubbles provides a reasonable floor that eliminates the worst jump cases (empty→filled bubble).

#### I: Suppress rebuilds during scroll/fling

- **Effort**: Low. The `ChatScrollActivitySensor` already provides
  `isScrolling(context)`. During active scroll, skip `setState` in
  `_AssistantMarkdownState.didUpdateWidget` entirely.
- **Risk**: Low. The last-rendered state is preserved until the user
  stops scrolling, then the current text renders. The user is reading
  (not watching the stream), so a brief freeze during a fling is
  invisible.
- **Recommendation**: **Do this.** Reduces frame competition between the
  scroll physics and the markdown re-build during active flings.

#### J: Eliminate the plain-text bailout transition

- **Effort**: Medium. Instead of switching widget identity at 4,000 chars,
  render `MarkdownBody` with a `maxLines` constraint and `overflow:
  TextOverflow.clip` during streaming. Let the parser accumulate normally.
  If performance degrades, the isolate-based parse (option C) handles it.
- **Risk**: Low. The 4,000-char limit was added before the coalescer and
  before the isolate option existed. With these in place, the long-stream
  path may not be needed.
- **Recommendation**: **Do this after C and F.** Remove the plain-text
  bailout once the isolate parse handles the load; if parse still
  dominates, keep it but hide the transition with an `AnimatedSwitcher`.

#### K: Reduce daemon-to-phone state change frequency

- **Effort**: Low. Increase the `chunkbuf` coalesce window from 80ms to
  120ms, and the phone batch window from 32ms to 50ms. Together they
  deliver ~8 state mutations/sec instead of ~15.
- **Risk**: Low. TTFT (time-to-first-token) is preserved by the
  leading-edge rule; only mid-stream granularity coarsens.
- **Recommendation**: **Do this after measuring.** Run with the current
   values first; profile to determine actual state-change frequency. Only
   adjust if >12 mutations/sec are observed in production.

#### L: Append fast-path in `_memoTranscriptRows`

- **Effort**: Low (~15 lines). Add a "new item appended" branch to the
  memo that creates a new `SingleRow` without scanning the full items list.
- **Risk**: Low. The branch only fires when (a) exactly one item was appended,
  (b) all prefix items are `identical` to before, and (c) the new item is
  NOT a completed tool (completed tools may fold into an existing group,
  requiring the full `buildTranscriptRows` scan).
- **Recommendation**: **Do this first alongside F.** Eliminates O(n) scanning
  and full-ListView relayout on every `tool_call`, `user_message`, `system`,
  and non-groupable tool completion event.

### 3.3 Summary of recommendations

| Priority | Proposal | Effort | Expected impact |
|----------|----------|--------|-----------------|
| **P0** | F: Frame-aligned throttle | Low | Eliminates timer-jitter stutter |
| **P0** | L: Append fast-path in row memo | Low | Eliminates O(n) scan + ListView relayout on every tool event |
| **P1** | C: Markdown parse on isolate | Medium | Removes O(n) parse from main thread |
| **P1** | I: Suppress rebuilds during scroll | Low | Reduces frame competition |
| **P2** | H: Item extent estimation | Medium | Reduces scroll-position jitter |
| **P2** | J: Remove plain-text bailout (after C) | Medium | Eliminates widget-identity swap hitch |
| **P3** | B: Incremental markdown parser | High | Long-term parse cost reduction |
| **P3** | K: Coarsen batch windows | Low | Reduces pipeline pressure |
| **P4** | G: Virtual list (SuperSliverList) | Medium | Improves large-transcript scrolling |
| **P4** | E: Custom canvas renderer | Very high | Last resort if isolate parse insufficient |

---

## 4. Open Questions

1. **Does `flutter_markdown_plus` leak memory on large documents?**
   The AST is held in the `MarkdownBody` widget tree. If a 30KB reply
   generates a deep AST, does it persist across rebuilds? Answer: the
   widget cache (`_built`) holds the `MarkdownBody` widget; the AST is
   internal to `MarkdownBody` and is rebuilt on each `data` change.

2. **Is the isolate transfer cost worth it for short messages?**
   Benchmark needed. Proposal C suggests a 500-char threshold. Below this,
   parse on the main thread (sub-millisecond parse time).

3. **Does `SuperSliverList` work with `reverse:true`?**
   The package docs don't explicitly mention reverse lists. Evaluation
   needed before adopting.

4. **Should we switch from `flutter_markdown_plus` back to `flutter_markdown`?**
   The `_plus` fork is actively maintained (Foresight Mobile) while the
   Google original is discontinued. The `_plus` fork has `noScroll` mode
   (useful for embedding in scroll views), theme-aware blockquotes, and
   active bug fixes. Stay with `_plus` and consider contributing back
   any fixes we need.

---

## 5. Phasing

### Phase 1 (low-hanging, high-impact)
- Implement F (frame-aligned throttle)
- Implement I (suppress rebuilds during scroll)
- Implement L (append fast-path in row memo) — eliminates tool-group re-layout
- Add `addPostFrameCallback` wiring in `_AssistantMarkdownState`
- Add "new item appended" branch in `_memoTranscriptRows`

### Phase 2 (structural improvements)
- Implement C (isolate-based markdown parse)
- Implement H (item extent estimation heuristic)
- Measure impact; determine if J (remove bailout) is needed

### Phase 3 (deep optimization — only if Phase 1-2 insufficient)
- Prototype B (incremental parser)
- Evaluate G (SuperSliverList)
- Adjust K (batch windows)

### Phase 4 (last resort)
- Consider E (custom canvas renderer) only if profiling proves
  `MarkdownBody` layout is still the bottleneck

---

## 6. Risk: Markdown Corner Cases

The `bufferStreamingMarkdown` function, even expanded, has a fundamental
limitation: it guesses what the stream will produce. It cannot handle:

- **Nested constructs across chunk boundaries**: `**bold *and italic**` where
  the split lands between `*` and `and`.
- **Link reference definitions**: `[text][ref]` followed 500 chars later by
  `[ref]: https://...`. The label and definition are separated; closing the
  bracket doesn't make the link renderable.
- **HTML entities**: `&amp;` is three chunks, and the semicolon arrives last.

These are rare in agent output (which is predominantly prose, code blocks,
and bullet-point plans), but they reinforce that a streaming buffer is a
best-effort aesthetic fix, not a correctness guarantee.

Once the isolate-based parse (C) is in place, the `bufferStreamingMarkdown`
function may become unnecessary — the isolate can parse the full text
asynchronously and the result replaces the old AST in a post-frame callback.
The raw-markdown problem is fundamentally a **latency** problem (the parse
hasn't happened yet when the frame is displayed), and moving the parse
off the main thread eliminates the latency-pressure trade-off.

---

## 7. Implementation Notes

### 7.1 Frame-aligned throttle (Proposal F)

```dart
class _AssistantMarkdownState extends State<_AssistantMarkdown> {
  bool _dirty = false;

  @override
  void didUpdateWidget(covariant _AssistantMarkdown old) {
    super.didUpdateWidget(old);
    if (old.data != widget.data && widget.data.length <= kAssistantShowMoreChars) {
      _expanded = false;
    }
    if (!widget.streaming) {
      _dirty = false;  // cancel pending frame callback
      if (!_upToDate(false)) {
        setState(() => _built = _render(context, widget.data, false));
      }
      return;
    }
    if (_upToDate(true)) return;
    if (ChatScrollActivity.isScrolling(context)) return;  // Proposal I
    if (_dirty) return;
    _dirty = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      _dirty = false;
      if (_upToDate(true)) return;
      setState(() => _built = _render(context, widget.data, true));
    });
  }
}
```

### 7.2 Isolate-based parse (Proposal C)

```dart
Future<String> _parseMarkdown(String text) async {
  if (text.length < 500) {
    return text;  // fast path: render directly, MarkdownBody handles it
  }
  return Isolate.run(() {
    // The markdown package is pure Dart — works in isolates.
    final ast = md.Document().parse(text);
    return renderToHtml(ast);  // or return the AST for a custom widget
  });
}
```

For `flutter_markdown_plus`, the best approach is to parse to an AST on the
isolate, send the AST back to the main isolate, and feed it to `MarkdownBody`
via `data:` with the original text (since `flutter_markdown_plus` doesn't
expose an AST-accepting constructor). The parse cost is still paid on the
isolate; `MarkdownBody` re-parses on the main thread but with the same text
so it hits the internal cache. Alternatively, switch to `Markdown` (the raw
widget from `flutter_markdown_plus`) which has a `data` setter that triggers
the parse — still on the main thread.

**Better approach**: Create a custom widget that accepts a pre-parsed AST.
`flutter_markdown_plus` builds on the `markdown` package's AST but doesn't
expose it publicly. We can fork the relevant `_build` methods or use the
`MarkdownElementBuilder` interface to intercept nodes.

### 7.3 Item extent estimation (Proposal H)

```dart
double _estimateAssistantHeight(String text, double maxWidth) {
  if (text.isEmpty) return 48.0;
  // Rough: ~80 chars per line at 13px font, 90% width, ~20px line height.
  final lines = text.length / ((maxWidth * 0.9) / 7.5);  // 7.5px avg char width
  return max(48.0, lines * 20.0 + 24.0);  // +24px for padding
}
```

Use this in `ListView.builder` via `prototypeItem` or a custom
`SliverChildBuilderDelegate` with `estimatedMaxScrollOffset`.

---

## 8. Cross-references

| What | Where | Status |
|------|-------|--------|
| Streaming markdown buffer | `apps/mobile/lib/data/chat/streaming_markdown.dart` | Covers `**`, `` ` ``, ``` only |
| Markdown rendering widget | `apps/mobile/lib/features/chat/chat_bubble.dart:492-711` | `_AssistantMarkdown` + throttle |
| Transcript panel | `apps/mobile/lib/features/chat/transcript_pane.dart` | `ListView.builder` with row memo |
| Daemon coalescer | `internal/chunkbuf/chunkbuf.go` | 80ms window, 8KB cap |
| OpenCode dialect | `internal/provider/opencode/http.go` | `HandleEvent` event mapping |
| Transport session | `internal/provider/httpagent/session.go` | `Emit` with chunkbuf wiring |
| Phone-side batching | `apps/mobile/lib/state/transcripts_notifier.dart` | 32ms batch, `_foldChunks` |
| Transcript reducer | `apps/mobile/lib/data/chat/transcript_reducer.dart` | `_appendChunk`, mutable list |
| Stream models | `apps/mobile/lib/data/chat/chat_models.dart` | `ChatItem`, `SessionTranscript` |
| Scroll activity sensor | `apps/mobile/lib/theme/scroll_activity.dart` | `isScrolling(context)` |
| flutter_markdown_plus | `pubspec.yaml` → v1.0.12 | Actively maintained fork |
| markdown parser | `pubspec.yaml` → v7.3.0 | Dart markdown package |
| MADR 0018 (prior performance work) | `docs/0018-MADR-mobile-chat-performance-action-plan.md` | Phases B1-B5, E1-E4 done |

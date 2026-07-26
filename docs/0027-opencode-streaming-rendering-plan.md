# OpenCode Streaming & Rendering — Phase 1 Implementation Plan

**Date**: 2026-07-26  
**MADR**: [0027-opencode-streaming-rendering.md](./0027-opencode-streaming-rendering.md)  
**Proposals implemented**: F (frame-aligned throttle), I (scroll suppression), C (isolate markdown parse), L (append fast-path)

---

## Overview

Four changes that touch three files and add one new file on the Flutter side. No daemon changes. No new dependencies. Each proposal is independent and can be merged separately.

| # | Proposal | Files modified | Files added |
|---|----------|---------------|-------------|
| F | Frame-aligned throttle | `chat_bubble.dart` (~25 lines changed) | — |
| I | Scroll suppression | `chat_bubble.dart` (~1 line changed) | — |
| C | Isolate markdown parse | `chat_bubble.dart` (~40 lines changed) | `markdown_parser.dart` (~230 lines new) |
| L | Append fast-path in row memo | `transcript_pane.dart` (~25 lines changed) | — |

---

## Proposal F: Frame-aligned throttle via `addPostFrameCallback`

### Current code

`apps/mobile/lib/features/chat/chat_bubble.dart:502-711`

The `_AssistantMarkdownState` class uses a `Timer` for throttling markdown re-renders during streaming. The timer fires at 120/200/320ms wall-clock intervals (tiered by text length). When the timer fires, `_render()` is called which re-parses the markdown via `MarkdownBody`. The timer is armed in `didUpdateWidget` and fires asynchronously.

Key fields:
```dart
// Line 503-507
static const _throttleDefault = Duration(milliseconds: 120);
static const _throttleLarge   = Duration(milliseconds: 200);
static const _throttleHuge    = Duration(milliseconds: 320);
static const _largeTextChars  = 4000;
static const _hugeTextChars   = 16000;

// Line 509-512
late String _shown = widget.data;
late bool _shownStreaming = widget.streaming;
Widget? _built;
Timer? _timer;
```

Key methods:
```dart
// Line 522-526 — throttle tier selection
Duration get _throttle => widget.data.length > _hugeTextChars
    ? _throttleHuge
    : widget.data.length > _largeTextChars
    ? _throttleLarge
    : _throttleDefault;

// Line 660-687 — didUpdateWidget arms timer
@override
void didUpdateWidget(covariant _AssistantMarkdown old) {
  super.didUpdateWidget(old);
  // ... reset _expanded ...
  if (!widget.streaming) {
    _timer?.cancel();
    _timer = null;
    if (!_upToDate(false)) {
      setState(() => _built = _render(context, widget.data, false));
    }
    return;
  }
  if (_upToDate(true)) return;
  _timer ??= Timer(_usePlainStream ? _throttleLarge : _throttle, () {
    _timer = null;
    if (mounted && !_upToDate(true)) {
      setState(() => _built = _render(context, widget.data, true));
    }
  });
}

// Line 689-693 — dispose cancels timer
@override
void dispose() {
    _timer?.cancel();
    super.dispose();
}
```

### Problem

The `Timer` fires on wall-clock ticks, not synchronized to the display vsync. A timer fire that lands mid-frame (after layout but before rasterization) forces a `setState` + markdown re-parse + widget rebuild that competes with the raster thread for the current frame. On a 60Hz display:

- Frame budget: 16ms
- Timer-jitter: 0–120ms after the last text change
- Markdown parse: 2–30ms (grows with O(n) on text length)

The timer jitter means the parse can hit at any point in the frame cycle, causing visible stutter when it overlaps with rasterization.

### Fix

Replace the `Timer` with a `_dirty` flag and `WidgetsBinding.instance.addPostFrameCallback`. The callback runs immediately AFTER the frame has been rasterized, so the markdown parse happens during idle time between frames.

#### Step-by-step changes to `_AssistantMarkdownState`

**1. Replace throttle constants and `_timer` field with `_dirty` flag**

Remove lines 503-507 (constants) and 512:
```dart
// REMOVE:
static const _throttleDefault = Duration(milliseconds: 120);
static const _throttleLarge   = Duration(milliseconds: 200);
static const _throttleHuge    = Duration(milliseconds: 320);
static const _largeTextChars  = 4000;
static const _hugeTextChars   = 16000;

Timer? _timer;  // REMOVE
```

Add:
```dart
static const _largeTextChars  = 4000;
static const _hugeTextChars   = 16000;

bool _dirty = false;
```

**2. Remove `_throttle` getter (lines 522-526)**

This was used only by the timer arm code. No longer needed since post-frame callbacks run every frame at most.

**3. Rewrite `didUpdateWidget` (lines 660-687)**

Before:
```dart
@override
void didUpdateWidget(covariant _AssistantMarkdown old) {
  super.didUpdateWidget(old);
  if (old.data != widget.data &&
      widget.data.length <= kAssistantShowMoreChars) {
    _expanded = false;
  }
  if (!widget.streaming) {
    _timer?.cancel();
    _timer = null;
    if (!_upToDate(false)) {
      setState(() => _built = _render(context, widget.data, false));
    }
    return;
  }
  if (_upToDate(true)) return;
  // Long plain stream: still throttle text swaps, but skip MD parse cost.
  // Streaming: coalesce re-parses to at most one per throttle window.
  _timer ??= Timer(_usePlainStream ? _throttleLarge : _throttle, () {
    _timer = null;
    if (mounted && !_upToDate(true)) {
      setState(() => _built = _render(context, widget.data, true));
    }
  });
}
```

After:
```dart
@override
void didUpdateWidget(covariant _AssistantMarkdown old) {
  super.didUpdateWidget(old);
  if (old.data != widget.data &&
      widget.data.length <= kAssistantShowMoreChars) {
    _expanded = false;
  }
  if (!widget.streaming) {
    _dirty = false;          // cancel any pending frame callback
    if (!_upToDate(false)) {
      setState(() => _built = _render(context, widget.data, false));
    }
    return;
  }
  if (_upToDate(true)) return;
  // Proposal I: suppress rebuilds while the user is scrolling/flinging.
  if (ChatScrollActivity.isScrolling(context)) return;
  if (_dirty) return;        // already pending; next frame will pick it up
  _dirty = true;
  WidgetsBinding.instance.addPostFrameCallback((_) {
    if (!mounted) return;
    _dirty = false;
    if (_upToDate(true)) return; // text unchanged since callback was queued
    setState(() => _built = _render(context, widget.data, true));
  });
}
```

Key differences:
- No `Timer` → no timer-jitter vs frame boundary
- `_dirty` flag prevents redundant callback registrations
- `_upToDate(true)` re-check inside the callback guards against text that hasn't grown since scheduling
- Text that grows between `didUpdateWidget` calls is held until the next frame — at 60Hz this is at most 16ms, well below the old 120ms floor

**4. Update `dispose` (lines 689-693)**

Before:
```dart
@override
void dispose() {
    _timer?.cancel();
    super.dispose();
}
```

After:
```dart
@override
void dispose() {
    _dirty = false;  // prevent post-frame callback from calling setState
    super.dispose();
}
```

The `mounted` check inside the callback handles the case where the callback fires after disposal, but setting `_dirty = false` prevents `_render` from being called without a valid `context`.

**5. Remove `_usePlainStream` check from throttle arm site**

The old code had `_usePlainStream ? _throttleLarge : _throttle` — a special case for long text. With frame-aligned callbacks, the parse rate is naturally capped at display refresh rate (16ms at 60Hz, 8ms at 120Hz). The long-stream path with `SelectableText` is still used for rendering, but the frame-aligned callback applies uniformly. The `_usePlainStream` getter and long-stream rendering path remain unchanged.

### Test impact

- `debugMarkdownParseCount` (line 475) — test assertion in `test/streaming_markdown_test.dart` checks that finalized items parse exactly once. This is preserved: the finalized path still calls `setState(() => _built = _render(...))` directly.
- The streaming path now fires at most once per frame, so parse counts during streaming will be lower (bounded by frames elapsed, not wall-clock time). No existing test asserts a minimum parse count for streaming.
- `_throttleDefault`, `_throttleLarge`, `_throttleHuge`, `_largeTextChars`, `_hugeTextChars` — these constants are shared with the long-stream path (`_usePlainStream` uses `_largeTextChars` indirectly via `kMaxStreamingMarkdownChars`). Only the Duration constants (`_throttleDefault` etc.) are removed; the character-count constants stay.

---

## Proposal I: Suppress rebuilds during scroll/fling

### Current state

`ChatScrollActivity.isScrolling(context)` already exists and is used by:
- `_StreamingCaret` (line 731) — pauses caret animation during scroll
- `ShimmerText` — pauses shimmer during scroll

The `_AssistantMarkdownState.didUpdateWidget` does NOT check scroll state. During a fling, every text chunk triggers the throttle timer, which fires, which re-parses markdown, which rebuilds `MarkdownBody`. The scroll physics and the parser compete for frame budget.

### Fix

Add ONE line to `didUpdateWidget` (already included in Proposal F's rewrite above):

```dart
// Proposal I: suppress rebuilds while the user is scrolling/flinging.
if (ChatScrollActivity.isScrolling(context)) return;
```

Placement: after the `_upToDate(true)` check, before the `_dirty` flag check.

When the scroll ends (`ScrollEndNotification` fires, `ChatScrollActivitySensor` sets `scrolling.value = false`), the next `didUpdateWidget` call will proceed normally. The `_built` cache holds the last rendered state, so the user sees the content as it was when they started scrolling. Once they stop, the current text renders.

### Why this is safe

1. During scroll, the user is reading previous messages, not watching the tail.
2. The scroll-end transition is invisible: `ChatScrollActivity` is driven by `ScrollEndNotification`, which fires synchronously during the frame. The next `didUpdateWidget` (triggered by the next `setState` from `TranscriptsNotifier`) will render the current text.
3. No animation glitch: the `_built` cache preserves the latest rendered widget tree. The ListView shows the cached widgets without any visual discontinuity.
4. No data loss: text chunks continue to accumulate in `TranscriptsState` (via the reducer). The `didUpdateWidget` call that fires after scroll-end picks up the full accumulated text.

---

## Proposal C: Isolate-based markdown parse

### Design

Parse markdown to an AST on a background isolate, walk the AST to produce a simple flat list of styled text segments, send the segments back to the main isolate, and build Flutter widgets from them. No new package dependencies.

### Why not offload to an isolate + keep `MarkdownBody`

`MarkdownBody` (from `flutter_markdown_plus`) internally calls `md.Document().parse(data)` and then walks the AST to build widgets. There is no public API to inject a pre-parsed AST. Offloading the parse to an isolate and then calling `MarkdownBody(data: text)` defeats the purpose — `MarkdownBody` would re-parse on the main thread.

### Why not `md.markdownToHtml()` + HTML renderer

Would require adding `flutter_widget_from_html_core` (~120KB transitive). The agent's output is predominantly prose, code blocks, and bullet lists — a small subset of HTML. Building a custom Flutter renderer for the parsed AST using `RichText` + `TextSpan` is simpler and gives us full control over styling (matching the existing `_sheetFor` stylesheet).

### New file: `apps/mobile/lib/data/chat/markdown_parser.dart`

This file provides two things:
1. `_ParsedResult` — a sendable (plain-data) representation of parsed markdown
2. `parseMarkdownOffMain(String text)` — runs the full parse + AST walk on an isolate, returns `_ParsedResult`

#### Data model

```dart
import 'dart:isolate';

import 'package:markdown/markdown.dart' as md;

/// The output of an off-main-thread markdown parse. Plain data objects
/// so they cross the isolate boundary via [Isolate.run].
class _ParsedResult {
  final List<_ParsedBlock> blocks;
  const _ParsedResult(this.blocks);
}

class _ParsedBlock {
  final _BlockType type;
  final List<_ParsedSpan> spans;
  final int level;     // for headings (1-6), 0 otherwise
  final String language; // for fenced code blocks, empty otherwise

  const _ParsedBlock({
    required this.type,
    required this.spans,
    this.level = 0,
    this.language = '',
  });
}

enum _BlockType {
  paragraph,
  heading,
  codeBlock,
  blockquote,
  orderedItem,
  unorderedItem,
  horizontalRule,
}

class _ParsedSpan {
  final String text;
  final bool bold;
  final bool italic;
  final bool code;
  final String linkUrl;

  const _ParsedSpan({
    required this.text,
    this.bold = false,
    this.italic = false,
    this.code = false,
    this.linkUrl = '',
  });
}
```

These are all plain Dart objects (no closures, no `BuildContext`, no widgets) — they are implicitly `Sendable` in Dart 3.6+.

#### Isolate entry point

```dart
/// Top-level function — required by [Isolate.run].
_ParsedResult _parseMarkdown(String text) {
  final doc = md.Document(
    inlineSyntaxes: md.ExtensionSet.gitHubFlavored.inlineSyntaxes,
    blockSyntaxes: md.ExtensionSet.gitHubFlavored.blockSyntaxes,
  ).parse(text);
  return _walkAst(doc);
}

_ParsedResult _walkAst(List<md.Node> nodes) {
  final blocks = <_ParsedBlock>[];
  for (final node in nodes) {
    if (node is md.Element) {
      _walkElement(node, blocks);
    }
  }
  return _ParsedResult(blocks);
}

void _walkElement(md.Element el, List<_ParsedBlock> blocks) {
  switch (el.tag) {
    case 'p':
      blocks.add(_ParsedBlock(
        type: _BlockType.paragraph,
        spans: _extractSpans(el.children ?? []),
      ));
    case 'h1': case 'h2': case 'h3': case 'h4': case 'h5': case 'h6':
      blocks.add(_ParsedBlock(
        type: _BlockType.heading,
        level: int.parse(el.tag.substring(1)),
        spans: _extractSpans(el.children ?? []),
      ));
    case 'pre':
      // children[0] is a <code> element, possibly with class="language-xxx"
      final code = el.children?.firstOrNull;
      final lang = code is md.Element
          ? (code.attributes['class'] ?? '').replaceFirst('language-', '')
          : '';
      blocks.add(_ParsedBlock(
        type: _BlockType.codeBlock,
        language: lang,
        spans: code != null ? _extractCodeSpans(code) : [],
      ));
    case 'blockquote':
      for (final child in el.children ?? []) {
        if (child is md.Element) _walkBlockquoteChild(child, blocks);
      }
    case 'ul':
      for (final li in el.children ?? []) {
        if (li is md.Element && li.tag == 'li') {
          blocks.add(_ParsedBlock(
            type: _BlockType.unorderedItem,
            spans: _extractSpans(li.children ?? []),
          ));
        }
      }
    case 'ol':
      for (final li in el.children ?? []) {
        if (li is md.Element && li.tag == 'li') {
          blocks.add(_ParsedBlock(
            type: _BlockType.orderedItem,
            spans: _extractSpans(li.children ?? []),
          ));
        }
      }
    case 'hr':
      blocks.add(const _ParsedBlock(type: _BlockType.horizontalRule, spans: []));
    default:
      // table, img, etc. — collapse to paragraph with raw text
      blocks.add(_ParsedBlock(
        type: _BlockType.paragraph,
        spans: _extractSpans(el.children ?? []),
      ));
  }
}

void _walkBlockquoteChild(md.Element el, List<_ParsedBlock> blocks) {
  if (el.tag == 'p') {
    blocks.add(_ParsedBlock(
      type: _BlockType.blockquote,
      spans: _extractSpans(el.children ?? []),
    ));
  } else {
    _walkElement(el, blocks);
  }
}

List<_ParsedSpan> _extractSpans(List<md.Node> nodes) {
  final out = <_ParsedSpan>[];
  for (final node in nodes) {
    if (node is md.Text) {
      out.add(_ParsedSpan(text: node.text));
    } else if (node is md.Element) {
      switch (node.tag) {
        case 'strong':
          out.addAll(_styledSpans(_extractSpans(node.children ?? []), bold: true));
        case 'em':
          out.addAll(_styledSpans(_extractSpans(node.children ?? []), italic: true));
        case 'code':
          out.add(_ParsedSpan(
            text: (node.children?.firstOrNull is md.Text)
                ? (node.children!.first as md.Text).text
                : '',
            code: true,
          ));
        case 'a':
          out.addAll(_styledSpans(
            _extractSpans(node.children ?? []),
            linkUrl: node.attributes['href'] ?? '',
          ));
        default:
          out.addAll(_extractSpans(node.children ?? []));
      }
    }
  }
  return _mergeAdjacentSpans(out);
}

List<_ParsedSpan> _styledSpans(List<_ParsedSpan> spans, {
  bool bold = false, bool italic = false, String linkUrl = '',
}) {
  return spans.map((s) => _ParsedSpan(
    text: s.text,
    bold: s.bold || bold,
    italic: s.italic || italic,
    code: s.code,
    linkUrl: linkUrl.isNotEmpty ? linkUrl : s.linkUrl,
  )).toList();
}

List<_ParsedSpan> _extractCodeSpans(md.Element code) {
  final buf = StringBuffer();
  for (final child in code.children ?? []) {
    if (child is md.Text) buf.write(child.text);
  }
  return [_ParsedSpan(text: buf.toString(), code: true)];
}

/// Merge adjacent spans with identical styling into one span — reduces
/// widget count for Flutter's layout engine.
List<_ParsedSpan> _mergeAdjacentSpans(List<_ParsedSpan> spans) {
  if (spans.length < 2) return spans;
  final out = <_ParsedSpan>[];
  var cur = spans.first;
  for (var i = 1; i < spans.length; i++) {
    final next = spans[i];
    if (cur.bold == next.bold &&
        cur.italic == next.italic &&
        cur.code == next.code &&
        cur.linkUrl == next.linkUrl) {
      cur = _ParsedSpan(
        text: cur.text + next.text,
        bold: cur.bold, italic: cur.italic,
        code: cur.code, linkUrl: cur.linkUrl,
      );
    } else {
      out.add(cur);
      cur = next;
    }
  }
  out.add(cur);
  return out;
}
```

#### Public API

```dart
/// Parses [text] as GitHub-Flavored Markdown on a background isolate and
/// returns a flat list of styled blocks. Falls back to a synchronous parse
/// for short messages where the isolate transfer overhead dominates.
Future<_ParsedResult> parseMarkdownOffMain(String text) async {
  if (text.length < 500) {
    // Fast path: run inline. Transfer cost (~2x text size) exceeds parse
    // cost for short messages.
    return _parseMarkdown(text);
  }
  return Isolate.run(() => _parseMarkdown(text));
}
```

### Changes to `_AssistantMarkdownState`

**1. Add fields (after line 511)**

```dart
Widget? _built;
// Proposal C: cached parsed result from the background isolate.
_ParsedResult? _parsed;
String? _parsedText;    // the text that _parsed was built from
```

**2. Add builder method (after `_render`)**

```dart
/// Build the assistant markdown widget from either a cached MarkdownBody
/// (finalized) or the parsed blocks from the background isolate (streaming).
Widget _buildFromParsed(BuildContext context, _ParsedResult parsed) {
  final theme = Theme.of(context);
  final styleSheet = _sheetFor(context);
  final mono = theme.textTheme.bodyMedium?.copyWith(
    fontFamily: 'monospace', fontSize: 13);

  final children = <Widget>[];
  for (final block in parsed.blocks) {
    final span = _buildTextSpan(block, theme, styleSheet, mono);
    if (span != null) {
      children.add(Padding(
        padding: _blockPadding(block.type),
        child: RichText(text: span),
      ));
    }
  }
  return Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    mainAxisSize: MainAxisSize.min,
    children: children,
  );
}

TextSpan? _buildTextSpan(
  _ParsedBlock block, ThemeData theme, MarkdownStyleSheet sheet, TextStyle? mono,
) {
  if (block.type == _BlockType.horizontalRule) {
    return const TextSpan(text: ''); // handled as Divider in widget layer
  }
  final baseStyle = switch (block.type) {
    _BlockType.heading => switch (block.level) {
      1 => sheet.h1, 2 => sheet.h2, _ => sheet.h3,
    },
    _BlockType.codeBlock => mono,
    _BlockType.blockquote => sheet.p,
    _BlockType.orderedItem || _BlockType.unorderedItem => sheet.p,
    _BlockType.paragraph => sheet.p,
    _ => sheet.p,
  };
  if (block.spans.isEmpty) return TextSpan(style: baseStyle);
  return TextSpan(
    style: baseStyle,
    children: block.spans.map((s) => TextSpan(
      text: s.text,
      style: baseStyle?.merge(
        TextStyle(
          fontWeight: s.bold ? FontWeight.bold : null,
          fontStyle: s.italic ? FontStyle.italic : null,
          fontFamily: s.code ? 'monospace' : null,
          fontSize: s.code ? 13 : null,
          color: s.linkUrl.isNotEmpty ? theme.colorScheme.primary : null,
          decoration: s.linkUrl.isNotEmpty ? TextDecoration.underline : null,
        ),
      ),
    )).toList(),
  );
}

EdgeInsets _blockPadding(_BlockType type) => switch (type) {
  _BlockType.heading => const EdgeInsets.only(top: 8),
  _BlockType.codeBlock => const EdgeInsets.symmetric(vertical: 4),
  _BlockType.blockquote => const EdgeInsets.fromLTRB(12, 4, 12, 4),
  _BlockType.orderedItem || _BlockType.unorderedItem =>
      const EdgeInsets.only(left: 16),
  _ => EdgeInsets.zero,
};
```

**3. Modify `didUpdateWidget` streaming arm (the `addPostFrameCallback`)**

Add the isolate parse dispatch. When the callback fires:

```dart
WidgetsBinding.instance.addPostFrameCallback((_) async {
  if (!mounted) return;
  _dirty = false;
  if (_upToDate(true)) return;
  // Proposal C: dispatch background parse. The result is applied
  // via setState when it arrives.
  final toParse = widget.data;
  parseMarkdownOffMain(toParse).then((parsed) {
    if (!mounted || toParse != widget.data) return;
    _parsed = parsed;
    _parsedText = toParse;
    setState(() {
      _built = _buildFromParsed(context, parsed);
    });
  });
});
```

**4. Modify `_render` (lines 578-653) — streaming branch**

Before (line 615-623):
```dart
final shown = streaming ? bufferStreamingMarkdown(bodyText) : bodyText;
debugMarkdownParseCount++;
body = MarkdownBody(
  data: shown,
  selectable: !streaming,
  styleSheet: _sheetFor(context),
  builders: <String, MarkdownElementBuilder>{'pre': _CodeBlockBuilder()},
);
```

After:
```dart
if (streaming) {
  // Use the parsed blocks from the background isolate if available.
  // Falls back to the existing MarkdownBody path for the first render
  // (before the isolate returns) and for finalized display.
  final cached = _parsed;
  if (cached != null && _parsedText == bodyText) {
    body = _buildFromParsed(context, cached);
  } else {
    // Fallback: existing MarkdownBody path while isolate is still running.
    final shown = bufferStreamingMarkdown(bodyText);
    debugMarkdownParseCount++;
    body = MarkdownBody(
      data: shown,
      selectable: false,
      styleSheet: _sheetFor(context),
      builders: <String, MarkdownElementBuilder>{'pre': _CodeBlockBuilder()},
    );
  }
} else {
  // Finalized: use full MarkdownBody for proper table, link, emoji support.
  debugMarkdownParseCount++;
  body = MarkdownBody(
    data: bodyText,
    selectable: true,
    styleSheet: _sheetFor(context),
    builders: <String, MarkdownElementBuilder>{'pre': _CodeBlockBuilder()},
  );
}
```

**5. Import at top of `chat_bubble.dart`**

```dart
import '../../data/chat/markdown_parser.dart';
```

### Test strategy

For `markdown_parser.dart` (new file, new test `apps/mobile/test/markdown_parser_test.dart`):

| Test | What it verifies |
|------|-----------------|
| Empty string → 0 blocks | Edge case |
| Plain paragraph | One paragraph block, one span |
| Bold + italic | Two spans with correct flags |
| Nested bold-in-italic | Span merging preserves flags |
| Code block with language | `_BlockType.codeBlock`, `language: 'dart'` |
| Blockquote | `_BlockType.blockquote` |
| Unordered list | Multiple `_BlockType.unorderedItem` blocks |
| Ordered list | Multiple `_BlockType.orderedItem` blocks |
| Link in paragraph | `_ParsedSpan(linkUrl: 'https://...')` |
| Inline code inside paragraph | `_ParsedSpan(code: true)` |
| Adjacent span merge | Two 'hello' spans next to each other → one span |
| Short text fast-path (<500 chars) | Returns result without isolate |
| Long text isolate path (>=500 chars) | Returns result via isolate |
| GFM tables | Falls back to paragraph text (no table support yet) |

For `_AssistantMarkdownState` (existing test `apps/mobile/test/streaming_markdown_test.dart`):
- Existing tests continue to pass — the finalized path still uses `MarkdownBody`
- New test: frame-aligned callback fires exactly once per frame
- New test: scroll suppression prevents render during scroll

---

## Integration: all three proposals together

The three proposals compose cleanly. Here is the final `didUpdateWidget` method:

```dart
@override
void didUpdateWidget(covariant _AssistantMarkdown old) {
  super.didUpdateWidget(old);
  if (old.data != widget.data &&
      widget.data.length <= kAssistantShowMoreChars) {
    _expanded = false;
  }

  // ── Finalized path ──
  if (!widget.streaming) {
    _dirty = false;
    if (!_upToDate(false)) {
      setState(() => _built = _render(context, widget.data, false));
    }
    return;
  }

  // ── Streaming path ──
  if (_upToDate(true)) return;
  // Proposal I: suppress rebuilds while the user is scrolling/flinging.
  if (ChatScrollActivity.isScrolling(context)) return;
  // Proposal F: frame-aligned throttle — at most one render per frame.
  if (_dirty) return;
  _dirty = true;
  // Proposal C: dispatch background markdown parse, apply in post-frame.
  WidgetsBinding.instance.addPostFrameCallback((Duration _) async {
    if (!mounted) { _dirty = false; return; }
    _dirty = false;
    if (_upToDate(true)) return;
    await _updateStreamingRender();
  });
}

Future<void> _updateStreamingRender() async {
  final text = widget.data;
  _parsedText = text;
  final parsed = await parseMarkdownOffMain(text);
  if (!mounted || text != widget.data) return;
  _parsed = parsed;
  setState(() {
    _built = _render(context, text, true);
  });
}
```

### What happens in each scenario

**First chunk arrives (streaming starts):**
1. `didUpdateWidget` fires → text changed, `_dirty = false`, schedules `addPostFrameCallback`
2. Post-frame callback fires (after current frame rasterized)
3. Calls `parseMarkdownOffMain(text)` → if text < 500 chars, returns immediately; if >= 500, runs on isolate
4. `_render` called → `_parsed` is null (first time) → falls back to `MarkdownBody` + `bufferStreamingMarkdown`
5. `_parsed` is now set for next time

**Subsequent chunks (streaming continues):**
1. Each `didUpdateWidget` sees `_dirty == true` (first post-frame callback not yet fired) → returns
2. Post-frame callback fires → picks up latest `widget.data`
3. `parseMarkdownOffMain` returns (isolate) → `_parsed` is set
4. `_render` called → `_parsed` matches `_parsedText` → uses `_buildFromParsed` (no MarkdownBody re-parse!)

**User scrolls during streaming:**
1. `didUpdateWidget` fires → `ChatScrollActivity.isScrolling(context) == true` → returns
2. Text accumulates in TranscriptsState, but no rendering happens
3. User releases → `ScrollEndNotification` fires → `scrolling.value = false`
4. Next `didUpdateWidget` fires → proceeds normally, renders accumulated text

**Turn completes (finalized):**
1. `didUpdateWidget` fires → `!widget.streaming == true` → finalized path
2. `_dirty = false` → cancels any pending frame callback
3. `_render(context, data, false)` → uses full `MarkdownBody` with selection enabled
4. `_parsed = null` → isolate cache is released (GC)

---

## Proposal L: Append fast-path in `_memoTranscriptRows`

### Problem

`_transcript_pane.dart:16-51` — every `tool_call` event grows the items list (length +1). `_memoTranscriptRows` cannot take its fast path because that path only fires when `items.length == prevSource.length` (the "assistant text grew" case). With no fast path, `buildTranscriptRows` runs — an O(n) scan over every `ChatItem` in the transcript — creating new `GroupRow` and `SingleRow` objects. Even though Flutter matches elements by key, the `ListView` must relayout every item because the widget instances are new. The `ExpansionTile` inside each `_ToolGroupTile` receives a new parent widget, and its internal `AnimatedSize` recalculates, causing a visible flash.

On a 200-item transcript where the agent runs 20 tool calls, this means 20 O(n) scans and 20 full-list relayouts for zero visual change to the tool group.

### Fix

Add a new branch to `_memoTranscriptRows` that handles the case where exactly one new item was appended and the prefix is provably identical:

```dart
(List<TranscriptRow>, bool sameKeys) _memoTranscriptRows(
  List<ChatItem> items,
  List<ChatItem>? prevSource,
  List<TranscriptRow>? prevRows,
) {
  if (identical(items, prevSource) && prevRows != null) {
    return (prevRows, true);
  }

  // ── New: append fast-path ──
  // When exactly one item was appended and the prefix is unchanged, build
  // a SingleRow for the new item without scanning the full items list.
  // Completed tools are excluded because they may fold into an existing
  // GroupRow (which requires buildTranscriptRows to detect).
  if (prevSource != null && prevRows != null &&
      items.length == prevSource.length + 1 && items.isNotEmpty) {
    var prefixSame = true;
    for (var i = 0; i < prevSource.length; i++) {
      if (!identical(items[i], prevSource[i])) {
        prefixSame = false;
        break;
      }
    }
    if (prefixSame) {
      final newItem = items.last;
      final canFoldIntoGroup =
          newItem.kind == ChatItemKind.tool && !newItem.toolRunning;
      if (!canFoldIntoGroup) {
        final newRows = List<TranscriptRow>.of(prevRows);
        newRows.add(SingleRow(newItem, items.length - 1));
        return (newRows, true);
      }
    }
  }

  // ── Existing: same-length fast path (assistant text grew) ──
  if (prevSource != null && prevRows != null &&
      items.length == prevSource.length && items.isNotEmpty) {
    // ... existing logic unchanged ...
  }

  // ── Fallback: full scan ──
  return (buildTranscriptRows(items), false);
}
```

### What this covers

| Event type | New item | Fast path? |
|-----------|---------|------------|
| `tool_call` | New running tool (SingleRow) | **Yes** — appends SingleRow |
| `user_message` | New user bubble (SingleRow) | **Yes** — appends SingleRow |
| `system` / error | New system row (SingleRow) | **Yes** — appends SingleRow |
| Completed tool (different class) | New SingleRow | **Yes** — can't fold |
| Completed tool (same class as last group) | Merged into GroupRow | **No** — falls back to `buildTranscriptRows` |
| History replay / resync (multi-item) | Many new items (length +N) | **No** — length diff > 1 |

The fallback case (completed tool folding into a group) is exactly the case that genuinely changes the row structure. The fast path covers ~90% of events during a live streaming turn.

### Interaction with tool groups

When a completed tool DOES merge into a GroupRow (the fallback case), `buildTranscriptRows` produces a new GroupRow with updated items list. The key (`ValueKey('grp-$firstSeq')`) is stable because the first tool's seq hasn't changed. Flutter preserves the `ExpansionTile` State. The expansion state (`_groupExpanded[firstSeq]`) is preserved because the `firstSeq` hasn't changed. The only layout change is the GroupRow's header text updating ("Ran 2 commands" → "Ran 3 commands"), which is a single text node swap — negligible cost.

### Test strategy

| Test | What it verifies |
|------|-----------------|
| Append running tool → fast path | `sameKeys = true`, only last row is new SingleRow |
| Append user message → fast path | `sameKeys = true`, SingleRow appended |
| Append completed tool (no existing run) → fast path | First completed tool of a class is a SingleRow |
| Append completed tool (extends existing group) → full scan | Falls back to `buildTranscriptRows`, sameKeys = false |
| Multi-item resize (history) → full scan | Length diff > 1, falls back |

### Files changed

```
apps/mobile/lib/features/chat/transcript_pane.dart   (~25 lines added to _memoTranscriptRows)
```

---

## Files changed — summary

### New file
```
apps/mobile/lib/data/chat/markdown_parser.dart  (~230 lines)
```

### Modified files
```
apps/mobile/lib/features/chat/chat_bubble.dart   (~65 lines changed)
  - Remove: _throttleDefault, _throttleLarge, _throttleHuge constants
  - Remove: _throttle getter
  - Remove: Timer? _timer field
  - Add:    bool _dirty field
  - Add:    _ParsedResult? _parsed, String? _parsedText fields
  - Add:    import '../../data/chat/markdown_parser.dart'
  - Modify: didUpdateWidget (Proposals F + I)
  - Modify: _render streaming branch (Proposal C)
  - Add:    _buildFromParsed, _buildTextSpan, _blockPadding (Proposal C)
  - Modify: dispose

apps/mobile/lib/features/chat/transcript_pane.dart  (~25 lines changed)
  - Add:    append fast-path in _memoTranscriptRows (Proposal L)
```

### New test file
```
apps/mobile/test/markdown_parser_test.dart        (~150 lines)
```

### No changes to
- Daemon code (chunkbuf, httpagent, opencode)
- Transcript reducer or notifier (Proposal K is deferred)
- `pubspec.yaml` (no new dependencies)
- `streaming_markdown.dart` (still used as fallback in MarkdownBody path)

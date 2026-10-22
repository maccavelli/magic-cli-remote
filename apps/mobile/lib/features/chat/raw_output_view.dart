import 'package:flutter/material.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';

import '../../data/chat/chat_models.dart' show kAssistantShowMoreChars;
import '../../data/chat/raw_output.dart';
import '../../theme/celestial.dart' show monoDetail;
import 'ansi_text.dart';

/// Agent output shown by its kind, the same way whichever agent sent it
/// (MADR 0208 D13): terminal text in monospace with its colours, JSON
/// pretty-printed, a diff with its added and removed lines marked, Markdown as
/// Markdown, and anything else as it came. A renderer that fails falls back to
/// plain monospace: text is never hidden.
class RawOutputView extends StatefulWidget {
  const RawOutputView({
    super.key,
    required this.text,
    this.color,
    this.collapseAt,
  });

  final String text;

  /// The text's colour where no escape sets one; the theme's muted colour
  /// when null.
  final Color? color;

  /// Output longer than this, in characters, collapses behind "Show more".
  /// Null means [kAssistantShowMoreChars].
  final int? collapseAt;

  /// Makes every renderer but plain throw, so a test can see the fallback.
  @visibleForTesting
  static bool debugThrowInRenderer = false;

  @override
  State<RawOutputView> createState() => _RawOutputViewState();
}

class _RawOutputViewState extends State<RawOutputView> {
  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final color = widget.color ?? theme.colorScheme.onSurfaceVariant;
    final base = monoDetail.copyWith(color: color);
    final limit = widget.collapseAt ?? kAssistantShowMoreChars;
    final collapsed = !_expanded && widget.text.length > limit;
    final text = collapsed ? _head(widget.text, limit) : widget.text;

    Widget body;
    try {
      if (RawOutputView.debugThrowInRenderer) {
        throw StateError('debugThrowInRenderer');
      }
      body = _render(theme, classifyRawOutput(widget.text), text, base);
    } catch (_) {
      body = SelectableText(stripAnsi(text), style: base);
    }
    if (widget.text.length <= limit) return body;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        body,
        TextButton(
          onPressed: () => setState(() => _expanded = !_expanded),
          child: Text(_expanded ? 'Show less' : 'Show more'),
        ),
      ],
    );
  }

  Widget _render(
    ThemeData theme,
    RawOutputKind kind,
    String text,
    TextStyle base,
  ) {
    switch (kind) {
      case RawOutputKind.terminal:
        return _scrollable(
          SelectableText.rich(
            ansiSpan(text, base: base, palette: AnsiPalette.of(theme)),
          ),
        );
      case RawOutputKind.json:
        return _scrollable(
          SelectableText(
            _expanded || text == widget.text ? prettyJson(text) : text,
            style: base,
          ),
        );
      case RawOutputKind.diff:
        return _scrollable(SelectableText.rich(_diffSpan(theme, text, base)));
      case RawOutputKind.markdown:
        return MarkdownBody(data: text, selectable: true);
      case RawOutputKind.plain:
        return SelectableText(text, style: base);
    }
  }

  /// Wide terminal and diff lines scroll sideways rather than wrap, so their
  /// columns stay aligned.
  Widget _scrollable(Widget child) =>
      SingleChildScrollView(scrollDirection: Axis.horizontal, child: child);

  TextSpan _diffSpan(ThemeData theme, String text, TextStyle base) {
    final palette = AnsiPalette.of(theme);
    final added = palette.colors[2];
    final removed = theme.colorScheme.error;
    final hunk = palette.colors[4];
    final lines = text.split('\n');
    return TextSpan(
      style: base,
      children: [
        for (var i = 0; i < lines.length; i++)
          TextSpan(
            text: i == lines.length - 1 ? lines[i] : '${lines[i]}\n',
            style: _diffLineStyle(lines[i], base, added, removed, hunk),
          ),
      ],
    );
  }

  TextStyle _diffLineStyle(
    String line,
    TextStyle base,
    Color added,
    Color removed,
    Color hunk,
  ) {
    if (line.startsWith('+++') ||
        line.startsWith('---') ||
        line.startsWith('diff ') ||
        line.startsWith('index ')) {
      return base.copyWith(fontWeight: FontWeight.bold);
    }
    if (line.startsWith('@@')) return base.copyWith(color: hunk);
    if (line.startsWith('+')) {
      return base.copyWith(
        color: added,
        backgroundColor: added.withValues(alpha: 0.08),
      );
    }
    if (line.startsWith('-')) {
      return base.copyWith(
        color: removed,
        backgroundColor: removed.withValues(alpha: 0.08),
      );
    }
    return base;
  }
}

/// The first [limit] characters of [text], cut back to a line break when one
/// is near, and never inside a surrogate pair.
String _head(String text, int limit) {
  var cut = limit;
  if (cut < text.length && (text.codeUnitAt(cut) & 0xFC00) == 0xDC00) cut--;
  final nl = text.lastIndexOf('\n', cut);
  if (nl > limit ~/ 2) cut = nl;
  return '${text.substring(0, cut)}\n…';
}

import 'dart:isolate';

import 'package:markdown/markdown.dart' as md;

/// The output of an off-main-thread markdown parse. Plain data objects
/// so they cross the isolate boundary via [Isolate.run].
class ParsedResult {
  final List<ParsedBlock> blocks;
  const ParsedResult(this.blocks);
}

class ParsedBlock {
  final BlockType type;
  final List<ParsedSpan> spans;
  final int level;
  final String language;

  const ParsedBlock({
    required this.type,
    required this.spans,
    this.level = 0,
    this.language = '',
  });
}

enum BlockType {
  paragraph,
  heading,
  codeBlock,
  blockquote,
  orderedItem,
  unorderedItem,
  horizontalRule,
}

class ParsedSpan {
  final String text;
  final bool bold;
  final bool italic;
  final bool code;
  final String linkUrl;

  const ParsedSpan({
    required this.text,
    this.bold = false,
    this.italic = false,
    this.code = false,
    this.linkUrl = '',
  });
}

/// Parses [text] as GitHub-Flavored Markdown on a background isolate and
/// returns a flat list of styled blocks. Falls back to a synchronous parse
/// for short messages where the isolate transfer overhead dominates.
Future<ParsedResult> parseMarkdownOffMain(String text) async {
  if (text.length < 500) {
    return _parseMarkdown(text);
  }
  return Isolate.run(() => _parseMarkdown(text));
}

// ── Isolate entry point (must be top-level) ────────────────────────────

ParsedResult _parseMarkdown(String text) {
  final doc = md.Document(
    inlineSyntaxes: md.ExtensionSet.gitHubFlavored.inlineSyntaxes,
    blockSyntaxes: md.ExtensionSet.gitHubFlavored.blockSyntaxes,
  ).parse(text);
  return _walkAst(doc);
}

// ── AST walker ─────────────────────────────────────────────────────────

ParsedResult _walkAst(List<md.Node> nodes) {
  final blocks = <ParsedBlock>[];
  for (final node in nodes) {
    if (node is md.Element) {
      _walkElement(node, blocks);
    }
  }
  return ParsedResult(blocks);
}

void _walkElement(md.Element el, List<ParsedBlock> blocks) {
  switch (el.tag) {
    case 'p':
      blocks.add(
        ParsedBlock(
          type: BlockType.paragraph,
          spans: _extractSpans(el.children ?? []),
        ),
      );
    case 'h1':
    case 'h2':
    case 'h3':
    case 'h4':
    case 'h5':
    case 'h6':
      blocks.add(
        ParsedBlock(
          type: BlockType.heading,
          level: int.parse(el.tag.substring(1)),
          spans: _extractSpans(el.children ?? []),
        ),
      );
    case 'pre':
      final code = el.children?.firstOrNull;
      final lang = code is md.Element
          ? (code.attributes['class'] ?? '').replaceFirst('language-', '')
          : '';
      blocks.add(
        ParsedBlock(
          type: BlockType.codeBlock,
          language: lang,
          spans: code is md.Element ? _extractCodeSpans(code) : [],
        ),
      );
    case 'blockquote':
      for (final child in el.children ?? []) {
        if (child is md.Element) _walkBlockquoteChild(child, blocks);
      }
    case 'ul':
      _walkList(el, blocks, depth: 0);
    case 'ol':
      _walkList(el, blocks, depth: 0);
    case 'hr':
      blocks.add(const ParsedBlock(type: BlockType.horizontalRule, spans: []));
    default:
      blocks.add(
        ParsedBlock(
          type: BlockType.paragraph,
          spans: _extractSpans(el.children ?? []),
        ),
      );
  }
}

void _walkList(
  md.Element list,
  List<ParsedBlock> blocks, {
  required int depth,
}) {
  final type = list.tag == 'ol'
      ? BlockType.orderedItem
      : BlockType.unorderedItem;
  for (final child in list.children ?? []) {
    if (child is! md.Element || child.tag != 'li') continue;
    final children = child.children ?? const <md.Node>[];
    final content = children.where(
      (node) => node is! md.Element || (node.tag != 'ul' && node.tag != 'ol'),
    );
    blocks.add(
      ParsedBlock(
        type: type,
        level: depth,
        spans: _extractSpans(content.toList()),
      ),
    );
    for (final nested in children) {
      if (nested is md.Element && (nested.tag == 'ul' || nested.tag == 'ol')) {
        _walkList(nested, blocks, depth: depth + 1);
      }
    }
  }
}

void _walkBlockquoteChild(md.Element el, List<ParsedBlock> blocks) {
  if (el.tag == 'p') {
    blocks.add(
      ParsedBlock(
        type: BlockType.blockquote,
        spans: _extractSpans(el.children ?? []),
      ),
    );
  } else {
    _walkElement(el, blocks);
  }
}

List<ParsedSpan> _extractSpans(List<md.Node> nodes) {
  final out = <ParsedSpan>[];
  for (final node in nodes) {
    if (node is md.Text) {
      out.add(ParsedSpan(text: node.text));
    } else if (node is md.Element) {
      switch (node.tag) {
        case 'strong':
          out.addAll(_styled(_extractSpans(node.children ?? []), bold: true));
        case 'em':
          out.addAll(_styled(_extractSpans(node.children ?? []), italic: true));
        case 'code':
          out.add(
            ParsedSpan(
              text: (node.children?.firstOrNull is md.Text)
                  ? (node.children!.first as md.Text).text
                  : '',
              code: true,
            ),
          );
        case 'a':
          out.addAll(
            _styled(
              _extractSpans(node.children ?? []),
              linkUrl: node.attributes['href'] ?? '',
            ),
          );
        default:
          out.addAll(_extractSpans(node.children ?? []));
      }
    }
  }
  return _mergeAdjacent(out);
}

List<ParsedSpan> _styled(
  List<ParsedSpan> spans, {
  bool bold = false,
  bool italic = false,
  String linkUrl = '',
}) {
  return spans
      .map(
        (s) => ParsedSpan(
          text: s.text,
          bold: s.bold || bold,
          italic: s.italic || italic,
          code: s.code,
          linkUrl: linkUrl.isNotEmpty ? linkUrl : s.linkUrl,
        ),
      )
      .toList();
}

List<ParsedSpan> _extractCodeSpans(md.Element code) {
  final buf = StringBuffer();
  for (final child in code.children ?? []) {
    if (child is md.Text) buf.write(child.text);
  }
  return [ParsedSpan(text: buf.toString(), code: true)];
}

/// Merge adjacent spans with identical styling into one span — reduces
/// widget count for Flutter's layout engine.
List<ParsedSpan> _mergeAdjacent(List<ParsedSpan> spans) {
  if (spans.length < 2) return spans;
  final out = <ParsedSpan>[];
  var cur = spans.first;
  for (var i = 1; i < spans.length; i++) {
    final next = spans[i];
    if (cur.bold == next.bold &&
        cur.italic == next.italic &&
        cur.code == next.code &&
        cur.linkUrl == next.linkUrl) {
      cur = ParsedSpan(
        text: cur.text + next.text,
        bold: cur.bold,
        italic: cur.italic,
        code: cur.code,
        linkUrl: cur.linkUrl,
      );
    } else {
      out.add(cur);
      cur = next;
    }
  }
  out.add(cur);
  return out;
}

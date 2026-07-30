/// While an assistant reply is still streaming, make the trailing, not-yet-
/// closed markdown renderable by appending the missing closers (`**`, `` ` ``,
/// `~~`, a closing fence, or a missing `)` on an open link destination)
/// instead of hiding the tail. Raw syntax markers still never flash
/// (`**bol` renders as bold "bol", not literal asterisks), and — unlike the
/// old cut-at-marker approach — a long code block streams onto the screen
/// line by line rather than materialising all at once when its fence finally
/// closes (the main "frozen, then jumps" artifact on terminal-heavy turns).
///
/// Intended for the streaming phase ONLY — once the turn completes the full,
/// unmodified text must be rendered, so an intentional lone marker (e.g. "use *
/// for multiply") is never permanently restyled.
///
/// Single `*` / `_` italic auto-close is deliberately **not** applied: list
/// markers and multiplication signs are too ambiguous (MADR 0057 D7).
///
/// A single left-to-right scan tracks the currently-open construct with
/// precedence fenced-code > inline-code > link-url > bold/strike (markers
/// inside code or a link destination are not parsed). Whatever is still open
/// at the end gets closed, innermost (latest opened) first.
String bufferStreamingMarkdown(String text) {
  if (text.isEmpty) return text;

  int? openFence; // index of an unclosed ```
  int? openInline; // index of an unclosed `
  int? openBold; // index of an unclosed **
  int? openStrike; // index of an unclosed ~~
  int? openLinkParen; // index of `(` in an unclosed `](…)` destination

  final n = text.length;
  var i = 0;
  while (i < n) {
    // Inside a fenced block: only a closing fence matters.
    if (openFence != null) {
      if (text.startsWith('```', i)) {
        openFence = null;
        i += 3;
      } else {
        i++;
      }
      continue;
    }
    // Inside inline code: only a closing backtick matters.
    if (openInline != null) {
      if (text[i] == '`') openInline = null;
      i++;
      continue;
    }
    // Inside a markdown link destination: only `)` closes it.
    if (openLinkParen != null) {
      if (text[i] == ')') openLinkParen = null;
      i++;
      continue;
    }
    if (text.startsWith('```', i)) {
      openFence = i;
      i += 3;
      continue;
    }
    if (text[i] == '`') {
      openInline = i;
      i++;
      continue;
    }
    if (text.startsWith('](', i)) {
      openLinkParen = i + 1;
      i += 2;
      continue;
    }
    if (text.startsWith('**', i)) {
      openBold = openBold == null ? i : null;
      i += 2;
      continue;
    }
    if (text.startsWith('~~', i)) {
      openStrike = openStrike == null ? i : null;
      i += 2;
      continue;
    }
    i++;
  }

  final closers = <(int, String)>[
    if (openFence != null) (openFence, '\n```'),
    if (openInline != null) (openInline, '`'),
    if (openLinkParen != null) (openLinkParen, ')'),
    if (openBold != null) (openBold, '**'),
    if (openStrike != null) (openStrike, '~~'),
  ];
  if (closers.isEmpty) return text;
  // Latest-opened construct closes first so nesting stays well-formed.
  closers.sort((a, b) => b.$1.compareTo(a.$1));
  final buf = StringBuffer(text);
  for (final c in closers) {
    buf.write(c.$2);
  }
  return buf.toString();
}

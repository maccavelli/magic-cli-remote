/// While an assistant reply is still streaming, hide the trailing, not-yet-
/// closed markdown so raw syntax markers never flash on screen (`**bol` before
/// `**bold**`, a half-open code fence, etc.). Returns the safe-to-render prefix.
///
/// Intended for the streaming phase ONLY — once the turn completes the full,
/// unmodified text must be rendered, so an intentional lone marker (e.g. "use *
/// for multiply") is never permanently hidden.
///
/// A single left-to-right scan tracks the currently-open construct with
/// precedence fenced-code > inline-code > bold (markers inside code are not
/// parsed). If anything is still open at the end, the string is cut at the
/// earliest open marker.
String bufferStreamingMarkdown(String text) {
  if (text.isEmpty) return text;

  int? openFence; // index of an unclosed ```
  int? openInline; // index of an unclosed `
  int? openBold; // index of an unclosed **

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
    if (text.startsWith('**', i)) {
      openBold = openBold == null ? i : null;
      i += 2;
      continue;
    }
    i++;
  }

  final opens = [openFence, openInline, openBold].whereType<int>();
  if (opens.isEmpty) return text;
  final cut = opens.reduce((a, b) => a < b ? a : b);
  return text.substring(0, cut).trimRight();
}

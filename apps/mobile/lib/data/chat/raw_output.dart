import 'dart:convert';

/// How a block of agent output is shown on the phone (MADR 0208 D13). The
/// classifier never asks which provider sent the text: it reads the text.
enum RawOutputKind { diff, json, terminal, markdown, plain }

final _ansiEscape = RegExp(r'\x1b[\[\]]');
final _boxDrawing = RegExp('[─-╿]');
final _diffHunk = RegExp(
  r'^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@',
  multiLine: true,
);
final _diffHeader = RegExp(r'^(?:diff --git |\+\+\+ |--- )', multiLine: true);
final _markdown = RegExp(
  r'^(?:#{1,6} |[-*+] |\d+\. |```|> |\|.*\|\s*$)',
  multiLine: true,
);
final _markdownInline = RegExp(r'\*\*[^*\n]+\*\*|`[^`\n]+`');
final _alignedColumns = RegExp(r'\S {2,}\S');

/// Decides how [text] is shown. Tried in order:
///
///  * `diff`: a unified diff, with a file header and a hunk header;
///  * `json`: the whole text parses as a JSON object or array;
///  * `terminal`, strong signs: an escape sequence or box-drawing characters;
///  * `markdown`: headings, lists, fences, quotes, table rows, or inline code
///    or bold;
///  * `terminal`, weak signs: two or more lines whose columns align on runs
///    of spaces, or most of three or more lines indented;
///  * `plain` otherwise.
///
/// The weak terminal signs come after Markdown's, so a reply that happens to
/// put two spaces between words stays Markdown. The recorded fixtures under
/// test/fixtures/raw_output are the evidence for each rule.
RawOutputKind classifyRawOutput(String text) {
  final t = text.trimRight();
  if (t.isEmpty) return RawOutputKind.plain;
  if (_diffHunk.hasMatch(t) && _diffHeader.hasMatch(t)) {
    return RawOutputKind.diff;
  }
  if (_isJson(t)) return RawOutputKind.json;
  if (_ansiEscape.hasMatch(t) || _boxDrawing.hasMatch(t)) {
    return RawOutputKind.terminal;
  }
  if (_markdown.hasMatch(t) || _markdownInline.hasMatch(t)) {
    return RawOutputKind.markdown;
  }
  final lines = t.split('\n').where((l) => l.trim().isNotEmpty).toList();
  if (lines.where(_alignedColumns.hasMatch).length >= 2) {
    return RawOutputKind.terminal;
  }
  if (lines.length >= 3 &&
      lines.where((l) => l.startsWith(' ') || l.startsWith('\t')).length * 2 >
          lines.length) {
    return RawOutputKind.terminal;
  }
  return RawOutputKind.plain;
}

bool _isJson(String t) {
  final s = t.trimLeft();
  if (!(s.startsWith('{') || s.startsWith('['))) return false;
  try {
    final v = jsonDecode(s);
    return v is Map || v is List;
  } on FormatException {
    return false;
  }
}

/// JSON text re-indented two spaces per level, or [text] unchanged when it is
/// not JSON.
String prettyJson(String text) {
  try {
    return const JsonEncoder.withIndent('  ').convert(jsonDecode(text));
  } on FormatException {
    return text;
  }
}

/// The largest old × new line product [unifiedDiff] compares line by line.
/// Above it the diff shows every old line removed and every new line added,
/// which is correct but coarse, rather than spend seconds on the phone.
const kMaxDiffCells = 4000000;

/// A unified diff of [oldText] against [newText] for the file at [path], with
/// three lines of context, built from a tool's old and new text (MADR 0208
/// D11). An empty [oldText] is a new file.
String unifiedDiff(String path, String oldText, String newText) {
  final a = _lines(oldText);
  final b = _lines(newText);
  final ops = a.length * b.length <= kMaxDiffCells
      ? _lineOps(a, b)
      : [for (final l in a) _Op('-', l), for (final l in b) _Op('+', l)];
  final out = StringBuffer()
    ..writeln(a.isEmpty ? '--- /dev/null' : '--- a$path')
    ..writeln('+++ b$path');
  const context = 3;
  var i = 0;
  while (i < ops.length) {
    if (ops[i].tag == ' ') {
      i++;
      continue;
    }
    // A hunk: the changes from i, joined while gaps stay within 2×context.
    var start = i - context < 0 ? 0 : i - context;
    var end = i;
    while (end < ops.length) {
      if (ops[end].tag != ' ') {
        end++;
        continue;
      }
      var run = end;
      while (run < ops.length && ops[run].tag == ' ') {
        run++;
      }
      if (run >= ops.length || run - end > 2 * context) {
        end = (end + context).clamp(0, ops.length);
        break;
      }
      end = run;
    }
    var oldStart = 0, newStart = 0;
    for (var k = 0; k < start; k++) {
      if (ops[k].tag != '+') oldStart++;
      if (ops[k].tag != '-') newStart++;
    }
    final hunk = ops.sublist(start, end);
    final oldLen = hunk.where((o) => o.tag != '+').length;
    final newLen = hunk.where((o) => o.tag != '-').length;
    out.writeln(
      '@@ -${oldLen == 0 ? oldStart : oldStart + 1},$oldLen '
      '+${newLen == 0 ? newStart : newStart + 1},$newLen @@',
    );
    for (final o in hunk) {
      out.writeln('${o.tag}${o.line}');
    }
    i = end;
  }
  return out.toString().trimRight();
}

class _Op {
  const _Op(this.tag, this.line);
  final String tag;
  final String line;
}

List<String> _lines(String text) {
  if (text.isEmpty) return const [];
  final t = text.endsWith('\n') ? text.substring(0, text.length - 1) : text;
  return t.split('\n');
}

/// The line edits from [a] to [b], by longest common subsequence.
List<_Op> _lineOps(List<String> a, List<String> b) {
  final n = a.length, m = b.length;
  final lcs = List.generate(n + 1, (_) => List<int>.filled(m + 1, 0));
  for (var i = n - 1; i >= 0; i--) {
    for (var j = m - 1; j >= 0; j--) {
      lcs[i][j] = a[i] == b[j]
          ? lcs[i + 1][j + 1] + 1
          : (lcs[i + 1][j] >= lcs[i][j + 1] ? lcs[i + 1][j] : lcs[i][j + 1]);
    }
  }
  final ops = <_Op>[];
  var i = 0, j = 0;
  while (i < n && j < m) {
    if (a[i] == b[j]) {
      ops.add(_Op(' ', a[i]));
      i++;
      j++;
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      ops.add(_Op('-', a[i++]));
    } else {
      ops.add(_Op('+', b[j++]));
    }
  }
  while (i < n) {
    ops.add(_Op('-', a[i++]));
  }
  while (j < m) {
    ops.add(_Op('+', b[j++]));
  }
  return ops;
}

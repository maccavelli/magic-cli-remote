import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/features/chat/ansi_text.dart';

/// A palette whose every slot is distinct, so a test can tell which one a
/// span took.
final _palette = AnsiPalette([
  for (var i = 0; i < 16; i++) Color(0xFF000000 + i),
]);

const _base = TextStyle(fontFamily: 'monospace');

/// The non-empty leaf spans of [span], in order.
List<TextSpan> _leaves(TextSpan span) => [
  if ((span.text ?? '').isNotEmpty) span,
  for (final c in span.children ?? const <InlineSpan>[])
    if (c is TextSpan) ..._leaves(c),
];

void main() {
  test('SGR colour and weight become span styles, and reset ends them', () {
    final spans = _leaves(
      ansiSpan(
        '\x1b[1;31mFAIL\x1b[0m  pkg/beta\n\x1b[92mok\x1b[39m done',
        base: _base,
        palette: _palette,
      ),
    );
    expect(spans.map((s) => s.text).join(), 'FAIL  pkg/beta\nok done');
    final fail = spans.firstWhere((s) => s.text == 'FAIL');
    expect(fail.style?.color, _palette.colors[1]);
    expect(fail.style?.fontWeight, FontWeight.bold);
    final rest = spans.firstWhere((s) => s.text!.startsWith('  pkg'));
    expect(rest.style?.color, isNot(_palette.colors[1]));
    expect(rest.style?.fontWeight, isNot(FontWeight.bold));
    final ok = spans.firstWhere((s) => s.text == 'ok');
    expect(ok.style?.color, _palette.colors[10], reason: '92 is bright green');
  });

  test("grok's real ls colours parse to plain text with styled names", () {
    final text = File(
      'test/fixtures/raw_output/grok/01-tool.txt',
    ).readAsStringSync();
    final spans = _leaves(ansiSpan(text, base: _base, palette: _palette));
    final joined = spans.map((s) => s.text).join();
    expect(joined, isNot(contains('\x1b')));
    expect(joined, contains('table.sh'));
    expect(
      spans.firstWhere((s) => s.text == '.git').style?.color,
      _palette.colors[4],
      reason: 'ls draws directories in blue (34)',
    );
  });

  test('stripAnsi drops CSI and OSC sequences, and keeps the text', () {
    expect(
      stripAnsi('\x1b]0;window title\x07hi\x1b[2K there\x1b[1;31m!\x1b[0m'),
      'hi there!',
    );
  });
}

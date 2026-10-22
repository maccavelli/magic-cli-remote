import 'package:flutter/material.dart';

/// The 16 basic terminal colours, drawn from the theme (MADR 0208 D13).
class AnsiPalette {
  AnsiPalette(this.colors) : assert(colors.length == 16);

  /// Normal black, red, green, yellow, blue, magenta, cyan, white, then the
  /// bright eight in the same order.
  final List<Color> colors;

  /// Colours that read on the theme's surface: red is the theme's error
  /// colour, and the rest are chosen per brightness.
  factory AnsiPalette.of(ThemeData theme) {
    final dark = theme.brightness == Brightness.dark;
    final error = theme.colorScheme.error;
    return AnsiPalette(
      dark
          ? [
              const Color(0xFF6E7681),
              error,
              const Color(0xFF7EE787),
              const Color(0xFFE3B341),
              const Color(0xFF79C0FF),
              const Color(0xFFD2A8FF),
              const Color(0xFF56D4DD),
              const Color(0xFFC9D1D9),
              const Color(0xFF8B949E),
              error,
              const Color(0xFF9EF0A8),
              const Color(0xFFF2CC60),
              const Color(0xFFA5D6FF),
              const Color(0xFFE2C5FF),
              const Color(0xFFB3F0FF),
              const Color(0xFFF0F6FC),
            ]
          : [
              const Color(0xFF24292F),
              error,
              const Color(0xFF116329),
              const Color(0xFF7D4E00),
              const Color(0xFF0550AE),
              const Color(0xFF8250DF),
              const Color(0xFF1B7C83),
              const Color(0xFF6E7781),
              const Color(0xFF57606A),
              error,
              const Color(0xFF1A7F37),
              const Color(0xFF9A6700),
              const Color(0xFF218BFF),
              const Color(0xFFA475F9),
              const Color(0xFF3192AA),
              const Color(0xFF8C959F),
            ],
    );
  }
}

/// One escape sequence: CSI (`ESC [` params, a final byte), OSC (`ESC ]` up
/// to BEL or `ESC \`), or a two-character escape.
final _escape = RegExp(
  r'\x1b(?:\[([0-9;?:]*)[ -/]*([@-~])|\][^\x07\x1b]*(?:\x07|\x1b\\)?|[@-Z\\-_])',
);

/// [text] with every terminal escape sequence removed.
String stripAnsi(String text) => text.replaceAll(_escape, '');

/// [text] as styled spans: SGR weight, italics, underline and colour applied
/// over [base], every other escape sequence dropped.
TextSpan ansiSpan(
  String text, {
  required TextStyle base,
  required AnsiPalette palette,
}) {
  final state = _SgrState();
  final spans = <TextSpan>[];
  var at = 0;
  void emit(String s) {
    if (s.isNotEmpty) spans.add(TextSpan(text: s, style: state.style(base)));
  }

  for (final m in _escape.allMatches(text)) {
    emit(text.substring(at, m.start));
    at = m.end;
    if (m.group(2) == 'm') state.apply(m.group(1) ?? '', palette);
  }
  emit(text.substring(at));
  return TextSpan(style: base, children: spans);
}

class _SgrState {
  bool bold = false, dim = false, italic = false, underline = false;
  Color? fg, bg;

  void reset() {
    bold = dim = italic = underline = false;
    fg = bg = null;
  }

  void apply(String params, AnsiPalette palette) {
    final codes = params.isEmpty
        ? [0]
        : [for (final p in params.split(RegExp('[;:]'))) int.tryParse(p) ?? 0];
    for (var i = 0; i < codes.length; i++) {
      final c = codes[i];
      switch (c) {
        case 0:
          reset();
        case 1:
          bold = true;
        case 2:
          dim = true;
        case 3:
          italic = true;
        case 4:
          underline = true;
        case 22:
          bold = dim = false;
        case 23:
          italic = false;
        case 24:
          underline = false;
        case >= 30 && <= 37:
          fg = palette.colors[c - 30];
        case >= 90 && <= 97:
          fg = palette.colors[c - 90 + 8];
        case 39:
          fg = null;
        case >= 40 && <= 47:
          bg = palette.colors[c - 40];
        case >= 100 && <= 107:
          bg = palette.colors[c - 100 + 8];
        case 49:
          bg = null;
        case 38 || 48:
          final (color, used) = _extended(codes, i + 1, palette);
          if (c == 38) {
            fg = color ?? fg;
          } else {
            bg = color ?? bg;
          }
          i += used;
      }
    }
  }

  TextStyle style(TextStyle base) {
    final color = fg ?? base.color;
    return base.copyWith(
      color: dim && color != null ? color.withValues(alpha: 0.6) : color,
      backgroundColor: bg,
      fontWeight: bold ? FontWeight.bold : null,
      fontStyle: italic ? FontStyle.italic : null,
      decoration: underline ? TextDecoration.underline : null,
    );
  }
}

/// A 256-colour (`5;n`) or true-colour (`2;r;g;b`) argument starting at
/// [at], and how many codes it used.
(Color?, int) _extended(List<int> codes, int at, AnsiPalette palette) {
  if (at >= codes.length) return (null, 0);
  if (codes[at] == 5 && at + 1 < codes.length) {
    final n = codes[at + 1];
    if (n < 16) return (palette.colors[n], 2);
    if (n < 232) {
      const levels = [0, 95, 135, 175, 215, 255];
      final v = n - 16;
      return (
        Color.fromARGB(
          255,
          levels[v ~/ 36],
          levels[(v ~/ 6) % 6],
          levels[v % 6],
        ),
        2,
      );
    }
    if (n < 256) {
      final g = 8 + 10 * (n - 232);
      return (Color.fromARGB(255, g, g, g), 2);
    }
    return (null, 2);
  }
  if (codes[at] == 2 && at + 3 < codes.length) {
    int ch(int v) => v.clamp(0, 255);
    return (
      Color.fromARGB(
        255,
        ch(codes[at + 1]),
        ch(codes[at + 2]),
        ch(codes[at + 3]),
      ),
      4,
    );
  }
  return (null, 1);
}

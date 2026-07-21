import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/streaming_markdown.dart';

void main() {
  group('bufferStreamingMarkdown', () {
    test('passes through fully-closed markdown unchanged', () {
      const s = 'Here is **bold** and `code` and done.';
      expect(bufferStreamingMarkdown(s), s);
    });

    test('hides an unclosed bold marker', () {
      expect(bufferStreamingMarkdown('start **bol'), 'start');
      expect(bufferStreamingMarkdown('a **b** then **c'), 'a **b** then');
    });

    test('hides an unclosed inline code span', () {
      expect(bufferStreamingMarkdown('run `npm ru'), 'run');
      expect(bufferStreamingMarkdown('`a` and `b'), '`a` and');
    });

    test('hides an unclosed fenced code block entirely', () {
      expect(
        bufferStreamingMarkdown('intro\n```dart\nvoid main() {'),
        'intro',
      );
    });

    test('keeps a closed fenced code block', () {
      const s = 'x\n```\ncode\n```\ny';
      expect(bufferStreamingMarkdown(s), s);
    });

    test('does not parse markers inside a closed code fence', () {
      // The ** inside code is literal and balanced-agnostic; nothing is hidden.
      const s = '```\na ** b\n```';
      expect(bufferStreamingMarkdown(s), s);
    });

    test('empty and marker-free strings are returned as-is', () {
      expect(bufferStreamingMarkdown(''), '');
      expect(bufferStreamingMarkdown('plain text'), 'plain text');
    });

    test('cuts at the earliest of several open markers', () {
      // Bold opens first and never closes, so the tail is hidden from there.
      expect(bufferStreamingMarkdown('a **b `c'), 'a');
    });
  });
}

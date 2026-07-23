import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/streaming_markdown.dart';

void main() {
  group('bufferStreamingMarkdown', () {
    test('passes through fully-closed markdown unchanged', () {
      const s = 'Here is **bold** and `code` and done.';
      expect(bufferStreamingMarkdown(s), s);
    });

    test('auto-closes an unclosed bold marker', () {
      expect(bufferStreamingMarkdown('start **bol'), 'start **bol**');
      expect(bufferStreamingMarkdown('a **b** then **c'), 'a **b** then **c**');
    });

    test('auto-closes an unclosed inline code span', () {
      expect(bufferStreamingMarkdown('run `npm ru'), 'run `npm ru`');
      expect(bufferStreamingMarkdown('`a` and `b'), '`a` and `b`');
    });

    test('auto-closes an unclosed fenced code block so it streams', () {
      expect(
        bufferStreamingMarkdown('intro\n```dart\nvoid main() {'),
        'intro\n```dart\nvoid main() {\n```',
      );
    });

    test('keeps a closed fenced code block', () {
      const s = 'x\n```\ncode\n```\ny';
      expect(bufferStreamingMarkdown(s), s);
    });

    test('does not parse markers inside a closed code fence', () {
      // The ** inside code is literal and balanced-agnostic; nothing changes.
      const s = '```\na ** b\n```';
      expect(bufferStreamingMarkdown(s), s);
    });

    test('empty and marker-free strings are returned as-is', () {
      expect(bufferStreamingMarkdown(''), '');
      expect(bufferStreamingMarkdown('plain text'), 'plain text');
    });

    test('closes nested open markers innermost-first', () {
      // Bold opens first, inline code second: the code span closes before
      // the bold so nesting stays well-formed.
      expect(bufferStreamingMarkdown('a **b `c'), 'a **b `c`**');
    });

    test('closes a fence opened inside an open bold span', () {
      expect(
        bufferStreamingMarkdown('**note\n```\nx'),
        '**note\n```\nx\n```**',
      );
    });
  });
}

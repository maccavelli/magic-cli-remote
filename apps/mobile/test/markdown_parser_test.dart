import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/markdown_parser.dart';

void main() {
  group('parseMarkdownOffMain', () {
    test('retains nested list order, type, and depth', () async {
      final parsed = await parseMarkdownOffMain('''
1. outer
   - nested bullet
     1. nested number
2. second outer
''');

      expect(parsed.blocks.map((block) => (block.type, block.level)), [
        (BlockType.orderedItem, 0),
        (BlockType.unorderedItem, 1),
        (BlockType.orderedItem, 2),
        (BlockType.orderedItem, 0),
      ]);
      expect(
        parsed.blocks
            .map((block) => block.spans.map((span) => span.text).join())
            .toList(),
        ['outer', 'nested bullet', 'nested number', 'second outer'],
      );
    });
  });
}

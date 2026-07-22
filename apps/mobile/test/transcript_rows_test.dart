import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/data/chat/chat_models.dart';
import 'package:magic_cli_remote/data/chat/transcript_rows.dart';

ChatItem _tool(
  int seq, {
  String kind = 'execute',
  String status = 'completed',
  String name = 'Tool',
}) => ChatItem.tool(
  id: 't$seq',
  name: name,
  status: status,
  toolKind: kind,
  seq: seq,
);

void main() {
  group('classifyTool', () {
    test('maps ACP kinds onto the three classes', () {
      expect(classifyTool('execute', null), ToolClass.command);
      expect(classifyTool('edit', null), ToolClass.fileEdit);
      expect(classifyTool('delete', null), ToolClass.fileEdit);
      expect(classifyTool('move', null), ToolClass.fileEdit);
      expect(classifyTool('read', null), ToolClass.other);
      expect(classifyTool('search', null), ToolClass.other);
      expect(classifyTool('fetch', null), ToolClass.other);
    });

    test('falls back to name heuristics when kind is missing', () {
      expect(classifyTool(null, 'Bash ls -la'), ToolClass.command);
      expect(classifyTool('', 'shell'), ToolClass.command);
      expect(classifyTool(null, 'Edit main.go'), ToolClass.fileEdit);
      expect(classifyTool(null, 'Write config.yaml'), ToolClass.fileEdit);
      expect(classifyTool(null, 'Fetch docs'), ToolClass.other);
      expect(classifyTool(null, null), ToolClass.other);
    });
  });

  group('buildTranscriptRows', () {
    test('two consecutive finished commands collapse into one group', () {
      final rows = buildTranscriptRows([_tool(0), _tool(1)]);
      expect(rows, hasLength(1));
      final g = rows.single as GroupRow;
      expect(g.toolClass, ToolClass.command);
      expect(g.items, hasLength(2));
      expect(g.title, 'Ran 2 commands');
    });

    test('a lone finished tool stays a single row', () {
      final rows = buildTranscriptRows([_tool(0)]);
      expect(rows.single, isA<SingleRow>());
    });

    test('a class change breaks the run into separate groups', () {
      final rows = buildTranscriptRows([
        _tool(0, kind: 'execute'),
        _tool(1, kind: 'execute'),
        _tool(2, kind: 'edit'),
        _tool(3, kind: 'edit'),
        _tool(4, kind: 'edit'),
      ]);
      expect(rows, hasLength(2));
      expect((rows[0] as GroupRow).title, 'Ran 2 commands');
      expect((rows[1] as GroupRow).title, 'Edited 3 files');
    });

    test('a running tool never folds into a group', () {
      final rows = buildTranscriptRows([
        _tool(0),
        _tool(1),
        _tool(2, status: 'running'),
      ]);
      expect(rows, hasLength(2));
      expect((rows[0] as GroupRow).items, hasLength(2));
      final live = rows[1] as SingleRow;
      expect(live.item.toolRunning, isTrue);
      expect(live.index, 2);
    });

    test('runs split by the live tool merge once it completes', () {
      final before = buildTranscriptRows([
        _tool(0),
        _tool(1, status: 'running'),
        _tool(2),
      ]);
      expect(before, hasLength(3));

      final after = buildTranscriptRows([_tool(0), _tool(1), _tool(2)]);
      final g = after.single as GroupRow;
      expect(g.title, 'Ran 3 commands');
      // Group identity (first seq) is stable across the merge.
      expect(g.items.first.seq, 0);
    });

    test('non-tool items break the run and keep source indices', () {
      final rows = buildTranscriptRows([
        ChatItem.assistant('hi').copyWith(seq: 0),
        _tool(1),
        _tool(2),
        ChatItem.assistant('done').copyWith(seq: 3),
      ]);
      expect(rows, hasLength(3));
      expect((rows[0] as SingleRow).index, 0);
      expect(rows[1], isA<GroupRow>());
      expect((rows[2] as SingleRow).index, 3);
    });

    test(
      'failed actions are counted so the collapsed row can surface them',
      () {
        final rows = buildTranscriptRows([
          _tool(0),
          _tool(1, status: 'failed'),
          _tool(2, status: 'failed'),
        ]);
        expect((rows.single as GroupRow).failedCount, 2);
      },
    );

    test('generic tool group titles use "Used N tools"', () {
      final rows = buildTranscriptRows([
        _tool(0, kind: 'read'),
        _tool(1, kind: 'search'),
      ]);
      expect((rows.single as GroupRow).title, 'Used 2 tools');
    });
  });
}

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
    test('two consecutive commands collapse into one group', () {
      final rows = buildTranscriptRows([_tool(0), _tool(1)]);
      expect(rows, hasLength(1));
      final g = rows.single as GroupRow;
      expect(g.leadClass, ToolClass.command);
      expect(g.items, hasLength(2));
      expect(g.title, 'Ran 2 commands');
    });

    // Reversed by MADR 0042 D1. This used to assert a lone tool stays a
    // SingleRow; the threshold dropped from 2 to 1 so that a run's key is
    // `grp-<first seq>` from its very first tool and is never rewritten when a
    // second one arrives. That re-key was a real element remount.
    test('a lone tool is a group of one, titled with the tool itself', () {
      final rows = buildTranscriptRows([_tool(0, name: 'bash')]);
      final g = rows.single as GroupRow;
      expect(g.items, hasLength(1));
      expect(g.title, 'bash', reason: 'its own name says more than a summary');
    });

    // Reversed by MADR 0042 D1. This used to assert a class change splits the
    // run. It was the reason the fold never engaged for OpenCode, which
    // alternates read/bash/read/edit constantly, leaving every run at length 1.
    test('a class change no longer breaks the run', () {
      final rows = buildTranscriptRows([
        _tool(0, kind: 'read'),
        _tool(1, kind: 'search'),
        _tool(2, kind: 'edit'),
      ]);
      expect(rows, hasLength(1));
      expect((rows.single as GroupRow).title, 'Used 3 tools');
    });

    // Reversed by MADR 0042 D1. A live tool used to break the run and render
    // standalone, then collapse into the group on completion — the visible
    // "expanded then closed and redrawn" artefact.
    test('a running tool joins its group and is exposed as the live head', () {
      final rows = buildTranscriptRows([
        _tool(0),
        _tool(1),
        _tool(2, status: 'running', name: 'npm test'),
      ]);
      expect(rows, hasLength(1));
      final g = rows.single as GroupRow;
      expect(g.items, hasLength(3), reason: 'the live tool is counted');
      expect(g.runningItem?.toolName, 'npm test');
    });

    test('runningItem is null once every member has finished', () {
      final rows = buildTranscriptRows([_tool(0), _tool(1)]);
      expect((rows.single as GroupRow).runningItem, isNull);
    });

    test('the row set is stable across a realistic OpenCode burst', () {
      // The sequence audit 0041 §9.2 measured. Under the old rule the row count
      // ran 1→2→1→2→3→4→5 and re-keyed on every fold; it must now hold at one
      // group whose key never moves.
      const script = [
        ('read', 'read'),
        ('grep', 'search'),
        ('bash', 'execute'),
        ('read', 'read'),
        ('edit', 'edit'),
        ('bash', 'execute'),
      ];
      final items = <ChatItem>[];
      final counts = <int>[];
      final firstSeqs = <int>{};

      for (var i = 0; i < script.length; i++) {
        final (name, kind) = script[i];
        for (final status in ['pending', 'running', 'completed']) {
          if (status == 'pending') {
            items.add(_tool(i, kind: kind, status: status, name: name));
          } else {
            items[i] = _tool(i, kind: kind, status: status, name: name);
          }
          final rows = buildTranscriptRows(items);
          counts.add(rows.length);
          firstSeqs.add((rows.single as GroupRow).items.first.seq);
        }
      }

      expect(counts.toSet(), {
        1,
      }, reason: 'one group row throughout — no oscillation');
      expect(
        firstSeqs,
        {0},
        reason: 'the run keeps one identity, so the row is never re-keyed',
      );
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

    test('a homogeneous edit run still reads "Edited N files"', () {
      // ACP kinds, as the daemon emits them — `kindForTool` already folds
      // OpenCode's write/patch/multiedit onto `edit`.
      final rows = buildTranscriptRows([
        _tool(0, kind: 'edit'),
        _tool(1, kind: 'move'),
        _tool(2, kind: 'delete'),
      ]);
      expect((rows.single as GroupRow).title, 'Edited 3 files');
    });

    test('a mixed run leads with the commands it contains', () {
      final rows = buildTranscriptRows([
        _tool(0, kind: 'execute'),
        _tool(1, kind: 'read'),
        _tool(2, kind: 'execute'),
        _tool(3, kind: 'edit'),
      ]);
      final g = rows.single as GroupRow;
      expect(g.title, 'Ran 2 commands +2 more');
      expect(
        g.leadClass,
        ToolClass.command,
        reason: 'the icon must agree with the label',
      );
    });

    test('a mixed run with one command still reads naturally', () {
      final rows = buildTranscriptRows([
        _tool(0, kind: 'execute'),
        _tool(1, kind: 'read'),
      ]);
      expect((rows.single as GroupRow).title, 'Ran 1 command +1 more');
    });
  });
}

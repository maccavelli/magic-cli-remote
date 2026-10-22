import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// The whole of the cut tool output the fake daemon returns.
const _wholeText = 'line 1\nline 2\nTHE-WHOLE-OUTPUT-TAIL';

/// Chat only calls sessionHistory() at open, and only for an empty
/// transcript; toolOutput() answers "Show all" (PLAN 0208 P2).
class _ToolOutputClient extends McremoteClient {
  final List<String> fetched = [];

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<ToolOutput> toolOutput(String sessionId, String toolCallId) async {
    fetched.add('$sessionId/$toolCallId');
    return const ToolOutput(text: _wholeText);
  }
}

Widget _host(SessionTranscript transcript, McremoteClient client) =>
    ProviderScope(
      overrides: [
        mcremoteClientProvider.overrideWithValue(client),
        connectionStateProvider.overrideWith(
          (ref) => Stream.value(McConnectionState.connected),
        ),
        sessionTranscriptProvider(
          transcript.sessionId,
        ).overrideWithValue(transcript),
      ],
      child: MaterialApp(home: ChatScreen(sessionId: transcript.sessionId)),
    );

SessionTranscript _seeded(List<ChatItem> items) => SessionTranscript(
  sessionId: 's1',
  status: 'idle',
  items: [for (var i = 0; i < items.length; i++) items[i].copyWith(seq: i)],
  nextSeq: items.length,
);

void main() {
  const diff = ToolDiff(path: '/a.go', oldText: 'old\n', newText: 'new\n');

  test('a tool card carries the cut flag and diffs, and a status-only '
      'update keeps them', () {
    var t = applySessionEvent(
      const SessionTranscript(sessionId: 's1'),
      SessionEvent(
        type: 'tool_call_update',
        sessionId: 's1',
        toolId: 't1',
        toolName: 'Run',
        status: 'running',
        text: '    indented first line\n  second',
        toolTruncated: true,
        diffs: [diff],
      ),
    );
    var item = t.items.single;
    expect(item.toolTruncated, isTrue);
    expect(item.diffs, [diff]);
    expect(
      item.text,
      '    indented first line\n  second',
      reason: "the first line's indentation is the agent's, not ours to trim",
    );

    t = applySessionEvent(
      t,
      SessionEvent(
        type: 'tool_call_update',
        sessionId: 's1',
        toolId: 't1',
        status: 'completed',
      ),
    );
    item = t.items.single;
    expect(item.toolStatus, 'completed');
    expect(item.toolTruncated, isTrue);
    expect(item.diffs, [diff]);
  });

  test('the transcript cache keeps the cut flag and diffs', () {
    final item = ChatItem.tool(
      id: 't1',
      name: 'Run',
      detail: 'x',
      truncated: true,
      diffs: const [diff],
    );
    final back = ChatItem.fromJson(item.toJson());
    expect(back.toolTruncated, isTrue);
    expect(back.diffs, [diff]);
  });

  testWidgets('a cut tool card offers "Show all" and shows the whole output', (
    tester,
  ) async {
    final client = _ToolOutputClient();
    await tester.pumpWidget(
      _host(
        _seeded([
          ChatItem.tool(
            id: 't1',
            name: 'Run tests',
            status: 'completed',
            detail: 'line 1\n…[truncated]',
            truncated: true,
          ),
        ]),
        client,
      ),
    );
    await tester.pump();
    await tester.tap(find.text('Run tests'));
    await tester.pumpAndSettle();

    expect(find.text('Show all'), findsOneWidget);
    await tester.tap(find.text('Show all'));
    await tester.pumpAndSettle();

    expect(client.fetched, ['s1/t1']);
    expect(find.textContaining('THE-WHOLE-OUTPUT-TAIL'), findsOneWidget);
  });

  testWidgets('a whole tool card offers no "Show all"', (tester) async {
    final client = _ToolOutputClient();
    await tester.pumpWidget(
      _host(
        _seeded([
          ChatItem.tool(
            id: 't2',
            name: 'List files',
            status: 'completed',
            detail: 'a.go\nb.go',
          ),
        ]),
        client,
      ),
    );
    await tester.pump();
    await tester.tap(find.text('List files'));
    await tester.pumpAndSettle();

    expect(find.textContaining('b.go'), findsOneWidget);
    expect(find.text('Show all'), findsNothing);
    expect(client.fetched, isEmpty);
  });
}

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

// PLAN 0190 K2: an ACP agent's compaction becomes one transcript row per
// compaction ID, updated in place, with its status and a collapsed summary
// (MADR 0190 D3).

class _CompactionClient extends McremoteClient {
  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [SessionMeta(id: 's1', provider: 'grok')],
        complete: true,
      );
}

SessionEvent _compaction(
  String id,
  String status, {
  String? summary,
  String? error,
  bool truncated = false,
}) => SessionEvent.fromJson({
  'type': 'compaction',
  'session_id': 's1',
  'compaction_id': id,
  'compaction_status': status,
  'compaction_summary': ?summary,
  'compaction_error': ?error,
  if (truncated) 'compaction_truncated': true,
});

SessionTranscript _apply(List<SessionEvent> events) {
  var t = const SessionTranscript(sessionId: 's1');
  for (final ev in events) {
    t = applySessionEvent(t, ev);
  }
  return t;
}

List<ChatItem> _compactions(SessionTranscript t) =>
    t.items.where((i) => i.kind == ChatItemKind.compaction).toList();

void main() {
  group('reducer', () {
    test(
      'one compaction is one row, updated in place at its first position',
      () {
        final t = _apply([
          _compaction('c1', 'in_progress'),
          SessionEvent(
            type: 'assistant_message_chunk',
            sessionId: 's1',
            text: 'later',
          ),
          _compaction('c1', 'completed', summary: 'Kept the parser fix.'),
        ]);
        expect(t.items.first.kind, ChatItemKind.compaction);
        final rows = _compactions(t);
        expect(rows, hasLength(1));
        expect(rows.single.toolId, 'c1');
        expect(rows.single.toolStatus, 'completed');
        expect(rows.single.text, 'Kept the parser fix.');
      },
    );

    test('two compactions are two rows', () {
      final t = _apply([
        _compaction('c1', 'completed'),
        _compaction('c2', 'in_progress'),
      ]);
      expect(_compactions(t).map((i) => i.toolId), ['c1', 'c2']);
    });

    test('an identical state is a no-op', () {
      final ev = _compaction('c1', 'completed', summary: 's');
      final t1 = _apply([ev]);
      expect(identical(applySessionEvent(t1, ev), t1), isTrue);
    });

    test('an event without an id is ignored', () {
      final t = _apply([
        SessionEvent.fromJson({
          'type': 'compaction',
          'session_id': 's1',
          'compaction_status': 'completed',
        }),
      ]);
      expect(t.items, isEmpty);
    });

    test('replaying the history gives the same row as watching live', () {
      final events = [
        _compaction('c1', 'in_progress', summary: 'Kept '),
        _compaction('c1', 'failed', summary: 'Kept ', error: 'store down'),
        _compaction(
          'c1',
          'completed',
          summary: 'Kept the fix.',
          truncated: true,
        ),
      ];
      final live = _compactions(_apply(events)).single;
      // A reconnecting phone replays the same events over what it already
      // had: the result must not grow a second row.
      final replayed = _compactions(_apply([...events, ...events])).single;
      expect(replayed.toolStatus, live.toolStatus);
      expect(replayed.text, live.text);
      expect(replayed.compactionError, live.compactionError);
      expect(replayed.compactionTruncated, live.compactionTruncated);
    });

    test('the row survives the transcript cache', () {
      final row = _compactions(
        _apply([
          _compaction(
            'c1',
            'failed',
            summary: 'part',
            error: 'boom',
            truncated: true,
          ),
        ]),
      ).single;
      final back = ChatItem.fromJson(row.toJson());
      expect(back.kind, ChatItemKind.compaction);
      expect(back.toolId, 'c1');
      expect(back.toolStatus, 'failed');
      expect(back.text, 'part');
      expect(back.compactionError, 'boom');
      expect(back.compactionTruncated, isTrue);
    });
  });

  group('row', () {
    Future<void> pump(WidgetTester tester, List<SessionEvent> events) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            mcremoteClientProvider.overrideWithValue(_CompactionClient()),
            connectionStateProvider.overrideWith(
              (ref) => Stream.value(McConnectionState.connected),
            ),
            sessionTranscriptProvider('s1').overrideWithValue(_apply(events)),
          ],
          child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
        ),
      );
      await tester.pump();
    }

    for (final (status, line) in [
      ('in_progress', 'Compacting the conversation…'),
      ('completed', 'Conversation compacted'),
      ('failed', 'Compaction failed'),
      ('cancelled', 'Compaction cancelled'),
      (
        'paused',
        'Compacting the conversation…',
      ), // unknown reads as in progress
    ]) {
      testWidgets('status $status reads "$line"', (tester) async {
        await pump(tester, [_compaction('c1', status)]);
        expect(find.text(line), findsOneWidget);
      });
    }

    testWidgets('the summary is collapsed until opened', (tester) async {
      await pump(tester, [
        _compaction('c1', 'completed', summary: 'Kept the parser fix.'),
      ]);
      expect(find.textContaining('Kept the parser fix.'), findsNothing);
      await tester.tap(find.text('Conversation compacted'));
      await tester.pumpAndSettle();
      expect(find.textContaining('What the agent kept'), findsOneWidget);
      expect(find.textContaining('Kept the parser fix.'), findsOneWidget);
    });

    testWidgets('a truncated summary says so', (tester) async {
      await pump(tester, [
        _compaction('c1', 'completed', summary: 'Kept a lot.', truncated: true),
      ]);
      await tester.tap(find.text('Conversation compacted'));
      await tester.pumpAndSettle();
      expect(find.textContaining('cut short'), findsOneWidget);
    });

    testWidgets('the error shows only while the status is failed', (
      tester,
    ) async {
      await pump(tester, [
        _compaction('c1', 'failed', error: 'context store unavailable'),
      ]);
      await tester.tap(find.text('Compaction failed'));
      await tester.pumpAndSettle();
      expect(find.textContaining('context store unavailable'), findsOneWidget);

      await pump(tester, [
        _compaction('c1', 'failed', error: 'context store unavailable'),
        // The agent cleared the error with null, which the daemon cannot see,
        // so the stale error is still on the event.
        _compaction('c1', 'completed', error: 'context store unavailable'),
      ]);
      expect(find.text('Conversation compacted'), findsOneWidget);
      expect(find.textContaining('context store unavailable'), findsNothing);
    });
  });
}

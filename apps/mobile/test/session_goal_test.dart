import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

class _GoalClient extends McremoteClient {
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
        sessions: [SessionMeta(id: 's1', provider: 'codex')],
        complete: true,
      );
}

void main() {
  test('session_goal event replaces and clears transcript goal', () {
    var t = const SessionTranscript(sessionId: 's1');
    t = applySessionEvent(
      t,
      SessionEvent(
        type: 'session_goal',
        sessionId: 's1',
        goal: const SessionGoal(
          objective: 'Ship it',
          status: 'active',
          tokenBudget: 10,
          tokenUsage: 2,
        ),
      ),
    );
    expect(t.goal?.objective, 'Ship it');
    expect(t.goal?.status, 'active');
    t = applySessionEvent(
      t,
      SessionEvent(type: 'session_goal', sessionId: 's1'),
    );
    expect(t.goal, isNull);
  });

  test('old-daemon events without goal leave state empty', () {
    const t = SessionTranscript(sessionId: 's1');
    final next = applySessionEvent(
      t,
      SessionEvent(type: 'session_status', sessionId: 's1', status: 'idle'),
    );
    expect(next.goal, isNull);
  });

  testWidgets('goal card renders truncated objective and usage', (
    tester,
  ) async {
    final transcript = SessionTranscript(
      sessionId: 's1',
      items: [ChatItem.assistant('hi')],
      goal: SessionGoal(
        objective: 'A' * 200,
        status: 'paused',
        tokenBudget: 20,
        tokenUsage: 5,
      ),
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mcremoteClientProvider.overrideWithValue(_GoalClient()),
          connectionStateProvider.overrideWith(
            (ref) => Stream.value(McConnectionState.connected),
          ),
          sessionTranscriptProvider('s1').overrideWithValue(transcript),
        ],
        child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
      ),
    );
    await tester.pump();
    expect(find.textContaining('Goal · paused'), findsOneWidget);
    expect(find.textContaining('5/20'), findsOneWidget);
    expect(find.textContaining('…'), findsWidgets);
  });
}

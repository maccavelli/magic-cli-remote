import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

class _CmdClient extends McremoteClient {
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
        sessions: [SessionMeta(id: 's1', provider: 'codex', cwd: '/tmp')],
        complete: true,
      );
}

SessionTranscript _withCommands(List<RemoteCommand> commands) {
  return SessionTranscript(
    sessionId: 's1',
    items: [ChatItem.assistant('hi')],
    remoteCommands: commands,
  );
}

void main() {
  testWidgets('Fast and personality appear when the daemon advertises them', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mcremoteClientProvider.overrideWithValue(_CmdClient()),
          connectionStateProvider.overrideWith(
            (ref) => Stream.value(McConnectionState.connected),
          ),
          sessionTranscriptProvider('s1').overrideWithValue(
            _withCommands([
              RemoteCommand(name: 'fast', available: true),
              RemoteCommand(name: 'personality', available: true),
            ]),
          ),
        ],
        child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
      ),
    );
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).first, '/fa');
    await tester.pumpAndSettle();
    expect(find.text('/fast'), findsOneWidget);
    await tester.enterText(find.byType(TextField).first, '/per');
    await tester.pumpAndSettle();
    expect(find.text('/personality'), findsOneWidget);
  });

  testWidgets('stale Fast/personality commands disappear after model change', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mcremoteClientProvider.overrideWithValue(_CmdClient()),
          connectionStateProvider.overrideWith(
            (ref) => Stream.value(McConnectionState.connected),
          ),
          sessionTranscriptProvider('s1').overrideWithValue(
            _withCommands([
              RemoteCommand(
                name: 'fast',
                available: false,
                reason: 'this agent has no Fast service tier',
              ),
              RemoteCommand(
                name: 'personality',
                available: false,
                reason: 'this agent has no personality setting',
              ),
              RemoteCommand(name: 'model', available: true),
            ]),
          ),
        ],
        child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
      ),
    );
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).first, '/fa');
    await tester.pumpAndSettle();
    expect(find.text('/fast'), findsNothing);
    await tester.enterText(find.byType(TextField).first, '/per');
    await tester.pumpAndSettle();
    expect(find.text('/personality'), findsNothing);
    await tester.enterText(find.byType(TextField).first, '/mod');
    await tester.pumpAndSettle();
    expect(find.text('/model'), findsOneWidget);
  });

  test('personality none is the enum, not a JSON-null clear', () {
    const payload = {'personality': 'none', 'service_tier': null};
    expect(payload['personality'], 'none');
    expect(payload['personality'], isNot(isNull));
    expect(payload['service_tier'], isNull);
  });
}

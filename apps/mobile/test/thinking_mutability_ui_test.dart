import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// MADR 0123 D9/F5. The thinking control used to be gated on
/// `provider == 'grok'`. Grok gained mid-session changes in 1.0.5 (MADR 0106),
/// so the app spent two releases refusing a control that worked and telling
/// the user to start a new session — a statement that was false.
///
/// These tests pin the replacement: the daemon reports mutability per session,
/// and nothing about the provider's *name* may decide this again (0123 C1).
class _ThinkingClient extends McremoteClient {
  _ThinkingClient({required this.provider, required this.mutability});

  final String provider;
  final ThinkingMutability mutability;
  final List<String> levelSwitches = [];

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [
          SessionMeta(
            id: 's1',
            provider: provider,
            cwd: '/home/mac',
            thinkingLevel: 'medium',
            thinkingMutability: mutability,
          ),
        ],
        complete: true,
      );

  @override
  Future<void> setThinkingLevel(String sessionId, String level) async {
    levelSwitches.add(level);
  }
}

/// The daemon advertises /thinking whenever the session implements
/// ThinkingSession — availability, not mutability (session/commands.go:67).
final _thinkingAvailable = [
  RemoteCommand(name: 'thinking', description: 'thinking', available: true),
];

Widget _host(_ThinkingClient client) {
  final transcript = SessionTranscript(
    sessionId: 's1',
    status: 'idle',
    items: const [],
    remoteCommands: _thinkingAvailable,
  );
  return ProviderScope(
    overrides: [
      mcremoteClientProvider.overrideWithValue(client),
      connectionStateProvider.overrideWith(
        (ref) => Stream.value(McConnectionState.connected),
      ),
      sessionTranscriptProvider('s1').overrideWithValue(transcript),
    ],
    child: MaterialApp(home: ChatScreen(sessionId: 's1')),
  );
}

void main() {
  testWidgets('grok is no longer refused: the level ladder opens', (
    tester,
  ) async {
    final client = _ThinkingClient(
      provider: 'grok',
      mutability: ThinkingMutability.live,
    );
    await tester.pumpWidget(_host(client));
    await tester.pumpAndSettle();

    await tester.tap(find.byIcon(Icons.psychology_outlined));
    await tester.pumpAndSettle();

    expect(
      find.text('Thinking level'),
      findsOneWidget,
      reason: 'grok reports live mutability, so the ladder must open',
    );
    // The exact falsehood the old hardcoded branch printed.
    expect(find.textContaining('start a new session'), findsNothing);
    expect(find.textContaining('New sessions only'), findsNothing);
  });

  testWidgets('an unknown capability is treated as settable, not locked', (
    tester,
  ) async {
    // An older daemon omits the field. Assuming "fixed" here would recreate
    // the very defect this record removed (MADR 0123 C2).
    final client = _ThinkingClient(
      provider: 'grok',
      mutability: ThinkingMutability.unknown,
    );
    await tester.pumpWidget(_host(client));
    await tester.pumpAndSettle();

    await tester.tap(find.byIcon(Icons.psychology_outlined));
    await tester.pumpAndSettle();

    expect(find.text('Thinking level'), findsOneWidget);
    expect(find.byIcon(Icons.lock_outline), findsNothing);
  });

  testWidgets('a genuinely fixed session is locked, whatever the provider', (
    tester,
  ) async {
    // Same provider name as the first case. Only the daemon's answer differs,
    // which is the entire point: the name decides nothing (0123 C1).
    final client = _ThinkingClient(
      provider: 'grok',
      mutability: ThinkingMutability.fixed,
    );
    await tester.pumpWidget(_host(client));
    await tester.pumpAndSettle();

    expect(
      find.byIcon(Icons.lock_outline),
      findsOneWidget,
      reason: 'fixed must read as locked before the user taps',
    );

    await tester.tap(find.byIcon(Icons.psychology_outlined));
    await tester.pumpAndSettle();

    expect(find.text('Thinking level'), findsNothing);
    expect(client.levelSwitches, isEmpty);
  });
}

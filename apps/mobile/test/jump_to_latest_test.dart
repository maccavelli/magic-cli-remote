import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// "Jump to latest" is the only way back to the live end once the user has
/// scrolled away, and tapping it also clears the unread count. If the jump is
/// skipped but the count is cleared anyway, the button disappears without
/// moving the list and the unread state is gone (MADR 0046 M-9).

class _QuietClient extends McremoteClient {
  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<List<SessionMeta>> listSessions() async => [
    SessionMeta(id: 's1', provider: 'codex', cwd: '/home/mac'),
  ];
}

SessionTranscript longTranscript() => SessionTranscript(
  sessionId: 's1',
  status: 'idle',
  nextSeq: 200,
  items: [
    for (var i = 0; i < 120; i++)
      ChatItem.user('message number $i').copyWith(seq: i + 1),
  ],
);

void main() {
  testWidgets('the jump lands at the live end even during a fling', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(600, 1200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          mcremoteClientProvider.overrideWithValue(_QuietClient()),
          connectionStateProvider.overrideWith(
            (ref) => Stream.value(McConnectionState.connected),
          ),
          sessionTranscriptProvider('s1').overrideWithValue(longTranscript()),
        ],
        child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
      ),
    );
    await tester.pumpAndSettle();

    final list = find.byType(Scrollable).first;
    final position = tester.state<ScrollableState>(list).position;

    // Scroll back through history. The list is reverse:true, so the live end
    // is offset 0 and scrolling away increases the offset.
    await tester.fling(list, const Offset(0, 300), 1200);
    await tester.pump();
    expect(position.pixels, greaterThan(0));

    // Tap while the ballistic fling is still running — the case the gesture
    // guard used to swallow.
    final fab = find.byTooltip(RegExp(r'^Jump to latest'));
    expect(fab, findsOneWidget);
    await tester.tap(fab);
    await tester.pumpAndSettle();

    expect(position.pixels, 0, reason: 'the tap must reach the live end');
    expect(
      find.byTooltip(RegExp(r'^Jump to latest')),
      findsNothing,
      reason: 'at the live end there is nothing left to jump to',
    );
  });
}

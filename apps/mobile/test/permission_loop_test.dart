import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// Fails every permission response, the way the daemon does once it has
/// already retired a request ("unknown permission").
class _RejectingClient extends McremoteClient {
  int respondAttempts = 0;

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [SessionMeta(id: 's1', provider: 'codex', cwd: '/home/mac')],
        complete: true,
      );

  @override
  Future<void> respondPermission({
    required String sessionId,
    required String permissionId,
    String? optionId,
    bool cancelled = false,
  }) async {
    respondAttempts++;
    throw McException(
      'unknown permission: $permissionId',
      code: 'permission_failed',
    );
  }
}

SessionTranscript withPendingPermission() {
  final ev = SessionEvent(
    type: 'permission_request',
    sessionId: 's1',
    permissionId: 'perm-1',
    toolName: 'command',
    text: 'rm -rf /tmp/x',
    options: [
      PermissionOption(
        optionId: 'accept',
        name: 'Allow once',
        kind: 'allow_once',
      ),
      PermissionOption(optionId: 'decline', name: 'Deny', kind: 'deny'),
    ],
  );
  return SessionTranscript(
    sessionId: 's1',
    status: 'running',
    items: const [],
    nextSeq: 0,
    pendingPermissions: {'perm-1': ev},
  );
}

SessionEvent alwaysPermission() => SessionEvent(
  type: 'permission_request',
  sessionId: 's1',
  permissionId: 'perm-always',
  toolName: 'command',
  options: [
    PermissionOption(
      optionId: 'allow_always',
      name: 'Allow always',
      kind: 'allow_always',
    ),
  ],
);

Widget host(McremoteClient client, SessionTranscript transcript) {
  return ProviderScope(
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
}

void main() {
  testWidgets('a failed permission response does not re-present in a loop', (
    tester,
  ) async {
    // The reported codex symptom: approve → "permission respond failed" → the
    // sheet comes straight back → approve again → forever, with the app
    // unusable behind an un-dismissable modal.
    final client = _RejectingClient();
    await tester.pumpWidget(host(client, withPendingPermission()));
    await tester.pumpAndSettle();

    expect(find.text('Allow once'), findsOneWidget);

    await tester.tap(find.text('Allow once'));
    await tester.pumpAndSettle();

    expect(client.respondAttempts, 1);
    expect(find.textContaining('Permission respond failed'), findsOneWidget);
    // The sheet stays down. Re-presenting here is what made the loop.
    expect(find.text('Allow once'), findsNothing);
  });

  testWidgets('the Review banner still allows a deliberate retry', (
    tester,
  ) async {
    // Breaking the loop must not strand the user: the request is still
    // outstanding, so there has to be a way back to it.
    final client = _RejectingClient();
    await tester.pumpWidget(host(client, withPendingPermission()));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Allow once'));
    await tester.pumpAndSettle();
    expect(client.respondAttempts, 1);

    expect(find.text('Review'), findsOneWidget);
    await tester.tap(find.text('Review'));
    await tester.pumpAndSettle();

    expect(
      find.text('Allow once'),
      findsOneWidget,
      reason: 'Review must re-present the outstanding request',
    );
    await tester.tap(find.text('Allow once'));
    await tester.pumpAndSettle();
    expect(client.respondAttempts, 2);
  });

  testWidgets(
    'external resolution closes both the always confirmation and its sheet',
    (tester) async {
      tester.view.physicalSize = const Size(500, 1200);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);
      final client = _RejectingClient();
      final container = ProviderContainer(
        overrides: [
          mcremoteClientProvider.overrideWithValue(client),
          connectionStateProvider.overrideWith(
            (ref) => Stream.value(McConnectionState.connected),
          ),
        ],
      );
      addTearDown(container.dispose);
      await tester.pumpWidget(
        UncontrolledProviderScope(
          container: container,
          child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
        ),
      );
      container
          .read(transcriptsProvider.notifier)
          .debugOnEvent(alwaysPermission());
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      await tester.pump(const Duration(milliseconds: 500));
      await tester.ensureVisible(find.text('Allow always'));
      await tester.pump();
      await tester.tap(find.text('Allow always'));
      await tester.pump(const Duration(milliseconds: 500));
      expect(find.text('Allow always?'), findsOneWidget);

      container
          .read(transcriptsProvider.notifier)
          .clearPending('s1', permissionId: 'perm-always');
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));

      expect(find.text('Allow always?'), findsNothing);
      expect(find.text('Allow always'), findsNothing);
      expect(tester.takeException(), isNull);
    },
  );
}

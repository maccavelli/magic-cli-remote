import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/features/sessions/sessions_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

import 'support/fake_path_provider.dart';

/// End-to-end navigation check for the reported bug: ending the only session
/// from the chat's session-actions menu must return the user to the sessions
/// landing screen, with the list refreshed (the ended row gone).
class _FakeClient extends McremoteClient {
  _FakeClient({this.sessions = const <SessionMeta>[]}) {
    linkHealth.value = LinkHealth.fresh;
  }

  List<SessionMeta> sessions;
  int listCalls = 0;
  final List<String> deleteCalls = [];
  Completer<void>? deleteGate;

  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async {
    listCalls++;
    return SessionListSnapshot(sessions: sessions, complete: true);
  }

  @override
  Future<List<ProviderInfo>> listProviders() async => const [];

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<void> cancel(String sessionId) async {}

  @override
  Future<void> deleteSession(String sessionId) async {
    deleteCalls.add(sessionId);
    final gate = deleteGate;
    if (gate != null) await gate.future;
    sessions = sessions.where((s) => s.id != sessionId).toList();
  }
}

void main() {
  setUp(() {
    SharedPreferences.setMockInitialValues({});
    useFakePathProvider(addTearDown);
  });

  Future<RouteObserver<PageRoute<void>>> pumpApp(
    WidgetTester tester,
    _FakeClient client,
  ) async {
    final observer = RouteObserver<PageRoute<void>>();
    final router = GoRouter(
      initialLocation: '/sessions',
      observers: [observer],
      routes: [
        GoRoute(
          path: '/sessions',
          builder: (context, state) => const SessionsScreen(),
          routes: [
            GoRoute(
              path: ':id',
              builder: (context, state) => ChatScreen(
                key: ValueKey(state.pathParameters['id']),
                sessionId: state.pathParameters['id']!,
                sessionName: state.uri.queryParameters['name'],
              ),
            ),
          ],
        ),
      ],
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          connectionStateProvider.overrideWith(
            (ref) => Stream.value(McConnectionState.connected),
          ),
          mcremoteClientProvider.overrideWithValue(client),
          routeObserverProvider.overrideWithValue(observer),
        ],
        child: MaterialApp.router(routerConfig: router),
      ),
    );
    await tester.pumpAndSettle();
    return observer;
  }

  testWidgets(
    'ending the only session in chat returns to a populated sessions list',
    (tester) async {
      final client = _FakeClient(
        sessions: [SessionMeta(id: 'sess-a', provider: 'kilo', name: 'Alpha')],
      );
      await pumpApp(tester, client);
      expect(find.text('Alpha'), findsOneWidget);

      // Open the chat for the only session.
      await tester.tap(find.text('Alpha'));
      await tester.pumpAndSettle();
      expect(find.text('End agent session?'), findsNothing);
      expect(find.byIcon(Icons.more_vert), findsOneWidget);

      // End the session from the chat's session-actions menu.
      final callsBeforeEnd = client.listCalls;
      await tester.tap(find.byIcon(Icons.more_vert));
      await tester.pumpAndSettle();
      await tester.tap(find.text('End session'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'End session'));
      await tester.pumpAndSettle();

      expect(client.deleteCalls, ['sess-a']);
      expect(
        client.listCalls,
        greaterThan(callsBeforeEnd),
        reason: 'the sessions list must refresh on return',
      );

      // The chat route is gone; the sessions landing screen is visible and
      // shows its empty state — not a blank screen.
      expect(find.text('End agent session?'), findsNothing);
      expect(find.text('Sessions'), findsOneWidget);
      expect(find.text('Alpha'), findsNothing);
      expect(find.text('No sessions on this device'), findsOneWidget);

      // The refresh keeps working afterwards (pull-to-refresh path).
      await tester.pump(const Duration(seconds: 4));
      await tester.pumpAndSettle();
      expect(find.text('No sessions on this device'), findsOneWidget);
    },
  );

  testWidgets(
    'back press while the delete is in flight leaves the sessions list intact',
    (tester) async {
      final client = _FakeClient(
        sessions: [SessionMeta(id: 'sess-b', provider: 'kilo', name: 'Beta')],
      );
      await pumpApp(tester, client);
      await tester.tap(find.text('Beta'));
      await tester.pumpAndSettle();

      // Start ending the session; the fake delete stalls on the gate.
      client.deleteGate = Completer<void>();
      await tester.tap(find.byIcon(Icons.more_vert));
      await tester.pumpAndSettle();
      await tester.tap(find.text('End session'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'End session'));
      await tester.pump();

      // The user backs out while the delete is still outstanding. The chat
      // route starts its exit transition; its State stays mounted until the
      // transition finishes.
      await tester.pageBack();
      await tester.pump(const Duration(milliseconds: 100));

      // The delete completes mid-transition.
      client.deleteGate!.complete();
      await tester.pumpAndSettle();

      // No rogue pop: the sessions route is still on the stack and renders
      // its landing state instead of a blank screen.
      expect(client.deleteCalls, ['sess-b']);
      expect(find.text('End agent session?'), findsNothing);
      expect(find.text('Sessions'), findsOneWidget);
      expect(find.text('Beta'), findsNothing);
      expect(find.text('No sessions on this device'), findsOneWidget);
    },
  );

  testWidgets(
    'a permission sheet open at delete completion still lands on the sessions screen',
    (tester) async {
      final client = _FakeClient(
        sessions: [
          SessionMeta(id: 'sess-c', provider: 'kilo', name: 'Gamma'),
        ],
      );
      await pumpApp(tester, client);
      await tester.tap(find.text('Gamma'));
      await tester.pumpAndSettle();

      // Start ending the session; the fake delete stalls on the gate.
      client.deleteGate = Completer<void>();
      await tester.tap(find.byIcon(Icons.more_vert));
      await tester.pumpAndSettle();
      await tester.tap(find.text('End session'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'End session'));
      await tester.pump();

      // A permission ask arrives over the socket while the delete is in
      // flight and its sheet comes up. The sheet is isDismissible:false —
      // only resolution or external retirement can close it.
      final container = ProviderScope.containerOf(
        tester.element(find.byType(ChatScreen)),
        listen: false,
      );
      container.read(transcriptsProvider.notifier).debugOnEvent(
        SessionEvent(
          type: 'permission_request',
          sessionId: 'sess-c',
          permissionId: 'perm-end-race',
          toolName: 'command',
          text: 'rm -rf /tmp/x',
          options: [
            PermissionOption(
              optionId: 'accept',
              name: 'Allow once',
              kind: 'allow_once',
            ),
            PermissionOption(
              optionId: 'decline',
              name: 'Deny',
              kind: 'deny',
            ),
          ],
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('Allow once'), findsOneWidget);

      // The delete completes with the sheet open: clearSession must
      // retire the sheet, and the flow must land on the sessions screen
      // with the post-delete list — no user action.
      client.deleteGate!.complete();
      await tester.pumpAndSettle();

      expect(client.deleteCalls, ['sess-c']);
      expect(find.text('Allow once'), findsNothing);
      expect(find.text('End agent session?'), findsNothing);
      expect(find.text('Sessions'), findsOneWidget);
      expect(find.text('Gamma'), findsNothing);
      expect(find.text('No sessions on this device'), findsOneWidget);
    },
  );
}

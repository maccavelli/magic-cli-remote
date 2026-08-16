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
  bool failDelete = false;
  bool failDeleteKeepsRow = false;

  /// MADR 0095 F1: model a host whose store enumeration skipped rows —
  /// a successful session.list_result carrying a PARTIAL list. Absence
  /// from such a list is not evidence of removal (MADR 0056 H-6).
  bool listIncomplete = false;

  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async {
    listCalls++;
    return SessionListSnapshot(sessions: sessions, complete: !listIncomplete);
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

  /// MADR 0095 F2: fail the prompt so the chat raises its
  /// "Send failed … [Sessions]" toast, which lives in the ROOT overlay
  /// and outlives this route.
  bool failPrompt = false;

  @override
  Future<void> prompt(
    String sessionId,
    String text, {
    List<PromptAttachment> attachments = const [],
  }) async {
    if (failPrompt) {
      throw McException('socket closed', code: 'connection_lost');
    }
  }

  @override
  Future<SessionDiagnostics> sessionDiagnostics(String sessionId) async =>
      SessionDiagnostics();

  @override
  Future<SessionMeta> forkSession(
    String sessionId, {
    String? messageId,
    String? lastTurnId,
  }) async => SessionMeta(id: 'sess-fork', provider: 'kilo', name: 'Fork');

  @override
  Future<void> deleteSession(String sessionId) async {
    deleteCalls.add(sessionId);
    final gate = deleteGate;
    if (gate != null) await gate.future;
    // Model host truth for the lost-ok case: the purge happened; only
    // the ok response was lost. Removing the row BEFORE throwing is what
    // makes the confirming list read see the row absent (D7/P4b).
    // failDeleteKeepsRow models the conservative case instead: the RPC
    // failed and the host still has the session (D7/P4).
    if (!failDeleteKeepsRow) {
      sessions = sessions.where((s) => s.id != sessionId).toList();
    }
    if (failDelete) {
      throw McException('timed out', code: 'timeout');
    }
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
        sessions: [SessionMeta(id: 'sess-c', provider: 'kilo', name: 'Gamma')],
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
      container
          .read(transcriptsProvider.notifier)
          .debugOnEvent(
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

  testWidgets('a delete whose ok was lost still lands on the sessions screen', (
    tester,
  ) async {
    final client = _FakeClient(
      sessions: [SessionMeta(id: 'sess-d', provider: 'kilo', name: 'Delta')],
    );
    client.failDelete = true;
    await pumpApp(tester, client);
    await tester.tap(find.text('Delta'));
    await tester.pumpAndSettle();

    // End the session: the delete RPC throws (the ok was lost), but the
    // fake models the host truth — the session was purged.
    await tester.tap(find.byIcon(Icons.more_vert));
    await tester.pumpAndSettle();
    await tester.tap(find.text('End session'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'End session'));
    // One pump: the whole fake-client microtask chain has unwound and
    // the toast is up (pumpAndSettle would animate it away).
    await tester.pump();

    // D7: the confirming list read sees the row gone, classifies the
    // delete as ended, and runs the completion tail — the success toast
    // is up now and will animate away during settle.
    expect(client.deleteCalls, ['sess-d']);
    expect(find.textContaining('End session failed'), findsNothing);
    expect(find.text('Session ended'), findsOneWidget);

    await tester.pumpAndSettle();
    expect(find.text('Sessions'), findsOneWidget);
    expect(find.text('Delta'), findsNothing);
    expect(find.text('No sessions on this device'), findsOneWidget);
  });

  testWidgets('a failed delete whose row survives keeps the user in the chat', (
    tester,
  ) async {
    final client = _FakeClient(
      sessions: [SessionMeta(id: 'sess-e', provider: 'kilo', name: 'Epsilon')],
    );
    client.failDelete = true;
    client.failDeleteKeepsRow = true;
    await pumpApp(tester, client);
    await tester.tap(find.text('Epsilon'));
    await tester.pumpAndSettle();

    await tester.tap(find.byIcon(Icons.more_vert));
    await tester.pumpAndSettle();
    await tester.tap(find.text('End session'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'End session'));
    await tester.pump();

    // P4 conservative: the confirming read sees the row, the error
    // toast shows, and the user stays in the chat. ('Sessions' is the
    // offstage route below — find.text skips offstage by default.)
    expect(client.deleteCalls, ['sess-e']);
    expect(find.textContaining('End session failed'), findsOneWidget);
    expect(find.text('Epsilon'), findsWidgets);
    expect(find.text('Sessions'), findsNothing);

    await tester.pumpAndSettle();
    expect(find.text('Epsilon'), findsWidgets);
    expect(find.text('Sessions'), findsNothing);
  });

  testWidgets(
    'a delete that fails against an incomplete list keeps the user in the chat',
    (tester) async {
      final client = _FakeClient(
        sessions: [SessionMeta(id: 'sess-f', provider: 'kilo', name: 'Zeta')],
      );
      // The purge did NOT happen; the row is missing from the confirming
      // read only because the host could not enumerate it (complete=false).
      client.failDelete = true;
      client.listIncomplete = true;
      await pumpApp(tester, client);
      await tester.tap(find.text('Zeta'));
      await tester.pumpAndSettle();

      await tester.tap(find.byIcon(Icons.more_vert));
      await tester.pumpAndSettle();
      await tester.tap(find.text('End session'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'End session'));
      // One pump: the fake-client microtask chain has unwound and the
      // toast is up (pumpAndSettle would animate it away).
      await tester.pump();

      // D2 (MADR 0095): a partial list cannot confirm a purge. Conservative
      // branch — error toast, user stays in the chat, transcript intact.
      expect(client.deleteCalls, ['sess-f']);
      expect(find.textContaining('End session failed'), findsOneWidget);
      expect(find.text('Session ended'), findsNothing);

      await tester.pumpAndSettle();
      expect(find.text('Zeta'), findsWidgets);
      expect(find.text('Sessions'), findsNothing);

      final container = ProviderScope.containerOf(
        tester.element(find.byType(ChatScreen)),
        listen: false,
      );
      expect(
        container.read(transcriptsProvider.notifier).debugIsCleared('sess-f'),
        isFalse,
        reason: 'an unconfirmed purge must not tombstone the session',
      );
    },
  );

  testWidgets(
    'the send-failure toast action does not pop the last page after a back press',
    (tester) async {
      final client = _FakeClient(
        sessions: [SessionMeta(id: 'sess-g', provider: 'kilo', name: 'Eta')],
      );
      client.failPrompt = true;
      await pumpApp(tester, client);
      await tester.tap(find.text('Eta'));
      await tester.pumpAndSettle();

      await tester.enterText(find.byType(TextField).first, 'hello');
      await tester.pump();
      await tester.tap(find.byIcon(Icons.send));
      await tester.pump();
      // The toast's action button is itself labelled "Sessions", so the
      // landing screen is identified by its AppBar title, not by raw text.
      expect(find.widgetWithText(AppBar, 'Sessions'), findsNothing);
      expect(find.widgetWithText(TextButton, 'Sessions'), findsOneWidget);

      // The user presses system back. Driven through the binding, not
      // tester.pageBack(): the notification card is positioned over the
      // app bar, so a tap aimed at the back button lands on the toast and
      // the route never pops (probe, MADR 0095 Step 4).
      await tester.binding.handlePopRoute();
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));

      // The toast lives in the root overlay, so it is still on screen and
      // still tappable, and the chat State stays mounted through the exit
      // transition (the fact MADR 0094 D1 rests on).
      expect(find.widgetWithText(TextButton, 'Sessions'), findsOneWidget);
      await tester.tap(find.widgetWithText(TextButton, 'Sessions'));
      await tester.pumpAndSettle();

      // No rogue pop: the sessions route still renders its list.
      expect(tester.takeException(), isNull);
      expect(find.widgetWithText(AppBar, 'Sessions'), findsOneWidget);
      expect(find.text('Eta'), findsOneWidget);
    },
  );

  testWidgets(
    'the send-failure toast action still navigates after the chat is gone',
    (tester) async {
      final client = _FakeClient(
        sessions: [
          SessionMeta(id: 'sess-h', provider: 'kilo', name: 'Theta'),
          SessionMeta(id: 'sess-i', provider: 'kilo', name: 'Iota'),
        ],
      );
      client.failPrompt = true;
      await pumpApp(tester, client);
      await tester.tap(find.text('Theta'));
      await tester.pumpAndSettle();

      await tester.enterText(find.byType(TextField).first, 'hello');
      await tester.pump();
      await tester.tap(find.byIcon(Icons.send));
      await tester.pump();

      // Let the exit finish so the first chat's State is disposed, then
      // open a DIFFERENT session. The toast still belongs to the first
      // chat, and `mounted` on that dead State is not a usable guard.
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      await tester.tap(find.text('Iota'));
      await tester.pumpAndSettle();
      expect(find.widgetWithText(AppBar, 'Sessions'), findsNothing);
      expect(find.widgetWithText(TextButton, 'Sessions'), findsOneWidget);

      await tester.tap(find.widgetWithText(TextButton, 'Sessions'));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.widgetWithText(AppBar, 'Sessions'), findsOneWidget);
      expect(find.text('Theta'), findsOneWidget);
    },
  );

  testWidgets(
    'an untracked dialog open at delete completion still lands on the sessions screen',
    (tester) async {
      final client = _FakeClient(
        sessions: [SessionMeta(id: 'sess-i2', provider: 'kilo', name: 'Iota2')],
      );
      await pumpApp(tester, client);
      await tester.tap(find.text('Iota2'));
      await tester.pumpAndSettle();

      client.deleteGate = Completer<void>();
      await tester.tap(find.byIcon(Icons.more_vert));
      await tester.pumpAndSettle();
      await tester.tap(find.text('End session'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'End session'));
      await tester.pump();

      // Nothing stops the user opening another action while the delete is
      // outstanding: _endingSession guards only a second End tap.
      await tester.tap(find.byIcon(Icons.more_vert));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Session diagnostics'));
      await tester.pumpAndSettle();
      expect(find.byType(AlertDialog), findsOneWidget);

      client.deleteGate!.complete();
      await tester.pumpAndSettle();

      expect(client.deleteCalls, ['sess-i2']);
      expect(find.widgetWithText(AppBar, 'Sessions'), findsOneWidget);
      expect(find.text('Iota2'), findsNothing);
      expect(find.text('No sessions on this device'), findsOneWidget);
    },
  );

  testWidgets(
    'a forked chat pushed over the chat still lands on the sessions screen',
    (tester) async {
      final client = _FakeClient(
        sessions: [SessionMeta(id: 'sess-j', provider: 'kilo', name: 'Kappa')],
      );
      await pumpApp(tester, client);
      await tester.tap(find.text('Kappa'));
      await tester.pumpAndSettle();

      client.deleteGate = Completer<void>();
      await tester.tap(find.byIcon(Icons.more_vert));
      await tester.pumpAndSettle();
      await tester.tap(find.text('End session'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'End session'));
      await tester.pump();

      // context.push keeps the old chat State ALIVE underneath — the case
      // 0094-PLAN's P5 row attributed to `mounted == false` (MADR 0095 F3).
      await tester.tap(find.byIcon(Icons.more_vert));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Fork session'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'Fork'));
      await tester.pumpAndSettle();

      client.deleteGate!.complete();
      await tester.pumpAndSettle();

      expect(client.deleteCalls, ['sess-j']);
      expect(find.widgetWithText(AppBar, 'Sessions'), findsOneWidget);
      expect(find.text('Kappa'), findsNothing);
    },
  );
}

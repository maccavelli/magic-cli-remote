import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:magic_cli_remote/features/sessions/sessions_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';

class MockMcremoteClient extends McremoteClient {
  MockMcremoteClient({this.sessions = const <SessionMeta>[]}) {
    linkHealth.value = LinkHealth.fresh;
  }

  final List<SessionMeta> sessions;
  int listCalls = 0;

  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async {
    listCalls++;
    return SessionListSnapshot(sessions: sessions, complete: true);
  }

  @override
  Future<List<ProviderInfo>> listProviders() async => const [];
}

void main() {
  testWidgets(
    'row taps still open a chat after a pushed chat was exited via go() '
    '(notification tap replaces the stack), and the return still refreshes',
    (tester) async {
      final client = MockMcremoteClient(
        sessions: [
          SessionMeta(id: 'sess-a', provider: 'claude', name: 'Alpha'),
          SessionMeta(id: 'sess-b', provider: 'claude', name: 'Beta'),
        ],
      );
      final observer = RouteObserver<PageRoute<void>>();
      // Mirrors app.dart: '/sessions' with a ':id' subroute and the shared
      // route observer on the root navigator. The chat body is a stub — the
      // wedge under test lives in SessionsScreen + go_router semantics, not
      // in ChatScreen.
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
                builder: (context, state) => Scaffold(
                  appBar: AppBar(
                    title: Text('chat-${state.pathParameters['id']}'),
                  ),
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
      expect(find.text('Alpha'), findsOneWidget);

      // 1. Row tap pushes chat A imperatively (context.push).
      await tester.tap(find.text('Alpha'));
      await tester.pumpAndSettle();
      expect(find.text('chat-sess-a'), findsOneWidget);

      // 2. A session-notification tap navigates declaratively (app.dart wires
      //    onOpenSession to router.go). The pushed chat A is removed without
      //    ever resolving its push future.
      router.go('/sessions/sess-b');
      await tester.pumpAndSettle();
      expect(find.text('chat-sess-b'), findsOneWidget);

      // 3. Back out to the list. The refresh-on-return is owned by
      //    didPopNext, so it must fire for this declarative chat too.
      final callsBeforePop = client.listCalls;
      router.pop();
      await tester.pumpAndSettle();
      expect(find.text('Alpha'), findsOneWidget);
      expect(
        client.listCalls,
        greaterThan(callsBeforePop),
        reason: 'returning from a chat no longer refreshes the list',
      );

      // 4. Row taps must still open chats. With the wedge, _openingSession is
      //    stuck true and this tap is silently swallowed.
      await tester.tap(find.text('Beta'));
      await tester.pumpAndSettle();
      expect(
        find.text('chat-sess-b'),
        findsOneWidget,
        reason: 'sessions screen is wedged: row taps no longer open chats',
      );
    },
  );

  testWidgets('double tap on a row pushes a single chat (MADR 0046 L-12)', (
    tester,
  ) async {
    final client = MockMcremoteClient(
      sessions: [SessionMeta(id: 'sess-a', provider: 'claude', name: 'Alpha')],
    );
    var chatBuilds = 0;
    final router = GoRouter(
      initialLocation: '/sessions',
      routes: [
        GoRoute(
          path: '/sessions',
          builder: (context, state) => const SessionsScreen(),
          routes: [
            GoRoute(
              path: ':id',
              builder: (context, state) {
                chatBuilds++;
                return const Scaffold(body: Text('chat'));
              },
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
        ],
        child: MaterialApp.router(routerConfig: router),
      ),
    );
    await tester.pumpAndSettle();

    // Two taps in the same frame — the guard, not the pushed route's modal
    // barrier, is what must swallow the second one.
    await tester.tap(find.text('Alpha'));
    await tester.tap(find.text('Alpha'), warnIfMissed: false);
    await tester.pumpAndSettle();

    expect(find.text('chat'), findsOneWidget);
    expect(chatBuilds, 1);
  });
}

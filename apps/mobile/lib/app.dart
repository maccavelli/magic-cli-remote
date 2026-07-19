import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'app_lifecycle.dart';
import 'features/chat/chat_screen.dart';
import 'features/connect/connect_screen.dart';
import 'features/sessions/sessions_screen.dart';
import 'state/app_providers.dart';

final goRouterProvider = Provider<GoRouter>((ref) {
  final client = ref.read(mcremoteClientProvider);
  final listenable = _ConnectionListenable(client);
  ref.onDispose(listenable.dispose);
  return GoRouter(
    initialLocation: '/',
    refreshListenable: listenable,
    routes: [
      GoRoute(
        path: '/',
        builder: (context, state) => const ConnectScreen(),
      ),
      GoRoute(
        path: '/sessions',
        builder: (context, state) => const SessionsScreen(),
        routes: [
          GoRoute(
            path: ':id',
            builder: (context, state) {
              final id = state.pathParameters['id']!;
              final name = state.uri.queryParameters['name'];
              return ChatScreen(sessionId: id, sessionName: name);
            },
          ),
        ],
      ),
    ],
    redirect: (context, state) {
      final loc = state.matchedLocation;
      final st = client.state;
      final connected = st == McConnectionState.connected ||
          st == McConnectionState.reconnecting;
      if (!connected && loc.startsWith('/sessions')) {
        return '/';
      }
      if (connected && loc == '/') {
        return '/sessions';
      }
      return null;
    },
  );
});

/// Notifies GoRouter when connection state changes so redirect re-runs.
class _ConnectionListenable extends ChangeNotifier {
  _ConnectionListenable(this._client) {
    _sub = _client.connectionStates.listen((_) => notifyListeners());
  }

  final McremoteClient _client;
  late final StreamSubscription<McConnectionState> _sub;

  @override
  void dispose() {
    _sub.cancel();
    super.dispose();
  }
}

class MagicCliRemoteApp extends ConsumerWidget {
  const MagicCliRemoteApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final router = ref.watch(goRouterProvider);

    return ConnectionLifecycleScope(
      child: MaterialApp.router(
        title: 'Magic CLI Remote',
        theme: ThemeData(
          colorScheme: ColorScheme.fromSeed(
            seedColor: const Color(0xFF1B6B4A),
            brightness: Brightness.light,
          ),
          useMaterial3: true,
        ),
        darkTheme: ThemeData(
          colorScheme: ColorScheme.fromSeed(
            seedColor: const Color(0xFF1B6B4A),
            brightness: Brightness.dark,
          ),
          useMaterial3: true,
        ),
        themeMode: ThemeMode.system,
        routerConfig: router,
      ),
    );
  }
}

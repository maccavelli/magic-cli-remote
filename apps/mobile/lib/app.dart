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
      final online = st == McConnectionState.connected;
      final linking = st == McConnectionState.connecting ||
          st == McConnectionState.authenticating ||
          st == McConnectionState.reconnecting;

      // Paired device: stay on sessions even if the socket dropped (screen lock,
      // mesh blip). Only leave after explicit sign-out / no pairing.
      if (client.shouldStayInApp) {
        if (loc == '/') return '/sessions';
        return null;
      }

      // Not paired: keep away from the sessions UI.
      if (loc.startsWith('/sessions')) {
        return '/';
      }
      // Mid-handshake without paired flag yet: still allow sessions route if
      // connect is in flight from the pair screen.
      if ((online || linking) && loc == '/') {
        return '/sessions';
      }
      return null;
    },
    errorBuilder: (context, state) => _RouteErrorScreen(
      location: state.uri.toString(),
      error: state.error,
    ),
  );
});

/// Shown instead of go_router's raw error page for unknown locations.
class _RouteErrorScreen extends StatelessWidget {
  const _RouteErrorScreen({required this.location, this.error});

  final String location;
  final Object? error;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Scaffold(
      appBar: AppBar(title: const Text('Page not found')),
      body: SafeArea(
        child: Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(Icons.explore_off_outlined,
                    size: 48, color: scheme.onSurfaceVariant),
                const SizedBox(height: 12),
                Text(
                  'Nothing here',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                const SizedBox(height: 8),
                SelectableText(
                  location,
                  textAlign: TextAlign.center,
                  style: TextStyle(color: scheme.onSurfaceVariant),
                ),
                if (error != null) ...[
                  const SizedBox(height: 8),
                  Text(
                    '$error',
                    textAlign: TextAlign.center,
                    style: TextStyle(color: scheme.error, fontSize: 12),
                  ),
                ],
                const SizedBox(height: 20),
                FilledButton(
                  onPressed: () => context.go('/'),
                  child: const Text('Go to start'),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

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

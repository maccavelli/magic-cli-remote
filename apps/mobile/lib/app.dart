import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'app_lifecycle.dart';
import 'features/chat/chat_screen.dart';
import 'features/connect/connect_screen.dart';
import 'features/sessions/sessions_screen.dart';
import 'features/settings/settings_screen.dart';
import 'state/app_providers.dart';
import 'theme/celestial.dart';

final goRouterProvider = Provider<GoRouter>((ref) {
  final client = ref.read(mcremoteClientProvider);
  final listenable = _ConnectionListenable(client);
  ref.onDispose(listenable.dispose);
  return GoRouter(
    initialLocation: '/',
    refreshListenable: listenable,
    routes: [
      GoRoute(path: '/', builder: (context, state) => const ConnectScreen()),
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
      GoRoute(
        path: '/settings',
        builder: (context, state) => const SettingsScreen(),
      ),
    ],
    redirect: (context, state) {
      final loc = state.matchedLocation;

      // Paired device: stay on sessions even if the socket dropped (screen lock,
      // mesh blip). Only leave after explicit sign-out / no pairing.
      if (client.shouldStayInApp) {
        if (loc == '/') return '/sessions';
        return null;
      }

      // Not paired: keep away from the sessions UI. Crucially, do NOT
      // redirect away from '/' while a handshake is merely in flight —
      // `connecting` is emitted synchronously at the start of every attempt,
      // and yanking ConnectScreen off screen mid-handshake unmounts the very
      // code that would show pairing errors (and, with a stale token, loops
      // '/' ↔ '/sessions' forever). ConnectScreen navigates itself to
      // /sessions on success; `shouldStayInApp` covers everything after.
      if (loc.startsWith('/sessions')) {
        return '/';
      }
      return null;
    },
    errorBuilder: (context, state) =>
        _RouteErrorScreen(location: state.uri.toString(), error: state.error),
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
                Icon(
                  Icons.explore_off_outlined,
                  size: 48,
                  color: scheme.onSurfaceVariant,
                ),
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

class MagicCliRemoteApp extends ConsumerStatefulWidget {
  const MagicCliRemoteApp({super.key});

  @override
  ConsumerState<MagicCliRemoteApp> createState() => _MagicCliRemoteAppState();
}

class _MagicCliRemoteAppState extends ConsumerState<MagicCliRemoteApp> {
  @override
  void initState() {
    super.initState();
    // Tapping a notification opens its session. Wired once, not per build.
    final router = ref.read(goRouterProvider);
    ref.read(notificationCoordinatorProvider).onOpenSession = (id) =>
        router.go('/sessions/$id');
  }

  @override
  Widget build(BuildContext context) {
    final router = ref.watch(goRouterProvider);
    final themeMode = ref.watch(themeModeProvider);

    return ConnectionLifecycleScope(
      child: MaterialApp.router(
        title: 'Magic CLI Remote',
        theme: celestialLight,
        darkTheme: celestialDark,
        themeMode: themeMode,
        routerConfig: router,
      ),
    );
  }
}

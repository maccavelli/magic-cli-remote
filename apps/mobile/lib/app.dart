import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'features/chat/chat_screen.dart';
import 'features/connect/connect_screen.dart';
import 'features/sessions/sessions_screen.dart';
import 'state/app_providers.dart';

class MagicCliRemoteApp extends ConsumerWidget {
  const MagicCliRemoteApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final router = GoRouter(
      initialLocation: '/',
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
                return ChatScreen(sessionId: id);
              },
            ),
          ],
        ),
      ],
      redirect: (context, state) {
        final client = ref.read(mcremoteClientProvider);
        final loc = state.matchedLocation;
        final connected = client.state == McConnectionState.connected;
        if (!connected && loc.startsWith('/sessions')) {
          return '/';
        }
        return null;
      },
    );

    return MaterialApp.router(
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
    );
  }
}

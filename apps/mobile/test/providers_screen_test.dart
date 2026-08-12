import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:magic_cli_remote/features/settings/provider_detail_screen.dart';
import 'package:magic_cli_remote/features/settings/providers_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';

import 'provider_test_fakes.dart';

class _DisconnectedClient extends McremoteClient {
  @override
  McConnectionState get state => McConnectionState.disconnected;
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  Future<FakeAuthClient> pump(
    WidgetTester tester, {
    required List<ProviderInfo> providers,
  }) async {
    final client = FakeAuthClient(providers);
    addTearDown(client.authPush.close);
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          settingsStoreProvider.overrideWithValue(FakeModeStore()),
          mcremoteClientProvider.overrideWithValue(client),
        ],
        child: const MaterialApp(home: ProvidersScreen()),
      ),
    );
    await tester.pumpAndSettle();
    return client;
  }

  testWidgets('one identity card per agent, chip folded to the worst state', (
    tester,
  ) async {
    await pump(
      tester,
      providers: [
        // configured + quota → the card must warn, not read green.
        providerWith('kilo', const [
          configuredTogether,
          UpstreamAuth(id: 'deepseek', label: 'DeepSeek', status: 'quota'),
        ], active: 'together'),
        ProviderInfo(id: 'grok', ready: true),
      ],
    );

    expect(find.byKey(const Key('provider-card-kilo')), findsOneWidget);
    expect(find.byKey(const Key('provider-card-grok')), findsOneWidget);
    expect(find.text('Quota reached'), findsOneWidget);
    // An agent without credential state (older daemon, MADR 0074 D6) carries
    // only the readiness chip.
    expect(find.text('Ready'), findsOneWidget);
    expect(find.text('1 credential · together'), findsOneWidget);
  });

  testWidgets('disconnected reads as such instead of an empty fleet', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          settingsStoreProvider.overrideWithValue(FakeModeStore()),
          mcremoteClientProvider.overrideWithValue(_DisconnectedClient()),
        ],
        child: const MaterialApp(home: ProvidersScreen()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Not connected'), findsOneWidget);
  });

  testWidgets('a credential push refreshes the cards in place', (tester) async {
    final client = await pump(
      tester,
      providers: [
        providerWith('kilo', const [
          UpstreamAuth(id: 'together', label: 'Together AI', status: 'missing'),
        ]),
      ],
    );
    expect(find.text('Needs setup'), findsOneWidget);

    client.providers = [
      providerWith('kilo', const [configuredTogether]),
    ];
    client.authPush.add(const {'id': 'kilo'});
    await tester.pumpAndSettle();

    expect(find.text('Needs setup'), findsNothing);
    expect(find.text('Configured'), findsOneWidget);
  });

  testWidgets('a card drills into the agent detail screen', (tester) async {
    final client = FakeAuthClient([
      providerWith('kilo', const [configuredTogether]),
    ]);
    addTearDown(client.authPush.close);
    final router = GoRouter(
      initialLocation: '/settings/providers',
      routes: [
        GoRoute(
          path: '/settings/providers',
          builder: (_, _) => const ProvidersScreen(),
          routes: [
            GoRoute(
              path: ':pid',
              builder: (_, state) => ProviderDetailScreen(
                providerId: state.pathParameters['pid']!,
              ),
            ),
          ],
        ),
      ],
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          settingsStoreProvider.overrideWithValue(FakeModeStore()),
          mcremoteClientProvider.overrideWithValue(client),
        ],
        child: MaterialApp.router(routerConfig: router),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('provider-card-kilo')));
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('provider-auth-tile-kilo-together')),
      findsOneWidget,
    );
  });
}

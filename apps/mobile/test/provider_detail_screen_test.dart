import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:magic_cli_remote/features/settings/provider_detail_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';

import 'provider_test_fakes.dart';

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  Future<(FakeAuthClient, FakeModeStore)> pump(
    WidgetTester tester, {
    required List<ProviderInfo> providers,
    String providerId = 'kilo',
  }) async {
    tester.view.physicalSize = const Size(1000, 3000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final client = FakeAuthClient(providers);
    final store = FakeModeStore();
    addTearDown(client.authPush.close);
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          settingsStoreProvider.overrideWithValue(store),
          mcremoteClientProvider.overrideWithValue(client),
        ],
        child: MaterialApp(home: ProviderDetailScreen(providerId: providerId)),
      ),
    );
    await tester.pumpAndSettle();
    return (client, store);
  }

  // ---------------------------------------------------------------------
  // MADR 0082 P1/P3 — one semantic chip per credential state, and a
  // confirmation before removal (the F5 fix). Ported from the settings
  // screen when the rows moved here (D3).
  // ---------------------------------------------------------------------

  testWidgets('credential rows carry one semantic chip per state', (
    tester,
  ) async {
    await pump(
      tester,
      providers: [
        providerWith('kilo', const [
          configuredTogether,
          UpstreamAuth(id: 'deepseek', label: 'DeepSeek', status: 'quota'),
          UpstreamAuth(id: 'openai', label: 'OpenAI', status: 'error'),
          UpstreamAuth(id: 'groq', label: 'Groq', status: 'missing'),
        ], active: 'together'),
      ],
    );

    // Row chips: one of each state. The Status section's summary chip folds
    // to the worst state (error), so error appears twice.
    expect(find.byKey(const Key('status-chip-ok')), findsOneWidget);
    expect(find.byKey(const Key('status-chip-caution')), findsOneWidget);
    expect(find.byKey(const Key('status-chip-error')), findsNWidgets(2));
    expect(find.byKey(const Key('status-chip-neutral')), findsOneWidget);
    // "Active" is its own filled chip, never a suffix on the status label.
    expect(find.byKey(const Key('status-chip-active')), findsOneWidget);
    expect(find.textContaining('· active'), findsNothing);
  });

  testWidgets('removing a credential asks first and honours Cancel', (
    tester,
  ) async {
    final (client, _) = await pump(
      tester,
      providers: [
        providerWith('kilo', const [configuredTogether]),
      ],
    );

    await tester.tap(find.byTooltip('Remove credential'));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('remove-credential-confirm')), findsOneWidget);

    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(client.removed, isEmpty);

    await tester.tap(find.byTooltip('Remove credential'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Remove'));
    await tester.pumpAndSettle();
    expect(client.removed, [('kilo', 'together')]);
  });

  // ---------------------------------------------------------------------
  // MADR 0082 D3 — the pieces F8 found split across sections live here.
  // ---------------------------------------------------------------------

  testWidgets('default mode picks persist through the store', (tester) async {
    final (_, store) = await pump(
      tester,
      providers: [
        providerWith('kilo', const [configuredTogether]),
      ],
    );

    await tester.tap(find.byKey(const Key('provider-default-mode-kilo')));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(ListTile, 'Plan'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Select'));
    await tester.pumpAndSettle();

    expect(store.modes['kilo'], 'plan');
    expect(find.text('plan'), findsOneWidget);
  });

  testWidgets('one configured upstream: no switch control (D14)', (
    tester,
  ) async {
    await pump(
      tester,
      providers: [
        providerWith('kilo', const [configuredTogether]),
      ],
    );
    expect(
      find.byKey(const Key('provider-active-upstream-kilo')),
      findsNothing,
    );
  });

  testWidgets('two configured upstreams: the switch control appears', (
    tester,
  ) async {
    await pump(
      tester,
      providers: [
        providerWith('kilo', const [
          configuredTogether,
          configuredDeepseek,
        ], active: 'together'),
      ],
    );
    expect(
      find.byKey(const Key('provider-active-upstream-kilo')),
      findsOneWidget,
    );
  });

  testWidgets('switching upstreams goes through the client', (tester) async {
    final (client, _) = await pump(
      tester,
      providers: [
        providerWith('kilo', const [
          configuredTogether,
          configuredDeepseek,
        ], active: 'together'),
      ],
    );

    await tester.tap(find.byKey(const Key('provider-active-upstream-kilo')));
    await tester.pumpAndSettle();
    // 'DeepSeek' also names the credential row behind the sheet; the picker
    // option is the topmost match.
    await tester.tap(find.widgetWithText(ListTile, 'DeepSeek').last);
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Select'));
    await tester.pumpAndSettle();

    expect(client.switched, [('kilo', 'deepseek')]);
  });

  testWidgets('Add credential opens the vendor catalog', (tester) async {
    final (client, _) = await pump(
      tester,
      providers: [
        providerWith('kilo', const [configuredTogether]),
      ],
    );
    client.catalogPage = const ProviderAuthCatalog(
      providerId: 'kilo',
      upstreams: [
        UpstreamAuth(
          id: 'groq',
          label: 'Groq',
          status: 'missing',
          methods: [AuthMethod(id: 'groq:0', type: 'api_key', label: 'Key')],
        ),
      ],
      total: 1,
    );

    await tester.tap(find.byKey(const Key('provider-add-credential-kilo')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('upstream-catalog-search')), findsOneWidget);
    expect(find.byKey(const Key('upstream-catalog-row-groq')), findsOneWidget);
  });

  testWidgets('Add credential clears a simulated gesture-nav inset', (
    tester,
  ) async {
    // MADR 0083 L3: the last row is Add credential — the row the whole
    // feature exists for must not sit under the system bar.
    tester.view.viewPadding = FakeViewPadding(bottom: 96);
    addTearDown(tester.view.resetViewPadding);
    await pump(
      tester,
      providers: [
        providerWith('kilo', [
          for (var i = 0; i < 14; i++)
            UpstreamAuth(id: 'v$i', label: 'Vendor $i', status: 'configured'),
        ], active: 'v0'),
      ],
    );

    final list = tester.widget<ListView>(find.byType(ListView));
    final bottom = list.padding!.resolve(TextDirection.ltr).bottom;
    expect(bottom, greaterThanOrEqualTo(96));
  });

  testWidgets('a keyring-managed failure surfaces actionably, as an error', (
    tester,
  ) async {
    // MADR 0083 D5 end to end: wire code → friendly copy → error styling.
    final (client, _) = await pump(
      tester,
      providers: [
        providerWith('goose', const [configuredTogether]),
      ],
      providerId: 'goose',
    );
    client.credentialError = McException(
      'goose keeps secrets in the OS keyring; …',
      code: 'keyring_managed',
    );

    await tester.tap(find.byTooltip('Remove credential'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Remove'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.textContaining('keyring'), findsOneWidget);
    // The toast must carry error severity, not the default info style.
    final toast = tester.widget(
      find.byWidgetPredicate(
        (w) => w.runtimeType.toString() == '_TopNotification',
      ),
    );
    expect((toast as dynamic).severity.toString(), contains('error'));
    // Let the toast timer run out so no timers outlive the test.
    await tester.pumpAndSettle(const Duration(seconds: 8));
  });

  testWidgets('an unknown agent reads as gone, not as a crash', (tester) async {
    await pump(tester, providers: const [], providerId: 'kilo');
    expect(find.textContaining('No agent named kilo'), findsOneWidget);
  });
}

import 'package:flutter/foundation.dart'
    show TargetPlatform, debugDefaultTargetPlatformOverride;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/notifications/notification_coordinator.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart' show TlsMode;
import 'package:magic_cli_remote/data/ws/client_identity.dart';
import 'package:magic_cli_remote/data/ws/transport_probes.dart';
import 'package:magic_cli_remote/features/settings/providers_screen.dart';
import 'package:magic_cli_remote/features/settings/settings_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';

class _FakeStore extends SettingsStore {
  _FakeStore({
    this.relayUrl,
    this.relayHostId,
    this.relayAuthority,
    this.sticky,
  });

  String themeMode = 'system';
  bool notifications = true;
  bool notifyAsks = true;
  bool notifyTurnComplete = true;
  bool notifyErrors = true;
  String? host = '10.0.0.5:7531';
  String? relayUrl;
  String? relayHostId;
  String? relayAuthority;
  TransportMode? sticky;
  TransportMode? selection;
  String connectMode = 'auto';
  String? token;
  bool clearTokenCalled = false;
  bool clearSecretsCalled = false;
  bool clearFingerprintCalled = false;
  ({String cert, String key})? identity;
  ({String op, String error, DateTime at})? storageFailure;

  @override
  Future<String> getThemeMode() async => themeMode;
  @override
  Future<void> setThemeMode(String mode) async => themeMode = mode;
  @override
  Future<bool> getNotificationsEnabled() async => notifications;
  @override
  Future<void> setNotificationsEnabled(bool enabled) async =>
      notifications = enabled;
  @override
  Future<bool> getNotifyAsks() async => notifyAsks;
  @override
  Future<void> setNotifyAsks(bool v) async => notifyAsks = v;
  @override
  Future<bool> getNotifyTurnComplete() async => notifyTurnComplete;
  @override
  Future<void> setNotifyTurnComplete(bool v) async => notifyTurnComplete = v;
  @override
  Future<bool> getNotifyErrors() async => notifyErrors;
  @override
  Future<void> setNotifyErrors(bool v) async => notifyErrors = v;
  @override
  Future<String?> getHost() async => host;
  @override
  Future<String?> getDefaultThinkingLevel() async => null;
  @override
  Future<String?> getDefaultSessionMode(String provider) async => null;
  @override
  Future<bool> getSendWithEnter() async => true;
  @override
  Future<List<String>> getPinnedCwds() async => const [];
  @override
  Future<List<String>> getRecentCwds() async => const [];
  @override
  Future<String?> getRelayUrl() async => relayUrl;
  @override
  Future<String?> getRelayHostId() async => relayHostId;
  @override
  Future<String?> getRelayAuthority() async => relayAuthority;
  @override
  Future<TransportMode?> getLastTransportSuccess(String? hostAuthority) async =>
      sticky;
  @override
  Future<void> setTransportSelection(
    TransportMode? mode,
    String? hostAuthority,
  ) async => selection = mode;
  @override
  Future<String?> getDeviceId() async => null;
  @override
  Future<({String fingerprint, TlsMode mode})?> getPinnedCert(
    String hostInput, {
    String? deviceId,
    bool fallbackToPersistedIdentity = false,
  }) async => null;
  @override
  Future<({String cert, String key})?> getClientCertAndKey() async => identity;
  @override
  Future<({String op, String error, DateTime at})?>
  getLastStorageFailure() async => storageFailure;
  @override
  Future<String> getConnectMode() async => connectMode;
  @override
  Future<void> setConnectMode(String mode) async => connectMode = mode;
  @override
  Future<String?> getToken() async => token;
  @override
  Future<void> setToken(String value) async => token = value;
  @override
  Future<void> clearToken() async {
    clearTokenCalled = true;
    token = null;
  }

  @override
  Future<void> clearSecrets() async {
    clearSecretsCalled = true;
    token = null;
  }

  @override
  Future<void> clearFingerprint() async {
    clearFingerprintCalled = true;
  }
}

void main() {
  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  testWidgets('selecting a theme updates the provider and persists', (
    tester,
  ) async {
    // Appearance sits below the fold now that the hub leads with Providers
    // and Sessions (MADR 0082 D1).
    tester.view.physicalSize = const Size(1000, 6000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final store = _FakeStore();
    final container = ProviderContainer(
      overrides: [settingsStoreProvider.overrideWithValue(store)],
    );
    addTearDown(container.dispose);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(home: SettingsScreen()),
      ),
    );
    await tester.pumpAndSettle();

    expect(container.read(themeModeProvider), ThemeMode.system);

    await tester.tap(find.text('Dark'));
    await tester.pumpAndSettle();

    expect(container.read(themeModeProvider), ThemeMode.dark);
    expect(store.themeMode, 'dark');
  });

  testWidgets('toggling agent alerts persists the preference', (tester) async {
    final store = _FakeStore();
    final container = ProviderContainer(
      overrides: [settingsStoreProvider.overrideWithValue(store)],
    );
    addTearDown(container.dispose);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(home: SettingsScreen()),
      ),
    );
    await tester.pumpAndSettle();

    // Starts on (fake default), then turn it off.
    // There are several SwitchListTiles now (B3 kind toggles); hit the master.
    expect(store.notifications, isTrue);
    await tester.tap(find.widgetWithText(SwitchListTile, 'Agent alerts'));
    await tester.pumpAndSettle();
    expect(store.notifications, isFalse);
  });

  // ---------------------------------------------------------------------
  // MADR 0067 D3/D4 — the notifications copy must not promise a background
  // connection iOS does not have (0063: no simulated liveness).
  // ---------------------------------------------------------------------

  Future<void> pumpNotificationsSection(WidgetTester tester) async {
    // Tall viewport: the Notifications section sits below a phone-height
    // fold, where the lazy ListView culls it out of the widget tree.
    tester.view.physicalSize = const Size(1000, 6000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final container = ProviderContainer(
      overrides: [
        settingsStoreProvider.overrideWithValue(_FakeStore()),
        // The tall viewport builds the Route section too — fake its client
        // and probes so no real probe timers outlive the test.
        mcremoteClientProvider.overrideWithValue(_FakeClient()),
        transportProbesProvider.overrideWithValue(_FakeProbes()),
      ],
    );
    addTearDown(container.dispose);
    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(home: SettingsScreen()),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('iOS: alerts copy is honest about foreground-only', (
    tester,
  ) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.iOS;
    try {
      await pumpNotificationsSection(tester);

      expect(find.text('Alerts arrive while the app is open'), findsOneWidget);
      expect(
        find.textContaining('Keeps a background connection'),
        findsNothing,
      );
    } finally {
      // Must be reset before the test body returns: the binding's invariant
      // check runs before addTearDown callbacks do.
      debugDefaultTargetPlatformOverride = null;
    }
  });

  testWidgets('Android: alerts copy keeps the background-connection promise', (
    tester,
  ) async {
    await pumpNotificationsSection(tester);

    expect(
      find.textContaining('Keeps a background connection'),
      findsOneWidget,
    );
    expect(find.text('Alerts arrive while the app is open'), findsNothing);
  });

  // ---------------------------------------------------------------------
  // MADR 0062 D6 — Route section: probes, transport control, Reconnect now.
  // ---------------------------------------------------------------------

  Future<_FakeClient> pumpSettings(
    WidgetTester tester, {
    required _FakeStore store,
    required TransportProbes probes,
  }) async {
    // The settings list is long; the Route section sits below a phone-height
    // fold, where a lazy ListView culls it out of the widget tree entirely.
    tester.view.physicalSize = const Size(1000, 6000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final client = _FakeClient();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          settingsStoreProvider.overrideWithValue(store),
          mcremoteClientProvider.overrideWithValue(client),
          transportProbesProvider.overrideWithValue(probes),
        ],
        child: const MaterialApp(home: SettingsScreen()),
      ),
    );
    await tester.pumpAndSettle();
    return client;
  }

  testWidgets('a mesh-only host offers no transport controls', (tester) async {
    // Nothing to choose, so the screen says so plainly instead of presenting a
    // decision the user does not have.
    await pumpSettings(
      tester,
      store: _FakeStore(),
      probes: _FakeProbes(meshUp: true, relayUp: true),
    );

    expect(find.textContaining('Mesh only'), findsOneWidget);
    expect(find.byType(SegmentedButton<TransportMode>), findsNothing);
    expect(find.text('Reconnect now'), findsNothing);
  });

  testWidgets('both transports up: control and Reconnect now are offered', (
    tester,
  ) async {
    await pumpSettings(
      tester,
      store: _FakeStore(
        relayUrl: 'wss://relay.example:8443',
        relayHostId: 'macos-laptop',
        relayAuthority: '10.0.0.5:7531',
        sticky: TransportMode.mesh,
      ),
      probes: _FakeProbes(meshUp: true, relayUp: true),
    );

    expect(find.byType(SegmentedButton<TransportMode>), findsOneWidget);
    expect(find.text('Reconnect now'), findsOneWidget);
    expect(find.textContaining('Last connected over Mesh'), findsOneWidget);
    expect(find.textContaining('Mesh · up'), findsOneWidget);
    expect(find.textContaining('Relay · up'), findsOneWidget);
  });

  testWidgets('Reconnect now forces the selected transport', (tester) async {
    final store = _FakeStore(
      relayUrl: 'wss://relay.example:8443',
      relayHostId: 'macos-laptop',
      relayAuthority: '10.0.0.5:7531',
      sticky: TransportMode.mesh,
    );
    final client = await pumpSettings(
      tester,
      store: store,
      probes: _FakeProbes(meshUp: true, relayUp: true),
    );

    // "Relay" also names the route ListTile, so target the segment itself.
    await tester.tap(
      find.descendant(
        of: find.byType(SegmentedButton<TransportMode>),
        matching: find.text('Relay'),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Reconnect now'));
    await tester.pumpAndSettle();

    expect(client.reconnectCalls, 1);
    expect(client.lastTransport, TransportMode.relay);
    // A user action keeps its own fallback budget (amendment A5), so switching
    // transports can never strand the user on a path that turns out to be dead.
    expect(client.lastUserInitiated, isTrue);
    expect(store.selection, TransportMode.relay);
  });

  testWidgets('a transport that did not answer is shown, not hidden', (
    tester,
  ) async {
    await pumpSettings(
      tester,
      store: _FakeStore(
        relayUrl: 'wss://relay.example:8443',
        relayHostId: 'macos-laptop',
        relayAuthority: '10.0.0.5:7531',
      ),
      probes: _FakeProbes(meshUp: true, relayUp: false),
    );

    // "no answer" rather than "down": a ~900ms health check that went
    // unanswered is not a verdict on the transport (D2).
    expect(find.textContaining('Relay · no answer'), findsOneWidget);
    // No menu, because only one transport is available — but Reconnect now
    // stays, so the user can still act.
    expect(find.byType(SegmentedButton<TransportMode>), findsNothing);
    expect(find.text('Reconnect now'), findsOneWidget);
  });

  testWidgets('a relay paired to another host is not offered', (tester) async {
    // MADR 0046 M-2: a relay hint is credentials for one daemon.
    await pumpSettings(
      tester,
      store: _FakeStore(
        relayUrl: 'wss://relay.example:8443',
        relayHostId: 'macos-laptop',
        relayAuthority: '100.64.0.9:7531',
      ),
      probes: _FakeProbes(meshUp: true, relayUp: true),
    );

    expect(find.textContaining('Mesh only'), findsOneWidget);
    expect(find.byType(SegmentedButton<TransportMode>), findsNothing);
  });

  // ---------------------------------------------------------------------
  // MADR 0064 — Connect mode (D6) and the long-lived token entry (D4).
  // ---------------------------------------------------------------------

  testWidgets('Connect mode shows the stored value and persists a change', (
    tester,
  ) async {
    final store = _FakeStore();
    await pumpSettings(tester, store: store, probes: _FakeProbes());

    // Default surfaced as Auto.
    expect(find.textContaining('Auto — scan and connect'), findsOneWidget);

    await tester.tap(find.text('Connect mode'));
    await tester.pumpAndSettle();
    // The option row and the confirm button are both labelled Select now
    // that this is the shared picker (MADR 0082 D6).
    await tester.tap(find.widgetWithText(ListTile, 'Select'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Select'));
    await tester.pumpAndSettle();

    expect(store.connectMode, 'select');
    expect(find.textContaining('Select — choose a transport'), findsOneWidget);
  });

  // "present"/"absent" also captions the Client identity tile, so the token
  // assertions must be scoped to their own tile.
  Finder tokenSubtitle(String text) => find.descendant(
    of: find.widgetWithText(ListTile, 'Long-lived token'),
    matching: find.text(text),
  );

  testWidgets('token dialog saves to the store and the subtitle flips', (
    tester,
  ) async {
    final store = _FakeStore();
    await pumpSettings(tester, store: store, probes: _FakeProbes());

    expect(tokenSubtitle('absent'), findsOneWidget);

    await tester.tap(find.text('Long-lived token'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'mcr_longlived');
    await tester.tap(find.text('Save'));
    await tester.pumpAndSettle();

    expect(store.token, 'mcr_longlived');
    expect(tokenSubtitle('present'), findsOneWidget);
  });

  testWidgets('clear icon empties the field; empty save clears the token', (
    tester,
  ) async {
    final store = _FakeStore()..token = 'mcr_old';
    await pumpSettings(tester, store: store, probes: _FakeProbes());

    expect(tokenSubtitle('present'), findsOneWidget);

    await tester.tap(find.text('Long-lived token'));
    await tester.pumpAndSettle();
    // The field is prefilled, so the clear affordance is offered.
    await tester.tap(find.byTooltip('Clear'));
    await tester.pumpAndSettle();
    expect(find.byTooltip('Clear'), findsNothing);
    await tester.tap(find.text('Save'));
    await tester.pumpAndSettle();

    expect(store.clearTokenCalled, isTrue);
    expect(store.token, isNull);
    expect(tokenSubtitle('absent'), findsOneWidget);
  });

  testWidgets('Cancel leaves the stored token untouched', (tester) async {
    final store = _FakeStore()..token = 'mcr_old';
    await pumpSettings(tester, store: store, probes: _FakeProbes());

    await tester.tap(find.text('Long-lived token'));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('Clear'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();

    expect(store.clearTokenCalled, isFalse);
    expect(store.token, 'mcr_old');
    expect(tokenSubtitle('present'), findsOneWidget);
  });

  testWidgets('Re-pair this host runs the scoped secret reset and returns '
      'to the connect screen (MADR 0066 D4/F12)', (tester) async {
    tester.view.physicalSize = const Size(1000, 6000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final store = _FakeStore()..token = 'mcr_old';
    final client = _FakeClient();
    // context.go('/') needs a router; mirror the app's shape with a stub '/'.
    final router = GoRouter(
      initialLocation: '/settings',
      routes: [
        GoRoute(
          path: '/',
          builder: (context, state) =>
              const Scaffold(body: Text('connect-stub')),
        ),
        GoRoute(
          path: '/settings',
          builder: (context, state) => const SettingsScreen(),
        ),
      ],
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          settingsStoreProvider.overrideWithValue(store),
          mcremoteClientProvider.overrideWithValue(client),
          transportProbesProvider.overrideWithValue(_FakeProbes()),
        ],
        child: MaterialApp.router(routerConfig: router),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('Re-pair this host'));
    await tester.pumpAndSettle();
    // The dialog names both recovery cases, not just certificate rotation.
    expect(find.textContaining('rejects this phone\'s key'), findsOneWidget);
    await tester.tap(find.widgetWithText(FilledButton, 'Re-pair'));
    await tester.pumpAndSettle();

    // The scoped reset plus — only on this rotation tile — the pin, and
    // the tap really lands back on the connect screen.
    expect(store.clearSecretsCalled, isTrue);
    expect(store.clearFingerprintCalled, isTrue);
    expect(client.clearMemoryCalled, isTrue);
    expect(find.text('connect-stub'), findsOneWidget);
  });

  // ---------------------------------------------------------------------
  // MADR 0066 D5/D9 — Storage diagnostics and identity fingerprint.
  // ---------------------------------------------------------------------

  testWidgets('Secret storage row reads clean with no recorded failure', (
    tester,
  ) async {
    await pumpSettings(tester, store: _FakeStore(), probes: _FakeProbes());

    expect(find.text('Secret storage'), findsOneWidget);
    expect(find.text('No failures recorded'), findsOneWidget);
  });

  testWidgets('Secret storage row surfaces the recorded platform failure', (
    tester,
  ) async {
    final store = _FakeStore()
      ..storageFailure = (
        op: 'write',
        error: 'PlatformException(KeyStore error -38)',
        at: DateTime.utc(2026, 8, 2, 12, 30),
      );
    await pumpSettings(tester, store: store, probes: _FakeProbes());

    // The timestamp renders in local time, so only op + error are pinned.
    expect(find.textContaining('write failed'), findsOneWidget);
    expect(find.textContaining('KeyStore error -38'), findsOneWidget);
  });

  testWidgets('Client identity tile shows the enrolled SPKI fingerprint and '
      'copies it on long-press', (tester) async {
    final id = ClientIdentity.generate();
    final fp = spkiFingerprintOfKeyPem(id.keyPem);
    final store = _FakeStore()..identity = (cert: id.certPem, key: id.keyPem);

    String? copied;
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform,
      (call) async {
        if (call.method == 'Clipboard.setData') {
          copied = (call.arguments as Map)['text'] as String?;
        }
        return null;
      },
    );
    addTearDown(
      () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        null,
      ),
    );

    await pumpSettings(tester, store: store, probes: _FakeProbes());

    expect(find.text(fp), findsOneWidget);
    await tester.longPress(find.text(fp));
    await tester.pumpAndSettle();
    expect(copied, fp);
    expect(find.text('Fingerprint copied'), findsOneWidget);
    // Drain the top-notification timer.
    await tester.pumpAndSettle(const Duration(seconds: 8));
  });

  testWidgets('Client identity tile stays "absent" without an identity', (
    tester,
  ) async {
    await pumpSettings(tester, store: _FakeStore(), probes: _FakeProbes());
    final tile = find.ancestor(
      of: find.text('Client identity'),
      matching: find.byType(ListTile),
    );
    expect(
      find.descendant(of: tile, matching: find.text('absent')),
      findsOneWidget,
    );
  });

  testWidgets('an unparseable stored key renders as unreadable, not a crash', (
    tester,
  ) async {
    final store = _FakeStore()
      ..identity = (cert: 'not-a-cert', key: 'not-a-key');
    await pumpSettings(tester, store: store, probes: _FakeProbes());

    final tile = find.ancestor(
      of: find.text('Client identity'),
      matching: find.byType(ListTile),
    );
    expect(
      find.descendant(of: tile, matching: find.text('unreadable')),
      findsOneWidget,
    );
  });

  // ---------------------------------------------------------------------
  // MADR 0082 P1 — provider credential rows: one semantic chip per state,
  // and a confirmation before removal (the F5 fix).
  // ---------------------------------------------------------------------

  ProviderInfo kiloWith(List<UpstreamAuth> ups, {String? active}) =>
      ProviderInfo(
        id: 'kilo',
        ready: true,
        auth: ProviderAuthInfo(
          status: AuthStatus.configured,
          activeUpstream: active,
          upstreams: ups,
        ),
      );

  Future<_AuthClient> pumpProviderSection(
    WidgetTester tester, {
    required List<ProviderInfo> providers,
  }) async {
    tester.view.physicalSize = const Size(1000, 6000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    // _load() awaits PackageInfo.fromPlatform() before it reaches
    // listProviders, and the real channel never answers under flutter_test.
    PackageInfo.setMockInitialValues(
      appName: 'mcremote',
      packageName: 'dev.mcremote',
      version: '0.0.0',
      buildNumber: '1',
      buildSignature: '',
      installTime: null,
      updateTime: null,
    );
    final client = _AuthClient(providers);
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          settingsStoreProvider.overrideWithValue(_FakeStore()),
          mcremoteClientProvider.overrideWithValue(client),
          transportProbesProvider.overrideWithValue(_FakeProbes()),
          // The real coordinator's osBlocked() waits on the notifications
          // plugin channel, which never answers under flutter_test — and
          // _load() awaits it before it ever reaches listProviders.
          notificationCoordinatorProvider.overrideWith(
            (ref) => _FakeCoordinator(client: client),
          ),
        ],
        child: const MaterialApp(home: SettingsScreen()),
      ),
    );
    await tester.pumpAndSettle();
    return client;
  }

  testWidgets('the spoke summarises the fleet and flags the first anomaly', (
    tester,
  ) async {
    await pumpProviderSection(
      tester,
      providers: [
        kiloWith([
          const UpstreamAuth(
            id: 'together',
            label: 'Together AI',
            status: AuthStatus.configured,
          ),
          const UpstreamAuth(
            id: 'deepseek',
            label: 'DeepSeek',
            status: AuthStatus.quota,
          ),
        ], active: 'together'),
      ],
    );

    final spoke = find.byKey(const Key('settings-providers-spoke'));
    expect(spoke, findsOneWidget);
    expect(
      find.descendant(
        of: spoke,
        matching: find.text('1 of 1 agents ready · kilo quota reached'),
      ),
      findsOneWidget,
    );
    // The rows themselves live on the detail screen now (MADR 0082 D3).
    expect(
      find.byKey(const Key('provider-auth-tile-kilo-together')),
      findsNothing,
    );
  });

  testWidgets('a connected host with no providers shows no spoke', (
    tester,
  ) async {
    await pumpProviderSection(tester, providers: const []);
    expect(find.byKey(const Key('settings-providers-spoke')), findsNothing);
  });

  testWidgets('disconnected: the spoke stays, and says to connect', (
    tester,
  ) async {
    await pumpSettings(tester, store: _FakeStore(), probes: _FakeProbes());
    final spoke = find.byKey(const Key('settings-providers-spoke'));
    expect(spoke, findsOneWidget);
    expect(
      find.descendant(
        of: spoke,
        matching: find.text('Connect to manage providers'),
      ),
      findsOneWidget,
    );
  });

  testWidgets('the spoke navigates to the Providers screen', (tester) async {
    tester.view.physicalSize = const Size(1000, 6000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    PackageInfo.setMockInitialValues(
      appName: 'mcremote',
      packageName: 'dev.mcremote',
      version: '0.0.0',
      buildNumber: '1',
      buildSignature: '',
      installTime: null,
      updateTime: null,
    );
    final client = _AuthClient([ProviderInfo(id: 'kilo', ready: true)]);
    final router = GoRouter(
      initialLocation: '/settings',
      routes: [
        GoRoute(
          path: '/settings',
          builder: (_, _) => const SettingsScreen(),
          routes: [
            GoRoute(
              path: 'providers',
              builder: (_, _) => const ProvidersScreen(),
            ),
          ],
        ),
      ],
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          settingsStoreProvider.overrideWithValue(_FakeStore()),
          mcremoteClientProvider.overrideWithValue(client),
          transportProbesProvider.overrideWithValue(_FakeProbes()),
          notificationCoordinatorProvider.overrideWith(
            (ref) => _FakeCoordinator(client: client),
          ),
        ],
        child: MaterialApp.router(routerConfig: router),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('settings-providers-spoke')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('provider-card-kilo')), findsOneWidget);
  });
}

class _FakeProbes extends TransportProbes {
  _FakeProbes({this.meshUp = false, this.relayUp = false});

  final bool meshUp;
  final bool relayUp;

  @override
  Future<bool> mesh(
    String hostInput, {
    Duration timeout = kTransportProbeTimeout,
  }) async => meshUp;

  @override
  Future<bool> relay(
    String relayBase, {
    Duration timeout = kTransportProbeTimeout,
  }) async => relayUp;
}

class _FakeClient extends McremoteClient {
  bool clearMemoryCalled = false;

  @override
  void clearMemoryCredentials({bool host = false}) {
    clearMemoryCalled = true;
  }

  int reconnectCalls = 0;
  TransportMode? lastTransport;
  bool? lastUserInitiated;

  @override
  McConnectionState get state => McConnectionState.disconnected;

  @override
  Future<void> reconnectFromStore(
    SettingsStore store, {
    TransportMode? transport,
    bool userInitiated = false,
  }) async {
    reconnectCalls++;
    lastTransport = transport;
    lastUserInitiated = userInitiated;
  }

  @override
  Future<void> disconnect({bool manual = true}) async {}
}

/// A coordinator whose OS probes answer immediately: the real one's
/// osBlocked() rides the notifications plugin channel, which never answers
/// under flutter_test.
class _FakeCoordinator extends NotificationCoordinator {
  _FakeCoordinator({required super.client});

  @override
  Future<bool?> osBlocked() async => false;

  @override
  Object? get notificationsUnavailable => null;
}

/// A connected client reporting a fixed provider list, recording credential
/// removals (MADR 0082 P1).
class _AuthClient extends _FakeClient {
  _AuthClient(this.providers);

  final List<ProviderInfo> providers;
  final removed = <(String, String)>[];

  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<List<ProviderInfo>> listProviders() async => providers;

  @override
  Future<void> clearProviderCredential({
    required String providerId,
    required String upstreamId,
  }) async {
    removed.add((providerId, upstreamId));
  }
}

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart' show TlsMode;
import 'package:magic_cli_remote/data/ws/transport_probes.dart';
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
  Future<({String cert, String key})?> getClientCertAndKey() async => null;
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
}

void main() {
  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  testWidgets('selecting a theme updates the provider and persists', (
    tester,
  ) async {
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
    await tester.tap(find.text('Select'));
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

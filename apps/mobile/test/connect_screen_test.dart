import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/ws/transport_probes.dart';
import 'package:magic_cli_remote/features/connect/connect_screen.dart';
import 'package:magic_cli_remote/features/settings/settings_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';

/// In-memory [SettingsStore] so tests never touch the platform keystore.
class FakeSettingsStore extends SettingsStore {
  FakeSettingsStore({this.host, this.token, this.connectMode = 'auto'});

  String? host;
  String? token;

  /// MADR 0064 D6. Defaults to 'auto', matching production — tests that pin
  /// the deferral must opt into 'select' explicitly (plan F1/F2).
  String connectMode;
  String? deviceId;
  bool clearTokenCalled = false;
  bool clearAllCalled = false;
  bool clearSecretsCalled = false;
  String? relayUrl;
  String? relayHostId;
  String? relayAuthority;

  /// MADR 0066 D2. Defaults to ok so existing tests see no banner; the
  /// real probe would hit the platform keystore, which tests must not.
  SecretStoreHealth probeResult = SecretStoreHealth.ok;
  bool probeCalled = false;

  @override
  Future<SecretStoreHealth> probeSecretStore() async {
    probeCalled = true;
    return probeResult;
  }

  @override
  Future<void> clearSecrets() async {
    clearSecretsCalled = true;
    token = null;
  }

  @override
  Future<void> setRelayRoute({
    required String? url,
    required String? hostId,
    required String? authority,
  }) async {
    relayUrl = url;
    relayHostId = hostId;
    relayAuthority = authority;
  }

  @override
  Future<String?> getRelayUrl() async => relayUrl;

  @override
  Future<String?> getRelayHostId() async => relayHostId;

  @override
  Future<String?> getRelayAuthority() async => relayAuthority;

  @override
  Future<String?> getHost() async => host;

  @override
  Future<String?> getToken() async => token;

  @override
  Future<void> setHost(String host) async => this.host = host;

  @override
  Future<void> setToken(String token) async => this.token = token;

  @override
  Future<void> setDeviceId(String id) async => deviceId = id;

  @override
  Future<void> clearToken() async {
    clearTokenCalled = true;
    token = null;
  }

  @override
  Future<void> clearAll() async {
    clearAllCalled = true;
    host = null;
    token = null;
    deviceId = null;
  }

  @override
  Future<String> getConnectMode() async => connectMode;

  @override
  Future<void> setConnectMode(String mode) async => connectMode = mode;
}

/// Scriptable [McremoteClient] that never opens a socket.
class FakeMcremoteClient extends McremoteClient {
  FakeMcremoteClient({
    this.stateValue = McConnectionState.disconnected,
    this.pairedValue = false,
    this.loggedOutValue = false,
    this.connectError,
    this.connectDelay,
    this.claimError,
    this.spentCredential = false,
    this.healthzBody = 'ok',
  });

  McConnectionState stateValue;
  bool pairedValue;
  bool loggedOutValue;
  Object? connectError;

  /// Stands in for a DialEpisode that is still working through its legs.
  final Duration? connectDelay;
  Object? claimError;

  /// Stands in for a claim that reached the host before failing (A1).
  bool spentCredential;
  String healthzBody;

  int connectCalls = 0;
  int claimCalls = 0;
  bool clearMemoryCalled = false;
  String? lastRelayUrl;
  String? lastRelayHostId;
  String? lastFingerprint;
  TransportMode? lastTransport;
  bool? lastUserInitiated;

  @override
  McConnectionState get state => stateValue;

  @override
  bool get isPaired => pairedValue;

  @override
  bool get userLoggedOut => loggedOutValue;

  @override
  Future<void> connect({
    required String hostInput,
    required String token,
    String? fingerprint,
    TlsMode? mode,
    String? relayUrl,
    String? relayHostId,
    bool enableAutoReconnect = true,
    TransportMode? transport,
    bool allowTransportFallback = true,
    bool userInitiated = true,
  }) async {
    connectCalls++;
    lastUserInitiated = userInitiated;
    if (connectDelay != null) await Future<void>.delayed(connectDelay!);
    lastRelayUrl = relayUrl;
    lastRelayHostId = relayHostId;
    lastFingerprint = fingerprint;
    lastTransport = transport;
    if (connectError != null) throw connectError!;
  }

  @override
  Future<String> claimPairCode({
    required String hostInput,
    required String code,
    String? name,
    String? fingerprint,
    TlsMode? mode,
    String? relayUrl,
    String? relayHostId,
    TransportMode? transport,
    bool allowTransportFallback = true,
  }) async {
    claimCalls++;
    lastRelayUrl = relayUrl;
    lastRelayHostId = relayHostId;
    lastFingerprint = fingerprint;
    lastTransport = transport;
    if (claimError != null) throw claimError!;
    return 'mcr_claimed';
  }

  @override
  bool get lastDialSpentCredential => spentCredential;

  @override
  Future<String> healthz(
    String hostInput, {
    String? fingerprint,
    TlsMode? mode,
  }) async => healthzBody;

  @override
  void clearMemoryCredentials({bool host = false}) {
    clearMemoryCalled = true;
  }

  @override
  Future<void> disconnect({bool manual = true}) async {}
}

/// Deterministic transport probes.
///
/// Without this the connect screen dials the machine running the tests: the
/// real probes open sockets, which a widget test both cannot satisfy and must
/// not depend on (MADR 0062 D2 probe-injection seam).
class FakeProbes extends TransportProbes {
  FakeProbes({this.meshUp = false, this.relayUp = false});

  final bool meshUp;
  final bool relayUp;
  int meshCalls = 0;
  int relayCalls = 0;

  @override
  Future<bool> mesh(
    String hostInput, {
    Duration timeout = kTransportProbeTimeout,
  }) async {
    meshCalls++;
    return meshUp;
  }

  @override
  Future<bool> relay(
    String relayBase, {
    Duration timeout = kTransportProbeTimeout,
  }) async {
    relayCalls++;
    return relayUp;
  }
}

Widget _wrap({
  required SettingsStore store,
  required McremoteClient client,
  TransportProbes? probes,
}) {
  return ProviderScope(
    overrides: [
      settingsStoreProvider.overrideWithValue(store),
      mcremoteClientProvider.overrideWithValue(client),
      transportProbesProvider.overrideWithValue(probes ?? FakeProbes()),
    ],
    child: const MaterialApp(home: ConnectScreen()),
  );
}

/// Gives the test a phone-tall surface so the scrollable connect form fits
/// without the lazy [ListView] culling or bottom-clipping its lower controls.
/// The default 800×600 harness is too short once the header logo is present —
/// buttons then sit below the fold where tap hit-tests and finders miss them.
void _useTallSurface(WidgetTester tester) {
  tester.view.physicalSize = const Size(1000, 2200);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
}

void main() {
  setUp(() {
    // The V9 test pumps the real SettingsScreen, whose non-faked preference
    // reads go through the platform SharedPreferences channel.
    SharedPreferences.setMockInitialValues({});
  });

  testWidgets('renders the core pairing affordances', (tester) async {
    await tester.pumpWidget(
      _wrap(store: FakeSettingsStore(), client: FakeMcremoteClient()),
    );
    await tester.pumpAndSettle();

    expect(find.text('Connect to your machine - Steps'), findsOneWidget);
    // Collapsed by default (D1): the instruction body is not on screen.
    expect(find.textContaining('On the host running mcremote'), findsNothing);
    expect(find.text('Scan QR'), findsOneWidget);
    expect(find.text('Enter code'), findsOneWidget);
    expect(find.text('Paste URI / code / token'), findsOneWidget);
    expect(find.text('Test healthz'), findsOneWidget);
    expect(find.text('Connect'), findsOneWidget);
    // Host field prefilled with the platform default; token empty by default.
    expect(find.widgetWithText(TextField, 'Host (mcremote)'), findsOneWidget);
    // The long-lived token lives in Settings now (MADR 0064 D4).
    expect(find.text('Advanced: long-lived token'), findsNothing);
    expect(find.widgetWithText(TextField, 'Device token'), findsNothing);
  });

  testWidgets('the steps disclosure opens on demand', (tester) async {
    await tester.pumpWidget(
      _wrap(store: FakeSettingsStore(), client: FakeMcremoteClient()),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('Connect to your machine - Steps'));
    await tester.pumpAndSettle();

    expect(
      find.text('mcremote pair code --name <name of device>'),
      findsOneWidget,
    );
  });

  testWidgets('Connect sits above the fold at 640 dp (V1)', (tester) async {
    // The whole point of MADR 0064: the action that completes pairing must
    // not be the thing you scroll to find. 360×640 is the short-phone floor.
    tester.view.physicalSize = const Size(360, 640);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      _wrap(store: FakeSettingsStore(), client: FakeMcremoteClient()),
    );
    await tester.pumpAndSettle();

    // Found at all ⇒ the lazy ListView laid it out inside the viewport.
    final connect = find.widgetWithText(FilledButton, 'Connect');
    expect(connect, findsOneWidget);
    expect(
      tester.getRect(connect).bottom,
      lessThanOrEqualTo(640),
      reason: 'V1: Connect must be visible without scrolling',
    );
  });

  testWidgets('Settings is reachable while unpaired (V9)', (tester) async {
    _useTallSurface(tester);
    final router = GoRouter(
      routes: [
        GoRoute(path: '/', builder: (_, _) => const ConnectScreen()),
        GoRoute(path: '/settings', builder: (_, _) => const SettingsScreen()),
      ],
    );
    addTearDown(router.dispose);
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          settingsStoreProvider.overrideWithValue(FakeSettingsStore()),
          mcremoteClientProvider.overrideWithValue(FakeMcremoteClient()),
          transportProbesProvider.overrideWithValue(FakeProbes()),
        ],
        child: MaterialApp.router(routerConfig: router),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byType(PopupMenuButton<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Settings'));
    await tester.pumpAndSettle();

    // The door exists (D4a), and what moved here is actually behind it.
    expect(find.byType(SettingsScreen), findsOneWidget);
    expect(find.text('Long-lived token'), findsOneWidget);
  });

  testWidgets('debug builds prefill Host with the emulator loopback', (
    tester,
  ) async {
    await tester.pumpWidget(
      _wrap(store: FakeSettingsStore(), client: FakeMcremoteClient()),
    );
    await tester.pumpAndSettle();

    // flutter test always runs in debug mode; the kDebugMode gate means a
    // release first run starts empty instead of health-checking a dead
    // loopback. Tests run on the host VM, so the non-Android default applies.
    final host = tester.widget<TextField>(
      find.widgetWithText(TextField, 'Host (mcremote)'),
    );
    expect(host.controller?.text, '127.0.0.1:7531');
  });

  testWidgets(
    'pasting a relay pair URI populates Host and Relay and passes both',
    (tester) async {
      _useTallSurface(tester);
      const fp = 'AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA';
      const pairUri =
          'mcremote://pair?host=wss%3A%2F%2F100.64.0.3%3A7531&fp=$fp'
          '&token=mcr_relay&relay=wss%3A%2F%2Fheadscale.example%3A8443'
          '&hid=macos-laptop';
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        (call) async => call.method == 'Clipboard.getData'
            ? <String, dynamic>{'text': pairUri}
            : null,
      );
      addTearDown(
        () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          SystemChannels.platform,
          null,
        ),
      );

      final store = FakeSettingsStore();
      final client = FakeMcremoteClient();
      await tester.pumpWidget(_wrap(store: store, client: client));
      await tester.pumpAndSettle();

      await tester.tap(find.text('Paste URI / code / token'));
      await tester.pumpAndSettle();

      expect(client.connectCalls, 1);
      // Attempt-scoped relay from the QR must reach the client (not mesh-only).
      expect(client.lastRelayUrl, 'wss://headscale.example:8443');
      expect(client.lastRelayHostId, 'macos-laptop');
      // And be persisted under the mcremote authority for reconnects.
      expect(store.relayUrl, 'wss://headscale.example:8443');
      expect(store.relayHostId, 'macos-laptop');
      expect(store.relayAuthority, '100.64.0.3:7531');
    },
  );

  testWidgets(
    'a failed pairing does not lend its relay route to the next host',
    (tester) async {
      _useTallSurface(tester);
      // Daemon A's QR: token plus a relay tunnel that belongs to A alone.
      // A secure host must carry a pin, so this QR has one — which makes it
      // the full set of per-daemon hints the next attempt must not reuse.
      const fp = 'AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA';
      const pairUri =
          'mcremote://pair?host=wss%3A%2F%2F100.64.0.1%3A7531&fp=$fp'
          '&token=mcr_a&relay=wss%3A%2F%2Frelay.a.example&hid=host-a';
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        (call) async => call.method == 'Clipboard.getData'
            ? <String, dynamic>{'text': pairUri}
            : null,
      );
      addTearDown(
        () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          SystemChannels.platform,
          null,
        ),
      );

      final store = FakeSettingsStore();
      final client = FakeMcremoteClient(
        connectError: McException('nope', code: 'unauthorized'),
      );
      await tester.pumpWidget(_wrap(store: store, client: client));
      await tester.pumpAndSettle();

      await tester.tap(find.text('Paste URI / code / token'));
      await tester.pumpAndSettle();
      expect(client.connectCalls, 1);
      expect(client.lastRelayUrl, 'wss://relay.a.example');

      // The attempt failed. The user now points the Host field at a different
      // daemon and retries.
      await tester.enterText(
        find.widgetWithText(TextField, 'Host (mcremote)'),
        'wss://100.64.0.9:7531',
      );
      await tester.pumpAndSettle();
      client.connectError = null;
      await tester.tap(find.widgetWithText(FilledButton, 'Connect'));
      await tester.pumpAndSettle();

      expect(client.connectCalls, 2);
      // Sending B's token down A's tunnel, then filing A's route under B's
      // authority, poisoned every later off-mesh reconnect (MADR 0046 M-2).
      expect(client.lastRelayUrl, isNull);
      expect(client.lastRelayHostId, isNull);
      expect(client.lastFingerprint, isNull);
      expect(store.relayUrl, isNull);
    },
  );

  testWidgets('Connect with an empty token shows a validation error and does '
      'not call the client', (tester) async {
    _useTallSurface(tester);
    final client = FakeMcremoteClient();
    await tester.pumpWidget(_wrap(store: FakeSettingsStore(), client: client));
    await tester.pumpAndSettle();

    await tester.tap(find.widgetWithText(FilledButton, 'Connect'));
    await tester.pumpAndSettle();

    expect(find.textContaining('Host and token required'), findsOneWidget);
    expect(client.connectCalls, 0);
  });

  testWidgets('Test healthz surfaces the reachable body', (tester) async {
    _useTallSurface(tester);
    final client = FakeMcremoteClient(healthzBody: 'mcremote ok');
    await tester.pumpWidget(_wrap(store: FakeSettingsStore(), client: client));
    await tester.pumpAndSettle();

    await tester.tap(find.widgetWithText(OutlinedButton, 'Test healthz'));
    await tester.pumpAndSettle();

    expect(find.textContaining('Reachable: mcremote ok'), findsOneWidget);
  });

  testWidgets('auto-connect on a bad saved token clears the token and offers '
      're-pair without navigating away', (tester) async {
    _useTallSurface(tester);
    final store = FakeSettingsStore(host: '10.0.0.5:7531', token: 'mcr_stale');
    final client = FakeMcremoteClient(
      connectError: McException('bad token', code: 'invalid_token'),
    );

    await tester.pumpWidget(_wrap(store: store, client: client));
    // initState kicks off _load(); let the auto-connect attempt resolve.
    await tester.pumpAndSettle();

    // The saved credentials were tried exactly once.
    expect(client.connectCalls, 1);
    // An invalid token is purged from both memory and the store.
    expect(client.clearMemoryCalled, isTrue);
    expect(store.clearTokenCalled, isTrue);
    // The re-pair guidance is shown, and we stayed on the connect screen.
    expect(find.textContaining('no longer valid'), findsOneWidget);
    expect(
      find.text('Host kept. Use Enter code or Scan QR to re-pair.'),
      findsOneWidget,
    );
    expect(find.text('Connect to your machine - Steps'), findsOneWidget);
  });

  testWidgets('a logged-out client skips auto-connect entirely', (
    tester,
  ) async {
    final store = FakeSettingsStore(host: '10.0.0.5:7531', token: 'mcr_ok');
    final client = FakeMcremoteClient(loggedOutValue: true);

    await tester.pumpWidget(_wrap(store: store, client: client));
    await tester.pumpAndSettle();

    expect(client.connectCalls, 0);
    expect(find.text('Connect to your machine - Steps'), findsOneWidget);
  });

  // ---------------------------------------------------------------------
  // MADR 0062 D5 — transport selection on the connect screen.
  // ---------------------------------------------------------------------

  /// A relay pair URI on the clipboard, ready to paste.
  void clipboardPairUri(WidgetTester tester, String uri) {
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform,
      (call) async => call.method == 'Clipboard.getData'
          ? <String, dynamic>{'text': uri}
          : null,
    );
    addTearDown(
      () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        null,
      ),
    );
  }

  const fp = 'AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA';
  const dualTokenUri =
      'mcremote://pair?host=wss%3A%2F%2F100.64.0.3%3A7531&fp=$fp'
      '&token=mcr_relay&relay=wss%3A%2F%2Fheadscale.example%3A8443'
      '&hid=macos-laptop';
  const dualCodeUri =
      'mcremote://pair?host=wss%3A%2F%2F100.64.0.3%3A7531&fp=$fp'
      '&code=K7M2-9X4P&relay=wss%3A%2F%2Fheadscale.example%3A8443'
      '&hid=macos-laptop';

  testWidgets('Select: the menu appears and a code is not dialled '
      'until Connect (V4)', (tester) async {
    _useTallSurface(tester);
    clipboardPairUri(tester, dualCodeUri);
    final client = FakeMcremoteClient();

    await tester.pumpWidget(
      _wrap(
        store: FakeSettingsStore(connectMode: 'select'),
        client: client,
        probes: FakeProbes(meshUp: true, relayUp: true),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    // This is the regression 0062 exists to fix: the old screen claimed
    // immediately, so the menu below could never be reached. Under 0064 D6
    // the pause is Select-mode-only, and only for one-shot codes.
    expect(client.claimCalls, 0);
    expect(client.connectCalls, 0);
    expect(find.byType(SegmentedButton<TransportMode>), findsOneWidget);
    expect(find.text('Mesh'), findsOneWidget);
    expect(find.text('Relay'), findsOneWidget);
    expect(find.textContaining('choose one, then Connect'), findsOneWidget);
  });

  testWidgets(
    'Select: Connect with no pick claims over the default Mesh (V3)',
    (tester) async {
      _useTallSurface(tester);
      clipboardPairUri(tester, dualCodeUri);
      final client = FakeMcremoteClient();

      await tester.pumpWidget(
        _wrap(
          store: FakeSettingsStore(connectMode: 'select'),
          client: client,
          probes: FakeProbes(meshUp: true, relayUp: true),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('Paste URI / code / token'));
      await tester.pumpAndSettle();

      // Connect is not gated on a transport pick (D2 invariant): with both up
      // and nothing chosen, the claim rides the mesh default.
      await tester.tap(find.widgetWithText(FilledButton, 'Claim & connect'));
      await tester.pumpAndSettle();

      expect(client.claimCalls, 1);
      expect(client.lastTransport, TransportMode.mesh);
    },
  );

  testWidgets('Select: picking Relay steers the claim', (tester) async {
    _useTallSurface(tester);
    clipboardPairUri(tester, dualCodeUri);
    final client = FakeMcremoteClient();

    await tester.pumpWidget(
      _wrap(
        store: FakeSettingsStore(connectMode: 'select'),
        client: client,
        probes: FakeProbes(meshUp: true, relayUp: true),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Relay'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Claim & connect'));
    await tester.pumpAndSettle();

    expect(client.lastTransport, TransportMode.relay);
  });

  testWidgets('Auto: a dual-available code claims immediately over mesh (V5)', (
    tester,
  ) async {
    _useTallSurface(tester);
    clipboardPairUri(tester, dualCodeUri);
    final client = FakeMcremoteClient();

    await tester.pumpWidget(
      _wrap(
        // 'auto' is the FakeSettingsStore default, as in production; spelled
        // out because this test is *about* the mode.
        store: FakeSettingsStore(connectMode: 'auto'),
        client: client,
        probes: FakeProbes(meshUp: true, relayUp: true),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    // Auto is declining to pause: the claim is already away, over the mesh
    // default, with the episode's relay fallback intact (D6).
    expect(client.claimCalls, 1);
    expect(client.connectCalls, 0);
    expect(client.lastTransport, TransportMode.mesh);
  });

  testWidgets('a dual-available *token* QR never pauses — auto (V7)', (
    tester,
  ) async {
    _useTallSurface(tester);
    clipboardPairUri(tester, dualTokenUri);
    final client = FakeMcremoteClient();

    await tester.pumpWidget(
      _wrap(
        store: FakeSettingsStore(connectMode: 'auto'),
        client: client,
        probes: FakeProbes(meshUp: true, relayUp: true),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    // A token is idempotent — there is nothing the pause could protect.
    expect(client.connectCalls, 1);
    expect(client.claimCalls, 0);
    expect(client.lastTransport, TransportMode.mesh);
  });

  testWidgets('a dual-available *token* QR never pauses — select (V7)', (
    tester,
  ) async {
    _useTallSurface(tester);
    clipboardPairUri(tester, dualTokenUri);
    final client = FakeMcremoteClient();

    await tester.pumpWidget(
      _wrap(
        store: FakeSettingsStore(connectMode: 'select'),
        client: client,
        probes: FakeProbes(meshUp: true, relayUp: true),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    // Select's deferral is scoped to codes (D6): pausing an idempotent token
    // for a transport choice protects nothing.
    expect(client.connectCalls, 1);
    expect(client.claimCalls, 0);
    expect(client.lastTransport, TransportMode.mesh);
  });

  testWidgets('a deferred QR code is claimed by Connect, not lost', (
    tester,
  ) async {
    // The regression a user hit on hardware: scanning a code-carrying QR with
    // both transports up deferred the dial (correct, D5 3a) but dropped the
    // code, and Connect — which needs a *token* — answered "Host and token
    // required". Entering the same code by hand worked, because that path
    // never goes through _applyPair. The QR looked broken.
    _useTallSurface(tester);
    clipboardPairUri(tester, dualCodeUri);
    final client = FakeMcremoteClient();

    await tester.pumpWidget(
      _wrap(
        // The deferral is Select-mode-only since 0064 D6.
        store: FakeSettingsStore(connectMode: 'select'),
        client: client,
        probes: FakeProbes(meshUp: true, relayUp: true),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    expect(client.claimCalls, 0, reason: 'the code waits for a transport');

    // The button says what it will do, so a held code does not read as a
    // scan that silently failed.
    expect(
      find.widgetWithText(FilledButton, 'Claim & connect'),
      findsOneWidget,
    );

    await tester.tap(find.text('Relay'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Claim & connect'));
    await tester.pumpAndSettle();

    expect(client.claimCalls, 1, reason: 'Connect must claim the held code');
    expect(client.connectCalls, 0, reason: 'a code is claimed, not connected');
    expect(client.lastTransport, TransportMode.relay);
    expect(
      find.textContaining('Host and token required'),
      findsNothing,
      reason: 'the scanned code must not be dropped',
    );
  });

  testWidgets('relay only: auto-connects over the relay with no menu', (
    tester,
  ) async {
    _useTallSurface(tester);
    clipboardPairUri(tester, dualTokenUri);
    final client = FakeMcremoteClient();

    await tester.pumpWidget(
      _wrap(
        store: FakeSettingsStore(),
        client: client,
        // Off-mesh: this is the case that must not pay an 8s mesh timeout.
        probes: FakeProbes(meshUp: false, relayUp: true),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    expect(client.connectCalls, 1);
    expect(client.lastTransport, TransportMode.relay);
    expect(find.byType(SegmentedButton<TransportMode>), findsNothing);
    expect(find.text('Using Relay'), findsOneWidget);
  });

  testWidgets('mesh only: auto-connects over the mesh with no menu', (
    tester,
  ) async {
    _useTallSurface(tester);
    clipboardPairUri(tester, dualTokenUri);
    final client = FakeMcremoteClient();

    await tester.pumpWidget(
      _wrap(
        store: FakeSettingsStore(),
        client: client,
        probes: FakeProbes(meshUp: true, relayUp: false),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    expect(client.connectCalls, 1);
    expect(client.lastTransport, TransportMode.mesh);
    expect(find.byType(SegmentedButton<TransportMode>), findsNothing);
    expect(find.text('Using Mesh'), findsOneWidget);
  });

  testWidgets('a mesh-only pairing never offers a transport menu', (
    tester,
  ) async {
    _useTallSurface(tester);
    const meshOnlyUri =
        'mcremote://pair?host=wss%3A%2F%2F100.64.0.3%3A7531&fp=$fp'
        '&token=mcr_mesh';
    clipboardPairUri(tester, meshOnlyUri);
    final client = FakeMcremoteClient();

    await tester.pumpWidget(
      _wrap(
        store: FakeSettingsStore(),
        client: client,
        // Even with both probes "up", there is no relay to choose.
        probes: FakeProbes(meshUp: true, relayUp: true),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    expect(find.byType(SegmentedButton<TransportMode>), findsNothing);
    expect(client.lastTransport, TransportMode.mesh);
    expect(client.lastRelayUrl, isNull);
  });

  testWidgets('neither probe answers: Connect still tries', (tester) async {
    _useTallSurface(tester);
    clipboardPairUri(tester, dualTokenUri);
    final client = FakeMcremoteClient();

    await tester.pumpWidget(
      _wrap(
        store: FakeSettingsStore(),
        client: client,
        probes: FakeProbes(meshUp: false, relayUp: false),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    // A probe is a soft signal — a firewalled healthz must not become a
    // refusal to dial. The transport is left null so the client decides.
    expect(client.connectCalls, 1);
    expect(client.lastTransport, isNull);
    expect(find.textContaining('Neither transport answered'), findsOneWidget);
  });

  testWidgets('Try-other is offered after a failed dial when both are '
      'configured', (tester) async {
    _useTallSurface(tester);
    clipboardPairUri(tester, dualTokenUri);
    final client = FakeMcremoteClient(
      connectError: McException('host went away', code: 'host_offline'),
    );

    await tester.pumpWidget(
      _wrap(
        store: FakeSettingsStore(),
        client: client,
        probes: FakeProbes(meshUp: true, relayUp: false),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    // Gated on *configured*, not on the probe: the failed relay probe is
    // exactly what the user may be overruling (D2/D5).
    expect(find.textContaining('Try '), findsWidgets);
  });

  testWidgets('Try-other is withheld once a pair code has been sent (A1)', (
    tester,
  ) async {
    _useTallSurface(tester);
    clipboardPairUri(tester, dualCodeUri);
    final client = FakeMcremoteClient(
      claimError: McException('host went away', code: 'host_offline'),
      spentCredential: true,
    );

    await tester.pumpWidget(
      _wrap(
        store: FakeSettingsStore(),
        client: client,
        probes: FakeProbes(meshUp: true, relayUp: false),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    expect(client.claimCalls, 1);
    // The host may already have consumed the code, so another transport can
    // only earn a permanent invalid_code. The recovery is a fresh code.
    expect(find.textContaining('Try Mesh'), findsNothing);
    expect(find.textContaining('Try Relay'), findsNothing);
    // Definite copy (0064 D7), in the card and the notification both.
    expect(find.textContaining('That pair code has been used'), findsWidgets);

    // Drain the notification's dismiss timer.
    await tester.pump(const Duration(seconds: 7));
    await tester.pumpAndSettle();
  });

  testWidgets('a burnt code raises the top notification with a one-tap '
      'recovery (V12)', (tester) async {
    _useTallSurface(tester);
    clipboardPairUri(tester, dualCodeUri);
    final client = FakeMcremoteClient(
      claimError: McException('host went away', code: 'host_offline'),
      spentCredential: true,
    );

    await tester.pumpWidget(
      _wrap(
        // Auto is the default, which makes the burn case the default path —
        // exactly why D7 exists.
        store: FakeSettingsStore(connectMode: 'auto'),
        client: client,
        probes: FakeProbes(meshUp: true, relayUp: true),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    expect(client.claimCalls, 1);
    // The notification carries the fact, definite and above the fold.
    expect(
      find.text('That pair code has been used. Get a new one and try again.'),
      findsOneWidget,
    );

    // The action is the notification's TextButton — the screen's own Enter
    // code button is a FilledButton, so the finder cannot confuse them.
    await tester.tap(find.widgetWithText(TextButton, 'Enter code'));
    await tester.pumpAndSettle();

    // Failure to recovery in one tap: the Enter-code sheet is open.
    expect(find.text('Enter pair code'), findsOneWidget);
  });

  testWidgets('the burn notification offers no transport retry (V13)', (
    tester,
  ) async {
    _useTallSurface(tester);
    clipboardPairUri(tester, dualCodeUri);
    final client = FakeMcremoteClient(
      claimError: McException('host went away', code: 'host_offline'),
      spentCredential: true,
    );

    await tester.pumpWidget(
      _wrap(
        store: FakeSettingsStore(connectMode: 'auto'),
        client: client,
        probes: FakeProbes(meshUp: true, relayUp: true),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    // Withheld everywhere — card and notification alike (0062 A1): retrying
    // a burnt code on another transport can only earn invalid_code.
    expect(find.textContaining('Try Mesh'), findsNothing);
    expect(find.textContaining('Try Relay'), findsNothing);

    await tester.pump(const Duration(seconds: 7));
    await tester.pumpAndSettle();
  });

  testWidgets('a slow auto-connect must not grey out the pairing buttons', (
    tester,
  ) async {
    // The regression: `disabled = _busy || _autoConnecting` gated Scan QR,
    // Enter code and Paste on the cold auto-connect. Pre-0062 that dial was
    // one attempt (~8 s worst case) and the greying was a flicker. 0062 made
    // it a DialEpisode — mesh 8 s + relay 20 s, up to a 35 s budget — so every
    // route back into pairing went dead for half a minute, exactly when
    // auto-connect is failing and you need them.
    _useTallSurface(tester);
    final store = FakeSettingsStore(host: '10.0.0.5:7531', token: 'mcr_saved');
    final client = FakeMcremoteClient(
      connectDelay: const Duration(seconds: 30),
    );

    await tester.pumpWidget(_wrap(store: store, client: client));
    // Several frames: the tall surface needs a layout pass to propagate, and
    // pumpAndSettle cannot be used while a 30 s dial is deliberately pending.
    for (var i = 0; i < 6; i++) {
      await tester.pump(const Duration(milliseconds: 50));
    }

    expect(client.connectCalls, 1, reason: 'the cold dial is under way');

    for (final label in ['Scan QR', 'Enter code', 'Paste URI / code / token']) {
      final button = tester.widget<ButtonStyleButton>(
        find
            .ancestor(
              of: find.text(label),
              // byWidgetPredicate, not byType: these are FilledButton /
              // OutlinedButton, and byType matches the exact runtime type.
              matching: find.byWidgetPredicate((w) => w is ButtonStyleButton),
            )
            .first,
      );
      expect(
        button.onPressed,
        isNotNull,
        reason: '"$label" must stay tappable while auto-connect is in flight',
      );
    }

    // Dialling twice at once is still prevented. Connect itself renders a
    // spinner mid-dial (so it has no label to find); Test healthz keeps its
    // label and is gated by the same flag.
    final health = tester.widget<OutlinedButton>(
      find.widgetWithText(OutlinedButton, 'Test healthz'),
    );
    expect(
      health.onPressed,
      isNull,
      reason: 'actions that dial stay gated while a dial is in flight',
    );

    await tester.pumpAndSettle(const Duration(seconds: 31));
  });

  group('secret-store banner (MADR 0066 D2)', () {
    testWidgets('credentialsLost renders the banner and auto-connect is not '
        'attempted', (tester) async {
      _useTallSurface(tester);
      // The silent-wipe shape: host survived (non-secret), token gone,
      // probe reports the marker outliving it.
      final store = FakeSettingsStore(host: '100.64.0.1:7531')
        ..probeResult = SecretStoreHealth.credentialsLost;
      final client = FakeMcremoteClient();
      await tester.pumpWidget(_wrap(store: store, client: client));
      await tester.pumpAndSettle();

      expect(store.probeCalled, isTrue);
      expect(find.text('Stored credentials were reset'), findsOneWidget);
      expect(
        find.textContaining('your hosts and preferences are intact'),
        findsOneWidget,
      );
      expect(client.connectCalls, 0);
    });

    testWidgets('credentialsLost skips auto-connect even when a token '
        'survived (identity-only wipe)', (tester) async {
      _useTallSurface(tester);
      // Incident #2's shape: the per-key wipe ate the identity, the token
      // survived — dialling would mint a fresh key and earn a guaranteed
      // client_key_mismatch on top of the banner.
      final store = FakeSettingsStore(host: '100.64.0.1:7531', token: 'mcr_ok')
        ..probeResult = SecretStoreHealth.credentialsLost;
      final client = FakeMcremoteClient();
      await tester.pumpWidget(_wrap(store: store, client: client));
      await tester.pumpAndSettle();

      expect(find.text('Stored credentials were reset'), findsOneWidget);
      expect(client.connectCalls, 0);
    });

    testWidgets('the banner action lands in the pair-code flow', (
      tester,
    ) async {
      _useTallSurface(tester);
      final store = FakeSettingsStore(host: '100.64.0.1:7531')
        ..probeResult = SecretStoreHealth.credentialsLost;
      await tester.pumpWidget(
        _wrap(store: store, client: FakeMcremoteClient()),
      );
      await tester.pumpAndSettle();

      // The banner carries its own Enter code button; tap the one inside
      // the banner card, not the pairing row's.
      await tester.tap(
        find.descendant(
          of: find.widgetWithText(Card, 'Stored credentials were reset'),
          matching: find.widgetWithText(FilledButton, 'Enter code'),
        ),
      );
      await tester.pumpAndSettle();

      // The recovery must actually arrive at the pairing flow, not just run
      // a handler (the Tailscale dead-button lesson, MADR 0066 D4).
      expect(find.text('Enter pair code'), findsOneWidget);
    });

    testWidgets('broken renders the keystore copy without an action', (
      tester,
    ) async {
      _useTallSurface(tester);
      final store = FakeSettingsStore()..probeResult = SecretStoreHealth.broken;
      await tester.pumpWidget(
        _wrap(store: store, client: FakeMcremoteClient()),
      );
      await tester.pumpAndSettle();

      expect(find.text('Secure storage unavailable'), findsOneWidget);
      expect(
        find.textContaining('Restart the device or re-enrol'),
        findsOneWidget,
      );
      expect(
        find.descendant(
          of: find.widgetWithText(Card, 'Secure storage unavailable'),
          matching: find.byType(FilledButton),
        ),
        findsNothing,
      );
    });

    testWidgets('a healthy store shows no banner', (tester) async {
      final store = FakeSettingsStore(host: '100.64.0.1:7531');
      await tester.pumpWidget(
        _wrap(store: store, client: FakeMcremoteClient()),
      );
      await tester.pumpAndSettle();

      expect(store.probeCalled, isTrue);
      expect(find.text('Stored credentials were reset'), findsNothing);
      expect(find.text('Secure storage unavailable'), findsNothing);
    });
  });

  group('key-mismatch recovery (MADR 0066 D4)', () {
    testWidgets('client_key_mismatch offers Reset & re-pair, and the action '
        'lands in the pair-code flow', (tester) async {
      _useTallSurface(tester);
      final store = FakeSettingsStore(host: '10.0.0.5:7531', token: 'mcr_ok');
      final client = FakeMcremoteClient(
        connectError: McException(
          'client key mismatch',
          code: 'client_key_mismatch',
          permanent: true,
        ),
      );

      await tester.pumpWidget(_wrap(store: store, client: client));
      await tester.pumpAndSettle();

      expect(find.textContaining('no longer matches the host'), findsOneWidget);
      await tester.tap(find.widgetWithText(TextButton, 'Reset & re-pair'));
      await tester.pumpAndSettle();

      // The full scoped reset ran — not a partial clear — and the user is
      // in the pairing flow, not staring at a dismissed toast.
      expect(store.clearSecretsCalled, isTrue);
      expect(client.clearMemoryCalled, isTrue);
      expect(find.text('Enter pair code'), findsOneWidget);

      // Dismiss the sheet and drain the notice timer.
      await tester.tapAt(const Offset(10, 10));
      await tester.pumpAndSettle(const Duration(seconds: 8));
    });

    testWidgets('an unpinned cert rejection reads as "scan the QR", not raw '
        'TLS text (incident #3 follow-up)', (tester) async {
      _useTallSurface(tester);
      final store = FakeSettingsStore(host: '10.0.0.5:7531', token: 'mcr_ok');
      final client = FakeMcremoteClient(
        connectError: McException(
          'TLS handshake failed for wss://10.0.0.5:7531/v1/ws: '
          'HandshakeException(unable to verify)',
          code: 'cert_unpinned',
          permanent: true,
        ),
      );

      await tester.pumpWidget(_wrap(store: store, client: client));
      await tester.pumpAndSettle();

      expect(
        find.textContaining('Scan the QR from `mcremote pair code`'),
        findsOneWidget,
      );
      expect(find.textContaining('HandshakeException'), findsNothing);
    });

    testWidgets('invalid_token keeps its own recovery — no reset chip', (
      tester,
    ) async {
      _useTallSurface(tester);
      final store = FakeSettingsStore(
        host: '10.0.0.5:7531',
        token: 'mcr_stale',
      );
      final client = FakeMcremoteClient(
        connectError: McException('bad token', code: 'invalid_token'),
      );

      await tester.pumpWidget(_wrap(store: store, client: client));
      await tester.pumpAndSettle();

      expect(find.text('Reset & re-pair'), findsNothing);
      expect(store.clearSecretsCalled, isFalse);
      // The deliberate-clear path still ran (it clears the marker too, so
      // no false credentialsLost banner on the next launch).
      expect(store.clearTokenCalled, isTrue);
    });
  });
}

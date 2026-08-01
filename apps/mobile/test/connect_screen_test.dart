import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/ws/transport_probes.dart';
import 'package:magic_cli_remote/features/connect/connect_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';

/// In-memory [SettingsStore] so tests never touch the platform keystore.
class FakeSettingsStore extends SettingsStore {
  FakeSettingsStore({this.host, this.token});

  String? host;
  String? token;
  String? deviceId;
  bool clearTokenCalled = false;
  bool clearAllCalled = false;
  String? relayUrl;
  String? relayHostId;
  String? relayAuthority;

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
}

/// Scriptable [McremoteClient] that never opens a socket.
class FakeMcremoteClient extends McremoteClient {
  FakeMcremoteClient({
    this.stateValue = McConnectionState.disconnected,
    this.pairedValue = false,
    this.loggedOutValue = false,
    this.connectError,
    this.claimError,
    this.spentCredential = false,
    this.healthzBody = 'ok',
  });

  McConnectionState stateValue;
  bool pairedValue;
  bool loggedOutValue;
  Object? connectError;
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
  testWidgets('renders the core pairing affordances', (tester) async {
    await tester.pumpWidget(
      _wrap(store: FakeSettingsStore(), client: FakeMcremoteClient()),
    );
    await tester.pumpAndSettle();

    expect(find.text('Connect to your machine'), findsOneWidget);
    expect(find.text('Scan QR'), findsOneWidget);
    expect(find.text('Enter code'), findsOneWidget);
    expect(find.text('Paste URI / code / token'), findsOneWidget);
    expect(find.text('Test healthz'), findsOneWidget);
    expect(find.text('Connect'), findsOneWidget);
    // Host field prefilled with the platform default; token empty by default.
    expect(find.widgetWithText(TextField, 'Host (mcremote)'), findsOneWidget);
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
    expect(find.text('Connect to your machine'), findsOneWidget);
  });

  testWidgets('a logged-out client skips auto-connect entirely', (
    tester,
  ) async {
    final store = FakeSettingsStore(host: '10.0.0.5:7531', token: 'mcr_ok');
    final client = FakeMcremoteClient(loggedOutValue: true);

    await tester.pumpWidget(_wrap(store: store, client: client));
    await tester.pumpAndSettle();

    expect(client.connectCalls, 0);
    expect(find.text('Connect to your machine'), findsOneWidget);
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

  testWidgets('both transports up: the menu appears and nothing is dialled '
      'until Connect', (tester) async {
    _useTallSurface(tester);
    clipboardPairUri(tester, dualTokenUri);
    final client = FakeMcremoteClient();

    await tester.pumpWidget(
      _wrap(
        store: FakeSettingsStore(),
        client: client,
        probes: FakeProbes(meshUp: true, relayUp: true),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    // This is the regression 0062 exists to fix: the old screen claimed or
    // connected immediately, so the menu below could never be reached.
    expect(client.connectCalls, 0);
    expect(find.byType(SegmentedButton<TransportMode>), findsOneWidget);
    expect(find.text('Mesh'), findsOneWidget);
    expect(find.text('Relay'), findsOneWidget);
    expect(find.textContaining('choose one, then Connect'), findsOneWidget);
  });

  testWidgets('both up: Connect dials the default Mesh', (tester) async {
    _useTallSurface(tester);
    clipboardPairUri(tester, dualTokenUri);
    final client = FakeMcremoteClient();

    await tester.pumpWidget(
      _wrap(
        store: FakeSettingsStore(),
        client: client,
        probes: FakeProbes(meshUp: true, relayUp: true),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    await tester.tap(find.widgetWithText(FilledButton, 'Connect'));
    await tester.pumpAndSettle();

    expect(client.connectCalls, 1);
    expect(client.lastTransport, TransportMode.mesh);
  });

  testWidgets('both up: picking Relay dials the relay', (tester) async {
    _useTallSurface(tester);
    clipboardPairUri(tester, dualTokenUri);
    final client = FakeMcremoteClient();

    await tester.pumpWidget(
      _wrap(
        store: FakeSettingsStore(),
        client: client,
        probes: FakeProbes(meshUp: true, relayUp: true),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Relay'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Connect'));
    await tester.pumpAndSettle();

    expect(client.lastTransport, TransportMode.relay);
  });

  testWidgets('a dual-available QR *code* is not auto-claimed', (tester) async {
    _useTallSurface(tester);
    clipboardPairUri(tester, dualCodeUri);
    final client = FakeMcremoteClient();

    await tester.pumpWidget(
      _wrap(
        store: FakeSettingsStore(),
        client: client,
        probes: FakeProbes(meshUp: true, relayUp: true),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Paste URI / code / token'));
    await tester.pumpAndSettle();

    // A pair code is one-shot: spending it on a guessed transport is exactly
    // what the menu exists to prevent (D5 + A1).
    expect(client.claimCalls, 0);
    expect(find.byType(SegmentedButton<TransportMode>), findsOneWidget);
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
    expect(find.textContaining('pair code may have been used'), findsOneWidget);
  });
}

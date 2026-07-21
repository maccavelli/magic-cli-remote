import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/ws/mc_exception.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
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
    this.healthzBody = 'ok',
  });

  McConnectionState stateValue;
  bool pairedValue;
  bool loggedOutValue;
  Object? connectError;
  String healthzBody;

  int connectCalls = 0;
  bool clearMemoryCalled = false;

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
    bool enableAutoReconnect = true,
  }) async {
    connectCalls++;
    if (connectError != null) throw connectError!;
  }

  @override
  Future<String> healthz(
    String hostInput, {
    String? fingerprint,
    TlsMode? mode,
  }) async =>
      healthzBody;

  @override
  void clearMemoryCredentials({bool host = false}) {
    clearMemoryCalled = true;
  }

  @override
  Future<void> disconnect({bool manual = true}) async {}
}

Widget _wrap({
  required SettingsStore store,
  required McremoteClient client,
}) {
  return ProviderScope(
    overrides: [
      settingsStoreProvider.overrideWithValue(store),
      mcremoteClientProvider.overrideWithValue(client),
    ],
    child: const MaterialApp(home: ConnectScreen()),
  );
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
    expect(find.widgetWithText(TextField, 'Host'), findsOneWidget);
  });

  testWidgets('Connect with an empty token shows a validation error and does '
      'not call the client', (tester) async {
    final client = FakeMcremoteClient();
    await tester.pumpWidget(
      _wrap(store: FakeSettingsStore(), client: client),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.widgetWithText(FilledButton, 'Connect'));
    await tester.pumpAndSettle();

    expect(
      find.textContaining('Host and token required'),
      findsOneWidget,
    );
    expect(client.connectCalls, 0);
  });

  testWidgets('Test healthz surfaces the reachable body', (tester) async {
    final client = FakeMcremoteClient(healthzBody: 'mcremote ok');
    await tester.pumpWidget(
      _wrap(store: FakeSettingsStore(), client: client),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.widgetWithText(OutlinedButton, 'Test healthz'));
    await tester.pumpAndSettle();

    expect(find.textContaining('Reachable: mcremote ok'), findsOneWidget);
  });

  testWidgets('auto-connect on a bad saved token clears the token and offers '
      're-pair without navigating away', (tester) async {
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

  testWidgets('a logged-out client skips auto-connect entirely',
      (tester) async {
    final store = FakeSettingsStore(host: '10.0.0.5:7531', token: 'mcr_ok');
    final client = FakeMcremoteClient(loggedOutValue: true);

    await tester.pumpWidget(_wrap(store: store, client: client));
    await tester.pumpAndSettle();

    expect(client.connectCalls, 0);
    expect(find.text('Connect to your machine'), findsOneWidget);
  });
}

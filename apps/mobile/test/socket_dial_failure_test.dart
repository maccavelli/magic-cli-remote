@TestOn('vm')
library;

import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/ws/mc_exception.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'support/test_certs.dart';

/// A dial that cannot succeed must still *fail*, promptly.
///
/// These use real sockets deliberately. The regression they cover (MADR 0046
/// H-A) lives in `web_socket_channel`'s error branch — a channel whose `ready`
/// failed never completes `sink.close()` — so a fake sink cannot reproduce it
/// and the whole class of bug is invisible to the rest of the suite.
///
/// A plain transport failure is rethrown as-is by `_connectInternal` (only the
/// pin/TLS cases are classified, and those are asserted typed below), so what
/// these assert is the property H-A broke: the future *completes* rather than
/// hanging, and the client lands in a definitive error state.

/// Raised only when the dial future never completes, so it stays distinct from
/// the client's own `ready` timeout — which is a dial failing correctly, on
/// time, and must not read as a hang.
class _DialHung implements Exception {
  @override
  String toString() => 'dial never completed';
}

/// Expects [dial] to complete with *some* error inside [within].
Future<void> _expectFailsFast(
  Future<void> dial, {
  Duration within = const Duration(seconds: 5),
}) => expectLater(
  dial.timeout(within, onTimeout: () => throw _DialHung()),
  throwsA(isNot(isA<_DialHung>())),
);

class _MemorySecureStorage implements FlutterSecureStorage {
  final values = <String, String>{};

  @override
  dynamic noSuchMethod(Invocation invocation) {
    final key = invocation.namedArguments[#key] as String?;
    switch (invocation.memberName) {
      case #read:
        return Future<String?>.value(values[key]);
      case #write:
        final value = invocation.namedArguments[#value] as String?;
        if (value == null) {
          values.remove(key);
        } else {
          values[key!] = value;
        }
        return Future<void>.value();
      case #delete:
        values.remove(key);
        return Future<void>.value();
      default:
        return Future<void>.value();
    }
  }
}

McremoteClient _newClient() {
  final client = McremoteClient(
    settings: SettingsStore(secure: _MemorySecureStorage()),
  );
  addTearDown(() async {
    await client.disconnect();
    await client.dispose();
  });
  return client;
}

/// A port nothing is listening on: `connect` is refused immediately.
Future<int> _deadPort() async {
  final probe = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
  final port = probe.port;
  await probe.close();
  return port;
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  test('a refused dial fails typed instead of hanging', () async {
    final port = await _deadPort();
    final client = _newClient();

    await _expectFailsFast(
      client.connect(
        hostInput: 'ws://${InternetAddress.loopbackIPv4.address}:$port',
        token: 'token',
        mode: TlsMode.off,
        enableAutoReconnect: false,
      ),
    );
    expect(client.state, McConnectionState.error);
    expect(client.lastErrorCode, 'connect_failed');
  });

  test('a black-holed dial fails at the ready timeout, not never', () async {
    // Accepts the TCP connection and then says nothing at all, so the
    // WebSocket upgrade never completes and `ready` hits its own timeout.
    final server = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
    final held = <Socket>[];
    server.listen(held.add);
    addTearDown(() async {
      for (final s in held) {
        s.destroy();
      }
      await server.close();
    });
    final client = _newClient();

    // The client's own 8s `ready` bound is what must fire here; the helper's
    // longer deadline only catches the case where nothing fires at all.
    await _expectFailsFast(
      client.connect(
        hostInput:
            'ws://${InternetAddress.loopbackIPv4.address}:${server.port}',
        token: 'token',
        mode: TlsMode.off,
        enableAutoReconnect: false,
      ),
      within: const Duration(seconds: 20),
    );
    expect(client.state, McConnectionState.error);
  }, timeout: const Timeout(Duration(seconds: 60)));

  test('the auto-reconnect loop survives a failed dial', () async {
    // Accepts, then immediately resets — every dial fails fast, and each one
    // is counted. Pre-fix the first dial never returns, so the backoff timer
    // is never armed and `dials` stays at 1 forever.
    var dials = 0;
    final server = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
    server.listen((socket) {
      dials++;
      socket.destroy();
    });
    addTearDown(server.close);
    final client = _newClient();

    await _expectFailsFast(
      client.connect(
        hostInput:
            'ws://${InternetAddress.loopbackIPv4.address}:${server.port}',
        token: 'token',
        mode: TlsMode.off,
      ),
    );

    // First backoff is 1s; give the timer room without pinning the test to it.
    final deadline = DateTime.now().add(const Duration(seconds: 10));
    while (dials < 2 && DateTime.now().isBefore(deadline)) {
      await Future<void>.delayed(const Duration(milliseconds: 50));
    }
    expect(
      dials,
      greaterThanOrEqualTo(2),
      reason: 'the reconnect loop must re-dial after a failed attempt',
    );
  }, timeout: const Timeout(Duration(seconds: 60)));

  test('a pin mismatch surfaces as cert_mismatch, not a hang', () async {
    final ctx = SecurityContext()
      ..useCertificateChainBytes(certA.codeUnits)
      ..usePrivateKeyBytes(keyA.codeUnits);
    final server = await HttpServer.bindSecure(
      InternetAddress.loopbackIPv4.address,
      0,
      ctx,
    );
    server.listen((req) async {
      req.response.statusCode = HttpStatus.notFound;
      await req.response.close();
    });
    addTearDown(() => server.close(force: true));
    final client = _newClient();

    // The host answers on the right address with a certificate this device
    // never paired with: the failure must reach the user as the typed,
    // permanent mismatch rather than being swallowed by the close await.
    await expectLater(
      client
          .connect(
            hostInput:
                'wss://${InternetAddress.loopbackIPv4.address}:${server.port}',
            token: 'token',
            fingerprint: fpB,
            mode: TlsMode.selfsigned,
            enableAutoReconnect: false,
          )
          .timeout(const Duration(seconds: 10)),
      throwsA(
        isA<McException>()
            .having((e) => e.code, 'code', 'cert_mismatch')
            .having((e) => e.permanent, 'permanent', isTrue),
      ),
    );
  }, timeout: const Timeout(Duration(seconds: 60)));
}

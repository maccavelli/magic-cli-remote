@TestOn('vm')
library;

import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/protocol/transport_policy.dart';
import 'package:magic_cli_remote/data/ws/mc_exception.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'support/test_certs.dart';

/// The inner hop over the relay must be TLS — asserted through the real dial.
///
/// mcrelay splices opaque bytes and the host-side bridge is a raw TCP forward,
/// so nothing between the phone and mcremote can add encryption: whatever the
/// client writes into the tunnel is what reaches the daemon. That makes the
/// first byte on the tunnel the whole test — 0x16 is a TLS handshake record,
/// 0x47 is `G` for `GET`.
///
/// This regressed from 2026-07-23 until it surfaced on hardware. dart:io uses a
/// `connectionFactory` socket **verbatim** and never secures it, so an
/// `https`/`wss` URL went out in cleartext; mcremote answered "client sent an
/// HTTP request to an HTTPS server" and closed, so the relay path failed closed
/// on every attempt rather than downgrading silently.
///
/// This drives `McremoteClient.connect(transport: relay)` end to end against a
/// fake mcrelay and a real TLS daemon. An earlier version re-implemented the
/// dial inline and passed with the fix reverted — covering the pattern instead
/// of the production path, which is the false confidence a regression test
/// must never give.

/// A minimal mcrelay: accepts the phone plane, answers `join`, then splices
/// binary frames to the daemon. Records the first byte the phone sends through.
class _FakeRelay {
  _FakeRelay._(this._server);

  final HttpServer _server;
  final firstBytes = <int>[];
  int joins = 0;

  static Future<_FakeRelay> start({required int daemonPort}) async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final relay = _FakeRelay._(server);
    server.listen((req) async {
      final ws = await WebSocketTransformer.upgrade(req);
      Socket? upstream;
      var recorded = false;
      ws.listen(
        (frame) async {
          if (frame is String) {
            // Join plane: the only text frame, and it must come first.
            final env = jsonDecode(frame) as Map<String, dynamic>;
            if (env['type'] != 'join') return;
            relay.joins++;
            final sock = await Socket.connect(
              InternetAddress.loopbackIPv4,
              daemonPort,
            );
            upstream = sock;
            sock.listen(
              ws.add,
              onDone: () => ws.close(),
              onError: (_) => ws.close(),
              cancelOnError: true,
            );
            ws.add(jsonEncode({'v': 1, 'type': 'join_ok', 'id': env['id']}));
            return;
          }
          // Opaque tunnel bytes from the phone's inner TLS client.
          final bytes = frame as List<int>;
          if (!recorded && bytes.isNotEmpty) {
            recorded = true;
            relay.firstBytes.add(bytes.first);
          }
          upstream?.add(bytes);
        },
        onDone: () => upstream?.destroy(),
        onError: (_) => upstream?.destroy(),
        cancelOnError: true,
      );
    }, onError: (_) {});
    return relay;
  }

  String get base => 'ws://127.0.0.1:${_server.port}';
  Future<void> close() => _server.close(force: true);
}

/// A real TLS mcremote listener that completes the auth handshake.
class _FakeDaemon {
  _FakeDaemon._(this._server);

  final HttpServer _server;
  int upgrades = 0;
  int auths = 0;

  static Future<_FakeDaemon> start() async {
    final ctx = SecurityContext()
      ..useCertificateChainBytes(certA.codeUnits)
      ..usePrivateKeyBytes(keyA.codeUnits);
    final server = await HttpServer.bindSecure('127.0.0.1', 0, ctx);
    final daemon = _FakeDaemon._(server);
    server.listen((req) async {
      final ws = await WebSocketTransformer.upgrade(req);
      daemon.upgrades++;
      ws.listen(
        (raw) {
          final env = jsonDecode(raw as String) as Map<String, dynamic>;
          if (env['type'] != 'auth') return;
          daemon.auths++;
          ws.add(
            jsonEncode({
              'v': 1,
              'type': 'auth_ok',
              'id': env['id'],
              'payload': {'device_id': 'dev-1', 'device_name': 'test'},
            }),
          );
        },
        onError: (_) {},
        cancelOnError: false,
      );
    }, onError: (_) {});
    return daemon;
  }

  int get port => _server.port;
  String get hostInput => 'wss://127.0.0.1:$port';
  Future<void> close() => _server.close(force: true);
}

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

void main() {
  late _FakeDaemon daemon;
  late _FakeRelay relay;

  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    daemon = await _FakeDaemon.start();
    relay = await _FakeRelay.start(daemonPort: daemon.port);
  });

  tearDown(() async {
    await relay.close();
    await daemon.close();
  });

  McremoteClient newClient() {
    final client = McremoteClient(
      settings: SettingsStore(secure: _MemorySecureStorage()),
    );
    addTearDown(() async {
      await client.disconnect();
      await client.dispose();
    });
    return client;
  }

  Future<Object?> connectViaRelay(
    McremoteClient client, {
    required String fingerprint,
  }) async {
    try {
      await client.connect(
        hostInput: daemon.hostInput,
        token: 'tok',
        fingerprint: fingerprint,
        mode: TlsMode.selfsigned,
        transport: TransportMode.relay,
        relayUrl: relay.base,
        relayHostId: 'macos-laptop',
        enableAutoReconnect: false,
        allowTransportFallback: false,
      );
      return null;
    } catch (e) {
      return e;
    }
  }

  test('the inner hop speaks TLS, not cleartext HTTP', () async {
    final client = newClient();
    final error = await connectViaRelay(client, fingerprint: fpA);

    expect(relay.joins, 1, reason: 'the phone must join the relay');
    expect(
      relay.firstBytes,
      isNotEmpty,
      reason: 'no tunnel bytes reached the relay at all',
    );
    final first = relay.firstBytes.first;
    expect(
      first,
      0x16,
      reason: first == 0x47
          ? "0x47 is 'G' — the inner request went out in cleartext, which "
                'mcremote rejects as HTTP-to-an-HTTPS-server'
          : 'expected a TLS handshake record',
    );
    expect(error, isNull, reason: 'relay dial failed: $error');
    expect(client.state, McConnectionState.connected);
    expect(daemon.auths, 1, reason: 'the session must authenticate end to end');
    expect(client.activeTransport, TransportMode.relay);
  });

  test('the pin is enforced inside the tunnel', () async {
    // The relay is an untrusted splice, so the pin must be checked on the
    // socket the client secures itself — the HttpClient's own
    // badCertificateCallback never fires for a connectionFactory socket.
    final client = newClient();
    final error = await connectViaRelay(client, fingerprint: fpB);

    expect(error, isA<McException>());
    expect((error as McException).code, 'cert_mismatch');
    expect(error.permanent, isTrue);
    expect(daemon.auths, 0, reason: 'no session may reach the daemon');
  });
}

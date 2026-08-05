@TestOn('vm')
library;

import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
import 'package:shared_preferences/shared_preferences.dart';

// MADR 0068 P2 (U3, dart side) — close code 4001 `replaced` parks the
// client instead of re-arming the reconnect loop: a newer connection of
// this device exists, and an awakened zombie must not fight it.

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

/// Completes the auth handshake, then closes the socket with a chosen close
/// code on demand — the daemon-side shape of 0068 P2's replaceElders.
class _ClosingPeer {
  _ClosingPeer._(this._server);

  final HttpServer _server;
  WebSocket? _ws;
  int upgrades = 0;

  static Future<_ClosingPeer> start() async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final peer = _ClosingPeer._(server);
    server.listen((req) async {
      final ws = await WebSocketTransformer.upgrade(req);
      peer.upgrades++;
      peer._ws = ws;
      ws.listen(
        (raw) {
          final env = jsonDecode(raw as String) as Map<String, dynamic>;
          switch (env['type']) {
            case 'auth':
              ws.add(
                jsonEncode({
                  'v': 1,
                  'type': 'auth_ok',
                  'id': env['id'],
                  'payload': {'device_id': 'dev-1', 'device_name': 'test'},
                }),
              );
            case 'ping':
              ws.add(jsonEncode({'v': 1, 'type': 'pong', 'id': env['id']}));
          }
        },
        onError: (_) {},
        cancelOnError: false,
      );
    }, onError: (_) {});
    return peer;
  }

  Future<void> closeWith(int code, String reason) async {
    await _ws?.close(code, reason);
  }

  String get hostInput => 'ws://127.0.0.1:${_server.port}';
  Future<void> close() => _server.close(force: true);
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

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

  test(
    '4001 replaced: parks disconnected, keeps pairing, never re-dials',
    () async {
      final peer = await _ClosingPeer.start();
      addTearDown(peer.close);
      final client = newClient();

      await client.connect(
        hostInput: peer.hostInput,
        token: 'tok',
        mode: TlsMode.off,
        allowTransportFallback: false,
      );
      expect(client.state, McConnectionState.connected);

      await peer.closeWith(kCloseReplaced, 'replaced');
      // Give the done-handler and any (wrongly) armed reconnect a beat.
      await Future<void>.delayed(const Duration(milliseconds: 1800));

      expect(client.state, McConnectionState.disconnected);
      expect(client.isPaired, isTrue, reason: 'replacement is not a sign-out');
      expect(
        peer.upgrades,
        1,
        reason: 'a replaced client must not reconnect-fight the newer login',
      );
    },
  );

  test('contrast: an ordinary abnormal close still auto-reconnects', () async {
    final peer = await _ClosingPeer.start();
    addTearDown(peer.close);
    final client = newClient();

    await client.connect(
      hostInput: peer.hostInput,
      token: 'tok',
      mode: TlsMode.off,
      allowTransportFallback: false,
    );
    expect(client.state, McConnectionState.connected);

    await peer.closeWith(1011, 'boom');
    // First backoff rung is 1 s; the redial lands within this window.
    await Future<void>.delayed(const Duration(milliseconds: 2500));

    expect(
      peer.upgrades,
      greaterThan(1),
      reason: 'a non-replaced close keeps the reconnect loop armed',
    );
  });
}

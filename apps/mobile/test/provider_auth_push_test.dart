import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// A daemon stub that authenticates and then pushes whatever frames a test
/// hands it. Server-initiated frames are the part of MADR 0074 the client had
/// no path for: `provider.auth_status`, `oauth.device_flow` and
/// `oauth.device_flow_result` carry no request id, so they are dropped unless
/// the read loop routes them explicitly.
class _PushServer {
  _PushServer._(this._server);

  final HttpServer _server;
  final _sockets = <WebSocket>[];

  String get host =>
      'ws://${InternetAddress.loopbackIPv4.address}:${_server.port}';

  static Future<_PushServer> start() async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final result = _PushServer._(server);
    unawaited(
      server.forEach((request) async {
        if (!WebSocketTransformer.isUpgradeRequest(request)) {
          request.response.statusCode = HttpStatus.notFound;
          await request.response.close();
          return;
        }
        final socket = await WebSocketTransformer.upgrade(request);
        result._sockets.add(socket);
        socket.listen((raw) {
          final req = jsonDecode(raw as String) as Map<String, dynamic>;
          if (req['type'] != 'auth') return;
          socket.add(
            jsonEncode({
              'v': 2,
              'type': 'auth_ok',
              'id': req['id'],
              'payload': {
                'device_id': 'dev',
                'device_name': 'dev',
                'caps': {'provider_auth': true},
              },
            }),
          );
        });
      }),
    );
    return result;
  }

  void push(Map<String, dynamic> envelope) {
    for (final s in _sockets) {
      s.add(jsonEncode(envelope));
    }
  }

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
  setUp(() => SharedPreferences.setMockInitialValues({}));

  Future<(McremoteClient, _PushServer)> connect() async {
    final server = await _PushServer.start();
    final client = McremoteClient(
      settings: SettingsStore(secure: _MemorySecureStorage()),
    );
    await client.connect(
      hostInput: server.host,
      token: 'mcr_test',
      mode: TlsMode.off,
    );
    return (client, server);
  }

  test('a credential-state push reaches listeners', () async {
    final (client, server) = await connect();
    addTearDown(() async {
      await client.dispose();
      await server.close();
    });

    final next = client.providerAuthStatus.first;
    server.push({
      'v': 2,
      'type': 'provider.auth_status',
      'payload': {
        'id': 'opencode',
        'auth': {'status': 'configured'},
      },
    });
    expect((await next.timeout(const Duration(seconds: 5)))['id'], 'opencode');
  });

  test('a device flow and its result reach listeners', () async {
    final (client, server) = await connect();
    addTearDown(() async {
      await client.dispose();
      await server.close();
    });

    final flow = client.deviceFlows.first;
    final result = client.deviceFlowResults.first;
    server.push({
      'v': 2,
      'type': 'oauth.device_flow',
      'payload': {
        'flow_id': 'f1',
        'provider_id': 'kilo',
        'verification_uri': 'https://app.kilo.ai/device-auth',
        'user_code': 'RX2Y-4H7X',
        'expires_in': 900,
      },
    });
    final got = await flow.timeout(const Duration(seconds: 5));
    expect(got['user_code'], 'RX2Y-4H7X');

    server.push({
      'v': 2,
      'type': 'oauth.device_flow_result',
      'payload': {'flow_id': 'f1', 'ok': true},
    });
    final done = await result.timeout(const Duration(seconds: 5));
    expect(done['ok'], isTrue);
  });
}

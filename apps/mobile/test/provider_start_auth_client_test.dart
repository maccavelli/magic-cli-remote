import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// A daemon stub that authenticates and then answers one scripted reply per
/// request type, so the frame `startProviderDeviceAuth` sends can be read as
/// the daemon reads it (MADR 0074 D35, PLAN P24).
class _StubDaemon {
  _StubDaemon._(this._server);

  final HttpServer _server;

  Map<String, Map<String, dynamic>? Function(Map<String, dynamic>)> replies =
      {};

  String get host =>
      'ws://${InternetAddress.loopbackIPv4.address}:${_server.port}';

  static Future<_StubDaemon> start() async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final stub = _StubDaemon._(server);
    unawaited(
      server.forEach((request) async {
        if (!WebSocketTransformer.isUpgradeRequest(request)) {
          request.response.statusCode = HttpStatus.notFound;
          await request.response.close();
          return;
        }
        final socket = await WebSocketTransformer.upgrade(request);
        socket.listen((raw) {
          final req = jsonDecode(raw as String) as Map<String, dynamic>;
          final type = req['type'] as String?;
          if (type == 'auth') {
            socket.add(
              jsonEncode({
                'v': 1,
                'type': 'auth_ok',
                'id': req['id'],
                'payload': {'device_id': 'dev', 'device_name': 'dev'},
              }),
            );
            return;
          }
          final build = stub.replies[type];
          if (build == null) return;
          final reply = build(req);
          if (reply != null) socket.add(jsonEncode(reply));
        });
      }),
    );
    return stub;
  }

  Future<void> close() => _server.close(force: true);
}

Future<McremoteClient> _connected(_StubDaemon daemon) async {
  final client = McremoteClient();
  await client.connect(
    hostInput: daemon.host,
    token: 'mcr_dev',
    mode: TlsMode.off,
  );
  return client;
}

void main() {
  // Deliberately no TestWidgetsFlutterBinding.ensureInitialized(): that binding
  // installs a mock HTTP client which refuses the real loopback WebSocket these
  // tests need, exactly as the existing client tests avoid it.
  setUp(() {
    SharedPreferences.setMockInitialValues({});
    FlutterSecureStorage.setMockInitialValues({});
  });

  Future<Map<String, dynamic>> sentPayload({
    Map<String, String> inputs = const {},
  }) async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    Map<String, dynamic>? sent;
    daemon.replies['provider.start_auth'] = (req) {
      sent = req['payload'] as Map<String, dynamic>;
      return {'v': 1, 'type': 'ok', 'id': req['id']};
    };
    final client = await _connected(daemon);
    addTearDown(client.dispose);

    await client.startProviderDeviceAuth(
      providerId: 'codex',
      upstreamId: 'openai',
      methodId: 'openai:device',
      inputs: inputs,
    );
    expect(
      sent,
      isNotNull,
      reason: 'the daemon received no provider.start_auth',
    );
    return sent!;
  }

  test('start_auth sends the method and no confirmation member', () async {
    final payload = await sentPayload();
    expect(payload.keys.toSet(), {
      'provider_id',
      'upstream_id',
      'method_id',
    }, reason: 'the legacy codex flow is gone, so nothing is confirmed (D35)');
    expect(payload['method_id'], 'openai:device');
  });

  test('start_auth sends inputs only when there are some', () async {
    final payload = await sentPayload(inputs: {'deploymentType': 'github.com'});
    expect(payload.keys.toSet(), {
      'provider_id',
      'upstream_id',
      'method_id',
      'inputs',
    });
    expect(payload['inputs'], {'deploymentType': 'github.com'});
  });
}

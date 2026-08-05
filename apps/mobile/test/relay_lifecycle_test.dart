@TestOn('vm')
library;

import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/ws/mc_exception.dart';
import 'package:magic_cli_remote/data/ws/relay_transport.dart';

// MADR 0068 P5/T1 (U8) — RelayTransport lifecycle: the half-alive states
// the A1 audit found (findings 8/9/10) and the episode-staleness guard.

/// A minimal mcrelay join plane: upgrade, answer `join` with `join_ok`.
class _FakeRelay {
  _FakeRelay._(this._server);

  final HttpServer _server;
  WebSocket? ws;
  int joins = 0;

  static Future<_FakeRelay> start() async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final relay = _FakeRelay._(server);
    server.listen((req) async {
      final socket = await WebSocketTransformer.upgrade(req);
      relay.ws = socket;
      socket.listen(
        (raw) {
          if (raw is! String) return;
          final env = jsonDecode(raw) as Map<String, dynamic>;
          if (env['type'] == 'join') {
            relay.joins++;
            socket.add(
              jsonEncode({
                'v': 1,
                'type': 'join_ok',
                'id': env['id'],
                'payload': {'session_id': 's-${relay.joins}'},
              }),
            );
          }
        },
        onError: (_) {},
        cancelOnError: false,
      );
    }, onError: (_) {});
    return relay;
  }

  String get base => 'ws://127.0.0.1:${_server.port}';
  Future<void> close() => _server.close(force: true);
}

Future<bool> _portRefuses(int port) async {
  try {
    final s = await Socket.connect(
      InternetAddress.loopbackIPv4,
      port,
      timeout: const Duration(milliseconds: 500),
    );
    s.destroy();
    return false;
  } catch (_) {
    return true;
  }
}

void main() {
  test(
    'close() is awaitable-idempotent: every caller gets the same future',
    () async {
      final relay = await _FakeRelay.start();
      addTearDown(relay.close);
      final t = await RelayTransport.open(relayBase: relay.base, hostId: 'h1');

      final f1 = t.close();
      final f2 = t.close();
      expect(
        identical(f1, f2),
        isTrue,
        reason:
            'lifecycle teardown and episode cleanup must both be able to '
            'await the SAME completion (A1 finding 10)',
      );
      await f1;
      expect(await _portRefuses(t.localPort), isTrue);
    },
  );

  test(
    'the outer hop ending tears the whole bridge down, not just the peer',
    () async {
      final relay = await _FakeRelay.start();
      addTearDown(relay.close);
      final t = await RelayTransport.open(relayBase: relay.base, hostId: 'h1');
      final port = t.localPort;

      // The relay drops the outer WSS — the iOS-normal case (A1 finding 9).
      await relay.ws!.close();
      await Future<void>.delayed(const Duration(milliseconds: 300));

      expect(
        await _portRefuses(port),
        isTrue,
        reason: 'a bridge whose outer hop died must not keep listening',
      );
      await t.close(); // idempotent no-op
    },
  );

  test('a superseded dial self-aborts at the join instead of stranding a '
      'half-built bridge', () async {
    final relay = await _FakeRelay.start();
    addTearDown(relay.close);

    try {
      await RelayTransport.open(
        relayBase: relay.base,
        hostId: 'h1',
        cancelled: () => true, // the episode was parked/superseded
      );
      fail('superseded open must throw');
    } on McException catch (e) {
      expect(e.code, 'dial_superseded');
    }
  });

  test(
    'a live peer still bridges after the lifecycle fixes (control case)',
    () async {
      final relay = await _FakeRelay.start();
      addTearDown(relay.close);
      final t = await RelayTransport.open(relayBase: relay.base, hostId: 'h1');
      addTearDown(t.close);

      // Outer→peer: bytes sent by the relay land on the loopback socket.
      final peer = await Socket.connect(
        InternetAddress.loopbackIPv4,
        t.localPort,
      );
      addTearDown(peer.destroy);
      final received = <int>[];
      peer.listen(received.addAll);
      relay.ws!.add([1, 2, 3]);
      await Future<void>.delayed(const Duration(milliseconds: 300));
      expect(received, [1, 2, 3]);
    },
  );
}

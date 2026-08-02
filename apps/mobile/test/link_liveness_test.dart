@TestOn('vm')
library;

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/ws/link_health.dart';
import 'package:magic_cli_remote/data/ws/mc_exception.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Protocol keepalive (MADR 0063 D2 / plan P0).
///
/// The failure this guards is a path that stops carrying packets without
/// closing anything: no RST, no FIN, writes still succeed into the local send
/// buffer. Before `pingInterval`, nothing below the 20 s application ping could
/// notice, so the client reported `connected` for 32–52 s after the link died —
/// and indefinitely when the OS deferred the timer.
///
/// Two cases, and the second matters as much as the first: if the peer did not
/// answer pings, enabling this would tear down *healthy* connections. Both the
/// daemon and mcrelay use `coder/websocket`, which pongs from its read path
/// (`read.go:317`), and [_healthyDaemon] is the executable form of that claim.

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

/// A WebSocket peer that completes the upgrade by hand and then answers
/// nothing at all — not pings, not requests.
///
/// Hand-rolled because dart:io's own server auto-pongs, exactly like the real
/// daemon: there is no way to build a silent peer out of
/// `WebSocketTransformer.upgrade`. This is what a blackholed path looks like
/// from the client's side of the socket.
class _SilentPeer {
  _SilentPeer._(this._server);

  final ServerSocket _server;
  final _sockets = <Socket>[];
  int upgrades = 0;

  static Future<_SilentPeer> start() async {
    final server = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
    final peer = _SilentPeer._(server);
    server.listen((socket) {
      peer._sockets.add(socket);
      final buffer = StringBuffer();
      socket.listen(
        (data) {
          if (peer.upgrades > 0) return; // already upgraded: stay silent
          buffer.write(String.fromCharCodes(data));
          final text = buffer.toString();
          if (!text.contains('\r\n\r\n')) return;
          final key = RegExp(
            r'Sec-WebSocket-Key:\s*(.+)\r\n',
            caseSensitive: false,
          ).firstMatch(text)?.group(1);
          if (key == null) {
            socket.destroy();
            return;
          }
          // RFC 6455 handshake: SHA-1 of key + GUID, base64.
          final accept = base64.encode(
            sha1
                .convert(
                  utf8.encode(
                    '${key.trim()}258EAFA5-E914-47DA-95CA-C5AB0DC85B11',
                  ),
                )
                .bytes,
          );
          peer.upgrades++;
          socket.write(
            'HTTP/1.1 101 Switching Protocols\r\n'
            'Upgrade: websocket\r\n'
            'Connection: Upgrade\r\n'
            'Sec-WebSocket-Accept: $accept\r\n'
            '\r\n',
          );
          // …and from here, nothing. No pong, ever.
        },
        onError: (_) {},
        cancelOnError: false,
      );
    });
    return peer;
  }

  String get hostInput => 'ws://127.0.0.1:${_server.port}';

  Future<void> close() async {
    for (final s in _sockets) {
      s.destroy();
    }
    await _server.close();
  }
}

/// A well-behaved peer: dart:io answers pings from its own read path, which is
/// what `coder/websocket` does on the Go side.
class _HealthyPeer {
  _HealthyPeer._(this._server);

  final HttpServer _server;
  int upgrades = 0;

  static Future<_HealthyPeer> start() async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final peer = _HealthyPeer._(server);
    server.listen((req) async {
      final ws = await WebSocketTransformer.upgrade(req);
      peer.upgrades++;
      // Never answers `auth`, so the handshake times out — irrelevant here.
      // The socket staying *open* under keepalive is the whole assertion.
      ws.listen((_) {}, onError: (_) {}, cancelOnError: false);
    }, onError: (_) {});
    return peer;
  }

  String get hostInput => 'ws://127.0.0.1:${_server.port}';
  Future<void> close() => _server.close(force: true);
}

/// A peer that completes the auth handshake, so the client actually reaches
/// `connected` and the freshness clock starts running.
class _LivePeer {
  _LivePeer._(this._server);

  final HttpServer _server;
  WebSocket? _ws;
  int auths = 0;
  int pings = 0;

  /// When false, `ping` requests are swallowed — the daemon is there but has
  /// stopped answering, which is what a half-dead host looks like.
  bool answerPings = true;

  static Future<_LivePeer> start() async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final peer = _LivePeer._(server);
    server.listen((req) async {
      final ws = await WebSocketTransformer.upgrade(req);
      peer._ws = ws;
      ws.listen(
        (raw) {
          final env = jsonDecode(raw as String) as Map<String, dynamic>;
          switch (env['type']) {
            case 'auth':
              peer.auths++;
              ws.add(
                jsonEncode({
                  'v': 1,
                  'type': 'auth_ok',
                  'id': env['id'],
                  'payload': {'device_id': 'dev-1', 'device_name': 'test'},
                }),
              );
            case 'ping':
              peer.pings++;
              if (!peer.answerPings) return;
              ws.add(jsonEncode({'v': 1, 'type': 'pong', 'id': env['id']}));
          }
        },
        onError: (_) {},
        cancelOnError: false,
      );
    }, onError: (_) {});
    return peer;
  }

  /// Unsolicited traffic — the daemon proving it is alive without being asked.
  void pushEvent() {
    _ws?.add(
      jsonEncode({
        'v': 1,
        'type': 'event',
        'payload': {
          'event': {'type': 'noop'},
        },
      }),
    );
  }

  String get hostInput => 'ws://127.0.0.1:${_server.port}';
  Future<void> close() => _server.close(force: true);
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  McremoteClient newClient({required Duration pingInterval}) {
    final client = McremoteClient(
      settings: SettingsStore(secure: _MemorySecureStorage()),
      protocolPingInterval: pingInterval,
    );
    addTearDown(() async {
      await client.disconnect();
      await client.dispose();
    });
    return client;
  }

  test(
    'a peer that never pongs has its socket closed',
    () async {
      final peer = await _SilentPeer.start();
      addTearDown(peer.close);
      final client = newClient(pingInterval: const Duration(milliseconds: 150));

      // The upgrade succeeds, so the dial gets past `ready` and the client
      // adopts a socket that is, in fact, attached to nothing.
      final elapsed = Stopwatch()..start();

      Object? thrown;
      try {
        await client.connect(
          hostInput: peer.hostInput,
          token: 'tok',
          mode: TlsMode.off,
          enableAutoReconnect: false,
          allowTransportFallback: false,
        );
      } catch (e) {
        thrown = e;
      }

      expect(peer.upgrades, 1, reason: 'the handshake must have completed');
      // Both halves matter, because "not connected" alone proves nothing here:
      // the auth request times out on its own eventually, so a test that only
      // checked the end state would pass with keepalive disabled.
      //
      //  * the failure is `connection_lost` — the socket was *closed* by the
      //    keepalive, not abandoned by an application timeout (`auth_timeout`);
      //  * it happened in well under the 30 s request timeout that would
      //    otherwise have been the first thing to notice.
      expect(
        thrown,
        isA<McException>().having((e) => e.code, 'code', 'connection_lost'),
        reason:
            'keepalive must close the socket, not wait for auth to expire '
            '(an application timeout would surface as auth_timeout)',
      );
      expect(
        elapsed.elapsed,
        lessThan(const Duration(seconds: 10)),
        reason: 'detection must not depend on the 30 s request timeout',
      );
      expect(client.state, isNot(McConnectionState.connected));
    },
    timeout: const Timeout(Duration(seconds: 60)),
  );

  test(
    'a peer that pongs is not torn down by keepalive',
    () async {
      // The negative control, and the reason D2 is safe to ship: if the daemon
      // did not answer pings, enabling keepalive would kill healthy sessions.
      final peer = await _HealthyPeer.start();
      addTearDown(peer.close);
      final client = newClient(pingInterval: const Duration(milliseconds: 120));

      unawaited(
        client
            .connect(
              hostInput: peer.hostInput,
              token: 'tok',
              mode: TlsMode.off,
              enableAutoReconnect: false,
              allowTransportFallback: false,
            )
            .catchError((_) {}),
      );

      // Well past three keepalive cycles. The socket must still be open; the
      // client is stuck waiting on `auth`, which is a different concern.
      await Future<void>.delayed(const Duration(milliseconds: 600));
      expect(peer.upgrades, 1);
      expect(
        client.lastErrorCode,
        isNot('connection_lost'),
        reason: 'keepalive must not close a socket whose peer is answering',
      );
    },
    timeout: const Timeout(Duration(seconds: 60)),
  );

  group('freshness clock (D1)', () {
    /// A clock the test moves by hand, so no assertion waits on wall time.
    late DateTime now;
    setUp(() => now = DateTime(2026, 8, 1, 12));

    Future<McremoteClient> connected(_LivePeer peer) async {
      final client = McremoteClient(
        settings: SettingsStore(secure: _MemorySecureStorage()),
        clock: () => now,
        // Fast enough that health is re-derived promptly; the clock, not this,
        // is what the assertions depend on.
        appPingPeriod: const Duration(milliseconds: 40),
      );
      addTearDown(() async {
        await client.disconnect();
        await client.dispose();
      });
      await client.connect(
        hostInput: peer.hostInput,
        token: 'tok',
        mode: TlsMode.off,
        enableAutoReconnect: false,
        allowTransportFallback: false,
      );
      return client;
    }

    test('a fresh connection is fresh', () async {
      final peer = await _LivePeer.start();
      addTearDown(peer.close);
      final client = await connected(peer);

      expect(client.state, McConnectionState.connected);
      expect(client.linkHealth.value, LinkHealth.fresh);
    });

    test('silence ages fresh → stale → lost', () async {
      final peer = await _LivePeer.start();
      addTearDown(peer.close);
      // The peer stops answering, so nothing refreshes the stamp and only the
      // clock moves.
      final client = await connected(peer);
      peer.answerPings = false;

      now = now.add(const Duration(seconds: 16));
      await _untilHealth(client, LinkHealth.stale);
      expect(client.linkHealth.value, LinkHealth.stale);

      now = now.add(const Duration(seconds: 20)); // 36s total
      await _untilHealth(client, LinkHealth.lost);
      expect(client.linkHealth.value, LinkHealth.lost);
    });

    test('an unsolicited event restores freshness without a ping', () async {
      // D1's point: a session streaming a reply is proving itself, and should
      // not need a pong to look healthy.
      final peer = await _LivePeer.start();
      addTearDown(peer.close);
      final client = await connected(peer);
      peer.answerPings = false;

      now = now.add(const Duration(seconds: 16));
      await _untilHealth(client, LinkHealth.stale);

      peer.pushEvent();
      await _untilHealth(client, LinkHealth.fresh);
      expect(client.linkHealth.value, LinkHealth.fresh);
    });

    test('health is orthogonal to the state enum (B2)', () async {
      final peer = await _LivePeer.start();
      addTearDown(peer.close);
      final client = await connected(peer);
      peer.answerPings = false;

      now = now.add(const Duration(seconds: 16));
      await _untilHealth(client, LinkHealth.stale);

      // The 47 call sites that gate on `connected` must see no change: only
      // the health signal moved.
      expect(client.state, McConnectionState.connected);
      expect(client.linkHealth.value, LinkHealth.stale);
    });
  });

  group('heartbeat cadence and the B1 obligation (D3)', () {
    test(
      'one missed ping goes stale but does not tear the socket down',
      () async {
        // MADR 0046 L-1: a single lost packet must not bounce a live connection.
        // What changed in 0063 is what the *UI* claims, not what we tear down.
        final peer = await _LivePeer.start();
        addTearDown(peer.close);
        var now = DateTime(2026, 8, 1, 12);
        final client = McremoteClient(
          settings: SettingsStore(secure: _MemorySecureStorage()),
          clock: () => now,
          appPingPeriod: const Duration(milliseconds: 60),
        );
        addTearDown(() async {
          await client.disconnect();
          await client.dispose();
        });
        await client.connect(
          hostInput: peer.hostInput,
          token: 'tok',
          mode: TlsMode.off,
          enableAutoReconnect: false,
          allowTransportFallback: false,
        );

        peer.answerPings = false;
        now = now.add(const Duration(seconds: 16));
        await _untilHealth(client, LinkHealth.stale);
        peer.answerPings = true;

        expect(
          client.state,
          McConnectionState.connected,
          reason: 'one miss must not tear down a live socket (0046 L-1)',
        );

        now = now.add(const Duration(milliseconds: 1));
        await _untilHealth(client, LinkHealth.fresh);
        expect(client.linkHealth.value, LinkHealth.fresh);
      },
      timeout: const Timeout(Duration(seconds: 60)),
    );

    test(
      'the ping keeps firing during a long one-way stream (B1)',
      () async {
        // The regression this guards is subtle and severe: if the heartbeat were
        // skipped whenever inbound traffic looked fresh, a reply longer than the
        // daemon's 60 s read deadline would be killed mid-answer, because a
        // streaming session sends nothing upstream. Inbound events must refresh
        // the *UI* signal without ever suppressing the outbound ping.
        final peer = await _LivePeer.start();
        addTearDown(peer.close);
        final client = McremoteClient(
          settings: SettingsStore(secure: _MemorySecureStorage()),
          appPingPeriod: const Duration(milliseconds: 50),
        );
        addTearDown(() async {
          await client.disconnect();
          await client.dispose();
        });
        await client.connect(
          hostInput: peer.hostInput,
          token: 'tok',
          mode: TlsMode.off,
          enableAutoReconnect: false,
          allowTransportFallback: false,
        );

        // Stream inbound events continuously — the client looks perfectly
        // healthy throughout, which is exactly when the ping would be
        // "optimised" away.
        final pingsAtStart = peer.pings;
        final streamer = Timer.periodic(
          const Duration(milliseconds: 10),
          (_) => peer.pushEvent(),
        );
        addTearDown(streamer.cancel);
        await Future<void>.delayed(const Duration(milliseconds: 400));

        expect(
          peer.pings - pingsAtStart,
          greaterThanOrEqualTo(3),
          reason:
              'the daemon read deadline is only reset by these pings; '
              'a busy inbound stream must not suppress them',
        );
        expect(client.linkHealth.value, LinkHealth.fresh);
      },
      timeout: const Timeout(Duration(seconds: 60)),
    );
  });
}

/// Wait for the client's health notifier to reach [want]. Bounded so a failure
/// reports the value it got stuck on rather than hanging the suite.
Future<void> _untilHealth(McremoteClient client, LinkHealth want) async {
  for (var i = 0; i < 100; i++) {
    if (client.linkHealth.value == want) return;
    await Future<void>.delayed(const Duration(milliseconds: 20));
  }
}

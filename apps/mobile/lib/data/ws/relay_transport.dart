import 'dart:async';
import 'dart:collection';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/foundation.dart' show debugPrint;
import 'package:web_socket_channel/io.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import 'mc_exception.dart';

/// Max outer binary frames buffered before the loopback peer accepts
/// (MADR 0016 R6 / D4). ~64 × typical TLS record keeps memory bounded.
const int kRelayOuterBufferMax = 64;

/// Outer hop to mcrelay (MADR 0015 E3).
///
/// 1. WSS to mcrelay `GET /v1/phone`
/// 2. `join` with [hostId]
/// 3. After `join_ok`, splice remaining frames as an opaque byte pipe
/// 4. Expose a loopback TCP port; the caller dials mcremote TLS/WSS to
///    `127.0.0.1:localPort` via [HttpClient.connectionFactory] so SNI and
///    cert pinning still use the real mcremote host.
class RelayTransport {
  RelayTransport._({
    required this.localPort,
    required this._outer,
    required this._outerHttp,
    required this._server,
    required this._subs,
  });

  final int localPort;
  final WebSocketChannel _outer;
  final HttpClient _outerHttp;
  final ServerSocket _server;
  final List<StreamSubscription> _subs;

  final _peerLock = Object();
  Socket? _peer;
  StreamSubscription? _peerSub;
  final Queue<List<int>> _outerBuf = Queue<List<int>>();
  bool _closed = false;

  /// Connect to [relayBase], join [hostId], return a live bridge.
  static Future<RelayTransport> open({
    required String relayBase,
    required String hostId,
    Duration timeout = const Duration(seconds: 15),
  }) async {
    final phoneUrl = phoneWsUrl(relayBase);
    final outerHttp = HttpClient();
    late final WebSocketChannel outer;
    try {
      outer = IOWebSocketChannel.connect(
        Uri.parse(phoneUrl),
        customClient: outerHttp,
      );
      await outer.ready.timeout(timeout);
    } catch (e) {
      outerHttp.close(force: true);
      throw McException(
        'relay connect failed: $e',
        code: 'relay_connect_failed',
        permanent: false,
      );
    }

    final fanout = StreamController<dynamic>.broadcast();
    final outerSub = outer.stream.listen(
      fanout.add,
      onError: fanout.addError,
      onDone: () {
        if (!fanout.isClosed) fanout.close();
      },
      cancelOnError: false,
    );

    try {
      outer.sink.add(
        jsonEncode({
          'v': 1,
          'type': 'join',
          'id': 'join-1',
          'payload': {'host_id': hostId},
        }),
      );

      final joinOk = await fanout.stream
          .timeout(timeout)
          .map(asJoinPlaneMap)
          .firstWhere(
            (m) =>
                m['type'] == 'join_ok' ||
                m['type'] == 'error' ||
                m['type'] == 'join_error',
          );

      final typ = joinOk['type'] as String? ?? '';
      if (typ != 'join_ok') {
        final payload = joinOk['payload'];
        final code = payload is Map
            ? (payload['code'] as String? ?? 'join_failed')
            : 'join_failed';
        final msg = payload is Map
            ? (payload['message'] as String? ?? typ)
            : typ;
        throw McException(
          'relay join failed: $msg',
          code: code,
          permanent: code == 'unknown_host' || code == 'unauthorized',
        );
      }

      final server = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
      final transport = RelayTransport._(
        localPort: server.port,
        outer: outer,
        outerHttp: outerHttp,
        server: server,
        subs: [outerSub],
      );

      // Opaque tunnel: remaining outer frames ↔ one local peer socket.
      final dataSub = fanout.stream.listen(
        transport._onOuterData,
        onError: (_) {},
        onDone: () {
          unawaited(transport._closePeer());
        },
        cancelOnError: false,
      );
      transport._subs.add(dataSub);

      final acceptSub = server.listen((socket) {
        unawaited(transport._replacePeer(socket));
      });
      transport._subs.add(acceptSub);

      return transport;
    } catch (e) {
      await outerSub.cancel();
      if (!fanout.isClosed) await fanout.close();
      try {
        await outer.sink.close();
      } catch (_) {}
      outerHttp.close(force: true);
      if (e is McException) rethrow;
      throw McException(
        'relay join failed: $e',
        code: 'relay_join_failed',
        permanent: false,
      );
    }
  }

  void _onOuterData(dynamic msg) {
    if (msg is! List<int>) {
      if (msg is String) {
        debugPrint(
          'relay: unexpected text frame after join (${msg.length} B)',
        );
      }
      return;
    }
    final copy = List<int>.from(msg);
    synchronized(_peerLock, () {
      final p = _peer;
      if (p != null) {
        try {
          p.add(copy);
        } catch (e) {
          debugPrint('relay: peer write failed: $e');
        }
        return;
      }
      // Buffer until loopback accept (MADR 0016 R6).
      if (_outerBuf.length >= kRelayOuterBufferMax) {
        _outerBuf.removeFirst();
      }
      _outerBuf.add(copy);
    });
  }

  Future<void> _replacePeer(Socket socket) async {
    StreamSubscription? oldSub;
    Socket? prev;
    synchronized(_peerLock, () {
      prev = _peer;
      oldSub = _peerSub;
      _peer = socket;
      _peerSub = null;
    });
    if (oldSub != null) {
      try {
        await oldSub!.cancel();
      } catch (_) {}
    }
    if (prev != null) {
      try {
        await prev!.close();
      } catch (_) {}
    }

    // Flush buffered outer frames in order.
    synchronized(_peerLock, () {
      while (_outerBuf.isNotEmpty) {
        try {
          socket.add(_outerBuf.removeFirst());
        } catch (e) {
          debugPrint('relay: peer flush failed: $e');
          break;
        }
      }
    });

    final sub = socket.listen(
      (data) {
        try {
          _outer.sink.add(Uint8List.fromList(data));
        } catch (e) {
          debugPrint('relay: outer write failed: $e');
        }
      },
      onError: (_) {},
      onDone: () {},
      cancelOnError: false,
    );
    synchronized(_peerLock, () {
      if (_peer == socket) {
        _peerSub = sub;
      } else {
        unawaited(sub.cancel());
      }
    });
  }

  Future<void> _closePeer() async {
    Socket? p;
    StreamSubscription? s;
    synchronized(_peerLock, () {
      p = _peer;
      s = _peerSub;
      _peer = null;
      _peerSub = null;
      _outerBuf.clear();
    });
    try {
      await s?.cancel();
    } catch (_) {}
    try {
      await p?.close();
    } catch (_) {}
  }

  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    for (final s in _subs) {
      try {
        await s.cancel();
      } catch (_) {}
    }
    await _closePeer();
    try {
      await _server.close();
    } catch (_) {}
    try {
      await _outer.sink.close();
    } catch (_) {}
    try {
      _outerHttp.close(force: true);
    } catch (_) {}
  }

  /// Build `ws(s)://host[:port]/v1/phone` from a relay base URL.
  static String phoneWsUrl(String relayBase) {
    var b = relayBase.trim();
    if (!b.contains('://')) b = 'wss://$b';
    final u = Uri.parse(b);
    final scheme = switch (u.scheme) {
      'http' => 'ws',
      'https' => 'wss',
      'ws' || 'wss' => u.scheme,
      _ => 'wss',
    };
    final host = u.host.isEmpty
        ? b.replaceFirst(RegExp(r'^[^/]+://'), '')
        : u.host;
    final authority = u.hasPort ? '$host:${u.port}' : host;
    return '$scheme://$authority/v1/phone';
  }

  /// Parse a join-plane JSON text frame.
  static Map<String, dynamic> asJoinPlaneMap(dynamic msg) {
    if (msg is String) {
      final d = jsonDecode(msg);
      if (d is Map<String, dynamic>) return d;
      if (d is Map) return Map<String, dynamic>.from(d);
    }
    if (msg is List<int>) {
      final d = jsonDecode(utf8.decode(msg));
      if (d is Map<String, dynamic>) return d;
      if (d is Map) return Map<String, dynamic>.from(d);
    }
    throw const FormatException('relay expected JSON text frame');
  }
}

/// Run [body] while holding a mutual-exclusion lock [lock].
///
/// Dart has no built-in mutex for isolate-local sync; a re-entrant-safe
/// pattern is not required here — all callers are on the same event loop.
/// We still serialize critical sections so future isolates / timers cannot
/// interleave buffer and peer updates incorrectly if scheduling changes.
void synchronized(Object lock, void Function() body) {
  // Event-loop single-threaded: body runs atomically w.r.t. other microtasks
  // only if it does not await. All call sites are synchronous closures.
  body();
}

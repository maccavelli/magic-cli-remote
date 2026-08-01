import 'dart:async';
import 'dart:collection';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/foundation.dart' show debugPrint, visibleForTesting;
import 'package:web_socket_channel/io.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import 'mc_exception.dart';

/// Max outer binary frames buffered before the loopback peer accepts
/// (MADR 0016 R6 / D4). ~64 × typical TLS record keeps memory bounded.
const int kRelayOuterBufferMax = 64;

/// Exact byte cap for pre-peer outer frames (MADR 0056 H-3). Bound memory on
/// phone; never drop bytes to stay under it — reject the frame instead.
const int kRelayOuterBufferMaxBytes = 256 * 1024;

/// Enqueue one outer binary frame while the loopback peer is not yet attached.
///
/// Returns `false` when the frame was rejected (fail-closed overflow). On
/// `false` the queue is **unchanged** — never drop bytes from an opaque
/// TLS/TCP stream (MADR 0056 H-3).
@visibleForTesting
bool enqueueRelayOuterFrame(
  Queue<List<int>> buf,
  List<int> frame, {
  int maxFrames = kRelayOuterBufferMax,
  int maxBytes = kRelayOuterBufferMaxBytes,
}) {
  var bytes = 0;
  for (final f in buf) {
    bytes += f.length;
  }
  if (buf.length >= maxFrames || bytes + frame.length > maxBytes) {
    return false;
  }
  buf.add(frame);
  return true;
}

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

  /// First fail-closed transport error (overflow, unexpected text, outer error).
  McException? failure;

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
        onError: (Object e) {
          transport._failClosed(
            McException(
              'relay outer stream error: $e',
              code: 'relay_outer_error',
              permanent: false,
            ),
          );
        },
        onDone: () {
          unawaited(transport._closePeer());
        },
        cancelOnError: false,
      );
      transport._subs.add(dataSub);

      final acceptSub = server.listen((socket) {
        // The loopback listener is a one-peer capability. Closing it as soon
        // as the TLS client attaches prevents a co-resident process from
        // replacing that peer and splicing/evicting the tunnel.
        unawaited(server.close());
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
    if (_closed) return;
    if (msg is! List<int>) {
      if (msg is String) {
        _failClosed(
          McException(
            'relay unexpected text frame after join (${msg.length} B)',
            code: 'relay_protocol_violation',
            permanent: false,
          ),
        );
      }
      return;
    }
    final bytes = msg is Uint8List ? msg : Uint8List.fromList(msg);
    var overflow = false;
    var peerWriteFailed = false;
    synchronized(_peerLock, () {
      final p = _peer;
      if (p != null) {
        try {
          p.add(bytes);
        } catch (e) {
          peerWriteFailed = true;
          debugPrint('relay: peer write failed: $e');
        }
        return;
      }
      // Buffer until loopback accept — never drop bytes (MADR 0056 H-3).
      if (!enqueueRelayOuterFrame(_outerBuf, bytes)) {
        overflow = true;
      }
    });
    if (overflow) {
      _failClosed(
        McException(
          'relay outer buffer overflow',
          code: 'relay_buffer_overflow',
          permanent: false,
        ),
      );
    } else if (peerWriteFailed) {
      _failClosed(
        McException(
          'relay peer write failed',
          code: 'relay_peer_write_failed',
          permanent: false,
        ),
      );
    }
  }

  void _failClosed(McException err) {
    failure ??= err;
    debugPrint('relay: fail-closed ${err.code}: ${err.message}');
    unawaited(close());
  }

  Future<void> _replacePeer(Socket socket) async {
    StreamSubscription? oldSub;
    var alreadyHasPeer = false;
    synchronized(_peerLock, () {
      if (_peer != null) {
        alreadyHasPeer = true;
        return;
      }
      oldSub = _peerSub;
      _peer = socket;
      _peerSub = null;
    });
    if (alreadyHasPeer) {
      socket.destroy();
      return;
    }
    if (oldSub != null) {
      try {
        await oldSub!.cancel();
      } catch (_) {}
    }
    // A write that fails asynchronously is reported on `done`, which nothing
    // else listens to — an unhandled async error rather than a diagnosable
    // one (MADR 0046 L-4).
    unawaited(
      socket.done.catchError((Object e) {
        debugPrint('relay: peer socket ended with an error: $e');
        return socket;
      }),
    );

    // Flush buffered outer frames in order; partial flush is fail-closed.
    var flushFailed = false;
    synchronized(_peerLock, () {
      while (_outerBuf.isNotEmpty) {
        try {
          socket.add(_outerBuf.removeFirst());
        } catch (e) {
          debugPrint('relay: peer flush failed: $e');
          flushFailed = true;
          _outerBuf.clear();
          break;
        }
      }
    });
    if (flushFailed) {
      _failClosed(
        McException(
          'relay peer flush failed',
          code: 'relay_peer_write_failed',
          permanent: false,
        ),
      );
      return;
    }

    final sub = socket.listen(
      (data) {
        try {
          _outer.sink.add(data);
        } catch (e) {
          debugPrint('relay: outer write failed: $e');
        }
      },
      // The loopback leg *is* the tunnel's client half. Once it goes, frames
      // from the outer hop have nowhere to land, so the transport ends rather
      // than writing into a closed socket.
      onError: (Object e) {
        debugPrint('relay: peer read failed: $e');
        unawaited(close());
      },
      onDone: () => unawaited(close()),
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

  /// Build `http(s)://host[:port]/healthz` from a relay base URL.
  ///
  /// Separate from [phoneWsUrl] on purpose: that one normalises to the
  /// `wss://…/v1/phone` join endpoint, which does not answer a plain GET.
  static String healthzUrl(String relayBase) {
    var b = relayBase.trim();
    if (!b.contains('://')) b = 'https://$b';
    final u = Uri.parse(b);
    final scheme = switch (u.scheme) {
      'ws' || 'http' => 'http',
      'wss' || 'https' => 'https',
      _ => 'https',
    };
    final host = u.host.isEmpty
        ? b.replaceFirst(RegExp(r'^[^/]+://'), '')
        : u.host;
    final authority = u.hasPort ? '$host:${u.port}' : host;
    return '$scheme://$authority/healthz';
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

@TestOn('vm')
library;

import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
import 'package:shared_preferences/shared_preferences.dart';

// MADR 0068 P4 (U4, dart side) — the resume round trip: the client stores
// the auth_ok resume_token, and a within-window reconnect's auth carries
// the token plus the transcripts' seq claims; the resumed block surfaces
// on the client for the synchronizer's fast path.

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

/// A v2 daemon fake: answers auth with a resume token + caps, records every
/// auth payload, and honours a resume claim with a canned `resumed` block.
class _ResumePeer {
  _ResumePeer._(this._server);

  final HttpServer _server;
  final authPayloads = <Map<String, dynamic>>[];
  WebSocket? _ws;
  int upgrades = 0;

  static Future<_ResumePeer> start() async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final peer = _ResumePeer._(server);
    server.listen((req) async {
      final ws = await WebSocketTransformer.upgrade(req);
      peer.upgrades++;
      peer._ws = ws;
      ws.listen(
        (raw) {
          final env = jsonDecode(raw as String) as Map<String, dynamic>;
          switch (env['type']) {
            case 'auth':
              final payload = Map<String, dynamic>.from(
                env['payload'] as Map? ?? const {},
              );
              peer.authPayloads.add(payload);
              final resumed = payload['resume'] != null
                  ? {
                      'sessions': {
                        'A': {'first_seq': 1, 'latest_seq': 2},
                      },
                    }
                  : null;
              ws.add(
                jsonEncode({
                  'v': 2,
                  'type': 'auth_ok',
                  'id': env['id'],
                  'payload': {
                    'device_id': 'dev-1',
                    'device_name': 'test',
                    'protocol': 2,
                    'resume_token': 'tok-${peer.upgrades}',
                    'caps': {
                      'protocol': 2,
                      'resume': {'window_ms': 120000},
                    },
                    'resumed': ?resumed,
                  },
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

  Future<void> dropAbnormally() async => _ws?.close(1011, 'boom');

  String get hostInput => 'ws://127.0.0.1:${_server.port}';
  Future<void> close() => _server.close(force: true);
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  test('reconnect within the window carries the token and seq claims; '
      'resumed surfaces on the client', () async {
    final peer = await _ResumePeer.start();
    addTearDown(peer.close);
    final client = McremoteClient(
      settings: SettingsStore(secure: _MemorySecureStorage()),
    );
    addTearDown(() async {
      await client.disconnect();
      await client.dispose();
    });
    client.resumeSeqSource = () => {'A': 2};

    await client.connect(
      hostInput: peer.hostInput,
      token: 'tok',
      mode: TlsMode.off,
      allowTransportFallback: false,
    );
    expect(client.state, McConnectionState.connected);
    expect(
      peer.authPayloads.single.containsKey('resume'),
      isFalse,
      reason: 'first auth has no token to resume with',
    );

    // Abnormal drop → auto-reconnect → the second auth resumes.
    await peer.dropAbnormally();
    await Future<void>.delayed(const Duration(milliseconds: 2500));

    expect(peer.upgrades, greaterThan(1), reason: 'must have reconnected');
    final second = peer.authPayloads[1];
    final resume = second['resume'] as Map?;
    expect(resume, isNotNull, reason: 'within-window auth must claim resume');
    expect(resume!['token'], 'tok-1');
    expect((resume['sessions'] as Map)['A'], 2);
    expect(client.lastResumed, isNotNull);
    expect(client.lastResumed!['A']!.latest, 2);
  });
}

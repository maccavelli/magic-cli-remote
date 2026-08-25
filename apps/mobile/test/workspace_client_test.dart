import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/ws/mc_exception.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Workspace client decoding against real frames (MADR 0112 A5, PLAN P6).
///
/// Deliberately no TestWidgetsFlutterBinding.ensureInitialized(): that binding
/// installs a mock HTTP client which refuses the real loopback WebSocket these
/// tests need, exactly as the existing client tests avoid it.

class _StubDaemon {
  _StubDaemon._(this._server);

  final HttpServer _server;

  /// Reply builder keyed by request type. Returning null sends nothing, which
  /// exercises the client's timeout path.
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
  setUp(() {
    SharedPreferences.setMockInitialValues({});
    FlutterSecureStorage.setMockInitialValues({});
  });

  test('a directory listing decodes and drops rows with no path', () async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    daemon.replies['workspace.list'] = (req) => {
      'v': 1,
      'type': 'workspace.list_result',
      'id': req['id'],
      'payload': {
        'session_id': 's1',
        'path': '',
        'entries': [
          {'name': 'lib', 'path': 'lib', 'dir': true},
          {'name': 'go.mod', 'path': 'go.mod'},
          {'name': 'broken', 'path': ''},
          'not-a-map',
        ],
      },
    };
    final client = await _connected(daemon);
    addTearDown(client.dispose);
    final entries = await client.listWorkspace('s1');
    expect(entries, hasLength(2));
    expect(entries.first.dir, isTrue);
    expect(entries.last.path, 'go.mod');
  });

  test('a path is sent only when it is non-empty', () async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    Map<String, dynamic>? seen;
    daemon.replies['workspace.list'] = (req) {
      seen = (req['payload'] as Map).cast<String, dynamic>();
      return {
        'v': 1,
        'type': 'workspace.list_result',
        'id': req['id'],
        'payload': {'session_id': 's1', 'entries': <dynamic>[]},
      };
    };
    final client = await _connected(daemon);
    addTearDown(client.dispose);
    await client.listWorkspace('s1');
    expect(seen!.containsKey('path'), isFalse);
    await client.listWorkspace('s1', path: 'lib');
    expect(seen!['path'], 'lib');
  });

  test('file content decodes with its byte count', () async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    daemon.replies['workspace.read'] = (req) => {
      'v': 1,
      'type': 'workspace.read_result',
      'id': req['id'],
      'payload': {
        'session_id': 's1',
        'path': 'go.mod',
        'text': 'module example',
        'bytes': 14,
      },
    };
    final client = await _connected(daemon);
    addTearDown(client.dispose);
    final content = await client.readWorkspace('s1', 'go.mod');
    expect(content.path, 'go.mod');
    expect(content.text, 'module example');
    expect(content.bytes, 14);
  });

  test('a read refusal becomes an exception carrying its code', () async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    daemon.replies['workspace.read'] = (req) => {
      'v': 1,
      'type': 'error',
      'id': req['id'],
      'payload': {'code': 'path_symlink', 'message': 'refused'},
    };
    final client = await _connected(daemon);
    addTearDown(client.dispose);
    await expectLater(
      client.readWorkspace('s1', 'link.txt'),
      throwsA(isA<McException>().having((e) => e.code, 'code', 'path_symlink')),
    );
  });

  test('search results decode with the cap that applied', () async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    Map<String, dynamic>? seen;
    daemon.replies['workspace.search'] = (req) {
      seen = (req['payload'] as Map).cast<String, dynamic>();
      return {
        'v': 1,
        'type': 'workspace.search_result',
        'id': req['id'],
        'payload': {
          'session_id': 's1',
          'kind': 'text',
          'cap': 10,
          'truncated': true,
          'matches': [
            {'path': 'a.go', 'line': 3, 'column': 5, 'text': 'hit'},
          ],
        },
      };
    };
    final client = await _connected(daemon);
    addTearDown(client.dispose);
    final res = await client.searchWorkspace('s1', kind: 'text', query: 'hit');
    expect(seen!['kind'], 'text');
    expect(seen!['query'], 'hit');
    expect(res.cap, 10);
    expect(res.truncated, isTrue);
    expect(res.matches.single.line, 3);
    expect(res.matches.single.column, 5);
  });

  test('a search refusal surfaces its code', () async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    daemon.replies['workspace.search'] = (req) => {
      'v': 1,
      'type': 'error',
      'id': req['id'],
      'payload': {'code': 'invalid_query', 'message': 'refused'},
    };
    final client = await _connected(daemon);
    addTearDown(client.dispose);
    await expectLater(
      client.searchWorkspace('s1', kind: 'text', query: ''),
      throwsA(
        isA<McException>().having((e) => e.code, 'code', 'invalid_query'),
      ),
    );
  });

  test('a listing refusal surfaces its code', () async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    daemon.replies['workspace.list'] = (req) => {
      'v': 1,
      'type': 'error',
      'id': req['id'],
      'payload': {'code': 'path_escape', 'message': 'refused'},
    };
    final client = await _connected(daemon);
    addTearDown(client.dispose);
    await expectLater(
      client.listWorkspace('s1', path: '..'),
      throwsA(isA<McException>().having((e) => e.code, 'code', 'path_escape')),
    );
  });

  test('a malformed entries payload yields an empty list', () async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    daemon.replies['workspace.list'] = (req) => {
      'v': 1,
      'type': 'workspace.list_result',
      'id': req['id'],
      'payload': {'session_id': 's1', 'entries': 'not-a-list'},
    };
    final client = await _connected(daemon);
    addTearDown(client.dispose);
    expect(await client.listWorkspace('s1'), isEmpty);
  });

  test('refresh_skills answers ok', () async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    Map<String, dynamic>? seen;
    daemon.replies['session.refresh_skills'] = (req) {
      seen = (req['payload'] as Map).cast<String, dynamic>();
      return {
        'v': 1,
        'type': 'ok',
        'id': req['id'],
        'payload': <String, dynamic>{},
      };
    };
    final client = await _connected(daemon);
    addTearDown(client.dispose);
    await client.refreshSkills('s1');
    expect(seen!['session_id'], 's1');
    // The request carries no path or content: the daemon has no skill writer.
    expect(seen!.keys, ['session_id']);
  });

  test('a busy instance surfaces instance_busy', () async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    daemon.replies['session.refresh_skills'] = (req) => {
      'v': 1,
      'type': 'error',
      'id': req['id'],
      'payload': {'code': 'instance_busy', 'message': 'busy'},
    };
    final client = await _connected(daemon);
    addTearDown(client.dispose);
    await expectLater(
      client.refreshSkills('s1'),
      throwsA(
        isA<McException>().having((e) => e.code, 'code', 'instance_busy'),
      ),
    );
  });
}

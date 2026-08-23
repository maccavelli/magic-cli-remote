import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/ws/mc_exception.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// A daemon stub that authenticates and then answers one scripted reply per
/// request type, so the client's `projects.list` decode contract can be tested
/// against real frames (MADR 0112 A1).
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
  // Deliberately no TestWidgetsFlutterBinding.ensureInitialized(): that binding
  // installs a mock HTTP client which refuses the real loopback WebSocket these
  // tests need, exactly as the existing client tests avoid it.
  setUp(() {
    SharedPreferences.setMockInitialValues({});
    FlutterSecureStorage.setMockInitialValues({});
  });

  test('decodes a project list and drops unusable rows', () async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    daemon.replies['projects.list'] = (req) => {
      'v': 1,
      'type': 'projects.list_result',
      'id': req['id'],
      'payload': {
        'provider': 'opencode',
        'projects': [
          {'id': 'p1', 'name': 'repo', 'worktree': '/work/repo'},
          // Rows the daemon should already have filtered, defended again here:
          // a picker entry with no worktree cannot start a session.
          {'id': 'p2', 'name': 'broken', 'worktree': ''},
          {'id': '', 'name': 'no-id', 'worktree': '/work/x'},
          'not-a-map',
        ],
      },
    };

    final client = await _connected(daemon);
    addTearDown(client.dispose);

    final projects = await client.listProjects('opencode');
    expect(projects, hasLength(1));
    expect(projects.single.id, 'p1');
    expect(projects.single.worktree, '/work/repo');
  });

  test('an empty project list is a valid answer, not an error', () async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    daemon.replies['projects.list'] = (req) => {
      'v': 1,
      'type': 'projects.list_result',
      'id': req['id'],
      'payload': {'provider': 'opencode', 'projects': <dynamic>[]},
    };

    final client = await _connected(daemon);
    addTearDown(client.dispose);

    expect(await client.listProjects('opencode'), isEmpty);
  });

  test(
    'a missing projects key decodes as empty rather than throwing',
    () async {
      final daemon = await _StubDaemon.start();
      addTearDown(daemon.close);
      daemon.replies['projects.list'] = (req) => {
        'v': 1,
        'type': 'projects.list_result',
        'id': req['id'],
        'payload': {'provider': 'opencode'},
      };

      final client = await _connected(daemon);
      addTearDown(client.dispose);

      expect(await client.listProjects('opencode'), isEmpty);
    },
  );

  test('an unsupported provider surfaces as an exception', () async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    daemon.replies['projects.list'] = (req) => {
      'v': 1,
      'type': 'error',
      'id': req['id'],
      'payload': {
        'code': 'unsupported',
        'message': 'provider does not support project discovery',
      },
    };

    final client = await _connected(daemon);
    addTearDown(client.dispose);

    // It must throw rather than return an empty list: an empty picker would
    // read as "this host has no projects" instead of "this provider cannot
    // enumerate them".
    await expectLater(
      client.listProjects('opencode'),
      throwsA(isA<McException>()),
    );
  });

  test('a provider failure surfaces as an exception', () async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    daemon.replies['projects.list'] = (req) => {
      'v': 1,
      'type': 'error',
      'id': req['id'],
      'payload': {
        'code': 'projects_list_failed',
        'message': 'engine unreachable',
      },
    };

    final client = await _connected(daemon);
    addTearDown(client.dispose);

    await expectLater(
      client.listProjects('opencode'),
      throwsA(isA<McException>()),
    );
  });

  test('sends the provider in the request payload', () async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    Map<String, dynamic>? seen;
    daemon.replies['projects.list'] = (req) {
      seen = req;
      return {
        'v': 1,
        'type': 'projects.list_result',
        'id': req['id'],
        'payload': {'provider': 'opencode', 'projects': <dynamic>[]},
      };
    };

    final client = await _connected(daemon);
    addTearDown(client.dispose);
    await client.listProjects('opencode');

    expect(seen, isNotNull);
    expect(seen!['type'], 'projects.list');
    expect((seen!['payload'] as Map)['provider'], 'opencode');
  });

  test('native sessions carry the additive discovery fields', () async {
    final daemon = await _StubDaemon.start();
    addTearDown(daemon.close);
    daemon.replies['agent_sessions.list'] = (req) => {
      'v': 1,
      'type': 'agent_sessions.list_result',
      'id': req['id'],
      'payload': {
        'provider': 'opencode',
        'sessions': [
          {
            'id': 'ses_1',
            'cwd': '/work/repo',
            'title': 'Refactor',
            'updated_at': '2026-08-22T10:00:00Z',
            'model_id': 'opencode/big-pickle',
            'thinking_level': 'high',
            'agent': 'build',
            'aggregate': {'input': 10, 'output': 5, 'cost_usd': 0.0},
          },
          // A legacy row from an older daemon must still be usable.
          {'id': 'ses_legacy', 'title': 'Old'},
        ],
      },
    };

    final client = await _connected(daemon);
    addTearDown(client.dispose);

    final sessions = await client.listAgentSessions('opencode');
    expect(sessions, hasLength(2));
    final rich = sessions.firstWhere((s) => s.id == 'ses_1');
    expect(rich.modelId, 'opencode/big-pickle');
    expect(rich.thinkingLevel, 'high');
    expect(rich.agent, 'build');
    expect(rich.aggregate, isNotNull);
    expect(rich.aggregate!.costUsd, 0);
    expect(rich.aggregate!.totalTokens, 15);

    final legacy = sessions.firstWhere((s) => s.id == 'ses_legacy');
    expect(legacy.modelId, isEmpty);
    expect(legacy.aggregate, isNull);
  });
}

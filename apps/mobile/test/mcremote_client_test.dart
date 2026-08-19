import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/ws/mc_exception.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _AuthServer {
  _AuthServer._(this._server, this.deviceId);

  final HttpServer _server;
  final String deviceId;
  var connections = 0;

  String get host =>
      'ws://${InternetAddress.loopbackIPv4.address}:${_server.port}';

  static Future<_AuthServer> start(
    String deviceId, {
    Map<String, dynamic> extraPayload = const <String, dynamic>{},
  }) async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final result = _AuthServer._(server, deviceId);
    unawaited(
      server.forEach((request) async {
        if (!WebSocketTransformer.isUpgradeRequest(request)) {
          request.response.statusCode = HttpStatus.notFound;
          await request.response.close();
          return;
        }
        final socket = await WebSocketTransformer.upgrade(request);
        result.connections++;
        socket.listen((raw) {
          final request = jsonDecode(raw as String) as Map<String, dynamic>;
          final type = request['type'];
          if (type != 'auth' && type != 'pair.claim') return;
          socket.add(
            jsonEncode({
              'v': 1,
              'type': type == 'auth' ? 'auth_ok' : 'pair_ok',
              'id': request['id'],
              'payload': {
                'device_id': deviceId,
                'device_name': deviceId,
                if (type == 'pair.claim') 'token': 'mcr_$deviceId',
                ...extraPayload,
              },
            }),
          );
        });
      }),
    );
    return result;
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

  test(
    'a stale post-open attempt cannot tear down the newer connection',
    () async {
      final a = await _AuthServer.start('device-a');
      final b = await _AuthServer.start('device-b');
      addTearDown(a.close);
      addTearDown(b.close);

      final openedA = Completer<void>();
      final releaseA = Completer<void>();
      var opens = 0;
      final client = McremoteClient(
        settings: SettingsStore(secure: _MemorySecureStorage()),
        afterSocketOpen: () async {
          if (opens++ == 0) {
            openedA.complete();
            await releaseA.future;
          }
        },
      );
      addTearDown(client.dispose);

      final first = client.connect(
        hostInput: a.host,
        token: 'token-a',
        mode: TlsMode.off,
      );
      await openedA.future.timeout(const Duration(seconds: 2));

      await client.connect(
        hostInput: b.host,
        token: 'token-b',
        mode: TlsMode.off,
      );
      releaseA.complete();
      await first.timeout(const Duration(seconds: 2));

      expect(client.state, McConnectionState.connected);
      expect(client.lastHostInput, b.host);
      expect(client.deviceId, 'device-b');
      expect(b.connections, 1);
    },
  );

  test('a stale pair attempt cannot overwrite the winning pair', () async {
    final a = await _AuthServer.start('device-a');
    final b = await _AuthServer.start('device-b');
    addTearDown(a.close);
    addTearDown(b.close);

    final openedA = Completer<void>();
    final releaseA = Completer<void>();
    var opens = 0;
    final client = McremoteClient(
      settings: SettingsStore(secure: _MemorySecureStorage()),
      afterSocketOpen: () async {
        if (opens++ == 0) {
          openedA.complete();
          await releaseA.future;
        }
      },
    );
    addTearDown(client.dispose);

    final first = client.claimPairCode(
      hostInput: a.host,
      code: 'K7M2-9X4P',
      mode: TlsMode.off,
    );
    await openedA.future.timeout(const Duration(seconds: 2));

    final token = await client.claimPairCode(
      hostInput: b.host,
      code: 'K7M2-9X4P',
      mode: TlsMode.off,
    );
    releaseA.complete();
    await expectLater(first, throwsA(isA<McException>()));

    expect(token, 'mcr_device-b');
    expect(client.state, McConnectionState.connected);
    expect(client.lastHostInput, b.host);
    expect(client.deviceId, 'device-b');
  });

  test('auth_ok display_name is captured', () async {
    final server = await _AuthServer.start(
      'dev',
      extraPayload: const {'display_name': 'Studio Mac'},
    );
    addTearDown(server.close);
    final client = McremoteClient(
      settings: SettingsStore(secure: _MemorySecureStorage()),
    );
    addTearDown(client.dispose);

    await client.connect(
      hostInput: server.host,
      token: 'token',
      mode: TlsMode.off,
    );

    expect(client.hostDisplayName, 'Studio Mac');
    expect(client.hostDisplayNameListenable.value, 'Studio Mac');
  });

  test('auth_ok without display_name leaves the field null', () async {
    final server = await _AuthServer.start('dev');
    addTearDown(server.close);
    final client = McremoteClient(
      settings: SettingsStore(secure: _MemorySecureStorage()),
    );
    addTearDown(client.dispose);

    await client.connect(
      hostInput: server.host,
      token: 'token',
      mode: TlsMode.off,
    );

    expect(client.hostDisplayName, isNull);
    expect(client.hostDisplayNameListenable.value, isNull);
  });

  test('pair_ok display_name is captured', () async {
    final server = await _AuthServer.start(
      'dev',
      extraPayload: const {'display_name': 'Studio Mac'},
    );
    addTearDown(server.close);
    final client = McremoteClient(
      settings: SettingsStore(secure: _MemorySecureStorage()),
    );
    addTearDown(client.dispose);

    await client.claimPairCode(
      hostInput: server.host,
      code: 'K7M2-9X4P',
      mode: TlsMode.off,
    );

    expect(client.hostDisplayName, 'Studio Mac');
    expect(client.hostDisplayNameListenable.value, 'Studio Mac');
  });

  test('display_name clears when the host authority changes', () async {
    final a = await _AuthServer.start(
      'device-a',
      extraPayload: const {'display_name': 'Studio Mac'},
    );
    final b = await _AuthServer.start('device-b');
    addTearDown(a.close);
    addTearDown(b.close);
    final client = McremoteClient(
      settings: SettingsStore(secure: _MemorySecureStorage()),
    );
    addTearDown(client.dispose);

    await client.connect(
      hostInput: a.host,
      token: 'token-a',
      mode: TlsMode.off,
    );
    expect(client.hostDisplayName, 'Studio Mac');

    await client.connect(
      hostInput: b.host,
      token: 'token-b',
      mode: TlsMode.off,
    );
    expect(client.hostDisplayName, isNull);
    expect(client.hostDisplayNameListenable.value, isNull);
  });
}

@TestOn('vm')
library;

import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/protocol/transport_policy.dart';
import 'package:magic_cli_remote/data/ws/mc_exception.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
import 'package:magic_cli_remote/data/ws/transport_probes.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// DialEpisode behaviour (MADR 0062 D4/D10/D11 + amendments A1, A2, A5).
///
/// Real loopback sockets, deliberately. What is being asserted is *which
/// endpoint got dialled and how many times* — a fake channel could not
/// distinguish "fell back to the relay" from "claimed it did", and the
/// credential-burn regression (A1) is precisely a case where the client
/// believes it is retrying something safe.
///
/// The relay endpoint here never speaks the mcrelay join protocol: reaching it
/// at all is the signal. A dial that touches the relay port proves the episode
/// hopped; a relay port with zero connections proves it did not.

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

/// Probes with scripted answers, so no test depends on the host's real network.
class _FakeProbes extends TransportProbes {
  _FakeProbes({this.meshUp = false, this.relayUp = false});

  final bool meshUp;
  final bool relayUp;
  int meshCalls = 0;
  int relayCalls = 0;

  @override
  Future<bool> mesh(
    String hostInput, {
    Duration timeout = kTransportProbeTimeout,
  }) async {
    meshCalls++;
    return meshUp;
  }

  @override
  Future<bool> relay(
    String relayBase, {
    Duration timeout = kTransportProbeTimeout,
  }) async {
    relayCalls++;
    return relayUp;
  }
}

McremoteClient _newClient({TransportProbes? probes, SettingsStore? settings}) {
  final client = McremoteClient(
    settings: settings ?? SettingsStore(secure: _MemorySecureStorage()),
    probes: probes ?? _FakeProbes(),
  );
  addTearDown(() async {
    await client.disconnect();
    await client.dispose();
  });
  return client;
}

/// A port nothing is listening on: `connect` is refused immediately.
Future<int> _deadPort() async {
  final probe = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
  final port = probe.port;
  await probe.close();
  return port;
}

/// A TCP endpoint that counts connections and drops them. Stands in for a
/// relay: the count is the whole assertion.
class _CountingEndpoint {
  _CountingEndpoint._(this._server);

  final ServerSocket _server;
  int connections = 0;

  static Future<_CountingEndpoint> start() async {
    final server = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
    final endpoint = _CountingEndpoint._(server);
    server.listen((socket) {
      endpoint.connections++;
      socket.destroy();
    });
    return endpoint;
  }

  int get port => _server.port;
  String get base => 'ws://${InternetAddress.loopbackIPv4.address}:$port';
  Future<void> close() => _server.close();
}

/// A WebSocket endpoint that completes the upgrade, then answers the first
/// request with an `error` envelope carrying [code].
///
/// The upgrade succeeding is what matters: it puts the episode *past* the
/// point where a pair code goes on the wire, which is the only place A1 can be
/// observed.
class _RespondingEndpoint {
  _RespondingEndpoint._(this._server);

  final HttpServer _server;
  int upgrades = 0;
  final requests = <String>[];

  static Future<_RespondingEndpoint> start({required String errorCode}) async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final endpoint = _RespondingEndpoint._(server);
    server.listen((req) async {
      final socket = await WebSocketTransformer.upgrade(req);
      endpoint.upgrades++;
      socket.listen((raw) {
        final decoded = jsonDecode(raw as String) as Map<String, dynamic>;
        endpoint.requests.add(decoded['type'] as String? ?? '');
        socket.add(
          jsonEncode({
            'v': 1,
            'type': 'error',
            'id': decoded['id'],
            'payload': {'code': errorCode, 'message': 'scripted $errorCode'},
          }),
        );
      });
    });
    return endpoint;
  }

  int get port => _server.port;
  String get host => 'ws://${InternetAddress.loopbackIPv4.address}:$port';
  Future<void> close() => _server.close(force: true);
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  group('automatic fallback', () {
    test('a retryable mesh failure hops to the relay exactly once', () async {
      final relay = await _CountingEndpoint.start();
      addTearDown(relay.close);
      final deadMeshPort = await _deadPort();
      final client = _newClient();

      await expectLater(
        client.connect(
          hostInput:
              'ws://${InternetAddress.loopbackIPv4.address}:'
              '$deadMeshPort',
          token: 'token',
          mode: TlsMode.off,
          transport: TransportMode.mesh,
          enableAutoReconnect: false,
          relayUrl: relay.base,
          relayHostId: 'hid-1',
        ),
        throwsA(isA<McException>()),
      );

      // The mesh dial was refused (`connect_failed`, retryable) and the episode
      // spent its one alternate on the relay.
      expect(relay.connections, 1);
    });

    test('no relay configured means no hop and no invented path', () async {
      final deadMeshPort = await _deadPort();
      final client = _newClient();

      await expectLater(
        client.connect(
          hostInput:
              'ws://${InternetAddress.loopbackIPv4.address}:'
              '$deadMeshPort',
          token: 'token',
          mode: TlsMode.off,
          transport: TransportMode.mesh,
          enableAutoReconnect: false,
        ),
        // A refused dial surfaces its raw transport exception; the typed code
        // lands on the client (unchanged by 0062).
        throwsA(anything),
      );
      // The point is that resolving a relay out of thin air would have thrown
      // `relay_misconfigured` instead.
      expect(client.lastErrorCode, 'connect_failed');
    });

    test('a permanent failure never hops', () async {
      final relay = await _CountingEndpoint.start();
      addTearDown(relay.close);
      // `unauthorized` is permanent on the auth handshake: the host said no,
      // and it will say no just as firmly over the relay.
      final mesh = await _RespondingEndpoint.start(errorCode: 'unauthorized');
      addTearDown(mesh.close);
      final client = _newClient();

      await expectLater(
        client.connect(
          hostInput: mesh.host,
          token: 'token',
          mode: TlsMode.off,
          transport: TransportMode.mesh,
          enableAutoReconnect: false,
          relayUrl: relay.base,
          relayHostId: 'hid-1',
        ),
        throwsA(
          isA<McException>().having((e) => e.code, 'code', 'unauthorized'),
        ),
      );
      expect(mesh.upgrades, 1);
      expect(relay.connections, 0, reason: 'permanent verdicts must not hop');
    });

    test('a second failure ends the episode — no mesh↔relay loop', () async {
      final relay = await _CountingEndpoint.start();
      addTearDown(relay.close);
      final deadMeshPort = await _deadPort();
      final client = _newClient();

      await expectLater(
        client.connect(
          hostInput:
              'ws://${InternetAddress.loopbackIPv4.address}:'
              '$deadMeshPort',
          token: 'token',
          mode: TlsMode.off,
          transport: TransportMode.mesh,
          enableAutoReconnect: false,
          relayUrl: relay.base,
          relayHostId: 'hid-1',
        ),
        throwsA(isA<McException>()),
      );
      // Give any (incorrect) third leg time to appear.
      await Future<void>.delayed(const Duration(milliseconds: 300));
      expect(relay.connections, 1);
    });
  });

  group('amendment A1 — a spent pair code is never re-sent', () {
    test(
      'a retryable failure *after* the claim is on the wire does not hop',
      () async {
        final relay = await _CountingEndpoint.start();
        addTearDown(relay.close);
        // `host_offline` is squarely retryable — the only reason not to hop
        // here is that the code has already left the device.
        final mesh = await _RespondingEndpoint.start(errorCode: 'host_offline');
        addTearDown(mesh.close);
        final client = _newClient();

        await expectLater(
          client.claimPairCode(
            hostInput: mesh.host,
            code: 'K7M29X4P',
            mode: TlsMode.off,
            transport: TransportMode.mesh,
            relayUrl: relay.base,
            relayHostId: 'hid-1',
          ),
          throwsA(
            isA<McException>().having((e) => e.code, 'code', 'host_offline'),
          ),
        );

        expect(mesh.requests, ['pair.claim']);
        expect(
          relay.connections,
          0,
          reason:
              'the daemon may already have consumed the code; re-sending it '
              'over the relay would meet a permanent invalid_code and strand '
              'a user whose token exists host-side',
        );
      },
    );

    test('a failure *before* the claim is sent still hops', () async {
      // The other half of A1: the latch must not disable fallback wholesale,
      // only after the credential is committed.
      final relay = await _CountingEndpoint.start();
      addTearDown(relay.close);
      final deadMeshPort = await _deadPort();
      final client = _newClient();

      await expectLater(
        client.claimPairCode(
          hostInput:
              'ws://${InternetAddress.loopbackIPv4.address}:'
              '$deadMeshPort',
          code: 'K7M29X4P',
          mode: TlsMode.off,
          transport: TransportMode.mesh,
          relayUrl: relay.base,
          relayHostId: 'hid-1',
        ),
        throwsA(isA<McException>()),
      );
      expect(relay.connections, 1);
    });
  });

  group('amendment A2 — config errors hop rather than strand', () {
    test('relay_misconfigured falls back to a configured mesh', () async {
      // Sticky/forced relay with no usable tuple: the old classification would
      // have called this permanent and left a reachable mesh untried.
      final mesh = await _RespondingEndpoint.start(errorCode: 'unauthorized');
      addTearDown(mesh.close);
      final client = _newClient();

      await expectLater(
        client.connect(
          hostInput: mesh.host,
          token: 'token',
          mode: TlsMode.off,
          transport: TransportMode.relay,
          enableAutoReconnect: false,
        ),
        // It reaches the mesh and fails there on the scripted verdict, which is
        // proof the hop happened: without it the error would be
        // relay_misconfigured.
        throwsA(
          isA<McException>().having((e) => e.code, 'code', 'unauthorized'),
        ),
      );
      expect(mesh.upgrades, 1);
    });
  });

  group('D11 — fallback budget per network generation', () {
    test(
      'a background reconnect spends the budget once per generation',
      () async {
        final relay = await _CountingEndpoint.start();
        addTearDown(relay.close);
        final deadMeshPort = await _deadPort();
        final host =
            'ws://${InternetAddress.loopbackIPv4.address}:$deadMeshPort';
        final client = _newClient();
        // A background reconnect carries no attempt tuple, so the relay has to
        // be a *stored* route for it to count as a configured alternate.
        client.setRelayRoute(
          relayUrl: relay.base,
          hostId: 'hid-1',
          authority: '${InternetAddress.loopbackIPv4.address}:$deadMeshPort',
        );

        // First background episode: mesh fails, hops to relay. Budget spent.
        await expectLater(
          client.reconnect(
            hostInput: host,
            token: 'token',
            transport: TransportMode.mesh,
            userInitiated: false,
          ),
          throwsA(anything),
        );
        expect(relay.connections, 1);

        // Second background episode, same generation: no hop left.
        await expectLater(
          client.reconnect(
            hostInput: host,
            token: 'token',
            transport: TransportMode.mesh,
            userInitiated: false,
          ),
          throwsA(anything),
        );
        expect(
          relay.connections,
          1,
          reason: 'a storm of triggers on one network shares one fallback',
        );

        // A new network is a new budget.
        client.bumpNetworkGeneration();
        await expectLater(
          client.reconnect(
            hostInput: host,
            token: 'token',
            transport: TransportMode.mesh,
            userInitiated: false,
          ),
          throwsA(anything),
        );
        expect(relay.connections, 2);
      },
      timeout: const Timeout(Duration(seconds: 60)),
    );

    test(
      'A5 — a user-initiated episode is exempt from a spent budget',
      () async {
        final relay = await _CountingEndpoint.start();
        addTearDown(relay.close);
        final deadMeshPort = await _deadPort();
        final host =
            'ws://${InternetAddress.loopbackIPv4.address}:$deadMeshPort';
        final client = _newClient();
        // A background reconnect carries no attempt tuple, so the relay has to
        // be a *stored* route for it to count as a configured alternate.
        client.setRelayRoute(
          relayUrl: relay.base,
          hostId: 'hid-1',
          authority: '${InternetAddress.loopbackIPv4.address}:$deadMeshPort',
        );

        await expectLater(
          client.reconnect(
            hostInput: host,
            token: 'token',
            transport: TransportMode.mesh,
            userInitiated: false,
          ),
          throwsA(anything),
        );
        expect(relay.connections, 1);

        // Same generation, but the human tapped Try Relay / Reconnect now. Under
        // a strict budget this would be a dead button.
        await expectLater(
          client.reconnect(
            hostInput: host,
            token: 'token',
            transport: TransportMode.mesh,
            userInitiated: true,
          ),
          throwsA(anything),
        );
        expect(relay.connections, 2);
      },
      timeout: const Timeout(Duration(seconds: 60)),
    );
  });

  group('P3 — cold auto-connect and multi-trigger', () {
    test(
      'a cold auto-connect resolves in the background: sticky, no probes',
      () async {
        final probes = _FakeProbes(meshUp: true, relayUp: true);
        final relay = await _CountingEndpoint.start();
        addTearDown(relay.close);
        final deadMeshPort = await _deadPort();
        final client = _newClient(probes: probes);
        client.setRelayRoute(
          relayUrl: relay.base,
          hostId: 'hid-1',
          authority: '${InternetAddress.loopbackIPv4.address}:$deadMeshPort',
        );

        // This is what ConnectScreen._load does on a cold start: it has
        // credentials and no user waiting on a menu.
        await expectLater(
          client.connect(
            hostInput:
                'ws://${InternetAddress.loopbackIPv4.address}:$deadMeshPort',
            token: 'token',
            mode: TlsMode.off,
            enableAutoReconnect: false,
            userInitiated: false,
          ),
          throwsA(anything),
        );

        expect(
          probes.meshCalls + probes.relayCalls,
          0,
          reason: 'a cold start must not spend ~1s probing before its dial',
        );
        // It still failed over, so the background path is not a worse
        // connection — only a quieter one.
        expect(relay.connections, 1);
      },
    );

    test(
      'triggers in one generation share a fallback; a new generation does not',
      () async {
        // Stands in for an airplane-mode toggle producing several reconnect
        // triggers: without the shared budget each one buys its own hop, and
        // the phone thrashes between transports.
        final relay = await _CountingEndpoint.start();
        addTearDown(relay.close);
        final deadMeshPort = await _deadPort();
        final host =
            'ws://${InternetAddress.loopbackIPv4.address}:$deadMeshPort';
        final client = _newClient();
        client.setRelayRoute(
          relayUrl: relay.base,
          hostId: 'hid-1',
          authority: '${InternetAddress.loopbackIPv4.address}:$deadMeshPort',
        );

        client.bumpNetworkGeneration();
        for (var i = 0; i < 3; i++) {
          await expectLater(
            client.reconnect(
              hostInput: host,
              token: 'token',
              transport: TransportMode.mesh,
              userInitiated: false,
            ),
            throwsA(anything),
          );
        }
        expect(relay.connections, 1, reason: 'three triggers, one hop');

        client.bumpNetworkGeneration();
        await expectLater(
          client.reconnect(
            hostInput: host,
            token: 'token',
            transport: TransportMode.mesh,
            userInitiated: false,
          ),
          throwsA(anything),
        );
        expect(relay.connections, 2, reason: 'a new network earns a new hop');
      },
      timeout: const Timeout(Duration(seconds: 90)),
    );
  });

  group('mode resolution', () {
    test('a mesh-only pairing never opens a relay transport', () async {
      // Manual checklist row 10, promoted to an assertion.
      final relay = await _CountingEndpoint.start();
      addTearDown(relay.close);
      final deadMeshPort = await _deadPort();
      final client = _newClient();

      await expectLater(
        client.connect(
          hostInput:
              'ws://${InternetAddress.loopbackIPv4.address}:'
              '$deadMeshPort',
          token: 'token',
          mode: TlsMode.off,
          enableAutoReconnect: false,
          // No relay tuple at all, and none stored.
        ),
        throwsA(anything),
      );
      await Future<void>.delayed(const Duration(milliseconds: 200));
      expect(relay.connections, 0);
    });

    test('an interactive dial with both configured probes first', () async {
      final probes = _FakeProbes(meshUp: false, relayUp: true);
      final relay = await _CountingEndpoint.start();
      addTearDown(relay.close);
      final deadMeshPort = await _deadPort();
      final client = _newClient(probes: probes);

      await expectLater(
        client.connect(
          hostInput:
              'ws://${InternetAddress.loopbackIPv4.address}:'
              '$deadMeshPort',
          token: 'token',
          mode: TlsMode.off,
          enableAutoReconnect: false,
          relayUrl: relay.base,
          relayHostId: 'hid-1',
        ),
        throwsA(anything),
      );

      expect(probes.meshCalls, 1);
      expect(probes.relayCalls, 1);
      // Mesh probe said down, relay said up, so the relay went first — this is
      // the off-mesh QR case that must not pay an 8s mesh timeout.
      expect(relay.connections, greaterThanOrEqualTo(1));
    });

    test('a background dial never probes', () async {
      final probes = _FakeProbes(meshUp: true, relayUp: true);
      final relay = await _CountingEndpoint.start();
      addTearDown(relay.close);
      final deadMeshPort = await _deadPort();
      final client = _newClient(probes: probes);

      await expectLater(
        client.reconnect(
          hostInput:
              'ws://${InternetAddress.loopbackIPv4.address}:'
              '$deadMeshPort',
          token: 'token',
          userInitiated: false,
        ),
        throwsA(anything),
      );
      expect(probes.meshCalls, 0);
      expect(probes.relayCalls, 0);
    });
  });

  group('sticky transport', () {
    test(
      'is recorded on auth success and read back for the authority',
      () async {
        final store = SettingsStore(secure: _MemorySecureStorage());
        await store.setLastTransportSuccess(TransportMode.relay, 'h:7531');
        expect(
          await store.getLastTransportSuccess('h:7531'),
          TransportMode.relay,
        );
        // A preference for one daemon is not advice about another.
        expect(await store.getLastTransportSuccess('other:7531'), isNull);
      },
    );

    test('is dropped by clearAll', () async {
      final store = SettingsStore(secure: _MemorySecureStorage());
      await store.setLastTransportSuccess(TransportMode.mesh, 'h:7531');
      await store.setTransportSelection(TransportMode.relay, 'h:7531');
      await store.clearAll();
      expect(await store.getLastTransportSuccess('h:7531'), isNull);
      expect(await store.getTransportSelection('h:7531'), isNull);
    });

    test('changing host discards the other transport preference', () async {
      final store = SettingsStore(secure: _MemorySecureStorage());
      await store.setTransportSelection(TransportMode.relay, 'h:7531');
      await store.setLastTransportSuccess(TransportMode.mesh, 'other:7531');
      expect(await store.getTransportSelection('other:7531'), isNull);
      expect(
        await store.getLastTransportSuccess('other:7531'),
        TransportMode.mesh,
      );
    });
  });
}

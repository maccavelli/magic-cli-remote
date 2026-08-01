import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/transport_policy.dart';
import 'package:magic_cli_remote/data/ws/relay_transport.dart';
import 'package:magic_cli_remote/data/ws/transport_probes.dart';

TransportAvailability _avail({
  bool meshCfg = true,
  bool relayCfg = true,
  bool meshOp = false,
  bool relayOp = false,
}) => TransportAvailability(
  meshConfigured: meshCfg,
  relayConfigured: relayCfg,
  meshOperational: meshOp,
  relayOperational: relayOp,
);

void main() {
  group('TransportAvailability', () {
    test('available requires configured AND operational', () {
      expect(_avail(meshOp: true).meshAvailable, isTrue);
      // Probe passed but no credentials: not a usable transport.
      expect(_avail(relayCfg: false, relayOp: true).relayAvailable, isFalse);
      // Configured but the probe did not answer.
      expect(_avail(meshOp: false).meshAvailable, isFalse);
    });

    test('soleAvailable is null when neither or both are available', () {
      expect(_avail().soleAvailable, isNull);
      expect(_avail(meshOp: true, relayOp: true).soleAvailable, isNull);
      expect(_avail(meshOp: true).soleAvailable, TransportMode.mesh);
      expect(_avail(relayOp: true).soleAvailable, TransportMode.relay);
    });

    test('bothConfigured ignores probe results', () {
      // This is what gates "Try Mesh / Try Relay" after a dial failure: a
      // failed probe is not proof the dial will fail (D2).
      expect(_avail().bothConfigured, isTrue);
      expect(_avail().bothAvailable, isFalse);
    });
  });

  group('transportAvailabilityFromConfig', () {
    test('relay needs url AND host id', () {
      final a = transportAvailabilityFromConfig(
        host: 'host:7531',
        relayUrl: 'wss://r.example',
        relayHostId: '',
        relayAuthority: 'host:7531',
        hostAuthority: 'host:7531',
      );
      expect(a.relayConfigured, isFalse);
      expect(a.meshConfigured, isTrue);
    });

    test('relay authority must match the host being dialled', () {
      // MADR 0046 M-2: a relay hint is credentials for one daemon, never a
      // generic fallback for whatever host is typed next.
      final a = transportAvailabilityFromConfig(
        host: 'other:7531',
        relayUrl: 'wss://r.example',
        relayHostId: 'hid-1',
        relayAuthority: 'host:7531',
        hostAuthority: 'other:7531',
      );
      expect(a.relayConfigured, isFalse);
    });

    test('legacy route with no stored authority still counts', () {
      final a = transportAvailabilityFromConfig(
        host: 'host:7531',
        relayUrl: 'wss://r.example',
        relayHostId: 'hid-1',
        relayAuthority: '',
        hostAuthority: 'host:7531',
      );
      expect(a.relayConfigured, isTrue);
    });

    test('empty host means mesh is not configured', () {
      final a = transportAvailabilityFromConfig(
        host: '  ',
        relayUrl: 'wss://r.example',
        relayHostId: 'hid-1',
        relayAuthority: '',
        hostAuthority: '',
      );
      expect(a.meshConfigured, isFalse);
      expect(a.relayConfigured, isTrue);
    });
  });

  group('resolveInteractive — full D3 matrix', () {
    // | mesh | relay | selection | result |
    final rows = <(bool, bool, TransportMode?, TransportMode?)>[
      (true, false, null, TransportMode.mesh),
      (true, false, TransportMode.mesh, TransportMode.mesh),
      // Sole available wins over a stale selection for the *other* transport:
      // the menu that produced it is not on screen, so the user can neither
      // see nor correct it.
      (true, false, TransportMode.relay, TransportMode.mesh),
      (false, true, null, TransportMode.relay),
      (false, true, TransportMode.mesh, TransportMode.relay),
      (false, true, TransportMode.relay, TransportMode.relay),
      // Both available: default Mesh, honour an explicit pick.
      (true, true, null, TransportMode.mesh),
      (true, true, TransportMode.mesh, TransportMode.mesh),
      (true, true, TransportMode.relay, TransportMode.relay),
      // Neither: no_transport.
      (false, false, null, null),
      (false, false, TransportMode.mesh, null),
      (false, false, TransportMode.relay, null),
    ];

    for (final (meshOp, relayOp, selection, want) in rows) {
      test('mesh=$meshOp relay=$relayOp selection=$selection → $want', () {
        expect(
          resolveInteractive(
            availability: _avail(meshOp: meshOp, relayOp: relayOp),
            userSelection: selection,
          ),
          want,
        );
      });
    }
  });

  group('resolveBackground', () {
    test('sticky wins when still configured', () {
      expect(
        resolveBackground(availability: _avail(), sticky: TransportMode.relay),
        TransportMode.relay,
      );
    });

    test('sticky is ignored when that transport is no longer configured', () {
      // The route was cleared on an authority change; mesh must still be tried
      // rather than the dial failing on a relay that no longer exists.
      expect(
        resolveBackground(
          availability: _avail(relayCfg: false),
          sticky: TransportMode.relay,
        ),
        TransportMode.mesh,
      );
    });

    test('no sticky prefers mesh, falls to relay, else null', () {
      expect(resolveBackground(availability: _avail()), TransportMode.mesh);
      expect(
        resolveBackground(availability: _avail(meshCfg: false)),
        TransportMode.relay,
      );
      expect(
        resolveBackground(
          availability: _avail(meshCfg: false, relayCfg: false),
        ),
        isNull,
      );
    });

    test('probe results are irrelevant to the background choice', () {
      // Background dials never probe (D4); the dial is the probe.
      expect(
        resolveBackground(
          availability: _avail(meshOp: false, relayOp: true),
          sticky: null,
        ),
        TransportMode.mesh,
      );
    });
  });

  group('isTransportRetryable', () {
    test('unknown and absent codes get one hop', () {
      expect(isTransportRetryable(null), isTrue);
      expect(isTransportRetryable(''), isTrue);
      expect(isTransportRetryable('something_new_from_the_daemon'), isTrue);
    });

    for (final code in transportRetryableCodes) {
      test('$code hops', () => expect(isTransportRetryable(code), isTrue));
    }

    for (final code in transportPermanentCodes) {
      test(
        '$code does not hop',
        () => expect(isTransportRetryable(code), isFalse),
      );
    }

    test('config errors hop rather than strand (A2)', () {
      // relay_misconfigured arrives flagged permanent:true — that flag is about
      // auto-reconnect parking, not about transports. A relay route cleared on
      // an authority change must not hide a reachable mesh.
      expect(isTransportRetryable('relay_misconfigured'), isTrue);
      expect(isTransportRetryable('no_transport'), isTrue);
    });

    test('the pair permanent set also blocks hops on token connects', () {
      // Fails closed on purpose: a missed hop costs one surfaced error, a
      // wrong hop can cost a one-shot credential.
      expect(isTransportRetryable('expired'), isFalse);
      expect(isTransportRetryable('invalid_code'), isFalse);
    });

    test('rate_limited never hops but stays out of the reconnect-park set', () {
      // Hopping does not placate a rate-limiting host, and on a claim it
      // spends the code. Background reconnect must still retry it (0046 L-3),
      // which is why the two sets are independent.
      expect(isTransportRetryable('rate_limited'), isFalse);
      expect(transportPermanentCodes, contains('rate_limited'));
    });

    test('codes are trimmed before classification', () {
      expect(isTransportRetryable(' invalid_token '), isFalse);
    });
  });

  group('TransportModeX', () {
    test('storage round-trips and rejects junk', () {
      for (final m in TransportMode.values) {
        expect(TransportModeX.fromStorage(m.storageValue), m);
      }
      expect(TransportModeX.fromStorage(null), isNull);
      expect(TransportModeX.fromStorage('carrier-pigeon'), isNull);
    });

    test('other flips the mode', () {
      expect(TransportMode.mesh.other, TransportMode.relay);
      expect(TransportMode.relay.other, TransportMode.mesh);
    });
  });

  group('RelayTransport.healthzUrl', () {
    test('normalises schemes and preserves the port', () {
      expect(
        RelayTransport.healthzUrl('wss://relay.example.com'),
        'https://relay.example.com/healthz',
      );
      expect(
        RelayTransport.healthzUrl('https://relay.example.com:8443'),
        'https://relay.example.com:8443/healthz',
      );
      expect(
        RelayTransport.healthzUrl('relay.example.com'),
        'https://relay.example.com/healthz',
      );
      expect(
        RelayTransport.healthzUrl('ws://127.0.0.1:8443'),
        'http://127.0.0.1:8443/healthz',
      );
    });

    test('is not the join endpoint', () {
      // phoneWsUrl normalises to wss://…/v1/phone, which does not answer a GET.
      expect(
        RelayTransport.healthzUrl('relay.example.com'),
        isNot(contains('/v1/phone')),
      );
    });
  });

  group('probeRelayReachable', () {
    late HttpServer server;

    tearDown(() async => server.close(force: true));

    Future<String> serve(int status, String body) async {
      server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      server.listen((req) async {
        req.response.statusCode = status;
        req.response.write(body);
        await req.response.close();
      });
      return 'http://127.0.0.1:${server.port}';
    }

    test('accepts 200 with an ok body', () async {
      final base = await serve(200, '{"ok":true}');
      expect(await probeRelayReachable(base), isTrue);
    });

    test('rejects a non-200', () async {
      final base = await serve(503, '{"ok":false}');
      expect(await probeRelayReachable(base), isFalse);
    });

    test('rejects a 200 that is not mcrelay', () async {
      // Some other service on the port must not steal the path.
      final base = await serve(200, '<html>hello</html>');
      expect(await probeRelayReachable(base), isFalse);
    });

    test('an unreachable relay is false, not an exception', () async {
      expect(
        await probeRelayReachable(
          'http://127.0.0.1:1',
          timeout: const Duration(milliseconds: 200),
        ),
        isFalse,
      );
    });

    test('empty base is false', () async {
      expect(await probeRelayReachable('   '), isFalse);
    });
  });

  group('TransportProbes.probe', () {
    test('runs both and folds results into availability', () async {
      final probes = _StubProbes(meshResult: true, relayResult: false);
      final got = await probes.probe(
        configured: _avail(),
        host: 'host:7531',
        relayUrl: 'wss://r.example',
      );
      expect(got.meshOperational, isTrue);
      expect(got.relayOperational, isFalse);
      expect(got.soleAvailable, TransportMode.mesh);
    });

    test('skips the probe for an unconfigured transport', () async {
      final probes = _StubProbes(meshResult: true, relayResult: true);
      final got = await probes.probe(
        configured: _avail(relayCfg: false),
        host: 'host:7531',
        relayUrl: 'wss://r.example',
      );
      expect(probes.relayCalls, 0, reason: 'no credentials → nothing to probe');
      expect(got.relayOperational, isFalse);
    });

    test('probes run in parallel, not one after the other', () async {
      final probes = _StubProbes(
        meshResult: true,
        relayResult: true,
        delayMs: 120,
      );
      final sw = Stopwatch()..start();
      await probes.probe(
        configured: _avail(),
        host: 'host:7531',
        relayUrl: 'wss://r.example',
      );
      sw.stop();
      expect(sw.elapsedMilliseconds, lessThan(220));
    });
  });
}

class _StubProbes extends TransportProbes {
  _StubProbes({
    required this.meshResult,
    required this.relayResult,
    this.delayMs = 0,
  });

  final bool meshResult;
  final bool relayResult;
  final int delayMs;
  int meshCalls = 0;
  int relayCalls = 0;

  Future<void> _pause() async {
    if (delayMs > 0) {
      await Future<void>.delayed(Duration(milliseconds: delayMs));
    }
  }

  @override
  Future<bool> mesh(
    String hostInput, {
    Duration timeout = kTransportProbeTimeout,
  }) async {
    meshCalls++;
    await _pause();
    return meshResult;
  }

  @override
  Future<bool> relay(
    String relayBase, {
    Duration timeout = kTransportProbeTimeout,
  }) async {
    relayCalls++;
    await _pause();
    return relayResult;
  }
}

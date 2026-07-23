import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/connection_path.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
import 'package:magic_cli_remote/data/ws/relay_transport.dart';

void main() {
  group('ConnectionPath + relay', () {
    test('relay when not directly reachable', () {
      final p = PairPayload.tryParse(
        'mcremote://pair?host=wss%3A%2F%2F100.64.0.1%3A7531'
        '&code=K7M29X4P&relay=wss%3A%2F%2Frelay.example.com&hid=devbox-1',
      )!;
      final path = ConnectionPath.resolve(p, directReachable: false);
      expect(path.usesRelay, isTrue);
      expect(path.relayUrl, 'wss://relay.example.com');
      expect(path.hostId, 'devbox-1');
      expect(path.mcremoteHost, contains('100.64.0.1'));
    });

    test('direct when reachable even if relay present', () {
      final p = PairPayload.tryParse(
        'mcremote://pair?host=h%3A7531&token=t'
        '&relay=relay.example.com&hid=devbox-1',
      )!;
      final path = ConnectionPath.resolve(p, directReachable: true);
      expect(path.kind, ConnectionPathKind.direct);
      expect(path.usesRelay, isFalse);
    });
  });

  group('RelayTransport helpers', () {
    test('phoneWsUrl normalizes https and bare hosts', () {
      expect(
        RelayTransport.phoneWsUrl('https://r.example:8443/'),
        'wss://r.example:8443/v1/phone',
      );
      expect(
        RelayTransport.phoneWsUrl('http://127.0.0.1:8080'),
        'ws://127.0.0.1:8080/v1/phone',
      );
    });

    test('asJoinPlaneMap rejects non-json', () {
      expect(
        () => RelayTransport.asJoinPlaneMap(Object()),
        throwsA(isA<FormatException>()),
      );
    });

    test('asJoinPlaneMap accepts bytes', () {
      final m = RelayTransport.asJoinPlaneMap(
        '{"type":"error","payload":{"code":"host_offline"}}'.codeUnits,
      );
      expect(m['type'], 'error');
    });
  });

  group('probeDirectReachable', () {
    test('returns false for unroutable host quickly', () async {
      final ok = await McremoteClient.probeDirectReachable(
        'wss://203.0.113.1:9', // TEST-NET-3, closed port
        timeout: const Duration(milliseconds: 200),
      );
      expect(ok, isFalse);
    });
  });
}

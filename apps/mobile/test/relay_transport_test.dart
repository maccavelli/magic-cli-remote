import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/ws/relay_transport.dart';

void main() {
  group('RelayTransport.phoneWsUrl', () {
    test('adds path and normalizes schemes', () {
      expect(
        RelayTransport.phoneWsUrl('wss://relay.example.com'),
        'wss://relay.example.com/v1/phone',
      );
      expect(
        RelayTransport.phoneWsUrl('https://relay.example.com:8443'),
        'wss://relay.example.com:8443/v1/phone',
      );
      expect(
        RelayTransport.phoneWsUrl('relay.example.com'),
        'wss://relay.example.com/v1/phone',
      );
      expect(
        RelayTransport.phoneWsUrl('ws://127.0.0.1:8443'),
        'ws://127.0.0.1:8443/v1/phone',
      );
    });
  });

  group('RelayTransport.asJoinPlaneMap', () {
    test('parses text JSON', () {
      final m = RelayTransport.asJoinPlaneMap(
        '{"v":1,"type":"join_ok","payload":{"session_id":"s"}}',
      );
      expect(m['type'], 'join_ok');
    });
  });
}

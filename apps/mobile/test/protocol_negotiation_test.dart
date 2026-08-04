import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/frame_budget.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';

// MADR 0068 P0 (U2, dart side) — capability parsing and envelope stamping.
void main() {
  group('ServerCaps.tryParse', () {
    test('parses the full v2 block', () {
      final caps = ServerCaps.tryParse({
        'protocol': 2,
        'read_deadline_ms': 60000,
        'ping_interval_ms': 10000,
        'ws_ping_resets_deadline': false,
        'history_ring': 800,
        'max_frame_bytes': 1048576,
        'tls_resumed': true,
      });
      expect(caps, isNotNull);
      expect(caps!.protocol, 2);
      expect(caps.readDeadlineMs, 60000);
      expect(caps.pingIntervalMs, 10000);
      expect(caps.wsPingResetsDeadline, isFalse);
      expect(caps.historyRing, 800);
      expect(caps.maxFrameBytes, 1 << 20);
      expect(caps.tlsResumed, isTrue);
      // Resume ships absent until the daemon implements 0068 P4.
      expect(caps.resumeWindowMs, isNull);
    });

    test('parses a future resume block when present', () {
      final caps = ServerCaps.tryParse({
        'protocol': 2,
        'resume': {'window_ms': 120000},
      });
      expect(caps?.resumeWindowMs, 120000);
    });

    test('absent, malformed, or v1-shaped input is null — consumers fall '
        'back to shipped constants', () {
      expect(ServerCaps.tryParse(null), isNull);
      expect(ServerCaps.tryParse('nope'), isNull);
      expect(ServerCaps.tryParse(<String, dynamic>{}), isNull);
      expect(ServerCaps.tryParse({'protocol': 1}), isNull);
    });

    test('missing limit fields default to the v1 constants', () {
      final caps = ServerCaps.tryParse({'protocol': 2});
      expect(caps, isNotNull);
      expect(caps!.readDeadlineMs, 60000);
      expect(caps.pingIntervalMs, 10000);
      expect(caps.historyRing, 800);
      expect(caps.maxFrameBytes, 1 << 20);
      expect(caps.tlsResumed, isFalse);
    });
  });

  group('envelope version stamping', () {
    test('defaults to v1 — pre-negotiation frames are byte-identical', () {
      final m =
          jsonDecode(encodeRequestEnvelope(id: 'i', type: 'ping'))
              as Map<String, dynamic>;
      expect(m['v'], 1);
    });

    test('carries the negotiated version when asked', () {
      final m =
          jsonDecode(
                encodeRequestEnvelope(id: 'i', type: 'ping', v: kProtocolV2),
              )
              as Map<String, dynamic>;
      expect(m['v'], 2);
    });

    test('"v":1 and "v":2 serialize to the same byte count, so the frame '
        'budget needs no version threading', () {
      final v1 = encodedRequestBytes(id: 'i', type: 'session.prompt');
      final v2 = utf8
          .encode(encodeRequestEnvelope(id: 'i', type: 'session.prompt', v: 2))
          .length;
      expect(v1, v2);
    });
  });
}

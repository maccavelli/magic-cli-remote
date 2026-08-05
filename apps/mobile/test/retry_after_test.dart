import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';

// 0068 P6 — server retry hints floor the backoff, never shorten it, and a
// hostile hint cannot park reconnection.

void main() {
  group('reconnectDelay', () {
    test('ladder unchanged without a hint: 1, 2, 4, 8, 16, 30, 30', () {
      final got = [for (var a = 0; a < 7; a++) reconnectDelay(a).inSeconds];
      expect(got, [1, 2, 4, 8, 16, 30, 30]);
    });

    test('hint above the ladder becomes the delay', () {
      expect(
        reconnectDelay(0, retryAfterFloorMs: 9000),
        const Duration(milliseconds: 9000),
      );
      expect(
        reconnectDelay(3, retryAfterFloorMs: 12500),
        const Duration(milliseconds: 12500),
      );
    });

    test('hint below the ladder never shortens it', () {
      // A 5s hint on an attempt already waiting 16s: the ladder wins —
      // shortening would turn the hint into an accelerator.
      expect(reconnectDelay(4, retryAfterFloorMs: 5000).inSeconds, 16);
    });

    test('hint is clamped to 60s', () {
      expect(
        reconnectDelay(0, retryAfterFloorMs: 3600000).inSeconds,
        60,
        reason: 'a confused or hostile server must not park reconnection',
      );
    });

    test('zero/negative hints are ignored', () {
      expect(reconnectDelay(0, retryAfterFloorMs: 0).inSeconds, 1);
      expect(reconnectDelay(0, retryAfterFloorMs: -5).inSeconds, 1);
    });
  });

  group('parseRetryAfterMs', () {
    test('daemon capacity close reason', () {
      expect(
        parseRetryAfterMs('too many clients; retry_after_ms=45000'),
        45000,
      );
    });

    test('absent / malformed / null', () {
      expect(parseRetryAfterMs('too many clients'), isNull);
      expect(parseRetryAfterMs('retry_after_ms=abc'), isNull);
      expect(parseRetryAfterMs(null), isNull);
    });

    test('absurdly long digit runs are rejected, not overflowed', () {
      expect(parseRetryAfterMs('retry_after_ms=99999999999999999999'), isNull);
    });
  });
}

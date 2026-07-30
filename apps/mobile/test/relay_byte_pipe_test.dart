import 'dart:collection';

import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/ws/relay_transport.dart';

/// MADR 0056 Phase 0 / H-3: outer relay frames are an opaque TLS/TCP byte pipe.
/// Dropping oldest frames on overflow is stream corruption. After Phase 1 the
/// queue must fail closed (`false` / close transport) without discarding bytes.
void main() {
  group('relay outer buffer (H-3)', () {
    test('under-budget frames stay ordered', () {
      final buf = Queue<List<int>>();
      for (var i = 0; i < 8; i++) {
        expect(enqueueRelayOuterFrame(buf, [i]), isTrue);
      }
      expect(buf.map((f) => f.single).toList(), [0, 1, 2, 3, 4, 5, 6, 7]);
    });

    test(
      'overflow must not silently drop oldest bytes (fails until Phase 1)',
      () {
        final buf = Queue<List<int>>();
        final total = kRelayOuterBufferMax + 5;
        var rejected = false;
        for (var i = 0; i < total; i++) {
          if (!enqueueRelayOuterFrame(buf, [i])) {
            rejected = true;
            break;
          }
        }
        // Acceptable post-Phase-1 outcomes: reject without drop, or never drop.
        // Current policy drops 0..4 and keeps 5..68 — first frame is not 0.
        final retained = buf.map((f) => f.single).toList();
        final lostPrefix = retained.isNotEmpty && retained.first != 0;
        expect(
          rejected || !lostPrefix,
          isTrue,
          reason:
              'MADR 0056 H-3: overflow must fail closed or retain every byte; '
              'got dropped prefix (first=${retained.isEmpty ? "empty" : retained.first}) '
              'len=${retained.length} rejected=$rejected',
        );
      },
    );
  });
}

import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/data/protocol/models.dart';

/// Latest-turn token and cost accounting (MADR 0112 A4, PLAN P5).
///
/// Two rules carry the weight: the legacy `used`/`size` pair keeps working for
/// clients that never learned the new fields, and an absent cost stays
/// distinguishable from a known-free one.
void main() {
  Usage parse(Map<String, dynamic> json) => Usage.fromJson(json);

  group('backward compatibility', () {
    test('a legacy payload still yields used and size', () {
      final u = parse({'used': 1200, 'size': 8000});
      expect(u.used, 1200);
      expect(u.size, 8000);
      expect(u.fraction, closeTo(0.15, 1e-9));
      expect(u.hasDetail, isFalse, reason: 'nothing to expand');
    });

    test('the detail is additive — legacy fields are untouched by it', () {
      final u = parse({
        'used': 1200,
        'size': 8000,
        'input': 900,
        'output': 200,
        'reasoning': 100,
        'cache_read': 50,
        'cache_write': 25,
        'cost_usd': 0.0125,
      });
      expect(u.used, 1200);
      expect(u.size, 8000);
      expect(u.input, 900);
      expect(u.output, 200);
      expect(u.reasoning, 100);
      expect(u.cacheRead, 50);
      expect(u.cacheWrite, 25);
      expect(u.costUsd, 0.0125);
      expect(u.hasDetail, isTrue);
    });
  });

  group('cost', () {
    test('absent cost and free cost are different answers', () {
      final unknown = parse({'used': 10, 'size': 100});
      expect(unknown.costUsd, isNull);

      final free = parse({'used': 10, 'size': 100, 'cost_usd': 0});
      expect(free.costUsd, 0);
      expect(
        free.hasDetail,
        isTrue,
        reason: 'a known-free turn is worth showing, not hiding',
      );
    });

    test('a fractional cost survives as a double', () {
      final u = parse({'used': 1, 'size': 2, 'cost_usd': 0.000125});
      expect(u.costUsd, closeTo(0.000125, 1e-12));
    });
  });

  group('missing and malformed input', () {
    test('absent buckets default to zero rather than null', () {
      final u = parse({'used': 5, 'size': 10});
      expect(u.input, 0);
      expect(u.output, 0);
      expect(u.reasoning, 0);
      expect(u.cacheRead, 0);
      expect(u.cacheWrite, 0);
    });

    test('an empty payload is inert, not an error', () {
      final u = parse(const {});
      expect(u.used, 0);
      expect(u.size, 0);
      expect(u.fraction, 0);
      expect(u.hasDetail, isFalse);
    });

    test('a zero window yields a zero fraction, never a divide', () {
      final u = parse({'used': 500, 'size': 0});
      expect(u.fraction, 0);
    });

    test('fraction is clamped when a provider over-reports', () {
      final u = parse({'used': 9000, 'size': 8000});
      expect(u.fraction, 1.0);
    });
  });

  group('hasDetail', () {
    test('is true when any single bucket is reported', () {
      for (final key in [
        'input',
        'output',
        'reasoning',
        'cache_read',
        'cache_write',
      ]) {
        final u = parse({'used': 1, 'size': 2, key: 1});
        expect(u.hasDetail, isTrue, reason: key);
      }
    });

    test('is false when every bucket is zero and cost is absent', () {
      final u = parse({
        'used': 1,
        'size': 2,
        'input': 0,
        'output': 0,
        'reasoning': 0,
        'cache_read': 0,
        'cache_write': 0,
      });
      expect(u.hasDetail, isFalse);
    });
  });
}

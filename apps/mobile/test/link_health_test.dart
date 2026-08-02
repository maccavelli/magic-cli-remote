import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/ws/link_health.dart';

/// The freshness classifier (MADR 0063 D1).
///
/// Pure by construction: it takes an elapsed [Duration] rather than a clock,
/// so every band is a table test with no timers, no `Future.delayed`, and no
/// flake. The client owns the clock; this owns the meaning.
void main() {
  group('classifyLinkHealth', () {
    const rows = <(Duration, bool, LinkHealth, String)>[
      (Duration.zero, true, LinkHealth.fresh, 'just verified'),
      (Duration(seconds: 14), true, LinkHealth.fresh, 'inside the window'),
      (Duration(seconds: 15), true, LinkHealth.stale, 'boundary is stale'),
      (Duration(seconds: 29), true, LinkHealth.stale, 'still only stale'),
      (Duration(seconds: 30), true, LinkHealth.lost, 'boundary is lost'),
      (Duration(minutes: 5), true, LinkHealth.lost, 'long gone'),
      // A closed socket is dead however recently it spoke: there is nothing
      // left to verify, so the freshness window is irrelevant.
      (Duration.zero, false, LinkHealth.lost, 'socket down beats freshness'),
      (Duration(seconds: 14), false, LinkHealth.lost, 'same at any age'),
    ];

    for (final (age, up, want, why) in rows) {
      test('${age.inSeconds}s socketUp=$up → $want ($why)', () {
        expect(classifyLinkHealth(sinceVerified: age, socketUp: up), want);
      });
    }

    test('thresholds are parameters, not hard-coded', () {
      // The client passes the defaults; tests and any future per-transport
      // tuning pass their own.
      expect(
        classifyLinkHealth(
          sinceVerified: const Duration(seconds: 5),
          socketUp: true,
          freshFor: const Duration(seconds: 2),
          deadAfter: const Duration(seconds: 10),
        ),
        LinkHealth.stale,
      );
    });
  });

  group('timing constants', () {
    test('red lands well inside the daemon read deadline', () {
      // internal/ws/server.go:165 drops a silent client at 60s. Reaching a
      // definitive state first is what keeps the two ends from disagreeing.
      expect(kLinkDeadAfter, lessThan(const Duration(seconds: 60)));
      expect(kLinkFreshFor, lessThan(kLinkDeadAfter));
    });

    test('the app ping is frequent enough to hold the deadline open', () {
      // Amendment B1: this ping is the *only* thing resetting the daemon's
      // read deadline — protocol pings do not. Several beats must fit inside
      // it so a single dropped one cannot cost the session.
      expect(kAppPingPeriod * 3, lessThan(const Duration(seconds: 60)));
      expect(kAppPingTimeout, lessThan(kAppPingPeriod));
    });

    test('the protocol backstop closes inside the dead window', () {
      // Amendment B4: 20s rather than the MADR's original 10s. It must still
      // close a dead socket before the freshness clock would have to call it,
      // or the hard signal would arrive after the soft one and add nothing.
      expect(kProtocolPingInterval, lessThan(kLinkDeadAfter));
    });
  });
}

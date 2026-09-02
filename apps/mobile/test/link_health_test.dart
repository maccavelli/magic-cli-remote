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
      // The daemon drops a silent client after
      // `limits.ws_read_deadline_seconds` (internal/config/config.go): 120 s by
      // default, floor 15 s. Reaching a definitive state first is what keeps
      // the two ends from disagreeing.
      //
      // The 60 s bound below is deliberately conservative and is NOT a claim
      // about the default — an earlier comment here said the daemon "drops a
      // silent client at 60s", which was a stale reading of a line number that
      // had moved (MADR 0126 F4). It is kept as-is rather than relaxed to the
      // real 120 s: a stricter bound costs nothing and a looser one would stop
      // catching anything. A daemon configured near the 15 s floor is out of
      // reach of any static bound and is handled at runtime by
      // McremoteClient._checkPingCadenceAgainstCaps (0126 D5).
      expect(kLinkDeadAfter, lessThan(const Duration(seconds: 60)));
      expect(kLinkFreshFor, lessThan(kLinkDeadAfter));
    });

    test('the app ping is frequent enough to hold the deadline open', () {
      // Amendment B1: this ping is the *only* thing resetting the daemon's
      // read deadline — protocol pings do not. Several beats must fit inside
      // it so a single dropped one cannot cost the session.
      //
      // Same conservative 60 s bound as above, and same reason: it is not the
      // daemon default (120 s), it is a floor-safe margin (MADR 0126 F4).
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

  // MADR 0126 D5. kAppPingPeriod is bounded ABOVE by kLinkFreshFor, not by the
  // daemon's read deadline: _noteInboundFrame stamps lastVerifiedAt on any
  // inbound frame, so on an idle session the app ping is the only thing that
  // verifies the link. A period slower than the freshness window would leave a
  // healthy idle session rendering amber for ever.
  //
  // This exists because the obvious "improvement" — deriving the cadence from
  // caps.read_deadline_ms (120 s default) — would give ~30 s and silently
  // break the status indicator. Fail loudly here instead.
  test('app ping verifies inside the freshness window (0126 D5)', () {
    expect(kAppPingPeriod, lessThanOrEqualTo(kLinkFreshFor));
    expect(
      classifyLinkHealth(sinceVerified: kAppPingPeriod, socketUp: true),
      LinkHealth.fresh,
      reason: 'a link verified exactly one ping ago must still read green',
    );
  });
}

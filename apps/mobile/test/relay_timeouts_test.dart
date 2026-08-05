import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';

// MADR 0068 P5/T10 (U8) — relay-leg timeouts derive from the episode's
// remaining budget: the fixed 15s+8s+20s worst case exceeded the 35s
// budget and silently forfeited the mesh fallback (A1 Dart finding 7).
void main() {
  test('full budget: serial worst case fits with margin for the fallback', () {
    final t = relayLegTimeouts(const Duration(seconds: 35));
    expect(t.join.inSeconds, 15);
    expect(t.loopback.inSeconds, 5);
    expect(t.innerReady.inSeconds, 12);
    final total = t.join + t.loopback + t.innerReady;
    expect(total < const Duration(seconds: 35), isTrue);
  });

  test('shrunken budget: legs shrink but never to zero', () {
    final t = relayLegTimeouts(const Duration(seconds: 10));
    expect(t.join >= const Duration(seconds: 2), isTrue);
    expect(t.innerReady >= const Duration(seconds: 2), isTrue);
    final total = t.join + t.loopback + t.innerReady;
    // Floors may overshoot a nearly-dead budget slightly; the episode
    // deadline check still bounds the fallback decision.
    expect(total <= const Duration(seconds: 12), isTrue);
  });

  test('large budget: legs are capped, not stretched', () {
    final t = relayLegTimeouts(const Duration(minutes: 5));
    expect(t.join.inSeconds, 15);
    expect(t.innerReady.inSeconds, 12);
  });
}

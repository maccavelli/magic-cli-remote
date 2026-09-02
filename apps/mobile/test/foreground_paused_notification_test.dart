import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/notifications/foreground_service.dart';

// MADR 0129 P2 / D3. After a swipe from recents the process and the foreground
// service survive but the main isolate does not (measured, 0126 P7), so the
// notification sat on "Connected to host" with no socket at all — the one
// surface MADR 0056 H-5a says must always reflect real socket state.
//
// The service isolate cannot see the main isolate; all it has is the last
// heartbeat. This is that decision, isolated from the plugin so it is testable
// at all.
void main() {
  final t0 = DateTime.utc(2026, 9, 1, 12, 0, 0);

  group('shouldShowPaused (0129 D3)', () {
    test('no heartbeat ever seen reads as paused', () {
      // The START_STICKY case: the system recreated the service after the
      // process died, so the main isolate is gone and has never checked in.
      // Assuming it is present would leave a stale "Connected to host" up for
      // the whole grace window — precisely the bug.
      expect(McRemoteTaskHandler.shouldShowPaused(null, t0), isTrue);
    });

    test('a recent heartbeat is not paused', () {
      expect(
        McRemoteTaskHandler.shouldShowPaused(
          t0.subtract(const Duration(seconds: 5)),
          t0,
        ),
        isFalse,
      );
    });

    test('one missed tick is tolerated', () {
      // A busy frame must not flip the notification; flapping between
      // "connected" and "paused" would be worse than being a minute late.
      expect(
        McRemoteTaskHandler.shouldShowPaused(
          t0.subtract(kForegroundHeartbeatCheck * 2),
          t0,
        ),
        isFalse,
        reason: 'two intervals is inside the grace window',
      );
    });

    test('past the grace window reads as paused', () {
      expect(
        McRemoteTaskHandler.shouldShowPaused(
          t0.subtract(kUiIsolatePresumedGone + const Duration(seconds: 1)),
          t0,
        ),
        isTrue,
      );
    });

    test('the grace window leaves room for a missed tick', () {
      // The window has to exceed the check interval, or a single late tick
      // would declare the UI isolate dead. Pinned so neither constant can be
      // tuned in isolation.
      expect(
        kUiIsolatePresumedGone,
        greaterThan(kForegroundHeartbeatCheck * 2),
        reason: '0129 P2: at least two checks of slack before declaring death',
      );
    });
  });
}

import 'dart:ui';

import 'package:flutter/services.dart';
import 'package:flutter_foreground_task/flutter_foreground_task.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/notifications/foreground_service.dart';

// MADR 0129 D2 / P6. The handover acknowledgement: the UI isolate must not
// dial until the foreground service says its socket is closed, or the daemon
// closes one of the two with 4001 and the app fights itself.
//
// Plain `test`, deliberately not `testWidgets`. The acknowledgement arrives on
// an isolate ReceivePort, which delivers on the real event loop, while
// testWidgets fakes the clock — so inside a widget test the reply never lands
// within a pump and `claimOwnership` can only ever reach its timeout. Testing
// it there would have measured the fallback and called it the mechanism.
void main() {
  const channel = MethodChannel('flutter_foreground_task/methods');
  const portName = 'flutter_foreground_task/isolateComPort';

  // Before any binding-dependent lookup below: the messenger only exists once
  // the test binding does.
  TestWidgetsFlutterBinding.ensureInitialized();
  final messenger =
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;

  tearDown(() {
    messenger.setMockMethodCallHandler(channel, null);
    FlutterForegroundTask.dataCallbacks.clear();
    IsolateNameServer.removePortNameMapping(portName);
  });

  /// Stub the plugin as a service that is running and answers the claim the
  /// way `McRemoteTaskHandler._releaseOwnership` does — release, then say so.
  void stubRunningService({required bool acknowledge}) {
    messenger.setMockMethodCallHandler(channel, (call) async {
      if (call.method == 'sendData' && acknowledge) {
        IsolateNameServer.lookupPortByName(
          portName,
        )?.send(<String, Object>{ForegroundServiceMessages.released: true});
      }
      return switch (call.method) {
        'isRunningService' => true,
        _ => null,
      };
    });
  }

  test(
    'the acknowledgement completes the claim, well inside the timeout',
    () async {
      // The port must exist first. main() opens it via initCommunicationPort;
      // without that call sendDataToMain looks the port up, finds nothing, and
      // drops the message — which is exactly the defect this test guards.
      FlutterForegroundTask.initCommunicationPort();
      stubRunningService(acknowledge: true);

      final sw = Stopwatch()..start();
      final claimed = await ForegroundServiceController().claimOwnership(
        timeout: const Duration(seconds: 3),
      );
      sw.stop();

      expect(claimed, isTrue, reason: 'the release ack should have been heard');
      expect(
        sw.elapsed,
        lessThan(const Duration(seconds: 1)),
        reason: 'completing on the ack, not on the timeout',
      );
    },
  );

  test(
    'an unregistered port loses the acknowledgement, and the claim times out',
    () async {
      // The bug as it shipped: the app never called initCommunicationPort, so
      // the port did not exist and every message the service sent the UI isolate
      // was dropped in silence. Handover still *appeared* to work, because the
      // service releases promptly on the heartbeat and the app dialled once the
      // stopwatch ran out — on a timer rather than on the acknowledgement.
      IsolateNameServer.removePortNameMapping(portName);
      stubRunningService(acknowledge: true);

      final claimed = await ForegroundServiceController().claimOwnership(
        timeout: const Duration(milliseconds: 250),
      );

      expect(
        claimed,
        isFalse,
        reason: 'no port means no ack, so only the timeout can end the wait',
      );
    },
  );

  test('a service that never answers still lets the app dial', () async {
    // Bounded on purpose: a service that does not reply must not strand the
    // app offline. The caller is told which happened via the return value.
    FlutterForegroundTask.initCommunicationPort();
    stubRunningService(acknowledge: false);

    final claimed = await ForegroundServiceController().claimOwnership(
      timeout: const Duration(milliseconds: 250),
    );

    expect(claimed, isFalse);
  });

  test('no running service is nothing to claim', () async {
    messenger.setMockMethodCallHandler(channel, (call) async {
      return switch (call.method) {
        'isRunningService' => false,
        _ => null,
      };
    });

    expect(await ForegroundServiceController().claimOwnership(), isTrue);
  });
}

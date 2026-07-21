import 'package:flutter/foundation.dart';
import 'package:flutter_foreground_task/flutter_foreground_task.dart';

/// Entry point for the foreground-service isolate. The service exists only to
/// keep the app process — and therefore the main-isolate WebSocket to the
/// daemon — alive while backgrounded, so agent events can still fire
/// notifications. It does no periodic work.
@pragma('vm:entry-point')
void mcRemoteForegroundCallback() {
  FlutterForegroundTask.setTaskHandler(_KeepAliveTaskHandler());
}

class _KeepAliveTaskHandler extends TaskHandler {
  @override
  Future<void> onStart(DateTime timestamp, TaskStarter starter) async {}

  @override
  void onRepeatEvent(DateTime timestamp) {}

  @override
  Future<void> onDestroy(DateTime timestamp, bool isTimeout) async {}
}

/// Starts/stops the Android foreground service. No-op on non-Android targets.
class ForegroundServiceController {
  bool _inited = false;

  bool get _isAndroid => defaultTargetPlatform == TargetPlatform.android;

  void _ensureInit() {
    if (_inited) return;
    FlutterForegroundTask.init(
      androidNotificationOptions: AndroidNotificationOptions(
        channelId: 'host_connection',
        channelName: 'Host connection',
        channelDescription:
            'Keeps the connection to your host alive so you get approval and '
            'completion alerts.',
        onlyAlertOnce: true,
      ),
      iosNotificationOptions: const IOSNotificationOptions(
        showNotification: false,
      ),
      foregroundTaskOptions: ForegroundTaskOptions(
        // No periodic callback — the service only keeps the process alive.
        eventAction: ForegroundTaskEventAction.nothing(),
        allowWakeLock: true,
        allowWifiLock: true,
      ),
    );
    _inited = true;
  }

  Future<void> start() async {
    if (!_isAndroid) return;
    // Best-effort: a denied notification permission or unavailable plugin
    // (tests) must not crash the app.
    try {
      _ensureInit();
      if (await FlutterForegroundTask.isRunningService) return;
      await FlutterForegroundTask.startService(
        serviceId: 42,
        notificationTitle: 'Connected to host',
        notificationText: 'Listening for approvals and completions',
        callback: mcRemoteForegroundCallback,
      );
    } catch (e) {
      debugPrint('ForegroundService.start failed (non-fatal): $e');
    }
  }

  Future<void> stop() async {
    if (!_isAndroid) return;
    try {
      if (await FlutterForegroundTask.isRunningService) {
        await FlutterForegroundTask.stopService();
      }
    } catch (e) {
      debugPrint('ForegroundService.stop failed (non-fatal): $e');
    }
  }
}

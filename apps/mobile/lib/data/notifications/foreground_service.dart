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
  ForegroundServiceController({this.onFailure});

  /// Reports a start failure to the on-device diary (MADR 0084 D1/A3). A
  /// service that never started means alerts never arrive — the user-visible
  /// failure this class could previously only debugPrint about.
  final void Function(Object error, StackTrace? stack)? onFailure;

  bool _inited = false;
  Future<void> _chain = Future.value();

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
        // The service keeps the process eligible for alerts; it must not hold
        // radio or CPU locks while a parked connection waits for its slow
        // maintenance retry.
        allowWakeLock: false,
        allowWifiLock: false,
      ),
    );
    _inited = true;
  }

  Future<void> start() async {
    return _enqueue(_start);
  }

  /// True when the last start attempt succeeded (or service was already
  /// running). Used so the UI never shows "Listening" without a live service
  /// (MADR 0056 H-5a).
  bool lastStartOk = false;

  Future<void> _start() async {
    if (!_isAndroid) return;
    // Best-effort: a denied notification permission or unavailable plugin
    // (tests) must not crash the app. H-5a: do not claim connected on failure.
    try {
      _ensureInit();
      if (await FlutterForegroundTask.isRunningService) {
        lastStartOk = true;
        return;
      }
      await FlutterForegroundTask.startService(
        serviceId: 42,
        notificationTitle: 'Connected to host',
        notificationText: 'Listening for approvals and completions',
        // Monochrome MC status icon (drawable resolved via the manifest
        // meta-data); otherwise the bar shows a white blob of the launcher.
        notificationIcon: const NotificationIcon(
          metaDataName: 'com.maccavelli.magic_cli_remote.notification_icon',
        ),
        callback: mcRemoteForegroundCallback,
      );
      lastStartOk = true;
    } catch (e, st) {
      lastStartOk = false;
      debugPrint('ForegroundService.start failed (non-fatal): $e');
      onFailure?.call(e, st);
    }
  }

  Future<void> stop() async {
    return _enqueue(_stop);
  }

  Future<void> _stop() async {
    if (!_isAndroid) return;
    try {
      if (await FlutterForegroundTask.isRunningService) {
        await FlutterForegroundTask.stopService();
      }
    } catch (e) {
      debugPrint('ForegroundService.stop failed (non-fatal): $e');
    }
  }

  Future<void> update({required String title, required String text}) {
    return _enqueue(() async {
      if (!_isAndroid) return;
      try {
        _ensureInit();
        if (await FlutterForegroundTask.isRunningService) {
          await FlutterForegroundTask.updateService(
            notificationTitle: title,
            notificationText: text,
          );
        }
      } catch (e) {
        debugPrint('ForegroundService.update failed (non-fatal): $e');
      }
    });
  }

  Future<void> _enqueue(Future<void> Function() operation) {
    _chain = _chain.then((_) => operation()).catchError((Object e) {
      debugPrint('ForegroundService operation failed (non-fatal): $e');
    });
    return _chain;
  }
}

import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_foreground_task/flutter_foreground_task.dart';

/// How often the service isolate checks whether the UI isolate is still there
/// (MADR 0129 P2). Not a wakeup: `allowWakeLock` is false, so this only runs
/// while the device is already awake — which is exactly when a stale
/// notification would be read.
const Duration kForegroundHeartbeatCheck = Duration(seconds: 30);

/// How long without a heartbeat before the UI isolate is presumed gone.
///
/// Three checks' worth. One missed tick is a busy frame; three is a dead
/// isolate. Erring long matters more than erring short here: flapping the
/// notification between "connected" and "paused" would be worse than telling
/// the truth a minute late.
const Duration kUiIsolatePresumedGone = Duration(seconds: 95);

/// Messages the UI isolate sends the service isolate. They share no memory, so
/// this is the whole vocabulary between them.
class ForegroundServiceMessages {
  /// "I am alive and the socket state is X." Sent on a timer and on every
  /// connection transition.
  static const heartbeat = 'mc.heartbeat';

  /// Title/text the UI isolate wants shown while it is alive.
  static const title = 'mc.title';
  static const text = 'mc.text';
}

/// Entry point for the foreground-service isolate.
///
/// **This isolate is not the one holding the WebSocket** (MADR 0129). The
/// socket, the app ping, the reconnect ladder and `NotificationCoordinator` all
/// live in the *main* isolate, which is bound to the Activity's FlutterEngine
/// and dies with it — measured in 0126 P7: after a swipe from recents the
/// process and this service survive, and `DartWorker` does not.
///
/// Until 0129 phase A moves the connection here, this handler's job is to stop
/// the app claiming a connection nobody is maintaining (0129 D3).
@pragma('vm:entry-point')
void mcRemoteForegroundCallback() {
  FlutterForegroundTask.setTaskHandler(McRemoteTaskHandler());
}

/// Watches for the UI isolate disappearing and corrects the notification.
///
/// Visible for testing: the pure state logic is [shouldShowPaused], which is
/// what the tests drive. The plugin calls are not reachable from a unit test.
@visibleForTesting
class McRemoteTaskHandler extends TaskHandler {
  DateTime? _lastHeartbeat;
  bool _showingPaused = false;

  /// Whether the notification should read "paused" given the last heartbeat.
  ///
  /// Pure so it can be tested without a service. A `null` heartbeat means the
  /// UI isolate has never checked in since this handler started — which is the
  /// normal case when the service was recreated by START_STICKY after the
  /// process died, and is exactly when the notification is most likely to be
  /// stale.
  static bool shouldShowPaused(DateTime? lastHeartbeat, DateTime now) {
    if (lastHeartbeat == null) return true;
    return now.difference(lastHeartbeat) > kUiIsolatePresumedGone;
  }

  @override
  Future<void> onStart(DateTime timestamp, TaskStarter starter) async {
    debugPrint('mcremote/fgs: task isolate started (${starter.name})');
    // Deliberately no heartbeat seeded here. If the service was restarted by
    // the system the UI isolate is probably gone, and assuming it is present
    // would keep a stale "Connected to host" on screen for the whole grace
    // window.
  }

  @override
  void onReceiveData(Object data) {
    if (data is! Map) return;
    if (data[ForegroundServiceMessages.heartbeat] != true) return;
    _lastHeartbeat = DateTime.now();
    if (_showingPaused) {
      _showingPaused = false;
      final title = data[ForegroundServiceMessages.title];
      final text = data[ForegroundServiceMessages.text];
      if (title is String && text is String) {
        unawaited(_update(title, text));
      }
    }
  }

  @override
  void onRepeatEvent(DateTime timestamp) {
    if (_showingPaused) return;
    if (!shouldShowPaused(_lastHeartbeat, timestamp)) return;
    _showingPaused = true;
    // Wording is deliberate (0129 P2). "Alerts paused" is a state the user can
    // act on; "Disconnected" reads as a fault they have to diagnose, and the
    // connection is not faulty — nothing is maintaining it.
    unawaited(_update('Alerts paused', 'Tap to reconnect to your host'));
  }

  @override
  void onNotificationPressed() {
    // The one-tap route back. Bringing the app up restarts the main isolate,
    // which reconnects and resumes owning the notification.
    FlutterForegroundTask.launchApp();
  }

  @override
  Future<void> onDestroy(DateTime timestamp, bool isTimeout) async {
    debugPrint('mcremote/fgs: task isolate destroyed (timeout=$isTimeout)');
  }

  Future<void> _update(String title, String text) async {
    try {
      await FlutterForegroundTask.updateService(
        notificationTitle: title,
        notificationText: text,
      );
    } catch (e) {
      // Best-effort, same rule as every other plugin call here: a notification
      // that cannot be corrected must not take the service down with it.
      debugPrint('mcremote/fgs: updateService failed (non-fatal): $e');
    }
  }
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
      // `stopWithTask` is deliberately NOT passed here (MADR 0126 D1). The
      // service's task-removal behaviour is set by the *manifest*, and setting
      // it from Dart is not equivalent: the plugin persists it as a preference
      // that `ForegroundServiceUtils.isSetStopWithTaskFlag` prefers over the
      // manifest, AND a `true` value additionally makes `ForegroundService`
      // install a visibility tracker that stops the service every time the app
      // becomes invisible — which is worse than the bug 0126 F1 describes.
      // Leave it null; `android/app/src/main/AndroidManifest.xml` is the one
      // place that decides this.
      foregroundTaskOptions: ForegroundTaskOptions(
        // A periodic callback, where there used to be none (MADR 0129 P2).
        //
        // The old comment read "the service only keeps the process alive", and
        // that was the mistaken assumption: keeping the process alive does not
        // keep the *main isolate* alive, so after a swipe the service sat there
        // with a notification claiming a connection that had died with the
        // Activity (0126 P7). This tick is what notices.
        //
        // It costs nothing while the device sleeps: allowWakeLock is false, so
        // the callback only fires when the device is already awake — which is
        // also the only time a stale notification can be read.
        eventAction: ForegroundTaskEventAction.repeat(
          kForegroundHeartbeatCheck.inMilliseconds,
        ),
        // The service keeps the process eligible for alerts; it must not hold
        // radio or CPU locks while a parked connection waits for its slow
        // maintenance retry.
        allowWakeLock: false,
        allowWifiLock: false,
        // MADR 0126 P3 step 2. The in-app updater replaces the package and the
        // OS kills the service with it; without this the user must tap the
        // "Updated" notification before alerts resume. Honoured by
        // RebootReceiver, which the manifest already declares — this is what
        // turns that exported receiver from an inert no-op into a reviewed,
        // used component.
        //
        // autoRunOnBoot stays false, deliberately: starting a service at boot
        // is a larger claim on the user's device than restarting one they were
        // already using.
        autoRunOnMyPackageReplaced: true,
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
      _startHeartbeat();
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
      _heartbeatTimer?.cancel();
      _heartbeatTimer = null;
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
          _lastTitle = title;
          _lastText = text;
          await FlutterForegroundTask.updateService(
            notificationTitle: title,
            notificationText: text,
          );
          // A state change is also a heartbeat: it proves the UI isolate is
          // alive, and carries the text the service should restore if it had
          // shown "Alerts paused".
          heartbeat(title: title, text: text);
        }
      } catch (e) {
        debugPrint('ForegroundService.update failed (non-fatal): $e');
      }
    });
  }

  /// Tell the service isolate the UI isolate is alive, and what the
  /// notification should say while it is (MADR 0129 P2/D3).
  ///
  /// The two isolates share no memory, so this message is the only thing
  /// standing between the service and a stale "Connected to host". Sent on a
  /// timer *and* on every connection transition — the timer alone would leave
  /// up to one interval of staleness after a state change.
  void heartbeat({required String title, required String text}) {
    if (!_isAndroid) return;
    try {
      FlutterForegroundTask.sendDataToTask(<String, Object>{
        ForegroundServiceMessages.heartbeat: true,
        ForegroundServiceMessages.title: title,
        ForegroundServiceMessages.text: text,
      });
    } catch (e) {
      debugPrint('ForegroundService.heartbeat failed (non-fatal): $e');
    }
  }

  /// Drives [heartbeat] while the UI isolate lives. Cancelled by [stop].
  Timer? _heartbeatTimer;
  String _lastTitle = 'Connected to host';
  String _lastText = 'Listening for approvals and completions';

  void _startHeartbeat() {
    _heartbeatTimer?.cancel();
    // Comfortably inside the service isolate's grace window, so a single
    // missed tick never flips the notification.
    _heartbeatTimer = Timer.periodic(
      const Duration(seconds: 30),
      (_) => heartbeat(title: _lastTitle, text: _lastText),
    );
    heartbeat(title: _lastTitle, text: _lastText);
  }

  Future<void> _enqueue(Future<void> Function() operation) {
    _chain = _chain.then((_) => operation()).catchError((Object e) {
      debugPrint('ForegroundService operation failed (non-fatal): $e');
    });
    return _chain;
  }
}

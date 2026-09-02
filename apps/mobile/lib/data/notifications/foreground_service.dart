import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_foreground_task/flutter_foreground_task.dart';

import '../local/settings_store.dart';
import '../ws/mcremote_client.dart';

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

  /// Service → main: "my socket is closed; you may dial" (MADR 0129 D2).
  ///
  /// The main isolate waits for this before connecting, so the two never hold
  /// a socket at once. Without it the daemon would close one of them with 4001
  /// (0068 D3) and the app would be fighting itself — the parked-zombie state
  /// 0126 F2 exists to repair.
  static const released = 'mc.released';
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

  /// The connection, while this isolate owns it (D1). Null whenever the UI
  /// isolate is alive — exactly one of the two holds a client at any moment.
  McremoteClient? _client;
  StreamSubscription<McConnectionState>? _connSub;

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
      // Release first, acknowledge second, and only then let the title follow
      // the UI isolate again. Any other order lets both isolates believe they
      // own the connection (D2).
      unawaited(
        _releaseOwnership().then((_) {
          if (title is String && text is String) {
            unawaited(_update(title, text));
          }
        }),
      );
    } else {
      // Already the UI isolate's connection; nothing to hand back, but it is
      // still waiting on an acknowledgement before it dials.
      FlutterForegroundTask.sendDataToMain(<String, Object>{
        ForegroundServiceMessages.released: true,
      });
    }
  }

  @override
  void onRepeatEvent(DateTime timestamp) {
    if (_showingPaused) return;
    if (!shouldShowPaused(_lastHeartbeat, timestamp)) return;
    _showingPaused = true;
    // The UI isolate is gone, so its socket died with it (measured, 0126 P7:
    // DartWorker absent and zero connections daemon-side). Nothing to conflict
    // with, so this isolate can take the connection — MADR 0129 D1/D2.
    unawaited(_takeOwnership());
  }

  /// Own the connection while no UI isolate exists (D1).
  ///
  /// Safe to dial unconditionally here: this runs only once the heartbeat has
  /// been silent past [kUiIsolatePresumedGone], and an isolate that is not
  /// answering is not holding a socket either.
  Future<void> _takeOwnership() async {
    await _update('Alerts paused', 'Reconnecting to your host…');
    try {
      final client = McremoteClient();
      _client = client;
      _connSub = client.connectionStates.listen((state) {
        // The title tracks the socket the same way the UI isolate's
        // NotificationCoordinator does (MADR 0056 H-5a) — the invariant does
        // not lapse just because a different isolate owns the connection.
        switch (state) {
          case McConnectionState.connected:
            unawaited(
              _update(
                'Connected to host',
                'Listening for approvals — app closed',
              ),
            );
          case McConnectionState.reconnecting:
          case McConnectionState.connecting:
          case McConnectionState.authenticating:
            unawaited(_update('Reconnecting to host', 'Retrying connection'));
          case McConnectionState.disconnected:
          case McConnectionState.error:
            unawaited(
              _update('Alerts paused', 'Tap to reconnect to your host'),
            );
        }
      });
      await client.reconnectFromStore(SettingsStore());
      debugPrint('mcremote/fgs: took ownership, state=${client.state}');
    } catch (e) {
      // No saved credentials, or the host is unreachable. Fall back to the
      // honest resting state rather than leaving a "Reconnecting…" that never
      // resolves.
      debugPrint('mcremote/fgs: takeOwnership failed: $e');
      await _update('Alerts paused', 'Tap to reconnect to your host');
    }
  }

  /// Give the connection back and say so (D2).
  ///
  /// The acknowledgement is the whole point: the main isolate does not dial
  /// until it arrives, so there is never a moment with two sockets.
  Future<void> _releaseOwnership() async {
    final client = _client;
    _client = null;
    await _connSub?.cancel();
    _connSub = null;
    if (client != null) {
      try {
        // manual: false — this is a handover, not a sign-out; the pairing and
        // the stored credentials stay exactly as they are.
        await client.disconnect(manual: false);
        await client.dispose();
      } catch (e) {
        debugPrint('mcremote/fgs: release failed (non-fatal): $e');
      }
    }
    FlutterForegroundTask.sendDataToMain(<String, Object>{
      ForegroundServiceMessages.released: true,
    });
    debugPrint('mcremote/fgs: released ownership');
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
    await _releaseOwnership();
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

  /// Claim the connection back from the service isolate, and wait for it to
  /// confirm its socket is closed (MADR 0129 D2 / C1).
  ///
  /// Call this **before** dialling on app start. The service isolate may be
  /// holding a live connection: if both dial, the daemon closes one with 4001
  /// (0068 D3) and the app fights itself.
  ///
  /// Bounded, but a timeout does **not** mean "go ahead anyway" — it means the
  /// service did not answer, which on Android is almost always because no
  /// service is running, and then there is nothing to conflict with. The
  /// caller is told which happened.
  Future<bool> claimOwnership({
    Duration timeout = const Duration(seconds: 3),
  }) async {
    if (!_isAndroid) return true;
    try {
      if (!await FlutterForegroundTask.isRunningService) return true;
    } catch (_) {
      return true;
    }
    final done = Completer<bool>();
    void onData(Object data) {
      if (data is Map && data[ForegroundServiceMessages.released] == true) {
        if (!done.isCompleted) done.complete(true);
      }
    }

    FlutterForegroundTask.addTaskDataCallback(onData);
    try {
      heartbeat(title: _lastTitle, text: _lastText);
      return await done.future.timeout(
        timeout,
        onTimeout: () {
          debugPrint(
            'ForegroundService.claimOwnership: no release ack within '
            '${timeout.inSeconds}s — proceeding (service likely not holding one)',
          );
          return false;
        },
      );
    } finally {
      FlutterForegroundTask.removeTaskDataCallback(onData);
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

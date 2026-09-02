import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_foreground_task/flutter_foreground_task.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import 'agent_notifications.dart';

/// A notification the user tapped, decoded back into something actionable.
class NotifResponse {
  const NotifResponse({required this.action, required this.payload});
  final NotifAction action;
  final NotifPayload payload;
}

/// Thin wrapper over flutter_local_notifications: creates channels, shows the
/// two notification kinds with their action buttons, and exposes a stream of
/// decoded taps. All local — no cloud push.
class NotificationService {
  NotificationService([FlutterLocalNotificationsPlugin? plugin])
    : _plugin = plugin ?? FlutterLocalNotificationsPlugin(),
      inServiceIsolate = false;

  /// The instance that runs inside the foreground service (MADR 0129 P5).
  ///
  /// The only difference is where an action tap is allowed to land: this one
  /// must not start an Activity, because the whole premise is that no Activity
  /// exists. See [inServiceIsolate].
  NotificationService.forServiceIsolate([
    FlutterLocalNotificationsPlugin? plugin,
  ]) : _plugin = plugin ?? FlutterLocalNotificationsPlugin(),
       inServiceIsolate = true;

  final FlutterLocalNotificationsPlugin _plugin;

  /// Whether this instance lives in the foreground-service isolate, which
  /// decides `showsUserInterface` on the Android action buttons and so decides
  /// where a tap is delivered (MADR 0129 P5 deviation, 2026-09-02):
  ///
  /// * false — `PendingIntent.getActivity`: the tap launches the app and the
  ///   main isolate answers on its socket. Correct while the UI isolate lives.
  /// * true — `PendingIntent.getBroadcast`: the tap reaches
  ///   `ActionBroadcastReceiver`, which runs [notificationBackgroundHandler]
  ///   in a third isolate; that isolate forwards to the service isolate, which
  ///   answers on *its* socket. No Activity is started, which is the point.
  ///
  /// The flag is "am I the UI isolate?", not a preference. Getting it wrong in
  /// either direction sends the answer to an isolate holding no socket.
  final bool inServiceIsolate;

  /// Android channel ids, public so Settings can map per-kind toggles to the
  /// OS channel state (MADR 0101 C). One channel per alert kind; asks
  /// (permissions and questions) share one.
  static const kPermissionChannelId = 'approval_needed';
  static const kTurnChannelId = 'agent_done';
  static const kErrorChannelId = 'agent_error';

  static const _permissionChannelId = kPermissionChannelId;
  static const _turnChannelId = kTurnChannelId;
  static const _errorChannelId = kErrorChannelId;

  /// iOS category carrying the Allow / Deny actions (MADR 0067 D4). Both
  /// actions are `foreground`: a suspended iOS process has no WebSocket to
  /// answer on, so the action must launch the app and route through the
  /// main-isolate handler.
  ///
  /// That used to be the same reason the Android actions set
  /// `showsUserInterface: true`, and it no longer is — Android now has an
  /// isolate that survives the Activity and holds the socket (MADR 0129), so
  /// its actions route by [inServiceIsolate]. iOS has no equivalent (0067 D2:
  /// the process suspends), so `foreground` here is not a parallel choice
  /// waiting to be revisited — it is the only option the platform offers.
  static const _approvalCategoryId = 'approval_actions';

  /// Recreated by [init] after a [dispose] (MADR 0128 D3): a closed
  /// `StreamController` cannot be reopened, and adding to one throws.
  ///
  /// Guarding `add` on `isClosed` instead would turn a restart into silent
  /// non-delivery, which is the failure this app least wants — the whole point
  /// of the class is that an approval alert reaches the user.
  StreamController<NotifResponse> _responses =
      StreamController<NotifResponse>.broadcast();

  /// Decoded taps on notification bodies / action buttons.
  ///
  /// Read this per use rather than caching it across a [dispose]: the
  /// controller behind it is replaced on restart.
  Stream<NotifResponse> get responses => _responses.stream;

  bool _ready = false;
  Object? _lastInitError;

  /// Why notifications are unavailable, or null when they work.
  ///
  /// A failed [init] used to be terminal for the process: nothing retried it
  /// and every later `show` threw into a swallowed catch, so the user got no
  /// alerts and no indication why (MADR 0046 L-5).
  Object? get lastInitError => _ready ? null : _lastInitError;

  /// Initialise if needed, retrying after an earlier failure. Returns whether
  /// the plugin is usable.
  ///
  /// [what] names the caller so a refusal says which alert was dropped. A bare
  /// `return` at the call sites used to be the quietest failure in the app:
  /// init fails once, every later `show*` returns immediately, and the user
  /// gets no alert and no line saying why. Found in MADR 0129 P5's first
  /// on-device run, where an Activity-scoped permission request failed init
  /// inside the service isolate and nothing said so.
  Future<bool> _ensureReady(String what) async {
    if (_ready) return true;
    await init();
    if (!_ready) {
      debugPrint('$what skipped: notifications unavailable ($_lastInitError)');
    }
    return _ready;
  }

  Future<void> init() async {
    if (_ready) return;
    // A restart after dispose() needs a live controller; the old one is closed
    // and cannot be reopened (MADR 0128 D3).
    if (_responses.isClosed) {
      _responses = StreamController<NotifResponse>.broadcast();
    }
    // Notifications are best-effort: a missing plugin (tests, unsupported
    // platform) or a denied permission must never crash the app.
    try {
      final initSettings = InitializationSettings(
        android: const AndroidInitializationSettings('@drawable/ic_stat_mc'),
        // Permission booleans false: the request is made explicitly below,
        // mirroring where Android asks, instead of as an initialize side
        // effect.
        iOS: DarwinInitializationSettings(
          requestAlertPermission: false,
          requestBadgePermission: false,
          requestSoundPermission: false,
          notificationCategories: [
            DarwinNotificationCategory(
              _approvalCategoryId,
              actions: [
                DarwinNotificationAction.plain(
                  'allow',
                  'Allow',
                  options: {DarwinNotificationActionOption.foreground},
                ),
                DarwinNotificationAction.plain(
                  'deny',
                  'Deny',
                  options: {DarwinNotificationActionOption.foreground},
                ),
              ],
            ),
          ],
        ),
      );
      await _plugin.initialize(
        settings: initSettings,
        onDidReceiveNotificationResponse: _onResponse,
        onDidReceiveBackgroundNotificationResponse:
            notificationBackgroundHandler,
      );

      // Asking for a permission is an Activity-scoped operation, and the
      // service isolate has no Activity: the plugin resolves the request
      // against a null one and throws
      // `NullPointerException: Context.checkPermission on a null object`,
      // which fails the whole init and leaves `_ready` false — so every later
      // `show*` returns silently and the service posts nothing at all.
      // Measured on device, MADR 0129 P5.
      //
      // Skipping it loses nothing. A permission can only be requested where
      // there is a UI to request it in, the UI isolate already does that at
      // the right moment, and by the time this isolate runs the foreground
      // service's own notification is on screen — which is proof
      // POST_NOTIFICATIONS is granted.
      if (!inServiceIsolate) {
        final ios = _plugin
            .resolvePlatformSpecificImplementation<
              IOSFlutterLocalNotificationsPlugin
            >();
        if (ios != null) {
          await ios.requestPermissions(alert: true, badge: true, sound: true);
        }
      }

      final android = _plugin
          .resolvePlatformSpecificImplementation<
            AndroidFlutterLocalNotificationsPlugin
          >();
      if (android != null) {
        if (!inServiceIsolate) {
          await android.requestNotificationsPermission();
        }
        // Channel creation stays on both paths: it needs only the application
        // context, and it is idempotent, so the service isolate re-declaring
        // the channels it posts on costs nothing and removes an ordering
        // dependency on the UI isolate having run first.
        await android.createNotificationChannel(
          const AndroidNotificationChannel(
            _permissionChannelId,
            'Approval needed',
            description:
                'The agent is blocked waiting for you to allow or deny a tool.',
            importance: Importance.high,
          ),
        );
        await android.createNotificationChannel(
          const AndroidNotificationChannel(
            _turnChannelId,
            'Agent finished',
            description: 'A session finished its turn.',
            importance: Importance.defaultImportance,
          ),
        );
        await android.createNotificationChannel(
          const AndroidNotificationChannel(
            _errorChannelId,
            'Agent error',
            description: 'A session reported an error.',
            importance: Importance.high,
          ),
        );
      }
      _ready = true;
      _lastInitError = null;
    } catch (e) {
      _lastInitError = e;
      debugPrint('NotificationService.init failed (non-fatal): $e');
    }
  }

  void _onResponse(NotificationResponse r) {
    final payload = NotifPayload.decode(r.payload);
    if (payload == null) return;
    final action = switch (r.actionId) {
      'allow' => NotifAction.allow,
      'deny' => NotifAction.deny,
      _ => NotifAction.open,
    };
    _responses.add(NotifResponse(action: action, payload: payload));
  }

  /// Notify that a tool needs approval, with inline Allow / Deny buttons.
  Future<void> showPermission({
    required String sessionId,
    required String permissionId,
    required String toolName,
    String? detail,
    String? allowOptionId,
  }) async {
    if (!await _ensureReady('showPermission')) return;
    final payload = NotifPayload(
      kind: NotifKind.permission,
      sessionId: sessionId,
      permissionId: permissionId,
      allowOptionId: allowOptionId,
    );
    final details = AndroidNotificationDetails(
      _permissionChannelId,
      'Approval needed',
      channelDescription: 'Agent is waiting for approval.',
      icon: '@drawable/ic_stat_mc',
      importance: Importance.high,
      priority: Priority.high,
      category: AndroidNotificationCategory.call,
      // showsUserInterface picks which isolate answers, and the right answer
      // is "whichever one holds the socket" — so it tracks [inServiceIsolate]
      // rather than being a constant (MADR 0129 P5 deviation).
      //
      // It was `true` unconditionally until 2026-09-02, correctly: the
      // plugin's default dispatches action taps to a background isolate, and
      // before 0129 no isolate but the main one could reach the WebSocket, so
      // that route made the buttons dead. 0129 moved the socket, not the
      // route — the background isolate still cannot answer, but it can now
      // forward to one that can.
      //
      // cancelNotification stays false in both directions so the notification
      // survives until the response actually succeeds (the coordinator cancels
      // it then); a dropped respond otherwise leaves the agent blocked with no
      // affordance. It is also what makes the "service not running" fallback
      // honest: the ask stays on screen and its body tap still opens the app.
      actions: [
        AndroidNotificationAction(
          'allow',
          'Allow',
          showsUserInterface: !inServiceIsolate,
          cancelNotification: false,
        ),
        AndroidNotificationAction(
          'deny',
          'Deny',
          showsUserInterface: !inServiceIsolate,
          cancelNotification: false,
        ),
      ],
    );
    try {
      await _plugin.show(
        id: notificationIdFor(
          kind: NotifKind.permission,
          sessionId: sessionId,
          requestId: permissionId,
        ),
        title: 'Approval needed: $toolName',
        body: detail == null || detail.isEmpty ? 'Tap to review' : detail,
        // interruptionLevel stays at the `active` default: `timeSensitive`
        // needs its entitlement and is a deliberate fast-follow
        // (MADR 0067 D4).
        notificationDetails: NotificationDetails(
          android: details,
          iOS: const DarwinNotificationDetails(
            categoryIdentifier: _approvalCategoryId,
          ),
        ),
        payload: payload.encode(),
      );
    } catch (e) {
      debugPrint('showPermission failed (non-fatal): $e');
    }
  }

  /// Notify that a session finished its turn.
  Future<void> showTurnComplete({
    required String sessionId,
    required String sessionLabel,
  }) async {
    if (!await _ensureReady('showTurnComplete')) return;
    final payload = NotifPayload(
      kind: NotifKind.turnComplete,
      sessionId: sessionId,
    );
    const details = AndroidNotificationDetails(
      _turnChannelId,
      'Agent finished',
      channelDescription: 'A session finished its turn.',
      icon: '@drawable/ic_stat_mc',
      importance: Importance.defaultImportance,
      priority: Priority.defaultPriority,
    );
    try {
      await _plugin.show(
        id: notificationIdFor(
          kind: NotifKind.turnComplete,
          sessionId: sessionId,
        ),
        title: 'Agent finished',
        body: sessionLabel,
        notificationDetails: const NotificationDetails(
          android: details,
          iOS: DarwinNotificationDetails(),
        ),
        payload: payload.encode(),
      );
    } catch (e) {
      debugPrint('showTurnComplete failed (non-fatal): $e');
    }
  }

  /// Notify that a session reported an error (MADR 0052 B3).
  Future<void> showError({
    required String sessionId,
    required String sessionLabel,
    String? detail,
  }) async {
    if (!await _ensureReady('showError')) return;
    final payload = NotifPayload(kind: NotifKind.error, sessionId: sessionId);
    const details = AndroidNotificationDetails(
      _errorChannelId,
      'Agent error',
      channelDescription: 'A session reported an error.',
      icon: '@drawable/ic_stat_mc',
      importance: Importance.high,
      priority: Priority.high,
    );
    try {
      await _plugin.show(
        id: notificationIdFor(kind: NotifKind.error, sessionId: sessionId),
        title: 'Agent error',
        body: detail == null || detail.isEmpty
            ? sessionLabel
            : '$sessionLabel · $detail',
        notificationDetails: const NotificationDetails(
          android: details,
          iOS: DarwinNotificationDetails(),
        ),
        payload: payload.encode(),
      );
    } catch (e) {
      debugPrint('showError failed (non-fatal): $e');
    }
  }

  /// Clear a permission notification once it resolves elsewhere (e.g. answered
  /// in-app or cancelled by the daemon), so a stale Allow/Deny can't be tapped.
  Future<void> cancelPermission(String sessionId, String permissionId) async {
    try {
      await _plugin.cancel(
        id: notificationIdFor(
          kind: NotifKind.permission,
          sessionId: sessionId,
          requestId: permissionId,
        ),
      );
    } catch (e) {
      // Same best-effort rule as every other plugin call: callers fire this
      // unawaited, an uncaught rejection here would surface as a zone error.
      debugPrint('cancelPermission failed (non-fatal): $e');
    }
  }

  /// Replace an ask's actionable notification with a passive expiry notice on
  /// the same channel and id (MADR 0101 A). No actions: the ask is dead and a
  /// stale Allow/Deny must not exist (0046 M-4). The payload carries no
  /// request id, so any tap takes the plain open-session path.
  Future<void> showAskExpired({
    required NotifKind kind,
    required String sessionId,
    required String requestId,
    required String sessionLabel,
  }) async {
    if (!await _ensureReady('showAskExpired')) return;
    final payload = NotifPayload(kind: kind, sessionId: sessionId);
    // Same channel as the ask it replaces, but default priority: the moment
    // has passed, this should sit in the shade rather than peek.
    const details = AndroidNotificationDetails(
      _permissionChannelId,
      'Approval needed',
      channelDescription: 'Agent is waiting for approval.',
      icon: '@drawable/ic_stat_mc',
      importance: Importance.high,
      priority: Priority.defaultPriority,
    );
    try {
      await _plugin.show(
        id: notificationIdFor(
          kind: kind,
          sessionId: sessionId,
          requestId: requestId,
        ),
        title: kind == NotifKind.question
            ? 'Question timed out'
            : 'Permission timed out',
        body: '$sessionLabel · the agent stopped waiting',
        notificationDetails: const NotificationDetails(
          android: details,
          iOS: DarwinNotificationDetails(),
        ),
        payload: payload.encode(),
      );
    } catch (e) {
      debugPrint('showAskExpired failed (non-fatal): $e');
    }
  }

  Future<void> showQuestion({
    required String sessionId,
    required String questionId,
    required String sessionLabel,
    String? detail,
  }) async {
    if (!await _ensureReady('showQuestion')) return;
    final payload = NotifPayload(
      kind: NotifKind.question,
      sessionId: sessionId,
      questionId: questionId,
    );
    const details = AndroidNotificationDetails(
      _permissionChannelId,
      'Approval needed',
      channelDescription: 'Agent is waiting for a response.',
      icon: '@drawable/ic_stat_mc',
      importance: Importance.high,
      priority: Priority.high,
    );
    try {
      await _plugin.show(
        id: notificationIdFor(
          kind: NotifKind.question,
          sessionId: sessionId,
          requestId: questionId,
        ),
        title: 'Question: $sessionLabel',
        body: detail == null || detail.isEmpty ? 'Tap to answer' : detail,
        // No category: questions carry no inline actions on Android either —
        // answering needs the in-app option list, so the tap opens the app.
        notificationDetails: const NotificationDetails(
          android: details,
          iOS: DarwinNotificationDetails(),
        ),
        payload: payload.encode(),
      );
    } catch (e) {
      debugPrint('showQuestion failed (non-fatal): $e');
    }
  }

  Future<void> cancelQuestion(String sessionId, String questionId) async {
    try {
      await _plugin.cancel(
        id: notificationIdFor(
          kind: NotifKind.question,
          sessionId: sessionId,
          requestId: questionId,
        ),
      );
    } catch (e) {
      debugPrint('cancelQuestion failed (non-fatal): $e');
    }
  }

  /// Channel ids the OS currently refuses to display (importance == none),
  /// i.e. the user blocked that category. Null when the platform has no
  /// channels (iOS, tests, desktop) or the probe fails.
  ///
  /// Deliberately not reporting lowered-but-nonzero importance: a silenced
  /// channel still displays, and conflating "quiet" with "blocked" would make
  /// the Settings warning cry wolf (MADR 0101 C).
  Future<Set<String>?> blockedChannelIds() async {
    try {
      final android = _plugin
          .resolvePlatformSpecificImplementation<
            AndroidFlutterLocalNotificationsPlugin
          >();
      if (android == null) return null;
      final channels = await android.getNotificationChannels();
      if (channels == null) return null;
      return {
        for (final c in channels)
          if (c.importance == Importance.none) c.id,
      };
    } catch (_) {
      return null;
    }
  }

  /// Whether the OS currently allows this app to post notifications. Null
  /// when the platform can't answer (tests, desktop).
  Future<bool?> areNotificationsEnabled() async {
    try {
      final android = _plugin
          .resolvePlatformSpecificImplementation<
            AndroidFlutterLocalNotificationsPlugin
          >();
      if (android != null) return await android.areNotificationsEnabled();
      final ios = _plugin
          .resolvePlatformSpecificImplementation<
            IOSFlutterLocalNotificationsPlugin
          >();
      // isEnabled covers full and provisional grants alike.
      return (await ios?.checkPermissions())?.isEnabled;
    } catch (_) {
      return null;
    }
  }

  /// Re-request the OS notification permission (Android 13+ / iOS). Returns
  /// whether it is granted afterwards; null when the platform can't answer.
  Future<bool?> requestPermission() async {
    try {
      final android = _plugin
          .resolvePlatformSpecificImplementation<
            AndroidFlutterLocalNotificationsPlugin
          >();
      if (android != null) {
        return await android.requestNotificationsPermission();
      }
      final ios = _plugin
          .resolvePlatformSpecificImplementation<
            IOSFlutterLocalNotificationsPlugin
          >();
      return await ios?.requestPermissions(
        alert: true,
        badge: true,
        sound: true,
      );
    } catch (_) {
      return null;
    }
  }

  /// The notification whose tap cold-launched this process, if any. Read once
  /// at coordinator start so the tap still routes to its session even when
  /// the app was not running to receive the response callback.
  Future<NotifResponse?> takeLaunchResponse() async {
    try {
      final details = await _plugin.getNotificationAppLaunchDetails();
      if (details == null || !details.didNotificationLaunchApp) return null;
      final r = details.notificationResponse;
      if (r == null) return null;
      final payload = NotifPayload.decode(r.payload);
      if (payload == null) return null;
      final action = switch (r.actionId) {
        'allow' => NotifAction.allow,
        'deny' => NotifAction.deny,
        _ => NotifAction.open,
      };
      return NotifResponse(action: action, payload: payload);
    } catch (e) {
      debugPrint('takeLaunchResponse failed (non-fatal): $e');
      return null;
    }
  }

  /// Release the plugin wiring and the response stream.
  ///
  /// Leaves the object **restartable** (MADR 0128 D3): `_ready` is cleared so a
  /// later [init] actually re-initialises instead of returning at its own
  /// `if (_ready) return`, and [init] recreates the closed controller. Without
  /// the reset, a `start()` after a `dispose()` skipped initialisation and then
  /// threw on the first `show*` — while its counterpart,
  /// `NotificationCoordinator.dispose()`, nulls its subscriptions precisely so
  /// a later `start()` re-subscribes. The two halves of that pair disagreed.
  void dispose() {
    _ready = false;
    _responses.close();
  }
}

/// Forwards an Allow/Deny tap to the isolate that holds the socket
/// (MADR 0129 P5 deviation, 2026-09-02).
///
/// **This runs in a third isolate**, in a `FlutterEngine` that
/// `ActionBroadcastReceiver` constructs on the tap — not the UI isolate and not
/// the foreground-service isolate. It has no socket and never will, so it does
/// not answer; it hands the decision to the service isolate, which does.
///
/// It fires only for actions shown with `showsUserInterface: false`, which
/// [NotificationService] sets only when the service isolate is the one showing
/// the ask. While the UI isolate lives, action taps launch the app instead and
/// never reach here.
///
/// Until 2026-09-02 this was an empty seam whose comment described it as "where
/// a future fully-headless Allow/Deny path would live". It was also
/// unreachable: `ActionBroadcastReceiver` was never declared in the app's
/// manifest, so the `PendingIntent.getBroadcast` it targets was delivered to
/// nothing. Both halves are fixed together — the receiver is declared in
/// `AndroidManifest.xml` and this body is real.
@pragma('vm:entry-point')
void notificationBackgroundHandler(NotificationResponse response) {
  final action = switch (response.actionId) {
    'allow' => NotifAction.allow,
    'deny' => NotifAction.deny,
    // A body tap is an Activity PendingIntent and never lands here, so
    // anything else is an action id this build does not know about. Dropping
    // it is right: forwarding an unknown action would make the service isolate
    // guess at an answer on the user's behalf.
    _ => null,
  };
  final payload = response.payload;
  if (action == null || payload == null || payload.isEmpty) {
    debugPrint(
      'mcremote/notif-bg: ignoring action=${response.actionId} '
      '(payload ${payload == null || payload.isEmpty ? "missing" : "present"})',
    );
    return;
  }
  // Fire-and-forget by construction: sendDataToTask returns void, and this
  // isolate's engine may be torn down as soon as the broadcast completes. The
  // acknowledgement the user sees is the notification disappearing, which the
  // service isolate does once the respond actually succeeds.
  //
  // If the service is not running the plugin drops this silently
  // (ForegroundService.sendData is guarded by isRunningServiceState). That is
  // the honest outcome: with no service there is no socket anywhere, and the
  // notification stays on screen — its actions set cancelNotification: false —
  // so the user can still tap the body to open the app and answer there.
  try {
    FlutterForegroundTask.sendDataToTask(
      NotifActionForward(action: action, payload: payload).encode(),
    );
    debugPrint(
      'mcremote/notif-bg: forwarded ${action.name} to service isolate',
    );
  } catch (e) {
    debugPrint('mcremote/notif-bg: forward failed: $e');
  }
}

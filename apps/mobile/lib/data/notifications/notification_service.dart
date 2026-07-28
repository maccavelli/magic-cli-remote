import 'dart:async';

import 'package:flutter/foundation.dart';
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
    : _plugin = plugin ?? FlutterLocalNotificationsPlugin();

  final FlutterLocalNotificationsPlugin _plugin;

  static const _permissionChannelId = 'approval_needed';
  static const _turnChannelId = 'agent_done';

  final _responses = StreamController<NotifResponse>.broadcast();

  /// Decoded taps on notification bodies / action buttons.
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
  Future<bool> _ensureReady() async {
    if (_ready) return true;
    await init();
    return _ready;
  }

  Future<void> init() async {
    if (_ready) return;
    // Notifications are best-effort: a missing plugin (tests, unsupported
    // platform) or a denied permission must never crash the app.
    try {
      const initSettings = InitializationSettings(
        android: AndroidInitializationSettings('@drawable/ic_stat_mc'),
      );
      await _plugin.initialize(
        settings: initSettings,
        onDidReceiveNotificationResponse: _onResponse,
        onDidReceiveBackgroundNotificationResponse:
            notificationBackgroundHandler,
      );

      final android = _plugin
          .resolvePlatformSpecificImplementation<
            AndroidFlutterLocalNotificationsPlugin
          >();
      if (android != null) {
        await android.requestNotificationsPermission();
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
    if (!await _ensureReady()) return;
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
      // showsUserInterface routes the tap through the MAIN-isolate handler —
      // the plugin's default dispatches action taps to a background isolate
      // that cannot reach the WebSocket, which made these buttons dead.
      // cancelNotification stays false so the notification survives until the
      // response actually succeeds (the coordinator cancels it then); a
      // dropped respond otherwise leaves the agent blocked with no affordance.
      actions: const [
        AndroidNotificationAction(
          'allow',
          'Allow',
          showsUserInterface: true,
          cancelNotification: false,
        ),
        AndroidNotificationAction(
          'deny',
          'Deny',
          showsUserInterface: true,
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
        notificationDetails: NotificationDetails(android: details),
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
    if (!await _ensureReady()) return;
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
        notificationDetails: const NotificationDetails(android: details),
        payload: payload.encode(),
      );
    } catch (e) {
      debugPrint('showTurnComplete failed (non-fatal): $e');
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

  Future<void> showQuestion({
    required String sessionId,
    required String questionId,
    required String sessionLabel,
    String? detail,
  }) async {
    if (!await _ensureReady()) return;
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
        notificationDetails: const NotificationDetails(android: details),
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

  /// Whether the OS currently allows this app to post notifications. Null
  /// when the platform can't answer (tests, non-Android).
  Future<bool?> areNotificationsEnabled() async {
    try {
      final android = _plugin
          .resolvePlatformSpecificImplementation<
            AndroidFlutterLocalNotificationsPlugin
          >();
      return await android?.areNotificationsEnabled();
    } catch (_) {
      return null;
    }
  }

  /// Re-request the OS notification permission (Android 13+). Returns whether
  /// it is granted afterwards; null when the platform can't answer.
  Future<bool?> requestPermission() async {
    try {
      final android = _plugin
          .resolvePlatformSpecificImplementation<
            AndroidFlutterLocalNotificationsPlugin
          >();
      return await android?.requestNotificationsPermission();
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

  void dispose() {
    _responses.close();
  }
}

/// Background isolate handler for taps that arrive while the app's main isolate
/// is not running. With the foreground service alive the main-isolate handler
/// normally fires instead; this entry point keeps the plugin happy and is where
/// a future fully-headless Allow/Deny path would live.
@pragma('vm:entry-point')
void notificationBackgroundHandler(NotificationResponse response) {
  // Intentionally minimal: acting on the WebSocket requires the app process,
  // which the foreground service keeps alive. Left as an explicit seam.
  debugPrint('background notification action: ${response.actionId}');
}

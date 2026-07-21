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

      final android = _plugin.resolvePlatformSpecificImplementation<
          AndroidFlutterLocalNotificationsPlugin>();
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
    } catch (e) {
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
      actions: const [
        AndroidNotificationAction('allow', 'Allow'),
        AndroidNotificationAction('deny', 'Deny'),
      ],
    );
    try {
      await _plugin.show(
        id: notificationIdFor(sessionId: sessionId, permissionId: permissionId),
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
    final payload = NotifPayload(kind: NotifKind.turnComplete, sessionId: sessionId);
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
        id: notificationIdFor(sessionId: sessionId),
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
  Future<void> cancelPermission(String sessionId, String permissionId) =>
      _plugin.cancel(
        id: notificationIdFor(sessionId: sessionId, permissionId: permissionId),
      );

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

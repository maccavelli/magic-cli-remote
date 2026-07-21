import 'dart:async';

import 'package:flutter/foundation.dart';

import '../protocol/models.dart';
import '../ws/mcremote_client.dart';
import 'agent_notifications.dart';
import 'foreground_service.dart';
import 'notification_service.dart';

/// Wires agent events to local notifications and routes notification taps back
/// to the daemon, and runs the foreground service that keeps the socket alive
/// in the background. All local/mesh — no cloud push.
class NotificationCoordinator {
  NotificationCoordinator({
    required McremoteClient client,
    NotificationService? notifications,
    ForegroundServiceController? service,
  })  : _notifs = notifications ?? NotificationService(),
        _service = service ?? ForegroundServiceController(),
        // ignore: prefer_initializing_formals
        _client = client;

  final McremoteClient _client;
  final NotificationService _notifs;
  final ForegroundServiceController _service;

  StreamSubscription<SessionEvent>? _events;
  StreamSubscription<NotifResponse>? _responses;
  StreamSubscription<McConnectionState>? _conn;

  /// Master switch from Settings. When false, no notifications are shown and
  /// the keep-alive service is stopped.
  bool enabled = true;

  /// True while the app is on screen; set from the app lifecycle observer.
  bool appForegrounded = true;

  /// The session currently open in the chat screen (null if none).
  String? currentSessionId;

  /// Invoked when the user taps a notification to open a session.
  void Function(String sessionId)? onOpenSession;

  Future<void> start() async {
    await _notifs.init();
    _events ??= _client.events.listen(_onEvent);
    _responses ??= _notifs.responses.listen(_onResponse);
    _conn ??= _client.connectionStates.listen(_onConn);
  }

  Future<void> dispose() async {
    await _events?.cancel();
    await _responses?.cancel();
    await _conn?.cancel();
    await _service.stop();
    _notifs.dispose();
  }

  // The user is "watching" — no notification needed — only when the app is
  // foregrounded on that exact session.
  bool _watching(String sessionId) =>
      appForegrounded && currentSessionId == sessionId;

  void _onEvent(SessionEvent ev) {
    // Keep permission notifications honest: drop one as soon as it resolves.
    if (ev.type == 'permission_resolved' && (ev.permissionId ?? '').isNotEmpty) {
      unawaited(_notifs.cancelPermission(ev.sessionId, ev.permissionId!));
      return;
    }
    if (!enabled) return;
    if (!shouldNotify(eventType: ev.type, watching: _watching(ev.sessionId))) {
      return;
    }
    switch (ev.type) {
      case 'permission_request':
        final pid = ev.permissionId;
        if (pid == null || pid.isEmpty) return;
        unawaited(_notifs.showPermission(
          sessionId: ev.sessionId,
          permissionId: pid,
          toolName: ev.toolName ?? 'tool',
          detail: ev.text,
          allowOptionId: _allowOptionId(ev.options),
        ));
      case 'turn_complete':
        unawaited(_notifs.showTurnComplete(
          sessionId: ev.sessionId,
          sessionLabel: _shortId(ev.sessionId),
        ));
    }
  }

  Future<void> _onResponse(NotifResponse r) async {
    final p = r.payload;
    switch (r.action) {
      case NotifAction.open:
        onOpenSession?.call(p.sessionId);
      case NotifAction.allow:
      case NotifAction.deny:
        final pid = p.permissionId;
        if (pid == null) {
          onOpenSession?.call(p.sessionId);
          return;
        }
        try {
          if (r.action == NotifAction.deny) {
            await _client.respondPermission(
              sessionId: p.sessionId,
              permissionId: pid,
              cancelled: true,
            );
          } else if (p.allowOptionId != null) {
            await _client.respondPermission(
              sessionId: p.sessionId,
              permissionId: pid,
              optionId: p.allowOptionId,
            );
          } else {
            // No allow option was carried; fall back to opening the sheet.
            onOpenSession?.call(p.sessionId);
          }
        } catch (e) {
          debugPrint('notification respond failed: $e');
          onOpenSession?.call(p.sessionId);
        }
    }
  }

  void _onConn(McConnectionState state) {
    // Run the keep-alive service only while notifications are on and there is a
    // live connection to preserve; stop it otherwise to save battery.
    if (enabled &&
        (state == McConnectionState.connected ||
            state == McConnectionState.reconnecting)) {
      unawaited(_service.start());
    } else if (state == McConnectionState.disconnected ||
        state == McConnectionState.error) {
      unawaited(_service.stop());
    }
  }

  /// Apply the master switch (from Settings): stop the service immediately when
  /// turned off; the next connection event restarts it when turned back on.
  Future<void> setEnabled(bool value) async {
    enabled = value;
    if (!value) await _service.stop();
  }

  static String _shortId(String id) =>
      id.length > 8 ? 'Session ${id.substring(0, 8)}' : id;

  /// Pick the option that means "allow", mirroring the in-app permission sheet.
  static String? _allowOptionId(List<PermissionOption> options) {
    for (final o in options) {
      final isAllow = (o.kind?.contains('allow') ?? false) ||
          o.optionId.contains('allow');
      if (isAllow) return o.optionId;
    }
    return options.isNotEmpty ? options.first.optionId : null;
  }
}

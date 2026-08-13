import 'dart:async';

import 'package:flutter/foundation.dart';

import '../protocol/models.dart';
import '../ws/lifecycle_policy.dart';
import '../diagnostics/error_recorder.dart';
import '../ws/mcremote_client.dart';
import 'agent_notifications.dart';
import 'foreground_service.dart';
import 'notification_service.dart';

typedef _AskKey = (NotifKind kind, String sessionId, String requestId);

/// Wires agent events to local notifications and routes notification taps back
/// to the daemon, and runs the foreground service that keeps the socket alive
/// in the background. All local/mesh — no cloud push.
class NotificationCoordinator {
  NotificationCoordinator({
    required McremoteClient client,
    NotificationService? notifications,
    ForegroundServiceController? service,
    ErrorRecorder? recorder,
  }) : _notifs = notifications ?? NotificationService(),
       _service =
           service ??
           ForegroundServiceController(
             // MADR 0084 A3: a keep-alive service that failed to start means
             // no alerts arrive — worth a recorded entry, not just a
             // debugPrint into a release no-op.
             onFailure: (e, st) => unawaited(
               recorder?.record(e, st, source: ErrorSource.app) ??
                   Future<void>.value(),
             ),
           ),
       // ignore: prefer_initializing_formals
       _client = client;

  final McremoteClient _client;
  final NotificationService _notifs;
  final ForegroundServiceController _service;

  StreamSubscription<SessionEvent>? _events;
  StreamSubscription<NotifResponse>? _responses;
  StreamSubscription<McConnectionState>? _conn;
  McConnectionState? _previousConnectionState;
  bool _reconcilingAsks = false;
  final Map<_AskKey, SessionEvent> _knownAsks = {};
  final Set<_AskKey> _shownAsks = {};
  Timer? _maintenanceTimer;
  Duration _maintenanceDelay = const Duration(minutes: 5);
  static const _maxMaintenanceDelay = Duration(minutes: 30);

  /// Master switch from Settings. When false, no notifications are shown and
  /// the keep-alive service is stopped.
  bool enabled = true;

  /// Per-kind filters under [enabled] (MADR 0052 B3).
  NotifyKinds kinds = NotifyKinds.all;

  /// True while the app is on screen; set from the app lifecycle observer.
  bool appForegrounded = true;

  /// Chat screens currently on the navigation stack, bottom→top. A stack —
  /// not a single id — because chat A can push chat B (notification tap):
  /// when B pops, A is visible again and must win the "watching" check.
  final List<String> _watchStack = [];

  /// The session whose chat screen is topmost (null if none).
  String? get currentSessionId => _watchStack.isEmpty ? null : _watchStack.last;

  /// A chat screen for [sessionId] became active (initState).
  ///
  /// Duplicates are kept: two screens for the same session can be stacked (a
  /// double-tapped row, or a notification tap while already in that chat).
  /// De-duplicating meant the first one's dispose released the only entry
  /// while an identical chat was still on screen, so notifications fired for
  /// a session the user was looking at (MADR 0046 L-12).
  void claimSession(String sessionId) {
    _watchStack.add(sessionId);
    _refreshAskNotifications();
  }

  /// A chat screen for [sessionId] left the tree (dispose). Removes one
  /// occurrence, topmost first, so an underlying screen's claim is restored.
  void releaseSession(String sessionId) {
    final at = _watchStack.lastIndexOf(sessionId);
    if (at < 0) return;
    _watchStack.removeAt(at);
    _refreshAskNotifications();
  }

  /// Invoked when the user taps a notification to open a session.
  void Function(String sessionId)? onOpenSession;

  Future<void> start() async {
    _events ??= _client.events.listen(_onEvent);
    _responses ??= _notifs.responses.listen(_onResponse);
    _previousConnectionState = _client.state;
    _conn ??= _client.connectionStates.listen(_onConn);
    await _notifs.init();
    if (_client.state == McConnectionState.connected) {
      unawaited(_reconcilePendingAsks());
    }
    // If a notification tap cold-started this process, the response callback
    // never fired — replay it so the tap still lands on its session.
    final launch = await _notifs.takeLaunchResponse();
    if (launch != null) {
      unawaited(_onResponse(launch));
    }
  }

  Future<void> dispose() async {
    await _events?.cancel();
    await _responses?.cancel();
    await _conn?.cancel();
    // Null the fields so a later start() re-subscribes (the ??= guards would
    // otherwise silently keep a disposed coordinator deaf forever).
    _events = null;
    _responses = null;
    _conn = null;
    for (final ask in _shownAsks) {
      unawaited(_cancelAsk(ask));
    }
    _shownAsks.clear();
    _knownAsks.clear();
    _maintenanceTimer?.cancel();
    _maintenanceTimer = null;
    await _service.stop();
    _notifs.dispose();
  }

  // The user is "watching" — no notification needed — only when the app is
  // foregrounded on that exact session.
  bool _watching(String sessionId) =>
      appForegrounded && currentSessionId == sessionId;

  void _onEvent(SessionEvent ev) {
    if (ev.type == 'session_title' && (ev.title ?? '').trim().isNotEmpty) {
      sessionLabels[ev.sessionId] = ev.title!.trim();
      return;
    }
    final request = _askKeyForRequest(ev);
    if (request != null) {
      _knownAsks[request] = ev;
      _refreshAskNotifications();
      return;
    }
    final resolution = _askKeyForResolution(ev);
    if (resolution != null) {
      _knownAsks.remove(resolution);
      if (_shownAsks.remove(resolution)) unawaited(_cancelAsk(resolution));
      return;
    }
    if (!enabled) return;
    if (!shouldNotify(
      eventType: ev.type,
      watching: _watching(ev.sessionId),
      kinds: kinds,
    )) {
      return;
    }
    switch (ev.type) {
      case 'turn_complete':
        unawaited(
          _notifs.showTurnComplete(
            sessionId: ev.sessionId,
            sessionLabel: _labelFor(ev.sessionId),
          ),
        );
      case 'error':
        unawaited(
          _notifs.showError(
            sessionId: ev.sessionId,
            sessionLabel: _labelFor(ev.sessionId),
            detail: ev.text,
          ),
        );
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
            return;
          }
          // Only a successful respond retires the notification (its actions
          // deliberately don't self-cancel): a failed send must leave the
          // user an affordance to retry.
          unawaited(_notifs.cancelPermission(p.sessionId, pid));
        } catch (e) {
          debugPrint('notification respond failed: $e');
          onOpenSession?.call(p.sessionId);
        }
    }
  }

  void _onConn(McConnectionState state) {
    final previous = _previousConnectionState;
    _previousConnectionState = state;
    if (state == McConnectionState.connected &&
        previous != null &&
        previous != McConnectionState.connected) {
      unawaited(_reconcilePendingAsks());
    }
    // Keep-alive service only while notifications are on and the app can start
    // an FGS (prefer foreground — API 31+ denies background starts). H-5a:
    // titles reflect real socket state; never claim "Listening" after a failed
    // start.
    if (!enabled) {
      unawaited(_service.stop());
    } else if (state == McConnectionState.connected ||
        state == McConnectionState.reconnecting) {
      if (appForegrounded || state == McConnectionState.connected) {
        unawaited(
          _service.start().then((_) {
            if (!_service.lastStartOk) return;
            unawaited(
              _service.update(
                title: state == McConnectionState.connected
                    ? 'Connected to host'
                    : 'Reconnecting to host',
                text: state == McConnectionState.connected
                    ? 'Listening for approvals and completions'
                    : 'Retrying connection',
              ),
            );
          }),
        );
      }
    } else if (state == McConnectionState.disconnected) {
      unawaited(_service.stop());
    } else if (state == McConnectionState.error) {
      unawaited(
        _service.update(
          title: 'Connection unavailable',
          text: 'Open the app to reconnect',
        ),
      );
    }
    _refreshMaintenanceRetry();
  }

  /// Apply the master switch (from Settings): stop the service immediately
  /// when turned off; start it immediately when turned on with a live
  /// connection — waiting for the next connection transition would leave the
  /// process unprotected until then.
  Future<void> setEnabled(bool value) async {
    enabled = value;
    if (!value) {
      await _service.stop();
      _cancelMaintenanceRetry();
      _refreshAskNotifications();
      return;
    }
    final s = _client.state;
    if (s == McConnectionState.connected ||
        s == McConnectionState.reconnecting) {
      await _service.start();
    }
    _refreshAskNotifications();
    _refreshMaintenanceRetry();
  }

  void setAppForegrounded(bool value) {
    appForegrounded = value;
    _refreshAskNotifications();
    _refreshMaintenanceRetry();
  }

  void _cancelMaintenanceRetry({bool reset = false}) {
    _maintenanceTimer?.cancel();
    _maintenanceTimer = null;
    if (reset) {
      _maintenanceDelay = const Duration(minutes: 5);
    }
  }

  void _refreshMaintenanceRetry() {
    // The slow retry exists to feed the Android keep-alive service. Where no
    // such service exists (iOS: the process suspends and Timers never fire,
    // MADR 0067 F3/D2) an armed timer is dead at best and replays as a stale
    // reconnect burst on resume at worst — so it is never armed there.
    final eligible =
        platformKeepsProcessAliveInBackground(defaultTargetPlatform) &&
        enabled &&
        !appForegrounded &&
        _client.reconnectParked &&
        _client.hasCredentials;
    if (!eligible) {
      _cancelMaintenanceRetry(
        reset: _client.state == McConnectionState.connected || appForegrounded,
      );
      return;
    }
    if (_maintenanceTimer != null) return;
    _maintenanceTimer = Timer(_maintenanceDelay, () async {
      _maintenanceTimer = null;
      if (!(enabled &&
          !appForegrounded &&
          _client.reconnectParked &&
          _client.hasCredentials)) {
        return;
      }
      try {
        // Background maintenance, not a user action: it must respect the
        // per-generation transport fallback budget, or every slow retry tick
        // would buy another free mesh↔relay hop (MADR 0062 D11).
        await _client.reconnect(userInitiated: false);
      } catch (e) {
        debugPrint('background maintenance reconnect: $e');
      }
      _maintenanceDelay = Duration(
        minutes: (_maintenanceDelay.inMinutes * 2).clamp(
          5,
          _maxMaintenanceDelay.inMinutes,
        ),
      );
      _refreshMaintenanceRetry();
    });
  }

  _AskKey? _askKeyForRequest(SessionEvent event) {
    final permission = event.permissionId;
    if (event.type == 'permission_request' &&
        permission != null &&
        permission.isNotEmpty) {
      return (NotifKind.permission, event.sessionId, permission);
    }
    final question = event.questionId;
    if (event.type == 'question_request' &&
        question != null &&
        question.isNotEmpty) {
      return (NotifKind.question, event.sessionId, question);
    }
    return null;
  }

  _AskKey? _askKeyForResolution(SessionEvent event) {
    final permission = event.permissionId;
    if (event.type == 'permission_resolved' &&
        permission != null &&
        permission.isNotEmpty) {
      return (NotifKind.permission, event.sessionId, permission);
    }
    final question = event.questionId;
    if (event.type == 'question_resolved' &&
        question != null &&
        question.isNotEmpty) {
      return (NotifKind.question, event.sessionId, question);
    }
    return null;
  }

  Future<void> _reconcilePendingAsks() async {
    if (_reconcilingAsks) return;
    _reconcilingAsks = true;
    try {
      final events = await _client.pendingAsks();
      final replacement = <_AskKey, SessionEvent>{};
      for (final event in events) {
        final key = _askKeyForRequest(event);
        if (key != null) replacement[key] = event;
      }
      final stale = _shownAsks
          .where((key) => !replacement.containsKey(key))
          .toList();
      _knownAsks
        ..clear()
        ..addAll(replacement);
      for (final key in stale) {
        _shownAsks.remove(key);
        unawaited(_cancelAsk(key));
      }
      _refreshAskNotifications();
    } catch (e) {
      debugPrint('pending ask reconciliation failed: $e');
    } finally {
      _reconcilingAsks = false;
    }
  }

  void _refreshAskNotifications() {
    for (final entry in _knownAsks.entries) {
      final key = entry.key;
      final visible = enabled && kinds.asks && !_watching(key.$2);
      if (visible && _shownAsks.add(key)) {
        unawaited(_showAsk(key, entry.value));
      } else if (!visible && _shownAsks.remove(key)) {
        unawaited(_cancelAsk(key));
      }
    }
  }

  /// Forget every outstanding ask for [sessionId] and pull its notifications.
  ///
  /// The daemon emits no resolution when a session closes — the entries simply
  /// stop appearing in the pending-ask snapshot — and on a stable connection
  /// nothing re-reads that snapshot. So without this an Allow/Deny notification
  /// for a session the user just ended stays in the shade, actionable, and
  /// tapping Allow lands them in a dead session (MADR 0046 M-4).
  void dropSessionAsks(String sessionId) {
    if (sessionId.isEmpty) return;
    _dropAsks((key) => key.$2 == sessionId);
  }

  /// Forget every outstanding ask, for sign-out: the responses could not be
  /// delivered even if the user tapped them.
  void dropAllAsks() => _dropAsks((_) => true);

  void _dropAsks(bool Function(_AskKey key) matches) {
    final doomed = _knownAsks.keys.where(matches).toList();
    for (final key in doomed) {
      _knownAsks.remove(key);
      if (_shownAsks.remove(key)) unawaited(_cancelAsk(key));
    }
  }

  Future<void> _showAsk(_AskKey key, SessionEvent event) => switch (key.$1) {
    NotifKind.permission => _notifs.showPermission(
      sessionId: key.$2,
      permissionId: key.$3,
      toolName: event.toolName ?? 'tool',
      detail: event.text,
      allowOptionId: _allowOptionId(event.options),
    ),
    NotifKind.question => _notifs.showQuestion(
      sessionId: key.$2,
      questionId: key.$3,
      sessionLabel: _labelFor(key.$2),
      detail: event.text,
    ),
    NotifKind.turnComplete || NotifKind.error => Future.value(),
  };

  Future<void> _cancelAsk(_AskKey key) => switch (key.$1) {
    NotifKind.permission => _notifs.cancelPermission(key.$2, key.$3),
    NotifKind.question => _notifs.cancelQuestion(key.$2, key.$3),
    NotifKind.turnComplete || NotifKind.error => Future.value(),
  };

  /// Session display names, fed by the sessions layer so notification bodies
  /// can say "Fix the build" instead of "Session 1a2b3c4d".
  final Map<String, String> sessionLabels = {};

  /// Whether the OS is currently blocking notifications (Settings surfaces
  /// this next to the in-app toggle). Null = unknown/unsupported.
  /// Set when the notification plugin could not be initialised, so Settings
  /// can say why alerts are missing instead of showing a healthy toggle.
  Object? get notificationsUnavailable => _notifs.lastInitError;

  Future<bool?> osBlocked() async {
    final enabled = await _notifs.areNotificationsEnabled();
    if (enabled == null) return null;
    return !enabled;
  }

  /// Re-request the OS permission (used when the user flips the toggle on).
  Future<bool?> requestOsPermission() => _notifs.requestPermission();

  String _labelFor(String id) {
    final name = sessionLabels[id];
    if (name != null && name.isNotEmpty) return name;
    return id.length > 8 ? 'Session ${id.substring(0, 8)}' : id;
  }

  /// Pick the option that means "allow", mirroring the in-app permission
  /// sheet. Deliberately conservative: substring matching would classify
  /// "disallow"/"not_allowed" as allow, and defaulting to the first option
  /// could fire an arbitrary action from a shade button — when unsure, return
  /// null and let the tap open the full sheet instead.
  static String? _allowOptionId(List<PermissionOption> options) {
    const allowKinds = {'allow_once', 'allow-once', 'allow'};
    const allowIds = {'allow', 'allow_once', 'allow-once', 'yes', 'approve'};
    for (final o in options) {
      final kind = (o.kind ?? '').toLowerCase();
      final id = o.optionId.toLowerCase();
      if (allowKinds.contains(kind) || allowIds.contains(id)) {
        return o.optionId;
      }
    }
    return null;
  }
}

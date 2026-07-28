import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/notifications/agent_notifications.dart';
import 'package:magic_cli_remote/data/notifications/notification_coordinator.dart';
import 'package:magic_cli_remote/data/notifications/notification_service.dart';
import 'package:magic_cli_remote/data/notifications/foreground_service.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';

class _AskClient extends McremoteClient {
  _AskClient() : super(settings: SettingsStore());

  final eventController = StreamController<SessionEvent>.broadcast();
  final connectionController = StreamController<McConnectionState>.broadcast();
  McConnectionState current = McConnectionState.disconnected;
  List<SessionEvent> snapshot = const [];
  Object? snapshotError;

  @override
  Stream<SessionEvent> get events => eventController.stream;

  @override
  Stream<McConnectionState> get connectionStates => connectionController.stream;

  @override
  McConnectionState get state => current;

  @override
  Future<List<SessionEvent>> pendingAsks() async {
    final error = snapshotError;
    if (error != null) throw error;
    return snapshot;
  }

  void emitConnection(McConnectionState state) {
    current = state;
    connectionController.add(state);
  }

  Future<void> close() async {
    await eventController.close();
    await connectionController.close();
  }
}

class _Notifications extends NotificationService {
  final shown = <String>[];
  final cancelled = <String>[];

  String _key(String kind, String sessionId, String requestId) =>
      '$kind:$sessionId:$requestId';

  @override
  Future<void> init() async {}

  @override
  Future<NotifResponse?> takeLaunchResponse() async => null;

  @override
  Future<void> showPermission({
    required String sessionId,
    required String permissionId,
    required String toolName,
    String? detail,
    String? allowOptionId,
  }) async => shown.add(_key('permission', sessionId, permissionId));

  @override
  Future<void> showQuestion({
    required String sessionId,
    required String questionId,
    required String sessionLabel,
    String? detail,
  }) async => shown.add(_key('question', sessionId, questionId));

  @override
  Future<void> cancelPermission(String sessionId, String permissionId) async =>
      cancelled.add(_key('permission', sessionId, permissionId));

  @override
  Future<void> cancelQuestion(String sessionId, String questionId) async =>
      cancelled.add(_key('question', sessionId, questionId));
}

class _ForegroundService extends ForegroundServiceController {
  @override
  Future<void> start() async {}

  @override
  Future<void> stop() async {}

  @override
  Future<void> update({required String title, required String text}) async {}
}

Future<void> _settle() => Future<void>.delayed(Duration.zero);

void main() {
  group('NotifPayload codec', () {
    test('round-trips a permission payload with allow option', () {
      const p = NotifPayload(
        kind: NotifKind.permission,
        sessionId: 's1',
        permissionId: 'perm-9',
        allowOptionId: 'allow_once',
      );
      final back = NotifPayload.decode(p.encode());
      expect(back, p);
    });

    test('round-trips a turn-complete payload', () {
      const p = NotifPayload(kind: NotifKind.turnComplete, sessionId: 's2');
      expect(NotifPayload.decode(p.encode()), p);
    });

    test('rejects malformed / empty / unknown-kind input', () {
      expect(NotifPayload.decode(null), isNull);
      expect(NotifPayload.decode(''), isNull);
      expect(NotifPayload.decode('not json'), isNull);
      expect(NotifPayload.decode('{"k":"bogus","s":"x"}'), isNull);
      expect(NotifPayload.decode('{"k":"permission"}'), isNull); // no session
    });
  });

  group('shouldNotify', () {
    test('notifies for permission and turn_complete when not watching', () {
      expect(
        shouldNotify(eventType: 'permission_request', watching: false),
        isTrue,
      );
      expect(shouldNotify(eventType: 'turn_complete', watching: false), isTrue);
    });

    test('suppresses when the user is watching that session', () {
      expect(
        shouldNotify(eventType: 'permission_request', watching: true),
        isFalse,
      );
      expect(shouldNotify(eventType: 'turn_complete', watching: true), isFalse);
    });

    test('ignores unrelated event types', () {
      expect(
        shouldNotify(eventType: 'assistant_message_chunk', watching: false),
        isFalse,
      );
      expect(shouldNotify(eventType: 'tool_call', watching: false), isFalse);
    });
  });

  group('notificationIdFor', () {
    test('is stable and positive, and distinguishes perm vs session', () {
      final permId = notificationIdFor(
        kind: NotifKind.permission,
        sessionId: 's1',
        requestId: 'p1',
      );
      final sessId = notificationIdFor(
        kind: NotifKind.turnComplete,
        sessionId: 's1',
      );
      expect(permId, greaterThanOrEqualTo(0));
      expect(sessId, greaterThanOrEqualTo(0));
      expect(permId, isNot(sessId));
      // Deterministic.
      expect(
        notificationIdFor(
          kind: NotifKind.permission,
          sessionId: 's1',
          requestId: 'p1',
        ),
        permId,
      );
    });
  });

  group('NotificationCoordinator watch stack', () {
    NotificationCoordinator coord() => NotificationCoordinator(
      client: McremoteClient(settings: SettingsStore()),
    );

    test('claim/release tracks the topmost chat screen', () {
      final c = coord();
      expect(c.currentSessionId, isNull);
      c.claimSession('a');
      expect(c.currentSessionId, 'a');
      c.claimSession('b');
      expect(c.currentSessionId, 'b');
      // Chat B pops: A is visible again and must win the watching check —
      // the old single-field logic left this null and notified about A while
      // the user was looking at it.
      c.releaseSession('b');
      expect(c.currentSessionId, 'a');
      c.releaseSession('a');
      expect(c.currentSessionId, isNull);
    });

    test('re-claiming an id already on the stack moves it to the top', () {
      final c = coord();
      c.claimSession('a');
      c.claimSession('b');
      c.claimSession('a');
      expect(c.currentSessionId, 'a');
      c.releaseSession('a');
      expect(c.currentSessionId, 'b');
    });

    test('releasing an id not on the stack is a no-op', () {
      final c = coord();
      c.claimSession('a');
      c.releaseSession('zzz');
      expect(c.currentSessionId, 'a');
    });

    test('two screens for one session need two releases', () {
      final c = coord();
      // A double-tapped row, or a notification tap while already in that
      // chat, pushes the same session twice. De-duplicating the claim let the
      // first dispose drop the only entry while an identical chat was still
      // on screen — and the coordinator then notified about it (L-12).
      c.claimSession('a');
      c.claimSession('a');
      c.releaseSession('a');
      expect(
        c.currentSessionId,
        'a',
        reason: 'a chat for this session is still visible',
      );
      c.releaseSession('a');
      expect(c.currentSessionId, isNull);
    });
  });

  group('NotificationCoordinator pending asks', () {
    late _AskClient client;
    late _Notifications notifications;
    late NotificationCoordinator coordinator;

    setUp(() async {
      client = _AskClient();
      notifications = _Notifications();
      coordinator = NotificationCoordinator(
        client: client,
        notifications: notifications,
        service: _ForegroundService(),
      );
      await coordinator.start();
    });

    tearDown(() async {
      await coordinator.dispose();
      await client.close();
    });

    test('reconnect snapshot cancels stale asks and is idempotent', () async {
      client.eventController.add(
        SessionEvent(
          type: 'permission_request',
          sessionId: 's1',
          permissionId: 'p1',
        ),
      );
      await _settle();
      expect(notifications.shown, ['permission:s1:p1']);

      client.snapshot = const [];
      client.emitConnection(McConnectionState.error);
      client.emitConnection(McConnectionState.connected);
      await _settle();
      await _settle();
      expect(notifications.cancelled, ['permission:s1:p1']);

      client.emitConnection(McConnectionState.reconnecting);
      client.emitConnection(McConnectionState.connected);
      await _settle();
      expect(notifications.cancelled, ['permission:s1:p1']);
    });

    test('ending a session pulls its actionable notifications', () async {
      client.eventController.add(
        SessionEvent(
          type: 'permission_request',
          sessionId: 's1',
          permissionId: 'p1',
        ),
      );
      client.eventController.add(
        SessionEvent(
          type: 'question_request',
          sessionId: 's2',
          questionId: 'q1',
        ),
      );
      await _settle();
      expect(notifications.shown, ['permission:s1:p1', 'question:s2:q1']);

      // The daemon emits no resolution when a session closes, and on a stable
      // connection nothing re-reads the pending-ask snapshot — so an Allow
      // notification for an ended session would sit there forever, and
      // tapping it would drop the user into a dead chat (MADR 0046 M-4).
      coordinator.dropSessionAsks('s1');
      await _settle();
      expect(notifications.cancelled, ['permission:s1:p1']);

      // Only that session's asks go; the other is untouched.
      expect(notifications.cancelled, isNot(contains('question:s2:q1')));

      // And it stays gone: a later refresh must not resurrect it.
      coordinator.claimSession('s2');
      coordinator.releaseSession('s2');
      await _settle();
      expect(
        notifications.shown.where((s) => s == 'permission:s1:p1').length,
        1,
      );
    });

    test('signing out pulls every actionable notification', () async {
      client.eventController.add(
        SessionEvent(
          type: 'permission_request',
          sessionId: 's1',
          permissionId: 'p1',
        ),
      );
      client.eventController.add(
        SessionEvent(
          type: 'question_request',
          sessionId: 's2',
          questionId: 'q1',
        ),
      );
      await _settle();

      // After sign-out the responses could not be delivered even if tapped.
      coordinator.dropAllAsks();
      await _settle();
      expect(
        notifications.cancelled,
        containsAll(<String>['permission:s1:p1', 'question:s2:q1']),
      );
    });

    test('failed snapshot retains the current actionable ask', () async {
      client.eventController.add(
        SessionEvent(
          type: 'question_request',
          sessionId: 's1',
          questionId: 'q1',
        ),
      );
      await _settle();
      client.snapshotError = StateError('offline');
      client.emitConnection(McConnectionState.error);
      client.emitConnection(McConnectionState.connected);
      await _settle();
      await _settle();
      expect(notifications.shown, ['question:s1:q1']);
      expect(notifications.cancelled, isEmpty);
    });

    test('watching a session suppresses but retains its pending ask', () async {
      coordinator.claimSession('s1');
      client.eventController.add(
        SessionEvent(
          type: 'permission_request',
          sessionId: 's1',
          permissionId: 'p1',
        ),
      );
      await _settle();
      expect(notifications.shown, isEmpty);

      coordinator.setAppForegrounded(false);
      await _settle();
      expect(notifications.shown, ['permission:s1:p1']);
    });
  });
}

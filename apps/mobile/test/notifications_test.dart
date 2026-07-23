import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/notifications/agent_notifications.dart';
import 'package:magic_cli_remote/data/notifications/notification_coordinator.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';

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
      final permId = notificationIdFor(sessionId: 's1', permissionId: 'p1');
      final sessId = notificationIdFor(sessionId: 's1');
      expect(permId, greaterThanOrEqualTo(0));
      expect(sessId, greaterThanOrEqualTo(0));
      expect(permId, isNot(sessId));
      // Deterministic.
      expect(notificationIdFor(sessionId: 's1', permissionId: 'p1'), permId);
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
  });
}

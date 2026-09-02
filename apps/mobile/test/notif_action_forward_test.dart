import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/notifications/agent_notifications.dart';
import 'package:magic_cli_remote/data/notifications/foreground_service.dart';

// MADR 0129 P5 deviation (2026-09-02). With the app swiped away, an Allow/Deny
// tap reaches neither isolate that could answer it: `showsUserInterface: false`
// sends it to `ActionBroadcastReceiver`, which runs the background callback in
// a THIRD isolate with no socket. That isolate forwards; the service isolate
// answers. This is the wire format for the hop.
//
// Worth testing at all because both ends are unreachable from a widget test —
// one lives in a plugin-spawned engine, the other in the foreground service —
// so the encoding is the only part a machine can check before the device does.
void main() {
  group('NotifActionForward round-trip', () {
    test('allow survives the hop with its payload intact', () {
      final payload = const NotifPayload(
        kind: NotifKind.permission,
        sessionId: 's-1',
        permissionId: 'p-1',
        allowOptionId: 'allow_once',
      ).encode();

      final decoded = NotifActionForward.decode(
        NotifActionForward(
          action: NotifAction.allow,
          payload: payload,
        ).encode(),
      );

      expect(decoded, isNotNull);
      expect(decoded!.action, NotifAction.allow);
      // The payload crosses as an opaque string so exactly one decoder parses
      // it, on the far side. A second parse here is what would let the two
      // sides disagree about a malformed payload.
      expect(decoded.payload, payload);
      expect(NotifPayload.decode(decoded.payload)?.permissionId, 'p-1');
    });

    test('deny survives the hop', () {
      final decoded = NotifActionForward.decode(
        const NotifActionForward(
          action: NotifAction.deny,
          payload: '{"k":"permission","s":"s-1","p":"p-1"}',
        ).encode(),
      );
      expect(decoded?.action, NotifAction.deny);
    });
  });

  group('NotifActionForward.decode rejects what is not a tap', () {
    // The service isolate has exactly one inbound channel, so the heartbeat and
    // a forwarded tap arrive the same way. Mistaking one for the other would
    // either answer a permission nobody tapped or stop the notification ever
    // being corrected.
    test('the UI isolate heartbeat is not a tap', () {
      expect(
        NotifActionForward.decode(<String, Object>{
          ForegroundServiceMessages.heartbeat: true,
          ForegroundServiceMessages.title: 'Connected to host',
          ForegroundServiceMessages.text: 'Listening',
        }),
        isNull,
      );
    });

    test('the release acknowledgement is not a tap', () {
      expect(
        NotifActionForward.decode(<String, Object>{
          ForegroundServiceMessages.released: true,
        }),
        isNull,
      );
    });

    for (final (name, value) in <(String, Object?)>[
      ('null', null),
      ('a bare string', 'allow'),
      ('a list', <String>['allow']),
      ('an empty map', <String, Object>{}),
    ]) {
      test('$name is not a tap', () {
        expect(NotifActionForward.decode(value), isNull);
      });
    }

    test('an unknown action name is refused, not guessed', () {
      // Forwarding an action this build does not understand would make the
      // service isolate answer on the user's behalf without knowing what they
      // pressed.
      expect(
        NotifActionForward.decode(<String, Object>{
          'mc.notifAction': 'allow_always',
          'mc.notifPayload': '{"k":"permission","s":"s-1","p":"p-1"}',
        }),
        isNull,
      );
    });

    test('an empty payload is refused', () {
      expect(
        NotifActionForward.decode(<String, Object>{
          'mc.notifAction': 'allow',
          'mc.notifPayload': '',
        }),
        isNull,
      );
    });
  });

  test('a tap is not mistaken for a heartbeat either', () {
    // The other direction of the same collision: onReceiveData checks the
    // forward first and returns, so a tap must not carry the heartbeat key.
    final encoded = const NotifActionForward(
      action: NotifAction.allow,
      payload: '{"k":"permission","s":"s-1","p":"p-1"}',
    ).encode();
    expect(encoded.containsKey(ForegroundServiceMessages.heartbeat), isFalse);
    expect(encoded.containsKey(ForegroundServiceMessages.released), isFalse);
  });
}

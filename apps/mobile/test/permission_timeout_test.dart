import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/chat_models.dart';
import 'package:magic_cli_remote/data/chat/transcript_reducer.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';

/// A permission that times out takes its pending card away with it, so the
/// system line is the only thing telling the user why the request vanished.

SessionEvent request(String id) => SessionEvent(
  type: 'permission_request',
  sessionId: 's1',
  permissionId: id,
  toolName: 'command',
  text: 'rm -rf /tmp/x',
);

SessionEvent timedOut(String id, {bool replay = false}) => SessionEvent(
  type: 'permission_resolved',
  sessionId: 's1',
  permissionId: id,
  timedOut: true,
  replay: replay,
);

int noticeCount(SessionTranscript t) =>
    t.items.where((i) => i.text == 'Permission request timed out').length;

void main() {
  test('every timeout is reported, not just the first of a session', () {
    var t = const SessionTranscript(sessionId: 's1');
    t = applySessionEvent(t, request('p1'));
    t = applySessionEvent(t, timedOut('p1'));
    expect(noticeCount(t), 1);

    // A second permission times out much later in the same session. Deduping
    // on the notice text hid this one entirely: the card disappeared and
    // nothing said why (MADR 0046 M-6).
    t = applySessionEvent(t, request('p2'));
    t = applySessionEvent(t, timedOut('p2'));
    expect(noticeCount(t), 2);
  });

  test('replaying one resolution does not duplicate its notice', () {
    var t = const SessionTranscript(sessionId: 's1');
    t = applySessionEvent(t, request('p1'));
    t = applySessionEvent(t, timedOut('p1'));
    t = applySessionEvent(t, timedOut('p1', replay: true));
    expect(noticeCount(t), 1);
  });

  test('the dedupe key survives a cache round-trip', () {
    var t = const SessionTranscript(sessionId: 's1');
    t = applySessionEvent(t, request('p1'));
    t = applySessionEvent(t, timedOut('p1'));
    final notice = t.items.last;
    expect(notice.dedupeKey, 'perm-timeout:p1');

    // Otherwise a resolution replayed after process death appends a duplicate.
    final restored = ChatItem.fromJson(notice.toJson());
    expect(restored.dedupeKey, 'perm-timeout:p1');
  });
}

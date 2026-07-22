import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

SessionEvent _ev(String type, {String? text, String? status}) =>
    SessionEvent(type: type, sessionId: 's1', text: text, status: status);

void main() {
  ProviderContainer makeContainer() {
    final c = ProviderContainer();
    addTearDown(c.dispose);
    return c;
  }

  group('history replay', () {
    test('feeds events into an empty transcript in order', () {
      final c = makeContainer();
      c.read(transcriptsProvider.notifier).replayHistory('s1', [
        _ev('user_message', text: 'hello'),
        _ev('assistant_message_chunk', text: 'hi '),
        _ev('assistant_message_chunk', text: 'there'),
      ]);
      final t = c.read(transcriptsProvider).forSession('s1');
      expect(t.items, hasLength(2));
      expect(t.items.first.kind, ChatItemKind.user);
      expect(t.items.first.text, 'hello');
      // Coalesced assistant chunks, exactly as the live path would build them.
      expect(t.items[1].kind, ChatItemKind.assistant);
      expect(t.items[1].text, 'hi there');
    });

    test('is skipped when the transcript is already populated (race guard)', () {
      final c = makeContainer();
      final notifier = c.read(transcriptsProvider.notifier);
      // A live event won the race and populated the transcript first.
      notifier.replayHistory('s1', [_ev('user_message', text: 'live')]);
      final before = c.read(transcriptsProvider).forSession('s1');
      expect(before.items, hasLength(1));

      // History arrives late: because the transcript is no longer empty it must
      // be dropped wholesale rather than double-applied.
      notifier.replayHistory('s1', [
        _ev('user_message', text: 'stale-a'),
        _ev('user_message', text: 'stale-b'),
      ]);
      final after = c.read(transcriptsProvider).forSession('s1');
      expect(after.items, hasLength(1));
      expect(after.items.single.text, 'live');
    });

    test('empty history list is a no-op', () {
      final c = makeContainer();
      c.read(transcriptsProvider.notifier).replayHistory('s1', const []);
      expect(c.read(transcriptsProvider).peek('s1'), isNull);
    });
  });

  group('prunePresentedPermissionIds', () {
    test('drops a resolved id but keeps still-pending ones', () {
      final presented = {'p1', 'p2', 'p3'};
      // p2 has resolved (left pendingPermissions); p1 and p3 are still pending.
      final dropped = prunePresentedPermissionIds(presented, {'p1', 'p3'});
      expect(dropped, {'p2'});
      expect(presented, {'p1', 'p3'});
    });

    test('never drops a still-pending id', () {
      final presented = {'p1'};
      final dropped = prunePresentedPermissionIds(presented, {'p1'});
      expect(dropped, isEmpty);
      expect(presented, {'p1'});
    });
  });
  group('seq-based resync', () {
    SessionEvent seqEv(String type, int seq, {String? text}) =>
        SessionEvent(type: type, sessionId: 's1', seq: seq, text: text);

    test('populated transcript gains events missed during an outage', () {
      final c = makeContainer();
      final n = c.read(transcriptsProvider.notifier);
      // Live events seen before the outage.
      n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
      n.debugOnEvent(seqEv('assistant_message_chunk', 2, text: 'hi'));
      // Outage: seq 3-4 broadcast while the socket was down. The refetched
      // ring contains everything; resync must rebuild rather than drop it.
      n.resyncHistory('s1', [
        seqEv('user_message', 1, text: 'hello'),
        seqEv('assistant_message_chunk', 2, text: 'hi'),
        seqEv('assistant_message_chunk', 3, text: ' there'),
        seqEv('turn_complete', 4),
      ]);
      final t = c.read(transcriptsProvider).forSession('s1');
      final assistant = t.items.where((i) => i.kind == ChatItemKind.assistant);
      expect(assistant.single.text, 'hi there');
      expect(t.status, 'idle');
    });

    test('resync is a no-op when nothing was missed', () {
      final c = makeContainer();
      final n = c.read(transcriptsProvider.notifier);
      n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
      n.debugOnEvent(seqEv('assistant_message_chunk', 2, text: 'hi'));
      final before = c.read(transcriptsProvider).forSession('s1');
      n.resyncHistory('s1', [
        seqEv('user_message', 1, text: 'hello'),
        seqEv('assistant_message_chunk', 2, text: 'hi'),
      ]);
      final after = c.read(transcriptsProvider).forSession('s1');
      expect(identical(after, before), isTrue);
    });
  });
}

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/chat_models.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// MADR 0056 Phase 0 / H-1: reconnect marks gaps for every known session, but
/// only a mounted ChatScreen fetches history. A populated inactive session
/// therefore never heals. Phase 2 SessionSynchronizer must clear the gap and
/// apply missed events without requiring that chat to be open during reconnect.
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  SessionEvent seqEv(String type, int seq, {String? text}) =>
      SessionEvent(type: type, sessionId: 'A', seq: seq, text: text);

  test('H-1: gap after reconnect is not healed by empty-only open path '
      '(fails until Phase 2)', () async {
    final c = ProviderContainer();
    addTearDown(c.dispose);
    final n = c.read(transcriptsProvider.notifier);

    // Session A was populated while online (inactive chat later).
    n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
    n.debugOnEvent(seqEv('assistant_message_chunk', 2, text: 'hi'));
    expect(n.debugLastSeq('A'), 2);
    expect(c.read(transcriptsProvider).forSession('A').items, isNotEmpty);

    // Reconnect while A is not mounted: only gap flags are set today.
    n.debugMarkAllGapsSuspected();
    expect(n.debugGapSuspected('A'), isTrue);

    // Host retained a newer event while the phone was offline.
    final hostHistory = [
      seqEv('user_message', 1, text: 'hello'),
      seqEv('assistant_message_chunk', 2, text: 'hi'),
      seqEv('assistant_message_chunk', 3, text: ' missed'),
    ];

    // Today's chat-open path: skip history when local items already exist.
    final local = c.read(transcriptsProvider).forSession('A');
    if (local.items.isEmpty) {
      await n.replayHistory('A', hostHistory);
    }
    // (No resyncHistory call — that only runs from mounted ChatScreen.)

    // Desired contract (Phase 2): inactive-session resync applies missed
    // events and clears the gap without a mounted chat during reconnect.
    final after = c.read(transcriptsProvider).forSession('A');
    final text = after.items
        .where((i) => i.kind == ChatItemKind.assistant)
        .map((i) => i.text ?? '')
        .join();
    expect(
      text.contains('missed'),
      isTrue,
      reason:
          'MADR 0056 H-1: missed offline events must reach a populated '
          'session after reconnect without requiring chat mounted at reconnect',
    );
    expect(
      n.debugGapSuspected('A'),
      isFalse,
      reason:
          'MADR 0056 H-1: gap flag must clear after connection-scoped resync',
    );
  });
}

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/session_synchronizer.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// MADR 0056 Phase 2 / H-1: connection-scoped synchronizer heals inactive
/// populated sessions after reconnect without requiring ChatScreen mounted.
class _HistoryClient extends McremoteClient {
  _HistoryClient(this.historyBySession);

  final Map<String, List<SessionEvent>> historyBySession;
  int historyCalls = 0;

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async {
    return SessionListSnapshot(
      sessions: [
        for (final id in historyBySession.keys)
          SessionMeta(id: id, provider: 'fake', name: id, live: true),
      ],
      complete: true,
    );
  }

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async {
    historyCalls++;
    return historyBySession[sessionId] ?? const [];
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  SessionEvent seqEv(String type, int seq, {String? text, String sid = 'A'}) =>
      SessionEvent(type: type, sessionId: sid, seq: seq, text: text);

  test(
    'H-1: ensureSession applies missed history to a populated gapped session',
    () async {
      final hostHistory = [
        seqEv('user_message', 1, text: 'hello'),
        seqEv('assistant_message_chunk', 2, text: 'hi'),
        seqEv('assistant_message_chunk', 3, text: ' missed'),
      ];
      final client = _HistoryClient({'A': hostHistory});
      final c = ProviderContainer(
        overrides: [mcremoteClientProvider.overrideWithValue(client)],
      );
      addTearDown(c.dispose);

      // Keep synchronizer alive.
      c.read(sessionSynchronizerProvider);
      final n = c.read(transcriptsProvider.notifier);

      n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
      n.debugOnEvent(seqEv('assistant_message_chunk', 2, text: 'hi'));
      expect(n.debugLastSeq('A'), 2);
      n.debugMarkAllGapsSuspected();
      expect(n.debugGapSuspected('A'), isTrue);

      // Chat was not mounted during reconnect — only ensureSession (open path)
      // or full resync runs.
      await c.read(sessionSynchronizerProvider.notifier).ensureSession('A');

      final after = c.read(transcriptsProvider).forSession('A');
      final text = after.items
          .where((i) => i.kind == ChatItemKind.assistant)
          .map((i) => i.text ?? '')
          .join();
      expect(text.contains('missed'), isTrue);
      expect(n.debugGapSuspected('A'), isFalse);
      expect(client.historyCalls, greaterThan(0));
    },
  );

  test('H-1: full resync heals without a mounted chat', () async {
    final hostHistory = [
      seqEv('user_message', 1, text: 'hello'),
      seqEv('assistant_message_chunk', 2, text: 'hi'),
      seqEv('assistant_message_chunk', 3, text: ' offline'),
    ];
    final client = _HistoryClient({'A': hostHistory});
    final c = ProviderContainer(
      overrides: [mcremoteClientProvider.overrideWithValue(client)],
    );
    addTearDown(c.dispose);
    c.read(sessionSynchronizerProvider);
    final n = c.read(transcriptsProvider.notifier);

    n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
    n.debugOnEvent(seqEv('assistant_message_chunk', 2, text: 'hi'));
    n.debugMarkAllGapsSuspected();

    await c.read(sessionSynchronizerProvider.notifier).resync();

    final text = c
        .read(transcriptsProvider)
        .forSession('A')
        .items
        .where((i) => i.kind == ChatItemKind.assistant)
        .map((i) => i.text ?? '')
        .join();
    expect(text.contains('offline'), isTrue);
    expect(n.debugGapSuspected('A'), isFalse);
  });
}

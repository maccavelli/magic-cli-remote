import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/session_synchronizer.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// MADR 0056 Phase 2 / H-1: connection-scoped synchronizer heals inactive
/// populated sessions after reconnect without requiring ChatScreen mounted.
class _HistoryClient extends McremoteClient {
  _HistoryClient(
    this.historyBySession, {
    this.epoch,
    this.seqs = const {},
    this.metaStatus = 'idle',
  });

  final Map<String, List<SessionEvent>> historyBySession;
  int historyCalls = 0;
  int listAttemptsCount = 0;

  /// MADR 0068 P3/P4 surfaces; mutable so tests can flip them mid-run.
  String? epoch;
  Map<String, SeqBounds> seqs;
  Map<String, SeqBounds>? resumedOverride;
  String metaStatus;

  @override
  Map<String, SeqBounds>? get lastResumed => resumedOverride;

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async {
    listAttemptsCount++;
    return SessionListSnapshot(
      sessions: [
        for (final id in historyBySession.keys)
          SessionMeta(
            id: id,
            provider: 'fake',
            name: id,
            live: true,
            status: metaStatus,
          ),
      ],
      complete: true,
      epoch: epoch,
      seqs: seqs,
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

  // -----------------------------------------------------------------------
  // MADR 0068 P3 — gap-scaled resync: fast-skip, truncation, epoch, re-arm.
  // -----------------------------------------------------------------------

  group('gap-scaled resync (MADR 0068 P3)', () {
    test(
      'up to date: matching latest_seq skips the history walk entirely',
      () async {
        final client = _HistoryClient(
          {'A': []},
          epoch: 'e1',
          seqs: {'A': const SeqBounds(first: 1, latest: 2)},
        );
        final c = ProviderContainer(
          overrides: [mcremoteClientProvider.overrideWithValue(client)],
        );
        addTearDown(c.dispose);
        c.read(sessionSynchronizerProvider);
        final n = c.read(transcriptsProvider.notifier);

        n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
        n.debugOnEvent(seqEv('assistant_message_chunk', 2, text: 'hi'));
        expect(n.debugLastSeq('A'), 2);

        await c.read(sessionSynchronizerProvider.notifier).resync();

        expect(
          client.historyCalls,
          0,
          reason:
              'lastSeq == latest_seq: the 3-second app switch must cost '
              'the list calls and nothing else',
        );
      },
    );

    test('behind the ring: differing latest_seq still fetches', () async {
      final client = _HistoryClient(
        {
          'A': [
            seqEv('user_message', 1, text: 'hello'),
            seqEv('assistant_message_chunk', 2, text: 'hi'),
            seqEv('assistant_message_chunk', 3, text: ' missed'),
          ],
        },
        epoch: 'e1',
        seqs: {'A': const SeqBounds(first: 1, latest: 3)},
      );
      final c = ProviderContainer(
        overrides: [mcremoteClientProvider.overrideWithValue(client)],
      );
      addTearDown(c.dispose);
      c.read(sessionSynchronizerProvider);
      final n = c.read(transcriptsProvider.notifier);

      n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
      n.debugOnEvent(seqEv('assistant_message_chunk', 2, text: 'hi'));

      await c.read(sessionSynchronizerProvider.notifier).resync();

      expect(client.historyCalls, greaterThan(0));
      final text = c
          .read(transcriptsProvider)
          .forSession('A')
          .items
          .where((i) => i.kind == ChatItemKind.assistant)
          .map((i) => i.text ?? '')
          .join();
      expect(text.contains('missed'), isTrue);
    });

    test(
      'epoch change forces the walk even when seqs look up to date',
      () async {
        final client = _HistoryClient(
          {
            'A': [seqEv('user_message', 1, text: 'hello')],
          },
          epoch: 'e1',
          seqs: {'A': const SeqBounds(first: 1, latest: 2)},
        );
        final c = ProviderContainer(
          overrides: [mcremoteClientProvider.overrideWithValue(client)],
        );
        addTearDown(c.dispose);
        c.read(sessionSynchronizerProvider);
        final n = c.read(transcriptsProvider.notifier);

        n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
        n.debugOnEvent(seqEv('assistant_message_chunk', 2, text: 'hi'));

        final sync = c.read(sessionSynchronizerProvider.notifier);
        await sync.resync();
        expect(client.historyCalls, 0, reason: 'baseline: up to date under e1');

        // Daemon restarted uncleanly: new epoch, same-looking seqs — which
        // are now a different lineage and cannot be trusted.
        client.epoch = 'e2';
        await sync.resync();
        expect(
          client.historyCalls,
          greaterThan(0),
          reason: 'an epoch change invalidates every cached seq',
        );
      },
    );

    test('resume seq-covered path: still lists for status, skips history '
        '(MADR 0068 D4 + 0072 §6.2 A)', () async {
      final client = _HistoryClient(
        {'A': []},
        epoch: 'e1',
        seqs: {'A': const SeqBounds(first: 1, latest: 2)},
        metaStatus: 'idle',
      );
      client.resumedOverride = {'A': const SeqBounds(first: 1, latest: 2)};
      final c = ProviderContainer(
        overrides: [mcremoteClientProvider.overrideWithValue(client)],
      );
      addTearDown(c.dispose);
      c.read(sessionSynchronizerProvider);
      final n = c.read(transcriptsProvider.notifier);

      n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
      n.debugOnEvent(seqEv('assistant_message_chunk', 2, text: 'hi'));
      // Sticky busy after a missed turn_complete while seq still matches.
      n.debugOnEvent(
        SessionEvent(
          type: 'session_status',
          sessionId: 'A',
          seq: 2,
          status: 'running',
        ),
      );
      expect(c.read(transcriptsProvider).forSession('A').status, 'running');

      await c.read(sessionSynchronizerProvider.notifier).resync();

      expect(
        client.listAttemptsCount,
        greaterThan(0),
        reason: '0072 A: status reconcile still needs one list snapshot',
      );
      expect(
        client.historyCalls,
        0,
        reason: 'seq covered — no history workers',
      );
      expect(
        c.read(transcriptsProvider).forSession('A').status,
        'idle',
        reason: 'host idle must clear sticky running on resume',
      );
    });

    test(
      'resume fast path misses fall through to the ordinary reconcile',
      () async {
        final client = _HistoryClient(
          {'A': []},
          epoch: 'e1',
          seqs: {'A': const SeqBounds(first: 1, latest: 2)},
        );
        // Daemon confirmed A at latest 5, but local lastSeq is 2 — changed.
        client.resumedOverride = {'A': const SeqBounds(first: 1, latest: 5)};
        final c = ProviderContainer(
          overrides: [mcremoteClientProvider.overrideWithValue(client)],
        );
        addTearDown(c.dispose);
        c.read(sessionSynchronizerProvider);
        final n = c.read(transcriptsProvider.notifier);

        n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
        n.debugOnEvent(seqEv('assistant_message_chunk', 2, text: 'hi'));

        await c.read(sessionSynchronizerProvider.notifier).resync();

        expect(
          client.listAttemptsCount,
          greaterThan(0),
          reason: 'a moved seq must run the truth path',
        );
      },
    );

    test('failed resync re-arms itself while still connected', () async {
      SessionSynchronizer.retryDelay = const Duration(milliseconds: 50);
      addTearDown(
        () => SessionSynchronizer.retryDelay = const Duration(seconds: 5),
      );
      final client = _FlakyClient(
        {
          'A': [
            seqEv('user_message', 1, text: 'hello'),
            seqEv('assistant_message_chunk', 2, text: ' healed'),
          ],
        },
        failures: 1, // one list per pass (0070 F1); first pass fails once
      );
      final c = ProviderContainer(
        overrides: [mcremoteClientProvider.overrideWithValue(client)],
      );
      addTearDown(c.dispose);
      c.read(sessionSynchronizerProvider);
      final n = c.read(transcriptsProvider.notifier);
      n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
      n.debugMarkAllGapsSuspected();

      await c.read(sessionSynchronizerProvider.notifier).resync();
      expect(client.listAttempts, 1, reason: 'first pass: single list failed');

      // The re-arm fires after retryDelay and re-drives the full pass —
      // including the list reconcile the first pass never got — without a
      // new connect edge.
      await Future<void>.delayed(const Duration(milliseconds: 300));
      expect(
        client.listAttempts,
        greaterThanOrEqualTo(2),
        reason: 'A1 finding 29: an interrupted resync must re-drive itself',
      );
    });

    test(
      'empty host ring skips history even under epoch force (0071 F6)',
      () async {
        final client = _HistoryClient(
          {'A': []},
          epoch: 'e1',
          seqs: {'A': const SeqBounds(first: 0, latest: 0)},
        );
        final c = ProviderContainer(
          overrides: [mcremoteClientProvider.overrideWithValue(client)],
        );
        addTearDown(c.dispose);
        c.read(sessionSynchronizerProvider);
        final sync = c.read(sessionSynchronizerProvider.notifier);

        await sync.resync();
        expect(client.historyCalls, 0, reason: 'baseline empty ring');

        // Epoch change forces the walk, but latest==0 still has nothing to pull.
        client.epoch = 'e2';
        client.historyCalls = 0;
        await sync.resync();
        expect(
          client.historyCalls,
          0,
          reason: 'force must not fetch empty host history',
        );
      },
    );

    test('one list snapshot per truth-path pass (0070 F1)', () async {
      final client = _HistoryClient(
        {
          'A': [
            seqEv('user_message', 1, text: 'hello'),
            seqEv('assistant_message_chunk', 2, text: 'hi'),
          ],
          // Host also lists H1; previously a second list call merged it in.
          'H1': [seqEv('user_message', 1, text: 'other', sid: 'H1')],
        },
        epoch: 'e1',
        seqs: {
          'A': const SeqBounds(first: 1, latest: 2),
          'H1': const SeqBounds(first: 1, latest: 1),
        },
      );
      final c = ProviderContainer(
        overrides: [mcremoteClientProvider.overrideWithValue(client)],
      );
      addTearDown(c.dispose);
      c.read(sessionSynchronizerProvider);
      final n = c.read(transcriptsProvider.notifier);
      n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
      n.debugOnEvent(seqEv('assistant_message_chunk', 2, text: 'hi'));

      await c.read(sessionSynchronizerProvider.notifier).resync();

      expect(
        client.listAttemptsCount,
        1,
        reason: 'one snapshot per pass — no second list',
      );
    });
  });
}

/// Fails the first [failures] list calls, then behaves like _HistoryClient —
/// the shape of a resync interrupted by a re-background (A1 finding 29).
class _FlakyClient extends _HistoryClient {
  _FlakyClient(super.historyBySession, {required this.failures});

  int failures;
  int listAttempts = 0;

  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async {
    listAttempts++;
    if (failures > 0) {
      failures--;
      throw Exception('transient list failure');
    }
    return super.listSessionSnapshot();
  }
}

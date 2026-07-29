import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/transcript_cache.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';
import 'package:shared_preferences/shared_preferences.dart';

SessionEvent _ev(String type, {String? text, String? status}) =>
    SessionEvent(type: type, sessionId: 's1', text: text, status: status);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  ProviderContainer makeContainer() {
    final c = ProviderContainer();
    addTearDown(c.dispose);
    return c;
  }

  group('history replay', () {
    test('feeds events into an empty transcript in order', () async {
      final c = makeContainer();
      await c.read(transcriptsProvider.notifier).replayHistory('s1', [
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

    test(
      'is skipped when the transcript is already populated (race guard)',
      () async {
        final c = makeContainer();
        final notifier = c.read(transcriptsProvider.notifier);
        // A live event won the race and populated the transcript first.
        await notifier.replayHistory('s1', [_ev('user_message', text: 'live')]);
        final before = c.read(transcriptsProvider).forSession('s1');
        expect(before.items, hasLength(1));

        // History arrives late: because the transcript is no longer empty it must
        // be dropped wholesale rather than double-applied.
        await notifier.replayHistory('s1', [
          _ev('user_message', text: 'stale-a'),
          _ev('user_message', text: 'stale-b'),
        ]);
        final after = c.read(transcriptsProvider).forSession('s1');
        expect(after.items, hasLength(1));
        expect(after.items.single.text, 'live');
      },
    );

    test('empty history list is a no-op', () async {
      final c = makeContainer();
      await c.read(transcriptsProvider.notifier).replayHistory('s1', const []);
      expect(c.read(transcriptsProvider).peek('s1'), isNull);
    });

    test('large history applies completely across batches', () async {
      final c = makeContainer();
      final events = <SessionEvent>[
        for (var i = 0; i < kHistoryApplyBatchSize * 3 + 5; i++)
          _ev('user_message', text: 'm$i'),
      ];
      await c.read(transcriptsProvider.notifier).replayHistory('s1', events);
      final t = c.read(transcriptsProvider).forSession('s1');
      expect(t.items, hasLength(events.length));
      expect(t.items.first.text, 'm0');
      expect(t.items.last.text, 'm${events.length - 1}');
    });
  });

  group('chunked hydrate integrity', () {
    SessionEvent seqEv(String type, int seq, {String? text}) =>
        SessionEvent(type: type, sessionId: 's1', seq: seq, text: text);

    List<SessionEvent> bigHistory(int n) => <SessionEvent>[
      for (var i = 0; i < n; i++) seqEv('user_message', i + 1, text: 'm$i'),
    ];

    test(
      'batch commits publish stable snapshots, never mutated in place',
      () async {
        final c = makeContainer();
        final n = c.read(transcriptsProvider.notifier);
        final events = bigHistory(kHistoryApplyBatchSize * 2 + 5);
        // Record every published items list; the UI's select/memo checks rely
        // on earlier snapshots keeping their identity AND contents.
        final published = <List<ChatItem>>[];
        c.listen<TranscriptsState>(transcriptsProvider, (_, next) {
          final t = next.peek('s1');
          if (t != null) published.add(t.items);
        });
        await n.replayHistory('s1', events);
        expect(published.length, greaterThanOrEqualTo(3));
        // The first published snapshot must still hold exactly its batch: a
        // later batch appending into the same list is the MADR 0018 D2
        // violation that froze the pane at the first batch.
        expect(published.first, hasLength(kHistoryApplyBatchSize));
        expect(published.last, hasLength(events.length));
        expect(identical(published.first, published.last), isFalse);
      },
    );

    test('resync rebuild never rewinds the visible transcript', () async {
      final c = makeContainer();
      final n = c.read(transcriptsProvider.notifier);
      // A populated chat the user is looking at.
      for (var i = 0; i < 10; i++) {
        n.debugOnEvent(seqEv('user_message', i + 1, text: 'seen$i'));
      }
      final events = bigHistory(kHistoryApplyBatchSize * 2 + 5);
      final published = <int>[];
      c.listen<TranscriptsState>(transcriptsProvider, (_, next) {
        final t = next.peek('s1');
        if (t != null) published.add(t.items.length);
      });
      await n.resyncHistory('s1', events);
      // No intermediate commit may show fewer items than the user already
      // had (the oldest-40 rewind flash); the rebuilt transcript lands whole.
      expect(published, isNotEmpty);
      expect(published.every((len) => len >= 10), isTrue);
      expect(published.last, events.length);
    });

    test('live event arriving mid-replay survives and is not lost', () async {
      final c = makeContainer();
      final n = c.read(transcriptsProvider.notifier);
      final events = bigHistory(kHistoryApplyBatchSize * 2 + 5);
      final replay = n.replayHistory('s1', events);
      // Land inside a batch-boundary yield.
      await Future<void>.delayed(Duration.zero);
      n.debugOnEvent(seqEv('user_message', events.length + 1, text: 'live'));
      await replay;
      final t = c.read(transcriptsProvider).forSession('s1');
      expect(t.items, hasLength(events.length + 1));
      expect(t.items.last.text, 'live');
      // Seq bookkeeping stayed truthful: a follow-up fetch containing the
      // same record gates as a no-op instead of rebuilding (or being needed
      // to repair a loss).
      final before = c.read(transcriptsProvider).forSession('s1');
      await n.resyncHistory('s1', [
        ...events,
        seqEv('user_message', events.length + 1, text: 'live'),
      ]);
      expect(
        identical(c.read(transcriptsProvider).forSession('s1'), before),
        isTrue,
      );
    });

    test('live rebroadcast of a fetched event is not double-applied', () async {
      final c = makeContainer();
      final n = c.read(transcriptsProvider.notifier);
      final events = bigHistory(kHistoryApplyBatchSize * 2 + 5);
      final replay = n.replayHistory('s1', events);
      await Future<void>.delayed(Duration.zero);
      // The newest fetched event also arrives on the live stream mid-apply.
      n.debugOnEvent(
        seqEv('user_message', events.length, text: 'm${events.length - 1}'),
      );
      await replay;
      final t = c.read(transcriptsProvider).forSession('s1');
      expect(t.items, hasLength(events.length));
    });

    test('live event arriving mid-resync rebuild survives', () async {
      final c = makeContainer();
      final n = c.read(transcriptsProvider.notifier);
      // Populated transcript that missed an outage gap.
      n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
      final events = bigHistory(kHistoryApplyBatchSize * 2 + 5);
      final resync = n.resyncHistory('s1', events);
      await Future<void>.delayed(Duration.zero);
      n.debugOnEvent(seqEv('user_message', events.length + 1, text: 'live'));
      await resync;
      final t = c.read(transcriptsProvider).forSession('s1');
      expect(t.items, hasLength(events.length + 1));
      expect(t.items.last.text, 'live');
    });
  });

  group('cache interplay', () {
    SessionEvent seqEv(String type, int seq, {String? text, String? status}) =>
        SessionEvent(
          type: type,
          sessionId: 's1',
          seq: seq,
          text: text,
          status: status,
        );

    setUp(() {
      SharedPreferences.setMockInitialValues({});
    });

    test('dispose persists a pending debounced save immediately', () async {
      final cache = TranscriptCache();
      final c = ProviderContainer();
      final n = c.read(transcriptsProvider.notifier);
      n.debugCache = cache;
      n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
      // The 400ms debounced save has not fired; dispose must flush it to the
      // cache without leaving a timer to fire against the disposed notifier.
      c.dispose();
      // Cache encoding runs in a background isolate. It is intentionally
      // best-effort and unawaited on notifier disposal, so wait for its
      // serialized queue rather than assuming one microtask is enough.
      await cache.debugWhenIdle;
      final saved = await cache.load('s1');
      expect(saved, isNotNull);
      expect(saved!.items.single.text, 'hello');
    });

    test('syncFromMeta evicts dead sessions from the cache too', () async {
      final cache = TranscriptCache();
      final c = makeContainer();
      final n = c.read(transcriptsProvider.notifier);
      n.debugCache = cache;
      n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
      await cache.save('s1', c.read(transcriptsProvider).forSession('s1'));
      expect(await cache.load('s1'), isNotNull);
      // Host no longer reports s1: its snapshot must not linger in prefs.
      n.syncFromMeta(const []);
      await Future<void>.delayed(Duration.zero);
      expect(await cache.load('s1'), isNull);
    });

    test(
      'hydrate keeps live non-item state that raced the cache load',
      () async {
        final cache = TranscriptCache();
        await cache.save(
          's1',
          SessionTranscript(
            sessionId: 's1',
            items: [ChatItem.user('old').copyWith(seq: 1)],
            nextSeq: 2,
          ),
        );
        final c = makeContainer();
        final n = c.read(transcriptsProvider.notifier);
        n.debugCache = cache;
        // Live status lands before the hydrate finishes.
        n.debugOnEvent(seqEv('session_status', 2, status: 'running'));
        expect(await n.hydrateFromCache('s1'), isTrue);
        final t = c.read(transcriptsProvider).forSession('s1');
        expect(t.items.single.text, 'old');
        expect(t.status, 'running');
      },
    );

    // The cache stores no mode list, and session_mode arrives at session create
    // — before any chat item — so a snapshot landing afterwards used to wipe the
    // mode strip (and with it /plan) for the rest of the session.
    test('hydrate keeps a live mode list the cache cannot carry', () async {
      final cache = TranscriptCache();
      await cache.save(
        's1',
        SessionTranscript(
          sessionId: 's1',
          items: [ChatItem.user('old').copyWith(seq: 1)],
          nextSeq: 2,
        ),
      );
      final c = makeContainer();
      final n = c.read(transcriptsProvider.notifier);
      n.debugCache = cache;
      n.debugOnEvent(
        SessionEvent(
          type: 'session_mode',
          sessionId: 's1',
          seq: 2,
          modes: const [
            SessionMode(id: 'default', name: 'Build'),
            SessionMode(id: 'plan', name: 'Plan'),
          ],
          currentModeId: 'plan',
        ),
      );
      expect(await n.hydrateFromCache('s1'), isTrue);
      final t = c.read(transcriptsProvider).forSession('s1');
      expect(t.items.single.text, 'old');
      expect(t.modes.map((m) => m.id), ['default', 'plan']);
      expect(t.currentModeId, 'plan');
    });

    test('hydrate keeps a usage report that raced the cache load', () async {
      final cache = TranscriptCache();
      await cache.save(
        's1',
        SessionTranscript(
          sessionId: 's1',
          items: [ChatItem.user('old').copyWith(seq: 1)],
          nextSeq: 2,
        ),
      );
      final c = makeContainer();
      final n = c.read(transcriptsProvider.notifier);
      n.debugCache = cache;
      n.debugOnEvent(
        SessionEvent(
          type: 'usage_update',
          sessionId: 's1',
          seq: 2,
          usage: Usage(used: 4200, size: 200000),
        ),
      );
      expect(await n.hydrateFromCache('s1'), isTrue);
      // ACP agents may not report again until the next turn, so clobbering
      // this blanks the context-window indicator until then (MADR 0046 L-6).
      final t = c.read(transcriptsProvider).forSession('s1');
      expect(t.usage?.used, 4200);
    });

    // Same exposure as modes: remote_commands arrives at session create, before
    // any chat item, and the cache stores none of it. A snapshot landing
    // afterwards must not wipe the composer's command list (MADR 0023).
    test(
      'hydrate keeps the daemon command list the cache cannot carry',
      () async {
        final cache = TranscriptCache();
        await cache.save(
          's1',
          SessionTranscript(
            sessionId: 's1',
            items: [ChatItem.user('old').copyWith(seq: 1)],
            nextSeq: 2,
          ),
        );
        final c = makeContainer();
        final n = c.read(transcriptsProvider.notifier);
        n.debugCache = cache;
        n.debugOnEvent(
          SessionEvent(
            type: 'remote_commands',
            sessionId: 's1',
            seq: 2,
            remoteCommands: [
              RemoteCommand(name: 'help', available: true),
              RemoteCommand(
                name: 'compact',
                available: false,
                reason: 'not here',
              ),
            ],
          ),
        );
        expect(await n.hydrateFromCache('s1'), isTrue);
        final t = c.read(transcriptsProvider).forSession('s1');
        expect(t.items.single.text, 'old');
        expect(t.remoteCommands.map((cmd) => cmd.name), ['help', 'compact']);
        expect(t.remoteCommands.first.available, isTrue);
      },
    );
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

    test('populated transcript gains events missed during an outage', () async {
      final c = makeContainer();
      final n = c.read(transcriptsProvider.notifier);
      // Live events seen before the outage.
      n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
      n.debugOnEvent(seqEv('assistant_message_chunk', 2, text: 'hi'));
      // Outage: seq 3-4 broadcast while the socket was down. The refetched
      // ring contains everything; resync must rebuild rather than drop it.
      await n.resyncHistory('s1', [
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

    test('resync is a no-op when nothing was missed', () async {
      final c = makeContainer();
      final n = c.read(transcriptsProvider.notifier);
      n.debugOnEvent(seqEv('user_message', 1, text: 'hello'));
      n.debugOnEvent(seqEv('assistant_message_chunk', 2, text: 'hi'));
      final before = c.read(transcriptsProvider).forSession('s1');
      await n.resyncHistory('s1', [
        seqEv('user_message', 1, text: 'hello'),
        seqEv('assistant_message_chunk', 2, text: 'hi'),
      ]);
      final after = c.read(transcriptsProvider).forSession('s1');
      expect(identical(after, before), isTrue);
    });

    test(
      'resync is a no-op after a chunked, usage-interleaved batch',
      () async {
        final c = makeContainer();
        final n = c.read(transcriptsProvider.notifier);
        // The ordinary OpenCode cadence, delivered as one coalesced batch. This
        // goes through the fold, which the per-event test above never exercises
        // — and the fold used to invent a gap here, so the resync rebuilt a
        // transcript that had missed nothing (MADR 0046 M-5).
        n.debugOnEventBatch([
          seqEv('user_message', 1, text: 'hello'),
          seqEv('assistant_message_chunk', 2, text: 'hi'),
          SessionEvent(
            type: 'usage_update',
            sessionId: 's1',
            seq: 3,
            usage: Usage(used: 10, size: 1000),
          ),
          seqEv('assistant_message_chunk', 4, text: ' there'),
        ]);
        final before = c.read(transcriptsProvider).forSession('s1');
        expect(n.debugGapSuspected('s1'), isFalse);

        await n.resyncHistory('s1', [
          seqEv('user_message', 1, text: 'hello'),
          seqEv('assistant_message_chunk', 2, text: 'hi'),
          seqEv('assistant_message_chunk', 4, text: ' there'),
        ]);
        final after = c.read(transcriptsProvider).forSession('s1');
        expect(identical(after, before), isTrue);
      },
    );
  });
}

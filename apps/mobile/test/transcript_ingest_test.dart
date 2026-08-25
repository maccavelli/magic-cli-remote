import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/transcript_reducer.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

import 'support/fake_path_provider.dart';

/// Ingest-side cost bounds, the counterpart to the render-side guarantees
/// asserted through `debugMarkdownParseCount` in streaming_markdown_test.dart.
///
/// What matters here is that the work of applying a batch scales with flush
/// windows, not with streamed tokens: `_appendChunk` copies the whole
/// accumulated reply every time it runs, so one call per token is O(N·k)
/// (MADR 0018 C1, closed by MADR 0024 phase 5).
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  // Transcript entries are files (MADR 0084 D3): keep the cache off the
  // real platform channel and out of real user directories.
  setUp(() => useFakePathProvider(addTearDown));

  SessionEvent chunk(String text, {int seq = 0}) => SessionEvent(
    type: 'assistant_message_chunk',
    sessionId: 's1',
    text: text,
    seq: seq,
  );

  ProviderContainer makeContainer() {
    final c = ProviderContainer();
    addTearDown(c.dispose);
    return c;
  }

  /// Open the assistant bubble so later chunks merge into it — the first chunk
  /// of a reply appends a new item and never calls `_appendChunk`.
  TranscriptsNotifier seeded(ProviderContainer c) {
    final n = c.read(transcriptsProvider.notifier);
    n.debugOnEvent(chunk('open'));
    debugAppendChunkCount = 0;
    return n;
  }

  test('a window of chunks costs one append, whatever its size', () {
    final c = makeContainer();
    final n = seeded(c);

    n.debugOnEventBatch([
      for (var i = 0; i < 200; i++) chunk('t$i ', seq: i + 2),
    ]);

    expect(
      debugAppendChunkCount,
      1,
      reason: '200 chunks in one window must fold to a single string append',
    );
    final t = c.read(transcriptsProvider).forSession('s1');
    expect(
      t.items.single.text,
      'open${[for (var i = 0; i < 200; i++) 't$i '].join()}',
    );
  });

  test('a tool update between two runs keeps them apart and in order', () {
    final c = makeContainer();
    final n = seeded(c);

    n.debugOnEventBatch([
      chunk('a', seq: 2),
      chunk('b', seq: 3),
      SessionEvent(
        type: 'tool_call_update',
        sessionId: 's1',
        toolId: 'tool-1',
        toolName: 'bash',
        status: 'running',
        seq: 4,
      ),
      chunk('c', seq: 5),
      chunk('d', seq: 6),
    ]);

    // 'a'+'b' fold into the already-open bubble as one append; the tool card
    // ends that bubble, so 'c'+'d' open a fresh item and cost no append at all.
    // Unfolded this batch would have cost four.
    expect(debugAppendChunkCount, 1, reason: 'at most one append per text run');
    final items = c.read(transcriptsProvider).forSession('s1').items;
    expect(items.map((i) => i.kind).toList(), [
      ChatItemKind.assistant,
      ChatItemKind.tool,
      ChatItemKind.assistant,
    ]);
    expect(items.first.text, 'openab');
    expect(items.last.text, 'cd');
  });

  test('interleaved usage updates do not fragment the text run', () {
    final c = makeContainer();
    final n = seeded(c);

    final events = <SessionEvent>[];
    for (var i = 0; i < 20; i++) {
      events.add(chunk('x', seq: i * 2 + 2));
      events.add(
        SessionEvent(
          type: 'usage_update',
          sessionId: 's1',
          usage: Usage(used: 100 + i, size: 1000),
          seq: i * 2 + 3,
        ),
      );
    }
    n.debugOnEventBatch(events);

    expect(
      debugAppendChunkCount,
      1,
      reason: 'usage_update appends no item, so it must not split the run',
    );
    final t = c.read(transcriptsProvider).forSession('s1');
    expect(t.items.single.text, 'open${'x' * 20}');
    expect(t.usage?.used, 119, reason: 'the last usage report still wins');
    // Nothing was missed: every seq from 2 to 41 arrived. Folding emits the
    // merged run *after* the usage events that arrived between its chunks, so
    // judging continuity at fold time invented a gap here — and a suspected
    // gap makes the next resync rebuild the transcript from the daemon ring,
    // truncating older local items and dropping staged thumbnails
    // (MADR 0046 M-5).
    expect(n.debugGapSuspected('s1'), isFalse);
  });

  test('an absent session reads back as the same empty transcript', () {
    final c = makeContainer();
    final n = seeded(c);

    // A chat screen open on a brand-new (or history-less) session selects a
    // transcript that has nothing in it. SessionTranscript has no value
    // equality, so materialising a fresh one per lookup made every commit of
    // *any* session look like a change to that screen — a rebuild per frame
    // of someone else's streaming reply (MADR 0046 L-7).
    final first = c.read(transcriptsProvider).forSession('other');
    n.debugOnEvent(chunk('x', seq: 100));
    final second = c.read(transcriptsProvider).forSession('other');
    expect(identical(first, second), isTrue);

    // Once it has real content the shared empty is gone, not shadowing it.
    n.debugOnEvent(
      SessionEvent(
        type: 'user_message',
        sessionId: 'other',
        seq: 1,
        text: 'hi',
      ),
    );
    final populated = c.read(transcriptsProvider).forSession('other');
    expect(identical(populated, first), isFalse);
    expect(populated.items, hasLength(1));
  });

  test('a real gap in a folded batch is still detected', () {
    final c = makeContainer();
    final n = seeded(c);

    // seq 3 and 4 never arrived.
    n.debugOnEventBatch([
      chunk('a', seq: 2),
      SessionEvent(
        type: 'usage_update',
        sessionId: 's1',
        usage: Usage(used: 1, size: 1000),
        seq: 5,
      ),
      chunk('b', seq: 6),
    ]);

    expect(n.debugGapSuspected('s1'), isTrue);
  });

  test('thought and assistant chunks never merge into each other', () {
    final c = makeContainer();
    final n = c.read(transcriptsProvider.notifier);

    n.debugOnEventBatch([
      SessionEvent(
        type: 'thought_chunk',
        sessionId: 's1',
        text: 'think ',
        seq: 1,
      ),
      SessionEvent(
        type: 'thought_chunk',
        sessionId: 's1',
        text: 'hard',
        seq: 2,
      ),
      chunk('reply ', seq: 3),
      chunk('now', seq: 4),
    ]);

    final items = c.read(transcriptsProvider).forSession('s1').items;
    expect(items.map((i) => i.kind).toList(), [
      ChatItemKind.thought,
      ChatItemKind.assistant,
    ]);
    expect(items[0].text, 'think hard');
    expect(items[1].text, 'reply now');
  });

  test('folding still records the seq window of every event it merged', () {
    final c = makeContainer();
    final n = c.read(transcriptsProvider.notifier);

    // Folding carries the newest seq on the merged event; the older ones have
    // to be noted too or the reconnect resync loses its lower bound.
    n.debugOnEventBatch([
      chunk('a', seq: 10),
      chunk('b', seq: 11),
      chunk('c', seq: 12),
    ]);

    expect(n.debugLastSeq('s1'), 12);
    expect(n.debugFirstSeq('s1'), 10);
  });

  test('usage_update no longer forces its own commit', () {
    final c = makeContainer();
    var commits = 0;
    c.listen<TranscriptsState>(transcriptsProvider, (_, _) => commits++);

    final n = c.read(transcriptsProvider.notifier);
    n.debugOnEventBatch([
      chunk('a', seq: 1),
      for (var i = 0; i < 10; i++)
        SessionEvent(
          type: 'usage_update',
          sessionId: 's1',
          usage: Usage(used: i, size: 1000),
          seq: i + 2,
        ),
      chunk('b', seq: 12),
    ]);

    expect(
      commits,
      1,
      reason: 'the whole window must publish once, not once per usage report',
    );
  });

  // ── Tool ingest (MADR 0042 D2) ──────────────────────────────────────────
  //
  // `tool_call` used to bypass the batch window, so OpenCode's parallel tool
  // fan-out cost one synchronous commit per tool. These bound the tool path the
  // way the tests above bound the text path.

  SessionEvent toolCall(
    String id, {
    required int seq,
    String kind = 'read',
    String status = 'pending',
  }) => SessionEvent(
    type: 'tool_call',
    sessionId: 's1',
    seq: seq,
    toolId: id,
    toolName: id,
    toolKind: kind,
    status: status,
  );

  SessionEvent toolUpdate(
    String id, {
    required int seq,
    String status = 'running',
    String text = '',
  }) => SessionEvent(
    type: 'tool_call_update',
    sessionId: 's1',
    seq: seq,
    toolId: id,
    status: status,
    text: text,
  );

  test('a parallel tool fan-out publishes once, not once per tool', () {
    final c = makeContainer();
    var commits = 0;
    c.listen<TranscriptsState>(transcriptsProvider, (_, _) => commits++);

    final n = c.read(transcriptsProvider.notifier);
    var seq = 0;
    n.debugOnEventBatch([
      for (var i = 0; i < 5; i++) toolCall('p$i', seq: ++seq),
      for (var i = 0; i < 5; i++) toolUpdate('p$i', seq: ++seq),
    ]);

    expect(
      commits,
      1,
      reason:
          'five parallel tool calls in one window are one publish; '
          'before MADR 0042 D2 this was six',
    );
    expect(c.read(transcriptsProvider).forSession('s1').items, hasLength(5));
  });

  test('consecutive updates for one tool collapse to the latest state', () {
    final c = makeContainer();
    final n = c.read(transcriptsProvider.notifier);

    var seq = 0;
    n.debugOnEventBatch([
      toolCall('t1', seq: ++seq, kind: 'execute'),
      for (var i = 0; i < 10; i++)
        toolUpdate('t1', seq: ++seq, text: 'line $i'),
    ]);

    final item = c.read(transcriptsProvider).forSession('s1').items.single;
    expect(item.text, 'line 9', reason: 'the last update wins');
    expect(item.toolStatus, 'running');
  });

  test('a terminal status is never folded away', () {
    final c = makeContainer();
    final n = c.read(transcriptsProvider.notifier);

    var seq = 0;
    n.debugOnEventBatch([
      toolCall('t1', seq: ++seq, kind: 'execute'),
      toolUpdate('t1', seq: ++seq, text: 'building'),
      toolUpdate('t1', seq: ++seq, text: 'still building'),
      toolUpdate('t1', seq: ++seq, status: 'completed', text: 'done'),
    ]);

    final item = c.read(transcriptsProvider).forSession('s1').items.single;
    expect(
      item.toolStatus,
      'completed',
      reason: 'keeping the LAST of a run is what guarantees this',
    );
    expect(item.text, 'done');
  });

  test('the fold never crosses a tool_call for the same id', () {
    final c = makeContainer();
    final n = c.read(transcriptsProvider.notifier);

    var seq = 0;
    n.debugOnEventBatch([
      toolUpdate('t1', seq: ++seq, text: 'first'),
      toolCall('t1', seq: ++seq),
      toolUpdate('t1', seq: ++seq, text: 'second'),
    ]);

    // The opening call is not an update, so it flushes the pending one rather
    // than being folded through.
    final items = c.read(transcriptsProvider).forSession('s1').items;
    expect(items, hasLength(1));
    expect(items.single.text, 'second');
  });

  test('updates with no tool id are never folded together', () {
    final c = makeContainer();
    final n = c.read(transcriptsProvider.notifier);

    // An id-less update folds into the most recent tool card by arrival order
    // (`transcript_reducer._upsertTool`). Folding two of them would apply both
    // to the same card and leave the earlier one's card untouched.
    SessionEvent anonUpdate(String text, int seq) => SessionEvent(
      type: 'tool_call_update',
      sessionId: 's1',
      seq: seq,
      status: 'completed',
      text: text,
    );

    var seq = 0;
    n.debugOnEventBatch([
      toolCall('a', seq: ++seq),
      anonUpdate('output of a', ++seq),
      toolCall('b', seq: ++seq),
      anonUpdate('output of b', ++seq),
    ]);

    final items = c.read(transcriptsProvider).forSession('s1').items;
    expect(items, hasLength(2));
    expect(items[0].text, 'output of a');
    expect(items[1].text, 'output of b');
  });

  test(
    'a discrete event still flushes pending tool calls before it applies',
    () {
      final c = makeContainer();
      final n = c.read(transcriptsProvider.notifier);

      // permission_request is not batchable, so it must observe the tool cards
      // staged ahead of it rather than landing before them.
      n.debugOnEvent(toolCall('t1', seq: 1));
      n.debugOnEvent(
        SessionEvent(
          type: 'permission_request',
          sessionId: 's1',
          seq: 2,
          permissionId: 'perm-1',
          toolName: 'bash',
        ),
      );

      final t = c.read(transcriptsProvider).forSession('s1');
      expect(t.items.first.kind, ChatItemKind.tool);
      expect(t.hasPendingPermission, isTrue);
    },
  );

  test('events for a cleared session are dropped (MADR 0094 D8)', () {
    final c = makeContainer();
    final n = c.read(transcriptsProvider.notifier);

    SessionEvent ask(String permId) => SessionEvent(
      type: 'permission_request',
      sessionId: 'sess-clear',
      permissionId: permId,
      toolName: 'command',
      text: 'ls',
      options: [
        PermissionOption(
          optionId: 'accept',
          name: 'Allow once',
          kind: 'allow_once',
        ),
      ],
    );

    // Baseline sanity: an ask for a live session creates its transcript.
    n.debugOnEvent(ask('perm-live'));
    expect(c.read(transcriptsProvider).byId.containsKey('sess-clear'), isTrue);

    n.clearSession('sess-clear');
    expect(c.read(transcriptsProvider).byId.containsKey('sess-clear'), isFalse);

    // The ghost: an ask that lands after the delete must not resurrect
    // the cleared transcript (no pendingPermissions, no sheet).
    n.debugOnEvent(ask('perm-post-delete'));
    expect(
      c.read(transcriptsProvider).byId.containsKey('sess-clear'),
      isFalse,
      reason: 'a post-delete event must not resurrect a cleared session',
    );
  });

  test(
    'syncFromMeta and clearAll lift tombstones for a re-created id (MADR 0094 D8)',
    () {
      final c = makeContainer();
      final n = c.read(transcriptsProvider.notifier);

      SessionEvent msg(String id, int seq, String text) => SessionEvent(
        type: 'user_message',
        sessionId: id,
        seq: seq,
        text: text,
      );

      // Tombstone: a cleared id drops events.
      n.clearSession('sess-re');
      n.debugOnEvent(msg('sess-re', 1, 'dropped'));
      expect(c.read(transcriptsProvider).byId.containsKey('sess-re'), isFalse);

      // The host lists the id again (re-create / resume): the tombstone is
      // lifted and events flow — mirrors the daemon clearing `purged` on
      // Create (0093 D3).
      n.syncFromMeta([
        SessionMeta(id: 'sess-re', provider: 'kilo', name: 'Re'),
      ]);
      n.debugOnEvent(msg('sess-re', 1, 'hi'));
      expect(c.read(transcriptsProvider).byId.containsKey('sess-re'), isTrue);

      // clearAll (sign-out) also lifts tombstones.
      n.clearSession('sess-re');
      n.debugOnEvent(msg('sess-re', 2, 'dropped-again'));
      expect(c.read(transcriptsProvider).byId.containsKey('sess-re'), isFalse);
      n.clearAll();
      n.debugOnEvent(msg('sess-re', 1, 'hi-again'));
      expect(c.read(transcriptsProvider).byId.containsKey('sess-re'), isTrue);
    },
  );

  test('the cleared-session tombstone set is bounded (MADR 0095 F7)', () {
    final c = makeContainer();
    final n = c.read(transcriptsProvider.notifier);
    for (var i = 0; i < kMaxClearedSessions + 64; i++) {
      n.clearSession('sess-$i');
    }
    expect(n.debugClearedCount, lessThanOrEqualTo(kMaxClearedSessions));
    // The most recent tombstones — the only ones whose ghost window is
    // still open — must survive the trim.
    expect(n.debugIsCleared('sess-${kMaxClearedSessions + 63}'), isTrue);
    // And the tombstone still does its job.
    n.debugOnEvent(
      SessionEvent(
        type: 'user_message',
        sessionId: 'sess-${kMaxClearedSessions + 63}',
        seq: 1,
        text: 'ghost',
      ),
    );
    expect(
      c
          .read(transcriptsProvider)
          .byId
          .containsKey('sess-${kMaxClearedSessions + 63}'),
      isFalse,
    );
  });

  group('streamed text', () {
    test('deltas for one part accumulate into a single row', () {
      final t = p4Feed([
        p4Ev('Hel', msg: 'm1', part: 'p1'),
        p4Ev('lo ', msg: 'm1', part: 'p1'),
        p4Ev('world', msg: 'm1', part: 'p1'),
      ]);
      expect(t.items, hasLength(1));
      expect(t.items.single.text, 'Hello world');
    });

    test('a snapshot replaces prior deltas instead of appending', () {
      final t = p4Feed([
        p4Ev('Hel', msg: 'm1', part: 'p1'),
        p4Ev('lo', msg: 'm1', part: 'p1'),
        p4Ev('Hello world', msg: 'm1', part: 'p1', replace: true),
      ]);
      expect(t.items, hasLength(1));
      expect(
        t.items.single.text,
        'Hello world',
        reason: 'appending the snapshot would render "HelloHello world"',
      );
    });

    test('replaying a whole turn over prior deltas does not duplicate it', () {
      final live = [
        p4Ev('Hel', msg: 'm1', part: 'p1'),
        p4Ev('lo', msg: 'm1', part: 'p1'),
      ];
      final replay = [p4Ev('Hello', msg: 'm1', part: 'p1', replace: true)];
      final t = p4Feed([...live, ...replay, ...replay, ...replay]);
      expect(t.items, hasLength(1));
      expect(t.items.single.text, 'Hello');
    });

    test('two parts of one message are separate rows', () {
      final t = p4Feed([
        p4Ev('first', msg: 'm1', part: 'p1'),
        p4Ev('second', msg: 'm1', part: 'p2'),
      ]);
      expect(p4Texts(t), ['first', 'second']);
    });

    test('a snapshot touches only its own part', () {
      final t = p4Feed([
        p4Ev('a', msg: 'm1', part: 'p1'),
        p4Ev('b', msg: 'm1', part: 'p2'),
        p4Ev('A', msg: 'm1', part: 'p1', replace: true),
      ]);
      expect(p4Texts(t), ['A', 'b']);
    });

    test('thought chunks reduce by identity too, separately from text', () {
      final t = p4Feed([
        p4Ev('thinking', msg: 'm1', part: 'p1', type: 'thought_chunk'),
        p4Ev('answer', msg: 'm1', part: 'p2'),
        p4Ev(
          'thought again',
          msg: 'm1',
          part: 'p1',
          type: 'thought_chunk',
          replace: true,
        ),
      ]);
      expect(p4Texts(t), ['thought again', 'answer']);
    });

    test('legacy chunks without identity keep append-only behaviour', () {
      final t = p4Feed([p4Ev('a'), p4Ev('b'), p4Ev('c')]);
      expect(t.items, hasLength(1));
      expect(t.items.single.text, 'abc');
    });

    test('a delta arriving after a snapshot extends it', () {
      final t = p4Feed([
        p4Ev('Hello', msg: 'm1', part: 'p1', replace: true),
        p4Ev(' world', msg: 'm1', part: 'p1'),
      ]);
      expect(t.items.single.text, 'Hello world');
    });
  });

  group('user messages', () {
    test('an authoritative part updates the optimistic row in place', () {
      final t = p4Feed([
        // Optimistic: message id, no part id.
        p4Ev('hi', msg: 'm1', type: 'user_message'),
        // Authoritative first user part for the same message.
        p4Ev('hi', msg: 'm1', part: 'p1', type: 'user_message', replace: true),
      ]);
      expect(
        t.items,
        hasLength(1),
        reason: 'resume must not render the user message twice',
      );
      expect(t.items.single.text, 'hi');
      expect(t.items.single.userParts, hasLength(1));
    });

    test('several native parts render as one bubble in order', () {
      final t = p4Feed([
        p4Ev('one ', msg: 'm1', part: 'p1', type: 'user_message'),
        p4Ev('two ', msg: 'm1', part: 'p2', type: 'user_message'),
        p4Ev('three', msg: 'm1', part: 'p3', type: 'user_message'),
      ]);
      expect(t.items, hasLength(1));
      expect(t.items.single.text, 'one two three');
      expect(
        [for (final p in t.items.single.userParts) p.nativePartId],
        ['p1', 'p2', 'p3'],
      );
    });

    test('a replaced component updates in place, keeping order', () {
      final t = p4Feed([
        p4Ev('one ', msg: 'm1', part: 'p1', type: 'user_message'),
        p4Ev('two', msg: 'm1', part: 'p2', type: 'user_message'),
        p4Ev(
          'ONE ',
          msg: 'm1',
          part: 'p1',
          type: 'user_message',
          replace: true,
        ),
      ]);
      expect(t.items.single.text, 'ONE two');
      expect(
        [for (final p in t.items.single.userParts) p.nativePartId],
        ['p1', 'p2'],
      );
    });

    test('two different messages stay two bubbles', () {
      final t = p4Feed([
        p4Ev('first', msg: 'm1', part: 'p1', type: 'user_message'),
        p4Ev('second', msg: 'm2', part: 'p1', type: 'user_message'),
      ]);
      expect(p4Texts(t), ['first', 'second']);
    });

    test('legacy user messages without identity still append', () {
      final t = p4Feed([
        p4Ev('a', type: 'user_message'),
        p4Ev('b', type: 'user_message'),
      ]);
      expect(p4Texts(t), ['a', 'b']);
    });
  });

  group('removal', () {
    test('a part tombstone removes only that row', () {
      final t = p4Feed([
        p4Ev('keep', msg: 'm1', part: 'p1'),
        p4Ev('gone', msg: 'm1', part: 'p2'),
        p4Removal(msg: 'm1', part: 'p2'),
      ]);
      expect(p4Texts(t), ['keep']);
    });

    test('a message tombstone removes every part of that message', () {
      final t = p4Feed([
        p4Ev('a', msg: 'm1', part: 'p1'),
        p4Ev('b', msg: 'm1', part: 'p2'),
        p4Ev('keep', msg: 'm2', part: 'p1'),
        p4Removal(msg: 'm1'),
      ]);
      expect(p4Texts(t), ['keep']);
    });

    test(
      'removing one component of a user bubble recomputes the aggregate',
      () {
        final t = p4Feed([
          p4Ev('one ', msg: 'm1', part: 'p1', type: 'user_message'),
          p4Ev('two', msg: 'm1', part: 'p2', type: 'user_message'),
          p4Removal(msg: 'm1', part: 'p1'),
        ]);
        expect(t.items, hasLength(1));
        expect(t.items.single.text, 'two');
        expect(t.items.single.userParts, hasLength(1));
      },
    );

    test('removing the last component removes the bubble', () {
      final t = p4Feed([
        p4Ev('only', msg: 'm1', part: 'p1', type: 'user_message'),
        p4Removal(msg: 'm1', part: 'p1'),
      ]);
      expect(t.items, isEmpty);
    });

    test('unknown ids are idempotent no-ops', () {
      final t = p4Feed([
        p4Ev('keep', msg: 'm1', part: 'p1'),
        p4Removal(msg: 'nope', part: 'nope'),
        p4Removal(msg: 'nope'),
        p4Removal(msg: 'm1', part: 'other'),
      ]);
      expect(p4Texts(t), ['keep']);
    });

    test('a repeated tombstone changes nothing after the first', () {
      final t = p4Feed([
        p4Ev('a', msg: 'm1', part: 'p1'),
        p4Ev('keep', msg: 'm2', part: 'p1'),
        p4Removal(msg: 'm1', part: 'p1'),
        p4Removal(msg: 'm1', part: 'p1'),
        p4Removal(msg: 'm1', part: 'p1'),
      ]);
      expect(p4Texts(t), ['keep']);
    });

    test('an empty message id is inert, never a wildcard', () {
      final t = p4Feed([
        p4Ev('a', msg: 'm1', part: 'p1'),
        p4Ev('b'),
        p4Removal(msg: ''),
        p4Removal(),
      ]);
      expect(p4Texts(t), ['a', 'b']);
    });

    test('legacy rows without identity are never removed by inference', () {
      final t = p4Feed([
        p4Ev('legacy'),
        p4Removal(msg: 'm1'),
        p4Removal(msg: 'm1', part: 'p1'),
      ]);
      expect(p4Texts(t), ['legacy']);
    });
  });

  group('cache round-trip', () {
    test('identity and components survive serialisation', () {
      const item = ChatItem(
        kind: ChatItemKind.user,
        text: 'one two',
        nativeMessageId: 'm1',
        nativePartId: 'p2',
        userParts: [
          UserPart(nativePartId: 'p1', text: 'one '),
          UserPart(nativePartId: 'p2', text: 'two'),
        ],
      );
      final back = ChatItem.fromJson(item.toJson());
      expect(back.nativeMessageId, 'm1');
      expect(back.nativePartId, 'p2');
      expect([for (final p in back.userParts) p.nativePartId], ['p1', 'p2']);
      expect(back.userText, 'one two');
    });

    test('a legacy cached row without identity reads back unchanged', () {
      final legacy = {'kind': 'assistant', 'seq': 4, 'text': 'hello'};
      final back = ChatItem.fromJson(legacy);
      expect(back.text, 'hello');
      expect(back.nativeMessageId, isNull);
      expect(back.nativePartId, isNull);
      expect(back.userParts, isEmpty);
    });

    test('a row without identity omits the new keys entirely', () {
      const item = ChatItem(kind: ChatItemKind.assistant, text: 'x');
      final json = item.toJson();
      expect(json.containsKey('nativeMessageId'), isFalse);
      expect(json.containsKey('nativePartId'), isFalse);
      expect(json.containsKey('userParts'), isFalse);
    });
  });

  group('user part serialisation', () {
    test('attachments round-trip inside a component', () {
      const part = UserPart(
        nativePartId: 'p1',
        text: 'look',
        attachments: [ChatAttachment(kind: 'image', mimeType: 'image/png')],
      );
      final back = UserPart.fromJson(part.toJson());
      expect(back.nativePartId, 'p1');
      expect(back.text, 'look');
      expect(back.attachments, hasLength(1));
      expect(back.attachments.single.kind, 'image');
      expect(back.attachments.single.mimeType, 'image/png');
    });

    test('an empty component omits optional keys', () {
      const part = UserPart(nativePartId: 'p1');
      final json = part.toJson();
      expect(json['native_part_id'], 'p1');
      expect(json.containsKey('text'), isFalse);
      expect(json.containsKey('attachments'), isFalse);
    });

    test('a malformed component decodes to safe defaults', () {
      final back = UserPart.fromJson(const {});
      expect(back.nativePartId, '');
      expect(back.text, '');
      expect(back.attachments, isEmpty);
    });

    test('userText prefers components over the flat text', () {
      const withParts = ChatItem(
        kind: ChatItemKind.user,
        text: 'stale',
        userParts: [
          UserPart(nativePartId: 'p1', text: 'one '),
          UserPart(nativePartId: 'p2', text: 'two'),
        ],
      );
      expect(withParts.userText, 'one two');
      const flat = ChatItem(kind: ChatItemKind.user, text: 'plain');
      expect(flat.userText, 'plain');
      const empty = ChatItem(kind: ChatItemKind.user);
      expect(empty.userText, '');
    });

    test('attachments from every component reach the aggregated row', () {
      final t = p4Feed([
        SessionEvent(
          type: 'user_message',
          sessionId: 's1',
          text: 'one',
          nativeMessageId: 'm1',
          nativePartId: 'p1',
          attachments: const [
            AttachmentInfo(kind: 'image', mimeType: 'image/png'),
          ],
        ),
        SessionEvent(
          type: 'user_message',
          sessionId: 's1',
          text: 'two',
          nativeMessageId: 'm1',
          nativePartId: 'p2',
          attachments: const [
            AttachmentInfo(kind: 'audio', mimeType: 'audio/wav'),
          ],
        ),
      ]);
      expect(t.items, hasLength(1));
      expect(t.items.single.attachments, hasLength(2));
      expect(
        [for (final a in t.items.single.attachments) a.kind],
        ['image', 'audio'],
      );
    });

    test('removing a component drops its attachments too', () {
      final t = p4Feed([
        SessionEvent(
          type: 'user_message',
          sessionId: 's1',
          text: 'one',
          nativeMessageId: 'm1',
          nativePartId: 'p1',
          attachments: const [
            AttachmentInfo(kind: 'image', mimeType: 'image/png'),
          ],
        ),
        SessionEvent(
          type: 'user_message',
          sessionId: 's1',
          text: 'two',
          nativeMessageId: 'm1',
          nativePartId: 'p2',
          attachments: const [
            AttachmentInfo(kind: 'audio', mimeType: 'audio/wav'),
          ],
        ),
        p4Removal(msg: 'm1', part: 'p1'),
      ]);
      expect(t.items.single.attachments, hasLength(1));
      expect(t.items.single.attachments.single.kind, 'audio');
    });
  });
}

// ---------------------------------------------------------------------------
// Transcript identity, replacement and removal (MADR 0112 A3, PLAN P4).
//
// The property under test throughout: a snapshot repeats text the agent already
// streamed, so a consumer that appends it renders the reply twice. Resume is
// exactly when that happens, which is why every case below reduces by native
// identity rather than by position.
//
// Helpers are prefixed `p4` to stay clear of the ingest-cost helpers above,
// which are local to main() and test a different property.

SessionEvent p4Ev(
  String text, {
  String? msg,
  String? part,
  bool replace = false,
  String type = 'assistant_message_chunk',
  List<AttachmentInfo> attachments = const [],
}) => SessionEvent(
  type: type,
  sessionId: 's1',
  text: text,
  nativeMessageId: msg,
  nativePartId: part,
  replace: replace,
  attachments: attachments,
);

SessionEvent p4Removal({String? msg, String? part}) => SessionEvent(
  type: 'transcript_remove',
  sessionId: 's1',
  nativeMessageId: msg,
  nativePartId: part,
);

SessionTranscript p4Feed(Iterable<SessionEvent> evs) {
  var t = const SessionTranscript(
    sessionId: 's1',
    status: 'idle',
    items: [],
    nextSeq: 0,
  );
  for (final ev in evs) {
    t = applySessionEvent(t, ev);
  }
  return t;
}

List<String> p4Texts(SessionTranscript t) => [
  for (final i in t.items) i.text ?? '',
];

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
}

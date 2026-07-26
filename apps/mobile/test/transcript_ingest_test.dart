import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/transcript_reducer.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// Ingest-side cost bounds, the counterpart to the render-side guarantees
/// asserted through `debugMarkdownParseCount` in streaming_markdown_test.dart.
///
/// What matters here is that the work of applying a batch scales with flush
/// windows, not with streamed tokens: `_appendChunk` copies the whole
/// accumulated reply every time it runs, so one call per token is O(N·k)
/// (MADR 0018 C1, closed by MADR 0024 phase 5).
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

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
}

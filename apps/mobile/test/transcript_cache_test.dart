import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/chat_models.dart';
import 'package:magic_cli_remote/data/chat/transcript_cache.dart';
import 'package:magic_cli_remote/data/chat/transcript_reducer.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  test('round-trips a transcript tail', () async {
    final cache = TranscriptCache();
    final t = SessionTranscript(
      sessionId: 's1',
      status: 'idle',
      nextSeq: 3,
      items: [
        ChatItem.user('hi').copyWith(seq: 1),
        ChatItem.assistant('hello there').copyWith(seq: 2),
      ],
    );
    await cache.save('s1', t);
    final loaded = await cache.load('s1');
    expect(loaded, isNotNull);
    expect(loaded!.items, hasLength(2));
    expect(loaded.items.first.text, 'hi');
    expect(loaded.items.last.text, 'hello there');
    expect(loaded.nextSeq, 3);
  });

  test('empty transcript removes the cache entry', () async {
    final cache = TranscriptCache();
    await cache.save(
      's1',
      SessionTranscript(
        sessionId: 's1',
        items: [ChatItem.user('x').copyWith(seq: 1)],
        nextSeq: 2,
      ),
    );
    expect(await cache.load('s1'), isNotNull);
    await cache.save('s1', const SessionTranscript(sessionId: 's1'));
    expect(await cache.load('s1'), isNull);
  });

  test('keeps only the last kTranscriptCacheMaxItems', () async {
    final cache = TranscriptCache();
    final items = [
      for (var i = 0; i < kTranscriptCacheMaxItems + 40; i++)
        ChatItem.user('m$i').copyWith(seq: i + 1),
    ];
    await cache.save(
      's1',
      SessionTranscript(
        sessionId: 's1',
        items: items,
        nextSeq: items.length + 1,
      ),
    );
    final loaded = await cache.load('s1');
    expect(loaded, isNotNull);
    expect(loaded!.items, hasLength(kTranscriptCacheMaxItems));
    expect(loaded.items.first.text, 'm40');
    expect(loaded.items.last.text, 'm${kTranscriptCacheMaxItems + 39}');
  });

  test('concurrent saves keep every session indexed (no orphaned blobs)', () async {
    final cache = TranscriptCache();
    SessionTranscript tx(String id) => SessionTranscript(
      sessionId: id,
      items: [ChatItem.user('hi from $id').copyWith(seq: 1)],
      nextSeq: 2,
    );
    // Fire the debounce-window race: both saves in flight at once. The
    // index read-modify-write must serialize or one session is dropped from
    // the index while its blob stays stored forever.
    await Future.wait([cache.save('a', tx('a')), cache.save('b', tx('b'))]);
    expect(await cache.load('a'), isNotNull);
    expect(await cache.load('b'), isNotNull);
    // Both must also be evictable: clear() leaves nothing behind.
    await cache.clear();
    final p = await SharedPreferences.getInstance();
    expect(p.getKeys().where((k) => k.startsWith('tx_cache_v1_')), isEmpty);
  });

  test('load normalizes a stale running status to idle', () async {
    final cache = TranscriptCache();
    await cache.save(
      's1',
      SessionTranscript(
        sessionId: 's1',
        status: 'running',
        items: [ChatItem.user('go').copyWith(seq: 1)],
        nextSeq: 2,
      ),
    );
    final loaded = await cache.load('s1');
    // A cached 'running' described a turn that died with the process; if the
    // host ring is gone too, nothing else would ever unwedge the composer.
    expect(loaded!.status, 'idle');
  });

  test('a live chunk after hydrate opens a new bubble, never merges', () async {
    final cache = TranscriptCache();
    await cache.save(
      's1',
      SessionTranscript(
        sessionId: 's1',
        items: [
          ChatItem.user('q1').copyWith(seq: 1),
          ChatItem.assistant('old completed reply').copyWith(seq: 2),
        ],
        nextSeq: 3,
      ),
    );
    final loaded = (await cache.load('s1'))!;
    expect(loaded.sealedTail, isTrue);
    // The cache is a debounced snapshot: this chunk may belong to a newer
    // turn whose user message never made it into the snapshot.
    final next = applySessionEvent(
      loaded,
      SessionEvent(
        type: 'assistant_message_chunk',
        sessionId: 's1',
        text: 'new reply',
      ),
    );
    expect(next.items, hasLength(3));
    expect(next.items[1].text, 'old completed reply');
    expect(next.items.last.text, 'new reply');
    // The freshly opened bubble streams normally from here on.
    final more = applySessionEvent(
      next,
      SessionEvent(
        type: 'assistant_message_chunk',
        sessionId: 's1',
        text: ' continues',
      ),
    );
    expect(more.items, hasLength(3));
    expect(more.items.last.text, 'new reply continues');
  });

  test('ChatItem JSON preserves tool fields', () {
    final item = ChatItem.tool(
      id: 't1',
      name: 'Shell',
      status: 'completed',
      detail: 'ok',
      toolKind: 'execute',
      seq: 9,
    );
    final back = ChatItem.fromJson(item.toJson());
    expect(back.toolId, 't1');
    expect(back.toolName, 'Shell');
    expect(back.toolStatus, 'completed');
    expect(back.toolKind, 'execute');
    expect(back.seq, 9);
    expect(back.toolClass, ToolClass.command);
  });
}

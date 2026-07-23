import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/chat_models.dart';
import 'package:magic_cli_remote/data/chat/transcript_cache.dart';
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

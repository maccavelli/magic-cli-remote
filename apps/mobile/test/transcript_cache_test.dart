import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/chat_models.dart';
import 'package:magic_cli_remote/data/chat/transcript_cache.dart';
import 'package:magic_cli_remote/data/chat/transcript_reducer.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late Directory tmp;

  setUp(() {
    SharedPreferences.setMockInitialValues({});
    // MADR 0084 D3: entries are files now. Every test gets its own directory
    // so none can touch the real application-support path.
    tmp = Directory.systemTemp.createTempSync('tx_cache_test');
  });

  tearDown(() {
    if (tmp.existsSync()) tmp.deleteSync(recursive: true);
  });

  TranscriptCache newCache() => TranscriptCache(directory: tmp);

  /// Session ids with an entry file — the file-era equivalent of sweeping
  /// prefs keys by prefix (MADR 0084 D3).
  Set<String> storedIds() {
    final dir = Directory('${tmp.path}/transcripts');
    if (!dir.existsSync()) return {};
    return dir
        .listSync()
        .whereType<File>()
        .where((f) => f.path.endsWith('.json'))
        .map(
          (f) => Uri.decodeComponent(
            f.path.split('/').last.replaceAll('.json', ''),
          ),
        )
        .toSet();
  }

  test('round-trips a transcript tail', () async {
    final cache = newCache();
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

  test('itemless control snapshot survives save/load', () async {
    final cache = newCache();
    await cache.save(
      's1',
      const SessionTranscript(
        sessionId: 's1',
        modes: [SessionMode(id: 'default', name: 'default')],
        currentModeId: 'default',
        collaborationModes: [
          CollaborationMode(id: 'plan', name: 'Plan'),
          CollaborationMode(id: 'default', name: 'Default'),
        ],
        currentCollaborationModeId: 'plan',
        goal: SessionGoal(objective: 'Ship', status: 'paused'),
      ),
    );
    final loaded = await cache.load('s1');
    expect(loaded, isNotNull);
    expect(loaded!.items, isEmpty);
    expect(loaded.currentModeId, 'default');
    expect(loaded.currentCollaborationModeId, 'plan');
    expect(loaded.collaborationModes.map((m) => m.id), ['plan', 'default']);
    expect(loaded.goal?.objective, 'Ship');
    expect(loaded.goal?.status, 'paused');
  });

  test('empty transcript removes the cache entry', () async {
    final cache = newCache();
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
    final cache = newCache();
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

  test(
    'concurrent saves keep every session indexed (no orphaned blobs)',
    () async {
      final cache = newCache();
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
    },
  );

  test('a large snapshot is stored, not dropped', () async {
    // MADR 0084 D3: the prefs-era code had to delete the user's snapshot when
    // it exceeded a 400 KB preferences budget. A file has no such limit, and
    // kTranscriptCacheMaxItems already bounds the entry.
    final cache = newCache();
    final big = 'x' * 300 * 1024;
    await cache.save(
      's1',
      SessionTranscript(
        sessionId: 's1',
        items: [
          for (var i = 0; i < 4; i++) ChatItem.user(big).copyWith(seq: i + 1),
        ],
        nextSeq: 5,
      ),
    );
    final loaded = await cache.load('s1');
    expect(loaded, isNotNull);
    expect(loaded!.items, hasLength(4));
  });

  test('load normalizes a stale running status to idle', () async {
    final cache = newCache();
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
    final cache = newCache();
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

  // Concurrency. Every mutation is a read-modify-write of the shared index key,
  // so two of them interleaving across their awaits drops a session from the
  // index while its entry blob stays stored — invisible to LRU eviction and to
  // clear(), i.e. prefs that grow without bound. The cache serializes them on
  // one future chain; these tests are what hold that.
  group('serialized mutations', () {
    SessionTranscript one(String id, String text) => SessionTranscript(
      sessionId: id,
      status: 'idle',
      nextSeq: 2,
      items: [ChatItem.user(text).copyWith(seq: 1)],
    );

    test('a remove queued behind a save wins', () async {
      final cache = newCache();
      await cache.save('s1', one('s1', 'first'));
      // Queued in this order without awaiting between them, which is how the
      // debounced save timer and a session eviction actually collide. Order is
      // the guarantee: the remove was asked for last, so it decides — otherwise
      // a closed session's transcript can come back from the dead on reopen.
      final save = cache.save('s1', one('s1', 'second'));
      final removed = cache.remove('s1');
      await Future.wait([save, removed]);
      final prefs = await SharedPreferences.getInstance();
      expect(prefs.getString('tx_cache_v1_s1'), isNull);
      expect(
        prefs.getStringList('tx_cache_v1_index') ?? <String>[],
        isNot(contains('s1')),
      );
    });

    test(
      'clear() racing saves removes every entry, not just indexed ones',
      () async {
        final cache = newCache();
        final saves = [
          for (var i = 0; i < 5; i++) cache.save('s$i', one('s$i', 'm$i')),
        ];
        final cleared = cache.clear();
        await Future.wait([...saves, cleared]);
        // clear() sweeps by key prefix, so anything queued before it is gone
        // whether or not the index knew about it. Saves queued after survive;
        // here all five were queued first.
        expect(storedIds(), isEmpty);
      },
    );

    test(
      'evicting the oldest session under load keeps index and blobs in step',
      () async {
        final cache = newCache();
        final n = kTranscriptCacheMaxSessions + 4;
        await Future.wait([
          for (var i = 0; i < n; i++) cache.save('s$i', one('s$i', 'm$i')),
        ]);
        final prefs = await SharedPreferences.getInstance();
        final index = prefs.getStringList('tx_cache_v1_index') ?? <String>[];
        expect(index, hasLength(kTranscriptCacheMaxSessions));
        final blobs = storedIds();
        // No blob without an index entry (invisible to eviction and clear), and
        // no index entry without a blob (a load that hydrates nothing).
        expect(blobs, equals(index.toSet()));
      },
    );

    test('retainOnly keeps the index for sessions that are all live', () async {
      final cache = newCache();
      await cache.save('s1', one('s1', 'a'));
      await cache.save('s2', one('s2', 'b'));

      // The sessions screen calls this on every refresh and every reconnect.
      // Sweeping by prefix also matched the index key itself, so the whole
      // index was deleted and rewritten empty (MADR 0046 H-C).
      await cache.retainOnly({'s1', 's2'});

      final prefs = await SharedPreferences.getInstance();
      expect(
        prefs.getStringList('tx_cache_v1_index'),
        equals(['s1', 's2']),
        reason: 'a live session must not lose its place in the LRU index',
      );
      expect(storedIds(), equals({'s1', 's2'}));
    });

    test('retainOnly re-adopts blobs stranded by a lost index', () async {
      final cache = newCache();
      await cache.save('s1', one('s1', 'a'));
      final prefs = await SharedPreferences.getInstance();
      // Exactly the state released builds left behind: the blob is stored but
      // no index entry points at it, so nothing can ever evict or clear it.
      await prefs.remove('tx_cache_v1_index');

      await cache.retainOnly({'s1'});
      expect(prefs.getStringList('tx_cache_v1_index'), equals(['s1']));

      await cache.clear();
      expect(storedIds(), isEmpty);
    });

    test('eviction still bites after a retainOnly', () async {
      final cache = newCache();
      final n = kTranscriptCacheMaxSessions + 2;
      for (var i = 0; i < n; i++) {
        await cache.save('s$i', one('s$i', 'm$i'));
      }
      await cache.retainOnly({for (var i = 0; i < n; i++) 's$i'});
      await cache.save('later', one('later', 'newest'));

      final prefs = await SharedPreferences.getInstance();
      final index = prefs.getStringList('tx_cache_v1_index') ?? <String>[];
      expect(index, hasLength(kTranscriptCacheMaxSessions));
      expect(index.last, 'later');
      final blobs = storedIds();
      expect(blobs, equals(index.toSet()));
    });
  });

  test('usage counts index entries and ignores unrelated prefs', () async {
    SharedPreferences.setMockInitialValues({
      'host': 'example.com',
      'token': 'x',
    });
    final cache = newCache();
    final empty = await cache.usage();
    expect(empty.sessions, 0);

    await cache.save(
      's1',
      SessionTranscript(
        sessionId: 's1',
        status: 'idle',
        nextSeq: 2,
        items: [ChatItem.user('a').copyWith(seq: 1)],
      ),
    );
    final u = await cache.usage();
    expect(u.sessions, 1);
    expect(u.bytes, greaterThan(0));

    await cache.clear();
    final after = await cache.usage();
    expect(after.sessions, 0);
    // Unrelated prefs survive.
    final p = await SharedPreferences.getInstance();
    expect(p.getString('host'), 'example.com');
  });

  // ---------------------------------------------------------------------
  // MADR 0084 D3 — the file backend's own invariants.
  // ---------------------------------------------------------------------

  test('a session id containing ../ cannot escape the directory', () async {
    // Session ids are host-supplied. The flat prefs key space made traversal
    // impossible; a file path does not, so the id is percent-encoded.
    final cache = newCache();
    const nasty = '../../escaped';
    await cache.save(nasty, one2(nasty, 'payload'));

    final dir = Directory('${tmp.path}/transcripts');
    final files = dir.listSync().whereType<File>().toList();
    expect(files, hasLength(1));
    // The property that matters is containment, not the absence of dots:
    // percent-encoding escapes the separator, so `../../escaped` becomes the
    // single filename `..%2F..%2Fescaped.json` *inside* the directory.
    expect(files.single.parent.path, dir.path);
    expect(
      files.single.absolute.uri.normalizePath().toFilePath(),
      startsWith(dir.absolute.uri.normalizePath().toFilePath()),
    );
    expect(files.single.path, contains('%2F'));
    // And it still round-trips under its real id.
    expect((await cache.load(nasty))?.items.first.text, 'payload');
  });

  test('eviction deletes the file, not just the index entry', () async {
    final cache = newCache();
    final n = kTranscriptCacheMaxSessions + 3;
    for (var i = 0; i < n; i++) {
      await cache.save('s$i', one2('s$i', 'm$i'));
    }
    // The oldest three are gone from disk, not merely unindexed — an
    // unindexed file is invisible to both eviction and clear().
    expect(storedIds(), hasLength(kTranscriptCacheMaxSessions));
    expect(storedIds().contains('s0'), isFalse);
    expect(await cache.load('s0'), isNull);
  });

  test('usage stats files without reading their contents', () async {
    final cache = newCache();
    await cache.save('s1', one2('s1', 'x' * 5000));
    final u = await cache.usage();
    expect(u.sessions, 1);
    // A stat sum, so it tracks the real file size (MADR 0084 B3).
    final f = Directory(
      '${tmp.path}/transcripts',
    ).listSync().whereType<File>().single;
    expect(u.bytes, f.lengthSync());
  });

  test('legacy prefs entries migrate to files exactly once', () async {
    // Seed the v1 shape a released build would leave behind.
    SharedPreferences.setMockInitialValues({
      'tx_cache_v1_index': ['s1'],
      'tx_cache_v1_s1': jsonEncode({
        'sessionId': 's1',
        'status': 'idle',
        'nextSeq': 2,
        'items': [ChatItem.user('from prefs').copyWith(seq: 1).toJson()],
      }),
      'host': 'example.com',
    });

    final cache = newCache();
    final loaded = await cache.load('s1');
    expect(loaded, isNotNull, reason: 'the migrated entry must still load');
    expect(loaded!.items.first.text, 'from prefs');

    final prefs = await SharedPreferences.getInstance();
    expect(
      prefs.getKeys().where(
        (k) => k.startsWith('tx_cache_v1_') && k != 'tx_cache_v1_index',
      ),
      isEmpty,
      reason: 'the prefs blobs must be gone, not duplicated',
    );
    expect(prefs.getBool('tx_cache_migrated_v2'), isTrue);
    // Unrelated preferences are untouched.
    expect(prefs.getString('host'), 'example.com');
  });

  test(
    'an unreadable legacy entry leaves the cache usable, not broken',
    () async {
      SharedPreferences.setMockInitialValues({
        'tx_cache_v1_index': ['s1'],
        'tx_cache_v1_s1': 'not json at all',
      });

      final cache = newCache();
      // Garbage migrates as-is and simply fails to decode; the point is that
      // the app still starts and the cache still works.
      expect(await cache.load('s1'), isNull);
      await cache.save('s2', one2('s2', 'fresh'));
      expect((await cache.load('s2'))?.items.first.text, 'fresh');
    },
  );
}

SessionTranscript one2(String id, String text) => SessionTranscript(
  sessionId: id,
  status: 'idle',
  nextSeq: 2,
  items: [ChatItem.user(text).copyWith(seq: 1)],
);

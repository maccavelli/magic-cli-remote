import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/chat_models.dart';
import 'package:magic_cli_remote/data/chat/transcript_cache.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// MADR 0201 D33 (0073 §7.1): the settings screen builds its own
/// TranscriptCache instances, for usage and for clear, and their writes
/// bypassed the live notifier's `_serial` chain, which belonged to one
/// instance. A save and a clear from two instances could then leave an index
/// naming files that are gone, or files no index names.
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late Directory tmp;

  setUp(() {
    SharedPreferences.setMockInitialValues({});
    tmp = Directory.systemTemp.createTempSync('tx_cache_serial_test');
  });

  tearDown(() {
    if (tmp.existsSync()) tmp.deleteSync(recursive: true);
  });

  Set<String> storedIds() {
    final dir = Directory('${tmp.path}/transcripts');
    if (!dir.existsSync()) return {};
    return dir
        .listSync()
        .whereType<File>()
        .where((f) => f.path.endsWith('.json'))
        .map(
          (f) => Uri.decodeComponent(
            entryBasename(f.path).replaceAll('.json', ''),
          ),
        )
        .toSet();
  }

  SessionTranscript transcript(String id) => SessionTranscript(
    sessionId: id,
    status: 'idle',
    nextSeq: 2,
    items: [ChatItem.user('hi').copyWith(seq: 1)],
  );

  test('two instances keep the index and the files in step', () async {
    final live = TranscriptCache(directory: tmp);
    final settings = TranscriptCache(directory: tmp);
    for (var round = 0; round < 20; round++) {
      await Future.wait([
        live.save('s$round', transcript('s$round')),
        settings.clear(),
        live.save('t$round', transcript('t$round')),
      ]);
      await live.debugWhenIdle;
      await settings.debugWhenIdle;
      final prefs = await SharedPreferences.getInstance();
      final index = (prefs.getStringList('tx_cache_v1_index') ?? const [])
          .toSet();
      expect(
        index,
        storedIds(),
        reason: 'round $round: the index must list exactly the files on disk',
      );
    }
  });
}

import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/chat_models.dart';
import 'package:magic_cli_remote/data/chat/transcript_cache.dart';

/// MADR 0084 P2 step 2: `compute`'s result crosses an isolate boundary, and
/// the plan requires that to be asserted rather than assumed — if
/// `SessionTranscript` were unsendable the fallback is to return the decoded
/// map instead. This test is what decides.
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  String payloadFor(String sessionId, int itemCount) => jsonEncode({
    'sessionId': sessionId,
    'status': 'idle',
    'nextSeq': itemCount + 1,
    'items': [
      for (var i = 0; i < itemCount; i++)
        ChatItem(
          seq: i + 1,
          kind: ChatItemKind.assistant,
          text: 'message $i',
        ).toJson(),
    ],
  });

  test('a decoded transcript survives a real isolate hop', () async {
    final decoded = await compute(decodeTranscriptCachePayload, (
      payloadFor('s1', 3),
      's1',
    ));

    expect(decoded, isNotNull);
    expect(decoded!.sessionId, 's1');
    expect(decoded.items, hasLength(3));
    expect(decoded.items.first.text, 'message 0');
    expect(decoded.status, 'idle');
    // The snapshot may end mid-turn, so the tail is sealed on restore.
    expect(decoded.sealedTail, isTrue);
  });

  test('a cached running status is downgraded to idle', () async {
    final raw = jsonEncode({
      'sessionId': 's2',
      'status': 'running',
      'nextSeq': 2,
      'items': [
        ChatItem(seq: 1, kind: ChatItemKind.assistant, text: 'hi').toJson(),
      ],
    });

    final decoded = await compute(decodeTranscriptCachePayload, (raw, 's2'));
    // A cached 'running' is always stale — the turn died with the process.
    expect(decoded!.status, 'idle');
  });

  test('unusable payloads decode to null rather than throwing', () async {
    expect(
      await compute(decodeTranscriptCachePayload, ('not json at all', 's3')),
      isNull,
    );
    expect(
      await compute(decodeTranscriptCachePayload, ('{"items": "nope"}', 's4')),
      isNull,
    );
    // Structurally valid but carrying nothing worth restoring.
    expect(
      await compute(decodeTranscriptCachePayload, ('{"items": []}', 's5')),
      isNull,
    );
  });
}

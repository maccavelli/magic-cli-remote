@Tags(['bench'])
library;

import 'dart:convert';

import 'package:flutter/foundation.dart' show compute;
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/chat_models.dart';
import 'package:magic_cli_remote/data/chat/transcript_cache.dart';

/// MADR 0128 D4 — measure the `compute()` cost per debounced transcript save.
///
/// 0126 deferred this as "noted during the pass, not measured, and a
/// performance question rather than a defect". This measures it and **changes
/// nothing** (0128-PLAN C2): a benchmark that arrives with an optimisation is
/// not a measurement.
///
/// Excluded from the default suite by its `bench` tag. Run with:
///   flutter test --tags bench test/transcript_cache_bench_test.dart
///
/// LIMIT OF THE INSTRUMENT: this is a desktop VM. Isolate spawn and JSON codec
/// costs here are not an Android phone's, so the numbers bound the question
/// rather than settle it. What transfers is the *ratio* between the isolate
/// path and the inline one, and the absolute payload size.
void main() {
  Map<String, dynamic> payloadOf(int items) => <String, dynamic>{
    'sessionId': 's1',
    'status': 'idle',
    'nextSeq': items + 1,
    'items': [
      for (var i = 0; i < items; i++)
        ChatItem.assistant(
          // Representative of a streamed reply rather than a one-liner.
          'Line $i of an assistant reply. ${'lorem ipsum dolor sit amet ' * 12}',
        ).copyWith(seq: i + 1).toJson(),
    ],
  };

  ({Duration median, Duration p90}) stats(List<Duration> xs) {
    xs.sort();
    return (median: xs[xs.length ~/ 2], p90: xs[(xs.length * 9) ~/ 10]);
  }

  Future<({Duration median, Duration p90})> timeAsync(
    int n,
    Future<void> Function() body,
  ) async {
    await body(); // warm up
    final xs = <Duration>[];
    for (var i = 0; i < n; i++) {
      final sw = Stopwatch()..start();
      await body();
      xs.add(sw.elapsed);
    }
    return stats(xs);
  }

  test('compute() vs inline, at kTranscriptCacheMaxItems', () async {
    const n = 50;
    final payload = payloadOf(kTranscriptCacheMaxItems);
    final encoded = jsonEncode(payload);

    final isolateEncode = await timeAsync(
      n,
      () async => compute(encodeTranscriptCachePayload, payload),
    );
    final inlineEncode = await timeAsync(
      n,
      () async => encodeTranscriptCachePayload(payload),
    );
    final isolateDecode = await timeAsync(
      n,
      () async => compute(decodeTranscriptCachePayload, (encoded, 's1')),
    );
    final inlineDecode = await timeAsync(
      n,
      () async => decodeTranscriptCachePayload((encoded, 's1')),
    );

    String us(Duration d) =>
        '${(d.inMicroseconds / 1000).toStringAsFixed(2)}ms';

    // ignore: avoid_print
    print('''
--- 0128 D4: transcript cache codec cost (N=$n, ${kTranscriptCacheMaxItems} items) ---
payload serialized:   ${encoded.length} bytes
encode via compute(): median ${us(isolateEncode.median)}  p90 ${us(isolateEncode.p90)}
encode inline:        median ${us(inlineEncode.median)}  p90 ${us(inlineEncode.p90)}
decode via compute(): median ${us(isolateDecode.median)}  p90 ${us(isolateDecode.p90)}
decode inline:        median ${us(inlineDecode.median)}  p90 ${us(inlineDecode.p90)}
worst-case cadence:   1 save / ${kTranscriptCacheDebounceMs}ms / session
------------------------------------------------------------------''');

    // Not an assertion about performance — just that the benchmark ran.
    expect(encoded.length, greaterThan(0));
  });
}

/// Mirrors `kTranscriptCacheDebounce` without importing the state layer.
const int kTranscriptCacheDebounceMs = 400;

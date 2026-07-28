import 'dart:typed_data';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// Lifecycle of the per-session image buffer.
///
/// The sending device holds the raw bytes of an outgoing prompt until the
/// daemon echoes the `user_message` back, then folds them into the rendered
/// bubble (the echo carries descriptors only). Those bytes are megabytes each,
/// so anything that ends a session has to release them — audit 0041 H3 found
/// the buffer was pruned only on a successful echo or an explicit unstage, and
/// so survived session deletion and sign-out for the process lifetime.
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  ProviderContainer makeContainer() {
    final c = ProviderContainer();
    addTearDown(c.dispose);
    return c;
  }

  SessionEvent imageEcho({required int seq, String sessionId = 's1'}) =>
      SessionEvent(
        type: 'user_message',
        sessionId: sessionId,
        seq: seq,
        text: 'look at this',
        attachments: const [
          AttachmentInfo(kind: 'image', mimeType: 'image/png'),
        ],
      );

  Uint8List bytes(int fill) => Uint8List.fromList(List.filled(8, fill));

  test('a staged image is folded into its own echo', () {
    final c = makeContainer();
    final n = c.read(transcriptsProvider.notifier);

    n.stageSentImages('s1', [bytes(1)]);
    n.debugOnEvent(imageEcho(seq: 1));

    final item = c.read(transcriptsProvider).forSession('s1').items.single;
    expect(
      item.attachments.single.bytes,
      isNotNull,
      reason: 'the sender sees a real thumbnail, not a placeholder',
    );
  });

  test('unstaging a failed send prevents its bytes reaching a later echo', () {
    final c = makeContainer();
    final n = c.read(transcriptsProvider.notifier);

    n.stageSentImages('s1', [bytes(6)]);
    n.unstageSentImages('s1');
    n.debugOnEvent(imageEcho(seq: 1));

    expect(
      c
          .read(transcriptsProvider)
          .forSession('s1')
          .items
          .single
          .attachments
          .single
          .bytes,
      isNull,
    );
  });

  test(
    'resync and its deferred live events never consume staged image bytes',
    () async {
      final c = makeContainer();
      final n = c.read(transcriptsProvider.notifier);
      n.stageSentImages('s1', [bytes(7)]);
      final history = <SessionEvent>[
        imageEcho(seq: 1),
        for (var i = 2; i <= kHistoryApplyBatchSize + 1; i++)
          SessionEvent(
            type: 'user_message',
            sessionId: 's1',
            seq: i,
            text: 'history $i',
          ),
      ];

      final resync = n.resyncHistory('s1', history);
      await Future<void>.delayed(Duration.zero);
      n.debugOnEvent(imageEcho(seq: history.length + 1));
      await resync;

      final replayed = c.read(transcriptsProvider).forSession('s1');
      expect(replayed.items.first.attachments.single.bytes, isNull);
      expect(replayed.items.last.attachments.single.bytes, isNull);

      // The abandoned FIFO cannot leak into the first normal live echo after
      // the resync has completed either.
      n.debugOnEvent(imageEcho(seq: history.length + 2));
      expect(
        c
            .read(transcriptsProvider)
            .forSession('s1')
            .items
            .last
            .attachments
            .single
            .bytes,
        isNull,
      );
    },
  );

  test('clearSession releases bytes whose echo never arrived', () {
    final c = makeContainer();
    final n = c.read(transcriptsProvider.notifier);

    n.debugOnEvent(
      SessionEvent(type: 'user_message', sessionId: 's1', seq: 1, text: 'hi'),
    );
    n.stageSentImages('s1', [bytes(2)]);

    // Ending the session on the host: the echo can never arrive now.
    n.clearSession('s1');

    // A later session reusing the id must not inherit the orphaned bytes.
    n.debugOnEvent(imageEcho(seq: 2));
    final item = c.read(transcriptsProvider).forSession('s1').items.last;
    expect(item.attachments.single.bytes, isNull);
  });

  test('clearAll releases every session\'s bytes', () {
    final c = makeContainer();
    final n = c.read(transcriptsProvider.notifier);

    n.stageSentImages('s1', [bytes(3)]);
    n.stageSentImages('s2', [bytes(4)]);

    // Sign-out. Image bytes must not survive it.
    n.clearAll();

    n.debugOnEvent(imageEcho(seq: 1));
    n.debugOnEvent(imageEcho(seq: 1, sessionId: 's2'));
    expect(
      c
          .read(transcriptsProvider)
          .forSession('s1')
          .items
          .last
          .attachments
          .single
          .bytes,
      isNull,
    );
    expect(
      c
          .read(transcriptsProvider)
          .forSession('s2')
          .items
          .last
          .attachments
          .single
          .bytes,
      isNull,
    );
  });

  test('syncFromMeta drops bytes for a session the host forgot', () {
    final c = makeContainer();
    final n = c.read(transcriptsProvider.notifier);

    n.debugOnEvent(
      SessionEvent(type: 'user_message', sessionId: 's1', seq: 1, text: 'hi'),
    );
    n.stageSentImages('s1', [bytes(5)]);

    // The host no longer lists s1 — the same rule that evicts its transcript,
    // seq bookkeeping and cache entry has to reach the image buffer too.
    n.syncFromMeta(const []);

    n.debugOnEvent(imageEcho(seq: 2));
    final item = c.read(transcriptsProvider).forSession('s1').items.last;
    expect(item.attachments.single.bytes, isNull);
  });
}

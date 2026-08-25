import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// Assistant artifacts (MADR 0112 A3, PLAN P5).
///
/// Two properties matter: an artifact is an addressable transcript row like any
/// other, so replay replaces rather than duplicates it; and content the daemon
/// could not carry is inert rather than a tap that cannot work.

/// A real 1x1 PNG, so an inline preview decodes rather than failing for reasons
/// of its own.
final pngBytes = base64Decode(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8'
  'z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
);

SessionEvent artifactEvent({
  String msg = 'm1',
  String part = 'p1',
  ArtifactInfo? info,
}) => SessionEvent(
  type: 'artifact',
  sessionId: 's1',
  nativeMessageId: msg,
  nativePartId: part,
  artifact: info ?? const ArtifactInfo(filename: 'a.png', mime: 'image/png'),
);

SessionTranscript feed(Iterable<SessionEvent> evs) {
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

/// Minimal client: chat only fetches history when the transcript is empty, and
/// these tests seed a non-empty one.
class _FakeClient extends McremoteClient {
  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [SessionMeta(id: 's1', provider: 'opencode', cwd: '/w')],
        complete: true,
      );
}

/// Renders one artifact row inside a real chat screen: the bubble widget is
/// private to chat_screen.dart, so the screen is the seam.
Future<void> pumpItem(WidgetTester tester, ChatItem item) async {
  tester.view.physicalSize = const Size(600, 1400);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
  final transcript = SessionTranscript(
    sessionId: 's1',
    status: 'idle',
    items: [item],
    nextSeq: 1,
  );
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        mcremoteClientProvider.overrideWithValue(_FakeClient()),
        connectionStateProvider.overrideWith(
          (ref) => Stream.value(McConnectionState.connected),
        ),
        sessionTranscriptProvider('s1').overrideWithValue(transcript),
      ],
      child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
    ),
  );
  await tester.pump();
}

void main() {
  tearDown(() => debugOpenArtifactUrl = null);

  group('reduction', () {
    test('an artifact becomes its own row', () {
      final t = feed([artifactEvent()]);
      expect(t.items, hasLength(1));
      expect(t.items.single.kind, ChatItemKind.artifact);
      expect(t.items.single.artifact?.filename, 'a.png');
    });

    test('a re-sent artifact replaces rather than duplicating', () {
      final t = feed([
        artifactEvent(info: const ArtifactInfo(filename: 'a.png', bytes: 10)),
        artifactEvent(info: const ArtifactInfo(filename: 'a.png', bytes: 20)),
        artifactEvent(info: const ArtifactInfo(filename: 'a.png', bytes: 20)),
      ]);
      expect(
        t.items,
        hasLength(1),
        reason: 'resume must not stack a card per replay',
      );
      expect(t.items.single.artifact?.bytes, 20);
    });

    test('two attachments of one tool are two rows', () {
      final t = feed([
        artifactEvent(part: 'prt_1#0'),
        artifactEvent(part: 'prt_1#1'),
      ]);
      expect(t.items, hasLength(2));
    });

    test('a tombstone removes an artifact row', () {
      final t = feed([
        artifactEvent(part: 'p1'),
        artifactEvent(part: 'p2'),
        SessionEvent(
          type: 'transcript_remove',
          sessionId: 's1',
          nativeMessageId: 'm1',
          nativePartId: 'p1',
        ),
      ]);
      expect(t.items, hasLength(1));
      expect(t.items.single.nativePartId, 'p2');
    });

    test('a message tombstone removes every artifact of that message', () {
      final t = feed([
        artifactEvent(part: 'p1'),
        artifactEvent(part: 'p2'),
        SessionEvent(
          type: 'transcript_remove',
          sessionId: 's1',
          nativeMessageId: 'm1',
        ),
      ]);
      expect(t.items, isEmpty);
    });

    test('an artifact event with no payload is ignored', () {
      final t = feed([
        SessionEvent(
          type: 'artifact',
          sessionId: 's1',
          nativeMessageId: 'm1',
          nativePartId: 'p1',
        ),
      ]);
      expect(t.items, isEmpty);
    });

    test('an artifact without identity still appends', () {
      final t = feed([
        SessionEvent(
          type: 'artifact',
          sessionId: 's1',
          artifact: const ArtifactInfo(filename: 'a.png'),
        ),
      ]);
      expect(t.items, hasLength(1));
      expect(t.items.single.nativePartId, isNull);
    });
  });

  group('cache serialisation', () {
    test('inline bytes are stripped and the row is marked truncated', () {
      final item = ChatItem(
        kind: ChatItemKind.artifact,
        artifact: ArtifactInfo(
          filename: 'a.png',
          mime: 'image/png',
          bytes: pngBytes.length,
          data: base64Encode(pngBytes),
        ),
      );
      final json = item.toJson();
      final cached = jsonEncode(json);
      expect(
        cached.contains(base64Encode(pngBytes)),
        isFalse,
        reason: 'a transcript cache must not become a file store',
      );
      final back = ChatItem.fromJson(json);
      expect(back.artifact!.data, isEmpty);
      expect(back.artifact!.filename, 'a.png');
      expect(back.artifact!.bytes, pngBytes.length);
      expect(
        back.artifact!.truncated,
        isTrue,
        reason: 'the cached row must admit its content is gone',
      );
    });

    test('an artifact decoded from a loosely-typed map still parses', () {
      // A cache entry read back through a generic JSON layer can arrive as
      // Map<dynamic, dynamic> rather than Map<String, dynamic>; the row must
      // still decode instead of silently losing its artifact.
      final loose = <String, dynamic>{
        'kind': 'artifact',
        'artifact': <dynamic, dynamic>{
          'filename': 'loose.png',
          'mime': 'image/png',
          'bytes': 12,
        },
      };
      final back = ChatItem.fromJson(loose);
      expect(back.kind, ChatItemKind.artifact);
      expect(back.artifact?.filename, 'loose.png');
      expect(back.artifact?.bytes, 12);
    });

    test(
      'an unknown cached kind falls back to system rather than throwing',
      () {
        final back = ChatItem.fromJson({
          'kind': 'from-a-future-build',
          'text': 'x',
        });
        expect(back.kind, ChatItemKind.system);
        expect(back.text, 'x');
      },
    );

    test('a cached retryAt is parsed, and a malformed one is ignored', () {
      final ok = ChatItem.fromJson({
        'kind': 'system',
        'retryAt': '2026-08-25T10:00:00Z',
      });
      expect(ok.retryAt, isNotNull);
      final bad = ChatItem.fromJson({
        'kind': 'system',
        'retryAt': 'not a date',
      });
      expect(bad.retryAt, isNull);
    });

    test('a url artifact round-trips without becoming truncated', () {
      const item = ChatItem(
        kind: ChatItemKind.artifact,
        artifact: ArtifactInfo(
          filename: 'a.png',
          url: 'https://example.com/a.png',
        ),
      );
      final back = ChatItem.fromJson(item.toJson());
      expect(back.artifact!.url, 'https://example.com/a.png');
      expect(back.artifact!.truncated, isFalse);
      expect(back.artifact!.isOpenable, isTrue);
    });
  });

  group('classification', () {
    test('an inline image previews; other inline data does not', () {
      const img = ArtifactInfo(mime: 'image/png', data: 'AAAA');
      expect(img.isInlineImage, isTrue);
      const pdf = ArtifactInfo(mime: 'application/pdf', data: 'AAAA');
      expect(pdf.isInlineImage, isFalse);
      const noData = ArtifactInfo(mime: 'image/png');
      expect(noData.isInlineImage, isFalse);
    });

    test('a truncated artifact is never openable', () {
      const t = ArtifactInfo(url: 'https://example.com/a.png', truncated: true);
      expect(t.isOpenable, isFalse);
      const ok = ArtifactInfo(url: 'https://example.com/a.png');
      expect(ok.isOpenable, isTrue);
      const none = ArtifactInfo(filename: 'a.png');
      expect(none.isOpenable, isFalse);
    });
  });

  group('rendering', () {
    testWidgets('an inline image renders a preview', (tester) async {
      await pumpItem(
        tester,
        ChatItem(
          kind: ChatItemKind.artifact,
          artifact: ArtifactInfo(
            filename: 'a.png',
            mime: 'image/png',
            bytes: pngBytes.length,
            data: base64Encode(pngBytes),
          ),
        ),
      );
      expect(find.byKey(const ValueKey('artifact-image')), findsOneWidget);
      expect(find.text('a.png'), findsOneWidget);
    });

    testWidgets('a non-image renders a metadata card without a preview', (
      tester,
    ) async {
      await pumpItem(
        tester,
        const ChatItem(
          kind: ChatItemKind.artifact,
          artifact: ArtifactInfo(
            filename: 'report.pdf',
            mime: 'application/pdf',
            bytes: 2048,
          ),
        ),
      );
      expect(find.byKey(const ValueKey('artifact-card')), findsOneWidget);
      expect(find.byKey(const ValueKey('artifact-image')), findsNothing);
      expect(find.text('report.pdf'), findsOneWidget);
      expect(find.textContaining('2 KB'), findsOneWidget);
    });

    testWidgets('a truncated artifact says so and offers no open action', (
      tester,
    ) async {
      await pumpItem(
        tester,
        const ChatItem(
          kind: ChatItemKind.artifact,
          artifact: ArtifactInfo(
            filename: 'huge.bin',
            mime: 'application/octet-stream',
            bytes: 900000,
            truncated: true,
          ),
        ),
      );
      expect(find.textContaining('content unavailable'), findsOneWidget);
      expect(
        find.byKey(const ValueKey('artifact-open')),
        findsNothing,
        reason: 'a tap that cannot work must not be offered',
      );
    });

    testWidgets('an https artifact opens through the OS handler', (
      tester,
    ) async {
      String? opened;
      debugOpenArtifactUrl = (url) async {
        opened = url;
        return true;
      };
      await pumpItem(
        tester,
        const ChatItem(
          kind: ChatItemKind.artifact,
          artifact: ArtifactInfo(
            filename: 'a.png',
            url: 'https://example.com/a.png',
          ),
        ),
      );
      await tester.tap(find.byKey(const ValueKey('artifact-open')));
      await tester.pumpAndSettle();
      expect(opened, 'https://example.com/a.png');
    });

    test('a non-https url is refused even if it reaches the opener', () async {
      var called = false;
      debugOpenArtifactUrl = (_) async {
        called = true;
        return true;
      };
      await openArtifactUrl('http://example.com/a.png');
      await openArtifactUrl('file:///etc/passwd');
      await openArtifactUrl('not a url at all ::::');
      expect(
        called,
        isFalse,
        reason: 'the opener re-checks the scheme it is handed',
      );
      await openArtifactUrl('https://example.com/a.png');
      expect(called, isTrue);
    });
  });
}

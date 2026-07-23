import 'package:flutter/material.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/data/chat/streaming_markdown.dart';
import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

/// Minimal client: chat only calls sessionHistory() at open, and only when the
/// transcript is empty. Tests seed non-empty transcripts, so it stays unused.
class _FakeClient extends McremoteClient {
  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async =>
      const [];

  @override
  Future<void> prompt(String sessionId, String text) async {}
}

/// Mutable transcript source so tests can push chunk/append updates through
/// the same provider the screen watches.
class _TranscriptCtl extends Notifier<SessionTranscript> {
  _TranscriptCtl(this._initial);

  final SessionTranscript _initial;

  @override
  SessionTranscript build() => _initial;

  void set(SessionTranscript t) => state = t;
}

typedef _Ctl = NotifierProvider<_TranscriptCtl, SessionTranscript>;

_Ctl _ctl(SessionTranscript initial) => _Ctl(() => _TranscriptCtl(initial));

SessionTranscript _seeded(List<ChatItem> items, {String status = 'idle'}) {
  // Assign monotonic seqs like the reducer does, so widget keys are stable.
  final withSeq = [
    for (var i = 0; i < items.length; i++) items[i].copyWith(seq: i),
  ];
  return SessionTranscript(
    sessionId: 's1',
    status: status,
    items: withSeq,
    nextSeq: items.length,
  );
}

Widget _host(_Ctl ctl, {Brightness? brightness}) {
  return ProviderScope(
    overrides: [
      mcremoteClientProvider.overrideWithValue(_FakeClient()),
      connectionStateProvider.overrideWith(
        (ref) => Stream.value(McConnectionState.connected),
      ),
      sessionTranscriptProvider('s1').overrideWith((ref) => ref.watch(ctl)),
    ],
    child: MaterialApp(
      theme: brightness == null ? null : ThemeData(brightness: brightness),
      home: const ChatScreen(sessionId: 's1'),
    ),
  );
}

void main() {
  group('bufferStreamingMarkdown', () {
    test('passes through fully-closed markdown unchanged', () {
      const s = 'Here is **bold** and `code` and done.';
      expect(bufferStreamingMarkdown(s), s);
    });

    test('auto-closes an unclosed bold marker', () {
      expect(bufferStreamingMarkdown('start **bol'), 'start **bol**');
      expect(bufferStreamingMarkdown('a **b** then **c'), 'a **b** then **c**');
    });

    test('auto-closes an unclosed inline code span', () {
      expect(bufferStreamingMarkdown('run `npm ru'), 'run `npm ru`');
      expect(bufferStreamingMarkdown('`a` and `b'), '`a` and `b`');
    });

    test('auto-closes an unclosed fenced code block so it streams', () {
      expect(
        bufferStreamingMarkdown('intro\n```dart\nvoid main() {'),
        'intro\n```dart\nvoid main() {\n```',
      );
    });

    test('keeps a closed fenced code block', () {
      const s = 'x\n```\ncode\n```\ny';
      expect(bufferStreamingMarkdown(s), s);
    });

    test('does not parse markers inside a closed code fence', () {
      // The ** inside code is literal and balanced-agnostic; nothing changes.
      const s = '```\na ** b\n```';
      expect(bufferStreamingMarkdown(s), s);
    });

    test('empty and marker-free strings are returned as-is', () {
      expect(bufferStreamingMarkdown(''), '');
      expect(bufferStreamingMarkdown('plain text'), 'plain text');
    });

    test('closes nested open markers innermost-first', () {
      // Bold opens first, inline code second: the code span closes before
      // the bold so nesting stays well-formed.
      expect(bufferStreamingMarkdown('a **b `c'), 'a **b `c`**');
    });

    test('closes a fence opened inside an open bold span', () {
      expect(
        bufferStreamingMarkdown('**note\n```\nx'),
        '**note\n```\nx\n```**',
      );
    });
  });

  group('assistant markdown parse count (MADR 0018)', () {
    setUp(() {
      debugMarkdownParseCount = 0;
    });

    testWidgets('finalized reply does not re-parse on unrelated rebuilds', (
      tester,
    ) async {
      final ctl = _ctl(
        _seeded([
          ChatItem.assistant('# Title\n\nSome **bold** body'),
          ChatItem.tool(id: 't1', name: 'Shell', status: 'running'),
        ]),
      );
      await tester.pumpWidget(_host(ctl));
      await tester.pumpAndSettle();

      expect(debugMarkdownParseCount, 1);

      // Unrelated change: the sibling tool item finishes. The assistant item
      // instance, seq key, and row position are untouched, so its cached
      // parse must be reused across the transcript rebuild.
      final container = ProviderScope.containerOf(
        tester.element(find.byType(ChatScreen)),
      );
      final t = container.read(ctl);
      container.read(ctl.notifier).set(
        t.copyWith(
          items: [
            t.items.first,
            t.items.last.copyWith(toolStatus: 'completed'),
          ],
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('COMPLETED'), findsOneWidget);
      expect(debugMarkdownParseCount, 1);
    });

    testWidgets('appending a row does not remount and re-parse earlier ones', (
      tester,
    ) async {
      final ctl = _ctl(_seeded([ChatItem.assistant('# Title\n\nfinal body')]));
      await tester.pumpWidget(_host(ctl));
      await tester.pumpAndSettle();

      expect(debugMarkdownParseCount, 1);

      // A length change shifts every position in the reverse list; only the
      // sliver keys (which must sit on the outermost row widget for the
      // delegate to see them) keep the assistant row's Element — and its
      // cached parse — alive.
      final container = ProviderScope.containerOf(
        tester.element(find.byType(ChatScreen)),
      );
      final t = container.read(ctl);
      container.read(ctl.notifier).set(
        t.copyWith(
          items: [...t.items, ChatItem.user('follow-up').copyWith(seq: t.nextSeq)],
          nextSeq: t.nextSeq + 1,
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('follow-up'), findsOneWidget);
      expect(debugMarkdownParseCount, 1);
    });

    testWidgets('streaming chunks below the throttle window coalesce into '
        'one parse', (tester) async {
      final ctl = _ctl(
        _seeded([ChatItem.assistant('chunk 0')], status: 'running'),
      );
      await tester.pumpWidget(_host(ctl));
      // Bounded pumps: the RUNNING status chip and caret animate forever.
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 16));

      expect(debugMarkdownParseCount, 1);

      final container = ProviderScope.containerOf(
        tester.element(find.byType(ChatScreen)),
      );
      // Three chunk appends inside one 120 ms throttle window.
      for (var i = 1; i <= 3; i++) {
        final t = container.read(ctl);
        final grown = t.items.last.copyWith(
          text: '${t.items.last.text} chunk $i',
        );
        container.read(ctl.notifier).set(t.copyWith(items: [grown]));
        await tester.pump(const Duration(milliseconds: 16));
      }
      // No per-chunk parse: still only the initial one.
      expect(debugMarkdownParseCount, 1);

      // Once the window elapses, exactly one coalesced parse shows the
      // latest text.
      await tester.pump(const Duration(milliseconds: 200));
      expect(debugMarkdownParseCount, 2);
      expect(
        tester.widget<MarkdownBody>(find.byType(MarkdownBody)).data,
        contains('chunk 3'),
      );
    });
  });

  group('long plain streaming path (MADR 0018 B3)', () {
    setUp(() {
      debugMarkdownParseCount = 0;
    });

    testWidgets('shows raw text without synthetic closers, and a theme flip '
        'does not defeat the render cache', (tester) async {
      // Open code fence past kMaxStreamingMarkdownChars: running
      // bufferStreamingMarkdown here would append a literal ``` closer to the
      // plain SelectableText.
      final text = 'intro\n```dart\n${'code line\n' * 500}';
      assert(text.length > kMaxStreamingMarkdownChars);
      final ctl = _ctl(_seeded([ChatItem.assistant(text)], status: 'running'));
      await tester.pumpWidget(_host(ctl, brightness: Brightness.light));
      // Bounded pumps: the RUNNING status chip and caret animate forever.
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 16));

      // Plain path: no markdown parse, and the shown text is exactly the
      // stream so far — no auto-close markers.
      expect(debugMarkdownParseCount, 0);
      expect(find.byType(MarkdownBody), findsNothing);
      expect(
        tester.widget<SelectableText>(find.byType(SelectableText)).data,
        text,
      );

      // Theme flip mid-stream: one re-render with the new theme...
      await tester.pumpWidget(_host(ctl, brightness: Brightness.dark));
      await tester.pump(const Duration(milliseconds: 16));

      // ...after which unrelated transcript rebuilds must reuse the cached
      // subtree instead of re-rendering every frame until the turn ends.
      final container = ProviderScope.containerOf(
        tester.element(find.byType(ChatScreen)),
      );
      var t = container.read(ctl);
      container.read(ctl.notifier).set(
        t.copyWith(
          items: [
            ...t.items,
            ChatItem.tool(
              id: 't1',
              name: 'Shell',
              status: 'running',
            ).copyWith(seq: t.nextSeq),
          ],
          nextSeq: t.nextSeq + 1,
        ),
      );
      await tester.pump(const Duration(milliseconds: 16));
      final w1 = tester.widget<SelectableText>(find.byType(SelectableText));

      t = container.read(ctl);
      container.read(ctl.notifier).set(
        t.copyWith(
          items: [
            t.items.first,
            t.items.last.copyWith(toolStatus: 'completed'),
          ],
        ),
      );
      await tester.pump(const Duration(milliseconds: 16));
      final w2 = tester.widget<SelectableText>(find.byType(SelectableText));

      expect(find.text('COMPLETED'), findsOneWidget);
      expect(
        identical(w1, w2),
        isTrue,
        reason: 'stale brightness bookkeeping would re-run _render on every '
            'parent rebuild after a theme flip',
      );
      expect(debugMarkdownParseCount, 0);
    });
  });

  group('limit notice refresh', () {
    testWidgets('re-renders each minute so the reset phrasing cannot go stale',
        (tester) async {
      final ctl = _ctl(
        _seeded([
          ChatItem.system(
            'Limit hit',
            error: true,
            errorKind: 'quota',
            retryAt: DateTime.now().add(const Duration(hours: 2)),
          ),
        ]),
      );
      await tester.pumpWidget(_host(ctl));
      await tester.pumpAndSettle();

      final before = tester.widget<Text>(
        find.textContaining('The limit resets at'),
      );
      await tester.pump(const Duration(minutes: 1, seconds: 1));
      final after = tester.widget<Text>(
        find.textContaining('The limit resets at'),
      );
      expect(
        identical(before, after),
        isFalse,
        reason: 'the card must rebuild when the minute timer fires',
      );

      // Dispose the card so its periodic timer is cancelled before teardown.
      await tester.pumpWidget(const SizedBox());
    });

    testWidgets('stops refreshing once the reset time has passed', (
      tester,
    ) async {
      final ctl = _ctl(
        _seeded([
          ChatItem.system(
            'Limit hit',
            error: true,
            errorKind: 'quota',
            retryAt: DateTime.now().subtract(const Duration(seconds: 1)),
          ),
        ]),
      );
      await tester.pumpWidget(_host(ctl));
      await tester.pumpAndSettle();
      expect(find.textContaining('now — try again'), findsOneWidget);

      // The first tick delivers one final rebuild and cancels the timer; the
      // card then stays inert (teardown would flag a still-pending timer).
      await tester.pump(const Duration(minutes: 1, seconds: 1));
      final w1 = tester.widget<Text>(
        find.textContaining('The limit resets at'),
      );
      await tester.pump(const Duration(minutes: 5));
      final w2 = tester.widget<Text>(
        find.textContaining('The limit resets at'),
      );
      expect(identical(w1, w2), isTrue);
    });
  });

  group('Show more clamp (E3)', () {
    setUp(() {
      debugMarkdownParseCount = 0;
    });

    testWidgets('a cut inside a code fence is closed, with the marker outside',
        (tester) async {
      final text = 'intro\n\n```dart\n${'code line\n' * 800}```\ntail';
      assert(text.length > kAssistantShowMoreChars);
      final ctl = _ctl(_seeded([ChatItem.assistant(text)]));
      await tester.pumpWidget(_host(ctl));
      await tester.pumpAndSettle();

      final body = tester.widget<MarkdownBody>(find.byType(MarkdownBody));
      final clamped = text.substring(0, kAssistantShowMoreChars);
      // The open fence gets a synthetic closer; the "…" marker lands on its
      // own paragraph after it, never as trailing text on the fence line.
      expect(body.data, '${bufferStreamingMarkdown(clamped)}\n\n…');
      expect(body.data, endsWith('\n```\n\n…'));

      // Expanding renders the full, unmodified text.
      await tester.tap(find.text('Show more'));
      await tester.pumpAndSettle();
      expect(
        tester.widget<MarkdownBody>(find.byType(MarkdownBody)).data,
        text,
      );
    });

    testWidgets('the clamp never splits a UTF-16 surrogate pair', (
      tester,
    ) async {
      // The emoji spans code units 5999-6000, so a naive cut at
      // kAssistantShowMoreChars would split its surrogate pair.
      final prefix = 'a' * (kAssistantShowMoreChars - 1);
      final text = '$prefix\u{1F600}${'b' * 100}';
      final ctl = _ctl(_seeded([ChatItem.assistant(text)]));
      await tester.pumpWidget(_host(ctl));
      await tester.pumpAndSettle();

      final body = tester.widget<MarkdownBody>(find.byType(MarkdownBody));
      expect(body.data, '$prefix…');
    });
  });
}

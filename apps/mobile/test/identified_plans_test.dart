import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/features/chat/chat_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';

// PLAN 0192 P2: an ACP agent's identified plans are kept by ID beside the
// stable plan, drawn above the composer in first-seen order, and a removal
// drops only its own plan (MADR 0192 D4).

class _PlansClient extends McremoteClient {
  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<List<SessionEvent>> sessionHistory(
    String sessionId, {
    int limit = kHistoryFetchLimit,
  }) async => const [];

  @override
  Future<SessionListSnapshot> listSessionSnapshot() async =>
      SessionListSnapshot(
        sessions: [SessionMeta(id: 's1', provider: 'grok')],
        complete: true,
      );
}

SessionEvent _stable(List<String> tasks) => SessionEvent.fromJson({
  'type': 'plan',
  'session_id': 's1',
  'entries': [
    for (final t in tasks) {'content': t, 'status': 'pending'},
  ],
});

SessionEvent _items(String id, List<String> tasks) => SessionEvent.fromJson({
  'type': 'identified_plan',
  'session_id': 's1',
  'plan_id': id,
  'plan_kind': 'items',
  'entries': [
    for (final t in tasks) {'content': t, 'status': 'pending'},
  ],
});

SessionEvent _markdown(String id, String content, {bool truncated = false}) =>
    SessionEvent.fromJson({
      'type': 'identified_plan',
      'session_id': 's1',
      'plan_id': id,
      'plan_kind': 'markdown',
      'plan_content': content,
      if (truncated) 'plan_truncated': true,
    });

SessionEvent _file(String id, String uri) => SessionEvent.fromJson({
  'type': 'identified_plan',
  'session_id': 's1',
  'plan_id': id,
  'plan_kind': 'file',
  'plan_uri': uri,
});

SessionEvent _removed(String id) => SessionEvent.fromJson({
  'type': 'identified_plan',
  'session_id': 's1',
  'plan_id': id,
  'plan_removed': true,
});

SessionTranscript _apply(List<SessionEvent> events) {
  var t = const SessionTranscript(sessionId: 's1');
  for (final ev in events) {
    t = applySessionEvent(t, ev);
  }
  return t;
}

void main() {
  group('event', () {
    test('parses an identified plan', () {
      final ev = _markdown('p1', '# Plan', truncated: true);
      expect(ev.planId, 'p1');
      expect(ev.planKind, 'markdown');
      expect(ev.planContent, '# Plan');
      expect(ev.planTruncated, isTrue);
      expect(ev.planRemoved, isFalse);
      final file = _file('p2', 'file:///tmp/PLAN.md');
      expect(file.planUri, 'file:///tmp/PLAN.md');
      expect(_removed('p3').planRemoved, isTrue);
    });
  });

  group('reducer', () {
    test('plans are kept apart by id, in first-seen order', () {
      final t = _apply([
        _items('a', ['Read']),
        _stable(['Stable task']),
        _markdown('b', '# B'),
        _items('a', ['Read', 'Write']),
      ]);
      expect(t.planOrder, ['a', '', 'b']);
      expect(t.plan.map((e) => e.content), ['Stable task']);
      expect(t.identifiedPlans['a']!.entries.map((e) => e.content), [
        'Read',
        'Write',
      ]);
      expect(t.identifiedPlans['b']!.kind, 'markdown');
      expect(t.identifiedPlans['b']!.content, '# B');
    });

    test('a removal drops only its own plan', () {
      final t = _apply([
        _stable(['Stable task']),
        _items('a', ['Read']),
        _markdown('b', '# B'),
        _removed('a'),
      ]);
      expect(t.plan.map((e) => e.content), ['Stable task']);
      expect(t.identifiedPlans.keys, ['b']);
      expect(t.planOrder, ['', 'b']);
    });

    test('clearing the stable plan leaves the identified plans', () {
      final t = _apply([
        _stable(['Stable task']),
        _items('a', ['Read']),
        _stable([]),
      ]);
      expect(t.plan, isEmpty);
      expect(t.identifiedPlans.keys, ['a']);
      expect(t.planOrder, ['a']);
    });

    test('a plan that comes back is placed where it reappeared', () {
      final t = _apply([
        _items('a', ['Read']),
        _items('b', ['Write']),
        _removed('a'),
        _items('a', ['Read again']),
      ]);
      expect(t.planOrder, ['b', 'a']);
    });

    test(
      'an unchanged plan, an unknown removal and a missing id are no-ops',
      () {
        final t = _apply([
          _items('a', ['Read']),
        ]);
        expect(
          identical(applySessionEvent(t, _items('a', ['Read'])), t),
          isTrue,
        );
        expect(identical(applySessionEvent(t, _removed('zzz')), t), isTrue);
        final noId = SessionEvent.fromJson({
          'type': 'identified_plan',
          'session_id': 's1',
          'plan_kind': 'markdown',
          'plan_content': 'x',
        });
        expect(identical(applySessionEvent(t, noId), t), isTrue);
      },
    );
  });

  group('panel', () {
    Future<void> pump(WidgetTester tester, List<SessionEvent> events) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            mcremoteClientProvider.overrideWithValue(_PlansClient()),
            connectionStateProvider.overrideWith(
              (ref) => Stream.value(McConnectionState.connected),
            ),
            sessionTranscriptProvider('s1').overrideWithValue(_apply(events)),
          ],
          child: const MaterialApp(home: ChatScreen(sessionId: 's1')),
        ),
      );
      await tester.pump();
    }

    double top(WidgetTester tester, String text) =>
        tester.getTopLeft(find.text(text)).dy;

    testWidgets(
      'the stable plan and identified plans show in first-seen order',
      (tester) async {
        await pump(tester, [
          _items('alpha', ['Read']),
          _stable(['Stable task']),
          _markdown('beta', '# Beta plan'),
        ]);
        expect(find.text('Plan: alpha'), findsOneWidget);
        expect(find.text('Todos'), findsOneWidget);
        expect(find.text('Plan: beta'), findsOneWidget);
        expect(top(tester, 'Plan: alpha'), lessThan(top(tester, 'Todos')));
        expect(top(tester, 'Todos'), lessThan(top(tester, 'Plan: beta')));
      },
    );

    testWidgets('a removal drops only its plan', (tester) async {
      await pump(tester, [
        _stable(['Stable task']),
        _items('alpha', ['Read']),
        _markdown('beta', '# Beta plan'),
        _removed('alpha'),
      ]);
      expect(find.text('Plan: alpha'), findsNothing);
      expect(find.text('Todos'), findsOneWidget);
      expect(find.text('Plan: beta'), findsOneWidget);
    });

    testWidgets('a Markdown plan is collapsed until opened', (tester) async {
      await pump(tester, [
        _markdown('beta', 'Intro\n\n- **Extract** the parser'),
      ]);
      expect(find.textContaining('Extract'), findsNothing);
      await tester.tap(find.text('Plan: beta'));
      await tester.pumpAndSettle();
      expect(find.textContaining('Extract'), findsOneWidget);
    });

    testWidgets('a truncated Markdown plan says so', (tester) async {
      await pump(tester, [_markdown('beta', 'Long plan', truncated: true)]);
      await tester.tap(find.text('Plan: beta'));
      await tester.pumpAndSettle();
      expect(find.textContaining('cut short'), findsOneWidget);
    });

    testWidgets('a Markdown plan never loads an image', (tester) async {
      await pump(tester, [
        _markdown('beta', 'See ![the diagram](https://example.com/d.png)'),
      ]);
      await tester.tap(find.text('Plan: beta'));
      await tester.pumpAndSettle();
      expect(find.byType(Image), findsNothing);
      expect(find.textContaining('the diagram'), findsOneWidget);
    });

    testWidgets('a file plan shows its name and path, and copies the path', (
      tester,
    ) async {
      final copied = <String>[];
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        (call) async {
          if (call.method == 'Clipboard.setData') {
            copied.add((call.arguments as Map)['text'] as String);
          }
          return null;
        },
      );
      addTearDown(
        () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          SystemChannels.platform,
          null,
        ),
      );
      await pump(tester, [
        _file('gamma', 'file:///home/%3Cuser%3E/project/PLAN.md'),
      ]);
      expect(find.text('Plan: gamma'), findsOneWidget);
      expect(find.textContaining('PLAN.md'), findsWidgets);
      expect(
        find.textContaining('/home/<user>/project/PLAN.md'),
        findsOneWidget,
      );
      expect(find.byType(Image), findsNothing);

      await tester.tap(find.byTooltip('Copy path'));
      await tester.pump();
      expect(copied, ['/home/<user>/project/PLAN.md']);
    });
  });
}

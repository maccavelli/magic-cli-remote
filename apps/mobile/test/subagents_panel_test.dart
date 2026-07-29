import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/chat/chat_models.dart';
import 'package:magic_cli_remote/data/chat/transcript_reducer.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/features/widgets/subagents_panel.dart';
import 'package:magic_cli_remote/theme/celestial.dart';

/// Background sub-agents: status in a panel, output nowhere (MADR 0051 D6/D8).
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  SessionEvent subagents(List<SubagentInfo> set) =>
      SessionEvent(type: 'subagents', sessionId: 's1', subagents: set);

  const empty = SessionTranscript(sessionId: 's1');

  group('reducer', () {
    test('replaces the whole set, never appends', () {
      var t = applySessionEvent(
        empty,
        subagents(const [SubagentInfo(id: 'a', name: 'explore', task: 'look')]),
      );
      expect(t.subagents, hasLength(1));

      t = applySessionEvent(
        t,
        subagents(const [
          SubagentInfo(id: 'a', name: 'explore', task: 'look'),
          SubagentInfo(id: 'b', name: 'general', task: 'read'),
        ]),
      );
      expect(t.subagents.map((s) => s.id), ['a', 'b']);
    });

    test('an event with no entries clears the set', () {
      var t = applySessionEvent(
        empty,
        subagents(const [SubagentInfo(id: 'a', name: 'explore')]),
      );
      expect(t.subagents, isNotEmpty);

      // The trap this guards: `plan` and `subagents` treat an absent list as a
      // *clear*, while `session_mode` treats it as "keep what you have"
      // (MADR 0046 I-1). Applying the wrong rule here would leave finished
      // sub-agents pinned in the panel forever.
      t = applySessionEvent(t, subagents(const []));
      expect(t.subagents, isEmpty);
    });

    test('an unchanged set returns the identical transcript', () {
      const set = [SubagentInfo(id: 'a', name: 'explore', task: 'look')];
      final t = applySessionEvent(empty, subagents(set));
      final again = applySessionEvent(t, subagents(set));
      expect(
        identical(t, again),
        isTrue,
        reason: 'a re-sent set must not rebuild the transcript',
      );
    });

    test('a status change is not treated as unchanged', () {
      var t = applySessionEvent(
        empty,
        subagents(const [SubagentInfo(id: 'a', name: 'explore')]),
      );
      t = applySessionEvent(
        t,
        subagents(const [
          SubagentInfo(id: 'a', name: 'explore', status: 'completed'),
        ]),
      );
      expect(t.subagents.single.status, 'completed');
    });

    test('sub-agent events add nothing to the transcript', () {
      final t = applySessionEvent(
        empty,
        subagents(const [SubagentInfo(id: 'a', name: 'explore')]),
      );
      expect(
        t.items,
        isEmpty,
        reason: 'status belongs in the panel, never in the chat stream',
      );
    });
  });

  group('panel', () {
    Future<void> pump(WidgetTester tester, List<SubagentInfo> entries) =>
        tester.pumpWidget(
          MaterialApp(
            theme: celestialDark,
            home: Scaffold(body: SubagentsPanel(entries: entries)),
          ),
        );

    testWidgets('summarises how many are running', (tester) async {
      await pump(tester, const [
        SubagentInfo(id: 'a', name: 'explore', task: 'look'),
        SubagentInfo(id: 'b', name: 'general', status: 'completed'),
      ]);
      expect(find.text('Sub-agents'), findsOneWidget);
      expect(find.text('1 running'), findsOneWidget);
    });

    testWidgets('expanding lists each agent with its task', (tester) async {
      await pump(tester, const [
        SubagentInfo(id: 'a', name: 'explore', task: 'read a.txt and b.txt'),
      ]);
      expect(find.text('read a.txt and b.txt'), findsNothing);

      await tester.tap(find.text('Sub-agents'));
      await tester.pumpAndSettle();
      expect(find.text('explore'), findsOneWidget);
      expect(find.text('read a.txt and b.txt'), findsOneWidget);
    });

    testWidgets('status drives the row icon', (tester) async {
      await pump(tester, const [
        SubagentInfo(id: 'a', name: 'explore'),
        SubagentInfo(id: 'b', name: 'general', status: 'completed'),
        SubagentInfo(id: 'c', name: 'other', status: 'failed'),
      ]);
      await tester.tap(find.text('Sub-agents'));
      await tester.pumpAndSettle();

      expect(find.byIcon(Icons.autorenew), findsOneWidget);
      expect(find.byIcon(Icons.check_circle), findsOneWidget);
      expect(find.byIcon(Icons.error_outline), findsOneWidget);
    });
  });
}

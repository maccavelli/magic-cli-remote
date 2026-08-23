import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/features/sessions/sessions_screen.dart';

/// MADR 0112 A1 — the additive discovery models.
///
/// These cover the decode contract the daemon promises: every new field is
/// optional, an older daemon that omits them must still produce a usable row,
/// and "unknown cost" must never be flattened into "free".
void main() {
  group('ProjectMeta', () {
    test('decodes a full row', () {
      final p = ProjectMeta.fromJson({
        'id': 'abc123',
        'name': 'magic-cli-remote',
        'worktree': '/work/repo',
      });
      expect(p.id, 'abc123');
      expect(p.name, 'magic-cli-remote');
      expect(p.worktree, '/work/repo');
      expect(p.displayName, 'magic-cli-remote');
    });

    test('falls back to the id when the daemon sent no name', () {
      final p = ProjectMeta.fromJson({
        'id': 'abc123',
        'worktree': '/work/repo',
      });
      expect(p.name, isEmpty);
      expect(p.displayName, 'abc123');
    });

    test('tolerates a malformed row without throwing', () {
      final p = ProjectMeta.fromJson({'id': 42, 'worktree': null});
      expect(p.id, isEmpty);
      expect(p.worktree, isEmpty);
    });

    test('ignores unknown fields so a newer daemon stays compatible', () {
      final p = ProjectMeta.fromJson({
        'id': 'a',
        'worktree': '/w',
        'some_future_field': {'nested': true},
      });
      expect(p.id, 'a');
      expect(p.worktree, '/w');
    });
  });

  group('AgentSessionUsage', () {
    test('decodes all buckets and a cost', () {
      final u = AgentSessionUsage.fromJson({
        'input': 1200,
        'output': 340,
        'reasoning': 10,
        'cache_read': 800,
        'cache_write': 5,
        'cost_usd': 0.25,
      });
      expect(u.input, 1200);
      expect(u.output, 340);
      expect(u.reasoning, 10);
      expect(u.cacheRead, 800);
      expect(u.cacheWrite, 5);
      expect(u.costUsd, 0.25);
      expect(u.totalTokens, 2355);
    });

    test('a reported zero cost stays zero, not null', () {
      final u = AgentSessionUsage.fromJson({'cost_usd': 0});
      expect(u.costUsd, 0);
      expect(u.costUsd, isNotNull);
    });

    test('an absent cost stays null so it can render as unknown', () {
      final u = AgentSessionUsage.fromJson({'input': 5});
      expect(u.costUsd, isNull);
    });

    test('drops values that cannot be rendered honestly', () {
      final negative = AgentSessionUsage.fromJson({
        'input': -5,
        'cost_usd': -1.0,
      });
      // A negative token count would render as a nonsense total, and a negative
      // cost as a credit the user does not have.
      expect(negative.input, 0);
      expect(negative.costUsd, isNull);

      final nan = AgentSessionUsage.fromJson({'cost_usd': double.nan});
      expect(nan.costUsd, isNull);

      final infinite = AgentSessionUsage.fromJson({
        'cost_usd': double.infinity,
      });
      expect(infinite.costUsd, isNull);
    });

    test('truncates fractional token counts', () {
      final u = AgentSessionUsage.fromJson({'input': 10.9});
      expect(u.input, 10);
    });

    test('a wrongly typed field falls back to zero rather than throwing', () {
      final u = AgentSessionUsage.fromJson({
        'input': 'lots',
        'cost_usd': 'free',
      });
      expect(u.input, 0);
      expect(u.costUsd, isNull);
    });
  });

  group('AgentSessionMeta additive fields', () {
    test('a legacy payload still decodes', () {
      final m = AgentSessionMeta.fromJson({
        'id': 'ses_1',
        'cwd': '/w',
        'title': 'Refactor',
        'updated_at': '2026-07-26T20:52:14Z',
      });
      expect(m.id, 'ses_1');
      expect(m.displayName, 'Refactor');
      expect(m.updatedAt, isNotNull);
      // Everything additive stays empty rather than being invented.
      expect(m.modelId, isEmpty);
      expect(m.thinkingLevel, isEmpty);
      expect(m.agent, isEmpty);
      expect(m.aggregate, isNull);
    });

    test('decodes the additive fields when present', () {
      final m = AgentSessionMeta.fromJson({
        'id': 'ses_1',
        'model_id': 'opencode/big-pickle',
        'thinking_level': 'high',
        'agent': 'build',
        'aggregate': {'input': 10, 'cost_usd': 0.0},
      });
      expect(m.modelId, 'opencode/big-pickle');
      expect(m.thinkingLevel, 'high');
      expect(m.agent, 'build');
      expect(m.aggregate, isNotNull);
      expect(m.aggregate!.costUsd, 0);
    });

    test('a non-map aggregate is ignored rather than crashing the picker', () {
      final m = AgentSessionMeta.fromJson({'id': 'a', 'aggregate': 'lots'});
      expect(m.aggregate, isNull);
    });

    test('falls back to the id when there is no title', () {
      final m = AgentSessionMeta.fromJson({'id': 'ses_1'});
      expect(m.displayName, 'ses_1');
    });
  });

  group('ProjectPickerList', () {
    Future<void> pump(
      WidgetTester tester,
      List<ProjectMeta> projects,
      void Function(String) onSelected,
    ) => tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ProjectPickerList(projects: projects, onSelected: onSelected),
        ),
      ),
    );

    testWidgets('renders a row per project with its worktree', (tester) async {
      await pump(tester, const [
        ProjectMeta(id: 'p1', name: 'repo', worktree: '/work/repo'),
        ProjectMeta(id: 'p2', name: 'other', worktree: '/work/other'),
      ], (_) {});

      expect(find.byType(ListTile), findsNWidgets(2));
      expect(find.text('repo'), findsOneWidget);
      expect(find.text('/work/repo'), findsOneWidget);
      expect(find.text('other'), findsOneWidget);
    });

    testWidgets('reports the chosen worktree', (tester) async {
      final chosen = <String>[];
      await pump(tester, const [
        ProjectMeta(id: 'p1', name: 'repo', worktree: '/work/repo'),
      ], chosen.add);

      await tester.tap(find.text('repo'));
      await tester.pumpAndSettle();

      expect(chosen, ['/work/repo']);
    });

    testWidgets('an unnamed project still renders a usable label', (
      tester,
    ) async {
      await pump(tester, const [
        ProjectMeta(id: 'p1', worktree: '/work/repo'),
      ], (_) {});

      // displayName falls back to the id rather than showing an empty row.
      expect(find.text('p1'), findsOneWidget);
    });

    testWidgets('an empty catalog explains itself instead of showing nothing', (
      tester,
    ) async {
      await pump(tester, const [], (_) {});

      expect(find.byType(ListTile), findsNothing);
      expect(find.textContaining('no known projects'), findsOneWidget);
      expect(find.textContaining('manually'), findsOneWidget);
    });
  });

  group('sessionUsageLabel', () {
    test('reports tokens and a real cost', () {
      final label = sessionUsageLabel(
        const AgentSessionUsage(input: 10, output: 5, costUsd: 0.1234),
      );
      expect(label, contains('15 tokens'));
      expect(label, contains('0.1234'));
    });

    test('a known-zero cost reads as free, never as a price', () {
      final label = sessionUsageLabel(
        const AgentSessionUsage(input: 10, costUsd: 0),
      );
      expect(label, contains('free'));
      expect(label, isNot(contains('\$0')));
    });

    test('an unreported cost is omitted rather than shown as free', () {
      final label = sessionUsageLabel(const AgentSessionUsage(input: 10));
      expect(label, contains('10 tokens'));
      expect(label, isNot(contains('free')));
    });

    test('an entirely empty aggregate says so', () {
      expect(sessionUsageLabel(const AgentSessionUsage()), 'No usage recorded');
    });
  });
}

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/skill_authoring_sheet.dart';

/// Agent-mediated skill authoring (MADR 0112 A10, PLAN P7 steps 3 and 4).
///
/// The load-bearing property is that this composes *text*. mcremote has no
/// skill writer, so every guarantee here is about what the prompt says, and the
/// negative assertions matter as much as the positive ones.
void main() {
  group('validation', () {
    test('accepts lowercase kebab-case names', () {
      for (final ok in ['a', 'review', 'review-checklist', 'go-1-2-3', 'x1']) {
        expect(
          validateSkillRequest(name: ok, description: 'd', intent: ''),
          isNull,
          reason: ok,
        );
      }
    });

    test('rejects names that could become a path', () {
      for (final bad in [
        '../escape',
        'a/b',
        r'a\b',
        'a.b',
        'a_b',
        'Review',
        'review--checklist',
        '-leading',
        'trailing-',
        'has space',
        'ünicode',
      ]) {
        expect(
          validateSkillRequest(name: bad, description: 'd', intent: ''),
          isNotNull,
          reason: '$bad must not be accepted as a directory segment',
        );
      }
    });

    test('requires a name and a description', () {
      expect(
        validateSkillRequest(name: '', description: 'd', intent: ''),
        contains('name'),
      );
      expect(
        validateSkillRequest(name: 'ok', description: '   ', intent: ''),
        contains('description'),
      );
    });

    test('enforces every length bound', () {
      expect(
        validateSkillRequest(
          name: 'a' * (kSkillNameMaxLen + 1),
          description: 'd',
          intent: '',
        ),
        isNotNull,
      );
      expect(
        validateSkillRequest(
          name: 'ok',
          description: 'd' * (kSkillDescriptionMaxLen + 1),
          intent: '',
        ),
        isNotNull,
      );
      expect(
        validateSkillRequest(
          name: 'ok',
          description: 'd',
          intent: 'i' * (kSkillIntentMaxLen + 1),
        ),
        isNotNull,
      );
    });

    test('accepts values exactly at each bound', () {
      expect(
        validateSkillRequest(
          name: 'a' * kSkillNameMaxLen,
          description: 'd' * kSkillDescriptionMaxLen,
          intent: 'i' * kSkillIntentMaxLen,
        ),
        isNull,
      );
    });
  });

  group('prompt composition', () {
    String prompt({
      String name = 'review-checklist',
      String description = 'Checks a diff',
      String intent = '',
    }) => composeSkillPrompt(
      name: name,
      description: description,
      intent: intent,
    );

    test('names the built-in skill and the project-local path', () {
      final p = prompt();
      expect(p, contains('customize-opencode'));
      expect(p, contains('.opencode/skills/review-checklist/SKILL.md'));
      expect(p, contains('current worktree'));
    });

    test('pins the frontmatter to the validated values', () {
      final p = prompt();
      expect(p, contains('`name` must be exactly `review-checklist`'));
      expect(p, contains('`description` must be exactly: Checks a diff'));
    });

    test('asks to preserve an existing skill', () {
      // A phone user cannot see what is already there, so replacing by default
      // would be destructive.
      expect(prompt(), contains('preserve its current content'));
    });

    test('forbids a global or home location', () {
      final p = prompt().toLowerCase();
      expect(p, contains('do not use a global or home-directory location'));
      expect(p, isNot(contains('~/.config')));
      expect(p, isNot(contains(r'$home')));
    });

    test('includes optional detail only when given', () {
      expect(prompt(), isNot(contains('What the skill should cover')));
      final withIntent = prompt(intent: 'Cover error handling.');
      expect(withIntent, contains('What the skill should cover'));
      expect(withIntent, contains('Cover error handling.'));
    });

    test('asks for the resulting path to be reported', () {
      expect(prompt(), contains('report the exact path'));
    });

    test('never instructs a permission bypass or a raw write', () {
      final p = prompt(intent: 'anything').toLowerCase();
      for (final forbidden in [
        'bypass',
        'without asking',
        'skip permission',
        'auto-approve',
        'sudo',
        'force',
      ]) {
        expect(
          p,
          isNot(contains(forbidden)),
          reason: '$forbidden would undermine OpenCode’s own permission rules',
        );
      }
    });

    test('trims incidental whitespace from every field', () {
      final p = composeSkillPrompt(
        name: '  spaced-name  ',
        description: '  A description  ',
        intent: '   ',
      );
      expect(p, contains('.opencode/skills/spaced-name/SKILL.md'));
      expect(p, contains('exactly: A description'));
      expect(p, isNot(contains('What the skill should cover')));
    });
  });

  group('sheet', () {
    Future<String?> composeVia(
      WidgetTester tester, {
      required String name,
      required String description,
      String intent = '',
    }) async {
      String? composed;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SkillAuthoringSheet(onCompose: (p) => composed = p),
          ),
        ),
      );
      await tester.enterText(find.byKey(const ValueKey('skill-name')), name);
      await tester.enterText(
        find.byKey(const ValueKey('skill-description')),
        description,
      );
      if (intent.isNotEmpty) {
        await tester.enterText(
          find.byKey(const ValueKey('skill-intent')),
          intent,
        );
      }
      await tester.tap(find.byKey(const ValueKey('skill-compose')));
      await tester.pumpAndSettle();
      return composed;
    }

    testWidgets('a valid request composes a prompt', (tester) async {
      final composed = await composeVia(
        tester,
        name: 'review-checklist',
        description: 'Checks a diff',
        intent: 'Cover error handling.',
      );
      expect(composed, isNotNull);
      expect(composed, contains('.opencode/skills/review-checklist/SKILL.md'));
      expect(composed, contains('Cover error handling.'));
    });

    testWidgets('an invalid name is refused with an explanation', (
      tester,
    ) async {
      final composed = await composeVia(
        tester,
        name: '../escape',
        description: 'Checks a diff',
      );
      expect(composed, isNull, reason: 'nothing may be composed from it');
      final error = tester.widget<Text>(
        find.byKey(const ValueKey('skill-error')),
      );
      expect(error.data, contains('lowercase'));
    });

    testWidgets('a missing description is refused', (tester) async {
      final composed = await composeVia(
        tester,
        name: 'ok-name',
        description: '',
      );
      expect(composed, isNull);
      expect(find.byKey(const ValueKey('skill-error')), findsOneWidget);
    });

    testWidgets('the sheet has no send or write affordance', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(body: SkillAuthoringSheet(onCompose: (_) {})),
        ),
      );
      // It composes text for the ordinary composer; it must not look like it
      // writes a file or sends on its own.
      for (final label in ['Send', 'Write', 'Save', 'Apply', 'Create file']) {
        expect(find.text(label), findsNothing, reason: label);
      }
      expect(find.text('Compose message'), findsOneWidget);
    });
  });
}

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/features/chat/question_sheet.dart';
import 'package:magic_cli_remote/theme/celestial.dart';

void main() {
  test('question models preserve ids, descriptions, Other, and secret', () {
    final item = QuestionItem.fromJson({
      'id': 'token',
      'header': 'Authentication',
      'text': 'Choose or enter a value',
      'custom': true,
      'secret': true,
      'options': [
        {
          'option_id': 'saved',
          'name': 'Saved token',
          'description': 'Use the token already on the host',
        },
      ],
    });
    expect(item.id, 'token');
    expect(item.custom, isTrue);
    expect(item.secret, isTrue);
    expect(
      item.options.single.description,
      'Use the token already on the host',
    );
  });

  testWidgets('generic form validates nested fields and submits keyed once', (
    tester,
  ) async {
    final submissions = <Map<String, List<String>>>[];
    var cancellations = 0;
    await tester.pumpWidget(
      MaterialApp(
        theme: celestialLight,
        home: Scaffold(
          body: QuestionSheet(
            title: 'Agent question',
            items: [
              QuestionItem(
                id: 'features',
                header: 'Features',
                text: 'Choose several',
                multiple: true,
                options: [
                  PermissionOption(
                    optionId: 'core',
                    name: 'Core',
                    description: 'Required runtime',
                  ),
                  PermissionOption(optionId: 'cli', name: 'CLI'),
                ],
              ),
              QuestionItem(
                id: 'other',
                header: 'Other',
                text: 'Add a custom value',
                custom: true,
              ),
              QuestionItem(
                id: 'token',
                header: 'Token',
                text: 'Enter the secret',
                secret: true,
              ),
            ],
            onSubmit: submissions.add,
            onCancel: () => cancellations++,
          ),
        ),
      ),
    );

    expect(find.text('Required runtime'), findsOneWidget);
    expect(
      tester
          .widget<FilledButton>(find.byKey(const Key('question-submit')))
          .onPressed,
      isNull,
    );
    expect(
      tester
          .widget<TextField>(find.byKey(const Key('question-text-2')))
          .obscureText,
      isTrue,
    );

    await tester.tap(find.byKey(const Key('question-option-0-core')));
    await tester.tap(find.byKey(const Key('question-option-0-cli')));
    await tester.enterText(find.byKey(const Key('question-text-1')), 'docs');
    await tester.enterText(
      find.byKey(const Key('question-text-2')),
      'P2-SECRET-MUST-NEVER-PERSIST',
    );
    await tester.pump();
    await tester.ensureVisible(find.byKey(const Key('question-submit')));
    await tester.tap(find.byKey(const Key('question-submit')));
    await tester.tap(find.byKey(const Key('question-submit')));
    await tester.pump();

    expect(submissions, hasLength(1));
    expect(submissions.single['features'], ['core', 'cli']);
    expect(submissions.single['other'], ['docs']);
    expect(submissions.single['token'], ['P2-SECRET-MUST-NEVER-PERSIST']);
    expect(cancellations, 0);
    expect(
      tester
          .widget<TextField>(find.byKey(const Key('question-text-2')))
          .controller!
          .text,
      isEmpty,
    );
  });

  testWidgets('cancel is exactly once and returns no secret', (tester) async {
    var cancellations = 0;
    await tester.pumpWidget(
      MaterialApp(
        theme: celestialDark,
        home: Scaffold(
          body: QuestionSheet(
            title: 'Secret',
            items: [QuestionItem(id: 'secret', secret: true)],
            onSubmit: (_) => fail('cancel submitted answers'),
            onCancel: () => cancellations++,
          ),
        ),
      ),
    );
    await tester.enterText(
      find.byKey(const Key('question-text-0')),
      'P2-SECRET-MUST-NEVER-PERSIST',
    );
    await tester.tap(find.byKey(const Key('question-cancel')));
    await tester.tap(find.byKey(const Key('question-cancel')));
    await tester.pump();
    expect(cancellations, 1);
    expect(
      tester
          .widget<TextField>(find.byKey(const Key('question-text-0')))
          .controller!
          .text,
      isEmpty,
    );
  });
}

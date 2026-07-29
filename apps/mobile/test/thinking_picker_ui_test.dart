import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/picker.dart';
import 'package:magic_cli_remote/features/widgets/option_picker_sheet.dart';

void main() {
  testWidgets('levels-bearing option shows chips; bare option shows none', (
    tester,
  ) async {
    final catalog = PickerCatalog(
      kind: PickerKind.single,
      source: PickerSource.staticSource,
      options: [
        PickerOption(
          id: 'with-levels',
          label: 'With levels',
          thinkingLevels: const [
            ThinkingLevel(id: 'low'),
            ThinkingLevel(id: 'medium', isDefault: true),
            ThinkingLevel(id: 'high'),
          ],
        ),
        PickerOption(id: 'bare', label: 'Bare'),
      ],
      defaultIds: const ['with-levels'],
    );

    PickerResult? result;
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (ctx) => Scaffold(
            body: TextButton(
              onPressed: () async {
                result = await showOptionPicker(
                  ctx,
                  catalog: catalog,
                  title: 'Model',
                  thinkingIntent: 'high',
                );
              },
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    // Selected default row has chips; high preselected from intent.
    expect(
      find.byKey(const ValueKey('thinking-with-levels-low')),
      findsOneWidget,
    );
    expect(
      find.byKey(const ValueKey('thinking-with-levels-high')),
      findsOneWidget,
    );

    // Switch to bare row: chips for the levels model disappear.
    await tester.tap(find.text('Bare'));
    await tester.pumpAndSettle();
    expect(
      find.byKey(const ValueKey('thinking-with-levels-low')),
      findsNothing,
    );

    // Back to levels row: chips return.
    await tester.tap(find.text('With levels'));
    await tester.pumpAndSettle();
    expect(
      find.byKey(const ValueKey('thinking-with-levels-medium')),
      findsOneWidget,
    );

    await tester.tap(find.text('Select'));
    await tester.pumpAndSettle();
    expect(result?.single, 'with-levels');
    expect(result?.thinkingLevel, 'high');
  });

  testWidgets(
    'resolved default is preselected for null intent via visual default',
    (tester) async {
      final catalog = PickerCatalog(
        options: [
          PickerOption(
            id: 'm',
            label: 'Model',
            thinkingLevels: const [
              ThinkingLevel(id: 'low'),
              ThinkingLevel(id: 'medium', isDefault: true),
              ThinkingLevel(id: 'high'),
            ],
          ),
        ],
        defaultIds: const ['m'],
      );

      PickerResult? result;
      await tester.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (ctx) => Scaffold(
              body: TextButton(
                onPressed: () async {
                  result = await showOptionPicker(
                    ctx,
                    catalog: catalog,
                    thinkingIntent: null,
                  );
                },
                child: const Text('open'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();

      // Intent null → wire null; visual highlights model default.
      final medium = tester.widget<ChoiceChip>(
        find.byKey(const ValueKey('thinking-m-medium')),
      );
      expect(medium.selected, isTrue);

      await tester.tap(find.text('Select'));
      await tester.pumpAndSettle();
      expect(result?.thinkingLevel, isNull);
    },
  );
}

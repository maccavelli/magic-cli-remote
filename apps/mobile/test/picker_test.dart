import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/picker.dart';
import 'package:magic_cli_remote/features/widgets/option_picker_sheet.dart';

void main() {
  group('PickerCatalog.fromJson', () {
    test('parses single catalog with defaults', () {
      final c = PickerCatalog.fromJson({
        'provider': 'fake',
        'kind': 'single',
        'source': 'static',
        'allow_custom': true,
        'default_ids': ['fake-echo'],
        'options': [
          {'id': 'fake-echo', 'label': 'Echo', 'group': 'fake'},
          {'id': 'disabled', 'label': 'No', 'enabled': false},
        ],
      });
      expect(c.kind, PickerKind.single);
      expect(c.source, PickerSource.staticSource);
      expect(c.allowCustom, isTrue);
      expect(c.defaultIds, ['fake-echo']);
      expect(c.options, hasLength(2));
      expect(c.options[0].displayLabel, 'Echo');
      expect(c.options[1].enabled, isFalse);
      // Omitted enabled → true
      expect(c.options[0].enabled, isTrue);
    });

    test('parses multi kind and max_select', () {
      final c = PickerCatalog.fromJson({
        'kind': 'multi',
        'max_select': 3,
        'min_select': 1,
        'options': [
          {'id': 'a'},
          {'id': 'b'},
        ],
      });
      expect(c.isMulti, isTrue);
      expect(c.maxSelect, 3);
      expect(c.minSelect, 1);
    });
  });

  testWidgets('option picker single-select confirms choice', (tester) async {
    final catalog = PickerCatalog(
      kind: PickerKind.single,
      source: PickerSource.staticSource,
      allowCustom: true,
      options: [
        PickerOption(id: 'a', label: 'Alpha', group: 'g'),
        PickerOption(id: 'b', label: 'Beta', group: 'g'),
      ],
      defaultIds: ['a'],
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
                  title: 'Pick one',
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
    expect(find.text('Pick one'), findsOneWidget);
    expect(find.text('Offline catalog'), findsOneWidget);

    await tester.tap(find.text('Beta'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Select'));
    await tester.pumpAndSettle();

    expect(result, isNotNull);
    expect(result!.single, 'b');
  });

  testWidgets('option picker multi-select accumulates', (tester) async {
    final catalog = PickerCatalog(
      kind: PickerKind.multi,
      maxSelect: 2,
      minSelect: 1,
      options: [
        PickerOption(id: 'a', label: 'A'),
        PickerOption(id: 'b', label: 'B'),
        PickerOption(id: 'c', label: 'C'),
      ],
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
                  title: 'Multi',
                  initialSelected: const [],
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

    await tester.tap(find.text('A'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('B'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Done'));
    await tester.pumpAndSettle();

    expect(result, isNotNull);
    expect(result!.selectedIds.toSet(), {'a', 'b'});
  });

  testWidgets('option picker keeps bottom controls above the keyboard', (
    tester,
  ) async {
    final catalog = PickerCatalog(
      kind: PickerKind.single,
      source: PickerSource.staticSource,
      allowCustom: true,
      options: [PickerOption(id: 'a', label: 'Alpha')],
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (ctx) => Scaffold(
            body: TextButton(
              onPressed: () =>
                  showOptionPicker(ctx, catalog: catalog, title: 'Pick'),
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    // Simulate the software keyboard occupying the bottom of the screen.
    tester.view.viewInsets = FakeViewPadding(
      bottom: 300 * tester.view.devicePixelRatio,
    );
    addTearDown(tester.view.reset);
    await tester.pumpAndSettle();

    // No overflow, and the confirm row sits fully above the keyboard.
    expect(tester.takeException(), isNull);
    final keyboardTop =
        tester.view.physicalSize.height / tester.view.devicePixelRatio - 300;
    expect(
      tester.getBottomLeft(find.text('Select')).dy,
      lessThanOrEqualTo(keyboardTop),
    );

    tester.view.reset();
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
  });
}

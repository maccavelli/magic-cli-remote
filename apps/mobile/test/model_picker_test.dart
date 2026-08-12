import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/picker.dart';
import 'package:magic_cli_remote/features/widgets/option_picker_sheet.dart';

/// Opens the picker over [catalog] and returns the tester's harness.
Future<void> pumpPicker(WidgetTester tester, PickerCatalog catalog) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (ctx) => TextButton(
            onPressed: () => showOptionPicker(ctx, catalog: catalog),
            child: const Text('open'),
          ),
        ),
      ),
    ),
  );
  await tester.tap(find.text('open'));
  await tester.pumpAndSettle();
}

/// The order group headers appear in, top to bottom.
List<String> headerOrder(WidgetTester tester, List<String> candidates) {
  final seen = <String, double>{};
  for (final c in candidates) {
    final f = find.text(c);
    if (f.evaluate().isEmpty) continue;
    seen[c] = tester.getTopLeft(f.first).dy;
  }
  final keys = seen.keys.toList()..sort((a, b) => seen[a]!.compareTo(seen[b]!));
  return keys;
}

void main() {
  testWidgets('group order is the catalog order, not alphabetical', (
    tester,
  ) async {
    // The daemon now orders meaningfully — connected model providers first —
    // and the picker used to throw that away by sorting group headers
    // alphabetically, which is how an OpenCode picker opened on `302ai`.
    final catalog = PickerCatalog(
      options: [
        PickerOption(id: 'opencode/a', label: 'A', group: 'Connected'),
        PickerOption(id: 'zeta/b', label: 'B', group: 'All providers'),
      ],
    );
    await pumpPicker(tester, catalog);
    expect(headerOrder(tester, ['Connected', 'All providers']), [
      'Connected',
      'All providers',
    ]);
  });

  testWidgets('options of one group stay together in first-appearance order', (
    tester,
  ) async {
    final catalog = PickerCatalog(
      options: [
        PickerOption(id: 'x1', label: 'X one', group: 'Second'),
        PickerOption(id: 'y1', label: 'Y one', group: 'First'),
        PickerOption(id: 'x2', label: 'X two', group: 'Second'),
      ],
    );
    await pumpPicker(tester, catalog);
    final secondHeader = tester.getTopLeft(find.text('Second')).dy;
    final firstHeader = tester.getTopLeft(find.text('First')).dy;
    expect(secondHeader, lessThan(firstHeader));
    // Both "Second" members sit above the "First" header.
    expect(tester.getTopLeft(find.text('X one')).dy, lessThan(firstHeader));
    expect(tester.getTopLeft(find.text('X two')).dy, lessThan(firstHeader));
  });

  testWidgets('a 71-value catalog scrolls', (tester) async {
    // Goose's `provider` config option really has 71 values. The agent-settings
    // sheet used to render them in a bare Column with no scroll view, which
    // overflowed and could not be reached (MADR 0043 D12).
    final catalog = PickerCatalog(
      options: [
        for (var i = 0; i < 71; i++)
          PickerOption(id: 'p$i', label: 'Provider $i'),
      ],
    );
    await pumpPicker(tester, catalog);
    final list = find.byType(ListView);
    expect(list, findsOneWidget);
    final scrollable = find
        .descendant(of: list, matching: find.byType(Scrollable))
        .first;
    final pos = tester.state<ScrollableState>(scrollable).position;
    expect(
      pos.maxScrollExtent,
      greaterThan(0),
      reason: '71 rows must be scrollable, not overflowing',
    );
    // And the tail is reachable.
    await tester.scrollUntilVisible(
      find.text('Provider 70'),
      300,
      scrollable: scrollable,
    );
    expect(find.text('Provider 70'), findsOneWidget);
  });

  testWidgets('truncation is reported, never silent', (tester) async {
    final catalog = PickerCatalog(
      truncated: true,
      options: [
        for (var i = 0; i < 3; i++) PickerOption(id: 'm$i', label: 'Model $i'),
      ],
    );
    await pumpPicker(tester, catalog);
    expect(find.textContaining('Showing the first 3'), findsOneWidget);
  });

  testWidgets('deprecated and unconfigured rows are badged', (tester) async {
    final catalog = PickerCatalog(
      options: [
        PickerOption(
          id: 'old',
          label: 'Old Model',
          meta: const {'status': 'deprecated'},
        ),
        PickerOption(
          id: 'anthropic',
          label: 'Anthropic',
          meta: const {'connected': 'false'},
        ),
        PickerOption(
          id: 'opencode',
          label: 'OpenCode Zen',
          meta: const {'connected': 'true'},
        ),
      ],
    );
    await pumpPicker(tester, catalog);
    expect(find.text('deprecated'), findsOneWidget);
    expect(find.text('not configured'), findsOneWidget);
  });

  testWidgets('search is debounced and filters across id, label and group', (
    tester,
  ) async {
    final catalog = PickerCatalog(
      options: [
        PickerOption(id: 'openai/gpt-5', label: 'GPT-5', group: 'openai'),
        PickerOption(
          id: 'anthropic/claude',
          label: 'Claude',
          group: 'anthropic',
        ),
      ],
    );
    await pumpPicker(tester, catalog);
    await tester.enterText(find.byType(TextField).first, 'claude');
    // Before the debounce window elapses nothing has changed yet.
    await tester.pump(const Duration(milliseconds: 40));
    expect(find.text('GPT-5'), findsOneWidget);
    await tester.pump(const Duration(milliseconds: 200));
    expect(find.text('GPT-5'), findsNothing);
    expect(find.text('Claude'), findsOneWidget);

    // Group name matches too — that is the "provider fuzzy search" path.
    await tester.enterText(find.byType(TextField).first, 'openai');
    await tester.pump(const Duration(milliseconds: 200));
    expect(find.text('GPT-5'), findsOneWidget);
    expect(find.text('Claude'), findsNothing);
  });

  testWidgets('a query matching nothing says so', (tester) async {
    final catalog = PickerCatalog(
      options: [PickerOption(id: 'a', label: 'Alpha')],
    );
    await pumpPicker(tester, catalog);
    await tester.enterText(find.byType(TextField).first, 'zzz');
    await tester.pump(const Duration(milliseconds: 200));
    expect(find.textContaining('Nothing matches'), findsOneWidget);
  });

  group('PickerCatalog scope fields', () {
    test('parses model_provider and truncated', () {
      final c = PickerCatalog.fromJson({
        'provider': 'opencode',
        'model_provider': 'anthropic',
        'truncated': true,
        'options': [
          {
            'id': 'anthropic/claude',
            'meta': {'release_date': '2026-01-01', 'context': '200K'},
          },
        ],
      });
      expect(c.modelProvider, 'anthropic');
      expect(c.truncated, isTrue);
      expect(c.options.first.contextWindow, '200K');
      expect(c.options.first.isDeprecated, isFalse);
      expect(c.options.first.connected, isNull);
    });

    test(
      'connected is tri-state so model rows are not mistaken for providers',
      () {
        final connected = PickerOption(
          id: 'a',
          meta: const {'connected': 'true'},
        );
        final not = PickerOption(id: 'b', meta: const {'connected': 'false'});
        final model = PickerOption(id: 'c');
        expect(connected.connected, isTrue);
        expect(not.connected, isFalse);
        expect(model.connected, isNull);
      },
    );

    test('parses provider model count and default model metadata', () {
      final c = PickerCatalog.fromJson({
        'options': [
          {
            'id': 'anthropic',
            'meta': {'model_count': '2', 'default_model': 'anthropic/claude'},
          },
          {'id': 'goose'},
        ],
      });

      expect(c.options.first.modelCount, '2');
      expect(c.options.first.defaultModel, 'anthropic/claude');
      expect(c.options.last.modelCount, isEmpty);
      expect(c.options.last.defaultModel, isEmpty);
    });
  });
}

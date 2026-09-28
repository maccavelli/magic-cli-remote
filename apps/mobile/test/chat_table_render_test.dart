import 'package:flutter/material.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:flutter_test/flutter_test.dart';

/// Mirrors `_sheetFor` table fields from `chat_bubble.dart` (P1). The production
/// helper is private; this duplicate is the contract the wrapper test pins.
class _FakeChatStyleSheet {
  static MarkdownStyleSheet build(BuildContext context) {
    final theme = Theme.of(context);
    return MarkdownStyleSheet.fromTheme(theme).copyWith(
      tableColumnWidth: const IntrinsicColumnWidth(),
      tableBorder: TableBorder.all(
        color: theme.colorScheme.outlineVariant,
        borderRadius: BorderRadius.circular(8),
      ),
      tableCellsPadding: const EdgeInsets.symmetric(
        horizontal: 10,
        vertical: 8,
      ),
      tablePadding: const EdgeInsets.only(bottom: 4),
      tableHead: theme.textTheme.bodyMedium?.copyWith(
        fontWeight: FontWeight.w600,
      ),
      tableHeadAlign: TextAlign.left,
      tableBody: theme.textTheme.bodyMedium,
    );
  }
}

const _wideTable = '''
| Column A | Column B | Column C |
| -------- | -------- | -------- |
| long prose text that should not wrap aggressively on a phone | another long prose cell | third long prose cell |
| another row | second row | third row |
''';

const _narrowTable = '''
| A | B |
| - | - |
| x | y |
''';

const _selectTable = '''
| Left | Right |
| ---- | ----- |
| alpha cell | beta cell |
''';

Finder _inMarkdown(Finder matching) =>
    find.descendant(of: find.byType(MarkdownBody), matching: matching);

Future<void> _pumpTable(
  WidgetTester tester,
  String markdown, {
  bool selectable = false,
}) async {
  tester.view.physicalSize = const Size(360, 800);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);

  await tester.pumpWidget(
    MaterialApp(
      home: MediaQuery(
        data: const MediaQueryData(size: Size(360, 800)),
        child: Scaffold(
          body: Builder(
            builder: (context) {
              return SizedBox(
                width: 360,
                child: MarkdownBody(
                  data: markdown,
                  selectable: selectable,
                  styleSheet: _FakeChatStyleSheet.build(context),
                ),
              );
            },
          ),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets(
    'renders a wide table wrapped in a horizontal SingleChildScrollView',
    (tester) async {
      await _pumpTable(tester, _wideTable);

      expect(_inMarkdown(find.byType(SingleChildScrollView)), findsOneWidget);
      final scsv = tester.widget<SingleChildScrollView>(
        _inMarkdown(find.byType(SingleChildScrollView)),
      );
      expect(scsv.scrollDirection, Axis.horizontal);
      expect(_inMarkdown(find.byType(Table)), findsOneWidget);
      expect(find.textContaining('long prose text'), findsOneWidget);

      final controller = scsv.controller;
      expect(controller, isNotNull);
      expect(controller!.position.maxScrollExtent, greaterThan(0));
      final before = controller.offset;
      await tester.drag(
        _inMarkdown(find.byType(SingleChildScrollView)),
        const Offset(-120, 0),
      );
      await tester.pumpAndSettle();
      expect(controller.offset, greaterThan(before));
    },
  );

  testWidgets(
    'renders a narrow table without overflow (wrapper still present)',
    (tester) async {
      await _pumpTable(tester, _narrowTable);

      expect(_inMarkdown(find.byType(SingleChildScrollView)), findsOneWidget);
      final scsv = tester.widget<SingleChildScrollView>(
        _inMarkdown(find.byType(SingleChildScrollView)),
      );
      expect(scsv.controller, isNotNull);
      expect(scsv.controller!.position.maxScrollExtent, 0);
    },
  );

  testWidgets('preserves text selection across cells', (tester) async {
    await _pumpTable(tester, _selectTable, selectable: true);

    await tester.longPress(find.textContaining('alpha cell'));
    await tester.pumpAndSettle();
    expect(find.byType(AdaptiveTextSelectionToolbar), findsWidgets);
  });
}

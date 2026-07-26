import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/features/widgets/work_items_panel.dart';
import 'package:magic_cli_remote/theme/celestial.dart';

void main() {
  Widget subject(List<PlanEntry> entries) => MaterialApp(
    theme: celestialLight,
    home: Scaffold(body: WorkItemsPanel(entries: entries)),
  );

  testWidgets('summarises work and expands all current entries', (
    tester,
  ) async {
    await tester.pumpWidget(
      subject([
        PlanEntry(content: 'Finish shared UI', status: 'completed'),
        PlanEntry(
          content: 'Verify providers',
          status: 'in_progress',
          priority: 'high',
        ),
      ]),
    );

    expect(find.text('Todos'), findsOneWidget);
    expect(find.text('1 done, 1 remaining · Verify providers'), findsOneWidget);
    expect(find.text('Finish shared UI'), findsNothing);

    await tester.tap(find.text('Todos'));
    await tester.pumpAndSettle();

    expect(find.text('Finish shared UI'), findsOneWidget);
    expect(find.text('Verify providers'), findsOneWidget);
    expect(find.byIcon(Icons.priority_high), findsOneWidget);
  });

  testWidgets('handles an empty work-item snapshot', (tester) async {
    await tester.pumpWidget(subject([]));

    expect(find.text('0 done, 0 remaining'), findsOneWidget);
    expect(find.byType(LinearProgressIndicator), findsOneWidget);
  });

  testWidgets('keeps a long work-item list scrollable', (tester) async {
    final entries = List.generate(
      30,
      (index) => PlanEntry(content: 'Work item $index'),
    );
    await tester.pumpWidget(subject(entries));
    await tester.tap(find.text('Todos'));
    await tester.pumpAndSettle();

    final list = find.descendant(
      of: find.byType(ListView),
      matching: find.byType(Scrollable),
    );
    await tester.scrollUntilVisible(
      find.text('Work item 29'),
      180,
      scrollable: list,
    );

    expect(find.text('Work item 29'), findsOneWidget);
  });
}

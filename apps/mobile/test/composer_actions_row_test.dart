import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/features/chat/session_controls/composer_actions_row.dart';

/// MADR 0123 D12/D13, acceptance A14–A16.
///
/// The capacity target is ten icons at the 360dp reference width. Default
/// `IconButton`s need 480dp for ten against 336dp available — 43% over — so
/// this is a layout rule, not a glyph choice, and it is asserted at capacity
/// rather than inferred from the nine codex happens to need today.

/// The composer's own padding: EdgeInsets.fromLTRB(12, 4, 12, 12).
const double _composerPadding = 24;

List<ComposerAction> _actions(int n) => [
  for (var i = 0; i < n; i++)
    ComposerAction(
      id: 'a$i',
      icon: Icons.circle,
      tooltip: 'action $i',
      onPressed: () {},
    ),
];

Widget _host(int count, double surfaceWidth) {
  return MaterialApp(
    home: Scaffold(
      body: Center(
        child: SizedBox(
          width: surfaceWidth - _composerPadding,
          child: ComposerActionsRow(actions: _actions(count)),
        ),
      ),
    ),
  );
}

Future<void> _pump(WidgetTester tester, int count, double width) async {
  tester.view.physicalSize = Size(width, 800);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(_host(count, width));
  await tester.pumpAndSettle();
}

void main() {
  group('capacity arithmetic', () {
    test('ten fit at the 360dp reference width', () {
      const available = 360 - _composerPadding; // 336
      expect(ComposerActionsRow.fits(available, 10), isTrue);
      expect(
        ComposerActionsRow.widthFor(available, 10),
        closeTo(33.6, 0.01),
        reason: 'the derived budget, not a hardcoded size',
      );
    });

    test('ten fit on the narrowest phone too', () {
      const available = 320 - _composerPadding; // 296
      expect(ComposerActionsRow.fits(available, 10), isTrue);
      expect(ComposerActionsRow.widthFor(available, 10), closeTo(29.6, 0.01));
    });

    test('a fixed 32dp width would have broken the narrow case', () {
      // Recorded because it was the obvious answer and it is wrong: 32 x 10 is
      // 320dp against the 296dp a 320dp phone offers (MADR 0123 D12).
      const available = 320 - _composerPadding;
      expect(32 * 10 > available, isTrue);
    });

    test('past the floor the row must scroll rather than shrink', () {
      const available = 320 - _composerPadding;
      expect(
        ComposerActionsRow.widthFor(available, 12),
        ComposerActionsRow.kMinIconWidth,
        reason: 'clamped at the floor, never below',
      );
      expect(ComposerActionsRow.fits(available, 12), isFalse);
    });

    test('a handful of icons do not sprawl', () {
      expect(
        ComposerActionsRow.widthFor(430 - _composerPadding, 3),
        ComposerActionsRow.kMaxIconWidth,
      );
    });
  });

  group('layout', () {
    testWidgets('ten icons lay out at 360dp with no overflow', (tester) async {
      // The capacity target, asserted with a real ten. Codex needs nine today;
      // a test that checked nine would pass while the target went unmet.
      await _pump(tester, 10, 360);

      for (var i = 0; i < 10; i++) {
        expect(find.byKey(ValueKey('composer-action-a$i')), findsOneWidget);
      }
      expect(
        tester.takeException(),
        isNull,
        reason: 'a RenderFlex overflow throws; presence checks alone miss it',
      );
      expect(
        find.byKey(const ValueKey('composer-actions-scroll')),
        findsNothing,
        reason: 'ten must fit outright at the reference width',
      );
    });

    testWidgets('ten icons lay out at 320dp with no overflow', (tester) async {
      await _pump(tester, 10, 320);
      expect(find.byKey(const ValueKey('composer-action-a9')), findsOneWidget);
      expect(tester.takeException(), isNull);
    });

    testWidgets('twelve at 320dp scrolls, dropping nothing', (tester) async {
      await _pump(tester, 12, 320);
      expect(
        find.byKey(const ValueKey('composer-actions-scroll')),
        findsOneWidget,
      );
      expect(tester.takeException(), isNull);
      // Never truncate: a hidden control is the defect being fixed.
      expect(
        find.byKey(const ValueKey('composer-action-a11'), skipOffstage: false),
        findsOneWidget,
      );
    });

    testWidgets('every icon keeps the full 48dp tap height', (tester) async {
      // Width is the only dimension traded away (MADR 0123 F9).
      await _pump(tester, 10, 360);
      final box = tester.getSize(
        find.byKey(const ValueKey('composer-action-a0')),
      );
      expect(box.height, ComposerActionsRow.kTapHeight);
      expect(box.width, closeTo(33.6, 0.5));
    });

    testWidgets('a tint survives the move off the app bar', (tester) async {
      // C4: a session that auto-approves must be visible without opening a
      // card, exactly as the tinted chip used to signal it.
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ComposerActionsRow(
              actions: [
                ComposerAction(
                  id: 'perms',
                  icon: Icons.bolt,
                  tooltip: 'Permissions',
                  tint: const Color(0xFFB3261E),
                  onPressed: () {},
                ),
              ],
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      final icon = tester.widget<Icon>(find.byIcon(Icons.bolt));
      expect(icon.color, const Color(0xFFB3261E));
    });

    testWidgets('an empty row renders nothing', (tester) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(body: ComposerActionsRow(actions: [])),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('composer-actions')), findsNothing);
    });

    testWidgets('a null callback disables without removing', (tester) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: ComposerActionsRow(
              actions: [
                ComposerAction(
                  id: 'off',
                  icon: Icons.circle,
                  tooltip: 'offline',
                ),
              ],
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      final button = tester.widget<IconButton>(
        find.byKey(const ValueKey('composer-action-off')),
      );
      expect(button.onPressed, isNull);
    });
  });
}

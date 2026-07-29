import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/theme/celestial.dart';
import 'package:magic_cli_remote/theme/top_notification.dart';

/// The transient-message overlay that replaced `ScaffoldMessenger.showSnackBar`
/// across the app. Two properties it lost in that migration and must keep:
/// screen readers announce it, and a burst does not collapse to its last
/// message (audit 0041 M2/M3, MADR 0042 D7).
void main() {
  Widget host(void Function(BuildContext) onReady) => MaterialApp(
    theme: celestialDark,
    home: Builder(
      builder: (context) {
        // Fire once the overlay exists.
        WidgetsBinding.instance.addPostFrameCallback((_) => onReady(context));
        return const Scaffold(body: SizedBox.expand());
      },
    ),
  );

  testWidgets('announces itself as a live region', (tester) async {
    final handle = tester.ensureSemantics();

    await tester.pumpWidget(host((c) => showTopNotification(c, 'Send failed')));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('Send failed'), findsOneWidget);
    expect(
      tester.getSemantics(find.text('Send failed')),
      matchesSemantics(
        label: 'Send failed',
        isLiveRegion: true,
        hasScrollUpAction: true,
        hasScrollDownAction: true,
      ),
      reason: 'SnackBar announced; the replacement must too',
    );

    handle.dispose();
  });

  testWidgets('a second message queues instead of replacing the first', (
    tester,
  ) async {
    await tester.pumpWidget(
      host((c) {
        showTopNotification(c, 'first failure');
        showTopNotification(c, 'second failure');
      }),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('first failure'), findsOneWidget);
    expect(
      find.text('second failure'),
      findsNothing,
      reason: 'it waits its turn rather than evicting the first',
    );

    // Let the first run its course and slide out.
    await tester.pump(const Duration(seconds: 3));
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pump(const Duration(milliseconds: 300));

    expect(find.text('first failure'), findsNothing);
    expect(find.text('second failure'), findsOneWidget);

    await tester.pump(const Duration(seconds: 4));
    await tester.pumpAndSettle();
  });

  testWidgets('colour and type come from the snackbar theme', (tester) async {
    await tester.pumpWidget(host((c) => showTopNotification(c, 'themed')));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    final snack = celestialDark.snackBarTheme;
    final material = tester.widget<Material>(
      find
          .ancestor(of: find.text('themed'), matching: find.byType(Material))
          .first,
    );
    expect(material.color, snack.backgroundColor);
    expect(material.shape, snack.shape);

    final text = tester.widget<Text>(find.text('themed'));
    expect(
      text.style?.color,
      snack.contentTextStyle?.color,
      reason: 'no second, drifting copy of the design tokens',
    );

    await tester.pump(const Duration(seconds: 4));
    await tester.pumpAndSettle();
  });

  testWidgets('can be dismissed with an upward swipe', (tester) async {
    await tester.pumpWidget(host((c) => showTopNotification(c, 'dismiss me')));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    await tester.fling(find.text('dismiss me'), const Offset(0, -180), 1000);
    await tester.pumpAndSettle();

    expect(find.text('dismiss me'), findsNothing);
  });

  testWidgets('a swipe hands straight over to the queued message', (
    tester,
  ) async {
    await tester.pumpWidget(
      host((c) {
        showTopNotification(c, 'first failure');
        showTopNotification(c, 'second failure');
      }),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    await tester.fling(find.text('first failure'), const Offset(0, -180), 1000);
    // The collapse resolves the dismissal. Replaying the exit animation on the
    // already-collapsed child then held the entry in the tree and delayed the
    // queue by another ~550 ms (MADR 0046 L-11).
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));

    expect(find.text('first failure'), findsNothing);
    expect(
      find.text('second failure'),
      findsOneWidget,
      reason: 'the queue advances as soon as the swipe completes',
    );

    await tester.pump(const Duration(seconds: 4));
    await tester.pumpAndSettle();
  });
}

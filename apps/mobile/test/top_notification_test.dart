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

  // The test above proves the notification follows the theme; this one pins
  // what the theme is allowed to say. Material's default snackbar recipe is
  // inverseSurface/onInverseSurface — a deliberate brightness flip that is
  // right for stock M3 and wrong here: on the dark celestial scheme it renders
  // a near-white slab over a #0B0D1A starfield, the one component in the app
  // that ignored its own palette.
  for (final (name, theme) in [
    ('dark', celestialDark),
    ('light', celestialLight),
  ]) {
    test('$name notifications sit on the app palette, not the inverse', () {
      final snack = theme.snackBarTheme;
      final scheme = theme.colorScheme;

      expect(
        snack.backgroundColor,
        isNot(scheme.inverseSurface),
        reason: 'inverseSurface flips brightness against the whole app',
      );
      expect(
        snack.backgroundColor,
        scheme.surfaceContainerHigh,
        reason: 'same material as dialogs and bottom sheets',
      );
      expect(snack.contentTextStyle?.color, scheme.onSurface);
      expect(snack.actionTextColor, scheme.primary);

      // A dark card on a dark background needs an edge; the white one did not.
      final shape = snack.shape;
      expect(shape, isA<RoundedRectangleBorder>());
      expect(
        (shape! as RoundedRectangleBorder).side.color,
        scheme.outlineVariant,
        reason: 'without an outline the card dissolves into the backdrop',
      );
    });
  }

  group('severity', () {
    testWidgets('an error carries a rail and an icon; info carries neither', (
      tester,
    ) async {
      await tester.pumpWidget(
        host(
          (c) => showTopNotification(
            c,
            'Resume failed',
            severity: NoticeSeverity.error,
          ),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      expect(find.byIcon(Icons.error_outline), findsOneWidget);
      final rail = tester.widget<DecoratedBox>(
        find
            .ancestor(
              of: find.text('Resume failed'),
              matching: find.byType(DecoratedBox),
            )
            .first,
      );
      final border = (rail.decoration as BoxDecoration).border!;
      expect(
        border.bottom.style,
        BorderStyle.none,
        reason: 'a leading rail, not a box',
      );
      expect(
        (border as Border).left.color,
        celestialDark.colorScheme.error,
        reason: 'severity colour comes from the scheme, not a literal',
      );

      await tester.pump(const Duration(seconds: 6));
      await tester.pumpAndSettle();
    });

    testWidgets('info stays quiet — no icon, no rail', (tester) async {
      await tester.pumpWidget(
        host((c) => showTopNotification(c, 'Reconnect to the host first')),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      expect(find.byType(Icon), findsNothing);
      expect(
        find.ancestor(
          of: find.text('Reconnect to the host first'),
          matching: find.byType(DecoratedBox),
        ),
        findsNothing,
        reason: 'the most common notification must not spend the accent',
      );

      await tester.pump(const Duration(seconds: 4));
      await tester.pumpAndSettle();
    });

    testWidgets('the severity glyph is not announced', (tester) async {
      final handle = tester.ensureSemantics();
      await tester.pumpWidget(
        host(
          (c) => showTopNotification(
            c,
            'Send failed',
            severity: NoticeSeverity.error,
          ),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      // The message already says "failed"; a screen reader reading a decorative
      // glyph on top of that is noise.
      expect(
        tester.getSemantics(find.text('Send failed')),
        matchesSemantics(
          label: 'Send failed',
          isLiveRegion: true,
          hasScrollUpAction: true,
          hasScrollDownAction: true,
        ),
      );

      handle.dispose();
      await tester.pump(const Duration(seconds: 6));
      await tester.pumpAndSettle();
    });

    testWidgets('a failure lingers longer than a confirmation', (tester) async {
      await tester.pumpWidget(
        host(
          (c) => showTopNotification(
            c,
            'Create failed',
            severity: NoticeSeverity.error,
          ),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      // Past the 3s default that info and success get. The two short pumps
      // matter: advancing the clock in one jump fires the dismiss timer but
      // never runs the 250ms slide-out, so the card would still be findable
      // whatever its duration and the test would prove nothing.
      await tester.pump(const Duration(seconds: 4));
      await tester.pump(const Duration(milliseconds: 300));
      await tester.pump(const Duration(milliseconds: 300));
      expect(
        find.text('Create failed'),
        findsOneWidget,
        reason:
            'nothing in the app keeps a notification history; 3s is not '
            'enough to read a failure you glanced away from',
      );

      await tester.pump(const Duration(seconds: 2));
      await tester.pumpAndSettle();
      expect(find.text('Create failed'), findsNothing);
    });

    testWidgets('an action keeps its own duration whatever the severity', (
      tester,
    ) async {
      await tester.pumpWidget(
        host(
          (c) => showTopNotification(
            c,
            'Send failed',
            severity: NoticeSeverity.error,
            actionLabel: 'Sessions',
            onAction: () {},
          ),
        ),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      // 6s, not the 5s an unactioned error gets: a button must stay tappable.
      await tester.pump(const Duration(seconds: 5));
      await tester.pump(const Duration(milliseconds: 300));
      await tester.pump(const Duration(milliseconds: 300));
      expect(find.text('Send failed'), findsOneWidget);

      await tester.pump(const Duration(seconds: 2));
      await tester.pumpAndSettle();
      expect(find.text('Send failed'), findsNothing);
    });

    testWidgets('an overflowing queue drops confirmations before failures', (
      tester,
    ) async {
      await tester.pumpWidget(
        host((c) {
          // One shows immediately; the rest queue, and the queue holds 3.
          showTopNotification(c, 'shown', severity: NoticeSeverity.error);
          showTopNotification(c, 'errA', severity: NoticeSeverity.error);
          showTopNotification(c, 'errB', severity: NoticeSeverity.error);
          showTopNotification(c, 'errC', severity: NoticeSeverity.error);
          showTopNotification(c, 'copied', severity: NoticeSeverity.success);
        }),
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      // Walk the queue out. Evicting by age would have dropped errA — the
      // first error of the cascade, which is usually the informative one.
      final seen = <String>['shown'];
      for (var i = 0; i < 3; i++) {
        await tester.pump(const Duration(seconds: 6));
        await tester.pump(const Duration(milliseconds: 300));
        await tester.pump(const Duration(milliseconds: 300));
        for (final m in ['errA', 'errB', 'errC', 'copied']) {
          if (find.text(m).evaluate().isNotEmpty) seen.add(m);
        }
      }

      expect(seen, ['shown', 'errA', 'errB', 'errC']);
      expect(
        seen,
        isNot(contains('copied')),
        reason: 'a confirmation is the most disposable message in the queue',
      );

      await tester.pump(const Duration(seconds: 8));
      await tester.pumpAndSettle();
    });
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

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:magic_cli_remote/features/sessions/sessions_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'package:magic_cli_remote/theme/celestial.dart';
import 'package:magic_cli_remote/theme/widgets.dart';

class MockMcremoteClient extends McremoteClient {
  MockMcremoteClient({
    this.sessions = const <SessionMeta>[],
    this.providers = const <ProviderInfo>[],
  });

  final List<SessionMeta> sessions;
  final List<ProviderInfo> providers;

  // Connected so _refresh actually fetches instead of early-returning.
  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<List<SessionMeta>> listSessions() async => sessions;

  @override
  Future<List<ProviderInfo>> listProviders() async => providers;
}

Widget _wrap(MockMcremoteClient client, {ThemeData? theme}) {
  return ProviderScope(
    overrides: [
      connectionStateProvider.overrideWith(
        (ref) => Stream.value(McConnectionState.connected),
      ),
      mcremoteClientProvider.overrideWithValue(client),
    ],
    child: MaterialApp(theme: theme, home: const SessionsScreen()),
  );
}

void main() {
  testWidgets(
    'SessionsScreen does not show bottom right FloatingActionButton',
    (tester) async {
      await tester.pumpWidget(_wrap(MockMcremoteClient()));

      await tester.pumpAndSettle();

      // The FloatingActionButton should not be found anywhere on the screen
      expect(find.byType(FloatingActionButton), findsNothing);

      // But the AppBar should be present
      expect(find.text('Sessions'), findsOneWidget);

      // Multi-device empty state (C.2): not "daemon empty", this-device owned.
      expect(find.text('No sessions on this device'), findsOneWidget);
      expect(find.textContaining('another phone'), findsOneWidget);
    },
  );

  testWidgets('long session names truncate to a single ellipsised line', (
    tester,
  ) async {
    const longName =
        'An extremely long session name that would otherwise wrap the tile '
        'title across several lines and break row alignment';
    await tester.pumpWidget(
      _wrap(
        MockMcremoteClient(
          sessions: [
            SessionMeta(id: 'abcdef1234', provider: 'grok', name: longName),
          ],
        ),
      ),
    );

    await tester.pumpAndSettle();

    final title = tester.widget<Text>(find.text(longName));
    expect(title.maxLines, 1);
    expect(title.overflow, TextOverflow.ellipsis);

    // The connected banner derives its hostname in build (no host dialled in
    // this test, so the generic fallback).
    expect(find.text('Connected to host'), findsOneWidget);
  });

  testWidgets('connection banner host label tracks hostInputListenable', (
    tester,
  ) async {
    final client = MockMcremoteClient();
    await tester.pumpWidget(_wrap(client));
    await tester.pumpAndSettle();
    expect(find.text('Connected to host'), findsOneWidget);

    // Simulates a host note without a connection-state transition.
    client.hostInputListenable.value = '10.0.2.2:7531';
    await tester.pump();

    expect(find.text('Connected to 10.0.2.2'), findsOneWidget);
  });

  testWidgets('unhealthy sessions banner reuses ConnBanner with subtitle', (
    tester,
  ) async {
    final client = MockMcremoteClient();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          connectionStateProvider.overrideWith(
            (ref) => Stream.value(McConnectionState.reconnecting),
          ),
          mcremoteClientProvider.overrideWithValue(client),
        ],
        child: const MaterialApp(home: SessionsScreen()),
      ),
    );
    // Linking spinner is unbounded; avoid pumpAndSettle.
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    expect(find.byType(ConnBanner), findsOneWidget);
    expect(
      find.text('Pairing stays active until you sign out.'),
      findsOneWidget,
    );
    // TextButton paints label text twice (button + semantics); assert presence.
    expect(find.text('Retry now'), findsWidgets);
  });

  // The new-session dialog mixes three different field widgets
  // (DropdownButtonFormField, TextField, InputDecorator). They only look like
  // one consistent form because each carries the same decoration; a stray
  // padding or a non-floating label silently desymmetrises one of them. These
  // assert the rendered geometry rather than the widget config, under the real
  // app theme, because that is what the eye actually judges.
  group('new-session dialog layout', () {
    const labels = <String>[
      'Available providers',
      'Friendly name',
      'Working directory',
      'Select model (optional)',
    ];

    // The dialog reads recent cwds before it opens; without a backing store
    // that read throws and the dialog never appears.
    setUp(() => SharedPreferences.setMockInitialValues({}));

    Future<void> openDialog(WidgetTester tester) async {
      await tester.pumpWidget(
        _wrap(
          MockMcremoteClient(
            providers: [ProviderInfo(id: 'opencode', ready: true)],
          ),
          theme: celestialDark,
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'New session'));
      await tester.pumpAndSettle();
    }

    testWidgets('every field renders at the same height', (tester) async {
      await openDialog(tester);

      final heights = <String, double>{};
      for (final label in labels) {
        final field = find
            .ancestor(
              of: find.text(label),
              matching: find.byType(InputDecorator),
            )
            .first;
        heights[label] = tester.getSize(field).height;
      }

      expect(heights.values.toSet(), hasLength(1), reason: 'heights: $heights');
    });

    testWidgets('every field label starts on the same vertical line', (
      tester,
    ) async {
      await openDialog(tester);

      final lefts = <String, double>{};
      for (final label in labels) {
        lefts[label] = tester.getTopLeft(find.text(label)).dx;
      }

      expect(lefts.values.toSet(), hasLength(1), reason: 'lefts: $lefts');
    });

    testWidgets('the header aligns with the field labels', (tester) async {
      await openDialog(tester);

      final title = tester.getTopLeft(find.text('Begin new session')).dx;
      final firstLabel = tester.getTopLeft(find.text(labels.first)).dx;

      expect(title, firstLabel);
    });

    testWidgets('the gaps between fields are identical', (tester) async {
      await openDialog(tester);

      Rect box(String label) => tester.getRect(
        find
            .ancestor(
              of: find.text(label),
              matching: find.byType(InputDecorator),
            )
            .first,
      );

      final gaps = <double>[
        for (var i = 0; i < labels.length - 1; i++)
          box(labels[i + 1]).top - box(labels[i]).bottom,
      ];

      expect(gaps.toSet(), hasLength(1), reason: 'gaps: $gaps');
      expect(gaps.first, greaterThan(12), reason: 'gaps: $gaps');
    });
  });
}

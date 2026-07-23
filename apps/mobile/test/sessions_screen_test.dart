import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:magic_cli_remote/features/sessions/sessions_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'dart:async';

class MockMcremoteClient extends McremoteClient {
  MockMcremoteClient({this.sessions = const <SessionMeta>[]});

  final List<SessionMeta> sessions;

  // Connected so _refresh actually fetches instead of early-returning.
  @override
  McConnectionState get state => McConnectionState.connected;

  @override
  Future<List<SessionMeta>> listSessions() async => sessions;

  @override
  Future<List<ProviderInfo>> listProviders() async => <ProviderInfo>[];
}

Widget _wrap(MockMcremoteClient client) {
  return ProviderScope(
    overrides: [
      connectionStateProvider.overrideWith(
        (ref) => Stream.value(McConnectionState.connected),
      ),
      mcremoteClientProvider.overrideWithValue(client),
    ],
    child: const MaterialApp(home: SessionsScreen()),
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
}

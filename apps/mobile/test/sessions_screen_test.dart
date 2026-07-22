import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:magic_cli_remote/features/sessions/sessions_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
import 'dart:async';

class MockMcremoteClient extends McremoteClient {
  @override
  Future<List<SessionMeta>> listSessions() async => <SessionMeta>[];
}

void main() {
  testWidgets(
    'SessionsScreen does not show bottom right FloatingActionButton',
    (tester) async {
      final mockClient = MockMcremoteClient();
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            connectionStateProvider.overrideWith(
              (ref) => Stream.value(McConnectionState.connected),
            ),
            mcremoteClientProvider.overrideWithValue(mockClient),
          ],
          child: const MaterialApp(home: SessionsScreen()),
        ),
      );

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
}

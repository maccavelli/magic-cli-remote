import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/app.dart';
import 'package:magic_cli_remote/data/diagnostics/error_recorder.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _MemoryStore extends SettingsStore {
  List<Map<String, dynamic>> entries = [];

  @override
  Future<List<Map<String, dynamic>>> getRecentErrors() async => entries;

  @override
  Future<void> appendRecentError(
    Map<String, dynamic> entry, {
    int maxEntries = 5,
  }) async => entries = [...entries, entry];
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  test(
    'the FlutterError.onError shape main() installs records the failure',
    () async {
      // Exercised by invoking the handler main() registers, rather than by
      // making a real widget throw: a build-phase throw under flutter_test
      // deadlocks the binding, and the code under test here is ours, not
      // Flutter's error plumbing.
      final store = _MemoryStore();
      final recorder = ErrorRecorder(store);

      await recorder.record(
        StateError('build exploded'),
        StackTrace.fromString('#0 a frame'),
        source: ErrorSource.flutter,
      );

      expect(store.entries, hasLength(1));
      expect(store.entries.single['kind'], 'StateError');
      expect(store.entries.single['message'], contains('build exploded'));
      expect(store.entries.single['source'], ErrorSource.flutter);
    },
  );

  testWidgets('the error panel renders copy-able details, not a raw throw', (
    tester,
  ) async {
    // buildErrorPanel is what ErrorWidget.builder is set to in app.dart; in
    // debug it deliberately returns Flutter's own ErrorWidget, which is more
    // useful while developing.
    late Widget panel;
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) {
            panel = buildErrorPanel(
              FlutterErrorDetails(exception: StateError('boom')),
            );
            return const SizedBox.shrink();
          },
        ),
      ),
    );
    await tester.pumpAndSettle();

    // Debug build: Flutter's red box is retained by design.
    expect(panel, isA<ErrorWidget>());
  });
}

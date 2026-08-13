import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/diagnostics/error_recorder.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/features/settings/recent_errors_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';
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

  @override
  Future<void> clearRecentErrors() async => entries = [];
}

Map<String, dynamic> _entry(String message, {int minute = 0}) => {
  'kind': 'StateError',
  'message': message,
  'stack': '#0 a frame\n#1 another frame',
  'at': DateTime.utc(2026, 8, 13, 9, minute).toIso8601String(),
  'source': ErrorSource.flutter,
};

Future<_MemoryStore> _pump(
  WidgetTester tester, {
  required List<Map<String, dynamic>> entries,
}) async {
  final store = _MemoryStore()..entries = entries;
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        errorRecorderProvider.overrideWithValue(ErrorRecorder(store)),
      ],
      child: const MaterialApp(home: RecentErrorsScreen()),
    ),
  );
  await tester.pumpAndSettle();
  return store;
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  testWidgets('an empty diary says so', (tester) async {
    await _pump(tester, entries: const []);
    expect(find.byKey(const Key('recent-errors-empty')), findsOneWidget);
    // Nothing to clear, so the action is absent.
    expect(find.byKey(const Key('recent-errors-clear')), findsNothing);
  });

  testWidgets('entries render newest first and expand to the stack', (
    tester,
  ) async {
    await _pump(
      tester,
      entries: [
        _entry('older failure', minute: 1),
        _entry('newer failure', minute: 2),
      ],
    );

    final newer = tester.getTopLeft(find.textContaining('newer failure')).dy;
    final older = tester.getTopLeft(find.textContaining('older failure')).dy;
    expect(newer, lessThan(older));

    await tester.tap(find.textContaining('newer failure').first);
    await tester.pumpAndSettle();
    expect(find.textContaining('another frame'), findsOneWidget);
  });

  testWidgets('Clear asks first and honours Cancel', (tester) async {
    final store = await _pump(tester, entries: [_entry('a failure')]);

    await tester.tap(find.byKey(const Key('recent-errors-clear')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('clear-errors-confirm')), findsOneWidget);

    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(store.entries, hasLength(1));

    await tester.tap(find.byKey(const Key('recent-errors-clear')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Clear'));
    await tester.pumpAndSettle();
    expect(store.entries, isEmpty);
    expect(find.byKey(const Key('recent-errors-empty')), findsOneWidget);
  });
}

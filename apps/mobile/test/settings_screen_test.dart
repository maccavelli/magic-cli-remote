import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/features/settings/settings_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';

class _FakeStore extends SettingsStore {
  String themeMode = 'system';
  bool notifications = true;
  String? host = '10.0.0.5:7531';

  @override
  Future<String> getThemeMode() async => themeMode;
  @override
  Future<void> setThemeMode(String mode) async => themeMode = mode;
  @override
  Future<bool> getNotificationsEnabled() async => notifications;
  @override
  Future<void> setNotificationsEnabled(bool enabled) async =>
      notifications = enabled;
  @override
  Future<String?> getHost() async => host;
}

void main() {
  testWidgets('selecting a theme updates the provider and persists', (
    tester,
  ) async {
    final store = _FakeStore();
    final container = ProviderContainer(
      overrides: [settingsStoreProvider.overrideWithValue(store)],
    );
    addTearDown(container.dispose);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(home: SettingsScreen()),
      ),
    );
    await tester.pumpAndSettle();

    expect(container.read(themeModeProvider), ThemeMode.system);

    await tester.tap(find.text('Dark'));
    await tester.pumpAndSettle();

    expect(container.read(themeModeProvider), ThemeMode.dark);
    expect(store.themeMode, 'dark');
  });

  testWidgets('toggling agent alerts persists the preference', (tester) async {
    final store = _FakeStore();
    final container = ProviderContainer(
      overrides: [settingsStoreProvider.overrideWithValue(store)],
    );
    addTearDown(container.dispose);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(home: SettingsScreen()),
      ),
    );
    await tester.pumpAndSettle();

    // Starts on (fake default), then turn it off.
    expect(store.notifications, isTrue);
    await tester.tap(find.byType(SwitchListTile));
    await tester.pumpAndSettle();
    expect(store.notifications, isFalse);
  });
}

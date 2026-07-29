import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart' show TlsMode;
import 'package:magic_cli_remote/features/settings/settings_screen.dart';
import 'package:magic_cli_remote/state/app_providers.dart';

class _FakeStore extends SettingsStore {
  String themeMode = 'system';
  bool notifications = true;
  bool notifyAsks = true;
  bool notifyTurnComplete = true;
  bool notifyErrors = true;
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
  Future<bool> getNotifyAsks() async => notifyAsks;
  @override
  Future<void> setNotifyAsks(bool v) async => notifyAsks = v;
  @override
  Future<bool> getNotifyTurnComplete() async => notifyTurnComplete;
  @override
  Future<void> setNotifyTurnComplete(bool v) async => notifyTurnComplete = v;
  @override
  Future<bool> getNotifyErrors() async => notifyErrors;
  @override
  Future<void> setNotifyErrors(bool v) async => notifyErrors = v;
  @override
  Future<String?> getHost() async => host;
  @override
  Future<String?> getDefaultThinkingLevel() async => null;
  @override
  Future<String?> getDefaultSessionMode(String provider) async => null;
  @override
  Future<bool> getSendWithEnter() async => true;
  @override
  Future<List<String>> getPinnedCwds() async => const [];
  @override
  Future<List<String>> getRecentCwds() async => const [];
  @override
  Future<String?> getRelayUrl() async => null;
  @override
  Future<String?> getRelayHostId() async => null;
  @override
  Future<String?> getRelayAuthority() async => null;
  @override
  Future<String?> getDeviceId() async => null;
  @override
  Future<({String fingerprint, TlsMode mode})?> getPinnedCert(
    String hostInput, {
    String? deviceId,
    bool fallbackToPersistedIdentity = false,
  }) async => null;
  @override
  Future<({String cert, String key})?> getClientCertAndKey() async => null;
}

void main() {
  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

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
    // There are several SwitchListTiles now (B3 kind toggles); hit the master.
    expect(store.notifications, isTrue);
    await tester.tap(find.widgetWithText(SwitchListTile, 'Agent alerts'));
    await tester.pumpAndSettle();
    expect(store.notifications, isFalse);
  });
}

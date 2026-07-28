import 'dart:async';

import 'package:flutter/material.dart' show ThemeMode;
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../data/local/settings_store.dart';
import '../data/notifications/notification_coordinator.dart';
import '../data/ws/mcremote_client.dart';

export '../data/protocol/models.dart';
export '../data/ws/mc_exception.dart';
export '../data/ws/mcremote_client.dart' show McConnectionState, McremoteClient;

final settingsStoreProvider = Provider<SettingsStore>((ref) {
  return SettingsStore();
});

final mcremoteClientProvider = Provider<McremoteClient>((ref) {
  // Share the settings store with the UI layer: a second private instance
  // would keep its own secure-storage state and re-run migrations.
  final client = McremoteClient(settings: ref.watch(settingsStoreProvider));
  ref.onDispose(() {
    client.dispose();
  });
  return client;
});

/// A one-shot in-memory destination captured when a notification deep link
/// reaches an unpaired app. It is intentionally not persisted: a replayed
/// notification must not surprise a later, unrelated pairing.
class PendingNavigationController {
  String? _location;

  void remember(Uri uri) {
    if (uri.scheme.isNotEmpty || uri.hasAuthority) return;
    final segments = uri.pathSegments;
    if (segments.length != 2 ||
        segments.first != 'sessions' ||
        segments.last.trim().isEmpty) {
      return;
    }
    _location = uri.toString();
  }

  String? take() {
    final value = _location;
    _location = null;
    return value;
  }

  void clear() => _location = null;
}

final pendingNavigationProvider = Provider<PendingNavigationController>((ref) {
  return PendingNavigationController();
});

/// App theme mode, persisted to settings. Defaults to system until loaded.
class ThemeModeController extends Notifier<ThemeMode> {
  @override
  ThemeMode build() {
    unawaited(_load());
    return ThemeMode.system;
  }

  Future<void> _load() async {
    state = _parse(await ref.read(settingsStoreProvider).getThemeMode());
  }

  Future<void> set(ThemeMode mode) async {
    state = mode;
    await ref.read(settingsStoreProvider).setThemeMode(mode.name);
  }

  static ThemeMode _parse(String s) => switch (s) {
    'light' => ThemeMode.light,
    'dark' => ThemeMode.dark,
    _ => ThemeMode.system,
  };
}

final themeModeProvider = NotifierProvider<ThemeModeController, ThemeMode>(
  ThemeModeController.new,
);

/// Owns the local-notification + foreground-service layer. Long-lived; started
/// from the app lifecycle scope.
final notificationCoordinatorProvider = Provider<NotificationCoordinator>((
  ref,
) {
  final client = ref.watch(mcremoteClientProvider);
  final coord = NotificationCoordinator(client: client);
  ref.onDispose(coord.dispose);
  return coord;
});

final connectionStateProvider = StreamProvider<McConnectionState>((ref) {
  final client = ref.watch(mcremoteClientProvider);
  // Seed current state then stream updates
  return Stream<McConnectionState>.multi((controller) {
    controller.add(client.state);
    final sub = client.connectionStates.listen(
      controller.add,
      onError: controller.addError,
      onDone: controller.close,
    );
    controller.onCancel = sub.cancel;
  });
});

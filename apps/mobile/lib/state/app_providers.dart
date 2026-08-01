import 'dart:async';

import 'package:flutter/material.dart' show ThemeMode;
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../data/local/settings_store.dart';
import '../data/notifications/notification_coordinator.dart';
import '../data/ws/mcremote_client.dart';
import '../data/ws/transport_probes.dart';

export '../data/protocol/models.dart';
export '../data/protocol/transport_policy.dart';
export '../data/ws/mc_exception.dart';
export '../data/ws/mcremote_client.dart' show McConnectionState, McremoteClient;

final settingsStoreProvider = Provider<SettingsStore>((ref) {
  return SettingsStore();
});

/// Transport reachability probes (MADR 0062 D2).
///
/// A provider rather than a direct call so widget tests can answer "is the
/// mesh up" deterministically instead of dialling the machine running them.
final transportProbesProvider = Provider<TransportProbes>((ref) {
  return const TransportProbes();
});

final mcremoteClientProvider = Provider<McremoteClient>((ref) {
  // Share the settings store with the UI layer: a second private instance
  // would keep its own secure-storage state and re-run migrations.
  final client = McremoteClient(
    settings: ref.watch(settingsStoreProvider),
    probes: ref.watch(transportProbesProvider),
  );
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
  bool _userChanged = false;
  @override
  ThemeMode build() {
    unawaited(_load());
    return ThemeMode.system;
  }

  Future<void> _load() async {
    final loaded = _parse(await ref.read(settingsStoreProvider).getThemeMode());
    if (!_userChanged) state = loaded;
  }

  Future<void> set(ThemeMode mode) async {
    _userChanged = true;
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

/// Whether Enter sends the prompt (true) or inserts a newline (false).
/// Default true preserves existing installs (MADR 0052 B6).
class SendWithEnterController extends Notifier<bool> {
  bool _userChanged = false;
  @override
  bool build() {
    unawaited(_load());
    return true;
  }

  Future<void> _load() async {
    final v = await ref.read(settingsStoreProvider).getSendWithEnter();
    if (!_userChanged) state = v;
  }

  Future<void> set(bool value) async {
    _userChanged = true;
    state = value;
    await ref.read(settingsStoreProvider).setSendWithEnter(value);
  }
}

final sendWithEnterProvider = NotifierProvider<SendWithEnterController, bool>(
  SendWithEnterController.new,
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

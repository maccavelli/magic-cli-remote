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
  final client = McremoteClient();
  ref.onDispose(() {
    client.dispose();
  });
  return client;
});

/// Owns the local-notification + foreground-service layer. Long-lived; started
/// from the app lifecycle scope.
final notificationCoordinatorProvider = Provider<NotificationCoordinator>((ref) {
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

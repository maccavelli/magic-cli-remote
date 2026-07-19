import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../data/local/settings_store.dart';
import '../data/protocol/models.dart';
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

final sessionEventsProvider = StreamProvider<SessionEvent>((ref) {
  final client = ref.watch(mcremoteClientProvider);
  return client.events;
});

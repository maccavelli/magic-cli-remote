import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../data/chat/chat_models.dart';
import '../data/chat/transcript_reducer.dart';
import 'app_providers.dart';

export '../data/chat/chat_models.dart';
export '../data/chat/transcript_reducer.dart'
    show applySessionEvent, clearPendingPermission, markCancelAnnounced;

class TranscriptsNotifier extends Notifier<TranscriptsState> {
  StreamSubscription<SessionEvent>? _sub;

  @override
  TranscriptsState build() {
    ref.keepAlive();
    final client = ref.watch(mcremoteClientProvider);
    _sub?.cancel();
    _sub = client.events.listen(_onEvent);
    ref.onDispose(() {
      _sub?.cancel();
      _sub = null;
    });
    return const TranscriptsState();
  }

  void _onEvent(SessionEvent ev) {
    final id = ev.sessionId;
    if (id.isEmpty) return;
    final current = state.forSession(id);
    final next = applySessionEvent(current, ev);
    // applySessionEvent returns the same instance when the event is a no-op.
    if (identical(next, current)) return;
    state = state.upsert(next);
  }

  void clearSession(String sessionId) {
    state = state.remove(sessionId);
  }

  void clearAll() {
    state = state.clearAll();
  }

  void clearPending(String sessionId, {String? permissionId}) {
    final current = state.byId[sessionId];
    if (current == null) return;
    state = state.upsert(
      clearPendingPermission(current, permissionId: permissionId),
    );
  }

  /// Local cancel announcement before server `turn_complete` arrives.
  void announceCancel(String sessionId) {
    final current = state.forSession(sessionId);
    state = state.upsert(markCancelAnnounced(current));
  }
}

final transcriptsProvider =
    NotifierProvider<TranscriptsNotifier, TranscriptsState>(
  TranscriptsNotifier.new,
);

final sessionTranscriptProvider =
    Provider.family<SessionTranscript, String>((ref, sessionId) {
  return ref.watch(transcriptsProvider).forSession(sessionId);
});

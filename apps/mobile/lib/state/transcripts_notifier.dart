import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../data/chat/chat_models.dart';
import '../data/chat/transcript_reducer.dart';
import 'app_providers.dart';

export '../data/chat/chat_models.dart';
export '../data/chat/transcript_reducer.dart'
    show
        applyMetaStatus,
        applySessionEvent,
        clearPendingPermission,
        markCancelAnnounced;

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
    final next = clearPendingPermission(current, permissionId: permissionId);
    if (identical(next, current)) return;
    state = state.upsert(next);
  }

  /// Local cancel announcement before server `turn_complete` arrives.
  void announceCancel(String sessionId) {
    // peek, not forSession: announcing on an unknown id would materialise a
    // permanent empty transcript.
    final current = state.peek(sessionId);
    if (current == null) return;
    final next = markCancelAnnounced(current);
    if (identical(next, current)) return;
    state = state.upsert(next);
  }

  /// Reconcile against `session.list`: adopt authoritative status for sessions
  /// whose `turn_complete` we may have missed across a socket drop, and evict
  /// transcripts for sessions the host no longer knows about.
  void syncFromMeta(List<SessionMeta> metas) {
    var next = state.retainOnly(metas.map((m) => m.id).toSet());
    for (final m in metas) {
      final current = next.byId[m.id];
      if (current == null) continue;
      final synced = applyMetaStatus(current, m.status, live: m.live);
      if (identical(synced, current)) continue;
      next = next.upsert(synced);
    }
    if (identical(next, state)) return;
    state = next;
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

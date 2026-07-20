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

  /// Replay recorded history into a session whose transcript is still empty.
  ///
  /// Race discipline: the caller fetches history *because* the transcript was
  /// empty at chat-open time, but live `event`s may have populated it while the
  /// `session.history` request was in flight. Live events are authoritative and
  /// more current, so if anything arrived meanwhile we drop history entirely
  /// rather than risk double-applying chunks — apply only if the transcript is
  /// STILL empty. History events share the exact JSON shape of live events, so
  /// they go through [applySessionEvent] in order, just like the live path.
  void replayHistory(String sessionId, List<SessionEvent> events) {
    if (events.isEmpty) return;
    // "Empty" is measured by items, matching the trigger condition: an empty
    // transcript may already carry commands/status without any chat items.
    final current = state.peek(sessionId);
    if (current != null && current.items.isNotEmpty) return;
    var t = current ?? state.forSession(sessionId);
    for (final ev in events) {
      t = applySessionEvent(t, ev);
    }
    if (current != null && identical(t, current)) return;
    state = state.upsert(t);
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

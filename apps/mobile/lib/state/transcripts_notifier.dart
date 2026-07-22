import 'dart:async';

import 'package:flutter/foundation.dart' show visibleForTesting;
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

  /// Highest / lowest daemon-stamped event `seq` seen per session (0 = none).
  /// Side tables, not transcript state, so tracking them never forces a
  /// rebuild. They drive the reconnect resync: history events at or below
  /// [_lastSeq] were already applied live.
  final Map<String, int> _lastSeq = {};
  final Map<String, int> _firstSeq = {};

  void _noteSeq(SessionEvent ev) {
    if (ev.seq <= 0) return;
    final id = ev.sessionId;
    if (id.isEmpty) return;
    final last = _lastSeq[id] ?? 0;
    if (ev.seq > last) _lastSeq[id] = ev.seq;
    final first = _firstSeq[id] ?? 0;
    if (first == 0 || ev.seq < first) _firstSeq[id] = ev.seq;
  }

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

  /// Test hook: inject a live event exactly as the WS stream would.
  @visibleForTesting
  void debugOnEvent(SessionEvent ev) => _onEvent(ev);

  void _onEvent(SessionEvent ev) {
    final id = ev.sessionId;
    if (id.isEmpty) return;
    final current = state.forSession(id);
    // A live replay event (agent re-emitting prior conversation during a
    // session resume) is content we either already display or will fetch via
    // session.history — appending it to a populated transcript duplicates
    // the whole conversation. (Newer daemons don't broadcast these at all;
    // this guards against older ones.)
    if (ev.replay && current.items.isNotEmpty) return;
    _noteSeq(ev);
    final next = applySessionEvent(current, ev);
    // applySessionEvent returns the same instance when the event is a no-op.
    if (identical(next, current)) return;
    state = state.upsert(next);
  }

  void clearSession(String sessionId) {
    _lastSeq.remove(sessionId);
    _firstSeq.remove(sessionId);
    state = state.remove(sessionId);
  }

  void clearAll() {
    _lastSeq.clear();
    _firstSeq.clear();
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
    if (current != null && current.items.isNotEmpty) {
      // Populated transcript: don't drop the fetch on the floor (the old
      // behavior lost the entire recorded conversation whenever one live
      // chunk raced the response) — reconcile via seq instead.
      resyncHistory(sessionId, events);
      return;
    }
    var t = current ?? state.forSession(sessionId);
    for (final ev in events) {
      _noteSeq(ev);
      t = applySessionEvent(t, ev);
    }
    if (current != null && identical(t, current)) return;
    state = state.upsert(t);
  }

  /// Reconcile a populated transcript against a fresh `session.history` fetch
  /// (chat-open races, socket-outage gaps).
  ///
  /// The daemon stamps every event with a per-session monotonic `seq`, carried
  /// identically on the live broadcast and in history. If the fetch contains
  /// anything this client has never seen — newer than our newest ([_lastSeq]),
  /// or older than our oldest ([_firstSeq], the chat-open race where the
  /// transcript started mid-conversation) — the ring is the more complete
  /// record: rebuild from it. Otherwise it is a no-op.
  void resyncHistory(String sessionId, List<SessionEvent> events) {
    if (events.isEmpty) return;
    var maxSeq = 0;
    var minSeq = 0;
    for (final ev in events) {
      if (ev.seq <= 0) continue;
      if (ev.seq > maxSeq) maxSeq = ev.seq;
      if (minSeq == 0 || ev.seq < minSeq) minSeq = ev.seq;
    }
    if (maxSeq == 0) return; // unstamped daemon: no safe merge exists
    final last = _lastSeq[sessionId] ?? 0;
    final first = _firstSeq[sessionId] ?? 0;
    final missedNewer = maxSeq > last;
    final missedOlder = first > 0 && minSeq > 0 && minSeq < first;
    if (!missedNewer && !missedOlder) return;

    var t = SessionTranscript(sessionId: sessionId);
    for (final ev in events) {
      _noteSeq(ev);
      t = applySessionEvent(t, ev);
    }
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
    final liveIds = metas.map((m) => m.id).toSet();
    _lastSeq.removeWhere((id, _) => !liveIds.contains(id));
    _firstSeq.removeWhere((id, _) => !liveIds.contains(id));
    var next = state.retainOnly(liveIds);
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

final sessionTranscriptProvider = Provider.family<SessionTranscript, String>((
  ref,
  sessionId,
) {
  return ref.watch(transcriptsProvider).forSession(sessionId);
});

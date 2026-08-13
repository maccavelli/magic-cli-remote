import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../protocol/models.dart';
import 'chat_models.dart';

/// Runs off the UI isolate so a large cached transcript does not add JSON
/// encoding work to an already busy frame.
String encodeTranscriptCachePayload(Map<String, dynamic> payload) =>
    jsonEncode(payload);

/// Runs off the UI isolate: jsonDecode plus model construction, which together
/// exceeded a frame budget on a large entry and put that cost on chat-open —
/// the interaction the cache exists to make feel instant (MADR 0084 B2/D4).
///
/// Takes the raw payload and the session id as a record because a `compute`
/// entry point is single-argument. Returns null for missing or unusable data,
/// exactly as the in-line version did.
SessionTranscript? decodeTranscriptCachePayload(
  (String, String) rawAndSessionId,
) {
  final (raw, sessionId) = rawAndSessionId;
  try {
    final map = jsonDecode(raw);
    if (map is! Map) return null;
    final itemsRaw = map['items'];
    if (itemsRaw is! List) return null;
    final items = <ChatItem>[];
    for (final e in itemsRaw) {
      if (e is Map<String, dynamic>) {
        items.add(ChatItem.fromJson(e));
      } else if (e is Map) {
        items.add(ChatItem.fromJson(Map<String, dynamic>.from(e)));
      }
    }
    List<SessionMode> modes = const [];
    final rawModes = map['modes'];
    if (rawModes is List) {
      modes = [
        for (final e in rawModes)
          if (e is Map) SessionMode.fromJson(Map<String, dynamic>.from(e)),
      ];
    }
    List<CollaborationMode> collab = const [];
    final rawCollab = map['collaborationModes'];
    if (rawCollab is List) {
      collab = [
        for (final e in rawCollab)
          if (e is Map)
            CollaborationMode.fromJson(Map<String, dynamic>.from(e)),
      ];
    }
    final currentModeId = map['currentModeId'] as String?;
    final currentCollabId = map['currentCollaborationModeId'] as String?;
    SessionGoal? goal;
    final rawGoal = map['goal'];
    if (rawGoal is Map) {
      goal = SessionGoal.fromJson(Map<String, dynamic>.from(rawGoal));
    }
    if (items.isEmpty &&
        modes.isEmpty &&
        collab.isEmpty &&
        currentModeId == null &&
        currentCollabId == null &&
        goal == null) {
      return null;
    }
    final toolIndex = <String, int>{};
    for (var i = 0; i < items.length; i++) {
      final id = items[i].toolId;
      if (id != null && id.isNotEmpty && items[i].kind == ChatItemKind.tool) {
        toolIndex[id] = i;
      }
    }
    var nextSeq = (map['nextSeq'] as num?)?.toInt() ?? 0;
    if (nextSeq <= 0) {
      nextSeq =
          items.map((i) => i.seq).fold<int>(0, (a, b) => a > b ? a : b) + 1;
    }
    // A cached 'running' is always stale: the turn it described ended (or
    // died) with the process. Restoring it would wedge the composer in
    // queue mode whenever the host ring is gone (daemon restart) and no
    // later event moves the status on. Live/history state re-establishes
    // a genuine running turn on its own.
    var status = (map['status'] as String?) ?? 'idle';
    if (status == 'running') status = 'idle';
    return SessionTranscript(
      sessionId: sessionId,
      items: items,
      status: status,
      toolIndex: toolIndex,
      nextSeq: nextSeq,
      modes: modes,
      currentModeId: currentModeId,
      collaborationModes: collab,
      currentCollaborationModeId: currentCollabId,
      goal: goal,
      // The snapshot may end mid-conversation; the next live chunk must
      // not merge into a restored bubble (it may be a different turn).
      sealedTail: true,
    );
  } catch (e, st) {
    debugPrint('TranscriptCache decode failed: $e\n$st');
    return null;
  }
}

/// Best-effort phone-side transcript durability for process death reopen
/// (MADR 0018 E1 / C16). Host history remains source of truth; this is a
/// last-N item snapshot only, not a full archive.
class TranscriptCache {
  TranscriptCache({SharedPreferences? prefs}) : _prefsOverride = prefs;

  static const _indexKey = 'tx_cache_v1_index';
  static const _entryPrefix = 'tx_cache_v1_';

  final SharedPreferences? _prefsOverride;
  SharedPreferences? _prefs;

  /// Tail of the mutation queue. Every index write is a read-modify-write of
  /// [_indexKey]; two debounced saves interleaving across their awaits would
  /// drop one session from the index while its entry blob stays stored —
  /// invisible to LRU eviction and [clear], so prefs grow without bound.
  Future<void> _serial = Future<void>.value();

  @visibleForTesting
  Future<void> get debugWhenIdle => _serial;

  Future<void> _serialized(Future<void> Function() op) {
    final next = _serial.then((_) => op());
    // Keep the chain alive after a failed op; errors surface to the caller.
    _serial = next.catchError((_) {});
    return next;
  }

  Future<SharedPreferences> get _p async =>
      _prefs ??= _prefsOverride ?? await SharedPreferences.getInstance();

  Future<void> save(String sessionId, SessionTranscript t) =>
      _serialized(() => _save(sessionId, t));

  Future<void> _save(String sessionId, SessionTranscript t) async {
    if (sessionId.isEmpty) return;
    final items = t.items;
    if (items.isEmpty && !t.hasControlState) {
      // _remove, not remove: a serialized call from inside the chain would
      // queue behind this op and deadlock.
      await _remove(sessionId);
      return;
    }
    final tail = items.length > kTranscriptCacheMaxItems
        ? items.sublist(items.length - kTranscriptCacheMaxItems)
        : items;
    final payload = <String, dynamic>{
      'sessionId': sessionId,
      'status': t.status,
      'nextSeq': t.nextSeq,
      'items': [for (final i in tail) i.toJson()],
      if (t.modes.isNotEmpty) 'modes': [for (final m in t.modes) m.toJson()],
      if (t.currentModeId != null) 'currentModeId': t.currentModeId,
      if (t.collaborationModes.isNotEmpty)
        'collaborationModes': [
          for (final m in t.collaborationModes) m.toJson(),
        ],
      if (t.currentCollaborationModeId != null)
        'currentCollaborationModeId': t.currentCollaborationModeId,
      if (t.goal != null) 'goal': t.goal!.toJson(),
    };
    final encoded = await compute(encodeTranscriptCachePayload, payload);
    // Soft size guard: SharedPreferences is not a blob store.
    if (encoded.length > 400 * 1024) {
      // Drop older half of the tail and retry once.
      final half = tail.sublist(tail.length ~/ 2);
      final smallerPayload = <String, dynamic>{
        'sessionId': sessionId,
        'status': t.status,
        'nextSeq': t.nextSeq,
        'items': [for (final i in half) i.toJson()],
        if (t.modes.isNotEmpty) 'modes': [for (final m in t.modes) m.toJson()],
        if (t.currentModeId != null) 'currentModeId': t.currentModeId,
        if (t.collaborationModes.isNotEmpty)
          'collaborationModes': [
            for (final m in t.collaborationModes) m.toJson(),
          ],
        if (t.currentCollaborationModeId != null)
          'currentCollaborationModeId': t.currentCollaborationModeId,
        if (t.goal != null) 'goal': t.goal!.toJson(),
      };
      final smaller = await compute(
        encodeTranscriptCachePayload,
        smallerPayload,
      );
      if (smaller.length > 400 * 1024) {
        // Cannot store a current snapshot; drop the old one rather than let
        // a stale transcript hydrate after process death.
        await _remove(sessionId);
        return;
      }
      await _writeEntry(sessionId, smaller);
      return;
    }
    await _writeEntry(sessionId, encoded);
  }

  Future<void> _writeEntry(String sessionId, String payload) async {
    final p = await _p;
    await p.setString('$_entryPrefix$sessionId', payload);
    final index = p.getStringList(_indexKey) ?? <String>[];
    index.remove(sessionId);
    index.add(sessionId);
    while (index.length > kTranscriptCacheMaxSessions) {
      final drop = index.removeAt(0);
      await p.remove('$_entryPrefix$drop');
    }
    await p.setStringList(_indexKey, index);
  }

  /// Returns a transcript snapshot, or null if missing/corrupt.
  Future<SessionTranscript?> load(String sessionId) async {
    if (sessionId.isEmpty) return null;
    final p = await _p;
    final raw = p.getString('$_entryPrefix$sessionId');
    if (raw == null || raw.isEmpty) return null;
    // Symmetric with save()'s compute(): the decode is the larger half of the
    // work and used to run on the UI thread (MADR 0084 B2).
    return compute(decodeTranscriptCachePayload, (raw, sessionId));
  }

  Future<void> remove(String sessionId) =>
      _serialized(() => _remove(sessionId));

  /// Removes phone snapshots for sessions absent from an authoritative
  /// `session.list`. This is serialized with saves/removes so an old debounce
  /// cannot revive an entry after it was evicted.
  Future<void> retainOnly(Set<String> liveIds) =>
      _serialized(() => _retainOnly(liveIds));

  Future<void> _retainOnly(Set<String> liveIds) async {
    final p = await _p;
    final keys = p.getKeys().where(_isEntryKey).toList(growable: false);
    final surviving = <String>{};
    for (final key in keys) {
      final id = key.substring(_entryPrefix.length);
      if (liveIds.contains(id)) {
        surviving.add(id);
      } else {
        await p.remove(key);
      }
    }
    final kept = (p.getStringList(_indexKey) ?? const <String>[])
        .where(surviving.contains)
        .toList();
    // Re-adopt blobs that are stored but unindexed. Released builds stranded
    // every entry this way, and an unindexed blob is invisible to both
    // eviction and [clear]. Their age is unknown, so they go to the LRU end
    // and are the first candidates to be evicted.
    final recovered = surviving.where((id) => !kept.contains(id));
    await p.setStringList(_indexKey, [...recovered, ...kept]);
  }

  /// The index key starts with [_entryPrefix] too, so a prefix sweep reads it
  /// as a session called `index`. Deleting it emptied the LRU index on every
  /// sessions refresh and every reconnect, which left every stored blob
  /// outside eviction and [clear] (MADR 0046 H-C).
  static bool _isEntryKey(String key) =>
      key.startsWith(_entryPrefix) && key != _indexKey;

  Future<void> _remove(String sessionId) async {
    final p = await _p;
    await p.remove('$_entryPrefix$sessionId');
    final index = p.getStringList(_indexKey) ?? <String>[];
    if (index.remove(sessionId)) {
      await p.setStringList(_indexKey, index);
    }
  }

  Future<void> clear() => _serialized(_clear);

  Future<void> _clear() async {
    final p = await _p;
    // Sweep by key prefix, not the index: entries orphaned by any historical
    // index loss must not survive a full clear. Also removes the index key
    // itself (it shares the prefix).
    final keys = p
        .getKeys()
        .where((k) => k.startsWith(_entryPrefix))
        .toList(growable: false);
    for (final k in keys) {
      await p.remove(k);
    }
  }

  /// Count sessions and approximate bytes held by the cache (MADR 0052 B4).
  ///
  /// Only keys under [_entryPrefix] listed in the index contribute to
  /// [sessions]; byte total sums entry blobs (and the index string).
  Future<({int sessions, int bytes})> usage() async {
    final p = await _p;
    final index = p.getStringList(_indexKey) ?? const <String>[];
    var bytes = 0;
    // Index is a string list, not a single string — sum id lengths.
    for (final id in index) {
      bytes += id.length;
    }
    var sessions = 0;
    for (final id in index) {
      final raw = p.getString('$_entryPrefix$id');
      if (raw == null) continue;
      sessions++;
      bytes += raw.length;
    }
    return (sessions: sessions, bytes: bytes);
  }
}

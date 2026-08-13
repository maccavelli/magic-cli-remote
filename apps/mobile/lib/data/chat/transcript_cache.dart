import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:path_provider/path_provider.dart';
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
  TranscriptCache({SharedPreferences? prefs, Directory? directory})
    : _prefsOverride = prefs,
      _dirOverride = directory;

  /// The LRU index stays in preferences: a short list of session ids is
  /// exactly the small key-value state preferences are for. Only the blobs
  /// moved to files (MADR 0084 D3).
  static const _indexKey = 'tx_cache_v1_index';

  /// Set once the prefs-era blobs have been rewritten as files.
  static const _migratedKey = 'tx_cache_migrated_v2';

  /// The prefix the v1 blobs used, still read by the migration.
  static const _legacyEntryPrefix = 'tx_cache_v1_';

  static const _dirName = 'transcripts';

  final SharedPreferences? _prefsOverride;
  SharedPreferences? _prefs;
  final Directory? _dirOverride;
  Directory? _dir;

  /// Tail of the mutation queue. Every index write is a read-modify-write of
  /// [_indexKey]; two debounced saves interleaving across their awaits would
  /// drop one session from the index while its entry file stays on disk —
  /// invisible to LRU eviction and [clear], so the directory grows without
  /// bound.
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

  Future<Directory> get _directory async {
    final existing = _dir;
    if (existing != null) return existing;
    final base = _dirOverride ?? await getApplicationSupportDirectory();
    final dir = Directory('${base.path}/$_dirName');
    if (!dir.existsSync()) await dir.create(recursive: true);
    _dir = dir;
    await _migrateLegacyEntries(dir);
    return dir;
  }

  /// One-way move of the v1 prefs blobs into files (MADR 0084 D3).
  ///
  /// On **any** failure the cache starts empty rather than blocking startup:
  /// it is a re-fetchable snapshot of host-owned history (MADR 0018 E1), so
  /// losing it costs one round trip, while a migration that throws on boot
  /// would cost the whole app.
  Future<void> _migrateLegacyEntries(Directory dir) async {
    try {
      final p = await _p;
      if (p.getBool(_migratedKey) ?? false) return;
      final legacyKeys = p
          .getKeys()
          .where((k) => k.startsWith(_legacyEntryPrefix) && k != _indexKey)
          .toList(growable: false);
      for (final key in legacyKeys) {
        final id = key.substring(_legacyEntryPrefix.length);
        final raw = p.getString(key);
        if (raw != null && raw.isNotEmpty) {
          await _writeFile(dir, id, raw);
        }
        await p.remove(key);
      }
      await p.setBool(_migratedKey, true);
    } catch (e) {
      debugPrint('TranscriptCache migration failed; starting empty: $e');
      try {
        final p = await _p;
        for (final k in p.getKeys().where(
          (k) => k.startsWith(_legacyEntryPrefix),
        )) {
          await p.remove(k);
        }
        await p.setStringList(_indexKey, const []);
        await p.setBool(_migratedKey, true);
      } catch (_) {
        // Nothing further to try; the cache simply stays cold.
      }
    }
  }

  /// A session id is host-supplied, so it must not be able to name a path
  /// outside the transcripts directory. Percent-encoding also keeps ids
  /// containing `/` addressable at all — something the flat prefs key space
  /// made impossible to get wrong.
  static String _fileName(String sessionId) =>
      '${Uri.encodeComponent(sessionId)}.json';

  static String _sessionIdFromFile(String name) =>
      Uri.decodeComponent(name.substring(0, name.length - '.json'.length));

  File _entryFile(Directory dir, String sessionId) =>
      File('${dir.path}/${_fileName(sessionId)}');

  /// Atomic: a torn write must never be readable as a transcript.
  static Future<void> _writeFile(
    Directory dir,
    String sessionId,
    String payload,
  ) async {
    final target = File('${dir.path}/${_fileName(sessionId)}');
    final tmp = File('${target.path}.tmp');
    await tmp.writeAsString(payload, flush: true);
    await tmp.rename(target.path);
  }

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
    // No size cap and no halve-and-retry: a file has no such limit, and
    // kTranscriptCacheMaxItems already bounds the entry. The old code had to
    // *delete the user's snapshot* when it would not fit a preferences-shaped
    // budget (MADR 0084 D3).
    final encoded = await compute(encodeTranscriptCachePayload, payload);
    await _writeEntry(sessionId, encoded);
  }

  Future<void> _writeEntry(String sessionId, String payload) async {
    final dir = await _directory;
    final p = await _p;
    await _writeFile(dir, sessionId, payload);
    final index = p.getStringList(_indexKey) ?? <String>[];
    index.remove(sessionId);
    index.add(sessionId);
    while (index.length > kTranscriptCacheMaxSessions) {
      final drop = index.removeAt(0);
      await _deleteFile(dir, drop);
    }
    await p.setStringList(_indexKey, index);
  }

  static Future<void> _deleteFile(Directory dir, String sessionId) async {
    final f = File('${dir.path}/${_fileName(sessionId)}');
    if (f.existsSync()) await f.delete();
  }

  /// Returns a transcript snapshot, or null if missing/corrupt.
  Future<SessionTranscript?> load(String sessionId) async {
    if (sessionId.isEmpty) return null;
    final dir = await _directory;
    final file = _entryFile(dir, sessionId);
    if (!file.existsSync()) return null;
    final raw = await file.readAsString();
    if (raw.isEmpty) return null;
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
    final dir = await _directory;
    final p = await _p;
    final surviving = <String>{};
    for (final id in await _storedIds(dir)) {
      if (liveIds.contains(id)) {
        surviving.add(id);
      } else {
        await _deleteFile(dir, id);
      }
    }
    final kept = (p.getStringList(_indexKey) ?? const <String>[])
        .where(surviving.contains)
        .toList();
    // Re-adopt files that are stored but unindexed. Released builds stranded
    // every entry this way, and an unindexed entry is invisible to both
    // eviction and [clear]. Their age is unknown, so they go to the LRU end
    // and are the first candidates to be evicted.
    final recovered = surviving.where((id) => !kept.contains(id));
    await p.setStringList(_indexKey, [...recovered, ...kept]);
  }

  /// Session ids with an entry file on disk, indexed or not.
  static Future<List<String>> _storedIds(Directory dir) async {
    if (!dir.existsSync()) return const [];
    final out = <String>[];
    await for (final e in dir.list()) {
      final name = e.path.split('/').last;
      if (e is File && name.endsWith('.json')) {
        out.add(_sessionIdFromFile(name));
      }
    }
    return out;
  }

  Future<void> _remove(String sessionId) async {
    final dir = await _directory;
    final p = await _p;
    await _deleteFile(dir, sessionId);
    final index = p.getStringList(_indexKey) ?? <String>[];
    if (index.remove(sessionId)) {
      await p.setStringList(_indexKey, index);
    }
  }

  Future<void> clear() => _serialized(_clear);

  Future<void> _clear() async {
    final dir = await _directory;
    final p = await _p;
    // Sweep the directory, not the index: entries orphaned by any historical
    // index loss must not survive a full clear (MADR 0046 H-C).
    for (final id in await _storedIds(dir)) {
      await _deleteFile(dir, id);
    }
    await p.remove(_indexKey);
  }

  /// Count sessions and bytes held by the cache (MADR 0052 B4).
  ///
  /// Stats the entry files rather than reading them: this backs a one-line
  /// subtitle in Settings, and used to touch every stored blob to do it
  /// (MADR 0084 B3).
  Future<({int sessions, int bytes})> usage() async {
    final dir = await _directory;
    final p = await _p;
    final index = p.getStringList(_indexKey) ?? const <String>[];
    var bytes = 0;
    var sessions = 0;
    for (final id in index) {
      final f = _entryFile(dir, id);
      if (!f.existsSync()) continue;
      sessions++;
      bytes += await f.length();
    }
    return (sessions: sessions, bytes: bytes);
  }
}

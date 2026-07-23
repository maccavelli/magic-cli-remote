import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'chat_models.dart';

/// Best-effort phone-side transcript durability for process death reopen
/// (MADR 0018 E1 / C16). Host history remains source of truth; this is a
/// last-N item snapshot only, not a full archive.
class TranscriptCache {
  TranscriptCache({SharedPreferences? prefs}) : _prefsOverride = prefs;

  static const _indexKey = 'tx_cache_v1_index';
  static const _entryPrefix = 'tx_cache_v1_';

  final SharedPreferences? _prefsOverride;
  SharedPreferences? _prefs;

  Future<SharedPreferences> get _p async =>
      _prefs ??= _prefsOverride ?? await SharedPreferences.getInstance();

  Future<void> save(String sessionId, SessionTranscript t) async {
    if (sessionId.isEmpty) return;
    final items = t.items;
    if (items.isEmpty) {
      await remove(sessionId);
      return;
    }
    final tail = items.length > kTranscriptCacheMaxItems
        ? items.sublist(items.length - kTranscriptCacheMaxItems)
        : items;
    final payload = jsonEncode({
      'sessionId': sessionId,
      'status': t.status,
      'nextSeq': t.nextSeq,
      'items': [for (final i in tail) i.toJson()],
    });
    // Soft size guard: SharedPreferences is not a blob store.
    if (payload.length > 400 * 1024) {
      // Drop older half of the tail and retry once.
      final half = tail.sublist(tail.length ~/ 2);
      final smaller = jsonEncode({
        'sessionId': sessionId,
        'status': t.status,
        'nextSeq': t.nextSeq,
        'items': [for (final i in half) i.toJson()],
      });
      if (smaller.length > 400 * 1024) return;
      await _writeEntry(sessionId, smaller);
      return;
    }
    await _writeEntry(sessionId, payload);
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
      if (items.isEmpty) return null;
      final toolIndex = <String, int>{};
      for (var i = 0; i < items.length; i++) {
        final id = items[i].toolId;
        if (id != null &&
            id.isNotEmpty &&
            items[i].kind == ChatItemKind.tool) {
          toolIndex[id] = i;
        }
      }
      var nextSeq = (map['nextSeq'] as num?)?.toInt() ?? 0;
      if (nextSeq <= 0) {
        nextSeq = items.map((i) => i.seq).fold<int>(0, (a, b) => a > b ? a : b) +
            1;
      }
      return SessionTranscript(
        sessionId: sessionId,
        items: items,
        status: (map['status'] as String?) ?? 'idle',
        toolIndex: toolIndex,
        nextSeq: nextSeq,
      );
    } catch (e, st) {
      debugPrint('TranscriptCache.load failed: $e\n$st');
      return null;
    }
  }

  Future<void> remove(String sessionId) async {
    final p = await _p;
    await p.remove('$_entryPrefix$sessionId');
    final index = p.getStringList(_indexKey) ?? <String>[];
    if (index.remove(sessionId)) {
      await p.setStringList(_indexKey, index);
    }
  }

  Future<void> clear() async {
    final p = await _p;
    final index = p.getStringList(_indexKey) ?? <String>[];
    for (final id in index) {
      await p.remove('$_entryPrefix$id');
    }
    await p.remove(_indexKey);
  }
}

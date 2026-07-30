import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../data/ws/mcremote_client.dart';
import 'app_providers.dart';
import 'transcripts_notifier.dart';

/// Connection-scoped owner of post-reconnect session reconciliation (MADR 0056
/// H-1 / Phase 2).
///
/// On every transition to [McConnectionState.connected], fetches one
/// authoritative session list snapshot and history for every locally known
/// session that has sequence bookkeeping or a suspected gap — without
/// requiring a mounted [ChatScreen].
class SessionSynchronizer extends Notifier<int> {
  int _generation = 0;
  StreamSubscription<McConnectionState>? _connSub;
  McConnectionState? _previous;
  Future<void>? _inFlight;

  /// Bumps whenever a resync starts; useful for tests.
  @override
  int build() {
    ref.keepAlive();
    final client = ref.watch(mcremoteClientProvider);
    _previous = client.state;
    _connSub?.cancel();
    _connSub = client.connectionStates.listen((next) {
      final previous = _previous;
      _previous = next;
      if (next == McConnectionState.connected &&
          previous != null &&
          previous != McConnectionState.connected) {
        unawaited(resync());
      }
    });
    ref.onDispose(() {
      _connSub?.cancel();
      _connSub = null;
    });
    return 0;
  }

  /// Full reconnect reconciliation. Safe to call concurrently: a newer call
  /// supersedes in-flight work via a generation counter.
  Future<void> resync() {
    final run = _resyncBody();
    _inFlight = run;
    return run;
  }

  Future<void> _resyncBody() async {
    final gen = ++_generation;
    state = gen;
    final client = ref.read(mcremoteClientProvider);
    final transcripts = ref.read(transcriptsProvider.notifier);

    try {
      final snap = await client.listSessionSnapshot();
      if (gen != _generation) return;
      transcripts.syncFromMeta(snap.sessions, complete: snap.complete);
    } catch (e) {
      debugPrint('SessionSynchronizer list failed: $e');
    }

    final ids = <String>{...transcripts.knownSessionIds()};
    // Also cover host-listed sessions that may lack local seq bookkeeping.
    try {
      final snap = await client.listSessionSnapshot();
      if (gen != _generation) return;
      for (final s in snap.sessions) {
        ids.add(s.id);
      }
    } catch (_) {}

    const concurrency = 2;
    final pending = ids.toList();
    var next = 0;

    Future<void> worker() async {
      while (true) {
        if (gen != _generation) return;
        final i = next++;
        if (i >= pending.length) return;
        final id = pending[i];
        final need =
            transcripts.isGapSuspected(id) || transcripts.lastSeq(id) > 0;
        if (!need) continue;
        try {
          final events = await client.sessionHistory(id);
          if (gen != _generation) return;
          if (events.isEmpty) continue;
          final local = ref.read(transcriptsProvider).peek(id);
          if (local == null || local.items.isEmpty) {
            await transcripts.replayHistory(id, events);
          } else {
            await transcripts.resyncHistory(id, events);
          }
        } catch (e) {
          debugPrint('SessionSynchronizer history $id failed: $e');
        }
      }
    }

    await Future.wait([for (var w = 0; w < concurrency; w++) worker()]);
  }

  /// Ensure one session is reconciled (chat open with suspected gap).
  Future<void> ensureSession(String sessionId) async {
    if (sessionId.isEmpty) return;
    final client = ref.read(mcremoteClientProvider);
    final transcripts = ref.read(transcriptsProvider.notifier);
    try {
      final events = await client.sessionHistory(sessionId);
      final local = ref.read(transcriptsProvider).peek(sessionId);
      if (local == null || local.items.isEmpty) {
        await transcripts.replayHistory(sessionId, events);
      } else if (transcripts.isGapSuspected(sessionId) || events.isNotEmpty) {
        await transcripts.resyncHistory(sessionId, events);
      }
    } catch (e) {
      debugPrint('SessionSynchronizer ensureSession $sessionId failed: $e');
    }
  }

  @visibleForTesting
  Future<void>? get debugInFlight => _inFlight;
}

final sessionSynchronizerProvider = NotifierProvider<SessionSynchronizer, int>(
  SessionSynchronizer.new,
);

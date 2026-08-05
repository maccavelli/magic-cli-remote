import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

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

  /// The daemon's seq-lineage epoch from the last snapshot (MADR 0068 P3).
  /// A change means every cached per-session seq is stale — the whole
  /// resync runs in force mode.
  String? _epoch;

  /// Failed-resync retries since the last connected edge, bounded so a
  /// persistently failing daemon cannot turn the re-arm into a hot loop.
  int _retriesThisConnection = 0;
  static const _maxRetriesPerConnection = 3;

  /// Delay before a failed resync is retried; mutable for tests only.
  @visibleForTesting
  static Duration retryDelay = const Duration(seconds: 5);

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
        _retriesThisConnection = 0;
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
    var failed = false;

    // Resume fast path (MADR 0068 D4): when this connection's auth
    // resumed and the daemon confirmed every locally known session is
    // unchanged, the whole reconcile — both list calls included — is
    // skipped. Any miss (uncovered session, moved seq, suspected gap)
    // falls through to the ordinary truth path below; pending-asks
    // reconciliation is connection-scoped in the coordinator and runs
    // regardless.
    final resumed = client.lastResumed;
    if (resumed != null) {
      final known = transcripts.knownSessionIds();
      final coveredAndUnchanged =
          known.isNotEmpty &&
          known.every((id) {
            final b = resumed[id];
            return b != null &&
                !transcripts.isGapSuspected(id) &&
                transcripts.lastSeq(id) == b.latest;
          });
      if (coveredAndUnchanged) {
        return;
      }
    }

    // Epoch + retained-seq windows from the snapshot (MADR 0068 P3). An
    // epoch change means the daemon's seq counters restarted (unclean
    // exit): every cached seq is untrustworthy, so the walk runs in force
    // mode and the fast-skip below is disabled for this pass.
    var force = false;
    var bounds = const <String, SeqBounds>{};
    try {
      final snap = await client.listSessionSnapshot();
      if (gen != _generation) return;
      if (snap.epoch != null) {
        force = _epoch != null && _epoch != snap.epoch;
        _epoch = snap.epoch;
      }
      bounds = snap.seqs;
      transcripts.syncFromMeta(snap.sessions, complete: snap.complete);
    } catch (e) {
      failed = true;
      debugPrint('SessionSynchronizer list failed: $e');
    }

    final ids = <String>{...transcripts.knownSessionIds()};
    // Also cover host-listed sessions that may lack local seq bookkeeping.
    try {
      final snap = await client.listSessionSnapshot();
      if (gen != _generation) return;
      if (snap.seqs.isNotEmpty) bounds = snap.seqs;
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
        final gap = transcripts.isGapSuspected(id);
        final last = transcripts.lastSeq(id);
        // Gap-scaled fast path (MADR 0068 P3): a session whose cached seq
        // equals the daemon's latest has nothing to fetch — the common
        // 3-second app-switch resume costs the list calls and nothing
        // else. Never taken in force mode or under a suspected gap.
        final b = bounds[id];
        if (!force && !gap && b != null && last == b.latest) {
          continue;
        }
        final need = force || gap || last > 0;
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
          failed = true;
          debugPrint('SessionSynchronizer history $id failed: $e');
        }
      }
    }

    await Future.wait([for (var w = 0; w < concurrency; w++) worker()]);

    // Re-arm on failure (MADR 0068 P3 / A1 finding 29): a resync that
    // errored used to stay broken until the next connect edge, which on
    // iOS may be an app-switch away — or hours away. Bounded retries; a
    // newer generation or a dead connection cancels silently.
    if (failed && gen == _generation) {
      _scheduleRetry(gen);
    }
  }

  void _scheduleRetry(int gen) {
    if (_retriesThisConnection >= _maxRetriesPerConnection) return;
    _retriesThisConnection++;
    Future<void>.delayed(retryDelay, () {
      if (gen != _generation) return;
      final client = ref.read(mcremoteClientProvider);
      if (client.state != McConnectionState.connected) return;
      unawaited(resync());
    });
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

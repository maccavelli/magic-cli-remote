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

    // Resume seq fast path (MADR 0068 D4 + 0072 §6.2 A): when auth resumed
    // and every locally known session's seq matches the daemon's, we still
    // take one list snapshot so `syncFromMeta` can clear sticky `running`
    // after a missed turn_complete — but we skip history workers below.
    // Seq equality does not imply status equality.
    final resumed = client.lastResumed;
    var seqCovered = false;
    if (resumed != null) {
      final known = transcripts.knownSessionIds();
      seqCovered =
          known.isNotEmpty &&
          known.every((id) {
            final b = resumed[id];
            return b != null &&
                !transcripts.isGapSuspected(id) &&
                transcripts.lastSeq(id) == b.latest;
          });
    }

    // One snapshot per pass (0070 F1): epoch, seq bounds, meta sync, and
    // host session ids all come from a single list call. A second list
    // used to enlarge ids but swallowed errors and doubled cost on every
    // connected edge.
    //
    // Epoch change (MADR 0068 P3): daemon seq counters restarted (unclean
    // exit) — every cached seq is untrustworthy, so the walk runs in force
    // mode and the fast-skip below is disabled for this pass.
    var force = false;
    var bounds = const <String, SeqBounds>{};
    final ids = <String>{...transcripts.knownSessionIds()};
    try {
      final snap = await client.listSessionSnapshot();
      if (gen != _generation) return;
      if (snap.epoch != null) {
        force = _epoch != null && _epoch != snap.epoch;
        _epoch = snap.epoch;
      }
      bounds = snap.seqs;
      transcripts.syncFromMeta(snap.sessions, complete: snap.complete);
      for (final s in snap.sessions) {
        ids.add(s.id);
      }
      // Status reconciled; seqs confirmed unchanged and no epoch force →
      // history would be empty work.
      if (seqCovered && !force) {
        return;
      }
    } catch (e) {
      failed = true;
      debugPrint('SessionSynchronizer list failed: $e');
    }

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
        // Up-to-date under normal force=false (0068 P3).
        if (!force && !gap && b != null && last == b.latest) {
          continue;
        }
        // Empty host ring (0071 F6): nothing to fetch even after an epoch
        // force — latest_seq 0 means the daemon retained no events.
        if (!gap && b != null && b.latest == 0 && last == 0) {
          continue;
        }
        final need = force || gap || last > 0;
        if (!need) continue;
        try {
          // The newest page, not a forward walk from the oldest event. The
          // walk is bounded at kHistoryMaxPages x kHistoryFetchLimit = 6,400
          // events counted from the *oldest*, so on a longer session it
          // returned the beginning of the conversation and never its end
          // (MADR 0141 F2 — a 900-turn session opened on turn 291).
          //
          // It also has to move in lockstep with the chat-open path below:
          // resyncHistory rebuilds from whatever it is handed, and its
          // missedOlder branch would happily rebuild a backward-paged
          // transcript down to the oldest events while the user was reading
          // the newest.
          final events = (await client.sessionHistoryNewest(id)).events;
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
      // Newest page: see resync above. These two call sites must not diverge.
      final events = (await client.sessionHistoryNewest(sessionId)).events;
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

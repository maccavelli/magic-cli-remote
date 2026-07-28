import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/foundation.dart' show debugPrint, visibleForTesting;
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../data/chat/chat_models.dart';
import '../data/chat/transcript_cache.dart';
import '../data/chat/transcript_reducer.dart';
import 'app_providers.dart';

export '../data/chat/chat_models.dart';
export '../data/chat/transcript_reducer.dart'
    show
        applyMetaStatus,
        applySessionEvent,
        clearPendingPermission,
        clearPendingQuestion,
        markCancelAnnounced;

/// High-frequency content events coalesced into one UI notify per window so a
/// burst of WS chunks does not rebuild the chat on every token. 32ms (~30
/// content updates/s) halves rebuild pressure versus per-frame flushing while
/// staying imperceptible for streamed text; discrete events (turn_complete,
/// permissions) still flush immediately.
const Duration kTranscriptBatchWindow = Duration(milliseconds: 32);

/// Debounce for phone-side transcript cache writes (MADR 0018 E1).
const Duration kTranscriptCacheDebounce = Duration(milliseconds: 400);

/// Publish a transcript with exclusive list ownership cleared so the next
/// apply always copies before mutating (MADR 0018 D2).
SessionTranscript _publish(SessionTranscript t) =>
    t.growableItems ? t.copyWith(growableItems: false) : t;

/// Events that may wait for the batch window instead of publishing state at
/// once. Anything not listed here forces an immediate commit, so this set has
/// to cover every type an agent can emit at streaming cadence — `usage_update`
/// in particular arrives per message update on OpenCode, and while it bypassed
/// the window it defeated the window (MADR 0024 §1.1).
///
/// The test is "does a 32ms delay change what the user can do?". Plan, command
/// and usage events are replace-snapshots rendered outside the transcript
/// list. Status, turn, permission, question, error, notice and user_message all
/// gate an affordance or a state machine, so they keep publishing immediately.
///
/// `tool_call` used to be in that immediate set, on the stated grounds that the
/// opening call "gates an affordance". It does not: in the reducer it appends a
/// tool item and nothing else, nothing outside `transcript_reducer.dart` reads
/// it, the composer gates on `status`/`hasBlockingPrompt`, and the notification
/// layer only handles `permission_request`/`turn_complete`. OpenCode fans tool
/// calls out in parallel, so excluding it cost one synchronous commit per tool
/// — five parallel calls measured six commits where the text path takes one
/// (MADR 0042 D2). The 32ms delay before the first card appears is the same one
/// streamed text already accepts.
bool _isBatchableEvent(SessionEvent ev) {
  switch (ev.type) {
    case 'assistant_message_chunk':
    case 'thought_chunk':
    case 'tool_call':
    case 'tool_call_update':
    case 'usage_update':
    case 'plan':
    case 'available_commands':
    case 'remote_commands':
      return true;
    default:
      return false;
  }
}

/// Streaming text runs that [_foldChunks] may merge. Replay events are excluded
/// so the per-event replay guard in [TranscriptsNotifier._flushSession] keeps
/// seeing them one at a time.
bool _isFoldableChunk(SessionEvent ev) =>
    !ev.replay &&
    (ev.type == 'assistant_message_chunk' || ev.type == 'thought_chunk');

/// Tool updates [_foldChunks] may collapse. Unlike text these fold by
/// *replacement*, not concatenation: `tool_call_update` is replace-semantics, so
/// within one window only the latest state of a given tool id can matter
/// (MADR 0042 D2).
///
/// Two exclusions are load-bearing. Replay events are excluded for the same
/// reason as text — the per-event replay guard in [TranscriptsNotifier._flushSession]
/// must keep seeing them one at a time. An update carrying **no tool id** is
/// never folded either: `transcript_reducer._upsertTool` folds those into the
/// most recent tool card by arrival order, so dropping one would lose a state
/// transition rather than supersede it.
bool _isFoldableToolUpdate(SessionEvent ev) =>
    !ev.replay &&
    ev.type == 'tool_call_update' &&
    (ev.toolId ?? '').trim().isNotEmpty;

/// Events that arrive at streaming cadence but append nothing to the
/// transcript list, so they can pass through a fold without splitting the text
/// run around them. Reordering one relative to text cannot change the final
/// transcript — it only sets a field — and letting `usage_update` split a run
/// would put the fragmentation straight back (MADR 0024 §2.2 applies the same
/// rule daemon-side).
bool _isNeutralInFold(SessionEvent ev) => ev.type == 'usage_update';

class TranscriptsNotifier extends Notifier<TranscriptsState> {
  StreamSubscription<SessionEvent>? _sub;
  StreamSubscription<McConnectionState>? _connectionSub;
  McConnectionState? _previousConnectionState;

  /// Highest / lowest daemon-stamped event `seq` seen per session (0 = none).
  /// Side tables, not transcript state, so tracking them never forces a
  /// rebuild. They drive the reconnect resync: history events at or below
  /// [_lastSeq] were already applied live.
  final Map<String, int> _lastSeq = {};
  final Map<String, int> _firstSeq = {};
  final Map<String, bool> _seqGapSuspected = {};

  /// Pending batchable events per session, in arrival order.
  final Map<String, List<SessionEvent>> _pending = {};
  Timer? _flushTimer;

  /// Phone-side last-N cache (process death polish). Host history still wins.
  TranscriptCache _cache = TranscriptCache();
  final Map<String, Timer> _cacheTimers = {};

  /// Mirror of the latest published state. Riverpod (correctly) asserts on
  /// touching `state` inside onDispose, but teardown still needs the current
  /// transcripts to flush in-flight text to the cache — so every write goes
  /// through [_setState] and dispose reads the mirror.
  TranscriptsState _mirror = const TranscriptsState();

  void _setState(TranscriptsState next) {
    _mirror = next;
    state = next;
  }

  @visibleForTesting
  set debugCache(TranscriptCache cache) => _cache = cache;

  /// The resync seq window recorded for [sessionId] (0 when nothing is known).
  /// Exposed so tests can assert that chunk folding still notes every event it
  /// merged away.
  @visibleForTesting
  int debugLastSeq(String sessionId) => _lastSeq[sessionId] ?? 0;

  @visibleForTesting
  int debugFirstSeq(String sessionId) => _firstSeq[sessionId] ?? 0;

  void _noteSeq(SessionEvent ev) {
    if (ev.seq <= 0) return;
    final id = ev.sessionId;
    if (id.isEmpty) return;
    final last = _lastSeq[id] ?? 0;
    if (last > 0 && ev.seq > last + 1) _seqGapSuspected[id] = true;
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
    _connectionSub?.cancel();
    _previousConnectionState = client.state;
    _connectionSub = client.connectionStates.listen((next) {
      final previous = _previousConnectionState;
      _previousConnectionState = next;
      if (next == McConnectionState.connected &&
          previous != null &&
          previous != McConnectionState.connected) {
        for (final id in _lastSeq.keys) {
          _seqGapSuspected[id] = true;
        }
      }
    });
    ref.onDispose(() {
      _flushTimer?.cancel();
      _flushTimer = null;
      // Persist stragglers straight to the cache: going through
      // _flushSession/_commit here would write state on a disposing notifier
      // and re-arm debounce timers that later fire against it. Sessions with
      // only a pending debounced save (no batch) are flushed too — their
      // timer dies with us.
      final ids = <String>{..._pending.keys, ..._cacheTimers.keys};
      for (final id in ids) {
        var t = _mirror.forSession(id);
        final hadItemsAtBatchStart = t.items.isNotEmpty;
        final batch = _pending.remove(id);
        for (final ev in batch ?? const <SessionEvent>[]) {
          if (ev.replay && hadItemsAtBatchStart) continue;
          t = applySessionEvent(t, ev);
        }
        _saveCacheBestEffort(id, _publish(t));
      }
      for (final t in _cacheTimers.values) {
        t.cancel();
      }
      _cacheTimers.clear();
      // Raw image bytes for prompts whose echo never arrived. Nothing will
      // consume them now.
      _sentImages.clear();
      _sub?.cancel();
      _sub = null;
      _connectionSub?.cancel();
      _connectionSub = null;
    });
    return const TranscriptsState();
  }

  /// Publish and store [t]. Returns the published copy: callers that keep
  /// applying events MUST continue from the return value, never from [t] —
  /// a still-growable local would mutate the published items list in place
  /// and defeat every identity-based rebuild check (MADR 0018 D2).
  SessionTranscript _commit(String sessionId, SessionTranscript t) {
    final published = _publish(t);
    _setState(state.upsert(published));
    _scheduleCacheSave(sessionId);
    return published;
  }

  void _scheduleCacheSave(String sessionId) {
    _cacheTimers[sessionId]?.cancel();
    _cacheTimers[sessionId] = Timer(kTranscriptCacheDebounce, () {
      _cacheTimers.remove(sessionId);
      final t = state.peek(sessionId);
      if (t != null) _saveCacheBestEffort(sessionId, t);
    });
  }

  /// Cache durability must never turn an otherwise-successful chat update into
  /// an unhandled asynchronous error. The daemon ring remains authoritative;
  /// this bounded cache only improves process-death reopen.
  void _saveCacheBestEffort(String sessionId, SessionTranscript transcript) {
    unawaited(
      _cache.save(sessionId, transcript).catchError((
        Object error,
        StackTrace st,
      ) {
        debugPrint('Transcript cache save failed for $sessionId: $error');
      }),
    );
  }

  /// Seed an empty transcript from the phone-side cache before host history
  /// arrives. Returns true when items were restored. Live/host history still
  /// override via [replayHistory] / [resyncHistory].
  Future<bool> hydrateFromCache(String sessionId) async {
    final current = state.peek(sessionId);
    if (current != null && current.items.isNotEmpty) return false;
    final cached = await _cache.load(sessionId);
    if (cached == null || cached.items.isEmpty) return false;
    final again = state.peek(sessionId);
    if (again != null && again.items.isNotEmpty) return false;
    // An item-less transcript can still carry live state that arrived while
    // the cache was loading (session_status, available_commands, plan,
    // session_mode, session_capabilities, session_config, permission_request
    // all precede chat items); the snapshot must not clobber it with its stale
    // defaults. The cache stores none of these, so anything already live here
    // is strictly better than what the snapshot carries.
    var seeded = cached;
    if (again != null) {
      seeded = seeded.copyWith(
        status: again.status == 'idle' ? null : again.status,
        commands: again.commands.isEmpty ? null : again.commands,
        remoteCommands: again.remoteCommands.isEmpty
            ? null
            : again.remoteCommands,
        plan: again.plan.isEmpty ? null : again.plan,
        modes: again.modes.isEmpty ? null : again.modes,
        currentModeId: again.currentModeId,
        capabilities: again.capabilities,
        configOptions: again.configOptions.isEmpty ? null : again.configOptions,
        pendingPermissions: again.pendingPermissions.isEmpty
            ? null
            : again.pendingPermissions,
        pendingQuestions: again.pendingQuestions.isEmpty
            ? null
            : again.pendingQuestions,
        cancelAnnounced: again.cancelAnnounced ? true : null,
      );
    }
    _setState(state.upsert(seeded));
    return true;
  }

  /// Test hook: inject a live event exactly as the WS stream would, then flush
  /// the batch window so assertions can read state synchronously.
  @visibleForTesting
  void debugOnEvent(SessionEvent ev) {
    _onEvent(ev);
    _flushTimer?.cancel();
    _flushTimer = null;
    _flushAllPending();
  }

  /// Stage [events] as one batch window, then flush once — the ingest path a
  /// real [kTranscriptBatchWindow] takes, without waiting on the timer. Needed
  /// to assert what a window actually costs; [debugOnEvent] flushes per event
  /// and so can never show coalescing.
  @visibleForTesting
  void debugOnEventBatch(List<SessionEvent> events) {
    for (final ev in events) {
      _onEvent(ev);
    }
    _flushTimer?.cancel();
    _flushTimer = null;
    _flushAllPending();
  }

  void _onEvent(SessionEvent ev) {
    final id = ev.sessionId;
    if (id.isEmpty) return;
    if (_hydrating.containsKey(id)) {
      // A chunked history apply owns this transcript right now. Applying the
      // event live would let the next batch commit clobber it (and poison the
      // resync gates with its seq); defer it and reconcile in
      // [_drainDeferred] once the apply finishes.
      (_deferred[id] ??= <SessionEvent>[]).add(ev);
      return;
    }
    final current = state.forSession(id);
    // A live replay event (agent re-emitting prior conversation during a
    // session resume) is content we either already display or will fetch via
    // session.history — appending it to a populated transcript duplicates
    // the whole conversation. (Newer daemons don't broadcast these at all;
    // this guards against older ones.)
    if (ev.replay && current.items.isNotEmpty) return;

    if (_isBatchableEvent(ev)) {
      (_pending[id] ??= <SessionEvent>[]).add(ev);
      _flushTimer ??= Timer(kTranscriptBatchWindow, () {
        _flushTimer = null;
        _flushAllPending();
      });
      return;
    }

    // Discrete UX events: drain any pending chunks first so ordering is
    // preserved (text before turn_complete / permission / status).
    _flushSession(id);
    _applyLive(ev);
  }

  void _flushAllPending() {
    final ids = _pending.keys.toList(growable: false);
    for (final id in ids) {
      _flushSession(id);
    }
  }

  /// Merge adjacent same-type streaming text into one event, and collapse
  /// consecutive updates for one tool down to the latest.
  ///
  /// [_flushSession] applies a batch event by event, and every assistant or
  /// thought apply copies the whole accumulated reply
  /// (`transcript_reducer._appendChunk`). Applying a window's worth of chunks
  /// individually therefore costs one full copy per chunk; folding first costs
  /// one per run. Only *adjacent* events of the same type merge, so a
  /// `tool_call_update` arriving between two runs of text still lands between
  /// them. This is how MADR 0018 D1 is satisfied without making the transcript
  /// model mutable (MADR 0024 phase 5).
  ///
  /// Tool updates fold on the same principle but by replacement rather than
  /// concatenation — see [_isFoldableToolUpdate]. A `bash` streaming its output
  /// produces one update per SSE delta, each carrying up to 8 KB that
  /// `_upsertTool` re-clips and copies; within a window only the last of those
  /// can be observed, so the earlier ones are superseded rather than applied
  /// (MADR 0042 D2).
  List<SessionEvent> _foldChunks(List<SessionEvent> batch) {
    if (batch.length < 2) return batch;
    final out = <SessionEvent>[];
    SessionEvent? head;
    StringBuffer? run;
    // Pending tool update, held so a later update for the same id can supersede
    // it. At most one of [head] / [toolHead] is ever open: opening either
    // flushes both, so their relative order in `out` is never ambiguous.
    SessionEvent? toolHead;

    void flush() {
      if (head == null) return;
      out.add(run == null ? head! : head!.withText(run!.toString()));
      head = null;
      run = null;
    }

    void flushTool() {
      if (toolHead == null) return;
      out.add(toolHead!);
      toolHead = null;
    }

    for (final ev in batch) {
      if (head != null && head!.type == ev.type && _isFoldableChunk(ev)) {
        // head is about to be replaced by the newer event, whose seq the
        // merged event will carry. Note the one being folded away now, or the
        // reconnect resync window loses its lower bound.
        _noteSeq(head!);
        run ??= StringBuffer(head!.text ?? '');
        run!.write(ev.text ?? '');
        head = ev;
        continue;
      }
      // Replace-fold: a newer update for the same tool supersedes the pending
      // one. Keeping the *last* is what guarantees a terminal `completed` /
      // `failed` status can never be folded away. A `tool_call` for the same id
      // is not a `tool_call_update`, so it falls through and flushes instead —
      // the fold never crosses one.
      if (toolHead != null &&
          _isFoldableToolUpdate(ev) &&
          toolHead!.toolId == ev.toolId) {
        _noteSeq(toolHead!);
        toolHead = ev;
        continue;
      }
      if (_isNeutralInFold(ev)) {
        out.add(ev);
        continue;
      }
      flush();
      flushTool();
      if (_isFoldableChunk(ev)) {
        head = ev;
      } else if (_isFoldableToolUpdate(ev)) {
        toolHead = ev;
      } else {
        out.add(ev);
      }
    }
    flush();
    flushTool();
    return out;
  }

  void _flushSession(String id) {
    final pending = _pending.remove(id);
    if (pending == null || pending.isEmpty) return;
    final batch = _foldChunks(pending);
    var t = state.forSession(id);
    // Replay is an all-or-nothing batch decision. Checking the evolving
    // transcript would accept the first replay event then discard the rest.
    final hadItemsAtBatchStart = t.items.isNotEmpty;
    var changed = false;
    for (final ev in batch) {
      if (ev.replay && hadItemsAtBatchStart) continue;
      _noteSeq(ev);
      final next = applySessionEvent(t, ev);
      if (!identical(next, t)) {
        t = next;
        changed = true;
      }
    }
    if (changed) _commit(id, t);
  }

  void _applyLive(SessionEvent ev) {
    final id = ev.sessionId;
    final current = state.forSession(id);
    _noteSeq(ev);
    var next = applySessionEvent(current, ev);
    // applySessionEvent returns the same instance when the event is a no-op.
    if (identical(next, current)) return;
    // On the sending device, fold the locally-staged image bytes into the
    // just-appended user bubble so the sender sees a real thumbnail (the
    // daemon echo carries descriptors only; other devices show a placeholder).
    if (ev.type == 'user_message' && ev.attachments.isNotEmpty) {
      next = _zipStagedImages(id, next);
    }
    _commit(id, next);
  }

  /// Per-session FIFO of image bytes for prompts sent from THIS device, awaiting
  /// their user_message echo. Never persisted; transient send-correlation only.
  final Map<String, List<List<Uint8List>>> _sentImages = {};

  /// Stage the images of an outgoing prompt (call immediately before sending).
  /// The matching user_message echo folds them into the rendered bubble.
  void stageSentImages(String sessionId, List<Uint8List> images) {
    if (images.isEmpty) return;
    (_sentImages[sessionId] ??= []).add(List.of(images));
  }

  /// How many prompt batches are still awaiting their echo (tests only).
  @visibleForTesting
  int debugStagedImageCount(String sessionId) =>
      _sentImages[sessionId]?.length ?? 0;

  /// Drop the most recently staged batch — call when the send failed, so a
  /// doomed prompt's bytes can't mis-attach to a later turn.
  void unstageSentImages(String sessionId) {
    final q = _sentImages[sessionId];
    if (q == null || q.isEmpty) return;
    q.removeLast();
    if (q.isEmpty) _sentImages.remove(sessionId);
  }

  SessionTranscript _zipStagedImages(String id, SessionTranscript t) {
    final q = _sentImages[id];
    if (q == null || q.isEmpty || t.items.isEmpty) return t;
    final last = t.items.last;
    if (last.kind != ChatItemKind.user) return t;
    final imgIdx = [
      for (var i = 0; i < last.attachments.length; i++)
        if (last.attachments[i].kind == 'image') i,
    ];
    if (imgIdx.isEmpty) return t;
    final bytes = q.removeAt(0); // FIFO: oldest unacked send
    if (q.isEmpty) _sentImages.remove(id);
    final atts = [...last.attachments];
    for (var k = 0; k < imgIdx.length && k < bytes.length; k++) {
      atts[imgIdx[k]] = atts[imgIdx[k]].withBytes(bytes[k]);
    }
    final items = [...t.items];
    items[items.length - 1] = last.copyWith(attachments: atts);
    return t.copyWith(items: items, growableItems: false);
  }

  void clearSession(String sessionId) {
    _pending.remove(sessionId);
    _lastSeq.remove(sessionId);
    _firstSeq.remove(sessionId);
    _seqGapSuspected.remove(sessionId);
    _historyGen.remove(sessionId);
    _hydrating.remove(sessionId);
    _deferred.remove(sessionId);
    _sentImages.remove(sessionId);
    _cacheTimers.remove(sessionId)?.cancel();
    unawaited(_cache.remove(sessionId));
    _setState(state.remove(sessionId));
  }

  void clearAll() {
    _pending.clear();
    _flushTimer?.cancel();
    _flushTimer = null;
    _lastSeq.clear();
    _firstSeq.clear();
    _seqGapSuspected.clear();
    _historyGen.clear();
    _hydrating.clear();
    _deferred.clear();
    _sentImages.clear();
    for (final t in _cacheTimers.values) {
      t.cancel();
    }
    _cacheTimers.clear();
    unawaited(_cache.clear());
    _setState(state.clearAll());
  }

  void clearPending(
    String sessionId, {
    String? permissionId,
    String? questionId,
  }) {
    final current = state.byId[sessionId];
    if (current == null) return;
    var next = current;
    if (permissionId != null || questionId == null) {
      next = clearPendingPermission(next, permissionId: permissionId);
    }
    if (questionId != null) {
      next = clearPendingQuestion(next, questionId: questionId);
    }
    if (identical(next, current)) return;
    _commit(sessionId, next);
  }

  /// Generation counters so a superseded chunked hydrate aborts cleanly.
  final Map<String, int> _historyGen = {};

  /// Sessions with a chunked history apply in flight (value = owning
  /// generation from [_historyGen]), and the live events deferred while one
  /// runs. See [_onEvent] / [_drainDeferred].
  final Map<String, int> _hydrating = {};
  final Map<String, List<SessionEvent>> _deferred = {};

  /// Replay recorded history into a session whose transcript is still empty.
  ///
  /// Race discipline: the caller fetches history *because* the transcript was
  /// empty at chat-open time, but live `event`s may have populated it while
  /// the `session.history` request was in flight. If they did, reconcile via
  /// [resyncHistory] instead of double-applying; if they arrive *during* the
  /// chunked apply they are deferred and drained afterwards. History events
  /// share the exact JSON shape of live events, so they go through
  /// [applySessionEvent] in order, just like the live path.
  Future<void> replayHistory(
    String sessionId,
    List<SessionEvent> events,
  ) async {
    if (events.isEmpty) return;
    // Drain live batch first so race checks see the true local state.
    _flushSession(sessionId);
    // "Empty" is measured by items, matching the trigger condition: an empty
    // transcript may already carry commands/status without any chat items.
    final current = state.peek(sessionId);
    if (current != null && current.items.isNotEmpty) {
      // Populated transcript: don't drop the fetch on the floor (the old
      // behavior lost the entire recorded conversation whenever one live
      // chunk raced the response) — reconcile via seq instead.
      await resyncHistory(sessionId, events);
      return;
    }
    await _applyChunked(
      sessionId,
      events,
      current ?? state.forSession(sessionId),
      progressive: true,
    );
  }

  /// Apply [events] onto [initial] in batches of [kHistoryApplyBatchSize]
  /// with a frame yield between batches so large histories do not freeze the
  /// UI isolate (MADR 0018 B4).
  ///
  /// [progressive] controls visibility: true paints each batch as it commits
  /// (cold open of an empty transcript — progress beats a blank pane); false
  /// keeps the existing transcript on screen and commits once at the end
  /// (resync rebuild — intermediate commits would visibly rewind a populated
  /// chat to its oldest events).
  ///
  /// Invariants this loop must uphold:
  /// - Continue each batch from the transcript [_commit] returned, so no
  ///   already-published items list is ever mutated in place (MADR 0018 D2 —
  ///   the UI's select/memo checks compare list identity).
  /// - Record seqs into [_lastSeq]/[_firstSeq] only once the commit covering
  ///   them has happened. An abandoned apply must not mark unseen events as
  ///   seen, or the [resyncHistory] gates would refuse the refetch that could
  ///   repair the gap (stale-evidence discipline, as in the H4 SSE resync).
  /// - Live events cannot interleave: [_onEvent] defers them while
  ///   [_hydrating] holds this session, and [_drainDeferred] reconciles them
  ///   at the end, on every exit path.
  Future<bool> _applyChunked(
    String sessionId,
    List<SessionEvent> events,
    SessionTranscript initial, {
    required bool progressive,
  }) async {
    // These bytes can only be correlated with the next live echo. History and
    // deferred events expose metadata-only attachments, so retaining them
    // across hydration could attach a thumbnail to the wrong user message.
    // Lossless recovery needs a daemon-echoed client_prompt_id.
    _sentImages.remove(sessionId);
    final gen = (_historyGen[sessionId] ?? 0) + 1;
    _historyGen[sessionId] = gen;
    _hydrating[sessionId] = gen;
    // Seqs applied but not yet committed.
    var minPending = 0;
    var maxPending = 0;
    void noteCommitted() {
      if (maxPending == 0) return;
      final last = _lastSeq[sessionId] ?? 0;
      if (maxPending > last) _lastSeq[sessionId] = maxPending;
      final first = _firstSeq[sessionId] ?? 0;
      if (first == 0 || minPending < first) _firstSeq[sessionId] = minPending;
      minPending = 0;
      maxPending = 0;
    }

    try {
      var t = initial;
      for (var i = 0; i < events.length; i++) {
        if ((_historyGen[sessionId] ?? 0) != gen) return false;
        final seq = events[i].seq;
        if (seq > 0) {
          if (seq > maxPending) maxPending = seq;
          if (minPending == 0 || seq < minPending) minPending = seq;
        }
        t = applySessionEvent(t, events[i]);
        final atBatchEnd =
            (i + 1) % kHistoryApplyBatchSize == 0 || i == events.length - 1;
        if (!atBatchEnd) continue;
        if (progressive) {
          t = _commit(sessionId, t);
          noteCommitted();
        }
        if (i < events.length - 1) {
          await Future<void>.delayed(Duration.zero);
          if ((_historyGen[sessionId] ?? 0) != gen) return false;
          if (progressive) {
            // Adopt out-of-band commits made during the yield (announceCancel,
            // syncFromMeta, clearPending); events themselves cannot land here
            // because they defer. Non-progressive rebuilds overwrite such
            // transient flags at the final commit — same as before, and far
            // rarer than the event loss the rebuild repairs.
            final after = state.peek(sessionId);
            if (after != null && !identical(after, t)) t = after;
          }
        }
      }
      if (!progressive) {
        _commit(sessionId, t);
        noteCommitted();
      }
      return true;
    } finally {
      // Only the owning generation releases the deferral; a superseding
      // apply keeps deferring and drains when it finishes.
      if (_hydrating[sessionId] == gen) {
        _hydrating.remove(sessionId);
        _drainDeferred(sessionId);
      }
    }
  }

  /// Apply the events deferred during a chunked history apply, in arrival
  /// order, as a single commit. Events whose seq the transcript already
  /// covers were part of the history fetch itself and are skipped.
  void _drainDeferred(String sessionId) {
    final queue = _deferred.remove(sessionId);
    if (queue == null || queue.isEmpty) return;
    var t = state.forSession(sessionId);
    final hadItemsAtBatchStart = t.items.isNotEmpty;
    var changed = false;
    for (final ev in queue) {
      final last = _lastSeq[sessionId] ?? 0;
      if (ev.seq > 0 && ev.seq <= last) continue;
      if (ev.replay && hadItemsAtBatchStart) continue;
      _noteSeq(ev);
      final next = applySessionEvent(t, ev);
      if (!identical(next, t)) {
        t = next;
        changed = true;
      }
    }
    if (changed) _commit(sessionId, t);
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
  Future<void> resyncHistory(
    String sessionId,
    List<SessionEvent> events,
  ) async {
    if (events.isEmpty) return;
    _flushSession(sessionId);
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
    final gap = _seqGapSuspected[sessionId] == true;
    if (!missedNewer && !missedOlder && !gap) return;

    // Rebuild from scratch, non-progressively: the populated transcript stays
    // on screen until the rebuilt one lands in a single commit — progressive
    // commits would visibly rewind the chat to its oldest events.
    final committed = await _applyChunked(
      sessionId,
      events,
      SessionTranscript(sessionId: sessionId),
      progressive: false,
    );
    if (committed) _seqGapSuspected[sessionId] = false;
  }

  /// Local cancel announcement before server `turn_complete` arrives.
  void announceCancel(String sessionId) {
    // peek, not forSession: announcing on an unknown id would materialise a
    // permanent empty transcript.
    _flushSession(sessionId);
    final current = state.peek(sessionId);
    if (current == null) return;
    final next = markCancelAnnounced(current);
    if (identical(next, current)) return;
    _commit(sessionId, next);
  }

  /// Reconcile against `session.list`: adopt authoritative status for sessions
  /// whose `turn_complete` we may have missed across a socket drop, and evict
  /// transcripts for sessions the host no longer knows about.
  void syncFromMeta(List<SessionMeta> metas) {
    final liveIds = metas.map((m) => m.id).toSet();
    // Evict dead sessions' cache entries too, not only their transcripts —
    // otherwise up to kTranscriptCacheMaxSessions dead snapshots linger in
    // prefs until LRU pressure happens to push them out.
    for (final id in state.byId.keys) {
      if (liveIds.contains(id)) continue;
      _cacheTimers.remove(id)?.cancel();
    }
    unawaited(_cache.retainOnly(liveIds));
    _pending.removeWhere((id, _) => !liveIds.contains(id));
    _lastSeq.removeWhere((id, _) => !liveIds.contains(id));
    _firstSeq.removeWhere((id, _) => !liveIds.contains(id));
    _seqGapSuspected.removeWhere((id, _) => !liveIds.contains(id));
    _historyGen.removeWhere((id, _) => !liveIds.contains(id));
    _hydrating.removeWhere((id, _) => !liveIds.contains(id));
    _deferred.removeWhere((id, _) => !liveIds.contains(id));
    _sentImages.removeWhere((id, _) => !liveIds.contains(id));
    var next = state.retainOnly(liveIds);
    for (final m in metas) {
      final current = next.byId[m.id];
      if (current == null) continue;
      final synced = applyMetaStatus(current, m.status, live: m.live);
      if (identical(synced, current)) continue;
      next = next.upsert(_publish(synced));
      _scheduleCacheSave(m.id);
    }
    if (identical(next, state)) return;
    _setState(next);
  }
}

final transcriptsProvider =
    NotifierProvider<TranscriptsNotifier, TranscriptsState>(
      TranscriptsNotifier.new,
    );

final sessionTranscriptProvider = Provider.autoDispose
    .family<SessionTranscript, String>((ref, sessionId) {
      return ref.watch(transcriptsProvider).forSession(sessionId);
    });

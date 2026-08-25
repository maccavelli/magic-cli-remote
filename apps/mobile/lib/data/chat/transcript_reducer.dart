import 'package:flutter/foundation.dart' show visibleForTesting;

import '../protocol/models.dart';
import 'chat_models.dart';

/// Apply a [SessionEvent] to a [SessionTranscript]. Pure and side-effect free.
///
/// Events for other session ids are ignored (returns [current] unchanged).
SessionTranscript applySessionEvent(
  SessionTranscript current,
  SessionEvent ev,
) {
  if (ev.sessionId != current.sessionId) return current;

  if (ev.type == 'session_status') {
    final s = ev.status;
    // Return the identical instance on a no-op so the notifier's
    // `identical()` check can suppress a full transcript rebuild.
    if (s == null || s.isEmpty || s == current.status) return current;
    return current.copyWith(status: s);
  }

  if (ev.type == 'available_commands') {
    // Replace catalog (agent may send empty to clear). No chat bubble noise.
    if (_sameCommands(current.commands, ev.commands)) return current;
    return current.copyWith(commands: List<AvailableCommand>.from(ev.commands));
  }

  if (ev.type == 'remote_commands') {
    // The daemon's canonical command list, resolved for this session
    // (MADR 0023). Replace-semantics, no chat bubble; identical instance on a
    // no-op since it is re-sent whenever resolution changes.
    if (_sameRemoteCommands(current.remoteCommands, ev.remoteCommands)) {
      return current;
    }
    return current.copyWith(
      remoteCommands: List<RemoteCommand>.from(ev.remoteCommands),
    );
  }

  if (ev.type == 'plan') {
    // Each `plan` event is the full current plan (replace-semantics), rendered
    // in a dedicated panel rather than the scrolling transcript — no chat
    // bubble. Return the identical instance when unchanged so the notifier's
    // `identical()` check suppresses a rebuild.
    if (_samePlan(current.plan, ev.plan)) return current;
    return current.copyWith(plan: List<PlanEntry>.from(ev.plan));
  }

  if (ev.type == 'subagents') {
    // Full current set on every event (replace-semantics), rendered in its own
    // panel rather than the transcript — sub-agent output never reaches chat,
    // only the fact that agents are running (MADR 0051 Part II). An event with
    // no entries is a *clear*, matching `plan` and unlike `session_mode`.
    if (_sameSubagents(current.subagents, ev.subagents)) return current;
    return current.copyWith(subagents: List<SubagentInfo>.from(ev.subagents));
  }

  if (ev.type == 'usage_update') {
    // Token/context report → context-window indicator only, no chat bubble.
    // Replace-semantics; identical instance on a no-op so the notifier can
    // suppress a rebuild (usage arrives frequently mid-turn).
    final u = ev.usage;
    if (u == null || u == current.usage) return current;
    return current.copyWith(usage: u);
  }

  if (ev.type == 'session_capabilities') {
    // Agent capability set → gates client UI. No chat bubble; identical
    // instance on a no-op.
    final c = ev.capabilities;
    if (c == null || c == current.capabilities) return current;
    return current.copyWith(capabilities: c);
  }

  if (ev.type == 'session_mode') {
    // Full list arrives at session create/load; a current_mode_update carries
    // only the new current id (empty modes list) — keep the existing list then.
    final modes = ev.modes.isNotEmpty ? ev.modes : current.modes;
    final currentId = ev.currentModeId ?? current.currentModeId;
    // Content comparison, not list identity: `List.from(ev.modes)` is a fresh
    // instance every time, so an identity check could never match and a
    // re-sent, byte-identical mode list rebuilt the whole transcript and the
    // chat shell with it (MADR 0042 D8).
    if (_sameModes(modes, current.modes) &&
        currentId == current.currentModeId) {
      return current;
    }
    return current.copyWith(
      modes: List<SessionMode>.from(modes),
      currentModeId: currentId,
    );
  }

  if (ev.type == 'session_goal') {
    if (ev.goal == null && current.goal == null) return current;
    if (ev.goal != null &&
        current.goal != null &&
        ev.goal!.objective == current.goal!.objective &&
        ev.goal!.status == current.goal!.status &&
        ev.goal!.tokenBudget == current.goal!.tokenBudget &&
        ev.goal!.tokenUsage == current.goal!.tokenUsage) {
      return current;
    }
    return current.copyWith(goal: ev.goal);
  }

  if (ev.type == 'collaboration_mode') {
    final modes = ev.collaborationModes.isNotEmpty
        ? ev.collaborationModes
        : current.collaborationModes;
    final currentId =
        ev.currentCollaborationModeId ?? current.currentCollaborationModeId;
    if (_sameCollaborationModes(modes, current.collaborationModes) &&
        currentId == current.currentCollaborationModeId) {
      return current;
    }
    return current.copyWith(
      collaborationModes: List<CollaborationMode>.from(modes),
      currentCollaborationModeId: currentId,
    );
  }

  if (ev.type == 'session_config') {
    // Merge by option id: session create/load carries the full set (merges into
    // empty = the full set); a single-option echo (e.g. the fake, or an agent
    // that reports one change) updates just that option and keeps the rest.
    if (ev.configOptions.isEmpty) return current;
    final merged = [...current.configOptions];
    for (final o in ev.configOptions) {
      final i = merged.indexWhere((e) => e.id == o.id);
      if (i >= 0) {
        merged[i] = o;
      } else {
        merged.add(o);
      }
    }
    // A re-sent, unchanged option set must not republish the transcript.
    if (_sameConfigOptions(merged, current.configOptions)) return current;
    return current.copyWith(configOptions: merged);
  }

  var t = current;
  switch (ev.type) {
    case 'user_message':
      final text = (ev.text ?? '').trim();
      // Append when there is text OR attachments (an image-only prompt carries
      // no text but must still show a bubble).
      if (text.isNotEmpty || ev.attachments.isNotEmpty) {
        // A new turn re-arms the cancel announcement: without this, cancel →
        // new turn → cancel again would announce nothing the second time
        // (the latch was only reset by a *non-cancelled* completion).
        if (t.cancelAnnounced) t = t.copyWith(cancelAnnounced: false);
        final atts = [
          for (final a in ev.attachments)
            ChatAttachment(kind: a.kind, mimeType: a.mimeType),
        ];
        t = _append(t, ChatItem.user(text, attachments: atts));
      }
    case 'assistant_message_chunk':
      final text = ev.text ?? '';
      if (text.isNotEmpty) t = _appendAssistant(t, text);
    case 'thought_chunk':
      final text = ev.text ?? '';
      if (text.isNotEmpty) t = _appendThought(t, text);
    case 'tool_call':
    case 'tool_call_update':
      t = _upsertTool(t, ev);
    case 'codex_progress':
    case 'codex_warning':
    case 'codex_model_reroute':
    case 'codex_model_verification':
    case 'codex_terminal_interaction':
    case 'codex_unsupported_item':
      t = _upsertCodex(t, ev);
    case 'approval_summary':
      t = _upsertApprovals(t, ev);
    case 'permission_request':
      t = _onPermissionRequest(t, ev);
    case 'permission_resolved':
      t = _onPermissionResolved(t, ev);
    case 'question_request':
      t = _onQuestionRequest(t, ev);
    case 'question_resolved':
      t = _onQuestionResolved(t, ev);
    case 'turn_complete':
      t = _onTurnComplete(t, ev);
    case 'notice':
      // Daemon-originated informational line (e.g. built-in slash command
      // output). Rendered as a neutral system message, not an error.
      final msg = (ev.text ?? '').trim();
      if (msg.isNotEmpty) t = _append(t, ChatItem.system(msg));
    case 'error':
      final msg = (ev.error ?? ev.text ?? '').trim();
      if (msg.isNotEmpty) {
        t = _append(
          t.copyWith(status: 'error'),
          ChatItem.system(
            _clip(msg, 300),
            error: true,
            errorKind: ev.errorKind,
            retryAt: ev.retryAt,
          ),
        );
      }
      // An errored turn will never answer outstanding permission requests;
      // leaving them pending would keep the composer disabled forever.
      t = _clearAllPending(t);
    default:
      break;
  }
  return t;
}

SessionTranscript _upsertCodex(
  SessionTranscript transcript,
  SessionEvent event,
) {
  final codex = event.codex;
  if (codex == null || codex.key.trim().isEmpty) return transcript;
  final detail = codex.text.trim();
  final title = codex.title.trim().isEmpty
      ? 'Codex activity'
      : codex.title.trim();
  final message = detail.isEmpty ? title : '$title: $detail';
  final index = transcript.items.indexWhere(
    (item) => item.dedupeKey == codex.key,
  );
  if (index >= 0) {
    final previous = transcript.items[index];
    if (previous.text == message) return transcript;
    final items = List<ChatItem>.of(transcript.items, growable: true);
    items[index] = ChatItem.system(
      message,
      dedupeKey: codex.key,
    ).copyWith(seq: previous.seq);
    return transcript.copyWith(items: items, growableItems: true);
  }
  return _append(transcript, ChatItem.system(message, dedupeKey: codex.key));
}

/// Reconcile transcript status against authoritative [SessionMeta] status from
/// `session.list`. A dropped socket can lose the `turn_complete` that would
/// otherwise move a session out of `running`, stranding the composer.
SessionTranscript applyMetaStatus(
  SessionTranscript current,
  String metaStatus, {
  bool live = true,
}) {
  final s = metaStatus.trim();
  if (!live) {
    // Host no longer has this session: nothing can resolve pending requests.
    final cleared = _clearAllPending(current);
    if (cleared.status == 'disconnected') return cleared;
    return cleared.copyWith(status: 'disconnected');
  }
  if (s.isEmpty || s == current.status) return current;
  // Only trust the host to move us *out* of a busy state; live events are
  // more current than a poll for everything else.
  if (current.status == 'running' && s != 'running') {
    return _clearAllPending(current).copyWith(status: s);
  }
  return current;
}

SessionTranscript _onPermissionRequest(SessionTranscript t, SessionEvent ev) {
  final id = (ev.permissionId ?? '').trim();
  if (id.isEmpty) return t;
  // Re-sent request for the same id: adopt the (possibly enriched) event —
  // the daemon may replay it with the actual command detail — but do not
  // append a second system line.
  if (t.pendingPermissions.containsKey(id)) {
    final prev = t.pendingPermissions[id]!;
    if ((ev.text ?? '') == (prev.text ?? '')) return t;
    final pending = Map<String, SessionEvent>.from(t.pendingPermissions);
    pending[id] = ev;
    return t.copyWith(pendingPermissions: pending);
  }

  final pending = Map<String, SessionEvent>.from(t.pendingPermissions);
  pending[id] = ev;
  return _append(
    t.copyWith(pendingPermissions: pending),
    ChatItem.system('Permission: ${ev.toolName ?? ev.text ?? 'tool'}'),
  );
}

SessionTranscript _onPermissionResolved(SessionTranscript t, SessionEvent ev) {
  final id = (ev.permissionId ?? '').trim();
  if (id.isEmpty) return _clearAllPending(t);
  final next = clearPendingPermission(t, permissionId: id);
  if (!ev.timedOut) return next;
  // A replay may repeat the resolution. The pending request is already gone,
  // so only the first live/replayed observation of *this* timeout writes the
  // durable notice. Keyed on the permission, not the wording: matching the
  // text meant the second and every later timeout in a session vanished with
  // no explanation at all (MADR 0046 M-6).
  final key = 'perm-timeout:$id';
  final alreadyShown = t.items.any((item) => item.dedupeKey == key);
  return alreadyShown
      ? next
      : _append(
          next,
          ChatItem.system('Permission request timed out', dedupeKey: key),
        );
}

SessionTranscript _onQuestionRequest(SessionTranscript t, SessionEvent ev) {
  final id = (ev.questionId ?? '').trim();
  if (id.isEmpty) return t;
  if (t.pendingQuestions.containsKey(id)) {
    final prev = t.pendingQuestions[id]!;
    if ((ev.text ?? '') == (prev.text ?? '') &&
        ev.questions.length == prev.questions.length) {
      return t;
    }
    final pending = Map<String, SessionEvent>.from(t.pendingQuestions);
    pending[id] = ev;
    return t.copyWith(pendingQuestions: pending);
  }
  final pending = Map<String, SessionEvent>.from(t.pendingQuestions);
  pending[id] = ev;
  final label = (ev.text ?? '').trim().isEmpty ? 'Question' : ev.text!.trim();
  return _append(
    t.copyWith(pendingQuestions: pending),
    ChatItem.system('Question: $label'),
  );
}

SessionTranscript _onQuestionResolved(SessionTranscript t, SessionEvent ev) {
  final id = (ev.questionId ?? '').trim();
  if (id.isEmpty) {
    return t.copyWith(pendingQuestions: const {});
  }
  return clearPendingQuestion(t, questionId: id);
}

SessionTranscript _clearAllPending(SessionTranscript t) {
  if (t.pendingPermissions.isEmpty && t.pendingQuestions.isEmpty) return t;
  return t.copyWith(pendingPermissions: const {}, pendingQuestions: const {});
}

SessionTranscript clearPendingPermission(
  SessionTranscript current, {
  String? permissionId,
}) {
  if (current.pendingPermissions.isEmpty) return current;
  if (permissionId == null) {
    return current.copyWith(pendingPermissions: const {});
  }
  if (!current.pendingPermissions.containsKey(permissionId)) return current;
  final pending = Map<String, SessionEvent>.from(current.pendingPermissions)
    ..remove(permissionId);
  return current.copyWith(pendingPermissions: pending);
}

SessionTranscript clearPendingQuestion(
  SessionTranscript current, {
  String? questionId,
}) {
  if (current.pendingQuestions.isEmpty) return current;
  if (questionId == null) {
    return current.copyWith(pendingQuestions: const {});
  }
  if (!current.pendingQuestions.containsKey(questionId)) return current;
  final pending = Map<String, SessionEvent>.from(current.pendingQuestions)
    ..remove(questionId);
  return current.copyWith(pendingQuestions: pending);
}

SessionTranscript _onTurnComplete(SessionTranscript t, SessionEvent ev) {
  final reason = (ev.stopReason ?? ev.status ?? '').trim();
  final cancelled = reason == 'cancelled' || reason == 'canceled';

  // A completed turn resolves nothing that is still pending; the daemon now
  // emits permission_resolved for those, but clear defensively so a lost
  // event cannot leave the composer disabled.
  var next = _clearAllPending(t).copyWith(status: 'idle');

  if (cancelled) {
    if (!t.cancelAnnounced) {
      // Latch the flag — the local `announceCancel` path races this event and
      // would otherwise append a second identical line.
      next = _append(
        next.copyWith(cancelAnnounced: true),
        ChatItem.system('Turn cancelled'),
      );
    }
    return next;
  }

  // `error` is silent here on purpose (MADR 0036 D1): the daemon guarantees an
  // `error` stop is always paired with an `error` event carrying the actual
  // failure text, so rendering a stop line too would print a contentless
  // "Turn ended (error)" above the real message. Do not "restore" this line
  // without also dropping that guarantee daemon-side.
  if (reason.isNotEmpty &&
      reason != 'end_turn' &&
      reason != 'end-turn' &&
      reason != 'error') {
    next = _append(next, ChatItem.system(_humanStopReason(reason)));
  }
  // Only a non-cancelled turn arms the next cancellation announcement.
  return next.copyWith(cancelAnnounced: false);
}

/// Human copy for non-standard stop reasons instead of leaking wire enums.
String _humanStopReason(String reason) => switch (reason) {
  'refusal' => 'The agent declined to continue.',
  'max_tokens' => 'Stopped — the reply hit the model\'s length limit.',
  'max_turn_requests' =>
    'Stopped — the agent reached its request limit for this turn.',
  _ => 'Turn ended ($reason)',
};

SessionTranscript markCancelAnnounced(SessionTranscript t) {
  if (t.cancelAnnounced) return t;
  return _append(
    t.copyWith(cancelAnnounced: true),
    ChatItem.system('Turn cancelled'),
  );
}

SessionTranscript _append(SessionTranscript t, ChatItem item) {
  final list = _mutableItems(t);
  list.add(item.copyWith(seq: t.nextSeq));
  return _enforceCap(
    // A fresh item ends the cache-restored tail: later chunks may merge into
    // it normally.
    t.copyWith(
      items: list,
      nextSeq: t.nextSeq + 1,
      growableItems: true,
      sealedTail: false,
    ),
  );
}

/// Owned growable list for in-batch last-index appends (MADR 0018 D2).
List<ChatItem> _mutableItems(SessionTranscript t) {
  if (t.growableItems) return t.items;
  return List<ChatItem>.of(t.items, growable: true);
}

/// Clip stored text; preserves identity when already within budget.
String clipItemText(String text, {int max = kMaxItemTextChars}) {
  if (text.length <= max) return text;
  final keep = max - kTextTruncatedMarker.length;
  if (keep <= 0) return kTextTruncatedMarker;
  return '${text.substring(0, _runeSafeCut(text, keep))}$kTextTruncatedMarker';
}

/// Counts [_appendChunk] calls so tests can bound ingest cost, the way
/// `debugMarkdownParseCount` bounds render cost. Each call copies the whole
/// accumulated reply, so this is the number that must stay proportional to
/// flush windows rather than to streamed tokens (MADR 0024 phase 5).
@visibleForTesting
int debugAppendChunkCount = 0;

String _appendChunk(String? prev, String chunk) {
  debugAppendChunkCount++;
  if (prev == null || prev.isEmpty) return clipItemText(chunk);
  // Already at cap: ignore further growth (keep seq/tool identity).
  if (prev.endsWith(kTextTruncatedMarker) && prev.length >= kMaxItemTextChars) {
    return prev;
  }
  if (prev.length >= kMaxItemTextChars) {
    return clipItemText(prev);
  }
  // StringBuffer avoids quadratic concat on multi-KB streams within a batch.
  if (prev.length + chunk.length > 256) {
    return clipItemText((StringBuffer(prev)..write(chunk)).toString());
  }
  return clipItemText(prev + chunk);
}

SessionTranscript _appendAssistant(SessionTranscript t, String text) {
  if (!t.sealedTail &&
      t.items.isNotEmpty &&
      t.items.last.kind == ChatItemKind.assistant) {
    final items = _mutableItems(t);
    final last = items.last;
    items[items.length - 1] = last.copyWith(
      text: _appendChunk(last.text, text),
    );
    return t.copyWith(items: items, growableItems: true);
  }
  // Whitespace mid-message is meaningful (paragraph breaks arrive as
  // standalone "\n\n" chunks), but a whitespace-only chunk must not OPEN a
  // bubble — that renders as an empty message.
  if (text.trim().isEmpty) return t;
  return _append(t, ChatItem.assistant(clipItemText(text)));
}

SessionTranscript _appendThought(SessionTranscript t, String text) {
  if (!t.sealedTail &&
      t.items.isNotEmpty &&
      t.items.last.kind == ChatItemKind.thought) {
    final items = _mutableItems(t);
    final last = items.last;
    items[items.length - 1] = last.copyWith(
      text: _appendChunk(last.text, text),
    );
    return t.copyWith(items: items, growableItems: true);
  }
  if (text.trim().isEmpty) return t;
  return _append(t, ChatItem.thought(clipItemText(text)));
}

/// Upsert the auto-approval card for `ev.approvalGroupId`.
///
/// Every `approval_summary` event carries the full list from the start of the
/// turn (replace-semantics), so this must find and replace the existing card —
/// appending would re-print the same approvals on every event, which is exactly
/// the noise the event exists to remove (MADR 0051 Part I).
///
/// The card is located by a backward scan rather than an index map: there is at
/// most one open group per turn, and the rate is bounded by tool executions.
SessionTranscript _upsertApprovals(SessionTranscript t, SessionEvent ev) {
  final id = (ev.approvalGroupId ?? '').trim();
  final approvals = ev.approvals;
  // Nothing to show, and no key to replace under: ignore rather than render an
  // empty card.
  if (id.isEmpty || approvals.isEmpty) return t;
  final status = (ev.status ?? '').trim();

  for (var i = t.items.length - 1; i >= 0; i--) {
    final prev = t.items[i];
    if (prev.kind != ChatItemKind.approvals || prev.toolId != id) continue;
    // Identity guard: a re-sent, unchanged snapshot must not rebuild the list
    // (same contract as the tool-card upsert).
    if (prev.approvals.length == approvals.length &&
        prev.toolStatus == (status.isEmpty ? prev.toolStatus : status)) {
      return t;
    }
    final items = _mutableItems(t);
    items[i] = prev.copyWith(
      approvals: approvals,
      toolStatus: status.isEmpty ? prev.toolStatus : status,
    );
    return t.copyWith(items: items, growableItems: true);
  }

  return _append(
    t,
    ChatItem.approvals(id: id, approvals: approvals, status: status),
  );
}

SessionTranscript _upsertTool(SessionTranscript t, SessionEvent ev) {
  final id = (ev.toolId ?? '').trim();
  final status = (ev.status ?? '').trim();
  final detail = (ev.text ?? '').trim();
  // `toolName` is only a real title when the event carried one. The daemon
  // substitutes the literal "tool" on updates that omit an ACP title, which
  // would otherwise rename an established card.
  final rawName = (ev.toolName ?? '').trim();
  final name = (rawName.isEmpty || rawName == 'tool') ? '' : rawName;
  final kind = (ev.toolKind ?? '').trim();

  final clippedDetail = detail.isNotEmpty ? clipItemText(detail) : detail;

  if (id.isNotEmpty && t.toolIndex.containsKey(id)) {
    final i = t.toolIndex[id]!;
    if (i >= 0 && i < t.items.length && t.items[i].kind == ChatItemKind.tool) {
      final prev = t.items[i];
      final nextStatus = status.isNotEmpty ? status : prev.toolStatus;
      final nextName = name.isNotEmpty ? name : prev.toolName;
      final nextKind = kind.isNotEmpty ? kind : prev.toolKind;
      final nextText = clippedDetail.isNotEmpty ? clippedDetail : prev.text;

      // Identity guard: if nothing observable changed, return t unchanged
      // to avoid copying item list and triggering rebuilds (MADR 0034 D4).
      if (nextStatus == prev.toolStatus &&
          nextName == prev.toolName &&
          nextKind == prev.toolKind &&
          nextText == prev.text) {
        return t;
      }

      final items = _mutableItems(t);
      items[i] = prev.copyWith(
        toolName: nextName,
        toolStatus: nextStatus,
        toolKind: nextKind,
        text: nextText,
      );
      return t.copyWith(items: items, growableItems: true);
    }
  }

  // An *update* that carries no usable tool id can only be a continuation of
  // the tool already streaming, so fold it into the most recent tool card
  // rather than spawning a duplicate. Without this, an agent that omits the id
  // on `tool_call_update` prints a fresh "Tool" card per status change — the
  // duplicated tool rows the transcript was showing. A fresh `tool_call` still
  // starts its own card (only updates coalesce here).
  if (id.isEmpty &&
      ev.type == 'tool_call_update' &&
      t.items.isNotEmpty &&
      t.items.last.kind == ChatItemKind.tool) {
    final items = _mutableItems(t);
    final prev = items.last;
    items[items.length - 1] = prev.copyWith(
      toolName: name.isNotEmpty ? name : prev.toolName,
      toolStatus: status.isNotEmpty ? status : prev.toolStatus,
      toolKind: kind.isNotEmpty ? kind : prev.toolKind,
      text: clippedDetail.isNotEmpty ? clippedDetail : prev.text,
    );
    return t.copyWith(items: items, growableItems: true);
  }

  final label = name.isNotEmpty
      ? name
      : (rawName.isNotEmpty ? rawName : 'Tool');
  final item = ChatItem.tool(
    id: id,
    name: label,
    status: status,
    detail: clippedDetail,
    toolKind: kind.isEmpty ? null : kind,
    seq: t.nextSeq,
  );
  final items = _mutableItems(t)..add(item);
  final toolIndex = Map<String, int>.from(t.toolIndex);
  if (id.isNotEmpty) {
    toolIndex[id] = items.length - 1;
  }
  return _enforceCap(
    t.copyWith(
      items: items,
      toolIndex: toolIndex,
      nextSeq: t.nextSeq + 1,
      growableItems: true,
    ),
  );
}

SessionTranscript _enforceCap(SessionTranscript t) {
  if (t.items.length <= kMaxTranscriptItems) return t;

  final drop = t.items.length - kMaxTranscriptItems;
  // New growable list so FIFO trim never mutates a published prefix list.
  final items = List<ChatItem>.of(t.items.sublist(drop), growable: true);

  // Rebuild tool index relative to the new list.
  final toolIndex = <String, int>{};
  for (var i = 0; i < items.length; i++) {
    final id = items[i].toolId;
    if (id != null && id.isNotEmpty && items[i].kind == ChatItemKind.tool) {
      toolIndex[id] = i;
    }
  }
  return t.copyWith(items: items, toolIndex: toolIndex, growableItems: true);
}

bool _sameCommands(List<AvailableCommand> a, List<AvailableCommand> b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i].name != b[i].name ||
        a[i].description != b[i].description ||
        a[i].hint != b[i].hint) {
      return false;
    }
  }
  return true;
}

bool _sameRemoteCommands(List<RemoteCommand> a, List<RemoteCommand> b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i].name != b[i].name ||
        a[i].available != b[i].available ||
        a[i].hint != b[i].hint ||
        a[i].description != b[i].description ||
        a[i].reason != b[i].reason) {
      return false;
    }
  }
  return true;
}

bool _sameModes(List<SessionMode> a, List<SessionMode> b) {
  if (identical(a, b)) return true;
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

bool _sameCollaborationModes(
  List<CollaborationMode> a,
  List<CollaborationMode> b,
) {
  if (identical(a, b)) return true;
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

bool _sameConfigOptions(List<ConfigOption> a, List<ConfigOption> b) {
  if (identical(a, b)) return true;
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

bool _samePlan(List<PlanEntry> a, List<PlanEntry> b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i].content != b[i].content ||
        a[i].status != b[i].status ||
        a[i].priority != b[i].priority) {
      return false;
    }
  }
  return true;
}

bool _sameSubagents(List<SubagentInfo> a, List<SubagentInfo> b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

/// Clip an error message for display.
///
/// Collapses runs of spaces and tabs but **keeps line breaks**: this is the
/// app's primary account of why a turn failed, and a multi-line CLI failure or
/// stack trace is unreadable once flattened to one run-on line. Blank-line runs
/// collapse to a single paragraph break so the card stays compact.
String _clip(String s, int max) {
  final trimmed = s
      .replaceAll(RegExp(r'[ \t]+'), ' ')
      .replaceAll(RegExp(r'\n{3,}'), '\n\n')
      .replaceAll(RegExp(r' *\n *'), '\n')
      .trim();
  if (trimmed.length <= max) return trimmed;
  return '${trimmed.substring(0, _runeSafeCut(trimmed, max))}…';
}

/// Back a cut index off the trailing half of a surrogate pair.
///
/// `substring` counts UTF-16 code units, so cutting between the halves of an
/// astral character (emoji, and much CJK-adjacent text) leaves a lone surrogate
/// that renders as a replacement glyph. `_AssistantMarkdown` already guards its
/// own show-more clamp this way; the technique belongs here too.
int _runeSafeCut(String s, int max) {
  if (max <= 0 || max >= s.length) return max.clamp(0, s.length);
  // 0xDC00..0xDFFF is a low surrogate — the second half of a pair.
  final unit = s.codeUnitAt(max);
  return (unit & 0xFC00) == 0xDC00 ? max - 1 : max;
}

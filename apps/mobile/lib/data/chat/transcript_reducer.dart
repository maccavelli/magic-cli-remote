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

  var t = current;
  switch (ev.type) {
    case 'user_message':
      final text = (ev.text ?? '').trim();
      if (text.isNotEmpty) {
        t = _append(t, ChatItem.user(text));
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
    case 'permission_request':
      t = _onPermissionRequest(t, ev);
    case 'permission_resolved':
      t = _onPermissionResolved(t, ev);
    case 'turn_complete':
      t = _onTurnComplete(t, ev);
    case 'error':
      final msg = (ev.error ?? ev.text ?? '').trim();
      if (msg.isNotEmpty) {
        t = _append(
          t.copyWith(status: 'error'),
          ChatItem.system('Error: ${_clip(msg, 300)}'),
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

SessionTranscript _onPermissionRequest(
  SessionTranscript t,
  SessionEvent ev,
) {
  final id = (ev.permissionId ?? '').trim();
  if (id.isEmpty) return t;
  // Replayed request: keep the original, do not append a second system line.
  if (t.pendingPermissions.containsKey(id)) return t;

  final pending = Map<String, SessionEvent>.from(t.pendingPermissions);
  pending[id] = ev;
  return _append(
    t.copyWith(pendingPermissions: pending),
    ChatItem.system('Permission: ${ev.toolName ?? ev.text ?? 'tool'}'),
  );
}

SessionTranscript _onPermissionResolved(
  SessionTranscript t,
  SessionEvent ev,
) {
  final id = (ev.permissionId ?? '').trim();
  if (id.isEmpty) return _clearAllPending(t);
  return clearPendingPermission(t, permissionId: id);
}

SessionTranscript _clearAllPending(SessionTranscript t) {
  if (t.pendingPermissions.isEmpty) return t;
  return t.copyWith(pendingPermissions: const {});
}

SessionTranscript clearPendingPermission(
  SessionTranscript current, {
  String? permissionId,
}) {
  if (current.pendingPermissions.isEmpty) return current;
  if (permissionId == null) return _clearAllPending(current);
  if (!current.pendingPermissions.containsKey(permissionId)) return current;
  final pending = Map<String, SessionEvent>.from(current.pendingPermissions)
    ..remove(permissionId);
  return current.copyWith(pendingPermissions: pending);
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

  if (reason.isNotEmpty && reason != 'end_turn' && reason != 'end-turn') {
    next = _append(next, ChatItem.system('Turn ended ($reason)'));
  }
  // Only a non-cancelled turn arms the next cancellation announcement.
  return next.copyWith(cancelAnnounced: false);
}

SessionTranscript markCancelAnnounced(SessionTranscript t) {
  if (t.cancelAnnounced) return t;
  return _append(
    t.copyWith(cancelAnnounced: true),
    ChatItem.system('Turn cancelled'),
  );
}

SessionTranscript _append(SessionTranscript t, ChatItem item) {
  return _enforceCap(
    t.copyWith(
      items: [...t.items, item.copyWith(seq: t.nextSeq)],
      nextSeq: t.nextSeq + 1,
    ),
  );
}

SessionTranscript _appendAssistant(SessionTranscript t, String text) {
  if (t.items.isNotEmpty && t.items.last.kind == ChatItemKind.assistant) {
    final items = List<ChatItem>.from(t.items);
    items[items.length - 1] =
        items.last.copyWith(text: (items.last.text ?? '') + text);
    return t.copyWith(items: items);
  }
  return _append(t, ChatItem.assistant(text));
}

SessionTranscript _appendThought(SessionTranscript t, String text) {
  if (t.items.isNotEmpty && t.items.last.kind == ChatItemKind.thought) {
    final items = List<ChatItem>.from(t.items);
    items[items.length - 1] =
        items.last.copyWith(text: (items.last.text ?? '') + text);
    return t.copyWith(items: items);
  }
  return _append(t, ChatItem.thought(text));
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

  if (id.isNotEmpty && t.toolIndex.containsKey(id)) {
    final i = t.toolIndex[id]!;
    if (i >= 0 && i < t.items.length && t.items[i].kind == ChatItemKind.tool) {
      final items = List<ChatItem>.from(t.items);
      final prev = items[i];
      items[i] = prev.copyWith(
        toolName: name.isNotEmpty ? name : prev.toolName,
        toolStatus: status.isNotEmpty ? status : prev.toolStatus,
        text: detail.isNotEmpty ? detail : prev.text,
      );
      return t.copyWith(items: items);
    }
  }

  final label = name.isNotEmpty ? name : (rawName.isNotEmpty ? rawName : 'Tool');
  final item = ChatItem.tool(
    id: id,
    name: label,
    status: status,
    detail: detail,
    seq: t.nextSeq,
  );
  final items = [...t.items, item];
  final toolIndex = Map<String, int>.from(t.toolIndex);
  if (id.isNotEmpty) {
    toolIndex[id] = items.length - 1;
  }
  return _enforceCap(
    t.copyWith(items: items, toolIndex: toolIndex, nextSeq: t.nextSeq + 1),
  );
}

SessionTranscript _enforceCap(SessionTranscript t) {
  if (t.items.length <= kMaxTranscriptItems) return t;

  final drop = t.items.length - kMaxTranscriptItems;
  final items = t.items.sublist(drop);

  // Rebuild tool index relative to the new list.
  final toolIndex = <String, int>{};
  for (var i = 0; i < items.length; i++) {
    final id = items[i].toolId;
    if (id != null && id.isNotEmpty && items[i].kind == ChatItemKind.tool) {
      toolIndex[id] = i;
    }
  }
  return t.copyWith(items: items, toolIndex: toolIndex);
}

bool _sameCommands(List<AvailableCommand> a, List<AvailableCommand> b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i].name != b[i].name || a[i].description != b[i].description) {
      return false;
    }
  }
  return true;
}

String _clip(String s, int max) {
  final trimmed = s.replaceAll(RegExp(r'\s+'), ' ').trim();
  if (trimmed.length <= max) return trimmed;
  return '${trimmed.substring(0, max)}…';
}

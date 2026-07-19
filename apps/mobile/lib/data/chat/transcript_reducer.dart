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
    if (s != null && s.isNotEmpty) {
      return current.copyWith(status: s);
    }
    return current;
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
      t = t.copyWith(
        pendingPermission: ev,
        items: [
          ...t.items,
          ChatItem.system(
            'Permission: ${ev.toolName ?? ev.text ?? 'tool'}',
          ),
        ],
      );
      t = _enforceCap(t);
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
    default:
      break;
  }
  return t;
}

SessionTranscript clearPendingPermission(
  SessionTranscript current, {
  String? permissionId,
}) {
  final pending = current.pendingPermission;
  if (pending == null) return current;
  if (permissionId != null && pending.permissionId != permissionId) {
    return current;
  }
  return current.copyWith(clearPendingPermission: true);
}

SessionTranscript _onTurnComplete(SessionTranscript t, SessionEvent ev) {
  var next = t.copyWith(status: 'idle', cancelAnnounced: false);
  final reason = (ev.stopReason ?? ev.status ?? '').trim();
  if (reason == 'cancelled' || reason == 'canceled') {
    if (!t.cancelAnnounced) {
      next = _append(
        next.copyWith(cancelAnnounced: true),
        ChatItem.system('Turn cancelled'),
      );
    }
    // Reset cancel flag after announcing (matches prior chat UI).
    next = next.copyWith(cancelAnnounced: false);
  } else if (reason.isNotEmpty &&
      reason != 'end_turn' &&
      reason != 'end-turn') {
    next = _append(next, ChatItem.system('Turn ended ($reason)'));
  }
  return next;
}

SessionTranscript markCancelAnnounced(SessionTranscript t) {
  if (t.cancelAnnounced) return t;
  return _append(
    t.copyWith(cancelAnnounced: true),
    ChatItem.system('Turn cancelled'),
  );
}

SessionTranscript _append(SessionTranscript t, ChatItem item) {
  return _enforceCap(t.copyWith(items: [...t.items, item]));
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
  final name = (ev.toolName ?? ev.text ?? 'Tool').trim();
  final status = (ev.status ?? '').trim();
  final detail = (ev.text ?? '').trim();
  final label = name.isEmpty ? 'Tool' : name;

  if (id.isNotEmpty && t.toolIndex.containsKey(id)) {
    final i = t.toolIndex[id]!;
    if (i >= 0 && i < t.items.length && t.items[i].kind == ChatItemKind.tool) {
      final items = List<ChatItem>.from(t.items);
      final prev = items[i];
      items[i] = ChatItem.tool(
        id: id,
        name: label,
        status: status.isNotEmpty ? status : prev.toolStatus,
        detail: detail.isNotEmpty ? detail : prev.text,
      );
      return t.copyWith(items: items);
    }
  }

  final item = ChatItem.tool(
    id: id,
    name: label,
    status: status,
    detail: detail,
  );
  final items = [...t.items, item];
  final toolIndex = Map<String, int>.from(t.toolIndex);
  if (id.isNotEmpty) {
    toolIndex[id] = items.length - 1;
  }
  return _enforceCap(t.copyWith(items: items, toolIndex: toolIndex));
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

String _clip(String s, int max) {
  final trimmed = s.replaceAll(RegExp(r'\s+'), ' ').trim();
  if (trimmed.length <= max) return trimmed;
  return '${trimmed.substring(0, max)}…';
}

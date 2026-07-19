import '../protocol/models.dart';

/// Soft cap on retained chat items per session (FIFO drop from the front).
const int kMaxTranscriptItems = 800;

enum ChatItemKind { user, assistant, thought, tool, system }

class ChatItem {
  const ChatItem({
    required this.kind,
    this.text,
    this.toolId,
    this.toolName,
    this.toolStatus,
  });

  final ChatItemKind kind;
  final String? text;
  final String? toolId;
  final String? toolName;
  final String? toolStatus;

  factory ChatItem.user(String t) => ChatItem(kind: ChatItemKind.user, text: t);
  factory ChatItem.assistant(String t) =>
      ChatItem(kind: ChatItemKind.assistant, text: t);
  factory ChatItem.thought(String t) =>
      ChatItem(kind: ChatItemKind.thought, text: t);
  factory ChatItem.system(String t) =>
      ChatItem(kind: ChatItemKind.system, text: t);
  factory ChatItem.tool({
    required String id,
    required String name,
    String? status,
    String? detail,
  }) =>
      ChatItem(
        kind: ChatItemKind.tool,
        toolId: id,
        toolName: name,
        toolStatus: status,
        text: detail,
      );

  ChatItem copyWith({
    String? text,
    String? toolStatus,
  }) =>
      ChatItem(
        kind: kind,
        text: text ?? this.text,
        toolId: toolId,
        toolName: toolName,
        toolStatus: toolStatus ?? this.toolStatus,
      );
}

/// In-memory transcript for one session (survives chat route disposal).
class SessionTranscript {
  const SessionTranscript({
    required this.sessionId,
    this.items = const [],
    this.status = 'idle',
    this.pendingPermission,
    this.cancelAnnounced = false,
    this.toolIndex = const {},
  });

  final String sessionId;
  final List<ChatItem> items;
  final String status;
  final SessionEvent? pendingPermission;
  final bool cancelAnnounced;

  /// toolId → index into [items] for O(1) upsert.
  final Map<String, int> toolIndex;

  SessionTranscript copyWith({
    List<ChatItem>? items,
    String? status,
    SessionEvent? pendingPermission,
    bool clearPendingPermission = false,
    bool? cancelAnnounced,
    Map<String, int>? toolIndex,
  }) {
    return SessionTranscript(
      sessionId: sessionId,
      items: items ?? this.items,
      status: status ?? this.status,
      pendingPermission: clearPendingPermission
          ? null
          : (pendingPermission ?? this.pendingPermission),
      cancelAnnounced: cancelAnnounced ?? this.cancelAnnounced,
      toolIndex: toolIndex ?? this.toolIndex,
    );
  }
}

class TranscriptsState {
  const TranscriptsState({this.byId = const {}});

  final Map<String, SessionTranscript> byId;

  SessionTranscript forSession(String id) =>
      byId[id] ?? SessionTranscript(sessionId: id);

  TranscriptsState upsert(SessionTranscript t) => TranscriptsState(
        byId: {...byId, t.sessionId: t},
      );

  TranscriptsState remove(String id) {
    if (!byId.containsKey(id)) return this;
    final next = Map<String, SessionTranscript>.from(byId)..remove(id);
    return TranscriptsState(byId: next);
  }

  TranscriptsState clearAll() => const TranscriptsState();
}

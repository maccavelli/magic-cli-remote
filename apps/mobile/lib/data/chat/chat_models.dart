import '../protocol/models.dart';

/// Soft cap on retained chat items per session (FIFO drop from the front).
const int kMaxTranscriptItems = 800;

enum ChatItemKind { user, assistant, thought, tool, system }

/// Coarse classification of an agent action, used to fold bursts of tool
/// activity into one collapsed row ("Ran 3 commands", "Edited 2 files",
/// "Used 4 tools").
enum ToolClass { command, fileEdit, other }

/// Classify a tool item from the agent-provided ACP tool kind, falling back to
/// name heuristics for agents that do not send one.
ToolClass classifyTool(String? toolKind, String? toolName) {
  switch ((toolKind ?? '').toLowerCase()) {
    case 'execute':
      return ToolClass.command;
    case 'edit':
    case 'delete':
    case 'move':
      return ToolClass.fileEdit;
    case 'read':
    case 'search':
    case 'fetch':
    case 'think':
    case 'switch_mode':
    case 'other':
      return ToolClass.other;
  }
  final n = (toolName ?? '').toLowerCase();
  if (n.contains('bash') ||
      n.contains('shell') ||
      n.contains('terminal') ||
      n.startsWith('run ') ||
      n.startsWith('exec')) {
    return ToolClass.command;
  }
  if (n.startsWith('edit') ||
      n.startsWith('write') ||
      n.startsWith('patch') ||
      n.startsWith('create file') ||
      n.startsWith('update ') ||
      n.startsWith('delete ')) {
    return ToolClass.fileEdit;
  }
  return ToolClass.other;
}

class ChatItem {
  const ChatItem({
    required this.kind,
    this.seq = 0,
    this.text,
    this.toolId,
    this.toolName,
    this.toolStatus,
    this.toolKind,
    this.isError = false,
  });

  final ChatItemKind kind;

  /// Monotonic per-transcript id. Stable across FIFO trims, so widget keys
  /// keep `ExpansionTile` state attached to the right item.
  final int seq;

  final String? text;
  final String? toolId;
  final String? toolName;
  final String? toolStatus;

  /// ACP tool-kind (`execute`, `edit`, …) as sent by the daemon; feeds
  /// [classifyTool] for action grouping.
  final String? toolKind;

  /// Classification for grouping; only meaningful for [ChatItemKind.tool].
  ToolClass get toolClass => classifyTool(toolKind, toolName);

  /// True while the daemon still reports this tool as in flight.
  bool get toolRunning => toolStatus == 'running' || toolStatus == 'pending';
  bool get toolFailed => toolStatus == 'failed' || toolStatus == 'error';

  /// True for system lines that represent a failure. Carried explicitly so
  /// styling never string-matches on a "Error:" prefix (a notice whose text
  /// merely starts that way must not render red).
  final bool isError;

  factory ChatItem.user(String t) => ChatItem(kind: ChatItemKind.user, text: t);
  factory ChatItem.assistant(String t) =>
      ChatItem(kind: ChatItemKind.assistant, text: t);
  factory ChatItem.thought(String t) =>
      ChatItem(kind: ChatItemKind.thought, text: t);
  factory ChatItem.system(String t, {bool error = false}) =>
      ChatItem(kind: ChatItemKind.system, text: t, isError: error);
  factory ChatItem.tool({
    required String id,
    required String name,
    String? status,
    String? detail,
    String? toolKind,
    int seq = 0,
  }) => ChatItem(
    kind: ChatItemKind.tool,
    seq: seq,
    toolId: id,
    toolName: name,
    toolStatus: status,
    toolKind: toolKind,
    text: detail,
  );

  ChatItem copyWith({
    int? seq,
    String? text,
    String? toolName,
    String? toolStatus,
    String? toolKind,
  }) => ChatItem(
    kind: kind,
    seq: seq ?? this.seq,
    text: text ?? this.text,
    toolId: toolId,
    toolName: toolName ?? this.toolName,
    toolStatus: toolStatus ?? this.toolStatus,
    toolKind: toolKind ?? this.toolKind,
    isError: isError,
  );
}

/// In-memory transcript for one session (survives chat route disposal).
class SessionTranscript {
  const SessionTranscript({
    required this.sessionId,
    this.items = const [],
    this.status = 'idle',
    this.pendingPermissions = const {},
    this.cancelAnnounced = false,
    this.toolIndex = const {},
    this.commands = const [],
    this.plan = const [],
    this.nextSeq = 0,
  });

  final String sessionId;
  final List<ChatItem> items;
  final String status;

  /// permissionId → request event, in arrival order.
  ///
  /// The daemon tracks concurrent permission requests in a map and blocks a
  /// goroutine on each, so a single slot here would strand every request but
  /// the newest and hang the agent turn.
  final Map<String, SessionEvent> pendingPermissions;

  final bool cancelAnnounced;

  /// toolId → index into [items] for O(1) upsert.
  final Map<String, int> toolIndex;

  /// ACP slash commands advertised for this session.
  final List<AvailableCommand> commands;

  /// Current agent plan (ACP `Plan`), replaced wholesale by each `plan` event.
  /// Rendered outside the scrolling transcript, so it is not a [ChatItem].
  final List<PlanEntry> plan;

  /// Next value for [ChatItem.seq].
  final int nextSeq;

  /// Oldest outstanding permission request, or null when none.
  SessionEvent? get pendingPermission =>
      pendingPermissions.isEmpty ? null : pendingPermissions.values.first;

  bool get hasPendingPermission => pendingPermissions.isNotEmpty;

  SessionTranscript copyWith({
    List<ChatItem>? items,
    String? status,
    Map<String, SessionEvent>? pendingPermissions,
    bool? cancelAnnounced,
    Map<String, int>? toolIndex,
    List<AvailableCommand>? commands,
    List<PlanEntry>? plan,
    int? nextSeq,
  }) {
    return SessionTranscript(
      sessionId: sessionId,
      items: items ?? this.items,
      status: status ?? this.status,
      pendingPermissions: pendingPermissions ?? this.pendingPermissions,
      cancelAnnounced: cancelAnnounced ?? this.cancelAnnounced,
      toolIndex: toolIndex ?? this.toolIndex,
      commands: commands ?? this.commands,
      plan: plan ?? this.plan,
      nextSeq: nextSeq ?? this.nextSeq,
    );
  }
}

class TranscriptsState {
  const TranscriptsState({this.byId = const {}});

  final Map<String, SessionTranscript> byId;

  SessionTranscript forSession(String id) =>
      byId[id] ?? SessionTranscript(sessionId: id);

  /// Existing transcript only — never materialises a new one.
  SessionTranscript? peek(String id) => byId[id];

  TranscriptsState upsert(SessionTranscript t) =>
      TranscriptsState(byId: {...byId, t.sessionId: t});

  TranscriptsState remove(String id) {
    if (!byId.containsKey(id)) return this;
    final next = Map<String, SessionTranscript>.from(byId)..remove(id);
    return TranscriptsState(byId: next);
  }

  /// Drop transcripts for sessions the host no longer knows about, so a
  /// server-side session death cannot leak its items for the process lifetime.
  TranscriptsState retainOnly(Set<String> liveIds) {
    if (byId.keys.every(liveIds.contains)) return this;
    final next = <String, SessionTranscript>{};
    for (final e in byId.entries) {
      if (liveIds.contains(e.key)) next[e.key] = e.value;
    }
    return TranscriptsState(byId: next);
  }

  TranscriptsState clearAll() => const TranscriptsState();
}

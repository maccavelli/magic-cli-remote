import '../protocol/models.dart';

/// Soft cap on retained chat items per session (FIFO drop from the front).
const int kMaxTranscriptItems = 800;

/// Matches host [historyMaxPage] / ring page size for `session.history` fetches.
/// Host ring and max page are 800 (MADR 0018 E4); clients auto-page when truncated.
const int kHistoryFetchLimit = 800;

/// Finalized assistant bubbles above this length get a "Show more" clamp (E3).
const int kAssistantShowMoreChars = 6000;

/// Phone-side durable cache: last N items per session (process death polish).
const int kTranscriptCacheMaxItems = 150;

/// Bound how many sessions keep a local cache entry.
const int kTranscriptCacheMaxSessions = 12;

/// Hard store clip for assistant / thought / tool detail text (chars).
const int kMaxItemTextChars = 100000;

/// Expanded tool/thought UI shows at most this many chars before scroll + copy.
const int kMaxExpandedDetailChars = 8000;

/// While streaming, full markdown parses only below this length; above it the
/// long-stream plain/mono path is used until the turn finalizes.
const int kMaxStreamingMarkdownChars = 4000;

/// Events applied per frame when hydrating history (MADR 0018 B4).
const int kHistoryApplyBatchSize = 40;

/// Suffix appended when store-clipping oversized item text.
const String kTextTruncatedMarker = '… [truncated]';

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
    this.errorKind,
    this.retryAt,
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

  /// Daemon error classification (`quota`, `rate_limit`) — drives the
  /// dedicated limit-hit card instead of the generic red error line.
  final String? errorKind;

  /// When the quota/rate limit resets, if the provider said.
  final DateTime? retryAt;

  bool get isLimitError => errorKind == 'quota' || errorKind == 'rate_limit';

  factory ChatItem.user(String t) => ChatItem(kind: ChatItemKind.user, text: t);
  factory ChatItem.assistant(String t) =>
      ChatItem(kind: ChatItemKind.assistant, text: t);
  factory ChatItem.thought(String t) =>
      ChatItem(kind: ChatItemKind.thought, text: t);
  factory ChatItem.system(
    String t, {
    bool error = false,
    String? errorKind,
    DateTime? retryAt,
  }) => ChatItem(
    kind: ChatItemKind.system,
    text: t,
    isError: error,
    errorKind: errorKind,
    retryAt: retryAt,
  );
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
    errorKind: errorKind,
    retryAt: retryAt,
  );

  Map<String, dynamic> toJson() => {
    'kind': kind.name,
    'seq': seq,
    if (text != null) 'text': text,
    if (toolId != null) 'toolId': toolId,
    if (toolName != null) 'toolName': toolName,
    if (toolStatus != null) 'toolStatus': toolStatus,
    if (toolKind != null) 'toolKind': toolKind,
    if (isError) 'isError': isError,
    if (errorKind != null) 'errorKind': errorKind,
    if (retryAt != null) 'retryAt': retryAt!.toIso8601String(),
  };

  factory ChatItem.fromJson(Map<String, dynamic> j) {
    final kindName = (j['kind'] as String?) ?? 'system';
    final kind = ChatItemKind.values.firstWhere(
      (k) => k.name == kindName,
      orElse: () => ChatItemKind.system,
    );
    DateTime? retryAt;
    final rawRetry = j['retryAt'];
    if (rawRetry is String && rawRetry.isNotEmpty) {
      retryAt = DateTime.tryParse(rawRetry);
    }
    return ChatItem(
      kind: kind,
      seq: (j['seq'] as num?)?.toInt() ?? 0,
      text: j['text'] as String?,
      toolId: j['toolId'] as String?,
      toolName: j['toolName'] as String?,
      toolStatus: j['toolStatus'] as String?,
      toolKind: j['toolKind'] as String?,
      isError: j['isError'] == true,
      errorKind: j['errorKind'] as String?,
      retryAt: retryAt,
    );
  }
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
    this.growableItems = false,
  });

  final String sessionId;
  final List<ChatItem> items;

  /// When true, [items] is exclusively owned by this transcript (batch flush)
  /// and last-index appends may mutate it in place (MADR 0018 D2).
  final bool growableItems;

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
    bool? growableItems,
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
      growableItems: growableItems ?? this.growableItems,
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

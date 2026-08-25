import 'dart:typed_data';

import '../protocol/models.dart';

/// An attachment shown on a user chat bubble. [bytes] is present only on the
/// device that sent the image (correlated from the local send buffer) and is
/// never persisted; [mimeType]/[kind] come from the user_message descriptor and
/// survive, so other devices / a cache reload render a labeled placeholder.
/// One native component of a user message (MADR 0112 A3).
///
/// A user turn can arrive as several native parts. They render as a single
/// bubble, but each keeps its own id so an authoritative snapshot replaces the
/// right component and a removal deletes exactly one.
class UserPart {
  const UserPart({
    required this.nativePartId,
    this.text = '',
    this.attachments = const [],
  });

  final String nativePartId;
  final String text;
  final List<ChatAttachment> attachments;

  Map<String, dynamic> toJson() => {
    'native_part_id': nativePartId,
    if (text.isNotEmpty) 'text': text,
    if (attachments.isNotEmpty)
      'attachments': [for (final a in attachments) a.toJson()],
  };

  static UserPart fromJson(Map<String, dynamic> json) => UserPart(
    nativePartId: (json['native_part_id'] as String?) ?? '',
    text: (json['text'] as String?) ?? '',
    attachments: [
      for (final a in (json['attachments'] as List<dynamic>? ?? const []))
        ChatAttachment.fromJson(a as Map<String, dynamic>),
    ],
  );
}

class ChatAttachment {
  const ChatAttachment({required this.kind, this.mimeType = '', this.bytes});

  final String kind;
  final String mimeType;
  final Uint8List? bytes;

  ChatAttachment withBytes(Uint8List b) =>
      ChatAttachment(kind: kind, mimeType: mimeType, bytes: b);

  Map<String, dynamic> toJson() => {'kind': kind, 'mime_type': mimeType};

  factory ChatAttachment.fromJson(Map<String, dynamic> j) => ChatAttachment(
    kind: j['kind'] as String? ?? '',
    mimeType: j['mime_type'] as String? ?? '',
  );
}

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

/// Streaming markdown size tiers (MADR 0057 Phase D / H-2). Frame-aligned
/// rebuilds always apply; above these lengths a minimum wall-clock interval
/// also applies so long replies do not re-parse the full document every frame.
/// (The former mono plain-text cliff at 4k was removed in MADR 0056 Phase 7.)
const int kStreamMdTier1Chars = 4000;
const int kStreamMdTier2Chars = 16000;

/// Events applied per frame when hydrating history (MADR 0018 B4).
const int kHistoryApplyBatchSize = 40;

/// Suffix appended when store-clipping oversized item text.
const String kTextTruncatedMarker = '… [truncated]';

enum ChatItemKind { user, assistant, thought, tool, system, approvals }

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
    this.attachments = const [],
    this.dedupeKey,
    this.approvals = const [],
    this.nativeMessageId,
    this.nativePartId,
    this.userParts = const [],
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

  /// Daemon error classification (`quota`, `rate_limit`, `auth`, `server`,
  /// `permission`) — drives a dedicated card instead of the generic red
  /// error line. Unknown values fall through to plain rendering (the
  /// vocabulary is additive; see protocol-v1.md).
  final String? errorKind;

  /// When the quota/rate limit resets, if the provider said.
  final DateTime? retryAt;

  bool get isLimitError => errorKind == 'quota' || errorKind == 'rate_limit';

  /// OS-level permission denial (MADR 0069 D4): sandbox policy, file modes,
  /// or macOS privacy protection. The daemon composes the actionable
  /// guidance into the message; the phone renders it in a distinct card.
  bool get isPermissionError => errorKind == 'permission';

  /// Provider credentials rejection (401/403 — MADR 0073 follow-up): the
  /// agent CLI on the daemon host needs re-authenticating; retrying from
  /// the phone cannot help.
  bool get isAuthError => errorKind == 'auth';

  /// Provider-side server failure (500/502/504): usually temporary, shares
  /// the limit card family with retry guidance.
  bool get isServerError => errorKind == 'server';

  /// Non-text blocks (images) sent with a user turn; empty for every other kind.
  final List<ChatAttachment> attachments;

  /// Identifies the *event* a notice describes, so a replay of that event does
  /// not append a second copy. Scoped to the thing being reported (one
  /// permission's timeout), never to the wording — matching on the text
  /// suppressed every later occurrence for the life of the session
  /// (MADR 0046 M-6).
  final String? dedupeKey;

  /// Auto-approved permissions on a [ChatItemKind.approvals] card; empty for
  /// every other kind. The whole list is replaced by each `approval_summary`
  /// event, so one card covers a turn rather than one line per approval
  /// (MADR 0051 Part I).
  final List<ApprovalItem> approvals;

  /// The agent's own identity for this row (MADR 0112 A3). Null for providers
  /// that supply none, and for rows cached before this existed; such rows keep
  /// the previous append-only behaviour and are never matched by identity.
  final String? nativeMessageId;
  final String? nativePartId;

  /// Ordered components of one user message, keyed by native part id.
  ///
  /// A user turn can be several native parts. They render as one bubble, so the
  /// components are cached in first-seen order and replaced in place: that
  /// makes removal exact — deleting a component and recomputing the aggregate —
  /// without guessing an id for a part the server created.
  final List<UserPart> userParts;

  /// Text derived from [userParts] when present, else the plain [text].
  String get userText {
    if (userParts.isEmpty) return text ?? '';
    return userParts.map((p) => p.text).where((t) => t.isNotEmpty).join();
  }

  factory ChatItem.user(
    String t, {
    List<ChatAttachment> attachments = const [],
  }) => ChatItem(kind: ChatItemKind.user, text: t, attachments: attachments);
  factory ChatItem.assistant(String t) =>
      ChatItem(kind: ChatItemKind.assistant, text: t);
  factory ChatItem.thought(String t) =>
      ChatItem(kind: ChatItemKind.thought, text: t);
  factory ChatItem.system(
    String t, {
    bool error = false,
    String? errorKind,
    DateTime? retryAt,
    String? dedupeKey,
  }) => ChatItem(
    kind: ChatItemKind.system,
    text: t,
    isError: error,
    errorKind: errorKind,
    retryAt: retryAt,
    dedupeKey: dedupeKey,
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

  /// One collapsing card for every permission auto-approved this turn.
  ///
  /// [id] is the daemon's `approval_group_id`, carried in [toolId] so the
  /// reducer can find and replace this card rather than appending a second one.
  factory ChatItem.approvals({
    required String id,
    required List<ApprovalItem> approvals,
    String? status,
    int seq = 0,
  }) => ChatItem(
    kind: ChatItemKind.approvals,
    seq: seq,
    toolId: id,
    toolStatus: status,
    approvals: approvals,
  );

  ChatItem copyWith({
    int? seq,
    String? text,
    String? toolName,
    String? toolStatus,
    String? toolKind,
    List<ChatAttachment>? attachments,
    List<ApprovalItem>? approvals,
    String? nativeMessageId,
    String? nativePartId,
    List<UserPart>? userParts,
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
    attachments: attachments ?? this.attachments,
    dedupeKey: dedupeKey,
    approvals: approvals ?? this.approvals,
    nativeMessageId: nativeMessageId ?? this.nativeMessageId,
    nativePartId: nativePartId ?? this.nativePartId,
    userParts: userParts ?? this.userParts,
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
    if (dedupeKey != null) 'dedupeKey': dedupeKey,
    // Descriptors only — image bytes are transient (sending device) and never
    // written to the cache.
    if (attachments.isNotEmpty)
      'attachments': [for (final a in attachments) a.toJson()],
    if (approvals.isNotEmpty)
      'approvals': [for (final a in approvals) a.toJson()],
    // Native identity is written additively (MADR 0112 A3). A cache entry
    // saved before this simply has no such keys and reads back as a legacy row.
    if (nativeMessageId != null) 'nativeMessageId': nativeMessageId,
    if (nativePartId != null) 'nativePartId': nativePartId,
    if (userParts.isNotEmpty)
      'userParts': [for (final p in userParts) p.toJson()],
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
      dedupeKey: j['dedupeKey'] as String?,
      // Absent for rows cached before native identity existed. They stay
      // readable and are never guessed at during removal (PLAN P4 step 10).
      nativeMessageId: j['nativeMessageId'] as String?,
      nativePartId: j['nativePartId'] as String?,
      userParts: switch (j['userParts']) {
        final List<dynamic> l => [
          for (final p in l)
            if (p is Map) UserPart.fromJson(Map<String, dynamic>.from(p)),
        ],
        _ => const <UserPart>[],
      },
      attachments: switch (j['attachments']) {
        final List<dynamic> l => [
          for (final a in l)
            if (a is Map) ChatAttachment.fromJson(Map<String, dynamic>.from(a)),
        ],
        _ => const [],
      },
      approvals: switch (j['approvals']) {
        final List<dynamic> l => [
          for (final a in l)
            if (a is Map) ApprovalItem.fromJson(Map<String, dynamic>.from(a)),
        ],
        _ => const [],
      },
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
    this.pendingQuestions = const {},
    this.cancelAnnounced = false,
    this.toolIndex = const {},
    this.commands = const [],
    this.remoteCommands = const [],
    this.plan = const [],
    this.subagents = const [],
    this.usage,
    this.capabilities,
    this.modes = const [],
    this.currentModeId,
    this.collaborationModes = const [],
    this.currentCollaborationModeId,
    this.goal,
    this.configOptions = const [],
    this.nextSeq = 0,
    this.growableItems = false,
    this.sealedTail = false,
  });

  final String sessionId;
  final List<ChatItem> items;

  /// When true, [items] is exclusively owned by this transcript (batch flush)
  /// and last-index appends may mutate it in place (MADR 0018 D2).
  final bool growableItems;

  /// When true, the last item was restored from the phone-side cache — a
  /// debounced snapshot that may end mid-conversation. A live chunk arriving
  /// after hydrate can belong to a *different* turn, so it must open a new
  /// bubble instead of merging into the restored one. Cleared by the first
  /// append of a new item.
  final bool sealedTail;

  final String status;

  /// permissionId → request event, in arrival order.
  ///
  /// The daemon tracks concurrent permission requests in a map and blocks a
  /// goroutine on each, so a single slot here would strand every request but
  /// the newest and hang the agent turn.
  final Map<String, SessionEvent> pendingPermissions;

  /// questionId → multi-question form event (OpenCode questions, MADR 0020 1b).
  final Map<String, SessionEvent> pendingQuestions;

  final bool cancelAnnounced;

  /// toolId → index into [items] for O(1) upsert.
  final Map<String, int> toolIndex;

  /// ACP slash commands advertised for this session.
  final List<AvailableCommand> commands;

  /// Canonical commands the daemon offers here (`remote_commands`), each marked
  /// available or not. Empty against a daemon too old to send them, which is
  /// when the composer falls back to its own built-in list.
  final List<RemoteCommand> remoteCommands;

  /// Current agent plan (ACP `Plan`), replaced wholesale by each `plan` event.
  /// Rendered outside the scrolling transcript, so it is not a [ChatItem].
  final List<PlanEntry> plan;

  /// Background sub-agents this session currently knows about, replaced
  /// wholesale by each `subagents` event. Like [plan], it lives outside the
  /// scrolling transcript: sub-agent *output* never appears in chat, only the
  /// fact that agents are running (MADR 0051 Part II).
  final List<SubagentInfo> subagents;

  /// Latest token/context usage report (ACP usage_update); null until the agent
  /// sends one. Advisory — drives the context-window indicator only.
  final Usage? usage;

  /// Agent capabilities (ACP session_capabilities); null until reported. Gates
  /// UI such as the image-attach button.
  final SessionCapabilities? capabilities;

  /// Available operating modes (ACP session_mode); empty when the agent exposes
  /// none. Absence hides the mode switcher.
  final List<SessionMode> modes;

  /// Active mode id (ACP session_mode / current_mode_update); null until known.
  final String? currentModeId;

  /// Collaboration presets (collaboration_mode); empty when the daemon
  /// does not expose the axis.
  final List<CollaborationMode> collaborationModes;

  /// Active collaboration-mode id; null until known.
  final String? currentCollaborationModeId;

  /// Current Codex thread goal; null when absent or cleared.
  final SessionGoal? goal;

  /// Agent-defined config options (ACP session_config); empty when none.
  final List<ConfigOption> configOptions;

  /// Next value for [ChatItem.seq].
  final int nextSeq;

  /// Oldest outstanding permission request, or null when none.
  SessionEvent? get pendingPermission =>
      pendingPermissions.isEmpty ? null : pendingPermissions.values.first;

  bool get hasPendingPermission => pendingPermissions.isNotEmpty;

  /// Oldest outstanding question form, or null when none.
  SessionEvent? get pendingQuestion =>
      pendingQuestions.isEmpty ? null : pendingQuestions.values.first;

  bool get hasPendingQuestion => pendingQuestions.isNotEmpty;

  /// True when the composer should block on a permission or question sheet.
  bool get hasBlockingPrompt => hasPendingPermission || hasPendingQuestion;

  static const Object _unset = Object();

  /// True when this snapshot holds live control state worth caching even
  /// without transcript items (MADR 0080 D10).
  bool get hasControlState =>
      modes.isNotEmpty ||
      currentModeId != null ||
      collaborationModes.isNotEmpty ||
      currentCollaborationModeId != null ||
      goal != null;

  SessionTranscript copyWith({
    List<ChatItem>? items,
    String? status,
    Map<String, SessionEvent>? pendingPermissions,
    Map<String, SessionEvent>? pendingQuestions,
    bool? cancelAnnounced,
    Map<String, int>? toolIndex,
    List<AvailableCommand>? commands,
    List<RemoteCommand>? remoteCommands,
    List<PlanEntry>? plan,
    List<SubagentInfo>? subagents,
    Usage? usage,
    SessionCapabilities? capabilities,
    List<SessionMode>? modes,
    Object? currentModeId = _unset,
    List<CollaborationMode>? collaborationModes,
    Object? currentCollaborationModeId = _unset,
    Object? goal = _unset,
    List<ConfigOption>? configOptions,
    int? nextSeq,
    bool? growableItems,
    bool? sealedTail,
  }) {
    return SessionTranscript(
      sessionId: sessionId,
      items: items ?? this.items,
      status: status ?? this.status,
      pendingPermissions: pendingPermissions ?? this.pendingPermissions,
      pendingQuestions: pendingQuestions ?? this.pendingQuestions,
      cancelAnnounced: cancelAnnounced ?? this.cancelAnnounced,
      toolIndex: toolIndex ?? this.toolIndex,
      commands: commands ?? this.commands,
      remoteCommands: remoteCommands ?? this.remoteCommands,
      plan: plan ?? this.plan,
      subagents: subagents ?? this.subagents,
      usage: usage ?? this.usage,
      capabilities: capabilities ?? this.capabilities,
      modes: modes ?? this.modes,
      currentModeId: identical(currentModeId, _unset)
          ? this.currentModeId
          : currentModeId as String?,
      collaborationModes: collaborationModes ?? this.collaborationModes,
      currentCollaborationModeId: identical(currentCollaborationModeId, _unset)
          ? this.currentCollaborationModeId
          : currentCollaborationModeId as String?,
      goal: identical(goal, _unset) ? this.goal : goal as SessionGoal?,
      configOptions: configOptions ?? this.configOptions,
      nextSeq: nextSeq ?? this.nextSeq,
      growableItems: growableItems ?? this.growableItems,
      sealedTail: sealedTail ?? this.sealedTail,
    );
  }
}

class TranscriptsState {
  const TranscriptsState({this.byId = const {}});

  final Map<String, SessionTranscript> byId;

  /// One canonical empty transcript per session id that has no data yet.
  ///
  /// [SessionTranscript] has no value equality — comparing whole transcripts
  /// on every commit is the hot path — so a fresh instance per lookup made the
  /// family provider for an unknown session report a change on *every* commit
  /// of *any* session, rebuilding a brand-new or history-less chat screen at
  /// streaming cadence (MADR 0046 L-7). Entries are dropped as soon as the
  /// session has real content, so this holds at most one small object per
  /// currently-empty session.
  static final Map<String, SessionTranscript> _empties = {};

  SessionTranscript forSession(String id) {
    final existing = byId[id];
    if (existing != null) {
      _empties.remove(id);
      return existing;
    }
    return _empties[id] ??= SessionTranscript(sessionId: id);
  }

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

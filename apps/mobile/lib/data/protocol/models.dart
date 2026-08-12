/// mcremote protocol models (v1 envelope + negotiated v2, MADR 0068).
library;

/// Protocol versions this client speaks (MADR 0068 D1).
const int kProtocolV1 = 1;
const int kProtocolV2 = 2;

/// The offer sent in auth/pair.claim, ascending.
const List<int> kSupportedProtocols = [kProtocolV1, kProtocolV2];

/// WebSocket close code: a newer connection of this device authenticated
/// and the daemon replaced this one (MADR 0068 D3). Must not auto-reconnect.
const int kCloseReplaced = 4001;

/// The v2 capability/limit block from `auth_ok.caps` (MADR 0068 D1).
/// Null on v1 daemons — consumers fall back to the shipped constants.
class ServerCaps {
  const ServerCaps({
    required this.protocol,
    required this.readDeadlineMs,
    required this.pingIntervalMs,
    required this.wsPingResetsDeadline,
    required this.historyRing,
    required this.maxFrameBytes,
    required this.tlsResumed,
    this.resumeWindowMs,
    this.epoch,
    this.receipts = false,
    this.providerAuth = false,
  });

  final int protocol;
  final int readDeadlineMs;
  final int pingIntervalMs;
  final bool wsPingResetsDeadline;
  final int historyRing;
  final int maxFrameBytes;

  /// Whether this connection's TLS handshake resumed a prior session —
  /// the observable for the SecurityContext cache (0068 Q3/P5).
  final bool tlsResumed;

  /// Resume window; null until the daemon implements 0068 P4.
  final int? resumeWindowMs;

  /// Daemon seq-lineage id (MADR 0068 P3); null when the daemon has no
  /// session store.
  final String? epoch;

  /// Whether the daemon keeps signed receipts (MADR 0078 D7): the phone shows
  /// its receipts UI only when true. Absent/false on daemons without receipts
  /// configured.
  final bool receipts;

  /// Whether the daemon can report and change upstream provider credentials
  /// (MADR 0074 D6). Every auth affordance in the UI is gated on this: a
  /// daemon without it behaves exactly as it did before the feature existed.
  final bool providerAuth;

  static ServerCaps? tryParse(Object? raw) {
    if (raw is! Map) return null;
    final m = Map<String, dynamic>.from(raw);
    final protocol = (m['protocol'] as num?)?.toInt();
    if (protocol == null || protocol < kProtocolV2) return null;
    int? resumeWindowMs;
    final resume = m['resume'];
    if (resume is Map) {
      resumeWindowMs = (resume['window_ms'] as num?)?.toInt();
    }
    return ServerCaps(
      protocol: protocol,
      readDeadlineMs: (m['read_deadline_ms'] as num?)?.toInt() ?? 60000,
      pingIntervalMs: (m['ping_interval_ms'] as num?)?.toInt() ?? 10000,
      wsPingResetsDeadline: m['ws_ping_resets_deadline'] == true,
      historyRing: (m['history_ring'] as num?)?.toInt() ?? 800,
      maxFrameBytes: (m['max_frame_bytes'] as num?)?.toInt() ?? (1 << 20),
      tlsResumed: m['tls_resumed'] == true,
      resumeWindowMs: resumeWindowMs,
      epoch: m['epoch'] as String?,
      receipts: m['receipts'] == true,
      providerAuth: m['provider_auth'] == true,
    );
  }
}

/// One paired device in the handoff-target roster (MADR 0078), from
/// `devices.list`. Identity fields only.
class DeviceInfo {
  const DeviceInfo({
    required this.deviceId,
    required this.name,
    this.isSelf = false,
  });

  final String deviceId;
  final String name;

  /// Whether this row is the calling device itself (excluded from a handoff
  /// picker).
  final bool isSelf;

  factory DeviceInfo.fromJson(Map<String, dynamic> json) => DeviceInfo(
    deviceId: json['device_id'] as String? ?? '',
    name: json['name'] as String? ?? '',
    isSelf: json['self'] == true,
  );
}

class Envelope {
  Envelope({this.v = 1, required this.type, this.id, this.payload, this.token});

  final int v;
  final String type;
  final String? id;
  final Map<String, dynamic>? payload;
  final String? token;

  Map<String, dynamic> toJson() {
    final m = <String, dynamic>{'v': v, 'type': type};
    if (id != null && id!.isNotEmpty) m['id'] = id;
    if (payload != null) m['payload'] = payload;
    if (token != null) m['token'] = token;
    return m;
  }

  factory Envelope.fromJson(Map<String, dynamic> json) {
    Map<String, dynamic>? payload;
    final raw = json['payload'];
    if (raw is Map<String, dynamic>) {
      payload = raw;
    } else if (raw is Map) {
      payload = Map<String, dynamic>.from(raw);
    }
    // Missing v is treated as 1 for older peers; an explicit non-1 is rejected
    // by the connection layer (MADR 0056 M-2).
    final v = (json['v'] as num?)?.toInt() ?? 1;
    return Envelope(
      v: v,
      type: json['type'] as String? ?? '',
      id: json['id'] as String?,
      payload: payload,
      token: json['token'] as String?,
    );
  }
}

/// Retained-seq window for one session (MADR 0068 P3): a cached seq below
/// [first] means the ring truncated past it; one equal to [latest] means
/// the history walk can be skipped entirely.
class SeqBounds {
  const SeqBounds({required this.first, required this.latest});

  final int first;
  final int latest;
}

/// Result of `session.list` including completeness (MADR 0056 H-6).
///
/// [complete] is true only when the host marked the snapshot complete. Clients
/// must not destructively prune local transcripts/cache unless [complete] is
/// true.
class SessionListSnapshot {
  const SessionListSnapshot({
    required this.sessions,
    required this.complete,
    this.degraded = false,
    this.skipped = 0,
    this.epoch,
    this.seqs = const {},
  });

  final List<SessionMeta> sessions;
  final bool complete;
  final bool degraded;
  final int skipped;

  /// Daemon seq-lineage id (MADR 0068 P3); null on pre-P3 daemons. A change
  /// between snapshots means every cached seq is stale.
  final String? epoch;

  /// Per-session retained-seq windows; empty on pre-P3 daemons.
  final Map<String, SeqBounds> seqs;
}

/// Additive `session.diff_result` body (MADR 0080 D15). [summary] remains
/// authoritative for older clients; extra fields may be absent.
class SessionDiffResult {
  const SessionDiffResult({
    this.sessionId = '',
    this.summary = '',
    this.baseSha = '',
    this.scope = '',
    this.truncated = false,
  });

  final String sessionId;
  final String summary;
  final String baseSha;
  final String scope;
  final bool truncated;

  factory SessionDiffResult.fromJson(Map<String, dynamic> json) =>
      SessionDiffResult(
        sessionId: json['session_id'] as String? ?? '',
        summary: json['summary'] as String? ?? '',
        baseSha: json['base_sha'] as String? ?? '',
        scope: json['scope'] as String? ?? '',
        truncated: json['truncated'] == true,
      );
}

class SessionMeta {
  SessionMeta({
    required this.id,
    required this.provider,
    this.name = '',
    this.model = '',
    this.thinkingLevel = '',
    this.cwd,
    this.agentSessionId,
    this.ownerDeviceId,
    this.pendingHandoffTo,
    this.createdAt,
    this.status = 'idle',
    this.live = true,
  });

  final String id;
  final String provider;
  final String name;

  /// Agent model this session was last (re)started with; empty = provider
  /// default.
  final String model;

  /// Reasoning/thinking effort override for this session; empty = provider
  /// default (MADR 0052).
  final String thinkingLevel;
  final String? cwd;
  final String? agentSessionId;

  /// Owning paired device id when the host includes it. Null/empty means the
  /// session is unowned — a legacy session or one released for handoff, and
  /// therefore claimable by this device (MADR 0078).
  final String? ownerDeviceId;

  /// When a released session is targeted at one device (MADR 0078 D2), that
  /// device id. Null for an owned session or an open release. Only ever set
  /// while `ownerDeviceId` is empty.
  final String? pendingHandoffTo;
  final DateTime? createdAt;
  final String status;
  final bool live;

  factory SessionMeta.fromJson(Map<String, dynamic> json) {
    DateTime? created;
    final c = json['created_at'];
    if (c is String) created = DateTime.tryParse(c);
    return SessionMeta(
      id: json['id'] as String? ?? '',
      provider: json['provider'] as String? ?? '',
      name: json['name'] as String? ?? '',
      model: json['model'] as String? ?? '',
      thinkingLevel: json['thinking_level'] as String? ?? '',
      cwd: json['cwd'] as String?,
      agentSessionId: json['agent_session_id'] as String?,
      ownerDeviceId: json['owner_device_id'] as String?,
      pendingHandoffTo: json['pending_handoff_to'] as String?,
      createdAt: created,
      status: json['status'] as String? ?? 'idle',
      live: json['live'] as bool? ?? true,
    );
  }

  /// Whether this session is unowned and can be claimed by this device — a
  /// released or legacy session (MADR 0078). Owned sessions (by definition
  /// owned by this device, since the host only shows a device its own + the
  /// unowned) are not claimable.
  bool get isClaimable => ownerDeviceId == null || ownerDeviceId!.isEmpty;

  SessionMeta copyWith({
    String? status,
    bool? live,
    String? name,
    String? thinkingLevel,
  }) {
    return SessionMeta(
      id: id,
      provider: provider,
      name: name ?? this.name,
      model: model,
      thinkingLevel: thinkingLevel ?? this.thinkingLevel,
      cwd: cwd,
      agentSessionId: agentSessionId,
      ownerDeviceId: ownerDeviceId,
      pendingHandoffTo: pendingHandoffTo,
      createdAt: createdAt,
      status: status ?? this.status,
      live: live ?? this.live,
    );
  }
}

/// Metadata-only provider-native session returned by `agent_sessions.list`.
/// Selecting it still creates a normal daemon-owned session through
/// `session.create` with `agent_session_id`.
class AgentSessionMeta {
  AgentSessionMeta({
    required this.id,
    this.cwd = '',
    this.title = '',
    this.updatedAt,
  });

  final String id;
  final String cwd;
  final String title;
  final DateTime? updatedAt;

  String get displayName => title.isNotEmpty ? title : id;

  factory AgentSessionMeta.fromJson(Map<String, dynamic> json) {
    final updated = json['updated_at'];
    return AgentSessionMeta(
      id: json['id'] as String? ?? '',
      cwd: json['cwd'] as String? ?? '',
      title: json['title'] as String? ?? '',
      updatedAt: updated is String ? DateTime.tryParse(updated) : null,
    );
  }
}

/// Bounded, read-only project metadata returned by `session.diagnostics`.
/// It deliberately has no repository paths, patches, URLs, or credential data.
class SessionDiagnostics {
  SessionDiagnostics({
    this.branch = '',
    this.defaultBranch = '',
    this.vcs,
    this.mcp = const [],
  });

  final String branch;
  final String defaultBranch;
  final VcsStatusSummary? vcs;
  final List<McpServerStatus> mcp;

  factory SessionDiagnostics.fromJson(Map<String, dynamic> json) {
    final rawMcp = json['mcp'];
    return SessionDiagnostics(
      branch: json['branch'] as String? ?? '',
      defaultBranch: json['default_branch'] as String? ?? '',
      vcs: json['vcs'] is Map
          ? VcsStatusSummary.fromJson(
              Map<String, dynamic>.from(json['vcs'] as Map),
            )
          : null,
      mcp: rawMcp is List
          ? rawMcp
                .whereType<Map>()
                .map(
                  (e) => McpServerStatus.fromJson(Map<String, dynamic>.from(e)),
                )
                .where((e) => e.name.isNotEmpty && e.state.isNotEmpty)
                .toList(growable: false)
          : const [],
    );
  }
}

class VcsStatusSummary {
  VcsStatusSummary({
    this.added = 0,
    this.modified = 0,
    this.deleted = 0,
    this.additions = 0,
    this.deletions = 0,
  });

  final int added;
  final int modified;
  final int deleted;
  final int additions;
  final int deletions;

  bool get isEmpty =>
      added == 0 &&
      modified == 0 &&
      deleted == 0 &&
      additions == 0 &&
      deletions == 0;

  factory VcsStatusSummary.fromJson(Map<String, dynamic> json) {
    int value(String key) => (json[key] as num?)?.toInt() ?? 0;
    return VcsStatusSummary(
      added: value('added'),
      modified: value('modified'),
      deleted: value('deleted'),
      additions: value('additions'),
      deletions: value('deletions'),
    );
  }
}

class McpServerStatus {
  McpServerStatus({required this.name, required this.state});

  final String name;
  final String state;

  factory McpServerStatus.fromJson(Map<String, dynamic> json) =>
      McpServerStatus(
        name: json['name'] as String? ?? '',
        state: json['state'] as String? ?? '',
      );
}

class ProviderInfo {
  ProviderInfo({required this.id, required this.ready, this.auth});

  final String id;
  final bool ready;

  /// Upstream credential state (MADR 0074 D4). Null on a daemon without the
  /// feature, or on a connection that did not negotiate `provider_auth` — in
  /// which case the UI shows exactly what it always did.
  final ProviderAuthInfo? auth;

  factory ProviderInfo.fromJson(Map<String, dynamic> json) {
    return ProviderInfo(
      id: json['id'] as String? ?? '',
      ready: json['ready'] as bool? ?? false,
      auth: ProviderAuthInfo.tryParse(json['auth']),
    );
  }
}

/// Auth status values (MADR 0074 D3). `quota` is distinct from `error` on
/// purpose: a rate-limited upstream needs waiting or switching, not a new key.
class AuthStatus {
  static const configured = 'configured';
  static const missing = 'missing';
  static const error = 'error';
  static const quota = 'quota';
}

/// Auth method types (MADR 0074 D5).
class AuthMethodType {
  static const apiKey = 'api_key';
  static const oauthDevice = 'oauth_device';
  static const oauthBrowser = 'oauth_browser';
}

/// One choice in a select input. Label and value differ ("Resource name" vs
/// "resourceName"), so submitting the label would send the wrong thing.
class AuthInputOption {
  const AuthInputOption({required this.value, this.label, this.hint});

  final String value;
  final String? label;
  final String? hint;

  String get display => (label != null && label!.isNotEmpty) ? label! : value;

  factory AuthInputOption.fromJson(Map<String, dynamic> j) => AuthInputOption(
    value: j['value'] as String? ?? '',
    label: j['label'] as String?,
    hint: j['hint'] as String?,
  );
}

/// Hides an input until another input holds a given value — Azure asks for
/// `resourceName` only when `endpointType` is `resourceName`. Without this the
/// form would show mutually exclusive fields side by side.
class AuthInputCondition {
  const AuthInputCondition({
    required this.key,
    required this.op,
    required this.value,
  });

  final String key;
  final String op;
  final String value;

  /// Whether the field should be visible given the current answers. An
  /// unrecognised operator shows the field: hiding something the user may
  /// need is worse than showing one field too many.
  bool satisfiedBy(Map<String, String> answers) {
    if (op != 'eq') return true;
    return (answers[key] ?? '') == value;
  }

  static AuthInputCondition? tryParse(Object? raw) {
    if (raw is! Map) return null;
    final m = Map<String, dynamic>.from(raw);
    final key = m['key'] as String?;
    if (key == null || key.isEmpty) return null;
    return AuthInputCondition(
      key: key,
      op: m['op'] as String? ?? 'eq',
      value: m['value'] as String? ?? '',
    );
  }
}

/// One field a method needs before it can run (MADR 0074 D5).
class AuthInput {
  const AuthInput({
    required this.key,
    required this.type,
    this.message,
    this.options = const [],
    this.placeholder,
    this.required = false,
    this.when,
  });

  final String key;
  final String type;
  final String? message;
  final List<AuthInputOption> options;
  final String? placeholder;
  final bool required;
  final AuthInputCondition? when;

  bool get isSelect => type == 'select';

  factory AuthInput.fromJson(Map<String, dynamic> j) => AuthInput(
    key: j['key'] as String? ?? '',
    type: j['type'] as String? ?? 'text',
    message: j['message'] as String?,
    options: ((j['options'] as List?) ?? const [])
        .whereType<Map>()
        .map((o) => AuthInputOption.fromJson(Map<String, dynamic>.from(o)))
        .toList(),
    placeholder: j['placeholder'] as String?,
    required: j['required'] == true,
    when: AuthInputCondition.tryParse(j['when']),
  );
}

/// One way to authenticate an upstream.
class AuthMethod {
  const AuthMethod({
    required this.id,
    required this.type,
    required this.label,
    this.inputs = const [],
  });

  final String id;
  final String type;
  final String label;
  final List<AuthInput> inputs;

  bool get isApiKey => type == AuthMethodType.apiKey;
  bool get isDeviceOAuth => type == AuthMethodType.oauthDevice;

  /// Browser OAuth needs a callback to the host's own loopback, which a phone
  /// browser cannot reach. Rendered but disabled until the tunnel workstream.
  bool get isBrowserOAuth => type == AuthMethodType.oauthBrowser;

  factory AuthMethod.fromJson(Map<String, dynamic> j) => AuthMethod(
    id: j['id'] as String? ?? '',
    type: j['type'] as String? ?? AuthMethodType.apiKey,
    label: j['label'] as String? ?? '',
    inputs: ((j['inputs'] as List?) ?? const [])
        .whereType<Map>()
        .map((i) => AuthInput.fromJson(Map<String, dynamic>.from(i)))
        .toList(),
  );
}

/// One model vendor reachable through an agent.
class UpstreamAuth {
  const UpstreamAuth({
    required this.id,
    required this.status,
    this.label,
    this.methods = const [],
  });

  final String id;
  final String status;
  final String? label;
  final List<AuthMethod> methods;

  String get display => (label != null && label!.isNotEmpty) ? label! : id;
  bool get isConfigured => status == AuthStatus.configured;

  factory UpstreamAuth.fromJson(Map<String, dynamic> j) => UpstreamAuth(
    id: j['id'] as String? ?? '',
    status: j['status'] as String? ?? AuthStatus.missing,
    label: j['label'] as String?,
    methods: ((j['methods'] as List?) ?? const [])
        .whereType<Map>()
        .map((m) => AuthMethod.fromJson(Map<String, dynamic>.from(m)))
        .toList(),
  );
}

/// An agent's whole credential picture (MADR 0074 D4).
class ProviderAuthInfo {
  const ProviderAuthInfo({
    required this.status,
    this.activeUpstream,
    this.upstreams = const [],
  });

  final String status;
  final String? activeUpstream;
  final List<UpstreamAuth> upstreams;

  static ProviderAuthInfo? tryParse(Object? raw) {
    if (raw is! Map) return null;
    final m = Map<String, dynamic>.from(raw);
    return ProviderAuthInfo(
      status: m['status'] as String? ?? AuthStatus.missing,
      activeUpstream: m['active_upstream'] as String?,
      upstreams: ((m['upstreams'] as List?) ?? const [])
          .whereType<Map>()
          .map((u) => UpstreamAuth.fromJson(Map<String, dynamic>.from(u)))
          .toList(),
    );
  }
}

class PermissionOption {
  PermissionOption({required this.optionId, required this.name, this.kind});

  final String optionId;
  final String name;
  final String? kind;

  factory PermissionOption.fromJson(Map<String, dynamic> json) {
    return PermissionOption(
      optionId: json['option_id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      kind: json['kind'] as String?,
    );
  }
}

/// One item in a multi-question form (`question_request`, MADR 0020 Sprint 1b).
class QuestionItem {
  QuestionItem({
    this.header = '',
    this.text = '',
    this.multiple = false,
    this.custom = false,
    this.options = const [],
  });

  final String header;
  final String text;
  final bool multiple;
  final bool custom;
  final List<PermissionOption> options;

  factory QuestionItem.fromJson(Map<String, dynamic> json) {
    final opts = <PermissionOption>[];
    final rawOpts = json['options'];
    if (rawOpts is List) {
      for (final o in rawOpts) {
        if (o is Map<String, dynamic>) {
          opts.add(PermissionOption.fromJson(o));
        } else if (o is Map) {
          opts.add(PermissionOption.fromJson(Map<String, dynamic>.from(o)));
        }
      }
    }
    return QuestionItem(
      header: json['header'] as String? ?? '',
      text: json['text'] as String? ?? '',
      multiple: json['multiple'] == true,
      custom: json['custom'] == true,
      options: opts,
    );
  }
}

class AvailableCommand {
  AvailableCommand({required this.name, this.description = '', this.hint = ''});

  final String name;
  final String description;
  final String hint;

  /// Text inserted into the composer when the user picks this command.
  String get insertText {
    final n = name.startsWith('/') ? name : '/$name';
    return '$n ';
  }

  factory AvailableCommand.fromJson(Map<String, dynamic> json) {
    var name = (json['name'] as String? ?? '').trim();
    if (name.startsWith('/')) name = name.substring(1);
    return AvailableCommand(
      name: name,
      description: json['description'] as String? ?? '',
      hint: json['hint'] as String? ?? '',
    );
  }
}

/// One canonical slash command the daemon offers in this session, carried by the
/// `remote_commands` event. Unavailable commands are still sent, with [reason],
/// so the composer can explain instead of silently hiding them.
class RemoteCommand {
  RemoteCommand({
    required this.name,
    this.hint = '',
    this.description = '',
    this.available = false,
    this.reason = '',
  });

  final String name;
  final String hint;
  final String description;
  final bool available;
  final String reason;

  /// The command as an autocomplete entry.
  AvailableCommand get asCommand =>
      AvailableCommand(name: name, description: description, hint: hint);

  factory RemoteCommand.fromJson(Map<String, dynamic> json) {
    var name = (json['name'] as String? ?? '').trim();
    if (name.startsWith('/')) name = name.substring(1);
    return RemoteCommand(
      name: name,
      hint: json['hint'] as String? ?? '',
      description: json['description'] as String? ?? '',
      available: json['available'] == true,
      reason: json['reason'] as String? ?? '',
    );
  }
}

/// One line of an agent plan (ACP `Plan` entry). Carried by the `plan` event.
class PlanEntry {
  PlanEntry({
    required this.content,
    this.status = 'pending',
    this.priority = 'medium',
  });

  final String content;

  /// One of `pending`, `in_progress`, `completed`.
  final String status;

  /// One of `high`, `medium`, `low`.
  final String priority;

  factory PlanEntry.fromJson(Map<String, dynamic> json) {
    return PlanEntry(
      content: json['content'] as String? ?? '',
      status: json['status'] as String? ?? 'pending',
      priority: json['priority'] as String? ?? 'medium',
    );
  }
}

/// One background sub-agent, carried by the `subagents` event.
///
/// Status only. A sub-agent's *output* never reaches the transcript — it
/// reports to the main agent over the engine's own channel and the parent's
/// reply carries the conclusion, so all the client needs is what is running
/// (MADR 0051 Part II).
class SubagentInfo {
  const SubagentInfo({
    required this.id,
    required this.name,
    this.task = '',
    this.status = 'running',
  });

  /// Provider-scoped and opaque: an OpenCode child session id, a grok
  /// `subagent_id`, or a codex agent thread id. Used only as a list key.
  final String id;

  /// The agent's role or kind — `general`, `explore`, …
  final String name;

  /// What it was asked to do; may be empty.
  final String task;

  /// One of `running`, `completed`, `failed`.
  final String status;

  bool get isRunning => status == 'running';

  factory SubagentInfo.fromJson(Map<String, dynamic> json) => SubagentInfo(
    id: json['id'] as String? ?? '',
    name: json['name'] as String? ?? 'subagent',
    task: json['task'] as String? ?? '',
    status: json['status'] as String? ?? 'running',
  );

  @override
  bool operator ==(Object other) =>
      other is SubagentInfo &&
      other.id == id &&
      other.name == name &&
      other.task == task &&
      other.status == status;

  @override
  int get hashCode => Object.hash(id, name, task, status);
}

/// One auto-approved permission inside an `approval_summary` event.
///
/// Auto-approve must not mean invisible: the user has to be able to scroll back
/// and see what ran on their behalf. These are the rows of that audit
/// (MADR 0051 Part I).
class ApprovalItem {
  const ApprovalItem({required this.toolName, this.detail = '', this.time});

  /// `bash`, `file`, `shell`, `mcp`, …
  final String toolName;

  /// A human summary — a command, a path, a pattern list. Never raw tool JSON.
  final String detail;

  /// When the approval happened; null when the daemon omitted or malformed it.
  final DateTime? time;

  /// Collapsed label: `bash (git status)`, or just the tool name when the
  /// daemon had no detail to give.
  String get label => detail.isEmpty ? toolName : '$toolName ($detail)';

  factory ApprovalItem.fromJson(Map<String, dynamic> json) {
    final raw = json['time'];
    return ApprovalItem(
      toolName: json['tool_name'] as String? ?? 'permission',
      detail: json['detail'] as String? ?? '',
      time: raw is String && raw.isNotEmpty ? DateTime.tryParse(raw) : null,
    );
  }

  Map<String, dynamic> toJson() => {
    'tool_name': toolName,
    if (detail.isNotEmpty) 'detail': detail,
    if (time != null) 'time': time!.toIso8601String(),
  };
}

/// Token/context report carried on `usage_update` events (ACP usage_update):
/// [used] tokens currently in context out of a [size]-token window.
class Usage {
  const Usage({required this.used, required this.size});

  final int used;
  final int size;

  /// Fraction of the context window in use, clamped to [0,1]. 0 when the
  /// agent did not report a window size.
  double get fraction => size > 0 ? (used / size).clamp(0.0, 1.0) : 0.0;

  factory Usage.fromJson(Map<String, dynamic> json) {
    return Usage(
      used: (json['used'] as num?)?.toInt() ?? 0,
      size: (json['size'] as num?)?.toInt() ?? 0,
    );
  }

  @override
  bool operator ==(Object other) =>
      other is Usage && other.used == used && other.size == size;

  @override
  int get hashCode => Object.hash(used, size);
}

/// A non-text prompt content block sent with a prompt (ACP image/audio).
/// [kind] is "image" or "audio"; [data] is base64-encoded; [mimeType] is the
/// media type (e.g. "image/png").
class PromptAttachment {
  const PromptAttachment({
    required this.kind,
    required this.mimeType,
    required this.data,
  });

  final String kind;
  final String mimeType;
  final String data;

  Map<String, dynamic> toJson() => {
    'kind': kind,
    'mime_type': mimeType,
    'data': data,
  };
}

/// Descriptor for a non-text block sent with a prompt, carried on user_message
/// events (kind + media type; never the payload bytes).
class AttachmentInfo {
  const AttachmentInfo({required this.kind, this.mimeType = ''});

  final String kind;
  final String mimeType;

  factory AttachmentInfo.fromJson(Map<String, dynamic> json) {
    return AttachmentInfo(
      kind: json['kind'] as String? ?? '',
      mimeType: json['mime_type'] as String? ?? '',
    );
  }
}

/// Maps a JSON list to a typed list via [fromJson], tolerating both
/// `Map<String,dynamic>` and plain `Map` elements. Non-list input → empty.
List<T> _mapList<T>(dynamic raw, T Function(Map<String, dynamic>) fromJson) {
  if (raw is! List) return const [];
  final out = <T>[];
  for (final e in raw) {
    if (e is Map<String, dynamic>) {
      out.add(fromJson(e));
    } else if (e is Map) {
      out.add(fromJson(Map<String, dynamic>.from(e)));
    }
  }
  return out;
}

/// Agent capabilities negotiated at ACP initialize, carried on
/// `session_capabilities` events. Gates client UI (e.g. the image-attach
/// button hides when [image] is false).
class SessionCapabilities {
  const SessionCapabilities({
    required this.image,
    required this.audio,
    required this.loadSession,
    required this.embeddedContext,
    required this.listSessions,
    required this.closeSession,
    required this.mcpHttp,
    required this.mcpSse,
    required this.mcpAcp,
  });

  final bool image;
  final bool audio;
  final bool loadSession;
  final bool embeddedContext;
  final bool listSessions;
  final bool closeSession;
  final bool mcpHttp;
  final bool mcpSse;
  final bool mcpAcp;

  factory SessionCapabilities.fromJson(Map<String, dynamic> json) {
    return SessionCapabilities(
      image: json['image'] == true,
      audio: json['audio'] == true,
      loadSession: json['load_session'] == true,
      embeddedContext: json['embedded_context'] == true,
      listSessions: json['list_sessions'] == true,
      closeSession: json['close_session'] == true,
      mcpHttp: json['mcp_http'] == true,
      mcpSse: json['mcp_sse'] == true,
      mcpAcp: json['mcp_acp'] == true,
    );
  }

  @override
  bool operator ==(Object other) =>
      other is SessionCapabilities &&
      other.image == image &&
      other.audio == audio &&
      other.loadSession == loadSession &&
      other.embeddedContext == embeddedContext &&
      other.listSessions == listSessions &&
      other.closeSession == closeSession &&
      other.mcpHttp == mcpHttp &&
      other.mcpSse == mcpSse &&
      other.mcpAcp == mcpAcp;

  @override
  int get hashCode => Object.hash(
    image,
    audio,
    loadSession,
    embeddedContext,
    listSessions,
    closeSession,
    mcpHttp,
    mcpSse,
    mcpAcp,
  );
}

/// One selectable agent operating mode (ACP SessionMode), carried on
/// `session_mode` events.
class SessionMode {
  const SessionMode({
    required this.id,
    required this.name,
    this.description = '',
    this.dangerous = false,
  });

  final String id;
  final String name;
  final String description;

  /// Whether this mode removes a safety control the user would otherwise
  /// have — today, one that answers permission requests without them.
  ///
  /// The daemon declares this; the UI never infers it from the mode id. Only
  /// the provider knows what a mode costs: goose has shipped an `auto` mode
  /// for a while and it is goose's *default*, so id-matching would alarm on a
  /// perfectly normal state. Defaults to false, which is what every daemon
  /// predating the field sends.
  final bool dangerous;

  factory SessionMode.fromJson(Map<String, dynamic> json) {
    return SessionMode(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      description: json['description'] as String? ?? '',
      dangerous: json['dangerous'] as bool? ?? false,
    );
  }

  Map<String, dynamic> toJson() => {
    'id': id,
    'name': name,
    if (description.isNotEmpty) 'description': description,
    if (dangerous) 'dangerous': true,
  };

  // Value equality so the reducer can return the identical transcript when a
  // re-sent `session_mode` carries nothing new, the way `Usage` and
  // `SessionCapabilities` already do (MADR 0042 D8).
  @override
  // `dangerous` participates in equality: without it, a mode list that differs
  // *only* in that flag compares equal, the reducer discards the update as
  // "nothing new", and the chip never changes — silently defeating the whole
  // point of the flag.
  bool operator ==(Object other) =>
      other is SessionMode &&
      other.id == id &&
      other.name == name &&
      other.description == description &&
      other.dangerous == dangerous;

  @override
  int get hashCode => Object.hash(id, name, description, dangerous);
}

/// One Codex (or compatible) collaboration preset on `collaboration_mode`.
/// Distinct from [SessionMode]: Plan is not an autonomy/permission mode.
class CollaborationMode {
  const CollaborationMode({
    required this.id,
    required this.name,
    this.description = '',
  });

  final String id;
  final String name;
  final String description;

  factory CollaborationMode.fromJson(Map<String, dynamic> json) {
    return CollaborationMode(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      description: json['description'] as String? ?? '',
    );
  }

  Map<String, dynamic> toJson() => {
    'id': id,
    'name': name,
    if (description.isNotEmpty) 'description': description,
  };

  @override
  bool operator ==(Object other) =>
      other is CollaborationMode &&
      other.id == id &&
      other.name == name &&
      other.description == description;

  @override
  int get hashCode => Object.hash(id, name, description);
}

/// One choice in a select-kind [ConfigOption].
class ConfigOptionValue {
  const ConfigOptionValue({required this.id, required this.name});

  final String id;
  final String name;

  factory ConfigOptionValue.fromJson(Map<String, dynamic> json) {
    return ConfigOptionValue(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
    );
  }

  @override
  bool operator ==(Object other) =>
      other is ConfigOptionValue && other.id == id && other.name == name;

  @override
  int get hashCode => Object.hash(id, name);
}

/// One agent-defined session config option (ACP SessionConfigOption), carried
/// on `session_config` events. [kind] is "select" or "boolean".
class ConfigOption {
  const ConfigOption({
    required this.id,
    required this.name,
    required this.kind,
    this.description = '',
    this.currentValue = '',
    this.boolValue = false,
    this.values = const [],
  });

  final String id;
  final String name;
  final String kind;
  final String description;

  /// Selected value id for a select option.
  final String currentValue;

  /// State for a boolean option.
  final bool boolValue;

  /// Choices for a select option.
  final List<ConfigOptionValue> values;

  bool get isBoolean => kind == 'boolean';

  factory ConfigOption.fromJson(Map<String, dynamic> json) {
    final vals = <ConfigOptionValue>[];
    final raw = json['values'];
    if (raw is List) {
      for (final v in raw) {
        if (v is Map) {
          vals.add(ConfigOptionValue.fromJson(Map<String, dynamic>.from(v)));
        }
      }
    }
    return ConfigOption(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      kind: json['kind'] as String? ?? 'select',
      description: json['description'] as String? ?? '',
      currentValue: json['current_value'] as String? ?? '',
      boolValue: json['bool_value'] == true,
      values: vals,
    );
  }

  // See [SessionMode] — value equality lets `session_config` no-op cleanly.
  @override
  bool operator ==(Object other) =>
      other is ConfigOption &&
      other.id == id &&
      other.name == name &&
      other.kind == kind &&
      other.description == description &&
      other.currentValue == currentValue &&
      other.boolValue == boolValue &&
      _listEquals(other.values, values);

  @override
  int get hashCode => Object.hash(
    id,
    name,
    kind,
    description,
    currentValue,
    boolValue,
    Object.hashAll(values),
  );
}

bool _listEquals<T>(List<T> a, List<T> b) {
  if (identical(a, b)) return true;
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

class SessionEvent {
  SessionEvent({
    required this.type,
    required this.sessionId,
    this.timestamp,
    this.status,
    this.title,
    this.text,
    this.toolId,
    this.toolName,
    this.toolKind,
    this.error,
    this.errorKind,
    this.retryAt,
    this.permissionId,
    this.options = const [],
    this.questionId,
    this.questions = const [],
    this.commands = const [],
    this.remoteCommands = const [],
    this.plan = const [],
    this.subagents = const [],
    this.approvalGroupId,
    this.approvals = const [],
    this.agentSessionId,
    this.stopReason,
    this.usage,
    this.capabilities,
    this.modes = const [],
    this.currentModeId,
    this.collaborationModes = const [],
    this.currentCollaborationModeId,
    this.configOptions = const [],
    this.attachments = const [],
    this.seq = 0,
    this.replay = false,
    this.timedOut = false,
  });

  final String type;
  final String sessionId;
  final DateTime? timestamp;
  final String? status;
  final String? title;
  final String? text;
  final String? toolId;
  final String? toolName;

  /// ACP tool-kind classification (`execute`, `edit`, `read`, `search`, …).
  /// Used to group actions in the transcript; null when the agent omitted it.
  final String? toolKind;
  final String? error;

  /// Daemon classification of an error event: `quota` (hard usage/credit
  /// limit) or `rate_limit` (transient throttling). Null for generic errors.
  final String? errorKind;

  /// When the quota/rate limit is expected to reset, if the provider's
  /// message said so.
  final DateTime? retryAt;
  final String? permissionId;
  final List<PermissionOption> options;

  /// Engine request id on `question_request` / `question_resolved`.
  final String? questionId;

  /// Multi-question form body on `question_request`.
  final List<QuestionItem> questions;
  final List<AvailableCommand> commands;

  /// Canonical daemon commands on `remote_commands` events; empty otherwise.
  final List<RemoteCommand> remoteCommands;

  /// Full current plan, carried by the `plan` event (replace-semantics).
  final List<PlanEntry> plan;

  /// Full current sub-agent set, carried by the `subagents` event
  /// (replace-semantics — an event with none means *clear*, as with `plan`).
  final List<SubagentInfo> subagents;

  /// Stable upsert key on `approval_summary` events. Events sharing it replace
  /// one another — the client must NOT append, or it renders the same
  /// approvals again on every event (MADR 0051 Part I).
  final String? approvalGroupId;

  /// Every permission auto-approved so far this turn, on `approval_summary`
  /// events. Replace-semantics: each event carries the full list.
  final List<ApprovalItem> approvals;
  final String? agentSessionId;
  final String? stopReason;

  /// Token/context report on `usage_update` events; null on all others.
  final Usage? usage;

  /// Agent capabilities on `session_capabilities` events; null otherwise.
  final SessionCapabilities? capabilities;

  /// Available modes on `session_mode` events (empty on a current-only update).
  final List<SessionMode> modes;

  /// Active mode id on `session_mode` events; null otherwise.
  final String? currentModeId;

  /// Collaboration catalog on `collaboration_mode` events (empty on
  /// a current-only update).
  final List<CollaborationMode> collaborationModes;

  /// Active collaboration-mode id on `collaboration_mode` events.
  final String? currentCollaborationModeId;

  /// Config options on `session_config` events; empty otherwise.
  final List<ConfigOption> configOptions;

  /// Non-text attachment descriptors on `user_message` events; empty otherwise.
  final List<AttachmentInfo> attachments;

  /// Per-session monotonic sequence stamped by the daemon (0 = unstamped).
  /// Usable to dedupe the live-broadcast/history-replay overlap on reconnect.
  final int seq;

  /// True when the agent re-emitted this event while loading an existing
  /// conversation (session resume). Such events rebuild history; they must
  /// never be appended to a transcript that already has content.
  final bool replay;

  /// True when a permission resolution was caused by the daemon timeout.
  final bool timedOut;

  /// This event with [text] replaced, every other field carried over.
  ///
  /// Used to fold a run of adjacent streaming chunks into one apply
  /// (TranscriptsNotifier), which is what keeps the reducer's per-chunk string
  /// copy from scaling with the number of chunks.
  SessionEvent withText(String text) => SessionEvent(
    type: type,
    sessionId: sessionId,
    timestamp: timestamp,
    status: status,
    title: title,
    text: text,
    toolId: toolId,
    toolName: toolName,
    toolKind: toolKind,
    error: error,
    errorKind: errorKind,
    retryAt: retryAt,
    permissionId: permissionId,
    options: options,
    questionId: questionId,
    questions: questions,
    commands: commands,
    remoteCommands: remoteCommands,
    plan: plan,
    subagents: subagents,
    approvalGroupId: approvalGroupId,
    approvals: approvals,
    agentSessionId: agentSessionId,
    stopReason: stopReason,
    usage: usage,
    capabilities: capabilities,
    modes: modes,
    currentModeId: currentModeId,
    collaborationModes: collaborationModes,
    currentCollaborationModeId: currentCollaborationModeId,
    configOptions: configOptions,
    attachments: attachments,
    seq: seq,
    replay: replay,
  );

  factory SessionEvent.fromJson(Map<String, dynamic> json) {
    DateTime? ts;
    final t = json['timestamp'];
    if (t is String) ts = DateTime.tryParse(t);
    DateTime? retryAt;
    final r = json['retry_at'];
    if (r is String) retryAt = DateTime.tryParse(r);

    final opts = <PermissionOption>[];
    final rawOpts = json['options'];
    if (rawOpts is List) {
      for (final o in rawOpts) {
        if (o is Map<String, dynamic>) {
          opts.add(PermissionOption.fromJson(o));
        } else if (o is Map) {
          opts.add(PermissionOption.fromJson(Map<String, dynamic>.from(o)));
        }
      }
    }

    final cmds = <AvailableCommand>[];
    final rawCmds = json['commands'];
    if (rawCmds is List) {
      for (final c in rawCmds) {
        if (c is Map<String, dynamic>) {
          cmds.add(AvailableCommand.fromJson(c));
        } else if (c is Map) {
          cmds.add(AvailableCommand.fromJson(Map<String, dynamic>.from(c)));
        }
      }
    }

    // `plan` events carry the full current plan under `entries`, mirroring how
    // `available_commands` carries `commands`.
    final plan = <PlanEntry>[];
    final rawPlan = json['entries'];
    if (rawPlan is List) {
      for (final e in rawPlan) {
        if (e is Map<String, dynamic>) {
          plan.add(PlanEntry.fromJson(e));
        } else if (e is Map) {
          plan.add(PlanEntry.fromJson(Map<String, dynamic>.from(e)));
        }
      }
    }

    final questions = <QuestionItem>[];
    final rawQuestions = json['questions'];
    if (rawQuestions is List) {
      for (final q in rawQuestions) {
        if (q is Map<String, dynamic>) {
          questions.add(QuestionItem.fromJson(q));
        } else if (q is Map) {
          questions.add(QuestionItem.fromJson(Map<String, dynamic>.from(q)));
        }
      }
    }

    return SessionEvent(
      type: json['type'] as String? ?? '',
      sessionId: json['session_id'] as String? ?? '',
      timestamp: ts,
      status: json['status'] as String?,
      title: json['title'] as String?,
      text: json['text'] as String?,
      toolId: json['tool_id'] as String?,
      toolName: json['tool_name'] as String?,
      toolKind: json['tool_kind'] as String?,
      error: json['error'] as String?,
      errorKind: json['error_kind'] as String?,
      retryAt: retryAt,
      permissionId: json['permission_id'] as String?,
      options: opts,
      questionId: json['question_id'] as String?,
      questions: questions,
      commands: cmds,
      remoteCommands: _mapList(json['remote_commands'], RemoteCommand.fromJson),
      plan: plan,
      subagents: _mapList(json['subagents'], SubagentInfo.fromJson),
      approvalGroupId: json['approval_group_id'] as String?,
      approvals: _mapList(json['approvals'], ApprovalItem.fromJson),
      agentSessionId: json['agent_session_id'] as String?,
      stopReason: json['stop_reason'] as String?,
      usage: switch (json['usage']) {
        final Map<String, dynamic> u => Usage.fromJson(u),
        final Map u => Usage.fromJson(Map<String, dynamic>.from(u)),
        _ => null,
      },
      capabilities: switch (json['capabilities']) {
        final Map<String, dynamic> c => SessionCapabilities.fromJson(c),
        final Map c => SessionCapabilities.fromJson(
          Map<String, dynamic>.from(c),
        ),
        _ => null,
      },
      modes: _mapList(json['modes'], SessionMode.fromJson),
      currentModeId: json['current_mode_id'] as String?,
      collaborationModes: _mapList(
        json['collaboration_modes'],
        CollaborationMode.fromJson,
      ),
      currentCollaborationModeId:
          json['current_collaboration_mode_id'] as String?,
      configOptions: _mapList(json['config_options'], ConfigOption.fromJson),
      attachments: _mapList(json['attachments'], AttachmentInfo.fromJson),
      seq: (json['seq'] as num?)?.toInt() ?? 0,
      replay: json['replay'] == true,
      timedOut: json['timed_out'] == true,
    );
  }
}

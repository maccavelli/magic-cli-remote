/// mcremote.v1 protocol models.
library;

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
    return Envelope(
      v: (json['v'] as num?)?.toInt() ?? 1,
      type: json['type'] as String? ?? '',
      id: json['id'] as String?,
      payload: payload,
      token: json['token'] as String?,
    );
  }
}

class SessionMeta {
  SessionMeta({
    required this.id,
    required this.provider,
    this.name = '',
    this.model = '',
    this.cwd,
    this.agentSessionId,
    this.ownerDeviceId,
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
  final String? cwd;
  final String? agentSessionId;

  /// Owning paired device id when the host includes it (debug / multi-device).
  /// Optional on the wire; ignored by UI unless we surface it later.
  final String? ownerDeviceId;
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
      cwd: json['cwd'] as String?,
      agentSessionId: json['agent_session_id'] as String?,
      ownerDeviceId: json['owner_device_id'] as String?,
      createdAt: created,
      status: json['status'] as String? ?? 'idle',
      live: json['live'] as bool? ?? true,
    );
  }

  SessionMeta copyWith({String? status, bool? live, String? name}) {
    return SessionMeta(
      id: id,
      provider: provider,
      name: name ?? this.name,
      model: model,
      cwd: cwd,
      agentSessionId: agentSessionId,
      ownerDeviceId: ownerDeviceId,
      createdAt: createdAt,
      status: status ?? this.status,
      live: live ?? this.live,
    );
  }
}

class ProviderInfo {
  ProviderInfo({required this.id, required this.ready});

  final String id;
  final bool ready;

  factory ProviderInfo.fromJson(Map<String, dynamic> json) {
    return ProviderInfo(
      id: json['id'] as String? ?? '',
      ready: json['ready'] as bool? ?? false,
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

class SessionEvent {
  SessionEvent({
    required this.type,
    required this.sessionId,
    this.timestamp,
    this.status,
    this.text,
    this.toolId,
    this.toolName,
    this.toolKind,
    this.error,
    this.errorKind,
    this.retryAt,
    this.permissionId,
    this.options = const [],
    this.commands = const [],
    this.plan = const [],
    this.agentSessionId,
    this.stopReason,
    this.usage,
    this.seq = 0,
    this.replay = false,
  });

  final String type;
  final String sessionId;
  final DateTime? timestamp;
  final String? status;
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
  final List<AvailableCommand> commands;

  /// Full current plan, carried by the `plan` event (replace-semantics).
  final List<PlanEntry> plan;
  final String? agentSessionId;
  final String? stopReason;

  /// Token/context report on `usage_update` events; null on all others.
  final Usage? usage;

  /// Per-session monotonic sequence stamped by the daemon (0 = unstamped).
  /// Usable to dedupe the live-broadcast/history-replay overlap on reconnect.
  final int seq;

  /// True when the agent re-emitted this event while loading an existing
  /// conversation (session resume). Such events rebuild history; they must
  /// never be appended to a transcript that already has content.
  final bool replay;

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

    return SessionEvent(
      type: json['type'] as String? ?? '',
      sessionId: json['session_id'] as String? ?? '',
      timestamp: ts,
      status: json['status'] as String?,
      text: json['text'] as String?,
      toolId: json['tool_id'] as String?,
      toolName: json['tool_name'] as String?,
      toolKind: json['tool_kind'] as String?,
      error: json['error'] as String?,
      errorKind: json['error_kind'] as String?,
      retryAt: retryAt,
      permissionId: json['permission_id'] as String?,
      options: opts,
      commands: cmds,
      plan: plan,
      agentSessionId: json['agent_session_id'] as String?,
      stopReason: json['stop_reason'] as String?,
      usage: switch (json['usage']) {
        final Map<String, dynamic> u => Usage.fromJson(u),
        final Map u => Usage.fromJson(Map<String, dynamic>.from(u)),
        _ => null,
      },
      seq: (json['seq'] as num?)?.toInt() ?? 0,
      replay: json['replay'] == true,
    );
  }
}

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
  });

  final String id;
  final String name;
  final String description;

  factory SessionMode.fromJson(Map<String, dynamic> json) {
    return SessionMode(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      description: json['description'] as String? ?? '',
    );
  }
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
    this.questionId,
    this.questions = const [],
    this.commands = const [],
    this.remoteCommands = const [],
    this.plan = const [],
    this.agentSessionId,
    this.stopReason,
    this.usage,
    this.capabilities,
    this.modes = const [],
    this.currentModeId,
    this.configOptions = const [],
    this.attachments = const [],
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

  /// Engine request id on `question_request` / `question_resolved`.
  final String? questionId;

  /// Multi-question form body on `question_request`.
  final List<QuestionItem> questions;
  final List<AvailableCommand> commands;

  /// Canonical daemon commands on `remote_commands` events; empty otherwise.
  final List<RemoteCommand> remoteCommands;

  /// Full current plan, carried by the `plan` event (replace-semantics).
  final List<PlanEntry> plan;
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
    agentSessionId: agentSessionId,
    stopReason: stopReason,
    usage: usage,
    capabilities: capabilities,
    modes: modes,
    currentModeId: currentModeId,
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
      configOptions: _mapList(json['config_options'], ConfigOption.fromJson),
      attachments: _mapList(json['attachments'], AttachmentInfo.fromJson),
      seq: (json['seq'] as num?)?.toInt() ?? 0,
      replay: json['replay'] == true,
    );
  }
}

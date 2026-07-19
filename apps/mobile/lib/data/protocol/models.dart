/// mcremote.v1 protocol models.

class Envelope {
  Envelope({
    this.v = 1,
    required this.type,
    this.id,
    this.payload,
    this.token,
  });

  final int v;
  final String type;
  final String? id;
  final Map<String, dynamic>? payload;
  final String? token;

  Map<String, dynamic> toJson() {
    final m = <String, dynamic>{
      'v': v,
      'type': type,
    };
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
    this.cwd,
    this.agentSessionId,
    this.createdAt,
    this.status = 'idle',
    this.live = true,
  });

  final String id;
  final String provider;
  final String name;
  final String? cwd;
  final String? agentSessionId;
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
      cwd: json['cwd'] as String?,
      agentSessionId: json['agent_session_id'] as String?,
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
      cwd: cwd,
      agentSessionId: agentSessionId,
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
  PermissionOption({
    required this.optionId,
    required this.name,
    this.kind,
  });

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

class SessionEvent {
  SessionEvent({
    required this.type,
    required this.sessionId,
    this.timestamp,
    this.status,
    this.text,
    this.toolId,
    this.toolName,
    this.error,
    this.permissionId,
    this.options = const [],
    this.agentSessionId,
    this.stopReason,
  });

  final String type;
  final String sessionId;
  final DateTime? timestamp;
  final String? status;
  final String? text;
  final String? toolId;
  final String? toolName;
  final String? error;
  final String? permissionId;
  final List<PermissionOption> options;
  final String? agentSessionId;
  final String? stopReason;

  factory SessionEvent.fromJson(Map<String, dynamic> json) {
    DateTime? ts;
    final t = json['timestamp'];
    if (t is String) ts = DateTime.tryParse(t);

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

    return SessionEvent(
      type: json['type'] as String? ?? '',
      sessionId: json['session_id'] as String? ?? '',
      timestamp: ts,
      status: json['status'] as String?,
      text: json['text'] as String?,
      toolId: json['tool_id'] as String?,
      toolName: json['tool_name'] as String?,
      error: json['error'] as String?,
      permissionId: json['permission_id'] as String?,
      options: opts,
      agentSessionId: json['agent_session_id'] as String?,
      stopReason: json['stop_reason'] as String?,
    );
  }
}

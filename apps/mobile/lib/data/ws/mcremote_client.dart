import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;
import 'package:uuid/uuid.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import '../local/settings_store.dart';
import '../protocol/models.dart';

enum McConnectionState {
  disconnected,
  connecting,
  authenticating,
  connected,
  error,
}

/// WebSocket client for mcremote.v1.
class McremoteClient {
  McremoteClient();

  final _uuid = const Uuid();
  WebSocketChannel? _channel;
  StreamSubscription? _sub;
  Timer? _pingTimer;

  final _events = StreamController<SessionEvent>.broadcast();
  final _connection = StreamController<McConnectionState>.broadcast();
  final _pending = <String, Completer<Envelope>>{};

  McConnectionState _state = McConnectionState.disconnected;
  String? lastError;
  String? deviceId;
  String? deviceName;
  String? wsUrl;

  Stream<SessionEvent> get events => _events.stream;
  Stream<McConnectionState> get connectionStates => _connection.stream;
  McConnectionState get state => _state;

  void _setState(McConnectionState s) {
    _state = s;
    if (!_connection.isClosed) _connection.add(s);
  }

  Future<String> healthz(String hostInput) async {
    final String url;
    try {
      url = SettingsStore.healthzUrl(hostInput);
    } catch (e) {
      throw Exception('invalid host for healthz: $e');
    }
    try {
      final res = await http
          .get(Uri.parse(url))
          .timeout(const Duration(seconds: 8));
      if (res.statusCode != 200) {
        throw Exception('healthz HTTP ${res.statusCode} for $url');
      }
      return res.body;
    } catch (e) {
      // Already includes URL (HTTP status path).
      if (e is Exception && e.toString().contains(url)) rethrow;
      // Surface the exact URL so port/host mis-parses are obvious in the UI.
      throw Exception('healthz $url → $e');
    }
  }

  Future<void> connect({
    required String hostInput,
    required String token,
  }) async {
    await disconnect();
    lastError = null;
    wsUrl = SettingsStore.normalizeWsUrl(hostInput);
    _setState(McConnectionState.connecting);

    try {
      final uri = Uri.parse(wsUrl!);
      _channel = WebSocketChannel.connect(uri);
      await _channel!.ready.timeout(const Duration(seconds: 8));
    } catch (e) {
      lastError = e.toString();
      _setState(McConnectionState.error);
      rethrow;
    }

    _sub = _channel!.stream.listen(
      _onMessage,
      onError: (Object e) {
        lastError = e.toString();
        _setState(McConnectionState.error);
      },
      onDone: () {
        _setState(McConnectionState.disconnected);
        _failAllPending('connection closed');
      },
      cancelOnError: false,
    );

    _setState(McConnectionState.authenticating);
    final auth = await request(
      'auth',
      payload: {'token': token},
      token: token,
    );
    if (auth.type == 'auth_error') {
      final msg = auth.payload?['message'] as String? ?? 'auth failed';
      lastError = msg;
      _setState(McConnectionState.error);
      await disconnect();
      throw Exception(msg);
    }
    if (auth.type != 'auth_ok') {
      throw Exception('unexpected auth response: ${auth.type}');
    }
    deviceId = auth.payload?['device_id'] as String?;
    deviceName = auth.payload?['device_name'] as String?;
    _setState(McConnectionState.connected);
    _pingTimer?.cancel();
    _pingTimer = Timer.periodic(const Duration(seconds: 30), (_) {
      if (_state == McConnectionState.connected) {
        unawaited(request('ping').catchError((_) => Envelope(type: 'error')));
      }
    });
  }

  Future<void> disconnect() async {
    _pingTimer?.cancel();
    _pingTimer = null;
    await _sub?.cancel();
    _sub = null;
    await _channel?.sink.close();
    _channel = null;
    _failAllPending('disconnected');
    _setState(McConnectionState.disconnected);
  }

  void _failAllPending(String reason) {
    for (final c in _pending.values) {
      if (!c.isCompleted) c.completeError(Exception(reason));
    }
    _pending.clear();
  }

  void _onMessage(dynamic data) {
    try {
      final map = jsonDecode(data as String) as Map<String, dynamic>;
      final env = Envelope.fromJson(map);

      if (env.type == 'event') {
        final raw = env.payload?['event'];
        Map<String, dynamic>? evMap;
        if (raw is Map<String, dynamic>) {
          evMap = raw;
        } else if (raw is Map) {
          evMap = Map<String, dynamic>.from(raw);
        } else if (env.payload != null && env.payload!.containsKey('type')) {
          // Some servers may send event fields flat — tolerate payload-as-event.
          evMap = env.payload;
        }
        if (evMap != null) {
          _events.add(SessionEvent.fromJson(evMap));
        }
        return;
      }

      final id = env.id;
      if (id != null && _pending.containsKey(id)) {
        _pending.remove(id)!.complete(env);
        return;
      }

      // Unsolicited error
      if (env.type == 'error') {
        lastError = env.payload?['message'] as String? ?? 'error';
      }
    } catch (e) {
      lastError = 'parse error: $e';
    }
  }

  Future<Envelope> request(
    String type, {
    Map<String, dynamic>? payload,
    String? token,
    Duration timeout = const Duration(seconds: 30),
  }) async {
    final ch = _channel;
    if (ch == null) throw StateError('not connected');
    final id = _uuid.v4();
    final completer = Completer<Envelope>();
    _pending[id] = completer;
    final env = Envelope(type: type, id: id, payload: payload, token: token);
    ch.sink.add(jsonEncode(env.toJson()));
    try {
      return await completer.future.timeout(timeout);
    } on TimeoutException {
      _pending.remove(id);
      throw TimeoutException('request $type timed out');
    }
  }

  Future<List<SessionMeta>> listSessions() async {
    final res = await request('session.list', payload: {});
    if (res.type == 'error') {
      throw Exception(res.payload?['message'] ?? 'list failed');
    }
    final list = res.payload?['sessions'];
    if (list is! List) return [];
    return list.map((e) {
      if (e is Map<String, dynamic>) return SessionMeta.fromJson(e);
      return SessionMeta.fromJson(Map<String, dynamic>.from(e as Map));
    }).toList();
  }

  Future<List<ProviderInfo>> listProviders() async {
    final res = await request('providers.list', payload: {});
    if (res.type == 'error') {
      throw Exception(res.payload?['message'] ?? 'providers failed');
    }
    final list = res.payload?['providers'];
    if (list is! List) return [];
    return list.map((e) {
      if (e is Map<String, dynamic>) return ProviderInfo.fromJson(e);
      return ProviderInfo.fromJson(Map<String, dynamic>.from(e as Map));
    }).toList();
  }

  /// Pick grok if ready, else fake, else first ready, else empty.
  Future<String> preferredProvider() async {
    final list = await listProviders();
    for (final p in list) {
      if (p.id == 'grok' && p.ready) return 'grok';
    }
    for (final p in list) {
      if (p.id == 'fake' && p.ready) return 'fake';
    }
    for (final p in list) {
      if (p.ready) return p.id;
    }
    return list.isNotEmpty ? list.first.id : 'fake';
  }

  Future<SessionMeta> createSession({
    String? provider,
    String? name,
    String? cwd,
  }) async {
    final res = await request('session.create', payload: {
      if (provider != null && provider.isNotEmpty) 'provider': provider,
      if (name != null && name.isNotEmpty) 'name': name,
      if (cwd != null && cwd.isNotEmpty) 'cwd': cwd,
    });
    if (res.type == 'error') {
      throw Exception(res.payload?['message'] ?? 'create failed');
    }
    // session.created payload is the meta object itself
    final p = res.payload ?? {};
    return SessionMeta.fromJson(p);
  }

  Future<void> prompt(String sessionId, String text) async {
    final res = await request('session.prompt', payload: {
      'session_id': sessionId,
      'text': text,
    });
    if (res.type == 'error') {
      throw Exception(res.payload?['message'] ?? 'prompt failed');
    }
  }

  Future<void> cancel(String sessionId) async {
    final res = await request('session.cancel', payload: {
      'session_id': sessionId,
    });
    if (res.type == 'error') {
      throw Exception(res.payload?['message'] ?? 'cancel failed');
    }
  }

  Future<void> closeSession(String sessionId) async {
    final res = await request('session.close', payload: {
      'session_id': sessionId,
    });
    if (res.type == 'error') {
      throw Exception(res.payload?['message'] ?? 'close failed');
    }
  }

  Future<void> respondPermission({
    required String sessionId,
    required String permissionId,
    String? optionId,
    bool cancelled = false,
  }) async {
    final res = await request('permission.respond', payload: {
      'session_id': sessionId,
      'permission_id': permissionId,
      if (optionId != null) 'option_id': optionId,
      if (cancelled) 'cancelled': true,
    });
    if (res.type == 'error') {
      throw Exception(res.payload?['message'] ?? 'permission failed');
    }
  }

  Future<void> dispose() async {
    await disconnect();
    await _events.close();
    await _connection.close();
  }
}

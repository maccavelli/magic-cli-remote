import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;

import 'package:http/http.dart' as http;
import 'package:uuid/uuid.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import '../local/settings_store.dart';
import '../protocol/models.dart';
import '../protocol/pair_uri.dart';
import 'mc_exception.dart';

enum McConnectionState {
  disconnected,
  connecting,
  authenticating,
  connected,
  reconnecting,
  error,
}

/// WebSocket client for mcremote.v1.
class McremoteClient {
  McremoteClient();

  final _uuid = const Uuid();
  WebSocketChannel? _channel;
  StreamSubscription? _sub;
  Timer? _pingTimer;
  Timer? _reconnectTimer;

  final _events = StreamController<SessionEvent>.broadcast();
  final _connection = StreamController<McConnectionState>.broadcast();
  final _pending = <String, Completer<Envelope>>{};

  McConnectionState _state = McConnectionState.disconnected;
  String? lastError;
  String? lastErrorCode;
  String? deviceId;
  String? deviceName;
  String? wsUrl;

  String? _lastHostInput;
  String? _lastToken;
  bool _autoReconnect = true;
  bool _manualDisconnect = false;
  bool _userLoggedOut = false;
  int _reconnectAttempt = 0;
  bool _reconnectInFlight = false;

  Stream<SessionEvent> get events => _events.stream;
  Stream<McConnectionState> get connectionStates => _connection.stream;
  McConnectionState get state => _state;
  String? get lastHostInput => _lastHostInput;
  bool get userLoggedOut => _userLoggedOut;

  /// Host + token present in memory and user has not explicitly logged out.
  bool get hasCredentials =>
      !_userLoggedOut &&
      (_lastHostInput?.isNotEmpty ?? false) &&
      (_lastToken?.isNotEmpty ?? false);

  void _setState(McConnectionState s) {
    _state = s;
    if (!_connection.isClosed) _connection.add(s);
  }

  /// Drop in-memory token (and optionally host). Does not touch secure storage.
  void clearMemoryCredentials({bool host = false}) {
    _lastToken = null;
    if (host) _lastHostInput = null;
    lastErrorCode = null;
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
      if (e is Exception && e.toString().contains(url)) rethrow;
      throw Exception('healthz $url → $e');
    }
  }

  Future<void> connect({
    required String hostInput,
    required String token,
    bool enableAutoReconnect = true,
  }) async {
    _manualDisconnect = false;
    _userLoggedOut = false;
    _autoReconnect = enableAutoReconnect;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    _reconnectAttempt = 0;
    lastErrorCode = null;

    await _connectInternal(hostInput: hostInput, token: token);
  }

  /// Claim an 8-char pair code, receive durable token, and stay connected.
  Future<String> claimPairCode({
    required String hostInput,
    required String code,
    String? deviceName,
  }) async {
    _manualDisconnect = false;
    _userLoggedOut = false;
    _autoReconnect = true;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    await _teardownSocket();

    lastError = null;
    lastErrorCode = null;
    _lastHostInput = hostInput.trim();
    wsUrl = SettingsStore.normalizeWsUrl(hostInput);
    _setState(McConnectionState.connecting);

    try {
      final uri = Uri.parse(wsUrl!);
      _channel = WebSocketChannel.connect(uri);
      await _channel!.ready.timeout(const Duration(seconds: 8));
    } catch (e) {
      lastError = e.toString();
      lastErrorCode = 'connect_failed';
      _setState(McConnectionState.error);
      await _teardownSocket();
      throw McException('connection failed: $e', code: 'connect_failed');
    }

    _sub = _channel!.stream.listen(
      _onMessage,
      onError: _onSocketError,
      onDone: _onSocketDone,
      cancelOnError: false,
    );

    _setState(McConnectionState.authenticating);
    try {
      final normalized = PairPayload.normalizePairCode(code);
      final res = await request(
        'pair.claim',
        payload: {
          'code': normalized,
          if (deviceName != null && deviceName.isNotEmpty) 'name': deviceName,
        },
        timeout: const Duration(seconds: 20),
      );

      final err = handshakeErrorFrom(
        'pair_ok',
        res.type,
        res.payload,
        isPair: true,
      );
      if (err != null) {
        await _failHandshake(err);
      }

      final token = res.payload?['token'] as String? ?? '';
      if (token.isEmpty) {
        await _failHandshake(
          McException(
            'pair_ok missing token',
            code: 'unexpected_pair_response',
            permanent: true,
          ),
        );
      }

      deviceId = res.payload?['device_id'] as String?;
      deviceName = res.payload?['device_name'] as String?;
      _lastToken = token;
      _reconnectAttempt = 0;
      lastError = null;
      lastErrorCode = null;
      _setState(McConnectionState.connected);
      _startPing();
      return token;
    } on McException {
      rethrow;
    } on TimeoutException catch (e) {
      await _failHandshake(
        McException(
          e.message ?? 'pair claim timed out',
          code: 'auth_timeout',
          permanent: false,
        ),
      );
    } catch (e) {
      await _failHandshake(
        McException(e.toString(), code: 'pair_failed', permanent: true),
      );
    }
  }

  Future<void> _connectInternal({
    required String hostInput,
    required String token,
  }) async {
    await _teardownSocket();
    lastError = null;
    // Do not clear lastErrorCode until success — callers may inspect prior codes.
    _lastHostInput = hostInput.trim();
    _lastToken = token;
    wsUrl = SettingsStore.normalizeWsUrl(hostInput);

    final isReconnect = _state == McConnectionState.reconnecting;
    if (!isReconnect) {
      _setState(McConnectionState.connecting);
    }

    try {
      final uri = Uri.parse(wsUrl!);
      _channel = WebSocketChannel.connect(uri);
      await _channel!.ready.timeout(const Duration(seconds: 8));
    } catch (e) {
      lastError = e.toString();
      _setState(McConnectionState.error);
      _scheduleReconnect();
      rethrow;
    }

    _sub = _channel!.stream.listen(
      _onMessage,
      onError: _onSocketError,
      onDone: _onSocketDone,
      cancelOnError: false,
    );

    _setState(McConnectionState.authenticating);
    try {
      final auth = await request(
        'auth',
        payload: {'token': token},
        token: token,
      );

      final err = handshakeErrorFrom(
        'auth_ok',
        auth.type,
        auth.payload,
        isPair: false,
      );
      if (err != null) {
        await _failHandshake(err);
      }

      deviceId = auth.payload?['device_id'] as String?;
      deviceName = auth.payload?['device_name'] as String?;
      _reconnectAttempt = 0;
      lastError = null;
      lastErrorCode = null;
      _setState(McConnectionState.connected);
      _startPing();
    } on McException {
      rethrow;
    } on TimeoutException catch (e) {
      await _failHandshake(
        McException(
          e.message ?? 'auth timed out',
          code: 'auth_timeout',
          permanent: false,
        ),
      );
    } catch (e) {
      // Network / unexpected during handshake: tear down, allow reconnect.
      lastError = e.toString();
      _setState(McConnectionState.error);
      await _teardownSocket();
      _scheduleReconnect();
      rethrow;
    }
  }

  /// Tear down the socket, set error state, optionally disable auto-reconnect.
  Future<Never> _failHandshake(McException err) async {
    lastError = err.message;
    lastErrorCode = err.code;
    _setState(McConnectionState.error);
    if (err.permanent) {
      _autoReconnect = false;
      // Permanent auth failures must not keep a bad token for resume reconnect.
      if (err.isInvalidToken || err.code == 'auth_failed') {
        _lastToken = null;
      }
    }
    await _teardownSocket();
    throw err;
  }

  void _startPing() {
    _pingTimer?.cancel();
    _pingTimer = Timer.periodic(const Duration(seconds: 30), (_) {
      if (_state == McConnectionState.connected) {
        unawaited(request('ping').catchError((_) => Envelope(type: 'error')));
      }
    });
  }

  void _onSocketError(Object e) {
    lastError = e.toString();
    if (_state != McConnectionState.reconnecting) {
      _setState(McConnectionState.error);
    }
    _scheduleReconnect();
  }

  void _onSocketDone() {
    _failAllPending('connection closed');
    if (_manualDisconnect || _userLoggedOut) {
      _setState(McConnectionState.disconnected);
      return;
    }
    if (_state == McConnectionState.connected ||
        _state == McConnectionState.authenticating ||
        _state == McConnectionState.error ||
        _state == McConnectionState.reconnecting) {
      _scheduleReconnect();
    } else {
      _setState(McConnectionState.disconnected);
    }
  }

  void _scheduleReconnect() {
    if (!_autoReconnect || _manualDisconnect || _userLoggedOut || !hasCredentials) {
      return;
    }
    if (_reconnectInFlight || (_reconnectTimer?.isActive ?? false)) {
      return;
    }
    final attempt = _reconnectAttempt;
    final delaySec = math.min(30, math.pow(2, math.min(attempt, 4)).toInt());
    // 1, 2, 4, 8, 16, 30…
    final delay = Duration(seconds: attempt == 0 ? 1 : delaySec);
    _setState(McConnectionState.reconnecting);
    _reconnectTimer = Timer(delay, () async {
      if (_manualDisconnect || _userLoggedOut || !hasCredentials) return;
      _reconnectInFlight = true;
      _reconnectAttempt++;
      try {
        await _connectInternal(
          hostInput: _lastHostInput!,
          token: _lastToken!,
        );
      } catch (_) {
        // _connectInternal already scheduled next attempt on failure paths
        // except permanent auth errors. If still not connected, schedule again.
        if (_state != McConnectionState.connected &&
            _autoReconnect &&
            !_manualDisconnect &&
            !_userLoggedOut) {
          _reconnectInFlight = false;
          _scheduleReconnect();
          return;
        }
      } finally {
        _reconnectInFlight = false;
      }
    });
  }

  /// Manual reconnect from UI or app resume.
  Future<void> reconnect() async {
    if (_userLoggedOut || !hasCredentials) {
      throw McException(
        'no saved credentials',
        code: 'no_credentials',
        permanent: true,
      );
    }
    _manualDisconnect = false;
    _userLoggedOut = false;
    _autoReconnect = true;
    _reconnectTimer?.cancel();
    _reconnectAttempt = 0;
    _setState(McConnectionState.reconnecting);
    await _connectInternal(
      hostInput: _lastHostInput!,
      token: _lastToken!,
    );
  }

  Future<void> disconnect({bool manual = true}) async {
    _manualDisconnect = manual;
    if (manual) {
      _autoReconnect = false;
      _userLoggedOut = true;
      // Keep host for the connect form; drop token so resume cannot revive session.
      _lastToken = null;
    }
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    await _teardownSocket();
    _setState(McConnectionState.disconnected);
  }

  Future<void> _teardownSocket() async {
    _pingTimer?.cancel();
    _pingTimer = null;
    await _sub?.cancel();
    _sub = null;
    try {
      await _channel?.sink.close();
    } catch (_) {}
    _channel = null;
    _failAllPending('disconnected');
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

      if (env.type == 'error') {
        lastError = env.payload?['message'] as String? ?? 'error';
        lastErrorCode = env.payload?['code'] as String?;
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
    _autoReconnect = false;
    _manualDisconnect = true;
    _userLoggedOut = true;
    _reconnectTimer?.cancel();
    await _teardownSocket();
    await _events.close();
    await _connection.close();
  }
}

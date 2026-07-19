import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Persists connection settings. Token is stored securely.
class SettingsStore {
  SettingsStore({
    FlutterSecureStorage? secure,
    SharedPreferences? prefs,
  })  : _secure = secure ?? const FlutterSecureStorage(),
        _prefs = prefs;

  static const _kHost = 'host';
  static const _kToken = 'device_token';
  static const _kDeviceId = 'device_id';

  final FlutterSecureStorage _secure;
  SharedPreferences? _prefs;

  Future<SharedPreferences> get _p async =>
      _prefs ??= await SharedPreferences.getInstance();

  Future<String?> getHost() async {
    final p = await _p;
    return p.getString(_kHost);
  }

  Future<void> setHost(String host) async {
    final p = await _p;
    await p.setString(_kHost, host);
  }

  Future<String?> getToken() => _secure.read(key: _kToken);

  Future<void> setToken(String token) =>
      _secure.write(key: _kToken, value: token);

  Future<void> clearToken() => _secure.delete(key: _kToken);

  Future<String?> getDeviceId() async {
    final p = await _p;
    return p.getString(_kDeviceId);
  }

  Future<void> setDeviceId(String id) async {
    final p = await _p;
    await p.setString(_kDeviceId, id);
  }

  Future<void> clearAll() async {
    final p = await _p;
    await p.remove(_kHost);
    await p.remove(_kDeviceId);
    await clearToken();
  }

  /// Normalize user input into a WebSocket URL.
  ///
  /// Accepts:
  /// - `ws://host:7531/v1/ws`
  /// - `host:7531`
  /// - `10.0.2.2:7531` (Android emulator → host loopback)
  /// - `host` (defaults port 7531)
  static String normalizeWsUrl(String input) {
    var s = input.trim();
    if (s.isEmpty) {
      throw ArgumentError('host is empty');
    }
    if (s.startsWith('ws://') || s.startsWith('wss://')) {
      if (!s.contains('/v1/ws')) {
        s = s.endsWith('/') ? '${s}v1/ws' : '$s/v1/ws';
      }
      return s;
    }
    // host or host:port
    if (!s.contains(':')) {
      s = '$s:7531';
    }
    return 'ws://$s/v1/ws';
  }

  static String httpBaseFromWs(String wsUrl) {
    final u = Uri.parse(wsUrl);
    final scheme = u.scheme == 'wss' ? 'https' : 'http';
    return Uri(
      scheme: scheme,
      host: u.host,
      port: u.hasPort ? u.port : null,
    ).toString();
  }
}

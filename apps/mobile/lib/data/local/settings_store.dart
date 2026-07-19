import 'dart:io' show Platform;

import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Persists connection settings.
///
/// Prefer [FlutterSecureStorage] when available. On Linux desktop (especially
/// headless/Xvfb) the system keyring is often locked (`KeyringLocked`); we fall
/// back to [SharedPreferences] so the app remains usable for development.
class SettingsStore {
  SettingsStore({
    FlutterSecureStorage? secure,
    SharedPreferences? prefs,
  })  : _secure = secure ?? const FlutterSecureStorage(),
        _prefs = prefs;

  static const _kHost = 'host';
  static const _kToken = 'device_token';
  static const _kDeviceId = 'device_id';
  static const _kTokenFallback = 'device_token_fallback';

  final FlutterSecureStorage _secure;
  SharedPreferences? _prefs;

  /// Once secure storage has failed once, skip it for the rest of the session.
  bool _secureDisabled = false;

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

  Future<String?> getToken() async {
    if (!_secureDisabled) {
      try {
        final t = await _secure.read(key: _kToken);
        if (t != null && t.isNotEmpty) return t;
      } catch (e) {
        _disableSecure(e);
      }
    }
    final p = await _p;
    return p.getString(_kTokenFallback);
  }

  Future<void> setToken(String token) async {
    if (!_secureDisabled) {
      try {
        await _secure.write(key: _kToken, value: token);
        // Clear any previous plaintext fallback.
        final p = await _p;
        await p.remove(_kTokenFallback);
        return;
      } catch (e) {
        _disableSecure(e);
      }
    }
    final p = await _p;
    await p.setString(_kTokenFallback, token);
  }

  Future<void> clearToken() async {
    if (!_secureDisabled) {
      try {
        await _secure.delete(key: _kToken);
      } catch (e) {
        _disableSecure(e);
      }
    }
    final p = await _p;
    await p.remove(_kTokenFallback);
  }

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

  void _disableSecure(Object e) {
    _secureDisabled = true;
    debugPrint(
      'SettingsStore: secure storage unavailable ($e); '
      'using SharedPreferences fallback'
      '${Platform.isLinux ? ' (unlock/login keyring for production)' : ''}.',
    );
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

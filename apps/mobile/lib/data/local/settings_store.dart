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

  /// Default mcremote listen port.
  static const int defaultPort = 7531;

  /// Parse free-form host input into scheme / host / port.
  ///
  /// Accepts:
  /// - `host:7531`
  /// - `10.0.2.2:7531` (Android emulator → host loopback)
  /// - `host` (defaults port [defaultPort])
  /// - `ws://host:7531/v1/ws`, `http://host:7531`, `https://…`, `wss://…`
  /// - `[ipv6]:7531`
  static ({bool secure, String host, int port}) parseEndpoint(String input) {
    var s = input.trim();
    if (s.isEmpty) {
      throw ArgumentError('host is empty');
    }

    var secure = false;
    final lower = s.toLowerCase();
    if (lower.startsWith('wss://') || lower.startsWith('https://')) {
      secure = true;
      s = s.substring(s.indexOf('://') + 3);
    } else if (lower.startsWith('ws://') || lower.startsWith('http://')) {
      s = s.substring(s.indexOf('://') + 3);
    }

    // Drop path / query / fragment (e.g. pasted `/v1/ws` or `/healthz`).
    final slash = s.indexOf('/');
    if (slash >= 0) {
      s = s.substring(0, slash);
    }
    final q = s.indexOf('?');
    if (q >= 0) {
      s = s.substring(0, q);
    }
    final hash = s.indexOf('#');
    if (hash >= 0) {
      s = s.substring(0, hash);
    }
    // Strip accidental userinfo.
    final at = s.lastIndexOf('@');
    if (at >= 0) {
      s = s.substring(at + 1);
    }
    s = s.trim();
    if (s.isEmpty) {
      throw ArgumentError('host is empty');
    }

    late final String host;
    late final int port;

    if (s.startsWith('[')) {
      // [IPv6] or [IPv6]:port
      final end = s.indexOf(']');
      if (end < 0) {
        throw ArgumentError('invalid IPv6 host (missing ])');
      }
      host = s.substring(1, end);
      final rest = s.substring(end + 1);
      if (rest.isEmpty) {
        port = defaultPort;
      } else if (rest.startsWith(':')) {
        port = _parsePort(rest.substring(1));
      } else {
        throw ArgumentError('invalid host after IPv6 address: $rest');
      }
    } else {
      // host, host:port, or bare IPv6 (must use brackets + port).
      final colon = s.lastIndexOf(':');
      if (colon > 0 && _isAllDigits(s.substring(colon + 1))) {
        // Only treat as host:port when the suffix is a plain port and the host
        // is not an unbracketed IPv6 (those contain multiple colons).
        final maybeHost = s.substring(0, colon);
        if (maybeHost.contains(':')) {
          throw ArgumentError(
            'unbracketed IPv6 is not supported; use [addr]:$defaultPort',
          );
        }
        host = maybeHost;
        port = _parsePort(s.substring(colon + 1));
      } else if (s.contains(':')) {
        throw ArgumentError(
          'unbracketed IPv6 is not supported; use [addr]:$defaultPort',
        );
      } else {
        host = s;
        port = defaultPort;
      }
    }

    if (host.isEmpty) {
      throw ArgumentError('host is empty');
    }
    return (secure: secure, host: host, port: port);
  }

  static bool _isAllDigits(String s) {
    if (s.isEmpty) return false;
    for (var i = 0; i < s.length; i++) {
      final c = s.codeUnitAt(i);
      if (c < 0x30 || c > 0x39) return false;
    }
    return true;
  }

  static int _parsePort(String s) {
    final p = int.tryParse(s);
    if (p == null || p < 1 || p > 65535) {
      throw ArgumentError('invalid port: $s');
    }
    return p;
  }

  /// Normalize user input into a WebSocket URL (`ws[s]://host:port/v1/ws`).
  static String normalizeWsUrl(String input) {
    final ep = parseEndpoint(input);
    final scheme = ep.secure ? 'wss' : 'ws';
    return Uri(
      scheme: scheme,
      host: ep.host,
      port: ep.port,
      path: '/v1/ws',
    ).toString();
  }

  /// HTTP(S) origin for `/healthz` and `/v1/hello` from a WS URL or host input.
  static String httpBaseFromWs(String wsUrl) {
    final ep = parseEndpoint(wsUrl);
    final scheme = ep.secure ? 'https' : 'http';
    return Uri(
      scheme: scheme,
      host: ep.host,
      port: ep.port,
    ).toString();
  }

  /// Absolute healthz URL for display and requests.
  static String healthzUrl(String hostInput) {
    final ep = parseEndpoint(hostInput);
    final scheme = ep.secure ? 'https' : 'http';
    return Uri(
      scheme: scheme,
      host: ep.host,
      port: ep.port,
      path: '/healthz',
    ).toString();
  }
}

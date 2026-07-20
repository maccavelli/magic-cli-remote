import 'dart:convert';
import 'dart:io' show Platform;

import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../protocol/pair_uri.dart' show normalizeFingerprint;

/// Thrown when secure storage is unavailable on a platform where the plaintext
/// [SharedPreferences] fallback is not permitted (Android / iOS).
class SecureStorageUnavailable implements Exception {
  const SecureStorageUnavailable(this.cause);

  final Object cause;

  @override
  String toString() =>
      'SecureStorageUnavailable: device keystore/keychain is unavailable '
      '($cause). Refusing to store the device token in cleartext.';
}

/// Persists connection settings.
///
/// Prefer [FlutterSecureStorage] when available. On *desktop* (especially
/// headless/Xvfb Linux) the system keyring is often locked (`KeyringLocked`);
/// we fall back to [SharedPreferences] there so the app remains usable for
/// development.
///
/// That fallback is a **cleartext** store and is therefore never used on
/// Android or iOS, where a real Keystore/Keychain is always present: on those
/// platforms a secure-storage failure surfaces as [SecureStorageUnavailable]
/// (writes) or a null token (reads), and any pre-existing fallback value left
/// behind by an older build is purged.
class SettingsStore {
  SettingsStore({
    FlutterSecureStorage? secure,
    SharedPreferences? prefs,
    @visibleForTesting bool? allowPlaintextFallback,
  })  : _secure = secure ?? const FlutterSecureStorage(),
        _allowPlaintextFallback =
            allowPlaintextFallback ?? _defaultAllowPlaintextFallback {
    // Assigned in the body rather than the initializer list: the field is
    // private and lazily (re)populated, so it cannot be an initializing formal.
    _prefs = prefs;
  }

  static const _kHost = 'host';
  static const _kToken = 'device_token';
  static const _kDeviceId = 'device_id';
  static const _kTokenFallback = 'device_token_fallback';
  static const _kPins = 'cert_pins';
  static const _kPinsFallback = 'cert_pins_fallback';

  // Legacy single-slot pin keys. Read once, migrated into [_kPins], removed.
  static const _kFingerprint = 'cert_fingerprint';
  static const _kFingerprintFallback = 'cert_fingerprint_fallback';
  static const _kFingerprintHost = 'cert_fingerprint_host';

  /// Upper bound on remembered pins, so churn (a host re-addressed repeatedly
  /// before its device id is known) cannot grow the record without limit.
  /// Insertion-ordered: the least recently written entry goes first.
  static const _maxPins = 32;

  final FlutterSecureStorage _secure;
  SharedPreferences? _prefs;

  /// Whether the cleartext [SharedPreferences] token fallback may be used.
  /// Desktop only — see the class doc.
  final bool _allowPlaintextFallback;

  /// Once secure storage has failed once, skip it for the rest of the session.
  bool _secureDisabled = false;

  /// The legacy single-slot pin is looked for once per session, not on every
  /// read.
  bool _legacyPinsChecked = false;

  static bool get _defaultAllowPlaintextFallback {
    if (kIsWeb) return false;
    return !(Platform.isAndroid || Platform.isIOS);
  }

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

  Future<String?> getToken() => _readSecret(_kToken, _kTokenFallback);

  Future<void> setToken(String token) =>
      _writeSecret(_kToken, _kTokenFallback, token);

  Future<void> clearToken() => _clearSecret(_kToken, _kTokenFallback);

  /// The pinned TLS certificate fingerprint for a daemon, or null.
  ///
  /// Pins are keyed on the **device id** the daemon issued at pair time, not on
  /// the address dialled: with hosts dialled by tailnet IP, a node re-registered
  /// in Headscale comes back on a new `100.x`, and an address-keyed pin would
  /// miss and demand a QR rescan for a certificate that never changed. The host
  /// authority is kept as a secondary record, used only before a device id is
  /// known (the first connect, prior to pair completing).
  ///
  /// A pin recorded for a *different* identity is never returned: it would
  /// guarantee a mismatch (at best) or vouch for the wrong daemon (at worst).
  ///
  /// [deviceId] defaults to the persisted one; pass it explicitly when the
  /// caller holds a fresher value than storage does.
  Future<String?> getFingerprint(String hostInput, {String? deviceId}) async {
    final id = _idOrNull(deviceId) ?? await getDeviceId();
    final authority = _authorityOf(hostInput);
    final pins = await _readPins(id);

    if (id != null) {
      final byId = _fpOf(pins['id:$id']);
      if (byId != null) return byId;
    }
    // No identity-keyed pin. Fall back to the secondary authority record, but
    // never to one another identity owns.
    for (final rec in pins.values) {
      if (rec is! Map) continue;
      if (rec['authority'] != authority) continue;
      final owner = _idOrNull(rec['device_id'] as String?);
      if (id != null && owner != null && owner != id) continue;
      final fp = _fpOf(rec);
      if (fp != null) return fp;
    }
    return null;
  }

  /// Pins [fingerprint] for the daemon reached at [hostInput]. Throws
  /// [ArgumentError] if it is not a SHA-256 digest — an unusable pin must never
  /// be persisted as if it were.
  Future<void> setFingerprint(
    String hostInput,
    String fingerprint, {
    String? deviceId,
  }) async {
    final canonical = normalizeFingerprint(fingerprint);
    if (canonical == null) {
      throw ArgumentError('not a SHA-256 certificate fingerprint: $fingerprint');
    }
    final id = _idOrNull(deviceId) ?? await getDeviceId();
    final authority = _authorityOf(hostInput);
    final pins = await _readPins(id);

    if (id != null) {
      // The identity is known now, so any address-keyed record for the same
      // daemon is superseded rather than left to rot as a second answer.
      pins.remove('host:$authority');
    }
    pins.remove(_pinKey(id, authority)); // re-insert last for LRU eviction
    pins[_pinKey(id, authority)] = <String, String>{
      'fp': canonical,
      'authority': authority,
      'device_id': ?id,
    };
    while (pins.length > _maxPins) {
      pins.remove(pins.keys.first);
    }
    await _writePins(pins);
  }

  /// Forgets every pin. Used on sign-out; a pin is only ever *replaced*
  /// otherwise, never silently dropped.
  Future<void> clearFingerprint() async {
    await _clearSecret(_kPins, _kPinsFallback);
    await _clearSecret(_kFingerprint, _kFingerprintFallback);
    final p = await _p;
    await p.remove(_kFingerprintHost);
    _legacyPinsChecked = true;
  }

  static String _pinKey(String? deviceId, String authority) =>
      deviceId != null ? 'id:$deviceId' : 'host:$authority';

  static String? _idOrNull(String? id) =>
      (id == null || id.isEmpty) ? null : id;

  static String? _fpOf(Object? rec) {
    if (rec is! Map) return null;
    final fp = rec['fp'];
    if (fp is! String || fp.isEmpty) return null;
    return normalizeFingerprint(fp);
  }

  /// The identity → pin record map, migrating the legacy single-slot keys the
  /// first time it is read.
  Future<Map<String, dynamic>> _readPins(String? deviceId) async {
    final raw = await _readSecret(_kPins, _kPinsFallback);
    Map<String, dynamic> pins = {};
    if (raw != null && raw.isNotEmpty) {
      try {
        final decoded = jsonDecode(raw);
        if (decoded is Map<String, dynamic>) pins = decoded;
      } catch (e) {
        debugPrint('SettingsStore: discarding unreadable pin store ($e).');
      }
    }
    if (pins.isEmpty) {
      pins.addAll(await _migrateLegacyPin(deviceId));
    }
    return pins;
  }

  Future<void> _writePins(Map<String, dynamic> pins) =>
      _writeSecret(_kPins, _kPinsFallback, jsonEncode(pins));

  /// Folds a pin written by an older build into the map, so a currently paired
  /// device is not forced to re-pair by the format change.
  Future<Map<String, dynamic>> _migrateLegacyPin(String? deviceId) async {
    if (_legacyPinsChecked) return const {};
    _legacyPinsChecked = true;

    final legacy = await _readSecret(_kFingerprint, _kFingerprintFallback);
    final canonical =
        (legacy == null || legacy.isEmpty) ? null : normalizeFingerprint(legacy);
    final p = await _p;
    if (canonical == null) {
      await _clearLegacyPin();
      return const {};
    }
    final authority = p.getString(_kFingerprintHost) ?? '';
    final pins = <String, dynamic>{
      _pinKey(deviceId, authority): <String, String>{
        'fp': canonical,
        'authority': authority,
        'device_id': ?deviceId,
      },
    };
    try {
      await _writePins(pins);
      await _clearLegacyPin();
    } catch (e) {
      // The pin still applies to this session; the legacy keys stay put so a
      // later run can try again rather than losing it to a locked keystore.
      debugPrint('SettingsStore: could not migrate the legacy cert pin ($e).');
    }
    return pins;
  }

  Future<void> _clearLegacyPin() async {
    await _clearSecret(_kFingerprint, _kFingerprintFallback);
    final p = await _p;
    await p.remove(_kFingerprintHost);
  }

  static String _authorityOf(String hostInput) {
    try {
      final ep = parseEndpoint(hostInput);
      return '${ep.host}:${ep.port}';
    } catch (_) {
      return hostInput.trim();
    }
  }

  Future<String?> _readSecret(String key, String fallbackKey) async {
    if (!_secureDisabled) {
      try {
        final v = await _secure.read(key: key);
        if (v != null && v.isNotEmpty) {
          await _purgePlaintextFallback(fallbackKey);
          return v;
        }
      } catch (e) {
        _disableSecure(e);
      }
    }
    if (!_allowPlaintextFallback) {
      // Mobile: never read a cleartext secret. Purge anything an older build
      // left behind and fail closed — the user re-pairs.
      await _purgePlaintextFallback(fallbackKey);
      return null;
    }
    final p = await _p;
    return p.getString(fallbackKey);
  }

  Future<void> _writeSecret(
    String key,
    String fallbackKey,
    String value,
  ) async {
    Object? failure;
    if (!_secureDisabled) {
      try {
        await _secure.write(key: key, value: value);
        // Clear any previous plaintext fallback.
        final p = await _p;
        await p.remove(fallbackKey);
        return;
      } catch (e) {
        failure = e;
        _disableSecure(e);
      }
    }
    if (!_allowPlaintextFallback) {
      // Mobile: degrading to cleartext is not an acceptable outcome.
      await _purgePlaintextFallback(fallbackKey);
      throw SecureStorageUnavailable(failure ?? 'secure storage disabled');
    }
    final p = await _p;
    await p.setString(fallbackKey, value);
  }

  Future<void> _clearSecret(String key, String fallbackKey) async {
    if (!_secureDisabled) {
      try {
        await _secure.delete(key: key);
      } catch (e) {
        _disableSecure(e);
      }
    }
    final p = await _p;
    await p.remove(fallbackKey);
  }

  /// Removes any cleartext value written by an older build. No-op where the
  /// fallback is a supported (desktop) code path.
  Future<void> _purgePlaintextFallback(String fallbackKey) async {
    if (_allowPlaintextFallback) return;
    final p = await _p;
    if (p.getString(fallbackKey) != null) {
      await p.remove(fallbackKey);
    }
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
    await clearFingerprint();
  }

  void _disableSecure(Object e) {
    _secureDisabled = true;
    if (!_allowPlaintextFallback) {
      debugPrint(
        'SettingsStore: secure storage unavailable ($e); '
        'no cleartext fallback on this platform.',
      );
      return;
    }
    debugPrint(
      'SettingsStore: secure storage unavailable ($e); '
      'using SharedPreferences fallback'
      '${!kIsWeb && Platform.isLinux ? ' (unlock/login keyring for production)' : ''}.',
    );
  }

  /// Default mcremote listen port.
  static const int defaultPort = 7531;

  /// Extracts the pinned certificate fingerprint carried in a host input as a
  /// `#fp=…` fragment, or null when there is none.
  ///
  /// The fragment is how a fingerprint scanned from a pair QR reaches the
  /// client: it rides inside the single host string that the connect flow
  /// already persists and replays. [parseEndpoint] strips it back off, so it
  /// never reaches a URL.
  static String? fingerprintFrom(String input) {
    final hash = input.indexOf('#');
    if (hash < 0) return null;
    final frag = input.substring(hash + 1).trim();
    const prefix = 'fp=';
    if (!frag.toLowerCase().startsWith(prefix)) return null;
    return normalizeFingerprint(frag.substring(prefix.length));
  }

  /// The host input without its `#fp=…` fragment, for display.
  static String stripFingerprint(String input) {
    final hash = input.indexOf('#');
    return hash < 0 ? input : input.substring(0, hash).trim();
  }

  /// Parse free-form host input into scheme / host / port.
  ///
  /// Accepts:
  /// - `host:7531`
  /// - `10.0.2.2:7531` (Android emulator → host loopback)
  /// - `host` (defaults port [defaultPort])
  /// - `ws://host:7531/v1/ws`, `http://host:7531`, `https://…`, `wss://…`
  /// - `[ipv6]:7531`
  /// - any of the above with a `#fp=<sha256>` fragment (stripped here)
  ///
  /// **Secure by default.** A bare host resolves to `wss`/`https`, matching the
  /// daemon's default TLS listener; plaintext must be asked for explicitly with
  /// a `ws://` or `http://` prefix. Defaulting the other way would mean a typo
  /// or a dropped scheme silently downgrades the connection carrying the device
  /// token.
  static ({bool secure, String host, int port}) parseEndpoint(String input) {
    var s = input.trim();
    if (s.isEmpty) {
      throw ArgumentError('host is empty');
    }

    var secure = true;
    final lower = s.toLowerCase();
    if (lower.startsWith('wss://') || lower.startsWith('https://')) {
      s = s.substring(s.indexOf('://') + 3);
    } else if (lower.startsWith('ws://') || lower.startsWith('http://')) {
      secure = false;
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

import 'dart:convert';
import 'dart:io' show Platform;

import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../protocol/pair_uri.dart' show TlsMode, normalizeFingerprint;

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
    @visibleForTesting DateTime Function()? clock,
  }) : _secure =
           secure ??
           const FlutterSecureStorage(
             aOptions: AndroidOptions(resetOnError: true),
           ),
       _allowPlaintextFallback =
           allowPlaintextFallback ?? _defaultAllowPlaintextFallback,
       _clock = clock ?? DateTime.now {
    // Assigned in the body rather than the initializer list: the field is
    // private and lazily (re)populated, so it cannot be an initializing formal.
    _prefs = prefs;
  }

  static const _kHost = 'host';
  static const _kToken = 'device_token';
  static const _kDeviceId = 'device_id';
  static const _kThemeMode = 'theme_mode';
  static const _kNotifications = 'notifications_enabled';
  static const _kLastCwd = 'last_session_cwd';
  static const _kRecentCwds = 'recent_session_cwds';

  /// How many recent working directories the new-session menu offers.
  static const kMaxRecentCwds = 5;
  static const _kPreferredModelPrefix = 'preferred_model_';
  static const _kPreferredModelProviderPrefix = 'preferred_model_provider_';
  static const _kRelayUrl = 'relay_url';
  static const _kRelayHostId = 'relay_host_id';
  static const _kRelayAuthority = 'relay_authority';
  static const _kTokenFallback = 'device_token_fallback';
  static const _kPins = 'cert_pins';
  static const _kPinsFallback = 'cert_pins_fallback';
  static const _kClientCert = 'client_cert';
  static const _kClientCertFallback = 'client_cert_fallback';
  static const _kClientKey = 'client_key';
  static const _kClientKeyFallback = 'client_key_fallback';

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

  /// A transient keyring failure should not permanently force desktop callers
  /// onto cleartext preferences. While this deadline is active we avoid
  /// repeatedly waking a locked keyring; the next operation after it expires
  /// probes secure storage again.
  static const _secureRetryCooldown = Duration(seconds: 2);
  final DateTime Function() _clock;
  DateTime? _secureRetryAfter;
  Object? _lastSecureFailure;

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

  /// Optional mcrelay base URL from the pair QR (`relay=`), for off-mesh path.
  Future<String?> getRelayUrl() async => (await _p).getString(_kRelayUrl);

  Future<void> setRelayUrl(String? url) async {
    final p = await _p;
    if (url == null || url.isEmpty) {
      await p.remove(_kRelayUrl);
    } else {
      await p.setString(_kRelayUrl, url);
    }
  }

  /// Public host id for mcrelay join (`hid=`).
  Future<String?> getRelayHostId() async => (await _p).getString(_kRelayHostId);

  Future<void> setRelayHostId(String? id) async {
    final p = await _p;
    if (id == null || id.isEmpty) {
      await p.remove(_kRelayHostId);
    } else {
      await p.setString(_kRelayHostId, id);
    }
  }

  /// Authority the relay tuple belongs to. A relay join hint is credentials
  /// for one daemon, never a generic fallback for whatever host is typed next.
  Future<String?> getRelayAuthority() async =>
      (await _p).getString(_kRelayAuthority);

  Future<void> setRelayRoute({
    required String? url,
    required String? hostId,
    required String? authority,
  }) async {
    final p = await _p;
    final usable =
        url != null &&
        url.trim().isNotEmpty &&
        hostId != null &&
        hostId.trim().isNotEmpty;
    if (!usable) {
      await p.remove(_kRelayUrl);
      await p.remove(_kRelayHostId);
      await p.remove(_kRelayAuthority);
      return;
    }
    await p.setString(_kRelayUrl, url.trim());
    await p.setString(_kRelayHostId, hostId.trim());
    await p.setString(_kRelayAuthority, authority ?? '');
  }

  Future<String?> getToken() => _readSecret(_kToken, _kTokenFallback);

  Future<void> setToken(String token) =>
      _writeSecret(_kToken, _kTokenFallback, token);

  Future<void> clearToken() => _clearSecret(_kToken, _kTokenFallback);

  // --- App preferences (non-secret; plain SharedPreferences). ---

  /// Theme mode, one of 'system' | 'light' | 'dark' (default 'system').
  Future<String> getThemeMode() async =>
      (await _p).getString(_kThemeMode) ?? 'system';

  Future<void> setThemeMode(String mode) async =>
      (await _p).setString(_kThemeMode, mode);

  /// Whether agent notifications are enabled (default true).
  Future<bool> getNotificationsEnabled() async =>
      (await _p).getBool(_kNotifications) ?? true;

  Future<void> setNotificationsEnabled(bool enabled) async =>
      (await _p).setBool(_kNotifications, enabled);

  /// Most-recently-used session working directories, newest first, capped at
  /// [kMaxRecentCwds]. Seeded from the legacy single last-cwd key so existing
  /// installs keep their remembered path.
  Future<List<String>> getRecentCwds() async {
    final p = await _p;
    final list = p.getStringList(_kRecentCwds);
    if (list != null) return list;
    final legacy = p.getString(_kLastCwd);
    return (legacy == null || legacy.isEmpty) ? const [] : [legacy];
  }

  /// Record [cwd] as the most recently used working directory.
  Future<void> addRecentCwd(String cwd) async {
    final trimmed = cwd.trim();
    if (trimmed.isEmpty) return;
    final recents = List<String>.from(await getRecentCwds())
      ..remove(trimmed)
      ..insert(0, trimmed);
    if (recents.length > kMaxRecentCwds) {
      recents.removeRange(kMaxRecentCwds, recents.length);
    }
    await (await _p).setStringList(_kRecentCwds, recents);
  }

  /// Last-chosen model id for [provider], used to seed the new-session picker.
  Future<String?> getPreferredModel(String provider) async {
    if (provider.isEmpty) return null;
    return (await _p).getString('$_kPreferredModelPrefix$provider');
  }

  Future<void> setPreferredModel(String provider, String model) async {
    if (provider.isEmpty) return;
    final p = await _p;
    if (model.isEmpty) {
      await p.remove('$_kPreferredModelPrefix$provider');
    } else {
      await p.setString('$_kPreferredModelPrefix$provider', model);
    }
  }

  /// Last-chosen **model provider** (anthropic, openai, …) for an agent
  /// provider. Stored alongside the preferred model so the second session with
  /// a given agent opens the model picker on the right list instead of the
  /// connected-set default (MADR 0043 D10).
  Future<String?> getPreferredModelProvider(String provider) async {
    if (provider.isEmpty) return null;
    return (await _p).getString('$_kPreferredModelProviderPrefix$provider');
  }

  Future<void> setPreferredModelProvider(
    String provider,
    String modelProvider,
  ) async {
    if (provider.isEmpty) return;
    final p = await _p;
    if (modelProvider.isEmpty) {
      await p.remove('$_kPreferredModelProviderPrefix$provider');
    } else {
      await p.setString(
        '$_kPreferredModelProviderPrefix$provider',
        modelProvider,
      );
    }
  }

  /// The device's client-identity certificate and private key (ADR 0005), or
  /// null when none has been generated (or only one PEM survives, which is
  /// treated as absent so a fresh, self-consistent pair is minted).
  ///
  /// Stored under the same secure-storage discipline as the token: never
  /// written or read in cleartext on Android / iOS. A [SecureStorageUnavailable]
  /// on write, or a null read, forces the identity to be regenerated — which
  /// costs a re-pair, exactly as a lost token does.
  Future<({String cert, String key})?> getClientCertAndKey() async {
    final cert = await _readSecret(_kClientCert, _kClientCertFallback);
    final key = await _readSecret(_kClientKey, _kClientKeyFallback);
    if (cert == null || cert.isEmpty || key == null || key.isEmpty) return null;
    return (cert: cert, key: key);
  }

  /// Persists the client-identity [cert]/[key] pair. Writes the key first so a
  /// mid-write failure never leaves a certificate without the key that proves
  /// it — [getClientCertAndKey] treats a half-written pair as absent either way.
  Future<void> setClientCertAndKey({
    required String cert,
    required String key,
  }) async {
    await _writeSecret(_kClientKey, _kClientKeyFallback, key);
    await _writeSecret(_kClientCert, _kClientCertFallback, cert);
  }

  Future<void> clearClientIdentity() async {
    await _clearSecret(_kClientCert, _kClientCertFallback);
    await _clearSecret(_kClientKey, _kClientKeyFallback);
  }

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
  /// Pass [deviceId] when the caller holds a fresher value than storage does.
  /// See [getPinnedCert] for [fallbackToPersistedIdentity].
  Future<String?> getFingerprint(
    String hostInput, {
    String? deviceId,
    bool fallbackToPersistedIdentity = false,
  }) async => (await getPinnedCert(
    hostInput,
    deviceId: deviceId,
    fallbackToPersistedIdentity: fallbackToPersistedIdentity,
  ))?.fingerprint;

  /// The pin *and* the TLS mode it was recorded under.
  ///
  /// The two travel together: the mode selects which acceptance rule the pin
  /// participates in, so restoring one without the other after process death
  /// would apply the wrong rule to a correct pin.
  /// With no [deviceId] in hand, the persisted identity is consulted only when
  /// [fallbackToPersistedIdentity] is set. It vouches for whichever daemon
  /// paired last, so the caller must have its own evidence that it is dialling
  /// that daemon — presenting the *stored* token is what proves it. Defaulting
  /// this on let an unclaimed pairing attempt against a second daemon read (and
  /// clobber) the first daemon's pin (MADR 0046 H-B).
  Future<({String fingerprint, TlsMode mode})?> getPinnedCert(
    String hostInput, {
    String? deviceId,
    bool fallbackToPersistedIdentity = false,
  }) async {
    final authority = _authorityOf(hostInput);
    final explicitId = _idOrNull(deviceId);
    final persistedId = _idOrNull(await getDeviceId());
    final effectiveId =
        explicitId ?? (fallbackToPersistedIdentity ? persistedId : null);
    // The migration keys a legacy pin under whoever is actually paired, which
    // is a question about storage, not about this lookup's trust decision.
    final pins = await _readPins(explicitId ?? persistedId);

    if (effectiveId != null) {
      final byId = _pinOf(pins['id:$effectiveId']);
      if (byId != null) return byId;
    }
    // No identity-keyed pin. Fall back to the secondary authority record, but
    // never to one another identity owns.
    for (final rec in pins.values) {
      if (rec is! Map) continue;
      if (rec['authority'] != authority) continue;
      final owner = _idOrNull(rec['device_id'] as String?);
      if ((effectiveId == null && owner != null) ||
          (effectiveId != null && owner != null && owner != effectiveId)) {
        continue;
      }
      final pin = _pinOf(rec);
      if (pin != null) return pin;
    }
    return null;
  }

  /// Pins [fingerprint] for the daemon reached at [hostInput], under the
  /// acceptance rule named by [mode]. Throws [ArgumentError] if it is not a
  /// SHA-256 digest — an unusable pin must never be persisted as if it were.
  ///
  /// Without a [deviceId] the pin is filed against the host authority alone,
  /// as a *pending* record owned by nobody, and is adopted by the identity that
  /// completes pairing. A write never borrows the persisted identity: that
  /// filed a not-yet-claimed daemon's certificate under the paired daemon's id
  /// and overwrote a working pin (MADR 0046 H-B).
  Future<void> setFingerprint(
    String hostInput,
    String fingerprint, {
    String? deviceId,
    TlsMode mode = TlsMode.fallback,
  }) async {
    final canonical = normalizeFingerprint(fingerprint);
    if (canonical == null) {
      throw ArgumentError(
        'not a SHA-256 certificate fingerprint: $fingerprint',
      );
    }
    final authority = _authorityOf(hostInput);
    final effectiveId = _idOrNull(deviceId);
    final pins = await _readPins(effectiveId ?? _idOrNull(await getDeviceId()));

    if (effectiveId != null) {
      // The identity is known now, so any address-keyed record for the same
      // daemon is superseded rather than left to rot as a second answer.
      pins.remove('host:$authority');
    }
    pins.remove(
      _pinKey(effectiveId, authority),
    ); // re-insert last for LRU eviction
    pins[_pinKey(effectiveId, authority)] = <String, String>{
      'fp': canonical,
      'authority': authority,
      'device_id': ?effectiveId,
      'mode': mode.wire,
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

  /// A stored record as (fingerprint, mode), or null if it holds no usable pin.
  ///
  /// A record written before the mode was stored — or one carrying a mode this
  /// build does not recognise — reads back as [TlsMode.fallback]: pin-only is
  /// the rule those pins were taken under, and the safe reading of the other.
  static ({String fingerprint, TlsMode mode})? _pinOf(Object? rec) {
    if (rec is! Map) return null;
    final raw = rec['fp'];
    if (raw is! String || raw.isEmpty) return null;
    final fp = normalizeFingerprint(raw);
    if (fp == null) return null;
    final mode = rec['mode'];
    return (
      fingerprint: fp,
      mode:
          (mode is String ? TlsMode.tryParse(mode) : null) ?? TlsMode.fallback,
    );
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
    final canonical = (legacy == null || legacy.isEmpty)
        ? null
        : normalizeFingerprint(legacy);
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
    if (_shouldTrySecure) {
      try {
        final v = await _secure.read(key: key);
        _recordSecureSuccess();
        if (v != null && v.isNotEmpty) {
          // A recovered secure store is authoritative. Retire any desktop
          // fallback too, so a later transient outage cannot resurrect stale
          // credentials.
          await _removeSecretFallback(fallbackKey);
          return v;
        }
      } on Exception catch (e) {
        _recordSecureFailure('read', e);
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
    Object? failure = _lastSecureFailure;
    if (_shouldTrySecure) {
      try {
        await _secure.write(key: key, value: value);
        _recordSecureSuccess();
        // Clear any previous plaintext fallback.
        final p = await _p;
        await p.remove(fallbackKey);
        return;
      } on Exception catch (e) {
        failure = e;
        _recordSecureFailure('write', e);
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

  /// Deletes a secret from the keystore, and from the desktop fallback.
  ///
  /// Unlike reads and writes this ignores the failure cooldown, and reports a
  /// failed delete instead of swallowing it. The cooldown exists to stop the
  /// hot path hammering a flaky keystore; a clear is one-shot and has no later
  /// attempt to defer to, so honouring it left live credentials in the
  /// keystore while telling the user they had been cleared (MADR 0046 M-3).
  Future<void> _clearSecret(String key, String fallbackKey) async {
    Object? failure;
    try {
      await _secure.delete(key: key);
      _recordSecureSuccess();
    } on Exception catch (e) {
      failure = e;
      _recordSecureFailure('delete', e);
    }
    final p = await _p;
    await p.remove(fallbackKey);
    if (failure != null && !_allowPlaintextFallback) {
      throw SecureStorageUnavailable(failure);
    }
  }

  /// Removes any cleartext value written by an older build. No-op where the
  /// fallback is a supported (desktop) code path.
  Future<void> _purgePlaintextFallback(String fallbackKey) async {
    if (_allowPlaintextFallback) return;
    await _removeSecretFallback(fallbackKey);
  }

  Future<void> _removeSecretFallback(String fallbackKey) async {
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
    await p.remove(_kRelayUrl);
    await p.remove(_kRelayHostId);
    await p.remove(_kRelayAuthority);
    await p.remove(_kLastCwd);
    await p.remove(_kRecentCwds);
    for (final key in p.getKeys()) {
      if (key.startsWith(_kPreferredModelPrefix) ||
          key.startsWith(_kPreferredModelProviderPrefix)) {
        await p.remove(key);
      }
    }
    // Every secret is attempted even if an earlier one failed — a partial
    // sign-out that stops at the first error leaves the rest live — but the
    // failure is still reported rather than presented as a successful clear.
    Object? failure;
    for (final clear in <Future<void> Function()>[
      clearToken,
      clearFingerprint,
      clearClientIdentity,
    ]) {
      try {
        await clear();
      } on SecureStorageUnavailable catch (e) {
        failure ??= e;
      }
    }
    if (failure != null) throw failure;
  }

  bool get _shouldTrySecure {
    final retryAfter = _secureRetryAfter;
    return retryAfter == null || !_clock().isBefore(retryAfter);
  }

  void _recordSecureSuccess() {
    _secureRetryAfter = null;
    _lastSecureFailure = null;
  }

  void _recordSecureFailure(String operation, Exception error) {
    _lastSecureFailure = error;
    _secureRetryAfter = _clock().add(_secureRetryCooldown);
    if (!_allowPlaintextFallback) {
      debugPrint(
        'SettingsStore: secure-storage $operation failed '
        '(${error.runtimeType}); '
        'no cleartext fallback on this platform.',
      );
      return;
    }
    debugPrint(
      'SettingsStore: secure-storage $operation failed '
      '(${error.runtimeType}); '
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
    final raw = _fragmentField(input, 'fp');
    return raw == null ? null : normalizeFingerprint(raw);
  }

  /// Extracts the TLS mode carried alongside the pin as `#…&mode=…`.
  ///
  /// Returns null when the fragment names no mode (every host string written
  /// before the parameter existed) or names one this build does not know — in
  /// both cases the caller applies [TlsMode.fallback], the pin-only rule.
  static TlsMode? tlsModeFrom(String input) {
    final raw = _fragmentField(input, 'mode');
    return raw == null ? null : TlsMode.tryParse(raw);
  }

  static String? _fragmentField(String input, String name) {
    final hash = input.indexOf('#');
    if (hash < 0) return null;
    final prefix = '$name=';
    for (final part in input.substring(hash + 1).trim().split('&')) {
      if (part.toLowerCase().startsWith(prefix)) {
        return part.substring(prefix.length);
      }
    }
    return null;
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
    return Uri(scheme: scheme, host: ep.host, port: ep.port).toString();
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

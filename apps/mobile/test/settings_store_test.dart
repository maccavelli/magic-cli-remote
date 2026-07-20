import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart'
    show TlsMode, normalizeFingerprint;
import 'package:shared_preferences/shared_preferences.dart';

/// The same 32-byte digest in each accepted encoding.
const _fpHex =
    '0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20';
const _fpColonHex = '01:02:03:04:05:06:07:08:09:0A:0B:0C:0D:0E:0F:10:'
    '11:12:13:14:15:16:17:18:19:1A:1B:1C:1D:1E:1F:20';
const _fpB64 = 'AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA';

/// A second, unrelated daemon's digest.
const _fpB64Other = 'ISIjJCUmJygpKissLS4vMDEyMzQ1Njc4OTo7PD0-P0A';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  // The daemon serves TLS by default, so a bare host must resolve to wss.
  // Defaulting to ws here would mean any host typed or pasted without a scheme
  // silently sends the device token in cleartext.
  test('normalizeWsUrl host:port is secure by default', () {
    expect(
      SettingsStore.normalizeWsUrl('10.0.2.2:7531'),
      'wss://10.0.2.2:7531/v1/ws',
    );
  });

  test('normalizeWsUrl host only is secure by default', () {
    expect(
      SettingsStore.normalizeWsUrl('devbox'),
      'wss://devbox:7531/v1/ws',
    );
  });

  test('explicit ws:// opts out of TLS', () {
    expect(
      SettingsStore.parseEndpoint('ws://h:7531').secure,
      isFalse,
    );
    expect(
      SettingsStore.parseEndpoint('http://h:7531').secure,
      isFalse,
    );
    expect(SettingsStore.parseEndpoint('h:7531').secure, isTrue);
    expect(SettingsStore.parseEndpoint('wss://h:7531').secure, isTrue);
    expect(SettingsStore.parseEndpoint('https://h:7531').secure, isTrue);
  });

  test('normalizeWsUrl full path', () {
    expect(
      SettingsStore.normalizeWsUrl('ws://h:7531/v1/ws'),
      'ws://h:7531/v1/ws',
    );
  });

  test('normalizeWsUrl strips http scheme (no ws://http:// bug)', () {
    expect(
      SettingsStore.normalizeWsUrl('http://100.64.0.1:7531'),
      'ws://100.64.0.1:7531/v1/ws',
    );
    expect(
      SettingsStore.normalizeWsUrl('https://100.64.0.1:7531'),
      'wss://100.64.0.1:7531/v1/ws',
    );
  });

  test('normalizeWsUrl strips pasted path', () {
    expect(
      SettingsStore.normalizeWsUrl('100.64.0.1:7531/v1/ws'),
      'wss://100.64.0.1:7531/v1/ws',
    );
    expect(
      SettingsStore.normalizeWsUrl('100.64.0.1:7531/healthz'),
      'wss://100.64.0.1:7531/v1/ws',
    );
  });

  test('httpBaseFromWs', () {
    expect(
      SettingsStore.httpBaseFromWs('ws://10.0.2.2:7531/v1/ws'),
      'http://10.0.2.2:7531',
    );
  });

  test('healthzUrl uses port 7531 not a garbage concatenation', () {
    expect(
      SettingsStore.healthzUrl('100.64.0.1:7531'),
      'https://100.64.0.1:7531/healthz',
    );
    expect(
      SettingsStore.healthzUrl('100.64.0.1'),
      'https://100.64.0.1:7531/healthz',
    );
    expect(
      SettingsStore.healthzUrl('ws://100.64.0.1:7531/v1/ws'),
      'http://100.64.0.1:7531/healthz',
    );
    expect(
      SettingsStore.healthzUrl('http://100.64.0.1:7531'),
      'http://100.64.0.1:7531/healthz',
    );
  });

  test('healthzUrl and httpBaseFromWs follow the ws scheme, fp and all', () {
    // The `#fp=` fragment must never leak into a request URL.
    const host = 'wss://100.64.0.1:7531#fp=$_fpB64';
    expect(
      SettingsStore.healthzUrl(host),
      'https://100.64.0.1:7531/healthz',
    );
    expect(
      SettingsStore.httpBaseFromWs(host),
      'https://100.64.0.1:7531',
    );
    expect(
      SettingsStore.normalizeWsUrl(host),
      'wss://100.64.0.1:7531/v1/ws',
    );
  });

  group('fingerprint parsing', () {
    test('accepts the canonical base64url form from a pair QR', () {
      expect(normalizeFingerprint(_fpB64), _fpB64);
    });

    test('accepts hex, colon-hex, uppercase and a sha256: prefix', () {
      for (final input in <String>[
        _fpHex,
        _fpHex.toUpperCase(),
        _fpColonHex,
        'sha256:$_fpHex',
        'SHA256:$_fpColonHex',
      ]) {
        expect(normalizeFingerprint(input), _fpB64, reason: input);
      }
    });

    test('rejects anything that is not a 32-byte digest', () {
      for (final bad in <String>[
        '',
        '   ',
        'deadbeef',
        'not-a-fingerprint',
        _fpHex.substring(2), // too short
        '${_fpHex}00', // too long
        'z' * 44, // right alphabet, wrong length
        'a' * 200, // over the guard
      ]) {
        expect(normalizeFingerprint(bad), isNull, reason: bad);
      }
    });

    test('fingerprintFrom reads the #fp= fragment, stripFingerprint drops it',
        () {
      expect(
        SettingsStore.fingerprintFrom('wss://h:7531#fp=$_fpB64'),
        _fpB64,
      );
      // Hex in the fragment normalizes too, so a hand-typed host still pins.
      expect(
        SettingsStore.fingerprintFrom('wss://h:7531#fp=$_fpHex'),
        _fpB64,
      );
      expect(SettingsStore.fingerprintFrom('wss://h:7531'), isNull);
      expect(SettingsStore.fingerprintFrom('wss://h:7531#other=1'), isNull);
      expect(SettingsStore.fingerprintFrom('wss://h:7531#fp=garbage'), isNull);
      expect(
        SettingsStore.stripFingerprint('wss://h:7531#fp=$_fpB64'),
        'wss://h:7531',
      );
      expect(SettingsStore.stripFingerprint('wss://h:7531'), 'wss://h:7531');
    });

    test('tlsModeFrom reads mode= alongside fp= in the same fragment', () {
      const host = 'wss://h:7531#fp=$_fpB64&mode=letsencrypt';
      expect(SettingsStore.tlsModeFrom(host), TlsMode.letsencrypt);
      // Adding the second field must not disturb the first, nor the strip.
      expect(SettingsStore.fingerprintFrom(host), _fpB64);
      expect(SettingsStore.stripFingerprint(host), 'wss://h:7531');

      expect(
        SettingsStore.tlsModeFrom('wss://h:7531#mode=selfsigned'),
        TlsMode.selfsigned,
      );
      // Absent or unrecognised both mean "the caller picks the safe rule".
      expect(SettingsStore.tlsModeFrom('wss://h:7531'), isNull);
      expect(SettingsStore.tlsModeFrom('wss://h:7531#fp=$_fpB64'), isNull);
      expect(SettingsStore.tlsModeFrom('wss://h:7531#mode=acme'), isNull);
    });
  });

  group('fingerprint persistence', () {
    setUp(() {
      SharedPreferences.setMockInitialValues({});
    });

    test('round-trips for the host it was pinned for', () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      await store.setFingerprint('wss://100.64.0.1:7531', _fpHex);
      // Stored canonically, whatever encoding went in.
      expect(await store.getFingerprint('100.64.0.1:7531'), _fpB64);
      // Scheme and fragment do not change which daemon this identifies.
      expect(
        await store.getFingerprint('ws://100.64.0.1:7531#fp=$_fpB64'),
        _fpB64,
      );
    });

    test('keeps a pin per host: pinning B does not evict A', () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      await store.setFingerprint('100.64.0.1:7531', _fpHex);
      await store.setFingerprint('100.64.0.9:7531', _fpB64Other);

      // Alternating between two daemons used to discard whichever pin was not
      // written last, forcing a QR rescan on every switch.
      expect(await store.getFingerprint('100.64.0.1:7531'), _fpB64);
      expect(await store.getFingerprint('100.64.0.9:7531'), _fpB64Other);
      expect(await store.getFingerprint('100.64.0.1:7531'), _fpB64);
    });

    test('migrates the old single-slot format instead of re-pairing', () async {
      SharedPreferences.setMockInitialValues({
        'flutter.cert_fingerprint_host': '100.64.0.1:7531',
      });
      final prefs = await SharedPreferences.getInstance();
      final secure = _InMemorySecureStorage({'cert_fingerprint': _fpB64});
      final store = SettingsStore(
        secure: secure,
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      expect(await store.getFingerprint('100.64.0.1:7531'), _fpB64);
      // Migrated, not merely read through: the legacy slot is retired and the
      // pin survives in the new store.
      expect(secure.values['cert_fingerprint'], isNull);
      expect(prefs.getString('cert_fingerprint_host'), isNull);
      expect(secure.values['cert_pins'], isNotNull);
      expect(await store.getFingerprint('100.64.0.1:7531'), _fpB64);
      // And it is still scoped: another daemon does not inherit it.
      expect(await store.getFingerprint('100.64.0.9:7531'), isNull);
    });

    test('survives tailnet IP churn when the device identity is known',
        () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );
      await store.setDeviceId('dev-abc');

      await store.setFingerprint('100.64.0.1:7531', _fpB64);
      // The node was deleted and re-registered in Headscale, so it answers on a
      // new 100.x with the same certificate. Keying the pin on the address
      // would miss here and demand a rescan for an identity that never changed.
      expect(await store.getFingerprint('100.64.0.9:7531'), _fpB64);
    });

    test('a different identity never inherits a pin, whatever the address',
        () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      await store.setFingerprint('100.64.0.1:7531', _fpB64,
          deviceId: 'dev-abc');
      expect(
        await store.getFingerprint('100.64.0.1:7531', deviceId: 'dev-abc'),
        _fpB64,
      );
      // Same address, different daemon identity: returning the other device's
      // pin would vouch for a host it says nothing about.
      expect(
        await store.getFingerprint('100.64.0.1:7531', deviceId: 'dev-xyz'),
        isNull,
      );
    });

    test('a pin taken before pairing is claimed by the identity that follows',
        () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      // First connect: the QR fingerprint is known, the device id is not.
      await store.setFingerprint('100.64.0.1:7531', _fpB64);
      expect(await store.getFingerprint('100.64.0.1:7531'), _fpB64);

      // Pair completes and the daemon issues an id; the pin follows it.
      await store.setDeviceId('dev-abc');
      expect(await store.getFingerprint('100.64.0.1:7531'), _fpB64);
      await store.setFingerprint('100.64.0.1:7531', _fpB64);
      expect(await store.getFingerprint('100.64.0.9:7531'), _fpB64);
    });

    test('is not returned for a different host', () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      await store.setFingerprint('100.64.0.1:7531', _fpB64);
      // A pin from another daemon would either fail closed or, worse, vouch
      // for the wrong identity.
      expect(await store.getFingerprint('100.64.0.9:7531'), isNull);
      expect(await store.getFingerprint('100.64.0.1:9999'), isNull);
    });

    test('refuses to persist a malformed fingerprint', () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      await expectLater(
        store.setFingerprint('h:7531', 'not-a-fingerprint'),
        throwsA(isA<ArgumentError>()),
      );
      expect(await store.getFingerprint('h:7531'), isNull);
    });

    test('the tls mode is stored with the pin and read back with it', () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      await store.setFingerprint(
        '100.64.0.1:7531',
        _fpB64,
        deviceId: 'dev-abc',
        mode: TlsMode.letsencrypt,
      );

      final pin =
          await store.getPinnedCert('100.64.0.1:7531', deviceId: 'dev-abc');
      expect(pin, isNotNull);
      expect(pin!.fingerprint, _fpB64);
      // The mode has to arrive with the pin: after process death, restoring one
      // without the other applies the wrong acceptance rule to a correct pin.
      expect(pin.mode, TlsMode.letsencrypt);
    });

    test('a pin stored without a mode reads back as pin-only', () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      // What a build predating the mode wrote. Reading it as letsencrypt would
      // silently widen the trust set of an existing pin to the public CAs.
      await store.setFingerprint('100.64.0.1:7531', _fpB64);
      final pin = await store.getPinnedCert('100.64.0.1:7531');
      expect(pin!.fingerprint, _fpB64);
      expect(pin.mode, TlsMode.selfsigned);
    });

    test('the mode is replaced along with the pin it belongs to', () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      await store.setFingerprint('100.64.0.1:7531', _fpB64,
          mode: TlsMode.letsencrypt);
      // The host was re-paired in selfsigned mode: the rule must narrow with
      // the pin rather than leaving the wider one in place.
      await store.setFingerprint('100.64.0.1:7531', _fpB64Other,
          mode: TlsMode.selfsigned);

      final pin = await store.getPinnedCert('100.64.0.1:7531');
      expect(pin!.fingerprint, _fpB64Other);
      expect(pin.mode, TlsMode.selfsigned);
    });

    test('clearAll removes the pin', () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      await store.setHost('h:7531');
      await store.setFingerprint('h:7531', _fpB64);
      await store.clearAll();
      expect(await store.getFingerprint('h:7531'), isNull);
    });

    test('mobile: a locked keystore yields no pin rather than a cleartext one',
        () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _FailingSecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      await expectLater(
        store.setFingerprint('h:7531', _fpB64),
        throwsA(isA<SecureStorageUnavailable>()),
      );
      expect(prefs.getString('cert_fingerprint_fallback'), isNull);
      expect(prefs.getString('cert_pins_fallback'), isNull);
    });
  });

  test('parseEndpoint rejects invalid ports', () {
    expect(
      () => SettingsStore.parseEndpoint('host:99999'),
      throwsA(isA<ArgumentError>()),
    );
    expect(
      () => SettingsStore.parseEndpoint('host:0'),
      throwsA(isA<ArgumentError>()),
    );
  });

  test('IPv6 with brackets', () {
    expect(
      SettingsStore.healthzUrl('[fd7a:115c:a1e0::1]:7531'),
      'https://[fd7a:115c:a1e0::1]:7531/healthz',
    );
    expect(
      SettingsStore.healthzUrl('http://[fd7a:115c:a1e0::1]:7531'),
      'http://[fd7a:115c:a1e0::1]:7531/healthz',
    );
  });

  test('normalizeWsUrl preserves wss scheme from a pair payload host', () {
    expect(
      SettingsStore.normalizeWsUrl('wss://secure.host:443'),
      'wss://secure.host:443/v1/ws',
    );
    expect(
      SettingsStore.normalizeWsUrl('wss://secure.host:443/v1/ws'),
      'wss://secure.host:443/v1/ws',
    );
    // Uri elides the default https port.
    expect(
      SettingsStore.httpBaseFromWs('wss://secure.host:443/v1/ws'),
      'https://secure.host',
    );
    expect(
      SettingsStore.healthzUrl('wss://secure.host:8443'),
      'https://secure.host:8443/healthz',
    );
  });

  group('token storage fallback gating', () {
    setUp(() {
      SharedPreferences.setMockInitialValues({});
    });

    test('mobile: secure-storage failure throws instead of writing cleartext',
        () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _FailingSecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      await expectLater(
        store.setToken('mcr_secret'),
        throwsA(isA<SecureStorageUnavailable>()),
      );
      expect(prefs.getString('device_token_fallback'), isNull);
      expect(prefs.getKeys(), isNot(contains('device_token_fallback')));
    });

    test('mobile: pre-existing cleartext fallback is purged, not returned',
        () async {
      SharedPreferences.setMockInitialValues({
        'flutter.device_token_fallback': 'mcr_legacy_plaintext',
      });
      final prefs = await SharedPreferences.getInstance();
      expect(prefs.getString('device_token_fallback'), 'mcr_legacy_plaintext');

      final store = SettingsStore(
        secure: _FailingSecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      expect(await store.getToken(), isNull);
      expect(prefs.getString('device_token_fallback'), isNull);
    });

    test('desktop: secure-storage failure still falls back to prefs', () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _FailingSecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: true,
      );

      await store.setToken('mcr_secret');
      expect(prefs.getString('device_token_fallback'), 'mcr_secret');
      expect(await store.getToken(), 'mcr_secret');
    });
  });
}

/// Secure storage that is always unavailable, as on a locked keyring.
class _FailingSecureStorage implements FlutterSecureStorage {
  @override
  dynamic noSuchMethod(Invocation invocation) =>
      Future<Never>.error(StateError('KeyringLocked'));
}

/// Working keystore stand-in, so tests exercise the real (non-fallback) path.
///
/// Implemented via noSuchMethod rather than concrete overrides: the plugin's
/// per-platform options types change between releases, and this fake only
/// cares about key/value.
class _InMemorySecureStorage implements FlutterSecureStorage {
  _InMemorySecureStorage([Map<String, String>? initial])
      : _values = {...?initial};

  final Map<String, String> _values;

  /// Raw contents, so a test can assert what was actually persisted.
  Map<String, String> get values => _values;

  @override
  dynamic noSuchMethod(Invocation invocation) {
    final key = invocation.namedArguments[#key] as String?;
    switch (invocation.memberName) {
      case #read:
        return Future<String?>.value(_values[key]);
      case #write:
        final value = invocation.namedArguments[#value] as String?;
        if (value == null) {
          _values.remove(key);
        } else {
          _values[key!] = value;
        }
        return Future<void>.value();
      case #delete:
        _values.remove(key);
        return Future<void>.value();
      default:
        return Future<Never>.error(
          UnimplementedError('${invocation.memberName}'),
        );
    }
  }
}

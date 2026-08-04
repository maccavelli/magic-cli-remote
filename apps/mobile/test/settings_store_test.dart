import 'package:flutter/foundation.dart'
    show TargetPlatform, debugDefaultTargetPlatformOverride, debugPrint;
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart'
    show TlsMode, normalizeFingerprint;
import 'package:shared_preferences/shared_preferences.dart';

/// The same 32-byte digest in each accepted encoding.
const _fpHex =
    '0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20';
const _fpColonHex =
    '01:02:03:04:05:06:07:08:09:0A:0B:0C:0D:0E:0F:10:'
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
    expect(SettingsStore.normalizeWsUrl('devbox'), 'wss://devbox:7531/v1/ws');
  });

  test('explicit ws:// opts out of TLS', () {
    expect(SettingsStore.parseEndpoint('ws://h:7531').secure, isFalse);
    expect(SettingsStore.parseEndpoint('http://h:7531').secure, isFalse);
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
    expect(SettingsStore.healthzUrl(host), 'https://100.64.0.1:7531/healthz');
    expect(SettingsStore.httpBaseFromWs(host), 'https://100.64.0.1:7531');
    expect(SettingsStore.normalizeWsUrl(host), 'wss://100.64.0.1:7531/v1/ws');
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

    test(
      'fingerprintFrom reads the #fp= fragment, stripFingerprint drops it',
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
        expect(
          SettingsStore.fingerprintFrom('wss://h:7531#fp=garbage'),
          isNull,
        );
        expect(
          SettingsStore.stripFingerprint('wss://h:7531#fp=$_fpB64'),
          'wss://h:7531',
        );
        expect(SettingsStore.stripFingerprint('wss://h:7531'), 'wss://h:7531');
      },
    );

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

    test('survives tailnet IP churn when the device identity is known', () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );
      await store.setDeviceId('dev-abc');

      // Pairing records the pin against the identity the daemon issued.
      await store.setFingerprint(
        '100.64.0.1:7531',
        _fpB64,
        deviceId: 'dev-abc',
      );
      // The node was deleted and re-registered in Headscale, so it answers on a
      // new 100.x with the same certificate. Keying the pin on the address
      // would miss here and demand a rescan for an identity that never changed.
      // Reconnecting presents the stored token, which only this daemon issued,
      // so the persisted identity is allowed to answer for the new address.
      expect(
        await store.getFingerprint(
          '100.64.0.9:7531',
          fallbackToPersistedIdentity: true,
        ),
        _fpB64,
      );
    });

    test('a second daemon cannot overwrite the paired one\'s pin', () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );
      await store.setDeviceId('dev-abc');
      await store.setFingerprint(
        '100.64.0.1:7531',
        _fpB64,
        deviceId: 'dev-abc',
      );

      // Scanning a second daemon's QR resolves its pin before any claim has
      // succeeded, so no device id is in hand. Borrowing the persisted one
      // filed daemon B's certificate under daemon A's identity and left A
      // unreachable behind a spoofing warning (MADR 0046 H-B).
      await store.setFingerprint('100.64.0.9:7531', _fpB64Other);

      expect(
        await store.getFingerprint('100.64.0.1:7531', deviceId: 'dev-abc'),
        _fpB64,
      );
      // The new pin is still recorded — as a pending, host-scoped record.
      expect(await store.getFingerprint('100.64.0.9:7531'), _fpB64Other);
    });

    test('an unproven caller never inherits the paired identity', () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );
      await store.setDeviceId('dev-abc');
      await store.setFingerprint(
        '100.64.0.1:7531',
        _fpB64,
        deviceId: 'dev-abc',
      );

      // A probe or pair-claim against another address has no evidence it is
      // talking to the paired daemon, so the pin must not vouch for it.
      expect(await store.getFingerprint('100.64.0.9:7531'), isNull);
      expect(
        await store.getFingerprint(
          '100.64.0.9:7531',
          fallbackToPersistedIdentity: true,
        ),
        _fpB64,
      );
    });

    test(
      'a different identity never inherits a pin, whatever the address',
      () async {
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
        );
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
      },
    );

    test(
      'a pin taken before pairing is claimed by the identity that follows',
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

        // Pair completes and the daemon issues an id; the client re-persists
        // the pin against it, which is what promotes the pending record.
        await store.setDeviceId('dev-abc');
        expect(await store.getFingerprint('100.64.0.1:7531'), _fpB64);
        await store.setFingerprint(
          '100.64.0.1:7531',
          _fpB64,
          deviceId: 'dev-abc',
        );
        expect(
          await store.getFingerprint(
            '100.64.0.9:7531',
            fallbackToPersistedIdentity: true,
          ),
          _fpB64,
        );
      },
    );

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

      final pin = await store.getPinnedCert(
        '100.64.0.1:7531',
        deviceId: 'dev-abc',
      );
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

      await store.setFingerprint(
        '100.64.0.1:7531',
        _fpB64,
        mode: TlsMode.letsencrypt,
      );
      // The host was re-paired in selfsigned mode: the rule must narrow with
      // the pin rather than leaving the wider one in place.
      await store.setFingerprint(
        '100.64.0.1:7531',
        _fpB64Other,
        mode: TlsMode.selfsigned,
      );

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

    test(
      'mobile: a locked keystore yields no pin rather than a cleartext one',
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
      },
    );
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

  group('recent working directories', () {
    setUp(() {
      SharedPreferences.setMockInitialValues({});
    });

    test('newest first, deduplicated, capped at kMaxRecentCwds', () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      for (var i = 1; i <= 7; i++) {
        await store.addRecentCwd('/proj/$i');
      }
      // Re-adding an existing path moves it to the front, not a duplicate.
      await store.addRecentCwd('/proj/5');
      expect(await store.getRecentCwds(), [
        '/proj/5',
        '/proj/7',
        '/proj/6',
        '/proj/4',
        '/proj/3',
      ]);
      // Blank input is ignored rather than stored.
      await store.addRecentCwd('   ');
      expect((await store.getRecentCwds()).length, 5);
    });

    test('seeds from the legacy single last-cwd key', () async {
      SharedPreferences.setMockInitialValues({
        'last_session_cwd': '/home/mac/old',
      });
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      expect(await store.getRecentCwds(), ['/home/mac/old']);
      await store.addRecentCwd('/home/mac/new');
      expect(await store.getRecentCwds(), ['/home/mac/new', '/home/mac/old']);
    });
  });

  group('token storage fallback gating', () {
    setUp(() {
      SharedPreferences.setMockInitialValues({});
    });

    test(
      'mobile: secure-storage failure throws instead of writing cleartext',
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
      },
    );

    test(
      'mobile: pre-existing cleartext fallback is purged, not returned',
      () async {
        SharedPreferences.setMockInitialValues({
          'flutter.device_token_fallback': 'mcr_legacy_plaintext',
        });
        final prefs = await SharedPreferences.getInstance();
        expect(
          prefs.getString('device_token_fallback'),
          'mcr_legacy_plaintext',
        );

        final store = SettingsStore(
          secure: _FailingSecureStorage(),
          prefs: prefs,
          allowPlaintextFallback: false,
        );

        expect(await store.getToken(), isNull);
        expect(prefs.getString('device_token_fallback'), isNull);
      },
    );

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

    test(
      'retries a transient secure read after the cooldown expires',
      () async {
        final prefs = await SharedPreferences.getInstance();
        await prefs.setString('device_token_fallback', 'fallback');
        var now = DateTime(2026);
        final secure = _ControlledSecureStorage(
          {'device_token': 'secure'},
          failOnce: {'read'},
        );
        final store = SettingsStore(
          secure: secure,
          prefs: prefs,
          allowPlaintextFallback: true,
          clock: () => now,
        );

        expect(await store.getToken(), 'fallback');
        expect(await store.getToken(), 'fallback');
        expect(secure.calls.where((op) => op == 'read'), hasLength(1));

        now = now.add(const Duration(seconds: 2));
        expect(await store.getToken(), 'secure');
        expect(secure.calls.where((op) => op == 'read'), hasLength(2));
        expect(prefs.getString('device_token_fallback'), isNull);
      },
    );

    test(
      'retries secure writes and deletes after a transient failure',
      () async {
        final prefs = await SharedPreferences.getInstance();
        var now = DateTime(2026);
        final secure = _ControlledSecureStorage(
          {'device_token': 'old-secure'},
          failOnce: {'write', 'delete'},
        );
        final store = SettingsStore(
          secure: secure,
          prefs: prefs,
          allowPlaintextFallback: true,
          clock: () => now,
        );

        await store.setToken('new-secret');
        expect(prefs.getString('device_token_fallback'), 'new-secret');
        now = now.add(const Duration(seconds: 2));
        await store.setToken('new-secret');
        expect(secure.values['device_token'], 'new-secret');
        expect(prefs.getString('device_token_fallback'), isNull);

        await store.clearToken();
        expect(secure.values['device_token'], 'new-secret');
        now = now.add(const Duration(seconds: 2));
        await store.clearToken();
        expect(secure.values['device_token'], isNull);
      },
    );

    test('a sign-out deletes even inside the failure cooldown', () async {
      final prefs = await SharedPreferences.getInstance();
      var now = DateTime(2026);
      final secure = _ControlledSecureStorage(
        {'device_token': 'live-token'},
        failOnce: {'read'},
      );
      final store = SettingsStore(
        secure: secure,
        prefs: prefs,
        allowPlaintextFallback: false,
        clock: () => now,
      );

      // A read fails, arming the 2s cooldown.
      expect(await store.getToken(), isNull);
      secure.calls.clear();

      // Sign-out lands inside that window. Skipping the delete here left the
      // live token in the keystore while the UI said it had been cleared
      // (MADR 0046 M-3); a one-shot clear has no later attempt to defer to.
      await store.clearToken();
      expect(secure.calls, contains('delete'));
      expect(secure.values['device_token'], isNull);
    });

    test('a refused delete is reported, not reported as cleared', () async {
      final prefs = await SharedPreferences.getInstance();
      final secure = _ControlledSecureStorage(
        {
          'device_token': 'live-token',
          'cert_pins': '{}',
          'client_cert': 'pem',
          'client_key': 'pem',
        },
        failOnce: {'delete'},
      );
      final store = SettingsStore(
        secure: secure,
        prefs: prefs,
        allowPlaintextFallback: false,
      );

      await expectLater(
        store.clearAll(),
        throwsA(isA<SecureStorageUnavailable>()),
      );
      // The first delete failed, but every other secret was still attempted:
      // stopping at the first error would leave the rest live.
      expect(
        secure.calls.where((c) => c == 'delete').length,
        greaterThanOrEqualTo(4),
      );
      expect(secure.values['client_cert'], isNull);
      expect(secure.values['client_key'], isNull);
    });

    test('does not swallow Errors from secure storage', () async {
      final prefs = await SharedPreferences.getInstance();
      final store = SettingsStore(
        secure: _ControlledSecureStorage(
          const {},
          failOnce: {'read'},
          failure: StateError('programming bug'),
        ),
        prefs: prefs,
        allowPlaintextFallback: true,
      );

      await expectLater(store.getToken(), throwsA(isA<StateError>()));
    });

    test('secure-storage diagnostics never include the secret value', () async {
      final prefs = await SharedPreferences.getInstance();
      final oldDebugPrint = debugPrint;
      final logs = <String?>[];
      debugPrint = (message, {wrapWidth}) => logs.add(message);
      addTearDown(() => debugPrint = oldDebugPrint);
      final store = SettingsStore(
        secure: _ControlledSecureStorage(
          const {},
          failOnce: {'write'},
          failure: Exception('mcr_secret'),
        ),
        prefs: prefs,
        allowPlaintextFallback: true,
      );

      await store.setToken('mcr_secret');
      expect(logs.join('\n'), isNot(contains('mcr_secret')));
      expect(logs.join('\n'), contains('write failed'));
    });
  });

  group('connect mode (MADR 0064 D6)', () {
    setUp(() {
      SharedPreferences.setMockInitialValues({});
    });

    Future<SettingsStore> makeStore() async => SettingsStore(
      secure: _InMemorySecureStorage(),
      prefs: await SharedPreferences.getInstance(),
      allowPlaintextFallback: false,
    );

    test('defaults to auto when unset (V8)', () async {
      final store = await makeStore();
      expect(await store.getConnectMode(), 'auto');
    });

    test('select persists across store instances (V8)', () async {
      await (await makeStore()).setConnectMode('select');
      // A fresh instance stands in for an app restart.
      expect(await (await makeStore()).getConnectMode(), 'select');
    });

    test('a corrupted value reads back as auto, not a third mode', () async {
      SharedPreferences.setMockInitialValues({
        'flutter.connect_mode': 'sometimes',
      });
      final store = await makeStore();
      expect(await store.getConnectMode(), 'auto');
    });

    test('an invalid write is ignored rather than stored', () async {
      final store = await makeStore();
      await store.setConnectMode('select');
      await store.setConnectMode('sometimes');
      expect(await store.getConnectMode(), 'select');
    });
  });

  group('secret-store health (MADR 0066)', () {
    setUp(() {
      SharedPreferences.setMockInitialValues({});
    });

    Future<SettingsStore> makeStore(
      FlutterSecureStorage secure, {
      DateTime Function()? clock,
    }) async {
      final prefs = await SharedPreferences.getInstance();
      return SettingsStore(
        secure: secure,
        prefs: prefs,
        allowPlaintextFallback: false,
        clock: clock,
      );
    }

    test('setToken sets the expect-credentials marker; clearToken removes '
        'it; reads never touch it', () async {
      final store = await makeStore(_InMemorySecureStorage());
      final prefs = await SharedPreferences.getInstance();

      await store.getToken();
      expect(prefs.getBool('expect_credentials'), isNull);

      await store.setToken('mcr_token');
      expect(prefs.getBool('expect_credentials'), isTrue);

      await store.clearToken();
      expect(prefs.getBool('expect_credentials'), isNull);
    });

    test('a failed token write does not set the marker', () async {
      final store = await makeStore(_FailingSecureStorage());
      final prefs = await SharedPreferences.getInstance();

      await expectLater(
        store.setToken('mcr_token'),
        throwsA(isA<SecureStorageUnavailable>()),
      );
      expect(prefs.getBool('expect_credentials'), isNull);
    });

    test('clearSecrets removes exactly the secrets and the marker; host and '
        'path preferences survive', () async {
      SharedPreferences.setMockInitialValues({
        'flutter.host': '100.64.0.1:7531',
        'flutter.pinned_session_cwds': ['/home/me/proj'],
        'flutter.recent_session_cwds': ['/home/me/other'],
      });
      final secure = _InMemorySecureStorage({
        'device_token': 't',
        'cert_pins': '{}',
        'client_cert': 'cert-pem',
        'client_key': 'key-pem',
      });
      final store = await makeStore(secure);
      await store.setToken('t2'); // sets the marker too
      final prefs = await SharedPreferences.getInstance();

      await store.clearSecrets();

      expect(secure.values.keys, ['cert_pins']);
      // The pin survives a client-credential reset: it is trust of the
      // *host*, and it is what lets a typed pair code (no fingerprint)
      // still complete TLS afterwards (0066 incident #3 follow-up).
      expect(secure.values['cert_pins'], '{}');
      expect(prefs.getBool('expect_credentials'), isNull);
      expect(prefs.getString('host'), '100.64.0.1:7531');
      expect(prefs.getStringList('pinned_session_cwds'), ['/home/me/proj']);
      expect(prefs.getStringList('recent_session_cwds'), ['/home/me/other']);
    });

    test('clearAll clears connection identity but keeps path preferences '
        '(0066 F9)', () async {
      SharedPreferences.setMockInitialValues({
        'flutter.host': '100.64.0.1:7531',
        'flutter.device_id': 'dev1',
        'flutter.relay_url': 'https://relay',
        'flutter.pinned_session_cwds': ['/home/me/proj'],
        'flutter.recent_session_cwds': ['/home/me/other'],
        'flutter.last_session_cwd': '/home/me/last',
      });
      final secure = _InMemorySecureStorage({
        'device_token': 't',
        'cert_pins': '{"k":{"fp":"x"}}',
      });
      final store = await makeStore(secure);
      final prefs = await SharedPreferences.getInstance();

      await store.clearAll();

      expect(prefs.getString('host'), isNull);
      expect(prefs.getString('device_id'), isNull);
      expect(prefs.getString('relay_url'), isNull);
      // Sign-out walks away from the host's trust record too (unlike
      // clearSecrets, which keeps the pin for same-host recovery).
      expect(secure.values['device_token'], isNull);
      expect(secure.values['cert_pins'], anyOf(isNull, '{}'));
      // Sign-out is not amnesia: the working-directory prefs stay.
      expect(prefs.getStringList('pinned_session_cwds'), ['/home/me/proj']);
      expect(prefs.getStringList('recent_session_cwds'), ['/home/me/other']);
      expect(prefs.getString('last_session_cwd'), '/home/me/last');
    });

    test('probe: fresh install is ok', () async {
      final store = await makeStore(_InMemorySecureStorage());
      expect(await store.probeSecretStore(), SecretStoreHealth.ok);
    });

    test('probe: paired store is ok', () async {
      final store = await makeStore(_InMemorySecureStorage());
      // A real pairing always has both: the identity is written before the
      // first socket, the token at pair_ok.
      await store.setClientCertAndKey(cert: 'cert-pem', key: 'key-pem');
      await store.setToken('mcr_token');
      expect(await store.probeSecretStore(), SecretStoreHealth.ok);
    });

    test('probe: identity gone but token intact is credentialsLost '
        '(the per-key wipe shape observed on hardware)', () async {
      final secure = _InMemorySecureStorage();
      final store = await makeStore(secure);
      await store.setClientCertAndKey(cert: 'cert-pem', key: 'key-pem');
      await store.setToken('mcr_token');

      // The plugin's resetOnError deletes corrupt keys one at a time; this
      // time it ate the identity PEMs and left the token (0066 incident #2).
      secure.values.remove('client_cert');
      secure.values.remove('client_key');

      expect(await store.probeSecretStore(), SecretStoreHealth.credentialsLost);
    });

    test('probe: marker outliving the token is credentialsLost', () async {
      final secure = _InMemorySecureStorage();
      final store = await makeStore(secure);
      await store.setToken('mcr_token');

      // The platform wiped the secure store behind the app's back
      // (resetOnError's silent per-key delete): marker survives, token gone.
      secure.values.clear();

      expect(await store.probeSecretStore(), SecretStoreHealth.credentialsLost);
      // The verdict is stable across launches until re-pair or deliberate
      // clear — nothing consumed the marker.
      expect(await store.probeSecretStore(), SecretStoreHealth.credentialsLost);
    });

    test(
      'probe: deliberate clearToken does not read as credentialsLost',
      () async {
        final store = await makeStore(_InMemorySecureStorage());
        await store.setToken('mcr_token');
        await store.clearToken();
        expect(await store.probeSecretStore(), SecretStoreHealth.ok);
      },
    );

    test('probe: a store that errors on everything is broken', () async {
      final store = await makeStore(_FailingSecureStorage());
      expect(await store.probeSecretStore(), SecretStoreHealth.broken);
    });

    // MADR 0067 D5 (U6) — the reinstall inversion, 0066 F14's dormant
    // mirror image: on iOS the Keychain outlives app deletion while the
    // prefs marker does not, so a reinstall finds a previous install's
    // secrets with no marker. Matrix: marker × secrets × platform.
    group('iOS reinstall inversion', () {
      setUp(() {
        debugDefaultTargetPlatformOverride = TargetPlatform.iOS;
      });
      tearDown(() {
        debugDefaultTargetPlatformOverride = null;
      });

      test('secrets with no marker are cleared; verdict is ok (the connect '
          'screen is the re-pair flow)', () async {
        // Reinstall shape: Keychain retained everything, prefs are empty.
        final secure = _InMemorySecureStorage({
          'device_token': 'stale-token',
          'client_cert': 'stale-cert',
          'client_key': 'stale-key',
          'cert_pins': '{"h":{"fp":"x"}}',
        });
        final store = await makeStore(secure);

        expect(await store.probeSecretStore(), SecretStoreHealth.ok);
        expect(await store.getToken(), isNull);
        expect(await store.getClientCertAndKey(), isNull);
        // clearSecrets semantics: the pin is trust of the host, not a
        // client credential — it survives here exactly as it does in the
        // 0066 recovery path.
        expect(secure.values['cert_pins'], '{"h":{"fp":"x"}}');
      });

      test('identity-only leftovers are cleared too (pairing writes the '
          'identity before the first token)', () async {
        final secure = _InMemorySecureStorage({
          'client_cert': 'stale-cert',
          'client_key': 'stale-key',
        });
        final store = await makeStore(secure);

        expect(await store.probeSecretStore(), SecretStoreHealth.ok);
        expect(await store.getClientCertAndKey(), isNull);
      });

      test('marker present with secrets present stays ok and keeps them '
          '(the ordinary paired launch)', () async {
        final store = await makeStore(_InMemorySecureStorage());
        await store.setClientCertAndKey(cert: 'cert-pem', key: 'key-pem');
        await store.setToken('mcr_token');

        expect(await store.probeSecretStore(), SecretStoreHealth.ok);
        expect(await store.getToken(), 'mcr_token');
      });

      test('fresh install (no marker, no secrets) is ok and clears nothing',
          () async {
        final secure = _InMemorySecureStorage();
        final store = await makeStore(secure);

        expect(await store.probeSecretStore(), SecretStoreHealth.ok);
        expect(secure.values, isEmpty);
      });

      test('restored-from-backup shape (marker migrated, device-bound '
          'secrets did not) is credentialsLost', () async {
        // first_unlock_this_device keeps secrets off the new phone; the
        // prefs marker rides the backup. This is 0066's existing loss
        // signal — both inversions land in re-pair, never a crash loop.
        SharedPreferences.setMockInitialValues({
          'flutter.expect_credentials': true,
        });
        final store = await makeStore(_InMemorySecureStorage());

        expect(
          await store.probeSecretStore(),
          SecretStoreHealth.credentialsLost,
        );
      });
    });

    test('Android: secrets with no marker are left alone (no inversion '
        'exists there; behaviour is byte-identical to 0066)', () async {
      // Default test platform is android — stated for symmetry with the
      // iOS group above.
      final secure = _InMemorySecureStorage({
        'device_token': 'live-token',
        'client_cert': 'cert',
        'client_key': 'key',
      });
      final store = await makeStore(secure);

      expect(await store.probeSecretStore(), SecretStoreHealth.ok);
      expect(await store.getToken(), 'live-token');
      expect(secure.values['device_token'], 'live-token');
    });

    test(
      'probe: unwritable-but-empty store self-heals via deleteAll',
      () async {
        final secure = _ControlledSecureStorage({}, failOnce: {'write'});
        final store = await makeStore(secure);

        expect(await store.probeSecretStore(), SecretStoreHealth.ok);
        expect(secure.calls, contains('deleteAll'));
      },
    );

    test(
      'probe: never deleteAlls a store whose token is still readable',
      () async {
        // Reads work, writes are broken: the phone can still connect with the
        // stored credentials, so healing must not destroy them.
        final secure = _ControlledSecureStorage(
          {'device_token': 'live-token'},
          failOnce: {'write'},
        );
        final store = await makeStore(secure);

        expect(await store.probeSecretStore(), SecretStoreHealth.broken);
        expect(secure.calls, isNot(contains('deleteAll')));
        expect(secure.values['device_token'], 'live-token');
      },
    );

    test('failures persist a bounded diagnostic record', () async {
      final at = DateTime.utc(2026, 8, 2, 12, 30);
      final store = await makeStore(_FailingSecureStorage(), clock: () => at);

      await expectLater(
        store.setToken('mcr_token'),
        throwsA(isA<SecureStorageUnavailable>()),
      );
      await pumpEventQueue(); // the persist is deliberately fire-and-forget

      final rec = await store.getLastStorageFailure();
      expect(rec, isNotNull);
      expect(rec!.op, 'write');
      expect(rec.error, contains('KeyringLocked'));
      expect(rec.at, at);
    });

    test('getLastStorageFailure is null with no history and survives a '
        'corrupt record', () async {
      final store = await makeStore(_InMemorySecureStorage());
      expect(await store.getLastStorageFailure(), isNull);

      SharedPreferences.setMockInitialValues({
        'flutter.last_storage_failure': 'not json',
      });
      final store2 = SettingsStore(
        secure: _InMemorySecureStorage(),
        prefs: await SharedPreferences.getInstance(),
        allowPlaintextFallback: false,
      );
      expect(await store2.getLastStorageFailure(), isNull);
    });
  });
}

/// Secure storage that is always unavailable, as on a locked keyring.
class _FailingSecureStorage implements FlutterSecureStorage {
  @override
  dynamic noSuchMethod(Invocation invocation) =>
      Future<Never>.error(Exception('KeyringLocked'));
}

class _ControlledSecureStorage implements FlutterSecureStorage {
  _ControlledSecureStorage(
    Map<String, String> initial, {
    Set<String> failOnce = const {},
    Object? failure,
  }) : _values = {...initial},
       _failOnce = {...failOnce},
       _failure = failure ?? Exception('KeyringLocked');

  final Map<String, String> _values;
  final Set<String> _failOnce;
  final Object _failure;
  final List<String> calls = [];

  Map<String, String> get values => _values;

  @override
  dynamic noSuchMethod(Invocation invocation) {
    final operation = switch (invocation.memberName) {
      #read => 'read',
      #write => 'write',
      #delete => 'delete',
      _ => null,
    };
    if (invocation.memberName == #deleteAll) {
      calls.add('deleteAll');
      if (_failOnce.remove('deleteAll')) return Future<Never>.error(_failure);
      _values.clear();
      return Future<void>.value();
    }
    if (operation == null) return Future<void>.value();
    calls.add(operation);
    if (_failOnce.remove(operation)) return Future<Never>.error(_failure);
    final key = invocation.namedArguments[#key] as String?;
    switch (operation) {
      case 'read':
        return Future<String?>.value(_values[key]);
      case 'write':
        final value = invocation.namedArguments[#value] as String?;
        if (value == null) {
          _values.remove(key);
        } else {
          _values[key!] = value;
        }
        return Future<void>.value();
      case 'delete':
        _values.remove(key);
        return Future<void>.value();
      default:
        throw StateError('unreachable');
    }
  }
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
      case #deleteAll:
        _values.clear();
        return Future<void>.value();
      default:
        return Future<Never>.error(
          UnimplementedError('${invocation.memberName}'),
        );
    }
  }
}

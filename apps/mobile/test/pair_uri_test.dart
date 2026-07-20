import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';

const _fpHex =
    '0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20';
const _fpB64 = 'AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA';

void main() {
  test('parses mcremote pair URI with token', () {
    final p = PairPayload.tryParse(
      'mcremote://pair?host=100.64.0.1%3A7531&token=mcr_deadbeef',
    );
    expect(p, isNotNull);
    expect(p!.host, '100.64.0.1:7531');
    expect(p.token, 'mcr_deadbeef');
    expect(p.hasToken, isTrue);
  });

  test('parses pair URI with short code', () {
    final p = PairPayload.tryParse(
      'mcremote://pair?host=100.64.0.1%3A7531&code=K7M2-9X4P',
    );
    expect(p, isNotNull);
    expect(p!.code, 'K7M2-9X4P');
    expect(p.hasCode, isTrue);
    expect(p.hasToken, isFalse);
  });

  test('rejects non-pair payloads', () {
    expect(PairPayload.tryParse(''), isNull);
    expect(PairPayload.tryParse('mcr_only'), isNull);
    expect(PairPayload.tryParse('https://evil/pair?host=a&token=b'), isNull);
    expect(PairPayload.tryParse('mcremote://pair?host=a'), isNull);
  });

  test('preserves an explicit insecure scheme (no silent upgrade)', () {
    // Bare hosts default to TLS now, so an explicit ws:// must be carried
    // through in `host` — otherwise a deliberately plaintext daemon becomes
    // unreachable the moment the payload round-trips through the host field.
    for (final scheme in ['ws%3A%2F%2F', 'http%3A%2F%2F']) {
      final p = PairPayload.tryParse(
        'mcremote://pair?host=$scheme'
        '100.64.0.1%3A7531%2Fv1%2Fws&token=mcr_x',
      );
      expect(p, isNotNull);
      expect(p!.host, 'ws://100.64.0.1:7531');
      expect(p.hostAuthority, '100.64.0.1:7531');
      expect(p.secure, isFalse);
      expect(
        SettingsStore.normalizeWsUrl(p.host),
        'ws://100.64.0.1:7531/v1/ws',
      );
    }
  });

  test('a scheme-less host defaults to TLS', () {
    final p = PairPayload.tryParse(
      'mcremote://pair?host=100.64.0.1%3A7531&token=mcr_x',
    );
    expect(p, isNotNull);
    expect(p!.secure, isTrue);
    expect(
      SettingsStore.normalizeWsUrl(p.host),
      'wss://100.64.0.1:7531/v1/ws',
    );
  });

  group('certificate fingerprint', () {
    test('is parsed from fp= and survives in host as a #fp= fragment', () {
      final p = PairPayload.tryParse(
        'mcremote://pair?host=wss%3A%2F%2F100.64.0.1%3A7531'
        '&code=K7M2-9X4P&fp=$_fpB64',
      );
      expect(p, isNotNull);
      expect(p!.fingerprint, _fpB64);
      expect(p.host, 'wss://100.64.0.1:7531#fp=$_fpB64');
      expect(p.hostAuthority, '100.64.0.1:7531');
      // The fragment is the transport for the pin through the connect flow,
      // and must not corrupt the URLs derived from the same string.
      expect(SettingsStore.fingerprintFrom(p.host), _fpB64);
      expect(
        SettingsStore.normalizeWsUrl(p.host),
        'wss://100.64.0.1:7531/v1/ws',
      );
      expect(
        SettingsStore.healthzUrl(p.host),
        'https://100.64.0.1:7531/healthz',
      );
    });

    test('is normalized from hex to the canonical base64url form', () {
      final p = PairPayload.tryParse(
        'mcremote://pair?host=h%3A7531&token=t&fp=$_fpHex',
      );
      expect(p, isNotNull);
      expect(p!.fingerprint, _fpB64);
    });

    test('is null when the QR carries none', () {
      final p = PairPayload.tryParse(
        'mcremote://pair?host=h%3A7531&token=t',
      );
      expect(p, isNotNull);
      expect(p!.fingerprint, isNull);
      expect(p.host, 'h:7531');
    });

    test('a malformed fp rejects the whole payload', () {
      // Falling back to an unpinned connection here would turn a corrupt or
      // tampered QR into a silent downgrade.
      for (final bad in <String>[
        'garbage',
        'deadbeef',
        '$_fpHex 00',
        'z' * 44,
      ]) {
        expect(
          PairPayload.tryParse(
            'mcremote://pair?host=h%3A7531&token=t&fp=${Uri.encodeQueryComponent(bad)}',
          ),
          isNull,
          reason: bad,
        );
      }
    });

    test('an oversized fp is rejected before normalization', () {
      final big = 'a' * (PairPayload.maxFingerprintLength + 1);
      expect(
        PairPayload.tryParse('mcremote://pair?host=h%3A7531&token=t&fp=$big'),
        isNull,
      );
    });
  });

  group('tls mode', () {
    test('defaults to selfsigned when the QR carries no mode=', () {
      // Every QR generated before mode= existed was pinned-only, and pin-only
      // is the conservative reading either way.
      final p = PairPayload.tryParse(
        'mcremote://pair?host=h%3A7531&token=t&fp=$_fpB64',
      );
      expect(p, isNotNull);
      expect(p!.mode, TlsMode.selfsigned);
      // The default stays implicit, so these host strings are unchanged.
      expect(p.host, 'h:7531#fp=$_fpB64');
      expect(SettingsStore.tlsModeFrom(p.host), isNull);
    });

    test('letsencrypt survives in host as a #…&mode= fragment', () {
      final p = PairPayload.tryParse(
        'mcremote://pair?host=wss%3A%2F%2Fhost.example%3A443'
        '&token=t&fp=$_fpB64&mode=letsencrypt',
      );
      expect(p, isNotNull);
      expect(p!.mode, TlsMode.letsencrypt);
      expect(p.host, 'wss://host.example:443#fp=$_fpB64&mode=letsencrypt');
      // Both fields must come back off the fragment, and neither may leak
      // into a derived URL.
      expect(SettingsStore.fingerprintFrom(p.host), _fpB64);
      expect(SettingsStore.tlsModeFrom(p.host), TlsMode.letsencrypt);
      expect(SettingsStore.healthzUrl(p.host), 'https://host.example/healthz');
    });

    test('an explicit mode=selfsigned parses', () {
      final p = PairPayload.tryParse(
        'mcremote://pair?host=h%3A7531&token=t&fp=$_fpB64&mode=selfsigned',
      );
      expect(p, isNotNull);
      expect(p!.mode, TlsMode.selfsigned);
    });

    test('an unknown mode rejects the whole payload', () {
      // Defaulting here would mean guessing which acceptance rule to relax.
      for (final bad in <String>['acme', 'none', 'letsencrypt2', 'self signed']) {
        expect(
          PairPayload.tryParse(
            'mcremote://pair?host=h%3A7531&token=t'
            '&mode=${Uri.encodeQueryComponent(bad)}',
          ),
          isNull,
          reason: bad,
        );
      }
    });

    test('an empty mode= is treated as absent, not as unknown', () {
      // A blank parameter is the same signal as a missing one; only a value
      // that names something we do not implement is a refusal.
      final p = PairPayload.tryParse(
        'mcremote://pair?host=h%3A7531&token=t&mode=',
      );
      expect(p, isNotNull);
      expect(p!.mode, TlsMode.selfsigned);
    });

    test('an oversized mode is rejected before parsing', () {
      final big = 'a' * (PairPayload.maxModeLength + 1);
      expect(
        PairPayload.tryParse('mcremote://pair?host=h%3A7531&token=t&mode=$big'),
        isNull,
      );
    });
  });

  test('preserves wss:// (no silent downgrade to ws://)', () {
    final p = PairPayload.tryParse(
      'mcremote://pair?host=wss%3A%2F%2Fsecure.host%3A443&token=mcr_x',
    );
    expect(p, isNotNull);
    expect(p!.secure, isTrue);
    expect(p.hostAuthority, 'secure.host:443');
    // The host must round-trip through normalizeWsUrl as wss, not ws.
    expect(
      SettingsStore.normalizeWsUrl(p.host),
      'wss://secure.host:443/v1/ws',
    );
  });

  test('preserves https:// as a secure signal', () {
    final p = PairPayload.tryParse(
      'mcremote://pair?host=https%3A%2F%2Fsecure.host%3A443%2Fv1%2Fws&token=t',
    );
    expect(p, isNotNull);
    expect(p!.secure, isTrue);
    expect(
      SettingsStore.normalizeWsUrl(p.host),
      'wss://secure.host:443/v1/ws',
    );
  });

  test('rejects oversized payloads', () {
    final bigToken = 'a' * (PairPayload.maxTokenLength + 1);
    expect(
      PairPayload.tryParse('mcremote://pair?host=h%3A7531&token=$bigToken'),
      isNull,
    );

    final bigHost = 'h' * (PairPayload.maxHostLength + 1);
    expect(
      PairPayload.tryParse('mcremote://pair?host=$bigHost&token=t'),
      isNull,
    );

    final bigCode = 'K' * (PairPayload.maxCodeLength + 1);
    expect(
      PairPayload.tryParse('mcremote://pair?host=h%3A7531&code=$bigCode'),
      isNull,
    );

    // A 100k-char raw payload is rejected before any parsing work.
    expect(
      PairPayload.tryParse(
        'mcremote://pair?host=h%3A7531&token=${'a' * 100000}',
      ),
      isNull,
    );
    expect(PairPayload.looksLikePairCode('K' * 100000), isFalse);
  });

  test('accepts payloads at the size limits', () {
    final token = 'a' * PairPayload.maxTokenLength;
    final p = PairPayload.tryParse(
      'mcremote://pair?host=h%3A7531&token=$token',
    );
    expect(p, isNotNull);
    expect(p!.token, token);
  });

  test('looksLikePairCode and format', () {
    expect(PairPayload.looksLikePairCode('K7M2-9X4P'), isTrue);
    expect(PairPayload.looksLikePairCode('k7m29x4p'), isTrue);
    expect(PairPayload.looksLikePairCode('mcr_abc'), isFalse);
    expect(PairPayload.formatPairCode('k7m29x4p'), 'K7M2-9X4P');
  });
}

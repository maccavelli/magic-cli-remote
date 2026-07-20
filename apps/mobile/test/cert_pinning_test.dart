@TestOn('vm')
library;

import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/io_client.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/ws/mc_exception.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// A self-signed P-256 leaf produced by `internal/certs`, plus a second,
/// unrelated one. Together they let us drive the exact case pinning exists
/// for: a host that answers on the right address with the wrong identity.
const _certA = '''
-----BEGIN CERTIFICATE-----
MIIB/DCCAaKgAwIBAgIRAN5NFABi09Yr97dp1e9iC/kwCgYIKoZIzj0EAwIwJjER
MA8GA1UEChMIbWNyZW1vdGUxETAPBgNVBAMTCG1jcmVtb3RlMB4XDTI2MDcyMDAx
MTI1M1oXDTM2MDcxNzAyMTI1M1owJjERMA8GA1UEChMIbWNyZW1vdGUxETAPBgNV
BAMTCG1jcmVtb3RlMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEkvA76EbbgRKk
sR/inxlmOX9/3kBAU+vuDF32gJA3D5/dmwm6CQv5K4SiMtx1Ns3mvOEblMLaAKRE
pMcbKxLisqOBsDCBrTAOBgNVHQ8BAf8EBAMCAqQwEwYDVR0lBAwwCgYIKwYBBQUH
AwEwDwYDVR0TAQH/BAUwAwEB/zAdBgNVHQ4EFgQUVPhZhHKkgG0GeQXpOT31i4DU
Kn0wVgYDVR0RBE8wTYIKYXdzdXRpbGl0eYIJbG9jYWxob3N0hwQKCnothwRkQAAB
hwR/AAABhxAAAAAAAAAAAAAAAAAAAAABhxD9ehFcoeAAAAAAAAAAAAABMAoGCCqG
SM49BAMCA0gAMEUCIAqmW6YGO1OXwXgnGrZOvqkyD+ST9+VAp0Yl157YwgaeAiEA
oxcGdNiqe/vT+pSAsKl9BIbTH3UU0BSGc7dDaemKowE=
-----END CERTIFICATE-----
''';

const _keyA = '''
-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIGB3oZEd8f81ajOahNu3oYN6u7dw1ll6LdUNYeNsXr9poAoGCCqGSM49
AwEHoUQDQgAEkvA76EbbgRKksR/inxlmOX9/3kBAU+vuDF32gJA3D5/dmwm6CQv5
K4SiMtx1Ns3mvOEblMLaAKREpMcbKxLisg==
-----END EC PRIVATE KEY-----
''';

/// SHA-256 of _certA's DER, base64url — what the daemon prints into the QR.
const _fpA = 'Qe0O3GUSGnH0EzRTJC0Lo1mthKzPt_LUuymFymtngd0';

/// A different daemon's fingerprint. Never served in these tests.
const _fpB = 'YaLoAn6LtTyDz3E9u74B5VFROBwLQc_xDOx-xlCH_JE';

Future<HttpServer> _startTlsServer() async {
  final ctx = SecurityContext()
    ..useCertificateChainBytes(_certA.codeUnits)
    ..usePrivateKeyBytes(_keyA.codeUnits);
  final server = await HttpServer.bindSecure('127.0.0.1', 0, ctx);
  server.listen((req) async {
    req.response
      ..statusCode = 200
      ..write('ok');
    await req.response.close();
  });
  return server;
}

Future<String> _get(HttpClient httpClient, HttpServer server, String path) async {
  final client = IOClient(httpClient);
  try {
    final res = await client
        .get(Uri.parse('https://127.0.0.1:${server.port}$path'))
        .timeout(const Duration(seconds: 10));
    return res.body;
  } finally {
    client.close();
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late HttpServer server;

  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    // TestWidgetsFlutterBinding installs an HttpOverrides that stubs every
    // HttpClient with a 400 responder. Pinning is precisely the behaviour of a
    // real HttpClient against a real TLS handshake, so opt back out.
    HttpOverrides.global = null;
    server = await _startTlsServer();
  });

  tearDown(() async {
    await server.close(force: true);
  });

  group('CertPinner', () {
    test('accepts the certificate whose fingerprint was pinned', () async {
      final pinner = CertPinner(_fpA);
      expect(await _get(pinner.newHttpClient(), server, '/healthz'), 'ok');
      expect(pinner.mismatched, isFalse);
      expect(pinner.observed, _fpA);
    });

    test('rejects a certificate that does not match the pin', () async {
      final pinner = CertPinner(_fpB);
      await expectLater(
        _get(pinner.newHttpClient(), server, '/healthz'),
        throwsA(isA<Exception>()),
      );
      expect(pinner.mismatched, isTrue,
          reason: 'rejection must be attributed to the pin, not the transport');
      // The fingerprint actually seen is reported, so the user can compare it
      // with what the host prints.
      expect(pinner.observed, _fpA);
    });

    test('a mismatch translates to a permanent cert_mismatch error', () {
      final pinner = CertPinner(_fpB)..mismatched = true;
      final err = pinner.translate(
        const HandshakeException('rejected'),
        'wss://host/v1/ws',
      );
      expect(err, isA<McException>());
      final mc = err as McException;
      expect(mc.code, 'cert_mismatch');
      expect(mc.permanent, isTrue,
          reason: 'auto-reconnect must not keep dialling an unverified peer');
      expect(mc.message, contains(_fpB));
      expect(mc.message, contains('sha256:'));
    });

    test('an unpinned self-signed host fails closed, it is not accepted',
        () async {
      // No fingerprint: ordinary platform validation applies, and a
      // self-signed leaf has no chain to a system root.
      final pinner = CertPinner(null);
      await expectLater(
        _get(pinner.newHttpClient(), server, '/healthz'),
        throwsA(isA<Exception>()),
      );
      expect(pinner.mismatched, isFalse);

      final err = pinner.translate(
        const HandshakeException('unable to verify'),
        'https://host/healthz',
      );
      expect((err as McException).code, 'cert_unpinned');
      expect(err.permanent, isTrue);
    });

    test('fingerprintOf agrees with the daemon-computed digest', () async {
      final pinner = CertPinner(_fpA);
      await _get(pinner.newHttpClient(), server, '/healthz');
      // pinner.observed came from CertPinner.fingerprintOf over the DER the
      // server actually sent; _fpA came from Go's sha256 over the same DER.
      expect(pinner.observed, _fpA);
    });
  });

  group('McremoteClient.healthz pinning', () {
    test('succeeds with the fingerprint supplied inline', () async {
      final client = McremoteClient(settings: await _store());
      addTearDown(client.dispose);

      final body = await client.healthz(
        'https://127.0.0.1:${server.port}',
        fingerprint: _fpA,
      );
      expect(body, 'ok');
    });

    test('reuses a pin persisted by an earlier connection', () async {
      final store = await _store();
      final host = 'https://127.0.0.1:${server.port}';

      // First run learns the pin from the QR...
      final first = McremoteClient(settings: store);
      await first.healthz(host, fingerprint: _fpA);
      await first.dispose();

      // ...a later process restores it from storage with no QR in hand.
      final second = McremoteClient(settings: store);
      addTearDown(second.dispose);
      expect(await second.healthz(host), 'ok');
      expect(second.pinnedFingerprint, _fpA);
    });

    test('fails closed with cert_mismatch against the wrong certificate',
        () async {
      final client = McremoteClient(settings: await _store());
      addTearDown(client.dispose);

      await expectLater(
        client.healthz('https://127.0.0.1:${server.port}', fingerprint: _fpB),
        throwsA(
          isA<McException>()
              .having((e) => e.code, 'code', 'cert_mismatch')
              .having((e) => e.permanent, 'permanent', isTrue),
        ),
      );
    });

    test('picks up the fingerprint from a #fp= host fragment', () async {
      final client = McremoteClient(settings: await _store());
      addTearDown(client.dispose);

      // This is the shape the QR scan flow produces.
      final body =
          await client.healthz('https://127.0.0.1:${server.port}#fp=$_fpA');
      expect(body, 'ok');
      expect(client.pinnedFingerprint, _fpA);
    });

    test('does not carry a pin over to a different host', () async {
      final store = await _store();
      final client = McremoteClient(settings: store);
      addTearDown(client.dispose);

      await client.healthz('https://127.0.0.1:${server.port}', fingerprint: _fpA);
      expect(client.pinnedFingerprint, _fpA);

      // Another daemon on another port: the previous pin must be dropped
      // rather than applied to an identity it says nothing about.
      await expectLater(
        client.healthz('https://127.0.0.1:1/healthz'),
        throwsA(isA<Exception>()),
      );
      expect(client.pinnedFingerprint, isNull);
    });
  });

  // !! KEEP THIS GROUP LAST !!
  //
  // These tests add _certA to SecurityContext.defaultContext, which is
  // process-wide and cannot be undone within the isolate. Moving them above
  // 'an unpinned self-signed host fails closed' would make that test trust the
  // very certificate it is asserting gets rejected, and it would pass
  // vacuously. There is no per-client way to do this: an unpinned CertPinner
  // deliberately builds a bare HttpClient() so it uses the platform trust
  // store, which is the exact behaviour under test.
  group('letsencrypt mode (publicly trusted cert, no pin)', () {
    // In letsencrypt mode the daemon omits fp= from the pair QR, so the client
    // runs unpinned and must fall through to ordinary platform validation.
    // Trusting _certA as a root reproduces exactly that: a chain the platform
    // accepts, with no fingerprint involved.
    test('unpinned client accepts a chain the trust store validates', () async {
      SecurityContext.defaultContext
          .setTrustedCertificatesBytes(_certA.codeUnits);
      final server = await _startTlsServer();
      addTearDown(() => server.close(force: true));

      final pinner = CertPinner(null);
      expect(pinner.isPinned, isFalse);

      final body = await _get(pinner.newHttpClient(), server, '/healthz');
      expect(body, 'ok');
      // The failure path must not have been touched at all.
      expect(pinner.mismatched, isFalse);
    });

    test('a trusted chain never yields cert_unpinned', () async {
      SecurityContext.defaultContext
          .setTrustedCertificatesBytes(_certA.codeUnits);
      final server = await _startTlsServer();
      addTearDown(() => server.close(force: true));

      final pinner = CertPinner(null);
      Object? thrown;
      try {
        await _get(pinner.newHttpClient(), server, '/healthz');
      } catch (e) {
        thrown = e;
      }
      expect(thrown, isNull,
          reason: 'LE-mode pairing must not be rejected as cert_unpinned');
    });
  });
}

Future<SettingsStore> _store() async {
  return SettingsStore(
    secure: _InMemorySecureStorage(),
    prefs: await SharedPreferences.getInstance(),
    allowPlaintextFallback: false,
  );
}

class _InMemorySecureStorage implements FlutterSecureStorage {
  final _values = <String, String>{};

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

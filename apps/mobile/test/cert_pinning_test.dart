@TestOn('vm')
library;

import 'dart:io';

import 'package:basic_utils/basic_utils.dart' show CryptoUtils;
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/io_client.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/ws/client_identity.dart';
import 'package:magic_cli_remote/data/ws/mc_exception.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'support/test_certs.dart';

// The certificate fixtures live in test/support/test_certs.dart so the
// dial-failure suite can drive the same wrong-identity case.
const _certA = certA;
const _keyA = keyA;
const _fpA = fpA;
const _fpB = fpB;

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

/// A server that requests a client certificate and reports whichever one the
/// peer presented, so a test can prove the client certificate rode the same
/// connection as the server pin.
///
/// The daemon's real `RequestClientCert` completes the handshake for any
/// client cert and verifies the key at the protocol layer; dart:io's server,
/// by contrast, drops a client cert it cannot chain-validate, so [clientTrust]
/// (the client's own self-signed cert) is added as a trusted authority to
/// reproduce "handshake completes, cert captured".
Future<HttpServer> _startMtlsServer(
  void Function(X509Certificate?) onClientCert, {
  List<int>? clientTrust,
}) async {
  final ctx = SecurityContext()
    ..useCertificateChainBytes(_certA.codeUnits)
    ..usePrivateKeyBytes(_keyA.codeUnits);
  if (clientTrust != null) {
    ctx.setTrustedCertificatesBytes(clientTrust);
  }
  final server = await HttpServer.bindSecure(
    '127.0.0.1',
    0,
    ctx,
    requestClientCertificate: true,
  );
  server.listen((req) async {
    onClientCert(req.certificate);
    req.response
      ..statusCode = 200
      ..write('ok');
    await req.response.close();
  });
  return server;
}

Future<String> _get(
  HttpClient httpClient,
  HttpServer server,
  String path,
) async {
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
      expect(
        pinner.mismatched,
        isTrue,
        reason: 'rejection must be attributed to the pin, not the transport',
      );
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
      expect(
        mc.permanent,
        isTrue,
        reason: 'auto-reconnect must not keep dialling an unverified peer',
      );
      expect(mc.message, contains(_fpB));
      expect(mc.message, contains('sha256:'));
    });

    test(
      'an unpinned self-signed host fails closed, it is not accepted',
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
      },
    );

    test('fingerprintOf agrees with the daemon-computed digest', () async {
      final pinner = CertPinner(_fpA);
      await _get(pinner.newHttpClient(), server, '/healthz');
      // pinner.observed came from CertPinner.fingerprintOf over the DER the
      // server actually sent; _fpA came from Go's sha256 over the same DER.
      expect(pinner.observed, _fpA);
    });
  });

  group('client certificate rides the same SecurityContext as the pin', () {
    test('the presented client cert is exactly this device\'s cert, and the '
        'server pin is still enforced on the same connection', () async {
      final identity = ClientIdentity.generate();
      X509Certificate? seen;
      final mtls = await _startMtlsServer(
        (c) => seen = c,
        clientTrust: identity.certPem.codeUnits,
      );
      addTearDown(() => mtls.close(force: true));

      // Self-signed server pin AND a client identity, both on one context.
      final pinner = CertPinner(
        _fpA,
        mode: TlsMode.selfsigned,
        identity: identity,
      );
      expect(await _get(pinner.newHttpClient(), mtls, '/healthz'), 'ok');

      // The server pin ran on this connection...
      expect(
        pinner.observed,
        _fpA,
        reason: 'the server cert was pinned on the same socket',
      );
      // ...and the daemon captured *this* device's client certificate on it.
      expect(
        seen,
        isNotNull,
        reason: 'the client certificate must be presented, not dropped',
      );
      final presentedDer = seen!.der;
      final ourDer = CryptoUtils.getBytesFromPEMString(identity.certPem);
      expect(
        presentedDer,
        orderedEquals(ourDer),
        reason: 'the presented cert must be the one we generated',
      );
    });

    test(
      'a mismatched server pin still fails hard even with a client cert',
      () async {
        final identity = ClientIdentity.generate();
        final mtls = await _startMtlsServer((_) {});
        addTearDown(() => mtls.close(force: true));

        // Attaching a client identity must not soften server pinning.
        final pinner = CertPinner(
          _fpB,
          mode: TlsMode.selfsigned,
          identity: identity,
        );
        await expectLater(
          _get(pinner.newHttpClient(), mtls, '/healthz'),
          throwsA(isA<Exception>()),
        );
        expect(pinner.mismatched, isTrue);
      },
    );
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

    test(
      'fails closed with cert_mismatch against the wrong certificate',
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
      },
    );

    test('picks up the fingerprint from a #fp= host fragment', () async {
      final client = McremoteClient(settings: await _store());
      addTearDown(client.dispose);

      // This is the shape the QR scan flow produces.
      final body = await client.healthz(
        'https://127.0.0.1:${server.port}#fp=$_fpA',
      );
      expect(body, 'ok');
      expect(client.pinnedFingerprint, _fpA);
    });

    test('does not carry a pin over to a different host', () async {
      final store = await _store();
      final client = McremoteClient(settings: store);
      addTearDown(client.dispose);

      await client.healthz(
        'https://127.0.0.1:${server.port}',
        fingerprint: _fpA,
      );
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

  group('pins keyed on device identity, not the dialled address', () {
    test('a pin survives the host moving to a new tailnet IP', () async {
      final store = await _store();
      await store.setDeviceId('dev-abc');

      // Paired at one address...
      final first = McremoteClient(settings: store)..deviceId = 'dev-abc';
      await first.healthz(
        'https://127.0.0.1:${server.port}',
        fingerprint: _fpA,
      );
      await first.dispose();

      // ...the node is re-registered and comes back on a different address
      // with the same certificate. An address-keyed pin would miss here, fall
      // through to the unpinned path and tell the user to re-pair.
      final moved = await _startTlsServer();
      addTearDown(() => moved.close(force: true));

      final second = McremoteClient(settings: store)..deviceId = 'dev-abc';
      addTearDown(second.dispose);
      expect(await second.healthz('https://127.0.0.1:${moved.port}'), 'ok');
      expect(second.pinnedFingerprint, _fpA);
    });

    test(
      'the same identity with a different certificate is a hard mismatch',
      () async {
        final store = await _store();
        await store.setDeviceId('dev-abc');
        await store.setFingerprint(
          'https://127.0.0.1:${server.port}',
          _fpB,
          deviceId: 'dev-abc',
        );

        // The asymmetry that keeps pinning meaningful: an *absent* pin may fall
        // through to platform validation, but a pin that is present and does not
        // match must stay a loud, permanent failure. Softening this one turns
        // pinning into trust-on-every-reconnect.
        final client = McremoteClient(settings: store)..deviceId = 'dev-abc';
        addTearDown(client.dispose);

        await expectLater(
          client.healthz('https://127.0.0.1:${server.port}'),
          throwsA(
            isA<McException>()
                .having((e) => e.code, 'code', 'cert_mismatch')
                .having((e) => e.permanent, 'permanent', isTrue),
          ),
        );
      },
    );

    test('a mismatch is still hard after the address changes', () async {
      final store = await _store();
      await store.setDeviceId('dev-abc');
      await store.setFingerprint(
        'https://127.0.0.1:${server.port}',
        _fpB,
        deviceId: 'dev-abc',
      );

      final moved = await _startTlsServer();
      addTearDown(() => moved.close(force: true));

      final client = McremoteClient(settings: store)..deviceId = 'dev-abc';
      addTearDown(client.dispose);

      await expectLater(
        client.healthz('https://127.0.0.1:${moved.port}'),
        throwsA(
          isA<McException>().having((e) => e.code, 'code', 'cert_mismatch'),
        ),
      );
    });
  });

  // The ACME-failure half of letsencrypt mode. These deliberately run *before*
  // the trust-store group below: the certificate served here must still be
  // untrusted, which is exactly the state the daemon's self-signed fallback
  // leaves the client in.
  group('letsencrypt mode, chain fails (the self-signed fallback)', () {
    test('accepts an untrusted chain when the pin matches', () async {
      // ACME failed, the daemon fell back to its self-signed leaf, and the
      // pair QR carried that leaf's fingerprint. Before ADR 0004 this was
      // permanently unrecoverable; the pin is what makes it survivable.
      final pinner = CertPinner(_fpA, mode: TlsMode.letsencrypt);
      expect(await _get(pinner.newHttpClient(), server, '/healthz'), 'ok');
      expect(pinner.observed, _fpA);
      expect(pinner.mismatched, isFalse);
    });

    test('rejects an untrusted chain when the pin does not match', () async {
      // Neither limb of the `or` holds. The `or` must not degrade into
      // accepting anything the chain happens to fail on.
      final pinner = CertPinner(_fpB, mode: TlsMode.letsencrypt);
      await expectLater(
        _get(pinner.newHttpClient(), server, '/healthz'),
        throwsA(isA<Exception>()),
      );
      expect(pinner.mismatched, isTrue);
      expect(pinner.observed, _fpA);

      final err = pinner.translate(
        const HandshakeException('rejected'),
        'wss://host/v1/ws',
      );
      expect((err as McException).code, 'cert_mismatch');
      expect(
        err.permanent,
        isTrue,
        reason: 'letsencrypt mode must not soften a pin mismatch',
      );
    });

    test(
      'the client surfaces cert_mismatch end to end in letsencrypt mode',
      () async {
        final client = McremoteClient(settings: await _store());
        addTearDown(client.dispose);

        await expectLater(
          client.healthz(
            'https://127.0.0.1:${server.port}',
            fingerprint: _fpB,
            mode: TlsMode.letsencrypt,
          ),
          throwsA(
            isA<McException>()
                .having((e) => e.code, 'code', 'cert_mismatch')
                .having((e) => e.permanent, 'permanent', isTrue),
          ),
        );
      },
    );

    test(
      'the mode is persisted with the pin and restored on reconnect',
      () async {
        final store = await _store();
        final host = 'https://127.0.0.1:${server.port}';

        // Paired in letsencrypt mode against the fallback certificate...
        final first = McremoteClient(settings: store);
        await first.healthz(host, fingerprint: _fpA, mode: TlsMode.letsencrypt);
        await first.dispose();

        // ...and after process death the rule comes back with the pin. Reading
        // one without the other would apply the wrong rule to a correct pin.
        final second = McremoteClient(settings: store);
        addTearDown(second.dispose);
        expect(await second.healthz(host), 'ok');
        expect(second.pinnedFingerprint, _fpA);
        expect(second.tlsMode, TlsMode.letsencrypt);
      },
    );

    test('a mode= host fragment selects the rule', () async {
      final client = McremoteClient(settings: await _store());
      addTearDown(client.dispose);

      // The shape the QR scan flow produces for a letsencrypt daemon.
      final body = await client.healthz(
        'https://127.0.0.1:${server.port}#fp=$_fpA&mode=letsencrypt',
      );
      expect(body, 'ok');
      expect(client.pinnedFingerprint, _fpA);
      expect(client.tlsMode, TlsMode.letsencrypt);
    });
  });

  // In letsencrypt mode the daemon serves a publicly-trusted chain, so the
  // client validates against roots rather than a pin. Since the client
  // certificate (ADR 0005) forces every mode onto an explicit SecurityContext,
  // these tests inject _certA as a trusted root into the pinner's *own* context
  // (the `trustedRootsPem` seam) rather than mutating the process-wide
  // SecurityContext.defaultContext — so the group is isolated and no longer
  // order-sensitive.
  group('letsencrypt mode (publicly trusted cert, no pin)', () {
    final trusted = _certA.codeUnits;

    test('unpinned client accepts a chain the trust store validates', () async {
      final pinner = CertPinner(
        null,
        mode: TlsMode.letsencrypt,
        trustedRootsPem: trusted,
      );
      expect(pinner.isPinned, isFalse);

      final body = await _get(pinner.newHttpClient(), server, '/healthz');
      expect(body, 'ok');
      // The failure path must not have been touched at all.
      expect(pinner.mismatched, isFalse);
    });

    test('a trusted chain never yields cert_unpinned', () async {
      final pinner = CertPinner(
        null,
        mode: TlsMode.letsencrypt,
        trustedRootsPem: trusted,
      );
      Object? thrown;
      try {
        await _get(pinner.newHttpClient(), server, '/healthz');
      } catch (e) {
        thrown = e;
      }
      expect(
        thrown,
        isNull,
        reason: 'LE-mode pairing must not be rejected as cert_unpinned',
      );
    });

    // The renewal case, and the reason the rule is `or` and not `and`.
    test(
      'letsencrypt mode accepts a trusted chain despite a stale pin',
      () async {
        // ACME renewed 60 days in; the pin from pairing is now the *previous*
        // leaf. The chain still validates, so the stale pin is not load-bearing
        // and the callback is never reached.
        final pinner = CertPinner(
          _fpB,
          mode: TlsMode.letsencrypt,
          trustedRootsPem: trusted,
        );
        expect(await _get(pinner.newHttpClient(), server, '/healthz'), 'ok');
        expect(
          pinner.mismatched,
          isFalse,
          reason: 'a validating chain must not consult the pin at all',
        );
        expect(pinner.observed, isNull);
      },
    );

    test(
      'selfsigned mode still rejects a trusted chain with the wrong pin',
      () async {
        // Pin-only is not weakened by the letsencrypt rule existing: with
        // `withTrustedRoots: false` and no injected root the platform's opinion
        // is irrelevant, so even a certificate a chain-validator would accept is
        // rejected on a pin miss.
        final pinner = CertPinner(_fpB, mode: TlsMode.selfsigned);
        await expectLater(
          _get(pinner.newHttpClient(), server, '/healthz'),
          throwsA(isA<Exception>()),
        );
        expect(pinner.mismatched, isTrue);
        expect(pinner.observed, _fpA);
      },
    );

    test(
      'selfsigned mode accepts its pin regardless of the trust store',
      () async {
        final pinner = CertPinner(_fpA, mode: TlsMode.selfsigned);
        expect(await _get(pinner.newHttpClient(), server, '/healthz'), 'ok');
        expect(
          pinner.observed,
          _fpA,
          reason: 'every certificate is routed through the pin check',
        );
      },
    );
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

import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/ws/jws.dart';
import 'package:pointycastle/export.dart';

BigInt _bigIntFromHex(String hex) => BigInt.parse(hex, radix: 16);

// RFC 7515 Appendix A.3's published ES256 example — the exact same vector
// internal/receipt/jws_test.go reproduces on the Go side (cryptographically
// re-verified against the RFC's own signing input before being hardcoded on
// either platform; see MADR 0077 PLAN P2's grounding notes). Both platforms
// verify independently, agreeing on a format neither is copying from the
// other's implementation, only from the shared published spec.
const _xHex =
    '7fcdce2770f6c45d4183cbee6fdb4b7b580733357be9ef13bacf6e3c7bd15445';
const _yHex =
    'c7f144cd1bbd9b7e872cdfedb9eeb9f4b3695d6ea90b24ad8a4623288588e5ad';
const _dHex =
    '8e9b109e719098bf980487df1f5d77e9cb29606ebed2263b5f57c213df84f4b2';

const _rfcCompact =
    'eyJhbGciOiJFUzI1NiJ9'
    '.eyJpc3MiOiJqb2UiLA0KICJleHAiOjEzMDA4MTkzODAsDQogImh0dHA6Ly9leGFtcGxlLmNvbS9pc19yb290Ijp0cnVlfQ'
    '.DtEhU3ljbEg8L38VWAfUAqOyKAM6-Xx-F4GawxaepmXFCgfTjDxw5djxLa8ISlSApmWQxfKTUJqPP3-Kg6NU1Q';
final Uint8List _rfcPayload = Uint8List.fromList(
  utf8.encode(
    '{"iss":"joe",\r\n "exp":1300819380,\r\n "http://example.com/is_root":true}',
  ),
);

ECPublicKey _rfcPublicKey() {
  final domain = ECDomainParameters('prime256v1');
  final q = domain.curve.createPoint(
    _bigIntFromHex(_xHex),
    _bigIntFromHex(_yHex),
  );
  return ECPublicKey(q, domain);
}

ECPrivateKey _rfcPrivateKey() {
  final domain = ECDomainParameters('prime256v1');
  return ECPrivateKey(_bigIntFromHex(_dHex), domain);
}

AsymmetricKeyPair<ECPublicKey, ECPrivateKey> _generateKeyPair() {
  final domain = ECDomainParameters('prime256v1');
  final keyGen = ECKeyGenerator();
  final seedSource = Random.secure();
  final secureRandom = FortunaRandom()
    ..seed(
      KeyParameter(
        Uint8List.fromList(
          List<int>.generate(32, (_) => seedSource.nextInt(256)),
        ),
      ),
    );
  keyGen.init(
    ParametersWithRandom(ECKeyGeneratorParameters(domain), secureRandom),
  );
  return keyGen.generateKeyPair();
}

// Signed by internal/receipt/jws.go's SignES256Compact using the RFC 7515
// A.3 private key (see _dHex above) over the payload
// {"interop":"go-to-dart"} — captured once by running the Go signer, not
// re-derivable at test time since ECDSA signing is randomized. Proves the
// Dart verifier accepts a real Go-produced signature, not just ones Dart
// itself produced (MADR 0077 PLAN P2's cross-platform interop test).
const _goProducedCompact =
    'eyJhbGciOiJFUzI1NiJ9'
    '.eyJpbnRlcm9wIjoiZ28tdG8tZGFydCJ9'
    '.l9Z1Dc2ipXng42crepwmXiKTMDOAuYQiY6VGh5rZVqENkUpsyoq4FbhD6yXinp-NSKBg6TK_-rtamYgwaMrruQ';

void main() {
  test('accepts a Go-produced signature', () {
    final payload = verifyEs256Compact(_rfcPublicKey(), _goProducedCompact);
    expect(payload, isNotNull);
    expect(utf8.decode(payload!), '{"interop":"go-to-dart"}');
  });

  test('verifies the RFC 7515 Appendix A.3 published signature', () {
    final payload = verifyEs256Compact(_rfcPublicKey(), _rfcCompact);
    expect(payload, isNotNull);
    expect(utf8.decode(payload!), utf8.decode(_rfcPayload));
  });

  test('round-trips with a freshly generated key', () {
    final pair = _generateKeyPair();
    final payload = Uint8List.fromList(utf8.encode('{"hello":"world"}'));
    final compact = signEs256Compact(pair.privateKey, payload);
    final got = verifyEs256Compact(pair.publicKey, compact);
    expect(got, isNotNull);
    expect(utf8.decode(got!), '{"hello":"world"}');
  });

  test('round-trips with the RFC key', () {
    final priv = _rfcPrivateKey();
    final pub = _rfcPublicKey();
    final payload = Uint8List.fromList(utf8.encode('{"a":1}'));
    final compact = signEs256Compact(priv, payload);
    final got = verifyEs256Compact(pub, compact);
    expect(got, isNotNull);
    expect(utf8.decode(got!), '{"a":1}');
  });

  test('rejects a tampered payload', () {
    final pair = _generateKeyPair();
    final compact = signEs256Compact(
      pair.privateKey,
      Uint8List.fromList(utf8.encode('{"a":1}')),
    );
    final parts = compact.split('.');
    final tampered = '${parts[0]}.${parts[1]}AAAA.${parts[2]}';
    expect(verifyEs256Compact(pair.publicKey, tampered), isNull);
  });

  test('rejects a tampered signature', () {
    final pair = _generateKeyPair();
    final compact = signEs256Compact(
      pair.privateKey,
      Uint8List.fromList(utf8.encode('{"a":1}')),
    );
    final tampered = '${compact.substring(0, compact.length - 1)}A';
    expect(verifyEs256Compact(pair.publicKey, tampered), isNull);
  });

  test('rejects the wrong public key', () {
    final pairA = _generateKeyPair();
    final pairB = _generateKeyPair();
    final compact = signEs256Compact(
      pairA.privateKey,
      Uint8List.fromList(utf8.encode('{"a":1}')),
    );
    expect(verifyEs256Compact(pairB.publicKey, compact), isNull);
  });

  test('rejects malformed compact strings', () {
    final pub = _rfcPublicKey();
    for (final s in <String>[
      'abc.def',
      'a.b.c.d',
      '!!!.eyJhIjoxfQ.sig',
      'eyJhbGciOiJFUzI1NiJ9.!!!.sig',
      'eyJhbGciOiJFUzI1NiJ9.eyJhIjoxfQ.!!!',
      'eyJhbGciOiJIUzI1NiJ9.eyJhIjoxfQ.AAAA',
    ]) {
      expect(verifyEs256Compact(pub, s), isNull, reason: s);
    }
  });
}

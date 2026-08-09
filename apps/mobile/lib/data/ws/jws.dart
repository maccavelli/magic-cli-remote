import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:crypto/crypto.dart' as crypto;
import 'package:pointycastle/export.dart';

/// ES256 (RFC 7518 §3.4) JWS Compact Serialization (RFC 7515 §3.1) — the
/// Dart mirror of internal/receipt/jws.go, sharing one correctness bar (RFC
/// 7515 Appendix A.3) with the Go side (MADR 0077 D7). No JWS library is
/// used on either side: `ECDSASigner`/`ECSignature` expose the raw `r`/`s`
/// `BigInt` values directly, the same raw-value API `client_identity.dart`
/// already relies on for this device's own key.

/// The fixed JWS Protected Header this module produces and accepts. No
/// "kid": device/daemon identity is established out-of-band via the
/// enrolled-cert lookup (MADR 0077 D9), not a JWS header field, which would
/// be one more place to spoof.
const _jwsHeader = '{"alg":"ES256"}';
final String _jwsHeaderB64 = _encodeB64Url(utf8.encode(_jwsHeader));

String _encodeB64Url(List<int> bytes) =>
    base64Url.encode(bytes).replaceAll('=', '');

/// base64Url.decode requires canonical padding; base64 also rejects the
/// standard alphabet's +/ here, which is what we want for a URL-safe field
/// (same idiom as pair_uri.dart's `_tryDecodeB64Url`).
Uint8List? _decodeB64Url(String s) {
  final padded = s.padRight((s.length + 3) & ~3, '=');
  try {
    return base64Url.decode(padded);
  } on FormatException {
    return null;
  }
}

/// Big-endian, left-zero-padded to exactly [length] bytes — the ES256
/// signature encoding RFC 7518 §3.4 requires (raw R||S, not ASN.1 DER).
/// Mirrors Go's `big.Int.FillBytes`.
void _writeFixedBigEndian(Uint8List out, int offset, int length, BigInt value) {
  var v = value;
  for (var i = length - 1; i >= 0; i--) {
    out[offset + i] = (v & BigInt.from(0xff)).toInt();
    v = v >> 8;
  }
}

BigInt _readBigEndian(Uint8List bytes) {
  var result = BigInt.zero;
  for (final b in bytes) {
    result = (result << 8) | BigInt.from(b);
  }
  return result;
}

/// A P-256 `SecureRandom` seeded from `dart:math`'s cryptographically secure
/// generator — pointycastle does not seed one for you.
SecureRandom _seededSecureRandom() {
  final rng = FortunaRandom();
  final seedSource = Random.secure();
  final seed = Uint8List.fromList(
    List<int>.generate(32, (_) => seedSource.nextInt(256)),
  );
  rng.seed(KeyParameter(seed));
  return rng;
}

/// Signs [payload] as a JWS in Compact Serialization using ES256, matching
/// `internal/receipt/jws.go`'s `SignES256Compact` byte for byte: the
/// signature is the raw 64-byte R||S concatenation, big-endian, 32 bytes
/// each — never ASN.1 DER.
String signEs256Compact(ECPrivateKey priv, Uint8List payload) {
  final payloadB64 = _encodeB64Url(payload);
  final signingInput = '$_jwsHeaderB64.$payloadB64';
  final digest = Uint8List.fromList(
    crypto.sha256.convert(utf8.encode(signingInput)).bytes,
  );

  final signer = ECDSASigner(null, null)
    ..init(
      true,
      ParametersWithRandom(PrivateKeyParameter(priv), _seededSecureRandom()),
    );
  final sig = signer.generateSignature(digest) as ECSignature;

  final sigBytes = Uint8List(64);
  _writeFixedBigEndian(sigBytes, 0, 32, sig.r);
  _writeFixedBigEndian(sigBytes, 32, 32, sig.s);

  return '$signingInput.${_encodeB64Url(sigBytes)}';
}

/// Verifies [compact] against [pub] and, on success, returns the decoded
/// payload bytes; `null` on any failure — malformed input, wrong part count,
/// an `alg` other than exactly `ES256` (no algorithm negotiation), a
/// wrong-length signature, or a signature that does not verify. Used both as
/// a generic ES256 verifier (the interop test) and, at P7, for the phone's
/// own structural sanity-check of a daemon-sent Statement before signing it —
/// though that check has no matching key to verify a signature against, so
/// it validates JSON shape only, not via this function.
Uint8List? verifyEs256Compact(ECPublicKey pub, String compact) {
  final parts = compact.split('.');
  if (parts.length != 3) return null;

  final headerBytes = _decodeB64Url(parts[0]);
  final payload = _decodeB64Url(parts[1]);
  final sig = _decodeB64Url(parts[2]);
  if (headerBytes == null || payload == null || sig == null) return null;
  if (utf8.decode(headerBytes, allowMalformed: true) != _jwsHeader) return null;
  if (sig.length != 64) return null;

  final r = _readBigEndian(sig.sublist(0, 32));
  final s = _readBigEndian(sig.sublist(32, 64));

  final signingInput = '${parts[0]}.${parts[1]}';
  final digest = Uint8List.fromList(
    crypto.sha256.convert(utf8.encode(signingInput)).bytes,
  );

  final verifier = ECDSASigner(null, null)
    ..init(false, PublicKeyParameter(pub));
  final ok = verifier.verifySignature(digest, ECSignature(r, s));
  return ok ? payload : null;
}

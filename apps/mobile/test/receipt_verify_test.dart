import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:crypto/crypto.dart' as crypto;
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/ws/jws.dart';
import 'package:magic_cli_remote/data/ws/receipts.dart';
import 'package:pointycastle/export.dart';

// verifyChainEntry re-derives a receipt's verdict on device (MADR 0078 D9).
// These tests mirror the Go Store.Verify key-selection and the jws_test.dart
// tamper idiom (flip a decoded byte, never a trailing base64 char).

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

String _flipPayloadByte(String compact) {
  final parts = compact.split('.');
  final padded = parts[1].padRight((parts[1].length + 3) & ~3, '=');
  final raw = Uint8List.fromList(base64Url.decode(padded));
  raw[0] ^= 0xFF;
  parts[1] = base64Url.encode(raw).replaceAll('=', '');
  return parts.join('.');
}

Map<String, dynamic> _permissionStatement(String device) => {
  '_type': 'https://mcremote.dev/attestations/receipt/v1',
  'subject': [
    {
      'name': 'session:s1/permission:p1',
      'digest': {'sha256': 'ab'},
    },
  ],
  'predicateType': kPermissionDecisionPredicate,
  'predicate': {'device_id': device, 'option_id': 'once', 'tool_name': 'bash'},
  'chain': {'scope': 'device:$device', 'prev_sha256': null},
};

Map<String, dynamic> _handoffReleaseStatement(String device) => {
  '_type': 'https://mcremote.dev/attestations/receipt/v1',
  'subject': [
    {
      'name': 'session:s1/handoff:n1',
      'digest': {'sha256': 'ab'},
    },
  ],
  'predicateType': kHandoffReleasePredicate,
  'predicate': {'session_id': 's1', 'from_device_id': device},
  'chain': {'scope': 'device:$device', 'prev_sha256': null},
};

String _sign(ECPrivateKey priv, Map<String, dynamic> statement) {
  return signEs256Compact(priv, utf8.encode(json.encode(statement)));
}

void main() {
  test('a device-signed permission entry verifies against the device key', () {
    final pair = _generateKeyPair();
    final stmt = _permissionStatement('dev-1');
    final jws = _sign(pair.privateKey, stmt);
    expect(
      verifyChainEntry(jws, stmt, pair.publicKey),
      ReceiptVerdict.verified,
    );
  });

  test('a handoff-release entry verifies against the device key', () {
    final pair = _generateKeyPair();
    final stmt = _handoffReleaseStatement('dev-1');
    final jws = _sign(pair.privateKey, stmt);
    expect(
      verifyChainEntry(jws, stmt, pair.publicKey),
      ReceiptVerdict.verified,
    );
  });

  test('a tampered (byte-flipped) entry fails verification', () {
    final pair = _generateKeyPair();
    final stmt = _permissionStatement('dev-1');
    final jws = _sign(pair.privateKey, stmt);
    final tampered = _flipPayloadByte(jws);
    // The statement we display is the original; the signature no longer
    // matches those bytes → failed, never a false pass.
    expect(
      verifyChainEntry(tampered, stmt, pair.publicKey),
      ReceiptVerdict.failed,
    );
  });

  test('a valid signature over DIFFERENT bytes than shown fails', () {
    final pair = _generateKeyPair();
    final signedStmt = _permissionStatement('dev-1');
    final jws = _sign(pair.privateKey, signedStmt);
    // Display a statement that differs from what was signed.
    final shownStmt = _permissionStatement('dev-1')
      ..['predicate'] = {'device_id': 'dev-1', 'option_id': 'always'};
    expect(
      verifyChainEntry(jws, shownStmt, pair.publicKey),
      ReceiptVerdict.failed,
    );
  });

  test('a wrong device key fails verification', () {
    final signer = _generateKeyPair();
    final other = _generateKeyPair();
    final stmt = _permissionStatement('dev-1');
    final jws = _sign(signer.privateKey, stmt);
    expect(verifyChainEntry(jws, stmt, other.publicKey), ReceiptVerdict.failed);
  });

  test('an unknown predicateType is unverifiable, not a crash', () {
    final pair = _generateKeyPair();
    final stmt = _permissionStatement('dev-1')
      ..['predicateType'] = 'https://example.com/unknown/v1';
    final jws = _sign(pair.privateKey, stmt);
    expect(
      verifyChainEntry(jws, stmt, pair.publicKey),
      ReceiptVerdict.unverifiable,
    );
  });

  test('a receipt-unavailable marker without a daemon key is unverifiable', () {
    final pair = _generateKeyPair();
    final stmt = _permissionStatement('dev-1')
      ..['predicateType'] = kReceiptUnavailablePredicate;
    final jws = _sign(pair.privateKey, stmt);
    // No daemonKey passed → cannot check, must not claim a pass.
    expect(
      verifyChainEntry(jws, stmt, pair.publicKey),
      ReceiptVerdict.unverifiable,
    );
  });

  group('verifyChainLinks', () {
    String hashHex(String s) {
      final d = crypto.sha256.convert(utf8.encode(s)).bytes;
      return d.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
    }

    ReceiptEntry entry(String jws, String? prev) => ReceiptEntry(
      jws: jws,
      statement: {
        'chain': {'prev_sha256': prev},
      },
      verdict: ReceiptVerdict.verified,
    );

    test('an intact two-link chain returns -1', () {
      final e0 = entry('line0', null);
      final e1 = entry('line1', hashHex('line0'));
      expect(verifyChainLinks([e0, e1]), -1);
    });

    test('a broken link is reported at its index', () {
      final e0 = entry('line0', null);
      final e1 = entry('line1', hashHex('WRONG'));
      expect(verifyChainLinks([e0, e1]), 1);
    });

    test('a first entry that claims a predecessor is broken at 0', () {
      final e0 = entry('line0', hashHex('phantom'));
      expect(verifyChainLinks([e0]), 0);
    });

    test('an empty chain is intact', () {
      expect(verifyChainLinks(const []), -1);
    });
  });
}

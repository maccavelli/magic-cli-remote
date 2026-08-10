import 'dart:convert';
import 'dart:typed_data';

import 'package:crypto/crypto.dart' as crypto;
import 'package:pointycastle/export.dart' show ECPublicKey;

import 'jws.dart';

/// Predicate types the phone understands (mirrors internal/receipt and MADR
/// 0078 D4). Device-signed kinds verify against the device's own key;
/// receipt-unavailable markers are daemon-signed.
const kPermissionDecisionPredicate =
    'https://mcremote.dev/attestations/permission-decision/v1';
const kReceiptUnavailablePredicate =
    'https://mcremote.dev/attestations/receipt-unavailable/v1';
const kHandoffReleasePredicate =
    'https://mcremote.dev/attestations/session-handoff-release/v1';
const kHandoffClaimPredicate =
    'https://mcremote.dev/attestations/session-handoff-claim/v1';

/// The locally-recomputed verdict for one chain entry (MADR 0078 D9): the
/// phone never trusts a daemon-asserted status, it re-derives it.
enum ReceiptVerdict {
  /// Signature verified against the expected key.
  verified,

  /// A signature was checked and did NOT verify — tampering or a wrong key.
  failed,

  /// Could not be checked here: an unknown predicateType, or a daemon-signed
  /// marker with no pinned daemon key available. Shown as a caution, never as
  /// a pass or a fail.
  unverifiable,
}

/// One entry of this device's receipt chain, as returned by `receipts.list`
/// and re-verified locally.
class ReceiptEntry {
  const ReceiptEntry({
    required this.jws,
    required this.statement,
    required this.verdict,
  });

  /// The raw JWS compact string the daemon stored.
  final String jws;

  /// The decoded Statement (`_type`, `subject`, `predicateType`, `predicate`,
  /// `chain`). Never null — an entry whose statement will not decode is
  /// dropped before this type is built.
  final Map<String, dynamic> statement;

  /// The locally-recomputed verdict.
  final ReceiptVerdict verdict;

  String get predicateType => statement['predicateType'] as String? ?? '';

  String get subjectName {
    final subject = statement['subject'];
    if (subject is List && subject.isNotEmpty) {
      final first = subject.first;
      if (first is Map) return first['name'] as String? ?? '';
    }
    return '';
  }

  Map<String, dynamic> get predicate {
    final p = statement['predicate'];
    return p is Map ? Map<String, dynamic>.from(p) : const {};
  }

  String? get prevSha256 {
    final chain = statement['chain'];
    if (chain is Map) return chain['prev_sha256'] as String?;
    return null;
  }
}

/// verifyChainEntry re-derives an entry's [ReceiptVerdict] locally (MADR 0078
/// D9): a device-signed kind is checked against [deviceKey]; a
/// receipt-unavailable marker against [daemonKey] when one is pinned
/// (otherwise unverifiable, never a false pass); an unknown predicateType is
/// unverifiable. [jws] must be the compact string; [statement] its decoded
/// form (they are cross-checked by re-decoding the JWS payload).
ReceiptVerdict verifyChainEntry(
  String jws,
  Map<String, dynamic> statement,
  ECPublicKey deviceKey, {
  ECPublicKey? daemonKey,
}) {
  final predicateType = statement['predicateType'];
  ECPublicKey? key;
  switch (predicateType) {
    case kPermissionDecisionPredicate:
    case kHandoffReleasePredicate:
    case kHandoffClaimPredicate:
      key = deviceKey;
      break;
    case kReceiptUnavailablePredicate:
      // Daemon-signed. Without a pinned daemon key we cannot check it — say so
      // rather than claim a pass.
      if (daemonKey == null) return ReceiptVerdict.unverifiable;
      key = daemonKey;
      break;
    default:
      return ReceiptVerdict.unverifiable;
  }

  final payload = verifyEs256Compact(key, jws);
  if (payload == null) return ReceiptVerdict.failed;

  // The signed payload must be the statement we're displaying — a valid
  // signature over different bytes than what we show is still a failure (the
  // daemon's own D2 check, from the reader's side).
  if (!_jsonSemanticallyEqual(payload, statement)) {
    return ReceiptVerdict.failed;
  }
  return ReceiptVerdict.verified;
}

/// verifyChainLinks walks [entries] oldest→newest confirming each
/// `chain.prev_sha256` equals the SHA-256 of the JWS line above it (the same
/// hash-chain the Go `Store.Verify` checks). [entries] must be in chain order
/// (oldest first). Returns the 0-based index of the first broken link, or -1
/// if intact. An empty or single-entry chain is intact.
int verifyChainLinks(List<ReceiptEntry> entries) {
  String? prevHash;
  for (var i = 0; i < entries.length; i++) {
    final want = entries[i].prevSha256;
    if (i == 0) {
      // First entry must not claim a predecessor.
      if (want != null && want.isNotEmpty) return 0;
    } else {
      if (want != prevHash) return i;
    }
    prevHash = _sha256Hex(utf8.encode(entries[i].jws));
  }
  return -1;
}

String _sha256Hex(List<int> bytes) {
  final digest = crypto.sha256.convert(bytes).bytes;
  final sb = StringBuffer();
  for (final b in digest) {
    sb.write(b.toRadixString(16).padLeft(2, '0'));
  }
  return sb.toString();
}

/// Whether [payloadBytes] (raw JWS payload) decodes to the same JSON value as
/// [statement], ignoring key order and whitespace — the Dart mirror of the
/// daemon's jsonSemanticallyEqual.
bool _jsonSemanticallyEqual(
  Uint8List payloadBytes,
  Map<String, dynamic> statement,
) {
  final Object? decoded;
  try {
    decoded = json.decode(utf8.decode(payloadBytes));
  } on FormatException {
    return false;
  }
  return _deepEquals(decoded, statement);
}

bool _deepEquals(Object? a, Object? b) {
  if (a is Map && b is Map) {
    if (a.length != b.length) return false;
    for (final key in a.keys) {
      if (!b.containsKey(key)) return false;
      if (!_deepEquals(a[key], b[key])) return false;
    }
    return true;
  }
  if (a is List && b is List) {
    if (a.length != b.length) return false;
    for (var i = 0; i < a.length; i++) {
      if (!_deepEquals(a[i], b[i])) return false;
    }
    return true;
  }
  return a == b;
}

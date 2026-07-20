import 'dart:convert';

import 'package:basic_utils/basic_utils.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart' show debugPrint;

import '../local/settings_store.dart';

/// The device's client identity for TLS client authentication (ADR 0005).
///
/// The phone generates a P-256 keypair once, wraps it in a self-signed client
/// certificate, and presents that certificate on every TLS connection. The
/// daemon binds the device to the key's fingerprint at pair time and requires
/// it thereafter — the model is SSH `authorized_keys`, not PKI, so there is no
/// CA anywhere.
///
/// Identity is the **key**, not the certificate: the daemon hashes the
/// certificate's `SubjectPublicKeyInfo`, so a certificate regenerated around
/// the same key keeps the same identity. The certificate's validity window is
/// deliberately ignored by the daemon.
///
/// The keypair is generated **once** and reused across reconnects and process
/// death. Regenerating it on every connect would change the device's identity
/// and break authentication, so [loadOrCreate] never mints a new key when one
/// is already stored.
class ClientIdentity {
  const ClientIdentity({required this.certPem, required this.keyPem});

  /// PEM-encoded self-signed client certificate (`-----BEGIN CERTIFICATE-----`).
  final String certPem;

  /// PEM-encoded EC private key (`-----BEGIN EC PRIVATE KEY-----`), the form
  /// `SecurityContext.usePrivateKeyBytes` consumes.
  final String keyPem;

  /// SHA-256 of the certificate's `SubjectPublicKeyInfo`, as unpadded
  /// base64url — the exact value the daemon derives from
  /// `RawSubjectPublicKeyInfo`. Not transmitted (the daemon reads the key from
  /// the presented certificate); retained for diagnostics and enrolment checks.
  String get spkiFingerprint => spkiFingerprintOfKeyPem(keyPem);

  /// Generate a fresh P-256 keypair and a self-signed client certificate around
  /// it. Pure — persistence is [loadOrCreate]'s job.
  static ClientIdentity generate() {
    final pair = CryptoUtils.generateEcKeyPair(curve: 'prime256v1');
    final priv = pair.privateKey as ECPrivateKey;
    final pub = pair.publicKey as ECPublicKey;

    final csr = X509Utils.generateEccCsrPem(
      const {'CN': 'mcremote-client'},
      priv,
      pub,
    );
    // The daemon ignores the certificate's validity window (ADR 0005:
    // identity is the key, as with SSH), but a long life avoids any local
    // tooling tripping over an expired-looking leaf.
    final certPem = X509Utils.generateSelfSignedCertificate(priv, csr, 3650);
    final keyPem = CryptoUtils.encodeEcPrivateKeyToPem(priv);
    return ClientIdentity(certPem: certPem, keyPem: keyPem);
  }

  /// Return the persisted identity, generating and storing one on first use.
  ///
  /// Load-or-create with load taking precedence: an identity already in secure
  /// storage is returned unchanged so the device keeps a stable key across
  /// reconnects and process death. A partially-written pair (one of the two
  /// PEMs missing) is treated as absent and regenerated.
  static Future<ClientIdentity> loadOrCreate(SettingsStore store) async {
    final existing = await store.getClientCertAndKey();
    if (existing != null) {
      return ClientIdentity(certPem: existing.cert, keyPem: existing.key);
    }
    final fresh = ClientIdentity.generate();
    await store.setClientCertAndKey(cert: fresh.certPem, key: fresh.keyPem);
    return fresh;
  }
}

/// SHA-256 of the `SubjectPublicKeyInfo` DER for the key in [keyPem], as
/// unpadded base64url.
///
/// The public key is derived from the private key (Q = d·G), encoded as the
/// RFC 5480 `SubjectPublicKeyInfo` SEQUENCE, and hashed — byte-for-byte the
/// structure Go exposes as `x509.Certificate.RawSubjectPublicKeyInfo`, so the
/// two agree without the daemon in the loop.
String spkiFingerprintOfKeyPem(String keyPem) {
  final priv = CryptoUtils.ecPrivateKeyFromPem(keyPem);
  final params = priv.parameters!;
  final q = params.G * priv.d!;
  final pub = ECPublicKey(q, params);
  final spkiPem = CryptoUtils.encodeEcPublicKeyToPem(pub);
  final der = CryptoUtils.getBytesFromPEMString(spkiPem);
  return base64Url.encode(sha256.convert(der).bytes).replaceAll('=', '');
}

/// Best-effort SPKI fingerprint for logging; empty string on any parse failure
/// so diagnostics never throw.
String debugSpkiFingerprint(String keyPem) {
  try {
    return spkiFingerprintOfKeyPem(keyPem);
  } catch (e) {
    debugPrint('ClientIdentity: could not compute SPKI fingerprint: $e');
    return '';
  }
}

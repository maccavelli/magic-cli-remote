@TestOn('vm')
library;

import 'dart:convert';

import 'package:basic_utils/basic_utils.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/ws/client_identity.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  group('ClientIdentity.generate', () {
    test('produces a PEM cert + EC key and a well-formed SPKI fingerprint', () {
      final id = ClientIdentity.generate();
      expect(id.certPem, contains('BEGIN CERTIFICATE'));
      expect(id.keyPem, contains('BEGIN EC PRIVATE KEY'));

      final fp = id.spkiFingerprint;
      // Unpadded base64url SHA-256 is exactly 43 chars, URL-safe alphabet.
      expect(fp.length, 43);
      expect(fp, matches(RegExp(r'^[A-Za-z0-9_-]{43}$')));

      // Recomputing from the same key is deterministic.
      expect(spkiFingerprintOfKeyPem(id.keyPem), fp);
    });

    test('two generations yield distinct identities', () {
      final a = ClientIdentity.generate();
      final b = ClientIdentity.generate();
      expect(a.spkiFingerprint, isNot(b.spkiFingerprint));
    });

    test('the fingerprint hashes the SPKI embedded in the certificate — the '
        'exact bytes the daemon hashes as RawSubjectPublicKeyInfo', () {
      final id = ClientIdentity.generate();

      // Derive the public key from the private key and encode its
      // SubjectPublicKeyInfo (RFC 5480), the same structure Go exposes as
      // x509.Certificate.RawSubjectPublicKeyInfo.
      final priv = CryptoUtils.ecPrivateKeyFromPem(id.keyPem);
      final pub = ECPublicKey(priv.parameters!.G * priv.d!, priv.parameters);
      final spkiDer = CryptoUtils.getBytesFromPEMString(
        CryptoUtils.encodeEcPublicKeyToPem(pub),
      );

      // Those SPKI bytes must appear verbatim inside the certificate DER: the
      // key we fingerprint is the key the daemon reads off the presented cert.
      final certDer = CryptoUtils.getBytesFromPEMString(id.certPem);
      expect(
        _containsSublist(certDer, spkiDer),
        isTrue,
        reason: 'the cert must embed the same SPKI we fingerprint',
      );

      // And the advertised fingerprint is sha256(SPKI) as unpadded base64url.
      final expected = base64Url
          .encode(sha256.convert(spkiDer).bytes)
          .replaceAll('=', '');
      expect(id.spkiFingerprint, expected);
    });
  });

  group('ClientIdentity.loadOrCreate', () {
    test('generates once and returns the same identity on reload', () async {
      final store = _storeOver(_InMemorySecureStorage());
      final first = await ClientIdentity.loadOrCreate(store);
      final second = await ClientIdentity.loadOrCreate(store);
      expect(second.certPem, first.certPem);
      expect(second.keyPem, first.keyPem);
      expect(second.spkiFingerprint, first.spkiFingerprint);
    });

    test('survives process death: a fresh store over the same secure storage '
        'restores the identity rather than minting a new one', () async {
      // One backing store (the device keystore) shared by two SettingsStore
      // instances, as across an app restart.
      final backing = _InMemorySecureStorage();

      final before = await ClientIdentity.loadOrCreate(_storeOver(backing));
      final after = await ClientIdentity.loadOrCreate(_storeOver(backing));

      expect(
        after.keyPem,
        before.keyPem,
        reason: 'a new key would change identity and break auth',
      );
      expect(after.spkiFingerprint, before.spkiFingerprint);
    });

    test(
      'a half-written pair (key without cert) is treated as absent',
      () async {
        final backing = _InMemorySecureStorage();
        final store = _storeOver(backing);
        // Only the key survives; the cert write was lost.
        final orphan = ClientIdentity.generate();
        await store.setClientCertAndKey(
          cert: orphan.certPem,
          key: orphan.keyPem,
        );
        backing.wipe('client_cert');

        final rebuilt = await ClientIdentity.loadOrCreate(_storeOver(backing));
        // A self-consistent pair is minted; it is not the orphaned key.
        expect(rebuilt.keyPem, isNot(orphan.keyPem));
        // ...and it round-trips on the next load.
        final again = await ClientIdentity.loadOrCreate(_storeOver(backing));
        expect(again.keyPem, rebuilt.keyPem);
      },
    );
  });
}

SettingsStore _storeOver(FlutterSecureStorage secure) => SettingsStore(
  secure: secure,
  // Mobile-parity: no cleartext fallback, so the identity lives only in the
  // (in-memory) secure store.
  allowPlaintextFallback: false,
);

bool _containsSublist(List<int> haystack, List<int> needle) {
  if (needle.isEmpty || needle.length > haystack.length) return false;
  outer:
  for (var i = 0; i + needle.length <= haystack.length; i++) {
    for (var j = 0; j < needle.length; j++) {
      if (haystack[i + j] != needle[j]) continue outer;
    }
    return true;
  }
  return false;
}

class _InMemorySecureStorage implements FlutterSecureStorage {
  final _values = <String, String>{};

  void wipe(String key) => _values.remove(key);

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

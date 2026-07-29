import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  test('re-pair clears pin + client identity and keeps host/token', () async {
    final store = SettingsStore(allowPlaintextFallback: true);
    await store.setHost('100.64.0.1:7531');
    await store.setToken('device-token-xyz');
    await store.setDeviceId('dev-1');
    await store.setFingerprint(
      '100.64.0.1:7531',
      'b95a85aabbccddeeff00112233445566778899aabbccddeeff00112233445566',
      deviceId: 'dev-1',
      mode: TlsMode.selfsigned,
    );
    await store.setClientCertAndKey(cert: 'CERT', key: 'KEY');

    // Re-pair scope (MADR 0052 B7).
    await store.clearFingerprint();
    await store.clearClientIdentity();

    expect(await store.getHost(), '100.64.0.1:7531');
    expect(await store.getToken(), 'device-token-xyz');
    expect(await store.getDeviceId(), 'dev-1');
    expect(
      await store.getPinnedCert(
        '100.64.0.1:7531',
        deviceId: 'dev-1',
        fallbackToPersistedIdentity: false,
      ),
      isNull,
    );
    expect(await store.getClientCertAndKey(), isNull);
  });

  test(
    'getPinnedCert without device id does not fall back when disabled',
    () async {
      final store = SettingsStore(allowPlaintextFallback: true);
      await store.setHost('100.64.0.1:7531');
      await store.setDeviceId('dev-1');
      await store.setFingerprint(
        '100.64.0.1:7531',
        'aa' * 32,
        deviceId: 'dev-1',
        mode: TlsMode.letsencrypt,
      );
      // Security card path: pass real device id, no fallback.
      final pin = await store.getPinnedCert(
        '100.64.0.1:7531',
        deviceId: 'dev-1',
        fallbackToPersistedIdentity: false,
      );
      expect(pin, isNotNull);
      expect(pin!.mode, TlsMode.letsencrypt);
    },
  );
}

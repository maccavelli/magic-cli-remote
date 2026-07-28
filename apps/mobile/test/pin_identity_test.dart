@TestOn('vm')
library;

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:magic_cli_remote/data/protocol/pair_uri.dart';
import 'package:magic_cli_remote/data/ws/mcremote_client.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// A pin says "this certificate belongs to *that* daemon". These cover the two
/// ways the client used to lose the second half of that sentence: probing an
/// unrelated host under the paired daemon's identity (MADR 0046 M-1), and
/// letting a write borrow the persisted identity (H-B).

const _fpA = 'AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA';
const _fpB = 'ISIjJCUmJygpKissLS4vMDEyMzQ1Njc4OTo7PD0-P0A';

class _InMemorySecureStorage implements FlutterSecureStorage {
  final values = <String, String>{};

  @override
  dynamic noSuchMethod(Invocation invocation) {
    final key = invocation.namedArguments[#key] as String?;
    switch (invocation.memberName) {
      case #read:
        return Future<String?>.value(values[key]);
      case #write:
        final value = invocation.namedArguments[#value] as String?;
        if (value == null) {
          values.remove(key);
        } else {
          values[key!] = value;
        }
        return Future<void>.value();
      case #delete:
        values.remove(key);
        return Future<void>.value();
      default:
        return Future<void>.value();
    }
  }
}

Future<SettingsStore> _pairedToA() async {
  final store = SettingsStore(
    secure: _InMemorySecureStorage(),
    prefs: await SharedPreferences.getInstance(),
    allowPlaintextFallback: false,
  );
  await store.setDeviceId('dev-a');
  await store.setHost('100.64.0.1:7531');
  await store.setFingerprint('100.64.0.1:7531', _fpA, deviceId: 'dev-a');
  return store;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => SharedPreferences.setMockInitialValues({}));

  test('probing another host leaves the paired pin alone', () async {
    final store = await _pairedToA();
    final client = McremoteClient(settings: store);
    addTearDown(client.dispose);

    // "Test connection" against a daemon this device has never paired with.
    // Nothing is listening, so the probe fails — but it has already resolved
    // its pin by then, which is the part under test.
    await expectLater(
      client.healthz('100.64.0.9:7531', fingerprint: _fpB),
      throwsA(anything),
    );

    expect(
      await store.getFingerprint('100.64.0.1:7531', deviceId: 'dev-a'),
      _fpA,
      reason: 'the probe must not overwrite the paired daemon\'s pin',
    );
    expect(
      client.pinnedFingerprint,
      isNull,
      reason: 'a probe must not arm the connection with its pin',
    );
    expect(client.tlsMode, TlsMode.fallback);
  });

  test('a probe never inherits the paired identity to pin with', () async {
    final store = await _pairedToA();
    final client = McremoteClient(settings: store);
    addTearDown(client.dispose);

    // No fingerprint in hand for the new host: the paired daemon's pin must
    // not be produced for it, which would fail closed against a host that
    // never claimed it while reading as an impersonation attempt.
    await expectLater(client.healthz('100.64.0.9:7531'), throwsA(anything));
    expect(await store.getFingerprint('100.64.0.9:7531'), isNull);
  });
}

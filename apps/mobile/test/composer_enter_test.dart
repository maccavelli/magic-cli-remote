import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  test('send_with_enter defaults to true (today\'s behaviour)', () async {
    final store = SettingsStore(allowPlaintextFallback: true);
    expect(await store.getSendWithEnter(), isTrue);
  });

  test('send_with_enter can be turned off and back on', () async {
    final store = SettingsStore(allowPlaintextFallback: true);
    await store.setSendWithEnter(false);
    expect(await store.getSendWithEnter(), isFalse);
    await store.setSendWithEnter(true);
    expect(await store.getSendWithEnter(), isTrue);
  });
}

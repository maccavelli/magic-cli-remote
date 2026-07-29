import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  test('default session mode is per-provider and clearable', () async {
    final store = SettingsStore(allowPlaintextFallback: true);
    expect(await store.getDefaultSessionMode('codex'), isNull);

    await store.setDefaultSessionMode('codex', 'auto');
    expect(await store.getDefaultSessionMode('codex'), 'auto');
    expect(await store.getDefaultSessionMode('grok'), isNull);

    await store.setDefaultSessionMode('codex', null);
    expect(await store.getDefaultSessionMode('codex'), isNull);
  });

  test('stored mode that is not in advertised list is still returned '
      '(apply path ignores it)', () async {
    final store = SettingsStore(allowPlaintextFallback: true);
    await store.setDefaultSessionMode('opencode', 'vanished');
    // The store returns what was saved; chat applies only if modes contain it.
    expect(await store.getDefaultSessionMode('opencode'), 'vanished');
  });
}

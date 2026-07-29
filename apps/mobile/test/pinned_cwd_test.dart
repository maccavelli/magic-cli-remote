import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/local/settings_store.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  test('pinned survive recents overflow; never double in lists', () async {
    final store = SettingsStore(allowPlaintextFallback: true);
    await store.pinCwd('/home/proj');
    for (var i = 0; i < SettingsStore.kMaxRecentCwds + 3; i++) {
      await store.addRecentCwd('/tmp/r$i');
    }
    // Pin is not displaced by recency churn.
    expect(await store.getPinnedCwds(), ['/home/proj']);
    // addRecentCwd skips pinned paths.
    await store.addRecentCwd('/home/proj');
    expect(await store.getRecentCwds(), isNot(contains('/home/proj')));
  });

  test('unpin returns path to recency', () async {
    final store = SettingsStore(allowPlaintextFallback: true);
    await store.pinCwd('/a');
    await store.unpinCwd('/a');
    expect(await store.getPinnedCwds(), isEmpty);
    expect(await store.getRecentCwds(), ['/a']);
  });

  test('reorder persists', () async {
    final store = SettingsStore(allowPlaintextFallback: true);
    await store.pinCwd('/a');
    await store.pinCwd('/b');
    // pinCwd inserts at front: [/b, /a]
    expect(await store.getPinnedCwds(), ['/b', '/a']);
    await store.reorderPinnedCwd(0, 2);
    expect(await store.getPinnedCwds(), ['/a', '/b']);
  });
}

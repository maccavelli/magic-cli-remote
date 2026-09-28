import 'dart:io';

import 'package:path_provider_platform_interface/path_provider_platform_interface.dart';
import 'package:plugin_platform_interface/plugin_platform_interface.dart';

/// Points path_provider at a throwaway directory so unit tests never reach a
/// real platform channel — or a real user directory (MADR 0084 D3, which moved
/// transcript entries from preferences to files).
class FakePathProvider extends PathProviderPlatform
    with MockPlatformInterfaceMixin {
  FakePathProvider(this.root);

  final Directory root;

  @override
  Future<String?> getApplicationSupportPath() async => root.path;

  @override
  Future<String?> getApplicationDocumentsPath() async => root.path;

  @override
  Future<String?> getTemporaryPath() async => root.path;
}

/// Installs the fake for the current test and cleans up after it. Returns the
/// directory so a test can assert against what landed on disk.
///
/// The platform instance is deliberately **not** restored on teardown: a
/// debounced transcript save can complete after the test that started it
/// ends, and restoring the real (absent) channel would make that late write
/// throw a MissingPluginException into the next test's output.
///
/// A late save is **not** harmless. The transcript cache resolves its
/// directory lazily through this global, so a save that outlives its test can
/// land in the *next* test's directory, sweep that test's temp files and
/// overwrite its transcript (MADR 0173 F7, F8). A test that disposes a
/// container holding transcript state must build it with
/// `transcriptsTestContainer`, whose teardown waits for those saves.
Directory useFakePathProvider(void Function(void Function()) addTearDown) {
  final dir = Directory.systemTemp.createTempSync('mcremote_test');
  PathProviderPlatform.instance = FakePathProvider(dir);
  addTearDown(() {
    try {
      if (dir.existsSync()) dir.deleteSync(recursive: true);
    } catch (_) {
      // Cleanup is best-effort; the OS reclaims the temp dir. This does not
      // make a late save safe: see the doc comment above.
    }
  });
  return dir;
}

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:magic_cli_remote/data/protocol/models.dart';
import 'package:magic_cli_remote/state/transcripts_notifier.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'support/fake_path_provider.dart';
import 'support/transcripts_container.dart';

/// Guards the harness itself (MADR 0173 D4): once `transcriptsTestContainer`'s
/// teardown completes, the disposal flush has already reached this test's own
/// directory. A helper that disposes without awaiting fails here, because the
/// save is still in flight when the teardown returns.
void main() {
  late Directory dir;

  setUp(() {
    SharedPreferences.setMockInitialValues({});
    dir = useFakePathProvider(addTearDown);
  });

  test('teardown returns only after the transcript save lands', () async {
    // Collect the helper's teardown instead of handing it to the framework,
    // so the test can run it and then look at the disk.
    final teardowns = <dynamic Function()>[];
    final c = transcriptsTestContainer(teardowns.add);
    c
        .read(transcriptsProvider.notifier)
        .debugOnEvent(
          SessionEvent(
            type: 'user_message',
            sessionId: 'guard',
            seq: 1,
            text: 'hello',
          ),
        );

    expect(teardowns, hasLength(1));
    await teardowns.single();

    expect(
      File('${dir.path}/transcripts/guard.json').existsSync(),
      isTrue,
      reason:
          'the save must land before the teardown returns, or it can reach '
          "the next test's directory (MADR 0173 F7)",
    );
  });
}

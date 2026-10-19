import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:magic_cli_remote/data/update/app_update.dart';

/// A sink that writes to the real file but fails when closed.
class _FailingCloseSink implements IOSink {
  _FailingCloseSink(this._inner);

  final IOSink _inner;

  @override
  void add(List<int> data) => _inner.add(data);

  @override
  Future<void> close() async {
    await _inner.close();
    throw const FileSystemException('close failed');
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

/// MADR 0201 D39 (0073 §7.5(c)). In the download's catch, `sink.close()` ran
/// before the partial file was deleted: a close that also failed masked the
/// original error and left a partial APK behind.
void main() {
  test('a failed download removes its partial file and reports the '
      'original error', () async {
    final client = MockClient.streaming((req, body) async {
      final stream = Stream<List<int>>.multi((c) {
        c.add([1, 2, 3]);
        c.addError(const SocketException('connection dropped'));
        c.close();
      });
      return http.StreamedResponse(stream, 200);
    });
    final svc = AppUpdateService(
      client: client,
      openSink: (f) => _FailingCloseSink(f.openWrite()),
    );
    final dir = await Directory.systemTemp.createTemp('apk-cleanup-');
    addTearDown(() => dir.delete(recursive: true));

    await expectLater(
      svc.downloadAndVerify(
        apk: const UpdateAsset(
          name: 'magic-cli-remote-x.apk',
          url: 'https://ex/apk',
          size: 3,
          digest: 'deadbeef',
        ),
        cacheDir: dir,
      ),
      throwsA(isA<SocketException>()),
      reason: 'the stream error, not the close error, is what failed',
    );
    expect(dir.listSync(), isEmpty, reason: 'no partial APK is left behind');
  });
}

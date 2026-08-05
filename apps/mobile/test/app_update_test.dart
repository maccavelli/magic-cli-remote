import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:magic_cli_remote/data/update/app_update.dart';

void main() {
  group('AppUpdateService base compare (0065)', () {
    test('parseBase', () {
      expect(AppUpdateService.parseBase('0.6.7'), (0, 6, 7));
      expect(AppUpdateService.parseBase('v0.7.0'), (0, 7, 0));
      expect(AppUpdateService.parseBase('0.6.7.4.gabc')?.$3, 7);
      expect(AppUpdateService.parseBase('dev'), isNull);
    });

    test('isNewerBase', () {
      expect(AppUpdateService.isNewerBase('0.6.8', '0.6.7'), isTrue);
      expect(AppUpdateService.isNewerBase('0.6.7', '0.6.7'), isFalse);
      expect(AppUpdateService.isNewerBase('0.6.6', '0.6.7'), isFalse);
      expect(AppUpdateService.isNewerBase('v0.7.0', '0.6.9.1.gdev'), isTrue);
    });
  });

  group('checkLatest MockClient', () {
    test('up to date', () async {
      final client = MockClient((req) async {
        return http.Response(
          jsonEncode({
            'tag_name': 'v0.6.7',
            'assets': [
              {
                'name': 'magic-cli-remote-v0.6.7-arm64.apk',
                'browser_download_url': 'https://example/a.apk',
                'size': 10,
              },
              {
                'name': 'SHA256SUMS-0.6.7',
                'browser_download_url': 'https://example/s',
                'size': 20,
              },
            ],
          }),
          200,
        );
      });
      final svc = AppUpdateService(
        client: client,
        localVersion: () async => '0.6.7',
      );
      final r = await svc.checkLatest();
      expect(r.updateAvailable, isFalse);
      expect(r.apk, isNotNull);
    });

    test('available', () async {
      final client = MockClient((req) async {
        return http.Response(
          jsonEncode({
            'tag_name': 'v0.9.0',
            'assets': [
              {
                'name': 'magic-cli-remote-v0.9.0-arm64.apk',
                'browser_download_url': 'https://example/a.apk',
                'size': 10,
              },
            ],
          }),
          200,
        );
      });
      final svc = AppUpdateService(
        client: client,
        localVersion: () async => '0.6.7',
      );
      final r = await svc.checkLatest();
      expect(r.updateAvailable, isTrue);
      expect(r.remoteTag, 'v0.9.0');
    });

    test('404', () async {
      final client = MockClient((req) async => http.Response('no', 404));
      final svc = AppUpdateService(
        client: client,
        localVersion: () async => '0.6.7',
      );
      expect(svc.checkLatest(), throwsA(isA<AppUpdateException>()));
    });
  });

  group('downloadAndVerify', () {
    test('checksum mismatch discards file', () async {
      final apkBytes = utf8.encode('apk-body');
      final client = MockClient((req) async {
        if (req.url.path.endsWith('sums')) {
          return http.Response('deadbeef  magic-cli-remote-x.apk\n', 200);
        }
        return http.Response.bytes(apkBytes, 200);
      });
      final svc = AppUpdateService(client: client);
      final dir = await Directory.systemTemp.createTemp('apk-test-');
      addTearDown(() => dir.delete(recursive: true));
      await expectLater(
        svc.downloadAndVerify(
          apk: const UpdateAsset(
            name: 'magic-cli-remote-x.apk',
            url: 'https://ex/apk',
            size: 1,
          ),
          sums: const UpdateAsset(
            name: 'SHA256SUMS',
            url: 'https://ex/sums',
            size: 1,
          ),
          cacheDir: dir,
        ),
        throwsA(isA<AppUpdateException>()),
      );
      expect(dir.listSync(), isEmpty);
    });

    test('ok verifies streaming hash', () async {
      final apkBytes = utf8.encode('apk-body-ok');
      final want = sha256.convert(apkBytes).toString();
      final client = MockClient((req) async {
        if (req.url.path.endsWith('sums')) {
          return http.Response('$want  magic-cli-remote-x.apk\n', 200);
        }
        return http.Response.bytes(apkBytes, 200);
      });
      final svc = AppUpdateService(client: client);
      final dir = await Directory.systemTemp.createTemp('apk-ok-');
      addTearDown(() => dir.delete(recursive: true));
      final f = await svc.downloadAndVerify(
        apk: const UpdateAsset(
          name: 'magic-cli-remote-x.apk',
          url: 'https://ex/apk',
          size: 1,
        ),
        sums: const UpdateAsset(
          name: 'SHA256SUMS',
          url: 'https://ex/sums',
          size: 1,
        ),
        cacheDir: dir,
      );
      expect(await f.exists(), isTrue);
      expect(svc.verifiedApk?.path, f.path);
    });
  });
}

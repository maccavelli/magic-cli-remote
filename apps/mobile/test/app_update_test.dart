import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:magic_cli_remote/data/update/app_update.dart';

void main() {
  group('AppUpdateService published compare (0065, 0128 D2)', () {
    test('parseVersion', () {
      final a = AppUpdateService.parseVersion('0.6.7')!;
      expect([a.major, a.minor, a.patch, a.n], [0, 6, 7, 0]);
      final b = AppUpdateService.parseVersion('v0.7.0')!;
      expect([b.major, b.minor, b.patch, b.n], [0, 7, 0, 0]);
      // Patch may carry a local suffix; the serial is still read.
      final c = AppUpdateService.parseVersion('0.6.7.4.gabc')!;
      expect([c.patch, c.n], [7, 4]);
      expect(AppUpdateService.parseVersion('dev'), isNull);
      expect(AppUpdateService.parseVersion('debug'), isNull);
      expect(AppUpdateService.parseVersion('0.6'), isNull);
    });

    // Unchanged from the three-part era: these all still hold.
    test('base differences still decide', () {
      expect(AppUpdateService.isNewerPublished('0.6.8', '0.6.7'), isTrue);
      expect(AppUpdateService.isNewerPublished('0.6.7', '0.6.7'), isFalse);
      expect(AppUpdateService.isNewerPublished('0.6.6', '0.6.7'), isFalse);
      expect(
        AppUpdateService.isNewerPublished('v0.7.0', '0.6.9.1.gdev'),
        isTrue,
      );
    });

    // MADR 0128 D2 — the case the old three-part compare got wrong. Go's
    // update/run.go has compared the serial since MADR 0103; the phone did not,
    // so a serial-only release was offered by the CLI and withheld by the app.
    test('a serial-only release is newer', () {
      expect(
        AppUpdateService.isNewerPublished('v0.15.3.3', '0.15.3.2'),
        isTrue,
        reason: '0128 D2: N decides when the base is equal',
      );
      expect(
        AppUpdateService.isNewerPublished('v0.15.3.1', '0.15.3.2'),
        isFalse,
      );
      expect(
        AppUpdateService.isNewerPublished('v0.15.3.2', '0.15.3.2'),
        isFalse,
      );
    });

    test('a three-part version reads as N=0, so it never beats a serial', () {
      expect(AppUpdateService.isNewerPublished('v0.15.3', '0.15.3.1'), isFalse);
      expect(AppUpdateService.isNewerPublished('v0.15.3.1', '0.15.3'), isTrue);
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

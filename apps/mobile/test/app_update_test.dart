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

  // MADR 0132. Replays the real v0.16.0 release shape: one APK carrying a
  // GitHub `digest`, plus SHA256SUMS and SHA256SUMS-0.16.0 in that order,
  // NEITHER of which lists the APK. Fails on the pre-0132 client with
  // "no checksum entry for magic-cli-remote-v0.16.0-arm64.apk" — the string
  // the phone reported in the field.
  group('0132 regression: the real v0.16.0 asset shape', () {
    test('the APK verifies from its digest alone', () async {
      final apkBytes = utf8.encode('v0160-apk-body');
      final want = sha256.convert(apkBytes).toString();
      // The canonical manifests list the Go binaries only, exactly as the
      // published release does.
      const canonicalSums =
          'dede3ff6371fb0f3a3b317141c66646ac8d7bbf33bda7f117b1d512edd3f28e5  '
          'mcremote-darwin-arm64\n'
          '24513396be3d31ae65f629e27fa4f1e5abf17012bf4bb7d68c83e6485f185734  '
          'mcremote-linux-amd64\n';
      final client = MockClient((req) async {
        if (req.url.host == 'api.github.com') {
          return http.Response(
            jsonEncode({
              'tag_name': 'v0.16.0',
              'assets': [
                {
                  'name': 'magic-cli-remote-v0.16.0-arm64.apk',
                  'browser_download_url': 'https://ex/apk',
                  'size': apkBytes.length,
                  'digest': 'sha256:$want',
                },
                {
                  'name': 'SHA256SUMS',
                  'browser_download_url': 'https://ex/sums',
                  'size': canonicalSums.length,
                },
                {
                  'name': 'SHA256SUMS-0.16.0',
                  'browser_download_url': 'https://ex/sums-bridge',
                  'size': canonicalSums.length,
                },
              ],
            }),
            200,
          );
        }
        if (req.url.path.contains('sums')) {
          return http.Response(canonicalSums, 200);
        }
        return http.Response.bytes(apkBytes, 200);
      });
      final svc = AppUpdateService(
        client: client,
        localVersion: () async => '0.15.3.13',
      );
      final r = await svc.checkLatest();
      expect(r.updateAvailable, isTrue);
      expect(r.apk, isNotNull);
      final dir = await Directory.systemTemp.createTemp('apk-0132-');
      addTearDown(() => dir.delete(recursive: true));
      final f = await svc.downloadAndVerify(
        apk: r.apk!,
        sums: r.sums!,
        cacheDir: dir,
      );
      expect(await f.exists(), isTrue);
      expect(svc.verifiedApk?.path, f.path);
    });
  });

  // MADR 0132. The digest path, the manifest fallback, and the two ways of
  // having no expected hash at all.
  group('0132 digest-first verification', () {
    UpdateAsset apkAsset({String? digest}) => UpdateAsset(
      name: 'magic-cli-remote-v0.16.0-arm64.apk',
      url: 'https://ex/apk',
      size: 1,
      digest: digest,
    );
    const sumsAsset = UpdateAsset(
      name: 'SHA256SUMS',
      url: 'https://ex/sums',
      size: 1,
    );

    test('normalizeDigest', () {
      expect(AppUpdateService.normalizeDigest('sha256:${'A' * 64}'), 'a' * 64);
      expect(AppUpdateService.normalizeDigest(null), isNull);
      // An algorithm this client cannot check must not stand in for a hash.
      expect(AppUpdateService.normalizeDigest('sha512:${'a' * 128}'), isNull);
      expect(AppUpdateService.normalizeDigest('a' * 64), isNull);
      expect(AppUpdateService.normalizeDigest('sha256:nothex'), isNull);
      expect(AppUpdateService.normalizeDigest('sha256:${'a' * 63}'), isNull);
    });

    test('the digest is preferred and the manifest is never fetched', () async {
      final apkBytes = utf8.encode('digest-preferred');
      final want = sha256.convert(apkBytes).toString();
      var sumsFetched = false;
      final client = MockClient((req) async {
        if (req.url.path.contains('sums')) {
          sumsFetched = true;
          return http.Response('deadbeef  wrong.apk\n', 200);
        }
        return http.Response.bytes(apkBytes, 200);
      });
      final svc = AppUpdateService(client: client);
      final dir = await Directory.systemTemp.createTemp('apk-digest-');
      addTearDown(() => dir.delete(recursive: true));
      final f = await svc.downloadAndVerify(
        apk: apkAsset(digest: want),
        sums: sumsAsset,
        cacheDir: dir,
      );
      expect(await f.exists(), isTrue);
      expect(
        sumsFetched,
        isFalse,
        reason: '0132: a usable digest makes the manifest request pointless',
      );
    });

    test('falls back to SHA256SUMS when the digest is absent', () async {
      final apkBytes = utf8.encode('fallback-body');
      final want = sha256.convert(apkBytes).toString();
      final client = MockClient((req) async {
        if (req.url.path.contains('sums')) {
          return http.Response(
            '$want  magic-cli-remote-v0.16.0-arm64.apk\n',
            200,
          );
        }
        return http.Response.bytes(apkBytes, 200);
      });
      final svc = AppUpdateService(client: client);
      final dir = await Directory.systemTemp.createTemp('apk-fallback-');
      addTearDown(() => dir.delete(recursive: true));
      final f = await svc.downloadAndVerify(
        apk: apkAsset(),
        sums: sumsAsset,
        cacheDir: dir,
      );
      expect(await f.exists(), isTrue);
    });

    test('a mismatched digest aborts and discards the file', () async {
      final client = MockClient(
        (req) async => http.Response.bytes(utf8.encode('real-body'), 200),
      );
      final svc = AppUpdateService(client: client);
      final dir = await Directory.systemTemp.createTemp('apk-badigest-');
      addTearDown(() => dir.delete(recursive: true));
      await expectLater(
        svc.downloadAndVerify(
          apk: apkAsset(digest: 'b' * 64),
          sums: sumsAsset,
          cacheDir: dir,
        ),
        throwsA(
          isA<AppUpdateException>().having(
            (e) => e.message,
            'message',
            contains('sha256 mismatch'),
          ),
        ),
      );
      expect(dir.listSync(), isEmpty);
    });

    test('no digest and no manifest at all fails closed', () async {
      final client = MockClient(
        (req) async => http.Response.bytes(utf8.encode('body'), 200),
      );
      final svc = AppUpdateService(client: client);
      final dir = await Directory.systemTemp.createTemp('apk-nosums-');
      addTearDown(() => dir.delete(recursive: true));
      await expectLater(
        svc.downloadAndVerify(apk: apkAsset(), sums: null, cacheDir: dir),
        throwsA(
          isA<AppUpdateException>().having(
            (e) => e.message,
            'message',
            contains('no checksum for'),
          ),
        ),
      );
      expect(dir.listSync(), isEmpty);
    });

    test('no digest and no matching manifest line fails closed', () async {
      final client = MockClient((req) async {
        if (req.url.path.contains('sums')) {
          return http.Response('deadbeef  mcremote-linux-amd64\n', 200);
        }
        return http.Response.bytes(utf8.encode('body'), 200);
      });
      final svc = AppUpdateService(client: client);
      final dir = await Directory.systemTemp.createTemp('apk-noline-');
      addTearDown(() => dir.delete(recursive: true));
      await expectLater(
        svc.downloadAndVerify(apk: apkAsset(), sums: sumsAsset, cacheDir: dir),
        throwsA(
          isA<AppUpdateException>().having(
            (e) => e.message,
            'message',
            contains('no checksum for'),
          ),
        ),
      );
      expect(dir.listSync(), isEmpty);
    });

    test(
      'the exact SHA256SUMS wins over a bridge manifest listed first',
      () async {
        final client = MockClient(
          (req) async => http.Response(
            jsonEncode({
              'tag_name': 'v0.16.0',
              'assets': [
                {
                  'name': 'SHA256SUMS-0.16.0',
                  'browser_download_url': 'https://ex/sums-bridge',
                  'size': 1,
                },
                {
                  'name': 'SHA256SUMS',
                  'browser_download_url': 'https://ex/sums',
                  'size': 1,
                },
                {
                  'name': 'magic-cli-remote-v0.16.0-arm64.apk',
                  'browser_download_url': 'https://ex/apk',
                  'size': 1,
                  'digest': 'sha256:${'c' * 64}',
                },
              ],
            }),
            200,
          ),
        );
        final svc = AppUpdateService(
          client: client,
          localVersion: () async => '0.15.3',
        );
        final r = await svc.checkLatest();
        expect(r.sums!.name, 'SHA256SUMS');
        expect(r.apk!.digest, 'c' * 64);
      },
    );
  });
}

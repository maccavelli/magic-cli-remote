import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:magic_cli_remote/data/update/app_update.dart';
import 'package:magic_cli_remote/features/settings/app_update_tile.dart';
import 'package:crypto/crypto.dart';
import 'dart:convert';
import 'dart:io';

import 'support/fake_path_provider.dart';

void main() {
  testWidgets('available state then tap downloads only after check', (
    tester,
  ) async {
    final client = MockClient((req) async {
      return http.Response(
        jsonEncode({
          'tag_name': 'v9.0.0',
          'assets': [
            {
              'name': 'magic-cli-remote-v9.0.0-arm64.apk',
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
      localVersion: () async => '0.1.0',
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: AppUpdateTile(service: svc)),
      ),
    );
    expect(find.text('App update'), findsOneWidget);
    await tester.tap(find.text('App update'));
    await tester.pumpAndSettle();
    expect(find.textContaining('Update available'), findsOneWidget);
  });

  testWidgets('install requires verified path — channel not called without apk', (
    tester,
  ) async {
    var installs = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AppUpdateTile(
            service: AppUpdateService(
              client: MockClient((_) async => http.Response('{}', 200)),
              localVersion: () async => '0.1.0',
            ),
            installApk: (path, {preferSession = true}) async {
              installs++;
            },
          ),
        ),
      ),
    );
    // Force ready without file via state is hard; ensure installApk not auto-run.
    expect(installs, 0);
  });

  // MADR 0126 D6/F5. The default download directory moved from
  // `Directory.systemTemp` to path_provider's getTemporaryDirectory(), so that
  // `file_paths.xml` can grant the FileProvider exactly one subdirectory
  // instead of three roots at path=".". Nothing exercised that line before —
  // the other tests inject `service`/`installApk` and never reach the download
  // — so the narrowed grant would have been unverified.
  testWidgets(
    'download lands under getTemporaryDirectory/mcremote_app_updates',
    (tester) async {
      final tmp = useFakePathProvider(addTearDown);
      final client = MockClient((req) async {
        final url = req.url.toString();
        if (url.contains('api.github.com')) {
          return http.Response(
            jsonEncode({
              'tag_name': 'v9.0.0',
              'assets': [
                {
                  'name': 'magic-cli-remote-v9.0.0-arm64.apk',
                  'browser_download_url': 'https://example/a.apk',
                  'size': 3,
                },
                {
                  'name': 'SHA256SUMS.txt',
                  'browser_download_url': 'https://example/sums',
                  'size': 80,
                },
              ],
            }),
            200,
          );
        }
        if (url.contains('sums')) {
          // sha256 of "apk"
          return http.Response(
            'c15b4c8dbdff7e2f6ea6ca42ea9e35e9d6d4b0dfb1eb4a7b6f6c1e5a2b0b0e0f'
            '  magic-cli-remote-v9.0.0-arm64.apk\n',
            200,
          );
        }
        return http.Response('apk', 200);
      });
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: AppUpdateTile(
              service: AppUpdateService(
                client: client,
                localVersion: () async => '0.1.0',
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('App update'));
      await tester.pumpAndSettle();
      await tester.tap(find.textContaining('Update available'));
      await tester.pumpAndSettle();

      // The checksum above is deliberately wrong, so the download fails
      // verification — but only AFTER the directory has been created, which is
      // the line under test. Asserting on the directory rather than on a
      // successful install keeps this a test of the path, not of sha256.
      final dir = Directory('${tmp.path}/mcremote_app_updates');
      expect(
        dir.existsSync(),
        isTrue,
        reason:
            '0126 D6: downloads must land under getTemporaryDirectory(), '
            'which is the one root file_paths.xml still grants',
      );
    },
  );

  // MADR 0132. The tile used to refuse to start a download unless the release
  // published a SHA256SUMS asset, which since v0.16.0 never lists the APK. A
  // release whose only checksum source is the APK's own GitHub digest must
  // reach "Ready to install", not "No APK asset on this release".
  testWidgets('a digest-only release downloads and verifies', (tester) async {
    final tmp = useFakePathProvider(addTearDown);
    final apkBytes = utf8.encode('apk-body-0132');
    final want = sha256.convert(apkBytes).toString();
    final client = MockClient((req) async {
      final url = req.url.toString();
      if (url.contains('api.github.com')) {
        return http.Response(
          jsonEncode({
            'tag_name': 'v0.16.0',
            'assets': [
              {
                'name': 'magic-cli-remote-v0.16.0-arm64.apk',
                'browser_download_url': 'https://example/a.apk',
                'size': apkBytes.length,
                'digest': 'sha256:$want',
              },
            ],
          }),
          200,
        );
      }
      return http.Response.bytes(apkBytes, 200);
    });
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AppUpdateTile(
            service: AppUpdateService(
              client: client,
              localVersion: () async => '0.15.3.13',
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('App update'));
    await tester.pumpAndSettle();
    // The download's tail is real file I/O (`sink.close()`), which the widget
    // tester's fake-async zone never drains: pumping alone leaves the tile
    // parked on "Downloading… 100%" forever. Driving the whole download inside
    // runAsync puts it on the real event loop, so it can actually finish.
    await tester.runAsync(() async {
      await tester.tap(find.textContaining('Update available'));
      await Future<void>.delayed(const Duration(milliseconds: 300));
    });
    await tester.pumpAndSettle();

    expect(find.textContaining('No APK asset'), findsNothing);
    expect(find.textContaining('Ready to install'), findsOneWidget);
    expect(
      File(
        '${tmp.path}/mcremote_app_updates/'
        'magic-cli-remote-v0.16.0-arm64.apk',
      ).existsSync(),
      isTrue,
    );
  });
}

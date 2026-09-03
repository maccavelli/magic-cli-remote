import 'dart:io';

import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;
import '../../data/update/app_update.dart';

/// Settings "App update" tile state machine (MADR 0065 P3–P5).
enum AppUpdateUiState {
  idle,
  checking,
  upToDate,
  available,
  downloading,
  ready,
  installing,
  error,
}

/// Channel for Kotlin install helpers. Injected for tests.
typedef InstallApkFn = Future<void> Function(String path, {bool preferSession});

class AppUpdateTile extends StatefulWidget {
  const AppUpdateTile({
    super.key,
    this.service,
    this.installApk,
    this.cacheDir,
  });

  /// Optional injected service (tests).
  final AppUpdateService? service;

  /// Optional install seam (tests). Default: MethodChannel.
  final InstallApkFn? installApk;

  /// Optional cache dir (tests).
  final Directory? cacheDir;

  static const channelName = 'mcremote/app_update';

  @override
  State<AppUpdateTile> createState() => AppUpdateTileState();
}

@visibleForTesting
class AppUpdateTileState extends State<AppUpdateTile> {
  late final AppUpdateService _svc =
      widget.service ?? AppUpdateService(client: http.Client());
  AppUpdateUiState _state = AppUpdateUiState.idle;
  String _status = 'Tap to check for updates';
  double? _progress;
  UpdateCheckResult? _check;
  File? _apkFile;

  Future<void> check() async {
    setState(() {
      _state = AppUpdateUiState.checking;
      _status = 'Checking…';
      _progress = null;
    });
    try {
      final r = await _svc.checkLatest();
      if (!mounted) return;
      setState(() {
        _check = r;
        if (r.updateAvailable) {
          _state = AppUpdateUiState.available;
          _status = 'Update available: ${r.remoteTag}';
        } else {
          _state = AppUpdateUiState.upToDate;
          _status = 'Up to date (${r.localVersion})';
        }
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _state = AppUpdateUiState.error;
        _status = e.toString();
      });
    }
  }

  Future<void> download() async {
    final r = _check;
    // MADR 0132: a release need not publish a manifest the APK appears in —
    // since v0.16.0 none does. The APK asset alone is enough to attempt the
    // download; downloadAndVerify decides whether it can be verified, and
    // fails closed when it cannot.
    if (r == null || r.apk == null) {
      setState(() {
        _state = AppUpdateUiState.error;
        _status = 'No APK asset on this release';
      });
      return;
    }
    setState(() {
      _state = AppUpdateUiState.downloading;
      _status = 'Downloading…';
      _progress = 0;
    });
    try {
      // path_provider's getTemporaryDirectory() is documented to return
      // context.getCacheDir() on Android, which is what `file_paths.xml`'s
      // cache-path root addresses. `Directory.systemTemp` resolves through an
      // engine detail this code should not depend on, and the FileProvider
      // grant is now narrowed to exactly this subdirectory (MADR 0126 D6).
      final dir =
          widget.cacheDir ??
          (Directory(
            '${(await getTemporaryDirectory()).path}/mcremote_app_updates',
          )..createSync(recursive: true));
      final file = await _svc.downloadAndVerify(
        apk: r.apk!,
        sums: r.sums,
        cacheDir: dir,
        onProgress: (recv, total) {
          if (!mounted) return;
          setState(() {
            _progress = total == null || total == 0 ? null : recv / total;
            _status =
                'Downloading… ${total == null ? recv : '${(100 * recv / total).toStringAsFixed(0)}%'}';
          });
        },
      );
      if (!mounted) return;
      setState(() {
        _apkFile = file;
        _state = AppUpdateUiState.ready;
        _status = 'Ready to install ${r.remoteTag}';
        _progress = 1;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _state = AppUpdateUiState.error;
        _status = e.toString();
        _progress = null;
        _apkFile = null;
      });
    }
  }

  Future<void> install() async {
    final path = _apkFile?.path;
    if (path == null) {
      setState(() {
        _state = AppUpdateUiState.error;
        _status = 'No verified APK — download first';
      });
      return;
    }
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Restart to update'),
        content: const Text(
          'Android will install the new package. You may need to allow '
          '"Install unknown apps" for Magic CLI Remote the first time. '
          'Pairing is preserved across in-place upgrades.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Install'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    setState(() {
      _state = AppUpdateUiState.installing;
      _status = 'Handing off to installer…';
    });
    try {
      final install =
          widget.installApk ??
          (p, {bool preferSession = true}) async {
            const ch = MethodChannel(AppUpdateTile.channelName);
            await ch.invokeMethod<void>('installApk', {
              'path': p,
              'preferSession': preferSession,
            });
          };
      await install(path, preferSession: true);
      if (!mounted) return;
      setState(() {
        _state = AppUpdateUiState.ready;
        _status = 'Installer launched';
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _state = AppUpdateUiState.error;
        _status = e.toString();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final trailing = switch (_state) {
      AppUpdateUiState.checking ||
      AppUpdateUiState.installing => const SizedBox(
        width: 24,
        height: 24,
        child: CircularProgressIndicator(strokeWidth: 2),
      ),
      AppUpdateUiState.downloading => SizedBox(
        width: 24,
        height: 24,
        child: CircularProgressIndicator(strokeWidth: 2, value: _progress),
      ),
      _ => const Icon(Icons.system_update_alt),
    };

    VoidCallback? onTap;
    switch (_state) {
      case AppUpdateUiState.idle:
      case AppUpdateUiState.upToDate:
      case AppUpdateUiState.error:
        onTap = check;
      case AppUpdateUiState.available:
        onTap = download;
      case AppUpdateUiState.ready:
        onTap = install;
      case AppUpdateUiState.checking:
      case AppUpdateUiState.downloading:
      case AppUpdateUiState.installing:
        onTap = null;
    }

    return ListTile(
      leading: trailing,
      title: const Text('App update'),
      subtitle: Text(_status),
      onTap: onTap,
    );
  }
}

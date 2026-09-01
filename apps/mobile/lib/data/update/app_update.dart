import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:http/http.dart' as http;
import 'package:package_info_plus/package_info_plus.dart';

/// GitHub release check + APK download/verify for the phone (MADR 0065).
class AppUpdateService {
  AppUpdateService({
    http.Client? client,
    this.apiUrl =
        'https://api.github.com/repos/maccavelli/magic-cli-remote/releases/latest',
    Future<String> Function()? localVersion,
  }) : _client = client ?? http.Client(),
       _localVersion =
           localVersion ??
           (() async => (await PackageInfo.fromPlatform()).version);

  final http.Client _client;
  final String apiUrl;
  final Future<String> Function() _localVersion;

  /// Path of a successfully verified APK, if any (this process).
  File? verifiedApk;

  /// Whether [remote] is a strictly newer **published** version than [local],
  /// comparing major, minor, patch, then the build serial N.
  ///
  /// Mirrors Go's `update.NewerPublished` (`internal/update/version.go`), which
  /// is what `update/run.go` uses. It previously mirrored `NewerBase`, which
  /// compares three parts only and which nothing in Go calls any more since
  /// MADR 0103 — so a release differing from the installed build **only** in
  /// its serial (`0.15.3.1` → `0.15.3.2`) was invisible to the phone while the
  /// CLI on the same machine offered it (MADR 0126 F8, 0128 D2).
  ///
  /// Harmless while every release tag is three-part, which all 97 of them are
  /// today. This makes it stay correct when one is not.
  static bool isNewerPublished(String remote, String local) {
    final r = parseVersion(remote);
    final l = parseVersion(local);
    if (r == null || l == null) return false;
    if (r.major != l.major) return r.major > l.major;
    if (r.minor != l.minor) return r.minor > l.minor;
    if (r.patch != l.patch) return r.patch > l.patch;
    return r.n > l.n;
  }

  /// Parse `[v]MAJOR.MINOR.PATCH[.N]`, or null when it is not a published
  /// version. `n` is 0 for a three-part version, matching Go's zero value, so a
  /// three-part remote never appears newer than the same base with a serial.
  static AppVersion? parseVersion(String v) {
    var s = v.trim();
    if (s.startsWith('v')) s = s.substring(1);
    if (s.isEmpty || s == 'dev' || s == 'debug') return null;
    final parts = s.split('.');
    if (parts.length < 3) return null;
    final maj = int.tryParse(parts[0]);
    final min = int.tryParse(parts[1]);
    // The patch field may carry a local-build suffix; Go's leadingInt does the
    // same and flags it as Local. Here a suffix simply does not contribute.
    final patDigits = RegExp(r'^\d+').stringMatch(parts[2]);
    final pat = patDigits == null ? null : int.tryParse(patDigits);
    if (maj == null || min == null || pat == null) return null;
    var n = 0;
    if (parts.length >= 4) {
      final nDigits = RegExp(r'^\d+$').stringMatch(parts[3]);
      // A non-numeric or trailing-suffix 4th field means a local build; Go
      // sets Local and leaves N at 0. Compare it as 0 rather than refusing.
      n = nDigits == null ? 0 : (int.tryParse(nDigits) ?? 0);
    }
    return AppVersion(major: maj, minor: min, patch: pat, n: n);
  }

  Future<UpdateCheckResult> checkLatest() async {
    final local = await _localVersion();
    final resp = await _client.get(
      Uri.parse(apiUrl),
      headers: {
        'Accept': 'application/vnd.github+json',
        'User-Agent': 'magic-cli-remote-mobile',
      },
    );
    if (resp.statusCode != 200) {
      throw AppUpdateException('GitHub releases HTTP ${resp.statusCode}');
    }
    final body = jsonDecode(resp.body) as Map<String, dynamic>;
    final tag = (body['tag_name'] as String?)?.trim() ?? '';
    final assets = (body['assets'] as List<dynamic>? ?? const [])
        .cast<Map<String, dynamic>>();
    final apk = _pickApk(assets);
    final sums = _pickSums(assets);
    final available = isNewerPublished(tag, local);
    return UpdateCheckResult(
      localVersion: local,
      remoteTag: tag,
      updateAvailable: available,
      apk: apk,
      sums: sums,
    );
  }

  /// Streams the APK to [cacheDir], hashing chunks as they arrive (single pass).
  Future<File> downloadAndVerify({
    required UpdateAsset apk,
    required UpdateAsset sums,
    required Directory cacheDir,
    void Function(int received, int? total)? onProgress,
  }) async {
    final sumsResp = await _client.get(Uri.parse(sums.url));
    if (sumsResp.statusCode != 200) {
      throw AppUpdateException('checksums HTTP ${sumsResp.statusCode}');
    }
    final want = sha256For(apk.name, sumsResp.body);
    if (want == null) {
      throw AppUpdateException('no checksum entry for ${apk.name}');
    }

    final dest = File('${cacheDir.path}/${apk.name}');
    final req = http.Request('GET', Uri.parse(apk.url));
    final streamed = await _client.send(req);
    if (streamed.statusCode != 200) {
      throw AppUpdateException('apk HTTP ${streamed.statusCode}');
    }
    final sink = dest.openWrite();
    var received = 0;
    final total = streamed.contentLength;
    final digest = AccumulatorSink<Digest>();
    final hash = sha256.startChunkedConversion(digest);
    try {
      await for (final chunk in streamed.stream) {
        sink.add(chunk);
        hash.add(chunk);
        received += chunk.length;
        onProgress?.call(received, total);
      }
      await sink.close();
      hash.close();
    } catch (e) {
      await sink.close();
      hash.close();
      if (await dest.exists()) await dest.delete();
      rethrow;
    }
    final got = digest.events.single.toString();
    if (got.toLowerCase() != want.toLowerCase()) {
      await dest.delete();
      throw AppUpdateException('sha256 mismatch for ${apk.name}');
    }
    verifiedApk = dest;
    return dest;
  }

  UpdateAsset? _pickApk(List<Map<String, dynamic>> assets) {
    for (final a in assets) {
      final name = a['name'] as String? ?? '';
      if (name.endsWith('.apk') && name.contains('magic-cli-remote')) {
        return UpdateAsset(
          name: name,
          url: a['browser_download_url'] as String? ?? '',
          size: (a['size'] as num?)?.toInt() ?? 0,
        );
      }
    }
    return null;
  }

  UpdateAsset? _pickSums(List<Map<String, dynamic>> assets) {
    for (final a in assets) {
      final name = a['name'] as String? ?? '';
      if (name.startsWith('SHA256SUMS')) {
        return UpdateAsset(
          name: name,
          url: a['browser_download_url'] as String? ?? '',
          size: (a['size'] as num?)?.toInt() ?? 0,
        );
      }
    }
    return null;
  }

  /// Exported for tests.
  static String? sha256For(String assetName, String sumsBody) {
    for (final line in sumsBody.split('\n')) {
      final t = line.trim();
      if (t.isEmpty || t.startsWith('#')) continue;
      final fields = t.split(RegExp(r'\s+'));
      if (fields.length < 2) continue;
      var name = fields.last;
      if (name.startsWith('*')) name = name.substring(1);
      if (name == assetName) return fields.first;
    }
    return null;
  }
}

/// A parsed published version: `MAJOR.MINOR.PATCH.N`, N=0 when absent.
class AppVersion {
  const AppVersion({
    required this.major,
    required this.minor,
    required this.patch,
    required this.n,
  });
  final int major;
  final int minor;
  final int patch;
  final int n;
}

class UpdateAsset {
  const UpdateAsset({
    required this.name,
    required this.url,
    required this.size,
  });
  final String name;
  final String url;
  final int size;
}

class UpdateCheckResult {
  const UpdateCheckResult({
    required this.localVersion,
    required this.remoteTag,
    required this.updateAvailable,
    this.apk,
    this.sums,
  });
  final String localVersion;
  final String remoteTag;
  final bool updateAvailable;
  final UpdateAsset? apk;
  final UpdateAsset? sums;
}

class AppUpdateException implements Exception {
  AppUpdateException(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Collects digest events from [sha256.startChunkedConversion].
class AccumulatorSink<T> implements Sink<T> {
  final List<T> events = [];
  @override
  void add(T data) => events.add(data);
  @override
  void close() {}
}

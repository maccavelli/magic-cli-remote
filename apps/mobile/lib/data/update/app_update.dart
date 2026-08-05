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

  /// Three-part base compare (mirrors Go update.NewerBase).
  static bool isNewerBase(String remote, String local) {
    final r = parseBase(remote);
    final l = parseBase(local);
    if (r == null || l == null) return false;
    if (r.$1 != l.$1) return r.$1 > l.$1;
    if (r.$2 != l.$2) return r.$2 > l.$2;
    return r.$3 > l.$3;
  }

  static (int, int, int)? parseBase(String v) {
    var s = v.trim();
    if (s.startsWith('v')) s = s.substring(1);
    if (s.isEmpty || s == 'dev') return null;
    final parts = s.split('.');
    if (parts.length < 3) return null;
    final maj = int.tryParse(parts[0]);
    final min = int.tryParse(parts[1]);
    final patDigits = RegExp(r'^\d+').stringMatch(parts[2]);
    final pat = patDigits == null ? null : int.tryParse(patDigits);
    if (maj == null || min == null || pat == null) return null;
    return (maj, min, pat);
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
    final available = isNewerBase(tag, local);
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

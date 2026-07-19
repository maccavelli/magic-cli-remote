/// Parses [mcremote://pair?host=…&token=…] or […&code=…] payloads from QR codes.
class PairPayload {
  const PairPayload({
    required this.host,
    this.token,
    this.code,
  });

  final String host;
  final String? token;
  final String? code;

  bool get hasToken => token != null && token!.isNotEmpty;
  bool get hasCode => code != null && code!.isNotEmpty;

  /// Returns null if [raw] is not a valid pair URI.
  static PairPayload? tryParse(String raw) {
    final s = raw.trim();
    if (s.isEmpty) return null;

    final uri = Uri.tryParse(s);
    if (uri == null) return null;
    if (uri.scheme.toLowerCase() != 'mcremote') return null;

    final hostPart = uri.host.isNotEmpty
        ? uri.host
        : (uri.pathSegments.isNotEmpty ? uri.pathSegments.first : '');
    if (hostPart.toLowerCase() != 'pair') return null;

    final host = _stripHost(uri.queryParameters['host'] ?? '');
    final token = (uri.queryParameters['token'] ?? '').trim();
    final code = (uri.queryParameters['code'] ?? '').trim();
    if (host.isEmpty) return null;
    if (token.isEmpty && code.isEmpty) return null;
    return PairPayload(
      host: host,
      token: token.isEmpty ? null : token,
      code: code.isEmpty ? null : code,
    );
  }

  /// True if [raw] looks like an 8-char pair code (with optional hyphen).
  static bool looksLikePairCode(String raw) {
    final n = normalizePairCode(raw);
    if (n.length != 8) return false;
    return RegExp(r'^[2-9A-HJ-NP-Z]{8}$').hasMatch(n);
  }

  static String normalizePairCode(String raw) {
    return raw
        .toUpperCase()
        .replaceAll(RegExp(r'[\s\-_]'), '')
        .trim();
  }

  static String formatPairCode(String raw) {
    final n = normalizePairCode(raw);
    if (n.length != 8) return n;
    return '${n.substring(0, 4)}-${n.substring(4)}';
  }

  static String _stripHost(String host) {
    var h = host.trim();
    for (final p in ['ws://', 'wss://', 'http://', 'https://']) {
      if (h.toLowerCase().startsWith(p)) {
        h = h.substring(p.length);
        break;
      }
    }
    final slash = h.indexOf('/');
    if (slash >= 0) h = h.substring(0, slash);
    return h.trim();
  }
}

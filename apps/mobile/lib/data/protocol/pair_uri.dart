import 'dart:convert';
import 'dart:typed_data';

/// Canonical wire form of a certificate fingerprint: unpadded base64url of the
/// SHA-256 digest over the leaf certificate DER (43 chars).
const int kFingerprintBytes = 32;
const int kFingerprintB64Length = 43;

/// Normalizes a SHA-256 certificate fingerprint into the canonical unpadded
/// base64url form, or returns null if [raw] is not a SHA-256 digest.
///
/// Accepts what a human is plausibly holding: the base64url form from the pair
/// QR, lowercase/uppercase hex, colon-separated hex as printed by
/// `openssl x509 -fingerprint -sha256`, and an optional `sha256:` prefix.
String? normalizeFingerprint(String raw) {
  var s = raw.trim();
  if (s.isEmpty) return null;
  // Guard before any allocation: no accepted encoding of 32 bytes is longer
  // than colon-hex (95 chars) plus the prefix.
  if (s.length > 128) return null;
  if (s.toLowerCase().startsWith('sha256:')) {
    s = s.substring('sha256:'.length);
  }

  final compact = s.replaceAll(RegExp(r'[\s:]'), '');
  if (compact.length == kFingerprintBytes * 2) {
    final bytes = _tryDecodeHex(compact);
    if (bytes != null) return _encodeB64Url(bytes);
  }

  final trimmed = s.replaceAll('=', '');
  if (trimmed.length == kFingerprintB64Length) {
    final bytes = _tryDecodeB64Url(trimmed);
    if (bytes != null && bytes.length == kFingerprintBytes) {
      return _encodeB64Url(bytes);
    }
  }
  return null;
}

Uint8List? _tryDecodeHex(String s) {
  if (s.length.isOdd) return null;
  final out = Uint8List(s.length ~/ 2);
  for (var i = 0; i < out.length; i++) {
    final v = int.tryParse(s.substring(i * 2, i * 2 + 2), radix: 16);
    if (v == null) return null;
    out[i] = v;
  }
  return out;
}

Uint8List? _tryDecodeB64Url(String s) {
  // base64Url.decode requires canonical padding; base64 also rejects the
  // standard alphabet's +/ here, which is what we want for a URL-safe field.
  final padded = s.padRight((s.length + 3) & ~3, '=');
  try {
    return base64Url.decode(padded);
  } on FormatException {
    return null;
  }
}

String _encodeB64Url(List<int> bytes) => base64Url.encode(bytes).replaceAll('=', '');

/// Parses [mcremote://pair?host=…&token=…] or […&code=…] payloads from QR codes.
class PairPayload {
  const PairPayload({
    required this.host,
    required this.secure,
    this.token,
    this.code,
    this.fingerprint,
  });

  /// Connection input, ready to hand to `SettingsStore.parseEndpoint`.
  ///
  /// This is deliberately a single opaque string rather than a record: it is
  /// the only value that survives the whole onboarding path (QR → host text
  /// field → `SettingsStore.setHost` → `McremoteClient.connect`), so the
  /// transport (`ws://` / `wss://`) and the pinned certificate fingerprint
  /// (`#fp=…`) ride along inside it. `parseEndpoint` strips both back off.
  final String host;

  /// True when the pair URI selected TLS — either explicitly via `wss://` /
  /// `https://`, or by omission (bare hosts default to TLS, matching the
  /// daemon default).
  final bool secure;

  final String? token;
  final String? code;

  /// Canonical base64url SHA-256 of the daemon's TLS leaf certificate, or null
  /// when the QR carried no `fp` (a plaintext or externally-terminated host).
  final String? fingerprint;

  /// The bare `host[:port]` authority, without scheme prefix or fp fragment.
  String get hostAuthority {
    var h = host;
    final hash = h.indexOf('#');
    if (hash >= 0) h = h.substring(0, hash);
    final sep = h.indexOf('://');
    return sep >= 0 ? h.substring(sep + 3) : h;
  }

  bool get hasToken => token != null && token!.isNotEmpty;
  bool get hasCode => code != null && code!.isNotEmpty;

  /// Upper bounds on untrusted QR / clipboard input. A QR code can encode a few
  /// KB, so refuse anything that is obviously not a real pair payload rather
  /// than persisting it to secure storage.
  static const int maxRawLength = 2048;
  static const int maxHostLength = 255;
  static const int maxTokenLength = 512;
  static const int maxCodeLength = 64;

  /// Colon-hex (95) plus a `sha256:` prefix is the longest accepted encoding.
  static const int maxFingerprintLength = 128;

  /// Returns null if [raw] is not a valid pair URI.
  static PairPayload? tryParse(String raw) {
    if (raw.length > maxRawLength) return null;
    final s = raw.trim();
    if (s.isEmpty) return null;

    final uri = Uri.tryParse(s);
    if (uri == null) return null;
    if (uri.scheme.toLowerCase() != 'mcremote') return null;

    final hostPart = uri.host.isNotEmpty
        ? uri.host
        : (uri.pathSegments.isNotEmpty ? uri.pathSegments.first : '');
    if (hostPart.toLowerCase() != 'pair') return null;

    final rawHost = uri.queryParameters['host'] ?? '';
    if (rawHost.length > maxHostLength) return null;
    final parsedHost = _parseHost(rawHost);

    final token = (uri.queryParameters['token'] ?? '').trim();
    final code = (uri.queryParameters['code'] ?? '').trim();
    final rawFp = (uri.queryParameters['fp'] ?? '').trim();
    if (parsedHost.host.isEmpty) return null;
    if (token.isEmpty && code.isEmpty) return null;
    if (token.length > maxTokenLength) return null;
    if (code.length > maxCodeLength) return null;
    if (rawFp.length > maxFingerprintLength) return null;

    String? fingerprint;
    if (rawFp.isNotEmpty) {
      fingerprint = normalizeFingerprint(rawFp);
      // A malformed fp is a corrupt or tampered QR, not a hint to fall back to
      // an unpinned connection. Refuse the whole payload.
      if (fingerprint == null) return null;
    }

    final scheme = parsedHost.explicit
        ? (parsedHost.secure ? 'wss://' : 'ws://')
        : '';
    final fragment = fingerprint == null ? '' : '#fp=$fingerprint';

    return PairPayload(
      host: '$scheme${parsedHost.host}$fragment',
      secure: parsedHost.secure,
      token: token.isEmpty ? null : token,
      code: code.isEmpty ? null : code,
      fingerprint: fingerprint,
    );
  }

  /// True if [raw] looks like an 8-char pair code (with optional hyphen).
  static bool looksLikePairCode(String raw) {
    if (raw.length > maxCodeLength) return false;
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

  /// Splits an operator-supplied host into its security signal and authority.
  ///
  /// Both secure *and* insecure schemes are reported back rather than
  /// discarded: since bare hosts now default to TLS, dropping an explicit
  /// `ws://` would silently upgrade a deliberately-plaintext daemon and make
  /// the connection fail.
  static ({bool secure, bool explicit, String host}) _parseHost(String host) {
    var h = host.trim();
    var secure = true; // TLS by default — matches the daemon default.
    var explicit = false;
    const schemes = <String, bool>{
      'wss://': true,
      'https://': true,
      'ws://': false,
      'http://': false,
    };
    for (final entry in schemes.entries) {
      if (h.toLowerCase().startsWith(entry.key)) {
        secure = entry.value;
        explicit = true;
        h = h.substring(entry.key.length);
        break;
      }
    }
    final slash = h.indexOf('/');
    if (slash >= 0) h = h.substring(0, slash);
    return (secure: secure, explicit: explicit, host: h.trim());
  }
}

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

String _encodeB64Url(List<int> bytes) =>
    base64Url.encode(bytes).replaceAll('=', '');

/// Which TLS strategy the daemon serves with, and therefore which certificate
/// acceptance rule the client must apply.
///
/// | Mode | Rule | Trust set |
/// |---|---|---|
/// | [selfsigned] | fingerprint **only** | exactly 1 certificate |
/// | [letsencrypt] | valid chain **or** fingerprint match | public CAs for that name ∪ this certificate |
///
/// The fingerprint is carried in the pair QR in *both* modes. In `letsencrypt`
/// mode it is not load-bearing while ACME is healthy — the chain keeps
/// validating across 60-day renewals — but it is what makes the daemon's
/// self-signed fallback reachable when ACME fails. See ADR 0004.
enum TlsMode {
  selfsigned('selfsigned'),
  letsencrypt('letsencrypt');

  const TlsMode(this.wire);

  /// The value as it appears in the pair URI's `mode=` parameter.
  final String wire;

  /// The rule applied when a payload carries no `mode=`: every QR generated
  /// before the parameter existed was pinned-only, and pin-only is also the
  /// conservative reading of an unknown payload.
  static const TlsMode fallback = TlsMode.selfsigned;

  /// Returns null for an unrecognised mode. Callers must reject the payload
  /// rather than default, since the permissive rule is the wrong guess.
  static TlsMode? tryParse(String raw) {
    final s = raw.trim().toLowerCase();
    for (final m in TlsMode.values) {
      if (m.wire == s) return m;
    }
    return null;
  }
}

/// Parses [mcremote://pair?host=…&token=…] or […&code=…] payloads from QR codes.
class PairPayload {
  const PairPayload({
    required this.host,
    required this.secure,
    this.token,
    this.code,
    this.fingerprint,
    this.mode = TlsMode.fallback,
    this.relay,
    this.hostId,
  });

  /// Connection input, ready to hand to `SettingsStore.parseEndpoint`.
  ///
  /// This is deliberately a single opaque string rather than a record: it is
  /// the only value that survives the whole onboarding path (QR → host text
  /// field → `SettingsStore.setHost` → `McremoteClient.connect`), so the
  /// transport (`ws://` / `wss://`) and the pinned certificate fingerprint
  /// (`#fp=…`) ride along inside it. `parseEndpoint` strips both back off.
  ///
  /// Always the **mcremote** peer (mesh/LAN/direct), never the relay URL.
  final String host;

  /// True when the pair URI selected TLS — either explicitly via `wss://` /
  /// `https://`, or by omission (bare hosts default to TLS, matching the
  /// daemon default).
  final bool secure;

  final String? token;
  final String? code;

  /// Canonical base64url SHA-256 of the daemon's TLS leaf certificate, or null
  /// when the QR carried no `fp` (a plaintext or externally-terminated host).
  /// Describes **mcremote**, not mcrelay (MADR 0015 S13).
  final String? fingerprint;

  /// The daemon's TLS strategy, which selects the certificate acceptance rule.
  /// Defaults to [TlsMode.fallback] when the QR carried no `mode=`.
  final TlsMode mode;

  /// Optional outer mcrelay URL (`wss://relay.example.com`), from `relay=`.
  final String? relay;

  /// Public host registration id for relay join (`hid=`). Not secret.
  final String? hostId;

  /// True when both [relay] and [hostId] are present (MADR 0015).
  bool get hasRelay =>
      relay != null &&
      relay!.isNotEmpty &&
      hostId != null &&
      hostId!.isNotEmpty;

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

  /// Longest accepted `mode=` value ('letsencrypt' is 11).
  static const int maxModeLength = 32;

  static const int maxRelayLength = 255;
  static const int maxHostIdLength = 128;

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

    // queryParameters throws FormatException on malformed UTF-8 percent
    // encodings — and this runs on every camera frame against arbitrary QR
    // content. "tryParse" must actually not throw.
    final Map<String, String> qp;
    try {
      qp = uri.queryParameters;
    } on FormatException {
      return null;
    }

    final rawHost = qp['host'] ?? '';
    if (rawHost.length > maxHostLength) return null;
    final parsedHost = _parseHost(rawHost);

    final token = (qp['token'] ?? '').trim();
    final code = (qp['code'] ?? '').trim();
    final rawFp = (qp['fp'] ?? '').trim();
    final rawMode = (qp['mode'] ?? '').trim();
    final rawRelay = (qp['relay'] ?? '').trim();
    final rawHid = (qp['hid'] ?? '').trim();
    if (parsedHost.host.isEmpty) return null;
    if (token.isEmpty && code.isEmpty) return null;
    if (token.length > maxTokenLength) return null;
    if (code.length > maxCodeLength) return null;
    if (rawFp.length > maxFingerprintLength) return null;
    if (rawMode.length > maxModeLength) return null;
    if (rawRelay.length > maxRelayLength) return null;
    if (rawHid.length > maxHostIdLength) return null;
    // relay and hid must both be set or both empty (MADR 0015).
    if ((rawRelay.isEmpty) != (rawHid.isEmpty)) return null;
    if (rawHid.isNotEmpty && !_validHostId(rawHid)) return null;

    // An unrecognised mode is a QR from a newer daemon or a tampered one.
    // Guessing would mean guessing *which acceptance rule to relax*, so refuse
    // rather than silently applying the permissive one.
    final mode = rawMode.isEmpty ? TlsMode.fallback : TlsMode.tryParse(rawMode);
    if (mode == null) return null;

    String? fingerprint;
    if (rawFp.isNotEmpty) {
      fingerprint = normalizeFingerprint(rawFp);
      // A malformed fp is a corrupt or tampered QR, not a hint to fall back to
      // an unpinned connection. Refuse the whole payload.
      if (fingerprint == null) return null;
    }

    String? relay;
    if (rawRelay.isNotEmpty) {
      final rp = _parseHost(rawRelay);
      if (rp.host.isEmpty) return null;
      // Bare relay hosts default to wss (public edge is TLS-only by design).
      final rScheme = rp.explicit ? (rp.secure ? 'wss://' : 'ws://') : 'wss://';
      relay = '$rScheme${rp.host}';
    }

    final scheme = parsedHost.explicit
        ? (parsedHost.secure ? 'wss://' : 'ws://')
        : '';
    return PairPayload(
      host: '$scheme${parsedHost.host}${hostFragment(fingerprint, mode)}',
      secure: parsedHost.secure,
      token: token.isEmpty ? null : token,
      code: code.isEmpty ? null : code,
      fingerprint: fingerprint,
      mode: mode,
      relay: relay,
      hostId: rawHid.isEmpty ? null : rawHid,
    );
  }

  static bool _validHostId(String id) {
    if (id.isEmpty || id.length > maxHostIdLength) return false;
    return RegExp(r'^[A-Za-z0-9._-]+$').hasMatch(id);
  }

  /// The `#fp=…&mode=…` fragment appended to [host].
  ///
  /// Both values ride inside the single host string because that is the only
  /// value that survives the whole onboarding path (see [host]); the mode has
  /// to arrive wherever the pin does, or a reconnect would apply the wrong
  /// acceptance rule to it. [TlsMode.fallback] is left implicit so host strings
  /// produced by the pinned-only path are byte-identical to before.
  static String hostFragment(String? fingerprint, TlsMode mode) {
    final parts = <String>[
      if (fingerprint != null) 'fp=$fingerprint',
      if (mode != TlsMode.fallback) 'mode=${mode.wire}',
    ];
    return parts.isEmpty ? '' : '#${parts.join('&')}';
  }

  /// True if [raw] looks like an 8-char pair code (with optional hyphen).
  static bool looksLikePairCode(String raw) {
    if (raw.length > maxCodeLength) return false;
    final n = normalizePairCode(raw);
    if (n.length != 8) return false;
    return RegExp(r'^[2-9A-HJ-NP-Z]{8}$').hasMatch(n);
  }

  static String normalizePairCode(String raw) {
    return raw.toUpperCase().replaceAll(RegExp(r'[\s\-_]'), '').trim();
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

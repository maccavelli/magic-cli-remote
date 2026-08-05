/// Typed error from mcremote client handshakes and protocol responses.
class McException implements Exception {
  McException(
    this.message, {
    this.code,
    this.permanent = false,
    this.retryAfterMs,
  });

  final String message;

  /// Server or client error code (e.g. `invalid_token`, `expired`).
  final String? code;

  /// When true, auto-reconnect must not continue with these credentials.
  final bool permanent;

  /// Server hint (0068 P6): the earliest a retry can usefully happen —
  /// the rate-limit window remainder or a capacity estimate. Consumed as
  /// a floor on the next backoff delay, never as a promise of success.
  final int? retryAfterMs;

  bool get isInvalidToken => code == 'invalid_token';

  @override
  String toString() => message;
}

/// Human copy for session-operation failures, keyed on the daemon's stable
/// error codes. Falls back to the raw message (never "Exception: …" noise).
String friendlyOpError(Object e) {
  if (e is McException) {
    switch (e.code) {
      case 'session_limit':
        return 'The host is at its session limit — end one first.';
      case 'session_forbidden':
        return 'This session belongs to another device.';
      case 'session_not_live':
        return 'That session is no longer running on the host — start again from Sessions.';
      case 'rate_limited':
        return 'The host is rate-limiting requests — try again shortly.';
      case 'client_key_required':
      case 'client_key_mismatch':
        return 'The host now requires re-pairing this device.';
    }
    return e.message;
  }
  final s = e.toString();
  return s.startsWith('Exception: ') ? s.substring('Exception: '.length) : s;
}

/// Handshake failures that retrying with the same credentials cannot fix, so
/// auto-reconnect must stop rather than dial forever (`docs/protocol-v1.md`,
/// "auth_error frames" / "pair_error frames").
///
/// Everything else is transient by default. Treating the whole set as
/// permanent meant one `rate_limited` pair claim, or a single daemon-side
/// store hiccup during an overnight reconnect, silently parked background
/// reconnection until the user next opened the app (MADR 0046 L-3).
const _permanentAuthCodes = <String>{
  'invalid_token',
  'client_key_required',
  'client_key_mismatch',
  'bad_version',
  'unauthorized',
};

const _permanentPairCodes = <String>{
  'invalid_code',
  'expired',
  'unavailable',
  'client_key_required',
  'bad_version',
  'unauthorized',
};

bool _isPermanent(String? code, {required bool isPair}) =>
    code != null &&
    (isPair ? _permanentPairCodes : _permanentAuthCodes).contains(code);

/// True when [code] is a permanent verdict on **either** handshake — the union
/// of the pair and auth sets above.
///
/// Exposed for the transport-fallback classifier (`isTransportRetryable`,
/// MADR 0062 D10), which must not hop after a verdict that the other transport
/// would receive identically. The two questions stay separate: this set decides
/// whether *auto-reconnect* parks, and is deliberately narrow (MADR 0046 L-3);
/// the transport classifier layers its own, broader denylist on top.
bool isPermanentHandshakeCode(String? code) =>
    _isPermanent(code, isPair: false) || _isPermanent(code, isPair: true);

/// Map an auth/pair envelope into a typed exception, or null if success-shaped.
McException? handshakeErrorFrom(
  String expectedType,
  dynamic envelopeType,
  Map<String, dynamic>? payload, {
  required bool isPair,
}) {
  final type = envelopeType as String? ?? '';
  if (type == expectedType) return null;

  if (isPair && type == 'pair_error') {
    final code = payload?['code'] as String?;
    final msg = payload?['message'] as String? ?? 'pair claim failed';
    return McException(
      msg,
      code: code,
      permanent: _isPermanent(code, isPair: true),
    );
  }
  if (!isPair && type == 'auth_error') {
    final code = payload?['code'] as String? ?? 'auth_failed';
    final msg = payload?['message'] as String? ?? 'auth failed';
    return McException(
      msg,
      code: code,
      permanent: _isPermanent(code, isPair: false),
    );
  }
  // The daemon answers some handshake refusals with its generic `error`
  // envelope (over-limit auth, bad_version, unauthorized). It carries a real
  // message and code, so surface those instead of "unexpected auth response".
  if (type == 'error') {
    final code = payload?['code'] as String?;
    final msg =
        payload?['message'] as String? ??
        (isPair ? 'pair claim failed' : 'auth failed');
    return McException(
      msg,
      code: code ?? (isPair ? 'pair_failed' : 'auth_failed'),
      permanent: _isPermanent(code, isPair: isPair),
    );
  }

  return McException(
    isPair
        ? 'unexpected pair response: $type'
        : 'unexpected auth response: $type',
    code: isPair ? 'unexpected_pair_response' : 'unexpected_auth_response',
    permanent: true,
  );
}

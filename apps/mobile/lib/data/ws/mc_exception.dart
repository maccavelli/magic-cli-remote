/// Typed error from mcremote client handshakes and protocol responses.
class McException implements Exception {
  McException(this.message, {this.code, this.permanent = false});

  final String message;

  /// Server or client error code (e.g. `invalid_token`, `expired`).
  final String? code;

  /// When true, auto-reconnect must not continue with these credentials.
  final bool permanent;

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
        return 'Another paired device owns that session.';
      case 'session_not_live':
        return 'That session is no longer running on the host.';
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
    return McException(msg, code: code, permanent: true);
  }
  if (!isPair && type == 'auth_error') {
    final code = payload?['code'] as String? ?? 'auth_failed';
    final msg = payload?['message'] as String? ?? 'auth failed';
    return McException(msg, code: code, permanent: true);
  }

  return McException(
    isPair
        ? 'unexpected pair response: $type'
        : 'unexpected auth response: $type',
    code: isPair ? 'unexpected_pair_response' : 'unexpected_auth_response',
    permanent: true,
  );
}

/// Typed error from mcremote client handshakes and protocol responses.
class McException implements Exception {
  McException(
    this.message, {
    this.code,
    this.permanent = false,
  });

  final String message;

  /// Server or client error code (e.g. `invalid_token`, `expired`).
  final String? code;

  /// When true, auto-reconnect must not continue with these credentials.
  final bool permanent;

  bool get isInvalidToken => code == 'invalid_token';

  @override
  String toString() => message;
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

import 'pair_uri.dart';

/// How the phone should reach mcremote (MADR 0015).
enum ConnectionPathKind {
  /// Dial [PairPayload.host] directly (mesh / LAN / known reachability).
  direct,

  /// Outer TLS to [relayUrl], join [hostId], then inner TLS to mcremote
  /// through the opaque splice (MADR 0015 E3).
  relay,
}

/// Resolved path for a pair / reconnect attempt.
class ConnectionPath {
  const ConnectionPath._({
    required this.kind,
    required this.mcremoteHost,
    required this.secure,
    required this.fingerprint,
    required this.mode,
    this.relayUrl,
    this.hostId,
  });

  final ConnectionPathKind kind;

  /// mcremote dial string (scheme + authority + optional #fp fragment).
  final String mcremoteHost;
  final bool secure;
  final String? fingerprint;
  final TlsMode mode;

  /// Outer mcrelay URL when [kind] is [ConnectionPathKind.relay].
  final String? relayUrl;

  /// Public host registration id when using the relay.
  final String? hostId;

  bool get usesRelay => kind == ConnectionPathKind.relay;

  /// Prefer direct when [directReachable] is true or the pair has no relay.
  ///
  /// [directReachable] is supplied by a reachability probe (or user force).
  /// When relay fields exist and direct is not reachable, choose relay so
  /// off-mesh pairing uses outer join + inner TLS.
  factory ConnectionPath.resolve(
    PairPayload payload, {
    bool directReachable = false,
  }) {
    if (!payload.hasRelay || directReachable) {
      return ConnectionPath._(
        kind: ConnectionPathKind.direct,
        mcremoteHost: payload.host,
        secure: payload.secure,
        fingerprint: payload.fingerprint,
        mode: payload.mode,
      );
    }
    return ConnectionPath._(
      kind: ConnectionPathKind.relay,
      mcremoteHost: payload.host,
      secure: payload.secure,
      fingerprint: payload.fingerprint,
      mode: payload.mode,
      relayUrl: payload.relay,
      hostId: payload.hostId,
    );
  }
}

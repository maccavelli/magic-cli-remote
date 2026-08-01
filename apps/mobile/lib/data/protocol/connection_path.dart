import 'pair_uri.dart';

/// How the phone should reach mcremote (MADR 0015).
///
/// **No longer used to choose a path.** Since MADR 0062 the dial transport is
/// a [TransportMode] resolved per leg by the DialEpisode in `McremoteClient`,
/// which probes, can be overridden by the user, and falls back once. This type
/// remains as a description of what a [PairPayload] *offers* — useful for
/// reasoning about a QR in isolation, and exercised as such by the pair-URI
/// tests. Do not reintroduce it into the dial path: its `directReachable`
/// parameter has no way to express "the mesh dial failed, try the relay".
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

  /// Describe the path a pair payload will take **for display** (MADR 0062 P1).
  ///
  /// This no longer decides anything. Path choice belongs to the DialEpisode
  /// in `McremoteClient`, which resolves a [TransportMode] per leg and can fall
  /// back; a screen that asked this helper what would happen would be
  /// guessing, and before 0062 it guessed "relay" for every QR that carried a
  /// relay tuple — which is the caption that no longer matches reality.
  ///
  /// [directReachable] is retained for the pre-0062 callers and tests that
  /// treat this as a pure function of the payload. Prefer [describe] with the
  /// mode the client actually reports.
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

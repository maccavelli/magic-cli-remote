import '../ws/mc_exception.dart' show isPermanentHandshakeCode;

/// How the phone reaches mcremote for one dial leg (MADR 0062 D1).
///
/// Unlike the older [ConnectionPathKind], this is chosen *per leg* and is
/// always explicit: no code below the UI infers a path from "relay arguments
/// are present", which is what let a half-up mesh and an attempt-scoped relay
/// tuple fight over the route (0061, superseded).
enum TransportMode {
  /// Dial the mcremote authority directly (Tailscale / LAN).
  mesh,

  /// Outer hop to mcrelay, join, then inner TLS to mcremote through the
  /// loopback bridge. The pin and client certificate still apply end-to-end.
  relay,
}

extension TransportModeX on TransportMode {
  /// Stable wire/storage form. Never localise this — it is persisted.
  String get storageValue => this == TransportMode.mesh ? 'mesh' : 'relay';

  /// Human label for chips, menus and status lines.
  String get label => this == TransportMode.mesh ? 'Mesh' : 'Relay';

  TransportMode get other =>
      this == TransportMode.mesh ? TransportMode.relay : TransportMode.mesh;

  static TransportMode? fromStorage(String? value) => switch (value?.trim()) {
    'mesh' => TransportMode.mesh,
    'relay' => TransportMode.relay,
    _ => null,
  };
}

/// What each transport is configured for and whether it just answered a probe
/// (MADR 0062 D1).
///
/// *Configured* is a statement about credentials the app holds; *operational*
/// is a soft, session-ephemeral probe result. Both must hold before a
/// transport is offered in the menu, and neither is a permanent verdict — a
/// probe pass can still fail to join (`host_offline`), which is why
/// [isTransportRetryable] and the DialEpisode fallback exist.
class TransportAvailability {
  const TransportAvailability({
    required this.meshConfigured,
    required this.relayConfigured,
    required this.meshOperational,
    required this.relayOperational,
  });

  /// Nothing configured and nothing reachable — the `no_transport` state.
  static const none = TransportAvailability(
    meshConfigured: false,
    relayConfigured: false,
    meshOperational: false,
    relayOperational: false,
  );

  final bool meshConfigured;
  final bool relayConfigured;
  final bool meshOperational;
  final bool relayOperational;

  bool get meshAvailable => meshConfigured && meshOperational;
  bool get relayAvailable => relayConfigured && relayOperational;
  bool get bothAvailable => meshAvailable && relayAvailable;
  bool get noneAvailable => !meshAvailable && !relayAvailable;

  /// The only available transport, or null when neither or both are.
  TransportMode? get soleAvailable {
    if (meshAvailable && !relayAvailable) return TransportMode.mesh;
    if (relayAvailable && !meshAvailable) return TransportMode.relay;
    return null;
  }

  /// Both transports have credentials, regardless of probe outcome. This — not
  /// [bothAvailable] — gates the "Try Mesh / Try Relay" action after a dial
  /// failure, because a probe that failed is not proof the dial will (D2).
  bool get bothConfigured => meshConfigured && relayConfigured;

  TransportAvailability copyWith({
    bool? meshConfigured,
    bool? relayConfigured,
    bool? meshOperational,
    bool? relayOperational,
  }) => TransportAvailability(
    meshConfigured: meshConfigured ?? this.meshConfigured,
    relayConfigured: relayConfigured ?? this.relayConfigured,
    meshOperational: meshOperational ?? this.meshOperational,
    relayOperational: relayOperational ?? this.relayOperational,
  );

  @override
  String toString() =>
      'TransportAvailability(mesh: cfg=$meshConfigured op=$meshOperational, '
      'relay: cfg=$relayConfigured op=$relayOperational)';
}

/// Configured-ness from credentials alone — no I/O, no probes.
///
/// The relay counts as configured only when the URL *and* host id are present
/// **and** the stored relay authority matches the host being dialled. A relay
/// join hint is credentials for one daemon, never a generic fallback for
/// whatever host is typed next (MADR 0046 M-2).
TransportAvailability transportAvailabilityFromConfig({
  required String? host,
  required String? relayUrl,
  required String? relayHostId,
  required String? relayAuthority,
  required String? hostAuthority,
  bool meshOperational = false,
  bool relayOperational = false,
}) {
  final meshConfigured = (host?.trim().isNotEmpty ?? false);
  var relayConfigured =
      (relayUrl?.trim().isNotEmpty ?? false) &&
      (relayHostId?.trim().isNotEmpty ?? false);
  if (relayConfigured && meshConfigured) {
    final stored = relayAuthority?.trim() ?? '';
    // An empty stored authority is a route written before this field existed;
    // treat it as matching rather than silently disabling a working relay.
    if (stored.isNotEmpty && stored != (hostAuthority?.trim() ?? '')) {
      relayConfigured = false;
    }
  }
  return TransportAvailability(
    meshConfigured: meshConfigured,
    relayConfigured: relayConfigured,
    meshOperational: meshOperational,
    relayOperational: relayOperational,
  );
}

/// Interactive path choice — the menu, or the sole available transport
/// (MADR 0062 D3). Returns null for `no_transport`.
///
/// [userSelection] is honoured **only** when both transports are available,
/// i.e. only when the menu that produced it was actually on screen. A stale
/// selection (or sticky) must never send an interactive dial down a transport
/// that just failed its probe — the user cannot see, and therefore cannot
/// correct, a choice the UI is not showing.
TransportMode? resolveInteractive({
  required TransportAvailability availability,
  TransportMode? userSelection,
}) {
  final sole = availability.soleAvailable;
  if (sole != null) return sole;
  if (availability.noneAvailable) return null;
  // Both available: user's pick, else Mesh (cheaper, lower latency, no relay
  // bandwidth) as the documented default.
  return userSelection ?? TransportMode.mesh;
}

/// Primary transport for a dial with no UI attached — cold auto-connect,
/// lifecycle reconnect, the backoff timer (MADR 0062 D4).
///
/// Deliberately probe-free: background dials must not pay ~1s of probes on
/// every backoff tick, and a stale probe would be worse than none. The dial
/// itself is the probe, and [isTransportRetryable] plus the one-shot fallback
/// recover from a wrong guess.
TransportMode? resolveBackground({
  required TransportAvailability availability,
  TransportMode? sticky,
}) {
  if (sticky != null) {
    final configured = sticky == TransportMode.mesh
        ? availability.meshConfigured
        : availability.relayConfigured;
    if (configured) return sticky;
  }
  if (availability.meshConfigured) return TransportMode.mesh;
  if (availability.relayConfigured) return TransportMode.relay;
  return null;
}

/// Failures where dialling the *other* transport cannot help, and may do harm
/// (MADR 0062 D10).
///
/// These are credential, version and identity verdicts: the host said no, and
/// it will say no just as firmly over the other path. Hopping would only
/// double the noise — and for pair codes, spend a one-shot credential.
///
/// Kept separate from [isPermanentHandshakeCode] on purpose. That set governs
/// whether *auto-reconnect* parks (MADR 0046 L-3) and is deliberately narrow;
/// this set governs whether a *transport hop* is allowed. `rate_limited`, for
/// instance, must keep retrying in the background but must never hop.
const transportPermanentCodes = <String>{
  'invalid_token',
  'expired',
  'invalid_code',
  'cert_mismatch',
  'cert_unpinned',
  'client_key_required',
  'client_key_mismatch',
  'bad_version',
  'unauthorized',
  'unavailable',
  'no_credentials',
  'pair_failed',
  'unexpected_pair_response',
  'rate_limited',
};

/// Failures that say "this transport is unusable **as configured**".
///
/// The natural reading is "give up", but that is exactly backwards: a config
/// fault on one path is the strongest argument for trying the other. The
/// common trigger is mundane — sticky says relay, the stored route was cleared
/// on an authority change, and a perfectly reachable mesh sits unused
/// (MADR 0062 amendment A2).
const transportConfigErrorCodes = <String>{
  'relay_misconfigured',
  'no_transport',
};

/// Failures where one hop to the other transport is worth trying. Not
/// exhaustive — the classifier is a denylist, so anything unlisted is
/// retryable once (D10: prefer connectivity recovery over stranding).
const transportRetryableCodes = <String>{
  'connect_failed',
  'auth_timeout',
  'connection_lost',
  'host_offline',
  'tls_failed',
  'relay_join_failed',
  'relay_connect_failed',
  'relay_outer_error',
  'relay_buffer_overflow',
  'relay_peer_write_failed',
  'relay_protocol_violation',
};

/// Whether a DialEpisode may try the other transport after [code]
/// (MADR 0062 D10).
///
/// Denylist, not allowlist: an unknown code is retryable **once**. Stranding a
/// user on an unrecognised transient beats burning nothing but a second dial.
///
/// Note this answers only "is this code hop-worthy". Two further gates live in
/// the episode itself and are *not* expressible here: the one-shot credential
/// latch (a pair code already on the wire must never be re-sent, amendment A1)
/// and the per-generation fallback budget (D11).
bool isTransportRetryable(String? code) {
  if (code == null) return true;
  final normalized = code.trim();
  if (normalized.isEmpty) return true;
  // Checked first, and deliberately ahead of the permanent sets: these arrive
  // flagged `permanent: true` on the exception (that flag is about
  // auto-reconnect, not about transports) and would otherwise be denied a hop.
  if (transportConfigErrorCodes.contains(normalized)) return true;
  if (transportPermanentCodes.contains(normalized)) return false;
  // The union of the pair and auth permanent sets. Consulting both means a
  // pair-only verdict such as `expired` also blocks a hop on a token connect,
  // where it cannot normally arise. That fails *closed*, which is the correct
  // asymmetry: a missed hop costs one surfaced error, a wrong hop can cost a
  // credential (D10 / amendment A1).
  if (isPermanentHandshakeCode(normalized)) return false;
  return true;
}

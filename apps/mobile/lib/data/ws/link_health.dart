/// Liveness timings and the pure health classifier (MADR 0063).
///
/// Kept in its own file, free of imports, so both the transport layer and the
/// UI can share the constants without either depending on the other.
library;

/// How long a verified connection stays green.
const kLinkFreshFor = Duration(seconds: 15);

/// How long before an unverified connection is declared dead.
///
/// Comfortably inside the daemon's rolling read deadline, so the client always
/// reaches a definitive state — and starts recovering — before the host gives
/// up on the session.
///
/// The deadline is `limits.ws_read_deadline_seconds`
/// (`internal/config/config.go`): **120 s by default, floor 15 s**, and
/// advertised to v2 clients as `caps.read_deadline_ms`. An earlier version of
/// this comment said "half the daemon's 60 s read deadline" and cited a line
/// number that no longer holds the constant (MADR 0126 F4). Name the config
/// field, not the number — the number moves.
const kLinkDeadAfter = Duration(seconds: 30);

/// Cadence of the application-level `ping` request.
///
/// **Unconditional app `ping` is mandatory for every negotiated version**
/// (MADR 0063 B1; still required after 0068 P1). It is not only a UI probe:
///
/// 1. **UI freshness** — stamps `lastVerifiedAt` / [LinkHealth] (0063 D1).
/// 2. **v1 daemons** — the host's rolling read deadline advances only on
///    *data* frames; transport ping/pong frames never satisfy that read, so
///    the app ping is what keeps a quiet v1 session alive.
/// 3. **v2 daemons** also extend the horizon on completed *transport* pongs
///    (`internal/ws/liveness.go` pingLoop/`lastPong`;
///    `caps.ws_ping_resets_deadline: true`). That does **not** allow skipping
///    the app ping: version skew (v2 phone → v1 daemon), UI freshness, and
///    long inbound-only streams still need the data heartbeat.
///
/// Skipping this while inbound traffic "looks healthy" would kill long
/// streams against a v1 daemon and still damage 0063 UI truth on any version.
///
/// **What actually pins this value is [kLinkFreshFor], not the host deadline**
/// (MADR 0126 D5). [_noteInboundFrame] stamps `lastVerifiedAt` on *any* inbound
/// frame, so on an idle session the app ping is the only thing that verifies
/// the link — and anything above [kLinkFreshFor] would leave a perfectly
/// healthy idle session rendering amber. Deriving the period from the daemon's
/// 120 s deadline would give ~30 s and break the status indicator, which is why
/// `caps.read_deadline_ms` is consulted as a *guard* (see
/// `McremoteClient._checkPingCadenceAgainstCaps`) and never as a driver.
///
/// The invariant is pinned by a test: `kAppPingPeriod <= kLinkFreshFor`.
const kAppPingPeriod = Duration(seconds: 10);

/// Per-request bound for that ping.
const kAppPingTimeout = Duration(seconds: 6);

/// WebSocket protocol keepalive (MADR 0063 D2, amended to 20 s by plan B4).
///
/// A *backstop*, not the primary detector: dart:io closes the socket when a
/// pong does not arrive within this interval, which is what converts a
/// blackholed path into a real close event even when Dart timers are being
/// throttled. 20 s still closes inside [kLinkDeadAfter]; 10 s was the original
/// lock and merely doubled keepalive traffic on the metered relay path.
const kProtocolPingInterval = Duration(seconds: 20);

/// How confident the client is that the peer is still there (MADR 0063 D1).
///
/// Deliberately **orthogonal** to `McConnectionState` (plan amendment B2):
/// 47 call sites test `state == connected` to decide whether to deliver
/// notifications, sync sessions or arm a reconnect. `connected` keeps meaning
/// "socket adopted and authenticated"; this answers the separate question
/// "and is it still answering?".
enum LinkHealth {
  /// Verified within [kLinkFreshFor]. The only state that may render green.
  fresh,

  /// No proof of life for a while, but not long enough to call it dead.
  /// Advisory only — it never tears a socket down.
  stale,

  /// Past [kLinkDeadAfter], or the socket is gone.
  lost,
}

/// Classify a link from elapsed time alone.
///
/// Takes a [Duration] rather than a clock so every band is a table test with
/// no timers and no flake.
LinkHealth classifyLinkHealth({
  required Duration sinceVerified,
  required bool socketUp,
  Duration freshFor = kLinkFreshFor,
  Duration deadAfter = kLinkDeadAfter,
}) {
  // A closed socket is dead regardless of how recently it spoke: there is
  // nothing left to verify.
  if (!socketUp) return LinkHealth.lost;
  if (sinceVerified >= deadAfter) return LinkHealth.lost;
  if (sinceVerified >= freshFor) return LinkHealth.stale;
  return LinkHealth.fresh;
}

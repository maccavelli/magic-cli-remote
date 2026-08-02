/// Liveness timings and the pure health classifier (MADR 0063).
///
/// Kept in its own file, free of imports, so both the transport layer and the
/// UI can share the constants without either depending on the other.
library;

/// How long a verified connection stays green.
const kLinkFreshFor = Duration(seconds: 15);

/// How long before an unverified connection is declared dead.
///
/// Half the daemon's 60 s read deadline (`internal/ws/server.go:165`), so the
/// client always reaches a definitive state — and starts recovering — before
/// the host gives up on the session.
const kLinkDeadAfter = Duration(seconds: 30);

/// Cadence of the application-level `ping` request.
///
/// **This is a protocol obligation, not just a liveness probe** (MADR 0063
/// plan amendment B1). The daemon's read loop waits for a *data* message
/// (`internal/ws/server.go:535`); `coder/websocket` answers ping/pong frames
/// internally and they never satisfy that read. So this is the only thing
/// resetting the host's 60 s read deadline.
///
/// It must stay **unconditional**. Skipping it while inbound traffic looks
/// healthy would drop a session mid-answer during any reply longer than the
/// deadline, because a streaming session sends nothing upstream.
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

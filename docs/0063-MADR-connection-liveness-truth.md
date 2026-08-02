# MADR 0063: Connection status must be verified, not assumed

<!-- markdownlint-disable MD013 MD024 MD060 -->

- **Status**: Accepted — decisions locked 2026-08-01; **not implemented**.
  The four review questions are closed; see "Review decisions" at the end.
- **Date**: 2026-08-01
- **Deciders**: Project Owner
- **Scope**: Flutter client connection-state semantics and the status UI that
  renders them (`McremoteClient` liveness, sessions/chat banners). Does **not**
  change the daemon, the wire protocol, mcrelay, or the 0062 transport
  selection rules.
- **Related**:
  [0046-MADR](0046-MADR-reconnect-and-pairing-hardening.md) (reconnect loop,
  epoch supersession, the ping-flap rule L-1),
  [0062-MADR-phone-transport-selection.md](0062-MADR-phone-transport-selection.md)
  (per-leg `TransportMode`, `activeTransport`, DialEpisode failover),
  [protocol-v1.md](protocol-v1.md) (`ping` request; server read deadline).
- **Extends**: 0046's liveness machinery. Does not reopen the reconnect
  backoff, the epoch rules, or the permanent-error taxonomy.

---

## Problem

The sessions screen shows a green state light and **"Connected to `<host>`"**
whenever the client reports `McConnectionState.connected`
(`sessions_screen.dart:1198`, dot at `:1306`). Drop the mesh — turn Tailscale
off on the phone, or lose the tailnet route — and the light stays green, and
the label keeps naming a host the device can no longer reach.

It is not a rendering bug. The UI is faithfully reporting a state that is
itself wrong.

### Why the client does not notice

`McConnectionState.connected` currently means *"a socket object exists and the
last handshake succeeded"*. Nothing re-establishes that claim afterwards. When
a path disappears mid-session the failure is **silent** at every layer the
client can see:

| Layer | What a blackholed path does | Result |
|-------|-----------------------------|--------|
| TCP | No RST, no FIN — packets are dropped, not refused | Socket stays "open" |
| dart:io `WebSocket` | `pingInterval` is never set (`mcremote_client.dart:615`, `:715`; `relay_transport.dart:86`) | No protocol keepalive, so dart:io never closes it |
| `_onSocketDone` / `_onSocketError` (`:1776`, `:1765`) | Never fire — there is no close frame and no error | Reconnect never triggered |
| `sink.add` for the next request | Succeeds into the local send buffer | Writes look healthy |

So the **only** thing that can detect the loss is the application ping loop
(`_startPing`, `:1690`), and it is deliberately slow:

- period **20 s** (`:1694`)
- request timeout **12 s** (`:1704`)
- **two consecutive** misses required before acting (`:1713`)

That yields **32–52 s of a green light that is false**, measured from the
moment the path dies:

```text
drop just after a pong:  ping@20s → timeout@32s (miss 1)
                         ping@40s → timeout@52s (miss 2) → error   = 52 s
drop just after a send:  in-flight ping fails@12s (miss 1)
                         ping@20s → timeout@32s (miss 2) → error   = 32 s
```

…and longer whenever the OS defers the Dart timer, which is routine for a
backgrounded app. There is no upper bound the client enforces.

### The two ends disagree, and only the phone lies

The daemon *does* notice: `internal/ws/server.go:165` sets a 60 s
`ReadDeadline` and drops the client with `reason=read_deadline` — observed in
production logs during 0062 hardware testing. But that FIN cannot traverse a
dead path. The host correctly reclaims the session while the phone continues
to advertise it as live. The asymmetry is the point: the side with a *positive*
signal (bytes stopped arriving) acts; the side relying on the *absence* of an
error does not.

### Secondary problem: the status names no transport

Since 0062 a session runs over **mesh or relay**, and the banner says neither
(`_connLabel`, `:1174`). "Connected to host" cannot distinguish:

- healthy on the mesh,
- healthy on the relay after a successful failover,
- stale on a mesh that died 40 seconds ago.

A user who has just switched transports, or just lost one, is given no way to
tell which of those they are looking at. That is what makes the false green
*confusing* as well as wrong — the reporter of this issue described it as "a
green indicator for mesh", and the UI never actually said mesh.

---

## Decision drivers

1. **A status light is a claim.** Green must mean "verified recently", not
   "not yet disproven". Silence is not health.
2. **Mobile power budget.** Heartbeats cost radio wakeups; the cadence has to
   buy real detection, not reassurance.
3. **Do not re-introduce flap.** 0046 L-1 fixed a bug where a dying ping
   bounced the state of a healthy handshake. Faster detection must not restore
   it.
4. **Post-0062, mesh loss is often recoverable.** With a relay configured, the
   right answer to "the mesh died" is usually *fail over*, not merely *go red*.
5. **Do not depend solely on app timers.** They are throttled exactly when the
   app is least able to notice — in the background.
6. **Stay inside the daemon's 60 s read deadline** so the client re-affirms
   liveness well before the host gives up on it.

---

## Decision outcome (locked)

### D1 — `connected` means *verified within `kFreshFor`*

Track `lastVerifiedAt`, refreshed by **any** inbound frame from the daemon —
pong, event, or response — not only by an explicit ping reply. A session that
is streaming a reply is proving itself continuously and should not also be
paying for pings.

Expose a derived health level rather than a bare boolean:

```text
fresh      lastVerifiedAt <= kFreshFor          → green
stale      kFreshFor < age <= kDeadAfter        → amber, "Checking connection…"
lost       age > kDeadAfter, or socket closed   → red
```

Green becomes a positive assertion with an expiry. The amber band is what
removes the lie without introducing a red flash for every transient blip.

**Locked:** `kFreshFor = 15 s`, `kDeadAfter = 30 s`.

Chosen against the daemon's 60 s `ReadDeadline`: red at 30 s means the client
always reaches a definitive state — and can start recovering — before the host
gives up on the session, so the two ends never disagree for long. With the 10 s
heartbeat of D3 that is one missed beat to amber and three to red, which is
enough tolerance that a single dropped packet on a lossy mobile link does not
repaint the UI.

### D2 — Protocol-level keepalive on every WebSocket

Set `pingInterval` on the inner socket and on the relay's outer socket. Per
`web_socket_channel`: *"If a ping message is not answered by a pong message
from the peer, the WebSocket is assumed disconnected and the connection is
closed."*

This is the highest-value change in this document, because it converts a
silent blackhole into a **real close event** — which the existing
`_onSocketDone` path already handles correctly. No new failure plumbing, and
it works without the app-level loop being scheduled on time.

**Locked:** `pingInterval = 10 s` on **both** hops — inner (phone → mcremote)
and outer (phone → mcrelay). The inner catches a dead daemon or a dead tunnel;
the outer catches a dead relay edge directly rather than inferring it from an
inner timeout.

The buffer concern raised during review does not apply: `kRelayOuterBufferMax`
(64 frames / 256 KiB, `relay_transport.dart:15`) stages bytes only *before* the
loopback peer attaches — in steady state frames are spliced straight through,
not queued. Protocol pings are also answered by the WebSocket layer beneath the
application, so they never reach `_onOuterData` at all.

### D3 — Tighten the app-level heartbeat, and surface the first miss

The app ping stays (it validates the *protocol*, not just the socket), but:

| | Now | Proposed |
|---|---|---|
| period | 20 s | 10 s |
| request timeout | 12 s | 6 s |
| first miss | invisible | → **amber** |
| second miss | → error | → error (unchanged) |

Worst-case detection drops from 32–52 s to roughly 12–22 s, and — more
importantly — the UI stops claiming health at the *first* miss rather than the
second. Tearing the socket down still requires two misses, so 0046 L-1's
anti-flap rule is preserved: what changes is what we *show*, not what we
*bounce*.

### D4 — Treat losing the transport's own interface as a strong signal

`app_lifecycle.dart:92` currently treats every connectivity change as routine
churn and probes with a 5 s timeout. When `activeTransport == mesh` and the
change *removes* the VPN/tailnet interface, that is not churn — it is the most
specific evidence available that this particular transport just died.

**Proposed:** in that case go amber immediately and probe with a 2 s timeout.
Interface loss for a transport we are *not* using stays routine.

### D5 — The status names the transport, and names the failure

- `Connected to <host> over Mesh` / `… over Relay`
- `Mesh connection lost — reconnecting over Relay`
- `Checking connection…` for the amber band

Reuses `activeTransport` from 0062; no new state is needed to say this.

**Locked: the amber band appears on the chat screen as well as sessions.** Chat
is where a dead link is actually noticed — a prompt is sent and nothing comes
back — and today that screen shows only linking/offline, so a stale connection
leaves the composer looking perfectly healthy. Amber there explains the silence
instead of letting the user blame the agent.

### D6 — A dead mesh should attempt failover, not just report

A drop while `activeTransport == mesh` with a relay configured for this
authority should reconnect through a DialEpisode with **relay** as primary,
rather than retrying the path that just failed. 0062 already provides the
episode, the sticky value, and the one-hop budget; this is a matter of
choosing the primary on a *reconnect after transport loss*, which today
defaults to the sticky value — i.e. the transport that just died.

**Locked: a transport-loss failover is exempt from the per-generation fallback
budget** (0062 D11), on the same reasoning as amendment A5 for user-initiated
episodes. The budget exists to stop a machine thrashing on *blind* retries; a
transport-loss failover is not blind — it is a response to a specific observed
event, the death of the path currently carrying the session. Making it consume
the budget produces the worst possible outcome: during a connectivity storm the
budget is already spent, so a genuine mesh death cannot fail over and the user
sits red with a working relay one hop away.

This is an **amendment to 0062 D11** and must be recorded there when this is
implemented. The flap risk it accepts — a mesh that oscillates could switch
transports repeatedly — is bounded by the fact that each switch requires an
actual dead transport, and is called out in V9/V10 below rather than
pre-emptively rate-limited.

### D7 — Never render green while a dial is in flight

`connecting`, `authenticating` and any in-flight episode render as linking.
Today a reconnect can briefly repaint green between legs because the socket
exists before the handshake completes.

---

## Consequences

**Good**

- Green stops lying. The worst case falls from *unbounded* (throttled timers,
  blackholed path) to `kDeadAfter`, and the common case to ~10–20 s.
- Detection no longer depends solely on Dart timer scheduling (D2).
- The user can finally see *which* transport is carrying the session, which is
  the missing half of 0062's UX.
- A recoverable mesh loss becomes a transport switch rather than an outage
  (D6).

**Costs and risks**

- **Power.** Two keepalives (protocol + app) at 10 s. Mitigation: the app ping
  is skipped whenever `lastVerifiedAt` is already fresh (D1 makes inbound
  traffic count), so an active session pays almost nothing; an idle one pays
  one small frame per 10 s per hop.
- **More transitions.** The amber band adds a state the UI must render calmly;
  done badly it flickers. Mitigation: amber is advisory and never triggers a
  teardown.
- **Transport flap.** D6's budget exemption means an oscillating mesh can move
  the session between transports repeatedly. Accepted deliberately: each switch
  requires a transport that is actually dead, and the alternative (consuming the
  budget) fails in the worse direction. Watched by V10.

The relay buffer risk raised in review was investigated and dismissed — see D2.

---

## Alternatives considered (rejected)

| Option | Why rejected |
|--------|--------------|
| TCP keepalive (`SO_KEEPALIVE` via `RawSocketOption`) | Platform-specific, not portably exposed by dart:io, and the OS default idle is minutes — far too coarse for a status light. |
| Trust `connectivity_plus` alone | It reports interface *classes*, not reachability. A VPN can be up with no route to the host, and a tailnet can be half-up — 0062 §0.3 already documents the mesh false-positive. |
| Poll `/healthz` over HTTP | A second connection with different path characteristics. It can succeed while the live WebSocket is dead, which would make the light *more* wrong, not less. |
| Only shorten the ping timeout | Does not help a blackhole: writes still succeed locally and nothing ever returns. Halves one term of a sum whose real problem is requiring two misses. |
| Show green optimistically, correct on failure | This is the current behaviour and the subject of this MADR. |

---

## Verification plan

| # | Check | Level |
|---|-------|-------|
| V1 | Freshness clock: fresh → amber → red at the configured thresholds | unit |
| V2 | Inbound event refreshes `lastVerifiedAt` without a ping | unit |
| V3 | A missed protocol pong closes the socket and lands in `error` | fake socket |
| V4 | One missed app ping goes amber but does **not** tear down (0046 L-1) | fake socket |
| V5 | Mesh-transport drop with a relay configured reconnects over relay | fake relay (extends `relay_inner_tls_test.dart` harness) |
| V6 | Status text names the active transport, on sessions **and** chat | widget |
| V7 | Turn Tailscale off mid-session → amber ≤ ~10 s, red or relay-failover ≤ 30 s | hardware |
| V8 | Kill the relay mid-session on a relay-carried session → same bounds | hardware |
| V9 | Idle overnight on a healthy link → no spurious amber; battery delta acceptable | hardware |
| V10 | Flap a mesh repeatedly (D6 exemption) → transport switches are bounded and the session stays usable | hardware |

V7 and V8 are the ones that matter: every other check can pass against a fake
that politely closes its socket, which is precisely what the real failure does
not do.

---

## Review decisions (closed 2026-08-01)

| # | Question | Decision |
|---|----------|----------|
| 1 | Freshness bands vs the daemon's 60 s read deadline | **15 s / 30 s** — red at half the daemon's deadline, so the client reacts before the host gives up (D1) |
| 2 | Amber on chat, or sessions only | **Both** — chat is where a dead link is noticed, and today it looks healthy (D5) |
| 3 | Does transport-loss failover consume the 0062 D11 budget | **Exempt**, as A5 does for user-initiated episodes; flap risk accepted and watched by V10. Amends 0062 D11 (D6) |
| 4 | Which hops get `pingInterval` | **Both** — the buffer concern was investigated and does not apply (D2) |

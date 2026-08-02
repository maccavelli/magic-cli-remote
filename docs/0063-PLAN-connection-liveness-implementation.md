# MADR 0063 — Implementation plan: connection liveness truth

<!-- markdownlint-disable MD013 MD024 MD060 -->

Companion to
[0063-MADR-connection-liveness-truth.md](0063-MADR-connection-liveness-truth.md).
This is the **review and build plan**: it re-grounds the MADR against the
current tree (Flutter *and* Go), names concrete APIs, files and tests,
sequences the work so the highest-value fix ships first, and defines
acceptance gates.

- **Status:** **Reviewed and accepted 2026-08-01** — not implemented.
  Amendments B1–B5 accepted as written; the four sequencing/cadence questions
  are closed (see §7). **P0 ships alone and is validated on hardware before
  P1–P4 begin.**
- **Date:** 2026-08-01
- **Scope:** Flutter client (`apps/mobile`) only. No daemon, mcrelay, or wire
  protocol changes.
- **MADR lock-ins (must not regress):** D1–D7 and the four closed review
  decisions (15 s/30 s bands, amber on both screens, failover exempt from the
  0062 D11 budget, `pingInterval` on both hops).
- **Plan amendments proposed here:** **B1** the app-level ping is a protocol
  obligation and must stay unconditional; **B2** health is an orthogonal signal,
  not a new `McConnectionState`; **B3** split the two keepalive cadences instead
  of running both at 10 s. See §0.3 — B1 is blocking.
- **Standards:** project `AGENTS.md`; `dart format`; `flutter analyze` +
  targeted `flutter test` before commit.

---

## 0. Assessment vs codebase (grounding)

### 0.1 What already exists (reuse)

| Capability | Location | Notes |
|------------|----------|-------|
| Connection state stream | `McremoteClient._connection` → `connectionStateProvider` (`app_providers.dart:139`) | Seeds current state, then streams |
| App-level ping loop | `_startPing` (`mcremote_client.dart:1690`) | 20 s period, 12 s timeout, 2 misses |
| One-shot liveness probe | `probeLiveness` (`:1744`) | 5 s timeout; used on resume/connectivity |
| Socket close/error paths | `_onSocketDone` (`:1776`), `_onSocketError` (`:1765`) | Already reconnect correctly **when they fire** |
| Inbound frame handler | `_onMessage` (`:2031`) | The natural place to stamp freshness |
| Active transport | `activeTransport` (0062) | Non-null only while connected |
| DialEpisode + sticky + budget | 0062 D4/D11 | Reused wholesale by D6 |
| Connectivity trigger | `app_lifecycle.dart:92` | Probes liveness on interface change |
| Status UI | `sessions_screen.dart:1198` (`healthy`), dot `:1306`, `_connLabel` `:1174` | Green iff `state == connected` |
| Chat banner | `chat_screen.dart:1962` (`linking`/`offline`) | Two states only |
| Banner widget | `ConnBannerKind { linking, offline }` (`theme/widgets.dart:139`) | Needs a third variant |

### 0.2 Gaps the MADR requires (not present today)

| Gap | Today | Required |
|-----|-------|----------|
| Freshness clock | none | `lastVerifiedAt`, stamped on every inbound frame |
| Health signal | `connected` boolean | fresh / stale / lost, exposed to UI |
| Protocol keepalive | `pingInterval` never set | set on inner + outer sockets |
| Amber colour | `CelestialColors` has `success` only (`celestial.dart:32/54/67`) | add `caution` to both palettes |
| Third banner kind | `linking`, `offline` | add `degraded` |
| Transport in status | not shown anywhere | "over Mesh" / "over Relay" |
| Transport-loss failover | reconnect reuses sticky = the dead path | prefer the alternate |
| Interface-loss urgency | every change = routine, 5 s probe | mesh VPN loss ⇒ 2 s probe + amber |

### 0.3 Critical findings that change the MADR

Four things the MADR assumed that the code does not support. **B1 is blocking
and would cause a regression if the MADR were implemented literally.**

#### B1 — The app-level ping is a *protocol obligation*, not just a probe (BLOCKING)

MADR D1 says a streaming session "is proving itself continuously and should not
also be paying for pings". That is true for the **UI freshness signal** and
false for the **wire**. The daemon's read loop is:

```go
// internal/ws/server.go:535
readCtx, cancel = context.WithTimeout(ctx, s.readDeadline) // 60 s (:165)
msgType, data, err := conn.Read(readCtx)
```

`coder/websocket`'s `Read` returns on a **data** message. Control frames —
including pings — are handled internally (`read.go:201` → `handleControl` →
`:317 opPing`) and never satisfy that read. So:

- **`pingInterval` cannot replace the app ping.** Protocol pings do not reset
  the daemon's 60 s deadline.
- **Suppressing the app ping while inbound traffic is fresh would kill long
  streams.** During a 60 s+ agent reply the phone receives many events and
  sends nothing; today's unconditional `Timer.periodic` (`:1694`) is the only
  thing resetting the daemon's deadline. Make the ping conditional on
  freshness and the daemon drops the session mid-answer — a *worse* bug than
  the one being fixed, and one that would look like the agent hanging.

**Resolution (accepted):** client-only. The outbound app ping stays
**unconditional** at a period comfortably under 60 s. D1's "inbound frames
count" applies to `lastVerifiedAt` and the UI only. Written into the code as a
named constant whose comment cites `internal/ws/server.go:535` and the 60 s
deadline, so a later battery optimisation cannot quietly remove it — that is
the whole failure mode this amendment exists to prevent.

**Deliberately not fixed here:** making the daemon's read deadline
control-frame aware would remove the obligation entirely and let the app ping
slow down. It was considered and deferred, for two reasons: it is a daemon
change (out of this plan's scope, §0.4) and it creates a version-skew window
where a new phone talking to an old daemon still needs the data heartbeat — so
the client-side obligation has to exist regardless. If that daemon change is
ever made, this constant is the thing to revisit, and G4 is the test that
proves it is still needed.

#### B2 — Health must be orthogonal to `McConnectionState`, not a new value

47 call sites across 10 files compare `== McConnectionState.connected` —
`notification_coordinator`, `session_synchronizer`, `transcripts_notifier`,
`lifecycle_policy`, three screens. Redefining `connected` to mean "fresh" would
silently change notification delivery, session sync and reconnect eligibility.
(Only two files switch *exhaustively*, so adding a value is cheap for the
compiler and expensive for behaviour — the dangerous combination.)

**Resolution:** leave `McConnectionState` alone. Add an orthogonal
`LinkHealth { fresh, stale, lost }`. `connected` keeps meaning "socket adopted
and authenticated"; health answers "and is it answering?". UI reads both.

#### B3 — Freshness expiry fires no event

`connectionStateProvider` is fed by the `_connection` `StreamController`.
Nothing emits when a clock crosses 15 s, so a UI bound only to that stream
would sit green forever — the same bug, one layer up.

**Resolution:** the client owns the clock. The heartbeat timer that already
runs re-evaluates health each tick and pushes changes to a
`ValueNotifier<LinkHealth>`. No UI-side ticker, no `Timer` in a widget, and
tests stay deterministic (drive the client, not the wall clock).

#### B4 — Two 10 s keepalives on two hops is more traffic than the MADR intends

The MADR locks `pingInterval = 10 s` (D2) *and* app ping 10 s (D3). On a relay
session that is three keepalives per 10 s (inner protocol, outer protocol, app
ping over the tunnel). They do different jobs and do not need the same cadence:

| Keepalive | Job | Proposed |
|-----------|-----|----------|
| App `ping` request | Drives `lastVerifiedAt`; resets the daemon's 60 s deadline (B1); bumps mcrelay's 5 min splice-idle | **10 s** (unchanged from D3) |
| `pingInterval`, inner + outer | Backstop that *closes the socket* when Dart timers are throttled | **20 s** |

20 s still closes a dead socket inside `kDeadAfter` (30 s) and halves the
backstop traffic on the metered path.

**Accepted** — this amends MADR D2, which locked 10 s. Recorded in
0063-MADR-D2. 30 s was rejected: a throttled-timer blackhole could then exceed
`kDeadAfter` before the socket closed, leaving red to be driven by the
freshness clock rather than a real close, which is the weaker signal.

#### B5 — `ConnectivityResult.vpn` is unreliable on Apple platforms

D4 keys urgency on the VPN interface disappearing.
`connectivity_plus_platform_interface`'s own doc:

> Note for iOS and macOS: There is no separate network interface type for
> `vpn`. It returns `other` on any device (also simulator).

Android — the actual target — does report `vpn`. **Resolution:** treat VPN
disappearance as an *accelerator*, never a precondition. If the signal is
absent the normal path still detects the loss via B1/D2/D3; D4 only makes it
faster where the platform cooperates.

#### 0.3.1 Confirmed-good assumptions (checked, not assumed)

- **The daemon and mcrelay both auto-pong.** Both use `coder/websocket`
  v1.8.15, whose read path handles ping/pong/close internally
  (`read.go:21,201,317`). D2 will not kill healthy connections.
- **`pingInterval` composes with `customClient`** — both are parameters of
  `IOWebSocketChannel.connect` (`web_socket_channel-3.0.3/lib/io.dart:36-49`),
  so the pinned client and the relay's `connectionFactory` are unaffected.
- **mcrelay's splice idle is 5 min**, bumped per transferred frame
  (`relay/server.go:702`, `config.go:60`). A 10 s inner app ping keeps it warm
  with three orders of magnitude of headroom.

### 0.4 Out of scope

- Daemon, mcrelay, or protocol changes (including making the daemon's read
  deadline control-frame-aware — that would be the *right* long-term fix for
  B1 and belongs in its own MADR).
- Reconnect backoff curve, epoch rules, permanent-error taxonomy (0046).
- Transport *selection* policy (0062) beyond D6's primary choice.
- Background/foreground heartbeat modulation for battery — see §8.

---

## 1. Target architecture

```text
   inbound frame ─┐
   ping reply ────┼──► lastVerifiedAt ──┐
   any response ──┘                     │
                                        ▼
   heartbeat timer (10 s) ──► evaluate ──► LinkHealth {fresh|stale|lost}
        │                                        │
        │ sends app `ping` unconditionally       ├──► ValueNotifier (UI)
        │ (B1: resets daemon 60 s deadline)      └──► lost ⇒ teardown + episode
        ▼
   pingInterval 20 s (dart:io, both hops)
        └── missed pong ⇒ socket closed ⇒ _onSocketDone ⇒ existing reconnect
```

Two independent detectors, deliberately: the timer path gives graded UI truth,
the protocol path gives a hard close even when timers are throttled.

### 1.1 New types

**File:** `apps/mobile/lib/data/ws/link_health.dart`

```dart
enum LinkHealth { fresh, stale, lost }

/// Pure classifier — no clock, no I/O, so it is fully testable.
LinkHealth classifyLinkHealth({
  required Duration sinceVerified,
  required bool socketUp,
  Duration freshFor = kLinkFreshFor,   // 15 s
  Duration deadAfter = kLinkDeadAfter, // 30 s
});

const kLinkFreshFor = Duration(seconds: 15);
const kLinkDeadAfter = Duration(seconds: 30);

/// Unconditional: resets the daemon's 60 s read deadline (B1).
const kAppPingPeriod = Duration(seconds: 10);
const kAppPingTimeout = Duration(seconds: 6);

/// Backstop only — dart:io closes the socket on a missed pong (B4).
const kProtocolPingInterval = Duration(seconds: 20);
```

### 1.2 Client additions

| Member | Role |
|--------|------|
| `DateTime? _lastVerifiedAt` | Stamped in `_onMessage` before any parsing, so a malformed frame still counts as proof of life |
| `ValueNotifier<LinkHealth> linkHealth` | UI signal; disposed with `dialProgress` |
| `void _noteInboundFrame()` | Single stamp point |
| `void _evaluateHealth()` | Called each heartbeat tick and on state changes |
| `bool _preferAlternateNextDial` | One-shot, set on transport-loss (D6) |

---

## 2. Phased delivery

Ordered so the largest truth-gain lands first and each phase is shippable
alone.

### P0 — Protocol keepalive (D2) — **ships and is validated alone**

The whole of D2, and on its own it converts a silent blackhole into a real
close. Smallest diff, largest single improvement, no UI change.

**Locked sequencing:** P0 is tagged and put on hardware **before P1–P4 are
written**. It rides the same build as the outstanding 0062 §6.4 (G7) checklist,
so one round of device testing covers both. P1–P4 are then built against
measured detection latency instead of the estimates in this document — and if
P0 alone proves sufficient in practice, the later phases can be re-scoped
rather than assumed.

- `pingInterval: kProtocolPingInterval` on `_openSocketDirect` (`:615`),
  `_openSocketViaRelay` (`:715`), `RelayTransport.open` (`relay_transport.dart:86`)
- **Exit:** fake-socket test — missed pong ⇒ `onDone` ⇒ `error` ⇒ reconnect
  scheduled; and a healthy socket survives ≥ 3 ping cycles (guards against
  the daemon *not* ponging, which would make this change catastrophic)

### P1 — Freshness clock + health signal (D1, B2, B3)

- `link_health.dart` + classifier tests
- `_lastVerifiedAt` stamped in `_onMessage`; `linkHealth` notifier
- Heartbeat re-evaluates health each tick
- **No UI change yet** — signal only, so it can be verified in isolation
- **Exit:** health transitions fresh→stale→lost on a driven clock; an inbound
  event resets to fresh without a ping

### P2 — Heartbeat cadence + first-miss visibility (D3, B1)

- Period 20 s → 10 s, timeout 12 s → 6 s
- First miss ⇒ `stale`; second ⇒ teardown (**unchanged** — preserves 0046 L-1)
- Ping stays unconditional, with B1 written into the comment
- **Exit:** one miss does not tear down; two do; a 90 s simulated one-way
  stream never skips a ping (the B1 regression test)

### P3 — UI truth (D5, D7, amber)

- `CelestialColors.caution` in both palettes; `ConnBannerKind.degraded`
- Sessions: green only when `connected && fresh`; amber strip when `stale`
- Chat: same amber band (locked review decision 2)
- Labels name the transport: "Connected to `<host>` over Mesh"
- Never green while a dial is in flight (D7)
- **Exit:** widget tests per state; golden-free (colour asserted via theme)

### P4 — Transport-aware recovery (D4, D6)

- Mesh interface loss ⇒ amber immediately + 2 s probe (D4, with B5 caveat)
- Transport-loss reconnect prefers the alternate; exempt from the 0062 D11
  budget (locked review decision 3)
- Status: "Mesh connection lost — reconnecting over Relay"
- **0062 D11 amendment recorded in that MADR**
- **Exit:** fake-relay test — mesh socket dies ⇒ next episode primary is relay,
  budget untouched

---

## 3. File-level checklist

| Path | P0 | P1 | P2 | P3 | P4 |
|------|----|----|----|----|-----|
| `lib/data/ws/link_health.dart` | | **C** | | | |
| `lib/data/ws/mcremote_client.dart` | U | **U** | **U** | | U |
| `lib/data/ws/relay_transport.dart` | U | | | | |
| `lib/theme/celestial.dart` | | | | U | |
| `lib/theme/widgets.dart` | | | | U | |
| `lib/features/sessions/sessions_screen.dart` | | | | **U** | U |
| `lib/features/chat/chat_screen.dart` | | | | **U** | |
| `lib/app_lifecycle.dart` | | | | | **U** |
| `test/link_health_test.dart` | | **C** | | | |
| `test/link_liveness_test.dart` | **C** | U | **U** | | |
| `test/sessions_screen_test.dart` | | | | U | |
| `test/chat_render_test.dart` | | | | U | |
| `test/relay_inner_tls_test.dart` | U | | | | U |
| `docs/0062-MADR-…` | | | | | note D11 amendment |

C = create, U = update.

---

## 4. Testing strategy

### 4.1 Unit (no sockets, no wall clock)

The classifier takes a `Duration`, not a clock, so every band is a table test.
Client-side timing is driven by injecting the heartbeat period, never by
`await Future.delayed`.

### 4.2 Fake socket

Reuse the `_FakeDaemon` harness from `relay_inner_tls_test.dart` (a real TLS
`HttpServer` + `WebSocketTransformer`), extended with:

- a daemon that stops answering `ping` but keeps the socket open → drives the
  app-ping path
- a daemon that stops ponging at the protocol level → drives `pingInterval`
- a daemon that streams events for 90 s with no request → the **B1** test

### 4.3 The one that cannot be faked

Every fake closes its socket politely; the real failure does not. A blackhole
needs a route that swallows packets, which is why V7/V8 stay hardware checks.
`scripts/` gains no new tooling here — this is a manual step.

### 4.4 Manual (hardware)

Run these from
[ops-hardware-validation.md](ops-hardware-validation.md), which carries the same
rows with macOS **and** Linux commands and the service-manager pitfalls that
silently invalidate them.


| # | Scenario | Pass |
|---|----------|------|
| 1 | Tailscale off mid-session, mesh-carried → amber ≤ ~10 s | |
| 2 | …and red or relay-failover ≤ 30 s | |
| 3 | Relay killed mid-session, relay-carried → same bounds | |
| 4 | 90 s+ agent reply with no user input → session survives (B1) | |
| 5 | Backgrounded 10 min, link healthy → still connected on resume | |
| 6 | Backgrounded, link killed → detected on resume, not "connected" | |
| 7 | Airplane-mode flap → no transport thrash (0062 D11 + D6 exemption) | |
| 8 | Idle overnight → no spurious amber; battery delta acceptable | |
| 9 | Status names the right transport after a Settings switch | |
| 10 | Mesh flap ×5 → switches bounded, session usable (MADR V10) | |

---

## 5. Acceptance gates

| Gate | Criterion |
|------|-----------|
| G1 | Green requires `connected` **and** `fresh`; never rendered during a dial |
| G2 | A missed protocol pong closes the socket and reaches `error` |
| G3 | One missed app ping ⇒ amber, no teardown (0046 L-1 preserved) |
| G4 | The app ping is unconditional — asserted by the 90 s stream test (B1) |
| G5 | Health is orthogonal: no existing `== connected` call site changes meaning (B2) |
| G6 | Both screens show amber; both name the transport |
| G7 | Transport-loss reconnect prefers the alternate and does not consume the D11 budget |
| G8 | Manual rows 1–10 signed off on hardware |
| G9 | `dart format` + `flutter analyze` clean |

---

## 6. Remaining implementation choices

Small enough to settle while coding; recommended answers stand unless a
reviewer objects.

| Choice | Recommendation |
|--------|----------------|
| `lastVerifiedAt` stamped before or after parse | **Before** — a malformed frame still proves the peer is alive, and stamping after would make a protocol bug look like a dead link |
| Amber on a *relay*-carried session losing mesh | No signal, no amber: the mesh is not carrying anything. Only the active transport's health matters |
| Show health in Settings → Route | Yes, cheap: it already renders probe chips; add "verified 3 s ago" |

---

## 7. Review decisions (closed 2026-08-01)

| # | Question | Decision |
|---|----------|----------|
| 1 | B1 — how to resolve the daemon's data-only read deadline | **Client-only**: the app ping stays unconditional and documented as a protocol obligation. The daemon-side fix (control-frame-aware deadline) is deferred — it is out of scope, and version skew means the client obligation must exist anyway |
| 2 | B4 — `pingInterval` cadence | **20 s protocol / 10 s app**, amending MADR D2's 10 s. 30 s rejected: it could exceed `kDeadAfter` under throttled timers |
| 3 | Heartbeat while backgrounded | **10 s always.** The foreground service exists to hold the link off-screen; slowing it re-opens the detection gap exactly when the app cannot see, and the 60 s deadline caps the possible saving anyway. Battery is measured by manual row 8, not assumed |
| 4 | Sequencing vs 0062 G7 | **P0 ships and is validated alone**, on the same hardware round as the G7 checklist. P1–P4 are built against measured latency |

Accepted as written, without separate votes: **B2** (health orthogonal to
`McConnectionState`, protecting 47 call sites), **B3** (client owns the
freshness clock; expiry emits no stream event), **B5** (VPN loss is an
accelerator, not a precondition — Android reports `vpn`, Apple platforms
report `other`).

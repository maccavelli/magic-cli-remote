# MADR 0068: Protocol v2 — reconnect-resilient transport

<!-- markdownlint-disable MD013 MD024 -->

- **Status**: Implemented (P0–P6 complete, 2026-08-05). Protocol v2 is
  the shipped contract behind negotiation; v1 (`docs/protocol-v1.md`)
  remains byte-identical for v1 clients (U1 golden). The finalized wire
  delta is [protocol-v2.md](protocol-v2.md); per-phase records live in
  the plan's Status. Remaining outside this MADR: hardware gate G1
  (ops-hardware-validation.md Part F row F6, needs an iPhone) and the
  U7 `tls_resumed` live check (needs a host codesigning identity).
- **Date**: 2026-08-04
- **Deciders**: Project Owner
- **Implementation plan**:
  [0068-PLAN-protocol-v2-reconnect-resilient-transport.md](0068-PLAN-protocol-v2-reconnect-resilient-transport.md)
  (P0–P6; carries refinement R1 — resume piggybacked on `auth` — for
  review against D4's separate-message shape)
- **Scope**: The phone↔daemon wire contract (`docs/protocol-v1.md` →
  v2), `internal/ws/`, `internal/relay/`, `internal/relayhost/`,
  `internal/session/` (gap signalling), and the Dart transport engine
  (`apps/mobile/lib/data/ws/*`). Carries the T1–T11 work list from
  [0067 Amendment A1](0067-MADR-ios-port.md) to a decision.
- **Related**: [0067-MADR-ios-port.md](0067-MADR-ios-port.md) A1 (the
  audit this answers; F10–F14 are assumed here, not restated),
  [0063-MADR-connection-liveness-truth.md](0063-MADR-connection-liveness-truth.md)
  (verified liveness — v2 formalizes its cadence into the contract),
  [0062-MADR-phone-transport-selection.md](0062-MADR-phone-transport-selection.md)
  (dial episodes/budgets — v2 adjusts their lifecycle interaction),
  [MADR-client-identity-decision.md](MADR-client-identity-decision.md)
  (auth model unchanged), [protocol-v1.md](protocol-v1.md), and
  [MADR 0074 §15](0074-MADR-remote-provider-auth-from-phone.md) with its
  [approved P17–P22 plan](0074-PLAN-remote-provider-auth-from-phone.md)
  (D27/P20 reuse this record's negotiated resume window for owned provider
  device-auth flows without changing protocol-v2 session resumption).
- **Non-goals**: Background push / APNs / `NEAppPushProvider` (0067 D3's
  follow-up — orthogonal: v2 makes *reconnects* cheap and lossless, not
  *absence* survivable); changing auth or the pin/identity model; moving
  off `dart:io`/BoringSSL or off `coder/websocket`; multi-daemon routing.

---

## Problem

0067 A1 established that wire parity between Android and iOS already
holds, but the *contract* was designed for Android's connection pattern —
one long-lived socket held by a foreground service. iOS (0067 D2) makes
disconnect/reconnect the steady state: every pocket is a disconnect,
every glance a cold dial. Under that pattern the shipped v1 contract has
four structural weaknesses (A1 F10–F14): an informal, client-driven-only
liveness contract; flat connection pools that a device's own half-open
zombies can exhaust; park→resume races in the phone engine concentrated
in the relay path; and resync that is neither gap-aware (no truncation
signal, seq can regress across daemon restarts) nor gap-scaled (a 3 s
app switch costs a full 32-page reconcile).

v1's version handling is a strict equality gate — `env.V != 1` is
rejected `bad_version` per envelope (`internal/ws/server.go:574-575`),
and `/v1/hello` reports a single `protocol` number (`:427`). There is no
capability negotiation, so none of the fixes above can ship incrementally
without a version story. That is what makes this protocol v2, and why v2
is primarily a *lifecycle* contract, not a new message format.

## Grounding facts (verified against the tree, 2026-08-04)

### F1 — Toolchain: Go 1.26.5, coder/websocket v1.8.15

`go.mod:3` (`go 1.26.5`), `go.mod:8` (`coder/websocket v1.8.15`).
Server-initiated WS pings are already proven in-repo — the relay pings
its host control connections (`internal/relay/server.go:462`). The
daemon's WS listener is a bare `net.Listen` with no TCP keepalive
configuration (`internal/daemon/daemon.go:334`).

### F2 — Go gives us the primitives v2 needs, today

- `net.ListenConfig.KeepAliveConfig` / `TCPConn.SetKeepAliveConfig`
  (since Go 1.23; current in 1.26): per-listener TCP keepalive with
  explicit `Idle`/`Interval`/`Count` — kernel-level detection of
  suspended-without-FIN peers *below* the app deadline
  (<https://pkg.go.dev/net#KeepAliveConfig>).
- Go 1.26 enables **hybrid post-quantum key exchange by default**
  (`SecP256r1MLKEM768`) (<https://go.dev/doc/go1.26>) — handshakes got
  larger, which raises the per-reconnect cost v2 is trying to amortize;
  TLS session resumption (tickets already on, 0067 A1) becomes more
  valuable, not less.
- Go 1.26's experimental `goroutineleak` pprof profile
  (<https://go.dev/doc/go1.26>) directly addresses A1's relay findings
  (undeadlined first-read goroutines, unswept slots) — a cheap
  verification instrument for T9.
- Go 1.26 has **no** changes to `net`/`net/http` connection lifecycle
  semantics that affect this design (release notes checked — the
  `Dialer.DialTCP`-with-context additions and `NotifyContext` cause are
  conveniences, not contract changes).

### F3 — The phone pays a full TLS handshake on every single reconnect

`newSecurityContext()` builds a **fresh** `SecurityContext` per
connection — both the pinned `HttpClient` and the relay inner hop call
it per dial (`apps/mobile/lib/data/ws/mcremote_client.dart:146-168`,
`:186`, `:197`). BoringSSL's TLS session cache is scoped to the context,
so a new context per dial forfeits resumption even though the Go server
issues tickets by default (0067 A1). Under iOS cadence this is a full
(now post-quantum-sized, F2) handshake per glance at the phone.

### F4 — v1 has no negotiation surface

Strict per-envelope version equality (`internal/ws/server.go:574-575`);
`hello` carries `protocol: 1` (`:427`); `auth_ok` carries only
`device_id`/`device_name`/`home_dir` (`:816-820`). No capability flags,
no advertised limits, no place to put "I support resume" — every v2
behaviour needs a negotiation anchor first.

### F5 — Per-session ordering machinery already half-exists

Sessions already have monotonic `seq`, an 800-event ring, paged
`session.history{since_seq}` and `session.pending_asks`
(`internal/session/manager.go:30-41`, `:803-851`), and the idempotency
ledger already de-duplicates retried mutations across sockets
(`internal/ws/idempotency.go:37-47`). v2's resume design is therefore an
*extension* (gap signalling + connection-level fast path), not a new
subsystem — most of an XEP-0198-style design is already paid for.

## External evidence — prior art survey (researched 2026-08-04)

Four mature protocols solved exactly this problem; their convergent
shapes calibrate v2:

- **XMPP XEP-0198 Stream Management**
  (<https://xmpp.org/extensions/xep-0198.html>): server-issued opaque
  SM-ID at enable-time; both sides count handled stanzas (`h`, explicit
  32-bit wraparound); resume = reconnect + `resume(previd, h)` →
  `resumed(h)`, both sides replay unacked items; a still-open previous
  stream gets a **`conflict` stream error and is closed** — the exact
  connection-replacement semantics A1 T4 needs; server advertises `max`
  resumption window and optional preferred reconnect `location`.
- **Socket.IO connection state recovery**
  (<https://socket.io/docs/v4/connection-state-recovery>): private
  session id + per-packet offset; server buffers missed packets for
  `maxDisconnectionDuration`; client learns via a `recovered` flag and
  **must handle recovery failure** as a first-class outcome — recovery
  is an optimization, full resync remains the fallback.
- **SignalR stateful reconnect**
  (<https://github.com/dotnet/aspnetcore/issues/46691>,
  <https://learn.microsoft.com/en-us/azure/azure-signalr/signalr-concept-client-disconnections>):
  explicit `Ack`/`Sequence` message types; a byte-bounded send buffer
  (default 100 KB) rather than a count-bounded one; Azure's managed
  service picked a ~30 s recovery window — evidence that short windows
  capture most of the value.
- **MQTT 5** (<https://www.emqx.com/en/blog/mqtt5-new-feature-clean-start-and-session-expiry-interval>):
  split "clean start" (this connection) from "session expiry interval"
  (state lifetime after disconnect) — the client *asks* for a state
  lifetime and the server grants/limits it. The lesson: make the resume
  window an explicit, negotiated number, not an implementation constant.

**Apple platform research** (TN3151 "Choosing the right networking API",
<https://developer.apple.com/documentation/technotes/tn3151-choosing-the-right-networking-api>;
`waitsForConnectivity`,
<https://developer.apple.com/documentation/foundation/urlsessionconfiguration/waitsforconnectivity>;
"Adapt to changing network conditions",
<https://developer.apple.com/videos/play/tech-talks/111378/>): Apple's
session-resilience machinery — connection migration, wait-for-
connectivity, Happy Eyeballs dual-stack fallback, Multipath TCP — lives
in URLSession/Network.framework and is **unavailable to raw BSD sockets**,
which is what `dart:io` uses. The design consequence is stated as a
driver below: every resilience property v2 wants must live in *our*
protocol, because the platform will not supply it underneath us. (This
also re-affirms 0067's rejection of a native-stack rewrite: adopting
Network.framework would mean platform-channel TLS outside Dart, which
0005 already ruled out for the client-identity path.)

## Decision drivers

- iOS cadence is the new baseline (0067 D2); Android must not regress —
  every v2 behaviour is negotiated, v1 clients keep working unchanged.
- Self-hosted, no-cloud: resilience must come from our own two binaries,
  not managed infrastructure (contrast Azure SignalR).
- `dart:io` stays (0005 constraint); therefore app-level protocol
  resilience, not platform networking features (external evidence).
- 0063's principle extends: the *contract* must be explicit enough that
  neither side simulates liveness or guesses windows.
- Milestone framing: v2 is the last planned wire-contract break before
  the D3 background-push work builds on it; get the negotiation surface
  right once.

## Decision outcome

### D1 — Version negotiation replaces version equality

`hello` advertises `protocols: [1, 2]` (and keeps `protocol: 1` for old
clients). `auth`/`pair.claim` gain an optional `protocols` field; the
server picks the highest mutual version and answers in `auth_ok` with
`protocol: 2` plus a **capability/limit block**: read deadline, expected
ping cadence, resume support + window, history ring size, max frame
size. The per-envelope `v` check accepts the negotiated version for that
connection. v1 clients (today's shipped Android APK) hit none of this
and see byte-identical behaviour. (Grounded: F4; MQTT's negotiated-
window lesson; A1 T5.)

### D2 — Formal liveness contract, enforced at two layers

The advertised contract (D1) replaces convention: client app-ping
cadence and server read deadline become negotiated numbers, and the
server MAY WS-ping (the capability block says whether transport pings
reset the deadline — v2 servers make them count, closing A1 F10's trap).
Beneath the app layer, the daemon's listener gains
`net.ListenConfig.KeepAliveConfig` (F2) tuned below the app deadline so
suspended-without-FIN sockets are reaped by the kernel path even when
the app-level deadline would have carried them. Relay legs get the same
treatment plus a first-envelope read deadline (A1 T9).

### D3 — Connection replacement: one live socket per device

XEP-0198's conflict semantics, adopted: a successful `auth` (or v2
`resume`) for a device closes that device's older authed sockets with a
distinct close code (`replaced`), and the same rule applies per
`host_id`+device on the relay join plane. Pools stop being exhaustible
by a device's own zombies (A1 F11/T4). The close code is distinguishable
from an error so a genuinely concurrent second device session (not a
goal, but possible today) fails loudly rather than flapping.

### D4 — Resume as a fast path; full resync remains the truth path

Following XEP-0198/Socket.IO shape, scaled to what F5 already provides:

- `auth_ok` (v2) issues an opaque **resume token** and advertises the
  resume window (server-configurable; default in the minutes range —
  Azure's 30 s and Socket.IO's short-window guidance argue small; exact
  default is Q1).
- On reconnect within the window the client sends
  `resume{token, sessions: {id: last_seq…}}`; the server answers
  `resumed{sessions: {id: {first_seq, latest_seq}}}` and the client
  fetches only real gaps via the existing `session.history` — or is
  told, per session, that the ring truncated (D5) and a full refetch is
  needed. `pending_asks` reconciliation stays as-is.
- Resume failure (`resume_failed`) is a first-class outcome that routes
  into today's full `SessionSynchronizer` reconcile — Socket.IO's
  `recovered=false` lesson: the fallback is the spec, not an error.
- No server-side per-connection replay buffer in v2.0: per-session rings
  + gap signalling already bound loss detection, and A1 F14's cost
  problem is solved by gap-*scaling*, not buffering. A SignalR-style
  byte-bounded buffer is noted as a v2.x extension if hardware shows
  chatty reconnects still over-fetch (Q2).

### D5 — Gap signalling and seq epochs

`session.history` responses gain `first_seq` (oldest retained) and
`latest_seq`; `session.list` entries carry the same pair plus a daemon
**boot epoch** (monotonic instance id persisted alongside
`history.json`). A client whose `since_seq` predates `first_seq` knows
the ring truncated; a client whose epoch differs knows `seq` may have
regressed after an unclean daemon exit (A1 F14) and treats its cache as
stale. Closes A1 T6 and the silent-filtering hazard.

### D6 — Phone engine hardening (the A1 P1 items, unchanged in scope)

T1 (serialized, epoch-guarded teardown; single-outstanding-join
invariant; `RelayTransport` half-alive states fixed; bounded closes),
T2 (Apple-correct urgent-probe gating), T3 (backoff/park state preserved
across lifecycle resumes; per-resume generation bumps revisited against
0062 D11) — plus one addition from F3: **hold one `SecurityContext` per
(authority, TlsMode, client-identity) tuple** instead of per dial, so
BoringSSL session resumption engages and the post-quantum handshake is
paid once per network epoch, not per glance. (Verify resumption actually
engages on-device — Q3.)

### D7 — Operational hygiene

Rate-limit responses (daemon and relay) carry `retry_after`; relay slot
accounting gains a reconciliation sweep; `goroutineleak` profiling (F2)
is wired into the daemon/relay debug endpoints and consulted in v2's
verification. (A1 T9, F11.)

### Rejected

- **Adopting URLSession/Network.framework via platform channels.**
  Re-affirmed rejection: 0005 requires the client key in Dart's
  `SecurityContext`; a native TLS stack splits the identity model in
  two. v2's premise is app-level resilience over raw sockets.
- **Server-side full replay buffers per connection (SignalR shape) in
  v2.0.** The per-session ring + gap signalling already bounds what a
  client can miss silently; buffering adds daemon memory per phone for
  a cost D4's gap-scaling mostly removes. Revisit with hardware data.
- **Unbounded/long resume windows (hours).** MQTT allows it; our state
  is already durable server-side (sessions persist independently of
  connections), so a long window buys nothing the truth path doesn't
  already provide — it only extends zombie lifetime. Short window +
  cheap full resync wins.
- **WS-level pings as the only liveness signal.** Transport pings prove
  the socket, not the daemon's event loop; the app-level ping stays the
  primary (0063), transport pings become an *additional* deadline-reset
  in v2 (D2) rather than the contract.
- **Version negotiation via URL path (`/v2/ws`).** The envelope already
  carries `v` and `hello` already reports capability; a second endpoint
  doubles the relay/tunnel surface for no expressiveness gain.

## Consequences

### Positive

- iOS's connection pattern becomes a supported profile of the protocol,
  not an abuse of it; Android silently benefits (faster reconnects
  after process death, honest gap detection after long doze).
- The negotiation surface (D1) is the durable asset: D3's follow-up
  (background push) and any future capability land without another
  breaking version.
- TLS resumption + resume fast path turn the per-glance cost from
  full-handshake + full-reconcile into resumed-handshake + gap check.

### Negative / trade-offs

- Two protocol versions live in the daemon until the shipped Android
  fleet (of one) upgrades — dual-path testing burden for that window.
- The capability block is new contract surface to keep honest; wrong
  advertised numbers are worse than no numbers (0063's lesson).
- Connection replacement (D3) is a behaviour change a misbehaving second
  client could observe; close-code discipline must be exact.

### Neutral

- `protocol-v1.md` gains a sibling `protocol-v2.md` (delta-spec over v1
  rather than a rewrite); v1 doc gains the missing connection-lifecycle
  section retroactively describing shipped behaviour (A1 T5's doc half).

## Verification (proposed; PLAN to bind)

| # | Check | How |
| --- | --- | --- |
| U1 | v1 client against v2 daemon: byte-identical behaviour, no capability block, strict-equality path intact | go unit |
| U2 | Negotiation matrix: v2↔v2 picks 2; v2 client↔v1 daemon falls back to 1; advertised limits match runtime enforcement | go unit + dart unit |
| U3 | Replacement (D3): second auth closes first socket with `replaced`; relay join-plane analogue; zombie pool cannot refuse a live dial | go unit |
| U4 | Resume (D4): within-window resume yields only gap fetches; post-window or unknown token routes to full resync with `resume_failed`; truncated ring reported per D5 and triggers refetch | go unit + dart unit |
| U5 | Seq epoch: unclean daemon restart with stale client cache detects epoch change, no silent event filtering | go unit (kill -9 harness) |
| U6 | Kernel keepalive (D2) reaps a blackholed peer below the app deadline; deadline numbers advertised == enforced | go unit (live-tagged) |
| U7 | SecurityContext reuse: one context per tuple; resumption observed (session reused) against the Go server | dart unit (live-tagged) + hardware confirm |
| U8 | A1's U8–U11 (engine hardening tests) as specified there | dart unit |
| G1 | 0067 Part F F6 (rapid-cycle hardware row) passes on a v2 pair | hardware |

## Open questions

- Q1: Resume window default — 60 s (covers app switches and brief
  suspensions, matches the daemon's current reap horizon) or minutes
  (covers elevator rides at the cost of longer token validity)? Azure
  picked ~30 s; MQTT lets clients ask. Proposal: server default 120 s,
  client may request lower, never higher.
- Q2: Does hardware data (Part F F6 + real resync byte counts) justify a
  SignalR-style bounded replay buffer in v2.x, or does gap-scaled fetch
  suffice?
- Q3: Does dart:io on iOS actually reuse TLS sessions when the
  `SecurityContext` is held (D6)? BoringSSL supports it; dart:io's
  surface doesn't document it. Needs the live-tagged U7 probe.
- Q4: Should `resumed` also carry the server's current `pending_asks`
  inline (saving one round trip on every resume), or is the existing
  separate call cleaner? Measure on hardware.
- ~~Q5: Relay join-plane replacement (D3) — is `host_id`+device identity
  available at join time, or does replacement need the tunnel's inner
  auth to complete first?~~ **Answered 2026-08-04 (P2.4 spike,
  `internal/relay/join_replacement_spike_test.go`)**: no device identity
  exists at the relay — `JoinPayload` carries only `host_id`
  (`internal/relay/protocol.go:41-43`) and the phone's client certificate
  terminates at the daemon *inside* the tunnel. Relay-side replacement
  would require leaking device identity into the join plane, contrary to
  the zero-knowledge posture — **rejected**. A second join coexists with
  a lingering first (0067 A1 Q6: tolerated, no conflict); the zombie
  window stays bounded by the daemon's read deadline tearing the splice
  down end-to-end, plus P6's slot sweep.

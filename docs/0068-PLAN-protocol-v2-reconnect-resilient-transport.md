# MADR 0068 — Implementation plan: protocol v2, reconnect-resilient transport

<!-- markdownlint-disable MD013 MD024 -->

**Provider-auth follow-up:** [MADR 0074 §15](0074-MADR-remote-provider-auth-from-phone.md)
uses this plan's negotiated resume window as the ownership bound for transiently
disconnected device-auth flows; the executable follow-up is
[0074-PLAN P17–P22](0074-PLAN-remote-provider-auth-from-phone.md). It does not
reopen or modify 0068's completed session-resumption phases.

- **Status**: **Implemented (P0–P6 complete, 2026-08-05).** All software
  phases and P6 docs close-out are finished. Remaining *outside* this
  plan (not incomplete plan steps): hardware gate G1
  (`ops-hardware-validation.md` Part F row F6, needs an iPhone) and the
  U7 `tls_resumed` live check (needs a host codesigning identity). Per-phase
  record below.
  **P0 implemented 2026-08-04** (U1 golden +
  U2 matrix green on both stacks; `protocol-v2.md` published, v1 lifecycle
  section added). **P1 implemented 2026-08-04** — with one design
  correction discovered by U6: coder/websocket closes the connection when
  a read context expires, so the v2 reap is owned by a per-connection
  deadline **watchdog** (`deadlineWatchdog` + `horizon` over atomic
  lastData/lastPong marks) rather than by read-context deadlines; v1
  connections keep the exact per-read timeout path. U6's in-CI half is
  green (pong-extension survival, v1 reap timing, relay silent-upgrade
  reap, keepalive config mapping); the blackhole-reap half is
  hardware-gated as planned. **P2 implemented 2026-08-04** (U3 green both
  stacks: elder closed with 4001 and its slot freed *synchronously* under
  the capacity check's lock — an async free let the device's next dial
  race into `too many clients`, caught by the churn test; dart parks on
  4001 and keeps pairing; dev-mode guard). P2.4 spike answered MADR Q5:
  no relay-side replacement — joins carry no device identity by design.
  **P3 implemented 2026-08-04** (U5 green: epoch kept across clean
  restart / minted after kill-shape restart / empty without store;
  SeqBounds from live ring and durable history; dart matrix — fast-skip,
  behind-ring fetch, epoch-forced walk, bounded re-arm of interrupted
  resync). **P4 implemented 2026-08-04** (U4 green both stacks: token
  issue/rotate/clamp/expiry with ownership-filtered `resumed` bounds;
  dart round trip via a fake v2 daemon — the second auth carries the
  token + seq claims; the synchronizer skips the entire reconcile on
  confirmed-unchanged and falls through on any miss; `resumeSeqSource`
  wired from transcripts in app_lifecycle; resume state cleared on
  sign-out while the token survives disconnects by design; config
  `limits.ws_resume_window_seconds`, default 120 s).
  **P5 implemented 2026-08-04** (phone engine hardening: T1 —
  `_teardownSocket` serialises through a chained `_closingFuture` that
  `_runDialEpisode` awaits bounded 3 s, and RelayTransport got the A1
  finding-8/9/10 fixes: awaitable-idempotent `close()`, outer-hop
  `onDone` tears the whole bridge down, `_replacePeer` rejects after
  close, outer sink close bounded 2 s, and a `cancelled` callback aborts
  a superseded dial with `dial_superseded` before join and before bind;
  T2 — urgent-probe accelerator gated by pure `vpnSignalMeaningful`
  (Apple platforms never emit `vpn`, so absence there is not evidence);
  T3 — backoff/handshake counters reset only on `userInitiated`
  connects, and the lifecycle bumps the network generation only on a
  connectivity event since the last dial or a >60 s background gap; F3 —
  CertPinner caches `SecurityContext` per `(mode, cert)` tuple so TLS
  session resumption survives redials, verifiable via `caps.tls_resumed`;
  A1 finding 13 — background park runs on captured client/coordinator
  refs with no `mounted` guard; T10 — `relayLegTimeouts()` derives
  join/loopback/inner-ready budgets from the remaining episode budget
  (≤32 s inside the 35 s episode); T8 — literal-IPv4 hosts get the
  NAT64 hint in the iOS failure copy. Also flipped the P0 negotiation
  golden that P4 had left stale — `caps.resume` is now asserted present
  with the 120 s default window. Deferred: client-level park→resume
  single-join integration test (needs a fake-relay + TLS daemon
  harness); U7 `tls_resumed` live check still pending a host
  codesigning identity. Suites green: Go full, mobile 738 + format.)
  **P6 implemented 2026-08-05 — plan complete.** (Operational hygiene:
  `retry_after_ms` on refusals — the daemon's genuine-capacity close
  carries `too many clients; retry_after_ms=<n>` in the close reason
  (soonest deadline horizon across clients, floor 5 s), the relay's
  `rate_limited` error payload carries the fixed-window remainder plus a
  standard `Retry-After` header on the HTTP 429 upgrade path, and
  capacity `limit` joins get the 5 s courtesy floor; the Dart client
  parses both channels into a one-shot floor on `reconnectDelay` —
  never shortening the ladder, clamped 60 s. Relay slot sweep: 30 s
  reconciliation of `phones` counters against live splices + pending
  reservations, correcting only divergences that survive two
  consecutive sweeps so legal in-flight windows can never be "fixed"
  into a double release; `activeSplice` gained `hostID`. Leak
  instrumentation: `internal/debugserve` (build tag `debugpprof`,
  loopback-only `MC_DEBUG_ADDR` listener, no-op twin in release builds)
  wired into daemon and relay; `make debug` builds with
  `GOEXPERIMENT=goroutineleakprofile`; `/debug/pprof/goroutineleak`
  live-verified HTTP 200 on a debug mcrelay; documented in
  ops-mcrelay.md §6. Docs close-out: protocol-v2.md finalized, MADR
  0068 status → Implemented, 0067 A1 work list annotated with
  dispositions, ops-hardware-validation F6 re-pointed at the v2 pair
  with P1/P2/P4/P6 expectations. Tests: Go — window-remainder +
  Retry-After header, capacity close-reason within [5 s, deadline],
  sweep corrects-after-two/holds-on-moving/leaves-agreeing; dart —
  backoff floor semantics + close-reason parser, 8 new.) Remaining
  outside the plan: hardware gate G1 (Part F row F6) and the U7
  `tls_resumed` live check. Plan
  grounded 2026-08-04 against the tree at `cee9824` and the 0067 A1
  audits.
- **Date**: 2026-08-04
- **Scope**: `internal/protocol`, `internal/ws`, `internal/relay`,
  `internal/relayhost`, `internal/session`, `internal/config`,
  `apps/mobile/lib/data/ws/*`, `apps/mobile/lib/state/session_synchronizer.dart`,
  `apps/mobile/lib/app_lifecycle.dart`, `docs/protocol-v1.md` (+ new
  `docs/protocol-v2.md`). No auth-model, pin, or pairing changes.
- **Source**: [0068-MADR-protocol-v2-reconnect-resilient-transport.md](0068-MADR-protocol-v2-reconnect-resilient-transport.md)

---

## 0. Grounding — where every change lands

Seams that already exist (verified 2026-08-04; A1 audit anchors re-used
where unchanged):

| Seam | Location | Today | v2 change |
| --- | --- | --- | --- |
| Protocol version constant | `internal/protocol/messages.go:14` (`const Version = 1`) | single value | becomes `V1`/`V2` + `Supported` set |
| Envelope version gate | `internal/ws/server.go:574-575` (`env.V != protocol.Version` → `bad_version`) | strict equality | per-connection negotiated version |
| `hello` payload | `internal/ws/server.go:427` (`"protocol": protocol.Version`) | single number | adds `protocols: [1,2]` |
| `auth` handler / `auth_ok` | `internal/ws/server.go:798` (handler), `:816-820` (payload: device id/name/home dir only) | no capability surface | capability block, resume token, `resumed` block, `tls_resumed` |
| Read deadline | `internal/ws/server.go:164-166` (60 s default), `:528-537` (per-read ctx); no config key | app-ping-only reset | negotiated + advertised; WS-pong reset (P1); config key |
| App ping handling | `internal/ws/server.go:588-590` | resets deadline | unchanged (primary signal) |
| Server→client WS ping precedent | `internal/relay/server.go:462` (`conn.Ping`) | relay→host only | daemon pinger goroutine per v2 conn |
| Daemon listener | `internal/daemon/daemon.go:334` (bare `net.Listen`) | no TCP keepalive | `net.ListenConfig.KeepAliveConfig` |
| Capacity / eviction | `internal/ws/server.go:477-487`, `:1269-1279`, `internal/config/config.go:86,666` (`MaxWSClients: 8`) | authed zombies unevictable | per-device replacement (P2) |
| Client set by device | `internal/ws/server.go:951-960` (N sockets/device coexist), `:249-260` (fan-out to all) | no dedupe | replacement closes elders |
| Relay first-envelope reads | `internal/relay/server.go:367`, `:481`, `:601` (`readEnv` with request ctx, no deadline) | unbounded | 10 s deadline |
| Relay join slots | `internal/relay/hub.go:188-219` (`MaxPhonesPerHost=8`, `internal/relay/config.go:51`), release `server.go:586` | held to splice end | reconciliation sweep + (Q5-gated) replacement |
| Relay rate limits | `internal/relay/config.go:54-57`, `internal/relay/server.go:286-322`, `:353-356` | no Retry-After | `retry_after` field |
| Session ring / paging | `internal/session/manager.go:30-41` (`historyBufferCap=800`), `:803-851` (`HistoryPage`), `:816-821` (silent truncation) | no gap signal | `first_seq`/`latest_seq` |
| Seq restore | `internal/session/manager.go:400-440` (seed from `history.json`), `:205-208` (5 s debounce) | seq can regress | boot epoch |
| Idempotency ledger | `internal/ws/idempotency.go:37-47` | cross-socket replay works | untouched (relied on) |
| Dart SecurityContext | `apps/mobile/lib/data/ws/mcremote_client.dart:146-168`; fresh per dial at `:186`, `:197` | resumption forfeited | cached per tuple (P5) |
| Dart dial episode | `mcremote_client.dart:958` (`_runDialEpisode`), `:971`+`:1018` (`userInitiated` already threaded), budget `:485` (35 s) | resets on resume | T3 fixes (P5) |
| Dart reconnect resets | `mcremote_client.dart:2072-2073` (`_handshakeFailures = 0` unconditional) | park unreachable on iOS | conditional on `userInitiated` |
| Dart teardown | `mcremote_client.dart:2151-2189` (sync detach, then awaits) | resume can race close | closing-future gate (P5) |
| RelayTransport lifecycle | `relay_transport.dart:154` (bind), `:175-178` (`onDone` partial), `:266-281` (`_replacePeer` no `_closed` check), `:367-384` (close; outer close unbounded at `:380`) | three half-alive states | P5 fixes |
| Relay leg timeouts | `relay_transport.dart:81` (15 s), `mcremote_client.dart:810` (8 s), `:839` (20 s) vs 35 s budget | worst case 43 s | derive from episode remainder |
| Urgent-probe heuristic | `app_lifecycle.dart:110-113` (`!contains(vpn)`), `mcremote_client.dart:1919-1932` (2 s budget) | inverted on Apple | platform-gated (P5) |
| Background park guard | `app_lifecycle.dart:143` (`!mounted` early-return precedes park) | socket suspends live | restructure (P5) |
| Resync driver | `lib/state/session_synchronizer.dart:32-91` (full reconcile per connected edge, 32 pages, concurrency 2), `:63-65`/`:100-102` (failures swallowed) | not gap-scaled | consumes P3 fields; resumable |

Notable non-facts (checked, not assumed):

- **No dependency changes needed.** `coder/websocket v1.8.15` already
  provides server `Ping` (used in-repo); `net.KeepAliveConfig` shipped in
  Go 1.23 and is current in the pinned Go 1.26.5 (`go.mod:3`); Dart needs
  no new packages.
- **v2 traverses today's relay unchanged.** The inner protocol rides the
  splice as opaque bytes (`internal/relay/server.go` splices; 1 MiB caps
  on both legs agree with the daemon's) — only the join plane and
  deadlines change relay-side; an un-upgraded relay still carries v2.
- **The idempotency ledger already makes v2 retries safe** — no changes
  to it are planned or needed (0068 F5).
- **Auth is already cheap per reconnect** (flock + file read,
  `internal/auth/store.go:403-448`) — v2 does not need to avoid re-auth,
  only the extra round trips and full TLS handshakes around it.
- **Go 1.26 changed nothing this plan relies on** beyond what 1.23+
  already had; the PQ-hybrid default (larger ClientHello) is why P5's
  SecurityContext cache matters but requires no action server-side.

Design refinement carried into the plan (flagged for MADR review, R1):
D4's `resume{…}` round trip is implemented as an **optional `resume`
object on `auth`** answered by an optional `resumed` object on
`auth_ok` — same semantics, one fewer RTT, no new top-level message
type. The MADR's separate-message shape is kept in `protocol-v2.md` as
a rejected alternative with this rationale.

---

## P0 — Spec + version negotiation (D1)

1. Write `docs/protocol-v2.md` as a **delta spec** over v1: negotiation
   rules, the `auth_ok` capability block schema (below), `resume`/
   `resumed`/`resume_failed`, `replaced` close code, `first_seq`/
   `latest_seq`/`epoch` fields, `retry_after`. Add the missing
   **Connection lifecycle** section to `docs/protocol-v1.md`
   retroactively documenting shipped behaviour (60 s deadline, app-ping
   reset, no server pings, no replacement — A1 T5's doc half; sourced
   from `internal/ws/server.go:164-166`, `:528-537`, `:588-590`).
2. `internal/protocol/messages.go`: `const V1 = 1; const V2 = 2`;
   `Supported = [...]`; keep `Version = V1` as an alias until all
   references migrate (grep: `protocol.Version` appears in server gate,
   hello, tests). `internal/protocol/errors.go`: no new error — the
   existing `bad_version` (`errors.go:24`) covers rejected versions.
3. `internal/ws/server.go`: per-client `negotiated int` (default `V1`)
   on the `client` struct; `hello` gains `protocols`; `auth` and
   `pair.claim` accept optional `protocols []int` and reply with
   `protocol: <picked>`; the envelope gate at `:574` compares against
   `c.negotiated` — pre-auth envelopes still require `V1` framing (the
   `auth` message itself stays v1-shaped so old daemons reject new
   clients cleanly with today's error).
4. Capability block in v2 `auth_ok` (single source of truth: a
   `LivenessSpec` struct owned by `internal/ws`, consumed by both the
   enforcement paths and the payload builder so advertised == enforced
   by construction):

   ```json
   "caps": {
     "protocol": 2,
     "read_deadline_ms": 60000,
     "ping_interval_ms": 10000,
     "ws_ping_resets_deadline": true,
     "resume": {"window_ms": 120000},
     "history_ring": 800,
     "max_frame_bytes": 1048576,
     "tls_resumed": false
   }
   ```

   `tls_resumed` is `tls.ConnectionState.DidResume` for this connection
   — free to compute, and it is what makes P5's resumption work
   verifiable from the client side (answers 0068 Q3 without new probe
   machinery).
5. Dart: `mcremote_client.dart` sends `protocols: [1, 2]` in auth,
   parses `caps` into a `ServerCaps` object (null on v1 daemons —
   every consumer falls back to today's constants), stores the
   negotiated version per connection, stamps envelopes accordingly.

**Tests:** U1 — go unit: v1-shaped auth (no `protocols`) against v2
daemon → byte-identical `auth_ok` (golden compare), strict `V1` gate
still enforced for that connection. U2 — go unit: negotiation matrix
(none→1, [1]→1, [1,2]→2, [2]→2, [3]→`bad_version`); dart unit: `caps`
parse + absent-caps fallback; advertised-equals-enforced pinned by
constructing `LivenessSpec` once in the test and asserting both paths.

## P1 — Liveness contract, two layers (D2)

1. Config: `internal/config/config.go` gains
   `ws.read_deadline_seconds` (0 → 60, floor 15) and
   `ws.tcp_keepalive` `{enable(bool, default true), idle_seconds(25),
   interval_seconds(5), count(4)}` following the `MaxWSClients`
   defaulting pattern (`config.go:666-678`). Chosen defaults reap a
   silent peer at ~25+4×5 = **45 s**, inside the 60 s app deadline, per
   `net.KeepAliveConfig` semantics
   (<https://pkg.go.dev/net#KeepAliveConfig>).
2. `internal/daemon/daemon.go:334`: replace bare `net.Listen` with
   `(&net.ListenConfig{KeepAliveConfig: …}).Listen(ctx, …)`. Same for
   the relay's listener (`internal/relay`), and the relay-host's dials
   (`internal/relayhost/client.go:217-277`) via `net.Dialer{KeepAliveConfig}`.
3. Daemon v2 pinger: per v2-negotiated connection, a goroutine sends
   `conn.Ping(ctx)` every `read_deadline/3`; a completed pong (Ping
   returns) resets the same deadline the app-ping path resets
   (`server.go:528-537` refactored so the deadline is a shared
   resettable timer rather than a per-Read ctx). v1 connections keep the
   exact current behaviour. The relay→host precedent at
   `internal/relay/server.go:462` is the implementation template.
4. Relay first-envelope deadline: wrap the three `readEnv` sites
   (`internal/relay/server.go:367`, `:481`, `:601`) in
   `context.WithTimeout(…, 10*time.Second)` — the value mirrors the
   HTTP `ReadHeaderTimeout` already chosen at `:97`.

**Tests:** U6 — go unit (live-tagged, mirrors the repo's live-test
convention): blackholed client (accept then silence, no FIN) is reaped
by TCP keepalive before the app deadline on Linux/macOS runners; go
unit: pong resets deadline for v2, does not for v1; relay unit:
upgrade-then-silence is closed at ~10 s (kills A1 Go finding 12).

## P2 — Connection replacement (D3)

1. Close code: `internal/protocol` defines `CloseReplaced = 4001`
   (WebSocket app range 4000–4999 per RFC 6455 §7.4.2) with reason
   `"replaced"`.
2. `internal/ws/server.go` `handleAuth` (`:798`): after successful auth
   of device D on connection C, enumerate other **authed** clients with
   the same `deviceID` (`:951-960` map) and close each with
   `CloseReplaced` (their slots free immediately — fixes A1 Go findings
   3/4; the queue-full `slow_client` path `:1759-1772` is untouched but
   becomes rare since elders die at replacement). v1 connections are
   replaced too — the close code is new information, the closing itself
   is legal server behaviour under v1.
3. Dart: `mcremote_client.dart` close-handling maps 4001 to a terminal
   non-retry state distinct from error (a *newer* connection of this
   device exists — an awakened zombie must not reconnect-fight it):
   reuse the `_manualDisconnect`-style park, log, no user-facing error.
4. Relay join plane: implementation gated on the Q5/Q6 spike — a
   live-tagged Go test (`internal/relay`) that opens a join for
   `host_id`, half-closes it, opens a second join, and records whether
   the tunnel establishes while the splice lingers. If identity is
   available at join time, apply replacement keyed
   `(host_id, device)`; if not (likely — join is pre-auth), rely on
   P1's deadlines + P6's sweep and record the residual window in the
   MADR (answers 0068 Q5).

**Tests:** U3 — go unit: second auth closes first with 4001, slot count
returns to 1, fan-out (`:249-260`) reaches only the survivor; eviction
test extended: pool of 8 zombies + replacement → ninth dial succeeds
(the A1 self-eviction scenario, inverted). Dart unit: 4001 → parked,
no reconnect timer armed.

## P3 — Gap signalling + seq epochs (D5)

1. `internal/session/manager.go`: `HistoryPage` (`:803-851`) returns
   `first_seq` (oldest retained seq, 0 when ring empty) and
   `latest_seq` alongside the page; `session.list` entries
   (`internal/ws/server.go:1092-1107`) gain the same pair.
2. Boot epoch: `internal/session` persists `epoch` (random 64-bit,
   regenerated on every daemon start where `history.json` was **not**
   cleanly flushed; kept when clean) next to the seq seed
   (`manager.go:400-440`); surfaced in v2 `auth_ok.caps` and in
   `session.list`. Clean-shutdown marker rides the existing debounced
   persist (`:205-208`, `:1582-1610`) — flush-on-SIGTERM already exists
   via the daemon's shutdown path; kill -9 leaves the marker dirty,
   which is exactly the signal.
3. Dart `SessionSynchronizer` (`session_synchronizer.dart:32-91`)
   consumes them: `lastSeq == latest_seq` → skip that session's history
   walk entirely (the 3-second-app-switch case drops to the two list
   calls); `since_seq < first_seq` → full refetch of that session
   (truncation now explicit); epoch mismatch → treat all cached seqs as
   stale, full resync. Interrupted resync (`:63-65`, `:100-102`) is
   re-armed: on failure or generation abandonment, set a dirty flag the
   next `→connected` edge *or* foreground resume re-drives (A1 finding
   29).
4. v1 clients: the new fields are additive JSON — ignored by the
   shipped Android client. Confirmed non-breaking by U1's golden test.

**Tests:** U5 — go unit with a kill-harness (write events, SIGKILL the
manager's owner, reload): epoch changes, reload seq regression is
detectable; unit: `first_seq` correct across ring wrap (800-cap,
`manager.go:30`). Dart unit: synchronizer matrix — up-to-date /
gapped / truncated / epoch-changed / interrupted-then-resumed.

## P4 — Resume fast path (D4, shape per R1)

1. Server: `internal/ws` keeps an in-memory `resumeTokens` map
   `deviceID → {token, expires}` (single token per device, rotated on
   every v2 `auth_ok`, window from `ws.resume_window_seconds` config,
   default 120 — MADR Q1's proposal; client may request lower via
   `auth.resume_window_ms`, never higher). Tokens are opaque 128-bit
   random; no persistence (a daemon restart invalidates them — the
   epoch path P3 covers that case anyway).
2. v2 `auth` accepts `resume: {token, sessions: {"<id>": last_seq}}`.
   On valid token within window: `auth_ok` carries
   `resumed: {sessions: {"<id>": {first_seq, latest_seq}}}` computed
   from the live rings — the client then fetches only real gaps. On
   invalid/expired: `auth_ok` carries `resume_failed: true` (auth
   itself still succeeds — resume failure is not an auth failure).
   Auth remains mandatory and unchanged (0005 SPKI check at
   `internal/ws/server.go:467-469` untouched) — resume only removes
   round trips *after* auth, per R1.
3. Dart: client holds `{token, window, perSessionLastSeq}` in memory
   only (cold start does full resync by design); includes `resume` in
   auth when within window; on `resumed` hands the per-session pairs to
   `SessionSynchronizer`, which now skips both list calls when every
   known session is covered and unchanged; on `resume_failed` or absent
   → today's full reconcile. `pending_asks` still runs on every
   reconnect (asks are the safety-critical surface; one call, cheap —
   MADR Q4 deferred to hardware measurement as written).
4. Wire `caps.resume.window_ms` (P0) from the same config value.

**Tests:** U4 — go unit: resume within window returns correct
per-session pairs; expired/unknown token → `resume_failed`; token
rotates per auth; window clamp (client asks 30 s → granted, asks 10 min
→ clamped to 120 s). Dart unit: resume payload built only within
window; `resumed` path performs zero history calls when seqs match;
`resume_failed` falls through to full resync (asserted via recorded
client calls).

## P5 — Phone engine hardening (D6 = A1 T1/T2/T3 + F3, T8, T10)

1. **T1 — teardown/rebuild serialization.**
   `mcremote_client.dart:2151-2189`: `_teardownSocket` records its
   completion future in `_closing`; `_runDialEpisode` (`:958`) awaits
   `_closing` (bounded 3 s) before any dial. `RelayTransport`:
   `_replacePeer` gains the `_closed` guard (close the incoming socket,
   `relay_transport.dart:266-281`); outer `onDone` runs the full close
   path (`:175-178` → close `_server`, `_outerHttp`, set `_closed`);
   outer `sink.close()` bounded 2 s (`:380`, matching the inner bound
   at `mcremote_client.dart:2178`); `close()` returns the same future
   to every caller (truly awaitable-idempotent). `RelayTransport.open`
   takes the episode's epoch and re-checks it at bind and at accept —
   a superseded open self-closes. Net invariant: **at most one
   outstanding join per client**, asserted in tests.
2. **T2 — Apple-correct urgent probe.** `app_lifecycle.dart:110-113`:
   the mesh-death accelerator requires a *positive* signal — platform
   gate so that on `TargetPlatform.iOS`/`macOS` (where
   `connectivity_plus` documents `other` instead of `vpn`, per the
   comment at `:105-108` and plugin docs) the urgent flag is never
   derived from `vpn`-absence; those platforms always use the lenient
   probe. Android behaviour unchanged.
3. **T3 — resume-aware backoff.** `mcremote_client.dart:2072-2073`:
   reset `_handshakeFailures`/`_reconnectAttempt` only when
   `userInitiated` (the flag already reaches `reconnect`, `:1350`,
   `:1376`); `app_lifecycle.dart` passes `userInitiated: false` through
   `reconnectFromStore`. A parked client (`reconnectParked`,
   `:517-522`) stays parked across resumes until a user action — the
   Settings "Reconnect now" and connect-screen paths already pass
   `true`. Generation bumps (`app_lifecycle.dart:214`): bump only when
   the connectivity stream reported a change since the last dial or
   the background gap exceeded 60 s — preserves 0062 D11's budget
   against per-glance refresh while keeping genuine-network-change
   semantics (documented as an 0062 amendment note in the commit).
4. **F3 — SecurityContext cache.** `CertPinner` caches one
   `SecurityContext` keyed `(authority, TlsMode, identity fingerprint,
   pin)`; invalidated on pin change, identity regeneration, and
   sign-out (`clearMemoryCredentials:544` hook). Verification is P0's
   `caps.tls_resumed`: the dart live-tagged test connects twice and
   asserts the second handshake resumed (closes 0068 Q3 on hardware
   later; CI asserts against the Go server on the host platform).
5. **Park correctness.** `app_lifecycle.dart:143`: capture the client
   and coordinator references before the `mounted` check so the park
   itself (`disconnect(manual: false)`) runs even when the widget tree
   is tearing down; only the `setState`-adjacent work stays
   mounted-guarded.
6. **T10 — relay leg fits the episode budget.** Thread the episode's
   remaining budget into `_openSocketViaRelay` (`:764`): outer
   join timeout = `min(15 s, remaining − 12 s)`, loopback = 5 s, inner
   ready = `min(15 s, remaining − 2 s)` — serial worst case ≤ 32 s
   < 35 s (`kDialEpisodeBudget`, `:485`), so a stalled relay leg can no
   longer silently forfeit the mesh fallback (A1 Dart finding 7).
7. **T8 — failure-shape copy.** Extend `_isNetworkShapedError`
   handling (0067 P4 work in `connect_screen.dart`) with the NAT64
   note: when the host is a literal IPv4 and the failure is
   network-shaped on iOS, the failure copy mentions that relay may
   still work — and the episode's automatic mesh→relay fallback is
   asserted by a unit test for exactly this shape (relay reachable,
   mesh `connect_failed`), which is the App-Store-review NAT64
   mitigation (0068 external evidence).

**Tests:** U8 group (dart): single-outstanding-join across
park→resume (fake relay counts joins); `close()` concurrent-caller
idempotency; accept-after-close leaves no socket (asserted via fake
server socket bookkeeping); outer-`onDone` full teardown; urgent-probe
platform matrix; parked-stays-parked across resume vs user reconnect;
budget-derived relay timeouts (fake clock); NAT64-shape fallback. U7
(dart, live-tagged): `tls_resumed` true on second dial with cache, and
false when the cache is deliberately dropped.

## P6 — Operational hygiene + close-out (D7)

1. `retry_after`: daemon `too many clients` refusal
   (`internal/ws/server.go:1269-1279` path) and relay `rate_limited`/
   `limit` responses (`internal/relay/server.go:353-356`, `:512-519`)
   gain `retry_after_ms` (daemon: time to next deadline expiry
   estimate, floor 5 s; relay: current window remainder from the
   fixed-window limiter `:293-322`). Dart backoff consumes it as a
   floor for the next attempt.
2. Relay slot sweep: periodic (30 s) reconciliation in
   `internal/relay/hub.go` comparing `phones[hostID]` counters against
   live splices; divergence logs + self-corrects (A1 Go unknown 8).
3. Leak instrumentation: `make debug`-tier build flag
   (`GOEXPERIMENT=goroutineleakprofile`, Go 1.26 —
   <https://go.dev/doc/go1.26>) and the `/debug/pprof/goroutineleak`
   endpoint on the existing debug listeners of daemon and relay;
   documented in the ops runbook. Not in release builds.
4. Docs close-out: `protocol-v2.md` finalized against implementation;
   0068 MADR Status flip; 0067 A1 work-list table annotated with
   0068-phase dispositions; `ops-hardware-validation.md` Part F row F6
   re-pointed at the v2 pair (G1).

**Tests:** go unit: `retry_after_ms` present and sane in both refusals;
sweep corrects an artificially diverged counter; dart unit: backoff
honours `retry_after_ms` floor.

---

## File checklist

| File | Phases |
| --- | --- |
| `docs/protocol-v2.md` (new) | P0, P6 |
| `docs/protocol-v1.md` (lifecycle section) | P0 |
| `internal/protocol/messages.go` | P0, P2 |
| `internal/ws/server.go` | P0, P1, P2, P3, P4, P6 |
| `internal/ws/liveness.go` (new — `LivenessSpec`, pinger) | P0, P1 |
| `internal/ws/resume.go` (new — token map) | P4 |
| `internal/config/config.go` | P1, P4 |
| `internal/daemon/daemon.go` | P1 |
| `internal/relay/server.go` | P1, P6 |
| `internal/relay/hub.go` | P2 (spike), P6 |
| `internal/relayhost/client.go` | P1 |
| `internal/session/manager.go` | P3 |
| `apps/mobile/lib/data/ws/mcremote_client.dart` | P0, P2, P4, P5 |
| `apps/mobile/lib/data/ws/relay_transport.dart` | P5 |
| `apps/mobile/lib/data/ws/transport_probes.dart` | P5 (only if T2 touches probe plumbing) |
| `apps/mobile/lib/app_lifecycle.dart` | P5 |
| `apps/mobile/lib/state/session_synchronizer.dart` | P3, P4 |
| `apps/mobile/lib/features/connect/connect_screen.dart` | P5.7 |
| `docs/ops-hardware-validation.md` | P6 |

## Verification map (MADR → plan)

| MADR ID | Plan phase | Kind |
| --- | --- | --- |
| U1, U2 | P0 | go unit + dart unit |
| U6 | P1 | go unit (live-tagged) |
| U3 | P2 | go unit + dart unit |
| U5 | P3 | go unit + dart unit |
| U4 | P4 | go unit + dart unit |
| U7, U8 | P5 | dart unit (U7 live-tagged) |
| — (D7) | P6 | go unit + dart unit |
| G1 | after P6 | hardware (Part F F6, v2 pair) |

## Edge cases held by design (for review)

- **v2 phone → v1 daemon (downgrade)**: `protocols` in auth is unknown
  JSON to a v1 daemon — v1 ignores unknown fields (`handleAuth` decodes
  known keys), replies plain `auth_ok`; the client sees no `caps` and
  runs v1 semantics. No flag day.
- **v1 phone → v2 daemon**: U1's golden test pins byte-identical
  behaviour; replacement (P2) still applies to it — being closed by a
  newer login was always legal, only the code is new.
- **Resume across transport switch** (mesh dial → relay resume): token
  is device-scoped, not connection- or transport-scoped — valid by
  construction; the inner protocol is transport-agnostic.
- **Resume token vs daemon restart**: tokens are memory-only; restart →
  `resume_failed` + epoch change (P3) → full resync. Two independent
  safety nets, both tested.
- **Replacement storm** (two devices sharing credentials — not a
  supported topology): each auth kicks the other, visibly, with a
  distinct close code; previously they silently double-received
  broadcasts (`:249-260`). Loud beats silent; noted in protocol-v2.md.
- **Zombie wakes after replacement**: 4001 parks it client-side (P2.3);
  it cannot fight the live connection.
- **Suspension mid-resume-window**: the window is server-side wall
  clock; a phone suspended past it simply full-resyncs — no correctness
  edge, only the fast path lost.
- **KeepAliveConfig on platforms without the syscalls**: Go falls back
  per-OS; the daemon targets macOS/Linux only (existing platform
  matrix), both fully supported.

## Sequencing and commits

House rules: one commit per phase (`git commit --no-edit`), full suites
green before each commit (`go test ./...` + `make preflight`'s mobile
trio: `dart format --set-exit-if-changed`, `flutter analyze`,
`flutter test`), no pushes until the phase batch is reviewed. Order is
strict P0 → P6: P0 is the negotiation substrate everything hangs off;
P1/P2 are server-side and independently shippable (v1 clients benefit
from both immediately); P3 before P4 because resume's `resumed` block
is defined in terms of P3's gap fields; P5 after P4 so the engine
tests can assert against the real v2 daemon behaviours; P6 last. The
Q5/Q6 relay spike (P2.4) runs as a live-tagged test early in P2 and
its answer is written back into 0068 (Q5) regardless of outcome.
Hardware G1 (Part F F6) runs whenever a device exists — it is the only
non-simulator gate in the plan.

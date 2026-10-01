# Network stack assessment: mcremote Go server

- **Status**: Findings for review (analysis only; no code changes)
- **Date**: 2026-07-21
- **Scope**: Go daemon network path — TCP/TLS listener, HTTP surface, WebSocket control plane, admin Unix socket, auth/pair, session event fan-out, and adjacent performance/caching. Mobile client noted only where it constrains server behavior.
- **Companions**: [0001-architecture](0001-MADR-architecture-mcremote.md), [protocol-v1](protocol-v1.md), [hardening-implementation-plan](0054-PLAN-hardening-implementation.md), [mcremote-server-remediation-plan](0055-PLAN-mcremote-server-remediation.md), [0009-post-hardening-action-plan](0009-MADR-post-hardening-action-plan.md)

---

## 1. Executive summary

The mcremote network stack is a **mesh-first, single-host control plane**: TLS-terminated HTTP + WebSocket on a Tailscale/Headscale bind, device tokens + client-key (mTLS-style SPKI) allowlist, short pair codes, and JSON event fan-out to owner devices. Prior hardening (phases 1–6) and server remediation (0–5) left the stack in **good shape for a personal/operator fleet** (one or few phones).

**No new P0** (remotely reachable RCE or unrecoverable design hole) was found in this pass.

Remaining value is mostly **defense-in-depth, stability under edge load, and caching where it cuts hot-path I/O or wire cost** — not a rewrite of transport or protocol.

| Lens | Grade | One-line |
|------|-------|----------|
| **Hardening** | Strong | Bind fail-closed, TLS + pin/ACME, token hash, client key, pair rate limits, owner isolation, WS limits |
| **Stability** | Good | Graceful shutdown, slow-client disconnect, session auto-close; residual: history loss, observability, auth-storm edges |
| **Performance** | Adequate for fleet size | Defaults (8 WS / 16 live) match single-user; history + auth store + pair disk I/O are the main levers |
| **Caching** | Partial | Devices + ACME + live history rings exist; pair codes re-read disk; no response/content cache layer (and little need for HTTP cache) |

---

## 2. Current architecture (as implemented)

```
Phone (Flutter)
    │  wss://  (TLS 1.2+, optional client cert)
    │  GET /v1/ws  +  JSON envelopes (protocol v1)
    ▼
┌─────────────────────────────────────────────────────────────┐
│  net.Listen(tcp, host:port)  host often "tailscale" → CGNAT │
│  http.Server  ReadHeaderTimeout=10s  IdleTimeout=120s       │
│  TLS: selfsigned pin | letsencrypt DNS-01 | off             │
│  ClientAuth: RequestClientCert (SPKI checked in protocol)   │
│                                                             │
│  GET /healthz          unauth liveness {"ok":true}          │
│  GET /v1/hello         Bearer (+ client key) diagnostics    │
│  GET /v1/ws            WebSocket control plane              │
│                                                             │
│  ws.Server → session.Manager → provider (Grok ACP stdio)    │
│  event fan-out: snapshot clients → per-client write 5s      │
└─────────────────────────────────────────────────────────────┘
    │  unix data_dir/admin.sock (0600)
    ▼
  mcremote pair revoke / kick live sockets
```

### 2.1 Layers and ownership

| Layer | Package | Role |
|-------|---------|------|
| Bind / config | `internal/config`, `internal/tailnet` | `listen.host: tailscale` fail-closed; limits |
| TLS identity | `internal/certs`, `internal/daemon` | Self-signed 10y leaf + pin; ACME DNS-01 + fallback |
| HTTP/WS | `internal/ws`, `internal/daemon` | Mux, upgrade, auth, session RPCs, broadcast |
| Device auth | `internal/auth` | Token (SHA-256 at rest), pair codes, prune |
| Sessions | `internal/session` | Live map, owner ACL, 500-event history ring, meta store |
| Providers | `internal/provider/grok` | Local ACP process; event coalesce under backpressure |
| Local admin | `internal/admin` | Unix socket kick after revoke |

### 2.2 Protocol surface (network-facing)

| Endpoint | Auth | Notes |
|----------|------|--------|
| `GET /healthz` | None | Minimal disclosure (intentional) |
| `GET /v1/hello` | Device token (+ client key when enforced) | Version, listen, Headscale URL, TLS mode/fallback |
| `GET /v1/ws` | Token on upgrade or first 30s via `auth` / `pair.claim` | 1 MiB read limit; origin check skipped |
| `admin.sock` | Filesystem 0600 only | `disconnect_device(s)`, `ping` |

### 2.3 Defaults that shape risk and load

| Setting | Default | Effect |
|---------|---------|--------|
| `auth.require_device_token` | true | Auth required on WS |
| `auth.require_client_key` | true | Stolen token alone is insufficient |
| `limits.max_ws_clients` | 8 | Soft connection cap |
| `limits.max_live_sessions` | 16 | Soft provider process cap |
| WS `ReadDeadline` | 60s | Idle authed clients need client traffic (mobile pings every 20s) |
| Broadcast write deadline | 5s | Slow peer disconnected |
| Pair claim (process) | 30/min | Global claim budget |
| Pair claim (conn) | 10 failed | Per-socket burn |
| History ring | 500 events | Memory + wire on `session.history` |

---

## 3. What is already strong (do not re-litigate)

These controls are real in code and largely tested. Treat them as baseline.

### Hardening

1. **Front door bind** — `tailscale` sentinel fails closed; warn on `0.0.0.0`.
2. **TLS by default** — MinVersion TLS 1.2; self-signed is leaf-only (`IsCA: false`); ACME uses DNS-01 only (correct for mesh).
3. **ACME failure mode** — Fall back to self-signed + pin so phones stay connectable; loud WARN on fallback.
4. **Device tokens** — Cryptographic random, prefix, **hashed at rest**, constant-time compare, atomic 0600 file write.
5. **Client-key allowlist (ADR 0005)** — `RequestClientCert` + SPKI fingerprint at protocol layer (typed errors, not opaque TLS alerts); enforced on `/v1/hello` as well as WS.
6. **Pair codes** — Short TTL, hashed storage, per-code attempt burn, process-wide rate limit, 30s unauth window.
7. **Session owner isolation** — List/broadcast/mutate gated by `owner_device_id`.
8. **Revoke kick** — Admin socket closes live WS after store revoke.
9. **WS resource bounds** — Client cap, 1 MiB message limit, write deadline, re-check max clients under lock after accept.
10. **Minimal unauth surface** — Healthz reveals almost nothing; hello is authenticated.

### Stability / performance already present

1. **Broadcast unlock** — Client snapshot under lock; I/O outside lock (slow peer cannot stall accept).
2. **Control-event priority** in Grok path — Coalesce high-frequency chunks; do not drop control under R5=A.
3. **Session lifecycle** — Auto-close on disconnect; close-and-replace create; process-group kill.
4. **HTTP server** — `ReadHeaderTimeout`, `IdleTimeout`, `MaxHeaderBytes`; `WriteTimeout` left 0 deliberately for long-lived WS.
5. **Auth store cache** — Devices held in memory; `LastUsedAt` disk flush debounced (5 min).
6. **ACME cache** — certmagic on-disk + in-process renewal.
7. **Mobile keepalive** — App pings every 20s (under 60s server read deadline).

---

## 4. Hardening opportunities

Severity: **P1** = meaningful security/correctness under realistic misuse; **P2** = defense-in-depth / multi-tenant edge; **P3** = polish.

### H1 — No rate limit on failed `auth` / invalid Bearer (P2)

**Today:** Pair claims are rate-limited (connection + process). Token validation is unbounded: a connected or connecting client can spam `auth` or `Authorization` on `/v1/hello` and force full-store constant-time scans.

**Risk:** Low for a single mesh phone; higher if bind is widened or many untrusted mesh nodes exist. CPU DoS is soft (small device lists), but **auth failure logging** could also become noisy.

**Recommendation:**

- Per-connection failed-auth counter (mirror pair claims).
- Optional process-wide failed-auth budget (e.g. 60/min).
- Exponential backoff or temporary close after N failures.
- Avoid logging full tokens (already hashed path; keep it that way).

### H2 — WebSocket `InsecureSkipVerify: true` (origin) (P3 today / P2 if browser)

**Today:** Origin check is skipped with comment that Flutter web may need flexibility later.

**Risk:** Negligible for native Flutter over mesh. **Material** if a browser client ever loads from an untrusted origin against a daemon reachable from that browser (LAN/mesh).

**Recommendation:** Keep skip for native; when/if web ships, require allowlisted origins or a same-origin + CSRF token design. Document as intentional non-browser posture.

### H3 — Compression context takeover (P3)

**Today:** `websocket.CompressionContextTakeover` enabled.

**Risk:** Classic compression+secrets concerns are reduced under TLS and non-browser attackers, but compression still costs CPU and expands attack surface slightly.

**Recommendation:** Measure mobile battery/CPU on mesh; if savings are small, prefer `CompressionDisabled` or no context takeover for simpler security posture. Not urgent.

### H4 — No per-device concurrent connection cap (P2)

**Today:** Global `max_ws_clients` only. One device can open all 8 slots (reconnect storms, buggy client).

**Recommendation:** Cap concurrent sockets **per `device_id`** (e.g. 2–3): newest wins or reject with `try again later`. Complements revoke kick and reduces reconnect-storm impact.

### H5 — Unauthenticated pre-auth sockets still consume the global client budget (P2)

**Today:** A peer can complete WS upgrade and sit in the 30s unauth window; those sockets count toward `max_ws_clients` (after accept).

**Recommendation:** Separate **pre-auth** and **authed** quotas, or shorter pre-auth deadline under load, or track half-open upgrades before map insert more aggressively. Combined with H1 limits, this hardens “cheap socket hold” behavior.

### H6 — Pair code store: non-constant-time hash lookup + disk round-trip (P3 security / P2 perf)

**Today:** `Claim` loads JSON from disk every time and compares hashes with `==` (not `subtle.ConstantTimeCompare`). Alphabet is small; codes are short-lived.

**Risk:** Timing leak is low practical impact for 8-char codes with attempt limits. Disk I/O under claim spam is more real (see caching).

**Recommendation:** In-memory pair-code map (like devices), constant-time compare on match path, debounce purge writes.

### H7 — TLS cipher / curve policy left to Go defaults (P3)

**Today:** `MinVersion: TLS 1.2` only; no explicit cipher suite or curve preference list on self-signed path. ACME path sets h2/http1.1 NextProtos.

**Recommendation:** Optionally set modern suites / prefer X25519 + AES-GCM/ChaCha20 for operator clarity. Low priority while Go defaults remain strong.

### H8 — Admin socket trust model is OS-user only (by design) (document)

**Today:** Any process as the daemon user can dial `admin.sock` and kick devices.

**Risk:** Correct for single-user laptop daemon; wrong if data dir is shared or multi-user host without care.

**Recommendation:** Document threat model; optional abstract socket or peer-cred check (`SO_PEERCRED`) on Linux if multi-user hosts become a goal.

### H9 — `tls.mode=off` / cleartext tokens (already warned) (ops)

Plaintext remains a footgun if operators disable TLS on non-loopback. Already logs WARN. Keep fail-soft; do not expand plaintext features.

### H10 — Outbound relay not present (product / architecture)

ADR 0001 still lists **outbound relay as primary** networking; ship path is mesh-direct. Until relay exists, phones off-mesh cannot connect — that is availability, not a hardening hole of the current path. **Design locked** in [MADR 0015](0015-MADR-mcrelay-transport-security.md): opaque join + end-to-end TLS to mcremote, full protocol parity, transport hardening (S1–S13). Implementation is 0009 Phase E0–E4.

---

## 5. Stability opportunities

### S1 — Server-side disconnect observability (P1 ops)

**Today:** Slow-client close is often `Debug`; production diagnosis of flapping phones is hard (also called out in 0009 B.3).

**Recommendation:** Log at **Info** once per disconnect with `device_id`, reason (`slow_client` | `auth_timeout` | `read_deadline` | `revoked` | `client_gone`), and optional remote addr (careful with privacy on shared logs).

### S2 — History ring is live-only (P1 product / UX)

**Today:** 500-event in-memory ring dies on session close or daemon restart; `session.history` returns empty. Protocol-compatible but surprising.

**Recommendation:** Short term — document + mobile banner (0009 B.1). Medium term — durable transcript (0009 Phase D). Network impact of durability: larger history payloads → need pagination (see P3).

### S3 — Permission waiter hygiene on close (P1)

**Today:** Grok permission cancel paths can use non-blocking send; waiters may hang if channel full (0009 B.2).

**Network effect:** Stuck permission state causes client retries / UI lock, increasing reconnect and prompt traffic.

**Recommendation:** Close or timeout waiter channels on session Close so protocol path unblocks cleanly.

### S4 — Broadcast is sequential per event (P2 scale)

**Today:** `BroadcastEvent` writes targets one-by-one with 5s each. With max 8 clients this is fine (worst case tens of seconds for a pathological multi-slow case, but each slow peer is closed).

**If multi-device grows:** Parallelize writes with a small worker pool / errgroup, still per-client deadlines. Not needed at current limits.

### S5 — Auth store never flushes `LastUsedAt` on shutdown unless dirty interval hit (P3)

**Today:** Debounced flush; daemon shutdown does not obviously call `store.Flush()`.

**Impact:** Last-used timestamps for prune can be stale after crash/restart.

**Recommendation:** Call `auth.Store.Flush()` (and pair store if cached) during graceful shutdown next to `httpServer.Shutdown`.

### S6 — Single-threaded message handle per connection (OK)

One read loop per client; handlers are synchronous. A slow `session.create` (provider Start) blocks that client’s other messages but not other clients — correct. Ensure create always has ctx timeout from client/network cancel (relies on request context; WS uses `r.Context()` for handleMessage after read — good).

### S7 — ACME startup can delay listen up to ~3 minutes

Synchronous `ManageSync` before serve; failure falls back. Stability for phones is good (fallback); operator experience is “slow start” on first ACME. Acceptable; optional async “serve self-signed first, upgrade to ACME when ready” is a future ops enhancement (complex for clients mid-flight).

### S8 — Reconnect storms after mass disconnect

Mobile exponential backoff exists. Server-side: combining **H4 per-device caps**, **S1 logging**, and optional **jittered 503 / close reason** on overload would improve mesh-wide flaps (e.g. host sleep/wake).

---

## 6. Performance opportunities

### P1 — `session.history` is a single large JSON frame (high value if history grows)

**Today:** Up to 500 events marshaled into one envelope; WS read limit 1 MiB on **inbound** only. Outbound history can still be large (many tool/chunk events).

**Issues:**

- Main-thread JSON marshal on server for large rings.
- Mobile parse cost and memory spike.
- No incremental sync (`since_seq` / cursor).

**Recommendations (ordered):**

1. **Pagination / cursor** — `session.history` with `limit` + `before`/`after` seq; default last N messages.
2. **Wire filtering** — Optional “ui-relevant only” history (drop thought chunks, collapse tool noise) for mobile replay.
3. **Streaming multi-frame history** — Multiple `session.history_chunk` frames if remaining on single 1 MiB budget is tight.
4. Keep live push as small events (already chunked from provider).

### P2 — Auth `Validate` is O(devices) linear scan (low value today)

Fine for &lt; tens of devices. If prune is neglected and many abandoned devices accumulate:

- Index `token_hash → index` map updated on create/revoke.
- Or store truncated hash prefix buckets.

Do not optimize before multi-device scale is real.

### P3 — Pair code `Claim` / `Create` always hit disk (medium)

Every claim: read file → parse → write file. Under pair UI retries this is unnecessary latency and fsync pressure.

**Fix:** In-memory authoritative map + write-through (same pattern as `auth.Store`).

### P4 — Session meta `persist` on status churn (medium-low)

Status transitions write `meta.json` under lock-friendly paths. High-frequency status could be debounced like `LastUsedAt`.

### P5 — Event path: double JSON (provider → event → envelope)

Each broadcast: `protocol.NewEnvelope` + `json.Marshal` per client. With 1–2 phones this is noise. Optional later:

- Marshal envelope once, write same bytes to all targets (same payload for multi-device owner case is rare today — owner filter usually 1 client).
- For multi-subscriber future: single marshal, fan-out bytes.

### P6 — Grok coalesce is already the right performance tool for mesh RTT

Coalesce-on-backpressure reduces WS frames on slow links without dropping reply text. Preserve this; any “cache” of partial assistant text on server is already this mechanism.

### P7 — HTTP/2 on ACME TLS config vs WebSocket

ACME `TLSConfig` advertises `h2`. WebSocket upgrade is HTTP/1.1; h2 is unused for the main path. Harmless. Do not invest in h2 push/caching for this API.

### P8 — TCP keepalive / dialer tuning (P3)

Default Go listener; no explicit `TCPKeepAlive`. Mesh peers can go dark (phone sleep, NAT). Application ping (20s) is the real keepalive. Optional: set `http.Server` ConnState / custom `ListenConfig` keepalive for dead connection reaping when app is killed without TCP FIN.

---

## 7. Caching: where it helps and where it does not

### 7.1 Existing caches (keep)

| Cache | Location | Benefit |
|-------|----------|---------|
| Device records in memory | `auth.Store` | Avoid disk on every Validate |
| Debounced LastUsedAt | `auth.Store` | Cut fsyncs |
| Live session map | `session.Manager` | Hot path for prompt/cancel |
| History ring (500) | per session entry | Reconnect UX without disk |
| certmagic ACME storage | `<data_dir>/acme` | Avoid re-issue every start |
| Self-signed cert files | `tls.crt` / `tls.key` | Stable pin identity |
| Grok `coalesced` text | provider session | Backpressure without data loss |
| Mobile secure store + local settings | Flutter | Offline pair material; not server |

### 7.2 High-value caching / memoization to add

| Opportunity | Expected gain | Complexity | Notes |
|-------------|---------------|------------|-------|
| **In-memory pair code store** | Lower claim latency; less disk thrash under retry | S | Mirror devices store; TTL purge timer |
| **Token hash → device index** | Faster Validate at large N | S | Only if device count grows |
| **History pagination + optional disk tier** | Faster reconnect + restart survival | M–L | Product (Phase D); biggest user-visible perf |
| **Marshal-once broadcast** | CPU on multi-subscriber | S | Marginal at N≤8 |
| **providers.list snapshot** | Trivial | XS | Registry is already in-memory; optional ETag if polled heavily |
| **Debounced session meta writes** | Disk under status storms | S | |

### 7.3 Caching that is **not** worth it (or harmful)

| Idea | Why skip |
|------|----------|
| HTTP reverse-proxy cache for `/v1/*` | Almost no cacheable GET traffic; hello is auth+dynamic; WS is not HTTP-cacheable |
| Caching Bearer validation results by raw token | Security smell (secret in cache key/memory longer); Validate is already cheap with few devices |
| Caching WebSocket event streams | Ordering/permissions make shared caches wrong; per-owner delivery is required |
| Redis / external cache | Single-node daemon; ops cost dwarfs benefit |
| CDN in front of mesh IP | Wrong trust/topology model |
| Long-lived prompt response cache | Agent answers are non-deterministic and side-effecting |

### 7.4 Caching and security interaction

- Any durable transcript cache (Phase D) must stay **0600 under data_dir**, same uid as daemon, and **owner-scoped** on read (already enforced for live history).
- Do not cache pair plaintext codes; only hashes (already).
- ACME cache dir permissions matter; already 0700 on create path.
- Client-side transcript cache on phone (mentioned in mobile README as missing durable history) is a **client** win and reduces `session.history` pressure after reconnect — complementary to server pagination.

### 7.5 Performance value model (qualitative)

Assume fleet: **1–2 phones**, **few live sessions**, mesh RTT tens of ms.

| Investment | User-visible impact |
|------------|---------------------|
| History pagination + wire filtering | **High** — faster open chat, fewer OOM/parse stalls |
| Durable history | **High** product; medium network (more data once, less confusion) |
| Pair code memory store | **Low–medium** only during pairing UX |
| Auth index / marshal-once | **Negligible** until multi-device or many sessions |
| HTTP caching layer | **None** |

**Bottom line:** Caching value on this server is **not CDN-style HTTP caching**. It is **(1) keep hot auth/session state in memory (mostly done), (2) stop re-reading pair files, (3) structure history so reconnect does not dump 500 raw events in one frame, (4) optional durable transcript with retention.**

---

## 8. Threat model reminder (scopes recommendations)

| Trust zone | Assumption |
|------------|------------|
| Daemon host OS user | Fully trusted (admin sock, data dir, provider processes as user) |
| Tailnet peers | Partially trusted — device token + client key required |
| Public internet | Not on default bind; ACME only via DNS-01 outbound |
| Paired phone | Trusted device; can run agent actions as host user once connected |
| Stolen token without client key | Rejected when `require_client_key` true |
| Stolen token + stolen client key material | Equivalent to phone compromise — out of scope for network-only controls |

Hardening that assumes multi-tenant SaaS (strict isolation, WAF, edge rate limits) is out of scope unless product goals change.

---

## 9. Prioritized roadmap (recommended)

### Tier 0 — Already good; maintain

- Keep TLS default, client key, owner isolation, client/session caps, broadcast deadlines.
- Keep verification gate: `go test` + race on `ws` / `session` / `auth` / `daemon`.

### Tier 1 — High ROI, small–medium (next engineering)

| ID | Item | Lens | Size |
|----|------|------|------|
| S1 | Info-level disconnect reasons | Stability / ops | S |
| S5 | Flush auth store on shutdown | Stability | XS |
| H1 | Failed-auth rate limits | Hardening | S |
| H4 | Per-device WS connection cap | Hardening / stability | S |
| H6 / cache | In-memory pair code store | Hardening + perf | S |
| S3 | Permission waiter close hygiene | Stability | S |
| P1a | History `limit` + protocol note | Performance | S–M |

### Tier 2 — Product-linked

| ID | Item | Lens | Size |
|----|------|------|------|
| S2 / D | Durable transcript + retention | Stability + cache | L |
| P1b | History cursor / multi-frame | Performance | M |
| H5 | Pre-auth vs authed quotas | Hardening | S |
| Relay (0009 E) | Off-mesh reachability | Architecture | L |

### Tier 3 — Optional / later

- Origin allowlist for browser clients  
- Compression policy revisit  
- Explicit TLS cipher suites  
- TCP keepalive ListenConfig  
- Token hash index  
- Parallel broadcast  

---

## 10. Suggested acceptance criteria (if Tier 1 is implemented)

1. **Auth spam:** 100 invalid `auth` messages on one connection → rate limit / disconnect; process remains healthy; valid pair still works after window.
2. **Per-device cap:** Third concurrent socket for same device is rejected or replaces oldest; other devices unaffected.
3. **Pair store:** Claim path does not re-read disk on every attempt (unit test with injectable clock/fs or save counters).
4. **Disconnect logs:** Killing a slow client produces a single Info line with reason.
5. **History limit:** `session.history` with default/max limit returns ≤ N events; protocol doc updated.
6. **Regression:** Existing WS/session/auth/daemon tests + race remain green.

---

## 11. Conclusion

The mcremote network stack is **already hardened for its intended deployment** (operator mesh, few devices, TLS + device identity + session ownership). The highest-value remaining work is not a new transport, but:

1. **Close soft abuse edges** (auth rate limits, per-device connection caps, pre-auth budget).  
2. **Make production behavior observable** (disconnect reasons, shutdown flushes).  
3. **Treat “cache” as hot-path state and history design** — finish memory consistency for pair codes; invest in **bounded, ideally durable, paginated history** rather than HTTP caches.

This assessment is intentionally non-implementing so it can be reviewed against 0009 and product priorities (mesh reliability vs durable chat vs relay) before scheduling work.

# MADR 0016: mcrelay audit — findings & P1–P6 hardening

- **Status**: Accepted (active)
- **Date**: 2026-07-23
- **Deciders**: Project Owner
- **Context**: Deep audit of the mcrelay stack after E0–E3 and ACME HTTP-01:
  `internal/relay`, `internal/relayhost`, mobile `RelayTransport` / path
  selection, certs HTTP-01, config/CLI. Focus: residual bugs, incomplete
  wiring, concurrency, public-edge hardening, stability.
- **Extends**: [MADR 0015](0015-MADR-mcrelay-transport-security.md)

**Companions**

| Doc | Role |
|-----|------|
| [0015-MADR-mcrelay-transport-security.md](0015-MADR-mcrelay-transport-security.md) | Trust model, join plane, phases E0–E3 |
| [config-mcrelay.md](config-mcrelay.md) | Operator config surface |
| [0009-MADR-post-hardening-action-plan.md](0009-MADR-post-hardening-action-plan.md) | Product Phase E tracking |

---

## 1. Executive summary

The relay is an **MVP-complete join-plane router** with the right trust shape
(opaque splice + inner TLS to mcremote). No P0 (remote RCE / auth bypass that
mints host sessions without credentials) found.

Highest remaining value: **capacity accounting under failure**, **host-client
reconnect backoff**, **limit defaulting**, **mobile byte-pipe races**, and
**public-edge Origin / rate-limit hygiene**.

| Lens | Grade | One-line |
|------|-------|----------|
| Trust model (0015) | **Strong** | Inner TLS + client key preserved when E3 path is used |
| Join-plane correctness | **Good / leaky** | Happy path works; phone slots leak on some failures |
| Public-edge security | **Adequate** | TLS OK; Origin `*` and unbounded rate map are soft |
| Host client stability | **Good** | Reconnect works; backoff never resets after success |
| Mobile relay path | **Fragile** | Unsynced peer + no buffer under early frames |
| Completeness vs 0015 | **MVP+** | Core join-plane + hardening shipped; device smoke is operator exit |

**Baseline verification (audit day):** `go test ./internal/relay/...
./internal/relayhost/... ./internal/certs/...`, Flutter relay unit tests green.

---

## 2. What is already solid (do not redo)

1. **Trust boundary:** Relay does not parse protocol-v1; host registration secret
   required; phone join cannot mint mcremote sessions alone.
2. **Host outbound:** mcremote dials mcrelay; no inbound required on the agent host.
3. **Inner identity:** Phone pin / `mode` / client key target **mcremote**, not the
   relay leaf (loopback + `connectionFactory` + SNI).
4. **ACME HTTP-01** on mcrelay (public edge) separate from mcremote DNS-01.
5. **Pair URI** `relay` + `hid` without registration secret on the QR.
6. **setup-service** + XDG default config for mcrelay.

---

## 3. Findings (severity-ordered)

### P1 — Correctness / stability

| ID | Finding | Where |
|----|---------|--------|
| **R1** | Host unregister with pending joins closes `ready` but never decrements `phones` → permanent join limit until restart | `hub.unregister` |
| **R2** | `completeTunnel` `already_claimed` deletes pending without releasing `phones` | `hub.completeTunnel` |
| **R3** | `relayhost.Client.Run` multiplies reconnect backoff forever; never resets after a successful `register_ok` | `relayhost/client.go` |
| **R4** | `New`: `MaxHosts == 0` replaces **entire** `Limits` with defaults, wiping partial operator overrides | `relay.New` |
| **R5** | No app-level ping on host control; idle middleboxes drop registration silently *(deferred past P1–6)* | control path |
| **R6** | Mobile `_peer` unsynchronized; outer binary frames before accept are dropped → stuck inner TLS | `relay_transport.dart` |
| **R7** | Direct probe is TCP-only *(deferred)* | mobile client |

### P2 — Security / hardening

| ID | Finding |
|----|---------|
| **R8** | `OriginPatterns: ["*"]` lets any browser open join-plane sockets |
| **R9** | Per-IP accept rate map grows without bound |
| **R10–R14** | Host enumeration, healthz leakage, tunnel re-sends secret, ACME :80 contention *(backlog)* |

### P2/P3 — Incomplete / polish

| ID | Finding |
|----|---------|
| **R15** | No splice idle / max lifetime |
| **R16** | Single pre-auth rate bucket |
| **R17** | Shutdown does not drain hijacked WS splices |
| **R18–R28** | Orphan tunnels, e2e CI, metrics, admin, typed join errors, etc. |

---

## 4. Decisions locked for this remediation

| # | Topic | Chosen |
|---|--------|--------|
| **D1** | Capacity accounting | Single `releasePhone` helper; all exit paths that held a slot call it |
| **D2** | Limit defaults | Field-wise fill (`ResolvedLimits`), never wholesale replace |
| **D3** | Host reconnect backoff | Reset to 1s after successful `register_ok` |
| **D4** | Mobile outer pipe | Mutex + bounded buffer of pre-peer frames (drop oldest if full) |
| **D5** | WS Origin | Default **no wildcard** — native / empty Origin only; opt-in allowlist later |
| **D6** | Rate map | Prune windows older than 2 minutes on each accept check; cap map size |

---

## 5. Phased work

### P1–P6 (this ADR — implement now)

| # | Work | Maps to |
|---|------|---------|
| **1** | R1 phone release on unregister pending | hub |
| **2** | R2 phone release on already_claimed | hub |
| **3** | R3 backoff reset | relayhost |
| **4** | R4 `ResolvedLimits` | relay.New / config |
| **5** | R6 peer lock + outer buffer | Flutter `RelayTransport` |
| **6** | R8 Origin harden + R9 rate GC | `server.go` |

### Follow-on tranche (R5 / R15 / R17 / R20 — shipped with E4)

| # | Work | Status |
|---|------|--------|
| R5 | Host control app-level Ping on `RegisterIdle` | **Done** |
| R15 | Splice idle + max lifetime | **Done** (`splice_idle_seconds` / `splice_max_seconds`) |
| R17 | Shutdown drains tracked splices | **Done** |
| R20 | e2e tests + CI race on relay packages | **Done** |

### Edge / stability tranche (R7 / R10–R14 / R16 / R18 — shipped)

| # | Work | Status |
|---|------|--------|
| R7 | Mobile direct probe: healthz → TLS → TCP | **Done** |
| R10 | Join errors do not enumerate allowlist; per-host join rate | **Done** |
| R11 | `/healthz` liveness-only `{"ok":true}` | **Done** |
| R12 | Short-lived dial `tunnel_token` (no secret re-send on tunnel) | **Done** |
| R13 | ACME HTTP-01 errors name challenge port contention | **Done** |
| R16 | Separate accept / join / register / join-host rate buckets | **Done** |
| R18 | Orphan pending-join GC sweeper | **Done** |

### Explicitly deferred (non-goals / product later)

- **R14** deeper ACME multi-process coordination (ops: exclusive port 80 or alternate `http_port`)
- Public Prometheus metrics / admin socket (0015 D10 non-goal for v1)
- Short-lived **join tickets** (0015 H6 reserved extension)
- Multi-tenant SaaS

---

## 6. Verification

```bash
go test ./internal/relay/... ./internal/relayhost/... ./internal/certs/...
cd apps/mobile && flutter test test/relay_transport_test.dart test/relay_path_test.dart
```

Each logic fix should carry a regression test where practical (hub capacity;
limits resolution; backoff via observed reset is harder — code review + unit
hook if cheap).

---

## 7. Implementation log

| Date | Change |
|------|--------|
| 2026-07-23 | Audit written; P1–P6 implemented. |
| 2026-07-23 | P1–P2: `releasePhoneLocked` on unregister pending + already_claimed; re-register preserves `phones`; nil-safe control Close. |
| 2026-07-23 | P3: `relayhost` resets backoff to 1s after `register_ok`; `normalizeRelayURL` strips path. |
| 2026-07-23 | P4: `ResolvedLimits` field-wise defaults in `relay.New`. |
| 2026-07-23 | P5: Flutter `RelayTransport` peer mutex + outer frame buffer (cap 64). |
| 2026-07-23 | P6: Origin empty (no `*`); rate map TTL prune + hard cap 4096 (make room before new IP). |
| 2026-07-23 | Tests: `hub_test`, `rate_test`, `backoff_test`, ResolvedLimits, Flutter relay. |
| 2026-07-23 | R5 host control Ping; R15 splice idle/max; R17 shutdown drain; R20 e2e + CI race. |
| 2026-07-23 | E4 ops: `docs/ops-mcrelay.md`, `deploy/systemd/mcrelay.user.service`, smoke checklist. |
| 2026-07-23 | R7 healthz/TLS probe; R10 join hygiene + per-host rate; R11 healthz; R12 tunnel token; R13 ACME port errors; R16 multi-bucket rates; R18 pending GC; Phase E security e2e. |

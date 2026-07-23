# MADR 0016: mcrelay audit — findings & P1–P6 hardening

- **Status**: Accepted (active)
- **Date**: 2026-07-23
- **Deciders**: Project Owner
- **Context**: Deep audit of the mcrelay stack after E0–E3 and ACME HTTP-01:
  `internal/relay`, `internal/relayhost`, mobile `RelayTransport` / path
  selection, certs HTTP-01, config/CLI. Focus: residual bugs, incomplete
  wiring, concurrency, public-edge hardening, stability.
- **Extends**: [MADR 0015](0015-mcrelay-transport-security.md)

**Companions**

| Doc | Role |
|-----|------|
| [0015-mcrelay-transport-security.md](0015-mcrelay-transport-security.md) | Trust model, join plane, phases E0–E3 |
| [config-mcrelay.md](config-mcrelay.md) | Operator config surface |
| [0009-post-hardening-action-plan.md](0009-post-hardening-action-plan.md) | Product Phase E tracking |

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
| Completeness vs 0015 | **MVP** | Idle timeouts, shutdown drain, e2e CI still open |

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

### Later (not this PR)

R5 control ping, R7 healthz probe, R15 idle splice, R17 shutdown drain, R20 e2e CI,
join tickets, metrics, admin sock.

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
| 2026-07-23 | Audit written; P1–P6 implemented (see commit). |

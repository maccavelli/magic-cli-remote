# MADR 0017: mcrelay memory / GC / overflow / security action plan

- **Status**: Accepted (decisions locked; implementation pending)
- **Date**: 2026-07-23
- **Deciders**: Project Owner
- **Context**: Follow-on deep dive after MADR 0016 (R1–R18 shipped). Lenses:
  memory retention, integer/config overflow, GC pressure, public-edge security
  hardening. Scope: `cmd/mcrelay`, `internal/relay`, `internal/relayhost`.
- **Extends**: [MADR 0015](0015-mcrelay-transport-security.md),
  [MADR 0016](0016-mcrelay-audit-hardening.md)
- **Companions**: [config-mcrelay.md](config-mcrelay.md), [ops-mcrelay.md](ops-mcrelay.md)
- **Implementation scope**: Phases **A–D** plus **E2** (splice buffer pooling
  study/implementation per D15). Phase E1/E3/E5 remain deferred.

---

## 1. Executive summary

MADR 0016 closed the join-plane correctness and baseline edge issues (phone-slot
leaks, Origin `*`, unbounded rate map, tunnel tokens, splice lifetime, orphan
GC). A second pass focused on **resource abuse and residual hardening** found
no P0 (RCE / auth bypass / content disclosure of inner TLS). Residual value is
in **unauthenticated memory DoS**, **limit clamping**, **capacity under host
flap**, and **constant-time / legacy-secret polish**.

| Lens | Grade after 0016 | Residual risk |
|------|------------------|---------------|
| Memory retention | Good / soft under attack | Huge `host_id` as rate-map keys; no upper clamp on limits |
| Overflows | Low | Config durations/sizes can wrap or OOM; counters otherwise safe |
| GC | Acceptable for MVP | Per-frame alloc on splice; authenticated control spam |
| Security hardening | Adequate → Strong with this plan | Timing oracle, legacy tunnel secret, shutdown drain gaps |

**Baseline verification (plan day):**

```bash
go test -race ./internal/relay/... ./internal/relayhost/...
```

Both packages **PASS** with the race detector.

---

## 2. Do not redo (0016 shipped)

Treat these as done unless regression tests fail:

| ID | Topic |
|----|--------|
| R1–R2 | Phone release on unregister pending / already_claimed |
| R3 | Host client backoff reset after `register_ok` |
| R4 | Field-wise `ResolvedLimits` |
| R5 | Host control app Ping |
| R8–R9 | Origin harden + rate map TTL/cap |
| R10–R12 | Join enumeration, healthz, tunnel tokens |
| R15–R18 | Splice idle/max, shutdown splice drain, multi-bucket rates, orphan sweeper |

---

## 3. Findings inventory (this pass)

IDs continue the **R** series from 0016 so logs and PRs can cite one namespace.

### 3.1 Severity model

| P | Meaning |
|---|--------|
| **P0** | Remotely reachable RCE, auth bypass that mints host power, or inner-TLS content break |
| **P1** | Unauthenticated resource exhaustion or clear policy hole under normal public exposure |
| **P2** | Hardening / accounting / correctness under failure; needs a small decision or careful design |
| **P3** | Perf, polish, ops, deferred product |

### 3.2 Table

| ID | P | Lens | Finding | Where |
|----|---|------|---------|--------|
| **R29** | P1 | Memory / DoS | Join (and related) paths do not validate `host_id` length/charset before rate-map keys; attacker can store ~`MaxMessageBytes` keys up to `rateMapMax` (4096) | `server.handlePhone` → `allowRate(join.HostID, …)` |
| **R30** | P1 | Memory / OOM | No upper bound on `MaxMessageBytes` / fan-out limits; config or env can force multi-GB frame buffers | `ResolvedLimits`, `FileConfig.ToServerConfig` / `Validate` |
| **R31** | P2 | Capacity | Host unregister drops host slot while **active** splices continue; `endPhone` no-ops if host gone; re-register starts at `phones=0` → concurrent splices can exceed `MaxPhonesPerHost` | `hub.unregister`, `releasePhoneLocked`, `endPhone` |
| **R32** | P2 | Lifecycle | `Serve` error path does not `closeAllSplices`; host control WS never tracked for drain | `server.Serve`, `handleHost` |
| **R33** | P2 | Security | `checkSecret` early-returns for unknown `host_id` → timing oracle for allowlist membership (error text already uniform) | `hub.checkSecret` |
| **R34** | P2 | Security | Legacy tunnel claim still accepts long-lived registration secret | `hub.completeTunnel` |
| **R35** | P2 | Security / ops | Logs can emit attacker-controlled `host_id` (size/content) until R29 | `handlePhone` / register deny logs |
| **R36** | P3 | Security / ops | No trusted-proxy model; `RemoteAddr` only (correct default; breaks rate limits behind unconfigured reverse proxy) | `clientIP` |
| **R37** | P3 | GC | Splice allocates a new buffer per frame both directions (library `Read`) | `splice` / `relayhost.bridge` WS→TCP |
| **R38** | P3 | GC | Host control read loop discards frames up to full `MaxMessageBytes` (authenticated host GC DoS) | `handleHost` read loop |
| **R39** | P3 | Contended lock | Full rate-map scan under `rateMu` on every check (bounded by 4096) | `pruneRateLocked` |
| **R40** | P3 | Overflow | Large YAML duration/size ints can wrap `time.Duration` or inflate maps; counter `++` paths otherwise safe | `ToServerConfig`, limits |

**Not findings (explicit non-issues this pass):**

- Classic phone-slot stuck-at-max after cancel/unregister (fixed R1/R2; tests green).
- Permanent `pending` / `activeSplices` map growth under happy path.
- Integer overflow RCE in pure-Go splice.
- Content confidentiality of protocol-v1 through the relay (inner TLS; 0015 D2).

---

## 4. Decisions (locked 2026-07-23)

Owner locked: **recommendations for D7–D12, D14, D16**; overrides **D13 #2**,
**D15 #2**. Implementation scope: **Phases A–D + E2**.

| # | Topic | Locked choice |
|---|--------|---------------|
| **D7** | Join/register/tunnel id validation | **Reuse `validateHostID`** (≤128, `[A-Za-z0-9._-]`) on **all** join-plane first messages; reject `bad_payload` before rate keys that embed the id |
| **D8** | Rate-map key material | **Plain validated id** (short after D7; simpler debug) |
| **D9** | Limit upper clamps | **Hard reject** in `FileConfig.Validate` + **clamp** in `ResolvedLimits`. Ceilings: `max_message_bytes` ≤ **16 MiB** (default 1 MiB); `max_hosts` ≤ **1024**; `max_phones_per_host` ≤ **256**; `max_concurrent_join` ≤ **4096**; rate `*_per_minute` ≤ **100_000**; duration seconds fields ≤ **7 days** |
| **D10** | Active splice vs host flap | **Durable per-host counter** across control drop/re-register; `MaxPhonesPerHost` applies to reserved+active; `endPhone` always decrements that counter (do **not** cancel splices on unregister) |
| **D11** | Shutdown / Serve exit | On **any** Serve exit: `closeAllSplices` + `hub.closeAllHosts`; track host control for drain |
| **D12** | Register timing | **Always hash** presented secret; compare to real or **dummy** hash in constant time |
| **D13** | Legacy tunnel secret | **`allow_legacy_tunnel_secret` default false now** (override). Token-only tunnel claim unless operator opts in; log when legacy path is used. **Breaks** host clients that still send registration secret without dial token — current `relayhost` already prefers token (R12) |
| **D14** | Trusted proxies | **Defer** — `RemoteAddr` only; document direct edge / LB that preserves src |
| **D15** | Splice GC / buffer pools | **Investigate / implement pooling in this tranche** (override; was “defer”). Phase **E2** promoted into implementation scope with A–D |
| **D16** | Control-plane read limit | **Separate smaller limit** for `/v1/host` control (e.g. **64 KiB**) vs splice `MaxMessageBytes` |

### Alternatives considered (not chosen)

| # | Rejected |
|---|----------|
| D7 | Hash-only rate keys without rejecting long ids; defer validation |
| D8 | SHA-256 hex rate keys |
| D9 | Clamp-only without Validate fail; document-only |
| D10 | Cancel all host splices on unregister; leave under-count behavior |
| D11 | Signal-path-only drain |
| D12 | Accept timing gap |
| D13 | Default true for one release (plan original); always-on no flag |
| D14 | Add `trusted_proxies` now |
| D15 | Defer until prod pprof (plan original) |
| D16 | Single `MaxMessageBytes` for control + splice |

---

## 5. Phased work

### Phase A — Stop unauthenticated memory bombs (P1)

**Goal:** Public edge cannot retain multi‑MB attacker strings in maps or log lines.

| Step | Work | Maps to | Primary files |
|------|------|---------|---------------|
| **A1** | Validate `host_id` on register / join / tunnel with `validateHostID` before rate + hub | R29, R35 | `server.go` |
| **A2** | Validate `session_id` length/charset on tunnel (UUID-shaped or max 64 hex/uuid) | R29 | `server.go`, maybe `protocol.go` helpers |
| **A3** | Truncate/redact `host_id` in slog fields (e.g. max 64 + optional short hash) | R35 | `server.go` |
| **A4** | Unit tests: oversized host_id rejected; rate map key count/size stays small under fuzz-like joins | R29 | `server_test.go` / `e2e_test.go` |

**Exit criteria:**

- Join with 1 MiB `host_id` → `bad_payload` / policy close; `len(rate)` keys remain short.
- Existing e2e + race tests still green.

---

### Phase B — Clamp config and numeric surface (P1/P3)

**Goal:** Misconfig cannot OOM; Duration wrap is rejected.

| Step | Work | Maps to | Primary files |
|------|------|---------|---------------|
| **B1** | `FileConfig.Validate` rejects over-ceiling limits (D9) | R30, R40 | `fileconfig.go` |
| **B2** | `ResolvedLimits` clamps as second line of defense (tests constructing `Config` directly) | R30 | `config.go` |
| **B3** | Document ceilings in `config-mcrelay.md` | R30 | `docs/config-mcrelay.md` |
| **B4** | Tests for reject/clamp of absurd values | R30, R40 | `fileconfig_test.go`, `config` tests |

**Exit criteria:**

- `max_message_bytes: 2000000000` fails Validate (or clamps with test-documented behavior per D9).
- Defaults unchanged for normal configs.

---

### Phase C — Capacity accounting under host flap (P2)

**Goal:** `MaxPhonesPerHost` (and global join/splice policy) holds across disconnect/re-register while splices live.

| Step | Work | Maps to | Primary files |
|------|------|---------|---------------|
| **C1** | Implement D10 accounting (recommended: durable per-host phone/splice counter independent of control conn) | R31 | `hub.go` |
| **C2** | Ensure every splice start/end path adjusts the same counter; re-register must **not** reset active count to 0 | R31 | `hub.go`, `server.handlePhone` |
| **C3** | *(skipped — D10 chose durable counter, not cancel-on-unregister)* | — | — |
| **C4** | Tests: beginJoin → completeTunnel → unregister host → re-register → further joins blocked until endPhone | R31 | `hub_test.go`, e2e |

**Exit criteria:**

- With `MaxPhonesPerHost=1`, one active splice blocks a second join even if control reconnected after drop.
- No permanent stuck-at-max after clean splice end (regression for R1/R2).

---

### Phase D — Lifecycle drain + register crypto hygiene (P2)

**Goal:** Clean process exit; no cheap allowlist timing leak; legacy secret controlled.

| Step | Work | Maps to | Primary files |
|------|------|---------|---------------|
| **D1** | `hub.closeAllHosts` + call from all `Serve` exits alongside `closeAllSplices` | R32 | `hub.go`, `server.go` |
| **D2** | `checkSecret` always hashes; dummy hash for unknown host (D12) | R33 | `hub.go` + test |
| **D3** | Config `allow_legacy_tunnel_secret` **default false** (D13); reject secret-only claims unless true; log when legacy path used | R34 | `config.go`, `fileconfig.go`, `hub.go` |
| **D4** | Control-plane `SetReadLimit` **64 KiB** (D16) | R38 | `server.handleHost` |
| **D5** | Tests: unknown vs known host hash path; legacy flag on/off (default off) | R33, R34 | hub/server tests |

**Exit criteria:**

- Context cancel **and** listener error both tear down splices and host controls.
- Wrong secret and unknown host take comparable work (unit test via same code path / benchmark optional).

---

### Phase E2 — Splice buffer pooling (P3, **in scope** per D15)

**Goal:** Reduce GC pressure on the opaque splice path without changing frame
semantics or correctness.

| Step | Work | Maps to | Primary files |
|------|------|---------|---------------|
| **E2.1** | Benchmark / baseline: splice throughput + `allocs/op` with default 1 MiB cap (use smaller fixtures) | R37 | new `splice_*_test.go` or bench in `server_test` |
| **E2.2** | Pool frame buffers where library + API allow (e.g. reuse write-side scratch if `Read` cannot; document if `coder/websocket` forces per-Read alloc) | R37 | `server.go` `splice`, optionally `relayhost.bridge` |
| **E2.3** | Confirm no use-after-free: buffer returned to pool only after `Write` completes; race tests | R37 | tests |
| **E2.4** | Record outcome in implementation log (pooling shipped **or** “blocked by library; no win”) | R37 | this doc |

**Exit criteria:**

- Either measurable alloc reduction on bench, or a written finding that pooling
  is not viable with `coder/websocket` without unsafe/API change — then close E2.

### Phase E (still deferred)

| Step | Work | Maps to | Notes |
|------|------|---------|--------|
| **E1** | Trusted proxy / PROXY protocol | R36 | D14 defer |
| **E3** | Rate map sharding or background prune | R39 | Only if lock contention shows in pprof |
| **E5** | Metrics: rate map size, active splices, legacy tunnel claims | — | 0015 non-goal for v1 metrics; revisit |

~~E4 default legacy false~~ — **superseded by D13**: default is false immediately.

---

## 6. Dependency graph

```text
Phase A (validate ids) ──────────────┐
Phase B (clamp limits) ──────────────┼──► Phase C (accounting) ──► Phase D (drain + crypto)
                                     │         │
                                     │         └── needs stable Max* meaning
                                     └── A before C tests that use join rate keys

Phase E2 (splice pooling) ── after A/B stable; can parallelize with C/D if PR risk ok
Phase E1/E3/E5  (deferred)
```

- **A** and **B** are independent; implement in parallel.
- **C** should land after **A** so tests use valid ids only.
- **D** independent of **C** except shared `Serve`/`hub` touch — serialize D1 with C if same PR risk is high; prefer separate PRs.
- **E2** can land after A/B (or last); must not regress splice correctness.
- **E1/E3/E5** do not block release of A–D + E2.

---

## 7. PR plan (suggested Graphite / stacked order)

| PR | Title (draft) | Phases | Risk |
|----|---------------|--------|------|
| **PR1** | relay: validate join-plane ids before rate maps | A | Low |
| **PR2** | relay: clamp and validate limit ceilings | B | Low |
| **PR3** | relay: durable phone/splice capacity across host re-register | C | Medium (accounting) |
| **PR4** | relay: full drain on Serve exit + constant-time checkSecret + legacy flag default off | D | Medium |
| **PR5** | relay: splice buffer pooling investigation/implementation | E2 | Medium (perf / lifetime) |

Each PR:

1. `gofmt` / `golint` / `govulncheck` on touched packages (project Go gate).
2. `go test -race ./internal/relay/... ./internal/relayhost/...`
3. Short note in 0017 implementation log (section 10).

---

## 8. Verification matrix

| Check | Command / method | Covers |
|-------|------------------|--------|
| Unit + race | `go test -race ./internal/relay/... ./internal/relayhost/...` | A–D, E2 |
| Hub capacity flap | New tests in Phase C | R31 |
| Oversized id | New tests in Phase A | R29 |
| Limit ceilings | New Validate tests | R30 |
| Legacy secret | Flag matrix: default off rejects secret-only; on accepts | R34 / D13 |
| Manual smoke (optional) | `mcrelay serve` + `mcremote` relay register + phone join | Integration |
| Splice bench | `go test -bench=Splice` (+ race on non-bench tests) | R37 / E2 |
| Profiling (optional) | pprof under splice load if E2 inconclusive | R37, R39 |

**Non-goals for this plan’s exit:** Prometheus dashboards, multi-tenant SaaS, join tickets (0015 H6), ACME multi-process (R14).

---

## 9. Risk and rollback

| Risk | Mitigation |
|------|------------|
| Strict `host_id` validation rejects previously “loose” ids | Align with existing allowlist rules; production hosts already validated at config load |
| Clamp rejects an intentional large `max_message_bytes` | Document ceilings; 16 MiB is far above protocol-v1 needs through opaque splice |
| Durable capacity blocks joins after bugs leave counter high | Reuse `releasePhoneLocked` discipline; sweeper only for pending; add counter self-check in tests; ops restart remains last resort |
| Legacy secret default **false** breaks old hosts | Current `relayhost` sends dial token (R12); operators with third-party/old hosts set `allow_legacy_tunnel_secret: true` |
| Buffer pooling UAF / data races | Return to pool only after Write; `-race`; no pool if API cannot guarantee lifetime |
| Drain on error path races with in-flight handlers | Same pattern as R17 `closeAllSplices`; cancel contexts first |

Rollback: each PR is independently revertable; capacity (PR3) is the highest-risk revert if field reports false `limit` denials; E2 (pooling) next if splice corruption reports.

---

## 10. Implementation log

| Date | Change |
|------|--------|
| 2026-07-23 | Plan written from memory/GC/overflow/security deep dive; status **Proposed**. |
| 2026-07-23 | **Decisions locked.** Recommendations for D7–D12, D14, D16. Overrides: **D13** legacy secret **default false now**; **D15** splice pooling **in scope** (E2). Implementation scope: **A–D + E2**. Status → **Accepted**. |
| 2026-07-23 | **Implemented A–D + E2** in tree: validate join-plane ids; limit ceilings + docs; durable `phones` map (D10); `drainConnections` on all Serve exits; constant-time `checkSecret`; `allow_legacy_tunnel_secret` default false; 64 KiB control read limit; splice `Reader`/`Writer` + `sync.Pool` CopyBuffer; tests + `BenchmarkSpliceRoundTrip`. |

---

## 11. Reviewer sign-off

| Item | Owner | Decision |
|------|-------|----------|
| Accept findings R29–R40 as accurate | Project Owner | **yes** (implicit with decision lock) |
| Lock D7–D16 | Project Owner | **locked** (2026-07-23) |
| Approve implementation scope | Project Owner | **A–D + E2** |
| Defer E1 / E3 / E5 | Project Owner | **yes** |

**Overrides / notes**

```
D13 option 2: allow_legacy_tunnel_secret default false immediately.
D15 option 2: investigate/implement splice buffer pooling in this tranche (Phase E2).
All other decisions: plan recommendations.
```

---

## 12. One-page priority stack

1. **A1–A2** — validate ids (kills the real unauthenticated memory bomb).
2. **B1–B2** — clamp limits (kills config OOM).
3. **C1–C2** — durable capacity (kills host-flap policy bypass).
4. **D1–D2** — drain + constant-time secret check.
5. **D3** — legacy tunnel secret flag **default off**.
6. **D4** — 64 KiB control read limit.
7. **E2** — splice buffer pooling study / implement.
8. E1 / E3 / E5 when convenient (still deferred).

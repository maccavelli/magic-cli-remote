# MADR 0012: mcremote Go daemon assessment & action plan

- **Status**: Accepted (active)
- **Date**: 2026-07-22
- **Deciders**: Project Owner
- **Context**: Fresh deep audit of the mcremote Go daemon after hardening (phases 1–6) and server remediation (0–5). Focus: residual bugs, incomplete wiring, concurrency, Go 1.26.5 practice, performance/stability.

**Companions**

| Doc | Role |
|-----|------|
| [hardening-implementation-plan.md](hardening-implementation-plan.md) | Front door, TLS, client identity — complete |
| [mcremote-server-remediation-plan.md](mcremote-server-remediation-plan.md) | Lifecycle, admin sock, fan-out — code complete |
| [0009-post-hardening-action-plan.md](0009-post-hardening-action-plan.md) | Product polish, durable history, relay |
| [protocol-v1.md](protocol-v1.md) | Wire contract |
| [0001-architecture-mcremote.md](0001-architecture-mcremote.md) | Relay-primary vision vs mesh-first ship |
| [0015-mcrelay-transport-security.md](0015-mcrelay-transport-security.md) | Outbound relay design (E2E TLS splice; Phase E) |

---

## 1. Executive summary

The daemon is a **mature mesh-first control plane**: TLS (self-signed pin + ACME DNS-01 fallback), device tokens + client-key SPKI binding, pair codes, session owner isolation, live limits, admin.sock revoke kick, dual providers (Grok ACP + OpenCode HTTP/ACP), careful process lifecycle, and strong event back-pressure design.

**No P0** (remote RCE or auth-bypass under default prod config) found in the 2026-07-22 audit.

Highest remaining value is **correctness under concurrency**, **paired-device DoS stability**, **OpenCode HTTP fidelity**, and **auth/TLS multi-process atomicity** — then reliability polish and product tracks in 0009.

| Lens | Grade | One-line |
|------|-------|----------|
| Security (personal fleet) | **Strong** | Token + client key + mesh bind + rate limits |
| Lifecycle / concurrency | **Good** | R2–R5 largely solid; residual races on async WS + shutdown |
| Provider fidelity | **Good / uneven** | ACP path polished; HTTP OpenCode has correctness gaps |
| Observability / ops | **Adequate** | slog is good; disconnect reasons often Debug-only |
| Go 1.26 idiomatic | **Strong** | `log/slog`, `context.WithoutCancel`, atomic files |
| Completeness vs ADR 0001 | **Mesh-complete, vision-incomplete** | No outbound relay; history not durable |

**Baseline verification (audit day):** Go 1.26.5, `go test ./...` green, race clean on core packages, `govulncheck` clean.

---

## 2. What is already solid (do not redo)

1. **Front door:** `listen.host=tailscale` fail-closed; warn on `0.0.0.0`; auth on `/v1/hello`; lean `/healthz`.
2. **TLS:** Managed self-signed + LE DNS-01; intentional ACME→self-signed fallback; pair URI advertises **fallback** pin in LE mode.
3. **Auth:** 256-bit tokens, SHA-256 at rest, constant-time compare, debounced `LastUsedAt`, mtime reload for CLI↔daemon, atomic fsync writes, pair rate limits, client-key before burning pair code.
4. **Sessions:** Per-id create locks, `reserved` live cap, close-and-replace, pump identity check, auto-close on disconnect, owner ACL, history ring + `seq` + replay filter.
5. **WS fan-out:** Bounded outbound queue, non-blocking enqueue, 5s write deadline, slow-client disconnect, 1 MiB read limit, field length caps.
6. **ACP providers:** Process groups, PID-recycle guards, control-event non-drop, chunk coalesce, permission timeout, stall notices, prewarm.
7. **Admin:** Unix `admin.sock` 0600, stale-socket ping, revoke → `DisconnectDevice`.
8. **Test depth:** Strong coverage on `session`, `ws`, `auth`, `acpagent`, config/certs.

---

## 3. Findings (severity-ordered)

### P1 — Correctness / stability

| ID | Finding | Where |
|----|---------|--------|
| **P1-1** | Read-loop starvation: `session.prompt`, `history` stay sync; `/model`/`/reset` and OpenCode HTTP Prompt can block for seconds | `ws/server.go`, `httpagent`, `commands.go` |
| **P1-2** | `deviceID` data race: async handlers read `c.deviceID` unlocked while `setAuthed` writes under `s.mu` | `ws/server.go` |
| **P1-3** | Unbounded `dispatchAsync` goroutines (paired-device DoS) | `ws/server.go` |
| **P1-4** | OpenCode HTTP model JSON: create uses `id`, prompt uses `modelID` (must not unify) | `opencode/http.go` |
| **P1-5** | Auth multi-process lost updates: CLI+daemon RMW without flock | `auth/store.go`, `paircode.go` |
| **P1-6** | Close-and-replace Start failure leaves non-live id; `/reset` recovery weaker than `/model` | `session/manager.go`, `commands.go` |

### P2 — Structural gaps

| ID | Finding |
|----|---------|
| **P2-1** | HTTP OpenCode never deletes server-side session on `session.delete` |
| **P2-2** | HTTP `permission_resolved` always reports resolved when cancelled |
| **P2-3** | Cert generate: key-then-cert can strand identity mid-renewal |
| **P2-4** | Shutdown vs Create: session can reappear after CloseAll |
| **P2-5** | Pair code burned before device create succeeds |
| **P2-6** | Re-auth can flip device identity on a live socket |
| **P2-7** | History live-only ring; no outbound byte cap |
| **P2-8** | `Meta.Model` not persisted |
| **P2-9** | ACP FS/terminal unsandboxed (single-operator OK) |
| **P2-10** | No unit tests for `internal/provider/httpagent` |
| **P2-11** | HTTP engine stderr discarded |
| **P2-12** | Disconnect reasons often Debug-only |
| **P2-13** | Builtin command catalog not on wire (mobile hardcodes) |
| **P2-14** | Docs/plan hygiene (stale checkboxes, stray files) |

### P3 — Polish / product backlog

Grok prewarm default off; config validation edges; status persist fsync load; freePort TOCTOU; WS origin/compression for future web; outbound relay (0001); durable transcript (0009 D); Antigravity / push (0009 F).

---

## 4. Decisions (locked for this plan)

| # | Topic | Chosen |
|---|--------|--------|
| **D1** | Durable history short term | Document + UX (0009 A1); full disk = Phase D product |
| **D2** | Multi-process auth | **flock** on devices/pair_codes (not admin-only mutations) |
| **D3** | Async concurrency per WS client | **2** in-flight |
| **D4** | FS jail | Defer; document single-operator threat model |
| **D5** | Sticky auth | One device identity per socket lifetime |

---

## 5. Phased work

### Severity model

| P | Meaning |
|---|---|
| **P0** | Remote RCE / no recovery |
| **P1** | Correctness or stability in normal use |
| **P2** | Structural gap / incomplete wiring |
| **P3** | Polish, docs, product backlog |

### Phase 0 — Guardrails (S)

| # | Work | Status |
|---|------|--------|
| 0.1 | Race/concurrency tests for async create + device id | **Implement with Phase 1** |
| 0.2 | Flood create → bounded concurrency | **Implement with Phase 1** |
| 0.5 | Auth concurrent RMW test (two Stores) | **Implement with Phase 1** |

(OpenCode model / HTTP permission tests belong with Phase 2.)

**Gate:** `go test -race ./internal/ws/... ./internal/session/... ./internal/auth/...`

### Phase 1 — Concurrency & control-plane stability (P1) — **ship first**

| # | Work | Files | Status |
|---|------|-------|--------|
| **1.1** | Snapshot `deviceID` under `s.mu` for all async handlers | `ws/server.go` | **This PR** |
| **1.2** | Per-connection async semaphore (max 2); `rate_limited` when full | `ws/server.go` | **This PR** |
| **1.3** | Async `session.prompt` + `session.history`; keep cancel on read path | `ws/server.go` | **This PR** |
| **1.4** | Sticky auth: refuse second device on same socket | `ws/server.go` | **This PR** |
| **1.5** | File lock around load→mutate→write for devices + pair codes | `auth/*` | **This PR** |
| **1.6** | Shutdown gate: `Manager` rejects Create after CloseAll; drain create locks | `session`, daemon | **This PR** |

### Phase 2 — Provider fidelity (P1–P2)

| # | Work | Status |
|---|------|--------|
| **2.1** | OpenCode HTTP model JSON: create=`id`, prompt=`modelID` (reverted false unify 2026-07-22) | **Done** |
| **2.2** | HTTP `permission_resolved` status = cancelled when cancelled | **Done** |
| **2.3** | Engine `DELETE /session` on purge (`PurgeSession`) | **Done** |
| **2.4** | Log provider `Close`/`Purge` errors | **Done** |
| **2.5** | ACP permission waiters unblocked + resolved events before `done` | **Done** |
| **2.6** | `/reset` retry recovery (mirror `/model`) | **Done** |
| **2.7** | HTTP engine stderr ring + health-fail tail | **Done** |
| **2.8** | `httpagent` unit tests | **Done** |

### Phase 3 — TLS / pair / durability edges (P2)

| # | Work | Status |
|---|------|--------|
| **3.1** | Atomic cert pair publish (`*.new` stage + promote) | **Done** |
| **3.2** | Pair Take + Restore on device create failure | **Done** |
| **3.3** | Persist `Model` on session Record / list | **Done** |
| **3.4** | History durability | **Done** (0009 Phase D — `history.json` ring tail) |
| **3.5** | History paging (`since_seq` / `limit` / `truncated`) + byte budget | **Done** |
| **3.6** | Info-level disconnect reasons | **Done** |

### Phase 4 — Hardening polish (P2–P3)

| # | Work | Status |
|---|------|--------|
| **4.1** | Config: stall seconds ≥ 0; warn if no provider Ready | **Done** |
| **4.2** | Grok `prewarm` default true (+ mesh example) | **Done** |
| **4.3** | Debounce session meta persist (flush on create/claim/close) | **Done** |
| **4.4** | ACP FS jail | Deferred (single-operator threat model; D4) |
| **4.5** | CI `go test -race` on hot packages | **Done** |
| **4.6** | Docs hygiene: README 0012 link; `test_json.dart` removed | **Done** |
| **4.7** | Typed `providers.list_result` payload | **Done** |

### Phase 5 — Product (L)

| Track | Status |
|-------|--------|
| **D** Durable transcript | **Done** 2026-07-22 (`sessions/<id>/history.json`) |
| **E** Outbound relay | Design accepted ([0015](0015-mcrelay-transport-security.md)); implementation open |
| **F** Second provider / push | Backlog |

**Near-term 0009 A–C + D closed** (2026-07-22). Remaining product track is **E** (relay) or **F** backlog.

---

## 6. Suggested PR stack

| PR | Title | Phase |
|----|-------|-------|
| **PR1** | docs: 0012 action plan; fix(ws/auth/session): Phase 0+1 | 0+1 · shipped |
| **PR2** | fix(opencode/httpagent/session): Phase 2 provider fidelity | 2 · shipped |
| **PR3** | fix(certs/pair/history): Phase 3 edges | 3 · shipped |
| **PR4** | docs/CI polish Phase 4 | 4 · shipped |
| **PR5+** | Product D/E | 5 |

---

## 7. Verification gate (every PR)

```bash
go build ./... && go vet ./... && go test ./...
go test -race ./internal/ws/... ./internal/session/... ./internal/auth/... \
  ./internal/daemon/... ./internal/provider/acpagent/...
# when mobile touched:
cd apps/mobile && flutter analyze && flutter test
govulncheck ./...   # periodic
```

---

## 8. Definition of done

**Near-term (Phases 0–1):** No unlocked `deviceID` races; bounded async work; prompt/history cannot starve ping/cancel; sticky auth; auth files multi-process safe; shutdown cannot leave new sessions; race suite green.

**Medium-term (2–4):** OpenCode HTTP fidelity; cert/pair edges; ops-visible disconnects; docs match code.

**Product-complete:** Choose durable history and/or relay with ADR; missing relay is not a daemon bug while mesh is the intentional ship path.

---

## 9. Implementation log

| Date | Change |
|------|--------|
| 2026-07-22 | Plan accepted; Phases 0+1 implemented (WS snapshot/semaphore/async prompt+history/sticky auth; auth flock; session shutdown gate + tests). |
| 2026-07-22 | Phase 2: OpenCode model field, HTTP permission status, engine purge delete, ACP close waiters, `/reset` retry, httpagent stderr + tests. |
| 2026-07-22 | Phase 3: atomic cert pair, pair Take/Restore, persist Model, history paging, Info disconnect logs; protocol-v1 history section updated. |
| 2026-07-22 | Phase 4: stall validation, grok prewarm default, debounced session persist, CI race, docs hygiene, typed providers list. |
| 2026-07-22 | Style: gofmt + golint clean. 0009 A–C closed (mobile history honesty, multi-device empty-state, error code UX). |
| 2026-07-22 | Phase D durable transcript: `history.json` under session dir; History from disk when not live; seed on Create; tests. |

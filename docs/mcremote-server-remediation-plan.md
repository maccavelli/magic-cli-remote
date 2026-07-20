# mcremote Go server remediation plan

**Status:** Phases 0–5 implemented (uncommitted / in progress on branch)  
**Date:** 2026-07-20 (decisions recorded same day)  
**Source:** Deep-dive audit of the mcremote Go server (bugs, gaps, wiring, hardening, concurrency, Go 1.26.5)  
**Companion:** [hardening-implementation-plan.md](hardening-implementation-plan.md) (phases 1–6 already complete), [protocol-v1.md](protocol-v1.md), [0001-architecture-mcremote.md](0001-architecture-mcremote.md)

This plan turns the audit into **actionable, sequenced work**. It is remediation of shipped defects and reliability gaps — not a product redesign. Out of scope here: Flutter client deep work (except protocol notes for new error fields), outbound relay product build, new providers (Antigravity), and load-test benchmarks as gates (optional evidence only).

---

## Severity model (unchanged)

| P | Meaning |
|---|---|
| **P0** | Remotely reachable path to code execution, or a failure with no user recovery |
| **P1** | Correctness/robustness defect with a workaround, or a security control that only applies on reconnect |
| **P2** | Structural gap / incomplete wiring / reliability defect |
| **P3** | Backlog, doc drift, polish, low-likelihood edge |

Audit found **no new P0**. Highest items are **P1 lifecycle and fan-out correctness**.

---

## Decisions (locked)

| # | Decision | **Chosen** | Implementation implication |
|---|----------|------------|----------------------------|
| **R1** | How does CLI `pair revoke` / `prune` kick **live** WS sockets? | **(A)** Unix admin socket under `data_dir` | Serve listens on `admin.sock` (0600); CLI dials after store mutate. |
| **R2** | Provider process exit | **(A)** Auto-close | Remove map entry, persist `disconnected`, drop history; no commandable tombstone. |
| **R3** | `session.create` with existing live `session_id` | **(B)** Close-and-replace, **ensuring the result is still active** | Fully close prior live session, then Start + register new; return live Meta only after success. |
| **R4** | Multi-device session isolation | **(B)** Owner `device_id` + filter broadcast/list | In scope for this remediation (not deferred). |
| **R5** | Event drop / back-pressure | **(A)** then **(B)** | Never drop control events; then disconnect slow clients if still stuck. |
| **R6** | Default `providers.fake.enabled` | **(A)** `false` | Tests enable fake explicitly. |

### R3=B detail — close-and-replace must leave a live session

When `Create` is called with a `LocalSessionID` that already maps to a **live** entry:

1. **Close prior fully** under manager rules: `cancel`, `sess.Close(ctx)`, remove from map, persist `disconnected` (same as intentional close, not purge unless later delete). Count `Close` once on the old provider session.
2. **Start** the new provider session with the same local id (and any resume opts).
3. **Register** the new entry only after `Start` succeeds; returned `Meta` must have `Live: true`, matching id, and accept `Prompt` immediately.
4. If `Start` fails after step 1: return the start error; map has no live entry for that id (honest failure — client may retry). Log at error with session_id.
5. **Never** overwrite the map without step 1 (that was the leak bug B3).

When the id is absent or only a disk record exists: normal create (R2 means dead in-memory entries should not linger).

### R4=B detail — owner isolation

1. Stamp `OwnerDeviceID` (JSON `owner_device_id`) on create from the authenticated WS `c.deviceID`.
2. Persist on `session.Record` so list merge after restart still knows owner.
3. **Authorize** mutating ops (`prompt`, `cancel`, `close`, `delete`, `history`, `permission.respond`) only if `owner_device_id == c.deviceID` (or legacy empty owner — see migration).
4. **List:** return only sessions owned by this device (plus legacy empty-owner rows, optional claim).
5. **Broadcast:** deliver `event` pushes only to clients whose `deviceID` equals the session owner (events without session_id stay global only if any — today all session events have session_id).
6. **Legacy migration:** records / live sessions with empty `owner_device_id` remain visible and operable by any authed device until first successful mutating op or create path stamps an owner; document in protocol-v1. New creates always set owner.

### R5 detail — A then B

1. **A:** Control events never use non-blocking drop (block on session lifetime / dedicated queue).
2. **B:** After unlock-during-broadcast + write deadlines, if a client still cannot accept control traffic (write timeout / full outbound queue), **close that client** with a logged reason — do not stall the process.

---

## Phase overview

| Phase | Theme | Severity | Est. | Depends on |
|-------|--------|----------|------|------------|
| **0** | Guardrails (tests that fail on the bugs) | — | S | none |
| **1** | Session lifecycle + revocation kick + create replace | P1 | M | R1–R3, Phase 0 |
| **1b** | Multi-device owner isolation | P2 (now in-scope) | M | R4, Phase 1 |
| **2** | Event fan-out, control priority, slow-client disconnect | P1–P2 | M | R5, Phase 1 preferred |
| **3** | Auth store / pair codes / crypto hygiene | P2–P3 | S–M | none (parallel OK) |
| **4** | Daemon timeouts, process hygiene, limits | P2 | M | none (parallel OK) |
| **5** | Dependencies, docs, config defaults, polish | P2–P3 | S | R6 |
| **6** | Product follow-ons (relay, Antigravity, durable history) | product | L | separate goals |

**Parallelism:** Phases 3–5 can land after Phase 0 in parallel with 1/1b/2 if careful. Prefer **1 → 1b → 2** serial for fewer merge conflicts on `ws`/`session`.

**Verification gate (every phase):**

```bash
go build ./... && go vet ./... && go test ./...
go test -race ./internal/ws/... ./internal/session/... ./internal/provider/grok/... ./internal/auth/... ./internal/daemon/...
```

Optional: `govulncheck ./...` after Phase 5 dependency bumps.

---

# Phase 0 — Regression guardrails (S)

**Goal.** Commit tests that **fail on current main** for each P1 defect, then turn them green in later phases.

### 0.1 Dead-session prompt rejection / auto-close

| | |
|--|--|
| **Audit** | B2, B5 / R2=A |
| **File** | `internal/session/manager_test.go` or `manager_dead_test.go` |
| **Drive** | Real `Manager` + scripted/fake provider that emits `session_status=disconnected` (or closes after status) |
| **Assert** | After disconnect: entry not live (gone from live map per R2=A); `Prompt` / `Cancel` / `RespondPermission` return error |
| **Acceptance** | Fails on current code; passes after 1.2 |

### 0.2 Create close-and-replace (R3=B)

| | |
|--|--|
| **Audit** | B3 |
| **File** | `internal/session/manager_test.go` |
| **Drive** | Scripted provider that records `Close` calls and unique process tokens; `Create` twice with same `LocalSessionID` |
| **Assert** | First session’s `Close` called exactly once; second `Create` succeeds; returned Meta `Live==true` and same id; `Prompt` on id talks to **second** instance only (no orphan first process) |
| **Acceptance** | Fails on current overwrite-without-close; passes after 1.3 |

### 0.3 Disconnect-after-revoke

| | |
|--|--|
| **Audit** | B1 / G1 / R1=A |
| **File** | `internal/ws/server_test.go` + admin client unit |
| **Drive** | Authed WS; invoke `DisconnectDevice` / admin `disconnect_device`; assert connection closes |
| **Acceptance** | Passes after 1.1 |

### 0.4 Control-event non-drop

| | |
|--|--|
| **Audit** | B4 / R5=A |
| **File** | `internal/provider/grok/session_test.go` |
| **Drive** | Small event buffer seam; fill with chunks; emit `permission_resolved` / `session_status` |
| **Assert** | Control event delivered (not silently dropped) |
| **Acceptance** | Fails under current `select/default`; passes after 2.2 |

### 0.5 Owner isolation (R4=B) — can land with Phase 1b

| | |
|--|--|
| **File** | `internal/ws/server_test.go` / `session` tests |
| **Drive** | Two device tokens; device A creates session; device B list/prompt/broadcast |
| **Assert** | B does not see A’s session in list; B’s prompt fails authz; A receives events, B does not |
| **Acceptance** | Passes after 1b |

### 0.6 Phase 0 exit criteria

- [x] Tests 0.1–0.4 committed (0.5 may land with 1b).
- [x] `go test` on touched packages run.
- [x] Phase 0 may be tests-only, or merged with first fix PR to avoid red main.

---

# Phase 1 — Session lifecycle and revocation (P1) — **M**

**Goal.** Revoke denies transport; non-live sessions reject mutating ops; create replace leaves a live session without leaks.

### 1.1 Wire revoke → kick live sockets (R1=A)

| | |
|--|--|
| **Audit** | B1, G1 |
| **Code today** | `ws.Server.DisconnectDevice` has **zero callers**. CLI only mutates `auth.Store`. |

**Design (locked):**

1. Serve opens Unix domain socket `$data_dir/admin.sock` (mode `0600`, remove stale sock on start).
2. JSON line or single request: `{"op":"disconnect_device","device_id":"..."}`; optional `disconnect_devices` for prune batches; optional `ping`.
3. Handler → `ws.Server.DisconnectDevice` / multi.
4. CLI after `Revoke` / `Prune`: dial admin sock, send disconnect for each removed id. If sock missing: print non-fatal “revoked on disk; no live daemon to kick”.
5. Auth for admin sock: filesystem permissions only (same uid as daemon) — acceptable for single-operator host.

**Files:** `internal/ws/server.go`, `internal/daemon/daemon.go`, `internal/cli/pair.go`, new `internal/admin/` (path + client + server).

**Acceptance:**

- [x] Live authed WS closed within ~1s of `mcremote pair revoke <id>` while serve runs (admin + DisconnectDevice tests).
- [x] New auths with revoked token still fail (existing).
- [x] Race-clean on ws/daemon.
- [ ] protocol-v1 “denies transport access” accurate (doc pass Phase 5).

### 1.2 Auto-close on provider disconnect + reject non-live ops (R2=A)

| | |
|--|--|
| **Audit** | B2, B5 |
| **Code today** | Prompt/Cancel/RespondPermission only check map presence. |

**Change:**

1. `liveEntry(id)` requires present and not dead (during transition) / present after auto-close simply missing.
2. On `session_status=disconnected` in `pump`: invoke full close path (persist disconnected, `cancel`, `sess.Close`, delete map, end pump). Avoid double-close races with explicit `Close`.
3. Mutating APIs error if not found / not live (`session_not_live` or clear message).

**Acceptance:**

- [x] 0.1 green.
- [x] After process death simulation: no map entry; Prompt fails; no stuck pump.

### 1.3 Create: close-and-replace (R3=B)

| | |
|--|--|
| **Audit** | B3 |

**Change (locked):**

```text
if live entry with same LocalSessionID:
  close it fully (1.2 path / m.close)
Start provider with that id
if Start fails → return error (no live session)
register new entry; return Meta{Live: true, ...}
```

- Do **not** map-insert before old close completes.
- Prefer close-then-start ordering to avoid two processes for one id (even briefly). If Start is slow, id is briefly absent from live list — acceptable; document.
- **Fake provider:** honor `opts.LocalSessionID` (B8).

**Acceptance:**

- [x] 0.2 green: old Close once; new session live and promptable; no orphan.
- [x] Concurrent create same id: serialized under createMu — no double leak under race test.

### 1.4 Phase 1 exit criteria

- [x] B1, B2, B3, B5 addressed.
- [x] Full `go test ./...` + scoped race green.

---

# Phase 1b — Multi-device owner isolation (R4=B) — **M**

**Goal.** A paired device cannot list, drive, or receive events for another device’s sessions.

### 1b.1 Data model

| Field | Where |
|-------|--------|
| `owner_device_id` | `session.Meta`, `session.Record`, create path |

- `Manager.Create(ctx, providerID, opts, ownerDeviceID string)` or put owner on `provider.StartOptions` / new `session.CreateOptions`.
- WS `handleSessionCreate` passes `c.deviceID` (require authed when tokens required; `"dev"` when auth off).

### 1b.2 Authorization

| Op | Rule |
|----|------|
| create | stamp owner = caller |
| list | filter to owner ( + legacy empty ) |
| prompt / cancel / close / delete / history / permission.respond | owner match or legacy empty |
| broadcast event | only clients with `deviceID == owner` for that session_id |

Resolve owner from live entry, else from store record when applicable.

**Legacy empty owner:** any authed device may operate; optional “claim”: first mutator sets owner (prefer **claim on first mutating op** so two phones don’t fight forever). Document.

### 1b.3 Errors

Stable code e.g. `session_forbidden` when owner mismatch (distinct from `session_not_live`).

### 1b.4 Protocol / Flutter note

- `session.created` / list Meta gain `owner_device_id` (optional for old clients — omit empty).
- Flutter single-phone: no behavior change if only one device.
- Second device: empty list until it creates its own sessions.

### 1b.5 Tests

- [ ] 0.5 green.
- [ ] Race: two devices creating different sessions; cross-prompt fails.

### 1b.6 Phase 1b exit criteria

- [ ] G4 closed for the multi-device case.
- [ ] protocol-v1 updated.
- [ ] Suite + race green.

---

# Phase 2 — Event fan-out and back-pressure (P1–P2) — **M**

**Goal.** Slow clients cannot stall the daemon or drop permission/status events; stuck clients are disconnected (R5 A→B).

### 2.1 Unlock during broadcast; write deadlines (N1, H2)

1. Snapshot authed clients under `s.mu`, unlock.
2. `writeJSON` with timeout context (e.g. 5s).
3. On write error / timeout: close that conn (feeds R5=B).
4. With R4: snapshot only clients that should receive this session’s event.

**Acceptance:** slow writer does not block client register; race-clean.

### 2.2 Non-droppable control events (R5=A)

| Control (must deliver) | Best-effort |
|------------------------|-------------|
| `session_status`, `permission_request`, `permission_resolved`, `turn_complete`, `error`, `user_message` | `assistant_message_chunk`, `thought_chunk` (+ optional tool spam) |

Control: blocking send on session lifetime or priority queue — **never** `select/default` drop.  
Chunks: non-blocking drop or coalesce.

**Acceptance:** 0.4 green.

### 2.3 Slow-client disconnect safety valve (R5=B) — **required**, not optional

After 2.1–2.2:

- Per-client write timeout failure → close conn + log.
- If a bounded per-client outbound queue is introduced: **control** envelope that cannot be queued → close client (do not block all sessions forever).
- Prefer minimal path first: snapshot + timeout + close; add queue only if tests show need.

**Acceptance:**

- [ ] Test or documented integration: hung client is dropped; other clients still get events.

### 2.4 WebSocket read limit (N9)

`conn.SetReadLimit(1 << 20)` (or similar) after Accept so history/prompts fit.

**Acceptance:** ~500-event history test does not hit default 32KiB limit.

### 2.5 Phase 2 exit criteria

- [ ] B4, N1, H2, N9, R5 A+B addressed.
- [ ] Race green; no deadlocks.

---

# Phase 3 — Auth store, pair codes, crypto hygiene (P2–P3) — **S–M**

### 3.1 Debounce / cache `LastUsedAt` (H7, N6)

In-memory device index; flush LastUsedAt debounced; Create/Revoke/Prune always durable.

**Acceptance:** 100 Validates → few disk writes; race-clean.

### 3.2 Constant-time token hash compare (B7)

`subtle.ConstantTimeCompare` on hex hashes.

### 3.3 Pair-code limits (B6)

Remove or fix dead `Attempts` / `NoteFailedAttempt`; add **process-wide** pair.claim rate limit so new connections cannot reset `failedClaims` cheaply. Document in protocol-v1.

### 3.4 Phase 3 exit criteria

- [ ] H7, B6, B7 closed.
- [ ] Auth race-clean.

---

# Phase 4 — Daemon robustness, process hygiene, limits (P2) — **M**

### 4.1 HTTP server timeouts (H1)

Set `ReadHeaderTimeout`, consider `ReadTimeout` / `IdleTimeout` / `MaxHeaderBytes`. **Verify** base `WriteTimeout` does not kill long-lived WS; if it does, leave WriteTimeout 0 and rely on Phase 2 per-write deadlines.

### 4.2 Process group kill (H5)

`Setpgid: true` + kill process group on Grok/terminal teardown.

### 4.3 Grok Start timeout

`context.WithTimeout` around Initialize / NewSession / LoadSession; kill process on failure.

### 4.4 Soft limits (G5)

| Knob | Default | Effect |
|------|---------|--------|
| `limits.max_ws_clients` | 8 | Reject/close excess WS |
| `limits.max_live_sessions` | 16 | create fails clearly |

(Per-device limits optional later under R4.)

### 4.5 Grok stderr

slog/debug or bounded capture — not unbounded `os.Stderr` spam in prod.

### 4.6 Phase 4 exit criteria

- [ ] H1, H5, G5 addressed; WriteTimeout decision documented.
- [ ] Suite + race green.

---

# Phase 5 — Dependencies, docs, defaults, polish (P2–P3) — **S**

### 5.1 Bump `golang.org/x/net` (H6)

To ≥ fixed release for `GO-2026-5026`; `govulncheck ./...`.

### 5.2 Config defaults (R6=A, G7, G8)

- `Defaults()`: `providers.fake.enabled = false`.
- Example YAML: `require_client_key: true`; fake only with “dev/smoke” comment.
- `docs/config.md`: admin sock, limits, owner isolation, fake default.

### 5.3 Protocol docs

- Revoke live-kick, live-session rules, close-and-replace create, `owner_device_id`, `session_forbidden`, control-event delivery expectations, providers.list shape.

### 5.4 Optional: history ring copy-trim (N7); staticcheck SA4017 in terminal_test

### 5.5 Phase 5 exit criteria

- [ ] govulncheck clean (or residual documented).
- [ ] Docs match ship; empty-config defaults safe.

---

# Phase 6 — Still out of scope (product follow-ons)

| Item | Audit | Notes |
|------|-------|-------|
| Outbound relay | G2 | Own design + plan |
| Antigravity provider | G3 | New adapter |
| Durable transcript on disk | G9–G10 | Meta-only today |
| Headscale API | G11 | Docs/ops |
| Browser origin checks | H3 | When Flutter web |
| FS/terminal cwd jail | H4 | Security model change |

**R4 multi-device isolation is no longer Phase 6** — it is Phase **1b**.

---

## Suggested PR / Graphite stack

| PR | Title | Phase | Risk |
|----|-------|-------|------|
| **PR1** | test+fix(session): auto-close, live checks, close-and-replace create | 0 + 1.2–1.3 | Medium |
| **PR2** | feat(admin): revoke kicks live websocket clients | 1.1 | Medium (admin.sock 0600) |
| **PR3** | feat(session/ws): owner_device_id isolation | 1b | Medium — protocol + multi-device |
| **PR4** | fix(ws): broadcast snapshot, write deadlines, read limit, drop slow clients | 2.1, 2.3, 2.4 | Medium |
| **PR5** | fix(grok): control-event priority delivery | 2.2 | Medium |
| **PR6** | perf(auth): memory store, constant-time, pair rate limit | 3 | Low–medium |
| **PR7** | harden(daemon): timeouts, pgid, limits, start timeout | 4 | Medium |
| **PR8** | chore: x/net, fake default off, docs | 5 | Low |

---

## Work breakdown by package

| Package | Phases | Primary work |
|---------|--------|----------------|
| `internal/session` | 0, 1.2, 1.3, 1b, 4.4 | liveEntry, replace create, owner, limits |
| `internal/ws` | 0, 1.1, 1b, 2, 3.3, 4.4 | admin kick, owner filter, broadcast, limits |
| `internal/admin` (new) | 1.1 | sock protocol client/server |
| `internal/daemon` | 1.1, 4.1 | admin listen, http.Server |
| `internal/cli` | 1.1 | revoke/prune notify |
| `internal/provider/grok` | 0, 2.2, 4.2–4.5 | emit priority, pgid, timeout, stderr |
| `internal/provider/fake` | 1.3 | LocalSessionID |
| `internal/auth` | 3 | cache, subtle, pair limits |
| `internal/config` | 4.4, 5.2 | limits, fake default |
| `docs/*`, `configs/*` | 1b, 5 | protocol + config |
| `go.mod` | 5.1 | x/net |

---

## Test matrix (must drive shipped code)

| Behavior | Package |
|----------|---------|
| Auto-close + Prompt fails after disconnect | `session` |
| Close-and-replace: Close once, new live, Prompt works | `session` |
| DisconnectDevice / admin disconnect | `ws` / `admin` |
| CLI revoke kicks when serve up | integration or admin unit + CLI dial |
| Device A events not to device B | `ws` |
| Device B cannot prompt A’s session | `ws`/`session` |
| Control event not dropped | `grok` |
| Slow client disconnected; other clients OK | `ws` |
| Validate disk writes bounded | `auth` |
| Constant-time path | `auth` |
| Process group kill | `grok` |
| Max sessions / clients | `session`/`ws` |
| Start timeout | `grok` |
| Fake default off | `config` |
| x/net bump | module + govulncheck |

**No test theater:** call real `Manager` / `ws.New` / `Store` / admin handlers.

---

## Rollout and operability

1. **Admin socket:** path, 0600, CLI degrades if serve down.
2. **Error codes:** `session_not_live`, `session_forbidden`; create replace is success path (no `session_exists` unless we add optional strict mode later).
3. **Flutter:** tolerate new Meta field `owner_device_id`; single device unchanged; second device sees only its sessions.
4. **Deploy:** session meta gains optional `owner_device_id` (backward compatible JSON). No devices.json schema break. Admin sock is runtime-only under data_dir.

---

## Definition of done (remediation complete)

Phases **0–5** (including **1b**) complete when:

1. Phase exit checklists ticked.
2. Audit P1 B1–B4 fixed with committed regression tests.
3. R1–R6 implemented as chosen (including R4 owner isolation and R3 close-and-replace leaving a live session).
4. `go test ./...`, `go vet ./...`, scoped `-race` green on Go 1.26.5.
5. `govulncheck` clean for known reachable `x/net` idna issue (or residual documented).
6. `docs/protocol-v1.md` and `docs/config.md` match revoke kick, live rules, replace create, owner isolation, limits.

Phase **6** product items remain backlog.

---

## Review checklist

- [x] **R1=A** admin sock  
- [x] **R2=A** auto-close on process death  
- [x] **R3=B** close-and-replace, result still active  
- [x] **R4=B** owner isolation (Phase 1b, in scope)  
- [x] **R5=A then B** control priority + disconnect slow clients  
- [x] **R6=A** fake default off  
- [x] Phase 6 excludes multi-device (moved to 1b); relay/history still out  
- [x] Admin sock auth = filesystem uid / 0600 (single-operator host)  
- [ ] Limit defaults 8 WS / 16 sessions — confirm or override at implement time  

---

## Appendix A — Audit ID → phase map

| Audit ID | Summary | Phase |
|----------|---------|-------|
| B1 / G1 | Revoke does not kick live WS | 1.1 |
| B2 | Prompt on dead sessions | 1.2 |
| B3 | Create id collision leak | 1.3 (replace) |
| B4 | Event drops lose control events | 2.2 |
| B5 | Process death leaves session mapped | 1.2 (R2=A) |
| B6 | Pair Attempts dead code | 3.3 |
| B7 | Non-constant-time token compare | 3.2 |
| B8 | Fake ignores LocalSessionID | 1.3 |
| H1 | HTTP timeouts thin | 4.1 |
| H2 | No WS write deadline | 2.1 |
| H5 | Process group / stderr | 4.2, 4.5 |
| H6 | x/net vuln | 5.1 |
| H7 / N6 | Validate full-file rewrite | 3.1 |
| N1 | Broadcast holds global lock | 2.1 |
| N2 | No per-client queue | 2.3 if needed |
| N7 | History ring trim | 5.4 optional |
| N9 | WS 32KiB read limit | 2.4 |
| G4 | No per-device isolation | **1b** (R4=B) |
| G2, G3, G9–G11 | Relay / providers / durable history | Phase 6 |
| G5 | No session/ws limits | 4.4 |
| G6–G8 | Docs/defaults | 5.2–5.3 |

## Appendix B — Explicit “do not redo”

Already shipped (hardening plan phases 1–6); do not re-litigate unless regression:

- Tailscale bind sentinel + fail-closed  
- Authenticated `/v1/hello`, bare `/healthz`  
- Client-key SPKI allowlist default-on (D7)  
- TLS self-signed + letsencrypt DNS-01 + fallback pin  
- Pair codes, device token hashing  
- `permission_resolved`, plan events, session.history ring (functional)  
- `session.delete` vs `session.close`  

This remediation **builds on** that baseline; it does not replace it. R4=B extends the trust model beyond single-phone convenience while keeping client-key + token as the device identity.

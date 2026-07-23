# MADR 0009: Post-hardening action plan

- **Status**: Accepted (planning)
- **Date**: 2026-07-21
- **Deciders**: Project Owner
- **Context**: Hardening plan (phases 1–6) and server remediation (phases 0–5 / 1b) are **implemented in code**. `go test ./...`, Flutter tests, and `govulncheck` are green. This plan sequences **what remains** — residual reliability bugs, client UX gaps, doc drift, and product follow-ons — without re-doing shipped work.

**Companions**

| Doc | Role |
|-----|------|
| [hardening-implementation-plan.md](hardening-implementation-plan.md) | Front door, TLS, client identity, operability — **complete** |
| [mcremote-server-remediation-plan.md](mcremote-server-remediation-plan.md) | Lifecycle, admin sock, fan-out, auth, limits — **code complete**; some checkboxes stale |
| [0012-mcremote-daemon-assessment-action-plan.md](0012-mcremote-daemon-assessment-action-plan.md) | Post-audit residual concurrency/auth/provider work (Phases 0–4 shipped) |
| [protocol-v1.md](protocol-v1.md) | Wire contract (mostly current) |
| [0001-architecture-mcremote.md](0001-architecture-mcremote.md) | Relay-primary vision vs mesh-first ship |
| [0015-mcrelay-transport-security.md](0015-mcrelay-transport-security.md) | **Phase E design accepted** — mcrelay E2E TLS splice, join plane, hardening |
| [0016-mcrelay-audit-hardening.md](0016-mcrelay-audit-hardening.md) | Post-E0–E3 audit findings; P1–P6 capacity/backoff/limits/Origin/rate |

---

## Baseline (do not redo)

Shipped and regression-tested unless a new failure appears:

- Tailscale bind default, fail-closed; authenticated `/v1/hello`; bare `/healthz`
- Device tokens + client-key binding (default on); pair claim limits; constant-time hash compare; debounced `LastUsedAt`
- TLS self-signed pin + Let's Encrypt DNS-01; mobile CertPinner + client identity
- Session auto-close, live checks, close-and-replace create; owner isolation; admin.sock revoke kick
- Broadcast snapshot + write deadline + slow-client disconnect; WS read limit 1 MiB
- Grok control-event non-drop; process-group kill; soft limits (WS clients / live sessions)
- Fake provider default off; Flutter permission lifecycle, transcript reducer, history replay (live ring)

**Verification gate (every phase below):**

```bash
go build ./... && go vet ./... && go test ./...
go test -race ./internal/ws/... ./internal/session/... ./internal/provider/grok/... ./internal/auth/... ./internal/daemon/...
cd apps/mobile && flutter analyze && flutter test
# Android packaging (when touching mobile native/resources):
flutter build apk --debug
```

---

## Severity model

| P | Meaning |
|---|---------|
| **P0** | Remotely reachable code execution, or failure with no recovery |
| **P1** | Correctness/reliability defect users hit in normal use |
| **P2** | Structural gap, multi-device/edge path, or incomplete UX |
| **P3** | Docs, polish, low-likelihood, ops backlog |

No open **P0** from the 2026-07-21 assessment.

---

## Goals and non-goals

### Goals

1. Close residual reliability edges (history loss clarity/fix, permission waiter hygiene).
2. Align docs and remediation checklists with shipped code.
3. Make multi-device / error codes usable on the phone.
4. Sequence product follow-ons (durable history, relay, second provider) with explicit decisions.

### Non-goals (this plan)

- Redesign protocol v1 or replace ACP/Grok path.
- Re-open ADR 0004/0005 TLS or client-identity choices.
- Load-test gates or formal SLOs (optional evidence only).
- Browser/Flutter web or full FS jail (listed as later backlog).

---

## Decisions (locked unless re-opened)

| # | Topic | **Chosen** | Implication |
|---|--------|------------|-------------|
| **A1** | History durability | **(D)** Persist ring tail to disk (Phase D shipped 2026-07-22) | Was document+UX only; reopened for durable `history.json` |
| **A2** | Slow-client disconnect | **Keep** 5s write deadline + close (R5=B) | Only tune if reconnect storms observed; do not re-block broadcast |
| **A3** | Multi-device | **Server isolation stays**; phone gets explicit errors/empty-state | Phase C; no shared session drive yet |
| **A4** | Outbound relay | **Separate design track** after reliability polish | Phase E; design locked in [0015](0015-mcrelay-transport-security.md); mesh remains preferred when available |
| **A5** | Second provider (Antigravity) | **After** durable history or relay decision, not before polish | Phase F |

---

## Phase overview

| Phase | Theme | Sev | Size | Depends |
|-------|--------|-----|------|---------|
| **A** | Truth in docs + plan hygiene | P3 | S | none |
| **B** | Reliability edges (history UX, permission waiters, cleanup) | P1–P2 | S–M | none |
| **C** | Mobile multi-device + error UX | P2 | M | A helpful |
| **D** | Durable transcript (product) | P1 product | L | A1 reopen if disk |
| **E** | Outbound relay design + MVP | product | L | A4 |
| **F** | Second provider / push / ops backlog | product | L | D or E optional |

Prefer **A → B → C** serially. **D / E / F** are product tracks; pick one primary based on whether users need offline-mesh phones (E) or restart-safe chat (D).

---

# Phase A — Docs and plan hygiene (P3) — S

**Goal.** One source of truth so engineers do not re-fix shipped bugs.

### A.1 Close remediation plan checkboxes

| | |
|--|--|
| **File** | `docs/mcremote-server-remediation-plan.md` |
| **Change** | Mark phases 1b–5 exit criteria done where code + tests exist; note `govulncheck` clean; leave Phase 6 product items open |
| **Acceptance** | Status header says “phases 0–5 implemented”; remaining open items only Phase 6 / optional polish |

### A.2 Cross-link this plan

| | |
|--|--|
| **Files** | README “Docs” section (if present), hardening plan footer |
| **Change** | Point remaining work at this ADR |
| **Acceptance** | New contributor finds 0009 within one hop from README or hardening plan |

### A.3 Remove stray experiment file

| | |
|--|--|
| **File** | repo root `test_json.dart` |
| **Change** | Delete (or move under `scripts/` if still useful) |
| **Acceptance** | No orphan dart at repo root |

### A.4 Phase A exit

- [x] Remediation plan status matches code
- [x] This plan linked from README or hardening companion list
- [x] Stray `test_json.dart` gone (removed 2026-07-22; also see [0012](0012-mcremote-daemon-assessment-action-plan.md))

---

# Phase B — Reliability edges (P1–P2) — S–M

**Goal.** Fewer silent failures under process death, load, and reconnect — without a full product redesign.

### B.1 History loss: explicit contract + phone UX (A1)

| | |
|--|--|
| **Audit** | Live-only ring; `History` empty after close/restart |
| **Server** | Optional: log at Info when history dropped on close; ensure `session.history` empty list (already) is documented in protocol-v1 as best-effort live buffer |
| **Mobile** | When opening a session with empty history after reconnect and `live: false` or after daemon restart, show a short banner: “Earlier messages aren’t on this host after restart” (wording flexible) |
| **Tests** | Flutter widget/unit: banner condition; server doc-only OK |
| **Acceptance** | User is never surprised into thinking transcript vanished due to a client bug |

### B.2 Permission waiter delivery on close

| | |
|--|--|
| **Audit** | Grok session close uses `select/default` when cancelling pending permission channels |
| **File** | `internal/provider/grok/session.go` |
| **Change** | Prefer blocking send with short timeout, or close waiter channels so `Prompt`/respond unblocks with cancelled; never leave a goroutine blocked forever |
| **Tests** | Unit: pending permission + Close → waiter returns cancelled/error promptly |
| **Acceptance** | No hung permission path after Close under full buffer |

### B.3 Reconnect storm observability (optional if B only)

| | |
|--|--|
| **Audit** | Broadcast write failure closes client (correct); hard to debug if flapping |
| **Change** | Ensure disconnect reason is logged at Info once per close (device_id + reason: slow client / revoked / read deadline); avoid Debug-only for production diagnosis |
| **Acceptance** | `journalctl --user -u mcremote` shows why a phone dropped |

### B.4 Phase B exit

- [x] B.1 UX or documented protocol note shipped (protocol-v1 live-only ring; mobile empty-chat note + non-live/reconnect banner)
- [x] B.2 green with test (acpagent Close unblocks waiters; covered in 0012 Phase 2.5)
- [x] B.3 disconnect reasons at Info (0012 Phase 3.6)
- [x] Suite + race green on touched packages

---

# Phase C — Mobile multi-device and error UX (P2) — M

**Goal.** Second paired device behaves honestly; protocol error codes surface as actions, not generic “failed”.

### C.1 Map server error codes in the client

| Code | Client behavior |
|------|-----------------|
| `session_forbidden` | Snackbar/dialog: “This session belongs to another device”; do not retry prompt forever |
| `session_not_live` | Offer “Start again” / create-replace path already used for dead rows |
| `invalid_token` / key failures | Existing permanent disconnect paths (keep) |

| | |
|--|--|
| **Files** | `apps/mobile/lib/data/ws/mcremote_client.dart`, chat/sessions screens, `mc_exception.dart` |
| **Tests** | Unit parse of error envelopes; optional widget for forbidden |
| **Acceptance** | Forbidden never looks like network blip |

### C.2 Empty session list when another device owns everything

| | |
|--|--|
| **UI** | Sessions screen empty state: “No sessions on this device. Create one to start.” (not “daemon empty” if connected) |
| **Acceptance** | Second phone after first phone created sessions is not a confusing blank without copy |

### C.3 Tolerate `owner_device_id` on Meta

| | |
|--|--|
| **Models** | Ensure Dart models ignore/pass through `owner_device_id` (display optional, debug only) |
| **Acceptance** | No decode failures if field present |

### C.4 Phase C exit

- [x] Forbidden / not-live handling covered by tests (`friendlyOpError` + snackbar recovery)
- [x] Empty-state copy for multi-device
- [x] `owner_device_id` tolerated on `SessionMeta`
- [x] `flutter test` green on touched packages (analyze when packaging)

---

# Phase D — Durable transcript (product, P1 when chosen) — L

**Goal.** Chat survives daemon restart and session close within a retention policy.

**Revisit A1:** choosing Phase D means changing A1 from “document only” to “persist”. **Done 2026-07-22.**

### D.1 Design decisions (recorded)

| Question | **Chosen** |
|----------|------------|
| Storage | Atomic `history.json` snapshot per session under `data_dir/sessions/<id>/` (same layout as `meta.json`; no SQLite) |
| Scope | **All** ring event kinds (chunks included) so mobile replay stays faithful |
| Retention | Same cap as live ring: **500 events/session**, oldest drop; delete purges dir |
| Privacy | Same uid as daemon; **0600** files; no off-host sync |
| Protocol | `session.history` after restart returns disk slice for listed ids; wire paging + byte budget unchanged |

### D.2 Implementation (shipped)

1. Pump schedules debounced `SaveHistory`; close / CloseAll flush immediately.
2. `History` / `HistoryPage` use live ring when present, else `LoadHistory`.
3. Create seeds ring + seq from disk so resume continues the transcript.
4. `session.delete` → `store.Delete` removes history with the session dir.
5. Mobile honesty banner only when history is still empty; empty-state copy notes host storage.

### D.3 Phase D exit

- [x] D.1 choices recorded in this file
- [x] Restart path: new Manager + same store → `History` non-empty
- [x] Tests: pump → close → new manager → history; purge on delete; cap
- [x] Disk budget = per-session 500-event cap under synthetic load

---

# Phase E — Outbound relay (product) — L

**Goal.** Phone reaches daemon without being on the same Headscale/Tailscale mesh (architecture primary in 0001).

**Design ADR:** [0015-mcrelay-transport-security.md](0015-mcrelay-transport-security.md) — **Accepted 2026-07-23**.

Locked decisions (see 0015 for full text):

| # | Choice |
|---|--------|
| Trust | Opaque join + **end-to-end TLS to mcremote** (relay does not see protocol plaintext) |
| Auth | Device token + client key still validated **only on mcremote** (ADR 0005 preserved) |
| Discovery | Pair URI gains `relay` + `hid`; registration secret **never** on phone |
| Ops | Self-hosted `mcrelay`; **multi-host** OK; no public multi-tenant SaaS in v1 |
| Fallback | Prefer mesh/direct when reachable; relay is fallback |
| Parity | **Full** protocol-v1 surface — not a reduced API |

### E.1 Design track

- [x] ADR accepted ([0015](0015-mcrelay-transport-security.md))

### E.2 MVP scope (implementation — 0015 phases E0–E4)

- [x] **E0** Pair URI `relay`/`hid` + mobile `ConnectionPath` stubs
- [x] **E1** `cmd/mcrelay` register / join / opaque splice / multi-host allowlist / limits
- [x] **E2** Daemon dials out, registers, tunnels bridged to local listener; pair URI emits relay
- [x] **E3** Phone outer TLS → join → **inner** TLS/WSS to mcremote via loopback bridge
- [ ] **E4** Ops docs (systemd, LE, secret rotation)

### E.3 Phase E exit

- [x] ADR accepted
- [x] E0+E1 code + unit tests (register/join/splice; pair URI round-trip)
- [ ] Smoke: phone off-mesh can auth + create + prompt + permission (+ history, `models.list`)
- [ ] Security review: relay cannot mint sessions without device credentials; evil splice cannot inject frames
- [ ] Mesh-direct still works with relay disabled
- [ ] Host revoke tears down live relay path

---

# Phase F — Backlog (product / ops) — various

| Item | Pri | Notes |
|------|-----|--------|
| Antigravity / second provider | Medium | Adapter behind `provider.Provider`; degraded path OK |
| Push (FCM) for permission_request | Medium | Daemon or side watcher → push; phone deep link to chat |
| Headscale API automation | Low | Docs/ops; blocked by upstream preferences (0008) |
| Tailnet lock | Low | Deferred (0007) |
| Browser origin checks | Low | Only if Flutter web |
| Tool cwd / FS jail | Security model | Explicit product decision; not a drive-by |
| Load / soak tests | Optional | Evidence for slow-client and history caps |

Do not start F items that fight D/E resource focus unless a concrete user need lands.

---

## Suggested PR / work stack

| PR | Title | Phase | Risk |
|----|-------|-------|------|
| **PR1** | docs: mark remediation complete; add 0009; drop `test_json.dart` | A | Low |
| **PR2** | fix(grok): unblock permission waiters on close | B.2 | Medium |
| **PR3** | feat(mobile): history-empty / restart messaging | B.1 | Low |
| **PR4** | feat(mobile): session_forbidden + empty-state multi-device | C | Medium |
| **PR5** | feat(session): durable transcript (after D.1) | D | High |
| **PR6** | design+feat: outbound relay MVP | E | High |

---

## Work breakdown by package

| Area | Phases | Work |
|------|--------|------|
| `docs/*`, README | A, D.1, E.1 | Plan truth, ADRs |
| `internal/provider/grok` | B.2 | Permission waiter close |
| `internal/ws` / logging | B.3 | Disconnect reason visibility |
| `internal/session` + store | D | Durable history |
| `apps/mobile` | B.1, C | UX, error codes, empty states |
| New `internal/relay` (TBD) | E | Outbound path |
| `internal/provider/*` | F | Second adapter |

---

## Definition of done (this plan)

### Near-term complete (A–C)

1. Docs match ship; remediation plan not misleading.
2. Permission close path cannot hang.
3. History loss is honest in product copy (or fixed by D).
4. Mobile handles `session_forbidden` / multi-device empty list.
5. Full verification gate green.

### Product complete (D or E — pick primary)

- **D:** Restart-safe history under stated retention. **Shipped 2026-07-22.**
- **E:** Off-mesh control path with credentials end-to-end. *(open)*

Phase **F** remains backlog until prioritized.

---

## Review checklist

- [x] No P0 open from 2026-07-21 assessment
- [x] Does not re-open hardening / remediation shipped items
- [x] A1 history = durable disk ring (Phase D shipped); was document+UX interim
- [x] A4 relay is separate design track ([0015](0015-mcrelay-transport-security.md) accepted)
- [x] Verification gate includes Go race packages + Flutter
- [x] Phase **D** chosen and shipped (2026-07-22); **E** still owner-priority

---

## Appendix — Assessment map → phase

| Finding | Phase |
|---------|--------|
| Remediation checkboxes / plan drift | A |
| Stray `test_json.dart` | A |
| Live-only history / restart empty transcript | B.1 → D |
| Permission `select/default` on close | B.2 |
| Slow-client disconnect hard to diagnose | B.3 |
| Flutter thin on `session_forbidden` / 2nd device | C |
| Outbound relay not built | E |
| Antigravity, push, Headscale API, jail | F |
| Stolen token+key on mesh | Accepted threat model (hardening); not a bug |

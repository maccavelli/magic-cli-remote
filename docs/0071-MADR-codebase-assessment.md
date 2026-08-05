# MADR 0071: Codebase assessment — bugs, gaps, hardening, robustness, performance

<!-- markdownlint-disable MD013 MD024 MD060 -->

- **Status**: Findings accepted; remediation under
  [0071-PLAN](0071-PLAN-codebase-assessment-remediation.md) (in progress).
- **Date**: 2026-08-05
- **Deciders**: Project Owner (priority); Implementer (verification)
- **Implementation plan**:
  [0071-PLAN-codebase-assessment-remediation.md](0071-PLAN-codebase-assessment-remediation.md)
- **Scope**: Full tree after 0070 remediation: `internal/*`, `apps/mobile`,
  build/install, docs status. Grounded against HEAD at `d4952e8`
  (2026-08-05). Does **not** re-open closed 0068/0069 software phases
  unless a finding falsifies their acceptance tests. **Does not cover
  update automation** — that work is governed by
  [0065-MADR](0065-MADR-update-automation.md) /
  [0065-PLAN](0065-PLAN-update-automation.md) and is unfinished by design.
- **Related**:
  [0070-MADR](0070-MADR-deep-dive-debugging-pass.md) /
  [0070-PLAN](0070-PLAN-deep-dive-remediation.md) (prior pass; many items
  closed), [0065](0065-MADR-update-automation.md) (update automation —
  out of scope here), [0048](0048-MADR-codex-sandbox-namespace.md)
  (landed via 0070 P3),
  [ops-hardware-validation.md](ops-hardware-validation.md)
- **Method**: marker sweep; review of freshly landed 0070 code paths
  (excluding 0065 surfaces); incomplete-wiring checks for non-update
  features; sandbox probe behaviour; empty-catch density; perf hotspots;
  adversarial “looks unfinished but intentional” filter.

---

## 0. Executive summary

0070 fixed real resync and resume-mint bugs and landed 0048 sandbox
recovery. This pass focuses on **remaining non-update debt**: sandbox
probe platform behaviour, template drift, transport hygiene, and residual
hardware/product backlog.

| Tier | Focus |
| --- | --- |
| **A — Bugs / correctness** | Codex sandbox probe blocking + Linux-shaped notices on all GOOS |
| **B — Incomplete wiring** | `sandbox_broken_policy` not in setup template |
| **C — Hardening / robustness** | Empty-catch density; resume token map growth |
| **D — Performance / stability** | 8s inline sandbox probe on engine start |
| **E — Residual hardware / product** | Part F, APNs, 0029/0039 platform debt |

**Out of scope for this MADR (0065 owns them):** phone update UI/install
channel, host `update` CLI polish (`os.Exit`, layering, CLI duplication,
self-update, swap/service-control tests, APK hashing, doctor/README for
updates, GitHub rate limits for releases). Do not re-plan those here.

**Recommendation:** prioritise sandbox probe GOOS/latency + template key
before trusting 0048 notices on macOS; keep 0065 as the single backlog
for update automation.

---

## 1. What is *not* a finding (checked and rejected)

| Candidate | Why rejected |
| --- | --- |
| SessionSynchronizer double-list | Fixed in 0070 P0 — single `listSessionSnapshot` remains |
| Empty resume token on RNG failure | Fixed in 0070 P1 — mint returns `ok`, fields omitted |
| Empty catch in `relay_transport` teardown | Best-effort close; lifecycle tests cover invariants |
| No APNs / background push | 0067 D3 non-goal |
| `debugserve` without unit tests | Thin opt-in debug surface; acceptable |
| 0063 “not implemented” docs | Flipped in 0070 P2 |
| Incomplete update automation (phone install, CLI exit codes, swap tests, layering, APK hash, self-update, etc.) | **In progress under [0065](0065-MADR-update-automation.md)** — not open 0071 findings |

---

## 2. Findings

Severity:

- **S0** — security / data loss / common-path break
- **S1** — user-visible wrong behaviour or silent incomplete product path
- **S2** — incomplete wiring / product gap with code half-present
- **S3** — harden, perf, maintainability, docs/process

---

### F1 — Codex sandbox probe blocks every engine start; Linux-shaped on all GOOS (S1)

**Where:** `internal/provider/codex/provider.go` (~post-initialize
`runSandboxProbe` with 8s timeout); `sandbox_health.go` `probeSandboxHealth`

**What:**

1. Probe runs **inline** on the engine start critical path (up to ~8s)
   before `ensureEngine` returns — first session / prewarm pays full cost.
2. Probe always runs `codex sandbox` + `bash`, with failure copy and
   notices written for **Linux userns/bwrap**. On macOS, failures are
   Seatbelt/TCC-shaped; `classifySandboxError` may label them
   `probe_failed` or misfit markers, then emit notices that tell operators
   to fix AppArmor sysctls.
3. Probe requires `bash` on PATH — Alpine-like hosts can fail closed into
   `probe_failed` even when sandbox policy is fine.

**Why it matters:** macOS is a primary host; 0069 already addresses FDA.
0048 probe must not slow healthy macOS starts or cry wolf with Linux ops
copy.

**Suggested fix:**

- Skip or short-circuit probe when `runtime.GOOS != "linux"` (or when
  `sandbox_broken_policy` is explicitly off / `skip`).
- Run probe in background after engine ready; session create waits with
  short timeout or treats `unknown` as non-notice until first result.
- Platform-specific notice copy (Linux userns vs macOS Seatbelt/FDA → 0069).
- Prefer `/bin/sh` or codex-only probe args if bash is optional.

---

### F2 — Setup template missing `sandbox_broken_policy` (S2)

**Where:** `internal/cli/service/defaults_mcremote.yaml` has
`allow_full_access` but not `sandbox_broken_policy`; examples only comment
the key in `configs/*.yaml`.

**What:** Same class as 0069 template drift. New installs won’t document
the escape hatch next to codex keys.

**Suggested fix:** add commented key + extend `template_parity_test` if
the key is represented in the example as a structural peer — or document
as operator-only advanced key next to the 0048 config.md section.

---

### F3 — Empty-catch density still high (S3)

**Where:** ~32 bare `catch (_) {}` in `apps/mobile/lib` production code
after 0070 P5 (mostly teardown + secondary UI).

**What:** Convention landed in `docs/standards/mobile/dart.md`. Primary
create/delete paths largely surface errors; residual discards remain in
settings/chat/sessions (prefs, clipboard, optional meta).

**Suggested fix:** second audit pass with `debugPrint` floor on remaining
non-teardown sites; optional custom lint later. Not a mass rewrite.

---

### F4 — Resume token map grows without eviction (S3)

**Where:** `internal/ws/resume.go` `byDevice map` — entries only replaced
per device, never removed on disconnect/expiry.

**What:** For normal device counts (≪ MaxWSClients) fine. Long-lived
daemon + many historical device IDs (pair churn) retains expired tokens
until overwrite.

**Suggested fix:** opportunistic purge of expired entries on `issue`, or
cap map size LRU.

---

### F5 — Residual hardware / product backlog (S3, carried)

Unchanged from 0070 unless noted. **Update automation residuals live in
0065, not here.**

| Item | Notes |
| --- | --- |
| iOS Part F / 0068 G1 | ⏸ no device |
| U7 `tls_resumed` live | needs codesigning identity |
| 0069 G1 / U8 | FDA + codesign walkthrough |
| APNs / background attention | 0067 D3 follow-up MADR |
| 0029 provider platform | structural debt |
| 0039 residual slices | mid-session model switch, MCP status UI |

---

### F6 — Hardening opportunities (bundled S3, non-update)

| Opportunity | Detail |
| --- | --- |
| **Stream coalescing / history** | Already strong (800 ring, chunkbuf); next win is avoid full history walk on epoch force when resume already confirmed empty/unchanged sessions |
| **Mobile catch → metrics** | Optional debug-only counter of discarded errors for field diagnosis |
| **Sandbox probe without bash** | See F1 — hosts without bash should not become false `probe_failed` |

---

## 3. Cross-cutting themes

1. **Platform awareness** — Linux-only probes and copy must not run or
   speak as if universal (F1).
2. **Template levers** — missing setup-template keys recreate the 0069
   “hidden config” operator pain (F2).
3. **Transport hygiene is mostly mature** — remaining wins are small
   (resume map GC, catch floor), not architectural rewrites.
4. **Hardware still dominates true confidence** for reconnect and iOS (F5).
5. **Update automation is a separate program of work** — track only under
   0065.

---

## 4. Suggested prioritisation (for a follow-up PLAN)

| Priority | Items | Findings |
| --- | --- | --- |
| **P0** | Sandbox probe GOOS gate + non-blocking or skip macOS; template key | F1, F2 |
| **P1** | Resume map GC; empty-catch floor on non-teardown sites | F3, F4 |
| **P2** | History/epoch skip polish; optional discard metrics | F6 |
| **P3** | Hardware backlog when devices exist | F5 |

Update-automation completion stays on **0065-PLAN**, not this table.

---

## 5. Verification performed this pass

| Check | Result |
| --- | --- |
| Marker sweep (TODO/FIXME/Unimplemented in prod) | No critical unimplemented production stubs |
| 0070 F1/F3 closure | Confirmed single list snapshot; resume mint `ok` path present |
| Codex probe path read | Inline 8s after initialize confirmed |
| Empty catch count (`apps/mobile/lib`) | ~32 bare `catch (_) {}` |
| Untested packages | `internal/debugserve` only (among non-update packages) |
| Update automation surfaces | **Not filed** — deferred to 0065 |

---

## 6. Open questions for the owner

1. Should sandbox probe be **Linux-only** by default, or also attempt macOS
   Seatbelt workspace-write with 0069-linked copy?
2. Prefer **background probe** (sessions start at `unknown`) vs **short
   inline timeout** (e.g. 1–2s) on Linux?

---

## 7. Decision record (this MADR)

| ID | Decision |
| --- | --- |
| D1 | Record findings only; no code in this MADR’s landing |
| D2 | Prefer fixing F1/F2 before trusting 0048 notices on non-Linux hosts |
| D3 | A follow-up **0071-PLAN** may own non-update sequencing only |
| D4 | **All update-automation work stays under 0065** — incomplete units are not 0071 findings |
| D5 | Do not reopen 0068 transport software without a falsified acceptance test |

---

## 8. Changelog

| Date | Note |
| --- | --- |
| 2026-08-05 | Initial assessment post-0070 |
| 2026-08-05 | Removed all auto-update findings (formerly F1, F3–F8, F11, F13 and related summary/priority items); those remain governed by MADR/PLAN 0065. Renumbered remaining findings F1–F6 |

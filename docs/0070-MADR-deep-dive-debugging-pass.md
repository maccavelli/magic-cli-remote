# MADR 0070: Deep-dive debugging pass — bugs, gaps, unfinished work

<!-- markdownlint-disable MD013 MD024 MD060 -->

- **Status**: Findings recorded; remediation **software-complete** for
  owned phases under [0070-PLAN](0070-PLAN-deep-dive-remediation.md)
  (P0–P6 + Track U host path). Phone install UI (0065 P4–P5) and hardware
  gates remain residual.
- **Date**: 2026-08-05
- **Deciders**: Project Owner (priority / sequencing); Implementer
  (verification of each finding against the tree)
- **Implementation plan**:
  [0070-PLAN-deep-dive-remediation.md](0070-PLAN-deep-dive-remediation.md)
  (P0–P6 owned work; P3 dispatches 0048; Track U dispatches 0065;
  residual backlog explicit)
- **Scope**: Full tree — Go daemon/relay (`internal/*`, `cmd/*`), Flutter
  client (`apps/mobile`), build/install scripts, and `docs/` status
  hygiene relative to shipped code. Grounded against HEAD on 2026-08-05
  after green `make test` and mobile trio (746 passed, 3 skipped).
- **Related**:
  [0068](0068-MADR-protocol-v2-reconnect-resilient-transport.md) /
  [0069](0069-MADR-macos-permissions-and-sandbox-parity.md) (recent
  closes), [0067](0067-MADR-ios-port.md) A1, [0065](0065-MADR-update-automation.md)
  (unbuilt product), [0063](0063-MADR-connection-liveness-truth.md)
  (status drift), [0048](0048-MADR-codex-sandbox-namespace.md)
  (unfixed host-class bug),
  [ops-hardware-validation.md](ops-hardware-validation.md)
- **Method**: incompleteness-marker sweep; open/proposed MADR+PLAN audit;
  residual-risk and empty-catch review in transport/session paths;
  provider capability matrix; hardware-gate inventory; adversarial
  re-check of “looks unfinished” items that are actually by design.

---

## 0. Executive summary

The recent transport and permissions work (0067–0069) is **software-complete**
and well tested. This pass did **not** find a critical correctness hole in
the new v2 stack under unit coverage. What it did find is a layered debt
picture:

| Tier | Count (approx.) | Nature |
| --- | --- | --- |
| **A — Real bugs / soft bugs in shipped paths** | 3 | Double-list resync with silent second failure; stale protocol-obligation comment vs 0068 P1; empty resume token on `rand.Read` failure |
| **B — Incomplete features / proposed-not-built** | 5+ | Update automation (0065), codex sandbox-namespace recovery (0048), Grok ACP parity remainder (0039), provider platform extraction (0029), thinking levels on goose/opencode |
| **C — Hardware / live gates still open** | Many rows | iOS Part F, 0062 G7 remainder, 0068 G1/U7, 0069 G1/U8, 0063 Part A completeness |
| **D — Documentation status drift** | Several MADRs | Plans/MADRs still say “not implemented” or “In progress” after code landed (0063 worst; 0052, 0029, pre-fix 0068/0069) |
| **E — Deferred test harnesses** | 2 | park→resume single-join integration; `tls_resumed` live |

**Recommendation:** remediate via
[0070-PLAN-deep-dive-remediation.md](0070-PLAN-deep-dive-remediation.md).
Do not re-open closed 0068/0069 software phases unless a finding below
contradicts their acceptance tests.

---

## 1. What is *not* a bug (checked and rejected)

These look like unfinished work on a grepping pass; they are intentional or
already fixed.

| Candidate | Why it is not a finding |
| --- | --- |
| Empty `catch (_)` in `relay_transport.dart` / socket teardown | Best-effort close of already-dead peers; races are asserted in lifecycle tests |
| No `internal/update` package | Expected — 0065 is proposed, not abandoned mid-impl |
| iOS without APNs / background push | 0067 D3 non-goal; entitlements comment documents absence |
| No relay-side connection replacement | 0068 Q5 answered: joins carry no device identity |
| Resume tokens memory-only | By design; epoch path covers daemon restart |
| `internal/debugserve` no unit tests | Thin pprof wiring; ops-verified under `make debug`; acceptable gap |
| Goose `auto` dangerous + default `approve` | 0069 D3 completed |
| `protocol.Version` alias still `V1` | Intentional pre-negotiation framing |

---

## 2. Findings

Severity scale:

- **S0** — correctness / security / data loss in a common path
- **S1** — user-visible failure on a real host class or silent incomplete recovery
- **S2** — missing product surface with an accepted/proposed plan
- **S3** — test / hardware / docs hygiene that hides true readiness

---

### F1 — SessionSynchronizer double-list + silent second failure (S1)

**Where:** `apps/mobile/lib/state/session_synchronizer.dart` (~lines 103–127)

**What:** `_resyncBody` calls `listSessionSnapshot()` **twice**. The first
call records failures (`failed = true` + debugPrint). The second call, used
to enlarge `ids` with host-listed sessions, swallows all errors:

```dart
} catch (_) {}
```

**Why it matters:** If the first list succeeds and the second fails (or the
inverse pattern under flaky connectivity), the resync can run with a
**partial** session id set and **not** schedule a retry when only the second
call fails. That re-opens a weaker form of the 0056/0068 “interrupted resync
stays broken until the next connect edge” class of bug — especially on iOS
where the next edge may be hours later.

**Also:** the double RPC is pure cost on every connected edge (including the
common app-switch case that P3/P4 tried to make cheap).

**Suggested fix:** single snapshot; share `seqs` / `sessions` / `epoch`;
always set `failed` on list errors; unit test that second-phase failure arms
retry.

**Verification:** extend `session_synchronizer_test.dart` with a client that
fails on the Nth `listSessionSnapshot`.

---

### F2 — Client “app ping is the only deadline reset” claim is stale after 0068 P1 (S3 → can become S1 if mis-optimised)

**Where:** `apps/mobile/lib/data/ws/link_health.dart` (comments on
`kAppPingPeriod`, ~lines 18–28)

**What:** Comments assert that transport ping/pong never resets the daemon
read deadline, so the **app-level ping is the only** thing keeping the host
session alive.

**Current server fact (v2):** `internal/ws/liveness.go` — `pingLoop` +
`deadlineWatchdog` treat completed transport pongs as horizon extensions
(`lastPong`). `caps.ws_ping_resets_deadline: true` is advertised on every v2
`auth_ok`.

**v1 fact:** still true — no watchdog pong path; app data frames (including
`ping` requests) reset the per-read timeout.

**Why it matters:** A battery or traffic optimisation that “skips app ping
when transport pongs are enough” would be **correct only against a v2
daemon** and would **kill long streams against a v1 daemon** (and still
damage 0063 UI freshness on any version). The comment no longer matches the
negotiated world; the unconditional app ping remains correct, but the
*rationale* must cite both versions.

**Suggested fix:** rewrite the constant comment to: unconditional app ping
for (1) v1 deadline reset, (2) UI `lastVerifiedAt`, (3) version skew; note
v2 also extends via transport pong. Optionally assert
`caps.ws_ping_resets_deadline` when negotiated v2.

---

### F3 — Resume token mint fails open to empty string (S2, rare)

**Where:** `internal/ws/resume.go` `issue()` — on `rand.Read` failure returns
`("", window)` without error.

**What:** Auth still succeeds; `ResumeToken` may be empty. Client will never
successfully resume (falls through to full reconcile) — safe, but silent.

**Why it matters:** Extremely rare on modern kernels; if it ever happens,
there is no daemon log line distinguishing “resume disabled” from “RNG
failed”.

**Suggested fix:** log at warn once per failure; omit `resume_token` /
`caps.resume` rather than sending empty; or fail closed only the resume
fields.

---

### F4 — Codex sandbox user-namespace failure still unmitigated (S1 on affected Linux hosts)

**Where:** MADR/PLAN [0048](0048-MADR-codex-sandbox-namespace.md) — **Status:
Proposed**. No `sandbox_broken_policy`, no health probe, no session notice
path in `internal/provider/codex` (grep: only copy mentioning bwrap/Seatbelt
in turn-error guidance from 0069).

**What:** On Ubuntu/Debian hosts where unprivileged user namespaces are
blocked, codex `auto` / `workspace-write` **looks** armed but every sandboxed
write fails. Operators must already know to enable `allow_full_access` and
switch modes — the 0048 problem statement remains live.

**Why it matters:** User-visible “agent loops on failed edits” with a green
mode chip. Distinct from 0069 macOS FDA/Seatbelt (which *does* now classify
and guide).

**Suggested fix:** implement 0048 plan phases (probe + notice + optional
policy seed). Cross-link from 0069 so operators do not confuse the two
denial classes.

---

### F5 — Update automation entirely unbuilt (S2 product)

**Where:** [0065-MADR](0065-MADR-update-automation.md) /
[0065-PLAN](0065-PLAN-update-automation.md) — **Proposed — not implemented**.

**Tree facts:**

- No `internal/update` package
- No `update` CLI subcommand on `mcremote` / `mcrelay`
- No phone update checker / `FileProvider` / install permission path
- Phone-stage gate **unblocked** (ops E1/E2 passed 2026-08-03)

**Why it matters:** Install/upgrade remains a manual ops procedure. 0066
made upgrades *safe*; 0065 would make them *automatic*. Highest-value
unbuilt plan once hardware confidence is enough.

---

### F6 — Provider platform canonicalization incomplete (S2 structural)

**Where:** [0029-MADR](0029-MADR-provider-platform-canonicalization.md) —
**Accepted — implementation in progress**. Plan notes Phases 0 and low-risk
Phase 1 portions only.

**Tree facts:** Four real providers remain dialect-heavy
(`acpagent`, `acphttp`, `httpagent`+opencode, `codex`) with large optional
interface surfaces (`ThinkingSession`, `ForkSession`, `UndoSession`,
`MCPStatusSession`, …). Capability is honest via interface absence, but
cross-provider UX is uneven by construction.

**Partial matrix (thinking):**

| Provider | Mid-session thinking |
| --- | --- |
| codex | yes (`SetThinkingLevel`) |
| grok (acpagent) | advertised; `ErrThinkingLevelFixed` (spawn-only) |
| goose | **no** `ThinkingSession` |
| opencode | **no** `ThinkingSession` |

**Why it matters:** Not a crash bug; it is the long-running structural gap
0029 named. New features land four times. Track as platform debt, not a
hotfix.

---

### F7 — Grok ACP parity plan still Proposed (S2)

**Where:** [0039](0039-MADR-grok-acp-parity.md) / PLAN — Proposed. Mentions
MCP status handlers and mid-session model switch beyond what 0052 thinking
levels already shipped.

**Tree facts:** 0052 thinking + catalog work exists; full 0039 surface
(live catalog merge policy, MCP status on phone, CLI policy surfaces) is
not closed as Implemented.

**Action:** re-ground 0039 against HEAD — either flip implemented slices to
done with residual checklist, or keep Proposed with an honest delta.

---

### F8 — Deferred integration tests leave a real race class only unit-covered (S3 / residual S1)

**Where:** 0067 A1 T11 / 0068 P5 deferred items.

| Gap | Coverage today | Risk |
| --- | --- | --- |
| Client park→resume **single outstanding join** end-to-end | Unit pieces; **no** fake-relay+TLS-daemon harness | Half-alive relay legs under iOS suspend |
| U7 `caps.tls_resumed` live | Unit/cache logic only; needs codesigning identity | Unverified TLS session resumption on real iOS/macOS |
| U6 blackhole TCP reap | CI half green; blackhole half hardware-gated | Keepalive effectiveness on real NICs |

**Why it matters:** 0068 P5 unit suite is strong; the deferred harness is
exactly the class of bug that only appears under lifecycle pressure.

---

### F9 — Hardware validation backlog is the dominant residual risk (S3, product-blocking for “daily driver iOS”)

**Source of truth:** [ops-hardware-validation.md](ops-hardware-validation.md)

| Gate | Status (doc) |
| --- | --- |
| 0062 G7 Part B | Partially passed (B4/B7/B10); B12 deferred; several rows blank |
| 0063 Part A | Code shipped; full Part A closure not marked complete in the checklist header |
| 0066 E1/E2 | ✔ 2026-08-03 |
| 0067 Part F (F1–F6) | ⏸ no iPhone |
| 0068 G1 (= F6) | ⏸ no device |
| 0069 G1 + U8 live codesign | Needs FDA walkthrough + Apple Development identity |

**Why it matters:** Software “Implemented” without these rows means
**simulator/unit confidence only** for suspend, Keychain reinstall, Local
Network prompt, and multi-transport churn.

---

### F10 — Documentation status drift (S3, process)

Authoritative code and tests disagree with several Status lines:

| Doc | Status claim | Reality (2026-08-05) |
| --- | --- | --- |
| **0063 MADR + PLAN** | “**not implemented**” | Implemented: `link_health.dart`, `LinkHealth` notifier, app ping + protocol ping constants, `link_liveness_test.dart`, commits `4ea4cf9` / `b3ab57c` / `f47d5f7` / `6e7f78d` |
| **0052 PLAN** | “In progress — Track A+B … C1 live probes ongoing” | Table in same file marks A1–A6, B1–B7, C1 **done** |
| **0029 MADR** | “implementation in progress” | Long-lived; no recent phase close-out |
| **0068/0069 PLAN** (pre-2026-08-05 hygiene) | opened “In progress” after body said complete | Status headers should match MADR “Implemented” (hygiene pass applied in-session for both) |

**Why it matters:** Agents and humans planning work will re-implement or
skip wrong items. 0063 is the worst: an entire “not implemented” MADR sits
on a shipped subsystem.

**Suggested fix:** status flip pass (docs-only commits), one MADR per
obvious close; keep open questions separate from implementation status.

---

### F11 — iOS background attention intentionally unfinished (S2 product, known)

**Where:** 0067 D3 non-goal; `Runner.entitlements` notes absence of
`UIBackgroundModes` / push.

**What:** Pocketed-phone permission asks and turn completions do not wake
the app. Foreground local notifications only.

**Why it matters:** Daily-driver iOS gap, not a regression. Needs its own
MADR (APNs zero-knowledge relay vs accept-no-push), not a silent TODO.

---

### F12 — `pending_asks` still full round-trip on every reconnect (S2 micro)

**Where:** 0068 Q4 open; P4 design keeps `pending_asks` on every reconnect
(safety-critical).

**What:** Resume fast path skips history/list when confirmed unchanged, but
asks are always fetched. Correct and cheap today; open question whether
`resumed` should carry ask snapshot.

**Action:** leave open until hardware F6 measures resync cost; not a bug.

---

### F13 — Widespread empty `catch (_)` in UI layers (S3 hygiene)

**Where:** `sessions_screen.dart`, `chat_screen.dart`, `settings_screen.dart`
(many sites).

**What:** Prefer “don’t crash the frame” over surfacing errors. Acceptable
for secondary UI loads; dangerous if a primary user action fails silently
(create session, send, mode switch — those paths generally set UI error
state, but the density of empty catches makes future regressions easy).

**Suggested fix:** lint or convention — empty catch only at teardown /
best-effort boundaries; elsewhere `debugPrint` + user-visible snack/banner.

---

### F14 — OpenCode streaming MADR still Proposed while coalesce shipped (S3)

**Where:** [0027](0027-MADR-opencode-streaming-rendering.md) Proposed;
`stream_coalesce_ms` wired in daemon for multiple providers; mobile stream
hardening from 0057.

**Action:** re-ground 0027 — accept residual rendering gaps or mark
implemented with leftover polish list.

---

## 3. Cross-cutting themes

1. **Transport stack maturity is high** (0062–0064, 0067 A1, 0068). Residual
   risk is **hardware and integration harnesses**, not missing phase code.
2. **Provider layer is the next structural debt** (0029, 0039, 0048, thinking
   matrix). Features continue to land unevenly by dialect.
3. **Ops automation (0065) and host sandbox recovery (0048)** are the
   highest user-pain unbuilt plans.
4. **Docs Status fields lag code** — treat status flips as part of “done”,
   not optional polish (0063 is the template failure).

---

## 4. Prioritisation → plan phases

Owned by [0070-PLAN](0070-PLAN-deep-dive-remediation.md):

| Plan phase | Item | Finding |
| --- | --- | --- |
| P0 | SessionSynchronizer single-snapshot + failed retry | F1 |
| P1 | link_health v2-aware comment; resume mint omits empty token | F2, F3 |
| P2 | Status hygiene: 0063, 0052, 0027, 0039, 0029, 0070 | F7, F10, F14 |
| P3 | Dispatch 0048 codex sandbox-namespace recovery | F4 |
| P4 | Fake-relay park→resume single-join harness | F8 |
| P5 | Empty-catch convention + primary-action audit | F13 |
| P6 | ops-hardware-validation honesty | F9 |
| Track U | Dispatch 0065 update automation | F5 |
| Residual | 0029 remainder, APNs MADR, U7 live, F12 | F6, F11, F12 |

---

## 5. Verification performed this pass

| Check | Result |
| --- | --- |
| `make test` / `go test ./...` | Pass (2026-08-05) |
| Mobile format + analyze + test | Pass — 746 passed, 3 skipped |
| Marker sweep (TODO/FIXME/Unimplemented) | No critical `UnimplementedError` in production lib paths |
| Open/proposed MADR inventory | 0065, 0048, 0039, 0029, 0027, … |
| Residual-risk comments in ws/relay/session | Races largely named and tested; no new panic paths |

---

## 6. Open questions for the owner

1. Should F1 ship as a tiny hotfix commit before any new plan, or wait for a
   0070-PLAN batch?
2. Is 0048 (Linux bwrap/namespace) still a host you care about, or is
   macOS-first enough that 0048 stays Proposed?
3. Is 0065 (update automation) next product priority now that E1/E2 passed?
4. When does an iPhone enter the lab so Part F / 0068 G1 stop being permanent
   debt?

---

## 7. Decision record (this MADR)

| ID | Decision |
| --- | --- |
| D1 | Record findings only; no code changes in this document’s landing commit set unless paired intentionally |
| D2 | Do not reopen 0068/0069 software phases; residual items stay “outside plan” unless a finding falsifies an acceptance test |
| D3 | Prefer fixing **docs status drift** (F10) in the same remediation window as F1 — cheap, prevents wasted rework |
| D4 | Sequencing lives in **0070-PLAN**; this MADR is the evidence base |

---

## 8. Changelog

| Date | Note |
| --- | --- |
| 2026-08-05 | Initial deep-dive pass; tree green; findings F1–F14 |
| 2026-08-05 | Companion [0070-PLAN](0070-PLAN-deep-dive-remediation.md) drafted for review |
| 2026-08-05 | P0 (F1 resync) + P1 (F2/F3) software landed; P2 status hygiene |
| 2026-08-05 | P3 0048 sandbox; P4 park-resume harness; P5 catch convention; P6 ops; Track U update (Go + Dart check) |

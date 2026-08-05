# MADR 0070 — Implementation plan: deep-dive remediation

<!-- markdownlint-disable MD013 MD024 MD060 -->

- **Status**: **In progress.** P0–P1 implemented; P2 docs hygiene next.
  Grounded 2026-08-05 against HEAD after green `make test` + mobile trio
  and [0070-MADR](0070-MADR-deep-dive-debugging-pass.md) findings F1–F14.
- **Date**: 2026-08-05
- **Source**: [0070-MADR-deep-dive-debugging-pass.md](0070-MADR-deep-dive-debugging-pass.md)
- **Scope**: Bugfixes and hygiene this plan **owns end-to-end** (P0–P2,
  P4 harness, P5 docs re-ground, P6 ops checklist); **dispatch phases**
  that execute existing plans without rewriting them (P3 → 0048; Track U
  → 0065); hardware gates that remain ops-owned (G1–G3). Does **not**
  reopen closed 0068/0069 software phases (0070 D2).
- **Non-goals**: APNs / background push product decision (F11 → separate
  MADR after this plan); full 0029 provider-platform rewrite (F6 →
  residual backlog only); folding `pending_asks` into resume (F12 →
  wait for hardware F6 cost data).

---

## 0. How to read this plan

| Kind | Meaning |
| --- | --- |
| **Own** | Steps, files, tests, and acceptance are fully specified here |
| **Dispatch** | Execute an existing PLAN as written; this plan only sequences and defines the entry/exit gate |
| **Ops** | Human + device; no software phase commits the gate |

**Commit protocol (house rules):** one commit per phase
(`GIT_EDITOR=true git commit` — never `-m`/`-F`); `make pre-add-check`
before any Go stage; mobile phases run
`dart format` + `flutter analyze` + `flutter test` (or the files touched
plus `session_synchronizer_test` / harness tests) before commit; no push
until the phase batch is reviewed.

**Default owner answers** (MADR §6) baked into sequencing so the plan is
actionable without a second review loop. Owner may override at review:

| MADR Q | Default for this plan |
| --- | --- |
| Q1 F1 timing | Ship F1 in **P0** of this plan (not a pre-plan hotfix) |
| Q2 0048 host care | **Yes** — implement 0048 (P3); macOS-first does not delete Linux breakage |
| Q3 0065 priority | **Yes after P0–P2** — Track U dispatches 0065; may pause after P2 if product says wait |
| Q4 iPhone date | Unknown — G2 stays parked; plan authors checklist readiness only |

---

## 0.1 Grounding — seams this plan touches

Verified 2026-08-05:

| Seam | Location | Finding | Change |
| --- | --- | --- | --- |
| Resync list path | `apps/mobile/lib/state/session_synchronizer.dart:103-127` | F1 | Single snapshot; unify id set; failures arm retry |
| Flaky-list test | `apps/mobile/test/session_synchronizer_test.dart:289-324` | F1 | Expect **1** list per pass after fix; keep re-arm assertion |
| App-ping contract comment | `apps/mobile/lib/data/ws/link_health.dart:18-28` | F2 | Version-aware rationale (v1 vs v2 pong horizon) |
| Resume mint | `internal/ws/resume.go:44-58` | F3 | No empty token on wire; warn log; omit fields |
| Auth attach of resume | `internal/ws/server.go` (~resume issue at auth_ok) | F3 | Skip empty token / empty `caps.resume` if mint fails |
| 0063 status | `docs/0063-MADR-*.md`, `docs/0063-PLAN-*.md` | F10 | → Implemented |
| 0052 / 0027 / 0039 / 0029 status | respective docs | F10, F7, F14 | Re-ground or flip |
| Codex sandbox | `internal/provider/codex/*`, `docs/0048-PLAN-*` | F4 | Dispatch 0048 |
| Update automation | no `internal/update` | F5 | Dispatch 0065 |
| Park→resume harness | absent | F8 | New test harness under `apps/mobile/test/` |
| Hardware rows | `docs/ops-hardware-validation.md` | F9 | Checklist + results section refresh |
| UI empty catch density | chat/sessions/settings | F13 | Convention note + one high-risk audit sample (not mass rewrite) |

---

## Phase map (MADR → plan)

| Phase | Owns findings | Kind | Ships |
| --- | --- | --- | --- |
| **P0** | F1 | Own | Single-snapshot resync + tests |
| **P1** | F2, F3 | Own | link_health comment; resume mint hygiene + tests |
| **P2** | F10, F14, F7, F2-doc, partial F6 | Own (docs) | Status flip / re-ground pass |
| **P3** | F4 | Dispatch | Execute [0048-PLAN](0048-PLAN-codex-sandbox-namespace.md) Phases 0–6 |
| **P4** | F8 (park→resume) | Own | Fake-relay + in-process TLS daemon harness + U8-style test |
| **P5** | F13 | Own | Catch convention + audit of primary actions (no mass rewrite) |
| **P6** | F9, F12 note | Ops docs | Hardware checklist honesty; leave F12 open |
| **Track U** | F5 | Dispatch | Execute [0065-PLAN](0065-PLAN-update-automation.md) after P0–P2 green |
| **Backlog** | F6 remainder, F11, U7 live, blackhole U6 | Residual | Named exits only — not sequenced commits here |

```
P0 (F1 resync) ──► P1 (F2/F3) ──► P2 (docs status)
                        │
                        ├─► P3 (0048 dispatch)     [can start after P1; parallel P2]
                        │
                        ├─► P4 (harness)           [after P0; independent of P3]
                        │
                        ├─► P5 (catch convention)  [anytime after P0]
                        │
                        └─► P6 (ops checklist)     [after P2]
                                 │
Track U (0065) ◄─────────────────┘  only after P0–P2; owner may delay

G1–G3 hardware  — whenever devices/identity exist (never blocks software close)
```

---

## P0 — SessionSynchronizer single-snapshot resync (F1) — **Own**

### Goal

One `listSessionSnapshot` per resync pass. That snapshot is the sole source
of epoch, `seqs` bounds, meta sync, **and** host session ids. Any list
failure sets `failed = true` and arms `_scheduleRetry`. Preserve resume
fast-path (zero lists when covered-and-unchanged). Preserve generation
supersession and history concurrency = 2.

### Steps

1. **Rewrite `_resyncBody` list section**
   (`session_synchronizer.dart:98-127`):
   - After the resume early-return block, call `listSessionSnapshot()`
     **once**.
   - On success: set `force` / `_epoch` from `snap.epoch`; `bounds =
     snap.seqs`; `transcripts.syncFromMeta(snap.sessions, complete:
     snap.complete)`; build
     `ids = {...transcripts.knownSessionIds(), ...snap.sessions.map((s) => s.id)}`.
   - On failure: `failed = true`; `debugPrint`; leave `bounds` empty and
     `ids` as known-only (history workers may still run for gap-suspected
     / lastSeq>0 — same as today after a first-list failure).
   - **Delete** the second try/catch list entirely.
2. **Comment** above the single call: “One snapshot per pass (0070 F1) —
   a second list used to enlarge ids but swallowed errors and doubled
   cost on every connected edge.”
3. **Update resume-path comment** at `:75-81` — change “both list calls”
   to “the list call” / “full reconcile”.

### Tests (must update + add)

File: `apps/mobile/test/session_synchronizer_test.dart`

| ID | Case | Expect |
| --- | --- | --- |
| U0.1 | Existing fast-skip / force / gap matrix | Unchanged behaviour; `listAttemptsCount` still `greaterThan(0)` only when truth path runs |
| U0.2 | `failed resync re-arms itself while still connected` | **`failures: 1`** (one list per pass); after first `resync`, `listAttempts == 1`; after re-arm delay, `listAttempts >= 2` (was 2 then ≥3) |
| U0.3 | **New:** first list succeeds with host sessions `H1` not yet known locally; no second call | History (or skip) covers `H1` when `lastSeq`/gap rules require; `listAttempts == 1` for that pass |
| U0.4 | Resume covered-and-unchanged | Still **zero** list calls |

### Acceptance

- [ ] `flutter test test/session_synchronizer_test.dart` green
- [ ] No `listSessionSnapshot` call site remains inside `_resyncBody`
  except one
- [ ] Grep for `// Also cover host-listed` is gone

### Commit

`fix(mobile): single-snapshot session resync (0070 F1)`

---

## P1 — Liveness comment truth + resume mint hygiene (F2, F3) — **Own**

### P1.A — `link_health.dart` contract comment (F2)

**File:** `apps/mobile/lib/data/ws/link_health.dart`

Replace the `kAppPingPeriod` doc block so it states:

1. **Unconditional app `ping` stays mandatory** for all negotiated
   versions — drives `lastVerifiedAt` / `LinkHealth` (0063 D1 UI) and
   resets the **v1** daemon data-frame deadline.
2. **v2 daemons** also extend the read horizon on completed **transport**
   pongs (`internal/ws/liveness.go` `pingLoop`/`lastPong`;
   `caps.ws_ping_resets_deadline: true` per `protocol-v2.md`). That does
   **not** allow skipping the app ping: version skew (v2 phone → v1
   daemon), UI freshness, and long inbound-only streams still need it.
3. Cite 0063 B1 **and** 0068 P1 so a later battery optimisation cannot
   “dedupe” pings based on the outdated “only thing that resets the
   deadline” sentence.

**No behaviour change.** Optional: one-line cross-ref in
`mcremote_client.dart` near `_appPingPeriod` if a duplicate claim exists
(grep `only thing` / `protocol obligation`).

**Tests:** none required (comment-only). `flutter analyze` clean.

### P1.B — Resume token mint (F3)

**Files:** `internal/ws/resume.go`, `internal/ws/server.go` (auth_ok
assembly), `internal/ws/resume_test.go`

1. Change `issue` signature to surface failure without empty tokens:

   ```go
   // issue returns token, grantedWindow, ok.
   // ok is false when the token could not be minted (RNG failure):
   // caller must omit resume_token and caps.resume rather than send "".
   func (r *resumeStore) issue(deviceID string, requested time.Duration) (token string, window time.Duration, ok bool)
   ```

2. On `rand.Read` error: do **not** store an entry; return `"", window, false`.
3. Call site (`server.go` ~auth path that sets `payload.ResumeToken` and
   `caps.Resume`): if `!ok`, leave `ResumeToken` empty **and** leave
   `Caps.Resume` nil (or omit); log once:

   ```text
   level=WARN msg="resume token mint failed; resume disabled for this auth" device_id=…
   ```

   Auth itself still succeeds (0068: resume failure is not auth failure).
4. `validate` already rejects `token == ""` — keep.

**Tests:**

| ID | Case |
| --- | --- |
| U1.1 | Inject `rand` failure via test seam (add `readRand func([]byte) (int, error)` on `resumeStore`, default `rand.Read`) → `ok == false`, map unchanged |
| U1.2 | Existing U4 resume suite still green with new signature |
| U1.3 | Auth path unit: mint fail → `auth_ok` has no `resume_token`, `caps.resume` absent, `resume_failed` not set unless client offered resume |

### Acceptance

- [ ] `go test ./internal/ws/ -count=1` green
- [ ] Grep `return "", window` without `ok` gone from `resume.go`
- [ ] link_health comment no longer claims app ping is the sole deadline
  reset on all versions

### Commit

`fix(ws,mobile): resume mint omits empty token; v2-aware ping docs (0070 F2/F3)`

---

## P2 — Documentation status hygiene (F10, F14, F7, F6 partial) — **Own (docs)**

### Goal

Status lines match the tree so agents stop re-planning shipped work or
skipping open work. **No code.**

### Steps (exact edits)

#### P2.1 — 0063 → Implemented (F10 worst)

1. `docs/0063-MADR-connection-liveness-truth.md` Status:

   ```text
   Implemented (software 2026-08; plan P0–P4 landed). Hardware Part A
   rows remain in ops-hardware-validation.md.
   ```

2. `docs/0063-PLAN-connection-liveness-implementation.md` Status: same
   shape; mark P0–P4 done in the phase headers if not already; leave
   hardware G* as open.
3. One-line pointer to `link_health.dart` + `link_liveness_test.dart` as
   the implementation anchors.

#### P2.2 — 0052 PLAN → Complete

1. Status bullet: **Complete** (Track A + B + C1 done 2026-07-29).
2. Ensure the phase table and Status do not contradict.

#### P2.3 — 0027 re-ground

1. Read tree: `stream_coalesce_ms`, mobile stream hardening (0057), chunk
   render paths.
2. Either:
   - **Status: Implemented** with a short “Residual polish” list, or
   - **Status: Partially implemented** with a checkbox delta of what
     remains from the original 0027 body.
3. Prefer honesty over “Proposed” if majority shipped.

#### P2.4 — 0039 re-ground (F7)

1. Inventory against HEAD: thinking levels (0052), catalog, MCP status
   wire, mid-session model switch, CLI policy.
2. Rewrite Status to **Partially implemented** with a table:

   | Slice | State |
   | --- | --- |
   | … | done / not done |

3. Do **not** implement missing slices in P2.

#### P2.5 — 0029 residual note (F6)

1. Status stays “implementation in progress” **or** becomes “Phase 0–1
   partial; remainder backlog (0070)”.
2. Add a short “As of 2026-08-05” residual: thinking matrix gap
   (goose/opencode), dialect duplication — link 0070 F6.
3. No new 0029 phases invented here.

#### P2.6 — 0070 MADR close-out pointer

1. Set companion **Implementation plan** field to this file.
2. Status: Findings accepted for remediation under 0070-PLAN; D4
   satisfied.
3. Changelog row for plan landing.

#### P2.7 — Cross-links

1. `ops-hardware-validation.md` header: note 0063 software implemented;
   Part A is verification-only.
2. 0068 MADR open Q3 still hardware — leave; ensure no doc claims U7
   done.

### Acceptance

- [ ] `rg 'not implemented' docs/0063*` returns no Status-line hit for
  the MADR/PLAN headers
- [ ] 0052 PLAN Status is Complete
- [ ] 0039 has an explicit done/not-done table

### Commit

`docs: status hygiene for 0063/0052/0027/0039/0029/0070 (0070 P2)`

---

## P3 — Codex sandbox namespace recovery (F4) — **Dispatch 0048**

### Entry gate

- P1 green (no dependency on F1/F2, but keep train order simple).
- Owner confirms Linux host class still in scope (default **yes**).

### Execution

Execute [0048-PLAN-codex-sandbox-namespace.md](0048-PLAN-codex-sandbox-namespace.md)
**as written**, phases **0 → 1 → 2 → 3 → 4 → 5 → 6**, one commit per 0048
phase (or one commit per 0048 phase group if that plan allows — prefer
0048’s own commit protocol).

**Do not re-specify** probe markers, `sandbox_broken_policy`, or notice
copy here — 0048 is the source of truth. This phase only adds:

### 0070 deltas on top of 0048

1. **Cross-link 0069:** in 0048 docs phase and `docs/ops-macos-tcc.md` (or
   a short `docs/config.md` note), disambiguate:
   - **macOS FDA / Seatbelt** → 0069
   - **Linux userns / bwrap** → 0048  
   so operators do not apply the wrong fix.
2. After 0048 Phase 6, flip **0048 MADR + PLAN Status → Implemented**
   (software); residual live pin stays live-tagged.
3. Record in 0070 MADR changelog: F4 closed by 0048.

### Acceptance (exit gate)

- [ ] All 0048 phase acceptance checkboxes done
- [ ] `go test ./internal/provider/codex/ ./internal/config/ -count=1`
- [ ] `go test -tags live_codex ./internal/provider/codex/ -count=1` when
  codex binary present (otherwise note skip in commit message)
- [ ] 0048 Status flipped

### Commits

Owned by 0048’s phase commits; final docs commit may be:

`docs: mark 0048 implemented; cross-link 0069 denial classes (0070 P3)`

---

## P4 — Park→resume single-join integration harness (F8) — **Own**

### Goal

Close 0067 A1 T11 / 0068 P5 deferred item: **exactly one relay `join`**
across a park→resume cycle when using relay transport, with teardown
completing or superseded before the next dial adopts a socket.

### Design (actionable)

Build a **Dart-only** harness (no real mcrelay binary required for CI):

1. **`FakeRelayServer`** (in `apps/mobile/test/support/` or
   `test/harness/`):
   - Binds `HttpServer` loopback; WebSocket upgrade on `/v1/join` (or the
     path `RelayTransport` uses — verify
     `apps/mobile/lib/data/ws/relay_transport.dart` join URL construction
     and match it).
   - Counts `join` frames / upgrade requests with a thread-safe counter.
   - Optionally holds the first join open until a second arrives (to
     detect double-join).
2. **Inner daemon stand-in:**
   - Minimal TLS or plain WS depending on what the client’s relay path
     expects after splice simulation.
   - If full TLS is too heavy for CI: inject a test seam on
     `McremoteClient` / `RelayTransport` that accepts a custom
     `RelayTransport` factory or `HttpClient` (prefer existing seams;
     add `@visibleForTesting` factory only if none exists).
3. **Scenario test** `park_resume_single_join_test.dart`:
   - Connect via relay (or bind path with fake).
   - Call park/disconnect path used by `app_lifecycle` background
     (`disconnect(manual: false)` or the production entrypoint).
   - Immediately `reconnect` with `userInitiated: false` (resume-shaped).
   - Assert `fake.joinCount == 1` at steady state (or == 2 only if the
     first fully closed before the second — never two concurrent active
     joins).
   - Assert no orphan sockets (fake tracks open connections → 1 or 0
     after park).

### Grounding work before coding (half-day spike, same phase)

1. Trace `RelayTransport.open` / join envelope fields and URL.
2. List existing fakes in `relay_lifecycle_test.dart`,
   `relay_transport_test.dart` — **reuse** before inventing parallel
   infrastructure.
3. If a pure unit path already proves single-join under concurrent
   close/open, upgrade that test to an explicit “park→resume episode”
   name and document T11 as closed at unit level; only build FakeRelay if
   a gap remains.

### Out of scope for P4

- U7 `tls_resumed` live (needs codesigning identity) — stays G3
- Blackhole TCP reap — stays hardware / live-tagged

### Acceptance

- [ ] Test name references `0067 T11` / `0070 P4`
- [ ] `flutter test` includes the new test; full mobile suite green
- [ ] 0067 A1 table: T11 disposition → ✅ (or ✅ unit+harness) with date
- [ ] 0068 PLAN deferred line annotated closed

### Commit

`test(mobile): park-resume single-join harness (0070 P4 / 0067 T11)`

---

## P5 — Empty-catch convention + primary-action audit (F13) — **Own**

### Goal

Reduce silent primary-action failures without a noisy mass rewrite of
every `catch (_)`.

### Steps

1. **Write the convention** in `docs/standards/mobile/dart.md` (or
   `apps/mobile/README.md` if standards edit is heavier) — short rules:

   | Allowed empty / discard catch | Not allowed |
   | --- | --- |
   | Teardown / best-effort socket close | User-initiated send, create, pair, mode switch, connect |
   | Secondary decoration (clipboard, optional meta) | Anything that leaves the UI claiming success |
   | Must `debugPrint` at minimum if not user-visible | — |

2. **Audit sample (mandatory):** grep
   `catch (_)` / `catch (e) {}` in:
   - `chat_screen.dart`
   - `sessions_screen.dart`
   - `settings_screen.dart`
   - `connect_screen.dart`  
   For each hit on a **primary action** path, either:
   - surface error (banner / snack / setState error field), or
   - document why discard is correct with a one-line comment
     `// best-effort: …`.
3. Fix **only** the primary-action gaps found (cap: ~5–10 sites). Do not
   churn teardown catches.

### Tests

- Prefer existing widget tests; add a case only if a primary path lost
  error surfacing and had no coverage.

### Acceptance

- [ ] Convention paragraph landed
- [ ] Audit notes in the commit message (list of files/lines touched)
- [ ] Mobile suite green

### Commit

`fix(mobile): surface primary-action errors; catch convention (0070 P5)`

---

## P6 — Hardware / ops checklist honesty (F9, F12) — **Ops docs**

### Goal

Make `ops-hardware-validation.md` the single place that shows what is
software-done vs device-blocked. No false “open software” signals.

### Steps

1. **Header table** — update open gates:

   | Gate | Software | Hardware |
   | --- | --- | --- |
   | 0063 Part A | done | rows A* verification |
   | 0062 G7 | done | Part B remainder (B12 deferred, blank rows) |
   | 0067/0068 F1–F6 | done | ⏸ no iPhone |
   | 0069 G1/U8 | done | identity + FDA walkthrough |
   | 0066 E3 | optional negative | still open if unused |

2. **Results section** — ensure A1–A4 / B4/B7/B10 pass notes remain;
   add “software complete as of 0070 P2” for 0063.
3. **F12** — one sentence under 0068 residual: `pending_asks` full fetch
   retained until F6 measures cost; no code change.
4. **0069 G1** — checklist pointer to `docs/ops-macos-tcc.md` (already
   exists); mark steps not run as ⏸.

### Acceptance

- [ ] Reader can answer “what blocks daily-driver iOS?” in one table
- [ ] No Status in linked MADRs claims software unfinished for 0063

### Commit

`docs: ops-hardware-validation gate table for 0070 residuals`

---

## Track U — Update automation (F5) — **Dispatch 0065**

### Entry gate

- P0–P2 merged and green.
- Phone gate already open (E1/E2 ✔ 2026-08-03) — reconfirm in
  `ops-hardware-validation.md`.
- Owner has not paused Track U.

### Execution

Execute [0065-PLAN-update-automation.md](0065-PLAN-update-automation.md)
from its phase 0 grounding forward. **0070 does not restate** discovery,
checksum, swap, or phone FileProvider work.

### Exit gate

- 0065 Status → Implemented (or Partially if phone/host split).
- 0070 MADR F5 marked closed in changelog.
- Re-sign note from 0069/0060 remains satisfied (`MC_CODESIGN_IDENTITY`).

### Sequencing vs P3/P4

Track U may run **in parallel** with P3 (0048) and P4 (harness) if
staffing allows; both touch different trees (update vs codex vs mobile
test). Avoid parallel edits to `Makefile` / `install-binary.sh` with
0065 — serialise those files behind 0065 if both land.

---

## Residual backlog (explicitly not scheduled)

| Item | Finding | Exit condition |
| --- | --- | --- |
| Full 0029 platform extraction | F6 | Separate plan when dialect cost forces it |
| iOS background attention / APNs | F11 | New MADR (0071+); 0067 D3 already scoped out |
| U7 `tls_resumed` live | F8 | Codesigning identity on host + live-tagged test |
| U6 blackhole TCP reap | F8 | Hardware / privileged live test |
| 0068 Q4 pending_asks in resume | F12 | After F6 resync byte counts |
| Thinking on goose/opencode | F6 matrix | Per-provider capability when engines expose it |

---

## File checklist (owned phases only)

| File | Phases |
| --- | --- |
| `apps/mobile/lib/state/session_synchronizer.dart` | P0 |
| `apps/mobile/test/session_synchronizer_test.dart` | P0 |
| `apps/mobile/lib/data/ws/link_health.dart` | P1 |
| `internal/ws/resume.go` | P1 |
| `internal/ws/server.go` | P1 |
| `internal/ws/resume_test.go` | P1 |
| `docs/0063-MADR-connection-liveness-truth.md` | P2 |
| `docs/0063-PLAN-connection-liveness-implementation.md` | P2 |
| `docs/0052-PLAN-thinking-levels-and-settings.md` | P2 |
| `docs/0027-MADR-opencode-streaming-rendering.md` | P2 |
| `docs/0039-MADR-grok-acp-parity.md` (+ PLAN if needed) | P2 |
| `docs/0029-MADR-provider-platform-canonicalization.md` | P2 |
| `docs/0070-MADR-deep-dive-debugging-pass.md` | P2 |
| `docs/ops-hardware-validation.md` | P2, P6 |
| `docs/0048-*` + codex provider tree | P3 (via 0048) |
| `apps/mobile/test/**` harness + park-resume test | P4 |
| `docs/0067-MADR-ios-port.md` (T11 disposition) | P4 |
| `docs/standards/mobile/dart.md` or `apps/mobile/README.md` | P5 |
| `apps/mobile/lib/features/{chat,sessions,settings,connect}/*` | P5 (limited) |
| `docs/0065-*` + update tree | Track U |

---

## Verification map (0070 findings → phase → test)

| Finding | Phase | Verification |
| --- | --- | --- |
| F1 | P0 | U0.1–U0.4 dart |
| F2 | P1.A | review + analyze |
| F3 | P1.B | U1.1–U1.3 go |
| F10, F14, F7, F6 note | P2 | doc review / rg Status |
| F4 | P3 | 0048’s U* + live_codex |
| F8 park→resume | P4 | new harness test |
| F13 | P5 | convention + sample fixes |
| F9 | P6 | ops table |
| F5 | Track U | 0065 acceptance |
| F11, F12, U7 live | residual | not in this plan’s software close |

### Full suite gate (end of each software phase)

```bash
# Go-touching phases (P1, P3, Track U):
make pre-add-check FILES="…touched.go…"
go test ./... 

# Dart-touching phases (P0, P1 comment, P4, P5):
cd apps/mobile && dart format --output=none --set-exit-if-changed .
flutter analyze
flutter test
```

---

## Edge cases held by design

- **Resume fast path with empty known set:** still falls through to one
  list (P0); never “success with zero work” when local has no sessions
  but host does.
- **List fails, history still attempted for gap-suspected:** kept — better
  partial heal than full stall; retry still armed.
- **RNG failure on resume mint:** auth succeeds; client full-resyncs;
  next auth retries mint.
- **0048 on a healthy userns host:** probe reports healthy; no notice;
  policy no-op — must not break macOS/Seatbelt hosts (0069).
- **P4 harness weaker than real mcrelay:** acceptable for join-count
  invariant; hardware F6 remains the churn proof.
- **Track U delayed by product:** plan still complete if P0–P6 done;
  F5 stays open in 0070 MADR until 0065 lands.

---

## Review checklist (for the owner before approval)

- [ ] Defaults for MADR Q1–Q4 accepted or overridden in writing
- [ ] P3 (0048) in or out for this train
- [ ] Track U (0065) in or deferred with a date
- [ ] P4 harness effort accepted (~1–2 days) vs “document T11 unit-only”
- [ ] No requirement to implement F11 APNs inside 0070

---

## Sequencing summary

| Order | Phase | Est. effort | Risk |
| --- | --- | --- | --- |
| 1 | P0 F1 resync | 0.5 d | Low — tests already target the shape |
| 2 | P1 F2/F3 | 0.5 d | Low |
| 3 | P2 docs status | 0.5–1 d | Low — judgment on 0027/0039 |
| 4a | P3 0048 | 2–4 d | Med — host-dependent probe |
| 4b | P4 harness | 1–2 d | Med — seam discovery |
| 5 | P5 catch audit | 0.5–1 d | Low |
| 6 | P6 ops docs | 0.25 d | Low |
| 7 | Track U 0065 | per 0065 plan | High product surface |

**Software-complete for 0070** means P0–P2 + P5 + P6 landed, and either
P3/P4 done or explicitly deferred in the 0070 MADR changelog with owner
sign-off. Track U is a product track, not a bugfix.

---

## Changelog

| Date | Note |
| --- | --- |
| 2026-08-05 | Initial plan drafted for review from 0070 MADR F1–F14 |

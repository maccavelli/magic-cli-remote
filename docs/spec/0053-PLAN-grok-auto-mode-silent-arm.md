# MADR 0053 — Implementation plan: grok auto silent arm and mode-path gaps

<!-- markdownlint-disable MD013 MD024 MD060 -->

Companion to [MADR 0053](./0053-MADR-grok-auto-mode-silent-arm.md). Read that
first: it carries the symptom, the host evidence (session histories + journald),
the root-cause analysis, and the finding table F1–F8. This document is the
**build order**, keyed to source as of `mcremote 0.5.3.3` / commit `8b7f3ba`
(2026-07-30), and is the only place that specifies exact code changes.

- **Status**: **Implemented** (2026-07-30). Phases A–E landed together:
  emit-on-arm, arm/disarm logs, pending sweep, live default→auto test, display
  name + EqualFold. F7 left open by design.
- **Date**: 2026-07-30
- **Scope**: `internal/provider/acpagent` (production + unit tests),
  `internal/provider/grok` (live test only). **No** protocol change, **no**
  mobile change, **no** codex/opencode/goose change.
- **Standards**: `/home/mac/standards/go` — `concurrency.md` (mutex + channel
  claim discipline on `permWaiter`), `testing.md` (table-driven; fail-without
  production change), `logging.md` (structured slog).
- **Related plans / MADRs**:
  - [MADR 0049](./0049-MADR-grok-auto-mode.md) +
    [plan](./0049-PLAN-grok-auto-mode.md) — synthetic auto; D1/D4; live-gate
    lesson that only covered plan→auto
  - [MADR 0044](./0044-MADR-auto-approve-modes.md) D4.5 — pending sweep on arm
  - [MADR 0051](./0051-MADR-auto-approve-chat-noise.md) — `noteAutoApproval` /
    `approval_summary` audit trail

---

## 0. Assessment of MADR 0053 (what holds, what we refine)

### 0.1 Claims that stand as facts

| Claim | Verdict | Pin |
|---|---|---|
| F1 silent arm is the user-visible bug | **Confirmed** | `armAutoMode` only self-emits when `target == ""`; grok's `normalModeID()` is always `"default"` |
| Already-on-default is the common UI path | **Confirmed** | Create-time `session_mode` always `current_mode_id=default`; every grok history with auto advertised has `n_mode_ev=1` |
| Live test hides F1 | **Confirmed** | `TestLiveGrokAutoModeArmsWithoutSendingAuto` parks in plan first |
| Codex/OpenCode always self-emit on arm | **Confirmed** | `codex/session.go` `SetMode` emits `TypeMode`; `httpagent/session.go` `SetMode` always emits dialect return |
| Mobile chip is event-only | **Confirmed** | `setMode` awaits ok; `_ModeChip` reads `resolveDisplayedMode(modes, currentModeId)` |
| No journal line for grok arm | **Confirmed** | No `log.Warn` in `armAutoMode`; journal has only codex/opencode arm lines |
| `autoApprove` may be true while chip says Build | **Confirmed by code** | Flag set before the (no-op) agent RPC; emit skipped — D4 inverted |

### 0.2 MADR decisions — adopt, refine, defer

| MADR §6 | Plan decision |
|---|---|
| **D1** Always announce successful arm | **Adopt as Phase A.** Use a **current-only** `session_mode` emit (empty `modes`), matching codex/httpagent — not `emitModesOrStatic(nil)` (full list). |
| **D2** Skip redundant `SetSessionMode` when already normal | **Defer.** acpagent does not track last agent mode id. Keeping the RPC is correct and cheap; D1 alone fixes the bug. Revisit only if live grok ever errors on same-mode set. |
| **D3** WARN log on arm | **Adopt as Phase B** (same commit as A is fine; listed separate for review). |
| **D4** Sweep pending permissions | **Adopt as Phase C.** Requires extending `permWaiter` (today it has only `ch` + `resolved` — no option id to answer with). Details in §Phase C. |
| **D5** Tests that fail without D1 | **Adopt** across A/C/D. |
| **D6** Out of scope | **Keep.** No mobile optimistic update, no `/always-approve` filtering, no `permission_mode` semantics change, no `remote_commands` race (F7). |

### 0.3 Findings — in-scope vs out

| ID | In this plan? | How |
|---|---|---|
| F1 silent arm | **Yes — Phase A** | Always emit after successful arm |
| F2 no pending sweep | **Yes — Phase C** | `sweepPendingPermissions` + waiter fields |
| F3 no arm log | **Yes — Phase B** | `log.Warn` on arm; optional `log.Info` on disarm |
| F4 test gap | **Yes — Phase A unit + Phase D live** | default→auto without plan detour |
| F5 name `"auto"` vs Build/Plan | **Optional Phase E** | `Name: "Auto"` only; id stays `auto` |
| F6 case-sensitive static ids | **Optional Phase E** | `EqualFold` in the synthetic validation branch |
| F7 early `remote_commands` | **No** | Separate MADR if slash UI freezes first snapshot |
| F8 dual auto controls | **No** | Already documented in MADR 0049 D6 |

### 0.4 What not to "fix"

- Do **not** change mobile. After Phase A the existing chip path works.
- Do **not** forward `auto` to the agent (MADR 0049 D1 still holds).
- Do **not** map onto `--permission-mode auto`.
- Do **not** remove the `reportedModeID` rewrite on `current_mode_update` — plan→auto still needs it when the agent confirms `default`.
- Do **not** replace the existing plan→auto live test; **add** a default→auto live test.

---

## 1. Goal, non-goals, ground rules

### Goal

From a grok session sitting on **Build** (`default`):

1. User confirms the dangerous-mode dialog.
2. Daemon arms `autoApprove`, leaves the agent in its normal ACP mode, and
   **emits** `session_mode` with `current_mode_id=auto`.
3. Phone shows the bolt / error-tinted chip.
4. Open permission sheets (if any) resolve as allowed.
5. Journal shows an arm line.
6. Unit + live tests fail if step 2's emit is removed.

### Non-goals

- Protocol / mobile / other providers.
- Tracking agent mode to skip `SetSessionMode` (D2 deferred).
- F7 remote_commands race, F8 dual controls.
- Changing when `finishApprovals` runs on leave-auto (already correct).

### Ground rules

1. **One phase → one commit** (A+B may land together if review prefers a single
   "arm path" commit; C stays separate because it touches the waiter contract).
2. **Go gate before `git add`**: `make pre-add-check FILES="…"` on every
   touched `.go` file.
3. **Commit without `-m`** — prepare-commit-msg writes the message.
4. Prefer **symbol names** over line numbers (they drift).
5. Every new unit test must be **verified to fail** when its production change
   is reverted (or not yet applied).
6. Live tests: `go test -tags live_grok ./internal/provider/grok/ -count=1`
   when a grok binary is available; not required for CI green.

### File map

| Area | Path |
|---|---|
| Arm / SetMode / permissions | `internal/provider/acpagent/session.go` |
| Unit tests (auto) | `internal/provider/acpagent/automode_test.go` |
| Unit helpers | `internal/provider/acpagent/session_test.go` (`recvEvent`, `modeSession`) |
| Live tests | `internal/provider/grok/live_mode_test.go` |
| Peer emit reference | `internal/provider/codex/session.go` `SetMode`, `internal/provider/httpagent/session.go` `SetMode` |
| Peer sweep reference | `internal/provider/codex/session.go` `sweepPendingApprovals` |
| Docs status | `docs/0053-MADR-grok-auto-mode-silent-arm.md` |

### Peer contracts to copy (do not reinvent)

**Emit on arm (codex):**

```go
s.emit(event.Event{
    Type:          event.TypeMode,
    SessionID:     s.localID,
    Timestamp:     time.Now().UTC(),
    CurrentModeID: m.mode.ID, // for us: autoModeID
})
```

**Emit on arm (httpagent):** always `Emit(TypeMode, CurrentModeID: current)`
after dialect `SetMode` returns — independent of engine notifications.

**Sweep on arm (codex):** snapshot pending under lock, clear map, answer each
with accept, emit `permission_resolved`, call `noteAutoApproval`. ACP cannot
copy the codex RPC shape; it must unblock `permWaiter` channels the same way
`Cancel` / `RespondPermission` do.

---

## 2. Phase order

```text
A  acpagent: always emit session_mode after successful arm   (F1, F4 unit)
B  acpagent: arm / disarm logs                               (F3)
C  acpagent: pending-permission sweep + waiter fields        (F2)
D  grok live: arm from default (no plan detour)              (F4 live)
E  optional polish: Name "Auto", EqualFold validation        (F5, F6)
F  docs + verification
```

A is load-bearing and can ship alone (fixes the reported bug). B is tiny and
should ship with A. C is the next user-facing gap (arm while a sheet is open).
D proves A against a real grok. E is cosmetic/latent. F closes the MADR.

---

## Phase A — Always emit after successful arm (F1)

**Files:** `internal/provider/acpagent/session.go`,
`internal/provider/acpagent/automode_test.go`

### A.1 Production change — `armAutoMode`

Replace the `if target == "" { emitModesOrStatic(nil) }` tail with an
**unconditional** current-mode emit after a successful arm.

Target shape:

```go
// armAutoMode turns on daemon-side auto-approve and puts the agent into its
// normal working mode. …
func (s *session) armAutoMode(ctx context.Context, agentID string) error {
    target := s.normalModeID()
    s.mu.Lock()
    s.autoApprove = true
    s.mu.Unlock()

    if target != "" {
        if _, err := s.conn.SetSessionMode(ctx, acp.SetSessionModeRequest{
            SessionId: acp.SessionId(agentID),
            ModeId:    acp.SessionModeId(target),
        }); err != nil {
            s.mu.Lock()
            s.autoApprove = false
            s.mu.Unlock()
            return err
        }
    }

    // Always announce. The agent only sends current_mode_update when its mode
    // actually changes; arming from the already-normal state is a no-op for
    // the agent and was silent for the client (MADR 0053 F1). Codex and
    // httpagent self-emit on every SetMode for the same reason.
    s.emitArmedMode()
    return nil
}

// emitArmedMode publishes the synthetic auto id as the current mode. Modes
// list is omitted — clients keep the create-time list (protocol-v1 session_mode
// change form; same as codex/httpagent).
func (s *session) emitArmedMode() {
    s.emit(event.Event{
        Type:          event.TypeMode,
        SessionID:     s.localID,
        Timestamp:     time.Now().UTC(),
        CurrentModeID: autoModeID,
    })
}
```

**Why not `emitModesOrStatic(nil)`?**

- That helper re-sends the full mode list and re-sets `syntheticModes`.
- Peer providers emit **current-only** on switch.
- Clients already merge: empty `modes` keeps the existing list
  (`transcript_reducer.dart`, `manager.go` TypeMode handler).

**Why not rely on `reportedModeID` + agent notify alone?**

That is exactly the path that fails when the agent is already on `default`.
Keep `reportedModeID` on the notification path so a *real* switch still
rewrites the agent's `default` confirmation to `auto` (existing
`TestCurrentModeUpdateDoesNotOverwriteArmedAuto`).

**Double-emit is OK.** plan→auto may produce:

1. daemon `emitArmedMode` → `current=auto`,
2. agent `current_mode_update(default)` → rewritten to `auto`.

Mobile reducer no-ops when `currentId` is already `auto`. History may show two
identical current ids — harmless; prefer not to suppress either path.

**Failed switch:** keep current behaviour — disarm, return err, **do not** emit.

**Idempotent re-arm:** `SetMode("auto")` while already armed sets the flag again
and emits again — fine.

### A.2 Comment updates

Update the `SetMode` doc comment that says "no local state is updated here /
agent confirms via current_mode_update" so it still describes **real** modes,
and note that synthetic auto is announced by the daemon (cross-ref MADR 0053).

### A.3 Unit tests — must fail without A.1

Add to `automode_test.go` (reuse `autoModeSession`, `startFakeAgent`,
`recvEvent`):

#### `TestArmingAutoFromDefaultEmitsCurrentMode`

The regression test for F1:

```text
Given: synthesizeAuto, syntheticModes, fake agent that accepts set_mode and
       NEVER sends current_mode_update
When:  SetMode(ctx, "auto")   // session was never put in plan
Then:
  - err == nil
  - autoApprove == true
  - fake saw exactly [default] (never "auto")
  - events contain TypeMode with CurrentModeID == "auto"
  - that event's Modes is empty or nil (current-only form)
```

**Verified to fail** against pre-A.1 code: today's `armAutoMode` returns nil and
sets the flag but emits nothing when `target == "default"`.

#### `TestArmingAutoWhenNormalModeRPCFailsDoesNotEmit`

```text
Given: failSet=true fake
When:  SetMode(ctx, "auto")
Then:  err != nil, autoApprove == false, no TypeMode with current auto
```

Extends the existing failed-switch test with the emit assertion.

#### Keep / do not break

- `TestArmingAutoSendsNormalModeNotAuto`
- `TestCurrentModeUpdateDoesNotOverwriteArmedAuto`
- `TestSelectingARealModeDisarmsAuto`
- `TestArmedAutoAnswersPermissionsWithoutPrompting`

### A.4 Acceptance

```bash
go test ./internal/provider/acpagent/ -count=1
go test ./internal/provider/grok/ -count=1   # unit only, no live tag
make pre-add-check FILES="internal/provider/acpagent/session.go internal/provider/acpagent/automode_test.go"
```

Manual (optional before C/D): rebuild/install mcremote, open a grok session on
the phone, Build → auto → confirm → chip shows bolt + auto.

---

## Phase B — Arm / disarm observability (F3)

**File:** `internal/provider/acpagent/session.go` (same as A if co-committed)

### B.1 On successful arm (after emit)

```go
s.log.Warn("acp auto-approve armed",
    slog.String("session_id", s.localID),
    slog.String("agent_session_id", agentID),
    slog.String("agent_mode", target), // "" if none; "default" for grok
)
```

Use **Warn** to match codex/opencode (`codex auto-approve armed`,
`opencode auto-approve armed`). Component name comes from the provider logger
already (`provider.grok`).

If `target == ""`, still log (empty `agent_mode` is fine).

### B.2 On successful real-mode switch that disarms

In `SetMode`, when `err == nil` and you clear `autoApprove`, log only if it
**was** armed (read previous value under the lock):

```go
s.mu.Lock()
wasAuto := s.autoApprove
s.autoApprove = false
s.mu.Unlock()
if wasAuto {
    s.log.Info("acp auto-approve disarmed",
        slog.String("session_id", s.localID),
        slog.String("agent_session_id", agentID),
        slog.String("mode", modeID),
    )
}
s.finishApprovals() // existing
```

Do not log every mode switch — only the leave-auto transition.

### B.3 Tests

No unit test required for slog. Manual check after install:

```bash
journalctl --user -u mcremote -f | rg 'auto-approve'
# arm on phone → "acp auto-approve armed"
# switch to plan → "acp auto-approve disarmed"
```

### B.4 Acceptance

Same package tests as A; log visible in a live session.

---

## Phase C — Sweep pending permissions on arm (F2)

**Files:** `internal/provider/acpagent/session.go`,
`internal/provider/acpagent/automode_test.go` (and any test that constructs
`permWaiter` literals)

### C.1 Why the waiter must grow

Today:

```go
type permWaiter struct {
    ch       chan permResult
    resolved bool
}
```

`awaitDecision` registers that waiter and emits a `permission_request` whose
options live **only on the event**. The sweep cannot invent an option id without
storing it. Codex stores `tool`/`detail`/`rpcID` on `pendingPerm`; we need the
ACP equivalent.

### C.2 Extend `permWaiter`

```go
type permWaiter struct {
    ch       chan permResult
    resolved bool
    // allowOptionID is the option auto-approve would select (precomputed when
    // the request was emitted). Empty means the sheet had no options — sweep
    // cancels rather than inventing an id.
    allowOptionID string
    // Audit fields for noteAutoApproval when this waiter is swept on arm.
    toolName string
    detail   string
}
```

### C.3 Precompute at registration (`awaitDecision`)

When building the waiter from `req event.Event`:

```go
w := &permWaiter{
    ch:            make(chan permResult, 1),
    allowOptionID: pickAllowOptionID(req.Options),
    toolName:      firstNonEmpty(req.ToolName, "permission"),
    detail:        req.Text,
}
```

Add `pickAllowOptionID([]event.PermissionOption) string` next to `autoAllow`:
same preference rules (`allow`/`approve` in kind or name, else first option's
id, else `""`). Keep `autoAllow` for the ACP `RequestPermission` hot path
(works on `acp.PermissionOption`); do not force a shared type if conversion is
noisier than a 15-line twin.

### C.4 `sweepPendingPermissions`

Call from `armAutoMode` **after** `emitArmedMode` (chip updates before sheets
disappear).

Mirror `Cancel`'s snapshot discipline (do **not** hold `s.mu` while sending on
channels):

```go
// sweepPendingPermissions answers every outstanding permission_request as
// allowed. Arming auto to unblock a stuck turn is the common reason to reach
// for it; without this the sheet stays up (MADR 0044 D4.5, MADR 0053 F2).
// Questions (s.questions) are intentionally not touched — same boundary as
// AlwaysApprove vs ask_user_question.
func (s *session) sweepPendingPermissions() {
    s.mu.Lock()
    pending := s.pending
    s.pending = make(map[string]*permWaiter)
    s.mu.Unlock()
    if len(pending) == 0 {
        return
    }
    for id, w := range pending {
        if w == nil {
            continue
        }
        opt := w.allowOptionID
        if opt == "" {
            select {
            case w.ch <- permResult{cancelled: true}:
            default:
            }
            continue
        }
        select {
        case w.ch <- permResult{optionID: opt}:
        default:
        }
        // awaitDecision's channel path emits permission_resolved; we only add
        // the auto-approve audit line that the hot path would have written.
        s.noteAutoApprovalFromSweep(w.toolName, w.detail)
        _ = id // usable in a Debug log if useful
    }
}
```

**Do not** set `w.resolved` yourself if you clear the map the Cancel way —
`awaitDecision` is blocked on `<-w.ch` and runs `applyResult`, which emits
`permission_resolved`. Setting `resolved` without removing from the map races
with `RespondPermission`; snapshot-and-replace avoids that.

**`noteAutoApprovalFromSweep`:** either a thin helper that appends an
`ApprovalItem` with the stored name/detail (preferred — no fake
`acp.RequestPermissionRequest`), or reuse `noteAutoApproval` by synthesizing
minimal params. Match MADR 0051 card semantics (`approval_summary` running).

**Do not** sweep `s.questions`.

### C.5 Wire into `armAutoMode`

```go
s.emitArmedMode()
// Phase B log here if co-landed
s.sweepPendingPermissions()
return nil
```

Only on the success path (after agent RPC ok / skipped).

### C.6 Tests

#### `TestArmingAutoSweepsPendingPermission`

```text
Given: session with synthesizeAuto; one permWaiter in s.pending with
       allowOptionID="allow-1", buffered ch; a goroutine blocked on <-ch
       OR simply read ch after SetMode
When:  SetMode(ctx, "auto") with fake agent ok
Then:
  - ch receives permResult{optionID: "allow-1"}
  - s.pending is empty
  - an approval_summary event is present (audit)
  - TypeMode current=auto still emitted (Phase A)
```

#### `TestArmingAutoDoesNotTouchQuestions`

```text
Given: a questionWaiter in s.questions
When:  SetMode(ctx, "auto")
Then:  questions map still has the entry; channel empty
```

#### Update constructors

Any test that builds `&permWaiter{ch: ch}` keeps compiling (new fields zero).
Sweep tests must set `allowOptionID`.

### C.7 Acceptance

```bash
go test ./internal/provider/acpagent/ -count=1 -run 'Auto|Sweep|Arming'
make pre-add-check FILES="internal/provider/acpagent/session.go internal/provider/acpagent/automode_test.go"
```

---

## Phase D — Live gate: arm from default (F4)

**File:** `internal/provider/grok/live_mode_test.go`

### D.1 New test: `TestLiveGrokAutoModeArmsFromDefault`

Build tag `live_grok`. Do **not** call `SetMode("plan")` first.

```text
Start grok session (AlwaysApprove off, PermissionMode empty/default)
Drain create-time session_mode (optional assert modes include auto)
SetMode(ctx, "auto")
Within 30s: receive TypeMode with CurrentModeID == "auto"
```

This is the test that would have blocked F1 from shipping. Keep
`TestLiveGrokAutoModeArmsWithoutSendingAuto` (plan→auto) as the leave-plan
coverage.

### D.2 Optional discrimination (if cheap)

If `TestLiveGrokAutoDiscriminationPair` already covers armed vs unarmed prompts,
do not duplicate. If arming from default still leaves prompts in some grok
versions, extend D.1 with a short write prompt — only if the existing live
automode test does not already cover default-start.

### D.3 Acceptance

```bash
go test -tags live_grok ./internal/provider/grok/ -run 'AutoMode|PlanMode' -count=1
```

Requires grok on PATH (host measured 0.2.114). Skip if not ready — same as
other live tests.

---

## Phase E — Optional polish (F5, F6)

Ship only if A–D are green and the extra diff is still cheap. Otherwise file as
follow-up in the MADR and stop.

### E.1 Display name

In `syntheticAutoMode`:

```go
Name: "Auto", // was autoModeID ("auto")
```

Id stays `"auto"`. Mobile chip and menu use `name`. Update any unit assertion
that compares `Name == "auto"` (grep `automode_test` / `grok_test`).

### E.2 Case-insensitive static validation

In `SetMode` synthetic branch:

```go
return strings.EqualFold(m.ID, modeID)
```

Add a one-line test: `SetMode(ctx, "PLAN")` either succeeds (if you normalize)
or still fails consistently — **prefer EqualFold match and forward the
canonical static id** (`m.ID` from the list) to the agent so grok sees `plan`
not `PLAN`.

```go
var canonical string
ok := slices.ContainsFunc(s.staticModes, func(m event.SessionMode) bool {
    if strings.EqualFold(m.ID, modeID) {
        canonical = m.ID
        return true
    }
    return false
})
if synthetic && !ok {
    return fmt.Errorf("unknown mode %q", modeID)
}
// use canonical in SetSessionMode when non-empty
```

### E.3 Acceptance

```bash
go test ./internal/provider/acpagent/ ./internal/provider/grok/ -count=1
```

---

## Phase F — Docs and verification

### F.1 Update MADR 0053

- Status → **Implemented** (date + short commit list).
- Mark F1–F4 (and E items if done) closed in §9 table.
- Point Status at this plan.

### F.2 Full gate

```bash
make pre-add-check FILES="$(git diff --name-only --diff-filter=ACM | rg '\.go$' || true)"
go test ./internal/provider/acpagent/ ./internal/provider/grok/ ./internal/session/ -count=1
# if grok available:
go test -tags live_grok ./internal/provider/grok/ -count=1
make preflight   # if that is the project-wide gate you use before release
```

### F.3 Manual smoke (phone)

1. Install the built `mcremote`, restart the user unit.
2. New grok session; chip shows Build.
3. Mode menu → auto → confirm **Turn on**.
4. Chip becomes error-tinted bolt + **Auto**/**auto**.
5. `journalctl --user -u mcremote -n 50 | rg auto-approve` shows arm line.
6. Trigger a tool that previously prompted → no sheet; optional
   `approval_summary` card.
7. Switch to Plan → chip plan tint; prompts return on next tool.
8. (Optional) With a permission sheet already open, arm auto → sheet clears.

### F.4 Done means

- F1 fixed and locked by unit + live default→auto tests.
- F2 fixed if Phase C landed.
- F3 visible in journal.
- No mobile/protocol change.
- MADR 0053 status Implemented.

---

## 3. Exact `armAutoMode` end-state (Phases A+B+C combined)

Reference implementation for the implementer (merge carefully with live code):

```go
func (s *session) armAutoMode(ctx context.Context, agentID string) error {
    target := s.normalModeID()
    s.mu.Lock()
    s.autoApprove = true
    s.mu.Unlock()

    if target != "" {
        if _, err := s.conn.SetSessionMode(ctx, acp.SetSessionModeRequest{
            SessionId: acp.SessionId(agentID),
            ModeId:    acp.SessionModeId(target),
        }); err != nil {
            s.mu.Lock()
            s.autoApprove = false
            s.mu.Unlock()
            return err
        }
    }

    s.emitArmedMode()
    s.log.Warn("acp auto-approve armed",
        slog.String("session_id", s.localID),
        slog.String("agent_session_id", agentID),
        slog.String("agent_mode", target),
    )
    s.sweepPendingPermissions()
    return nil
}
```

---

## 4. Risk register

| Risk | Mitigation |
|---|---|
| Double `session_mode` (daemon emit + agent notify) confuses a client | Protocol allows current-only updates; mobile/manager merge is idempotent on same id. Existing plan→auto already can double after A. |
| Sweep cancels when `allowOptionID` empty | Prefer first option in `pickAllowOptionID`; only cancel if options were empty (agent bug). |
| Sweep races `RespondPermission` | Snapshot-and-replace `pending` like `Cancel`; sole sender to each channel. |
| Sweep races timeout branch in `awaitDecision` | Same as Cancel: map cleared so `claim()` fails; channel result wins. |
| `noteAutoApproval` from sweep double-counts with channel path | Channel path does **not** call `noteAutoApproval` (only the hot auto path does). Sweep is the only audit writer for pre-existing sheets. |
| Live grok starts sending `current_mode_update` for same-mode set | Still fine — rewrite keeps `auto`. |
| Operator confuses session auto with `/always-approve` | Out of scope; document only. |

---

## 5. Commit sketch

| Commit | Contents |
|---|---|
| 1 | Phase A (+ B if combined): `armAutoMode` emit, `emitArmedMode`, unit tests, logs |
| 2 | Phase C: `permWaiter` fields, `pickAllowOptionID`, `sweepPendingPermissions`, tests |
| 3 | Phase D: live default→auto test |
| 4 | Phase E (optional): name + EqualFold |
| 5 | Phase F: MADR 0053 status |

Each Go commit: `make pre-add-check FILES=…` then `git add` then `git commit`
(no `-m`).

---

## 6. Checklist (print and tick)

- [x] A.1 `emitArmedMode` always called after successful arm
- [x] A.1 failed agent switch still disarms and does not emit
- [x] A.3 `TestArmingAutoFromDefaultEmitsCurrentMode` (unit)
- [x] A.3 existing auto tests still green
- [x] B.1 WARN log on arm
- [x] B.2 INFO log only when leaving auto
- [x] C.2 `permWaiter` carries allow option + audit fields
- [x] C.3 `awaitDecision` fills those fields
- [x] C.4 sweep does not touch `questions`
- [x] C.6 sweep unit test green
- [x] D.1 live default→auto green on this host (`live_grok`)
- [x] F.1 MADR status Implemented
- [ ] F.3 phone smoke: Build → auto → bolt chip (operator; after deploy)

---

## 7. Out of plan (do not silently expand scope)

- Mobile optimistic `currentModeId` on set_mode ok
- Filtering grok's agent `/always-approve` from `available_commands`
- Manager-side re-emit if provider forgets (band-aid; fix the provider)
- Tracking last agent mode id to skip `SetSessionMode` (MADR D2)
- F7 first `remote_commands` before modes
- Any change to codex/opencode/goose

If a fix forces a mobile change, stop and amend MADR 0053 before coding it.

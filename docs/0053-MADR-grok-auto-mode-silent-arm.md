# MADR 0053: Grok auto mode — silent arm, missing chip, mode-path gaps

<!-- markdownlint-disable MD013 MD060 -->

- **Status**: **Findings** (2026-07-30). Assessment only — no code change in this
  document. Root cause is grounded in source, session history on this host, and
  journald. Fix order is proposed in §6; **actionable build order** is the
  companion plan.
- **Date**: 2026-07-30
- **Scope**: `internal/provider/acpagent` (synthetic auto arm path),
  `internal/provider/grok` (opt-in consumer), mobile mode chip consumption of
  `session_mode`, observability parity with codex/opencode. No protocol change
  required for the primary fix.
- **Companion plan**:
  [0053-PLAN-grok-auto-mode-silent-arm.md](./0053-PLAN-grok-auto-mode-silent-arm.md)
- **Related**:
  - [MADR 0049](./0049-MADR-grok-auto-mode.md) — grok synthetic `auto`; D1/D4
  - [MADR 0049 plan](./0049-PLAN-grok-auto-mode.md) — live-gate note that already
    fixed one D4 overwrite; the silent-arm case was never covered
  - [MADR 0044](./0044-MADR-auto-approve-modes.md) — `Dangerous`, confirmation
    gate, codex/opencode auto
  - [MADR 0047](./0047-MADR-codex-default-mode.md) — `resolveDisplayedMode`,
    normal mode first
  - [MADR 0051](./0051-MADR-auto-approve-chat-noise.md) — approval_summary,
    finishApprovals on leave-auto
  - [protocol-v1.md](./protocol-v1.md) — `session_mode`, `session.set_mode`
- **Runtime evidence** (this host, 2026-07-30):
  - `mcremote 0.5.3.3` (`8b7f3ba`), journald unit `mcremote.service`
  - Grok sessions under `~/.local/share/mcremote/sessions/`
  - Config: `providers.grok.always_approve: false`, `permission_mode: ""`
    (`~/.config/mcremote/config.yaml`)

---

## 1. Reported symptom

Setting Grok mode to **auto** on the phone:

1. shows the dangerous-mode confirmation ("Run without approvals?"),
2. after **Turn on**, never shows the armed state — the app-bar chip stays
   **Build**, the bolt / error-tinted "graphic button" never appears,
3. (subjectively) the session does not "enter" auto mode.

Codex and OpenCode auto on the same phone and daemon work: confirmation → chip
becomes `auto` with the bolt tint, journal logs `… auto-approve armed`.

---

## 2. What works (so the feature is not "missing")

| Layer | Fact | Evidence |
|---|---|---|
| Advertisement | Grok sessions advertise `default`, `plan`, `auto` with `auto.dangerous=true` | Session `6d1814da…` history seq 6 `session_mode`: modes include `auto` / `dangerous: true`, `current_mode_id: "default"` |
| Opt-in | `SynthesizeAutoMode: true` on the grok Spec | `internal/provider/grok/grok.go:75` |
| Confirmation UI | Mobile gates on `SessionMode.dangerous`, not the id string | `apps/mobile/lib/features/chat/chat_screen.dart` `_ModeSelector.onSelected` → `_confirmDangerousMode` |
| RPC success shape | `session.set_mode` returns a bare `ok` envelope; UI does **not** optimistically update the chip | `internal/ws/server.go` `handleSessionSetMode`; `mcremote_client.dart` `setMode` |
| Chip rendering | Dangerous chip is driven only by the **resolved current** mode's `dangerous` flag | `_ModeChip` + `resolveDisplayedMode(modes, currentModeId)` |
| Interception (when armed) | `RequestPermission` short-circuits on `s.autoApprove` | `acpagent/session.go` `RequestPermission` |
| Peer providers | Codex/OpenCode arm emits `session_mode` with `current_mode_id=auto` and a WARN log | Codex session `904a1914…` n_mode=2; journal `codex auto-approve armed` / `opencode auto-approve armed` |

So the user reaches the confirmation dialog because advertisement and the
dangerous flag work. The failure is **after** confirm: the client never learns
that current mode is `auto`.

---

## 3. Root cause — silent arm when already in the normal mode

### 3.1 Code path

`acpagent.session.armAutoMode` (`internal/provider/acpagent/session.go`):

```go
func (s *session) armAutoMode(ctx context.Context, agentID string) error {
    target := s.normalModeID()   // "default" for grok
    s.mu.Lock()
    s.autoApprove = true
    s.mu.Unlock()

    if target != "" {
        if _, err := s.conn.SetSessionMode(ctx, acp.SetSessionModeRequest{
            SessionId: acp.SessionId(agentID),
            ModeId:    acp.SessionModeId(target),
        }); err != nil {
            // disarm + return err
            ...
        }
    }
    // The agent confirms a real switch with current_mode_update, which reports
    // `auto` through reportedModeID. When there was no switch to make, nothing
    // would announce the arm, so say so here.
    if target == "" {
        s.emitModesOrStatic(nil)
    }
    return nil
}
```

The design intent (MADR 0049 D1 / D4, plan §D notes):

1. set the per-session `autoApprove` flag,
2. put the agent into its **normal** ACP mode (`default`), never send `auto` on
   the wire,
3. rely on the agent's `current_mode_update` for the confirming `session_mode`
   event, rewritten through `reportedModeID` so the chip shows `auto`.

The **only** daemon-initiated emit is when `target == ""` (no normal mode at
all). For grok, `target` is always `"default"`, so that branch never runs.

### 3.2 Why the common UI path is silent

Grok sessions start in `default` (advertised `current_mode_id: "default"`). The
user opens the mode menu and picks `auto` while already on Build:

| Step | What happens |
|---|---|
| Confirm | Mobile calls `session.set_mode` with `mode_id=auto` |
| `SetMode` | Routes to `armAutoMode` |
| Flag | `autoApprove = true` |
| Agent RPC | `session/set_mode` with `ModeId=default` — **no-op** when already default |
| Agent notify | Grok does **not** emit `current_mode_update` for a no-change switch |
| Daemon emit | Skipped (`target != ""`) |
| Client | No second `session_mode` event → chip stays **Build** |
| RPC reply | `ok` → no error toast |

Result matching the report:

- confirmation dismissed successfully,
- graphic bolt chip never appears,
- product appears broken even if (see §3.4) enforcement may already be on.

### 3.3 Why the live gate did not catch it

`TestLiveGrokAutoModeArmsWithoutSendingAuto`
(`internal/provider/grok/live_mode_test.go`) **parks the session in plan first**:

```go
// Park it in plan first: arming auto has to leave plan, or the session
// would be prompts-off and edits-off at once.
if err := ms.SetMode(ctx, "plan"); err != nil { ... }
if err := ms.SetMode(ctx, "auto"); err != nil { ... }
// waits for session_mode with CurrentModeID == "auto"
```

`plan → auto` forces a real agent mode change (`plan` → `default`), so
`current_mode_update` fires and `reportedModeID` rewrites it to `auto`. That is
exactly the uncommon path. The default (already-normal) path was never asserted.

Unit fakes (`automode_test.go`) assert:

- agent never receives mode id `auto`,
- interception when `autoApprove` is forced true,
- current-mode reporting when emit is driven by hand,

but **do not** assert that `SetMode(ctx, "auto")` from an already-default session
emits a `session_mode` with `CurrentModeID == "auto"`.

### 3.4 Lying chip (D4 inverted)

When the silent arm succeeds:

- daemon `autoApprove` is **true** → future `RequestPermission` auto-allows,
- client `currentModeId` remains **`default`** → chip says Build, not auto.

MADR 0049 D4 was written against the opposite lie (chip says auto while the
daemon still prompts). The silent arm produces the dual: **daemon enforces auto
while the chip denies it**. Both violate the honesty contract.

`/mode` and manager `currentModeID` are updated only from `session_mode` events
(`manager.go` event loop), so slash-command status is wrong after a silent arm
as well.

### 3.5 Host history corroboration

Every grok session on this host that advertises `auto` has **exactly one**
`session_mode` event (create-time), never a follow-up with `current_mode_id=auto`:

| Session (prefix) | Created | `n` mode events | ever `current=auto` | advertises auto |
|---|---|---|---|---|
| `6d1814da` | 2026-07-30 | 1 | no | yes |
| `b534129c` | 2026-07-30 | 1 | no | yes |
| earlier grok | pre-0049 | 1 | no | no |

Codex/OpenCode sessions that armed auto always show **two** mode events (create
+ switch to `auto`), e.g. codex `904a1914`, opencode `6cb1c064`.

Journald since 2026-07-28:

- many `codex auto-approve armed` / `opencode auto-approve armed` lines,
- **zero** equivalent lines for grok/acpagent (no log call exists — §4.1).

No `session_set_mode_failed` / `unknown mode` errors for grok auto in the same
window: the RPC is succeeding; the client simply never gets a mode update.

---

## 4. Additional gaps (modes stack)

These are secondary to the silent arm but belong in the same modes audit.

### 4.1 No arm observability for acpagent / grok

| Provider | On arm |
|---|---|
| Codex | `log.Warn("codex auto-approve armed", …)` then emit `TypeMode` |
| OpenCode | `log.Warn("opencode auto-approve armed", …)`; httpagent always emits `TypeMode` with the returned id |
| acpagent / grok | **no log**, emit only via agent notification or the `target==""` branch |

Operators cannot prove a phone arm from journald. The absence of a log is
indistinguishable from "user never armed".

### 4.2 Emit contract inconsistent across providers

| Path | Who emits `session_mode` after `SetMode` |
|---|---|
| Codex | Provider itself, always, with `CurrentModeID = m.mode.ID` |
| OpenCode via httpagent | httpagent always emits with dialect-returned `currentID` (`httpagent/session.go` `SetMode`) |
| acpagent real modes | Agent `current_mode_update` only |
| acpagent synthetic auto | Agent update only **if** normal-mode RPC causes a change; else silent |

Codex/OpenCode never depend on an agent notification for the **synthetic** arm.
acpagent does — that is the design that fails when the agent is already normal.

### 4.3 No pending-permission sweep on arm (parity hole)

MADR 0044 D4.5 / codex+opencode: arming auto must answer **already-open**
permission requests so the user is not stuck on a sheet after "Turn on".

- OpenCode: `sweepPendingPermissions` from `SetMode(auto)`
- Codex: `sweepPendingApprovals` from `SetMode(auto)`
- acpagent: **nothing** in `armAutoMode` — open `pending` waiters stay until the
  user answers, Cancel, Close, or timeout

Symptom if the user arms auto *because* a prompt is up: confirmation succeeds,
chip may still not update (§3), and the sheet can remain.

### 4.4 Case-sensitive validation for non-auto synthetic modes

```go
if synthetic && !slices.ContainsFunc(s.staticModes,
    func(m event.SessionMode) bool { return m.ID == modeID }) {
    return fmt.Errorf("unknown mode %q", modeID)
}
```

`auto` uses `EqualFold`; real static ids use exact match. Mobile always sends
lowercase ids from the list, so this is latent, not the current bug.

### 4.5 Synthetic display name is bare `auto`

```go
func syntheticAutoMode() event.SessionMode {
    return event.SessionMode{
        ID: autoModeID, Name: autoModeID, // "auto"
        Description: "Auto-approve — no prompts",
        Dangerous: true,
    }
}
```

Grok's real modes use human names (`Build`, `Plan`). The armed chip would show
lowercase `auto` even after a successful emit. Cosmetic; codex/opencode also use
id-like names, but Build/Plan makes the inconsistency visible on grok.

### 4.6 Early `remote_commands` race (modes not yet known)

On create, history for `6d1814da` shows `remote_commands` **before**
`session_mode` advertising `/plan` and `/mode` as unavailable
(`"this agent has no plan mode"` / `"doesn't expose switchable modes"`), then a
later `remote_commands` after modes arrive with both available.

Manager re-resolves on mode events, so the final state is correct. A client that
cached the first list briefly offers a wrong command surface. Separate from the
chip bug; worth hardening if slash UI ever freezes the first snapshot.

### 4.7 Mobile has no local fallback after successful `setMode`

By protocol design the chip is event-driven. That is correct **if** every
provider emits. After §3 is fixed, mobile need not change. Optional hardening:
on `ok` from `session.set_mode`, optimistically set `currentModeId` (or refetch)
so a future silent provider cannot strand the UI again. Trade-off: optimistic
state can diverge if the daemon later rejects asynchronously (not today's WS
contract — set_mode is sync ok/error).

### 4.8 Process-wide `permission_mode` vs session auto still dual (documented)

MADR 0049 D6: `providers.grok.permission_mode` remains a launch flag. Config on
this host leaves it empty. Not the current regression; still a support pitfall
if an operator sets `bypassPermissions` and wonders why the mode chip is
"advisory".

### 4.9 Grok agent slash `/always-approve` vs session mode `auto`

Grok's ACP `available_commands` still advertises `always-approve` (process/agent
tooling). That is orthogonal to the daemon synthetic mode. Users may confuse the
two. Out of scope for the silent-arm fix; product copy / filtering is a separate
decision.

---

## 5. End-to-end wire (expected vs actual)

```text
Phone                         Daemon                         Grok ACP
  |                              |                              |
  | session.set_mode auto        |                              |
  |----------------------------->|                              |
  |                              | autoApprove=true             |
  |                              | session/set_mode default     |
  |                              |----------------------------->|
  |                              |  (already default: no-op)    |
  |                              |<-----------------------------|
  |                              |  no current_mode_update      |
  |                              |  *** no session_mode emit ***|
  | ok                           |                              |
  |<-----------------------------|                              |
  | chip still Build             |                              |
```

Expected after fix:

```text
  |                              | autoApprove=true             |
  |                              | session/set_mode default     |
  |                              |  (skip or no-op OK)          |
  |                              | emit session_mode            |
  |                              |   current_mode_id=auto       |
  | session_mode auto            |  (+ WARN log)                |
  |<-----------------------------|                              |
  | ok                           |                              |
  | chip: bolt + auto            |                              |
```

---

## 6. Decisions (proposed — not yet implemented)

### D1 — Always announce a successful arm

After `autoApprove` is set and any normal-mode switch succeeds (or is skipped
because the agent is already normal), the daemon **must** emit a `session_mode`
event with `CurrentModeID = auto` (via `reportedModeID` or a direct emit of the
current-only form used by codex/httpagent).

Do **not** depend on the agent confirming a no-op switch.

Rejected alternatives:

- Optimistic mobile-only update without daemon emit — leaves `/mode`, history,
  reconnect, and second clients wrong.
- Mapping session auto onto `--permission-mode auto` — process-wide, MADR 0049
  D1 rejected.

### D2 — Prefer skip redundant `SetSessionMode` when already normal

Optional optimization once the provider tracks the last known agent mode id:
if current agent mode is already `normalModeID()`, skip the RPC and only emit.
If tracking is incomplete, keeping the RPC is fine **as long as D1 always
emits**.

### D3 — Observability parity

Log at WARN on arm (and INFO on disarm/mode switch), matching codex/opencode
message shape (`… auto-approve armed`, session ids). Required for journal
diagnosis of the next mode regression.

### D4 — Sweep pending permissions on arm

Answer every waiter in `s.pending` with `autoAllow` (and audit via
`noteAutoApproval` / the existing approval_summary path) when arming, matching
MADR 0044 D4.5. Leave open questions (`s.questions`) alone — same rule as
AlwaysApprove vs ask_user_question (extensions.go).

### D5 — Tests that fail without D1

1. **Unit (fake agent):** session already on `default`; `SetMode("auto")`; assert
   a `session_mode` (or TypeMode) with `CurrentModeID == "auto"` is emitted
   **even when** the fake does not send `current_mode_update`.
2. **Unit:** fake sends no mode notifications at all after `set_mode default`;
   arm still announces auto.
3. **Live (`live_grok`):** arm auto **from default** (no plan detour); assert
   `CurrentModeID == "auto"` within timeout. Keep the existing plan→auto test.
4. Optional: pending permission open, then arm → waiter resolved without client
   answer.

### D6 — Out of scope for the primary fix

- Mobile optimistic update (optional hardening only).
- Renaming synthetic `Name` to `Auto`.
- Collapsing grok `/always-approve` command advertising.
- `permission_mode` process flag semantics.
- Early `remote_commands` race (track separately if slash UI freezes first list).

---

## 7. Consequences

**After D1–D5:** the default phone path (Build → confirm auto) shows the bolt
chip; history records `current_mode_id=auto`; journal shows arm; open permission
sheets clear. Codex/OpenCode behaviour is unchanged. No protocol bump.

**Risk if only the live plan→auto test is kept:** the original silent path
regresses again. D5 item 3 is the non-negotiable live gate.

**Honesty:** D1 restores MADR 0049 D4 for the common path: chip and enforcement
move together.

---

## 8. Source map (pin these when fixing)

| Area | Path / symbol |
|---|---|
| Silent arm | `acpagent/session.go` `armAutoMode` |
| Mode rewrite on agent notify | `CurrentModeUpdate` → `reportedModeID` |
| Advertisement | `advertisedModes`, `emitModesOrStatic`, `syntheticAutoMode` |
| Grok opt-in | `grok/grok.go` `SynthesizeAutoMode: true` |
| Manager mode mirror | `session/manager.go` TypeMode handler → `currentModeID` |
| WS | `ws/server.go` `handleSessionSetMode` → bare ok |
| Mobile chip | `chat_screen.dart` `_ModeSelector`, `_ModeChip`, `_confirmDangerousMode` |
| Mobile resolve | `chat_helpers.dart` `resolveDisplayedMode` |
| Transcript apply | `transcript_reducer.dart` `session_mode` |
| Good peer emit | `codex/session.go` `SetMode`; `httpagent/session.go` `SetMode` |
| Live gap | `grok/live_mode_test.go` `TestLiveGrokAutoModeArmsWithoutSendingAuto` |

---

## 9. Summary table

| ID | Severity | Finding | Status |
|---|---|---|---|
| F1 | **P0** | `armAutoMode` does not emit `session_mode` when already on normal mode; chip never shows auto | Open — root cause of report |
| F2 | P1 | No pending-permission sweep on acpagent arm | Open — parity with 0044 D4.5 |
| F3 | P2 | No WARN log on grok/acpagent arm | Open — ops blind spot |
| F4 | P1 | Live + unit tests only cover plan→auto or forced flag, not default→auto emit | Open — allowed F1 to ship |
| F5 | P3 | Synthetic name `"auto"` vs `Build`/`Plan` | Cosmetic |
| F6 | P3 | Case-sensitive static mode validation | Latent |
| F7 | P3 | First `remote_commands` before modes advertises modes unavailable | Minor race |
| F8 | info | Dual controls: session `auto` vs launch `permission_mode` / agent `/always-approve` | Documented (0049 D6) |

**Bottom line:** Grok auto is implemented and advertised; the confirmation UI
works; the arm flag may even engage. The product failure is **missing
confirmation of current mode to the client** on the path every real user takes
(already on Build). Fix emit-on-arm (D1), log it (D3), sweep pending (D4), and
lock it with a default→auto test (D5).

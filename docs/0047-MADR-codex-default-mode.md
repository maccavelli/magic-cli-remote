# MADR 0047: Codex default mode, create-time selection, and auto sandbox

- **Status**: Implemented (2026-07-29)
- **Date**: 2026-07-29
- **Deciders**: Project Owner (product surface); Implementer (daemon/providers/mobile)
- **Related**:
  - [MADR 0044](./0044-MADR-auto-approve-modes.md) — auto-approve as a session
    mode; codex mode table introduced in D5
  - [MADR 0022](./0022-MADR-plan-mode-parity.md) — session modes end to end
  - [MADR 0028](./0028-MADR-codex-provider.md) — codex app-server transport
  - [protocol-v1.md](./protocol-v1.md) — `session_mode`, `session.set_mode`
- **Companion plan**:
  [0047-plan-codex-default-mode.md](./0047-plan-codex-default-mode.md)
- **Evidence**: code inspection of `internal/provider/codex/mode.go`,
  `session.go` seed/create path, `apps/mobile` `_ModeSelector`; live probes
  against **codex-cli 0.145.0** app-server (2026-07-29) for effective sandbox
  when only `approvalPolicy: "never"` is set.

---

## 1. Problem

Three related defects on the Android (and every Flutter) client and the
codex provider:

1. **The codex mode menu only offers `read-only` and `auto`** (plus
   opt-in `full-access`). The ordinary working mode — *edit the
   workspace, still ask before risky actions* — is entirely missing.
2. **A new session's mode chip selects the first entry in the advertised
   list**, not the provider's normal permissions mode. On codex that is
   `read-only`.
3. **Auto-approve does not reliably set workspace-write.** The mode
   *table* pairs `auto` with `never` + `workspace-write`, and
   `SetMode(auto)` updates both fields in memory, but several real paths
   arm "no prompts" **without** elevating the OS sandbox. On untrusted
   projects the engine then stays **`readOnly`**: permissions may be
   auto-answered while every edit still fails at the sandbox.

Users who want normal interactive coding cannot choose it. Users who open
a fresh codex session believe they are in read-only when the engine may
disagree. Users who arm auto expect unattended *edits in the workspace*
and can get unattended *refusals to write*.

---

## 2. Evidence

### 2.1 Missing normal mode is a design gap (MADR 0044 D5)

`internal/provider/codex/mode.go` as shipped:

| mode id | `approvalPolicy` | sandbox | `dangerous` | default menu |
|---|---|---|---|---|
| `read-only` | `on-request` | `read-only` | no | yes |
| `auto` | `never` | `workspace-write` | **yes** | yes |
| `full-access` | `never` | `danger-full-access` | **yes** | gated |

There is **no** row for codex's normal interactive pair:

| (missing) | `on-request` | `workspace-write` | no | — |

MADR 0044 D5 documented that pair as codex's own "Auto" preset and
correctly rejected it as the meaning of *auto-approve*. It never added
it as a **selectable normal mode**. The menu is only "locked down" and
"unattended".

Pinned by test today:

```go
// mode_test.go — want [read-only auto]
if strings.Join(got, ",") != "read-only,auto" { … }
```

### 2.2 Create-time seed leaves the session unmatched

`newSession` seeds:

```go
approvalPolicy: cfg.ApprovalPolicy, // default ""
sandboxMode:    cfg.SandboxMode,    // default ""
autoApprove:    cfg.ApprovalPolicy == "never",
```

Empty config omits both fields on `thread/start` / `turn/start`
("inherit engine config.toml"). `modeIDFor` only matches exact pairs, so
`emitModes` sends `current_mode_id: ""`.

### 2.3 Mobile falls back to the first list entry

`apps/mobile/.../chat_screen.dart` — `_ModeSelector`:

```dart
final current = modes.firstWhere(
  (m) => m.id == currentModeId,
  orElse: () => modes.first,   // ← invents a selection
);
```

When `currentModeId` is empty/unknown, the chip and checked item claim
**`modes.first`**. For codex that is always `read-only`.

| layer | truth on a new empty-config codex session |
|---|---|
| engine | own defaults (often `readOnly` on untrusted cwds — §2.5) |
| daemon `current_mode_id` | `""` |
| phone chip | **`read-only`** (lie by list order) |

### 2.4 Cross-provider create defaults

| provider | create-time current | ok? |
|---|---|---|
| OpenCode | `build` (or first agent) | yes |
| grok | `DefaultModeID: "default"` | yes |
| goose | `DefaultModeID: "auto"` (intentional) | yes |
| **codex** | `""` when config empty/unmatched | **no** |

Any future unmatched id re-triggers the mobile footgun. Fix both layers:
provider always sends a real current id; client never invents from order.

### 2.5 Auto without workspace-write (live, codex 0.145.0)

MADR 0044 D5 *specified* `auto = never + workspace-write`. The table and
`SetMode` assignment match. The failure is paths that never put **both**
on the wire, and engine defaults when only `never` is set.

Live `thread/start` on an **untrusted** cwd (`/var/tmp/…`, not in
`~/.codex/config.toml` `[projects]`):

| params sent | effective approval | effective sandbox |
|---|---|---|
| `approvalPolicy: "never"` only | `never` | **`readOnly`** |
| `never` + `sandbox: "workspace-write"` | `never` | **`workspaceWrite`** |
| (none) | `on-request` | **`readOnly`** |

Trusted paths are more forgiving; remote sessions often use arbitrary or
temp cwds, so the untrusted behaviour is the one that matters.

Daemon paths that arm "no prompts" without sandbox elevation:

| path | effect |
|---|---|
| `Config{ApprovalPolicy: "never"}` + empty `SandboxMode` | `autoApprove=true`, sandbox `""`; wire sends `never`, **omits** sandbox → untrusted **readOnly** |
| `providers.codex.always_approve` | interception only; sandbox untouched |
| Empty config create | both omitted → untrusted **readOnly** |
| `SetMode(auto)` then next `turn/start` | **correct**: memory is both fields; pipe capture shows `sandboxPolicy.type = workspaceWrite` |

So the mode-switch happy path can send workspace-write on the **next**
turn, but the product still fails when:

- the user never switched (empty create + untrusted = readOnly; chip
  lies as read-only-selected);
- config only set `approval_policy: never`;
- auto was armed mid-turn: approval sweep is immediate, sandbox only
  changes on the **next** `turn/start`;
- "auto-approve worked" (daemon answered a sheet) is confused with
  "workspace-write is on" — different layers (D6 interception vs engine
  sandbox).

**Contract:** any path that means "auto" must put **both** `never` and
`workspace-write` on every policy wire (`thread/start`, `thread/resume`,
`turn/start`). Partial "never alone" is a bug.

---

## 3. Decision drivers

1. Normal coding is a first-class mode (not only extremes).
2. Create lands on that mode — not "first in the array".
3. The chip never invents a selection from list order.
4. Auto means never **and** workspace-write on the wire, every time.
5. Reuse existing mode machinery (no new protocol messages).
6. Auto stays dangerous, confirm-to-arm, non-default (MADR 0044).
7. Config is authoritative for a recognised *pair*; `never`+empty must
   not silently run auto under readOnly.

---

## 4. Decision

### D1 — Add a `default` mode to the codex table

Advertise first (order is load-bearing for residual first-item fallbacks):

| mode id | display | `approvalPolicy` | sandbox | `dangerous` |
|---|---|---|---|---|
| **`default`** | `default` | **`on-request`** | **`workspace-write`** | no |
| `read-only` | `read-only` | `on-request` | `read-only` | no |
| `auto` | `auto` | `never` | `workspace-write` | yes |
| `full-access` | `full access` | `never` | `danger-full-access` | yes (gated) |

**Why id `default`:** matches grok; `session.defaultMode` already prefers
`"default"` then `"build"`. Description: *"Edit workspace; ask before
shell, network, and out-of-tree writes"*.

### D2 — Seed every new codex session

On `newSession` / create (`seedPolicy` helper):

1. If config **exactly matches** an advertised mode → seed that pair.
2. If approval is `never` and sandbox is **empty** → seed the **`auto`
   pair** (`never` + `workspace-write`), not `default`. Log Warn. A
   config that asked for "never ask" must not land on untrusted
   **readOnly** (§2.5).
3. Otherwise (empty config or other unmatched partial) → seed
   **`default`** (`on-request` + `workspace-write`).
4. Always send the seeded values on `thread/start` / `resume` /
   `turn/start`. Do not omit and hope the engine agrees with the chip.

Resume (MADR 0044 D8): phone-chosen auto still does not persist. Empty
resume → `default`. Config-matched auto (or D2.2 repair) may start as
auto — operator-driven only.

### D3 — Cross-provider create contract

Every provider that emits a non-empty `modes` list on create/resume
**must** set `current_mode_id` to one of those ids (the provider normal
default). Codex → `default`; OpenCode → `build`; grok → `default`; goose
→ `auto` (unchanged).

### D4 — Mobile: never invent from list order

Replace `orElse: () => modes.first` with a resolver that mirrors daemon
`defaultMode`: exact match → prefer `default` → `build` → first non-plan
non-dangerous mode. Defence-in-depth; D2/D3 remain primary.

No create-session mode picker. Mode stays a mid-session switcher.

### D5 — Auto sandbox invariant

- `auto` = `never` + `workspace-write`, dangerous, confirm on arm,
  non-persistent, sweeps pending.
- `full-access` gated by `allow_full_access`.
- **Invariant:** if the session is in mode `auto`, live sandbox is
  `workspace-write` and every `turn/start` carries
  `sandboxPolicy.type = workspaceWrite`. No `autoApprove` with empty
  sandbox from mode auto.
- **Required wire test:** `SetMode(auto)` → `beginTurn` over a pipe;
  assert both fields (not only in-memory).
- Mid-turn arming: approval sweep immediate; sandbox on next turn
  (engine semantic). Confirm-dialog copy may note that.

### D6 — Docs and tests are part of the decision

Update mode-list expectations, seed/create wire tests, mobile unmatched-id
tests, and a short supersession note on MADR 0044 D5's mode **list** only
(auto/full-access semantics unchanged).

---

## 5. Options considered

| option | verdict |
|---|---|
| **A. Add `default` + seed + auto sandbox invariant + mobile honesty** (chosen) | Closes all three bugs at the right layers |
| B. Mobile-only hardcode | Mode still missing from menu; daemon still reports `""` |
| C. Repurpose `read-only` as workspace-write | Lies about sandbox |
| D. Map empty config to `read-only` as "default" | Matches accidental UI, not user need |
| E. Create-session mode picker | Heavier UX; correct default is enough now |
| F. Id `normal` instead of `default` | Splits from grok / `defaultMode` |
| G. Cosmetic `current_mode_id` without seeding policy | Chip lies while engine differs — same class of bug |

---

## 6. Consequences

**Good**

- Menu: **default · read-only · auto** (+ optional full-access).
- New sessions report a real normal mode; chip, wire, and engine agree.
- Auto cannot arm as never-without-sandbox on untrusted projects.
- `/plan off` / `defaultMode` gain a codex-friendly `"default"` id.

**Accepted trade-offs**

- Empty `providers.codex.approval_policy` / `sandbox_mode` means
  mcremote's `default` mode for remote sessions, not silent engine-file
  inheritance of the live pair. Operators who need a specific pair set
  both fields explicitly.
- One more mode id in provider-specific vocabulary (already accepted).

**Out of scope**

- Create-time auto checkbox on the phone.
- Codex granular approval / `experimentalApi` `thread/settings/update`.
- Changing goose's default.
- Persisting last-used mode across sessions.

---

## 7. Verification criteria (acceptance)

1. Fresh codex session (empty policy config): menu has `default`,
   `read-only`, `auto`; chip = **default**; `thread/start` carries
   `on-request` + `workspace-write`.
2. `SetMode(auto)` → next `turn/start` has `never` +
   `sandboxPolicy.type=workspaceWrite` (pipe test).
3. Config `approval_policy: never` alone seeds auto pair (both fields),
   not never+empty.
4. `allow_full_access: false` still hides full-access.
5. OpenCode/grok/goose create defaults unchanged.
6. Mobile: empty `currentModeId` with a list containing `default` does
   not check a non-default first entry when resolver prefers default.
7. `go test ./internal/provider/codex/…`, `./internal/session/…`, mobile
   mode tests, format/lint gates green.

---

## 8. Summary

| bug | root cause | fix |
|---|---|---|
| Menu missing normal mode | D5 table never included `on-request`+`workspace-write` | **D1** add `default` |
| Chip shows first list item | empty `current_mode_id` + `modes.first` | **D2** seed; **D4** honest resolver |
| Cross-provider defaulting | no create contract | **D3** |
| Auto does not set workspace-write | never alone → engine **readOnly** on untrusted; autoApprove without sandbox | **D2.2** repair; **D5** invariant + wire test |

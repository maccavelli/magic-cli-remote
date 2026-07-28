# MADR 0044 — Implementation plan: auto-approve session modes

Companion to [MADR 0044](./0044-auto-approve-modes.md). Read that first: it
carries the live-probe evidence and the reasoning. This document is the build
order.

- **Status**: Proposed — for review
- **Date**: 2026-07-28
- **Targets**: OpenCode 1.18.7 (HTTP), codex-cli 0.145.0 (app-server)

---

## 0. Summary of the change

| layer | change |
|---|---|
| protocol | **none** |
| mobile | **one** cosmetic change (chip treatment + arm confirmation) |
| `internal/provider/httpagent` | per-session `autoApprove` flag + `Host` accessors |
| `internal/provider/opencode` | `auto` mode; hardened auto-approve in `emitPermissionAsk`; pending sweep |
| `internal/provider/codex` | session modes (new); per-turn policy override; **wire-shape fix** |
| `internal/config` | `providers.codex.allow_full_access` |

Six phases. Phases 1–3 (OpenCode) and 4–5 (codex) are independent and can land
separately; phase 0 is a standalone bug fix that should land first regardless of
whether the rest is approved.

---

## Phase 0 — Fix the codex sandbox wire shape (independent, land first)

Standalone bug fix (MADR 0044 Finding 5 / D7). Ship it even if the rest of this
plan is rejected: today, setting the documented `providers.codex.sandbox_mode`
option makes every codex session fail to start.

### 0.1 `internal/provider/codex/session.go` — `startNew`

Replace the object form (currently `session.go:183-188`):

```go
	if s.cfg.SandboxMode != "" {
		params["sandbox"] = map[string]any{
			"type":          s.cfg.SandboxMode,
			"networkAccess": false,
		}
	}
```

with the plain kebab-case string the server's `SandboxMode` enum expects:

```go
	// thread/start takes SandboxMode — a kebab-case *string*. turn/start's
	// sandboxPolicy is a different type (a camelCase-tagged object); do not
	// confuse the two. Verified against codex 0.145: the object form is
	// rejected with -32600 "expected map with a single key" (MADR 0044).
	if s.cfg.SandboxMode != "" {
		params["sandbox"] = s.cfg.SandboxMode
	}
```

`approvalPolicy` (`session.go:189-191`) is already correct — a bare string.

### 0.2 Apply the same fix to `resume`

`s.resume` (`session.go:172`) builds `thread/resume` params. `ThreadResumeParams`
has the identical `sandbox: SandboxMode` field. Audit it and apply the same
shape; if it does not currently send sandbox/approvalPolicy at all, add both, so
a resumed thread does not silently drop back to engine defaults.

### 0.3 Correct the fixture that locked in the bug

`internal/provider/codex/fixtures_test.go:283-330` asserts the object form and
its kebab-case `type`. Update it to assert `sandbox` is a **JSON string** equal
to the configured mode, and keep the existing "empty mode omits the field"
assertion (`fixtures_test.go:375-385`) as-is.

### 0.4 Add a live contract test

New `internal/provider/codex/live_sandbox_test.go`, behind the same live build
gate the other `live_*_test.go` files use. Against a real `codex app-server`:

- `thread/start` with `sandbox` as a string for each of the three enum values →
  succeeds;
- `thread/start` with `sandbox` as an object → fails (pins the finding, so a
  future codex release that accepts both does not silently un-document this);
- `turn/start` with `sandboxPolicy` as an object → accepted;
- `turn/start` with `sandboxPolicy` as a string → fails.

This is the drift guard the fake could not provide.

**Exit criteria**: `providers.codex.sandbox_mode = "workspace-write"` in config
produces a working session against a real engine.

---

## Phase 1 — Per-session auto-approve state in `httpagent`

Provider-agnostic, so goose (and any future HTTP dialect) inherits it.

### 1.1 `internal/provider/httpagent/session.go` — session state

Add beside the existing `agent` field (`session.go:32`):

```go
	// autoApprove answers permission requests without the phone (MADR 0044).
	// Per session, never persisted, cleared on any non-auto mode switch.
	autoApprove bool
```

Guarded by the existing `s.mu`, exactly like `agent`.

### 1.2 Accessors, mirroring `Agent`/`SetAgent` (`session.go:298-306`)

```go
// AutoApprove reports whether this session answers permission requests itself.
func (s *session) AutoApprove() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autoApprove
}

// SetAutoApprove arms or disarms daemon-side permission auto-approval. It is
// session-scoped and deliberately not persisted (MADR 0044 D8).
func (s *session) SetAutoApprove(on bool) {
	s.mu.Lock()
	s.autoApprove = on
	s.mu.Unlock()
}
```

### 1.3 `internal/provider/httpagent/httpagent.go` — `Host` interface

Add both methods to the `Host` interface (near `Agent`/`SetAgent`, ~line 305),
with a doc comment pointing at MADR 0044.

### 1.4 `PendingPermissions` accessor

The arming sweep (D4.5) needs the set of outstanding ids. `s.pending`
(`session.go:989`) already holds them; expose a snapshot:

```go
// PendingPermissions returns the ids of permission requests this session has
// surfaced and not yet resolved. Snapshot — safe to iterate after return.
func (s *session) PendingPermissions() []string
```

Add to `Host` alongside `TakePending`.

**Tests** (`internal/provider/httpagent/session_test.go`):
- default is `false`;
- set/get round-trips and is race-free under `-race` with concurrent
  `SetAutoApprove` / `AutoApprove` / `SetAgent`;
- `PendingPermissions` reflects `TrackPermission` / `TakePending`, and the
  returned slice is a copy (mutating it does not affect session state).

---

## Phase 2 — OpenCode: harden the auto-approve path

Do this **before** wiring the mode. The existing global `always_approve` path
(`internal/provider/opencode/permission.go:102-138`) has three defects that are
tolerable for a config flag and not for a chat-screen control.

### 2.1 Rewrite `emitPermissionAsk`

Target shape:

```go
func (o *httpSession) emitPermissionAsk(p permAsk) {
	if p.ID == "" {
		return
	}
	origin := firstNonEmpty(p.SessionID, o.h.EventAgentSessionID(), o.h.AgentSessionID())

	// Track origin and pending *before* deciding how to answer: the
	// session-scoped reply fallback needs the origin even on the auto path,
	// and TrackPermission is what makes a duplicated asked/updated pair for
	// one id collapse to a single reply (MADR 0044 D4.2).
	o.h.TrackPermissionOrigin(p.ID, origin)
	o.h.TrackPermission(p.ID)

	if o.h.Config().AlwaysApprove || o.h.AutoApprove() {
		o.autoApprove(p)
		return
	}
	// …unchanged: build options, Emit permission_request…
}
```

Note `TrackPermission` starts the `PermissionTimeout` fail-safe goroutine
(`session.go:993`). That is desirable: if auto-approval fails outright, the
existing expiry still unblocks the engine.

### 2.2 New `autoApprove` helper (`permission.go`)

```go
// autoApproveAttempts / autoApproveBackoff bound the retry. A permission that
// cannot be answered must end up in front of the user, not in a log line: the
// agent is blocked until someone answers it.
const autoApproveAttempts = 3

func (o *httpSession) autoApprove(p permAsk) {
	go func() {
		defer recoverLog("auto-approve permission")

		var err error
		for attempt := 0; attempt < autoApproveAttempts; attempt++ {
			if attempt > 0 {
				select {
				case <-o.h.Done():
					return
				case <-time.After(backoff(attempt)):
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err = o.RespondPermission(ctx, p.ID, "once", false)
			cancel()
			if err == nil {
				o.h.Log().Info("auto-approved permission",
					slog.String("permission", p.Name),
					slog.String("patterns", strings.Join(p.Patterns, ",")),
					slog.String("agent_session_id", origin))
				o.h.Emit(event.Event{
					Type: event.TypeNotice,
					Text: "Auto-approved: " + permissionSummary(p),
				})
				return
			}
			// Someone else already answered it (phone, resync, expiry):
			// not an error, and retrying would 4xx forever.
			if o.h.PermissionAlreadyResolved(p.ID) {
				return
			}
		}

		// Fail-safe: surface the sheet rather than leaving the agent blocked
		// with nothing on screen (MADR 0044 D4.1).
		o.h.Log().Warn("auto-approve failed; surfacing permission",
			slog.String("permission_id", p.ID), slog.String("err", err.Error()))
		o.h.Emit(event.Event{
			Type: event.TypeNotice,
			Text: "Auto-approve failed for this request — please answer it.",
		})
		o.emitPermissionSheet(p)
	}()
}
```

Notes for the implementer:

- **`RespondPermission` calls `TakePending` internally.** Check `http.go:989`
  before writing the retry: if it removes the id from `pending` on entry, a
  retry will find nothing pending and may short-circuit. Either move the
  `TakePending` to after a successful engine reply, or have `autoApprove` call
  `respondPermissionEngine` directly and do the bookkeeping once on success.
  **This is the single most likely place to introduce a bug in this phase.**
- Split the sheet-building half of the current `emitPermissionAsk` into
  `emitPermissionSheet(p permAsk)` so both the normal path and the fail-safe
  use it — no duplicated option construction.
- `PermissionAlreadyResolved` may not exist; if `TakePending` semantics make it
  redundant, drop it rather than adding a near-duplicate accessor.
- Reply is `"once"`, never `"always"` (MADR 0044 D4.4), even when
  `p.Always` is non-empty.

### 2.3 `permissionSummary`

Small helper: `"bash (git status)"` — permission name plus up to ~2 patterns,
truncated to ~120 chars. Keeps the notice line readable when a permission
carries a long pattern list.

### 2.4 Tests (`internal/provider/opencode/permission_test.go`)

Table-driven against the existing fake HTTP engine:

| case | expectation |
|---|---|
| auto on, `permission.asked` | one `POST …/reply {"reply":"once"}`; **no** `permission_request` event; one `notice` |
| auto on, `permission.updated` for the same id | exactly **one** reply total (dedup) |
| auto on, `permission.v2.asked` | auto-approved (v2 shape goes through the same funnel) |
| auto on, child-session permission | reply routes via the child origin when the global path 404s |
| auto **off** | `permission_request` emitted, no reply sent |
| reply fails 3× | `permission_request` **is** emitted + failure notice; agent not left blocked |
| reply fails once then succeeds | one approval, no sheet |
| auto on, reply says already-resolved | no sheet, no retry storm |
| `Config().AlwaysApprove` true, session flag false | still auto-approves (no regression) |
| resync (`resync.go`) with auto on | pending permissions drained, no sheets |

Run the whole package under `-race`.

---

## Phase 3 — OpenCode: the `auto` mode

### 3.1 `internal/provider/opencode/mode.go` — advertise it

Add the synthetic mode constant and append it **last** in both
`staticModes()` (`mode.go:109`) and `sessionModes()` (`mode.go:118`):

```go
// autoModeID is a synthetic mode: OpenCode has no auto-approve agent, so this
// id maps to the normal agent plus daemon-side permission auto-approval
// (MADR 0044 D3). Appended last so session.defaultMode still resolves "build".
const autoModeID = "auto"

func autoMode() event.SessionMode {
	return event.SessionMode{
		ID:          autoModeID,
		Name:        "auto",
		Description: "Auto-approve — permissions answered automatically (dangerous)",
	}
}
```

**Ordering matters.** `internal/session/commands.go:557` resolves the
"return to normal" mode as `build`, else *the first non-plan mode advertised*.
Appending `auto` last keeps `build` first, so `/plan off` cannot land the user
in auto mode. Add a regression test for exactly that.

### 3.2 `currentMode` must report `auto`

`currentMode` (`mode.go:132`) returns the agent name. Auto-approve is not an
agent, so check the flag first:

```go
func (o *httpSession) currentMode(modes []event.SessionMode) string {
	if o.h.AutoApprove() {
		return autoModeID
	}
	// …unchanged…
}
```

Otherwise the chip reads "build" while the session silently auto-approves —
the worst possible failure mode for this feature.

### 3.3 `SetMode` — intercept before the agent lookup

```go
func (o *httpSession) SetMode(ctx context.Context, modeID string) (string, error) {
	modeID = strings.TrimSpace(modeID)
	if modeID == "" {
		return "", fmt.Errorf("empty mode id")
	}

	if strings.EqualFold(modeID, autoModeID) {
		// Auto implies the normal agent: the menu is single-select and must
		// stay honest about what is running (MADR 0044 D2).
		o.h.SetAgent(normalAgentID(o.sessionModesList(ctx)))
		o.h.SetAutoApprove(true)
		o.h.Log().Warn("auto-approve armed",
			slog.String("agent_session_id", o.h.AgentSessionID()))
		o.sweepPendingPermissions()
		return autoModeID, nil
	}

	// Any other mode disarms auto-approve.
	modes, _ := o.sessionModes(ctx)
	for _, m := range modes {
		if strings.EqualFold(m.ID, modeID) {
			o.h.SetAutoApprove(false)
			o.h.SetAgent(m.ID)
			// …existing log…
			return m.ID, nil
		}
	}
	return "", fmt.Errorf("unknown mode %q", modeID)
}
```

`normalAgentID` returns `build` when advertised, else the first non-`plan`,
non-`auto` mode — same resolution rule as `session.defaultMode`, so the two
cannot disagree.

Note the `auto` branch must run **before** the loop: `auto` is in the advertised
list, so the loop would otherwise match it and try `SetAgent("auto")`, pointing
prompts at a non-existent agent. A test must pin this.

### 3.4 The arming sweep (D4.5)

```go
// sweepPendingPermissions answers permissions that were already waiting when
// auto-approve was armed. Without this, arming auto to unblock a stuck agent
// does nothing until the agent asks again (MADR 0044 D4.5).
func (o *httpSession) sweepPendingPermissions() {
	for _, id := range o.h.PendingPermissions() {
		o.autoApprove(permAsk{ID: id, Name: "pending"})
	}
}
```

The engine's `GET /permission` list (already used by
`resyncPendingPermissions`, `resync.go:142`) is the better source, since it
carries the name and patterns for the audit notice. Prefer it, and fall back to
`PendingPermissions()` if the list call fails. Emit a `permission_resolved` for
each swept id so the sheet already on the phone's screen is dismissed — this is
the one place where `permission_resolved` is correct, because a sheet really
was emitted earlier.

### 3.5 Disarm on session lifecycle

- Not persisted, not restored on resume (D8). `Create`/`Resume` leave the flag
  at its zero value; assert that in a test.
- Re-advertise modes after any `SetMode` so the chip updates — the transport
  already emits `session_mode` with `CurrentModeID` after `SetMode`
  (`httpagent/session.go:233`), so this is a no-op; confirm rather than add.

### 3.6 Tests (`internal/provider/opencode/mode_test.go`)

- `auto` appears last in both live-catalog and static mode lists;
- `SetMode("auto")` → flag on, agent set to `build`, returns `"auto"`, and does
  **not** call `SetAgent("auto")`;
- `SetMode("plan")` → flag off, agent `plan`;
- `SetMode("build")` from auto → flag off;
- `currentMode` returns `auto` when armed, regardless of agent;
- `SetMode("bogus")` → error, flag unchanged (a failed switch must not disarm);
- arming sweeps outstanding permissions and emits `permission_resolved` per id;
- **`/plan off` from auto lands in `build`, not `auto`** (`internal/session`
  test against the advertised list).

---

## Phase 4 — codex: session modes

codex has no mode support today. This adds it.

### 4.1 `internal/config` — gate full access

`internal/config/config.go` (codex block, ~line 490):

```go
	// AllowFullAccess advertises the full-access session mode, which runs with
	// no approval prompts *and* no sandbox. Off by default: auto-approve
	// without a sandbox is a materially different risk (MADR 0044 D5).
	AllowFullAccess bool `mapstructure:"allow_full_access"`
```

Default `false` in the defaults block (~line 597) and
`internal/config/load.go` (~line 202). Thread through
`internal/daemon/daemon.go:193` into `codex.Config`
(`internal/provider/codex/config.go:25`).

### 4.2 New `internal/provider/codex/mode.go`

```go
// codexMode is one advertised mode and the engine policy pair it maps to.
// codex expresses "auto-approve" as approvalPolicy=never; the sandbox is what
// keeps an unattended session contained, which is why auto pairs never with
// workspace-write and full access is a separate, opt-in mode (MADR 0044 D5).
type codexMode struct {
	mode           event.SessionMode
	approvalPolicy string // AskForApproval: untrusted | on-request | never
	sandbox        string // SandboxMode:    read-only | workspace-write | danger-full-access
}

var codexModes = []codexMode{
	{event.SessionMode{ID: "read-only", Name: "read-only",
		Description: "Read files; ask before anything else"},
		"on-request", "read-only"},
	{event.SessionMode{ID: "auto", Name: "auto",
		Description: "Auto-approve — no prompts; edits confined to the workspace"},
		"never", "workspace-write"},
	{event.SessionMode{ID: "full-access", Name: "full access",
		Description: "Auto-approve with no sandbox (dangerous)"},
		"never", "danger-full-access"},
}
```

- `availableModes(cfg)` drops `full-access` unless `cfg.AllowFullAccess`.
- `defaultModeID(cfg)` resolves the configured `approval_policy` +
  `sandbox_mode` pair to a mode id, falling back to `read-only`. When the
  configured pair matches no mode, advertise the list with **no** current id
  rather than lying about which one is active.

### 4.3 Per-session policy state (`internal/provider/codex/session.go`)

Add under `s.mu`:

```go
	// approvalPolicy / sandboxMode are the session's live policy. Seeded from
	// config at start and re-sent on every turn/start so the engine converges
	// on daemon state after a restart or resume (MADR 0044 D5).
	approvalPolicy string
	sandboxMode    string
	autoApprove    bool
```

### 4.4 `startNew` — send the seeded policy

Read the three fields under the lock and send `approvalPolicy` (string) and
`sandbox` (string — post-Phase-0 shape). Then advertise the modes:

```go
	s.emit(event.Event{
		Type:          event.TypeMode,
		SessionID:     s.localID,
		Timestamp:     time.Now().UTC(),
		Modes:         availableModes(s.cfg),
		CurrentModeID: s.currentModeID(),
	})
```

Do the same in `resume`, next to the existing `emitCapabilities()` call
(`session.go:225`), so a resumed session gets a mode chip.

### 4.5 `runTurn` — re-send policy every turn

In the params block (`session.go:384-397`), after `model`:

```go
	// Override for this turn and subsequent turns. Re-sent every turn so the
	// engine converges on daemon state after an engine restart (MADR 0044 D5).
	// NB: turn/start takes sandboxPolicy — an object with a camelCase type tag
	// — not thread/start's kebab-case sandbox string.
	if approval != "" {
		params["approvalPolicy"] = approval
	}
	if pol := sandboxPolicyParam(sandbox); pol != nil {
		params["sandboxPolicy"] = pol
	}
```

```go
// sandboxPolicyParam maps a SandboxMode string to turn/start's SandboxPolicy
// object. Verified against codex 0.145 (MADR 0044 Finding 4).
func sandboxPolicyParam(mode string) map[string]any {
	switch mode {
	case "read-only":
		return map[string]any{"type": "readOnly", "networkAccess": false}
	case "workspace-write":
		return map[string]any{"type": "workspaceWrite", "networkAccess": false, "writableRoots": []string{}}
	case "danger-full-access":
		return map[string]any{"type": "dangerFullAccess"}
	default:
		return nil
	}
}
```

### 4.6 `SetMode` + `provider.ModeSession`

```go
func (s *session) SetMode(ctx context.Context, modeID string) error {
	m, ok := findCodexMode(availableModes(s.cfg), modeID)
	if !ok {
		return fmt.Errorf("unknown mode %q", modeID)
	}
	s.mu.Lock()
	s.approvalPolicy = m.approvalPolicy
	s.sandboxMode = m.sandbox
	s.autoApprove = m.approvalPolicy == "never"
	s.mu.Unlock()

	s.sweepPendingApprovals()   // MADR 0044 D4.5
	s.emit(/* TypeMode with CurrentModeID: m.mode.ID */)
	return nil
}
```

Takes effect on the **next turn** (that is the protocol's own semantic —
"this turn and subsequent turns" applies at `turn/start`). Say so in the
confirmation notice rather than implying it is immediate; the daemon's
`session.set_mode` confirmation text lives in `internal/session/commands.go`.

Add the compile-time assertion `var _ provider.ModeSession = (*session)(nil)`.

### 4.7 Interception as the second layer (D6)

`handleApprovalRequest` (`session.go:1130`):

```go
	s.mu.Lock()
	auto := s.autoApprove
	s.mu.Unlock()
	if s.cfg.AlwaysApprove || auto {
		// …accept, and emit the audit notice (same helper as OpenCode)…
		return
	}
```

The existing branch's `_ = send(...)` is fire-and-forget. Unlike the OpenCode
path there is no retry to add — `send` is a local JSON-RPC write, and if the
connection is gone the turn is already dead — but it **must** gain the audit
notice and an INFO log, and it should log at WARN when `send` returns an error.

### 4.8 `sweepPendingApprovals`

codex tracks outstanding approvals in `s.pendingPerms`
(`session.go:1140`, id → JSON-RPC request id). On arming, accept each, emit
`permission_resolved` per id (dismissing the phone's sheet), clear the map.

### 4.9 `/plan` must still report unsupported

codex advertises no `plan` mode, so `cmdPlan` (`commands.go:580`) already emits
*"This agent doesn't offer a plan mode."* Add a regression test — adding modes
to codex is exactly the change that could accidentally make `/plan` route into
`read-only`.

### 4.10 Tests

- `availableModes` hides `full-access` unless `AllowFullAccess`;
- `defaultModeID` round-trips each config pair; unknown pair → empty current id;
- fixtures: `thread/start` carries `approvalPolicy` + string `sandbox` for the
  seeded mode; `turn/start` carries `approvalPolicy` + the **object**
  `sandboxPolicy`, with a case-sensitivity assertion on `workspaceWrite`
  (mirroring the existing kebab-case guard at `fixtures_test.go:320`);
- `SetMode("auto")` → policy fields set, `autoApprove` true, mode event emitted;
- `SetMode("full-access")` with the gate off → error;
- `handleApprovalRequest` auto-accepts when armed and emits the notice;
- arming sweeps `pendingPerms` and emits `permission_resolved` for each;
- `/plan` on a codex session reports unsupported;
- live-gated: arm `auto`, run a turn that shells out, assert no approval request
  arrives and the command runs.

---

## Phase 5 — Mobile: make the armed state unmissable

The mode list, the switcher and `set_mode` all work unchanged. Two changes, both
in `apps/mobile/lib/features/chat/chat_screen.dart`.

### 5.1 Chip treatment for `auto` / `full-access` (`_ModeChip`, line 2479)

`_ModeChip` already special-cases plan mode (tinted container +
`Icons.edit_off`, line 2481). Generalise it:

```dart
  static bool isPlan(SessionMode m) => m.id.toLowerCase() == 'plan';
  static bool isAuto(SessionMode m) =>
      const {'auto', 'full-access'}.contains(m.id.toLowerCase());
```

Auto renders with `scheme.errorContainer` and `Icons.bolt` — deliberately the
loudest treatment on the app bar. The plan-mode comment already states the
principle ("the one mode difference worth noticing at a glance"); auto-approve
is the second.

Use `colorScheme` roles, not literal colours, so light/dark and the existing
theme both work.

### 5.2 Confirm on arm (`_ModeSelector.onSelected`, line 2447)

Before calling `setMode` for an auto-ish id, show an `AlertDialog`:

> **Run without approvals?**
> This session will approve every permission request automatically, including
> file edits and shell commands. It stays on until you switch modes.
> *(codex full access adds: "and runs with no sandbox.")*
> — Cancel / Turn on

Disarming needs no confirmation. Match the existing failure handling: the
current `try/catch` → `showTopNotification` stays as-is.

### 5.3 Tests (`apps/mobile/test/`)

Widget tests: chip renders the alert treatment for `auto` and `full-access` and
not for `build`; selecting `auto` shows the dialog; cancelling does **not** call
`setMode`; confirming does; selecting `build` never shows a dialog.

---

## Phase 6 — Docs and configuration

1. `docs/protocol-v1.md` — note that `session_mode` may carry an `auto` id whose
   enforcement is daemon-side, and that codex advertises `read-only` / `auto` /
   `full-access`. No message-shape change.
2. `configs/` sample + README — document
   `providers.codex.allow_full_access`, and cross-reference the existing
   `always_approve` as the *global* form of the same behaviour so the two are
   not confused.
3. `docs/agent_cli_slash_commands_matrix.md` — codex now supports `/mode`;
   `/plan` remains unsupported there.
4. MADR 0044 status → Accepted, with the shipped date.

---

## Risk register

| # | risk | mitigation |
|---|---|---|
| R1 | `RespondPermission` consumes the pending id on entry, so Phase 2's retry silently no-ops | Called out inline at 2.2; the "reply fails once then succeeds" test fails loudly if it happens |
| R2 | `SetMode("auto")` falls through to the agent loop and sets a non-existent agent | Interception ordered before the loop, with a dedicated test |
| R3 | Chip shows `build` while auto is armed | `currentMode` checks the flag first (3.2), test asserts it |
| R4 | `/plan off` lands in `auto` | `auto` appended last + explicit regression test (3.1) |
| R5 | codex `sandboxPolicy` sent in `thread/start`'s string shape (or vice versa) | Two distinct helpers, fixture assertions on both, live contract test (0.4) |
| R6 | Auto-approve armed by accident from the app bar | Confirmation dialog + alert-coloured chip + non-persistence |
| R7 | Notice spam on a permission-heavy turn | Accepted for v1; coalescing is a follow-up. Do **not** silence it — the audit trail is the point |
| R8 | An OpenCode reply failure leaves the agent blocked with a clean-looking UI | Retry, then surface the sheet; `PermissionTimeout` remains as the outer fail-safe |
| R9 | Auto silently survives a daemon restart | Zero-value flag, not persisted; asserted in a resume test |

---

## Definition of done

- `make lint test` clean; `go test -race ./internal/provider/...` clean.
- `gofmt`, `golint`, `govulncheck` pass before `git add` (repo pre-add rule).
- Live-gated tests pass against OpenCode 1.18.7 and codex 0.145.0.
- Manual end-to-end, per provider: start a session → switch to `auto` →
  confirm → prompt something that requests permission → **no sheet**, notice
  line present in the transcript, action performed → switch back to
  `build` / `read-only` → next request **does** prompt.
- Manual fail-safe check: arm auto, kill the engine mid-turn, confirm the
  session surfaces a sheet or an error rather than hanging silently.

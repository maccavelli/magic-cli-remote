# MADR 0044 — Implementation plan: auto-approve session modes

Companion to [MADR 0044](./0044-auto-approve-modes.md). Read that first: it
carries the live-probe evidence and the reasoning. This document is the build
order.

- **Status**: Proposed — for review. **Phase 0 is complete** (2026-07-28);
  phases 1–6 are unstarted.
- **Date**: 2026-07-28
- **Targets**: OpenCode 1.18.7 (HTTP), codex-cli 0.145.0 (app-server)

---

## 0. Summary of the change

| layer | change |
|---|---|
| protocol | one **additive, optional** field: `SessionMode.dangerous` (see 5.0) |
| mobile | chip treatment + arm confirmation, both driven by that flag |
| goose | **none** — see "Goose: what already exists" |
| `internal/provider/httpagent` | per-session `autoApprove` flag + `Host` accessors |
| `internal/provider/opencode` | `auto` mode; hardened auto-approve in `emitPermissionAsk`; pending sweep |
| `internal/provider/codex` | session modes (new); per-turn policy override; ~~wire-shape fix~~ **(done)** |
| `internal/config` | `providers.codex.allow_full_access` |

Six phases. Phases 1–3 (OpenCode) and 4–5 (codex) are independent and can land
separately; phase 0 was a standalone bug fix that landed first, independently of
review of the rest.

| phase | status |
|---|---|
| 0 — codex sandbox wire shape | ✅ **complete** (2026-07-28) |
| 1 — `httpagent` per-session state | ⬜ not started |
| 2 — OpenCode auto-approve hardening | ⬜ not started |
| 3 — OpenCode `auto` mode | ⬜ not started |
| 4 — codex session modes | ⬜ not started |
| 5 — mobile | ⬜ not started |
| 6 — docs and config | ⬜ not started |

Phase 4 now starts from a fixed, live-tested policy path — `applyPolicyParams`
and the `captureThreadRequest` test helper both exist and should be extended
rather than reinvented.

---

## Phase 0 — Fix the codex sandbox wire shape ✅ **complete (2026-07-28)**

Standalone bug fix (MADR 0044 Finding 5 / D7), landed independently of review of
the rest: before it, setting the documented `providers.codex.sandbox_mode`
option made every codex session fail to start.

### 0.1 `applyPolicyParams` — one helper, both call sites ✅

`internal/provider/codex/session.go` gained:

```go
// applyPolicyParams sets the sandbox and approval-policy overrides on
// thread/start and thread/resume params. Both take plain enum *strings*:
// ThreadStartParams.sandbox is a SandboxMode ("read-only" | "workspace-write" |
// "danger-full-access"), not an object. …
// Note the asymmetry with turn/start, whose sandboxPolicy *is* an object with a
// camelCase type tag — the two are different protocol types and must not be
// cross-wired.
func applyPolicyParams(params map[string]any, cfg Config) {
	if cfg.SandboxMode != "" {
		params["sandbox"] = cfg.SandboxMode
	}
	if cfg.ApprovalPolicy != "" {
		params["approvalPolicy"] = cfg.ApprovalPolicy
	}
}
```

`startNew` calls it in place of the old object literal. `approvalPolicy` was
already correct and is unchanged in behaviour.

**Phase 4 note**: `applyPolicyParams` currently reads `cfg`. When per-session
policy state lands (4.3), it takes the session's live values instead — extend
this helper rather than adding a parallel one, or the two will diverge exactly
the way the fake and the engine did.

### 0.2 `resume` ✅ — was a second instance of the same bug

`resume` sent **neither** `sandbox` nor `approvalPolicy`, though
`ThreadResumeParams` carries both. A resumed thread silently fell back to the
engine's defaults, so one session ran under a different policy after a daemon
restart. It now calls `applyPolicyParams` too.

This matters beyond tidiness: MADR 0044 D5 depends on resume re-asserting the
session's policy, so phase 4 would have been built on sand.

### 0.3 Fixture rewrite ✅ — drive the code, never a literal

The old `TestThreadStartWireShape` / `TestThreadResumeWireShape` built the
params map *inside the test* and asserted against their own literal, so they
never executed production code — which is exactly why the bug survived them.

Replaced by `captureThreadRequest(t, cfg, opts)`, which drives `session.create`
through a real `conn` over `io.Pipe`, answers the JSON-RPC request, and returns
the frame that actually went on the wire. Tests now:

| test | asserts |
|---|---|
| `TestThreadStartSandboxIsEnumString` | `sandbox` is a JSON **string** equal to the configured mode, for all three enum values; `approvalPolicy` accompanies it |
| `TestThreadResumeCarriesPolicy` | `thread/resume` carries `threadId` + both policy fields |
| `TestEmptySandboxOmitsWireField` | unset config **omits** both fields (rather than sending `""`, which codex rejects as an unknown variant) |

**Guard verified, not assumed**: reverting `applyPolicyParams` to the object
form fails all four subtests with
`sandbox = map[...] (map[string]interface {}), want a plain enum string`, then
passes again on restore.

### 0.4 Live contract test ✅

`internal/provider/codex/live_sandbox_test.go`, build tag `live_codex` (matching
the existing `live_*_test.go` files). Against a real `codex app-server`, in
**both** directions — asserting the wrong shape is *rejected* is what stops the
fake drifting back:

| subtest | result vs codex 0.145.0 |
|---|---|
| `thread/start` `sandbox` string, all three values | accepted ✅ |
| `thread/start` `sandbox` as object | rejected ✅ |
| `thread/start` `sandbox` bogus value | rejected — server really does validate ✅ |
| `turn/start` `sandboxPolicy` as object | accepted ✅ |
| `turn/start` `sandboxPolicy` as string | rejected ✅ |

The turn/start subtests treat model/auth/network failures as acceptable and fail
only on JSON-RPC `-32600` (via `isParamError`), so they stay meaningful on a
machine without codex credentials.

A future codex release that starts accepting both shapes will fail
`object_rejected` — deliberately, to force a conscious decision rather than a
silent re-drift.

### Verification ✅

`go build ./...`; `go test ./internal/...`;
`go test -race ./internal/provider/codex/`; `go test -tags live_codex` (5/5
subtests pass against codex 0.145.0); `gofmt`; `go vet` with and without
`-tags live_codex`; `govulncheck` — all clean.

**Exit criteria met**: `providers.codex.sandbox_mode = "workspace-write"`
produces a working session against a real engine.

---

## Goose: what already exists (read before phase 5)

Goose **already ships a working auto-approve mode**, and it is the precedent
this design should follow rather than collide with.

```go
// internal/provider/goose/goose.go:23
var staticModes = []event.SessionMode{
	{ID: "auto",          Name: "Auto",          Description: "Automatically approve tool calls"},
	{ID: "approve",       Name: "Approve",       Description: "Ask before every tool call"},
	{ID: "smart_approve", Name: "Smart Approve", Description: "Ask only for sensitive tool calls"},
	{ID: "chat",          Name: "Chat",          Description: "Chat only, no tool calls"},
}
// …
DefaultModeID: "auto"
```

Facts that matter here:

1. **The id is already `auto`**, with the same user-facing meaning. Reusing it
   for OpenCode and codex is therefore *consistency*, not a collision — one id
   means one thing to the user across four providers, even though the
   enforcement differs (goose: engine-native ACP `session/set_mode`; OpenCode:
   daemon-side interception; codex: `approvalPolicy: never`).
2. **Goose enforces it in the engine**, via `session/set_mode`
   (`acphttp/session.go:758`). Nothing in phases 1–4 alters that path.
3. **Goose's default mode *is* `auto`** (`goose.go:47`). Every goose session
   starts auto-approving.
4. Goose has four modes, not two. The mode strip already carries
   provider-specific vocabulary — further evidence for MADR 0044 D1/D2.

Point 3 is the trap. **Phase 5 as originally written would have regressed
shipped goose behaviour**: it keys the alert-red chip and the "Run without
approvals?" confirmation off the mode id `auto`, so goose would have gained a
loud alarm chip on its *normal, default* state from session start, plus a
confirmation dialog on a control that has always been one tap. Phase 5 below is
rewritten to prevent that.

Also note `acphttp` has its own `AlwaysApprove` config gate
(`acphttp/session.go:1188`), parallel to but independent of the `httpagent` one.
Whether goose's auto mode should also drive that flag — today it does not, so
`auto` relies purely on the engine honouring it — is out of scope here.

---

## Phase 1 — Per-session auto-approve state in `httpagent`

Scoped to `httpagent` dialects, which today means **OpenCode only**.

**This does not touch goose.** Goose is not an `httpagent` dialect — it is built
on `internal/provider/acphttp` (`internal/provider/goose/goose.go:9`), a
separate shared package that does not import `httpagent`. The two have no
compile-time or runtime coupling, so nothing in phases 1–4 can reach goose. See
"Goose: what already exists" below before touching phase 5, which *can*.

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

Read the session's policy fields under the lock and pass them to
`applyPolicyParams` (phase 0.1) — **change that helper to take the session's
live values rather than `cfg`**, so `startNew`, `resume` and the mode switch all
go through one place. Do not add a second helper: a parallel path is how the
create and resume halves of Finding 5 got out of step in the first place.

Then advertise the modes:

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
- fixtures: extend `captureThreadRequest` (phase 0.3) to also capture
  `turn/start`, then assert `thread/start` carries `approvalPolicy` + string
  `sandbox` for the seeded mode, and `turn/start` carries `approvalPolicy` + the
  **object** `sandboxPolicy` with a case-sensitivity assertion on
  `workspaceWrite`. Drive the session — never hand-build the params map;
- extend `live_sandbox_test.go` with the mode-driven equivalents, so the
  per-session policy path gets the same real-engine guard the config path now
  has;
- `SetMode("auto")` → policy fields set, `autoApprove` true, mode event emitted;
- `SetMode("full-access")` with the gate off → error;
- `handleApprovalRequest` auto-accepts when armed and emits the notice;
- arming sweeps `pendingPerms` and emits `permission_resolved` for each;
- `/plan` on a codex session reports unsupported;
- live-gated: arm `auto`, run a turn that shells out, assert no approval request
  arrives and the command runs.

---

## Phase 5 — Mobile: make the armed state unmissable *(without regressing goose)*

The mode list, the switcher and `set_mode` all work unchanged.

### 5.0 The mobile UI must not infer danger from the mode id

The original draft matched on `id == 'auto'`. Per "Goose: what already exists"
above, that silently restyles and gates goose's **default** mode. Any
id-matching scheme has the same flaw: the id says *what the mode is called*, not
*how alarming it is here*, and only the provider knows the latter.

So the danger signal comes from the daemon, as one **additive, optional** field
on `SessionMode` (`internal/event/event.go:263`):

```go
type SessionMode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Dangerous marks a mode that removes a safety control the user would
	// otherwise have — today, one that answers permission requests without
	// them. Clients may style it distinctly and confirm before switching to
	// it. Optional: an omitted value means "no special treatment", which is
	// what every provider that predates this field wants (MADR 0044).
	Dangerous bool `json:"dangerous,omitempty"`
}
```

Set it on OpenCode's `auto` (phase 3.1) and codex's `auto` + `full-access`
(phase 4.2). **Leave goose's modes untouched**, so goose behaves exactly as it
does today; whether goose's `auto` should be flagged is goose's call to make
later, not a side effect of this work.

This is a **correction to MADR 0044 D1's "no new protocol" claim** — it is one
optional boolean with `omitempty`, so old daemons and old clients are unaffected
in both directions, but the claim as written was too strong. Update the MADR
rather than contorting the UI to preserve it.

### 5.1 Chip treatment (`_ModeChip`, `chat_screen.dart:2479`)

`_ModeChip` already special-cases plan mode (tinted container + `Icons.edit_off`,
line 2481). Generalise on the flag, not the id:

```dart
  static bool isPlan(SessionMode m) => m.id.toLowerCase() == 'plan';
  static bool isDangerous(SessionMode m) => m.dangerous;
```

Dangerous modes render with `scheme.errorContainer` and `Icons.bolt` —
deliberately the loudest treatment on the app bar. The plan-mode comment already
states the principle ("the one mode difference worth noticing at a glance");
auto-approve is the second.

Use `colorScheme` roles, not literal colours, so light and dark both work.

Requires adding `dangerous` to the Dart `SessionMode` model
(`apps/mobile/lib/data/protocol/models.dart`), defaulting to `false` when the
key is absent — which is what every current daemon sends.

### 5.2 Confirm on arm (`_ModeSelector.onSelected`, `chat_screen.dart:2447`)

When the selected mode is `dangerous`, show an `AlertDialog` before calling
`setMode`:

> **Run without approvals?**
> This session will approve every permission request automatically, including
> file edits and shell commands. It stays on until you switch modes.
> *(codex full access adds: "and runs with no sandbox.")*
> — Cancel / Turn on

Switching *away* needs no confirmation. Existing failure handling
(`try/catch` → `showTopNotification`) stays as-is.

Because goose sends no `dangerous` flag, **goose's switcher behaviour is
byte-for-byte unchanged** — no dialog, no restyle.

### 5.3 Tests (`apps/mobile/test/`)

- chip renders the alert treatment when `dangerous` is true, and not otherwise;
- **a goose-shaped mode list (`auto` / `approve` / `smart_approve` / `chat`, no
  `dangerous` flag, `auto` current) renders a plain chip and shows no dialog** —
  the explicit regression guard for this phase;
- selecting a dangerous mode shows the dialog; cancelling does **not** call
  `setMode`; confirming does;
- selecting a non-dangerous mode never shows a dialog;
- a mode list from a daemon that omits `dangerous` entirely decodes with
  `dangerous == false` (wire-compat).

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
| R5 | codex `sandboxPolicy` sent in `thread/start`'s string shape (or vice versa) | **Mitigated (phase 0)** — `applyPolicyParams` owns the string shape, `sandboxPolicyParam` (4.5) owns the object shape, and `live_sandbox_test.go` fails if either is cross-wired. Residual risk is phase 4 adding a *third* path; 4.4 forbids it explicitly |
| R6 | Auto-approve armed by accident from the app bar | Confirmation dialog + alert-coloured chip + non-persistence |
| R7 | Notice spam on a permission-heavy turn | Accepted for v1; coalescing is a follow-up. Do **not** silence it — the audit trail is the point |
| R8 | An OpenCode reply failure leaves the agent blocked with a clean-looking UI | Retry, then surface the sheet; `PermissionTimeout` remains as the outer fail-safe |
| R9 | Auto silently survives a daemon restart | Zero-value flag, not persisted; asserted in a resume test |
| R10 | **Phase 5 regresses goose**, whose default mode is already `auto` — alarm chip on the normal state, new dialog on a one-tap control | Danger is signalled by the daemon (`SessionMode.dangerous`), not inferred from the id; goose sends no flag and is unchanged. Explicit goose-shaped widget test in 5.3 |
| R11 | Phases 1–4 leak into goose | They cannot: goose is on `acphttp`, which does not import `httpagent`. Confirmed by inspection, not assumed |

---

## Definition of done

Phase 0 met its own exit criteria (see above). The list below covers the feature
as a whole; nothing here is satisfied yet by phase 0 alone.

- `make lint test` clean; `go test -race ./internal/provider/...` clean.
- `gofmt`, `golint`, `govulncheck` pass before `git add` (repo pre-add rule).
- Live-gated tests pass against OpenCode 1.18.7 and codex 0.145.0.
- No test of a wire shape asserts against a literal the test itself built — the
  lesson of Finding 5. Drive `create` / `resume` / `prompt` and read the frame.
- Manual end-to-end, per provider: start a session → switch to `auto` →
  confirm → prompt something that requests permission → **no sheet**, notice
  line present in the transcript, action performed → switch back to
  `build` / `read-only` → next request **does** prompt.
- Manual fail-safe check: arm auto, kill the engine mid-turn, confirm the
  session surfaces a sheet or an error rather than hanging silently.

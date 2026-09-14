# Grok ACP parity: implementation plan

**Status:** Proposed
**Date:** 2026-07-27
**Decision:** [MADR 0039](./0039-MADR-grok-acp-parity.md)
**Verified targets:** grok 0.2.112 (9bbd559437). Re-probe phase 0 before
accepting newer versions.
**Assessment:** [0038-MADR-grok-acp-parity-assessment.md](./0038-MADR-grok-acp-parity-assessment.md)
— the wire probe whose findings drive this plan.

## Goal and non-goal

Take the six grounded gaps from the assessment: mid-session model switch
(D1), live model catalog (D2), `deep-research`/`workflow` canonical
commands (D3), seven typed policy config fields (D4), MCP lifecycle status
forwarding (D5). Pin every external fact with a live-tagged test.

**Non-goals.** No `grok agent serve` transport, no `_x.ai/hooks`
consumption, no TUI/telemetry notification consumption, no `--worktree`
or `--sandbox` work, no `--max-turns` / `--json-schema` / `--rules` /
`--system-prompt-override` (deferred in MADR 0039 §2.6).

---

## Dependency order

D2 and D5 share the extension-notification hook (E1) — build that once, in
phase 2, before either consumer. D3 is independent — build in parallel with
phase 2 if staffing allows.

```
Phase 0 (pin wire contracts)                 →  live-test gate
                                                │
Phase 1 (D1 — set_model + ModelSession)      ─┐
Phase 2 (D2 — live catalog)  ◀── depends E1  ├─→ Phase 6 (docs + verify)
Phase 3 (D3 — deep-research/workflow)        ─┤     (after D5)
Phase 4 (D4 — policy config fields)          ─┤
Phase 5 (D5 — MCP status)     ◀── depends E1  ─┘
```

Concretely:

- E1 is in phase 2 (first consumer is `_x.ai/models_update`); phase 5
  registers `_x.ai/mcp/server_status` and `_x.ai/mcp_initialized` against
  the hook built in phase 2.
- Phases 1, 3, 4 are mutually independent and can run concurrently.

---

## Phase 0 — P1: pin the wire contracts with live tests

Per AGENTS.md ("CLI behaviour changes silently, and an assumption with no
test is a future bug report"), every behavioral claim later phases rely on
becomes a live-tagged test under `internal/provider/grok/`. These run under
`go test -tags live_grok ./...` (AGENTS.md "Tests" section). They spend no
real tokens — they only handshake and send control RPCs.

1. `live_setmodel_test.go` — runs the new `set_model` flow:
   `initialize → session/new → session/set_model {modelId:"grok-4.5"}` and
   asserts the result shape `{"_meta":{"model":{"Ok":"<id>"}}}`. Then sends
   `session/set_model {"modelId":"grok-nonexistent"}` and asserts
   `{code:-32602, "unknown model id"}`. Documents the wire contract D1
   depends on.

2. `live_initializemeta_test.go` — runs `initialize` only and asserts that
   `_meta.modelState.currentModelId` and `_meta.modelState.availableModels`
   are present in the raw response frame. Since the SDK drops `_meta`, this
   test must parse the raw JSON-RPC frame; an `acpagent` package-private raw
   hook (E0 below) is the natural probe point. Phase 0 builds E0.

3. `live_commandcatalog_test.go` — runs `initialize`, captures
   `availableCommands`, and asserts that `deep-research`, `workflow`, and
   `goal` are present. Pins D3.

4. `live_policyflags_test.go` — runs `grok --help` and `grok agent --help`
   and asserts `--permission-mode`, `--tools`, `--disallowed-tools`,
   `--allow`, `--deny`, `--no-subagents`, `--disable-web-search` appear.
   Pins D4.

5. `live_mcpnotifications_test.go` — runs `initialize → session/new` with
   a stdio MCP configured and asserts `_x.ai/mcp/server_status` arrives
   for the configured server. Pins D5's wire source.

6. `live_setmodel_test.go` already pins `_x.ai/models_update`'s existence in
   a real turn — no separate test needed, but add an assertion that the
   notification appears when the model list changes within the turned
   session. If the notification does not fire deterministically on a single
   set_model, document the trigger and adjust phase 2 (D2) accordingly.

**Gate:** phases 1, 2, 5 each depend on a wire fact this phase pins. Fix
the live tests first; do not implement against a contract phase 0 cannot
find.

### E0 — raw JSON-RPC hook for `acp.ClientSideConnection`

Both D1 (`session/set_model`) and D2/D5 (reading `_meta` from `initialize`
and consuming extension notifications) need parts of the wire the typed SDK
does not surface.

E0 lives in `internal/provider/acpagent/` as a small package-private
helper:

```go
// rawRequest sends a JSON-RPC request and decodes the result into out.
// Used for methods acp-go-sdk@v0.13.5 does not model (session/set_model).
func (s *session) rawRequest(ctx context.Context, method string, params any, out any) error
```

Implementation accesses `s.conn`'s underlying framer the way `acphttp`'s
`fr.sendRequest` does (`acphttp/provider.go:147`). The SDK exposes the
`ClientSideConnection` field structure enough to do this; verify in the
first phase-0 commit and adjust if the SDK has refactored. Restraint: do
not export this. Stdio ACP is the only consumer; exposing it through
`provider.Provider` would force every provider to think about raw RPC.

E0 is the only shared primitive. Phases 1, 2, 5 each call it; the
extension-notification dispatch (E1) is built on E0.

---

## Phase 1 — P1: D1 mid-session `/model` switch

**Files:**
- `internal/provider/acpagent/acpagent.go` (add `ModelSession` interface assertion)
- `internal/provider/acpagent/session.go` (add `SetModel`)
- `internal/provider/acpagent/session_test.go` (unit test)
- `internal/provider/grok/grok.go` (rewrite doc-comment, drop "no stable
  list API" claim)
- `internal/provider/grok/commandtable.go` (remap `model` to
  `KindOp OpSetModel`)
- `internal/provider/grok/live_setmodel_test.go` (already exists from phase 0;
  keep the live tag)
- `internal/session/commands.go` (verify `cmdModel` in-place path activates
  for grok — no source change expected; the interface check is at line 726)

**Steps:**

1. Using E0, add:

   ```go
   func (s *session) SetModel(ctx context.Context, model string) error {
       s.mu.Lock()
       agentID := s.agentID
       closed := s.closed
       s.mu.Unlock()
       if closed {
           return fmt.Errorf("session closed")
       }
       var resp struct {
           Meta struct {
               Model struct {
                   Ok   string `json:"Ok,omitempty"`
                   Err  string `json:"Err,omitempty"`
               } `json:"model"`
           } `json:"_meta"`
       }
       if err := s.rawRequest(ctx, "session/set_model",
           map[string]any{"sessionId": agentID, "modelId": model}, &resp); err != nil {
           return err
       }
       // grok returns {"_meta":{"model":{"Ok":"<id>"}}} on success; an
       // unknown id surfaces as -32602 Invalid params (the SDK error
       // carries "unknown model id"). Pass both through unchanged.
       if resp.Meta.Model.Err != "" {
           return fmt.Errorf("set_model: %s", resp.Meta.Model.Err)
       }
       if resp.Meta.Model.Ok != "" && resp.Meta.Model.Ok != model {
           s.log.Warn("set_model accepted a different id",
               slog.String("requested", model),
               slog.String("accepted", resp.Meta.Model.Ok))
       }
       return nil
   }
   ```

2. Add the interface assertion at the bottom of
   `internal/provider/acpagent/session.go`:

   ```go
   var _ provider.ModelSession = (*session)(nil)
   ```

3. In `internal/provider/grok/commandtable.go` change the `model` entry
   (and rewrite the comment to retract the wrong claim):

   ```go
   // model switches the live model mid-session via ACP session/set_model
   // (verified live against grok 0.2.112; MADR 0039 D1). grok validates
   // the id against its live model list and rejects unknown ids.
   "model": {Kind: command.KindOp, Op: command.OpSetModel},
   ```

4. In `internal/provider/grok/grok.go` rewrite the `staticModels` comment
   (lines 18-25) to retract the "no stable list API" claim. The static list
   becomes the no-session fallback only (D2 in phase 2 makes the live list
   authoritative).

5. Unit test: `session_test.go` — fake the raw-RPC reply and assert
   `SetModel` parses the outcome, rejects an error, and returns nil on a
   clean `Ok`. Same shape as the existing `autoAllow` test
   (`session_test.go:17-30`).

6. Live test: confirm `live_setmodel_test.go` (phase 0) passes end-to-end
   with the real grok binary.

**Gate:** `cmdModel` at `internal/session/commands.go:726` now resolves to
the in-place path for grok. Manual: `/model grok-4.5` against a live grok
session does not relaunch and the conversation continues. Live-tagged
test verifies.

**No mobile change.** `cmdModel`'s in-place branch is the same code path
OpenCode and Codex already use.

---

## Phase 2 — P1: D2 live model catalog

**Files:**
- `internal/provider/acpagent/acpagent.go` (initialize response capture;
  provider catalog cache; `Spec.ListModels` use)
- `internal/provider/acpagent/extensions.go` (E1 hook — generic extension-
  notification dispatcher)
- `internal/provider/grok/grok.go` (install `_x.ai/models_update` handler,
  `Spec.ListModels` provider-side cache reader)
- `internal/picker/picker.go` (likely no change — `MergeLiveStatic`
  already exists)
- `internal/provider/grok/live_modelrefresh_test.go` (new live test)

### E1 — extension-notification dispatch hook

`acp.Client` already implements `HandleExtensionMethod` (requests); the SDK
drops extension notifications silently (`extensions.go:75-76`). Add a
package-private dispatcher on `*acp.ClientSideConnection`:

```go
// extensionNotificationHandler is invoked for an extension notification
// the SDK would otherwise drop. Default no-op; providers register via
// session.registerExtensionNotification.
type extensionNotificationHandler func(ctx context.Context, method string, params json.RawMessage)
```

Hook point: the SDK's notification router (verify exact method name in
`acp-go-sdk@v0.13.5`'s `connection.go`; if not overridable through the
existing `acp.Client` interface, the dispatch is built atop E0 with a side
channel — phase 0 will pin this as part of E0's design).

The registry is per-session, populated by `Spec.ExtensionNotifications
map[string]extensionNotificationHandler` so providers that do not register
keep the no-op default. grok registers:

```go
"_x.ai/models_update":      handleModelsUpdate,
"_x.ai/mcp/server_status":  handleMCPStatus,   // phase 5
"_x.ai/mcp_initialized":    handleMCPInit,     // phase 5
```

### 2.1 Parse `initialize` `_meta`

At `acpagent.go:218-232`, alongside the typed `initResp`, decode the raw
frame (E0) to extract:

```go
type grokInitializeMeta struct {
    ModelState struct {
        CurrentModelID   string `json:"currentModelId"`
        AvailableModels  []struct {
            ModelID string `json:"modelId"`
            Name    string `json:"name"`
            Meta    struct {
                TotalContextTokens    int    `json:"totalContextTokens"`
                SupportsReasoningEffort bool  `json:"supportsReasoningEffort"`
                ReasoningEffort       string `json:"reasoningEffort"`
                ReasoningEfforts      []struct {
                    ID    string `json:"id"`
                    Label string `json:"label"`
                } `json:"reasoningEfforts"`
            } `json:"_meta"`
        } `json:"availableModels"`
    } `json:"modelState"`
}
```

This is intentionally minimal — only the fields D2 needs. Extend in future
MADRs as more of `_meta` becomes useful. Decode defensively: grok pins the
shape today, future grok may add or rename.

### 2.2 Provider-side cache

Add to `Provider` in `acpagent.go`:

```go
type Provider struct {
    // ...existing fields...
    catalogMu     sync.RWMutex
    catalogCache  picker.Catalog   // last parsed live catalog, or zero
    catalogHas    bool             // set once initialize has populated it
}
```

`spawnAgent` updates the cache after `initialize` succeeds. The cache is
read by `Spec.ListModels`:

```go
ListModels: func(ctx context.Context, cfg Config) (picker.Catalog, error) {
    p.catalogMu.RLock()
    defer p.catalogMu.RUnlock()
    if p.catalogHas {
        return p.catalogCache.Clone(), nil
    }
    return picker.Catalog{}, nil   // fall through to static + allowCustom
},
```

The existing `Provider.ListModels` (`acpagent.go:138-152`) already merges
live + static; the static fallback covers the "no session yet" case.

### 2.3 `_x.ai/models_update` handler

```go
func handleModelsUpdate(ctx context.Context, s *session, params json.RawMessage) {
    var p struct {
        AvailableModels []grokAvailableModel `json:"availableModels"`
        CurrentModelID  string               `json:"currentModelId"`
    }
    if err := json.Unmarshal(params, &p); err != nil {
        s.log.Debug("models_update: parse failed", slog.String("err", err.Error()))
        return
    }
    cat := modelsToCatalog(p.CurrentModelID, p.AvailableModels)
    s.provider.catalogMu.Lock()
    s.provider.catalogCache = cat
    s.provider.catalogHas = true
    s.provider.catalogMu.Unlock()
}
```

**Phase 0 gate:** before this lands, the live test
(`live_modelrefresh_test.go`) must capture a real `_x.ai/models_update`
frame and assert the payload shape. If grok does not push the notification
on a single `session/set_model` (likely — it fires on changes to the
server-side list, not per-call), the test instead asserts the notification
arrives when grok's daemon pushes an update and documents the trigger
window in the test comment. D2 is still useful without it — the
`initialize` parse path is enough for the catalog wrong-default fix. The
notification subscription is forward-looking and silent if no frames
arrive.

### 2.4 Spec wiring

`internal/provider/grok/grok.go`:

```go
var spec = acpagent.Spec{
    // ...existing fields...
    ListModels: func(ctx context.Context, cfg acpagent.Config) (picker.Catalog, error) {
        // Provider owns the cache; this hook is the live path.
        // ...handled via a closure over the Provider...
    },
    ExtensionNotifications: map[string]acpagent.ExtensionNotificationHandler{
        "_x.ai/models_update": handleModelsUpdate,
    },
}
```

The closure binding the Provider's catalog to the live `ListModels` hook
requires the Spec to carry a `*Provider` reference or for `ListModels` to
be a method receiver instead of a Spec field. Prefer the latter: move
`ListModels` to `Provider.ListModels` (already exists at
`acpagent.go:138-152`) and have it consult the cache first; drop the
`Spec.ListModels` field. This is a small refactor, but the alternative —
passing `*Provider` into `Spec.ListModels` — creates a cycle the type
system will not allow.

**Gate:** the existing `TestPickerMergeLiveStatic` and the static-catalog
tests still pass; the live catalog test asserts `grok-4.5` is the default
picker entry when the host reports it, and `grok-code-fast-1` is absent.

---

## Phase 3 — P2: D3 canonical `deep-research` and `workflow` commands

**Files:**
- `internal/command/specs.go` (add the two entries)
- `internal/command/command_test.go` (table tests for `Resolve`)
- `internal/provider/grok/commandtable.go` (optional: declare them
  explicitly as `KindNative` instead of relying on the agent-owns path —
  preference below)
- `internal/command/conformance_test.go` (update if it iterates `Specs`)
- `internal/session/commands.go` (no source change; `available_commands`
  event already forwards the agent's list)

**Steps:**

1. Add to `internal/command/specs.go` after the `goal` entry:

   ```go
   {
       Name:        "deep-research",
       Args:        "<query>",
       Description: "Research with bounded parallel agents, cross-check evidence, and write a cited report",
       Default:     Mapping{Kind: KindNative, Native: "deep-research"},
   },
   {
       Name:        "workflow",
       Args:        "<name> [args] | pause|resume|stop|save [name]",
       Description: "Launch a saved workflow, or manage a run",
       Default:     Mapping{Kind: KindNative, Native: "workflow"},
   },
   ```

2. Decide: declare in `grok/commandtable.go` or rely on the undeclared
   agent-owns path (`command.go:199-208`)? Declare explicitly — it makes
   the contract visible and lets the grok `commandCaveat` apply uniformly:

   ```go
   "deep-research": {Kind: command.KindNative, Native: "deep-research"},
   "workflow":      {Kind: command.KindNative, Native: "workflow"},
   ```

3. Table test in `command_test.go`:

   - `Resolve("deep-research", grokTable, state{AgentCommands:["deep-research"]})` → `Available: true`.
   - `Resolve("deep-research", gooseTable, state{AgentCommands:[]})` → `Available: false, Reason: "this agent doesn't offer it"`.

4. Live test (`live_commandcatalog_test.go` from phase 0) now also asserts
   `/help` output includes `/deep-research` and `/workflow` for grok
   (extend the phase 0 test rather than adding a new one).

**Non-goal:** the subcommand policies for `workflow` (`pause`/`resume`/
`stop`/`save`) are grok's to enforce on receipt of `/workflow pause foo`.
The canonical Spec only models free-form `Args` for help text; mobile
input is plain text.

**Gate:** `/help` on a grok session lists `/deep-research <query>` and
`/workflow <name> [args] | pause|resume|stop|save [name]`; typing them
runs them (forwarded as session/prompt with the slash text).

---

## Phase 4 — P2: D4 typed policy config fields

**Files:**
- `internal/provider/acpagent/config.go` (add the seven fields)
- `internal/provider/grok/grok.go` (append flags in `defaultArgs`; carry
  through `ModelArgs`)
- `internal/config/config.go` (add the seven config keys; validation)
- `internal/config/config_test.go` / `internal/config/acp_config_test.go`
  (validation tests)
- `internal/daemon/daemon.go` (copy fields from
  `GrokProviderConfig` to `acpagent.Config`)
- `docs/config.md` (operator docs — phase 6)
- `internal/provider/grok/live_policyflags_test.go` (from phase 0 — flags)

**The seven fields and flags:**

| Config field (Go)           | Config key (YAML)                  | Flag (repeatable means append multiple)  |
| --------------------------- | ---------------------------------- | --------------------------------------- |
| `PermissionMode string`     | `providers.grok.permission_mode`   | `--permission-mode <MODE>`              |
| `AllowedTools []string`     | `providers.grok.allowed_tools`     | `--tools a,b,c` (CSV, single flag)      |
| `DisallowedTools []string`  | `providers.grok.disallowed_tools`  | `--disallowed-tools a,b,c`              |
| `AllowRules []string`       | `providers.grok.allow_rules`        | `--allow <RULE>` (repeatable)           |
| `DenyRules []string`        | `providers.grok.deny_rules`        | `--deny <RULE>` (repeatable)            |
| `NoSubagents bool`          | `providers.grok.no_subagents`      | `--no-subagents`                        |
| `DisableWebSearch bool`     | `providers.grok.disable_web_search`| `--disable-web-search`                  |

**Steps:**

1. Add the seven fields to `acpagent.Config` (`config.go`). Document each
   with the §2.4 rationale: no value validation, grok owns the enums,
   compose with `--always-approve` (permission_mode wins on conflict).

2. Update `grok.go defaultArgs` — rewrite to a builder so the model
   override path (the `ModelArgs` closure at `grok.go:45-51`) carries all
   fields through the rebuild:

   ```go
   func defaultArgs(cfg Config) []string {
       args := []string{"agent", "--no-leader"}
       if cfg.AlwaysApprove {
           args = append(args, "--always-approve")
       }
       if cfg.Model != "" {
           args = append(args, "-m", cfg.Model)
       }
       if cfg.ReasoningEffort != "" {
           args = append(args, "--reasoning-effort", cfg.ReasoningEffort)
       }
       if cfg.PermissionMode != "" {
           args = append(args, "--permission-mode", cfg.PermissionMode)
       }
       if len(cfg.AllowedTools) > 0 {
           args = append(args, "--tools", strings.Join(cfg.AllowedTools, ","))
       }
       if len(cfg.DisallowedTools) > 0 {
           args = append(args, "--disallowed-tools", strings.Join(cfg.DisallowedTools, ","))
       }
       for _, r := range cfg.AllowRules {
           args = append(args, "--allow", r)
       }
       for _, r := range cfg.DenyRules {
           args = append(args, "--deny", r)
       }
       if cfg.NoSubagents {
           args = append(args, "--no-subagents")
       }
       if cfg.DisableWebSearch {
           args = append(args, "--disable-web-search")
       }
       args = append(args, "stdio")
       return args
   }
   ```

3. Update `grok.go ModelArgs` to carry all fields (same pattern as
   `ReasoningEffort` from MADR 0037):

   ```go
   ModelArgs: func(cfg Config, model string) []string {
       return defaultArgs(Config{
           AlwaysApprove:    cfg.AlwaysApprove,
           Model:            model,
           ReasoningEffort:  cfg.ReasoningEffort,
           PermissionMode:   cfg.PermissionMode,
           AllowedTools:     cfg.AllowedTools,
           DisallowedTools:  cfg.DisallowedTools,
           AllowRules:       cfg.AllowRules,
           DenyRules:        cfg.DenyRules,
           NoSubagents:      cfg.NoSubagents,
           DisableWebSearch: cfg.DisableWebSearch,
       })
   },
   ```

   No composition trap remains — every typed field survives the
   rebuild. This is the headline correctness benefit beyond just exposing
   the flags.

4. Add config keys to `internal/config/config.go`'s `GrokProviderConfig`
   with sensible defaults (all empty / false) and an absence of enum
   validation. Document the `permission_mode` × `always_approve`
   interaction (MADR 0039 §2.4): if both are set, `permission_mode` wins
   (more specific).

5. `internal/daemon/daemon.go` at `:108-119` — extend the acpCfg builder
   to copy the seven new fields.

6. Validation tests: `acp_config_test.go` — assert that empty values
   produce no flag (`defaultArgs` byte-identical to today's output); that
   lists reject duplicate-but-not-empty entries on the same kind (mirrors
   Goose's `with_builtins` validation at `config.go:769-780`); that
   `allowed_tools` and `disallowed_tools` reject overlapping entries
   (grok would too; fail cheaply here).

7. Builder unit test (`grok_test.go`) covering `defaultArgs` permutations
   and the `ModelArgs` carry-through rule for each new field. This is the
   regression test for the MADR 0037 §1.2 trap: any future added field
   must be added to ModelArgs's rebuild or this test fails.

**Gate:** `go test ./internal/provider/grok/...` green;
`go test -tags live_grok ./internal/provider/grok/...` green; manual
config with `providers.grok.permission_mode = "acceptEdits"` produces a
grok session that does not prompt for edit approval; manual config with
`providers.grok.allow_rules = ["Bash(git status)"]` auto-allows that and
nothing else.

**No mobile change.** Operators set these; users never see them.

---

## Phase 5 — P3: D5 MCP lifecycle status forwarding

**Files:**
- `internal/provider/provider.go` (add `MCPStatusSession`)
- `internal/provider/acpagent/session.go` (snapshot field, register
  notifications — handler from phase 2's E1)
- `internal/provider/grok/grok.go` (already registered in phase 2's
  `ExtensionNotifications` map)
- `internal/session/commands.go` (the `Diagnostics` path — check the
  optional interface)
- `internal/provider/grok/live_mcpnotifications_test.go` (from phase 0 — pins
  the wire)

**Steps:**

1. Add to `internal/provider/provider.go`:

   ```go
   // MCPStatusSession optionally exposes per-MCP-server connection state.
   // The session keeps a snapshot updated from agent lifecycle notifications;
   // polled by Diagnostics — pull-shaped rather than event-shaped because
   // MCP state changes twice per session (init, then idle).
   type MCPStatusSession interface {
       Session
       MCPStatus(ctx context.Context) ([]MCPServerStatus, error)
   }
   ```

   `MCPServerStatus` already exists (`provider.go:186-190` — `Name`,
   `State`).

2. In `acpagent.session` add:

   ```go
   mcpMu     sync.Mutex
   mcpStatus []provider.MCPServerStatus
   ```

   The E1 handlers (registered in phase 2's grok `ExtensionNotifications`
   map) update this snapshot:

   ```go
   func handleMCPStatus(ctx context.Context, s *session, params json.RawMessage) {
       var p struct {
           Name   string `json:"name"`
           Status string `json:"status"`
           Reason string `json:"reason"`
       }
       if err := json.Unmarshal(params, &p); err != nil { return }
       s.mcpMu.Lock()
       defer s.mcpMu.Unlock()
       // upsert by name
       ...
   }
   func handleMCPInit(ctx context.Context, s *session, params json.RawMessage) {
       var p struct{ MCPToolCount int `json:"mcpToolCount"` }
       // marks all servers ready if status was not yet reported
       ...
   }
   ```

3. `session.MCPStatus(ctx)` returns the snapshot copy.

4. `internal/session/commands.go` Diagnostics path: check for
   `MCPStatusSession` and include if present. The existing
   `DiagnosticsSession` check at `commands.go` (around line ~62) is the
   precedent — add a second check.

5. Live test (`live_mcpnotifications_test.go`) asserts: configure `gopls
   stdio` MCP locally, start a grok session, assert `_x.ai/mcp/server_status`
   arrives with `status:"ready"` and a `name` matching the configured
   server; the session's `MCPStatus()` then returns the same.

**Gate:** `client/diagnostics` returns live MCP connection state for grok
sessions; existing Diagnostics tests still pass for OpenCode/Goose/Codex
which do not implement `MCPStatusSession` (the optional check skips
them).

---

## Phase 6 — docs and verify

**Files:**
- `docs/config.md` (operator docs for the seven new config keys, with
  the `permission_mode` × `always_approve` interaction note)
- `README.md` (grok provider section — add `/model` mid-session switch
  and `/deep-research`, `/workflow` to the supported-commands list)
- `docs/0039-MADR-grok-acp-parity.md` (fill in the Implementation Record
  block at the bottom)
- `docs/agent_cli_slash_commands_matrix.md` (add `deep-research`,
  `workflow` rows for grok)

**Verify gate:** the AGENTS.md pre-add rule applies. Before any commit that
touches Go:

```bash
make pre-add-check                # every tracked Go file
make pre-add-check FILES="..."     # or just the changed ones
go test -race ./...
go test -tags live_grok ./...      # where real CLI skins are exercised
```

Live-tagged tests spend real tokens — run them once at acceptance per phase,
not in a loop (AGENTS.md "Tests"). The phase 0 tests are the cheapest
(no turn, just handshake + control RPC) and should run in CI.

---

## Tracking checklist

Phase owners update this as phases land (MADR 0039 §6 Implementation
Record backs it up):

- [x] Phase 0 — five live-tagged tests pinning the wire contracts (E0 raw hook)
- [x] Phase 1 — D1 `session/set_model` + `ModelSession` + command remap
- [x] Phase 2 — D2 live catalog parse + provider cache + E1 extension-notification dispatch
- [x] Phase 3 — D3 `deep-research` + `workflow` canonical specs
- [x] Phase 4 — D4 seven typed policy config fields, full `ModelArgs` carry-through
- [x] Phase 5 — D5 MCP status interface + handlers from E1
- [x] Phase 6 — operator docs, README, MADR record, slash-commands matrix
- [x] Verify — `make pre-add-check`, `go test -race ./...`, `go test -tags live_grok ./...` all green
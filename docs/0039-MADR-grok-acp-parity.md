# MADR 0039: Grok ACP parity — mid-session model switch, live catalog, and CLI policy surfaces

- **Status**: Proposed
- **Date**: 2026-07-27
- **Deciders**: Project Owner (scope, phasing); Implementer (wire contract, SDK limits)
- **Related**:
  - [Assessment](./0038-grok-acp-parity-assessment.md) — the wire probe whose
    findings this MADR turns into decisions
  - [MADR 0004](./0004-phase2-grok-acp.md) — original Grok ACP provider
  - [MADR 0022](./0022-plan-mode-parity.md) — plan-mode parity (mode contract)
  - [MADR 0023](./0023-canonical-slash-commands.md) — canonical slash commands
  - [MADR 0031](./0031-opencode-catalog-and-metadata-parity.md) — live model
    catalog merge policy for OpenCode (the precedent being extended here)
  - [MADR 0036](./0036-protocol-contract-completeness.md) — edge cases of the
    remote protocol
  - [MADR 0037](./0037-cli-capability-uptake.md) — typed CLI flag uptake
- **Evidence**: `grok 0.2.112 (9bbd559437)` probed live on 2026-07-27. Wire
  frames captured to `/tmp/g3.out`, `/tmp/g5.out`, `/tmp/g6.out` during the
  assessment; the decisive frames are cited inline below.
- **Companion plan**: [0039-plan-grok-acp-parity.md](./0039-plan-grok-acp-parity.md)

---

## 1. Problem

The assessment ([0038](./0038-grok-acp-parity-assessment.md)) probed grok
0.2.112 headless as `grok agent --no-leader stdio` — exactly what the daemon
launches (`internal/provider/grok/grok.go:72-86`) — and compared what grok
actually advertises against what the codebase reads. Three findings overturn
current decisions; the rest are new CLI policy surfaces not yet exposed.

### 1.1 `session/set_model` works, but the daemon relaunches on `/model`

`internal/provider/grok/commandtable.go:19-22` states:

> ACP has no set-model call and grok exposes none as an extension, so a model
> change means relaunching the agent (the daemon's own path).
> `"model": {Kind: command.KindDaemon},`

`internal/provider/grok/grok.go:18-25` repeats the assumption as the reason
the picker uses a static model list.

**Probe evidence (decisive):** `session/set_model` with
`{"sessionId":…,"modelId":"grok-4.5"}` returned `{"_meta":{"model":{"Ok":"grok-4.5"}}}`;
`"grok-3"` returned `{code:-32602,"unknown model id"}`. The method is
standard JSON-RPC (no `_x.ai/` prefix), it validates the id against the live
list, and it switches without dropping session context. The codebase claim in
both files is simply wrong.

**Cost:** `/model` degrades to a daemon-side relaunch
(`internal/session/commands.go` cmdModel path) because
`acpagent.session` does **not** implement `provider.ModelSession`
(`internal/provider/provider.go:210-213`) — so `state.Ops[OpSetModel]` is
false (`internal/session/commands.go:62`) and the model command falls back
to the table's `KindDaemon` mapping. The user loses the conversation and the
prewarmed spare.

**SDK limit:** `acp-go-sdk@v0.13.5` has no `SetSessionModelRequest`
(`grep SetSessionModel ~/go/pkg/mod/github.com/coder/acp-go-sdk@v0.13.5/*.go`
— zero hits), and `NewSessionRequest` has no `mode` field either
(`acp-go-sdk@v0.13.5/types_gen.go:3236-3269`). The typed SDK is not going to
carry this for us.

### 1.2 The model catalog is wrong against the live host

`internal/provider/grok/grok.go:20-25`:

```go
var staticModels = []picker.Option{
    {ID: "grok-code-fast-1", …},
    {ID: "grok-4", …},
    {ID: "grok-3", …},
    {ID: "grok-3-mini", …},
}
```

The Spec has no `ListModels`, so `Provider.ListModels`
(`internal/provider/acpagent/acpagent.go:138-152`) returns **only** this list.

**Probe evidence:** this install's `initialize` returned
`_meta.modelState.currentModelId = "grok-4.5"` and
`availableModels = [{"modelId":"grok-4.5", "_meta":{"totalContextTokens":500000,…}}]`.
The picker's first entry (`grok-code-fast-1`) is not in the live list; the
live default (`grok-4.5`) is not in the picker catalog. A `/model` relaunch
to "grok-code-fast-1" would fail today with "unknown model id".

grok also sends `_x.ai/models/update` when the catalog changes mid-session
(seen in `/tmp/g5.out`) — nothing subscribes (grep clean).

### 1.3 grok's slash-command catalog includes commands the canonical table ignores

The probe's `availableCommands` for grok: `compact`, `always-approve`,
`context`, `session-info`, `deep-research`, `workflow`, `goal`. Of these:

- `compact` is locked to `KindNone` (`grok/commandtable.go:26-29`) — correct,
  it returns nothing over ACP.
- `context` → `session-info` (`commandtable.go:24`), `goal` → `goal`
  (`commandtable.go:25`) — accurate.
- **`deep-research` and `workflow` are undeclared.** They fall under the
  "agent advertises this name → owns the command" path
  (`internal/command/command.go:199-208`), so a user can type them — but
  `/help` does not list them and they are not in `specs.go`'s canonical
  vocabulary (`internal/command/specs.go:11-83`).

These are valuable grok-owned commands (bounded parallel subagents and a
workflow runner) that the daemon has no daemon-side equivalent for. They
should be promoted to first-class slash commands.

### 1.4 grok's richer permission policy model is mostly unexposed

`grok --help` exposes `--permission-mode` (six modes: `default`,
`acceptEdits`, `auto`, `dontAsk`, `bypassPermissions`, `plan`), `--tools`,
`--disallowed-tools`, `--allow`/`--deny` rules, `--no-subagents`,
`--disable-web-search`, `--max-turns`, `--json-schema`, `--rules`, and
`--system-prompt-override`. The daemon currently surfaces only
`always_approve` (`Config.AlwaysApprove`), `model`, `reasoning_effort`
([0037](./0037-cli-capability-uptake.md), just landed), `bin`, `args`,
`default_cwd`, `mcp_servers`, and `permission_timeout_seconds`. Everything
else is reached — clumsily — through the raw `args` override, which the
MADR 0037 §1.2 trap shows is unsafe: `ModelArgs` rebuilds from `defaultArgs`
and discards `cfg.Args`.

The high-value policy surfaces are:

- `--permission-mode` — rounds `always_approve` out into the six real modes
  the agent already enforces.
- `--tools` / `--disallowed-tools` — agent tool whitelist/blacklist.
- `--allow` / `--deny` — persistent permission rules that reduce phone-prompt
  noise without turning on full AlwaysApprove.
- `--no-subagents` — disable subagent spawning (grok advertises
  `general-purpose`, `explore`, `plan` agents — the 27 `_x.ai/session_notification`
  frames in `/tmp/g5.out` are subagent activity).
- `--disable-web-search` — disable grok's built-in web tool.

The remaining flags (`--max-turns`, `--json-schema`, `--rules`,
`--system-prompt-override`) carry product implications of their own and are
deferred (§2.6).

### 1.5 MCP lifecycle notifications are dropped

grok emits `_x.ai/mcp/servers_updated`, `_x.ai/mcp/init_progress`,
`_x.ai/mcp/server_status`, `_x.ai/mcp_initialized`. None are consumed (grep
clean). The daemon's `provider.Diagnostics.MCP`
(`internal/provider/provider.go:186-190`) models per-server state from a
probe snapshot; the live notifications would turn it into a live view at
small cost.

### 1.6 What the assessment ruled out — recorded so it is not re-proposed

These were considered against the probe and explicitly **not** taken. Each
appears with reasoning in [0038 §2.1, §3.5, §3.6, §3.7](./0038-grok-acp-parity-assessment.md):

- Wiring `grok agent serve` (WebSocket transport): the per-session process
  model is right; the daemon already speaks the framer for Goose
  (`internal/provider/acphttp/ws.go`), but a grok `acphttp` variant would pay
  complexity for no isolation benefit.
- Consuming `_x.ai/hooks` (grok's blocking-hook surface): predates a
  daemon-side policy layer; needs its own MADR.
- Consuming `_x.ai/session_notification`, `_x.ai/sessions/changed`,
  `_x.ai/queue/changed`, `_x.ai/announcements/update`, `_x.ai/settings/update`,
  `_x.ai/session/prompt_complete`: TUI or telemetry surfaces, no UX yet.
- `cancelRewind`, `sessionRecap`, `voiceMode`, `grokShell`: TUI-only.
- `--worktree`, `--rules`: TUI-only (probed in MADR 0037 §1.3).
- `--sandbox`: different threat model from the daemon's `FSRoots`
  confinement (`session.go:1457-1479`).

---

## 2. Decision

Take the six items the assessment ranked P1–P3, plus MCP lifecycle as a
P3. Defer three flag surfaces that imply product decisions beyond scope.
Record the rest as intentionally not taken.

### 2.1 D1 — Mid-session `/model` switch via `session/set_model` (P1)

**The mechanism:** add a raw JSON-RPC send hook on
`*acp.ClientSideConnection` (the SDK exposes the framed connection but no
`set_model` typed call) and implement `provider.ModelSession` on
`acpagent.session`:

```go
func (s *session) SetModel(ctx context.Context, model string) error {
    // session/set_model — verified live against grok 0.2.112 (0038 §3.1)
    return s.rawRequest(ctx, "session/set_model", map[string]any{
        "sessionId": s.agentID,
        "modelId":   model,
    })
}
```

The raw-RPC helper resembles `acphttp`'s `fr.sendRequest`
(`internal/provider/acphttp/provider.go:147`); lift a typed equivalent into
the stdio path inside `internal/provider/acpagent/` (not exported to
providers — ACP stdio is the only consumer).

**Re-map the command.** In `internal/provider/grok/commandtable.go` change:

```go
"model": {Kind: command.KindDaemon},
```

to:

```go
"model": {Kind: command.KindOp, Op: command.OpSetModel},
```

mirroring `codex/commandtable.go:17` and `opencode/commandtable.go:22`. With
`provider.ModelSession` implemented, `state.Ops[OpSetModel]` becomes true
and `cmdModel` runs `SetModel` in place instead of relaunching
(`internal/session/commands.go:726`).

**Correct the doc-comments.** Both `grok.go:18-25` ("Grok Build does not
expose a stable list API over ACP") and `commandtable.go:19-22` ("ACP has no
set-model call and grok exposes none") are wrong against the probe and
became the bug report. They will be rewritten in the same change.

**Live probe pin.** Per AGENTS.md ("an assumption with no test is a future
bug report"), add a live-tagged test in `grok/live_command_test.go` that
sends `session/set_model`, asserts a successful `Ok` result, and documents
the validation failure on an unknown id. Matches the existing
`live_mode_test.go` shape for `session/set_mode`.

**Why not the SDK upgrade path:** `acp-go-sdk@v0.13.5` modelling
`session/set_model` is upstream's call, not ours, and waiting on it blocks
the fix. The raw-RPC hook is the smallest change and the one
`acphttp` already uses. When the SDK does add `SetSessionModelRequest`,
swap the implementation behind the same `SetModel` interface — no caller
change.

### 2.2 D2 — Live model catalog from `_meta.modelState` (P1)

**Parse the initialize response.** `initialize` returns `_meta.modelState`
verbatim (probe §2.2 of assessment). Parse:

- `modelState.currentModelId` → the picker's `Default` for the catalog.
- `modelState.availableModels[].{modelId, name, _meta.totalContextTokens,
  _meta.reasoningEfforts}` → the picker's `Options`.

The SDK does not surface `_meta` at the top level of `InitializeResponse`, so
this needs a raw-JSON path alongside the typed `initResp` (same measurement
technique the entire `_meta` block is fetched with in
`extensions.go:91-95` for `exit_plan_mode`).

**Add a `Spec.ListModels` hook for grok.** The merge logic in
`acpagent.go:144-151` already prefers live + static via
`picker.MergeLiveStatic`:
1. If live succeeds → `MergeLiveStatic(live, static)`.
2. If live fails → static (existing behaviour).

So adding the hook makes the live catalog the primary source, with the
existing static list as a fallback when the host reports nothing — exactly
the [0031](./0031-opencode-catalog-and-metadata-parity.md) policy applied to
OpenCode, now extended to grok.

**Keep the static list as a fallback.** The probe shows the static list is
stale, but it is the only catalog available without `initialize` — e.g.
`Provider.ListModels(ctx)` is called by `models.list` independently of any
session (`internal/provider/acpagent/acpagent.go:138-152`). Two options:

1. Spawn a throwaway `initialize`-only process to refresh the catalog on
   `models.list` — costly, and `initialize` is one of the heavier RPCs.
2. Cache the model list from the most recent `initialize`, fall back to
   static.

Option 2 is taken. The cache lives on the `Provider` (one mutex, last-write
wins), updated whenever any session completes `initialize`. `models.list`
returns the cached live catalog if present, else the static fallback. This
preserves the "list models without starting a session" semantic at zero added
cost.

**Subscribe to `_x.ai/models/update`.** When grok pushes a new model list
mid-session, refresh the provider's cache from the notification payload. The
SDK drops extension notifications silently (`extensions.go:75-76`), so this
requires plumbing a handler hook in `acpagent` for extension notifications
the way `HandleExtensionMethod` exists for extension requests. The hook is
generic (keyed by method name, defaulted to no-op); grok registers
`_x.ai/models/update` only.

### 2.3 D3 — Promote `deep-research` and `workflow` to canonical commands (P2)

Add to `internal/command/specs.go`:

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

These are agent-owned (`KindNative`), so providers that do not advertise
them simply do not offer them (`command.go:228-247` — `available()` returns
false when `s.AgentCommands` does not contain the name). Grok advertises
both; the daemon forwards as a normal `session/prompt` with text like
`/deep-research <query>`, same as today — but `/help` and the client's
autocomplete now include them.

`workflow` takes a structured hint (`pause|resume|stop|save [name]`); the
canonical Spec only models free-form `Args` for help text, which is enough.
The subcommand policies (`pause`/`resume`/`stop`/`save`) are grok's to
enforce on receipt.

### 2.4 D4 — Expose grok's policy flags as typed config (P2)

Add the following typed fields to `acpagent.Config` and
`providers.grok.*` config, all appended in `grok.go defaultArgs` (with
carry-through in `ModelArgs`, per the [0037 §1.2](./0037-cli-capability-uptake.md)
composition rule):

| Field                         | Config key                         | Flag                     | Semantics |
| ----------------------------- | ---------------------------------- | ------------------------ | --------- |
| `PermissionMode string`       | `providers.grok.permission_mode`   | `--permission-mode <MODE>` | One of `default`, `acceptEdits`, `auto`, `dontAsk`, `bypassPermissions`, `plan`. No enum validation — grok owns the list, and a hard-coded enum breaks the day grok adds a mode. |
| `AllowedTools []string`       | `providers.grok.allowed_tools`     | `--tools <csv>`            | Whitelist of built-in tool names; empty means grok's default. |
| `DisallowedTools []string`   | `providers.grok.disallowed_tools`  | `--disallowed-tools <csv>` | Blacklist of built-in tool names. |
| `AllowRules []string`        | `providers.grok.allow_rules`       | `--allow <RULE>` (repeatable) | Persistent permission allow rules. |
| `DenyRules []string`         | `providers.grok.deny_rules`        | `--deny <RULE>` (repeatable)  | Persistent permission deny rules. |
| `NoSubagents bool`           | `providers.grok.no_subagents`      | `--no-subagents`            | Disable subagent spawning. |
| `DisableWebSearch bool`      | `providers.grok.disable_web_search`| `--disable-web-search`      | Disable grok's built-in web tool. |

These all carry through `ModelArgs` identically to `ReasoningEffort`
([0037](./0037-cli-capability-uptake.md) D1). Default-zero values produce
no flag — byte-identical behaviour when unset.

**Why all seven in one decision:** the mechanical change (Config field →
`defaultArgs` append → `ModelArgs` carry-through) is identical for all seven,
the validation is empty (grok owns every enum), and the phone-side visibility
is zero (these are operator-set policy knobs, surfaced through
`/help` config and config alone). Splitting them into seven MADRs would
multiply ceremony without adding information.

**Composing with `always_approve`:** `--permission-mode bypassPermissions`
and `--always-approve` are near-synonyms. Keep both for backwards
compatibility; if both are set, `permission_mode` wins (it is the richer
flag). Document the interaction in the config doc.

### 2.5 D5 — Forward MCP lifecycle notifications into `Diagnostics.MCP` (P3)

Define an optional `provider.MCPStatusSession` interface in
`internal/provider/provider.go`:

```go
type MCPStatusSession interface {
    Session
    MCPStatus(ctx context.Context) ([]MCPServerStatus, error)
}
```

`acpagent.session` keeps a `[]MCPServerStatus` snapshot updated from
`_x.ai/mcp/server_status` and `_x.ai/mcp_initialized` notifications. The
existing `DiagnosticsSession.Diagnostics` path
(`internal/session/commands.go`) checks the optional interface and returns
it; the `client/diagnostics` polling path stays unchanged.

**Why an interface, not events:** MCP server state changes rarely (twice per
session: init, then idle). An event type would be over-engineered; a polled
snapshot matches `Diagnostics`'s existing pull model and does not touch the
remote protocol.

This needs the same extension-notification hook as D2. Build the hook once,
register both `models/update` and `mcp/server_status` handlers.

### 2.6 Deferred, with reasons

- **`--max-turns <N>`** — needs a per-session `StartOptions.MaxTurns` field
  that the daemon then enforces after `turn_complete` events (since grok
  does not expose a `set_max_turns` RPC). The cross-provider semantics (does
  OpenCode enforce it client-side? does Codex?) need design. Own MADR.
- **`--json-schema <SCHEMA>`** — structured-output constraint at prompt
  time. Implies a new per-prompt `response_format` field on the remote
  protocol, mobile UI, and SDK behaviour across all providers. Out of scope
  here; this is the "agent-as-API" surface and deserves its own MADR.
- **`--rules <RULES>`** — extra rules appended to the system prompt. Useful,
  but the daemon's rules story is currently per-host (`AGENTS.md`); surfacing
  per-session rules touches the prompt assembly semantics and needs design.
- **`--system-prompt-override`** — admin escape hatch; expose via config when
  a real use case appears.

### 2.7 Not taken (intentionally — see assessment §1.6)

Repeated verbatim from [0038 §1.6](./0038-grok-acp-parity-assessment.md):
no `grok agent serve` transport, no `_x.ai/hooks` consumption, no
TUI/telemetry notification consumption, no `cancelRewind`/`sessionRecap`/
`voiceMode`/`grokShell`, no `--worktree`/`--rules` (TUI-only), no `--sandbox`
muddled with `FSRoots`.

---

## 3. Consequences

### 3.1 Positive

- `/model` becomes a free mid-session switch on grok — matching OpenCode and
  Codex — with no conversation loss and no prewarmed-spare waste.
- The model picker stops offering `grok-code-fast-1` on installs where it is
  not a valid model id. The catalog reflects what the host actually accepts.
- `deep-research` and `workflow` become first-class slash commands with
  `/help` entries and autocomplete — grok's strongest agent-side features
  are no longer hidden.
- Operators get grok's actual permission policy model (`--permission-mode`,
  `--allow`/`--deny`) with typed config, the raw-`args` footgun
  ([0037 §1.2](./0037-cli-capability-uptake.md)) closed for these flags too.
- MCP status becomes live rather than snapshot, sharing the extension-
  notification hook with the catalog refresh.

### 3.2 Costs and risks

- **D1 requires a raw-RPC hook on `*acp.ClientSideConnection`.** This is the
  first stdio-path departure from typed SDK calls in `acpagent`. The hook is
  scoped (`session/set_model` only), kept package-private, and the
  implementation plan pins the wire contract with a live test. When the SDK
  adds `SetSessionModelRequest`, the implementation swaps behind the same
  `provider.ModelSession` interface — no caller change.
- **D2 caches the model list on the `Provider`.** A second `initialize` on
  the same provider overwrites the cache; that is correct (last-write-wins)
  but means a stale host reporting an outdated list briefly shadows a
  fresher one. Static fallback handles the no-session case.
- **D2 + D5 require an extension-notification dispatch hook.** The SDK
  currently drops these silently (`extensions.go:75-76`); the new hook is
  generic and defaults to no-op for handlers we do not register. Negligible
  risk of breaking other providers (no other ACP agent emits these names).
- **D4 adds seven config fields.** Ceremony is real: docs, validation,
  `cmdModel`-related tests. The alternative — letting operators reach them
  through raw `args` — is the trap MADR 0037 §1.2 documented. Typed fields
  are the fix.
- **D4 does not expose `--max-turns` / `--json-schema`.** The assessment
  ranked these P3 with product implications; deferring them is a deliberate
  boundary, not an oversight.

### 3.3 Not changed

- No remote protocol change beyond `provider.MCPStatusSession` (an optional
  interface consumed by an existing diagnostics path).
- No mobile UI change. The `/model` switch uses the existing `cmdModel`
  path (`internal/session/commands.go:726`); the policy flags are
  operator-only, surfaced via `/help` config.
- No new event types. `event.TypeUsage`, `event.TypeMode`,
  `event.TypeAvailableCommands` already cover what the new notifications
  feed.
- No change to prewarm, process model, or transport choice.

---

## 4. Rejected

| Rejected | Why |
|---|---|
| Waiting on an SDK upgrade for `set_model` | Blocks the P1 fix on upstream; the raw-RPC hook is small, scoped, and swappable later (§2.1) |
| Per-session `reasoning_effort` | Already rejected in MADR 0037 §2.1 — operator-set, config-level is right |
| Per-session policy flags (permission_mode, allow/deny) | Same reasoning as reasoning effort — operator policy, set once, no mobile UI for it (§2.4) |
| Validating `permission_mode` enum values | grok owns the enum; a hardcoded list breaks when grok adds a mode (mirrors MADR 0037 §2.1) |
| Throwing away the static model catalog | It is the fallback for `models.list` without a session (§2.2) |
| Wiring `grok agent serve` | Per-session process model is right; transports assessment §2.1 |
| Consuming `_x.ai/hooks` now | Needs a policy-engine MADR first; assessment §3.5 |
| Forwarding TUI surfaces (`grokShell`, `voiceMode`, `cancelRewind`, `sessionRecap`) | No remote UX; assessment §3.6 |
| Forwarding telemetry notifications (`announcements`, `settings`, `queue`, `sessions/changed`, `prompt_complete`) | No UX; assessment §3.7 |
| `--worktree` flag on the agent path | TUI-only; MADR 0037 §1.3 |
| `--sandbox` | Different threat model from `FSRoots`; assessment §1.6 |
| Event-typed MCP status | Over-engineered for a twice-per-session change; pull model reused (§2.5) |

---

## 5. Verification

Every "exists" below was probed against the installed binary on 2026-07-27,
not read from release notes.

| Claim | How verified |
|---|---|
| `session/set_model` with `modelId` succeeds on grok | `id=3 session/set_model {"sessionId":…,"modelId":"grok-4.5"}` → `{"_meta":{"model":{"Ok":"grok-4.5"}}}` (Assessment §3.1) |
| `session/set_model` validates ids | `grok-3` → `{code:-32602, "unknown model id"}` (Assessment §3.1) |
| `_meta.modelState.availableModels` returned at initialize | Probe `/tmp/g3.out` line 1 (Assessment §2.2) |
| `_x.ai/models_update` emitted mid-session | `/tmp/g5.out` notifications (Assessment §2.4) |
| grok advertises `deep-research`, `workflow`, `goal` in `availableCommands` | Probe `availableCommands` (Assessment §2.2) |
| grok exposes `--permission-mode`, `--tools`, `--disallowed-tools`, `--allow`, `--deny`, `--no-subagents`, `--disable-web-search` | `grok --help` (Assessment §5) |
| grok emits `_x.ai/mcp/server_status`, `_x.ai/mcp/init_progress`, `_x.ai/mcp_initialized` | `/tmp/g5.out` and Assessment §2.4 |
| grok `ModelArgs` discards `cfg.Args` | `grok.go:44-51` and its own comment |
| `acpagent.session` does not implement `provider.ModelSession` | `provider.go:210-213`; grep clean |
| SDK has no `SetSessionModelRequest` | `grep SetSessionModel` on `acp-go-sdk@v0.13.5` — zero hits |
| `acphttp` already uses raw-RPC send for stdio-agnostic framer | `acphttp/provider.go:147` |
| Static catalog's `grok-code-fast-1` is not in the live list | Probe `availableModels` vs `grok.go:20-25` — disjoint |

**Not verified:** `_x.ai/models_update` payload shape (probe saw it emitted;
the receiver was not yet registered so the buffer was dropped — D2's
implementation plan pins the shape with a live test before consuming it).

---

## 6. Implementation

Phased, in
[0039-plan-grok-acp-parity.md](./0039-plan-grok-acp-parity.md).

Summary: **Phase 0** pin wire contracts with live tests → **Phase 1 (D1)**
raw-RPC `session/set_model` + `ModelSession` + command remap →
**Phase 2 (D2)** live catalog parse + cache + extension-notification hook →
**Phase 3 (D3)** `deep-research`/`workflow` canonical commands →
**Phase 4 (D4)** seven typed policy Config fields →
**Phase 5 (D5)** MCP status interface → **Phase 6** docs + verify.

### Implementation Record

- **Phase 0 (2026-07-27)**: Added live-tagged tests (`live_setmodel_test.go`, `live_initializemeta_test.go`, `live_commandcatalog_test.go`, `live_policyflags_test.go`, `live_mcpnotifications_test.go`) and package-private E0 raw JSON-RPC request helper (`rawRequest`).
- **Phase 1 (2026-07-27)**: Implemented `SetModel` on `acpagent.session`, asserted `provider.ModelSession`, remapped `model` in `grok/commandtable.go` to `OpSetModel`.
- **Phase 2 (2026-07-27)**: Implemented E1 extension notification dispatch (`HandleExtensionMethod`), parsed `_meta.modelState` on `initialize`, populated `Provider` catalog cache, and registered `_x.ai/models_update`.
- **Phase 3 (2026-07-27)**: Promoted `deep-research` and `workflow` to canonical command specs in `command/specs.go` and mapped in `grok/commandtable.go`.
- **Phase 4 (2026-07-27)**: Added 7 typed policy config fields to `acpagent.Config` and `GrokProviderConfig`, updated `defaultArgs` and `ModelArgs` builder.
- **Phase 5 (2026-07-27)**: Added `provider.MCPStatusSession` and `DiagnosticsSession` implementations on `acpagent.session`, registered `_x.ai/mcp/server_status` and `_x.ai/mcp_initialized` notification handlers.
- **Phase 6 (2026-07-27)**: Updated operator docs (`docs/config.md`), `README.md`, command matrix, and completed pre-add checks and unit tests.
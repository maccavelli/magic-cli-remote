# MADR 0011: OpenCode ACP provider

- **Status**: Accepted — implemented 2026-07-21 (Milestones 1–3; spike passed,
  see §Spike results; live tests green incl. model selection)
- **Date**: 2026-07-21
- **Related**: [MADR 0004](./0004-phase2-grok-acp.md) (Grok ACP provider),
  [MADR 0003](./0003-phase1-decisions.md) (provider abstraction)

## Context

The daemon currently drives one real coding agent — Grok Build — over ACP
(Agent Client Protocol, JSON-RPC 2.0 on the subprocess's stdio), via
`github.com/coder/acp-go-sdk v0.13.5`, confined to `internal/provider/grok`.
The core is already agent-agnostic: `provider.Provider` / `provider.Session` /
`provider.PermissionSession` interfaces, a `Registry`, a session `Manager`
keyed on opaque `provider.ID` strings, and per-session provider persistence.
`fake` and `grok` coexist today; the phone already sends a `Provider` field on
`session.create` and can call `providers.list`.

We want to add **OpenCode** (SST/Anomaly's TypeScript-on-Bun agent,
opencode.ai, npm `opencode-ai`, MIT) as a second real provider.

Disambiguation: the maintained OpenCode is the SST/Anomaly rewrite. The
original Go TUI once named "opencode" (`opencode-ai/opencode`) is now
**Crush** (`charmbracelet/crush`) — a different project, out of scope.

### How OpenCode can be driven

| Path | Mechanism | Assessment |
|---|---|---|
| `opencode acp` | Native ACP agent, JSON-RPC 2.0 over stdio | **Chosen** — identical surface to grok |
| `opencode serve` + `opencode-sdk-go` | HTTP + SSE, official Stainless Go SDK | Fallback for features ACP lacks (fork, diff, file search) |
| `opencode run --format json` | One-shot JSONL | Rejected: no interactive permissions, known premature-exit bug |

ACP wins because it reuses the entire existing grok stack (transport,
streaming update mapping, permission round-trips, terminal host) and the ACP
`initialize` capability handshake insulates the daemon from OpenCode's fast
internal API churn (multiple releases/week, no semver guarantee, in-flight
message-model v2 and permission-event renames — all of which only bite the
HTTP path).

## Decision

1. **Integrate OpenCode over ACP**: spawn `opencode acp`, speak ACP with the
   already-pinned `coder/acp-go-sdk v0.13.5`.
2. **Extract a shared ACP core** rather than copy the grok package. Grok's
   `session.go` (~740 lines: `SessionUpdate` mapping, `RequestPermission`,
   `fs/*`, `terminal/*`) is ACP-generic — nothing grok-semantic beyond the
   provider-ID literal and default CLI args. A new `internal/provider/acpagent`
   package owns that machinery, parameterized by a small `Spec` (provider ID,
   binary, default-args builder, capabilities, permission timeout). `grok` and
   `opencode` become thin wrappers.
3. **Sequence the extraction to be as safe as copying**: it lands as a pure
   refactor gated on grok's existing test suite staying green *before* the
   opencode provider exists. If the suite proves too thin to trust mid-way,
   fall back to copy-and-adapt for the first ship and extract later.
4. **OpenCode ships disabled by default** (`providers.opencode.enabled: false`)
   and is never auto-selected: `defaultProviderID` stays grok-first, so
   existing users see zero behavior change. Opt-in is per-session via the
   phone's `Provider` field (or config default flip).

## Plan

### Spike (first, before any refactor)

Prove `opencode acp` ↔ `acp-go-sdk v0.13.5` end-to-end on this host:
install OpenCode (user-scope, `~/.opencode/bin`), run a guarded live test
that performs `initialize` → `session/new` → one `prompt` → stream updates →
`cancel`/close, and record:

- negotiated ACP protocol version and advertised `agentCapabilities`
  (expect fork/resume/mcp; confirm `SetSessionModel`)
- whether the SDK's client-callback surface (`fs/*`, `terminal/*`,
  `session/request_permission`) is exercised without protocol errors
- shape of streamed updates vs. grok's (thought chunks, tool calls, plans)

Abort criteria: protocol-version mismatch or SDK panic on OpenCode frames →
revisit (newer SDK pin, or the HTTP/Go-SDK fallback path).

### Spike results (2026-07-21, OpenCode v1.18.4, acp-go-sdk v0.13.5)

Full success — initialize → session/new → prompt (streamed) → close, exit 0:

- **Protocol version 1 on both sides**; no skew. Agent advertises
  `loadSession`, session `fork`/`resume`/`list`/`close`, `image` +
  `embeddedContext` prompts, MCP http/sse.
- **`mcpServers` must be a non-nil array** on `session/new` — OpenCode's zod
  validation rejects `null`. Grok's code already passes `[]acp.McpServer{}`;
  the shared core must preserve this.
- **Model selection answered**: no `SetSessionModel` anywhere; instead
  `session/new` returns `configOptions` with a **select `id=model`**
  (`provider/model` values, e.g. `opencode/big-pickle`) and a **select
  `id=mode`** (`build`/`plan`). Per-session model = the SDK's
  `SetSessionConfigOption`. The mode select maps cleanly onto a future
  plan-mode feature.
- **Works with zero provider auth**: OpenCode ships free "Zen" models
  (default `opencode/big-pickle`), so the provider is usable out of the box;
  `opencode auth login` only needed for paid providers.
- Streaming updates observed: `agent_thought_chunk`, `agent_message_chunk`,
  `available_commands_update` (3 commands), plus a **`usage_update`** type
  unknown to our mapper — grok's update switch has no default case, so
  unknown updates are silently ignored (safe; optionally map to a context
  meter later).
- `stopReason=end_turn` round-trips; `session/close` clean.

### Milestone 1 — shared ACP core (pure refactor)

- New `internal/provider/acpagent/`: move `grok/session.go`,
  `grok/terminal.go`, and the subprocess-launch/init/new/load logic from
  `grok/grok.go`, generalized behind `Spec`.
- Rewrite `internal/provider/grok` as a thin `Spec` (binary `grok`, args
  `agent --no-leader … stdio`, `IDGrok`).
- Gate: `go test ./...` and `go vet ./...` green; grok observable behavior
  unchanged.

### Milestone 2 — opencode provider (additive)

- `internal/provider/opencode`: `Spec` with `IDOpencode`, binary `opencode`,
  args `["acp"]`.
- `provider.IDOpencode ID = "opencode"` in `provider/provider.go`.
- `config`: `OpencodeProviderConfig` in `ProvidersConfig` + defaults
  (`enabled:false`, `bin:"opencode"`, `permission_timeout_seconds:900`) in
  `config.go` and `load.go`.
- `daemon.go`: conditional `reg.Register(opencode.New(...))`.
- Example `providers.opencode` block in `configs/`.

### Milestone 3 — ops & selection

- `internal/cli/service/setup.go`: add `~/.opencode/bin` to the systemd unit
  PATH.
- `defaultProviderID` unchanged (grok-first).
- Docs: prerequisite that the host has run `opencode auth login`
  (credentials at `~/.local/share/opencode/auth.json`; config at
  `~/.config/opencode/opencode.json`) — the daemon only launches the binary.

### Milestone 4 — mobile provider picker + create-time model (delivered)

The create-session dialog already had a provider dropdown fed by
`providers.list`, so opencode appears there with no UI change. Delivered on
top:

- `session.create` gained an optional `model` field (protocol +
  `handleSessionCreate` → `StartOptions.Model`), making per-session models
  reachable at creation — previously only the `/model` built-in could set one.
- Mobile: `SessionMeta.model` parsed and shown on session tiles; a Model
  field in the New-session dialog; `resumeSession` re-sends the prior model so
  a resumed grok session keeps its `-m` flag.

## Model selection

Grok takes `-m <model>` on the CLI. OpenCode models are `provider/model`
strings (e.g. `anthropic/claude-sonnet-4-5`) and OpenCode advertises the ACP
`SetSessionModel` capability. Plan: apply `StartOptions.Model` via a
capability-gated set-session-model request after `session/new`, falling back
to OpenCode's own config default. If the pinned SDK lacks SetSessionModel
(spike will tell), per-session model selection for opencode is deferred and
the OpenCode config default applies.

## Testing

- Unit: fake in-process ACP agent feeding `SessionUpdate` /
  `RequestPermission` frames into `acpagent`; assert `event.Event` mapping.
  Grok's existing session/terminal tests are the refactor regression gate.
- Registry/config: opencode registers iff enabled.
- Live (guarded, opt-in like grok's `live_test.go`): real `opencode acp`
  handshake + one prompt — the compatibility surface worth exercising against
  a real binary.

## Risks

1. **ACP version skew** between SDK v0.13.5 and OpenCode's ACP — mitigated by
   the spike; both are Zed-lineage ACP.
2. **OpenCode release cadence** (no semver guarantee) — ACP mostly insulates
   us; document a known-good OpenCode version and re-run the live smoke on
   upgrades.
3. **Refactor regression in grok** — mitigated by pure-refactor gating and
   the copy-and-adapt fallback.
4. **`/undo`, `/redo`** are unsupported over OpenCode's ACP — cosmetic,
   documented.

## Non-goals (later)

- HTTP/`opencode serve` control path and its extra features (fork, diff,
  file/symbol search)
- Image/file prompt attachments
- Mobile provider picker (Milestone 4 tracked separately)

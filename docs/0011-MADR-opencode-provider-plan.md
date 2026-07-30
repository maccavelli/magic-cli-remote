# MADR 0011: OpenCode ACP provider

> **Superseded in part by [MADR 0019](./0019-MADR-opencode-process-management-plan.md)
> (2026-07-24).** The ACP transport chosen here was removed: OpenCode is now
> driven exclusively through the shared `opencode serve` engine described in the
> Performance addendum below. The `providers.opencode` keys `transport`, `args`
> and `fs_roots` no longer exist. Everything else in this document — the spike
> results, the acpagent extraction (still used by grok), the model-selection
> analysis — remains accurate as written. Left unedited as a record of the
> decision at the time.
>
> **Follow-on (accepted):** [MADR 0020](./0020-MADR-opencode-session-tree.md) —
> OpenCode HTTP session-tree demux, tree-idle turn completion, child
> permissions/questions, todos→plan, FIFO queue, agent/command pickers,
> fork/revert/diff (Sprints 1–5 on `master`). Grok ACP does not implement 0020
> tree semantics (it has its own ACP queue parity for busy prompts).

- **Status**: Accepted — implemented 2026-07-21 (Milestones 1–3; spike passed,
  see §Spike results; live tests green incl. model selection).
  ACP transport superseded by MADR 0019 on 2026-07-24.
- **Date**: 2026-07-21
- **Related**: [MADR 0004](./MADR-phase2-grok-acp.md) (Grok ACP provider),
  [MADR 0003](./0003-MADR-phase1-decisions.md) (provider abstraction),
  [MADR 0019](./0019-MADR-opencode-process-management-plan.md) (ACP removal,
  single-engine process management)

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
4. **OpenCode is enabled by default** (`providers.opencode.enabled: true` —
   originally shipped opt-in; flipped on 2026-07-21 once the layer passed its
   debugging pass) but is never auto-selected: `defaultProviderID` stays
   grok-first, so existing sessions see no behavior change. Selection is
   per-session via the phone's new-session provider menu. Registration with a
   missing binary is harmless (listed as not ready, startup warning).

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

## Performance addendum (2026-07-22)

Field reports of slow/frozen OpenCode interactions led to a measurement +
research pass. Findings:

- `opencode acp` is **a full engine per process** (it starts an internal HTTP
  server and bridges it to stdio). Measured on this host: ~2.8s Bun boot to
  `initialize`, ~1.2s `session/new` — ≈4s per session create, paid again on
  every resume, `/reset`, and `/model`.
- OpenCode stores all state in one global SQLite DB; concurrent processes
  contend on it (upstream issue #15188, closed "not planned"). ACP mode also
  has a known event-loop busy-spin bug (#15600, closed "not planned").
- Every comparable remote-control project (opencode.nvim, opencode-remote,
  Sesori, the official TUI itself) drives the **HTTP server**
  (`opencode serve` + SSE, official Go SDK `opencode-sdk-go`), not ACP.

Shipped mitigations (daemon-side, ACP retained):

1. WS session ops dispatched off the connection read loop — a multi-second
   create no longer starves pings into phone-side reconnect storms.
2. **Prewarm pool** (`providers.opencode.prewarm`, default true): one spare
   spawned+initialized engine; session create/resume/relaunch claims it and
   re-arms in the background. Warm create measured at **1.2s vs ~4s** cold.
3. Turn **stall watchdog** (`turn_stall_notice_seconds`): notices when a
   running turn goes silent, with escalating back-off, so a wedged engine is
   distinguishable from a long tool run.
4. `session/load` gets its own 120s timeout (conversation replay is long);
   permission timeout default dropped 900s → 120s.
5. Tool call/update events are now no-drop (a lost terminal status looked
   like a hang); whitespace-only chunks stream through (paragraph breaks).

**Implemented (2026-07-22): `internal/provider/httpagent`** (named for the
transport, mirroring `acpagent`, so future HTTP-driven CLI tools can reuse
it). Like acpagent, the package is agent-agnostic: engine supervision, the
SSE pump, session registry/demux, REST helper, delivery guarantees, and
turn/permission bookkeeping are generic, parameterized by a `Dialect` /
`DialectSession` pair (launch args, health/event paths, SSE frame decoding,
REST shapes, event translation). The OpenCode dialect lives in
`internal/provider/opencode/http.go` (`opencode.NewHTTP`), keeping all
OpenCode knowledge in one package alongside its ACP Spec — one
long-lived `opencode serve` engine per daemon, sessions as server-side
objects, one `/global/event` SSE stream demultiplexed across sessions, REST
for prompt_async/abort/permissions. Now the default
(`providers.opencode.transport: http`); ACP remains available as
`transport: acp`. Live-verified: session create <2s warm-engine, resume <3s
with full server-side context retention (no replay cost), streaming deltas
computed from cumulative part snapshots. Notable server-mode quirks handled:
/global/event wraps events in a `{directory, project, payload}` envelope, and
the engine's own default-model resolution is broken (legacy `zen/…` alias) —
the provider **seeds** `opencode/deepseek-v4-flash-free` immediately (free
tier, ~1s short prompts vs ~3–8s for `big-pickle`) and may refine from
`/provider` asynchronously, preferring flash-free over the engine's slower
default. OpenCode 1.18 streams tokens primarily via `message.part.delta`;
the dialect emits those for low time-to-first-token on mobile.
The daemon supervises the engine (loopback-only, ephemeral port, health-poll,
respawn on death with sessions failed loudly).

## Non-goals (later)

- HTTP/`opencode serve` control path and its extra features (fork, diff,
  file/symbol search) — see Performance addendum: now the recommended v2
  direction
- Image/file prompt attachments

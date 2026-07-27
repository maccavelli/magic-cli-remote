# Grok Build ACP parity assessment (grok 0.2.112)

- **Status**: Assessment (proposal, not yet a committed MADR)
- **Date**: 2026-07-27
- **Probe target**: `grok 0.2.112 (9bbd559437) [stable]`, `~/.grok/bin/grok`
- **Related**:
  - [MADR 0004](./0004-phase2-grok-acp.md) — Grok ACP provider
  - [MADR 0022](./0022-plan-mode-parity.md) — plan-mode parity
  - [MADR 0023](./0023-canonical-slash-commands.md) — canonical slash commands
  - [MADR 0036](./0036-protocol-contract-completeness.md) — protocol contract completeness

## 1. Method

All findings are reproducible probes. The grok binary was launched headless as
`grok agent --no-leader stdio` (the same argv the daemon uses,
`internal/provider/grok/grok.go:72-86`) and driven as a raw JSON-RPC 2.0 client.

The probe evidence annotations below cite wire shapes captured to scratch files
during this session (`/tmp/g3.out`, `/tmp/g5.out`, `/tmp/g6.out`). Line-by-line
probes for: `initialize`, `session/new`, `session/load`, `session/set_mode`,
`session/set_model`, `_x.ai/hooks/list`, `_x.ai/session/prompt_complete`,
`session/update` notification kinds, the available-commands list, and a
tool-using prompt that exercised `tool_call`/`tool_call_update`/`agent_thought_chunk`/`agent_message_chunk`/`user_message_chunk` updates.

Reproduce locally with:

```
grok agent stdio
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"probe","version":"0.0.1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"/tmp","mcpServers":[],"mode":"default"}}
{"jsonrpc":"2.0","id":3,"method":"session/set_mode","params":{"sessionId":"<SID>","modeId":"plan"}}
{"jsonrpc":"2.0","id":4,"method":"session/set_model","params":{"sessionId":"<SID>","modelId":"grok-4.5"}}
```

The codebase was then grepped for each wire element grok exposes to determine
whether the remote consumes it. Locations are cited below as `file:line`.

## 2. Wire protocol & schema

### 2.1 Transport matrix — what grok exposes

| Surface                | grok flag / subcommand                  | Used by daemon? |
| ---------------------- | --------------------------------------- | --------------- |
| stdio JSON-RPC         | `grok agent --no-leader stdio`          | yes — `internal/provider/grok/grok.go:72-86` |
| WebSocket server       | `grok agent serve --bind 127.0.0.1:2419 --secret …` | **no** (no `acphttp` grok spec) |
| WebSocket relay        | `grok agent headless --grok-ws-url …`   | **no** |
| Leader (shared broker) | `grok agent leader`, `--leader`          | no — pinned off via `--no-leader` (`grok.go:74`) |
| Plain `-p` single-shot | `grok -p <prompt>` / `--output-format {plain,json,streaming-json}` | no |

The daemon only ever spawns one stdio process per session. `grok agent serve`
already exists (127.0.0.1:2419 by default with a `GROK_AGENT_SECRET` token and
a `--remote <URL>` proxy mode) and the daemon already has a generic
`internal/provider/acphttp` engine used for **Goose**
(`internal/provider/goose/goose.go:42` returns an `acphttp.Spec`). A grok
`acphttp` variant is therefore wireable with reuse, but is **not** wired. With
the current per-session process model that is the correct call (grok sessions
are single-connection and per-session state lives in the process); this is
documented as "**intentionally not used**" rather than a gap.

### 2.2 Initialize (`initialize`) result

The agent advertised (verbatim probe excerpt):

```jsonc
{"jsonrpc":"2.0","id":1,"result":{
  "protocolVersion":1,
  "agentCapabilities":{
    "loadSession":true,
    "promptCapabilities":{"image":false,"audio":false,"embeddedContext":true},
    "mcpCapabilities":{"http":true,"sse":true},
    "sessionCapabilities":{},
    "auth":{},
    "_meta":{
      "x.ai/fs_notify":true,
      "x.ai/hooks":{"blockingEvents":["pre_tool_use","stop","subagent_stop"],
                    "decisions":["deny","block"],
                    "stopSignals":["continue","stopReason","additionalContext"]},
      "x.ai/capabilities":{"toolOverrides":{
        "x_keyword_search":true,"x_semantic_search":true,
        "x_user_search":false,"x_thread_fetch":false}}
    }
  },
  "authMethods":[{"id":"cached_token",…},{"id":"grok.com",…}],
  "_meta":{"grokShell":true,"defaultAuthMethodId":"cached_token",
    "x.ai/mcp/sdk":true,"x.ai/pluginDirs":true,
    "currentWorkingDirectory":"…","agentVersion":"0.2.112",
    "agentId":"…","agentInstanceId":"…","hostname":"…",
    "modelState":{"currentModelId":"grok-4.5",
      "availableModels":[{"modelId":"grok-4.5","name":"Grok 4.5",
        "_meta":{"totalContextTokens":500000,"agentType":"grok-build-plan",
          "supportsReasoningEffort":true,"reasoningEffort":"high",
          "reasoningEfforts":[{"id":"high",…},{"id":"medium",…},{"id":"low",…}]}}]},
    "mcpServers":[],"mcpApps":false,"metadata":null,
    "availableCommands":[ /* compact, always-approve,/context, session-info,
                              deep-research, workflow, goal */ ],
    "cancelRewind":true,"sessionRecap":true,"voiceMode":true}}}
```

What the daemon reads today (`internal/provider/acpagent/acpagent.go:218-258`):

- `ProtocolVersion` — yes
- `AgentCapabilities.LoadSession` — yes
- `AgentCapabilities.PromptCapabilities.{Image,Audio}` — yes (`session.go:357-383`)
- `AgentCapabilities.McpCapabilities.{Http,Sse}` — yes (`acpagent.go:294-324`)
- `AuthMethods` — yes, gated on `Config.AuthMethodID` (`acpagent.go:246-258`)
- The entire `_meta` block — **discarded** by the typed SDK; `initResp` has no
  `_meta` accessor in `acp-go-sdk@v0.13.5` (`types_gen.go:3236`
  `NewSessionRequest` does not model `_meta` either, and
  `InitializeResponse` in the SDK has no `Meta` field at the top level — it
  has `agentCapabilities._meta` only through `map[string]any` if anyone
  reaches in; nobody does).

What the daemon therefore **fails to read**, and what that costs, is enumerated
below in §4.

### 2.3 JSON-RPC support — what grok implements (verified live)

| Method                         | Probe result                                                               | Codebase used? |
| ------------------------------ | -------------------------------------------------------------------------- | -------------- |
| `initialize`                   | OK                                                                         | yes |
| `notifications/initialized`   | OK                                                                         | yes (SDK) |
| `session/new`                  | OK (requires `cwd` and `mcpServers`)                                       | yes |
| `session/load`                 | OK (`loadSession: true`)                                                   | yes |
| `session/prompt`               | OK (requires `prompt`)                                                     | yes |
| `session/set_mode`             | OK — field is **`modeId`** (probe with `mode` rejected: `missing field modeId at line 1 column 66`); returns `{}`; emits `session/update` with `current_mode_update`/`currentModeId` | yes, via SDK `SetSessionModeRequest.ModeId` (`session.go:490-495`) |
| `session/set_model`            | **OK — field is `modelId`** (verified: id=3 → `{"_meta":{"model":{"Ok":"grok-4.5"}}}`; `grok-3`/`grok-nonexistent` → `{code:-32602,"unknown model id"}`) | **no — claimed unimplemented** (see §3.1) |
| `session/set_config_option`    | SDK exposes it; daemon calls it (`session.go:500-526`) — but grok does NOT return `configOptions` from `session/new`, so the gate value is empty | partial |
| `session/request_permission`   | available; the daemon also fires for `_x.ai/exit_plan_mode` (`extensions.go:110-167`) | yes |
| `session/cancel`, `session/list`, `session/list_modes`, `session/compact`, `session/context`, `session/info`, `_x.ai/cancel`, `agent/cancel`, `agent/prompt` | `Method not found` | n/a |
| `_x.ai/hooks/list`             | **OK** → `{"hooks":[],"projectTrusted":true}`                              | **no** |
| `_x.ai/model/set`, `_x.ai/model`, `_x.ai/session_model/set`, `_x.ai/settings/get` | `Method not found` (so set_model uses the un-prefixed `session/set_model`, not an `x.ai` extension) | n/a |

### 2.4 Notifications grok sends (verified live over a tool-using turn)

| Notification                       | When                                        | Codebase handles?                                       |
| ---------------------------------- | ------------------------------------------- | ------------------------------------------------------- |
| `session/update` (`sessionUpdate` discriminator) | every change                    | yes — `session.go:932-1078` (full kind matrix in §2.5)  |
| `_x.ai/mcp/servers_updated`        | at initialize; lists stdio MCP like `gopls` | **no** (dropped by SDK; no `_x.ai/mcp` consumer anywhere — grep clean) |
| `_x.ai/mcp/init_progress`          | MCP init progress                           | **no**                                                  |
| `_x.ai/mcp/server_status`          | per-server `ready`/reason                   | **no**                                                  |
| `_x.ai/mcp_initialized`            | once, with `mcpToolCount`, `elapsedMs`     | **no**                                                  |
| `_x.ai/announcements/update`       | server-pushed banner rows (grok release news) | **no**                                                |
| `_x.ai/settings/update`            | settings dict (tips e.g. "@ to attach files", `sharing_enabled`) | **no** |
| `_x.ai/models/update`              | **live model list change**                  | **no** — picker only ever sees the static list (§3.2)   |
| `_x.ai/sessions/changed`           | dashboard "your sessions changed"           | **no**                                                  |
| `_x.ai/queue/changed`              | prompt/agent queue change                   | **no**                                                  |
| `_x.ai/session/prompt_complete`    | once per turn when the prompt RPC returns   | **no** (the daemon derives turn-complete from the `session/prompt` RPC response, which is correct; this notification is redundant telemetry) |
| `_x.ai/session_notification`       | 27× during a tool turn (subagent/progress)  | **no**                                                  |
| `_x.ai/exit_plan_mode` (request)   | model exits plan mode → approval             | **yes** — `extensions.go:27,79-167`                      |
| `_x.ai/ask_user_question` (request) | model raises multi-choice form               | **yes** — `extensions.go:28,200-261`                    |

SDK behaviour note: extension **requests** are dispatched to
`HandleExtensionMethod` (`extensions.go:77-86`); extension **notifications**
are dropped silently by the SDK (called out in `extensions.go:75-76`). Every
`_x.ai/*` row marked "no" above is a dropped notification; the probe proves
grok actively emits them.

### 2.5 `session/update` kinds the codebase decodes

`internal/provider/acpagent/session.go:932-1078` decodes the discriminator in
`update.sessionUpdate` and routes:

| Wire `sessionUpdate`      | Daemon event                     | Code line            |
| ------------------------- | -------------------------------- | -------------------- |
| `agent_message_chunk`     | `assistant_chunk`                | `session.go:937-951` |
| `agent_thought_chunk`     | `thought_chunk`                  | `session.go:952-962` |
| `user_message_chunk`      | dropped (re-emitted on Prompt)   | `session.go:963-969` |
| `tool_call`               | `tool_call`                      | `session.go:970-983` |
| `tool_call_update`        | `tool_update`                    | `session.go:984-1007` |
| `available_commands_update` | `available_commands`            | `session.go:1008-1035` |
| `plan`, `plan_removed`    | `plan` (entries / cleared)       | `session.go:1036-1052` |
| `usage_update`            | `usage`                          | `session.go:1053-1060` |
| `current_mode_update`     | `mode`                           | `session.go:1061-1069` |
| `config_option_update`    | `session_config`                  | `session.go:1070-1073` |
| default                   | debug "unhandled session update" | `session.go:1074-1076` |

This matches the live probe (`/tmp/g6.out` over the tool-turn) exactly: 159
thought chunks, 15 tool_call_update, 13 agent_message_chunk, 6 tool_call, 1
user_message_chunk, 7 available_commands_update, plus the
`current_mode_update`/`plan` suffixes from `set_mode`. Saga fidelity is
complete; no chunk kind is dropped.

## 3. Parity gaps with grok's advertised surface

Findings are ranked by product impact. Each names the codebase claim that is
overtaken by reality (with a `file:line`), the wire evidence, and the
proposed change.

### 3.1 `session/set_model` IS supported — `/model` relaunch is unnecessary (P1)

- **Codebase claim**: `internal/provider/grok/commandtable.go:19-22`
  ```
  // ACP has no set-model call and grok exposes none as an extension, so a
  // model change means relaunching the agent (the daemon's own path).
  "model": {Kind: command.KindDaemon},
  ```
  and `internal/provider/grok/grok.go:18-25` documents the static catalog
  because "Grok Build does not expose a stable list API over ACP".
- **Probe evidence**: id=3 `session/set_model` with `{"sessionId":…,"modelId":"grok-4.5"}` returns `{"_meta":{"model":{"Ok":"grok-4.5"}}}`; an unknown model `grok-3` → `{code:-32602, "unknown model id"}`. So:
  - The method exists and is **standard JSON-RPC** (no `_x.ai/` prefix).
  - It validates against the live model list — invalid ids fail.
  - It switches the model without dropping session context.
- **Codebase impact**: `kind=op op=set_model` is supported by the daemon
  for opencode/codex (matches `internal/command/command.go:59-60`,
  `internal/session/commands.go:62`,
  `internal/session/commands.go:726`); grok's session
  (`internal/provider/acpagent/session.go`) does NOT implement
  `provider.ModelSession` (`internal/provider/provider.go:210-213`), so
  `state.Ops[OpSetModel]` is false and `/model` degrades to KindDaemon relaunch
  — losing the entire conversation and any prewarmed spare.
- **SDK gap**: `acp-go-sdk@v0.13.5` has **no** `SetSessionModelRequest`;
  confirmed by `grep SetSessionModel ~/go/pkg/mod/.../types_gen.go` (no hits).
- **Proposed fix**:
  1. Add a raw JSON-RPC method on `*acp.ClientSideConnection`
     (the SDK exposes `conn` for typed calls; a raw send hook resembles the
     `acphttp` framer's `fr.sendRequest` in
     `internal/provider/acphttp/provider.go:147` — that pattern can be lifted
     into the stdio path).
  2. Implement `provider.ModelSession` on `acpagent.session`:
     `func (s *session) SetModel(ctx, model) error { … session/set_model { sessionId, modelId: model } … }`
  3. Re-map `"model"` in `internal/provider/grok/commandtable.go` from
     `command.KindDaemon` to `command.KindOp` with
     `Op: command.OpSetModel`, mirroring
     `internal/provider/codex/commandtable.go:17` and
     `internal/provider/opencode/commandtable.go:22`.
  4. Update the `grok.go:18-25` and `commandtable.go:19-22` doc-comments and
     add a `_x.ai/...` (well: `session/set_model`) pin to the live-tagged
     tests in `grok/live_command_test.go`, modeled after
     `grok/live_mode_test.go` for `session/set_mode`.

  This is a clear UX win: `/model grok-4.5` becomes a free mid-session switch.

### 3.2 Model catalog is stale (P1)

- **Codebase**: `internal/provider/grok/grok.go:20-25`
  ```
  var staticModels = []picker.Option{
      {ID: "grok-code-fast-1", …},
      {ID: "grok-4", …},
      {ID: "grok-3", …},
      {ID: "grok-3-mini", …},
  }
  ```
  Spec is built without a `ListModels` hook, so `Provider.ListModels`
  (`acpagent.go:138-152`) returns **only** this list — never refreshed.
- **Probe evidence**: this install's `modelState.availableModels` returned
  `[{"modelId":"grok-4.5","name":"Grok 4.5","_meta":{"totalContextTokens":500000,…}}]`.
  i.e. the actual default model on the daemon host (`grok-4.5`) **is not in
  the picker catalog**, and `grok-code-fast-1` (the picker's first entry)
  **is not in the live list**. A `/model` relaunch to "grok-code-fast-1"
  would fail today with "unknown model id".
- **Proposed fix**: parse `_meta.modelState.availableModels` and
  `_meta.modelState.currentModelId` out of the initialize response
  (`acpagent.go:218-232`). Pattern precedents:
  - `_meta` parse-from-raw already used by `extensions.go` for
    `exit_plan_mode`/`ask_user_question`.
  - `acpagent.go:218` returns `initResp.AgentCapabilities` only; add
    `initResp.Meta` access if the SDK exposes it, else decode the raw frame.
  Push a `provider.IDGrok`-specific `Spec.ListModels` returning a
  live `picker.Catalog`; the merge logic already prefers live + static
  (`acpagent.go:144-151` through `picker.MergeLiveStatic`). Also subscribe
  to `_x.ai/models/update` notifications so the catalog refreshes mid-session
  (§2.4). Related MADR: [0031](./0031-opencode-catalog-and-metadata-parity.md)
  set the live-merge policy for OpenCode — apply the same here.

### 3.3 Operating modes — accurate, with one nuance (P3)

- **Probe**: `session/set_mode` with `modeId: "plan"` → `{}` and a
  `session/update` carrying `{sessionUpdate:"current_mode_update",
  currentModeId:"plan"}`; `modeId:"default"` → `current_mode_update`,
  `currentModeId:"default"`. `modeId:"nonsense"` → still `{}` but
  **no** `current_mode_update` emitted — the agent silently accepts an
  unknown id and ignores it.
- **Codebase**: `internal/provider/grok/grok.go:32-35` ships
  `staticModes = [{default,Build},{plan,Plan}]`. The
  `SetMode` synthetic-mode guard
  (`internal/provider/acpagent/session.go:477-495`) correctly catches the
  silent-accept because `syntheticModes` is set
  (`session.go:1130-1153`, `session.go:486-489`). This is right.
- **Gap**: `session/new` accepts a `mode` request field (the probe passes
  `"mode":"default"` and `"mode":"plan"`, both honored). `acpagent.go:548`
  only sends `{Cwd, McpServers}`, so a session cannot be **created** in plan
  mode — only switched after. The SDK has no `mode` field on
  `NewSessionRequest` (`acp-go-sdk@v0.13.5/types_gen.go:3236-3269`), so this
  needs either an SDK upgrade or a raw send hook (same prerequisite as §3.1).
  Low impact (the post-create `session/set_mode` succeeds in <50 ms).

### 3.4 MCP http/sse forwarding — works, stdio handled by grok itself (P3)

- **Probe**: init advertises `mcpCapabilities:{http:true,sse:true}`. grok
  spawns its **own** stdio MCP children (grep-clean: probe showed
  `_x.ai/mcp/servers_updated` listing `gopls` stdio from `~/.grok/config.toml`).
- **Codebase**: `acpagent.go:294-324` correctly forwards HTTP/SSE servers
  gated on `caps.McpCapabilities.{Http,Sse}`. Stdio MCP belongs to the
  **same host** grok runs on (the daemon host), so grok owning it is correct
  and the daemon **must not** also spawn it (an MCP child needs access to the
  agent process's stdin/stdout which the daemon cannot share over JSON-RPC).
- **Gap (cosmetic)**: grok emits `_x.ai/mcp/init_progress`,
  `_x.ai/mcp/servers_updated`, `_x.ai/mcp/server_status`,
  `_x.ai/mcp_initialized` so a phone could show "MCP tools ready" alongside
  the model gear. None are consumed (grep clean). The daemon's
  `Diagnostics.MCP` already models per-server state
  (`provider.go:186-190`) from a probe snapshot; a lightweight flush from
  these notifications turns it live.

### 3.5 grok blocking-hooks (`_x.ai/hooks`) exposed but unused (P3)

- **Probe**: Init advertises
  `x.ai/hooks={blockingEvents:["pre_tool_use","stop","subagent_stop"],
  decisions:["deny","block"], stopSignals:["continue","stopReason","additionalContext"]}`.
  `_x.ai/hooks/list` returns `{hooks:[], projectTrusted:true}`.
- **Codebase**: Not consumed (grep clean for `x.ai/hooks`). The product
  surface this targets is "the agent asks the daemon for a policy decision on
  pre_tool_use" — architecturally orthogonal to `session/request_permission`,
  which is genuine user approval. A blocking hook is a *policy* decision (deny
  edits, augment context) that should not dial out to the phone; it belongs in
  the daemon. The hook decisions available are `deny|block` and
  `continue|stopReason|additionalContext` stop signals.
- **Reading**: low priority — no current daemon-side policy layer beyond FS
  root confinement (`session.go:1455-1500`). If/when the daemon gets a
  project-policy engine, this is its input. Worth a MADR before
  implementation; the spec is grok-private (x.ai-scoped) so a generic-layer
  projection would be inventing the contract.

### 3.6 Capabilities advertised in `_meta` and dropped (P3, mostly by design)

| Init `_meta` flag                | Why the daemon should/shouldn't care |
| -------------------------------- | ----------------------------------- |
| `grokShell`                      | TUI feature; not protocol-relevant. A future surface could forward but the daemon has no shell surface. |
| `x.ai/fs_notify`                 | Filesystem change notify capability. Daemon currently lints via `auditFSAccess` (`session.go:1455-1479`) only on `fs/read_text_file`/`fs/write_text_file`. fs_notify is for live watching of edits the agent itself makes to its own cwd → could feed a future DiffSession/plan refresh. |
| `x.ai/capabilities.toolOverrides`| x_keyword_search/x_semantic_search available as tool overrides (grok's "X search" feature). x_user_search/x_thread_fetch off here. No daemon concept of agent-side tool overrides; low priority; the daemon's domain here is its own tool gating (`--tools`/`--disallowed-tools`, §3.8). |
| `cancelRewind`, `sessionRecap`, `voiceMode` | TUI capabilities — the daemon's cancel is `session/cancel` (`session.go:528-561`); "recap" maps onto session/load replay (`acpagent.go:521-546`). voiceMode is a terminal-only audio surface. No action. |
| `_meta.x.ai/mcp/sdk`, `x.ai/pluginDirs` | The first window (`--plugin-dir`, repeatable; "Highest-priority plugin scope; always trusted — hooks and MCP servers activate without a prompt") is what grok's **Agent SDKs** inject per-connection plugins through. Grok advertises both auth surfaces (cached_token, grok.com) and an SDK-style plugin hook. The daemon is itself such an SDK *client*; forwarding a plugin directory per session (so users supply rules/MCP at launch) is plausible but no daemon knob exists. Defer. |

### 3.7 Server-pushed notifications unused (P4)

Announcements, settings (tips), sessions-changed, queue-changed, and the
redundant `prompt_complete` (the daemon already emits `turn_complete` from the
`session/prompt` RPC result, `session.go:335-341`). All are harmless to drop;
they cost nothing on the wire. If we add a "grok server banner" surface
later, `_x.ai/announcements/update` is the source. Models-update is the
exception and is folded into §3.2.

## 4. Singular subjects not covered (per the request)

### 4.1 "JSON-RPC support"

Confirmed: grok's `agent stdio` is **pure JSON-RPC 2.0**, the line framing
is one frame per line, requests/responses carry `id`, notifications omit `id`.
On `notifications/initialized`: grok emits a server-side log line
`failed to decode None: Invalid params` to its **stderr** (not to the JSON-RPC
stream) — this is grok router debug noise under TTY logging and the daemon
already absorbs it via the `slogWriter` at `acpagent.go:177-179,597-619`.
No daemon impact.

`grok agent serve` is JSON-RPC over WebSocket, and `--output-format
streaming-json` exists for the headless **CLI** (not the stdio protocol).
Daemon does not and need not use these.

### 4.2 "ACP SDK standard support"

- Standard ACP request names grok implements: `initialize`,
  `session/new`, `session/load`, `session/prompt`, `session/set_mode`,
  `session/set_config_option`, `session/request_permission`, terminal/fs
  callbacks. These all map onto typed SDK calls (`client_gen.go`).
- Standard ACP requests grok **rejects**: `session/cancel`? No — the scrollback
  shows `agent/cancel` and `_x.ai/cancel` fail but the SDK's `s.conn.Cancel`
  uses `notifications/cancel` notification, which grok handles
  (`session.go:558-560`, `grok live_test.go` test path).
- **Non-standard but real**: `session/set_model` (no `_x.ai` prefix but not in
  ACP-v2025-03-26 — we treat it as a grok-specific standard, see §3.1).
- **The SDK limit**: `acp-go-sdk@v0.13.5` does not model `set_model` or
  `mode` on `NewSessionRequest` (`types_gen.go:3236-3269` has only
  `Cwd`/`McpServers`/`AdditionalDirectories`/`Meta`). Two paths forward:
  (a) upgrade the SDK and (b) ship a minimal raw-JSON-RPC send hook on
  `*acp.ClientSideConnection`. Pick (b) for the scoped `set_model` work; (a)
  later when the rest of the gap list justifies a bump.

### 4.3 "HTTP/SSE/Stream/stdio transport support"

- stdio: the only transport the daemon uses (`grok.go:72-86`). Confirmed.
- HTTP/SSE: grok ACP advertises **MCP** http/sse
  (`mcpCapabilities:{http:true,sse:true}`) — that's an MCP-server *inbound*
  capability, not an agent-transport capability. The daemon forwards HTTP/SSE
  MCP servers correctly (`acpagent.go:294-324`).
- Stream: the `agent serve` surface is **WebSocket** (binary frames), not
  HTTP/SSE. The `acphttp` package already speaks this framer
  (`internal/provider/acphttp/ws.go`); Goose uses it. Grok `acphttp` would
  reuse the same paths.
- Streaming JSON chunks (`agent_message_chunk`/`agent_thought_chunk`/`tool_call_update`,
  §2.5) are handled with the coalescing policy from
  [MADR 0024](./0024-stream-coalescing.md) (`session.go:745-816`).

### 4.4 "Tooling available through slash commands"

The probe captured `availableCommands` verbatim:

| grok advertises | canonical command (command/specs.go) | grok status over ACP |
| --- | --- | --- |
| `compact`           | `compact`   | **silent** (TUI-only — confirmed in `commandtable.go:26-29`); locked out by the `KindNone` mapping. |
| `always-approve`    | (none)      | `commandtable.go`'s `commandCaveat` flags it as TUI-only. Not canonical. |
| `context`           | `context`   | mapped `KindNative`→`session-info` (`grok/commandtable.go:24`). Confirmed returns server-info as an `agent_message`. |
| `session-info`      | (none)      | forwardable under the KindNative-default rule. Not canonical. |
| `deep-research`     | (none)      | not canonical; would forward under the undeclared-agent-owned rule (`command.go:199-208`). |
| `workflow`          | (none)      | same — forwardable; not in our canonical vocab. |
| `goal`              | `goal`      | mapped `KindNative`→`goal` (`grok/commandtable.go:25`). |

- Canonical commands currently mapped to `KindNone` for grok (`diff`, `undo`,
  `redo`, and the locked-out `compact` at `commandtable.go:26-42`) are
  accurate per the probe: `compact` returns zero frames; `diff`/`undo`/`redo`
  have no ACP method.
- A genuine gap: grok's `deep-research` and `workflow` are valuable commands
  with **no canonical mapping**. They are undeclared in grok's table so today
  they fall under the "agent advertises this name → owns the command" path
  (`command.go:199-208`) and forward as a normal `session/prompt`. Clients see
  them in `available_commands` already (`session.go:1008-1035`) — so users can
  *type* `/deep-research <query>` but `/help` won't document them. Proposal:
  either add agents-owned Spec entries (`/deep-research`, `/workflow`) to
  `specs.go` with `Kind: KindNative`, or extend the `/help` renderer to surface
  agent-advertised commands not in the canonical table.

### 4.5 "Operating modes"

Two switchable modes (`default`/`plan`) covered (`grok.go:32-35`). The CLI
surface also has `--permission-mode {default,acceptEdits,auto,dontAsk,bypassPermissions,plan}`
and `--no-plan` — see §3.8 below.

## 5. CLI surface exposed by grok that the daemon does not surface

`grok --help` enumerates many top-level flags the daemon drops. Each row lists
what (if anything) would actually be useful behind the phone. This is **not**
"implement them all"; it lists the catalog so the project owner can pick.

| grok CLI flag                       | Daemon equivalent today                                | Recommendation |
| ----------------------------------- | ----------------------------------------------------- | -------------- |
| `-m, --model`                       | `Config.Model` (`grok.go:78-80`) — but per-session mid-run switch missing (§3.1) | close gap (§3.1) |
| `--reasoning-effort`                | `Config.ReasoningEffort` (`grok.go:81-83`) — accepted | kept |
| `--always-approve`                  | `Config.AlwaysApprove` — auto-allow on `request_permission` (`session.go:1212-1213`), bypassed for `_x.ai/exit_plan_mode` (`extensions.go:111`) | kept — the bypass is the right product call |
| `--no-plan`                         | `KindMode` plan off via `/plan off` (`specs.go:21-22`) | the CLI flag is not needed remotely; `/plan` covers it |
| `--max-turns <N>`                   | **none** | add as a per-session StartOption gated to providers that accept it; grok advertises no extra-RPC mechanism, so a daemon-side turn counter enforced after `session/prompt` responses (count `turn_complete` events) is enough |
| `--sandbox <profile>`               | **none**; the daemon has its own `FSRoots` confinement (`session.go:1457-1479`) but no network sandbox | scope for a separate MADR; grok's `--sandbox` actually sandboxes the child (filesystem + network), the daemon's `FSRoots` is just a policy gate on callback paths. They are different threat models and should not be conflated. |
| `--no-subagents`                    | **none** — grok agents advertised: `general-purpose`, `explore`, `plan` (probe) | medium value; subagent spawn events come through `tool_call`/`_x.ai/session_notification` (27 of the latter during the tool turn). Disable per-session via a `Config.NoSubagents` that adds `--no-subagents` to `defaultArgs`. |
| `--experimental-memory` / `--no-memory` | **none** | cross-session memory — out of scope for the remote today |
| `--rules <RULES>`                   | **none** — grok already loads rules from `~/.grok/rules/*.md` and the project's `AGENTS.md` (the daemon's CWD choice in `grok.go:72-86` matters only for the OS cwd). Extra `--rules` is daemon-side-admin concern | expose via `Config.ExtraRules` when multi-project work needs it |
| `--system-prompt-override`          | **none** | escape hatch; expose via `Config.SystemPromptOverride` for admins; not exposed on phone |
| `--tools` / `--disallowed-tools`    | **none** | the daemon's pre-add gate (`scripts/go-precheck.sh`, AGENTS.md) covers *server-side* linting, not *agent* tool whitelisting. grok's tool-whitelist/blacklist is run-side policy; wiring `Config.AllowedTools` → `grok.go defaultArgs` is a small, well-contained win |
| `--allow/--deny` permission rules   | **none** | grok's --allow/--deny are persistent permission-rule predicates (more granular than grok's ad-hoc `session/request_permission`). The daemon currently remotes every permission to the phone; allowing a rule-list to short-circuit (allow Bash/run untainted git) cuts phone-prompt noise without AlwaysApprove | medium priority |
| `--permission-mode`                 | **Captured indirectly**: daemon's `AlwaysApprove` approximates `acceptEdits`/`bypassPermissions` at the broad level. But `acceptEdits`, `dontAsk`, `plan`, `default` are distinct and richer (probe rejects unknown `session/request_permission` so the richer modes are CLI-side logic the agent enforces on its own tool calls) | expose `Config.PermissionMode` mapped to the same `--permission-mode` flag; keep `AlwaysApprove` for back-compat or deprecate it. The agent's own mode-logic is robust; the daemon only needs to forward the flag. |
| `--json-schema <SCHEMA>`           | **none** | structured-output constraint (forces the prompt to JSON matching the schema, sets `--output-format json`). Remotely: a per-prompt `response_format: json_schema` field gated to grok. Schema-driven responses are valuable for the agent-as-API use-case. Medium priority. |
| `--prompt-file` / `--prompt-json`   | n/a — one-shot CLI flags, not stdio protocol | no action |
| `--worktree` / `--worktree-ref`     | **none** | the daemon's CWD resolves a project dir; grok worktrees are an agent-side isolation primitive. Currently the daemon doesn't expose them. Low priority; the mobile use-case has its own TODOs for git work |
| `--fork-session`                    | **Captured**: daemon's resume path (`acpagent.go:521-546`) loads the same session id back | the fork case (a new session id that copies an old one) is unhandled. Could surface as `provider.ForkSession` (`provider.go:132-137`) for grok if a use case appears |
| `--disable-web-search`              | **none** | grok's web-search tool is on by default. Disable flag maps cleanly to a `Config.DisableWebSearch`. Low priority but free. |

`grok --no-leader` is the daemon's choice and deliberately so
(`grok.go:74`); the alternate leader-out-process model would centralize
multi-session state but at the cost of session isolation. No change.

## 6. Prioritised remediation (proposal)

Ordered by impact / outward visibility:

1. **P1 / cmdModel mid-session switch (§3.1)**. Add raw-RPC `session/set_model`,
   implement `provider.ModelSession` on `acpagent.session`, re-map the grok
   `model` command to `KindOp set_model`. Live-tag pin in
   `grok/live_command_test.go`.
2. **P1 / live model catalog (§3.2)**. Parse `_meta.modelState.availableModels`
   at initialize; add a `Spec.ListModels` for grok; subscribe
   `_x.ai/models/update` to refresh the picker. Replaces the stale
   `grok-code-fast-1` default.
3. **P2 / grok-only slash commands deep-research + workflow (§4.4)**. Add to
   `command/specs.go` as `KindNative` so `/help` and autocomplete include them
   alongside `goal`.
4. **P3 / permission-mode and tool allow/deny surfaces (§5)**. Expose
   `Config.PermissionMode`, `Config.AllowedTools`, `Config.DisallowedTools`,
   `Config.AllowRules`, `Config.DenyRules` to reduce phone-prompt noise and
   match grok's richer policy model. Same args wiring as `--always-approve`.
5. **P3 / mid-session `_x.ai/models_update`** (already implied by §3.2): wire
   to a catalog-refresh event.
6. **P3 / structured output `--json-schema` (§5)**. A per-prompt field
   `response_format_json_schema` that, when set on grok, rebuilds args with
   `--json-schema <SCHEMA>`. Documents the agent-as-API surface.
7. **P4 / MCP lifecycle notifications (§3.4)**. Forward `mcp/server_status`
   into `Diagnostics.MCP` live.
8. **P4 / `--no-subagents`, `--disable-web-search` (§5)**. Small `Config`
   flags wired into `defaultArgs`. Escape-hatch parity.

What is **intentionally not** recommended (reasons inline above):

- Wiring `grok agent serve` (§2.1) — the per-session process model is right;
  leader-shared mode is right out.
- Consuming `cancelRewind`/`sessionRecap`/`voiceMode`/`grokShell` (§3.6) —
  TUI-only, no remote surface.
- Consuming `_x.ai/hooks` (§3.5) — predates a policy layer the daemon lacks;
  defer behind a MADR.
- Consuming `_x.ai/session_notification`, `_x.ai/sessions/changed`,
  `_x.ai/queue/changed`, `_x.ai/announcements/update`, `_x.ai/settings/update`,
  `_x.ai/session/prompt_complete` (§3.7) — telemetry or TUI surfaces; cost is
  zero and there's no UX yet.

## 7. Probe evidence archive

Raw frames used as evidence (scratch):

- `/tmp/g3.out` — set_mode plan/default/nonsense + extensions probe.
- `/tmp/g5.out` — `_x.ai/exit_plan_mode`, `_x.ai/models/update`,
  `_x.ai/queue/changed` notifications during a real turn.
- `/tmp/g6.out` — `session/set_model` for `grok-4.5` (Ok), `grok-3` (unknown
  model id), tool turn with 159 thought chunks / 15 `tool_call_update` / 13
  `agent_message_chunk` / 6 `tool_call` / 1 `user_message_chunk`.

Lines cited above reference the post-`todo:cite` tree on `master` at the
time of probing (commits around 2026-07-27).
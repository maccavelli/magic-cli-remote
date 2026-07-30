# MADR 0028: Codex CLI provider — surfaces, protocol, and implementation specifications

- **Status**: **Accepted for implementation** (Milestone 0 spike complete on host;
  provider package not started).
- **Date**: 2026-07-26
- **Deciders**: Project Owner (scope, enablement, phasing); Implementer
  (daemon/provider/transport)
- **Related**:
  - [MADR 0004](./MADR-phase2-grok-acp.md) — Grok ACP provider (stdio ACP pattern)
  - [MADR 0011](./0011-MADR-opencode-provider-plan.md) — OpenCode provider (spike
    methodology; later HTTP engine)
  - [MADR 0019](./0019-MADR-opencode-process-management-plan.md) — shared-engine
    process lifecycle
  - [MADR 0020](./0020-MADR-opencode-session-tree.md) — session tree / fork / compact
    optional interfaces
  - [MADR 0023](./0023-MADR-canonical-slash-commands.md) — canonical slash vocabulary
    (Codex command table must be probe-backed)
  - [MADR 0025](./0025-MADR-goose-provider.md) — Goose ACP-over-HTTP (shared-engine
    template closest to Codex multi-thread server)
  - [Codex provider implementation plan](./0028-PLAN-codex-provider.md)
  - [MADR 0035](./0035-MADR-codex-ui-ux-remediation.md) — Codex chat remediation
    (item-stream fidelity, command truth, capability disclosure, turn
    completion normalization, hardening). Landed 2026-07-27.
    — repository-grounded delivery phases and acceptance gates
  - [protocol-v1.md](./protocol-v1.md) — phone control plane
  - [agent_cli_slash_commands_matrix.md](./agent_cli_slash_commands_matrix.md) —
    historical survey only (superseded by MADR 0023)
  - Spike evidence: [docs/codex-spike-0.145.0/](./codex-spike-0.145.0/)

**Researched / live-probed against** (2026-07-26):

| Source | Version / note |
|---|---|
| Host CLI | **`codex-cli 0.145.0`** (`~/.local/bin/codex` → npm `@openai/codex`, musl linux-x64 vendor binary) |
| Auth on host | `codex login status` → **Logged in using ChatGPT** |
| Schema dump | `codex app-server generate-ts` / `generate-json-schema` → 617 `.ts`, 273 JSON (v1+v2) |
| Live stdio | Four app-server sessions; summaries under `docs/codex-spike-0.145.0/` |
| Official docs | [CLI](https://developers.openai.com/codex/cli/), [App Server](https://developers.openai.com/codex/app-server), harness post |
| Peers | Remodex, codex-web, `codex-acp` (third-party ACP shim) |

> **Milestone 0 complete.** Wire behaviour below overrides pure-doc assumptions
> where they differ (sandbox enum casing, `turn/steer` fields, concurrent turns,
> experimental gating).

---

## 1. Context

The daemon already drives four providers:

| Provider | Transport | Package | Engine model |
|---|---|---|---|
| grok | ACP stdio | `acpagent` | Per-session subprocess |
| opencode | REST + SSE | `httpagent` | One shared `opencode serve` |
| goose | ACP over WebSocket | `acphttp` | One shared `goose serve` |
| fake | In-process | — | In-process |

We want **OpenAI Codex CLI** as a fifth real provider so the phone can start,
stream, approve, cancel, and resume Codex sessions the same way it does for
grok / OpenCode / goose.

Codex is **not** an ACP-native agent. Its first-class integration surface for
rich clients (VS Code extension, desktop app, community remotes) is
**`codex app-server`**: a bidirectional JSON-RPC protocol over stdio, Unix
socket (WebSocket-framed), or experimental TCP WebSocket. Community ACP adapters
exist only as *translators onto app-server*.

That means Codex does not drop cleanly into `acpagent` or `acphttp`. It needs a
**dialect-specific transport** that speaks app-server primitives
(**Thread / Turn / Item**) and maps them onto mcremote's `provider.Session` and
domain events.

---

## 2. Codex product surfaces (assessment)

Codex is a multi-surface product sharing one agent harness
(`codex-rs/core`) and, for rich clients, one client protocol (app-server).

| Surface | Entry | Role for mcremote |
|---|---|---|
| **Interactive TUI** | `codex` | Operator UX only; not a control API |
| **Non-interactive exec** | `codex exec …` | CI/automation; weak interactive approvals; **reject** as primary transport |
| **App Server** | `codex app-server` | **Chosen integration API** — threads, turns, approvals, history, models |
| **Codex SDK** | `@openai/codex-sdk` (TS), Python SDK | Thin clients over app-server / CLI runtime; useful for protocol reference, not for Go daemon |
| **MCP server** | `codex mcp` | Exposes Codex *as* an MCP server to other hosts; wrong direction for us |
| **IDE extension / Desktop** | VS Code + ChatGPT desktop | First-party app-server clients; good protocol reference |
| **Codex cloud** | Hosted environments | Out of scope (no local sandbox / local files) |
| **Official remote control** | `remoteControl/*` + ChatGPT mobile pairing | Vendor mobile path; **do not depend on** for mcremote |
| **Remote TUI** | `codex app-server --listen ws://…` + `codex --remote ws://…` | Ops curiosity; WS marked experimental/unsupported for production |

### 2.1 Auth and runtime home

- Auth: ChatGPT browser login (`codex login`) or API key
  (`printenv OPENAI_API_KEY | codex login --with-api-key`).
- Config / state home: `CODEX_HOME` (default `~/.codex`) — `config.toml`,
  rules, sessions/rollouts, app-server control socket.
- Session durability: JSONL rollouts under `~/.codex/sessions` (shared with
  desktop/IDE when they use the same home).

mcremote must **not** manage OpenAI credentials. Auth is the host Codex
installation's concern (`codex login` / API key in Codex's own config). Same
pattern as Remodex and codex-web.

**Product decision — Ready / auth:** `Provider.Ready()` is **binary-only**
(executable found and runnable). Missing ChatGPT session or API key does **not**
make the provider not-ready. On engine start (and optionally at register time),
**log a warning** if neither a logged-in Codex session nor an API key appears
available (best-effort: `codex login status` failure and no `OPENAI_API_KEY` /
Codex auth file). Sessions may still fail at first turn; that surfaces as a
normal provider error to the phone.

### 2.2 Sandbox and approvals (agent runtime, not mcremote TLS)

Two orthogonal layers (from official agent-approvals docs):

1. **Sandbox mode** — OS-enforced: `read-only`, `workspace-write` (default
   Auto), `danger-full-access` / `--yolo`.
2. **Approval policy** — when to stop: `untrusted`, `on-request`, `never`, or
   granular.

Linux sandbox uses `bwrap` + `seccomp`. Network defaults off in
`workspace-write` unless `sandbox_workspace_write.network_access = true`.

**Product decision — inherit host config:** By default, mcremote does **not**
force sandbox or approval on `thread/start` / `turn/start`. Empty
`providers.codex.approval_policy` / `sandbox_mode` means **omit those RPC
fields** so app-server applies `~/.codex/config.toml` (and project trust) as
the interactive CLI would. Optional config keys remain available for operators
who want an override for remote sessions only.

For mcremote, approvals that *do* fire must still surface on the phone via
`permission_request` / `permission.respond`, not auto-accept unless the operator
sets `providers.codex.always_approve`.

### 2.3 CLI tooling worth knowing (not the provider path)

| Tooling | Purpose |
|---|---|
| `codex app-server generate-ts` / `generate-json-schema` | Version-pinned protocol schemas — **pin spike artifacts** |
| `codex app-server daemon` | Managed long-lived app-server (+ optional `--remote-control`) |
| `codex app-server proxy` | Stdio ↔ Unix control socket byte proxy |
| `codex sandbox linux\|macos\|windows` | Local sandbox dry-run |
| `codex execpolicy check` | Test Starlark rules |
| Slash commands in TUI | `/model`, `/permissions`, `/status`, `/review`, `/agent`, … — **TUI only** unless re-implemented via app-server methods |

---

## 3. App-server protocol (the integration contract)

### 3.1 Wire format

- JSON-RPC 2.0 **without** the `"jsonrpc":"2.0"` field on the wire (MCP-like).
- **Requests**: `{ "method", "id", "params" }`
- **Responses**: `{ "id", "result" }` or `{ "id", "error" }`
- **Notifications**: `{ "method", "params" }` (no `id`)
- **Server-initiated requests**: approvals and elicitations use a request `id`;
  the client **must** respond with a matching response. This is the same pattern
  goose uses for `session/request_permission`, but with Codex-specific methods.

### 3.2 Transports — **decision: stdio for v1**

| Transport | Flag | Production readiness | mcremote use |
|---|---|---|---|
| **stdio** | `--listen stdio://` (default) / `--stdio` | Supported; primary | **v1 chosen** — live-proven on this host |
| **Unix socket** | `--listen unix://` or `unix://PATH` | Supported local control plane | Phase 2 if detachable shared engine needed |
| **TCP WebSocket** | `--listen ws://IP:PORT` | **Experimental / unsupported** | **Not for v1** |
| **off** | `--listen off` | No local listener | Unused |

**Why stdio (confirmed in spike):** cold start ~500 ms; full initialize →
thread → turn → stream → unsubscribe works; no bind/auth surface; matches IDE
embedding model. Multi-session is multi-**thread** on one stdio connection, not
multi-process.

Health probes (`/readyz`, `/healthz`) apply only to WS listeners.
Backpressure: saturated ingress → JSON-RPC `-32001` `"Server overloaded; retry later."`

### 3.3 Core primitives

```
Thread  →  durable conversation (mcremote Session)
  Turn  →  one user request + agent work (mcremote turn)
    Item →  user message, agent message, reasoning, command, file change, MCP, …
```

Lifecycle (happy path):

1. Connect transport → `initialize` → `initialized`
2. `thread/start` (or `thread/resume` / `thread/fork`)
3. `turn/start` with `input: [{type:"text", text:"…"}]`
4. Stream notifications: `turn/started`, `item/*`, deltas, approvals
5. `turn/completed` (or `turn/interrupt` → status `interrupted`)

### 3.4 Client identity

```json
{
  "method": "initialize",
  "id": 0,
  "params": {
    "clientInfo": {
      "name": "mcremote",
      "title": "magic-cli-remote",
      "version": "<daemon-version>"
    },
    "capabilities": {
      "experimentalApi": false
    }
  }
}
```

- `clientInfo.name` is used for OpenAI Compliance Logs. Use a stable product id
  (`mcremote`), not a per-build random string.
- Start with **`experimentalApi: false`** for MVP (live-proven sufficient for
  start/turn/resume/fork/compact/model list). Enable only when calling gated
  methods (e.g. `thread/turns/list`).
- Optional: `optOutNotificationMethods` to drop high-volume deltas if mobile
  needs bandwidth relief (prefer daemon-side coalescing first — MADR 0024).
- Live `initialize` result also includes `codexHome` (absolute path) for
  diagnostics — useful in `Ready`/status logs.

### 3.5 Method inventory (MVP vs later)

#### MVP (must implement)

| Method | Direction | Maps to |
|---|---|---|
| `initialize` / `initialized` | C→S | Connection handshake |
| `thread/start` | C→S | `Provider.Start` (new) |
| `thread/resume` | C→S | `Start` with `AgentSessionID` |
| `thread/unsubscribe` / close path | C→S | `Session.Close` (soft) |
| `thread/delete` | C→S | `PurgeSession.Purge` when hard-delete |
| `turn/start` | C→S | `Session.Prompt` |
| `turn/interrupt` | C→S | `Session.Cancel` |
| `model/list` | C→S | `ModelCatalog.ListModels` |
| `item/commandExecution/requestApproval` | S→C | `permission_request` |
| `item/fileChange/requestApproval` | S→C | `permission_request` (diff summary) |
| `item/permissions/requestApproval` | S→C | `permission_request` (network/fs grants) |
| Approval response `{decision}` | C→S | `PermissionSession.RespondPermission` |

#### Strong follow-ons (map to existing optional interfaces)

| Method | Interface / event |
|---|---|
| `thread/fork` | `ForkSession` |
| `thread/compact/start` | `CompactSession` |
| `turn/steer` | Mid-turn follow-up (`expectedTurnId`) |
| `thread/name/set` + `thread/name/updated` | `TypeSessionTitle` |
| `thread/tokenUsage/updated` | `TypeUsage` |
| `turn/plan/updated` | `TypePlan` |
| `item/tool/requestUserInput` | `QuestionSession` (1–3 questions) |
| `collaborationMode/list` + turn mode settings | Plan / build style modes |
| `thread/list` (+ experimental parent/ancestor filters) | History + **nested session discovery** |
| collab/subagent item + child `thread/started` | Session tree demux (MADR 0020-style) |

#### Explicit non-goals for v1

- `thread/realtime/*` (voice)
- Official `remoteControl/*` pairing to ChatGPT mobile
- `process/spawn` / unsandboxed host process API
- Plugin marketplace install/uninstall (unstable; docs say do not call from
  production clients yet)
- Full `fs/*` app-server filesystem API as a phone feature
- Desktop IPC live-sync (Remodex-specific)

### 3.6 Event / item mapping (wire-confirmed + schema)

**Observed on wire** (cheap text turns, 0.145.0):

| Notification | Seen | Maps to |
|---|---|---|
| `item/agentMessage/delta` | yes — `{threadId,turnId,itemId,delta}` | `TypeAssistantChunk` (coalesce) |
| `item/started` / `item/completed` | yes — full `item` union | tool/plan/message finalize |
| `turn/started` / `turn/completed` | yes — `turn.status` ∈ schema | latch / `TypeTurnComplete` |
| `thread/status/changed` | yes | session status |
| `thread/tokenUsage/updated` | yes — `tokenUsage.total|last` + `modelContextWindow` | `TypeUsage` |
| `account/rateLimits/updated` | yes | ignore or notice |
| `mcpServer/startupStatus/updated` | yes — e.g. `codex_apps` | ignore / debug |
| `configWarning` | yes | notice optional |
| `remoteControl/status/changed` | yes (even when unused) | ignore |
| `thread/started`, `thread/goal/cleared` | yes | lifecycle |

**Reasoning deltas** (schema; not hit on short terra turns):  
`item/reasoning/textDelta`, `item/reasoning/summaryTextDelta`,
`item/reasoning/summaryPartAdded` → `TypeThoughtChunk`.

**ThreadItem.type** (generated union, 18 variants):  
`userMessage`, `agentMessage`, `reasoning`, `commandExecution`, `fileChange`,
`mcpToolCall`, `webSearch`, `plan`, `contextCompaction`, `collabAgentToolCall`,
`subAgentActivity`, `dynamicToolCall`, `hookPrompt`, `imageGeneration`,
`imageView`, `sleep`, `enteredReviewMode`, `exitedReviewMode`.

| Item / notification | mcremote event | Notes |
|---|---|---|
| `agentMessage` + deltas | `TypeAssistantChunk` | Skip duplicate final if deltas already streamed |
| `reasoning` + reasoning deltas | `TypeThoughtChunk` | |
| `commandExecution` + `item/commandExecution/outputDelta` | `TypeToolCall` / `TypeToolUpdate` | Live-seen `commandExecution` on shell turns |
| `fileChange` + patch deltas | tool + approval | |
| `mcpToolCall` + progress | `TypeToolCall` | |
| `webSearch` | `TypeToolCall` | |
| `turn/plan/updated` / `item/plan/delta` | `TypePlan` | |
| `contextCompaction` | notice | Seen after `thread/compact/start` |
| `userMessage` | **skip** | Echo; server fills `text_elements: []` |
| Server approval requests | `TypePermissionRequest` | §3.7 — **not exercised** on this host (sandbox) |
| `item/tool/requestUserInput` | `TypeQuestionRequest` | follow-on |

### 3.7 Approvals mapping (schema-backed; wire pending sandbox)

**Server → client requests** (from generated `ServerRequest`, 10 methods):

| Method | Response shape (schema) |
|---|---|
| `item/commandExecution/requestApproval` | `{ decision: CommandExecutionApprovalDecision }` |
| `item/fileChange/requestApproval` | `{ decision: FileChangeApprovalDecision }` |
| `item/permissions/requestApproval` | `{ permissions, scope, strictAutoReview? }` (different!) |
| `item/tool/requestUserInput` | answers array (questions form) |
| `item/tool/call` | dynamic tool (host-implemented tools) |
| `mcpServer/elicitation/request` | MCP elicitation |
| `execCommandApproval` / `applyPatchApproval` | **legacy** — `ReviewDecision` (`approved` / `denied` / …) |
| `account/chatgptAuthTokens/refresh` | token refresh |
| `attestation/generate` | `{ token }` |

**Command / file-change decisions** (`CommandExecutionApprovalDecision` /
`FileChangeApprovalDecision`):

| Codex `decision` | mcremote option |
|---|---|
| `accept` | Allow once |
| `acceptForSession` | Allow for session |
| `decline` | Deny |
| `cancel` | cancelled |
| `acceptWithExecpolicyAmendment` / network amendments | v1: collapse to accept |

> **Spike gap:** no approval server-request was observed on this host. Linux
> sandbox logs `bubblewrap … needs access to create user namespaces` and
> `unshare --user` fails with `Operation not permitted` despite
> `kernel.unprivileged_userns_clone=1`. Shell tools either fail closed or the
> model avoids them. Approval mapping must be unit-tested from schema fixtures
> and re-probed on a host with a working sandbox.

`always_approve` → auto-reply `accept` without prompting.

### 3.8 Prompt input shapes (wire + schema)

`UserInput` union (generated): `text`, `image`, `localImage`, `audio`,
`localAudio`, `skill`, `mention`.

- **MVP:** `{ "type": "text", "text": "…" }` — **works without**
  `text_elements` on the wire (TS marks `text_elements` required; server
  accepts omit and echoes `text_elements: []` on `userMessage` items).
- Images: data-URL `image` when phone sends image content blocks.
- Skills / mentions / audio: later.

### 3.9 Turn control (wire-confirmed)

| RPC | Params notes | Result / behaviour |
|---|---|---|
| `turn/start` | `threadId`, `input[]`; optional model/cwd; **omit** approval/sandbox unless config override | Returns `turn` immediately with `status: inProgress`, **before** `turn/started` |
| `turn/steer` | **`expectedTurnId` required** (not optional); `threadId`, `input[]` | `{ turnId }` — only after turn is active; too early → `no active turn to steer`. **Live:** steer after `turn/started` altered the in-flight reply (output contained `STEERED`) |
| `turn/interrupt` | `threadId`, `turnId` | `{}` or error `no active turn to interrupt` if already finished |
| Concurrent `turn/start` | Both accepted with **distinct** turn ids | Serializes: second turn may wait; demux all events by `turnId`. **Do not** treat concurrent start as parallel agent work |

**Busy-turn policy for mcremote (decided after spike):**

1. Track `activeTurnID` from `turn/start` result + `turn/started` /
   `turn/completed` / `turn/interrupt`.
2. While a turn is active, phone follow-ups → **`turn/steer`** with
   `expectedTurnId` (preferred) or daemon FIFO queue of `turn/start`.
3. Do **not** fire a second bare `turn/start` from the phone as “parallel” —
   map to steer or queue; expose `ErrTurnBusy` only if we refuse queue.
4. Cancel → `turn/interrupt` for `activeTurnID`.

### 3.10 Enums that bite on the wire

| Field | **Must** use (live) | Rejected |
|---|---|---|
| `sandbox` / `sandbox_mode` | `read-only`, `workspace-write`, `danger-full-access` | `readOnly`, `workspaceWrite` → `-32600 unknown variant` |
| `approvalPolicy` | `untrusted`, `on-request`, `never`, or granular object | — |

### 3.11 Experimental API

- `thread/turns/list` **requires** `capabilities.experimentalApi: true`
  (live error without it; succeeds with it).
- MVP can stay `experimentalApi: false` and skip turns pagination.
- Enable experimental only for methods we actually call.

---

## 4. Internals (architecture of Codex itself)

From OpenAI's harness write-up and the open-source tree:

- **`codex-rs/core`** — agent loop, tools, sandbox, persistence of a thread.
- **`codex-rs/app-server`** — client-friendly bidirectional JSON-RPC facade used
  by VS Code, desktop, and external remotes.
- **TUI** historically special-cased; work is underway to make the TUI *also* an
  app-server client (enables remote TUI).
- **Rollouts** — durable JSONL history under `CODEX_HOME`; multiple clients can
  share history when they share home, subject to single-writer rules for some
  experimental paginated modes.
- **Subagents** — native collab/spawn items (`collabAgentToolCall`,
  `subAgentActivity`) may expose child activity, but the pinned 0.145.0
  `ThreadListParams` schema has no `parentThreadId` or `ancestorThreadId`
  filters. The child-thread identity and relationship contract still need a
  live collaboration probe before a session tree can be implemented.

**Product decision — nested sessions:** Subagent / collab child threads are
**first-class nested mcremote sessions** (OpenCode-style tree demux, MADR 0020
pattern), not flattened tool rows only. MVP may stream parent-thread tool items
first; **Milestone 3** must wire child `thread.id`s into the session tree so the
phone can open, follow, and (where supported) steer nested agents.

Schema generation is version-locked:

```bash
codex app-server generate-ts --out ./tmp/codex-schema
codex app-server generate-json-schema --out ./tmp/codex-schema
```

Spike artifacts from these commands must be checked into a test fixture or
recorded in this MADR when Milestone 0 completes, with the CLI version string.

---

## 5. Open-source remote-control projects (lessons)

### 5.1 Remodex (iOS + Mac bridge)

- **Architecture**: Phone ↔ WebSocket bridge ↔ **`codex app-server`** JSON-RPC
  (stdio child or existing endpoint).
- **Auth model**: E2E pairing QR + device identity keys; relay may be
  self-hosted; prefers Tailscale for phone reachability.
- **Does not** manage OpenAI credentials; requires pre-authenticated Codex CLI.
- **Features we care about**: stream turns, approvals (On-Request vs Full
  access), steer, queue follow-ups, plan mode, subagents command, git helpers
  *in the bridge* (not via Codex).
- **Lesson**: Treat app-server as the single source of truth; keep bridge thin;
  persist thread ids for resume; map approval policies to product modes.

### 5.2 codex-web (browser frontend)

- Thin browser UI over host-side bridge; default bind `127.0.0.1`.
- Strongly recommends **long-lived app-server** separate from UI process so UI
  restarts do not kill agent work.
- Security: anyone who reaches the UI operates Codex as the host user — put
  authn **outside** (Tailscale / SSH / reverse proxy). Same threat model as
  mcremote's “daemon is the security boundary.”

### 5.3 Vendor ChatGPT mobile remote control

- Uses `remoteControl/enable`, pairing codes, enrollment to OpenAI's relay.
- Community reports: often exclusive with local TUI; docs sparse; Linux SSH
  edge cases.
- **Lesson for mcremote**: do **not** build on `remoteControl/*`. We already
  own phone ↔ host transport (mesh + TLS + device tokens). Speak app-server
  *locally* and map events ourselves.

### 5.4 Third-party `codex-acp`

- Stdio ACP agent that **spawns app-server** and translates ACP ↔ Codex.
- Attractive shortcut to reuse `acpagent`, but:
  - Extra process hop and version skew (adapter + CLI + SDK).
  - Lossy mapping of Codex-only features (steer, item-typed tools, file-change
    approvals, token usage shapes).
  - ACP adapters are community-maintained; OpenAI's stable surface is
    app-server.
- **Decision**: implement **native app-server dialect**. Revisit ACP adapter only
  if Milestone 0 fails hard on protocol instability.

### 5.5 Protocol SDKs

- Official TS/Python Codex SDKs wrap app-server for app embedding.
- `codex-codes` (Rust) and community wrappers document the same JSON-RPC.
- No first-party Go SDK — we implement a minimal client in-tree (same as we did
  for goose WS framing).

---

## 6. Decision

### 6.1 Integrate Codex via `codex app-server` JSON-RPC

**Chosen path**: daemon spawns (or attaches to) `codex app-server` and speaks
the Thread/Turn/Item protocol directly.

**Rejected paths**:

| Path | Why rejected |
|---|---|
| `codex exec` / one-shot JSONL | No durable interactive approval loop; poor multi-turn UX |
| Official `remoteControl/*` | Vendor relay + pairing; conflicts with mcremote control plane |
| `codex-acp` ACP adapter | Extra hop; incomplete fidelity; community dependency |
| TCP WebSocket as primary | Documented experimental/unsupported |
| Driving the TUI with PTY | Fragile; no structured events |

### 6.2 New package: `internal/provider/codex/`

Thin provider package owning Codex dialect + process lifecycle. Transport
primitives (JSON-RPC framer over stdio or Unix/WS) live either:

- **in-package** for v1 (faster ship), or
- extracted to `internal/provider/appserver/` if a second app-server-like agent
  appears.

Do **not** force-fit Codex into `acpagent` / `acphttp` / `httpagent` — different
method names, approval shapes, and multi-thread server semantics.

### 6.3 Engine model: **one shared stdio app-server** (confirmed)

| Phase | Model | Status |
|---|---|---|
| **v1** | Single long-lived `codex app-server --listen stdio://` child; multiplex many `thread/*` on one JSONL connection | **Chosen** — spike cold start ~500 ms; multi-thread APIs work |
| **Not v1** | TCP WebSocket | Experimental |
| **Later** | `unix://` detachable daemon | Only if we need engine to outlive provider restarts |

Process hygiene (match goose/OpenCode):

- `procutil.SetProcessGroup` + death signal
- Health: process alive (stdio has no `/readyz`)
- Death monitor: fail live sessions; lazy restart on next `Start`
- Never non-loopback network listeners in v1

### 6.4 Enablement

- Config key: `providers.codex.*`
- Default `enabled: true`; **Ready = binary present** (missing binary → not
  ready + startup warning). Auth absence only logs — same “listed not ready
  only if unusable binary” pattern as other providers, but **weaker** than a
  hard auth gate.
- Do not steal default auto-select from grok until product decides otherwise.
- `prewarm: false` by default (spike cold start ~500 ms).

### 6.5 Config sketch

```yaml
providers:
  codex:
    enabled: true
    bin: codex                    # or absolute path
    # args reserved for future; engine argv owned by package for MVP
    default_cwd: ""               # empty → daemon/home fallback
    model: ""                     # empty → Codex default / host config
    always_approve: false
    permission_timeout_seconds: 900
    prewarm: false
    # EMPTY = inherit ~/.codex/config.toml (do not send sandbox/approval on RPC)
    approval_policy: ""           # optional override: on-request | never | untrusted
    sandbox_mode: ""              # optional override: read-only | workspace-write | danger-full-access
    turn_stall_notice_seconds: 0
```

Auth remains entirely in the host Codex installation. Empty sandbox/approval
keys are **inherit**, not “daemon default to on-request”.

---

## 7. Mapping onto mcremote interfaces

| mcremote interface | Codex realization | Notes |
|---|---|---|
| `provider.ID` | `"codex"` (`IDCodex`) | — |
| `Provider.Ready` | **binary only** | Warn-log if no auth/API key; still Ready |
| `Provider.Start` | ensure stdio engine → `thread/start` or `thread/resume` | omit sandbox/approval unless config override |
| `Session.AgentSessionID` | Codex `thread.id` (UUIDv7) | parent or nested child |
| `Session.Prompt` | idle → `turn/start`; active → `turn/steer` + `expectedTurnId` | — |
| `Session.Cancel` | `turn/interrupt` `{threadId,turnId}` | — |
| `Session.Close` | `thread/unsubscribe` | soft close; rollout kept |
| `PurgeSession.Purge` | `thread/delete` | hard delete |
| `PermissionSession` | reply to `item/*/requestApproval` ids | fixture + live when sandbox works |
| `ModelCatalog` | `model/list` | spike: 6 models; default `gpt-5.6-sol` |
| `ModelSession.SetModel` | `turn/start` `model` override | sticky for later turns |
| `ForkSession` | `thread/fork` | new root-ish thread |
| `CompactSession` | `thread/compact/start` | — |
| `QuestionSession` | `item/tool/requestUserInput` | follow-on |
| `ModeSession` | collaboration modes (Plan / Default) | list OK; set path TBD |
| Session tree (MADR 0020) | child identity/relationship from a live collab probe | **nested sessions** (product, conditional) |
| `command.Table` | MADR 0023 probe table | pending `live_command` |

---

## 8. Package layout (target)

```
internal/provider/codex/
├── provider.go       # Provider, engine lifecycle, Ready, Start, ListModels
├── session.go        # thread session, Prompt/Cancel/Close, event mapping
├── conn.go           # initialize, request/response correlation
├── framer.go         # newline JSON (stdio) or WS frames (unix/ws)
├── approvals.go      # permission_request ↔ decision mapping
├── events.go         # item/* → event.Event
├── commandtable.go   # MADR 0023 command table
├── config.go         # Config struct
├── version.go        # optional CLI version gate
└── *_test.go
live_test.go          # //go:build live_codex
```

Daemon registration mirrors goose/opencode:

- `provider.IDCodex = "codex"`
- `reg.Register(codex.New(...))` when config enabled
- configs example block under `configs/`
- mobile preferred-provider list update (follow-on MADR or small app PR)

---

## 9. Canonical slash commands (preliminary — must probe)

Per MADR 0023, **do not trust TUI help menus**. After wire access, map only
what executes:

| Canonical | Likely Codex mechanism | Kind (provisional) |
|---|---|---|
| `/model` | `model/list` + turn/thread model override | `KindDaemon` or native settings |
| `/permissions` | approval_policy / sandbox on thread or config write | `KindDaemon` / mode map |
| `/compact` | `thread/compact/start` | `KindNative` if RPC succeeds |
| `/clear` | new thread / session clear | `KindDaemon` |
| `/diff` | synthesize from fileChange items or git | `KindDaemon` or unavailable |
| `/undo` | `thread/rollback` **deprecated** — probe alternate | probe |
| `/plan` | collaboration mode Plan / plan items | probe |
| `/goal` | `thread/goal/set` | probe |
| `/help` | daemon static help | `KindDaemon` |
| `/status` | compose model + sandbox + usage | `KindDaemon` |

Live command tests: `//go:build live_codex` in
`internal/provider/codex/live_command_test.go`.

---

## 10. Implementation plan

### Milestone 0 — Spike — **DONE** (2026-07-26)

See §16. Abort criteria **not** met: stable path works without experimental API;
approvals blocked only by host sandbox, not protocol.

### Milestone 1 — Provider MVP

- `internal/provider/codex/`: stdio framer, initialize, **shared** engine lifecycle.
- **`Ready()` = binary only**; on register/engine start, best-effort auth probe
  and **warn log** if no ChatGPT session / API key (still Ready).
- `thread/start|resume` **without** sandbox/approval fields unless config
  override set (inherit `~/.codex`).
- `turn/start`, event demux by `threadId`/`turnId`.
- Assistant deltas + turn complete + usage; cancel via interrupt.
- Close → unsubscribe; resume via stored agent session id.
- Config + `IDCodex` registration; unit tests from recorded JSONL fixtures
  (`docs/codex-spike-0.145.0/` + golden frames).
- `//go:build live_codex` smoke (initialize → pong → close).

### Milestone 2 — Control plane fidelity

- Steer path when turn active (`expectedTurnId`).
- Permission handlers (fixture + live when sandbox works).
- `model/list` catalog; fork; compact.
- Coalescing if delta rate hurts mobile.
- Command table + `live_command` probes.
- Optional config overrides for sandbox/approval (kebab-case on wire).

### Milestone 3 — Nested sessions + product polish

- **Session tree:** map collab/subagent child threads to nested mcremote
  sessions (reuse MADR 0020 demux patterns: parent/child ids, tree-idle,
  permissions on children) only after the wire contract is live-proven. Probe:
  - `collabAgentToolCall` / `subAgentActivity` items
  - the actual schema-supported `thread/list` filters (the 0.145.0 schema has
    **no** `parentThreadId` / `ancestorThreadId` fields)
  - `thread/started` for spawned children when subscribed
- Mobile provider entry; images; collaboration Plan mode.
- Ops note: Linux user-namespace / bwrap requirement for sandbox; auth is
  operator-owned (`codex login`).

---

## 11. Security specifications

1. **Never** bind app-server to a non-loopback address without explicit operator
   opt-in **and** WS auth (`--ws-auth` + token file). Default: stdio or
   `unix://` under a private path, or `ws://127.0.0.1:PORT`.
2. mcremote remains the remote authn boundary (TLS, device tokens, pair codes).
3. Codex sandbox/approval **default to host `config.toml`** (inherit). Optional
   `providers.codex.{sandbox_mode,approval_policy}` are operator overrides only.
   Phone “always approve” is mcremote `always_approve`, not a silent yolo of the
   OS sandbox.
4. Do not log full prompts/tool outputs at info level (compliance + privacy).
5. `clientInfo.name` stable: `mcremote`.
6. Auth secrets never enter mcremote config; only warn-level diagnostics about
   missing login/API key.

---

## 12. Testing specifications

| Layer | What |
|---|---|
| Unit | Framer, id correlation, approval mapping, item→event table, busy latch |
| Fake frames | Golden JSON from spike / schema examples |
| `live_codex` | initialize → prompt → cancel; approval path; resume; model/list |
| Race | `go test -race` on package (pre-commit) |
| Pre-add | `gofmt`, `golint`, `govulncheck` per AGENTS.md |

Live tests spend real Codex usage (ChatGPT credits or API tokens). Run at
acceptance, not in PR loops — same rule as `live_grok` / `live_opencode`.

---

## 13. Risks and open questions

| Risk | Mitigation |
|---|---|
| Protocol churn (92 client methods, experimental gates) | Pin CLI; inventory in `docs/codex-spike-0.145.0/protocol-inventory.json`; stable subset only |
| Linux sandbox / userns broken on some hosts | Document; approvals/tools degrade; still stream chat |
| Concurrent `turn/start` accepted but serialized | Steer/queue policy §3.9 — never assume parallel |
| Steer before `turn/started` fails | Latch active only after `turn/started` (or successful steer) |
| Approval shapes differ by method | Per-method response builders; fixture tests |
| Official remote control confusion | Docs: mcremote ≠ ChatGPT `remoteControl/*` |

**Resolved by spike:**

1. **Transport:** stdio shared engine (not TCP WS).
2. **Mid-turn input:** `turn/steer` + `expectedTurnId` after `turn/started`.
3. **Sandbox enum:** kebab-case only on wire.
4. **experimentalApi:** not required for MVP core.

**Resolved by product (this update):**

1. **`Ready()`:** binary only; **log warning** if no auth session / API key.
2. **Sandbox / approval:** **inherit** host Codex config by default (omit RPC
   overrides when config keys empty).
3. **Subagents:** **nested sessions** (MADR 0020-style tree), not flatten-only.

**Still open / follow-up:**

1. Re-probe approvals on a machine with working bwrap userns.
2. Exact child-thread relationship and subscription behavior from a live collab
   probe. `thread/list` parent/ancestor filters are not available in the pinned
   0.145.0 schema; `thread/turns/list` is the recorded experimental-gated
   method.
3. How aggressively to auto-subscribe child threads vs lazy open from the phone.

---

## 14. Consequences

### Positive

- Full-fidelity Codex integration aligned with first-party clients and with
  successful open-source remotes (Remodex, codex-web).
- Reuses mcremote's phone UX (permissions, streaming, resume) without vendor
  remote-control lock-in.
- Durable threads in `CODEX_HOME` remain visible to desktop/IDE if the operator
  wants multi-surface use.

### Negative / cost

- New transport dialect (not ACP, not OpenCode HTTP).
- Ongoing schema maintenance when CLI upgrades.
- Larger event-mapping surface than goose ACP.

### Neutral

- Fifth provider in the registry; mobile picker gains one entry.
- No change to mesh/TLS/pairing.

---

## 15. References

### Official

- Codex CLI: <https://developers.openai.com/codex/cli/>
- App Server: <https://developers.openai.com/codex/app-server>
- Docs full export: <https://developers.openai.com/codex/llms-full.txt>
- Agent approvals & sandbox: within Codex docs “Agent approvals & security”
- Unlocking the Codex harness (App Server design):  
  <https://openai.com/index/unlocking-the-codex-harness/>
- Source: <https://github.com/openai/codex> (`codex-rs/app-server`, `codex-rs/core`)
- SDK (TS/Python): <https://developers.openai.com/codex/codex-sdk>
- npm CLI package: `@openai/codex`

### Community / remote control

- Remodex: <https://github.com/Emanuele-web04/remodex>
- codex-web: <https://github.com/0xcaff/codex-web>
- codex-acp: <https://github.com/agentclientprotocol/codex-acp>
- Discussion on remote-control direction:  
  <https://github.com/openai/codex/discussions/21935>

### In-repo precedents

- Goose shared-engine ACP HTTP: [MADR 0025](./0025-MADR-goose-provider.md)
- OpenCode HTTP engine: [MADR 0019](./0019-MADR-opencode-process-management-plan.md)
- Slash-command discipline: [MADR 0023](./0023-MADR-canonical-slash-commands.md)

---

## 16. Spike results (2026-07-26, `codex-cli 0.145.0`)

Artifacts: [docs/codex-spike-0.145.0/](./codex-spike-0.145.0/)  
(`protocol-inventory.json`, `summary.json`, `summary2.json`, `summary4.json`;
full schema dump was generated under `/tmp/codex-spike-0.145.0/{schema,ts}`).

### 16.1 Environment

| Item | Value |
|---|---|
| CLI | `codex-cli 0.145.0` via npm `@openai/codex` (musl `x86_64-unknown-linux-musl` vendor binary) |
| Auth | ChatGPT session (`codex login status` OK) |
| `CODEX_HOME` | `~/.codex` — `config.toml` sets `model = "gpt-5.6-terra"`, `model_reasoning_effort = "high"`; project trust for this repo |
| Install layout | JS wrapper + native `codex` binary under `node_modules/@openai/codex-linux-x64/vendor/...` |
| Doctor | State DBs healthy; 1 active rollout; bwrap present but **user namespaces fail** (`unshare --user` → Operation not permitted) |
| App-server stderr | `ERROR codex_app_server: Codex's Linux sandbox uses bubblewrap and needs access to create user namespaces.` |

### 16.2 Schema generation

```bash
codex app-server generate-json-schema --out …  # ~273 JSON (v1: 2, v2: 234+)
codex app-server generate-ts --out …          # 617 TypeScript files (ts-rs)
```

| Inventory | Count |
|---|---|
| ClientRequest methods | **92** |
| ServerRequest methods | **10** |
| Notification types (v2 `*Notification.json`) | **69** |

### 16.3 Live stdio session (core path)

Command:

```bash
codex app-server --listen stdio:// \
  -c 'approval_policy="never"' -c 'sandbox_mode="read-only"'
```

Wire format confirmed: **no** `"jsonrpc":"2.0"` field; newline-delimited JSON.

| Step | Result |
|---|---|
| `initialize` | OK in **~509 ms** cold start. Result: `userAgent`, `codexHome`, `platformFamily: "unix"`, `platformOs: "linux"` |
| `initialized` | notification accepted |
| `model/list` | **6** models: `gpt-5.6-sol` (**isDefault**), `gpt-5.6-terra`, `gpt-5.6-luna`, `gpt-5.5`, `gpt-5.4`, `gpt-5.4-mini`; all `inputModalities: [text,image]` |
| `thread/start` with `sandbox: "readOnly"` | **FAIL** `-32600` unknown variant; must be `read-only` |
| `thread/start` (valid) | OK — `thread.id` UUIDv7; emits `thread/started`, `mcpServer/startupStatus/updated` (`codex_apps`) |
| `turn/start` “Reply … pong” | OK — `item/agentMessage/delta` → text `pong`; `turn/completed` `status: completed` (~1.1 s turn) |
| `thread/tokenUsage/updated` | e.g. `totalTokens` / `modelContextWindow: 258400` |
| `thread/loaded/list` | `{ data: [threadId] }` |
| `thread/unsubscribe` | `{ status: "unsubscribed" }` |
| `thread/turns/list` without experimental | **FAIL** requires `experimentalApi` |
| `thread/turns/list` with experimental | **OK** |
| `thread/fork` | **OK** — new thread id |
| `thread/compact/start` | **OK** — `{}`; compaction items observed |
| `thread/resume` | **OK** |
| `collaborationMode/list` | Plan (`mode: plan`, medium effort) + Default |
| `skills/list` | **OK** |

### 16.4 Turn steer / interrupt / concurrency

| Behaviour | Observed |
|---|---|
| `turn/steer` missing `expectedTurnId` | `-32600` missing field |
| `turn/steer` before `turn/started` | `-32600` `no active turn to steer` |
| `turn/steer` after `turn/started` with `expectedTurnId` | **OK** `{ turnId }`; model output included steered instruction (`STEERED`) |
| Two concurrent `turn/start` | **Both accept**, distinct turn ids, `status: inProgress` |
| Parallelism | **Serialized** in practice — demux by `turnId`; do not assume parallel tools |
| `turn/interrupt` when idle | `-32600` `no active turn to interrupt` |

### 16.5 Approvals / tools

| Behaviour | Observed |
|---|---|
| Approval server-requests | **None** in spikes (on-request / untrusted + shell prompts) |
| `commandExecution` items | Seen on some multi-step turns (session2), not on pure chat |
| Host sandbox | **Broken userns** — treat approval wire tests as blocked until fixed |

### 16.6 CLI surface inventory (top-level)

Subcommands seen via `codex --help`: `exec`, `review`, `login`/`logout`, `mcp`,
`plugin`, `mcp-server`, `app-server`, `remote-control`, `completion`, `update`,
`doctor`, `sandbox`, `debug`, `apply`, `resume`, `archive`/`delete`/`unarchive`,
`fork`, `cloud`, `exec-server`, `features`.

`codex features list`: large feature matrix; e.g. `multi_agent` stable,
`remote_control` **removed** as feature flag name (subcommand still exists),
`collaboration_modes` removed-but-true, `goals` stable, `hooks` stable.

### 16.7 Decision adjustments after spike + product

1. **Transport: stdio only for v1** (TCP WS deferred; `unix://` only if detachable engine is needed later).
2. **Shared single app-server process** multiplexes threads.
3. **Steer is first-class** for mid-turn phone input (`expectedTurnId`).
4. **Kebab-case sandbox** on all RPC params **when overrides are sent**.
5. **MVP without experimentalApi**; enable when nested-session list filters or turns pagination need it.
6. **Approval live test** deferred to environment with working bwrap; implement from schema fixtures now.
7. **Default model in catalog** is `gpt-5.6-sol`; host config may override (`terra` on this machine).
8. **Ready = binary only**; warn-log missing auth/API key (product).
9. **Inherit** `~/.codex` sandbox/approval unless operator overrides (product).
10. **Nested sessions** for subagents/collab remain the product target, pending
    a live-proven child relationship contract (Milestone 3).

### 16.8 Abort criteria evaluation

| Criterion | Outcome |
|---|---|
| Cannot run initialize/thread/turn without experimental | **Pass** — works |
| Approvals require desktop-only API | **Inconclusive** — host sandbox broken; schema is client-complete |
| Protocol un-pinnable | **Pass** — generate-ts/json-schema works; inventory committed |

→ **Proceed to Milestone 1 implementation.**

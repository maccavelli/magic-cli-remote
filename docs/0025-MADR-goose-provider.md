# MADR 0025: Goose ACP-over-HTTP provider

- **Status**: **Implemented** (WebSocket transport; spike complete 2026-07-26).
- **Date**: 2026-07-26
- **Deciders**: Project Owner (scope, enablement, phasing); Implementer
  (daemon/provider/transport)
- **Related**:
  - [MADR 0004](./0004-phase2-grok-acp.md) — Grok ACP provider (the ACP pattern we
    already ship; goose is the HTTP counterpart)
  - [MADR 0011](./0011-opencode-provider-plan.md) — OpenCode provider (HTTP+SSE
    engine, spike methodology)
  - [MADR 0019](./0019-opencode-process-management-plan.md) — OpenCode single-engine
    invariant (the shared-engine pattern `acphttp` reuses, minus the ACP transport
    removal that was OpenCode-specific)
  - [MADR 0023](./0023-canonical-slash-commands.md) — Canonical slash-command
    vocabulary (goose command table, verified from real behaviour)
  - [0025-goose-provider-plan.md](./0025-goose-provider-plan.md) — pre-implementation
    plan / spike notes (historical; this MADR is the source of truth for as-built)
  - [0026-mobile-goose-support.md](./0026-mobile-goose-support.md) — mobile app
    surface for selecting goose
  - [protocol-v1.md](./protocol-v1.md) — Phone control plane (goose advertised
    commands and permission modes)

**Verified against**: goose **v1.44.0** (live-probed 2026-07-25 via MADR 0023;
`goose serve` spike 2026-07-26), ACP Streamable HTTP / WebSocket surface as
observed on the wire, `acp-go-sdk v0.13.5` + `coder/websocket v1.8.15` (in
`go.mod`). Spike findings that diverge from the original SSE design are
recorded in §4.2.4 and the companion plan doc.

---

## 1. Context

The daemon drives four providers:

| Provider | Transport | Package | Engine model |
|---|---|---|---|
| grok | ACP stdio (`subcommand stdio`) | `acpagent` | Per-session subprocess |
| opencode | REST + SSE (`opencode serve`) | `httpagent` | One shared engine |
| **goose** | **ACP over WebSocket** (`goose serve`) | **`acphttp`** | **One shared engine** |
| fake | In-process echo | — | In-process |

Goose ([github.com/aaif-goose/goose](https://github.com/aaif-goose/goose), ~51.7k
stars, Apache-2.0, Linux Foundation / AAIF) is a general-purpose AI agent
written in Rust. It exposes its agent through **ACP over HTTP** (`goose serve`).
The shipped transport is a **WebSocket** upgrade on `GET /acp` for all session
communication (not SSE as originally sketched). `POST /acp` is used only for the
`initialize` and optional `authenticate` handshake.

Goose is consolidating its custom `goosed` HTTP API toward native ACP-over-HTTP
(issue #6642); `goose serve` is the unified ACP entry point. The `acphttp`
package is the shared transport for that shape — ACP-standard framing, not
goose-specific REST.

Neither prior transport fit goose:

- **`acpagent`** — ACP-over-stdio, one subprocess per session. Goose *can*
  speak stdio via `goose acp`, but that loses the shared-engine benefits of
  `goose serve`.
- **`httpagent`** — OpenCode REST+SSE dialect (custom paths, SSE schemas,
  session-tree demux, question forms, stream coalescing). Goose is a single
  ACP JSON-RPC 2.0 surface, not that dialect.

`internal/provider/acphttp/` is the third transport: shared-engine ACP client
over HTTP initialize + WebSocket session traffic.

---

## 2. Goose serve API surface

Live-probed against goose **v1.44.0** (`goose serve --help` + wire spike
2026-07-26) and cross-checked with the goose source tree / ACP Streamable HTTP
spec. Where the original design assumed SSE, the as-built surface uses
WebSocket (§2.3, §4.2.4).

### 2.1 `goose serve` command

```
goose serve [--host <HOST>] [--port <PORT>]
            [--tls] [--tls-cert-path <PATH>] [--tls-key-path <PATH>]
            [--platform <cli|desktop>]
            [--with-builtin <NAME>...]
            [--dangerously-unauthenticated]
            [--allowed-origin <ORIGIN>...]
```

Env vars: `GOOSE_SERVER__SECRET_KEY`, `GOOSE_TLS`, `GOOSE_TLS_CERT_PATH`,
`GOOSE_TLS_KEY_PATH`.

> **NOTE (spike)**: goose v1.44.0 accepts **HTTP/1.1** on loopback; no h2c
> transport is required. The ACP Streamable HTTP RFD still mentions HTTP/2 for
> pure SSE, but our WebSocket path works over HTTP/1.1. `POST /acp` for
> initialize returns a normal JSON-RPC body plus `Acp-Connection-Id` header
> (not 202-empty).

### 2.2 HTTP endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/acp` | ACP JSON-RPC 2.0 for `initialize`/`authenticate` only. Returns `Acp-Connection-Id` header. |
| `GET` | `/acp` | WebSocket upgrade (not SSE). Carries `Acp-Connection-Id`. All session JSON-RPC and notifications flow over this WebSocket. |
| `GET` | `/health` | Health check — returns `200 OK` with body `"ok"`. |
| `GET` | `/status` | Alias for `/health`. |
| `GET` | `/mcp-app-proxy` | MCP app sandbox proxy HTML. |
| `POST` | `/mcp-app-guest` | Store guest HTML content. |

### 2.3 ACP connection lifecycle (actual — WebSocket)

1. **Initialize**: `POST /acp` with JSON-RPC `initialize`. Server creates a
   connection, forwards to the agent, returns `Acp-Connection-Id` both in the
   JSON response body and the `Acp-Connection-Id` response header.
2. **WebSocket upgrade**: `GET /acp` upgrades to a WebSocket (carrying
   `Acp-Connection-Id`). All subsequent JSON-RPC (requests and notifications)
   flow bidirectionally over this single WebSocket connection.
3. **Session JSON-RPC**: `session/new`, `session/prompt`, `session/set_mode`,
   etc. are sent as JSON-RPC 2.0 messages over the WebSocket. Responses and
   notifications (`session/update`, `request_permission`) arrive as WebSocket
   text frames.
4. **No `DELETE /acp`**: Session teardown is done via the `session/close` RPC;
   connection teardown closes the WebSocket.
5. **No SSE reader**: Planned `sse.go` was never written — all post-init traffic
   is WebSocket (`ws.go`).

> **Cookie support**: Not needed. Spike observed no cookies on loopback; the
> single persistent WebSocket carries affinity.
>
> **`POST /acp` usage**: Only `initialize` and optional `authenticate`. All
> session methods go over WebSocket.
>
> **Agent→client requests**: goose issues `session/request_permission` as a
> JSON-RPC **request** (method + **string UUID** `id`, params without a nested
> `requestId`). The daemon replies with a JSON-RPC **response** on the same
> `id` (`{"result":{"outcome":…}}`). It is not a notification and not a
> separate `session/respond_permission` method.

### 2.4 ACP protocol methods

| Method | Direction | Description |
|---|---|---|
| `initialize` | C→A | Protocol version, capability negotiation |
| `authenticate` | C→A | Auth method invocation |
| `session/new` | C→A | Create a new conversation session |
| `session/load` | C→A | Resume an existing session by id |
| `session/fork` | C→A | Fork a session from existing history |
| `session/list` | C→A | List historical sessions |
| `session/prompt` | C→A | Send user prompt; starts a turn |
| `session/cancel` | C→A notification | Cancel the active turn (no JSON-RPC response) |
| `session/close` | C→A | Close a session (keeps history) |
| `session/delete` | C→A | Delete session permanently |
| `session/set_mode` | C→A | Switch permission mode |
| `session/set_config_option` | C→A | Set session config (e.g. model) |
| `session/update` | A→C | Notification: streaming turn output |
| `session/request_permission` | A→C | **JSON-RPC request** (has `id`); client must reply with result |
| `logout` | C→A | End authentication state |

### 2.5 ACP session/update notification variants (WebSocket frame payload)

Every WebSocket text frame for a notification is a JSON-RPC 2.0 message body:

```json
{
  "jsonrpc": "2.0",
  "method": "session/update",
  "params": {
    "sessionId": "<agent-session-id>",
    "update": { "sessionUpdate": "<discriminator>", ... }
  }
}
```

The `update` field is the standard ACP `SessionUpdate` union. Known
discriminators and their mapping to daemon events:

| discriminator | event.Event.Type | Notes |
|---|---|---|
| `agent_message_chunk` | `TypeAssistantChunk` | Text content; whitespace-only chunks are paragraph breaks |
| `agent_thought_chunk` | `TypeThoughtChunk` | Model reasoning tokens |
| `user_message_chunk` | **skipped** | Echo of prompt we sent; duplicates client bubble |
| `tool_call` | `TypeToolCall` | Kind, status, title, location, content summary |
| `tool_call_update` | `TypeToolUpdate` | Status transitions, output update |
| `plan` | `TypePlan` | Full replace-semantics plan entries |
| `plan_removed` | `TypePlan` | Empty entries array (clear) |
| `available_commands_update` | `TypeAvailableCommands` | Slash command catalog |
| `current_mode_update` | `TypeMode` | Mode switch (auto/approve/smart_approve/chat) |
| `config_option_update` | `TypeSessionConfig` | Streaming handler present; goose v1.44.0 only emits options on session/new|load in practice |
| `usage_update` | `TypeUsage` | Token/context usage |
| `session_info_update` | `TypeSessionTitle` | Title forwarded when non-nil; `updatedAt` ignored |

This mapping is **transport-agnostic** and lives in `acphttp/session.go`
`handleUpdate` (parallel to `acpagent`'s `SessionUpdate`). Config options are
also emitted once from `session/new` / `session/load` responses via
`emitConfigOptions`.

---

## 3. Decision

### 3.1 New transport: `internal/provider/acphttp/`

A shared transport package for ACP agents that expose their protocol over
HTTP + WebSocket. The HTTP counterpart of `acpagent` (ACP-over-stdio). Design:

- **Single long-lived engine process** per daemon (like `httpagent` for
  OpenCode), not per-session subprocesses (like `acpagent` for grok).
- **Standard ACP client** over WebSocket: `POST /acp` for initialize
  (and optional authenticate), then WebSocket on `GET /acp` for all session
  JSON-RPC, notifications, and agent-initiated requests.
- **ACP connection management**: initialize handshake, `Acp-Connection-Id`
  tracking, WebSocket read pump, session-ID-based event routing.
- **Shared mapping**: same `acp.SessionUpdate` → `event.Event` switch-case
  shape as `acpagent` — framing is identical, only the transport differs.
- **Engine lifecycle**: spawn, health poll, death monitor (fail live sessions),
  lazy re-spawn on next `Start`/`EnsureServer`. **No** automatic exponential
  backoff respawn (unlike early design / httpagent notes in §4.2.3).

### 3.2 Goose dialect: `internal/provider/goose/`

A thin spec package above `acphttp`, exactly as `grok` is a thin spec above
`acpagent`:

- `goose.go`: `Spec` (binary `goose`, args
  `serve --host 127.0.0.1 --port PORT --dangerously-unauthenticated`),
  static models/modes, `Config`/`McpServer` aliases, `New()`/`NewWithLogger()`.
- `commandtable.go`: slash-command table from verified goose behaviour
  (MADR 0023 lesson: prefer observed execution over advertised catalog).
- `live_test.go`: build-tagged `live_goose` smoke.

### 3.3 Enabled by default; selection per session

- `providers.goose.enabled: true` by default (same pattern as opencode):
  registration with a missing binary is harmless (listed not ready, startup
  warning).
- grok remains the preferred auto-select on the phone when multiple providers
  are ready; the user picks goose from the provider menu for a session.
- See [0026-mobile-goose-support.md](./0026-mobile-goose-support.md) for the
  one-line preferred-provider list change on mobile.

### 3.4 Prewarm off by default

Goose is a Rust binary (~30MB, sub-second cold start on this host), much
faster than OpenCode's Bun (~3s). Default is **`prewarm: false`** — the serve
engine starts on first session use. Operators can set `prewarm: true` to boot
at daemon start.

### 3.5 Loopback only, no auth

`goose serve` binds `127.0.0.1` with `--dangerously-unauthenticated`.
Acceptable: the engine is a daemon child on the same host; mcremote's own TLS
authenticates remote phones. Spike: goose advertises auth method
`goose-provider` but `session/new` works without `authenticate` when the
local goose config already has a provider.

---

## 4. Architecture

### 4.1 Transport layering

```
daemon (mcremote)
   │
   ├── internal/provider/grok     (acpagent.Spec)     →  acpagent  →  subprocess stdio
   ├── internal/provider/opencode (httpagent.Dialect) →  httpagent →  engine REST+SSE
   └── internal/provider/goose    (acphttp.Spec)      →  acphttp   →  engine HTTP+WebSocket
```

### 4.2 Package: `internal/provider/acphttp/`

```
internal/provider/acphttp/
├── provider.go      # Provider, New, EnsureServer, Start, Shutdown, death monitor
├── session.go       # session lifecycle, event mapping, permissions
├── ws.go            # WebSocket framer + readPump (JSON-RPC over WebSocket)
├── conn.go          # ACP connection: initialize, conn-id, auth, dialWS
├── config.go        # Config + McpServer
├── spec.go          # Spec struct
└── session_test.go  # unit tests (event mapping, permissions, helpers)
```

> **Note**: Planned `sse.go` / `engine.go` were never added. WebSocket is in
> `ws.go`; engine lifecycle is in `provider.go`.

#### 4.2.1 `Spec` (as shipped)

```go
type Spec struct {
    ID            provider.ID
    DefaultBin    string
    ServeArgs     func(port int) []string
    HealthPath    string
    StaticModels  []picker.Option
    StaticModes   []event.SessionMode // fallback when agent declares none
    DefaultModeID string
    Commands      command.Table
}
```

Unlike `acpagent.Spec`, there is no `ConfigureSession` hook and no live
`ListModels` func. Model override after `session/new` is done inline in
`session.createNew` via `session/set_config_option` when `cfg.Model` /
`opts.Model` is set. `Provider.ListModels` always returns the static catalog.

#### 4.2.2 `Config` (per-provider config)

```go
type Config struct {
    Bin               string
    AlwaysApprove     bool
    DefaultCWD        string
    Model             string
    PermissionTimeout time.Duration
    Prewarm           bool
    TurnStallNotice   time.Duration
    AuthMethodID      string
    // McpServers forwarded to the agent at session/new (ACP MCP capability).
    McpServers        []McpServer
}
```

Same base as `acpagent.Config` — the shared `ACPProviderConfig` in
`internal/config` maps to both. Two fields from `ACPProviderConfig` are
**intentionally absent**:
- **`Args`**: Engine argv is fixed by `Spec.ServeArgs`, not operator-configurable.
- **`FSRoots`**: ACP filesystem callbacks are a stdio transport concern; over
  HTTP the agent manages its own filesystem.

`acpHTTPConfig` in `internal/daemon/daemon.go` maps `GooseProviderConfig` to
`goose.Config`, embedding the shared `acphttp.Config` while dropping `Args`
and `FSRoots`. Goose-only `with_builtins` is typed and maps exclusively to
repeatable `goose serve --with-builtin` flags.

#### 4.2.3 Engine lifecycle (historical SSE sketch)

The original design assumed SSE + health-based auto-respawn with exponential
backoff. **Not implemented.** As-built behaviour is §4.2.4 / §4.2.4a only.

#### 4.2.4 WebSocket framer (as built)

`POST /acp` is used only for the `initialize`/`authenticate` handshake. Once
the connection is established, the daemon upgrades `GET /acp` to a WebSocket
(`conn.dialWS`), and all subsequent JSON-RPC flows bidirectionally over that
connection.

```
Provider.startServer()
  1. Find free port → exec goose serve (+ SetProcessGroup, SetDeathSignal)
  2. Health poll GET /health → 200 OK (60s timeout, 50ms interval)
  3. POST /acp → ACP initialize → Acp-Connection-Id
  4. GET /acp → WebSocket upgrade (conn.dialWS)
  5. readPump goroutine: read WebSocket text frames
     → if response (id + result/error, no method): deliver to pending client RPC
     → if agent request (method + id): routeAgentRequest
        (session/request_permission → JSON-RPC response on same id)
     → if notification (method, no id): routeNotification → session.handleUpdate
```

Key files:
- **`conn.go`**: `acpConn.postJSON` (HTTP POST for init/auth only),
  `acpConn.initialize`, `acpConn.authenticate`, `acpConn.dialWS`
- **`ws.go`**: `wsFramer.sendRequest` (client→agent RPC, integer ids),
  `wsFramer.sendResponse` (reply to agent requests; preserves string or
  number ids), `readPump`
- **`session.go`**: `handleUpdate`, `handlePermissionRequest` /
  `replyPermission` (ACP permission outcome on the request id)

JSON-RPC **ids** on the wire: client→agent uses monotonic integers; goose
agent→client (`session/request_permission`) uses **UUID strings**. The
framer stores ids as `json.RawMessage` so both forms work.

No SSE parser, no `DELETE /acp`, no per-session GET stream.

#### 4.2.4a Engine lifecycle (in provider.go)

```
Provider.EnsureServer() / ensureServer()
  1. Find free port on 127.0.0.1
  2. exec.Command(bin, serveArgs(port)...)
  3. procutil.SetProcessGroup + SetDeathSignal
  4. Poll GET /health until 200 (60s timeout, 50ms interval)
  5. POST /acp → ACP initialize → Acp-Connection-Id header
  6. GET /acp → WebSocket upgrade → wsFramer + readPump goroutine
  7. Store engine, ws, wsFramer

Death monitor (cmd.Wait only — no healthCtx / auto-respawn loop):
  → clear engine, ws, wsFramer
  → fail all live sessions (serverDied → TypeSessionStatus disconnected + TypeError)
  → sessions map cleared
  → next Start/EnsureServer lazily starts a new engine
```

#### 4.2.5 Session lifecycle

```
session.Prompt(ctx, parts)
   1. wsFramer.sendRequest("session/prompt", {sessionId, prompt})
   2. Streaming updates arrive as session/update notifications
   3. When the prompt RPC returns stopReason=end_turn → TypeTurnComplete

session.Cancel(ctx)
   wsFramer.sendNotification("session/cancel", {sessionId})

session.Close(ctx)
   wsFramer.sendRequest("session/close", {sessionId})
   Remove from sessions map

session load (Start with opts.AgentSessionID)
   wsFramer.sendRequest("session/load", {sessionId, cwd, mcpServers})
   Engine replays conversation as session/update (marked Replay while loading)

session.RespondPermission / autoAllow
   JSON-RPC response on the original session/request_permission id
   result.outcome = selected|cancelled (not a separate RPC method)
```

---

## 5. Goose dialect

### 5.1 `internal/provider/goose/goose.go`

```go
package goose

import (
    "strconv"
    "github.com/maccavelli/magic-cli-remote/internal/provider"
    "github.com/maccavelli/magic-cli-remote/internal/provider/acphttp"
)

type Config = acphttp.Config

var spec = acphttp.Spec{
    ID:         provider.IDGoose,
    DefaultBin: "goose",
    ServeArgs: func(port int) []string {
        return []string{
            "serve", "--host", "127.0.0.1",
            "--port", strconv.Itoa(port),
            "--dangerously-unauthenticated",
        }
    },
    HealthPath:    "/health",
    StaticModels:  staticModels,
    StaticModes:   staticModes,
    DefaultModeID: "auto",
    Commands:      commandTable,
}

type Provider = acphttp.Provider

func New(cfg Config) *Provider { return acphttp.New(spec, cfg) }
func NewWithLogger(cfg Config, log *slog.Logger) *Provider { return acphttp.NewWithLogger(spec, cfg, log) }
```

### 5.2 Goose session modes

Goose advertises permission-autonomy modes (not plan/build modes) via ACP
`session/new`'s `modes` field. These are real ACP modes — `session/set_mode`
works and is confirmed by `current_mode_update`. Goose supplies its own mode
list, so `StaticModes` in the Spec is a fallback only:

| Mode ID | Name | Description |
|---|---|---|
| `auto` | Auto | Agent proceeds without asking |
| `approve` | Approve | Agent asks for approval on every tool call |
| `smart_approve` | Smart Approve | Agent asks only for write/destructive ops |
| `chat` | Chat | Conversational only, no tool execution |

### 5.3 Command table

Built from *observed* goose behaviour over ACP, not from its
`available_commands_update` advertisement (MADR 0023 lesson: grok advertises
`/compact` and `/context` over ACP while executing neither).

As shipped in `commandtable.go`. Terminal-local Goose commands are not ACP
commands merely because a terminal accepts them; a live, version-pinned ACP
execution probe is required before one becomes remote-native:

| Command | Kind | Notes |
|---|---|---|
| `help` | `daemon` | |
| `plan` | `none` | goose has permission modes, not plan/build modes |
| `mode` | `daemon` | daemon manages from ACP `current_mode_update` |
| `model` | `daemon` | `session/set_config_option` |
| `context` | `none` | goose doesn't expose token counts over ACP |
| `compact` | `none` | Goose compaction is not exposed through ACP |
| `clear` | `daemon` | |
| `new` | `daemon` | |
| `sessions` | `daemon` | |
| `goal` | `none` | Goose goals are not exposed through ACP |
| `diff` | `none` | no diff RPC over ACP |
| `undo` | `none` | undo is git-based, not exposed over ACP |
| `redo` | `none` | same |

---

## 6. Config

### 6.1 `internal/config/config.go`

```go
type ProvidersConfig struct {
    Fake     FakeProviderConfig     `mapstructure:"fake"`
    Grok     GrokProviderConfig     `mapstructure:"grok"`
    Opencode OpencodeProviderConfig `mapstructure:"opencode"`
    Goose    GooseProviderConfig    `mapstructure:"goose"`
}

type GooseProviderConfig struct {
    ACPProviderConfig `mapstructure:",squash"`
}
```

### 6.2 Defaults

```go
Goose: GooseProviderConfig{
    ACPProviderConfig: ACPProviderConfig{
        Enabled:                  true,  // register by default; Ready() gates use
        Bin:                      "goose",
        AlwaysApprove:            false,
        PermissionTimeoutSeconds: 120,
        Prewarm:                  false, // cold-start on first session
        TurnStallNoticeSeconds:   120,
    },
},
```

### 6.3 Example YAML

```yaml
providers:
  goose:
    enabled: true
    bin: "goose"
    always_approve: false
    default_cwd: ""
    model: ""                   # empty uses goose's own config default
    permission_timeout_seconds: 120
    prewarm: false              # engine starts on first use
    turn_stall_notice_seconds: 120
    auth_method_id: ""          # optional; session/new works without auth
    with_builtins: []            # typed Goose built-in extension names only
    mcp_servers: []
```

### 6.4 Daemon registration

In `internal/daemon/daemon.go`, alongside the existing grok and opencode blocks:

```go
import "github.com/maccavelli/magic-cli-remote/internal/provider/goose"

if cfg.Providers.Goose.Enabled {
    gp := goose.NewWithLogger(acpHTTPConfig(cfg.Providers.Goose), log)
    reg.Register(gp)
    if !gp.Ready() {
        log.Warn("goose provider enabled but binary not found in PATH",
            slog.String("bin", cfg.Providers.Goose.Bin))
    }
    if cfg.Providers.Goose.Prewarm {
        gp.EnsureServer()
    }
}
```

The `acpHTTPConfig` converter mirrors `acpAgentConfig` but returns
`acphttp.Config` instead of `acpagent.Config` (same fields, different
package).

---

## 7. Risks

| Risk | Mitigation |
|---|---|---|
| **ACP connection-id management** — HTTP is connectionless; our transport must maintain virtual connection state. Server reset drops all sessions. | Engine death detection fails all sessions; client re-creates. Documented as design constraint. |
| **`goose serve` changes between releases** — goose is consolidating its three binaries and moving from a custom `goosed` server to standard ACP (issue #6642, ongoing). | Pin known-good version in docs; run live smoke on upgrades. The consolidation means the endpoint shapes may shift between releases. |
| **HTTP/2 requirement** — the ACP Streamable HTTP spec mandates HTTP/2. The implementation sidesteps this by using a WebSocket upgrade instead of SSE; WebSocket works over HTTP/1.1. `POST /acp` (used only for init/auth) uses HTTP/1.1. | No special transport configuration needed. The SSE-derived HTTP/2 risk is moot. |
| **ACP cookie requirement** — the spec requires clients to accept cookies for session affinity. Not relevant: all session communication is over a single persistent WebSocket, not connectionless HTTP. | No cookie support needed. |
| **Goose ACP auth requirements** — goose advertises `goose-provider` but does not require it when local config already has a provider. | `AuthMethodID` optional; authenticate only when set and auth methods are advertised. |
| **Permission JSON-RPC shape** — goose sends `session/request_permission` as a request with a **string UUID** id (not a notification; no nested `requestId`). | `readPump` accepts `json.RawMessage` ids; replies via `sendResponse` with ACP `outcome` on the same id. |
| **Loopback `--dangerously-unauthenticated`** — no shared secret on loopback. | Acceptable: engine is child of daemon on same host; mcremote's own TLS authenticates remote phones. |
| **WebSocket back-pressure** — the WebSocket framer has a pending RPC map without timeouts. A slow engine could leave pending requests hanging. | Timeout context from caller propagates through `wsFramer.sendRequest` via `ctx.Done()`. The framer cleans up pending entries on return. |
| **`goose serve --platform`** — `desktop` mode may expect Electron. | Use `--platform cli` or omit (default). Verified in spike. |

---

## 8. What acphttp does NOT do (non-goals for v1)

- **No session-tree demux** (MADR 0020) — goose has no multi-agent concept.
- **No question forms** — goose uses standard ACP `request_permission`, not
  OpenCode-style multi-question forms. No `QuestionSession` interface needed.
- **No stream coalescing** (MADR 0024) — ACP delivers events per-discriminator, not
  per-model-token, so token-granular coalescing is irrelevant. The `acpagent`
  session already handles this (its coalescing is for stdio token streams, not
  needed here).
- **No SSE reconnect/resync** — simple design: engine death fails all sessions;
  no reconnection state machine. Client re-creates sessions.
- **No `/undo`/`/redo`/`/diff`** — goose does not expose these over ACP.
- **No `/plan` mode** — goose has permission modes, not plan/build modes.
- **No HTTP `goose acp` path** — we use `goose serve` (ACP-over-HTTP), not
  piping `goose acp` (ACP-over-stdio) through a daemon-managed socket.
- **No gateway, schedule, skills, recipes** — goose-ecosystem features, not
  daemon concerns.

---

## 9. Implementation plan (status)

### Spike — **done** (2026-07-26)

Live-probed goose v1.44.0 `goose serve`. Headline findings (full table in
[0025-goose-provider-plan.md](./0025-goose-provider-plan.md)):

| Assumption | Finding |
|---|---|
| May require HTTP/2 (h2c) | HTTP/1.1 works |
| SSE primary GET transport | WebSocket upgrade (101) used instead |
| Auth required | `goose-provider` advertised; `session/new` works without auth |
| Permission shape | `session/request_permission` as JSON-RPC request with UUID string id |
| Modes | auto / approve / smart_approve / chat confirmed |
| Protocol version | v1 (matches acp-go-sdk v0.13.5) |
| Cookies | none on loopback |
| Serve flags | `--host`, `--port`, `--tls*`, `--platform`, `--with-builtin`, `--dangerously-unauthenticated`, `--allowed-origin` all present |

### Milestone 1 — `acphttp` transport — **done**

- `config.go`, `spec.go`, `provider.go`, `conn.go`, `ws.go`, `session.go`,
  `session_test.go`
- WebSocket framer (not SSE); death monitor fails sessions (no auto-respawn)
- Permission path: agent request → JSON-RPC response on same id

### Milestone 2 — goose dialect — **done**

- `goose.go`, `commandtable.go`, `goose_test.go`, `live_test.go` (`live_goose`)

### Milestone 3 — Config + daemon wiring — **done**

- `IDGoose`, `GooseProviderConfig`, load/env defaults, daemon register + optional
  prewarm, `configs/config.example.yaml` + `config.mesh-grok.yaml`

### Milestone 4 — Docs — **partial**

- This MADR updated to as-built ✓
- `docs/config.md` goose key reference — still open
- Matrix file already has goose v1.44.0 probe data (MADR 0023); no further
  matrix change required for ship
- Mobile preferred-provider list — see MADR 0026

---

## 10. Testing

| Layer | What | How | Status |
|---|---|---|---|
| Unit | Event mapping | `session_test.go` handleUpdate variants | ✓ |
| Unit | Permissions | pending map + always_approve; UUID rpc id | ✓ |
| Unit | Session title | `session_info_update` → `TypeSessionTitle` | ✓ |
| Unit | Command table / models / modes | `goose_test.go` | ✓ |
| Unit | SSE frame parsing | n/a — no SSE | removed |
| Unit | Engine auto-respawn | n/a — lazy re-start only | not applicable |
| Live | Real `goose serve` | `go test -tags live_goose ./internal/provider/goose/` | present |
| Smoke | Full phone round-trip | Manual / existing ws smoke with goose selected | open |

Live test:

```bash
go test -tags live_goose ./internal/provider/goose/ -count=1 -timeout 90s
```

---

## 11. Cross-references (as-built)

| What | Where | Status |
|---|---|---|
| Goose dialect | `internal/provider/goose/` | ✓ shipped |
| ACP-over-HTTP transport | `internal/provider/acphttp/` | ✓ shipped (WebSocket) |
| Provider ID | `internal/provider/provider.go` `IDGoose` | ✓ |
| Config + defaults | `internal/config/config.go` `GooseProviderConfig` | ✓ enabled=true, prewarm=false |
| Config load / env | `internal/config/load.go` | ✓ |
| Daemon wiring + `acpHTTPConfig` | `internal/daemon/daemon.go` | ✓ |
| Example configs | `configs/config.example.yaml`, `config.mesh-grok.yaml` | ✓ |
| Live test | `internal/provider/goose/live_test.go` | ✓ tag `live_goose` |
| acp-go-sdk / websocket | `go.mod` | ✓ v0.13.5 / v1.8.15 |
| Goose version probed | this MADR + MADR 0023 | v1.44.0 |
| Mobile selection | `docs/0026-MADR-mobile-goose-support.md` | assessment; one-line app change |
| Pre-add / pre-commit | `scripts/go-precheck.sh`, `AGENTS.md` | ✓ standard |

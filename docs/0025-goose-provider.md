# MADR 0025: Goose ACP-over-HTTP provider

- **Status**: **Accepted** — design approved; not yet implemented.
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
  - [protocol-v1.md](./protocol-v1.md) — Phone control plane (goose advertised
    commands and permission modes)

**Verified against**: goose **v1.44.0** (live-probed 2026-07-25 via MADR 0023; not
yet probed for `goose serve`), ACP Streamable HTTP specification (RFD, requires
HTTP/2), `acp-go-sdk v0.13.5` (in `go.mod`). The `goose serve` flags, endpoint
shapes and ACP notification variants below are drawn from the Goose source tree
and ACP spec — they have NOT been live-probed against a running `goose serve`;
the spike (section 9) is where that happens.

---

## 1. Context

The daemon currently drives three providers:

| Provider | Transport | Package | Engine model |
|---|---|---|---|
| grok | ACP stdio (`subcommand stdio`) | `acpagent` | Per-session subprocess |
| opencode | REST + SSE (`opencode serve`) | `httpagent` | One shared engine |
| fake | In-process echo | — | In-process |

Goose ([github.com/aaif-goose/goose](https://github.com/aaif-goose/goose), ~51.7k
stars, Apache-2.0, Linux Foundation / AAIF) is a general-purpose AI agent
written in Rust. It exposes its agent through **ACP over HTTP** (`goose serve`)
using the standard ACP Streamable HTTP transport — a single `POST /acp` endpoint
for JSON-RPC 2.0 messages, a `GET /acp` SSE stream for notifications, and a
`DELETE /acp` endpoint for teardown. Every ACP-compatible client (editors,
desktop apps, bots) can connect to it.

Goose is actively consolidating its custom `goosed` HTTP API toward native
ACP-over-HTTP (issue #6642); `goose serve` is the new unified ACP entry point.
The `acphttp` design tracks this work — the transport is ACP-standard, not
goose-specific.

Neither existing transport fits goose:

- **`acpagent`** manages ACP-over-stdio subprocesses (one per session). Goose
  speaks ACP over HTTP, not stdio. The JSON-RPC message shapes are standard ACP
  and *could* be piped through stdio via `goose acp`, but that would be
  per-session processes, losing the shared-engine benefits of `goose serve`.
- **`httpagent`** manages REST+SSE engines (OpenCode's `prompt_async` +
  `/global/event`). Goose's API is not REST — it is a single ACP JSON-RPC 2.0
  endpoint. The `httpagent` Dialect abstractions (REST paths, custom SSE event
  schemas, session-tree demux, question forms, stream coalescing, reconnect
  resync) are all OpenCode-shaped and do not map to ACP-over-HTTP.

We need a third transport: a shared-engine ACP-over-HTTP client that speaks the
same ACP protocol as `acpagent` but over HTTP+SSE instead of stdio.

---

## 2. Goose serve API surface

Research of the `goose` source tree
(`crates/goose-acp`, `crates/goose/src/acp/`, `crates/goose-cli/src/cli.rs`)
and the ACP Streamable HTTP specification (`agent-client-protocol-http` crate)
reveals the following surface. **The flags and endpoints below are drawn from
source and spec — they have NOT been live-probed against a running binary.** The
spike (section 9) will confirm or correct them.

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

> **NOTE**: The ACP Streamable HTTP specification **requires HTTP/2** (see RFD).
> `goose serve` is a Rust binary using `axum`/`hyper`, which support HTTP/2 with
> and without TLS. The daemon's Go HTTP client must connect over HTTP/2;
> `net/http` enables this automatically over TLS, and for cleartext
> (`--dangerously-unauthenticated`) requires explicit `h2c` transport
> configuration (`golang.org/x/net/http2/h2c`). **The spike must confirm that
> `goose serve` actually requires HTTP/2 and, if so, that its h2c setup is
> compatible with Go's client.** If goose accepts HTTP/1.1 connections in
> practice, the HTTP/2 note is just a spec compliance observation and the
> implementation can use HTTP/1.1 SSE.

### 2.2 HTTP endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/acp` | ACP JSON-RPC 2.0 messages. `initialize` creates a connection and returns `Acp-Connection-Id`. Subsequent requests carry that header. |
| `GET` | `/acp` | SSE stream (`Accept: text/event-stream`) or WebSocket upgrade. Carries `Acp-Connection-Id`. |
| `DELETE` | `/acp` | Tear down the ACP connection. |
| `GET` | `/health` | Health check — returns `200 OK` with body `"ok"`. |
| `GET` | `/status` | Alias for `/health`. |
| `GET` | `/mcp-app-proxy` | MCP app sandbox proxy HTML. |
| `POST` | `/mcp-app-guest` | Store guest HTML content. |

### 2.3 ACP connection lifecycle (Streamable HTTP)

1. **Initialize**: `POST /acp` with JSON-RPC `initialize`. Server creates a
   connection, forwards to the agent, returns `Acp-Connection-Id` both in the
   JSON response body and the `Acp-Connection-Id` response header.
2. **Connection-scoped SSE stream**: `GET /acp` with `Accept: text/event-stream`
   and `Acp-Connection-Id` header. Carries connection-level responses
   (`session/new`, `session/load`) and server-initiated messages not tied to a
   specific session.
3. **Session-scoped SSE stream**: `GET /acp` with both `Acp-Connection-Id` and
   `Acp-Session-Id`. Carries session-level notifications (`session/update`,
   `request_permission`) for a single session.
4. **Session-scoped POST**: `POST /acp` carries both `Acp-Connection-Id` and
   `Acp-Session-Id`. Returns `202 Accepted`; the actual JSON-RPC response
   arrives later on the appropriate GET stream, correlated by JSON-RPC `id`.
5. **Teardown**: `DELETE /acp` with `Acp-Connection-Id` closes the connection.

> **Cookie support**: The ACP Streamable HTTP spec mandates that clients accept,
> store and return cookies set by the server for session affinity (e.g. behind a
> load balancer). For the loopback `goose serve` child process this is
> unlikely to matter, but the HTTP client (`http.Client` with a `CookieJar`)
> should be configured to handle cookies rather than ignore `Set-Cookie`.

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
| `session/cancel` | C→A | Cancel the active turn |
| `session/close` | C→A | Close a session (keeps history) |
| `session/delete` | C→A | Delete session permanently |
| `session/set_mode` | C→A | Switch permission mode |
| `session/set_config_option` | C→A | Set session config (e.g. model) |
| `session/update` | A→C | Notification: streaming turn output |
| `session/request_permission` | A→C | Notification: permission prompt |
| `logout` | C→A | End authentication state |

### 2.5 ACP session/update notification variants (SSE frame payload)

Every SSE `data:` line is a JSON-RPC 2.0 message body:

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
| `config_option_update` | `TypeSessionConfig` | Session config options (⚠️ only emitted at session create/load in current `acpagent`; add streaming handler for `acphttp`) |
| `usage_update` | `TypeUsage` | Token/context usage |
| `session_info_update` | — | Title/updatedAt metadata (no mapping) |

This mapping is **transport-agnostic**. The `acpagent` session already implements
most of this mapping in its `SessionUpdate` method
(`session.go:943-1085`).  `config_option_update` is the exception: it is
currently emitted once at session create/load time (`emitConfigOptions`), not as
a streaming `session/update` handler. `acphttp` should add the streaming
handler so that live config changes from goose are forwarded. `session_info_update`
has no daemon mapping and can be dropped (the daemon manages its own title/updatedAt).

---

## 3. Decision

### 3.1 New transport: `internal/provider/acphttp/`

A shared transport package for ACP agents that expose their protocol over
HTTP+SSE (Streamable HTTP). The HTTP counterpart of `acpagent` (which handles
ACP-over-stdio). Design:

- **Single long-lived engine process** per daemon (like `httpagent` for
  OpenCode), not per-session subprocesses (like `acpagent` for grok).
- **Standard ACP client** over HTTP: `POST /acp` for all JSON-RPC requests,
  `GET /acp` SSE for notifications.
- **ACP connection management**: initialize handshake, `Acp-Connection-Id`
  tracking, SSE stream pump, session-ID-based event routing.
- **Shared mapping**: the same `acp.SessionUpdate` → `event.Event` switch-case
  that `acpagent` uses, because the protocol framing is identical — only the
  transport differs.
- **Engine lifecycle**: spawn, health poll, death monitor, respawn with
  exponential backoff, shutdown — mirroring `httpagent`'s pattern.

### 3.2 Goose dialect: `internal/provider/goose/`

A thin spec package above `acphttp`, exactly as `grok` is a thin spec above
`acpagent`. ~80 lines of Go:

- `goose.go`: `Spec` (binary `goose`, args `serve --host 127.0.0.1 --port PORT`),
  `Config` alias, `New()`/`NewWithLogger()`, `defaultArgs()`.
- `commandtable.go`: canonical slash-command table from *verified* goose
  behaviour (not from its advertised ACP catalog — MADR 0023 lesson).

### 3.3 Opt-in, not default

- `providers.goose.enabled: false` — too large a project to auto-select.
- grok stays the default provider.
- Selection is per-session from the phone's provider menu.
- Registration with a missing binary is harmless (listed as not ready, startup
  warning).

### 3.4 Prewarm on by default

Goose is a Rust binary (~30MB, ~500ms cold start), much faster than OpenCode's
Bun (~3s), but a shared engine still benefits from booting at daemon start.
`prewarm: true` by default.

### 3.5 Loopback only, no auth

`goose serve` binds `127.0.0.1` and uses `--dangerously-unauthenticated`.
Acceptable: the engine is a daemon child on the same host, and mcremote's own
TLS authenticates remote phones.

---

## 4. Architecture

### 4.1 Transport layering

```
daemon (mcremote)
   │
   ├── internal/provider/grok     (acpagent.Spec)  →  acpagent  →  subprocess stdio
   ├── internal/provider/opencode  (httpagent.Dialect) → httpagent →  engine REST+SSE
   └── internal/provider/goose     (acphttp.Spec)   →  acphttp   →  engine HTTP+SSE
                                                                (NEW)
```

### 4.2 Package: `internal/provider/acphttp/`

```
internal/provider/acphttp/
├── acphttp.go       # Spec, Provider, New, EnsureServer, Start, Shutdown
├── session.go       # session: ACP session lifecycle, event mapping, permissions
├── engine.go        # Engine spawn, health poll, death monitor, respawn
├── sse.go           # SSE stream reader, JSON-RPC frame demux by sessionId
├── conn.go          # ACP connection: initialize, conn-id management, auth
├── config.go        # Config (mirrors acpagent.Config shape)
└── *_test.go
```

#### 4.2.1 `Spec` (what varies)

```go
type Spec struct {
    ID              provider.ID
    DefaultBin      string                     // e.g. "goose"
    ServeArgs       func(port int) []string    // build the serve argv
    HealthPath      string                     // e.g. "/health"
    // ConfigureSession applies per-session settings after session/new
    // (e.g. model selection via ACP set_config_option).
    ConfigureSession func(ctx context.Context, api acphttpAPI, resp acp.NewSessionResponse, opts provider.StartOptions, cfg Config, log *slog.Logger) error
    StaticModels    []picker.Option
    ListModels      func(ctx context.Context, cfg Config) (picker.Catalog, error)
    Commands        command.Table
    StaticModes     []event.SessionMode        // fallback when agent declares none
    DefaultModeID   string
}
```

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
- **`Args`**: The HTTP engine's argv is fixed (`goose serve --host 127.0.0.1
  --port <port> ...`), not configurable per-provider like a stdio subprocess.
- **`FSRoots`**: ACP filesystem callbacks (`fs/read_text_file`,
  `fs/write_text_file`) are a stdio transport concern; over HTTP the agent manages
  its own filesystem and the daemon never intercepts file I/O.

The `acpHTTPConfig` converter (analogous to `acpAgentConfig` in
`internal/daemon/daemon.go`) must therefore map `ACPProviderConfig` into
`acphttp.Config` while dropping `Args` and `FSRoots`.

#### 4.2.3 Engine lifecycle

```
Provider.EnsureServer()
  1. Find free port on 127.0.0.1
  2. exec.Command(bin, serveArgs(port)...)
  3. procutil.SetProcessGroup(cmd)
  4. Poll GET /health until 200 (15s timeout, 200ms interval)
  5. POST /acp → ACP initialize → get Acp-Connection-Id header
  6. GET /acp (Accept: text/event-stream, Acp-Conn-ID) → SSE pump goroutine
  7. Store baseURL, connID, enginePID

Death monitor:
  select {
    case <-cmd.Wait():
    case <-healthCtx.Done():           // N consecutive health failures
  }
  → set engineDead flag
  → fail all live sessions (TypeError + TypeSessionStatus disconnected)
  → if !shuttingDown:
      → exponential backoff (1s, 5s, 15s, max 30s)
      → respawn → EnsureServer()
      → sessions are NOT auto-recreated
```

#### 4.2.4 SSE pump

```go
// ssePump reads the SSE stream and demuxes by sessionId.
//
// Wire format:
//
//  event: message
//  data: {"jsonrpc":"2.0","method":"session/update",
//         "params":{"sessionId":"<id>","update":{...}}}
//
func (p *Provider) ssePump(ctx context.Context, r io.Reader) {
    scanner := bufio.NewScanner(r)
    var buffer bytes.Buffer
    for scanner.Scan() {
        line := scanner.Text()
        if strings.HasPrefix(line, "data: ") {
            data := strings.TrimPrefix(line, "data: ")
            buffer.WriteString(data)
        } else if line == "" && buffer.Len() > 0 {
            p.handleSSEFrame(buffer.Bytes())
            buffer.Reset()
        }
    }
}

func (p *Provider) handleSSEFrame(data []byte) {
    var envelope struct {
        Method string          `json:"method"`
        Params json.RawMessage `json:"params"`
    }
    json.Unmarshal(data, &envelope)
    if envelope.Method != "session/update" {
        return
    }
    var notif struct {
        SessionID string              `json:"sessionId"`
        Update    acp.SessionUpdate   `json:"update"`
    }
    json.Unmarshal(envelope.Params, &notif)

    p.sessionsMu.RLock()
    sess := p.sessions[notif.SessionID]
    p.sessionsMu.RUnlock()
    if sess == nil {
        return
    }
    // Identical mapping to acpagent.session.SessionUpdate:
    sess.handleUpdate(notif.Update)
}
```

#### 4.2.5 Session lifecycle

```
session.Prompt(ctx, parts)
  1. POST /acp (Acp-Conn-ID, Acp-Session-ID)
     Body: {"jsonrpc":"2.0","method":"session/prompt",
            "params":{"message":[{"type":"text","text":"..."}]}}
  2. Response: 202 Accepted
  3. Turn active; response arrives as SSE session/update frames
  4. On stop_reason=end_turn → emit TypeTurnComplete

session.Cancel(ctx)
  POST /acp (Acp-Conn-ID, Acp-Session-ID)
    Body: {"jsonrpc":"2.0","method":"session/cancel"}

session.Close(ctx)
  POST /acp (Acp-Conn-ID, Acp-Session-ID)
    Body: {"jsonrpc":"2.0","method":"session/close"}
  Remove from sessions map, close events channel

session.Resume(ctx, agentSessionID)
  POST /acp (Acp-Conn-ID)
    Body: {"jsonrpc":"2.0","method":"session/load",
           "params":{"sessionId":"<id>"}}
  Engine replays conversation as session/update (marked Replay)
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
    ID:          provider.IDGoose,
    DefaultBin:  "goose",
    ServeArgs: func(port int) []string {
        return []string{
            "serve", "--host", "127.0.0.1",
            "--port", strconv.Itoa(port),
            "--dangerously-unauthenticated",
        }
    },
    HealthPath:  "/health",
    StaticModels: staticModels,
    Commands:    commandTable,
    StaticModes: staticModes,
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

The table will be finalised during the spike, but the expected shape:

| Command | Kind | Notes |
|---|---|---|
| `help` | `daemon` | |
| `plan` | `none` | goose has permission modes, not plan/build modes |
| `mode` | `daemon` | daemon manages from ACP `current_mode_update` |
| `model` | `daemon` | daemon relaunches or uses `set_config_option` |
| `context` | `none` | goose doesn't expose token counts over ACP |
| `compact` | `native` | goose advertises and executes `/compact` (verify) |
| `clear` | `daemon` | |
| `new` | `daemon` | |
| `sessions` | `daemon` | |
| `goal` | `native` | goose advertises and executes `/goal` (verify) |
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
    Goose    GooseProviderConfig    `mapstructure:"goose"`  // NEW
}

type GooseProviderConfig struct {
    ACPProviderConfig `mapstructure:",squash"`
}
```

### 6.2 Defaults

```go
Goose: GooseProviderConfig{
    ACPProviderConfig: ACPProviderConfig{
        Enabled:                  false,
        Bin:                      "goose",
        AlwaysApprove:            false,
        PermissionTimeoutSeconds: 120,
        Prewarm:                  true,
        TurnStallNoticeSeconds:   120,
    },
},
```

### 6.3 Example YAML

```yaml
providers:
  goose:
    enabled: false              # opt-in
    bin: "goose"
    always_approve: false
    default_cwd: ""
    model: ""                   # empty uses goose's own config default
    permission_timeout_seconds: 120
    prewarm: true               # boot goose serve engine at daemon start
    turn_stall_notice_seconds: 120
    auth_method_id: ""
    mcp_servers: []
```

### 6.4 Daemon registration

In `internal/daemon/daemon.go`, alongside the existing grok and opencode blocks:

```go
import "github.com/maccavelli/magic-cli-remote/internal/provider/goose"

if cfg.Providers.Goose.Enabled {
    gp := goose.NewWithLogger(acpHTTPConfig(cfg.Providers.Goose.ACPProviderConfig), log)
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
| **HTTP/2 requirement** — the ACP Streamable HTTP spec mandates HTTP/2. Without TLS (`--dangerously-unauthenticated`), Go's client needs explicit `h2c` transport configuration. If `goose serve` actually accepts HTTP/1.1, this risk collapses; the spike must confirm. | Spike tests with both HTTP/1.1 and HTTP/2. If HTTP/2 is required, configure `http.Transport` with `golang.org/x/net/http2`. Document the constraint. |
| **ACP cookie requirement** — the spec requires clients to accept cookies for session affinity. The loopback engine is unlikely to set cookies, but the client must not ignore `Set-Cookie`. | Configure `http.Client` with a `CookieJar` (`cookiejar.New`). Even if unused, correctness costs one import. |
| **Goose ACP auth requirements** — goose may require `authenticate` before `session/new`. | Already plumbed in `acphttp` via `AuthMethodID` + `Authenticate` call, mirroring `acpagent`. |
| **Loopback `--dangerously-unauthenticated`** — no shared secret on loopback. | Acceptable: engine is child of daemon on same host; mcremote's own TLS authenticates remote phones. |
| **SSE stream buffers** — unbounded SSE could accumulate in the kernel buffer if the daemon is slow to read. | Read loop in dedicated goroutine; bounded channel per session; drop oldest under backpressure (same as `acpagent`). |
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

## 9. Implementation plan

### Spike (before any code)

Prove `POST /acp` ↔ `acp-go-sdk v0.13.5` end-to-end on this host:

1. Install goose
2. Start `goose serve --host 127.0.0.1 --port 0 --dangerously-unauthenticated`
3. Verify `/health` returns 200
4. `POST /acp` with ACP `initialize` → capture `Acp-Connection-Id`
5. `POST /acp` with `session/new` → capture `agentSessionID`
6. Open SSE `GET /acp` → observe `session/update` frames
7. `POST /acp` with `session/prompt` → confirm SSE carries response
8. Record:
   - Negotiated ACP protocol version
   - Advertised `agentCapabilities` (loadSession, fork, image, etc.)
   - `authMethods` (does goose require auth?)
   - `configOptions` from `session/new` (model select shape)
   - Advertised `modes` (confirm auto/approve/smart_approve/chat)
   - `available_commands_update` payload
   - `RequestPermission` callback shapes
   - Whether `session/load` works with prior session id
   - Whether `/compact` and `/goal` actually execute vs. advertise
   - Whether `goose serve` actually requires HTTP/2 or accepts HTTP/1.1
   - Whether it sets cookies on initialize or session/new
   - Whether the documented `--allowed-origin` and `--with-builtin` flags exist
     on actual `goose serve --help`

Abort criteria: SDK panic on goose frames, protocol version mismatch, or goose
requiring ACP auth that our SDK cannot satisfy.

### Milestone 1 — `acphttp` transport

- `internal/provider/acphttp/config.go` — Config struct (mirrors acpagent.Config)
- `internal/provider/acphttp/acphttp.go` — Spec, Provider, New, EnsureServer,
  Start, Shutdown, ListModels, Ready, CommandTable
- `internal/provider/acphttp/engine.go` — spawn, health poll, death monitor,
  respawn backoff, shutdown
- `internal/provider/acphttp/conn.go` — ACP initialize, authenticate,
  maintain connection-id, POST helper
- `internal/provider/acphttp/sse.go` — SSE stream reader, JSON-RPC frame
  parse, session-ID-based routing
- `internal/provider/acphttp/session.go` — session lifecycle (new/load/prompt/
  cancel/close), event mapping (reuse switch-case from acpagent), permission
  handling
- `internal/provider/acphttp/*_test.go` — unit tests for each component

Gate: `go test ./...` green; all existing tests pass.

### Milestone 2 — goose dialect

- `internal/provider/goose/goose.go` — Spec, Config alias, New/NewWithLogger
- `internal/provider/goose/commandtable.go` — verified command table
- `internal/provider/goose/live_test.go` — build tag `live_goose`
- `internal/provider/goose/goose_test.go` — unit tests

### Milestone 3 — Config + daemon wiring

- `internal/provider/provider.go` — add `IDGoose ID = "goose"`
- `internal/config/config.go` — add `GooseProviderConfig` + `Goose` field in
  `ProvidersConfig` + defaults + validate call
- `internal/config/load.go` — defaults + env bindings
- `internal/daemon/daemon.go` — import goose, register, warm, ready-check
- `configs/config.example.yaml` — add `providers.goose` block
- `configs/config.mesh-grok.yaml` — add `providers.goose` block

### Milestone 4 — Docs

- `docs/config.md` — goose provider keys reference
- `docs/agent_cli_slash_commands_matrix.md` — update with verified goose data
- This MADR — implementation notes

---

## 10. Testing

| Layer | What | How |
|---|---|---|
| Unit | Event mapping | Test `session.handleUpdate` with every `SessionUpdate` variant |
| Unit | SSE frame parsing | Test `handleSSEFrame` with recorded wire data |
| Unit | Command table | Test `ResolveAll` returns expected mechanisms |
| Unit | Engine lifecycle | Mock subprocess, health poll, death → respawn |
| Integration | Registry + config | Goose registers iff enabled; validate rejects bad config |
| Live | Real `goose serve` | Build tag `live_goose`: initialize → session/new → prompt → turn_complete |
| Smoke | Full phone round-trip | Existing ws smoke test with goose as active provider |

Live test pattern (same as `grok/live_test.go`):

```go
//go:build live_goose

func TestLiveGoosePrompt(t *testing.T) {
    p := goose.New(goose.Config{AlwaysApprove: true})
    if !p.Ready() {
        t.Skip("goose not in PATH")
    }
    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    defer cancel()

    s, err := p.Start(ctx, provider.StartOptions{Name: "live", CWD: t.TempDir()})
    if err != nil {
        t.Fatalf("start: %v", err)
    }
    defer s.Close(context.Background())

    if err := s.Prompt(ctx, []provider.Content{
        {Type: "text", Text: "Reply with exactly the word pong and nothing else."},
    }); err != nil {
        t.Fatalf("prompt: %v", err)
    }

    deadline := time.Now().Add(55 * time.Second)
    for time.Now().Before(deadline) {
        select {
        case ev, ok := <-s.Events():
            if !ok {
                t.Fatal("events closed early")
            }
            t.Logf("event type=%s status=%s text=%q", ev.Type, ev.Status, ev.Text)
            if ev.Type == event.TypeTurnComplete {
                return
            }
            if ev.Type == event.TypeError {
                t.Fatalf("agent error: %s", ev.Error)
            }
        case <-time.After(100 * time.Millisecond):
        }
    }
    t.Fatal("timeout waiting for turn_complete")
}
```

---

## 11. Cross-references

| What | Where | Status |
|---|---|---|---|
| Grok provider pattern | `internal/provider/grok/grok.go` (82 lines over `acpagent`) | ✓ verified |
| Shared ACP stdio core | `internal/provider/acpagent/*.go` | ✓ exists |
| ACP event mapping | `internal/provider/acpagent/session.go:943-1085` | ✓ confirmed; `config_option_update` needs streaming handler (see §2.5) |
| `emitConfigOptions` (one-time) | `internal/provider/acpagent/session.go:1162-1215` | ✓ exists; config options emitted at create/load only |
| ACP config shape | `internal/config/config.go:ACPProviderConfig` | ✓ exists; has `Args`/`FSRoots` not in `acphttp.Config` |
| Config defaults | `internal/config/config.go:Defaults()` | ✓ exists; goose defaults need adding (§6.2) |
| Config load + env | `internal/config/load.go` | ✓ exists; goose env bindings need adding |
| Daemon provider wiring | `internal/daemon/daemon.go:Run()` | ✓ exists; goose block not yet present (§6.4) |
| `acpAgentConfig` converter | `internal/daemon/daemon.go:391-417` | ✓ exists; `acpHTTPConfig` counterpart needed |
| Provider interfaces | `internal/provider/provider.go` | ✓ exists; `IDGoose` not yet declared |
| Provider ID constants | `internal/provider/provider.go:ID*` | Only `IDFake`, `IDGrok`, `IDOpencode` — `IDGoose` needs adding |
| Command vocabulary | `internal/command/specs.go` + `command.go` | ✓ verified |
| Grok command table | `internal/provider/grok/commandtable.go` | ✓ 48 lines, 12 entries |
| OpenCode command table | `internal/provider/opencode/commandtable.go` | ✓ exists |
| Live test pattern (grok) | `internal/provider/grok/live_test.go` | ✓ template for `live_goose` tests |
| `acpagent` engine pattern | `internal/provider/acpagent/acpagent.go:spawnAgent` | ✓ reference for `acphttp` engine lifecycle |
| `httpagent` engine pattern | `internal/provider/httpagent/provider.go` | ✓ reference for shared-engine supervision |
| `acp-go-sdk` version | `go.mod` | ✓ v0.13.5 |
| Goose GitHub | `github.com/aaif-goose/goose` | ✓ 51.7k★, Apache-2.0, Rust, AAIF/Linux Foundation |
| Goose version probed | MADR 0023, matrix file | v1.44.0 (2026-07-25) — not v1.23.0 |
| Config example files | `configs/config.example.yaml`, `configs/config.mesh-grok.yaml` | Neither has `providers.goose` block yet (§6.3) |
| Pre-add check | `scripts/go-precheck.sh`, `AGENTS.md` | ✓ standard |
| Pre-commit hook checks | `Makefile`, `scripts/pre-commit.sh` | ✓ standard |

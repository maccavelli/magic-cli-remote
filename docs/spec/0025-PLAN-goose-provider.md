# Goose ACP-over-HTTP Provider — Implementation Plan

**Status**: Implementation-ready (spike complete)
**Date**: 2026-07-26
**Target files**: 13 new, 7 modified
**Goose version**: v1.44.0 (live-probed)

---

## 1. Spike Findings (vs MADR 0025 assumptions)

| Assumption | Finding | Impact |
|---|---|---|
| May require HTTP/2 (h2c) | **HTTP/1.1 works** — no h2c needed | Use standard net/http, no h2c dependency |
| SSE is primary GET transport | **WebSocket upgrade works** (101 Switching Protocols) | Use `coder/websocket` as single transport |
| One SSE stream per engine | **Two streams**: connection-scoped + session-scoped | With WS this is irrelevant — one WS carries everything |
| Auth required before session operations | `goose-provider` advertised but **session/new works without auth** | AuthMethodID is optional; skip auth when unset |
| Async responses described in theory | **All POST → 202 Accepted** (empty body), response on SSE | Every RPC goes through id-correlated WS round-trip |
| Config options speculative | **4 select options**: provider(67), mode(4), model(11), thinking_effort(5) | Map all as `TypeSessionConfig` events |
| 4 modes (auto/approve/smart_approve/chat) | **Confirmed** | Map as `TypeMode` events |
| 12 commands from spec | **9 builtin + 2 skills**: prompts, prompt, compact, clear, skills, doctor, goal, grind, status | Command table corrected |
| ConfigOptionUpdate streaming | **Not emitted** by goose v1.44.0 (only at session/new/load) | Handler still implemented speculatively |
| Protocol version unknown | **v1** | Aligned with acp-go-sdk v0.13.5 |
| Cookie support needed | **No cookies set** on loopback | CookieJar still configured for spec compliance |

---

## 2. Architecture

```
daemon (mcremote)
   │
   ├── acpagent    ← grok     (per-session subprocess, ACP stdio)
   ├── httpagent   ← opencode (shared engine, custom REST+SSE)
   └── acphttp     ← goose    (shared engine, ACP-over-WebSocket)  ★ NEW
```

### Transport flow

```
Provider.EnsureServer()
  1. freePort() → port 9999
  2. exec goose serve --host 127.0.0.1 --port PORT --dangerously-unauthenticated
  3. procutil.SetProcessGroup, SetDeathSignal
  4. Poll GET /health until 200 (60s timeout)
  5. POST /acp → initialize → get Acp-Connection-Id
  6. GET /acp + Upgrade: websocket + Acp-Connection-Id → WebSocket
  7. Read pump goroutine: JSON-RPC 2.0 over WS
  8. All RPCs (session/new, session/prompt, etc.) = WS send + id-correlated response
  9. All notifications (session/update, request_permission) = WS notification dispatch

Death monitor:
  - select { <-cmd.Wait():, <-healthCtx.Done(): }
  - Fail all sessions (TypeError + TypeSessionStatus disconnected)
  - if !shuttingDown: exponential backoff → respawn

Shutdown:
  - Close WS
  - procutil.TerminateProcessGroup
  - Accept pending in-flight RPCs will fail
```

### JSON-RPC 2.0 correlation

```json
// Request (sent over WS)
{"jsonrpc":"2.0","id":42,"method":"session/new","params":{"cwd":"/tmp","mcpServers":[]}}

// Response (received over WS, correlated by id=42)
{"jsonrpc":"2.0","id":42,"result":{"sessionId":"20260726_8","modes":{...},"configOptions":[...]}}

// Notification (received over WS, method without id)
{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"20260726_8","update":{"sessionUpdate":"agent_message_chunk",...}}}
```

---

## 3. New Files

### 3.1 `internal/provider/acphttp/config.go` (35 lines)

```go
package acphttp

import "time"

type Config struct {
    Bin               string
    AlwaysApprove     bool
    DefaultCWD        string
    Model             string
    PermissionTimeout time.Duration
    Prewarm           bool
    TurnStallNotice   time.Duration
    AuthMethodID      string
    McpServers        []McpServer
}

type McpServer struct {
    Name      string
    Transport string
    URL       string
    Headers   map[string]string
}
```

**Note**: `Args` and `FSRoots` from `ACPProviderConfig` are intentionally absent — the engine argv is fixed (`goose serve --host 127.0.0.1 --port PORT --dangerously-unauthenticated`) and filesystem callbacks are a stdio transport concern.

### 3.2 `internal/provider/acphttp/spec.go` (50 lines)

```go
package acphttp

import (
    "github.com/maccavelli/magic-cli-remote/internal/command"
    "github.com/maccavelli/magic-cli-remote/internal/event"
    "github.com/maccavelli/magic-cli-remote/internal/picker"
    "github.com/maccavelli/magic-cli-remote/internal/provider"
)

type Spec struct {
    ID              provider.ID
    DefaultBin      string
    ServeArgs       func(port int) []string
    HealthPath      string
    StaticModels    []picker.Option
    StaticModes     []event.SessionMode
    DefaultModeID   string
    Commands        command.Table
}
```

**Design rationale**: No `ConfigureSession` callback needed — goose exposes model selection as a standard ACP config option ("model"), so Start sets it via `session/set_config_option` with `optionID="model"`. No `ListModels` callback — goose returns model options in `session/new`'s `configOptions`.

### 3.3 `internal/provider/acphttp/conn.go` (90 lines)

Initializes the ACP connection and handles optional auth:

```go
type acpConn struct {
    connID   string
    baseURL  string
    httpc    *http.Client
}

func (c *acpConn) initialize(ctx context.Context) error {
    req := acp.InitializeRequest{
        ProtocolVersion: 1,
        ClientInfo: &acp.Implementation{
            Name:    "mcremote",
            Version: "dev",
        },
        ClientCapabilities: acp.ClientCapabilities{},
    }
    var resp acp.InitializeResponse
    if err := c.postJSON(ctx, "initialize", req, &resp); err != nil {
        return fmt.Errorf("initialize: %w", err)
    }
    // Auth if needed
    if len(resp.AuthMethods) > 0 && c.cfg.AuthMethodID != "" {
        if err := c.authenticate(ctx); err != nil {
            return fmt.Errorf("authenticate: %w", err)
        }
    }
    return nil
}

// postJSON posts a JSON-RPC 2.0 request and decodes the response body.
// Only used for initialize (before WebSocket is established).
func (c *acpConn) postJSON(ctx context.Context, method string, params, result any) error {
    // POST /acp with Acp-Connection-Id, parse JSON-RPC response
}

func (c *acpConn) dialWS(ctx context.Context) (*websocket.Conn, error) {
    // ws://127.0.0.1:PORT/acp with Acp-Connection-Id header
}
```

### 3.4 `internal/provider/acphttp/ws.go` (180 lines)

```go
type rpcPending struct {
    ch    chan rpcResponse
    timer *time.Timer
}
type rpcResponse struct {
    result json.RawMessage
    error  *rpcError
}
type rpcError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}

type wsReadPump struct {
    ws      *websocket.Conn
    log     *slog.Logger
    pending map[int64]chan rpcResponse
    nextID  atomic.Int64
    mu      sync.Mutex
}

// sendRequest sends a JSON-RPC 2.0 request over the WebSocket and waits for
// the matching response (correlated by auto-incrementing id). Returns the
// JSON-RPC result or an error.
func (p *Provider) sendRequest(ctx context.Context, method string, params any) (json.RawMessage, error) {
    id := p.nextID.Add(1)
    ch := make(chan rpcResponse, 1)
    p.mu.Lock()
    p.pending[id] = ch
    p.mu.Unlock()
    defer func() {
        p.mu.Lock()
        delete(p.pending, id)
        p.mu.Unlock()
    }()

    msg := map[string]any{
        "jsonrpc": "2.0",
        "id":      id,
        "method":  method,
        "params":  params,
    }
    data, _ := json.Marshal(msg)
    if err := p.ws.Write(ctx, websocket.MessageText, data); err != nil {
        return nil, fmt.Errorf("ws write: %w", err)
    }

    select {
    case res := <-ch:
        if res.error != nil {
            return nil, &acpError{Code: res.error.Code, Message: res.error.Message}
        }
        return res.result, nil
    case <-ctx.Done():
        return nil, ctx.Err()
    }
}

// readPump runs in a goroutine, reading all messages from the WebSocket
// and routing them to either the pending-response map or session dispatch.
func (p *Provider) readPump() {
    defer p.ws.Close(websocket.StatusNormalClosure, "read pump done")
    for {
        _, data, err := p.ws.Read(context.Background())
        if err != nil {
            p.handleWSError(err)
            return
        }
        var base struct {
            ID     int64           `json:"id"`
            Method string          `json:"method"`
            Result json.RawMessage `json:"result"`
            Error  *rpcError       `json:"error"`
        }
        if err := json.Unmarshal(data, &base); err != nil {
            p.log.Debug("ws parse error", slog.String("err", err.Error()))
            continue
        }
        if base.ID != 0 && (base.Result != nil || base.Error != nil) {
            // Response to a pending request
            p.mu.Lock()
            ch, ok := p.pending[base.ID]
            p.mu.Unlock()
            if ok {
                ch <- rpcResponse{result: base.Result, error: base.Error}
            }
        } else if base.Method != "" {
            // Notification (session/update, request_permission, etc.)
            p.routeNotification(base.Method, data)
        }
    }
}
```

### 3.5 `internal/provider/acphttp/provider.go` (350 lines)

The main provider struct — engine lifecycle, session registry, WS management.

```go
type Provider struct {
    spec Spec
    cfg  Config
    log  *slog.Logger

    mu       sync.Mutex
    eng      *engine
    starting bool
    closed   bool

    ws        *websocket.Conn
    connID    string
    sessions  map[string]*session
    pending   map[int64]chan rpcResponse
    nextID    atomic.Int64
    generation int

    httpc *http.Client
}

type engine struct {
    cmd  *exec.Cmd
    url  string
    port int
    dead chan struct{}
}

func (p *Provider) Ready() bool {
    _, err := exec.LookPath(p.cfg.Bin)
    return err == nil
}

func (p *Provider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
    base, err := p.ensureServer(ctx)
    if err != nil {
        return nil, err
    }
    return p.startSession(ctx, base, opts)
}

func (p *Provider) startSession(ctx context.Context, base string, opts provider.StartOptions) (*session, error) {
    s := newSession(p, p.cfg, opts, p.log)
    if err := s.create(ctx, base); err != nil {
        return nil, err
    }
    p.mu.Lock()
    p.sessions[s.agentID] = s
    p.mu.Unlock()
    return s, nil
}

// ensureServer returns a healthy engine URL, spawning one if needed.
func (p *Provider) ensureServer(ctx context.Context) (string, error) {
    // Same pattern as httpagent/provider.go ensureServer:
    // 1. Check p.eng under lock; if present, return p.eng.url
    // 2. If starting, wait
    // 3. Call startServer
}

func (p *Provider) startServer(ctx context.Context) (string, error) {
    port, _ := freePort()
    cmd := exec.Command(p.cfg.Bin, p.spec.ServeArgs(port)...)
    procutil.SetProcessGroup(cmd)
    procutil.SetDeathSignal(cmd)
    // ... health poll, initialize, WS dial ...
    // Publish engine
    go p.deathMonitor(cmd, dead)
    return url, nil
}

func (p *Provider) deathMonitor(cmd *exec.Cmd, dead chan struct{}) {
    <-dead
    p.mu.Lock()
    p.eng = nil
    sessions := p.sessions
    p.sessions = make(map[string]*session)
    p.mu.Unlock()
    // Fail all sessions
    if !p.closed {
        // exponential backoff → respawn
    }
}

func (p *Provider) Shutdown() {
    p.mu.Lock()
    p.closed = true
    eng := p.eng
    p.eng = nil
    p.mu.Unlock()
    if eng != nil {
        procutil.TerminateProcessGroup(eng.cmd.Process, eng.dead, 5*time.Second)
    }
}
```

### 3.6 `internal/provider/acphttp/session.go` (450 lines)

Session lifecycle and event mapping.

```go
type session struct {
    provider *Provider
    cfg      Config
    opts     provider.StartOptions
    log      *slog.Logger

    mu        sync.Mutex
    localID   string
    agentID   string
    closed    bool
    events    chan event.Event
    turnBusy  bool

    agentCaps  acp.AgentCapabilities
    staticModes []event.SessionMode
}

func newSession(p *Provider, cfg Config, opts provider.StartOptions, log *slog.Logger) *session {
    localID := opts.LocalSessionID
    if localID == "" {
        localID = uuid.NewString()
    }
    return &session{
        provider: p,
        cfg:      cfg,
        opts:     opts,
        localID:  localID,
        events:   make(chan event.Event, 256),
        log:      log.With(slog.String("session", localID)),
    }
}

func (s *session) create(ctx context.Context, base string) error {
    params := acp.NewSessionRequest{
        Cwd:      resolveCWD(s.opts.CWD, s.cfg.DefaultCWD),
        McpServers: buildMcpServers(s.cfg.McpServers),
    }
    data, err := s.provider.sendRequest(ctx, "session/new", params)
    if err != nil {
        return err
    }
    var resp acp.NewSessionResponse
    json.Unmarshal(data, &resp)
    s.agentID = string(resp.SessionId)

    // Emit modes
    s.emitModesOrStatic(resp.Modes)
    // Emit config options
    s.emitConfigOptions(resp.ConfigOptions)
    // Emit capabilities (defaults from initialize)
    s.emitCapabilities(nil)
    // Apply per-session model override
    if s.opts.Model != "" {
        if err := s.setModel(ctx, s.opts.Model); err != nil {
            s.log.Warn("model override failed", slog.String("err", err.Error()))
        }
    }
    return nil
}

func (s *session) setModel(ctx context.Context, model string) error {
    params := map[string]any{
        "sessionId": s.agentID,
        "optionId":  "model",
        "value":     model,
    }
    _, err := s.provider.sendRequest(ctx, "session/set_config_option", params)
    return err
}

func (s *session) Prompt(ctx context.Context, parts []provider.Content) error {
    s.mu.Lock()
    if s.closed { s.mu.Unlock(); return fmt.Errorf("session closed") }
    s.turnBusy = true
    s.mu.Unlock()
    defer func() {
        s.mu.Lock()
        s.turnBusy = false
        s.mu.Unlock()
    }()

    prompt := make([]acp.ContentBlock, 0, len(parts))
    for _, p := range parts {
        prompt = append(prompt, acp.ContentBlock{
            Text: &acp.ContentBlockText{Type: "text", Text: p.Text},
        })
    }
    params := map[string]any{
        "sessionId": s.agentID,
        "prompt":    prompt,
    }
    _, err := s.provider.sendRequest(ctx, "session/prompt", params)
    return err
}

func (s *session) Cancel(ctx context.Context) error {
    err := s.provider.sendNotification(ctx, "session/cancel", map[string]any{
        "sessionId": s.agentID,
    })
    return err
}

func (s *session) Close(ctx context.Context) error {
    s.mu.Lock()
    if s.closed { s.mu.Unlock(); return nil }
    s.closed = true
    s.mu.Unlock()
    _, err := s.provider.sendRequest(ctx, "session/close", map[string]any{
        "sessionId": s.agentID,
    })
    close(s.events)
    return err
}

// handleUpdate is called from the WS read pump when a session/update
// notification arrives. Identical mapping to acpagent/session.go:SessionUpdate
// (lines 943-1085), with the addition of ConfigOptionUpdate.
func (s *session) handleUpdate(notif json.RawMessage) {
    var u acp.SessionUpdate
    json.Unmarshal(notif, &u)
    now := time.Now().UTC()
    switch {
    case u.AgentMessageChunk != nil:
        text := contentText(u.AgentMessageChunk.Content)
        if text == "" { return }
        s.emit(event.Event{Type: event.TypeAssistantChunk, SessionID: s.localID, Timestamp: now, Text: text})
    case u.AgentThoughtChunk != nil:
        text := contentText(u.AgentThoughtChunk.Content)
        if text == "" { return }
        s.emit(event.Event{Type: event.TypeThoughtChunk, SessionID: s.localID, Timestamp: now, Text: text})
    case u.ToolCall != nil:
        // ...same as acpagent...
    case u.ToolCallUpdate != nil:
        // ...same as acpagent...
    case u.Plan != nil:
        // ...same as acpagent...
    case u.PlanRemoved != nil:
        // ...same as acpagent...
    case u.AvailableCommandsUpdate != nil:
        // ...same as acpagent...
    case u.CurrentModeUpdate != nil:
        // ...same as acpagent...
    case u.ConfigOptionUpdate != nil:
        // ★ NEW streaming handler — also add to acpagent for parity
        s.emitConfigOptions(u.ConfigOptionUpdate.ConfigOptions)
    case u.UsageUpdate != nil:
        // ...same as acpagent...
    default:
        s.log.Debug("unhandled session update")
    }
}

// handlePromptResponse is called when the JSON-RPC response to session/prompt
// arrives (with stopReason, usage).
func (s *session) handlePromptResponse(data json.RawMessage) {
    var resp struct {
        StopReason string `json:"stopReason"`
    }
    json.Unmarshal(data, &resp)
    if resp.StopReason == "end_turn" {
        s.emit(event.Event{Type: event.TypeTurnComplete, SessionID: s.localID, Timestamp: time.Now().UTC()})
    }
}
```

### 3.7 `internal/provider/goose/goose.go` (75 lines)

```go
package goose

import (
    "strconv"
    "github.com/maccavelli/magic-cli-remote/internal/event"
    "github.com/maccavelli/magic-cli-remote/internal/picker"
    "github.com/maccavelli/magic-cli-remote/internal/provider"
    "github.com/maccavelli/magic-cli-remote/internal/provider/acphttp"
)

type Config = acphttp.Config

var staticModels = []picker.Option{
    {ID: "poolside/laguna-s-2.1:free", Label: "Laguna S 2.1", Group: "poolside"},
    {ID: "x-ai/grok-code-fast-1", Label: "Grok Code Fast 1", Group: "xai"},
    {ID: "anthropic/claude-sonnet-4.5", Label: "Claude Sonnet 4.5", Group: "anthropic"},
    {ID: "google/gemini-2.5-pro", Label: "Gemini 2.5 Pro", Group: "google"},
    {ID: "google/gemini-2.5-flash", Label: "Gemini 2.5 Flash", Group: "google"},
    {ID: "deepseek/deepseek-r1-0528", Label: "DeepSeek R1", Group: "deepseek"},
}

var staticModes = []event.SessionMode{
    {ID: "auto", Name: "Auto", Description: "Automatically approve tool calls"},
    {ID: "approve", Name: "Approve", Description: "Ask before every tool call"},
    {ID: "smart_approve", Name: "Smart Approve", Description: "Ask only for sensitive tool calls"},
    {ID: "chat", Name: "Chat", Description: "Chat only, no tool calls"},
}

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

### 3.8 `internal/provider/goose/commandtable.go` (50 lines)

```go
package goose

import "github.com/maccavelli/magic-cli-remote/internal/command"

var commandTable = command.Table{
    "help":     {Kind: command.KindDaemon},
    "plan":     {Kind: command.KindNone, Note: "goose has permission modes, not plan/build modes — use /mode"},
    "mode":     {Kind: command.KindDaemon},
    "model":    {Kind: command.KindDaemon},
    "clear":    {Kind: command.KindDaemon},
    "new":      {Kind: command.KindDaemon},
    "sessions": {Kind: command.KindDaemon},
    "context":  {Kind: command.KindNone, Note: "goose doesn't expose token breakdown over ACP"},
    "compact":  {Kind: command.KindNative, Native: "compact"},
    "goal":     {Kind: command.KindNative, Native: "goal"},
    "diff":     {Kind: command.KindNone, Note: "no diff RPC over ACP"},
    "undo":     {Kind: command.KindNone, Note: "undo is git-based, not exposed over ACP"},
    "redo":     {Kind: command.KindNone, Note: "same as undo"},
    // Goose-specific commands (advertised via available_commands_update)
    "status": {Kind: command.KindNative, Native: "status"},
    "grind":  {Kind: command.KindNative, Native: "grind"},
    "skills": {Kind: command.KindNative, Native: "skills"},
    "doctor": {Kind: command.KindNative, Native: "doctor"},
}
```

### 3.9 `internal/provider/goose/live_test.go` (55 lines)

```go
//go:build live_goose

package goose_test

import (
    "context"
    "testing"
    "time"
    "github.com/maccavelli/magic-cli-remote/internal/event"
    "github.com/maccavelli/magic-cli-remote/internal/provider"
    "github.com/maccavelli/magic-cli-remote/internal/provider/goose"
)

func TestLiveGoosePrompt(t *testing.T) {
    p := goose.New(goose.Config{AlwaysApprove: true})
    if !p.Ready() { t.Skip("goose not in PATH") }
    ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
    defer cancel()
    s, err := p.Start(ctx, provider.StartOptions{Name: "live", CWD: t.TempDir()})
    if err != nil { t.Fatalf("start: %v", err) }
    defer s.Close(context.Background())

    if err := s.Prompt(ctx, []provider.Content{{Type: "text", Text: "Reply with exactly the word pong and nothing else."}}); err != nil {
        t.Fatalf("prompt: %v", err)
    }
    deadline := time.Now().Add(110 * time.Second)
    for time.Now().Before(deadline) {
        select {
        case ev, ok := <-s.Events():
            if !ok { t.Fatal("events closed early") }
            t.Logf("event type=%s status=%s text=%q", ev.Type, ev.Status, ev.Text)
            if ev.Type == event.TypeTurnComplete { return }
            if ev.Type == event.TypeError { t.Fatalf("agent error: %s", ev.Error) }
        case <-time.After(100 * time.Millisecond):
        }
    }
    t.Fatal("timeout waiting for turn_complete")
}
```

---

## 4. Modified Files

### 4.1 `internal/provider/provider.go` — Add IDGoose

```go
const (
    IDFake     ID = "fake"
    IDGrok     ID = "grok"
    IDOpencode ID = "opencode"
    IDGoose    ID = "goose"  // NEW
)
```

### 4.2 `internal/config/config.go` — Add GooseProviderConfig

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

In `Defaults()`:

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

In `Validate()` — add:

```go
if c.Providers.Goose.PermissionTimeoutSeconds < 0 {
    return fmt.Errorf("providers.goose.permission_timeout_seconds must be >= 0, got %d",
        c.Providers.Goose.PermissionTimeoutSeconds)
}
if c.Providers.Goose.TurnStallNoticeSeconds < 0 {
    return fmt.Errorf("providers.goose.turn_stall_notice_seconds must be >= 0, got %d",
        c.Providers.Goose.TurnStallNoticeSeconds)
}
if err := validateACPProvider("goose", c.Providers.Goose.ACPProviderConfig); err != nil {
    return err
}
```

Also update the `anyEnabled` check (line 167) to include `cfg.Providers.Goose.Enabled`.

### 4.3 `internal/config/load.go` — Add viper defaults + env bindings

In `setDefaults`:

```go
v.SetDefault("providers.goose.enabled", d.Providers.Goose.Enabled)
v.SetDefault("providers.goose.bin", d.Providers.Goose.Bin)
v.SetDefault("providers.goose.always_approve", d.Providers.Goose.AlwaysApprove)
v.SetDefault("providers.goose.default_cwd", d.Providers.Goose.DefaultCWD)
v.SetDefault("providers.goose.model", d.Providers.Goose.Model)
v.SetDefault("providers.goose.permission_timeout_seconds", d.Providers.Goose.PermissionTimeoutSeconds)
v.SetDefault("providers.goose.prewarm", d.Providers.Goose.Prewarm)
v.SetDefault("providers.goose.turn_stall_notice_seconds", d.Providers.Goose.TurnStallNoticeSeconds)
v.SetDefault("providers.goose.auth_method_id", d.Providers.Goose.AuthMethodID)
```

Env aliases (automatic via `MCREMOTE_PROVIDERS_GOOSE_*` prefix, no explicit `BindEnv` needed for the standard `MCREMOTE_` prefix pattern — viper's `AutomaticEnv` handles these since `setDefaults` registers the keys).

### 4.4 `internal/daemon/daemon.go` — Register goose provider

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

Also add `acpHTTPConfig` converter:

```go
func acpHTTPConfig(c config.ACPProviderConfig) acphttp.Config {
    mcp := make([]acphttp.McpServer, 0, len(c.MCPServers))
    for _, m := range c.MCPServers {
        mcp = append(mcp, acphttp.McpServer{
            Name:      m.Name,
            Transport: m.Transport,
            URL:       m.URL,
            Headers:   m.Headers,
        })
    }
    return acphttp.Config{
        Bin:               c.Bin,
        AlwaysApprove:     c.AlwaysApprove,
        DefaultCWD:        c.DefaultCWD,
        Model:             c.Model,
        PermissionTimeout: time.Duration(c.PermissionTimeoutSeconds) * time.Second,
        Prewarm:           c.Prewarm,
        TurnStallNotice:   time.Duration(c.TurnStallNoticeSeconds) * time.Second,
        AuthMethodID:      c.AuthMethodID,
        McpServers:        mcp,
    }
}
```

Update the `anyEnabled` check:

```go
if anyEnabled := cfg.Providers.Fake.Enabled || cfg.Providers.Grok.Enabled ||
    cfg.Providers.Opencode.Enabled || cfg.Providers.Goose.Enabled; anyEnabled {
```

### 4.5 `configs/config.example.yaml` — Add goose block

```yaml
  goose:
    enabled: false              # opt-in
    bin: "goose"
    args: []                    # ignored for HTTP engine (serve argv is fixed)
    always_approve: false
    default_cwd: ""
    model: ""                   # empty uses goose's own config default
    permission_timeout_seconds: 120
    prewarm: true
    turn_stall_notice_seconds: 120
    auth_method_id: ""
    mcp_servers: []
```

### 4.6 `configs/config.mesh-grok.yaml` — Add same goose block

---

## 5. Backport: `config_option_update` to `acpagent`

Add a case to `acpagent/session.go:SessionUpdate` (after the `UsageUpdate` case):

```go
case u.ConfigOptionUpdate != nil:
    s.emitConfigOptions(u.ConfigOptionUpdate.ConfigOptions)
```

This ensures the streaming handler exists in both transports. Without this, a future agent that emits `config_option_update` at runtime (not just at create/load) would have its config changes silently dropped by `acpagent`'s `default:` log line.

---

## 6. Test Plan

| Layer | What | How | File |
|---|---|---|---|
| Unit: event mapping | Every SessionUpdate variant maps correctly | Test `handleUpdate` with crafted JSON | `acphttp/session_test.go` |
| Unit: config_option_update | Streaming config changes emit TypeSessionConfig | Test with `SessionConfigOptionUpdate` fixture | `acphttp/session_test.go` |
| Unit: WS sendRequest | Request/response correlation by id | Mock WS conn, verify matching | `acphttp/provider_test.go` |
| Unit: WS readPump | Route response by id, notification by method | Mock WS conn, inject frames | `acphttp/provider_test.go` |
| Unit: engine lifecycle | Spawn → health → init → WS → death → respawn | Mock subprocess, fake health endpoint | `acphttp/provider_test.go` |
| Unit: command table | ResolveAll returns expected mechanisms | Table-driven over all entries | `goose/goose_test.go` |
| Config: parse + validate | YAML → struct, validation rejects bad config | Existing `config_test.go` pattern | `config/acp_config_test.go` |
| Live: full round-trip | Real `goose serve`, init → session → prompt → turn_complete | Build tag `live_goose` | `goose/live_test.go` |
| Smoke: registry | Goose registers iff enabled | Integration test | `daemon/daemon_test.go` |

All existing tests must pass: `go test ./...`

---

## 7. Pre-add Check

```bash
make pre-add-check FILES="internal/provider/acphttp/*.go internal/provider/goose/*.go"
```

This runs `gofmt`, `golint`, and `govulncheck`. Fix any issues before staging.

---

## 8. Implementation Order

1. **`acphttp/config.go`** + `spec.go` — types only, no logic
2. **`acphttp/conn.go`** — initialize + auth + WS dial
3. **`acphttp/ws.go`** — WebSocket read pump + sendRequest + correlation
4. **`acphttp/provider.go`** — engine lifecycle + session registry + Shutdown
5. **`acphttp/session.go`** — session lifecycle + event mapping + emit helpers
6. **`acphttp/*_test.go`** — unit tests for each component
7. **`goose/goose.go`** + `commandtable.go` — dialect package
8. **`goose/live_test.go`** — live probe test
9. **Config modifications** — provider.go, config.go, load.go, daemon.go, example yamls
10. **Backport** — `config_option_update` handler to `acpagent/session.go`
11. **`go test ./...`** — green across the board

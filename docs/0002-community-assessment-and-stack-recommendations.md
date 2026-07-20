# Report: Community Landscape Assessment & Foundation Stack Recommendations for `mcremote`

- **Status**: Draft for review
- **Date**: 2026-07-19
- **Audience**: Project owner / design reviewers
- **Related**: [MADR 0001](./0001-architecture-mcremote.md)
- **Scope of this report**: Community project analysis, best practices synthesis, Headscale/Tailscale posture, Cobra/Viper/slog/Go 1.26.x recommendations, Linux + macOS foundation guidance

---

## 1. Executive summary

The remote-control market for coding agents has matured rapidly in 2025–2026. Three architectural patterns dominate:

| Pattern | Examples | Core idea |
|---------|----------|-----------|
| **A. Structured protocol host (ACP / JSON-RPC)** | [grok-remote](https://github.com/daniel-farina/grok-remote) | Daemon speaks ACP to agent stdio; streams structured events to a UI |
| **B. Outbound relay + E2E encryption** | [Shellular](https://shellular.dev/), [Agents At Work](https://www.agentsatwork.app), Claude Code Remote Control | Host opens outbound connection; phone never needs inbound ports; QR pairing for keys |
| **C. Terminal / PTY proxy** | [VibeTunnel](https://vibetunnel.sh/), classic SSH | Browser/phone is a terminal; works with any CLI but loses structured tool/permission UX |

`mcremote` (MADR 0001) correctly aims at **Pattern A + hybrid networking (Headscale/Tailscale primary, optional relay later)**. That combination is differentiated: most commercial apps own a cloud relay; grok-remote is the closest open-source peer and is Tailscale-first + ACP-first, but Node/TypeScript and Grok-only.

**Recommended differentiation for `magic-cli-remote`:**

1. **Go daemon** (`mcremote`) — single static binary, systemd/launchd native, low memory, strong process control.
2. **Provider adapters** — Grok Build ACP first; Antigravity and others later (not Grok-only).
3. **Self-hosted mesh first** — Headscale + Tailscale clients; no mandatory vendor cloud for connectivity.
4. **Flutter client** — native mobile (vs PWA-only or iOS-only peers).
5. **WebSocket + JSON control plane** — simple for Flutter; map cleanly from ACP session events.

This report is **not** a commit to implement features listed under “later.” It is a review baseline for Foundation design.

---

## 2. Landscape map (projects assessed)

### 2.1 Closest peers (directly relevant)

#### grok-remote (+ PWA)

| | |
|--|--|
| **URL** | https://github.com/daniel-farina/grok-remote |
| **Stack** | TypeScript/Node, Vite PWA UI, PM2 process manager |
| **Networking** | Tailscale tailnet (or `--local` bind to 127.0.0.1) |
| **Agent protocol** | Full ACP host over `grok agent stdio` (JSON-RPC NDJSON) |
| **UI** | Web + installable PWA (mobile-optimized: 44px targets, safe-area, dynamic viewport) |

**Strengths to learn from:**

- Implements a **real ACP client/host**: not just forwarding prompts — handles `terminal/*`, `fs/*`, `session/request_permission`.
- **Per-agent isolation**: `~/.grok-remote/agents/<id>/cwd/` + `meta.json` + append-only `history.jsonl`.
- **SSE stream** of every `session/update` (thought chunks, tool cards, token usage).
- **Reconnect semantics**: disconnect kills process but keeps session; next prompt does `session/load`.
- **Ops CLI** (`gr status|logs|start|stop`) separate from the dashboard.
- **PROTOCOL.md** culture: probe agent stdio, document wire format — extremely valuable for ACP work.
- Flags observed in practice: `grok agent --no-leader --always-approve stdio` for multi-process independence.

**Gaps relative to mcremote:**

- Grok-only; Node/PM2 (not a single Go binary).
- No first-class Flutter; PWA is good enough for many but weaker offline/push than native.
- Optional bearer auth still “what’s next”; relies heavily on Tailscale perimeter.
- No Headscale-specific docs (works with any Tailscale-compatible network, including Headscale).

**Takeaway:** Treat grok-remote as the **reference implementation for Grok ACP hosting**. Port the *concepts* (agent manager, history JSONL, SSE/event bus, permission host) into Go — not the TypeScript code.

---

#### Shellular (shellular.dev)

| | |
|--|--|
| **URL** | https://shellular.dev/ · CLI docs https://shellular.dev/docs/ |
| **Stack** | Host CLI (Node/npx), mobile apps (iOS/Android), open-source relay |
| **Networking** | **Outbound relay** (`wss://api.shellular.dev`) — primary path |
| **Agents** | Claude Code, Codex, OpenCode, Hermes, Cursor CLI, Pi, Grok Build, Copilot, … |
| **Surfaces** | Agent UI + full terminal + files/git + localhost tunnel + browser DevTools |

**Strengths to learn from:**

- **Product completeness**: “whole stack in your pocket,” not chat-only.
- **Security model** (industry gold standard for relays):
  - E2E encryption; relay is ciphertext-only.
  - Pairing QR carries encryption key; key never traverses the network.
  - Device approval on host (`requires-approval` / allow / reject).
  - Account allowlist as a second gate.
- **Daemon UX**: `start` / `stop` / `status` / `logs` / `clients` / `startup` (PM2 under the hood).
- **Single instance** per machine (clear operational constraint).
- Relay server is open-sourced → pattern for a future optional mcremote relay.

**Gaps:**

- Closed product economics; host is not a pure Go control plane.
- Less emphasis on typed multi-provider ACP; more “forward the environment.”
- Depends on their relay unless self-hosting their server.

**Takeaway:** Steal **pairing, device approval, E2E, and host-daemon command surface**. Defer full “localhost + DevTools + file editor” until after agent remote control is solid.

---

#### Agents At Work (“You can Go!”)

| | |
|--|--|
| **URL** | App Store / agentsatwork.app |
| **Stack** | iOS app + macOS companion |
| **Networking** | Firebase relay; **AES-256 E2E** after QR key exchange |
| **Agents** | Claude Code, Codex, Gemini CLI, Grok CLI, Ollama (chat + agentic for some models) |
| **UX focus** | Push notifications, one-tap permission approvals, conversation feed, multi-agent color coding |

**Strengths to learn from:**

- **Permission-centric mobile UX** — the killer feature for long agent runs is *approve/deny without opening a laptop*.
- Push when waiting / finished / error (Claude Code Remote Control is converging on the same idea).
- Multi-agent session list with status: Running / Thinking / Waiting / Finished.
- Auto-approve mode with visible badge.
- Explicit trust story: “code stays on Mac; Firebase never sees plaintext.”

**Gaps:**

- macOS-only host (no Linux daemon).
- Proprietary companion; hard to audit.
- Firebase dependency (not Headscale-native).

**Takeaway:** For Flutter UI, **permission cards + push + multi-session status** are table stakes. Plan the event schema so approvals are first-class messages, not afterthoughts.

---

#### Claude Code Remote Control (first-party pattern)

| | |
|--|--|
| **Docs** | https://code.claude.com/docs/en/remote-control |
| **Networking** | **Outbound-only HTTPS** to vendor API; no inbound ports |
| **UX** | QR / session URL; mobile app + web; multi-device sync |
| **Security** | Short-lived scoped credentials; org Trusted Devices (enterprise); no ZDR orgs blocked |

**Strengths to learn from:**

- Outbound-only model is the cleanest answer to “machine behind VPN.”
- Session resume, reconnection queues, worktree spawn modes, capacity limits.
- Push notifications with presence awareness (suppress push if user is at the machine).
- Clear limitations documentation (process must stay running, ~10 min network timeout, etc.).

**Gaps for us:**

- Vendor lock-in; Grok/Antigravity not supported.
- Transcript stored on vendor servers during remote — may conflict with self-hosted privacy goals.

**Takeaway:** Document **operational limits** the same way (host awake, process alive, reconnect policy). Prefer **self-hosted mesh/relay** so transcript storage stays under user control.

---

### 2.2 Related infrastructure / orchestration (adjacent)

| Project | Relevance |
|---------|-----------|
| **[VibeTunnel](https://github.com/amantus-ai/vibetunnel)** | Terminal-in-browser; recommends Tailscale; PTY/WebSocket architecture. Good for “raw terminal fallback” later, not Foundation primary. |
| **[AWS CAO](https://github.com/awslabs/cli-agent-orchestrator)** | Multi-agent orchestration over tmux + MCP; multi-provider including Antigravity. Teaches **provider adapter profiles**, session lifecycle, localhost-only security + DNS rebinding guards. Not a phone remote. |
| **superagent-ai/grok-cli** | Telegram remote control with **6-char pair code** + first-user approval. Lightweight messaging channel pattern. |
| **CodeAgent Mobile** | VS Code extension + phone; QR/6-digit pairing; async prompt relay for IDE agents. Confirms pairing UX universality. |
| **Coder Agents / Augment Remote Agents** | Cloud workers / enterprise sandboxes — different problem (cloud execution vs local remote control). |

### 2.3 Positioning matrix

| Capability | grok-remote | Shellular | Agents At Work | Claude RC | **mcremote (planned)** |
|------------|:-----------:|:---------:|:--------------:|:---------:|:----------------------:|
| Structured ACP (Grok) | Yes | Partial | Unknown | N/A | **Yes (primary)** |
| Multi-provider adapters | No | Yes (env forward) | Yes | Claude only | **Yes (design)** |
| Outbound relay | No | Yes | Yes (Firebase) | Yes (Anthropic) | Later / optional |
| Tailscale / mesh | **Yes** | Optional | No | No | **Headscale primary** |
| Mobile native | PWA | Yes | iOS | Official apps | **Flutter** |
| Linux host | Yes | Yes | No | Yes | **Yes** |
| macOS host | Yes | Yes | Yes | Yes | **Yes** |
| Go single binary | No | No | No | N/A | **Yes** |
| Self-host / OSS control plane | Yes | Partial | No | No | **Yes** |

---

## 3. Cross-cutting best practices (what the market converged on)

### 3.1 Networking

1. **Never depend on inbound ports as primary** on corporate/VPN machines.
2. **Two proven primaries:**
   - **Mesh VPN** (Tailscale / Headscale): LAN-like latency, identity-based ACLs, no custom crypto if you trust the mesh.
   - **Outbound relay + E2E**: works without mesh on the phone; needs careful key exchange (QR).
3. **Hybrid is winning** (matches MADR 0001): mesh when available, relay when not.
4. **Local bind default for dev**: `127.0.0.1` only unless mesh/relay enabled.
5. **Host identity**: expose hostname, OS, version, tailnet name in a `/hello` or equivalent (grok-remote pattern).

### 3.2 Pairing & auth

Nearly every product uses some combination of:

| Mechanism | Use |
|-----------|-----|
| QR code | Bootstrap URL + shared secret / encryption key |
| Short pair code (6 chars) | Telegram / CLI confirm-in-terminal |
| Device allowlist | Remember approved phones |
| Account allowlist | Second gate (Shellular) |
| Mesh identity | Tailscale user/device as authN (grok-remote) |
| Optional bearer / JWT | Defense in depth on top of mesh |

**Recommendation for Foundation:**

- Phase 0: **Headscale/Tailscale perimeter only** + bind to tailnet IP or localhost; no public bind.
- Phase 1: **Device pairing token** (JWT or random secret stored in config) for WebSocket auth even on the mesh.
- Phase 2: Optional E2E app-layer encryption if a public relay is added.

### 3.3 Session & process model

| Practice | Why |
|----------|-----|
| One OS process per agent session (or per conversation) | Crash isolation; mirrors grok-remote |
| Persist `sessionId` + cwd + history on disk | Survive daemon restart |
| Soft archive vs hard delete | User safety |
| Graceful shutdown: save session IDs, SIGTERM children | Avoid orphan agents |
| Explicit connect/disconnect | Free CPU when idle without losing chat |
| Working directory isolation | Multi-agent safety on same machine |

### 3.4 Event model (UI contract)

Successful UIs render a **turn-based stream of typed blocks**, not a raw log:

- user_message  
- thought_chunk (collapsible)  
- tool_call (status: pending → running → completed/failed)  
- tool_output_delta  
- assistant_message_chunk  
- permission_request (**must be interruptible on phone**)  
- turn_complete (tokens, stop reason)  
- session_status  

Transport choices seen:

- **SSE** (grok-remote) — excellent for server→client, awkward for client→server duplex.
- **WebSocket** (Shellular, many terminals) — duplex; better for approvals + prompts + streaming.
- **Vendor streaming API** (Claude RC).

**Recommendation:** WebSocket + JSON (MADR 0001) with **request/response ids** and **server-push events** (like JSON-RPC-ish messages without full JSON-RPC ceremony). Keep event names stable and versioned (`v1.tool_call`).

### 3.5 ACP-specific (Grok Build)

From grok-remote PROTOCOL probing:

- Transport: NDJSON JSON-RPC 2.0 on stdio.
- Must implement **client-side** `terminal/*`, `fs/*`, and `session/request_permission` or tools fail.
- Prefer `session/new` + `session/prompt` + `session/load` + cancel.
- Stream via `session/update` discriminators.
- Grok extensions: `_x.ai/*` notifications (models, git head, session notifications).
- Multi-agent: `--no-leader`; careful with `--always-approve` vs real remote permission UX.

**Recommendation:** Use `github.com/coder/acp-go-sdk` (≥ v0.13.5) and still maintain an **internal Event type** so Flutter is not coupled to raw ACP.

### 3.6 Host daemon UX

Command surface users expect (align with Shellular / `gr`):

```text
mcremote serve          # run foreground (dev)
mcremote start|stop     # optional service helpers
mcremote status
mcremote logs
mcremote pair           # show QR / pair code
mcremote providers      # list adapters
mcremote version
```

Config locations (XDG + macOS conventions):

- Linux: `~/.config/mcremote/config.yaml`, state in `~/.local/share/mcremote/`
- macOS: same XDG under home, or `~/Library/Application Support/mcremote/` (pick one and document; XDG works on both if documented)

### 3.7 Security checklist (Foundation)

- [ ] Default listen: `127.0.0.1` or Tailscale interface only  
- [ ] No “auto-approve everything” default in production config  
- [ ] Permission requests always remoted to UI when not auto-approved  
- [ ] Redact secrets in slog (tokens, pair secrets)  
- [ ] DNS rebinding / Host header checks if HTTP server is used (CAO lesson)  
- [ ] Clear data directory permissions (`0700`)  
- [ ] Document that phone on Headscale is trusted as network peer — still add app auth  

---

## 4. Headscale + Tailscale posture

You plan to run **open-source Headscale** as the control plane and use official Tailscale clients on daemon hosts and phones.

### 4.1 Why this fits mcremote

- Solves “daemon on internal network, phone not on VPN” without building a relay in v1.
- Identity and ACLs live in Headscale policy (huJSON; prefer **Grants** over legacy ACLs).
- grok-remote and VibeTunnel already validated “Tailscale as the remote fabric” for agent UIs.

### 4.2 Operational recommendations

1. **Tag nodes**: e.g. `tag:mcremote-host`, `tag:mcremote-client`.
2. **Grants**: only client tags may open TCP to host tag on the mcremote port (7531; this doc predates that decision and shows the exploratory 7910 below — see [0003](0003-phase1-decisions.md)).
3. **MagicDNS**: give hosts stable names (`devbox.mcremote.ts.net` style) for Flutter deep links.
4. **Do not** bind `0.0.0.0` on untrusted interfaces even on Headscale hosts; bind Tailscale IP or use interface binding where possible.
5. **Auth layering**: mesh = transport auth; still implement application-level device tokens for defense in depth (stolen laptop scenarios).
6. **Headscale reload**: policy changes need SIGHUP/reload — document for operators.
7. **Phone clients**: iOS/Android Tailscale apps must join the same Headscale tailnet (custom coordination server URL).

### 4.3 Hybrid path (later)

When mesh is unavailable (travel, locked-down phone):

- Optional outbound relay (Shellular-style) with E2E.
- Or temporary public tunnel (ngrok/cloudflared) — accept as last resort, not default.

MADR 0001’s hybrid model remains correct; **Foundation can ship Headscale-only**.

---

## 5. Go stack recommendations (aligned with your constraints)

### 5.1 Language version

- **Toolchain: Go 1.26.5** (or latest 1.26.x patch).
- In `go.mod`, set `go 1.26` (or `1.26.0` per module policy). Note: Go 1.26’s `go mod init` defaults new modules to an older compatible version — override explicitly.

### 5.2 Go 1.26 modernizations to adopt deliberately

| Feature | Why it matters for mcremote |
|---------|-----------------------------|
| **Green Tea GC (default)** | Better mark/scan for many small objects (JSON events, WebSocket frames). Expect lower GC overhead under multi-session streaming. No code change required. |
| **Faster small allocations / more stack-backed slices** | Hot paths in JSON encode/decode and event fan-out benefit automatically. Prefer small structs and avoid unnecessary heap escapes in event paths. |
| **`new(expr)`** | Cleaner optional pointer fields in config/API structs (`Age: new(yearsSince(born))` style). |
| **`errors.AsType[T](err)`** | Type-safe error classification for provider/adapter failures. |
| **`log/slog.NewMultiHandler`** | Dual sinks: JSON file + pretty text for terminal without third-party glue. |
| **`os/signal.NotifyContext` + cancel cause** | Graceful daemon shutdown with signal reason in logs. |
| **`os.Process.WithHandle`** | Better child-process control on Linux (pidfd) when managing agent processes. |
| **Experimental `goroutineleak` profile** | Enable in CI (`GOEXPERIMENT=goroutineleakprofile`) to catch session teardown leaks early. |
| **`runtime/secret` (experimental)** | Consider later for pairing secrets / JWT HMAC material wipe; not Foundation-critical. |
| **`go fix` modernizers** | Run periodically after upgrades. |
| **macOS note** | Go 1.26 is last release supporting macOS 12; 1.27 will require macOS 13+. Document minimum: **macOS 13+**, modern Linux (glibc/musl as you choose). |

### 5.3 Memory & struct efficiency guidelines

1. **Event structs**: use value types for small enums/status; avoid large interface{} maps on hot paths — prefer typed fields + `json.RawMessage` only for provider-specific extensions.
2. **Ring buffers** for per-session event history (bounded memory), with disk JSONL as durability (grok-remote pattern).
3. **Object pooling** (`sync.Pool`) only after profiling; Green Tea reduces pressure — measure first.
4. **Context everywhere** for session and provider I/O; cancel on disconnect.
5. **One goroutine ownership** per session writer to ACP stdin; fan-out events via channels with clear backpressure (drop or slow-client disconnect policy).
6. **Avoid unbounded `[]byte` growth** on tool output — stream chunks with size limits (mirror ACP `outputByteLimit`).

### 5.4 slog (structured logging)

**Standards to enforce:**

- `log/slog` only (no zap/zerolog unless bridging).
- Production: `JSONHandler` to stdout/stderr (journald/launchd friendly).
- Dev: `TextHandler` or tinted wrapper.
- Use **`slog.Attr` consistently**; enable `sloglint` with `attr-only: true`.
- Child loggers with stable keys:

```text
component=session|provider|ws|acp
session_id=...
provider=grok
peer=...
```

- Levels: Debug (protocol frames, gated), Info (lifecycle), Warn (retries), Error (failures).
- **Never log** pair secrets, JWT, or full prompt bodies at Info (opt-in debug with redaction).
- Wire `slog.NewMultiHandler` if you need file + console simultaneously.

### 5.5 Cobra best practices

| Practice | Detail |
|----------|--------|
| `cmd/mcremote/main.go` | Thin: `cmd.Execute()` only |
| Commands as packages/files under `internal/cli` or `cmd/mcremote/` | One command per file for larger trees |
| Prefer **`RunE`** | Errors bubble to `SilenceUsage` / consistent exit codes |
| Persistent flags | `--config`, `--log-level`, `--log-format` |
| Subcommands | `serve`, `version`, later `pair`, `status` |
| Version | Inject via `-ldflags` (`main.version`, commit, date) |
| Completions | `cobra` completion generation for bash/zsh/fish |
| Don’t put business logic in `cmd` | Call into `internal/daemon`, `internal/session`, etc. |

Suggested initial tree:

```text
cmd/mcremote/main.go
internal/cli/root.go
internal/cli/serve.go
internal/config/
internal/daemon/
internal/session/
internal/provider/          # Provider interface
internal/provider/grok/     # ACP adapter
internal/event/
internal/ws/                # local WebSocket server
internal/logging/
```

### 5.6 Viper best practices

Official guidance: **do not use the global Viper singleton**; construct `viper.New()` and pass (or wrap in a `Config` struct).

**Precedence (high → low):**

1. Command-line flags  
2. Environment variables (`MCREMOTE_*`)  
3. Config file (`config.yaml`)  
4. Defaults in code  

**Integration pattern:**

- Define a typed `config.Config` struct (yaml/json/mapstructure tags).
- Bind flags → viper keys carefully (Carolyn Van Slyck “Sting of the Viper” pattern still applies).
- `AutomaticEnv` + `SetEnvPrefix("MCREMOTE")` + `SetEnvKeyReplacer(".", "_", "-", "_")`.
- Unmarshal once at startup into immutable `Config`; pass by value or `*Config` to subsystems.
- Optional: `WatchConfig` for log level only; full daemon reconfig usually needs restart for Foundation.

**Example keys:**

```yaml
listen:
  host: "127.0.0.1"
  port: 7531   # decided in 0003; this doc's exploratory value was 7910
log:
  level: info
  format: json   # json|text
data_dir: ""     # default XDG
providers:
  grok:
    enabled: true
    bin: "grok"
    args: ["agent", "stdio"]
auth:
  mode: "tailnet"   # tailnet|token|none (dev only)
  token: ""
tailscale:
  # documentation only in v1 — mesh is external
```

### 5.7 Other libraries (confirm MADR 0001)

| Need | Recommendation |
|------|----------------|
| ACP | `github.com/coder/acp-go-sdk` ≥ v0.13.5 |
| WebSocket | Prefer `github.com/coder/websocket` (ex-nhooyr) or `gorilla/websocket` — pick one; coder/websocket is modern context-friendly |
| CLI | `github.com/spf13/cobra` |
| Config | `github.com/spf13/viper` + optional `mapstructure` |
| Auth later | `github.com/golang-jwt/jwt/v5` |
| Logging | stdlib `log/slog` |
| Testing | stdlib `testing` + `net/http/httptest`; table-driven; fake Provider |

Avoid premature gRPC.

### 5.8 Service packaging (Linux + macOS)

| Platform | Foundation approach |
|----------|---------------------|
| Linux | Document systemd user unit (`mcremote.service`) running `mcremote serve` |
| macOS | Document launchd agent plist (user-level) |
| Both | Foreground `serve` for dev; optional `mcremote install-service` later |
| Process supervision | Prefer native init over PM2 (Go binary doesn’t need Node’s PM2) |

---

## 6. Recommended Foundation architecture (refined from MADR + landscape)

```text
                    ┌─────────────────────────────┐
   Flutter app      │  WebSocket + JSON (v1)       │
   (via Headscale)  │  auth: device token / mesh  │
                    └─────────────┬───────────────┘
                                  │
                    ┌─────────────▼───────────────┐
                    │         mcremote            │
                    │  ┌─────────┐  ┌──────────┐  │
                    │  │  WS API │  │ Event Bus│  │
                    │  └────┬────┘  └────▲─────┘  │
                    │       │            │        │
                    │  ┌────▼────────────┴─────┐  │
                    │  │    Session Manager    │  │
                    │  └────┬────────────┬─────┘  │
                    │       │            │        │
                    │  ┌────▼────┐  ┌────▼─────┐  │
                    │  │ Grok    │  │ (future  │  │
                    │  │ ACP     │  │ adapters)│  │
                    │  │ Adapter │  │          │  │
                    │  └────┬────┘  └──────────┘  │
                    └───────┼─────────────────────┘
                            │ stdio ACP
                    ┌───────▼───────┐
                    │ grok agent    │
                    │ stdio         │
                    └───────────────┘
```

**Data dir layout (inspired by grok-remote, Go-idiomatic):**

```text
$data_dir/
  config overrides (optional)
  sessions/
    <session-id>/
      meta.json
      history.jsonl
      cwd/                 # optional sandbox cwd
  device-tokens.json       # later
```

**Core interfaces (Foundation):**

```go
type Provider interface {
    ID() string
    Start(ctx context.Context, opts StartOptions) (Session, error)
}

type Session interface {
    ID() string
    Prompt(ctx context.Context, parts []Content) error
    Cancel(ctx context.Context) error
    Events() <-chan Event
    Close(ctx context.Context) error
}
```

Map ACP ↔ `Event` inside the Grok adapter only.

---

## 7. Competitive differentiation checklist

| Do (mcremote) | Don’t (unless later) |
|---------------|----------------------|
| Excellent Grok ACP fidelity | Boil the ocean with full IDE-on-phone |
| Headscale-first security story | Require a proprietary cloud |
| Multi-provider interface from day one | Hard-code Grok types into the WS schema |
| Flutter-native UX for approvals | Ship terminal-only (VibeTunnel already exists) |
| Single Go binary + systemd/launchd | Depend on Node/PM2 for the daemon |
| Durable session history | Ephemeral-only memory sessions |
| Documented protocol version | Ad-hoc undocumented JSON |

---

## 8. Risks & open decisions (for review)

| ID | Topic | Options / note |
|----|-------|----------------|
| R1 | **WS schema ownership** | Design a stable `mcremote.v1` schema; do not stream raw ACP to Flutter |
| R2 | **Permission policy** | Default remote-approve vs local auto-approve flags (dev convenience vs safety) |
| R3 | **Bind address discovery** | Manual config vs `tailscale ip -4` helper vs Headscale MagicDNS only |
| R4 | **History retention** | Days-based GC like grok-remote “what’s next” |
| R5 | **PTY fallback provider** | For Antigravity if ACP unavailable — degraded mode |
| R6 | **Relay timeline** | Ship mesh-only first; relay only when phone cannot join Headscale |
| R7 | **go.mod module path** | e.g. `github.com/<org>/magic-cli-remote` — confirm before first push |
| R8 | **Minimum OS** | Linux: modern amd64/arm64; macOS 13+ (Go 1.27 will require it) |

---

## 9. Suggested Foundation milestone (implementation order)

1. **Scaffold**: module, Cobra root/`serve`, Viper config, slog, `version`.  
2. **Event + Session Manager** in-memory with fake provider.  
3. **Local WebSocket server** + minimal JSON protocol + `websocat`/script client.  
4. **Grok ACP adapter** (initialize, session/new, prompt, stream updates, fs/terminal stubs).  
5. **Disk persistence** (meta + history.jsonl).  
6. **Permission events** over WebSocket (approve/deny).  
7. **Headscale packaging docs** + example grants.  
8. **systemd + launchd** unit templates.  
9. Flutter client (separate track once WS schema freezes).

---

## 10. Sources (primary)

Community & products:

- https://github.com/daniel-farina/grok-remote  
- https://shellular.dev/ · https://shellular.dev/docs/  
- https://www.agentsatwork.app / App Store listing “Agents At Work”  
- https://code.claude.com/docs/en/remote-control  
- https://vibetunnel.sh/ · https://github.com/amantus-ai/vibetunnel  
- https://github.com/awslabs/cli-agent-orchestrator  
- https://github.com/superagent-ai/grok-cli  
- https://agentclientprotocol.com · https://github.com/coder/acp-go-sdk  

Platform & Go:

- https://headscale.net/stable/ref/policy/  
- https://go.dev/doc/go1.26  
- https://go.dev/blog/slog  
- https://github.com/spf13/cobra · https://github.com/spf13/viper  

---

## 11. Review ask

Please confirm or correct:

1. **Networking v1**: Headscale/Tailscale only (no relay) — OK?  
2. **Auth v1**: mesh-only vs mesh + device token from day one?  
3. **Config format**: YAML as default — OK?  
4. **Data dir**: XDG on both Linux and macOS — OK?  
5. **Port default**: 7910 (grok-remote familiar) vs something else? → **Resolved: 7531** ([0003](0003-phase1-decisions.md)).  
6. **Any provider besides Grok** required in the first milestone, or interface-only?

Once reviewed, the next concrete step is scaffolding the Go module and freezing a short `docs/protocol-v1.md` WebSocket schema.

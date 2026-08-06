# MADR 0075 — Kilo CLI as a session provider

<!-- markdownlint-disable MD013 MD024 MD033 MD036 -->

| field | value |
| --- | --- |
| status | **accepted for implementation** (Milestone 0 live spike complete 2026-08-06 on **kilo 7.4.20**; provider package not started) |
| plan | [0075-PLAN-kilo-cli-provider.md](0075-PLAN-kilo-cli-provider.md) (proposed 2026-08-06, phases P0–P4) |
| date | 2026-08-06 |
| deciders | @saxsmith |
| related | MADR 0011 (OpenCode provider), **0019** (single-engine), **0020** (session tree), **0021** (OpenCode HTTP API), **0023** (slash commands), **0024** (stream coalescing), **0025** (goose), **0028** (codex), **0029** (provider platform), **0031** (catalog), **0037** (CLI uptake), **0043** (models), **0074** (remote auth) |
| method | Codebase (`httpagent`, `opencode`, daemon, config); official Kilo docs; **live wire spike** against installed `kilo` 7.4.20 — artifacts in [docs/kilo-spike-7.4.20/](./kilo-spike-7.4.20/) (`summary.json`); **auth re-probe 2026-08-06 after host credentials added** (Appendix E) |
| known-good CLI | **`kilo` 7.4.20** (`/opt/homebrew/bin/kilo` ← npm `@kilocode/cli`) |

**Host probe (this workspace, 2026-08-06):**

| binary | result |
| --- | --- |
| `opencode` | **1.18.11** (`~/.opencode/bin/opencode`) |
| `kilo` | **7.4.20** on PATH (`/opt/homebrew/bin/kilo`) |
| Live serve | **passed** — health, OpenAPI 243 paths, session create, `prompt_async` 204, full SSE turn (assistant **PONG**), abort, catalogs, ACP stdio initialize |

---

## 0. Executive summary

**Decision.** Add **`kilo`** as a fifth real mcremote session provider, driven by a **daemon-owned shared `kilo serve` engine** over existing **`httpagent`**, with a thin **`internal/provider/kilo` dialect** forked from `internal/provider/opencode`.

**Spike verdict: high compatibility, full feature capacity for platform session loop.** On kilo **7.4.20**:

| Capability | Live result |
| --- | --- |
| Shared HTTP engine | `kilo serve --hostname 127.0.0.1 --port <p>` (+ **`--pure` confirmed**) |
| Health | `GET /global/health` → `{"healthy":true,"version":"7.4.20"}` |
| Basic Auth | Required when `KILO_SERVER_PASSWORD` set; user **`kilo`** works; user **`opencode`** → **401** |
| OpenAPI | `GET /doc` → OpenAPI **3.1.0**, title `kilo`, **243** paths, **453** schemas |
| Session create | `POST /session` → `ses_*` id, directory scoped |
| Prompt | `POST /session/{id}/prompt_async` → **204**; body model `{providerID, modelID}` |
| SSE stream | `GET /global/event` — same GlobalEvent envelope OpenCode decoder already unwraps |
| Assistant stream | `message.part.delta` + `message.part.updated` (text **PONG** via openrouter free) |
| Turn lifecycle | `session.status` busy/idle, `session.idle`, `session.turn.open` / `session.turn.close` |
| Errors | `session.error` with structured message (bad model id) |
| Abort | `POST /session/{id}/abort` → 200 `true` |
| Catalogs | `GET /agent` (10), `GET /command`, `GET /provider` (**179** providers, multi-MB — cache required) |
| Agents | primary: `code`, `ask`, `debug`, `plan`, `orchestrator`; subagents `explore`, `general` |
| Permissions API | `POST …/permissions/{id}` body `{response: once\|always\|reject}` |
| Questions API | `POST /question/{id}/reply` with `answers` arrays |
| Directory routing | `x-kilo-directory`, **`x-opencode-directory`**, and `?directory=` all **200** |
| ACP stdio | `kilo acp` initialize → protocolVersion **1**, loadSession/fork/list/resume, authMethod `kilo-login` |
| Auth store | `~/.local/share/kilo/auth.json` (0600); spike day: 0 file creds. **Re-probed 2026-08-06 after operator login:** 2 creds — `kilo` (**oauth**, Gateway session) + `opencode-go` (**api** key); env `OPENROUTER_API_KEY` + `HF_TOKEN` still picked up |
| Credential API | **Live-proven:** `PUT /auth/{providerID}` (body `Auth` schema) and `DELETE /auth/{providerID}` both return `true` and edit `auth.json` (Appendix E) |

**Why ship.** mcremote’s OpenCode path is the same architectural shape; wire compatibility is **proven**, not inferred. Kilo remains a **distinct** product (paths, Basic user, Gateway defaults, `~` model aliases, Kilo-only routes).

**What this is not.**

| Surface | Role for mcremote |
| --- | --- |
| Kilo Gateway API | Upstream inference (0074) — not the session provider |
| `kilo run` | Reject as control plane |
| `kilo remote` / cloud | Out of scope |
| “Just set bin=kilo on opencode package” | Reject long-term — Basic user, model aliases, Kilo SSE extras, config homes differ |

**Order of work.** ~~Milestone 0~~ **done** → dialect + daemon register → mobile picker free → 0074 credential hooks later.

---

## 1. Context — mcremote provider platform today

### 1.1 Registered agents (code reality)

| Provider ID | Transport | Package | Engine model | Registration |
| --- | --- | --- | --- | --- |
| `fake` | in-process | `internal/provider/fake` | none | `daemon.go` if enabled |
| `grok` | ACP **stdio** | `acpagent` + `grok` | **per-session** subprocess | `cfg.Providers.Grok.Enabled` |
| `goose` | ACP over **WebSocket** | `acphttp` + `goose` | **one** `goose serve` | `cfg.Providers.Goose.Enabled` |
| `opencode` | **REST + SSE** | `httpagent` + `opencode` | **one** `opencode serve` | `cfg.Providers.Opencode.Enabled` |
| `codex` | app-server JSON-RPC | `codex` | one `codex app-server` | `cfg.Providers.Codex.Enabled` |

Well-known IDs live in `internal/provider/provider.go` (`IDGrok`, `IDOpencode`, `IDGoose`, `IDCodex`, `IDFake`). There is **no** `IDKilo` yet.

Phone `session.create` already accepts a `provider` field; `providers.list` returns `{id, ready}` from the registry. Adding a registered, ready provider surfaces it in the mobile picker without a protocol change (same pattern as OpenCode MADR 0011 / Goose 0025).

### 1.2 OpenCode HTTP path (the template Kilo should follow)

OpenCode is **not** driven over ACP anymore (MADR **0019**). Reasons that apply equally to Kilo:

- Per-session ACP = full engine cold start + multi-engine DB contention.
- Shared `serve` is the surface the product itself uses for IDE/programmatic clients.
- HTTP retains session ops ACP lacked or under-featured (`/undo`, `/redo`, fork, diff, catalog, session tree).

As-built OpenCode integration (code):

| Concern | Implementation |
| --- | --- |
| Launch | `ServeArgs(port)` → `opencode serve --hostname 127.0.0.1 --port <p>` [+ optional `--pure`] (`opencode/http.go:579`) |
| Health | `GET /global/health` |
| Events | `GET /global/event` (SSE; process-wide demux) |
| Dialect | `opencode.httpDialect` implements `httpagent.Dialect` (+ Model/Agent/Command listers, tree hooks) |
| Ownership | `procutil` env stamps (`MCREMOTE_ENGINE_ID` / `MCREMOTE_ENGINE_OWNER`, `procutil/reap.go`) + process group + death signal; reject foreign engines |
| Config | `providers.opencode.*` in `OpencodeProviderConfig` (incl. `RetiredTransport` guard rejecting the pre-0019 `transport` key) |
| Daemon | `opencode.NewHTTPWithLogger(...)` then `reg.Register(op)` (`daemon.go:182,198`) |
| **Engine auth** | **None.** As-built, the opencode engine is not password-gated: no `OPENCODE_SERVER_PASSWORD` is set and `httpagent` sends **no Authorization header** on health poll, SSE dial, or REST calls (`httpagent/provider.go:491,625,874`); the `Dialect` interface has no header hook. Kilo's password-gated spawn (D5) therefore requires **new httpagent capability**, not reuse — see §4.4 |

`httpagent.Dialect` contract (`internal/provider/httpagent/httpagent.go`):

```text
ID, DefaultBin, ServeArgs(port), HealthPath, EventsPath,
AfterBoot, DecodeFrame, NewSession → DialectSession
  Create / Resume / Replay / Prompt / Abort /
  RespondPermission / RespondQuestion / Delete / Resync …
```

### 1.3 Product gap

Users can already run OpenCode, Goose, Codex, and Grok from the phone. They cannot start a **Kilo CLI** session. Kilo brings Gateway-default models, Kilo Pass balance routing, and a large installed IDE base — as a **distinct agent product**, not only as a Gateway upstream.

---

## 2. Kilo CLI product surfaces (research)

Sources: [CLI docs](https://kilo.ai/docs/code-with-ai/platforms/cli), [CLI reference](https://kilo.ai/docs/code-with-ai/platforms/cli-reference), [CLI runtime architecture](https://kilo.ai/docs/contributing/architecture/cli-runtime), [Kilo vs OpenCode](https://kilo.ai/cli/opencode), [Gateway](https://kilo.ai/docs/gateway), [Gateway auth](https://kilo.ai/docs/gateway/authentication), [ACP registry / Zed](https://zed.dev/acp/agent/kilo).

### 2.1 Install and identity

| Item | Value |
| --- | --- |
| Binary | `kilo` |
| npm | `@kilocode/cli` (`npm install -g @kilocode/cli`) |
| Other installs | curl installer, Homebrew `Kilo-Org/tap/kilo`, GitHub release binaries (incl. baseline for non-AVX) |
| Lineage | **Fork of OpenCode**; Kilo adds Gateway, Pass, IDE/cloud product layer |
| Config (global) | `~/.config/kilo/kilo.json[c]` (legacy `opencode.json[c]` names under that tree) |
| Config (project) | `./kilo.json[c]`, `./.kilo/`, legacy `.kilocode/` still read |
| Auth credentials | `auth.json` under **data** dir (XDG: typically `~/.local/share/kilo/auth.json`), mode `0600` |
| DB | SQLite default under data dir (`kilo.db`); override `KILO_DB` |
| OpenCode isolation | Kilo **does not** fall back to `~/.config/opencode` / project `.opencode/` — operators must migrate deliberately |

### 2.2 Surface assessment for mcremote

| Surface | Entry | Role for mcremote |
| --- | --- | --- |
| Interactive TUI | `kilo` | Operator only |
| Non-interactive run | `kilo run [message] [--auto] [--format json]` | Probe / CI; **reject** as session transport |
| **Headless HTTP server** | **`kilo serve`** | **Chosen primary** — HTTP + SSE for local clients |
| Attach | `kilo attach <url>` | Human debugging; documents Basic Auth flags |
| Daemon | `kilo daemon start\|status\|stop` | Optional host convenience; **mcremote must own its own serve child** (do not adopt foreign daemon — same KD as OpenCode 0019) |
| **ACP** | **`kilo acp`** | Alternate; registry lists npm `acp`; spike if serve dialect diverges too far |
| Auth CLI | `kilo auth list\|login\|logout` (alias **`kilo providers`**), TUI `/connect` | Host credential setup; phone path via 0074 later |
| Models | `kilo models [provider]` | Catalog probe / doctor |
| Web UI | `kilo web` (serve + browser UI) | Operator only; mcremote spawns plain `serve` |
| Session ops CLI | `kilo session`, `kilo export [sessionID]`, `kilo import <file>` (JSON) | Debug / ops tooling; not a transport |
| Remote / cloud | `kilo remote`, `kilo cloud *` | **Out of scope** |
| Console | `kilo console` | **Deprecated** upstream — ignore |

(Full 7.4.20 subcommand surface also includes `mcp`, `agent`, `stats`, `github`, `pr`, `plugin`, `db`, `roll-call`, `profile`, `upgrade`, `uninstall`, `config` — none are session-transport candidates; re-verified via `kilo --help` 2026-08-06.)

### 2.3 `kilo serve` (primary integration contract) — live 7.4.20

**CLI flags** (`kilo serve --help` on host):

```text
kilo serve
  --port         default 0
  --hostname     default 127.0.0.1
  --mdns         default false
  --mdns-domain  default kilo.local
  --cors         additional origins
  --pure         run without external plugins   ← CONFIRMED (same idea as OpenCode)
```

**mcremote spawn policy (decision, spike-validated):**

1. Bind **`127.0.0.1`** + ephemeral free port.
2. Set random **`KILO_SERVER_PASSWORD`**; HTTP Basic with username **`kilo`** (override via `providers.kilo.server_username` / env). **Never** send username `opencode` — live **401**.
3. Optional **`--pure`** via `providers.kilo.pure` (default false, match OpenCode).
4. Stamp `MCREMOTE_ENGINE_*` ownership; do **not** adopt host `kilo daemon`.
5. No mDNS / non-loopback. (Not hypothetical: `--mdns` help states it **defaults hostname to `0.0.0.0`** — enabling it would expose the engine off-host.)

**Argv:**

```text
kilo serve --hostname 127.0.0.1 --port <port> [--pure]
```

**Local serve access (live):**

| Probe | HTTP |
| --- | --- |
| With Basic `kilo:<password>` | **200** health |
| No auth | **401** |
| Basic `opencode:<password>` | **401** |

### 2.4 HTTP + SSE API — live inventory (7.4.20)

OpenAPI: `GET /doc` returns raw OpenAPI **3.1.0** JSON (`info.title=kilo`, **243** paths). Full path list: [kilo-spike-7.4.20/openapi-paths.txt](./kilo-spike-7.4.20/openapi-paths.txt).

#### Critical routes (all present and exercised or schema-confirmed)

| Method | Path | Live note |
| --- | --- | --- |
| `GET` | `/global/health` | `{"healthy":true,"version":"7.4.20"}` |
| `GET` | `/global/event` | SSE GlobalEvent stream; heartbeats |
| `GET` | `/event` | Also streams (instance bus); prefer global |
| `POST` | `/session` | Creates `ses_*`; accepts title/directory |
| `GET` | `/session/{sessionID}` | Session metadata |
| `POST` | `/session/{sessionID}/prompt_async` | **204**; model object required |
| `POST` | `/session/{sessionID}/abort` | **200** `true` |
| `GET` | `/session/{sessionID}/message` | History replay |
| `POST` | `/session/{sessionID}/permissions/{permissionID}` | body `{response: once\|always\|reject}` |
| `POST` | `/session/{sessionID}/fork` | OpenAPI present |
| `GET` | `/session/{sessionID}/children` | OpenAPI present (session tree) |
| `GET` | `/session/{sessionID}/todo` | OpenAPI present |
| `POST` | `/session/{sessionID}/revert` / `unrevert` | OpenAPI present |
| `GET` | `/agent` | 10 agents live |
| `GET` | `/command` | Built-ins + project commands |
| `GET` | `/provider` | **179** providers; ~4.7 MB — **must cache** |
| `GET` | `/config/providers` | defaults map — **auth-state-dependent**: kilo → `kilo-auto/free` unauthenticated, `kilo-auto/balanced` with Gateway OAuth (re-probe 2026-08-06) |
| `GET` | `/kilo/auth-status` | `{"authenticated":false}` spike day; `{"authenticated":true,"type":"oauth"}` after Gateway login — response carries a `type` field |
| `PUT` | `/auth/{providerID}` | **Live-proven** credential write (`auth.set`, body `Auth`, returns `true`) — 0074 write path |
| `DELETE` | `/auth/{providerID}` | **Live-proven** credential removal (returns `true`) — 0074 clear path |
| `GET` | `/provider/auth` | Map providerID → typed auth methods (13 providers live; oauth + api with prompt specs) |
| `POST` | `/provider/{providerID}/oauth/authorize` | `{method: <index>, inputs?}` → `{url, method: "auto"\|"code", instructions}` — engine-hosted OAuth start |
| `POST` | `/provider/{providerID}/oauth/callback` | `{method, code}` → boolean — completes code-paste OAuth without any local browser |

Kilo-only / extended surfaces (not required for v1 session loop): `/kilocode/*`, `/kilo/cloud/*`, `/api/*` v2-style aliases, Agent Manager, indexing, etc.

#### SSE envelope (compatible with OpenCode decoder)

`/global/event` frames are JSON objects:

```json
{
  "directory": "/path/to/cwd",
  "project": "<project-id>",
  "payload": {
    "id": "evt_…",
    "type": "message.part.delta",
    "properties": { "sessionID": "ses_…", "…": "…" }
  }
}
```

**Fact:** `opencode.httpDialect.DecodeFrame` already unwraps `{payload:{type,properties}}` (`http.go` ~720–739). Kilo can reuse that logic unchanged for demux.

**Event types observed on a successful turn** (see `sse-or.raw` / `sse-samples.json`):

| type | Role for mcremote |
| --- | --- |
| `server.connected` / `server.heartbeat` | Stream alive |
| `session.status` (`busy`/`idle`) | Running UI / EndTurn |
| `session.idle` | Turn idle signal |
| `session.turn.open` / `session.turn.close` | Turn boundaries (`reason: error` on failure) |
| `session.error` | Provider errors → agenterr / error cards |
| `session.updated` / `session.diff` | Metadata / diffs |
| `message.part.updated` | Full part snapshots (text, reasoning, tools) |
| `message.part.delta` | Token deltas: `{sessionID,messageID,partID,field,delta}` |
| `message.part.removed` | Transient UI cleanup |
| `session.next.agent.switched` / `session.next.model.switched` | Agent/model apply |
| `sync` | Sync bus (ignore or treat as non-transcript) |
| `file.watcher.updated` | Ignore for chat |

**Dialect extras to handle:**

1. **Synthetic lifecycle text** — parts with `metadata.kilocode.lifecycle: "transient"` (e.g. “Initializing snapshot…”) must **not** be rendered as assistant chat (filter).
2. **`reasoning` parts** — map to thought chunks (parity with OpenCode thought path).
3. **`session.turn.close`** — useful EndTurn signal alongside `session.idle` / status idle.

#### Prompt body (live)

```json
{
  "parts": [{ "type": "text", "text": "…" }],
  "model": { "providerID": "openrouter", "modelID": "openrouter/free" },
  "agent": "ask"
}
```

- Returns **204** when accepted.
- Wrong model id → still 204 enqueue, then **`session.error`**: e.g. `Model not found: kilo/deepseek/…. Did you mean: ~deepseek/…?`
- Kilo provider models often use **`~vendor/model`** aliases; CLI list also shows `kilo/~anthropic/…` form. Catalog picker must expose **engine-valid** ids, not invented concatenations.

**Successful turn (this host):** `openrouter` + `openrouter/free` with env `OPENROUTER_API_KEY` → assistant parts including text **`PONG`**.

#### Directory routing (live)

All three work (GET /session → 200):

- Header `x-kilo-directory`
- Header `x-opencode-directory` (compat)
- Query `?directory=`

mcremote OpenCode dialect already uses `?directory=`; keep that for Kilo (proven).

### 2.5 `kilo acp` (alternate) — live stdio

| Probe | Result |
| --- | --- |
| Command | `kilo acp` (stdio JSON-RPC line protocol) |
| `initialize` | **protocolVersion 1** |
| Capabilities | `loadSession`, MCP http/sse, prompt image + embeddedContext, session close/fork/list/resume |
| authMethods | `[{id: "kilo-login", name: "Login with Kilo", description: "Run \`kilo auth login\` in the terminal"}]` |
| agentInfo | `Kilo` / `7.4.20` |
| Re-probe | 2026-08-06: initialize response reproduced byte-for-byte in capability content (stdio, no flags) |
| Help flags | `--port`, `--hostname`, `--pure`, `--cwd` (network ACP optional; **stdio works without --port**) |

**Platform stance (unchanged, now evidence-backed):** Primary = **serve + httpagent**. ACP is a viable fallback and useful for live parity tests, but **do not** ship dual operator transports.

### 2.6 Auth / credentials (host and phone) — live, re-probed 2026-08-06

Spike-day facts, then the 2026-08-06 re-probe after the operator logged into Kilo Gateway and added an OpenCode Go key (full transcript: Appendix E):

| Path | Spike day (unauthenticated) | Re-probe 2026-08-06 (authenticated) |
| --- | --- | --- |
| `kilo debug paths` | config `~/.config/kilo`, data `~/.local/share/kilo`, cache `~/.cache/kilo`, state `~/.local/state/kilo` | unchanged |
| `kilo auth list` | **0** credentials; env OpenRouter + HF | **2** credentials — **Kilo Gateway (`kilo`, type `oauth`)** + **OpenCode Go (`opencode-go`, type `api`)**; env unchanged |
| `~/.local/share/kilo/auth.json` | absent creds | mode 0600; entries `{kilo: oauth, opencode-go: api}` — same provider-id/type shape as OpenCode's `auth.json` |
| `GET /kilo/auth-status` | `{"authenticated":false}` | `{"authenticated":true,"type":"oauth"}` |
| `GET /config/providers` defaults | `kilo` → `kilo-auto/free` | `kilo` → **`kilo-auto/balanced`**, `opencode-go` → `gpt-5.6-luna`, openrouter → `google/gemini-3-pro-image-preview`, huggingface → `zai-org/GLM-5.2` |
| Connected providers | `["openrouter","huggingface","kilo"]` | `["openrouter","huggingface","opencode-go","kilo"]` |

**Key consequence:** the engine's default-model map is **a function of auth state**, not a constant. Static seeds and doctor output must not hard-code `kilo-auto/free` (see §4.2).

#### Credential write/read/OAuth API (live-proven 2026-08-06)

| Surface | Fact |
| --- | --- |
| `PUT /auth/{providerID}` | operationId `auth.set`; body is `Auth` = `ApiAuth {type:"api", key, metadata?}` \| `OAuth {type:"oauth", refresh, access, expires, accountId?, enterpriseUrl?}` \| `WellKnownAuth {type:"wellknown", key, token}`; returns `true`; entry appears in `auth.json` immediately (round-trip tested with a dummy provider id, then removed) |
| `DELETE /auth/{providerID}` | Returns `true`; entry removed from `auth.json` |
| Access control | Requires the serve Basic Auth (`kilo:<password>`) when `KILO_SERVER_PASSWORD` is set — same gate as every other route; no extra auth tier |
| `GET /provider/auth` | Returns 13 providers with typed method lists, e.g. `kilo` → **“Kilo Gateway (Device Authorization)”** (oauth), `openai` → “ChatGPT Pro/Plus (browser)” / **“ChatGPT Pro/Plus (headless)”** / API key, `github-copilot` → GitHub login, `gitlab`, `poe`, plus api-key entries; methods carry structured `prompts` (text/select field specs) |
| `POST /provider/{id}/oauth/authorize` | Body `{method: <index into method list>, inputs?: {field: value}}` → `ProviderAuthAuthorization {url, method: "auto"\|"code", instructions}` |
| `POST /provider/{id}/oauth/callback` | Body `{method, code}` → boolean — completes a `"code"`-mode flow with a user-pasted code; **no local browser or loopback callback needed on the host** |

**Ready policy:** binary-only Ready(); missing keys → ready still true; turn errors via `session.error`.

**Phone injection (0074):** `PUT /auth/{providerID}` is the proven key-write path and `DELETE` the clear path. For OAuth, the engine-hosted `authorize → {url, instructions} → callback {code}` loop means the phone can drive **device-style flows for Kilo Gateway and headless ChatGPT** by displaying the URL/instructions and posting the pasted code back — Strategy A shaped, no tunnel. `"auto"`-mode authorizations (browser redirect to an engine-local callback) remain the only case that would need 0074's Strategy B tunnel.

### 2.7 Permissions

| Layer | Detail |
| --- | --- |
| Config rules | host `kilo.jsonc` `permission` allow/ask/deny (inherited) |
| REST | `POST /session/{id}/permissions/{permissionID}` `{response: once\|always\|reject}` |
| Alternate | `POST /permission/{requestID}/reply` (`reply` enum + optional message) |
| mcremote | `always_approve` auto-responds `once`/`always` like OpenCode |

Permission **SSE ask** frames not forced in this spike (no tool-using turn); OpenAPI schemas include `EventPermissionAsked` / V2 variants — implement with fixtures when first live permission fires.

### 2.8 Agents (live `GET /agent`)

| name | mode | Use |
| --- | --- | --- |
| **`code`** | primary | **Default** full agent (not OpenCode’s `build`) |
| `ask` | primary | Q&A / no edits (spike used this) |
| `debug` | primary | Diagnostics |
| `plan` | primary | Plan files only |
| `orchestrator` | primary | Multi-agent coordination (still present; not deprecated in this version) |
| `explore` / `general` | **subagent** | Not top-level user turns |
| `compaction` / `summary` / `title` | primary + **hidden** | Filter from picker |

Static catalog default agent: **`code`** (not `build`).

### 2.9 What we explicitly will not integrate

1. **`kilo remote`** — account remote control of the machine.
2. **`kilo cloud`** — hosted agents.
3. **`kilo run` as the session bus**.
4. **Adopting host `kilo daemon`**.
5. **Sharing OpenCode engine/auth paths**.
6. **Rendering synthetic `kilocode.lifecycle` transient parts** as chat.

---

## 2.10 Platform compatibility matrix (post-spike)

| mcremote need | Kilo 7.4.20 | Gap |
| --- | --- | --- |
| Shared engine | Yes (`serve`) | — |
| Health + version gate | Yes | Pin min version after more field data |
| SSE multiplex | Yes (`/global/event`) | Unwrap already in OpenCode decoder |
| Create / prompt / abort | Yes | Model object shape |
| Stream text | Yes (delta + updated) | Filter transient synthetic parts |
| Thought/reasoning | Yes (`type: reasoning`) | Map to thought chunks |
| Permissions | API yes; SSE ask unproven this spike | Live permission fixture next |
| Questions | API yes | Same |
| Session tree / children / fork | Routes present | Enable `session_tree` after child event fixture |
| Model picker | Huge catalog | Cache; prefer connected providers |
| Agent picker | Yes | Default `code` |
| Slash commands | `/command` + POST command | Map 0023 table |
| Resume / replay | message list yes | Same as OpenCode |
| Process ownership | mcremote-side | Same as OpenCode |
| Phone protocol changes | None for v1 | — |

**Feature capacity rating:** **Full** for core remote session product (create → stream → complete/error → cancel → catalogs). **High** for parity features (tree, permissions, questions, undo/fork) with residual fixture work. **Out of scope** for Kilo cloud/remote product features.

---

## 3. Decision

### D1 — Provider identity

- Registry ID: **`kilo`** (`provider.IDKilo = "kilo"`).
- Config block: **`providers.kilo`**.
- Default: **`enabled: false`** until Milestone 0 + 1 green (flip to true after acceptance, same caution as early OpenCode).
- **Not** the default provider; `defaultProviderID` remains grok-first (or current policy).

### D2 — Transport: shared `kilo serve` + `httpagent`

- Implement `internal/provider/kilo` as a dialect package that constructs `httpagent.NewWithLogger(dialect, cfg, log)`.
- One shared engine per mcremote process; sessions are server-side objects.
- Reuse process ownership, prewarm, stall notice, stream coalesce, session tree **if** SSE/REST parity allows (gate tree on version probe like OpenCode).

### D3 — Do not ship ACP as a configurable second transport

- Spike may validate `kilo acp` for parity notes.
- Production path is serve-only unless spike fails hard on HTTP.

### D4 — Engine lifecycle invariant

> Each running mcremote owns exactly one `kilo serve` process it spawned, and no such process survives its owner.

Same enforcement stack as OpenCode (`procutil` group, death signal, engine registry, shutdown reap). Reject adoption of foreign `kilo serve` / `kilo daemon`.

### D5 — Auth and config isolation

- Engine uses Kilo XDG paths only.
- Credentials: host Kilo installation (`kilo auth` / Gateway / env). Daemon does not manage OAuth loops in v1.
- Local serve always password-gated when spawned by mcremote.

### D6 — Phone protocol

- No new control-plane messages required for v1 session create / prompt / cancel / permission / model list if existing OpenCode-shaped capabilities map cleanly.
- Capability bits / command table filled from live probe (MADR 0023 / 0036).
- Auth status / set_credential deferred to **0074** phases.

### D7 — Relationship to Gateway-as-upstream

- Shipping provider `kilo` does **not** replace configuring Kilo Gateway as an OpenCode/Goose upstream.
- Both can coexist: Gateway key in OpenCode for multi-model under `opencode`; full Kilo agent UX under `kilo`.

---

## 4. Design specifications

### 4.1 Package layout

```text
internal/provider/kilo/
  kilo.go            // NewHTTP / NewHTTPWithLogger → httpagent.Provider
  dialect.go         // Dialect: ServeArgs, Health, Events, DecodeFrame, AfterBoot
  session.go         // DialectSession: Create, Prompt, Abort, permissions, …
  catalog.go         // models / agents / commands (static + live)
  commandtable.go    // MADR 0023 table (probe-backed)
  version.go         // optional min version for session_tree
  live_test.go       // //go:build live_kilo
```

Prefer **copy-and-adapt from `opencode`** then delete divergences, rather than sharing one dialect with a `flavor` flag (fork drift will force painful conditionals). Extract shared helpers only after both dialects stabilize (post-acceptance cleanup).

### 4.2 Config schema

Add `KiloProviderConfig` to `ProvidersConfig` (mirror OpenCode, not ACP squash):

| key | type | default | notes |
| --- | --- | --- | --- |
| `enabled` | bool | `false` | until acceptance |
| `bin` | string | `"kilo"` | PATH lookup |
| `always_approve` | bool | `false` | permission auto-reply |
| `default_cwd` | string | `""` | same resolution as other providers |
| `model` | string | `""` | `provider/model` or Kilo id; empty = engine default |
| `permission_timeout_seconds` | int | `120` | same semantics as OpenCode |
| `prewarm` | bool | `true` | cold start likely Bun-class |
| `turn_stall_notice_seconds` | int | `120` | never 0 in shipped templates (0072 lesson) |
| `stream_coalesce_ms` | int | `80` | MADR 0024 |
| `session_tree` | bool | **`false` until child SSE fixtures**; then default true | routes present (`/children`, fork); events not fully exercised this spike |
| `server_username` | string | `"kilo"` | Basic Auth user (**must not** default to opencode). New field class — `OpencodeProviderConfig` has no auth fields today because the opencode engine runs un-gated (§1.2) |
| `pure` | bool | `false` | maps to `kilo serve --pure` (**live flag**) |

**Model default:** empty config model → engine default for connected provider; static offline fallback **`openrouter/openrouter/free`** is **invalid** as a single string — use picker ids as `providerID`/`modelID` pairs. Prefer static seeds:

- `kilo` / `kilo-auto/free` (Gateway free auto when account path works) — **caveat (re-probe 2026-08-06):** the engine's own default flips to `kilo-auto/balanced` once Gateway OAuth is present, so treat `/config/providers` as the runtime source of truth and never hard-code the `free` id in doctor/UI copy
- connected env providers (this host: openrouter free tier)

**Default agent:** `code` (not OpenCode `build`).

**Do not** add free-form `args`. **Do not** add `transport`.

Viper defaults in `internal/config/load.go`; validate ranges consistent with other providers.

### 4.3 Daemon registration

In `internal/daemon/daemon.go`, parallel to OpenCode:

```go
if cfg.Providers.Kilo.Enabled {
    kp := kilo.NewHTTPWithLogger(kilo.Config{ /* map fields */ }, log)
    reg.Register(kp)
    if cfg.Providers.Kilo.Prewarm {
        // EnsureServer / prewarm hook (same pattern as opencode)
    }
}
```

Shutdown path already iterates `reg.All()` — ensure `httpagent.Provider` Shutdown covers kilo.

### 4.4 Dialect spawn details (spike-locked)

| Item | Spec |
| --- | --- |
| `DefaultBin` | `kilo` |
| `ServeArgs(port)` | `serve --hostname 127.0.0.1 --port <port>` [+ `--pure`] |
| `HealthPath` | `/global/health` |
| `EventsPath` | `/global/event` |
| Child env | `KILO_SERVER_PASSWORD=<random>`, `KILO_SERVER_USERNAME=kilo` (or config), `MCREMOTE_ENGINE_*` |
| HTTP client | Basic Auth on every request including SSE — **requires extending `httpagent`**: as-built it sends no Authorization header on health poll (`provider.go:491`), SSE dial (`:625`), or REST calls (`:874`), and `Dialect` has no header hook. Add a request-decorator (e.g. optional `AuthHeader()` on the dialect or a `Config` field) covering all three paths before M1 |
| Cwd | `?directory=` (proven) and/or session create `directory` field |
| DecodeFrame | Copy OpenCode unwrap of GlobalEvent `payload` |
| Stderr | line ring prefix `kilo-stderr` |

### 4.5 Session operations (target mapping)

| mcremote | Expected engine op (OpenCode baseline) |
| --- | --- |
| Start | `POST /session` (+ model/agent if supported on create or first prompt) |
| Prompt | `POST /session/:id/prompt_async` with parts |
| Cancel | `POST /session/:id/abort` |
| Permission | `POST /session/:id/permissions/:permissionID` |
| Question | OpenCode question endpoint if present |
| Delete | `DELETE /session/:id` |
| Resume / replay | `GET /session/:id` + `GET /session/:id/message` |
| Model list | live `/provider` or `/config/providers` |
| Agent list | `GET /agent` |
| Commands | `GET /command` + static table |

### 4.6 SSE / event mapping

Reuse OpenCode decode strategy; pin differences in spike artifacts under `docs/kilo-spike-<version>/`:

- Raw SSE frames (anonymized)
- Event type histogram
- Diff vs OpenCode 1.18.x decoder switch

Tree demux (MADR 0020) enables only when child session events and parentID fields exist and idle-confirm REST works.

### 4.7 Slash commands (MADR 0023)

Build `command.Table` from:

1. Live `GET /command` if available.
2. Documented TUI slash set (`/models`, `/agents`, `/connect`, `/new`, …) for coverage matrix.
3. Only advertise commands the **HTTP API** can execute; pure-TUI commands stay unsupported with honest matrix entries.

### 4.8 Mobile

- Provider appears via `providers.list` when registered and ready.
- New-session dialog already provider-aware (Goose 0026 pattern) — verify label “kilo” is acceptable; optional display name later.
- Model field uses provider model catalog once `ModelLister` implemented.
- No dedicated mobile MADR required if parity is “same as opencode chips”; open follow-on only if agent/mode UX differs.

### 4.9 PATH / service install

- Document install: `npm i -g @kilocode/cli` (or brew).
- Service PATH: if systemd/launchd setup injects `~/.opencode/bin`, also consider common npm global bins and `~/.local/bin`. `kilo debug paths` on this host (2026-08-06): bin **`~/.cache/kilo/bin`** (self-managed updates land there), log `~/.local/share/kilo/log`, tmp under system temp; this host's active binary is `/opt/homebrew/bin/kilo`.
- `engines` CLI / doctor: list `kilo serve` owned processes like opencode/goose.

### 4.10 Live tests

```text
//go:build live_kilo
```

Minimum suite:

1. Health + version parse.
2. Create session → prompt_async → receive assistant chunk → abort/close.
3. Permission path (with always_approve true).
4. Resume/replay if API allows.
5. Catalog non-empty or static fallback.

Document known-good version in README prerequisites after spike.

### 4.11 Auth from phone (dependency on 0074)

| Phase | Kilo provider interaction (updated with 2026-08-06 live proof) |
| --- | --- |
| 0075 v1 | Host auth only |
| 0074 Phase 0 | Report auth_status from `GET /kilo/auth-status` (`{authenticated, type}`) + `GET /provider/auth` method catalog — **agent-native, no file probing** |
| 0074 Phase 1 | `set_credential` → `PUT /auth/{providerID}` `{type:"api", key}`; `clear_credential` → `DELETE /auth/{providerID}` — **both live-proven** (Appendix E) |
| 0074 Phase 2 | Engine-hosted code-mode OAuth: `POST /provider/{id}/oauth/authorize` → show `{url, instructions}` on phone → `POST …/callback {code}` — covers **Kilo Gateway device authorization** and **headless ChatGPT**; no CLI stdout parsing |
| 0074 Phase 3 | Only `"auto"`-mode authorizations (engine-local browser callback) need the loopback tunnel |

---

## 5. Alternatives considered

| Alternative | Verdict | Why |
| --- | --- | --- |
| **A. `kilo serve` + httpagent dialect** | **Chosen** | Matches OpenCode architecture, shared engine, feature-rich API |
| B. `kilo acp` + acpagent | Reject as primary | Per-session cost; 0019 lessons; HTTP is first-class for editors |
| C. Dual transport config | Reject | Operational complexity; 0019 removed this for OpenCode |
| D. `kilo run --format json` polling | Reject | No interactive permissions / weak multi-turn control plane |
| E. Only Gateway upstream on OpenCode | Incomplete | Does not deliver Kilo agent product (modes, skills, Kilo-native UX) |
| F. Adopt host `kilo daemon` | Reject | Ownership / password / version mismatch; mcremote must own engine |
| G. Thin wrapper reusing `opencode` package with bin override | Reject as long-term | Config paths, headers (`x-kilo-directory`), Basic user `kilo` vs `opencode`, SSE renames, branding — fork drift |

---

## 6. Risks and mitigations

| Risk | Likelihood | Impact | Mitigation |
| --- | --- | --- | --- |
| HTTP API renamed from OpenCode | Med | High | Milestone 0 OpenAPI dump + fixture tests; version pin |
| SSE event schema drift | Med | High | Frame capture; decoder unit tests from fixtures |
| Basic Auth username mismatch (`opencode` vs `kilo`) | High (known class) | Med | Force username `kilo`; never assume OpenCode defaults |
| Heavy cold start / RSS | High | Med | prewarm default true; document memory |
| Fork diverges monthly | High | Med | `live_kilo` in CI optional; README pin; doctor version |
| Product overlap confuses users | Med | Low | Docs: when to pick kilo vs opencode vs Gateway-on-opencode |
| Telemetry default on | Med | Low | Document `experimental.openTelemetry`; optional env to disable if stable |
| Session tree incomplete | Med | Med | Feature-flag `session_tree` false until proven |
| No binary on build hosts | — | — | ready=false; no register crash |

---

## 7. Implementation phases

### Milestone 0 — Live spike — **COMPLETE (2026-08-06)**

Artifacts: [docs/kilo-spike-7.4.20/](./kilo-spike-7.4.20/) (`summary.json`, SSE samples, OpenAPI path inventory, agents, messages).

| Gate | Result |
| --- | --- |
| proceed HTTP | **Yes** |
| pivot ACP | No (stdio ACP works but serve is complete enough) |
| abort | No |

Residual for implementation (not blocking M1): live **permission.asked** SSE fixture; **session tree** child events; optional Gateway authenticated turn with `kilo-auto/*` (**unblocked 2026-08-06** — host now holds a Gateway OAuth session, Appendix E).

### Milestone 1 — Provider skeleton

- `IDKilo`, config, daemon register, Ready(), static models, ServeArgs, health.
- **`httpagent` Basic Auth support** (health + SSE + REST request paths) — prerequisite for the password-gated spawn (D5, §4.4); keep it optional so the opencode dialect is unchanged.
- Unit tests with fake HTTP (httptest) where possible.
- No phone UX beyond providers.list.

### Milestone 2 — Session loop

- Create / prompt / stream map / abort / permission / delete.
- always_approve + permission timeout.
- live_kilo smoke green.

### Milestone 3 — Parity

- Catalog (models/agents/commands), stream coalesce, stall notice, resume/replay.
- Session tree if spike OK.
- Command table + docs/README prerequisites.
- Example config snippet in `configs/`.

### Milestone 4 — Ops + 0074 hooks

- engines list / doctor.
- Service PATH notes.
- Optional auth_status probe fields when 0074 lands.

---

## 8. Acceptance criteria

1. With `providers.kilo.enabled: true` and `kilo` on PATH, phone **New session** offers **kilo** and `ready: true`.
2. Phone can create a session, send a prompt, receive streamed assistant text, and complete a turn without SSH.
3. Permission requests either auto-approve (config) or round-trip to the phone and continue.
4. Cancel stops an in-flight turn.
5. Daemon restart does not leave orphan `kilo serve` processes owned by mcremote (reap / death signal).
6. OpenCode and Kilo can both be enabled; engines and auth stores remain isolated.
7. `go test ./...` green; `go test -tags live_kilo ./internal/provider/kilo/...` green on spike host.
8. README documents install, known-good version, and that credentials are host-side until 0074.

---

## 9. Open questions — resolved / remaining

| # | Question | Status |
| --- | --- | --- |
| 1 | Health JSON + version? | **Resolved:** `{"healthy":true,"version":"7.4.20"}` |
| 2 | SSE types vs OpenCode? | **Resolved:** same core types + GlobalEvent wrapper already handled by OpenCode decoder; plus Kilo `sync`, turn open/close, next.* switches |
| 3 | Default free model? | **Resolved (amended 2026-08-06):** engine default is auth-state-dependent — `kilo` → `kilo-auto/free` unauthenticated, `kilo-auto/balanced` with Gateway OAuth; successful live turn used openrouter `openrouter/free` with env key |
| 4 | Default agent? | **Resolved:** primary default **`code`** (no `build`) |
| 5 | `--pure`? | **Resolved:** yes on `serve` and `acp` |
| 6 | Directory header? | **Resolved:** `x-kilo-directory`, `x-opencode-directory`, `?directory=` all work |
| 7 | Session tree min version? | **Open** — routes exist; need child SSE fixture before enabling default `session_tree` |
| 8 | ACP transport? | **Resolved:** stdio initialize works; port flags optional |
| 9 | Host daemon collision? | **Open / ops** — document: mcremote owns its serve; operators should not rely on shared `kilo daemon` |
| 10 | Permission SSE shape? | **Open** — REST schema locked; ask-event fixture still needed |
| 11 | Model id alias rules? | **Partial** — `~vendor/model` under kilo provider; document from catalog not string concat |

---

## 10. Conclusion

Milestone 0 **proved** that Kilo CLI **7.4.20** is an OpenCode-class HTTP+SSE agent with **full capacity** for mcremote’s session product: shared serve, Basic Auth, prompt_async, SSE text streaming, turn lifecycle, catalogs, abort, and ACP as a secondary surface.

Implementation is **accepted**: new provider id `kilo`, `httpagent` dialect forked from OpenCode, known-good pin **7.4.20**, evidence under [docs/kilo-spike-7.4.20/](./kilo-spike-7.4.20/). Phone credential write paths remain MADR **0074**. Do not dual-stack transports; do not adopt host daemon or Kilo cloud remote.

---

## Appendix A — Code touch list (implementation checklist)

| Area | Files / actions |
| --- | --- |
| ID | `internal/provider/provider.go` — `IDKilo` |
| Transport | `internal/provider/httpagent/provider.go` — optional Basic Auth on health/SSE/REST paths (`:491`, `:625`, `:874`) |
| Dialect | `internal/provider/kilo/*` |
| Config | `internal/config/config.go`, `load.go`, tests |
| Daemon | `internal/daemon/daemon.go` register + prewarm |
| Commands | session command tables if provider-specific |
| Docs | README providers table, `docs/config.md`, this MADR status → accepted |
| Configs | example `providers.kilo` block |
| Mobile | verify picker label only (likely zero change) |
| Live | `//go:build live_kilo` |

## Appendix B — Primary URLs

- https://kilo.ai/docs/code-with-ai/platforms/cli
- https://kilo.ai/docs/code-with-ai/platforms/cli-reference
- https://kilo.ai/docs/contributing/architecture/cli-runtime
- https://kilo.ai/cli/opencode
- https://kilo.ai/docs/gateway
- https://kilo.ai/docs/gateway/authentication
- https://opencode.ai/docs/server/
- https://zed.dev/acp/agent/kilo
- https://github.com/Kilo-Org/kilocode

## Appendix C — Comparison snapshot (live vs as-built OpenCode)

| Dimension | OpenCode 1.18.11 (mcremote) | Kilo CLI **7.4.20** (spike) |
| --- | --- | --- |
| Binary | `opencode` | `kilo` |
| Serve | `opencode serve` | `kilo serve` (+ **`--pure`**) |
| Health | `/global/health` | same; version field present |
| SSE | `/global/event` GlobalEvent | **same envelope**; decoder reusable |
| Basic Auth user | `opencode` | **`kilo` only** (opencode user → 401) |
| Config home | `~/.config/opencode` | `~/.config/kilo` |
| Auth home | `~/.local/share/opencode/auth.json` | `~/.local/share/kilo/auth.json` |
| Default agent | `build` | **`code`** |
| Model body | provider/model object | same shape |
| Catalog size | multi-MB `/provider` | **179** providers, ~4.7 MB live |
| Transport package | `httpagent` | `httpagent` |
| Dialect package | `opencode` | `kilo` (new) |
| ACP | not used for sessions | stdio ACP viable; not primary |
| Product extras | Zen/Go | Gateway, Pass, cloud (ignored for mcremote) |

## Appendix D — Spike evidence index

| File | Contents |
| --- | --- |
| [kilo-spike-7.4.20/summary.json](./kilo-spike-7.4.20/summary.json) | Machine-readable probe summary |
| [kilo-spike-7.4.20/openapi-paths.txt](./kilo-spike-7.4.20/openapi-paths.txt) | 243 OpenAPI paths |
| [kilo-spike-7.4.20/sse-samples.json](./kilo-spike-7.4.20/sse-samples.json) | Sample SSE frames by type |
| [kilo-spike-7.4.20/messages-success.json](./kilo-spike-7.4.20/messages-success.json) | Successful PONG turn |
| [kilo-spike-7.4.20/agents-summary.json](./kilo-spike-7.4.20/agents-summary.json) | Agent list summary |
| [kilo-spike-7.4.20/provider-summary.json](./kilo-spike-7.4.20/provider-summary.json) | Slim provider catalog |

## Appendix E — Auth re-probe, 2026-08-06 (kilo 7.4.20, post-login host)

Context: after the spike, the operator ran Kilo Gateway login (OAuth) and added an OpenCode Go API key. A fresh password-gated `kilo serve` was started on loopback and probed; the host `auth.json` was byte-compared against a pre-probe backup after the write tests and was identical.

| # | Probe | Result |
| --- | --- | --- |
| 1 | `kilo --version` | `7.4.20` (unchanged pin) |
| 2 | `kilo auth list` | Credentials: **Kilo Gateway** (oauth), **OpenCode Go** (api); env: OpenRouter, Hugging Face |
| 3 | `auth.json` shape | `{kilo: {type: oauth, …}, opencode-go: {type: api, …}}`, mode 0600, data-dir path confirmed |
| 4 | Basic Auth | `kilo:<password>` → 200; no auth → **401**; `opencode:<password>` → **401** (spike behavior reproduced exactly) |
| 5 | `GET /kilo/auth-status` | `{"authenticated":true,"type":"oauth"}` |
| 6 | `GET /config/providers` | connected `["openrouter","huggingface","opencode-go","kilo"]`; defaults `kilo → kilo-auto/balanced`, `opencode-go → gpt-5.6-luna` |
| 7 | `PUT /auth/mcremote-probe-dummy` body `{"type":"api","key":"<dummy>"}` | → `true`; entry present in `auth.json` immediately after |
| 8 | `DELETE /auth/mcremote-probe-dummy` | → `true`; entry removed; `auth.json` byte-identical to pre-probe backup |
| 9 | `GET /provider/auth` | 13 providers with typed methods; `kilo` → “Kilo Gateway (Device Authorization)”; `openai` → browser + **headless** ChatGPT + API key; `github-copilot`, `gitlab`, `poe`, api-key providers |
| 10 | OpenAPI (`/doc`) | `Auth` = `OAuth \| ApiAuth \| WellKnownAuth`; `/auth/{providerID}` supports **PUT + DELETE**; `ProviderAuthAuthorization {url, method: auto\|code, instructions}` |

Facts 5–6 supersede the spike-day rows in §2.6's left column; both states are recorded because mcremote must handle hosts in either state.

A follow-up check later the same day found the credential state unchanged (same 2 entries) and re-reproduced the `kilo --help` subcommand surface, `kilo serve --help` flags, `kilo debug paths`, and the ACP stdio initialize response, alongside code-level verification of the §1.2 as-built claims (including the httpagent no-auth finding).

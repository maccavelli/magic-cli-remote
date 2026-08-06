# MADR 0074 — Remote Provider Authentication from Phone

<!-- markdownlint-disable MD013 MD024 MD033 MD036 -->

| field | value |
| --- | --- |
| status | **research expanded 2026-08-05; facts re-verified 2026-08-06** (proposed; implementation not started — see Appendix A confirmation) |
| date | 2026-08-06 |
| deciders | @saxsmith |
| related | MADR 0021 (OpenCode API), 0025 (Goose), 0028 (Codex), 0029 (platform), 0043 (models), **0073 (goose quota hang)**, **0075 (Kilo CLI provider — adds a fifth agent whose auth is deferred to this MADR)** |
| method | Live CLI probes on this host (goose 1.45.0, opencode 1.18.11, codex-cli 0.146.0, grok 0.2.118); official docs (opencode.ai/docs/providers, docs.x.ai enterprise + Grok auth guide, goose-docs.ai providers + subscription blog, ChatGPT Codex auth docs); binary string analysis of goose; local config inventory (`~/.config/goose`, `~/.local/share/opencode/auth.json`, `~/.grok/auth.json`); kilo 7.4.20 live spike (0075, `docs/kilo-spike-7.4.20/`); codebase verification at `8e2524d` (2026-08-06) |

## 0. Executive summary

**Problem.** mcremote can select providers/models from the phone, but **cannot configure credentials**. Headless hosts require SSH to run `goose configure`, `opencode auth login`, `codex login`, or `grok login`. That breaks the product promise and is the operational root of incidents like MADR 0073 (goose stuck on `opencode_go` weekly quota with no phone-side path to switch/auth another provider).

**What prior draft got wrong or incomplete.** Early 0074 text under-counted OAuth:

- **Goose** is not “Gemini OAuth + a couple of device flows.” Live binary + docs show a **rich OAuth matrix**: `gemini_oauth`, `chatgpt_codex`, `xai_oauth`, Hugging Face OAuth, OpenRouter login, Tetrate login, GitHub Copilot device code, Kimi Code device flow, plus keyring API keys for dozens of providers. `GOOSE_OAUTH_CALLBACK_PORT` is real.
- **Codex** is not “browser OAuth only.” **`codex login --device-auth`** and **`codex login --with-api-key`** (stdin) exist in 0.146.0 — ideal for phone/headless.
- **OpenCode** is not “interactive OAuth + plugins only.” Official `/connect` documents **ChatGPT Plus/Pro OAuth**, **GitHub Copilot device code**, **GitLab OAuth**, **DigitalOcean OAuth**, **Snowflake browser OAuth**, **xAI SuperGrok device-code OAuth**, and API keys for **OpenCode Go/Zen**, Hugging Face, OpenRouter, Anthropic (API key; Pro/Max path restricted), etc.
- **Grok** is the best-documented headless story: browser OIDC, **device code**, external auth provider, and `XAI_API_KEY`, with clear precedence and enterprise OIDC.
- **OpenAI Platform API** still has **no third-party OAuth for raw API keys**; **ChatGPT subscription OAuth via Codex/OpenCode/Goose** is a **different product path** and must not be conflated with `OPENAI_API_KEY`.

**Goal.** Phone can (1) paste API keys into host storage via the agent’s own auth channels, and (2) complete OAuth (device flow preferred; loopback tunnel only where required) without SSH.

**Scope update (2026-08-06).** MADR 0075 accepted **Kilo CLI** as a fifth session provider (spike complete on kilo 7.4.20; provider package not started) and explicitly defers its auth-from-phone work to this MADR. Kilo’s auth surface is inventoried in §4.5 — notably it is the only agent that exposes **server-side HTTP credential and OAuth endpoints** (`PUT /auth/{providerID}`, `/provider/{providerID}/oauth/authorize|callback`), which simplifies Strategies B and C for that agent.

---

## 1. Problem statement

magic-cli-remote manages four agent CLIs — **Grok**, **OpenCode**, **Codex**, **Goose** — with a fifth, **Kilo**, accepted in MADR 0075 (provider package not started). Each agent may authenticate to many **upstream** model providers. Credentials today live only on the host:

| store | path / mechanism (this host) |
| --- | --- |
| Goose | `~/.config/goose/config.yaml` + OS keyring / provider token files (e.g. `gemini_oauth/tokens.json`, `chatgpt_codex/tokens.json`, `xai_oauth/tokens.json`) |
| OpenCode | `~/.local/share/opencode/auth.json` (`type` + `key` per provider id; `type: api` on this host) + env (`OPENROUTER_API_KEY`, `HF_TOKEN`, …) |
| Codex | ChatGPT session auth (status: “Logged in using ChatGPT”) or API key via `codex login --with-api-key` |
| Grok | `~/.grok/auth.json` (OAuth session, mode 0600) or `XAI_API_KEY` |
| Kilo (0075, planned) | `~/.local/share/kilo/auth.json` (0600) + env; Kilo Gateway session; see §4.5 |

Phone protocol today: `ProviderInfoPayload` (`internal/protocol/messages.go`) is only `{id, ready}` — **no auth status, no methods, no set-credential, no OAuth orchestration**.

### 1.1 Session context (this workspace, 2026-08-05)

| finding | implication for 0074 |
| --- | --- |
| MADR 0073: goose hang on `opencode_go` weekly 429 | User needs phone-side switch to another configured provider **or** re-auth / new key without SSH |
| This host goose `active_provider: opencode_go` | Also has `gemini_oauth`, `chatgpt_codex`, `xai_oauth`, `google` configured — multi-auth is real, not theoretical |
| agenterr limit surfacing (stderr + goose file logs) | Auth *failures* can now show as quota/rate cards; still no *setup* path |
| OpenCode auth list: Zen + Go API keys + OpenRouter + HF env | Phone key injection maps cleanly onto `auth.json` / env |

---

## 2. Current mcremote architecture (auth-related)

### 2.1 Agent stack

| Provider ID | Transport | Package | Host auth today |
| --- | --- | --- | --- |
| `grok` | ACP stdio | `acpagent` | Host `~/.grok/auth.json` / `XAI_API_KEY`; optional static `auth_method_id` on ACP `Authenticate` |
| `opencode` | HTTP + SSE | `opencode` / `httpagent` | Host `~/.local/share/opencode/auth.json` + env |
| `codex` | app-server JSON-RPC | `codex` | Host ChatGPT login or API key |
| `goose` | ACP-over-HTTP | `acphttp` | Host goose config + keyring + OAuth token files |
| `kilo` (planned, 0075) | HTTP + SSE (shared `kilo serve`) | `kilo` dialect forked from `opencode` (not started) | Host `kilo auth` / Gateway / env |

### 2.2 Auth infrastructure already in tree

1. **Device auth** (`internal/auth`) — phone ↔ daemon pairing only (`paircode.go`, `token.go`, `store.go`). Unrelated to LLM credentials.
2. **ACP `auth_method_id`** (`providers.grok.auth_method_id`, `providers.goose.auth_method_id`; `internal/config/config.go` `AuthMethodID`) — if the agent advertises `authMethods` at `initialize`, daemon calls `Authenticate` with a **static** id from config (`acpagent/acpagent.go`, `acphttp/conn.go`). Not phone-controllable; not used for goose/chatgpt/gemini OAuth (those are outside ACP).
3. **Limit surfacing** (`internal/agenterr`) — `IsLimit` / `LooksLikeLongBackoff` classify provider backoff and quota text; `acpagent` aborts an in-flight turn on a stderr limit line, and `acphttp/engine_log_tail.go` tails goose’s on-disk logs for the same signals (goose does not write them to stderr). This surfaces auth/quota **failures**; it provides no setup path.
4. **Protocol** — no `provider.auth_*` or credential messages exist in `internal/protocol/messages.go` (see §8).

### 2.3 What “ready” means today

`ready` ≈ binary on `PATH` / engine can start (`internal/provider/registry.go`: “Ready probes (PATH lookups)”). A provider can be `ready: true` and still fail every turn with 401/429/quota. Phone cannot distinguish “not installed” from “needs login” from “quota exhausted.”

---

## 3. Research method & source table

| source | used for |
| --- | --- |
| Live CLI `--help` / `login` / `auth` on host | Ground truth for flags (`codex login --device-auth`, `grok login --device-auth`, `opencode auth login`) |
| [OpenCode Providers](https://opencode.ai/docs/providers/) (fetched 2026-08-05) | Per-provider auth methods for 75+ backends |
| [xAI Enterprise / Grok auth](https://docs.x.ai/build/enterprise) + Grok Build auth guide | Device code, OIDC, external provider, API key precedence |
| [goose providers.md](https://github.com/block/goose) + [subscription OAuth blog](https://goose-docs.ai/blog/2026/03/19/use-goose-with-your-ai-subscription/) | Provider table, ChatGPT/Gemini OAuth, ACP providers |
| goose 1.45.0 binary strings | OAuth provider IDs, `GOOSE_OAUTH_CALLBACK_PORT`, device_flow module, HF/xAI callback paths |
| [ChatGPT Codex auth docs](https://learn.chatgpt.com/docs/auth) | ChatGPT vs API key sign-in |
| Local config inventory | What a real multi-provider host looks like |

**Caveat.** CLI behavior drifts silently (MADR rule: pin with live tests). Every implementation claim in §9 must be re-probed against the pinned CLI versions in CI.

---

## 4. Agent CLI inventory (auth surface)

### 4.1 Grok Build (agent = mcremote `grok`)

**Versions probed:** `grok 0.2.118`.

| method | command / config | flow type | headless-friendly | storage |
| --- | --- | --- | --- | --- |
| Browser OIDC (default) | `grok login` / `grok login --oauth` | Loopback / browser to `auth.x.ai` | No (needs local browser) | `~/.grok/auth.json` (0600) |
| **Device code** | `grok login --device-auth` (`--device-code`) | **RFC 8628** — URL + user code | **Yes** | same |
| Enterprise OIDC | `[auth.oidc]` / `GROK_OIDC_*` | PKCE loopback to customer IdP | Partial (loopback on host) | same |
| External auth provider | `auth_provider_command` / `GROK_AUTH_PROVIDER_COMMAND` | stdout token / JSON | **Yes** (scriptable) | same |
| API key | `XAI_API_KEY` or per-model `api_key` | static secret | **Yes** | env / config.toml |

**Precedence (official):** per-model `api_key`/`env_key` → active session token → `XAI_API_KEY`.

**mcremote fit:** Device flow is **P0-grade** — parse URL/code from CLI stdout, send to phone. API key is **P0** via env or writing `~/.grok/config.toml` / daemon env. External auth provider can wrap phone-injected tokens later.

### 4.2 OpenAI Codex CLI (agent = mcremote `codex`)

**Versions probed:** `codex-cli 0.146.0`.

| method | command | flow type | headless-friendly | notes |
| --- | --- | --- | --- | --- |
| ChatGPT OAuth (browser) | `codex login` (default) | Browser / localhost callback | No | This host: “Logged in using ChatGPT” |
| **ChatGPT device auth** | `codex login --device-auth` | Device flow (flag present; help text sparse) | **Yes (expected)** | Must live-pin output format |
| **API key via stdin** | `printenv OPENAI_API_KEY \| codex login --with-api-key` | Static secret | **Yes** | Perfect for phone paste |
| Access token via stdin | `codex login --with-access-token` | token inject | **Yes** | Advanced / CI |
| Logout | `codex logout` | clear store | Yes | Phone “disconnect” |

**Important distinction:** ChatGPT subscription auth ≠ OpenAI Platform `OPENAI_API_KEY`. Features may differ (docs: some Codex capabilities require ChatGPT sign-in).

**mcremote fit:** `--with-api-key` is the cleanest **phone API key** path of any agent. `--device-auth` is the cleanest **ChatGPT OAuth** path if stdout is parseable (live probe required before implementing).

### 4.3 OpenCode (agent = mcremote `opencode`)

**Versions probed:** `1.18.11`. Credentials: `opencode auth login` / `/connect`; store `~/.local/share/opencode/auth.json`.

#### 4.3.1 First-party / product plans

| product | auth method | type | notes |
| --- | --- | --- | --- |
| **OpenCode Zen** | API key from opencode.ai/auth (GitHub/Google login on web) | **API key** (web account is OAuth, product key is opaque) | Paste key into `/connect` |
| **OpenCode Go** | Same pattern — subscribe, copy API key, paste | **API key only** in CLI | **No** device OAuth in TUI; this is the path that hit weekly quota in 0073 |
| OpenAuth web | Continue with GitHub / Google at opencode.ai/auth | Browser OAuth **to mint keys**, not for inference | Phone can open browser to mint, then paste key |

#### 4.3.2 Official `/connect` OAuth / device flows (OpenCode docs)

| upstream | auth method in OpenCode | flow | phone strategy |
| --- | --- | --- | --- |
| **OpenAI / ChatGPT** | ChatGPT Plus/Pro **or** manual API key | Browser OAuth vs API key | Device/tunnel for OAuth; key paste for API |
| **GitHub Copilot** | Device code at `github.com/login/device` | **RFC 8628-style** | **P1** — native phone display |
| **GitLab Duo** | OAuth (recommended) or PAT | Loopback OAuth or token | PAT = key paste; OAuth = tunnel/shim |
| **DigitalOcean** | OAuth (recommended) or model access key | Browser OAuth | Tunnel or open URI |
| **Snowflake Cortex** | Browser OAuth or PAT/JWT | Loopback OAuth | Tunnel or paste |
| **xAI** | SuperGrok **device-code OAuth** or API key | **Device code** + API key | **P1** device flow |
| Anthropic | Manual API key; Pro/Max option noted with **ToS warning** (plugins prohibited; official path restricted) | Prefer **API key** | Phone paste only |
| Hugging Face | API token (fine-grained inference write) | **API key** | Phone paste (`HF_TOKEN` / `/connect`) |
| OpenRouter | API key | **API key** | Phone paste |
| Azure OpenAI / Cognitive | API key + resource name env | API key + config | Phone paste + fields |
| Amazon Bedrock | AWS keys / profile / bearer token | cloud IAM | Advanced; env inject |
| Google Vertex | ADC / service account | GCP | Advanced |
| Local (Ollama, LM Studio, llama.cpp, Atomic Chat) | none / baseURL | no secret | config only |

Most other directory providers (Groq, DeepSeek, Fireworks, Together, Moonshot, MiniMax, NVIDIA, Cerebras, …) are **API key via `/connect`**.

#### 4.3.3 Storage shape (this host)

```text
~/.local/share/opencode/auth.json
  opencode     → { type, key }   # Zen
  opencode-go  → { type, key }   # Go (quota surface in 0073)
env:
  OPENROUTER_API_KEY, HF_TOKEN
```

**mcremote fit:** Writing `auth.json` entries or running a non-interactive connect is P0 for keys. OAuth providers that already print device codes (Copilot, xAI SuperGrok) map to Strategy A. Loopback OAuth needs Strategy B.

### 4.4 Goose (agent = mcremote `goose`)

**Versions probed:** `1.45.0`. Configure: `goose configure`. Active on this host: `opencode_go`.

#### 4.4.1 Subscription / OAuth providers (first-class)

Documented and/or present in binary / local config:

| goose provider id | user-facing | auth | flow | notes |
| --- | --- | --- | --- | --- |
| `gemini_oauth` | Gemini (Google account) | OAuth | Browser loopback | Official subscription blog; tokens under `gemini_oauth/` |
| `chatgpt_codex` | ChatGPT Plus/Pro | OAuth | Browser | “Nothing — OAuth sign-in”; Codex models |
| `xai_oauth` | xAI / Grok | OAuth | Browser loopback (`xai_oauth/tokens.json`) | Configured on this host |
| `huggingface` | Hugging Face | OAuth **and/or** token | Callback path in binary (`/oauth_callback`) | Docs also list HF as token-capable |
| OpenRouter (configure UX) | OpenRouter | Login recommended | Browser OAuth during setup | “OpenRouter Login (Recommended)” in configure strings |
| Tetrate Agent Router | Tetrate | Login | Browser OAuth | Onboarding option with free credits promo |
| `github_copilot` | GitHub Copilot | Device code | **RFC 8628** (`oauth_device_flow` module) | Binary confirms device grant |
| `kimi_code` | Kimi Code | Device flow | Device | Binary lists `kimi_code` with device_flow |
| `claude-acp` | Claude Code via ACP | External CLI auth | Requires `@…/claude-agent-acp` + Claude subscription | Not pure API key |
| `codex-acp` | Codex via ACP | External | Requires codex-acp + ChatGPT/API | Pass-through agent |

#### 4.4.2 API-key / cloud providers (documented table excerpt)

| provider | typical credentials |
| --- | --- |
| Anthropic | `ANTHROPIC_API_KEY` |
| OpenAI (classic) | `OPENAI_API_KEY` |
| Google (API key) | `GOOGLE_API_KEY` / AI Studio key (distinct from `gemini_oauth`) |
| xAI (API key) | `XAI_API_KEY` (distinct from `xai_oauth`) |
| OpenRouter | `OPENROUTER_API_KEY` |
| Groq, DeepSeek, Fireworks, Together, Cerebras, … | `*_API_KEY` |
| Amazon Bedrock | AWS env / bearer token |
| Azure OpenAI | endpoint + key or Entra token |
| Databricks | host + token |
| Ollama / local | host only |

Secrets: OS keyring (macOS Keychain, Secret Service) or file-backed keyring (configure option).

#### 4.4.3 Goose OAuth implementation knobs (binary)

| env / path | role |
| --- | --- |
| `GOOSE_OAUTH_CALLBACK_PORT` | Force loopback bind port for OAuth callback server |
| `GOOSE_OAUTH_CALLBACK_TIMEOUT_SECONDS` | Callback wait timeout |
| Client metadata | `https://goose-docs.ai/oauth/client-metadata.json` |
| Device flow | `urn:ietf:params:oauth:grant-type:device_code` in binary |

**mcremote fit:**

- API keys → write keyring / env / invoke configure non-interactively if available.
- Device-code providers (Copilot, Kimi) → Strategy A.
- Loopback OAuth (`gemini_oauth`, `xai_oauth`, ChatGPT, OpenRouter, HF) → Strategy B with **fixed port** via `GOOSE_OAUTH_CALLBACK_PORT` (best loopback target of any agent).

### 4.5 Kilo CLI (agent = mcremote `kilo`, planned — MADR 0075)

**Versions probed:** `kilo 7.4.20` (live spike 2026-08-06; artifacts in [docs/kilo-spike-7.4.20/](./kilo-spike-7.4.20/)). Provider package not started; auth-from-phone for kilo is explicitly deferred by 0075 to this MADR.

| method | command / endpoint | flow type | headless-friendly | notes |
| --- | --- | --- | --- | --- |
| Provider API keys | `kilo auth list\|login\|logout` (same UX family as OpenCode), TUI `/connect` | API key paste | Partial (TUI) | Store `~/.local/share/kilo/auth.json` (0600); this host 2026-08-06: `kilo` (**oauth**, Gateway session) + `opencode-go` (**api**); env `OPENROUTER_API_KEY` + `HF_TOKEN` picked up |
| **Server-side credential write** | `PUT /auth/{providerID}` body `{type:"api", key}` (also `oauth` / `wellknown` variants); `DELETE /auth/{providerID}` to clear | HTTP API | **Yes — live-proven 2026-08-06** | Unique among the five agents: daemon already owns the serve engine, so Strategy C needs **no file poking and no CLI spawn**; gated only by serve Basic Auth |
| Auth status probe | `GET /kilo/auth-status` → `{authenticated, type}`; `GET /provider/auth` → per-upstream typed method catalog | HTTP API | **Yes** | Live: `{"authenticated":true,"type":"oauth"}` after Gateway login — native Phase 0 status source |
| **Engine-hosted OAuth** | `POST /provider/{id}/oauth/authorize` `{method, inputs?}` → `{url, method: "auto"\|"code", instructions}`; `POST …/oauth/callback {method, code}` | Device-style code paste (`"code"`) or engine-local browser callback (`"auto"`) | **Yes for `"code"` mode** | Live catalog includes **Kilo Gateway (Device Authorization)** and **ChatGPT Pro/Plus (headless)** — Strategy A shaped, no tunnel; only `"auto"` mode would need Strategy B |
| Kilo account / Gateway | ACP authMethod `kilo-login`; Gateway OAuth or key | Browser login / device / key | Partial | Gateway is an upstream inference path (this MADR), not the session transport (0075) |

**mcremote fit:** Best-in-class target for **both** Strategy C and Strategy A — `PUT`/`DELETE /auth/{providerID}` give an authoritative, agent-native credential API over HTTP the daemon already speaks (round-trip live-proven, MADR 0075 Appendix E), `GET /kilo/auth-status` + `GET /provider/auth` are the only native auth-status/method-catalog endpoints any agent offers (Phase 0), and code-mode `authorize`/`callback` runs device-style OAuth entirely engine-side with no CLI stdout parsing. Remaining probe: drive one code-mode flow end-to-end (§12 Q7).

---

## 5. Upstream provider matrix (truth table)

Two layers must stay separate: **(1) what the model vendor allows**, **(2) what each agent CLI implements**.

### 5.1 Model vendor / platform (capability, not agent-specific)

| platform | static API key | OAuth for CLI / agent use | device authorization | notes |
| --- | --- | --- | --- | --- |
| **xAI / Grok** | ✅ `XAI_API_KEY` | ✅ SuperGrok / Grok Build OIDC | ✅ device code | Best documented dual path |
| **Google Gemini (consumer)** | ✅ AI Studio key | ✅ via Goose `gemini_oauth` / community plugins | Varies | Consumer OAuth ≠ Vertex ADC |
| **Google Vertex / GCP** | service account / ADC | ✅ ADC / `gcloud` | device via gcloud | Cloud IAM, not Gemini consumer |
| **OpenAI Platform API** | ✅ `OPENAI_API_KEY` | ❌ no third-party API OAuth | ❌ | Pay-as-you-go API only |
| **ChatGPT (subscription)** | ❌ (not an API key product) | ✅ via Codex / OpenCode / Goose ChatGPT paths | ✅ Codex `--device-auth` | Subscription-bound |
| **Anthropic API** | ✅ | ❌ official third-party OAuth discouraged/prohibited for Pro/Max plugins | ❌ | Prefer API keys in agents |
| **GitHub Copilot** | ❌ | ✅ OAuth/device | ✅ `github.com/login/device` | OpenCode + Goose |
| **Hugging Face** | ✅ fine-grained token | ✅ HF OAuth in some clients (goose callback) | ✅ HF hub device/browser | Inference Providers permission |
| **OpenRouter** | ✅ | ✅ login to mint keys (PKCE/loopback in ecosystem) | ❌ typical | Key still ends up in agent store |
| **Azure OpenAI** | ✅ keys | ✅ Entra ID | ✅ device code via Azure CLI | Agent may only expose key fields |
| **AWS Bedrock** | IAM / bearer | federation / IRSA | via AWS tools | Not consumer OAuth |
| **GitLab Duo** | PAT | ✅ OAuth | loopback | OpenCode |
| **DigitalOcean Inference** | model access key | ✅ OAuth | browser | OpenCode |
| **Snowflake Cortex** | PAT/JWT | ✅ browser OAuth | loopback | OpenCode |

### 5.2 Agent × upstream OAuth/API matrix (implementation surface for mcremote)

Legend: **D** = device flow, **L** = loopback browser OAuth, **K** = API key/token paste, **E** = external/env/cloud IAM, **—** = not applicable / not exposed.

| upstream ↓ / agent → | Grok CLI | Codex CLI | OpenCode | Goose |
| --- | --- | --- | --- | --- |
| xAI SuperGrok / Grok Build | **D, L, K, E** | — | **D, K** | **L** (`xai_oauth`), **K** (`XAI_API_KEY`) |
| ChatGPT subscription | — | **L, D, K\*** | **L, K** | **L** (`chatgpt_codex`) |
| OpenAI Platform API | — | **K** | **K** | **K** |
| Gemini consumer OAuth | — | — | plugin/community | **L** (`gemini_oauth`) |
| Gemini / Google API key | — | — | **K** | **K** (`google` / `GOOGLE_API_KEY`) |
| Anthropic API | — | — | **K** (Pro/Max restricted) | **K** |
| GitHub Copilot | — | — | **D** | **D** |
| Hugging Face | — | — | **K** | **L/K** |
| OpenRouter | — | — | **K** | **L/K** |
| OpenCode Go / Zen | — | — | **K** | **K** (`opencode_go` provider) |
| Azure / Bedrock / Vertex | — | limited | **K/E** | **K/E** |

\*Codex API key is Platform billing, not ChatGPT subscription.

Kilo (0075, planned) is omitted from the matrix pending its provider package: expected surface is **K** for upstream API keys (via `kilo auth` / `PUT /auth/{providerID}`) plus engine-hosted **L** endpoints (`/provider/{providerID}/oauth/*`) — see §4.5.

---

## 6. Reassessed solution strategies

### Strategy A — Device Authorization Grant (RFC 8628) — **preferred for OAuth**

**Agents/providers with confirmed or strong support:**

| agent | flow | implementation sketch |
| --- | --- | --- |
| Grok | `grok login --device-auth` | Spawn, parse URL + user_code from stdout/stderr, `oauth.device_flow` to phone |
| Codex | `codex login --device-auth` | Same; **live-pin** message format (help text empty) |
| OpenCode | GitHub Copilot connect; xAI SuperGrok device | Drive `/connect` or underlying auth module; parse code |
| Goose | Copilot, Kimi device modules | `goose configure` non-interactive path or provider-specific entry |

**Phone UX:** show user code + “Open verification URL”; poll is host-side until success/fail.

**Pros:** No reverse tunnel; works over pure WS control plane.  
**Cons:** Only where vendor supports device grant.

### Strategy B — Loopback OAuth + reverse callback tunnel — **required for some Goose/OpenCode**

**Targets:** Goose `gemini_oauth`, `xai_oauth`, ChatGPT, OpenRouter, HF OAuth; OpenCode ChatGPT/GitLab/Snowflake/DigitalOcean browser flows; Grok default browser login if device flow not used.

**Goose-specific win:** set `GOOSE_OAUTH_CALLBACK_PORT=<fixed>` so the tunnel target is known without parsing.

**General steps:**

1. Daemon starts OAuth (configure / login / BROWSER shim).
2. Phone opens authorization URL (possibly rewritten callback).
3. Phone local HTTP listener accepts redirect; tunnels request body/headers to host `127.0.0.1:port/callback`.
4. CLI completes token exchange; tokens land in host store.

**Pros:** Covers real loopback CLIs.  
**Cons:** Highest complexity (WS reverse HTTP, single-use tunnels, CSRF state, timeouts).

### Strategy C — Phone API key / token injection — **P0 for nearly all agents**

| agent | preferred write path |
| --- | --- |
| **Codex** | `echo -n "$KEY" \| codex login --with-api-key` |
| **OpenCode** | Write `~/.local/share/opencode/auth.json` entry `{type,key}` for provider id, **or** invoke documented connect if non-interactive API exists; env for `OPENROUTER_API_KEY` / `HF_TOKEN` |
| **Goose** | Keyring / provider secret store / env vars documented in providers.md; set `active_provider` when switching |
| **Grok** | Set `XAI_API_KEY` in daemon service env **or** per-model `api_key` in `~/.grok/config.toml` |
| **Kilo** (once 0075 lands) | `PUT /auth/{providerID}` on the daemon-owned `kilo serve` engine (fallback: `auth.json` write / env) |

**Phone UX:** secure text field, write-only, clear after send; never persist key in phone secure storage long-term.

**OpenCode Go / Zen:** Web OAuth at opencode.ai/auth only **mints** a key; phone flow = open browser to mint → paste key (hybrid of C + open-URL).

### Strategy D — Auth status + multi-provider switch (product, not OAuth)

Independent of OAuth: expose **which** upstream is active and **whether** credentials exist. For goose, phone can switch `active_provider` among already-configured OAuth providers (this host already has four) — mitigates 0073 without new login.

### Strategy E — External auth provider (Grok) / CI tokens

Advanced: phone or MDM supplies a short-lived token; host runs `auth_provider_command`. Deferred unless enterprise demand.

---

## 7. Feasibility reassessment (post-research)

| feature | feasibility | complexity | coverage | priority | change vs prior draft |
| --- | --- | --- | --- | --- | --- |
| **API key entry (all agents)** | ✅ Full | Low–Med | OpenCode Go/Zen, HF, OpenRouter, Anthropic, OpenAI Platform, Grok key, Goose keys, Codex `--with-api-key` | **P0** | Codex stdin is better than generic env dump |
| **Auth status + active provider list** | ✅ Full | Low | All | **P0** | New — required for UX |
| **Switch goose active_provider** | ✅ Full | Low | Goose multi-config | **P0** | New — 0073 mitigation |
| **Kilo key injection + auth status** (blocked on 0075 provider) | ✅ Full | Low | Kilo upstream keys | **P0 once 0075 lands** | New — `PUT /auth/{providerID}` + `GET /kilo/auth-status` are agent-native HTTP |
| **Device flow: Grok** | ✅ Full | Med | xAI OAuth | **P1** | Confirmed docs |
| **Device flow: Codex ChatGPT** | ✅ Likely | Med | ChatGPT | **P1** | **Was missing** — flag exists |
| **Device flow: OpenCode Copilot / xAI** | ✅ Full | Med–High | Copilot, SuperGrok | **P1** | Under-specified before |
| **Device flow: Goose Copilot / Kimi** | ✅ Full | Med–High | Copilot, Kimi | **P1** | Under-specified before |
| **Loopback tunnel: Goose** | ✅ Full | High | Gemini, xAI, ChatGPT, OpenRouter, HF OAuth | **P1** | `GOOSE_OAUTH_CALLBACK_PORT` confirmed |
| **Loopback tunnel: OpenCode browser OAuth** | ⚠️ Partial | Very high | ChatGPT, GitLab, DO, Snowflake | **P2** | BROWSER shim |
| **Loopback tunnel: Grok browser** | ⚠️ Optional | High | Prefer device flow | **P2** | Prefer Strategy A |
| **Anthropic Pro/Max OAuth** | ❌ Avoid | — | ToS risk | **Won’t do** | API key only |
| **OpenAI Platform OAuth** | ❌ N/A | — | — | **N/A** | Keys only |

---

## 8. Protocol & phone UX (refined)

### 8.1 Messages (proposed)

```text
providers.list                 → include auth_status, auth_methods[], active_upstream?
provider.auth_status           → server push on change
provider.set_credential        → client: { provider_id, upstream_id?, kind: api_key|token, secret }
provider.clear_credential      → client
provider.start_auth            → client: { provider_id, method_id }  # oauth_device | oauth_loopback | …
oauth.device_flow              → server: { verification_uri, user_code, expires_in?, interval? }
oauth.device_flow_result       → server: { ok | error }
oauth.open_browser             → server: { url }  # phone opens system browser
oauth.loopback_tunnel_start    → server: { tunnel_id, listen_hint?, rewrite_url }
oauth.loopback_tunnel_http     → bidirectional HTTP fragments
provider.set_active_upstream   → goose active_provider / opencode default model provider
```

### 8.2 Extended ProviderInfo (sketch)

```json
{
  "id": "goose",
  "ready": true,
  "auth_status": "configured",
  "auth_detail": "opencode_go · weekly quota may apply",
  "active_upstream": "opencode_go",
  "upstreams": [
    {
      "id": "opencode_go",
      "label": "OpenCode Go",
      "auth_status": "configured",
      "methods": [{ "type": "api_key", "label": "OpenCode Go API key" }]
    },
    {
      "id": "gemini_oauth",
      "label": "Gemini (Google OAuth)",
      "auth_status": "configured",
      "methods": [{ "type": "oauth_loopback", "label": "Sign in with Google" }]
    },
    {
      "id": "chatgpt_codex",
      "label": "ChatGPT Codex",
      "auth_status": "configured",
      "methods": [{ "type": "oauth_loopback", "label": "Sign in with ChatGPT" }]
    },
    {
      "id": "xai_oauth",
      "label": "xAI OAuth",
      "auth_status": "configured",
      "methods": [{ "type": "oauth_loopback", "label": "Sign in with xAI" }]
    },
    {
      "id": "github_copilot",
      "label": "GitHub Copilot",
      "auth_status": "missing",
      "methods": [{ "type": "oauth_device", "label": "Device code" }]
    }
  ]
}
```

### 8.3 Phone UI

1. **Providers** — chip: configured / needs setup / error / quota (reuse agenterr `error_kind` from live sessions).
2. **Setup sheet**
   - **API key** field (paste) → `provider.set_credential`.
   - **Open browser to mint key** (Zen/Go) → system browser → paste.
   - **Device code** → large code + open URI.
   - **OAuth loopback** → “Continue” opens browser; spinner until tunnel completes.
3. **Switch active model provider** (goose) without re-login when multiple upstreams configured.
4. **Never** store secrets on phone beyond the send buffer.

---

## 9. Security

| topic | decision |
| --- | --- |
| Transit | Existing mTLS + device token WS (MADR 0015/0068 lineage) |
| Host write | 0600 files; prefer agent-native stores (`auth.json`, keyring, `codex login`) over inventing a parallel vault |
| Phone | Write-only secrets; no long-term key cache |
| Loopback tunnel | Single-use, short TTL, path allowlist (`/callback`, `/oauth_callback`), no arbitrary host ports |
| Logging | Never log secret values; redact auth.json keys in doctor output |
| Anthropic / ToS | Do not implement unofficial Pro/Max OAuth plugins |
| Multi-tenant hosts | Credentials are per-OS-user; document that phone auth is to **that** user’s agent stores |

---

## 10. Implementation phases (revised)

### Phase 0 — Discovery & protocol (small)

- Extend `providers.list` with `auth_status` / methods (best-effort probes: file presence, `codex login status`, `opencode auth list`, goose config).
- Phone UI chips only (no writes yet).
- Live-tagged probes for stdout formats of `grok login --device-auth`, `codex login --device-auth`.

### Phase 1 — API keys from phone (P0)

- `provider.set_credential` / clear.
- Codex `--with-api-key`; OpenCode `auth.json` + env; Grok `XAI_API_KEY`/config; Goose keyring/env + `active_provider` switch.
- Kilo (if 0075 provider has landed): `PUT /auth/{providerID}`; status chip from `GET /kilo/auth-status`.
- Acceptance: cold host, phone pastes OpenCode Go key, session prompt works without SSH.

### Phase 2 — Device OAuth (P1)

- Grok device auth end-to-end.
- Codex device auth (if format stable).
- OpenCode Copilot + xAI SuperGrok device.
- Goose Copilot/Kimi if exposeable without full TUI.

### Phase 3 — Loopback tunnel (P1–P2)

- Goose first (`GOOSE_OAUTH_CALLBACK_PORT` + gemini/xai/chatgpt).
- OpenCode browser OAuth second.

### Phase 4 — Polish

- Auth error classification (401/invalid_key) via agenterr.
- Doctor integration; revoke/logout from phone.

---

## 11. Acceptance criteria

1. Phone shows per-agent **and** per-upstream auth status for grok/opencode/codex/goose.
2. Phone can inject an API key for OpenCode Go and Codex without SSH; host stores via native mechanism.
3. Phone can complete **at least one** device OAuth (Grok) end-to-end on a headless host.
4. Phone can switch goose away from a quota-exhausted upstream to another configured OAuth provider without SSH (0073 operational fix).
5. No secrets appear in mcremote logs at info level; tunnel cannot be reused after success.
6. Live tests pin CLI auth flag behavior for the versions in README.

---

## 12. Open questions

1. Does `codex login --device-auth` print a stable, parseable `user_code` + URL on all platforms? (**must probe**)
2. Can OpenCode `/connect` be driven non-interactively for a named provider + key? (file write may be enough)
3. Goose: is there a supported non-interactive `goose configure` / secret set API, or only keyring CLI + config edit?
4. Should mcremote **restart** engines after credential change (yes by default for opencode/goose shared engines)?
5. Multi-device: last writer wins for host credentials — confirm product expectation.
6. iOS: local HTTP listener for loopback tunnel vs ASWebAuthenticationSession — platform choice (MADR 0067).
7. Kilo: ~~do `PUT /auth/{providerID}` and the OAuth endpoints work, and what body schema does the auth write take?~~ **Resolved 2026-08-06** (live probe, MADR 0075 Appendix E): `PUT /auth/{providerID}` with `{type:"api", key}` returns `true` and writes `auth.json`; `DELETE /auth/{providerID}` clears it; both sit behind the serve Basic Auth only. `GET /provider/auth` enumerates typed auth methods per upstream (13 live, incl. **Kilo Gateway device authorization** and **headless ChatGPT**), and `POST /provider/{id}/oauth/authorize` → `{url, method: "auto"|"code", instructions}` + `POST …/oauth/callback {code}` complete code-mode OAuth engine-side — Strategy A shaped, no tunnel. Remaining sub-question: live-drive one full code-mode flow end-to-end, and determine which providers return `"auto"` (tunnel-requiring) vs `"code"`.

---

## 13. Conclusion

Remote provider auth from the phone is **not** blocked by a lack of OAuth in the ecosystem. The opposite is true: **Grok, Codex, OpenCode, and Goose all implement multiple OAuth or device flows**, and nearly every commercial model path accepts **API keys**. The gap is entirely in mcremote’s control plane.

Prior research under-weighted **Goose’s OAuth surface** (`gemini_oauth`, `chatgpt_codex`, `xai_oauth`, HF, OpenRouter, Copilot device) and **Codex/OpenCode device/key stdin paths**. With those corrected, the recommended order is:

1. **API key injection + auth status + goose upstream switch (P0)**  
2. **Device-code OAuth for Grok/Codex/Copilot/xAI (P1)**  
3. **Loopback reverse tunnel, Goose-first via `GOOSE_OAUTH_CALLBACK_PORT` (P1/P2)**

That combination covers headless setup, subscription OAuth, and the operational failure mode of MADR 0073 without requiring SSH.

When the 0075 kilo provider lands, kilo slots into Phase 0/1 almost for free: its serve engine natively exposes auth status (`GET /kilo/auth-status`) and credential writes (`PUT /auth/{providerID}`) over the HTTP transport the daemon already owns, pending the live probes in §12 Q7.

---

## Appendix A — This host snapshot (research evidence, 2026-08-05)

| agent | evidence |
| --- | --- |
| Goose 1.45.0 | `active_provider: opencode_go`; configured: `gemini_oauth`, `google`, `chatgpt_codex`, `opencode_go`, `xai_oauth` |
| OpenCode 1.18.11 | `auth.json`: `opencode`, `opencode-go` (both `type: api`); env: `OPENROUTER_API_KEY`, `HF_TOKEN`; `opencode auth list\|login\|logout` |
| Codex 0.146.0 | `codex login status` → Logged in using ChatGPT; flags: `--device-auth` (present, **empty help text**), `--with-api-key`, `--with-access-token` |
| Grok 0.2.118 | `~/.grok/auth.json` present (0600); `login --device-auth` (alias `--device-code`) / `--oauth` in `--help` |
| Kilo 7.4.20 | On PATH; `kilo auth list` → Kilo Gateway (oauth) + OpenCode Go (api) as of 2026-08-06; live serve probe: `GET /kilo/auth-status` → `{"authenticated":true,"type":"oauth"}`, `PUT`/`DELETE /auth/{providerID}` round-trip proven, `GET /provider/auth` → 13 upstreams with typed methods (details: MADR 0075 §2.6 + Appendix E) |

### Verification re-run, 2026-08-06

Every host fact above was re-probed on 2026-08-06 and reproduced exactly (versions, flags, stores, goose provider set). Codebase claims were verified against the tree at `8e2524d`: `ProviderInfoPayload` is still `{id, ready}` (`internal/protocol/messages.go`); no `provider.auth_*`, `set_credential`, or `oauth.*` message exists anywhere in Go code; `internal/auth` remains pairing-only; static ACP `auth_method_id` remains the sole in-tree auth hook. Note: the commit messages of `11902fe` and `8e2524d` describe 0074 protocol messages as implemented — they are **not**; those commits landed this research document, the 0073 limit-surfacing work (`agenterr`, `acphttp/engine_log_tail.go`), and the 0075 kilo spike artifacts. Status remains **proposed / implementation not started**.

## Appendix B — Primary URLs

- https://opencode.ai/docs/providers/
- https://opencode.ai/docs/go/
- https://opencode.ai/auth
- https://docs.x.ai/build/enterprise
- https://github.com/xai-org/grok-build (auth user guide)
- https://goose-docs.ai/blog/2026/03/19/use-goose-with-your-ai-subscription/
- https://github.com/block/goose (documentation/docs/getting-started/providers.md)
- https://learn.chatgpt.com/docs/auth
- https://huggingface.co/docs/inference-providers/en/integrations/opencode

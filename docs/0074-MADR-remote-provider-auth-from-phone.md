# MADR 0074 — Remote Provider Authentication from Phone

<!-- markdownlint-disable MD013 MD024 MD033 MD036 -->

| field | value |
| --- | --- |
| status | **accepted** 2026-08-10 — decisions D1–D15 locked; all §12 questions resolved (Q1–Q3, Q7 by live probe 2026-08-10; Q4–Q6 decided). Supersedes the *research-report* form of this document (2026-08-05/06), which proposed strategies but locked nothing. Implementation not started. |
| date | 2026-08-10 |
| deciders | @saxsmith |
| related | MADR 0015 (relay), **0019** (single-engine, engine-owned config), 0021 (OpenCode API), 0025 (Goose), 0028 (Codex), 0029 (platform), 0043 (models), **0044** (permission funnel), **0067** (iOS port), **0068** (protocol v2 capabilities), **0073** (goose quota hang), **0075** (Kilo provider — **now implemented and default-enabled**, auth deferred here) |
| method | Live CLI probes on this host (grok **1.0.0**, opencode 1.18.11, codex-cli 0.146.0, goose 1.45.0, kilo 7.4.20); **live HTTP probes against the daemon-owned `kilo serve` engine** (auth catalog, auth-status, four OAuth authorize flows); official docs (opencode.ai/docs/providers, docs.x.ai, goose-docs.ai, ChatGPT Codex auth); local config inventory; codebase verification at `e4269ef` (2026-08-10) |
| supersedes | The 2026-08-05/06 research revision of this file (git history: `287b680`, `01934c2`) |

## 0. Executive summary

**Problem.** mcremote selects providers and models from the phone but **cannot configure credentials**. Headless hosts require SSH to run `goose configure`, `opencode auth login`, `codex login`, `grok login`, or `kilo auth login`. That breaks the product promise and is the operational root of MADR 0073 (goose wedged on an `opencode_go` weekly quota with no phone-side path to switch or re-auth).

**What this revision changes.** The prior text was thorough research that **decided nothing**: five strategies, a priority table, and a protocol "sketch". Plans cannot be built on it. This revision locks **D1–D15**, resolves every open question against live evidence, and partitions the work into four plan-sized workstreams (§11).

**Four findings from the 2026-08-10 probes materially change the design:**

1. **`codex login --device-auth` is destructive at flow start.** It deletes `~/.codex/auth.json` *before* the user completes (or abandons) the flow. Observed directly: a logged-in host went to `Not logged in` the moment the flow started, and stayed logged out after cancellation. Any phone-triggered codex device flow must therefore be an explicit, warned, snapshot-protected action (**D8**).
2. **The OpenCode/Kilo CLI cannot accept a key non-interactively.** `opencode auth login -p <provider> -m <method>` *does* skip both selection menus, but the key prompt is a TUI masked-input widget that consumed piped stdin as keystrokes and wrote nothing. Credential injection for these agents is therefore a **direct store write**, not a CLI spawn (**D1**).
3. **`goose configure` has no non-interactive surface at all** — its entire flag set is `-h`. Goose key injection is a direct `config.yaml` + keyring write (**D1**).
4. **Kilo's `authorize` response field `method` is not the device-vs-browser discriminator.** All four probed flows returned `"auto"`, including a pure browser loopback. The real discriminator is the returned **URL shape** (**D7**). The prior text's rule — `"code"` means device-style, `"auto"` means tunnel — is wrong and would have mis-routed every flow.

**Direction.** Kilo is now the **lead** target, not a deferred one: it is implemented, default-enabled since 2026-08-10, and is the only agent exposing native HTTP credential write, auth status, and engine-hosted OAuth on a transport the daemon already owns.

---

## 1. Context and Problem Statement

magic-cli-remote manages five agent CLIs — **Grok**, **OpenCode**, **Codex**, **Goose**, **Kilo**. Each authenticates to many upstream model providers. Credentials live only on the host:

| store | path / mechanism (this host, 2026-08-10) |
| --- | --- |
| Grok | `~/.grok/auth.json` (0600) or `XAI_API_KEY` |
| OpenCode | `~/.local/share/opencode/auth.json` (`{type, key}` per provider) + env (`OPENROUTER_API_KEY`, `HF_TOKEN`) |
| Codex | `~/.codex/auth.json` (ChatGPT session) or API key via `codex login --with-api-key` |
| Goose | `~/.config/goose/config.yaml` + OS keyring + per-provider token files (`gemini_oauth/`, `chatgpt_codex/`, `xai_oauth/`) |
| Kilo | `~/.local/share/kilo/auth.json` (0600) + env; Kilo Gateway session |

**Protocol today.** `ProviderInfoPayload` (`internal/protocol/messages.go:407`) is exactly `{id, ready}`. No `provider.auth_*`, no `set_credential`, no `oauth.*` message exists anywhere in Go code. `internal/auth` is device pairing only. The sole in-tree auth hook is the static ACP `auth_method_id` (`internal/config/config.go:479`, `acpagent/acpagent.go:386`), which is config-pinned and not phone-controllable.

**What `ready` means today.** `ready` ≈ binary on `PATH` (`internal/provider/registry.go:54`). A provider can be `ready: true` and fail every turn with 401 or 429. The phone cannot distinguish *not installed* from *needs login* from *quota exhausted*.

**Caution for implementers.** Three commits (`11902fe`, `8e2524d`, `287b680`) carry generated messages describing 0074 protocol work as implemented. **They are documentation-only commits.** No auth code has ever landed. Verified at `e4269ef`.

---

## 2. Decision Drivers

* **Headless-first.** Every flow must complete with no browser, no keyboard, and no SSH on the host.
* **Never invent a parallel secret store.** Agents own their credential formats; a second vault would drift and double the blast radius.
* **Do not destabilise shipped providers.** Kilo went default-on 2026-08-10; opencode/goose/codex/grok are in daily use.
* **Evidence over inference.** CLI behaviour drifts silently (grok moved 0.2.118 → 1.0.0 inside four days). Every claim below is pinned to a probe and must be re-pinned in CI.
* **Plan-sized units.** The surface is 5 agents × ~15 upstreams × 3 strategies. One plan cannot hold it.

---

## 3. Considered Options

* **Strategy A — Device Authorization Grant (RFC 8628).** Host starts a device flow; phone displays URL + user code; host polls to completion.
* **Strategy B — Loopback OAuth with a reverse callback tunnel.** Phone opens a browser OAuth URL whose `redirect_uri` points at the *host's* loopback; the redirect is tunnelled back over the existing WebSocket.
* **Strategy C — Credential injection.** Phone sends an API key or token; host writes it to the agent's native store.
* **Strategy D — Auth status and upstream switching.** Surface which upstream is active and whether credentials exist; let the phone switch among already-configured upstreams.
* **Strategy E — External auth provider command.** Host executes an operator-supplied command that prints a token (Grok `auth_provider_command`).

---

## 4. Decision Outcome

**Chosen: C + D first (one wave), then A, with B deferred to a successor MADR and E declined.**

Strategy C+D covers every agent and every commercial upstream that accepts a key, needs no new network machinery, and directly fixes the MADR 0073 operational failure. Strategy A is the only OAuth that works headlessly and — for Kilo — is already implemented engine-side. Strategy B is the only remaining gap, is an order of magnitude more complex (reverse HTTP over WS, single-use tunnels, CSRF state), and earns its own decision record. Strategy E is declined for now: no demand, and it duplicates C with more moving parts.

### 4.1 Locked decisions

Plans cite these IDs.

| id | decision |
| --- | --- |
| **D1** | **Credential write path is per-agent and native.** Kilo → `PUT /auth/{providerID}` on the daemon-owned engine. OpenCode → direct `auth.json` write (0600). Goose → direct `config.yaml` + keyring write. Codex → `codex login --with-api-key` on stdin. Grok → `XAI_API_KEY` in the service environment, or per-model `api_key` in `~/.grok/config.toml`. **No CLI spawn is used where a probe proved the CLI non-drivable** (OpenCode/Kilo key prompt, `goose configure`). |
| **D2** | **mcremote never creates its own credential vault.** Secrets pass through the daemon into the agent's native store and are not persisted anywhere else — not in mcremote config, not in its data dir, not in logs. |
| **D3** | **Auth status is agent-native where available, best-effort elsewhere.** Kilo → `GET /kilo/auth-status` + `GET /provider/auth`. OpenCode/Kilo files → parse `auth.json` keys (names only, never values). Codex → `codex login status`. Goose → `config.yaml` provider set. Grok → `auth.json` presence. Status is advisory; a turn may still 401. |
| **D4** | **`ProviderInfoPayload` gains an `auth` block; `{id, ready}` stays wire-compatible.** New fields are additive and omitted when empty. |
| **D5** | **Auth methods are typed descriptors carrying declared inputs.** A method is `{id, type: api_key\|oauth_device\|oauth_browser, label, inputs[]}` where each input is `{key, type: text\|select, message, options?, placeholder?, required}`. This is required, not speculative: **8 of the 13** upstreams in Kilo's live catalog declare prompt inputs (§7.3). |
| **D6** | **All new messages are gated behind a protocol v2 capability** (`provider_auth`), negotiated in the `auth_ok` `Caps` block (MADR 0068 D1). A phone that does not advertise it never sees auth affordances, and the daemon must not send auth frames to it. |
| **D7** | **Flow classification is derived from the authorize URL, never from the response's `method` field.** A returned URL whose query contains `redirect_uri` pointing at `localhost`/`127.0.0.1` is a **browser loopback** flow (Strategy B, out of scope for wave 1). Otherwise, if the URL or instructions yield a user code, it is a **device** flow (Strategy A). `method: "auto"` is returned for both and carries no routing information. |
| **D8** | **Codex device auth is a guarded, destructive operation.** `codex login --device-auth` deletes the existing credential at flow start. The daemon must (a) copy `~/.codex/auth.json` to a 0600 sidecar before starting, (b) require an explicit phone confirmation naming the consequence, and (c) restore the sidecar if the flow fails, is cancelled, or times out. |
| **D9** | **Engine restart policy after a credential change.** Kilo via HTTP write → **no restart**. OpenCode, Goose, and any Kilo file-path fallback → **restart the shared engine** (idle sessions only; refuse while a turn is running and report why). Codex and Grok → no restart; the next session picks the credential up. |
| **D10** | **Last writer wins across devices.** Credential operations are idempotent overwrites with no locking or merge. Concurrent writes from two phones are a supported race whose outcome is "one of the two values"; the daemon pushes the resulting status to all connected devices. |
| **D11** | **Phone treats secrets as write-only.** The key never leaves the compose buffer, is cleared on send, is never written to phone secure storage, and is never echoed back by the daemon. Status carries presence and metadata only. |
| **D12** | **Anthropic Pro/Max subscription OAuth will not be implemented.** Their terms prohibit third-party plugin auth. Anthropic is API-key only. |
| **D13** | **Device flows need no in-app browser.** Wave 1 opens the verification URL in the system browser (`url_launcher`) and displays the user code for manual entry. `ASWebAuthenticationSession` / an in-app listener is deferred with Strategy B, where a callback actually has to be caught. |
| **D14** | **Phone can switch a provider's active upstream** without re-authenticating, where the agent supports it (Goose `active_provider`; OpenCode/Kilo default model provider). This is the MADR 0073 mitigation and ships in wave 1. |
| **D15** | **Live-tagged tests pin every CLI and HTTP auth surface asserted here**, per agent, behind existing build tags (`live_kilo` and siblings). A CLI drift that breaks a parse is a test failure, not a field incident. |

### 4.2 Consequences

* Good, because wave 1 needs **no new network machinery** — every path is an HTTP call, a file write, or a stdin pipe the daemon already knows how to make.
* Good, because kilo delivers the richest surface almost free: native status, native credential write, and engine-hosted device OAuth over the transport the daemon owns.
* Good, because D7 replaces a wrong inference with a rule derived from four live flows, so wave 2 routing is decided before any code is written.
* Good, because D8 converts a silent credential-destroying trap into a guarded, reversible action.
* Neutral, because direct store writes (D1) couple mcremote to three third-party on-disk formats; D15 mitigates by pinning them in CI.
* Bad, because browser-only upstreams (ChatGPT via OpenCode, GitLab, Snowflake, DigitalOcean) stay SSH-bound until the Strategy B successor MADR lands.
* Bad, because D9's engine restart interrupts idle sessions on opencode/goose, which is user-visible.

### 4.3 Confirmation

1. On a cold host with no credentials, the phone pastes an OpenCode Go key and a subsequent prompt completes — no SSH. (D1, D4)
2. The phone pastes a key for a Kilo upstream; `PUT /auth/{providerID}` returns success and a turn runs **without an engine restart**. (D1, D9)
3. The phone completes a **Kilo Gateway device authorization** end-to-end: authorize returns a code, the phone shows it, the engine polls, and `GET /kilo/auth-status` flips to authenticated. (Strategy A, D7)
4. A phone lacking the `provider_auth` capability receives no auth frames and renders exactly today's UI. (D6)
5. Starting codex device auth from the phone with an existing session shows the destructive-action confirmation; cancelling it leaves `~/.codex/auth.json` byte-identical. (D8)
6. Goose is switched off a quota-exhausted upstream to another configured one from the phone, and the next turn succeeds. (D14, MADR 0073)
7. `grep -ri` over daemon logs at info level after every flow above yields no secret material. (D2, D11)
8. Live-tagged tests fail when a pinned CLI's auth output format changes. (D15)

---

## 5. Agent auth surface (probed)

Versions pinned 2026-08-10. **Grok moved 0.2.118 → 1.0.0** since the previous revision; its auth surface was re-probed and is unchanged.

### 5.1 Kilo — `kilo 7.4.20` (lead target)

Implemented and default-enabled 2026-08-10 (MADR 0075 acceptance flip). Uniquely, the daemon already owns a `kilo serve` engine speaking HTTP.

| capability | endpoint | live result (2026-08-10) |
| --- | --- | --- |
| Auth status | `GET /kilo/auth-status` | `{"authenticated":true,"type":"oauth"}` |
| Method catalog | `GET /provider/auth` | 13 upstreams; **10 oauth + 11 api** methods; 8 declare `prompts` |
| Credential write | `PUT /auth/{providerID}` `{type:"api", key}` | Round-trip proven (MADR 0075 Appendix E) |
| Credential clear | `DELETE /auth/{providerID}` | Proven |
| OAuth start | `POST /provider/{id}/oauth/authorize {method}` | `{url, method, instructions}` — see §7 |
| OAuth finish | `POST /provider/{id}/oauth/callback {method, code}` | For flows that return a pasteable code |

**CLI note.** `kilo auth login` has **no** `-p`/`-m` flags (an older OpenCode fork), so it is even less drivable than OpenCode's. Irrelevant under D1, which uses the HTTP API.

### 5.2 Grok — `grok 1.0.0` (was 0.2.118)

| method | command | headless | storage |
| --- | --- | --- | --- |
| Browser OIDC | `grok login --oauth` | No | `~/.grok/auth.json` (0600) |
| **Device code** | `grok login --device-auth` (alias `--device-code`) | **Yes** | same |
| API key | `XAI_API_KEY` / per-model `api_key` | **Yes** | env / `config.toml` |
| External provider | `auth_provider_command` | Yes | Strategy E, declined |

Precedence: per-model `api_key` → active session → `XAI_API_KEY`. The major version bump did not change any of this.

### 5.3 Codex — `codex-cli 0.146.0`

| method | command | headless | note |
| --- | --- | --- | --- |
| ChatGPT browser | `codex login` | No | — |
| **Device auth** | `codex login --device-auth` | **Yes** | **Destructive at start — D8.** Flag present with empty help text |
| **API key** | `printenv KEY \| codex login --with-api-key` | **Yes** | Cleanest key path of any agent |
| Access token | `codex login --with-access-token` | Yes | CI/advanced |

**Device output (verbatim, stdout, 2026-08-10).** ANSI colour codes wrap the URL (`\x1b[94m`) and the parenthetical (`\x1b[90m`); a parser must strip them.

```text
Follow these steps to sign in with ChatGPT using device code authorization:

1. Open this link in your browser and sign in to your account
   https://auth.openai.com/codex/device

2. Enter this one-time code (expires in 15 minutes)
   K5GK-PUGKG
```

The verification URL is **static**; the code is `[A-Z0-9]{4}-[A-Z0-9]{5}` and expires in 15 minutes.

### 5.4 OpenCode — `1.18.11`

`opencode auth list|login|logout`. Store `~/.local/share/opencode/auth.json` → `{provider: {type, key}}`; this host has `opencode` and `opencode-go`, both `type: api`, plus `OPENROUTER_API_KEY` / `HF_TOKEN` from env.

**Non-interactive probe (2026-08-10).** `opencode auth login -p anthropic -m "Manually enter API Key"` skipped both menus and reached `Enter your API key` — then the masked TUI widget consumed piped stdin as keystrokes, reset, and **wrote nothing**. `auth.json` was byte-identical afterwards. Hence D1's direct-write path.

### 5.5 Goose — `1.45.0`

`goose configure` accepts **only `-h`**. There is no non-interactive credential path, no `goose auth`, and no secret-set subcommand. Hence D1's direct `config.yaml` + keyring write.

Configured on this host: `opencode_go` (active), `gemini_oauth`, `google`, `chatgpt_codex`, `xai_oauth` — the multi-upstream case D14 targets. OAuth knobs in the binary: `GOOSE_OAUTH_CALLBACK_PORT`, `GOOSE_OAUTH_CALLBACK_TIMEOUT_SECONDS`, and an RFC 8628 device-code grant. The fixed callback port makes goose the best Strategy B target when that MADR is written.

---

## 6. Upstream capability matrix

Two layers stay separate: what the **vendor** allows, and what each **agent** implements.

| platform | API key | OAuth for agents | device grant | note |
| --- | --- | --- | --- | --- |
| xAI / Grok | ✅ | ✅ | ✅ | Best dual path |
| ChatGPT (subscription) | ❌ | ✅ via Codex/OpenCode/Goose/Kilo | ✅ | Not an API-key product |
| OpenAI Platform | ✅ | ❌ | ❌ | Keys only |
| Anthropic | ✅ | ❌ (D12) | ❌ | Key only |
| GitHub Copilot | ❌ | ✅ | ✅ | Device at `github.com/login/device` |
| Google Gemini (consumer) | ✅ | ✅ (goose `gemini_oauth`) | varies | ≠ Vertex ADC |
| Hugging Face | ✅ | ✅ | ✅ | Token is simplest |
| OpenRouter | ✅ | ✅ (mints a key) | ❌ | Key still lands in the store |
| OpenCode Go / Zen | ✅ | web login mints key | ❌ | The 0073 quota surface |
| GitLab / DigitalOcean / Snowflake | PAT | ✅ browser | ❌ | Strategy B only |
| Azure / Bedrock / Vertex | ✅ / IAM | Entra / federation | via cloud CLI | Advanced; env injection |

Legend for agents: **D** device, **L** loopback browser, **K** key, **E** external/env.

| upstream ↓ / agent → | Grok | Codex | OpenCode | Goose | **Kilo** |
| --- | --- | --- | --- | --- | --- |
| xAI SuperGrok | **D, L, K, E** | — | **D, K** | **L, K** | **K** |
| ChatGPT subscription | — | **L, D, K** | **L, K** | **L** | **D** (headless), **L** (browser) |
| OpenAI Platform | — | **K** | **K** | **K** | **K** |
| GitHub Copilot | — | — | **D** | **D** | **D** |
| Gemini consumer | — | — | plugin | **L** | — |
| Anthropic | — | — | **K** | **K** | **K** |
| OpenRouter / HF | — | — | **K** | **L/K** | **K** |
| OpenCode Go / Zen | — | — | **K** | **K** | **K** |
| Kilo Gateway | — | — | — | — | **D** |
| GitLab / Snowflake / Cloudflare / Azure | — | — | **L/K** | **K/E** | **L/K** + inputs |

---

## 7. Live OAuth flow evidence (2026-08-10)

Four `POST /provider/{id}/oauth/authorize` calls against the daemon's engine. This is the evidence behind **D7**.

### 7.1 Device flows — no tunnel required

| upstream | url | instructions |
| --- | --- | --- |
| `kilo` (Gateway Device Authorization) | `https://app.kilo.ai/device-auth?code=RX2Y-4H7X` | `Open … and enter code: RX2Y-4H7X` |
| `openai` (ChatGPT Pro/Plus **headless**) | `https://auth.openai.com/codex/device` | `Enter code: K5K8-L04UB` |
| `github-copilot` | `https://github.com/login/device` | `Enter code: 8A31-10BC` |

All three returned `"method": "auto"`. The engine polls to completion by itself, so the phone only displays the URL and code — **no `oauth/callback` POST and no tunnel**.

### 7.2 Browser loopback — tunnel required

`openai` method 0 (ChatGPT Pro/Plus **browser**) also returned `"method": "auto"`, but:

```text
url:          https://auth.openai.com/oauth/authorize?response_type=code&client_id=…
              &redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback&…
instructions: Complete authorization in your browser. This window will close automatically.
```

The engine listens on the **host's** `localhost:1455`. A phone browser cannot reach it, so this class needs Strategy B.

**Therefore (D7):** classify by URL. `redirect_uri` targeting loopback ⇒ browser flow ⇒ out of scope for wave 1. Otherwise extract the user code from the URL query or the `Enter code: <CODE>` instruction ⇒ device flow.

### 7.3 Declared inputs — evidence for D5

Eight of thirteen upstreams cannot be authenticated with a bare key or a bare URL:

| upstream | method | required inputs |
| --- | --- | --- |
| `github-copilot` | Login with GitHub Copilot | `deploymentType` (select), `enterpriseUrl` (text) |
| `azure` | API key | `endpointType` (select), `resourceName`, `baseURL` |
| `snowflake-cortex` | External Browser | `account`, `role` |
| `snowflake-cortex` | Paste PAT | `account` |
| `gitlab` | OAuth | `instanceUrl` |
| `gitlab` | Personal Access Token | `instanceUrl` |
| `cloudflare-ai-gateway` | Gateway API token | `accountId`, `gatewayId` |
| `cloudflare-workers-ai` | API key | `accountId` |

A protocol modelling methods as `{type, label}` fails on all eight. Note that `authorize` for `github-copilot` succeeded **without** supplying its prompts, so inputs are defaultable rather than strictly mandatory — the phone should render them but must tolerate their absence.

---

## 8. Protocol design

### 8.1 Capability gate (D6)

`provider_auth` is advertised in the v2 `Caps` block (`internal/protocol/messages.go:159`). Absent it, the daemon behaves exactly as today.

### 8.2 Messages

```text
providers.list_result          → ProviderInfoPayload gains optional "auth" (D4)
provider.auth_status           → server push on any credential/status change
provider.set_credential        → client { provider_id, upstream_id, method_id, secret, inputs{} }
provider.clear_credential      → client { provider_id, upstream_id }
provider.set_active_upstream   → client { provider_id, upstream_id }            (D14)
provider.start_auth            → client { provider_id, upstream_id, method_id, inputs{}, confirm_destructive? }
oauth.device_flow              → server { flow_id, verification_uri, user_code, expires_in, interval }
oauth.device_flow_result       → server { flow_id, ok | error, error_kind? }
oauth.cancel                   → client { flow_id }
```

`oauth.open_browser` and the `oauth.loopback_tunnel_*` family from the prior sketch are **removed** — they belong to the Strategy B successor MADR.

### 8.3 Payload shapes

```json
{
  "id": "kilo",
  "ready": true,
  "auth": {
    "status": "configured",
    "active_upstream": "kilo",
    "upstreams": [
      {
        "id": "kilo",
        "label": "Kilo Gateway",
        "status": "configured",
        "methods": [
          { "id": "kilo:0", "type": "oauth_device", "label": "Kilo Gateway (Device Authorization)", "inputs": [] }
        ]
      },
      {
        "id": "github-copilot",
        "label": "GitHub Copilot",
        "status": "missing",
        "methods": [
          {
            "id": "github-copilot:0",
            "type": "oauth_device",
            "label": "Login with GitHub Copilot",
            "inputs": [
              { "key": "deploymentType", "type": "select", "message": "Select GitHub deployment type",
                "options": ["github.com", "enterprise"], "required": false },
              { "key": "enterpriseUrl", "type": "text", "message": "Enterprise URL", "required": false }
            ]
          }
        ]
      }
    ]
  }
}
```

`status` ∈ `configured | missing | error | quota`. `quota` reuses the `agenterr` classification already surfaced by MADR 0073 work, so a wedged upstream is visibly distinct from an unauthenticated one.

### 8.4 Phone UX

1. **Provider list** — per-upstream chip: configured / needs setup / error / quota.
2. **Setup sheet** — renders the method's `inputs[]` as a form, then either a masked key field (`api_key`) or a "Start sign-in" button (`oauth_device`). Browser-only methods render disabled with "requires host access" until Strategy B lands.
3. **Device sheet** — large monospaced user code with copy, verification URL button, expiry countdown, live poll status, cancel.
4. **Destructive confirmation** — codex device auth names the consequence: "This signs the host out of ChatGPT immediately, before you finish signing in." (D8)
5. **Switch active upstream** — one tap among configured upstreams (D14).

---

## 9. Security

| topic | decision |
| --- | --- |
| Transit | Existing mTLS + device-token WS (MADR 0015 / 0068) |
| Host storage | Agent-native stores only (D2); 0600 on every file mcremote writes |
| Phone storage | Write-only; buffer cleared on send; never persisted (D11) |
| Echo-back | Status carries presence and metadata, never key material |
| Logging | No secret at any level; `auth.json` values redacted in doctor output |
| Destructive ops | Snapshot-and-restore around codex device auth (D8) |
| Concurrency | Last writer wins, documented, status pushed to all devices (D10) |
| ToS | No Anthropic Pro/Max plugin OAuth (D12) |
| Multi-tenant | Credentials are per-OS-user; phone auth targets *that* user's agent stores |

---

## 10. Alternatives rejected

### Strategy B in wave 1

* Good, because it is the only path to ChatGPT-via-OpenCode, GitLab, Snowflake, and DigitalOcean.
* Bad, because it requires reverse HTTP over the WS control plane, single-use tunnel lifecycle, CSRF state propagation, and a phone-side listener or `ASWebAuthenticationSession` — each an independent failure mode.
* Bad, because it would gate the entire P0 key-injection win behind the hardest component.

### Strategy E (external auth provider command)

* Good, because it is scriptable and enterprise-friendly.
* Bad, because it duplicates Strategy C's outcome with an extra indirection, and no demand exists.

### A single mcremote credential vault

* Good, because it would give one uniform write path instead of five.
* Bad, because it violates D2, drifts from every agent's format on upgrade, and doubles the number of places a secret can leak.

### Driving agent CLIs interactively via a pty

* Good, because it would reuse each vendor's own validation logic.
* Bad, because probes show two of the five TUIs actively defeat piped input; a pty screen-scraper against five drifting TUIs is unmaintainable.

---

## 11. Implementation workstreams

This MADR is the backbone for **four** plans. Only W1 inherits this MADR's identifier; later waves get their own MADR + PLAN pair, per the repository's `NNNN-MADR`/`NNNN-PLAN` pairing rule.

| id | scope | artifact | depends on |
| --- | --- | --- | --- |
| **W1** | Auth status + credential injection + active-upstream switch. All five agents, D1–D6, D9–D12, D14, D15. Includes the protocol block, capability gate, daemon plumbing, and the phone setup sheet. | `0074-PLAN-remote-provider-auth-from-phone.md` | this MADR |
| **W2** | Device OAuth (Strategy A). Kilo engine-hosted first (already proven), then Grok, then Codex behind D8's guard, then Copilot via OpenCode/Goose. | successor MADR + plan | W1 protocol |
| **W3** | Loopback OAuth with reverse callback tunnel (Strategy B). Goose first via `GOOSE_OAUTH_CALLBACK_PORT`; OpenCode browser flows second. Owns the D13 revisit. | successor MADR + plan | W2 |
| **W4** | Polish: auth-failure classification through `agenterr` (401/invalid_key), doctor integration, phone-side revoke/logout for every agent. | folded into W1/W2 plans or a small successor | W1 |

**W1 acceptance** is §4.3 items 1, 2, 4, 6, 7, 8. **W2 acceptance** adds items 3 and 5.

---

## 12. Questions — all resolved

| # | question | resolution |
| --- | --- | --- |
| 1 | Does `codex login --device-auth` print a stable, parseable code and URL? | **Yes** (probe 2026-08-10, §5.3): static URL `https://auth.openai.com/codex/device`, code `[A-Z0-9]{4}-[A-Z0-9]{5}`, 15-minute expiry, on stdout, ANSI-wrapped. **Also discovered:** the flow deletes the existing credential at start → **D8**. |
| 2 | Can OpenCode `/connect` be driven non-interactively for a named provider + key? | **Partially — and not for the key.** `-p`/`-m` skip both menus, but the masked key widget ignores piped stdin and writes nothing (probe 2026-08-10, §5.4). Resolved by **D1** direct `auth.json` write. |
| 3 | Does goose have a non-interactive configure or secret-set API? | **No.** `goose configure` exposes only `-h` (probe 2026-08-10, §5.5). Resolved by **D1** direct `config.yaml` + keyring write. |
| 4 | Should engines restart after a credential change? | **Decided — D9.** Kilo HTTP write: no. OpenCode/Goose/file paths: yes, idle only. Codex/Grok: no. |
| 5 | Multi-device write semantics? | **Decided — D10.** Last writer wins, no locking, status pushed to all devices. |
| 6 | iOS: local HTTP listener vs `ASWebAuthenticationSession`? | **Decided — D13.** Neither in wave 1: device flows need no callback, so the system browser plus a displayed code suffices. Revisited in W3, where a callback must actually be caught. Note the iOS port is simulator-only today (MADR 0067). |
| 7 | Kilo: do the auth endpoints work, and which providers need a tunnel? | **Yes, re-verified 2026-08-10** (§5.1, §7). Catalog: 13 upstreams, 10 oauth + 11 api, 8 with declared inputs. Four authorize flows driven live. **Correction:** the `method` field is `"auto"` for device *and* browser flows and is not a discriminator — routing is by URL shape → **D7**. |

---

## 13. Conclusion

Remote provider auth is not blocked by the ecosystem — every agent implements OAuth or device flows, and nearly every commercial upstream accepts a key. The gap is entirely in mcremote's control plane, and it is now fully specified.

The 2026-08-10 probes replaced three guesses with facts: OpenCode and Goose CLIs cannot be driven for credentials (so mcremote writes their stores directly), codex device auth is destructive before it is useful (so it must be guarded), and Kilo's flow-mode field means something other than what the prior text assumed (so routing keys off the URL). Kilo, deferred as "not started" in the previous revision, is now shipped, default-enabled, and the strongest target in the tree: native status, native credential write, and engine-hosted device OAuth over a transport the daemon already owns.

Wave 1 (W1) is a self-contained, plan-ready unit that fixes MADR 0073 operationally and delivers headless setup for every agent, using nothing more exotic than an HTTP call, a file write, and a stdin pipe.

---

## Appendix A — Host snapshot and probe log

**Versions, 2026-08-10:** grok **1.0.0** (was 0.2.118 on 2026-08-06), opencode 1.18.11, codex-cli 0.146.0, goose 1.45.0, kilo 7.4.20.

| agent | evidence |
| --- | --- |
| Grok 1.0.0 | `login --help` → `--oauth`, `--device-auth` (alias `--device-code`); `~/.grok/auth.json` present, 0600. Major-version bump did not change the auth surface. |
| Codex 0.146.0 | `login status` → "Logged in using ChatGPT"; `--with-api-key`, `--with-access-token`, `--device-auth` (empty help text). Device flow output captured verbatim (§5.3). |
| OpenCode 1.18.11 | `auth list` → OpenCode Zen (api), OpenCode Go (api), OpenRouter via `OPENROUTER_API_KEY`; `auth login -p/-m` reach the key prompt but ignore piped stdin. |
| Goose 1.45.0 | `configure --help` → `-h` only. Configured providers: `opencode_go` (active), `gemini_oauth`, `google`, `chatgpt_codex`, `xai_oauth`. |
| Kilo 7.4.20 | `GET /kilo/auth-status` → `{"authenticated":true,"type":"oauth"}`; `GET /provider/auth` → 13 upstreams / 21 methods / 8 with inputs; four authorize flows driven (§7). |

**Probe safety.** Every credential store was copied to a 0600 sidecar before probing. The codex device-auth probe deleted `~/.codex/auth.json`; it was restored byte-identically and `codex login status` re-confirmed. The OpenCode probe wrote nothing (`auth.json` byte-identical). The Kilo authorize probes started device flows that were left to expire and wrote nothing (`auth.json` byte-identical). No credential was changed by this research.

**Codebase verification at `e4269ef` (2026-08-10).** `ProviderInfoPayload` is `{id, ready}` (`internal/protocol/messages.go:407`). No `provider.auth_*`, `set_credential`, or `oauth.*` symbol exists in Go code. `internal/auth` is pairing-only. Static ACP `auth_method_id` remains the sole auth hook. The Flutter app has no provider-auth UI. Commits `11902fe`, `8e2524d`, and `287b680` claim auth features in their messages but are documentation-only.

## Appendix B — Primary URLs

- <https://opencode.ai/docs/providers/>
- <https://opencode.ai/docs/go/>
- <https://docs.x.ai/build/enterprise>
- <https://github.com/xai-org/grok-build>
- <https://goose-docs.ai/blog/2026/03/19/use-goose-with-your-ai-subscription/>
- <https://github.com/block/goose>
- <https://learn.chatgpt.com/docs/auth>
- <https://kilo.ai/docs/code-with-ai/platforms/cli>

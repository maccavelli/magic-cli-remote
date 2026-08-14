# MADR 0074 — Remote Provider Authentication from Phone

<!-- markdownlint-disable MD013 MD024 MD033 MD036 -->

| field | value |
| --- | --- |
| status | **accepted** 2026-08-10, **implemented** 2026-08-12. D1–D15 locked 2026-08-10; **D16–D19 added 2026-08-12** after re-probing all five CLIs. W1, W2 and W4 are in the tree and verified below (§14); W3 (loopback tunnel) remains deferred to a successor MADR. Supersedes the *research-report* form of this document (2026-08-05/06), which proposed strategies but locked nothing. |
| date | 2026-08-10, revised 2026-08-12 |
| deciders | @saxsmith |
| related | MADR 0015 (relay), **0019** (single-engine, engine-owned config), 0021 (OpenCode API), 0025 (Goose), 0028 (Codex), 0029 (platform), 0043 (models), **0044** (permission funnel), **0067** (iOS port), **0068** (protocol v2 capabilities), **0073** (goose quota hang), **0075** (Kilo provider — **now implemented and default-enabled**, auth landed here), **0079** (provider/model drill-down picker — same "browse a large catalog" problem, solved the same way) |
| method | Live CLI probes on this host, twice: 2026-08-10 (grok 1.0.0, opencode 1.18.11, codex-cli 0.146.0, goose 1.45.0, kilo 7.4.20) and **2026-08-12** (grok **1.0.3**, opencode **1.18.16**, codex-cli **0.147.0**, goose 1.45.0, kilo **7.4.21**). Live HTTP probes against `opencode serve` and `kilo serve` (vendor catalog, auth-method catalog, credential write round-trip in an isolated `XDG_DATA_HOME`); goose provider registry read from the vendor's own source checkout at 1.46.0; codex config schema read out of the shipped binary; codebase verification at `e4269ef` (2026-08-10) and at `7b07609` + working tree (2026-08-12) |
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

### 0.1 What the 2026-08-12 revision changes

W1/W2/W4 shipped, and using them surfaced a gap the original decisions did not cover: **the phone could only re-key a vendor the host was already using.** Status answered "what is configured", and nothing answered "what could be configured", so a vendor with no credential yet had no row to tap. Together AI, DeepSeek, Groq, Cerebras, Mistral, Perplexity and roughly 170 others were unreachable from the phone even though every agent supports them.

Three probes closed it, and two of them correct earlier text:

1. **OpenCode's engine has the same auth API as Kilo's.** `GET /provider` returns 184 vendors with display names, `GET /provider/auth` returns typed methods, and `PUT`/`DELETE /auth/{id}` write credentials — round-trip proven for `togetherai` in an isolated `XDG_DATA_HOME`, with the engine picking it up without a restart. D1's "OpenCode → direct `auth.json` write" was a correct reading of the *CLI* probe and a wrong conclusion about the *agent*: **D17** moves OpenCode onto the engine API and keeps the file write as the cold-host fallback.
2. **The vendor catalog is two orders of magnitude larger than the status block** — 185 upstreams on kilo 7.4.21, 184 on opencode 1.18.16, against 13 and 10 in the respective auth-method catalogs. Riding it along with `providers.list` would put tens of kilobytes on the wire every time a chip changed colour, so **D16** makes it a separate, searchable, paged request.
3. **Goose supports 73 vendors and can list none of them.** `goose configure` still takes only `-h`, the ACP surface carries no catalog, and the declarative provider definitions are compiled into the binary with `include_dir!`. **D18** pins the table from goose's own metadata and writes keys into goose's file secret store — and refuses, with an actionable message, on a host where goose reads the OS keyring instead.

Codex and Grok stay single-vendor, and **D19** records why rather than leaving it looking like an omission.

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

**Protocol before this MADR** (state at `e4269ef`, 2026-08-10, preserved because the decisions below are written against it). `ProviderInfoPayload` was exactly `{id, ready}`. No `provider.auth_*`, no `set_credential`, no `oauth.*` message existed anywhere in Go code. `internal/auth` was device pairing only. The sole in-tree auth hook was the static ACP `auth_method_id` (`internal/config/config.go:479`, `acpagent/acpagent.go:386`), config-pinned and not phone-controllable.

**What `ready` meant.** `ready` ≈ binary on `PATH` (`internal/provider/registry.go:54`). A provider could be `ready: true` and fail every turn with 401 or 429. The phone could not distinguish *not installed* from *needs login* from *quota exhausted*. It now can: `agenterr.KindAuth` separates a dead credential from a quota block, and both reach the phone as upstream status.

**Caution for implementers, as written 2026-08-10.** Three commits (`11902fe`, `8e2524d`, `287b680`) carry generated messages describing 0074 protocol work as implemented. **They are documentation-only commits**, and were still the only 0074 commits at `e4269ef`. The real implementation landed afterwards in `a5c408b`, `58906a6`, `e604146`, `03d435c`, `9429f5a` — see §14.

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
| **D16** | **Every upstream an agent supports is configurable from the phone, through a catalog that is separate from status.** `provider.auth_status` / the `auth` block answer *what is configured* and stay small; `provider.auth_catalog` answers *what could be configured* and is fetched on demand, filtered server-side by a query, and paged (100 default, 200 max). A page that is not the last says so, and the answer names its source (`engine` for a live read, `static` for a pinned table) so the phone can admit when its list may be stale. Two consequences that matter operationally: **status never reads the vendor catalog** — it uses `GET /config/providers` (connected set plus names), because `GET /provider` is 4.7 MB and status runs on every `providers.list` — and the catalog itself is **cached for 5 minutes per engine and invalidated on any credential write**, so paging and searching do not re-pull it. |
| **D17** | **OpenCode credentials go through its engine, not its file.** `PUT /auth/{id} {type:"api", key}` and `DELETE /auth/{id}` — the same calls kilo takes, proven on 1.18.16. This narrows D1: the direct `auth.json` write remains, but only as the fallback for when no engine can be started. Consequence for **D9**: an OpenCode credential change through the engine needs **no restart**; only the fallback path does. |
| **D18** | **Goose's catalog is pinned, and its credential write is file-store-only.** Goose can enumerate its providers to no one — `goose configure` takes only `-h`, and its declarative vendor definitions are compiled into the binary — so mcremote ships a table transcribed from goose's own metadata (73 vendors, pinned to 1.46.0) and labels the answer `static`. Writes go to `~/.config/goose/secrets.yaml`, which is goose's own format, **only when goose reads that file** (`GOOSE_DISABLE_KEYRING` in the environment or in `config.yaml`). On a keyring-backed host the daemon **refuses with an actionable error** instead of writing a file goose will not read. mcremote does not write the OS keyring: the portable ways to do it either put the secret in `argv`, where any process can read it out of `ps` (forbidden by D2), or need an interactive unlock no headless flow can answer. |
| **D19** | **Codex and Grok are single-upstream by nature, and stay that way.** Grok talks only to xAI. Codex does support third-party OpenAI-compatible vendors through `model_providers` in `config.toml` (fields verified in the 0.147.0 binary: `base_url`, `env_key`, `env_key_instructions`, `experimental_bearer_token`, `wire_api`, `requires_openai_auth`), but a key for one is read from **an environment variable**, which would require mcremote to hold the secret itself to inject at spawn — precisely the parallel vault D2 forbids. The phone therefore configures `openai` for codex and `xai` for grok, and any third-party codex vendor stays an operator's `config.toml` + environment job. Revisit only if codex gives `experimental_bearer_token` a stable name. |

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
9. From a cold host, the phone finds **Together AI** in OpenCode's catalog by typing "together", pastes a key, and the vendor reads back configured — with no engine restart. (D16, D17)
10. The same search finds Together AI under **Kilo** and under **goose**, and goose's answer is labelled as a pinned list. (D16, D18)
11. A catalog page stays inside a phone-sized frame: 100 upstreams in one page, well under 64 KB. (D16)
12. On a keyring-backed host, a goose credential write fails with a message naming what to do about it, and writes no file. (D18)

---

## 5. Agent auth surface (probed)

Versions pinned 2026-08-10 and re-probed 2026-08-12 (grok 1.0.3, opencode 1.18.16, codex-cli 0.147.0, goose 1.45.0, kilo 7.4.21). **Grok moved 0.2.118 → 1.0.0 → 1.0.3** across these probes; its auth surface is unchanged throughout. The one surface that changed materially is OpenCode's — see §5.4 and D17.

### 5.1 Kilo — `kilo 7.4.20`, re-probed at `7.4.21` (lead target)

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

### 5.4 OpenCode — `1.18.11`, re-probed at `1.18.16`

`opencode auth list|login|logout`. Store `~/.local/share/opencode/auth.json` → `{provider: {type, key}}`; this host has `opencode` and `opencode-go`, both `type: api`, plus `OPENROUTER_API_KEY` / `HF_TOKEN` from env.

**Non-interactive CLI probe (2026-08-10).** `opencode auth login -p anthropic -m "Manually enter API Key"` skipped both menus and reached `Enter your API key` — then the masked TUI widget consumed piped stdin as keystrokes, reset, and **wrote nothing**. `auth.json` was byte-identical afterwards.

**Engine probe (2026-08-12) — this is what D17 rests on.** The CLI result above says nothing about `opencode serve`, which the daemon already runs. It answers the same endpoints kilo does:

| capability | endpoint | live result (1.18.16) |
| --- | --- | --- |
| Vendor catalog | `GET /provider` | `{all: [184 vendors], connected: […]}`; each entry carries `id`, `name` ("Together AI"), `env` (`["TOGETHER_API_KEY"]`) and its model list |
| Method catalog | `GET /provider/auth` | 10 upstreams; the same typed prompts kilo returns (copilot's `deploymentType`, gitlab's `instanceUrl`, …) |
| Credential write | `PUT /auth/{id}` `{type:"api", key}` | `200 true`; `togetherai` appeared in `GET /provider`'s `connected` set immediately, **no restart** |
| Credential clear | `DELETE /auth/{id}` | `200 true`; entry removed |
| Store written | `~/.local/share/opencode/auth.json` | written by the engine itself at **0600** |

The write probe ran against an engine booted with an isolated `XDG_DATA_HOME`, so the host's real store was never touched.

`connected` is strictly better than parsing `auth.json`: it includes vendors keyed through the environment (`openrouter`, `huggingface` on this host), which never appear in the file.

### 5.5 Goose — `1.45.0` (source read at `1.46.0`)

`goose configure` accepts **only `-h`**. There is no non-interactive credential path, no `goose auth`, and no secret-set subcommand.

Configured on this host: `opencode_go` (active), `gemini_oauth`, `google`, `chatgpt_codex`, `xai_oauth` — the multi-upstream case D14 targets. OAuth knobs in the binary: `GOOSE_OAUTH_CALLBACK_PORT`, `GOOSE_OAUTH_CALLBACK_TIMEOUT_SECONDS`, and an RFC 8628 device-code grant. The fixed callback port makes goose the best Strategy B target when that MADR is written.

**Provider universe (read from goose's source, 2026-08-12).** Goose knows 73 vendors, from three places, and exposes none of them to a caller:

| source | count | examples |
| --- | --- | --- |
| `crates/goose-providers/src/declarative/definitions/*.json` — data files compiled in with `include_dir!` | 39 | `together` (Together AI, `TOGETHER_API_KEY`), `custom_deepseek`, `groq`, `cerebras`, `mistral`, `perplexity`, `opencode_go` |
| coded providers with `ProviderMetadata` + `ConfigKey` | ~31 | `xai`, `anthropic`, `openai`, `snowflake`, `databricks`, `litellm`, `azure_openai` |
| `crates/goose-provider-types/src/canonical/catalog.rs` `provider_id`s | 32 (overlapping) | the ids real `config.yaml` files carry |

**Secret storage (`crates/goose/src/config/base.rs`).** Precedence is environment → keyring (service `goose`, account `secrets`, one JSON blob) → `~/.config/goose/secrets.yaml`. The file is **not** a fallback for a keyring miss: goose reads one store or the other, chosen by `GOOSE_DISABLE_KEYRING` (environment or `config.yaml`). It does flip to the file at runtime when a keyring operation fails with an availability error — the headless case — but that decision happens inside a goose process and cannot be observed from the daemon. Hence D18: write the file when goose is configured to read it, and refuse otherwise.

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

### 6.1 Coverage delivered (D16) — what the phone can configure, per agent

Counts are live as of 2026-08-12 and are asserted by the live-tagged tests.

| agent | vendors offered | catalog source | write path | restart (D9) |
| --- | --- | --- | --- | --- |
| **Kilo** 7.4.21 | **185** — every vendor in the engine's models.dev snapshot, including `togetherai`, `deepseek`, `groq`, `cerebras`, `mistral`, `perplexity`, `zai`, `venice`, plus the 13 with typed methods | `GET /provider` ∪ `GET /provider/auth`, live | `PUT`/`DELETE /auth/{id}` | none |
| **OpenCode** 1.18.16 | **184** — same catalog, minus kilo's own gateway | same, live | `PUT`/`DELETE /auth/{id}`, falling back to a 0600 `auth.json` merge when no engine can start | none via engine; restart on the fallback |
| **Goose** 1.45.0 | **73** — goose's whole registry: 39 declarative vendors (`together`, `custom_deepseek`, `groq`, …), ~31 coded ones (`xai`, `anthropic`, `openai`, `snowflake`, `databricks`, …), and the canonical ids | pinned table (`internal/provider/goose/catalog.go`), labelled `static` | `secrets.yaml` merge under the vendor's own key name, **only** when goose reads that store; refusal with guidance otherwise (D18) | engine restart, as before |
| **Codex** 0.147.0 | **1** — `openai` (ChatGPT session or platform key) | n/a | `codex login --with-api-key` on stdin | none |
| **Grok** 1.0.3 | **1** — `xai` | n/a | `~/.grok/config.toml` `[auth] api_key`, 0600 | none |

Vendors whose only method is a browser callback to the host's loopback (GitLab OAuth, Snowflake external browser, DigitalOcean, OpenAI's browser flow) appear in the catalog but render disabled: they need the W3 tunnel, and offering a button that fails on press would be worse than saying so.

The 26 subscription- and CLI-backed goose providers (`chatgpt_codex`, `gemini_oauth`, `xai_oauth`, `github_copilot`, `claude-code`, `codex`, `cursor-agent`, the `*-acp` bridges, `ollama`, …) also appear, typed as host-side sign-in. They have no key to paste — goose gets them from another CLI's own session — so the phone shows them rather than hiding a provider the user can see in `goose configure`.

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
provider.auth_catalog          → client { provider_id, query?, offset?, limit? }   (D16)
provider.auth_catalog_result   → server { provider_id, upstreams[], offset, total,
                                          truncated, source }                      (D16)
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

`provider.auth_catalog` is deliberately **not** a field on `providers.list_result`. The status block is a handful of upstreams and rides on every listing and every status push; the catalog is ~185 vendors. Measured: one 100-vendor page is ~6 KB, so a full catalog on every listing would be ~12 KB of pure repetition per refresh on both OpenCode-family agents, on a link that is often cellular.

### 8.4 Phone UX

1. **Provider list** — per-upstream chip: configured / needs setup / error / quota.
2. **Add credential** — one row per agent opens the catalog browser: a search field, a paged list of every vendor the agent supports, "showing 100 of 185" when the list is partial, and a note when the list is a pinned table rather than a live read (D16). Picking a vendor drops into the same setup sheet below.
3. **Setup sheet** — renders the method's `inputs[]` as a form, then either a masked key field (`api_key`) or a "Start sign-in" button (`oauth_device`). Browser-only methods render disabled with "requires host access" until Strategy B lands.
4. **Device sheet** — large monospaced user code with copy, verification URL button, expiry countdown, live poll status, cancel.
5. **Destructive confirmation** — codex device auth names the consequence: "This signs the host out of ChatGPT immediately, before you finish signing in." (D8)
6. **Switch active upstream** — one tap among configured upstreams (D14).

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

| id | scope | artifact | depends on | status |
| --- | --- | --- | --- | --- |
| **W1** | Auth status + credential injection + active-upstream switch. All five agents, D1–D6, D9–D12, D14, D15. Includes the protocol block, capability gate, daemon plumbing, and the phone setup sheet. | `0074-PLAN-remote-provider-auth-from-phone.md` P1–P6 | this MADR | **done** |
| **W2** | Device OAuth (Strategy A). Kilo engine-hosted first (already proven), then Grok, then Codex behind D8's guard, then Copilot via OpenCode/Goose. | same plan, P7–P10 | W1 protocol | **done** for Kilo, Grok and Codex; Copilot-via-OpenCode/Goose not started |
| **W3** | Loopback OAuth with reverse callback tunnel (Strategy B). Goose first via `GOOSE_OAUTH_CALLBACK_PORT`; OpenCode browser flows second. Owns the D13 revisit. | successor MADR + plan | W2 | **not started** |
| **W4** | Polish: auth-failure classification through `agenterr` (401/invalid_key), doctor integration, phone-side revoke/logout for every agent. | same plan, P11 | W1 | **done** |
| **W5** | Full vendor coverage (D16–D19): on-demand catalog, OpenCode engine writes, goose's pinned registry and file-store writes, phone catalog browser. | same plan, P12–P15 | W1 | **done** 2026-08-12 |

**W1 acceptance** is §4.3 items 1, 2, 4, 6, 7, 8. **W2 acceptance** adds items 3 and 5. **W5 acceptance** is items 9–12.

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

Remote provider auth was never blocked by the ecosystem — every agent implements OAuth or device flows, and nearly every commercial upstream accepts a key. The gap was entirely in mcremote's control plane, and it is now closed for wave 1, wave 2 and full vendor coverage.

The 2026-08-10 probes replaced three guesses with facts: the OpenCode and Goose *CLIs* cannot be driven for credentials, codex device auth is destructive before it is useful (so it must be guarded), and Kilo's flow-mode field means something other than what the prior text assumed (so routing keys off the URL).

The 2026-08-12 probes replaced one of those facts with a better one. "The CLI cannot take a key" is true of OpenCode and says nothing about `opencode serve`, which answers the same credential API kilo does — so OpenCode writes go through the engine, and its 184-vendor catalog is readable the same way kilo's 185 is. That, plus a pinned table for goose's 73, is what turns "re-key what you already have" into "configure anything the agent supports" (D16–D19).

What remains is W3: the browser-loopback vendors — GitLab, Snowflake, DigitalOcean, and ChatGPT-via-OpenCode's browser flow — which need a reverse callback tunnel and their own decision record. They are visible in the catalog today, marked as needing host access, so the gap is legible to the user rather than silent.

---

## 14. Implementation verification (2026-08-12)

Every claim below was checked against the tree, not against a commit message. Line numbers are as of the working tree at `7b07609`+.

### 14.1 Decisions → code

| decision | where it lives | evidence |
| --- | --- | --- |
| D1 native per-agent writes | `credstore/write.go`, `codex/auth.go:29`, `grok/auth.go:22`, `kilo/auth.go`, `opencode/auth.go` | per-agent tests in each package |
| D2 no parallel vault | `credstore` package doc; `SetCredentialPayload.LogValue` | `protocol` redaction test; log-capture test in `internal/ws` |
| D3 agent-native status | `provider.Auth` in `provider/auth.go:118`; five implementations | `TestAuthStatus*` per provider |
| D4 additive `auth` block | `protocol/messages.go` `ProviderInfoPayload.Auth` | v1 byte-identity test |
| D5 typed methods with inputs | `AuthMethod`/`AuthInput` in `provider/auth.go` | kilo catalog fixture test (8 prompt-bearing methods) |
| D6 capability gate | `ws/liveness.go:168`, `ws/server.go` `clientWantsProviderAuth` | v1/v2 tests, and every auth handler refuses without it |
| D7 classify by URL | `internal/providerauth/classify.go` | table test over the four captured authorize responses |
| D8 codex guard | `codex/device_auth.go` | sidecar create/restore lifecycle tests |
| D9 restart policy | `httpagent.RestartForCredentialChange`; kilo and OpenCode-via-engine skip it | restart-guard test |
| D10 last writer wins | `credstore.MergeJSONAuth`; push consumed by `providerAuthStatus` on the phone | concurrent-write test in `internal/ws`; `provider_auth_push_test.dart` — the phone ignored these pushes until 2026-08-12 |
| D11 write-only secrets | `provider_auth_sheet.dart` | widget test: controller empty after send |
| D12 no Anthropic OAuth | not implemented, by decision | — |
| D13 system browser | `device_flow_sheet.dart`, reached from `settings_screen.dart` `_runDeviceSignIn` | widget tests; the sheet had **no caller** until 2026-08-12 (see the plan's §0.3) |
| D14 upstream switch | `goose/auth.go` `setActiveUpstream`, `kilo/upstream.go`, `opencode/upstream.go` | goose switch tests; OpenCode's half was missing until 2026-08-12 (see the plan's §0.3) |
| D15 live-tagged tests | `live_kilo`, `live_opencode`, `live_codex` files | run below |
| **D16 catalog** | `provider/auth.go` `AuthCataloger`, `httpagent/authcatalog.go`, `ws/server.go` `handleAuthCatalog`, `upstream_catalog_sheet.dart` | Go + widget tests; live counts below |
| **D17 OpenCode engine writes** | `opencode/auth.go` `SetCredential`/`ClearCredential`; fallback in `httpagent/httpagent.go` | `TestSetCredentialUsesEngineAPI`, live round-trip test |
| **D18 goose table + file store** | `goose/catalog.go`, `credstore.SetGooseSecret`, `credstore.ErrGooseKeyringManaged` | `TestSetCredentialWritesGooseSecretStore`, `TestSetCredentialRefusesWhenKeyringManaged` |
| **D19 codex/grok single-vendor** | `codex/auth.go:17`, `grok/auth.go:15` | documented constants; no catalog implementation, by decision |

### 14.2 Live counts (2026-08-12)

```text
go test -tags live_opencode ./internal/provider/opencode/ -run TestLiveOpenCodeAuthCatalog -v
  catalog: 184 upstreams (184 on opencode 1.18.16)
  one page of 100 upstreams is 6035 bytes
  status: 14 upstreams, active "opencode"

go test -tags live_kilo ./internal/provider/kilo/ -run TestLiveKiloAuthCatalog -v
  live catalog: 16 upstreams, api=11 device=7 browser=3, 8 methods with inputs
  catalog: 185 upstreams (185 on kilo 7.4.21)
```

The status/catalog gap those numbers show — 14 against 184, 16 against 185 — is the whole argument for D16 in one line.

### 14.3 Three gaps this verification found, and fixed

Reading the code rather than the commit messages turned up three places where a
decision was recorded as done while the path a user takes did not exist:

1. **Device sign-in was unreachable from the phone.** `device_flow_sheet.dart`
   and its widget tests were in the tree, `startProviderDeviceAuth` was in the
   client, and the daemon side was complete — but nothing constructed the sheet,
   nothing called the client method, and the setup sheet answered "Device
   sign-in is not available yet". Every device flow the daemon supports (Kilo
   Gateway, ChatGPT via codex, xAI via grok) was dead UI. Now wired, with D8's
   destructive confirmation in front of the codex flow.
2. **The phone dropped every server-pushed auth frame.** The client's read loop
   routed request replies and `event`; `provider.auth_status`,
   `oauth.device_flow` and `oauth.device_flow_result` fell through to a debug
   log. D10's "status is pushed to all devices" therefore had no observable
   effect. Now routed to three streams, with the settings screen refreshing off
   the first.
3. **OpenCode's active-upstream switch never existed.** `httpagent.Provider`
   declares `SetActiveUpstream` for every dialect, so the type assertion in the
   WS handler succeeded and the call returned `ErrAuthUnsupported` at runtime —
   a gap no compile check could catch. Implemented, mirroring kilo.

All three are the same class of error: an optional interface satisfied
structurally while the behaviour behind it is absent.
`internal/provider/auth_conformance_test.go` now pins the type side, and the
Dart push test pins the frame routing.

### 14.4 Known gaps, stated plainly

* **Corrected by 0086:** engine `PUT /auth` 2xx is not completion; kilo-via-opencode `:api` is not a real method. See [0086-MADR-phone-provider-auth-completion.md](0086-MADR-phone-provider-auth-completion.md).
* **W3 (browser loopback) is not built.** GitLab, Snowflake, DigitalOcean and OpenAI's browser flow are listed but disabled.
* **Goose writes need `GOOSE_DISABLE_KEYRING`.** On a keyring-backed host the phone reports why and changes nothing (D18).
* **Goose's catalog is a pin, not a read.** A vendor added in a goose release newer than 1.46.0 will not appear until the table is refreshed; a vendor already configured on the host always appears regardless.
* **Codex third-party vendors stay operator-configured** (D19).
* **Copilot device auth via OpenCode/Goose** — listed under W2 — is still unimplemented; the Kilo, Grok and Codex device flows are done.
* **Goose has no device flow of its own.** Its OAuth is loopback-based (`GOOSE_OAUTH_CALLBACK_PORT`), which is W3 territory; the acphttp transport it rides carries no `StartDeviceAuth` at all.

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

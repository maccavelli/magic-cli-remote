# MADR 0074 — Remote Provider Authentication from Phone

<!-- markdownlint-disable MD013 MD024 MD033 MD036 -->

| field | value |
| --- | --- |
| status | **accepted** 2026-08-10, **implemented** 2026-08-12. D1–D15 locked 2026-08-10; **D16–D19 added 2026-08-12** after re-probing all five CLIs. **Accepted amendment 2026-08-21:** §15 records the credential-loss defect and locks D20–D29; implementation is pending under approved P17–P22. W3 (loopback tunnel) remains deferred to a successor MADR. Supersedes the *research-report* form of this document (2026-08-05/06), which proposed strategies but locked nothing. |
| date | 2026-08-10, revised 2026-08-12; amendment accepted 2026-08-21 |
| deciders | @saxsmith |
| related | MADR 0015 (relay), **0019** (single-engine, engine-owned config), 0021 (OpenCode API), 0025 (Goose), 0028 (Codex), 0029 (platform), 0043 (models), **0044** (permission funnel), **0067** (iOS port), **0068** (protocol v2 capabilities), **0073** (goose quota hang), **0075** (Kilo provider — **now implemented and default-enabled**, auth landed here), **0079** (provider/model drill-down picker — same "browse a large catalog" problem, solved the same way) |
| method | Live CLI probes on this host, twice: 2026-08-10 (grok 1.0.0, opencode 1.18.11, codex-cli 0.146.0, goose 1.45.0, kilo 7.4.20) and **2026-08-12** (grok **1.0.3**, opencode **1.18.16**, codex-cli **0.147.0**, goose 1.45.0, kilo **7.4.21**). Live HTTP probes against `opencode serve` and `kilo serve` (vendor catalog, auth-method catalog, credential write round-trip in an isolated `XDG_DATA_HOME`); goose provider registry read from the vendor's own source checkout at 1.46.0; codex config schema read out of the shipped binary; codebase verification at `e4269ef` (2026-08-10), `7b07609` + working tree (2026-08-12), and `b944880` (2026-08-21). The 2026-08-21 amendment also uses isolated-home probes against installed codex-cli 0.148.0 and Grok 1.0.5, version-matched upstream Codex and grok-build source, focused Go tests, daemon logs, git history, and primary standards and implementation research. |
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
| D8 codex guard | `codex/device_auth.go` | **2026-08-12 verification was incorrect:** the code has no sidecar, and its unit tests cover only in-process restore paths; see accepted amendment §15. |
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

## 15. Accepted amendment — keep Codex and Grok device auth online with transactional credentials (2026-08-21)

**Amendment status: accepted 2026-08-21; revised 2026-08-21 (F14, see 15.11);
implemented 2026-08-21 through plan phases P17–P22, with the token-spending
live acceptance run (P22 steps 5–6) still outstanding — see 15.12.** This section records a defect found while
investigating repeated Codex CLI re-authentication on the host, then expands the
repair to Grok because both providers use the same process and WebSocket
lifecycle. It does not silently rewrite the 2026-08-10 rationale: D8 remains the
historical decision, and accepted D20-D29 supersede its mechanism. The owner
explicitly requires both device-auth flows to remain available during the
repair. The paired P17–P22 plan is approved; implementation has not begun and
starts only on explicit execution direction.

### 15.1 Context and Problem Statement

The original D8 correctly identified the upstream Codex hazard but its
implementation does not provide the durability D8 and P9 required. Installed
codex-cli 0.148.0 still deletes its active `auth.json` before device
authorization becomes useful. mcremote starts that destructive command against
the live credential store, then depends on a callback and a byte slice in daemon
memory to repair the deletion. Several ordinary lifecycle paths can discard or
outlive that callback. The result is exactly the reported symptom: the Codex
CLI's credential disappears or is replaced and the operator must authenticate
again.

Grok 1.0.5 does not delete its credential at flow start, but that difference is
not a sufficient safety boundary. Its successful device flow writes the live
`auth.json` through the same orphanable flow ownership, cancellation, shutdown,
and concurrent-writer path. It also has no mcremote-managed last-known-good
generation. A shared credential transaction is therefore the appropriate unit
of repair; two provider-specific collections of callbacks are not.

#### Facts established in this investigation

| ID | Finding | Evidence | Confidence |
| --- | --- | --- | --- |
| **F1** | codex-cli 0.148.0 remains destructive at device-flow start. | On 2026-08-21 a valid `~/.codex/auth.json` was copied to a `0700` isolated `CODEX_HOME`. `codex login --device-auth` emitted the verification URL and a redacted code; at that point the isolated `auth.json` was already absent. The real host file remained byte-identical. An earlier network-denied probe also deleted the isolated file before the first request succeeded. | Confirmed by reproduction. |
| **F2** | The implemented “sidecar” is not a sidecar. | `internal/provider/codex/device_auth.go:54-69` reads the live file into `backup []byte` and later calls `os.WriteFile`. It creates no durable file, performs no startup recovery, and does not verify a restored byte hash. No mcremote sidecar exists under this host's `~/.codex`. | Confirmed by code and filesystem inspection. |
| **F3** | The server can lose the only cleanup function after the CLI has deleted the credential. | `internal/ws/server.go:2308` starts provider auth before `deviceFlows.Add` at `:2313`. If registration hits its per-device/global cap, `:2322-2323` returns without invoking `wait`. If either response write fails at `:2326-2341`, `Finish` cancels a context but the waiter that observes it is not started until `:2344`; the function then returns and drops `wait`. In all three branches the Codex process and in-memory restore closure can be orphaned. | Confirmed by control-flow inspection; no test covers these branches. |
| **F4** | Daemon shutdown is detached from active auth flows. | `handleStartAuth` uses `context.WithoutCancel(ctx)` at `internal/ws/server.go:2307`, removing connection and server lifecycle cancellation. `CloseClients` cancels `lifeCtx`, but the auth context cannot observe it. The daemon does not cancel or drain `deviceFlows`, and the registry has no `CancelAll`. A restart therefore kills the child while also destroying the only in-memory backup. | Confirmed by code inspection. |
| **F5** | The implemented disconnect and phone-dismissal behavior is narrower than its comments and plan. | `providerauth.Registry.CancelDevice` says it is used on disconnect, but has no production caller. `DeviceFlowSheet` calls `onCancel` only from the visible Cancel button (`device_flow_sheet.dart:239-243`); `dispose` only stops a timer. Barrier tap, swipe, Back, route disposal, process loss, or app lifecycle changes do not send `oauth.cancel`. | Confirmed by repository search and widget code. |
| **F6** | Restore and success use file presence as proof of ownership and validity. | `device_auth.go:65-67` refuses to restore if any file exists, even if it is partial, corrupt, or written by a concurrent operation. `:93-97` treats any existing file after a zero exit as success. Codex API-key set and logout are not serialized with device auth. | Confirmed by code inspection. |
| **F7** | mcremote may inspect a different credential home than the CLI mutates. | `credstore.CodexAuthPath` always resolves `$HOME/.codex/auth.json` (`credstore.go:163-169`), while the spawned Codex CLI inherits `CODEX_HOME`. The current LaunchAgent does not set `CODEX_HOME`, so this is not established as the cause of this host's incidents, but the mismatch is a deterministic bug on configured hosts and in tests. | Confirmed latent defect; not active in the inspected LaunchAgent. |
| **F8** | This daemon is a demonstrated writer of the host credential. | The daemon log records successful Codex device flows at 2026-08-19 14:36:25 and 2026-08-21 06:39:10. At initial inspection, `auth.json` had matching birth and modification timestamps of 2026-08-21 06:39:10; a later `codex login status` probe refreshed its modification time. The initial observation proves that the remote flow replaced the file at that time; it does not by itself prove that every reported re-authentication was caused by the same branch. | Confirmed correlation, limited attribution. |
| **F9** | Grok shares the generic ownership and backup defect. | `internal/provider/grok/device_auth.go` starts `grok login --device-auth` through `StartCLIDeviceFlow` and returns another wait closure. It has temporary browser-suppression cleanup but no credential snapshot, durable generation, transaction journal, startup recovery, or commit conflict check. It enters the same `handleStartAuth` branches in F3-F5. | Confirmed by code inspection. |
| **F10** | Grok has the same effective-home split. | `credstore.GrokAuthPath` always returns `$HOME/.grok/auth.json`, while grok 1.0.5 resolves non-empty `GROK_HOME` before `$HOME/.grok`. The existing live test sets both variables to the same temporary path, masking disagreement. | Confirmed by mcremote and grok-build source. |
| **F11** | Upstream Codex source confirms both the destructive start and a non-atomic file writer. | The official 0.148.0 `cli/src/login.rs` calls `clear_existing_auth_before_login` before device authorization. Its file backend opens `auth.json` with truncate/create, writes, and flushes without a same-directory temporary file, `fsync`, or rename. File storage is the 0.148.0 default; keyring, auto, and ephemeral modes also exist, while this mcremote implementation only observes `auth.json`. | Confirmed against the installed version's official tag. |
| **F12** | Grok's own store already supplies coordination primitives mcremote must respect. | The installed 1.0.5 binary contains the `auth.json.lock` and refresh-coordination paths. The version-matched [grok-build commit `d92c5b0`](https://github.com/xai-org/grok-build/commit/d92c5b0b8582fda358de1f97446aa74af44a464f) uses that sibling lock, read-modify-write under advisory locking, expiry ordering to avoid rolling a rotated token backward, owner-only permissions, temporary-file `fsync` + rename, and corrupt-file preservation. An independent mcremote rename that ignores that lock can still race a refresh writer. | Confirmed against the installed binary and version-matched upstream source. |
| **F13** | This repository already has the right low-level idioms. | `internal/fsutil.WriteFileAtomic` uses a unique same-directory temporary file, restrictive mode, optional file/directory sync, symlink refusal, and rename. `fsutil.WithLock` provides bounded advisory locking. `internal/certs` stages, validates, promotes, and repairs interrupted multi-file identity writes at startup. | Confirmed by code and tests. |
| **F14** | Codex does not merely delete the credential at flow start — it revokes it server-side first, so restoring bytes cannot restore access. | `clear_existing_auth_before_login` calls `logout_with_revoke` (`codex-rs/cli/src/login.rs:120-136`), which POSTs the stored refresh token to `/oauth/revoke` and only then removes the file (`codex-rs/login/src/auth/manager.rs:931-956`). Upstream's own test is `logout_with_revoke_revokes_refresh_token_then_removes_auth`. Revocation is skipped when no auth is stored and applies only when `auth_mode == Chatgpt`; API-key credentials are never revoked; revoke failure is a warning and deletion proceeds regardless (`codex-rs/login/src/auth/revoke.rs:55-85`). | Confirmed against the installed version's official tag. |

The shortest deterministic loss sequence is:

1. An authenticated phone requests Codex device auth and confirms the warning.
2. Codex revokes the stored refresh token server-side, then deletes the live
   `auth.json`; mcremote holds the old bytes only in RAM.
3. The socket closes before the `ok` or `oauth.device_flow` frame is queued, or
   registry admission fails.
4. `handleStartAuth` returns before starting `awaitDeviceFlow`, so no code calls
   `wait`, kills the CLI, or restores the bytes.
5. The abandoned CLI expires or the daemon restarts. The live credential remains
   absent and the in-memory backup is unrecoverable.

#### F14 changes the shape of the defect

F14 is not an additional hazard on the same axis as F1-F13; it changes what
"restore" can mean. The original D8 mechanism is not merely fragile, it is
ineffective for ChatGPT-mode Codex credentials. Even on the path where nothing
is orphaned and the in-memory restore executes perfectly, it writes back bytes
whose refresh token was revoked at step 2. The host is signed out anyway.

This is a simpler and more complete explanation of the reported symptom than
the orphaned-callback races alone, and it revises F8's attribution: **every**
remote Codex device-flow attempt that does not complete invalidates the prior
credential, not only the ones that hit F3's or F4's branches. The symptom
presents intermittently because revocation is best-effort — a network-denied or
very fast cancellation may never reach `/oauth/revoke`, and in exactly those
cases the byte restore does work.

Three design consequences follow, and the decisions below encode them:

* **Isolation must be emptiness, not copying.** An isolated pending
  `CODEX_HOME` protects the live credential only because it contains no
  credential to revoke. Seeding it with a copy of the live credential — the
  procedure this investigation used in F1 to demonstrate deletion — would make
  the child revoke the live refresh token server-side while leaving LIVE
  byte-identical. Every fingerprint and mode check would pass and the user would
  still be signed out. Filesystem isolation does not contain a network side
  effect, so D22 forbids the copy explicitly.
* **A revoking probe is a point of no return.** Any procedure that runs
  `codex logout` against a real ChatGPT credential, including one cloned into a
  scratch directory purely to verify behavior, has already revoked it before any
  fingerprint comparison can decide to roll back. Such a step cannot promise to
  leave state unchanged on failure, and zero exit does not prove revocation
  succeeded.
* **Recovery must distinguish revoked from stale.** A Codex ChatGPT generation
  captured before a device-login attempt is deterministically revoked, not
  merely possibly stale. Offering it as a recovery candidate would offer a
  restore guaranteed to fail.

The daemon-restart sequence is independent of a socket race: an established
flow is intentionally detached from `lifeCtx`, so even graceful shutdown does
not run its restore path before process memory disappears.

### 15.2 Unit and integration test assessment

The focused suite was green on 2026-08-21:

```text
go test ./internal/provider/codex ./internal/providerauth ./internal/ws
ok  github.com/maccavelli/magic-cli-remote/internal/provider/codex
ok  github.com/maccavelli/magic-cli-remote/internal/providerauth
ok  github.com/maccavelli/magic-cli-remote/internal/ws
```

That result does not exercise the failing composition:

* `codex/device_auth_test.go` covers refusal without confirmation, explicit
  in-process cancellation after the caller invokes `wait`, a clean exit with no
  credential, success, a cold host, and an unknown upstream.
* The test helper deletes the live test file, so it validates the same
  repair-after-destruction design. It does not assert that a sidecar exists,
  survive a helper-process crash, restart recovery, atomic restore, concurrent
  writers, `CODEX_HOME`, or current Codex behavior.
* `providerauth/cli_test.go` covers output parsing, scan timeout, process-group
  termination, and `Wait` cancellation in isolation.
* `grok/device_auth_test.go` covers browser suppression, Darwin sandbox argv,
  and expiry. Its live test proves an isolated cancelled start does not modify
  the real host file, but no test completes a Grok login, promotes a credential,
  preserves generations, recovers after a crash, or races a refresh writer.
* `providerauth/registry_test.go` covers limits and `CancelDevice` in isolation,
  but no WebSocket test sends `provider.start_auth`. There is no assertion that
  admission occurs before provider side effects, that response-write failure
  invokes cleanup, or that server shutdown drains flows.
* `device_flow_sheet_test.dart` labels one test “dismissing” but taps the Cancel
  button. It does not exercise barrier dismissal, swipe, Back, widget disposal,
  or app termination. `provider_detail_screen_test.dart` does not drive device
  auth end to end.
* No `live_codex` test pins the destructive login behavior or proves that the
  live credential remains unchanged through failure and cancellation. This
  contradicts the original D15 claim and P9 steps 7-9.

The gap entered in separate commits: flow registration and limits in `03d435c`,
WebSocket orchestration in `d6c52f3`, and the Codex in-memory restore closure in
`9429f5a`. Their package-local tests pass, but no integration test joins all
three ownership boundaries.

The expanded focused suite was also green before this amendment was written:

```text
go test -count=1 ./internal/provider/codex ./internal/provider/grok \
  ./internal/provider/credstore ./internal/providerauth ./internal/ws \
  ./internal/fsutil
```

Green tests establish the gap: they do not establish crash safety.

### 15.3 External research and comparable implementations

The solution is based on primary standards and mature implementations rather
than an mcremote-specific backup convention:

| Source | Relevant practice | Application here |
| --- | --- | --- |
| [RFC 8628](https://www.rfc-editor.org/rfc/rfc8628.html) | Device authorization is a pending poll followed by a terminal success, denial, expiry, or error. `authorization_pending` is normal and other terminal errors stop polling. | Pending authorization is not a credential commit. The live credential remains usable until a terminal success has produced and validated a candidate. |
| [AWS Secrets Manager rotation](https://docs.aws.amazon.com/secretsmanager/latest/userguide/rotate-secrets_lambda-functions.html) | Rotation is split into create, set, test, and finish. A candidate is `AWSPENDING`; finish moves `AWSCURRENT` and retains `AWSPREVIOUS` as the last known good version. | Use immutable candidate/current/previous generations and move labels only after validation, while keeping the prior generation. |
| [Git lockfile API](https://github.com/git/git/blob/master/lockfile.h) | Git creates a lock with `O_CREAT\|O_EXCL`, writes the replacement, commits by atomic rename, and rolls back uncommitted lockfiles. Readers see old or new content. | Serialize managed writers and express commit/rollback as an owned handle rather than a callback callers can drop. |
| [SQLite atomic commit and hot-journal recovery](https://www.sqlite.org/atomiccommit.html) | Original state is journaled and flushed before live mutation. A surviving hot journal identifies an interrupted transaction and is recovered under an exclusive lock. | Persist transaction state before promotion and reconcile it at daemon startup before accepting auth work. |
| [POSIX `rename`](https://pubs.opengroup.org/onlinepubs/9799919799/functions/rename.html) | Replacing an existing directory entry keeps either the old or new entry visible throughout the rename; cross-filesystem rename may fail with `EXDEV`. | Stage the final live-file temporary in the live directory and use one same-filesystem rename as the publication point. |
| [Google `renameio`](https://github.com/google/renameio) and [Tailscale `atomicfile`](https://github.com/tailscale/tailscale/blob/main/atomicfile/atomicfile.go) | Both use a same-directory temporary, restrictive permissions, file sync, and rename. `renameio` explicitly distinguishes visibility atomicity from power-loss durability. | Reuse and strengthen `internal/fsutil` rather than adding a new dependency; propagate directory-sync failures instead of discarding them. |
| [OAuth 2.0 Security BCP, RFC 9700 §4.14](https://www.rfc-editor.org/rfc/rfc9700.html#section-4.14) | With refresh-token rotation, each refresh invalidates the previous refresh token; reuse can revoke the active grant. | “Known good” means the most recently validated generation, not a promise that an old refresh token remains server-valid forever. Never overwrite a newer live generation with an older backup automatically. |
| [OWASP Secrets Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html) | Secret handling needs explicit creation, rotation, revocation, expiry, least-privilege access, auditing, and recovery rules across the full lifecycle. | Bound retained generations, use owner-only directories/files, never log payloads or identifying hashes, make logout purge explicit, and expose only non-secret recovery state. |
| [OpenAI Codex 0.148.0 login source](https://github.com/openai/codex/blob/rust-v0.148.0/codex-rs/cli/src/login.rs) and [storage source](https://github.com/openai/codex/blob/rust-v0.148.0/codex-rs/login/src/auth/storage.rs) | Device login clears existing auth first; the default file backend truncates in place. Other credential backends exist. | Isolation must be outside the live Codex home, and mcremote must explicitly detect the supported file-backed contract instead of assuming every configured login is `auth.json`. |

The AWS labels are an analogy, not a request to add a cloud secret manager.
mcremote remains a local, same-user daemon. The reusable ideas are staged
versions, validation before promotion, an explicit commit point, a retained
previous version, and idempotent recovery.

### 15.4 Decision Drivers

* Codex and Grok device auth remain available; the repair must not use a
  temporary feature shutdown as its rollout strategy.
* A pending, failed, cancelled, disconnected, or crashed login must never modify
  the last known-good live credential.
* Every configured, mcremote-managed file credential has a durable current
  recovery generation; after the second success it also has one previous
  generation. Candidate and historical generations are bounded, not accumulated.
* Correctness must not depend on a Go closure eventually being called.
* A daemon restart, upgrade, panic, SIGKILL, or host reboot at any instruction
  boundary must leave either the old or fully committed new credential usable.
* The CLI and mcremote resolve the same effective provider home, including
  `CODEX_HOME` and `GROK_HOME`.
* A new credential may replace the live credential only after positive success
  and a final conflict check; managed writers must be excluded, and races from
  non-cooperating external writers must be detected wherever the filesystem
  permits rather than silently treated as success.
* Credential promotion must not race provider refresh writers or permit an old
  long-lived process to write stale tokens after promotion.
* Flow admission, process ownership, cancellation, and cleanup must compose
  across provider, registry, WebSocket, phone, and daemon shutdown boundaries.
* Backup copies are secrets: owner-only access, bounded retention, no logs,
  symlink refusal, and explicit lifecycle rules are mandatory.
* Tests must reproduce failures at those boundaries, not only within each
  package's happy-path abstraction.

### 15.5 Considered Options

* **Option 1: Keep live-store login and add a durable rollback sidecar**
* **Option 2: Isolate device login and atomically replace the live file, but retain no managed generations**
* **Option 3: Keep Codex and Grok online through a shared isolated transaction with current/previous backup generations and startup recovery (chosen)**
* **Option 4: Replace both CLI flows with provider-specific RPC integrations**

### 15.6 Decision Outcome

Chosen option: **“Option 3: Keep Codex and Grok online through a shared isolated
transaction with current/previous backup generations and startup recovery”**,
because it removes the live credential from the pending phase, preserves the
existing phone flow, makes candidate validation and publication explicit, and
keeps a bounded recovery history. Failure and process death become recovery of
an mcremote-owned journal or cleanup of an isolated candidate, not credential
loss. It follows existing `fsutil`, certificate-staging, provider-error, and
daemon-path idioms instead of introducing a second secrets service.

#### Locked decisions

| ID | Decision |
| --- | --- |
| **D20** | **Keep both device-auth methods online.** Codex `openai:device` and Grok `xai:device` remain advertised and usable throughout rollout. Remove Codex's destructive confirmation once isolation is active because a pending flow no longer signs out the host. D20 supersedes D8's warning-plus-in-memory-restore mechanism, not the device-auth feature. |
| **D21** | **Use one shared credential transaction coordinator.** Add a `providerauth` transaction component created by the daemon with its effective `DataDir` and injected into Codex and Grok. It owns provider-scoped admission, immutable generations, the transaction journal, validation, promotion, recovery, and cleanup. Codex/Grok adapters supply the effective home, CLI environment, auth filename, and validator. API-key set, explicit clear/logout, device auth, and backup reconciliation take the same provider mutation lock so two mcremote paths cannot race. |
| **D22** | **Resolve one effective home and run device login in an isolated pending home.** Codex uses non-empty `CODEX_HOME`, otherwise `$HOME/.codex`; Grok uses non-empty `GROK_HOME`, otherwise `$HOME/.grok`. Status and every managed mutation use that resolver. Create a private `0700` pending home under the transaction root and run Codex with `CODEX_HOME=<pending>` or Grok with `GROK_HOME=<pending>`. Keep Grok's existing host-browser suppression. Neither pending child may read or write the live `auth.json`. **The pending home must start empty of credential material, and no live credential may ever be copied into it** (F14): Codex revokes any stored ChatGPT token server-side before deleting it, so a seeded pending home would revoke the live grant while leaving LIVE byte-identical and every fingerprint check passing. Emptiness, not the directory boundary, is what makes isolation sound. Seeding CURRENT means writing the generation store under `<DataDir>`, never the pending home. A test must assert the pending home contains no credential file at spawn. The current Codex contract is its default file-backed store; detect a configured keyring/auto/ephemeral backend and return a typed unsupported-backend error rather than claiming `auth.json` protection that does not exist. |
| **D23** | **Maintain immutable CURRENT and PREVIOUS recovery generations.** Store them under `<DataDir>/provider-auth/<provider>/`, with the directory `0700`, payload files `0600`, and a `0600` versioned manifest containing generation IDs, SHA-256 fingerprints, state, source, and timestamps but no token fields. Before the first managed mutation of an existing credential, validate and durably seed CURRENT. A successful candidate becomes CURRENT only after validation and commit; the former CURRENT becomes PREVIOUS. Retain exactly those two committed generations plus at most one PENDING transaction, so copies cannot grow without bound. This is a narrow recovery exception to D2's “no mcremote vault”: these files are never an authentication source, never leave the host, and exist only to recover the provider-native store. |
| **D24** | **Define the backup lifecycle and its limits.** Reconcile at daemon startup, before every managed mutation, after every successful managed commit, and when a provider-file watcher observes a stable newer live generation. Debounce rename/write notifications, confirm the fingerprint with two stable reads, validate the provider auth material, and accept only monotonic freshness before promoting an autonomous refresh to CURRENT/PREVIOUS under the provider lock; defer watcher reconciliation during an active transaction. Startup and pre-mutation reconciliation are mandatory fallbacks for missed events. Never replace a newer CURRENT with an older or merely parseable live file. A configured file credential must have a durable CURRENT generation before a live mutation begins; after one rotation it must retain PREVIOUS. An explicit successful logout records a tombstone, then removes pending and committed payloads because revoked tokens are no longer known-good; an unauthenticated cold host has no artificial backup. RFC 9700 refresh rotation means PREVIOUS is “last validated” rather than guaranteed server-valid forever. Distinguish two grades of doubt: a generation may be *stale* (possibly superseded by an autonomous refresh) or *known-revoked* (F14 — mcremote itself ran a Codex operation that revoked that ChatGPT grant server-side). Record the known-revoked grade in the manifest when a coordinator-initiated action revokes a generation, never offer a known-revoked generation as a recovery candidate, and never present it as `recovery_available`. Surface backup state and `recovery_available` without exposing paths, hashes, or secrets. |
| **D25** | **Validate and publish as one conditional commit.** CLI exit zero is necessary but insufficient. Require a bounded, regular, non-symlink candidate with owner-only mode and provider-specific JSON/auth-material validation; run `codex login status` in the pending Codex home and a Grok cached-token initialization probe in the pending Grok home. Before publication, quiesce mcremote-owned provider processes that could refresh the old credential; if work is active, retain the validated PENDING generation for a bounded activation retry and return `ErrAuthBusy` without another OAuth exchange. Acquire the provider's sibling `auth.json.lock`, which every mcremote mutation uses and Grok upstream already honors, recompare the live start fingerprint, and report a typed conflict if another writer won. Write a same-directory `0600` live temporary, `fsync`, rename, `fsync` the parent, and verify bytes. Strengthen `fsutil.WriteFileAtomic` so requested directory-sync errors are returned, not discarded. |
| **D26** | **Use an explicit crash-recovery state machine.** Persist and sync manifest transitions `idle → pending → committing → idle` before their associated side effects. Startup recovery runs under the provider lock before auth status or mutation. A PENDING transaction never touched live and can be retained for its bounded activation window or removed. For COMMITTING, compare live, candidate, CURRENT, and PREVIOUS fingerprints: finish the label move if live equals the validated candidate; roll back transaction metadata if live equals the old CURRENT; otherwise preserve every file and require an explicit recovery choice. Automatic restore is allowed only for an mcremote-owned hot transaction whose journal proves the expected live generation; never resurrect an externally deleted or explicitly logged-out credential, and never restore a generation the manifest marks known-revoked (D24). Where the only surviving generation is known-revoked, recovery reports that re-authentication is required rather than offering a restore that is guaranteed to fail. |
| **D27** | **Make device-flow execution an owned lifecycle.** Reserve registry capacity before provider side effects. Replace the bare wait closure with an idempotent `Wait`/`Cancel` transaction handle, start its owner goroutine before writing response frames, and cancel it on every later error. Derive it from server lifetime rather than `context.WithoutCancel`; add `CancelAll` plus bounded drain on daemon shutdown. A transient phone disconnect retains ownership only for the negotiated resume window, then cancels. Cleanup removes child processes, browser stubs, and expired pending homes but never CURRENT or PREVIOUS. |
| **D28** | **Make phone completion and cancellation truthful and idempotent.** Cancel, Back, barrier tap, swipe, route disposal, and every app lifecycle callback that can still communicate send at most one cancel request; reconnect may resume within the server window. Server expiry and shutdown own cleanup when hard process loss prevents a phone callback. Copy says the current credential remains active while the new sign-in is pending — a claim the isolated pending home makes true, and which D22's no-copy rule is what actually guarantees. Typed recovery states distinguish “a newer sign-in is required” (the only surviving generation is known-revoked, D24) from an ordinary restorable backup. A successful OAuth exchange that is waiting for an idle provider reports “ready to activate,” not failure and not completion. Typed busy, conflict, recovery-available, and unsupported-backend results remain distinguishable. |
| **D29** | **Boundary and fault-injection tests are release gates.** Add shared transaction unit tests for generation creation/rotation/retention, autonomous-refresh reconciliation, manifest compatibility, owner-only modes, symlink and size refusal, lock contention, fingerprint conflict, explicit-logout cleanup, and injected failure at every write/sync/rename/state transition. Add Codex and Grok provider tests for isolated success/failure/cancel/timeout, effective home, candidate validation, busy activation retry, and byte-identical live files on all incomplete outcomes. Add F14 revocation gates: assert the pending home holds no credential file at spawn, that no code path copies LIVE into a pending home, that a known-revoked generation is never offered for recovery or restored, and that any procedure invoking a revoking Codex subcommand against real ChatGPT credential material records its manifest transition before the invocation rather than after it. Add helper-process crash/startup recovery, WebSocket reserve/write-failure/disconnect/shutdown tests, phone dismissal tests, race tests, and isolated `live_codex`/`live_grok` probes. Assert no secret, device code, full fingerprint, child, temporary file, or unbounded generation leaks. |

#### Required test matrix

| Boundary | Primary test location | Required assertions |
| --- | --- | --- |
| Shared transaction and retention | New `internal/providerauth/transaction_test.go` | First credential seeds CURRENT; each accepted candidate is validated before publication; second commit shifts CURRENT to PREVIOUS; later commits retain exactly two committed generations; manifest upgrade is lossless; logout tombstone purges payloads. |
| Atomic filesystem behavior | `internal/fsutil/atomic_test.go` plus transaction fault tests | File and directory sync failures propagate; symlinks, oversized input, wrong modes, and cross-directory publication are refused; failure before rename leaves old LIVE; failure after rename is classified and recovered from the synced journal. |
| Autonomous provider refresh | New transaction watcher tests | Write and rename events coalesce; two stable reads are required; valid monotonic refresh advances CURRENT/PREVIOUS; partial, invalid, older, deleted, and active-transaction events do not overwrite generations; startup reconciliation covers missed events. |
| Codex adapter | `internal/provider/codex/device_auth_test.go` and `internal/provider/credstore/credstore_test.go` | Effective `CODEX_HOME` is used consistently; device auth runs only in the pending home; success validates and offers a candidate; cancel, denial, timeout, malformed output, and child failure leave LIVE byte-identical; non-file backends return the typed result. |
| Revocation containment (F14) | `internal/provider/codex/device_auth_test.go` and new transaction tests | The pending home is empty of credential material at spawn; no path copies LIVE into a pending home; a generation revoked by a coordinator action is marked known-revoked and is never restored or advertised as `recovery_available`; logout orders its durable tombstone before any revoking invocation; a warned/failed revoke still yields a deterministic recorded outcome. |
| Grok adapter | `internal/provider/grok/device_auth_test.go` and `internal/provider/credstore/credstore_test.go` | Effective `GROK_HOME` is used consistently; browser suppression remains isolated and cleaned; the native lock is honored; newer refresh expiry wins; every incomplete flow leaves LIVE byte-identical. |
| Process and registry ownership | `internal/providerauth/cli_test.go`, `registry_test.go`, and helper-process tests | Admission precedes spawn; `Wait` and `Cancel` are idempotent; process groups are reaped; capacity rejection owns no process; daemon restart recovers each persisted state transition. |
| WebSocket/server lifecycle | `internal/ws/provider_auth_test.go` and server shutdown tests | Frame-write failure, disconnect expiry, explicit cancellation, and shutdown each invoke exactly one cleanup; resumable disconnect keeps the handle only for the negotiated window; shutdown cancels and drains all flows. |
| Phone lifecycle | `apps/mobile/test/device_flow_sheet_test.dart` and `provider_detail_screen_test.dart` | Cancel, Back, barrier, swipe, route disposal, and communicable lifecycle callbacks send at most one cancel; ready-to-activate and typed recovery states render distinctly; hard process loss relies on server expiry. |
| Version-pinned live behavior | `live_codex` and `live_grok` tagged tests in the provider packages | An isolated cancelled Codex start proves upstream deletes only the pending credential; an isolated Grok probe pins its home and lock behavior; real host credentials remain byte-identical and tests never print codes or tokens. |
| Cross-package concurrency and hygiene | `go test -race`, full Go and Flutter suites | Concurrent refresh/login/logout cannot roll credentials backward; no token, device code, complete fingerprint, orphan child, stale temporary, or unbounded generation appears in output or on disk. |

This amendment deliberately does not adopt “last writer wins” from D10 for
Codex or Grok login. D10 described two complete credential writes. A 10- or
15-minute OAuth transaction racing token refresh, API-key login, logout, or a
second OAuth flow needs locking and conflict detection because blindly winning
can discard a newer refresh token. Grok's upstream code already makes the same
choice when it refuses to persist an older expiry over a newer one.

The coordination guarantee is exact for all mcremote writers and for Grok,
whose upstream writer honors the same sibling lock. Codex 0.148.0 does not honor
that lock, and ordinary advisory locks cannot exclude an unrelated process that
does not cooperate. For a separately launched Codex CLI, the coordinator
therefore narrows but cannot mathematically eliminate the interval between its
last fingerprint comparison and rename. It verifies the published bytes,
preserves all observed generations on conflict, and lets the watcher accept a
subsequent newer Codex refresh. Absolute exclusion of an external Codex writer
would require upstream lock/CAS support; the implementation must document this
residual race rather than claim a guarantee the filesystem does not provide.

#### Backup lifecycle invariant

For a configured, file-backed Codex or Grok credential managed by mcremote:

```text
LIVE       provider-native auth.json used by the CLI
CURRENT    immutable copy of the most recently validated committed generation
PREVIOUS   immutable prior CURRENT, when one exists
PENDING    isolated candidate; never used by the provider until commit
MANIFEST   no-secret labels and hot-transaction state
```

At a reconciled checkpoint, LIVE and CURRENT have the same committed
fingerprint. An autonomous provider refresh can make LIVE newer for the short,
debounced validation window; the coordinator checkpoints it but never rolls it
back to CURRENT. During a device flow, LIVE and CURRENT remain unchanged while
PENDING is created and validated. At the atomic publication point, readers see
old LIVE or new LIVE; the journal makes either outcome recoverable. PREVIOUS is
not automatically restored over an unknown newer file. Explicit logout is the
intentional transition to no live credential and no retained token payload.

### 15.7 Consequences

* Good, because Codex and Grok device auth remain available.
* Good, because no provider CLI behavior during an incomplete flow can touch
  the live credential.
* Good, because crash safety comes from isolation rather than best-effort repair
  after destructive mutation.
* Good, because CURRENT and PREVIOUS have an explicit creation, promotion,
  retention, recovery, and logout lifecycle.
* Good, because provider-specific path resolvers eliminate both home splits.
* Good, because the transaction reuses the repository's atomic-file, lock, data
  directory, typed error, and startup-recovery idioms.
* Neutral, because a successful login remains a deliberate replacement of the
  prior credential; the difference is that replacement happens at commit time.
* Neutral, because an old refresh-token backup can later be server-invalid; it
  is still valuable for local rollback but is never misrepresented as immortal.
* Bad, because each configured provider stores up to two additional owner-only
  copies of token material under mcremote's data directory.
* Bad, because safe promotion can wait when a provider has active work, though
  the validated candidate avoids repeating OAuth.
* Bad, because conditional promotion, recovery, and lifecycle integration are
  materially more code than a byte slice and wait callback.

### 15.8 Pros and Cons of the Options

#### Option 1: Keep live-store login and add a durable rollback sidecar

* Good, because it follows the original D8 and P9 text closely.
* Good, because a durable journal could recover some interrupted writes.
* Bad, because the live credential is still removed while the flow is pending.
* Bad, because recovery must distinguish old, new, partial, and concurrently
  refreshed credentials after an unclean exit; a wrong recovery can overwrite a
  newer valid token.
* Bad, because it temporarily duplicates a live secret and requires a persistent
  recovery protocol solely to undo a mutation mcremote can avoid making.

#### Option 2: Isolate device login and atomically replace the live file, but retain no managed generations

* Good, because incomplete OAuth cannot alter live credentials.
* Good, because it is smaller than the chosen option.
* Bad, because it does not meet the required known-good backup lifecycle.
* Bad, because an interrupted post-exchange commit has no durable labels or
  operator recovery generation beyond whatever the filesystem happened to keep.

#### Option 3: Keep Codex and Grok online through a shared isolated transaction with current/previous backup generations and startup recovery

* Good, because all incomplete outcomes leave the live credential untouched by
  construction.
* Good, because `CODEX_HOME` and `GROK_HOME` are demonstrated isolation
  boundaries and preserve the existing phone UX.
* Good, because it follows established pending/current/previous, lockfile,
  journal, and atomic-replace patterns.
* Good, because one implementation covers both providers and their shared
  server lifecycle.
* Bad, because it needs explicit conflict, quiesce, recovery, retention, and
  unsupported-backend behavior.

#### Option 4: Replace both CLI flows with provider-specific RPC integrations

* Good, because Codex app-server and Grok ACP expose account-related RPCs that
  could eventually remove stdout parsing.
* Bad, because the two vendors expose different RPCs and lifecycle semantics.
* Bad, because moving the call does not provide backup generations, conditional
  commit, or crash recovery by itself.
* Bad, because this tree does not implement or integration-test those paths.

### 15.9 Confirmation

The amendment is confirmed only when all of the following hold:

1. The phone can start both Codex and Grok device auth throughout rollout.
2. Repeated hashing shows live credentials byte-identical before code display,
   during polling, and after cancellation, denial, timeout, socket loss, response
   write failure, registry rejection, and forced daemon death.
3. Before either provider child starts, an existing validated live credential
   has a durable CURRENT generation and manifest; after two successes, CURRENT
   and PREVIOUS contain exactly the two expected generations.
4. Successful isolated flows validate and atomically install candidates. Codex
   login status/session and Grok cached-token initialization/session succeed.
5. Fault injection after every journal, backup, sync, rename, and label operation
   converges on startup to old committed or new committed state without loss.
6. A concurrent live-file change or busy provider preserves the live credential
   and PENDING candidate and returns the appropriate typed result.
7. `CODEX_HOME`, `GROK_HOME`, and default-home tests prove status and mutations
   address the same store; non-file Codex backends are detected honestly.
8. Explicit logout does not get undone by startup recovery and removes retained
   token payloads; unknown external deletion is never silently resurrected.
9. The full Go, race, Flutter, `live_codex`, and `live_grok` gates pass, with no
   leaked process, temporary directory, unbounded generation, device code,
   fingerprint, or credential value.

### 15.10 Required follow-up

The paired implementation amendment is complete as P17–P22 in
[`0074-PLAN-remote-provider-auth-from-phone.md`](0074-PLAN-remote-provider-auth-from-phone.md).
It records exact affected files, migration/cleanup behavior, verification
commands, acceptance criteria, rollback, and phase commits. Implementation must
follow the approved phase boundaries and begins only on explicit execution
direction; no implementation phase has begun at acceptance time.

---

### 15.11 Revision — 2026-08-21: server-side revocation (F14)

A pre-execution audit of this amendment re-verified F1-F13 against the working
tree and the installed `codex-cli 0.148.0`. All thirteen findings reproduce as
written, and every file, line, and configuration-key citation resolves to the
cited content. The audit then established one fact the original amendment did
not model, recorded above as **F14**: Codex's pre-login clear is a *revoking*
logout, not a deletion.

This does not change the chosen option. Isolation with `CODEX_HOME=<pending>`
remains correct — and F14 explains why it is correct, which the original text
left implicit. It does change four decisions and adds release gates:

| Change | Decision | Effect |
| --- | --- | --- |
| The pending home must start empty and no live credential may ever be copied into it. | D22 | Emptiness, not the directory boundary, is what makes isolation sound. A seeded pending home would revoke the live grant while every filesystem check passed. |
| Recovery state distinguishes *stale* from *known-revoked*. | D24 | A generation revoked by a coordinator action is recorded as such and never offered as a recovery candidate. |
| Recovery never restores a known-revoked generation. | D26 | Where only known-revoked generations survive, recovery reports that re-authentication is required instead of offering a restore guaranteed to fail. |
| Phone copy and typed states separate "a newer sign-in is required" from an ordinary restorable backup. | D28 | The "your current credential stays active" promise is true because of D22's no-copy rule, and the operator is never invited into a dead restore. |
| Revocation containment becomes a release gate. | D29 | Tests assert pending-home emptiness at spawn, that no path copies LIVE into a pending home, that known-revoked generations are never restored or advertised, and that revoking invocations record their manifest transition first. |

The paired plan's P18 changed in two places: step 5 now states that seeding
CURRENT means writing the generation store rather than the pending home, and
step 11 splits native logout by whether the invocation revokes. The original
clone-and-probe logout was unsound for Codex ChatGPT credentials because the
clone carries the same refresh token, so the probe destroyed the grant before
any fingerprint comparison could roll back; that path now writes its tombstone
before the invocation. P19 gains a `reauth_required` state.

F14 also revises F8's attribution. Because the pre-login revoke runs on the live
credential today, every incomplete remote Codex device flow invalidates the
prior grant, not only the ones that hit F3's or F4's orphan branches. The
original in-memory restore was therefore ineffective for ChatGPT-mode
credentials rather than merely fragile. The symptom presented intermittently
because revoke failure is only a warning upstream, so a network-denied or very
fast cancellation can still leave the byte restore working.

No other decision, boundary, phase, or acceptance criterion changed.

### 15.12 Implementation record — 2026-08-21

Plan phases P17–P22 are implemented. The defect this amendment describes is
closed in the shipping code path: Codex and Grok device logins run against a
private, empty provider home inside a credential transaction, the daemon owns
every flow from admission through terminal cleanup, and shutdown cancels and
drains before the process can exit.

| Decision | Where it landed |
| --- | --- |
| D20 keep both device flows online | Both remain advertised; Codex's destructive confirmation is gone on the isolated path only, and the phone shows it only when the daemon advertises the capability. |
| D21 shared transaction coordinator | `internal/providerauth` coordinator, adapters in each provider package. |
| D22 effective homes and isolated login | `credstore.CodexHome`/`GrokHome`; pending homes created empty and asserted empty at spawn. |
| D23 CURRENT/PREVIOUS generations | Immutable `0600` payloads, exactly two retained, verified by retention tests. |
| D24 backup lifecycle | Startup, pre-mutation, post-commit, and watcher reconciliation through one method; known-revoked grade added by 15.11. |
| D25 conditional validation and publication | Stage, provider probe, then atomic publish with byte verification under both locks. |
| D26 crash recovery | Exhaustive transition table plus helper-process kills at every boundary, run twice for idempotence. |
| D27 owned flow lifecycle | `Reservation` with one owner goroutine, one terminal result, panic recovery, `CancelAll`/`WaitAll`. |
| D28 truthful phone cancellation and state | Single idempotent cancel behind every dismissal path; `ready_to_activate` rendered as neither success nor failure. |
| D29 boundary and fault tests | Fault injection at every write/sync/rename/state transition, recursive secret-sentinel checks, race suite. |

**Not yet performed.** P22 steps 5 and 6 are the acceptance run against real
credentials on a configured host: completing one isolated login per provider,
a second rotation, one busy activation, and one explicit logout. Those spend
real tokens and mutate the operator's own credentials, so they remain for the
owner to run. The `live_codex` and `live_grok` tests that support them are
written and build-tagged; they cancel rather than complete an authorization and
assert the host credential stays byte-identical.

Until that run is recorded here, this amendment is implemented but not
acceptance-confirmed.

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

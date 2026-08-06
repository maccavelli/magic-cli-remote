# MADR 0074 — Remote Provider Authentication from Phone

| field | value |
|---|---|
| status | proposed |
| date | 2026-08-05 |
| deciders | @saxsmith |
| context | MADR 0021 (OpenCode API coverage), MADR 0025 (Goose provider), MADR 0028 (Codex provider), MADR 0029 (Provider canonicalization), MADR 0043 (Model selection) |

## 1 Problem Statement

magic-cli-remote (mcremote) manages multiple agent CLI providers — **Grok**, **OpenCode**, **Codex**, and **Goose** — each of which connects to various upstream AI model providers. Currently, every credential must be pre-configured on the headless host (via env vars, config files, or direct CLI login). 

The phone app can select providers and models, but has **zero** ability to configure or inject provider credentials. For headless machines (cloud VMs, SSH servers, homelab boxes), the initial provider setup is an SSH ceremony that contradicts the "control everything from the phone" promise.

**Goal:** enable the phone app to configure provider authentication — both API key entry and browser-based OAuth — so that a user who installs mcremote on a new machine can complete the entire setup from the phone. For OAuth flows, the phone opens the browser natively and handles the redirect callback or Device Flow challenge transparently to the headless host.

## 2 Current Architecture Assessment

### 2.1 Provider Stack

The codebase defines five provider IDs in [`provider.go`](file:///Users/saxsmith/gitrepos/go/magic-cli-remote/internal/provider/provider.go):

| Provider ID | Transport | Engine | Auth Handled By |
|---|---|---|---|
| `grok` | ACP-stdio | `acpagent` | xAI OAuth / `XAI_API_KEY` on host |
| `opencode` | HTTP | `opencode/http.go` | Provider API keys / Interactive OAuth |
| `codex` | JSON-RPC | `codex/provider.go` | OpenAI OAuth (`codex login`) / `OPENAI_API_KEY` |
| `goose` | ACP-over-HTTP | `acphttp` | Provider API keys / Keyring / Google Gemini OAuth |

### 2.2 Auth Infrastructure Today

1. **Device auth** (`auth/store.go`) — manages phone ↔ daemon pairing tokens. Unrelated to provider credentials.
2. **ACP `auth_method_id`** — the daemon invokes an ACP `Authenticate` call when the agent reports `authMethods`. Currently a static config-file string, not phone-controllable.
3. **Protocol** — `ProviderInfo` carries only `{id, ready}`. No auth-status or auth-methods metadata is exposed to the phone.

## 3 CLI Tool Provider & Auth Inventory

Based on deep technical research of the CLI tools and provider APIs, the authentication landscape is significantly more complex and capable than initially assessed.

### 3.1 Providers Supported per Agent CLI

| Agent CLI | Auth Methods & Capabilities |
|---|---|
| **Goose** (aaif-goose) | **Google Gemini OAuth**: Native browser loopback support. The callback port can be fixed via `GOOSE_OAUTH_CALLBACK_PORT`.<br>**Device Flow (RFC 8628)**: Utilized for GitHub Copilot, Kimi Code.<br>**API Keys**: Keyring storage (macOS Keychain, Secret Service) and env vars. |
| **OpenCode** | **Interactive OAuth**: `opencode auth login` initiates local browser OAuth loops. MCP servers use `opencode mcp auth`.<br>**Community Plugins**: `opencode-gemini-auth` / `opencode-claude-auth` proxy credentials into macOS Keychain.<br>**OpenCode-Go**: Strictly API Key via `/connect` (OAuth removed due to legal restrictions). |
| **Codex** | **Browser OAuth**: `codex login` uses OpenAI's internal OAuth with a localhost callback.<br>**Fallback**: `OPENAI_API_KEY` env var. |
| **Grok** | **Browser OAuth**: Native `auth.x.ai` flow caching to `~/.grok/auth.json`.<br>**Device Code Flow**: `grok login --device-auth` directly supports headless auth.<br>**SSO/OIDC**: Enterprise OIDC support. |
| **Hugging Face CLI** | **Browser OAuth & Device Code**: Natively handles browser loopback and provides a "copy-paste code" fallback for headless. Saves to `~/.cache/huggingface/token`. |

### 3.2 Upstream AI Provider OAuth Capabilities Matrix

A critical finding is that several major providers **do** support standard OAuth 2.0 (and Device Authorization Grants) for API access, contrary to standard consumer LLM assumptions.

| AI Provider | Standard API OAuth | Device Auth (RFC 8628) | PKCE / Redirect | Notes |
|---|---|---|---|---|
| **Google Gemini / Vertex** | ✅ Yes (via GCP) | ✅ Yes | ✅ Yes (Loopback ok) | Fully supports headless CLI auth. |
| **Azure OpenAI** | ✅ Yes (Entra ID) | ✅ Yes | ✅ Yes | Native support via `azure-identity` (`az login --use-device-code`). |
| **HuggingFace** | ✅ Yes | ✅ Yes | ✅ Yes (Loopback ok) | Fully supported via `huggingface_hub`. |
| **xAI** | ✅ Yes | ✅ Yes | ✅ Yes | Excellent CLI integration via Grok. |
| **OpenRouter** | ✅ Yes | ❌ No | ✅ Yes (Loopback ok) | PKCE flow is primary; users can generate keys via OAuth. |
| **OpenAI** | ❌ API Key Only | ❌ No | ❌ Restricted | No direct third-party API OAuth. (MCP/GPTs use it, but not for general API). |
| **Anthropic** | ❌ API Key Only | ❌ No | ❌ No | Strictly forbids consumer OAuth tokens in 3rd party tools. |
| **AWS Bedrock** | ❌ IAM/SigV4 | ❌ No | ❌ No | OAuth only for AgentCore external auth, not AWS API access. |

> [!IMPORTANT]
> **The OAuth landscape is highly bifurcated.** Providers tied to major cloud infrastructure (Google, Azure) or open ecosystems (HuggingFace, OpenRouter, xAI) offer robust OAuth and Device Flow. Standalone foundational models (OpenAI, Anthropic) rigidly enforce static API keys for developer access.

## 4 OAuth Remote Proxy — Technical Design

To enable headless remote setup from the phone, the daemon must proxy two distinct OAuth flows, in addition to supporting API key injection.

### Strategy A: Device Authorization Grant (RFC 8628) — *The Ideal Headless Path*

Providers that support Device Flow (Grok, Google Gemini, Azure, HuggingFace) provide the cleanest UX for remote authentication.

1. Daemon spawns the CLI with the device flow flag (e.g., `grok login --device-auth`).
2. Daemon parses the CLI stdout to extract the `verification_uri` and `user_code`.
3. Daemon sends an `oauth.device_flow` protocol message to the phone.
4. The phone app displays the code and opens the URI in the system browser.
5. The user completes authentication on the phone; the remote CLI's polling loop succeeds and stores the token locally.

**Pros:** Zero networking complexity. No redirect interception needed.

### Strategy B: Browser Callback Loopback Proxying

For CLIs that initiate a browser loopback (e.g., Goose's Google Gemini OAuth, OpenCode interactive OAuth, Codex), the CLI spawns a local server expecting a redirect to `http://localhost:<port>/callback`.

**The Solution: Reverse Port Tunneling + `GOOSE_OAUTH_CALLBACK_PORT`**

1. **Port Forcing:** For Goose, the daemon sets `GOOSE_OAUTH_CALLBACK_PORT=8484` in the CLI's environment. For others, the daemon parses the URL the CLI attempts to open (via a `BROWSER` shim script) to determine the callback port.
2. **WebSocket Tunnel:** The daemon instructs the phone to start a local listener on a random port (e.g., `127.0.0.1:9090`) and binds it to a reverse tunnel over the existing mcremote WebSocket.
3. **URL Rewriting:** The daemon sends the phone the OAuth URL, replacing the remote localhost callback URI with the phone's local listener URI.
4. **Execution:** The phone opens the system browser. The IdP redirects to the phone's local listener. The phone tunnels the HTTP GET request back to the daemon, which forwards it to the CLI's waiting localhost port.

**Pros:** Transparent to the CLI. Allows true browser-based OAuth for CLIs that lack Device Flow.
**Cons:** Requires `BROWSER` intercept shimming for CLIs that don't support explicit callback port definitions (like Goose does).

### Strategy C: Phone-Initiated API Key Entry

For OpenAI, Anthropic, OpenCode-Go, and AWS Bedrock, OAuth is unsupported or restricted. 
The phone UI will provide a secure text field to paste the API key. The daemon writes it to a `.env` file or directly to the OS keyring (for Goose).

## 5 Feasibility Assessment & Phasing

| Feature | Feasibility | Complexity | Providers Covered | Priority |
|---|---|---|---|---|
| **API key entry from phone** | ✅ Full | Low | OpenAI, Anthropic, OpenCode-Go, Bedrock, Mistral, etc. | **P0** |
| **Keyring write (Goose)** | ✅ Full | Medium | Goose-managed credentials | **P0** |
| **Device Flow OAuth** | ✅ Full | Medium | Grok, HuggingFace, Azure | **P1** |
| **Loopback Proxy (Goose)** | ✅ Full | High | Goose (Google Gemini), OpenRouter | **P1 (Leveraging `GOOSE_OAUTH_CALLBACK_PORT`)** |
| **Loopback Proxy (Codex/OpenCode)** | ⚠️ Partial | Very High | Codex, OpenCode | **P2 (Requires `BROWSER` shims)** |

## 6 Security Considerations

- **Credential Transit:** All credentials transit the TLS-encrypted WebSocket authenticated via client-key mTLS (MADR 0005).
- **Host Storage:** The daemon writes injected keys to a `0600` `.env` file under `data_dir` or delegates to the OS keyring (`libsecret`/Keychain).
- **Phone Storage:** The phone app treats credentials as write-only memory (paste → send → clear) and never caches them locally.
- **Tunnel Security:** Reverse callback tunnels are strictly short-lived, single-use, and restricted to the `/oauth_callback` path.

## 7 Protocol Changes

### 7.1 New Messages
```
provider.auth_status        → server → client   (auth status per provider)
provider.set_credential     → client → server   (API key injection)
provider.auth_methods       → server → client   (supported methods per provider)
oauth.device_flow           → server → client   (device flow: open URL + enter code)
oauth.open_browser_tunnel   → server → client   (redirect flow: URL + tunnel ID)
```

### 7.2 Extended ProviderInfo
```json
{
  "id": "goose",
  "ready": true,
  "auth_status": "configured",
  "auth_methods": [
    {"type": "api_key", "label": "API Key (Keyring)"},
    {"type": "oauth_loopback", "label": "Google Gemini OAuth"}
  ]
}
```

## 8 Phone UI Flow

1. **Provider List:** Shows status chip (configured / needs setup / error).
2. **Setup Sheet (⚙️):**
   - **API Key:** Secure text field + Save button.
   - **OAuth (Device Flow):** "Sign in with xAI" → UI displays user code and "Open Browser" button.
   - **OAuth (Browser):** "Sign in with Google" → Seamlessly opens system browser, intercepting the redirect via local socket.
3. Status dynamically updates upon success.

## 9 Conclusion

Phone-driven remote provider authentication is technically robust and addresses a severe UX friction point for headless deployments. The ecosystem offers diverse authentication paths; by combining **API Key Injection** (for legacy/static providers), **Device Flow parsing** (for modern CLIs like Grok), and **Reverse Loopback Tunneling** (for Goose/OpenCode), mcremote can offer a seamless, zero-SSH configuration experience that is currently unmatched in the agent CLI space.

# go-llmprovider-sdk provider-auth adoption assessment for magic-cli-remote

<!-- markdownlint-disable MD013 -->

Observed 2026-10-20, at `master` `cebf744a` of this repository and at `go-llmprovider-sdk` `v1.18.0`
(`d19f7a3`). The owner asked whether Magic should adopt `go-llmprovider-sdk` to provide the provider
auth that the daemon offers the phone, as a better designed, flexible, tested and hardened framework
behind a stable API, and for the technical feasibility, the pros and cons, and the immediate benefits.

This report decides nothing. Magic's standing decision is
[0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md](../decisions/0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md):
it puts no `go-llmprovider-sdk` module in `go.mod`, and says that bringing the SDK back into scope takes
a new MADR that supersedes it. Any adoption this report describes needs that MADR first.

Paths are repository-relative. `go-llmprovider-sdk` paths are marked **SDK**; a relative link cannot
reach that repository, so its records are cited by repository and filename. Nothing was run that
writes a file, and no credential, keyring or session file was opened (see Verification).

## Executive assessment

**Replacing Magic's provider-auth framework with the SDK is not feasible as asked.** The two solve
different problems:

- **The SDK authenticates its own program's calls to ten LLM vendor APIs** (OpenAI, Claude, Gemini, Grok,
  OpenCode Zen and Go, Hugging Face, Kilo, Together, Ollama). It obtains a token (an API key, a device
  or browser OAuth login), keeps it behind a caller-supplied `TokenStore`, refreshes it, and sends it
  on requests it makes itself.
- **Magic authenticates nothing for itself.** It configures the credential stores of the agent CLIs
  it supervises (`codex`, `grok`, `opencode`, `kilo`), over about 185 upstream vendors that the
  OpenCode and Kilo engines catalogue. It writes into each agent's own store (a CLI on stdin, an
  engine `PUT /auth/{id}`, a file merge), runs the CLI's own device login and parses its output, and
  never holds a vendor token. The only credential bytes it keeps are recovery backups of the Codex
  and Grok `auth.json` files.

The SDK has no path to any of Magic's stores, and declined to build one. Its
`0016-MADR-provider-auth-and-support-baseline.md` (**SDK**) measured Magic directly, adopted three of
Magic's ideas (durable writes, self-redacting values, the device-flow handle), and left out writing
vendor CLI stores, the transactional coordinator and driving agent CLIs (its D10).

**What is feasible:**

1. **Through gobble, with no SDK dependency in Magic.** gobble is built on the SDK and pins it
   (`gobble-cli` `go.mod`: `v1.17.0`).
   [0186-MADR-integrate-current-library-releases-and-prepare-gobble.md](../decisions/0186-MADR-integrate-current-library-releases-and-prepare-gobble.md)
   Amendment Q4 already plans for gobble to implement ACP `authenticate` on the SDK's `TokenStore` and
   logins, with Magic driving it through the 0074 phone flow as it drives Grok. This route gives the
   phone SDK-hardened auth for gobble's providers, and needs no MADR to supersede 0185.
2. **A shared contract, at a cost.** A transport-neutral "remote provider auth" contract in the SDK
   (method descriptors, a device-flow handle, a status vocabulary), implemented by Magic's adapters,
   is technically possible. It contradicts 0185 and the SDK's own scope decision, and it would move a
   wire contract the phone already depends on into a library with a different release cadence (see
   Option D).

**The phone already has a stable, data-driven API.** Protocol v2 carries typed methods, declared
inputs, availability, reason codes, status and a paged catalog. The phone hard-codes three method
types, two input kinds, one condition operator and a few Codex behaviours. The daemon can change its
backend without a phone release as long as it keeps that contract ("The phone's API contract" below).
The SDK, a Go library, cannot be that API. It can only be one of the backends behind it.

**This study also found defects in Magic,** whatever is decided ("Immediate benefits"). The most
important is that Codex API-key set and clear bypass the transactional coordinator that
[0074-MADR](../decisions/0074-MADR-remote-provider-auth-from-phone.md) D21 requires.

## What "provider auth" means in each codebase

| | magic-cli-remote | go-llmprovider-sdk |
| :--- | :--- | :--- |
| Who uses the credential | the agent CLI (`codex`, `grok`) or engine (`opencode serve`, `kilo serve`) | the program that imports the SDK |
| Who owns storage | each agent's own store (0074 D1, D2) | the caller, through `auth.TokenStore` (**SDK** `llmprovider/auth/tokenstore.go:171`); API keys are always the caller's |
| Vendors covered | about 185 upstreams from the engines' models.dev catalogues, plus OpenAI for Codex and xAI for Grok | 10 provider ids |
| Login performed by | the agent CLI or engine; Magic orchestrates and verifies | the SDK itself, as an OAuth client (with the Codex and Grok CLIs' public client ids for OpenAI and xAI) |
| Refresh | the agent, never Magic | the SDK, inside `OAuthSession.Token` |
| Remote UI | the phone, over protocol v2 | none; `wizard` drives a `Prompter` with no context |

## How Magic's provider auth works today

### Providers and methods

Magic registers `fake`, `grok`, `opencode`, `codex` and `kilo` (`internal/provider/provider.go:64-72`).
Goose was removed under `0160-MADR-remove-goose-cli-support.md`; gobble is not registered until it
advertises an ACP auth method (0186 Q4). Each provider exposes upstreams (`provider.UpstreamAuth`,
`internal/provider/auth.go:121-127`), and each upstream exposes typed methods `api_key`,
`oauth_device` or `oauth_browser` (`internal/provider/auth.go:35-39`).

| Provider | Store | API key | Device code | Browser |
| :--- | :--- | :--- | :--- | :--- |
| codex (upstream `openai`) | Codex CLI `auth.json` under the effective `CODEX_HOME` | `codex login --with-api-key`, the key on stdin (`internal/provider/codex/auth.go:28-58`) | `codex login --device-auth` in an isolated, empty pending home, then validated and published (`internal/provider/codex/owned_device_auth.go:27-69`) | not offered |
| grok (upstream `xai`) | Grok CLI `auth.json`; key in `config.toml` under a quoted model table | daemon writes `config.toml` at 0600 (`internal/provider/credstore/write.go:197-246`) | `grok login --device-auth` in an isolated home, host browser suppressed (`internal/provider/grok/owned_device_auth.go:58-92`; 0107 D6) | advertised, always `browser_only` |
| opencode, kilo (multi-vendor) | each engine's `auth.json` | engine `PUT /auth/{id}`, then a verify-after-write ladder and a compensating `DELETE` (`internal/provider/httpagent/connected.go:198-242`); a 0600 file merge when the engine is cold | engine `POST /provider/{id}/oauth/authorize`, classified by URL shape; the daemon polls for a change in the connected set (`internal/provider/httpagent/deviceauth.go:41-172`) | `oauth_browser` when the engine's method is browser-only |

Grok also authenticates the ACP session itself with `authenticate {methodId}` for headless-safe
methods only (`internal/provider/acpagent/authselect.go:24-41`).

### Storage, transactions and status

- **Native writes** are owner-only and atomic (`internal/provider/credstore/write.go:42-74`).
- **The coordinator.** `internal/providerauth` keeps CURRENT and PREVIOUS generations of the Codex and
  Grok `auth.json` under `<DataDir>/provider-auth/<provider>/` (0700, files 0600). Its transaction API
  is `Begin`, `StageCandidate`, `ValidateCandidate`, `Commit`, `Abort`, `MarkRevoked` and
  `RecordLogout` (`internal/providerauth/transaction.go`). An fsnotify watcher promotes autonomous
  refreshes (`internal/providerauth/watch.go:50`), and an operator recovers with
  `mcremote auth-recovery`. It is about 3,600 lines of production code.
- **Status.** Codex combines the `auth.json` mode with `codex doctor --json` (0136). Grok checks the
  file or the environment. OpenCode and Kilo use the engine's connected set, with a disk fallback.
  The wire vocabulary is `configured | missing | error | quota` (`internal/provider/auth.go:27-32`).

### The phone protocol

Messages are defined in `internal/protocol/messages.go:683-960`, and error codes in
`internal/protocol/errors.go:171-200`:

- requests: `providers.list`, `provider.auth_catalog`, `provider.set_credential`,
  `provider.clear_credential`, `provider.set_active_upstream`, `provider.start_auth`, `oauth.cancel`;
- pushes: `provider.auth_status`, `oauth.device_flow`, `oauth.device_flow_update`,
  `oauth.device_flow_result`.

Methods carry declared inputs (`text` or `select`, with `when` conditions), `available` and `reason`.
The capabilities `provider_auth` and `provider_auth_transactions` sit in `auth_ok.caps`, and every
handler is gated on protocol v2 (`internal/ws/server.go:2316-2324`). Owned device flows are admitted by
a registry (at most 2 per device, 8 in all, 15 minutes each), survive a disconnect for the resume
window, and are cancelled and reaped at shutdown (`internal/ws/provider_auth_owned.go:26-99`).

### Hardening and size

- **Secrets:**
  - the secret is redacted in `SetCredentialPayload`'s `LogValue` and `String`;
  - it is at most 8 KiB, and an empty one is refused;
  - for Codex it is passed on stdin only;
  - reads that would hold keys decode only ids.
- **Flows:**
  - device logins run in isolated pending homes;
  - the Grok host browser is sandboxed;
  - flow ids cannot be enumerated.
- **Transport:** a device-token WebSocket over TLS, with leaf pinning on the phone. The relay splices
  opaque frames and holds no auth code.
- **Size:** about 10,000 lines of production auth code, and 61 test files with 337 top-level tests
  (12,878 lines), 7 of them live-tagged. The phone side is about 2,400 lines of Dart and 2,300 lines
  of tests.

## How the SDK's auth works today

| Method | Providers | Shape | Fit for a remote UI |
| :--- | :--- | :--- | :--- |
| API key (`WithAPIKey`, `StaticToken`) | every provider except Ollama | data | yes: the phone sends the key, the daemon builds the source |
| Device code (`auth.StartDeviceOAuth`, then `DeviceLogin.Wait` / `Cancel`) | openai, grok, kilo | **split**: `UserCode`, `VerificationURI` and `Expiry` are fields (**SDK** `llmprovider/auth/oauth_device.go:83-100`) | yes; this handle was taken from Magic (**SDK** 0016-MADR D6). The login state lives in process memory, so a restart loses it |
| Browser OAuth (`auth.LoginBrowserOAuth`) | openai, grok | one blocking call; a loopback listener on the host (OpenAI on fixed ports 1455/1457) | only by pasting the redirected URL back through `InputCode`, which is what Magic's W3 has not built |
| ChatGPT plan sign-in (`auth.LoginChatGPTPlan`) | openai | one blocking call, loopback | as browser, and its first sign-in needs the full callback URL |
| Vendor CLI session (`auth.VendorCLISession`) | openai (Codex `auth.json`), grok (Grok CLI `auth.json`) | read-only; never refreshes, never writes | a reader of the very files Magic writes, not a writer |

- **Storage** is the caller's `TokenStore`. The built-in `FileTokenStore` writes plaintext JSON at
  0600 in a 0700 directory, through a temp file, fsync and rename, with a cross-process refresh lock
  (**SDK** `llmprovider/auth/tokenstore_file.go:155-292`). There is no keyring, by decision.
- **Refresh** is single-flight, detached from the caller, adopts another process's rotation, and never
  overwrites a newer session (**SDK** `llmprovider/auth/oauth_session.go:131-296`).
- **Status:** there is no "is provider X authenticated" API. A caller works it out offline from
  `TokenStore.Load`, the session's fields and `auth.ValidateOAuthSession`. The only live check is a
  model listing (`catalog.List`).
- **Hardening:**
  - secret types redact themselves under `String`, `GoString`, `LogValue` and `MarshalJSON`;
  - typed errors (`ErrAuthFailure`, `ErrNotPermitted`, `ErrUnsupported`, …) have tests per flow;
  - a typed-nil token source is refused (v1.18.0);
  - `llmprovider/auth` has 89.4 % statement coverage (measured for this report) against the default
    80 % floor;
  - `apidiff` guards the v1 compatibility promise, and every tag has a changelog section and a GitHub
    Release.
- **Gaps:**
  - there are no fuzz tests on the callback and device-reply parsers;
  - the OpenAI device login has no live test;
  - the browser flows bind host ports;
  - the redaction helpers are in an internal package that Magic could not import.

## Fit, provider by provider

| Magic provider | What its auth needs | What the SDK offers | Fit |
| :--- | :--- | :--- | :--- |
| codex | a Codex-owned `auth.json` that Codex itself refreshes; `codex logout` revocation; mode detection; the coordinator's generations | an OpenAI device or browser login under Codex's client id, yielding an `OAuthSession` in the SDK's own format | **No.** Writing an SDK session into Codex's `auth.json` would make Magic author a CLI-owned format that changes with Codex releases (0.148's non-file backends, 0136's doctor schema); 0074 D1 has the CLI write its own store |
| grok | Grok CLI `auth.json` and a `config.toml` key; ACP `authenticate` | a Grok device login and a Grok API key, in the SDK's format | **No**, for the same reason; the key write is Magic-specific TOML |
| opencode, kilo | engine HTTP auth over about 185 vendors, with engine-declared inputs | 10 providers; Kilo device login | **No.** The engines run their own logins; the SDK knows none of the other vendors' auth |
| gobble (planned) | ACP `authenticate` methods that gobble advertises | gobble's own logins and `TokenStore` | **Yes**, inside gobble (0186 Q4) |

## Options

### A. Replace Magic's provider auth with the SDK

- Good, because one hardened library would own login logic.
- Bad, because the SDK cannot write any store Magic configures, and its maintainers decided not to
  (**SDK** 0016-MADR D10).
- Bad, because it covers 10 vendors against the engines' 185.
- Bad, because Magic would become an OAuth client under borrowed CLI client ids, which the vendors
  can restrict (**SDK** `llmprovider/auth/doc.go:8-11`). Today the CLIs bear that risk.
- Bad, because it supersedes 0185 and discards a coordinator built for exactly these files.

### B. Use the SDK only for direct-vendor credentials Magic would hold itself

- Neutral, because Magic has no consumer for such a token: it calls no vendor API. This becomes
  relevant only if Magic adds a direct-API provider.

### C. Reach the SDK through gobble (0186 Q4)

- Good, because Magic adds no dependency and 0185 stands.
- Good, because the phone needs no release, provided gobble's methods map to `api_key` and
  `oauth_device`.
- Good, because the SDK's split device login, durable store, refresh and typed errors serve the phone
  through gobble.
- Bad, because ACP `authenticate` is one RPC with no standard way to hand a device code and URL back
  mid-call. gobble must surface them, for example in an ACP extension notification or on stderr for
  Magic's existing CLI-output parser (`internal/providerauth/cli.go:87`). This is an open design point
  for gobble's 0019-MADR.
- Bad, because a device login held in gobble's memory dies with the gobble process, so Magic's
  restart and resume rules would need gobble to cooperate.

### D. Move a remote-auth contract into the SDK

- Good, because Magic and gobble would share one Go model of methods, inputs, device flows and
  status.
- Bad, because Magic's model is richer than anything the SDK has (inputs with conditions, reason
  codes, a paged catalog, backup states), so the SDK would be absorbing Magic's protocol, not
  offering Magic a better one.
- Bad, because the phone's contract would then change on the SDK's release cadence; R48's
  compatibility promise protects Go callers, not the wire.
- Bad, because it contradicts 0185 and the SDK's scope (an LLM provider library).

### E. Import SDK primitives piecemeal

- Neutral, because Magic already has the equivalents the SDK took from it: durable writes, redacting
  payloads, the device handle. The remaining helpers sit in SDK internal packages.

## Pros and cons of adopting the SDK for provider auth

**Pros:**

- A maintained, versioned library with a compatibility promise, changelog and Releases.
- A device login already split into start, wait and cancel, the shape a phone needs.
- A durable token store and refresh with cross-process locking, which Magic does not need for agents
  that refresh themselves, but would need for any token it held.
- Typed error kinds with per-flow tests, 89.4 % coverage in `auth`, and self-redacting secret types.
- The same auth code across Magic's and gobble's surfaces, if gobble is the route.

**Cons:**

- A category mismatch: the SDK cannot configure codex, grok, opencode or kilo, which is everything
  Magic's provider auth does today.
- It contradicts 0185 (no dependency) and the SDK's 0016-MADR D10 (no vendor CLI stores).
- Browser and plan logins bind host loopback ports and block, so they do not close Magic's W3 gap
  without the paste path.
- There is no status API and no resumable login; Magic's status and resume rules would stay Magic's.
- Borrowed CLI client ids and the ChatGPT plan terms (**SDK** 0043-MADR) would become Magic's risk if
  Magic ran the logins itself.
- The phone's contract does not get more stable. It is already versioned by capabilities and
  data-driven.

## Immediate benefits

### Of option C, once gobble advertises auth methods

- The phone can configure gobble's providers with API keys for every SDK provider, and with device
  code for OpenAI, Grok and Kilo, through the existing screens and no app release.
- Magic adds no code for the logins themselves; it maps ACP methods into `AuthMethodPayload`, as it
  does for Grok.

### Of this study, whatever is decided

These are defects or drift found while mapping Magic. Each was checked against the code at
`cebf744a`:

1. **Codex API-key set and clear bypass the coordinator.** The WebSocket handlers call
   `SetCredential` and `ClearCredential` (`internal/provider/codex/auth.go:28,61`), which run against
   the live home. The coordinated `SetCredentialCoordinated` and `CoordinatedLogout`
   (`internal/provider/codex/logout.go:64,161`) have no callers outside tests. 0074 D21 requires these
   mutations to take the provider mutation lock.
2. **The Grok credential probe never runs.** `CredentialAdapter.SetProbe`
   (`internal/provider/grok/adapter.go:112`) has no callers, so 0074 D25's cached-token probe is a
   no-op.
3. **`credstore.ZeroString`** (`internal/provider/credstore/write.go:320`) has no callers.
4. **`GET /kilo/auth-status`** is named in the `internal/provider/kilo/auth.go:44` comment and in 0083
   D3, but no code calls it.
5. **The protocol guide is behind the code.** `docs/guides/protocol-v1.md` does not mention
   `oauth.device_flow_update`, `clear_credential.method_id`, or the transactional result fields.
6. **The phone forces Codex's destructive confirmation** (`apps/mobile/lib/features/settings/provider_detail_screen.dart:579`)
   even against a transactional daemon, where 0074 D20 says the warning goes away.
7. **The phone ignores `retryable`, `backup_state` and `recovery_available`** on
   `oauth.device_flow_result`, and has no copy for `confirm_required`, `credential_failed`, `conflict`,
   `recovery_required` or `unsupported_backend` (`apps/mobile/lib/data/ws/mc_exception.dart:31-81`).

## The phone's API contract

The phone can be served by any backend that keeps all of the following:

- the message names and JSON shapes in `internal/protocol/messages.go:683-960`;
- `provider.start_auth` answered with `ok`, then an `oauth.device_flow` push carrying `provider_id`.
  The phone matches by provider with a 60 s timeout, so one flow per provider at a time;
- every method expressed as `api_key` or `oauth_device`, with `oauth_browser` shown only as disabled;
- inputs that are `text` or `select`, with `eq` conditions;
- the existing error codes, or the raw-message fallback;
- Codex's `confirm_destructive`, and its two methods sharing one credential.

There is no shared schema: the Dart models are hand-written mirrors, and only
`internal/protocol/op_timeouts.json` is shared. A generated schema from `internal/protocol` would make
this contract checkable from both sides, whichever backend serves it.

## Risks and decision points

- **0185.** Options A, B, D and E add the module to `go.mod` and need a superseding MADR. Option C
  does not.
- **ACP device-code surfacing** (option C) needs a gobble decision before Magic can register gobble.
- **Borrowed client ids.** In option A, Magic would carry the vendors' right to restrict the Codex and
  Grok public client ids.
- **Process-memory logins.** In C or D, an SDK device login does not survive its process. Magic's
  owned flows survive a phone disconnect, but not a daemon restart.

## Verification

- **Three read-only surveys:** Magic's daemon auth, the phone app's auth, and the SDK's auth surface.
  Each read source, tests and records only, and opened no credential, keyring, session or
  `*.devops.*.json` file.
- **Spot checks of the claims this report rests on:** the provider set; the v2 gate; the callers of
  the coordinated Codex functions (none outside tests); the callers of `SetProbe` (none) and
  `ZeroString` (none); `kilo/auth-status` (comment only); the guide's missing `device_flow_update`;
  the phone's Codex confirmation; 0186 Q4's text; gobble's pin.
- **Coverage,** measured with `go test -cover ./llmprovider/auth/` in `go-llmprovider-sdk`: 89.4 % of
  statements.
- **Line counts** are approximate, from the surveys.
- **Not done:** no probe ran against a live engine or vendor, and no claim about a CLI's behaviour was
  re-measured here. They come from the records that measured them (0074, 0107, 0136).

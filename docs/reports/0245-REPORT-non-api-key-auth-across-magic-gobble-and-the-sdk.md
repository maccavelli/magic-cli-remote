# Non-API-key auth across Magic, gobble and the SDK

<!-- markdownlint-disable MD004 -->

Observed 2026-11-10, at this repository's `master` `995d442b` with its uncommitted work, `gobble-cli` `main`
`d6c10a5` with the owner's uncommitted work, and `go-llmprovider-sdk` `main` `773b84c`. This report decides nothing.
It records what three read-only audits found, checks the claims its recommendations rest on, and lays out options. Where it
recommends, it says so. Every decision is left to the MADR and PLAN pairs that follow, in each of the three
repositories.

The owner asked for this because auth "has been janky and hacked together, so we might as well really fix it. … I want
to get it right." The report is planned as `docs/reports/0245-REPORT-non-api-key-auth-across-magic-gobble-and-the-sdk.md`.
The slug avoids the words `plan` and `madr`, because this repository's markdownlint globs exclude any filename that
contains them (`.markdownlint-cli2.jsonc`).

Paths are repository-relative, and each is marked **SDK**, **gobble** or **Magic** where the context leaves it open. A
relative link cannot reach another repository, so records there are cited by repository and full filename. `file:N` is a
line and `file:N-M` a range.

Associated records:

* Magic:
  [0074-MADR-remote-provider-auth-from-phone.md](../decisions/0074-MADR-remote-provider-auth-from-phone.md) and
  [0074-PLAN-remote-provider-auth-from-phone.md](../decisions/0074-PLAN-remote-provider-auth-from-phone.md);
  [0083-MADR-provider-auth-activation-and-layout-gaps.md](../decisions/0083-MADR-provider-auth-activation-and-layout-gaps.md);
  [0086-MADR-phone-provider-auth-completion.md](../decisions/0086-MADR-phone-provider-auth-completion.md);
  [0107-MADR-grok-phone-api-key-and-device-code-auth.md](../decisions/0107-MADR-grok-phone-api-key-and-device-code-auth.md);
  [0179-MADR-gobble-native-acp-provider.md](../decisions/0179-MADR-gobble-native-acp-provider.md) and
  [0179-PLAN-gobble-native-acp-provider.md](../decisions/0179-PLAN-gobble-native-acp-provider.md);
  [0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md](../decisions/0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md);
  [0208-MADR-make-the-provider-layer-agent-neutral.md](../decisions/0208-MADR-make-the-provider-layer-agent-neutral.md);
  [0202-REPORT-go-llmprovider-sdk-provider-auth-adoption-assessment.md](0202-REPORT-go-llmprovider-sdk-provider-auth-adoption-assessment.md).
* go-llmprovider-sdk: `0006-MADR-subscription-auth-for-llm-providers.md`, `0016-MADR-provider-auth-and-support-baseline.md`,
  `0043-MADR-openai-chatgpt-plan-sign-in.md`, `0057-MADR-wizard-device-code-and-paste-prompt.md`,
  `0017-REPORT-openai-sign-in-for-agents.md`.
* gobble-cli: `0005-MADR-v1-feature-scope.md`, `0016-MADR-acp-go-sdk2-v0.15.0-and-stable-surface.md`,
  `0025-MADR-provider-choice-and-auth.md`, `0031-MADR-sdk-current-releases-and-adoption.md` and its PLAN,
  `0042-MADR-acpext-extension-contract-module.md`, `0045-MADR-auth-status-json.md`; issue gobble-cli#16.

## 1. Summary

**What is wrong.** Non-API-key sign-in is built three times, in three vocabularies, around one data shape: the device
code.

* **No shared model.** The SDK has six method ids, gobble has four `--method` words and a `Login` bool in acpext, Magic
  has three method types, and each agent CLI has its own flags (§3.4). None says how a method *completes* (loopback,
  paste, device code, a value, or the host only). Every consumer guesses: the SDK's `HeadlessOK` is never read, gobble
  hard-codes per-provider predicates, and Magic guesses from labels and URLs (F1, F17, F18, F34).
* **Only the device code is a session.** The SDK has a handle for device logins alone. Browser and plan sign-ins are
  blocking calls whose authorize URL is passed to a callback that may fail without anyone being told. gobble's `--json`
  cannot carry a paste or a question, and Magic's protocol cannot carry a URL without a user code, nor any answer (F6-F9).
* **So nothing browser-based completes from the phone, for any agent** (F11). This is true even where the code
  underneath accepts a pasted callback: the SDK's OpenAI and plan flows do.
* **Consent is hand-rolled in gobble's WIP and reaches no one remotely.** The terms' background-use consent is asked
  only on one terminal path, and its answer handling has bugs (F29-F32).
* **Magic carries per-agent flow logic and two parallel lifecycles.** There are label heuristics, `p.ID == "grok"`,
  `providerId == 'codex'`, regex scrapers, five status readers, and coordinator paths that are tested but not wired
  (F34-F51).

**The target shape** follows the owner's constraints:

* A small, stdlib-only **auth contract module**, nested in the SDK repository and versioned on its own. It holds the
  method description, the sign-in step, event and answer types, the error kinds, a JSON Schema, golden vectors and
  written evolution rules (§5.2-§5.3). This is the recommended option (c) of §5.2. Mirroring and full import are set out
  fairly beside it.
* The **SDK engine** in `llmprovider/auth`: one `Start` → sign-in handle (`Steps`, `Answer`, `Wait`, `Cancel`) for every
  interactive method. Its method facts are descriptor data, and the wizard renders on top of it. The refresh, lock and
  rotation code stays as it is (§5.4).
* **gobble** keeps its store, policy, notice wording and background-request gating. It emits the contract's steps from
  `--json`, and takes answers on stdin (§5.5).
* **Magic** relays: its own envelope (ids, admission, resume, cancel, host-only reasons) carries the contract's
  payloads. The daemon and the phone app ship together and cut over in one step, with no compatibility path for old
  phones (owner, 2026-11-10). Thin adapters map codex, grok, kilo and opencode into the same steps. Magic keeps one justified
  helper, a pasted-callback replay to a loopback listener, for agents that offer no paste channel (§5.6).
* **Consent** is split, option C, which is the owner's tentative lean: the facts, the step and the storage contract go in
  the SDK; the wording, the classification and the gating stay in gobble; Magic relays a generic confirm (§6).

**The order of work** (§8.3):

1. Defect fixes that stand on their own, inside the existing records of each repository, now.
2. The contract module's MADR and its first tag.
3. The SDK engine on the contract, and an SDK minor tag.
4. gobble and acpext adopt it, then tag.
5. Magic supersedes 0185 in part, pins the contract, adds the envelope and the phone UI, maps the four other agents, and
   removes its heuristics.
6. ACP-native sign-in by elicitation comes later.

The gobble WIP is best fixed, then landed, before step 4 (§8.5, Q6).

## 2. Scope, method and limits

### 2.1 What was audited

| Repository | Commit | Label | Notes |
| --- | --- | --- | --- |
| go-llmprovider-sdk | `773b84c` | HEAD | `v1.29.0` plus one docs-only commit (`git describe`: `v1.29.0-1-g773b84c`; it touches only `docs/`). The SDK audit called HEAD "tag `v1.29.0`"; the code is `v1.29.0`'s. No `llmprovider/x/` package exists yet. |
| gobble-cli | `d6c10a5` | [HEAD] | = `acpext/v0.4.0`; the root's last tag is `v0.5.0` (`8371b96`). |
| gobble-cli | working tree | [WIP] | The owner's 0031-PLAN P4b-2, the ChatGPT plan sign-in: 39 modified files, +1010/−84. Read, never touched. |
| magic-cli-remote | `995d442b` | [HEAD] | Plus [WIP]: 0179-PLAN P4 registers gobble (`internal/provider/registered/registered.go`); staged pins move `acp-go-sdk2` to v1.35.0 and the SDK to v1.29.0; `gobble-cli/acpext` stays at v0.3.0 (`go.mod:15`, re-verified). |
| Agent CLIs | – | `--help` only | codex-cli 0.167.0, grok 1.0.77, kilo 7.8.27, opencode v2.2.6, gobble 0.5.0 (`probe_clis.log`; nothing signed in). |

### 2.2 Method

* Three audits were run, one per repository, each read-only. Each ran its own existing tests:
  * the SDK's `auth`, `wizard` and `internal/chatgpt` packages passed;
  * gobble's `go vet`, `go test` and `staticcheck -checks U1000` passed on the WIP tree for `internal/cli`,
    `llm/provider`, `internal/auth` and `acpserver`;
  * Magic's `providerauth`, `httpagent` and `protocol` packages passed.

  `git status` was identical before and after each run.
* This report merges the three. It re-read every claim that drives a recommendation, and every point where the audits
  disagree, against the code. Those claims are marked "re-verified" in Appendix A. It is not a fourth audit.

### 2.3 Constraints set during the audit

Two owner directions arrived on 2026-11-10, while this report was being written. They bind everything below.

* **Tie the phone contract to the SDK.** The owner said "this seems like it would be worth it to tie the phone contract
  to the sdk in this case, to be able to remove so much duplication, and optimize the sdk which in turn optimizes all
  consumers … consider future-proofing and enhancing extensibility and flexibility." §5.2 sets out the three options
  that follow from this.
* **Old phones do not matter.** The owner said "old phones do not matter, i am the only person with this installed." So
  the daemon and the phone app may change their protocol incompatibly, in lockstep. Nothing here keeps a compatibility
  path for an older phone. Stored credentials are still the owner's real data, and §9 gives them a one-time migration.

0185-MADR is not a barrier to the first direction. It is about model catalogs: "catalogs stay engine-backed, and no
catalog moves to the SDK's listing". Its no-dependency bullet was already superseded in part by 0235-MADR D5
(`0185-MADR-…md:13-19`, re-verified). A new Magic MADR can supersede it in part again for auth, as 0235 did.

### 2.4 Limits

* **Not run:**
  * no sign-in, no live-tagged test and no provider quota;
  * no daemon was started and no engine queried;
  * no credential file was opened, and no `*.devops.*.json` file was read;
  * no network call was made.
* **Taken from the audits, not re-read here:**
  * the text of issue gobble-cli#16, which comes from the gobble audit;
  * the OpenAI terms, which gobble's WIP records as read and pasted by the owner on 2026-11-10;
  * the kilo and opencode engine behaviour beyond Magic's fixtures and records.
* **Unverified items** are collected, each with a probe, in §12.

### 2.5 Where the audits disagree, and how it was resolved

| Topic | What was said | Resolution |
| --- | --- | --- |
| Magic's use of `acpext.ErrorData.Command` | gobble audit: Magic consumes it ("per brief; not re-verified"). Magic audit: no code decodes it. | **Magic audit is right.** The only non-test `acpext.ErrorData` use in Magic is `internal/provider/gobble/errorlimit.go:42`, which reads `Limit` and `Kind` (re-verified). |
| Elicitation as a sign-in path | gobble audit: Magic advertises form elicitation, so request-scoped elicitation during `authenticate` is a candidate. Magic audit: URL mode and request-scoped forms are answered `cancel`. | **Both are true.** `internal/provider/acpagent/elicitation.go:33-48` cancels URL mode and any form with no session (re-verified). An ACP-native sign-in would be cancelled by Magic today; it needs a Magic change under 0191-MADR (§5.5). |
| SDK HEAD | SDK audit: HEAD is tag `v1.29.0`. | HEAD is one docs-only commit past `v1.29.0` (§2.1). Nothing in the findings changes. |
| SDK exports with no known consumer | SDK audit: gobble's use of `LoginDeviceOAuth`, `NotifyDevice` and `GrokFlowFromEnv` is unverified. | gobble audit: gobble uses none of them; Magic audit: Magic uses no SDK auth at all. A third consumer, prepare-commit-msg, is named in 0043-MADR's `informed:`; its use is unverified. Deprecation stays bound by `make api-check`. |
| Unknown `--json` events | gobble audit: unverified whether Magic tolerates them. | **Magic ignores them.** `firstDeviceCode` skips every event but `device_code` and `error` (`internal/provider/gobble/auth.go:389-410`, re-verified). |
| Pi's OpenAI login client | gobble records conflict: `0005-PLAN-v1-feature-scope.md:4664-4667` against `0025-MADR-provider-choice-and-auth.md:232-233`. | **Not resolved; unverified** (§12, U9). The recommendation does not depend on it: gobble's Pi import must skip it either way. |
| Line numbers | Magic audit cites `provider_detail_screen.dart:719-723` for codex's removal copy. | The `providerId == 'codex'` test is at `:729` (re-verified). |

## 3. Current state

### 3.1 An agent's own account sign-in

"Completes today" means from the phone, through Magic. "Would need" names the smallest change, and its home.

| Agent and method | Implemented in | How Magic learns it | What Magic runs | Completes today | Would need |
| --- | --- | --- | --- | --- | --- |
| codex, ChatGPT device code | codex CLI (`codex login --device-auth`) | hard-coded `openai:device` (`internal/provider/codex/auth.go:146-152`, re-verified) | owned flow: the CLI under an empty pending `CODEX_HOME`; the code is scraped from stdout by regex; the coordinator stages, validates and publishes | yes | – |
| codex, ChatGPT browser | codex CLI (`codex login`, loopback `localhost:1455`, 0074-MADR §7.2) | not advertised | – | no | a paste channel. codex 0.167.0 has no paste flag (`probe_clis.log:4-39`), so Magic would replay the pasted URL to the CLI's own listener (§5.6.4) |
| codex, access-token paste | codex CLI (`codex login --with-access-token`, `probe_clis.log:28-30`) | not advertised | – | no | a `token_paste` method and a value flow over stdin; a mapping only |
| grok, device code | grok CLI (`grok login --device-auth`) | hard-coded `xai:device` (`internal/provider/grok/auth.go:169-174`) | owned flow under a pending `GROK_HOME`, with browser suppression: PATH stubs on Linux, `sandbox-exec` on macOS | yes | – |
| grok, browser | grok CLI (`grok login --oauth`) | hard-coded `xai:browser`, annotated `browser_only` centrally (`grok/auth.go:175-181`; `internal/ws/server.go:2388-2389`, re-verified) | nothing | no | proof that accounts.x.ai offers a pasteable fallback (unverified, §12 U1), then the replay; or grok's ACP `x.ai/auth/*` (unverified at 1.0.77, U7) |
| kilo, Kilo Gateway | kilo engine (device authorization) | engine `GET /provider/auth`, label heuristic (`internal/provider/httpagent/authcatalog.go:186-207`, re-verified) | `POST /provider/kilo/oauth/authorize`, then polling `GET /config/providers` for a change | yes | – |
| opencode, its own account | opencode (`opencode-login`, an ACP terminal method) | ACP `authMethods` | never: Magic advertises no `auth.terminal` | no | an agent-side remote path, or a "finish on the host" step with the command |
| gobble | – | – | – | – | gobble has no account of its own: it holds upstream credentials only |

### 3.2 Upstream sign-ins inside multi-provider agents

| Agent / upstream | Method | Learned from | Completes today | Notes |
| --- | --- | --- | --- | --- |
| kilo / openai | "ChatGPT Pro/Plus (browser)"; "(headless)" | engine catalog + label marker `browser` | headless yes; browser no | browser rows annotated host-only |
| kilo / xai | "SuperGrok Subscription" (browser, by a per-label marker, 0086-MADR D6); "Headless / Remote / VPS" | same | VPS yes; SuperGrok no | |
| kilo / github-copilot | device, with `deploymentType` and `enterpriseUrl` inputs | same | yes | |
| kilo / poe, snowflake-cortex | browser | same | no | |
| kilo / digitalocean, gitlab | typed `oauth_device` (no marker) | same | **offered, likely to fail at start**: 0074-MADR §6.1 lists both as loopback vendors | unverified on kilo 7.8.27 (U4) |
| opencode / any integration | `<id>:oauth:<m>`, typed by the substring `browser` (`internal/provider/opencode/acp_auth.go:365-370`, re-verified); `command` and `external` are host-only | control plane `GET /api/integration` | device yes; `mode: "code"` refused (`acp_auth.go:658-661`); browser no | a paste-code route is unverified (U6) |
| gobble / grok | device (`--method device --json`); browser | `acpext.AuthProviders()` `Login`; browser by `p.ID == "grok"` (`internal/provider/gobble/auth.go:160-171`, re-verified) | device yes, once gobble is registered; browser no (`host_oauth`) | gobble's grok browser flow is the SDK's, with the borrowed Grok CLI client |
| gobble / kilo | device, which yields a key | `Login` | yes, once registered | |
| gobble / openai | [WIP] ChatGPT plan: browser plus paste, under gobble's own client | at acpext v0.3.0, key only. At v0.4.0, Magic would offer `openai:device`, which gobble refuses | no | `--json` fails a first plan sign-in (F52) |
| gobble / openai, Codex browser or device | refused by gobble's policy (`0005-MADR-v1-feature-scope.md:1057-1058`) | – | – | |

### 3.3 What the SDK offers any consumer

| Provider | SDK method | Yields | Completes with the browser on another machine |
| --- | --- | --- | --- |
| openai | `browser_oauth` (Codex's borrowed client) | refreshable session | yes, by paste of the redirected URL or the bare code |
| openai | `device_code` (Codex's device variant) | refreshable session | yes |
| openai | `token_stdin` | an API key, or an access-only session (a JWT) | yes (a value) |
| openai | `import_vendor_cli` | read-through `~/.codex/auth.json` | only if codex is signed in on the same host |
| openai | `chatgpt_plan` (client registered for the app) | refreshable plan session | yes, by paste of the **full** URL on a first sign-in (0043-MADR A3) |
| grok | `browser_oauth` (Grok CLI's borrowed client) | refreshable session | **unverified** (SDK 0008-MADR F5: the mechanism is inferred) |
| grok | `device_code` (RFC 8628) | refreshable session | yes |
| grok | `token_stdin`, `import_vendor_cli` | key; read-through file | value; same host |
| kilo | `device_code` (Kilo's own protocol) | a non-refreshing token used as a key | yes |

Sources: SDK `providers/*/descriptor.go`, `llmprovider/auth/oauth_*.go`, `kilo_device.go`.

### 3.4 Vocabularies

Methods:

| Concept | SDK `AuthMethodID` | gobble `--method` | gobble `auth.json` and `--json` `type` | acpext | Magic method type | Agent CLI flags |
| --- | --- | --- | --- | --- | --- | --- |
| API key | `api_key` | `api-key` (or `--api-key -`) | `api_key` | `Login` false | `api_key` | codex `login --with-api-key`; kilo `PUT /auth/{id}`; opencode `connect/key` |
| Browser loopback | `browser_oauth` | `browser` | `oauth` | `Login` true | `oauth_browser` (always unavailable) | codex `login`; grok `login --oauth`; kilo `auth login -m <label>`; opencode `auth login --method <id>` |
| Device code | `device_code` | `device` (or `--device`) | `oauth`, or `api_key` for kilo | `Login` true | `oauth_device` | codex `login --device-auth`; grok `login --device-auth` / `--device-code` |
| Token paste | `token_stdin` | – | – | – | – | codex `login --with-access-token` |
| Vendor import | `import_vendor_cli` | – | – | – | – | – |
| ChatGPT plan | `chatgpt_plan` | [WIP] `chatgpt-plan` | `oauth` (with `resource`) | `Login` true (v0.4.0) | – | – |
| Host only | – | – | – | – | reasons `browser_only`, `host_oauth`, `device_unsupported`, `keyring_managed` | opencode `command`, `external` |

Events, errors and reasons:

| Concern | SDK | gobble | acpext | Magic |
| --- | --- | --- | --- | --- |
| Sign-in in progress | `DeviceLogin{UserCode, VerificationURI, Expiry}`; `wizard.DeviceCode{URL, Code, Expiry}` | `--json` events `device_code`, `browser_url`, [WIP] `notice`, `saved`, `error` | – | `oauth.device_flow`, `oauth.device_flow_update` (one value: `ready_to_activate`), `oauth.device_flow_result` |
| Failure kinds | sentinels: `ErrAuthFailure`, `ErrRateLimited`, `ErrOffline`, `ErrProviderUnavailable`, `ErrInvalidRequest`, `ErrUnsupported`, `ErrNotPermitted`, `ErrIncomplete` | `FailureKind`, 10 words (`llm/provider/list.go:107-136`) | `ErrorKind`, 14 words (`llm/kind.go:25-41`) | `authErrCode` codes; result `state`; `error_kind` from a text classifier (`agenterr.Present`) |
| Status | – | `auth status --json`: `source`, `authType`, `usable`, `reason` | – | `configured`, `missing`, `error`, `quota` (`quota` has no producer) |

### 3.5 The owner's statement, tested against the code

The owner said: "the browser auth compatible providers are grok, openai/codex, and kilo. we are adding openai/chatgpt to
gobble." Read against the code, the statement is right about the agents' own CLIs on the host, and needs four
refinements:

* **Right.** The codex CLI and the grok CLI each have a browser (loopback) login: `codex login` by default, and
  `grok login --oauth` ("Force browser (loopback) login", `probe_clis.log:217-218`). The SDK offers `browser_oauth` for
  openai and grok. gobble's WIP is adding a ChatGPT sign-in, and it is a browser sign-in.
* **kilo's own account has no browser method.** "Kilo Gateway" is device authorization in kilo's engine catalog (Magic
  fixture `internal/provider/kilo/testdata/provider-auth-7.4.20.json`), and the SDK offers device code only for kilo.
  kilo's browser methods are *upstream* sign-ins inside kilo: OpenAI ChatGPT, SuperGrok, Poe and Snowflake, and, by
  0074-MADR §6.1, DigitalOcean and GitLab.
* **Two agents are missing from the list.** opencode has the same family of upstream browser sign-ins through its control
  plane. gobble already has a grok browser sign-in at HEAD, which Magic shows as host-only.
* **gobble's "openai/chatgpt" is not codex's ChatGPT login.** It is OpenAI's "Sign in with ChatGPT for agents", under a
  client OpenAI registers for gobble by name. It has no device code (`internal/cli/auth.go:209-213` [WIP],
  re-verified), and gobble's own record forbids Codex's client id (`0005-MADR-v1-feature-scope.md:1057-1058`).
* **"Compatible" holds on the host, not on the phone.** In Magic, codex's browser login is not advertised, grok's is
  advertised but always disabled, and no browser method of any agent completes from the phone today (F11).

## 4. Findings

Each finding has a stable id. **Home** is where its fix belongs: SDK (the generic engine), Contract (the proposed auth
contract module, §5.3), gobble, acpext, Magic, or the agent upstream. **Now** says whether it is a defect worth fixing
inside the existing records regardless of the redesign: *yes*; *partly* (one part can be fixed now); or *no* (the
redesign fixes it). Evidence is kept from the audits; "re-verified" means it was re-read for this report.

### 4.1 Vocabulary fragmentation

| ID | Repo | Finding and evidence | Impact | Home | Now |
| --- | --- | --- | --- | --- | --- |
| F1 | all | **Seven vocabularies for one set of methods.** SDK `AuthMethodID` (`llmprovider/descriptor.go:8-25`, re-verified); gobble `--method` words (`llm/provider/signin.go:30-35` [WIP]); gobble's `type` (`api_key`, `oauth`); acpext's `Login` bool (`acpext/auth.go:9-12`, re-verified); Magic's three types (`internal/provider/auth.go:35-39`); Magic's method-id grammars (§4.7, F38); each CLI's flags (§3.4). | Every hop translates by hand, and each translation is a place to be wrong (F19, F35). | Contract | no |
| F2 | SDK | **The SDK's ids mix mechanism and product.** `browser_oauth` names a mechanism; `chatgpt_plan` names a product that is also browser OAuth; `device_code` covers Kilo's non-OAuth flow, whose result is an `OAuthSession`. Field names drift too: `NotifyDevice(verificationURL, userCode)`, `wizard.DeviceCode{URL, Code}` and `DeviceLogin{VerificationURI, UserCode}`, and gobble's `Expires` against the SDK's `Expiry`. | A consumer cannot tell a plan sign-in is a browser flow without knowing the product. | SDK (keep the ids; add kind and completion facts) | no |
| F3 | all | **Error kinds do not line up.** In the SDK, revocation returns the standard library's `errors.ErrUnsupported` while the flows return `llmprovider.ErrUnsupported`, so `errors.Is(revokeErr, llmprovider.ErrUnsupported)` is false (`oauth_revoke.go:61-62`, `:112-127`), and OpenAI's device time-out is a plain error (`oauth_device.go:233-235`). gobble's `FailureKind` (10 words) differs from `llm.Kind` / `acpext.ErrorKind` (14), so an offline sign-in is `other` in `--json` but `offline` in a turn. Magic classifies a legacy flow's failure by its text (`agenterr.Present(err.Error())`, `internal/ws/server.go:2706`) and flattens coordinator sentinels to `credential_failed` (F48). | A failure's kind is lost or contradicted at every hop; the phone shows generic text. | Contract (the kinds); SDK; gobble; Magic | partly: the SDK's revocation error and device time-out kind, compatibly, because `llmprovider.ErrUnsupported` wraps the standard library's |
| F4 | Magic | **Reason codes diverge, and two have no producer.** `browser_only` (central) and `host_oauth` (a bare literal in `httpagent/authcatalog.go:261`, `opencode/acp_auth.go:73`, `gobble/auth.go:169`) describe the same reality; `keyring_managed` and status `quota` have no producer (`protocol/messages.go:783`; `provider/auth.go:31`). | The phone shows two texts for one state, and keeps code for states that never occur. | Magic (host reasons stay in Magic's envelope, §5.6) | yes: remove the dead values and use one constant |
| F5 | SDK, Magic | **Identity by position.** kilo method ids are array indexes `<upstream>:<i>` (`httpagent/authcatalog.go:135-137`), so an engine that reorders its methods changes what a stored id means. The SDK placed `chatgpt_plan` last "so that every earlier method keeps its place" (`providers/openai/descriptor.go:50-51`). | A reordering upstream silently retargets a method. | Magic (kilo ids by label or engine id); Contract (stable ids are required) | no |

### 4.2 No sign-in handle or event model beyond the device code

| ID | Repo | Finding and evidence | Impact | Home | Now |
| --- | --- | --- | --- | --- | --- |
| F6 | SDK | **Only the device flow has a handle.** `StartDeviceOAuth` returns `DeviceLogin` with `Wait` and `Cancel` (`oauth_device.go:83-126`, re-verified). `LoginBrowserOAuth` and `LoginChatGPTPlan` are blocking calls with `OpenURL` and `InputCode` callbacks. `OpenURL`'s error is discarded (`oauth_loopback.go:147-151`, re-verified; `oauth_plan.go:138-142`). The two browser flows duplicate the listener, channels and select (`oauth_loopback.go:125-181`, `oauth_plan.go:123-172`). | A relay cannot hold a browser sign-in as an object, and a failure to deliver the URL is invisible. | SDK | no |
| F7 | SDK | **The wizard speaks English prose.** `Prompter` questions have no id or kind. The plan notice is a `Confirm` string (`wizard/auth.go:277-289`, re-verified), and the paste prompt is told apart only by its prompt text (`auth.go:728-733`). Only the device code has a data hook (`ShowDeviceCode`, 0057-MADR D1). | A non-terminal caller has to match strings. | SDK | no |
| F8 | gobble | **`--json` is output-only.** It cannot carry a paste request, a question, a waiting state, the browser URL's expiry, what was saved, or a cancel (gobble audit §2.4). Paste is offered only on a terminal and never with `--json` (`offersPaste`, `internal/cli/auth.go:238-241`). | A remote driver cannot finish a grok browser or ChatGPT plan sign-in. | gobble (an answer channel), with the contract's steps | no |
| F9 | Magic | **The protocol is device-code-shaped.** `oauth.device_flow` needs `user_code`; `Classify` refuses a flow without one (`providerauth/classify.go:73-85`); authorize URLs are thrown away (`httpagent/deviceauth.go:79-83`, `opencode/acp_auth.go:665-667`); there is no phone-to-daemon answer message (only `oauth.cancel`); `oauth.device_flow_update` has one value. | Paste, confirm, form and "finish on the host" cannot be expressed. | Magic envelope + Contract payloads | no |
| F10 | Magic | **Sessions cannot be listed or resumed.** `Registry.PendingResults` has no non-test caller (re-verified: `providerauth/registry.go:301`; callers only in `reservation_test.go:412`, `:427`), so a retained result is never delivered (0074-PLAN P20 step 7 half done). Legacy results go to the originating `*client` (`server.go:2712`), and are lost after a reconnect. The phone matches a start push by `provider_id` with a 60 s timeout (`provider_detail_screen.dart:609-611`), though the registry allows two flows per device. | A dropped connection or an app restart loses the outcome; two flows for one provider confuse the phone. | Magic | partly: deliver retained results now |

### 4.3 Remote completion

| ID | Repo | Finding and evidence | Impact | Home | Now |
| --- | --- | --- | --- | --- | --- |
| F11 | Magic, gobble | **No browser method completes from the phone, for any agent** (Magic audit §5). The phone also refuses every browser method whatever the daemon says: `isUsable = available && !isBrowserOAuth` (`apps/mobile/lib/data/protocol/models.dart:1522`, re-verified). | The owner's "browser auth compatible" providers are host-only from the phone. | Agents and SDK (paste channels) + Magic relay | no |
| F12 | SDK, gobble | **Paste exists but is switched off on remote paths.** The SDK's OpenAI browser flow accepts a pasted URL or a bare code, and the plan flow accepts a full URL (`oauth_loopback.go:153-168`, `:606-628`; 0043-MADR A3). The wizard's `NoPastePrompt` disables it (0057-MADR D2), and gobble sets `NoPaste` whenever there is no terminal or `--json` is given. | Capability already written is unreachable. | gobble, with the SDK handle | no |
| F13 | SDK, Magic | **Grok's browser remote completion is unverified.** accounts.x.ai delivers the code by a cross-origin fetch to the loopback (`oauth_loopback.go:474-498`); SDK 0008-MADR F5 calls the mechanism "inferred … not captured as a HAR". Whether the page shows a pasteable URL or code when that fetch fails is not known. | Grok browser cannot be designed as pasteable until it is probed; device code stays grok's phone path. | probe (§12 U1) | probe |
| F14 | SDK, agents | **Fixed loopback ports.** OpenAI and plan sign-ins bind 1455, else 1457 (`oauth_loopback.go:38`, `:360-370`). codex's own login uses 1455 (0074-MADR §7.2), and kilo's openai browser flow listens on the host too. So there are at most two concurrent OpenAI-style browser sign-ins per host, across all processes. | A second agent's sign-in fails with "registered callback ports unavailable". | SDK (expose the redirect facts), Magic (admission per port) | no |
| F15 | agents, Magic | **Engine-side paste routes are unused and unverified.** kilo's OpenAPI lists `POST /provider/{id}/oauth/callback` (`kilo/testdata/kilo-spike-7.4.23/openapi-paths.txt:188`), and Magic never calls it. opencode's `mode: "code"` attempts are refused. grok's ACP `x.ai/auth/get_url` and `submit_code` were declined by 0107-MADR D5; whether 1.0.77 still has them is unverified. | A native paste path may already exist in three agents. | agent upstream; Magic mapping | probe (U5-U7) |
| F16 | Magic | **Grok's host-browser suppression is per-OS.** It uses PATH stubs on Linux and a `sandbox-exec` deny-default profile on macOS (`grok/device_auth.go:24-40`, `:86-99`), and nothing on Windows. Whether grok opens a browser on Windows is unverified. | A phone-driven grok device sign-in may pop a browser on the Windows test host. | Magic adapter | probe (U11) |

### 4.4 Descriptor and method metadata

| ID | Repo | Finding and evidence | Impact | Home | Now |
| --- | --- | --- | --- | --- | --- |
| F17 | SDK | **`Interactive` and `HeadlessOK` are dead, and `HeadlessOK` is wrong.** No non-test code reads either field (re-verified: no hits outside tests). `HeadlessOK` is false for `browser_oauth` and `chatgpt_plan`, though both complete remotely by paste. | A consumer that trusted the field would hide working methods. | SDK (deprecate; replace with completion facts) | partly: a doc deprecation now |
| F18 | SDK, gobble, Magic | **The facts that matter are missing, so every consumer hard-codes them.** Missing: how a method completes, paste acceptance, what it yields, client identity, prerequisites (store, agent name), refreshable, revocable, consent and notice, which methods share one stored slot, the vendor-file source; and no JSON tags (`descriptor.go:29-35`, re-verified). The re-derivations: the SDK wizard's `needsStore` (`wizard/auth.go:120-129`) and `Logout`'s switch (`logout.go:42-52`); gobble's `HasLogin`, `HasKeyLogin`, `offers`, `signInWords` and its revoke rule; Magic's `Login ⇒ device` and label markers. | The same fact is decided in five places, differently. | SDK descriptors → Contract `Method` | no |
| F19 | acpext | **`AuthProvider.Login` conflates three things, and acpext's doc is false.** `Login` covers a refreshable OAuth login (grok), a device sign-in that yields a key (kilo) and a browser-only plan sign-in (openai). The doc says "its command offers both, as OpenAI's offers its ChatGPT plan sign-in and its API key" (`acpext/auth.go:3-8`, re-verified). But `gobble auth login <id>` never shows a menu (`internal/cli/auth.go:192-221`), and `gobble acp --login` never stores a key for a login provider. | Magic reads `Login` as "device code" (F35), which is wrong for openai at v0.4.0. | acpext | yes: the doc |
| F20 | SDK | **A consumer cannot name itself or pass its own identity.** `originator` and Grok's `referrer` are the hard-coded `go-llmprovider-sdk` (`oauth_loopback.go:35`, `:464`); auth requests use the SDK's default User-Agent; `wizard.Options` has no `ClientID`, `Issuer` or `HostID` (`wizard/auth.go:722-724`; `:291-292`), so `GrokFlowFromEnv` cannot reach the wizard. | gobble's plan sign-in cannot keep a host-lifetime host id (F24); vendors see every consumer as the SDK. | SDK | yes: additive options |
| F21 | SDK | **Implicit and undescribed credentials.** Eight descriptors list no methods, so their API key is implicit; `CommandToken` has no method id; Kilo's anonymous token is not described. | Consumers special-case "no methods". | SDK | no |

### 4.5 Storage and identity

| ID | Repo | Finding and evidence | Impact | Home | Now |
| --- | --- | --- | --- | --- | --- |
| F22 | SDK, gobble | **One session per provider per store.** `TokenStore` is keyed by `ProviderID` alone (`tokenstore.go:171-175`, re-verified). A plan session and a Codex session evict each other (0043-MADR D3). In gobble, an OpenAI plan session and an OpenAI key share the `openai` member and cannot coexist (`internal/auth/store.go:176-177` [WIP]). | Switching kind loses the other credential. | SDK (an optional keyed store), gobble (policy) | no |
| F23 | SDK, gobble | **Client re-registration churn.** A first plan sign-in registers a new OpenAI client (`oauth_plan.go:263-266`). `wizard.Logout` deletes the session, and gobble's `SetKey` and `Delete` drop `clientId` and `hostId` (`store.go:176-177`, `:333-344` [WIP]), so the next sign-in registers yet another client ("Each run registers a new client under the account", `0017-REPORT-openai-sign-in-for-agents.md:293-294`). | The user's ChatGPT settings fill with stale client entries. | SDK (keep the issued client id apart from the session) + gobble | no |
| F24 | SDK, gobble | **The host id belongs to a sign-in, not to the host.** `planHostID` mints a random `urn:uuid` when neither `HostID` nor `Previous` is given (`oauth_plan.go:223-238`, re-verified); the wizard passes no `HostID` (`wizard/auth.go:291-292`, re-verified). gobble's `0025-MADR-provider-choice-and-auth.md` D6 decided "a stable `HostID` in the state directory", which is not built. | OpenAI's guidance, one id per host, is not met. | SDK (`wizard.Options.HostID`, F20) + gobble (persist it) | yes |
| F25 | gobble | **A dormant key survives in an `oauth` entry** [HEAD]. `SetOAuth` does not remove `key` or `workspace` (`internal/auth/store.go:251-330`), so a Grok sign-in after a Grok key, or [WIP] a plan sign-in after an OpenAI key, leaves the old secret in the file. | An unused secret stays on disk. | gobble | yes |
| F26 | SDK, gobble | **A session's kind is inferred from strings.** The issuer and `Resource` decide the kind in seven SDK sites (`oauth_session.go:78-102`, `:359-401`, `:693-728`, `:884-901`; `oauth_revoke.go:54-63`). gobble's `Login.Plan()` re-states `PlanUsage()` without its issuer check (`llm/provider/login.go:30` [WIP]). Kilo's device token is an `OAuthSession` whose `Issuer` is an API origin (`kilo_device.go:121-122`). | Misrouting risk grows with each new kind. | SDK (an explicit, persisted kind) | no |
| F27 | SDK | **Exported mutable session fields are written outside the lock.** The wizard writes `OAuthSession` fields directly (`wizard/auth.go:358`, `:455`, `:915`, `:933`; `logout.go:44-45`), while the type says it is "safe for concurrent use". No race was shown (unverified, U14). | Possible latent race. | SDK | probe |
| F28 | Magic | **Several status readers per agent, which disagree.** codex has three (`auth.json` parse, `codex doctor --json`, a TOML scan), and its probe `codex login status` always exits 0 (quoted from 0136-MADR in `store_reality.go:59-63`). grok's aggregate counts `auth.json` as configured if the file merely exists, while its per-method reader validates it (`grok/auth.go:127-145` against `methods.go:21-33`). | A stub file shows "configured". | Magic | yes |

### 4.6 Consent and terms

| ID | Repo | Finding and evidence | Impact | Home | Now |
| --- | --- | --- | --- | --- | --- |
| F29 | SDK | **The SDK carries no consent fact.** 0043-MADR D8 keeps "consent before background use" the consumer's (`0043-MADR-…md:257-262`, re-verified). `openai.ChatGPTUsageURL` is a Go constant, not data. The wizard's plan `Confirm` is a second, separate notice (`wizard/auth.go:277-285`, re-verified). The 0017 report foresaw this: "An SDK can carry the consent as an option and expose the usage URL; it cannot meet them alone" (`0017-REPORT-openai-sign-in-for-agents.md:289-292`, re-verified). | Every plan consumer re-implements consent, and no client can learn of it in advance. | SDK + Contract (§6) | no |
| F30 | gobble | **The notice and consent are on one path only** [WIP]. `loginWith` shows `planNotice` and calls `askPlanBackground` (`internal/cli/auth.go:226-235`, re-verified). `/login`, `gobble setup` and first run call `signInTo` directly (`internal/cli/login.go:62`, re-verified), so they reach the plan sign-in with neither. 0031-PLAN deviation 1 requires the notice "before the browser opens". | A user can sign in to the plan without the disclosure the owner decided on. | gobble | yes |
| F31 | gobble | **The consent answer is handled wrongly** [WIP]. `askPlanBackground` asks at every plan sign-in, not "once" as P4b-Q2 (a) says; a "no" writes nothing, so an earlier "yes" survives a later "no" (`internal/cli/login.go:136-144`, re-verified). It is skipped silently under `--json` and without a terminal (`:130-135`). 0031-MADR says turning it off works "from the next request"; the setting is read per session (`ReadSession`). | A user's "no" may not be honoured. | gobble | yes |
| F32 | gobble, Magic | **Consent cannot be asked remotely.** gobble's consent is terminal-only (F31), and Magic has no confirm message and cancels request-scoped elicitation (F9; `acpagent/elicitation.go:44-48`, re-verified). | A phone-driven plan sign-in can never record consent; background use stays off. | Contract step + gobble + Magic relay | no |
| F33 | all | **Policy exposures nobody has decided.** The SDK's default OpenAI and Grok OAuth clients are borrowed from Codex and the Grok CLI (`auth/doc.go:8-11`, re-verified); gobble accepts the Grok one as risk and refuses Codex's. The plan's terms require "local and under the user's control" storage, and requests from "a remote runtime only that user controls" (`0017-REPORT-openai-sign-in-for-agents.md:157-163`). The owner's answer is "own host only, disclosed" (gobble 0031-MADR, WIP terms amendment). A phone-pasted callback URL carries an authorization code across Magic's link. | Exposure grows once the phone can drive browser flows. | owner decision (Q10, Q11) | decide |

### 4.7 Per-agent heuristics and parallel implementations

| ID | Repo | Finding and evidence | Impact | Home | Now |
| --- | --- | --- | --- | --- | --- |
| F34 | Magic | **Label heuristics decide browser or device.** `browserOAuthMarkers = {"browser", "external browser", "supergrok subscription"}` (`httpagent/authcatalog.go:186`, re-verified), the third a single vendor's label; opencode's `oauthType` looks for the substring `browser` in id and label (`opencode/acp_auth.go:365-370`, re-verified). DigitalOcean and GitLab are offered as device methods although 0074-MADR §6.1 lists them as loopback. | Methods are offered that fail at start, or hidden that would work. | agents declare; Magic falls back to URL classification at start only | yes: drop the label rule, decide at start |
| F35 | Magic | **gobble's adapter hard-codes ids and a wrong rule.** It uses `p.ID == "claude"` for a workspace input, `p.ID == "grok"` for a host-only browser row, and `Login ⇒ device` (`internal/provider/gobble/auth.go:151-171`, re-verified). With acpext v0.4.0, Magic would advertise `openai:device`, which gobble refuses. | A pin bump breaks openai on the phone. | acpext per-method data | partly: do not bump to v0.4.0 |
| F36 | Magic | **The phone hard-codes codex.** `destructive = providerId == 'codex'` (`provider_detail_screen.dart:581`, re-verified) shows "This signs the host out of ChatGPT immediately", while the owned flow is non-destructive (0074-MADR D20), and the sheet then says "Your current sign-in stays active" (`device_flow_sheet.dart:236-243`). The removal copy is also gated on codex (`:729`, re-verified). `confirmDestructiveSignIn` has no caller. | Two contradictory messages on one screen. | Magic, driven by method facts | yes |
| F37 | Magic | **Two parallel flow lifecycles.** Legacy `DeviceAuth` (kilo, opencode, gobble) and owned `OwnedDeviceAuth` (codex, grok) differ in admission (after against before side effects), expiry, delivery, disconnect, restart and cancel (Magic audit §2.2). | Each defect has to be fixed twice, and is fixed once (F42-F47). | Magic (one lifecycle) | no |
| F38 | Magic | **Codes are scraped and completion is inferred.** Codes are taken by regex from CLI output (codex, grok: `providerauth/cli.go:18-27`, `:87-174`) and from engine instructions (`classify.go:52-59`, `:108-125`). Completion is a process exit (codex, grok, gobble), a polled change (kilo, 5 s for 15 min) or a polled attempt status (opencode). Method-id grammars differ per adapter. | Brittle against upstream wording; each agent is different. | agents (structured output); Magic keeps a quarantined scraper | no |
| F39 | Magic | **codex's credential-store mode is parsed by hand.** There is a TOML scan (`codex/authstore.go:53-127`), and `doctor --json` detail keys are matched by prefix (`doctorreport.go:93-113`), although 0136 notes the text "carries no stability contract". | Fragile on codex upgrades. | Magic (quarantine); codex upstream | no |
| F40 | SDK | **Internal duplication and provider switches in generic code.** Two loopback implementations, three device pollers, two discovery resolvers, three revocation builders and five JWT decoders (SDK audit §7.1). Provider `switch` statements sit inside generic flow, wizard and logout code. Vendor-file path resolution lives in `wizard/import.go`, parsing in `auth/vendor_session.go`. | New providers touch many places. | SDK (behind the handle; leave refresh alone) | no |
| F41 | gobble | **Per-id predicates instead of descriptor data.** `HasLogin`, `HasKeyLogin` and `HasPiLogin` (`llm/provider/login.go:73-85` [WIP]), `offers`, `signInWords`, the revoke-iff-`HasLogin` rule, and five new `id == "openai"` checks in the WIP (`signin.go:87`, `provider.go:107`, `internal/cli/auth.go:209`, `:465`, `internal/cli/agent.go:101`). gobble's 0031-MADR D8 already decided they should "read `Descriptor.AuthMethods`" (`0031-MADR-…md:405-416`). | Each new method adds special cases. | SDK facts → one gobble policy filter | no |

### 4.8 Magic lifecycle defects

| ID | Repo | Finding and evidence | Impact | Home | Now |
| --- | --- | --- | --- | --- | --- |
| F42 | Magic | **Owned-flow expiry is wrong and not enforced.** A reservation's `expiresAt` is reserve time + 15 min (`providerauth/registry.go:255`, re-verified), and that is what the phone is told (`provider_auth_owned.go:88`). No timer enforces it. grok's code lives 10 min (0107-MADR D9), so the countdown runs 5 min long. | Users wait on a dead code. | Magic | yes |
| F43 | Magic | **gobble's device-code read has no time-out.** `firstDeviceCode` loops on `events.Scan()` with no bound (`gobble/auth.go:389-410`, re-verified), unlike `providerauth`'s 30 s scan bound. | `provider.start_auth` can hang. | Magic | yes |
| F44 | Magic | **codex's phone key write and logout bypass the coordinator.** `SetCredentialCoordinated` and `CoordinatedLogout` have no non-test caller (re-verified: `codex/logout.go:64`, `:161`; callers only in `apikey_test.go`, `logout_test.go`), although 0074-PLAN P18 step 9 and P18's logout step are logged done. Reported in 0202-REPORT ("Immediate benefits" 1) and still present. | The live store changes outside the transaction that is meant to own it. | Magic | yes (wire them, or amend 0074) |
| F45 | Magic | **grok's probe is not wired.** `CredentialAdapter.SetProbe` has no caller (re-verified: only its definition, `grok/adapter.go:112`), so 0074-PLAN P18 step 7 is unmet (0202-REPORT item 2). | D25's validation is a no-op for grok. | Magic | yes |
| F46 | Magic | **The owned grok flow is available only by accident.** `deviceAuthWired` reads the legacy `Spec.StartDeviceAuth` hook (`acpagent/acpagent.go:417-419`); remove that hook and the owned flow is annotated `device_unsupported`. | A clean-up of dead code would break grok. | Magic | yes |
| F47 | Magic | **Legacy flows lack lifecycle handling.** Nothing happens on disconnect (`CancelDevice` has no non-test caller); kilo's engine attempts are never cancelled (`httpagent/deviceauth.go`); results are lost after a reconnect. | Orphaned polls run on in the engine. | Magic (one lifecycle, F37) | partly |
| F48 | Magic | **Errors are flattened, and the phone ignores result detail.** `ErrTransactionBusy`, `ErrConflict`, `ErrUnsupportedBackend`, `ErrRecoveryRequired` and `ErrTooManyFlows` all become `credential_failed` (`server.go:2791-2815`). The phone reads only `ok` and `error` from a result (`provider_detail_screen.dart:644-649`), and `mc_exception.dart:31-81` has no copy for `confirm_required`, `credential_failed`, `unsupported` or any result state. | Users cannot tell busy from broken. | Magic | yes |
| F49 | Magic | **Recovery state never reaches the status payload.** `authStatePayload` drops `BackupState` and `RecoveryAvailable` (`server.go:2332-2348`), which reach the phone only on a device-flow result. | Recovery is invisible until the next sign-in. | Magic | yes (project them or remove them) |
| F50 | Magic | **gobble's `reason` is ignored.** Magic's status row has no `Reason` field (`gobble/auth.go:55-60`) and reads `usable` alone. gobble's WIP can report `usable: true` with `reason: "oauth_unused"` (F54). | A refused openai entry shows as configured. | Magic (and gobble F54) | yes |
| F51 | Magic | **Dead code.** codex's legacy `StartDeviceAuth` with its backup and restore is unreachable in the daemon (`codex/section.go:317-326`, `server.go:2641-2643`); `ErrAuthConfirmRequired` is returned only there; also `Registry.CancelDevice`, `credstore.ZeroString`, the phone's `confirmDestructiveSignIn`, and stale comments (`codex/provider.go:141-147`; `kilo/auth.go:41-44` names a `GET /kilo/auth-status` no code calls). | Noise, and false leads for anyone reading the code. | Magic | yes |

### 4.9 gobble WIP defects

F30, F31 and F24 above are WIP defects too. The others:

| ID | Repo | Finding and evidence | Impact | Home | Now |
| --- | --- | --- | --- | --- | --- |
| F52 | gobble | **A first plan sign-in fails under `--json` or without a terminal.** The prompter is `unasked`, and the SDK's plan `Confirm` (`wizard/auth.go:277-285`) returns `errNoTerminal` (gobble audit §2.3). No test reaches this, because `TestAuthLoginChatGPTPlan` replaces `signInWith`. | Magic or any script cannot start a plan sign-in. | gobble now (a clear usage error); later the answer channel | yes |
| F53 | gobble | **`gobble acp --login openai` lost its key path.** `acpLogin` branches on `HasLogin` before `readAPIKey` (`internal/cli/acp.go:101-106`); 0031-PLAN P4b-2 step 4 says it "offers the plan and the API key" (`0031-PLAN-…md:328`). This is an unrecorded deviation. | An ACP terminal user cannot store an OpenAI key. | gobble | yes (restore it, or record the deviation) |
| F54 | gobble | **The resolver is plan-blind, so its surfaces disagree.** For a non-plan openai `oauth` entry, the provider build refuses it (`llm/provider/provider.go:107-112`), while `auth status --json` gives `usable: true` with `reason: "oauth_unused"` (`internal/cli/auth.go:446-467`, re-verified). That breaks 0045-MADR D2, which allows `reason` only when not usable. `auth check` says `ready`, ACP `authenticate` answers `{}`, provider choice counts it, and `auth logout openai` would revoke it at OpenAI: another app's token. | A public JSON contract is broken; a foreign token could be revoked. | gobble | yes |
| F55 | gobble | **Docs and help are stale.** `docs/guides/credentials.md:208-214` says "Signing in to ChatGPT comes later"; the `AuthCmd` help says "or sign in to Grok or Kilo" (`internal/cli/auth.go:27`; also the released 0.5.0 binary, `probe_clis.log:554`); the OpenAI row of `0005-MADR-v1-feature-scope.md:168` is stale. These are P4b-2 step 8. | Users and Magic read wrong guidance. | gobble | yes |
| F56 | gobble | **Host-side behaviour under `--json`** [HEAD]. `openURL` still opens a browser on the host (`internal/cli/auth.go:251-261`), where a `--json` client is usually remote. `/login` does not open the browser while `auth login` does, and the text device code omits its expiry. | Surprise browser windows on the host. | gobble | yes |

### 4.10 Tests that encode wrong rules

| ID | Repo | Finding and evidence | Impact | Home | Now |
| --- | --- | --- | --- | --- | --- |
| F57 | Magic | `TestCatalogIsAcpextsProviders` asserts `Login ⇒ device` and grok's `host_oauth` browser row (`gobble/auth_test.go:21-52`). `TestAuthStatusSplitsBrowserFromDeviceOAuth` (`kilo/auth_test.go:136-164`) and `TestSuperGrokSubscriptionClassifiesBrowser` / `TestHeadlessVPSClassifiesDevice` (`httpagent/authcatalog_honesty_test.go:45-57`) pin labels as the rule. `TestProviderAuthInterfaceConformance` asserts the legacy `DeviceAuth` for codex and grok and has no gobble case. | The tests defend the heuristics this report removes. | Magic | yes (rewrite alongside F34, F35) |
| F58 | gobble | `TestAuthLoginChatGPTPlan` stubs `signInWith`, so F52 is unreachable; `TestAuthStatusOpenAINotAPlan` checks text only, not the JSON contradiction; `TestAcpextAuthProvidersAreTheRegistry` ties `Login` to `HasLogin \|\| HasKeyLogin`; nothing checks that each acpext `Command` and method word is accepted by the CLI (issue #16's criterion). | WIP defects pass CI. | gobble | yes |
| F59 | SDK | `providers/descriptors_test.go` pins the dead `HeadlessOK` and `Interactive` values. There is no live OpenAI device-code test, and no remote-paste test anywhere. 0043-MADR A3 lists the plan's port fallback, `id_token_hint` alone and a bare code on a first sign-in as untested. | False confidence in the facts a consumer would read. | SDK | partly |

### 4.11 Drift between records and code

| ID | Repo | Finding and evidence | Impact | Home | Now |
| --- | --- | --- | --- | --- | --- |
| F60 | Magic | 0074-MADR D3 names codex's `codex login status` and kilo's `GET /kilo/auth-status` as status sources (`0074-MADR-…md:99`); the code uses neither as described. 0074-PLAN logs P18 steps 7 and 9, the coordinated logout and P20 step 7 as done, and they are unwired (F10, F44, F45). 0074-MADR 15.12 says the destructive dialog shows "only when the daemon advertises the capability" (`:1024`), which is false (F36). 0202-REPORT items 1, 2, 4, 6 and 7 are still present. | The records claim behaviour the code lacks. | Magic | yes |
| F61 | gobble | 0031-MADR "from the next request" against the per-session read (F31); 0025-MADR D6's stable HostID, not built (F24); 0031-MADR D8, not done (F41); P4b-Q2 "asked once", against the code; the Pi OpenAI-client conflict between records (§2.5). | Same. | gobble | yes |
| F62 | SDK | `FileTokenStore.LockRefresh` says "gives up after 40 s", but `lockWait` is 60 s (`tokenstore_file.go:26` against `:240-241`). `GoString` prints `&llmprovider.` (`tokenstore.go:112`). The `RevokeOAuthSession` doc omits plan sessions. 0006-MADR §7 says vendor import uses the stored client id, and the code uses the default (`vendor_session.go:168`). | Same. | SDK | yes |

## 5. Target architecture under the owner's constraints

### 5.1 Layering

```text
+--------------------------------------------------------------------------------------------+
| auth contract module (SDK repo, nested, stdlib only, own tags)                             |
|   Method facts . Step / event kinds . Answer . ErrorKind . JSON Schema . golden vectors     |
+--------------------------------------------------------------------------------------------+
        ^ emits / embeds                ^ emits                         ^ embeds as payload
+--------------------------+   +------------------------------+   +-------------------------+
| SDK engine (auth)        |   | gobble + acpext              |   | Magic daemon + phone    |
|  Start -> SignIn handle  |-->|  store, policy, wording,     |-->|  envelope: ids,         |
|  descriptors -> facts    |   |  background gating,          |   |  admission, resume,     |
|  refresh/lock unchanged  |   |  --json steps + answers      |   |  cancel, host reasons   |
|  wizard renders on top   |   |  (later: ACP elicitation)    |   |  thin agent mappers     |
+--------------------------+   +------------------------------+   +-------------------------+
                                                                    ^ CLI output, engine HTTP
                                                     +----------------------------------------+
                                                     | codex, grok, kilo, opencode: their own |
                                                     | sign-in engines (SDK 0016-MADR D10)    |
                                                     +----------------------------------------+
```

* **The contract module** is vocabulary and data only: no flow and no HTTP.
* **The SDK engine** owns every generic flow, and every fact about a provider's methods.
* **gobble** owns what is specific to gobble (constraint 3), and leverages SDK helpers for it.
* **Magic** is a relay (constraint 1). It owns its envelope and its thin mappings for the agents the SDK does not drive.
  It carries no flow logic and no product knowledge.

### 5.2 Where the shared types live: three options

Following the owner's direction of 2026-11-10 (§2.3), the question is how the phone contract is tied to the SDK.

| | (a) Mirror | (b) Import the full SDK | (c) Hybrid: a contract module (recommended) |
| --- | --- | --- | --- |
| What | Magic defines its own wire vocabulary in `internal/protocol`, maps it to the SDK's and gobble's, and hand-writes Dart models. | The daemon imports `llmprovider/auth` (and `wizard`), and uses its types on the wire. | The SDK repository publishes a small nested module, stdlib only and versioned on its own, as gobble does with acpext. The SDK, gobble and Magic all use its types. |
| Good | Magic's wire moves only when Magic decides; no new dependency. | One set of types; direct access to the engine. | One vocabulary and one schema. Magic's types, its mapping and central annotation, and the hand-written Dart models are removed. SDK improvements reach every consumer through a pin bump. Shared conformance vectors. The SDK is forced to write rigorous evolution rules. |
| Bad | Keeps the duplication the owner wants gone (F1, F18); every SDK change is re-mapped by hand. | Heavy dependency (HTTP clients, flows, providers) in a daemon that runs no SDK sign-in itself (Magic audit: Magic uses no SDK auth). The wire would follow the SDK's v1 API, which has no wire-stability rules. Pulls flow code across 0016-MADR D10's line. | The phone's wire compatibility is governed by another repository, so the module needs a stricter promise than the SDK's v1 (no renames, additive only). Release order: a contract tag, then the Magic pin, then the app release. Risk of Magic-specific needs creeping into the SDK. Needs 0185-MADR superseded in part. |
| Records | none new for Magic | a Magic MADR superseding 0185 in part | an SDK MADR for the module; a Magic MADR superseding 0185 in part; a gobble 0042-MADR amendment if acpext references it |

**Recommendation: (c).** Its cons are bounded:

* The stricter promise is a written rule the SDK MADR can make and a CI gate can hold (§5.3.3).
* The release order is the same one gobble and Magic already follow with acpext.
* Creep is held off by the no-baggage rule applied in both directions: Magic's envelope keeps everything that is about
  phones (§5.6.1).
* Dependency weight is avoided by the stdlib-only module.

Since old phones do not matter (§2.3), the cost of a contract change to the phone is one lockstep release. The strict
evolution rules are there for the SDK's other consumers and for gobble.

### 5.3 The auth contract module (option c)

Its name and path are the SDK MADR's to choose. This report uses `authcontract`, at
`github.com/maccavelli/go-llmprovider-sdk/authcontract`, tagged `authcontract/vX.Y.Z`. The SDK has no nested module yet,
and gobble's `acpext/go.mod` is the precedent. It holds four things:

* the canonical method description;
* the sign-in step and event model;
* the answer shapes;
* the error kinds.

Each comes with JSON tags, a published JSON Schema and golden vectors.

#### 5.3.1 Types (illustrative)

```go
// Package authcontract is the shared vocabulary of non-API-key sign-in: data only, standard library only.
package authcontract

import (
    "encoding/json"
    "time"
)

// SchemaVersion counts incompatible changes, which the evolution rules forbid inside one major version.
const SchemaVersion = 1

// Features names additive capabilities a producer uses, so a reader can say what it understands.
const (
    FeatureFormStep    = "form"
    FeatureConsent     = "consent"
    FeaturePasteAnswer = "paste"
)

// Open string enums: a reader keeps an unknown value and renders it generically.
type (
    Kind           string // "api_key", "token_paste", "device_code", "browser", "vendor_import", "host_only"
    Completion     string // "value", "device", "browser_paste", "browser_loopback", "local_file", "host_command"
    Paste          string // "", "url_or_code", "url_only", "code"
    Yields         string // "api_key", "oauth_session", "access_only", "device_key", "read_through"
    ClientIdentity string // "none", "vendor_borrowed", "app_registered"
    StepKind       string // "open_url", "show_code", "paste", "confirm", "form", "waiting", "done", "failed"
    ErrorKind      string // "denied", "expired", "rate_limited", "offline", "unavailable", "invalid_request",
    //                       "unsupported", "not_permitted", "canceled", "timeout", "other"
)

// Method is one way to sign in, as data.
type Method struct {
    ID             string                     `json:"id"`
    Kind           Kind                       `json:"kind"`
    Label          string                     `json:"label"`
    Completion     Completion                 `json:"completion"`
    Paste          Paste                      `json:"paste,omitempty"`
    FirstPaste     Paste                      `json:"first_paste,omitempty"` // e.g. url_only on a plan's first sign-in
    Yields         Yields                     `json:"yields"`
    ClientIdentity ClientIdentity             `json:"client_identity"`
    Refreshable    bool                       `json:"refreshable"`
    Revocable      bool                       `json:"revocable"`
    Slot           string                     `json:"slot,omitempty"` // methods sharing one stored credential
    Consents       []Consent                  `json:"consents,omitempty"`
    Links          []Link                     `json:"links,omitempty"` // e.g. the usage page
    Meta           map[string]json.RawMessage `json:"_meta,omitempty"`
}

// Consent is a yes/no the terms require, with a stable id such as "chatgpt_plan.background_use".
type Consent struct {
    ID       string `json:"id"`
    Required bool   `json:"required"`
    Text     string `json:"text,omitempty"` // default wording; an app may supply its own
    Links    []Link `json:"links,omitempty"`
}

type Link struct {
    Rel string `json:"rel"` // "terms", "usage", "notice"
    URL string `json:"url"`
}

type Field struct {
    Key, Label, Type string // Type: "text", "secret", "select"
    Options          []string
    Required         bool
}

// Step is one thing a sign-in shows or asks.
type Step struct {
    Seq       int                        `json:"seq"`
    Kind      StepKind                   `json:"kind"`
    URL       string                     `json:"url,omitempty"`
    Code      string                     `json:"code,omitempty"`
    ExpiresAt time.Time                  `json:"expires_at,omitzero"`
    Accepts   Paste                      `json:"accepts,omitempty"`
    ConfirmID string                     `json:"confirm_id,omitempty"`
    Text      string                     `json:"text,omitempty"`
    Default   *bool                      `json:"default,omitempty"`
    Links     []Link                     `json:"links,omitempty"`
    Fields    []Field                    `json:"fields,omitempty"`
    Yields    Yields                     `json:"yields,omitempty"`
    Error     *Failure                   `json:"error,omitempty"`
    Meta      map[string]json.RawMessage `json:"_meta,omitempty"`
}

// Answer returns what a paste, confirm or form step asked for. Paste is a secret: its String and LogValue redact it.
type Answer struct {
    Seq     int               `json:"seq"`
    Paste   string            `json:"paste,omitempty"`
    Confirm *bool             `json:"confirm,omitempty"`
    Fields  map[string]string `json:"fields,omitempty"`
}

type Failure struct {
    Kind      ErrorKind `json:"kind"`
    Message   string    `json:"message"`
    Retryable bool      `json:"retryable"`
}
```

Why these step kinds:

* They are generic and composable.
* A device sign-in is `show_code` followed by `waiting`.
* A pasteable browser sign-in is `open_url` with `accepts`, then `paste`.
* The plan is a `confirm` (the notice and the consent), then `open_url`, then `paste` with `url_only`.
* kilo's Copilot inputs are a `form`.
* opencode's `mode: "code"` is a `paste` with `code`.
* A host-only method has no steps: its `Method.Completion` is `host_command` or `browser_loopback`.

#### 5.3.2 What stays out of the module

These are not contract concerns, by the owner's no-baggage rule, applied in both directions:

* transport, session ids, admission, resume, redelivery and cancel;
* host-specific availability reasons (`host_oauth`, `keyring_managed`);
* product names and app setting names (gobble's `chatgptPlanBackground`);
* any agent CLI's wire format.

#### 5.3.3 Evolution rules (for the SDK MADR to adopt)

* **Additive only within a major version.** No rename and no removal. A new field is optional. A new enum value is
  allowed, and readers must tolerate it.
* **Unknown values are tolerated and rendered generically.** An unknown step kind shows its `text` and a cancel; an
  unknown method kind is listed but not drivable. With lockstep phone releases this is good hygiene, not a phone
  requirement. It does matter for gobble and other SDK consumers, which upgrade on their own schedules.
* **`schema_version` plus a feature set.** A producer states both, and a consumer refuses a higher `schema_version`.
* **`_meta` is the extension namespace** for vendor- or app-specific data, keyed by owner (for example `_meta.gobble`).
  It is never required to complete a sign-in.
* **Gates:**
  * a JSON Schema generated from the Go types and committed;
  * golden vectors (JSON files) that every consumer's tests decode and re-encode;
  * an `apidiff`-style check on the module, stricter than the SDK's `make api-check`, which covers the root module's
    packages against `v1.*` tags (SDK audit §10), not a nested module.

### 5.4 The SDK engine on the contract

#### 5.4.1 Method facts as descriptor data

* `llmprovider.AuthMethod` gains comparable scalar fields: completion, paste, first paste, yields, client identity,
  refreshable, revocable, slot, and a consent id. It keeps no slice or map, the lesson of 0043-MADR A1. A function
  builds the contract's richer `Method` from them:

  ```go
  func Methods(d llmprovider.Descriptor) []authcontract.Method
  ```

* `Interactive` and `HeadlessOK` are deprecated in v1 (F17). The wizard's `needsStore` and `Logout` switch, and gobble's
  predicates, read the facts instead.

#### 5.4.2 One sign-in handle for every interactive method

```go
// In llmprovider/auth (or llmprovider/x/signin first, Q1).
type StartOptions struct {
    HTTPClient        *http.Client
    Store             TokenStore
    Identity          Identity // the consumer's name and version: originator, referrer, User-Agent (F20)
    ClientID, Issuer  string
    AgentName, HostID string
    Previous          *OAuthSession
    Consents          []authcontract.Consent // the app's wording for required consents
    NoLoopback        bool                   // paste only
}

type SignIn interface {
    Method() authcontract.Method
    Steps() <-chan authcontract.Step // closed after done or failed
    Answer(ctx context.Context, a authcontract.Answer) error
    Wait(ctx context.Context) (Credential, error)
    Cancel()
}

func Start(ctx context.Context, p llmprovider.ProviderID, m llmprovider.AuthMethodID, o StartOptions) (SignIn, error)
func ErrorKindOf(err error) authcontract.ErrorKind // maps the llmprovider sentinels, stably
```

* `Credential` is one sum type: an API key, a refreshable session, an access-only session, a non-refreshing device key,
  or a read-through vendor session.
* `LoginBrowserOAuth`, `StartDeviceOAuth`, `LoginDeviceOAuth` and `LoginChatGPTPlan` become wrappers. Under R48 they
  stay.
* `DeviceLogin` stays as the device case: 0016-MADR D6's handle, generalised.
* An `OpenURL` that cannot be delivered becomes an observable `failed`, not a discarded error (F6).

#### 5.4.3 What stays exactly as it is

* The refresh, lock, rotation, re-save and sibling-adoption code, about 60 tests (SDK audit §3.3, §9). It is re-dispatched
  by an explicit session kind (F26), not rewritten. Both vendors revoke a token family on reuse
  (`tokenstore.go:180-182`).
* id_token verification, PKCE, request bounds and `FileTokenStore`'s write discipline.

#### 5.4.4 The wizard on top

* `wizard` drives a `SignIn` and renders its steps through `Prompter`: `Notify` for `open_url` and `show_code`,
  `Input` / `InputContext` for `paste`, `Confirm` for `confirm`.
* An optional `Options.Driver` interface, following the `InputerContext` precedent, hands the `SignIn` to a non-terminal
  caller instead.
* `ShowDeviceCode` and `NoPastePrompt` remain, as special cases.
* New `Options` fields: `HostID`, `ClientID`, `Issuer` and `Identity` (F20, F24).

#### 5.4.5 Where it starts (Q1)

* **Recommended:** the contract module, at v0 until gobble and Magic both consume it, then v1 under §5.3.3. The engine
  API under `llmprovider/x/signin`, which is exempt from R48 and does not exist yet. Promote it into `auth` in a minor
  release once both consumers have run it.
* **Alternative:** additive in `auth` at once. This is simpler, but every early mistake is then frozen under `make
  api-check`.

### 5.5 gobble and acpext

* **gobble's `--json`** emits contract steps as JSON lines, in an envelope of gobble's own:

  ```json
  {"event": "step", "provider": "openai", "step": {"seq": 1, "kind": "confirm", "confirm_id": "chatgpt_plan.background_use"}}
  ```

  Answers come back on stdin under a new opt-in flag, for example `--answers`:

  ```json
  {"seq": 1, "confirm": true}
  ```

  Each line is one answer; closing stdin cancels. Without the flag, `--json` stays output-only, and `gobble auth login`
  keeps its exit codes. The present events `device_code`, `browser_url`, `notice`, `saved` and `error` can be kept or
  retired: Magic is the only known consumer, and it ignores unknown events (re-verified). This removes F8 and F52, and
  lets the WIP's notice and consent become steps (§6).
* **gobble's terminal surfaces** (`gobble auth login`, `/login`, setup, first run, `acp --login`) render the same steps
  through the SDK wizard, so the notice and consent are on every path by construction (F30).
* **gobble keeps** `auth.json`, credential precedence, its policy filter (for example, no `vendor_borrowed` client unless
  accepted, which replaces `HasLogin` / `HasKeyLogin` / `offers`), the notice wording, the background classification and
  gating, and the Pi import (gobble 0005-MADR, 0003-MADR, 0031-MADR D7).
* **acpext** publishes data only (gobble 0042-MADR). Two choices:
  * **(i) acpext stops publishing auth shapes** beyond the provider list, and gobble's root uses the contract module
    directly. Clients read per-method facts from `gobble auth methods --json` (a new command) or from `_meta.gobble` on
    each ACP agent method.
  * **(ii) acpext references the contract module.** This breaks acpext's stdlib-only rule (`0042-MADR-…md:142`, A1
    `:292-296`, the `check-acpext-deps` gate), so 0042-MADR needs amending.

  Recommended: (i), with the provider list kept static in acpext so a client can show providers before it connects.
  Either way, `AuthProvider` stays comparable, and `Login` is documented as "has a method other than an API key", never
  "device code".
* **ACP-native sign-in by elicitation is later.**
  * It needs request-scoped `elicitation/create` during `authenticate` (stable in acp-go-sdk2 v1.35.0, gobble audit
    §5.4).
  * It needs an amendment to gobble's 0016-MADR A2: `authenticate` is presence-only, and there is no `logout`.
  * Magic would have to accept URL mode and request-scoped forms; it cancels both today (`elicitation.go:33-48`,
    re-verified), so 0191-MADR D1 and D6 need amending.
  * Whether acp-go-sdk2 gives the handler the request id needed for the scope is unverified (U8).

  The CLI answer channel is the near-term path, because Magic already drives gobble's auth through the CLI (0179-MADR
  D3). With the same step payloads, elicitation is later a change of transport, not of model.

### 5.6 Magic

#### 5.6.1 The envelope

With option (c) and lockstep releases, this is one clean cut-over. The old provider-auth messages and types are
removed, not deprecated:

* `provider.start_auth`;
* `oauth.device_flow`, `oauth.device_flow_update`, `oauth.device_flow_result` and `oauth.cancel`;
* the three method types and the central annotation in `upstreamAuthPayload`.

```text
phone  -> daemon  provider.auth_start    {provider_id, upstream_id, method_id, inputs?}
daemon -> phone   ok                      {session_id}
daemon -> phone   provider.auth_event    {session_id, provider_id, upstream_id, step: <authcontract.Step>}
phone  -> daemon  provider.auth_answer   {session_id, answer: <authcontract.Answer>}   // paste redacted in logs
phone  -> daemon  provider.auth_cancel   {session_id}
phone  -> daemon  provider.auth_sessions {}
daemon -> phone   provider.auth_sessions_result {sessions[{session_id, provider_id, upstream_id, last_step}],
                                                 finished[{session_id, step}]}   // drains retained results
```

* In `providers.list_result`, each method is `{method: <authcontract.Method>, available, reason?, configured?}`.
* `available` and `reason` are Magic's host reasons: one vocabulary, for example `host_only`, `not_wired`, `policy`,
  `unsupported_backend`, `input_unsupported`.
* A version check at connect can refuse a mismatched daemon and app pair, if one is wanted (Q20). No capability
  negotiation is needed.
* The envelope stays Magic's. It holds:
  * the session ids;
  * admission (2 per device, 8 in all);
  * device scoping, resume-window detach and reattach, delivery to the current connection and retained results;
  * cancel and the shutdown drain;
  * the codex and grok coordinator's `ready_to_activate` and recovery states (a Magic `waiting` detail in `_meta.magic`,
    or envelope fields).
* All flows use the owned lifecycle (F37). Its fixes are in F42-F48.

#### 5.6.2 Each agent mapped into the steps, thinly

| Agent | Source Magic reads | Steps Magic emits | Magic-side logic left |
| --- | --- | --- | --- |
| gobble | `--json --answers` step lines | the same steps, passed through; answers passed back | none beyond the envelope |
| codex | device: CLI stdout. Browser: the CLI's authorize URL and its loopback. Token paste: stdin. | `show_code`, `waiting`, `done`/`failed`; browser `open_url` + `paste` (accepts `url_only`); token `form` with a secret field | the quarantined stdout scraper (F38) until codex emits structure; the loopback replay (§5.6.4) |
| grok | device: CLI stdout; browser: unverified (U1, U7) | `show_code`, `waiting`, `done`/`failed`; browser host-only until probed | the scraper; per-OS browser suppression (F16) |
| kilo | engine `/provider/auth`, `/oauth/authorize` `{url, instructions}` | the engine's method list as `Method`s, with completion from the authorize response, not the label (F34); `show_code`, or `open_url` + `paste` if `/oauth/callback` accepts a paste (U5) | the URL classification at start (0074-MADR D7, kept as the fallback) |
| opencode | control-plane integrations and attempts | `show_code`; `paste` with `code` for `mode: "code"` once its submit route is known (U6); a `form` for non-string fields; `host_command` for `command` / `external` | none beyond the mapping |

Under SDK 0016-MADR D10 ("It does not drive agent CLIs"), these mappings stay in Magic.

#### 5.6.3 The phone

* The Flutter app's auth models are generated from the module's JSON Schema. Today they are hand-written
  (`apps/mobile/lib/data/protocol/models.dart:1379-1665`), and the app has no code generator: `pubspec.yaml` lists no
  `build_runner`, `json_serializable` or `freezed` (re-verified).
* Magic's precedent is
  [0236-MADR-re-pin-codex-to-0-167-0-and-generate-more-of-its-contract.md](../decisions/0236-MADR-re-pin-codex-to-0-167-0-and-generate-more-of-its-contract.md).
  It generates a contract manifest from codex's JSON Schema bundles, by `scripts/codex-contract.ps1`, with a drift gate
  in `make live-codex-contract`: the method inventory, stability, response types and enum value sets.
* 0236 rejected full type generation (its option C): codex's bundle needs about 300 lines of normalisation, and silently
  drops fields. Those reasons do not apply to a schema the SDK designs to be generator-friendly: flat objects, string
  enums, no `oneOf`.
* Two routes, for the Magic MADR to choose (Q9):
  * generate Dart types;
  * or generate enum sets plus a decode test over the golden vectors, keeping the models thin and hand-written.
* The UI gains a paste box, a confirm dialog with links, a form, and "finish on the host" with the command. It loses
  `providerId == 'codex'` (F36) and the browser floor (F11).

#### 5.6.4 The one justified helper: pasted-callback replay

**What it does.** When the agent's method is `browser_paste` with a loopback redirect, and the agent offers no paste
channel, the daemon receives the URL the phone's browser landed on and replays it as one HTTP GET to the agent's own
listener on `127.0.0.1`. That is the case for codex's `codex login`, and for kilo and opencode upstream browser flows if
their routes do not take a paste.

**Why it lives in Magic.** The daemon is the only process on that host that can reach the agent's loopback on the user's
behalf. It carries the user's answer and needs no vendor knowledge. Its checks are generic OAuth (RFC 6749):

* the host is loopback;
* the port and path equal those of the `redirect_uri` in the authorize URL the agent produced;
* the `state` equals that URL's `state`;
* one shot, while the session is live;
* no redirect is followed;
* nothing is logged or stored.

**The alternatives:**

* **Agent-side paste (preferred where it exists).** gobble through `--answers`, and the SDK's `InputCode`; kilo's
  `/oauth/callback` and opencode's code submit, if U5 and U6 confirm them; and an upstream request to codex for a paste
  flag.
* **A reverse tunnel** (0074-MADR W3, not started). It is needed only where the browser delivers the code by a
  cross-origin fetch the user cannot see: grok, if F13's inference holds.
* **Do nothing.** Browser methods stay host-only, and are labelled so.

### 5.7 The home of every item

| Item | Home | Why there, and why not elsewhere |
| --- | --- | --- |
| Method, step, answer and error-kind vocabulary; schema; vectors; evolution rules | Contract module | shared by three repositories; data only |
| Provider method facts; all generic flows; sign-in handle; refresh; revocation table; vendor-file import; consent facts and storage helper; optional enforcement helper | SDK | generic to every consumer (constraint 2) |
| Wizard rendering and `Driver` | SDK | generic terminal and non-terminal callers |
| `auth.json`, precedence, product policy, notice wording, background classification and gating, settings UI, Pi import, `--json` envelope and answer channel | gobble | gobble-specific (constraint 3) |
| Provider list for clients before connect | acpext | static data (0042-MADR) |
| Phone envelope, admission, resume, retained results, cancel, host reasons, coordinator for codex and grok | Magic | phones and device identity are the daemon's. The coordinator guards CLI-owned files against a CLI behaviour (pre-login revoke) that only a supervisor of those CLIs can guard; the SDK will not (0016-MADR D10) |
| Thin mappings for codex, grok, kilo and opencode; quarantined scraper | Magic | those agents run their own engines; 0016-MADR D10 |
| Pasted-callback replay | Magic | §5.6.4: only the daemon can reach the host's loopback; no vendor knowledge |
| Structured sign-in output, paste routes | agent upstream | the agents own their flows (constraint 1) |

## 6. Consent and terms

### 6.1 The four parts

| Part | What it is | Home under option C |
| --- | --- | --- |
| 1. That consent is required | OpenAI's agent terms bind every app that uses `LoginChatGPTPlan` | SDK method facts: `Consent{ID: "chatgpt_plan.background_use", Required: true, Links}` |
| 2. Asking it | a yes/no prompt | a contract `confirm` step with that stable id; each front end renders it (gobble terminal, `--json`, the phone) |
| 3. Keeping the answer | storage | the app's storage, under an SDK contract (§6.4) |
| 4. Enforcing it | which requests are background (gobble's session titles from `smallModel`, and auto-compaction), and gating them | gobble classifies and gates (`acpserver/title.go:50-78`, `autocompact.go:70-78` [WIP]); an optional SDK helper refuses a request marked background on a plan session without consent |

### 6.2 The options

* **Option A, all in gobble (the status-quo WIP).**
  * Good: it matches 0043-MADR D8 ("What stays the consumer's"), needs no SDK API, and gobble knows its own background
    requests.
  * Bad: it is terminal-only and cannot be relayed (F32). It has three hand-rolled bugs: asked every sign-in, a "no"
    keeps an earlier "yes" (both F31), and absent from `/login`, setup and first run (F30). Clients cannot know in
    advance that consent is needed. There are two disjoint prompts (the SDK's usage `Confirm` and gobble's). Every other
    plan consumer re-implements it.
* **Option B, all in the SDK.**
  * Good: one implementation, compliant by default.
  * Bad: the SDK cannot know an app's background requests, its settings storage, or gobble's wording. That is gobble
    baggage in the SDK, against constraint 3, and it reverses D8.
* **Option C, split (recommended; the owner's tentative lean).**
  * **The SDK owns:**
    * the method's consent metadata, and its notice and usage links;
    * the canonical `confirm` step with a stable id, and the answer channel;
    * a small contract for keeping the answer (§6.4);
    * an optional enforcement helper: a `context` marker (for example `llmprovider.WithBackground(ctx)`) that the
      openai provider refuses on a plan session without consent, with `ErrNotPermitted`.
  * **gobble owns:** the notice wording, the background-request classification and gating, the `chatgptPlanBackground`
    setting, and its UI.
  * **Magic** relays a generic confirm step only.
  * Costs: an amendment to SDK 0043-MADR D8, additive SDK API under `make api-check` (or starting under
    `llmprovider/x/`), and an amendment to gobble's 0031-MADR.

### 6.3 What the Magic audit changes

* **C needs a Magic item, not only gobble work.** Magic has no confirm message, and cancels request-scoped elicitation
  (re-verified), so even a gobble that emitted the question could not have it relayed. Under C, the confirm is an
  ordinary `provider.auth_event` and `provider.auth_answer` (§5.6.1). This confirms A's "terminal-only" verdict from the
  relay's side.
* **Magic must not keep the answer.** 0074-MADR D2: "mcremote never creates its own credential vault" (re-verified). The
  answer goes back to gobble, which stores it. The phone learns the state from status: `auth status --json` gains a
  consent field, additive under gobble's 0045-MADR D6.
* **The stable id must be generic.** The gobble audit proposed `Consent: "chatgptPlanBackground"` for acpext (its §B).
  That is gobble's setting name, and under the no-baggage rule it must not reach the SDK or Magic. They key on
  `chatgpt_plan.background_use`; gobble maps that id to its setting.
* **One notice, not two.** The SDK's wizard `Confirm` ("You're using your ChatGPT plan … Continue?") becomes the SDK's
  default notice text for the method's `confirm`, and gobble supplies its own six-line disclosure in its place. The
  result is one step, with the usage link as data rather than the Go constant `openai.ChatGPTUsageURL`.
* **A correction to the analysis already given.** It cited "gobble.md finding 3" for terminal-only. In the gobble audit's
  list (§6.5), item 3 is the `acp --login openai` key path. The terminal-only property is in its §2.3-§2.5. Here it is
  F32, and the three bugs are F30 and F31.
* **What changes.** C stands, and is strengthened: it gains a concrete relay path and one Magic envelope item.
  Two questions open: whether to ask before the browser opens or after the save (Q4), and the storage sub-question.

### 6.4 Where the answer is kept

| Option | Good | Bad |
| --- | --- | --- |
| (a) On the OAuth session: a comparable `Consents string` (space-separated ids, as `Scope` is, 0043-MADR A1), persisted by `TokenStore`; an SDK helper `SetConsent(ctx, store, provider, id, granted)` under `RefreshLocker` | tied to the sign-in, as the terms tie consent to the connection; cleared on logout; one source of truth for the enforcement helper | changing it needs the SDK helper; gobble's `auth.json` gains a field (the plan fields already set the precedent) |
| (b) In app settings (the WIP's `chatgptPlanBackground` in `settings.yaml`) | easy to toggle with `gobble config set` | outlives the sign-in; can disagree with the session; a "no" must write `false` (F31) |
| (c) Both: on the session, with gobble's setting as the toggle UI writing through the helper | the advantages of (a), with the WIP's UX | two names for one fact, held equal by code |

The lean is (a), with the SDK helper. (c) keeps the WIP's setting as a view over it if the owner wants `gobble config
set` to keep working (Q3). Ask when unset, and record a "no" as `false`. Under (a), "from the next request" holds,
because the session is re-read per request. That fixes F31's record drift.

### 6.5 Policy questions the owner must decide (unverified)

* **Borrowed client ids.** The SDK's `browser_oauth` and `device_code` sign in as Codex (openai) and as the Grok CLI
  (grok). gobble refuses the Codex one (0005-MADR) and accepts the Grok one as risk. Magic's own codex and grok flows run
  the vendors' CLIs, so nothing is borrowed there. A phone that can drive SDK browser flows widens the borrowed-id
  exposure. The `client_identity` fact lets policy filter by it. Whether the vendors object is unverified.
* **Plan terms on a phone-driven host.** Tokens stay in gobble's `auth.json` on the host. Requests come from the host.
  What crosses Magic's link is the pasted callback URL: an authorization code and the state, single-use, worth a token
  only with the host's PKCE verifier. Whether OpenAI regards "a remote runtime only that user controls, driven from that
  user's phone" as compliant is unverified. The owner's recorded stance is "own host only, disclosed" (gobble
  0031-MADR, WIP terms amendment). The terms page was read by the owner, not fetched (gobble 0031-PLAN deviation 1).

## 7. Remove, modify and add: the ledger

"Now" items can land inside existing records before the redesign (§8.4, step 0). Dependencies name ledger ids or owner
questions.

### 7.1 The contract module (SDK repository)

| ID | Kind | Item | Evidence | Home | Depends on |
| --- | --- | --- | --- | --- | --- |
| K1 | Add | A nested module, stdlib only, with its own `go.mod` and tags (`authcontract/vX.Y.Z`), starting at v0 | the SDK has no nested module (re-verified: one `go.mod`); acpext is the precedent | Contract | SDK MADR (§8.1) |
| K2 | Add | `Method`, `Consent`, `Link`, `Field`, `Step`, `Answer`, `Failure`, `ErrorKind`, and the open enums (§5.3.1) | F1, F3, F9, F18 | Contract | K1 |
| K3 | Add | A JSON Schema generated from the types and committed; golden vectors; written evolution rules; a stricter API gate for the nested module | §5.3.3; `make api-check` covers the root module | Contract | K2 |
| K4 | Add | `Answer.Paste` redacted in `String`, `GoString` and `LogValue`, with vectors for it | F33; Magic's `SetCredentialPayload` redaction precedent | Contract | K2 |

### 7.2 go-llmprovider-sdk

| ID | Kind | Item | Evidence | Home | Depends on |
| --- | --- | --- | --- | --- | --- |
| S1 | Remove (deprecate in v1) | `AuthMethod.Interactive`, `HeadlessOK` | F17 | SDK | S3 |
| S2 | Remove (deprecate) | `LoginDeviceOAuth`, `OAuthFlowOptions.NotifyDevice`, `GrokFlowFromEnv` once wrapped; the fixture string in `ValidateOAuthSession` | F40; no known consumer (§2.5) | SDK | S4 |
| S3 | Add | Comparable fact fields on `AuthMethod`, with JSON tags; `Methods(Descriptor) []authcontract.Method`; explicit entries for implicit API keys | F18, F21 | SDK | K2 |
| S4 | Add | `Start` → `SignIn` (`Steps`, `Answer`, `Wait`, `Cancel`), the `Credential` sum type, `ErrorKindOf`; under `llmprovider/x/signin` first | F6, F3 | SDK | K2, Q1 |
| S5 | Modify | One loopback, paste and select implementation for the browser and plan flows; an undeliverable `OpenURL` becomes `failed` | F6, F40 | SDK | S4 |
| S6 | Modify | An explicit, persisted session kind; refresh re-dispatched by kind, not rewritten; Kilo's token as `device_key` | F26, F2 | SDK | S4 |
| S7 | Add (now) | `wizard.Options` `HostID`, `ClientID`, `Issuer`, `Identity`; the consumer's identity for `originator`, `referrer` and User-Agent | F20, F24 | SDK | – |
| S8 | Add | Consent facts on `chatgpt_plan`; `OAuthSession.Consents` persisted by `FileTokenStore`; `SetConsent`; the optional `WithBackground` marker and the openai provider's refusal | F29, §6 | SDK | K2, Q2-Q5 |
| S9 | Modify | Keep the plan's issued client id and host id across a logout or a kind switch | F22, F23 | SDK | Q12 |
| S10 | Modify | The wizard renders `SignIn` steps; `Options.Driver`; the plan notice becomes the method's default `confirm` text | F7, F29 | SDK | S4 |
| S11 | Modify (now) | Revocation returns `llmprovider.ErrUnsupported`; OpenAI's device time-out becomes `ErrAuthFailure`; doc drift (`LockRefresh`'s 40 s, the `GoString` prefix, `RevokeOAuthSession`, the 0006-MADR §7 note) | F3, F62 | SDK | – |
| S12 | Modify | Vendor-file path resolution moves into `auth` with an explicit lookup (0015-MADR D9); one revocation table | F40 | SDK | S4 |
| S13 | Add | Tests: descriptor facts instead of the dead pins; a gated live OpenAI device test; a recorded remote-paste probe per browser flow | F59 | SDK | S3 |

### 7.3 gobble-cli

| ID | Kind | Item | Evidence | Home | Depends on |
| --- | --- | --- | --- | --- | --- |
| G1 | Modify (now, WIP) | The notice and consent on every plan path, or refuse the plan on `/login`, setup and first run until they are | F30 | gobble | – |
| G2 | Modify (now, WIP) | Consent: ask only when unset; write `false` on a "no"; align 0031-MADR's "next request" with the code | F31, F61 | gobble | – |
| G3 | Modify (now, WIP) | A first plan sign-in under `--json` or without a terminal: a usage error saying why, instead of `errNoTerminal` | F52 | gobble | – |
| G4 | Modify (now, WIP) | `gobble acp --login openai`: restore the key choice, or record the deviation in 0031-PLAN | F53 | gobble | – |
| G5 | Modify (now, WIP) | A plan-aware resolver, so status, check, authenticate, provider choice and logout agree; `auth status --json` obeys 0045-MADR D2; never revoke a foreign token | F54 | gobble | – |
| G6 | Modify (now) | `SetOAuth` drops `key` and `workspace` | F25 | gobble | – |
| G7 | Modify (now) | Persist a host-lifetime host id in the state directory, and pass it | F24; 0025-MADR D6 | gobble | S7 |
| G8 | Modify (now, WIP) | Docs and help: P4b-2 step 8 | F55 | gobble | – |
| G9 | Modify (now) | No host `openURL` under `--json`; `/login` and `auth login` aligned on opening the browser; the device code's expiry in text | F56 | gobble | – |
| G10 | Add | `--json` step events and an opt-in `--answers` stdin channel; the old events retired or kept (Q21) | F8, §5.5 | gobble | K2, S4 |
| G11 | Remove | `HasLogin`, `HasKeyLogin` and `HasPiLogin` bodies, `offers`, the five openai checks, `signInWords`, revoke-iff-`HasLogin` → one policy filter over facts | F41 | gobble | S3 |
| G12 | Remove | `FailureKind` in `--json` → the contract's `ErrorKind` | F3 | gobble | K2, Q15 |
| G13 | Modify | Consent by option C: the SDK's `confirm` step with gobble's wording; storage by Q3; gating through the SDK helper or its own | F29, F32 | gobble | S8 |
| G14 | Add | Tests: the real wizard with `--json` and the plan; `/login`'s notice; status JSON for a non-plan entry; a "no" after a "yes"; `acp --login`'s key; every method word and `Command` accepted, each seen to fail on a planted bad value | F58 | gobble | G1-G5 |
| G15 | Add | `auth status --json`: a consent field and the method that produced the credential (additive under 0045-MADR D6) | §6.3 | gobble | G13 |
| G16 | Add | The one-time `auth.json` migration (§9.3) | §9.3 | gobble | G7, G13 |

### 7.4 acpext

| ID | Kind | Item | Evidence | Home | Depends on |
| --- | --- | --- | --- | --- | --- |
| A1 | Modify (now) | The `AuthProvider` doc says what `Command` does; `Login` documented as "has a method other than an API key" | F19 | acpext | – |
| A2 | Modify | Option (i): no auth shapes beyond the provider list; or option (ii): reference the contract module, with a 0042-MADR amendment | §5.5 | acpext | Q14 |
| A3 | Add | Tag `acpext/v0.5.0` if A1 or A2 changes anything; Magic skips v0.4.0 | F35 | acpext | A1 |

### 7.5 magic-cli-remote (daemon and phone)

| ID | Kind | Item | Evidence | Home | Depends on |
| --- | --- | --- | --- | --- | --- |
| M1 | Add | A MADR superseding 0185 in part, for auth; pin the contract module | §2.3; 0235-MADR D5 precedent | Magic | K1 tag |
| M2 | Remove | The old provider-auth messages and types: `provider.start_auth`, `oauth.*`, the three method types, `DeviceFlow`, `AuthInput`, the central annotation in `upstreamAuthPayload`, and the phone's matching models. One cut-over, daemon and app together | F1, F9; owner 2026-11-10 | Magic | M1 |
| M3 | Add | The envelope messages (§5.6.1), including `provider.auth_sessions`, which delivers retained results; an optional version check | F10, Q20 | Magic | M1 |
| M4 | Modify | One lifecycle (the owned one) for every flow; cancel kilo's engine attempts; handle disconnects | F37, F47 | Magic | M3 |
| M5 | Remove (now) | `browserOAuthMarkers`, `ClassifyCatalogMethod`'s label rule and opencode's `oauthType`; classify at start by URL only; stop using index ids for kilo | F34, F5 | Magic | Q18 |
| M6 | Remove | The gobble adapter's `p.ID` checks and `Login ⇒ device` → gobble's step pass-through | F35 | Magic | G10 |
| M7 | Remove (now, partly) | The phone's `providerId == 'codex'` (twice), the browser floor, `confirmDestructiveSignIn` | F36, F11 | Magic | – |
| M8 | Modify (now) | Owned flows carry the CLI's real expiry, and the reservation deadline is enforced | F42 | Magic | – |
| M9 | Modify (now) | `firstDeviceCode` bounded by the 30 s scan limit | F43 | Magic | – |
| M10 | Modify (now) | Wire `SetCredentialCoordinated` and `CoordinatedLogout`, or delete them and amend 0074-PLAN P18 | F44, Q17 | Magic | – |
| M11 | Modify (now) | Wire grok's `SetProbe`, or amend 0074 D25 for grok | F45 | Magic | – |
| M12 | Modify (now) | `deviceAuthWired` true for `OwnedDeviceAuth` directly | F46 | Magic | – |
| M13 | Modify (now) | Own error codes for the coordinator sentinels and `ErrTooManyFlows`; the phone's copy for them | F48 | Magic | – |
| M14 | Modify (now) | Project `BackupState` and `RecoveryAvailable` on status, or remove them | F49 | Magic | – |
| M15 | Modify (now) | Read gobble's `reason`: `usable` with a `reason` is not configured | F50 | Magic | – |
| M16 | Remove (now) | Dead vocabulary and code: `keyring_managed` (constant and error) and `quota`, which have no producer; `ZeroString`; `CancelDevice`; codex's legacy device path and `ErrAuthConfirmRequired`; stale comments; the three `host_oauth` literals → one constant | F4, F51 | Magic | – |
| M17 | Modify (now) | One status reader per agent; grok's aggregate uses `Validate` | F28 | Magic | – |
| M18 | Add | Thin mappers for codex, grok, kilo and opencode into contract steps (§5.6.2); the scraper quarantined in one package with live pins | F38 | Magic | M2 |
| M19 | Add | The pasted-callback replay helper (§5.6.4) | §5.6.4 | Magic | M2, Q13 |
| M20 | Add | Phone UI for paste, confirm, form and "finish on the host"; Dart models generated from the schema, or tested against the vectors | §5.6.3 | Magic | M2, Q9 |
| M21 | Modify | Rewrite the tests that pin heuristics (F57), with the new rules seen to fail on planted inputs | F57 | Magic | M5, M6 |
| M22 | Modify (now) | Record drift: 0074-MADR D3, 15.12; 0074-PLAN P18 and P20 entries | F60 | Magic | M10, M11 |
| M23 | Add | codex's access-token paste as a `form` method; codex's browser login through M19 | §3.1 | Magic | M18, M19 |

## 8. Records impact and proposed work breakdown

### 8.1 go-llmprovider-sdk

Existing records to amend:

* `0043-MADR-openai-chatgpt-plan-sign-in.md`:
  * D8 (consent stays the consumer's) is amended: the requirement, the step and the storage contract move into the SDK;
    the wording, the classification and the gating stay with the consumer.
  * D7 (the wizard's `AgentName` gate and notice): the notice becomes a step.
  * D3 (one OpenAI login per store) is revisited only if Q12 (b).
* `0016-MADR-provider-auth-and-support-baseline.md`:
  * D6 (device login returns a handle) is generalised to every interactive method.
  * D10 is affirmed unchanged: the SDK drives no agent CLI and writes no vendor store.
* `0006-MADR-subscription-auth-for-llm-providers.md`: the `AuthMethod{Interactive, HeadlessOK}` shape is superseded in
  part, and §7's vendor-import client-id note is corrected.
* `0057-MADR-wizard-device-code-and-paste-prompt.md`: D1 (`ShowDeviceCode`) and D2 (`NoPastePrompt`) are kept, as
  special cases of the driver.
* `0056-MADR-wizard-credential-only-entry-point.md`: D2's `KiloDeviceKey` is kept, and subsumed by the `yields` fact.
* `0015-MADR-canonical-sdk-api-and-module-layout.md`: its amendment "what `auth` holds" gains the nested contract module
  and `x/signin`; D9 (no ambient state) governs the move of vendor-path resolution.
* `0008-MADR-repair-oauth-loopback-and-session-wiring.md`: F5's inferred Grok mechanism is replaced by U1's result.
* `0017-REPORT-openai-sign-in-for-agents.md`: its open item "The terms in a consumer" is answered by the new consent
  pair.

New pairs, each at the next free number:

1. `NNNN-MADR-publish-an-auth-contract-module.md` and its PLAN: K1-K4. **First.**
2. `NNNN-MADR-one-sign-in-handle-and-method-facts.md`: S1-S6, S10, S12, S13.
3. `NNNN-MADR-chatgpt-plan-consent-host-id-and-client-reuse.md`: S7's host id, S8, S9. It amends 0043-MADR D8.
4. `NNNN-MADR-auth-error-kinds-and-doc-drift.md`: S11, and S7's other options. It can land now as a minor release.

### 8.2 gobble-cli

Existing records to amend:

* `0005-MADR-v1-feature-scope.md`: "writes no OAuth code of its own" is affirmed, and is what places the engine in the
  SDK. The OpenAI row is corrected (F55).
* `0016-MADR-acp-go-sdk2-v0.15.0-and-stable-surface.md` A2 (presence-only `authenticate`, no ACP `logout`): unchanged
  now, amended only at the elicitation phase.
* `0025-MADR-provider-choice-and-auth.md` D6 (a stable host id in the state directory; consent asked once and saved):
  built by G7; its storage is refined by Q3.
* `0031-MADR-sdk-current-releases-and-adoption.md`:
  * D7's amendments: the consent setting, the notice placement, and the phone flow "through `gobble acp --login openai`",
    which Magic cannot drive (gobble audit §5.4 item 10), become the `--answers` channel.
  * D8 (read `Descriptor.AuthMethods`) is realised by G11.
* `0031-PLAN-sdk-current-releases-and-adoption.md` P4b-2: G1-G9 as dated deviations (§8.5).
* `0042-MADR-acpext-extension-contract-module.md`: unchanged under §5.5 option (i); amended under (ii) (`:142`, A1).
* `0045-MADR-auth-status-json.md`: D2 is enforced (G5); D6 admits the additive fields of G15; D7 is unchanged under (i).
* `0049-MADR-structured-quota-and-rate-limit-errors-for-acp-clients.md`: its `ErrorKind` aligns with the contract's
  kinds when `FailureKind` retires (G12).

New pairs, each at the next free number:

1. `NNNN-MADR-structured-sign-in-steps-and-answers.md`: G10-G12, G14-G16, A2, A3.
2. `NNNN-MADR-chatgpt-plan-consent-on-the-sign-in.md`: G13. It could instead be a further amendment to 0031-MADR D7, as
   the same topic.

### 8.3 magic-cli-remote

Existing records to amend or supersede:

* [0074-MADR-remote-provider-auth-from-phone.md](../decisions/0074-MADR-remote-provider-auth-from-phone.md):
  * D2 (no vault) is affirmed.
  * D5 (typed methods with inputs) gives way to the contract's `Method`.
  * D6 (the v2 capability) is reduced to lockstep plus an optional version check.
  * D7 (classify by authorize URL) becomes the fallback.
  * D13 (system browser, no in-app listener) is revisited for paste.
  * W3 (reverse callback tunnel) is replaced by agent paste and the replay.
  * §15's drift is corrected (F60).
* [0074-PLAN-remote-provider-auth-from-phone.md](../decisions/0074-PLAN-remote-provider-auth-from-phone.md): the P18 and
  P20 entries (F44, F45, F10), and P22's live debt.
* [0082-MADR-settings-provider-menu-ux-overhaul.md](../decisions/0082-MADR-settings-provider-menu-ux-overhaul.md): the
  related row on the destructive warning (F36).
* [0083-MADR-provider-auth-activation-and-layout-gaps.md](../decisions/0083-MADR-provider-auth-activation-and-layout-gaps.md)
  D4: per-method `available` and `reason` become the envelope's host reasons.
* [0086-MADR-phone-provider-auth-completion.md](../decisions/0086-MADR-phone-provider-auth-completion.md) D6 (SuperGrok
  is browser by label): superseded by start-time classification. D5 and D1 are kept.
* [0107-MADR-grok-phone-api-key-and-device-code-auth.md](../decisions/0107-MADR-grok-phone-api-key-and-device-code-auth.md):
  D3 (browser host-only) stays until U1; D5 (spawn the CLI, not ACP `x.ai/auth/*`) is revisited if U7 is positive; D9's
  600 s feeds M8.
* [0179-MADR-gobble-native-acp-provider.md](../decisions/0179-MADR-gobble-native-acp-provider.md) D3 (phone auth through
  gobble's CLI) gains `--answers`; A6 stands.
* [0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md](../decisions/0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md):
  superseded in part for auth types (M1), as
  [0235-MADR-save-the-last-live-model-catalog-and-take-groks-fallback-from-the-sdk.md](../decisions/0235-MADR-save-the-last-live-model-catalog-and-take-groks-fallback-from-the-sdk.md)
  D5 did for the catalog functions.
* [0191-MADR-answer-agent-elicitation-forms-on-the-phone.md](../decisions/0191-MADR-answer-agent-elicitation-forms-on-the-phone.md)
  D1 and D6: amended only at the elicitation phase.
* [0208-MADR-make-the-provider-layer-agent-neutral.md](../decisions/0208-MADR-make-the-provider-layer-agent-neutral.md):
  its pass-through rule is what this applies.
* [0202-REPORT-go-llmprovider-sdk-provider-auth-adoption-assessment.md](0202-REPORT-go-llmprovider-sdk-provider-auth-adoption-assessment.md):
  its Option D objection ("the phone's contract would then change on the SDK's release cadence") is answered by the
  contract module's own cadence and by lockstep phone releases.

New pairs, each at the next free number (0246 at the time of writing, by `scripts/check_records.py --next`):

1. `NNNN-MADR-take-provider-auth-types-from-the-sdk-contract.md` *(became
   `0246-MADR-take-provider-auth-types-from-the-sdk-auth-contract-in-one-cut-over.md`, 2026-11-11)*: M1-M4, M6, M18, M20,
   M21. It supersedes 0185 in part,
   and makes the one cut-over.
2. `NNNN-MADR-replay-a-pasted-sign-in-callback-to-the-agents-loopback.md`: M19 and M23. This is the only non-relay logic,
   so it gets its own decision.

Defect fixes go into amendments of the open pairs, because they are the same topic:

* 0074: M5, M7, M8, M10-M14, M16, M17, M22;
* 0179: M9, M15, and the hold at acpext v0.3.0 *(the owner answered Q14 against the hold on 2026-11-10:
  `0179-MADR-gobble-native-acp-provider.md` A9 moves to acpext v0.4.0 now, with the device-rule patch)*.

### 8.4 Sequencing and cross-repository choreography

| Step | Repo | Work | Blocked by | Blocks |
| --- | --- | --- | --- | --- |
| 0 | all, in parallel | Defect fixes in the existing records: S11 and S7 (SDK minor tag); G1-G9 (G7 after S7's tag), A1; M5, M7, M8-M17, M22. The owner answers §11. | – | – |
| 1 | SDK | The contract module MADR, then K1-K4, then tag `authcontract/v0.1.0` | the owner's Q1, Q8 | 2, 3, 4 |
| 2 | SDK | The engine on the contract (S1-S6, S8-S10, S12, S13) under `x/signin`; an SDK minor tag | 1 | 3 |
| 3 | gobble, acpext | G10-G16, A2, A3; pin the SDK and the contract; tag gobble, and `acpext/v0.5.0` if it changed | 2 | 4's gobble part |
| 4 | Magic | M1-M4, M18-M21, M23, M6; pin the contract (and acceptance against the gobble tag); ship the daemon and the app together | 1 for the types; 3 for gobble's steps; codex, kilo and opencode mapping need only 1 | 5 |
| 5 | SDK | Promote `x/signin` into `auth`; contract v1 under §5.3.3; S1, S2 deprecations | 3 and 4 having run it | – |
| 6 | gobble, Magic | Later: ACP-native sign-in by elicitation (gobble 0016-MADR A2 and Magic 0191-MADR amendments) | 3, 4; U8 | – |

Live acceptance debt sits beside this: 0074-PLAN P22 steps 5-6 and 0179-PLAN P6 are owner-run, and spend quota (Q19).

### 8.5 The owner's in-flight gobble WIP

The WIP builds, its tests pass (gobble audit, verification run), and its terms work is done. Its storage half (the plan
fields in `Entry`, `Login` and `sdkStore`) is needed by every option. Its CLI half adds five openai special cases, which
G11 later removes.

| Option | Good | Bad |
| --- | --- | --- |
| (a) Land as is | fastest; the plan reaches users now | ships F30, F31, F52, F53 and F54; the last breaks gobble's public `auth status --json` contract, and could revoke a foreign token |
| (b) Fix first, then land (recommended) | G1-G9 are small, well located and inside 0031-PLAN P4b-2 as dated deviations; the openai checks gathered into one predicate ease G11 | some days now; the CLI half still churns at step 3 |
| (c) Fold into the redesign | no churn | the plan sign-in waits for steps 1-3; uncommitted work sits at risk of loss and drift |
| (d) Land the storage half only | low risk; no defects shipped | dead fields until the CLI half lands; P4b-2 stays open |

## 9. Compatibility and migration

### 9.1 Code and wire contracts

* **SDK v1 (`make api-check`).**
  * Everything in §7.2 is additive: new fields, functions, optional interfaces and method ids. `x/signin` is exempt
    until promotion.
  * Deprecated names stay until a v2, if there ever is one.
  * `AuthMethod` and `OAuthSession` stay comparable, with no slices or maps (0043-MADR A1).
  * A new method id changes menus for consumers that index by position (`providers/openai/descriptor.go:50-51`); S3's
    stable ids end that.
* **The contract module.** v0 while gobble and Magic adopt it; v1 under §5.3.3 thereafter.
* **acpext consumers.** Only gobble's root and Magic. `AuthProvider` stays comparable, so gobble's `slices.Equal` test
  holds. Magic moves from v0.3.0 straight to v0.5.0, skipping v0.4.0's `Login` for openai.
* **gobble CLI JSON.**
  * `--json` events are additive, and Magic ignores unknown ones (re-verified).
  * `--answers` is opt-in.
  * `auth status --json` gains fields only (0045-MADR D6).
  * Retiring `FailureKind` changes the `kind` words. Magic reads `message` only (Magic audit §3.5), so it is not
    affected.
  * Exit codes are unchanged.
* **Phone and daemon.** Not required: they ship together (owner, 2026-11-10). An optional version check can refuse a
  mismatched pair (Q20).

### 9.2 Stored credentials: one-time migrations

The owner's real data is in these files. Each migration backs up first, is idempotent, and writes nothing it cannot
explain.

* **gobble `auth.json`** (G16, run once by the first gobble version that has S8 and G7):
  1. Back up to a sibling file restricted to the owner (0600, or the protected DACL on Windows).
  2. For an `openai` plan entry: if `settings.yaml` holds `chatgptPlanBackground: true`, write the consent id onto the
     session (Q3 (a) or (c)); otherwise leave consent unset, so it is asked at the next plan sign-in.
  3. If no host-lifetime host id is stored, adopt the plan entry's `hostId` as the host's id, so OpenAI sees no new host.
  4. Drop a dormant `key` or `workspace` from `oauth` entries (F25), with one log line per provider, and the backup
     still holding them (Q22).
  5. Leave unknown fields as they are (they round-trip today, `store.go`).
* **SDK `FileTokenStore` records.** A new `kind` or `consents` field is written on the next `Save`, never on a read. Plan
  sessions keep `resource`, so an older SDK still routes them as plan sessions. The downgrade loss of new fields is the
  one 0043-MADR D3 already accepted.
* **Agents' own stores** (codex, grok, kilo, opencode) are untouched, and Magic's coordinator manifests are unchanged.
  Magic stores no credential (0074-MADR D2), so it has nothing to migrate.

## 10. Risks

1. **Refresh regression.** Both vendors revoke a whole token family on reuse (`tokenstore.go:180-182`). The refresh,
   lock and rotation code must be re-dispatched, not rewritten (§5.4.3).
2. **Contract creep in both directions.** Phone concerns could leak into the SDK, or SDK flow into Magic. The guard is
   the §5.3.2 list, checked in review.
3. **A wrong field frozen at v1.** The contract starts at v0, and is promoted only after gobble and Magic have run it.
4. **Release choreography across three repositories.** Pin skew between the contract, the SDK, gobble and Magic is held
   off by the §8.4 order and by golden vectors in every consumer.
5. **Pasted callbacks are secrets.** A pasted URL holds an authorization code. It must be redacted on the wire and in
   logs, forwarded once, and never stored (K4, §5.6.4).
6. **Fixed loopback ports 1455 and 1457** allow at most two concurrent OpenAI-style browser sign-ins per host (F14).
7. **Policy.** Borrowed client ids and the plan terms on a phone-driven host are unverified and undecided (§6.5).
8. **Grok browser** remote completion is unverified (F13); the design must say "device only" until U1.
9. **The coordinator** (about 3,600 lines, heavily tested) must not be weakened while the codex and grok paths move into
   the contract.
10. **The WIP** collides with the redesign (§8.5).
11. **Upstream churn** breaks the quarantined scrapers on CLI upgrades (F38); the live pins (0074-MADR D15) catch it.
12. **Consent migration.** A "yes" lost, or a "no" read as a "yes" (§9.2, G16); the migration is tested on copies of
    real-shaped files.
13. **Dart code generation is new** to the app (§5.6.3); its tooling has to be chosen and gated (Q9).

## 11. Questions for the owner

Q1. Where does the engine API start? (a) **recommended**: the contract module at v0, and the engine under
`llmprovider/x/signin`, promoted later; (b) additive in `llmprovider/auth` at once; (c) a separate repository.

Q2. How is consent split? (a) **recommended, the owner's lean**: option C; (b) option A, all in gobble; (c) option B,
all in the SDK.

Q3. Where is the consent answer kept? (a) **recommended**: on the OAuth session, with an SDK helper; (b) in gobble's
settings; (c) both, with the setting as a toggle that writes through the helper.

Q4. When is consent asked? (a) **recommended**: before the browser opens, with the notice, committed only when the
sign-in saves; (b) after the save, as the WIP does; (c) lazily, at the first background request.

Q5. Does the SDK ship the optional enforcement helper (a `context` marker and the openai provider's refusal)? (a)
**recommended**: yes, opt-in; (b) no, gobble gates alone.

Q6. What happens to the gobble WIP? (a) **recommended**: fix G1-G9 first, then land; (b) land as is; (c) fold into
the redesign; (d) land the storage half only.

Q7. What is gobble's remote answer channel? (a) **recommended**: `--json` with an opt-in `--answers` on stdin; (b) ACP
elicitation now; (c) both now.

Q8. How is the phone contract tied to the SDK? (a) mirror; (b) import the full SDK; (c) **recommended**: the
stdlib-only contract module, embedded in Magic's envelope.

Q9. How are the phone's Dart models made? (a) **recommended**: generated from the contract's JSON Schema, which is
designed for it; (b) generate enum sets and decode the golden vectors in tests, with hand-written models; (c)
hand-written, as today.

Q10. What is the policy on borrowed client ids? (a) **recommended**: keep the current per-app policy (gobble refuses
Codex's and accepts the Grok CLI's), and show `client_identity` on the phone; (b) refuse `vendor_borrowed` on
phone-driven sign-ins; (c) refuse it everywhere.

Q11. Is a ChatGPT plan sign-in allowed from the phone? (a) **recommended**: yes, under "own host only, disclosed", with
the disclosure shown as the phone's `confirm` and the pasted callback treated as a secret; (b) refuse the plan from the
phone; (c) ask OpenAI first.

Q12. How many store slots? (a) **recommended**: one per provider, keeping the issued client id and host id apart from
the session; (b) an optional keyed store now; (c) no change.

Q13. Does Magic build the pasted-callback replay? (a) **recommended**: yes, scoped as §5.6.4, and only where the agent
has no paste channel; (b) never, so browser methods stay host-only until the agents add paste; (c) a reverse tunnel
(W3).

Q14. What does acpext publish? (a) **recommended**: option (i), the provider list only, with Magic skipping v0.4.0;
(b) option (ii), referencing the contract module and amending 0042-MADR; (c) bump Magic to v0.4.0 now and fix the
mapping.

Q15. One error-kind vocabulary? (a) **recommended**: the contract's `ErrorKind`, with gobble retiring `FailureKind` in
`--json`; (b) keep both.

Q16. Issue gobble-cli#16? (a) **recommended**: close it as superseded, with §13's text; (b) keep it open, retitled.

Q17. codex's coordinated key write and logout? (a) **recommended**: wire them; (b) delete them and amend 0074-PLAN P18.

Q18. Magic's label heuristics? (a) **recommended**: remove them now, and decide browser or device at start by URL;
(b) keep them until the agents declare.

Q19. The live acceptance debt? (a) **recommended**: run 0074-PLAN P22 steps 5-6 and 0179-PLAN P6 before the
cut-over; (b) re-scope them into the new pairs.

Q20. A daemon and app version check? (a) **recommended**: a simple refusal of a mismatched pair at connect; (b) none.

Q21. gobble's present `--json` events? (a) **recommended**: retire them at the cut-over, since Magic is the only known
consumer; (b) keep them as aliases.

Q22. The dormant keys in `oauth` entries, at migration? (a) **recommended**: drop them, with the backup kept; (b) keep
them.

## 12. Unverified items and proposed probes

Asking before any probe that spends quota, or that touches a real sign-in, is a standing rule. Where a probe signs in,
it uses a scratch home, never the owner's.

| ID | Item | Probe | Live sign-in | Quota |
| --- | --- | --- | --- | --- |
| U1 | Whether accounts.x.ai shows a pasteable URL or code when its fetch to the loopback fails (F13) | `grok login --oauth` with a scratch `GROK_HOME` on the Linux test host; open the URL on another device; observe | yes | no |
| U2 | That the SDK's OpenAI browser flow completes by paste from another device | an SDK example under the borrowed Codex client, with a scratch store | yes | no |
| U3 | Plan sign-in: a pasted full URL from another device; the port fallback; `id_token_hint` alone; a bare code on a first sign-in (0043-MADR A3) | the SDK's gated live test, extended; each run registers a client under the account | yes | no for the sign-in; yes if a request is sent |
| U4 | kilo's DigitalOcean and GitLab method shapes on 7.8.27 (F34) | read `GET /provider/auth` from a local engine with a scratch home | no | no |
| U5 | What kilo's `POST /provider/{id}/oauth/callback` accepts (F15) | read kilo's OpenAPI and source offline; then one throwaway upstream sign-in | yes, for proof | no |
| U6 | opencode's code-submit route for `mode: "code"` (F15) | read opencode's control-plane API offline; then one throwaway sign-in | yes, for proof | no |
| U7 | Whether grok 1.0.77 still serves ACP `x.ai/auth/get_url` and `submit_code` | ACP `initialize` and a method call against a scratch home, approving nothing | no | no |
| U8 | Whether acp-go-sdk2 v1.35.0 gives the `authenticate` handler the request id, for request-scoped elicitation | read the module's source | no | no |
| U9 | Which client Pi's OpenAI login uses (gobble's records conflict) | read Pi's source | no | no |
| U10 | The shape of codex's `auth.json` after `codex login --with-api-key` | a dummy key in a scratch `CODEX_HOME`; whether codex calls out first is itself unverified, so ask | no | no (to be confirmed) |
| U11 | Whether grok opens a host browser on Windows during a device sign-in (F16) | start the owned device flow on the Windows test host in a scratch `GROK_HOME`; cancel before approving | starts one; nothing approved | no |
| U12 | Whether OpenAI accepts a phone-driven plan sign-in on the user's own host (F33) | not a probe: the owner re-reads the terms, or asks OpenAI | – | – |
| U13 | That a replayed callback completes `codex login` (§5.6.4) | `codex login` in a scratch `CODEX_HOME`; paste the landed URL through a scratch replay; the scratch home avoids codex's pre-login revoke of a real grant | yes | no |
| U14 | Whether the wizard's direct `OAuthSession` field writes race (F27) | a targeted `go test -race` in a scratch clone | no | no |
| U15 | How acp-go-sdk2's `RequestError` renders the command in a handshake failure (0179-MADR A6) | a unit test in a scratch clone | no | no |

## 13. Issue gobble-cli#16: disposition

The issue was read through the gobble audit (§8 there), not re-read here. It asks acpext to publish each provider's
`--method` words as `AuthProvider.Methods []string`, with `MethodAPIKey`, `MethodBrowser` and `MethodDevice` constants.
It asks to keep `Login` for one minor version, to test that each listed word is accepted by `methodOf`, and to tag
`acpext/v0.5.0` after P4b-2. Its expected openai value is `["api-key","browser"]` "or whatever word P4b-2 gives". A
"related, separable" part asks for a paste under `--json --method browser`.

**Disposition: partly right, and superseded.**

* **Carried forward:**
  * clients need per-method data;
  * words derived from gobble's own method list;
  * a test that every published word and `Command` is accepted by the CLI (G14);
  * a v0.5.0 tag when acpext changes (A3).
* **Not carried forward:**
  * `Methods []string` as a field, which makes `AuthProvider` incomparable;
  * words alone, because they do not say how a method completes, what it yields, or that it needs consent: the
    contract's `Method` does;
  * the openai value `["api-key","browser"]`, because P4b-2's word is `chatgpt-plan`, and `browser` is the Codex flow
    gobble refuses;
  * `Login` read as "device".
* **Moved:** the "separable" paste under `--json` is the central gap. It becomes G10's `--answers` channel.

Suggested closing text:

```text
Closing as superseded by magic-cli-remote's report
docs/reports/0245-REPORT-non-api-key-auth-across-magic-gobble-and-the-sdk.md (repository magic-cli-remote).

What carries forward: clients get per-method facts (how a method completes, what it yields, its consent),
drawn from the SDK's descriptors and published through a shared auth contract module rather than as
AuthProvider.Methods []string, which would make AuthProvider incomparable. A test will assert that every
published method word and Command is accepted by gobble auth login. acpext's Login stays, documented as
"has a method other than an API key", not "device code". The paste under --json becomes an opt-in
answer channel on stdin (the report's G10). The work is tracked in new gobble-cli MADR/PLAN pairs that
cite the report.
```

## Appendix A. Evidence re-verified for this report

Read on 2026-11-10, read-only. Each item backs a finding or a recommendation above.

* SDK:
  * `llmprovider/descriptor.go:8-35`: the ids, and `AuthMethod`, with no JSON tags;
  * no non-test read of `.Interactive` or `.HeadlessOK`;
  * `auth/oauth_loopback.go:147-151`: `ignoreOAuthError` on `OpenURL`;
  * `auth/doc.go:8-26`: borrowed ids and terms;
  * `auth/tokenstore.go:171-187`: `TokenStore` keyed by provider, and `RefreshLocker`;
  * `wizard/auth.go:268-298`: the plan `Confirm`, and no `HostID` passed;
  * `auth/oauth_plan.go:223-238`: `planHostID`;
  * `auth/oauth_device.go:83-126`: `DeviceLogin` and `StartDeviceOAuth`;
  * `0043-MADR-…md:257-262` D8;
  * `0017-REPORT-openai-sign-in-for-agents.md:289-294`;
  * `0016-MADR-…md:365-375` D10;
  * `git describe`: `v1.29.0-1-g773b84c`, with HEAD touching only `docs/`;
  * no `llmprovider/x/`, and one `go.mod`.
* gobble [WIP]:
  * `internal/cli/login.go:62`, `:93-145`: signInTo without a notice; the notice lines; `askPlanBackground`;
  * `internal/cli/auth.go:199-236`: `loginWith`;
  * `internal/cli/auth.go:441-470`: `statusRow`, with `Usable` from `Has` beside `oauth_unused`;
  * `acpext/auth.go:1-34`: the doc, and `AuthProviders`;
  * `0042-MADR-…md:110`, `:142`, `:292-296`: stdlib only, and the `check-acpext-deps` gate;
  * HEAD `d6c10a5` = `acpext/v0.4.0`.
* Magic:
  * `internal/ws/server.go:2384-2397`: the central annotation;
  * `internal/provider/gobble/auth.go:151-171`, `:389-410`;
  * `internal/provider/httpagent/authcatalog.go:184-207`;
  * `internal/provider/opencode/acp_auth.go:363-370`;
  * `internal/providerauth/registry.go:249-258`, `:301`, with `PendingResults` callers in tests only;
  * `internal/provider/codex/auth.go:136-154`;
  * `internal/provider/grok/auth.go:164-182`;
  * `SetProbe`, `SetCredentialCoordinated` and `CoordinatedLogout`: no non-test callers;
  * `acpext.ErrorData`, used only at `internal/provider/gobble/errorlimit.go:42`;
  * `internal/provider/acpagent/elicitation.go:31-49`;
  * `apps/mobile/lib/data/protocol/models.dart:1522`;
  * `apps/mobile/lib/features/settings/provider_detail_screen.dart:581`, `:729`;
  * `apps/mobile/pubspec.yaml`, with no code generator;
  * `go.mod:15`, acpext v0.3.0;
  * `0074-MADR-…md:98` D2;
  * `0208-MADR-…md:298`;
  * `0185-MADR-…md:11-19`;
  * `0236-MADR-…md:134-160`, `:203-240`;
  * `.markdownlint-cli2.jsonc`;
  * no YAML front matter on any report in `docs/reports/` (16 files).

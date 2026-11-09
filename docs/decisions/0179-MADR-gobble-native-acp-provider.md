---
status: accepted
date: 2026-11-09
decision-makers: Project Owner
consulted: "this repository at 938b0002, read from code; gobble-cli at v0.2.0 (0da1f2d, 2026-11-06) and acpext/v0.1.0 (4d1eb01), read from code and, for initialize, session/new, session/list and authenticate, from a no-credential run of the installed binary; acp-go-sdk2 v1.34.0, the pin of both; the module proxy for acpext/v0.1.0; this record's previous body (e020cd54); 0074, 0184, 0185, 0186, 0208, 0234 and 0235; gobble-cli's 0008, 0017, 0042 and 0043 records and its 0009-REPORT. For amendment A1 (2026-11-08): issue #36; this repository at 5b3533fc; gobble-cli's go.mod at v0.2.0, v0.3.0, v0.3.1 and main (4970cca), and acpext/v0.1.0's types.go, read through the GitHub API. For amendment A2 (2026-11-09): issue #37 and the owner's comment on it; this repository at 15cec5a8; gobble-cli v0.4.0 (2e83cce): its go.mod and internal/cli/version.go; acpext/v0.2.0 (253e366): methods.go, types.go, doc.go, go.mod and testdata/, and its diff from acpext/v0.1.0, read through the GitHub API"
informed: gobble-cli, acp-go-sdk2
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Register gobble as a native ACP provider: a thin Spec on gobble v0.2.0 and acpext v0.1.0, phone auth through gobble's own CLI, and a credential-aware handshake

## Context and Problem Statement

gobble is the platform's own coding agent, built in the `gobble-cli` repository. `gobble acp` serves the Agent Client
Protocol on stdio. This repository drives ACP agents through one shared adapter, `internal/provider/acpagent`, with one
`acpagent.Spec` per agent and, since 0208's P8, one `provider.Descriptor` per provider. grok and opencode are
registered that way. gobble is not: there is no `provider.IDGobble` and no `internal/provider/gobble`.

On 2026-11-06 the owner asked for "a serious assessment of building support for gobble-cli as a provider. Evaluate
historical docs and reports. Analyze the current codebases. We should be at a point where this should go smoothly. Be
thorough. Write findings to a detailed, facts driven, and comprehensive madr for review."

This record answers that. It is this number's third body. The second (2026-11-03, last changed in `e020cd54`) wrote
gobble's contract and named six gates. Since then both codebases moved: gobble cut `v0.2.0` and `acpext/v0.1.0`, and
this repository landed 0208's P7 and P8, 0234 and 0235. Every fact below was re-read at the baselines, and the
contract is restated on them. Where the second body still holds, its decisions are kept under their numbers. The
History says what changed.

**The short answer: yes, it should go smoothly.** Every gate the second body set that is outside this repository's
own work is now met (F6). What is left is this repository's package and registration, plus four small gaps the
re-read found (F7, G8 to G11). None needs a change to the adapter's behaviour for other agents. One needs a small,
agent-neutral `acpagent` hook (D4). One needs a small gobble-cli change, or a slower fallback until it lands (D3, Q1).

### Baselines

| Side | Read at | acp-go-sdk2 |
| :--- | :--- | :--- |
| This repository | `938b0002` (master, 2026-11-06) | `v1.34.0` (`go.mod:11`) |
| gobble-cli | `v0.2.0` = `0da1f2d` (main, 2026-11-06), clean; tags `v0.1.0`, `v0.1.1`, `v0.2.0`, `acpext/v0.1.0`, all pushed | `v1.34.0` (gobble-cli `go.mod`) |
| gobble-cli `acpext` | `acpext/v0.1.0` = `4d1eb01`; identical in `v0.2.0` (`git diff acpext/v0.1.0 v0.2.0 -- acpext` is empty); served by the module proxy | none: `acpext/go.mod` has no `require` |
| The installed gobble | `gobble version`: `gobble 0.2.0`, commit `0da1f2d`, go 1.27.3 darwin/arm64 | — |

gobble-cli's other pins at `v0.2.0`: go-llmprovider-sdk `v1.27.0`, go-selfupdate-lib `v1.24.0`, go-tui-lib/v2
`v2.12.0`, `gobble-cli/acpext v0.1.0`. This repository pins go-llmprovider-sdk `v1.27.0` too, since 0235.

**Re-read 2026-11-08, for amendment A1.** The table above is the third body's read and stays as written.

| Side | Read at | acp-go-sdk2 | go-llmprovider-sdk | acpext |
| :--- | :--- | :--- | :--- | :--- |
| This repository | `5b3533fc` (master, tag `v0.25.1`) | `v1.34.2` (0217 P3g) | `v1.28.0` (0217 P3g) | not required yet |
| gobble-cli `v0.2.0` | `0da1f2d` | `v1.34.0` | `v1.27.0` | `v0.1.0` |
| gobble-cli `v0.3.0` | `b67cbbe` (released 2026-11-07) | `v1.34.0` | `v1.27.0` | `v0.1.0` |
| gobble-cli `v0.3.1` | `6f82468` (released 2026-11-07) | `v1.34.0` | `v1.27.0` | `v0.1.0` |
| gobble-cli `main` | `4970cca` | `v1.34.2` | `v1.28.0` | `v0.1.0` |

Only the `go.mod` of each gobble-cli ref was read for A1. Nothing else in `v0.3.0` or `v0.3.1` was re-read.

**Re-read 2026-11-09, for amendment A2.** The tables above stay as written.

| Side | Read at | acp-go-sdk2 | go-llmprovider-sdk | acpext |
| :--- | :--- | :--- | :--- | :--- |
| This repository | `15cec5a8` (master) | `v1.34.2` | `v1.28.0` | not required yet |
| gobble-cli `v0.4.0` | `2e83cce` (released 2026-11-08) | `v1.34.2` | `v1.28.0` | `v0.2.0` |
| gobble-cli `acpext/v0.2.0` | `253e366` | — | — | requires no module, as at `v0.1.0` |

For A2, `v0.4.0`'s `go.mod` and `internal/cli/version.go` were read, and acpext `v0.2.0`'s whole diff from `v0.1.0`.
The rest of `v0.4.0` was not re-read; issue #37 describes what it sends a client that offers `session.compaction`.

The `initialize`, `session/new`, `session/list` and `authenticate` facts marked **(run)** come from one run of the
installed `gobble acp`. It ran under `env -i`, with `HOME` and `GOBBLE_HOME` in a scratch directory, so no credential
was present and no provider could be reached. No prompt was sent. The directory was removed afterwards.

### F1. What this repository provides now

Re-verified at `938b0002`. None of it is rebuilt for gobble.

* **One descriptor per provider** (0208 P8, complete 2026-11-04). A provider is its package and one line in
  `registered.Descriptors()` (`internal/provider/registered/registered.go:17-25`).
  * A `provider.Descriptor` has `ID`, `Name`, `DevOnly`, `Section`, `Floor`, `Credentials` and `New`
    (`internal/provider/descriptor.go:13-31`).
  * Its section, defaults, environment variables, validation, construction, prewarm, credential adapter and doctor
    floor live in the provider's package. The daemon, `doctor` and the loader iterate the list.
  * Provider sections are strict: an unknown key, or a `providers.<id>` for an unregistered id, fails the load by name.
  * grok's `section.go` (`internal/provider/grok/section.go:130-145`) is the pattern: a `Section` embedding
    `config.ACPProviderConfig`, and `New` building the Spec's provider.
* **The Spec's hooks** (`internal/provider/acpagent/acpagent.go:57-250`): identity and launch (`ID`, `DefaultBin`,
  `DefaultArgs`, `ModelArgs`), version (`KnownGoodVersion`, `MinVersion`, `UpgradeURL`), `StartTimeout`, models
  (`StaticModels`, `StaticDefaultModel`, `ListModels`, `ModelsFromOption`, `ModelProviders`), auth (`AuthStatus`,
  `AuthCatalogList`, `SetCredential`, `ClearCredential`, `SafeAuthMethodIDs`, `AuthPreference`, `KeyAuthMethodID`,
  `HasHeadlessCredential`, `KeyNotAdvertisedHint`, `CredentialModel`, `SetActiveUpstream`, `StartDeviceAuth`), modes
  (`StaticModes`, `DefaultModeID`, `SynthesizeAutoMode`, `DangerousModeIDs`), `Commands`, `CommandCaveat`,
  `ExtensionNotifications`, `ExtensionRequests`, `MetaHandlers`, `ClientCapabilities`, `CredentialEnv`,
  `NewSessionState`, `InitializeMeta` and `Ops`.
* **Ops and Host** (`acpagent/host.go`). `Ops` holds Fork, Compact, Rename, UndoLast, RuntimeUsage, RuntimeStatus,
  ConfirmLimit, the workspace, shell, revert, unrevert, diff and skill operations, Diagnostics and BeforePrompt. A nil
  hook is "not offered". A hook acts through `Host`, which offers `CallExtension`, `RawRequest`, `Notice`,
  `UpdateCompaction`, `Emit`, `SpecState`, `Busy` and `SetModelCatalog` among others.
* **Dangerous modes** (0208 P7, 2026-11-04). `Spec.DangerousModeIDs` marks an agent's own modes for the phone's
  confirmation (`acpagent.go:164-169`, `session.go:2263-2270`; `TestDangerousModeIDsMarkTheAgentsModes`).
* **Declared capabilities, pass-through commands, the standard model and thinking path, and the event mapping.** As
  the second body listed them (0208 D1, D9; 0184 D6). `acp.ConfigOptionByCategory` finds a grouped select, and
  `ModelsFromOption` makes the session's `model` option the picker's catalog, groups included
  (`acpagent/modelcatalog.go:21-52`).
* **Saved model catalogs** (0235, 2026-11-06). A live catalog a session reports through `SetModelCatalog` is saved per
  provider and per instance, and served where the static list would be when the agent cannot answer
  (`acpagent/host.go:308-317`, `acpagent.go:420-480`). A gobble session's grouped `model` option, made the catalog by
  `ModelsFromOption`, is saved this way. After the first gobble session on a host, the picker has gobble's models with
  no gobble process running. The second body's "no gobble models before the first session" holds only for a host's
  first session.
* **Cold catalog harvest.** With no `Spec.ListModels`, `ListModels` starts one agent process to read `initialize`'s
  model state (`acpagent.go:420-512`). opencode sets a no-op `ListModels` so that no process starts to learn nothing
  (`opencode/acp_catalog.go:38-43`).
* **Auth selection** (0085 D2, as 0234 D2 changed it; `acpagent/authselect.go:15-38`). The handshake picks the pinned
  method, else the first of `AuthPreference` that is advertised and in `SafeAuthMethodIDs`, else `KeyAuthMethodID`
  when `HasHeadlessCredential` is true. The preference is a fixed list. **It does not ask which upstream has a
  credential.** With nothing chosen, `Start` fails `ErrACPAuthRequired` before `session/new`
  (`acpagent.go:736-790`).
* **Client capabilities.** The default advertises `fs.readTextFile`, `fs.writeTextFile`, `terminal`,
  `session.configOptions.boolean`, `session.notices`, `session.compaction`, `elicitation.form` and `plan`
  (`acpagent.go:1354-1366`). A Spec's own set replaces it.
* **Fork.** `Manager.Fork` calls the session's fork, then creates a new session whose `AgentSessionID` is the fork's
  result, in a new process that loads it. The source's process stays open (`internal/session/manager.go:3262-3321`).
* **Close.** `session/close` is bounded at 2 s (`acpagent/session.go:1365-1366`), then the process group is killed.
* **Version.** `reportEngineVersion` reads `agentInfo.version` and warns on a mismatch with `KnownGoodVersion`. It never
  refuses (`acpagent/version.go:30-58`). `MinVersion` refuses a lower one.
* **Doctor's floors.** `doctor` runs `<bin> --version` only for a descriptor with a `Floor`
  (`internal/cli/doctor.go:167-195`).
* **Phone auth precedent.** grok's Spec implements `AuthStatus`, `SetCredential`, `ClearCredential` and
  `StartDeviceAuth` with grok's own CLI and files (`internal/provider/grok/auth.go`). `AuthStatus` rides on every
  `providers.list` (`internal/provider/auth.go:199-215`), so it must be fast.
* **Not yet agent-neutral** (0208 P9, not started):
  * `defaultProviderID` prefers a ready grok by name (`internal/ws/server.go:3523-3545`);
  * `config.KnownProviderIDs` names the four agents for the prewarm toggle (`internal/config/prewarm_write.go:27-28`);
  * the phone branches on provider ids in ten places, for example `chat_screen.dart:2626-2645`. Where a session has
    `available_commands_update`, the diff and fork menus follow it instead (`useRemote`).
* **Phone icons.** `apps/mobile/assets/vendor_icons/` holds 67 vendor SVGs and none for gobble.
* **go-llmprovider-sdk** is now in `go.mod` (0235), imported by `internal/provider/grok` alone, which
  `TestSDKIsImportedOnlyByGrok` enforces for every other package, gobble's included.

### F2. What acp-go-sdk2 provides

Both repositories pin `v1.34.0`, so they share one schema and one set of capability gates (0184 D12). The second body
read `v1.31.2` for these: capability gates on every typed call, the refusal of an unadvertised `fs/*` or
`terminal/*` request with -32601, `ConfigOptionByCategory`, `CallExtension`, `IsAuthRequired`, `AuthenticateMethodIDs`
(used since 0234), `agentproc`, and unstable `session/fork`. They were not re-read at `v1.34.0` for this record. P1 of
the PLAN re-reads them, as its step 1 already says.

### F3. What gobble v0.2.0 provides

Read at `v0.2.0`. Paths are gobble-cli's.

* **The entry point.** `gobble acp` (`internal/cli/acp.go:20-53`) runs `acpserver.Serve`, which wraps `acp.ServeAgent`
  with `WithPromptOverlapPolicy(PromptOverlapConcurrent)` among its options (`acpserver/serve.go:45-47`).
  * Flags: `--login`, `--device`, `--mode`, and the global `--log-level` and `--set PATH=VALUE`. There is no `--model`,
    `--provider`, `--cwd` or `--config` under `acp`. Only global settings and `--set` apply; a project's own settings
    do not (`acp.go:38-40`).
  * stdout carries JSON-RPC only. Under `acp` logs go to `<state>/logs/gobble.log`. **(run)** stderr was empty, and the
    process exited 0, 5 ms after stdin closed. `acpserver/binary_test.go:128-130` asserts the empty stderr.
  * At start it writes `settings.yaml` (and its lock) if absent (`acp.go:28`). It also starts a model listing for
    every provider with a credential: network, no generation, bounded at 10 s by the SDK (`internal/cli/agent.go:75`,
    `internal/cli/models.go:175-184`).
  * There is no self-update and no update check: production code imports only go-selfupdate-lib's `buildinfo`, which
    gobble's archtest enforces (`internal/archtest/check.go:203-214`).
  * `gobble --version` is not a flag (exit 2). The version is `gobble version` (`--json` exists). *Note,
    2026-11-07:* gobble-cli's `main` added a root `--version` flag after `v0.2.0` (`90b015a`, "add --version flag to
    every command"), which prints the same text as `gobble version`. No tag carries it yet, so at `v0.2.0` this fact
    stands, and so does D5.
* **`initialize`** **(run)**; `acpserver/agent.go:380-420`:
  * `protocolVersion: 1`;
  * `loadSession: true`, `mcpCapabilities: {http: true}`, `promptCapabilities: {embeddedContext: true}`. No image,
    audio or SSE;
  * `sessionCapabilities: {additionalDirectories, close, list, resume}`. No fork and no delete;
  * `_meta.gobble.extensions`: the twenty `_gobble/*` request names. No model or version data in `_meta`;
  * `agentInfo: {name: "gobble", title: "gobble", version: "0.2.0"}`;
  * `authMethods`: always one agent method per key provider, ids `gemini`, `openai`, `claude`, `grok`, `opencode-zen`,
    `opencode-go`, `huggingface`, `kilo`, `together`. Nine `terminal` methods (`<id>-login`) are added only when the
    client advertises `auth.terminal` (`acpserver/auth.go:32-53`, `acpext/auth.go:20-32`);
  * `initialize` itself makes no network call. A request sent before its reply is refused -32601 ("initialize has
    not completed").
* **`authenticate`** **(run)**; `acpserver/auth.go:55-70`. A presence check; no secret crosses ACP.
  * A known id with a credential: `{}`.
  * A known id without one: -32000 with data `{command, provider, reason: "no credential"}`, for example
    `{"command":"gobble auth login grok","provider":"grok","reason":"no credential"}`.
  * An unknown id: -32602.
* **Credentials** (`internal/auth/resolve.go:33-90`), in order: `<config>/auth.json` (which owns its provider when
  present), a `models.json` key, the provider's variable. The variables are `GEMINI_API_KEY`, `ANTHROPIC_API_KEY`,
  `OPENAI_API_KEY`, `XAI_API_KEY`, `OPENCODE_API_KEY` (both OpenCode providers), `HF_TOKEN`, `KILO_API_KEY` and
  `TOGETHER_API_KEY`. `<config>` is `GOBBLE_CONFIG_DIR`, else `GOBBLE_HOME`, else XDG (`internal/appdirs/dirs.go:31-77`).
* **The auth CLI** (`internal/cli/auth.go:27-31`, `:44-55`, `:264-386`). Each subcommand is non-interactive with the
  right flags:
  * `gobble auth login <provider> --api-key -` stores a key read from stdin. `--workspace ID` adds Claude's workspace.
  * `gobble auth login <provider> --method device --json` runs a device sign-in for grok or kilo. Its stdout is JSON
    events only, including `device_code {provider, url, code}` and `error {provider, kind, message}`
    (`auth.go:151-165`, `:235`, `:84`).
  * `--method browser` opens a browser (grok).
  * `gobble auth logout <provider>` removes an entry.
  * `gobble auth check --provider <id> --json` reports one provider: exit 0 ready, 1 without a credential, 2 on error.
  * `gobble auth status` lists every key provider's source as a table. It has no `--json`.
* **Sessions** (`acpserver/agent.go:429-1306`). `session/new`, `session/load` (replays, then answers), `session/resume`
  (no replay), `session/list` (with an optional `cwd`) and `session/close` (waits up to 5 s for a cancelled turn).
  `session/delete`, `logout` and `session/set_model` are -32601. `session/fork` is not implemented.
  * Ids are UUIDv7. Each session holds an exclusive writer lock. Reopening one that is open or locked is -32600.
  * State is Pi-compatible JSONL under `<data>/sessions/`, written once a session has its first message.
  * **(run)** Before each session response come `available_commands_update`, `current_mode_update`, `usage_update`
    and the `_gobble/personality` notification, in that order (`acpserver/commands.go:730-743`).
  * MCP: http (and stdio) servers are taken, merged with `<config>/mcp.json`.
* **Config options** (`acpserver/config.go:187-305`):
  * `model`, category `model`, a **grouped** select: up to five `recent` picks, then one group per usable provider,
    with the provider id as the group id. Values are `provider/model` refs.
  * `thought_level`, category `thought_level`, ungrouped: the model's levels, from `off` to `max`.
  * **(run)** With no credential there is no `model` option; only `thought_level` came back.
  * `session/set_config_option` takes select values only. A `provider/model` ref switches provider. The answer carries
    the full `configOptions`. `/model` and `/thinking` send `config_option_update`.
  * The list comes from go-llmprovider-sdk `catalog.List` per provider, with gobble's own last-good cache.
* **Modes** (`acpserver/modes.go:16-46`): `default`, `accept-edits`, `plan`, and `bypass` only when
  `permissions.allowBypass` is set. **(run)** Three were listed, current `default`. A mode is not persisted.
* **Prompts.** Text, embedded `resource` and `resource_link` blocks. Image and audio are dropped. Stop reasons are
  `end_turn`, `max_tokens`, `max_turn_requests`, `cancelled` and `refusal`. A missing or refused credential is -32000
  with data `{error}`; other failures are -32603 (`acpserver/prompt.go:270-278`). A second prompt during a turn is
  -32600 ("a prompt is already running"), and the running turn is not cancelled.
* **Updates.** Message and thought chunks (coalesced at 50 ms or 4 KiB), `tool_call` and `tool_call_update` (title at
  most 60 runes, summary at most 400 bytes, diffs, locations), `plan`, `usage_update` (with USD cost when priced),
  `user_message_chunk`, `available_commands_update`, `current_mode_update`, `config_option_update` and
  `session_info_update`'s title.
* **Permissions.** `session/request_permission` is gobble's only request to the client. Option ids are gobble's
  (`allow_once`, `allow_session`, `allow_project`, `allow_always`, `reject_once`, `reject_session`, `reject_project`,
  `reject_always`) with the standard kinds (`acpserver/question.go:56-85`). Grants, `/trust` and the level-5
  personality switch use it too, with their own option ids.
* **Files and terminals.** Client files only when the client advertises both `fs` capabilities; a client terminal
  only when it advertises `terminal` and the host sets `tools.bash.useClientTerminal` (default false). Otherwise gobble
  works locally, confined to `cwd` and `additionalDirectories` (`agent.go:720-728`).
* **Commands.** `available_commands_update` lists eighteen built-ins: help, compact, usage, context, session, model,
  providers, models, thinking, mode, plan, name, fork, clone, todos, personality, trust and permissions. Then come the
  prompt templates (`acpserver/commands.go:30-70`). `/skill:<name>` runs and is not advertised.
* **The `_gobble/*` contract** (`acpext/methods.go`, `acpext/types.go`; served from `acpserver/extensions.go:30-75`):
  * twenty requests, among them `_gobble/set_session_name {sessionId, name} → {}` and
    `_gobble/clone {sessionId} → {sessionId}`, plus steer, follow_up, clear_queue, the two queue modes, compact, usage,
    fork, get_state, get_messages, get_entries, get_last_assistant_text, get_session_stats, get_fork_messages,
    set_auto_retry, set_auto_compaction, abort_retry and set_personality;
  * three notifications: `_gobble/retry` (`RetryNotice`), `_gobble/compaction` (`CompactionNotice`) and
    `_gobble/personality` (`PersonalityNotice`);
  * `ErrorData {command, error, provider, reason, sessionId}`, `CapabilitiesMeta`, `MetaKey = "gobble"`,
    `AuthProvider` and `AuthProviders()`, `LoginSuffix = "-login"`;
  * `acpext/testdata/`: 34 recorded frames, one per params, result and notification shape.
  * *A2 (2026-11-09): from gobble `v0.4.0`, a client that offered `session.compaction` gets each compaction as ACP's
    standard `compaction_update` and never `_gobble/compaction`, which goes only to a client that did not offer it.
    acpext `v0.2.0` keeps `MethodCompaction` and `CompactionNotice` for those clients, and adds `CompactionMeta`, sent
    under `_meta.gobble` of each `compaction_update`, with three more frames (37 in all).*
* **gobble's records.** `0042-MADR-acpext-extension-contract-module.md` is accepted (2026-11-06), and its
  `docs/reports/0042-REPORT-acpext-sdk-surface-and-magic-import.md` §4 lists what this record uses. `0043-MADR`
  protects main and the `acpext` tags. `0017-MADR` (a `cancelled` todo status and `_gobble/get_todos`) is proposed and
  not built. `0008-MADR` D19 item 4 still calls the `model` option ungrouped; the code groups it (0025 D5 superseded
  it).

### F4. gobble's findings about this repository

gobble-cli's `docs/reports/0009-REPORT-magic-cli-remote-findings.md` tracks M1 to M14 against this repository at
`9778cbc1` and `9b75e5f3`. They were not re-verified here. Their bearing on registration:

| Finding | gobble's status | Bearing |
| :--- | :--- | :--- |
| M1, the record named for the earlier agent | open | Stale: this record is gobble's. |
| M2, the old D2/D3 (op reporting, strict fallback) | open | Stale: 0208 P6 built both. |
| M3, six companion items | open | To triage with gobble-cli; none blocks registration. |
| M4, delivery behaviour | reference | — |
| M5, a log-backup collision (0157 D6) | open | Not a gobble path; to triage. |
| M6, `appdirs` tests skip on host state | open | Not a gobble path; to triage. |
| M7, the version pin ignores a prerelease | open | Intended: a prerelease warns (D5). |
| M8, a fork `SetLogger` race | open | Touches D9's fork; to re-check in the PLAN. |
| M9, "sixteen" methods; `instructions` vs `customInstructions` | open | Stale: twenty methods; acpext names them (D13). |
| M10, the SDK cancels a running prompt | reference | Superseded by `PromptOverlapConcurrent` (F3). |
| M11, a replayed plan dropped by dedupe | open | Touches `session/load` replay (D6); to re-check in the PLAN. |
| M12, a cold-start phone loses the plan | open | Phone; to triage. |
| M13, grok's cancelled todos show done | open | Its claim that gobble sends a `cancelled` status is not true at `v0.2.0` (`tool/builtin/todo.go:22`). |
| M14, no todo resync; `_gobble/get_todos` | open | `_gobble/get_todos` does not exist at `v0.2.0`. |

### F5. What changed since the second body

| Second body (2026-11-03) | Now |
| :--- | :--- |
| Gate 1: 0208 P7 not on master | Met: `DangerousModeIDs` exists (F1). |
| Gate 2: a gobble tag after `v0.1.1` | Met: `v0.2.0`, pushed. |
| Gate 3: one acp-go-sdk2 tag, `v1.31.2` | Met: both at `v1.34.0`. |
| Gate 6: an `acpext` tag on the proxy, requiring acp-go-sdk2 at or below the pin | Met, and simpler: `acpext/v0.1.0` requires nothing. |
| D10: registration in seven places | One descriptor and one line (0208 P8). |
| Confirmation 7: `go list -m all` names no go-llmprovider-sdk | False since 0235. The gobble package still may not import it (`TestSDKIsImportedOnlyByGrok`). |
| D3: Magic writes `<config>/auth.json` itself | gobble's CLI does it, with the key on stdin (F3). |
| D3: `AuthPreference` is the configured upstreams | The selection ignores which upstream has a credential (F1, G8). |
| D6: no gobble models before any session | After a host's first session, the saved catalog has them (0235). |
| D6: no `ListModels` | Without one, every cold picker open starts a gobble process that reports no model state (G10). |
| D4: no floor | Also: a floor would break `doctor`, since gobble has no `--version` (F1, F3). |
| — | gobble sends `_gobble/personality` before every session response. It is declined at Debug, as any unregistered notification. |
| — | gobble's `session/close` waits up to 5 s, and this repository's close waits 2 s (G11). |

### F6. The gates, now

| Gate (second body's D12) | Status |
| :--- | :--- |
| 1. 0208's P7 on master | Met. |
| 2. A gobble release tag after `v0.1.1` carrying F3's surface | Met: `v0.2.0`. |
| 3. Both repositories on one acp-go-sdk2 tag | Met: `v1.34.0`. |
| 4. The unit tests pass, each seen failing first; `live-gobble` on the owner's word | This repository's work: the PLAN. |
| 5. The other providers' suites pass unchanged | This repository's work: the PLAN. |
| 6. An `acpext` tag on the proxy, built into the gate-2 release | Met: `acpext/v0.1.0`, unchanged in `v0.2.0`. |
| 7 (new). D4's selection hook in `acpagent` | This repository's work: the PLAN. |

### F7. What is missing

* **G1. There is no gobble provider:** no id, section, Spec, descriptor or registration line.
* **G2. The phone's Rename and Fork reach no gobble operation.** They need `Ops.Rename` and `Ops.Fork`. The schema
  has no rename, and gobble does not serve `session/fork`.
* **G3. gobble's retry and compaction notices are declined.** A provider retry is a silent wait on the phone, and a
  compaction shows no progress. *A2 (2026-11-09): the compaction half no longer holds from gobble `v0.4.0`. Its
  `compaction_update` reaches the phone through 0190's standard path with no gobble code. Only the retry half, and
  `CompactionMeta`'s extra facts, which the adapter declines, are gobble's to handle.*
* **G4. Nothing pins gobble's wire in this repository:** no fixtures, no `live-gobble`.
* **G5. The phone cannot configure gobble's credentials:** no `AuthStatus`, `AuthCatalogList`, `SetCredential`,
  `ClearCredential` or `StartDeviceAuth` for gobble.
* **G6. gobble's `bypass` mode is not marked dangerous.** The field exists now; the gobble Spec must set it.
* **G7. This repository would hand-write gobble's JSON** without `acpext`. acpext closes it (D13).
* **G8 (new). The handshake would pick an upstream with no credential.** gobble advertises all nine agent methods
  whatever is stored. A fixed `AuthPreference` makes the selection choose its first entry, `gemini` say, even when only
  `openai` has a key. gobble answers that `authenticate` with -32000, and `Start` fails with
  `acp authenticate (gemini): …` instead of starting. With no credential at all it should fail `ErrACPAuthRequired`
  before any `authenticate`, and the fixed list cannot express that either.
* **G9 (new). `AuthStatus` has no fast source.** It rides on every `providers.list`. `gobble auth check` answers for
  one provider per process, nine processes for gobble's catalog. `gobble auth status` answers for all, but only as a
  table.
* **G10 (new). A cold picker would start a gobble process that learns nothing.** gobble's `initialize` carries no
  model state, so the adapter's default harvest spawns `gobble acp` and gets no catalog. On each cache expiry it does so
  again, and each spawn starts gobble's model listings.
* **G11 (new). The close bound is shorter than gobble's.** A close after a cancel can be killed at 2 s while gobble is
  still releasing the turn, for up to 5 s. The session file's lock is released by the process's death; whether the
  last entry of a cancelled turn can be lost was not read for this record.

## Decision Drivers

* Use what the shared adapter, the descriptor and the SDK already do. Build only G1 to G11.
* gobble's names and shapes come from gobble, through `acpext`. This repository keeps no copy (G7).
* gobble's credentials live in gobble's store and are written by gobble's own code. No secret is in mcremote's config,
  argv or logs, and none crosses ACP `authenticate`.
* Every command and menu the phone shows for a gobble session runs in gobble (0208 D7). gobble's commands pass through
  unchanged (0208 D9).
* `acpagent` names no vendor (0208 D2). A gap the adapter must close is closed by an agent-neutral hook.
* The contract is pinned by fixtures from a gobble release tag, never gobble's main branch (0186 D9).
* gobble is to become the default provider (owner, 2026-10-21; 0208 P9). Registration must not block that, nor wait
  for it.

## Considered Options

* **A. Register now, on today's adapter:** the second body's chosen option C with `acpext`, on the new baselines,
  with three changes: registration by descriptor; phone auth through gobble's own CLI; and a credential-aware handshake
  through one agent-neutral `acpagent` hook.
* **B. As A, without the handshake `authenticate`.** No `SafeAuthMethodIDs`; a missing credential is found at the
  first prompt, as gobble's -32000, presented `KindAuth`. No `acpagent` change.
* **C. As A, with gobble's native surface now:** steer and follow-up for input typed during a turn, gobble's queue
  controls, `_gobble/get_state`, and todos, per the owner's 2026-10-21 goal.
* **D. Wait for 0208's P9 and P10:** register gobble once the default order, capability-driven phone menus and the
  shared conformance suite exist.
* **E. As A, but this repository reads and writes gobble's `auth.json` itself,** as grok's `AuthStatus` reads grok's
  files.

## Decision Outcome

Chosen option: **"A. Register now, on today's adapter"**, because:

* every external gate is met (F6);
* it closes G1 to G11 with one package, one registration line, one neutral hook, and at most one small gobble-cli
  change, which has a working fallback (D3);
* it keeps the second body's contract where that still holds.

B fails G8's purpose: a host with no gobble credential would open sessions that fail at the first prompt. C is a
product change to typed-ahead input that registration does not need. It is the next record (D15). D delays a working
provider for general work that gains gobble no safety, since the daemon already refuses an undeclared operation. E
copies gobble's credential resolution and Pi's file format into this repository, which acpext exists to avoid.

### D1. Identity and launch

* `provider.IDGobble`, wire id `"gobble"`, in `internal/provider/provider.go`.
* A new `internal/provider/gobble`, as thin as grok's package, with:
  * `ID: provider.IDGobble`, `DefaultBin: "gobble"`, `DefaultArgs` returning `["acp"]`;
  * no `ModelArgs`: the model is chosen by `session/set_config_option` (D7);
  * no `--set` on the command line, and no `GOBBLE_*` variable set by the daemon. gobble reads its own global settings
    as the service account;
  * the default `StartTimeout` (30 s). gobble's `session/new` can wait up to 10 s for its run provider's listing (F3).
    The PLAN measures a cold start from `v0.2.0`, and a measured need for more is a deviation. *A1: from the target
    release.*
* The process model is the adapter's: one `gobble acp` per session, plus the warm spare when prewarm is on.

### D2. No client files or terminals

Unchanged from the second body (0186 Q3).

* `Spec.ClientCapabilities` is the default less `fs.readTextFile`, `fs.writeTextFile` and `terminal`. `auth.terminal`
  stays false, so gobble advertises no terminal method.
* gobble then reads, writes and runs locally, confined to the session's `cwd` (F3). Tool output reaches the phone in
  `tool_call_update` content.
* An `fs/*` or `terminal/*` request from gobble is refused by the SDK with -32601. A fixture test pins that gobble
  sends none (D12).
* *A2 (2026-11-09):* the Spec keeps the default's `session.compaction` and `session.notices`. Without
  `session.compaction`, gobble `v0.4.0` would send `_gobble/compaction`, which nothing here handles (D10), and without
  `session.notices` a paused automatic compaction would not reach the phone.

### D3. Phone provider-auth through gobble's own CLI

0186 Q4, as the second body's amendment of 2026-11-03 restored it, with the write path changed. The store is written
by gobble, not by this repository.

* **Catalog** (`AuthCatalogList`). `acpext.AuthProviders()`, in its order. An entry's `ID` is both the upstream id and
  the agent method id; its `Name` is the label. Source `static`.
  * Every upstream offers `api_key`. Claude's key takes an optional workspace input.
  * `oauth_device` for the providers `AuthProviders()` marks with a login: grok and kilo at `v0.1.0`.
  * `oauth_browser` for grok, marked unavailable (`host_oauth`, W3): gobble's browser login opens a browser on the
    host.
* **Writes.**
  * `SetCredential` runs `gobble auth login <id> --api-key -`, with the key on the child's stdin, never in argv, and
    `--workspace` for Claude's workspace. Exit 0 is success. A failure is reported without the key.
  * `ClearCredential` runs `gobble auth logout <id>`.
  * `StartDeviceAuth` runs `gobble auth login <id> --method device --json`. It reads the `device_code` event for the
    phone's `DeviceFlow` (URL and code), then waits for the process to end, surfacing an `error` event's message. The
    child runs with the daemon's environment, so it writes the store gobble sessions read.
  * gobble is not added to the Codex/grok transactional coordinator (`internal/daemon/credentials.go`). Its writes are
    gobble's own, under gobble's own lock on `auth.json` (`internal/auth/store.go:26`).
* **Status** (`AuthStatus`): presence per upstream, never the secret, fast enough for every `providers.list`.
  * **Preferred source:** one `gobble auth status --json` call, which gobble-cli would add. Its rows already exist as a
    table (`internal/cli/auth.go:324-336`). This is a request to gobble-cli, not a change here (Q1).
  * **Until then:** `gobble auth check --provider <id> --json` for each catalogued upstream, run concurrently, with
    the answer cached for a short TTL and refreshed after every write. The PLAN measures it.
* **`CredentialEnv`** is not set. gobble's variables end in `_API_KEY` or `_TOKEN`, which the stderr tail already
  scrubs by name (`internal/provider/stderrtail/stderrtail.go:49`).

### D4. A credential-aware handshake, through one agent-neutral hook

This closes G8.

* **The hook.** `acpagent.Spec` gains `AuthPreferenceFunc func(ctx context.Context) []string`. When set, the
  handshake uses its answer in place of `AuthPreference`. The selection rule itself is unchanged: the pin, else the
  first advertised and safe id of the preference, else the key method. Specs that do not set it behave exactly as
  today. It names no vendor.
* **gobble's answer** is the upstreams with a credential, in `acpext.AuthProviders()` order, from D3's status.
  * With one or more, the handshake sends `authenticate` with the first. gobble answers `{}`. That matches the provider
    gobble itself runs when its `defaultProvider` is unset: the first key provider with a credential, in registry
    order. When `defaultProvider` is set, gobble runs that provider. The `authenticate` still only confirms that a
    credential exists.
  * With none, the answer is empty, nothing is selected, and `Start` fails `ErrACPAuthRequired` before `session/new`.
    The phone already handles that error: open settings, add a key or finish a device flow, and start again.
* `SafeAuthMethodIDs` is the `ID`s of `acpext.AuthProviders()`. `HasHeadlessCredential` reports whether D3's status
  has any credential. `KeyAuthMethodID` is not set.
* `providers.gobble.auth_method_id`, when set, must be one of those ids. Validation refuses any other value by name.
* The data of a -32000 is read as `acpext.ErrorData`. The phone's message names `ErrorData.Command` when it is set.
* A prompt that still meets -32000 (a key removed after the handshake) is presented `KindAuth` by `agenterr`, as for
  any agent.

### D5. Version

* `KnownGoodVersion: "0.2.0"`, the tag the D12 fixtures are captured from. A mismatch warns and never refuses (0186
  Q5). A development build reports a prerelease or `0.0.0-dev…`, and warns. *A1 (2026-11-08): the tag is now A1's
  target release, not `v0.2.0`, so this PLAN is the one that moves `KnownGoodVersion` past `v0.2.0` and revisits the
  floor below.*
* No `MinVersion`, and no descriptor `Floor`. A floor at a tag would refuse every development build of that release.
  `doctor`'s floor check runs `<bin> --version`, which gobble does not accept (F1, F3). Its next release will accept
  it (F3's note of 2026-11-07); the PLAN that moves `KnownGoodVersion` past `v0.2.0` revisits the floor.
* This repository never runs gobble's update. gobble has none (F3).
* The `acpext` version required is the one the `KnownGoodVersion` release was built with: `v0.1.0` for `v0.2.0`.
  *A1: the same rule holds for the target release. Every gobble release so far, and `main`, carries `acpext v0.1.0`.*
  *A2 (2026-11-09): no longer so. `v0.4.0` requires `acpext v0.2.0`, so that is the version for it.*
* *A2 (2026-11-09): **the supported minimum is gobble `v0.4.0`**, the first release that sends compaction as
  `compaction_update` (D10) and the first on this repository's acp-go-sdk2 tag (A1). It is stated here and in
  `docs/guides/config.md`, and not enforced: no `MinVersion` is set for it, since `MinVersion` counts a prerelease as
  below its floor (`acpagent/version.go:58-76`) and would refuse every development build. An older gobble still
  starts, warned by `KnownGoodVersion`, and shows no compaction progress. `v0.4.0` accepts `--version`
  (`internal/cli/version.go`), which is the input D5's floor revisit needs. That revisit stays with the PLAN.*

### D6. Session lifecycle

What the adapter already does with any Spec. With gobble's capabilities, that is:

* `session/new` with `cwd` and the configured MCP servers. gobble advertises `http`, so the SDK keeps `sse` out.
* `session/load`, since `loadSession` is true. A fork loads the clone's own session id in a new process (F1). The
  source's process still holds only the source's lock, so gobble's lock does not refuse it. The PLAN confirms that no
  other path loads a session whose process is still open.
* `session/list` and `session/close`. `session/delete` falls back to close.
* Not called: `session/resume` and `session/fork`.
* **The close bound (G11).** The PLAN reads what gobble writes between a cancel and the lock's release. If a kill at
  2 s can lose a cancelled turn's last entry, the fix is an agent-neutral `Spec.CloseTimeout` set to 5 s for gobble
  (Q6). Otherwise the 2 s bound stands.
* Prompts carry text only. gobble advertises no image or audio, so the SDK's gate drops them.
* The daemon's FIFO never sends a second `session/prompt` during a turn, so gobble's -32600 overlap refusal is never
  met.

### D7. Model and thinking

* **The standard path, unchanged** (0184 D6). The phone's model and thinking settings, and `/model`, send
  `session/set_config_option` to gobble's `model` and `thought_level` options, found by category. No
  `SetModelFallback`, `SetThinkingFallback`, `HarvestSessionMeta`, `ConfigureSession`, `SessionMeta` or
  `InitializeMeta`.
* **`ModelsFromOption` is set.** The picker's catalog is the session's grouped `model` option, and each value is
  gobble's `provider/model` ref. gobble's `recent` group duplicates entries from the provider groups. The PLAN fixes
  whether the picker keeps it as a group or drops it.
* **`ListModels` is a no-op** that answers an empty catalog, as opencode's does (G10). No process starts to read a
  catalog that `initialize` does not carry.
* **No `StaticModels`.** Before a host's first gobble session, the picker has no gobble models and takes free text.
  After it, 0235's saved catalog serves the last one.
* **`ModelProviders`** (the vendor step) is not set at registration. The groups already show the providers.

### D8. Modes

* gobble's modes come from its session answers: `default`, `accept-edits`, `plan`, and `bypass` when the host allows
  it.
* `DefaultModeID: "default"`, `SynthesizeAutoMode: false`.
* `DangerousModeIDs: ["bypass"]` (G6). The phone confirms it, as it confirms grok's synthetic `auto`.

### D9. Commands and the two operations

Unchanged from the second body, written with acpext.

* The command table declares nothing gobble advertises. Each built-in and template resolves `KindNative` and passes
  through as typed (0208 D9), including `/compact [focus]`, `/usage`, `/context`, `/model`, `/thinking`, `/mode`,
  `/plan`, `/fork [entry]`, `/clone`, `/name`, `/todos`, `/trust` and `/permissions`. `help`, `new`, `sessions` and
  `clear` stay the daemon's.
* `CommandCaveat` says that gobble's skills run as `/skill:<name>` prompt text and are not listed.
* **`Ops.Rename`** calls `acpext.MethodSetSessionName` with `acpext.SetSessionNameParams`. gobble answers `{}` and sends
  `session_info_update`, which the adapter maps to the title.
* **`Ops.Fork`** calls `acpext.MethodClone` with `acpext.SessionParams` and decodes `acpext.CloneResult`. The result's
  `AgentSessionID` is the clone's id and `ForkedFromID` the source's. `ForkOptions.LastTurnID` is ignored, as grok's
  fork ignores it: gobble copies the whole session. A fork before an earlier entry is `/fork [entry]`, passed through.
* Both claim the turn, so each is refused -32600 during a prompt. The daemon reports that refusal as the operation's
  error.
* No other op is set. The gobble package holds no `"_gobble/…"` literal.

### D10. Two notification handlers

Unchanged from the second body.

* `_gobble/retry` (`acpext.MethodRetry`, `acpext.RetryNotice`): `Host.Notice` at `start`, naming the attempt, the wait
  and the cause; a notice at an `end` with an `error`; nothing at a successful `end`.
* **A cause this repository does not know** (A1, 2026-11-08). `acpext.RetryCause` is an open set: acpext defines twelve
  constants, and its own doc says "a client should take a cause it does not know, which a later gobble may add". The
  handler treats an unknown cause exactly like a known one: the notice is produced, and names the cause as gobble sent
  it. If the handler gives known causes friendlier wording, an unknown cause falls back to its raw value. It is never
  dropped, refused or logged as an error.
* ~~`_gobble/compaction` (`acpext.MethodCompaction`, `acpext.CompactionNotice`): `Host.UpdateCompaction`, in progress at
  `start`, then completed or failed. One compaction id per start, kept in `Spec.NewSessionState`.~~ *A2 (2026-11-09):
  no `_gobble/compaction` handler, and no compaction id in the session state. gobble `v0.4.0`, the supported minimum
  (D5), sends this repository `compaction_update` instead, which 0190's path shows: one `in_progress` update, then a
  terminal one with the same id, the summary in its `summary` field. An older gobble's `_gobble/compaction` is
  declined at Debug, as `_gobble/personality` is.*
* *A2 (2026-11-09): **gobble's compaction facts.** `_meta.gobble` on a `compaction_update` is decoded as
  `acpext.CompactionMeta`, through `Spec.MetaHandlers` under `acpext.MetaKey`, which the adapter extends to
  `compaction_update` (A2).*
  * *A live completed compaction carries both `reason` and `tokensBefore`. For it, the handler sends one
    `Host.Notice`, after the compaction's own event, naming the reason and the tokens before. `reason` is an open set,
    like `RetryCause`: a reason this repository does not know is named as gobble sent it, never dropped.*
  * *Every other combination sends nothing. An `in_progress` or `failed` update carries `reason` alone, and the card
    already shows that compaction's state and any error. A replayed `completed` update carries `tokensBefore` without `reason`, and must not notify
    again. A `paused` failure is already a `warning` notice of gobble's own, through `session.notices`.*
  * *A value that does not decode is declined at Debug, never an error.*
* `_gobble/personality` is not registered and is declined at Debug.

### D11. Registration, by descriptor

0208 P8's shape, with no other place touched:

* **The package.** `gobble.go` (the Spec), `section.go` (a `Section` embedding `config.ACPProviderConfig`, its
  defaults and validation, and `Descriptor()`), `auth.go` (D3, D4), `ops.go` (D9), `notify.go` (D10), and their tests.
  *A2: `notify.go` holds the `_gobble/retry` handler and the `CompactionMeta` handler, and no `_gobble/compaction`.*
* **The descriptor.** `ID`, `Name: "gobble"`, `Section`, `New`. No `Floor` (D5) and no `Credentials` (D3).
* **The list.** One line in `registered.Descriptors()`. Its position is Q5.
* **The closed lists P9 has not removed yet:** `config.KnownProviderIDs` gains `"gobble"`, so the phone's prewarm
  toggle works for it.
* **The shipped configs.** `configs/config.example.yaml`, `config.prod.example.yaml` and `config.mesh-grok.yaml` gain a
  commented `providers.gobble` block, as P8 gave every provider. `TestTheShippedConfigFilesLoadThroughTheSections`
  loads them.
* **`docs/guides/config.md`** gains gobble's rows: its keys, `auth_method_id`'s allowed values, that the phone writes
  gobble's store through gobble's CLI (D3), and that `bypass` needs gobble's own `permissions.allowBypass`. *A2: and
  that gobble `v0.4.0` is the supported minimum, an older one showing no compaction progress (D5).*
* **Tests that list providers:** `internal/command/resolve_order_test.go`,
  `internal/provider/auth_conformance_test.go` and `internal/provider/provider_test.go`.
* **The phone.** gobble's turkey icon (Q4, as answered on 2026-11-07), `gobble.svg`, with `gobble` in the vendor icon
  manifest. No Dart branch on the provider id is added.
  * The icon and its generator live in gobble-cli, as `assets/gobble.svg` and `scripts/gen_icon.py` (gobble-cli
    `0044-MADR-publish-gobbles-icon-and-its-generator.md`). This repository holds no generator and draws no icon.
  * `tools/vendor-icons/sync.sh` gains a second pinned source beside `LOBE_ICONS_VERSION`: `GOBBLE_ICON_REF`, a gobble-cli
    `v*` tag. It downloads `assets/gobble.svg` from GitHub's raw content at that tag, after the Lobe fetch has rewritten
    the directory, and adds `gobble` to the manifest. The committed result is a plain file, like every other bundled
    icon. Changing the turkey means a gobble-cli release, then a bump to `GOBBLE_ICON_REF` and a re-run of the sync.
  * Until a gobble-cli tag carries the file, gobble shows the phone's monogram, and the PLAN's icon step waits for that
    tag. Registration does not wait for it.
* **Make:** `live-gobble`, its `.PHONY` entry and the build-tag vet.
* **`go.mod`:** `github.com/maccavelli/gobble-cli/acpext v0.1.0`, with no `replace`. Nothing else enters the module
  graph, since acpext requires nothing. *A2: `v0.2.0`, the version `v0.4.0` requires (D5). It still requires nothing.*
* **The default provider** is not changed here. That is 0208 P9's step 2 (Q3).

### D12. What pins the contract

* **acpext's own frames.** `acpext/testdata/` (34 files, at `v0.1.0`; *A2: 37 at `v0.2.0`, the three new ones
  `compaction.meta.start`, `.end` and `.paused`*) are gobble's recorded shapes. The gobble package's
  unit tests use them through acpext's types, with no capture and no quota.
* **Release fixtures,** captured with `internal/wirecap` from the installed `v0.2.0` into
  `internal/provider/gobble/testdata/wire/0.2.0/`. *A1: from the installed target release instead, into
  `testdata/wire/<its version>/`.* They cover `initialize`, `session/new`, `authenticate`, a prompt with
  a tool call, `/model`, `/thinking`, rename and clone idle and mid-turn, a retry and a compaction, a close then a load,
  and a prompt with no credential. *A2: the compaction is recorded as gobble sends it from `v0.4.0`: a
  `compaction_update` pair, `in_progress` then `completed`, each with `_meta.gobble`, and after the load the replayed
  `completed` update with the same id.* The capture needs a credential and spends quota, so it runs on the owner's word.
  Frames are scanned for identifiers before they are committed.
* **Unit tests in the gobble package,** on `acpagent/acptest` and `Provider.StartOn`, each seen failing first on a
  planted break:
  1. `initialize` advertises no `fs`, no `terminal` and no `auth.terminal`; *A2: and does advertise
     `session.compaction` and `session.notices` (D2);*
  2. with a stored credential for one upstream, `authenticate` names that upstream; with none, `Start` fails
     `ErrACPAuthRequired` and sends no `authenticate` and no `session/new`;
  3. the session declares `CapRename` and `CapFork`, and model and thinking from the options, and no other Ops-backed
     capability;
  4. Rename and Fork send exactly D9's frames and map the answers; during a prompt they return gobble's refusal;
  5. the two notifications produce D10's events, including a `_gobble/retry` whose `cause` is none of acpext's twelve
     constants, which still produces its notice naming that cause (A1); *A2: one notification, `_gobble/retry`. A
     `_gobble/compaction` produces no event;*
  6. a no-credential prompt is presented `KindAuth`, with `ErrorData.Command` in the text;
  7. a recorded session sends nothing outside D6, D7 and D9: no other `_gobble/*`, no `_x.ai/*`, no `fs/*` or
     `terminal/*` answer; *A2: and gobble sends no `_gobble/compaction`;*
  8. `AuthCatalogList` equals `acpext.AuthProviders()`. `SetCredential` passes the key on stdin and never in argv.
     `StartDeviceAuth` for a provider without a login is `ErrAuthMethodUnsupported`. These run against a scratch
     `GOBBLE_HOME` and a stub `gobble` binary, never the host's store;
  9. every captured `_gobble/*` frame, and `initialize`'s `_meta.gobble`, decodes into its acpext type with unknown
     members refused, and encodes back to the same JSON; *A2: so does each captured `compaction_update`'s
     `_meta.gobble`, as `acpext.CompactionMeta`;*
  10. only `internal/provider/gobble` imports acpext, and it imports no go-llmprovider-sdk, by source test;
  11. `ListModels` starts no process (G10);
  12. *A2:* the `CompactionMeta` handler (D10): a live `completed` meta, `reason` `manual` and a `tokensBefore`, sends
      one notice naming both, after the compaction event; a reason none of acpext's constants names is named as sent;
      a replayed `completed` meta (no `reason`), an `in_progress` one and a `paused` one send none; a meta that does
      not decode is declined, with no notice and no error.
* **`acpagent`'s new hook** (D4) gets its own test, with vendor names kept out: a Spec with `AuthPreferenceFunc`
  answering `["vendor.b"]` authenticates `vendor.b` though `AuthPreference` lists `vendor.a` first; answering nothing
  fails `ErrACPAuthRequired`. It is seen failing with the hook ignored.
* *A2: **`acpagent`'s compaction meta source** gets its own test, vendor names kept out: a Spec with a `MetaHandler`
  for `vendor.a` receives that key's value from a `compaction_update` with `MetaFromCompaction`, after the
  compaction's event; a key with no handler is still declined, and the compaction event is unchanged. It is seen
  failing with the source not wired.*
* **`live-gobble`,** against the installed tag, on the owner's word, since it spends quota: `/model` and `/thinking`
  producing `session/set_config_option`, the grouped catalog, a phone rename and fork, `/compact` passed through (*A2:*
  shown as a compaction, with one notice naming its reason and tokens before), a turn
  with a tool call, a recorded connection with no `fs/*` or `terminal/*`, `authenticate` after a stored key, and a
  phone key write that opens no session.

### D13. gobble's `acpext` module

The second body's D13, with its facts updated.

* `github.com/maccavelli/gobble-cli/acpext`, accepted in gobble-cli's `0042-MADR`, tagged `acpext/v0.1.0` and served by
  the proxy. It holds gobble's method names, params and results, `CapabilitiesMeta`, `MetaKey`, `AuthProvider`,
  `AuthProviders()`, `LoginSuffix` and `ErrorData`. It holds no behaviour, and **it requires no module at all**.
* This repository uses `AuthProviders()` and `ErrorData` (D3, D4), `MethodSetSessionName`, `SetSessionNameParams`,
  `MethodClone`, `SessionParams` and `CloneResult` (D9), `MethodRetry`, `RetryNotice`, `MethodCompaction`,
  `CompactionNotice` and their constants (D10), and `CapabilitiesMeta` and `MetaKey` in the fixture test (D12).
  *A2 (2026-11-09): not `MethodCompaction` or `CompactionNotice`. In their place `CompactionMeta`, its
  `CompactionReason` and `CompactionPause` constants, and `MetaKey` as the handler's key (D10). The tag is
  `acpext/v0.2.0` (D5).*
* Only `internal/provider/gobble` imports it (D12 item 10). `acpagent` stays agent-neutral.
* The release fixtures stay: acpext says what gobble means to send, a capture shows what a release sends, and item 9
  ties the two.
* A bump is a dependency change under a PLAN, with fixtures re-captured from the gobble release that carries it. acpext
  is gobble-cli's, changed only through gobble-cli's records, never through a `replace` or a fork.

### D14. The gates, restated

gobble is registered when all of these hold:

1. 0208 P7 on master. **Met.**
2. A gobble release tag after `v0.1.1`. **Met: `v0.2.0`.** *A1: the release must also meet gate 3; it is A1's
   target release.* *A2 (2026-11-09): **met by `v0.4.0`**, which is also the supported minimum (D5).*
3. Both repositories on one acp-go-sdk2 tag. ~~**Met: `v1.34.0`.**~~ A later bump on either side is 0184 D12's
   lockstep. **Not met since 2026-11-08 (A1):** this repository pins `v1.34.2` (0217 P3g), and every gobble release
   pins `v1.34.0`. gobble-cli's `main` pins `v1.34.2`, so the gate is met again at the first gobble release cut from
   it. *A2 (2026-11-09): **met again**: gobble `v0.4.0` and this repository both pin `v1.34.2`.*
4. D12's unit tests pass, each seen failing first; `live-gobble` passes on the owner's word.
5. grok's, opencode's, kilo's and codex's suites pass unchanged.
6. An `acpext` tag on the proxy, the one the gate-2 release was built with. **Met: `acpext/v0.1.0`.** *A2: for
   `v0.4.0`, **met: `acpext/v0.2.0`**, served by the proxy (`253e366`, 2026-11-08).*
7. D4's `AuthPreferenceFunc` is in `acpagent`, with its test.

### D15. Not in this registration

Each needs its own record:

* **gobble's native surface** (owner, 2026-10-21: "gobble's whole surface is first-class"): `_gobble/steer` and
  `_gobble/follow_up` for input typed during a turn in place of the daemon's queue, the queue modes and `clear_queue`,
  `_gobble/get_state` for the phone's session panel, `_gobble/set_auto_retry`, `abort_retry` and
  `set_auto_compaction` as phone controls, and personality. It is the next record after this one (Q2).
* **Todos.** gobble-cli's `0017-MADR` (`cancelled` and `_gobble/get_todos`) is not built (F3, F4 M13 and M14).
* **gobble as the default provider** (0208 P9, Q3).
* **Image and audio input,** which gobble does not advertise.
* **Tool-call `locations`,** which gobble sends and the adapter does not read. That is general.

### D16. Relation to 0185 and 0235

* 0185's "No dependency" bullet was superseded in part by 0235 D5, for `internal/provider/grok`. The gobble package
  does not import go-llmprovider-sdk. `TestSDKIsImportedOnlyByGrok` already refuses it, and D12 item 10 restates it.
* acpext is not go-llmprovider-sdk and does not bring it in: it requires nothing.

### A1. Amendment, 2026-11-08: unknown retry causes, and the SDK gate after 0217 P3g

Made on the owner's request ("amend 0179 for the two #36 gaps"). Issue #36 compared this record's second body with
the published `acpext v0.1.0`. The third body covers its checklist, except for two gaps.

**Gap 1: a retry cause this repository does not know.** #36 item 5 says `RetryCause` is an open set, and that a
handler should take a cause it does not know. acpext's doc comment at `v0.1.0` says the same. D10 and D12 did not say
it, and no test pinned it. A1 adds D10's last bullet, and widens D12 item 5 to cover it. Decoding already works,
because `RetryCause` is a `string`. The risk was a handler that maps causes through a fixed table and drops a cause
outside it.

**Gap 2: the SDK gate no longer holds.** The third body found gate 3 met, with both repositories on acp-go-sdk2
`v1.34.0`. On 2026-11-07, 0217 P3g moved this repository to `v1.34.2`. gobble released `v0.3.0` and `v0.3.1` the same
day, and both still pin `v1.34.0`; only gobble-cli's `main` pins `v1.34.2` (Baselines, re-read). No gobble release now
shares this repository's SDK tag, so the third body's capture target, `v0.2.0`, fails gate 3.

The options for gap 2:

* **Wait for a gobble release on this repository's SDK tag, and target it (chosen).** It keeps 0184 D12's lockstep. It
  costs nothing here but time, and gobble-cli's `main` already has the pin.
* **Capture `v0.2.0` anyway.** Rejected: it breaks gate 3. The fixtures and `live-gobble` would pin a build whose
  schema and capability gates differ from the ones this repository compiles against.
* **Move this repository back to `v1.34.0`.** Rejected: it undoes 0217 P3g's bug fixes, including G14's `null`
  handling, for a record that has not started.

**The decision.** The **target release** is the newest gobble release that pins the same acp-go-sdk2 tag as this
repository when the PLAN's P2 starts. It replaces `v0.2.0` wherever the third body names `v0.2.0` as the release to
build against: D5's `KnownGoodVersion`, D12's release fixtures and their directory, `live-gobble`, and Confirmation 5.
D5's floor is revisited for it, as D5 already required of the PLAN that moves past `v0.2.0`. F3's facts were read at
`v0.2.0`, so they are re-read at the target release before P2's code is written. A difference is a deviation under
the PLAN's C11. If this repository's SDK pin moves again before P2, the target moves with it.

**Not changed:** go-llmprovider-sdk is not a gate. The gobble package does not import it (D16), so gobble's pin of it
does not have to match this repository's.

### A2. Amendment, 2026-11-09: gobble sends compaction as standard updates (issue #37)

Made on the owner's request ("amend 0179 for #37"). Issue #37 reports gobble-cli's `0016-PLAN` P3c (`3d6fb65`).
The owner's comment on it adds that gobble-cli `v0.4.0` (`2e83cce`, 2026-11-08) is the first release that carries it.

**The findings** [observed 2026-11-09; Baselines, A2's re-read]:

* **What `v0.4.0` sends a client that offered `session.compaction`.** This repository's default capabilities offer it
  (F1), and D2 keeps it. gobble picks one channel per connection: such a client gets ACP's `compaction_update` and
  never `_gobble/compaction`. A live compaction is one `in_progress` update, then one terminal update with the same id.
  A `completed` update carries the summary in its `summary` field, with no `compaction_summary_chunk`. A `failed` one
  carries the error's first line. `cancelled` comes only when the cancel came before the entry was written. A
  `session/load` replays the compaction as one `completed` update with the live id. A refusal ("Nothing to compact")
  sends no update (issue #37).
* **What this repository already does with it.** 0190's fold shows each compaction by id: a first-seen id is in
  progress, and a terminal update's `summary` replaces the text (`acpagent/compaction.go:97-120`). A `_meta` on the
  update is declined (`compaction.go:103-104`), so gobble's extra facts are dropped, harmlessly.
* **gobble's extra facts.** `_meta.gobble` is `acpext.CompactionMeta` (acpext `v0.2.0`), always the whole object:
  `reason` (`manual`, `threshold`, `overflow`) on each live update; `tokensBefore` on a `completed` one, live or
  replayed; `paused` (`turn`, `session`) on a `failed` one that paused automatic compaction. A pausing failure is also
  a `warning` notice, through `session.notices`.
* **The versions.** `v0.4.0` pins acp-go-sdk2 `v1.34.2`, this repository's tag, and `acpext v0.2.0`, which the proxy
  serves and which still requires no module. `v0.3.1` and earlier pin `v1.34.0` and send `_gobble/compaction`.
  `v0.4.0` accepts a root `--version`.
* **Where the record stood.** No gobble package exists yet (`internal/provider/gobble` is absent), so nothing built
  has to change. F7 G3's compaction half, D10's `_gobble/compaction` handler, D11's `notify.go`, D12's live list,
  D5's acpext note and D12's frame count are stale.

**The owner's answers**, 2026-11-09, asked with options:

* *D10's handler (#37 item 2): "No handler; v0.4.0 minimum (Recommended)". Not chosen: enforce `MinVersion = "0.4.0"`,
  which refuses development builds; keep the handler for releases before `v0.4.0`.*
* *`CompactionMeta` (#37 item 5, optional): "Decode it in registration". Not chosen (had been recommended): leave it
  to a later record, beside D15's native surface.*

**The decisions**, each written in place above and marked *A2*:

* **No `_gobble/compaction` handler** (D10), and no compaction id in `Spec.NewSessionState`. gobble's compactions reach
  the phone through 0190's standard path.
* **gobble `v0.4.0` is the supported minimum** (D5), stated and not enforced. It is also, today, A1's target release:
  the newest gobble release on this repository's SDK tag. If a later release is cut before P2 starts, A1's rule picks
  it.
* **D2 keeps `session.compaction` and `session.notices`,** and D12 item 1 checks that `initialize` advertises them.
* **`CompactionMeta` is decoded, through one agent-neutral change to `acpagent`.** `MetaSource` gains
  `MetaFromCompaction` (`"compaction_update"`). `onCompactionUpdate` passes the update's `_meta` to the Spec's
  `MetaHandlers` after emitting the compaction's event, and declines only a key with no handler, as
  `session_info_update` already does (`acpagent/session.go:2120-2121`). No Spec field is added, and the event the phone
  receives is unchanged.
* **gobble's handler** (D10) sends one notice for a live completed compaction, naming its reason and the tokens
  before. Nothing else notifies: the card shows progress and errors, a replay must not repeat the notice, and a pause
  is gobble's own warning.
* **The rest follows:** acpext `v0.2.0` in `go.mod` (D11); D13's list of names; D12's frame count, release fixtures,
  items 1, 5, 7 and 9, a new item 12, and a test for the `acpagent` change; D14's gates 2, 3 and 6, met again.

**Options weighed for showing the facts**, within the owner's answer:

* **One notice from the Spec's handler (chosen).** Good, because it needs one `MetaSource` constant and no change to
  the event, the protocol or the phone. Bad, because the reason appears as a line beside the compaction's card, not on
  it.
* **New compaction fields on the event and the card** (`compaction_reason`, `compaction_tokens_before`). Good, because
  the card would carry the facts. Bad, because it changes `protocol-v1.md`, the daemon's event and the phone, for one
  agent's facts, which is D15's kind of work rather than registration's.

**Consequences of A2:**

* Good, because gobble's compactions show on the phone with no gobble code on the main path, and the code that would
  have handled `_gobble/compaction` is never written.
* Good, because the version D5 targets, the SDK gate and acpext's tag now line up on one released gobble.
* Neutral, because an older gobble still runs, without compaction progress, and nothing refuses it.
* Bad, because `acpagent` gains one `MetaSource` for one agent's need. It names no vendor, and any agent that puts
  facts in a compaction's `_meta` can use it.

### Consequences

* Good, because every gate outside this repository is met, and the work left is one package, one registration line,
  one neutral hook and their tests.
* Good, because gobble's names, shapes, auth providers and error data come from gobble's own module, and its credential
  store is written by gobble's own CLI. This repository holds no copy of either.
* Good, because the phone can add an API key or finish a grok or kilo device sign-in before any gobble session exists,
  and the key never appears in argv.
* Good, because a host with no gobble credential fails at `Start` with the error the phone already handles, and a host
  with one authenticates the upstream that has it.
* Good, because after a host's first gobble session the picker lists gobble's models with no gobble process running
  (0235), and no picker open starts a process that cannot list them (G10).
* Good, because every gobble command reaches gobble as typed.
* Neutral, because input typed during a turn waits in the daemon's queue and reaches gobble after the turn, as for
  grok. gobble's steer waits for D15's record.
* Neutral, because grok stays the default provider until 0208 P9.
* Neutral, because `AuthStatus` costs nine short `gobble auth check` processes per refresh until gobble-cli adds
  `auth status --json` (Q1). The cache bounds how often.
* Bad, because the phone's Fork copies the whole session; a fork before an earlier message is `/fork [entry]`, typed.
* Bad, because gobble's wire and CLI are owned by another repository. A change there reaches this provider at the next
  acpext bump or release, which the fixtures and acpext's types catch but do not prevent.
* Bad, because `acpagent` gains a hook for one agent's need. It is agent-neutral, and any agent that advertises one
  method per upstream would need the same.

### Confirmation

1. `provider.IDGobble == "gobble"`, the daemon starts `gobble acp` for it, and `mcremote doctor` lists it with no
   version check.
2. D12's eleven unit tests and the hook's test pass, each seen failing first on a planted break in a scratch copy.
   *A2: twelve, with the `CompactionMeta` handler's, and the `acpagent` compaction meta source's test beside the
   hook's.*
3. `TestNoProviderIsNamedInAcpagent` passes unchanged. No file outside `internal/provider/gobble` and D11's places
   names gobble.
4. A gobble session's `bypass` is confirmed on the phone.
5. `live-gobble` passes against ~~`v0.2.0`~~ A1's target release, on the owner's word.
6. The other providers' unit, conformance and wire suites pass unchanged, and `make race`, the Linux test host's
   `go test -race ./...` and `make ci-windows` pass.
7. `go.mod` requires `github.com/maccavelli/gobble-cli/acpext` at a tag, with no `replace`, and `go list -m all`
   gains only that module.
8. A phone key write for gobble, with the daemon's account, makes `gobble auth check` report the provider ready, and
   no key appears in any process's argv or in mcremote's logs.

## Pros and Cons of the Options

### A. Register now, on today's adapter

* Good, because it uses gobble `v0.2.0` and `acpext/v0.1.0` as they are, with no gobble-cli change required.
* Good, because phone auth uses gobble's own CLI, so gobble's resolution order and locking stay gobble's.
* Neutral, because one acpagent hook is added, agent-neutral and opt-in.
* Bad, because `AuthStatus` is slower than it could be until Q1's gobble-cli change lands.

### B. As A, without the handshake `authenticate`

* Good, because `acpagent` does not change.
* Bad, because a host with no credential opens a session that only fails at the first prompt.
* Bad, because it reverses the owner's 2026-11-03 amendment, which made the handshake part of the contract.

### C. As A, with gobble's native surface now

* Good, because typed-ahead input would steer the turn, and the phone would show gobble's queue and state.
* Bad, because it changes what typed-ahead input means for every gobble user. That is a product decision with its own
  phone work, larger than registration.
* Bad, because todos depend on gobble-cli work (0017) that is not built.

### D. Wait for 0208's P9 and P10

* Good, because the registration would land with capability-driven phone menus and a shared conformance suite.
* Bad, because P9 and P10 are not started, and gobble gains no safety from them: undeclared operations are already
  refused, and D11's registration is one line.

### E. This repository reads and writes gobble's `auth.json`

* Good, because `AuthStatus` would be a file read, as grok's is.
* Bad, because it copies Pi's `auth.json` format, gobble's resolution order (`auth.json`, `models.json`, the
  variable) and gobble's lock rules into this repository. A change in gobble would silently break it.
* Bad, because acpext was created so that this repository keeps no copy of gobble's contract.

## More Information

### Owner questions

Each has a recommended answer, which the decisions above assume.

*Answered by the owner on 2026-11-06:*

* *Q1: open an issue in gobble-cli. It is [maccavelli/gobble-cli#13](https://github.com/maccavelli/gobble-cli/issues/13),
  "auth status: add --json, so a remote client can read every provider's credential presence in one call". It offers
  three ways: `auth status --json` (recommended), `auth check --all --json`, and a `_gobble/auth_status` request. Until
  a release carries one, D3's fallback (`auth check` per upstream, concurrent and cached) applies.*
* *Q2: a follow-up record, after registration.*
* *Q3: 0208 P9 step 2, as its own phase after registration.*
* *Q4 and Q5: ask gobble-cli for its mark, with the generic icon until it arrives; register gobble right after `fake`.*
  *Q4 was then answered again: "Gobble gobble. Turkey. Bird. Cartoonish. An icon styled and optimized cartoon turkey is
  the icon. Make it." The icon is drawn here: a cartoon turkey in profile, monochrome on the 24-unit grid the bundled
  icons use, its detail in cut-outs so the phone's tint keeps it.*
* *Q4, 2026-11-07: the owner approved the turkey as drawn ("No tweaks required"), and asked for it and its generator
  to live in gobble-cli, with this repository importing the SVG rather than regenerating it. Asked four follow-ups,
  each with three options, the owner chose:*
  * *the location: gobble-cli's `assets/gobble.svg` and `scripts/gen_icon.py` (recommended; not chosen: inside
    `acpext`, `docs/brand/`);*
  * *the import: the raw file at a pinned `v*` tag (recommended; not chosen: the Go module proxy, a commit SHA);*
  * *who changes gobble-cli: the agent, under a gobble-cli MADR/PLAN pair, after approval (recommended had been an
    issue with the files attached). That pair is gobble-cli `0044-MADR-publish-gobbles-icon-and-its-generator.md`;*
  * *until a tag carries the file: the monogram, then the icon (recommended; not chosen: hold registration for the
    tag, pin to a commit on `main`).*

  *D11's phone bullet says how the file is fetched.*
* *Q6: decide after the PLAN reads gobble's close, with `Spec.CloseTimeout` as the fix if a kill can lose an entry.*

* **Q1. `AuthStatus`'s source.** Options: ask gobble-cli for `gobble auth status --json`, and use `auth check` per
  upstream until it lands; `auth check` per upstream only; read `auth.json` and the variables here (option E).
  Recommended: the first. It is one flag on a command that already computes the rows.
* **Q2. gobble's native surface** (steer and follow-up, queue, state, retry and compaction controls, personality).
  Options: a follow-up record right after registration; fold steer and follow-up into this one; leave the daemon's
  queue as the only path. Recommended: a follow-up record. Registration does not need it, and it changes what users get.
* **Q3. gobble as the default provider.** Options: 0208 P9's step 2 as its own phase, after registration; fold that
  step into this rollout; keep grok the default. Recommended: 0208 P9 step 2, after this registration, since it removes
  the grok-first rule for every provider, not only for gobble.
* **Q4. The phone's gobble icon.** Options: an SVG from gobble-cli, added through `tools/vendor-icons/sync.sh`; the
  phone's generic icon until gobble has one; draw one here. Recommended: ask gobble-cli for its mark, and use the
  generic icon until it arrives.
* **Q5. gobble's place in `registered.Descriptors()`.** Options: right after `fake`, ahead of grok; last; alphabetical.
  Recommended: right after `fake`. 0208 P9 orders the default "gobble, then the rest in registration order", and the
  list is where that order will come from.
* **Q6. The close bound** (G11). Options: decide after the PLAN reads gobble's close; an agent-neutral
  `Spec.CloseTimeout`, 5 s for gobble, now; keep 2 s. Recommended: decide after the read, with the hook as the fix if a
  kill can lose a cancelled turn's last entry.

### What gobble keeps for this contract

These are gobble-cli's, under its own records. This repository writes nothing in gobble-cli. The icon's records and
files there are gobble-cli's own pair, written on the owner's word (Q4, 2026-11-07), not this record's work.

* F3's surface at the tag D5 names. In particular the two requests and two notifications and their fields, the config
  option categories, the `bypass` id, `available_commands_update` before the session responses, the local fallback with
  no `fs` or `terminal`, the -32000 data, and the auth CLI's flags, exit codes and `--json` events.
* `acpext` at a pushed tag, served from by each release (gobble-cli `0042-MADR`, `0043-MADR`).
* **Requests to gobble-cli** arising from this record, each for gobble-cli to decide:
  * `gobble auth status --json` (Q1);
  * the phone's icon (Q4): drawn in this repository's session, and published by gobble-cli under its
    `0044-MADR-publish-gobbles-icon-and-its-generator.md`;
  * its 0009-REPORT's stale findings: M1, M2, M9, M10 and M13 (F4);
  * its 0008-MADR D19 item 4, which still calls the `model` option ungrouped.

### Relationship to other records

* [0208-MADR-make-the-provider-layer-agent-neutral.md](0208-MADR-make-the-provider-layer-agent-neutral.md): its D8
  narrowed this record to gobble. P6 to P8 are done and used here. P9 (the default order) and P10 (the conformance
  suite) are not prerequisites.
* [0186-MADR-integrate-current-library-releases-and-prepare-gobble.md](0186-MADR-integrate-current-library-releases-and-prepare-gobble.md):
  Q3 kept (D2), Q4 implemented (D3, D4), Q5 kept (D5).
* [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md):
  D6's standard model path, D12's SDK lockstep.
* [0074-MADR-remote-provider-auth-from-phone.md](0074-MADR-remote-provider-auth-from-phone.md): the phone's catalog,
  write and device-flow messages D3 fills.
* [0234-MADR-authenticate-only-an-id-the-agent-advertised-using-the-sdks-list.md](0234-MADR-authenticate-only-an-id-the-agent-advertised-using-the-sdks-list.md):
  the selection D4 extends.
* [0235-MADR-save-the-last-live-model-catalog-and-take-groks-fallback-from-the-sdk.md](0235-MADR-save-the-last-live-model-catalog-and-take-groks-fallback-from-the-sdk.md):
  the saved catalog D7 relies on, and D5's partial supersession of 0185.
* [0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md):
  opencode's Spec, the nearest pattern, and its no-op `ListModels`.
* `docs/reports/0179-REPORT-gobble-native-acp-integration-assessment.md`: the 2026-10-11 assessment. Its R2, R4, R5,
  R7 and R8 are carried here; R3 (acpext) is taken by D13; R6 (credentials) is decided by D3 and D4.
* [0179-PLAN-gobble-native-acp-provider.md](0179-PLAN-gobble-native-acp-provider.md): rewritten on 2026-11-07 for this
  body, as P0 to P6.
* Issue [#33](https://github.com/maccavelli/magic-cli-remote/issues/33): its item 4 is this record.
* gobble-cli: `0002-MADR`, `0008-MADR` (D19, D20), `0017-MADR`, `0025-MADR`, `0042-MADR` and its REPORT, `0043-MADR`, and
  `docs/reports/0009-REPORT-magic-cli-remote-findings.md`.

### History

* **2026-10-01.** First written, `proposed`, for the agent under its earlier name, on schema 0.13.5. 0208 later took
  its general parts, and 0184, 0189 and 0208's P1 to P6 built most of them.
* **2026-10-13.** Renamed for gobble with `git mv`, by 0186 Q2.
* **2026-10-14.** Notes on `usage_update`'s cost, by 0189.
* **2026-11-03.** Rewritten as gobble's contract on schema 1.25, from code read at `3489184b`, gobble-cli `9b5535a` and
  acp-go-sdk2 `v1.31.2`. Amended the same day twice: phone provider-auth with the handshake `authenticate` (0186 Q4
  restored), then gobble's `acpext` module (D13). That body is in this file's history at `e020cd54`. It chose its
  option C with acpext, and set six gates.
* **2026-11-06.** Rewritten again on the owner's request for a full assessment, from code read at `938b0002`,
  gobble-cli `v0.2.0` and `acpext/v0.1.0`, and acp-go-sdk2 `v1.34.0`. In summary (F5):
  * gates 1, 2, 3 and 6 are met;
  * registration is one descriptor;
  * phone auth goes through gobble's CLI, not a file this repository writes;
  * the handshake gains a credential-aware, agent-neutral hook (G8, D4, gate 7);
  * `ListModels` is a no-op (G10), and 0235's saved catalog serves gobble's models after the first session;
  * Confirmation's go-llmprovider-sdk item is replaced (D16);
  * G9, G11 and Q1 to Q6 are new.

  D1, D2 and D8 to D10 keep the second body's decisions. D3 changes its write path. D4 is new (the old D4 is now D5).
  D11 replaces the old D10. D12 extends the old D11. D13 is updated, and D14 to D16 are new.
* **2026-11-07.** Amended before approval. Q4 was answered again: the turkey and its generator live in gobble-cli, and
  `sync.sh` fetches the SVG at a pinned gobble-cli tag (D11). That replaces the committed local-icons directory and
  the generator this record had placed here. F3 and D5 note gobble-cli's untagged `--version` flag.
* **2026-11-07.** Accepted on the owner's "Approved to update 0179 madr and plan". The PLAN was rewritten the same day
  for this body, as P0 to P6.
* **2026-11-08.** Amendment A1, on the owner's "amend 0179 for the two #36 gaps":
  * D10 takes a retry cause it does not know, and D12 item 5 tests it;
  * gate 3 is no longer met after 0217 P3g, so the release to build against becomes the newest gobble release on this
    repository's acp-go-sdk2 tag (the target release), replacing `v0.2.0` in D5, D12, D14 and Confirmation 5.

  The Baselines gain a dated re-read. The third body's text is kept, with A1's changes marked in place.
* **2026-11-09.** Amendment A2, on the owner's "amend 0179 for #37", with the owner's answers to two questions:
  * gobble `v0.4.0` sends compaction as standard `compaction_update`, so D10 has no `_gobble/compaction` handler, and
    `v0.4.0` is the supported minimum, stated and not enforced (D5);
  * gobble's `CompactionMeta` is decoded in registration, through a `compaction_update` source for `MetaHandlers`
    in `acpagent`, and named in one notice per live completed compaction (D10);
  * acpext moves to `v0.2.0`, and gates 2, 3 and 6 are met again by `v0.4.0` (D5, D11, D13, D14).

  The Baselines gain a second dated re-read. The text before A2 is kept, with A2's changes marked in place.

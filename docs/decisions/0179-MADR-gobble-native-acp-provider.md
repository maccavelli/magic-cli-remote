---
status: proposed
date: 2026-11-03
decision-makers: Project Owner
consulted: "this repository at 3489184b, read from code; gobble-cli at 9b5535a and its tags v0.1.0 and v0.1.1, read from code, and at 67f72fb with its proposed 0042-MADR; acp-go-sdk2 v1.31.2, read from source; 0184, 0186 and 0208 and their PLANs; issue #33"
informed: gobble-cli, acp-go-sdk2
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Register gobble as a native ACP provider: a thin Spec over the shared adapter, two `_gobble/*` operations on gobble's `acpext` types, phone provider-auth, and no client files or terminals

## Context and Problem Statement

gobble is the platform's own coding agent, built in the `gobble-cli` repository. `gobble acp` serves the Agent Client
Protocol on stdio, through acp-go-sdk2's `acp.ServeAgent`. This repository drives ACP agents through one shared
adapter, `internal/provider/acpagent`, and one `acpagent.Spec` per agent. grok is registered that way today. opencode's
ACP Spec is written but not yet registered (0214-PLAN P7). gobble is not registered. There is no `provider.IDGobble`
and no `internal/provider/gobble`.

The question is what this repository must build, and what gobble must guarantee, so that mcremote can register gobble
as a first-class provider. The answer must use what the shared adapter and the SDK already do, and build only the
rest.

This record was first written on 2026-10-01 for the agent under its earlier name. Since then:

* the pair was renamed for gobble ([0186-MADR-integrate-current-library-releases-and-prepare-gobble.md](0186-MADR-integrate-current-library-releases-and-prepare-gobble.md),
  owner answer Q2), which asked for this rewrite once 0184's A2 had landed;
* 0184's A2 landed (`01c07620`);
* [0208-MADR-make-the-provider-layer-agent-neutral.md](0208-MADR-make-the-provider-layer-agent-neutral.md) D8 took the
  old D1 to D3 and D7 as general decisions, and narrowed this record to gobble's Spec, its `_gobble/*` operations, its
  auth hooks, its command table and its registration gates;
* 0208's P6 made capabilities declared rather than inferred.

The owner's instruction for the rewrite (2026-11-03) was:

* "determine what functionality is provided by the shared libraries and sdk, and ensure only what needs to be built is
  included";
* "write the full scope of the gobble contract as required by magic-cli-remote to adopt and register it";
* "omit fs/terminal and auth via acp authenticate".

The third bullet's authenticate omit was withdrawn the same day (see the amendment below). D2 still omits fs and
terminal. D3 now implements 0186 Q4: the phone configures gobble's upstreams through 0074, and the session handshake
calls `authenticate` when a credential exists.

Later the same day the owner added: "in addition to acp auth and phone configuration working, i want the acpext
module." D13 takes gobble's `acpext` module (gobble-cli `docs/decisions/0042-MADR-acpext-extension-contract-module.md`,
proposed), and the second amendment below lists what that changed.

The old body is in this file's history (last changed in `b1454ab1`), and the note at the end says what it decided.

### Baselines

| Side | Read at | SDK pin |
| :--- | :--- | :--- |
| This repository | `3489184b` (master) | acp-go-sdk2 `v1.31.2` |
| gobble-cli | `9b5535a` (main); newest tag `v0.1.1` (`66f4957`, 2026-10-19), 119 commits behind `9b5535a` | acp-go-sdk2 `v1.31.2` |
| acp-go-sdk2 | `v1.31.2`, ACP schema `1.25.0` | — |

Another session had uncommitted 0214 P6e work in `acpagent` and `internal/session` while this was read. Every line
cited below is `HEAD`'s, read through `git show HEAD:`.

### What this repository already provides

These were verified at `3489184b`. None of them is rebuilt for gobble.

* **Declared capabilities** (0208 D1, P6a–P6d):
  * `provider.Session.Capabilities()` (`internal/provider/provider.go:152-213`).
  * `commandContext` reads the declaration, not Go types (`internal/session/commands.go:44-108`).
  * The phone's rename, fork, revert and diff are gated on it in `internal/session/manager.go`.
  * A `session_config` event re-resolves commands.
* **Vendor operations are Spec hooks.** `acpagent.Spec.Ops` (`acpagent/host.go:111-138`) holds Fork, Compact, Rename,
  UndoLast, RuntimeUsage, RuntimeStatus and ConfirmLimit, and the workspace, shell, revert, diff and skill operations.
  A nil hook is "not offered" and sends nothing (`sessioncaps.go:176-216`, `fork.go:14-28`, `rewind.go:12-17`,
  `runtime.go:13-27`). A Spec's hooks act on the session through `acpagent.Host` (0208 amendment of 2026-11-01).
* **Spec-owned extensions.** `Spec.ExtensionNotifications`, `Spec.ExtensionRequests`, `Spec.MetaHandlers`,
  `Spec.NewSessionState` and `Spec.InitializeMeta`. No vendor name is in `acpagent`'s non-test files
  (`TestNoProviderIsNamedInAcpagent`).
* **Per-Spec client capabilities.** `Spec.ClientCapabilities` replaces the default `initialize` advertisement
  (`acpagent.go:1274-1297`), checked by `CheckClientCapabilities` (`:1330`). This was 0186 Q3.
* **Pass-through commands** (0208 D9). `command.Resolve` (`internal/command/command.go:243-297`) resolves a name the
  agent advertises and the table does not declare as `KindNative`. A declared `KindOp` the agent advertises also
  becomes native. A default's `KindOp` is available only when the session declares the op.
* **Model and thinking by the standard** (0184 D6). `SetModel` and `SetThinkingLevel` send
  `session/set_config_option` for the `model` or `thought_level` category (`session.go:1055-1119`,
  `thinking.go:17-55`). Current state follows `config_option_update` and the set's answer (`session_meta.go:98-114`).
  `acp.ConfigOptionByCategory` finds a grouped select too. `Spec.ModelsFromOption` (`acpagent.go:163`) makes the
  session's `model` option the picker's catalog, groups included (`modelcatalog.go:21-39`).
* **Event mapping** (`session.go:1866-2158`):
  * message, thought and replayed user chunks;
  * tool calls with diffs as `{path, old_text, new_text}`;
  * `plan`;
  * `available_commands_update`;
  * `current_mode_update`;
  * `config_option_update`;
  * `session_info_update`'s title, without replacing a user's name;
  * `usage_update`, with `cost` as the session total (0189);
  * child-session frames dropped.
* **Lifecycle:**
  * one process per session, plus a warm spare;
  * `session/load` gated on `loadSession` (120 s);
  * `session/list` and `session/close` gated on the negotiated capabilities;
  * `session/delete` falling back to close;
  * close bounded at 2 s, then a process-group kill through agentproc.
* **Prompt path:**
  * text, image and audio blocks, each only when the agent advertises it;
  * a daemon FIFO of 4 for input during a turn, so no second `session/prompt` is ever sent (`session.go:334-360`);
  * `session/cancel`.
* **Version** (`acpagent/version.go`):
  * `Spec.KnownGoodVersion` warns on a mismatch, by full SemVer; a prerelease never matches
    (`provider/version.go:10-27`);
  * `Spec.MinVersion` refuses a lower version.
* **Errors:** a prompt's JSON-RPC error is presented through `agenterr`. Its `data` is read for the cause
  (`internal/agenterr/agenterr.go:470-497`), and "authentication required" is `KindAuth` (`:133-147`).
* **Tests:**
  * `acpagent/acptest`, a fixture ACP agent on in-memory pipes, and `Provider.StartOn` (0208 P6b);
  * `internal/wirecap`, generic and keyed on the Spec ID, with fixtures in `internal/provider/<id>/testdata/wire/<ver>/`.

### What acp-go-sdk2 `v1.31.2` provides

The adapter already uses these, and gobble needs nothing beyond them:

* **Capability gates.** `Negotiation.Allows` and `AllowsValue`, run by every typed client call. An unadvertised method,
  MCP transport or prompt block fails locally with `*CapabilityError`.
* **Refusals.** An agent's `fs/*` or `terminal/*` request is refused with -32601 before any handler runs, when the
  client did not advertise it (`connection.go:3136`).
* **Config options.** `ConfigOptionByCategory` and `SessionConfigSelectOptions.All()`, and the category constants
  `model` and `thought_level`.
* **Extensions.** `ClientSideConnection.CallExtension` and `SendRequest[T]`.
* **Typed session calls** for every stable method. `session/fork` is unstable (`UnstableForkSession`), and
  `session/set_model` does not exist in schema 1.25.
* **Auth.** No SDK rule requires `authenticate`. An agent signals a missing credential with `auth_required` (-32000),
  which `acp.IsAuthRequired` tests (`errors.go:354`).
* **Process management.** `agentproc` launches, ties and tears down the process.
* **Defaults.** `OverflowBlock` is the default overflow policy. The default message limit is 10 MiB.

### What gobble provides

Verified from gobble-cli's source at `9b5535a`. gobble-cli's own records state most of it; where they disagree with the
code, the code is cited.

* **`initialize`** (`acpserver/agent.go:370-415`):
  * protocol `1`;
  * `loadSession: true`;
  * `mcpCapabilities: {http: true}`;
  * `promptCapabilities: {embeddedContext: true}`. Image and audio are false;
  * `sessionCapabilities: {list, resume, close, additionalDirectories}`. There is no fork and no delete;
  * `_meta.gobble.extensions`, the `_gobble/*` request names;
  * `agentInfo {name: "gobble", title: "gobble", version}`.
* **The version.** A release build reports its tag without the `v`. A `make` build between tags reports
  `git describe`'s prerelease, such as `0.1.1-103-g8250cc4-dirty`. An unstamped build reports `0.0.0-dev+…`
  (`internal/buildinfo/buildinfo.go:71-83`; `Makefile:85-88`).
* **Auth methods.**
  * gobble always advertises one `agent` method per keyed provider: gemini, openai, claude, grok, opencode-zen,
    opencode-go, huggingface, kilo and together.
  * It adds `terminal` methods only when the client advertises `auth.terminal` (`acpserver/auth.go:45-66`).
* **Credentials** are gobble's own, on the host. The order is:
  1. `<config>/auth.json`, written by `gobble auth login`;
  2. a `models.json` key;
  3. the provider's environment variable (`internal/auth/resolve.go:33-36`).

  `<config>` is `GOBBLE_CONFIG_DIR`, else `GOBBLE_HOME`, else `$XDG_CONFIG_HOME/gobble`, or `%APPDATA%\gobble` on
  Windows (`internal/appdirs/dirs.go:36-46`).
* **No credential.**
  * `session/new` succeeds, with no `model` option.
  * The first prompt that needs a model fails with -32000 "Authentication required". Its data is `{"error":"llm:
    authentication failed: no model credential: set one of …"}` (`acpserver/prompt.go:280-288`).
  * Commands other than `/compact` still run.
* **Session methods** (`acpserver/agent.go`):
  * `session/new` (`:424-504`);
  * `session/load` (`:1071-1121`). It replays the transcript and the last plan, and refuses -32600 if the session is
    open in this process or locked by another;
  * `session/resume` (`:1124-1141`);
  * `session/list` (`:1148-1175`);
  * `session/close` (`:1253-1283`), which waits up to 5 s for a cancelled turn;
  * `session/cancel`, `session/set_mode` and `session/set_config_option` (select values only);
  * `session/prompt`.

  `session/fork`, `session/delete` and `logout` are -32601.
* **The prompt.**
  * Text, `resource` and `resource_link` blocks are accepted.
  * A second prompt while one runs is refused with -32600, data `{"reason":"a prompt is already running"}`. The
    running turn is not cancelled (`agent.go:780-797`).
* **Config options** (`acpserver/config.go:187-234`):
  * `model`, category `model`, a **grouped** select: up to five "Recent" picks, then one group per usable provider,
    with values `provider/model`;
  * `thought_level`, an ungrouped select of the model's levels.

  They are sent in the responses to `session/new`, load, resume and `set_config_option`. `config_option_update` is
  sent from `/model` and `/thinking`.
* **Modes** (`acpserver/modes.go:17-46`): `default`, `accept-edits` and `plan`, plus `bypass` only when the host's
  global `permissions.allowBypass` is true. `current_mode_update` is sent on every change.
* **Commands** (`acpserver/commands.go:29-48`):
  * `available_commands_update` is sent before the response of `session/new`, `session/load` and `session/resume`;
  * eighteen built-ins: help, compact, usage, context, session, model, providers, models, thinking, mode, plan, name,
    fork, clone, todos, personality, trust and permissions;
  * then the trusted prompt templates.

  Every one executes in gobble. Skills run as `/skill:<name>` prompt text and are not advertised.
* **Updates** (`acpserver/prompt.go`, `history.go`):
  * message and thought chunks;
  * `user_message_chunk`, on replay and for queued input;
  * `tool_call` and `tool_call_update`, with kind, a title of at most 60 runes, `rawInput`, `locations`, diff blocks
    and a summary of at most 400 bytes;
  * `plan`;
  * `session_info_update`'s title, on `/name`, `_gobble/set_session_name` and the automatic title (off by default);
  * `usage_update`, with `used`, `size` and, for a priced model, `cost` in USD, at every turn end.
* **Requests to the client:** `session/request_permission` only. That covers tool calls, plan exit with the plan sent
  first, grants, trust and the personality switch, all with the standard option kinds (`acpserver/question.go`,
  `modes.go:243-272`). There is no elicitation and no extension request.
* **Files and terminals.**
  * Client files are used only when the client advertises both `fs` capabilities (`agent.go:718-720`).
  * A client terminal is used only when the client advertises one and the host sets `tools.bash.useClientTerminal`,
    which defaults to false (`agent.go:721-723`).
  * Otherwise gobble reads, writes and runs locally (`tool/builtin/builtin.go:161-187`). Nothing fails.
* **The `_gobble/*` table** (`acpserver/extensions.go:28-49`):
  * There are twenty requests. The two this record uses are:
    * `_gobble/set_session_name {sessionId, name}` → `{}`;
    * `_gobble/clone {sessionId}` → `{sessionId}`.

    Both claim the turn, so each is refused with -32600 while a prompt runs. The others are steer, follow_up,
    clear_queue, the two queue modes, compact (`customInstructions`), usage, fork (`entryId`), get_state, get_messages,
    get_entries, get_last_assistant_text, get_session_stats, get_fork_messages, set_auto_retry, set_auto_compaction,
    abort_retry and set_personality.
  * There are three notifications:
    * `_gobble/retry {sessionId, phase, cause, attempt?, maxAttempts?, delayMs?, error?}`;
    * `_gobble/compaction {sessionId, phase, reason, tokensBefore?, error?, paused?}`;
    * `_gobble/personality {sessionId, name, level}`.
  * The first sixteen requests are frozen by gobble-cli's `0002-PLAN-cli-acp-headless-mcp-v1.md` ("Phase 7 made
    executable"). The rest come from its `0005-PLAN-v1-feature-scope.md` F5-1 and F5-2 and its
    `0022-MADR-gobble-voice-and-personalities.md`. gobble exports no Go package of these types.
  * *Added 2026-11-03, second amendment:* gobble-cli's proposed `0042-MADR-acpext-extension-contract-module.md` publishes
    them as the nested module `github.com/maccavelli/gobble-cli/acpext`. It holds the twenty-three method names, every
    params and result type, `_meta.gobble`'s `CapabilitiesMeta`, `AuthProvider` and `AuthProviders()`, and
    `ErrorData`. It requires acp-go-sdk2 alone, and gobble's `acpserver` serves from it with its wire unchanged. Its
    first tag is to be `acpext/v0.1.0`. Neither the module nor the tag exists at gobble-cli's `67f72fb`.
* **Embedding.** `gobble.New`'s `ServeACP` and `Connect` return `errors.ErrUnsupported`. The supported integration is
  the binary.
* **Self-update.** `gobble update` does not exist yet (gobble-cli `0005-PLAN-v1-feature-scope.md` F10).

### What is missing

Set against the three lists above, this is the whole gap:

* **G1. There is no gobble provider:** no id, no configuration section, no Spec, no registration.
* **G2. The phone's Rename and Fork reach no gobble operation.** They are gated on `CapRename` and `CapFork`, which
  need `Ops.Rename` and `Ops.Fork`. The standard schema has no rename, and gobble does not offer `session/fork`.
* **G3. gobble's retry and compaction notices are declined.** A provider retry is a silent wait on the phone, and a
  compaction shows no progress.
* **G4. gobble's `bypass` mode would reach the phone with no confirmation gate.** `Spec.DangerousModeIDs` does not
  exist; 0208's P7 adds it, and it is not started.
* **G5. Nothing pins gobble's wire.** There are no fixtures, no `live-gobble`, and no gobble release that carries the
  surface above. `v0.1.1` predates `ServeAgent` and the grouped model option; it already had the two requests and
  `_gobble/retry`.
* **G6. The phone cannot configure gobble's credentials.** gobble advertises agent auth methods and stores keys in its
  own `auth.json`. Magic has no gobble `AuthStatus` / `SetCredential` / `StartDeviceAuth` hooks, so the 0074 screens
  have nothing to drive. A host still needs `gobble auth login` or an environment variable.
* **G7. This repository would hand-write gobble's JSON.** Method names, field names and the auth provider list would
  be copied from gobble's source into the gobble package, where only a fixture capture would catch a drift. That is
  how the old body's `instructions` came to differ from gobble's `customInstructions` (0179-REPORT M9, R3).

### What the old body or issue #33 asked for that is no longer needed

* **Strict fallback, and moving grok's operations into its Spec.** Done generally by 0208 D1 and D2 (P6).
* **`ConfigureSession` after `session/load`.** gobble writes a `model_change` and a `thinking_level_change` entry into
  its own session. A load restores both, and its answer carries the current `configOptions`.
* **`SetModel`/`SetThinking` operations or fallbacks.** The standard path finds gobble's grouped `model` option and its
  `thought_level` option by category.
* **A title, a cost, or diffs mapping.** Already mapped.
* **`_gobble/compact`, `_gobble/usage` and `_gobble/fork` as operations.** gobble advertises `/compact [focus]`,
  `/usage`, `/fork [entry]` and `/context`, so they pass through as native slash text with their arguments
  (0208 D9). An op would add a second path to the same behaviour.
* **A command typed before the first `available_commands_update`.** gobble sends that update before the
  `session/new` and `session/load` responses, so there is no such window.
* **Steer and follow-up.** The daemon's FIFO never sends a second `session/prompt`, so gobble's refusal of an overlap
  is never met. `_gobble/steer` would change what the user gets (input during the turn, not after it). That is a
  product decision for a later record, not a registration need.
* **stdio MCP.** gobble takes `http` servers; this daemon forwards `http` and `sse`, each only when advertised.
* **A shared `acpext` module.** This record calls two requests and handles two notifications. A golden fixture of
  each pins the JSON instead.
  *Withdrawn 2026-11-03 by the second amendment: the owner asked for the module, and D13 takes it. The fixtures stay,
  as D11 says.*
* **Client files and terminals.** Omitted (D2); 0186 Q3 stands. ACP `authenticate` and the phone's provider-auth flow
  are in D3, not omitted.

## Decision Drivers

* Use what the shared adapter and the SDK already do. Build only G1 to G7.
* gobble's names and shapes come from gobble, through `acpext`. This repository keeps no copy of them (G7).
* Every command and menu the phone shows for a gobble session executes in gobble. Nothing is sent that gobble does not
  serve (0023, 0208 D7).
* gobble's own commands pass through unchanged (0208 D9).
* No vendor code in `acpagent` (0208 D2), and no gobble code outside `internal/provider/gobble` and the registration
  places.
* gobble's credentials live in gobble's store. Magic writes that store through 0074, the same way it writes grok's and
  the OpenCode/Kilo engines'. No secret is kept in mcremote config. No secret crosses ACP `authenticate` (gobble-cli
  0016 A2).
* The contract is pinned by fixtures from a gobble release tag, never from gobble's main branch (0186 D9).

## Considered Options

* **A. A thin `internal/provider/gobble` on today's adapter.** Spec settings, two operations and two notification
  handlers; commands passed through; no client files or terminals; no ACP authentication and no phone provider-auth;
  gated on 0208's P7 and a gobble release tag.
* **B. Issue #33's full set.** As A, plus operations for compact, usage and fork by entry, a steer and follow-up path,
  `ListModels` before the first session, and a shared `acpext` module.
* **C. As A, with ACP authentication and phone provider-auth** (0186 Q4). gobble's `agent` methods, chosen through
  `SafeAuthMethodIDs`, and the phone's 0074 flow writing gobble's store.
* **D. As A, after 0208's P8 to P10.** Register gobble only once one descriptor, capability-driven phone menus and the
  shared conformance suite exist.

## Decision Outcome

Chosen option: **"C. As A, with ACP authentication and phone provider-auth"**, because it closes G1 to G6, keeps D2's
omit of fs and terminal, and is 0186 Q4. The rewrite-day omit of `authenticate` was withdrawn (amendment below). D1 to
D12 below are the contract.

*Second amendment, 2026-11-03:* the chosen option is C **with gobble's `acpext` module** (D13), which closes G7. Of
option B, only its `acpext` item is taken; its operations, steer path and `ListModels` stay out. D1 to D13 are the
contract.

### D1. Identity and launch

* `provider.IDGobble` is wire id `"gobble"`.
* The Spec lives in a new `internal/provider/gobble`, as thin as `internal/provider/grok/grok.go`. It sets:
  * `ID: provider.IDGobble`;
  * `DefaultBin: "gobble"`;
  * `DefaultArgs: func(acpagent.Config) []string { return []string{"acp"} }`.
* No `ModelArgs`: the model is chosen by `session/set_config_option` (D6), never on the command line.
* No `StartTimeout` of its own; the default 30 s applies. gobble's `session/new` waits for its run provider's first
  model listing. The PLAN measures it from the tag, and a measured need for more is a deviation.
* The process model is the adapter's: one `gobble acp` per session, plus the warm spare.

### D2. No client files or terminals

* `Spec.ClientCapabilities` is the adapter's default advertisement with these removed:
  * `fs.readTextFile`, `fs.writeTextFile` and `terminal`;
  * `auth.terminal`, left false, so gobble advertises no terminal auth method.

  The rest of the default is kept: `session.configOptions.boolean`, `session.notices`, `session.compaction`,
  `elicitation.form` and `plan`. gobble requests none of them.
* gobble then uses its own workspace file tools and its local shell (gobble-cli `acpserver/agent.go:718-723`). Tool
  output reaches the phone in `tool_call_update` content, as for any agent.
* An `fs/*` or `terminal/*` request from gobble would be refused by the SDK with -32601 before any handler runs. The
  adapter counts the refusal and ends the turn with a notice (`session.go:635-668`). A fixture test pins that gobble
  sends none (D11).

### D3. Phone provider-auth writes gobble's store; ACP `authenticate` is the handshake

This is 0186 Q4. It does not add `go-llmprovider-sdk` to this module (0185). gobble already implements ACP
authentication on that SDK's `TokenStore` (gobble-cli 0016 A2, 0019 D11). Magic drives it through
[0074-MADR-remote-provider-auth-from-phone.md](0074-MADR-remote-provider-auth-from-phone.md), as it drives grok.

* **Catalog and status** (0074 D16, D3). The Spec sets `AuthCatalogList` and `AuthStatus`.
  * The catalog is gobble's key providers, in registry order: gemini, openai, claude, grok, opencode-zen, opencode-go,
    huggingface, kilo and together. Source is `static` (the SDK descriptor table, not a live engine). Ollama takes no
    key and is omitted.
  * Each upstream offers `api_key`. `oauth_device` is offered for `grok` (gobble keeps the login) and `kilo` (device
    yields a key). `oauth_browser` is offered for `grok` and marked unavailable (`host_oauth` / W3), as grok's own
    browser method is. Claude is key-only (0074 D12). OpenAI browser and device logins are the Codex client's; gobble
    does not offer them (gobble-cli 0031 D7).
  * Status is presence in gobble's store (and the provider's environment variable), never the secret. `session/new` is
    not required: the phone's settings sheet lists and writes credentials with no gobble session open.
  * *Second amendment, 2026-11-03:* the catalog is `acpext.AuthProviders()` (D13), not a list kept here. Its order is
    the registry's, an entry's `ID` is the upstream id and the agent method id, and its `Name` is the label. `Login`
    marks the providers with a sign-in, grok and kilo today, which are the ones offered `oauth_device`. A provider
    gobble adds reaches the phone at the next `acpext` bump, with no edit here.
* **Writes** (0074 D1, D2, D11). The Spec sets `SetCredential` and `ClearCredential`.
  * A key is written into gobble's native store (`<config>/auth.json`, gobble-cli `internal/auth/resolve.go`), for the
    service account. Magic keeps no copy. The phone treats the secret as write-only.
  * `<config>` is gobble's: `GOBBLE_CONFIG_DIR`, else `GOBBLE_HOME`, else `$XDG_CONFIG_HOME/gobble`, or
    `%APPDATA%\gobble` on Windows.
  * Device OAuth is `StartDeviceAuth`, running gobble's device sign-in for `grok` and `kilo` (gobble-cli
    `llm/provider.SignIn` with `MethodDevice`, and `gobble auth login … --json` as 0016 A3). The phone shows the user
    code over the existing 0074 device-flow messages. Browser loopback stays W3.
  * gobble is not added to the Codex/Grok transactional coordinator (`internal/daemon/credentials.go`). Those
    coordinators exist for destructive CLI logins (0074 D8, D21). gobble's file write is the OpenCode-family shape: the
    agent's store, then a presence check.
* **Handshake.** The Spec sets `SafeAuthMethodIDs` to gobble's agent method ids (the provider ids). `HasHeadlessCredential`
  is true when any catalogued upstream has a credential. `AuthPreference` is the configured upstreams, then the rest.
  `providers.gobble.auth_method_id`, when set, must be one of those ids.
  * When a credential exists, `acpagent` calls `authenticate` with that agent method. gobble answers `{}` or
    `auth_required`; no secret is on that call.
  * When none exists, `authenticate` is not called, `authRequired` is set, and `Start` fails with
    `ErrACPAuthRequired` (`acpagent.go:982-984`). The phone already handles that: open settings, add a key or finish a
    device flow, start again. A prompt with no model still returns gobble's -32000, classified `KindAuth`, as a second
    line of defence (D11).
  * Terminal methods (`<provider>-login`) are not advertised, because D2 leaves `auth.terminal` false. The phone must
    not depend on them.
  * *Second amendment, 2026-11-03:* `SafeAuthMethodIDs` is the `ID`s of `acpext.AuthProviders()`. The `data` of
    `authenticate`'s `auth_required`, and of a prompt's -32000, is read as `acpext.ErrorData` (`provider`, `reason`,
    `command`, `error`). The phone's message names `ErrorData.Command` when it is set.
* `Spec.CredentialEnv` is not set. gobble's provider variables are `GEMINI_API_KEY`, `ANTHROPIC_API_KEY`,
  `OPENAI_API_KEY`, `XAI_API_KEY`, `OPENCODE_API_KEY`, `HF_TOKEN`, `KILO_API_KEY` and `TOGETHER_API_KEY`
  (go-llmprovider-sdk `v1.25.0`'s `provider.go:97-107`). Each ends in `_API_KEY` or `_TOKEN`, which the stderr tail
  already scrubs by name (`internal/provider/stderrtail/stderrtail.go:49`).
* **0186 Q4 stands.** The rewrite-day replacement of Q4 is withdrawn. The PLAN's records phase notes that on 0186.

### D4. Version

* `KnownGoodVersion` is the gobble release tag the D11 fixtures are captured from, without its `v`. A mismatch warns,
  and never refuses (0186 Q5). A `make` build between tags reports a SemVer prerelease, and an unstamped build reports
  `0.0.0-dev…`; neither matches, so both warn.
* `MinVersion` is not set. A build made after a tag reports a prerelease of that tag, which SemVer orders before the
  tag itself. A floor at the tag would therefore refuse every development build of the release it names.
* This repository never runs `gobble update` (0186 Q5). gobble does not have one yet.
* *Second amendment, 2026-11-03:* the `acpext` version this repository requires is the one the `KnownGoodVersion`
  release was built with. `go version -m` on that release binary names it, and the PLAN records both at capture time.

### D5. Session lifecycle

Only what the adapter already does with any Spec. With gobble's capabilities, that is:

* `session/new` with `cwd` and the configured MCP servers. gobble advertises `http` only, so the SDK's gate keeps `sse`
  servers out.
* `session/load`, since `loadSession` is true. gobble refuses a load while another process holds the session's lock,
  with -32600 and the lock's text. Whether the adapter ever loads a session while an earlier process for it still
  runs, for instance after a restart or a fork, was not read for this record. The PLAN reads it first, and the D11
  fixtures include a close then a load.
* `session/list` and `session/close`.
* `session/delete`, which falls back to close, since gobble does not advertise delete.
* Not called: `session/resume` (the adapter never calls it) and the unstable `session/fork` (gobble does not offer it;
  D8 covers fork).
* Prompts carry text only. gobble advertises no image or audio, so the SDK's gate drops those blocks, and the adapter
  warns.

### D6. Model and thinking

* **The standard path, unchanged** (0184 D6). `/model`'s op and the phone's model and thinking settings send
  `session/set_config_option` to gobble's `model` and `thought_level` options, found by category.
* **No fallbacks or vendor `_meta`.** The gobble Spec sets no `SetModelFallback`, `SetThinkingFallback`,
  `HarvestSessionMeta`, `ConfigureSession`, `SessionMeta` or `InitializeMeta`.
* **`Spec.ModelsFromOption` is set,** so the picker's catalog is the session's grouped `model` option. Its groups are
  gobble's recent picks and its providers, and each value is gobble's `provider/model` ref. The PLAN fixes the mapping
  of gobble's group ids to picker groups.
* **No `ListModels` and no `StaticModels`.** gobble's models are the ones its credentials reach, known only inside a
  session. Before a gobble session exists, the picker has no gobble models.

### D7. Modes

* gobble's modes come from its `session/new` and `session/load` answers: `default`, `accept-edits`, `plan`, and
  `bypass` when the host allows it.
* `DefaultModeID: "default"`, and `SynthesizeAutoMode: false`. gobble has its own `bypass`; the daemon adds no `auto`.
* **`bypass` is marked dangerous** through 0208 P7's `Spec.DangerousModeIDs`: `DangerousModeIDs: []string{"bypass"}`.
  The phone then confirms it, as it confirms grok's synthetic `auto`. This is G4, and the reason D12 waits for P7.

### D8. Commands and the two operations

* **The command table declares nothing gobble advertises.** Each of gobble's built-ins and templates resolves as
  `KindNative`, by the resolver's pass-through (0208 D9), and is sent as typed. That includes `/compact [focus]`,
  `/usage`, `/context`, `/model`, `/thinking`, `/mode`, `/plan`, `/fork [entry]` and `/name`.
  * The canonical names gobble does not advertise fall to their defaults. These are undo, redo, diff, status, review,
    archive, delete, ps, stop, goal, deep-research and the rest.
  * A default's `KindOp` is unavailable without a declared op (D9), so those resolve to `KindNone` and the phone does
    not offer them.
  * `help`, `new`, `sessions` and `clear` stay the daemon's, as for every provider.
* **`CommandCaveat`**, the note appended to the daemon's `/help`, says that gobble's skills run as `/skill:<name>`
  prompt text and are not listed.
* **Two operations,** for the phone's menus (G2), each a `Host.CallExtension` in the gobble package:
  * **`Ops.Rename`** calls `_gobble/set_session_name {sessionId, name}`. gobble answers `{}`, and sends
    `session_info_update`, which the adapter maps to the title.
  * **`Ops.Fork`** calls `_gobble/clone {sessionId}`. gobble answers `{sessionId}`; the result's `AgentSessionID` is
    that id and `ForkedFromID` the source's. The daemon then loads the new session in its own process, as for any
    fork. `ForkOptions.LastTurnID` is ignored, as grok's fork ignores it (`internal/provider/grok/ops.go:31`): gobble
    copies the whole session. A fork before an earlier entry is gobble's `/fork [entry]`, passed through.
  * Both are refused with -32600 while a prompt runs, and the daemon reports the refusal as the operation's error. The
    menus' availability follows the declared capability, not the turn, so a rename or fork asked for mid-turn shows
    that refusal.
* No other op is set. Compact, undo, usage, status, the limit confirmation, and the workspace, shell, revert, diff and
  skill operations stay nil.
* *Second amendment, 2026-11-03:* both calls are written with `acpext`'s names and types (D13):
  * Rename sends `acpext.MethodSetSessionName` with `acpext.SetSessionNameParams`;
  * Fork sends `acpext.MethodClone` with `acpext.SessionParams`, and decodes `acpext.CloneResult`.

  The gobble package holds no `"_gobble/…"` string literal.

### D9. Two notification handlers

`Spec.ExtensionNotifications` registers two of gobble's notifications, each written on `Host` in the gobble package:

* **`_gobble/retry`.** A `start` phase becomes `Host.Notice`, naming the attempt, the wait and the cause from gobble's
  params. An `end` phase with an `error` becomes a notice of the failure. A successful `end` adds nothing.
* **`_gobble/compaction`.** Its phases become `Host.UpdateCompaction`: in progress at `start`, then completed or failed
  at `end`, with gobble's `error` as the failure text. One compaction id per start, kept in the Spec's session state
  (`Spec.NewSessionState`).

`_gobble/personality` is not registered; the adapter declines it at Debug, as any unregistered notification. gobble's
other requests are not called.

*Second amendment, 2026-11-03:* the handlers are registered under `acpext.MethodRetry` and `acpext.MethodCompaction`.
They decode `acpext.RetryNotice` and `acpext.CompactionNotice`, and compare phases and reasons with acpext's
constants. The decode is `encoding/json`'s, through the `json.RawMessage` the adapter hands a handler, which is the
same type as `jsontext.Value`.

### D10. Registration

0208's P8 descriptor does not exist. gobble is registered at the places a provider is registered today, with no third
way invented. If P8 has landed by the PLAN's registration phase, it is one descriptor instead.

* **Identity:** `provider.IDGobble` (`internal/provider/provider.go:62-73`).
* **Configuration:**
  * a `GobbleProviderConfig` embedding `ACPProviderConfig` in `ProvidersConfig` (`internal/config/config.go:449-507`);
  * its defaults and viper defaults, with the environment prefix `MCREMOTE_PROVIDERS_GOBBLE_*`;
  * the relative-path fix-up;
  * validation through `validateACPProvider`; `providers.gobble.auth_method_id`, when set, must be one of gobble's
    agent method ids (D3);
  * `docs/guides/config.md`.
* **The daemon:**
  * `Run`, `InspectProviders` and `prewarmPlan` (`internal/daemon/daemon.go`), through `acpAgentConfig`;
  * `KnownProviderIDs` (`internal/config/prewarm_write.go:28`).

  The credential guard (`daemon/credentials.go:45-50`) does not list gobble: gobble's writes go to gobble's store
  (D3), not the Codex/Grok coordinators.
* **Doctor:** `engineBins` gains `gobble` (`internal/cli/doctor.go:171-179`).
* **Tests:**
  * the provider lists in `internal/command/conformance_test.go`, `resolve_order_test.go`,
    `internal/provider/auth_conformance_test.go` and `provider_test.go`;
  * a capability-honesty test in the gobble package, as `grok/advertise_test.go` is grok's.
* **The phone:** a vendor icon through `tools/vendor-icons/sync.sh`. No Dart branch on the provider id is added; the ten
  that exist are 0208 P9's.
* **Make:** `live-gobble`, its `.PHONY` entry and the build-tag vet.
* **Default provider:** unchanged here. 0208 P9 makes gobble the built-in default.
* *Second amendment, 2026-11-03:* **the module graph.** `go.mod` and `go.sum` gain
  `github.com/maccavelli/gobble-cli/acpext`, in the commit that adds its first import (the gobble package). Nothing
  else enters the graph: acpext requires only acp-go-sdk2, which this repository already pins.

### D11. What pins the contract

* **Fixtures from a gobble release tag,** captured with `internal/wirecap` into
  `internal/provider/gobble/testdata/wire/<version>/`. They cover:
  * `initialize`, `session/new`, a prompt with a tool call, `/model` and `/thinking`;
  * `_gobble/set_session_name` and `_gobble/clone`, each idle and mid-turn;
  * `_gobble/retry` and `_gobble/compaction`;
  * a close then a load;
  * a prompt with no credential.
* **Unit tests in the gobble package,** on `acpagent/acptest` and `Provider.StartOn`, replaying those frames. Each is
  seen failing first on a planted break:
  1. the `initialize` request advertises no `fs`, no `terminal` and no `auth.terminal`;
  2. with a credential, `authenticate` is sent with that provider's agent method id; with none, `Start` fails
     `ErrACPAuthRequired` and no `session/new` is sent;
  3. the session declares exactly `CapRename` and `CapFork` among the Ops-backed capabilities, plus model and thinking
     from the options;
  4. Rename and Fork send exactly the D8 frames and map the answers; mid-turn they return the refusal;
  5. the two notifications produce the D9 events;
  6. a no-credential prompt (when `Start` is forced past the handshake, or gobble answers -32000) is presented as
     `KindAuth` with gobble's sentence;
  7. a recorded session sends no method outside D5, D6 and D8 (no `_x.ai/*`, no other `_gobble/*`, no `fs/*` or
     `terminal/*` answer);
  8. `AuthCatalogList` names the nine key providers; `SetCredential` for `openai` (or another key-only id) makes
     `AuthStatus` configured and the next `Start` authenticate; `StartDeviceAuth` for `claude` is
     `ErrAuthMethodUnsupported`.
  9. *Second amendment, 2026-11-03:* every captured `_gobble/*` frame, and `initialize`'s `_meta.gobble`, decodes
     into its `acpext` type and encodes back to the same JSON, compared as values. The test decodes with unknown
     members rejected, so a field the capture has and acpext lacks fails it. That strictness is the test's only;
     gobble's own decoding takes none (gobble-cli 0039-MADR D19). `AuthCatalogList` equals
     `acpext.AuthProviders()`.
  10. *Second amendment:* only `internal/provider/gobble` imports `acpext`, checked by a source test as
      `TestNoProviderIsNamedInAcpagent` checks names.
* **`live-gobble`,** against the installed tag, only on the owner's word, since it spends a provider's quota. It runs:
  * `/model` and `/thinking` producing `session/set_config_option` and the phone's state;
  * the grouped model catalog;
  * a phone rename and fork;
  * `/compact` passed through with its argument;
  * a turn with a tool call;
  * a recorded connection with no `fs/*` or `terminal/*`;
  * `authenticate` after a stored key, and a catalog write that does not open a session.

### D12. Gates

gobble is registered only when all of these hold:

1. **0208's P7 is on master.** `DangerousModeIDs` exists (D7). Grouped options forwarded with their category improve
   gobble's settings panel, but the picker does not need them (D6).
2. **A gobble release tag after `v0.1.1`** carries the surface in "What gobble provides". The fixtures and
   `KnownGoodVersion` name that tag. gobble's main branch is never pinned (0186 D9).
3. **Both repositories pin the same acp-go-sdk2 tag** at capture time (0184 D12). They do today: `v1.31.2`.
4. **D11's unit tests pass,** each seen failing first, and `live-gobble` passes on the owner's word.
5. **grok's, opencode's, kilo's and codex's suites pass unchanged.**
6. *Second amendment, 2026-11-03:* **an `acpext` tag exists and is served by the module proxy.** It is the one the
   gate-2 release was built with (D4), and its acp-go-sdk2 requirement is at or below this repository's pin. A higher
   one would raise this repository's SDK through MVS, which is 0184 D12's lockstep bump, not part of this record.

### D13. gobble's `acpext` module

*Added 2026-11-03 by the second amendment.*

* **What it is.** gobble-cli's nested module `github.com/maccavelli/gobble-cli/acpext`, tagged `acpext/vX.Y.Z`
  (gobble-cli `0042-MADR-acpext-extension-contract-module.md`).
  * It holds gobble's `_gobble/*` method names, their params and results, `_meta.gobble`'s `CapabilitiesMeta`,
    `AuthProvider` and `AuthProviders()`, and `ErrorData`.
  * It holds no behaviour. It requires acp-go-sdk2 alone.
  * gobble serves from it, so its types are the wire of every gobble built with that version.
* **What this repository uses.**
  * D3: `AuthProviders()` and `ErrorData`;
  * D8: `MethodSetSessionName`, `SetSessionNameParams`, `MethodClone`, `SessionParams` and `CloneResult`;
  * D9: `MethodRetry`, `RetryNotice`, `MethodCompaction`, `CompactionNotice` and their constants;
  * D11: `CapabilitiesMeta` and `MetaKey`, read in the fixture test.

  Nothing else is called, so nothing else in acpext changes what this repository sends.
* **Where it may be imported.** Only `internal/provider/gobble` (D11 item 10). `acpagent` stays agent-neutral
  (0208 D2).
* **What it does not replace.**
  * The D11 fixtures stay. acpext says what gobble means to send; a capture shows what a release sends. Item 9 ties
    the two.
  * The `KindAuth` presentation stays `agenterr`'s.
* **Bumps.** An `acpext` bump is a dependency change under a PLAN, with the fixtures re-captured from the gobble
  release that carries it. A field acpext renames is a compile error in the gobble package at that bump.
* **What gobble owns.** acpext's content, versions and tags are gobble-cli's, decided there. This repository requests
  changes through gobble-cli's issues and records, and never through a `replace` or a fork.

### Consequences

* Good, because the gobble package holds about one Spec, two operations, two handlers and their tests. Nothing in
  `acpagent`, the resolver, the session manager or the phone changes for gobble.
* Good, because every gobble command reaches gobble as typed, arguments included, and none is rewritten into another
  mechanism.
* Good, because no file or shell crosses ACP, and `authenticate` carries no secret. Keys and device codes travel on the
  existing 0074 messages, then sit in gobble's store. gobble's tools run on the host as the daemon's account, with
  gobble's own permission rules.
* Good, because the phone can add an API key or finish a grok/kilo device flow before any gobble session exists.
* Good, because what gobble must keep is pinned by fixtures from a tag. A change of gobble's wire shows up as a fixture
  failure at the next capture.
* Neutral, because input typed during a turn waits in the daemon's queue and reaches gobble after the turn, as for
  grok. gobble's steer is not used.
* Neutral, because before a gobble session exists the picker lists no gobble models.
* Neutral, because a missing credential fails `Start` (`ErrACPAuthRequired`) instead of opening a session that can
  still run `/compact`. The operator adds a key from settings and starts again.
* Bad, because the phone's Fork copies the whole session; a fork before an earlier message is `/fork [entry]` typed.
* Bad, because gobble's wire is owned by another repository. A rename there breaks this provider until the next
  capture, which the fixtures catch but do not prevent.
  *Second amendment, 2026-11-03:* with `acpext` (D13), a rename gobble makes is a compile error here at the next
  `acpext` bump. Before the bump, an older gobble's wire and this repository's types still agree.
* Good, because the names, shapes and auth provider list come from gobble, and the gobble package keeps no copy (D13,
  G7).
* Bad, because a gobble change this repository needs waits for an `acpext` tag and a gobble release, both
  gobble-cli's. This repository's `go.mod` gains a requirement it does not control.

### Confirmation

1. `provider.IDGobble == "gobble"`, and the daemon starts `gobble acp` for it.
2. D11's eight unit tests pass, each seen failing first on a planted break, run on a scratch copy.
3. `TestNoProviderIsNamedInAcpagent` passes unchanged, and no file outside `internal/provider/gobble` and D10's
   registration places names gobble.
4. A gobble session's `bypass` is confirmed on the phone (D7, through 0208 P7).
5. `live-gobble` passes against the tag, on the owner's word.
6. The other providers' unit, conformance and wire suites pass unchanged.
7. *Second amendment, 2026-11-03:* `go.mod` requires `github.com/maccavelli/gobble-cli/acpext` at a tag, with no
   `replace`, and `go list -m all` names no go-llmprovider-sdk (0185). D11 items 9 and 10 pass, each seen failing
   first.

## Pros and Cons of the Options

### A. A thin gobble package on today's adapter

* Good, because it builds only the Spec, two operations and two handlers.
* Good, because it keeps gobble's commands gobble's, and the adapter agent-neutral.
* Bad, because the phone cannot configure gobble, which is the 0074 hole for this agent.

### B. Issue #33's full set

* Good, because a steer path and a model catalog before any session would be there from the start.
* Bad, because compact, usage and fork-by-entry ops duplicate what passes through as slash text.
* Bad, because steer changes what typed-ahead input means for the user. That is a product decision this registration
  should not make.
* Bad, because an `acpext` module adds a cross-repository dependency for two requests.
  *Second amendment, 2026-11-03:* the owner took that cost. The module is also D3's provider catalog and error data,
  and gobble serves from it (D13).

### C. As A, with ACP authentication and phone provider-auth

* Good, because the phone can list gobble's upstreams, paste a key, and run grok/kilo device OAuth, with no gobble
  session and no SDK in Magic's `go.mod`.
* Good, because `authenticate` then selects the stored provider; it still carries no secret.
* Neutral, because gobble's `authenticate` is still a presence check. The store write is 0074, not that RPC.
* Bad, because browser OAuth and OpenAI's Codex-client logins stay out (W3, gobble-cli 0031 D7, 0074 D12).
* Good, *second amendment*, because with `acpext` the catalog and the auth error data are gobble's own list and type,
  not this repository's copy.

### D. As A, after 0208's P8 to P10

* Good, because the registration would be one descriptor, and the phone's menus would follow declared capabilities.
* Bad, because P8 to P10 are general work with no gobble need. P9 and P10 are not started, and gobble's registration
  would wait on them for no safety gain: the daemon already refuses an undeclared op (0208 P6d).

## More Information

### Not decided here

Each needs its own record when it is taken up:

* **Mid-turn input** through `_gobble/steer` and `_gobble/follow_up`, in place of the daemon's queue for gobble.
* **A gobble model catalog before the first session,** for instance from `gobble models --json`.
* **A compact operation with instructions**, should a phone control need one. `/compact [focus]` passes through
  today.
* **Tool-call `locations`**, which gobble sends and the adapter does not read. That is general, not gobble's.
* **Image and audio input,** which gobble does not advertise yet (gobble-cli `0036-MADR-adopt-go-llmprovider-sdk-new-capabilities.md`).
* **gobble as the default provider,** and capability-driven phone menus (0208 P9).

### What gobble keeps for this contract

These are gobble-cli's to keep, under gobble-cli's records. This repository does not write to gobble-cli.

* **The surface in "What gobble provides"**, at the release tag D12 names. In particular:
  * the two `_gobble/*` requests and the two notifications, with their field names;
  * the categories of its config options;
  * its `bypass` mode id;
  * `available_commands_update` sent before the session responses;
  * its local fallback when no `fs` or `terminal` is advertised;
  * its -32000 data for a missing credential.
* **A release tag** after `v0.1.1`, on a pushed commit.
* *Second amendment, 2026-11-03:* **`acpext`** at a pushed tag, with the types D13 uses, served from by that release.
  Its plan is gobble-cli `0042-PLAN-acpext-extension-contract-module.md`.
* **Its own records' corrections.** gobble-cli's audit of its records against its code (2026-11-03) found records that
  say:
  * the `model` option is ungrouped;
  * `logout` is offered;
  * `/mcp:` prompts exist;
  * a 2 s close bound.

  The code differs on each, and this record follows the code. Correcting them is gobble-cli's work.

### Relationship to other records and issues

* [0208-MADR-make-the-provider-layer-agent-neutral.md](0208-MADR-make-the-provider-layer-agent-neutral.md): its D8
  narrows this record to gobble. This record depends on its P6, which is done, and its P7 (D12 gate 1). It does not
  take its P8 to P10 (option D).
* [0186-MADR-integrate-current-library-releases-and-prepare-gobble.md](0186-MADR-integrate-current-library-releases-and-prepare-gobble.md):
  * D9 and Q2 asked for this rewrite;
  * Q3 is kept (D2);
  * Q4 is implemented by D3 (phone 0074 plus `authenticate`); the rewrite-day replacement of Q4 is withdrawn;
  * Q5 is kept (D4).
* [0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md):
  D6's standard model and thinking path, and D12's pin lockstep.
* [0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md):
  opencode's ACP Spec, the nearest pattern.
* [0189-MADR-carry-the-agent-session-cost-as-a-session-total.md](0189-MADR-carry-the-agent-session-cost-as-a-session-total.md):
  `usage_update`'s cost.
* `docs/reports/0179-REPORT-gobble-native-acp-integration-assessment.md`: the 2026-10-11 assessment. Its R2 (standard
  first), R4 (per-Spec client capabilities), R5 (one SDK tag on both sides), R7 (warn-only version) and R8 (never a
  second `session/prompt`) are carried here. R6 (credentials) is decided by D3. R3 (`acpext`) is not taken. R1 (one
  SDK can hide a shared misreading) is why D11 checks the frames against gobble's code, not only the SDK's types.
  *Second amendment, 2026-11-03:* R3 is taken, by D13.
* gobble-cli `docs/decisions/0042-MADR-acpext-extension-contract-module.md` and its PLAN, proposed 2026-11-03: the
  module D13 requires.
* Issue [#33](https://github.com/maccavelli/magic-cli-remote/issues/33): its item 4 is this rewrite. This record takes
  less than the issue's item 5 proposed (option B).
* In gobble-cli, its records `0002-MADR-cli-acp-headless-mcp-v1.md` (fourth and fifth amendments),
  `0008-MADR-native-cli-mode.md` D19 and D20, and `docs/reports/0009-REPORT-magic-cli-remote-findings.md` carry gobble's
  side.

### History

* **2026-10-01.** First written, `proposed`, for the agent under its earlier name. It decided:
  * Spec-declared session operations and capability reporting;
  * per-provider strict fallback;
  * phone menus acting on the resolved command;
  * complete ACP event mapping;
  * start-up and model catalog from `configOptions`;
  * dangerous modes;
  * the agent's registration, command table and live probes, on schema 0.13.5 and a fork of the earlier SDK.

  0208 took the general parts, and 0184, 0189 and 0208's P1 to P6 built most of them.
* **2026-10-13.** Renamed for gobble with `git mv`, by 0186's Q2. The body was left unchanged, pending this rewrite.
* **2026-10-14.** Notes on `usage_update`'s cost, by 0189.
* **2026-11-03.** Rewritten as gobble's contract on schema 1.25, from code read at the baselines above, by the
  owner's instruction. The earlier body is in this file's history at `b1454ab1`. That rewrite's D3 omitted ACP
  `authenticate` and every phone-auth hook, and said it replaced 0186 Q4.
* **2026-11-03, later.** The owner reversed that omit. D3 is phone provider-auth plus handshake `authenticate`; Q4
  stands. See the amendment below.
* **2026-11-03, second amendment.** The owner asked for gobble's `acpext` module. D13, G7 and gate 6 are added, and
  D3, D4, D8 to D11 annotated. See the second amendment below.

## Amendment — 2026-11-03: phone provider-auth, and ACP authenticate

The owner asked that this record reflect the change: the phone must be able to add API keys and OAuth for gobble, as
it does for grok, Codex, OpenCode and Kilo. The rewrite-day third instruction ("omit … auth via acp authenticate") is
withdrawn. "omit fs/terminal" stands (D2, 0186 Q3).

The record stays `proposed`. Where this amendment and the rewrite-day D3 disagree, this amendment governs. The D3
section in the body is the current contract.

Withdrawn rewrite-day D3, in summary:

* no `SafeAuthMethodIDs` and no phone hooks;
* `providers.gobble.auth_method_id` refused;
* credentials only via `gobble auth login` or the service environment;
* missing credential found at the first prompt as `KindAuth`;
* 0186 Q4 replaced for gobble.

What changes:

* Chosen option is C, not A.
* G6 is in the gap list; D3 closes it.
* D11 gains the catalog/write/device assertions (item 8) and handshake authenticate (item 2).
* 0186 Q4 stands; PLAN P0 notes the withdrawal instead of recording a replacement.

## Second amendment — 2026-11-03: gobble's `acpext` module

The owner's words: "in addition to acp auth and phone configuration working, i want the acpext module."

The first amendment's D3 (phone provider-auth and ACP `authenticate`) stands. This amendment adds the module beneath
it and beneath D8 and D9. The record stays `proposed`. Where an annotation marked *second amendment* and the text above
it disagree, the annotation governs.

The module is gobble-cli's: `docs/decisions/0042-MADR-acpext-extension-contract-module.md`, proposed the same day.
gobble-cli `67f72fb` was read for it. At that commit there is neither an `acpext/` directory nor an `acpext/` tag.

What changes:

* **The option.** The chosen option is C with `acpext`. Of option B only its `acpext` item is taken.
* **G7** joins the gap list. **D13** closes it, and gate 6 joins D12.
* **D3:** the catalog and `SafeAuthMethodIDs` come from `acpext.AuthProviders()`, and auth error data is read as
  `acpext.ErrorData`.
* **D4:** the required `acpext` version is the `KnownGoodVersion` release's.
* **D8 and D9:** the two calls and the two handlers use acpext's names and types.
* **D10:** `go.mod` and `go.sum` gain one requirement.
* **D11:** item 9 (frames against acpext's types) and item 10 (only the gobble package imports acpext) are added.
* **"No longer needed":** the `acpext` bullet is withdrawn. R3 is taken.
* **Confirmation** gains item 7.

What does not change:

* the fixtures;
* D2's omit of fs and terminal;
* the two operations and the two handlers;
* D12's gates 1 to 5;
* the phases' order. P1 now also waits for the `acpext` tag.

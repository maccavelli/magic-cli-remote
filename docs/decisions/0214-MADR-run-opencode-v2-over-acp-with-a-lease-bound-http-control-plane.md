---
status: proposed
date: 2026-10-25
decision-makers: Project Owner
consulted: none
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Run OpenCode v2 over ACP with a lease-bound v2 HTTP control plane

## Context and Problem Statement

[0019](./0019-MADR-opencode-process-management-plan.md) made one
daemon-owned `opencode serve` process and its HTTP/SSE API the only OpenCode
transport. [0020](./0020-MADR-opencode-session-tree.md),
[0021](./0021-MADR-opencode-http-api-coverage.md),
[0034](./0034-MADR-opencode-tool-stream-fidelity.md) and
[0112](./0112-MADR-opencode-1.18.21-surface-parity.md) built the phone
session loop, the session tree, the catalog, phone-side auth and the
workspace browser on that API. The provider's floor is `MinVersion = "1.18.0"`.
Its assessed release is `KnownGoodVersion = "1.18.26"`
(`internal/provider/opencode/version.go`), pinned by
[0137](./0137-MADR-prompt-to-first-token-latency-regression.md).

The installed engine is now **OpenCode v2.1.4**:

* `/Users/<user>/.opencode/bin/opencode`, a Mach-O arm64 executable.
* SHA-256 `d8a65dd253dbea759ae0dd829792ff219b40178c7decffea822430a4d1ccadb3`.
* `opencode --version` prints `opencode v2.1.4`.
* `GET /api/info` returns `"version": "2.1.4"`.

Upstream, the tag `v2.1.4` points at
[`68bc26b7da`](https://github.com/anomalyco/opencode/commit/68bc26b7da1990ce10ffecfbc1e09eb13aa5d824)
("release: v2.1.4", 2026-10-23). v2 has **no GitHub Release**:

* `gh release list` still marks **v1.19.4** (2026-10-22) as Latest.
* v2 installs through `https://opencode.ai/v2/install` (the repository's `install`
  script, lines 24–25).

The 1.x and 2.x lines therefore ship in parallel. A host that installs
OpenCode from GitHub releases still gets 1.x.

The sibling checkout `/Users/<user>/gitrepos/opencode` is used as reference
source only:

* It is on `dev` at `fecd50d21f` (2026-10-25).
* Its packages read `2.1.4`, synchronised by `2a3e7c0f34`
  ("sync release versions for v2.1.4").
* The release commit `68bc26b7da` is not in its local history, so no claim here
  rests on local HEAD being the release.
* The installed binary's live OpenAPI document is the contract. It lists 149
  operations, and so does the checkout's `packages/protocol/openapi.json`. They
  differ by one operation each way: HEAD adds `experimental.usage.limits`, and
  the binary has `experimental.voice.instructions`.

The upgrade broke the provider outright, and nothing in mcremote names the
cause. This record asks four questions:

* **What exactly fails against v2?**
* **What did v2 change in its process model, transports, API, events, CLI and ACP surfaces?**
* **What must change to restore full function, and which new v2 capabilities are worth adopting?**
* **Which architecture should the OpenCode provider use from now on?**

### What fails today, step by step

The probe replays the provider's own launch, `ServeArgs` in
`internal/provider/opencode/http.go:645-651`, against v2.1.4 in an isolated home.
It sends no credentials, as the provider does today.

| # | mcremote does | v2.1.4 answers | Effect in mcremote |
| --- | --- | --- | --- |
| 1 | `opencode serve --hostname 127.0.0.1 --port <n>` with `pure: true` appends `--pure` | exit 1, `Unrecognized flag: --pure in command opencode serve` | engine never starts |
| 2 | Same command without `--pure` | starts. With no `OPENCODE_PASSWORD` in the environment it generates a random password and prints `server password <secret>` on stdout | `httpagent` sends stdout to `io.Discard` (`provider.go:542`), so the secret is not logged, and mcremote never learns it |
| 3 | `GET /global/health` (`HealthPath`) | **`200 text/html`**: the web UI's single-page-app fallback answers every unknown path | the health probe passes. `OnHealthy` fails to decode JSON, logs at Debug and records an empty version. `CheckMinVersion` skips an empty version, so the gate never fires |
| 4 | `GET /global/event` (`EventsPath`) | `200 text/html` | the SSE reader sees no `data:` lines. The body ends, and `streamOnce` reconnects with backoff, forever and silently |
| 5 | `POST /session?directory=…` | `405` with an empty body | every session start fails with `POST /session…: HTTP 405:` and no explanation |
| 6 | `GET /provider`, `/config/providers` | `200 text/html` | catalog decode errors. `AfterBoot` falls back to its seeded model |
| 7 | anything under `/api/*` without credentials | `401 {"_tag":"UnauthorizedError","message":"Authentication required"}` | — |
| 8 | SIGTERM | exit status 130 | the provider's restart and exit classification have never seen 130 from this engine |

The defect that hides all of this is general and lives in `httpagent`, not
OpenCode. A `200` with a non-JSON body counts as healthy, and an SSE `200` that
is not `text/event-stream` counts as connected. The same gap would mask a
future kilo break.

### What v2 changed

#### Process model and server lifecycle

* `opencode serve` flags are now `--hostname`, `--port`, `--cors`, `--service`
  and `--stdio`. `--pure` is gone. A search of v2's `cli` and `core` sources
  finds no "pure" or no-plugin mode anywhere.
* `--stdio` is the embedding contract that v2 uses for its own children
  (`packages/cli/src/services/standalone.ts`):
  * The parent runs `serve --stdio --port 0` with `OPENCODE_PASSWORD` set in the
    child's environment.
  * The server prints one JSON line, `{"url":"http://127.0.0.1:<port>"}`, and
    nothing else.
  * The server deletes `OPENCODE_PASSWORD` and `OPENCODE_SERVER_PASSWORD` from
    its own environment, so tools it runs do not inherit them
    (`server-process.ts:86-89`).
  * The server treats EOF on its stdin as the end of its ownership lease.
  * The parent stops it with SIGTERM, then a forced kill after 3 seconds.
* Measured on the isolated probe:
  * the ready line arrived after 0.20 s;
  * closing stdin ended the server with exit status 0 in 0.04 s.
* Default mode (no `--stdio`) prints `server password <secret>` unless a password
  is in the environment, and it keeps that password in its own environment.
* v2 adds a **managed background service** (`opencode service
  start|restart|status|stop|get|set|unset`). Interactive commands (`opencode`,
  `run`, `mini`, `api`, `auth`, `models`, `session`) attach to it unless
  `--standalone` or `--server` is given.
  * The service registers itself in a 0600 file in the state directory that
    holds url, pid, version and the **password**
    (`packages/client/src/effect/service.ts:16-20`).
  * On this host `opencode service status` reports `http://127.0.0.1:49374`.
  * On Windows the managed port is `28894` (CHANGELOG v2.1.3).
* `opencode acp` always starts its own private `serve --stdio` child through the
  same standalone path (`commands/handlers/acp.ts`). It never attaches to the
  background service.

#### Authentication

* Every `/api/*` route requires HTTP Basic auth: user `opencode`, the server
  password (`packages/server/src/auth.ts`).
* Pairing links issue HMAC session tokens derived from the password, valid for
  30 days. Rotating the password revokes every token.
* `GET /api/credential` (`credential.list`) is documented as "List every stored
  integration credential, **including its secret value**". Anyone who holds the
  server password can read every provider key. That makes the password a
  credential-grade secret, as sensitive as the keys it unlocks.

#### HTTP API

* There is a single `/api/*` namespace: 149 operations on 124 paths, OpenAPI
  3.1, served at `/openapi.json`.
* Every v1 path Magic uses is gone. The upstream audit
  (`V2_HTTP_API_AUDIT.md`) records each one's disposition. The mappings that
  matter here:

| mcremote today (v1) | v2.1.4 |
| --- | --- |
| `GET /global/health` | `GET /api/info` → `{version, pid, urls, paths, capabilities}`; readiness is the status code |
| `GET /global/event`, `/event` | `GET /api/event` (all locations) |
| `GET /session?roots=true&limit=100` | `GET /api/session` with `limit`, `order`, `search`, `parentID`, `archived`, `directory`, `project`, `subpath`, `cursor`; returns `{data, cursor:{previous,next}}` |
| `POST /session`, `GET/PATCH/DELETE /session/{id}` | `/api/session`, `/api/session/{id}`; `PATCH` takes `title`, `metadata`, `permissions`, `archived` |
| `POST /session/{id}/message` (prompt) | `POST /api/session/{id}/prompt` `{text, files, agents, skills, metadata, delivery, resume}`: **durable admission**, returns the inbox item; the turn runs asynchronously |
| `POST /session/{id}/abort` | `POST /api/session/{id}/interrupt` → `{interrupted: bool}`; `resume=true` resumes steering input |
| `GET /session/status`, `/session/{id}/children` | `GET /api/session/active`; `GET /api/session?parentID=` |
| `POST /permission/{id}/reply`, `/session/{id}/permissions/{id}` | `POST /api/session/{id}/permission/{requestID}/reply` `{decision: once\|always\|reject, message?}` (field renamed from `reply`) |
| `GET/POST /question…` | **forms**: `GET /api/session/{id}/form`, `POST …/form/{formID}/reply` `{answer}`, `DELETE …/form/{formID}` |
| `GET /command`, command submit | `GET /api/command`; `POST /api/session/{id}/command` `{name, text, …}` (field renamed from `command`) |
| `GET /agent`, `/skill`, `/mcp`, `/vcs`, `/vcs/status`, `/project` | `/api/agent`, `/api/skill` (`location`→`path`), `/api/mcp`, `/api/vcs`, `/api/vcs/status`, `/api/project` |
| `GET /provider`, `/config/providers` | `GET /api/provider`, `GET /api/provider/{id}` (`activation`), `GET /api/model`, `GET /api/model/default` |
| `PUT/DELETE /auth/{id}`, `POST /provider/{id}/oauth/authorize` | integrations and credentials: `/api/integration/{id}/connect/{key\|oauth\|command\|external}`, oauth `status`/`complete`/`cancel`; `/api/credential/{id}` `activate`/`PATCH`/`DELETE` |
| `GET /lsp`, `GET /formatter` | **no v2 route** |
| `POST /instance/dispose` | `DELETE /api/debug/location` (evict) or `POST /api/location/reload` |
| `POST/DELETE /session/{id}/share` | **no v2 route**: sharing is not in the v2 HTTP contract |
| `GET /session/{id}/todo`, `todo.updated` | **none**. `todowrite` is back only behind `experimental.todo` and has no read API |
| revert / unrevert | `POST /api/session/{id}/revert/stage`, `/revert/commit`, `DELETE /api/session/{id}/revert` |
| file list, read, search | `GET /api/fs/list`, `GET /api/fs/read/*`, `GET /api/fs/find` (ranked; no full-text grep route) |

* Responses are **enveloped**: `{location:{directory}, data}` for location-scoped
  reads, `{data}` or `{data, cursor}` elsewhere. Errors are typed:
  `{_tag, message, …}`, for example `SessionNotFoundError`,
  `CommandNotFoundError`, `InvalidRequestError`, `ConflictError` and
  `FormAlreadySettledError`.

#### Location scoping

* **Directory scoping changed silently.** A request selects its location with
  the `location[directory]` query parameter, or the `x-opencode-directory`
  header (URL-encoded). Without either, it falls back to the server process's
  working directory (`packages/server/src/location.ts:71-79`).
* mcremote's `?directory=` is ignored. Probe: `GET /api/location?directory=<other>`
  answered with the server's own directory, not `<other>`.
* `session.create` takes its location from the **body** (`location.directory`),
  not the query:
  * a create with the query parameter alone landed in the server's directory;
  * a create with the body field landed in the requested one.

#### Event stream

* `GET /api/event` sends `data: {id, created, type, location, data:{…},
  durable?:{aggregateID, seq, version}}` plus `: heartbeat` comment lines.
  * The payload key is now `data`, not `properties`, and there is no `payload`
    wrapper.
  * mcremote's `DecodeFrame` therefore reads the type and an empty property
    set. `sessionIDOf` finds no session, and every frame is treated as
    engine-global.
* The stream is **volatile by contract**. Per the operation's own description:
  "a slow consumer overflows and fails the stream, and events during
  disconnection are missed". The per-subscriber queue holds 4,096 frames
  (`event-feed.ts`).
* Durable session events carry a per-session `seq`, so a gap is detectable.
  Ephemeral events have none: text, reasoning and tool-input deltas,
  `session.tool.progress`, `session.usage.updated`.
* The public session events are listed in full in
  `packages/schema/src/session-event.ts`. The `message.*` and `part.*` families
  mcremote consumes no longer exist; neither do `session.updated`,
  `session.error`, `session.compacted`, `question.*`, `todo.updated`,
  `command.executed`, `session.diff` and `permission.updated`.
* The v2 events:
  * `session.created|renamed|metadata.updated|agent.selected|model.selected|moved|forked|archived|unarchived|deleted|viewed|permissions`
  * `session.execution.started|succeeded|failed|interrupted`
  * `session.step.started|streamed|ended|failed` (with `cost`, `tokens`, `finish`)
  * `session.text.started|delta|ended`
  * `session.reasoning.started|delta|ended`
  * `session.tool.input.started|delta|ended`, `session.tool.called|progress|success|failed`
  * `session.retry.scheduled` (`attempt`, `at`, `error`)
  * `session.compaction.started|delta|ended|failed`
  * `session.inbox.enqueued|delivered|cancelled|delivery.changed`
  * `session.shell.started|ended`
  * `session.revert.staged|cleared|committed`
  * `session.skill.activated`, `session.synthetic`, `session.instructions.updated`
  * `session.usage.updated`
* Engine events now include `permission.asked|replied` (new schema),
  `form.created|replied|cancelled`, `model|provider|agent|command|skill|plugin|reference|integration|websearch.updated`,
  `credential.updated|switched`, `mcp.status.changed`, `pty.*`, `shell.*`,
  `vcs.branch.updated`, `worktree.*`, `location.shutdown` and `tui.*`.
* The 2.1.0 breaking changes renamed provider replay data to `native` and
  removed `session.compacted`, five days after 2.0.32.
* A lifecycle probe produced this event order:
  `server.connected`, `session.created`, `project.updated`, `session.renamed`,
  `session.agent.selected`, `session.inbox.enqueued`, then a catalog burst
  (`integration`, `provider`, `model`, `agent`, `command`, `skill`, `websearch`,
  `reference` and `plugin` `.updated`), then `session.archived`,
  `session.unarchived`, `session.forked`, `session.deleted`.
* `GET /api/experimental/session/{id}/log?after=&follow=` is meant to replay
  the durable log. On 2.1.4 it returned only `log.synced` (with the current
  `seq`), with or without `after` and `follow`, including while two renames
  happened. Whether it carries turn content is **unverified without a prompt**.

#### Sessions, turns and inputs

* Prompts go into a per-session inbox:
  * `delivery: steer|queue`, changeable while pending with
    `PATCH …/inbox/{id}`;
  * `resume: false` admits a prompt without running it.
* Other session operations:
  * `session.background` moves a backgroundable foreground tool to the
    background;
  * `session.fork` takes an optional `before` message ID;
  * archive and unarchive are back (2.1.0);
  * `session.generate` produces transient text from session context;
  * `experimental.session.wait` is a race-free idle barrier.
* Switching agent (`POST …/agent`) **accepts an unknown agent with 204**.
  * Probe: `{"agent":"nonexistent-agent"}` was stored on the session and
    recorded as an `agent-switched` message.
  * The engine does not validate agent IDs, so mcremote must.
* Sessions carry a **permission ruleset** (`[{action, resource,
  effect: allow|deny|ask}]`), settable at create or by `PATCH`.
* Saved permissions are listed, edited and removed under `/api/permission/saved`.
* Errors on a session carry `retryAfterMs` (CHANGELOG v2.1.2).

#### Credentials across processes

This was measured with two `serve --stdio` servers on one isolated home. The
credential was a fake key stored with `POST /api/credential`, which stores only;
no provider request was made.

* A credential created on server A was visible on running server B immediately:
  * `GET /api/provider/openrouter` went from `404` to `activation: "auto"`;
  * the integration's connection count went from 1 to 2.
* After A deleted it, B's connection count returned to 1, but B still reported
  `activation: "auto"`. **Creation propagates; removal leaves a stale provider
  activation in the other process.**
* v2 keeps credentials in the database, not in `auth.json`. Legacy `auth.json`
  is accepted only on import or migration.
  * mcremote's `opencodeAuthFingerprint` reads
    `$XDG_DATA_HOME/opencode/auth.json` (`credstore.OpenCodeAuthPath`).
  * Against v2 that fingerprint is therefore stale.

#### CLI surface

* Subcommands: `upgrade`, `uninstall`, `acp [--login]`, `api`, `debug
  (agents|config|heap-dump|paths)`, `auth (list|login|logout|export|import|switch)`,
  `mcp`, `plugin`, `models`, `stats`, `mini`, `run`, `session
  (list|delete|export|import)`, `service`, `reload`, `pair`, `serve`.
* Global flags: `--standalone`, `--server`, `--auto` ("auto-approve
  permissions that are not explicitly denied"), `--log-level`, `--print-logs`.
* `opencode api <operationId | METHOD path>` makes an authenticated request
  against the running service from the shell. That is useful for an operator
  doctor command, but not as a transport.
* `run --format json` exists. It is not a turn transport mcremote needs.
* `pair --remote` exposes the server through OpenTunnel. That overlaps
  mcremote's own reason to exist, and this record does not adopt it.

#### ACP surface (`opencode acp`)

* This was measured on an isolated home with no prompt sent. `initialize`
  answered in 0.35 s with:
  * `protocolVersion: 1`;
  * `loadSession: true`;
  * `promptCapabilities {image, embeddedContext}`;
  * `mcpCapabilities {http: true, sse: false}`;
  * `sessionCapabilities {list, fork, resume, close, delete,
    additionalDirectories}`;
  * `_meta {"opencode/child-session-updates": true}`;
  * one auth method, `opencode-login`, which runs `opencode auth login` in a
    terminal.
* `session/new` answered in 1.21 s with the session ID, in the same `ses_…`
  form the HTTP API uses. `session/list` also returned sessions created
  earlier over HTTP in the same home, so both surfaces read one session store.
* It also returned `configOptions`:
  * `model`: category `model`, **1,087 options**, which became 1,095 in the
    following `config_option_update` once the catalog refreshed. Every enabled
    model is listed, connected or not. On an empty home, `openrouter` alone
    contributed 372 enabled models.
  * `mode`: category `mode`, `build` and `plan`.
  * `effort`: category `thought_level`, per the source
    (`acp/config-option.ts:36-48`). It is offered only when the current model
    has variants.
* It sent `available_commands_update` (`init`, `review`, `compact`).
* The translator (`packages/cli/src/acp/translate.ts`) emits these update kinds:
  * `agent_message_chunk`, `agent_thought_chunk`, `user_message_chunk`;
  * `tool_call`, `tool_call_update`;
  * `usage_update`;
  * `compaction_update`, `compaction_summary_chunk`;
  * `session_info_update`, `config_option_update`, `available_commands_update`.
* It also emits opencode extensions:
  * `_meta["opencode/retry"]` on `session_info_update` and the prompt result;
  * `_meta["opencode/compaction"]`;
  * `opencode/session/child_update` notifications for client-requested child
    sessions, with `_meta["opencode/child-session"] {id, parentID, depth,
    title}` on projected updates and tool-call IDs prefixed `<childID>:`.
* Permission options are fixed:
  * `once` / `allow_once`;
  * `always` / `allow_always`;
  * `reject` / `reject_once`.
* MCP elicitation forms map to ACP `elicitation/create` when the client
  advertises `elicitation.form` (2.1.4).
* Closing stdin ended `opencode acp` with exit status 0 in 0.01 s.
* mcremote's `acpagent` already handles every standard kind above, including
  `compaction_update` (`compaction.go`) and `usage_update`. It declines
  `session_info_update._meta` and `compaction_update._meta`, which is where
  opencode's retry and compaction markers ride.

### mcremote features against v2

| mcremote surface (dialect method) | v2 HTTP | v2 ACP |
| --- | --- | --- |
| Session create, resume, delete, list (`Create`, `Resume`, `Delete`, `ListAgentSessionsLive`) | yes, with new envelope and body location | yes: `session/new`, `load`, `resume`, `delete`, `list` |
| Streaming text, thinking, tools (`HandleEvent`) | yes, but every event renamed and reshaped; volatile stream | yes, translated upstream |
| Abort (`Abort`) | `interrupt` | `session/cancel` |
| Permissions (`RespondPermission`, auto mode) | yes, `decision` field | yes: `session/request_permission` with exact kinds |
| Questions (`RespondQuestion`) | forms | `elicitation/create` (form) |
| Modes (`SetMode`) | `switchAgent` (unvalidated) | `mode` config option |
| Model, thinking (`SetModel`, `SetThinkingLevel`) | `switchModel` with `variant` | `model` and `effort` config options |
| Commands (`ListCommandsLive`, `submitCommand`) | `command.list`, `session.command` | `available_commands_update`; prompt `/name` |
| Subagents, session tree (`ConfirmTreeIdle`, `fetchChildren`) | `parentID` sessions, `session.active` | `opencode/session/child_update` (opt-in) |
| Compaction (`Compact`) | `session.compact` and lifecycle events | `/compact` and `compaction_update` |
| Usage and cost (`emitUsage`) | `step.ended`, `usage.updated` | `usage_update` and the prompt result's `usage` |
| Fork, rename (`Fork`, `Rename`) | yes | `session/fork`; rename has no ACP method |
| Revert, undo (`Revert`, `UndoLast`, `Unrevert`) | `revert/stage\|commit\|clear` | **none** |
| Shell (`Shell`) | `session.shell` | **none** |
| Share (`Share`, `Unshare`) | **none** | **none** |
| Todo panel (`handleTodoUpdated`) | **none** (experimental tool only) | **none** |
| Diff (`Diff`) | `session.diff` (structured, per turn) | tool-call diff content only |
| Diagnostics: MCP, skills, LSP, formatter | `/api/mcp`, `/api/skill`; **no** LSP or formatter | none |
| Workspace list, read, search | `/api/fs/*` (no text search) | none (the agent asks the client for fs; mcremote is the client) |
| Phone auth: key, oauth, device (`SetCredential`, `StartDeviceAuth`) | integrations and credentials | **terminal login only** |
| Active upstream (`SetActiveUpstream`), connected-provider grouping (0079) | `provider.activation`, integration connections | not exposed; the model option lists every enabled model |

### New in v2 and worth adopting

* **Child-session updates over ACP.** These lift
  [0195](./0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md)'s
  deferral for OpenCode: an engine now sends them.
* **MCP elicitation forms** (2.1.4), through mcremote's existing elicitation path
  ([0191](./0191-MADR-answer-agent-elicitation-forms-on-the-phone.md)).
* **Effort variants** as a `thought_level` config option
  ([0052](./0052-MADR-thinking-levels-and-settings.md)).
* **Compaction lifecycle** ([0190](./0190-MADR-show-agent-context-compaction-on-the-phone.md)),
  and the opt-in `experimental.background_compaction`.
* **Retry metadata** with the next-attempt time and `retryAfterMs`, for the
  limit card in [0073](./0073-MADR-goose-prompt-hang-and-debug-pass.md)'s
  family of fixes.
* **Session archive and unarchive**, and **fork before a message**.
* **Inbox steering vs queueing** for a prompt sent while a turn runs.
* **Structured per-turn diffs** (`session.diff`).
* **Saved-permission management** (list, edit, remove), and per-session
  permission rulesets.
* **Persistent PTYs**, now on Windows too, and the shell API with paged
  output. These are candidates for a later terminal record, not this one.
* **Client-hosted MCP tools** over the experimental client-MCP WebSocket
  (`connectClientMcp`). This is a candidate for phone-provided tools later.
* `OPENCODE_DISABLE_FREE_MODELS` and `experimental.location_idle_timeout`, as
  operator configuration.

## Decision Drivers

* **Restore every working phone flow on v2.1.4.** No silent regressions; a
  dropped feature is named and its reason recorded.
* **Track OpenCode without re-translating it.**
  * Its HTTP event contract broke in 2.1.0, five days after 2.0.32, and much of
    the surface is marked experimental.
  * OpenCode maintains its own event-to-ACP translator, replay and child
    projection.
* **Agent-neutral provider layer and provider parity**
  ([0184](./0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md),
  [0208](./0208-MADR-make-the-provider-layer-agent-neutral.md)): grok and gobble
  already run on `acpagent`. A fix there should help all three.
* **No secret in argv or logs** (organisation rule 007).
  * The v2 server password unlocks every stored provider key.
  * It must be generated per launch, passed only in the child's environment,
    never logged, and never persisted.
* **No orphan engines and no port races.** Prefer the engine's own lease
  (stdin EOF) and a `--port 0` ready line over choosing a free port and hoping.
* **Kilo must not regress.** It still speaks the 1.x-shaped API through
  `httpagent`.
* **The installed binary is the contract.** Every decision below rests on a
  live probe, or is marked as a gate that needs one.

## Considered Options

* **A**: Port the HTTP/SSE dialect to the v2 `/api` surface.
* **B**: Move OpenCode onto ACP only.
* **C**: ACP for sessions, plus a lease-bound v2 HTTP control plane for what ACP lacks.
* **D**: Attach to the user's OpenCode background service.
* **E**: Stay on OpenCode 1.x and defer v2.

## Decision Outcome

Chosen option: **"C: ACP for sessions, plus a lease-bound v2 HTTP control
plane"**, because:

* It restores the session loop on a protocol mcremote already implements and
  tests for two other agents.
* OpenCode, not mcremote, then owns the translation of its fast-moving events.
* It keeps the phone-side auth, catalog grouping, workspace and diagnostics
  features that only the HTTP API can serve.
* It does so with a password the engine never prints and a lease that cannot
  orphan.

The decision consists of these parts:

* **D1. Session transport.**
  * OpenCode sessions run on `opencode acp` through an `acpagent.Spec`, the way
    grok does: a thin `Spec` over the shared machinery.
  * `Spec.ClientCapabilities` advertises:
    * `fs.readTextFile` and `fs.writeTextFile`;
    * `terminal`;
    * `elicitation.form`;
    * `session.compaction`;
    * `_meta["opencode/child-session-updates"]: true`.
  * The per-session HTTP session code in `internal/provider/opencode` (dialect,
    tree demux, resync, SSE decode) is retired once the gates below pass.
* **D2. Control plane.** For the surfaces ACP lacks, mcremote owns one
  `opencode serve --stdio --port 0` child.
  * **Credentials:**
    * a per-launch random password goes in the child's `OPENCODE_PASSWORD`
      environment variable only;
    * requests use HTTP Basic, user `opencode`;
    * the password is never put in argv, logged or persisted.
  * **Startup:**
    * readiness is the single JSON ready line, followed by `GET /api/info` with
      a JSON content type and a version;
    * stdout keeps being drained after readiness, as `standalone.ts` does, so a
      later write cannot hit a closed pipe.
  * **Lifetime:**
    * the stdin pipe is the lease;
    * shutdown closes stdin, then sends SIGTERM, then kills after a bounded
      wait;
    * exit 0 after a lease close and 130 after SIGTERM are both orderly.
  * **Requests:**
    * every location-scoped request sends `location[directory]`;
    * `session.create`-style bodies carry `location.directory`.
  * **On demand:** it starts when one of those surfaces is used and stops after
    an idle period, so a host that never opens them pays for one engine, not
    two.
  * **Never:** it never calls `GET /api/credential`.
* **D3. Version policy.**
  * The OpenCode provider requires **≥ 2.1.0**, the release that fixed the
    current message and event names. `KnownGoodVersion` becomes `2.1.4`.
  * A 1.x engine is refused with an error that names the version found and
    `https://opencode.ai/v2/install`, rather than being half-driven.
  * The version comes from ACP `initialize.agentInfo.version`, and on the
    control plane from `/api/info`.
  * Kilo keeps `httpagent` and its 1.x dialect unchanged.
* **D4. Configuration.**
  * **Retired, with a validate error that names the key and this record (the
    `RetiredTransport` pattern):** `pure` (no v2 equivalent), `session_tree`
    (child sessions now arrive over ACP) and `stream_coalesce_ms`. On the last
    one, `acpagent` coalescing already applies, so the key is not needed;
    confirm in the PLAN whether to retire it or map it.
  * **Mapped onto the shared `ACPProviderConfig`:** `bin`, `always_approve`,
    `default_cwd`, `model`, `permission_timeout_seconds`, `prewarm` and
    `turn_stall_notice_seconds`.
  * **Dropped:** `allow_remote_share`, because v2 has no share API.
* **D5. Subagents.**
  * Handle `opencode/session/child_update` (`status`
    `created|running|completed|failed|interrupted`, and `update` frames) and
    `_meta["opencode/child-session"]` on projected updates.
  * Map both into the existing subagent panel.
  * Keep child tool calls out of the parent's tool list unless they are
    projected with the prefixed ID.
* **D6. Retry and limits.**
  * Read `_meta["opencode/retry"]` on `session_info_update` and on the prompt
    result.
  * A short retry is a notice. A quota or long back-off becomes the limit card
    with `RetryAt`, keeping parity with the HTTP provider's
    `handleSessionStatus`.
  * `_meta["opencode/compaction"]` is read and folded into the compaction
    state.
* **D7. Catalog.**
  * The ACP `model` option is the source of selectable IDs, along with
    `effort` and `mode`.
  * When the control plane is up, the option list is grouped into
    connected / not configured from `GET /api/provider` activation, preserving
    [0079](./0079-MADR-provider-model-drill-down-picker.md)'s drill-down.
  * The phone is never sent the full 1,000-plus flat list in one event without
    that grouping. The wire size is measured against
    [0181](./0181-MADR-configurable-mcremote-message-size.md).
* **D8. Phone auth.** Key, OAuth-attempt and command connects, `activate` and
  `remove` go through the control plane's integration and credential
  operations.
  * After a credential **removal**, the running ACP engine is restarted at its
    next idle point, because removal was measured not to clear another
    process's provider activation.
  * `opencodeAuthFingerprint`'s `auth.json` read is replaced by the
    integration's connections from the API.
* **D9. Permissions and auto mode.**
  * ACP's options carry exact kinds, so automatic choices select by kind:
    `allow_once` first, never a `reject_*`. For this provider, that removes the
    substring defect A2 named in acp-go-sdk2's
    `0035-REPORT-consumer-acp-code-for-the-sdk.md`.
  * mcremote's synthetic auto mode stays the mechanism.
  * Rulesets are not used for it, because a rule written through the control
    plane to a session owned by the ACP engine's process is unverified.
* **D10. Workspace, diagnostics and VCS** move to the control plane.
  * **Workspace:** `/api/fs/list`, `/api/fs/read/*` and `/api/fs/find`.
    Full-text search has no v2 route; it is answered by mcremote's own bounded
    search under the session root, or dropped. The PLAN chooses.
  * **Diagnostics and VCS:** `/api/mcp`, `/api/skill`, `/api/plugin`,
    `/api/vcs`, `/api/vcs/status` and `session.diff`.
  * **LSP and formatter status** are dropped; v2 has no route for them.
* **D11. Named regressions.** These are deferred to records of their own and
  not faked:
  * **share:** no v2 API;
  * **todo panel:** experimental only, with no read API;
  * **revert, undo and unrevert, and `!` shell:** ACP has no method for them.
    The control plane's routes act on the ACP engine's in-memory session from
    another process, which is unverified (gate G5).
* **D12. Hardening in `httpagent`, which kilo also benefits from.**
  * A JSON endpoint must answer `application/json` and an event stream
    `text/event-stream`; otherwise the request is an error naming the content
    type.
  * The health probe requires a decodable version.
  * These checks would have turned this outage into one clear log line.
* **D13. Live tests.**
  * Every OpenCode live test runs with an isolated home: `HOME`, `XDG_*`,
    `OPENCODE_DB`, `OPENCODE_CONFIG_DIR`,
    `OPENCODE_DISABLE_AUTOUPDATE`. The exception is the auth tests, which must
    see a real credential.
  * The probe facts above are pinned as `live_opencode` tests. Those that need
    no prompt spend no quota.

### Gates before implementation

These need quota, a host, or both, and each needs the owner's word.

* **G1.** One real ACP turn on a free model, recorded:
  * text and thinking;
  * a tool call with a permission;
  * a form elicitation;
  * a subagent (child updates);
  * usage;
  * a forced retry, if one can be provoked.
* **G2.** `session/load` and `session/resume` after an mcremote restart.
  Replayed history and cost must rebuild the transcript and the session total
  ([0199](./0199-MADR-total-grok-per-turn-costs-into-the-session-cost.md)'s
  pattern).
* **G3.** The `--stdio` lease, the environment password and `opencode acp`
  on the Linux test host and the Windows host. Windows pipes, signals and exit
  codes all differ.
* **G4.** Credential removal against a running ACP engine: does the engine
  keep using the removed key? This confirms or retires D8's restart.
* **G5.** The control plane's `revert/stage|commit` and `session.shell` on a
  session the ACP engine owns. This decides whether D11's deferral can be
  lifted.
* **G6.** The `config_option_update` size on the phone wire with a real
  credential set.

### Consequences

* Good, because the outage is fixed on a transport that two providers already
  exercise. OpenCode's event churn becomes the OpenCode team's problem, inside
  its own translator.
* Good, because subagents, elicitation forms, effort, compaction and retry
  metadata arrive through code mcremote already has. Only opencode's extension
  keys are new.
* Good, because the lease and environment password remove the free-port race,
  the orphaned engine and any chance of logging the server secret.
* Good, because D12 makes the next silent break, in either HTTP engine, loud.
* Neutral, because a host that uses phone auth or the workspace browser runs a
  second Bun process (the control plane), on demand and idle-stopped.
* Bad, because share, the todo panel, revert/undo, `!` shell and LSP/formatter
  status are lost for OpenCode until G5 or a later record restores them.
* Bad, because a 1.x OpenCode, still GitHub's Latest, is refused. An operator on
  1.x must install v2 or disable the provider.
* Bad, because about 6,750 production lines and 14,250 test lines in
  `internal/provider/opencode` are retired or rewritten. Their fixtures (`testdata/surface-1.18.21`, `wire/1.18.26`) stop being
  evidence for anything current.

### Confirmation

* `live_opencode` tests:
  * one for each probe fact above that needs no prompt: the help surface, the
    ready line, `401` without auth, JSON `/api/info`, the location parameter,
    the `initialize` capabilities and the `session/new` options;
  * the gate tests G1–G6 once run.
  * Each is seen to fail on a planted input, on a scratch copy.
* Unit tests on recorded ACP fixtures for:
  * child updates;
  * the retry and compaction `_meta`;
  * permission choice by kind;
  * catalog grouping.
* The `httpagent` content-type tests run against an HTML `200`, for both the
  OpenCode and kilo dialects.
* `make test`, `make race` and `make live-opencode` (with the owner's word on
  quota) pass on macOS, the Linux test host and the Windows host.
* The PLAN's execution record lists every dropped surface (D11) with its
  reason. None may be dropped silently.

## Pros and Cons of the Options

### A: Port the HTTP/SSE dialect to the v2 `/api` surface

Rewrite the dialect against `/api/*`: the envelope, the `data` payload, the new
event families, forms, the inbox, the location parameter and Basic auth.

* Good, because it keeps every v2 HTTP feature in one process: share is gone
  either way, but revert, shell, diff, fs, auth and rulesets stay.
* Good, because mcremote owns the session state directly, including the per-session
  durable `seq` for gap detection.
* Neutral, because `httpagent` remains the shared base for kilo either way.
* Bad, because it is a full rewrite of the event translator:
  * 95 event types in the generated v2 client's union, against a contract that
    broke in 2.1.0, five days after 2.0.32;
  * OpenCode already maintains a translator for exactly this, in ACP.
* Bad, because `/api/event` is volatile by contract:
  * overflow fails the stream, and missed events are gone;
  * the documented replay (`session.log`) returned no events in the probe;
  * correctness would rest on resync logic mcremote has to rebuild for v2.
* Bad, because none of the work transfers to grok or gobble.

### B: Move OpenCode onto ACP only

* Good, because it is the smallest change: one `acpagent.Spec` plus
  extension handlers.
* Good, because the engine's lease, replay, child projection and
  translation come with it.
* Bad, because it drops phone auth, connected-provider grouping, the
  workspace browser and diagnostics. ACP offers terminal login only, and
  lists every enabled model.

### C: ACP for sessions, plus a lease-bound v2 HTTP control plane

* Good, because it keeps B's small, shared session path and restores A's
  control-plane features.
* Good, because the control plane touches only stateless or database-backed
  surfaces. Cross-process credential creation was measured to propagate.
* Neutral, because the control plane is a second process, started on demand.
* Bad, because credential removal does not propagate to the ACP engine's
  provider state. D8's restart covers that until G4 says otherwise.
* Bad, because revert, shell and share stay unavailable until G5.

### D: Attach to the user's OpenCode background service

Read the service registration file (url, pid, version, password; 0600) and
drive the shared service over HTTP.

* Good, because sessions would be shared with the user's TUI and desktop app,
  a natural fit for [0078](./0078-MADR-session-handoff-and-receipt-surfacing.md)'s
  handoff.
* Bad, because mcremote would read another program's secret file and depend
  on a process it neither starts nor versions. The service may also be absent,
  for example on a headless Linux host.
* Bad, because it inherits all of A's translation and stream costs.

### E: Stay on OpenCode 1.x and defer v2

* Good, because the existing code works against 1.19.x, which is GitHub's
  Latest release.
* Bad, because the owner's hosts run v2, and the owner asked for v2.
* Bad, because v2 is where OpenCode develops: tags `v2.1.0` through `v2.1.4`
  landed between 2026-10-20 and 2026-10-23. The 1.x line will fall behind.

## More Information

### How the evidence was gathered

* All probes ran on 2026-10-25 against the installed binary, in an isolated home:
  * `HOME`, the `XDG_*` directories, `OPENCODE_DB` and `OPENCODE_CONFIG_DIR`
    pointed at a scratch directory;
  * `OPENCODE_DISABLE_AUTOUPDATE` and `OPENCODE_DISABLE_FILEWATCHER` were set.
* No probe read or wrote the owner's OpenCode configuration, credentials or
  database, and none sent a prompt.
* The only commands run against the real install were `opencode --version`,
  `opencode debug paths` and `opencode service status`, plus hashing the
  executable. Help output was captured in the isolated home.
* The probe scripts:
  * captured `--help` for every subcommand;
  * replayed mcremote's launch;
  * fetched `/openapi.json` and every read-only `GET`;
  * drove a session lifecycle while subscribed to `/api/event` and the session log;
  * spoke ACP `initialize`, `session/new` and `session/list`;
  * compared credentials across two servers.
* They live in the session scratchpad and are not committed. The PLAN converts
  the stable facts into `live_opencode` tests (Confirmation).
* Upstream sources read in the sibling checkout:
  * `V2_HTTP_API_AUDIT.md`;
  * `CHANGELOG.md` (v2.0.26 to v2.1.4);
  * `packages/cli/src/{server-process.ts,env.ts,services/standalone.ts,commands/handlers/*.ts,acp/*.ts}`;
  * `packages/server/src/{auth.ts,location.ts,event-feed.ts,routes.ts}`;
  * `packages/protocol/src/groups/event.ts`;
  * `packages/schema/src/session-event.ts`;
  * `packages/client/src/effect/service.ts`.

### Related records

* [0019](./0019-MADR-opencode-process-management-plan.md),
  [0020](./0020-MADR-opencode-session-tree.md),
  [0021](./0021-MADR-opencode-http-api-coverage.md),
  [0112](./0112-MADR-opencode-1.18.21-surface-parity.md) and
  [0137](./0137-MADR-prompt-to-first-token-latency-regression.md) describe the
  1.x HTTP provider this record replaces for OpenCode. Their kilo-shared parts
  stay.
* [0184](./0184-MADR-adopt-acp-go-sdk2-and-offload-protocol-handling.md) and
  [0208](./0208-MADR-make-the-provider-layer-agent-neutral.md) set the ACP and
  agent-neutral direction this follows.
* [0195](./0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md)
  deferred ACP subagent sessions until an engine sends them. OpenCode v2 now
  does (D5).
* [0074](./0074-MADR-remote-provider-auth-from-phone.md) and
  [0083](./0083-MADR-provider-auth-activation-and-layout-gaps.md) cover phone
  auth (D8). [0079](./0079-MADR-provider-model-drill-down-picker.md) covers the
  picker (D7).

### Numbering

This record takes 0214, the number `scripts/check_records.py --next` returned.
The adoption pair for acp-go-sdk2 issue #28, previously discussed as 0214, takes
the next free number when it is written.

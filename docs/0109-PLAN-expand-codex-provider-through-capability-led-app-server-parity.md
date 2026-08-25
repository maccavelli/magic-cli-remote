---
status: "in_progress"
date: 2026-08-25
associated-madr: "0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md"
owner: [Project Owner, Phase implementers]
target-milestone: "Phase P17 rollout readiness"
---

<!-- markdownlint-disable MD004 MD013 MD024 MD029 MD033 MD036 MD060 -->

# Plan: Implement capability-led Codex app-server parity

## Executive Summary & Goal

* **Associated Decision Record:**
  [0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md](./0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md)
* **Approval:** Approved by the Project Owner on 2026-08-20.
* **Goal:** Implement every accepted decision D1-D38 in MADR 0109 in dependency
  order, starting with the three observed protocol-correctness defects and
  ending with feature-rich, capability-negotiated Codex administration and
  runtime surfaces on the mobile client.
* **Accepted 0.149.1 delta:** On 2026-08-25, the Project Owner accepted D32-D38
  and every recommended policy resolution. Their implementation steps are now
  in scope, while phase execution still requires a separate explicit
  instruction.

The implementation is complete only when all of these success criteria are
true:

1. The daemon proves the running Codex binary's exact contract from a checked-in
   version manifest and independently gates every stable or experimental
   capability; `experimentalApi:true` is never treated as blanket support.
2. Structured questions, secret fields, command/file/network/granular
   approvals, MCP elicitation, and resolved callbacks use their exact Codex
   request and response types. The phone never displays a successful decision
   that Codex did not accept.
3. Thread-scoped and provider-scoped notifications have separate, exhaustive,
   fail-closed routing. Account, configuration, app, MCP, warning, and rate-limit
   state can no longer disappear because a payload lacks `threadId`.
4. The daemon can supervise Codex over stdio, daemon-owned Unix-socket
   WebSocket, and daemon-owned TCP WebSocket. WSS uses the daemon's existing
   TLS/auth boundary as a proxy; externally managed Codex endpoints remain out
   of scope.
5. The managed/native thread browser, permission and reviewer controls,
   status/usage/diagnostics, event fidelity, execution, terminals, queue,
   filesystem, audio/realtime, configuration, skills, hooks, apps, plugins,
   marketplaces, MCP, account, import, feedback, and agent-operated
   browser/computer-use surfaces satisfy the acceptance contracts below.
6. Stable app-server methods work without enabling experimental methods.
   Approved experimental adjuncts degrade independently to their documented
   stable or daemon fallback without disabling unrelated features.
7. Secrets, raw provider-global payloads, absolute paths outside an authorized
   root, account identifiers, tokens, browser-auth material, and configuration
   values marked secret never enter logs, transcript history, receipts, crash
   reports, or ordinary mobile persistence.
8. Existing providers, protocol-v1 clients, protocol-v2 clients that do not
   advertise the Codex surface, and old persisted session records remain
   readable and retain their current behavior.
9. Every phase's focused tests, pre-add checks, race suite, Flutter checks, and
   final no-model/live acceptance pass, and each phase is committed separately
   using the repository's generated commit-message workflow.
10. Deferred decisions remain unavailable with explicit capability reasons.
    Under accepted D1-D38 this plan does not implement Codex Remote Control,
    dynamic tool registration, memory administration, Codex Cloud, Windows
    sandbox setup, TUI presentation emulation, Bedrock setup, `/undo`, or
    `/redo`. Managed environments, capability-gated projects, default-off
    standalone processes, and redacted doctor diagnostics are in scope.

MADR 0109 is `accepted`, and this plan is approved. This approval-only change
does not start P0 or authorize product-file changes in the same commit; phase
execution begins only in response to a separate explicit instruction.

## Prerequisites & Dependencies

* **Decision authority:** MADR 0109 must remain `accepted`, and each product
  implementation phase requires a separate explicit execution instruction.
* **Provider-login dependency:** [MADR 0074 §15](./0074-MADR-remote-provider-auth-from-phone.md)
  and [0074-PLAN P17-P22](./0074-PLAN-remote-provider-auth-from-phone.md)
  exclusively own Codex account/API-key login, `CODEX_HOME`, credential
  backup/logout, and phone device-auth lifecycle.
* **MCP OAuth boundary:** This plan covers individual MCP-server credentials
  only and must not write or replace Codex `auth.json` outside the 0074
  coordinator.
* **Pre-flight evidence:** Before product changes, P0 must reconcile the current
  repository and installed Codex binary against the evidence baseline below.

## Evidence Baseline

This plan was written against repository commit
`802c53339b08e3da32e112362b078eb15a266679` on `master` and the evidence
captured in MADR 0109 on 2026-08-20. Both artifacts were then committed as
`b944880` ("docs(codex): add MADR and plan for app-server parity").

The tree has since moved and P0 must reconcile it before recording a baseline:
`ff92858` ("docs(auth): amend MADR-0074 to require transactional credentials")
landed after `b944880`, and an in-flight docs-only change adds MADR 0074
cross-references to eighteen records including both 0109 artifacts. None of it
touches product code, so the green baseline below is unaffected, but P0 records
the then-current commit rather than `802c533` and must not silently absorb
those uncommitted doc edits into a phase commit.

Re-verified on 2026-08-21 against the installed binary: `codex-cli 0.148.0`,
schema counts 95/72/10 stable and 141/72/11 experimental, and every stable
method this plan names present in the stable `ClientRequest` set. Every
experimental adjunct listed below is experimental-only; none is accidentally
stable.

### Reproduced local facts

| Area | Observed fact | Plan consequence |
| --- | --- | --- |
| Installed CLI | `/opt/homebrew/bin/codex` reports `codex-cli 0.148.0`; it is newer than repository evidence pinned to 0.147.0, 0.146.0, and 0.145.0. | P1 adds an exact 0.148.0 manifest and a repeatable drift probe; it does not impose an executable maximum version. |
| Installed schema | Stable generation contains 95 client requests, 72 server notifications, and 10 server requests. Experimental generation contains 141 client requests, 72 notifications, and 11 server requests. | P1 records stable and experimental inventories separately and refuses to infer method support from the aggregate counts. |
| Current tests | `go test ./internal/provider/codex ./internal/protocol ./internal/event ./internal/session ./internal/ws` passes. | This is the green baseline. Each phase must preserve it and add a failing test before production changes. |
| Current transport | `internal/provider/codex/provider.go` launches one supervised `codex app-server --listen stdio://`; `conn.go` is JSONL/stdio-specific. | P3 introduces a transport abstraction without changing stdio as the default. |
| Current routing | `provider.go` dispatches notifications only through `params.threadId`. | P2 adds explicit thread, provider, and ignored destinations before any provider-global feature is exposed. |
| Current questions | `session.go` decodes options as `[]string`, allocates local-only item ids, and returns `[][]string`. | P2 adopts option objects and an answer map keyed by upstream question id. |
| Current granular permissions | `session.go` funnels `item/permissions/requestApproval` through generic `{decision:"accept"}` handling. | P2 adds a method-specific subset grant `{permissions,scope}` and makes generic acceptance impossible for this callback. |
| Current app-server process | A single engine generation is shared by Codex sessions; process death disconnects all of them. | All provider-global caches and capability latches are generation-bound and rebuilt after replacement. |
| Current daemon protocol | Protocol v2 has additive `Caps`, a 1 MiB maximum frame, typed request payloads, auth, history, idempotency, and per-operation timeouts. | New features extend v2 additively under a nested Codex capability block; no protocol-v3 fork is introduced. All lists and file/output bodies are paged or bounded. |
| Current persistence | `session.Record` is additive JSON under the daemon data directory; the history ring holds 800 events. | New ordinary state is additive and backward compatible. Secret form values, audio, terminal byte streams, filesystem contents, and raw global state are never stored in the history ring. |
| Current native session UI | `agent_sessions.list` and a resume dialog exist, but Codex advertises `ListSessions:false`. | P6 replaces the Codex-only resume dialog with a unified managed/native browser while retaining the generic list operation for other providers. |
| Current queue UI | `chat_screen.dart` owns an in-memory `_QueuedPrompt` list and auto-flushes it when the agent becomes idle. | P8 moves queue authority to the daemon/provider and makes ordinary Send steer an active turn. |
| Existing dependency | `github.com/coder/websocket` is already used by daemon, relay, and tests. | P3 reuses it; no second WebSocket dependency is added. |
| Existing auth UI | `provider_auth_sheet.dart` obscures the API-key field, but generic `AuthInput` text controls are not secret-aware. | P2 introduces one reusable secret-input contract and applies it to questions, elicitation, login, connectors, and MCP. |

### P0 execution baseline drift — 2026-08-24

Execution began on repository commit
`3ad5533ceeb3c15568092bb026e9569e51ddde86` on `master`, with Go 1.26.5,
Dart 3.12.2, Flutter 3.44.6, and a clean local Codex source checkout at
`1f41cc5d92722748e45cae9cecc6d883a4e7cbb1`. The active binary is now
`codex-cli 0.149.0` with SHA-256
`bbc3341e44c9ead340ed9570c17be936e37870f570751a941699ffd04d672827`.

Fresh schema generation reproduced 95 stable requests, 75 stable
notifications, and 10 stable server requests; experimental generation produced
150 requests, 75 notifications, and 11 server requests. All stable methods
named by this plan and all approved experimental adjuncts remain present. This
is provisional P0 evidence: the Project Owner subsequently directed P0 to run
the supported `codex update` command before locking the execution baseline. P1
uses the exact post-update version and counts. The 0.148.0 facts above remain
the historical research baseline; they are not rewritten.

The baseline tests passed for `internal/provider/codex`, `internal/protocol`,
`internal/event`, and `internal/session`. `internal/ws` was blocked when the
restricted sandbox denied an `httptest` loopback listener. Direct Flutter
version probing was also blocked by the read-only mise SDK cache, although
`mise current flutter` reports 3.44.6. P0 is not accepted until the full command
passes in an environment that permits loopback sockets and Flutter cache
writes.

### Resolved post-update binary — 2026-08-24

The active standalone binary now resolves to `codex-cli 0.149.1` at
`/home/mac/.codex/packages/standalone/releases/0.149.1-x86_64-unknown-linux-musl/bin/codex`
with SHA-256
`73dc5888888f411c1f0fa7b81d866e721dcc86b527ce8e3b2cf4708661e823ba`.
`codex doctor --json` reports `latest version: 0.149.1` and that the current
version is not older. This assessment did not rerun the updater; it verified
the already-updated installation.

Fresh generation reproduces 95/75/10 stable and 150/75/11 experimental.
Compared with 0.148.0, the nine experimental requests are the seven
`project/*` methods plus `account/bedrock/discover` and
`account/bedrock/setup`. The three added notifications are `project/changed`,
`thread/project/updated`, and
`autoApprovalReview/strictReviewRequired`. A no-model live handshake also
confirmed six models, three profiles, 119 feature descriptors, provider
capabilities, content-free diagnostics, project and loaded-thread catalogs,
and workspace skill/hook discovery.

The 0.149.1 release has the same method counts as 0.149.0, but P1 still pins its
field contract, including stable `thread/start.threadSource`, initialization
extensions and notification opt-outs, WebSocket overload error `-32001`, and
all 75 notification shapes. The generated schema and binary digest outrank the
older aggregate counts.

### Updated code and source grounding — 2026-08-24

This revision was checked against mcremote commit
`3ad5533ceeb3c15568092bb026e9569e51ddde86` and the clean sibling Codex source
checkout at `../codex`, commit
`6143217c6730e147f4a1a5a3405d10f580fe9244` ("Cancel Guardian reviews with
their tool calls"). The existing documentation changes in the mcremote
worktree are not product evidence and must remain separate from phase commits.

The sibling source checkout is leading evidence, not the executable contract:

| Evidence | Stable requests / notifications / callbacks | Experimental requests / notifications / callbacks | Authority |
| --- | --- | --- | --- |
| Installed `codex-cli 0.149.1` | 95 / 75 / 10 | 150 / 75 / 11 | Authoritative for P1 capability enablement and fixtures. |
| `../codex` at `6143217c67` | 95 / 76 / 10 | 152 / 76 / 11 | Design and future-drift evidence only. |

The source-only method delta is exactly:

* experimental `mcpServer/event/stream/start`;
* experimental `mcpServer/event/stream/stop`; and
* stable notification `mcpServer/event/stream/notification`.

P1 records these three names in a source-head watch list. P13 does not add a
handler, capability id, wire operation, or UI for them while the installed
binary omits them. A future installed binary that contains them fails the P17
drift check and requires an amendment deciding event-stream ownership,
retention, cancellation, and fallback before implementation.

#### Current mcremote implementation anchors

| Current symbol | Observed code fact | Required change |
| --- | --- | --- |
| `codex.Provider`, `engine`, and `launchEngineProcess` in `internal/provider/codex/provider.go` | One shared child is launched as `codex app-server --listen stdio://`; `engine` stores one JSONL `conn`, one broad `experimental` Boolean, and no immutable contract snapshot. | P1 adds a generation-bound snapshot; P3 moves process launch behind a transport factory without changing stdio defaults. |
| `initializeParams` in `internal/provider/codex/provider.go` | It sends only `experimentalApi`; the response parser retains only `codexHome`. | P1 sends the accepted D33 typed capabilities, parses `userAgent`, `platformFamily`, and `platformOs`, and never exposes `codexHome` to the phone. |
| `conn` in `internal/provider/codex/conn.go` | Request correlation and JSON-RPC semantics are sound, but framing is newline-delimited `io.Writer`/`io.Reader`; `rpcErrorBody` already retains code, message, and raw data. | P1 classifies bounded error data without string scraping; P3 replaces only framing and I/O through a transport interface. |
| `routeNotification` and `routeServerRequest` in `internal/provider/codex/provider.go` | Both extract `threadId`; notifications with no matching loaded thread are logged and dropped, and callbacks without a matching thread receive `unknown thread`. | P2 installs exhaustive notification and callback registries before any provider-global surface is enabled. |
| `session` and `pendingPerm` in `internal/provider/codex/session.go` | The file is 2,301 lines; question options decode as ordered local values and granular permissions still share generic approval state. | P2 extracts callbacks/routing first; later phases add domain adapters rather than extending this monolith. |
| Optional interfaces in `internal/provider/provider.go` | Existing abstractions cover sessions, models, native-session listing, fork, diff, rename, and bounded diagnostics; there are no provider-global project, environment, configuration, extension, or account contracts. | Owning phases add narrow provider-neutral interfaces and compile-time assertions; raw Codex JSON never crosses the adapter. |
| `protocol.Envelope`, `AuthPayload`, and `Caps` in `internal/protocol/messages.go` | Protocol v2 is additive, but the client has no Codex-surface offer and `Caps` has no Codex block. | P2 adds the single version offer and optional response block with absent-field compatibility tests. |
| `Server.handleMessage`, `dispatchAsync`, `asyncOpTimeout`, and `idempotencyLedger` in `internal/ws/` | Dispatch is one switch; async work is capped at eight per client; timeouts are mirrored in `op_timeouts.json`; replay currently keys only `(deviceID, envelopeID)`. | P2 adds one table-driven Codex dispatcher; every new operation declares timeout, mutation class, capability id, and authorization in one registry. P3 extends the ledger key to include operation type. |
| `McremoteClient.request` in `apps/mobile/lib/data/ws/mcremote_client.dart` | It mints a UUID envelope id and, only when explicitly requested, retries once with the same id. | The envelope id is the operation id; no duplicate `operation_id` payload field is introduced. New methods opt into retry only according to the registry. |
| `session.Record` in `internal/session/store.go` | Persistence is additive JSON metadata; history is a separate bounded event file. | New durable fields remain ordinary identifiers/settings only. Secrets, process bytes, file contents, and raw diagnostics never enter either file. |
| Codex mobile surfaces | Protocol parsing, client calls, and chat UI are already large monoliths (`models.dart`, `mcremote_client.dart`, and `chat_screen.dart`). | New Codex models, operations, and screens use the fixed split in Scope; phase review rejects additions to those monoliths except one-line registration/delegation. |

#### Upstream source anchors

The implementation must cite these source files and mirror their named tests in
local fixtures; line numbers are deliberately omitted because the commit hash
is the stable locator.

| Surface | Source-of-truth types/handlers | Behavioral tests to mirror |
| --- | --- | --- |
| Initialize and thread source | `app-server-protocol/src/protocol/v1.rs`, `protocol/v2/thread.rs`, `protocol/v2/thread_data.rs`; `app-server/src/request_processors/initialize_processor.rs` | `app-server/tests/suite/v2/initialize.rs`, `client_metadata.rs`, `thread_start.rs`, and `thread_fork.rs` |
| Projects | `app-server-protocol/src/protocol/v2/project.rs`; `app-server/src/request_processors/projects.rs` and `thread_processor.rs` | `projects_persist_and_assign_threads`, `project_import_is_atomic_and_notifies_after_commit_in_order`, `projects_validate_filters_cursors_and_sqlite_less_assignment`, and `assigned_forks_inherit_projects_for_persistent_and_ephemeral_children` in `app-server/tests/suite/v2/projects.rs` |
| Environments | `app-server-protocol/src/protocol/v2/environment.rs`; `app-server/src/request_processors/environment_processor.rs` and `resolve_turn_environment_selections` | `environment_add.rs`, `environment_status.rs`, `environment_info.rs`, and `selected_environment.rs` |
| Standalone processes | `app-server-protocol/src/protocol/v2/process.rs`; `app-server/src/request_processors/process_exec_processor.rs` | `process_exec.rs`, including early spawn response, cap reporting, kill, and local-environment-disabled cases |
| WebSocket transport/auth | `app-server-transport/src/transport/websocket.rs`, `auth.rs`, and `transport/mod.rs` | `connection_handling_websocket.rs`, including health, Origin, both bearer modes, non-loopback refusal, and disconnect behavior |
| Managed daemon/proxy | `app-server-daemon/README.md`, `app-server-daemon/src/lib.rs`, and `cli/src/main.rs` | lifecycle JSON/status tests in `app-server-daemon` plus CLI daemon/proxy parse tests |
| Diagnostics | `app-server-protocol/src/protocol/v2/diagnostics.rs`; `cli/src/doctor.rs` | `server_diagnostics.rs` and `redacted_json_report_structures_and_sanitizes_details` |

No ACP command, protocol module, initialize capability, transport, or adapter
exists in these Codex source anchors. The existing mcremote Codex capability
test already requires ACP-specific fields to stay false. P1 and P17 preserve
that negative contract rather than manufacturing an app-server-to-ACP bridge.

### Installed stable method groups in scope

P1 must capture the complete generated inventory. The implementation phases
must, at minimum, account for these stable method groups observed in the
0.148.0 schema:

* thread lifecycle and organization: `thread/list`, `thread/read`,
  `thread/loaded/list`, `thread/start`, `thread/resume`, `thread/fork`,
  `thread/name/set`, `thread/metadata/update`, `thread/section/move`,
  `thread/archive`, `thread/unarchive`, `thread/unsubscribe`, `thread/delete`,
  and the `threadSection/*` CRUD methods;
* turn and execution: `turn/start`, `turn/steer`, `turn/interrupt`,
  `thread/shellCommand`, `command/exec`, `command/exec/write`,
  `command/exec/resize`, and `command/exec/terminate`;
* configuration and catalogs: `model/list`,
  `modelProvider/capabilities/read`, `permissionProfile/list`,
  `experimentalFeature/list`, `experimentalFeature/enablement/set`,
  `config/read`, `config/value/write`, `config/batchWrite`, and
  `configRequirements/read`;
* extensibility: `skills/list`, `skills/extraRoots/set`,
  `skills/config/write`, `hooks/list`, `app/list`, `app/read`,
  `app/installed`, `marketplace/*`, `plugin/*`, `mcpServerStatus/list`,
  `mcpServer/oauth/login`, `config/mcpServer/reload`,
  `mcpServer/resource/read`, and `mcpServer/tool/call`;
* filesystem: `fs/getMetadata`, `fs/readDirectory`, `fs/readFile`,
  `fs/writeFile`, `fs/createDirectory`, `fs/copy`, `fs/remove`, `fs/watch`,
  `fs/unwatch`, and `fuzzyFileSearch`;
* account and administration: `account/read`, `account/login/start`,
  `account/login/cancel`, `account/logout`, `account/rateLimits/read`,
  `account/usage/read`, `account/workspaceMessages/read`, reset-credit and
  add-credit-nudge actions, `externalAgentConfig/*`, and `feedback/upload`.

The installed stable server-request inventory is also explicit: command
approval, file-change approval, user input, MCP elicitation, granular
permissions, dynamic tool call, token refresh, attestation, and the two legacy
approval callbacks. D21 implements every stable elicitation form; D22 exposes
direct MCP calls. D30 requires typed rejection for attestation, D21 defers
dynamic-tool registration, and provider-owned token refresh is handled only by
the account/auth implementation in P14.

### Approved experimental adjuncts

Only the following experimental methods may be enabled by this plan, each with
its own capability latch and fallback: `thread/search`,
`thread/searchOccurrences`, `thread/turns/list`, `thread/items/list`,
`thread/settings/update`, `thread/queue/*`, `thread/backgroundTerminals/*`,
`thread/realtime/*`, session-based fuzzy search, `plugin/search`,
`server/diagnostics`, and `collaborationMode/list`. All other experimental-only
methods remain unavailable even if initialization accepted the experimental
flag.

Source-head methods are evidence for future compatibility only. They do not
become enabled until the installed-binary contract probe records them.

### Accepted 0.149.1 adjuncts

The following installed additions are authorized by D32-D38: `project/*` and
its experimental thread fields; administrator-owned `environment/add`,
`environment/status`, `environment/info`, and thread/turn environment
selection; and the explicitly unsandboxed `process/*` family. Each receives an
independent latch and fallback in P6/P7. Project or process availability must
not be inferred from the stable presence of their notifications.

The accepted amendment also adds transport and diagnostic adjuncts that are not
JSON-RPC method latches: app-server bearer authentication, health probes,
bounded-overload retry, an opt-in owned local-daemon/proxy mode, and explicit
`codex doctor --json` projection. Remote Control, arbitrary external endpoint
attachment, Bedrock setup, memory administration, and production plugin
mutation under the official maturity warning remain deferred.

## Scope

### Existing production areas expected to change

* `internal/provider/provider.go` and new domain files in `internal/provider/`
  for narrow optional interfaces and typed, provider-neutral values.
* `internal/provider/codex/` for schema contracts, capabilities, transport,
  routing, callbacks, provider-global state, thread administration, execution,
  extensibility, auth, and event translation.
* `internal/config/config.go`, `internal/config/load.go`, and
  `internal/daemon/daemon.go` for managed transport, roots, feature policy,
  confirmation defaults, and provider wiring.
* `internal/command/` and every provider command table for newly canonicalized
  commands and explicit unavailable mappings.
* `internal/session/` for authorization, persistence, queue/terminal state,
  lifecycle, and provider-domain delegation.
* `internal/event/`, `internal/protocol/`, and `internal/ws/` for additive
  events, typed operations, capability negotiation, bounded responses,
  idempotency, timeouts, and redacted dispatch.
* `apps/mobile/lib/data/`, `apps/mobile/lib/state/`, and
  `apps/mobile/lib/features/` for typed models, client methods, reconnect state,
  forms, screens, transcript cards, confirmations, and realtime signaling.
* `docs/protocol-v1.md`, `docs/config.md`, the Codex support matrix/README
  material, `Makefile`, and focused Go/Flutter tests.

### Files to add

The following file split is fixed so the implementation does not expand the
existing monoliths. Tests are paired with the production file in the phase that
introduces it.

* provider contracts: `internal/provider/capabilities.go`,
  `threads.go`, `execution.go`, `filesystem.go`, `extensions.go`,
  `account.go`, and `forms.go`;
* Codex contract and transport: `internal/provider/codex/contract.go`,
  `contract_test.go`, `capabilities.go`, `capabilities_manifest_test.go`,
  `transport.go`, `transport_stdio.go`, `transport_websocket.go`,
  `transport_test.go`, and `routing.go`;
* Codex domains: `callbacks.go`, `notifications.go`, `threads.go`, `queue.go`,
  `execution.go`, `filesystem.go`, `realtime.go`, `configuration.go`,
  `skills.go`, `hooks.go`, `apps.go`, `plugins.go`, `mcp.go`, `account.go`,
  `imports.go`, `feedback.go`, and `computer_use.go`, each with a same-name
  `_test.go` file; add focused `projects.go`, `environments.go`, `processes.go`,
  and `doctor.go` pairs rather than folding those accepted policy boundaries
  into generic execution or configuration files;
* exact-version evidence under
  `internal/provider/codex/testdata/0.149.1/`: `manifest.json`, sanitized
  request/response/notification fixtures, and `README.md` containing the
  generation command and binary digest;
* daemon layers: `internal/session/provider_ops.go`,
  `internal/session/queue.go`, `internal/session/terminals.go`,
  `internal/protocol/codex.go`, `internal/protocol/codex_test.go`,
  `internal/ws/codex_handlers.go`, and `internal/ws/codex_handlers_test.go`;
* event types: `internal/event/codex.go` and
  `internal/event/codex_test.go`; `event.Event` remains the envelope and gains
  only bounded typed pointers/summaries;
* mobile protocol/state: `apps/mobile/lib/data/protocol/codex_models.dart`,
  `apps/mobile/lib/data/ws/codex_client.dart`,
  `apps/mobile/lib/state/codex_provider_notifier.dart`,
  `apps/mobile/lib/state/codex_thread_notifier.dart`, and focused tests;
* mobile shared UI: `apps/mobile/lib/features/codex/codex_hub_screen.dart`,
  `secret_form.dart`, `confirmation.dart`, `paged_list.dart`, and
  `capability_unavailable.dart`;
* mobile domain screens under `apps/mobile/lib/features/codex/`: `threads/`,
  `runtime/`, `execution/`, `files/`, `realtime/`, `configuration/`,
  `extensions/`, `mcp/`, `account/`, `imports/`, and `feedback/`, with widget
  tests beside the existing `apps/mobile/test/` convention; add separate
  `projects/`, `environments/`, and `diagnostics/` domains for their sanitized
  projections.

If a file would exceed 800 lines, split it by request/response model versus
controller while preserving these package/domain boundaries. Do not put new
Codex handlers into `internal/ws/server.go` beyond switch registration and
shared authorization calls.

### Explicit non-goals

This plan does not:

* replace app-server with ACP, `codex exec`, an SDK, `codex mcp-server`, or
  Codex Remote Control;
* attach to a separately managed Codex endpoint;
* expose a raw JSON-RPC proxy or allow arbitrary app-server methods;
* implement memory administration, Cloud tasks, dynamic-tool registration,
  external-clock callbacks, Windows sandbox setup, or Bedrock account setup
  under this amendment;
* emulate the Codex TUI's theme, layout, terminal multiplexing, or manual
  remote desktop;
* map conversation rollback/revert to `/undo` or `/redo`;
* store or replay secret answers, raw audio, terminal bytes, file bodies, or
  unredacted provider-global data; or
* publish a commit, tag, release, PR, or push. Publishing requires a separate
  explicit user request in the same turn.

## Architecture & Technical Design Summary

### Capability manifest

`contract.go` defines a closed `CapabilityID` vocabulary grouped by the
phases below. `manifest.json` records, for each id:

* stability (`stable` or `experimental`);
* required request methods, notifications, server requests, and response
  fixture names;
* minimum observed exact version (historical decision baseline `0.148.0` and
  accepted execution fixture baseline `0.149.1`);
* probe kind (`schema`, `initialize`, `no_model_live`, or `behavioral_live`);
* fallback capability, if any; and
* security class (`read`, `write`, `destructive`, `secret`, `execution`, or
  `realtime`).

At engine initialization, the provider creates an immutable
`CapabilitySnapshot` containing binary path, reported version, executable
SHA-256, engine generation, experimental acceptance, supported ids, denied ids
with typed reasons, and probe time. Only the sanitized ids/reasons are exposed
to clients. A `method not found`, `invalid params`, unsupported capability, or
managed-policy rejection disables only the implicated id for that engine
generation. Transport death discards the snapshot.

The provider must never use version comparison alone to enable a method.
Version and digest identify evidence; schema presence plus the defined probe
enables the capability.

### Additive daemon protocol

Protocol v2 `Caps` gains an optional `codex_surface` object:

```text
version: 1
operations: sorted capability ids
experimental: sorted enabled experimental ids
max_page_size: 100
max_text_bytes: 262144
max_binary_chunk_bytes: 262144
```

The phone advertises `codex_surface_version:1` on the way up. No client
capability channel exists yet: `protocol.AuthPayload`
(`internal/protocol/messages.go:164-177`) carries only `token`, `protocols`,
`resume`, and `resume_window_ms`, and `Caps` is server-to-client in `auth_ok`.
P2 therefore adds one additive, omitempty client field to `AuthPayload` and to
the `pair.claim` payload that shares its version offer:

```go
CodexSurfaceVersion int `json:"codex_surface_version,omitempty"`
```

This matches the additive precedent set by `protocols` in MADR 0068 D1. Absent or
zero means the client does not support the surface. The server omits the
`codex_surface` block and all Codex-only provider pushes for such clients, and
v1 clients are unaffected because the field is ignored on the v1 path. A wire
test must prove an old client's auth payload still parses and still negotiates
successfully with the field absent.

Existing generic operations (`session.prompt`, `session.cancel`,
`agent_sessions.list`, provider auth, models, fork, rename, diff, diagnostics)
remain the primary operation when their contract suffices.

New operations use the `codex.` prefix and typed payloads in
`internal/protocol/codex.go`. Domain operation names are fixed as follows:

| Domain | Operations |
| --- | --- |
| Runtime | `codex.runtime.read`, `codex.permission_profiles.list`, `codex.thread_settings.update` |
| Threads | `codex.threads.list`, `.read`, `.search`, `.rename`, `.pin`, `.move`, `.archive`, `.unarchive`, `.delete`, `.unsubscribe`; `codex.sections.list`, `.create`, `.update`, `.delete`, `.reorder` |
| Queue | `codex.queue.list`, `.add`, `.update`, `.delete`, `.reorder`, `.start` |
| Execution | `codex.shell.start`; `codex.exec.start`, `.write`, `.resize`, `.terminate`; `codex.terminals.list`, `.terminate`, `.terminate_all` |
| Files | `codex.fs.metadata`, `.list`, `.read`, `.write`, `.mkdir`, `.copy`, `.remove`, `.watch`, `.unwatch`, `.search` |
| Realtime | `codex.realtime.start`, `.append_audio`, `.append_text`, `.append_speech`, `.stop`, `.voices`, `.signal` |
| Configuration | `codex.config.read`, `.write`, `.batch_write`; `codex.features.list`, `.set` |
| Extensions | `codex.skills.list`, `.read`, `.set_enabled`, `.set_roots`, `.refresh`; `codex.hooks.list`, `.set_enabled`, `.refresh`; `codex.apps.list`, `.read`, `.installed`, `.auth`; `codex.marketplaces.list`, `.add`, `.remove`, `.upgrade`; `codex.plugins.list`, `.search`, `.read`, `.install`, `.uninstall`, `.set_enabled`, `.share` |
| MCP | `codex.mcp.status`, `.oauth_start`, `.oauth_cancel`, `.reload`, `.resources`, `.resource_read`, `.tool_call`, `.cancel` |
| Account | `codex.account.read`, `.login_start`, `.login_cancel`, `.logout`, `.rates`, `.usage`, `.messages`, `.consume_reset_credit`, `.send_credit_nudge` |
| Import/feedback | `codex.import.detect`, `.preview`, `.start`, `.history`; `codex.feedback.submit` |
| Projects (D35) | `codex.projects.list`, `.read`, `.create`, `.import`, `.update`, `.move`, `.delete`; `codex.threads.set_project` |
| Environments (D36) | `codex.environments.list`, `.status`, `.info`; `codex.thread_environment.select` |
| Standalone processes (D37) | `codex.process.spawn`, `.write`, `.resize`, `.kill` |
| Host diagnostics (D38) | `codex.doctor.read` |

The existing envelope `id` is the caller-generated operation id. The Flutter
client already mints a UUID and reuses it for its single explicit idempotent
retry, so a second `operation_id` payload field would create two competing
identities and is forbidden. P3 changes the daemon ledger key from
`(deviceID,envelopeID)` to `(deviceID,operationName,envelopeID)`. Where Codex
requires a native idempotency key, the adapter derives
`mcremote:<device-id-hash>:<operation>:<envelope-id>`; it never generates a new
key during retry or engine replacement.

Read-only operations may retry explicit app-server `-32001` responses with
250 ms, 1 s, and 4 s jittered delays, bounded by the daemon operation deadline.
The Codex source proves `-32001` is emitted when the inbound request was not
enqueued, so an explicit overload response is a known non-execution outcome.
Project create/import may also retry after transport ambiguity because their
native `idempotencyKey` is durable. Every other destructive, login, feedback,
configuration-write, execution, environment-registration, and side-effecting
MCP operation must reconcile or fail as `outcome_unknown`; it is never
automatically replayed after EOF, timeout, or replacement.

### Provider contracts

New provider interfaces are narrow and optional. They use typed domain structs
from the new `internal/provider/*.go` files; no layer passes `map[string]any`
outside the Codex wire adapter.

* provider-level interfaces own native thread catalogs, account state,
  configuration, extensions, and provider-global diagnostics;
* session-level interfaces own thread settings, queue, execution, terminals,
  realtime, and session-scoped MCP calls;
* filesystem operations require a `provider.RootGrant` resolved by the daemon
  before delegation;
* forms use `Form`, `FormField`, `FormOption`, `FormAnswer`, and `FormAction`.
  Field ids are upstream ids, field kinds are `text`, `secret`, `boolean`,
  `single_select`, `multi_select`, `number`, and `url`, and nested validation
  errors are keyed by field id;
* all list results use `Items`, optional opaque `NextCursor`, `Total` when
  known, and `Source` (`native`, `daemon_fallback`, or `cache`);
* all unavailable results use a typed code from `unsupported`,
  `experimental_disabled`, `managed_denied`, `not_loaded`, `not_found`,
  `conflict`, `busy`, `invalid`, `secret_required`, or `provider_error`.

The session manager continues to own paired-device authorization and session
ownership. Provider-global read operations require an authenticated v2 device.
Provider-global writes additionally use existing mutating-operation audit and
idempotency machinery. No provider method receives a device credential.

### Secret path

All secret-bearing forms use one path:

1. Codex wire decoding marks the field secret without copying a default value.
2. The event or direct response contains metadata only.
3. The Flutter control is obscured, disables suggestions/autocorrect, and
   keeps its controller only for the visible sheet lifetime.
4. A dedicated typed response implements `protocol.LogValuer`, renders every
   secret as `[REDACTED]`, and zeroes the local Go string/byte slice immediately
   after the provider call returns.
5. Session history stores only a `resolved`/`cancelled` marker. Receipts record
   form id and action, never answers.
6. Reconnect cancels an unresolved secret form and requests a fresh callback;
   it never attempts to rehydrate the value.

Until this path is present in P2, every secret question or elicitation is
rejected with the typed reason `secret_required`.

### Bounds and persistence

* list page size defaults to 50 and is capped at 100;
* text/file/diff/config/diagnostic bodies are capped at 256 KiB after UTF-8
  validation; larger bodies return metadata plus `truncated:true` and a cursor
  or explicit unavailable reason;
* binary/audio chunks are capped at 256 KiB and never enter JSON logs;
* terminal output is a sequence-numbered live stream with a 1 MiB per-terminal
  daemon replay buffer, not session history;
* provider-global caches hold at most 1000 catalog entries per domain and are
  discarded on engine generation change;
* stable thread-list fallback walks at most 10 pages or 1000 threads and stable
  read/search fallback scans at most 200 loaded/read threads per request;
* filesystem watches are limited to 32 per device and are removed on device
  disconnect, session teardown, root revocation, or engine replacement;
* persisted `session.Record` gains only ordinary control state: permission
  profile, reviewer, queue metadata, section/pin metadata cache, and terminal
  identifiers needed for reconciliation. Payload bodies remain provider-owned;
* old JSON records with none of these fields load with current defaults.

### Notification destinations

`notifications.go` defines one exhaustive table for all 75 installed stable
notifications in 0.149.1 (the accepted 0.148.0 evidence began with 72). Each
entry declares exactly one destination:

* `thread`: route by validated `threadId` to one loaded session;
* `provider`: update a bounded generation cache and emit a sanitized provider
  state notification only to opted-in v2 clients;
* `thread_and_provider`: update both projections without duplicating a
  transcript item; or
* `ignored`: deliberate no-op with a comment and a fixture test.

Unknown notifications are counted and logged by method name only at warning
level. They never log params and never become transcript events. A contract
test fails if a manifest notification has no classification or if a handler
names a method absent from the manifest.

### Command model

Add canonical specs for `/status`, `/usage`, `/debug-config`, `/experimental`,
`/skills`, `/mcp`, `/approve`, `/ps`, `/stop`, `/archive`, `/delete`, `/import`,
`/apps`, `/plugins`, `/hooks`, `/feedback`, and `/logout`. Codex maps them to
typed operations, not native text forwarding. Every other provider table
receives an explicit existing-equivalent mapping or `KindNone` reason in the
same phase. `/permissions` remains the autonomy/profile entry point;
`/approve` performs an exact tracked Guardian-denial retry and exposes the
independent reviewer axis without widening the active profile. Exact
`!<command>` is handled as unsandboxed shell only after a dedicated
confirmation, while the structured execution screen defaults to sandboxed
`command/exec`.

`/stop <id>` and `/stop --all` parse strictly. Bare `/stop` displays usage and
does not guess a terminal. `/delete` always previews descendant impact and
requires a fresh confirmation. `/archive` archives the active native thread.
Presentation-only Codex commands remain absent with an explicit reason.

### Deterministic implementation map

The implementation uses one closed registry rather than independent switches
in the provider, WebSocket server, and phone. Each registry row contains:

* mcremote operation name and `CapabilityID`;
* exact Codex request, notification, callback, CLI command, or transport
  prerequisite;
* owner (`provider`, `session`, `host_admin`, or `connection`);
* security class and whether a fresh phone confirmation is mandatory;
* async timeout key from `internal/protocol/op_timeouts.json`;
* retry class (`read_only`, `native_idempotency`, `reconcile`, or `never`);
* response size bound and cursor behavior; and
* typed fallback or unavailable reason.

`internal/provider/codex/capabilities.go`,
`internal/ws/codex_handlers.go`, and the mobile capability projection consume
that registry through typed views. They do not maintain parallel method-name
lists. A test fails when an advertised operation lacks any registry field,
when a mutating operation has no confirmation/reconciliation rule, when an
operation timeout is absent from the JSON timeout mirror, or when a phone
method is not implemented by both the daemon dispatcher and Codex adapter.

Every request follows this fixed order:

1. authenticate the paired device and verify protocol-v2 Codex-surface
   negotiation;
2. resolve the registry row and reject an unknown operation;
3. read one immutable engine-generation capability snapshot;
4. enforce owner, session, root, managed-policy, and confirmation requirements;
5. consult the idempotency ledger using
   `(deviceID, operationName, envelopeID)`;
6. translate the typed request to the exact Codex params shape;
7. execute once, applying only the registry row's retry class;
8. validate and bound the Codex response before committing the ledger result;
   and
9. emit only the typed result or typed unavailable/error code.

An engine-generation change between steps 3 and 8 cancels connection-owned
work. Read-only work may restart against the new snapshot within its original
deadline. Mutating work follows its row's native-idempotency or reconciliation
rule and otherwise returns `outcome_unknown`.

#### Accepted 0.149.1 surface map

These accepted rows state the complete mapping so later implementation does
not invent wire behavior. Each row still requires its owning phase to be
explicitly authorized before product mutation.

| Decision / mcremote surface | Exact Codex surface | Owner and deterministic behavior | Retry, confirmation, and fallback |
| --- | --- | --- | --- |
| D32 contract fixture | installed schema generation plus `initialize` | Provider pins installed 0.149.1 digest and 95/75/10 stable, 150/75/11 experimental counts. `../codex` is a separately recorded watch input. | No runtime retry. Any installed shape/count drift blocks P17; source-only drift never enables a feature. |
| D33 initialization | `initialize.capabilities.{experimentalApi,requestAttestation,optOutNotificationMethods,extensions}` and response `{userAgent,codexHome,platformFamily,platformOs}` | Provider sends `requestAttestation:false`; advertises only implemented extensions; parses every response field but redacts `codexHome` from clients. The initial opt-out list is empty; any later entry is exact, sorted, fully classified, and proven redundant by P2 fixtures. | Retry initialization once without `experimentalApi` only for an initialization rejection, matching current behavior; never weaken another declared capability silently. |
| D33 thread attribution | stable `thread/start.threadSource` and `thread/fork.threadSource` | Session adapter sends the valid feature string `mcremote` on newly created or forked threads. Resume has no `threadSource` field and is unchanged. | If the exact field is absent, omit it and retain normal thread behavior. |
| D33 MCP extensions | `openai/form`, `openai/standard-form-input`, and `io.modelcontextprotocol/ui` in initialization `extensions` | Advertise only extensions for which P2/P13 has a complete renderer. The profile is immutable for a thread created, resumed, or forked on that initialized connection; subagents inherit it. `openai/standard-form-input` is client-only downstream, while Codex filters all unrecognized extension ids. | Prefer the typed map. Use `mcpServerOpenaiFormElicitation:true` only when the installed contract lacks `extensions` and the OpenAI-form renderer is complete. Otherwise omit and use typed unsupported-form fallback. |
| D34 direct transports | `codex app-server --listen stdio://`, `unix://...`, or loopback `ws://...`; native bearer auth; `/readyz`; `/healthz` | Provider transport factory owns the child/listener and keeps JSON-RPC correlation in `conn`. Any `Origin` header is forbidden. TCP app-server binds loopback only and still uses native bearer auth; a daemon WSS proxy connects to that authenticated listener. The phone sees neither endpoint nor credential. | Read-only explicit `-32001` retries at 250 ms, 1 s, and 4 s with jitter. EOF/timeout is ambiguous and follows the operation row. Stdio remains the default fallback. |
| D34 managed daemon | `codex app-server daemon start/status/restart/stop` and `app-server proxy` | Host provider parses the single JSON lifecycle object. It leases only a `started` result whose backend, PID, managed Codex path/version, socket path, CLI version, and app-server version match expectations. `alreadyRunning` is foreign ownership: fail closed, do not attach, restart, proxy, or stop. | Unix-only and opt-in. On lost ownership or version drift, invalidate the lease and fall back to configured stdio only after connection-owned work is cancelled; never mutate the foreign daemon. |
| D35 project reads | `project/list` and `project/read`; `project/changed` | Provider-global adapter always sends explicit list `limit:50`, follows at most 20 pages/1000 entries, validates opaque cursors, and updates a generation-bound cache from notifications. Native default 25 and maximum 100 are fixture assertions, not inferred defaults. | Read-only overload retry. Missing capability falls back to ungrouped native threads. |
| D35 project create/import | `project/create` and `project/import` | Provider validates trimmed nonempty name, absolute roots, logical and canonical root uniqueness, unique import thread ids, and native idempotency key length 1-512. Import response/notifications are applied only after the atomic response succeeds. | Fresh confirmation for imported thread assignment. Derive and reuse the native key from envelope identity; safe to replay after ambiguity. Reconcile by native key/project catalog before returning failure. |
| D35 project update/move/delete | `project/update`, `project/move`, and `project/delete` | Provider validates target and ordering ids. Delete preview states that threads are unassigned and neither threads nor files are deleted; notifications reconcile caches and thread projections. | Fresh confirmation for delete. No native idempotency: after ambiguity, re-read project list/thread assignments; return `outcome_unknown` if the result cannot be proven. |
| D35 thread assignment | experimental `thread/metadata/update.projectId`, `thread/list.projectId`, and `thread/project/updated` | Session adapter uses omitted = unchanged, empty string = clear, nonempty existing id = assign. Forked children inherit assignments upstream. | Reconcile with `thread/read`/filtered list after ambiguity. Missing field disables grouping without blocking thread operations. |
| D36 environment catalog | host configuration plus `environment/add`, `environment/status`, and `environment/info` | `environment/add` is host-admin startup/reload work only and upserts `(environmentId,execServerUrl,connectTimeoutMs)`. Plain `ws://` is accepted only for a loopback target; every other target requires `wss://`. Phone `codex.environments.list` projects configured ids; status is observational and does not reconnect; info exposes typed shell name/path and sanitized canonical cwd only. | No phone registration endpoint. Reads may retry explicit overload; host add never automatically replays after ambiguity and reconciles via status/info. Missing capability disables environment controls. |
| D36 environment selection | experimental `turn/start.environments` with `{environmentId,cwd,runtimeWorkspaceRoots}` plus thread start/resume/fork runtime-root fields | Session stores only the selected host-configured environment id and translated allowed roots, then injects the exact selection into the next `turn/start`; omitted preserves sticky upstream selection and empty disables it. It is not represented as a nonexistent Codex “select” RPC. | Fresh confirmation when changing host execution context. Validate environment readiness and absolute translated roots immediately before turn start; on failure leave the old selection unchanged. |
| D37 process spawn | `process/spawn`, `process/outputDelta`, and `process/exited` | Connection adapter generates a unique opaque `processHandle`, validates nonempty argv and absolute granted cwd, enforces tty/size implications, accepts environment names only from the host-configured allowlist, applies explicit output cap/timeout, and maps base64 output into the terminal sequence buffer. The upstream source logs argv at debug, so secret content is forbidden in argv. | Default-off and fresh unsandboxed-execution confirmation every spawn; never auto-replay after ambiguity. Missing local environment or capability keeps sandboxed `command/exec` as the default. |
| D37 process control | `process/writeStdin`, `process/resizePty`, and `process/kill` | Connection adapter accepts only handles it created in the current generation. Resize requires PTY; writes are bounded base64; disconnect, replacement, and explicit shutdown kill and forget all owned handles. | Control operations never replay after EOF/timeout. Explicit `-32001` may retry only while the same connection and handle remain live. |
| D38 diagnostics | explicit `codex doctor --json` subprocess | Any authenticated protocol-v2 device that negotiated Codex surface v1 may request it. Provider accepts only `schemaVersion:1`, `generatedAt`, `overallStatus`, `codexVersion`, and a bounded map of typed checks (`id`, `category`, `status`, `summary`, sanitized details/issues/notes/remediation, `durationMs`). It never invokes remediation and discards raw JSON after projection. | Read-only, single-flight, 30-second timeout, 256 KiB stdout/stderr cap. No periodic execution or extra confirmation; unknown schema version returns `unsupported`; ordinary runtime diagnostics remain available. |

ACP remains a negative contract: the source tree has no ACP command, module,
transport, or initialization capability, and mcremote continues to advertise
all ACP capability flags as false. Neither a source search nor future method
name resemblance can enable ACP without a new MADR amendment.

#### File and phase ownership

| Phase | Existing code changed first | New focused implementation | Required phase artifact |
| --- | --- | --- | --- |
| P1 | `internal/provider/codex/provider.go`, `conn.go` | `contract.go`, `capabilities.go`, exact-version fixtures and manifest tests | Deterministically sorted installed manifest, separate source-watch manifest, immutable generation snapshot. |
| P2 | `session.go`, `internal/protocol/messages.go`, `internal/ws/server.go` | `callbacks.go`, `notifications.go`, `routing.go`, protocol and dispatcher files | Exhaustive 75-notification/10-callback routing report and protocol-v2 wire compatibility fixtures. |
| P3 | `provider.go`, `conn.go`, config and daemon wiring | transport implementations and managed-daemon lease parser | Shared transport conformance report and lifecycle ownership matrix. |
| P4 | current session notification projection | item/event/runtime/doctor adapters | One-upsert-key event matrix and fully redacted diagnostic fixtures. |
| P6 | current native-session list/resume/fork adapters | provider thread/project contracts and Codex project adapter | Reconciliation state table covering success, overload, ambiguity, notification, reconnect, and delete. |
| P7 | current execution/session terminal paths | environment and standalone-process adapters | Execution-class audit matrix proving sandboxed, thread-shell, background-terminal, and standalone-process separation. |
| P13 | current MCP callback handling | MCP extension/form/tool adapters | Extension-profile fixture for create/resume/fork/subagent plus unsupported-widget fallback. |
| P17 | Make targets and support documentation | live contract/drift tests | Installed-vs-fixture diff, source-vs-installed watch diff, deferral audit, and redacted live-test report. |

Within a phase, implementation order is deterministic: provider-neutral types,
Codex wire fixtures, Codex adapter, daemon session delegation, WebSocket
registry, protocol documentation, mobile client/state, mobile UI, then full
phase verification. A later layer cannot define a new field or error code; it
must consume the type introduced by the owning earlier layer.

## Phased Implementation Plan

### Phase and commit contract

Phases execute strictly P0 through P17. A later phase may not begin while an
earlier phase has failing acceptance criteria. Within every phase:

1. update the task tracker with exactly one in-progress item;
2. write failing unit/fixture/wire/widget tests before production code;
3. implement only that phase's files and contracts;
4. run focused tests, then the affected package tests;
5. run `gofmt` on changed Go files and `dart format` on changed Dart files;
6. run `make pre-add-check FILES="<every changed Go file>"` before staging any
   Go file; run the Flutter checks named by the phase before staging Dart;
7. run `go test -race` for every changed Go package before the phase commit;
8. update protocol/config docs and the implementation log in this plan;
9. stage only the phase files and inspect `git diff --cached --check` and
   `git diff --cached --stat`;
10. commit with `git commit --no-edit` and record the generated commit id in the
    implementation log. Never use `-m`, `-M`, `-F`, or an editor override.

If a test exposes work outside the accepted MADR or this plan, stop, amend the
MADR/PLAN, present the amendment, and obtain fresh approval.

### Phase P0 - Accept artifacts and freeze the execution baseline

#### Objective

Convert the reviewed documents into the authorized implementation contract and
prove that the repository and installed binary have not drifted unnoticed.

#### Steps

1. Confirm MADR 0109 is `accepted` and this plan is approved. Both already
   carry that state and were committed as `b944880` on 2026-08-20, so this
   step is a verification, not a status edit. Retain the decision date.
2. Record the starting repository commit, branch, `git status --short`, Go,
   Dart, Flutter, Codex version/path/digest, and local Codex source commit in the
   implementation log. Do not silently absorb unrelated worktree changes.
3. Record the target and resolved path of the current standalone release. If
   the installation has not already advanced to the owner-directed latest
   release, run the supported updater exactly once:

   ```text
   codex update
   ```

   Record whether the updater ran and the post-update Codex
   version/path/digest. The supported command is `update`, not `upgrade`; an
   unknown bare token can be treated as interactive prompt input. For the
   current tree, the already-updated 0.149.1 binary and `doctor` latest-version
   check satisfy the version portion without rerunning the updater. Do not
   remove retained releases. If a required updater fails, verify the current
   binary still runs and stop P0 without advancing the baseline.
4. Regenerate stable and experimental schemas into a new `mktemp -d` directory
   and compare method counts/names with the MADR evidence. The temporary tree
   is not committed. Use exactly:

   ```text
   codex_evidence_dir="$(mktemp -d)"
   codex app-server generate-json-schema --out "$codex_evidence_dir/stable"
   codex app-server generate-json-schema --experimental --out "$codex_evidence_dir/experimental"
   codex app-server generate-ts --experimental --out "$codex_evidence_dir/ts"
   ```

   Record the path for review, then remove that exact temporary directory after
   the manifest comparison succeeds.
5. Run the baseline package tests and `git diff --check` on both artifacts.
6. Commit only the accepted MADR and approved PLAN, plus any amendment this
   reconciliation produces. The original acceptance commit is `b944880`; a
   further P0 commit is needed only when step 2 or 3 finds drift to record.

#### Verification

```text
git status --short
codex --version
shasum -a 256 "$(command -v codex)"
go test ./internal/provider/codex ./internal/protocol ./internal/event ./internal/session ./internal/ws
git diff --check -- docs/0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md docs/0109-PLAN-expand-codex-provider-through-capability-led-app-server-parity.md
```

Acceptance: both artifacts are accepted/approved; `codex update` completed or
confirmed the installation was already latest; post-update evidence drift is
either absent or documented in an approved amendment; and no product file
changed.

### Phase P1 - Exact-version contract manifest and capability negotiation

The 2026-08-24 execution-baseline evidence resolves P1's fixture target to
0.149.1: 95/75/10 stable and 150/75/11 experimental, with SHA-256
`73dc5888888f411c1f0fa7b81d866e721dcc86b527ce8e3b2cf4708661e823ba`.
Retain 0.148.0 and 0.149.0 references elsewhere as labeled historical and
provisional evidence. Accepted D32-D38 work remains independently capability-
gated even though the factual 0.149.1 manifest is authoritative.

#### Tests and fixtures first

1. Generate a candidate manifest from each schema's request `oneOf` method
   enum, notification `oneOf`, and server-request `oneOf`; normalize referenced
   method definitions, sort bytewise by JSON method name, and fail on a missing
   method enum, duplicate name, unresolved `$ref`, or method without params and
   response classification. Do not hand-type aggregate method inventories.
2. Add sanitized fixtures for every stable request, notification, and callback
   used by this plan, plus every approved experimental adjunct. Fixture tests
   assert request method, required params, response shape, and stability.
3. Add manifest tests that reproduce 95/75/10 stable and 150/75/11
   experimental for installed 0.149.1 and reject duplicate, unknown,
   unclassified, or fallback-cyclic capability entries. Generate a distinct
   source-watch manifest from `../codex` precomputed exports; assert its only
   present delta is the recorded MCP event-stream trio, without merging it into
   the executable manifest.
4. Add initialization tests for experimental accepted, experimental rejected,
   one experimental method missing, one stable method missing, engine
   replacement, and binary digest/version change. Pin accepted D33
   `threadSource`, typed MCP `extensions`, legacy OpenAI-form fallback,
   `requestAttestation:false`, and an initially empty notification opt-out
   list. A fixture may add one exact opt-out only after P2 proves the typed
   projection redundant.
5. Add a build-tagged `live_codex_contract` test that generates schemas and
   probes initialization/model/profile/thread/MCP catalogs without starting a
   model turn or spending tokens.

#### Production steps

1. Implement `CapabilityID`, manifest loading, validation, snapshot creation,
   per-generation disable, and sanitized diagnostics in `contract.go` and
   `capabilities.go`.
2. Bound and copy the structured code/message/raw-data fields already retained
   by `rpcErrorBody` in `conn.go`; classify known codes/data through typed
   predicates so capability handling never scrapes human strings when a
   machine-readable signal exists.
3. Bind a snapshot to `engine.generation`; replace current broad experimental
   booleans with individual latches while keeping compatibility accessors for
   existing collaboration/diff paths.
4. Stamp `threadSource:"mcremote"` on new/forked threads only; parse and retain
   `userAgent`, `platformFamily`, and `platformOs` from initialize; negotiate
   the supported MCP extension map; send `requestAttestation:false`; and send
   no notification opt-outs initially. Later opt-outs require an exact
   manifest entry and a P2 fixture proving the typed projection redundant.
5. Add `make live-codex-contract`; keep token-bearing targets separate.

#### Verification

```text
go test ./internal/provider/codex -run 'Test(Contract|Manifest|Capability|Initialize)'
go test -race ./internal/provider/codex
go test -tags live_codex_contract ./internal/provider/codex -count=1 -timeout 180s -v
```

Acceptance: capability output is deterministic for the same binary digest;
stable support works when experimental initialization is rejected; disabling
one capability leaves all unrelated capabilities enabled; no generated schema
tree is checked in.

### Phase P2 - Correct callbacks, secret forms, and provider routing

This is a blocking correctness phase. No feature phase may proceed until it is
green.

#### Tests first

1. Pin the 0.149.1 `item/tool/requestUserInput` request exactly as the installed
   schema declares it. `ToolRequestUserInputParams` requires
   `{threadId,turnId,itemId,isBlocking,questions}` and carries a deprecated
   nullable `autoResolutionMs`. Each `ToolRequestUserInputQuestion` requires
   `{id,header,question}` and may carry
   `options:[{label,description}]` (both option fields required),
   `isOther`, and `isSecret` — note the field is `isSecret`, not `secret`.
   Assert the phone event preserves every upstream question id and option
   description.
2. Assert question responses encode `{"answers":{"<upstream-id>":{"answers":[...]}}}`; cover single, multiple, Other, cancellation, duplicate response, resolved notification, and mismatched ids.
3. Assert a secret question is rejected before the secret path exists and,
   after implementation, never appears in event JSON snapshots, history,
   protocol logs, receipts, Flutter cache, or reconnect state.
4. Pin method-specific approval fixtures for command, file, network/granular,
   MCP, legacy apply-patch, and legacy exec-command callbacks. Assert each
   acceptance and rejection response type exactly.
5. For granular permission acceptance, assert the response is a subset of the
   requested network/filesystem grant, uses `scope:"turn"` (the
   `PermissionGrantScope` enum is `turn | session`, defaulting to `turn`), and
   can never contain `decision` — the installed
   `PermissionsRequestApprovalResponse` has no such member. Cover the optional
   nullable `strictAutoReview` flag in both states and assert a denial is an
   explicit empty grant rather than a dropped reply.
6. Add routing table tests for all 75 notifications, including null/missing
   thread id, unknown thread, provider-global rate limits, global warnings,
   server-request resolution, project changes, strict-review requirements, and
   unknown method redaction.
7. Add Go wire and Flutter widget tests for generic forms, nested validation,
   option descriptions, Other, secret entry, cancellation, reconnect, and
   exactly-once resolution.

#### Production steps

1. Replace `pendingPerm` with a typed callback record containing RPC id,
   callback kind, requested grant, allowed decisions, tool/detail metadata,
   and engine generation. Replace `pendingQuestions` with upstream field ids
   and form metadata.
2. Move callback parsing/response construction from `session.go` to
   `callbacks.go`. Generic decision helpers must reject granular permissions
   at compile-time through distinct types/functions.
3. Extend `event.QuestionItem` additively with `id` and `secret`, and extend
   `event.PermissionOption` additively with `description` — the upstream
   `description` belongs to the option, not the question. Reuse the existing
   `event.QuestionItem.Custom` flag for Codex `isOther`; do not add a parallel
   `allow_other` field, because `Custom` is already the cross-provider
   free-text affordance populated by `internal/provider/kilo/question.go:70`
   and `internal/provider/opencode/question.go:71`. Replace ordered question
   answers with a keyed map in the provider, protocol, manager, WS handler,
   mobile models, reducer, and sheet.
4. Implement the fixed secret path and redacted `LogValuer`. Clear secret
   values after provider dispatch and on every cancellation/error path.
5. Implement `routing.go` and the notification classification table. Move
   provider-global state out of session handlers; make existing rate-limit
   handling reachable through the provider destination.
6. Register every Codex phone operation in one table in
   `internal/ws/codex_handlers.go`. The table supplies its capability id,
   timeout key, mutability, authorization function, decoder, and handler; the
   `server.go` switch performs one delegation only. Generate the phone-visible
   operation list from this table and compare it with the P1 capability
   registry in a test.
7. Consume `serverRequest/resolved` authoritatively: close matching phone UI,
   clear pending provider state, and make late phone responses return
   `not_found` without a second Codex reply.
8. Document additive form/question fields and keyed-answer compatibility.

#### Verification

```text
go test ./internal/provider/codex -run 'Test(Question|Form|Secret|Approval|Permission|Routing|Notification|Resolved)'
go test ./internal/event ./internal/protocol ./internal/session ./internal/ws
go test -race ./internal/provider/codex ./internal/session ./internal/ws
cd apps/mobile && dart format --output=none --set-exit-if-changed .
cd apps/mobile && flutter analyze
cd apps/mobile && flutter test test/question_sheet_test.dart test/provider_auth_sheet_test.dart
```

Acceptance: captured 0.149.1 request/response JSON matches byte-semantic fixtures;
no approval kind can be answered with the wrong response struct; all 75
notifications are classified; secret sentinel values are absent from recursive
repository test output and serialized state.

### Phase P3 - Managed transports, supervision, and reconnection

#### Tests first

1. Define one transport conformance suite and run it against stdio, Unix-socket
   WebSocket, and TCP WebSocket fakes: request/response correlation, server
   request ids, notifications, cancellation, malformed frames, frame bounds,
   concurrent writes, EOF, and shutdown.
2. Add launch-argument tests for `stdio://`, `unix://<daemon-owned-path>`,
   `ws://127.0.0.1:<reserved-port>`, and `off` only during shutdown.
3. Add reconnect tests proving engine replacement creates a new generation,
   rebuilds capabilities/global state, reconciles loaded threads once, and
   marks stale pending callbacks cancelled.
4. Add proxy tests showing external clients cannot reach the Codex listener,
   WSS uses the daemon's authenticated WebSocket/TLS boundary, and no raw Codex
   JSON-RPC frame can be injected through mcremote.
5. Test capability-token file/verifier and signed-bearer launch modes,
   pre-initialize authentication failure, `/readyz`, `/healthz`, Origin
   rejection, and `-32001` retry versus unknown mutation outcome.
6. Test owned `app-server daemon` start/version/proxy/stop,
   every lifecycle status (`alreadyRunning`, `started`, `restarted`, `stopped`,
   `notRunning`, and `running`), malformed/multiple JSON values, version
   mismatch, an independently managed daemon, proxy loss, and Unix-only
   availability. No test may attach to or stop a daemon it did not start.

#### Production steps

1. Extract `transport` with `Send`, `Read`, and `Close`; adapt `conn.go` to it.
   Keep request ids and JSON-RPC semantics in `conn`, not transports.
2. Implement stdio JSONL and coder/websocket transports. Enforce loopback or a
   daemon-owned `0700` runtime directory and `0600` socket; reject wildcard
   bind addresses in configuration.
3. Extend Codex config with the exact enum
   `transport = stdio|unix_ws|ws|managed_daemon_proxy`, an optional managed
   loopback listen address, native WebSocket auth mode
   `capability_token|signed_bearer`, and `reconnect_attempts` capped at three.
   Reject auth fields for stdio, reject `managed_daemon_proxy` off Unix, and
   reject wildcard/non-loopback binds. Defaults remain stdio, no listen
   address, three attempts, and existing operation timeouts. Secret material is
   generated into a daemon-owned file and is never accepted inline in config.
4. Reserve the TCP port before launch, wait for readiness with a bounded
   backoff, and terminate the child if readiness or initialization fails.
5. Add daemon-side typed proxy operations; do not expose the Codex socket path,
   port, or bearer material to phones.
6. On unexpected death, cancel pending operations, retain daemon session
   records, launch at most one replacement, renegotiate, then resume/reconcile
   eligible sessions. Backoff is 250 ms, 1 s, 4 s, then disconnected until the
   next explicit provider action.
7. Update configuration docs with authority and threat boundaries.
8. Generate bearer material in a daemon-owned secret file,
   chmod it `0600` before launch, use native app-server authentication for
   non-loopback WS, preserve bearer verification through the WSS TLS proxy,
   and add health-aware readiness. The shared listener's `/readyz` and
   `/healthz` 200 response proves only transport liveness; successful
   authenticated initialization proves application readiness.
   Retry `-32001` with exponential backoff and jitter only when the operation
   is read-only or has a proven idempotency key.
9. Add `managed_daemon_proxy` as an opt-in Unix-only mode.
   Parse exactly one JSON object from each daemon command and validate backend,
   PID, managed path/version, socket path, CLI version, and app-server version.
   Record a lease only when `start` returns `started`; `alreadyRunning` is an
   ownership failure even when versions match. Use `app-server proxy` only
   under that lease, and refuse to attach to, restart, or stop a daemon whose
   ownership/version can no longer be proved. If the lease is lost, cancel all
   connection-owned work, invalidate the generation, and start the configured
   stdio fallback; do not wait for operator intervention.

#### Verification

```text
go test ./internal/provider/codex -run 'Test(Transport|Launch|Reconnect|Replacement|Proxy)'
go test ./internal/config ./internal/daemon ./internal/ws
go test -race ./internal/provider/codex ./internal/daemon ./internal/ws
```

Acceptance: the same conformance suite passes all three transports; stdio
behavior is unchanged; Codex listeners are daemon-owned and locally bounded;
WSS terminates TLS at mcremote and the underlying app-server connection uses
Codex's native bearer authentication before
`initialize`; bearer material never reaches the phone.

### Phase P4 - Event fidelity, runtime status, and diagnostics

#### Tests first

1. Add fixture tests for every implemented item delta/final pair, readable
   reasoning, progress, terminal interaction, command exit, patch updates,
   Guardian review, model reroute/verification/safety buffering, warnings,
   config/deprecation notices, and turn terminal states.
2. Assert a started item creates one card, deltas update it, completion closes
   it, and later authoritative completion cannot create a duplicate.
3. Add status snapshot tests covering account plan, usage/rates/workspace
   messages, model capabilities/context, transport/generation, config
   provenance, feature state, and bounded MCP diagnostics.
4. Add Flutter reducer/golden tests for progress, warning, reroute, verification,
   resolved, and terminal interaction cards.
5. Capture a sanitized `codex doctor --json`
   `schemaVersion:1` fixture with `generatedAt`, `overallStatus`,
   `codexVersion`, and the checks map keyed by check id. For every check, pin
   `id`, `category`, `status`, `summary`, a `details` object whose label values
   are string-or-string-array, optional `issues` and `notes`, nullable
   `remediation`, and `durationMs`. Each issue pins `severity`, `cause`,
   nullable `measured`/`expected`/`remedy`, and `fields`. Test timeout, nonzero
   overall status, key/id mismatch, unknown checks, unknown schema version,
   payload bounds, secret/path redaction, concurrent requests, and proof that
   no remediation or upload command runs. Mirror upstream
   `redacted_json_report_structures_and_sanitizes_details` with a local
   sentinel fixture.

#### Production steps

1. Move notification decoding from `session.go` to `notifications.go` and item
   lifecycle projection to focused helpers in `items.go`.
2. Add bounded event types/payloads in `event/codex.go`; register every type in
   `event.Types`, `IsControl`, transcript reduction, replay, and protocol docs.
3. Build a typed runtime snapshot in provider-global state and expose it via
   `codex.runtime.read`; extend existing diagnostics only with sanitized typed
   summaries, never raw paths/URLs/config.
4. Add a Codex hub Runtime screen and command mappings for `/status` and
   `/usage`.
5. Preserve unknown item types as a sanitized unsupported card only when they
   affect turn completion; otherwise count and ignore them fail-closed.
6. Preserve bounded memory-generated notifications/items as ordinary readable
   events when present, without adding memory administration controls.
7. Add an explicit host-diagnostics operation that invokes
   the exact argv `codex doctor --json` out of process, single-flight, with a
   30-second timeout and 256 KiB combined output bound. Accept only
   `schemaVersion:1`; project known categories into typed summaries, reduce
   unknown checks to id/status/summary, discard the raw report after the
   response, persist no report fields, and never run it periodically. Permit
   any authenticated protocol-v2 device that negotiated Codex surface version
   1 to call it; no additional confirmation is required because the projection
   is read-only and redacted.

#### Verification

```text
go test ./internal/provider/codex -run 'Test(Item|Event|Runtime|Status|Diagnostic|Rate|Warning|Guardian|Model)'
go test ./internal/event ./internal/protocol ./internal/session ./internal/ws
go test -race ./internal/provider/codex ./internal/event ./internal/ws
cd apps/mobile && flutter analyze && flutter test test/codex_runtime_test.dart test/transcript_reducer_test.dart
```

Acceptance: every implemented lifecycle has one stable upsert key and terminal
state; provider-global rate limits are phone-visible; diagnostics contain no
forbidden raw values and remain under the response bound.

### Phase P5 - Permission profiles, reviewer axis, and managed settings

#### Tests first

1. Pin `permissionProfile/list` built-in/custom profiles, descriptions, and
   managed `allowed` state; cover missing method fallback to the legacy four
   modes.
2. Exercise the full matrix of permission profile, sandbox, reviewer
   (`user`/`auto_review`), and unattended mode. Assert changing one axis does
   not silently widen another.
3. Assert `/approve` can retry only the exact tracked Guardian-denied action
   once, and that reviewer-policy changes never change the permission profile;
   `/permissions` changes only the profile/autonomy choice.
4. Add config provenance and atomic write tests for user/project/managed layers,
   conflict, denied key, hot reload, engine replacement, and old record load.
5. Add Flutter tests for managed-disabled choices, dangerous confirmation,
   effective-versus-requested state, and reconnect convergence.

#### Production steps

1. Add dynamic profile catalog and `ApprovalsReviewer` state to provider
   contracts, `StartOptions`, session meta/record, Codex session state, and
   turn/start/settings construction.
2. Read effective state from authoritative settings notifications and config
   provenance. Managed requirements override user/project writes and are shown,
   not hidden.
3. Implement `/approve` with tracked denial id/generation/one-shot semantics,
   add the independent reviewer control, expand `/permissions`, and update
   every provider command table/conformance test with explicit mappings.
4. Add profile/reviewer controls to the Runtime screen and preserve the current
   unattended `auto` mode as a separately labeled dangerous choice. Omit
   managed-disallowed profiles from the chooser, while an authenticated policy
   detail explains the governing requirement without exposing secret values.
5. Persist only ids and ordinary state. Do not persist policy documents or
   developer-instruction contents.

#### Verification

```text
go test ./internal/provider/codex -run 'Test(PermissionProfile|Reviewer|Guardian|Settings|Provenance)'
go test ./internal/command ./internal/session ./internal/protocol ./internal/ws
go test -race ./internal/provider/codex ./internal/session
cd apps/mobile && flutter analyze && flutter test test/codex_permissions_test.dart
```

Acceptance: catalog-driven profiles replace hard-coded choices when supported;
legacy fallback remains usable; managed-denied controls explain why; no matrix
transition widens sandbox or approval authority implicitly.

### Phase P6 - Unified managed/native thread browser and sections

#### Tests first

1. Pin stable `thread/list`, `thread/read`, loaded-list, resume, fork, rename,
   metadata, archive, unarchive, unsubscribe, permanent delete, and section CRUD
   fixtures, including cursors and unloaded threads.
2. Test stable bounded search fallback and independently negotiated native
   search/turn/item pagination. Assert source labels and limits.
3. Test managed/native identity reconciliation, loaded-thread adoption,
   duplicate suppression, cold replay, reconnect reconciliation, and aliases.
4. Test archive versus permanent delete, descendant impact preview, loaded
   descendants, partial failure, and idempotent reconciliation after unknown
   outcome.
5. Add mobile tests for filter/search/pagination, pin/section ordering, empty
   sections, archive/unarchive, rename, fork, replay, and destructive confirm.
6. Pin all seven project methods, experimental thread
   project fields, `project/changed`, and `thread/project/updated`. Test
   idempotent create/import, pagination, ordering, root validation, assignment,
   fork inheritance, reconnect, and delete-unassigns-without-deleting. Mirror
   the upstream behavioral cases
   `projects_persist_and_assign_threads`,
   `deleted_project_is_dropped_before_first_durable_thread_persistence`,
   `project_import_is_atomic_and_notifies_after_commit_in_order`,
   `projects_validate_filters_cursors_and_sqlite_less_assignment`, and
   `assigned_forks_inherit_projects_for_persistent_and_ephemeral_children`.

#### Production steps

1. Expand `provider.AgentSessionMeta` additively with native status, archived,
   pinned, section, parent/fork metadata, source, loaded, and pagination fields.
2. Implement provider-level thread/section interfaces and Codex adapters.
   Preserve the generic `agent_sessions.list` path for existing providers; route
   Codex UI to the richer typed operations when advertised.
3. Add additive managed-record aliases linking local session id to provider
   thread id. Reconciliation prefers exact provider id and never creates a
   second managed record for the same loaded thread.
4. Implement history replay from `thread/read`; stamp replay events through the
   existing manager path and never broadcast them live to an already hydrated
   transcript.
5. Build the unified Threads screen and replace the Codex resume dialog. The
   section editor includes create, rename, icon, color, drag ordering, pin,
   move in/out, and confirmed delete that preserves and unsections members.
6. Implement `/archive` and `/delete` against active native threads with the
   fixed confirmations and explicit descendant result.
7. Add project grouping and filters to the Threads screen.
   Project create/import/update/move/delete use exact capability latches;
   deletion preview states that member threads and filesystem roots are
   preserved. Send `limit:50` explicitly for project lists, stop after 20 pages
   or 1000 projects, and fixture the upstream default of 25 and maximum of 100.
   Before create/import/update, reject a blank trimmed name, relative roots,
   logical or canonical duplicate roots, and duplicate import thread ids.
   Create/import reuse the envelope-derived native idempotency key; update,
   move, delete, and project assignment reconcile with reads after ambiguity.
   Assignment uses `thread/metadata/update.projectId` exactly: omit means no
   change, empty clears, and an existing id assigns. Absence falls back to the
   existing ungrouped browser.

#### Verification

```text
go test ./internal/provider/codex -run 'Test(Thread|Section|Search|Archive|Delete|Replay|Reconcile)'
go test ./internal/session ./internal/protocol ./internal/ws
go test -race ./internal/provider/codex ./internal/session ./internal/ws
cd apps/mobile && flutter analyze && flutter test test/codex_threads_test.dart test/sessions_screen_test.dart
```

Acceptance: all observed native threads are browseable and resumable without
duplication; every mutation reconciles after reconnect; stable fallback remains
bounded when experimental pagination/search is absent; permanent deletion is
never one tap.

### Phase P7 - Sandboxed execution, explicit unsandboxed shell, and terminals

#### Tests first

1. Pin `command/exec` start/write/resize/terminate and `thread/shellCommand`
   response/output fixtures. Assert their labels, policy checks, and audit
   records are distinct.
2. Test exact `!` parsing, blank command, control characters, cwd/root
   validation, fresh confirmation, cancellation, timeout, output bounds, and
   unknown outcome.
3. Test experimental background terminal list/terminate/clean fallback,
   daemon terminal registry, phone detach, session detach, app-server shutdown,
   engine replacement, `/ps`, `/stop <id>`, and `/stop --all`.
4. Add mobile terminal tests for streamed sequence gaps, replay buffer,
   interactive stdin, resize, stop, exit status, and unsandboxed warning.
5. Test administrator-configured environment registration,
   status/info, thread/turn selection, root translation, policy denial,
   connect/disconnect events, credential redaction, and an arbitrary phone URL
   rejection. Assert `environment/add` upserts the tuple
   `(environmentId,execServerUrl,connectTimeoutMs)`, status returns exactly
   `ready|pending|disconnected|unknown` without triggering recovery, and info
   returns shell name/path plus an optional canonical-URI cwd.
6. Pin `process/spawn`, write, resize, kill, output, and
   exit. Test argv-only commands, cwd/root validation, environment-delta
   allow/deny rules, secret-name rejection, fresh confirmation, output caps,
   timeout, tty-implies-streaming, size-requires-tty, duplicate handles,
   base64 validation, cap-reached final chunks, disconnect, and generation
   cleanup.

#### Production steps

1. Implement typed execution interfaces and Codex adapters. Sandboxed
   `command/exec` is the default structured action. Unsandboxed
   `thread/shellCommand` and exact `!` require an unmistakable label and fresh
   confirmation for every invocation.
2. Add a daemon terminal registry keyed by provider generation/thread/terminal
   id, with a 1 MiB sequence buffer and no transcript persistence.
3. Use native background terminals when negotiated; otherwise maintain only
   terminals created through mcremote and return `native_unavailable` for
   unknown Codex terminals.
4. Keep terminals alive across phone and daemon-session detach. Explicit stop,
   app-server shutdown, process exit, or engine death closes them.
5. Add `/ps` and strict `/stop`; update all command tables and docs.
6. Let the daemon register only host-configured execution
   environments from `providers.codex.environments`, whose entries contain
   `id`, `exec_server_url`, `connect_timeout_ms`, and allowed
   `runtime_workspace_roots`. Reject empty/duplicate ids, non-WS(S) URLs,
   nonpositive or over-60-second connect timeouts, relative roots, and
   duplicate canonical roots at config load. Permit `ws://` only when the
   parsed host is `localhost` or a literal loopback address, and verify the
   connected peer is loopback; require `wss://` for every other host. Expose
   status/info plus allowed selection. The phone never submits or persists
   `execServerUrl` or remote credentials. Selection is adapter state, not a
   fabricated upstream RPC: validate the configured `environmentId`, cwd, and
   translated absolute runtime roots, then inject them into
   `turn/start.environments`; omitted preserves the sticky upstream selection
   and an explicit empty list disables it.
7. Add a default-off standalone terminal mode over
   `process/*`, enabled only by
   `providers.codex.standalone_processes_enabled:true`. Add the host-only list
   `providers.codex.standalone_process_env_allowlist`, default empty; reject
   empty/duplicate/invalid environment names at config load, and reject every
   phone delta name not on that list. Generate process handles in the adapter,
   bind them to the current connection and engine generation, and reject all
   foreign/stale handles. Supply explicit bounded output caps and timeouts;
   decode base64 notifications into the existing sequence buffer; strip
   upstream-defined non-inheritable environment variables before applying the
   allowlisted delta; and kill/forget every remaining handle on connection
   close or engine replacement. Show host, argv, cwd, and the names of
   environment changes before a fresh confirmation for every spawn; never put
   secret content in argv because upstream debug logs record argv, and never
   show or persist secret values. Keep this mode separate from sandboxed
   `command/exec`, thread shell commands, and native background terminals.

#### Verification

```text
go test ./internal/provider/codex -run 'Test(Shell|Exec|Terminal|Output|PTY|Environment|Process)'
go test ./internal/command ./internal/session ./internal/protocol ./internal/ws
go test -race ./internal/provider/codex ./internal/session ./internal/ws
cd apps/mobile && flutter analyze && flutter test test/codex_execution_test.dart
```

Acceptance: sandboxed and unsandboxed execution cannot be confused in API, UI,
audit, or tests; terminal state survives detach and never survives app-server
shutdown; output bounds and sequence recovery are deterministic.

### Phase P8 - Steering and persistent queued turns

#### Tests first

1. Assert ordinary Send during a live turn calls stable `turn/steer`, never
   silently appends to the queue.
2. Pin native `thread/queue/*` fixtures and test add/list/update/delete/reorder/
   start/change notifications independently.
3. Test daemon memory fallback for every queue operation, explicit source
   labeling, engine replacement, session close, phone disconnect, duplicate
   operation ids, and queue-start failure.
4. Add Flutter tests replacing `_QueuedPrompt`: explicit Queue action, edit,
   reorder, delete, start, attachment metadata, and concurrent updates.

#### Production steps

1. Add typed queue state to provider/session/protocol and a daemon-owned
   fallback queue. Persist fallback queue metadata/content only as ordinary
   prompt data under the session store; never persist audio bytes or secrets.
2. Route normal composer Send to `session.prompt` when idle and `turn/steer`
   when running. Add a separate Queue action.
3. Use native queue methods/notifications when negotiated; migrate an empty
   fallback queue to native only after a successful list reconciliation. Never
   duplicate entries across sources.
4. Remove the screen-local auto-flush behavior. Queue start is explicit except
   when Codex itself advances its native queue.

#### Verification

```text
go test ./internal/provider/codex -run 'Test(Steer|Queue)'
go test ./internal/session ./internal/protocol ./internal/ws
go test -race ./internal/provider/codex ./internal/session
cd apps/mobile && flutter analyze && flutter test test/codex_queue_test.dart test/chat_screen_test.dart
```

Acceptance: Send and Queue have different deterministic semantics; queue state
survives the promised lifecycle; native/fallback transitions never duplicate
or lose an acknowledged item.

### Phase P9 - Managed filesystem, fuzzy search, and watches

#### Tests first

1. Pin every stable `fs/*` request/response and notification plus simple and
   session fuzzy search fixtures.
2. Test default workspace root, additional admin grants, path normalization,
   symlink escape, root revocation, metadata/list/read bounds, UTF-8/binary
   rejection, atomic write, overwrite preview, copy/remove confirmation, watch
   limits, and reconnect cleanup.
3. Test stable fuzzy search and independently negotiated incremental session
   search, cancellation, out-of-order updates, and result caps.
4. Add mobile explorer/editor/search tests for breadcrumbs, metadata, preview,
   dirty edits, conflict, partial failure, and destructive confirmation.

#### Production steps

1. Add managed filesystem roots to daemon config. The session cwd root is
   granted by default; additional roots require explicit host configuration and
   are reported by label, not leaked as arbitrary paths before authorization.
2. Validate every path before Codex dispatch and validate returned paths again.
   Reject symlink/canonicalization escape and operations outside a current
   grant.
3. Implement stable filesystem methods, atomic write preconditions, and watch
   registry. P9 never exposes `process/*`; the accepted D37 process surface is
   isolated in P7 with its own authority and confirmation contract.
4. Implement fuzzy search with one-shot stable fallback and optional
   incremental session updates.
5. Build Files and Search screens; require preview/confirm for overwrite, copy
   onto an existing target, and remove.

#### Verification

```text
go test ./internal/provider/codex -run 'Test(FS|Root|Path|Watch|Fuzzy)'
go test ./internal/config ./internal/session ./internal/protocol ./internal/ws
go test -race ./internal/provider/codex ./internal/session ./internal/ws
cd apps/mobile && flutter analyze && flutter test test/codex_files_test.dart
```

Acceptance: no operation escapes a managed root; writes are conflict-aware and
atomic; watch resources are bounded and cleaned; experimental search absence
does not remove stable search.

### Phase P10 - Audio attachments and realtime relay

#### Tests first

1. Extend ordinary prompt attachment tests for supported audio media types,
   size rejection, unsupported capability, ordering with text/images, replay
   descriptors, and absence of bytes from history/logs.
2. Pin every approved realtime method/notification and test lifecycle, voices,
   text/speech/audio input, transcripts, output audio, error, stop, reconnect,
   and capability disable.
3. Test phone WebRTC signaling relay authorization, SDP/candidate bounds,
   session ownership, stale generation, disconnect, and proxied WebSocket
   fallback.
4. Add Flutter microphone permission, recording lifecycle, backgrounding,
   playback, transcript, route change, stop, and error widget tests.

#### Production steps

1. Complete ordinary audio attachment support before realtime. Reuse existing
   content blocks and attachment descriptors; add bounded upload/chunk
   handling without storing raw media.
2. Implement realtime session state and signaling in `realtime.go` and typed
   daemon operations. Prefer phone WebRTC with daemon relay; use authenticated
   proxied WebSocket only when WebRTC negotiation fails and capability permits.
3. Never expose the Codex realtime endpoint or auth material to the phone.
4. Build Realtime controls with explicit start/stop and visible connection,
   recording, transcript, playback, and fallback states.
5. In a combined composer flow, acknowledge and send ordinary attachments
   first; only after the attachment-bearing turn is accepted may the client
   start realtime immediately. A realtime failure does not retract or duplicate
   the accepted attachment turn.

#### Verification

```text
go test ./internal/provider/codex -run 'Test(Audio|Realtime|Signal|Voice)'
go test ./internal/protocol ./internal/ws
go test -race ./internal/provider/codex ./internal/ws
cd apps/mobile && flutter analyze && flutter test test/audio_attachment_test.dart test/codex_realtime_test.dart
```

Acceptance: ordinary audio works independently of realtime; realtime state is
session-owned and bounded; no audio/auth bytes enter persistent state; fallback
does not weaken authentication.

### Phase P11 - Configuration, feature flags, and provenance

#### Tests first

1. Pin config read/value write/batch write/requirements and feature list/set
   fixtures for stable, beta, development, enabled, disabled, and managed
   states.
2. Test typed setting validation, advanced key-path validation, user/project
   scope, atomic batches, conflicts, managed denial, secret masking, hot reload,
   engine replacement, and partial failure.
3. Add Flutter tests for typed controls, advanced editor, provenance display,
   restart-required state, destructive/development confirmations, and secret
   masks.

#### Production steps

1. Implement typed config values and provenance. Allow known typed keys through
   dedicated controls and unknown advanced keys through a validated key-path
   editor; forbid keys classified secret from readback.
2. Apply user/project writes atomically where app-server supports batch write.
   A failed batch leaves the prior configuration effective.
3. Expose stable, beta, and development feature lifecycle and managed state.
   Enabling a feature does not imply its methods are in the capability
   snapshot; both conditions are required.
4. Reconcile hot-reload notifications into provider/session state and label
   restart-required changes.
5. Wire `/debug-config` to the layered configuration/provenance view and
   `/experimental` to the complete lifecycle/policy-aware feature catalog;
   update every provider command table and conformance test explicitly.

#### Verification

```text
go test ./internal/provider/codex -run 'Test(Config|Feature|Provenance|Requirement|HotReload)'
go test ./internal/config ./internal/protocol ./internal/ws
go test -race ./internal/provider/codex ./internal/ws
cd apps/mobile && flutter analyze && flutter test test/codex_configuration_test.dart
```

Acceptance: configuration writes are scoped, validated, atomic, and
provenance-visible; managed policy wins; secret keys never round-trip to UI or
logs; feature enablement and method capability remain independent gates.

### Phase P12 - Skills and hooks

#### Tests first

1. Pin skill list/read/root/config and hooks list plus skill/hook lifecycle
   notification fixtures.
2. Test source/path/root/dependency/error/input/preview metadata, enable/disable,
   refresh, extra-root grants, missing dependencies, and engine replacement.
3. Test hook grouping, metadata, trust hash, config mutation, start/completion
   output, prompt fragments, refresh, and changed-on-disk trust invalidation.
4. Add mobile list/detail/filter/enable/disable/trust/output tests and `/skills`
   and `/hooks` command routing.

#### Production steps

1. Implement the full skill catalog and management contract. Install flows are
   delegated to plugins/marketplaces/managed roots; there is no duplicate raw
   installer.
2. Implement hook discovery and atomic enable/disable config writes. Trust is
   tied to content hash; a changed hook returns to untrusted until reviewed.
3. Project hook prompt fragments as bounded content in an explicitly opened,
   authenticated hook detail view. Keep them out of provider-global pushes,
   logs, transcripts, and ordinary persistence; mask values classified secret.
4. Build Skills and Hooks screens and canonical command mappings; update every
   provider table explicitly.

#### Verification

```text
go test ./internal/provider/codex -run 'Test(Skill|Hook|Trust|Refresh)'
go test ./internal/command ./internal/protocol ./internal/ws
go test -race ./internal/provider/codex ./internal/ws
cd apps/mobile && flutter analyze && flutter test test/codex_skills_test.dart test/codex_hooks_test.dart
```

Acceptance: catalogs preserve complete metadata; root grants and hook trust are
enforced; lifecycle output is bounded; changed hook content cannot inherit an
old trust decision.

### Phase P13 - Apps, plugins, marketplaces, and MCP

#### Tests first

1. Pin app list/read/installed/update, plugin list/search/read/install/uninstall/
   share, marketplace add/remove/upgrade, and MCP status/OAuth/reload/resource/
   tool/progress fixtures.
2. Pin every standard MCP form, OpenAI form, and URL elicitation variant,
   including nested schema, secret, browser resume, action, persisted approval,
   cancellation, validation failure, and resolved notification.
   Pin typed `initialize.capabilities.extensions` and the legacy
   `mcpServerOpenaiFormElicitation` fallback independently. Assert Codex
   retains only `openai/form`, `openai/standard-form-input`, and
   `io.modelcontextprotocol/ui`; assert the standard-form-input extension is
   client-only when Codex initializes downstream MCP servers.
3. Test connector and MCP OAuth start/cancel/completion, reconnect, stale flow,
   wrong device/session, browser closure, and secret redaction.
4. Test plugin destructive confirmation, managed denial, local fallback search,
   workspace share targets, partial install/upgrade, MCP exact arguments,
   side-effect classification, destructive confirmation, progress/cancel, and
   widget fallback.
5. Add sandboxed MCP widget security tests: origin allowlist, CSP, no arbitrary
   navigation, no daemon token access, bounded messages, and native-card
   fallback.
6. Add a negative contract test for source-head-only
   `mcpServer/event/stream/start`, `mcpServer/event/stream/stop`, and
   `mcpServer/event/stream/notification`: they exist in the recorded
   `../codex` watch manifest, do not exist in installed 0.149.1, and produce no
   capability, handler, protocol operation, or UI route.

#### Production steps

1. Implement apps/connectors with catalog, installed snapshot, auth state, tool
   cards, and sandboxed widget metadata. Reuse the secret/auth path.
2. Implement marketplaces and plugin lifecycle, including enable/disable in
   config, policy reasons, workspace share, and native search with bounded
   local fallback. Direct production mutation stays disabled while the
   official app-server documentation labels these calls under development,
   unless the Project Owner separately approves that maturity conflict.
3. Implement every stable MCP elicitation form/URL path through generic forms.
   Render persisted dynamic tool-call items through typed cards, but keep
   dynamic-tool registration/callback execution rejected with a typed reason.
   Negotiate supported MCP extensions explicitly before thread creation.
   Advertise only `openai/form` when its renderer is complete,
   `openai/standard-form-input` when the standard input renderer is complete,
   and `io.modelcontextprotocol/ui` with the exact MIME types supported by the
   sandboxed widget. Use the legacy OpenAI-form Boolean only when the installed
   contract lacks the typed map. Treat a thread's profile as immutable across
   turns, direct tool calls, and subagents; use a new initialized connection to
   change it.
4. Implement direct MCP status, OAuth, reload, resources, tool calls, exact
   args, progress, cancel, and side-effect review. No generic untyped proxy is
   exposed.
5. Build Apps, Plugins/Marketplaces, and MCP screens plus `/apps`, `/plugins`,
   and `/mcp` mappings; update all provider tables.
6. Keep the source-head MCP event stream trio watch-only. If a later installed
   manifest gains any member, fail the contract drift gate and amend this pair
   before deciding stream ownership, retention, cancellation, and fallback.

#### Verification

```text
go test ./internal/provider/codex -run 'Test(App|Plugin|Marketplace|MCP|Elicitation|Widget|OAuth)'
go test ./internal/command ./internal/protocol ./internal/ws
go test -race ./internal/provider/codex ./internal/ws
cd apps/mobile && flutter analyze && flutter test test/codex_apps_test.dart test/codex_plugins_test.dart test/codex_mcp_test.dart
```

Acceptance: all stable elicitation forms complete end to end; OAuth can resume
or cancel safely; every side-effecting MCP/plugin operation is classified and
confirmed; widgets cannot cross their sandbox; unsupported dynamic
registration remains explicit.

### Phase P14 - Account lifecycle, usage, workspace messages, and auth

#### Tests first

1. Pin account read, browser/device/API/external login, cancel, switch, logout,
   completion, rates, usage, messages, reset credit, nudge, token refresh, and
   error fixtures actually present in the installed stable schema. Classify
   installed experimental `account/bedrock/discover` and
   `account/bedrock/setup` as deferred under D38; do not mislabel them stable.
2. Test auth methods, secret inputs, browser/device progress, cancellation,
   account switch, logout confirmation, stale flow, engine replacement, token
   refresh, and managed restriction.
3. Test provider-global update fanout, account-id masking, rate-limit reset
   times, workspace-message dedupe, and no transcript persistence.
4. Add Flutter account/status/login/logout/usage/message tests.

#### Production steps

1. Extend existing provider-auth abstractions rather than adding parallel
   credentials. Add secret generic inputs and browser/device/external method
   status to the same catalog.
2. Implement account callbacks and provider-owned token refresh only for
   installed stable forms. Typed-reject unsupported setup variants; do not
   infer source-head project setup support.
3. Add Account screen with plan, masked identity, rates, usage, workspace
   messages, login/switch/cancel/logout, reset-credit, and nudge controls.
4. Require confirmations for logout, account switch that drops active state,
   reset-credit consumption, and email nudge.
5. Wire `/logout` to the confirmed account lifecycle action and update every
   provider command table and conformance test explicitly.

#### Verification

```text
go test ./internal/provider/codex -run 'Test(Account|Login|Logout|Auth|TokenRefresh|Usage|WorkspaceMessage)'
go test ./internal/protocol ./internal/ws
go test -race ./internal/provider/codex ./internal/ws
cd apps/mobile && flutter analyze && flutter test test/codex_account_test.dart test/provider_auth_sheet_test.dart
```

Acceptance: every installed stable account method has a typed adapter or typed
unsupported result; secret/token values are absent from state and logs;
provider-global updates refresh the screen without creating transcript noise.

### Phase P15 - External-agent import and feedback

#### Tests first

1. Pin detect/import/history/progress/completion fixtures for Claude and Cursor,
   home and repository scopes, categories, age/count filters, conflicts, and
   partial results.
2. Test preview-before-write, no candidates, duplicate import, cancel,
   reconnect, partial failure, history record, and connector handoff.
3. Pin feedback tags/reason/thread/log inventory/attachments and upload result;
   test explicit consent, redaction preview, size cap, retry, cleanup, and no
   automatic submission.
4. Add mobile import wizard/history and feedback preview/consent tests plus
   `/import` and `/feedback` routing.

#### Production steps

1. Implement guided detection and preview. The user selects Claude Code or
   Cursor, home or repository scope, age/count, conflict policy, and each
   offered category independently: instructions, config, skills, plugins, MCP,
   subagents, hooks, commands, memory, and sessions.
2. Stream bounded progress/results and record provider import history only
   after app-server reports completion. Show partial success item by item.
3. Implement feedback as an explicit-consent flow. Build a redacted inventory,
   show exactly what will upload, cap attachments/logs, retry only after a known
   failure, and clean temporary artifacts.
4. Add canonical command mappings and update all provider tables.

#### Verification

```text
go test ./internal/provider/codex -run 'Test(Import|ExternalAgent|Feedback|Redact)'
go test ./internal/command ./internal/protocol ./internal/ws
go test -race ./internal/provider/codex ./internal/ws
cd apps/mobile && flutter analyze && flutter test test/codex_import_test.dart test/codex_feedback_test.dart
```

Acceptance: no import mutates before a complete preview; conflicts and partial
failure are deterministic; feedback is never automatic and uploads only the
reviewed redacted inventory.

### Phase P16 - Agent-operated browser and computer-use presentation

#### Tests first

1. Pin current browser/computer-use item variants, screenshot/image content,
   approvals, MCP-backed actions, progress, completion, and error fixtures.
2. Test screenshot bounds, ordered action timeline, safety/permission
   transitions, stale action, model reroute, reconnect, and terminal state.
3. Add Flutter action-card and screenshot tests for zoom, approval, progress,
   fallback, and accessibility. Assert there is no manual input/RDP path.

#### Production steps

1. Extend item/event projection with typed browser/computer-use summaries and
   bounded screenshots using existing image transport.
2. Route approvals through P2 and policy through P5. Route MCP-backed browser
   actions through P13; do not add a second approval or tool-call system.
3. Render an agent-operated action timeline and screenshots. The phone can
   approve/deny/cancel supported actions but cannot inject arbitrary mouse or
   keyboard events.

#### Verification

```text
go test ./internal/provider/codex -run 'Test(Browser|ComputerUse|Screenshot)'
go test ./internal/event ./internal/protocol ./internal/ws
go test -race ./internal/provider/codex ./internal/ws
cd apps/mobile && flutter analyze && flutter test test/codex_computer_use_test.dart
```

Acceptance: browser/computer-use work is understandable and controllable from
the phone; it reuses existing approval/MCP/security paths; no manual remote
desktop capability exists.

### Phase P17 - Contract closure, live acceptance, documentation, and rollout readiness

#### Contract and deferral audit

1. Regenerate stable and experimental schemas from the installed binary and
   compare them with `manifest.json`. Every difference is classified as added,
   removed, or shape-changed and either handled or recorded in an approved
   amendment. Separately decompress/read the precomputed stable and
   experimental exports at
   `../codex/codex-rs/app-server-protocol/schema/precomputed/app-server-exports-stable.json.zst`
   and `app-server-exports-experimental.json.zst` from the pinned commit and
   compare them to the source-watch manifest. Never substitute source output
   for the installed executable manifest.
2. Assert the current expected comparison before implementation: installed is
   95/75/10 stable and 150/75/11 experimental; source is 95/76/10 stable and
   152/76/11 experimental; the source-only set is exactly experimental
   `mcpServer/event/stream/start`, experimental
   `mcpServer/event/stream/stop`, and stable
   `mcpServer/event/stream/notification`. Any other difference blocks P17 and
   requires investigation plus an amendment.
3. Assert every installed stable request in the manifest is one of
   `implemented`, `internal`, or `typed_deferred`; every stable notification is
   classified; every stable server request is handled or typed-rejected.
4. Assert only the approved experimental adjunct ids can be enabled. The
   accepted project/environment/process ids remain individually gated and
   default to their documented fallback when absent or policy-disabled.
5. Add negative tests for the remaining D31 deferrals and D30
   attestation/external-clock/Windows conditions. Keep negative coverage for
   Remote Control, Bedrock setup, memory administration, and production plugin
   mutation under its maturity warning. Deferred commands and screens show a
   capability reason, not a dead control.
6. Search for stale 0.145/0.146/0.147 claims, `ListSessions:false`, generic
   permission acceptance, ordered Codex question answers, raw app-server proxy
   calls, and provider-name UI gating. Update only claims superseded by 0109;
   keep historical fixtures/records labeled as evidence.

#### Live acceptance split

No-model acceptance, safe for ordinary validation:

* exact schema/manifest and initialization;
* all three managed transports and engine replacement, including native bearer
  authentication, health probes, overload handling, and owned
  local-daemon/proxy mode;
* model/profile/feature/thread/section/MCP/app/plugin/skill/hook/config/account
  read catalogs;
* filesystem metadata/list/read in a temporary authorized root;
* project and managed-environment reads; project mutation and environment
  endpoint registration remain fake-only here;
* redacted `doctor` projection;
* login/import/feedback mutation paths only against fakes, never live.

Token-bearing acceptance, run once after all no-model checks pass and only with
explicit awareness that it spends model tokens:

* one ordinary turn with text, image, and supported audio attachment;
* one steer and one explicitly queued turn when native queue is available;
* one method-specific approval and one option-bearing question;
* one sandboxed command, one confirmed unsandboxed shell command in a temporary
  directory, and cleanup;
* one realtime start/stop only if the account and installed contract advertise
  it; and
* one browser/computer-use presentation case only if a deterministic harmless
  action can be constrained.

Live tests never mutate account login, plugins, marketplaces, config, imports,
feedback, or user filesystem outside an `mktemp -d` root.

#### Final verification

```text
git diff --check
make test
make race
make vet
make pre-add-check
cd apps/mobile && dart format --output=none --set-exit-if-changed .
cd apps/mobile && flutter analyze
cd apps/mobile && flutter test
make live-codex-contract
make live-codex
```

Run token-bearing targets only at final acceptance:

```text
make live-codex-turn
make live-codex-review
make live-codex-rich
```

`make live-codex-rich` is added in this phase and carries a dedicated
`live_codex_rich` build tag. It must print the installed binary version/digest,
selected capabilities, temporary root, and cleanup result without printing
secrets or raw payloads.

#### Documentation closure

1. Update `docs/protocol-v1.md` with every additive operation, event, bound,
   retry class, error code, confirmation, and secret rule. Update
   `docs/protocol-v2.md` as well: it is the delta document that specifies the
   negotiated `Caps` block and the client version offer, so the
   `codex_surface` capability block and the additive
   `codex_surface_version` client field belong there, not only in v1.
2. Update `docs/config.md` with transport, roots, reconnect, policy, and feature
   configuration plus secure defaults.
3. Update the provider matrix/README and command help with stable,
   experimental-adjunct, fallback, and deferred states.
4. Record every phase commit, verification result, live binary digest, and
   accepted deviation in the implementation log below.
5. Change no MADR status during implementation except through an explicit
   reviewed decision update. Completion is recorded in the plan log.

Acceptance: all commands pass; the manifest accounts for the complete installed
contract; no-model and approved token-bearing live cases pass; docs and runtime
capabilities agree; the worktree contains no temporary schema, credentials,
audio, import, feedback, or terminal artifacts.

## Verification & Testing Strategy

### Phase acceptance matrix

| Phase | Required observable outcome |
| --- | --- |
| P0 | Accepted MADR, approved plan, reproducible baseline, docs-only commit. |
| P1 | Exact-binary manifest and independent capability latches. |
| P2 | Correct typed callbacks, secret path, exhaustive routing. |
| P3 | Conformant stdio/Unix-WS/WS plus authenticated WSS proxy, native WS auth, health, overload, and owned daemon/proxy mode. |
| P4 | Complete event lifecycle, sanitized runtime state, and ephemeral redacted doctor output. |
| P5 | Dynamic profiles, independent reviewer/unattended axes, managed settings. |
| P6 | Unified native/managed thread, section, and complete capability-gated project lifecycle. |
| P7 | Distinct sandboxed/unsandboxed execution, durable terminals, managed environments, and default-off standalone processes. |
| P8 | Ordinary Send steers; explicit native/fallback queue is manageable. |
| P9 | Root-confined filesystem, watch, and fuzzy search. |
| P10 | Ordinary audio then authenticated realtime relay. |
| P11 | Typed/advanced config, provenance, and feature lifecycle. |
| P12 | Full skill/hook discovery, trust, control, and events. |
| P13 | Apps/plugins/marketplaces and complete stable MCP forms/direct calls. |
| P14 | Stable account lifecycle, usage, messages, and secret-safe auth. |
| P15 | Previewed imports and consented/redacted feedback. |
| P16 | Agent-operated browser/computer-use cards without manual RDP. |
| P17 | Full contract accounting, regression, live acceptance, and docs closure. |

### MADR decision traceability

| Decision | Executing phase(s) | Mechanical confirmation |
| --- | --- | --- |
| D1 | P3, P17 | Transport conformance for managed stdio/Unix-WS/WS and authenticated WSS proxy; negative external-attach/ACP/MCP-server/exec boundary tests. |
| D2 | P1, P17 | Exact P0 post-update execution manifest, sanitized fixtures, digest, and no-model drift target; preserve 0.148.0 as historical and 0.149.0 as provisional evidence. |
| D3 | P2 | Compile-time-separated callback types, exact response fixtures, bounded unknown rejection. |
| D4 | P2 | Upstream ids/options/descriptions/Other/keyed replies and recursive secret-sentinel test. |
| D5 | P2, P4 | Exhaustive 75-notification destination table and sanitized provider projection. |
| D6 | P6 | Unified thread browser with lifecycle, organization, replay, aliases, search, and fallbacks. |
| D7 | P5 | Dynamic profile/reviewer/unattended matrix and exact one-shot Guardian retry. |
| D8 | P4, P13, P14 | Typed runtime/account/model/config/MCP detail views with bounds and secret masks. |
| D9 | P4 | Delta/final upsert tests, readable reasoning, progress, terminal/patch/safety/reroute/verification/resolved states. |
| D10 | P7 | Distinct typed APIs, labels, confirmation, and audit for shell versus sandboxed exec. |
| D11 | P7 | `/ps`/strict `/stop`, detach survival, sequence replay, shutdown termination. |
| D12 | P8 | Steer-on-Send, explicit native/fallback queue CRUD/reorder/start, no screen-local auto-flush. |
| D13 | P10 | Ordinary audio acceptance precedes WebRTC/proxied-WS realtime and is not rolled back by realtime failure. |
| D14 as superseded by D36 | P7, P17 | No arbitrary phone endpoint; host-configured WS-loopback/WSS environment registration, typed status/info/selection/events, and credential-persistence negatives. |
| D15 | P3, P17 | No external attach or Remote Control operations; typed deferred state. |
| D16 | P16 | Agent-operated action/screenshot/approval timeline and negative manual-input/RDP test. |
| D17 | P13 | App catalog/read/installed/auth/tool-card/widget/fallback acceptance. |
| D18 | P13 | Marketplace and plugin browse/search/install/uninstall/enable/share/policy/confirmation tests. |
| D19 | P11 | Typed and advanced config, provenance, atomic writes, feature lifecycle, managed/secret tests. |
| D20 | P12 | Skill catalog/roots/source/dependency/input/preview/manage/refresh/event tests. |
| D21 | P2, P13 | Generic secret-capable forms plus every stable MCP/OpenAI/URL elicitation and resolved cleanup; negative dynamic registration. |
| D22 | P13 | Direct paginated MCP OAuth/reload/resource/tool/widget/progress/cancel and side-effect review. |
| D23 | P15 | Claude/Cursor scope/category/filter/preview/conflict/progress/result/history wizard. |
| D24 | P14 | Full installed stable account/auth/usage/message lifecycle and secret/confirmation tests. |
| D25 | P15 | Explicit consent, classification/tags/reason/thread/log/attachment inventory, redaction/retry/cleanup. |
| D26 as superseded by D37 | P9 | Root-confined explorer/editor/watch/search, preview/confirm, and incremental fallback; standalone process authority remains isolated in P7. |
| D27 | P12 | Workspace grouping/metadata/trust-hash/config/events/output/prompt-fragment/refresh tests. |
| D28 | P6 | Section pagination/create/rename/icon/color/order/membership/pin/delete-preserves-thread tests. |
| D29 | P1 and owning feature phases | One latch and stable fallback per approved experimental adjunct; cross-capability isolation test. |
| D30 | P17 | Typed attestation rejection and negative external-clock/Windows setup advertisements. |
| D31 as superseded by D35 | P4, P17 | Memory events preserved read-only; negative undo/redo/memory-admin/Cloud/TUI/Bedrock tests; source-head-only MCP event streams remain watch-only. |
| D32 | P1, P17 | Exact 0.149.1 manifest, 95/75/10 and 150/75/11 counts, SHA-256, sanitized fixtures, and historical-baseline labels. |
| D33 | P1, P13, P17 | Create/fork-only thread source, typed MCP extensions/fallback, attestation refusal, initially empty notification opt-outs, and independent maturity gates. |
| D34 | P3, P17 | Native bearer auth, health/Origin tests, safe `-32001` retry, owned daemon/proxy lifecycle, automatic stdio fallback, and negative external ownership. |
| D35 | P6, P17 | Complete project CRUD/import/order/assignment/filter/fork/event tests and delete-preserves-threads/files confirmation. |
| D36 | P7, P17 | Host-configured WS-loopback/WSS environment registration/status/info/selection/events and negative arbitrary phone URL/credential persistence. |
| D37 | P7, P17 | Default-off process argv/cwd/root/allowlisted-env/per-spawn-confirmation/I/O/timeout/generation tests and distinct sandbox labels. |
| D38 | P4, P13, P14, P17 | Authenticated-v2 ephemeral `doctor` projection plus negative Remote Control/Bedrock/memory/plugin-maturity/ACP boundaries. |

### Cross-cutting tests that must remain green

* protocol negotiation and v1/v2 compatibility;
* session ownership, handoff, receipt, history, replay, and idempotency tests;
* provider command conformance for fake, Grok, Goose, OpenCode, Kilo, and Codex;
* every event type documented and classified for backpressure;
* JSON persistence load from records predating MADR 0109;
* provider engine replacement and pending-callback cancellation under race;
* recursive redaction tests using unique secret sentinel values;
* Flutter process-death/reconnect with no secret or binary state restored; and
* maximum-frame and pagination tests at one byte/item below, at, and above each
  fixed bound.

### Review evidence required from the implementer

For each phase, attach to the implementation log:

* commit id and exact file list;
* focused test command and result;
* race/pre-add/Flutter result as applicable;
* fixture or screenshot demonstrating the phase's user-visible contract;
* capability ids added or changed;
* any fallback exercised; and
* confirmation that no unapproved scope, secret, generated schema tree, or live
  account mutation entered the commit.

## Rollback & Mitigation Procedures

### Rollout

1. Ship P1-P5 dormant behind `codex_surface_version:1`; existing clients see
   current behavior except the P2 correctness fixes, which are unconditional.
2. Enable read-only Runtime and Threads surfaces first for opted-in v2 clients.
   Observe unknown method counts, provider errors, reconnects, and response
   bounds without logging params.
3. Enable thread organization and managed settings next; keep destructive
   delete and writes behind explicit confirmations.
4. Enable execution/queue/filesystem/realtime domains independently through
   daemon configuration. Defaults: sandboxed execution on, unsandboxed shell
   confirmation required, workspace root only, realtime off until negotiated.
5. Enable configuration/extensions/MCP/account/import/feedback domains
   independently. Stable read surfaces may default on; every write/destructive/
   secret surface retains its confirmation and policy gate.
6. Promote an experimental adjunct only after its no-model contract test passes
   on the installed digest. A failure disables that id for the engine
   generation and activates the documented stable/daemon fallback.
7. After one full release with clean capability/error metrics, remove no
   fallback. Fallback removal requires a later MADR because this decision
   promises compatibility across installed versions.

### Rollback

Rollback is capability-by-capability, not all-or-nothing:

* disable a failing experimental id in the generation snapshot and use its
  stable/daemon fallback;
* set Codex transport back to `stdio` and restart the managed engine; existing
  thread records remain resumable;
* disable one domain in daemon configuration; the phone hides mutations and
  displays a typed managed-unavailable reason while read/history remains;
* stop accepting new queue/terminal/realtime operations, reconcile or terminate
  existing native resources explicitly, then disable the capability;
* revoke additional filesystem roots and close their watches before disabling
  filesystem writes;
* cancel active OAuth/import/feedback/realtime flows and wait for authoritative
  resolution before rolling back their handlers;
* retain additive JSON fields and protocol/event decoders during rollback so
  older binaries do not destroy newer state; and
* revert phase commits in reverse order only when capability disablement cannot
  contain the issue. Do not revert P2's wire-correctness fixes to restore a
  feature.

Data rollback requires no schema migration: all added persistence is additive
JSON. A rolled-back daemon ignores unknown fields. It must not delete provider
native threads, queues, sections, plugins, configuration, or account state as a
rollback side effect.

## Task Checklist

- [x] Accept MADR 0109 and approve this implementation plan (`b944880`).
- [x] Re-verify the Codex 0.148.0 schema counts and baseline package tests.
- [x] Confirm the standalone Codex CLI is current and record resolved 0.149.1
  version/path/digest plus exact schema counts. The updater had already run;
  this assessment did not rerun it.
- [x] Record Project Owner acceptance of D32-D38 and all recommended policy
  resolutions (2026-08-25). This does not by itself execute a phase.
- [x] Complete P0 repository, toolchain, binary-digest, and schema-drift
  reconciliation.
- [x] Complete P1 exact-version contract manifest and capability negotiation.
- [ ] Complete P2 callback correctness, secret forms, and provider routing.
- [ ] Complete P3 managed transports, supervision, and reconnection.
- [ ] Complete P4 event fidelity, runtime status, and diagnostics.
- [ ] Complete P5 permission profiles, reviewer axis, and managed settings.
- [ ] Complete P6 unified managed/native thread browser and sections.
- [ ] Complete P7 sandboxed execution, explicit shell, and terminals.
- [ ] Complete P8 steering and persistent queued turns.
- [ ] Complete P9 managed filesystem, fuzzy search, and watches.
- [ ] Complete P10 audio attachments and realtime relay.
- [ ] Complete P11 configuration, feature flags, and provenance.
- [ ] Complete P12 skills and hooks.
- [ ] Complete P13 apps, plugins, marketplaces, and MCP.
- [ ] Complete P14 account lifecycle, usage, workspace messages, and auth.
- [ ] Complete P15 external-agent import and feedback.
- [ ] Complete P16 agent-operated browser and computer-use presentation.
- [ ] Complete P17 contract closure, live acceptance, documentation, and
  rollout readiness.

## Implementation Log

This table is intentionally empty until execution is explicitly approved.

| Phase | Commit | Verification | Capability/fallback evidence | Notes |
| --- | --- | --- | --- | --- |
| P0 | `b944880` (artifacts accepted/approved); `f2a56a4` (reconciliation and accepted 0.149.1 delta) | `go test ./internal/provider/codex ./internal/protocol ./internal/event ./internal/session ./internal/ws` and the focused 0109 `git diff --check` passed 2026-08-25; Flutter 3.44.6 and Dart 3.12.2 resolved successfully | Resolved `codex-cli 0.149.1`, SHA-256 `73dc5888888f411c1f0fa7b81d866e721dcc86b527ce8e3b2cf4708661e823ba`; regenerated 95/75/10 stable and 150/75/11 experimental schemas; no-model catalogs/probes green | Started at `3ad5533` on `master`; Go 1.26.5. The binary was already updated and `doctor` reports 0.149.1 latest. The temporary schema/TypeScript evidence tree was removed after comparison. D32-D38 were accepted 2026-08-25. Pre-existing process-rule worktree changes remain excluded. |
| P1 | `a89c9ca` | Focused contract/manifest/capability/initialize tests, full Codex package tests, `go test -race ./internal/provider/codex`, `go test ./...`, and `make live-codex-contract` passed 2026-08-25; `make pre-add-check` reported all 15 changed Go files clean (the unreachable vulnerability database was a warning under the repository gate) | Installed manifest reproduces 95/75/10 stable and 150/75/11 experimental with SHA-256 `73dc5888888f411c1f0fa7b81d866e721dcc86b527ce8e3b2cf4708661e823ba`; source watch reproduces 95/76/10 and 152/76/11 with exactly the approved MCP event-stream trio delta; disabling one latch preserves unrelated stable and experimental capabilities | Added 133 independently gated capability entries and 236 sanitized request/notification/callback shape fixtures; initialize sends attestation false, an empty opt-out list, and an empty typed extension profile until its renderer is complete; new/fork stamp `threadSource:mcremote`, resume omits it. The live gate started no model turn, and all temporary schema trees were removed. |
| P2 | Not started | Not run | Not captured | |
| P3 | Not started | Not run | Not captured | |
| P4 | Not started | Not run | Not captured | |
| P5 | Not started | Not run | Not captured | |
| P6 | Not started | Not run | Not captured | |
| P7 | Not started | Not run | Not captured | |
| P8 | Not started | Not run | Not captured | |
| P9 | Not started | Not run | Not captured | |
| P10 | Not started | Not run | Not captured | |
| P11 | Not started | Not run | Not captured | |
| P12 | Not started | Not run | Not captured | |
| P13 | Not started | Not run | Not captured | |
| P14 | Not started | Not run | Not captured | |
| P15 | Not started | Not run | Not captured | |
| P16 | Not started | Not run | Not captured | |
| P17 | Not started | Not run | Not captured | |

## Amendment Log

### 2026-08-21 — evidence re-verification and wire-shape corrections

A pre-execution audit re-checked this plan and MADR 0109 against the installed
`codex-cli 0.148.0` schema and the working tree. The evidence held: schema
counts reproduced exactly, every stable method named here is stable, every
approved experimental adjunct is experimental-only, and the baseline packages
are green. No phase, decision, scope boundary, or acceptance criterion changed.
Six corrections were applied:

| # | Correction | Why it mattered |
| --- | --- | --- |
| 1 | P2 test 1 now pins `isSecret`, not `secret`, and names the required `ToolRequestUserInputParams` envelope (`threadId,turnId,itemId,isBlocking,questions`). | The installed schema's field is `isSecret`. A fixture written to the old spelling would have passed against itself and failed against Codex — the exact class of defect P2 exists to eliminate. |
| 2 | P2 step 3 reuses `event.QuestionItem.Custom` for `isOther` instead of adding `allow_other`, and moves `description` onto `event.PermissionOption`. | `Custom` is already the cross-provider free-text flag populated by Kilo and OpenCode. A second field for the same concept would have forked question semantics per provider. Upstream `description` is an option field, not a question field. |
| 3 | P2 test 5 pins `PermissionGrantScope` (`turn \| session`) and the optional nullable `strictAutoReview`. | `strictAutoReview` is the per-grant expression of the D7 reviewer axis. It was reachable only through the Confirmation section's strict-review clause, so P2 could have shipped a subset grant that silently dropped it. |
| 4 | The `codex_surface_version:1` client advertisement now names a concrete additive `AuthPayload`/`pair.claim` field with an old-client parse test. | No client capability channel exists: `AuthPayload` carries only `token`, `protocols`, `resume`, and `resume_window_ms`, and `Caps` is server-to-client. The gate the whole rollout depends on had no defined wire mechanism. |
| 5 | Documentation closure now updates `docs/protocol-v2.md` alongside `protocol-v1.md`. | v2 is the delta document that specifies the negotiated `Caps` block and the client version offer. Documenting `codex_surface` only in v1 would have left the v2 contract incomplete. |
| 6 | The evidence baseline records `b944880`, the later `ff92858`, and the in-flight docs-only cross-reference change; the P0 row and steps 1 and 5 reflect that acceptance already happened. | P0 instructed an already-completed status change and told the implementer not to absorb unrelated worktree changes while the log still read "Not started" against a tree that had moved. |

MADR 0109 received a paired erratum on the same date covering the
`strictAutoReview`/`PermissionGrantScope` detail, the `5e3a6fe4ee` provenance
wording, and the removal of stale `proposed` language.

### 2026-08-24 — Codex 0.149.0 execution baseline

P0 found the installed binary at 0.149.0 rather than the 0.148.0 research
baseline. The paired MADR amendment records the binary/source digests and schema
count drift. Those 0.149.0 counts are provisional evidence. After the
owner-directed `codex update`, P1 captures `testdata/<resolved-version>` with
the exact post-update stable and experimental counts. No D1-D31 decision or
feature boundary changed.

All required stable methods and approved experimental adjuncts remain present,
but P1 must classify the complete new inventories before enabling any added
surface. P0 remains incomplete because this sandbox denied the WS baseline's
loopback listener and Flutter's mise-managed cache writes. Fresh owner approval
of this amendment and an execution environment with those permissions are
required before the P0 commit and P1.

The Project Owner subsequently directed P0 to upgrade to the latest installed
Codex CLI first. Local 0.149.0 help confirms the supported command is
`codex update`, not `codex upgrade`. The 0.149.0 counts above are now
provisional. P0 records pre/post version, path, digest, updater result, fresh
schema counts, and required-method presence; P1 uses only the post-update exact
contract. This requires network access and write access beneath
`/home/mac/.codex/packages/standalone` in addition to the existing loopback and
Flutter-cache requirements.

### 2026-08-24 — Codex 0.149.1 resolved baseline and proposed parity delta

The active standalone install now resolves to 0.149.1 with SHA-256
`73dc5888888f411c1f0fa7b81d866e721dcc86b527ce8e3b2cf4708661e823ba`.
`codex doctor --json` reports that 0.149.1 is the latest version. Fresh schemas
reproduce 95/75/10 stable and 150/75/11 experimental, so P1's concrete fixture
directory is `internal/provider/codex/testdata/0.149.1/`.

The paired MADR classifies the drift and proposes D32-D38 for thread-source and
MCP-extension negotiation, native WebSocket authentication/health/overload,
owned daemon/proxy transport, projects, managed environments, standalone
processes, and redacted doctor diagnostics. This plan allocates those additions
to existing phases but marks them conditional. They are not executable until
the Project Owner explicitly approves D32-D38. Existing D1-D31 execution scope
and the P2 correctness gate remain unchanged.

This paragraph records the proposal state on 2026-08-24. The owner resolution
below supersedes its approval condition without rewriting the historical
sequence.

### 2026-08-25 — code/source grounding and owner acceptance

This revision re-read the implementation at mcremote commit
`3ad5533ceeb3c15568092bb026e9569e51ddde86` and the clean sibling Codex source
at `6143217c6730e147f4a1a5a3405d10f580fe9244`, then re-drove installed
`codex-cli 0.149.1`.

The plan now distinguishes three evidence classes mechanically: the installed
0.149.1 manifest is executable authority, source types/handlers/tests define
behavioral detail, and source-only methods remain a non-operative watch list.
It records the exact source-only MCP event-stream delta and preserves ACP as a
negative contract.

The implementation design now has one operation registry, a fixed request
evaluation order, file/phase ownership, and exact mappings for initialization,
MCP extension lifetime, transport/daemon leases, projects, environments,
standalone processes, and doctor JSON. It also corrects two easy abstraction
errors revealed by source: environment selection is injected through
`turn/start.environments`, not sent to a fictional selection method, and the
existing envelope id is the operation identity, so retries must not mint a
second payload id. Phase steps now specify native idempotency, reconciliation,
confirmation, bounds, cleanup, and fallback well enough for table-driven tests.

The Project Owner then accepted D32-D38 and every recommended resolution:
stdio remains default; owned Unix/loopback WS and owned daemon proxy are in
scope; lost daemon ownership falls back to stdio; environment `ws://` is
loopback-only and remote endpoints require `wss://`; standalone processes are
default-off with per-spawn confirmation and a host environment allowlist; the
full project lifecycle is in scope; the redacted doctor view is available to
authenticated v2 devices; and initialization begins with no notification
opt-outs and attestation disabled. This makes the written phase work operative
but does not resume or advance P0, start P1, or authorize product mutation
without a separate phase instruction.

## Review Checklist

Approve this plan only if every answer is yes:

* Does the amendment preserve D1-D31 except for the narrow D14/D26/D31
  deferrals explicitly superseded by accepted D35-D37?
* Does the plan record all eight owner-selected policies and keep individual
  capability gates and separate phase authorization intact?
* Do P1 and P2 block all feature expansion until exact contract evidence and
  callback/routing correctness are green?
* Are stdio, Unix-socket WS, WS, and daemon-terminated WSS all managed without
  adding external endpoint attachment?
* Are stable features implemented by default and experimental adjuncts gated
  independently with explicit fallbacks?
* Are secret, root, output, pagination, persistence, idempotency, confirmation,
  and reconnect rules concrete enough to test mechanically?
* Does every approved feature have provider, daemon, protocol, mobile, test,
  documentation, rollout, and rollback work assigned to a phase?
* Are all deferred items explicit and protected by negative tests?
* Does each phase end with focused verification, pre-add/race/Flutter checks,
  a narrow commit, and implementation-log evidence?
* Is live testing split so ordinary contract validation cannot spend tokens or
  mutate real account/config/plugin/import/feedback state?
* Is publishing excluded until the user asks separately?

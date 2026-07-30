# Codex CLI provider: implementation plan

**Status:** Proposed for review  
**Date:** 2026-07-26  
**Decision:** [MADR 0028](./0028-MADR-codex-provider.md)  
**Verified target:** `codex-cli 0.145.0` on this host, using `codex app-server`
over stdio. Re-probe the tagged live suite and regenerate the compact fixture
set before accepting another CLI version.

## 1. Goal, scope, and non-goals

Add Codex as a daemon provider (`provider.IDCodex == "codex"`) that lets a
paired phone create or resume a durable Codex thread, stream a turn, cancel it,
answer supported approval requests, and later use the common model, command,
compact, fork, and session-management surfaces.

The provider is a native client of `codex app-server`. It is not ACP and is not
an OpenCode HTTP dialect. Version 1 uses exactly one daemon-owned,
long-lived `codex app-server --listen stdio://` process; many mcremote sessions
are Codex threads multiplexed over that one JSON-RPC connection.

Out of scope for this plan:

- `codex exec`, TUI/PTY automation, third-party ACP shims, `remoteControl/*`,
  cloud environments, and a non-loopback app-server listener;
- managing a user's OpenAI credentials, writing `~/.codex/config.toml`, or
  treating a missing login as a daemon configuration failure;
- app-server filesystem, plugin, marketplace, dynamic-tool, and process APIs;
- importing arbitrary historic Codex threads into the mcremote session store;
- claiming a phone-visible nested-session product before its required Codex
  relationship data has been verified; and
- a generic JSON-RPC or engine-supervisor rewrite. The only new shared code
  permitted during this work is a small, demonstrably reusable primitive.

## 2. Repository-grounded assessment

### 2.1 What exists today

| Area | Fact | Planning consequence |
|---|---|---|
| Provider inventory | `internal/provider/provider.go` declares fake, Grok, OpenCode, and Goose IDs only. No `internal/provider/codex/` package exists. | The work starts with a new adapter, an ID, config, registration, and tests; it is not a partial feature toggle. |
| Daemon wiring | `internal/daemon/daemon.go` constructs/registers providers, starts optional shared engines, warns on a missing binary, and calls optional `Shutdown()` on all registered providers. | Codex follows this lifecycle seam and needs no new daemon service abstraction. |
| Session control plane | `internal/session.Manager` already owns local IDs, authorization, persistence, history, event sequencing, `Cancel`, `Close`, `Purge`, `Fork`, `Compact`, model, command, permission, and question dispatch. | Keep Codex state inside the adapter. Do not bypass the manager or add a provider-specific WebSocket path. |
| Event vocabulary | `internal/event/event.go` already represents assistant/reasoning chunks, tool lifecycle, permissions, questions, plans, usage, title, mode, status, and turn completion. | The MVP needs event mapping, not a phone protocol redesign. |
| Shared-engine precedents | `httpagent.Provider` (OpenCode) and `acphttp.Provider` (Goose) already supervise a child process, process group, death signal, restart generation, session map, prewarm, and shutdown. | Reuse their lifecycle invariants and `procutil`, but do not reuse their HTTP, SSE, WebSocket, or ACP framers. |
| Mobile provider UI | Flutter obtains `providers.list`, `models.list`, and canonical commands dynamically; the new-session UI is not an enum of provider IDs. | A ready Codex provider automatically appears. MVP needs no provider-specific Flutter screen. |
| Config surfaces | `ProvidersConfig`, `Defaults`, Viper defaults, `Validate`, `configs/config*.yaml`, service defaults, and `docs/config.md` enumerate each provider separately. | Every new Codex key must be added and tested at all of these locations; adding only a struct would silently break environment overrides. |
| Existing tree implementation | OpenCode's `childAliases` route child SSE events into the *parent* local session. `session.Meta` and the wire session metadata have no parent/local-child field. | MADR 0028's “first-class nested mcremote sessions” is not implementable by copying OpenCode tree demux; it requires a new product contract and verified Codex child IDs. |

### 2.2 Relevant decision records and their effect

| Record | Binding consequence for Codex |
|---|---|
| [0028](./0028-MADR-codex-provider.md) | Native app-server JSON-RPC, shared stdio engine, host-config inheritance, binary-only readiness, and no vendor remote-control dependency are the primary decisions. |
| [0004](./MADR-phase2-grok-acp.md), [0025](./0025-MADR-goose-provider.md) | ACP patterns are useful for permission/session semantics only. Codex must not be forced through ACP because its wire protocol and item model are different. |
| [0019](./0019-MADR-opencode-process-management-plan.md) | Apply the shared-engine process guarantees: process group, death signal, lazy restart, prewarm, and graceful shutdown. Do not copy the HTTP health-check or orphan-port machinery, which is specific to `opencode serve`. |
| [0020](./0020-MADR-opencode-session-tree.md) | Reuse tree-idle, cancellation, and child-lifecycle lessons only after the Codex relationship contract is live-proven. The current alias design is not nested local sessions. |
| [0023](./0023-MADR-canonical-slash-commands.md) | A command is advertised only after a provider operation is implemented and wire-tested; Codex TUI help text is not evidence of an app-server command. |
| [0024](./0024-MADR-stream-coalescing.md) and [0029](./0029-MADR-provider-platform-canonicalization.md) | Preserve first-chunk, control-event, and back-pressure guarantees. Reuse `chunkbuf`/event policy where it fits instead of inventing a Codex-only buffering rule. |
| [0031](./0031-MADR-opencode-catalog-and-metadata-parity.md) | Dynamic provider/model UI and optional session metadata interfaces are already the platform path. Do not add a separate Codex picker protocol or uncontrolled historic-thread discovery. |

### 2.3 Verified Codex contract and corrections

The checked-in spike artifacts and generated schemas are the source of truth
for the pinned CLI. They show that the core path works without
`experimentalApi`: `initialize`, `thread/start`, `thread/resume`, `turn/start`,
`turn/interrupt`, `thread/unsubscribe`, `model/list`, `thread/fork`, and
`thread/compact/start`. `turn/steer` requires `expectedTurnId` and cannot be
sent before `turn/started`. The daemon must demultiplex every notification by
`threadId` and `turnId`; two starts may be accepted but are serialized by
Codex, not parallel turns.

The schema also corrects one material assumption in MADR 0028: pinned
`ThreadListParams` contains `sourceKinds`, `archived`, `cursor`, `cwd`,
`limit`, `modelProviders`, `useStateDbOnly`, `searchTerm`, `sortDirection`, and
`sortKey`; it contains **no** `parentThreadId` or `ancestorThreadId` fields.
`thread/turns/list`, not ordinary `thread/list`, is the currently recorded
experimental-gated method. Therefore the implementation must not enable the
experimental API or build nested-session discovery on assumed parent filters.

The current `codex --version` command emits a PATH-alias warning on this host
before returning `codex-cli 0.145.0`. Version/auth diagnostics must capture
bounded stderr as diagnostics and must not make `Ready()` false merely because
of that warning.

Approval server requests are schema-backed but not live-proven here: the host
cannot create the user namespaces required by bubblewrap. Fixture coverage is
mandatory; the live approval acceptance test is conditional on a working
sandbox host. This is an environment gap, not permission to auto-approve.

## 3. Architecture and invariants

### 3.1 Provider boundary

Create `internal/provider/codex/` with these responsibilities:

```text
provider.go     Provider construction, Ready, engine lifecycle, model catalog
conn.go         newline-JSON-RPC framing, request correlation, server replies
session.go      Start/resume, prompt/steer queue, cancel, close, purge
events.go       thread/turn/item notifications to daemon event.Event
approvals.go    server approval/question request parsing and replies
config.go       adapter-level validated config and defaults
commandtable.go observed canonical command table only
fixtures_test.go recorded frame fixtures and schema-shape regression tests
live_test.go    //go:build live_codex acceptance checks
```

This package may use `internal/procutil`, `internal/chunkbuf`, `event`,
`picker`, `command`, and the existing provider interfaces. It must own its
JSON-RPC dialect. Do not add a new external Go dependency for JSON-RPC: the
wire is newline-delimited JSON and the adapter needs direct handling of
server-initiated requests.

### 3.2 Non-negotiable behavior

1. `Ready()` performs only `exec.LookPath`. It never runs a network/auth probe
   and never reports false for a logged-out-but-installed CLI.
2. Empty `approval_policy` and `sandbox_mode` mean omit their RPC fields. That
   preserves Codex's own `~/.codex/config.toml` and trusted-project behavior.
3. Stdio is private to the daemon. There is no TCP/WebSocket listener,
   attachment, or detach mode in this delivery.
4. There is one serialized JSON writer and a request-id map. A reader goroutine
   alone routes responses, notifications, and server requests. A malformed or
   oversized frame fails the engine and disconnects its live sessions rather
   than silently corrupting a conversation.
5. Control events are never lost. Pending assistant/reasoning chunks flush
   before tool, permission, error, status, or turn-complete events.
6. A failed engine invalidates every session belonging to its generation,
   resolves/cancels pending permissions and questions, emits a disconnected
   status, and allows lazy creation of a later engine. No stale reader may
   mutate a replacement engine/session.
7. Cancel sends `turn/interrupt` for the active turn ID. A prompt during an
   active turn waits for `turn/started` and uses `turn/steer` with
   `expectedTurnId`; if steering is unavailable, the adapter queues a later
   `turn/start`. It never sends a second turn as purported parallel work.
8. Close is soft (`thread/unsubscribe`); hard deletion is only
   `PurgeSession.Purge` → `thread/delete` after the manager's authorization
   and persistence deletion path. Resume always uses the stored native thread
   ID.

## 4. Phased delivery plan

### Phase 0 — reconcile and pin the contract

**Purpose:** turn the accepted MADR into an executable, version-bounded
contract before adding daemon code.

1. Update MADR 0028 to link this plan and replace the unsupported
   `thread/list` parent/ancestor-filter claim with the schema fact above.
   Keep the desired nested-session product decision, but label its discovery
   transport as unproven.
2. Create a compact fixture directory under
   `internal/provider/codex/testdata/` from the existing 0.145.0 spike. Include
   only the minimal request/response/notification frames needed by the adapter:
   initialize, start/resume, start/complete/interrupt/steer, assistant and
   reasoning deltas, command/file items, usage, unsubscribe/delete, and each
   server approval/question shape. Do not check in a generated 273-file schema
   dump or real prompts, paths, credentials, or rollouts.
3. Add a `live_codex` test harness that starts the configured `codex`
   executable over stdio, records its parsed version, sends `initialize` then
   `initialized`, and asserts the stable core methods. It must have a bounded
   context, consume stderr, and skip with a clear message when the CLI or auth
   is unavailable. It is an acceptance test, not a normal `go test` input.
4. Add a separate live assertion that `thread/turns/list` is rejected without
   `experimentalApi` and accepted only when that capability is deliberately
   enabled. Do not add experimental mode to the MVP connection as a result.
5. Add a sandbox preflight to the live approval test. If bubblewrap/user
   namespaces are unavailable, skip only the live approval scenario with the
   detected reason; fixture tests still run and remain required. On a working
   Linux host, require a command or file-change approval request and exercise
   both deny and accept responses.

**Exit gate:** every MVP wire field is backed by a fixture or a live test;
MADR 0028 has no claim that the installed schema disproves.

### Phase 1 — configuration, registration, and engine foundation

**Purpose:** make Codex selectable and safely bootable without yet accepting a
user turn.

1. Add `IDCodex` in `internal/provider/provider.go` and add a dedicated
   `CodexProviderConfig` to `internal/config/config.go`. Do not embed
   `ACPProviderConfig`; MCP advertisement, filesystem callbacks, and ACP auth
   options do not apply to app-server.
2. Use the following initial config shape, matching MADR 0028 and existing
   provider conventions:

   ```yaml
   providers:
     codex:
       enabled: true
       bin: "codex"
       default_cwd: ""
       model: ""
       always_approve: false
       permission_timeout_seconds: 900
       prewarm: false
       turn_stall_notice_seconds: 0
       stream_coalesce_ms: 80
       approval_policy: "" # empty: inherit Codex config
       sandbox_mode: ""    # empty: inherit Codex config
   ```

   Validate non-negative durations, coalescing `0..1000`, approval policy
   (`untrusted`, `on-request`, `never`), and sandbox mode (`read-only`,
   `workspace-write`, `danger-full-access`). Keep granular policies and
   `approvalsReviewer` out of config until a product requirement exists.
3. Add Viper defaults for every scalar key, defaults/validation tests including
   environment overrides, and documented example blocks in all three config
   samples, service setup defaults, `docs/config.md`, and the README. An empty
   override must be visibly documented as inheritance, never as `never`.
4. Add the daemon conversion function and registration beside Goose/OpenCode.
   It logs a missing binary, calls `EnsureServer` only when `prewarm` is true,
   includes Codex in the all-providers-unready check, and relies on the existing
   generic shutdown loop.
5. Implement engine startup using `exec.CommandContext` with
   `app-server --listen stdio://`, `procutil.SetProcessGroup`, and
   `procutil.SetDeathSignal`. Give stdin/stdout/stderr distinct bounded paths;
   do not merge stderr into the JSON stream. Publish the engine only after a
   successful `initialize`/`initialized` handshake.
6. Implement `ensureEngine`, prewarm, shutdown, and one owner `Wait` goroutine.
   Concurrent starts wait on the same startup attempt. Engine exit atomically
   clears the current generation, fails pending RPCs, and tells every mapped
   session that it disconnected.
7. Implement a newline JSON framer with monotonic request IDs, context-aware
   response waiters, a serialized writer, frame-size limits, protocol-error
   decoding, and server-request responses that preserve the request ID.
   `initialize` sends stable `clientInfo` (`mcremote`) and
   `experimentalApi: false`.
8. Run a bounded best-effort `codex login status` only at engine boot (or an
   explicit diagnostics call), logging a warning when it fails. It does not
   alter `Ready()` or block `thread/start`; stderr warnings such as the current
   PATH-alias message are retained only in bounded diagnostics.

**Tests:** config default/validation/environment tests; daemon registration
tests; fake `exec`/pipe framer tests for concurrent requests, server requests,
bad JSON, EOF, cancellation, restart generation, and prewarm/shutdown races.

**Exit gate:** an enabled installed Codex provider appears in `providers.list`,
an absent binary appears as not ready, and startup/exit cannot leak a child
process or leave an RPC waiter blocked.

### Phase 2 — MVP thread and turn lifecycle

**Purpose:** deliver useful phone chat with durable resume and correct cancel
semantics.

1. Implement `Provider.Start`:
   - resolve the configured/default/session CWD using the same concrete-CWD
     convention as other providers;
   - use `thread/resume` for `StartOptions.AgentSessionID`, otherwise
     `thread/start`;
   - send `model` only when a session or provider model was explicitly set;
   - send `sandbox`/`approvalPolicy` only for validated non-empty operator
     overrides; and
   - return a session whose native ID is the Codex `thread.id` and which
     implements `CWDSession`.
2. Maintain an engine session map keyed by native thread ID and a session-owned
   active-turn state machine. Register the mapping before any turn can emit;
   remove it only after unsubscribe/delete or engine death.
3. Implement text `Prompt` as `turn/start`. Store the returned turn ID
   immediately so a near-immediate cancel targets the right turn; latch
   steerability only on `turn/started`. Queue follow-up input while the start
   race is unresolved; issue `turn/steer` after the latch or dequeue it after
   completion. Bound the queue and return `provider.ErrTurnBusy` when its
   documented capacity is exceeded.
4. Implement `Cancel` with `turn/interrupt`; classify the known idle/not-active
   response as a harmless already-finished race only after the local terminal
   event is observed. Do not falsely emit turn completion from a failed
   interrupt.
5. Map the wire-confirmed MVP notifications:
   - `item/agentMessage/delta` → `TypeAssistantChunk`;
   - reasoning deltas/items → `TypeThoughtChunk`;
   - `item/started`, command/file/MCP/web-search items and output deltas →
     `TypeToolCall`/`TypeToolUpdate` with stable `itemId` keys;
   - `thread/status/changed`, `turn/started`, and `turn/completed` → status
     plus exactly one `TypeTurnComplete` per turn;
   - `thread/tokenUsage/updated` → `TypeUsage`; and
   - transport/server errors → classified `TypeError` and disconnected status
     when the engine is no longer usable.
6. Avoid duplicate final assistant text when deltas were already emitted.
   Pass assistant and reasoning chunks through the established coalescing path
   using `stream_coalesce_ms`; flush before every control event and leave zero
   as an explicit no-coalescing setting.
7. Implement `Close` as idempotent `thread/unsubscribe`, and `Purge` as
   idempotent `thread/delete`. Preserve the native ID in `session.Meta` so a
   closed mcremote session can later resume the same Codex thread.

**Tests:** table-driven event fixtures; exact start/resume/turn/interrupt JSON;
turn-start/turn-started race; steer ordering and queue overflow; cancel after
response/before notification; duplicate delta/final suppression; close/purge;
engine death fan-out; manager persistence/resume; protocol create/prompt/cancel
round trips. Add race tests around event demux and shutdown.

**Live acceptance:** `go test -tags live_codex ./internal/provider/codex/...`
must create a low-cost thread, receive streamed text, cancel a deliberately
long turn, close it, and resume it. Run it once at acceptance, not in a loop.

### Phase 3 — common control-plane fidelity

**Purpose:** add only capabilities that have existing mcremote interfaces and
proven Codex app-server methods.

1. Implement `ModelCatalog.ListModels` from live `model/list`. Preserve the
   configured provider model as the catalog default, as the existing OpenCode
   path does. Cache a successful catalog per engine generation; never make a
   model-picker request block indefinitely on a cold or dead engine.
2. Implement `ModelSession.SetModel` as a session-owned model override used by
   later `turn/start` calls. Reject or queue a model change during an active
   turn; do not pretend Codex changed the already-started turn.
3. Implement `RenameSession` with `thread/name/set`, `ForkSession` with
   `thread/fork`, and `CompactSession` with `thread/compact/start`. Each must
   use the existing manager authorization and persistence behavior. A fork
   returns a new native thread to the existing manager fork flow; a compact
   completes through normal item/turn events rather than a synthetic finish.
4. Implement command/file approval server requests as separate response
   builders, not one shared untyped `decision` object. Emit actionable
   `TypePermission` options for accept once, accept for session where valid,
   decline, and cancel. `always_approve` auto-replies only with a schema-valid
   accept decision. Resolve timeout, close, and engine-death requests with a
   safe cancellation/decline response when a response can still be sent.
5. Add `item/tool/requestUserInput` only after a captured frame proves its
   question fields and response shape. Map it to the existing `QuestionSession`
   interface; reject unsupported dynamic tools, MCP elicitations, token refresh,
   and attestation requests safely with their method-specific response rather
   than leaving Codex blocked.
6. Implement canonical commands incrementally. `/help`, `/clear`, `/new`, and
   `/sessions` are daemon commands already. Advertise `/model`, `/compact`,
   `/status`, `/goal`, `/plan`, `/permissions`, `/diff`, or `/undo` only after
   the exact app-server read/write method, session semantics, and a live-tagged
   test are known. In particular, do not expose a writable Plan mode merely
   because `collaborationMode/list` exists; a verified set/write path is still
   required.
7. Add image input only after bounding decoded and data-URL sizes at the daemon
   boundary and verifying `UserInput.image` against a live app-server. Audio,
   skills, mentions, realtime, and host dynamic tools remain later work.

**Tests:** catalog merge/default tests; per-method approval and timeout
fixtures; question validation; unsupported server-request replies; rename/fork/
compact/model manager tests; command conformance; redaction/size-limit tests;
and live model, compact, fork, rename, and approval tests where the host
supports them.

**Exit gate:** every advertised capability has a manager operation, a fixture,
and a version-pinned live check. A provider never leaves a Codex server request
unanswered merely because the phone cannot render that request type.

### Phase 4 — nested-agent decision gate, then product work

**Purpose:** resolve the largest remaining product gap without building a UI on
fictional app-server fields.

#### 4A. Required investigation

1. On the pinned CLI, capture a real collaboration/subagent turn and record the
   exact `collabAgentToolCall`, `subAgentActivity`, and `thread/started` payloads
   that identify a child thread, if any. Capture whether `thread/resume` is
   necessary to receive a child's stream on the shared connection.
2. Call `thread/list` with the actual schema-supported filters only and prove
   whether it can ever supply parent linkage indirectly. Do not send invented
   parent/ancestor fields. Separately record every method that requires
   `experimentalApi`; do not enable it globally.
3. Verify cancellation, terminal state, permission routing, and unsubscribe
   behavior for parent and child with two active threads. The test must prove
   that a child cannot keep a parent turn visibly running forever after it has
   completed.
4. Update MADR 0028 with the observed relationship contract and add a tagged
   live regression test. If Codex exposes no safe child relationship, record
   that fact and retain child activity as a tool card rather than inventing a
   session tree.

#### 4B. Conditional implementation

Only if 4A exposes stable child thread IDs and ownership:

1. Write a small follow-up MADR/protocol plan for `session.Meta.ParentID`,
   persistence/migration, authorization inheritance, list ordering, resume,
   delete semantics, and Flutter tree rendering. Current `Meta`, protocol
   payloads, and the mobile sessions list have no parent field, so this is a
   cross-layer change rather than a provider-local enhancement.
2. Subscribe immediately to a newly observed **active direct child** so its
   activity, error, and approval can reach the owner. Persist the relation but
   do not enumerate or subscribe historical descendants at daemon boot.
   Once terminal, drain ordered terminal events and unsubscribe; resume/open
   historical children lazily when the user expands or opens them. This hybrid
   policy supplies live safety feedback without unbounded background streams.
3. Treat child permissions as owned by the parent session owner, preserve a
   child-specific native ID in the event/session metadata, and cascade cancel
   only when the observed Codex relationship semantics prove it is safe.
4. If 4A instead supports only parent-event items, implement a bounded
   synthetic child tool-card lifecycle comparable to OpenCode's current
   `childAliases`; explicitly defer first-class nested sessions.

**Exit gate:** no nested-session code lands until its live proof, durable data
model, authorization model, and mobile rendering contract are reviewed
together.

### Phase 5 — documentation, rollout, and long-term maintenance

1. Update MADR 0028 implementation status after each accepted phase; update
   `docs/protocol-v1.md`, `docs/config.md`, README configuration guidance, and
   the provider/support matrix only for shipped behavior.
2. Document operator prerequisites: installed `codex`, host-owned `codex
   login`, private stdio engine, and Linux bubblewrap/user-namespace conditions
   for sandboxed tool approvals. Do not instruct operators to disable sandbox
   merely to make tests pass.
3. Add release notes describing binary-only readiness: an installed but
   unauthenticated CLI is selectable yet will report a normal session error on
   its first turn. Make the daemon warning actionable but redact paths,
   credentials, prompts, and tool output.
4. Before staging any Go change, run `make pre-add-check` for every changed Go
   file. For the release candidate run `go test ./...`, `go test -race ./...`,
   `flutter analyze`, `flutter test`, and Dart format verification. Run the
   `live_codex` suite once on an authenticated host; run the live approval
   scenario on a working-sandbox host and record its version/result in MADR
   0028.
5. Upgrade procedure: regenerate the Codex schema in a temporary directory,
   diff only the methods/fields used by this provider, update curated fixtures,
   run unit/race/live tests, and update the pinned CLI version in the MADR.
   A CLI upgrade is not accepted on the basis of a successful text prompt
   alone.

## 5. Delivery order and review checkpoints

| Checkpoint | Deliverable | Review question |
|---|---|---|
| A | Phase 0 fixtures, live harness, and corrected MADR facts | Are all promised wire fields real for the pinned CLI? |
| B | Phase 1 selectable/supervised provider | Can the daemon safely start, share, restart, and stop the engine without leaking state? |
| C | Phase 2 chat, cancel, close, resume | Is the ordinary phone conversation reliable under turn races, reconnects, and engine death? |
| D | Phase 3 models, permissions, optional operations | Does each UI affordance correspond to a proven server method and safe fallback? |
| E | Phase 4 investigation result | Is a nested-session product actually supportable, or should activity remain a tool-card presentation? |
| F | Phase 5 release evidence | Has the pinned-version, sandbox, documentation, race, and mobile acceptance evidence been recorded? |

The phases are intentionally serial through Phase 2: transport correctness and
turn state are prerequisites for permissions, commands, and any child-agent
work. Phase 4 is conditional and must not delay a useful, secure Codex MVP.

## 6. Mandatory unit-test and guard matrix

No phase is complete merely because a happy-path live turn works. The following
tests and guards are release requirements; they turn the plan's safety rules
into executable regressions.

| Surface | Deterministic unit tests | Required guard / invariant |
|---|---|---|
| Config | defaults, YAML decode, every environment override, invalid enum/duration, empty inheritance, and config copy isolation | Reject unknown sandbox/approval values before spawning Codex; empty override omits the wire field. |
| Process lifecycle | simultaneous `ensureEngine`, startup failure, EOF, non-zero exit, restart generation, prewarm, shutdown during boot, and no orphaned waiter | One owner calls `Wait`; no old reader/engine may emit into a new generation; process group is terminated on failed startup and shutdown. |
| JSON-RPC | request/response correlation, request-ID uniqueness, write serialization, context cancellation, malformed/oversized frame, unknown notification, and server request response-ID echo | Bound input size and pending-request count; never write concurrent/interleaved JSON; fail closed on framing corruption. |
| Thread lifecycle | start vs resume body, native-ID persistence, close/unsubscribe, purge/delete, duplicate close, and engine-death cleanup | A provider session is registered before it can receive events and is removed exactly once; soft close never deletes a resumable rollout. |
| Turn state | start-result/start-notification race, prompt queue ordering/cap, steer prerequisites, cancel before/after start notification, terminal duplication, and concurrent thread demux | At most one active/steerable turn per local session; a second user prompt never becomes an assumed parallel Codex turn; each turn emits one terminal event. |
| Event translation | every recorded item/delta/status/usage fixture, tool start/update ordering, coalescer flush before control, duplicate final-message suppression, and unknown-item fallback | `event.IsControl` semantics are preserved; unknown Codex items become a bounded notice/debug record, never a panic or unbounded payload. |
| Permissions/questions | every supported server-request shape, option-to-decision mapping, `always_approve`, timeout, phone cancellation, session close, engine death, and unsupported method reply | Every server request receives one safe, method-valid response or is failed when transport is already gone; no pending permission can block a session indefinitely. |
| Optional operations | model default precedence/cache generation, rename/fork/compact request shape, active-turn rejection, manager authorization, and persistence atomicity | Do not advertise an operation until its provider implementation and contract tests pass; provider failure never partially mutates daemon metadata. |
| Privacy and bounds | stderr truncation/redaction, prompt/tool-output non-logging, model/item/list response caps, image decode/data-URL caps, queue caps, and fixture secret scan | Diagnostics are bounded and redacted; arbitrary Codex-controlled content cannot exhaust daemon memory or enter info logs. |
| Nested agents | parent/child fixture ordering, ownership inheritance, terminal-drain/unsubscribe, historical lazy-open behavior, and parent cancellation only after live proof | This suite and a tagged live proof are prerequisites to enabling the feature flag; absent proof leaves the feature unavailable rather than guessed. |

### Test execution gates

1. Fast deterministic tests run in the normal Go suite. New provider tests use
   fake pipes/recorded frames and never need a login, network, sandbox, or
   token budget.
2. Race-sensitive package tests run with `go test -race ./...` before
   acceptance. They must cover engine death, event demux, pending RPCs,
   permission timeout, and shutdown, not only the pure parsers.
3. `live_codex` tests are opt-in and version-pinned. They assert the public
   app-server contract that fixtures encode; a skipped sandbox-only approval
   test is reported as an environment prerequisite, not a passing approval
   result.
4. Before staging a Go file, `make pre-add-check` is mandatory under
   `AGENTS.md`. The final acceptance set also includes `go test ./...`,
   `go test -race ./...`, `flutter analyze`, `flutter test`, and Dart format
   verification whenever mobile/protocol code changes.

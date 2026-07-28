# Provider platform canonicalization: implementation plan

**Status:** Accepted; Phases 0 and the low-risk portions of Phase 1 implemented
**Date:** 2026-07-26  
**Decision:** [MADR 0029](./0029-provider-platform-canonicalization.md)  
**Scope:** the mcremote Go daemon, Grok/Goose/OpenCode provider adapters and
their shared transports, the v1 history/capability contract, and the Flutter
session UI.  
**Out of scope:** adding a CLI provider, changing provider-native semantics,
changing the numeric 80 ms streaming default, replacing direct dependencies, a
generic plugin ABI, and a wholesale transport rewrite.

This is an implementation plan, not an instruction to merge every provider.
The codebase has three different, valid agent-facing transport models. The
plan centralizes only data and behavioral policy that must agree across them.

## Implementation record (2026-07-26)

Completed:

- Phase 0 command conformance: Goose is now covered and its noncanonical
  static command-table entries were removed.
- Phase 0 / Phase 1 ACP plan normalization: one tested
  `internal/provider/acpcommon.PlanEntries` conversion is used by ACP stdio
  and ACP-over-HTTP.
- Phase 1 session-content helpers: all existing prompt clone and transcript
  user-message projections now use `internal/provider/sessionutil`.
- Phase 1 shared UI: `WorkItemsPanel` replaces ChatScreen's private part and
  has focused widget coverage for mixed, empty, and long lists.

Not yet started: ACP mode/config conversion, the stream-policy migration,
retention/capabilities work, typed provider declarations, and the conditional
engine-supervisor spike. These remain deliberately sequenced as described
below; their scope is not implied by the completed extraction.

## 1. Fact base and assessment

### 1.1 Current topology

| Provider | Adapter path | Lifecycle model | Important provider-specific behavior |
|---|---|---|---|
| Grok | grok to acpagent | one ACP stdio process per session; optional warm spare | ACP callbacks, terminal host, filesystem callbacks, static plan modes |
| Goose | goose to acphttp | one local goose serve engine, ACP JSON-RPC over WebSocket | framed RPC demux, ACP session lifecycle over a shared engine |
| OpenCode | opencode to httpagent | one opencode serve REST/SSE engine | REST dialect, SSE reconnect/resync, session tree, in-place model and undo/diff operations |
| Fake | fake | in process | deterministic daemon/session tests |

This is grounded in the constructors and registrations in
internal/daemon/daemon.go, the thin Grok/Goose adapters, and OpenCode's
httpagent Dialect implementation. It means a single provider framework would
be an overreach: ACP stdio has host callbacks, Goose has RPC framing, and
OpenCode has a distinct REST/SSE vocabulary.

### 1.2 Findings disposition

| Finding | Evidence | Disposition | Priority |
|---|---|---|---|
| Work items/Todos need a shared menu | TypePlan is reduced into SessionTranscript.plan; ChatScreen renders its one PlanPanel; ACP transports map plans and OpenCode maps todo.updated plus resync. | **Accepted, narrowed.** Functionality is already shared. Make the widget reusable and make normalization contractual; do not add per-provider UI. | P2 |
| ACP data conversion is duplicated and drifting | mapPlanEntries, mode/config emitters, prompt cloning, and user-message projection occur in acpagent and acphttp; ACP plan priority handling differs. | **Accepted.** Extract pure conversions only. | P1 |
| Streaming policy is not universal | chunkbuf drives acphttp and httpagent; acpagent has its own backpressure-only map that drains assistant chunks before thought chunks. | **Accepted.** Migrate only after order/backpressure contracts are pinned. | P1 |
| History/cache policy is fragmented | Host: 800-event ring and 512 KiB response cap; mobile: hard-coded 800 fetch limit, 800 items, 150-item/12-session cache, 400 KiB cache-entry guard. Host durable history is count-bounded only. | **Accepted.** Add byte-aware host retention and advertise server capabilities. | P2 |
| Provider inventory is hand-maintained | daemon.Run, config defaults/validation/Viper defaults, and tests name providers separately. command/conformance_test.go currently omits Goose. | **Accepted.** Fix omission now; add a typed declaration inventory later. | P1 then P2 |
| Shared engine supervision is duplicated | acphttp.Provider and httpagent.Provider separately own process, port, health, generation, and shutdown machinery. | **Deferred conditional extraction.** Extract only an engine supervisor once its behavior is fully tested. | P3 |
| Dependency reduction | Direct dependencies each map to a real boundary: ACP, WebSocket, TLS/Route 53, CLI/config, QR, UUID, platform primitives. | **Rejected.** No dependency replacement or wrapper in this work. | — |

### 1.3 Non-negotiable invariants

Every phase preserves these existing guarantees:

1. internal/session.Manager remains the sole owner of session IDs, owner
   authorization, event sequencing, history, persistence scheduling, and WS
   broadcast entry. Providers only emit provider-domain events.
2. event.IsControl remains the single classification for events that may not
   silently drop after a consumer attaches.
3. Provider command tables remain local statements of observed CLI behavior;
   internal/command remains the resolver and the mobile app does not derive
   command availability itself.
4. plan stays a full replace-snapshot on the v1 wire. Empty entries clear a
   visible work-item panel.
5. The first streamed text chunk is not delayed and pending text must be
   delivered before a turn-ending control event.
6. Replay events seed history without becoming duplicate live transcript
   events.
7. A caller can explicitly set stream_coalesce_ms to zero; this means disabled,
   never “use the default.”
8. Existing provider-specific behavior—OpenCode session trees, Grok ACP
   filesystem/terminal callbacks, and Goose RPC framing—does not move into a
   generic domain package.

## 2. Sequencing and change boundaries

| Phase | Deliverable | Main files | Depends on | Production-risk class |
|---|---|---|---|---|
| 0 | Regression tests and two correctness fixes | command, acpagent, acphttp, goose tests | none | Low |
| 1 | Canonical work-item and session-content helpers; reusable Flutter widget | new provider/acpcommon, provider/sessionutil, plan panel, tests | 0 | Low |
| 2 | One stream-policy API and ACP stdio migration to chunkbuf | chunkbuf, all three transports, config/daemon | 0; preferably 1 | High |
| 3 | Byte-aware history retention and advertised server limits | session, store, protocol, ws, Dart WS/cache | 0 | Medium |
| 4 | Typed provider declarations and shared contract-suite inventory | daemon, config, command, provider tests | 0–1 | Medium |
| 5 | Engine-supervisor extraction decision/spike | acphttp, httpagent, possible provider/enginehost | 2–4 stable | High / optional |

Phases 1 and 3 can proceed in parallel after Phase 0. Phase 2 is deliberately
serial: it modifies highly concurrent event paths and should not share a
release with history retention or engine lifecycle refactoring. Phase 5 does
not start merely because the earlier phases finish; it requires the explicit
go/no-go criteria in section 7.

## 3. Phase 0 — lock in current contracts and correct known drift

**Goal:** make the two known correctness gaps fail in tests before broader
extraction obscures their source.

### 3.1 Add Goose to canonical command conformance

**Fact:** internal/command/conformance_test.go constructs Fake, Grok, and
OpenCode in both provider-table tests. Goose is registered by the daemon and
has goose/commandtable.go, but it is absent from this suite.

**Implementation:**

1. Import internal/provider/goose in the external command_test package.
2. Add goose.New(goose.Config{}) to both provider lists.
3. Make the existing test's “all table keys must be canonical” check pass.
   Goose presently lists status, grind, skills, and doctor, which are not
   entries in command.Specs. These are provider-native commands and must not be
   in the canonical table. Surface them only through Goose's observed
   available_commands updates in a live session; commands.list may remain
   canonical-only until Goose has a verified provider catalog. Do not weaken
   the canonical-table test.
4. Add a Goose-specific test verifying native catalog entries remain advertised
   after removal from the canonical table.

**Why this order matters:** simply adding Goose exposes a real mismatch between
the stated contract and its table. The right correction is to preserve the
boundary between canonical commands and agent-native commands, not to expand
the global command vocabulary based on one CLI.

**Acceptance:** normal Go tests for command and Goose pass; the table has
exactly one declaration per command.Spec; provider-native Goose commands remain
reachable through their advertised catalog.

### 3.2 Make ACP plan normalization identical

**Fact:** acpagent mapPlanEntries maps unknown ACP priorities to medium.
acphttp mapPlanEntries converts priority directly to a string. protocol-v1.md
promises the fixed high, medium, low vocabulary and medium fallback.

**Implementation:**

1. Add table-driven tests in both ACP transport packages for high, medium, low,
   empty, and unknown priority, and for pending, in_progress, completed, empty,
   and unknown status.
2. Normalize unknown priorities to medium and unknown statuses to pending in
   both transports. This gives the documented fixed vocabulary a deterministic
   compatibility rule instead of passing new provider strings through.
3. Use the same fixture data in both package tests. Replace the duplicate
   assertions with one acpcommon unit suite when Phase 1 lands; do not export
   implementation details merely to create a temporary parity package.

**Acceptance:** the same ACP fixture yields equivalent event PlanEntry values
in both transports; an empty plan remains a clear snapshot.

### 3.3 Baseline commands

Run before and after Phase 0:

    env GOCACHE=/tmp/mcremote-gocache GOTMPDIR=/tmp go test ./...
    go test -race ./internal/command/... ./internal/provider/acpagent/... \
      ./internal/provider/acphttp/... ./internal/provider/goose/...

The first command has already passed on the assessment worktree using writable
temporary Go build locations; the environment override is sandbox-specific,
not a repository configuration change.

## 4. Phase 1 — canonical domain helpers and shared work-item surface

**Goal:** remove pure semantic duplication without coupling transports.

### 4.1 Extract ACP conversion package

Create internal/provider/acpcommon with no process, connection, goroutine,
timer, channel, or provider-registry dependency. It may depend on the ACP SDK
and internal/event.

| Helper | Existing sources | Target behavior |
|---|---|---|
| PlanEntries | acpagent/session.go, acphttp/session.go | map content, fixed status/priority vocabulary, unknown status becomes pending and unknown priority becomes medium |
| Modes and mode-state conversion | emitModes and emitModesOrStatic in both transports | return normalized event payload data; transport decides when/how to emit and applies its own static fallback |
| ConfigOptions conversion | emitConfigOptions in both transports | return normalized event ConfigOption values with existing boolean/select semantics |
| ACP content text and benign prompt error classification | duplicate ACP session helpers | retain verbatim-chunk behavior and ACP request-error handling |

Do not move RequestPermission, filesystem callbacks, terminal host code,
session-new/load RPCs, RPC framing, or emit/locking functions. Those are
transport responsibilities.

**Tests:** table-driven unit tests live with acpcommon; existing ACP tests
remain and assert each transport calls the shared mapping. Include a compile
time dependency check by keeping acpcommon free of imports from acpagent or
acphttp.

### 4.2 Extract provider-neutral session-content helpers

Create internal/provider/sessionutil, depending only on internal/provider and
internal/event.

| Helper | Contract |
|---|---|
| CloneContent | preserves order and all fields; returns nil for an empty input |
| UserMessage | concatenates text content in order, describes image/audio attachments without copying payload bytes, and leaves timestamp/session/agent ID unset for the transport to stamp |

Replace the three cloneContent helpers and the equivalent user-message loops in
acpagent, acphttp, and httpagent. Preserve each transport's present
event-emission seam: the helper creates data; the transport assigns context,
ordering, replay state, and delivery behavior.

**Tests:**

- text-only, attachment-only, mixed ordering, unknown content type, and
  mutation-after-clone cases;
- existing provider session tests prove the emitted event still has the correct
  session ID and timestamp;
- confirm base64 attachment data never enters an event or durable history.

### 4.3 Make the work-item widget a reusable Flutter component

**Fact:** the current PlanPanel is already provider-neutral but private and
declared as a part of chat_screen.dart.

**Implementation:**

1. Convert apps/mobile/lib/features/chat/plan_panel.dart from a private part to
   an imported public widget, preferably
   apps/mobile/lib/features/widgets/work_items_panel.dart with
   WorkItemsPanel(entries: ...).
2. Replace the chat-screen part directive and PlanPanel call. Keep the location
   above the composer and outside the scrolling transcript.
3. Keep PlanEntry as the protocol model for this release. UI copy can say
   “Todos”; changing the v1 event/model name would create churn without adding
   capability.
4. Add widget tests for absent/empty invocation, mixed statuses, high priority,
   long list scrolling, and the replace-to-empty reducer path.

**Acceptance:** no provider ID enters Flutter chat rendering; Grok ACP plan,
Goose ACP plan, and OpenCode todo fixtures all produce the same widget input.

### 4.4 Phase 1 gate

    make pre-add-check FILES="<touched Go files>"
    cd apps/mobile && dart format --output=none --set-exit-if-changed .
    cd apps/mobile && flutter analyze && flutter test \
      test/transcript_reducer_test.dart test/chat_render_test.dart

Run the full Go suite plus focused ACP/HTTP package tests before merge.

## 5. Phase 2 — one streaming policy, then migrate ACP stdio

**Goal:** make stream ordering, coalescing, and retry behavior identical before
the manager history ring sees events.

### 5.1 Define a small, transport-independent stream policy

Extend internal/chunkbuf with an exported policy/value API, not with transport
or session knowledge. The policy owns the existing defaults now duplicated in
acphttp/session.go and httpagent/session.go:

- default coalescing window: 80 ms;
- pending run cap: 8 KiB;
- retry delay: 50 ms;
- explicit zero window: disabled.

The buffer remains a pure state machine. The owning session remains responsible
for its mutex, timer, and send function. This preserves the good separation
already described in MADR 0024.

### 5.2 Align configuration with the intended policy

1. Add StreamCoalesceMs to the shared config.ACPProviderConfig; Goose then
   inherits it rather than carrying a duplicate field. Set Grok's default to
   80 ms as part of the deliberate Phase 2 migration. This is a behavior
   change from Grok's present backpressure-only coalescing, not a no-op.
2. Retain OpenCode's same-named field while it has OpenCode-specific config;
   route both shapes through one conversion function that produces the shared
   stream policy/window pointer.
3. Add the corresponding field to acpagent.Config and pass it in
   daemon.acpAgentConfig.
4. Update Viper defaults, config validation, example YAML, config docs, and
   defaults tests together. This is essential because Viper ignores an
   environment value for a key it was never taught.
5. Preserve existing Goose/OpenCode config compatibility; the new Grok key is
   additive. Zero remains opt-out.

### 5.3 Add cross-transport stream contract tests before changing ACP stdio

Use a deterministic fake clock/timer seam or direct buffer transitions. Cover:

| Case | Required observation |
|---|---|
| first chunk | immediately emitted, no latency regression |
| same-type chunks inside window | one later merged event, bytes in order |
| assistant to thought switch | prior run flushes first; no assistant-before-thought reordering |
| control event | pending text delivered before turn_complete, permission, error, mode, plan, or status |
| non-control telemetry | does not fragment a text run |
| replay boundary | replay and live chunks never merge |
| full provider channel | text is unflushed/retried, not discarded until the explicit bounded-loss guard |
| close | final buffered text flushes or behavior is explicitly reported; no timer leak |
| manager history | each flushed event receives a normal monotonic sequence and history never omits an in-flight tail after the flush boundary |

Run equivalent transport-level tests for acphttp and httpagent first. These
prove the contract before touching Grok's mature path.

### 5.4 Migrate acpagent

Replace the coalesced map and drainCoalescedLocked with a single ordered
chunkbuf Buffer, timer, and serialization seam. Do not copy an HTTP transport's
mutex structure blindly: ACP callbacks, close, and current deliver behavior
have their own locking rules. The implementation review must document lock
order and demonstrate that a boundary cannot overtake a timer flush.

Remove the obsolete per-type coalescer only after tests prove:

- interleaved thought/assistant output keeps source order;
- normal Grok output has the same first-token behavior;
- control events are still non-droppable after attachment; and
- ACP session-load replay remains pre-attach and non-live-broadcast.

### 5.5 Phase 2 acceptance and rollback

    go test -race ./internal/chunkbuf/... ./internal/provider/acpagent/... \
      ./internal/provider/acphttp/... ./internal/provider/httpagent/...
    go test -tags live_grok ./internal/provider/grok/ -run "Command|Mode" -count=1
    go test -tags live_opencode ./internal/provider/opencode/ -run Command -count=1

Run the live Goose suite when its binary is available. Capture frame-count and
history-ring fixtures for the same synthetic long reply before/after; this is
evidence, not a benchmark gate.

The rollback is configuration-level for an operational incident:
stream_coalesce_ms set to zero restores one-event-per-chunk behavior. Keep that
switch through at least one release after all transports share the
implementation.

## 6. Phase 3 — authoritative retention and cache compatibility

**Goal:** bound memory and durable history by bytes as well as event count,
without changing session ordering or silently claiming complete history.

### 6.1 Introduce session.RetentionPolicy

Replace scattered manager/store constants with a single policy object supplied
when the manager/store is constructed. Initially it uses current behavior:
800 retained events, default page 200, maximum page 800, and approximately
512 KiB response pages. Add a new per-session retained-byte budget only after
a fixture-based profile determines a value.

The policy must be applied consistently in:

- entry.appendHistoryLocked in internal/session/manager.go;
- durable SaveHistory and defensive LoadHistory handling in
  internal/session/store.go; and
- HistoryPage sizing in the manager.

Use an exact or deliberately conservative encoded-size function shared by
in-memory retention and page construction. The existing approxEventBytes is
only a page estimator and omits some fields; it is insufficient as a durable
memory budget without a testable definition.

### 6.2 Define retention semantics before coding

1. Stamp sequence numbers before deciding whether an event is retained; never
   reuse or renumber a sequence after eviction.
2. Retain the newest ordered suffix satisfying both the event and byte budgets.
3. A single oversized event needs a defined behavior: safely clip fields whose
   UI/protocol already treats as summaries, or preserve it alone with an
   explicit oversize marker. Do not silently split UTF-8 text or turn a control
   event into a non-control event.
4. Persist the exact retained suffix, not the pre-eviction event list.
5. HistoryPage continues to report truncated and next_since_seq; extend the
   response only when a client needs an explicit retention-gap signal. Decide
   this from a replay/resync test, not by guessing a new wire field.

### 6.3 Advertise host limits to clients

Add a backward-compatible history capability object to the AuthOK payload. The
mobile client already waits for AuthOK before using the WebSocket, while the
authenticated HTTP hello endpoint is presently diagnostic-only. The object
contains at least the recommended page limit and maximum response bytes. Old
clients ignore it; a new Dart client uses it instead of hard-coding
kHistoryFetchLimit equals 800 in mcremote_client.dart.

Keep the mobile cache policy local: 150 items, 12 sessions, and the 400 KiB
SharedPreferences guard serve phone storage, not host replay. Bump the cache
schema key if serialization fields or retention interpretation change.

### 6.4 Tests and compatibility

- manager tests: count-only, byte-only, combined, single-oversize, ordering,
  monotonic sequence, and history paging across an eviction boundary;
- store tests: atomic durable save/load has the same suffix as memory;
- WS/protocol tests: capability absent is accepted by old client parsing and
  present limits are honored by the new client;
- Flutter tests: capability-derived page size, automatic multi-page replay,
  cache migration/miss, and a live event racing hydration.

No mobile release may assume the capability exists until the daemon version
floor is explicitly raised; use the current 800 fallback for old hosts.

## 7. Phase 4 — typed provider declaration and contract-suite inventory

**Goal:** eliminate missed edits when adding/configuring a built-in provider,
without introducing runtime plugins or untyped maps.

### 7.1 Design constraints

- config.Config stays typed. Do not decode provider configuration from an
  arbitrary map at runtime.
- Provider packages do not import daemon or config; avoid reversing the
  existing dependency direction.
- Registry construction remains explicit about Fake being opt-in and each
  provider's prewarm/shutdown policy.
- Registry order becomes deterministic so providers.list and tests do not
  depend on Go map iteration.

### 7.2 Implementation shape

Create a daemon-local providerDeclaration slice, likely in a new
internal/daemon/providers.go. Each entry holds:

- provider ID;
- whether the typed config enables it;
- constructor closure from the typed config and logger;
- readiness label/binary path for diagnostics;
- pre-start action: none, EnsureWarm, or EnsureServer;
- optional pre-registration action; OpenCode orphan-engine reap remains
  explicitly attached to OpenCode; and
- contract-fixture constructor for tests.

daemon.Run loops this declaration to register providers, emit readiness
warnings, perform startup actions, and later shut down all providers. Keep
OpenCode's special session-tree and orphan-reap logic visible in its closure;
the goal is an authoritative inventory, not false uniformity.

Move the command conformance test to consume the declaration's test inventory,
or define a small cycle-free inventory package used by both daemon and tests.
If a cycle-free inventory cannot be achieved cleanly, retain a test-only list
but add a daemon test asserting the two ID sets are equal. The latter is safer
than introducing a bad package dependency just to remove a three-item list.

### 7.3 Contract suite

Run the following against every declared fixture in ordinary Go tests:

| Contract | Owner |
|---|---|
| complete canonical command table; valid kinds/ops; readable unavailable reasons | internal/command |
| canonical table contains no provider-native command | internal/command |
| picker catalogs normalize and preserve allow-custom/default semantics | internal/provider and picker |
| work-item fixtures use only canonical status/priority values | event and ACP/OpenCode fixture tests |
| attached control events are not silently dropped | individual transport contract adapters |
| replay is history-only, not duplicate live broadcast | session plus transport fixtures |

Do not pretend every provider can implement every optional operation. The
command table and session interfaces are exactly where a provider says “not
available” with a user-readable reason.

## 8. Phase 5 — conditional engine-supervisor extraction

**Goal:** decide whether duplicated local-engine lifecycle can be reduced
without coupling ACP RPC and REST/SSE.

### 8.1 Entry criteria

Start only when all are true:

1. Phases 0–4 are green and have shipped without a transport lifecycle
   regression.
2. acphttp and httpagent lifecycle tests cover spawn failure, health timeout,
   engine death, stale generation, prewarm, shutdown during boot, process-group
   reap, and stderr diagnostic behavior.
3. A side-by-side behavior matrix identifies a stable common subset larger
   than the extraction's API/adapter cost.

### 8.2 Narrow proposed API

An optional internal/provider/enginehost may own only:

- loopback listener/port reservation;
- process group spawn and bounded stderr capture;
- health polling with caller-supplied probe;
- publication of URL, generation, and death notification;
- graceful terminate, kill escalation, and single Wait ownership.

It must not own ACP initialization, WebSocket RPC framing, REST API calls, SSE
reconnect/resync, session maps, OpenCode child aliases, or agent capabilities.
Each transport continues to own those concerns.

### 8.3 Go/no-go and rollback

Proceed only if both transports can retain their public/provider tests with
minimal semantic changes and Go race tests remain clean. Otherwise close the
spike as “not worth extracting” and retain duplicated but separately tested
supervisors. There is no user-visible dependency on this phase.

## 9. Documentation, config, and release discipline

Every phase updates its source-of-truth documentation in the same change:

- [MADR 0029](./0029-provider-platform-canonicalization.md) implementation log
  and status;
- [protocol-v1](./protocol-v1.md) for any capability/history wire addition;
- [config.md](./config.md) and example YAML for a new or moved
  stream_coalesce_ms key;
- provider live-test notes when observed CLI behavior changes;
- Flutter README only if client compatibility or cache behavior changes.

No Go file is staged before the repository's pre-add gate is clean:

    make pre-add-check FILES="<changed Go files>"

Before each merge, run proportional verification:

    go test ./...
    go test -race ./internal/session/... ./internal/ws/... ./internal/provider/...
    cd apps/mobile && dart format --output=none --set-exit-if-changed .
    cd apps/mobile && flutter analyze && flutter test

Run the live-tagged CLI suites once at the end of a provider transport phase,
not repeatedly during unit-test iteration. Govulncheck is part of the pre-add
gate; this plan does not authorize dependency upgrades unless a separate
security finding requires one.

## 10. Definition of review-ready completion

The plan is successfully implemented when:

1. Grok, Goose, OpenCode, and Fake all participate in the appropriate
   canonical contract inventory.
2. A native plan/todo feed from any provider renders one reusable work-item
   component without provider-specific mobile UI code.
3. ACP conversions and provider-content projections have exactly one pure
   implementation each, with transport-specific I/O left local.
4. All streaming transports share one ordered coalescing policy and preserve
   first-token, control-boundary, replay, and backpressure guarantees.
5. Host history has documented count and byte retention semantics, and a new
   mobile client learns host paging limits without breaking against an older
   daemon.
6. Adding a built-in provider has one typed declaration path and fails tests if
   config, registration, command mapping, catalog, or contract coverage is
   omitted.
7. Engine supervision is extracted only if the Phase 5 evidence demonstrates
   a real common lifecycle; otherwise the explicit decision is to leave the
   two implementations separate.

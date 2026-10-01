# MADR 0029: Canonical provider platform, session surfaces, and retention policy

- **Status**: Accepted — **Phase 0–1 partial; remainder backlog** (as of
  2026-08-05, [0070 F6](./0070-MADR-deep-dive-debugging-pass.md)). Dialects
  remain provider-specific (`acpagent`, `acphttp`, `httpagent`+opencode,
  `codex`). Thinking matrix gap: codex mid-session yes; grok spawn-only;
  goose/opencode no `ThinkingSession`. Not a hotfix train — continue only
  when dialect cost forces it.
- **Date**: 2026-07-26
- **Deciders**: Project Owner
- **Scope**: `mcremote` daemon, provider transports/adapters, provider configuration and registration, the v1 protocol, and the shared Flutter session UI.
- **Related**: [MADR 0018](./0018-MADR-mobile-chat-performance-action-plan.md), [MADR 0019](./0019-MADR-opencode-process-management-plan.md), [MADR 0023](./0023-MADR-canonical-slash-commands.md), [MADR 0024](./0024-MADR-stream-coalescing.md), and [protocol-v1](./protocol-v1.md).
- **Implementation plan**: [0029-PLAN-provider-platform-canonicalization.md](./0029-PLAN-provider-platform-canonicalization.md).

## Context

`mcremote` has reached the useful point where it drives three materially
different agent implementations:

| Provider | Agent-facing transport | Current reusable transport |
|---|---|---|
| Grok | ACP over a per-session stdio process | `internal/provider/acpagent` |
| Goose | ACP JSON-RPC over a shared local HTTP/WebSocket engine | `internal/provider/acphttp` |
| OpenCode | native REST + shared SSE engine | `internal/provider/httpagent` plus the `opencode` dialect |
| Fake | deterministic in-process test provider | `internal/provider/fake` |

The daemon already has several good canonical seams:

- `internal/event` is the provider-neutral event model and one definition of
  control-event delivery.
- `internal/session.Manager` owns session identity, ownership, durable replay,
  sequencing, command dispatch, and fan-out. Providers do not implement these
  independently.
- `internal/command` provides one slash-command vocabulary and resolves it per
  live session. Provider tables state facts about their CLI rather than leaking
  CLI branches into the mobile app.
- `internal/picker` provides one catalog schema for models, agents, commands,
  and future option surfaces.
- `internal/chunkbuf` is a pure, tested streaming-text coalescer already used
  by the two shared-engine transports.
- The Flutter transcript reducer stores one provider-neutral `plan` snapshot,
  and the chat screen renders one plan/todo panel from it.

This is the direction to extend. The remaining work is not “make every
provider identical”: ACP callbacks, OpenCode's session tree, and engine
lifecycle are genuinely different. It is to centralize the semantics that
must never differ, make those semantics testable for every registered
provider, and leave agent-specific wire knowledge at the edge.

## Assessment

### The todo widget is already a shared session feature, but not yet a reusable component

There is not one chat screen per CLI. `ChatScreen` reads the provider-neutral
`SessionTranscript.plan` and displays `_PlanPanel` whenever it is non-empty.
Grok's ACP plan updates, Goose's ACP plan updates, and OpenCode's
`todo.updated`/todo-resync all normalize to `event.TypePlan`; the reducer uses
replace semantics. This is the correct architecture for “every CLI's chat
session gets the Todos menu.”

The remaining issue is packaging and enforcement:

- `_PlanPanel` is a private `part` of `chat_screen.dart`, so it cannot be
  reused by another session surface or independently widget-tested as a public
  capability component.
- The event is named `plan`, while OpenCode's source is a todo list. That is
  acceptable in v1 because the common shape is a replace-snapshot of work
  items, not a provider-specific planning mechanism.
- A provider with no work-item feed correctly emits no plan; the UI stays
  absent. It must not manufacture a generic list by scraping chat text.

### Transport-adjacent code has real semantic duplication

The ACP transports independently implement prompt cloning, user-message
projection, plan/mode/config-option translation, turn queues, permission
completion, and event delivery. Some duplication is intentional because the
stdio client receives callbacks while Goose routes framed RPC messages through
a shared engine. Some is not:

1. `cloneContent` exists in `acpagent`, `acphttp`, and `httpagent`; two
   transports also independently build the same transcript user-message from
   `provider.Content`.
2. ACP plan conversion exists in both ACP transports but has drifted: stdio
   normalizes an unknown priority to `medium`, as the protocol promises, while
   ACP-over-HTTP currently passes it through.
3. `acphttp` and `httpagent` duplicate the coalescing default (80 ms), pending
   byte cap (8 KiB), retry delay, and `StreamCoalesceWindow` semantics.
4. `acpagent` has an older, backpressure-only coalescer. It keeps one pending
   value per event type and flushes assistant text before thought text, which
   can reorder interleaved runs. `chunkbuf` instead represents one ordered run
   and already has the desired boundary semantics.
5. `acphttp` and `httpagent` both supervise a local engine process (spawn,
   health wait, process group, shutdown, generation/death handling) with
   separate private implementations.

### Registration and development-standard checks still have hand-maintained lists

Adding a provider currently entails edits in provider code, `config` defaults
and validation, Viper defaults, `daemon.Run` registration/readiness/prewarm
branches, tests, and documentation. That is manageable for three adapters but
will become omission-prone.

There is an example now: `internal/command/conformance_test.go` says every
registered provider must declare every canonical command, but its provider set
contains Fake, Grok, and OpenCode, not Goose. Goose's table therefore has no
cross-provider enforcement. The test must be fixed immediately, before any
structural extraction.

### Caches and buffers are bounded in useful places, but the retention contract is fragmented

The phone keeps at most 800 transcript items in memory, a local cache tail of
150 items across 12 sessions, caps individual stored text at 100,000
characters, and refuses a SharedPreferences entry above 400 KiB. The daemon
keeps an 800-event history ring, pages history at up to 800 events, limits a
history response to roughly 512 KiB, debounces persistence, and has bounded
WebSocket/client queues. Chunk coalescing materially improves both the phone
and host because it stops a single reply from consuming the entire event ring.

The limits are nevertheless duplicated constants across Go and Dart, and the
host ring is count-bounded rather than aggregate-byte-bounded. A provider can
still put a very large event into the live ring and durable `history.json`; the
response page will split around it but storage and memory have already paid the
cost. These limits should be one explicit product policy rather than a set of
coincidental matching values.

### Dependency posture

The direct module dependencies are purposeful and localized:

| Dependency | Owned boundary | Decision |
|---|---|---|
| `caddyserver/certmagic`, `libdns/route53` | ACME DNS-01 and managed TLS | Keep direct; do not add a project certificate abstraction over it. |
| `coder/acp-go-sdk` | ACP wire types and client-side connection | Keep direct; share only pure ACP-to-domain conversion, never hide the SDK behind a lossy generic protocol. |
| `coder/websocket` | daemon WS, relay splice, ACP-over-HTTP engine | Keep direct; its repeated use is a protocol choice, not duplicate implementation. |
| `google/uuid` | session, process, and pairing identities | Keep direct. |
| `spf13/cobra`, `pflag`, `viper` | `mcremote`/`mcrelay` commands and typed config loading | Keep direct; CLI parsing remains at the application boundary. |
| `mdp/qrterminal/v3` | local pairing QR rendering | Keep isolated in the pair command. |
| `go.uber.org/zap` | CertMagic logger adaptation | Keep until CertMagic no longer needs this adapter. |
| `golang.org/x/sys` | platform file locks and process primitives | Keep direct and isolated behind `auth`, `certs`, and `procutil`. |

The remaining module graph is transitively introduced by those choices (notably
AWS SDK via Route 53, Viper's config readers, CertMagic's ACME stack, and Go's
`x/*` libraries). No replacement or broad wrapper is justified merely to
“reduce dependencies.” In particular, keep ACP SDK and coder WebSocket as
direct dependencies: the daemon deliberately owns their protocol and lifecycle
semantics.

## Decision

Adopt a **layered provider platform**. Canonicalize only provider-neutral data,
policy, and process mechanics; keep each CLI's protocol dialect and feature
mapping local.

## Implementation status

Implemented on 2026-07-26:

- The canonical command conformance suite now includes Goose. Goose's static
  command table contains canonical commands only; agent-native commands remain
  session capabilities received from ACP `available_commands` updates.
- `internal/provider/acpcommon.PlanEntries` is the one ACP plan/work-item
  conversion. Both ACP transports use it, including the pending/medium safe
  fallback for unknown values.
- `internal/provider/sessionutil` now owns prompt-content cloning and the safe
  user-message projection used by all three transports. Attachment payloads
  are not copied into transcript events.
- Flutter's provider-neutral `WorkItemsPanel` is a public widget and
  `ChatScreen` composes it above the message composer as before.

The implementation deliberately stops before the higher-risk stream-policy,
retention, provider-declaration, and engine-supervisor phases. Those phases
have explicit prerequisites and acceptance tests in the implementation plan;
they must not be folded into this low-risk extraction without the prescribed
contract coverage.

### 1. Make session surfaces provider-neutral and reusable

Retain `event.TypePlan` and its full-snapshot semantics as the v1 canonical
work-item surface. Rename no wire field in this decision.

Move the private Flutter `_PlanPanel` into a public,
provider-neutral `features/widgets/work_items_panel.dart` (or equivalent) as
`WorkItemsPanel`. It accepts only `List<PlanEntry>` and presentation options;
it never receives a provider ID. `ChatScreen` composes it above the composer as
today. Add widget tests for empty, mixed-status, clear, and long-list cases.

Provider acceptance rules:

- An adapter that receives a real plan or todo feed normalizes it to a
  `TypePlan` full snapshot and emits an empty snapshot to clear it.
- Statuses normalize to `pending`, `in_progress`, or `completed`; priorities
  normalize to `high`, `medium`, or `low`, with unknown/absent values becoming
  `medium`.
- An adapter without a native work-item feed emits no synthetic plan. Prompting
  an agent to “make a todo list” is not a reliable transport feature.

Extract the ACP-only translation into `internal/provider/acpcommon` (name
subject to implementation review). It owns pure ACP-to-`event` conversions for
plan entries, modes, config options, content text, and permission-shape helpers.
Both `acpagent` and `acphttp` use it. Keep ACP request dispatch, process
management, and callback routing in their present transports.

Extract non-ACP helpers into a small `internal/provider/sessionutil` package:
clone prompt content and build the canonical user-message/attachment event.
It must contain no goroutines, timers, locks, or transport I/O.

### 2. One streaming policy for every provider

Promote `chunkbuf` from a useful HTTP implementation detail to the canonical
streaming run state machine.

All provider transports, including `acpagent`, must use the same policy:

- a run is one ordered `(type, session, agent session, replay)` stream;
- the first chunk is delivered immediately;
- a type/replay/session change flushes the prior run before beginning another;
- a control event, as defined only by `event.IsControl`, flushes pending text
  before that control event is delivered;
- a window expiry, byte cap, close, and retry after backpressure flush it;
- no text is buffered between the session manager history ring and the client;
- retry preserves text and ordering; a bounded growth guard has one loud,
  measurable loss policy.

Move shared defaults and validation (`default window`, pending-byte cap, retry
delay) beside `chunkbuf` or a small stream-policy type. Provider configuration
may still override the window; `nil` means default and an explicit zero means
disabled everywhere. Do not change the existing 80 ms default as part of the
extraction. Preserve the current first-token and end-of-turn latency contract.

This completes the follow-up explicitly left open by MADR 0024 and removes the
known ACP stdio ordering defect. It requires the existing Grok live tests plus
new interleaved assistant/thought, control-boundary, replay, close, and
backpressure contract tests before migration.

### 3. Define an authoritative retention policy

Create a `session.RetentionPolicy` used by the manager and passed to the
history store. It defines at least:

- maximum retained events;
- maximum retained/persisted bytes per session;
- maximum history-page events and encoded bytes; and
- event-specific safe clipping/summarization rules where a single event would
  violate the byte budget.

The manager enforces both count and byte budgets at ingestion, before durable
write and broadcast history retention. It retains the newest valid ordered
suffix, never reorders sequence numbers, and makes truncation observable in
history responses. The exact byte budget must be selected from a representative
tool-heavy transcript profile, not guessed in this MADR.

Expose host history capabilities in the authenticated hello/capabilities
response so the Flutter client requests the server's page limit instead of
depending on a duplicated `kHistoryFetchLimit`. Phone-only cache limits remain
local UX policy, but their cache schema version must change whenever its
serialization or retention semantics change.

### 4. Standardize provider declaration and acceptance checks before engine extraction

Add a daemon-owned provider declaration/manifest used to construct the
registry. A declaration contains the provider ID, enabled/configured state,
constructor, startup policy (none, warm spare, or shared-engine prewarm), and
the provider contract test fixture. Provider-specific config parsing remains
typed; this is not a plugin ABI and does not expose arbitrary third-party
loading.

The declaration becomes the one inventory for:

- daemon registration and readiness diagnostics;
- startup/shutdown/prewarm handling;
- the command-table conformance suite;
- provider-list ordering and test fixtures; and
- the “adding a provider” checklist.

Immediately add Goose to the current conformance test. Add a contract suite
that every declared provider runs:

- command table covers every canonical command and has readable unavailable
  reasons;
- emitted plan values conform to the fixed work-item vocabulary;
- `event.IsControl` events are never silently dropped after attachment;
- replay events do not broadcast as live events; and
- every declared catalog normalizes through `picker.Catalog`.

The contract suite must use fakes/fixtures and run in ordinary `go test`; live
CLI tests remain separate evidence that a table accurately represents a CLI
version.

### 5. Extract a local-engine supervisor only after behavior is pinned

After the earlier steps, evaluate extracting an `internal/provider/enginehost`
component shared by `acphttp` and `httpagent`. Its narrow responsibility is:
reserve/bind loopback endpoint, start a process group, capture bounded stderr,
health-poll, publish generation/death, shut down gracefully, and reap safely.

It must not know ACP, REST paths, SSE frames, WebSocket RPC, session routing,
or OpenCode's session tree. Those remain in `acphttp` and `httpagent`. The
extractor is accepted only if the supervisor contract can be tested once and
both existing engine lifecycle suites pass unchanged in meaning.

## Consequences

### Positive

- Every capable CLI supplies the same Todos/work-items panel through a single
  event contract and one mobile widget; no provider-specific Flutter branch is
  added.
- Command, plan, stream-ordering, picker, and retention invariants become
  enforceable at one seam rather than copied into each new adapter.
- Stream coalescing behaves consistently across Grok, Goose, and OpenCode,
  reducing frames, history churn, manager contention, and phone work without
  delaying the first token.
- A new provider has an explicit declaration and test path, reducing the odds
  of missing a config/default/daemon/test edit.
- Byte-aware retention protects daemon memory and durable history from an
  unusually large agent event, not only a history response frame.

### Costs and risks

- Moving `acpagent` to `chunkbuf` touches the most mature path and needs live
  Grok acceptance coverage. It is a semantic migration, not a mechanical
  rename.
- A manifest must not erase typed configuration or make startup order opaque.
  Keep constructors explicit and compile-time typed.
- A byte budget deliberately truncates history sooner in pathological sessions;
  the protocol must report that fact rather than silently appearing complete.
- The enginehost extraction can be harmful if it becomes a second transport
  framework. Defer it until its shared lifecycle contract is demonstrably
  stable.

## Alternatives rejected

### One giant provider/transport framework now

Rejected. ACP stdio callbacks, ACP-over-WebSocket RPC, and OpenCode REST/SSE
have different failure modes and session semantics. A common framework would
hide those differences behind a large optional-interface surface and make
debugging worse.

### Per-provider mobile chat screens or per-provider todo widgets

Rejected. The provider-neutral event/reducer path already proves this is
unnecessary. It would duplicate behavior and make feature parity depend on UI
branches rather than provider normalization.

### Trust agent-advertised commands or todos without a daemon contract

Rejected. MADR 0023 established that advertised commands are not necessarily
executable. The same standard applies to work-item data: normalize a verified
protocol feed, otherwise omit the feature.

### Introduce a new dependency or external plugin system

Rejected. The needed abstractions are small internal packages and typed Go
constructors. Existing direct dependencies are justified by owned protocol or
platform behavior.

## Delivery plan

| Phase | Change | Gate |
|---|---|---|
| 0 — correctness | Add Goose to command conformance; make ACP plan normalization identical; add work-item contract fixtures. | `go test ./internal/command/... ./internal/provider/...` |
| 1 — pure shared code | Extract `sessionutil` and ACP conversion helpers with table-driven parity tests; move and test `WorkItemsPanel`. | Go tests plus `flutter analyze` and focused widget/reducer tests |
| 2 — stream policy | Move ACP stdio to `chunkbuf`/one policy after contract and live tests cover ordering and backpressure. | `go test -race ./internal/provider/...`; live Grok/Goose/OpenCode acceptance once |
| 3 — retention | Add byte-aware manager/store retention and advertised history capabilities; update Flutter fetch/cache behavior. | manager/WS tests, Flutter history/cache tests, size-profile fixture |
| 4 — declaration | Introduce typed daemon provider declarations and make test inventory derive from them. | daemon/config/command conformance suite |
| 5 — optional enginehost | Extract only common engine supervision after behavior is pinned. | existing `acphttp` and `httpagent` lifecycle/reconnect/race suites unchanged |

Every Go implementation phase follows the repository pre-add gate: `gofmt`,
`golint`, and `govulncheck` must be clean before staging. Dart moves run
`dart format`, `flutter analyze`, and `flutter test` before staging.

## Definition of done

This decision is complete when a new provider can be added by declaring its
typed configuration and manifest entry, choosing a verified transport, mapping
its native capabilities into the existing event/command/picker contracts, and
passing the same contract suite as Grok, Goose, OpenCode, and Fake. The mobile
session surface must remain provider-agnostic: a provider gains a Todos panel
only by emitting valid canonical work-item snapshots, never by adding UI code.

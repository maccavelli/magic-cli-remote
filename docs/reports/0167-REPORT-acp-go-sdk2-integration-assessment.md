# `acp-go-sdk2` integration assessment for `magic-cli-remote`

Observed 2026-10-10; extended 2026-10-11 with
[Full adoption](#full-adoption-what-moving-acp-adherence-into-the-sdk-requires) (PLAN 0167 P15).
This report evaluates the local `acp-go-sdk2` and `magic-cli-remote` sources and recommends an
implementation sequence. It does not approve or implement the dependency migration.

Associated records:

- [0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md](../decisions/0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md)
- [0167-PLAN-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md](../decisions/0167-PLAN-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md)
- [0179-MADR-pigo-native-acp-provider.md](../decisions/0179-MADR-pigo-native-acp-provider.md)

## Executive assessment

`acp-go-sdk2` is a suitable successor to the patched Coder SDK now used by
`magic-cli-remote`. It already contains the three fork changes Magic depends on: configurable
notification overflow behavior, a drop counter, and direct access to the underlying connection.
It also adds a construction-time logger option and updates the generated protocol surface from the
old `0.13.5` schema stamp to stable ACP schema 1.25.0.

The migration is not a blind module-path replacement, but it is bounded. A compile-only scratch
migration reported 24 diagnostics at 23 source locations. They fall into six coherent groups:
stable method promotion, new client-interface methods, named identifier types, schema-correct
unsigned numeric types, one field rename, and tests using the old shapes. Minimal compatibility
changes made the ACP packages compile and pass their tests.

One semantic regression appeared only after compilation: absent raw tool input/output is now a
`json.RawMessage`, so Magic's generic summary code rendered it as the literal `null`. Treating an
empty or JSON-null raw message as absent restored the tests. This demonstrates why the migration
needs behavior tests in addition to compiler-driven edits.

The recommended direction is:

1. release the relied-on `acp-go-sdk2/main` source under an immutable tag;
2. migrate Magic directly to that module while preserving its current advertised capabilities and
   user-visible behavior;
3. use the SDK's accessor and logger option to remove Magic's `unsafe.Pointer` cast and restore
   structured connection logging;
4. add conformance guards around initialization, inbound validation, unknown updates, and the
   stable session surface;
5. consider elicitation, notices, compaction, plan extensions, and unstable subagents separately,
   advertising each only after its complete daemon and phone behavior exists.

The dependency migration should proceed. Capability expansion should not be bundled into it.

The 2026-10-11 extension answers what *full* adoption requires: Magic keeping no protocol
mechanism of its own where the SDK or the stable schema provides one. In summary:

- **Model and thinking selection use a method that is no longer in the protocol.** Magic sends
  `session/set_model` by hand. That method is absent from stable schema 1.25, which carries model
  and reasoning choice as session config options (categories `model` and `thought_level`).
- **Magic implements something it never advertises.** It handles boolean config options end to
  end, phone included, but does not advertise the `session.configOptions.boolean` capability. A
  conforming agent therefore never sends one.
- **Most remaining hand-rolled code has an SDK equivalent.** That covers the unsafe cast, the raw
  `session/resume` read-back, and the "unhandled session update" default. The `_x.ai/*` methods
  are genuine vendor extensions and stay, isolated in the grok spec.
- **Only one provider uses ACP.** `grok` is the only ACP consumer; full adoption of the SDK does
  not by itself move `codex`, `kilo` or `opencode` onto ACP.
- **The SDK is current, and v2 is coming.** Schema 1.25.0 is still the newest stable ACP release,
  and v2.0.0 is in alpha upstream.

## Scope and evidence

### Source baselines

| Repository | Source baseline | Go floor | ACP dependency/schema state |
| :--- | :--- | :--- | :--- |
| `magic-cli-remote` | `9778cbc17b39` | 1.27.2 | requires Coder SDK 0.13.5 and replaces it with the `v0.13.6-mcr.1` fork |
| `acp-go-sdk2` | `d534f27fab2d` | 1.27.2 | module `github.com/maccavelli/acp-go-sdk2`, stable ACP schema 1.25.0 |

The SDK baseline is five commits ahead of tag `v0.14.0` and is not itself tagged. The five commits
add required-key generator coverage, schema-directed lenient decoding, generated validation, and
the corresponding documentation. The report therefore evaluates current source behavior but does
not recommend pinning a branch, local path, or pseudo-version.

Refreshed 2026-10-11: SDK `main` is now `f2a6a3a`, 12 commits past `v0.14.0`, and still has no
newer tag. The seven commits since `d534f27` change the generator module, documentation, and
`RELEASING.md`. Outside `cmd/` and `docs/`, the only changed file is `RELEASING.md`, so the runtime
API evaluated here is unchanged.

Both working trees contained unrelated owner work. That work was left in place. Claims about
implementation use committed source at the SHAs above; uncommitted record edits are not treated as
shipped SDK behavior.

### Read and compared

In `magic-cli-remote`:

- `go.mod`, `go.sum`, and `internal/modguard_test.go`;
- `internal/provider/acpagent`, including connection construction, initialization, sessions,
  session lifecycle, terminal and filesystem callbacks, extensions, and tests;
- `internal/provider/acpcommon`;
- current architecture and the relevant 0166, 0167, and 0179 records.

In `acp-go-sdk2`:

- the hand-written connection, client, agent, error, extension, and helper files;
- generated stable and unstable types and dispatchers;
- schema stamps and generator model/emit code;
- architecture, release, schema, self-containment, and generator-hardening records;
- the history from `v0.13.6-mcr.1` through `v0.14.0` and current `main`.

### Commands and probes

The following green baselines were observed:

- existing Magic packages: `go test ./internal/provider/acpagent
  ./internal/provider/acpcommon ./internal`;
- SDK root: `go test ./...` and `go test -race ./...`;
- SDK generator module: `go test ./...`.

A copied Magic tree then received only these dependency-level changes before the first compile:

- imports changed from `github.com/coder/acp-go-sdk` to
  `github.com/maccavelli/acp-go-sdk2`;
- the old requirement and fork replacement were removed;
- the new module was required and temporarily replaced with the local SDK source for the probe.

`go test -run '^$' -gcflags=all=-e ./internal/provider/acpagent` produced 24 compiler diagnostics at
23 locations. After minimal compatibility edits, the package compiled and the ACP-focused package
tests passed. The full copied-tree run had only the expected local-replace guard failure and
permission-mode failures introduced by copying the tree. No other ACP failure appeared.

The scratch edits were not applied to either repository.

## Current integration in Magic

### Dependency indirection

Magic imports `github.com/coder/acp-go-sdk` from 29 Go files. `go.mod` requires Coder version
`v0.13.5`, then replaces that module with `github.com/maccavelli/acp-go-sdk
v0.13.6-mcr.1`.

This arrangement preserved the old import path while shipping the overflow-policy fork. It was a
reasonable compatibility device for MADR 0167, but it now hides which project actually owns the
code and prevents the module graph from expressing the intended successor directly.

`internal/modguard_test.go` enforces the old arrangement: an ACP replacement must be remote and
must use a `vX.Y.Z-mcr.N` tag. A direct migration therefore has to update the guard, not merely
delete the `replace` line until the test fails.

### Transport behavior that must survive

Magic constructs one `acp.ClientSideConnection` per ACP engine process and opts into
`OverflowDropNewest`. It reads the SDK's cumulative drop counter at turn boundaries and emits a
single notice when output may be incomplete. The stalled-pump test demonstrates why this is a
required property, not an optional tuning choice.

`acp-go-sdk2` retains the relevant API and behavior:

- `WithNotificationOverflowPolicy(OverflowDropNewest)`;
- `DroppedNotifications()` on the connection wrappers;
- the default close-on-overflow behavior when the option is absent;
- the ordered notification barrier and the existing overflow-policy tests.

The migration can preserve this behavior without a compatibility wrapper.

### Unsafe raw access

Magic uses `unsafe.Pointer` to recover the private `*acp.Connection` stored inside
`ClientSideConnection`. It needs the raw connection for non-extension methods that are absent from
the generated surface or whose typed response drops provider-specific fields. The current examples
are `session/set_model` and a raw `session/resume` response used for provider model state.

`acp-go-sdk2` exports `ClientSideConnection.Connection()` and the symmetric agent-side accessor.
The migration can replace the field-layout assumption with `s.conn.Connection()` and remove the
`unsafe` import, `rawConnOf` test seam, and field-order commentary. The raw request path remains
valid for genuine provider deviations; only its unsupported access mechanism goes away.

### Logger limitation

Magic deliberately does not call `SetLogger` after constructing the old connection because the old
SDK starts goroutines inside the constructor and the logger field was not synchronized.
Consequently, SDK diagnostics go through `slog.Default()` and lose the session's structured fields.

`acp-go-sdk2` stores the logger through `atomic.Pointer` and adds `WithLogger`, applied before the
connection goroutines start. Magic can pass its session logger in the same constructor options as
the overflow policy. This removes the race and recovers structured connection diagnostics.

### Capability advertisement is deliberately narrow

Magic currently advertises filesystem read/write and terminal callbacks. It does not advertise
elicitation, session notices, compaction, plan extensions, or ACP unstable subagents.

That narrow advertisement must remain unchanged during the dependency migration. Magic has some
adjacent internal concepts—question cards, notices, compaction commands, plans, and subagent UI—but
similar product concepts are not proof that the exact ACP lifecycle is implemented.

## What `acp-go-sdk2` adds

### Direct ownership and a self-contained generator

The module is now `github.com/maccavelli/acp-go-sdk2`; it no longer impersonates the Coder module.
The generator is a separate module without third-party requirements and emits Go through `go/ast`
and `go/format`. A declarative protocol table records behavior the schema cannot express, including
session cancellation, optional `session/load`, method capability gates, and union constructors.

For Magic, the immediate benefit is dependency clarity. The longer-term benefit is that schema
updates and protocol fixes are maintained in a repository governed by the same record, test, Go,
and disclosure rules as Magic.

### Stable ACP schema 1.25.0

The current fork used by Magic was generated from the historical `0.13.5` schema stamp. SDK2 is
generated from stable `schema-v1.25.0`. Material changes visible to Magic include:

- `session/delete` and elicitation promoted from unstable generated names to stable names;
- stable session notices and compaction types;
- current client session capability types;
- corrected identifier, counter, line, limit, terminal, and exit-code types;
- `planId` replacing the old `id` spelling for plan removal;
- current stable and unstable method constants generated from schema metadata.

This does not make Magic conformant by itself. It gives Magic types that describe the current
stable wire contract, so discrepancies become compiler errors or explicit adaptation points rather
than silently stale structs.

### Wire fidelity and forward compatibility

The generated code now preserves several distinctions the old surface lost:

- unknown discriminated-union variants are retained as raw JSON instead of being discarded;
- catch-all variants preserve undeclared fields;
- absent discriminators are resolved according to required keys where the schema permits it;
- untyped values such as `rawInput`, `rawOutput`, and error `data` remain `json.RawMessage`;
- number formats generate sized Go types rather than a generic `int` or lossy `float64` path;
- missing required JSON properties fail decoding and name the type and property;
- generated `Validate` methods traverse nested values and enforce schema constraints;
- lenient schema keywords use the same default/drop behavior described by the ACP schema.

Inbound typed dispatchers unmarshal and validate before invoking Magic's client implementation.
Malformed requests therefore receive JSON-RPC invalid-params errors at the SDK boundary instead of
arriving as plausible zero-valued Go structs.

The strictness is a benefit and a rollout risk. A provider that currently emits malformed but
tolerated frames will fail earlier after migration. Provider live tests and wire captures are
therefore acceptance gates, not optional follow-up.

### Existing limitations remain

SDK2 does not remove every limitation recorded in MADR 0167:

- `Connection` still exposes `Done()` without an explicit `Close()`;
- the line-delimited reader still has a finite scanner buffer;
- `OverflowDropNewest` still trades transport survival for possible update loss;
- notifications still pass through a single ordered consumer and response barrier;
- stable `Agent` and `Client` interfaces require all stable methods at compile time even when a
  capability is not advertised.

The per-turn loss notice and the existing stalled-pump coverage must remain. Migration to SDK2 is
not evidence that these risks disappeared.

## Compatibility findings

### Compiler findings by category

| Category | Observed Magic locations | Required adaptation |
| :--- | :--- | :--- |
| Stable elicitation methods | `session.go`, plus constructors in tests | Implement the two required `acp.Client` methods; return method-not-found while no elicitation capability is advertised, or implement the full product flow in a later decision |
| Stable `session/delete` | `sessioncaps.go` | Replace `UnstableDeleteSession` and its request type with `DeleteSession` and `DeleteSessionRequest` |
| Named IDs | authentication and terminal code | Convert at the boundary between Magic strings and `AuthMethodId`/`TerminalId`; convert back only where Magic's own interface is string-based |
| Unsigned schema values | usage, file line/limit, terminal output limit and exit status | Use checked conversions; never rely on a wrapping `uint64` to `int` cast |
| Field spelling | plan-removal test | Use `PlanId` instead of `Id` |
| Required interface conformance | connection construction in production and tests | Add the stable client methods before constructing `ClientSideConnection` |

These diagnostics are migration guidance, not defects in either codebase. Most are the compiler
exposing schema drift that the old generated types could not express.

### Raw JSON is the semantic compatibility seam

Old untyped fields decoded into `any`. JSON `null` became a nil interface, which Magic's
`shortAny` helper treated as absent. SDK2 deliberately represents those fields as
`json.RawMessage` to preserve bytes and integer precision. An empty raw message or a raw `null` is
not a nil interface, so the same helper produced `in: null · null` in a permission detail.

The correct adapter rule is:

- empty `json.RawMessage` and trimmed `null` mean absent for the human-summary fallback;
- JSON objects and arrays remain omitted from chat summaries as today;
- scalar raw JSON may be rendered within the existing length bound;
- the raw bytes remain available for protocol logic and diagnostics.

A test must cover empty, `null`, scalar, object, array, and large raw values. The test must be seen
failing with the old generic behavior before the compatibility rule is accepted.

### Numeric conversions need policy, not casts

SDK2 correctly uses `uint64` for context usage and terminal output limits, `uint32` for file line
and limit values, and `uint32` for normal terminal exit codes. Magic's internal event model uses
`int` for context usage and its terminal host uses `int` buffer sizes and OS exit codes.

On the currently shipped 64-bit targets, file `uint32` values fit in `int`, but context usage and
output limits can still exceed `MaxInt`. Recommended adapters should:

- clamp an agent-requested terminal buffer to `maxTerminalBuffer` before converting to `int`;
- convert file line/limit after bounding them against the actual line count;
- reject, omit, or saturate an unrepresentable usage value according to an explicit product rule,
  with a test at `MaxUint64`;
- convert a non-negative OS exit status to `uint32`, and keep signal termination represented by the
  signal field rather than fabricating an unsigned exit code.

The scratch probe used the minimum casts needed to expose later failures; those casts are not the
recommended production implementation.

### Elicitation is a compile requirement, not yet a capability claim

Schema 1.25 makes `elicitation/create` and `elicitation/complete` stable methods on `acp.Client`.
Magic's session therefore must implement their Go methods to compile, even while it advertises no
elicitation capability.

For the behavior-preserving migration, method-not-found is the honest response if a non-conforming
agent calls an unadvertised method. Reusing Magic's existing question-card code is a later product
option, not a mechanical change: ACP form elicitation carries a typed schema and accept/decline/
cancel response, and URL elicitation has its own completion lifecycle. Both need daemon, protocol,
mobile, cancellation, retention, and security decisions before advertisement.

### Stable delete is an immediate adherence improvement

Magic already checks `SessionCapabilities.Delete` and sends `session/delete`, but it does so through
the old unstable generated method. Switching to `DeleteSession` changes no wire method and aligns
the code with the stable schema. This belongs in the dependency migration rather than a later
capability phase.

## Opportunities to improve ACP adherence

### Immediate, behavior-preserving opportunities

The dependency phase can improve correctness without widening advertised capabilities:

- send `InitializeRequest.ClientInfo` with the Magic client name and build version;
- keep protocol version 1 and the current filesystem/terminal capabilities;
- use stable `DeleteSession`;
- return method-not-found for unadvertised stable elicitation methods;
- use `WithLogger` at construction;
- use `Connection()` instead of `unsafe.Pointer`;
- record unknown session-update variants with bounded diagnostics instead of the generic
  “unhandled session update” message;
- add negative-wire tests proving missing required fields receive `-32602`.

`ClientInfo` already exists in both SDK generations; its omission is a Magic gap, not a reason for
the migration. It belongs beside the migration because initialization wire behavior is already
under review there.

### Adherence opportunities that need product work

SDK2 exposes stable schema features that Magic does not yet implement end to end:

| ACP surface | Magic has adjacent machinery | Missing before advertisement |
| :--- | :--- | :--- |
| Session notices | `event.TypeNotice` | severity/title/description mapping, retention and mobile presentation tests |
| Compaction updates and summary chunks | compact commands and transcript events | ID-addressed lifecycle, summary replacement/append semantics, history and mobile rendering |
| Elicitation form and URL modes | question cards and provider callbacks | schema-to-form mapping, URL trust/opening policy, accept/decline/cancel completion, reconnect behavior |
| Plan update/removal extension | baseline plan and plan-clear handling | ID-addressed plan-update content and complete capability coverage |
| Unstable ACP subagents | Magic's own subagent event model | exact ownership, message, restriction, and lifecycle mapping |
| Session info update | session title event exists | title/clear behavior, timestamp policy, persistence and phone update tests |
| Usage cost | usage event and cost fields exist | currency handling and cumulative-vs-turn accounting rules |

Proposed MADR 0179 already identifies session-title, usage-cost, config-option, tool-location, and
vendor-operation gaps. Those findings remain relevant. This report does not accept 0179 or fold its
larger pigo/provider design into the SDK migration.

### Capability honesty should become a testable invariant

Magic should have a table-driven initialization test that compares every advertised ACP capability
with the concrete handlers and phone mapping that implement it. The invariant is:

> A capability is advertised only when every method or update it permits has defined daemon
> behavior, bounded failure behavior, and a tested client-facing result.

This is more durable than manually inspecting an initialization literal after each schema update.

## Recommended implementation sequence

These are recommendations for a later approved PLAN. They are not executed by this report.

### Phase 0 — Publish an immutable SDK release

1. Finish and verify the current SDK source intended for Magic.
2. Run both module gates, the race suite, record checks, and release checks at the exact commit.
3. Tag a release containing the five post-`v0.14.0` commits on which this assessment relies.
4. Confirm the tag resolves through the Go module proxy and checksum database from a clean module.

Magic should not use a filesystem replacement, a branch, or a pseudo-version as its committed end
state. That would recreate the portability problem `internal/modguard_test.go` was written to
prevent.

### Phase 1 — Direct-module, behavior-preserving migration

1. Replace the Coder requirement and `-mcr.1` replacement with one direct requirement on the new
   immutable SDK tag; run `go mod tidy`.
2. Change all 29 Go imports to `github.com/maccavelli/acp-go-sdk2`.
3. Rewrite `internal/modguard_test.go` to reject ACP filesystem replacements and the retired Coder
   path. If the project requires exact SDK pinning, assert the direct module and approved tag form
   rather than requiring a replacement.
4. Add the stable elicitation methods with honest unsupported behavior; do not advertise
   elicitation.
5. Migrate stable delete, named IDs, unsigned fields, and `PlanId` with boundary tests.
6. Replace the unsafe raw-connection cast with `ClientSideConnection.Connection()`.
7. Pass both `OverflowDropNewest` and `WithLogger(sessionLogger)` when constructing the connection.
8. Add the raw-JSON compatibility rule and keep the existing drop notice and stalled-pump tests.
9. Keep the initialization capability block otherwise unchanged.

The phase should land as one dependency migration because the new module cannot compile until the
API adaptations land with it. Within the phase, commits may separate mechanical imports, adapters,
and behavior tests if every commit builds.

### Phase 2 — Conformance and observability hardening

1. Add `ClientInfo` and pin its serialized name/version in a wire test.
2. Add malformed-frame tests for required properties, invalid union shapes, numeric overflow, and
   invalid params; observe each failing against a deliberately weakened copy first.
3. Add unknown-union/update fixtures and prove the connection survives with bounded diagnostics.
4. Add the advertised-capability invariant test.
5. Map session info title and usage cost only after their internal accounting semantics are
   explicit.
6. Capture initialize, new/load/resume, prompt, permission, terminal, filesystem, delete, cancel,
   and representative session-update frames against each supported ACP engine.

This phase should not add capabilities. It proves that the upgraded baseline is strict where the
schema is strict and tolerant where the schema provides an extension or unknown-variant path.

### Phase 3 — Capability uptake, one decision at a time

Evaluate notices, compaction, elicitation, plan extensions, and unstable subagents as separate
product increments. Each increment should include:

- a decision about phone behavior and persistence;
- daemon and mobile implementation;
- exact initialization capability advertisement;
- positive and negative wire fixtures;
- reconnect, cancellation, timeout, and unsupported-peer behavior;
- a live probe against an agent that implements the capability.

Unstable ACP surfaces should additionally be isolated so a schema refresh can remove or rename
them without breaking baseline sessions.

## File-level migration map

| Area | Mandatory dependency phase | Later or optional |
| :--- | :--- | :--- |
| `go.mod`, `go.sum` | direct SDK2 requirement; remove Coder requirement and fork replacement | SDK release upgrades |
| 29 importing Go files | module-path replacement | none |
| `internal/modguard_test.go` | retire `-mcr.N` replacement assumptions; reject local ACP replacement | exact-version policy if desired |
| `acpagent/acpagent.go` | constructor options, named auth ID conversions | `ClientInfo`, new advertised capabilities |
| `acpagent/session.go` | client-interface methods, checked usage conversion, raw JSON, accessor replacing unsafe | notices, compaction, session info, cost, unknown-update presentation |
| `acpagent/sessioncaps.go` | stable `DeleteSession` | broader session-operation work from proposed MADR 0179 |
| `acpagent/terminal.go` | named terminal IDs, bounded output-limit conversion, unsigned exit code | none |
| ACP tests | new types and field names; fail-first raw/null and numeric boundaries | capability-specific fixtures and live tests |
| Docs/records | dependency and execution record | capability MADRs and protocol guide updates |

## Acceptance gates for a later migration

### Dependency and source gates

- `go list -m` reports the direct `github.com/maccavelli/acp-go-sdk2` tag.
- No Go file imports `github.com/coder/acp-go-sdk`.
- `go.mod` contains no ACP `replace` directive and no filesystem replacement.
- `unsafe` is absent from `internal/provider/acpagent` unless another independently justified use
  exists.
- The only generic raw standard-method path uses the SDK's public `Connection()` accessor.

### Behavior gates

- Overflow policy removed on a scratch copy: the stalled-pump test is observed failing; restored:
  it passes for the required repeated run.
- Empty and JSON-null raw inputs produce no `null` permission detail; scalar raw values remain
  bounded; objects and arrays remain suppressed from chat summaries.
- Maximum numeric fixtures cannot wrap negative or allocate above configured bounds.
- `session/delete` uses the stable method and preserves close-on-failure behavior.
- An agent calling unadvertised elicitation receives method-not-found and does not create a phone
  question.
- Initialization advertises exactly the pre-migration capability set, plus `ClientInfo` only if
  that separately reviewed wire change is included.
- The SDK logger carries the session fields under `go test -race` with no logger race.
- Notification-drop notices remain once per affected turn and precede turn completion.

### Protocol gates

- Missing required request properties receive invalid-params responses.
- Known schema 1.25 session updates decode and map deterministically.
- Unknown union variants do not tear down the connection.
- No notice, compaction, elicitation, plan-extension, or unstable-subagent capability is advertised
  without its own complete acceptance coverage.
- Wire captures show no unintended change to initialize, prompt, permission, cancel, terminal,
  filesystem, or turn-completion behavior.

### Repository gates

Use the repository's current pre-add, race, Windows, records, and Markdown gates. Run the live ACP
provider probes at acceptance, because stricter decoding can reveal peer frames unit fixtures do
not contain. A new or changed assertion is accepted only after it has failed on a deliberately
broken scratch copy.

## Risks and mitigations

| Risk | Consequence | Mitigation |
| :--- | :--- | :--- |
| Depending on untagged SDK source | irreproducible builds or a local-only migration | release and resolve an immutable tag first |
| Stricter decoding exposes peer nonconformance | session startup or a callback fails earlier | wire fixtures and live probes before rollout; fix the peer or document a schema-approved leniency, not a blanket bypass |
| Raw JSON changes human summaries | `null` noise or accidental structured-data leakage | explicit raw-message adapter and boundary tests |
| Unsigned values are cast directly | overflow, negative values, or oversized allocation | bound before conversion and test maximum values |
| New stable interface methods are confused with advertised support | agents invoke incomplete product flows | compile-time stubs plus no capability advertisement until end-to-end implementation |
| Unknown variants are decoded but ignored by Magic | silent feature loss | bounded diagnostic naming the unknown update; capability-specific mapping later |
| Capability expansion is bundled with the module change | difficult rollback and unclear regressions | behavior-preserving migration first, one capability decision at a time |
| Drop-on-overflow is mistaken for lossless delivery | incomplete transcripts without warning | retain the counter, per-turn notice, and stalled-pump/drop tests |

## Full adoption: what moving ACP adherence into the SDK requires

Added 2026-10-11 under PLAN 0167 P15. The sections above cover a behavior-preserving dependency
migration. This section answers the owner's wider question: what must happen for Magic to stop
maintaining ACP protocol behavior of its own, so that adherence to the current standard is the
SDK's job. Everything here is a recommendation for later decisions. MADR 0167 D23's separation of
transport migration from capability uptake still applies.

### Upstream position

- **Stable schema.** `git ls-remote` on `agentclientprotocol/agent-client-protocol` (2026-10-11)
  shows `schema-v1.25.0` as the newest stable schema tag, after `v1.24.0` and `v1.24.1`. SDK2 is
  generated from it (`schema/version` reads `1.25.0`), so SDK2 is current for ACP v1.
- **Next major.** Eight `schema-v2.0.0-alpha.N` tags exist, the latest `alpha.8`. A v2 schema is
  therefore expected to break v1 shapes.
- **Where v2 lands.** In SDK2's generator first. Magic's exposure to v2 is proportional to how much
  protocol it handles outside generated types: raw requests, vendor `_meta`, and hand-written
  JSON. Reducing that surface, as listed in L2 below, is also the cheapest preparation for v2.
- **Recommendation:** neither SDK2 nor Magic tracks v2 alphas. The SDK adopts v2 under its own
  decision record after a stable tag. Magic follows only when its ACP engines speak v2.

### The five layers of full adoption

| Layer | What it means | Depends on |
| :--- | :--- | :--- |
| L1. Dependency | direct `acp-go-sdk2` tag, no fork or `replace`, no `unsafe` (Phases 0–1 above) | an SDK release tag |
| L2. Offload | no hand-rolled request, decode or dispatch for anything the schema defines; vendor extensions isolated in the provider spec | L1 |
| L3. Capabilities | each stable client capability is either implemented end to end and advertised, or explicitly declined | L1; one product decision per capability |
| L4. Conformance | wire validity enforced by SDK-generated validation, with Magic tests pinning it | L1 |
| L5. Provider scope | which Magic providers use ACP at all | separate provider decisions |

L1 is the only layer the other four all need. L2 and L4 shrink Magic's code. L3 grows Magic's
product surface. L5 is not an SDK question.

### L2: hand-rolled protocol mechanisms and their disposition

Observed at `9778cbc17b39` in `internal/provider/acpagent` unless stated otherwise.

| Mechanism | Where | Why it exists | Recommended disposition |
| :--- | :--- | :--- | :--- |
| `rawConnOf` `unsafe.Pointer` cast | `session.go:2622-2631` | reach the raw connection for methods the old SDK could not send | **Delete**: use `ClientSideConnection.Connection()` (Phase 1) |
| Generic `rawRequest` for non-`_` methods | `session.go:2648-2668` | standard methods absent from, or lossy in, the old generated surface | **Shrink to zero for standard methods** once the two rows below move. Keep it only for `_`-prefixed vendor methods (`CallExtension`). Gate: no `rawRequest(…, "session/…")` outside a vendor fallback |
| Raw `session/set_model` for `SetModel` | `session.go:946-990` | grok accepted it live (MADR 0039 D1); the reply carries `_meta.model.Ok/Err` | **Replace.** `session/set_model` is not a method in stable schema 1.25: `constants_gen.go` has no such constant. Model choice is a session config option with category `model`, set with the typed `SetSessionConfigOption`, whose response returns the updated `configOptions`. Use that when the agent advertises a `model` option. Keep raw `session/set_model` only as a fallback inside the grok spec, for an engine that advertises none |
| Raw `session/set_model` with `_meta.reasoningEffort` for thinking | `thinking.go:13,70` | grok vendor shape (MADR 0106) | **Replace** with the `thought_level` config option category when advertised; otherwise the same vendor fallback |
| Raw `session/resume` read-back after setting thinking | `thinking.go:77` | confirm grok applied the effort, from its resume snapshot | **Replace.** The typed `ResumeSession` now returns `Meta`, `ConfigOptions` and `Modes`. Better still, read back from `SetSessionConfigOptionResponse.ConfigOptions`, so a live session is not re-resumed to read its own state |
| `x.ai/sessionConfig` harvesting from `_meta` | `session_meta.go:42-78` | grok mirrors its config, with categories, in vendor `_meta` | **Prefer the standard field.** Read selections from `configOptions` (category plus `currentValue`) when present, and keep `_meta` harvesting as the grok fallback |
| `_x.ai/session/fork` | `fork.go:44` | grok's fork | **Keep as vendor** until the engine advertises `sessionCapabilities.fork`. Then prefer `UnstableForkSession` behind that capability, isolated as unstable |
| Inbound `_x.ai/exit_plan_mode`, `_x.ai/ask_user_question` | `extensions.go:28-29` | grok client interactions over extension methods | **Keep**: genuine extension methods, routed by the SDK's `ExtensionMethodHandler`. `ask_user_question` overlaps stable form elicitation; if grok adopts `elicitation/create`, migrate to it under L3 |
| `_x.ai/*` notifications (models, MCP status, session notification) | `internal/provider/grok/grok.go:86-104` | vendor telemetry | **Keep** in the grok spec. Re-check `_x.ai/session_notification` against stable `notice` when notices are adopted |
| `default:` "unhandled session update" (Debug) | `session.go:1785-1786` | catch-all | **Replace** with an explicit case per schema 1.25 variant: handled, or declined with a bounded diagnostic naming the variant. Name `SessionUpdate.Unknown` separately |
| `PlanRemoved` handled without advertising `plan` | `session.go:1749` | tolerant receive | **Keep** the tolerance. Advertise `plan: {}` only after `plan_update` is also implemented (L3) |
| Engine version from `AgentInfo`, else `_meta` | `version.go:22-27` | engine identification | **Keep**: it already prefers the standard field |
| `acpcommon.PlanEntries` and enum mapping | `internal/provider/acpcommon/plan.go` | maps ACP plan enums to Magic's event vocabulary | **Keep**: product mapping, not protocol; import path change only |

L2's net effect: after L1 and L2, the only ACP code in `acpagent` that is not a typed SDK call is
the grok vendor surface (`_x.ai/*` methods and `_meta` fallbacks). That surface sits behind
`acpagent.Spec` hooks owned by `internal/provider/grok`.

### L3: schema 1.25 client surface coverage

"Stable" and "unstable" follow the SDK's generated markings. "Today" is Magic at `9778cbc17b39`.

#### Methods the agent calls on Magic

| Method | Stability | Today | Full-adoption target |
| :--- | :--- | :--- | :--- |
| `session/update` | stable | handled | keep; make the variant switch exhaustive |
| `session/request_permission` | stable | handled | keep |
| `fs/read_text_file`, `fs/write_text_file` | stable | handled, advertised | keep |
| `terminal/create`, `output`, `wait_for_exit`, `kill`, `release` | stable | handled, advertised | keep; checked conversions for unsigned types (above) |
| `elicitation/create`, `elicitation/complete` | stable | not present (compile requirement after migration) | Phase 1: method-not-found. L3: implement form mode on the phone's question cards, and URL mode with an explicit trust policy, then advertise `elicitation` |
| `mcp/message` | unstable | not handled | decline (method-not-found) unless MCP-over-ACP is adopted |

#### Methods Magic calls on the agent

| Method | Stability | Today | Full-adoption target |
| :--- | :--- | :--- | :--- |
| `initialize` | stable | typed; no `clientInfo` | add `clientInfo` (Phase 2) |
| `authenticate` | stable | typed | keep; `AuthMethodId` conversion |
| `logout` | stable | not used | optional: wire to provider sign-out where Magic offers one |
| `session/new`, `session/load`, `session/prompt`, `session/cancel`, `session/set_mode`, `session/set_config_option`, `session/list`, `session/close` | stable | typed | keep |
| `session/delete` | stable | typed through the old `UnstableDeleteSession` | `DeleteSession` (Phase 1) |
| `session/resume` | stable | raw, read-back only | typed `ResumeSession`; consider resume-on-reconnect where `sessionCapabilities.resume` is advertised |
| `session/set_model` | **absent from 1.25** | raw | config option `model` (L2) |
| `session/fork` | unstable | vendor `_x.ai/session/fork` instead | as L2 |
| `document/*`, `nes/*`, `providers/*` | unstable | not used | decline: editor-centric features with no phone counterpart |

#### Session-update variants

| Variant | Stability | Today | Full-adoption target |
| :--- | :--- | :--- | :--- |
| `user_message_chunk`, `agent_message_chunk`, `agent_thought_chunk`, `tool_call`, `tool_call_update`, `plan`, `available_commands_update`, `current_mode_update`, `config_option_update` | stable | mapped | keep |
| `usage_update` | stable | `used` and `size` mapped; `cost` ignored | map `cost` after the accounting rule (per turn or cumulative, currency) is decided |
| `session_info_update` | stable | falls to `default` | map title (and clear) to Magic's session-title event |
| `notice` | stable, gated on `session.notices` | not advertised, falls to `default` | map to `event.TypeNotice`, then advertise |
| `compaction_update`, `compaction_summary_chunk` | stable, gated on `session.compaction` | not advertised, falls to `default` | ID-addressed lifecycle in transcript and phone, then advertise |
| `plan_update` | unstable, gated on `plan` | not advertised, falls to `default` | implement with `plan_removed`, then advertise `plan` |
| `plan_removed` | unstable | handled as plan clear | keep |
| `subagent_update`, `session_message`, `session_message_chunk` | unstable, gated on `subagents` | not advertised, fall to `default` | separate decision mapping onto Magic's own subagent model |
| unknown (`SessionUpdate.Unknown`) | n/a | falls to `default` | bounded diagnostic naming the raw `sessionUpdate` value |

#### Client capability fields

| Field | Today | Full-adoption target |
| :--- | :--- | :--- |
| `fs.readTextFile`, `fs.writeTextFile`, `terminal` | advertised | keep |
| `session.configOptions.boolean` | **not advertised, although implemented end to end**: the daemon maps boolean options (`session.go:1967`) and sends them (`SetConfigOption`, `session.go:1016`); the phone renders them (`apps/mobile/lib/data/protocol/models.dart:2445-2471`) | **advertise `{}`**. The schema says agents may include boolean entries only when the client advertises this, so a conforming agent never sends Magic one today. A small, immediate adherence fix |
| `session.notices`, `session.compaction` | not advertised | advertise each with its L3 mapping |
| `elicitation` | not advertised | advertise with L3 elicitation |
| `plan` (unstable) | not advertised | advertise with `plan_update` |
| `subagents` (unstable) | not advertised | separate decision |
| `auth.terminal` | not advertised | decline: Magic cannot reproduce an interactive terminal sign-in on the phone; its own provider auth transactions (0074) cover sign-in |
| `nes`, `positionEncodings` (unstable) | not advertised | decline |
| `_meta` | none | none needed |

### L4: conformance owned by the SDK

- **Inbound** requests and notifications are unmarshalled and validated by SDK2's generated
  dispatcher before they reach Magic: 12 `p.Validate()` call sites in `client_gen.go`. Magic owns
  only the tests that pin this at its boundary (Phase 2 above).
- **Outbound** requests are not validated by the SDK. The typed methods call `SendRequest`
  directly (for example `NewSession`, `client_gen.go:283-285`), although 145 generated `Validate`
  methods exist. **Recommendation:** make this an SDK change, not a Magic one: generated outbound
  methods validate their params and decoded results. Magic's request builders then cannot emit a
  schema-invalid frame without a test noticing. Until that SDK change ships, a Magic test can call
  `Validate()` on each request its builders produce.
- **Capability honesty** remains Magic's invariant (see above). L3's matrices are its expected
  table.
- **Wire fixtures** per engine stay acceptance gates, because stricter decoding meets real peer
  frames only in live runs.

### L5: provider scope

Observed: only `internal/provider/grok` builds on `acpagent` (`grok.go:49-139`). `codex` talks
to the Codex app server over its own JSON-RPC (`internal/provider/codex`). `kilo` and `opencode`
use HTTP dialects behind `internal/provider/httpagent`. None of the three imports an ACP SDK.

Full adoption of `acp-go-sdk2` therefore makes the grok path standards-current. It does not put
any other engine on ACP. Whether another engine should move to ACP, or a new native ACP provider
should be added, is a provider decision with its own trade-offs:
[0176-MADR-phase-2-grok-acp-provider.md](../decisions/0176-MADR-phase-2-grok-acp-provider.md)
and the proposed
[0179-MADR-pigo-native-acp-provider.md](../decisions/0179-MADR-pigo-native-acp-provider.md) are
where those choices are recorded. This report does not absorb them. What L1–L4 give such a
decision is an `acpagent` whose protocol behavior is the SDK's, so a second ACP engine needs a
`Spec` and fixtures, not more protocol code.

### Revised sequence for full adoption

This extends Phases 0–3 above. Phases 0–2 are unchanged except where noted.

| Phase | Adds to the earlier phase | Code expected to go | Owner decision needed |
| :--- | :--- | :--- | :--- |
| 0. SDK release | also: outbound validation in the SDK (L4), if accepted there | none in Magic | SDK record for outbound validation |
| 1. Direct-module migration | unchanged | `unsafe` cast and `rawConnOf` seam; the fork `replace`; `-mcr.N` guard logic | amend MADR 0167 for the migration |
| 2. Conformance | also: exhaustive session-update switch; advertise `session.configOptions.boolean` | the generic `default` path | boolean advertisement is a wire change: record it |
| 2b. Standard config options (new) | `SetModel` and thinking through `session/set_config_option` (categories `model`, `thought_level`); typed `ResumeSession`; read-back from config-option responses; `_meta` only as grok fallback | raw `session/set_model` and `session/resume` outside the grok fallback; most `_meta` model and effort parsing | amend MADR 0039 D1 and MADR 0106. **Needs a live probe** (owner approval, since it spends provider quota) of whether grok advertises `model` and `thought_level` config options |
| 3. Capabilities, one decision each | notices; session info; usage cost; compaction; elicitation (possibly subsuming `_x.ai/ask_user_question`); `plan_update` with `plan` advertisement | per capability, the corresponding `default` fall-through | one MADR per capability |
| 4. Schema v2 | when a stable v2 schema and a v2-speaking engine exist | whatever v1-only shapes remain | SDK and Magic records at that time |

### Additional acceptance gates for full adoption

- No `rawRequest` or `SendRequest` call with a non-`_` method name outside an explicitly named
  vendor fallback in the grok spec.
- A table-driven test covers every schema 1.25 session-update variant plus `Unknown`. Each is
  mapped, or declined with a diagnostic, and none reaches a silent default.
- The initialize capability block equals the L3 matrix's "advertised" column, enforced by the
  capability-honesty test.
- With a config-option-capable engine, model and thinking changes produce
  `session/set_config_option` frames, and the vendor fallback is exercised only by a fixture that
  advertises no such option.
- Every new or changed assertion is first seen failing on a deliberately broken scratch copy.

## Conclusions

The source comparison supports replacing the current Coder-path plus fork replacement with a direct
`acp-go-sdk2` dependency. The compile surface is finite, the transport behavior Magic needs is
present, and the scratch migration reached green ACP package tests after localized adaptations.

The strongest codebase benefits are not new UI features. They are removal of an unsafe field-layout
dependency, restoration of race-free structured SDK logging, direct module ownership, current
stable schema types, and generated validation that turns wire drift into explicit errors.

The strongest ACP-adherence benefit is a trustworthy baseline from which capability advertisement
can be audited. SDK2 exposes more protocol surface than Magic currently supports; the correct
response is not to advertise it all. Migrate the baseline first, prove existing providers, then add
notices, compaction, elicitation, plan extensions, and subagents only with complete daemon and phone
semantics.

The 2026-10-11 extension sharpens what "full adoption" means. The dependency migration is
necessary but not sufficient. Magic still sends a model-selection method the stable schema no
longer has, and it under-advertises a capability it already implements. It also handles several
standard update variants through a silent default. Moving those to typed SDK calls and explicit,
tested mappings leaves only the grok vendor surface as Magic-owned protocol code. That is the
smallest surface Magic can have while grok's extensions exist.

No source migration was performed by this assessment.

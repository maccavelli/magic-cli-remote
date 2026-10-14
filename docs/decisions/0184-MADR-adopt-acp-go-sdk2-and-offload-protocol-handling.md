---
status: proposed
date: 2026-10-13
decision-makers: Project Owner
consulted: none
informed: operators of ACP providers (grok today; gobble when registered); maintainers of acp-go-sdk2 and gobble-cli
---

<!-- markdownlint-disable MD013 MD024 MD060 -->

# Adopt `acp-go-sdk2` and move ACP protocol handling into the SDK

## Context and Problem Statement

Magic speaks ACP through `internal/provider/acpagent`, built on `github.com/coder/acp-go-sdk v0.13.5`.
`go.mod` replaces that module with the owner's fork, `github.com/maccavelli/acp-go-sdk v0.13.6-mcr.1`,
which adds the notification overflow policy decided in
[0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md](0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md)
(D16–D20). 0167 D20 makes leaving the fork a planned step. Its 2026-10-10 amendment (D21–D23)
commissioned
[0167-REPORT-acp-go-sdk2-integration-assessment.md](../reports/0167-REPORT-acp-go-sdk2-integration-assessment.md),
which assessed `github.com/maccavelli/acp-go-sdk2` as the successor and, in its "Full adoption"
section, listed the protocol mechanisms Magic still maintains by hand.

The owner's direction (2026-10-13): *"I want magic to offload as much as possible to the
libraries, and provide the fullest adoption possible."* This record decides what that means for
ACP. It is a separate decision from 0167, whose subject is an upstream overflow-policy
contribution. The owner chose a new record for it on 2026-10-13. Self-update and the removal of
`mcplib` are
[0054-MADR-standardize-self-update-on-go-selfupdate-lib.md](0054-MADR-standardize-self-update-on-go-selfupdate-lib.md).
The direction does not extend to `go-llmprovider-sdk`. Migrating Magic's model catalogs to it is
out of scope by
[0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md](0185-MADR-migrating-to-go-llmprovider-sdk-is-out-of-scope.md).

### What changed since the 0167 report

The report evaluated SDK `main` at `d534f27`/`f2a6a3a`, then untagged past `v0.14.0`, and made an
immutable tag its Phase 0. The SDK has since published `v0.15.0` (2026-10-11), `v0.16.0`,
`v0.17.0` (2026-10-12) and `v0.17.1` (2026-10-13). `git ls-remote` lists each tag. The schema is
still stable ACP `1.25.0`. The release notes (`acp-go-sdk2` `docs/reports/0013`, `0014`, `0017` and
`0018-REPORT-release-notes-*`) record behavior the report had expected Magic to build itself:

* **Capability gates are enforced in both directions** (`v0.17.0`). The SDK refuses to send a
  method or value the peer did not advertise, and refuses one it receives that it did not
  advertise itself. The gates cover the session methods, `fs/*`, `terminal/*`, MCP transports,
  prompt block kinds, notices, compaction updates and **boolean config options**. A response
  carrying an unadvertised value is returned to the caller as an error. `Negotiated()` and
  `Negotiation.Allows(method)` expose what `initialize` agreed.
* **`initialize` comes first, and the version is negotiated** (`v0.17.0`). Typed calls before it
  fail with `ErrNotInitialized`. A mismatched version gives a `*ProtocolVersionError`.
* **Cancellation follows ACP** (`v0.17.0`). A cancelled turn is answered
  `{"stopReason":"cancelled"}`, and a client's pending `session/request_permission` handlers are
  answered `cancelled` and have their contexts cancelled with `ErrPromptCancelled` when the
  client cancels.
* **Malformed input is answered** with `-32700` or `-32600`, and an over-long line is skipped
  instead of closing the connection (`v0.17.0`, `WithMaxMessageSize`).
* **Protocol paths must be absolute** in 17 properties, in both directions (`v0.16.0`). A stdio MCP
  server with a bare command is refused before sending. `Roots()` on the session requests returns
  the session's root set.
* **Validation errors are typed** (`*ValidationError`, `*CapabilityError`), and `ErrorCode` is an
  `int32` with a constant per code (`v0.15.0`, `v0.16.0`). Inbound hooks
  `WithInboundRewrite` and `WithInboundValidation` exist (`v0.16.0`).
* **An optional raw `null` is absent** (`v0.15.0`). `rawInput` and `rawOutput` arrive as an empty
  `json.RawMessage`. This addresses at the SDK the regression the report found in Magic's
  permission summary, as long as Magic tests `len(v) == 0`.
* **`Connection()`, `SendRequest` and the extension methods are not checked** against the
  negotiated capabilities (`v0.17.0` README, "Initialization and capabilities"). A standard method
  sent raw bypasses every gate above.

### What Magic does today (evidence)

Observed at `9778cbc17b39`, plus a scratch-copy probe on 2026-10-13.

* **The compile surface is unchanged by `v0.15`–`v0.17.1`.** Moving a scratch copy's 29 importing
  files to `github.com/maccavelli/acp-go-sdk2 v0.17.1`, with the `replace` removed, gives 25 compiler
  diagnostics in 8 files, all in `internal/provider/acpagent`. They are the report's groups: the
  stable elicitation methods on `acp.Client` (`CompleteElicitation` missing; 5 sites), named IDs
  (`AuthMethodId`, `TerminalId`; 10), unsigned numbers (usage `uint64`, file line and limit
  `uint32`, terminal output limit `uint64`, exit status `*uint32`; 7), `UnstableDeleteSession` (2),
  and `PlanId` (1). The breaking changes of `v0.15`–`v0.17` change runtime behavior, not Magic's
  compile.
* **Raw standard methods.** `session/set_model` for model selection (`session.go:962`) and for
  thinking (`thinking.go:70`), and a `session/resume` read-back (`thinking.go:77`), go through
  `rawRequest`, which reaches the connection by an `unsafe.Pointer` cast (`rawConnOf`). Schema 1.25
  has no `session/set_model`. Raw sends bypass the SDK's gates.
* **Hand-kept capability state.** `buildMcpServers` filters MCP servers by the agent's
  `mcpCapabilities` (`acpagent.go:621-651`), and `sessioncaps.go` gates session operations from
  copied flags. Both duplicate what `Negotiated()` now provides.
* **Duplicate validation.** `ReadTextFile` and `WriteTextFile` refuse a non-absolute path
  themselves (`session.go:2309-2360`). The SDK now refuses it before the handler runs.
* **A catch-all for session updates.** Unmapped variants fall to a Debug "unhandled session
  update" (`session.go:1785-1786`). Grok `1.0.13`'s committed wire capture
  (`internal/provider/grok/testdata/wire/1.0.13/frames.jsonl`) contains `session_info_update`
  titles that reach this branch and are dropped.
* **The advertisement under-reports.** `initialize` advertises `fs` and `terminal` only
  (`acpagent.go:521-527`), and no `clientInfo`. Magic maps and sends boolean config options end to
  end, phone included (0167 report, L3), but does not advertise `session.configOptions.boolean`.
  From `v0.17.0`, an agent's response that carries a boolean option is returned to Magic as an
  error. The committed grok capture contains no `configOptions` at all, so grok is not affected
  today, but any conforming agent that offers a boolean option would fail `session/new`.
* **Method-not-found is detected by literal.** `isMethodNotFound` compares `-32601`
  (`sessioncaps.go:166-169`).
* **Cancellation assumes `-32800`.** `session.go:2512-2515` maps a cancelled prompt from
  `*RequestError{Code: -32800}`. An `acp-go-sdk2` agent at `v0.17.0` or later answers
  `stopReason: cancelled` instead.
* **Only grok uses ACP** (`internal/provider/grok`). gobble, built on `acp-go-sdk2 v0.14.0`, is the
  intended second ACP agent under the proposed
  [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md) and
  [0179-REPORT-gobble-native-acp-integration-assessment.md](../reports/0179-REPORT-gobble-native-acp-integration-assessment.md).

## Decision Drivers

* Offload, as the owner directs: no ACP mechanism stays in Magic where the SDK or the stable
  schema provides one. Vendor extensions are the only Magic-owned protocol code, and they are
  isolated in the vendor's provider spec.
* Leave the fork, as 0167 D20 requires, for an immutable tag of a module the owner maintains
  under its own records.
* Keep the availability guarantee of 0167 D17–D19: drop-newest overflow, the per-turn loss notice,
  and the survival assertion.
* Advertise honestly: a capability is advertised exactly when its behavior exists, which the SDK
  now enforces in both directions.
* Do not break grok, the one ACP provider in production, while the stricter SDK meets its real
  frames.
* Prepare for gobble: after this decision, a second ACP agent needs a `Spec` and fixtures, not
  more protocol code.

## Considered Options

* **Option A — Full adoption: migrate to `v0.17.1` and offload every standard mechanism to the
  SDK** (chosen).
* **Option B — Behavior-preserving migration only:** the 0167 report's Phases 0–2. Move the
  dependency, keep the raw paths and the hand-kept capability state, and add conformance tests.
* **Option C — Stay on the fork:** keep `v0.13.6-mcr.1`, or move to `v0.13.6-mcr.2`, and wait.

## Decision Outcome

Chosen option: "**Option A — Full adoption: migrate to `v0.17.1` and offload every standard
mechanism to the SDK**", because the SDK now enforces capability gates, version negotiation,
cancellation, malformed-input handling, path rules and typed validation that Magic would otherwise
have to write and maintain. Every raw standard-method path Magic keeps is a path those guarantees
do not cover. Option B would migrate the dependency but keep exactly the surface the SDK's gates
skip, and Option C keeps a fork 0167 D20 already decided to leave.

### The decisions

**D1 — Depend directly on `github.com/maccavelli/acp-go-sdk2 v0.17.1`.** Remove the
`github.com/coder/acp-go-sdk` requirement and the fork `replace`, and change the 29 imports. A
later tag replaces `v0.17.1` only under this record's PLAN, after its release notes are read
against D4–D8. `internal/modguard_test.go` is rewritten: it rejects any `replace` of an ACP module,
any import of the Coder path, and any requirement of `acp-go-sdk2` that is not a semantic-version
tag. This executes 0167 D20 and supersedes 0167 D16 for this repository's build.

**D2 — Construct the connection with the SDK's options, and keep 0167's guarantees.** Pass
`WithNotificationOverflowPolicy(OverflowDropNewest)` and `WithLogger(<the session logger>)` at
construction, and set `WithMaxMessageSize` explicitly to the 10 MiB default so the bound is
visible in Magic's code. `ClientSideConnection.Connection()` replaces the `unsafe.Pointer` cast,
and `rawConnOf` and the `unsafe` import are deleted. The 0167 D18 per-turn loss notice and the
D19 survival assertion stay, and are re-run against the new module.

**D3 — Adapt the compile surface with explicit conversion rules, not casts.**

* A terminal output limit is clamped to `maxTerminalBuffer` before it becomes an `int`.
* A file line and limit are bounded by the file's line count before conversion.
* A usage count above `math.MaxInt` saturates at `math.MaxInt`, with a test at `math.MaxUint64`.
* An OS exit status becomes a `uint32` only when it is non-negative. Termination by a signal is
  reported in the signal field and is never turned into an exit code.
* `AuthMethodId` and `TerminalId` are converted at the boundary of Magic's string-typed
  interfaces.
* `DeleteSession` replaces `UnstableDeleteSession`, and `PlanId` replaces `Id`.

**D4 — `initialize` describes Magic exactly, and the SDK enforces it.**

* Send `clientInfo` with the name `mcremote` and the build's version (its `buildinfo` version once
  0054's S2 lands).
* Advertise `session.configOptions.boolean`. Magic already implements boolean options end to end,
  and under `v0.17.0`'s gates leaving it unadvertised turns an agent's boolean option into a failed
  `session/new`.
* Keep `fs.readTextFile`, `fs.writeTextFile` and `terminal`. Advertise nothing else until its
  behavior exists (D10).
* Read the agent's capabilities from `Negotiated()`. `sessioncaps.go`'s copied flags and
  `buildMcpServers`' transport filter use `Negotiation` and `Allows` instead of their own copies.
* A table-driven test compares the advertised block with the behavior that implements each field.
  The test is the capability-honesty invariant of the 0167 report.

**D5 — Validation and error classification belong to the SDK.**

* Magic stops re-checking what the SDK refuses before a handler runs: the absolute-path checks in
  `ReadTextFile` and `WriteTextFile` go once a wire test shows the SDK answering `-32602` for an
  empty and a relative path.
* Errors are classified with the SDK's types and constants: `errors.As` to `*ValidationError` and
  `*CapabilityError`, and `ErrorCodeMethodNotFound` in place of the `-32601` literal.
* Wire tests pin the SDK's answers at Magic's boundary: `-32700`, `-32600`, `-32602` for a missing
  required key and a non-absolute path, `-32601` for an unadvertised method, and an over-long line
  skipped without closing the connection.
* Raw values follow the SDK's rule: an empty `json.RawMessage` is absent (`len(v) == 0`), so a
  permission summary never shows `null`.

**D6 — Standard methods go through typed calls only, and vendor methods live in the vendor's
spec.**

* No `rawRequest` or `SendRequest` with a method name that does not begin with `_` remains outside
  a named vendor fallback. A test enforces it.
* Model and thinking selection use `SetSessionConfigOption` for an option whose category is
  `model` or `thought_level`, whenever the agent advertises one. The response's `configOptions`
  is the read-back, so a live session is not resumed to read its own state.
* Grok's raw `session/set_model`, its `_meta.reasoningEffort`, the `session/resume` read-back and
  the `x.ai/sessionConfig` harvesting move behind `acpagent.Spec` hooks owned by
  `internal/provider/grok`. They run only when the agent advertises no standard option. The
  committed grok `1.0.13` capture advertises none, so the fallback stays live for grok until a
  live probe shows otherwise. That probe spends provider quota and needs the owner's approval.
* `ResumeSession` is typed wherever a resume remains.
* `_x.ai/*` methods and notifications stay, as genuine extensions, in the grok spec.

**D7 — Every schema 1.25 session update is mapped or explicitly declined.** The `default:` branch
is replaced by one case per variant. A variant this record does not map (`session_info_update`,
`notice`, compaction, `plan_update`, the subagent variants, and `usage_update`'s `cost`) is
declined with a bounded, rate-limited diagnostic naming the variant. `SessionUpdate.Unknown` is
named separately. Each declined variant is mapped by its own record under D10.

**D8 — Magic follows the SDK's cancellation semantics.** A cancelled prompt is recognised by
`stopReason: cancelled` as well as by `-32800`, because grok and an `acp-go-sdk2` agent may answer
either way. When Magic cancels a turn, a pending permission request's handler context ends with
`ErrPromptCancelled`, and Magic closes the corresponding phone permission card instead of leaving
it answerable.

**D9 — Elicitation compiles, but is not offered.** `CreateElicitation` and `CompleteElicitation`
return method-not-found, and `elicitation` is not advertised. Under `v0.17.0`'s gates a conforming
agent cannot send an unadvertised mode, so the stubs exist for the interface. The behavior is
D10's.

**D10 — Capabilities are taken up one record at a time** (owner decision, 2026-10-13, consistent
with 0167 D23). Each of these gets its own MADR, with the phone behavior designed first. It is
advertised only when the daemon and phone behavior, the wire fixtures and a live probe exist. The
proposed order puts first what grok already sends:

1. session title from `session_info_update`;
2. `notice` (`session.notices`);
3. `usage_update` cost, after the accounting rule (per turn or cumulative, currency) is decided;
4. compaction (`session.compaction`);
5. elicitation, form mode first, then URL mode with a trust policy;
6. `plan_update` with `plan_removed`, then the unstable `plan` capability;
7. the unstable subagent variants, mapped onto Magic's own subagent model.

**D11 — Ask the SDK for what Magic would otherwise rebuild.** Under `acp-go-sdk2`'s own records,
not here:

* validation of outbound typed params and decoded results beyond the path rules, if `v0.17.1`
  does not already do it (to be verified in the PLAN);
* whether a second `session/prompt` should cancel the running turn (gobble's finding M10), as an
  agent-side choice.

**D12 — Magic and gobble pin the same `acp-go-sdk2` tag** and bump it together, each bump checked
by both repositories' live ACP suites. gobble is at `v0.14.0` today.

**D13 — ACP schema v2 is not tracked.** The SDK adopts a stable v2 under its own records, and Magic
follows only when an engine it runs speaks v2.

### Consequences

* Good, because the fork, its `replace` and the `unsafe` cast all go, and the dependency becomes a
  tagged module with release notes.
* Good, because capability gates, version negotiation, cancellation, malformed-input answers, path
  rules and typed validation become the SDK's, enforced on every typed message, and Magic keeps
  tests that pin them rather than code that repeats them.
* Good, because the only Magic-owned ACP code left is grok's vendor surface, behind the grok spec,
  and adding gobble becomes a `Spec`, a command table and fixtures.
* Good, because advertising boolean config options removes a latent failure: a conforming agent
  that offers a boolean option no longer fails `session/new`.
* Neutral, because 0167's drop-newest policy and loss notice carry over unchanged. The SDK fixes
  neither the single ordered consumer nor the possibility of update loss.
* Bad, because stricter decoding and gating meet grok's real frames for the first time in
  production. A frame grok sends that the schema forbids now fails where it was tolerated. Live
  probes and wire captures are therefore acceptance gates, and they spend quota.
* Bad, because the cancellation change touches the phone: permission cards must close on cancel,
  which is new client work.
* Bad, because D10 leaves grok's titles dropped until its first capability record is written, even
  though the frames already arrive.

### Confirmation

* `go list -m github.com/maccavelli/acp-go-sdk2` reports `v0.17.1`. No Go file imports
  `github.com/coder/acp-go-sdk`, `go.mod` has no ACP `replace`, and the rewritten modguard test is
  seen failing on a scratch copy that re-adds the `replace`.
* `unsafe` is absent from `internal/provider/acpagent`. A test fails if any non-`_` method reaches
  `rawRequest` or `SendRequest` outside the grok fallback.
* The stalled-pump survival test (0167 D19) is seen failing with the overflow option removed, and
  passes with it. The loss notice precedes turn completion once per affected turn.
* The capability-honesty test fails when a field is advertised without its behavior. The
  initialize wire test pins `clientInfo` and `session.configOptions.boolean`.
* A fixture agent that sends a boolean option: `session/new` fails before D4 and succeeds after
  it.
* Each D3 conversion has a boundary test at the type's maximum.
* Wire tests show the SDK's `-32700`, `-32600`, `-32602` and `-32601` answers at Magic's boundary,
  and a permission summary with no `null`.
* A fixture agent with `model` and `thought_level` options produces `session/set_config_option`
  frames for `/model` and `/thinking`. A fixture without them exercises the grok fallback only.
* A table test covers every schema 1.25 session-update variant plus `Unknown`. None reaches a
  silent default.
* `make live-grok` passes at acceptance, with wire captures of initialize, new, load, resume,
  prompt, permission, cancel, terminal, filesystem and delete.

## Pros and Cons of the Options

### Option A — Full adoption

* Good, because each mechanism Magic deletes is replaced by one the SDK tests in its own conformance
  suite.
* Good, because raw sends, the only path the SDK's gates do not see, shrink to vendor methods.
* Neutral, because the vendor fallback for grok remains real code, moved rather than removed.
* Bad, because it is several phases of work, and phases D6 and D10 depend on probes and on product
  design.

### Option B — Behavior-preserving migration only

* Good, because it is the smallest change that leaves the fork.
* Bad, because the raw `session/set_model` and `session/resume` paths, the hand-kept capability
  copies and the duplicate validation remain, which is the surface `v0.17.0` documents as
  unchecked.
* Bad, because the boolean-option failure under `v0.17.0`'s gates must be fixed anyway, so B is
  not actually behavior-preserving at `v0.17.1`.

### Option C — Stay on the fork

* Good, because nothing changes.
* Bad, because 0167 D20 already decided to leave, and upstream Coder has merged nothing since
  `v0.13.5`.
* Bad, because every conformance fix since `v0.14.0` stays out of Magic, and gobble and Magic stay
  on different SDKs.

## More Information

* Implementation:
  [0184-PLAN-adopt-acp-go-sdk2-and-offload-protocol-handling.md](0184-PLAN-adopt-acp-go-sdk2-and-offload-protocol-handling.md).
* SDK evidence, by repository and path, not by link: `acp-go-sdk2` tags `v0.14.0`–`v0.17.1`;
  `docs/reports/0013-REPORT-release-notes-v0-15-0.md`, `0014-REPORT-release-notes-v0-16-0.md`,
  `0017-REPORT-release-notes-v0-17-0.md`, `0018-REPORT-release-notes-v0-17-1.md`;
  `docs/decisions/0015-MADR-validate-absolute-protocol-paths.md`,
  `0017-MADR-remediate-acp-v1-conformance-violations.md`; `schema/version` (`1.25.0`).
* Records this one relates to: 0167 (D16 superseded for the build; D17–D19 kept; D20 executed; D23
  applied as D10); 0179 and its report (gobble, which this record prepares for but does not
  register); [0039-MADR-grok-acp-parity.md](0039-MADR-grok-acp-parity.md) D1 and
  [0106-MADR-grok-1.0.5-surface-parity.md](0106-MADR-grok-1.0.5-surface-parity.md), whose raw `session/set_model` paths D6 moves into the
  grok spec as a fallback. Those two records get pointer amendments when D6 lands.
* **Open, not decided here:** whether the `fs/*` callbacks should be confined to the session's
  `Roots()` (for example with `os.OpenRoot`). The SDK makes it possible. Today the callbacks act
  with the daemon user's permissions, as decided in
  [0069-MADR-macos-permissions-and-sandbox-parity.md](0069-MADR-macos-permissions-and-sandbox-parity.md)
  D4. Confinement would change what an agent can do, so it needs its own record.

## Amendment — 2026-10-13: the program record's findings and the owner's answers

[0186-MADR-integrate-current-library-releases-and-prepare-gobble.md](0186-MADR-integrate-current-library-releases-and-prepare-gobble.md)
(its D4) asked for these amendments. The owner's answers to its questions Q3 and Q7 add two more.
This record stays `proposed`. Where an item below contradicts a decision above, the item governs,
and the original text stays as first proposed.

* **D12's fact has changed.** gobble pins `acp-go-sdk2 v0.17.1` (and `go-selfupdate-lib v1.15.2`)
  as of 2026-10-13. Both repositories are on the same tag today, so the one-pin rule already holds.
  D11's request about a second `session/prompt` stands.
* **`v0.17.1` ships known defects. Each is accepted with a mitigation, deleted at `v0.18.0`** (0186
  D6). The SDK's `0019-MADR-remediate-debugging-pass-findings.md` (accepted 2026-10-13) fixes them
  in `v0.18.0`, which is not tagged.
  * **B16:** a write ignores its context. Magic's close stops relying on the context to bound
    `CloseSession`: a timer that does not wait for the write starts the process-tree kill.
  * **B4:** `CloseSession` leaves pending permission requests open. Closing a session ends them,
    and closes their phone cards, as D8 does on cancel.
  * **B9:** a typed call is written even after its context has ended. The turn and close paths
    check the context first.
  * **B19:** local errors lose their identity. Classify by code constants and `errors.As` until
    `v0.18.0`, then by `errors.Is`.
  * **B1:** a request sent from a notification handler deadlocks. `SessionUpdate` keeps sending
    none, and a comment at the handler says why.
* **The `v0.18.0` bump is a named later phase,** taken only after its release notes are read
  against D4–D8 and the list above. It removes exported identity `MarshalJSON` methods, adds
  `Close`, the connection hand-off and error unwrapping, and gates extension messages received
  before `initialize`.
* **The host path rule applies to every phase** (0186 D5).
  * From `v0.16.0`, the SDK refuses a received path that the host's `filepath.IsAbs` rejects.
  * Every test path that reaches the SDK is absolute on the running host, and a replayed wire
    capture's root is rewritten to the host's on read. No test is skipped on Windows.
  * A1 is not done until the `go-native` `windows/amd64` CI leg and `make ci-windows` pass
    ([0145-MADR-local-windows-ci-style-tests.md](0145-MADR-local-windows-ci-style-tests.md)).
* **D4's client capabilities become per-`Spec`** (owner answer Q3).
  * The `fs.readTextFile`, `fs.writeTextFile` and `terminal` advertisement is a field of
    `acpagent.Spec`. grok's `Spec` keeps all three.
  * gobble's `Spec`, added by
    [0179-MADR-gobble-native-acp-provider.md](0179-MADR-gobble-native-acp-provider.md) after A2,
    advertises none of them.
  * The capability-honesty test covers each `Spec`.
* **Quota for A3's live grok probe and acceptance `make live-grok` is pre-approved** (owner answer
  Q7). Other live runs still need approval.

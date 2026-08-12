# Implement MADR 0080 — First-class Codex collaboration modes and app-server command parity

<!-- markdownlint-disable MD004 MD013 MD024 MD029 MD033 MD036 -->

Associated MADR: [0080-MADR-add-first-class-codex-collaboration-modes-and-app-server-command-parity.md](0080-MADR-add-first-class-codex-collaboration-modes-and-app-server-command-parity.md)

## Goal

Implement every required decision D1–D21 in MADR 0080, in its mandated
dependency order, so a managed Codex session exposes truthful, independently
stateful controls for collaboration and autonomy plus the required `/diff`,
`/goal`, `/fast`, `/personality`, `/review`, and `/fork` operations.

The implementation is complete only when:

1. `/plan` uses Codex app-server collaboration RPCs and never prompt emulation;
2. Plan/Default and permission/autonomy modes remain independent in provider,
   daemon, protocol, persistence, cache, and Flutter state;
3. every required command is advertised from typed runtime capability state,
   never from a provider-name branch or literal slash forwarding;
4. app-server settings converge after resume and engine replacement;
5. old clients and persisted records remain readable through additive wire and
   store changes;
6. all phase-specific unit, integration, race, static-analysis, Flutter, and
   live acceptance tests described below pass; and
7. the implementation contains no ranked follow-on from MADR 0080.

MADR 0080 is `Accepted`; D1–D21 (including the 2026-08-12 review lock-ins)
are therefore locked. Review of this plan may refine execution detail, but a
change to an architectural decision requires revising the MADR before code.

## Codebase and CLI Baseline

This plan is grounded at repository commit
`4b1185256f78d9d7b35baa0d3a11a72157f35a08` and installed `codex-cli 0.147.0`.
Re-run the baseline checks at implementation start and record drift before
editing behavior.

| Area | Current fact | Consequence for implementation |
| --- | --- | --- |
| Codex initialization | `internal/provider/codex/provider.go` sends `initialize.capabilities.experimentalApi:false`. `Provider` already increments `generation`, but `engine` stores only process/connection/death state and no negotiated capabilities. Live 0.147 accepts `true`; the rejection payload was not observed. | P1 adds the D2 classifier, `rpcErrorBody.Data`, and generation-bound latches before Plan is exposed. |
| Codex RPC seam | `internal/provider/codex/conn.go` owns request IDs, `sendRequest`, `rpcErrorBody` (`Code`, `Message` only), and the read pump. | New methods use this seam. P1 adds `Data json.RawMessage`. Downgrade decisions inspect structured JSON-RPC code/message/data, not error strings except for the documented `no rollout found` normalization. |
| Provider modes | `provider.ModeSession` and Codex `SetMode` mean approval/sandbox autonomy. | They remain unchanged; collaboration gets a separate interface and event. |
| Start and persisted state | `provider.StartOptions`, `session.Meta`, and `session.Record` lack collaboration, autonomy selection, service tier, and personality fields. | Add optional fields before features that must survive resume/fork. |
| Session state | Codex session state tracks busy turn, approval/sandbox, thinking, and model, but not collaboration, Fast, personality, goals, diff fallback, or review routing. | Each feature adds lock-protected state in its dependency phase. |
| Commands | The canonical registry has daemon/mode/operation/native/none kinds; `/goal` is native, `/plan` uses existing modes, and there are no `/permissions`, `/fast`, `/personality`, `/review`, or `/fork` specs. | P3 adds one collaboration kind and typed operations, then every provider table explicitly maps every new spec. |
| Command echo | `Manager.Prompt` returns before assembling attachments when `runCanonical` reports `handled`. `runCanonical` always echoes first. | Codex inline `/plan <prompt>` must thread original blocks through `Manager.Prompt` so attachments survive and the user text appears once. KindMode `/plan` is unchanged. |
| Events | `event.TypeMode` carries autonomy modes; no collaboration or goal event exists. | Add distinct control events and cover them in `event.Types`, `IsControl`, protocol docs, history, and replay tests. |
| WebSocket | `session.set_mode`, `session.fork`, and `session.diff` exist. Diff returns only `summary`; fork accepts optional `message_id`. | Add collaboration/settings operations additively; extend diff/fork payloads without breaking old clients. |
| Diff | Manager/provider `DiffSession` returns a string. Codex has no implementation. The server's outbound limit is 1 MiB. | Introduce a structured bounded result, adapt all current implementations, and cap Codex patches at 256 KiB before history or wire serialization. |
| Fork | Codex sends optional `turnId`; the installed schema names the field `lastTurnId`. Manager preserves only model/thinking/CWD when constructing the child. | P5 corrects the field and carries all daemon-owned settings through the existing ownership lifecycle. |
| Model catalog | Codex decodes model choices but discards `serviceTiers`, `defaultServiceTier`, and `supportsPersonality`. | P6 retains typed metadata and makes command availability model-dependent. |
| Review items | entered/exited review items render generic notices; the fallback review body is not decoded. | P8 adds a single busy review lifecycle and output deduplication. |
| Mobile | `SessionMode` and `_ModeSelector` represent only autonomy. `TranscriptCache._save` deletes an entry when `items.isEmpty`; `hydrateFromCache` bails on empty items. `_maybeApplyDefaultMode` calls `session.set_mode` with `SettingsStore.getDefaultSessionMode`, and the settings floor includes `plan`. Diff/fork menu visibility uses provider-name checks. | P4 persists control-only snapshots, ignores a stored `plan` default on Codex, and later phases use remote commands instead of provider names. |
| Tests | Existing focused Codex/command/session/protocol/event tests pass. `live_codex` currently mixes no-turn and token-bearing tests. | Preserve the green baseline, then split live tags so ordinary protocol acceptance cannot spend model tokens. |

The installed CLI exposes schema generation through:

```text
codex app-server generate-ts --out <directory>
codex app-server generate-ts --experimental --out <directory>
```

Generated trees are temporary evidence only. Check in minimal redacted JSON
fixtures and explicit assertions, not generated TypeScript.

## Scope

### Existing areas expected to change

* `internal/provider/provider.go` for typed optional capability interfaces,
  structured diff/fork types, and additive start options.
* `internal/provider/codex/` for negotiation, decoded schemas, provider/session
  state, request construction, notification handling, live probes, and tests.
* `internal/provider/{fake,httpagent,opencode,kilo}/` and their tests only as
  required to adapt shared `DiffSession` and `ForkSession` contracts and make
  explicit command mappings.
* `internal/command/` for canonical specs, the collaboration command kind,
  typed operations, dynamic availability reasons, and conformance tests.
* `internal/session/` for command execution, runtime state, persistence,
  replay, settings convergence, diff/fork lifecycle, goals, and tests.
* `internal/event/`, `internal/protocol/`, and `internal/ws/` for additive wire
  events/operations, stable errors, authenticated dispatch, and coverage tests.
* `apps/mobile/lib/data/{protocol,chat,ws}/`,
  `apps/mobile/lib/state/transcripts_notifier.dart`,
  `apps/mobile/lib/features/chat/chat_screen.dart`, and
  `apps/mobile/lib/features/settings/settings_screen.dart` for independent
  controls, cache/reconnect behavior, Codex default-mode ignore, and
  goal/diff/review/fork presentation.
* `internal/command/conformance_test.go` to register kilo, accept
  `KindCollaborationMode`, and extend `knownOps`.
* Corresponding Go and Flutter test files, `docs/protocol-v1.md`, the Codex
  capability matrix/comments, and `Makefile` live-test targets.

### Files to add

Names may be split further only when package organization requires it; their
responsibilities are fixed.

* `internal/provider/codex/collaboration.go` and
  `internal/provider/codex/collaboration_test.go` — strict catalog decoding,
  mode request construction, setting reconciliation, and probe state.
* `internal/provider/codex/settings.go` and
  `internal/provider/codex/settings_test.go` — model metadata, tier, and
  personality request/effective-state handling.
* `internal/provider/codex/live_turn_test.go` — token-bearing tests moved
  off the `live_codex` tag.
* `internal/provider/codex/goal.go`, `diff.go`, `review.go` and focused tests —
  typed RPC adapters and bounded state.
* `internal/provider/codex/testdata/0.147.0/` — minimal, redacted request,
  response, notification, and catalog fixtures named by method and direction.
* New focused Flutter tests, created in the phase that needs them:
  `apps/mobile/test/session_diff_fork_test.dart`,
  `apps/mobile/test/fast_personality_commands_test.dart`,
  `apps/mobile/test/session_goal_test.dart`,
  `apps/mobile/test/session_review_test.dart`.
  Production behavior stays in the current protocol/state/screen layers.

### Explicit non-goals

* `/status`, `/rename`, `/skills`, apps/MCP diagnostic pickers, background
  terminals, Guardian retry, memories, plugin or hook mutation, cloud tasks,
  and environment switching.
* Forwarding Codex TUI slash commands, copying Codex Plan prompts, scraping TUI
  output, or sending host-global administrative actions from the phone.
* Detached review threads, arbitrary caller-selected diff paths, or a new
  cross-provider semantic definition of Plan.
* A cartesian-product mode id or any change to `SessionMode.dangerous`.
* Checking generated app-server schema trees into the repository.
* Pushing commits. Each phase creates a local commit; pushing requires a later
  explicit instruction.

## Fixed Interfaces and State Model

These contracts make the phases deterministic. Equivalent package-local naming
is acceptable only if wire names, invariants, and behavior remain identical.

### Provider capability contracts

Add these domain types and optional interfaces to `internal/provider/provider.go`:

```go
type CollaborationMode struct {
	ID          string
	Name        string
	Description string
}

type CollaborationModeSession interface {
	CollaborationModes() ([]CollaborationMode, string, error)
	SetCollaborationMode(context.Context, string) error
}

type ServiceTier struct {
	ID          string
	Name        string
	Description string
}

type ServiceTierSession interface {
	ServiceTiers() ([]ServiceTier, string, error)
	SetServiceTier(context.Context, *string) error
}

type PersonalitySession interface {
	Personalities() ([]string, *string, error)
	SetPersonality(context.Context, *string) error
}

type Goal struct {
	Objective   string
	Status      string
	TokenBudget *int64
	TokensUsed  int64
	TimeUsedSeconds int64
	CreatedAt   int64
	UpdatedAt   int64
}

type GoalSession interface {
	Goal(context.Context) (*Goal, error)
	SetGoal(context.Context, GoalMutation) (*Goal, error)
}

type ReviewSession interface {
	Review(context.Context, ReviewTarget) error
}
```

`GoalMutation` is a closed action enum (`replace`, `edit`, `pause`, `resume`,
`clear`) with an objective only for replace/edit. `ReviewTarget` is a tagged
value with exactly `uncommittedChanges`, `baseBranch`, `commit`, and `custom`;
constructors reject invalid empty values before any provider call.

Decode the generated goal status as the closed set `active`, `paused`,
`blocked`, `usageLimited`, `budgetLimited`, and `complete`. Retain
`timeUsedSeconds`, `createdAt`, and `updatedAt` as well as token usage. A goal is
“continuable” for Plan/fork safety when its state is active, blocked,
usage-limited, or budget-limited; paused and complete goals do not autonomously
continue. `/goal` with no current goal reports that fact. Edit/pause/resume with
no goal fail locally; clear is idempotent and may call the provider. Resume is
valid only from paused. User input never directly selects an engine-only status.

Replace the string-only diff contract with:

```go
type DiffResult struct {
	Summary   string
	BaseSHA   string
	Scope     string
	Truncated bool
}

type DiffSession interface {
	Diff(context.Context, string) (DiffResult, error)
}
```

`Scope` is `working_tree` for `gitDiffToRemote` and `latest_codex_turn` for the
notification fallback. Existing providers set `Summary` and may leave additive
metadata empty.

Replace the fork string contract with:

```go
type ForkOptions struct {
	LastTurnID            string
	DeferGoalContinuation bool
}

type ForkResult struct {
	AgentSessionID string
	ForkedFromID   string
}

type ForkSession interface {
	Fork(context.Context, ForkOptions) (ForkResult, error)
}
```

Adapt existing providers mechanically: their current message boundary maps to
`LastTurnID`, unsupported defer behavior returns a typed unsupported error, and
their returned native id populates `AgentSessionID`.

### Persisted settings

Add these optional values to `provider.StartOptions`, `session.Meta`, and
`session.Record` using lower-snake-case JSON names and `omitempty`:

```text
mode_id
collaboration_mode_id
service_tier
personality
```

`mode_id` persists the autonomy selection that Manager currently holds only in
memory. Empty collaboration means `default`; empty service tier means off;
empty personality means provider default. Old records decode to those defaults.
Do not persist catalog descriptions, developer instructions, diff bodies, or
review output.

Codex session lock-protected state additionally retains:

* validated collaboration catalog and confirmed current id;
* explicit user thinking preference separately from effective preset effort;
* effective model;
* typed model catalog metadata and confirmed service tier/personality;
* current goal and goal capability state;
* the latest aggregate `turn/diff/updated` patch keyed by turn id;
* review busy/turn/assistant-output state; and
* engine generation plus per-method supported/unsupported/unknown probe state.

Never hold the session mutex across a blocking RPC. Snapshot request state
under lock, issue the request, then reconcile only if the session and engine
generation still match.

`rpcErrorBody` gains `Data json.RawMessage`. The D2 initialize retry fires
only when initialize returns a JSON-RPC error whose `message` or stringified
`data` matches `(?i)experimental`. Transport EOF, timeout, cancel, spawn/exec
failure, an already-false initialize, and any other JSON-RPC error do not
retry.

Every `turn/start` that has collaboration support is this object, plus later
P6 fields:

```text
threadId, input
approvalPolicy, sandboxPolicy          # unchanged
model                                  # effective current model
collaborationMode.mode
collaborationMode.settings.model
collaborationMode.settings.reasoning_effort
collaborationMode.settings.developer_instructions = null
```

While Plan is active, omit top-level `effort`. On Default, omit top-level
`effort` when the user has no explicit preference; otherwise send the stored
preference both nested and top-level. `/thinking` during Plan updates the
stored preference only and emits “applies when you leave Plan”; the next Plan
turn still uses the catalog preset.

Use the MADR stable-reason table verbatim. P3 Codex placeholders use
`integration not wired` and must not contain `TODO`.

### Events and protocol

Add two event types:

* `collaboration_mode`: full discovery has `collaboration_modes` plus
  `current_collaboration_mode_id`; later updates may carry only the current id.
* `session_goal`: `goal` contains the bounded current goal; JSON `null` means
  clear.

Add each to `event.Types`, control-event classification, JSON coverage,
history/replay, and `docs/protocol-v1.md`. Never put developer instructions or
raw goal objectives in diagnostic logs.

Add this authenticated WebSocket operation:

* `session.set_collaboration_mode` with `{session_id, mode_id}`.

Keep existing `session.diff` and `session.fork` operation names. Extend diff
results additively with `base_sha`, `scope`, and `truncated` while retaining
`summary`. Interpret the existing fork `message_id` as the requested turn
boundary for old clients and add the clearer `last_turn_id` alias; reject a
payload that supplies both with different values.

The collaboration setter and existing fork require current ownership and enter
the appropriate WebSocket async/mutating dispatch set. Goal, Fast, personality,
and review remain canonical slash operations through the existing authorized
prompt/command path; do not add redundant direct WebSocket verbs for them.
Diff keeps its existing session authorization.

Use stable protocol errors:

```text
collaboration_mode_unsupported
collaboration_mode_invalid
set_collaboration_mode_failed
```

Busy mutations reuse `turn_busy`; existing command notices and fork/diff errors
remain stable. Provider method-not-found becomes an unavailable command
capability, not an internal error. Error data/notices may include a safe
actionable reason but never raw RPC input.

### Canonical command model

Add `KindCollaborationMode` and typed operations `goal`, `service_tier`,
`personality`, `review`, and `fork`; keep `diff`. `SessionState` gains the
collaboration catalog/current id and a per-operation availability/reason map.
Resolution requires both the optional interface and live runtime prerequisites.

Canonical specs and Codex mappings are fixed:

| Command | Codex mapping | Grammar |
| --- | --- | --- |
| `/plan` | `KindCollaborationMode` → `plan` | Codex only: bare/on enter; off/exit/stop restore default and reject leftover tokens; every other non-empty remainder is inline prompt text. KindMode providers keep MADR 0022 usage grammar. |
| `/mode` | autonomy mode | list when bare; otherwise existing mode id |
| `/permissions` | autonomy mode | same handler as `/mode`; Codex table only. Not a global alias and not a Goose mapping in this change. |
| `/diff` | diff operation | no path or other argument |
| `/goal` | goal operation | view; objective; `edit objective`; `pause`; `resume`; `clear` |
| `/fast` | service-tier operation | bare toggles; `on`; `off` |
| `/personality` | personality operation | bare lists; `friendly`; `pragmatic`; `none` (enum, not JSON null) |
| `/review` | review operation | bare/`uncommitted`; `base branch`; `commit sha`; `custom instructions` |
| `/fork` | fork operation | whole thread when bare; optional turn id |

Non-Codex rows are the MADR mapping table, not implementer choice. Update
`knownOps`, add kilo to `conformance_test.go`, and accept
`KindCollaborationMode` in the same change.

### Manager invariants

* Collaboration and autonomy are separate fields and setters.
* Mode-changing, goal-mutating, review, and fork operations check busy state
  before provider RPC and do not queue hidden work.
* Entering Plan is rejected while a continuable goal exists. Creating/resuming
  a goal is rejected while Plan is active. View, pause, clear, and editing an
  already paused goal remain available in Plan. These goal constraints are
  implemented in P7, not P2.
* State changes are confirmed only after successful provider response or an
  authoritative settings notification; failure retains prior state.
* A capability/catalog/settings/model change recomputes and emits
  `remote_commands` only when its value changes.
* Codex `/plan <prompt>` is the only command that consumes attachments from
  `Manager.Prompt`. Refactor that function so a handled slash no longer
  returns before `attachments` are visible to the handler. Other commands
  ignore attachments. KindMode `/plan` remains usage-help for unknown
  remainder (`/plan sideways`).
* Bare Codex `/plan`/`on` while already in Plan is a same-mode no-op. Codex
  `/plan <prompt>` while already in Plan skips the settings RPC and submits.
  `/plan off leftover` is usage; no mode change.
* `/thinking` during Plan stores the preference and notices that it applies
  when leaving Plan.
* Fork children are created through Manager ownership/persistence, inherit all
  settings, and cause exactly one source-session notice with local child id and
  name.

## Implementation Steps

### Test-first and commit contract for every phase

Execute P0–P9 strictly in order. Do not begin a phase while the prior phase has
uncommitted changes or a failing required gate.

For P1–P8, run the phase's focused-verification commands before editing as the
baseline, then run the same commands after implementation as the focused green
gate. P0 and P9 use their explicitly listed verification commands.

For each phase:

1. run its listed baseline before editing;
2. write the named tests and fixtures before production code;
3. run the narrowest new test and observe a failure caused by the missing
   behavior (compile failure is acceptable; environment failure is not);
4. implement only that phase;
5. run focused tests, then every listed phase regression gate;
6. for every implementation phase, run `staticcheck ./...` and
   `go test -race ./...` from the repository root; do not substitute the
   package-focused commands for these full pre-commit gates;
7. inspect `git diff --check`, `git diff --stat`, and `git status --short`;
8. before staging Go, run `make pre-add-check FILES="<every changed Go file>"`;
9. before staging Dart, run `dart format` on changed Dart files and then
   `dart format --output=none --set-exit-if-changed .` from `apps/mobile`;
   also run `make preflight` from the repository root for every Dart phase;
10. stage only phase files and run `git diff --cached --check`;
11. run `git commit` with no `-m`, `-F`, or other message option so the
    repository hook supplies the commit message; and
12. verify the commit with `git show --stat --oneline HEAD` and confirm the
    worktree contains only the next phase's intended edits.

The implementation log must record the red test command and expected failure,
green commands, generated Codex version, and commit hash for every phase.

## Phase P0 — Freeze the reviewed artifacts and implementation baseline

### Objective

Make the decision executable without silently changing its scope.

### Steps

1. Confirm MADR 0080 remains `Accepted` and its relative link resolves to this
   plan; do not change D1–D21.
2. Re-run `codex --version`; if it is not 0.147.0, regenerate temporary normal
   and experimental schemas and revise evidence in the MADR before code.
3. Capture `git rev-parse HEAD` in the implementation log. If code has drifted
   from this plan's baseline, map changed seams and amend this plan first.

### Verification and commit

```text
git diff --check
git status --short
```

Commit only the accepted MADR and reviewed plan. This documentation commit is
the prerequisite for P1, not an implementation claim.

## Phase P1 — Protocol fixtures and experimental capability negotiation

Implements D1 and D2 and required-sequence item 1. It must not advertise Plan.

### Tests and fixtures to write first

1. Add minimal 0.147.0 JSON fixtures for:
   * experimental initialize request/success/rejection;
   * `collaborationMode/list` request, measured success, missing params error,
     method-not-found, and experimental-gate rejection;
   * valid Plan/Default catalog, additive unknown fields, empty id, duplicate
     id, absent required modes, and invalid reasoning effort;
   * `thread/settings/update` request/response and
     `thread/settings/updated` notification.
2. Add connection/provider tests proving:
   * initialize first sends `experimentalApi:true`;
   * `rpcErrorBody` decodes `data`;
   * a JSON-RPC initialize error whose message or data matches
     `(?i)experimental` causes exactly one fresh-process retry with false;
   * transport EOF, timeout, context cancellation, executable failure, and
     unrelated JSON-RPC initialize errors do not retry;
   * the collaboration list request always carries `params:{}`;
   * the read-only probe executes exactly once per engine generation;
   * replacement increments generation and resets probe latches;
   * method-not-found/gate rejection/malformed catalog disables only
     collaboration and stores the frozen experimental-unavailable or
     catalog-invalid reason; and
   * unknown notifications remain ignored without panic.
3. Add strict decoder tests for all catalog fixtures. Unknown additive fields
   pass; invalid required shapes fail without retaining a partial catalog.

### Production steps

1. Add negotiated `experimental` and assigned generation fields to the Codex
   engine wrapper. Reuse `Provider.generation` as the monotonic source and keep
   method capability latches on the engine so a process replacement cannot
   inherit stale results.
2. Refactor `startEngine` into one launch/initialize attempt plus an orchestrator
   that may kill and reap the rejected process before one non-experimental
   relaunch. Never reuse the rejected connection.
3. Add `Data json.RawMessage` to `rpcErrorBody`. Classify rejection with the
   D2 `(?i)experimental` matcher on message and stringified data. Log method,
   Codex version, generation, and the frozen reason; do not log raw request
   bodies.
4. After either successful initialization path, issue
   `collaborationMode/list` once with an explicit empty params object. Decode a
   successful response to an immutable catalog and require unique non-empty
   `plan` and `default` ids; classify the expected method/gate rejection on the
   downgraded path as collaboration-only unavailability.
5. Store supported/unavailable plus reason for that engine generation and
   expose it internally to sessions. Do not add commands/events yet.

### Focused verification

```text
go test ./internal/provider/codex -run 'Test(Experimental|Initialize|CollaborationCatalog|CollaborationProbe|EngineGeneration)'
go test -race ./internal/provider/codex
staticcheck ./internal/provider/codex
```

Then run the phase commit contract and commit P1 alone.

## Phase P2 — Provider collaboration state and per-turn convergence

Implements D3, D6, and D7 and required-sequence item 2. It may emit provider
events to tests, but daemon command exposure waits for P3.

### Tests to write first

1. Add compile-time interface assertions and tests for
   `CollaborationModeSession` discovery/current-state behavior.
2. Request-construction tests prove:
   * Plan uses effective current model, catalog preset effort, and
     `developer_instructions:null`, and omits top-level `effort`;
   * Default restores the user's explicit thinking selection or JSON null and
     also sends null developer instructions;
   * `/thinking` during Plan updates stored preference only and does not
     change the next Plan `turn/start` preset;
   * approval/sandbox/autonomy fields are unchanged;
   * the model and stored user thinking preference are not overwritten by a
     collaboration preset; and
   * every later `turn/start` matches the D6 payload after switch, resume,
     and simulated engine replacement.
3. State tests cover valid switch, same-mode idempotence, invalid local id with
   no RPC, failed update retaining prior state, authoritative
   `thread/settings/updated` reconciliation, malformed notification, and
   Default normalization.
4. Concurrency tests cover close, cancel, and engine loss during update and
   reject stale responses from a replaced generation without deadlock, waiter
   leak, post-close emission, or state resurrection.
5. Sequential tests switch collaboration and autonomy in both orders and prove
   neither mutates the other.

### Production steps

1. Add the provider types, optional interface, and additive `StartOptions`
   fields defined above.
2. Retain the validated catalog/current id, explicit thinking preference,
   effective model, and engine generation under the Codex session mutex.
3. Seed new sessions to Default; seed resumed sessions from the additive
   `StartOptions` value supplied by Manager, falling back to Default only when
   absent/invalid for the current catalog. P3 wires that option to persistence.
4. Implement a pure full-settings builder. Use the catalog mask as a template,
   overwrite model with effective model, select Plan preset effort or restored
   user effort for Default, and force developer instructions to null.
5. Implement `SetCollaborationMode`: snapshot and reject when the session is
   busy; do **not** implement goal constraints here (P7). Call
   `thread/settings/update`, commit only on success for the same engine
   generation, and surface a typed unavailable/invalid/provider error.
6. Decode `thread/settings/updated` as authoritative effective state. Ignore
   developer instruction contents for remote state and logs.
7. Extend `runTurn` to emit the D6 `turn/start` payload on every turn:
   approval/sandbox, effective model, full `collaborationMode` settings, and
   top-level `effort` only on Default when the user has an explicit
   preference.

### Focused verification

```text
go test ./internal/provider/codex -run 'Test(Collaboration|Build.*Settings|TurnStart.*Collaboration|SettingsUpdated)'
go test -race ./internal/provider/codex
staticcheck ./internal/provider/codex
```

Run the phase commit contract and commit P2 alone.

## Phase P3 — Daemon command registry, persistence, events, and WebSocket

Implements D4, D5, D8, D9, D11, D12, D21 and required-sequence item 3.

### Tests to write first

1. Extend command conformance tests with every required canonical spec,
   operation, provider-table entry, and known operation. Register kilo in
   `conformance_test.go`. Accept `KindCollaborationMode` in the kind switch.
   Assert:
   * Codex `/plan` resolves through collaboration state;
   * Codex `/mode` and `/permissions` resolve through autonomy state;
   * Grok/OpenCode/Kilo Plan mappings remain `KindMode` with 0022 usage
     grammar (`/plan sideways` still usage help);
   * Goose `/permissions` is `KindNone` with the existing Goose `/mode` note;
   * every other provider follows the MADR mapping table; and
   * unavailable reasons match the frozen table, including
     `integration not wired` for unwired Codex ops, with no `TODO` substring.
2. Add command manager table tests for Codex Plan bare/on/off/exit/stop,
   leftover after off, already-in-Plan no-op vs inline submit, invalid id,
   busy state, `/mode plan` directing to `/plan`, and `/thinking` during
   Plan storing the preference with an “applies when you leave Plan”
   notice.
3. Add Codex inline Plan tests with text-only and attachment blocks through
   `Manager.Prompt`. Assert mode RPC precedes prompt unless already in Plan,
   a failed mode RPC sends no prompt, attachments survive, successful input
   appears once, and it bypasses recursive slash parsing. Leave
   `internal/session/mode_test.go` KindMode cases unchanged.
4. Add event/protocol JSON tests for full and current-only collaboration state,
   event classification, old JSON without fields, and documented errors.
5. Add persistence tests proving all four optional settings load from old/new
   records, survive restart/resume, and preserve separate axes.
6. Add WebSocket authorization/dispatch tests for
   `session.set_collaboration_mode`, invalid/busy/provider failures, response
   correlation, and command re-advertisement on capability arrival/failure.
7. Add resolver tests proving command events emit once per changed resolution,
   not on identical provider notifications.

### Production steps

1. Add the command kind, operations, session-state fields, availability reasons,
   specs, descriptions, dispatch arms, and explicit provider mappings. Add
   `OpFork` to `knownOps` and `commandContext` (`sess.(provider.ForkSession)`).
   OpenCode/Kilo `/fork` dispatch through existing `Manager.Fork` in this
   phase. Codex `/fork` stays `integration not wired` until P5.
2. Replace Codex's stale capability notes only for operations made real in each
   later phase. In this phase, announce unimplemented required operations with
   the frozen reason `integration not wired`; do not claim app-server lacks
   them and do not write `TODO`. Keep `TestCodexAdvertisesNoPlanMode` green;
   update only its comment.
3. Refactor `Manager.Prompt` so command handlers receive the original
   attachments. Only Codex inline `/plan <prompt>` consumes them. Preserve the
   existing echo for all commands except that inline path; direct inline
   submission uses the ordinary authorized prompt path once after successful
   mode change, or immediately when already in Plan. KindMode `/plan` dispatch
   stays `cmdPlan` with 0022 grammar.
4. Add collaboration catalog/current fields to manager entries. Merge full and
   current-only provider events, recompute commands, and emit the separate
   collaboration event.
5. Add optional persisted fields to Meta/Record/writePersist/Create/resume.
   Thread the autonomy `mode_id` through start/resume even though current
   behavior previously kept it only in memory.
6. Add protocol payloads/results/errors and authenticated WebSocket dispatch.
   Add mutating async classification and preserve response ordering.
7. Document exact event merge semantics, operation payload, errors, old-client
   behavior, and no-developer-instruction rule in `docs/protocol-v1.md`.
   Rewrite the sentences that treat `plan` as a universal `session_mode` id
   and that enable `/plan` from `session_mode` alone. Codex `/plan` is
   enabled from the collaboration event.

### Focused verification

```text
go test ./internal/command
go test ./internal/event ./internal/protocol
go test ./internal/session -run 'Test(Plan|Mode|Permissions|Persist|RemoteCommands|Collaboration|Thinking)'
go test ./internal/ws -run 'Test.*(Collaboration|SetMode|Command|Authorization)'
go test -race ./internal/command ./internal/event ./internal/protocol ./internal/session ./internal/ws
staticcheck ./internal/command ./internal/event ./internal/protocol ./internal/session ./internal/ws
```

Run the phase commit contract and commit P3 alone.

## Phase P4 — Flutter collaboration state, controls, and reconnect behavior

Implements D10 and the client portions of D9/D11; required-sequence item 4.

### Tests to write first

1. Add model tests for collaboration JSON parsing, equality, full/current-only
   values, and coexistence with `SessionMode`.
2. Add reducer tests proving a current-only collaboration event retains the
   catalog, a full event replaces it, clear/reset is deterministic, and autonomy
   events never alter collaboration.
3. Extend `test/transcript_cache_test.dart` so itemless snapshots persist.
   Change `TranscriptCache._save` so an empty `items` list no longer deletes
   the entry when collaboration/autonomy/goal control state is present.
   Change `hydrateFromCache` so `cached.items.isEmpty` is not a bail-out when
   control fields exist. Both axes survive cache hydration, history replay,
   resync, reconnect, and route disposal. Assert first authoritative live
   state wins over stale cache.
4. Add WebSocket client tests for `session.set_collaboration_mode`, stable error
   propagation, and no local optimistic mutation.
5. Add widget tests for:
   * separate Plan/Default and Permissions/autonomy controls;
   * Plan hidden for old-daemon/no-capability fixtures;
   * Plan disabled while busy/offline;
   * provider failure retaining prior display;
   * danger confirmation only for autonomy;
   * Codex Permissions labeling while Grok/OpenCode/Goose behavior remains;
   * a stored default session mode of `plan` ignored on Codex and still
     applied on Grok/OpenCode/Kilo (`test/default_session_mode_test.dart`);
   * semantics, predictive back, narrow screen, and large text accessibility.
6. Change diff/fork action-menu tests in `test/chat_render_test.dart` (or the
   current menu test file) to use advertised remote commands, not provider
   names, while retaining current visibility until P5 implementations are
   advertised.

### Production steps

1. Add `CollaborationMode` and event fields in protocol models. Keep
   `SessionMode`, equality, and `dangerous` untouched.
2. Add independent transcript fields and reducer merge semantics. Use an
   explicit nullable sentinel in `copyWith` so goal/control clears cannot be
   mistaken for “preserve old value.”
3. Extend `transcript_cache.dart` serialization to retain catalogs/current ids
   and allow a control-only snapshot. Reconcile cached state beneath
   replay/live state rather than overwriting newer data.
4. Add the client set operation and rely on daemon events for displayed state.
5. Render a separate Plan/Default chip or toggle beside the autonomy selector
   in `chat_screen.dart`. Label Codex autonomy as Permissions. Never apply
   the dangerous confirmation to collaboration changes.
6. In `_maybeApplyDefaultMode` and `settings_screen.dart`, ignore a stored
   default of `plan` when the session provider is Codex. Do not add a
   collaboration default-mode setting.
7. Gate Plan and session actions from reducer state and `remote_commands`;
   remove provider-string branching for diff/fork once those commands are
   advertised.

### Focused verification

From `apps/mobile`:

```text
flutter test test/transcript_reducer_test.dart test/transcript_cache_test.dart test/history_replay_test.dart test/resume_flow_test.dart
flutter test test/mcremote_client_test.dart test/mode_selector_dangerous_test.dart test/default_session_mode_test.dart
flutter test test/chat_render_test.dart test/session_synchronizer_test.dart test/resolve_displayed_mode_test.dart
dart format --output=none --set-exit-if-changed .
flutter analyze
flutter test
```

Run `make preflight` from the repository root, then the phase commit contract
and commit P4 alone.

## Phase P5 — Bounded working-tree diff and managed fork

Implements D15 and D20 and required-sequence item 5.

### Tests to write first: diff

1. Provider tests assert `gitDiffToRemote` receives only the resolved session
   CWD and never a command/user path; returned SHA accepts exactly 40 or 64
   hexadecimal characters.
2. Cover empty/full patches, invalid SHA, multi-byte UTF-8 at 256 KiB, clipping
   back to a valid newline, explicit truncation text, and final serialized
   envelope below the 1 MiB limit.
3. Decode `turn/diff/updated` as aggregate replacement keyed by turn, not
   append. On `-32601`, latch the compatibility method unavailable once per
   engine generation and return the latest cached patch with
   `scope=latest_codex_turn`, or an actionable unavailable error.
4. Adapt every existing `DiffSession` implementation/test to `DiffResult` and
   prove old wire clients still receive `summary`.

### Tests to write first: fork

1. Assert whole-thread fork omits both boundary fields and boundary fork sends
   `lastTurnId` only. Assert unknown boundary errors do not silently fork all.
2. Cover busy rejection before RPC, `no rollout found` normalization, engine
   error, returned `forkedFromId`, and direct serialization of the optional
   defer-goal flag. P7 adds the manager's active-goal selection of that flag.
3. Manager tests prove same-device ownership, new local session registration,
   one source notice with child local id/name, and child inheritance of CWD,
   model, thinking, autonomy, collaboration, service tier, and personality.
4. Adapt every existing `ForkSession` implementation/test to the structured
   request/result without changing its proven behavior.
5. Protocol/mobile tests cover optional `last_turn_id`, conflict with legacy
   `message_id`, structured diff metadata, fenced patch rendering, base SHA,
   unmistakable truncation, and opening the returned child session. Add
   `apps/mobile/test/session_diff_fork_test.dart` for the new rendering and
   navigation contract; extend `test/mcremote_client_test.dart` for the
   additive wire fields.

### Production steps

1. Add the structured provider contracts and adapt fake/httpagent/OpenCode/Kilo
   implementations before changing Manager call sites.
2. Implement the Codex diff RPC, SHA validation, UTF-8/newline clipper, and
   bounded notification cache. Apply the cap before history or notice emission.
3. Extend Manager/protocol/mobile diff results additively; keep `summary`
   authoritative for compatibility.
4. Implement Codex fork with `lastTurnId`; never serialize `turnId`. Translate
   only the specific never-materialized error into “nothing to fork yet.”
5. Extend Manager Fork to build the child through existing `Create`/resume
   ownership and persist every setting. Validate the returned native source id.
6. Carry the optional defer-goal field in the provider contract and serialize
   it only when requested and experimentally negotiated. Until P7 introduces
   manager goal state, Manager always requests false.
7. Map `/diff` and `/fork` to the now-live typed operations and re-advertise.
   Remove the corresponding “not wired” reasons.

### Focused verification

```text
go test ./internal/provider/codex -run 'Test(Diff|Fork|TurnDiff|Clip)'
go test ./internal/provider/fake ./internal/provider/httpagent ./internal/provider/opencode ./internal/provider/kilo
go test ./internal/session -run 'Test(Diff|Fork)'
go test ./internal/protocol ./internal/ws -run 'Test.*(Diff|Fork)'
go test -race ./internal/provider/... ./internal/session ./internal/ws
staticcheck ./internal/provider/... ./internal/session ./internal/ws
```

From `apps/mobile`:

```text
flutter test test/mcremote_client_test.dart test/chat_render_test.dart test/session_diff_fork_test.dart
flutter analyze
flutter test
```

Run `make preflight` from the repository root, then the phase commit
contract and commit P5 alone.

## Phase P6 — Catalog-driven Fast and model-gated personality

Implements D17 and D18 and required-sequence item 6.

### Tests to write first

1. Decode `serviceTiers`, `defaultServiceTier`, and `supportsPersonality` from
   minimal model fixtures while ignoring additive fields. Reject empty tier ids
   and duplicate rows named exactly `Fast`.
2. Resolve the active model deterministically: explicit session model, provider
   configured model, then catalog default. Never scan another model's tiers.
3. Fast tests cover list/status, bare toggle, on/off, repeat idempotence,
   exact-name lookup, opaque id serialization, JSON null clear, effective
   null/empty/default normalization, unsupported model, and provider failure.
4. Personality tests cover list, friendly/pragmatic/none, invalid values,
   unsupported model, and provider failure. `/personality none` serializes
   the enum `none`. There is no JSON-null clear and no `/personality default`
   alias.
5. For both settings, cover immediate `thread/settings/update` confirmation when
   experimental is available and the stable next-turn-only path otherwise.
6. Assert subsequent `turn/start` carries confirmed tier/personality without
   changing model, thinking, collaboration, permissions, goal, or review state.
7. Model-switch tests clear unsupported overrides, persist effective state, and
   emit one changed `remote_commands` event with deterministic reason.
8. Mobile command/autocomplete/client tests in
   `test/mcremote_client_test.dart` and a new
   `test/fast_personality_commands_test.dart` add/remove commands on model
   changes and never retain stale Fast/personality actions. `/personality
   none` is the enum, not a JSON-null clear.

### Production steps

1. Retain typed model metadata in the Codex provider catalog instead of
   flattening it into picker rows only. Rebuild both typed and picker views from
   one decoded source.
2. Add lock-protected tier/personality state and the provider interfaces. Fast
   resolution compares display name exactly to `Fast` and sends its opaque id;
   never hard-code `priority`.
3. Implement common settings-update reconciliation with generation checks.
   When experimental settings update is unavailable, confirm daemon state for
   the next turn and emit an explicit “applies next turn” notice.
4. Include stable `serviceTier` and `personality` fields on every turn start.
5. Revalidate after model changes and settings notifications. Persist queried
   effective state, clear unsupported values, and recompute commands.
6. Add Manager command handlers, map `/fast` and `/personality`, and remove only
   their now-obsolete unavailable notes. They continue through slash-command
   routing; no direct mobile/WebSocket setting operation is added.

### Focused verification

```text
go test ./internal/provider/codex -run 'Test(ModelMetadata|ServiceTier|Fast|Personality|ModelSwitch|TurnStart.*Settings)'
go test ./internal/command ./internal/session ./internal/protocol ./internal/ws -run 'Test.*(Fast|Personality|RemoteCommands|Model)'
go test -race ./internal/provider/codex ./internal/session ./internal/ws
staticcheck ./internal/provider/codex ./internal/session ./internal/ws
```

From `apps/mobile`:

```text
flutter test test/mcremote_client_test.dart test/fast_personality_commands_test.dart
flutter analyze
flutter test
```

Run `make preflight` from the repository root, then the phase commit
contract and commit P6 alone.

## Phase P7 — Goals, hydration, and Plan safety matrix

Implements D16 and required-sequence item 7.

### Tests to write first

1. Decode goal set/get/update/clear responses and updated/cleared notifications,
   all generated statuses, token budget/usage, and additive fields.
2. Parser tests cover view, replace, edit, pause, resume, clear, empty objective,
   4,000 Unicode scalar multi-byte success, and 4,001-scalar rejection using
   `utf8.RuneCountInString` semantics.
3. Busy tests prove every mutation makes no RPC while a turn is active; view
   remains read-only.
4. Exhaustively table-test the Plan/goal matrix:
   * active goal rejects Plan entry;
   * Plan rejects create/replace and resume;
   * view, pause, clear, and edit of an already-paused goal are allowed;
   * editing an active goal in Plan is rejected; and
   * failure changes neither state.
5. Resume tests call `thread/goal/get`, publish one bounded event, clear stale
   state when absent, and never infer goals from transcript text.
6. Log-capture tests prove objectives are absent from provider/manager logs and
   errors.
7. Protocol/mobile tests cover event/update/clear, cache hydration/reconnect,
   all statuses, bounded objective rendering, usage, and old-daemon absence.
   Add `apps/mobile/test/session_goal_test.dart` and extend
   `test/transcript_reducer_test.dart` plus `test/transcript_cache_test.dart`.

### Production steps

1. Add `Goal`, `GoalMutation`, and `GoalSession`; implement Codex goal RPCs and
   notification decoding.
2. Add manager entry goal state and `session_goal` events. Hydrate with
   `thread/goal/get` after start/resume without blocking initial session use;
   reconcile responses using session/generation identity.
3. Implement exact slash grammar and scalar validation. Only engine
   notifications/responses may set blocked/usage-limited/budget-limited/complete.
4. Enforce the safety matrix in Manager and recheck in the Codex provider to
   close command/WebSocket races.
5. Complete D20's goal-aware fork behavior now that goal state exists: active
   goals request `deferGoalContinuation:true` only when negotiated; otherwise
   Manager rejects before RPC with pause/clear guidance. Add both manager and
   provider tests here.
6. Add the goal event/docs and wire client/reducer/cache/UI goal state. Render a
   compact goal card with safe truncation and status/usage; mutations continue
   through slash-command routing.
7. Map `/goal` to the typed operation and remove its stale native/unwired note.
   Re-advertise only on actual capability change.

### Focused verification

```text
go test ./internal/provider/codex -run 'TestGoal'
go test ./internal/command ./internal/session ./internal/event ./internal/protocol ./internal/ws -run 'Test.*Goal'
go test -race ./internal/provider/codex ./internal/session ./internal/ws
staticcheck ./internal/provider/codex ./internal/session ./internal/ws
```

From `apps/mobile`:

```text
flutter test test/session_goal_test.dart test/transcript_reducer_test.dart test/transcript_cache_test.dart test/mcremote_client_test.dart
flutter analyze
flutter test
```

Run `make preflight` from the repository root, then the phase commit
contract and commit P7 alone.

## Phase P8 — Inline review turn and output deduplication

Implements D19 and required-sequence item 8.

### Tests to write first

1. Parser/request fixtures cover bare/uncommitted, base branch, commit SHA with
   null title, and custom instructions; reject empty/missing arguments locally.
2. Assert every request uses `delivery:"inline"` and preserves all session
   settings.
3. Busy lifecycle tests prove review shares normal turn ownership, rejects a
   concurrent prompt/review, supports interrupt/cancel, and restores idle on
   success, error, disconnect, and cancellation.
4. Routing tests handle a returned `reviewThreadId` by temporarily aliasing it
   to the managed session until terminal cleanup; notifications for unrelated
   thread ids remain ignored.
5. Item tests render entered/exited lifecycle notices. Track whether assistant
   review text was emitted; use `exitedReviewMode.review` exactly once only when
   it was not, and never append the fallback to normal output.
6. Assert review changes none of collaboration, autonomy, model, thinking,
   Fast, personality, or goal state.
7. Protocol/mobile tests keep cancel/stop reachable, render lifecycle once, and
   avoid duplicate final output. Add `apps/mobile/test/session_review_test.dart`
   and extend `test/chat_render_test.dart`.

### Production steps

1. Add `ReviewTarget` and `ReviewSession`; parse grammar into the four exact
   app-server target shapes.
2. Refactor normal turn busy acquisition/release into a shared helper used by
   prompt and review without changing existing prompt semantics.
3. Implement `review/start`, generation-safe thread alias routing, terminal
   cleanup, cancellation, and item fallback deduplication.
4. Add the Manager command operation, event/item documentation, mobile
   lifecycle state, and `/review` mapping. Keep review on slash-command routing
   and remove the final required-command unwired note.
5. Re-run command conformance and assert no required command can fall through
   to literal native slash forwarding.

### Focused verification

```text
go test ./internal/provider/codex -run 'TestReview'
go test ./internal/command ./internal/session ./internal/protocol ./internal/ws -run 'Test.*Review'
go test -race ./internal/provider/codex ./internal/session ./internal/ws
staticcheck ./internal/provider/codex ./internal/session ./internal/ws
```

From `apps/mobile`:

```text
flutter test test/session_review_test.dart test/chat_render_test.dart
flutter analyze
flutter test
```

Run `make preflight` from the repository root, then the phase commit
contract and commit P8 alone.

## Phase P9 — Full regression, live acceptance, and documentation closure

This phase implements no new behavior. It confirms all decisions and leaves
follow-ons explicitly out of scope.

### Split live-test contracts

1. Keep `live_codex` strictly no-model-turn. Move these tests to
   `internal/provider/codex/live_turn_test.go` with
   `//go:build live_codex_turn`:
   * `TestLiveThreadLifecycle`
   * `TestLiveTurnPlanUpdatedNotSkipped`
   * `TestLiveTurnStartSandboxPolicyShape`
   * `TestLiveModePoliciesAreAccepted`
   Leave under `live_codex`: `TestLiveInitializeConnect`,
   `TestLiveStartSession`, `TestLiveModelList`,
   `TestLiveImageInputAdvertised`, `TestLiveCodexListModelsWithoutThread`,
   `TestLiveThreadStartSandboxShape`, and the new no-turn 0080 suite.
   Add `make live-codex-turn` beside `make live-codex`. After the split,
   `go test -tags live_codex ./internal/provider/codex` must not compile the
   four moved tests.
2. Add the MADR's no-turn sequence using a temporary `CODEX_HOME` and temporary
   Git repository. Select models/tiers from returned catalogs; do not hard-code
   inventory. Assert no `turn/start` lifecycle occurred.
3. Add `live_codex_review` as a separate explicitly opt-in, bounded-timeout,
   token-bearing target. Record version/model, run one minimal custom inline
   review, verify lifecycle/output deduplication, and clean up.
4. Live tests may skip only for an absent Codex binary or the repository's
   explicit live-test environment contract. Protocol drift on an acceptance
   host fails.

### Temporary schema verification

Generate both schema trees into a directory created by `mktemp -d`. Automated
assertions must prove collaboration/settings are experimental and goal,
review, fork, diff, tier, and personality shapes are in the normal surface.
Remove the temporary directory after inspection; do not stage it.

### Decision traceability audit

Before acceptance, complete this matrix in the implementation log with test
names and commit hashes:

| Decision | Owning phase | Required evidence |
| --- | --- | --- |
| D1–D2 | P1 | negotiation, fixture, authoritative-RPC tests |
| D3, D6–D7 | P2 | independent state, request construction, convergence/concurrency tests |
| D4–D5, D8–D9, D11–D12, D21 | P3 | command, inline prompt, wire, persistence, resolver tests |
| D10 | P4 | reducer/cache/reconnect/control widget tests |
| D15, D20 | P5 | bounded diff/fallback and managed fork tests |
| D17–D18 | P6 | catalog-driven Fast/personality and model-switch tests |
| D16 | P7 | grammar, hydration, redaction, and safety-matrix tests |
| D19 | P8 | inline review lifecycle/routing/deduplication tests |
| D13–D14 | P9 | scope audit showing no follow-on or host-global command landed |

### Final verification commands

Run focused package tests first, then all repository gates:

```text
go test ./internal/provider/codex ./internal/command ./internal/session ./internal/event ./internal/protocol ./internal/ws
make pre-add-check
staticcheck ./...
go test -race ./...
make preflight
make live-codex
```

Run the token-bearing suites only with explicit acceptance-host authorization:

```text
make live-codex-turn
make live-codex-review
```

Then:

1. run the exact detailed-acceptance scenarios from MADR 0080;
2. verify old-client fixtures ignore additive events and old-daemon fixtures
   hide unsupported controls;
3. verify the app-server and WebSocket scanners/envelopes remain below their
   limits with a maximal diff and goal;
4. run `git diff --check`, inspect every changed-file stat, and confirm no
   generated schema, temporary home, repository, log, or objective leaked;
5. update only factual capability matrices/comments/protocol docs and link test
   evidence from MADR 0080's Confirmation section; and
6. run the phase commit contract and create the final local acceptance commit.

Do not push.

## Verification

The following matrix supplements the per-phase commands and P9 acceptance
sequence; all cells must be green before implementation is complete.

| Feature | Unit/provider | Manager/protocol | Flutter | Live |
| --- | --- | --- | --- | --- |
| Experimental negotiation | true then bounded false retry; per-generation probe | unavailable reason drives resolver | hidden when absent | both schemas plus collaboration list |
| Plan/Default | full settings masks, notifications, every turn | grammar, busy, inline once, persistence | separate non-optimistic control/cache | Plan then Default, no model turn |
| Permissions/autonomy | unchanged approval/sandbox | `/mode` and Codex `/permissions` same path | dangerous confirmation unchanged | existing provider behavior |
| Diff | CWD-only RPC, SHA, cap, fallback latch | structured additive response | patch/base/truncation rendering | temp repository tracked+untracked |
| Fork | `lastTurnId`, defer flag, errors | ownership, inheritance, source notice | child navigation | materialized rollout and invalid boundary |
| Fast | typed tier lookup, opaque id, turn field | grammar/persistence/re-advertise | command/client state | catalog-selected set/clear |
| Personality | model gate, enum, turn field | grammar/persistence/re-advertise | dynamic autocomplete | supporting-model set/clear if present |
| Goal | RPC/status decoding, hydration | grammar/busy/Plan matrix/redaction | event/cache/card/usage | paused set/get/update/clear |
| Review | typed targets, busy, routing, dedupe | operation/errors/no state mutation | lifecycle/cancel/no duplicate | one opt-in inline review |

## Rollout and Rollback

### Rollout

1. Land phases as separate local commits in the order above. Do not cherry-pick
   a UI/command phase without its provider and protocol dependencies.
2. Wire changes are additive. A new daemon may serve an old phone; unknown
   events/fields are ignored. A new phone hides controls when an old daemon does
   not advertise them.
3. Do not ship a phone build that *requires* collaboration events, goal events,
   or the new commands before the daemon commit that emits them.
4. Runtime capability truth is the feature gate. An unavailable experimental
   collaboration method disables Plan only; a removed diff compatibility
   method falls back for that engine generation; model metadata independently
   gates Fast/personality.
5. Start with ordinary unit/race/Flutter gates. Run no-turn live acceptance on
   the target Codex host. Run token-bearing turn/review tests only after the
   user explicitly authorizes cost and external model activity.
6. Observe safe version-bearing capability logs and command advertisements
   during one create, resume, engine replacement, reconnect, and model switch.
   Never enable a command by config/provider name to work around a failed probe.

### Rollback

1. Revert phase commits from newest to oldest (`git revert` each hash, or
   reset an unpushed local branch). Do not revert P3 protocol docs while
   leaving P4 expecting the new event.
2. Additive wire fields and events mean an old phone against a new daemon
   continues to use autonomy modes and chat. Rolling the daemon back removes
   Plan/Fast/personality/goal/review/fork advertisements; the new phone must
   hide those controls from missing `remote_commands` rather than error.
3. Persisted `mode_id`, `collaboration_mode_id`, `service_tier`, and
   `personality` are `omitempty`. An older daemon ignores unknown JSON keys
   on `Record`. After rollback, a session stored as `auto` resumes via
   `seedPolicy` as today; that is the pre-0080 behavior, not data loss.
4. Do not delete session records to roll back. Do not run a migration.

## Implementation log

| Phase | Baseline / red | Green | Codex | Commit |
| --- | --- | --- | --- | --- |
| P0 | `HEAD=4b1185256f78d9d7b35baa0d3a11a72157f35a08`; no seam drift; `codex-cli 0.147.0` | `git diff --check`; status only the two 0080 docs | 0.147.0 | `d289bef` |
| P1 | experimental negotiation red compile | `TestInitialize*`, 0.147.0 fixtures | 0.147.0 | `f369813` |
| P2 | collaboration state + turn payload | `TestTurnStartCollaboration*`, `TestCollaboration*` | 0.147.0 | `c005184` |
| P3 | commands/events/persist/WS | `TestCollaborationPlan*`, resolver + persist tests | 0.147.0 | `a965f6b` |
| P4 | Flutter Plan/Permissions | collaboration reducer/cache/widget tests | 0.147.0 | `9db75fa` |
| P5 | bounded diff + managed fork | `TestDiff*`, `TestFork*`, `session_diff_fork_test.dart` | 0.147.0 | `ee428bc` |
| P6 | Fast + personality | `TestModelMetadata*`, `TestSetServiceTier*`, `TestSetPersonality*` | 0.147.0 | `384c102` |
| P7 | goals + Plan matrix | `TestGoal*`, `TestGoalPlanMatrix*`, `session_goal_test.dart` | 0.147.0 | `5df120a` |
| P8 | inline review | `TestReview*`, `TestReviewCommand*`, `session_review_test.dart` | 0.147.0 | `8272c9a` |
| P9 | live split + docs closure | `Test0080ScopeLeavesFollowOnsUnimplemented`, `TestLive0080NoTurnSurface` (live_codex) | 0.147.0 | (this commit) |

### Decision traceability

| Decision | Owning phase | Evidence |
| --- | --- | --- |
| D1–D2 | P1 `f369813` | `TestInitialize*`, `testdata/0.147.0/initialize-*` |
| D3, D6–D7 | P2 `c005184` | `TestCollaboration*`, `TestTurnStartCollaboration*` |
| D4–D5, D8–D9, D11–D12, D21 | P3 `a965f6b` | command/resolver/persist/WS tests, `TestProvidersDeclareEveryCanonicalCommand` |
| D10 | P4 `9db75fa` | collaboration reducer/cache/widget tests |
| D15, D20 | P5 `ee428bc` | `TestDiff*`, `TestFork*`, `TestManagerFork*` |
| D17–D18 | P6 `384c102` | `TestModelMetadata*`, `TestFast*`, `TestSetPersonality*` |
| D16 | P7 `5df120a` | `TestParseGoalMutation`, `TestGoalPlanMatrix`, `TestGoalLogsOmitObjective` |
| D19 | P8 `8272c9a` | `TestReview*`, `TestReviewFallback*`, `TestCodexRequiredCommandsAreNotNative` |
| D13–D14 | P9 (this commit) | `Test0080ScopeLeavesFollowOnsUnimplemented` |

## Rollback

Rollback is phase-aware and preserves user data:

1. Revert local commits in reverse order, stopping before the first bad phase.
   Do not use destructive reset commands.
2. For a runtime protocol regression, first disable only the affected command
   through its capability latch/reason; autonomy and ordinary prompts must
   continue.
3. A downgrade daemon ignores optional persisted fields. Do not delete or
   rewrite session records; `omitempty` fields remain harmless for later
   re-upgrade.
4. A downgrade phone ignores additive events/result fields. The retained
   `summary` diff field and existing `message_id` fork input preserve its wire
   path.
5. If experimental initialization itself destabilizes launch, revert P1's
   negotiation commit or force the already-designed single non-experimental
   retry. Do not add an unreviewed global escape hatch.
6. After any rollback, run the focused tests for the reverted phase plus
   `go test -race ./...` and `make preflight`, then record which capability is
   unavailable and why.

## Review Checklist

Reviewers should approve this plan only if every answer is yes:

* Does the phase order exactly match MADR 0080's required sequence?
* Are collaboration and autonomy independent at every layer?
* Is every required command backed by an RPC or bounded daemon operation, never
  literal slash forwarding?
* Are runtime capability and model metadata, rather than provider names,
  authoritative for advertisement and UI?
* Do settings survive resume, reconnect, engine replacement, model switch, and
  fork without optimistic drift?
* Are diff size, goal text, developer instructions, paths, and review output
  handled within the documented security/privacy bounds?
* Does each phase write failing tests first, run all required gates, and produce
  one reviewable local commit?
* Are live no-turn and token-bearing suites separated so cost cannot be incurred
  by ordinary tests?
* Are D1–D21 covered with no ranked follow-on smuggled into acceptance scope?

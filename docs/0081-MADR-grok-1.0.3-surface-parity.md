---
status: accepted
date: 2026-08-12
decision-makers: Project Owner (scope and acceptance); Implementer (measurement)
consulted: none
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Adopt grok 1.0.3's measured CLI and ACP surface

## Context and Problem Statement

The grok provider last pinned its CLI/ACP contract against **0.2.112**
([0038](./0038-MADR-grok-acp-parity-assessment.md) /
[0039](./0039-MADR-grok-acp-parity.md)) and **0.2.114**
([0050](./0050-MADR-grok-cli-surface-drift.md),
[0052](./0052-MADR-thinking-levels-and-settings.md)). The binary on this
host is now **grok 1.0.3 (`1a29d5bc12d4`) [stable]** at
`/Users/saxsmith/.local/bin/grok`. That is a major-version jump, not a
patch.

This record asks: **which newly exposed 1.0.3 surfaces should the daemon
adopt, and which should stay out of scope?** The binary is the source of
truth. Official docs
([docs.x.ai/build](https://docs.x.ai/build/overview),
[CLI reference](https://docs.x.ai/build/cli/reference),
[headless](https://docs.x.ai/build/cli/headless-scripting),
[settings](https://docs.x.ai/build/settings)) and the installed user guide
(`~/.grok/docs/user-guide/`) are supporting evidence only; where they
disagree with `grok --help` or a live ACP probe, the binary wins.

The remote already speaks grok as `grok <globals> agent --no-leader stdio`
(`internal/provider/grok/grok.go` `defaultArgs`). That argv placement
contract from MADR 0050 is **still valid** on 1.0.3: every policy flag
still belongs before `agent`; placing `--permission-mode`, `--sandbox`,
`--tools`, `--allow`/`--deny`, `--no-subagents`, or `--disable-web-search`
after `agent` still fails with `unexpected argument`.

What changed is the *rest* of the surface: models, reasoning rungs,
session RPCs, session `_meta`, slash-command catalog, and a set of new
CLI subcommands. Several of those are now live on the wire and either
unmodeled or modeled under a stale name.

## Decision Drivers

* The live binary, not release notes, is the contract (AGENTS.md; MADR 0050 D2).
* Adopt only surfaces that change remote session behaviour or operator
  control. TUI-only flags and pager chrome stay out.
* Do not ship a flag or RPC whose effect on `agent stdio` has not been
  measured. `--tools` / `--allow` / `--deny` remain documented no-ops for
  ACP sessions (MADR 0050 Phase E); new flags start in the same bucket.
* A hardcoded enum or catalog that grok no longer serves becomes a picker
  lie. Live `initialize._meta.modelState` is the primary catalog; the
  static list is only the no-session floor.
* Mid-session thinking stays spawn-only until a probe *discriminates*
  success from grok's silent-accept trap (MADR 0052 §2.2).
* Official docs that name flags or subcommands the installed binary
  rejects are recorded as docs drift, not as work.

## Considered Options

* Measure 1.0.3 and adopt the daemon-relevant surfaces in a phased
  backlog, leaving TUI-only and unpinned schemas out
* Implement every newly advertised CLI flag, subcommand, and ACP method
* Defer all uptake until the next incident (a flag move, a missing model,
  a hung update check)

## Decision Outcome

Chosen option: "Measure 1.0.3 and adopt the daemon-relevant surfaces in a
phased backlog, leaving TUI-only and unpinned schemas out", because the
binary exposes a small set of remote-visible gaps (default model,
`xhigh`, a mis-registered catalog notification, a recommended
`--no-auto-update` spawn flag, and new session `_meta` / session RPCs)
and a much larger TUI surface that would not change what the phone can
do.

This MADR is accepted. Implementation is
[0081-PLAN-grok-1.0.3-surface-parity.md](./0081-PLAN-grok-1.0.3-surface-parity.md)
(Complete, 2026-08-12).

Implementation outcomes:

* P1.1–P1.5, P3.12 (`/review`), P3.10 (`/loop`): **done**.
* P2.6 `session/new` `_meta`: **measured, not taken.** baseline requested
  permission and did not write; `yoloMode` and `autoMode` requested no
  permission, did not finish the prompt in 90s, and did not write.
* P2.8 `_x.ai/session/fork`: **measured, not taken.** All four listed
  shapes failed with `missing field newCwd`. Plan forbade guessing a
  fifth field; `/fork` stays `KindNone`.
* P2.7 `session/close`: already wired; live `Close` succeeds.
* P4: not taken. T-P4 pins argv.

### Recommended uptake (proposed, not implemented)

Priorities are product impact against a *measured* 1.0.3 wire. Each
item names the code that is currently wrong or silent.

#### P1 — correctness of what we already claim to expose

1. **Put `grok-4.6` on the static floor.**
   `internal/provider/grok/grok.go` `staticModels` is still
   `grok-4.5`, `grok-code-fast-1`, `grok-4`. Live `grok models` and
   `initialize._meta.modelState` on this host are **`grok-4.6` (default)
   and `grok-4.5`**. `session/set_model` accepts those two ids and
   rejects `grok-code-fast-1` and `grok-build` with
   `unknown model id`. The live catalog from `initialize` already
   replaces the static list once any session starts (MADR 0039 D2,
   `acpagent.go` `modelsToCatalog`); the stale floor is what
   `models.list` returns before that. Drop `grok-code-fast-1` and
   `grok-4` from the floor, or keep them only because
   `AllowCustom: true` already lets an older install type them.

2. **Register the live models-update method name.**
   1.0.3 emits `_x.ai/models/update` (slash; no `id` — a notification).
   `grok.go` still registers `_x.ai/models_update` (underscore). That
   mismatch is in the 0039 implementation record itself. Even with the
   name fixed, delivery still depends on the SDK routing extension
   *notifications* into `HandleExtensionMethod`
   (`acpagent/extensions.go:80-94`). The first catalog is safe because
   it is parsed from `initialize`; mid-session refresh is the gap.
   Register the slash name (keep the underscore as an alias if a live
   test ever sees it) and pin the method string in a `live_grok` test.

3. **Pass `--no-auto-update` on daemon-spawned grok.**
   Official headless docs tell ACP and `-p` users to pass
   `--no-auto-update` so the agent does not check for updates in the
   background. On 1.0.3 the flag is **global** (accepted before `agent`,
   rejected after it — same 0050 placement rule). `defaultArgs` does
   not emit it. A grok that decides to self-update under `mcremote` is
   an operational hazard the docs already warn about.

4. **Treat `xhigh` as a live grok-4.6 rung, still spawn-only.**
   `initialize` for `grok-4.6` advertises
   `reasoningEfforts: xhigh, high, medium, low`. `grok-4.5` is still
   `high, medium, low`. `modelsToCatalog` already copies the array and
   `picker.thinkingRank` already orders `xhigh`. No parser change is
   required for the list to appear after a live initialize.
   `SetThinkingLevel` must stay `ErrThinkingLevelFixed`
   (`acpagent/session.go:764-768`): `session/set_model` with
   `reasoningEffort: "xhigh"` still returns
   `{"_meta":{"model":{"Ok":"grok-4.6"}}}` — the same silent-accept
   trap MADR 0052 recorded. Discriminate with a follow-up that reads
   the applied effort (session `_meta.x.ai/sessionConfig` selected
   option, or a `_x.ai/session_notification` `model_changed` that
   carries `reasoning_effort`) before claiming mid-session `/thinking`.
   Grok marks **both** `xhigh` and `high` `default: true` on 4.6;
   `NormalizeThinkingLevels` keeps the first default after
   cheapest-first sort, which is `high`, matching
   `modelState.reasoningEffort: "high"` and MADR 0052 D3 (a global
   default must not select `xhigh`).

5. **Re-pin the live suite to 1.0.3.**
   `live_argv_test.go` still uses `Model: "grok-4.5"` only. Add a
   `grok-4.6` row and a `ReasoningEffort: "xhigh"` row so the next
   flag move or rejected rung fails the same way 0050's test was
   meant to. Refresh comments that still say 0.2.112 / 0.2.114.

#### P2 — new session-scoped ACP that is real and unused

6. **`session/new` `_meta` options (docs + live accept).**
   The installed agent-mode guide lists `rules`,
   `systemPromptOverride`, `agentProfile`, `yoloMode`, `autoMode` on
   `session/new`. Live 1.0.3 accepted `_meta.yoloMode: true`,
   `_meta.autoMode: true`, and `_meta.rules: "…"` without error.
   The daemon today sends only `{Cwd, McpServers}`
   (`acpagent.go:770-773`). This is the first *per-session* path grok
   has offered for always-approve and for grok-native classifier auto,
   which MADR 0049 had to synthesize because `--permission-mode` is
   process-wide. **Do not replace 0049 yet.** Accepting the field is
   not the same as changing permission behaviour. A live
   discrimination pair (write-a-file under `autoMode` vs not) is the
   gate, the same standard 0050 used for `--permission-mode`.

7. **`session/list`, `session/resume`, `session/close`.**
   `agentCapabilities.sessionCapabilities` is no longer empty: 1.0.3
   advertises `{list:{}, resume:{}, close:{}}`. Live:
   `session/list` returns grok's own session roster (id, cwd, title,
   updatedAt, `_meta.x.ai/session` facets);
   `session/close` returns `{_meta: {x.ai/closeOutcome: "closed"}}`;
   `session/resume` requires `cwd` and then returns the same `models`
   block as `session/new`. The daemon already has its own session
   directory and uses `session/load` (gated on `loadSession`, which is
   still true). Adopting grok's `session/list` as the phone's session
   list would mix grok-local TUI sessions with mcremote sessions.
   **Recommended:** use `session/close` on provider `Close` if a live
   test shows it releases grok-side state that process-kill does not;
   do **not** replace the daemon session roster with `session/list`.

8. **`_x.ai/session/fork` exists; `/fork` is still `KindNone`.**
   `commandtable.go:54` maps `fork` to `ReasonNoFork`. The method
   exists. The first two param shapes failed (`missing field sourceCwd`
   after `sourceSessionId` was supplied). Pin the full request before
   remapping `/fork`. This is the first plausible grok fork since 0038
   ruled the TUI `/fork` out.

9. **`session/new` now returns `models` at the top level** and
   `_meta.x.ai/sessionConfig` (models *and* effort rungs as
   `category: "model" | "mode"`), `_meta.x.ai/sessionDetail`, and
   `_meta.x.ai/schedulerBackgroundLoops: true`.
   `emitConfigOptions` only sees the typed SDK `ConfigOptions`, which
   1.0.3 still does not populate. Do not feed `sessionConfig` into the
   mode chip: grok put effort rungs in `category: "mode"`, which would
   collide with Build/Plan/auto. If anything consumes this block, it
   belongs on the thinking picker, not `session/set_mode`.

#### P3 — slash-command catalog growth

10. **`/loop` is advertised over ACP and has no canonical spec.**
    Live `available_commands_update` includes `loop` with hint
    `[interval] <prompt>`. Session `_meta` says
    `x.ai/schedulerBackgroundLoops: true`. This is the same class of
    gap 0039 closed for `deep-research` and `workflow`: the command
    already forwards as undeclared-agent-owned, but `/help` and
    autocomplete do not list it. Promote to `command/specs.go` as
    `KindNative` if a live prompt of `/loop 60s …` actually schedules
    (not measured this pass — the command is advertised, the scheduler
    flag is true, the effect is not pinned).

11. **Hook commands are advertised: `hooks-trust`, `hooks-list`,
    `hooks-add`, `hooks-remove`, `hooks-untrust`.**
    `_x.ai/hooks/list` now *requires* `sessionId` (0.2.112 accepted
    `{}`). The daemon still does not consume hooks (0038 §3.5 / 0039
    §2.7). Forwarding the slash commands is cheap (`KindNative` once
    advertised); building a daemon policy engine on `_x.ai/hooks` is
    still its own MADR.

12. **User-invocable skills appear as slash commands**, and one of them
    collides with a `KindNone` lock. This host's ACP catalog included
    `/create-workflow`, `/design`, `/execute-plan`, `/pr-babysit`,
    `/resume-claude`, and `/review` (the bundled review skill). Names
    that are *not* in `command/specs.go` already forward as the agent's
    own command when `Lookup` fails (`command.go:207-209`). Do **not**
    add every skill to the canonical vocabulary.
    `/review` is the exception that hurts: it *is* canonical
    (`specs.go` `review`) and grok's table locks it to `KindNone`
    (`commandtable.go:53`, `ReasonNoReview`). `Resolve` treats an
    explicit `KindNone` as final (`command.go:211-225`) — that is how
    inert `/compact` stays unavailable. On 1.0.3 the same lock now
    hides a skill grok actually advertised. Decision for the plan: either
    drop the grok `review` `KindNone` so the advertised skill forwards
    as `KindNative`, or keep the lock and document that `/review` on
    grok is still the remote "no inline review" command, not the skill.
    `/imagine` collided on this host and arrived as `/bundled:imagine`;
    no table change needed. `/help` will omit non-canonical skills
    unless the help renderer grows an "agent-advertised" section —
    that is help-UX, not a spec explosion.

13. **`/compact` is still silent over ACP.** Advertised with a hint;
    `session/compact` and `_x.ai/compact` are method-not-found;
    `_x.ai/compact_conversation` did not return within the probe
    window. Keep `KindNone` until a method is pinned.

#### P4 — explicitly not taken

These are new or newly documented on 1.0.3 and should not be adopted
in the first implementation pass:

| Surface | Why not |
| --- | --- |
| `--minimal` / `--fullscreen` / `--no-alt-screen` | Pager render modes. Session-scoped TUI only. |
| `--oauth` | TUI welcome-screen auth. Device-code is already wired (`grok login --device-auth`, MADR 0074). |
| `--include-partial-messages`, `--output-format streaming-messages-json` | Headless `-p` only. The remote is ACP stdio. |
| `--restore-code`, `--verbatim`, `--fork-session`, `-c/--continue`, `-r/--resume`, `-s/--session-id` | Headless/TUI session identity. The daemon already owns resume via `session/load`. |
| `--worktree` / `--worktree-ref` | Optional-value flag: `grok --worktree agent …` **consumes `agent` as the worktree name** and never reaches `agent stdio`. Unsafe to append blindly. Still TUI/isolation, not the remote process model. |
| `--cwd` | Redundant with process cwd + `session/new.cwd`. |
| `--json-schema`, `--max-turns`, `--prompt-file`, `--prompt-json` | Still headless / agent-as-API (0039 §2.6). |
| `--agents` / `--agent` / `--agent-profile` / `--plugin-dir` | Subagent definitions and SDK plugin injection. Out of scope; `--plugin-dir` remains the Agent-SDK hook 0038 deferred. |
| `--experimental-memory` / `--no-memory` / `grok memory` | Cross-session memory. No remote UX. |
| `grok dashboard`, `doctor`, `du`, `export`, `inspect`, `setup`, `trace`, `wrap`, `plugin marketplace`, `update` | Operator/TUI utilities. `inspect` is useful *to the implementer* (`grok inspect` on this repo listed skills, hooks, MCP); the daemon should not wrap it. |
| `grok import` | Official CLI reference lists it. **1.0.3 has no `import` subcommand** (`grok import --help` prints top-level help). Docs drift. |
| Model id `grok-build` | Recommended in official overview/settings as the coding default. `session/set_model` and `grok models` on this host do not accept or list it. |
| Hidden aliases `--yolo`, `--dangerously-skip-permissions`, `--allowedTools`, `--disallowedTools`, `--system-prompt`, `--append-system-prompt`, `--effort` | Accepted by the binary (some only as aliases of flags we already emit). No new typed config. |
| `session/set_config_option` | Every probed shape failed (`missing field configId` or `SessionConfigOptionValue` mismatch, then `Method not found` on a retry). Unpinned. |
| `session/set_mode` with `xhigh` | Returns `{}` and emits no `current_mode_update` — the same silent-ignore 0038 recorded for unknown mode ids. Effort is not an ACP mode. |
| `grok agent serve` / `headless` / `leader` | Same call as 0038 §2.1: per-session stdio is correct. New leader flags (`--relay-on-demand`, `--no-exit-on-disconnect`) do not change that. |
| `_x.ai/fs/*`, `_x.ai/git/*`, `_x.ai/git/worktree/*` | Live methods (`_x.ai/fs/list` and `_x.ai/git/status` returned results). These are grok-internal tools, not daemon callbacks. Consuming them would duplicate terminal/fs access the agent already has. |
| `_x.ai/hooks` as a policy engine | Still needs its own MADR (0038 §3.5). |
| Announcements, settings tips, campaigns (`Grok 4.6 is here!`) | TUI banners. Dropped as before. |

### Consequences

* Good, because the static picker stops offering models 1.0.3 rejects
  and starts offering the host default (`grok-4.6`) before the first
  session initializes.
* Good, because `--no-auto-update` matches official ACP guidance and
  keeps a daemon-spawned grok from trying to replace itself.
* Good, because `xhigh` appears automatically from live
  `reasoningEfforts` once a session has initialized; the rank table
  already knows the rung.
* Good, because the argv placement contract is re-confirmed rather
  than rewritten: 0050's live test is still the right pin.
* Neutral, because MADR 0049's synthetic `auto` stays until `_meta.autoMode`
  is discrimination-tested. Two autos (grok-native classifier vs
  daemon interceptor) must not be silently conflated.
* Neutral, because `--tools` / `--allow` / `--deny` remain typed
  config that 0050 measured as ACP no-ops. 1.0.3 did not change that
  measurement.
* Bad, because a mid-session models-update handler that is registered
  under the wrong name has been "implemented" since 0039 and does not
  see 1.0.3's notification. The initialize path hides the bug.
* Bad, because several official pages (`grok import`, `grok-build` as
  a model id, `--yolo` as a documented flag) do not match 1.0.3.
  Implementing from the docs without a binary probe would ship dead
  flags.

### Confirmation

Compliance is the test inventory below, not a checklist of source
edits. A P1/P2/P3 item is not accepted until its named tests exist
and are green. Unit tests run in CI (`go test ./…`). Live tests use
`//go:build live_grok`, skip when `grok` is not on `PATH`, and must
be run on a host that has 1.0.3 before that item is marked done.
Measure-gated items (P2.6, P2.8, P3.10) require the live
discrimination test *first*; production code follows only if that
test's pass condition is met.

`grok --version` on the implementer's PATH is recorded in the live-test
file headers. `make pre-add-check` is required on every touched Go
file.

#### Required tests (new or extended)

| ID | Decision | Test | File | Tag | Must fail when |
| --- | --- | --- | --- | --- | --- |
| T-A1 | P1.1 static floor | `TestStaticModelsFloor` | `internal/provider/grok/grok_test.go` | unit | floor ≠ `[grok-4.6, grok-4.5]` or contains `grok-code-fast-1` / `grok-4` / `grok-build` |
| T-A2 | P1.1 live still replaces floor | existing `TestLiveGrokColdCatalog` | `live_model_test.go` | `live_grok` | harvest returns `SourceStatic` or pads with the floor (`len > 3`) |
| T-B1 | P1.3 `--no-auto-update` | extend `TestDefaultArgs`, `TestDefaultArgsPutsGlobalsBeforeSubcommand`, `TestSpecModelArgs`, `TestSpecModelArgsPolicyFlags`, `TestDefaultArgsSandboxProfile` | `grok_test.go` | unit | flag missing or appears after `"agent"` |
| T-B2 | P1.3 placement vs binary | extend `TestLiveGrokArgvAcceptsEveryConfiguredFlag` | `live_argv_test.go` | `live_grok` | `Start` fails with `unexpected argument '--no-auto-update'` |
| T-C1 | P1.2 handler names | `TestSpecRegistersBothModelsUpdateNames` | `internal/provider/grok/extnotif_test.go` (new) | unit | either `_x.ai/models/update` or `_x.ai/models_update` is unset |
| T-C2 | P1.2 wire spelling | `TestLiveGrokModelsUpdateMethodName` | `live_modelsupdate_test.go` (new) | `live_grok` | initialize/`session/new` stdout lacks `"method":"_x.ai/models/update"` |
| T-D1 | P1.5 argv pin | extend `TestLiveGrokArgvAcceptsEveryConfiguredFlag` with `model46` and `reasoningXHigh` rows | `live_argv_test.go` | `live_grok` | `grok-4.6` or `xhigh` rejected at start |
| T-D2 | P1.1 / P1.5 set_model | extend `TestLiveGrokSetModelWireContract` | `live_setmodel_test.go` | `live_grok` | `grok-4.6` fails or `grok-build` / `grok-code-fast-1` succeeds |
| T-D3 | P1.4 live catalog | extend `TestLiveGrokInitializeMetaWireContract` | `live_initializemeta_test.go` | `live_grok` | live options omit `grok-4.6`, or grok-4.6 levels include `xhigh` marked default |
| T-D4 | P2.7 close already wired | `TestLiveGrokCloseSucceeds` | `live_setmodel_test.go` or `live_test.go` | `live_grok` | `Session.Close` returns an error |
| T-E1 | P1.4 xhigh parse | `TestModelsToCatalogGrok46XHigh` | `internal/provider/acpagent/thinking_test.go` | unit | order ≠ `low,medium,high,xhigh` or default is `xhigh` |
| T-E2 | P1.4 still spawn-only | existing `TestSetThinkingLevelFixed` | `thinking_test.go` | unit | `SetThinkingLevel` is not `ErrThinkingLevelFixed` |
| T-E3 | P1.4 dual-default | `TestNormalizeGrok46DualDefaultKeepsHigh` | `internal/picker/thinking_test.go` | unit | both defaults survive or `xhigh` wins |
| T-F1 | P3.12 `/review` | `TestGrokReviewIsNativeWhenAdvertised` | `internal/provider/grok/commandtable_test.go` (new) | unit | grok table is still `KindNone` for `review` |
| T-F2 | P3.12 resolve | `TestResolveGrokReviewForwardsWhenAdvertised` | `internal/command/command_test.go` | unit | advertised `review` is unavailable, or unaadvertised `review` is available |
| T-F3 | P3.12 live catalog | extend `TestLiveGrokCommandCatalogContainsDeepResearchAndWorkflow` | `live_commandcatalog_test.go` | `live_grok` | 1.0.3 catalog omits `review` (table can revert to KindNone only after this fails) |
| T-F4 | P3.13 compact unchanged | existing `TestLiveGrokCommandTableMatchesReality` `/compact` silence | `live_command_test.go` | `live_grok` | `/compact` starts returning text (re-probe; do not "fix" in 0081) |
| T-G1 | P3.10 `/loop` measure | `TestLiveGrokLoopSchedules` | `live_loop_test.go` (new) | `live_grok` | advertised `/loop 60s …` is silence (do not promote) or, after promote, catalog omits `loop` |
| T-G2 | P3.10 promote (gate) | `TestResolveLoop` + `TestProvidersDeclareEveryCanonicalCommand` | `command_test.go`, `conformance_test.go` | unit | written **only if** T-G1 passes; every provider table must declare `loop` |
| T-H1 | P2.6 `_meta` measure | `TestLiveGrokSessionMetaDiscrimination` | `live_sessionmeta_test.go` (new) | `live_grok` | production `NewSession` is changed before this matrix is recorded |
| T-I1 | P2.8 fork measure | `TestLiveGrokSessionForkShapes` | `live_fork_test.go` (new) | `live_grok` | `ForkSession` is implemented before a listed shape returns a new id |
| T-I2 | P2.8 fork implement (gate) | `TestGrokForkOp` + live `Fork` | `commandtable_test.go`, `live_fork_test.go` | unit + live | written **only if** T-I1 finds a winning shape |
| T-P4 | P4 not taken | `TestDefaultArgsDoesNotEmitP4Flags` | `grok_test.go` | unit | argv contains `--worktree`, `--minimal`, `--cwd`, `--oauth`, `--json-schema`, `--max-turns`, or `--experimental-memory` |

Existing tests that remain the regression net and must stay green:
`TestLiveGrokArgvAcceptsEveryConfiguredFlag`,
`TestLiveGrokAutoDiscriminationPair` (MADR 0049 — H must not break it),
`TestLiveGrokPlanModeSwitch`, `TestSpecSynthesizesAutoMode`,
`TestDefaultArgsPutsGlobalsBeforeSubcommand`,
`TestProvidersDeclareEveryCanonicalCommand`.

A phase that adds production code without its T-* row is incomplete.
A measure-gated T-* that is red is a documented stop, not a licence
to implement.

How to write each test (function body, helpers, commands) is in
[0081-PLAN Tests](./0081-PLAN-grok-1.0.3-surface-parity.md#tests).
The IDs in that section must stay aligned with this table.

## Pros and Cons of the Options

### Measure 1.0.3 and adopt the daemon-relevant surfaces in a phased backlog

* Good, because it separates "the binary grew a TUI" from "the remote
  is lying about models or dropping a notification".
* Good, because every recommended item is already grounded in a
  command, a wire frame, or a file:line in this repo.
* Neutral, because P2 `autoMode` / `yoloMode` / fork still need a
  second probe before code lands.
* Bad, because the phone will not grow `/loop`, fork, or grok-native
  per-session auto until those later phases.

### Implement every newly advertised CLI flag, subcommand, and ACP method

* Good, because nothing grok prints in `--help` would be left
  undocumented in config.
* Bad, because 0050 already showed that several "headless only" flags
  accept on `agent stdio` and do nothing. Repeating that for
  `--minimal`, `--restore-code`, `--verbatim`, marketplace, wrap, and
  dashboard would add config surface with no remote behaviour.
* Bad, because `--worktree` as an optional value can steal the `agent`
  token and break session start — the exact failure class 0050 fixed.

### Defer all uptake until the next incident

* Good, because the live initialize path already feeds `grok-4.6` into
  the picker after the first session, and argv placement did not
  regress.
* Bad, because `models.list` without a session still offers
  `grok-code-fast-1` / `grok-4` and omits `grok-4.6`.
* Bad, because a daemon-spawned grok may auto-update itself.
* Bad, because the models-update handler has been registered under a
  name 1.0.3 does not emit, so the "we already subscribe" claim is
  false.

## More Information

### Method

* Binary: `grok 1.0.3 (1a29d5bc12d4) [stable]`,
  `/Users/saxsmith/.local/bin/grok`.
* CLI: `grok --help`, `grok agent --help`, `grok agent stdio --help`,
  every subcommand `--help`, `grok models`, `grok inspect`,
  `grok mcp list`, `grok plugin list`, `grok sessions list`,
  `grok doctor`. Flag-placement matrix for 28 flags before and after
  `agent`. Sandbox start of an unknown profile.
* ACP: `grok --permission-mode default agent --no-leader stdio`
  driven as raw JSON-RPC (same vector as `defaultArgs` plus the
  existing permission-mode default). Frames: `initialize`,
  `session/new`, `session/set_model`, `session/set_mode`,
  `session/set_config_option` (failed shapes), `session/list`,
  `session/close`, `session/resume`, `session/load`,
  `_x.ai/hooks/list`, `_x.ai/session/fork`, `_x.ai/session/list`,
  `_x.ai/fs/list`, `_x.ai/git/status`, `_x.ai/git/worktree/list`.
* Docs: DuckDuckGo via MagicTools (`ddg-search:search_web`) for
  "xAI Grok CLI official documentation" and "Grok Build TUI official
  docs"; the useful hits were
  [docs.x.ai/build/overview](https://docs.x.ai/build/overview),
  [docs.x.ai/build/cli/reference](https://docs.x.ai/build/cli/reference),
  [docs.x.ai/build/cli/headless-scripting](https://docs.x.ai/build/cli/headless-scripting),
  [docs.x.ai/build/settings](https://docs.x.ai/build/settings),
  [docs.x.ai/build/modes-and-commands](https://docs.x.ai/build/modes-and-commands).
  Markdown permalinks (`….md`) 404. Installed user guide
  `~/.grok/docs/user-guide/{04,14,15,20}-*.md` is richer than the
  public pages (ACP `_meta` options, `/loop`, `/effort`, `--yolo`).

### What the remote already surfaces (unchanged and still correct)

| Surface | Code | 1.0.3 status |
| --- | --- | --- |
| stdio ACP, `--no-leader` | `grok.go` `defaultArgs` | still the right transport |
| Global flags before `agent` | `grok.go:107-154`; `live_argv_test.go` | still required |
| `-m`, `--reasoning-effort`, `--always-approve` | `defaultArgs` | accepted both before and after `agent`; we keep them global |
| `--permission-mode` enum | `config.md`; clap on 1.0.3 | still `default, acceptEdits, auto, dontAsk, bypassPermissions, plan`. `classifier` rejected |
| `--sandbox` built-ins | `Config.Sandbox` | `off, workspace, devbox, read-only, strict` still accepted. Unknown profile now fails at **start** (`could not apply … Refusing to start`), not at `--help` parse |
| `--no-subagents`, `--disable-web-search` | typed config | still accepted as globals |
| `--tools` / `--allow` / `--deny` | typed config | still accepted as globals; 0050's "ACP no-op" measurement not re-run |
| Synthetic `auto` mode | `SynthesizeAutoMode`; MADR 0049 | still necessary until `_meta.autoMode` is discrimination-tested |
| `session/set_model` | `acpagent.session.SetModel` | works for `grok-4.6` and `grok-4.5` |
| `session/set_mode` plan/default | static modes + SDK | still the Build/Plan switch |
| Live catalog from `initialize._meta.modelState` | `acpagent.go:422-429` | works; this is how `grok-4.6` already appears after first start |
| MCP http/sse forward | `buildMcpServers` | `mcpCapabilities.http/sse` still true |
| MCP status notifications | `_x.ai/mcp/server_status`, `_x.ai/mcp_initialized` | names still match the wire (slash) |
| Subagent notifications | `_x.ai/session_notification` | still emitted (`model_changed` seen with `model_id` + `reasoning_effort`) |
| `deep-research`, `workflow`, `goal` | `commandtable.go`; `specs.go` | still advertised |
| Device-code auth | `device_auth.go`; `auth.go` (already notes 1.0.0) | `grok login --device-auth` still present |
| Thinking spawn flag | `spawnArgs` / `ModelArgs` | `--reasoning-effort` still launch-only |

### New or changed vs 0.2.114 (measured)

| 1.0.3 surface | Evidence | Remote today |
| --- | --- | --- |
| Default model `grok-4.6`; also `grok-4.5` | `grok models`; `initialize._meta.modelState`; announcement `Grok 4.6 is here!` | static floor stale; live initialize OK |
| `xhigh` effort on grok-4.6 only | `reasoningEfforts` on initialize and `session/new.models` | parsed if live catalog loads; `/thinking` still fixed-at-start |
| `--effort` alias | `grok --help`; clap | we emit the canonical `--reasoning-effort` |
| `--no-auto-update` global | accepted before `agent`; official headless page | not emitted |
| `--yolo` / `--dangerously-skip-permissions` | accepted, not listed in `--help` | covered by `AlwaysApprove` |
| `--oauth`, `--minimal`, `--fullscreen`, `--restore-code`, `--verbatim`, `--cwd`, `--include-partial-messages` | `grok --help` | unused (P4) |
| `streaming-messages-json` output format | `grok --help` | headless only |
| New subcommands: `dashboard`, `doctor`, `du`, `export`, `inspect`, `setup`, `trace`, `wrap`; `plugin marketplace`; `mcp doctor`; `memory clear`; `worktree db` | `--help` trees | unused (P4) |
| `sessionCapabilities.list/resume/close` | initialize | unused |
| `session/list`, `session/close`, `session/resume` | live RPC | unused |
| `session/new.models` top-level | live RPC | unused (initialize `_meta` is) |
| `session/new._meta.{yoloMode,autoMode,rules}` | user guide + live accept | not sent |
| `_x.ai/sessionConfig` effort rungs as `category: "mode"` | live `session/new` | not parsed; must not become session modes |
| `_x.ai/schedulerBackgroundLoops: true` | live `session/new` | unused |
| `_x.ai/session/fork` | live RPC, schema incomplete | `/fork` is `KindNone` |
| `/loop`, `/hooks-*`, `/feedback`, skill names | `available_commands_update` | non-canonical names forward; `/review` is blocked by grok `KindNone` |
| `_x.ai/hooks/list` requires `sessionId` | live RPC | still unused |
| `_x.ai/fs/list`, `_x.ai/git/status` | live RPC | unused (P4) |
| `--worktree` optional value steals `agent` | `grok --worktree agent --no-leader stdio --help` prints TUI help | not emitted (keep it that way) |

### Official docs vs this binary

* [CLI reference](https://docs.x.ai/build/cli/reference) lists
  `grok import [targets…]`. 1.0.3 has no `import` command.
* [Overview](https://docs.x.ai/build/overview) / [settings](https://docs.x.ai/build/settings)
  recommend model id `grok-build`. ACP and `grok models` on this host
  do not.
* [Headless](https://docs.x.ai/build/cli/headless-scripting) recommends
  `--no-auto-update` for ACP — that flag *does* exist and is the one
  P1 item taken from the docs rather than from `--help` (it is not
  listed on the top-level `--help` either, but clap accepts it as a
  global).
* User guide `15-agent-mode.md` is the only place that documents
  `session/new` `_meta` (`yoloMode`, `autoMode`, `rules`, …) and the
  `x.ai/*` extension map. Public docs do not.

### Related records

* [0038](./0038-MADR-grok-acp-parity-assessment.md) / [0039](./0039-MADR-grok-acp-parity.md) — original ACP parity vs 0.2.112
* [0049](./0049-MADR-grok-auto-mode.md) / [0053](./0053-MADR-grok-auto-mode-silent-arm.md) — synthetic auto; revisit only after `_meta.autoMode` is measured
* [0050](./0050-MADR-grok-cli-surface-drift.md) — argv placement; still the spawn contract
* [0052](./0052-MADR-thinking-levels-and-settings.md) — thinking lists; `xhigh` is new on grok-4.6, still spawn-only
* [0074](./0074-MADR-remote-provider-auth-from-phone.md) — device-code auth, already notes the 1.0.0 bump
* [0023](./0023-MADR-canonical-slash-commands.md) — where `/loop` would land if P3 is accepted
* [0081-PLAN](./0081-PLAN-grok-1.0.3-surface-parity.md) — implementation order, gates, and stop conditions

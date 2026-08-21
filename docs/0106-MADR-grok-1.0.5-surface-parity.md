---
status: accepted
date: 2026-08-19
decision-makers: Project Owner (scope and acceptance); Implementer (measurement)
consulted: none
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Adopt grok 1.0.5's ACP `_meta` model and effort surface, and stop treating spawn flags as applied

## Context and Problem Statement

The grok provider last pinned its CLI/ACP contract against **1.0.4
(`d846eb93d94d`)** in
[0092](./0092-MADR-grok-1.0.4-surface-parity.md)
([0092-PLAN](./0092-PLAN-grok-1.0.4-surface-parity.md), Complete
2026-08-15). The binary on this host is now **grok 1.0.5
(`5115b46bc909`) [stable]** at `/Users/saxsmith/.local/bin/grok` →
`/Users/saxsmith/.grok/bin/grok` → `../downloads/grok-1.0.5-macos-aarch64`
(symlink mtime 2026-08-19 12:03). `grok version --json` reports
`currentVersion=1.0.5 (5115b46bc909)`, channel `stable`.
`grok update --check --json` reports `latestVersion=1.0.5`,
`updateAvailable=false`.

Sources for this release live in `/Users/saxsmith/gitrepos/grok-build`
(`crates/codegen/xai-grok-version` `version = "1.0.5"`, `SOURCE_REV`
`7bd63df3c9bb1bf98e7a9b3486f4a0189ea94e55`, last dump commit 2026-08-16).
The shipped binary stamps a different short hash than the dump revision;
the crate version and installed changelog both say 1.0.5. Where they
disagree with `grok --help` or a live ACP probe, the binary wins.

This record asks: **what did 1.0.5 add, expose, or change on the
remote-visible CLI and ACP surface, and which of those should the grok
provider adopt?** The headline is not a new subcommand. It is that the
spawn flags we already emit for model and thinking **do not apply** to
`agent stdio` sessions, while a newly documented ACP `_meta` path does —
and mid-session thinking, locked since [0052](./0052-MADR-thinking-levels-and-settings.md)
as a silent-accept trap, now has a discriminating wire shape.

The spawn vector from MADR 0050 / 0081 / 0092 is **still valid as
argv placement** on 1.0.5: `grok <globals> --no-auto-update agent
--no-leader stdio`. Policy flags still belong before `agent`.
`--always-approve` / `--yolo` remain valid in both positions.
Placement is not the bug. Application is.

## Decision Drivers

* The live binary, not the public changelog, is the contract
  (AGENTS.md; MADR 0050 D2; MADR 0081; MADR 0092).
* Adopt only surfaces that change remote session behaviour or
  operator control. TUI chrome, pager selection, and dashboard
  shortcuts stay out.
* Do not ship a flag or RPC whose effect on `agent stdio` has not
  been *discriminated*. `Start` succeeding is not evidence the flag
  did anything (MADR 0050's original lesson, now repeating for
  `-m` / `--reasoning-effort`).
* A measure-gated 0081/0052 item that now has a winning probe is no
  longer "later".
* Official pages and the installed user guide that omit
  `_meta.reasoningEffort` / `_meta.modelId` are docs drift, not work.
* Do not replace MADR 0049's synthetic `auto` with grok-native
  `autoMode` until a discrimination pair writes a file (0092 P2.3
  still stands; 1.0.5 did not re-run that pair).
* Match grok's own ACP-client idioms where they are load-bearing
  (explicit `_meta` keys; `_meta.modelId` not top-level `modelId`;
  `_meta.reasoningEffort` not a top-level field on `session/set_model`).

## Considered Options

* Measure 1.0.5, adopt the discriminated ACP `_meta` model and
  reasoning-effort surface on `session/new`, `session/load`,
  `session/resume`, and `session/set_model`, re-pin the live
  contract, and leave TUI / hooks / config-only 1.0.5 features out
* Implement every 1.0.5 changelog item and every newly advertised
  initialize `_meta` capability (`pluginDirs`, `sessionRecap`,
  `voiceMode`, `GROK_CONFIG`, worktree gc, image/video caps)
* Treat 1.0.5 as a no-op patch and only bump version strings

## Decision Outcome

Chosen option: "Measure 1.0.5, adopt the discriminated ACP `_meta`
model and reasoning-effort surface on `session/new`, `session/load`,
`session/resume`, and `session/set_model`, re-pin the live contract,
and leave TUI / hooks / config-only 1.0.5 features out", because the
0081/0092 spawn contract still *parses*, the new ACP `_meta` path is
the first winning discrimination of applied model and effort, and
most of the 1.0.5 changelog is pager, worktree, or process-internal.

This MADR is **accepted**. Companion plan:
[0106-PLAN-grok-1.0.5-surface-parity.md](0106-PLAN-grok-1.0.5-surface-parity.md)
(Complete, 2026-08-19). Phase commits: `630d8e5` (A, 1.0.5 pin +
live discrimination), `0c22a73` (B, SessionMeta + harvest),
`6286e9e` (C, SetThinkingLevel). Phase D was run-only
(`go test -tags live_grok ./internal/provider/grok/` green,
473s). Binary: grok 1.0.5 (`5115b46bc909`) [stable].

Implementation outcomes:

* P1.1–P1.4 SessionMeta + harvest + stable spawnArgs: **done.**
  T-M2 `/session-info` shows grok-4.5; T-E3 `ThinkingLevel()=="low"`.
* P1.5 live pins: **done.** Headers name 1.0.5 (`5115b46bc909`).
* P2.6 SetThinkingLevel: **done.** T-T2 green (`low`, `xhigh` when
  advertised, `quantum` errors and does not mutate).
* T-K `--no-plan` on the P4 forbid list: **done.**
* P3/P4: **not taken**, as written.

### Recommended uptake (proposed, not implemented)

Priorities are product impact against a *measured* 1.0.5 wire.

#### P1 — spawn flags we already emit do not apply; `_meta` does

1. **Send `_meta.modelId` on `session/new` and `session/load`.**
   Exact daemon argv
   `grok -m grok-4.5 --reasoning-effort low --no-auto-update agent --no-leader stdio`
   still opens `currentModelId: grok-4.6` and
   `reasoningEffort: high`. `session/new` with
   `_meta: { "modelId": "grok-4.5" }` selects grok-4.5. Top-level
   `modelId` on `session/new` is ignored (stays grok-4.6).
   grok-build reads the id from `_meta.modelId`
   (`crates/codegen/xai-grok-shell/src/agent/mvp_agent/session_setup.rs`
   around the `custom_model_id` extract).

   Today's path (`internal/provider/acpagent/acpagent.go` `spawnArgs`
   + `grok.defaultArgs` + `conn.NewSession({Cwd, McpServers})`) never
   puts a model on the ACP request. `ConfigureSession` is nil for
   grok. Mid-session `session/set_model` still works (0092 pin);
   create-session and config default model do not.

2. **Send `_meta.reasoningEffort` on `session/new` and `session/load`.**
   Changelog: "Reasoning effort can now be supplied when an ACP
   client opens or resumes a session." Live 1.0.5, same
   `--permission-mode default` spawn, no `--reasoning-effort`:

   | `session/new` `_meta` | grok-4.6 `reasoningEffort` | `sessionConfig` selected mode |
   | --- | --- | --- |
   | (omitted) | `high` | `high` |
   | `{reasoningEffort:"xhigh"}` | `xhigh` | `xhigh` |
   | `{reasoningEffort:"low"}` | `low` | `low` |

   `session/load` with `_meta.reasoningEffort: "xhigh"` on a session
   created at `low` reports `xhigh`. `session/resume` with
   `_meta.reasoningEffort: "medium"` then reports `medium`.
   grok-build: `parse_reasoning_effort_meta` on
   `session/new` / `session/load` (`reasoning_effort.rs`
   `split_new_session_effort`; `session_setup.rs`
   `initial_reasoning_effort`). Canonical tokens:
   `none, minimal, low, medium, high, xhigh, max`
   (`xai-grok-sampling-types`). grok-4.6 advertises
   `xhigh, high, medium, low`; grok-4.5 advertises `high, medium, low`.

   Seed from `StartOptions.ThinkingLevel` then
   `Config.ReasoningEffort` — the same precedence `spawnArgs`
   already documents (MADR 0052 A3.2) — but put the value on
   `_meta`, not on argv.

3. **Stop relying on `-m` and `--reasoning-effort` for ACP
   application.** Keep emitting them only if a follow-up probe
   shows a non-ACP consumer (TUI leftover, headless `-p`) still
   needs them on this process. For `agent stdio` they are accepted
   globals and **no-ops**. `TestLiveGrokArgvAcceptsEveryConfiguredFlag`
   will keep passing today because it only asserts `Start` succeeds.

4. **Prewarm becomes available for per-session model and thinking.**
   `spawnArgs` rebuilds argv whenever `ThinkingLevel` or (when
   `Config.Model` is empty) `StartOptions.Model` is set, so those
   sessions never claim the warm spare (`acpagent.go` claimWarm
   comment). Once model and effort ride `_meta`, default argv is
   stable and prewarm can serve them. Do not take this as a
   separate product feature; it falls out of P1.1–P1.3 if argv
   stops changing.

5. **Re-pin the live suite to 1.0.5 (`5115b46bc909`).** Headers in
   `live_*_test.go`, `grok.go`, and `commandtable.go` still say
   1.0.4. Behaviour they pin (argv *placement*, set_model accept /
   reject ids, models-update slash name, fork shape, `/loop`,
   `/review`, `/compact` silence, `--no-auto-update`) still holds
   unless noted below.

#### P2 — mid-session thinking is unblocked

6. **Implement `SetThinkingLevel` via `session/set_model` with
   `_meta.reasoningEffort`.** 0052 §2.2 / 0081 P1.4 / 0092 P4
   recorded the silent-accept trap for a *top-level*
   `reasoningEffort` field. That trap is still real. The `_meta`
   shape is not:

   | `session/set_model` params | RPC result | `session/resume` grok-4.6 effort | selected mode |
   | --- | --- | --- | --- |
   | `{sessionId, modelId:"grok-4.6", reasoningEffort:"xhigh"}` (top-level) | `{_meta:{model:{Ok:"grok-4.6"}}}` | **unchanged** (reverts toward catalog default `high` from a prior `low`) | `high` |
   | `{sessionId, modelId:"grok-4.6", _meta:{reasoningEffort:"xhigh"}}` | same Ok envelope | **`xhigh`** | `xhigh` |
   | `{sessionId, modelId:"grok-4.6", _meta:{reasoningEffort:"low"}}` | same Ok envelope | **`low`** | `low` |
   | `{sessionId, modelId:"grok-4.6", _meta:{reasoningEffort:"quantum"}}` | same Ok envelope | (not re-read; treat as ignore) | |

   grok-build `set_session_model` calls
   `parse_reasoning_effort_meta(args.meta)` and passes it to
   `model_switch::apply`. Our `session.SetModel` currently sends
   only `{sessionId, modelId}`
   (`acpagent/session.go`). `SetThinkingLevel` returns
   `ErrThinkingLevelFixed`.

   Implementation shape:

   * Keep `SetModel` as model-only unless a later PLAN wants a
     combined call.
   * `SetThinkingLevel(level)`: `rawRequest("session/set_model",
     {sessionId, modelId: current, _meta: {reasoningEffort: level}})`.
     Current model id is `ThinkingLevel`'s sibling — harvest from
     the live catalog / last `set_model` / `sessionDetail.currentModelId`,
     not from spawn argv.
   * Discriminate before advertising success: either parse
     `session/resume`'s `models.availableModels[current]._meta.reasoningEffort`
     (measured) or the `x.ai/sessionConfig` selected `category:"mode"`
     option (same payload; do **not** feed those options into
     `session/set_mode` — 0081 P2.9 still holds).
   * Unknown tokens: grok-build warns and ignores
     (`parse_reasoning_effort_meta`). Do not claim the level changed
     if resume still shows the previous value.
   * `/thinking` is already `KindOp` / `OpSetThinkingLevel` on grok
     (`commandtable.go`). No table remap. The op starts working.

#### P3 — recorded, not taken, unless a later product request lands

7. **`GROK_CONFIG` / `GROK_CONFIG_PATH` overlay.** New in 1.0.5.
   Launcher JSON/TOML overlay, allowlisted to `models`, `features`,
   narrowed `toolset.web_search.{allowed,excluded}_domains`,
   `toolset.bash.login_shell_capture`, and
   `shell_environment_policy` filter fields
   (`xai-grok-config/src/config_override.rs` `OVERLAY_ALLOW_PATHS`).
   Live: `GROK_CONFIG='{"models":{"default_reasoning_effort":"low"}}'`
   makes a no-meta `session/new` report grok-4.6 effort `low`. That
   is a *process* default, not per-session. Prefer P1 `_meta` for
   the phone. Do not add a `providers.grok.*` key that writes
   grok's config.toml. Optional later: set `GROK_CONFIG` on the
   spawned process for operator overlays (web-search domain lists)
   without touching `$HOME`.

8. **`_meta.pluginDirs`.** `initialize._meta.x.ai/pluginDirs: true`.
   Absolute directories, CliOverride trust, per-session
   (`mvp_agent/mod.rs` `parse_session_plugin_dirs`). Same class as
   0081 P4 `--plugin-dir`: Agent-SDK injection, out of scope until
   a product request.

9. **`additionalDirectories` on `session/new`.** ACP SDK field
   (`acp-go-sdk` `NewSessionRequest.AdditionalDirectories`). grok
   accepts it on new. `session/resume` refuses it
   (`RESUME_REFUSES_EXTRA_DIRS`). Distinct from daemon `FSRoots`
   (callback confinement). Not taken: the remote already sets
   process cwd + `session/new.cwd`.

10. **`hooks-*` slash commands are back in the ACP catalog.** 0092
    recorded them missing on 1.0.4. 1.0.5 `available_commands_update`
    again lists `hooks-trust`, `hooks-list`, `hooks-add`,
    `hooks-remove`, `hooks-untrust`. Keep 0081's rule: forward as
    undeclared-agent-owned; do not add them to `command/specs.go`.
    `_x.ai/hooks` as a daemon policy engine still needs its own MADR.

11. **`GROK_FORCE_LOGIN_TEAM_ID`.** Restricts interactive login to
    listed teams. Device-code (`grok login --device-auth`) is
    already wired (MADR 0074 / 0085). Not taken.

12. **Grok-client `_meta` idiom (yoloMode / autoMode always
    explicit).** grok's own pager always stamps `_meta.yoloMode`
    and `_meta.autoMode` because "absent key ≠ off" under leader
    injection (`xai-grok-pager/.../effects/helpers.rs` `to_meta`).
    We spawn `--no-leader`, so that injection should not apply.
    Production `NewSession` still sends `{Cwd, McpServers}` only.
    Do not start sending `autoMode`/`yoloMode` from this MADR
    (0092 P2.3). If a later MADR takes them, stamp both polarities
    explicitly.

13. **Host skill catalog growth is not a canonical-vocab change.**
    1.0.5 advertised 29 commands on this host, including 0081's
    `loop` / `review` / `workflow` / `deep-research` / `goal`,
    bundled skills (`create-workflow`, `design`, `execute-plan`,
    `pr-babysit`, `resume-*`, `bundled:imagine`, `code-review`,
    `implement`, `create-skill`, `skill-design-principles`,
    `build-with-ai`), plus user skill `madr-and-plan-writing`.
    `/compact` stays `KindNone` (`session/compact` still
    method-not-found; `_x.ai/compact_conversation` did not return
    within 12s). `/fork` stays `KindOp` / `OpFork` (0092).

#### P4 — explicitly not taken

| Surface | Why not |
| --- | --- |
| Worktree auto-reclaim under `~/.grok/worktrees`; `grok worktree gc` | Grok-internal isolation. `--worktree` is still unsafe to append (0081 P4: optional value steals the `agent` token). |
| Image/video generation per-step call limits; `GROK_MAX_PARALLEL_*` | Process-internal caps. `promptCapabilities.image` is still `false`. |
| Arabic/Persian TUI reorder; preparing-spinner labels; minimal-mode stream fix | Pager chrome. |
| Hook policy blocks reporting "Turn blocked by a hook" | Grok-side copy. Daemon still has no hook policy engine. |
| `grok inspect` pipe-close crash; Windows skill home-dir; `/dev/null` tool-call fix | Reliability inside grok. No daemon API. |
| `--no-plan` | New global (accepted before `agent`, rejected after). TUI plan-mode kill switch. We already have ACP `session/set_mode`. |
| `--no-ask-user` | Still a real global. Emitting it would strip `_x.ai/ask_user_question` (0092 P3.7). |
| `--disable-image-generation` / `--disable-video-generation` | Still rejected as CLI flags. |
| `session/set_config_option` | Still `Invalid params` / unpinned. |
| `session/list` as the phone roster | Still grok-local TUI sessions mixed with mcremote temp dirs. `session/close` remains the one we already call (`x.ai/closeOutcome: "closed"`). |
| `session/resume` as the daemon resume path | We use `session/load` (capability still `loadSession: true`). Resume accepted on a live id and returned a `models` block; using it as a *read-back* for P2 is fine, replacing `session/load` is not. |
| `_x.ai/fs/*`, `_x.ai/git/*` | Still grok-internal tools. Both returned results. |
| `_x.ai/mcp/init_progress`, `_x.ai/mcp/servers_updated`, `_x.ai/settings/update`, `_x.ai/announcements/update` | Emitted on 1.0.5. Known since 0038; deliberately unconsumed. |
| `initialize._meta.sessionRecap`, `voiceMode`, `cancelRewind`, `grokShell`, `x.ai/mcp/sdk`, `mcpApps` | Capabilities we do not surface. Recap is a TUI `/resume` feature. |
| `grok agent serve` / `headless` / `leader` | Same call as 0038 §2.1: per-session stdio is correct. |
| Model id `grok-build` | Still recommended in the installed agent-mode guide. `session/set_model` still `Invalid params`. `grok models` lists `grok-4.6` (default) and `grok-4.5`. |
| Auth method set | Unchanged: `xai.api_key`, `cached_token`, `grok.com`. `defaultAuthMethodId: cached_token`. 0085 remains its own MADR. |
| `--tools` / `--allow` / `--deny` | Still typed config that 0050 measured as ACP no-ops. 1.0.5 did not re-discriminate them. |

### Consequences

* Good, because create-session model and thinking stop being
  picker lies: the phone's choice would ride a field grok actually
  applies.
* Good, because `/thinking` on an existing grok session can start
  working without respawn, using a shape the binary discriminates.
* Good, because per-session model/effort no longer have to bust
  prewarm.
* Good, because the 0050 argv *placement* contract is re-confirmed
  rather than rewritten. 1.0.5 did not move flags back after
  `agent`.
* Neutral, because MADR 0049's synthetic `auto` and 0085's auth
  wiring are unchanged.
* Neutral, because `--tools` / `--allow` / `--deny` remain typed
  config of unremeasured ACP effect.
* Bad, because `live_argv_test` and `TestDefaultArgs*` will keep
  green while `-m` / `--reasoning-effort` do nothing to the
  session. Confirmation must add applied-effort / applied-model
  assertions, not another `Start` succeeds row.
* Bad, because implementing `SetThinkingLevel` on the top-level
  `reasoningEffort` field would look like success (`Ok: grok-4.6`)
  and change nothing — the same 0052 trap, still live.
* Bad, because the installed agent-mode guide's `session/new`
  `_meta` table still lists only `rules`, `systemPromptOverride`,
  `agentProfile`, `yoloMode`, `autoMode`. Implementing from that
  table without this probe would miss the 1.0.5 feature.

### Confirmation

Compliance is the test inventory below, not a checklist of source
edits. A P1/P2 item is not accepted until its named tests exist and
are green. Unit tests run in CI (`go test ./…`). Live tests use
`//go:build live_grok`, skip when `grok` is not on `PATH`, and must
be run on a host that has 1.0.5 before that item is marked done.

`grok --version` on the implementer's PATH is recorded in the
live-test file headers. `make pre-add-check` is required on every
touched Go file. No PLAN, no mutation of non-docs files, until the
owner approves execution.

| ID | Decision | Test | File | Tag | Must fail when |
| --- | --- | --- | --- | --- | --- |
| T-M1 | P1.1 | live `session/new` `{_meta.modelId:"grok-4.5"}` → `models.currentModelId` / `sessionDetail.currentModelId` is grok-4.5; argv `-m grok-4.5` with empty `_meta` stays grok-4.6 | `live_sessionmeta_test.go` or new `live_newmeta_test.go` | `live_grok` | `_meta.modelId` ignored, or argv-only is treated as success |
| T-M2 | P1.1 implement | `Start(StartOptions{Model:"grok-4.5"})` results in agent current grok-4.5 | same | `live_grok` | production `NewSession` still omits `_meta.modelId` |
| T-E1 | P1.2 | live `session/new` `_meta.reasoningEffort` `xhigh` vs `low` vs omitted: grok-4.6 effort and selected `category:"mode"` match the hint; omitted is `high` | same | `live_grok` | all three rows report `high`, or `xhigh` is claimed without selected-mode evidence |
| T-E2 | P1.2 | live `session/load` `_meta.reasoningEffort` changes the loaded session's reported effort | same | `live_grok` | load ignores the hint |
| T-E3 | P1.2 implement | `Start(StartOptions{ThinkingLevel:"low"})` without relying on argv: reported effort is `low` | same | `live_grok` | production `NewSession` still omits `_meta.reasoningEffort` |
| T-A1 | P1.3 | extend argv live test *comments* to say 1.0.5 accepts `-m` / `--reasoning-effort` and does not apply them over ACP; do not delete the placement pin | `live_argv_test.go` | `live_grok` | a flag after `agent` is accepted that 0050 forbids, or a new global we emit is rejected |
| T-P | P1.5 pin | comments + existing argv / set_model / fork / models-update tests still green against 1.0.5 | existing live files | `live_grok` | 1.0.5 rejects a current global or fork shape |
| T-T1 | P2.6 | live `session/set_model` `_meta.reasoningEffort` `xhigh` then `low`; `session/resume` reports matching effort. Top-level `reasoningEffort` must *not* be treated as success | new `live_thinking_test.go` | `live_grok` | `_meta` does not move selected mode, or top-level is used in production |
| T-T2 | P2.6 implement | `sess.(ThinkingSession).SetThinkingLevel(ctx, "low")` no longer returns `ErrThinkingLevelFixed`; `ThinkingLevel()` reports `low` | same + unit on request shape | `live_grok` + unit | production still returns `ErrThinkingLevelFixed`, or request omits `_meta` / includes top-level `reasoningEffort` |
| T-K | P4 not taken | existing `TestDefaultArgsDoesNotEmitP4Flags` plus `--no-plan` | `grok_test.go` | unit | argv grows `--worktree`, `--no-ask-user`, `--minimal`, `--cwd`, `--oauth`, `--no-plan` |
| T-A | auto not replaced | existing `TestLiveGrokSessionMetaDiscrimination` logs only; production `NewSession` does not grow `autoMode` / `yoloMode` | `live_sessionmeta_test.go` | `live_grok` | those keys appear in production `NewSession` without a new MADR |

Existing tests that remain the regression net:
`TestStaticModelsFloor`, `TestSpecRegistersBothModelsUpdateNames`,
`TestLiveGrokModelsUpdateMethodName`,
`TestLiveGrokSetModelWireContract` (ids; not effort),
`TestLiveGrokInitializeMetaWireContract`,
`TestLiveGrokCloseSucceeds`,
`TestLiveGrokCommandCatalogContainsDeepResearchAndWorkflow`,
`TestLiveGrokAutoDiscriminationPair`,
`TestLiveGrokSessionForkShapes`,
`TestGrokForkIsOpFork`,
`TestDefaultArgsPutsGlobalsBeforeSubcommand`,
`TestProvidersDeclareEveryCanonicalCommand`.

## Pros and Cons of the Options

### Measure 1.0.5, adopt `_meta` model/effort, re-pin, leave the rest out

* Good, because it closes the 0052 silent-accept trap with a
  verbatim discriminating payload, and it fixes create-session
  model/effort which 0081/0092 never discriminated.
* Good, because it refuses to grow config surface for TUI
  settings, worktree gc, and overlay env vars the phone does not
  need.
* Neutral, because argv `-m` / `--reasoning-effort` can remain as
  accepted no-ops until a PLAN deletes them; placement tests stay
  valuable.
* Bad, because operators who wanted daemon-written `GROK_CONFIG`
  domain lists or `pluginDirs` will not get them from this
  decision.

### Implement every 1.0.5 changelog item and initialize capability

* Good, because `~/.grok/CHANGELOG.md` would map 1:1 onto
  `providers.grok.*` and initialize `_meta`.
* Bad, because most items are pager selection, worktree reaping,
  or hook-engine features deferred since 0038.
* Bad, because emitting `--no-plan` or `--no-ask-user` would
  remove phone-visible surfaces we already handle over ACP.

### Treat 1.0.5 as a no-op patch

* Good, because argv placement, fork, `/loop`, `/review`,
  `--no-auto-update`, and the model floor did not move.
* Bad, because create-session model and thinking are now known
  no-ops, and mid-session thinking has a winning probe behind
  `ErrThinkingLevelFixed`.
* Bad, because every live-test header would keep claiming 1.0.4
  after the binary on this host is 1.0.5.

## More Information

### Method

* Binary: `grok 1.0.5 (5115b46bc909) [stable]`,
  `/Users/saxsmith/.local/bin/grok` → `/Users/saxsmith/.grok/bin/grok`
  → `../downloads/grok-1.0.5-macos-aarch64` (Mach-O arm64, 134349648
  bytes, mtime 2026-08-19 12:03).
* Sources: `/Users/saxsmith/gitrepos/grok-build`, crate version
  1.0.5, `SOURCE_REV` `7bd63df3c9bb1bf98e7a9b3486f4a0189ea94e55`.
* CLI: `grok --version` / `grok version --json`,
  `grok update --check --json`, `grok --help`, `grok agent --help`,
  `grok agent stdio --help`, `grok models`, `grok inspect --json`,
  subcommand `--help` for login / sessions / worktree / mcp /
  plugin / memory / leader / update / setup. Hidden-flag matrix
  before and after `agent` (`--no-auto-update`, `--no-ask-user`,
  `--yolo`, `--no-plan`, `--no-memory`, `--experimental-memory`,
  `--disable-image-generation`).
* ACP: `grok --no-auto-update --permission-mode default agent --no-leader stdio`
  driven as raw JSON-RPC (same vector as `defaultArgs` plus the
  existing permission-mode default). Frames under
  `/tmp/grok105-probe/`: `initialize`, `session/new` baseline and
  `_meta` variants, method inventory, `session/set_model` effort
  shapes, `session/resume` read-back, `session/load` effort,
  argv-vs-`_meta` precedence, `_meta.modelId` vs top-level
  `modelId`, `GROK_CONFIG` overlay.
* Docs: installed `~/.grok/CHANGELOG.md` (**1.0.5 — 2026-08-15**),
  `~/.grok/docs/user-guide/` (especially 14-headless, 15-agent-mode,
  05-configuration), public
  [changelog](https://x.ai/build/changelog) (latest **v1.0.5 · Aug 15,
  2026** — 0092 recorded this page still naming 1.0.3).

### What 1.0.5 added (changelog vs wire)

Installed changelog features, classified:

| Changelog item | Remote-visible? | This MADR |
| --- | --- | --- |
| `GROK_CONFIG` / `GROK_CONFIG_PATH` | Yes, process overlay | P3.7, not taken as config keys |
| Worktree auto-reclaim | Grok-internal | P4 |
| Hook block copy | Grok-side string | P4 |
| Image/video step caps | Process-internal | P4 |
| Arabic/Persian TUI | Pager | P4 |
| ACP client can supply reasoning effort on open/resume | **Yes** | P1.2, P2.6 |
| Session titles / recap | `initialize._meta.sessionRecap: true`; TUI `/resume` | P4 |
| `GROK_FORCE_LOGIN_TEAM_ID` | Login only | P3.11 |
| Preparing spinner labels | Pager | P4 |

Public changelog now lists 1.0.5. That lag from 0092 is closed.

### Initialize and session contract (1.0.5, this host)

`protocolVersion`: 1.

`agentCapabilities`: `loadSession: true`;
`promptCapabilities: {image:false, audio:false, embeddedContext:true}`;
`mcpCapabilities: {http:true, sse:true}`;
`sessionCapabilities: {list:{}, resume:{}, close:{}}`; `auth: {}`.

`agentCapabilities._meta`: `x.ai/fs_notify`, hooks blocking events
still `pre_tool_use` / `stop` / `subagent_stop`, toolOverrides
`x_keyword_search`/`x_semantic_search` true, `x_user_search` /
`x_thread_fetch` false.

`initialize._meta` (partial): `agentVersion: "1.0.5"`,
`defaultAuthMethodId: "cached_token"`, `x.ai/pluginDirs: true`,
`x.ai/mcp/sdk: true`, `sessionRecap: true`, `voiceMode: true`,
`cancelRewind: true`, `grokShell: true`, `mcpApps: false`,
`modelState.currentModelId: grok-4.6`.

Auth methods: `xai.api_key`, `cached_token`, `grok.com`.

`session/new` still returns top-level `models` plus
`_meta.x.ai/sessionConfig` (models *and* effort rungs as
`category: "model" | "mode"`), `_meta.x.ai/sessionDetail`,
`_meta.x.ai/schedulerBackgroundLoops: true`.
`emitConfigOptions` still sees empty typed SDK `ConfigOptions`.
Do not feed `sessionConfig` `category:"mode"` into the mode chip.

Fork `{sourceSessionId, sourceCwd, newCwd}` still returns
`newSessionId` (measured this pass).

### How the grok provider is designed today

Thin `acpagent.Spec` (`internal/provider/grok/grok.go`):

* Spawn: `defaultArgs` builds globals then
  `--no-auto-update agent --no-leader stdio`.
* Model/effort: argv `-m` / `--reasoning-effort`, rebuilt by
  `spawnArgs` / `ModelArgs` for per-session overrides.
* `session/new` / `session/load`: `{Cwd, McpServers}` only
  (`acp-go-sdk` `NewSessionRequest` already has `Meta` and
  `AdditionalDirectories`; we leave them empty).
* Thinking: `SetThinkingLevel` → `ErrThinkingLevelFixed`;
  `ThinkingLevel()` returns the spawn-argv value we *intended*.
* Models catalog: initialize `_meta.modelState` +
  `_x.ai/models/update` (slash and underscore aliases).
* Modes: static `default`/`plan` plus synthetic `auto` (0049).
* Extensions: `_x.ai/exit_plan_mode`, `_x.ai/ask_user_question`,
  models-update, MCP status/init, `_x.ai/session_notification`.
* Fork: `_x.ai/session/fork` (0092).
* Auth: 0085 still proposed; SafeAuthMethodIDs
  `cached_token`, `xai.api_key`; device-auth entry point exists.

Grok-build's ACP-client idiom (the pager) always sends a populated
`_meta` (permission seeds, optional `agentProfile`,
`askUserQuestion`). The agent reads `modelId` and `reasoningEffort`
from that map. We are the unusual client that sends none of it.

### Idiom and optimization notes (for the PLAN, not extra scope)

* Prefer grok's `_meta` keys over inventing top-level ACP fields.
  The SDK's `Meta map[string]any` is the right carrier
  (`NewSessionRequest`, `LoadSessionRequest`; `SetModel` already
  uses `rawRequest` and can pass `_meta`).
* Harvest applied effort from `session/new` / `session/load` /
  `session/resume` `models.availableModels[current]._meta.reasoningEffort`
  (or selected `sessionConfig` mode id) so `ThinkingLevel()`
  reports what grok applied, not what we put on argv.
* Do not parse `sessionConfig` effort rungs as session modes.
* If P1 drops per-session argv rebuilds, grok `Prewarm` starts
  covering model and thinking variants. Keep AlwaysApprove /
  PermissionMode / Sandbox on argv — those remain process-wide.
* `GROK_CONFIG` is the Codex-shaped overlay grok added so an ACP
  launcher need not write `config.toml`. Use it for *process*
  defaults we cannot express per session; use `_meta` for
  per-session model/effort.

### Related

* [0038](./0038-MADR-grok-acp-parity-assessment.md) /
  [0039](./0039-MADR-grok-acp-parity.md) — original ACP wiring
* [0049](./0049-MADR-grok-auto-mode.md) — synthetic auto; not replaced
* [0050](./0050-MADR-grok-cli-surface-drift.md) — argv placement
* [0052](./0052-MADR-thinking-levels-and-settings.md) — thinking
  ladder; grok spawn-only
* [0081](./0081-MADR-grok-1.0.3-surface-parity.md) —
  1.0.3 surface; `_meta` yolo/auto measured not taken; fork schema
  incomplete
* [0085](./0085-MADR-grok-acp-auth-method-wiring.md) — auth methods;
  still proposed; 1.0.5 catalog unchanged
* [0092](./0092-MADR-grok-1.0.4-surface-parity.md) — fork adopted;
  1.0.4 pin this record supersedes for the live binary
* [MADR 0074 §15](./0074-MADR-remote-provider-auth-from-phone.md) and
  [its approved P17–P22 plan](./0074-PLAN-remote-provider-auth-from-phone.md)
  — accepted credential-lifecycle follow-up pinned to this same Grok 1.0.5
  binary; it owns isolated device login, backup generations, logout, and flow
  ownership without reopening this record's ACP `_meta` decisions

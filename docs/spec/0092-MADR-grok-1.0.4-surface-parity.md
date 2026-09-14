---
status: accepted
date: 2026-08-15
decision-makers: Project Owner (scope and acceptance); Implementer (measurement)
consulted: none
---
<!-- markdownlint-disable MD013 MD024 MD060 -->

# Adopt grok 1.0.4's pinned session fork and re-pin the 0081 contract

## Context and Problem Statement

The grok provider last pinned its CLI/ACP contract against **1.0.3
(`1a29d5bc12d4`)** in
[0081](./0081-MADR-grok-1.0.3-surface-parity.md)
([0081-PLAN](./0081-PLAN-grok-1.0.3-surface-parity.md), Complete
2026-08-12). The binary on this host is now **grok 1.0.4
(`d846eb93d94d`) [stable]** at `/Users/saxsmith/.grok/bin/grok` →
`../downloads/grok-1.0.4-macos-aarch64` (symlink mtime 2026-08-15
12:30). `grok update --check --json` reports
`currentVersion=1.0.4`, `latestVersion=1.0.4`, `updateAvailable=false`.

This record asks: **does 1.0.4 change any remote-visible surface we
must add or modify to keep functional parity, and which new TUI or
config features are worth adopting?** The binary is the source of
truth. Official public pages
([changelog](https://x.ai/build/changelog),
[CLI reference](https://docs.x.ai/build/cli/reference),
[headless](https://docs.x.ai/build/cli/headless-scripting),
[modes and commands](https://docs.x.ai/build/modes-and-commands),
[settings](https://docs.x.ai/build/settings)) still describe **1.0.3**
as latest (crawled 2026-08-15). The notes that shipped with this
install — `~/.grok/CHANGELOG.md` dated **1.0.4 — 2026-08-13**, plus
the user guide under `~/.grok/docs/user-guide/` rewritten 2026-08-15
22:16 — are supporting evidence. Where they disagree with
`grok --help` or a live ACP probe, the binary wins.

The spawn vector from MADR 0050 / 0081 is **still valid** on 1.0.4:
`grok <globals> --no-auto-update agent --no-leader stdio`. Policy
flags (`--permission-mode`, `--sandbox`, `--tools`, `--allow` /
`--deny`, `--no-subagents`, `--disable-web-search`,
`--no-auto-update`, `--no-ask-user`) are still rejected after
`agent` with `unexpected argument`. `--always-approve` / `--yolo`
remain valid in both positions.

## Decision Drivers

* The live binary, not the public changelog, is the contract
  (AGENTS.md; MADR 0050 D2; MADR 0081).
* Adopt only surfaces that change remote session behaviour or
  operator control. TUI chrome, pager selection, and dashboard
  shortcuts stay out.
* Do not ship a flag or RPC whose effect on `agent stdio` has not
  been measured. 0081 already left `--tools` / `--allow` / `--deny`
  as typed config that 0050 measured as ACP no-ops.
* A measure-gated 0081 item that now has a winning probe is no
  longer "later" — it is the point of this record.
* Official pages that still name 1.0.3, `grok import`, or model id
  `grok-build` are docs drift, not work.
* Do not replace MADR 0049's synthetic `auto` with grok-native
  `autoMode` until a discrimination pair writes a file.

## Considered Options

* Measure 1.0.4, adopt the one newly pinned remote RPC (`_x.ai/session/fork`),
  re-pin the 0081 live contract, and leave TUI/hooks/config-only
  1.0.4 features out
* Implement every 1.0.4 changelog item and every newly documented
  config key (`StopCancelled`, web-search domain lists, session-search
  disable, follow-up steering, image/video kill switches)
* Treat 1.0.4 as a no-op patch and do nothing until the next
  flag-move incident

## Decision Outcome

Chosen option: "Measure 1.0.4, adopt the one newly pinned remote RPC
(`_x.ai/session/fork`), re-pin the 0081 live contract, and leave
TUI/hooks/config-only 1.0.4 features out", because the CLI/ACP
surface that 0081 already wired did not regress, the public
changelog does not even list 1.0.4, and the only remote-visible
gap that flipped from "schema incomplete" to "measured success" is
session fork.

This MADR is **accepted**. Implementation is
[0092-PLAN-grok-1.0.4-surface-parity.md](0092-PLAN-grok-1.0.4-surface-parity.md)
(Complete, 2026-08-15). Do not treat 0081-PLAN as still open: that
plan is Complete against 1.0.3.

Implementation outcomes:

* P1.1 fork + T-F1/T-F5/T-F2/T-F4/T-F3: **done.** Winning shape
  `{sourceSessionId, sourceCwd, newCwd}` returns `newSessionId`;
  a second process `session/load`s that id; grok `/fork` is
  `KindOp` / `OpFork`.
* P1.2 live pins: **done.** Headers name 1.0.4 (`d846eb93d94d`).
* P2.3 T-A: **measured, not taken.** 1.0.4 baseline requested
  permission and did not write; yolo/auto requested no permission,
  did not finish in 90s, and did not write. MADR 0049 auto still
  discriminates (unarmed prompts; armed writes).
* P2.4 T-S, T-C, T-P: **green.** `/compact` still silent. Argv
  still accepted. Subagent pin still passes.
* T-K: `--no-ask-user` added to the P4 argv forbid list; not
  emitted.

### Recommended uptake (proposed, not implemented)

Priorities are product impact against a *measured* 1.0.4 wire.

#### P1 — the 0081 item that is now unblocked

1. **Implement `_x.ai/session/fork` and remap grok `/fork` to
   `KindOp` / `OpFork`.**
   0081 P2.8 / Phase I stopped because the four listed shapes all
   failed with `missing field newCwd`, and the plan forbade guessing
   a fifth field. Live 1.0.4 against the same method:

   | params | result |
   | --- | --- |
   | `{sourceSessionId}` | `missing field sourceCwd` |
   | `{sourceSessionId, sourceCwd}` | `missing field newCwd` |
   | `{sourceSessionId, sourceCwd, newCwd}` | **OK** |

   Winning result (verbatim keys):

   ```json
   {
     "newSessionId": "01a00898-63ce-79d2-b608-2f2f2fa1b575",
     "chatMessagesCopied": 2,
     "updatesCopied": 2,
     "planStateCopied": false,
     "newCwd": "/tmp/grok104-acp-cwd",
     "parentSessionId": "01a00898-5291-7ad3-be30-328d09ef8caa"
   }
   ```

   `internal/provider/grok/live_fork_test.go` would have missed this
   even if `newCwd` had been tried: `forkSessionID` looks for
   `sessionId` / `result.sessionId`, not `newSessionId`. The
   implementer must read `newSessionId`.

   Implementation shape (same as 0081-PLAN Phase I, now with a
   pinned payload):

   * `provider.ForkSession` on `acpagent.session`: send
     `_x.ai/session/fork` with
     `{sourceSessionId: s.agentID, sourceCwd: s.cwd, newCwd: s.cwd}`.
     Ignore `ForkOptions.LastTurnID` and
     `DeferGoalContinuation` — the winning payload has neither.
   * Return
     `ForkResult{AgentSessionID: newSessionId, ForkedFromID: parentSessionId}`.
   * Remap grok `"fork"` from `KindNone` / `ReasonNoFork` to
     `{Kind: command.KindOp, Op: command.OpFork}`. Do not change
     other providers.
   * `cmdFork` (`internal/session/commands.go:986`) and
     `Manager.Fork` (`internal/session/manager.go:1842`) already
     exist. After `fs.Fork` they call `Create` with
     `StartOptions.AgentSessionID = newSessionId`, which for grok
     is a **new process** and ACP `session/load`
     (`acpagent.go:834-864`). In-process fork is not enough.
     A live `session/load` of the forked id on a second `Start`
     is the implement gate. If that load fails, keep `/fork`
     `KindNone` and update this MADR — do not invent a
     same-process attach path here.
   * Pass the daemon session cwd as both `sourceCwd` and `newCwd`.
     Do not invent a worktree. `--worktree` is still unsafe to
     append (0081 P4: optional value steals the `agent` token).

2. **Re-pin the live suite and comments to 1.0.4
   (`d846eb93d94d`).**
   `live_helpers_test.go`, `live_argv_test.go`,
   `live_setmodel_test.go`, `live_initializemeta_test.go`,
   `live_modelsupdate_test.go`, `live_command_test.go`,
   `live_commandcatalog_test.go`, `live_loop_test.go`,
   `live_sessionmeta_test.go`, `live_fork_test.go`, and the
   `grok.go` / `commandtable.go` headers still say 1.0.3. The
   behaviour they pin still holds (see "What the remote already
   surfaces"). The pin is the version string, the fork shape list,
   and `forkSessionID`.

#### P2 — grok-side fixes that change reliability, not our API

3. **Do not replace MADR 0049 `auto` because 1.0.4 fixed grok-native
   auto-allow honoring.**
   Bundled notes: "Auto permission mode now correctly honors your
   explicit always-allow grants and narrow allow rules from
   settings." That is a TUI/classifier fix for `--permission-mode
   auto` / `_meta.autoMode`. 0081 Phase H already accepted
   `_meta.yoloMode` / `_meta.autoMode` on the wire and **did not
   take** them: baseline requested permission and did not write;
   yolo/auto requested no permission, did not finish in 90s, and
   did not write. Re-run `TestLiveGrokSessionMetaDiscrimination`
   on 1.0.4 as confirmation only. Production `session/new` still
   sends `{Cwd, McpServers}`.

4. **Re-run the subagent live pin; do not rewrite the handler.**
   Bundled notes: "Subagent lifecycle events are now preserved even
   when delivered out of order." We already consume
   `_x.ai/session_notification`
   (`grok.go` `ExtensionNotifications`). 1.0.4 still emits that
   method name during `session/new`. If
   `TestLiveGrokSubagentSuppressedAndPromoted` stays green, there is
   no code change.

#### P3 — recorded, not taken, unless a later product request lands

5. **`[toolset.web_search] allowed_domains` / `excluded_domains`**
   (max 5, mutually exclusive; authoritative over the model's
   per-call list). Config-only; no CLI flag. Operators set this in
   `~/.grok/config.toml`. Do not add a `providers.grok.*` key until
   someone needs the daemon to write grok's config.

6. **`GROK_SESSION_ID` in tool and MCP child environments.**
   Grok injects this itself. HTTP/SSE MCP we forward already runs
   inside grok. No daemon env-plumbing.

7. **`--no-ask-user` is a real global** (accepted before `agent`,
   rejected after). It disables `ask_user_question`. The remote
   already handles `_x.ai/ask_user_question`. Emitting the flag
   would *remove* a phone-visible surface. Leave it off.

8. **`StopCancelled` hook event and `PreToolUse` `updatedInput`.**
   Documented in `~/.grok/docs/user-guide/10-hooks.md`. Live
   `initialize.agentCapabilities._meta.x.ai/hooks.blockingEvents`
   is still `pre_tool_use`, `stop`, `subagent_stop` — StopCancelled
   is observation-only, so it does not appear. The daemon still has
   no hook policy engine (0038 §3.5 / 0081 P4). Same MADR
   prerequisite as before.

9. **Host skill catalog growth is not a canonical-vocab change.**
   1.0.4 `available_commands_update` on this host still includes
   `compact`, `always-approve`, `context`, `session-info`,
   `feedback`, `deep-research`, `workflow`, `goal`, `loop`,
   `review`, plus host skills (`create-workflow`, `design`,
   `execute-plan`, `pr-babysit`, `resume-*`, `bundled:imagine`,
   and newly visible bundled `code-review`, `implement`,
   `create-skill`, `skill-design-principles`, `build-with-ai`).
   `hooks-*` did **not** appear in this 1.0.4 catalog (0081 P3.11
   recorded them on 1.0.3). Keep 0081's rule: do not add every
   skill to `command/specs.go`. `/review` stays `KindNative` gated
   on advertisement. `/compact` stays `KindNone`
   (`session/compact` is still method-not-found;
   `_x.ai/compact_conversation` did not return within 12s).

#### P4 — explicitly not taken

| Surface | Why not |
| --- | --- |
| Public changelog items that are TUI-only (session-info drag-copy, double-click word select, follow-up steer, dashboard Ctrl+4, Flameshot paste, composer keystroke hold, `/loop` 7-day expiry toast) | Pager chrome. No ACP method. |
| `GROK_SESSION_SEARCH` / `[features] session_search` | Disables grok's own session search index. The daemon owns the phone session roster. |
| `[ui] follow_up_behavior = "steer"` | Mid-turn TUI queue policy. The remote already has its own queue. |
| Image / video generation kill switches | Official modes page says config/env; 1.0.4 clap has no `--disable-image-generation` / `--disable-video-generation`. `--disallowed-tools imagine` (and siblings) *parse* as globals — same class as 0050's unmeasured ACP tool flags. `promptCapabilities.image` is still `false`. |
| `--minimal` / `--fullscreen` / `--oauth` / `--worktree` / `--json-schema` / `--max-turns` / `--agents` / memory / `grok agent serve` | Unchanged 0081 P4. |
| `grok import` | Still listed in official CLI reference. **1.0.4 has no `import` subcommand.** |
| Model id `grok-build` | Still recommended in official settings. `session/set_model` and `grok models` still reject / omit it. |
| `session/set_config_option` | Still `missing field configId`. Unpinned. |
| `session/set_model` + `reasoningEffort: "xhigh"` | Still returns `{"_meta":{"model":{"Ok":"grok-4.6"}}}` — silent-accept. `/thinking` stays spawn-only (`ErrThinkingLevelFixed`). |
| `session/list` / `session/resume` as the phone roster | Still grok-local TUI sessions mixed with mcremote temp dirs. `session/close` remains the only one we already call. |
| `_x.ai/fs/*`, `_x.ai/git/*` | Still grok-internal tools. |

### Consequences

* Good, because `/fork` on grok stops being a documented lie: the
  method exists, the schema is pinned, and Codex already taught the
  daemon how to wrap a provider fork.
* Good, because the 0081 spawn contract, model floor, models-update
  name, `--no-auto-update`, `/loop`, and `/review` do not need a
  rewrite — 1.0.4 did not move them.
* Good, because the public changelog lag is recorded, so a future
  reader does not treat "Latest v1.0.3" as evidence this host is
  stale.
* Neutral, because MADR 0049's synthetic auto and 0085's auth-method
  wiring are unchanged. 1.0.4 still advertises
  `xai.api_key`, `cached_token`, `grok.com`.
* Neutral, because `--tools` / `--allow` / `--deny` remain typed
  config that 0050 measured as ACP no-ops. 1.0.4 did not change
  that measurement.
* Bad, because 0081 Phase I's "do not guess `newCwd`" rule delayed a
  field the error message itself named. The confirmation tests
  below require the winning shape to be listed, so the next missing
  field is a new MADR, not another silent year.
* Bad, because implementing fork without fixing `forkSessionID` would
  look like a failed probe.

### Confirmation

Compliance is the test inventory below. P1 is not accepted until
the named tests exist and are green. Unit tests run in CI
(`go test ./…`). Live tests use `//go:build live_grok`, skip when
`grok` is not on `PATH`, and must be run on a host that has 1.0.4.

`grok --version` on the implementer's PATH is recorded in the live-test
file headers. `make pre-add-check` is required on every touched Go
file.

| ID | Decision | Test | File | Tag | Must fail when |
| --- | --- | --- | --- | --- | --- |
| T-F1 | P1.1 winning shape | extend `TestLiveGrokSessionForkShapes` with `{sourceSessionId, sourceCwd, newCwd}` and `newSessionId` parse | `live_fork_test.go` | `live_grok` | shape errors, or result id equals source id |
| T-F2 | P1.1 implement | `TestGrokForkIsOpFork` | `commandtable_test.go` | unit | grok `"fork"` is not `KindOp` / `OpFork` |
| T-F3 | P1.1 implement | live `sess.(provider.ForkSession).Fork(...)` | `live_fork_test.go` | `live_grok` | `AgentSessionID` empty or equal to the parent |
| T-F5 | P1.1 load gate | `TestLiveGrokForkLoadOnNewProcess` | `live_fork_test.go` | `live_grok` | second `Start` with `AgentSessionID=newSessionId` fails `session/load`. **Must pass before KindOp is remapped.** |
| T-F4 | P1.1 no LastTurnID | `TestForkRequestOmitsOptionalForkOptions` | `acpagent/fork_test.go` (new) | unit | request includes `lastTurnId` / `LastTurnID` / `deferGoalContinuation` or omits `newCwd` |
| T-P | P1.2 pin | comments + `TestLiveGrokArgvAcceptsEveryConfiguredFlag` still green | `live_argv_test.go` | `live_grok` | 1.0.4 rejects a current global |
| T-A | P2.3 auto not replaced | `TestLiveGrokSessionMetaDiscrimination` logs only; production `NewSession` unchanged | `live_sessionmeta_test.go` | `live_grok` | `session/new` grows `_meta.autoMode` / `yoloMode` without a new MADR |
| T-S | P2.4 subagents | existing `TestLiveGrokSubagentSuppressedAndPromoted` | `live_subagent_test.go` | `live_grok` | notifications vanish or the handler drops a terminal status |
| T-C | P3.9 compact | existing `TestLiveGrokCommandTableMatchesReality` `/compact` silence | `live_command_test.go` | `live_grok` | `/compact` starts returning text (re-probe; do not "fix" here) |
| T-K | P4 not taken | existing `TestDefaultArgsDoesNotEmitP4Flags` | `grok_test.go` | unit | argv grows `--worktree`, `--no-ask-user`, `--minimal`, `--cwd`, `--oauth` |

Existing tests that remain the regression net:
`TestStaticModelsFloor`, `TestSpecRegistersBothModelsUpdateNames`,
`TestLiveGrokModelsUpdateMethodName`,
`TestLiveGrokSetModelWireContract`,
`TestLiveGrokInitializeMetaWireContract`,
`TestLiveGrokCloseSucceeds`,
`TestLiveGrokCommandCatalogContainsDeepResearchAndWorkflow`,
`TestLiveGrokAutoDiscriminationPair`,
`TestDefaultArgsPutsGlobalsBeforeSubcommand`,
`TestProvidersDeclareEveryCanonicalCommand`.

## Pros and Cons of the Options

### Measure 1.0.4, adopt fork, re-pin, leave the rest out

* Good, because it closes the only 0081 measure-gate that 1.0.4
  actually opened, and it does so with a verbatim wire payload.
* Good, because it refuses to grow config surface for TUI settings
  the phone cannot use.
* Neutral, because `/fork` still will not copy plan state
  (`planStateCopied: false` on a fresh session) until a later
  probe with an active plan.
* Bad, because operators who wanted daemon-written web-search
  domain lists or image-gen kill switches will not get them from
  this decision.

### Implement every 1.0.4 changelog item and documented config key

* Good, because `~/.grok/CHANGELOG.md` would map 1:1 onto
  `providers.grok.*`.
* Bad, because most items are pager selection, dashboard
  shortcuts, or hook-engine features the daemon has explicitly
  deferred since 0038.
* Bad, because emitting `--no-ask-user` or unmeasured
  `--disallowed-tools imagine` would claim a capability 0050
  already taught us not to advertise without a discrimination
  test.

### Treat 1.0.4 as a no-op patch

* Good, because argv, models, `/loop`, `/review`, and
  `--no-auto-update` did not move.
* Bad, because `/fork` is now a working RPC behind a `KindNone`
  lock, and the live test that was supposed to notice this still
  walks four losing shapes and parses the wrong result key.
* Bad, because every live-test header would keep claiming 1.0.3
  after the binary on this host is 1.0.4.

## More Information

### Method

* Binary: `grok 1.0.4 (d846eb93d94d) [stable]`,
  `/Users/saxsmith/.local/bin/grok` → `/Users/saxsmith/.grok/bin/grok`
  → `../downloads/grok-1.0.4-macos-aarch64`.
* CLI: `grok --version` / `grok version --json`,
  `grok update --check --json`, `grok --help`, `grok agent --help`,
  `grok agent stdio --help`, every top-level subcommand `--help`,
  `grok models`, `grok inspect --json`, `grok mcp list`,
  `grok plugin list`. Hidden-flag matrix before and after `agent`
  (including `--no-auto-update`, `--yolo`, `--no-ask-user`,
  `--disable-image-generation`).
* ACP: `grok --no-auto-update --permission-mode default agent --no-leader stdio`
  driven as raw JSON-RPC (same vector as `defaultArgs` plus the
  existing permission-mode default). Frames saved under
  `/tmp/grok104-probe/`: `initialize`, `session/new`,
  `session/set_model` (`grok-4.6` OK; `grok-build` /
  `grok-code-fast-1` `unknown model id`), `session/set_mode`,
  `session/list`, `session/close` (`notResident` on a fake id),
  `session/resume`, `_x.ai/hooks/list` (requires `sessionId`),
  `_x.ai/session/fork` (three shapes), `session/set_config_option`,
  `session/compact`, `_x.ai/compact_conversation`, `_x.ai/fs/list`.
* Docs: official
  [changelog](https://x.ai/build/changelog) (latest still **v1.0.3
  · Aug 12, 2026**),
  [overview](https://docs.x.ai/build/overview),
  [CLI reference](https://docs.x.ai/build/cli/reference),
  [headless](https://docs.x.ai/build/cli/headless-scripting),
  [modes and commands](https://docs.x.ai/build/modes-and-commands),
  [settings](https://docs.x.ai/build/settings),
  [settings reference](https://docs.x.ai/build/settings/reference).
  Markdown permalinks (`….md`) 404. Installed
  `~/.grok/CHANGELOG.md` + `CHANGELOG.json` (1.0.4 — 2026-08-13)
  and `~/.grok/docs/user-guide/{04,05,10,15}-*.md`.

### What the remote already surfaces (unchanged and still correct)

| Surface | Code | 1.0.4 status |
| --- | --- | --- |
| stdio ACP, `--no-leader` | `grok.go` `defaultArgs` | still the right transport |
| Global flags before `agent` | `grok.go`; `live_argv_test.go` | still required |
| `--no-auto-update` unconditional | `defaultArgs` last globals | still accepted before `agent`, rejected after |
| Static floor `grok-4.6`, `grok-4.5` | `staticModels` | matches `grok models` and initialize `modelState` |
| `_x.ai/models/update` (slash) + underscore alias | `ExtensionNotifications` | slash name still emitted during `session/new` |
| `session/set_model` | `acpagent.session.SetModel` | `grok-4.6` / `grok-4.5` OK; `grok-build` / `grok-code-fast-1` rejected |
| `session/set_mode` plan/default | static modes + SDK | still the Build/Plan switch |
| `xhigh` on grok-4.6 only; dual default `xhigh`+`high`; applied default `high` | `modelsToCatalog` / `NormalizeThinkingLevels` | unchanged; mid-session set still silent-accept |
| Synthetic `auto` | `SynthesizeAutoMode`; MADR 0049 | still necessary |
| `/loop`, `/review` as `KindNative` | `commandtable.go`; `specs.go` | still advertised |
| `/compact` `KindNone` | `commandtable.go` | still silent over ACP |
| `session/close` on `Close` | `acpagent/session.go` | still succeeds; fake id returns `x.ai/closeOutcome: notResident` |
| MCP http/sse + status notifications | `buildMcpServers`; slash names | still emitted |
| Device-code auth | `device_auth.go` | `grok login --device-auth` still present |
| Auth methods | initialize | `xai.api_key`, `cached_token`, `grok.com` — 0085 still applies |

### New or changed vs 1.0.3 (measured)

| 1.0.4 surface | Evidence | Remote today |
| --- | --- | --- |
| `_x.ai/session/fork` with `newCwd` returns `newSessionId` | live RPC id=22 | `/fork` is `KindNone` (0081 stop) |
| Auto mode honors always-allow grants | bundled CHANGELOG | no daemon change; 0049 stays |
| Subagent events survive out-of-order delivery | bundled CHANGELOG | handler already registered |
| `StopCancelled` hook; `PreToolUse` `updatedInput` | bundled CHANGELOG; user-guide `10-hooks.md` | hooks engine still out of scope |
| `[toolset.web_search]` domain allow/deny | bundled CHANGELOG; user-guide `05-configuration.md` | config-only |
| `GROK_SESSION_ID` in tool/MCP env | bundled CHANGELOG | grok injects it |
| Session search can be disabled | bundled CHANGELOG (`GROK_SESSION_SEARCH` / `[features] session_search`) | TUI index; not our roster |
| `[ui] follow_up_behavior` `queue` \| `steer` | user-guide `05-configuration.md` | TUI queue |
| `--no-ask-user` global | clap accepts before `agent` | not emitted (would hide questions) |
| `--trust` accepted as global | clap | folder-trust; grok already trusted this repo |
| `hooks-*` slash commands absent from this ACP catalog | `available_commands_update` | 0081 recorded them; 1.0.4 on this host does not advertise them |
| Bundled skills `code-review`, `implement`, … | ACP catalog `_meta.scope=bundled` | host skills; do not canonicalize |
| Public changelog still "Latest v1.0.3" | [x.ai/build/changelog](https://x.ai/build/changelog) | docs drift |

### Official docs vs this binary

* [Changelog](https://x.ai/build/changelog) latest entry is **Grok
  Build 1.0.3 (Aug 12)**. 1.0.4 is only in the installed
  `~/.grok/CHANGELOG.md` (dated Aug 13) and `grok version`.
* [CLI reference](https://docs.x.ai/build/cli/reference) still lists
  `grok import [targets…]`. 1.0.4 has no `import` command (same
  drift 0081 recorded on 1.0.3).
* [Settings](https://docs.x.ai/build/settings) still recommends
  `default = "grok-build"`. ACP and `grok models` on this host do
  not.
* [Headless](https://docs.x.ai/build/cli/headless-scripting) still
  tells ACP users to pass `--no-auto-update`. We already do
  (0081 P1.3).
* [Modes and commands](https://docs.x.ai/build/modes-and-commands)
  documents `/imagine`, `/imagine-video`, and workflows-on-by-default
  (`[workflows] enabled = false` / `GROK_WORKFLOWS=0`). Those are
  TUI/skill/config surfaces; ACP `promptCapabilities.image` is
  still `false`, and this host advertises Imagine as
  `/bundled:imagine`.
* User guide `15-agent-mode.md` still documents `session/new`
  `_meta` (`yoloMode`, `autoMode`, `rules`, …) and the `x.ai/*`
  extension map. Public docs still do not.

### Related records

* [0081](./0081-MADR-grok-1.0.3-surface-parity.md) /
  [0081-PLAN](./0081-PLAN-grok-1.0.3-surface-parity.md) — the
  contract this release is measured against; Phase I fork stop is
  superseded by 0092 T-F1 / T-F5 / T-F3
* [0038](./0038-MADR-grok-acp-parity-assessment.md) /
  [0039](./0039-MADR-grok-acp-parity.md) — original ACP parity
* [0049](./0049-MADR-grok-auto-mode.md) /
  [0053](./0053-MADR-grok-auto-mode-silent-arm.md) — synthetic auto;
  still not replaced by `_meta.autoMode`
* [0050](./0050-MADR-grok-cli-surface-drift.md) — argv placement;
  still the spawn contract
* [0052](./0052-MADR-thinking-levels-and-settings.md) — thinking
  still spawn-only; `xhigh` silent-accept unchanged
* [0080](./0080-MADR-add-first-class-codex-collaboration-modes-and-app-server-command-parity.md) —
  `ForkSession` / `cmdFork` already exist for Codex
* [0085](./0085-MADR-grok-acp-auth-method-wiring.md) — auth-method
  catalog unchanged on 1.0.4
* [0023](./0023-MADR-canonical-slash-commands.md) — do not explode
  the vocab for host skills
* [0092-PLAN](./0092-PLAN-grok-1.0.4-surface-parity.md) —
  implementation order, the `session/load` gate, and stop conditions

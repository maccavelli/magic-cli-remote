# Implement grok 1.0.5 ACP `_meta` model and effort surface

Associated MADR: [0106-MADR-grok-1.0.5-surface-parity.md](0106-MADR-grok-1.0.5-surface-parity.md)

<!-- markdownlint-disable MD013 MD024 MD060 -->

- **Status**: **proposed** (2026-08-19) — for review; no production code in
  this commit
- **Date**: 2026-08-19
- **Keyed to**: repository HEAD at plan-write time; grok **1.0.5
  (`5115b46bc909`) [stable]** at `/Users/saxsmith/.grok/bin/grok`.
  Re-record `grok --version` at the top of the first live-test commit
  if the implementer's binary differs. If it is not 1.0.5, **stop**
  and update MADR 0106 rather than guessing a new schema.
- **Scope**: `internal/provider/grok` (spec meta, live pins, comments),
  `internal/provider/acpagent` (session/new|load `_meta`, spawnArgs,
  SetThinkingLevel, harvest helpers), `internal/provider/provider.go`
  (StartOptions comment), `internal/config/config.go` (Grok
  ReasoningEffort comment), `docs/config.md`, `README.md` grok rows,
  MADR 0106 status, this plan. No remote-protocol change. No mobile
  change. No new `providers.grok.*` config keys. Do not edit
  goose/codex/opencode/kilo/fake command tables. Do not edit
  `0081-PLAN` / `0092-PLAN` or reopen those todos.

## Goal

Make the grok provider tell the truth about grok 1.0.5: create-session
and resume model/thinking ride ACP `_meta` fields the binary actually
applies; spawn `-m` / `--reasoning-effort` stay accepted globals but
are no longer treated as the application path; mid-session `/thinking`
uses `session/set_model` `{_meta.reasoningEffort}` with a measured
read-back, not the 0052 top-level silent-accept trap.

Tests are a deliverable of every phase, not a follow-up. MADR 0106
Confirmation IDs (T-M1 … T-A) are the contract. A phase commit that
changes production code without its named tests is incomplete.

## MADR assessment (grounded)

The 0106 decision is **proposed as written**. This plan is the
execution of that option. It does not add architecture.

### What the MADR got right (code facts)

| Claim | Code fact |
| --- | --- |
| Production `session/new` sends only `Cwd` + `McpServers` | `internal/provider/acpagent/acpagent.go:870-873` |
| Production `session/load` sends only `Cwd` + `McpServers` + `SessionId` | `acpagent.go:853-857` |
| SDK already has `NewSessionRequest.Meta` and `LoadSessionRequest.Meta` | `acp-go-sdk@v0.13.5` `types_gen.go` (`map[string]any` `_meta`) |
| Typed `NewSessionResponse` has `Meta`, not top-level `models` | same file; grok's `models` block is dropped by the SDK; applied model/effort survive in `_meta.x.ai/sessionDetail` and `_meta.x.ai/sessionConfig` |
| `spawnArgs` rebuilds argv for `ThinkingLevel` and (when `Config.Model` is empty) `StartOptions.Model` | `acpagent.go:723-757` |
| Prewarm is claimed only when `spawnArgs` equals baked `cfg.Args` **and** cwd matches | `acpagent.go:778-785` |
| `SetThinkingLevel` returns `ErrThinkingLevelFixed` | `acpagent/session.go:770-777` |
| `SetModel` sends `{sessionId, modelId}` only | `session.go:793-821` |
| `rawRequest` already exists | `session.go:2341-2358` |
| `thinkingLevel` is stored on the session and treated as spawn-immutable | `session.go:142-145` |
| No `currentModelID` field on `session` | `session.go:55-148` — SetThinkingLevel cannot name `modelId` until one is stored |
| acpagent is grok-only | only `internal/provider/grok/grok.go:104-110` calls `acpagent.New` |
| `cmdThinking` already dispatches `OpSetThinkingLevel` and maps `ErrThinkingLevelFixed` to a "new sessions only" notice | `internal/session/commands.go:1098-1146` |
| `/thinking` is already `KindOp` / `OpSetThinkingLevel` on grok | `grok/commandtable.go:23-27` |
| Manager create/fork already pass `StartOptions.Model` and `ThinkingLevel` | `commands.go:1221-1222`; `manager.go:1923-1933` (fork load) |
| Live helper can attach `_meta` on `session/new` | `live_helpers_test.go` `startACP(t, extraNewMeta)` |
| `defaultArgs` still emits `-m` / `--reasoning-effort` from config | `grok.go:127-163` |
| P4 argv forbid list exists and already includes `--no-ask-user` | `grok_test.go:240-252`; **does not yet list `--no-plan`** |
| `TestSetThinkingLevelFixed` and `TestSpawnArgsThinkingPrecedence` encode the 0052 spawn-only contract | `acpagent/thinking_test.go:180-241` |
| README and `docs/config.md` still say reasoning effort is the CLI flag | `README.md:871`; `docs/config.md:96` |

### What the MADR under-specified (now plan gates, not new architecture)

1. **Applied model/effort cannot be read from typed `models`.** grok
   returns top-level `models.availableModels[]._meta.reasoningEffort`
   (probe). The Go SDK unmarshals `NewSessionResponse` without a
   `Models` field. Production harvest uses `_meta` only:
   `x.ai/sessionDetail.currentModelId` and the selected
   `x.ai/sessionConfig` option with `category: "mode"`. Do **not**
   feed those options into `session/set_mode` (0081 P2.9).

2. **`SetThinkingLevel` needs a stored current model id.** The
   measured RPC is `session/set_model` with the current `modelId` plus
   `_meta.reasoningEffort`. Harvest `currentModelID` at new/load from
   sessionDetail; `SetModel` updates it from `resp._meta.model.Ok` or
   the requested id.

3. **Read-back is mandatory.** `session/set_model` returns
   `{_meta:{model:{Ok}}}` for `_meta.reasoningEffort`, top-level
   `reasoningEffort`, and even `quantum`. Success is the
   `session/resume` snapshot, not the Ok envelope. Production
   `SetThinkingLevel` must `session/resume` `{sessionId, cwd}`
   **without** `_meta` (the measured idle read-back). If that resume
   errors, replays the transcript, or fails to show the requested
   effort, **stop** and update MADR 0106 — do not skip the read-back
   and do not invent a fifth method.

4. **Argv `-m` / `--reasoning-effort` stay in `defaultArgs`.** They
   still parse (0050 placement). They do not apply over ACP. Removing
   them is a later MADR; this plan stops *relying* on them and stops
   *rebuilding* argv for per-session model/thinking so prewarm can
   claim.

### What the MADR must not be read as

* "Delete `-m` / `--reasoning-effort` from `defaultArgs`." Placement
  tests stay. Application moves to `_meta`.
* "Send top-level `modelId` or `reasoningEffort` on `session/new`."
  Top-level `modelId` is ignored; top-level set_model effort is the
  silent trap.
* "Replace 0049 auto with `_meta.autoMode` / `yoloMode`." Production
  `_meta` in this plan contains only `modelId` and/or
  `reasoningEffort`.
* "Use `GROK_CONFIG` for per-session effort." Process overlay only
  (MADR P3.7).
* "Treat `session/resume` as the daemon resume path." Resume is a
  SetThinkingLevel *read-back*. `AgentSessionID` still uses
  `session/load`.
* "Feed `sessionConfig` category:`mode` into the mode chip."

## Scope

### In scope

MADR 0106 P1 items 1–5 and P2 item 6, plus T-K (`--no-plan` on the
existing P4 argv test) and P2 confirmation runs of the existing live
suite (no production in that run phase).

### Out of scope (MADR P3 / P4 — do not implement)

`GROK_CONFIG` / `GROK_CONFIG_PATH` as daemon config or spawn env,
`_meta.pluginDirs`, `additionalDirectories`, `hooks-*` in
`command/specs.go`, `GROK_FORCE_LOGIN_TEAM_ID`, stamping
`yoloMode`/`autoMode`, host skills in the canonical vocabulary,
`--no-plan` / `--no-ask-user` as emitted flags, worktree gc,
image/video caps, TUI chrome, `session/set_config_option`,
`session/list` as the phone roster, replacing `session/load` with
`session/resume`, `_x.ai/fs/*` / `_x.ai/git/*`, `grok agent serve` /
`headless` / `leader`, model id `grok-build`, 0085 auth work.

## Plan-level decisions (MADR left these to the plan)

These are execution choices, not new architecture:

1. **`Spec.SessionMeta` is the injection point.** Add to
   `acpagent.Spec`:

   ```go
   // SessionMeta, when non-nil, is copied onto session/new and
   // session/load `_meta`. Empty/nil omits the field. Grok is the
   // only Spec that sets this (MADR 0106).
   SessionMeta func(opts provider.StartOptions, cfg Config) map[string]any
   ```

   Do not hardcode `"modelId"` / `"reasoningEffort"` inside
   `Provider.Start`. Do not use `ConfigureSession` (it runs *after*
   `session/new`). Goose does not use acpagent.

2. **Grok builder is exactly:**

   ```go
   func grokSessionMeta(opts provider.StartOptions, cfg Config) map[string]any {
       meta := map[string]any{}
       model := strings.TrimSpace(opts.Model)
       if model == "" {
           model = strings.TrimSpace(cfg.Model)
       }
       if model != "" {
           meta["modelId"] = model
       }
       effort := strings.TrimSpace(opts.ThinkingLevel)
       if effort == "" {
           effort = strings.TrimSpace(cfg.ReasoningEffort)
       }
       if effort != "" {
           meta["reasoningEffort"] = effort
       }
       return meta
   }
   ```

   Precedence matches MADR 0052 A3.2 and spawnArgs *intent*, moved
   onto `_meta`. Omit a key rather than send `""`. Never insert
   `yoloMode`, `autoMode`, `pluginDirs`, or `agentProfile`.

3. **`Start` copies SessionMeta onto both RPCs when `len(meta)>0`:**

   ```go
   req := acp.NewSessionRequest{Cwd: cwd, McpServers: mcpServers}
   if p.spec.SessionMeta != nil {
       if m := p.spec.SessionMeta(opts, p.cfg); len(m) > 0 {
           req.Meta = m
       }
   }
   ```

   Same for `LoadSessionRequest`. Nil SessionMeta preserves today's
   empty `_meta` for a future acpagent user.

4. **`spawnArgs` always returns a copy of `p.cfg.Args`.** Per-session
   model and thinking no longer rebuild argv. `ModelArgs` stays on
   the grok spec so `TestSpecModelArgs*` keep passing; Start does
   not call it. Comment the prewarm block: thinking/model no longer
   bust the spare. `defaultArgs` still emits `-m` /
   `--reasoning-effort` from **config** (baked at `New`).

5. **Harvest applied state from `_meta` after new/load**, then
   overwrite `s.thinkingLevel` / `s.currentModelID` when the
   snapshot names them. If `_meta` lacks those keys, keep the
   intended values already stored from `opts`/`cfg` (set in Start
   before the RPC, as today). Prefer harvest when present — it is
   what grok applied.

6. **`SetModel` stays model-only** (no effort `_meta`). On success,
   set `s.currentModelID` to `resp._meta.model.Ok` if non-empty,
   else the requested id.

7. **`SetThinkingLevel` request is exactly**

   ```json
   {"sessionId":"<agentID>","modelId":"<currentModelID>","_meta":{"reasoningEffort":"<level>"}}
   ```

   Never a top-level `reasoningEffort`. After the Ok envelope, read
   back with `session/resume` `{sessionId, cwd}` (no `_meta`, no
   `mcpServers` — the probe shape). Parse applied effort from
   `models.availableModels[current]._meta.reasoningEffort` if that
   array is present, else the selected `x.ai/sessionConfig`
   `category:"mode"` id. If applied ≠ requested, return an error
   and do **not** mutate `s.thinkingLevel`. Closed session →
   `session closed`. Missing `agentID` or `currentModelID` →
   `thinking: missing session id or model`. Do **not** return
   `ErrThinkingLevelFixed`.

8. **`/thinking` table is unchanged.** `cmdThinking`'s
   `ErrThinkingLevelFixed` branch stays for any future locker;
   grok will not hit it once Phase C is green. Do not change the
   success notice (`Thinking level is now %s, from your next
   message.`) — grok applies before the next prompt, matching
   Codex wording closely enough. Do not special-case grok in
   `cmdThinking`.

9. **Live tests skip when `grok` is not on `PATH`.** They are not a
   CI gate. Run them on this host before marking a measure-gated
   phase done. Phase A is the implement gate for B and C.

10. **One phase → one commit.** Do not push unless asked.
    `git commit --no-edit` with no `-m` / `--message` / `-F`.
    Before `git add` of any Go file:
    `make pre-add-check FILES="…"`.

## Tests

This section is the implementation of MADR 0106 Confirmation. Copy
function names exactly so reviews can grep them.

### Rules

* **Unit first.** Every production change has a unit test that fails
  if the change is reverted. Live tests pin the binary; they do not
  replace the unit test.
* **One ID → one function** unless the MADR row says "extend".
  Extensions add subtests (`t.Run`) rather than silent new
  assertions in an unrelated test.
* **Skip, don't fail, without grok.** `t.Skip("grok not in PATH")`.
* **A then B then C.** T-M1 / T-E1 / T-E2 / T-T1 red on this host →
  no production `_meta`, no `SetThinkingLevel` rewrite. A red A is
  a documented stop, not a licence to "send the keys anyway".
* **Do not weaken T-A.** Production `NewSession` `_meta` in this
  plan must not grow `autoMode` / `yoloMode`. T-A is run-only in
  Phase D; the unit test `TestGrokSessionMetaOmitsPermissionSeeds`
  is the CI net.
* **Do not spend tokens in unit tests.** Live tests start real grok
  processes; run them at acceptance, not in a loop.

### Inventory

| ID | Function | File | When |
| --- | --- | --- | --- |
| T-M1 | `TestLiveGrokSessionNewMetaModelID` | `internal/provider/grok/live_newmeta_test.go` **new** | Phase A |
| T-E1 | `TestLiveGrokSessionNewMetaReasoningEffort` | same | Phase A |
| T-E2 | `TestLiveGrokSessionLoadMetaReasoningEffort` | same | Phase A |
| T-T1 | `TestLiveGrokSetModelMetaReasoningEffort` | `internal/provider/grok/live_thinking_test.go` **new** | Phase A |
| T-K | extend `TestDefaultArgsDoesNotEmitP4Flags` with `--no-plan` | `grok_test.go` | Phase A |
| T-P header | comments → 1.0.5 (`5115b46bc909`) | existing `live_*_test.go`, `grok.go`, `commandtable.go` | Phase A |
| T-M2 | `TestLiveGrokStartAppliesMetaModel` | `live_newmeta_test.go` | Phase B |
| T-E3 | `TestLiveGrokStartAppliesMetaEffort` | same | Phase B |
| T-B1 | `TestGrokSessionMeta` | `grok_test.go` | Phase B |
| T-B2 | `TestGrokSessionMetaOmitsPermissionSeeds` | `grok_test.go` | Phase B |
| T-B3 | `TestSpawnArgsIgnoresPerSessionModelAndThinking` | `acpagent/thinking_test.go` (replace `TestSpawnArgsThinkingPrecedence`) | Phase B |
| T-B4 | `TestAppliedModelAndEffortFromMeta` | `acpagent/session_meta_test.go` **new** | Phase B |
| T-B5 | `TestNewSessionRequestIncludesSessionMeta` | `acpagent/session_meta_test.go` | Phase B |
| T-T2 | `TestLiveGrokSetThinkingLevel` | `live_thinking_test.go` | Phase C |
| T-T2u | `TestSetThinkingRequestShape` | `acpagent/thinking_test.go` (replace `TestSetThinkingLevelFixed`) | Phase C |
| T-T2b | `TestSetThinkingLevelRequiresIdentity` | same | Phase C |
| T-A | existing `TestLiveGrokSessionMetaDiscrimination` | `live_sessionmeta_test.go` | Phase D **run only** — production `_meta` still must not include auto/yolo |
| T-P run | existing `TestLiveGrokArgvAcceptsEveryConfiguredFlag` | `live_argv_test.go` | Phase D **run only** |
| T-S | existing `TestLiveGrokSubagentSuppressedAndPromoted` | `live_subagent_test.go` | Phase D **run only** |
| T-C | existing `TestLiveGrokCommandTableMatchesReality` `/compact` | `live_command_test.go` | Phase D **run only** |
| T-F | existing fork live tests | `live_fork_test.go` | Phase D **run only** |

Existing tests that must stay green and are not rewritten except as
named above:
`TestStaticModelsFloor`, `TestSpecRegistersBothModelsUpdateNames`,
`TestDefaultArgsPutsGlobalsBeforeSubcommand`,
`TestDefaultArgsWithReasoningEffort`, `TestSpecModelArgs`,
`TestGrokReviewIsNativeWhenAdvertised`, `TestGrokForkIsOpFork`,
`TestProvidersDeclareEveryCanonicalCommand`,
`TestLiveGrokModelsUpdateMethodName`,
`TestLiveGrokSetModelWireContract` (ids, not effort),
`TestLiveGrokInitializeMetaWireContract`,
`TestLiveGrokCloseSucceeds`,
`TestLiveGrokCommandCatalogContainsDeepResearchAndWorkflow`,
`TestLiveGrokAutoDiscriminationPair`.

## Implementation Steps

### Ground rules (every phase)

1. One phase → one commit. Do not push unless asked.
2. Before `git add` of any Go file:
   `make pre-add-check FILES="…touched.go files…"`.
3. `git commit --no-edit` with no `-m` / `--message` / `-F`.
4. The binary is the contract. If a step's expected clap/RPC result
   disagrees with the implementer's `grok --version`, stop and update
   MADR 0106 rather than guessing.
5. Keep exactly one `in_progress` todo; mark `completed` only after
   the phase's **named tests** are written and green.
6. Live tests: `//go:build live_grok`, `package grok_test`. Raw-stdio
   probes use `startACP` / `startACPInit` from `live_helpers_test.go`.
   Provider-level probes use `grok.New` + `p.Ready()` skip.
7. Do not edit P4 flags into `defaultArgs`, mobile, or protocol docs.
8. Do not send `autoMode` / `yoloMode` on production `_meta`.

### Verified baseline (do not rediscover)

Measured 2026-08-19 against grok 1.0.5; recorded in MADR 0106.
Frames: `/tmp/grok105-probe/`.

| Claim | Where |
| --- | --- |
| Global policy flags still rejected after `agent` | hidden-flag matrix; `--no-plan` ACCEPT before / REJECT after |
| `--no-auto-update` accepted before `agent`, rejected after | already in `defaultArgs` |
| Exact daemon argv `-m grok-4.5 --reasoning-effort low --no-auto-update agent --no-leader stdio` → session grok-4.6 / high | probe P10 / defaultArgs-order rerun |
| `session/new` `_meta.modelId=grok-4.5` → current grok-4.5 | probe P8 |
| Top-level `modelId` on `session/new` ignored | probe P8 |
| `session/new` `_meta.reasoningEffort` xhigh/low selected-mode matches | probe P4 |
| `session/load` `_meta.reasoningEffort` applies | probe P6 |
| `session/resume` `_meta.reasoningEffort` applies | probe P6 |
| `session/set_model` `_meta.reasoningEffort` discriminated via resume | probe P5 |
| `session/set_model` top-level `reasoningEffort` does not stick | probe P5 |
| `GROK_CONFIG` `models.default_reasoning_effort` applies process-wide | probe P9 — **not taken** |
| Fork `{sourceSessionId,sourceCwd,newCwd}` still returns `newSessionId` | method inventory |
| `session/compact` method-not-found; `/compact` advertised | method inventory |
| Production `session/new` is still `{Cwd,McpServers}` | `acpagent.go:870-873` |
| `SetThinkingLevel` is still `ErrThinkingLevelFixed` | `session.go:775-777` |

---

### Phase A — Re-pin 1.0.5 and prove the wire before any production change

**MADR:** P1.5, T-M1, T-E1, T-E2, T-T1, T-K, T-P header
**Files:** `internal/provider/grok/live_helpers_test.go` (header
only), every `internal/provider/grok/live_*.go` header comment,
`grok.go` comments, `commandtable.go` comments,
`grok_test.go` (T-K), **new** `live_newmeta_test.go`, **new**
`live_thinking_test.go`. **No production behaviour change.**

Only start this phase if `grok --version` is 1.0.5. If not, update
MADR 0106 and stop.

#### Steps

1. Replace every "grok 1.0.4 (`d846eb93d94d`)" pin in those files
   with "grok 1.0.5 (`5115b46bc909`) [stable] (MADR 0106)". Do not
   rewrite the behavioural assertions of existing tests.

2. `live_helpers_test.go` file comment currently says the daemon
   vector is 1.0.4. Update to 1.0.5. Do **not** change the argv
   `startACPInit` uses
   (`--no-auto-update --permission-mode default agent --no-leader stdio`).

3. New `live_newmeta_test.go` (`//go:build live_grok`):

   **T-M1 `TestLiveGrokSessionNewMetaModelID`**
   * Subtest `argv-m-noop`: spawn with
     `exec.Command(bin, "-m", "grok-4.5", "--no-auto-update",
     "--permission-mode", "default", "agent", "--no-leader", "stdio")`,
     initialize, `session/new` `{cwd, mcpServers:[]}` (no `_meta`).
     Fail unless `currentModelID(result)` is `grok-4.6` (not 4.5).
   * Subtest `meta-modelId`: `startACP(t, map[string]any{"modelId":"grok-4.5"})`.
     Fail unless current model is `grok-4.5`.
   * Subtest `toplevel-modelId-ignored`: `startACPInit`, then
     `session/new` `{cwd, mcpServers, "modelId":"grok-4.5"}` **not**
     under `_meta`. Fail unless current model stays `grok-4.6`.

   Helper `currentModelID(result map[string]any) string`: prefer
   `result["models"].(map)["currentModelId"]`, else
   `result["_meta"]["x.ai/sessionDetail"]["currentModelId"]`.

   **T-E1 `TestLiveGrokSessionNewMetaReasoningEffort`**
   Table: omitted / `xhigh` / `low`. `startACP` with that `_meta`
   (nil for omitted). Fail unless
   `appliedEffort(result)` matches (`high`, `xhigh`, `low`).
   `appliedEffort`: grok-4.6 entry's `_meta.reasoningEffort` if
   `models.availableModels` exists, else selected
   `x.ai/sessionConfig` option with `category=="mode"`.

   **T-E2 `TestLiveGrokSessionLoadMetaReasoningEffort`**
   * Process 1: `startACP(t, {"reasoningEffort":"low"})`, capture
     `sessionId` + cwd from sessionDetail.
   * Close that process (`t.Cleanup` already kills).
   * Process 2: `startACPInit`, `session/load`
     `{sessionId, cwd, mcpServers:[], _meta:{reasoningEffort:"xhigh"}}`.
   * Fail unless `appliedEffort(load result)` is `xhigh`.
   * If load errors, `t.Fatal` the RPC error — do not skip.

4. New `live_thinking_test.go` (**T-T1**
   `TestLiveGrokSetModelMetaReasoningEffort`):
   * `startACP(t, nil)` (baseline high).
   * `session/set_model`
     `{sessionId, modelId:"grok-4.6", _meta:{reasoningEffort:"xhigh"}}`.
   * `session/resume` `{sessionId, cwd}` (no `_meta`).
   * Fail unless applied effort is `xhigh`.
   * Repeat with `_meta.reasoningEffort:"low"` → `low`.
   * Then top-level `{sessionId, modelId:"grok-4.6", reasoningEffort:"xhigh"}`
     (no `_meta`) after the session is at `low`. Fail unless applied
     effort is **not** `xhigh` (the silent trap). Log the Ok envelope.
   * If resume errors or starts streaming a prompt replay
     (`session/update` `agent_message_chunk` after resume, before the
     resume response), `t.Fatal` naming `session/resume` as unsafe
     read-back — Phase C is then forbidden.

5. T-K: add `"--no-plan"` to `TestDefaultArgsDoesNotEmitP4Flags`
   `forbidden`. Do not emit the flag.

#### If a Phase A live test is red

**Stop.** Do not start Phase B or C. Commit the tests as failing
evidence only if they `t.Fatal` with the RPC error/applied values
named. Update MADR 0106 Decision Outcome with the new wire. Do not
guess a third key name.

#### Tests (required deliverable)

* T-M1, T-E1, T-E2, T-T1 written and green (or red + MADR update).
* T-K green.
* Headers say 1.0.5.

#### Verification

```bash
make pre-add-check FILES="internal/provider/grok/grok_test.go internal/provider/grok/live_newmeta_test.go internal/provider/grok/live_thinking_test.go internal/provider/grok/live_helpers_test.go internal/provider/grok/grok.go internal/provider/grok/commandtable.go"
go test ./internal/provider/grok/ -count=1
go test -tags live_grok ./internal/provider/grok/ -run 'TestLiveGrokSessionNewMeta|TestLiveGrokSessionLoadMeta|TestLiveGrokSetModelMetaReasoningEffort' -count=1 -timeout 600s
```

#### Acceptance

The implementer's 1.0.5 binary matches MADR 0106's tables. Phase B
is allowed.

---

### Phase B — Send `_meta` on `session/new` and `session/load`; stop rebuilding argv

**MADR:** P1.1–P1.4, T-M2, T-E3, T-B1–T-B5
**Files:**
`internal/provider/acpagent/acpagent.go` (Spec field, Start RPCs,
spawnArgs, prewarm comment),
`internal/provider/acpagent/session.go` (`currentModelID` field;
harvest after new/load; SetModel updates current model),
`internal/provider/acpagent/session_meta.go` **new** (harvest +
request attach helpers),
`internal/provider/acpagent/session_meta_test.go` **new**,
`internal/provider/acpagent/thinking_test.go` (T-B3),
`internal/provider/grok/grok.go` (`SessionMeta: grokSessionMeta`),
`internal/provider/grok/grok_test.go` (T-B1, T-B2),
`internal/provider/grok/live_newmeta_test.go` (T-M2, T-E3),
`internal/provider/provider.go` (StartOptions.ThinkingLevel
comment),
`internal/config/config.go` (ReasoningEffort comment),
`docs/config.md:96`,
`README.md:869-871`.

Only start this phase if Phase A is green.

#### Production

1. `session_meta.go`:

   ```go
   func attachSessionMeta(meta map[string]any) map[string]any {
       if len(meta) == 0 {
           return nil
       }
       return meta
   }

   func appliedModelFromMeta(meta map[string]any) string { /* sessionDetail.currentModelId, else selected category=="model" */ }

   func appliedEffortFromMeta(meta map[string]any) string { /* selected category=="mode" id */ }
   ```

   Nested JSON from `acp.NewSessionResponse.Meta` is
   `map[string]any`. Type-assert conservatively; return `""` on
   mismatch. Do not import grok.

   Also a tiny `setModelRequest` stays in session.go; do not put
   thinking RPCs here yet.

2. `Spec.SessionMeta` as in decision 1. `Provider.Start`:
   * Compute `meta := attachSessionMeta(p.spec.SessionMeta(opts, p.cfg))`
     when SessionMeta is non-nil.
   * Set `NewSessionRequest.Meta` / `LoadSessionRequest.Meta`.
   * After a successful new/load, under `s.mu`:
     if `appliedModelFromMeta` non-empty, `s.currentModelID = that`;
     else if intended model (`opts.Model` then `cfg.Model`)
     non-empty, store that.
     Same for `thinkingLevel` via `appliedEffortFromMeta` vs
     intended effort (already assigned before the RPC at
     `acpagent.go:812-815` — keep that assignment, then overwrite
     from harvest when present).

3. Add `currentModelID string` on `session`, guarded by `s.mu`,
   next to `thinkingLevel`. Comment: last applied ACP model id
   (sessionDetail or SetModel), not spawn argv.

4. `SetModel`: after the existing Ok/Err checks, 
   `s.currentModelID = resp.Meta.Model.Ok` if non-empty else
   `model`. Do not send `_meta` from SetModel.

5. `spawnArgs`: delete the rebuild branches. Body:

   ```go
   func (p *Provider) spawnArgs(opts provider.StartOptions) []string {
       return append([]string{}, p.cfg.Args...)
   }
   ```

   `opts` remains on the signature so Start does not fork a second
   helper. Update the comment to MADR 0106: per-session model/effort
   are `_meta`; argv is the process baked at New. Update the
   claimWarm comment at `acpagent.go:778-782` — thinking/model no
   longer rebuild argv.

6. `grok.go`: set `SessionMeta: grokSessionMeta` on `spec`. Put
   `grokSessionMeta` in `grok.go` (same file as `defaultArgs`).
   Update `defaultArgs` comment: `-m` / `--reasoning-effort` remain
   globals for placement; ACP application is `_meta` (MADR 0106).
   Do **not** remove those flags from `defaultArgs`.

7. Docs in this same commit (they become false the moment Start
   sends `_meta`):
   * `provider.go:63-66`: grok applies ThinkingLevel on
     `session/new|load` `_meta.reasoningEffort`; mid-session is
     Phase C.
   * `config.go:505-506`: still emitted as `--reasoning-effort`
     (ACP no-op on 1.0.5); applied via `_meta.reasoningEffort`.
   * `docs/config.md:96`: same distinction, plus "live 1.0.5".
   * `README.md:869-871`: `args` / `model` / `reasoning_effort`
     rows name `_meta.modelId` / `_meta.reasoningEffort` as the
     application path; flags remain accepted no-ops. Bump the
     "live 1.0.3 floor" clause to 1.0.5.

   Do not mention GROK_CONFIG as something we set.

#### Tests (required deliverable)

* T-B1: table — empty opts+cfg → `len==0`; Model only; Thinking only;
  both; opts override cfg; whitespace-only omitted. Never contains
  `yoloMode`/`autoMode`.
* T-B2: even if someone later adds keys, this test currently
  `t.Fatal`s on those two names. Keep it.
* T-B3: `TestSpawnArgsIgnoresPerSessionModelAndThinking` — baked
  argv with `--reasoning-effort medium`; `StartOptions{ThinkingLevel:"low", Model:"grok-4.5"}`
  returns the baked argv unchanged.
* T-B4: fixture copied from `/tmp/grok105-probe/new-xhigh.json`
  `_meta` (xhigh selected) and `new-low.json` (low selected) and
  baseline high. Do not require the live binary.
* T-B5: `newSessionRequestForTest` or export a package helper
  `sessionOpenParams(cwd, mcp, meta)` that Start uses, and assert
  Meta is set iff len>0. Do not spin grok.
* T-M2: `grok.New(Config{})`, `Start(StartOptions{Model:"grok-4.5", CWD: temp})`,
  then a raw ACP `session/resume` **or** inspect
  `s.(interface{…})` — simplest: after Start, call `SetModel` is
  wrong. Add a test-only getter? Prefer: type-assert to a small
  exported method is overkill. Use live raw: after
  `Start(Model:"grok-4.5")`, the session's `ThinkingLevel` is not
  the model. Check model via a live `session/resume` through
  `rawRequest` is unexported.

  **T-M2 implementation:** keep it a provider-level test that
  starts with `Model:"grok-4.5"`, then uses `startACP`-style? That
  would be a second process. Better: add package-level
  `func currentModelIDForTest(s provider.Session) string` in
  acpagent behind a test file in grok_test? Cross-package:
  implement `provider` optional interface is too much.

  Deterministic approach: in `live_newmeta_test.go` T-M2, after
  `p.Start(..., Model:"grok-4.5")`, call
  `ms := s.(provider.ModelSession);` then we still don't read
  current. **Add `provider.ModelSession` has no getter.**

  Use the existing live helper pattern: don't use `p.Start` for
  T-M2's *observation*. T-M2 must exercise **production Start**.
  Add on `acpagent.session` an unexported field already harvested,
  and export via a narrow method on the Session used only in tests:

  Put this on `session` in session.go:

  ```go
  func (s *session) AppliedModelID() string
  ```

  That is a new public API. Avoid it.

  **Chosen:** T-M2 / T-E3 use `grok.New` + `Start`, then send
  `session/resume` the same way T-T1 does — but Start's conn is
  inside the session. The live grok_test package cannot call
  `rawRequest`.

  So T-M2/T-E3 observe through **already-exported** surface:
  * T-E3: `s.(provider.ThinkingSession).ThinkingLevel()` must be
    `low` after `Start(ThinkingLevel:"low")`. That is the product
    path and is enough if harvest works.
  * T-M2: there is no exported current-model getter. After Start
    with `Model:"grok-4.5"`, call `SetModel(ctx, "grok-4.5")`
    (idempotent, live pin already) — that does not prove new.

  Add a test-only file in **package acpagent** (not grok_test):
  `start_meta_test.go` cannot spawn grok without live tag.

  Live file in package `grok_test` can import acpagent but not
  unexported session fields.

  **Resolve:** export nothing new. T-M2 drives production Start
  then uses `session/prompt` `/session-info` and fails if the
  text does not contain `**Model:** grok-4.5`. `/session-info`
  was measured to return `**Model:** grok-4.6` on baseline
  (`/tmp/grok105-probe/session-info-text.txt`). That is a
  product-visible discriminator and does not require new getters.
  Timeout 40s as the probe. AlwaysApprove true so the slash
  command cannot block on permissions.

  T-E3 uses `ThinkingLevel()=="low"` after Start **and** the same
  `/session-info` is *not* required to mention effort (probe text
  does not). Harvest of sessionConfig is what fills
  `ThinkingLevel()`. Fail T-E3 if ThinkingLevel is `""` or `high`.

8. Existing `TestSpecModelArgs*` stay: they call `spec.ModelArgs`
   directly, which Start no longer uses. Do not delete them; add a
   one-line comment that Start applies model via SessionMeta.

#### Verification

```bash
make pre-add-check FILES="internal/provider/acpagent/acpagent.go internal/provider/acpagent/session.go internal/provider/acpagent/session_meta.go internal/provider/acpagent/session_meta_test.go internal/provider/acpagent/thinking_test.go internal/provider/grok/grok.go internal/provider/grok/grok_test.go internal/provider/grok/live_newmeta_test.go internal/provider/provider.go internal/config/config.go"
go test ./internal/provider/acpagent/ ./internal/provider/grok/ ./internal/config/ -count=1
go test -tags live_grok ./internal/provider/grok/ -run 'TestLiveGrokStartAppliesMeta' -count=1 -timeout 600s
```

#### Acceptance

`Start(Model:"grok-4.5")` surfaces grok-4.5; `Start(ThinkingLevel:"low")`
reports `low`; argv for those options is the baked default; production
`_meta` has no auto/yolo keys.

---

### Phase C — Mid-session `SetThinkingLevel`

**MADR:** P2.6, T-T2, T-T2u, T-T2b
**Files:**
`internal/provider/acpagent/session.go` (`SetThinkingLevel`),
`internal/provider/acpagent/thinking.go` **new** (request builder +
resume snapshot parse) *or* keep builders in `session_meta.go` —
prefer `thinking.go` next to the RPC, mirroring `fork.go`,
`internal/provider/acpagent/thinking_test.go`,
`internal/provider/grok/live_thinking_test.go` (T-T2),
`internal/provider/grok/commandtable.go` comment on `"thinking"`.

Only start this phase if Phase A T-T1 is green **and** Phase B is
green.

#### Production

1. New `thinking.go`:

   ```go
   func setThinkingRequest(agentID, modelID, level string) map[string]any {
       return map[string]any{
           "sessionId": agentID,
           "modelId":   modelID,
           "_meta":     map[string]any{"reasoningEffort": level},
       }
   }

   func resumeSnapshotRequest(agentID, cwd string) map[string]any {
       return map[string]any{"sessionId": agentID, "cwd": cwd}
   }
   ```

   Snapshot struct unmarshals `session/resume` result:

   ```go
   type grokResumeSnapshot struct {
       Models struct {
           CurrentModelID  string                   `json:"currentModelId"`
           AvailableModels []GrokAvailableModel     `json:"availableModels"`
       } `json:"models"`
       Meta map[string]any `json:"_meta"`
   }

   func (sn grokResumeSnapshot) appliedEffort(modelID string) string {
       for _, m := range sn.Models.AvailableModels {
           if m.ModelID == modelID && m.Meta.ReasoningEffort != "" {
               return m.Meta.ReasoningEffort
           }
       }
       return appliedEffortFromMeta(sn.Meta)
   }
   ```

2. Replace `SetThinkingLevel` in `session.go`:

   * Trim `level`; empty → `fmt.Errorf("thinking level required")`.
   * Lock: closed / agentID / cwd / currentModelID; if
     `level == s.thinkingLevel` return nil (cmdThinking also
     short-circuits; keep both).
   * `rawRequest(ctx, "session/set_model", setThinkingRequest(...), &setResp)`
     using the existing set_model Ok/Err envelope. Fail on Err.
   * `rawRequest(ctx, "session/resume", resumeSnapshotRequest(...), &snap)`.
     Fail on RPC error.
   * `got := snap.appliedEffort(modelID)`; if `got != level`
     return `fmt.Errorf("thinking level %q not applied (agent reports %q)", level, got)`
     and do not write `s.thinkingLevel`.
   * On match, `s.thinkingLevel = level`.
   * Never `ErrThinkingLevelFixed`.
   * Never put `reasoningEffort` next to `modelId` at the top level.

3. `commandtable.go` `"thinking"` comment: 1.0.5
   `session/set_model` `_meta.reasoningEffort` is mid-session;
   spawn argv is not the application path (MADR 0106). Keep
   `KindOp` / `OpSetThinkingLevel`.

4. Do not change `cmdThinking`, other providers, or mobile.

#### Tests (required deliverable)

* T-T2u `TestSetThinkingRequestShape`: `setThinkingRequest`
  has `_meta.reasoningEffort`, no top-level `reasoningEffort`,
  no `lastTurnId`. `resumeSnapshotRequest` has only `sessionId`
  and `cwd`.
* T-T2b `TestSetThinkingLevelRequiresIdentity`: `&session{}`
  returns an error that is **not** `ErrThinkingLevelFixed` and
  does not mutate thinkingLevel. Closed session errors with
  `session closed`.
* Delete or rewrite `TestSetThinkingLevelFixed` so it does not
  expect `ErrThinkingLevelFixed`.
* T-T2 `TestLiveGrokSetThinkingLevel`: `grok.New(AlwaysApprove)`,
  `Start` (no thinking override → high),
  `s.(provider.ThinkingSession).SetThinkingLevel(ctx, "low")`
  must return nil, `ThinkingLevel()=="low"`. Then `"xhigh"` if
  the live catalog includes it (skip xhigh if
  `ListModels` grok-4.6 has no such rung). Then `"quantum"` must
  error and leave the previous level. Do not send a prompt.

#### If T-T2 is red because resume is unsafe

**Stop.** Do not weaken the read-back. Update MADR 0106 with the
resume error. Leave `SetThinkingLevel` returning
`ErrThinkingLevelFixed` if you must revert this phase's production
function; do not ship Ok-envelope-only success.

#### Verification

```bash
make pre-add-check FILES="internal/provider/acpagent/session.go internal/provider/acpagent/thinking.go internal/provider/acpagent/thinking_test.go internal/provider/grok/commandtable.go internal/provider/grok/live_thinking_test.go"
go test ./internal/provider/acpagent/ ./internal/provider/grok/ -count=1
go test -tags live_grok ./internal/provider/grok/ -run TestLiveGrokSetThinkingLevel -count=1 -timeout 600s
```

#### Acceptance

`/thinking low` on a live grok session no longer prints "new
sessions only". `ThinkingLevel()` matches the resume snapshot.

---

### Phase D — Run-only regression net

**MADR:** T-A, T-P, T-S, T-C, T-F, existing pins
**Files:** none, unless a test header still says 1.0.4.

#### Steps

Run, do not rewrite:

```bash
go test -tags live_grok ./internal/provider/grok/ -count=1 -timeout 900s
```

If `TestLiveGrokSessionMetaDiscrimination` starts failing because
production `_meta` now has keys: that test sends *its own* raw
`session/new` via `startACP`, not `Provider.Start`. It must still
pass. If it fails for another reason, stop and update the MADR.

If argv placement, fork, compact silence, set_model ids, or
0049 auto pair regress, stop. Do not "fix" compact.

T-A extra assertion (add only if the existing test does not
already cover it): production Start's `_meta` is not visible to
that test. The unit `TestGrokSessionMetaOmitsPermissionSeeds` is
the CI net. Do **not** change `TestLiveGrokSessionMetaDiscrimination`
to send `reasoningEffort` — that would mix 0049 with 0106.

#### Verification

The command above, plus:

```bash
go test ./internal/provider/acpagent/ ./internal/provider/grok/ ./internal/command/ ./internal/session/ -count=1
```

`./internal/session/` because `cmdThinking` still compiles against
the new SetThinkingLevel error set. No session test should expect
grok to return `ErrThinkingLevelFixed` by constructing an acpagent
session; grep before this phase:

```bash
rg ErrThinkingLevelFixed --glob '*.go'
```

Leave fake/codex paths alone. If a grok-specific test expected
Fixed, update it in Phase C, not here.

#### Acceptance

Existing live pins green on 1.0.5. Auto still not replaced.

---

### Phase E — Close the pair

**MADR:** status
**Files:** `docs/0106-MADR-grok-1.0.5-surface-parity.md`,
`docs/0106-PLAN-grok-1.0.5-surface-parity.md`

1. MADR `status: accepted`, date today, Decision Outcome: this
   PLAN Complete. List phase commits. Do not silently rewrite the
   2026-08-19 probe tables.
2. This PLAN status **Complete**, with commit SHAs and
   `grok --version` as run.
3. Do not push.

## Verification (plan-wide)

| Gate | Command / criterion |
| --- | --- |
| Format/lint/vuln | `make pre-add-check FILES="…"` on every staged Go file, every phase |
| Unit | `go test ./internal/provider/acpagent/ ./internal/provider/grok/ ./internal/config/ ./internal/command/ -count=1` |
| Live (A) | T-M1 T-E1 T-E2 T-T1 |
| Live (B) | T-M2 T-E3 |
| Live (C) | T-T2 |
| Live (D) | `go test -tags live_grok ./internal/provider/grok/ -count=1 -timeout 900s` |
| Race | `go test -race ./internal/provider/acpagent/ ./internal/provider/grok/ -count=1` before the last phase commit |
| Binary | `grok --version` is 1.0.5 (`5115b46bc909`) or the MADR is updated first |

### Acceptance criteria (all must hold)

1. `Start(StartOptions{Model:"grok-4.5"})` selects grok-4.5 without
   depending on argv `-m`.
2. `Start(StartOptions{ThinkingLevel:"low"})` reports `ThinkingLevel()=="low"`
   without depending on argv `--reasoning-effort`.
3. `spawnArgs` is identical for empty vs thinking/model overrides.
4. Production `_meta` contains only `modelId` and/or
   `reasoningEffort`.
5. `SetThinkingLevel("low")` returns nil and reports `low`;
   `"quantum"` errors; top-level `reasoningEffort` is not sent.
6. `ErrThinkingLevelFixed` is no longer returned by grok's session.
7. `/fork`, `/compact` silence, 0049 auto, argv *placement*, and
   set_model unknown-id rejects still hold.
8. No `providers.grok.*` keys added. No mobile. No push.

## Rollout and Rollback

### Rollout

Ship behind the existing grok provider. No flag, no config key, no
protocol bump. Phones already send create-session model and
`/thinking`; those calls start working. Operators with
`providers.grok.model` / `reasoning_effort` set get `_meta` plus the
old flags (flags no-op; `_meta` applies).

Prewarm: sessions with per-session model/thinking can now claim the
spare when cwd matches. No config change required.
`providers.grok.prewarm` stays default false (MADR 0089 D5).

### Rollback

Revert the phase commits newest-first (E → A). A revert of C
restores `ErrThinkingLevelFixed` and the "new sessions only"
notice. A revert of B restores argv rebuilds and empty `_meta`;
create-session model/thinking become no-ops again on 1.0.5, which
is today's behaviour. Do not leave C in without B (SetThinkingLevel
needs `currentModelID` harvest).

If grok 1.0.6 moves `_meta.reasoningEffort`, live T-E1/T-T1 fail;
do not patch a third key in a hotfix — amend MADR 0106.

## More Information

* Probe frames: `/tmp/grok105-probe/` (not committed).
* grok-build: `session_setup.rs` `_meta.modelId`;
  `reasoning_effort.rs` `split_new_session_effort`;
  `acp_agent.rs` `set_session_model` →
  `parse_reasoning_effort_meta(args.meta)`.
* Prior pairs: [0081-PLAN](0081-PLAN-grok-1.0.3-surface-parity.md),
  [0092-PLAN](0092-PLAN-grok-1.0.4-surface-parity.md). This plan
  does not reopen them.

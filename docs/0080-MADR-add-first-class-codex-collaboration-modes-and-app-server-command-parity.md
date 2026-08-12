<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 -->

# MADR 0080: Add first-class Codex collaboration modes and app-server command parity

| field | value |
| --- | --- |
| status | **Accepted** |
| date | 2026-08-12 |
| deciders | Project Owner (scope and acceptance); Implementer (daemon, provider, protocol, and mobile design) |
| related | [MADR 0022](0022-MADR-plan-mode-parity.md), [MADR 0023](0023-MADR-canonical-slash-commands.md), [MADR 0028](0028-MADR-codex-provider.md), [MADR 0035](0035-MADR-codex-ui-ux-remediation.md), [MADR 0044](0044-MADR-auto-approve-modes.md), [protocol-v1.md](protocol-v1.md) |
| research baseline | Official OpenAI documentation and source searched 2026-08-12; local schema and live probes against **`codex-cli 0.147.0`** on macOS arm64 |
| implementation plan | [0080-PLAN-add-first-class-codex-collaboration-modes-and-app-server-command-parity.md](0080-PLAN-add-first-class-codex-collaboration-modes-and-app-server-command-parity.md) |

## Context and Problem Statement

magic-cli-remote already drives Codex through `codex app-server`, but its
remote command surface stops short of capabilities that the current Codex CLI
and app-server expose. The immediate visible gaps are `/plan` and `/mode`:

* `internal/provider/codex/commandtable.go` explicitly resolves both commands
  to `command.KindNone`, so the daemon advertises them as unavailable.
* `internal/provider/codex/mode_test.go` intentionally asserts that Codex has
  no plan mode.
* `internal/provider/codex/session.go` implements `provider.ModeSession`, but
  that implementation changes approval policy and sandbox policy. It exposes
  `default`, `read-only`, `auto`, and optionally `full-access`; it does **not**
  represent Codex's `default` and `plan` collaboration modes.
* `runTurn` sends model, reasoning effort, approval policy, and sandbox policy
  on `turn/start`, but no `collaborationMode`.
* `Provider.startEngine` initializes app-server with
  `capabilities.experimentalApi: false`. Codex 0.147 hides
  `collaborationMode/list`, `thread/settings/update`, and the
  `collaborationMode` field on `turn/start` unless that capability is true.
* The event bridge already translates `turn/plan/updated` into structured
  `TypePlan` events. That is execution-plan progress, not the control that puts
  Codex into Plan collaboration mode.

The naming collision is architectural, not cosmetic. There are two independent
controls:

| control axis | values in this repository today | effect |
| --- | --- | --- |
| **Autonomy / permissions** | `default`, `read-only`, `auto`, `full-access` | Changes approval and sandbox policy; `auto` and `full-access` may remove a human safety control. |
| **Codex collaboration mode** | `default`, `plan` | Changes the developer collaboration instructions and, for Plan, the reasoning-effort preset. It does not grant filesystem or command permissions. |

A user can validly be in `plan + read-only`, `plan + default`, or
`default + auto`. One `current_mode_id` cannot truthfully describe both axes.
Putting all six labels in one list would hide half the state; constructing the
Cartesian product would produce eight synthetic modes, complicate every
provider, and make `/plan off` capable of changing a user's permission posture.

The wider assessment also found that the Codex command table is behind the
installed protocol. Its `/goal` note says goals are not exposed even though
0.147 exports `thread/goal/{set,get,clear}`. Its `/diff` note says app-server
has no diff even though 0.147 exposes both `gitDiffToRemote` and
`turn/diff/updated`. The canonical vocabulary has no `/fast`, `/personality`,
`/review`, or `/fork` entries even though the official TUI surfaces every one
and the installed app-server has typed mechanisms for them. Codex also exposes
skills, apps, background-terminal, thread-management, and diagnostic surfaces
that could materially improve remote parity.

The problem to decide is therefore:

> How should magic-cli-remote add reliable `/plan`, `/mode`, `/diff`, `/goal`,
> `/fast`, `/personality`, `/review`, and `/fork` support for current Codex
> while preserving permission safety, provider-neutral command semantics,
> compatibility with older Codex versions, and a deliberate path to later
> Codex capabilities?

### Official Codex evidence

The authoritative presentation surface is OpenAI's
[Developer commands documentation](https://developers.openai.com/codex/cli/slash-commands).
The authoritative integration surface is the
[Codex app-server protocol](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md),
not the interactive terminal UI.

The official command documentation establishes these relevant behaviors:

* `/plan` enters Plan mode and may include inline prompt text. It is
  temporarily unavailable while Codex is already working.
* `/permissions` changes approval presets, including Auto and Read Only, and
  can show configured named profiles.
* `/model`, `/fast`, `/personality`, `/goal`, `/skills`, `/review`, `/diff`,
  `/status`, `/fork`, `/rename`, `/compact`, `/ps`, and `/stop` are distinct
  controls. In particular, Plan and permissions are not the same setting.
* `/fast` is catalog-driven and is hidden when the active model does not
  advertise a Fast service tier.
* `/personality` is model-gated and currently supports `friendly`,
  `pragmatic`, and `none`.
* `/goal` supports set, view, edit, pause, resume, and clear, with a maximum
  4,000-character objective.
* `/review` reviews the working tree; `/status` composes model, permissions,
  writable roots, token usage, and context capacity.

The current
[Codex TUI slash-command source](https://github.com/openai/codex/blob/main/codex-rs/tui/src/slash_command.rs)
also shows that command visibility is feature-, model-, platform-, or
run-state-gated. That source is useful for parity discovery, but it does not
turn a TUI command into an app-server RPC. Commands such as `/copy`, `/raw`,
`/keymap`, `/theme`, and `/pets` are local presentation actions and must not be
sent to the model as text or claimed as remote engine capabilities.

The official app-server documentation and protocol source establish that:

* `collaborationMode/list` lists collaboration-mode presets and is
  experimental.
* `thread/settings/update` applies settings to subsequent turns without
  adding transcript content and emits `thread/settings/updated` only when the
  effective settings change.
* `turn/start.collaborationMode` is experimental and overrides model,
  reasoning effort, and developer instructions for that request.
* Built-in presets omit their full developer instructions from
  `collaborationMode/list`. A client sends
  `settings.developer_instructions: null` to request Codex's built-in
  instructions rather than copying or maintaining the prompt template.
* The Plan preset selects medium reasoning effort and does not select a model.
* `review/start`, `thread/goal/{set,get,clear}`, `skills/list`, `app/list`,
  `thread/name/set`, `thread/fork`, `thread/compact/start`, `model/list`,
  `turn/steer`, and `turn/interrupt` are app-server methods.
* `review/start` accepts uncommitted-changes, base-branch, commit, or custom
  targets and inline or detached delivery. Inline delivery streams an ordinary
  turn plus `enteredReviewMode` and `exitedReviewMode` items.
* `model/list` carries `serviceTiers`, `defaultServiceTier`, and
  `supportsPersonality`; `turn/start` accepts stable `serviceTier` and
  `personality` overrides.
* `gitDiffToRemote {cwd}` remains available as a v1 compatibility method and
  returns `{sha,diff}`. It is useful on the installed release but is a weaker
  long-term dependency than the v2 APIs.
* `thread/backgroundTerminals/{list,terminate,clean}` and several settings
  operations remain experimental.

Relevant primary sources:

* [App-server API overview](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md)
* [v2 thread and settings types](https://github.com/openai/codex/blob/main/codex-rs/app-server-protocol/src/protocol/v2/thread.rs)
* [app-server request registry and experimental gates](https://github.com/openai/codex/blob/main/codex-rs/app-server-protocol/src/protocol/common.rs)
* [Plan built-in template](https://github.com/openai/codex/blob/main/codex-rs/collaboration-mode-templates/templates/plan.md)
* [Default built-in template](https://github.com/openai/codex/blob/main/codex-rs/collaboration-mode-templates/templates/default.md)

### Local 0.147.0 schema and live-probe evidence

The Plan, goal, settings, diff, and fork probes used a temporary `CODEX_HOME`;
they started threads but sent no model turns. Review is inherently a model
turn, so its separate probe used an ephemeral, read-only thread, a minimal
custom review instruction, and attempted immediate interruption. The valid
`review/start` response arrived with `status: inProgress`; by the time the
interrupt request was processed, Codex reported no active turn. This proves
the method and response path but is not a completed-review quality test.

| probe | result |
| --- | --- |
| `codex --version` | `codex-cli 0.147.0` |
| normal generated schema | `TurnStartParams` omits `collaborationMode`; client request union omits `collaborationMode/list` and `thread/settings/update` |
| `generate-ts --experimental` | Adds those methods and fields. `ModeKind` is exactly `plan \| default`. |
| current daemon handshake | Sends `experimentalApi: false`, explaining why the provider cannot call the methods today. |
| initialized with `experimentalApi: true` | Accepted; `collaborationMode/list` returned Plan then Default. |
| Plan catalog row | `{name:"Plan", mode:"plan", model:null, reasoning_effort:"medium"}` |
| Default catalog row | `{name:"Default", mode:"default", model:null, reasoning_effort:null}` |
| `thread/settings/update` to Plan | Returned `{}` and emitted effective mode `plan`, effort `medium`, and Codex's populated built-in Plan instructions. |
| `thread/settings/update` back to Default | Returned `{}` and emitted effective mode `default`, effort `null`, and built-in Default reset instructions beginning `# Collaboration Mode: Default`. |
| normal versus experimental schema | Normal 0.147 exports `thread/fork`, all three goal methods, `review/start`, `model/list`, and `turn/start.{serviceTier,personality}`. `generate-json-schema` names the v2 diff notification `turn/diff/updated`; the live v1 RPC `gitDiffToRemote` remains callable but is not a string in the generated bundle. Only `thread/settings/update` and collaboration fields/methods require experimental opt-in. |
| live model catalog | `gpt-5.6-{sol,terra,luna}` and `gpt-5.5` each advertised service tier `{id:"priority", name:"Fast", description:"1.5x speed, increased usage"}`; `gpt-5.2` advertised none. Only `gpt-5.5` reported `supportsPersonality:true` in this catalog. |
| live Fast setting | On `gpt-5.5`, `thread/settings/update {serviceTier:"priority"}` emitted effective `priority`; clearing with JSON `null` emitted effective `default`. The user-facing name and wire id are therefore not interchangeable. |
| live personality setting | `thread/settings/update` accepted `friendly` and `none` and emitted each as the effective personality. Generated `Personality` is exactly `none \| friendly \| pragmatic`. |
| live goal CRUD | A paused goal set returned the full goal and emitted `thread/goal/updated`; get returned the same value; clear returned `{cleared:true}` and emitted `thread/goal/cleared`. No model turn started. |
| generated goal state | `active \| paused \| blocked \| usageLimited \| budgetLimited \| complete`; objective, optional token budget, tokens/time used, and timestamps are returned. |
| live working-tree diff | Non-experimental `gitDiffToRemote {cwd}` returns `-32600` unless the cwd repository has at least one remote-tracking SHA (`refs/remotes/<remote>/<branch>`). Against an origin-backed temp repo it returned a SHA equal to that repository's `HEAD` and a unified patch containing both the tracked change and the untracked file. The earlier 43,808-byte MADR 0080 probe was the same method against this working tree, which already had `origin`. |
| live fork | After the source thread had a materialized rollout, `thread/fork {ephemeral:true}` returned a new id with `forkedFromId` and emitted `thread/started`; a brand-new, unmaterialized thread returned `no rollout found`. |
| current fork field defect | The provider sends `turnId`, but 0.147 schema requires `lastTurnId`. Live, unknown `turnId` was silently ignored and forked the full thread; unknown `lastTurnId` correctly failed. |
| live review recognition | Without experimental opt-in, an invalid thread produced `-32600 thread not found`, proving method registration. A valid ephemeral inline custom review returned `{turn:{status:"inProgress"}, reviewThreadId}`. |

The full update object accepted by 0.147 is:

```json
{
  "threadId": "<thread-id>",
  "collaborationMode": {
    "mode": "plan",
    "settings": {
      "model": "<effective-model>",
      "reasoning_effort": "medium",
      "developer_instructions": null
    }
  }
}
```

`settings.model` is required in the full `CollaborationMode` type even though
the catalog mask deliberately leaves model unset. The client must merge a mask
with the current effective model; it must not send a blank model or let Plan
silently change model selection.

The other measured request/response shapes are:

```text
gitDiffToRemote {cwd} -> {sha, diff}
thread/goal/set {threadId, objective?, status?, tokenBudget?} -> {goal}
thread/goal/get {threadId} -> {goal|null}
thread/goal/clear {threadId} -> {cleared}
thread/settings/update {threadId, serviceTier?} -> {}
thread/settings/update {threadId, personality?} -> {}
review/start {threadId, target, delivery?} -> {turn, reviewThreadId}
thread/fork {threadId, lastTurnId?, ephemeral?, ...} -> {thread, ...settings}
```

`TurnDiffUpdatedNotification` is
`{threadId, turnId, diff}` and describes the latest aggregate for one turn.
`ThreadGoalUpdatedNotification` adds nullable `turnId` plus the full goal.
`ThreadItem` includes `{type:"enteredReviewMode", id, review}` and
`{type:"exitedReviewMode", id, review}`. These shapes are generated from the
installed binary, not inferred from the TUI.

### Current command coverage and candidate parity

The official slash catalog is larger than a remote client should copy
verbatim. The correct disposition is based on whether a command has a safe
app-server or daemon equivalent, not merely whether it appears in the TUI.

| official Codex surface | current repository state | recommended disposition |
| --- | --- | --- |
| `/plan` | Explicitly unavailable; structured plan updates already render. | **Implement first** through the separate collaboration-mode axis, including inline prompt support. |
| `/permissions` | Equivalent controls exist under mcremote's Codex `/mode`, but the official spelling is absent. Codex's own TUI does not expose `/mode`; that is this repository's provider-neutral command. | **Implement first** as a Codex mapping to the existing autonomy modes; retain `/mode` as the provider-neutral spelling. |
| `/model` | Implemented through `ModelSession` and `turn/start.model`. | Keep; enrich catalog metadata for service tiers and personality support. |
| `/compact` | Implemented through `thread/compact/start`. | Keep. |
| `/clear`, `/new`, `/resume`, session list | Daemon owns new/clear/sessions and provider resume. | Keep daemon-owned; do not forward TUI commands. |
| `/rename` | Manager and Codex `thread/name/set` already exist, but no canonical slash entry. | Add a daemon command using the existing authorized rename path. |
| `/fork` | Codex `ForkSession` is implemented, but no canonical slash entry; its optional boundary field is wrong (`turnId` instead of `lastTurnId`). | **Implement in this decision.** Fix the field, add a canonical operation, create a managed child session, and report the new session id. |
| `/status` | `/context` reports usage only; metadata and modes already exist elsewhere. | Add a bounded daemon-composed status report; do not scrape TUI output. |
| `/diff` | Codex table says unavailable; non-experimental `gitDiffToRemote` works and `turn/diff/updated` is not consumed. | **Implement in this decision.** Use the explicit working-tree RPC, bounded for the remote frame; retain the v2 turn notification as a compatibility fallback. |
| `/review` | `review/start` and review lifecycle items are unused. | **Implement in this decision.** Add an inline review operation with uncommitted, base, commit, and custom target grammar. |
| `/goal` | Table incorrectly says unsupported; 0.147 exports stable goal RPCs and reports the `goals` feature stable and enabled. | **Implement in this decision.** Add set/view/edit/pause/resume/clear plus goal-state events, with explicit Plan and busy-turn guards. |
| `/fast` | Service-tier metadata is discarded and no session setter exists. | **Implement in this decision.** Add a catalog-gated setting using the advertised tier named Fast (`priority` on the measured catalog), never a hard-coded wire id. |
| `/personality` | Model `supportsPersonality` is discarded; settings accept the generated enum. | **Implement in this decision.** Add a model-gated next-turn setting for `friendly`, `pragmatic`, and `none`. |
| `/skills` | `skills/list` is unused. | Add discovery and safe prompt insertion; never pretend selecting a row is an RPC that activates a skill by itself. |
| `/apps`, `/plugins`, `/hooks`, `/mcp` | App/plugin/hook/MCP inventory exists in Codex, with mixed stable and experimental surfaces. | Add read-only diagnostics and prompt attachment in later decisions; installation, trust, auth, and config mutation remain out of this MADR. |
| `/agent`, `/subagents` | Subagent summaries render, but switching to child threads is not a command. | Defer to session-tree navigation; do not flatten agent threads into collaboration modes. |
| `/ps`, `/stop` | Experimental background-terminal methods are unused. | Later, version-gated and explicitly labeled experimental; terminate only exact process ids returned for the current thread. |
| `/archive`, `/delete` | Daemon already owns session lifecycle. | Keep as authenticated daemon actions; optional slash aliases may call those same paths, never raw Codex commands. |
| `/copy`, `/raw`, `/keymap`, `/vim`, `/theme`, `/title`, `/statusline`, `/pets` | TUI-only presentation. | Do not expose as Codex engine commands. Equivalent mobile presentation belongs in Flutter. |
| `/ide`, `/mention` | IDE/TUI composer context. Mobile already has its own attachment path. | Implement as native mobile context/attachment UX if needed, not slash forwarding. |
| `/experimental`, `/setup-default-sandbox`, `/sandbox-add-read-dir`, `/logout`, `/feedback`, `/app` | Mutates host/TUI/account state or transfers to another client. | Out of remote command scope; expose diagnostics or instructions only where useful. |
| `/approve` | Retries a Guardian/auto-review denial, distinct from ordinary permission responses. | Defer until Guardian denial state is modeled explicitly; never map it to a generic approval tap. |
| `/memories`, `/import`, cloud/worktree/side-chat commands | Separate persistent or environment-level products. | Separate MADRs if requested; do not couple them to Plan enablement. |

## Decision Drivers

* **Safety-state integrity.** Entering or leaving Plan must never arm or disarm
  auto-approval, broaden the sandbox, or lose the current permission mode.
* **Semantic parity.** `/plan` must invoke Codex's real collaboration-mode
  machinery, not merely prepend “please plan” to a user prompt.
* **Protocol truth.** TUI presence is discovery evidence, not proof that a
  command can be forwarded over app-server.
* **Current-version usefulness.** The design must work against the installed
  0.147 protocol and its experimental gating, not only against Codex `main`.
* **Graceful degradation.** An older or changed Codex must produce an honest,
  actionable unavailable reason rather than a missing menu, silent no-op, or
  literal slash prompt sent to the model.
* **Convergence after resume/restart.** The daemon, Codex thread settings, and
  phone must converge on the same confirmed collaboration state.
* **Provider compatibility.** Existing Grok/OpenCode plan modes and Goose/Codex
  permission modes must not change semantics.
* **Single command truth source.** Command routing, `/help`, autocomplete, and
  mobile controls must derive availability from the same live capabilities.
* **Catalog-driven settings.** Fast and personality availability must follow
  the active model's catalog metadata; display labels must not be mistaken for
  wire ids.
* **Bounded remote output.** Diff and review content can be large. A command
  must not create a frame the daemon will drop at its 1 MiB outbound ceiling.
* **Bounded remote surface.** Local presentation commands and host-global
  configuration mutations must not be exposed merely to increase a parity
  count.
* **Testability without token spend.** Catalog and settings behavior must be
  testable with fixtures and a no-turn live app-server probe.

## Considered Options

1. **Forward Codex slash text to app-server or the model.** Treat `/plan`,
   `/permissions`, and later commands as ordinary prompt text and rely on Codex
   to interpret them.
2. **Prompt-emulate Plan.** Keep app-server non-experimental and prepend a
   daemon-authored “do not edit; make a plan” instruction.
3. **Put Plan into the existing Codex permission-mode list.** Add `plan` and
   `default` beside `read-only`, `auto`, and `full-access` under one
   `ModeSession` and one `current_mode_id`.
4. **Expose a Cartesian product of collaboration and permission modes.** Offer
   names such as `plan/read-only`, `plan/auto`, and `default/full-access`.
5. **Replace Codex permission modes with collaboration modes.** Make `/mode`
   show only Default and Plan, and remove or hide the current autonomy selector.
6. **Add a second, explicit collaboration-mode capability and negotiate
   app-server experimental support.** Keep autonomy and collaboration state
   independent, map `/plan` to collaboration, map `/mode` and `/permissions` to
   autonomy, and expand other commands only through proven RPC or daemon
   capabilities.

## Decision Outcome

Chosen option: **Option 6 — add a separate collaboration-mode capability and a
version-negotiated app-server feature layer.**

This is the only option that represents Codex truthfully without weakening the
current safety controls or regressing providers where `SessionMode` already has
a different meaning.

### Locked decisions

| ID | decision |
| --- | --- |
| **D1** | **App-server RPC is authoritative.** Do not forward Codex TUI slash text and do not maintain a copy of Codex's Plan/Default prompt templates. `/plan` uses `collaborationMode/list`, `thread/settings/update`, and `turn/start.collaborationMode`; `developer_instructions` is `null` so Codex supplies its installed built-in instructions. |
| **D2** | **Negotiate experimental support deliberately.** Start the dedicated Codex app-server with `initialize.capabilities.experimentalApi: true`. Live 0.147 **accepts** that capability; the rejection payload was not observed, so downgrade classification is locked rather than inferred. Retry **once** with `false` only when initialize returns a JSON-RPC error whose `message` or stringified `data` matches `(?i)experimental`. Never retry transport EOF, timeout, context cancellation, spawn/exec failure, an already-false initialize, or any other JSON-RPC error. Extend `rpcErrorBody` with `Data json.RawMessage` so classification can inspect data. After either successful initialize, probe read-only `collaborationMode/list {}` once per engine generation. `-32601`, experimental-gate rejection, malformed required fields, or a catalog without both `plan` and `default` disables only collaboration for that generation with the exact reason in the stable-reason table. Unknown notifications remain safely ignored. |
| **D3** | **Maintain two independent axes.** Existing `provider.ModeSession`, `session_mode`, `session.set_mode`, and `SessionMode.dangerous` continue to mean operating/autonomy modes. Add `provider.CollaborationModeSession`, a distinct collaboration-mode event/state, and an authenticated `session.set_collaboration_mode` operation. Never encode one axis into the other's id or current value. |
| **D4** | **Keep canonical `/mode`; add official `/permissions`.** For Codex, `/mode [id]` continues to list/switch `default`, `read-only`, `auto`, and optionally `full-access`. Add `/permissions [id]` as a separate canonical command mapped to the same Codex autonomy capability and handler. Do not make it a global alias: Grok, OpenCode, and Kilo modes are collaboration/agent modes rather than permission presets. Goose `/mode` is still `KindNone` (“isn't wired up yet”), so this MADR maps Goose `/permissions` to `KindNone` with that same honest reason. A later Goose decision may wire both; this one must not advertise a working `/permissions` on Goose. |
| **D5** | **Map Codex `/plan` to the new axis only.** Add a command mechanism such as `KindCollaborationMode`; map Codex `/plan` to `plan`. Keep Grok/OpenCode `/plan` mappings on their existing `KindMode` path. `/plan off`, `/plan exit`, and `/plan stop` restore collaboration mode `default` while leaving the autonomy mode unchanged. `/mode plan` is not accepted for Codex; its notice points to `/plan`. |
| **D6** | **Build full mode requests by merging the catalog mask with session state.** `settings.model` is the effective current model. For Plan, `reasoning_effort` is the catalog preset (`medium` on 0.147). For Default, restore the user's explicit `/thinking` selection or `null` for provider default. Set `developer_instructions: null`. A mode preset must not silently change the model or permanently overwrite the user's reasoning preference. `/thinking` while Plan is active updates the stored preference only and emits an “applies when you leave Plan” notice; it does not override the Plan preset on the next turn. The thinking chip continues to show the stored preference, not the effective Plan preset. Every later `turn/start` that has collaboration support sends `collaborationMode: {mode, settings:{model, reasoning_effort, developer_instructions:null}}` plus today's `approvalPolicy`/`sandboxPolicy`. Omit top-level `effort` while Plan is active so the nested preset cannot fight a stale top-level rung. On Default, omit top-level `effort` when the user has no explicit preference; otherwise send that stored preference both nested and top-level. Keep top-level `model` as the effective model on both paths. |
| **D7** | **Persist and converge.** Store the selected collaboration-mode id separately in Codex session state and persisted daemon session metadata. Also persist the autonomy `mode_id` that Manager currently holds only in memory. This is an intentional resume fix: today a mid-session `auto` is lost on resume/`Create` because `Record` keeps only CWD/model/thinking. After this change, resume and fork re-seed both axes from the record. Reapply the full selected collaboration mode on every `turn/start`, just as approval/sandbox policy is reapplied today. A successful `thread/settings/update` accepts the target for the next turn; `thread/settings/updated`, when emitted, is the authoritative full effective state and refreshes clients. On RPC failure, retain and display the previous confirmed state. |
| **D8** | **Match Codex run-state and inline semantics, without changing KindMode `/plan`.** Collaboration `/plan`, `/plan off`, `session.set_collaboration_mode`, and the mobile Plan toggle are unavailable while a turn is active; return the existing stable `turn_busy` error and do not queue a hidden state change. The inline-prompt grammar is **Codex `KindCollaborationMode` only**. Grok/OpenCode/Kilo/fake `KindMode` `/plan` keeps MADR 0022 usage grammar: `/plan sideways` remains usage help and `internal/session/mode_test.go` stays green. For Codex: bare `/plan` and `/plan on` enter Plan; `off`, `exit`, and `stop` are reserved exit aliases and reject any leftover tokens with usage (no mode change); any other non-empty remainder is inline prompt text. Bare `/plan`/`on` while already in Plan is a same-mode no-op. `/plan <prompt>` while already in Plan does **not** no-op: skip a redundant settings RPC and submit the remainder. `/plan <prompt>` while idle first applies Plan successfully and then submits the remainder as the first planning request. It is one authorized action, has no duplicate user echo, accepts the same attachments as an ordinary prompt via `Manager.Prompt` (today a handled slash drops attachments), and sends no prompt if the mode change fails. |
| **D9** | **Add an additive protocol surface.** A collaboration-mode event carries the full available list when discovered and only the current id on later updates, mirroring the merge behavior of `session_mode` without overloading it. The set operation takes `{session_id, mode_id}` and uses distinct error codes for unsupported mode, invalid id, busy turn, and provider failure. Older clients ignore the event; older daemons simply do not expose the Plan control. Document the wire fields and compatibility behavior in `protocol-v1.md`. That edit must also rewrite the current sentences that say a `plan` mode id is “the read-only planning state on every provider that has one” and that clients enable `/plan` from `session_mode` — both become false for Codex. |
| **D10** | **Give mobile two controls.** Retain the current dangerous-aware autonomy selector. For Codex, label it as permissions/autonomy rather than implying Plan. Add a separate Plan/Default collaboration chip or toggle beside it, driven only by the new event. The Plan control has no dangerous confirmation; the existing confirmation remains attached only to `SessionMode.dangerous`. Hydration, replay, transcript caching, and reconnect must preserve both axes independently. `TranscriptCache` must persist control state even when `items` is empty; both the `_save` empty-item delete and the `hydrateFromCache` `cached.items.isEmpty` bail-out change. `_maybeApplyDefaultMode` / `SettingsStore.getDefaultSessionMode` must not call `session.set_mode("plan")` on Codex — a stored default of `plan` is ignored there. This MADR does not add a collaboration default-mode setting; new Codex sessions start Default. The settings-screen static floor may keep `plan` for Grok/OpenCode/Kilo. |
| **D11** | **Use runtime capability truth for commands.** Extend command session state and resolver availability so `/plan` appears only after the collaboration catalog is valid and the live session implements the new interface. Re-advertise `remote_commands` when that capability arrives, fails, or changes. `/help`, autocomplete, slash routing, and the mobile button therefore agree. Advertised unavailable reasons are the frozen strings in the stable-reason table; do not invent per-phase prose. |
| **D12** | **Correct stale Codex claims as capability work lands.** The Codex command table must not continue saying goals or diffs do not exist. Change a note only in the same phase that implements and tests its actual operation; until then, use the frozen reason `integration not wired` rather than claiming app-server lacks the feature. Notes must not contain `TODO` — `TestCommandTableHasNoInternalTODONotes` still fails on that substring. `TestCodexAdvertisesNoPlanMode` stays green: Plan is never an autonomy `SessionMode`. Only the comment “Codex has no plan agent” is updated. |
| **D13** | **The required command scope is explicit.** Acceptance requires `/plan`, `/mode`, `/permissions`, `/diff`, `/goal`, `/fast`, `/personality`, `/review`, and `/fork`. `/status`, `/rename`, `/skills`, and read-only app/MCP diagnostics remain ranked follow-ons. Background terminals, Guardian retry, memories, plugins/hooks mutation, cloud, and environment switching require separate decisions and remain out of this implementation. |
| **D14** | **Do not expose host-global or TUI-only actions as provider commands.** Copy/raw/theme/keymap/status-line/title/pets belong to the phone UI if desired. Logout, feedback, experimental flags, plugin installation, hook trust, and sandbox setup affect host-global state and require a separately authorized administrative design. |
| **D15** | **Implement `/diff` with the installed working-tree RPC and a v2 fallback.** Call non-experimental `gitDiffToRemote` with the provider-resolved session CWD only; never accept a caller-supplied path. Validate the returned SHA, render the unified patch, and clip on a UTF-8 line boundary to **256 KiB** with an explicit truncation notice so the JSON envelope remains below the 1 MiB outbound limit. `sha == HEAD` and untracked-file inclusion are measured 0.147 behavior, not universal assumptions. If the compatibility method returns `-32601`, disable it for that engine generation and use the latest cached `turn/diff/updated` patch when available, labeled “latest Codex turn”; otherwise report the capability unavailable. |
| **D16** | **Implement the complete documented `/goal` grammar over goal RPCs.** `/goal` reads; `/goal <objective>` creates or replaces an active goal; `/goal edit <objective>` changes its objective while preserving status; `/goal pause`, `/goal resume`, and `/goal clear` map to status updates or clear. Validate non-empty objectives at no more than 4,000 Unicode scalar values. Treat `blocked`, `usageLimited`, `budgetLimited`, and `complete` as engine-reported states, not user-set aliases. Mutations reject while a turn is active. Entering Plan rejects while a goal is active and says to pause it; goal creation/resume rejects while Plan is active. View, pause, edit of an already-paused goal, and clear remain available in Plan. |
| **D17** | **Make `/fast` catalog-driven and idempotent.** Add `/fast`, `/fast on`, and `/fast off`; bare `/fast` toggles. Resolve the tier whose display `name` is exactly `Fast` from the active model's `serviceTiers`; send its opaque id (`priority` in the measured catalog), never the word `fast`. The command is unavailable when no Fast tier exists. Off sends `serviceTier:null`; treat effective `null`, empty, or `default` as off. Persist the chosen tier and reapply it on `turn/start`. Use `thread/settings/update` for immediate confirmation when experimental support exists; otherwise announce next-turn application and rely on the stable turn field. A model switch revalidates and clears an unsupported tier. |
| **D18** | **Make `/personality` model-gated.** `/personality` shows provider default/current plus available values; `/personality friendly`, `/personality pragmatic`, and `/personality none` store the generated enum and reapply it on `turn/start`. `/personality none` sends the enum value `none`, which is not JSON `null` and is not “provider default.” This MADR adds no `/personality default` / clear-to-provider-default alias. Advertise the command only when the active model reports `supportsPersonality:true`; the measured default `gpt-5.6-sol` therefore does not offer it, while `gpt-5.5` does. Use `thread/settings/update` for immediate confirmation when available and the stable turn field otherwise. Switching to a model without personality support clears the override and re-advertises commands. |
| **D19** | **Implement `/review` as an inline review turn.** `/review` and `/review uncommitted` use `{type:"uncommittedChanges"}`; `/review base <branch>` uses `baseBranch`; `/review commit <sha>` uses `commit` with null title; `/review custom <instructions>` uses `custom`. Reject empty target values and a busy turn. Use `delivery:"inline"` so the review remains in the managed session; detached review requires separate child-thread ownership and is out of scope. Treat the returned turn like a normal busy turn, render entered/exited notices, stream the normal agent message, and use `exitedReviewMode.review` only as a fallback when no assistant review text was delivered, avoiding duplication. The review does not change collaboration, permission, Fast, or personality state. |
| **D20** | **Expose `/fork` through the existing authorized manager lifecycle and correct its wire field.** `/fork` branches the whole idle conversation; optional `/fork <turn-id>` sends `lastTurnId`, never the currently wrong `turnId`. Reject while a turn is active. Convert `no rollout found` on a never-materialized thread into “nothing to fork yet.” The returned native thread is resumed as a new managed session owned by the same device, and the source transcript receives its local session id/name. Preserve model, thinking, collaboration, permission, Fast, and personality selections. If a goal is active, use experimental `deferGoalContinuation:true`; without that capability, require the user to pause or clear the goal before forking. |
| **D21** | **Add capability operations rather than provider-name branches.** Extend the canonical registry with typed operations for goal, service tier, personality, review, and fork; keep the existing Diff operation. Codex maps them to optional provider interfaces. Other providers follow the explicit mapping table below — no implementer-chosen fallback. Add kilo to `internal/command/conformance_test.go` in the same change (it is missing today). Add `KindCollaborationMode` to that test's kind switch. Model changes, capability failures, and settings notifications trigger `remote_commands` re-resolution. |

### Provider command-table mappings for new specs

Every provider table must declare every new spec. Falling through to
`Spec.Default` is forbidden by conformance.

| Command | Codex | OpenCode / Kilo | Grok | Goose | Fake |
| --- | --- | --- | --- | --- | --- |
| `/plan` | `KindCollaborationMode` → `plan` | existing `KindMode` | existing `KindMode` | keep `KindNone` | existing `KindMode` |
| `/mode` | existing autonomy `KindMode` | existing `KindMode` | existing `KindMode` | keep `KindNone` | existing `KindMode` |
| `/permissions` | same autonomy handler as `/mode` | `KindNone`: agent/plan modes, not permission presets | `KindNone`: agent/plan modes, not permission presets | `KindNone`: same note as Goose `/mode` | `KindNone`: fake modes are not permission presets |
| `/diff` | typed diff op (wired in P5) | existing `KindOp` + `OpDiff` | keep `KindNone` | keep `KindNone` | existing `KindOp` + `OpDiff` |
| `/goal` | typed goal op (wired in P7) | keep `KindNone` | keep existing native `/goal` | keep `KindNone` | keep existing native `/goal` |
| `/fast` | typed service-tier op (wired in P6) | `KindNone`: no Fast service tier | `KindNone`: no Fast service tier | `KindNone`: no Fast service tier | `KindNone`: no Fast service tier |
| `/personality` | typed personality op (wired in P6) | `KindNone`: no personality setting | `KindNone`: no personality setting | `KindNone`: no personality setting | `KindNone`: no personality setting |
| `/review` | typed review op (wired in P8) | `KindNone`: no inline review RPC | `KindNone`: no inline review RPC | `KindNone`: no inline review RPC | `KindNone`: no inline review RPC |
| `/fork` | typed fork op (wired in P5) | `KindOp` + `OpFork` on existing `ForkSession` | `KindNone`: no fork over ACP | `KindNone`: no fork over ACP | `KindNone`: fake has no `ForkSession` |

Until a required Codex operation is wired, Codex uses the frozen
`integration not wired` reason rather than a false “app-server lacks this”
note.

### Stable unavailable reasons

These strings are the user-visible `remote_commands.reason` / `KindNone` notes
for this MADR. Substitute `<version>` with the negotiated Codex version
(for example `0.147.0`). Do not paraphrase them in tests or UI.

| Situation | Exact reason |
| --- | --- |
| Experimental initialize downgraded, or `collaborationMode/list` is `-32601` / experimental-gate | `codex <version> does not expose collaboration modes (experimental API unavailable)` |
| Catalog missing `plan`/`default`, duplicate/empty ids, or invalid required shapes | `collaboration catalog is invalid` |
| Required Codex operation not yet wired in this phase | `integration not wired` |
| Active model has no service tier whose display name is exactly `Fast` | `this model has no Fast service tier` |
| Active model `supportsPersonality` is not true | `this model does not support personality` |
| `gitDiffToRemote` is `-32601` and no cached turn patch exists | `working-tree diff is unavailable` |
| Review/goal/fork method-not-found for this engine generation | `codex <version> does not expose this command` |
| OpenCode/Kilo `/permissions` | `this agent uses /mode for agent modes, not permission presets` |
| Grok `/permissions` | `this agent uses /mode for agent modes, not permission presets` |
| Fake `/permissions` | `this agent uses /mode for agent modes, not permission presets` |
| Goose `/permissions` | same existing Goose `/mode` note |
| Grok/Goose `/fork` | `this agent can't fork a session over the remote` |
| Fake `/fork` | `this agent can't fork a session over the remote` |
| Non-Codex `/fast` | `this agent has no Fast service tier` |
| Non-Codex `/personality` | `this agent has no personality setting` |
| Non-Codex `/review` | `this agent has no inline review command` |

### Required implementation sequence

The decisions above should be delivered in dependency order. This sequence is
part of the decision so an implementation plan cannot accidentally expose a
control before its state or compatibility behavior exists.

1. **Protocol fixtures and capability negotiation**
   * Capture minimal 0.147 experimental fixtures for collaboration list,
     settings update, and settings notification; do not check in generated
     schema trees.
   * Enable experimental initialization with the D2 classifier, one downgrade
     retry, `rpcErrorBody.Data`, and per-engine-generation capability results.
   * Decode catalog masks strictly enough to reject missing ids, duplicate ids,
     unknown required shapes, or absent Default/Plan, while ignoring additive
     unknown fields.
2. **Provider collaboration state**
   * Add the provider interface and Codex session fields for catalog, current
     collaboration id, explicit user reasoning selection, and effective model.
   * Implement full mode construction, the D6 `turn/start` payload,
     `thread/settings/update`, notification handling, and per-turn convergence.
   * Check idle before a collaboration switch. Do **not** implement the Plan/goal
     matrix here — that lands with goals.
   * Keep all mutable fields under the existing session lock and preserve
     request/notification ordering under concurrent close, cancel, and engine
     loss.
3. **Daemon command registry and WebSocket operation**
   * Add the separate command mechanism and live resolver state.
   * Implement Codex `/plan` inline grammar from D8; leave KindMode `/plan`
     on the MADR 0022 usage path.
   * Implement `/mode` and Codex `/permissions` with truthful help text.
   * Add canonical specs, operations, dispatch arms, kilo conformance, and the
     explicit provider-table rows from the mapping table, including P3
     `integration not wired` placeholders.
   * Add the collaboration event and authenticated set operation, stable error
     codes, history/replay behavior, and the D9 `protocol-v1.md` rewrite.
4. **Flutter state and controls**
   * Add collaboration-mode models and reducer state without changing
     `SessionMode` or its equality/danger semantics.
   * Render an independent Plan control and preserve it through cache hydrate,
     resync, history replay, route disposal, and reconnect, including
     itemless control-only cache snapshots.
   * Ignore a stored default session mode of `plan` on Codex.
   * Keep optimistic UI off: display the new mode only after daemon
     acknowledgement/event, and retain the previous mode on error.
5. **Diff and fork**
   * Implement bounded `gitDiffToRemote`, cache `turn/diff/updated`, and
     downgrade the compatibility RPC on method-not-found.
   * Fix `turnId` to `lastTurnId`, add busy/materialization errors, preserve all
     session settings, and register the fork as a managed local session.
6. **Fast and personality**
   * Retain typed service tiers and `supportsPersonality` from `model/list`.
   * Add locked session state, immediate settings updates when supported,
     stable next-turn fallback, model-switch revalidation, command
     re-advertisement, and status notices.
7. **Goals**
   * Add `GoalSession`, goal notification decoding, manager state, resume
     hydration through `thread/goal/get`, complete slash grammar, and the
     Plan/busy guards from D16.
   * Surface bounded objective/status/usage state to reconnecting clients; do
     not expose goal prompt text in logs.
8. **Review**
   * Add `ReviewSession`, target parsing, busy-turn ownership, inline
     `review/start`, lifecycle rendering, output deduplication, cancellation,
     and completion/error cleanup.
9. **Ranked follow-ons in later decisions**
   * `/status`, `/rename`, `/skills`, and read-only app/MCP diagnostics.
   * Each gets its own runtime gate, command-table mapping, error behavior, and
     test evidence; none is part of this MADR's acceptance gate.

### Detailed acceptance behavior

| scenario | required result |
| --- | --- |
| New Codex 0.147 session | Autonomy modes and collaboration modes arrive as separate state. Default collaboration mode is selected. `/plan`, `/mode`, `/permissions`, `/diff`, `/goal`, `/fast`, `/review`, and `/fork` are advertised when their measured capabilities are present; `/personality` follows the selected model. |
| `/plan` while idle | App-server accepts Plan settings; UI shows Plan; next prompt carries Plan with current model and medium preset effort. Permission mode is unchanged. |
| `/plan off` | App-server receives Default with its built-in reset instructions; UI shows Default; explicit user thinking is restored; permission mode is unchanged. |
| `/plan explain the migration` | On Codex: Plan is applied before the text is submitted; the text is the first planning prompt and appears once. On Grok/OpenCode/Kilo: still usage help (MADR 0022). |
| `/plan` while already in Plan | Same-mode no-op; no settings RPC. |
| `/plan explain` while already in Plan | No settings RPC; remainder is submitted once. |
| `/plan off leftover` | Usage error; collaboration mode unchanged. |
| `/thinking high` while Plan is active | Stored preference becomes `high`; notice says it applies when leaving Plan; next `turn/start` still sends the Plan preset and omits top-level `effort`. |
| `/plan` while running | Stable `turn_busy`; no queued or partial mode change. |
| `/mode` | Lists only autonomy ids and marks the current one. It does not list `plan`. |
| Stored mobile default `plan` on a new Codex session | Ignored; session stays Default collaboration. Grok/OpenCode/Kilo still apply that default through `session.set_mode`. |
| `/personality none` | Sends enum `none`, not JSON `null`. |
| `/permissions read-only` | Uses the existing safe mode path; collaboration state remains unchanged. |
| `/mode auto` while Plan is active | Dangerous confirmation behavior is unchanged; after success the state is `collaboration=plan`, `autonomy=auto`. |
| Invalid collaboration id | Rejected locally; no RPC; previous state retained. |
| Experimental method unavailable | `/plan` is advertised unavailable with a version/capability reason; `/mode` and existing Codex operation continue working. |
| Malformed catalog/notification | Logged with method and Codex version, no panic, no false current-mode update, and no raw developer instructions sent to the phone. |
| Resume | Persisted collaboration selection is re-established and sent on the next turn even if Codex emitted no initial settings notification. |
| `/diff` with the measured untracked MADR | Returns a patch whose base SHA is `HEAD`, includes the untracked file, stays below the configured output cap, and says when clipped. It never accepts a path argument. |
| `/diff` after compatibility RPC removal | Uses the latest cached turn patch and labels its narrower scope, or reports unavailable; no literal command reaches the model. |
| `/goal ship the release` | Creates an active persisted goal and publishes its state. `/goal` reads it; pause/resume/edit/clear deterministically map to the goal RPCs. |
| Active goal plus `/plan` | Plan entry is refused with an instruction to pause the goal. `/goal resume` is likewise refused in Plan. No autonomous continuation crosses the mode boundary. |
| `/fast` on `gpt-5.6-sol` | Sends catalog id `priority`, reports Fast on, and reapplies it on later turns. `/fast off` clears the override and accepts effective `default` as off. |
| `/fast` on `gpt-5.2` | Command is advertised unavailable because that measured model has no Fast service tier. |
| `/personality` on `gpt-5.5` | Lists and sets `friendly`, `pragmatic`, or `none`; the next turn carries the setting. |
| `/personality` on `gpt-5.6-sol` | Command is unavailable because the measured catalog says `supportsPersonality:false`. Switching between these models re-advertises the command. |
| `/review` | Starts one inline review turn with the parsed target, streams progress/output, blocks concurrent prompts, and restores idle without changing other settings. |
| `/fork` | Creates and registers a new managed session with `forkedFromId` equal to the source. A provided boundary uses `lastTurnId`; a fresh unmaterialized source gets an actionable notice. |
| `/fork` with an active goal | Uses deferred goal continuation when negotiated; otherwise requires pause/clear and performs no fork. |
| Old phone | Ignores additive collaboration events and continues using autonomy modes and chat normally. |
| Old daemon | New phone sees no collaboration capability and hides/disables Plan without affecting the existing mode selector. |

## Consequences

### Positive

* Codex Plan becomes a real engine-enforced collaboration mode with the same
  built-in instructions as the installed CLI.
* `/mode` becomes useful immediately for Codex because its already-implemented
  autonomy modes are no longer masked by `KindNone`.
* Users gain the official `/permissions` vocabulary without changing the
  provider-neutral `/mode` contract.
* Plan changes cannot accidentally alter approval or sandbox posture.
* The daemon stops treating Codex's TUI catalog as a remotely forwardable
  command API and gains a repeatable method for assessing new features.
* Runtime capability failures degrade one feature instead of disconnecting the
  provider or making all commands suspect.
* Diff, goal, Fast, personality, review, and fork become real typed operations
  instead of literal prompts or inaccurate “not exposed” notices.
* Model catalog metadata determines Fast/personality truth, so a model switch
  cannot leave a stale control presented as working.

### Negative

* A second mode axis adds provider, daemon, protocol, persistence, and Flutter
  state instead of reusing the existing event and operation.
* The collaboration/settings path opts the dedicated app-server connection
  into an experimental API.
  The method shapes may change between Codex releases and therefore require
  schema fixtures, live probes, and graceful downgrade handling.
* Sending collaboration settings on every turn slightly enlarges each
  `turn/start` request.
* `gitDiffToRemote` is a legacy compatibility RPC. Supporting exact
  working-tree diff on 0.147 introduces a monitored dependency that may need a
  future v2 replacement.
* Explicit `/diff` can carry repository content to the paired phone and history
  ring. Output must be bounded, and users should treat diffs as potentially
  sensitive.
* Review and active goals can consume model tokens without an ordinary prompt;
  the UI and notices must make those actions explicit.
* The word “mode” remains overloaded in product language. UI labels and help
  text must consistently call one axis **Plan/collaboration** and the other
  **Permissions/autonomy**.
* Exact TUI parity is intentionally not a goal: local UI commands and
  host-global mutations remain unavailable remotely.

### Neutral or follow-up effects

* `SessionMode` remains unchanged, so Goose/Grok/OpenCode and existing phones
  do not need migration logic.
* Codex's populated built-in developer instructions may be large, but they stay
  inside app-server/provider state. Wire events expose id, label, and bounded
  description only, never the prompt template.
* A future generic provider may implement the collaboration interface. It is
  optional and does not require every provider to expose two axes.
* Goals remain a separate state machine from Plan. The mutual-exclusion rules
  are deliberate remote safety policy, not a claim that Codex cannot store both
  values internally.
* Review stays inline. Detached review and historical-boundary fork remain
  available in the app-server protocol but need additional session-tree UX.

## Confirmation

The decision is confirmed only when tests for every required command pass and
the live probes prove the installed Codex behavior. Tests are part of the
implementation, not post-implementation cleanup. Phase commits and the
decision-to-test matrix live in
`docs/0080-PLAN-add-first-class-codex-collaboration-modes-and-app-server-command-parity.md`
under **Implementation log**. Token-bearing live suites are
`make live-codex-turn` and `make live-codex-review`; `make live-codex` stays
no-model-turn.

### Go unit and integration tests

1. **Schema/catalog decoding**
   * Decode the measured 0.147 Plan/Default list.
   * Ignore additive unknown fields.
   * Reject empty ids, duplicate ids, missing Plan or Default, and invalid
     reasoning-effort types without panicking.
   * Assert requests always contain `{params:{}}`; Codex rejects omitted params
     on list methods.
2. **Experimental negotiation**
   * Experimental initialize success exposes collaboration capability.
   * A JSON-RPC initialize error whose message or data matches
     `(?i)experimental` performs exactly one non-experimental retry.
   * Transport EOF, timeout, cancel, spawn failure, and unrelated JSON-RPC
     initialize errors do not retry.
   * `rpcErrorBody` decodes `data`.
   * `-32601`/gate rejection disables only collaboration commands for the
     engine generation with the frozen experimental-unavailable reason.
   * Engine replacement clears the prior probe result and probes once again.
3. **Request construction**
   * Plan includes current model, preset effort, and null developer
     instructions. Top-level `effort` is omitted.
   * Default includes current model, restored explicit effort or null provider
     default, and null developer instructions.
   * `/thinking` during Plan updates stored preference only.
   * Neither collaboration switch changes approval or sandbox fields.
   * Every `turn/start` includes the selected collaboration mode after switch,
     resume, and simulated engine replacement.
4. **State and concurrency**
   * Successful update changes current collaboration id.
   * Failed update retains the previous id and emits one error/notice.
   * `thread/settings/updated` reconciles to the engine's effective value.
   * Close/cancel during an update does not deadlock, leak a waiter, emit after
     close, or resurrect stale state.
   * Plan and autonomy switches can occur sequentially in either order without
     one overwriting the other.
5. **Command resolution and routing**
   * Codex advertises `/mode` and `/permissions` from autonomy state and `/plan`
     only from collaboration capability.
   * Grok/OpenCode/Kilo `/plan` still use their existing `ModeSession` path and
     the 0022 usage grammar.
   * Codex `/plan`, `/plan on`, each off alias, leftover after off, already-in-
     Plan no-op vs inline submit, attachments through `Manager.Prompt`, and
     busy-turn behavior are deterministic.
   * `/plan <prompt>` sends no prompt when the mode switch fails and echoes the
     user action once on success.
   * Provider command-table conformance covers every added canonical command,
     including kilo, and every frozen reason.
6. **Wire/store compatibility**
   * Encode/decode full-list and current-only collaboration events.
   * Old JSON without collaboration fields loads unchanged.
   * Persisted selection survives daemon restart and resume.
   * New error codes are documented and included in protocol coverage tests.
7. **Model metadata, Fast, and personality**
   * Decode service-tier id/name/description, default tier, and
     `supportsPersonality` from catalog fixtures while ignoring additive
     fields.
   * Resolve Fast by catalog display name and prove the request uses its opaque
     id, not `fast`; reject duplicate Fast rows and empty ids as malformed.
   * Cover `/fast`, `on`, `off`, bare toggle, idempotent repeats, effective
     `default` normalization, an unsupported model, and re-advertisement after
     model changes.
   * Cover personality list/set for all generated enum values, invalid values,
     unsupported-model gating, clearing on model switch, and both immediate
     settings confirmation and stable next-turn fallback. `/personality none`
     sends enum `none`, not JSON `null`.
   * Assert every later `turn/start` carries the confirmed service tier and
     personality and that neither setting alters collaboration or permissions.
8. **Diff**
   * Assert `gitDiffToRemote` receives only the provider-resolved session CWD,
     validates a hexadecimal SHA, and returns the full patch below the cap.
   * Cover empty diffs, invalid SHA, multi-byte UTF-8 at the 256 KiB boundary,
     line-boundary clipping, explicit truncation text, and a response whose
     envelope remains below the WebSocket frame limit.
   * Decode and replace aggregate `turn/diff/updated` state by turn id; do not
     concatenate repeated aggregate notifications.
   * On `-32601`, call the compatibility RPC only once per engine generation,
     use and accurately label a cached turn diff, or return unavailable when no
     fallback exists.
9. **Goals**
   * Decode set/get/clear responses and updated/cleared notifications, including
     every generated engine status and additive fields.
   * Cover view, create/replace, edit, pause, resume, and clear; empty and
     4,001-scalar objectives; a 4,000-scalar multi-byte objective; absent goal;
     token budget/usage; and status formatting.
   * Prove mutations return `turn_busy` during a turn and make no RPC.
   * Prove active-goal/Plan mutual exclusion in both directions, while view,
     clear, pause, and paused edit retain the permissions allowed by D16.
   * Resume calls `thread/goal/get`, publishes one bounded state event, and
     clears cached state when Codex reports no goal.
10. **Review**
    * Parse exact request fixtures for uncommitted, base branch, commit SHA, and
      custom instructions; reject missing/empty arguments before any RPC.
    * A review owns the normal busy-turn lifecycle, rejects concurrent review or
      prompt submission, supports interrupt/cancel, and always restores idle on
      success, error, disconnect, and cancellation.
    * Entered/exited review items produce lifecycle notices. Normal agent text
      wins over `exitedReviewMode.review`; the fallback appears exactly once
      only when no assistant review text arrived.
    * Assert `delivery:"inline"` and prove review does not modify collaboration,
      permissions, model, thinking, Fast, personality, or goal state.
11. **Fork**
    * Full-thread fork omits both boundary fields; boundary fork sends
      `lastTurnId` and never `turnId`. An unknown boundary propagates a useful
      provider error instead of silently forking the whole thread.
    * Cover busy rejection, `no rollout found`, engine error, returned
      `forkedFromId`, source ownership, new managed-session registration, and a
      single source transcript notice containing the local child id.
    * The child preserves model, thinking, collaboration, permissions, Fast,
      and personality without mutating the source.
    * Active goal sends `deferGoalContinuation:true` only when negotiated;
      otherwise the manager requires pause/clear and sends no fork request.
12. **Dynamic command resolution**
    * Required commands appear only when both their typed session capability and
      runtime prerequisites are true; unavailable reasons are deterministic.
    * Catalog/model/settings changes re-resolve `/fast` and `/personality` once
      without duplicate command events.
    * Method-not-found disables only the affected command for the current engine
      generation. Replacing the engine permits a fresh probe.
    * No requested command is ever forwarded as literal slash text.

### Flutter tests

* Model parsing and value equality distinguish collaboration state changes.
* Transcript reducer merges a current-only event without discarding the list
  and replaces the list when a full event arrives.
* Cache hydration, resync, and history replay retain both axes.
* Codex renders separate Plan and Permissions controls; Grok/OpenCode/Goose
  retain their current selector behavior.
* Plan toggle is disabled while running and shows provider/daemon failures
  without optimistic state drift.
* Permission danger confirmation still triggers only from
  `SessionMode.dangerous`; Plan never triggers that dialog.
* Predictive back, semantics labels, narrow-screen layout, and large text do
  not make either control unreachable.
* An old-daemon fixture with no collaboration event hides Plan cleanly.
* A stored default session mode of `plan` is ignored on Codex and still
  applied on Grok/OpenCode/Kilo.
* Itemless cache snapshots persist collaboration/autonomy catalogs and
  current ids; `hydrateFromCache` no longer bails on empty `items`.
* Goal parsing and reducer tests cover full events, current-only updates,
  clearing, reconnect hydration, all statuses, bounded objective display, and
  token/time usage without exposing the objective in diagnostic logs.
* Remote-command fixtures add diff, goal, Fast, personality, review, and fork;
  model changes add/remove Fast and personality controls without stale
  autocomplete entries.
* Diff transcript tests preserve fenced patch formatting, display the base SHA,
  and make truncation unmistakable at narrow widths and large text.
* Review lifecycle tests avoid duplicate final output and keep cancel/stop
  reachable. A successful fork exposes the returned child as a separately
  selectable managed session.

### Live Codex tests

Add a `live_codex`-tagged no-model-turn suite. Every case uses a temporary
`CODEX_HOME`; diff uses a temporary Git repository so acceptance does not
depend on this working tree:

1. Launch the configured Codex binary and record `codex --version`.
2. Generate or inspect both normal and experimental schemas and assert the
   expected gate: collaboration/settings are experimental, while goal, review,
   fork, diff, service tier, and personality shapes are present normally.
3. Initialize with `experimentalApi: true`, call
   `collaborationMode/list {}`, and assert Plan and Default are present.
4. Start a read-only, ephemeral temporary thread.
5. Update to Plan using the returned current model; assert the response and
   effective settings notification report Plan, medium effort for the measured
   preset, and non-empty server-populated Plan instructions.
6. Update to Default; assert effective Default and non-empty reset instructions.
7. List models. Select catalog rows dynamically rather than assuming a model
   inventory. If a row advertises a Fast tier, set its exact id and clear it;
   assert the settings notifications normalize to on and off. If a row supports
   personality, set `friendly` then `none` and assert both notifications.
8. Set a paused goal, get it, update it, and clear it. Assert the full response
   and notification sequence and that no `turn/started` event occurs.
9. In a temporary repository with a tracked change and an untracked file, call
   `gitDiffToRemote`. Assert SHA equals that repository's `HEAD`, both changes
   appear in the patch, and no path outside that repository appears.
10. Materialize a rollout without a model turn by setting/clearing a paused
    goal, fork it, and assert `forkedFromId`. Send a known-invalid
    `lastTurnId` and require an error, proving the installed field is honored;
    never send the obsolete `turnId` field.
11. Send no `turn/start`, assert the suite observed no model-turn lifecycle,
    terminate app-server, and remove all temporary homes/repositories.

Add a separate `live_codex_review`-tagged acceptance test because review is
inherently token-bearing. It starts an ephemeral read-only thread, submits one
minimal custom inline review, and waits for the review turn to reach a terminal
state. Assert the response carries `reviewThreadId`, entered/exited review
lifecycle is coherent, assistant/fallback output is non-duplicated, and the
thread can still accept or cleanly reject an interrupt according to its current
state. This test is opt-in, records model/version, has a bounded timeout, and
never runs in ordinary unit or pre-commit loops.

The live suites must fail loudly on protocol drift. They may skip only when the
Codex binary is absent or the explicit live-test environment contract allows a
skip; “experimental method disappeared” is a failure on an acceptance host.

### Repository gates

Before each implementation commit:

* run `gofmt`, `golint`, and `govulncheck` through `make pre-add-check` for
  changed Go files;
* run focused Go tests, `go test -race ./...`, and `staticcheck ./...`;
* run `dart format --output=none --set-exit-if-changed .`, `flutter analyze`,
  and `flutter test` under `apps/mobile` for Flutter phases;
* run protocol documentation/conformance tests and provider command-table
  conformance tests;
* run the no-model-turn live Codex suite once at acceptance and the explicitly
  authorized token-bearing review suite once at final acceptance, never in the
  normal unit-test loop.

## Pros and Cons of the Options

### Option 1: Forward slash text

* Good, because it is superficially small.
* Good, because it would inherit any model-recognized natural-language
  behavior without a new protocol type.
* Bad, because app-server does not advertise or execute the TUI slash parser for
  remote prompts.
* Bad, because `/plan` could become literal user text and would not establish
  Codex's collaboration-mode instructions.
* Bad, because success, state, availability, and errors could not be confirmed.
* Bad, because it repeats the advertisement-is-not-capability error corrected
  by MADR 0023.

### Option 2: Prompt-emulate Plan

* Good, because it avoids the experimental app-server API.
* Good, because it could work on older Codex releases.
* Bad, because the daemon would maintain behavior that belongs to Codex's
  versioned built-in templates.
* Bad, because it would miss Codex's strict mode lifecycle, default reset
  instructions, request-user-input behavior, and Plan effort preset.
* Bad, because a prompt is weaker and less inspectable than an engine setting.

### Option 3: Mix Plan into permission modes

* Good, because it reuses `ModeSession`, `session_mode`, `session.set_mode`, and
  the existing Flutter selector.
* Bad, because one current id cannot show both collaboration and autonomy.
* Bad, because switching Plan after `auto` would visually hide that approvals
  remain automatic.
* Bad, because `/plan off` would need to guess which safety mode to restore.

### Option 4: Cartesian-product modes

* Good, because one id could encode both state values.
* Bad, because it invents product concepts Codex does not expose.
* Bad, because every new permission or collaboration value multiplies the
  catalog and tests.
* Bad, because canonical `/mode`, `/plan`, and mobile labels become unwieldy.
* Bad, because it would pressure other providers into Codex-specific state.

### Option 5: Replace permission modes

* Good, because Codex `/mode` would look like Plan/Default in one simple list.
* Bad, because it removes already-implemented remote control over approval and
  sandbox behavior.
* Bad, because it makes dangerous autonomy state less visible and creates a
  regression in MADR 0044's safety design.
* Bad, because official Codex itself keeps `/plan` and `/permissions` distinct.

### Option 6: Separate collaboration capability

* Good, because it exactly models the two independent Codex controls.
* Good, because it keeps safety confirmation and persistence attached to the
  correct state.
* Good, because it supports real app-server Plan semantics and truthful
  runtime gating.
* Good, because the same capability-oriented command registry can expose goal,
  diff, review, fork, service tier, and personality without Codex-specific
  branches in daemon routing.
* Good, because existing provider behavior remains compatible.
* Bad, because it requires additive interfaces, events, persistence, command
  resolution, and mobile UI.
* Bad, because the first implementation depends on an experimental app-server
  surface and must carry explicit drift handling.
* Bad, because the required scope spans stable, experimental, and legacy
  app-server surfaces and therefore needs per-capability degradation rather
  than a single Codex-supported flag.

## More Information

### Review lock-ins

A codebase review on 2026-08-12 found several implicit decisions. They are
now folded into D2, D4, D6–D12, D18, and D21 rather than added as D22+.
Option 6 and the required command scope did not change. Status stays
`Accepted` on that clarified text.

### Capability-selection rule for future Codex features

Use the following order when evaluating another official slash command:

1. **Is it only TUI/IDE presentation?** Implement it in Flutter if useful; do
   not create a provider capability.
2. **Does the daemon already own equivalent state or lifecycle?** Route the
   command to the existing authorized manager operation.
3. **Is there a stable/current app-server method or notification?** Add a small
   optional provider interface and advertise it only when implemented.
4. **Is the method experimental?** Require explicit app-server capability
   negotiation, a harmless runtime probe where possible, version-bearing logs,
   fixtures, and an acceptance-host live test.
5. **Does it mutate host-global state or credentials?** Require a separate
   security decision and explicit administration authorization.
6. **Is there no RPC or bounded daemon equivalent?** Mark it unavailable with a
   precise reason; do not forward TUI text and hope.

### Implementation notes for required commands

These details are required scope, not follow-on ideas:

* **Diff:** use `gitDiffToRemote` for an explicit session-working-tree patch,
  not merely the last turn's diff. Cache `turn/diff/updated` as the narrower
  method-not-found fallback. Enforce the byte cap before the value reaches
  history or the phone.
* **Review:** use `review/start` with inline delivery and the four typed target
  variants. Existing entered/exited item handling becomes structured lifecycle
  state, and the final review field is a deduplicated fallback, not an
  additional answer.
* **Fast:** retain `serviceTiers` and `defaultServiceTier` from `model/list`,
  resolve the tier named Fast, and send its opaque id through settings and
  `turn/start`. Never hard-code `priority`, because it is probe evidence rather
  than a protocol guarantee.
* **Personality:** retain `supportsPersonality`, advertise only for a supporting
  selected model, and store `friendly`, `pragmatic`, or `none` independently of
  prompt text. `none` is the enum, not a JSON-null clear. Apply it through
  settings when available and every next turn.
* **Goals:** implement a typed `GoalSession`, consume updated/cleared events,
  hydrate with get on resume, enforce the 4,000-scalar bound, and implement the
  Plan/busy safety matrix from D16. Do not infer a goal from ordinary prompt
  text.
* **Fork:** correct the provider field to `lastTurnId`, preserve all daemon-owned
  settings, and convert the returned native fork into an owned managed session.
  A never-materialized thread and an active non-deferrable goal fail before any
  misleading success notice.

### Ranked follow-on solution details

These findings are deliberately outside this MADR's acceptance scope:

* **Status:** compose a redacted response from managed session metadata,
  current model/thinking/service tier/personality, both mode axes, latest usage,
  sandbox roots summarized without arbitrary paths, and Codex version. This is
  safer and more stable than scraping the TUI `/status` panel.
* **Skills/apps:** use `skills/list` and `app/list` for searchable pickers. A
  selection should produce the official prompt reference/input form; listing an
  item is not itself activation. Never expose secrets or raw plugin config in
  picker metadata.
* **Background terminals:** if later enabled, scope every operation to the
  current thread, list first, terminate only exact returned process ids, bound
  output, and surface experimental/version status. Do not call
  `thread/shellCommand`: official docs state it runs unsandboxed with full
  access and it is not an acceptable implementation of a remote `!` shortcut.

### Superseded statements

When this MADR is accepted and its required command scope lands:

* `TestCodexAdvertisesNoPlanMode` remains required: Codex still must not
  advertise `plan` as an autonomy `SessionMode`. Only the “no plan agent”
  comment is superseded;
* the Codex comments asserting “Codex has no mode list of its own” are
  superseded only with respect to collaboration modes; their description of
  the existing permission-mode table remains valid;
* MADR 0028's “set path unverified” limitation is superseded by the 0.147 live
  `thread/settings/update` evidence above;
* MADR 0022 remains authoritative for providers whose plan behavior is modeled
  through `ModeSession`; this MADR adds the separate Codex path rather than
  rewriting that history;
* MADR 0023 remains authoritative that provider command tables and live
  capabilities, not agent/TUI advertisement alone, determine remote command
  truth.

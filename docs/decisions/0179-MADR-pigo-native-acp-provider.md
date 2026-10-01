---
status: proposed
date: 2026-10-01
decision-makers: Project Owner
consulted: pi-go records 0002-MADR-cli-acp-headless-mcp-v1.md, 0004-MADR-go-module-architecture.md, 0005-MADR-v1-feature-scope.md; acp-go-sdk fork at v0.13.6-mcr.1
informed: pi-go, go-llmprovider-sdk, go-core-lib
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# MADR 0179: Drive pigo as a first-class ACP provider — Spec-declared session operations, strict command fallback, and complete ACP event mapping

## Context and Problem Statement

`pigo` is a Go rewrite of the Pi coding agent, built in the `pi-go` repository. Its records make it the **native agentic CLI of this platform**:

* `pigo acp` is an ACP agent on stdio.
* Every agent-owned slash command it advertises executes.
* It never implements grok's `_x.ai/*` vendor methods.

The contract it asks of this repository is in pi-go `0002-MADR-cli-acp-headless-mcp-v1.md` (fourth amendment, "Companion Spec") and in the target command table of pi-go `0005-MADR-v1-feature-scope.md`. Nothing in this repository mentions pigo yet: there are no matches for `pigo`, `pi-go` or `IDPi` in code, docs or `git log --all`.

The question for this repository is: what must change here so that mcremote can drive pigo as a first-class provider, beside grok, opencode, kilo, codex and fake? The requirements are:

* every command the phone shows as available actually executes;
* no grok vendor method is ever sent to pigo;
* the ACP events pigo emits reach the phone.

This record is the result of a read-only assessment, made on 2026-10-01, of four codebases:

* this repository at `64282e29` / `772c041e`;
* the acp-go-sdk fork at tag `v0.13.6-mcr.1`;
* pi-go's records;
* TypeScript Pi at `312184edb`.

It records the findings and decides the shape of the companion work. It does not implement anything.

### Findings

The assessment found the issues below. Each one is evidenced at file:line and checked against code, not against earlier records.

**F1 — A command that falls back to its default can send grok vendor RPCs to any ACP agent (P0, verified).**

* `command.Resolve` (`internal/command/command.go:235-263`) resolves a canonical name in three steps:
  1. If the provider's table declares the name with a non-`KindNone` mapping and that mapping is available, the table wins.
  2. If the declared mapping is not available, resolution falls through to `spec.Default`.
  3. Only if the default is also unavailable does the command become `KindNone`.
* A `KindNative` mapping is available only while the agent advertises that exact native name (`command.go:298-299`).
* `KindOp` availability is a Go type assertion against the live session (`internal/session/commands.go:62-88`).
* The shared ACP session `*acpagent.session` statically implements all of these interfaces for **every** Spec:

  | Interface | Calls | Where |
  |---|---|---|
  | `CompactSession` | `_x.ai/compact_conversation` | `acpagent/sessioncaps.go:223-231` |
  | `RenameSession` | `x.ai/session/rename` | `sessioncaps.go:234-247` |
  | `ForkSession` | `_x.ai/session/fork` | `fork.go:25-44` |
  | `UndoSession` | `_x.ai/rewind/points`, then `/execute` | `rewind.go:89-108` |
  | `RuntimeSession` | `_x.ai/session/usage`, `_x.ai/billing` | `runtime.go:87-94,229` |
  | `ModelSession` | raw `session/set_model` | `session.go:946-983` |
  | `ThinkingSession` | raw `session/set_model` `_meta.reasoningEffort`, then a raw `session/resume` read-back | `thinking.go:41-88` |
* The defaults in `internal/command/specs.go` are `compact` → `KindOp OpCompact`, `thinking` → `KindOp OpSetThinkingLevel`, `undo` → `KindOp OpUndo`, `diff` → `KindOp OpDiff`, and `model` → `KindDaemon` (relaunch, context lost).
* pi-go's plan declares `compact`, `thinking`, `model`, `fork` and `undo` as `KindNative` until Spec hooks exist. Under today's resolver, those rows resolve to the grok vendor methods whenever pigo has not advertised the name:
  * in the window between `session.create` and pigo's first `available_commands_update` (`advertiseCommands` runs at create time, `internal/session/manager.go:1216-1219`);
  * after a load;
  * in any release that drops a command.
* The phone then shows the command as available, and running it returns "not implemented" (-32601 → `ErrNotImplemented`) or a raw MethodNotFound notice.
* **Adding Spec fields alone does not fix this.** A nil field does not remove an interface from a type.

**F2 — Phone menus bypass the resolved command (P0, verified).**

* `apps/mobile/lib/features/chat/chat_screen.dart:2538-2549` gates the Fork and Diff menu items on `remote_commands` showing `fork`/`diff` as available.
* The items then send the WebSocket messages `session.fork` (`:1667`) and `session.diff` (`:1571`).
* `session.fork` reaches `Manager.Fork`, which calls `_x.ai/session/fork`.
* `session.diff` needs `DiffSession`, which `acpagent` does not implement.
* Phone rename (`session.rename`) calls `x.ai/session/rename` (`manager.go:2412-2416`).
* For pigo, a `KindNative` `fork` row would therefore light up a menu item that sends a grok method.

**F3 — Vendor operations are hard-coded on the session, not on the Spec (verified).**

* `acpagent.Spec` (`acpagent.go:44-140`) parameterizes these fields:
  * launch: `DefaultArgs`, which is `func(cfg Config) []string` and not a slice, and `ModelArgs`;
  * `ConfigureSession`, `SessionMeta`, `KnownGoodVersion`;
  * auth hooks;
  * models and modes: `StaticModels`, `ListModels`, `StaticModes`, `DefaultModeID`, `SynthesizeAutoMode`;
  * `Commands`, `CommandCaveat`, `ExtensionNotifications`.
* It has no field for Compact, Rename, Fork, UndoLast, Runtime, SetModel or SetThinkingLevel.
* Nor does it have a field for the incoming client-side extension handlers `_x.ai/exit_plan_mode` and `_x.ai/ask_user_question` (`acpagent/extensions.go:28-29,81-95`).
* `sessioncaps.go:24` still says "This package is grok only", which contradicts the generic-Spec design that MADR 0160 (`0160-MADR-remove-goose-cli-support.md`) left in place.

**F4 — ACP events pigo will emit are dropped or degraded (verified, `acpagent/session.go:1608-1788`).**

* `session_info_update` falls to the `default` branch (`:1784`). `event.TypeSessionTitle` is already handled at `manager.go:1342`, so a title from the agent never reaches the phone.
* `usage_update` keeps only `used` and `size` (`:1757-1764`).
  * `cost` is dropped, although the SDK carries it (`types_gen.go:9206-9219`) and `event.Usage.CostUSD` exists.
  * `usage_update` is marked **UNSTABLE** in schema 0.13.5 (`types_gen.go:9201-9205`). It is not baseline ACP.
* For `tool_call` / `tool_call_update`:
  * a diff becomes the string `"diff <path>"` (`:2556-2586`);
  * `locations` are ignored.
* For `config_option_update` and `configOptions` (`:1940-1991`):
  * only ungrouped select options and booleans are forwarded;
  * the option **category** is not forwarded, so the phone cannot tell which option is the model.
* Neither `config_option_update` nor a natively forwarded `/model` or `/thinking` updates `meta.Model`, `ThinkingLevel` or `currentModelID` (`manager.go:2374-2387`). `/context`, relaunch and turn records then show a stale model.
* Plan-exit detail carried in a standard `session/request_permission` is cut to 400 runes, with whitespace collapsed (`session.go:2110-2120`). The grok extension card allows 8000 characters (`extensions.go:69`).

**F5 — Model and start-up wiring is grok-shaped (verified).**

* `ConfigureSession` runs after `session/new` only, not after `session/load` (`acpagent.go:57-60,878-892,905-910`). A resumed or forked pigo session never gets its start-up model or thinking level applied.
* The phone's `models.list` catalog comes from one of three places (`acpagent.go:310-401,1179-1199`):
  * grok's `initialize` `_meta.modelState`;
  * `Spec.ListModels`;
  * `StaticModels`.

  `CurrentModel` comes from grok `_meta` (`session_meta.go:42-75`). Standard `configOptions` are never read for either, so pigo's model picker would be empty.
* There is no slash path to `session/set_config_option`. It is reachable only through the WebSocket `session.set_config_option` (`manager.go:2374-2386`).

**F6 — Modes and permissions (verified).**

* Agent-advertised modes are never marked `Dangerous` (`session.go:1815-1826,1879-1892`). Only the daemon's synthetic `auto` mode gets the phone's confirmation gate.
* pigo advertises a `bypass` mode when its `permissions.allowBypass` setting is on. That mode would appear on the phone without the gate.

**F7 — The prompt path filters what pigo can receive (verified).**

* A leading `!` is intercepted before slash parsing. It requires `ExecutionSession`, which only codex has, so pigo never sees `!cmd` (`manager.go:915-964,2238-2240`).
* A slash name must match `[A-Za-z0-9][A-Za-z0-9_-]*` (`commands.go:345-358`). pigo's `/skill:<name>` and `/mcp:<server>:<prompt>` therefore travel as plain prompt text. They execute, but never appear in `remote_commands`.
* `buildPromptBlocks` sends only text, image and audio blocks (`session.go:509-540`). mcremote never sends `resource_link` or embedded resources.
* There is no steer path. Prompts sent while a turn runs join a daemon-side FIFO capped at 4 (`session.go:297-322`).

**F8 — Hops the pigo records describe imprecisely (verified).**

These facts correct pi-go's records. pi-go amends its own records with them.

* mcremote's `fs/read_text_file` and `fs/write_text_file` handlers use plain `os.ReadFile` and `os.WriteFile` (mode 0644, absolute paths, optional `fs_roots` confinement, an audit tool event per call; `session.go:2309-2382`). They do not expose editor buffers.
* `terminal/*` runs on the host. The phone sees only a `"terminal <id>"` summary (`session.go:2453-2477`).
* The codex default transport is `app-server --listen stdio://` (`internal/config/config.go:838`; `internal/provider/codex/launch.go:10-60`). WebSocket, `unix_ws` and the managed proxy are options.
* MCP forwarding is http/sse only. Config validation rejects any other transport at load (`config.go:1041-1048`), before `buildMcpServers` (`acpagent.go:621-651`) ever runs. `MCPServerConfig` has no command or args fields.
* `initialize` sends no `clientInfo` and no `_meta` (`acpagent.go:521-585`), so an agent cannot tell that its client is mcremote.
* The child process inherits the daemon's environment; `cmd.Env` is never set (`acpagent.go:431-470`). There is no per-provider environment setting.
* `Ready()` is only a PATH lookup. There is no version probe.
* `OverflowDropNewest` is set on the one client-side construction site (`acpagent.go:495,1024-1026`), per MADR 0167 (`0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md`). The fork's policy governs only a connection's **inbound** notification queue.

**F9 — Internal inconsistencies found along the way (verified).**

* The grok command table keeps `compact`, `usage`, `status` and `undo` as `KindNone` (`internal/provider/grok/commandtable.go:28-48`), and `KindNone` is final. MADR 0138 (`0138-MADR-overhaul-provider-surfaces-and-turn-path.md`) nevertheless implemented those ops, and its PLAN's acceptance item 9 says grok answers `/compact`. `internal/session/live_command_test.go:46-68` still asserts that `/compact` is refused. Either the table or the record is wrong.
* `0023-MADR-canonical-slash-commands.md` "Known limitations" says no `_x.ai` call exposes compact. 0138 says one does.
* `docs/protocol-v1.md` is out of date:
  * `:684` says `/thinking` on grok is spawn-only, but it is live (`commands.go:1373-1375`);
  * `:705` says grok `/model` restarts the agent, but it uses `OpSetModel`;
  * `:147` says the read deadline is 60 s, but the code default is 120 s (`config.go:131-136,866`);
  * `:155-158` predates close code 4001.
* `docs/protocol-v2.md:13` links to the old path of `0068-MADR-protocol-v2-reconnect-resilient-transport.md`.
* Comments that disagree with the code: `specs.go:9-10` ("only `/goal` defaults to forwarding"), and `command.go:72-75` (grok thinking "spawn-only").
* `internal/session/tokencost.go:54` always suggests `/compact`, even where it is unavailable.
* The phone composer still offers agent commands the daemon resolved as unavailable (`chat_screen.dart:633-640`). This contradicts 0023 D2 ("table beats advertisement") on the client.
* Spec `Args` for `archive`, `delete`, `review` and `stop` use codex grammar (`specs.go:134-168`). That grammar is shown in `/help` and `commands.list` for every provider.
* The raw `session/set_model` and `session/resume` calls go through an `unsafe.Pointer` cast (`session.go:2622-2668`), and the comment about `session/resume` is stale.

**F10 — Policy findings (verified; repair is out of this record's scope).**

* `mcremote serve --relay-secret` (`internal/cli/serve.go:131`) and `mcrelay serve --allow host_id:secret` (`internal/relay/cli.go:327`) take secrets as CLI arguments. Org rule ADR 007 forbids that. The environment alternative `MCREMOTE_RELAY_SECRET` exists.
* `scripts/quick-probe.py:11` hard-codes a real-machine absolute home path. That breaks the identifier rule in `AGENTS.md` and in MADR 0171 (`0171-MADR-redact-published-identifiers-so-pushes-pass-the-disclosure-guard.md`). The path is not reproduced here.

## Decision Drivers

* The rule in `0023-MADR-canonical-slash-commands.md`: a command the phone shows as available must execute. Advertisement is not capability, and neither is a Go type assertion.
* No vendor coupling. pigo implements no `_x.ai/*` method, and this daemon must never send one to it.
* Grok, opencode, kilo, codex and fake keep working unchanged. Their command tables and live tests stay green.
* pigo speaks only baseline ACP schema 0.13.5 plus `_pigo/*` extensions. The SDK cannot route `session/set_model` to an agent: only `_`-prefixed methods reach `ExtensionMethodHandler` (`acp-go-sdk agent_gen.go:467-468`, `extensions.go:36-50`).
* One ACP adapter (`acpagent`) for every ACP agent, rather than a second copy of its 45 files.
* Every capability is proven by a live probe (MADR 0137: `0137-MADR-prompt-to-first-token-latency-regression.md`, `KnownGoodVersion` warns and never refuses).

## Considered Options

* **A.** Spec-declared session operations with capability reporting, strict fallback per provider, and complete ACP event mapping
* **B.** Register pigo with a command table only, and reuse `acpagent` unchanged
* **C.** A separate `pigoagent` package, copied from `acpagent`
* **D.** pigo implements the `_x.ai/*` methods `acpagent` already calls
* **E.** A strict-fallback resolver change alone, with no Spec hooks

## Decision Outcome

Chosen option: **A**. It is the only option under which every canonical row pigo maps is honest from the first frame of a session, the phone's menus agree with the resolver, and grok's behaviour is preserved by moving its vendor calls into its own Spec rather than deleting them.

The decision has ten parts, D1 to D10.

**D1 — Session operations become Spec fields.**

* Add a value of hooks to `acpagent.Spec`. Each field is a function over the client connection and session id. A nil field means "not supported".

  ```go
  type SessionOps struct {
      Compact      func(ctx context.Context, c Conn, sid acp.SessionId, instructions string) error
      Rename       func(ctx context.Context, c Conn, sid acp.SessionId, title string) error
      Fork         func(ctx context.Context, c Conn, sid acp.SessionId, opts provider.ForkOptions) (provider.ForkResult, error)
      UndoLast     func(ctx context.Context, c Conn, sid acp.SessionId) (string, error)
      RuntimeUsage func(ctx context.Context, c Conn, sid acp.SessionId) (string, error)
      RuntimeStatus func(ctx context.Context, c Conn, sid acp.SessionId) (string, error)
      SetModel     func(ctx context.Context, c Conn, sid acp.SessionId, model string) error
      SetThinking  func(ctx context.Context, c Conn, sid acp.SessionId, level string) error
  }
  ```

  The signatures are illustrative. The PLAN fixes them.
* Today's grok bodies move into `internal/provider/grok` as that Spec's `SessionOps`, unchanged on the wire.
* The incoming `_x.ai/exit_plan_mode` and `_x.ai/ask_user_question` handlers move behind the Spec's `ExtensionNotifications` / client-extension map. They are not registered for other Specs.

**D2 — Capability reporting reads the Spec, not the Go type.**

* `*acpagent.session` implements a new `provider.OpReporter` interface: `Ops() map[command.Op]bool`.
* It reports an op only when the corresponding `SessionOps` field is non-nil.
* `commandContext` (`internal/session/commands.go`) consults `OpReporter` when the session implements it. Only otherwise does it use the existing type assertions, so httpagent, codex and fake are unchanged.
* The methods that remain on `*session` return `ErrNotImplemented` when their field is nil. They never fall through to a vendor method.

**D3 — Strict fallback, opt-in per provider.**

* `command.Table` gains a strict flag. It is carried on the provider's command table, so it needs no new interface.
* When it is set, a declared non-`KindNone` mapping that is not available resolves to `KindNone`. The note reads: the agent has not advertised `/<native>` in this session.
* The resolver no longer falls through to `spec.Default` for that provider.
* pigo's table sets it. Grok, opencode, kilo, codex and fake do not, so their resolution is unchanged and their conformance tests stay green.

**D4 — The phone acts on the resolved mechanism.**

* `RemoteCommand` gains the resolved `kind` (and, for `KindNative`, the native name).
* The Fork, Diff and Rename menu items, and the `session.fork`, `session.diff` and `session.rename` handlers, act on that resolution:
  * when it is `KindNative`, they send the forwarded slash text through `session.prompt`;
  * when it is `KindOp`, they use the op;
  * otherwise they are hidden.
* The composer offers only commands the daemon resolved as available. That brings 0023 D2 to the client.

**D5 — Complete ACP event mapping in `acpagent`.**

These changes apply to every ACP Spec, not to pigo only:

* `session_info_update.title` → `event.TypeSessionTitle`.
* `usage_update.cost` → `event.Usage.CostUSD`. The event type is unstable in schema 0.13.5, so the mapping tolerates its absence.
* Tool-call `locations` and diff old/new text are forwarded. The phone renders the diff, within the frame budget of MADR 0154.
* `configOptions` forwards the option category and grouped values.
* `config_option_update` with category `model` or `thought_level` updates `meta.Model` and `ThinkingLevel`, and so does a forwarded `/model` or `/thinking` that is confirmed by such an update.
* The plan text inside a standard `session/request_permission` is kept up to the same 8000-character bound as the grok card.

**D6 — Start-up and model catalog without vendor `_meta`.**

* `ConfigureSession` also runs after `session/load`, applying model and thinking the same way it does after `session/new`.
* A shared helper picks the option whose category is `model` (or `thought_level`) from `configOptions`, and calls `session/set_config_option` with that option's `configId`.
* The same helper backs a `ListModels` implementation for any Spec whose agent publishes a `model` config option.

**D7 — Dangerous modes.**

* `Spec.DangerousModeIDs []string` marks agent-advertised modes that need the phone's confirmation gate.
* pigo's Spec lists `bypass`.

**D8 — pigo registration.**

* **Provider id and configuration:**
  * `provider.IDPigo` with wire id `"pigo"` (`internal/provider/provider.go:62-73`). pi-go's records call it `IDPi`, and they read this name from here.
  * A `PigoProviderConfig` embedding `ACPProviderConfig` (`config.go:450,471-506`), with defaults, `validateACPProvider` (`:1104`), and `MCREMOTE_PROVIDERS_PIGO_*` documented in `docs/config.md`.
  * `KnownProviderIDs` in `config/prewarm_write.go:27`.
* **Registration:** in `internal/daemon/daemon.go`, through `acpAgentConfig` (`:734-757`).
* **The pigo Spec:**
  * launch: `DefaultBin: "pigo"`, `DefaultArgs: func(Config) []string { return []string{"acp"} }`;
  * `KnownGoodVersion` taken from pigo's release notes and read from `agentInfo.version`;
  * hooks: `ConfigureSession` and `ListModels` per D6;
  * vendor surface empty: `SessionMeta` nil, `ExtensionNotifications` empty, `SynthesizeAutoMode` false;
  * modes: `DangerousModeIDs: ["bypass"]`;
  * the command table: strict, every canonical name declared (D9).
* **`SessionOps` for pigo:**
  * `SetModel` and `SetThinking` → `session/set_config_option` by category, per D6;
  * `Compact` → `_pigo/compact {sessionId, instructions?}`;
  * `RuntimeUsage` → `_pigo/usage`;
  * `Rename` → `_pigo/set_session_name`;
  * `Fork` → `_pigo/fork`, returning the new agent session id for `session/load`.

  The `_pigo/*` method names are frozen by pi-go `0002-PLAN-cli-acp-headless-mcp-v1.md` Phase 7. This repository registers each field only for the pigo release that implements it, and probes it live.
* **Other places that name providers:**
  * the conformance test lists in `internal/command/conformance_test.go:52-80`;
  * a pigo row in `internal/cli/doctor.go:80-110`;
  * the mobile vendor icon manifest, `provider_detail_screen`, and the diagnostics gating at `chat_screen.dart:2548`;
  * the provider lists in `docs/protocol-v1.md`.
* **Testing:** a `live-pigo` Make target, with wire fixtures captured by `internal/wirecap`.

**D9 — pigo's command table. It is the 0005 target, expressed under D1–D3.**

| Canonical | Mapping | Condition |
|---|---|---|
| help, clear/reset, new, sessions | `KindDaemon` | — |
| plan, mode | `KindMode` (`plan`; `default` for `/plan off`) | — |
| model | `KindOp OpSetModel` | `SessionOps.SetModel` set (D6) |
| thinking | `KindOp OpSetThinkingLevel` | `SessionOps.SetThinking` set |
| context | `KindOp OpContext` | after the first `usage_update` |
| compact | `KindOp OpCompact` | `_pigo/compact` probed. Until then `KindNative compact`, safe under D3 |
| usage, status | `KindNative usage` / `status` | until `RuntimeUsage` / `RuntimeStatus` are registered |
| fork | `KindNative fork` | until `SessionOps.Fork` is registered |
| permissions | `KindNative permissions` | — |
| undo, redo, diff | `KindNative` | pigo 1.x checkpoints. Until pigo advertises them, they resolve to `KindNone` under D3 |
| ps, stop | `KindNative` | pigo 1.x background jobs |
| review, archive, delete | `KindNative` | pigo 1.x |
| deep-research | `KindNative deep-research` | pigo advertises `/deep-research` as an alias of its skill, because `/skill:…` fails `isCommandName` |
| reviewer, approve, goal, workflow, loop, fast, personality | `KindNone`, with a readable note | — |

**D10 — Live probes before each row is registered.**

The `live-pigo` suite must show these:

* `/compact`, `/usage` and `/context` each produce non-empty `session/update` frames.
* `/settings` is absent from `available_commands`.
* `session/set_model` gets MethodNotFound from pigo.
* No `_x.ai/*` frame is sent to pigo. The test records every outbound method.
* `/compact` typed before the first `available_commands_update` is refused with D3's note, and is not sent as a vendor call. This test must be seen failing on the current resolver first.

### Consequences

* Good, because F1 and F2 close for every ACP Spec, not only for pigo: a future ACP agent without vendor ops can no longer receive `_x.ai/*`.
* Good, because grok's vendor calls stay exactly where they are on the wire, now owned by the grok Spec. Its tables and live tests do not change.
* Good, because D5 improves the phone for grok too: tool diffs, usage cost, titles, and model metadata that stays correct.
* Good, because pigo's `KindOp` rows become reachable through standard ACP (`session/set_config_option`) plus `_pigo/*`. That is the mechanism opencode and kilo already have over HTTP.
* Neutral, because strict fallback is opt-in. Providers that rely on default fallback (grok `model` → relaunch) keep it.
* Bad, because the change touches the `acpagent` session type, the command resolver, the WebSocket handlers and the mobile client in one record. The PLAN must phase it, with the resolver and Spec work landing before any pigo registration.
* Bad, because the `_pigo/*` method names are owned by another repository. A rename there needs a coordinated change here. Live probes catch the drift; they do not prevent it.

### Confirmation

* A resolver unit test: with a strict pigo table that maps `compact` to `KindNative compact`, and agent commands empty, `Resolve("compact")` is `KindNone`. With strict off, the same input resolves to `KindOp` (today's behaviour). The test is shown failing on the current resolver before the change.
* A unit test with a fake `acpagent` Spec whose `SessionOps` is empty. `commandContext` reports none of OpCompact, OpFork, OpUndo, OpSetModel, OpSetThinkingLevel, OpUsage or OpStatus. A recorded connection shows no `_x.ai/*` method across `/compact`, `/fork`, `/undo`, `/thinking high`, a phone fork, and a phone rename.
* Grok's existing unit, conformance and `live-grok` suites pass unchanged.
* An `acpagent` mapping test feeds `session_info_update`, `usage_update` (with cost), a tool call with `locations` and a diff, and a categorized `config_option_update`. It asserts the resulting daemon events.
* The D10 probes run green against a tagged pigo release before `IDPigo` ships.

## Pros and Cons of the Options

### A — Spec-declared operations, capability reporting, strict fallback, complete event mapping

* Good, because it fixes the defect at its cause: capability follows declaration, not type.
* Good, because every existing provider keeps its behaviour.
* Bad, because it is the largest change of the five.

### B — A command table only, reusing `acpagent` unchanged

* Good, because it is about one file plus registration.
* Bad, because F1 makes it unsafe. Every `KindNative` row falls back to a grok vendor RPC until pigo advertises the name. That is the "advertised but silent" failure 0023 exists to prevent, now as "available but MethodNotFound".
* Bad, because the F2 phone menus would still call `_x.ai/session/fork` and `x.ai/session/rename`.

### C — A separate `pigoagent` package copied from `acpagent`

* Good, because grok's code is untouched.
* Bad, because it duplicates 45 files of transport, supervision, overflow and teardown handling. MADR 0166 (`0166-MADR-a-stalled-session-can-still-take-the-acp-transport-down.md`) and MADR 0167 hardened that code once.
* Bad, because the D5 event-mapping gaps would be fixed for pigo only.

### D — pigo implements `_x.ai/*`

* Good, because no change is needed here.
* Bad, because it couples the native CLI to another vendor's private extensions. pi-go 0002 rejects it, with the grok-shaped adapter option.
* Bad, because the `session/set_model` path still fails: the SDK answers MethodNotFound for it on the agent side, whatever pigo implements.

### E — Strict fallback alone

* Good, because it is a small, contained resolver change that closes F1 for slash input.
* Bad, because F2 (phone menus) and F3 (no way to declare pigo's real ops) stay open. pigo could never reach `KindOp` for compact or fork.

## More Information

### Deferred (not decided here; each needs its own record when picked up)

* **Instructions on compact.** `Compact(ctx, instructions)` with per-mapping `Args` overrides, so `/compact focus on X` keeps its argument when a pigo row is `KindOp`. Today `CompactSession.Compact(ctx)` takes none (`provider.go:627-630`).
* **The `!` gate.** Apply it only when the session implements `ExecutionSession`, so `!cmd` reaches agents that run shell input themselves (pigo, Pi).
* **Steer.** A steer message in protocol-v1/v2 and a `SteerSession` interface (pigo `_pigo/steer`), in place of the daemon FIFO for mid-turn input.
* **Stdio MCP.** Daemon stdio MCP server config, which ACP's untagged default form allows.
* **Additional directories.** `AdditionalDirectories` on `session/new`, from workspace roots.
* **Questions.** A standard question path to the phone. ACP elicitation is unstable in 0.13.5, so `session/request_permission` with custom options is the interim. pigo can then ask the user without `_x.ai/ask_user_question`.
* **Status notifications.** Subscribing to pigo's `_pigo/status` notifications (retry and compaction progress) through `ExtensionNotifications` once pigo freezes them.
* **Per-provider environment.** A per-provider `env` setting, so `PIGO_*` and provider API keys need not live in the service environment.
* **Repairs:** the F9 inconsistencies, and the F10 policy findings.

### Facts the pigo contract can rely on (verified)

* **Process model:** one subprocess per session, plus an optional warm spare (`acpagent.go:422-616,768-931`). Start-up latency is user-visible.
* **Timeouts:** initialize and new 30 s, load 120 s, close bounded at 2 s and followed by a process-group kill.
* **`initialize`:** protocol `1`, client capabilities `fs.readTextFile`, `fs.writeTextFile` and `terminal` all true.
* **Methods mcremote calls when advertised:**
  * `session/load` (gated on `loadSession`);
  * `session/list` (`sessionCapabilities.list`);
  * unstable `session/delete`.

  It never calls the typed `session/resume`.
* **Child-session frames** are dropped (`session.go:1626-1633`, MADR 0051 D6: `0051-MADR-auto-approve-chat-noise.md`).
* **Discovery** is a PATH lookup. The service PATH already includes `~/go/bin`, `~/.local/bin`, `/opt/homebrew/bin` and `/usr/local/bin` (`internal/cli/service/setup.go:902-923`).
* **Self-update** covers mcremote and mcrelay only (`internal/updateclient/client.go:19-30`). This daemon never updates agent CLIs, so pigo's own `pigo update` must stay inert in `acp` mode.

### Cross-references

* pi-go `0002-MADR-cli-acp-headless-mcp-v1.md` (fourth and fifth amendments) and `0005-MADR-v1-feature-scope.md` (companion target table) carry the agent side of this contract. Their companion checklists defer to this record.
* `0023-MADR-canonical-slash-commands.md`, `0137-MADR-prompt-to-first-token-latency-regression.md`, `0138-MADR-overhaul-provider-surfaces-and-turn-path.md`, `0160-MADR-remove-goose-cli-support.md`, `0166-MADR-a-stalled-session-can-still-take-the-acp-transport-down.md`, `0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md`.
* No PLAN is attached yet. When this record is accepted, `0179-PLAN-pigo-native-acp-provider.md` phases the work:
  1. D2 + D3 with their failing-first tests;
  2. D1, moving grok's ops into its Spec;
  3. D5 and D6;
  4. D4 and the mobile changes;
  5. D7–D10 and the pigo registration, after a pigo release exists.

  The number was taken on 2026-10-01: `scripts/check_records.py --next` printed `0178`, which `0175-PLAN-conform-docs-tree-to-adopted-record-layout.md` P4 reserves for the mobile-UX report, so this record took the next free number.

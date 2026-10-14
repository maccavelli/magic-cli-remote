# gobble native ACP integration assessment

Observed 2026-10-11. The owner's direction: `gobble-cli` uses ACP over stdio as its native transport
and command protocol, and is built on `acp-go-sdk2`. gobble is to be the one truly native agent CLI
for magic-cli-remote, and magic-cli-remote is to be gobble's native remote-control platform. This
report evaluates that integration against both codebases. It decides nothing, changes no source,
and ran no probe. Statements marked **[derived]** are reasoned from source and have not been run.

Associated records:

- [0179-MADR-gobble-native-acp-provider.md](../decisions/0179-MADR-gobble-native-acp-provider.md)
  (proposed; written for the agent under its earlier name)
- [0179-PLAN-gobble-native-acp-provider.md](../decisions/0179-PLAN-gobble-native-acp-provider.md)
  (phase R1 produced this report)
- [0167-REPORT-acp-go-sdk2-integration-assessment.md](0167-REPORT-acp-go-sdk2-integration-assessment.md)
  (full-adoption section)
- [0023-MADR-canonical-slash-commands.md](../decisions/0023-MADR-canonical-slash-commands.md),
  [0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md](../decisions/0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md)
- gobble-cli records, cited by name because a relative link cannot reach another repository:
  - `0002-MADR-cli-acp-headless-mcp-v1.md` (the ACP agent and its place in this stack);
  - `0008-MADR-native-cli-mode.md` D19 and D20 (delivery rules and companion changes);
  - `0014-MADR-move-to-acp-go-sdk2.md`;
  - `0009-REPORT-magic-cli-remote-findings.md`, gobble's running list of findings about this
    repository, M1–M10.

## Executive assessment

**The architecture is sound, and gobble is further along than this repository.**

- **The boundary is right.** ACP stdio, with this daemon as the client, keeps `acpagent` generic.
  gobble's 0002-MADR rejects speaking this repository's WebSocket protocol, which would have forked
  pairing, auth, resume and command resolution into a second implementation. That rejection is
  correct.
- **gobble is ready.** `gobble acp` ships on `acp-go-sdk2 v0.14.0` and was designed around this
  daemon's delivery limits.
- **This repository is not.** Nothing here registers gobble. The contract record, MADR 0179, is
  still `proposed`, still names the agent `pigo`, and was written against schema 0.13.5. Several of
  its premises no longer hold:
  - `usage_update` and elicitation are stable in schema 1.25;
  - gobble froze `_gobble/*` method names, not `_pigo/*`;
  - one compact parameter is named differently (gobble finding M9).
- **Registered today, gobble would be partly broken [derived].** Sessions, prompts, cancel, load,
  list and close would work. `/model` and `/thinking` would fail with method-not-found. Commands
  that fall back to grok's private methods would show as available, then fail. Titles would be
  dropped, and diffs and plan text degraded.
- **The repair is mostly work already identified.** It is the 0167 full-adoption work (standard
  config options, complete update mapping) plus MADR 0179's safety work: strict fallback,
  capability reporting from the Spec, and phone menus that act on the resolved command. Those two
  records overlap heavily and should be planned as one sequence.

**Recommended direction:**

1. Replace or amend MADR 0179 for gobble on schema 1.25.
2. Land the SDK migration and 0179's strict-fallback and capability-reporting safety (D2, D3)
   before registering gobble.
3. Drive gobble with standard ACP first, and use `_gobble/*` only where the schema has no form.
4. Settle five design questions:
   - per-provider client capabilities;
   - a shared contract module for gobble's extensions;
   - a single SDK pin for both repositories;
   - credentials;
   - who updates gobble on a magic-cli-remote host.

## Scope and evidence

### Baselines

| Repository | Commit | ACP SDK | Notes |
| :--- | :--- | :--- | :--- |
| `magic-cli-remote` | `9778cbc17b39` | `coder/acp-go-sdk v0.13.5`, replaced by the fork `v0.13.6-mcr.1` | owner work not yet committed in records and `go.mod`; no Go source differs |
| `gobble-cli` | `ad6ea57c5d0d`, clean tree | `acp-go-sdk2 v0.14.0` (`go.mod`) | also pins `go-selfupdate-lib v1.9.0`, `go-llmprovider-sdk v1.2.1`, MCP Go SDK `v1.8.0` |
| `acp-go-sdk2` | `main` `f2a6a3a`; tag `v0.14.0` → `3588f8a3…` | — | schema 1.25.0; 12 commits past `v0.14.0`, untagged |

### What was read

- **gobble:** `README.md`; `acpserver/agent.go`, `config.go`, `modes.go`, `extensions.go`,
  `prompt.go`, `clientfs.go`, `clientterm.go`; `gobble/doc.go`; the records listed above.
- **This repository:** MADR 0179 in full; `internal/provider/acpagent/{acpagent.go, session.go,
  session_meta.go}`; `internal/session/commands.go`.
- **`acp-go-sdk2`:** `agent_gen.go`, `client_gen.go`, `types_gen.go` (`AuthMethod`, capabilities).

## gobble's ACP surface (observed)

| Area | What gobble does | Evidence |
| :--- | :--- | :--- |
| Identity | `agentInfo {name: "gobble", title, version}`. Dev builds report `0.0.0-dev+<revision>[.dirty]`, so they never match a release pin | `acpserver/agent.go:224-235`; gobble 0009-REPORT M7 |
| Agent capabilities | `loadSession`; MCP `http` (stdio is baseline); prompt `embeddedContext` (no images yet); session `additionalDirectories`, `close`, `list`, `resume` | `agent.go:203-221` |
| Extension discovery | `agentCapabilities._meta.gobble.extensions` lists every `_gobble/*` method | `agent.go:217-218`; `extensions.go:47-48` |
| Auth | `authMethods: []`; `Authenticate` and `Logout` are stubs | `agent.go:233,934-939` |
| Delete | `DeleteSession` is implemented to satisfy the SDK interface; `delete` is not advertised | `agent.go:889`; `delete_test.go` |
| Config options | `model` and `thought_level` as ungrouped `select` options **with categories** | `config.go:42-53` |
| Modes | `default`, `plan`; accept-edits and bypass are planned (F6) | `modes.go:15-33` |
| Plan exit | standard `session/request_permission` with `start_implementing` / `keep_planning` | `modes.go:25-28,153` |
| Session updates emitted | agent, user (replay) and thought chunks; `tool_call` and `tool_call_update` with **diff content** and **locations**; `plan`; `usage_update`; `session_info_update`; `current_mode_update`; `config_option_update`; `available_commands_update` | `prompt.go:192-194`; grep of `acpserver/*.go` |
| Delivery shape | snapshot-shaped updates (each states its whole current value), a short text block before any diff, compact replay, few updates. Designed for this daemon's drop-newest queue and coalescing | gobble `0008-MADR-native-cli-mode.md` D19 |
| Extensions | sixteen `_gobble/*` methods: steer, follow-up and queue control; compact; usage; set name; fork; clone; six state reads | `extensions.go:29-44`; gobble 0009-REPORT M9 |
| Client file and terminal use | when the client advertises `fs` read and write, file tools use `fs/*`; when it advertises `terminal`, `bash` runs through `terminal/*` | `agent.go:467-471`; `clientfs.go`; `clientterm.go` |

## What today's `acpagent` would do with that surface [derived]

| Area | Result | Evidence here |
| :--- | :--- | :--- |
| `initialize`, `session/new`, `load`, `prompt`, `cancel`, `list`, `close` | **works** | typed calls in `acpagent.go`, `sessioncaps.go` |
| MCP servers | **works.** Servers are filtered by the agent's `mcpCapabilities` and sent as a present array. SSE servers are dropped for gobble | `acpagent.go:618-651` |
| `/model`, `/thinking` | **broken.** Raw `session/set_model` gets method-not-found from gobble's SDK, because only `_`-prefixed names reach an extension handler. The model picker reads grok `_meta`, `ListModels` or `StaticModels`, never `configOptions` | `session.go:946-990`; `thinking.go:70`; MADR 0179 F5 |
| `/compact`, fork, rename, usage, undo | **unsafe.** Resolution falls through to grok `_x.ai/*` methods whenever gobble has not advertised the name. The phone shows the command, and it fails | MADR 0179 F1, F2 |
| Session title | **dropped.** `session_info_update` reaches the `default` branch | `session.go:1785-1786` |
| Tool diffs and locations | **degraded.** A diff becomes `"diff <path>"`; locations are ignored | `session.go:2569`; MADR 0179 F4 |
| Plan exit text | **truncated** to 400 runes | `session.go:2113-2117` |
| `usage_update` | `used` and `size` kept; gobble sends no cost yet | `session.go:1757-1764` |
| `/skill:<name>` | runs as prompt text, but never appears in the phone's command list | `internal/session/commands.go:343-345` |
| Mid-turn input | **works only because the daemon queues it.** The SDK's agent side cancels a session's running prompt when another `session/prompt` arrives (`acp-go-sdk2 agent_gen.go:427-428`; gobble M10). `_gobble/steer` exists, but the daemon does not use it | `session.go:292-304` |
| File and terminal callbacks | gobble's file tools and `bash` would run **through this daemon**, because `initialize` advertises `fs` and `terminal` to every agent | `acpagent.go:521-527` |
| Credentials | the child inherits the daemon's environment; no per-provider environment exists | MADR 0179 F8; no `cmd.Env` in `acpagent.go` |

## Evaluation

### Strengths

- **One SDK at both ends.** Request and response types are generated from the same schema on
  both sides. A wire disagreement becomes one SDK fix, not two adapters.
- **gobble already uses the standard forms.** Config options carry categories, `agentInfo` is
  set, stable delete exists, and titles, diffs and locations use their schema shapes. Most of what
  breaks is on this side.
- **gobble's delivery rules match this daemon's limits.** Snapshot-shaped updates make
  `OverflowDropNewest` loss repairable by the next update. MADR 0167's per-turn loss notice stays
  meaningful.
- **Extensions are discoverable.** Reading `_meta.gobble.extensions` at `initialize` lets
  capability reporting follow advertisement, which is what MADR 0179 D2 asks for, without
  probing each method.

### Risks and recommendations

| # | Risk | Recommendation |
| :--- | :--- | :--- |
| R1 | **Both ends on one SDK can hide a shared misreading of the spec.** gobble-to-daemon tests pass while another ACP client or agent fails | Keep grok and a third-party ACP client as cross-checks. Add wire fixtures derived from the upstream schema's examples, so each side is tested against the schema, not only against the other |
| R2 | **`_gobble/*` can become the next `_x.ai/*`.** Sixteen private methods couple the daemon to one agent | Drive gobble with standard ACP first. Use an extension only where schema 1.25 has no form. Candidates to move: fork → unstable `session/fork` behind `sessionCapabilities.fork`; compaction → `compaction_update` once this client advertises `session.compaction`; status → `notice`; questions → elicitation. Steer, follow-up, rename, compact-as-a-request and the state reads have no standard form and justify the extension |
| R3 | **The extension contract can drift.** gobble's extension result types are unexported (`compactResult` and others, `extensions.go:132-289`), so this repository would hand-write the JSON. That is how 0179's `instructions` came to differ from gobble's `customInstructions` (M9) | gobble publishes a small nested module (for example `acpext`) with method names and types, depending only on `acp-go-sdk2`, and this repository imports it. Vendor extensions do not belong in the generic SDK |
| R4 | **gobble's tools would run through this daemon.** On the same host, `fs/*` here is plain OS file I/O (0179 F8), so the hop adds latency and an audit event, and nothing else. `terminal/*` would replace gobble's process-tree `bash` with this daemon's terminal code | Add a per-Spec override for advertised client capabilities. Advertise neither `fs` nor `terminal` to gobble unless a measured reason appears |
| R5 | **The SDK pins differ.** gobble pins `v0.14.0`; this repository's migration needs a later tag (0167-REPORT, Phase 0). Wire shapes match, but decoding strictness changed after `v0.14.0` (lenient-decoding and validation commits) | Both repositories pin the same tag and bump it together, with each bump checked by the `live-gobble` suite |
| R6 | **Credentials have no home.** gobble advertises no auth methods, so its model-provider keys would have to live in this daemon's service environment | Either per-provider environment (MADR 0179 "Deferred"), or gobble implementing ACP `authenticate` with the stable `agent` method so the phone's provider-auth flow (0074) applies. Decide before registration |
| R7 | **Self-update is lopsided.** gobble pins `go-selfupdate-lib v1.9.0`; this repository's 0054 assessment targets `v1.14.0`. This daemon never updates agent CLIs (0179 "Facts"), so on a magic-cli-remote host nothing updates gobble | Align the library version. Decide whether this daemon only warns through `KnownGoodVersion`, or later runs `gobble update` as an operation |
| R8 | **A second `session/prompt` cancels the running turn** (SDK agent behavior, M10) | Never send a second prompt mid-turn. Replace the daemon FIFO with `_gobble/steer` and `_gobble/follow_up` where the agent advertises them. Separately, consider whether the SDK should leave that choice to the agent |

## MADR 0179 against the current facts

| 0179 says | Now | Recommended change |
| :--- | :--- | :--- |
| Provider `IDPigo`, wire id `"pigo"`, `DefaultBin: "pigo"` | the agent is `gobble`; gobble asks for `IDGobble`, `"gobble"` (M1, M3) | rename throughout |
| `_pigo/compact {sessionId, instructions?}`, `_pigo/usage`, `_pigo/set_session_name`, `_pigo/fork` | `_gobble/compact {sessionId, customInstructions?}` returning Pi's `CompactionResult`; `_gobble/usage` → `{text, stats}`; `_gobble/set_session_name {sessionId, name}`; `_gobble/fork {sessionId, entryId}` → `{sessionId, text}`; plus clone, steer, follow-up, queue and reads (M9) | base `SessionOps` on gobble's frozen table; read `_meta.gobble.extensions` for D2 capability reporting |
| Schema 0.13.5: `usage_update` and elicitation unstable | schema 1.25: both stable; `session/set_model` absent from the schema | restate D5 and D6 on schema 1.25 |
| D5 event mapping and D6 config-option model selection | the same work as the 0167-REPORT full-adoption L2, L3 and Phase 2b | plan once, in the order given below |
| "Deferred": steer, `clientInfo`, per-provider environment, stdio MCP | gobble implements steer, and accepts stdio MCP (baseline) and additional directories | promote steer and per-provider environment to decisions; `clientInfo` is already in 0167 Phase 2 |
| D10: `session/set_model` gets MethodNotFound from the agent | still true under `acp-go-sdk2` | keep it as a probe |

## Recommended order of work

These are recommendations for later records and plans. None is approved by this report.

1. **SDK.** Tag `acp-go-sdk2` past `v0.14.0`, and add outbound validation if the SDK accepts it
   (0167-REPORT L4). Both repositories then pin that tag.
2. **Records here.** Amend MADR 0179 or supersede it with a gobble record on schema 1.25. Fold in
   the 0167 full-adoption L2 and L3 decisions, and decide R3, R4 and R6.
3. **This repository, in dependency order:**
   1. 0167 Phase 1, the direct-module migration;
   2. 0179 D2 and D3, capability reporting from the Spec and strict fallback, with failing-first
      tests (the safety prerequisite for any non-grok ACP agent);
   3. 0179 D1, moving grok's vendor operations into its Spec;
   4. full-adoption Phase 2 and 2b: config-option model and thinking, complete update mapping,
      `session.configOptions.boolean` advertised, plan text kept to the 8000-character bound;
   5. 0179 D4, phone menus acting on the resolved mechanism;
   6. gobble registration: `IDGobble`, Spec, strict command table, the per-Spec client-capability
      override, and a `live-gobble` suite with wire captures;
   7. steer through `_gobble/steer`.
4. **gobble.**
   - Publish the extension-types module.
   - Move to standard forms where they exist (R2).
   - Align `go-selfupdate-lib`.
   - Add an auth or credentials path.
   - Bump the SDK in step with this repository.

### Acceptance gates for registration

- With gobble registered, a recorded connection shows no `_x.ai/*` method across `/compact`,
  `/fork`, `/undo`, `/thinking`, phone fork and phone rename (0179 D10).
- `/model` and `/thinking` produce `session/set_config_option` frames and update the phone's model
  and thinking state.
- `session_info_update` reaches the phone as a title. Diffs and locations reach the phone within
  the frame budget.
- `/compact` typed before the first `available_commands_update` is refused, not sent as a vendor
  call. The test is first seen failing on the current resolver.
- No `fs/*` or `terminal/*` request arrives from gobble when the Spec advertises neither.
- `agentInfo.version` of a dev build does not match the `KnownGoodVersion` pin.

## Decisions for the owner

1. Amend MADR 0179 in place, or supersede it with a gobble record.
2. Whether this daemon advertises `fs` and `terminal` to gobble (R4).
3. Whether gobble publishes a shared extension-types module (R3).
4. Where gobble's model-provider credentials live on a magic-cli-remote host (R6).
5. Whether this daemon ever updates gobble, or only warns (R7).

## Conclusions

gobble is a credible native agent for this platform. It speaks current, standard ACP, it was
shaped around this daemon's delivery limits, and it shares the SDK this repository is moving to. The
work left is on this side, and most of it is already named: the 0167 full-adoption phases make this
daemon a standards-current ACP client, and MADR 0179's safety decisions make a second ACP agent safe
to register. Planned together, and with the five decisions above, gobble can be registered without
special-casing beyond a Spec, a command table and a small extension surface.

No source in either repository was changed, and no probe was run, for this report.

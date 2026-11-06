---
status: accepted
date: 2026-11-03
decision-makers: Project Owner
consulted: "opencode 2.2.3 probes on isolated homes and its source at b44b7164ba ('release: v2.2.3'); codex-cli 0.166.0 app-server v2 schema; grok 1.0.70 `plugin` subcommands; gobble-cli at dded45c"
informed: Implementers of the provider layer, the daemon protocol and the phone
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Add an agent-neutral plugins section with declared read and manage operations

## Context and Problem Statement

Agent CLIs now extend themselves with plugins. A plugin can bring skills, MCP servers, hooks,
agents, commands, tools or a UI, and it can fail to load. When it fails, the session behaves
differently and the phone has no way to say why. Magic shows none of this today:

* `provider.Diagnostics` (`internal/provider/provider.go:623`) carries branch, VCS, MCP, skills,
  LSP and formatters. It has no plugin field.
* No Go code outside the test data names an agent plugin. The phone's only "plugins" are Flutter
  plugins.
* The phone's diagnostics sheet (`apps/mobile/lib/features/chat/diagnostics_sheet.dart`) has
  Skills, Language services, Formatters and MCP servers. It has no Plugins section.

Earlier records each took a position for one engine, and none for the others:

* [0080-MADR-add-first-class-codex-collaboration-modes-and-app-server-command-parity.md](0080-MADR-add-first-class-codex-collaboration-modes-and-app-server-command-parity.md)
  D14 keeps plugin installation out of the provider commands.
* [0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md](0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md)
  D18 decides complete plugin and marketplace administration for codex, and D38 keeps production
  plugin mutation deferred while the official app-server documentation labels it under
  development. Its PLAN's P13 has not started (status note, 2026-10-18).
* [0112-MADR-opencode-1.18.21-surface-parity.md](0112-MADR-opencode-1.18.21-surface-parity.md)
  A12 excludes OpenCode plugin administration for 1.x.
* [0081-MADR-grok-1.0.3-surface-parity.md](0081-MADR-grok-1.0.3-surface-parity.md) leaves
  `grok plugin marketplace` unwrapped as an operator utility, and
  [0106-MADR-grok-1.0.5-surface-parity.md](0106-MADR-grok-1.0.5-surface-parity.md) defers the ACP
  `x.ai/pluginDirs` injection.
* [0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md)
  D10 lists `/api/plugin` among the diagnostics routes without saying what the phone shows.

Since then,
[0208-MADR-make-the-provider-layer-agent-neutral.md](0208-MADR-make-the-provider-layer-agent-neutral.md)
made the provider layer agent-neutral. Sessions declare what they support (D1), vendor behaviour
lives in its vendor's package (D2), and the phone gates on capabilities, never on provider ids
(D4). A codex-only plugin screen, as 0109 P13 drew it, would now be the kind of vendor branch
0208 removed.

The question: **what does a plugins section show and let the owner do, for every engine that
has plugins, so that it is useful, safe on a public protocol, and does not have to be rebuilt
for each new engine?**

## Decision Drivers

* **Parity through declaration (0208 D1, D4).** One model and one phone section. Each session
  declares which plugin operations it really has, and the phone shows only those.
* **Pass-through first.** Show what the engine reports, in the engine's terms. Magic does not
  invent a plugin state the engine did not report, and it does not maintain its own plugin store.
* **Nothing sensitive crosses.** No host paths, plugin code, plugin options, URLs or credentials
  reach the phone, a log or the wire capture. This repository and its protocol are public.
* **Mutations are host-wide.** Updating or installing a plugin changes the agent for every
  session and every project on that host, not just this session. That needs a stronger gate than
  a read.
* **Engine maturity is respected.** Where the engine labels a surface experimental or under
  development, Magic does not ship mutation through it by default (0109 D38 is the precedent).
* **Evidence before declaration.** A capability is declared only for a shape a probe or a
  pinned live test has shown.

## Considered Options

* **A. Agent-neutral inventory, with each manage operation declared on its own and gated by
  operator policy.**
* **B. Read-only inventory in the diagnostics answer, with no actions.**
* **C. Per-engine plugin screens (0109 P13 as written, plus an OpenCode screen and a grok
  screen).**
* **D. No plugins section. Leave plugins to each engine's own CLI or TUI.**

## Decision Outcome

Chosen option: **"A. Agent-neutral inventory, with each manage operation declared on its own
and gated by operator policy"**, because it is the only option that makes the section useful on
every engine with plugins while keeping 0208's guarantees. B leaves a failed plugin visible but
unrecoverable from the phone. C rebuilds the vendor branches 0208 removed. D leaves the most
common cause of an unexplained capability change unexplained.

The decision has seven parts.

**D1 — One plugin model.**

* `provider.PluginInfo` describes one installed plugin:
  * `ID`, and a display `Name` (the engine's display name, else the id, else the package or
    directory basename);
  * `Version`;
  * `Kind`: `builtin`, `package`, `local`, `marketplace`, `sdk` or `other`;
  * `Scope`: `user`, `project`, `managed` or empty when the engine does not say;
  * `State`: `active`, `failed` or `disabled`;
  * `Error`: present only when failed, sanitized under D4;
  * `Provides`: what the plugin contributes, as flags (skills, MCP, hooks, agents, commands,
    tools, UI), only where the engine reports them;
  * `Update`: whether a newer version is known, and whether an update is running.
* `provider.PluginInventory` is the list, a `Truncated` flag, and the time it was read.
* The model has no field for a path, a URL, an option, code or a credential, so nothing can put
  one there by accident. Each engine's decoder drops those fields when it decodes, as
  `SkillInfo` already drops a skill's location and content.

**D2 — Operations are declared one by one.**

* `provider.PluginSession` is an optional session interface:
  * `ListPlugins(ctx) (PluginInventory, error)`;
  * `PluginAction(ctx, PluginActionRequest) (PluginActionResult, error)`, where the action is
    one of `check_updates`, `update`, `enable`, `disable`, `install` or `uninstall`, with a
    plugin id (or none, for "check all").
* `provider.Capabilities` gains a plugins value: `List` plus one flag per action. acpagent
  declares it from new `Spec.Ops` hooks. httpagent, codex and fake declare it from what their
  engine offers. A nil hook means the operation is not offered.
* The manager's checks read the declaration, as they do for workspace and shell. The phone
  receives it in `session_capabilities` and shows a control only for a declared action.
* An action the session did not declare answers "not offered" without reaching the engine.

**D3 — What each engine declares now.**

| Engine | List | Actions | Source |
| --- | --- | --- | --- |
| opencode v2 (ACP + control plane) | yes | `check_updates`, `update` (package plugins only), behind D5's operator policy | `GET /api/plugin`, `POST /api/plugin/check`, `POST /api/plugin/update` |
| codex app-server | yes | as 0109 D18 decides, each still behind 0109 D38's maturity gate and D5's policy | `plugin/list`, `plugin/installed`, `plugin/read`, `plugin/install`, `plugin/uninstall`, config writes for enable and disable |
| grok | yes, once the shape of an installed plugin is pinned (below) | none yet | `grok plugin list --json` |
| opencode 1.x, kilo | no | none | no plugin route; `--pure` disables external plugins entirely |
| gobble | no | none | gobble has no plugin system |

* **opencode v2.**
  * The list waits for plugin activation in the location, as P6b's auth operations do. "A cold
    Location lists no plugins until activation finishes" (`packages/cli/src/acp/catalog.ts` at
    `b44b7164ba`, line 129).
  * Every plugin route is labelled "Experimental plugin routes" in 2.2.3's protocol
    (`packages/protocol/src/groups/plugin.ts` at `b44b7164ba`). The read is admitted because it
    is a GET of status metadata only. The two mutations are admitted only behind D5's policy,
    which defaults off.
  * Enable, disable, install and uninstall are not routes. In v2 they are edits to
    `opencode.json`'s `plugin` list, and 0112 A12 and 0214 keep config mutation out of Magic.
    They stay undeclared, named here rather than hidden.
  * `plugin.update` reloads the plugin in the server it is sent to, which is the control plane.
    Each ACP session runs its own engine, which spawns its own server
    (`packages/cli/src/services/standalone.ts` at `b44b7164ba`), so after a successful update
    the provider calls `RestartSessionsWhenIdle` with a notice, as P6b does after a credential
    change.
  * **`GET /api/plugin/gui` is refused by the control plane**, as `/api/credential`,
    `/api/provider` and `/api/model` are (0214 D2 as amended at P6c). It returns the plugin's
    code and its `opencode.json` options, and options can hold secrets.
* **codex.** 0109 D18 and P13 stay the decision for what codex offers. This record changes only
  how it reaches the phone: through D1's model and D2's operations, not a codex-only screen.
  Codex-only detail (marketplaces, share targets, install and auth policy) travels as optional
  fields the phone renders when present. 0109 D38's maturity gate is unchanged. It is checked
  again against the installed codex before P13 starts, since 0.166.0 is newer than the 0.149.1
  0109 assessed.
* **grok.** grok has no ACP plugin method, so the list comes from `grok plugin list --json`:
  * the session's binary and environment;
  * a fixed argv with no phone-supplied text;
  * a timeout and an output bound.

  This narrows 0081's "operator utilities are not wrapped" for one machine-readable read only.
  On this host the command printed `[]`, so the installed-plugin shape is unverified: `--available`
  showed the marketplace shape (351 entries with name, version, description, marketplace, skill
  count and component flags), not the installed one. grok declares List only after a probe on an
  isolated home with one installed plugin pins that shape. Its CLI mutations (`install`,
  `uninstall`, `enable`, `disable`, `update`) are not declared by this record.

**D4 — Hardening.**

* **Decode-time dropping.** A local plugin's path, a package plugin's install location, any URL,
  options and code are dropped at decode (D1). A local plugin with no id is named by its
  directory's basename, which must contain no path separator.
* **Error text.** A failed plugin's error is clipped to 160 characters (`maxDiagnosticTextLen`).
  If it matches a path, URL or secret pattern, it is replaced whole by a fixed
  "failed to load" text, as codex's doctor sanitizer does (`internal/provider/codex/diagnostics.go`
  `sanitizeDoctor`). The phone learns that a plugin failed, never what the host's layout is.
* **Bounds.** At most 64 plugins in an inventory, marked `Truncated` beyond that. Each name and
  version is clipped. The engine's answer is read with the same response bound as other
  control-plane reads.
* **Logs and capture.** The wire capture records plugin bodies through its existing redaction.
  Nothing outside D1's model is logged at info level.
* **Mutation gate**, in this order:
  1. the operator policy (D5) allows the action for this provider;
  2. the device owns the session (`Manager.Authorize(..., true)`);
  3. the phone has confirmed, with consequence text the daemon supplies;
  4. no other plugin action is running for this provider (one at a time per provider);
  5. the action has a timeout, and an idempotency key, so a resent request does not repeat it.
* **Audit.** Each mutation logs one line: provider, action, plugin id, device id, outcome.

**D5 — Operator policy.**

* A provider config key, `plugins.manage`, lists the actions the operator allows for that
  provider. It is empty by default, so a fresh install offers List only. This follows the shell's
  operator policy, `providers.opencode.allow_remote_shell`, off by default
  ([0112-MADR-opencode-1.18.21-surface-parity.md](0112-MADR-opencode-1.18.21-surface-parity.md) A9;
  `internal/config/config.go:595-602`).
* A session declares an action only when its engine offers it **and** the policy allows it, so
  the phone never shows a control that would be refused.

**D6 — The phone.**

* The diagnostics sheet gains a **Plugins** section, built like its others: a count, one row per
  plugin (name, version, kind, and state, with failed plugins first), and the sanitized error
  under a failed row.
* Tapping a row opens a detail sheet with what the plugin provides, its scope and its update
  state. Declared actions appear there and nowhere else.
* Every label and consequence text that names an engine comes from the daemon (0208 D4). The
  phone compares no provider id.
* After an action, the section re-reads the inventory. A restart notice (D3, opencode v2)
  appears as the existing session notice does.

**D7 — Plugins and skills are linked, not merged.** Where an engine reports which plugin owns a
skill (codex `SkillMetadata.pluginId`), the skill row in
[0229-MADR-make-skill-discovery-freshness-and-refresh-agent-neutral.md](0229-MADR-make-skill-discovery-freshness-and-refresh-agent-neutral.md)
names it, and the plugin's detail sheet lists those skills. Neither record owns the other's data.

### Consequences

* Good, because a failed plugin, the commonest silent cause of a missing skill, MCP server or
  command, becomes visible on the phone for opencode v2, codex and (once pinned) grok.
* Good, because codex's planned administration (0109 P13) lands through the same model, so grok
  and opencode get the same section rather than a second design.
* Good, because the model cannot carry a path, an option or code, so a later engine cannot
  leak one through it.
* Good, because every mutation is off until the operator allows it, and still needs ownership
  and a confirmation.
* Neutral, because opencode v2 enable, disable, install and uninstall stay unavailable. They are
  config edits, and config mutation is outside Magic by earlier decision.
* Bad, because it adds a protocol message pair, a capability value, a config key and a phone
  section, which all have to land together and stay compatible with older phones.
* Bad, because opencode v2's update restarts every session's agent at its next idle point, which
  the owner experiences as a short reconnect.
* Bad, because the grok list depends on a CLI subcommand's JSON, which can change between
  releases without an ACP version signal. The live test (Confirmation) is the guard.

### Confirmation

* **Unit tests** for each engine's decoder: fixtures with a local plugin path, a package
  location, a URL, options and a failed plugin with a path in its error. They assert none of it
  reaches `PluginInfo`, and that the error is replaced.
* **A control-plane test** that `GET /api/plugin/gui` is refused before any request is sent,
  beside `TestNeverCallsProviderRoutes`.
* **Declaration tests** (0208 D7's conformance suite): a session declares an action only when the
  engine hook exists and the policy allows it, and an undeclared action answers "not offered"
  without an engine call.
* **Live-tagged tests**, each on an isolated home and spending no model quota:
  * opencode v2: the list after activation; a failed local plugin's error is sanitized;
  * grok: `plugin list --json` with one installed plugin matches the decoder;
  * codex: `plugin/list` matches the decoder.

  A plugin update reaches the package registry, so that test runs only on the owner's word.
* **Phone widget tests**: the section's rows and states, actions shown only when declared, and
  the confirmation dialog's daemon-supplied text. A Dart test fails if `lib/` compares a provider
  id (0208 D4's existing guard).
* Every new test is seen failing on a deliberately broken copy before it is trusted.

## Pros and Cons of the Options

### A. Agent-neutral inventory, with each manage operation declared on its own and gated by operator policy

* Good, because one section serves every engine with plugins, and a new engine needs only its
  decoder and hooks.
* Good, because per-action declaration matches the real differences: opencode v2 can update but
  not install, and codex can install but has a maturity gate.
* Good, because operator policy keeps host-wide changes off unless the host's owner opts in.
* Neutral, because codex's 0109 D18 scope is kept, while its screen design is replaced.
* Bad, because it is the largest of the four: model, protocol pair, capability, policy and phone.

### B. Read-only inventory in the diagnostics answer, with no actions

* Good, because it is small: one more field in `provider.Diagnostics`.
* Good, because it carries no mutation risk.
* Bad, because a plugin with an update waiting, or a failed one, can be seen but not acted on
  from the phone.
* Bad, because 0109 D18 would still need its own codex screen for administration, which brings
  the vendor branch back.

### C. Per-engine plugin screens

* Good, because each screen can show everything its engine has.
* Bad, because it contradicts 0208 D4: the phone would branch on the provider to choose a screen.
* Bad, because each new engine needs a new screen, and the hardening is repeated per screen.

### D. No plugins section

* Good, because it costs nothing.
* Bad, because a failed plugin stays invisible, and the owner sees only its effects.
* Bad, because 0109 D18, already accepted, still has to be built somewhere.

## More Information

### Probe evidence, opencode 2.2.3

On an isolated home with made-up secrets in its MCP configuration, and no prompt sent:

* `GET /api/plugin` answered 10.8 KB:
  `{id?, source: builtin | package{target, version?, outdated?, updating?} | local{path} | sdk,
  features{server?, tui?, rpc?, gui?}, state: active | failed{error, ref?}}`. No made-up secret
  was echoed. This matches `Plugin.Info` in `packages/schema/src/plugin.ts` at `b44b7164ba`.
* The protocol group lists `plugin.list`, `plugin.gui`, `plugin.check` (`{target?}`, returns the
  list) and `plugin.update` (`{targets[]}`, 204, "notify active locations to reload them"). The
  group's title annotation is "Experimental plugin routes."
* `plugin.gui` returns `{code, revision, options}`, where `options` is "the plugin's options from
  its `opencode.json` entry".
* The ACP catalog loader polls `plugin.list` until it is non-empty before it reads models
  (`packages/cli/src/acp/catalog.ts` at `b44b7164ba`, lines 129-137). That is the activation wait
  D3 copies.

### Other engines

* **codex-cli 0.166.0** app-server v2 schema, generated locally: `plugin/list` (`cwds`,
  `forceRefetch`, `marketplaceKinds`), `plugin/installed`, `plugin/read` (`PluginDetail` with
  skills, apps, hooks and MCP servers), `plugin/install`, `plugin/uninstall` (`pluginId`),
  `plugin/share/*` and `plugin/skill/read`. `PluginSummary` carries `id`, `name`, `enabled`,
  `installed`, `installPolicy`, `authPolicy`, `availability`, `disabledReason`, `localVersion`,
  `interface` and `source`.
* **grok 1.0.70**: `grok plugin list | install | uninstall | update | enable | disable | details
  | validate | tag | marketplace`. `plugin list --json` printed `[]` on this host;
  `plugin list --available --json` listed 351 entries with
  `{status, name, version, description, marketplace, skill_count, has_hooks, has_agents, has_mcp,
  components}`.
* **kilo 7.8.18 and opencode 1.x**: no plugin route; `--pure` runs without external plugins.
* **gobble-cli** at `dded45c`: no plugin system.

### Relation to other records

* Builds on 0208 (D1, D2, D4, D7).
* Narrows 0081's stance for one read (`grok plugin list --json`) and nothing else.
* Keeps 0080 D14 (no plugin installation as a provider *command*), 0109 D18 and D38, and 0112 A12.
* Replaces only the screen design in 0109 PLAN P13 step 5. When this record is accepted, 0109's
  PLAN gets a dated note pointing here.
* Extends 0214 D2's refused routes with `/api/plugin/gui`, and gives 0214 D10's `/api/plugin`
  its use.

### Implementation

Accepted by the owner on 2026-11-03. The PLAN is
[0228-PLAN-add-an-agent-neutral-plugins-section-with-declared-read-and-manage-operations.md](0228-PLAN-add-an-agent-neutral-plugins-section-with-declared-read-and-manage-operations.md).

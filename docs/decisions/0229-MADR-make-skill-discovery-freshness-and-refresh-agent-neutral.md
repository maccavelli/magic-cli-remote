---
status: proposed
date: 2026-11-03
decision-makers: Project Owner
consulted: "opencode 2.2.3 probes on isolated homes (macOS, the Linux test host, the Windows test host) and its source at b44b7164ba ('release: v2.2.3'); codex-cli 0.166.0 app-server v2 schema; grok 1.0.70 and recorded grok 1.0.13 wire frames; gobble-cli at dded45c"
informed: Implementers of the provider layer, the daemon protocol and the phone
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Make skill discovery, freshness and refresh agent-neutral

## Context and Problem Statement

A skill is a `SKILL.md` that teaches the agent a workflow. Owners write them, often through the
agent itself, and expect the next turn to use them. Whether it does depends on the engine, and
Magic today models only one engine's answer.

**What Magic has.**

* `provider.SkillRefreshSession.RefreshSkills(ctx) error` (`internal/provider/provider.go:734-743`)
  is documented as "recycles an idle engine instance so newly written skills become
  discoverable". It is never automatic, and a busy instance refuses with `ErrInstanceBusy`
  ([0112-MADR-opencode-1.18.21-surface-parity.md](0112-MADR-opencode-1.18.21-surface-parity.md)
  A10).
* Only httpagent implements it, for opencode 1.x and kilo. It runs `POST /instance/dispose`, then
  re-reads `/skill` and re-advertises commands (`internal/provider/httpagent/session.go:556-567`).
* `session.refresh_skills` (`internal/protocol/messages.go:1126-1131`) carries only a session id.
  `CapSkillRefresh` is one boolean.
* acpagent has an `Ops.RefreshSkills` hook (0214 PLAN P6a) and no Spec sets it.
* `provider.SkillInfo` is a name and a description (`provider.go:643-646`). Skills arrive only in
  the pull-only `session.diagnostics` answer, and nothing tells the phone they changed.
* The phone ties all of this to one engine:
  * the refresh dialog says "This restarts the OpenCode instance for this project"
    (`diagnostics_sheet.dart:70-73`);
  * the "Create or update with agent" composer names the `customize-opencode` skill and the path
    `.opencode/skills/<name>/SKILL.md` (`skill_authoring_sheet.dart:52-80`);
  * the composer is offered whenever the sheet opens, for any engine.
* codex's `skills/changed` notification is routed provider-wide and then dropped
  (`internal/provider/codex/routing.go:69`, `:128-131`). No codex skill surface exists in Magic.
  [0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md](0109-MADR-expand-codex-provider-through-capability-led-app-server-parity.md)
  D20 decides one, and its PLAN's P12 has not started.

**What the engines do** (evidence in More Information):

| Engine | How a new skill is noticed | Can Magic force it? | Signal of a change |
| --- | --- | --- | --- |
| opencode 1.x, kilo | not until the instance is recycled | `POST /instance/dispose`, idle only | none |
| opencode v2 over ACP | the engine's own server watches the skill directories and rescans | only by restarting the session's agent process | none over ACP; the control plane emits `skill.updated` |
| codex app-server | cached; `skills/list {forceReload:true}` rescans | yes, without disruption | `skills/changed` ("treat this as an invalidation signal") |
| grok over ACP | unverified | a new process re-reads | skills arrive as ACP commands, marked in `_meta` |
| gobble over ACP | re-read on every prompt | not needed | none; skills are not ACP commands |

So "refresh" means a different thing on each engine. On opencode v2 it is usually unnecessary, on
codex it is cheap, and on 1.x it is disruptive. Today's single boolean, dialog and composer fit
only the last of these.

**Question:** how does Magic make a skill the owner adds or changes usable, visible and
verifiable on every engine, through one contract, without disrupting work that does not need it?

## Decision Drivers

* **The skill works on the next turn, and the phone can show it.** The owner should not have to
  guess whether a refresh is needed, or whether it worked.
* **Honest declaration (0208 D1, D4).** Each session declares the mechanism it really has. The
  phone gates on that declaration, never on a provider id, and shows engine names only as text
  the daemon supplies.
* **Least disruption.** Restarting an engine is used only where nothing lighter works, and stays
  confirmed and idle-only (0112 A10).
* **Pass-through first.** Show the engine's own catalog and its own change signals. Magic does
  not scan skill directories itself; that would duplicate each engine's discovery rules.
* **Nothing sensitive crosses.** Skill locations and content never reach the phone (0112 A6).
* **Evidence before declaration.** A mechanism is declared for an engine only after a probe or a
  pinned live test shows it.

## Considered Options

* **A. Declared freshness modes, one refresh contract that returns the catalog, an in-band change
  feed, and descriptor-driven authoring.**
* **B. Keep "recycle", and implement it for every engine.**
* **C. Live only: drop manual refresh and rely on engine watchers.**
* **D. Leave it as it is: refresh for opencode 1.x and kilo only.**

## Decision Outcome

Chosen option: **"A. Declared freshness modes, one refresh contract that returns the catalog,
an in-band change feed, and descriptor-driven authoring"**, because it is the only option that
is correct on all five engines:

* B restarts opencode v2 and codex sessions that never needed it, and on v2 a restart is a full
  agent start.
* C leaves 1.x and kilo with no way to notice a skill at all.
* D leaves v2, codex, grok and gobble without the feature.

The decision has eight parts.

**D1 — A richer, still metadata-only skill model.**

* `provider.SkillInfo` gains:
  * `ID`, the engine's id when it differs from the name;
  * `Scope`: `project`, `user`, `bundled`, `plugin`, `system` or `admin`, or empty when the
    engine does not say;
  * `Plugin`, the owning plugin's id when the engine reports it
    ([0228-MADR-add-an-agent-neutral-plugins-section-with-declared-read-and-manage-operations.md](0228-MADR-add-an-agent-neutral-plugins-section-with-declared-read-and-manage-operations.md)
    D7);
  * `Enabled`, only when the engine reports it.
* `provider.Diagnostics` gains `SkillErrors`: skills that failed to load, with a name and an error
  sanitized as 0228 D4 does. The engine's own load diagnostics are there to explain why a skill is
  missing.
* Location and content stay dropped at decode. Each engine's decoder derives `Scope` from a
  location (for example, under the session root means `project`) before it drops the location.
* Bounds are kept: 64 skills, names 128 and descriptions 512 characters
  (`internal/provider/opencode/diagnostics_surface.go`).
* Sources:
  * opencode v2: `GET /api/skill`, read after plugin activation (D8);
  * codex: `skills/list`;
  * grok: the session's skill-marked commands;
  * opencode 1.x and kilo: `/skill`, as today;
  * gobble: none (D6).

**D2 — Freshness is declared as modes.**

* `provider.Capabilities` replaces the `CapSkillRefresh` boolean with a freshness value of three
  flags. A session may declare more than one.
  * **Live:** the engine notices changes by itself. The phone says skills update automatically,
    in daemon-supplied text that can carry a caveat (D3), and offers **Check again**. That re-reads
    the catalog and asks nothing of the engine.
  * **Rescan:** the engine re-reads on request without disrupting anything. The phone offers
    **Refresh**, with no confirmation.
  * **Recycle:** the engine notices only after a restart. The phone offers **Restart to
    refresh**, confirmed, idle only, as 0112 A10 decided.
* For phones that predate this, `session_capabilities.skill_refresh` stays, set when Rescan or
  Recycle is declared, so an older phone keeps its button.
* Declarations now:

| Engine | Live | Rescan | Recycle |
| --- | --- | --- | --- |
| opencode 1.x, kilo | | | yes (`POST /instance/dispose`, unchanged) |
| opencode v2 | yes | | yes (D4), the fallback |
| codex | | yes (`skills/list {forceReload:true}`) | |
| grok | | | after a probe (below) |
| gobble | yes (read on every prompt) | | |

* grok declares Recycle only after a probe on an isolated home shows a restarted session lists a
  skill added while it ran. That needs no model turn. Whether grok re-sends its commands when a
  skill changes is probed at the same time. If it does, grok also declares Live.

**D3 — One refresh contract, and it returns the result.**

* `RefreshSkills(ctx, mode)` returns the skill catalog after the refresh, so the phone shows the
  outcome in one round trip: "Refreshed: 12 skills, 1 new (`release-notes`)". It does not ask the
  owner to re-open the sheet to find out.
* **Rescan** runs at once and is single-flight per session.
* **Recycle** keeps today's rules. It waits for the session to be idle, refuses with
  `ErrInstanceBusy` when it is not, and is never started automatically.
* The phone states what will happen in text the daemon supplies, for example "Restarts this
  session's OpenCode agent". It names no engine itself (0208 D4).
* **The Live caveat.** On opencode v2 the engine's watcher took 0.5 to 14.7 s on macOS, against
  0.5 s or less on the Linux and Windows test hosts, and one macOS run missed a change at 6 s
  that it picked up later. The Live text therefore says changes appear within seconds, and the
  Recycle fallback stays declared so the owner has a guaranteed route.

**D4 — Restart one session's agent.**

* acpagent gains a per-session form of P6a's `RestartSessionsWhenIdle`. One session, at its next
  idle point, closes its agent process, and resume reattaches it with `session/load`.
* It is agent-neutral, so any ACP Spec can declare Recycle through it once that Spec's probe
  holds.
* For opencode v2 it is the only way to force the engine's view, for two reasons. The engine runs
  its own `opencode serve`, with a password only it knows (`packages/cli/src/services/standalone.ts`
  at `b44b7164ba`). And `POST /api/location/reload` on the control plane rebuilds only the
  control plane's own locations.
* The control plane's `location.reload` is **not** used for refresh. It would make the
  diagnostics answer current while the engine was not, and it cancels pending permissions and
  forms in the process it reaches (its OpenAPI description).

**D5 — An in-band change feed.**

* A session event, `skills_changed`, carries no skill data. It tells the phone to re-read the
  catalog if the diagnostics sheet is open, and to mark the Skills section otherwise.
* Sources:
  * **codex:** `skills/changed`, fanned out to the codex sessions, since the notification names
    no directory;
  * **ACP agents that advertise skills as commands:** an `available_commands_update` whose
    skill-marked set changed. Which commands are skills is a Spec hook (0208 D2): grok marks them
    with `_meta.path` ending in `SKILL.md` and `_meta.scope`. acpagent never reads a vendor
    `_meta` key itself;
  * **after any refresh:** D3's answer is itself the change.
* **Not a source: opencode v2's control-plane `skill.updated`.** Following it means holding
  `GET /api/event` open, which keeps the lease-bound control plane from its idle stop
  (`internal/provider/opencode/control/control.go:52-53`, 10 minutes) for as long as a session
  lives. Live plus **Check again** covers v2 instead. This is named here so it is revisited if the
  control plane gains a cheaper signal.

**D6 — Authoring is a declared descriptor.**

* A Spec or dialect may declare skill authoring:
  * the project skill directory, relative to the session root;
  * an optional helper skill the agent should use;
  * the label text.
* The phone composes its prompt from that descriptor and offers the composer only when it is
  declared. It never writes a skill file itself (0112 A10).
* The prompt asks the agent to write `<dir>/<name>/SKILL.md` under the agent's normal tools and
  permissions. When the authoring turn ends, the phone shows the session's freshness control
  (D2).
* Declarations:
  * **opencode 1.x:** today's text, `.opencode/skills` with helper `customize-opencode`;
  * **opencode v2:** `.opencode/skills` (watched; probe below) with helper `opencode`, the
    built-in whose description covers "using or configuring" OpenCode. 2.2.3 ships no
    `customize-opencode` (its built-ins are `opencode`, `report` and `browser`), so today's
    composer would name a skill v2 lacks;
  * **kilo, grok, codex:** declared once each one's project skill directory is verified. grok
    bundles `create-skill`; codex follows 0109 P12.
  * **gobble:** reads a trusted project's skill directories (`acpserver/sysprompt.go`
    `skills`); declared when gobble registers (0208 P6).

**D7 — Invocation stays pass-through.**

* Where the engine advertises skills as commands (grok, opencode 1.x), the command picker already
  shows them.
* codex's skill inputs follow 0109 P12.
* **opencode v2 over ACP has no direct invocation.** Its ACP layer has no skill handling
  (`packages/cli/src/acp` at `b44b7164ba`), and the HTTP prompt route's `skills` field is not on
  the ACP path. The model still loads a skill through its `skill` tool when the task matches, and
  the owner can ask for one by name. This is a named gap. An upstream issue is opened for it, as
  #34 was for full-text search.

**D8 — The v2 skill list waits for activation.**

* A cold location answered a partial list (2 of its 3 built-in skills) before plugins activated.
* The v2 read therefore waits for activation as P6b's auth operations do, and that read lands
  with [0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md)
  P6d, which already reads `/api/skill`.
* This record owns everything after the read: freshness, refresh, the feed, authoring and the
  richer model.

### Consequences

* Good, because each engine gets the cheapest correct route: nothing on gobble, a free re-read on
  opencode v2, a no-confirmation rescan on codex, and the confirmed restart only where nothing
  else works.
* Good, because the phone can say "Refreshed: 1 new" instead of "Skills refreshed.", so the owner
  knows the skill is there before the next turn.
* Good, because a failed skill now shows why it is missing.
* Good, because the composer stops naming a helper skill and a directory that may not exist on
  the session's engine.
* Neutral, because opencode v2 skills still reach the model only through its own instructions and
  `skill` tool. Magic adds no invocation path.
* Bad, because the capability changes shape, and the old boolean has to be kept and derived for
  one protocol version.
* Bad, because opencode v2's guaranteed refresh is a full agent restart, measured in seconds to a
  minute on a cold engine (0214 A6's start bound is 120 s).
* Bad, because v2 has no push signal under this record, so an open sheet does not update by
  itself.

### Confirmation

* **Declaration tests** in the conformance suite (0208 D7). Each session's freshness modes match
  its hooks, and a mode it did not declare answers "not offered" without an engine call.
* **Contract tests:**
  * Rescan returns the new catalog;
  * Recycle refuses while busy and returns the catalog after the restart;
  * the per-session restart reattaches with `session/load` and touches no other session.
* **Feed tests:**
  * codex `skills/changed` reaches every codex session;
  * a grok-shaped `available_commands_update` with a changed skill set emits `skills_changed`,
    and one with only non-skill changes does not;
  * acpagent production code names no vendor `_meta` key (0208 D2's source test).
* **Decoder tests:** a v2 `/api/skill` fixture carrying `path` and `content` yields no location
  or content, and derives `Scope`.
* **Live-tagged tests**, on isolated homes with the file watcher on and no model turn:
  * v2: a project skill added while the engine runs appears in the engine's rescan;
  * v2: the per-session restart lists it on `session/load`;
  * grok's probe, once that Spec declares anything.
* A prompt-level test that a running v2 session's model sees a new skill (More Information,
  "Running sessions are told") uses the free model and runs only on the owner's word.
* **Phone widget tests:**
  * each mode shows its control and only its control;
  * the confirmation appears only for Recycle;
  * the composer appears only with a declared descriptor, and its prompt uses the descriptor's
    directory and helper.
* Every new test is seen failing on a deliberately broken copy before it is trusted.

## Pros and Cons of the Options

### A. Declared freshness modes, one refresh contract that returns the catalog, an in-band change feed, and descriptor-driven authoring

* Good, because it is correct on every engine and disrupts only where it must.
* Good, because it keeps 0112 A10's safeguards exactly where a restart happens.
* Good, because the composer and dialog become data, so a new engine needs no phone change.
* Neutral, because codex's 0109 D20 scope is kept; only its route to the phone changes.
* Bad, because it is the largest change: model, capability, protocol event, acpagent restart,
  Spec descriptors and phone.

### B. Keep "recycle", and implement it for every engine

* Good, because the contract and the phone barely change.
* Bad, because it restarts opencode v2 sessions that had already noticed the skill, and codex
  sessions a free rescan would serve.
* Bad, because the confirmation dialog then appears where nothing disruptive was needed, which
  teaches the owner to dismiss it.

### C. Live only

* Good, because it is the simplest for the owner where it works.
* Bad, because opencode 1.x and kilo do not watch, so their skills would never appear.
* Bad, because on macOS a v2 change took up to 14.7 s, and the owner would have no way to make
  sure.

### D. Leave it as it is

* Good, because it costs nothing now.
* Bad, because opencode v2, the 0214 default path, would have no refresh, and its phone text
  would describe 1.x.
* Bad, because codex's change signal stays dropped and 0109 D20 still needs building.

## More Information

### Probe evidence, opencode 2.2.3

All probes ran on isolated homes (`HOME`, the XDG directories, `OPENCODE_CONFIG_DIR` and
`OPENCODE_DB` under a scratch directory) with the file watcher on, which Magic never turns off,
and sent no prompt.

* **The engine watches, and rescans.** `opencode acp` spawns its own `opencode serve --stdio
  --port 0` (the engine's log: `spawning process … args=["serve","--stdio","--port","0"]`).
  That server subscribes watchers to the global config directory and the project's `.opencode`.
  After a project skill and then a global skill were written while the engine ran, its log showed
  `skills rescanned` for each, with the new skill in the list.
* **Over ACP, nothing is announced.** The first session's commands were `compact`, `goal`, `init`
  and `review`. No skill, whether present at start or added later, appeared among any session's
  commands. No `available_commands_update` or other notification followed either change, in
  three runs. Skills are not ACP commands in v2
  (`packages/cli/src/acp/catalog.ts`: commands come from `command.list`; the catalog reloads on
  `model|agent|command.updated` only).
* **Watcher latency.** Through the control plane's skill list, polled every 0.5 s, in two rounds
  of three cases (project skills directory present at start, absent at start, global):
  * macOS: 14.7, 12.2, 1.0, 13.2, 3.5 and 11.4 s;
  * the Linux test host: 0.5 s in all six;
  * the Windows test host: 0.5 s in all six.

  `skill.updated` arrived 0.1 to 0.5 s before the list showed the change, on every host. An
  earlier macOS run with engine and control plane together missed a project skill at 6 s and
  showed it by the next check. The macOS runs used the system temporary directory, and the
  latency was not tested under the home directory.
* **A cold location is partial.** The first `/api/skill` in a new location answered 2 rows. After
  `/api/plugin` answered, it gave 3: `opencode` ("…using or configuring it…"), `report` and
  `browser`. Rows carry `id`, `name`, `description`, `path` and `content` (4.6 to 12.7 KB each).
* **`POST /api/location/reload`** answered 204. It emitted `location.shutdown`, then every
  `*.updated` event including `skill.updated`, and the list then held the new skill.
* **Running sessions are told.** `SkillInstructions` (`packages/core/src/skill/instructions.ts`
  at `b44b7164ba`) diffs the skill list and renders "New skills are available…", changed entries
  and "…no longer available and must not be used" into the session's instructions. The skill
  tool reads the live catalog when called (`packages/core/src/tool/plugin/skill.ts`). This is
  source-level. Pinning it live needs a model turn.
* **Source pin.** Between `b44b7164ba` and the working tree read (`97a71f0cc6`), the skill watcher,
  `SkillInstructions`, the ACP catalog, `location.reload` and the standalone spawn are unchanged.
  `skill.ts` and the skill tool changed only for virtual skill files.

### Other engines

* **codex-cli 0.166.0** schema: `skills/list {cwds?, forceReload?}` ("bypass the skills cache and
  re-scan skills from disk"); `skills/changed` ("Treat this as an invalidation signal and re-run
  `skills/list`…"); `SkillMetadata {name, description, enabled, scope: user|repo|system|admin,
  pluginId?, path, interface?, dependencies?}`; `SkillsListEntry {cwd, skills, errors[{message,
  path}]}`; `skills/config/write {enabled, name|path}`; `skills/extraRoots/set`.
* **grok** (wire frames recorded at 1.0.13, `internal/provider/grok/testdata/wire/1.0.13/frames.jsonl`):
  skills are ACP commands with `_meta {bareName, qualifiedName, scope: user|bundled, path:
  …/SKILL.md}`. 1.0.70's `--help` has no skill subcommand.
* **gobble-cli** at `dded45c`: skills load on every prompt from a trusted project's skill
  directories, the config directory's `skills` and `~/.agents/skills`, with load diagnostics
  (`acpserver/sysprompt.go`). ACP commands are prompt templates read once.
* **opencode 1.x and kilo:** 0112 A10's recycle, implemented in httpagent.

### Relation to other records

* Amends 0112 A10. Its confirmation and idle-only rules now apply to the Recycle mode, and
  refresh is no longer defined as a recycle.
* Changes 0214 PLAN P6a's `Ops.RefreshSkills` hook signature (D3), adds the per-session restart
  (D4), and leaves the v2 skill read in 0214 P6d (D8).
* Implements 0109 D20's forced refresh and live change events through this model. 0109 P12's
  screens are replaced by this record's section. Enable and disable, extra roots and inputs stay
  0109's, declared through the same capability pattern when P12 runs.
* Pairs with 0228 for the plugin link (D1, 0228 D7).

### Implementation

A PLAN is written after this record is accepted. Its expected order: the model and the capability
(with the derived boolean); the contract and the per-session restart; opencode v2's declarations
and descriptor; the phone; codex's Rescan and feed; the grok probe and declarations.

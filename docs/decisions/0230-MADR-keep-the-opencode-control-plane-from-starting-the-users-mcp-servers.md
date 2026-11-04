---
status: accepted
date: 2026-11-03
decision-makers: Project Owner
consulted: "opencode 2.2.3 probes on isolated homes, and its source at b44b7164ba ('release: v2.2.3'); 0214-PLAN-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md, P6d Deviation 1"
informed: Implementers of the opencode v2 provider
---
<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 MD060 -->

# Keep the OpenCode control plane from starting the user's MCP servers

## Context and Problem Statement

[0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md)
runs OpenCode v2 as two kinds of process:

* each session's engine, `opencode acp`, which spawns its own `opencode serve` with a password
  only it knows (`packages/cli/src/services/standalone.ts` at `b44b7164ba`);
* one lease-bound control plane, `opencode serve --stdio`, which Magic starts on first use and
  stops after 10 idle minutes (`internal/provider/opencode/control/control.go`).

The control plane serves what ACP does not, all through `control.Plane.Do`:
* auth (P6b, `acp_auth.go`) and the vendor step (P6c, `acp_catalog.go`), in the plane's default
  location, the user's home (`control.go`: `cmd.Dir = home`);
* workspace, diagnostics and diff (P6d, `acp_surfaces.go`), in each session's directory.

When OpenCode boots a location, it starts every configured MCP server in the background
(`packages/core/src/mcp/index.ts:660-688`). The control plane is a full OpenCode process, so it
does the same. A probe proved it (P6d Deviation 1):
* the user's local MCP server was started by the engine at `session/new`;
* and again by the control plane at its first `GET /api/integration`, the call every P6b auth
  operation makes first.

A second probe (More Information) shows the copies are per location. A control plane that has
served auth and one project starts two copies of each server, beside the engine's own. A server
that a plugin adds (`ctx.mcp.transform`, as the bundled Figma plugin does) is copied the same way.

The copies do nothing Magic uses:
* Magic's auth and vendor step leave MCP sign-ins out (`acp_auth.go:452`, `:475`;
  `acp_catalog.go:70`);
* P6d reads no MCP state from the control plane.

They are not free, though:
* a local MCP server is a user program, often with side effects at start: a database
  connection, a lock, a token refresh;
* it now runs two or three times, for as long as the control plane lives.

Nothing registers the v2 provider until 0214's P7, so no phone has met this. The owner decided to
fix it in a record of its own before P7: "Own record; P6d omits MCP".

**Question:** how does Magic keep the control plane from starting the user's MCP servers, without
changing the engine's and without losing what the control plane serves?

## Decision Drivers

* **The engine is untouched.** The user's MCP servers run where the model uses them, exactly as
  configured.
* **Every source is covered.** Servers from config files and servers that plugins add.
* **The control plane keeps working.** Integrations, skills, plugins, workspace, VCS, diff, and
  P6e's revert and shell all answer as before.
* **Magic does not reimplement OpenCode's config.** It does not parse the user's layers, which
  include JSONC, variable substitution, project discovery and organisation documents.
* **Evidence before reliance.** The mechanism is shown on 2.2.3 and pinned by a live test that
  spends no model quota.

## Considered Options

* **A. A deny-all `mcp.use` policy in a config file of Magic's own, given to the control plane
  as `OPENCODE_CONFIG`.**
* **B. The same policy in `OPENCODE_CONFIG_CONTENT`.**
* **C. Remove the built-in `opencode.config.mcp` plugin in the control plane
  (`plugins: ["-opencode.config.mcp"]`).**
* **D. Disable each server by name, from names Magic reads out of the user's config.**
* **E. One process for both roles: the engine attaches to a server Magic starts.**
* **F. Start the control plane with no user config (`OPENCODE_CONFIG_DIR` empty,
  `OPENCODE_DISABLE_PROJECT_CONFIG`).**

## Decision Outcome

Chosen option: **"A. A deny-all `mcp.use` policy in a config file of Magic's own, given to the
control plane as `OPENCODE_CONFIG`"**, because:

* it is the only option shown to stop every copy while the control plane keeps serving;
* OpenCode's own policy layer applies it after every source has added its servers;
* the explicit file outranks the user's global, direct and project files, so a project policy
  that allows a server does not bring the copy back (probe case D).

The decision has five parts.

**D1 — The policy.** The control plane is started with a config file holding exactly:

```json
{"policies":[{"action":"mcp.use","resource":"*","effect":"deny"}]}
```

* `opencode.config.policy` removes every server a `mcp.use` deny matches, on the final list
  (`packages/core/src/config/plugin/policy.ts:82-87`).
* That plugin is guarded, so config cannot remove it (`packages/core/src/plugin/internal.ts:288`).
  It also activates after the built-ins that add servers (`post`, `:264-283`).
* The file holds no secret. It is written by Magic before each start, with that content alone.

**D2 — How it reaches the control plane.**

* **The usual case:** `OPENCODE_CONFIG` names the file, in the control plane's environment only
  (`control.Config.Env`). The engine's environment is unchanged.
* **When the operator already sets `OPENCODE_CONFIG`,** replacing it would drop the operator's
  file from the control plane. Then the policy goes in `OPENCODE_CONFIG_CONTENT`.
  * If the operator also sets `OPENCODE_CONFIG_CONTENT` to a JSON object, Magic appends the
    statement to that object's `policies`. A later statement within a document wins
    (`packages/core/src/managed-policy.ts:29-35`).
  * If the content cannot be extended, because it is not a JSON object, the control plane starts
    without the policy. Magic logs a warning once per start that names this record.
  * The content document loads after the user's files, so in this case a user policy that allows
    a server keeps that server's copy (probe case C). That is named, not hidden.

**D3 — What stays as it was.**

* Managed (MDM or system) policies and an organisation's Console or well-known documents load
  earlier and outrank any file the user or Magic supplies
  (`packages/core/src/config.ts:363-373`). An organisation policy that explicitly allows a server
  keeps its copy. Magic does not try to outrank an organisation.
* `/api/mcp` on the control plane now lists nothing, and P6d reads it no longer. MCP status for v2
  stays omitted from diagnostics, as P6d records.

**D4 — The pin.** A live-tagged test, on an isolated home with no prompt:

* starts the control plane through `control.New`, the production path;
* configures a local MCP server and a plugin-added one, each of whose commands writes a marker;
* calls the auth and project routes P6b and P6d call.

It asserts that no control-plane marker appears. As its own control, it first runs a plain
`opencode serve` without the policy and asserts the markers *do* appear, so the test cannot pass
by seeing nothing.

**D5 — The related finding, not decided here.** The control plane also runs every user plugin's
`setup` (probe: the probe plugin `active` in the control plane). Denying `plugin.use` would also
remove plugins from the list
`0228-MADR-add-an-agent-neutral-plugins-section-with-declared-read-and-manage-operations.md`
reads, so it is not part of this record. If plugins with side effects at setup prove to matter,
they take a record of their own.

### Consequences

* Good, because each local MCP server runs once, in the engine, as configured.
* Good, because remote MCP servers, and their OAuth refreshes, are no longer driven from a
  second process.
* Good, because the fix is one environment variable and one small file. Nothing in Magic parses
  OpenCode config.
* Neutral, because the control plane's `/api/mcp` becomes empty, which nothing in Magic reads.
* Bad, because an operator who sets `OPENCODE_CONFIG` falls back to the weaker content
  precedence, and one who also sets `OPENCODE_CONFIG_CONTENT` to something other than a JSON
  object gets no policy at all, with only a warning.
* Bad, because a later OpenCode could change policy evaluation or the load order. The live test
  is the guard, and it runs at each engine pin.

### Confirmation

* Unit tests in `internal/provider/opencode`:
  * the control plane's environment carries `OPENCODE_CONFIG` naming a file with exactly D1's
    content, and the engine's does not;
  * an operator `OPENCODE_CONFIG` moves the policy into the content;
  * an operator JSON object in the content gains the statement, and keeps every other key;
  * an operator value that cannot be extended leaves the content alone and logs the warning.
* The live test of D4 on macOS, the Linux test host and the Windows test host.
* Each new test is seen failing on a deliberately broken copy before it is trusted.

## Pros and Cons of the Options

### A. A deny-all `mcp.use` policy in a config file of Magic's own, given to the control plane as `OPENCODE_CONFIG`

* Good, because it covers config-file and plugin-added servers alike (probe case B, the same
  policy).
* Good, because the explicit file outranks the user's global, direct and project files (probe
  case D).
* Good, because nothing else the control plane serves changed in the probe: 237 integrations, 3
  skills and 92 plugins answered, as without it.
* Bad, because it needs a file and a fallback for an operator who already sets `OPENCODE_CONFIG`.

### B. The same policy in `OPENCODE_CONFIG_CONTENT`

* Good, because it needs no file.
* Bad, because the content loads after project files, so a project policy allowing a server
  brings its copy back (probe case C).
* Neutral, because it is A's fallback in D2.

### C. Remove the built-in `opencode.config.mcp` plugin

* Good, because the plugin directive exists and the plugin is not guarded
  (`packages/core/src/plugin/supervisor.ts:39-44`).
* Bad, because it stops only config-file servers. A plugin that adds a server through
  `ctx.mcp.transform` still starts it, and probe case A showed such a server copied.

### D. Disable each server by name

* Good, because it uses OpenCode's per-server `disabled`.
* Bad, because the names come from config layers Magic would have to parse as OpenCode does:
  JSONC, substitutions, project discovery, organisation documents and plugins. Every route that
  reports them boots a location first, which starts the servers.

### E. One process for both roles

* Good, because it would remove the duplicate at its cause.
* Bad, because 2.2.3 cannot do it. `opencode acp` has no attach flag (`--help`: only `--login`),
  and its handler always calls `Standalone.start()` with no options
  (`packages/cli/src/commands/handlers/acp.ts`), which spawns its own `serve`.

### F. Start the control plane with no user config

* Good, because no user server is configured at all.
* Bad, because the control plane would also lose the user's skills, plugins and config-defined
  providers, which P6c's vendor names, P6d's skill list and 0228's plugin list read.

## More Information

### Probe evidence, opencode 2.2.3

Isolated homes (`HOME`, the XDG directories, `OPENCODE_CONFIG_DIR` and `OPENCODE_DB` under a
scratch directory), with no prompt sent (`mcp/probe_policy.py` in the session scratchpad). Each
home's global config declared a local MCP server `marker`. A global plugin
(`plugins/probe-mcp.js`) added a second local server, `plugin-marker`. Both servers' commands only
append `<tag> <role> <pid>` to a file and exit.

| Case | Control plane's environment | Project config | Copies started |
| --- | --- | --- | --- |
| A | nothing added | none | `config control-plane` ×2, `plugin control-plane` ×2 (the home location and the project location) |
| B | `OPENCODE_CONFIG_CONTENT` = D1's policy; an engine also ran | none | `config engine`, `plugin engine`: none in the control plane |
| C | as B, no engine | allows `mcp.use` `marker` | `config control-plane` |
| D | `OPENCODE_CONFIG` = a file with D1's policy | allows `mcp.use` `marker` | none |

In every case `GET /api/integration` (home and project), `/api/skill` and `/api/plugin` answered
200. The counts were 237 integrations (239 in B, the one case where an engine had also run), 3
skills and 92 plugins, with the probe plugin `active`. `/api/mcp` in the project listed both
servers `pending` in A, `marker` in C, and none in B and D.

### Source, at `b44b7164ba`

* Load order and precedence: `packages/core/src/config.ts:363-373` returns managed policies,
  integration documents, well-known documents, global, explicit, direct, project, content, then
  managed settings.
* `ManagedPolicy.statements` reverses that order, so "earliest-loaded first so it wins"
  (`packages/core/src/managed-policy.ts:9-35`).
* `OPENCODE_CONFIG` and `OPENCODE_CONFIG_CONTENT` are read from the `serve` process's environment
  (`packages/cli/src/server-process.ts:132-133`).
* v2 has no switch for MCP as a whole. `Config.MCP` is `{timeout, servers}`
  (`packages/schema/src/config/mcp.ts`).

### Relation to other records

* Carries 0214 PLAN P6d Deviation 1's decision, "Own record; P6d omits MCP". It is to land before
  0214 PLAN P7.
* Leaves 0214 D10 (MCP omitted from v2 diagnostics) as amended at P6d.
* Names, and does not decide, the plugin-setup finding (D5), which touches 0228.

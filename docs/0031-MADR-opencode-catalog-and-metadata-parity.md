# MADR 0031: OpenCode catalog correctness and bounded session metadata

- **Status**: Accepted — implemented
- **Date**: 2026-07-26
- **Deciders**: Project Owner
- **Scope**: OpenCode 1.18.5 through the existing shared HTTP+SSE provider;
  catalog correctness and the smallest useful session/project metadata surface.
- **Related**: [MADR 0019](./0019-opencode-process-management-plan.md),
  [MADR 0020](./0020-opencode-session-tree.md),
  [MADR 0022](./0022-plan-mode-parity.md),
  [MADR 0023](./0023-canonical-slash-commands.md), and
  [protocol-v1.md](./protocol-v1.md).
- **Implementation plan**:
  [0031-plan-opencode-catalog-metadata-parity.md](./0031-plan-opencode-catalog-metadata-parity.md).

## Context and evidence

The installed `opencode` reports **1.18.5**. Its loopback `serve` instance
returned an OpenAPI 3.1 document with 162 paths and 472 schemas. The adapter
correctly uses that server's HTTP API and `/global/event` SSE stream, rather
than creating one ACP process per session. The live ACP initialize probe is
useful for interoperability, but it is not the right replacement transport:
it advertises protocol v1, stdio JSON-RPC, image/embedded-context prompts,
session load/list/resume/fork/close, and MCP HTTP/SSE; it does not provide the
HTTP-only session-tree, async SSE, undo/revert, or rich control-plane surface.

The current live `GET /agent` catalog contains:

| Agent | OpenCode mode | Correct remote meaning |
|---|---|---|
| `build`, `plan` | `primary` | selectable top-level work / session mode |
| `explore`, `general` | `subagent` | engine-invoked delegated work; not a user turn target |
| `compaction`, `summary`, `title` | hidden internal primary | never user-selectable |

`internal/provider/opencode/mode.go` already excludes subagents when it builds
switchable modes because they cannot run a user turn. In contrast,
Previously `ListAgentsLive` put visible subagents in `agents.list`; the mobile
new-session agent picker consumed that catalog directly. The implementation now
uses one startable-agent predicate for picker, mode, and server-side validation.

There is a second catalog inconsistency. `httpagent.Provider.Start` correctly
uses `providers.opencode.model` when a session is created without an explicit
model. But `ListModelsLive` sets its default to OpenCode's
`default["opencode"]`, and `picker.MergeLiveStatic` gives a live default
precedence over the configured static default. The picker can therefore display
a different default from the model the daemon will actually use. This affects a
configured non-Zen default such as `opencode-go/deepseek-v4-flash`.

The live API exposes `PATCH /session/{id}` for a title, `GET /vcs` and
`GET /vcs/status`, and `GET /mcp` status. Those are useful, bounded metadata
surfaces. In contrast, `/experimental/session` lists provider-native history
outside mcremote ownership, `/pty` and `/session/{id}/shell` grant arbitrary
terminal access, and MCP/provider/config/OAuth routes administer host secrets.

## Decision

Adopt the following bounded scope. Native OpenCode historical-session discovery
is explicitly **out of scope**: existing daemon-owned sessions already resume
through their persisted agent-session ID, and no product need justifies an
experimental cross-project catalog.

### P0 — make catalog choices truthful and enforceable

1. **Top-level OpenCode agent catalogs contain only startable agents.**
   `agents.list` and its static fallback include visible `primary` and `all`
   agents only; hidden and `subagent` entries are excluded. The catalog does
   not allow arbitrary agent text. A configured or plugin-defined primary agent
   remains available because the live engine catalog is authoritative.

2. **Reject an invalid OpenCode agent on every input path.**
   Do not rely on Flutter filtering. A direct `session.create` carrying an
   unknown, hidden, or subagent agent must fail before its first prompt with a
   stable, actionable error. Empty agent retains the OpenCode default. The
   same primary/all predicate used by `agents.list` and `session_mode` is the
   sole classification rule.

3. **The configured mcremote model wins the pre-session default.**
   When `providers.opencode.model` is non-empty, `models.list.default_ids`
   returns it even when the engine reports another default. The live catalog
   still supplies options and labels; an explicit user model continues to win
   for that session. If config is empty, retain the present live/default
   fallback behaviour.

### P2 — expose bounded user and diagnostic metadata

1. **Session rename.** Add an owner-authorized `session.rename` operation that
   updates the daemon record and, where supported, the provider-native session
   title. OpenCode implements it with `PATCH /session/{id}`. A failed native
   update leaves the daemon record unchanged. Success returns the updated
   `session` metadata; the requesting client updates or refreshes its list.
   The title is user metadata, not an agent instruction.

2. **Read-only project diagnostics.** Add a single capability-gated
   `session.diagnostics` request/result for a live session. For OpenCode it
   may return only: current branch, default branch, a bounded VCS status
   summary, and MCP server names plus connection states. It must not include
   repository file contents, patches, MCP URLs, headers, tokens, OAuth state,
   command arguments, or arbitrary engine configuration. Unsupported providers
   return `unsupported`; a stopped session returns the existing not-live error.

3. **Mobile presentation.** Put rename in the existing session action sheet.
   Put diagnostics behind an explicit, on-demand session action/detail sheet;
   do not poll it or make it a chat transcript event. Empty sections are hidden.

## Explicit exclusions

- `agent_sessions.list` for OpenCode and `/experimental/session`.
- Session share/unshare, native message edits/deletes, export/import, and raw
  transcript browsing.
- PTY, arbitrary shell, file/LSP/formatter/worktree control, patch apply.
- MCP add/remove/auth/logout, provider credentials/OAuth, config mutation,
  logs, databases, upgrades, and server lifecycle controls.
- A WebSocket or ACP transport migration. HTTP+SSE remains canonical for
  OpenCode; ACP is only a separately probed interoperability surface.

## Implementation record

- `agents.list` now includes only visible `primary`/`all` options and rejects
  custom text; `httpagent` validates every requested OpenCode agent before
  creating or resuming a daemon session and returns `bad_agent` on failure.
- The configured HTTP-provider model is applied after live/static catalog
  merging, so the picker default agrees with actual session creation.
- `session.rename` persists only after `PATCH /session/{id}` succeeds.
- `session.diagnostics` is owner-authorized, direct-response-only, and returns
  branch/default-branch, aggregate VCS counts, and capped MCP name/state rows.

## Consequences

The phone will no longer advertise actions OpenCode cannot run as a user turn,
and its displayed model default will match daemon behavior. Rename and compact,
read-only diagnostics improve session orientation without widening host control
authority. The added provider interfaces remain optional and semantic; no
OpenCode HTTP structures reach the shared protocol or Flutter model.

This decision deliberately leaves historical-session import to a future,
separately justified decision. It avoids exposing cross-project titles and
paths through an experimental upstream endpoint merely to duplicate resume.

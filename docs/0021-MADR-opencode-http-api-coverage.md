# OpenCode HTTP API coverage matrix (mcremote)

- **Status**: Living inventory
- **Date**: 2026-08-22 (last refreshed by MADR 0112 P1)
- **OpenCode verified**: **1.18.21** — the known-good release pinned by
  [MADR 0112](./0112-MADR-opencode-1.18.21-surface-parity.md) D1 and exported as
  `opencode.KnownGoodVersion`. The 1.18.0 session-tree floor
  (`opencode.MinVersion`) is unchanged and remains a separate policy: a release
  above the floor but off the pin starts normally and logs one drift warning per
  engine boot.
- **Sources**:
  - Official server docs: [opencode.ai/docs/server](https://opencode.ai/docs/server)
  - Live OpenAPI: `GET http://<engine>/doc` (OpenAPI 3.1)
  - SDK types: `@opencode-ai/sdk` `dist/gen/types.gen.d.ts` + `dist/v2/gen/types.gen.d.ts`
  - Our dialect: `internal/provider/opencode/http.go`
  - Plan: [MADR 0020](./0020-MADR-opencode-session-tree.md)

This is **not** a promise of full OpenCode parity on the phone. It classifies every
documented REST route (and the SSE event surface) so we know what we support,
what 0020 plans, and what we deliberately leave to the engine or out of scope.

> **2026-08-22 refresh (MADR 0112 P1).** Verified against 1.18.21. The primary
> OpenAPI surface is **162 paths, 188 operations, 472 schemas and 89 top-level
> Event discriminators**, set-identical to 1.18.7 in all four categories — the
> only content change between those releases is `ProviderConfig`/`Model`
> broadening interleaved-reasoning field configuration. The committed evidence
> corpus is `internal/provider/opencode/testdata/surface-1.18.21/`, validated on
> every default-tag run by `surface_contract_test.go`.
>
> Route dispositions set by 0112, beyond the table below:
>
> - `GET /skill` is **admitted** as a read despite being absent from the
>   official server route table — the skills feature is documented, the route is
>   GET-only in both the release-boundary source and the OpenAPI, and its full
>   discovery/cache/refresh lifecycle was reproduced on 1.18.21 (A15).
> - `GET /vcs/status` stays **grandfathered**: it works and Diagnostics already
>   aggregates it, but it is not in the official route table, so no new phone
>   operation is built on it.
> - `GET /file/status` and `GET /find/symbol` are documented and present in the
>   OpenAPI but their 1.18.21 handlers return a hard-coded empty array. They are
>   **excluded** until an assessed release implements them.
> - `POST /mcp` is documented but **excluded**: it mutates only instance memory,
>   can launch a local command with environment or open a remote connection with
>   headers and OAuth client secrets, and has no documented delete (A7).
> - `POST /api/session/{id}/wait` and `POST /api/session/{id}/compact` still
>   answer 503 "not available yet"; `POST /api/session/{id}/model` remains the
>   single proven v2 exception.
>
> **2026-07-26 audit note.** This inventory predates completed MADR 0020 and
> MADR 0023 work in a few cells. The code now handles session lifecycle/tree
> status, global permission reply fallback, questions, child-aware abort,
> summarize, command execution, and modes. MADR 0031 scopes the remaining
> catalog-correctness and bounded-metadata work. Native OpenCode history
> discovery through `/experimental/session` is deliberately excluded: normal
> daemon-owned sessions already resume using their persisted agent-session ID.

---

## Status legend

| Status | Meaning |
|---|---|
| **shipped** | Daemon dialect (or transport) calls / handles it today |
| **partial** | Used for a subset of fields, sids, or shapes only |
| **planned** | Explicitly in MADR 0020 (sprint called out) |
| **gap** | Useful for remote multi-agent UX; not yet planned in 0020 — needs product call |
| **engine** | Engine-internal or agent-side only; mcremote must not re-export as phone API |
| **wontfix** | Explicit non-goal for mcremote remote control plane |
| **n/a** | Not applicable to our architecture |

**Phone column**: whether the Flutter client should eventually see a first-class
control-plane mapping (`protocol-v1` event / request), not whether the agent can
use the capability inside OpenCode.

---

## 1. Summary counts (REST — primary v1 server surface)

Counts are approximate against the official **Sessions / Messages / Global /
Agents / Commands / Permission helpers** tables on the server docs plus
provider catalog we already use. TUI and experimental paths are summarized
separately.

| Bucket | ~Count | Notes |
|---|---|---|
| **shipped** | 12+ | health, SSE, session tree, todos, questions, permissions, prompt_async+agent, agents.list, queue, summarize |
| **planned (0112)** | 13 | root sessions, projects, file list/content, find text/file, skill, lsp, formatter, instance dispose, share, unshare, shell |
| **gap** | 2 | sync message POST, message-by-id |
| **engine / wontfix** | rest | vcs/pty/tui/mcp admin/oauth/config write, etc. |

**Product bar after 0020 sprints 1–5:** remote chat + tree + permissions +
questions + todos + cancel + model pick + basic agent/command/diff/fork — **not**
a full OpenCode admin/TUI client.

---

## 2. REST coverage matrix

Paths use the **primary (non-`/api`) server surface** documented for `opencode
serve` and present in SDK v1 types. V2-only aliases are noted in §4.

### 2.1 Global

| Method | Path | Status | Phone | 0020 / notes |
|---|---|---|---|---|
| `GET` | `/global/health` | **shipped** | n/a | Engine health + KD10 version pin (≥ 1.18.0 when session_tree) |
| `GET` | `/global/event` | **shipped** | n/a | Primary SSE multiplex; child alias demux + parentID bootstrap (PR1) |
| `GET` | `/global/config` | engine | n/a | v2; host config, not phone |
| `POST` | `/global/dispose` | engine | n/a | Instance lifecycle; we own process via 0019 |
| `POST` | `/global/upgrade` | wontfix | n/a | Self-upgrade of engine |

### 2.2 Sessions

| Method | Path | Status | Phone | 0020 / notes |
|---|---|---|---|---|
| `GET` | `/session` | out of scope | n/a | Daemon-owned session list is canonical; do not expose a second native-history catalog |
| `POST` | `/session` | **shipped** | via `session.create` | Create parent session |
| `GET` | `/session/status` | **shipped** | indirect | Tree-scoped idle confirmation and resync |
| `GET` | `/session/:id` | **shipped** | n/a | Resume verify |
| `DELETE` | `/session/:id` | **shipped** | via purge | Hard delete |
| `PATCH` | `/session/:id` | **shipped** | `session.rename` | Owner-authorized title rename |
| `GET` | `/session/:id/children` | **shipped** | indirect | Tree alias binding + idle confirmation |
| `GET` | `/session/:id/todo` | **shipped** (PR6) | via `plan` | Parent resync even while tree busy |
| `POST` | `/session/:id/init` | gap | optional | AGENTS.md bootstrap; product later |
| `POST` | `/session/:id/fork` | **shipped** | via `session.fork` | Sprint 5 |
| `POST` | `/session/:id/abort` | **shipped** | via cancel | Parent plus best-effort child abort cascade |
| `POST` | `/session/:id/share` | gap | optional | Share link UX |
| `DELETE` | `/session/:id/share` | gap | optional | |
| `GET` | `/session/:id/diff` | **shipped** | via `session.diff` + SSE notice | Sprint 5 |
| `POST` | `/session/:id/summarize` | **shipped** | via `/compact` | Compact/summarize |
| `POST` | `/session/:id/revert` | **shipped** | via `session.revert` | Sprint 5 |
| `POST` | `/session/:id/unrevert` | **shipped** | via `session.unrevert` | Sprint 5 |
| `POST` | `/session/:id/permissions/:permissionID` | **partial** | via `permission.respond` | Parent sid only; child origin + prefer global reply in Sprint 1 PR3 |

### 2.3 Messages

| Method | Path | Status | Phone | 0020 / notes |
|---|---|---|---|---|
| `GET` | `/session/:id/message` | **shipped** | via history/resync | 0014 resync + replay |
| `GET` | `/session/:id/message/:messageID` | gap | n/a | Rarely needed if list + SSE work |
| `POST` | `/session/:id/message` | gap | n/a | Sync wait; we use async |
| `POST` | `/session/:id/prompt_async` | **shipped** | via prompt | Core turn path; `agent` field from session.create (Sprint 3) |
| `POST` | `/session/:id/command` | **shipped** | via slash prompt | Sprint 5 |
| `POST` | `/session/:id/shell` | gap | optional | Explicit shell turn; agent tools cover many cases |

### 2.4 Permissions & questions (global helpers)

| Method | Path | Status | Phone | 0020 / notes |
|---|---|---|---|---|
| `GET` | `/permission` | **shipped** | via re-emit | Pending-list resync |
| `POST` | `/permission/:requestID/reply` | **shipped** | via respond | Preferred global reply with scoped fallback |
| `GET` | `/question` | **shipped** | via re-emit | Resync re-emits pending for tree (Sprint 1b) |
| `POST` | `/question/:requestID/reply` | **shipped** | via `question.respond` | Sprint 1b |
| `POST` | `/question/:requestID/reject` | **shipped** | via reject/timeout | Sprint 1b |

Session-scoped v2 shapes (`/session/:id/permission…`, `/session/:id/question…`)
are **planned as fallbacks** when global routes fail or are unavailable.

### 2.5 Agents & commands

| Method | Path | Status | Phone | 0020 / notes |
|---|---|---|---|---|
| `GET` | `/agent` | **shipped** | via `agents.list` | Sprint 3 picker catalog |
| `GET` | `/command` | **shipped** | via `commands.list` + available_commands | Sprint 5 |

### 2.6 Provider / config / project (catalog & host)

| Method | Path | Status | Phone | 0020 / notes |
|---|---|---|---|---|
| `GET` | `/provider` | **shipped** | via `models.list` | Live model catalog |
| `GET` | `/provider/auth` | engine | n/a | OAuth methods on host |
| `POST` | `/provider/:id/oauth/*` | engine / wontfix | n/a | Host operator concern |
| `GET` | `/config` | engine | n/a | |
| `PATCH` | `/config` | engine | n/a | Do not remote-write engine config from phone |
| `GET` | `/config/providers` | engine | n/a | Prefer `/provider` (already used) |
| `GET` | `/project` | engine | n/a | CWD/project owned by session StartOptions |
| `GET` | `/project/current` | engine | n/a | |
| `GET` | `/path` | engine | n/a | |
| `GET` | `/vcs` | **shipped** | `session.diagnostics` | Read-only branch metadata only |
| `GET` | `/vcs/status` | **shipped** | `session.diagnostics` | Bounded read-only status summary only |
| `POST` | `/instance/dispose` | engine | n/a | 0019 owns process lifecycle |

### 2.7 Files / find / tools experimental

| Method | Path | Status | Phone | 0020 / notes |
|---|---|---|---|---|
| `GET` | `/find` | engine | n/a | Agent tools |
| `GET` | `/find/file` | engine | n/a | |
| `GET` | `/find/symbol` | engine | n/a | |
| `GET` | `/file` | engine | n/a | |
| `GET` | `/file/content` | engine | n/a | |
| `GET` | `/file/status` | engine | n/a | |
| `GET` | `/experimental/tool/*` | engine | n/a | |
| `GET` | `/lsp` | engine | n/a | |
| `GET` | `/formatter` | engine | n/a | |

### 2.8 MCP / PTY / auth / log

| Method | Path | Status | Phone | 0020 / notes |
|---|---|---|---|---|
| `GET` | `/mcp` | **shipped** | `session.diagnostics` | Names and connection states only; no admin or secrets |
| `POST`/`DELETE` | `/mcp` … | engine | n/a | Host-side MCP administration remains out of scope |
| `*` | `/pty/*` | wontfix | n/a | Interactive PTY not in remote chat scope |
| `PUT` | `/auth/:id` | engine | n/a | Credential store on host |
| `POST` | `/log` | engine | n/a | |

### 2.9 TUI remote control

| Method | Path | Status | Phone | 0020 / notes |
|---|---|---|---|---|
| `*` | `/tui/*` | **wontfix** | n/a | Drives local TUI / IDE plugins; mcremote is the client |

### 2.10 Docs / alt event stream

| Method | Path | Status | Phone | 0020 / notes |
|---|---|---|---|---|
| `GET` | `/doc` | n/a | n/a | Spec browser for humans |
| `GET` | `/event` | wontfix | n/a | Per-docs alternate SSE; we standardize on `/global/event` |

---

## 3. SSE event coverage matrix

Source: SDK event type strings (v1 + v2). Handled = `HandleEvent` cases today.

### 3.1 Must-handle for remote multi-agent (0020)

| Event | Status | 0020 | Notes |
|---|---|---|---|
| `message.updated` | **shipped** | keep | Role tracking |
| `message.part.delta` | **shipped** | keep | Streaming text |
| `message.part.updated` | **shipped** | keep | Snapshots + tools |
| `message.part.removed` | gap | optional | Rare; heal on resync |
| `message.removed` | gap | optional | |
| `permission.asked` | **shipped** | keep | Dual-shape + child-aware normalization |
| `permission.replied` | **shipped** | keep | |
| `permission.updated` | **shipped** | keep | Alias / dual field shape |
| `permission.v2.asked` | **shipped** | keep | |
| `permission.v2.replied` | **shipped** | keep | |
| `session.idle` | **shipped** | keep | Tree-idle plus one-shot idle confirmation |
| `session.error` | **shipped** | keep | Child-isolated terminal handling |
| `session.created` | **shipped** | keep | Bootstrap demux + bind |
| `session.updated` | **shipped** | keep | parentID/title lifecycle tracking |
| `session.deleted` | **shipped** | keep | Unbind + complete cards |
| `session.status` | **shipped** | keep | busy/idle/retry |
| `todo.updated` | **shipped** (PR6) | → `event.TypePlan` | cancelled → pending + prefix; empty clears |
| `question.asked` | **shipped** | Sprint 1b | |
| `question.replied` | **shipped** | Sprint 1b | |
| `question.rejected` | **shipped** | Sprint 1b | |
| `question.v2.*` | **shipped** | Sprint 1b | |

### 3.2 Useful later / low priority

| Event | Status | Notes |
|---|---|---|
| `session.diff` | **shipped** | Notice (paths + +/−) + `session.diff` RPC |
| `session.compacted` | gap | Notice optional |
| `session.next.*` | gap | Experimental stream; watch if models depend on it |
| `command.executed` | **shipped** | Notice/event for executed slash command |
| `file.edited` | engine | Tool cards already surface edits |
| `file.watcher.updated` | engine | |
| `lsp.*` | engine | |
| `pty.*` | wontfix | |
| `tui.*` | wontfix | |
| `server.connected` | shipped (implicit) | Stream setup; no user event |
| `server.instance.disposed` | engine | Engine death already handled by transport |
| `installation.updated` | wontfix | |
| `vcs.branch.updated` | gap | Optional status chrome |
| `mcp.*` / `plugin.*` / `integration.*` | engine | |

---

## 4. V2 / alternate path notes

SDK **v2** adds `/api/…` prefixes and richer permission/question/session routes
(e.g. `/api/session/{id}/prompt`, `/permission/{id}/reply`,
`/api/question/request`). Runtime on 1.18.4 still exposes the **classic** paths
our dialect uses (`/session/.../prompt_async`, `/global/event`).

**Policy:**

1. Implement against **classic paths first** (what live_http tests and docs tables use).
2. Prefer **global** permission/question reply when available.
3. Add v2 path fallbacks only when live captures show classic routes missing or 404.
4. Do not dual-maintain TUI or experimental control-plane routes.

---

## 5. Mapping to mcremote protocol (phone)

| OpenCode capability | protocol-v1 / event today | After 0020 |
|---|---|---|
| Stream text / thoughts / tools | `assistant_message_chunk`, `thought_chunk`, `tool_call*` | same + child fan-in |
| Permissions | `permission_request` / `permission.respond` | child + v2 origin |
| Plan / todos | `plan` (Grok ACP + OpenCode PR6) | `todo.updated` + parent `GET …/todo` resync |
| Questions | — | `question_request` / `question.respond` (1b) |
| Turn busy | free-text error | `turn_busy` then FIFO queue |
| Modes / config | Grok ACP only | **n/a** for OpenCode HTTP unless engine gains equivalent |
| Model list | `models.list` | keep (`GET /provider`) |
| Agent pick | `agents.list` + session.create `agent` | **shipped** (Sprint 3) |
| Slash command | `commands.list` + `/name` → `POST …/command` | **shipped** |
| Fork / revert / diff | `session.fork` / `revert` / `unrevert` / `diff` | **shipped** |

---

## 6. Priority backlog (beyond current 0020 text)

Ordered by remote multi-agent value, after Sprint 1 P0 is green:

| Pri | Item | Why |
|---|---|---|
| P0 | Everything in 0020 Sprint 1 P0 | Product emergency |
| P0 | Sprint 1b questions | Blocks interactive agents |
| P1 | Sprint 2 todos + `TypePlan` IsControl | “Loses place” |
| P1 | Sprint 3 queue + agent field | **Done** (PR7b + agent/picker) |
| P2 | `/command` + command catalog | Power-user parity |
| P2 | fork / revert / diff | Undo / review |
| P3 | `POST …/message` sync (debug only) | Not needed if async solid |
| P3 | share / summarize / init / shell | Nice-to-have |
| — | TUI, PTY, file browser, OAuth admin | **wontfix** for phone |

---

## 7. How to refresh this matrix

```bash
# Engine OpenAPI (engine must be running)
curl -sS "http://127.0.0.1:<port>/doc"   # or fetch raw openapi if exposed

# SDK path dump (installed client package)
rg -o "'(/[a-zA-Z0-9_{}/.-]+)'" \
  ~/.config/opencode/node_modules/@opencode-ai/sdk/dist/gen/types.gen.d.ts

# Our calls
rg -n 'API\(ctx,|"/[a-z]' internal/provider/opencode/http.go
```

Re-run after OpenCode minor upgrades; pin min version in Sprint 4 (0020 KD10:
≥ 1.18.x).

---

## 7A. MADR 0112 stable-parity ownership matrix

Every surface admitted by [MADR 0112](./0112-MADR-opencode-1.18.21-surface-parity.md),
with the route it calls, the provider interface that carries it, the phone
operation it answers, the operator flag that gates it, the committed fixture,
and the live gate that exercises it.

| Surface | OpenCode route | Provider interface | Phone operation | Flag | Fixture | Live gate |
| --- | --- | --- | --- | --- | --- | --- |
| Known-good pin | `GET /global/health` | — | — | — | `health.json` | `TestLiveVersionAndStableSurface` |
| Native sessions | `GET /session` | `AgentSessionLister` | `agent_sessions.list` | — | `session-project-lists.json` | `TestLiveDiscovery` |
| Projects | `GET /project` | `ProjectCatalog` | `projects.list` | — | `session-project-lists.json` | `TestLiveDiscovery` |
| Model surface / variants | `GET /provider` | `picker.Option` + `ThinkingSession` | `models.list`, `/thinking` | — | `model-surface.json` | `TestLiveModelSurface` |
| Prompt attachments | `POST …/prompt_async` | `provider.Content` | `session.prompt` | — | `model-surface.json` | `TestLivePromptFileParts` |
| Transcript identity | SSE `message.*` | native ids on `event.Event` | all transcript events | — | `message-parts.json` | `TestLiveReplayIdentity` |
| Compaction | SSE `session.compacted` | — | `notice` | — | `message-parts.json` | `TestLiveCompactionReconcile` |
| Artifacts | `FilePart`, `ToolStateCompleted.attachments` | `event.Artifact` | `artifact` | — | `message-parts.json` | (covered by replay gate) |
| Detailed usage | assistant `message.updated` | `event.Usage` | `usage_update` | — | `usage-and-share.json` | (covered by replay gate) |
| Workspace | `GET /file`, `/file/content`, `/find`, `/find/file` | `WorkspaceSession` | `workspace.list/read/search` | — | `workspace-endpoints.json` | `TestLiveWorkspaceReadOnly` |
| Diagnostics | `GET /vcs`, `/vcs/status`, `/mcp`, `/skill`, `/lsp`, `/formatter` | `DiagnosticsSession` | `session.diagnostics` | — | `diagnostics-endpoints.json` | `TestLiveDiagnosticsSurface` |
| Skill refresh | `POST /instance/dispose` | `SkillRefreshSession` | `session.refresh_skills` | — | `skill-lifecycle.json` | `TestLiveSkillDiscoveryRefresh` |
| Skill authoring | *(none — ordinary prompt)* | — | `session.prompt` | — | — | deterministic only, by design |
| Share | `GET /session/{id}`, `POST`/`DELETE …/share` | `ShareSession` | `session.share_state/share/unshare` | `allow_remote_share` | `usage-and-share.json` | **not run** — external service, needs per-run consent |
| Direct shell | `POST …/shell` | `ShellSession` | `session.shell` | `allow_remote_shell` | `shell-events.json` | `TestLiveDirectShell` |

### Deliberately excluded

| Surface | Why |
| --- | --- |
| `POST /mcp` (dynamic add) | Transient, one-way, secret- and command-bearing, no documented delete — no coherent phone lifecycle (A7/A12) |
| MCP connect/disconnect/OAuth | Secret-bearing host-owned mutations, absent from the public route table |
| `GET /file/status`, `GET /find/symbol` | Return hard-coded empty arrays on 1.18.21 |
| `/experimental/*` | No stable compatibility contract |
| Broad `/api/*` v2 | Only the proven `/api/session/{id}/model` remains, as a compatibility exception |
| ACP transport | Starts one engine per subprocess, violating the single-engine invariant (0019) |

Skills, references, plugins and MCP servers remain **engine-owned
configuration**: mcremote reads their state and never writes it. Skill authoring
is agent-mediated — the phone composes an ordinary prompt and OpenCode's own
write and permission rules decide the outcome.

---

## 8. Related

- [MADR 0020](./0020-MADR-opencode-session-tree.md) — session tree + control plane plan  
- [MADR 0014](./0014-MADR-sse-reconnect-resync-decision.md) — resync gates  
- [MADR 0019](./0019-MADR-opencode-process-management-plan.md) — single engine  
- [protocol-v1.md](./protocol-v1.md) — phone wire protocol  

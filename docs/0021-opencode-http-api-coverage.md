# OpenCode HTTP API coverage matrix (mcremote)

- **Status**: Living inventory
- **Date**: 2026-07-24
- **OpenCode verified**: **1.18.4** (`~/.opencode/bin/opencode`)
- **Sources**:
  - Official server docs: [opencode.ai/docs/server](https://opencode.ai/docs/server)
  - Live OpenAPI: `GET http://<engine>/doc` (OpenAPI 3.1)
  - SDK types: `@opencode-ai/sdk` `dist/gen/types.gen.d.ts` + `dist/v2/gen/types.gen.d.ts`
  - Our dialect: `internal/provider/opencode/http.go`
  - Plan: [MADR 0020](./0020-opencode-session-tree.md)

This is **not** a promise of full OpenCode parity on the phone. It classifies every
documented REST route (and the SSE event surface) so we know what we support,
what 0020 plans, and what we deliberately leave to the engine or out of scope.

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
| **shipped** | 9 | health, SSE, session CRUD-ish, message list, prompt_async, abort, parent permission reply, provider list |
| **planned (0020)** | 12 | children, status, todos, questions, global permission, command, agent, fork/revert, diff (later) |
| **gap** | 6 | shell, summarize, init, share, sync message POST, message-by-id |
| **engine / wontfix** | rest | file/find/vcs/pty/tui/mcp admin/oauth/config write, etc. |

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
| `GET` | `/global/health` | **shipped** | n/a | Engine health + version pin (Sprint 4) |
| `GET` | `/global/event` | **shipped** | n/a | Primary SSE multiplex; child alias demux + parentID bootstrap (PR1) |
| `GET` | `/global/config` | engine | n/a | v2; host config, not phone |
| `POST` | `/global/dispose` | engine | n/a | Instance lifecycle; we own process via 0019 |
| `POST` | `/global/upgrade` | wontfix | n/a | Self-upgrade of engine |

### 2.2 Sessions

| Method | Path | Status | Phone | 0020 / notes |
|---|---|---|---|---|
| `GET` | `/session` | gap | optional | List engine sessions; we key by daemon session + agent id |
| `POST` | `/session` | **shipped** | via `session.create` | Create parent session |
| `GET` | `/session/status` | **planned** | indirect | Sprint 1: tree-scoped idle-confirm + resync (global map; filter to treeIDs) |
| `GET` | `/session/:id` | **shipped** | n/a | Resume verify |
| `DELETE` | `/session/:id` | **shipped** | via purge | Hard delete |
| `PATCH` | `/session/:id` | gap | optional | Title rename only; low value |
| `GET` | `/session/:id/children` | **planned** | indirect | Sprint 1: bind aliases + resync |
| `GET` | `/session/:id/todo` | **planned** | via `plan` | Sprint 2 |
| `POST` | `/session/:id/init` | gap | optional | AGENTS.md bootstrap; product later |
| `POST` | `/session/:id/fork` | **planned** | later | Sprint 5 / PR10 |
| `POST` | `/session/:id/abort` | **shipped** | via cancel | Parent only today; multi-node abort in Sprint 1 A7 |
| `POST` | `/session/:id/share` | gap | optional | Share link UX |
| `DELETE` | `/session/:id/share` | gap | optional | |
| `GET` | `/session/:id/diff` | **planned** | later | Sprint 5 (notice or diff event) |
| `POST` | `/session/:id/summarize` | gap | optional | Compact/summarize |
| `POST` | `/session/:id/revert` | **planned** | later | Sprint 5 |
| `POST` | `/session/:id/unrevert` | **planned** | later | Sprint 5 |
| `POST` | `/session/:id/permissions/:permissionID` | **partial** | via `permission.respond` | Parent sid only; child origin + prefer global reply in Sprint 1 PR3 |

### 2.3 Messages

| Method | Path | Status | Phone | 0020 / notes |
|---|---|---|---|---|
| `GET` | `/session/:id/message` | **shipped** | via history/resync | 0014 resync + replay |
| `GET` | `/session/:id/message/:messageID` | gap | n/a | Rarely needed if list + SSE work |
| `POST` | `/session/:id/message` | gap | n/a | Sync wait; we use async |
| `POST` | `/session/:id/prompt_async` | **shipped** | via prompt | Core turn path; `agent` field Sprint 3 |
| `POST` | `/session/:id/command` | **planned** | later | Sprint 5; slash commands |
| `POST` | `/session/:id/shell` | gap | optional | Explicit shell turn; agent tools cover many cases |

### 2.4 Permissions & questions (global helpers)

| Method | Path | Status | Phone | 0020 / notes |
|---|---|---|---|---|
| `GET` | `/permission` | **planned** | via re-emit | Sprint 1 PR5 pending list resync |
| `POST` | `/permission/:requestID/reply` | **planned** | via respond | Preferred reply path (v2 SDK); PR3 |
| `GET` | `/question` | **shipped** | via re-emit | Resync re-emits pending for tree (Sprint 1b) |
| `POST` | `/question/:requestID/reply` | **shipped** | via `question.respond` | Sprint 1b |
| `POST` | `/question/:requestID/reject` | **shipped** | via reject/timeout | Sprint 1b |

Session-scoped v2 shapes (`/session/:id/permission…`, `/session/:id/question…`)
are **planned as fallbacks** when global routes fail or are unavailable.

### 2.5 Agents & commands

| Method | Path | Status | Phone | 0020 / notes |
|---|---|---|---|---|
| `GET` | `/agent` | **planned** | picker | Sprint 3 / PR10 |
| `GET` | `/command` | **planned** | later | Catalog for slash UI; Sprint 5 |

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
| `GET` | `/vcs` | engine | n/a | Agent-side; optional later status strip |
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
| `GET`/`POST` | `/mcp` … | engine | n/a | Host-side MCP; Grok ACP has separate MCP config — not OpenCode HTTP admin |
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
| `permission.asked` | **partial** | Sprint 1 | Parent only; dual-shape + child + v2 in PR3 |
| `permission.replied` | **shipped** | keep | |
| `permission.updated` | **planned** | Sprint 1 | Alias / dual field shape (PR3) |
| `permission.v2.asked` | **planned** | Sprint 1 | |
| `permission.v2.replied` | **planned** | Sprint 1 | |
| `session.idle` | **partial** | Sprint 1 | Today: bare EndTurn; need tree-idle + idle-confirm |
| `session.error` | **partial** | Sprint 1 | Parent; child isolate (Q3) |
| `session.created` | **planned** | Sprint 1 | Bootstrap demux + bind |
| `session.updated` | **planned** | Sprint 1 | parentID / title |
| `session.deleted` | **planned** | Sprint 1 | Unbind + complete cards |
| `session.status` | **planned** | Sprint 1 | busy/idle/retry |
| `todo.updated` | **planned** | Sprint 2 | → `event.TypePlan` |
| `question.asked` | **shipped** | Sprint 1b | |
| `question.replied` | **shipped** | Sprint 1b | |
| `question.rejected` | **shipped** | Sprint 1b | |
| `question.v2.*` | **shipped** | Sprint 1b | |

### 3.2 Useful later / low priority

| Event | Status | Notes |
|---|---|---|
| `session.diff` | **planned** (Sprint 5) | Notice or future diff event |
| `session.compacted` | gap | Notice optional |
| `session.next.*` | gap | Experimental stream; watch if models depend on it |
| `command.executed` | gap | Pair with `/command` |
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
| Plan / todos | `plan` (Grok ACP only today) | OpenCode `todo.updated` |
| Questions | — | `question_request` / `question.respond` (1b) |
| Turn busy | free-text error | `turn_busy` then FIFO queue |
| Modes / config | Grok ACP only | **n/a** for OpenCode HTTP unless engine gains equivalent |
| Model list | `models.list` | keep (`GET /provider`) |
| Agent pick | — | Sprint 3 (`GET /agent` + prompt `agent`) |
| Slash command | partial via prompt text | `POST …/command` Sprint 5 |
| Fork / revert / diff | — | Sprint 5 |

---

## 6. Priority backlog (beyond current 0020 text)

Ordered by remote multi-agent value, after Sprint 1 P0 is green:

| Pri | Item | Why |
|---|---|---|
| P0 | Everything in 0020 Sprint 1 P0 | Product emergency |
| P0 | Sprint 1b questions | Blocks interactive agents |
| P1 | Sprint 2 todos + `TypePlan` IsControl | “Loses place” |
| P1 | Sprint 3 queue + agent field | Owner Q1 |
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

## 8. Related

- [MADR 0020](./0020-opencode-session-tree.md) — session tree + control plane plan  
- [MADR 0014](./0014-sse-reconnect-resync-decision.md) — resync gates  
- [MADR 0019](./0019-opencode-process-management-plan.md) — single engine  
- [protocol-v1.md](./protocol-v1.md) — phone wire protocol  

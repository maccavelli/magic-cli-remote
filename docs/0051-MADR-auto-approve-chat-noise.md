# MADR 0051: Chat transcript noise — auto-approval notices and sub-agent output

<!-- markdownlint-disable MD013 MD060 -->

- **Status**: **Implemented** (2026-07-29), commits `fcc67d7`…`c9ec886`. Part II
  re-grounded against live provider runs before implementation; several claims
  in the first draft were wrong and are corrected in §11. Post-implementation
  measurements in §14.
- **Date**: 2026-07-29
- **Deciders**: Project Owner (product surface, protocol stability); Implementer
  (daemon/providers/mobile)
- **Related**:
  - [MADR 0044](./0044-MADR-auto-approve-modes.md) — auto-approve modes,
    `Dangerous` signalling, synthetic `auto` mode
  - [MADR 0049](./0049-MADR-grok-auto-mode.md) — grok auto-mode, per-session arm
  - [MADR 0020](./0020-MADR-opencode-session-tree.md) — OpenCode child sessions,
    `childAliases`, tree-idle turn accounting
  - [MADR 0024](./0024-MADR-stream-coalescing.md) — chunkbuf; the tool-lane
    fold-away (MADR 0042 D4)
  - [protocol-v1.md](./protocol-v1.md) — event vocabulary, `TypeNotice`,
    `TypeToolCall`/`TypeToolUpdate` in-place semantics, `plan` replace semantics
- **Companion plan**:
  [0051-plan-auto-approve-chat-noise.md](./0051-plan-auto-approve-chat-noise.md)
- **Code evidence**: inspection at `9192f3a` of `internal/event/event.go`,
  `internal/chunkbuf/chunkbuf.go`, `internal/provider/{opencode,codex,acpagent,acphttp,httpagent}`,
  `apps/mobile/lib/{data/chat,features}`.
- **Runtime evidence** (this host, 2026-07-29): opencode **1.18.7**, codex
  **0.145.0**, grok **0.2.114** (`0c78503879`), goose **1.44.0**. Method per
  provider in §11; every number below was executed, not inferred.

---

## Part I — Auto-approval notices

## 1. Problem

When auto-approve is armed, every auto-approved permission produces an
individual `TypeNotice` line in the session transcript:

| Provider | Emission site | Notice text |
|---|---|---|
| OpenCode | `opencode/permission.go:241-244` | `Auto-approved: bash (git status)` |
| Codex | `codex/session.go:1321-1326` | `Auto-approved: shell (git status)` |
| ACP (grok/goose) | `acpagent/session.go:1401-1409` | *(silent — `RequestPermission` returns `autoAllow(params)` with no emit)* |

A single auto-mode turn that runs `make`, edits five files and runs two shell
commands emits **8+ individual notice lines** before any assistant output
begins. Each carries the full command or path (capped at 120 runes by
`truncateRunes`, but individually visible). The transcript becomes unreadable:
the user cannot separate what the agent did from what was approved on their
behalf.

`TypeNotice` is a control event (`event.go:125`), so these lines are *never*
dropped under back-pressure — they are guaranteed to arrive, all of them.

**This is a product bug.** Auto-approve was designed to reduce friction, not to
replace it with an audit dump. The mode chip says `auto` means "no prompts"; the
transcript says "here is every permission, one line each, forever".

## 2. Existing collapsing patterns (precedent)

### 2.1 Tool card "+N more" (`codex/items.go:178-193`)

`fileChange` collapses to `Changes[0].Path + " (+N more)"` — one card, first
item plus a count, full detail behind the card.

### 2.2 `TypeToolUpdate` in-place updates (`event.go:154-165`)

Tool cards are positioned by `TypeToolCall` and updated in place by
`TypeToolUpdate` events sharing a `ToolID`; the client upserts. `IsInPlaceUpdate`
is the contract — but see §4.4, it is a *transport* contract, not a rendering
one, and that distinction turns out to matter.

### 2.3 `TypePlan` replace semantics (`event.go:387-393`)

A `plan` event carries the full current list; one with no entries clears it. The
type *is* the replace signal — no key needed. Rendered outside the transcript in
`WorkItemsPanel`.

### 2.4 chunkbuf tool-lane fold-away (`chunkbuf.go:89-110, 147-154`)

`WithToolLane()` holds at most one pending `TypeToolUpdate` per `ToolID`, keeping
the latest and merging fields forward. Enabled only on the HTTP transport
(`httpagent/session.go:430, 1233`) — i.e. OpenCode.

None of these applies to auto-approval notices today: `TypeNotice` has no key,
no replace semantics, and no expandable list.

## 3. Design options

### Option A — accumulate in the provider, flush one notice at boundaries

Zero protocol change, but the user sees nothing until the turn ends. A long
build runs for minutes with no feedback. Rejected for the same reason chunkbuf
has a leading-edge immediate emit: "what is being approved right now" carries
first-token urgency.

### Option B — add `NoticeID` to `Event` for in-place `TypeNotice` replacement

One new field, but it changes `TypeNotice`'s contract for every consumer, and a
client that ignores `NoticeID` still appends — producing exactly the noise being
fixed. Rejected.

### Option C — new type `TypeApprovalSummary` with a stable group key

Dedicated type carrying structured approval data, upserted client-side on
`ApprovalGroupID`. Costs one entry in `Types()`, `IsControl`, the protocol spec
and the mobile reducer.

### Decision: Option C

Option C mirrors the closest existing pattern exactly: a dedicated type,
positioned by its first emission, replaced by later ones with the same key. The
vocabulary grows by one, but the change is additive — `Text` carries a
server-generated fallback line so an older client renders one system message
instead of twenty.

## 4. Design

### 4.1 Event type and payload

```go
// event.go
TypeApprovalSummary Type = "approval_summary"

// ApprovalItem is one auto-approved permission inside an approval_summary.
type ApprovalItem struct {
    ToolName string    `json:"tool_name"` // "bash", "file", "shell", ...
    Detail   string    `json:"detail"`    // "git status", "header.html", ...
    Time     time.Time `json:"time"`
}
```

On `Event`:

```go
ApprovalGroupID string         `json:"approval_group_id,omitempty"`
Approvals       []ApprovalItem `json:"approvals,omitempty"`
```

`Status` reuses the existing field with a closed two-value vocabulary for this
type: `running` while the turn is live, `completed` once it ends or the mode
leaves auto.

### 4.2 `IsControl` yes, `IsInPlaceUpdate` **no**

`TypeApprovalSummary` is added to `IsControl`: it carries replacement state, and
dropping the last one leaves a card showing a stale, short list.

It is **not** added to `IsInPlaceUpdate`, and this is the non-obvious call.
`IsInPlaceUpdate` is not a rendering hint — it is consumed in exactly two places,
both in `chunkbuf.Add`:

- `chunkbuf.go:137` — in-place updates skip the boundary path, so they do not
  drain pending text;
- `chunkbuf.go:147` — **when the tool lane is on, they are parked in
  `holdTool(ev)`, keyed by `ev.ToolID`.**

An approval summary has no `ToolID`. It would be filed under the empty-string key
and `mergeTool`d (`chunkbuf.go:279-297`) with any other keyless in-place event —
copying `ToolName`, `ToolKind`, `Status` and `Text` across two unrelated cards.
The tool lane is enabled on the HTTP transport, which is OpenCode: the provider
that raises the most auto-approvals. Adding the type to `IsInPlaceUpdate` would
corrupt both the summary and the tool card.

Treating it as an ordinary control event costs one extra text flush per
approval. Approval rate is bounded by tool executions — the same argument
`event.go:111-115` already makes for tool events being control — so the cost is
acceptable and the failure mode is eliminated rather than mitigated.

**The client-side upsert contract is carried by `ApprovalGroupID` alone**, which
is where a rendering contract belongs.

### 4.3 Provider accumulation

Each auto-approving provider keeps a per-session `[]ApprovalItem`. On each
auto-approval: append, then emit the full list with
`ApprovalGroupID = "auto-approvals"` and `Status = "running"`. On turn end or a
mode switch away from auto: emit once more with `Status = "completed"` and reset.

Two correctness constraints the first draft got wrong:

1. **The emitted slice must be cloned.** `out := s.autoApprovals` shares a
   backing array with the next `append`; the event is marshalled later, on the
   writer goroutine. Emit `slices.Clone(s.autoApprovals)`. (Same hazard as the
   shared-spec slice fixed in MADR 0049.)
2. **OpenCode's emits are concurrent.** `autoApprove`
   (`opencode/permission.go:209-266`) runs `go func()` **per permission** —
   deliberately, because a control `Emit` blocks until consumed and the caller is
   the SSE handler. Two approvals in flight can therefore emit snapshots out of
   order, and under replace semantics a stale 2-item snapshot arriving after a
   3-item one silently drops an approval for good. Snapshot **and** emit must be
   serialised under a dedicated `approvalMu` — not `o.mu`, which is held across
   `Emit` elsewhere (`emitTextCatchUp`, `http.go:1297-1324`) and must not gain a
   blocking-emit path.

   Codex has the opposite property and needs no such lock:
   `handleApprovalRequest` is reached only from `routeServerRequest`, which runs
   on the single `readPump` goroutine (`codex/conn.go:171-200`).

### 4.4 Codex's sweep cannot build items as specified

`sweepPendingApprovals` (`codex/session.go:382-414`) iterates
`s.pendingPerms`, which is `map[permID]json.RawMessage` (`session.go:67`) —
**the JSON-RPC id
only**. It has no tool name and no detail, so swept approvals cannot be turned
into `ApprovalItem`s without recording the descriptor when the permission is
first surfaced. `describeApproval` is already called there
(`session.go:1335`); its result must be stored alongside the rpc id.

Map iteration order is also random, so the swept items must be emitted in a
deterministic order (insertion order, or sorted by permission id) — otherwise
the same turn produces a differently-ordered audit list on every run.

### 4.5 Client rendering

Collapsed: `Auto-approved: bash (git status) +2 more`. Expanded: the full
chronological list with timestamps. `Text` carries `Auto-approved (3)` for
clients that do not know the type; per-item detail always comes from
`Approvals`, never from parsing `Text`.

### 4.6 Multi-provider symmetry

| Provider | Today | After |
|---|---|---|
| OpenCode | `TypeNotice` per approval | `TypeApprovalSummary`, serialised under `approvalMu` |
| Codex | `TypeNotice` per approval | `TypeApprovalSummary`, single-goroutine, descriptor recorded for sweeps |
| ACP (grok/goose) | silent | `TypeApprovalSummary` (first audit trail these get) |

## 5. Non-goals

- Not batching approvals across turns; each turn resets.
- Not touching the `slog` audit lines (`permission.go:236`, `session.go:1315`).
- Not collapsing other `TypeNotice` events (`/model`, compaction, retry).
- Not changing the non-auto path: `TypePermission` /
  `TypePermissionResolved` / `emitPermissionSheet` are untouched.
- Not touching the auto-approve *failure* notice
  (`opencode/permission.go:260-263`) — it precedes a real permission sheet and
  must stay a standalone line.

## 6. Risks and mitigations

| Risk | Severity | Mitigation |
|---|---|---|
| Old clients append instead of upserting | Medium | `Text` fallback: one scrollable line per event instead of twenty; still an improvement, never worse |
| Out-of-order snapshots drop approvals (OpenCode) | **High** | §4.3: serialise snapshot+emit under `approvalMu` |
| Slice aliasing races the marshaller | **High** | §4.3: `slices.Clone` before emit |
| Tool-lane key collision | **High** | §4.2: do not add to `IsInPlaceUpdate` |
| Unbounded list in a long auto session | Low | Cap at `maxApprovalItems = 512`, matching `maxAutoHandled` (`permission.go:139`); drop oldest |
| Mobile reducer lacks the case | Medium | Reducer lands before the providers emit |

## 7. Acceptance criteria

1. A codex session in `mode=auto` running several commands produces **one**
   approval card, not one per command.
2. Tapping the card lists the approvals in chronological order.
3. A session with no auto-approvals never emits `approval_summary`.
4. OpenCode behaves identically.
5. `protocol-v1.md` documents the type, fields and client contract.
6. `TestTypesEnumerationIsComplete` (`internal/event/types_test.go`) and
   `TestEventTypesAreDocumented` (`internal/protocol/doc_coverage_test.go`) pass.
7. Existing OpenCode/Codex auto-approve tests pass with the emitted shape updated.
8. A race-detector test drives concurrent OpenCode auto-approvals and asserts the
   final snapshot contains every approval.

## 8. Rejected alternatives

**Silently drop the notices.** The audit trail is the point: with auto armed the
user must be able to scroll back and see what was allowed.

**Client-side grouping only.** Every client would reimplement the same grouping
against provider- and latency-dependent timing. Server-side accumulation is
authoritative.

---

## Part II — Sub-agent output

## 9. Problem

Every sub-agent's output is written into the parent session's chat. A parent
turn that spawns explorers produces interleaved streams of assistant text,
reasoning and tool cards in one transcript, with nothing marking which agent
produced what. The user reads a conversation that is not the one they are having.

Sub-agent output does not need to reach the phone at all: the sub-agent reports
to the **main agent** over the engine's own channel regardless of what the daemon
forwards, and the parent's reply carries the conclusion.

### 9.1 Measured scale

Two OpenCode turns and one grok turn, each spawning exactly one sub-agent for a
trivial two-file read (`a.txt`, `b.txt`). Frames classified by the session id
each frame carries.

| Run | Parent stream | Child stream | Child share |
|---|---|---|---|
| OpenCode A (`opencode-go/kimi-k2.7-code`) | 194 `message.part.delta` / 702 B | 146 delta / 624 B | **43%** of frames, 47% of text |
| OpenCode B (same model, stronger delegation) | 81 delta / 386 B | 350 delta / 1403 B | **81%** of frames, 78% of text |
| grok 0.2.114 (`grok-4.5`, ACP) | 56 `agent_message_chunk`, 81 `agent_thought_chunk` | 63 `agent_message_chunk`, 46 `agent_thought_chunk` | **53%** of assistant text chunks |

OpenCode run B's child additionally emitted 4 `reasoning` parts (→
`thought_chunk`), 6 `tool` part updates (→ `tool_call`/`tool_call_update` for its
two `read` calls) and 10 `message.updated` frames. grok's child additionally
emitted 2 `tool_call`, 4 `tool_call_update`, 1 `user_message_chunk` (its own
prompt) and 2 `available_commands_update`.

For a two-file read. The ratio grows with the sub-agent's workload, and a turn
that spawns three explorers multiplies it.

### 9.2 Mechanism — why it leaks, per transport

The three transports differ, and only one of them filters:

| Transport | Providers | Child-frame handling | Leaks? |
|---|---|---|---|
| `httpagent` (SSE) | OpenCode | `lookupSessionLocked` (`provider.go:755-788`) resolves child sids to the **parent** session via `childAliases`; `dispatch` (`session.go:823-835`) records the origin in `eventAgentID`, exposed as `Host.EventAgentSessionID()`. The dialect consults it for *routing* only, never for content. | **Yes** — by design, then unfiltered |
| `acpagent` (stdio) | grok, goose-over-stdio | One process and one `acp.ClientSideConnection` per session (`acpagent.go:306-326`). `SessionUpdate` (`session.go:1046`) **never reads `params.SessionId`** — every notification on the connection is emitted. | **Yes** — silently, and nobody knew |
| `acphttp` (HTTP/WS) | goose | `routeNotification` (`provider.go:421-441`) does `p.sessions[notif.SessionID]` and **returns on miss**. | **No** |
| codex JSON-RPC | codex | `routeNotification` (`provider.go:427-442`) does `p.sessions[info.ThreadID]` and logs-and-drops on miss. Every codex notification carries `threadId` (52 of 72 notification schemas). | **No** |

The first draft of this MADR asserted that grok had no child-session concept and
therefore leaked nothing. That was wrong on both halves (§11.3).

For OpenCode the origin is already known at every emit site, so suppression needs
no new plumbing. A repo-wide search for a child guard before `Emit` returns
nothing.

### 9.3 OpenCode: emits reachable from a child frame

Enumerated by inspecting every `o.h.Emit` in the dialect and cross-checking
against the captured frame stream.

**Content — must be suppressed:**

| Site | Emits | Seen from a child in the capture |
|---|---|---|
| `http.go:1081` (`message.part.delta`) | assistant / thought chunk | yes, 146 and 350 frames |
| `http.go:1154` (`message.part.updated`, tool) | `tool_call` / `tool_call_update` | yes, 6 frames |
| `http.go:1323` (`emitTextCatchUp`, from `message.part.updated` text/reasoning) | assistant / thought chunk | yes |

**Session-state emits that also accept child frames — the first draft missed
both:**

| Site | Emits | Consequence |
|---|---|---|
| `todo.go:22-34` (`handleTodoUpdated`) | `TypePlan` | The code comments say it deliberately accepts *any* tree-scoped sid. A sub-agent's todo list therefore **replaces the parent's plan panel wholesale**. Neither captured run exercised it (neither child called `todowrite`), so this is code-confirmed and measurement-pending. |
| `http.go:1015-1038` → `usage.go:53-72` (`emitUsage`) | `TypeUsage` | A child `message.updated` reports the **child's** token counts as the session's context usage. 10 such frames per run were captured. |

**Must keep flowing — control and lifecycle:**

`http.go:933` (`EmitReplay`; resume replay of the *parent's own* fetched history,
outside the SSE dispatch path — `EventAgentSessionID()` is empty there and a
guard would be dead code), `http.go:1186` (`permission_resolved`),
`http.go:1255/1263/1275` (turn-complete, error, subagent-error notice),
`http.go:1366` (`session_status`), `http.go:1223-1228` (`session.idle` →
`noteNodeIdle` → `tryTreeEndTurn`), `http.go:1230-1281` (`session.error`),
`lifecycle.go:31-123` (`session.created/updated/deleted/status`),
`permission.go:106-134`, `question.go`. These keep tree-idle accounting correct
(MADR 0020); breaking them ends turns early or strands permissions.

## 10. What each provider actually reports about sub-agents

### 10.1 OpenCode — child sessions, measured

`session.created` for a child carries, verbatim from the capture:

```json
{"id": "ses_051b0561…", "parentID": "ses_051b26d6…",
 "agent": "general",
 "title": "Read a.txt and b.txt (@general subagent)",
 "permission": [{"permission": "task", "pattern": "*", "action": "deny"}]}
```

Two things the first draft missed:

- **`info.agent` exists** (`"general"`). `handleSessionLifecycle`
  (`lifecycle.go:31-69`) reads only `Info.Title`, so today the card is titled
  with the task and the agent name is discarded. `agent` → `Name`, `title` →
  `Task` is the correct mapping.
- **The parent already renders a `task` tool card** for the invocation
  (`task`, `pending`→`running`→`completed`, captured in both runs). The synthetic
  `subagent:<agentID>` card (`lifecycle.go:261-349`) is therefore a *second*
  inline representation of the same fact — which strengthens the case for moving
  it out of the transcript rather than weakening it.

`opencode agent list` reports `explore` and `general` as `subagent` kind;
`build`, `plan`, `compaction`, `summary`, `title` as `primary`.

### 10.2 grok — a first-class sub-agent surface that the daemon discards

**This is the finding that most changes the design.** grok 0.2.114 emits three
dedicated notifications on `_x.ai/session_notification`, all addressed to the
**parent** session. Captured payloads, field-for-field:

```json
{"sessionUpdate": "subagent_spawned",
 "subagent_id": "019fae56-…", "child_session_id": "019fae56-…",
 "parent_session_id": "019fae55-…", "parent_prompt_id": "f101937d-…",
 "subagent_type": "explore", "description": "Read a.txt and b.txt words",
 "role": "explore", "model": "grok-4.5",
 "capability_mode": "read-only", "effective_context_source": "new"}

{"sessionUpdate": "subagent_progress",
 "subagent_id": "019fae56-…", "duration_ms": 2498, "turn_count": 1,
 "tool_call_count": 0, "tokens_used": 1457, "tools_used": [],
 "context_window_tokens": 500000, "context_usage_pct": 0, "error_count": 0}

{"sessionUpdate": "subagent_finished",
 "subagent_id": "019fae56-…", "status": "completed",
 "tool_calls": 2, "turns": 1, "duration_ms": 4929, "tokens_used": 7231,
 "will_wake": false, "output": "## Summary…"}
```

That is a complete lifecycle with a **terminal status** — strictly richer than
what OpenCode offers. The daemon receives none of it: `grok.go:79-81` registers
handlers for exactly three extension methods (`_x.ai/models_update`,
`_x.ai/mcp/server_status`, `_x.ai/mcp_initialized`), and everything else,
including `_x.ai/session_notification`, is dropped.

grok also exposes the spawn as ordinary ACP tool calls in the parent stream —
`spawn_subagent` (with `rawInput.subagent_type`, `description`,
`capability_mode`, `background`) and `get_command_or_subagent_output`, whose
completion title is `[subagent:explore] Read a.txt and b.txt words (019fae56)`.
Those are legitimate parent tool cards and stay.

### 10.3 codex — two item types, and the daemon parses neither correctly

Ground truth from the binary's own generated schema
(`codex app-server generate-json-schema --experimental`, 609 definitions) plus
two live `codex exec --json` runs.

`SubAgentActivityThreadItem` — required fields are exactly:

```text
id, type, kind, agentThreadId, agentPath
```

`SubAgentActivityKind` ∈ `started | interacted | interrupted`.

`internal/provider/codex/items.go:201-208` parses `agentName`, `goal` and
`instructions`. **None of the three exists.** Every `subAgentActivity` card
therefore renders as the literal string `sub-agent` with an empty detail — dead
code producing an empty card, shipping today.

`CollabAgentToolCallThreadItem` — required fields:

```text
id, type, tool, status, senderThreadId, receiverThreadIds, agentsStates
```

plus optional `prompt`, `model`, `reasoningEffort`. `CollabAgentTool` ∈
`spawnAgent | sendInput | resumeAgent | wait | closeAgent`;
`CollabAgentToolCallStatus` ∈ `inProgress | completed | failed`;
`agentsStates` maps thread id → `{status, message}` with `CollabAgentStatus` ∈
`pendingInit | running | interrupted | completed | errored | shutdown | notFound`.

`items.go:194-200` parses `agentName` (does not exist) and `prompt` (does). So
the collab card is always titled `collab agent`.

Two constraints from the live runs, which matter for the panel:

- codex emitted `collab_tool_call` with `tool: "wait"`,
  `receiver_thread_ids: []` and `agents_states: {}` on **both** runs — including
  the run where nothing was spawned. Panel entries must be derived from
  `agentsStates` / `receiverThreadIds`, **never** from the item's existence, or
  every `wait` produces a phantom sub-agent.
- `SubAgentActivityKind` has **no terminal value**. This settles the question the
  plan previously left open: there is no completion signal on
  `subAgentActivity`; completion must come from `collabAgentToolCall`'s
  `agentsStates` or from turn end.

Separately, `SessionSource` (on `Thread`) has a `subAgent` variant whose
`SubAgentSource` is `review | compact | memory_consolidation | {thread_spawn:
{parent_thread_id, depth, agent_nickname, agent_role, agent_path}} | {other}`.
codex classifies its own review and compaction passes as sub-agent sessions. Any
future work that routes on thread source must filter to `thread_spawn`, or the
panel will announce "compaction" as a user sub-agent. Out of scope here — the
daemon drops non-parent threads today — but recorded so it is not rediscovered.

### 10.4 goose — subagents exist, cannot leak, report nothing over ACP

goose 1.44.0 ships a `subagent` / delegate platform extension (`summon.rs`,
`GOOSE_SUBAGENT_PROVIDER`, `GOOSE_SUBAGENT_MAX_TURNS`, session type
`sub_agent`), so sub-agents are real. But `acphttp.routeNotification`
(`provider.go:421-441`) drops any `session/update` whose `sessionId` is not a
known session, so a child's frames cannot reach a parent transcript by
construction. No sub-agent status notification was found on the ACP surface.

### 10.5 Summary

| Provider | Content leaks today | Status surface available | Terminal status |
|---|---|---|---|
| **OpenCode** | **Yes** (43–81% of streamed text, plus plan and usage) | child `session.created/updated/deleted/status` with `agent` + `title`; parent `task` tool card | via `session.idle` / `session.deleted` |
| **grok** | **Yes** (53% of assistant chunks) | `_x.ai/session_notification`: `subagent_spawned` / `_progress` / `_finished` — **richest of the four** | **yes**, `subagent_finished.status` |
| **codex** | No (`threadId` demux drops it) | `collabAgentToolCall.agentsStates`; `subAgentActivity` (name/task fields are wrong today) | via `agentsStates` only |
| **goose** | No (`sessionId` demux drops it) | none found | n/a |

## 11. Decisions

### D6 — Suppress child *content* at the provider, in both leaking transports

**OpenCode.** Add one helper on `httpSession`:

```go
func (o *httpSession) fromChild() bool {
    ev := o.h.EventAgentSessionID()
    return ev != "" && ev != o.h.AgentSessionID()
}
```

and guard exactly the three content emits in §9.3, plus the `partText`/`partType`
bookkeeping for child parts (otherwise child part ids accumulate in per-turn maps
that only `turnCleanup` clears).

**ACP stdio.** Add the sessionId check that `acphttp` and codex already have:
`SessionUpdate` compares `params.SessionId` against `s.agentID` and drops content
updates from any other session. This is a one-line origin test that fixes grok
and hardens every future stdio ACP agent. It is *not* a full child-session
implementation — grok's sub-agents remain invisible until D8 promotes their
status.

Chosen over the two alternatives:

- *Filter in the mobile reducer* — requires tagging every event with its origin
  and shipping bytes the client discards, over a phone link.
- *Do not bind child aliases at all* (OpenCode) — breaks `ConfirmTreeIdle` /
  `noteNodeIdle`, which is what stops a turn ending while a child works
  (MADR 0020). The aliases are load-bearing for turn lifecycle, independently of
  content.

Only assistant chunks, thought chunks and child tool calls are dropped. Control
and lifecycle frames keep flowing (§9.3).

### D7 — Child `todo` and `usage` are scoped to the parent

`handleTodoUpdated` ignores child sids: a sub-agent's todo list must not replace
the parent's plan panel. `emitUsage` likewise ignores child `message.updated`:
the context-window indicator reports the session the user is talking to.

This is a behaviour change with a visible upside — today a sub-agent silently
overwrites the parent's todo strip — and no downside, since the daemon has no
surface on which a child's todos or token counts would be rendered.

### D8 — Sub-agent status becomes replace-semantics state, not transcript items

Add `TypeSubagents`, carrying the full current set, with `TypePlan`'s contract
(`event.go:387-393`): entries replace, none clears.

```go
type SubagentInfo struct {
    ID     string `json:"id"`             // provider-scoped agent/session/thread id
    Name   string `json:"name"`           // OpenCode info.agent; grok subagent_type; codex agentPath basename
    Task   string `json:"task,omitempty"` // OpenCode info.title; grok description; codex prompt
    Status string `json:"status"`         // running | completed | failed
}
```

`Event.Subagents []SubagentInfo`. In `IsControl` (it carries replacement state);
**not** in `IsInPlaceUpdate`, for the reason in §4.2 — `TypePlan` is not in it
either.

Replace semantics rather than a `TypeToolCall`/`TypeToolUpdate` pair because the
panel renders a *set*, not positioned cards, and every provider knows the whole
set at each change.

Per-provider `Status` mapping, from measured vocabularies:

| Provider | running | completed | failed |
|---|---|---|---|
| OpenCode | `session.created`, `session.status=busy` | `session.idle`, `session.deleted`, turn end | `session.error` for a child sid |
| grok | `subagent_spawned`, `subagent_progress` | `subagent_finished.status == "completed"` | any other `subagent_finished.status` |
| codex | `agentsStates[id].status` ∈ `pendingInit`/`running` | `completed`/`shutdown` | `errored`/`interrupted`/`notFound` |

### D9 — Panel mirrors `WorkItemsPanel`, and clears at turn end

`SubagentsPanel` sits beside `WorkItemsPanel` above the composer
(`chat_screen.dart:2223`), driven by
`sessionTranscriptProvider(sid).select((t) => t.subagents)`, hidden when empty.
Same collapsible affordance; `running` → `Icons.autorenew`, `completed` →
`Icons.check_circle`, `failed` → error colour.

Deliberately *not* a floating snackbar: the plan panel established the
above-composer slot for ambient session state, and a transient snackbar cannot
represent something true for minutes.

**The set clears at turn end**, matching `turnCleanup`'s existing disposal of
`o.subagents` (`http.go:1336`). The record of what ran is not lost — the parent's
own `task` / `spawn_subagent` tool card stays in the transcript, which is where a
scroll-back audit belongs. A panel that accumulated completed entries across
turns would grow without bound and has no clear-affordance.

### D10 — Retire the inline `subagent:` cards only where a panel replaces them

`subagentToolPrefix` and the four card emitters (`lifecycle.go:261-349`) go away
once `TypeSubagents` is in place; likewise codex's `subAgentActivity` /
`collabAgentToolCall` tool cards. Sequencing is load-bearing: removing the cards
first makes running sub-agents invisible, which is worse than the noise.

Consumers to update, not just delete: `lifecycle_test.go:111,176`,
`live_http_test.go:843,909`, `subagent_test.go`. No mobile code matches
`subagent:` — the phone renders it as a generic tool card — so the client side is
free.

codex's dead `agentName`/`goal`/`instructions` parse is deleted with it; the
replacement reads the fields that exist (§10.3).

### D11 — goose reports nothing, and that is a fact, not a gap

No sub-agent notification exists on goose's ACP surface, and its transport cannot
leak child content. goose emits no `TypeSubagents` and the panel never appears.
Inventing entries by parsing assistant text would be wrong exactly when it
mattered. If goose later exposes delegate state, it plugs into the same event
with no protocol change.

## 12. Interaction with Part I

Both halves reduce the same surface and share the event-model work, but they are
independent and either can ship alone. The approval summary collapses many
notices into one card *in* the transcript; sub-agent handling removes content
*from* the transcript and moves status *out* of it.

Both are affected by the same `IsInPlaceUpdate` trap (§4.2), and neither type
belongs in it.

## 13. Acceptance criteria (continuing §7)

- **(9)** An OpenCode turn that spawns sub-agents shows no sub-agent assistant
  text, thoughts or tool cards in the transcript. Verified against a recorded
  frame capture, not by eye.
- **(10)** A grok turn that spawns a sub-agent likewise. This is the case that
  regressed silently before, so it gets a `live_grok` test.
- **(11)** The parent's own reply — carrying the sub-agents' conclusions — is
  unaffected in both.
- **(12)** A child's `todo.updated` does not replace the parent's plan; a child's
  `message.updated` does not move the context-usage indicator.
- **(13)** Running sub-agents appear in the panel with name and task while they
  run and leave it when they finish; absent when none run.
- **(14)** Turn lifecycle unchanged: a turn with a slow sub-agent still does not
  end early (tree-idle accounting untouched); a grok turn still ends.
- **(15)** codex `collabAgentToolCall` with empty `receiverThreadIds` and empty
  `agentsStates` produces **no** panel entry.
- **(16)** goose never emits `subagents` and is otherwise unchanged.

## 14. Measured after implementation (2026-07-29)

Every number below was executed against the installed binaries, on the daemon's
own event stream unless stated otherwise.

### 14.1 OpenCode — how much noise actually went away

Same prompt (spawn the `general` sub-agent to read two files), same model,
counted at the daemon's `Events()` channel, with the `fromChild` guard live and
again with it stubbed to `false`:

| | assistant chunks | assistant bytes | thought chunks | tool events |
|---|---|---|---|---|
| before (guard off) | 4 | 72 | 12 | 9 |
| after (guard on) | 2 | 20 | 7 | 3 |
| removed | **50%** | **72%** | **42%** | **67%** |

Consistent in direction and magnitude with the raw-SSE captures in §9.1, which
attributed 43–81% of a turn's streamed frames to the child. The parent's own
reply — and therefore the answer — is unchanged.

### 14.2 grok — the leak is closed and the status is live

`TestLiveGrokSubagentSuppressedAndPromoted` drives a real spawn and asserts the
whole contract. Observed:

```text
subagent id=019faea2-608f-… name=explore status=running   task="Read a.txt and b.txt words"
subagent id=019faea2-608f-… name=explore status=completed task="Read a.txt and b.txt words"
```

running → completed → cleared, the parent's reply still carries `hello` and
`world`, and no `<subagent_meta>` from the sub-agent's own report reaches the
transcript.

### 14.3 codex — the phantom `wait` reproduced a third time

A third `codex exec` run, prompted explicitly to use `spawnAgent`, again
produced only:

```json
{"type":"collab_tool_call","tool":"wait","receiver_thread_ids":[],
 "prompt":null,"agents_states":{},"status":"completed"}
```

So the guard in D8/§10.3 is load-bearing: seeding an entry from the item's
existence would invent a sub-agent on every one of these. `TestCollabWait
ProducesNoPhantomSubagent` pins it.

**Honest limit:** the model never chose `spawnAgent` on this host across three
attempts, so a *populated* `agentsStates` was never observed live. That mapping
is taken from the binary's own generated schema, which is authoritative for
field names and enum values but not for which path a model takes. It is
schema-derived, not observed.

### 14.4 goose — unchanged, as predicted

Full `live_goose` suite green; no `subagents` event, and `acphttp`'s sessionId
demux makes a child leak structurally impossible.

### 14.5 Gates

`make preflight` (gofmt, tidy, vet, staticcheck, `go test -race ./...`, systemd
units, release build, dart format, flutter analyze, 466 flutter tests) green.
Full `live_opencode`, `live_grok`, `live_codex` and `live_goose` suites green.

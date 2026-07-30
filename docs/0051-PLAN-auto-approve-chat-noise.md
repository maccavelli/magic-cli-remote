# MADR 0051 — Implementation plan: approval-summary cards and sub-agent suppression

<!-- markdownlint-disable MD013 MD060 -->

Companion to [MADR 0051](./0051-MADR-auto-approve-chat-noise.md). Read that
first — in particular §4.2 (why neither new type joins `IsInPlaceUpdate`),
§9.2 (which transports leak) and §10 (what each provider actually reports).

- **Status**: **Implemented** 2026-07-29, all 12 phases in order, one commit
  each (`fcc67d7`…`c9ec886`). Measurements recorded in
  [MADR §14](./0051-MADR-auto-approve-chat-noise.md). Deviations from this plan
  as written are listed in §D below.
- **Date**: 2026-07-29 (Part II re-grounded against live provider runs)
- **Line references**: verified at `9192f3a`
- **Provider versions the runtime claims were measured against**: opencode
  1.18.7, codex 0.145.0, grok 0.2.114, goose 1.44.0

---

## 0. Summary

| Layer | Change | Phase |
|---|---|---|
| `internal/event/event.go` | `TypeApprovalSummary` + `ApprovalItem`; `TypeSubagents` + `SubagentInfo`; `Types()`; `IsControl` | 0 |
| `docs/protocol-v1.md` | both types documented, incl. the `Event type values:` line | 0 |
| `internal/provider/opencode/permission.go`, `http.go`, `mode.go` | approval accumulation, serialised emit | 1 |
| `internal/provider/codex/session.go`, `mode.go` | approval accumulation; `pendingPerms` gains a descriptor | 2 |
| `internal/provider/acpagent/session.go` | first ACP approval audit trail | 3 |
| `apps/mobile` | approval card | 4 |
| `apps/mobile` | `SubagentsPanel` + reducer | 5 |
| `internal/provider/opencode/http.go`, `todo.go`, `usage.go` | drop child content; scope todo/usage to the parent | 6 |
| `internal/provider/opencode/lifecycle.go` | subagent set → `TypeSubagents`; retire inline cards | 7 |
| `internal/provider/acpagent/session.go` | `SessionUpdate` sessionId guard (fixes the grok leak) | 8 |
| `internal/provider/acpagent/*`, `internal/provider/grok/grok.go` | `_x.ai/session_notification` → `TypeSubagents` | 9 |
| `internal/provider/codex/items.go`, `session.go` | fix the dead item parse; promote to `TypeSubagents` | 10 |
| — | live verification against all four binaries | 11 |

No config changes. No mode-menu changes. Both new event types are additive.

### Dependency order

```text
Phase 0  (event model + protocol doc)
   │
   ├── Part I ────────────────────────────────────────────┐
   │   Phase 1 (opencode)  Phase 2 (codex)  Phase 3 (ACP) │
   │        └──────────────┴────────────────┘             │
   │                       ▼                              │
   │                 Phase 4 (mobile approval card)       │
   │                                                      │
   └── Part II                                            │
       Phase 5 (mobile SubagentsPanel)  ← must precede 7 and 10
           │
           ├── Phase 6 + 7  (opencode: suppress + promote — ONE commit)
           ├── Phase 8 + 9  (grok:     suppress + promote — ONE commit)
           └── Phase 10     (codex:    promote only)
                                │
                                ▼
                        Phase 11 (live verification)
```

**Two atomic pairs.** Phases 6+7 and 8+9 each suppress content and promote
status; landing either half alone makes running sub-agents invisible, which is
worse than the noise (MADR D10). Phase 5 ships the panel before any inline card
is retired, so there is never a window with no representation.

Part I and Part II are otherwise independent — either can ship alone.

### Commit protocol

One commit per phase (Phases 6+7 and 8+9 are single commits). Before every
`git add` of a Go file:

```bash
make pre-add-check FILES="<space-separated paths>"
```

Commit with `GIT_EDITOR=true git commit` — never `-m` / `-F`; the hook generates
the message. **Do not push.**

---

## Phase 0 — Event model and protocol spec

**Files:** `internal/event/event.go`, `docs/protocol-v1.md`

### 0.1 Constants

Append to the `const` block (`event.go:10-67`), after `TypeRemoteCommands`:

```go
// TypeApprovalSummary is a collapsing summary of the permissions auto-approved
// in the current turn. The daemon re-emits the full list on every approval;
// clients upsert on ApprovalGroupID rather than appending, so an auto-armed
// session shows one card per turn instead of one line per approval
// (MADR 0051 Part I).
TypeApprovalSummary Type = "approval_summary"
// TypeSubagents carries the session's currently-known sub-agents with replace
// semantics, exactly like TypePlan: an event with entries replaces the set, one
// with none clears it. Status only — sub-agent *output* never reaches the
// transcript (MADR 0051 D6/D8).
TypeSubagents Type = "subagents"
```

### 0.2 Structs

Beside `PlanEntry` (`event.go:248-254`):

```go
// ApprovalItem is one auto-approved permission inside an approval_summary
// event. Clients render these as rows in a collapsible card.
type ApprovalItem struct {
    ToolName string    `json:"tool_name"` // "bash", "file", "shell", "mcp", …
    Detail   string    `json:"detail"`    // "git status", "header.html", …
    Time     time.Time `json:"time"`      // when the approval happened
}

// SubagentInfo is one sub-agent the provider has told us about, carried on
// subagents events. Status is one of SubagentStatus*.
type SubagentInfo struct {
    // ID is provider-scoped: an OpenCode child session id, a grok subagent_id,
    // or a codex agent thread id.
    ID     string `json:"id"`
    Name   string `json:"name"`
    Task   string `json:"task,omitempty"`
    Status string `json:"status"`
}

// Sub-agent statuses carried on subagents events.
const (
    SubagentStatusRunning   = "running"
    SubagentStatusCompleted = "completed"
    SubagentStatusFailed    = "failed"
)

// Approval-summary statuses carried on approval_summary events.
const (
    ApprovalStatusRunning   = "running"
    ApprovalStatusCompleted = "completed"
)
```

### 0.3 Fields on `Event`

After `Entries` (`event.go:387-393`):

```go
// ApprovalGroupID is the stable client-side upsert key for approval_summary
// events. Events sharing a group id replace one another (MADR 0051 §4.2:
// the key carries the rendering contract; the type is NOT in IsInPlaceUpdate).
ApprovalGroupID string `json:"approval_group_id,omitempty"`

// Approvals is the full chronological list of auto-approved requests on an
// approval_summary event. Replace-semantics, like Entries on plan.
Approvals []ApprovalItem `json:"approvals,omitempty"`

// Subagents is the full current sub-agent set on subagents events. Absent
// means "clear the set", matching plan and contrasting with session_mode's
// merge rule (MADR 0046 I-1).
Subagents []SubagentInfo `json:"subagents,omitempty"`
```

### 0.4 `Types()`

Append `TypeApprovalSummary` and `TypeSubagents` to the slice (`event.go:78-102`)
in declaration order. `TestTypesEnumerationIsComplete`
(`internal/event/types_test.go:20`) parses the source and fails until this is
done.

### 0.5 `IsControl` — add both

In the case list (`event.go:117-148`), beside `TypePlan`:

```go
// Approval summaries and the sub-agent set are low-rate replace snapshots.
// Dropping the last one leaves a stale card (a short approval list) or a
// panel showing sub-agents that already finished.
TypeApprovalSummary,
TypeSubagents,
```

### 0.6 `IsInPlaceUpdate` — add **neither**

Leave `event.go:163-165` unchanged and extend its doc comment:

```go
// Only tool updates qualify. approval_summary and subagents carry their own
// client-side replace keys (ApprovalGroupID; the type itself), but they are
// NOT in-place updates for transport purposes: chunkbuf's tool lane files
// every IsInPlaceUpdate event under ev.ToolID (chunkbuf.go:147, holdTool),
// and a keyless event would collide on "" and be mergeTool'd with an
// unrelated tool card. See MADR 0051 §4.2.
```

### 0.7 Protocol spec

`docs/protocol-v1.md`:

1. Add both type names to the canonical enumeration line, which begins with the
   exact prefix:

   ```text
   Event `type` values:
   ```

   `TestEventTypesAreDocumented` (`internal/protocol/doc_coverage_test.go:43`)
   scans for the first line with that prefix and then requires each entry of
   `event.Types()` to appear on it wrapped in backticks. Nothing else in the
   spec satisfies the check.
2. Add an `approval_summary` event section: JSON example, the `ApprovalItem`
   shape, the `ApprovalGroupID` upsert contract, the `running` → `completed`
   lifecycle, and the `Text` fallback.
3. Add a `subagents` event section modelled on the `plan` section
   (`protocol-v1.md:1113-1137`), including the explicit statement that an absent
   `subagents` key means **clear**, and the same asymmetry-with-`session_mode`
   note that section carries.

### 0.8 Verify

```bash
make pre-add-check FILES="internal/event/event.go"
go test ./internal/event/ ./internal/protocol/ ./internal/chunkbuf/
```

**Acceptance:** both enumeration tests pass; chunkbuf tests unchanged (proving
0.6 kept the tool lane's behaviour identical).

---

## Phase 1 — OpenCode approval summary

**Files:** `internal/provider/opencode/http.go`, `permission.go`, `mode.go`

### 1.1 State

Add to `httpSession` (`http.go:772-819`):

```go
// approvalMu serialises the approval snapshot with its emit. Deliberately NOT
// o.mu: autoApprove runs one goroutine per permission (permission.go:209), so
// two concurrent emits could deliver snapshots out of order and a stale one
// would replace a longer list, losing approvals for good. o.mu is unsuitable
// because it is already held across non-blocking chunk emits and must not gain
// a blocking-emit path (MADR 0051 §4.3).
approvalMu   sync.Mutex
autoApprovals []event.ApprovalItem
```

### 1.2 Emit

In `permission.go`, replace the `TypeNotice` emit at `:241-244` with
`o.emitApprovalSummary(p)` and add:

```go
// maxApprovalItems bounds the per-turn audit list. Same order as
// maxAutoHandled: it only has to outlive one turn's tool executions.
const maxApprovalItems = 512

// emitApprovalSummary appends one auto-approval and re-publishes the whole
// list. The whole function runs under approvalMu so that concurrent
// auto-approve goroutines cannot deliver snapshots out of order.
func (o *httpSession) emitApprovalSummary(p permAsk) {
    o.approvalMu.Lock()
    defer o.approvalMu.Unlock()

    now := time.Now().UTC()
    o.autoApprovals = append(o.autoApprovals, event.ApprovalItem{
        ToolName: firstNonEmpty(p.Name, "permission"),
        Detail:   permissionDetail(p),
        Time:     now,
    })
    if n := len(o.autoApprovals); n > maxApprovalItems {
        o.autoApprovals = append([]event.ApprovalItem(nil), o.autoApprovals[n-maxApprovalItems:]...)
    }
    o.h.Emit(event.Event{
        Type:            event.TypeApprovalSummary,
        Timestamp:       now,
        ApprovalGroupID: approvalGroupID,
        // Clone: the event is marshalled later, on the writer goroutine, while
        // the next append may reallocate or overwrite this backing array.
        Approvals: slices.Clone(o.autoApprovals),
        Status:    event.ApprovalStatusRunning,
        Text:      approvalFallbackText(len(o.autoApprovals)),
    })
}

const approvalGroupID = "auto-approvals"

// permissionDetail is permissionSummary without the leading tool name: the
// ApprovalItem carries name and detail as separate fields.
func permissionDetail(p permAsk) string {
    pats := p.Patterns
    if len(pats) > 2 {
        pats = pats[:2]
    }
    return truncateRunes(strings.Join(pats, ", "), maxPermissionSummary)
}

// approvalFallbackText is what a client that does not know the type renders.
func approvalFallbackText(n int) string {
    return "Auto-approved (" + strconv.Itoa(n) + ")"
}
```

`permissionSummary` (`permission.go:274-285`) stays — the `slog` audit line at
`:236-240` still uses its inputs, and removing it would churn `mode_test.go`.

### 1.3 Reset and finalise

Add a helper and call it from three places:

```go
// finishApprovals publishes a terminal summary and clears the list. Called on
// turn end and on any mode switch away from auto.
func (o *httpSession) finishApprovals() {
    o.approvalMu.Lock()
    items := slices.Clone(o.autoApprovals)
    o.autoApprovals = nil
    o.approvalMu.Unlock()
    if len(items) == 0 {
        return
    }
    o.h.Emit(event.Event{
        Type:            event.TypeApprovalSummary,
        Timestamp:       time.Now().UTC(),
        ApprovalGroupID: approvalGroupID,
        Approvals:       items,
        Status:          event.ApprovalStatusCompleted,
        Text:            approvalFallbackText(len(items)),
    })
}
```

Call sites:

1. `tryTreeEndTurn` (`lifecycle.go:136-144`) — **before** the `TypeTurnComplete`
   emit, so the card is marked done ahead of the turn boundary.
2. The parent-error branch of `session.error` (`http.go:1242-1270`), beside the
   existing `turnCleanup()`.
3. `SetMode` (`mode.go:216`) whenever the resolved mode is not `autoModeID`
   (`mode.go:112`).

Do **not** call it from `turnCleanup` itself: that function holds `o.mu`
(`http.go:1330-1343`) and an `Emit` under it would introduce the blocking-emit
path 1.1 exists to avoid.

### 1.4 Tests

`internal/provider/opencode/permission_test.go`: switch the auto-approve
assertions from `TypeNotice` + `Auto-approved:` text to `TypeApprovalSummary`
with `ApprovalGroupID == "auto-approvals"` and the expected `Approvals` slice.

Add `TestAutoApprovalSummaryConcurrent`: fire N=50 `emitPermissionAsk` calls
concurrently against a fake host, wait for all goroutines, and assert the
**longest** emitted snapshot has 50 items and that snapshot lengths are
monotonically non-decreasing in emit order. Run it under `-race`. Confirm it
**fails** with `approvalMu` removed before accepting it.

### 1.5 Verify

```bash
make pre-add-check FILES="internal/provider/opencode/permission.go internal/provider/opencode/http.go internal/provider/opencode/mode.go"
go test -race ./internal/provider/opencode/
```

---

## Phase 2 — Codex approval summary

**Files:** `internal/provider/codex/session.go`, `mode.go`

### 2.1 Record the descriptor when a permission is tracked

`s.pendingPerms` is `map[string]json.RawMessage` (`session.go:67`) and holds
only the JSON-RPC id, so `sweepPendingApprovals` cannot name the tool it is
approving (MADR §4.4). Change it to:

```go
// pendingPerm is a permission awaiting the phone: the JSON-RPC id to answer,
// plus the description captured at ask time so a later sweep can put it in the
// approval audit without re-parsing params it no longer holds.
type pendingPerm struct {
    rpcID  json.RawMessage
    tool   string
    detail string
}

pendingPerms map[string]pendingPerm
pendingOrder []string // insertion order: map iteration is random and the
                      // audit list must be stable across runs
```

Populate at `session.go:1330-1335` from the `describeApproval` call already made
there. Update every `pendingPerms` reader (`grep -n pendingPerms
internal/provider/codex/`) to use `.rpcID`, and delete from `pendingOrder`
wherever an id is removed.

### 2.2 Accumulate and emit

Add to `session`:

```go
autoApprovals []event.ApprovalItem
```

No extra mutex: `handleApprovalRequest` and `sweepPendingApprovals` are reached
only from `readPump` (`conn.go:171-200`), a single goroutine. Guard the slice
with the existing `s.mu` and clone before emitting.

Replace the `TypeNotice` emit at `session.go:1321-1326` with a call to:

```go
func (s *session) noteAutoApproval(tool, detail string) {
    s.mu.Lock()
    now := time.Now().UTC()
    s.autoApprovals = append(s.autoApprovals, event.ApprovalItem{
        ToolName: firstNonEmpty(tool, "approval"),
        Detail:   truncateRunes(detail, maxApprovalSummary),
        Time:     now,
    })
    if n := len(s.autoApprovals); n > maxApprovalItems {
        s.autoApprovals = append([]event.ApprovalItem(nil), s.autoApprovals[n-maxApprovalItems:]...)
    }
    out := slices.Clone(s.autoApprovals)
    s.mu.Unlock()

    s.emit(event.Event{
        Type:            event.TypeApprovalSummary,
        SessionID:       s.localID,
        Timestamp:       now,
        ApprovalGroupID: "auto-approvals",
        Approvals:       out,
        Status:          event.ApprovalStatusRunning,
        Text:            "Auto-approved (" + strconv.Itoa(len(out)) + ")",
    })
}
```

`truncateRunes` and `maxApprovalSummary` (= 120) already exist in
`codex/mode.go:155, 175` — reuse them rather than adding a second cap.

### 2.3 Sweeps

`sweepPendingApprovals` (`session.go:382-414`) keeps its
`TypePermissionResolved` emit and additionally calls `noteAutoApproval(p.tool,
p.detail)` for each swept permission, **iterating `pendingOrder`, not the map**.
Rationale (MADR §4.4): the user armed auto mode; everything approved after that
point belongs in the audit, in a deterministic order.

### 2.4 Finalise

Emit the terminal summary from the turn-complete handler and from `SetMode` when
leaving the auto policy, symmetrical to Phase 1.3.

### 2.5 Tests

`internal/provider/codex/permission_test.go` and `mode_test.go`: update the
auto-approve assertions. Add a sweep test asserting the swept approvals appear
in insertion order with their captured tool names — this is what proves 2.1
actually plumbed the descriptor through.

### 2.6 Verify

```bash
make pre-add-check FILES="internal/provider/codex/session.go internal/provider/codex/mode.go"
go test -race ./internal/provider/codex/
```

---

## Phase 3 — ACP approval summary

**File:** `internal/provider/acpagent/session.go`

`RequestPermission` (`:1401-1409`) returns `autoAllow(params)` with no emit —
grok and goose auto-approvals leave no record at all.

1. Before returning, build an `ApprovalItem` with `ToolName` from
   `params.ToolCall.Title` (fall back to `string(params.ToolCall.Kind)`, then
   `"permission"`) and `Detail` from `summarizeToolContent(params.ToolCall.Content,
   params.ToolCall.RawInput, nil, 120)` (`session.go:1824`) — the same summariser
   the permission sheet uses below at `:1425+`, so nothing new can leak raw JSON
   into chat.
2. Accumulate and emit exactly as Phase 2 (`s.mu`-guarded, cloned).
3. Finalise on turn end and on `SetMode` away from the auto mode — note the
   synthetic auto mode from MADR 0049 lives here, so hook `armAutoMode`'s
   counterpart.

`RequestPermission` may be called concurrently by the ACP connection; keep the
snapshot and emit inside one `s.mu` critical section, or add an `approvalMu` as
in Phase 1.1 if profiling shows `s.mu` contention.

**Verify:** `make pre-add-check FILES="internal/provider/acpagent/session.go"`;
`go test -race ./internal/provider/acpagent/`.

---

## Phase 4 — Mobile: approval card

**Files:** `apps/mobile/lib/data/protocol/models.dart`,
`data/chat/transcript_reducer.dart`, `data/chat/chat_models.dart`,
`features/chat/` (card widget)

1. `ApprovalItem` model with tolerant parsing, matching `PlanEntry.fromJson`
   (`models.dart:361-372`).
2. `SessionEvent`: `approvalGroupId` from `approval_group_id`, and
   `approvals: _mapList(json['approvals'], ApprovalItem.fromJson)` beside the
   existing `remoteCommands` line (`models.dart:889`).
3. Reducer: on `approval_summary`, upsert a `ChatItem` keyed by
   `approvalGroupId` — replace in place if present, append otherwise. Mirror the
   tool-card upsert already in `applySessionEvent`'s `switch` (from
   `transcript_reducer.dart:106`).
4. Card widget: collapsed shows `first.toolName (first.detail) +N more`;
   expanded lists every item with its time. `Status == 'completed'` greys the
   activity indicator.
5. Unknown-type safety: confirm a `SessionEvent` with an unrecognised `type`
   still round-trips through `applySessionEvent` unchanged (it returns `current`)
   — this is what makes the protocol change additive for older builds.

**Verify:** `flutter test` in `apps/mobile`; widget test for collapsed/expanded
and for the upsert (two events, same group id, one card).

---

## Phase 5 — Mobile: `SubagentsPanel`

**Files:** `apps/mobile/lib/data/protocol/models.dart`,
`data/chat/transcript_reducer.dart`, `data/chat/chat_models.dart`,
`features/widgets/subagents_panel.dart` (new),
`features/chat/chat_screen.dart`

Lands **before** any daemon-side card retirement so the panel exists the moment
status starts arriving.

1. `SubagentInfo` model, tolerant parsing, `PlanEntry` style.
2. `SessionEvent.subagents` via `_mapList(json['subagents'], SubagentInfo.fromJson)`.
3. `SessionTranscript.subagents` (default `const []`) with `copyWith` support —
   mirror `plan` at `chat_models.dart:290, 342, 389, 409`.
4. Reducer case, an exact copy of the `plan` case
   (`transcript_reducer.dart:41-48`) including the identical-instance no-op and
   the absent-means-clear rule:

   ```dart
   if (ev.type == 'subagents') {
     if (_sameSubagents(current.subagents, ev.subagents)) return current;
     return current.copyWith(subagents: List<SubagentInfo>.from(ev.subagents));
   }
   ```

5. `SubagentsPanel`, a near-copy of `WorkItemsPanel`
   (`features/widgets/work_items_panel.dart`): `ExpansionTile`, leading
   `Icons.groups`, title `Sub-agents`, subtitle `N running`; per row
   `running` → `Icons.autorenew` (`tokens.running`), `completed` →
   `Icons.check_circle` (`tokens.success`), `failed` → error colour; name as
   title, task as subtitle.
6. Mount above the plan panel at `chat_screen.dart:2223`:

   ```dart
   if (subagents.isNotEmpty) SubagentsPanel(entries: subagents),
   if (plan.isNotEmpty) WorkItemsPanel(entries: plan),
   ```

   with the watch beside the existing `plan` watch (`chat_screen.dart:1804-1806`):

   ```dart
   final subagents = ref.watch(
     sessionTranscriptProvider(sid).select((t) => t.subagents),
   );
   ```

**Acceptance:** widget test — absent when empty, renders entries with the right
status icons, disappears when the set clears. Reducer test — a `subagents` event
with no entries clears the set; a byte-identical repeat returns the identical
instance (no rebuild).

---

## Phases 6 + 7 — OpenCode: suppress child content, promote status

**One commit.** Files: `internal/provider/opencode/http.go`, `todo.go`,
`usage.go`, `lifecycle.go`.

### 6.1 The origin helper

```go
// fromChild reports whether the SSE frame being handled originated in a child
// (sub-agent) session rather than this one. Child *content* never reaches the
// transcript: the sub-agent reports to the main agent over the engine's own
// channel, and the parent's reply carries the conclusion (MADR 0051 D6).
//
// Only valid inside HandleEvent: httpagent clears eventAgentID when dispatch
// returns (httpagent/session.go:823-835), so a goroutine that outlives the
// handler sees "" and must capture the answer before it starts.
func (o *httpSession) fromChild() bool {
    ev := o.h.EventAgentSessionID()
    return ev != "" && ev != o.h.AgentSessionID()
}
```

### 6.2 Guard exactly three content emits

| Site | Case | Guard placement |
|---|---|---|
| `http.go:1044-1081` | `message.part.delta` | return early **before** the `o.partText[p.PartID] += p.Delta` bookkeeping at `:1072`, so child part ids never enter the per-turn maps |
| `http.go:1114-1161` | `message.part.updated`, `part.Type == "tool"` | return before `noteTool`/`noteToolEmit` so `seenTools` and `lastToolEmit` stay parent-only |
| `http.go:1114-1116` → `emitTextCatchUp` | `message.part.updated`, `text`/`reasoning` | skip the `emitTextCatchUp` call; do not guard inside `emitTextCatchUp` itself, which is also called from `resync.go` outside dispatch where `EventAgentSessionID()` is legitimately empty |

Also skip the `o.partType[part.ID] = part.Type` write at `:1107-1109` for child
parts.

### 6.3 Scope todo and usage to the parent (MADR D7)

- `todo.go:22-34` — replace the `_ = p.SessionID` no-op with an explicit guard:
  return when `fromChild()`. Update the comment: the frame was routed here for
  tree bookkeeping, but a sub-agent's todo list is not this session's plan.
- `http.go:1029-1038` — skip `o.emitUsage(...)` when `fromChild()`. Keep the
  `o.msgRole[p.Info.ID]` write: it is keyed by message id and is what stops a
  later part being misclassified.

`resyncTodos` (`todo.go:75-86`) already fetches the **parent** session's todos
explicitly and is unaffected.

### 7.1 Track name and task, not just card state

`o.subagents` is `map[string]string` (agentID → `cardRunning`/`cardCompleted`).
Widen it:

```go
type subagentState struct {
    name   string // OpenCode info.agent, e.g. "general" / "explore"
    task   string // OpenCode info.title
    status string // event.SubagentStatus*
}

subagents map[string]subagentState
```

`handleSessionLifecycle` (`lifecycle.go:31-69`) must parse `info.agent`, which it
does not read today. Measured child `session.created` payload:

```json
{"info": {"id": "ses_…", "parentID": "ses_…", "agent": "general",
          "title": "Read a.txt and b.txt (@general subagent)"}}
```

So: `Name = firstNonEmpty(info.Agent, "subagent")`,
`Task = info.Title`.

### 7.2 Emit the set

```go
// emitSubagents publishes the whole current set (replace semantics). Callers
// must NOT hold o.mu: Emit of a control event blocks until consumed.
func (o *httpSession) emitSubagents() {
    o.mu.Lock()
    ids := make([]string, 0, len(o.subagents))
    for id := range o.subagents {
        ids = append(ids, id)
    }
    slices.Sort(ids) // map order is random; the panel must not reshuffle
    out := make([]event.SubagentInfo, 0, len(ids))
    for _, id := range ids {
        st := o.subagents[id]
        out = append(out, event.SubagentInfo{
            ID: id, Name: st.name, Task: st.task, Status: st.status,
        })
    }
    o.mu.Unlock()
    o.h.Emit(event.Event{Type: event.TypeSubagents, Subagents: out})
}
```

Call it from every point where the map changes today — `emitSubagentCard`
(`:261`), `refreshSubagentCard` (`:289`), `completeSubagentCard` (`:310`),
`completeAllSubagentCards` (`:327`) — renaming those to `noteSubagent*` since
they no longer emit cards. Keep the existing "a completed entry is never
reopened" rule (`:266-270`), which exists because OpenCode keeps sending
`session.updated` for a finished child.

Map `session.error` for a child sid (`http.go:1272-1278`) to
`SubagentStatusFailed` rather than `completed`, and keep the
`Subagent error: …` notice — a failed sub-agent is worth a transcript line.

### 7.3 Clear at turn end

`turnCleanup` (`http.go:1329-1344`) already nils `o.subagents` under `o.mu`. Emit
the empty set from the callers, **after** `turnCleanup` returns and outside the
lock: `tryTreeEndTurn` (`lifecycle.go:136-144`) and the parent branch of
`session.error` (`http.go:1242-1252`).

### 7.4 Retire the inline cards

Delete `subagentToolPrefix` (`lifecycle.go:15`) and the four
`TypeToolCall`/`TypeToolUpdate` emits. Before deleting, `grep -rn "subagent:"`
and update:

- `internal/provider/opencode/lifecycle_test.go:111, 176`
- `internal/provider/opencode/live_http_test.go:843, 909`
- `internal/provider/opencode/subagent_test.go`

`apps/mobile` has no match — the phone rendered these as generic tool cards, so
nothing client-side depends on the prefix.

The parent's own `task` tool card is untouched and remains the transcript record
of the invocation.

### 6+7 acceptance

Unit tests with a fake host asserting:

- a child-origin `message.part.delta` emits **nothing**;
- a parent-origin one still emits;
- a child-origin `todo.updated` does not emit `TypePlan`; a parent-origin one does;
- a child-origin `message.updated` with tokens does not emit `TypeUsage`;
- `session.created` for a child emits `TypeSubagents` with `Name == "general"`
  from `info.agent`;
- the set is cleared (empty `TypeSubagents`) at turn end;
- **turn lifecycle is unchanged**: reuse `subagent_test.go`'s existing
  `endViaTree` assertion to prove tree-idle accounting still ends the turn
  exactly once.

Each new test must be shown to **fail** against the pre-change code before it is
accepted — the discipline that caught the non-discriminating tests in MADR 0050.

```bash
make pre-add-check FILES="internal/provider/opencode/http.go internal/provider/opencode/todo.go internal/provider/opencode/usage.go internal/provider/opencode/lifecycle.go"
go test -race ./internal/provider/opencode/
```

---

## Phases 8 + 9 — grok: suppress child content, promote status

**One commit.** Files: `internal/provider/acpagent/session.go`,
`internal/provider/acpagent/acpagent.go`, `internal/provider/grok/grok.go`.

### 8.1 The leak

`acpagent` runs one process and one `acp.ClientSideConnection` per session
(`acpagent.go:306-326`), and `SessionUpdate` (`session.go:1046`) **never reads
`params.SessionId`**. Every notification on the connection is emitted, including
a sub-agent's. Measured against grok 0.2.114: 63 child `agent_message_chunk`
(53% of all assistant chunks), 46 child `agent_thought_chunk`, 2 child
`tool_call`, 4 child `tool_call_update`, 1 child `user_message_chunk`.

`acphttp/provider.go:433` and `codex/provider.go:434` both already do this
lookup and drop on miss. This phase brings the stdio transport in line.

### 8.2 The guard

At the top of `SessionUpdate`:

```go
// Sub-agent frames arrive on this same connection carrying the child's
// session id (grok 0.2.114 streams its subagents that way). Their content is
// not this conversation: the sub-agent reports to the main agent and the
// parent's reply carries the conclusion (MADR 0051 D6). Compare against the
// live agent id rather than dropping unknown ids outright, so a frame that
// arrives before session/new returns (agentID still "") is not lost.
if id := string(params.SessionId); id != "" && s.AgentSessionID() != "" && id != s.AgentSessionID() {
    s.log.Debug("acp: dropping child-session update",
        slog.String("frame_session_id", id))
    return nil
}
```

This is a whole-notification drop, not a per-variant one: on this transport a
foreign session id means the frame is not about this conversation at all. Its
tool calls, plan and mode updates all belong to the child.

**Confirm the guard bites** before accepting: run the Phase 11 grok capture with
and without it and diff the child-chunk count (expect 109 → 0).

### 9.1 Consume `_x.ai/session_notification`

grok's sub-agent lifecycle is a first-class extension notification addressed to
the **parent** session; the daemon drops it today because `grok.go:79-81`
registers only three `_x.ai/*` handlers. Add a fourth:

```go
"_x.ai/session_notification": acpagent.HandleXAISessionNotification,
```

The handler dispatches on `update.sessionUpdate`, ignoring every variant except
the three sub-agent ones, and ignores frames whose `sessionId` is not this
session (the same guard as 8.2 — `tool_call_delta_chunk` and `turn_completed`
arrive on this channel for the child too).

Measured payloads to parse (field names verbatim from the capture):

| Variant | Fields used | → `SubagentInfo` |
|---|---|---|
| `subagent_spawned` | `subagent_id`, `subagent_type`, `description` | `ID`, `Name`, `Task`, `Status = running` |
| `subagent_progress` | `subagent_id` | refresh `Status = running` (keeps a long-running agent from looking stale); ignore the metrics |
| `subagent_finished` | `subagent_id`, `status` | `Status = completed` when `status == "completed"`, else `failed` |

Ignore `output` on `subagent_finished` — it is the sub-agent's full report, i.e.
exactly the content this MADR removes from the transcript. The parent relays the
conclusion itself.

### 9.2 Session state and emit

Add `subagents map[string]event.SubagentInfo` plus insertion order to
`acpagent.session`, an `emitSubagents()` matching Phase 7.2 (sorted, emitted
outside the lock), and clear + emit the empty set at turn end (where
`TypeTurnComplete` is emitted).

### 9.3 Scope

Only grok registers the handler. goose (`acphttp`) is untouched, and the
`acpagent` guard in 8.2 is transport-wide but inert for any agent that does not
emit foreign session ids.

### 8+9 acceptance

- Unit test with the pipe-backed fake agent (the `startFakeAgent` harness in
  `acpagent/automode_test.go`): feed a `session/update` carrying a foreign
  session id and assert **no** event is emitted; feed one with the live id and
  assert it is.
- Unit test feeding the three captured `_x.ai/session_notification` payloads
  verbatim and asserting the resulting `TypeSubagents` sets.
- `live_grok` test: prompt a sub-agent spawn, assert zero child-origin content
  and a `subagents` set that reaches `completed`.

```bash
make pre-add-check FILES="internal/provider/acpagent/session.go internal/provider/acpagent/acpagent.go internal/provider/grok/grok.go"
go test -race ./internal/provider/acpagent/ ./internal/provider/grok/
go test -tags live_grok ./internal/provider/grok/ -count=1 -timeout 600s
```

---

## Phase 10 — codex: fix the dead parse, promote to `TypeSubagents`

**Files:** `internal/provider/codex/items.go`, `session.go`

codex cannot leak child content — `routeNotification` (`provider.go:427-442`)
exact-matches `p.sessions[threadId]` and drops the rest. Only the promotion half
applies. But the promotion cannot reuse the existing parse, because the existing
parse reads fields that do not exist.

### 10.1 Correct the item shapes

Ground truth: `codex app-server generate-json-schema --experimental`, codex
0.145.0.

`items.go:201-208` (`subAgentActivity`) parses `agentName`, `goal`,
`instructions`. The schema's required set is `id, type, kind, agentThreadId,
agentPath`; **none of the three parsed fields exists**, so the card renders
`sub-agent` with empty detail on every session shipping today. Replace with:

```go
case "subAgentActivity":
    var p struct {
        AgentThreadID string `json:"agentThreadId"`
        AgentPath     string `json:"agentPath"`
        Kind          string `json:"kind"` // started | interacted | interrupted
    }
    _ = json.Unmarshal(item, &p)
    return firstOr(agentDisplayName(p.AgentPath), "sub-agent"), p.Kind
```

where `agentDisplayName` takes the last path segment with any extension
stripped.

`items.go:194-200` (`collabAgentToolCall`) parses `agentName` (does not exist)
and `prompt` (does). Required set: `id, type, tool, status, senderThreadId,
receiverThreadIds, agentsStates`. Replace the name with the `tool` verb
(`spawnAgent`, `sendInput`, `resumeAgent`, `wait`, `closeAgent`).

### 10.2 Build the set from state, never from the item's existence

Measured twice live: codex emits `collab_tool_call` with `tool: "wait"`,
`receiver_thread_ids: []` and `agents_states: {}` **even when nothing was
spawned**. Deriving a panel entry from the item's presence would invent a
sub-agent on every `wait`.

Track `map[threadID]event.SubagentInfo` keyed by agent thread id, populated
only from:

- `collabAgentToolCall.agentsStates` — a map of thread id → `{status, message}`,
  where `CollabAgentStatus` ∈ `pendingInit | running | interrupted | completed |
  errored | shutdown | notFound`. Map to `running` for
  `pendingInit`/`running`, `completed` for `completed`/`shutdown`, `failed`
  otherwise. `Task` from the item's `prompt`.
- `subAgentActivity.agentThreadId` — `Name` from `agentPath`, `Status = running`.

### 10.3 `subAgentActivity` has no terminal state

`SubAgentActivityKind` is exactly `started | interacted | interrupted`. This
settles the question the previous draft left for the implementer to measure: an
entry seeded from `subAgentActivity` alone can only be closed by
`agentsStates` or by turn end. Clear the whole set at turn end
(`turn/completed`), matching Phase 7.3.

### 10.4 Stop rendering them as tool cards

Remove `collabAgentToolCall` and `subAgentActivity` from `itemsRenderedAsTools`
(`items.go:14-25`) and from `codexToolKindForItem` (`items.go:86-87`). Update
`item_test.go:26-27, 50`, which asserts the current mapping.

**Only after Phase 5 has shipped the panel.**

### 10.5 Verify

```bash
make pre-add-check FILES="internal/provider/codex/items.go internal/provider/codex/session.go"
go test -race ./internal/provider/codex/
```

Add a fixture test built from the captured `collab_tool_call` JSON (empty
`agents_states`) asserting **no** `TypeSubagents` entry is produced.

---

## Phase 11 — Live verification

All four binaries are installed on this host. Record the measurements into the
MADR the way §9.1 and §10 are written — **a claim about a provider's behaviour
that was not executed does not belong in these docs.**

### 11.1 OpenCode (the largest leak)

Reproduction that worked, verbatim:

```bash
opencode serve --port 34117 --hostname 127.0.0.1 &
curl -sN "http://127.0.0.1:34117/event?directory=$D" > sse.jsonl &
SID=$(curl -s -X POST "http://127.0.0.1:34117/session?directory=$D" \
  -H 'content-type: application/json' -d '{"title":"probe"}' | jq -r .id)
curl -s -X POST "http://127.0.0.1:34117/session/$SID/message?directory=$D" \
  -H 'content-type: application/json' -d '{
    "agent":"build",
    "model":{"providerID":"opencode-go","modelID":"kimi-k2.7-code"},
    "parts":[{"type":"text","text":"Use the task tool to launch the general subagent…"}]}'
```

Two traps found the hard way: the workspace default model `zen/big-pickle` does
not resolve (`Model not found`), the `opencode` provider returns
`Insufficient balance`, and `google/*` fails tool-schema validation. Only
`opencode-go/*` completed a sub-agent turn. Also note `cd X && cmd &` backgrounds
the whole `cd`, so `$PWD` in the foreground commands is *not* the probe
directory — pass an absolute `$D` everywhere or the session and the event tap
end up scoped to different directories and the capture is empty.

Classify frames by `properties.sessionID` against the child ids discovered from
`info.parentID`. Assert after the change:

- zero child-origin `message.part.delta` / `message.part.updated` reaching the
  daemon event stream;
- `subagents` events tracking the set, `Name == "general"`;
- the parent's own reply still carries the conclusion;
- the parent's `task` tool card still present;
- the turn does not end while the child is working, and does end.

### 11.2 grok

Re-run `scratchpad/probe_grok_subagent.py` (drives `grok agent --no-leader
stdio`, answers `session/request_permission`, logs every frame). Baseline
capture: 118 child-origin `session/update` frames of 267 total, plus
`subagent_spawned` / `subagent_progress` ×2 / `subagent_finished` on
`_x.ai/session_notification`.

Assert after the change: child-origin content 0; a `subagents` set that appears
on spawn and reaches `completed`.

### 11.3 codex

`codex exec --json --skip-git-repo-check -C $D --sandbox read-only "<spawn
prompt>"`. Baseline: `collab_tool_call` with `tool: "wait"` and empty
`agents_states`, and no sub-agent content in the parent stream. Assert no
phantom panel entry.

Note: on this host the model chose `wait` over `spawnAgent` on both attempts, so
a populated `agentsStates` was never observed live. The mapping in 10.2 is taken
from the binary's own generated schema, which is authoritative for field names
and enum values but not for which path the model takes. **Say so in the MADR
rather than implying it was observed.**

### 11.4 goose

Confirm the negative: no `subagents` event, session otherwise unchanged.
`acphttp.routeNotification` makes a child leak structurally impossible, so this
is a regression check, not a discovery.

### 11.5 Full gates

```bash
make preflight
make race
go test -tags live_grok ./internal/provider/grok/ -count=1 -timeout 600s
go test -tags live_opencode ./internal/provider/opencode/ -count=1 -timeout 600s
cd apps/mobile && flutter test
```

---

## G. Deferred

| Item | Reason | When |
|---|---|---|
| Sub-agent child sessions as first-class nested mcremote sessions | MADR 0028 already records this as the eventual product shape. codex 0.145.0 now exposes `parent_thread_id` via `SubAgentSource.thread_spawn`, and grok exposes `child_session_id` — so it is newly feasible, and out of scope here. | Future |
| Filtering codex `SessionSource.subAgent` by `thread_spawn` | codex classifies its own `review` / `compact` / `memory_consolidation` passes as sub-agent sessions. Irrelevant while non-parent threads are dropped; load-bearing the moment they are not. | With the above |
| Approval count badge on the mode chip | Nice-to-have, not noise-fixing | Future |
| Cross-turn approval audit view | Each turn resets; a session-wide audit would reuse the items and render differently | Future |

---

## D. Deviations from this plan, and why

Recorded so the next reader trusts the plan where it was right and knows where
it was not.

| Plan said | What shipped | Why |
|---|---|---|
| Phase 1.2: keep `permissionSummary` "so `mode_test.go` is not churned" | Deleted it | It had **zero** callers after the change, tests included — the stated reason was simply wrong, and `golint` flags unused functions. The slog audit line builds its own fields. |
| Phase 4/5: mobile `SubagentInfo`/`ApprovalItem` widgets get bespoke card code | Approval card reuses `_CompactStatusTile`, the same collapsible affordance tool and thought rows use | A new bespoke tile would have diverged in styling from every other collapsed row for no gain. `SubagentsPanel` is still its own widget, as planned, because it mirrors `WorkItemsPanel` rather than a transcript row. |
| Phases 6+7/8+9: clear the sub-agent set at every turn end | Clear only when a non-empty set was actually published (`subagentsPublished` latch) | Caught by the resync tests: the unconditional version put an empty `subagents` control event on the wire at the end of *every* turn in *every* session, to undo something that had never happened. |
| Phase 10: track codex sub-agents in `session.go` | Own file, `codex/subagents.go` (and `acpagent/subagents.go`) | `session.go` is already 1900+ lines; the sub-agent state machine is self-contained. |
| Phase 2.1: `pendingOrder` maintained ad hoc at each call site | `trackPendingLocked` / `dropPendingLocked` helpers | Five call sites mutate `pendingPerms`; keeping the slice in step by hand at each was the obvious way to leave a stale id behind. |

Two things the plan called for that turned out to matter more than expected:

- **Proving every test bites.** Six new tests were run against deliberately
  unfixed code before being accepted. Two did not discriminate on the first
  attempt and were rewritten: the codex sweep-order test passed 14 runs in 20
  with only three items (Go's map iteration coincidentally reproducing insertion
  order), fixed by widening to twelve; and the child-tool-suppression test
  passed for the wrong reason because the tool-emit dedupe swallowed the second
  frame, fixed by using distinct call ids.
- **The `IsInPlaceUpdate` decision (§0.6).** Adding either new type would have
  filed it in chunkbuf's tool lane under an empty `ToolID` and merged it with an
  unrelated tool card. Leaving both out cost nothing and the chunkbuf suite
  passed unchanged, which is the evidence that the transport behaviour is
  untouched.

## K. Resolved questions

- **Q1 — hard-coded or per-turn `ApprovalGroupID`?** Hard-coded
  `"auto-approvals"`. One card per turn, reset at turn end; per-turn keys buy
  scroll-back history that the transcript already provides via the tool cards.
- **Q2 — codex `Detail` format?** `describeApproval` (`session.go:1252-1280`)
  already produces a human-readable description; use it, truncated to 120 runes
  to match OpenCode's `maxPermissionSummary`.
- **Q3 — does codex signal sub-agent completion?** **No.**
  `SubAgentActivityKind` is `started | interacted | interrupted` with no terminal
  value (schema, codex 0.145.0). Completion comes from
  `collabAgentToolCall.agentsStates` or from turn end. *(Previously listed as an
  open question for the implementer to measure.)*
- **Q4 — does grok expose sub-agent state?** **Yes**, and it is the richest of
  the four: `subagent_spawned` / `subagent_progress` / `subagent_finished` on
  `_x.ai/session_notification`, with a terminal status. *(The first draft
  asserted the opposite.)*
- **Q5 — does the panel keep completed sub-agents after a turn?** No; it clears
  with `turnCleanup`. The parent's own `task` / `spawn_subagent` tool card is the
  scroll-back record.

# Report 0032: Codex Chat UI/UX — Noise, Correctness, and Hardening

- **Date**: 2026-07-27 (revision 2 — re-grounded against the codebase)
- **Status**: Analysis complete and verified; recommendations for review
- **Related**: [MADR 0028](./0028-MADR-codex-provider.md), [MADR 0023](./0023-MADR-canonical-slash-commands.md),
  [MADR 0024](./0024-MADR-stream-coalescing.md), [MADR 0020](./0020-MADR-opencode-session-tree.md)

---

## 0. What changed in this revision

Revision 1 was written from a partial reading. Every claim in it has now been
checked against the source, the spike captures, and — where reading was not
decisive — an executed test. Three of its findings were wrong, one was
materially mis-targeted, and the single largest source of chat noise was not
mentioned at all.

| Rev-1 claim | Verdict | Notes |
|---|---|---|
| 2.1 `turn/completed` → "Turn ended (completed)" | **Confirmed** | Wire shape verified in the spike capture; fix was incomplete (see F1) |
| 2.2 `turn_complete` + `session_status` is not noise | **Confirmed** | No change needed |
| 2.3 `turn/started` emits nothing, correctly | **Confirmed** | No change needed |
| 2.4 `thread/status/changed` suppressed deliberately | **Confirmed** | No change needed |
| 3 `turn/plan/updated` is received and discarded | **Wrong target** | That notification has never been observed. `plan` is a *thread item*. See F8 |
| 3.2 "listed in protocol-inventory.json under the 69 notification types" | **Unfounded** | The inventory contains no notification list and no such count |
| 4.2 tool-card folding "not yet implemented in the mobile UI" | **Wrong** | Fully implemented in `transcript_rows.dart`. See F10 |
| 5.1 gap matrix (`remote_commands` per provider) | **Wrong** | `remote_commands` is daemon-layer; codex already receives it. See F11 |
| 5.3 "emitting `remote_commands` would enable autocomplete" | **Wrong** | Already emitted. The real command defect is F4/F5 |
| 5.2 `session_mode` is P3, "requires a probe" | **Stale** | The probe is already in the spike: codex has Plan + Default modes. See F12 |

Net effect: the work in rev 1 was sequenced around a one-line status mapping and
a mobile-side folding project that already exists. The actual priorities are a
malformed tool-card stream, four slash commands that dead-end, and a shipped
image feature that is invisible.

---

## 1. Findings

Severity is user impact, not implementation size.

| # | Finding | Severity | Evidence |
|---|---|---|---|
| **F2** | `item/started` default arm turns non-tool items (`agentMessage`, `reasoning`, `plan`, …) into tool cards that never complete | **Critical** | `session.go:673-683` vs `685-701` |
| **F3** | Codex emits tool status `in_progress`; every other provider and the mobile client use `running` | **High** | `session.go:614,631,649,669,680,714`; `chat_models.dart:137` |
| **F4** | `/mode`, `/context`, `/compact` dead-end with "is not wired up in this build" | **High** | Proven by test; `commandtable.go:11-13` vs `commands.go:220-233` |
| **F5** | `/model` relaunches the agent and destroys the conversation despite a working in-place `SetModel` | **High** | `commandtable.go:10`; `session.go:480` |
| **F6** | `session_capabilities` never emitted → image-attach button hidden though images are fully implemented | **High** | `chat_screen.dart:1597`; `session.go:1183-1217` |
| **F1** | `turn/completed` status leaks into a system bubble; the fix must cover the whole enum and both emitters | **Medium** | `session.go:526,532`; `session.go:975-992` |
| **F7** | Internal TODO text is shown to users verbatim ("plan mode - probe required") | **Medium** | `commandtable.go:17-18`; `commands.go:178` |
| **F8** | Plan/todo arrives as a thread item, not `turn/plan/updated`; the widget exists and is unwired | **Medium** | `protocol-inventory.json`; `session.go:737` |
| **F13** | `drainChunks` drops coalesced assistant text when the event channel is full | **Medium** | `session.go:1144-1152` |
| **F9** | Conformance test's `KindDaemon` arm accepts any name; codex omitted from the alias test | **Medium** | `conformance_test.go:58,90-95` |
| **F14** | `resetStallTimer` allocates and cancels a timer on every notification, including every stream delta | **Low** | `session.go:515,1154-1176` |
| **F12** | Codex Plan mode is already probed but declared unavailable | **Low** | `summary2.json`; `0028:852` |
| **F15** | Two observed notifications are unhandled, including rate-limit updates | **Low** | `summary.json` `notif_methods` |
| **F16** | `protocol-v1.md` never specifies the `tool_call` status vocabulary — the root enabler of F3 | **Low** | `docs/protocol-v1.md:733-746` |

---

## 2. F2 — Malformed tool-card stream (Critical)

This is the dominant source of visual noise and it was missed entirely in rev 1.

### 2.1 The defect

`item/started` classifies four item types and sends everything else to a
catch-all that emits a tool card named after the raw item type:

```go
// session.go:673-683
default:
    s.emit(event.Event{
        Type:     event.TypeToolCall,
        ToolID:   p.ItemID,
        ToolName: p.ItemType,   // ← "agentMessage", "reasoning", "plan", …
        Status:   "in_progress",
    })
```

`item/completed` does the opposite — it *excludes* exactly the item types that
are not tools:

```go
// session.go:691
if p.ItemType != "agentMessage" && p.ItemType != "userMessage" && p.ItemType != "reasoning" {
    s.emit(event.Event{Type: event.TypeToolUpdate, Status: "completed", ...})
}
```

That exclusion list is itself the proof that these item types do arrive. The two
handlers disagree: `started` opens a card for them, `completed` refuses to close
it.

### 2.2 What the user sees

`_upsertTool` keys cards by `toolId` (`transcript_reducer.dart:406-432`), so
each stray item becomes a real card:

- A card literally titled **`agentMessage`** next to the assistant's actual reply.
- A card titled **`reasoning`** duplicating the thought stream already rendered
  from `item/reasoning/textDelta`.
- A card titled **`plan`** where the todo panel should be (see F8).
- All of them stuck at `in_progress` forever, because `item/completed` skips them.

The observed item-type vocabulary is 18 entries
(`protocol-inventory.json` → `thread_item_types`); the provider names 4. The
other 14 — `mcpToolCall`, `webSearch`, `imageGeneration`, `imageView`,
`contextCompaction`, `enteredReviewMode`, `exitedReviewMode`, `hookPrompt`,
`sleep`, `dynamicToolCall`, and the message/reasoning/plan types — all fall
through.

### 2.3 Fix

Replace the catch-all with an explicit split. Silence is the correct default for
an unknown item type; a debug log keeps it discoverable.

```go
// itemsRenderedAsTools are the thread item types that are genuinely agent
// actions. Everything else is either its own event stream (agentMessage,
// reasoning), a different UI surface (plan), or not worth a card. A catch-all
// here opens cards that item/completed refuses to close.
var itemsRenderedAsTools = map[string]struct{}{
    "commandExecution": {}, "fileChange": {}, "mcpToolCall": {},
    "webSearch": {}, "dynamicToolCall": {}, "imageGeneration": {},
    "imageView": {}, "collabAgentToolCall": {}, "subAgentActivity": {},
    "hookPrompt": {},
}
```

- `item/started`: emit a tool card only for members of that set; give
  `mcpToolCall`/`webSearch`/`dynamicToolCall` a real `ToolKind` (`other`,
  `fetch`, `other`) so `classifyTool` groups them correctly.
- `item/completed`: gate on the **same** set instead of the current
  three-name deny-list, so the two handlers can never drift again.
- `contextCompaction`, `enteredReviewMode`, `exitedReviewMode`: emit
  `TypeNotice` — they are real state changes worth one line, not tool cards.
- `plan`: route to F8.

### 2.4 Test

There is currently **no fixture covering `item/started` at all** — the whole
default arm is untested. Add a table test over every one of the 18 item types
asserting: (a) which emit `TypeToolCall`, (b) that every type that opens a card
also closes it on `item/completed`, and (c) that no card is left `in_progress`
after a synthetic turn.

---

## 3. F3 — Tool status vocabulary mismatch (High)

Codex is the only provider that emits `in_progress` as a **tool** status. It is
otherwise the daemon's **plan-entry** vocabulary (`event.go:122`,
`protocol-v1.md:820`).

The mobile client's contract:

```dart
// chat_models.dart:137-138
bool get toolRunning => toolStatus == 'running' || toolStatus == 'pending';
bool get toolFailed  => toolStatus == 'failed'  || toolStatus == 'error';
```

OpenCode emits `running` / `completed` (`lifecycle.go:301,323`); the ACP
providers pass the ACP status through. No normalization exists anywhere in the
daemon or the Dart parsing layer — verified by a repo-wide search for
`in_progress`.

**Consequences**

1. Codex tool cards never show a running spinner.
2. Worse, they are immediately *groupable*: `buildTranscriptRows` groups on
   `!item.toolRunning` (`transcript_rows.dart:67`), so a command that is still
   executing gets folded into "Ran 2 commands" and the count then mutates as
   more arrive. That is visible churn in the transcript.

**Fix**: emit `running` at all six sites. One-word change, no client work.

**Guard**: `protocol-v1.md` documents `tool_kind` but never the `status` values
(F16). Specify `pending | running | completed | failed` there, then add a
cross-provider test asserting every provider's tool events use only those.

---

## 4. F4/F5/F7 — The codex command table is wrong in four ways (High)

### 4.1 Dead-ends (F4)

`available()` returns `true` unconditionally for `KindDaemon`
(`command.go:230-231`), so a `KindDaemon` entry is always advertised as working.
But `runCanonical`'s `KindDaemon` arm only dispatches five names — `help`,
`model`, `clear`, `new`, `sessions` (`commands.go:220-233`). Anything else falls
to:

```go
m.emitNotice(id, fmt.Sprintf("“/%s” is not wired up in this build.", res.Spec.Name))
```

Codex declares `mode`, `context`, and `compact` as `KindDaemon`. All three are
advertised in `remote_commands`, listed by `/help` under "You can run:", offered
by composer autocomplete — and then fail.

Proven by executing `runCanonical` over each provider's `KindDaemon` entries:

```
DEAD-END: codex /compact (KindDaemon, handled=true) -> "“/compact” is not wired up in this build."
DEAD-END: codex /mode    (KindDaemon, handled=true) -> "“/mode” is not wired up in this build."
DEAD-END: codex /context (KindDaemon, handled=true) -> "“/context” is not wired up in this build."
DEAD-END: goose /mode    (KindDaemon, handled=true) -> "“/mode” is not wired up in this build."
```

Note the fourth line: **goose has the same bug** (`goose/commandtable.go:8`).

### 4.2 `/model` destroys the conversation (F5)

Codex implements `SetModel` (`session.go:480`), so it satisfies
`provider.ModelSession` — the in-place switch that keeps context. But the table
declares `"model": {Kind: KindDaemon}`, which routes to
`cmdModel(..., inPlace=false)` → `relaunch()` → a destroy-and-recreate that
throws the conversation away (`commands.go:225-226, 654-675`).

OpenCode gets this right: `{Kind: KindOp, Op: OpSetModel}` → `inPlace=true`.

`SetModel` genuinely works despite only mutating local state, because codex's
model is **per-turn**: `beginTurn` reads `s.opts.Model` and sends it as
`params["model"]` on `turn/start` (`session.go:310-317`). The switch takes
effect from the next turn with the thread intact.

One gap the fix must close: unlike opencode's `SetModel`, which validates and
calls the engine synchronously (`session_ops.go:175-192`), codex's accepts any
string. A typo would be reported as "Model is now …" and then fail at the next
`turn/start`. Routing `/model` to `KindOp` requires adding validation against
`model/list` (already implemented as `Provider.ListModels`, `provider.go:73`).

This is the most damaging item in the report: a user typing `/model gpt-5.6-luna`
silently loses their session context.

### 4.3 Corrected table

Codex implements `Compact` (`session.go:457`) and emits `TypeUsage` from
`thread/tokenUsage/updated` (`session.go:718-734`), so `OpCompact` and
`OpContext` both resolve as available.

```go
var commandTable = command.Table{
    "help":     {Kind: command.KindDaemon},
    "new":      {Kind: command.KindDaemon},
    "sessions": {Kind: command.KindDaemon},
    "clear":    {Kind: command.KindDaemon},

    // thread/compact/start — verified OK in the 0.145.0 spike.
    "compact": {Kind: command.KindOp, Op: command.OpCompact},
    // In-place switch keeps the thread; KindDaemon would relaunch and lose it.
    "model": {Kind: command.KindOp, Op: command.OpSetModel},
    // thread/tokenUsage/updated feeds lastUsage, which gates OpContext.
    "context": {Kind: command.KindOp, Op: command.OpContext},

    // Codex exposes Plan and Default collaboration modes; until the set path is
    // wired these stay unavailable with a reason a user can act on.
    "mode": {Kind: command.KindNone,
        Note: "codex mode switching isn't wired up yet — start a new session to change mode"},
    "plan": {Kind: command.KindNone,
        Note: "codex plan mode isn't wired up yet — ask the agent to plan before it edits"},
    "goal": {Kind: command.KindNone,
        Note: "codex goals aren't exposed over the app-server protocol"},
    "diff": {Kind: command.KindNone,
        Note: "codex exposes no diff over the app-server — ask the agent to show its changes"},
    "undo": {Kind: command.KindNone,
        Note: "codex can't undo a turn remotely — ask the agent to revert its changes"},
    "redo": {Kind: command.KindNone,
        Note: "codex can't redo a turn remotely"},
}
```

The rewritten notes fix **F7**: `Resolution.Reason()` returns `Mapping.Note`
verbatim into user-facing copy (`command.go:143-151`, `commands.go:178`), so
"plan mode - probe required" and "not supported" were being shown to users. Every
other provider writes these as sentences that tell the user what to do instead.

### 4.4 Guards (F9)

The conformance test cannot catch any of this — its `KindDaemon` case is an
empty accept-all (`conformance_test.go:58`). Two additions:

1. **Dispatch conformance**: assert every `KindDaemon` name a provider declares
   has a dispatch arm in `runCanonical`. The proof test above is the shape;
   promote it to a permanent regression test. This closes goose's `/mode` too.
2. **Capability conformance**: assert that when a provider's session type
   implements `CompactSession`/`ModelSession`/`DiffSession`/`UndoSession`/
   `RevertSession`, the table routes the corresponding command through `KindOp`
   rather than shadowing it with `KindDaemon`. This is what would have caught F5.

Also add `codex` to `TestTablesAreKeyedByCanonicalName` — it is the only
registered provider missing from that list (`conformance_test.go:90-95`).

---

## 5. F6 — Image attach is hidden though images work (High)

The mobile composer gates its image button on a negotiated capability:

```dart
// chat_screen.dart:1597
).select((t) => t.capabilities?.image ?? false),
```

Only `acpagent` and `acphttp` emit `TypeSessionCapabilities`. Codex never does,
so the fallback `false` applies and the button never appears.

Meanwhile the codex provider implements image input completely — MIME handling,
a 10 MB cap, and attachment metadata (`session.go:26,1183-1217`) — and the spike
confirms **all six** codex models advertise `inputModalities: [text, image]`
(`0028:840`).

A shipped, tested feature is unreachable from the UI.

**Fix**: emit `session_capabilities` at session create with `image: true`. Derive
the rest from what the provider actually implements rather than hardcoding:
`load_session` from `thread/resume` (verified OK in the spike), `compact` from
`thread/compact/start` (verified OK), `fork` from `thread/fork` (verified OK).
Do not claim `audio` — codex advertises no audio modality.

Because `TypeSessionCapabilities` is a control event (`event.go:100-101`) it is
delivered on the blocking path and cannot be dropped, so a single emit at create
is sufficient.

---

## 6. F1 — Turn-completion status (Medium, revised)

Rev 1's diagnosis is correct and the wire shape is now confirmed from the spike
capture rather than inferred:

```json
{ "method": "turn/completed",
  "params": { "threadId": "…",
    "turn": { "id": "…", "status": "completed", "error": null,
              "startedAt": …, "completedAt": …, "durationMs": 1096 } } }
```

`params.turn.status` matches the provider's unmarshal struct
(`session.go:519-524`), so `StopReason: "completed"` reaches
`transcript_reducer.dart:296` and prints "Turn ended (completed)".

Rev 1's fix was incomplete in three ways.

**1. The status enum has four values, not one.** `protocol-inventory.json` →
`turn_status`: `completed`, `interrupted`, `failed`, `inProgress`. Mapping only
`completed` leaves "Turn ended (interrupted)" and "Turn ended (failed)" —
the second of which reports a failure as a neutral end-of-turn.

**2. There are two emitters.** `handleNotification` builds the events inline
(`session.go:528-542`) and `emitTurnComplete` builds them again
(`session.go:975-992`, called from the cancel and error paths at `330-332`). A
fix applied to one site misses the other. They should be one function.

**3. `turn.error` is available and unused.** A failed turn carries a reason that
currently goes nowhere.

**Fix** — normalize once, in `emitTurnComplete`, and route `turn/completed`
through it:

```go
// codexStopReason maps codex's turn_status enum onto the daemon's stop-reason
// vocabulary. The mobile reducer prints a system bubble for anything it does
// not recognise, so an unmapped value becomes visible noise.
func codexStopReason(status string) string {
    switch status {
    case "completed":
        return "end_turn"      // reducer treats this as a silent no-op
    case "interrupted":
        return "cancelled"     // reducer's cancel path: "Turn cancelled"
    case "failed":
        return "error"
    default:
        return status
    }
}
```

For `failed`, also emit a `TypeError` carrying `turn.error` so the failure is
reported rather than swallowed.

**Test**: a table test over all four enum values asserting the emitted
`StopReason`, plus a reducer test asserting `end_turn` appends no `ChatItem`.

Rev 1's sections 2.2, 2.3 and 2.4 are confirmed correct — no action.

---

## 7. F8 — Plan/todo: right goal, wrong mechanism (Medium)

The widget already exists and is provider-agnostic:
`WorkItemsPanel` (`work_items_panel.dart`) renders progress, per-entry status
icons, and the active item; the reducer stores `SessionTranscript.plan`. Nothing
mobile-side is needed.

But rev 1's target is wrong. `turn/plan/updated` **has never been observed**.
The spike's `notif_methods` — the actual notifications captured from a live
0.145.0 engine — is nine entries:

```
account/rateLimits/updated      item/agentMessage/delta
item/completed                  item/started
mcpServer/startupStatus/updated thread/status/changed
thread/tokenUsage/updated       turn/completed
turn/started
```

No plan notification. And rev 1's citation — "listed in the Codex v2 schema
(inventoried in protocol-inventory.json under the 69 notification types)" — does
not hold: that file has no notification list and no such count. The `case
"turn/plan/updated"` arm in `session.go:737` is dead code for a method that may
not exist.

What *is* real: `plan` is one of the 18 `thread_item_types`. So the plan arrives
as an **item** — `item/started` / `item/completed` with `itemType: "plan"` —
which today lands in the F2 catch-all and renders as a stuck tool card named
"plan".

**Implementation**

1. Probe first. The item payload shape is genuinely unknown; do not guess it.
   Run a turn that produces a plan against the pinned 0.145.0 engine and capture
   the `item/started` and any update payload for `itemType: "plan"`. The engine
   is installed locally (`~/.local/bin/codex`, 0.145.0), so this is a short
   scripted probe, not a blocked task.
2. Add a `case "plan"` to `item/started` (and to whatever update notification the
   probe reveals) that emits `event.TypePlan`.
3. Normalize to the daemon's fixed vocabulary — statuses
   `pending|in_progress|completed`, priorities `high|medium|low`
   (`event.go:119-131`) — mirroring `acpcommon/plan.go:7-20` and
   `opencode/todo.go:20-28`, both of which coerce unknown values to
   `pending`/`medium` rather than passing them through.
4. Clear path: an empty plan must emit `TypePlan` with an empty **non-nil**
   slice, matching opencode.
5. Delete the `turn/plan/updated` arm, or keep it wired to the same handler with
   a comment recording that it was never observed on 0.145.0.

Until the probe lands, F2's fix should route `itemType: "plan"` to silence — a
missing panel is better than a stuck card labelled "plan".

---

## 8. F10/F11 — Corrections to rev 1's sections 4 and 5

### 8.1 Tool-card folding is already implemented (F10)

Rev 1: *"The classification is wired through but the actual folding/collapsing is
not yet implemented in the mobile UI"* — and then scheduled 4-6 hours for it.

It is implemented. `buildTranscriptRows` (`transcript_rows.dart:47-83`) folds
consecutive finished tool items of the same `ToolClass` into a `GroupRow`,
`GroupRow.title` produces exactly the strings rev 1 quoted as aspirational
("Ran 5 commands", "Edited 3 files", "Used 4 tools"), and it is wired into
`transcript_pane.dart:79,193` with a memo that skips the O(n) fold when only the
last item changed. Running tools deliberately stay unfolded so their spinner
remains visible.

So rev 1's "Phase 4, 4-6 hours, cross-provider" is already done. What actually
degrades codex grouping is **F3**: `in_progress` defeats the `!toolRunning`
predicate, so codex tools group *too eagerly* rather than not at all. Fixing F3
fixes codex grouping. Rev 1's Option A (provider-side summary notices) should be
dropped — it would double up on a mechanism that already works.

### 8.2 `remote_commands` is daemon-layer (F11)

Rev 1's matrix lists `remote_commands` and `available_commands` as emitted by
opencode/grok/goose but not codex, and proposes emitting it from codex at
session create.

`remote_commands` has exactly one emitter in the entire repo:
`Manager.advertiseCommands` (`internal/session/commands.go:86-123`). It is
provider-independent — it resolves the canonical vocabulary from whatever
`command.Tabler` the provider supplies. Codex implements `CommandTable()`
(`provider.go:70`), so **codex already emits `remote_commands` and already has
composer autocomplete**. There is nothing to add.

That is precisely why F4 is severe: the mechanism works, and it is faithfully
advertising three commands that dead-end.

Corrected matrix (emitters only; `remote_commands` is daemon-wide):

| Event | Codex | OpenCode | ACP (grok/goose) |
|---|---|---|---|
| `remote_commands` | **Yes** (daemon) | Yes (daemon) | Yes (daemon) |
| `available_commands` | No — codex advertises none | Yes | Yes |
| `session_capabilities` | **No** → F6 | No | Yes |
| `session_title` | No | No | acphttp only |
| `session_mode` | No → F12 | No | No |
| `session_config` | No | No | Yes |
| `plan` | **No** → F8 | Yes | Yes |
| `usage_update` | **Yes** | Yes | Yes |

Two rev-1 claims corrected here: opencode does not emit `session_capabilities`
or `session_title`, and no provider emits `session_mode`.

---

## 9. Stability and performance

### 9.1 F13 — Coalesced assistant text can be silently dropped

`drainChunks` sends with the non-blocking `trySend` and **discards the result**:

```go
// session.go:1144-1152
func (s *session) drainChunks() {
    s.emitMu.Lock()
    ev, ok := s.chunkBuffer().Drain()
    if ok {
        s.trySend(ev)          // ← return value ignored
    }
    s.emitMu.Unlock()
    s.stopFlush()
}
```

`Drain()` clears the buffer (`chunkbuf.go:142-148`). The two other senders both
recover from a failed send by calling `Unflush` and re-arming
(`session.go:1068-1072`, `1133-1136`); this one does not. If the 256-slot event
channel (`session.go:79`) is full, the buffered text is gone.

The call sites are the worst possible ones: immediately before `turn_complete`
(`session.go:527`, `976`) and on close (`392`). Because `TypeTurnComplete` is a
control event it uses the *blocking* path — so under backpressure the turn ends
correctly while the final assistant text vanishes. The user sees a truncated
reply and no error.

**Fix**: mirror `onFlushTimer` — on send failure, `Unflush` and re-arm. On the
close path, where retrying is pointless, log the dropped byte count that
`Unflush` returns.

**Note**: `httpagent/session.go:1264-1272` has the identical pattern. Fix both,
or lift the drain into a shared helper.

### 9.2 F14 — Timer churn on every stream delta

`handleNotification` calls `resetStallTimer()` unconditionally at the top
(`session.go:515`), and each call takes `s.mu`, stops the existing timer, and
allocates a new `time.AfterFunc` (`session.go:1154-1176`). Every
`item/agentMessage/delta` and `item/reasoning/textDelta` — hundreds to thousands
per turn — pays a timer allocation plus two runtime timer-heap operations under
the session mutex.

This is codex-specific; no other provider has a stall timer.

**Fix**: keep a `lastActivity atomic.Int64` updated on every notification (a
single atomic store, no lock) and run **one** periodic ticker for the session
that emits the stall notice when `now - lastActivity > stallNotice` and the turn
is still busy. Constant cost per notification, one timer per session.

### 9.3 Tool output back-pressure

`item/commandExecution/outputDelta` emits one `TypeToolUpdate` per delta
(`session.go:702-717`), and `TypeToolUpdate` is a **control** event
(`event.go:92`) — so every fragment of command output takes the blocking channel
send. Chunk coalescing does not apply: `IsChunk` covers only assistant and
thought chunks (`chunkbuf.go:81-83`).

A command with large output (a build, a test run) therefore drives one blocking
send per delta through the whole daemon path, and stalling there back-pressures
the JSON-RPC reader, delaying control events and potentially tripping the stall
notice.

This is a design question rather than a defect, so flagging rather than
prescribing: either coalesce tool-output deltas on a short window (the same
treatment MADR 0024 gave assistant text), or reclassify a *text-carrying*
`tool_call_update` as droppable while keeping status transitions as control
events. The second is smaller but splits the control classification by payload,
which needs a protocol note.

### 9.4 F15 — Unhandled observed notifications

Both fall through to `default: s.log.Debug(...)` (`session.go:739-740`):

- **`account/rateLimits/updated`** — the daemon already has a rate-limit UI
  contract (`error_kind: rate_limit`, `retry_at`, rendered as an actionable limit
  card, `protocol-v1.md:737-744`). Codex is pushing exactly that data and it is
  being discarded. Wiring it would let the phone show a real reset time instead
  of a generic failure.
- **`mcpServer/startupStatus/updated`** — an MCP server failing to start is
  currently invisible; the user sees tools mysteriously absent. One
  `TypeNotice` on failure would do.

---

## 10. Recommended order

Sequenced by user impact per unit of risk. Estimates assume tests.

### Phase 1 — Stop the bleeding (~3-4 h)

1. **F5** — `"model": {Kind: KindOp, Op: OpSetModel}`. One line; stops silent
   conversation loss.
2. **F4/F7** — replace the codex command table (§4.3); fix goose's `/mode`.
3. **F2** — explicit item-type allowlist in `item/started`, shared with
   `item/completed`; route `plan` to silence pending F8.
4. **F3** — `in_progress` → `running` at all six sites.

### Phase 2 — Restore hidden functionality (~2-3 h)

5. **F6** — emit `session_capabilities` at session create; image attach appears.
6. **F1** — unify both turn-complete emitters behind `codexStopReason`; emit
   `TypeError` carrying `turn.error` on `failed`.

### Phase 3 — Guards (~2-3 h)

7. **F9** — dispatch conformance + capability conformance tests; add codex to
   the alias test.
8. **F16** — document the `tool_call` status vocabulary in `protocol-v1.md`; add
   the cross-provider assertion.
9. Item-lifecycle fixture test (§2.4) — the first coverage `item/started` has.

### Phase 4 — Hardening (~2-3 h)

10. **F13** — `Unflush` + re-arm in `drainChunks`, codex and httpagent.
11. **F14** — replace per-notification timer resets with an atomic timestamp and
    one ticker.
12. **F15** — wire `account/rateLimits/updated` into the limit card; notice on
    MCP startup failure.

### Phase 5 — Plan widget (~2-4 h, probe-gated)

13. Probe `itemType: "plan"` against the local 0.145.0 engine; implement
    `TypePlan` with normalization and the empty-clear path; delete or re-target
    the dead `turn/plan/updated` arm.

### Deferred

- **F12** — codex Plan mode. `collaborationMode/list` is confirmed (Plan +
  Default); the *set* path is unverified (`0028:573`). Worth a probe, but until
  it lands the honest table entry from §4.3 is the correct state.
- §9.3 tool-output coalescing — needs a protocol decision first.

---

## 11. Appendix A: corrected turn lifecycle

Current (defects marked):

```
User Prompt
  → beginTurn: user_message, session_status "running", turn/start RPC
  ← turn/started
  ← item/started {itemType: "reasoning"}  → tool_call "reasoning"  ← F2 stuck card
  ← item/reasoning/textDelta …            → thought_chunk (×N)
  ← item/started {itemType: "agentMessage"} → tool_call "agentMessage" ← F2 stuck card
  ← item/agentMessage/delta …             → assistant_message_chunk (×N)
  ← item/started {itemType: "commandExecution"} → tool_call status "in_progress" ← F3 no spinner
  ← item/commandExecution/outputDelta     → tool_call_update (×N, blocking) ← §9.3
  ← item/completed {commandExecution}     → tool_call_update "completed"
  ← item/completed {agentMessage}         → (skipped — card never closes) ← F2
  ← turn/completed {turn.status "completed"}
      → drainChunks (may drop final text)  ← F13
      → turn_complete {stop_reason "completed"} → "Turn ended (completed)" ← F1
      → session_status "idle"
```

After:

```
User Prompt
  → beginTurn: user_message, session_status "running", turn/start RPC
  → session_capabilities {image: true, …}   ← F6 (at create)
  ← turn/started
  ← item/started {itemType: "reasoning"}    → (no card)                  ← F2 fixed
  ← item/reasoning/textDelta …              → thought_chunk (×N)
  ← item/started {itemType: "agentMessage"} → (no card)                  ← F2 fixed
  ← item/agentMessage/delta …               → assistant_message_chunk (×N)
  ← item/started {itemType: "plan"}         → plan {entries: […]}        ← F8
  ← item/started {itemType: "commandExecution"} → tool_call "running"    ← F3 fixed
  ← item/completed {commandExecution}       → tool_call_update "completed"
  ← turn/completed {turn.status "completed"}
      → drainChunks (Unflush + retry on backpressure) ← F13 fixed
      → turn_complete {stop_reason "end_turn"} → no bubble               ← F1 fixed
      → session_status "idle"
```

## 12. Appendix B: verification method

Claims in this revision are backed by one of:

- **Source read** — file and line cited inline.
- **Spike capture** — `docs/codex-spike-0.145.0/{protocol-inventory,summary,summary2,summary4}.json`,
  recorded against codex-cli 0.145.0, the pinned version and the one installed
  locally. Used for: the `turn/completed` payload, the `turn_status` enum, the
  observed `notif_methods` list, `thread_item_types`, model input modalities,
  and `collaborationMode/list`.
- **Executed test** — F4's dead-ends were proven by driving `runCanonical` over
  each provider's `KindDaemon` entries and asserting on the emitted notice. That
  scratch test was removed after use; §4.4 proposes promoting it to a permanent
  regression guard.

Two items are explicitly **not** verified and are marked as probe-gated: the
`itemType: "plan"` payload shape (F8) and the codex mode *set* path (F12). No
wire format is asserted in this report without a capture behind it.

---

## 13. References

- [MADR 0028 — Codex provider](./0028-MADR-codex-provider.md) — spike results §16, capability matrix §573
- [MADR 0023 — Canonical slash commands](./0023-MADR-canonical-slash-commands.md) — command table contract
- [MADR 0024 — Stream coalescing](./0024-MADR-stream-coalescing.md) — chunk buffer design
- [MADR 0020 — OpenCode session tree](./0020-MADR-opencode-session-tree.md) — `TypePlan` / `PlanEntry`
- [protocol-v1](./protocol-v1.md) — event vocabulary; §733-746 needs the tool-status addition (F16)

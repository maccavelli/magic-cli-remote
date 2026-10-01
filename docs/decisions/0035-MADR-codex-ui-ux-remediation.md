# MADR 0035: Codex chat remediation — item-stream fidelity, command truth, and capability disclosure

- **Status**: Accepted
- **Date**: 2026-07-27
- **Deciders**: Project Owner
- **Related**: [MADR 0028](./0028-MADR-codex-provider.md) (codex provider; spike
  evidence this MADR draws on), [MADR 0023](./0023-MADR-canonical-slash-commands.md)
  (canonical slash commands — this MADR closes a hole in its conformance
  contract), [MADR 0024](./0024-MADR-stream-coalescing.md) (stream coalescing —
  `drainChunks` hardening), [MADR 0034](./0034-MADR-opencode-tool-stream-fidelity.md)
  (sibling: opencode tool stream; shares the protocol tool-status work)
- **Evidence**: [Report 0032 rev 2](./0032-MADR-codex-ui-ux-polish-report.md)
- **Companion plan**: [0035-PLAN-codex-ui-ux-remediation.md](./0035-PLAN-codex-ui-ux-remediation.md)

---

## 1. Problem

The codex provider (MADR 0028, phases 1-5) reached feature completeness without
a pass over what the phone actually renders. Four independent defect clusters
have accumulated. Three are user-visible; one is a silent data-loss path.

### 1.1 The item stream is malformed

Codex reports everything a turn does as *thread items*. The provider classifies
four of the eighteen item types in `protocol-inventory.json` → `thread_item_types`
and sends the rest to a catch-all that emits a tool card named after the raw
item type (`internal/provider/codex/session.go:673-683`):

```go
default:
    s.emit(event.Event{
        Type:     event.TypeToolCall,
        ToolID:   p.ItemID,
        ToolName: p.ItemType,   // "agentMessage", "reasoning", "plan", …
        Status:   "in_progress",
    })
```

`item/completed` does the opposite — it *excludes* exactly the item types that
are not tools (`session.go:691`):

```go
if p.ItemType != "agentMessage" && p.ItemType != "userMessage" && p.ItemType != "reasoning" {
```

That exclusion list is the proof the author knew these types arrive. The two
handlers disagree: `started` opens a card, `completed` refuses to close it.
Since `_upsertTool` keys cards by tool id
(`apps/mobile/lib/data/chat/transcript_reducer.dart:419`), the user gets cards
literally titled `agentMessage` and `reasoning` beside the real content, stuck
at in-progress forever, plus a card titled `plan` where the todo panel belongs.

The whole default arm is untested — no fixture covers `item/started` at all.

### 1.2 Tool status uses the wrong vocabulary

Codex emits `in_progress` at all six tool-status sites
(`session.go:614,631,649,669,680,714`). Every other provider emits `running`
(opencode `lifecycle.go:301,323`; the ACP providers pass ACP status through),
and `in_progress` is otherwise the daemon's **plan-entry** vocabulary
(`event.go:122`, `protocol-v1.md:820`). No normalization exists anywhere.

The client contract is `toolRunning => toolStatus == 'running' || 'pending'`
(`chat_models.dart:137`). So codex tool cards never show a spinner, and — worse
— they satisfy the `!item.toolRunning` grouping predicate
(`transcript_rows.dart:67`) while still executing, so a running command folds
into "Ran 2 commands" and the count mutates as more arrive.

Root cause is a documentation gap: `protocol-v1.md` specifies `tool_kind`
(`:733-736`) but never the `status` vocabulary, so nothing flagged the drift.

### 1.3 Four slash commands are advertised and dead

`available()` returns `true` unconditionally for `KindDaemon`
(`internal/command/command.go:230-231`), but `runCanonical` dispatches only five
names — `help`, `model`, `clear`, `new`, `sessions`
(`internal/session/commands.go:220-233`). Anything else falls through to
`"“/%s” is not wired up in this build."`

Codex declares `mode`, `context` and `compact` as `KindDaemon`. All three are
advertised in `remote_commands`, listed by `/help` under "You can run:", offered
in composer autocomplete — and then fail. Proven by driving `runCanonical` over
each provider's `KindDaemon` entries:

```
DEAD-END: codex /compact  -> "“/compact” is not wired up in this build."
DEAD-END: codex /mode     -> "“/mode” is not wired up in this build."
DEAD-END: codex /context  -> "“/context” is not wired up in this build."
DEAD-END: goose /mode     -> "“/mode” is not wired up in this build."
```

goose has the same bug (`goose/commandtable.go:8`).

The conformance test cannot catch it: its `KindDaemon` case is an empty
accept-all (`internal/command/conformance_test.go:58`). `codex` is also missing
from `TestTablesAreKeyedByCanonicalName` (`:90-95`).

**`/model` is worse than dead — it is destructive.** Codex implements
`SetModel` (`session.go:480`), and it works: codex's model is per-turn, so
`beginTurn` reads `s.opts.Model` and sends it on `turn/start`
(`session.go:310-317`); the switch lands on the next turn with the thread
intact. But the table declares `KindDaemon`, which routes to
`cmdModel(..., inPlace=false)` → `relaunch()` — destroy-and-recreate, discarding
the conversation (`commands.go:225-226,654-675`). A user typing
`/model gpt-5.6-luna` silently loses their context.

**Internal TODOs are shown to users.** `Resolution.Reason()` returns
`Mapping.Note` verbatim into user copy (`command.go:143-151`,
`commands.go:178`), so codex's `"plan mode - probe required"` and `"not
supported"` are what the phone displays.

### 1.4 A shipped feature is unreachable

The composer gates image attach on `capabilities?.image ?? false`
(`chat_screen.dart:1597`). Only `acpagent` and `acphttp` emit
`TypeSessionCapabilities`, so codex falls back to `false` and the button never
appears — while the provider implements image input completely (MIME handling,
a 10 MB cap, attachment metadata: `session.go:26,1183-1217`) and all six codex
models advertise `inputModalities: [text, image]` (MADR 0028 §16.3).

### 1.5 Turn completion, and two hardening defects

**Status leaks into the transcript.** `params.turn.status` is fed verbatim into
`StopReason` (`session.go:526-532`); the reducer prints a system bubble for
anything it does not recognise (`transcript_reducer.dart:296`), so every turn
ends with "Turn ended (completed)". The enum has four values — `completed`,
`interrupted`, `failed`, `inProgress` — so `interrupted` and `failed` leak too,
the latter reporting a failure as a neutral end-of-turn. `turn.error` is
captured in the wire payload and discarded. There are also **two** emitters —
inline at `session.go:528-542` and `emitTurnComplete` at `:975-992` — so a
one-site fix misses the cancel/error path.

**Assistant text can be dropped.** `drainChunks` sends with the non-blocking
`trySend` and discards the result (`session.go:1144-1152`), while the two other
senders recover via `Unflush` + re-arm (`:1068-1072`, `:1133-1136`). `Drain()`
clears the buffer (`chunkbuf.go:142-148`), so a full 256-slot channel loses the
text. The call sites are immediately before `turn_complete` (`:527`, `:976`) and
on close (`:392`) — and because `TypeTurnComplete` is control and therefore
blocking, the turn ends correctly while the final reply text silently vanishes.
`httpagent/session.go:1264-1272` shares the pattern.

**Timer churn per delta.** `handleNotification` calls `resetStallTimer()`
unconditionally at the top (`session.go:515`); each call takes `s.mu`, stops the
timer and allocates a new `time.AfterFunc` (`:1154-1176`). Every
`item/agentMessage/delta` — hundreds to thousands per turn — pays a timer
allocation plus two runtime timer-heap operations under the session mutex. No
other provider has a stall timer.

### 1.6 Two observed notifications are discarded

The spike's `notif_methods` records nine notifications from a live 0.145.0
engine. Two fall through to `default: s.log.Debug(...)` (`session.go:739-740`):

- `account/rateLimits/updated` — the daemon already has a rate-limit UI contract
  (`error_kind: rate_limit`, `retry_at`, rendered as an actionable limit card,
  `protocol-v1.md:737-744`). Codex is pushing exactly that data into a debug log.
- `mcpServer/startupStatus/updated` — an MCP server failing to start is
  invisible; the user sees tools mysteriously absent.

---

## 2. Decision

Nine changes. D1-D5 are user-visible correctness; D6 is the guard that would
have prevented D3; D7-D9 are hardening.

### 2.1 D1 — An explicit item-type allowlist, shared by both handlers

Replace the catch-all with a single set consulted by `item/started` **and**
`item/completed`, so the two can never disagree again:

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

Silence is the correct default for an unknown item type; a debug log keeps it
discoverable. `contextCompaction`, `enteredReviewMode` and `exitedReviewMode`
become `TypeNotice` — real state changes worth one line, not cards. `plan`
routes to D-plan (§2.10) once probed, and to silence until then: a missing panel
beats a stuck card titled "plan".

### 2.2 D2 — Emit `running`, not `in_progress`

One word at six sites. No client change. Paired with the protocol
specification in §2.11 so the vocabulary is written down rather than folklore.

### 2.3 D3 — Correct the codex command table

Codex implements `Compact` (`session.go:457`) and emits `TypeUsage` from
`thread/tokenUsage/updated` (`:718-734`), which is what gates `OpContext`
(`commands.go:53`). Both resolve as available once routed through `KindOp`.

```go
var commandTable = command.Table{
    "help":     {Kind: command.KindDaemon},
    "new":      {Kind: command.KindDaemon},
    "sessions": {Kind: command.KindDaemon},
    "clear":    {Kind: command.KindDaemon},

    // thread/compact/start — verified OK in the 0.145.0 spike.
    "compact": {Kind: command.KindOp, Op: command.OpCompact},
    // Codex's model is per-turn: SetModel updates opts.Model and beginTurn
    // sends it on the next turn/start, so the thread survives. KindDaemon
    // would relaunch the agent and lose the conversation.
    "model": {Kind: command.KindOp, Op: command.OpSetModel},
    // thread/tokenUsage/updated feeds lastUsage, which gates OpContext.
    "context": {Kind: command.KindOp, Op: command.OpContext},

    // Codex exposes Plan and Default collaboration modes, but the set path is
    // unverified (MADR 0028 §573). Until it is, say so in words a user can act
    // on — the note is displayed verbatim.
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

**`SetModel` must validate.** Unlike opencode's, which validates and calls the
engine synchronously (`session_ops.go:175-192`), codex's accepts any string and
defers failure to the next `turn/start`. Under `KindOp` the daemon reports
"Model is now X — the conversation is kept", so an unvalidated typo becomes a
lie that breaks the next turn. `SetModel` therefore validates against
`Provider.ListModels` (`model/list`, `provider.go:73`) and returns an error the
existing `cmdModel` path already surfaces.

Goose's `"mode": {Kind: KindDaemon}` is corrected to `KindNone` with a reason in
the same change — same bug, found by the same test.

### 2.4 D4 — Emit `session_capabilities` at session create

Mirror `acpagent.emitCapabilities` (`acpagent/session.go:1083-1095`). Derive
each field from what the provider implements and the spike verified, rather than
hardcoding optimism:

| Field | Value | Basis |
|---|---|---|
| `Image` | `true` | `session.go:1183-1217`; all 6 models `inputModalities: [text,image]` |
| `Audio` | `false` | codex advertises no audio modality |
| `LoadSession` | `true` | `thread/resume` verified OK (MADR 0028 §16.3) |

`TypeSessionCapabilities` is a control event (`event.go:100-101`), so a single
emit at create cannot be dropped.

### 2.5 D5 — One turn-completion emitter, with the full status enum

Route `turn/completed` through `emitTurnComplete` instead of duplicating it
inline, and normalize there:

```go
// codexStopReason maps codex's turn_status enum onto the daemon's stop-reason
// vocabulary. The mobile reducer prints a system bubble for anything it does
// not recognise, so an unmapped value becomes visible noise.
func codexStopReason(status string) string {
    switch status {
    case "completed":
        return "end_turn"   // reducer treats this as a silent no-op
    case "interrupted":
        return "cancelled"  // reducer's cancel path: "Turn cancelled"
    case "failed":
        return "error"
    default:
        return status
    }
}
```

On `failed`, also emit `TypeError` carrying `turn.error` so the failure is
reported instead of swallowed.

### 2.6 D6 — Make the conformance test able to catch D3

Two additions to `internal/command/conformance_test.go`:

1. **Dispatch conformance** — every `KindDaemon` name a provider declares must
   have a handler. This requires `runCanonical`'s `KindDaemon` switch to become
   a data table (`daemonCommands`), so the test asserts against the same source
   the dispatcher uses rather than a hand-copied list that can drift.
2. **Capability conformance** — when a provider's session implements
   `CompactSession` / `ModelSession` / `DiffSession` / `UndoSession` /
   `RevertSession`, the table must route the corresponding command through
   `KindOp`, not shadow it with `KindDaemon`. This is what would have caught the
   `/model` regression.

Plus `codex` added to `TestTablesAreKeyedByCanonicalName`.

### 2.7 D7 — Delete the redundant turn-boundary drains; log the close-path drop

Report 0032 F13 proposed `Unflush` + re-arm at every `drainChunks` site. That is
the wrong fix at the turn boundary, for two reasons: retrying would deliver the
text *after* `turn_complete`, inverting transcript order — and the drain is
**redundant there anyway**.

`emit(TypeTurnComplete)` already drains the pending run through the chunkbuf
boundary path (`chunkbuf.go:98-103`), in order, on the blocking path. Verified
by direct test against the real buffer:

```
PROVEN: control event drains "BC" before turn_complete, blocking=true, deadline zero
```

`Add` returns `[coalesced-chunk, turn_complete]` with `blocking = true`, so both
take the blocking send in `emit`'s loop (`session.go:1057-1073`); and because
`drain()` empties `parts`, `deadline()` returns zero (`chunkbuf.go:213-217`) and
`emit` stops the flush timer itself (`:1076-1079`). The explicit call adds
nothing — it merely gets there first, using the non-blocking `trySend`, which is
the *only* thing that can lose the text.

So:

- **Turn boundaries** (`session.go:527`, `:976`): **delete** the
  `s.drainChunks()` call. Fewer lines, no drop, ordering preserved by
  construction.
- **Close** (`session.go:392`): keep it — no control event follows to trigger a
  boundary drain. A blocking send is unsafe here (`close(s.done)` happens later,
  at `:416`, so a stalled consumer would hang `Close`), so keep `trySend` and
  log the byte count `Unflush` returns — the signal that API exists to provide
  (`chunkbuf.go:150-153`).
- **`httpagent`**: its single `drainChunks` call is already close-path only
  (`session.go:731`, before `close(s.done)`), so it needs the logging change
  only, not the deletion.

This decision depends on `TypeTurnComplete` remaining a chunkbuf boundary. MADR
0034 D3 narrows boundary semantics to exclude in-place updates, but scopes that
to `TypeToolUpdate` with a tool id — `turn_complete` is unaffected. The two
MADRs are compatible; the regression test in §2.6 pins it.

### 2.8 D8 — Stall detection by timestamp, not by timer-per-notification

Replace the per-notification reset with a `lastActivity atomic.Int64` (one
atomic store, no lock) plus one per-session ticker that fires the notice when
`now - lastActivity > stallNotice` and the turn is still busy. Constant cost per
notification; one timer per session instead of one per delta.

### 2.9 D9 — Wire the two discarded notifications

`account/rateLimits/updated` feeds the existing limit-card contract
(`error_kind`, `retry_at`). `mcpServer/startupStatus/updated` emits a
`TypeNotice` on failure only — a successful startup needs no line.

### 2.10 Probe-gated, deliberately not decided here

- **Plan/todo panel.** `WorkItemsPanel` exists and is provider-agnostic; the
  daemon side is unwired. But `turn/plan/updated` — the method report 0032 rev 1
  targeted — **has never been observed**: the spike's `notif_methods` is nine
  entries and does not include it, and the inventory contains no notification
  list at all. What is real is that `plan` is a `thread_item_type`, so the plan
  arrives as an item. The payload shape is unknown and will not be guessed
  (AGENTS.md: record probe evidence, pin with a live test).
- **Codex plan mode.** `collaborationMode/list` is confirmed (Plan + Default,
  MADR 0028 §852); the *set* path is unverified (§573). D3's table tells the
  truth in the meantime.

### 2.11 Protocol specification

`protocol-v1.md` gains the `tool_call` / `tool_call_update` status vocabulary —
`pending | running | completed | failed` — with opencode's `mapToolStatus`
(`http.go:1092-1105`) as the reference. Its absence is why §1.2 happened. Shared
with MADR 0034 phase 5; whichever lands first writes it.

---

## 3. Consequences

### 3.1 Positive

- The transcript stops showing cards for things that are not tools, and stops
  leaving them spinning forever.
- `/compact` and `/context` start working; `/model` stops destroying
  conversations; `/mode`, `/plan` and the rest say something true and actionable.
- Image attach appears for codex, making an already-shipped feature reachable.
- D6 converts a whole class of "advertised but dead" defects into test failures,
  for every provider present and future.
- D7 closes a silent text-loss path shared with `httpagent`.

### 3.2 Costs and risks

- **D1 changes what the transcript contains.** Item types that produced (broken)
  cards now produce nothing. That is the intent, but it is a visible behaviour
  change for anyone who had learned to read the noise.
- **D3 changes `/model` semantics** from relaunch to in-place. The conversation
  now survives — but so does any context the user *wanted* cleared; `/clear`
  remains the explicit path for that.
- **D6 requires refactoring `runCanonical`'s dispatch to a table.** Mechanical,
  but it touches the command router, so it lands on its own commit.
- **D8 changes stall-notice timing granularity** from exact to within one tick.
  Acceptable for a 120 s advisory notice.

### 3.3 Cross-provider blast radius

D6 (conformance), D7 (`httpagent` drain) and §2.11 (protocol doc) reach beyond
codex. D6 fixes goose's `/mode` as a direct consequence. Everything else is
codex-local.

---

## 4. Known limitations

- **D1's allowlist must track codex's item vocabulary.** A new item type in a
  future release renders as nothing until added. That is the safe direction, and
  the debug log makes it discoverable — but it is a maintenance edge pinned by
  the live test in plan phase 0.
- **D3 leaves `/mode` and `/plan` unavailable** despite codex having modes. The
  table says so honestly; lifting it needs the §2.10 probe.
- **D4's capabilities are static.** Codex has no ACP-style negotiated
  capabilities exchange, so these are derived from the pinned CLI version rather
  than reported by the engine. A live test pins them.
- **D9's rate-limit mapping depends on an unobserved payload shape** —
  `account/rateLimits/updated` was seen in `notif_methods` but its params were
  not captured. Probe with the phase-0 capture.

---

## 5. Rejected

| Rejected | Why |
|---|---|
| Map only `"completed"` → `"end_turn"` (report rev 1) | Leaves `interrupted` and `failed` leaking; `failed` would report a failure as a neutral end-of-turn |
| Implement `turn/plan/updated` | Never observed on 0.145.0; the plan is a thread item (§2.10) |
| Mobile-side tool-card folding (report rev 1 Phase 4) | Already implemented (`transcript_rows.dart:47-83`). The codex grouping defect is D2, not missing folding |
| Emit `remote_commands` from codex | Already emitted — it is daemon-layer (`internal/session/commands.go:86-123`) and provider-independent |
| Provider-side tool summary notices (report rev 1 §4.3 Option A) | Duplicates the folding that already works |
| Keep `/model` as `KindDaemon` relaunch | Loses the conversation for no benefit; `SetModel` works (§1.3) |
| Guess the plan item payload | AGENTS.md: probe evidence, pinned with a live test |

---

## 6. Verification

| Claim | How verified |
|---|---|
| `item/started` default arm opens uncloseable cards | `session.go:673-683` vs `:691` |
| No fixture covers `item/started` | Search of `fixtures_test.go` |
| `in_progress` is codex-only | Repo-wide search; opencode/ACP emit `running` |
| Four commands dead-end | **Executed test** driving `runCanonical` per provider |
| `SetModel` reaches the engine | `session.go:310-317` sends `opts.Model` on `turn/start` |
| `SetModel` does not validate | `session.go:480-486` vs opencode `session_ops.go:175-192` |
| Image attach gated on capabilities | `chat_screen.dart:1597` |
| Codex implements images | `session.go:26,1183-1217`; MADR 0028 §16.3 |
| `turn/completed` wire shape | Spike capture `summary.json` → `turn_completed` |
| `turn_status` enum | `protocol-inventory.json` → `turn_status` |
| `turn/plan/updated` never observed | `summary.json`/`summary2.json` → `notif_methods` (9 entries) |
| `drainChunks` drops on full channel | `session.go:1144-1152` vs `:1068-1072`, `chunkbuf.go:142-148` |
| Turn-boundary drains are redundant | **Executed test** against `chunkbuf`: a control event returns `[drained-run, control]` with `blocking=true` and a zero deadline |
| Tool-card folding already exists | `transcript_rows.dart:47-83`, wired at `transcript_pane.dart:79,193` |
| `remote_commands` is daemon-layer | Single emitter at `internal/session/commands.go:118` |
| Plan item payload shape | **Not verified** — plan phase 0 |
| `account/rateLimits/updated` params | **Not verified** — plan phase 0 |

---

## 7. Implementation

Phased, with acceptance criteria and rollback per phase, in
[0035-PLAN-codex-ui-ux-remediation.md](./0035-PLAN-codex-ui-ux-remediation.md).

Summary: **P0** probe the item stream, the `plan` item and the rate-limit params
→ **P0** stop the destructive and the malformed (`/model` in-place, D1, D2)
→ **P1** command tables (D3), hidden function (D4, D5), guards (D6)
→ **P2** hardening (D7, D8, D9) → **P3** probe-gated plan panel
→ **P1** protocol and MADR documentation.

Ten phases, ~15-22 h excluding the probe-gated panel. `/model` is split out of
the table work and shipped first: it is the only defect here that destroys user
data, and it is a two-line change plus validation.

---

## 8. Implementation record

Phases 0–9 landed on 2026-07-27 against `codex-cli 0.145.0`. Net changes:

| Phase | Commit | Notes |
|---|---|---|
| 0 | `scripts/probe-codex-item-stream.py`, `docs/codex-spike-0.145.0/item-stream.json` | Probe revealed the codex app-server emits the v2 wire format (item type lives on `params.item.type`, not `params.itemType`). The previous codex provider implementation was written against v1 and was silently broken — every `item/started` produced a tool card with empty fields. The v2 migration is the foundation of every later phase. |
| 1 | `internal/provider/codex/{commandtable.go,session.go,model_test.go}` | `/model` switched to `KindOp` + `OpSetModel`. `SetModel` now validates against the live catalog and fails open on `model/list` errors. |
| 2 | `internal/provider/codex/{session.go,items.go,item_test.go}` | `itemsRenderedAsTools` allowlist + v2 wire-format migration. `contextCompaction` / review-mode become notices. Tool cards no longer spawn for non-tool items. |
| 3 | `internal/provider/codex/items.go` | Tool status goes through `codexToolStatus`: `inProgress` → `running`, `declined` → `failed`. No raw `"in_progress"` left in production code. |
| 4 | `internal/provider/codex/commandtable.go`, `internal/provider/goose/commandtable.go` | `compact` / `context` / `model` route to the right ops; `mode` / `plan` / `goal` / `diff` / `undo` / `redo` get user-facing notes; goose's `/mode` dead-end fixed. |
| 5a | `internal/provider/codex/session.go` | `session_capabilities` emitted at create: `image=true` (verified MADR 0028 §16.3), `load_session=true` (verified), `audio=false`, ACP fields false. Image-attach button now appears. |
| 5b | `internal/provider/codex/session.go` | One `emitTurnComplete` implementation. `codexStopReason` maps `completed`/`interrupted`/`failed` to the daemon vocabulary. Failed turns surface the engine's `turn.error.message` as a `TypeError`. |
| 6 | `internal/session/commands.go`, `internal/command/conformance_test.go` | `runCanonical`'s `KindDaemon` arm refactored to a data table (`daemonCommands`) with `DaemonCommandNames()` for the test. New conformance tests: `TestEveryKindDaemonMappingIsDispatched` and `TestKindOpNamesKnownOp`. Codex added to `TestTablesAreKeyedByCanonicalName`. The chunkbuf boundary regression (`TestBoundaryFlushesTailAheadOfItselfAndBlocks`) continues to pass. |
| 7 | `internal/provider/codex/{session.go,hardening_test.go}` | D7: redundant `drainChunks` removed at turn boundary; close-path drain now logs dropped bytes. D8: stall detection is now one atomic store per notification + one per-session ticker. D9: `account/rateLimits/updated` produces a `TypeError` with `error_kind: rate_limit` at ≥100% and a `TypeNotice` at ≥90%; `mcpServer/startupStatus/updated` failure surfaces a `TypeNotice`. |
| 8 | `internal/provider/codex/{session.go,items.go,plan_test.go}`, `live_test.go` | `turn/plan/updated` wired to `TypePlan` with status normalization (`inProgress` → `in_progress`). Empty plan emits non-nil empty entries (replace-semantics). Live tests added for plan emission and capabilities. |
| 9 | `docs/protocol-v1.md`, this file | Tool status vocabulary documented in `protocol-v1.md` (was the MADR 0035 D2 root cause). |

Phase 0 also revised some report-0032 claims: the `turn/plan/updated`
notification IS in the v2 schema (it was just not exercised by the
0.145.0 free-tier model used in the original spike). The schema is
captured in `docs/codex-spike-0.145.0/item-stream.json` and the unit
tests pin the translation.

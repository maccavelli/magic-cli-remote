# MADR 0034: Tool-stream fidelity — dedup, visible output, and in-place update ordering

- **Status**: Accepted — implemented 2026-07-27
- **Date**: 2026-07-27
- **Deciders**: Project Owner
- **Related**: [MADR 0024](./0024-stream-coalescing.md) (stream coalescing — this
  amends its control-boundary rule), [MADR 0014](./0014-sse-reconnect-resync-decision.md)
  (resync — evaluated and unchanged), [MADR 0020](./0020-opencode-session-tree.md)
  (session tree — subagent cards share the tool-card path),
  [MADR 0021](./0021-opencode-http-api-coverage.md) (`message.part.updated` = "Snapshots + tools")
- **Evidence**: [Report 0033 rev 2](./0033-opencode-ui-ux-polish-report.md)
- **Companion plan**: [opencode-tool-stream-fidelity-implementation-plan.md](./opencode-tool-stream-fidelity-implementation-plan.md)

---

## 1. Problem

MADR 0024 solved assistant *text* streaming. Tool events were left on the
pre-0024 path, and three defects have accumulated there. Two are visible to
users; the third silently undoes 0024's work whenever a turn uses tools.

### 1.1 Tool updates emit on every SSE frame with no change detection

`message.part.updated` with `type: "tool"` emits unconditionally
(`internal/provider/opencode/http.go:833-840`). The only state tracked is
`noteTool(id)` (`http.go:1050-1059`), which distinguishes the first frame
(`tool_call`) from the rest (`tool_call_update`). Nothing compares the payload
against what was last sent.

The payload is near-static across a tool run: `State.Title`, else
`shortJSON(State.Input, 300)`, else `clip(State.Error, 300)`. So a tool that
reports progress over N frames produces N-1 near-identical `tool_call_update`
events.

This provider is known to behave exactly this way for a sibling event. The
`emitStatus` comment records it (`http.go:1024-1031`): *"OpenCode re-sends
`session.status` busy for every step of a turn"*, and `runningSent` exists to
absorb it. MADR 0024 §2.6 added the same treatment for `usage_update`. Tool
frames were never given it.

### 1.2 Tool output never reaches the phone

`State.Output` — the actual stdout/stderr — is unmarshalled at `http.go:799`
and has **zero** uses in the package. A `bash` call shows its command and a
status; the result is invisible. Same for `read`, `grep`, and every other tool.

The client is already built for this content: `kMaxExpandedDetailChars = 8000`
(`apps/mobile/lib/data/chat/chat_models.dart:47`) documents an expandable tool
detail view — *"Expanded tool/thought UI shows at most this many chars before
scroll + copy"*. The surface exists and is fed nothing.

### 1.3 Every tool update destroys the pending text run

This is the defect with the widest blast radius, and it is a consequence of
MADR 0024's own control-boundary rule.

```go
// internal/chunkbuf/chunkbuf.go:98-103
case !IsChunk(ev.Type) && event.IsControl(ev.Type):
    // Boundary: the pending tail must land before it, and the next chunk
    // earns a fresh immediate emit.
    out = append(b.drain(), ev)
    b.leading = true
    blocking = true
```

`TypeToolUpdate` is in `IsControl` (`internal/event/event.go:92`). So every tool
update:

1. **force-drains** the pending assistant-text run, shipping a fragment before
   its 80 ms window matured;
2. **sets `leading = true`**, so the next chunk is emitted immediately as a
   fresh leading edge (`chunkbuf.go:121-125`) instead of starting a buffered run.

Combined with §1.1, a tool-heavy turn fires this N times. Between them, the
assistant text stream is chopped into unbuffered fragments — coalescing is
effectively disabled for the duration of the turn. The daemon returns to
roughly pre-0024 frame counts precisely when the turn is busiest.

### 1.4 The client does not absorb the redundancy either

`_upsertTool` (`apps/mobile/lib/data/chat/transcript_reducer.dart:419-432`) has
no identity check. Every update — including a byte-identical one — runs
`copyWith` and returns a new `SessionTranscript`, publishing new state and
driving a rebuild. The redundant frames are not cheap on arrival.

### 1.5 What is *not* a problem

Report 0033 rev 1 proposed retuning the coalescing window and batching resync.
Both were investigated and rejected on evidence; recorded here so they are not
re-proposed.

- **The 8 KB size trigger does not fire during live streaming.** At 200 tok/s
  and ~4 chars/token, an 80 ms window accumulates ~64 bytes against an
  8192-byte trigger — ~128× short. `chunkbuf.New`'s own doc comment states it:
  *"maxBytes is a safety cap on catch-up bursts (resync tails, message-log
  replay), not a normal flush trigger — at typical token rates it never fires"*
  (`chunkbuf.go:66-68`). The observed jitter is §1.3, not a timer/size race.
- **Resync does not amplify.** `resyncParentMessageTurn` replays only `text` and
  `reasoning` parts (`resync.go:245-249`) — the struct it decodes carries no
  tool state — emits a *delta* via `emitTextCatchUp`, and is gated to finished
  turns three ways (`resync.go:219,222-224,233-235`) plus a `!o.h.EndTurn()`
  race guard (`:252`). The concurrency concern is documented as handled
  (`resync.go:236-244`).
- **`stream_coalesce_ms` is already documented** in `docs/config.md:76` with a
  full explanation of the tradeoff and the `0` escape hatch. (`config.example.yaml`
  does not exist; rev 1's recommendation targeted the wrong file.)

---

## 2. Decision

Four changes. D3 is the only one that touches shared machinery and is the only
one requiring a decision against MADR 0024.

### 2.1 D1 — Dedup tool events at the dialect

Track the last emitted `{status, toolName, toolKind, text}` per tool ID; suppress
a frame whose tuple is unchanged. Always emit when `status` changes, so no
terminal frame can be suppressed. Clear the map in `turnCleanup` alongside
`seenTools`.

This is the third instance of an established pattern in this file —
`runningSent` (`http.go:1032-1045`) and `usageSent` (`http.go:517-522`) — and
uses the same lock (`o.mu`) and the same lifecycle hook.

### 2.2 D2 — Emit tool output as a clipped snapshot, not a delta

Promote `State.Output` into the emitted detail, clipped to a new
`maxToolOutputChars = 8000` with an explicit truncation marker. Precedence:
`Error` > `Output` > `Title` > `shortJSON(Input)`.

**Snapshot, not delta.** OpenCode already sends the full accumulated output in
every frame, and the client's tool detail is **replace** semantics —
`text: clippedDetail.isNotEmpty ? clippedDetail : prev.text`
(`transcript_reducer.dart:428`), where `prev.text` is the *empty-input*
fallback. Delta encoding against a replacing reducer would leave the card
showing only the final fragment: it would look like complete output while being
a tail slice. Snapshot semantics need no client change, no protocol change, and
compose with D1 — frames where output has not grown are suppressed by the same
tuple comparison.

The cap is set at the client's `kMaxExpandedDetailChars`, not at
`kMaxItemTextChars` (100,000): shipping 100 KB the UI will never render is
waste, and the daemon should not depend on the client to clip.

### 2.3 D3 — In-place control events are not stream boundaries

**Amends MADR 0024 §2.2.**

MADR 0024 treats every control event as a stream boundary. That conflates two
distinct guarantees:

- **Delivery** — the event must not be dropped under back-pressure.
- **Ordering** — the event must land after the pending text run, because its
  position in the transcript is relative to that text.

`tool_call` needs both: it *creates* a transcript item, so its position matters.
`tool_call_update` needs only the first: it *mutates an item that already
exists*, positioned by the earlier `tool_call`. A client applying it before or
after some buffered text renders the identical transcript.

So classify by that distinction:

```go
// internal/event
//
// IsInPlaceUpdate reports control events that mutate an existing transcript
// item rather than creating one. They keep the delivery guarantee of
// [IsControl] but carry no ordering constraint against streaming text: the
// item they update was positioned by an earlier event, so a client applying
// them out of order renders the same transcript.
//
// Identified by payload, not type alone: an update with no tool id cannot be
// matched to an existing item, so clients fall back to "the most recent tool
// card" — which *is* order-dependent. Those keep boundary semantics.
func IsInPlaceUpdate(ev Event) bool {
    return ev.Type == TypeToolUpdate && ev.ToolID != ""
}
```

and route them to `chunkbuf`'s existing order-independent arm
(`chunkbuf.go:105-108`), which passes the event through without touching the
pending run.

**The delivery guarantee is unchanged.** Provider sessions send with
`if blocking || event.IsControl(e.Type)` (`httpagent/session.go:1149`,
`codex/session.go:1058`, `acphttp/session.go:1117`). `IsControl(TypeToolUpdate)`
stays `true`, so these still take the blocking channel path. Only the
*boundary* behaviour changes, not droppability.

**Why not make them droppable** (the framing in report 0033 rev 1's §2.3.D):
droppability is only safe when the next frame supersedes the one dropped. That
holds for opencode, whose `part.updated` carries a full snapshot. It does **not**
hold for codex, whose `item/commandExecution/outputDelta` carries incremental
text (`internal/provider/codex/session.go:702-717`) — dropping one would lose
output permanently. A type-level droppable rule would silently corrupt codex
output. D3 avoids the hazard entirely by changing ordering rather than delivery.

**Invariant D3 rests on**: `tool_call` remains a boundary and is never buffered
(only chunks are), so it always lands before any update for the same id.

### 2.4 D4 — Identity guard in the client reducer

`_upsertTool` returns `t` unchanged when `status`, `toolName`, `toolKind` and
`text` all match the existing item. Cross-provider, independent of D1, and it
also covers replayed history and any future engine that re-sends snapshots.

### 2.5 Rejected

| Rejected | Why |
|---|---|
| Raise `stream_coalesce_ms` 80 → 200 | Aimed at a size/timer race that cannot occur (§1.5). The current 80 ms is deliberately aligned to the client's `kTranscriptBatchWindow = 32ms` (`config.go:566-569`); 200 ms discards that alignment for no measured gain |
| Raise `maxPendingChunkBytes` 8 KB → 32 KB | Would change only the resync/replay path the cap exists to protect, and quadruples growth-guard retention to 128 KB/session (`growthFactor = 4`) |
| Batch resync events | The amplification does not exist (§1.5) |
| Delta-encode tool output | Broken against replace semantics (§2.2) |
| Make `tool_call_update` droppable by type | Corrupts codex delta output (§2.3) |
| Per-tool rate limiting | Held pending measurement (§3.3). Adds a timer per tool for a frame count nobody has counted |

---

## 3. Consequences

### 3.1 Positive

- Tool results become visible — the single largest functional gap in the
  opencode chat surface.
- Assistant-text coalescing survives tool-heavy turns. D3 restores MADR 0024's
  intended behaviour in exactly the case where it was silently absent.
- D1 + D4 remove redundant work on both sides of the socket.
- D3 and D4 benefit every provider, not just opencode.

### 3.2 Costs and risks

- **D2 increases per-event payload.** Bounded by the 8 KB cap and by D1's
  suppression. Land D1 and D3 first (see the plan's phase order) so the larger
  payload rides on a reduced frame count and a non-boundary path.
- **D3 changes event ordering on the wire.** A `tool_call_update` may now be
  delivered before text chunks that were generated before it. Safe by the
  argument in §2.3, but it is an observable change: any consumer that infers
  transcript position from tool-update arrival order would break. The mobile
  reducer does not (it is keyed by `toolId`); the guard in `IsInPlaceUpdate`
  covers the one path that would.
- **`seq` ordering changes** for affected events, since seq is assigned at
  emit. Nothing consumes seq as a tool-ordering signal today.

### 3.3 Unmeasured

The frame count per tool run is unknown. Report 0033 rev 1's "30 frames /
29 redundant" was a constructed example, not an observation. That number decides
whether D1 alone suffices or per-tool rate limiting is also needed. opencode
1.18.5 is installed locally (`~/.opencode/bin/opencode`), so this is a probe,
not a blocker — it is phase 0 of the plan, and its capture is committed
alongside the codex spike.

---

## 4. Known limitations

- **D1 is per-turn.** `turnCleanup` clears the map, so the first frame of each
  turn always emits. Correct, and matches `runningSent`/`usageSent`.
- **D2 shows the tail of long output.** Clipping keeps the head with a
  truncation marker; a 50 MB build log shows its first 8 KB. Full output remains
  an agent-mediated affair ("show me the rest").
- **D3 does not reduce event count**, only boundary damage. D1 reduces count.
- **The id-less update path stays a boundary.** No opencode event hits it today
  (`callID` or `part.ID` is always set), so this is future-proofing, not a
  live case.

---

## 5. Verification

Every claim above is anchored to source, to arithmetic, or to a probe:

| Claim | How verified |
|---|---|
| `State.Output` unused | Package-wide search: zero references |
| No tool dedup exists | `http.go:833-840`; `noteTool` tracks presence only |
| Tool updates are boundaries | `chunkbuf.go:98-103` + `event.go:92` |
| Reducer replaces, not appends | `transcript_reducer.dart:428`; `prev.text` is the empty-input fallback |
| Reducer has no identity check | `transcript_reducer.dart:419-432` |
| Size trigger cannot fire live | Arithmetic (§1.5) + `chunkbuf.go:66-68` |
| Resync does not amplify | `resync.go:203-207,219-249` |
| Delivery preserved under D3 | `httpagent/session.go:1149` and the two sibling sites |
| Codex would break under type-level droppable | `codex/session.go:702-717` emits deltas |
| Frame count per tool run | **Not verified** — plan phase 0 |

Per AGENTS.md, the phase-0 probe capture is committed and pinned with a
live-tagged test, since it records external CLI behaviour.

---

## 6. Implementation

Phased, with acceptance criteria and rollback per phase, in
[opencode-tool-stream-fidelity-implementation-plan.md](./opencode-tool-stream-fidelity-implementation-plan.md).

Summary: **P0** measure (phase 0) → **P0** D1 dedup → **P1** D3 ordering →
**P1** D2 output → **P1** D4 client guard → **P2** re-measure and decide on
rate limiting.

---

## 7. Implementation record

- **Phase 0**: Pinned empirical behavior via `live_tool_stream_test.go` and committed capture (`docs/opencode-spike-1.18.5/tool-frames.json`). Verified monotonic output growth and terminal status frame ordering.
- **Phase 1 (D1)**: Implemented dialect-level deduplication (`lastToolEmit` map + `noteToolEmit` latch) in `internal/provider/opencode/http.go` with unit tests in `dedup_test.go`.
- **Phase 2 (D3)**: Added `event.IsInPlaceUpdate` and updated `chunkbuf.Add` to allow in-place tool updates with a tool ID to pass through without force-draining pending assistant text runs. Unit tests added in `chunkbuf_test.go`.
- **Phase 3 (D2)**: Implemented `clipBlock` (preserving newlines and rune boundaries) and updated tool output detail precedence chain (`maxToolOutputChars = 8000`). Unit tests added in `http_delta_test.go`.
- **Phase 4 (D4)**: Added client-side identity guard in `apps/mobile/lib/data/chat/transcript_reducer.dart` to prevent redundant transcript copies and re-renders. Verified with unit tests in `transcript_reducer_test.dart`.
- **Phase 5**: Protocol spec (`docs/protocol-v1.md`), MADR 0024, and MADR 0034 updated. Verification suite green.

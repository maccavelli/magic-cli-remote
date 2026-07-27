# Report 0033: OpenCode Chat UI/UX — Tool Streaming and Coalescing

- **Date**: 2026-07-27 (revision 2 — re-grounded against the codebase)
- **Status**: Analysis complete and verified; recommendations for review
- **Related**: [MADR 0024](./0024-stream-coalescing.md), [MADR 0020](./0020-opencode-session-tree.md),
  [MADR 0014](./0014-sse-reconnect-resync-decision.md), [MADR 0021](./0021-opencode-http-api-coverage.md),
  [Report 0032](./0032-codex-ui-ux-polish-report.md)

---

## 0. What changed in this revision

Every claim in revision 1 was checked against source, the chunkbuf design
comments, and arithmetic. The result splits cleanly:

- **§2 (tool firehose) and §4 (tool output dropped) are real**, and §2 is
  materially *worse* than described — for a reason rev 1 didn't identify.
- **§3 (coalescing jitter) does not survive contact with the numbers.** Its
  central mechanism is refuted by the chunkbuf source comment and by a
  two-order-of-magnitude arithmetic error.
- **§5 (resync amplification) describes behaviour the code explicitly prevents.**

| Rev-1 claim | Verdict | Notes |
|---|---|---|
| §2.1 tool handler emits per SSE frame, no dedup | **Confirmed** | `http.go:817-841`; no content latch exists |
| §2.1.2 `TypeToolUpdate` is a control event | **Confirmed, understated** | It also drains the chunk buffer — see N1 |
| §2.1.3 "mobile reducer short-circuits on the identical-check path" | **Wrong** | No identity check exists — see N2 |
| §2.2 "30 frames / 29 no-op returns" | **Unmeasured** | Presented as measurement; it is a hypothetical |
| §2.3.B / §4.3 "the reducer already appends delta text" | **Wrong** | `transcript_reducer.dart:428` *replaces*. The proposed fix would show only the last fragment |
| §3.2 "8KB trigger fires early (~500 chars)" | **Wrong** | 8KB is 8192 chars; off by ~16× |
| §3.3.B "the 8KB cap fires frequently" | **Refuted by source** | `chunkbuf.go:66-68`: "at typical token rates it never fires" |
| §3.3.A "inside the client's 120ms streaming-markdown throttle tier" | **Unfounded** | No 120ms tier exists; the throttle is frame-aligned |
| §4 `part.State.Output` parsed and never emitted | **Confirmed** | Zero uses in the package |
| §5.1 resync "replays … assistant text, tool cards" | **Wrong** | Replays `text`/`reasoning` parts only |
| §5.1 "happens in parallel with the live SSE pump, doubling throughput" | **Wrong** | Gated finished-turn-only; emits a delta under `o.mu` |
| §2.3.A precedent latches (`runningSent`, `usageSent`) | **Confirmed** | Accurate citations; good pattern to copy |

Net effect: rev 1 budgeted 30 minutes on a config change that addresses nothing
and 3-4 hours on a resync problem that does not exist, while proposing a
tool-output fix that would not work against the actual client.

---

## 1. Findings

| # | Finding | Severity | Evidence |
|---|---|---|---|
| **F1** | Tool updates emit on every SSE frame with no content dedup | **High** | `http.go:833-840` |
| **N1** | Each tool update drains the chunk buffer and resets the leading edge, degrading assistant-text coalescing | **High** | `chunkbuf.go:98-103` |
| **F2** | `part.State.Output` is parsed and never emitted — tool results are invisible | **High** | `http.go:799`; zero uses |
| **N2** | `_upsertTool` has no identity short-circuit: every redundant update publishes new transcript state | **Medium** | `transcript_reducer.dart:419-432` |
| **N3** | Tool detail is replace-semantics, not append — delta encoding is not viable without a client change | **Medium** | `transcript_reducer.dart:428` |
| **N4** | The §2 impact model was never measured, though the engine is installed locally | **Low** | opencode 1.18.5 at `~/.opencode/bin/opencode` |

Rev 1's §3 and §5 produce no findings. See §5 and §6 for why.

---

## 2. F1 + N1 — The tool firehose, and why it costs more than event count

### 2.1 Confirmed mechanics

`message.part.updated` with `type: "tool"` emits unconditionally
(`http.go:833-840`). The only state tracked is `noteTool(id)`
(`http.go:1050-1059`), which distinguishes *first* frame (→ `TypeToolCall`)
from *subsequent* frames (→ `TypeToolUpdate`). There is no comparison of the
emitted payload against the last one.

Rev 1's characterisation of the detail field is correct: it is
`State.Title`, else `shortJSON(State.Input, 300)`, else `clip(State.Error, 300)`
— all static across a tool run. So repeat frames carry near-identical payloads.

The engine's chattiness is independently documented in this same file. The
`emitStatus` comment (`http.go:1024-1031`) records that *"OpenCode re-sends
`session.status` busy for every step of a turn"*, and the `runningSent` latch
exists specifically to absorb that. The same provider gets no such treatment for
tool frames.

### 2.2 N1 — the part rev 1 missed

Rev 1 framed the cost as "N extra events". The larger cost is what a control
event does to the *text* stream. In `chunkbuf.Add`:

```go
// chunkbuf.go:98-103
case !IsChunk(ev.Type) && event.IsControl(ev.Type):
    // Boundary: the pending tail must land before it, and the next chunk
    // earns a fresh immediate emit.
    out = append(b.drain(), ev)
    b.leading = true
    blocking = true
```

Every `tool_call_update` therefore:

1. **Force-drains** the pending assistant-text run, emitting a short fragment
   before its 80ms window matured.
2. **Sets `b.leading = true`**, so the *next* assistant chunk is emitted
   immediately as a fresh leading edge (`chunkbuf.go:121-125`) rather than
   being buffered.
3. Takes the **blocking** channel path.

So during a tool-heavy turn, the tool frames repeatedly chop the assistant text
stream into unbuffered fragments. Coalescing is effectively disabled for the
duration. This is the real "uneven delivery cadence" — rev 1 attributed that
symptom to a timer/size race in §3 (which cannot happen, see §5) when the
actual cause is the control-event boundary in §2.

That also inverts rev 1's recommendation ordering: option **D** (droppable
semantics for non-terminal tool updates) is not merely "the most efficient"
— it is the only option that addresses N1. Dedup (A) reduces how often the
boundary fires; only D stops non-terminal updates from being boundaries at all.

### 2.3 N2 — the client does not short-circuit either

Rev 1 asserted that redundant updates are cheap on the phone because the
reducer "short-circuits on the identical-check path" and "29 are no-op identity
returns". There is no such path. `_upsertTool` unconditionally rebuilds:

```dart
// transcript_reducer.dart:419-432
if (id.isNotEmpty && t.toolIndex.containsKey(id)) {
  final i = t.toolIndex[id]!;
  if (i >= 0 && i < t.items.length && t.items[i].kind == ChatItemKind.tool) {
    final items = _mutableItems(t);          // copies the list when !growableItems
    final prev = items[i];
    items[i] = prev.copyWith(...);           // no equality check
    return t.copyWith(items: items, growableItems: true);   // publishes new state
  }
}
```

Every redundant frame publishes a new `SessionTranscript`, which drives a
listener notification and a transcript rebuild. Rev 1's impact table understates
the client cost rather than overstating it.

**This is worth fixing independently of the provider.** A provider-side dedup
latch fixes opencode; an identity guard in `_upsertTool` fixes every provider,
including replayed history and any future engine that re-sends snapshots. Return
`t` unchanged when `status`, `toolName`, `toolKind` and `text` all match `prev`.

### 2.4 N4 — measure before tuning

Rev 1's §2.2 table ("30 SSE frames", "29 no-op returns") and §7.1's 22-frame
trace read as observations but are constructed examples. The frame count per
tool run is the single number that determines whether A alone suffices or C
(rate-limiting) is needed, and it is unknown.

opencode 1.18.5 is installed (`~/.opencode/bin/opencode`), so this is a short
probe, not a blocked task: run a session with a `bash` command producing
incremental output, count `message.part.updated` frames with `type: "tool"` per
call ID, and record whether `State.Output` grows monotonically across them.
That capture also settles the F2 design (§3) and belongs in the repo next to the
codex spike.

### 2.5 Recommended fix

1. **Dedup latch** (rev 1's A) — sound, and the cited precedents
   (`runningSent` `http.go:1032-1045`, `usageSent` `http.go:517-522`) are
   accurate and clear it for reuse. Key on the tuple actually emitted, and
   always emit on a status transition so terminal frames can never be
   suppressed. Clear it in `turnCleanup` alongside `seenTools`.
2. **Reducer identity guard** (N2) — independent, cross-provider.
3. **Droppable non-terminal updates** (rev 1's D) — the only fix for N1. It is a
   change to the MADR 0024 §1.1 guarantees and needs sign-off: `tool_call_update`
   would become droppable only when it carries a non-terminal status *and* no
   status transition. Terminal (`completed`/`failed`) stays control. Note this
   splits `IsControl` by payload rather than by type, which the protocol
   currently does not do — that is the substance of the sign-off.
4. **Rate limiting** (rev 1's C) — hold until N4 is measured. If dedup drops the
   frame count to a handful, this is unnecessary complexity.

---

## 3. F2 + N3 — Tool output is invisible, and the proposed fix does not work

### 3.1 The gap is real

`State.Output` is unmarshalled at `http.go:799` and has **zero** uses in the
opencode package. Confirmed by search across the package and its tests. A `bash`
call shows its command and status; the stdout never reaches the phone. Same for
`read`, `grep`, and every other tool.

The client is ready for it: `kMaxExpandedDetailChars = 8000`
(`chat_models.dart:47`) documents an expandable tool detail view — *"Expanded
tool/thought UI shows at most this many chars before scroll + copy"*. There is a
place to put this content.

### 3.2 N3 — delta encoding is broken against this client

Rev 1 recommends emitting only the new suffix, twice justified by the claim that
the reducer appends:

> "The mobile reducer already handles `tool_call_update` with text deltas that
> append to the detail field (line 428 of transcript_reducer.dart:
> `text: clippedDetail.isNotEmpty ? clippedDetail : prev.text`)"

The quoted line is a **replace**, not an append. `prev.text` is the fallback for
when the incoming text is *empty*; a non-empty incoming value overwrites. Under
rev 1's design the card would end up showing only the final delta fragment —
strictly worse than today, because it would look like complete output while
being a tail slice.

### 3.3 Two viable designs

**Option 1 — provider accumulates, sends the snapshot (recommended).**

opencode already sends the full accumulated output in each frame, so the
provider does not need to accumulate anything. Emit the clipped full output as
`Text` and let the existing replace semantics do the right thing:

```go
detail := strings.TrimSpace(part.State.Title)
if detail == "" {
    detail = shortJSON(part.State.Input, 300)
}
if out := strings.TrimSpace(part.State.Output); out != "" {
    detail = clip(out, maxToolOutputChars)
}
if part.State.Error != "" {
    detail = clip(part.State.Error, 300)   // error still wins
}
```

- No client change, no protocol change.
- Composes correctly with the F1 dedup latch: frames where the output has not
  grown are suppressed by the same tuple comparison.
- Cost is re-sending the snapshot each time it changes. Bounded by the cap and
  by dedup; with option 2.5.3 in place these are droppable anyway.

**Option 2 — deltas plus append semantics in the reducer.**

Only viable if `_upsertTool` gains an explicit append mode, which means a
protocol flag distinguishing "this text replaces" from "this text appends".
Cheaper on the wire, but it adds a mode to an event that is currently
unambiguous, and it interacts badly with resync (a replayed snapshot would
double-append). Not recommended.

**Cap.** Rev 1 suggests `kMaxItemTextChars` (100,000) or ~10,000. Prefer a
provider-side cap near the client's `kMaxExpandedDetailChars` (8,000) with an
explicit truncation marker — sending 100 KB the UI will never show is waste, and
the daemon should not rely on the client to clip.

**Ordering note.** F2 raises the payload size of exactly the events N1 makes
boundary-forcing. Land the F1 dedup latch (and ideally the droppable change)
before or with F2, not after.

---

## 4. Tool status vocabulary is correct here

Worth recording because the sister report found the opposite. `mapToolStatus`
(`http.go:1092-1105`) normalises to `pending` / `running` / `completed` /
`failed`, which is exactly what the mobile client consumes
(`chat_models.dart:137-138`) and what its grouping predicate needs.

OpenCode is the reference implementation. Report 0032 F3 covers codex emitting
`in_progress` instead. Any cross-provider status conformance test added there
should treat this file as the baseline.

---

## 5. Why rev 1 §3 (coalescing) produces no finding

### 5.1 The size trigger cannot fire during live streaming

Rev 1's §3.2 rests on a timer-versus-size race, and states the 8KB trigger
"can fire early (~500 chars of dense output)". 8 KB is 8192 chars — the figure
is off by roughly 16×.

The arithmetic settles it. At 200 tokens/sec and ~4 chars/token, output is
~800 chars/sec. In an 80ms window that is **~64 bytes**. The trigger is at
**8192 bytes** — about 128× higher. To fire it in one window a model would need
to emit ~100 KB/s, roughly 25,000 tokens/sec.

The chunkbuf source says so directly:

```go
// chunkbuf.go:66-68
// maxBytes is a safety cap on catch-up bursts (resync
// tails, message-log replay), not a normal flush trigger — at typical token
// rates it never fires.
```

So §3.3.B ("the 8KB cap fires frequently with modern high-output models") is
refuted by the design it proposes to change, and raising 8 KB → 32 KB would
alter only the resync/replay path — the one place the cap is *meant* to act —
while quadrupling the growth-guard retention from 32 KB to 128 KB per session
(`growthFactor = 4`, `chunkbuf.go:33,165`).

### 5.2 The 120ms throttle tier does not exist

§3.3.A justifies 80ms → 200ms by placing it "inside the mobile client's 120ms
streaming-markdown throttle tier". There is no such tier. The streaming markdown
throttle is **frame-aligned** — *"Proposal F: frame-aligned throttle — at most
one render per frame"* (`chat_bubble.dart:505`) — i.e. ~16ms at 60Hz, with the
parsed result cached and markdown parsing offloaded to a background isolate.

The client's real batching constant is `kTranscriptBatchWindow = 32ms`
(`transcripts_notifier.dart:26`), and the current 80ms default is explicitly
chosen against it (`config.go:566-569`): *"~12 mid-stream updates/sec instead of
one per token (MADR 0024). Inside the phone's 32ms event batch window."*

Raising to 200ms would move each coalesced event into its own batch window,
discarding that alignment for no measured benefit. Because rendering is already
frame-aligned and parse-cached, the client is not the bottleneck the change was
aimed at.

### 5.3 Verdict

No change to `stream_coalesce_ms` or `maxPendingChunkBytes`. The jitter rev 1
observed is real, but its cause is N1 (control-event boundaries chopping the
text run), not the timer/size interaction. Fixing N1 addresses it; retuning the
window would not have.

Rev 1's §3.3.C — documenting the knob in `config.example.yaml` — is worth
keeping. It is independently useful, costs nothing, and should state the actual
tradeoff (larger values coarsen mid-stream granularity; `0` disables coalescing
and reproduces pre-MADR-0024 behaviour, per `chunkbuf.go:64-66`).

---

## 6. Why rev 1 §5 (resync amplification) produces no finding

§5 claims resync "fetches the full message log via REST and replays: all
messages the SSE pump may have missed (assistant text, tool cards)", "in
parallel with the live SSE pump", "doubling throughput" for 1-2 seconds.

Four things are wrong.

**It does not replay tool cards.** `resyncParentMessageTurn` iterates parts and
handles exactly two types (`resync.go:245-249`):

```go
for _, part := range last.Parts {
    if part.Type == "text" || part.Type == "reasoning" {
        o.emitTextCatchUp(part.ID, part.Type, part.Text)
    }
}
```

The struct it decodes into does not even carry tool state (`resync.go:203-207`).

**It does not replay messages — it emits a delta.** `emitTextCatchUp` compares
against `o.partText` and emits only what is missing.

**It is gated to finished turns.** The path returns early if the last message is
not the assistant's (`:219`), if the turn is still streaming engine-side
(`:222-224`), and if the completion timestamp predates this turn (`:233-235`).
The transport calls `Resync` only while turn-active and not `promptInFlight`
(`resync.go:25-27`). So the "overlap with a live pump mid-turn" window is
precisely what the gates exclude.

**The concurrency it worries about is handled and documented** (`resync.go:238-244`):

> "emitTextCatchUp holds `o.mu` across the comparison, so it is safe against a
> concurrently running SSE pump (part.delta handler also serializes on `o.mu`).
> … the authoritative snapshot is always the full text, so a stale prev with
> extra text just means no delta is emitted."

There is even a race guard for the turn-end itself: `if !o.h.EndTurn() { return }`
(`:252`) — *"the live stream delivered the turn-end while we were fetching"*.

Actual resync emission volume: at most one text delta per part, one plan event
from `resyncTodos`, any genuinely-pending permissions/questions, and the
turn-end pair. That is not a 2× amplification.

**Verdict**: no change. Rev 1's 3-4 hour P3 should be dropped, not deferred.

---

## 7. Recommended order

| Priority | Item | Fix | Effort |
|---|---|---|---|
| **P0** | F1 — tool update dedup | Per-tool `{status, detail, toolName}` latch; always emit on status transition; clear in `turnCleanup` | 1-2 h |
| **P0** | N4 — measure | Probe opencode 1.18.5: tool `part.updated` frames per call ID, and whether `State.Output` grows monotonically | 1 h |
| **P1** | F2 — tool output visible | Emit clipped `State.Output` as the detail **snapshot** (§3.3 Option 1), cap ~8,000 chars | 1-2 h |
| **P1** | N2 — reducer identity guard | Return `t` unchanged when status/name/kind/text all match | 30 min |
| **P2** | N1 — droppable non-terminal updates | Non-terminal, non-transition `tool_call_update` becomes droppable; terminal stays control. **Needs MADR 0024 sign-off** | 2-3 h + review |
| **P3** | §3.3.C — document the knob | `config.example.yaml` note on `stream_coalesce_ms`, including `0` | 15 min |
| — | Rev 1 §3.3.A/B — retune coalescing | **Dropped.** Refuted in §5 | — |
| — | Rev 1 §5 — batch resync | **Dropped.** Refuted in §6 | — |
| — | Rev 1 §2.3.C — rate limiting | **Held** pending N4 | — |

---

## 8. Appendix A: corrected tool execution path

Rev 1's §7.1/§7.2 traces are hypothetical and their event counts are unmeasured
(N4). The *shape* below is verified from source; the frame count is deliberately
left as N.

Current, for a `bash` call producing incremental output:

```
Engine: N × message.part.updated {type:tool, status:running, title:"ls -la", output:<grows>}

Provider (http.go:833-840), for each frame:
  tool_call        {id, name:"ls -la", status:"running", text:"ls -la"}   ← first only
  tool_call_update {id, status:"running", text:"ls -la"}                  ← ×(N-1), identical
      each is a control event → chunkbuf drains the pending assistant run,
      sets leading=true, and takes the blocking path                       ← N1
  tool_call_update {id, status:"completed", text:"ls -la"}

Client: every frame runs _upsertTool → copyWith → publishes new state       ← N2
Gap:    State.Output never leaves the daemon                                ← F2
```

After F1 + F2 + N1 + N2:

```
Engine: [same N frames]

Provider:
  tool_call        {id, name:"ls -la", status:"running", text:"ls -la"}
  tool_call_update {id, status:"running", text:"total 48\ndrwxr-xr-x …"}  ← emitted only when
  tool_call_update {id, status:"running", text:"total 48\n… -rw-r--r-- …"}   the tuple changes
      non-terminal + no status transition → droppable, coalesced           ← N1 fixed
  tool_call_update {id, status:"completed", text:<final, clipped 8k>}      ← terminal, control

Client: unchanged frames return t identity                                  ← N2 fixed
Filled: user sees tool output in the expandable detail view
```

## 9. Appendix B: verification method

- **Source read** — file and line cited inline. All rev-1 line citations were
  re-checked; `http.go:817-841`, `http.go:799`, `config.go:569`,
  `event.go:92` and the two latch citations are accurate. `transcript_reducer.dart:428`
  is accurate as a *location* but was misread as append semantics.
- **Arithmetic** — §5.1 refutes the size-trigger premise from the constants
  (`maxPendingChunkBytes = 8 << 10`, `growthFactor = 4`) against realistic token
  rates, corroborated by the `chunkbuf.New` doc comment.
- **Design comments as authority** — `chunkbuf.go:63-71`, `resync.go:15-27` and
  `resync.go:236-244` document the behaviours rev 1 §3 and §5 asserted were
  problems. Where a comment and a claim disagreed, the code was read to confirm
  the comment.
- **Not measured** — the per-tool SSE frame count (N4). No frame count in this
  report is asserted as observed; the engine is installed locally and §2.4
  specifies the probe.

---

## 10. References

- [MADR 0024 — Stream coalescing](./0024-stream-coalescing.md) — chunkbuf, control boundaries, §1.1 guarantees (N1 sign-off)
- [MADR 0014 — SSE reconnect resync](./0014-sse-reconnect-resync-decision.md) — the gates §6 relies on
- [MADR 0020 — OpenCode session tree](./0020-opencode-session-tree.md) — tree-aware EndTurn, subagent cards
- [MADR 0021 — OpenCode HTTP API coverage](./0021-opencode-http-api-coverage.md) — `message.part.updated` = "Snapshots + tools"
- [Report 0032 — Codex UI/UX](./0032-codex-ui-ux-polish-report.md) — sister report; §4 here is the counterpart to its F3

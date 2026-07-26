# MADR 0024: Coalesce streaming chunk text at the transport emit seam

- **Status**: Accepted — phases 0–3 implemented 2026-07-26
- **Date**: 2026-07-26
- **Deciders**: Project Owner
- **Related**: [MADR 0011](./0011-opencode-provider-plan.md) (OpenCode provider;
  performance addendum), [MADR 0014](./0014-sse-reconnect-resync-decision.md)
  (SSE reconnect resync — the concurrent `Emit` caller this design must order
  against), [MADR 0018](./0018-mobile-chat-performance-action-plan.md) (mobile
  chat performance; **closes decision D1**),
  [MADR 0019](./0019-opencode-process-management-plan.md) (HTTP-only OpenCode),
  [MADR 0020](./0020-opencode-session-tree.md) (session tree)
- **Companion**: [docs/chat-performance.md](./chat-performance.md)

---

## 1. Problem

**The daemon emits one WebSocket frame per OpenCode token.** OpenCode 1.18
streams assistant text as `message.part.delta` SSE frames carrying token
fragments; the dialect emits one `event.Event` per fragment
(`internal/provider/opencode/http.go:775`), and nothing between that call and
the socket coalesces on time. A 1–5 byte token ships inside a ~150–200 byte
JSON envelope (`v`, `type`, `payload.event.{type, session_id, timestamp, seq,
text}`). A single medium reply is hundreds to low-thousands of frames.

Per frame the daemon pays a manager-global write lock
(`internal/session/manager.go:441`), a history-ring append, a second
`RWMutex` acquisition via `OwnerOf` (`manager.go:569`) inside `BroadcastEvent`,
one `json.Marshal` (`internal/ws/server.go:243`), and one `conn.Write` per
client. The phone pays a `jsonDecode`, an `Envelope.fromJson`, a
`SessionEvent.fromJson` and a broadcast-stream dispatch — all on the UI isolate
(`apps/mobile/lib/data/ws/mcremote_client.dart:1238-1279`).

Field symptom: OpenCode chat sessions on the phone are close to unusable. The
chat window cannot keep up with the arrival rate.

Four amplifiers make it worse than the raw frame count suggests.

**1.1 — No change detection on `usage_update` or `session_status`.**
`emitUsage` (`internal/provider/opencode/usage.go:47-63`) emits on *every*
`message.updated` frame with `tokens.inContext() > 0`, and OpenCode ships those
repeatedly mid-stream. `handleSessionStatus`
(`internal/provider/opencode/lifecycle.go:98-102`) re-emits
`session_status: running` on every busy frame. On the phone, neither type is in
`_isBatchableEvent` (`apps/mobile/lib/state/transcripts_notifier.dart:36-45`),
so each one bypasses the 32 ms batch window and forces an immediate state commit
(`transcripts_notifier.dart:225-226`). The client's own coalescer is therefore
substantially disabled mid-stream — the batching MADR 0018 credits as "Strong"
does much less than it appears to.

**1.2 — The client copies the whole reply twice per chunk.**
`_appendChunk` (`apps/mobile/lib/data/chat/transcript_reducer.dart:347-361`)
runs once per *chunk*, not once per batch, because `_flushSession` loops
`applySessionEvent` per event (`transcripts_notifier.dart:241-249`). Both the
`StringBuffer(prev)` construction and the `.toString()` copy the accumulated
reply, so total cost is O(N·k). MADR 0018 recorded this as finding C1 and
decided it as D1, but the decision was never implemented.

**1.3 — The 800-event history ring holds ~800 *tokens*, not ~800 messages.**
`historyBufferCap = 800`, `historyTrimTo = 600`
(`internal/session/manager.go:27,104`). One long reply appends ~2000 events and
evicts the entire preceding conversation, leaving the ring holding the tail of
that single answer. `session.history` on cold reopen then returns a truncated
transcript in ~200-token pages (`historyDefaultPage = 200`), most of which is
envelope against the 512 KiB response cap. **This is a correctness bug, not a
performance one**, and it is the strongest argument for fixing this upstream of
the ring rather than downstream.

**1.4 — A slow client is disconnected, not throttled.** `writeBytes`
(`internal/ws/server.go:1357-1370`) kills any peer that falls
`outboundQueueLen = 1024` frames behind. At per-token granularity that is
roughly one reply.

Markdown rendering is **not** implicated. It is already defended by a widget
cache, 120/200/320 ms throttle tiers, and a plain-text bailout above
`kMaxStreamingMarkdownChars = 4000` (`apps/mobile/lib/features/chat/chat_bubble.dart:492-711`),
with `debugMarkdownParseCount` assertions pinning the guarantees.

### 1.5 Prior art

No comparable system ships one frame per unit of output.

- **xterm.js** buffers writes against a `WRITE_TIMEOUT_MS = 12` budget
  (deliberately under a 16 ms frame) and, for remote producers, documents a
  high/low-watermark ACK protocol — pause the source at ~100 KB outstanding,
  resume at ~10 KB, attaching write callbacks only every ~100 KB so most chunks
  take a fast path.
- **VS Code's integrated terminal** renders in an animation-frame callback and
  skips frames whose result would change within 20 ms anyway.
- **Mosh** adapts its frame rate specifically so it never fills the network
  queue, and transmits a diff against the last acknowledged screen state rather
  than every intermediate byte.
- **LLM streaming practice** converges on "flush every N tokens *or* T ms",
  with a bounded per-connection queue and an explicit slow-client policy.

Notably, the OpenCode web clients surveyed do **no** batching at all — SSE
events go straight into a query cache. That is viable for a desktop on
localhost; it is not viable over a mesh link to a phone.

---

## 2. Decision

Coalesce streaming chunk text at **`httpagent.session.Emit`**
(`internal/provider/httpagent/session.go:1052`), the single seam through which
every dialect event passes on its way to the daemon.

Policy lives in a new dependency-free package **`internal/chunkbuf`**: a pure
state machine with no goroutines, no timers and no internal mutex. The session
owns the timer and the lock.

### 2.1 Why this seam

`Emit` is upstream of *everything*: the 256-cap provider channel
(`session.go:306`), the history ring, the persistence debounce, and the WS
fan-out. One change therefore fixes frame count, ring eviction, manager lock
traffic, and the chunk-drop path together.

| | `httpagent.session.Emit` | `session.Manager.pump` | `ws.Server` |
|---|---|---|---|
| Relieves the 256-cap channel — the only place text is *actually dropped* today | yes | no (downstream) | no |
| Fixes ring eviction / `session.history` truncation (§1.3) | yes | yes | no |
| Fixes `m.mu` global write-lock traffic | yes | partly | no |
| Fixes frame count and `json.Marshal` cost | yes | yes | yes |
| `Seq` semantics | one merged event, one seq, stamped normally | must hold text outside the ring | merging post-seq breaks client gap logic |
| Testable without a live engine | yes | needs Manager + fake | needs a WS server |

`Manager.pump` was rejected on correctness: to coalesce there you must hold text
*outside* the ring while pending, which opens a window where a reconnecting
client's `session.history` / `since_seq` response
(`docs/protocol-v1.md`) is missing the tail the daemon still holds. The ring is
the durable contract; nothing may buffer between it and the client.

`ws.Server` was rejected because it leaves §1.3, the manager lock, and the
channel drop untouched — three of the four problems.

### 2.2 Shape: single run, leading edge, control boundaries

- **One run at a time.** The buffer holds pending text for exactly one
  `(type, session, agent session, replay)` run. A type switch flushes the
  previous run first, so byte order out always equals byte order in.
- **Leading edge.** The first chunk after any boundary is emitted verbatim with
  nothing buffered, so **time-to-first-token is unchanged byte-for-byte**.
- **Control events are boundaries.** Pending text flushes ahead of them,
  blocking, so `turn_complete` can never precede the text it terminates.
- **Non-control, non-chunk events pass through without draining.**
  `usage_update` and `available_commands` do not fragment a run. Treating them
  as boundaries would defeat the coalescer entirely — that is precisely the
  client-side pathology of §1.1, and it must not be reproduced on the daemon.
- The boundary set is **derived from `event.IsControl`**
  (`internal/event/event.go:79-112`) rather than re-listed, so the delivery
  contract and the coalescing contract cannot drift.
- Flush triggers: window elapsed, `maxPendingChunkBytes` reached, a boundary
  event, or session close.

### 2.3 Ordering improvement over the acpagent precedent

`acpagent.session` already coalesces (`internal/provider/acpagent/session.go:756-827`),
but only *on backpressure*, and `drainCoalescedLocked` (`:806-826`) flushes in a
fixed order — assistant text, then thoughts — which **reorders** interleaved
reasoning and reply text within a window. The single-run design here cannot do
that: a type switch flushes first. This is a correctness improvement, and it is
the direction the eventual unification should take.

Unifying acpagent onto `chunkbuf` is an explicit **follow-up, not a
prerequisite**. Its coalescer works, grok regressions are expensive, and the two
changes should not share a blast radius.

### 2.4 Tunables

```go
// internal/provider/httpagent/session.go
const defaultStreamCoalesce = 80 * time.Millisecond
const maxPendingChunkBytes  = 8 << 10
var   chunkRetryDelay       = 50 * time.Millisecond
```

80 ms caps mid-stream updates at ~12/s: comfortably inside the mobile client's
32 ms batch window and well inside its 120 ms markdown throttle tier, so finer
updates were being coalesced away by the phone regardless.

`maxPendingChunkBytes` is a **cap, not a knob**. At normal token rates
(~200 B/s) 8 KiB is ~40 seconds, so it never fires in steady state; it bounds
catch-up bursts (MADR 0014 resync tails, message-log replay).

`Config.StreamCoalesce` is `*time.Duration`, mirroring the `SessionTree *bool`
idiom (`httpagent.go:46`), because `0` must mean "coalescing off" rather than
"unset".

The operator-facing key is `providers.opencode.stream_coalesce_ms`, a plain
`int` matching the existing `permission_timeout_seconds` /
`turn_stall_notice_seconds` shape — viper always supplies the 80 ms default, so
a pointer would buy nothing at that layer, and `0` carries the same
"disabled" meaning it does for those keys. `daemon.go` takes its address so the
transport can tell an explicit 0 from an unset `Config`, exactly as it already
does for `SessionTree`. Validation bounds it to `0..1000`: past about a second
the stream stops reading as live typing, and nothing downstream would flag it.

### 2.5 Locking

`emitMu` is held **across the send**, not merely across the buffer operation.
Two goroutines call `Emit`: the shared SSE reader (`provider.go:546`) and the
MADR 0014 resync goroutine (`provider.go:609`). Without that, a timer flush can
be overtaken by a concurrent boundary emit, putting `turn_complete` ahead of
text.

Lock order is `s.mu -> s.emitMu`, never the reverse; nothing held under
`emitMu` takes `s.mu` or the dialect's own mutex. `emitTextCatchUp`
(`opencode/http.go:967-994`) holds the dialect mutex across `Emit`, but only
ever emits chunks, and the chunk path never blocks — so MADR 0014's bounded-hold
requirement still holds.

The flush timer is `time.AfterFunc`, **armed once per run and never `Reset`**.
Resetting per event is exactly why `historyPersistDebounce`
(`manager.go:1351`) never fires under a continuous stream.

### 2.6 Backpressure

A chunk flush whose non-blocking send fails is **`Unflush`ed and retried**, not
dropped. This is strictly better than today's
`default: log.Warn("dropping event; slow consumer")` (`session.go:1075`), which
loses reply text outright. A bounded-growth guard drops the oldest half of a run
with one loud warning above `4 * maxPendingChunkBytes`, for the case where the
pump is genuinely dead.

---

## 3. Consequences

- Mid-stream frame rate is capped at ~12/s per session regardless of token
  rate. A 40 s reply with 12 tool calls emits ~500 chunk frames instead of
  ~2000, and the framing overhead per byte of text drops by roughly the same
  factor.
- The history ring holds a real conversation again (§1.3), so `session.history`
  on cold reopen returns the whole reply rather than its tail.
- Reply text is no longer dropped under backpressure (§2.6).
- Time-to-first-token and end-of-turn latency are unchanged (§2.2).
- Interleaved reasoning/reply ordering is preserved exactly (§2.3).

### Closes MADR 0018 D1

D1 ("open-turn `StringBuffer` / chunk rope, materialize on throttle +
finalize") is **satisfied at ingest** and closed without building the rope.
`ChatItem` (`chat_models.dart:103`) and `SessionTranscript` (`:266`) both have
`const` constructors; hanging mutable state off either would break `const`,
`transcript_cache.dart` serialization, the `chat_bubble.dart` widget-cache
identity checks, and every `expect(t.items.last.text, ...)` in the reducer
tests. The real cost driver was the per-event apply loop, not the model. With
80 ms coalescing upstream plus the phase-5 `_foldChunks` pass, `_appendChunk`
runs ~10–30 times per turn instead of ~2000.

---

## 4. Known limitations

- **Lost tail on abnormal close.** `Close` drains pending text best-effort
  before tearing down, but if the manager pump is already gone the tail is lost.
  Identical to today's behaviour; accepted.
- **One extra blocking send per boundary.** `streamOnce` dispatches
  synchronously on one goroutine for every session (`provider.go:543-549`), so a
  blocking emit stalls the engine-wide pump. This is already true for control
  events; the change adds the pending-text flush that precedes them. Bounded by
  `<-s.done`.
- **A dropped coalesced event costs more than a dropped token.** Mitigated by
  never dropping chunk flushes (§2.6), but the failure mode is chunkier if the
  guard ever fires.
- **`EmitReplay` is not coalesced** (phase 7). Its burst is O(parts), not
  O(tokens), and routing it through the buffer breaks
  `internal/provider/httpagent/expiry_test.go:240-282`, which asserts
  drop-oldest semantics using distinct per-event text that coalescing merges.
- **grok/acpagent is unchanged** (phase 8).

---

## 5. Alternatives rejected

- **Coalesce in `Manager.pump` or `ws.Server`** — §2.1.
- **A per-client outbound ring buffer in `ws.Server`** — addresses only the
  socket write, leaves ring eviction, the manager lock and the 256-cap channel
  drop in place, and turns the slow-client disconnect into silent text loss.
- **xterm.js-style ACK/watermark flow control from the phone** — the most
  robust answer for a congested mesh link, but it adds protocol surface and a
  new client state machine for a problem that a fixed 80 ms window solves. Worth
  revisiting only if measurement after phase 6 shows the window is still too
  fine on slow links.
- **Adaptive window keyed on `ws` queue depth** — better behaviour, but it
  couples the provider to the transport and is hard to test deterministically.
  Reconsider with real numbers.
- **A rope in the Flutter model (MADR 0018 D1 as written)** — §3.

---

## 6. Implementation phases

| Phase | Content | Status |
|---|---|---|
| 0 | This MADR + README docs-table row | done |
| 1 | `internal/chunkbuf` + tests | done |
| 2 | Wire into `httpagent.session`; window as a `Config` field with the package default | done |
| 3 | `providers.opencode.stream_coalesce_ms` config key + docs | done |
| 4 | Dedup `usage_update` and `session_status: running` in the dialect | pending |
| 5 | Flutter: widen `_isBatchableEvent`, add `_foldChunks`, `debugAppendChunkCount` | pending |
| 6 | `live_opencode` acceptance test; record measured before/after here | pending |
| 7 | *(optional)* route `EmitReplay` through the buffer | pending |
| 8 | *(follow-up PR)* retire `acpagent.coalesced` in favour of `chunkbuf` | pending |

Phase 2 alone fixes the overload; the window ships behind a compile-time default
so phase 3 slipping costs nothing.

---

## 7. Verification

`internal/chunkbuf/buffer_test.go` — pure, no timing, no fakes: leading edge
returns verbatim; N chunks concatenate exactly with `Timestamp == since`;
crossing `maxBytes` returns the run from `Add`; a boundary returns
`[pendingText, boundary]` in that order with `blocking == true`; **a
`usage_update` returns `[usage]` and leaves the run intact** (the
anti-regression that pins §2.2); `assistant, thought, assistant` yields three
events in original order; differing `AgentSessionID`/`Replay` forces a flush;
`Unflush` round-trips including prepend order across two retries.

`internal/provider/httpagent/coalesce_test.go` — short window: frames-per-turn
bound with byte-identical reassembly; TTFT readable without waiting a window;
ordering under two concurrent emitters (`-race`), mirroring the pump-vs-resync
split; zero text lost when the 256-cap channel is full; `Close` drains the tail;
`StreamCoalesce = 0` reproduces exact pre-change behaviour.

Phase 6 (`make live-opencode`, spends real tokens — acceptance only): prompt a
real engine for a long deterministic reply, assert reassembled text is complete
and ordered, chunk-frame count is within ~2× of `elapsed / window`,
time-to-first-chunk is within noise of the pre-change baseline, and
`session.history` after the turn contains the whole reply rather than a
truncated tail.

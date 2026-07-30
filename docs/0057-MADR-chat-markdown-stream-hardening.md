# MADR 0057: Chat session markdown streaming — cross-stack re-assessment

<!-- markdownlint-disable MD013 MD024 MD060 -->

- **Date:** 2026-07-30
- **Status:** **Proposed** — implementation plan locked in
  [0057-PLAN](0057-PLAN-chat-markdown-stream-hardening.md) (decisions D1–D11)
- **Baseline:** `74e3c49` (`master`, re-assessment post–MADR 0056 phases 0–7)
- **Scope:** End-to-end path from provider stream → mcremote protocol → Android
  transcript → session-chat markdown rendering. Providers: **Grok**, **OpenCode**,
  **Codex**, **Goose**. Lenses: **throughput**, **stability**, **hardening**,
  **caching**, **buffering**.
- **Related:**
  [protocol v1](protocol-v1.md),
  [MADR 0018](0018-MADR-mobile-chat-performance-action-plan.md),
  [MADR 0024](0024-MADR-stream-coalescing.md),
  [MADR 0027](0027-MADR-opencode-streaming-rendering.md),
  [MADR 0034](0034-MADR-opencode-tool-stream-fidelity.md),
  [MADR 0042](0042-MADR-android-app-remediation.md),
  [MADR 0051](0051-MADR-auto-approve-chat-noise.md),
  [MADR 0056](0056-MADR-mcremote-android-protocol-stack-audit.md),
  [chat-performance.md](chat-performance.md)
- **Out of scope:** Pairing/auth redesign, FCM push product, non-chat surfaces,
  iOS, replacing the WebSocket control plane.

## 1. Decision

Accept this document as the current backlog for **chat markdown + stream
throughput** across mcremote and the Android client. Prefer work that:

1. Unifies **provider emit coalescing** so Grok matches Goose/OpenCode/Codex.
2. Keeps **one markdown engine** for stream and finalize (already true after
   0056 Phase 7) while bounding **re-parse cost** on long replies.
3. Closes residual **raw-marker flash** and **doc/code drift** without
   reintroducing multi-engine swaps.
4. Tightens **buffer budgets** (history ring semantics, tool lanes, cache
   bytes) so a single high-rate provider cannot starve the phone or the ring.

No P0 security defect was found on the markdown path itself. Highest product
risk is **asymmetric stream pressure by provider** plus **full-document
markdown re-parse** under sustained long replies.

## 2. Executive assessment

The stack is substantially stronger than the pre-0024/0018 baseline:

| Layer | Strength | Residual risk |
|---|---|---|
| Daemon text coalesce (`chunkbuf`) | Leading-edge + control boundaries + unflush | Not used by Grok/acpagent |
| Tool-update lane | OpenCode only (`WithToolLane`) | Codex/Goose tool dumps still per-delta |
| Phone batch + fold | 32 ms + adjacent text/tool fold | Discrete status still immediate (by design) |
| Markdown engine | Single `MarkdownBody` stream+finalize | Full re-parse of whole bubble each frame |
| Stream closer | Closes `` ` `` / `**` / fences | Open `*`, `~~`, links still flash |
| History / cache | Ring 800 + phone last-N prefs | Event-token ring; 1s debounce + **5s max latency** (0056 Phase 5 shipped; full journal optional) |
| Reconnect resync | `SessionSynchronizer` (0056 H-1) | Multi-page / unstamped gap edge cases remain |

**Markdown is no longer the primary frame-rate killer** for OpenCode-class
providers (chunkbuf + phone fold absorbed that). The remaining pain is:

- **Grok** still emits roughly one control-plane event per healthy token when
  the consumer keeps up (backpressure coalesce only).
- **Long assistant replies** re-parse the entire growing string through
  `flutter_markdown_plus` on every scheduled stream frame.
- **Cross-provider tool stream** fidelity and frame cost still diverge.

## 3. End-to-end pipeline (verified)

```text
Provider engine
  Grok     → ACP stdio  → acpagent.emit   (backpressure map coalesce)
  Goose    → ACP HTTP   → acphttp.emit    (chunkbuf 80ms / 8 KiB)
  OpenCode → HTTP SSE   → httpagent.Emit  (chunkbuf 80ms / 8 KiB + tool lane)
  Codex    → app-server → codex session   (chunkbuf 80ms / 8 KiB)
        │
        ▼
 event.Event {assistant_message_chunk | thought_chunk | tool_* | …}
        │
        ▼
 session.Manager pump  → history ring (cap 800)  → WS fan-out
        │                 durable debounce (~1s quiet)
        ▼
 Android McremoteClient  → TranscriptsNotifier
        │   batchable: 32ms window
        │   _foldChunks: adjacent text merge + tool replace
        │   applySessionEvent → ChatItem text (clip 100k)
        ▼
 TranscriptPane / ChatBubble
        │   row memo when only last non-tool text grew
        ▼
 _AssistantMarkdown
        │   frame-aligned dirty flag
        │   scroll-pause + scroll-end flush (0056 L-3)
        │   bufferStreamingMarkdown while streaming
        │   MarkdownBody (stream + finalize, single engine)
        ▼
 Flutter layout / raster
```

### 3.1 Key constants (live)

| Constant | Value | Location |
|---|---|---|
| Default stream coalesce | 80 ms | `httpagent` / `acphttp` / `codex` configs |
| Max coalesce window | 1000 ms | `config.maxStreamCoalesceMs` |
| Coalesce size force-flush | 8 KiB | `maxPendingChunkBytes` |
| Unflush growth guard | 4 × maxBytes | `chunkbuf.growthFactor` |
| Provider event channel | 256 (typical) | provider sessions |
| WS outbound queue | 1024 frames | `ws.outboundQueueLen` |
| History ring | 800 events | `historyBufferCap` |
| History soft page | ~512 KiB estimate | `historyMaxResponseBytes` |
| Phone batch window | 32 ms | `kTranscriptBatchWindow` |
| Phone item cap | 800 items | `kMaxTranscriptItems` |
| Phone item text clip | 100_000 chars | `kMaxItemTextChars` |
| Transcript prefs cache | 150 items / 12 sessions / ~400 KiB | `TranscriptCache` |
| Client request frame cap | 1 MiB exact | `kMaxClientFrameBytes` |
| Show-more clamp | 6_000 chars | `kAssistantShowMoreChars` |
| Legacy long-stream cliff | 4_000 chars | `kMaxStreamingMarkdownChars` (**unused path**) |

## 4. Already shipped (do not re-solve)

Treat as done unless regression tests fail:

- Daemon timed text coalesce at the emit seam (MADR 0024) for OpenCode, Goose,
  Codex.
- Leading-edge first token; control boundaries flush text ahead of
  `turn_complete` / permissions.
- Chunk unflush instead of silent drop under backpressure (`chunkbuf.Unflush`).
- OpenCode tool-update lane + phone tool replace-fold (MADR 0034 / 0042).
- Phone 32 ms batch; `usage_update` batchable and neutral in fold.
- OpenCode `session_status: running` dedup (`emitStatus` / `runningSent`).
- Reverse `ListView`, near-bottom follow, row memo, `ValueKey(seq)`.
- Single `MarkdownBody` for stream and finalize (0056 Phase 7) — mono cliff
  and isolate subset engine **removed from the live path**.
- Frame-aligned stream render; scroll suppress + reschedule on scroll end.
- Phone last-N transcript cache with serialized index (process-death polish).
- Connection-scoped `SessionSynchronizer` for post-reconnect history.

## 5. Provider matrix — coalescing and stream pressure

| Provider | Adapter | Coalescer | Tool lane | Config | Healthy-path text frame rate |
|---|---|---|---|---|---|
| **Grok** | `acpagent` (stdio ACP) | **Backpressure-only** map; no timer | No | *(none)* | ~1 event / token when pump keeps up |
| **Goose** | `acphttp` | `chunkbuf` 80 ms / 8 KiB | **No** | `providers.goose.stream_coalesce_ms` | ~12.5 text frames/s mid-run |
| **OpenCode** | `httpagent` + dialect | `chunkbuf` 80 ms / 8 KiB | **Yes** | `providers.opencode.stream_coalesce_ms` | ~12.5 text + coalesced tool updates |
| **Codex** | `codex` session | `chunkbuf` 80 ms / 8 KiB | **No** | `providers.codex.stream_coalesce_ms` | ~12.5 text; tools pass-through |

### 5.1 Why Grok is the outlier

`acpagent.session.emit` only folds assistant/thought text when the session
event channel is full. On a healthy path the channel never fills, so each
ACP chunk becomes one `assistant_message_chunk` into the manager ring, one WS
frame, and one phone decode — **pre-0024 OpenCode economics**, for the default
provider many users run first.

There is **no** `providers.grok.stream_coalesce_ms` (or acpagent-wide) knob.
Grok sub-agent content is correctly suppressed from the parent transcript
(MADR 0051 D6), which helps volume, but the parent token stream remains untimed.

### 5.2 Tool streams

OpenCode is the only path with daemon-side tool-lane supersede
(`chunkbuf.WithToolLane`). Codex and Goose still emit every non-terminal
`tool_call_update`. The phone folds adjacent same-id updates inside the 32 ms
window, but:

- each still pays marshal + WS + decode before fold;
- updates separated by text or other tools do not fold;
- large tool output strings are re-clipped and copied per applied update.

## 6. Findings

Severity:

| Sev | Meaning |
|---|---|
| **H** | Throughput/stability defect that makes a provider or long turn unusable, or causes permanent transcript divergence |
| **M** | Clear jank, waste, asymmetry, or correctness cliff under normal agent sessions |
| **L** | Drift, dead code, polish, or low-probability hardening |

| ID | Sev | Area | Finding |
|---|---|---|---|
| **H-1** | High | Provider / throughput | Grok (`acpagent`) has no timed stream coalesce; healthy path ≈ one WS event per token |
| **H-2** | High | Markdown / FPS | Every stream frame re-parses the **entire** assistant string with `MarkdownBody` (O(message length) per frame) |
| **M-1** | Medium | Markdown / UX | `bufferStreamingMarkdown` still only closes fences, inline `` ` ``, and `**` — open `*`, `~~`, `[links](` flash raw |
| **M-2** | Medium | Provider / throughput | Tool lane enabled only for OpenCode; Codex/Goose tool dumps remain high-rate |
| **M-3** | Medium | History / stability | History ring is **event-count** capped (800); Grok-scale token rings still evict conversation structure |
| **M-4** | Medium | Buffering / durability | Durable history still full-ring rewrite on flush; crash-loss now **bounded at 5s** (`historyMaxLatency`, 0056 Phase 5) — remaining work is journal/segment I/O cost, not unbounded quiet loss |
| **M-5** | Medium | Phone / GC | Open-turn text is still a full `String` on `ChatItem`; fold reduces applies, not store identity / allocation shape |
| **M-6** | Medium | Protocol / frames | History soft cap uses incomplete `approxEventBytes`; outbound events lack exact size budget (0056 M-1) |
| **M-7** | Medium | Caching | Phone prefs cache is best-effort JSON; large tails half-then-drop; no integrity/version beyond parse try/catch |
| **M-8** | Medium | Stability | Slow client: WS queue 1024 → disconnect; unflush growth discards oldest text after 32 KiB pending |
| **L-1** | Low | Drift | `kMaxStreamingMarkdownChars` + comments still describe a mono long-stream path that Phase 7 removed |
| **L-2** | Low | Dead code | `markdown_parser.dart` / `parseMarkdownOffMain` unused by UI after single-engine path |
| **L-3** | Low | Docs | MADR 0056 §§M-9…M-12 / L-3 partially stale vs Phase 7 implementation |
| **L-4** | Low | UX | Finalize reflow when synthetic closers disappear (stream buffered → raw final) |
| **L-5** | Low | Hardening | `MarkdownBody` link/image policy not explicitly fail-closed for unexpected schemes |
| **L-6** | Low | Provider | No shared metrics (frames/s, coalesce hit rate, unflush drops) exported for field diagnosis |

### 6.1 H-1 — Grok untimed stream

**Evidence:** `internal/provider/acpagent/session.go` `emit` +
`isHighFrequencyEvent`; config only wires `stream_coalesce_ms` for goose /
opencode / codex (`internal/config/config.go`, `load.go`).

**Impact:** On mesh + phone, Grok sessions recreate the original OpenCode
“chat cannot keep up” failure mode: history ring fills with tokens, manager
lock + marshal cost scales with token rate, phone batch helps but still pays
decode per frame.

**Direction:** Port `chunkbuf` (+ timer) into `acpagent` with the same
leading-edge / boundary / unflush contract as `httpagent` and `acphttp`.
Expose `providers.grok.stream_coalesce_ms` (or a shared ACP default). Keep
backpressure map only as a secondary safety net if desired, or delete once
chunkbuf owns retries.

**Acceptance:** Live or synthetic Grok stream ≥ 200 tokens/s produces ≤ ~15
assistant frames/s mid-run at default 80 ms; first token latency within one
token of today; `turn_complete` never precedes text.

### 6.2 H-2 — Full-document markdown re-parse

**Evidence:** `_AssistantMarkdownState._render` always builds a new
`MarkdownBody(data: shown)` (`chat_bubble.dart`). Frame throttle bounds
**frequency**, not **work per render**. A 40 KB reply re-walks ~40 KB of GFM
on every dirty frame until finalize.

**Impact:** Sustained jank on mid/low-end Android once replies leave a few
kilobytes, independent of provider coalesce. Tests assert parse **count**
(`debugMarkdownParseCount`), not parse **cost**.

**Direction (pick one, prefer in order):**

1. **Closed-prefix cache:** parse only the suffix past the last stable block
   boundary; keep prefix widgets frozen (hard with `MarkdownBody` internals).
2. **Size-tiered stream schedule:** restore 120/200/320 ms tiers by length
   *on top of* frame alignment (Phase 7 removed wall timers; reintroduce as
   minimum interval, not vsync replacement).
3. **Streaming plain + finalize full MD** only for replies above a high
   threshold (product regression risk — rejected once; only as last resort).
4. **Block-model isolate parser** revived *for both* stream and finalize with
   full GFM (tables/strike/images) so engines never diverge (heavy).

**Acceptance:** For a 20 KB streaming reply at 12 frames/s UI updates, main
isolate markdown parse time stays under a measured budget (device-class
matrix); no engine swap; `debugMarkdownParseCount` still bounded.

### 6.3 M-1 — Stream closer subset

Unchanged from 0056 M-10 / 0027. Heuristic closer is intentional and cheap;
expanding it toward full GFM is fragile. Prefer H-2 path that always feeds a
complete parser with well-formed partial input (or accept residual flash for
rare open italic/strike/link and document it).

Minimum cheap wins if H-2 is deferred:

- close single `*` / `_` when not list markers (careful with `* item`);
- close `~~` pairs;
- drop incomplete `[text](partial` to plain text for stream only.

### 6.4 M-2 — Tool lane asymmetry

Enable `WithToolLane` for Codex and Goose (`acphttp` / `codex` `chunkBuffer`
constructors). Shared tests already live in `chunkbuf`. Measure Codex item
streams and Goose tool updates before defaulting — opt-in flag first if
behavior differs.

### 6.5 M-3 — Event ring vs “message” ring

Even with 80 ms coalesce, a long multi-tool turn still multiplies events
(status, tools, usage, plan, subagents). Under Grok H-1, the ring is almost
pure tokens. Options:

- Prefer coalesce (H-1) before raising cap.
- Optional **text compaction on ring insert**: merge adjacent same-type chunks
  already in the ring (careful with seq contracts).
- Page/history APIs that return **transcript items** rather than raw events
  for cold open (larger protocol change).

### 6.5a M-4 — Durability (corrected)

MADR 0056 described unbounded debounce under continuous stream. Current code
tracks `historyDirtySince` and force-flushes when `historyMaxLatency` (5s) is
exceeded (`internal/session/manager.go`). The residual issue is **write cost**
(full ring marshal/fsync every flush), not infinite postponement. Treat full
append journal as optional Phase E, not a High.

### 6.6 M-5 — Phone text storage

`_foldChunks` already concatenates within a batch. Per flush, `_appendChunk`
still materializes a new full string. For multi-second streams that is fine;
for multi-100 KB tails under H-2 it doubles pressure. Open-turn
`StringBuffer` on the notifier (materialize into `ChatItem` only when
publishing) remains the 0018 D1 ideal if allocation profiles demand it.

### 6.7 L-1 / L-2 — Drift and dead code

- Remove or re-purpose `kMaxStreamingMarkdownChars` comments/tests that imply
  a mono cliff (tests already assert MarkdownBody past 4k — good; constants
  and dartdocs should match).
- Either delete `markdown_parser.dart` + tests or revive it under H-2 option 4
  with full GFM parity gates.
- Annotate 0056 M-9…M-12 as **superseded by Phase 7** for engine swap / mono
  cliff; keep M-10 as open.

### 6.8 L-5 — Link / media hardening

Confirm `MarkdownBody` / `flutter_markdown_plus` does not auto-navigate
unexpected schemes; restrict to `https`/`http`/`mailto` or disable link tap
while streaming. Images in assistant MD (if ever rendered) should not fetch
arbitrary URLs without policy. Low urgency if taps are inert today — verify
and pin with a widget test.

## 7. Caching inventory

| Cache | Owner | What | Bound | Weakness |
|---|---|---|---|---|
| `chunkbuf` run | Provider session | Pending text (+ tools on OpenCode) | time + 8 KiB + growth×4 | Grok missing; tool lane incomplete |
| Session events chan | Provider session | In-flight events | ~256 | Full → unflush / drop telemetry |
| History ring | `session.Manager` | Replay + resync source | 800 events | Token-shaped under high rate |
| Durable history file | `session.Store` | Snapshot of ring | 800 events | Debounce crash window |
| WS outbound | `ws.client` | Serialized frames | 1024 | Disconnect, not throttle source |
| Phone `_pending` | `TranscriptsNotifier` | Batchable events | per session list | Unbounded if timer starved (rare) |
| Phone transcript RAM | Notifier | `ChatItem` list | 800 items / 100k chars | Byte-unaware |
| Phone prefs cache | `TranscriptCache` | Last-N JSON | 150×12 / ~400 KiB | Half-drop; no hash |
| Markdown widget | `_AssistantMarkdown` | Cached `Widget? _built` | 1 per bubble | Invalid = full reparse |
| Markdown style sheet | same | Theme-keyed sheet | 1 | OK |
| Row memo | `TranscriptPane` | Last rows when tail text only | structural | Tool growth forces O(n) rebuild |
| Isolate MD parser | *(dead)* | Parsed blocks | — | Unused |

### 7.1 Buffering principles (recommended policy)

1. **Coalesce before history** — anything that multiplies events must sit
   upstream of the ring (already true for chunkbuf paths).
2. **Never buffer between ring and client** — reconnect contract depends on
   seq-stamped ring membership (0024 decision; keep).
3. **Phone batches presentation, not truth** — 32 ms window is UI; history
   resync remains authoritative.
4. **Prefer supersede over append** for replace-semantics events (tools,
   usage, plan, approvals) — done in several places; extend tool lane to all
   providers.
5. **One render engine** — never again fork stream/finalize markdown paths.

## 8. Throughput model (order of magnitude)

Assume mid-run assistant text only, healthy links:

| Stage | Grok today | OpenCode/Goose/Codex @ 80 ms |
|---|---|---|
| Provider → daemon events | ~token rate (e.g. 30–80/s) | ~12.5/s (+ leading edge) |
| Manager lock + ring append | per event | per coalesced event |
| WS frames / client | per event | per coalesced event |
| Phone decode | per frame | per frame |
| Notifier commits | ≤ ~30/s batchable | ≤ ~30/s but fewer events in batch |
| Markdown parses | ≤ 1/frame while dirty | same, but less dirty if fewer commits |

**Multiplier:** Moving Grok onto chunkbuf is roughly a **3–6×** reduction in
control-plane frames for text-heavy turns, before any markdown work.

Tool-heavy OpenCode turns already benefit from the tool lane; Codex/Goose
tool turns may still spike control events.

## 9. Stability notes (markdown-adjacent)

| Scenario | Behavior today | Risk |
|---|---|---|
| Slow phone | Unflush holds text; WS may disconnect at 1024 | Tail loss after growth guard; user sees disconnect |
| Continuous stream crash | 1s debounce + 5s max latency force flush | Crash-loss ≤ ~5s of tail (0056 Phase 5); full-ring rewrite cost remains |
| Reconnect mid-stream | Synchronizer + gap flags | Incomplete multi-page / unstamped history still open |
| Scroll while streaming | Render paused; flush on scroll end | OK after L-3 fix |
| Finalize | Immediate full MD, selectable on | Possible layout reflow (L-4) |
| Process death | Prefs cache last-N; host ring truth | Large single item may fail cache write |

## 10. Recommended work order

| Phase | Items | Goal |
|---|---|---|
| **A** | H-1: `chunkbuf` in `acpagent` + grok config; parity tests | Kill provider asymmetry |
| **B** | M-2: tool lane on Codex + Goose; provider tests | Uniform tool frame cost |
| **C** | L-1, L-2, L-3: constant/doc/dead-code cleanup; 0056 supersession notes | Stop false cliffs in docs/tests |
| **D** | H-2 + M-1: size-tier stream interval and/or incremental strategy; closer gaps only if cheap | Long-reply FPS + less raw flash |
| **E** | M-3, M-4, M-6: ring semantics / durability / exact frames | Stability under load |
| **F** | M-5, M-7, L-5, L-6: phone rope, cache integrity, link policy, metrics | Hardening + observability |

Phase A is the highest ROI for real users on Grok. Phase D is the highest ROI
once all providers are time-coalesced.

## 11. Locked decisions (proposed)

Owner should confirm before implementation:

| # | Topic | Proposal |
|---|---|---|
| **D1** | Grok coalesce | **Adopt `chunkbuf` in `acpagent`**, default 80 ms, config `providers.grok.stream_coalesce_ms` (0 disables) |
| **D2** | Tool lane default | **On** for OpenCode (already); **on** for Codex and Goose after green parity tests |
| **D3** | Markdown engines | **Keep single `MarkdownBody`**; no reintroduction of mono/isolate stream forks |
| **D4** | Long-stream cost | Prefer **minimum interval by size** + frame alignment over engine swap |
| **D5** | Stream closer | Keep heuristic; expand only for safe constructs; full GFM is the parser’s job |
| **D6** | History model | Keep event ring for v1; fix pressure via coalesce before inventing item-ring protocol |
| **D7** | Metrics | Add daemon counters: coalesce flushes, unflush drops, frames/s per session (log/debug endpoint) |

## 12. Test plan (markdown / stream focused)

1. **Grok coalesce:** synthetic high-rate assistant chunks → frame bound +
   text equality + boundary order (`turn_complete` last).
2. **Cross-provider:** same scripted token stream through grok/opencode/codex/goose
   adapters → comparable event counts at equal windows.
3. **Tool lane:** non-terminal updates superseded; terminal never delayed past
   window without delivery.
4. **Markdown:** stream open `**` / fence / `` ` `` never show raw closers;
   open `*`/`~~` documented or fixed; finalize matches full GFM golden files
   (tables, lists, code).
5. **Long reply:** 20 KB stream — parse count and (where measurable) frame
   budget; no mono path; show-more only when finalized.
6. **Regression:** existing `streaming_markdown_test.dart`,
   `transcript_ingest_test.dart`, `chunkbuf_test.go`, provider coalesce tests
   stay green.
7. **Cache:** prefs save under 400 KiB half-drop; sealedTail prevents merge into
   restored bubble.

## 13. Compatibility

- Protocol event types and seq semantics **unchanged** by phases A–D.
- Coalesce merges multiple tokens into one event → **one seq** for the merge
  (already the 0024 contract clients rely on).
- Disabling coalesce (`stream_coalesce_ms: 0`) remains the kill switch for
  debugging provider fidelity.
- Android clients older than fold/batch still function; they only see larger
  chunks (forward compatible).

## 14. Non-goals

- Replacing WebSocket with a binary frame codec for chat text.
- Host-side markdown rendering or HTML sanitization of assistant output for
  the phone.
- Infinite transcript archives.
- Matching desktop localhost OpenCode web UX frame rates over mesh.

## 15. Summary

The mcremote ↔ Android chat path is **architecturally sound** after stream
coalescing, phone batching, and the single-engine markdown pass. The largest
remaining gaps are **not new ideas** — they are **incomplete rollouts**:

1. Timed coalesce never reached **Grok/acpagent**.
2. Tool-lane coalesce never left **OpenCode**.
3. Markdown cost control still relies on **frame rate**, not **work-per-frame**.
4. Stream closer and docs still carry **pre–Phase 7 scars**.

Close those four and session-chat markdown streaming should be stable and
fast enough across all four providers without another protocol rewrite.

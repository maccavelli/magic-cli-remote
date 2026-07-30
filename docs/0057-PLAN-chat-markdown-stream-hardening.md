# MADR 0057 — Implementation plan: chat markdown + stream hardening

<!-- markdownlint-disable MD013 MD024 MD060 -->

Companion to
[MADR 0057](./0057-MADR-chat-markdown-stream-hardening.md).
Read that first for the problem framing and severity model. This document is
the **build order**: phase-sequenced, file-specific, acceptance-gated, and
grounded in the tree at baseline `74e3c49` (`master`, 2026-07-30).

- **Status:** **Ready to implement**
- **Date:** 2026-07-30
- **Baseline:** `74e3c49`
- **Scope:**
  - Go: `internal/chunkbuf`, `internal/provider/acpagent`, `internal/provider/acphttp`,
    `internal/provider/codex`, `internal/provider/httpagent` (reference only),
    `internal/config`, `internal/daemon`, `internal/session` (optional later phases)
  - Flutter: `apps/mobile/lib/data/chat/*`, `features/chat/chat_bubble.dart`,
    `state/transcripts_notifier.dart` (optional later), tests under `apps/mobile/test/`
  - Docs/examples: `docs/0057-MADR-*`, `docs/protocol-v1.md` only if wire changes,
    `configs/*.yaml`, `README.md` config tables
- **Standards:** [Go sessions](standards/go/session.md),
  [Go concurrency](standards/go/concurrency.md),
  [Go testing](standards/go/testing.md),
  [mobile networking](standards/mobile/networking.md),
  [Flutter](standards/mobile/flutter.md)
- **Related plans (do not re-open done work):**
  [0024 stream coalescing](0024-MADR-stream-coalescing.md) (shipped for OC/Goose/Codex),
  [0056 plan](0056-PLAN-mcremote-android-protocol-stack-remediation.md) phases 0–7
  (markdown single engine + max-latency history shipped)

---

## 0. MADR assessment (grounded in codebase)

### 0.1 Overall judgment

MADR 0057 is **directionally correct and high-ROI**. Independent re-check at
`74e3c49` confirms the primary throughput asymmetry (Grok/acpagent) and the
markdown re-parse cost model. Severity ranking is right for product impact:

| Priority | Finding | Why it ranks there |
|---|---|---|
| 1 | **H-1** Grok untimed stream | Re-creates pre-0024 OpenCode economics on the default path many users run |
| 2 | **H-2** Full-document MD reparse | Independent of provider; grows with reply length |
| 3 | **M-2** Tool lane OC-only | Codex/Goose tool dumps still pay per-delta frames |
| 4 | **M-1** Closer subset | Visible raw `*` / `~~` / links mid-stream |
| 5 | Drift L-1/L-2 | Cheap; prevents false “mono cliff” regressions |

No P0 security defect on the markdown path. Protocol rewrite is not required.

### 0.2 Finding validity matrix (live tree)

| ID | Still present? | Primary evidence at baseline |
|---|---|---|
| **H-1** | **Yes** | `acpagent.session.emit` only coalesces when `events` channel is full (`session.go` map `coalesced`); no timer. `acpagent.Config` has **no** `StreamCoalesce`. `GrokProviderConfig` has no `stream_coalesce_ms`. `daemon.acpAgentConfig` does not pass coalesce. Config load defaults only set `goose`/`opencode`/`codex` stream coalesce (`config/load.go`). |
| **H-2** | **Yes** | `_AssistantMarkdownState._render` always constructs new `MarkdownBody(data: shown)` and increments `debugMarkdownParseCount` (`chat_bubble.dart`). Frame throttle (`_dirty` + `addPostFrameCallback`) bounds **rate**, not **work**. |
| **M-1** | **Yes** | `bufferStreamingMarkdown` tracks only fence / inline `` ` `` / `**` (`streaming_markdown.dart`). Tests cover those only. |
| **M-2** | **Yes** | `httpagent` `chunkbuf.New(..., WithToolLane())`; `acphttp` and `codex` `chunkbuf.New(win, max)` **without** tool lane. |
| **M-3** | **Yes (mitigated)** | `historyBufferCap = 800` events still. Coalesce reduces pressure for OC/Goose/Codex; **Grok H-1 still token-fills the ring**. |
| **M-4** | **Partially fixed** | `historyPersistDebounce = 1s` **and** `historyMaxLatency = 5s` force flush (`manager.go`). 0057 originally overstated “unbounded” — corrected in MADR. Residual: full-ring rewrite cost, not infinite postpone. |
| **M-5** | **Yes (low urgency)** | `_appendChunk` materializes full string; `_foldChunks` reduces apply count. Acceptable until profiles demand open-turn rope. |
| **M-6** | **Yes** | `approxEventBytes` incomplete; no exact outbound event cap (0056 M-1; protocol phase, not markdown-critical). |
| **M-7** | **Yes (acceptable)** | `TranscriptCache` 400 KiB soft half-drop; sealedTail on load. Product-acceptable polish. |
| **M-8** | **Yes (by design)** | `outboundQueueLen = 1024`; `chunkbuf` growthFactor 4 unflush discard. Document + metrics more than redesign. |
| **L-1** | **Yes** | `kMaxStreamingMarkdownChars = 4000` still defined; comments on `debugMarkdownParseCount` still mention “plain long-stream path”; live path always `MarkdownBody` (Phase 7). |
| **L-2** | **Yes** | `markdown_parser.dart` / `parseMarkdownOffMain` only referenced from `markdown_parser_test.dart`, not production UI. |
| **L-3** | **Doc only** | 0056 M-9…M-12 text still describes multi-engine; **code fixed** in Phase 7. Plan updates 0056 note or leaves supersession pointer. |
| **L-4** | **Yes (minor)** | Finalize drops synthetic closers → possible reflow; single engine keeps widget type stable. |
| **L-5** | **Unverified / low** | No `onTapLink` in `chat_bubble.dart`; default package behavior needs a one-test pin, not a redesign. |
| **L-6** | **Yes** | No coalesce/unflush counters on sessions. |

### 0.3 Already shipped (do not re-solve)

| Work | Evidence |
|---|---|
| Timed text coalesce OC/Goose/Codex | `chunkbuf` + 80 ms defaults; config keys; `coalesce_test.go` / acphttp tests |
| OpenCode tool lane | `WithToolLane` in `httpagent` |
| Phone 32 ms batch + fold | `kTranscriptBatchWindow`, `_foldChunks` |
| OpenCode `running` status dedup | `opencode.emitStatus` / `runningSent` |
| Single markdown engine | `chat_bubble.dart` Phase 7 comments; `streaming_markdown_test` long-stream uses `MarkdownBody` |
| Scroll-end stream flush | `_pendingAfterScroll` + `ChatScrollActivity` listener |
| History max latency 5s | `historyMaxLatency` in `manager.go` |
| SessionSynchronizer | `session_synchronizer.dart` (0056 H-1) |

### 0.4 Corrections applied to MADR 0057

1. **M-4** reworded: crash-loss is **≤ ~5s**, not unbounded.
2. Executive table history row updated for max-latency.
3. Stability table continuous-stream row updated.

No other finding was false; H-1/H-2/M-1/M-2 remain the spine of this plan.

### 0.5 Gaps the MADR left for the plan to fill

1. **acpagent integration shape.** httpagent uses a separate `emitMu` + timer;
   acpagent today folds under `s.mu` and uses `deliver` with attach-time drop
   semantics. The plan must specify lock order and Replay preservation.
2. **Config placement.** Grok embeds `ACPProviderConfig` without
   `stream_coalesce_ms`. Plan puts the key on **GrokProviderConfig** (or shared
   ACP) and wires `acpAgentConfig` — prefer **Grok field first** so Goose’s
   existing HTTP key stays the source of truth for goose.
3. **Tool lane risk.** Enabling tool lane changes seq density and history
   content (fewer intermediate updates). Plan requires parity tests before
   default-on.
4. **H-2 solution choice.** Plan locks a concrete first step (size-tier min
   interval) before optional incremental AST work.
5. **M-6 / protocol framing** deferred to 0056 Phase 4 residual — not blocking
   markdown/stream throughput.

### 0.6 What must not regress

- Leading-edge first token (no delay on first chunk after boundary)
- Control boundary flush: text before `turn_complete` / permission / status
- Unflush never drops reply text until growth guard (and then logs)
- Replay flag preserved across coalesce during `session/load`
- Phone fold + 32 ms batch
- Single `MarkdownBody` engine (no mono / isolate stream fork)
- `make pre-add-check` / Dart format+analyze gates

---

## 1. Locked decisions for this plan

| # | Question | **Chosen** | Implication |
|---|---|---|---|
| **D1** | Grok coalesce | **Port `chunkbuf` into `acpagent`**, default 80 ms | Replace healthy-path per-token emit; keep kill switch `0` |
| **D2** | Where is the config key? | **`providers.grok.stream_coalesce_ms`** on `GrokProviderConfig` | Does not force every future ACP stdio agent; wire through `acpagent.Config.StreamCoalesce` |
| **D3** | Keep old backpressure map? | **Remove after chunkbuf owns path** | One mechanism only; existing backpressure tests rewrite against unflush |
| **D4** | Tool lane | **Enable on Codex + Goose after red/green tests** | Same `WithToolLane()` option; terminal tools never delayed past boundary rules |
| **D5** | Markdown engines | **Keep single `MarkdownBody` forever for stream+finalize** | No revive of mono cliff or isolate stream primary path |
| **D6** | Long-stream cost (H-2 first step) | **Size-tiered minimum interval** on top of frame alignment | 0–4k: frame; 4–16k: ≥120 ms; 16k+: ≥200 ms (tunable constants) |
| **D7** | Stream closer (M-1) | **Expand carefully** for `~~` and incomplete links; **defer** aggressive single-`*` (list ambiguity) | Prefer tests over clever heuristics |
| **D8** | Dead parser | **Delete production dependency**; keep or delete file in Phase C | Prefer delete `markdown_parser.dart` + test if unused |
| **D9** | History ring / M-6 | **Out of scope for A–D**; optional Phase E | Coalesce first |
| **D10** | Phone StringBuffer rope | **Optional Phase F** only if profiles demand | Not on critical path after H-1 |
| **D11** | Metrics | **slog counters / debug fields** on unflush drop + coalesce flush | No new protocol surface in v1 |

---

## 2. Ground rules

1. **One phase → one logical commit** unless the phase table says “split ok”.
2. **Go pre-add:** `make pre-add-check` / `./scripts/go-precheck.sh` on touched
   packages before any `git add` of `.go`.
3. **Dart:** `dart format` on touched files; `cd apps/mobile && dart analyze`;
   phase’s `flutter test` list green.
4. **`git commit` without `-m`** (prepare-commit-msg hook).
5. Prefer **symbol names** over line numbers in patches (lines drift).
6. **Red then green** for H-1 and M-2: land a failing or bounding test first
   when practical; always leave a regression that would fail if coalesce is
   disabled accidentally on Grok.
7. **Do not change protocol event type names or seq meaning.** Coalesce still
   means one seq per emitted (merged) event.
8. **Kill switch:** `stream_coalesce_ms: 0` must restore one-event-per-chunk
   for debugging fidelity (matches OC/Goose/Codex).

### Verification commands (module root)

```bash
# Go — provider + config + chunkbuf
make pre-add-check
go test -race ./internal/chunkbuf/... \
  ./internal/provider/acpagent/... \
  ./internal/provider/acphttp/... \
  ./internal/provider/codex/... \
  ./internal/provider/httpagent/... \
  ./internal/config/... \
  ./internal/daemon/...

# Mobile
cd apps/mobile
dart format --output=none --set-exit-if-changed lib test
dart analyze
flutter test test/streaming_markdown_test.dart \
  test/chat_render_test.dart \
  test/transcript_ingest_test.dart \
  test/transcript_reducer_test.dart
```

---

## 3. File map

### Go (Phase A–B)

| Area | Path |
|---|---|
| Coalescer | `internal/chunkbuf/chunkbuf.go` (+ tests; reuse) |
| ACP stdio session | `internal/provider/acpagent/session.go` |
| ACP config | `internal/provider/acpagent/config.go` |
| ACP coalesce tests | `internal/provider/acpagent/session_coalesce_test.go`, `session_test.go` (**rewrite**) |
| ACP HTTP (Goose) | `internal/provider/acphttp/session.go` `chunkBuffer` |
| Codex session | `internal/provider/codex/session.go` `chunkBuffer` |
| Reference impl | `internal/provider/httpagent/session.go` (`Emit`, `armFlush`, `onFlushTimer`) |
| Daemon wiring | `internal/daemon/daemon.go` (`acpAgentConfig` or grok-specific builder) |
| Config types | `internal/config/config.go` (`GrokProviderConfig`) |
| Config defaults/validate | `internal/config/load.go`, `config.go` validate, `config_test.go` |
| Examples | `configs/config.example.yaml`, `config.prod.example.yaml`, `config.mesh-grok.yaml` |
| Service defaults | `internal/cli/service/defaults_mcremote.yaml` |
| README tables | `README.md` provider config rows |

### Flutter (Phase C–D)

| Area | Path |
|---|---|
| Stream closer | `apps/mobile/lib/data/chat/streaming_markdown.dart` |
| Constants | `apps/mobile/lib/data/chat/chat_models.dart` |
| Bubble / MD | `apps/mobile/lib/features/chat/chat_bubble.dart` |
| Dead parser | `apps/mobile/lib/data/chat/markdown_parser.dart` (+ test) |
| Tests | `streaming_markdown_test.dart`, `chat_render_test.dart` |

### Optional later

| Area | Path |
|---|---|
| History journal | `internal/session/manager.go`, `store.go` |
| Exact frames | `internal/ws/server.go`, `approxEventBytes` |
| Phone rope | `transcripts_notifier.dart`, `transcript_reducer.dart` |
| Metrics | provider session `noteUnflush` + slog |

---

## 4. Phase overview

| Phase | Theme | Findings | Est. | Depends |
|---|---|---|---|---|
| **0** | Guardrails + measurement tests | H-1, M-2, H-2 bounds | S | none |
| **A** | Grok/acpagent timed `chunkbuf` | **H-1** | M–L | 0 |
| **B** | Tool lane Codex + Goose | **M-2** | S–M | 0 (parallel A) |
| **C** | Drift / dead code cleanup | L-1, L-2, L-3 docs | S | none (parallel) |
| **D** | Markdown FPS + closer | **H-2**, **M-1**, L-4, L-5 | M | C preferred |
| **E** | Ring pressure + durability I/O (optional) | M-3 residual, M-4 residual, M-6 | M–L | A |
| **F** | Phone GC + metrics + cache polish (optional) | M-5, M-7, M-8, L-6 | S–M | A, D |

**Recommended serial spine:** `0 → A → D`  
**Parallel with A:** `B`, `C`  
**Later / opportunistic:** `E`, `F`

```text
0 ──► A ──► D ──► F
 │     │
 ├──► B │
 └──► C ─┘
       │
       └──► E (optional)
```

---

## Phase 0 — Guardrails (tests first)

**Goal.** Pin current behavior and give Phase A/B a red→green target without
shipping production changes (except pure test helpers).

### 0.1 H-1 — acpagent healthy path is one event per chunk

| | |
|---|---|
| **File** | `internal/provider/acpagent/session_coalesce_test.go` (extend) or new `session_stream_coalesce_test.go` |
| **Drive** | Build session with non-blocking consumer (drain events in goroutine). Emit N assistant chunks with no channel pressure, then a `turn_complete`. |
| **Assert (today)** | N assistant events received (document as pre-A baseline) **or** if Phase A lands in same PR, assert ≤ bound. Preferred: two tests — `TestHealthyPathNoTimedCoalesce` marked for deletion after A, and `TestTimedCoalesceBoundsFrames` written first as `t.Skip` or failing until A. |
| **Assert (after A)** | With default 80 ms window: mid-run frame count ≤ `ceil(duration/win)+O(1)`; concatenated text equals input; first chunk latency immediate; `turn_complete` after full text. |

Reference for shape: `internal/provider/httpagent/coalesce_test.go`
(`TestCoalesceBoundsFramesAndPreservesText`, `TestCoalesceKeepsTimeToFirstToken`,
`TestCoalesceFlushesTailBeforeTurnComplete`).

### 0.2 M-2 — tool updates not coalesced on acphttp/codex

| | |
|---|---|
| **Files** | `internal/provider/acphttp/session_test.go`, `internal/provider/codex/*_test.go` or shared chunkbuf wiring test |
| **Drive** | Emit many non-terminal `tool_call_update` same ToolID inside one window |
| **Assert (today)** | One event per update (or document count) |
| **Assert (after B)** | One superseded update per tool id per window; terminal status flushes immediately |

### 0.3 H-2 — parse count bound (already exists; extend)

| | |
|---|---|
| **File** | `apps/mobile/test/streaming_markdown_test.dart` |
| **Extend** | Long growing stream: after Phase D, assert minimum wall time between parses when length > tier thresholds (or inject fake clock if pure unit is hard — may stay widget-level with `tester.pump(Duration)`) |

### 0.4 Phase 0 gate

```bash
go test -race ./internal/provider/acpagent/ -count=1
go test -race ./internal/provider/httpagent/ -run Coalesce -count=1
cd apps/mobile && flutter test test/streaming_markdown_test.dart
```

**Commit shape:** test-only preferred; may merge 0+A if smaller review surface.

---

## Phase A — Grok / acpagent timed stream coalesce (H-1)

**Goal.** Healthy-path Grok streams match OpenCode/Goose/Codex economics:
~one assistant/thought frame per `stream_coalesce_ms` mid-run, leading edge
intact, boundaries flush text first, unflush under backpressure.

### A.1 Config surface

**Files:**

- `internal/config/config.go` — add to `GrokProviderConfig`:

  ```go
  // StreamCoalesceMs holds assistant/thought text for merging (MADR 0024/0057).
  // 0 disables. Default 80. Max maxStreamCoalesceMs.
  StreamCoalesceMs int `mapstructure:"stream_coalesce_ms"`
  ```

- Defaults: `StreamCoalesceMs: 80` next to other Grok defaults.
- Validate: same `0..maxStreamCoalesceMs` as goose/opencode/codex.
- `internal/config/load.go` — `v.SetDefault("providers.grok.stream_coalesce_ms", …)`.
- `internal/config/config_test.go` — load/default/reject tests for grok key.
- YAML examples + `defaults_mcremote.yaml` + README provider table.

**Do not** put the key only on squashed `ACPProviderConfig` unless also
migrating goose (goose already has its own field for HTTP). Avoid double
keys on goose.

### A.2 `acpagent.Config`

**File:** `internal/provider/acpagent/config.go`

Add:

```go
// StreamCoalesce is how long assistant/thought text is held for merging.
// nil → default 80ms; explicit 0 → disabled.
StreamCoalesce *time.Duration
```

Helper `StreamCoalesceWindow()` mirroring `httpagent` / `acphttp`.

### A.3 Daemon wiring

**File:** `internal/daemon/daemon.go`

Grok construction today uses `acpAgentConfig(cfg.Providers.Grok.ACPProviderConfig)`
and separate grok fields (reasoning, sandbox, …). Extend the grok build path
to set:

```go
streamCoalesce := time.Duration(cfg.Providers.Grok.StreamCoalesceMs) * time.Millisecond
// … acpagent.Config{ …, StreamCoalesce: &streamCoalesce }
```

Use an **explicit pointer** so `0` means off (same comment as OpenCode).

If `acpAgentConfig` stays shared and pure ACP-only, either:

- add optional `StreamCoalesce` parameter, or
- set the field on the returned config in the grok branch after `acpAgentConfig`.

Prefer setting on the grok branch to avoid surprising future ACP agents.

### A.4 Session emit rewrite

**File:** `internal/provider/acpagent/session.go`

**Reference implementation:** `httpagent.session.Emit` + `armFlush` /
`onFlushTimer` / `chunkBuffer` / `trySend` / `noteUnflush` /
`drainChunks` on close.

**Structural changes:**

1. Add fields on `session` (mirror httpagent):

   - `chunks *chunkbuf.Buffer`
   - `emitMu sync.Mutex` — serializes Add/Drain/Unflush **and delivery of
     returned events** (chunkbuf package doc requirement)
   - `flushMu sync.Mutex` + `flushTimer *time.Timer`
   - constants: `maxPendingChunkBytes = 8 << 10`, `chunkRetryDelay` as httpagent

2. **Replace** `coalesced map[event.Type]coalescedChunk` healthy-path logic in
   `emit` with chunkbuf path:

   ```text
   prepareEvent (Replay, AgentSessionID rules)
   emitMu.Lock
   out, deadline, blocking := chunkBuffer().Add(ev)
   for each out:
     if blocking || IsControl → deliver(control)
     else if !trySend:
       if IsChunk → Unflush + reschedule
       else drop telemetry (log)
   emitMu.Unlock
   arm/stop flush timer from deadline
   ```

3. **Replay / loading:** `prepareEvent` already sets `Replay` when `s.loading`.
   chunkbuf mergeable compares Type/SessionID/AgentSessionID/Replay — ensure
   template carries Replay so load-time chunks do not merge with live (or
   incorrectly clear Replay). Cover with existing
   `TestCoalescedFlushPreservesReplay` rewritten.

4. **Boundaries:** control events already flush via `chunkbuf.Add` boundary
   path. Remove `drainCoalescedLocked` once unused.

5. **Lock order:** document `emitMu` vs `s.mu`. Prefer:

   - `prepareEvent` needs `s.loading` / `s.agentID` under `s.mu` **or** atomic
     snapshot before emitMu (today `emit` takes `s.mu` for closed check).
   - Pattern from current `emit`: check closed under `s.mu`, then release, then
     emitMu — avoid holding both while blocking on `events` channel.
   - Control `deliver` may block; must not hold `emitMu` during blocking send
     **or** must match httpagent (httpagent holds emitMu during blocking send
     on control — check carefully). **httpagent holds `emitMu` during
     blocking send.** acpagent’s `deliver` can block on `s.done`. Holding
     emitMu during that is OK if no other path needs emitMu for close; on
     Close, stop timer, drain under emitMu, then close `done`. Mirror
     httpagent close/drain order.

6. **Close / cancel:** call drain of pending chunks (and tools if tool lane
   later) before tearing down, same as `drainChunks` / `drainChunksClose`.

7. **Tool lane in Phase A:** **do not** enable `WithToolLane` for acpagent yet
   unless Phase B also targets Grok tools. Grok tools are lower volume than
   OpenCode bash streams; text H-1 is the win. Optional follow-up.

8. **`isHighFrequencyEvent`:** still useful for AgentSessionID stamping noise
   reduction; can remain. Do not use it as the coalesce mechanism.

### A.5 Tests (must pass)

| Test | Intent |
|---|---|
| Bounds frames + preserves concatenated text | H-1 core |
| Time-to-first-token immediate | Leading edge |
| Flush before turn_complete | Boundary |
| Timer flush without boundary | Mid-run |
| Unflush / full channel does not drop text | Backpressure |
| Replay flag preserved | Load path |
| `StreamCoalesce=0` one event per chunk | Kill switch |
| Concurrent emitters (if acpagent has them) | Ordering |

Port/adapt from `httpagent/coalesce_test.go` and keep rewritten versions of
`TestChunksCoalesceUnderBackpressure` / `TestCoalescedFlushPreservesReplay`.

### A.6 Docs

- README: document `providers.grok.stream_coalesce_ms`.
- MADR 0057 status note: H-1 implemented when done.
- Optional one line in `docs/chat-performance.md` if still referenced.

### A.7 Phase A gate

```bash
make pre-add-check
go test -race ./internal/provider/acpagent/... ./internal/config/... ./internal/daemon/...
# Manual / live optional:
# go test -tags live_grok ./internal/provider/grok/...   # only at acceptance
```

**Acceptance:**

- Default config: synthetic 100 chunks @ &lt;5 ms spacing → ≤ ~15 assistant
  frames over ~1s window (+ leading + final).
- Full text equality.
- Existing permission/plan/mode tests still green.
- No protocol changes.

**Commit shape:** config + acpagent + daemon + tests + yaml/README.

---

## Phase B — Tool lane for Codex + Goose (M-2)

**Goal.** Same supersede-per-tool-id behavior as OpenCode for non-terminal
`tool_call_update`.

### B.1 Code

| File | Change |
|---|---|
| `acphttp/session.go` `chunkBuffer()` | `chunkbuf.New(win, max, chunkbuf.WithToolLane())` |
| `codex/session.go` `chunkBuffer()` | same |
| Flush timers | Ensure `onFlushTimer` drains **tools with blocking/control send** like httpagent (`DrainTools` loop). **Audit codex/acphttp timers** — today they may only `Drain()` text. |

Critical: if tool lane is enabled but timer only drains text, tools stick until
a boundary. Copy httpagent’s `onFlushTimer` tool drain + close-path
`DrainTools`.

### B.2 Tests

- Same-id non-terminal updates → one held/latest per window.
- Terminal `completed`/`failed` emits immediately (blocking).
- Tool update does **not** flush text run (in-place, not boundary) —
  already chunkbuf contract; pin with test.
- Text + tool interleave order preserved (text run / tool card ordering).

### B.3 Phase B gate

```bash
go test -race ./internal/provider/acphttp/... ./internal/provider/codex/... ./internal/chunkbuf/...
```

**Acceptance:** under a scripted 50 tool updates / same id / 80 ms, wire emits
O(1–2) updates + terminal, not 50.

**Split ok:** Codex commit then Goose commit if review size hurts.

---

## Phase C — Drift and dead code (L-1, L-2, L-3 docs)

**Goal.** Code and docs match single-engine reality; remove footguns.

### C.1 Constants and comments

**File:** `apps/mobile/lib/data/chat/chat_models.dart`

- Remove `kMaxStreamingMarkdownChars` **or** repurpose as the first size-tier
  threshold for Phase D (`kStreamMdTier1Chars = 4000`). Prefer **repurpose
  with renamed constant** if Phase D is imminent; else delete and fix tests
  that only use it as “>4k still MarkdownBody”.

**File:** `chat_bubble.dart`

- Fix `debugMarkdownParseCount` dartdoc: remove “plain long-stream path does
  not parse” language.
- Ensure comments say single engine only.

### C.2 Dead parser

**Decision D8:** delete:

- `apps/mobile/lib/data/chat/markdown_parser.dart`
- `apps/mobile/test/markdown_parser_test.dart`

If `markdown` package becomes unused after delete, remove from `pubspec.yaml`
only if no other import (flutter_markdown_plus may still depend on it
transitively — check `dart pub deps` before removing direct dep; package is
direct today for the parser).

### C.3 Doc supersession

In `docs/0056-MADR-mcremote-android-protocol-stack-audit.md` (short note at
M-9…M-12) **or** only in 0057: “Phase 7 implemented; engine swap/mono cliff
closed; M-10 residual tracked in 0057.”

Prefer a 4-line callout under 0056 §M-9 rather than rewriting the whole audit.

### C.4 Phase C gate

```bash
cd apps/mobile && dart analyze && flutter test test/streaming_markdown_test.dart test/chat_render_test.dart
```

**Commit shape:** cleanup only; safe to land anytime.

---

## Phase D — Markdown throughput + closer (H-2, M-1)

**Goal.** Bound main-isolate markdown work on long streams; reduce raw-marker
flash without a second engine.

### D.1 Size-tiered minimum interval (H-2 / D6)

**File:** `apps/mobile/lib/features/chat/chat_bubble.dart`

Keep frame-aligned `_dirty` / post-frame callback. Add:

```dart
// Pseudocode
Duration _minStreamInterval(int len) {
  if (len > 16000) return const Duration(milliseconds: 200);
  if (len > 4000) return const Duration(milliseconds: 120);
  return Duration.zero; // frame-aligned only
}
```

In `didUpdateWidget` streaming path:

- If last render time + min interval not elapsed → set `_pendingTier = true`,
  schedule a Timer for the remainder **or** only re-arm post-frame until due.
- On fire: render if still dirty and still streaming.

Constants live in `chat_models.dart` next to show-more clamp.

**Do not** reintroduce mono plain path.

**Optional later (not in D unless profile forces):** visible-window clamp of
last N characters of markdown with “full reply on finalize” — product-sensitive;
requires UX sign-off.

### D.2 Stream closer expansion (M-1 / D7)

**File:** `streaming_markdown.dart`

Expand scan with precedence (innermost/latest close first, same as today):

1. Existing: fence > inline `` ` `` > `**`
2. Add: `~~` strikethrough pairs (toggle like bold)
3. Add: incomplete markdown link — if `[`…`](` seen without closing `)`, either
   close with `)` or strip to plain link text for stream only. Prefer **append
   `)`** only when `](` already present; if lone `[` without `](`, leave alone.

**Defer** single `*` / `_` italic auto-close (list markers / multiplication).
Document residual in closer dartdoc.

**Tests** in `streaming_markdown_test.dart`:

| Input (stream) | Expect |
|---|---|
| `hello ~~str` | closer adds `~~` |
| `see [x](http://a` | no raw hang; defined policy |
| `* item` list | unchanged (no false italic close) |
| existing bold/code/fence cases | still pass |

### D.3 Finalize reflow (L-4)

Acceptable if widget type stable. Optional: if stream text equals finalize
text after stripping only synthetic closers, skip rebuild — micro-opt.

### D.4 Link policy pin (L-5)

**File:** `chat_bubble.dart` `MarkdownBody`

- Set `onTapLink` to no-op or scheme allowlist (`http`, `https`, `mailto`).
- Widget test: crafted `javascript:` or unknown scheme does not throw / does
  not launch.

### D.5 Phase D gate

```bash
cd apps/mobile && flutter test test/streaming_markdown_test.dart \
  test/chat_render_test.dart test/transcript_ingest_test.dart
```

**Acceptance:**

- 20 KB synthetic stream: parse count scales with tier interval, not every
  16 ms frame under load.
- No mono path; always `MarkdownBody`.
- `~~` open no longer flashes raw.
- Existing 0018 parse-count identity tests for finalized replies still hold.

---

## Phase E — Optional host stability (M-3 residual, M-4 residual, M-6)

**Only after A is live in production long enough to re-measure ring pressure.**

### E.1 Measure first

Add temporary/debug metrics or test:

- Events per Grok turn before/after A (should drop sharply).
- History ring occupancy after long Grok reply.

If ring still thrashing after A, consider:

1. Adjacent same-type **chunk merge inside ring** on append (careful with seq:
   keep first or last seq? Client gap detection uses seq — merging after stamp
   breaks gaps). **Safer:** only coalesce pre-stamp (already true via chunkbuf).
2. Raise `historyBufferCap` slightly — last resort, more RAM/disk.

### E.2 Full-ring write cost (M-4 residual)

Optional journal spike (0056 Phase 5b): append-only segments + compact. **Out of
band design** if pursued; not required for markdown UX.

### E.3 Exact frame budgets (M-6)

Track under remaining 0056 Phase 4 work; do not block A–D.

---

## Phase F — Optional phone GC + observability

### F.1 Open-turn buffer (M-5)

If DevTools shows large copy traffic after A+D:

- Keep growable `StringBuffer` on notifier for open assistant/thought item.
- Materialize into `ChatItem` only on `_commit` / throttle.

High complexity vs identity/`sealedTail` rules — only with profiles.

### F.2 Metrics (L-6 / M-8)

In `noteUnflush` paths (all providers): already logs drops. Add:

- counter fields on session or slog attributes `coalesce_flush_total`,
  `unflush_drop_bytes` for ops.

### F.3 Cache integrity (M-7)

Optional payload version field + CRC; low priority.

---

## 5. Cross-phase regression matrix

| Scenario | Phases | Assert |
|---|---|---|
| Grok 200-token reply | A | Frames ≪ tokens; full text; TTFT ok |
| OpenCode tool bash stream | B (no reg) | Still supersedes; terminal immediate |
| Codex tool stream | B | Frame count drops |
| Goose text stream | A N/A; already chunkbuf | Unchanged |
| Phone reconnect mid-stream | A | History/resync still applies (0056 synchronizer) |
| Stream markdown table | D | MarkdownBody, not fused plain |
| Stream `**bold` / fence | D | No raw markers |
| Stream `~~x` | D | Closed |
| Finalize selectable | D | Selectable on; caret off |
| Kill switch coalesce 0 | A | Per-chunk events |
| `go test -race` providers | A,B | Green |
| `flutter test` stream suite | C,D | Green |

---

## 6. Implementation checklist (copy into PR / todo)

### Phase 0

- [ ] acpagent healthy-path frame count test (baseline or skip-to-green)
- [ ] Tool-lane absence test for codex/acphttp (or shared)

### Phase A

- [ ] `GrokProviderConfig.StreamCoalesceMs` + validate + default + load
- [ ] YAML examples + README
- [ ] `acpagent.Config.StreamCoalesce` + window helper
- [ ] Daemon wires explicit pointer for grok
- [ ] session: chunkbuf fields, emit rewrite, timer, drain on close
- [ ] Remove old `coalesced` map
- [ ] Port httpagent-style coalesce tests + replay + kill switch
- [ ] `make pre-add-check` + race tests green

### Phase B

- [ ] `WithToolLane` on acphttp + codex `chunkBuffer`
- [ ] Timer/close drain tools (control delivery)
- [ ] Tests for supersede + terminal + text interleave
- [ ] Race tests green

### Phase C

- [ ] Rename/remove `kMaxStreamingMarkdownChars` comments
- [ ] Delete or quarantine `markdown_parser.dart` + test
- [ ] 0056 supersession note
- [ ] analyze + flutter tests green

### Phase D

- [ ] Size-tier min interval constants + `_AssistantMarkdown` logic
- [ ] Closer: `~~` + safe link rule + tests
- [ ] `onTapLink` allowlist/no-op + test
- [ ] Long-stream parse bound test
- [ ] Full mobile stream suite green

### Phase E/F (optional)

- [ ] Measure ring after A
- [ ] Metrics counters
- [ ] Rope only if profiled

---

## 7. Risk register

| Risk | Mitigation |
|---|---|
| acpagent lock deadlock with emitMu + deliver | Follow httpagent order; never take s.mu while blocked on events if emitMu held without matching Close path; test Close under load |
| Replay text re-broadcast live | Pin Replay on tmpl; existing replay tests |
| Tool lane hides intermediate tool output users wanted | Supersede is replace-semantics already; terminal always ships; document |
| Size-tier makes stream feel “stuck” | Leading edge still frame-aligned for short text; 120–200 ms only when large; caret still animates |
| Closer breaks lists with `*` | Do not auto-close single `*` in D |
| Config `0` accidentally left in prod mesh yaml | Defaults 80; example yaml comments |

---

## 8. Out of scope (explicit)

- WebSocket binary codec / compression
- Message-level fork/revert IDs (0056 M-4 gated)
- FGS service-owned socket (0056 Phase 9)
- Replacing `flutter_markdown_plus`
- Host-side HTML sanitization of assistant markdown
- iOS
- Changing 32 ms phone batch window (works; re-tune only with metrics)

---

## 9. Definition of done (0057 program)

The 0057 program is **done** when:

1. **H-1:** Grok default path uses timed chunkbuf; tests prove frame bound +
   text integrity + kill switch.
2. **M-2:** Codex and Goose tool lanes on; tests green.
3. **H-2 first step:** size-tier stream interval landed; long-stream test
   bounds parse cadence.
4. **M-1 partial:** `~~` (and safe link rule) covered; residual single-`*`
   documented.
5. **L-1/L-2:** no dead mono/parser primary path; comments accurate.
6. MADR 0057 status flipped to **Accepted (phases A–D implemented)** with date
   and commit SHAs.

Phases E–F may remain open without blocking “Accepted”.

---

## 10. Suggested first implementation PR

**PR1 = Phase 0 tests + Phase A (H-1)** — largest user-visible throughput win
for Grok sessions.

**PR2 = Phase B** — tool lane symmetry.

**PR3 = Phase C + D** — markdown polish and FPS.

Stop after PR1 if only bandwidth for one change: **A alone is worth shipping**.

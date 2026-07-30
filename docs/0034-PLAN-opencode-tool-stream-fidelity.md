# OpenCode tool-stream fidelity: implementation plan

**Status:** Proposed
**Date:** 2026-07-27
**Decision:** [MADR 0034](./0034-MADR-opencode-tool-stream-fidelity.md)
**Evidence:** [Report 0033 rev 2](./0033-MADR-opencode-ui-ux-polish-report.md)
**Verified target:** OpenCode 1.18.5 (`~/.opencode/bin/opencode`). Re-probe
phase 0 before accepting a newer minor version.

## Goal and non-goal

Make the tool half of an OpenCode turn as well-behaved as MADR 0024 made the
text half: suppress redundant frames, show tool results, and stop tool events
from destroying the text stream they interleave with.

**Non-goals.** No change to `stream_coalesce_ms` or `maxPendingChunkBytes`
(MADR 0034 §2.5). No resync batching. No delta-encoded tool output. No new
protocol event types. Per-tool rate limiting is deliberately deferred to
phase 6, behind measurement.

## Dependency order

Phase 0 gates nothing structurally but sets the acceptance numbers for phases 1
and 6. Phases 1 and 2 are independent of each other and both must land **before**
phase 3, which increases payload size: D1 cuts frame count and D3 removes the
boundary cost, so the larger payload rides on an already-reduced stream. Phase 4
is independent of all of them.

```
Phase 0 (measure) ─┬─> Phase 1 (D1 dedup) ──┬─> Phase 3 (D2 output) ─> Phase 5 (docs) ─> Phase 6 (re-measure)
                   └─> Phase 2 (D3 order) ──┘
                       Phase 4 (D4 client guard) — independent, land any time
```

---

## Phase 0 — P0: measure and pin the contract

The one number this plan depends on and nobody has: how many
`message.part.updated` frames a single tool call produces, and whether
`State.Output` grows monotonically across them.

1. Add a `live_opencode`-tagged probe under `internal/provider/opencode/`
   (alongside `live_http_test.go`) that runs a session with a `bash` command
   producing incremental output — e.g. a loop emitting a line every 200 ms for
   ~5 s — and records, per `callID`:
   - total `message.part.updated` frames with `part.type == "tool"`;
   - the distinct `state.status` values and the frame index of each transition;
   - `len(state.output)` per frame, and whether it is non-decreasing;
   - whether `state.title` / `state.input` ever change mid-run.
2. Repeat for a `read` of a large file and a `grep` with many matches — three
   tool shapes, since output cadence may differ by tool.
3. Commit the capture as `docs/opencode-spike-1.18.5/tool-frames.json` with the
   recorded CLI version, mirroring `docs/codex-spike-0.145.0/`.
4. Pin the two facts the design rests on with assertions in the live test:
   output is non-decreasing within a call, and a terminal status
   (`completed`/`error`) is the last frame for that call.

**Gate:** phases 1 and 6 take their acceptance thresholds from this capture.
Live tests are acceptance tests, not part of the normal loop (AGENTS.md).

**Why it is P0 and not deferred:** if a tool run turns out to produce 3 frames
rather than 30, phase 1 is still correct but phase 6 is unnecessary, and the
whole plan shrinks. Measuring first is cheaper than building a rate limiter
nobody needs.

---

## Phase 1 — P0: D1, dedup tool events at the dialect

**Files:** `internal/provider/opencode/http.go`

1. Add to `httpSession` (`http.go:489-523`), documented in the same style as the
   neighbouring `runningSent` / `usageSent` fields:

   ```go
   // lastToolEmit holds the last payload actually emitted per tool id, so the
   // engine's repeated part.updated frames do not each cost a frame (MADR
   // 0034 D1). Cleared by turnCleanup alongside seenTools.
   lastToolEmit map[string]toolEmit
   ```

   ```go
   type toolEmit struct{ status, name, kind, text string }
   ```

2. Add `noteToolEmit(id string, e toolEmit) bool` next to `noteTool`
   (`http.go:1050`), returning whether this payload differs from the last one
   for `id`. Take `o.mu`, matching every sibling latch. Lazily allocate the map,
   as `noteTool` does.

3. In the `case "tool":` arm (`http.go:820-840`), build the four values first,
   then gate the emit:

   ```go
   e := toolEmit{status, name, kind, detail}
   isNew := o.noteTool(id)
   if !isNew && !o.noteToolEmit(id, e) {
       return // unchanged repeat frame
   }
   ```

   `isNew` must be evaluated before the dedup check so the first frame always
   emits as `tool_call`. A status change always differs in the tuple, so
   terminal frames can never be suppressed — no special case needed, but assert
   it in tests rather than relying on the reader to see it.

4. Clear the map in `turnCleanup` (`http.go:1008-1022`), next to
   `o.seenTools = nil`.

**Tests** (`internal/provider/opencode/dedup_test.go` — existing home for the
`runningSent`/`usageSent` latch tests):

- N identical tool frames → 1 `tool_call`, 0 `tool_call_update`.
- Status transition `running` → `completed` always emits, even when title,
  input and output are byte-identical.
- A changed `state.output` emits (guards against phase 3 regressing to the
  static-title tuple).
- `state.error` appearing mid-run emits.
- Two interleaved tool ids do not suppress each other.
- `turnCleanup` clears the map: the same tool id in a later turn re-emits.
- Map does not grow across turns (mirrors the existing `seenTools` bound test
  at `http_delta_test.go:243`).

**Acceptance:** with the phase-0 capture replayed as a fixture, emitted tool
events per call drop to `1 + (number of distinct payload states)`.

**Risk:** suppressing a frame the client needed. Mitigated by keying on the
exact tuple emitted — anything the client could observe is in the key.

**Rollback:** delete the gate in step 3; the map becomes inert.

---

## Phase 2 — P1: D3, in-place control events stop being stream boundaries

The one change to shared machinery. Small, but it amends MADR 0024 §2.2 and
touches every provider, so it lands on its own commit with its own tests.

**Files:** `internal/event/event.go`, `internal/chunkbuf/chunkbuf.go`

1. Add `event.IsInPlaceUpdate(ev Event) bool` next to `IsControl`
   (`event.go:69-105`), with the doc comment from MADR 0034 §2.3 — specifically
   including *why* it is payload-keyed and not type-keyed:

   ```go
   func IsInPlaceUpdate(ev Event) bool {
       return ev.Type == TypeToolUpdate && ev.ToolID != ""
   }
   ```

2. In `chunkbuf.Add` (`chunkbuf.go:92-138`), exclude in-place updates from the
   boundary arm so they fall through to the existing order-independent arm:

   ```go
   case !IsChunk(ev.Type) && event.IsControl(ev.Type) && !event.IsInPlaceUpdate(ev):
       // Boundary: creates a transcript position, so the pending tail must
       // land first and the next chunk earns a fresh immediate emit.
       out = append(b.drain(), ev)
       b.leading = true
       blocking = true

   case !IsChunk(ev.Type):
       // Order-independent: telemetry (usage_update, available_commands) and
       // in-place updates, which mutate an item an earlier event positioned.
       // Pass through without touching the run. Delivery is still guaranteed —
       // the caller blocking-sends on event.IsControl.
       out = []event.Event{ev}
   ```

3. Do **not** change `IsControl`. The delivery guarantee at
   `httpagent/session.go:1149`, `codex/session.go:1058` and
   `acphttp/session.go:1117` is `blocking || event.IsControl(e.Type)` and must
   keep returning true for `TypeToolUpdate`.

**Tests** (`internal/chunkbuf/chunkbuf_test.go`):

- A `tool_call_update` **with** a tool id, interleaved mid-run: the pending run
  is **not** drained, `leading` stays false, and the update is returned in `out`.
- The same update **without** a tool id: boundary behaviour preserved (drain +
  `leading = true`).
- `tool_call` (not update): boundary behaviour preserved — this is the invariant
  the whole design rests on.
- Ordering: `tool_call` for id X is always emitted before any update for X,
  under an interleaved chunk/tool sequence.
- A regression test asserting `IsControl(TypeToolUpdate)` is still true, so a
  future refactor cannot quietly make it droppable — with a comment pointing at
  MADR 0034 §2.3 and the codex delta hazard.

**Cross-provider check:** run `go test ./internal/provider/...`. codex,
acphttp and acpagent all pass tool updates through the same seam.

**Acceptance:** in a synthetic turn interleaving 200 text chunks with 20 tool
updates, the emitted assistant-chunk count matches the tool-free baseline
(within the leading-edge allowance) instead of scaling with tool-update count.
This is the direct measurement of MADR 0034 §1.3.

**Risk:** a consumer inferring transcript position from tool-update arrival
order. The mobile reducer is keyed by `toolId` (`transcript_reducer.dart:419`);
its only order-dependent path is the id-less fallback
(`transcript_reducer.dart:440-453`), which `IsInPlaceUpdate` explicitly excludes.

**Rollback:** drop the `&& !event.IsInPlaceUpdate(ev)` clause. One term.

---

## Phase 3 — P1: D2, tool output becomes visible

**Depends on phases 1 and 2.**

**Files:** `internal/provider/opencode/http.go`

1. Add `maxToolOutputChars = 8000` near the other clip constants, with a
   comment tying it to the client's `kMaxExpandedDetailChars`
   (`chat_models.dart:47`) and stating why it is not `kMaxItemTextChars`.

2. **`clip` must not be used for output.** It is wrong for this payload in two
   ways (`http.go:1148-1154`):

   ```go
   func clip(s string, max int) string {
       s = strings.Join(strings.Fields(s), " ")   // collapses ALL whitespace,
       if len(s) <= max {                          // including newlines
           return s
       }
       return s[:max] + "…"                        // cuts mid-rune
   }
   ```

   `strings.Fields` flattens a directory listing or grep result onto one line —
   the structure is the content for tool output. And `s[:max]` can split a
   multi-byte rune, which renders as a replacement character.

   Both are fine for its current callers (titles, single-line errors, JSON
   input), so leave `clip` alone and add a sibling:

   ```go
   // clipBlock truncates multi-line tool output for transport. Unlike clip it
   // preserves line structure — a directory listing or grep result is
   // unreadable once newlines are collapsed — and never cuts mid-rune.
   func clipBlock(s string, max int) string {
       s = strings.TrimRight(s, " \t\n")
       if len(s) <= max {
           return s
       }
       cut := max
       for cut > 0 && !utf8.RuneStart(s[cut]) {
           cut--
       }
       return s[:cut] + "\n…[truncated]"
   }
   ```

   The rune-safety loop exists for the reason already recorded in
   `chunkbuf.Unflush` (`chunkbuf.go:168-172`): *"Never cut mid-rune: the
   surviving text is shown to a user, and a leading partial rune renders as a
   replacement character."* Note the loop **decrements** here — it keeps the
   head and drops the straddling rune — whereas chunkbuf keeps the *tail* and
   so increments. Do not copy that one verbatim.

   Verified against the three cases the tests below pin: multi-line input keeps
   its newlines; a cap landing mid-rune cuts back to the preceding boundary and
   yields valid UTF-8; a cap landing on a boundary keeps the whole rune.

3. In the `case "tool":` arm, insert output into the precedence chain between
   input and error:

   ```go
   detail := strings.TrimSpace(part.State.Title)
   if detail == "" {
       detail = shortJSON(part.State.Input, 300)
   }
   if out := strings.TrimRight(part.State.Output, " \t\n"); out != "" {
       detail = clipBlock(out, maxToolOutputChars)
   }
   if part.State.Error != "" {
       detail = clip(part.State.Error, 300)   // an error still wins; single-line
   }
   ```

   Snapshot, not delta — MADR 0034 §2.2.

4. Verify `ToolName` still comes from `firstNonEmpty(part.State.Title, part.Tool,
   "tool")` — the name must stay the command, not the output.

**Tests** (`internal/provider/opencode/http_delta_test.go`):

- A tool frame carrying output emits it as `Text`.
- Growing output across frames emits each distinct snapshot (composes with
  phase 1) and suppresses frames where it did not grow.
- Output longer than `maxToolOutputChars` is clipped with the marker.
- Error takes precedence over output.
- Empty output falls back to title, then to `shortJSON(input)` — the pre-change
  behaviour is preserved when there is nothing to show.
- **`clipBlock` preserves newlines**: multi-line output survives with its line
  structure intact (the direct regression guard against reaching for `clip`).
- **`clipBlock` is rune-safe**: output whose byte at the cap is mid-rune is cut
  at the preceding rune boundary, and the result is valid UTF-8.
- `clip` is unchanged: its existing single-line callers keep current behaviour.

**Mobile check:** `apps/mobile/test/transcript_reducer_test.dart` — a tool card
whose text is replaced by a longer snapshot renders the newer value; and
`transcript_rows_test.dart` — a completed tool with long output still groups.

**Acceptance:** a `bash ls -la` in a live session shows its listing in the
expandable tool card on the phone.

**Risk:** payload growth. Bounded by the cap, reduced by phase 1, and no longer
boundary-forcing after phase 2 — which is exactly why this phase is third.

**Rollback:** delete the output clause; behaviour returns to title/input.

---

## Phase 4 — P1: D4, client identity guard

Independent of the daemon work; benefits every provider and replayed history.

**Files:** `apps/mobile/lib/data/chat/transcript_reducer.dart`

1. In `_upsertTool`, in the existing-card branch (`:419-432`), return `t`
   unchanged when the incoming values all match `prev` — remembering that an
   empty incoming value means "keep previous", so compare against the resolved
   value, not the raw one:

   ```dart
   final nextStatus = status.isNotEmpty ? status : prev.toolStatus;
   final nextName   = name.isNotEmpty ? name : prev.toolName;
   final nextKind   = kind.isNotEmpty ? kind : prev.toolKind;
   final nextText   = clippedDetail.isNotEmpty ? clippedDetail : prev.text;
   if (nextStatus == prev.toolStatus &&
       nextName == prev.toolName &&
       nextKind == prev.toolKind &&
       nextText == prev.text) {
     return t;   // nothing observable changed; do not publish new state
   }
   ```

2. Comment it with the reason: a redundant update otherwise copies the item
   list and publishes a new `SessionTranscript`, driving a rebuild for no
   visible change (MADR 0034 §1.4).

**Tests** (`apps/mobile/test/transcript_reducer_test.dart`):

- Applying the identical `tool_call_update` twice returns an identical
  transcript instance the second time.
- A change in any one of the four fields still updates.
- An update with empty text does not clear existing text (guards the
  resolved-value comparison).
- `transcript_ingest_test.dart`: a replayed history burst of identical tool
  updates produces one state publication.

**Acceptance:** `flutter test` green; no change in rendered output for any
existing test.

**Rollback:** delete the early return.

---

## Phase 5 — documentation, verification, rollout

1. Update `docs/protocol-v1.md`:
   - Specify the `tool_call` / `tool_call_update` `status` vocabulary —
     `pending | running | completed | failed`. It is currently undocumented
     (only `tool_kind` is, at `:733-736`), which is what let codex diverge to
     `in_progress` (Report 0032 F3). OpenCode's `mapToolStatus`
     (`http.go:1092-1105`) is the reference.
   - Document that `tool_call_update` carries a **delivery** guarantee but not
     an **ordering** guarantee relative to streaming text when it carries a tool
     id, and that `tool_call` carries both. This is the wire-visible consequence
     of phase 2 and must be written down before a second client exists.
   - Note that `tool_call_update.text` is a **snapshot** of tool output, not a
     delta, and is clipped daemon-side.
2. Update MADR 0024: mark §2.2's control-boundary rule as amended by MADR 0034
   §2.3, and correct its §1 reference to the mobile "120/200/320 ms throttle
   tiers" — those were superseded by the frame-aligned throttle
   (`chat_bubble.dart:505`, "Proposal F"), so the description is now historical.
3. Leave `docs/config.md:76` as-is. `stream_coalesce_ms` is already documented
   with the tradeoff and the `0` escape hatch; report 0033 rev 1's claim that it
   was "buried" does not hold, and `config.example.yaml` does not exist.
4. Set MADR 0034 status to Accepted with the implementation date; add an
   "Implementation record" section noting what landed and what phase 0 measured.
5. Full verification: `make preflight` (fmt, vet, lint, staticcheck, vulncheck),
   `make race`, `make test-all`, `flutter test`, and the phase-0 live probe
   re-run against the pinned 1.18.5.

---

## Phase 6 — P2: re-measure, then decide on rate limiting

Deliberately last and deliberately conditional.

1. Re-run the phase-0 probe with phases 1-3 landed. Record emitted events per
   tool call, and assistant-chunk count for a tool-heavy turn versus the
   tool-free baseline.
2. Decide:
   - If emitted tool events per call are in single digits and the chunk count
     matches baseline, **stop**. Rate limiting is unnecessary complexity — a
     timer per tool for a problem already solved.
   - If a tool genuinely produces many *distinct* payloads per second (plausible
     for a build with fast-scrolling output, where every frame's output really
     has grown), add a per-tool minimum interval — ~250 ms — that suppresses
     intermediate frames while **always** letting status transitions and the
     terminal frame through.
3. If implemented, the interval belongs in the dialect next to the dedup latch,
   not in `chunkbuf`: it is a policy about one engine's chattiness, not a
   transport guarantee.

**Gate:** do not implement step 2's rate limiter without the step-1 numbers.

---

## Delivery order

| # | Phase | Priority | Effort | Blocks |
|---|---|---|---|---|
| 0 | Measure and pin | P0 | 1-2 h | acceptance numbers for 1, 6 |
| 1 | D1 dedup latch | P0 | 1-2 h | phase 3 |
| 2 | D3 in-place ordering | P1 | 1-2 h | phase 3 |
| 3 | D2 tool output visible | P1 | 1-2 h | — |
| 4 | D4 client identity guard | P1 | 30 min | — |
| 5 | Docs and verification | P1 | 1 h | — |
| 6 | Re-measure, conditional rate limit | P2 | 1 h + conditional | — |

Total ~7-10 h excluding phase 6's conditional branch.

## Commit boundaries

One commit per phase, in delivery order. Phase 2 must not be folded into
phase 1 or 3 — it is the only change to shared transport machinery, and it needs
to be revertible on its own if a second client turns out to depend on tool-update
ordering.

## Definition of done

- Tool output visible on the phone for `bash`, `read`, and `grep`.
- Emitted tool events per call reduced to distinct payload states.
- Assistant-chunk count for a tool-heavy turn matches the tool-free baseline.
- `protocol-v1.md` specifies tool status vocabulary and the ordering guarantee.
- MADR 0024 §2.2 marked amended; MADR 0034 Accepted with an implementation record.
- `make preflight`, `make race`, `make test-all`, `flutter test` all green.
- Phase-0 capture committed with a live-tagged test pinning the two facts the
  design rests on.

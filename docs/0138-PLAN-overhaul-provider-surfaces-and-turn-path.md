---
status: in-progress
date: 2026-09-03
associated-madr: "0138-MADR-overhaul-provider-surfaces-and-turn-path.md"
---

# Implement the turn-path content fix, the RAM-backed transcript, and the provider surface close-out

Associated MADR:
[0138-MADR-overhaul-provider-surfaces-and-turn-path.md](0138-MADR-overhaul-provider-surfaces-and-turn-path.md)

## Goal

Stop losing the operator's transcript and the agents' command output, spend the
daemon's abundant RAM on keeping conversations instead of telemetry, make token
cost visible at the moment it is incurred, and close the provider surface gap —
in that order, because each earlier item is a precondition for the next.

Concretely, when this plan is complete:

* A session that produces 50,000 events still shows the operator's first prompt.
* Two codex output deltas inside one coalesce window both reach the phone.
* Opening a transcript costs one JSON marshal, not 505.
* A phone opening a long session renders the newest page first, in one round
  trip.
* A session that has grown to 1.5M tokens of context says so before the next
  turn, not in a log line nobody reads.
* grok answers `/compact`, `/rename`, `/undo` and `/sessions` instead of
  "this agent can't".

## Scope

### In scope

| item | MADR finding |
| --- | --- |
| Hash-index the replay dedupe | F15 |
| Single-pass byte budgeting in `HistoryPage` | F14 |
| Byte-budgeted, content-classed retention (host) | F1, F13, F16 |
| Newest-first / `before_seq` history paging | F17 |
| Phone transcript cap raised and driven by `Caps.HistoryRing` | F13 |
| `GOMEMLIMIT` and budget observability | amendment step 4 |
| codex tool-output append semantics | F2 |
| grok tool lane enabled | F3 |
| `permission.respond` / `question.respond` off the read loop | F4 |
| ACP notification path cannot kill an engine connection | F5 |
| `session.shell` off the prompt's async budget | F6 |
| Pair QR advertises the bound address | F11 |
| Auth-method warning corrected | F12 |
| Context-pressure, uncached-share and session-total reporting | T1–T3 |
| grok session interfaces via `x.ai/*` ext methods | F7 |
| kilo/grok structured quota in place of log scraping | F9 |
| kilo `model-usage` / `context` / `capabilities` consumption | F10 |
| `ModelReporter` for grok, goose, codex | F8 |

### Out of scope

* **Any model pin or substitution.** MADR 0137's third amendment stands: a
  provider runs on its own default model unless the user asks otherwise. Every
  token item here is *reporting*.
* **Automatic compaction.** T1 adds the signal and the affordance. Deciding
  that the daemon may compact a user's conversation on its own is a separate
  decision record.
* **kilo `/sync` seq-based resume.** Re-verified in this pass; MADR 0137 step
  7.1's decline stands and `/sync/history` is unchanged upstream.
* **Adopting `x.ai/hooks` as a permission mechanism.** Two permission systems
  on one turn needs its own record (0137 step 7.7).
* **The phone's on-disk transcript cache (`kTranscriptCacheMaxItems = 150`).**
  Raising it touches mobile storage policy; it is named in Phase 4 as a
  deliberate non-change.
* **A new `codex.*`-style namespace for another vendor.** F8's raggedness is
  recorded, not restructured, in this plan.

### Sizing decisions this plan fixes

These are the numbers every later step is written against. They come from the
MADR's amendment and are restated here so the plan is executable without
re-deriving them.

| constant | today | after | basis |
| --- | --- | --- | --- |
| per-session retention | 800 events | **32 MiB** | 806 B/event mean × ~40k events; MADR F16 |
| global retention | unbounded (16 × 800) | **384 MiB** | 12 × per-session, under `max_live_sessions: 16` |
| `GOMEMLIMIT` | unset | **1 GiB** | 384 MiB budget + 51 MB baseline + headroom; host has 32 GB |
| phone `kMaxTranscriptItems` | 800 | **4,000** | phone RAM is the scarce side; ~4× not ~50× |
| `historyMaxPage` | 800 | **2,000** | one page must stay under the 512 KiB frame cap |
| `historyDefaultPage` | 200 | 200 | unchanged |

## Implementation Steps

Every phase ends with `make pre-add-check` on the files it stages, `go test
-race ./internal/... ./cmd/...`, and one commit (`git commit --no-edit`; the
repo's `prepare-commit-msg` hook writes the message). No `git push` at any
point unless the owner asks in that turn.

**Every phase's new checks must be seen to fail before they are trusted.** Each
phase below names its fail-first experiment explicitly. Per `AGENTS.md`, run
those against a scratch copy — never by dirtying the tree — and record what the
failure actually printed in the execution record.

---

### Phase 1 — Remove the two quadratics. Nothing grows before this lands.

Precondition for every later phase. Both are live defects worth fixing on their
own merits.

**1.1 Hash-index the replay dedupe (F15).**
`internal/session/manager.go:201-207`.

* Add `replayKeys map[uint64]struct{}` to `entry`, keyed on a 64-bit FNV-1a of
  `Type | 0x1F | Text | 0x1F | ToolID | 0x1F | AgentSessionID` — the exact four
  fields the current scan compares, in that order, with a separator that cannot
  occur in a type name.
* On a `Replay` event, probe the map. On a hit, **still confirm by field
  comparison against the stored candidates** before discarding: a hash
  collision that silently drops a user's message is a worse bug than the one
  being fixed. Store `map[uint64][]int` (indices into `e.history`) so
  confirmation is exact.
* Rebuild the index whenever `e.history` is trimmed or an element is removed
  (`removeNativeLocked`, `removeOptimisticUserLocked`), or store generation
  counters — whichever the implementer measures as cheaper; the rebuild is
  O(n) and trims are rare.
* Delete the linear scan.

**1.2 Single-pass byte budgeting in `HistoryPage` (F14).**
`internal/session/manager.go:1484-1494`.

* Replace the shrink loop with a forward accumulation: walk `ring[start:]`,
  marshal **each event once**, accumulate encoded length plus the two bytes of
  separator, and stop at the first event that would exceed
  `historyMaxResponseBytes` or `limit`. Always include at least one event.
* Assemble the page from the already-encoded elements rather than re-marshalling
  the slice, so total marshal work is exactly one pass over the events returned.
* `truncated` and `nextSinceSeq` keep their current meaning.

**1.3 Stop copying the whole ring to read one page.**
`internal/session/manager.go:1508-1522` (`historyRing`) copies the entire ring
under `RLock` for every paged read, and `History` (`:1431`) does the same.

* Add `historySlice(id string, sinceSeq uint64, max int) ([]event.Event, uint64,
  uint64, bool)` that locates the start index under the lock and copies **only
  the candidate window** (`max` events), returning `firstSeq`, `latestSeq` and
  whether more remain.
* `HistoryPage` uses it. `History` keeps its full-copy contract — it has one
  caller (`HistoryFor`, `:1532`) — but gains a doc note that wire callers must
  not use it.

**Files:** `internal/session/manager.go`, `internal/session/manager_history_test.go`.

**Fail-first evidence required:**

* `A1` — revert 1.1's index in a scratch copy; a new
  `TestReplayDedupeIsNotQuadratic` (asserting a comparison counter, not wall
  time) must FAIL.
* `A2` — force a hash collision by stubbing the hash to a constant;
  `TestReplayDedupeConfirmsCollisionsByField` must FAIL if the field
  confirmation is removed, and PASS with it.
* `A3` — restore the shrink loop; `TestHistoryPageMarshalsEachEventOnce`
  (counting marshals via a counting `json.Marshaler` or an injected encoder)
  must FAIL.

**Acceptance:**

* Replaying 20,000 distinct events costs O(n) comparisons, measured by the
  counter in `A1`, against the 185,966,000 recorded in the MADR.
* `HistoryPage` with `limit=800` over the operator's `a787cbb9` fixture
  performs ≤ 606 marshals total (one per candidate event) against the measured
  505 *re*-marshals of growing slices, and allocates under 10 MB against the
  measured 709 MB. Fixture copied into `internal/session/testdata/`, redacted
  the same way `internal/wirecap` redacts.

---

### Phase 2 — Byte-budgeted, content-classed retention

This is the RAM decision. It changes what "full" means, not just how big.

**2.1 Declare the content classes.** New file `internal/event/retention.go`.

```go
// Class ranks an event for eviction. Lower classes are evicted first, and no
// event is evicted while any event of a lower class remains.
type Class uint8

const (
    ClassTelemetry Class = iota // available_commands, remote_commands,
                                // session_config, usage_update, notice
    ClassProgress               // tool_call_update, thought_chunk, plan,
                                // session_status
    ClassContent                // assistant_message_chunk, tool_call,
                                // artifact, session_title
    ClassAnchor                 // user_message, turn_complete,
                                // permission_request, permission_resolved,
                                // question_request, question_resolved, error
)

func ClassOf(t Type) Class
```

* `ClassAnchor` is **never evicted** while any lower-class event remains, and
  an anchor is only evicted when the ring holds nothing but anchors and is
  still over budget. That is the property that makes the F1 table impossible to
  reproduce.
* The mapping is exhaustive over `event.Type` and pinned by a test that fails
  when a new type is added without a class — the same shape as the existing
  `internal/protocol/doc_coverage_test.go`.

**2.2 Measure an event's retained size.** In the same file:

```go
// Bytes reports the approximate retained size of ev: the 44-field struct
// header plus the length of every string and the retained size of every slice
// element. Approximate on purpose — it is a budget input, not an allocator.
func Bytes(ev Event) int
```

* Pinned by `TestBytesTracksStructGrowth`, which fails when
  `unsafe.Sizeof(Event{})` changes without `Bytes`'s header constant changing.
  A 45th field that the budget does not count is how a budget silently stops
  bounding anything.

**2.3 Replace the count cap with a byte budget.**
`internal/session/manager.go:36-43`, `:222-225`.

* `historyBufferCap` (800) → `historyBudgetBytes = 32 << 20`, with
  `historyTrimTo` becoming `historyTrimToBytes = historyBudgetBytes * 3 / 4`.
* `entry` carries a running `historyBytes int`, maintained on append, trim and
  every removal path.
* Eviction: while `historyBytes > historyBudgetBytes`, drop the **oldest event
  of the lowest present class** until `historyBytes <= historyTrimToBytes`.
  Implement as a class-bucketed index into the ring so eviction is O(dropped),
  not O(ring) — the scan that Phase 1 removed must not reappear here.
* `HistoryRingCap` (`:41-43`) is advertised in `Caps.HistoryRing` and is an
  *event count*. Keep the field for compatibility and set it to a conservative
  event-count estimate derived from the byte budget; add
  `Caps.HistoryBudgetBytes` alongside it as the truthful value. Old clients
  keep reading a number that means what it always meant.

**2.4 Global budget.** New in `Manager`:

* `globalBudgetBytes = 384 << 20`, tracked as the sum of live entries' bytes.
* When exceeded, evict across sessions **least-recently-updated first**, and
  within a session by the class rule above. A session being actively prompted
  is never the eviction target while another live session has evictable bytes.
* Emit one `notice` per session the first time it is trimmed by the global
  budget, so a shrinking transcript is never silent. Deduped by
  `event.NoticeDeduper`, which already exists (`manager.go:173`).

**2.5 `GOMEMLIMIT`.** `cmd/mcremote/main.go` (serve path):

* `debug.SetMemoryLimit(1 << 30)` unless `GOMEMLIMIT` is already set in the
  environment, so an operator override wins and the default is stated in one
  place. Log the effective limit at start alongside the existing
  `starting mcremote` line.

**2.6 Durable file follows the same policy.**
`internal/session/store.go:278-280` re-trims to `historyBufferCap` on write,
which would undo 2.3.

* Take the already-classed, already-budgeted ring as authoritative and write it
  whole, with an independent `historyFileBudgetBytes = 32 << 20` guard that
  applies the *same* class rule rather than a count.
* `LoadHistory` (`:296`) and its `:310` re-trim get the same treatment.

**Files:** `internal/event/retention.go` (new), `internal/event/retention_test.go`
(new), `internal/session/manager.go`, `internal/session/store.go`,
`cmd/mcremote/main.go`, `internal/protocol/messages.go`.

**Fail-first evidence required:**

* `B1` — set `ClassOf` to return `ClassTelemetry` for `user_message`;
  `TestAnchorsSurviveTelemetryFlood` (replay of the real `5e360a4e` fixture:
  695 `tool_call_update` + 1 `user_message`, budget set to hold 100 events)
  must FAIL with the user message evicted.
* `B2` — remove the global budget's cross-session eviction;
  `TestGlobalBudgetEvictsColdestSessionFirst` must FAIL.
* `B3` — add a 45th field to a copy of `Event` without updating `Bytes`;
  `TestBytesTracksStructGrowth` must FAIL.
* `B4` — restore `store.go`'s count trim;
  `TestDurableHistoryUsesTheClassRule` must FAIL.

**Acceptance:**

* Replaying every one of the operator's six truncated sessions
  (`20e9170b`, `32da1cc5`, `5e360a4e`, `a787cbb9`, `c8ede651`, `dbc29fc3`,
  redacted into `internal/session/testdata/`) through the new retention retains
  **100% of `user_message` events** — against the MADR's measured 0, 0, 1, 1, 2,
  3.
* A synthetic session emitting 50,000 `tool_call_update` events and 20
  `user_message` events retains all 20 user messages and stays under 32 MiB.
* Daemon RSS after that synthetic run is under 200 MB, measured with
  `runtime.ReadMemStats` in the test and spot-checked with `ps` on the live
  daemon during Phase 6's acceptance run.

---

### Phase 3 — Newest-first history paging (F17)

Without this, a bigger ring makes the phone slower, not better.

**3.1 Protocol.** `internal/protocol/messages.go:386-392`:

```go
type SessionHistoryPayload struct {
    SessionID string `json:"session_id"`
    SinceSeq  uint64 `json:"since_seq,omitempty"`  // exclusive lower bound
    BeforeSeq uint64 `json:"before_seq,omitempty"` // exclusive upper bound; newest-first
    Limit     int    `json:"limit,omitempty"`
}
```

* `BeforeSeq` and `SinceSeq` are mutually exclusive; both set is `bad_payload`.
* `BeforeSeq: 0` **with the field present** means "the newest page". JSON cannot
  distinguish absent from zero for a `uint64`, so add a companion
  `Newest bool \`json:"newest,omitempty"\`` and treat `newest: true` as the
  request for the tail. This is uglier than a pointer and is chosen because
  every other payload in this file uses value types and `omitempty`; a pointer
  here would be the only one.
* `SessionHistoryResultPayload` (`:634-644`) gains
  `PrevBeforeSeq uint64 \`json:"prev_before_seq,omitempty"\`` — the cursor for
  the next older page — alongside the existing `NextSinceSeq`. `FirstSeq` and
  `LatestSeq` already exist and already bound the ring.

**3.2 Manager.** `HistoryPageBefore(id string, beforeSeq uint64, limit int)`,
sharing Phase 1.3's `historySlice` and Phase 1.2's single-pass budget, walking
backwards from the tail and returning events **oldest-first within the page**
so the client's reducer is unchanged.

**3.3 Server.** `internal/ws/server.go` history handler routes on the new
fields. `op_timeouts.json` needs no change: `session.history` stays at 30 s.

**3.4 Documentation.** `docs/protocol-v1.md` and `docs/protocol-v2.md` gain the
new fields with the mutual-exclusion rule. The repo treats these as the wire
contract; a field that ships undocumented is a field the next client author
guesses at.

**Files:** `internal/protocol/messages.go`, `internal/ws/server.go`,
`internal/session/manager.go`, `docs/protocol-v1.md`, `docs/protocol-v2.md`,
plus tests in `internal/ws/` and `internal/session/`.

**Fail-first evidence required:**

* `C1` — make `HistoryPageBefore` return oldest-first from the head;
  `TestNewestPageReturnsTheTail` must FAIL.
* `C2` — accept both `since_seq` and `before_seq`;
  `TestSinceAndBeforeAreMutuallyExclusive` must FAIL.
* `C3` — drop `prev_before_seq`; `TestBackwardPagingTerminatesAtFirstSeq` must
  FAIL (the client cannot walk further back).

**Acceptance:** a 20,000-event session serves its newest 200 events in **one**
round trip, and walks backwards to `FirstSeq` in exactly
`ceil(20000/limit)` requests with no request returning an event twice.

---

### Phase 4 — Raise the phone's cap and teach it the newest-first path

`apps/mobile`. Runs after Phase 3 so the client has something to call.

**4.1** `kMaxTranscriptItems` 800 → **4,000**
(`apps/mobile/lib/data/chat/chat_models.dart:61`). Phone RAM is the scarce
side; this is a 5× raise, not the host's 50×.

**4.2** Read the host's advertised budget rather than assuming.
`kHistoryFetchLimit` (`:69`) and the `for (var page = 0; page < 32; page++)`
bound (`mcremote_client.dart:3474`) both encode "the ring is ≤800". Replace
with `Caps.historyRing` / `Caps.historyBudgetBytes` from `auth_ok`, and a page
bound derived from it.

**4.3** `sessionHistory` fetches the **newest page first** (`newest: true`) and
renders it, then pages backwards with `before_seq` on scroll. The current
implementation blocks on the full forward walk before showing anything
(`mcremote_client.dart:3466-3524`); that is the visible hang on opening a long
session.

**4.4** `_enforceCap` (`transcript_reducer.dart:873-889`) rebuilds the entire
tool index on every drop. At 4,000 items that is 4,000 map writes per event
once the cap is reached. Rebuild incrementally, or store items in a structure
whose indices survive a front trim.

**4.5 Deliberate non-change:** `kTranscriptCacheMaxItems = 150` (`:71`) stays.
Raising the on-disk cache is a mobile storage decision with its own trade-offs
and is out of scope; it is named here so a reader does not read its absence as
an oversight.

**Files:** `apps/mobile/lib/data/chat/chat_models.dart`,
`apps/mobile/lib/data/chat/transcript_reducer.dart`,
`apps/mobile/lib/data/ws/mcremote_client.dart`,
`apps/mobile/lib/state/transcripts_notifier.dart`, and their tests.

**Pre-commit:** `flutter analyze`, `flutter test`, and
`dart format --output=none --set-exit-if-changed .` over `apps/mobile` — CI
runs the format check and one unformatted file is a red build with green tests
(`AGENTS.md`). `make preflight` runs the trio.

**Fail-first evidence required:**

* `D1` — restore the forward-only fetch;
  `test/history_newest_first_test.dart` must FAIL.
* `D2` — hardcode 800 again; a test asserting the client honours an advertised
  budget of 4,000 must FAIL.
* Widget tests must not assert on text-width-dependent layout: the test font is
  fixed-width and does not match the shipping font.

**Acceptance:** opening a 20,000-event session on the emulator renders the
newest content in one round trip, and scrolling up loads older pages without
refetching from `FirstSeq`.

---

### Phase 5 — The content-loss defects at the edges (F2, F3)

Independent of Phases 1–4 and could land first; sequenced here because Phase 2
changes what a lost event costs.

**5.1 Codex append semantics (F2).** `internal/chunkbuf/chunkbuf.go`.

* Add `WithToolLaneAppend()`, or a `ToolTextMode` option with values
  `ToolTextReplace` (today's behaviour) and `ToolTextAppend`.
* In append mode, `mergeTool` **concatenates** `prev.Text + next.Text` under a
  per-tool cap (`maxPendingChunkBytes`, already defined per provider), flushing
  when the cap is reached rather than discarding.
* `internal/provider/codex/session.go:2399` switches to append mode. Both codex
  delta sites (`notifications.go:65`, `session.go:1583`) are the reason and get
  a comment naming it.
* kilo, opencode and goose stay on replace — verified in the MADR, and pinned
  by a test per provider so a future edit cannot flip one silently.

**5.2 grok gets the lane (F3).**
`internal/provider/acpagent/session.go:1208` gains `chunkbuf.WithToolLane()` in
replace mode, matching its `summarizeToolContent` emission (`:1480`).

**Fail-first evidence required:**

* `E1` — codex on replace mode; `TestCodexOutputDeltasAreConcatenated`
  (two deltas inside one window, asserting both lines present) must FAIL, and
  the failure must name the missing first line rather than a count.
* `E2` — kilo switched to append mode;
  `TestReplaceProvidersDoNotConcatenate` must FAIL with duplicated text.
* `E3` — grok's lane removed; `TestGrokCoalescesToolUpdates` must FAIL.

**Acceptance:** the codex fixture
(`internal/provider/codex/testdata/wire/0.152.1/frames.jsonl`) replayed through
the session produces a transcript containing every `outputDelta` byte, compared
against the concatenation of the fixture's own deltas.

---

### Phase 6 — Token cost becomes visible (T1, T2, T3)

Reporting only. No compaction is triggered, no model is changed.

**6.1 Context pressure (T1).** `internal/session/turnlatency.go` already
computes `context_used` and `Usage.Size` (`:117`).

* At turn end, when `Used / Size` crosses **0.75** and again at **0.90**, emit
  one `notice` per threshold per session naming the numbers and the remedy:
  *"This session is using 1,526,598 of 2,000,000 context tokens (76%). `/compact`
  summarises it; `/new` starts fresh."*
* Thresholds are crossings, not levels — a session that drops back below after
  a compaction re-arms. Deduped through the existing `event.NoticeDeduper`.
* When `Size` is 0 or absent, emit nothing. A percentage of an unknown
  denominator is the kind of confident wrong number this record's predecessor
  was corrected for twice.

**6.2 Per-turn cost on the phone (T2).** Extend `event.Usage` with the fields
0137 Phase 6 already parses for every provider — `Input`, `Output`,
`Reasoning`, `CacheRead`, `CacheWrite` — and carry them on `usage_update`.

* This **is** a protocol change (`event.Event` is serialized to the phone),
  which 0137 Phase 2 deliberately deferred. It is taken here, additively:
  absent fields mean an engine that does not report them, and old clients
  ignore them.
* The phone shows the uncached share per turn. Session `10fe2896` re-paying
  11,090 uncached tokens on two consecutive identical turns is invisible today
  and is exactly what this makes visible.

**6.3 Session cost totals (T3).** `Meta` gains cumulative
`InputTokens`, `CachedTokens`, `OutputTokens` and `Turns`, persisted with the
rest of the record and returned on `session.list_result`.

* The phone can then show "34 sessions, 2.7 turns each, 21% of your input
  tokens uncached" without anyone reading a log.
* Cheap: five integers per session, updated once per turn on a path that
  already writes `Meta`.

**6.4 Structured quota in place of prose (F9).**

* kilo/opencode: on an engine limit signal, call
  `GET /kilocode/provider-usage` (verified live in this pass) and attach the
  structured result to the `error` event's `ErrorKind: "quota"`.
* grok: `x.ai/billing` and `x.ai/limit` on the ext-method path Phase 8 builds.
* `internal/agenterr` **stays** as the fallback for engines with no structured
  source, and gains a log line when it fires *without* a structured
  confirmation — so the day a vendor changes its wording is a day the daemon
  reports rather than a day it goes quiet.

**Files:** `internal/session/turnlatency.go`, `internal/session/manager.go`,
`internal/event/event.go`, `internal/protocol/messages.go`,
`internal/provider/kilo/`, `internal/provider/opencode/`,
`internal/agenterr/agenterr.go`, `docs/protocol-v1.md`, and the mobile client.

**Fail-first evidence required:**

* `F1x` — set the threshold to 1.01; `TestContextPressureNoticeAtSeventyFive`
  must FAIL.
* `F2x` — return `Size: 0`; `TestNoPressureNoticeWithoutAContextWindow` must
  FAIL if the guard is removed.
* `F3x` — drop the cache fields from the wire;
  `TestUsageEventCarriesCacheAccounting` must FAIL.
* `F4x` — a live-tagged `make live-kilo` probe asserting
  `/kilocode/provider-usage` answers the shape the code parses. Per `AGENTS.md`,
  a decision resting on external CLI behaviour is pinned with a live test.

**Acceptance:** replaying the operator's 15 turn-latency records through the
new code produces a pressure notice for `1b3742ba` (1,526,598 tokens) and
`84b277cd` (275,939), and none for the twelve turns under threshold.

---

### Phase 7 — The read-loop and budget defects (F4, F5, F6, F11, F12)

Small, independent, no provider quota.

**7.1 (F4)** Move `permission.respond` and `question.respond` to
`dispatchAsync` (`internal/ws/server.go:874`, `:882`). Add both to
`asyncOpTimeout` and `internal/protocol/op_timeouts.json` at **15 s** — above
the provider's own 10 s call timeout (`httpagent/session.go:1232`) so the
daemon's error is the authoritative one, per MADR 0095 D7.

* Audit and record a verdict for the seven handlers that remain inline. `auth`
  and `pair.claim` must stay inline (they establish the connection's identity).
  `receipts.list` and `devices.list` read and verify from disk on the read loop
  — move them or state why not.
* `op_timeouts.json`'s comment — *"Methods handled inline on the daemon's read
  loop are absent by design"* — is stale since 0137 Phase 4 and is corrected in
  the same commit.

**7.2 (F5)** `internal/provider/acpagent/session.go:1337-1342`: the control
send blocks on `s.events` or `s.done`. Add a third arm — a bounded overflow
buffer, or a deadline after which the session is faulted with an explicit
`error` event — so the ACP SDK's 1024-slot notification queue
(`connection.go:108`, `:446`) can never overflow and destroy the engine
connection.

* The MADR labels this unobserved. The fix is therefore conservative: it must
  not change behaviour for a healthy pump, and the test drives the pathological
  case directly.

**7.3 (F6)** Give `session.shell` its own concurrency budget, separate from
`maxAsyncPerClient` (`internal/ws/server.go:181`). Two concurrent shells per
connection, and shell slots never consume prompt slots.

**7.4 (F11)** `internal/cli/pair.go:499`: `detectAdvertiseHost` takes
`cfg.Listen.Host`. A loopback bind advertises loopback. A bind to a specific
non-tailnet address advertises that address. `tailscale`/empty keeps today's
Tailscale-IPv4 preference. Reproduced in the MADR with `curl` exit 7 against
the advertised address.

**7.5 (F12)** `internal/provider/acpagent/acpagent.go:583-586`: demote to
`Debug`, or suppress when the agent's own credential store shows it is already
authenticated. It has fired 90 times and been true zero times.

**Fail-first evidence required:**

* `G1` — restore the inline permission handler;
  `TestPermissionRespondDoesNotStallTheReadLoop` (a stalled fake engine, then a
  `session.cancel` on the same connection asserted to complete under 1 s) must
  FAIL.
* `G2` — remove 7.2's third arm; `TestACPConnectionSurvivesAStalledPump` must
  FAIL.
* `G3` — share the shell budget again; `TestShellDoesNotStarveThePrompt` must
  FAIL.
* `G4` — restore the tailnet-only detection;
  `TestPairAdvertisesTheBoundHost` must FAIL.

---

### Phase 8 — Close the provider surface gap (F7, F8, F10)

Delivered per provider and per interface, so it can be stopped at any point
without leaving the daemon inconsistent. grok first: it is the furthest behind.

**8.1 grok ext-method plumbing.** A typed helper in
`internal/provider/acpagent/xaiextensions.go` for `conn.ExtMethod(name, args)`,
with per-method request/response types and a capability probe so a grok build
that lacks a method degrades to "unsupported" rather than erroring a turn.

**8.2 grok interfaces**, in this order (each is one interface, one ext method,
one test, and can be committed alone):

| step | interface | method |
| --- | --- | --- |
| 8.2a | `CompactSession` | `x.ai/compact_conversation` |
| 8.2b | `RenameSession` | `x.ai/session/rename` |
| 8.2c | `PurgeSession` | `x.ai/session/delete` |
| 8.2d | `AgentSessionLister` | `x.ai/sessions/list` |
| 8.2e | `RevertSession` / `UndoSession` | `x.ai/rewind/points`, `x.ai/rewind/execute` |
| 8.2f | `CommandCatalog` | `x.ai/commands/list` |
| 8.2g | `SkillRefreshSession` | `x.ai/skills/refresh-baseline` |

Each is exercised against grok 1.0.13 under `-tags live_grok`. **Ask the owner
before any run that spends grok quota**; the interface calls above are session
management and should not invoke a model, which each test asserts by checking
that no `usage_update` arrives.

**8.3 `ModelReporter` for grok (F8).** grok's `initialize` result carries
`_meta.modelState.currentModelId` — present in the checked-in fixture
(`internal/provider/grok/testdata/wire/1.0.13/frames.jsonl`) and unread. Store
it at handshake and implement `CurrentModel()`. Today **7 of 7** grok turn
records carry no model. codex has `cliVersion`/model on `thread/started`; goose
has none and stays absent rather than guessed.

**8.4 kilo surfaces (F10).**

* `GET /session/{id}/model-usage` → per-session cost, feeding Phase 6.3.
* `GET /api/session/{id}/context` → the `/context` command's answer, in place
  of an inferred one.
* `GET /experimental/capabilities` at engine ready, replacing hardcoded
  assumptions and logged once.

**8.5 Recorded, not adopted.** A table in the execution record, following the
pattern MADR 0137 step 7.7 set, for every grok ext method deliberately left
unwired — `x.ai/cloud/*`, `x.ai/marketplace/*`, `x.ai/feedback*`,
`x.ai/privacy/*`, `x.ai/consent/record`, `x.ai/getApiKey`/`setApiKey`,
`x.ai/hooks/*` — with one sentence each. An unexplained gap reads as an
oversight.

**Acceptance:** the MADR's F8 matrix is regenerated and grok moves from 12
implemented interfaces to at least 19, with every addition covered by a
live-tagged test.

---

## Verification

Run at the end of every phase:

```bash
make pre-add-check FILES="<the files this phase stages>"
go test -race ./internal/... ./cmd/... -count=1
```

Run at the end of Phase 4 and again at Phase 8:

```bash
make preflight          # flutter analyze + flutter test + dart format check
```

Live probes, at acceptance only, and **after asking the owner** — these spend
provider quota:

```bash
go test -tags live_kilo  ./... -count=1
go test -tags live_grok  ./... -count=1
go test -tags live_codex ./... -count=1
```

### Acceptance criteria for the plan as a whole

1. **No transcript loses a user message.** Every one of the operator's six
   truncated fixtures replays with 100% of its `user_message` events retained.
   Today: 0, 0, 1, 1, 2, 3 retained out of the originals.
2. **No command output is dropped.** The codex fixture's `outputDelta` bytes
   appear in the transcript in full.
3. **Opening a transcript is cheap.** `session.history` at `limit=800` over the
   `a787cbb9` fixture allocates under 10 MB and completes under 50 ms. Today:
   709 MB, 2,516 ms.
4. **A long session opens at the bottom in one round trip**, verified on the
   emulator against a 20,000-event session.
5. **Replay is linear.** 20,000 replayed events cost O(n) comparisons, measured
   by counter. Today: 185,966,000 comparisons, 352 ms.
6. **RAM stays bounded.** A 16-session soak, each driven to its 32 MiB budget,
   holds daemon RSS under 600 MB with `GOMEMLIMIT` at 1 GiB, measured with `ps`
   against today's 51.4 MB idle baseline.
7. **Token cost is visible.** A session crossing 75% of its context window
   produces a notice; every `usage_update` carries cache accounting; a
   `session.list` shows cumulative tokens per session.
8. **The read loop cannot be stalled by a provider call.** `permission.respond`
   against a stalled engine does not delay a concurrent `session.cancel`.
9. **grok answers `/compact`, `/rename`, `/undo` and `/sessions`.**
10. **Every new check has been seen to fail.** The execution record contains
    the actual failure output for A1–A3, B1–B4, C1–C3, D1–D2, E1–E3, F1x–F4x
    and G1–G4 — not a claim that they were run.

### What would falsify this plan

Named up front, because the MADR this plan serves was corrected twice for
believing an instrument:

* If the class-based eviction is measured to evict content while telemetry
  survives on any of the six real fixtures, Phase 2's policy is wrong and the
  budget must not ship on it.
* If raising the phone cap to 4,000 pushes emulator memory past its budget or
  drops frames on scroll, 4.1 reverts to a smaller number and the host budget
  stands alone — the host and phone budgets are deliberately independent so
  this is possible.
* If `Bytes` is measured to under-report retained size by more than 25% against
  `runtime.ReadMemStats` on a real ring, the budget does not bound anything and
  Phase 2 stops until it does.

## Rollout and Rollback

**Order.** Phases 1 → 2 → 3 → 4 are a chain: each is a precondition for the
next, and none may be skipped. Phases 5, 6, 7 are independent of that chain and
of each other; any of them may land at any point. Phase 8 is last because it
adds surface to a path the earlier phases are still repairing, and because its
tests spend quota.

**Per-phase rollout.** One commit per phase (`git commit --no-edit`). No push
unless the owner asks in that same turn. The daemon is restarted from the
operator's launchd agent after Phases 2, 3 and 7, and the phone is rebuilt
after Phase 4.

**Compatibility.**

* Phase 2 changes the durable history format's *policy*, not its schema.
  Existing `history.json` files load unchanged; they are simply no longer
  re-trimmed by count. Already-truncated histories stay truncated — nothing
  recovers them, and the MADR says so.
* Phase 3 is additive: `before_seq`/`newest` are new optional fields; a client
  that never sends them sees today's behaviour exactly.
* Phase 6.2 is additive on `event.Usage`; absent fields mean an engine that
  does not report them.
* `Caps.HistoryRing` keeps its meaning (an event count) and gains
  `HistoryBudgetBytes` beside it, so a phone built before Phase 4 keeps working
  against a daemon built after Phase 2.

**Rollback.**

* Phases 1, 5, 7 are self-contained code changes: revert the commit.
* Phase 2 rolls back by restoring `historyBufferCap` and the count trim. The
  on-disk files a budgeted daemon wrote are larger than the old cap; the old
  code re-trims them on the next write and loses the surplus. **That is
  destructive to retained transcript**, so a rollback of Phase 2 is announced to
  the owner before it is performed, never taken unilaterally.
* Phase 3 rolls back by ignoring the new fields server-side; a Phase-4 client
  then falls back to forward paging, which still works.
* Phase 4 rolls back independently of the host — the host tolerates both client
  behaviours by construction.
* `GOMEMLIMIT` is a one-line revert and can also be overridden per host from
  the launchd/systemd unit's environment without a rebuild.

**Operational note.** The operator's daemon runs under
`~/Library/LaunchAgents/com.magiccliremote.mcremote.plist` with `KeepAlive` and
no memory limit set. Phase 2.5's `GOMEMLIMIT` default is applied in-process
precisely so it does not require touching that plist, and so an operator who
wants a different limit can set the environment variable there without a
rebuild.

## Execution Record

### Phase 1 — 2026-09-03, complete

**1.1 Replay dedupe is indexed.** `entry` gains `replayIndex map[uint64][]int`
and `replayCompares uint64`. `replayKey` folds the exact four fields the old
scan compared — `Type`, `Text`, `ToolID`, `AgentSessionID`, separated by
`0x1F` — through an allocation-free FNV-1a. A hash hit is **confirmed field by
field** before anything is discarded; a 64-bit collision is improbable, but the
failure it would cause is precisely the defect this record exists to fix.

The index is rebuilt on every trim and on both native-identity removal paths,
and is built from the seed when a restart loads durable history into a fresh
`entry` — without that last one, a `session/load` immediately after a restart
would have re-appended the entire durable transcript as new content.

**1.2 `HistoryPage` encodes each event once.** The shrink loop is replaced by
`historyBudgetPrefix`, a single forward pass that encodes each candidate once
and stops at the first event that would exceed `historyMaxResponseBytes`. It
always returns at least one event, so a single oversized event makes progress
instead of wedging the pager at that seq.

`historyMarshal` is a package variable so a test can count encodes. It is never
reassigned in production; a call count is the only thing that separates the two
implementations without timing them, and a timing assertion is not a check this
repository trusts.

**1.3 Paged reads no longer copy the ring.** `historyRing` is deleted and
replaced by `historySlice`, which binary-searches the start (the ring is
Seq-ascending; deletions leave gaps, which "first Seq greater than since"
handles and an equality search would not) and copies only the candidate window.
`History` keeps its full-copy contract for its single caller and gains a doc
note that wire callers must not use it.

### Fail-first evidence — three breakages, each verified against a scratch copy

Run against `…/scratchpad/failfirst/repo`, a copy of the tree. The working tree
was never dirtied, and no `git checkout` was used to clean up.

```text
A1. the pre-index linear scan restored (counter kept inside it)
    FAIL TestReplayDedupeIsNotQuadratic
         replay dedupe is scanning: 490000 field comparisons for 1400
         events, want <= 2800
    (490,000 = n²/2 × 2 passes at n=700, exactly the quadratic)

A2. field confirmation removed; trust the hash
    FAIL TestReplayDedupeConfirmsCollisionsByField
         a hash hit that does not match on fields must not suppress the
         event: history has 1 entries, want 2

A3. the shrink loop restored, routed through the same counted seam
    FAIL TestHistoryPageMarshalsEachEventOnce
         the byte budget encoded 312774 times for a 800-event window;
         want at most one pass
```

A3's first attempt failed for the *wrong* reason: the restored loop called
`json.Marshal` on the slice and so bypassed the counted seam entirely, tripping
a secondary assertion (`encoded 0 times but returned 124 events`) rather than
the one that matters. Re-run with the old loop routed through `historyMarshal`,
the count itself is what fails. A fail-first that fails for an unintended reason
has not tested the check.

### Acceptance — measured against the operator's real transcripts

`HistoryPage` at `limit=800`, the value the phone actually sends
(`kHistoryFetchLimit`), over the live data dir:

| session | ring | returned | encodes | allocated | elapsed |
| --- | --- | --- | --- | --- | --- |
| a787cbb9 | 606 | 102 | **103** | **1.0 MB** | **1.0 ms** |
| 1efee1da | 629 | 251 | **252** | **1.2 MB** | **1.3 ms** |
| 20e9170b | 799 | 799 | 799 | 1.0 MB | 1.2 ms |

Against the MADR's pre-fix measurements on the same two files: 505 marshals /
709 MB / 2,516 ms, and 379 marshals / 286 MB / 1,114 ms. **709 MB → 1.0 MB and
2,516 ms → 1.0 ms**, returning the identical page.

```text
go build ./...                                   -> ok
go test -race ./internal/... ./cmd/... -count=1  -> ok (full tree)
```

### Deviation — 2026-09-03: fixtures are synthetic, not the operator's transcripts

*What was found.* Phase 1's and Phase 2's acceptance criteria as written say to
copy the operator's real `history.json` files into `internal/session/testdata/`,
"redacted the same way `internal/wirecap` redacts". `wirecap`'s redaction strips
the home path only — it does not remove conversation content — and
`maccavelli/magic-cli-remote` is a **public** repository
(`gh repo view --json visibility` → `PUBLIC`). Committing the fixtures would
publish the operator's agent conversations permanently into git history, which
no step of this plan asked for and which cannot be undone by deleting the file
later.

Content was inspected rather than assumed: `a787cbb9` carries 8,233 characters
of free text, 22 absolute-home-path hits, and no emails, tokens, IP addresses or
URLs. Benign here — but the decision is about a one-way door, not this one file.

*Resolution, chosen by the owner:* **synthetic fixtures plus an env-gated real
test.** Committed tests use a deterministic generator calibrated to the
per-event-type byte distribution measured in MADR 0138 F16. A second test reads
real transcripts only when `MCREMOTE_HISTORY_FIXTURE_DIR` names a directory, and
skips otherwise, so the acceptance measurement above stays reproducible on the
operator's own machine without shipping anything.

*Scope added to Phase 1:* `internal/session/history_fixture_test.go`. Phase 2's
acceptance criteria inherit the same substitution.

*Not worked around.* The acceptance numbers in the table above were measured
against the real files before this decision was taken; the substitution changes
what is committed, not what was verified.

### Phase 2 — 2026-09-03, complete

**2.1/2.2 `internal/event/retention.go`.** Four classes — telemetry, progress,
content, anchor — with `ClassOf` exhaustive over every declared `event.Type`,
and `Bytes` reporting an event's retained size from the struct header plus every
string and slice element it owns.

Both are pinned by tests that read the *source*, not a hand-written list:
`TestClassOfIsExhaustive` parses every `Type… Type = "…"` constant out of the
package and checks each appears in the switch, and `TestBytesTracksStructGrowth`
walks `Event`'s fields reflectively and fails on any string field `Bytes` does
not sum. A list maintained by hand would be updated by the same person who
forgot to classify the new type, so it would agree with the mistake.

`Bytes` was measured against the allocator rather than asserted:
`TestBytesDoesNotUnderReportRetainedSize` builds 20,000 realistic events and
compares the accounting to `runtime.ReadMemStats`. It reports **1,523 B/event**
and over-reports by 2× against `HeapAlloc` — conservative, which is the safe
direction for a budget, and close to the 1.5 KB/event this plan's sizing table
assumed.

**2.3 The count cap is gone.** `historyBufferCap = 800` and `historyTrimTo` are
replaced by `historyBudgetBytes = 32 MiB` and `historyTrimToBytes` (75%).
`entry` carries a running `historyBytes`, and `enforceHistoryBudgetLocked`
evicts lowest class first, oldest first within a class, **never the newest
event** — a transcript that drops what just arrived is worse than one briefly
over budget, and a single event larger than the whole budget would otherwise be
discarded the instant it landed.

`rebuildReplayIndexLocked` became `reindexHistoryLocked` and now recomputes the
replay index and the byte total in one pass. They are two views of the same
slice; maintaining them separately is how one ends up describing a ring that no
longer exists.

**2.4 Global budget.** `globalBudgetBytes = 384 MiB`, enforced across live
sessions coldest-first by `lastEventAt`, and never against the session being
prompted while any colder one still has bytes to give. The first time a session
is trimmed this way its operator gets one notice, emitted after the manager lock
is released and deduped by the flag on the entry. A transcript that silently
shrinks is the failure this record is about; a global budget that shrank it
silently would only move the failure.

**2.5 `GOMEMLIMIT`.** `applyMemoryLimit` sets a 1 GiB soft ceiling unless the
operator already set `GOMEMLIMIT`, whose value wins, and the effective limit and
its source are logged. Verified live on both paths:

```text
msg="starting mcremote" … mem_limit_bytes=1073741824 mem_limit_source=default
GOMEMLIMIT=256MiB …      … mem_limit_bytes=268435456  mem_limit_source=GOMEMLIMIT
```

**2.6 The durable file uses the same rule.** `store.go`'s two count re-trims are
replaced by `boundHistoryByClass`, the class rule expressed over a plain slice,
so the file and the ring cannot disagree about what a transcript is. Re-trimming
by count on write would have undone the retention the ring had just enforced.

**Protocol.** `Caps.HistoryBudgetBytes` is advertised beside `Caps.HistoryRing`.
`HistoryRing` stays an event count — that is what it has always meant and phones
size their own buffers from it — and is now a conservative estimate derived from
the byte budget; the truthful value sits next to it. Additive, so a phone built
before this keeps working.

### Fail-first evidence — five breakages, one of which exposed a worthless test

```text
B1. user_message demoted to ClassTelemetry
    FAIL TestAnchorsSurviveATelemetryFlood
         retained 0 of 20 user messages; anchors must outlive telemetry

B2. coldest-first ordering reversed
    FAIL TestGlobalBudgetEvictsColdestSessionFirst
         the coldest session was not trimmed: 10516160 bytes, was 10516160

B3. a 45th string field added to Event, uncounted by Bytes
    FAIL TestBytesTracksStructGrowth
         string fields Bytes does not count: [Unbudgeted]

B4. store.go's count trim restored
    FAIL TestDurableHistoryRespectsCap
         kept 2 user messages, want all 3: telemetry is evicted before the
         conversation

B5. a new event type added with no class
    FAIL TestClassOfIsExhaustive
         event types with no explicit class (they would fall to ClassTelemetry
         and be evicted first): [TypeUnclassified]
```

**B2 passed on its first run, and that was the most useful result in the
phase.** The original `TestGlobalBudgetEvictsColdestSessionFirst` sized its
three sessions so that clearing the overage required trimming *all* of them —
so every visit order produced the same outcome and the test asserted nothing
about ordering at all. It was rewritten so exactly one session's worth of
trimming clears the overage, which makes the order the only variable, and it
then failed against the reversed comparison as shown above.

A check that has only ever been seen to pass is indistinguishable from one that
does nothing. This one was, for about ten minutes.

### Acceptance — measured against the operator's real transcripts

Every stored transcript replayed through the new retention:

```text
user_message events: 93 offered, 93 retained across all 34 real transcripts
```

Against the MADR's F1 table, where the six truncated sessions retained
**0, 0, 1, 1, 2 and 3**. Nothing was evicted at all, which is the finding: those
transcripts are a rounding error against 32 MiB and were being truncated by a
cap that had nothing to do with memory.

Synthetic acceptance, at sizes the real data does not reach:

```text
TestAnchorsSurviveATelemetryFlood
  offered=6020 retained=421 bytes=26602673 user_messages=20/20
```

6,020 events — 20 prompts each followed by 300 lines of 64 KiB tool output,
about 390 MiB — reduced to 421 events inside the 32 MiB budget, with **every
prompt kept**. That is the shape of codex session `5e360a4e`, which under the
old cap kept 695 lines of one command's stdout and one user message.

```text
go build ./...                                            -> ok
go test ./internal/... ./cmd/... -count=1                 -> ok
go test -race ./internal/... ./cmd/... -count=1           -> ok (full tree)
```

### Deviations

**2026-09-03 — the global budget needed a seam to be testable.** As specified,
`globalBudgetBytes` was a constant, and exercising cross-session eviction
against 384 MiB would mean allocating that much in a unit test. A `globalBudget`
field on `Manager` (zero means the constant) was added so tests can drive the
rule at 24 MiB. Production behaviour is unchanged; the rule under test is the
ordering, not the number.

**2026-09-03 — two existing tests asserted the old cap and were rewritten, not
deleted.** `TestHistoryRingBufferCapsAndOrders` checked *where* the 800-event
drop landed; it now checks that 900 small events are all retained, which is the
behaviour change. `TestDurableHistoryRespectsCap` checked that the file kept the
last 800 events; it now checks that the file stays inside its byte budget and
keeps every `user_message`. Both names were kept so the history of what they
guarded stays findable.

### Phase 3 — 2026-09-03, complete

**3.1 Protocol.** `SessionHistoryPayload` gains `BeforeSeq` (exclusive upper
bound, newest-first) and `Newest` (the tail of the ring).
`SessionHistoryResultPayload` gains `PrevBeforeSeq`, the backward twin of
`NextSinceSeq`.

`Newest` is a separate flag rather than a `BeforeSeq` sentinel because `0` is a
natural "no bound" for a `uint64` with `omitempty` and cannot be told apart from
an absent field. A pointer would work and would be the only one in the file; the
flag reads better at both ends of the wire.

**3.2 Manager.** `HistoryPageBefore` / `HistoryPageBeforeFor`, sharing Phase 1's
`historySlice` discipline through a new `historySliceBefore` (binary search to
the upper bound, copy only the candidate window) and a new
`historyBudgetSuffix`.

The suffix function is the reason this is not just the forward pager with
reversed arguments: the byte budget must trim the **oldest** end of a backward
page, because the newest events are the ones the screen is opening on. Pinned by
`TestNewestPageEncodesEachEventOnce`, which asserts the page still ends at the
newest seq after the budget shortened it.

Pages are returned **oldest-first within the page** in both directions, so the
client's reducer is unchanged.

**3.3 Server.** The history handler routes on the new fields and refuses
`since_seq` together with `before_seq`/`newest` as `bad_payload`. A reply
carries the cursor for the direction it was asked in and omits the other, so a
client cannot accidentally walk away from the screen it just rendered.

**3.4 Docs.** `docs/protocol-v1.md` documents both directions, the
mutual-exclusion rule, the backward cursor and the new byte-based retention with
its class order. `docs/protocol-v2.md` documents `history_budget_bytes` beside
`history_ring` and explains why the latter stays an event count. The eight
markdownlint findings in `protocol-v1.md` are all on lines this phase did not
touch — checked against `git show HEAD:` rather than assumed.

### Fail-first evidence — three breakages

```text
C1. HistoryPageBefore returns the head of the ring
    FAIL TestNewestPageReturnsTheTail
         newest event in the page is seq 200, want 5000 — this page is not
         the tail

C2. since_seq and before_seq both accepted
    FAIL TestWSSinceAndBeforeAreMutuallyExclusive
         want error for since_seq + before_seq, got session.history_result

C3. prev_before_seq dropped from the reply
    FAIL TestBackwardPagingTerminatesAtFirstSeq
         prev_before_seq did not advance: 0 then 0
```

### Acceptance

`TestBackwardPagingTerminatesAtFirstSeq` walks a 1,000-event ring backward at
`limit=200` and asserts **exactly 5 pages, 1,000 distinct events, and no event
returned twice**. `TestNewestPageReturnsTheTail` gets the newest 200 of 5,000 in
**one** call — the round trip a chat screen needs, which under forward-only
paging would have been a walk from seq 1.

```text
go build ./...                             -> ok
go test ./internal/... ./cmd/... -count=1  -> ok (full tree)
```

### Phase 4 — 2026-09-03, complete

**4.1 `kMaxTranscriptItems` 800 → 4,000.** A 5× raise, not the host's ~26×.
Phone RAM is the scarce side of this pair, and the two budgets are deliberately
independent: the goal is that the client stops being the *tighter* of the two
caps, not that it matches the host.

**4.2 The client reads the host's budget.** `ServerCaps` gains
`historyBudgetBytes`, parsed from `auth_ok.caps.history_budget_bytes` and
defaulting to 0 for a daemon that still bounds by count. `historyRing` keeps its
meaning and is no longer assumed to be 800.

**4.3 Newest-first fetch.** New `McremoteClient.sessionHistoryNewest`, returning
a `HistoryPage` with the events, the backward cursor, and the ring bounds. The
existing forward `sessionHistory` is untouched and still used where a full walk
is what is wanted; the difference is that opening a screen no longer has to be
one.

Two details that are not obvious and are pinned by tests: a page whose
`truncated` is true but which carries **no** cursor is reported as the end
rather than retried, because a client that re-requests the same window forever
is worse than one that shows slightly less; and `kHistoryFetchLimit` drops from
**800 to 200**, because 800 was the exact request that made the host's old
byte-budget loop re-encode a shrinking slice 505 times (MADR 0138 F14). A page
is one screen plus lookahead, not the whole ring.

The hardcoded `for (var page = 0; page < 32; ...)` bound, whose comment read
*"Safety bound: ring is ≤800"*, becomes `kHistoryMaxPages`.

**4.4 The FIFO trim is batched.** `_enforceCap` dropped a single item per event
once at the cap, copying the whole list and rebuilding the whole tool index each
time — tolerable at 800, five times worse at 4,000. It now cuts back to 75% in
one batch, the same shape as the host's eviction, and **shifts** the tool index
rather than rebuilding it: the map holds one entry per tool call, which is far
smaller than the transcript.

**4.5 `kTranscriptCacheMaxItems` stays at 150**, as the plan specified. Raising
the on-disk cache is a mobile storage decision with its own trade-offs.

### Fail-first evidence — two breakages, in a scratch copy of `apps/mobile`

```text
D1. the forward-only fetch restored
    FAIL the first fetch asks for the newest page, not the oldest
         Expected: true      Actual: <null>     ('newest' absent)
    FAIL an older page is requested with before_seq, not newest
         Expected: <98>      Actual: <null>

D2. historyBudgetBytes hardcoded to 0 and the cap put back to 800
    FAIL the client reads the host budget instead of assuming 800
         Expected: <33554432>  Actual: <0>
```

The first attempt at D1 did not run at all: the `cp` that was supposed to make
the scratch copy failed on a stale working directory, the `&&` chain stopped
before the edit, and the `flutter test` on the following line ran against the
**real tree** and passed. It was reported as a pass for the wrong code. Re-run
with absolute paths, both breakages failed as shown. The real tree was confirmed
untouched (`grep -c 'D1:'` → 0) before continuing.

### Verification

```text
dart format --output=none --set-exit-if-changed .  -> Formatted 208 files (0 changed)
flutter analyze                                     -> No issues found!
flutter test                                        -> 1406 passed, 3 skipped
```

### Deviation — 2026-09-03: the transcript-reducer cap test was rewritten

`soft cap drops oldest items` asserted the exact one-item-per-event trim
(`items.first.text == 'm50'`). Batched trimming makes that assertion false by
design. It now asserts the property — length within `[75%, 100%]` of the cap,
newest item always retained, oldest derived from the length — and is joined by
`a batched trim keeps the tool index pointing at the right items`, which catches
the off-by-one that shifting the index rather than rebuilding it could
introduce. The original name was kept so what it guarded stays findable.

### Phase 5 — 2026-09-03, complete

**5.1 Codex gets append semantics.** `chunkbuf` gains `WithToolLaneAppend()`,
and `mergeTool` becomes the method `mergeToolInto` so it can consult the
buffer's mode: replace keeps the newer text (correct for a provider sending the
current whole), append concatenates (correct for one sending an increment).
`internal/provider/codex/session.go` switches to the append lane.

Append mode needed one thing replace did not: the held text now *grows*, so it
is flushed at the buffer's byte cap rather than being allowed to become an
unbounded frame. `releaseTool` exists for that flush — it removes the hold
without merging, because the held event is already the merged state and merging
it with itself in append mode would duplicate its text.

**5.2 grok gets the lane.** `acpagent` now constructs with `WithToolLane()` in
replace mode, matching its `summarizeToolContent` emission. It was the only
provider whose payload shape made the lane safe and the only one that did not
have it — and, per MADR 0138 F3, the one whose event volume most needed it.

**A per-provider pin.** `TestEachProviderUsesTheToolLaneModeItsPayloadNeeds`
reads the four construction sites and asserts each matches the shape of its own
`tool_call_update`. The mode and the payload shape are decided in different
files; without this, flipping one silently discards (append→replace) or
duplicates (replace→append) an agent's command output. It checks the *absence*
of the append spelling for replace providers explicitly, because
`WithToolLaneAppend` contains `WithToolLane` as a substring.

### Fail-first evidence — four breakages

```text
E1. codex back on the replacing lane
    FAIL TestToolLaneConcatenatesNonTerminalUpdates
         text = "out 7", want every delta concatenated
         ("out 0out 1out 2out 3out 4out 5out 6out 7")
    FAIL TestEachProviderUsesTheToolLaneModeItsPayloadNeeds
         codex does not construct its buffer with chunkbuf.WithToolLaneAppend()

E1b. same, against the real notification decode path
    FAIL TestOutputDeltaNotificationsSurviveTheLane/item/commandExecution/outputDelta
         delivered "3 passed\n", want "compiling...\nrunning tests\n3 passed\n"
    (identical failure for item/fileChange/outputDelta — two of three lines
     of command output silently lost)

E2. kilo/opencode switched to the append lane
    FAIL TestEachProviderUsesTheToolLaneModeItsPayloadNeeds
         kilo/opencode (httpagent) uses the append lane; its updates are
         snapshots and would be duplicated

E3. grok's lane removed
    FAIL TestEachProviderUsesTheToolLaneModeItsPayloadNeeds
         grok (acpagent) does not construct its buffer with
         chunkbuf.WithToolLane()
```

```text
go build ./...                                   -> ok
go test -race ./internal/... ./cmd/... -count=1  -> ok (full tree)
```

### Deviations

**2026-09-03 — two existing codex tests pinned the defect and were rewritten.**
`TestToolLaneSupersedesNonTerminalUpdates` and
`TestToolLaneTerminalFlushesImmediately`
(`internal/provider/codex/tool_lane_baseline_test.go`, neither in this phase's
file list) asserted that the lane keeps only the **last** delta — the exact
behaviour MADR 0138 F2 identifies as data loss. They failed the moment the fix
landed, with `text = "out 0out 1…out 7", want last`.

*This is not a contradiction of MADR 0057, it is the completion of it.* 0057 M-2
reads: *"Measure Codex item streams and Goose tool updates before defaulting —
opt-in flag first if behavior differs."* Codex was opted into the replacing lane
without that measurement. 0138 F2 is the measurement, arriving late, and it
found that the behaviour does differ. The tests are rewritten to assert the
output survives *and* that the coalescing 0057 wanted still happens — 8 deltas,
1 frame — with both records named at the site. The first test's name changed
(`Supersedes` → `Concatenates`) because the old name asserts the defect.

**2026-09-03 — the acceptance criterion as written could not be met, and was
replaced rather than quietly dropped.** The step said to replay the codex wire
fixture and compare against the concatenation of its own deltas. That fixture
(`testdata/wire/0.152.1/frames.jsonl`) is a `hi` turn and contains **zero**
`outputDelta` frames — it never ran a command, so it cannot exercise this path.
Capturing one that does would spend codex quota against the live engine.

*Resolution:* `TestOutputDeltaNotificationsSurviveTheLane` feeds the real wire
JSON for both delta methods through `handleNotification`, so the decode in
`notifications.go` / `session.go` and the append lane it feeds are covered
together — the same ground the fixture would have covered, without a capture.
Verified to fail against the shipped code, as E1b above.

### Phase 6 — 2026-09-03, complete

**6.1 Context pressure (T1).** `internal/session/tokencost.go`. A notice at 75%
and again at 90% of the model's context window, fired on a *crossing* and
re-armed when usage drops back — a `/compact` should make the next climb worth
reporting again. It names the numbers (`1,526,598 of 2,000,000`, thousands
separators, because a seven-digit figure is unreadable without them) and offers
the remedy.

**Nothing is reported without a known window.** A provider that gives no `size`
gets no notice rather than a percentage of an unknown denominator. The
fail-first below shows what the guard prevents.

**6.2 was already delivered, and is recorded rather than re-done.**
`event.Usage` has carried `Input`, `Output`, `Reasoning`, `CacheRead`,
`CacheWrite` and `CostUSD` since MADR 0112 A4; MADR 0137 Phase 6 populated them
for all five providers; and `apps/mobile/lib/data/protocol/models.dart:708-745`
already parses every one. The per-turn cache accounting is on the wire and
reaching the phone today. The step's protocol change was unnecessary, which is
a better outcome than making it.

**6.3 Session totals (T3).** `Meta` gains `Turns`, `InputTokens`,
`OutputTokens`, `CachedTokens`, accrued at each turn end and persisted with the
record. Deliberately *cumulative*, where `event.Usage` is deliberately
per-turn — MADR 0112 A4 calls labelling a per-turn figure as a session total
"the specific error this split exists to avoid", so the two live in different
places and are named differently. A report carrying only a context total and no
per-turn tokens does not count as a turn.

**6.4 Structured quota for kilo (F9).** `internal/provider/kilo/quota.go`. When
`agenterr` classifies a limit from prose, the engine is asked
`GET /kilocode/provider-usage` and the structured answer is appended to the
error the phone sees.

The types are transcribed from the engine's **own OpenAPI document**
(`GET /doc`, `components.schemas.ProviderUsage*` on kilo 7.5.6), not inferred
from a sample: this host's account returns `{"items":[],...}`, so a sample would
have told us nothing about the fields that matter.

The more valuable half is the negative case. When the prose classifier fires and
structured usage does **not** confirm it, that is logged at warn — because that
is the day a vendor changed its wording and `internal/agenterr`'s 967 lines of
regular expressions matched something they should not have. It is the only
signal we would get.

grok's half of F9 (`x.ai/billing`, `x.ai/limit`) needs the ext-method plumbing
Phase 8 builds and is deferred to it.

### Fail-first evidence — four breakages

```text
F1x. thresholds moved to 101/102
     FAIL TestContextPressureNoticeAtSeventyFive
          75% of the context window must be reported

F2x. guess a 200k window when the provider reports none
     FAIL TestNoPressureNoticeWithoutAContextWindow
          usage {Used:900000 Size:0} produced a notice:
          "This session is using 900,000 of 0 context tokens (450%) …"

F3x. the exhausted-state comparison broken
     FAIL TestExhaustedWindowsSummarisesWhatTheEngineReports
          summary = "", want the provider and the exhausted resource

F4x. providerUsage.GeneratedAt renamed, against the live engine
     FAIL TestLiveProviderUsageAnswersTheShapeWeParse
          provider-usage no longer decodes into providerUsage:
          json: unknown field "generatedAt"
```

F2x's output is the point of the guard: **"900,000 of 0 context tokens (450%)"**
is exactly the confidently wrong number that MADR 0137 was corrected for twice.

### Live verification — no model tokens spent

`TestLiveProviderUsageAnswersTheShapeWeParse` (`-tags live_kilo`) is one GET
against a read-only endpoint; it invokes no model. Run against the running
engine:

```text
plans=0 generatedAt=2026-09-04T04:44:41.107Z exhausted=""
--- PASS (0.57s)
```

It decodes with `DisallowUnknownFields`, so an upstream rename fails loudly
rather than reading as a silent zero — demonstrated by F4x. Its skip path was
also exercised (unset env → SKIP, not a silent pass), because a skip that is
really a broken test is indistinguishable from one that ran.

```text
go build ./...                                   -> ok
go test -race ./internal/... ./cmd/... -count=1  -> ok (full tree)
```

### Deviation — 2026-09-03: 6.2 needed no work, and no protocol change was made

The step called for extending `event.Usage` with the cache fields and carrying
them to the phone, noting it *is* a protocol change that MADR 0137 Phase 2 had
deferred. Checked before building: the fields exist, are populated for all five
providers, and are parsed by the client. The step is recorded as already
satisfied. No protocol change was made, so none needs documenting or rolling
back.

### Phase 7 — 2026-09-03, complete

**7.1 (F4) Four handlers off the read loop.** `permission.respond`,
`question.respond`, `receipts.list` and `devices.list` now go through
`dispatchAsync`. Each took the deviceID snapshot itself; that is now the
parameter `dispatchAsync` passes, which is also the contract the `asyncHandler`
doc states ("handlers must use it instead of reading `c.deviceID`, which races
`setAuthed`").

`permission.respond` and `question.respond` get **15 s** — above the provider's
own 10 s call timeout, so the authoritative failure is the daemon's error frame
rather than this deadline firing first (MADR 0095 D7). The phone gets 25 s to
match the ladder.

The seven handlers still inline were reviewed and a verdict recorded in
`op_timeouts.json`'s comment: `auth` and `pair.claim` establish the connection's
identity, `session.pending_asks` must not queue behind the prompts it unblocks,
`oauth.cancel` is a cancel, and `permission.receipt` is a phone-signed reply on
a path that already has its own timeout.

**7.2 (F5) The ACP control send is bounded.** The blocking send in
`acpagent.deliver` runs on the SDK's single notification consumer, whose 1024
queue closes the whole connection on overflow. It now tries a non-blocking send
first, then waits up to 30 s, and on expiry **faults the session explicitly**
rather than dropping the event silently.

Stated plainly: no such stall has been observed, and the guard is conservative
by design. 30 s is far longer than any in-memory pump should take and only has
to be shorter than the time grok needs to queue 1024 notifications behind us.

**7.3 (F6) `session.shell` gets its own lane.** `maxShellPerClient = 2`,
counted separately from `maxAsyncPerClient = 8`. A 30-minute op and a 60-second
op no longer draw on one budget.

**7.4 (F11) The pair QR follows the bind.** `detectAdvertiseHost` takes
`cfg.Listen.Host`. Only "follow the config" binds — empty, `tailscale`, or a
wildcard — keep the Tailscale-IPv4 preference; an explicit bind advertises
itself. Reproduced end to end against the same config that produced the bug:

```text
BEFORE (installed 0.16.3)   Host: 100.64.0.3:7642   <- connection refused
AFTER  (this build)         Host: 127.0.0.1:7642    <- the address it listens on
```

**7.5 (F12) The auth-method warning is demoted to debug**, with the reason at
the site: it fired 90 times and was true zero of them.

### An unplanned finding: the async table was a hand-maintained shadow

`asyncDispatchedTypes()` was a literal list "hand-maintained on purpose", and
the list is what drifted. When Phase 7 moved four handlers onto the async path,
`TestEveryAsyncDispatchedMethodIsInTheTable` reported them as **stale entries** —
the exact opposite of the truth.

It is now derived from the source: it parses `handleMessage`'s switch, plus the
second registry in `codex_handlers.go` (`codexPhoneOperations`, keyed by type
with an explicit `timeoutKey`) that a scan of `handleMessage` alone would have
missed entirely, and maps constants to wire strings out of `messages.go`.

That immediately surfaced **pre-existing drift**: `session.list`,
`session.cancel`, `session.set_mode` and `session.set_config_option` have
reached `dispatchAsync` since MADR 0137 Phase 4 and were absent from
`op_timeouts.json`, silently taking `default_ms`. They are now listed at that
same 30 s — the table becomes honest, and no deadline changes.

### Fail-first evidence — three breakages

```text
G1. permission.respond inline again
    FAIL TestEveryAsyncDispatchedMethodIsInTheTable
         op_timeouts.json lists "permission.respond", which no longer
         reaches dispatchAsync

G3. the shell lane sharing the general budget
    FAIL TestShellDoesNotStarveThePrompt
         the shell lane (8) is not smaller than the general lane (8); it
         exists to bound the slow op, not to match the fast one

G4. the tailnet-only advertise restored
    FAIL TestPairAdvertisesTheBoundHost/loopback_ipv4
         detectAdvertiseHost("127.0.0.1") = "100.64.0.3:7531",
         want "127.0.0.1:7531"
```

**G2 was not run, and this says so rather than implying it was.** The step
called for `TestACPConnectionSurvivesAStalledPump`, driving the SDK's queue to
overflow against a stalled consumer. Writing it means standing up a real ACP
`Connection` with a scripted peer and pushing 1024+ notifications through it —
a test harness this package does not have, for a failure mode with no observed
instance. 7.2's guard is verified by reading the SDK source (the queue depth,
the overflow branch, and that `shutdownReceive` closes the connection) plus the
30-second bound being unreachable by any in-memory pump. That is weaker
evidence than the other three and is recorded as such.

### Verification

```text
go build ./...                                     -> ok
go test -race ./internal/... ./cmd/... -count=1    -> ok (full tree)
dart format --output=none --set-exit-if-changed .  -> clean
flutter analyze                                    -> No issues found!
flutter test                                       -> 1406 passed, 3 skipped
```

### Deviations

**2026-09-03 — a scripted edit over-matched and the file was recovered from
HEAD.** A `python3` pass meant to remove three `deviceID := c.deviceID`
snapshots matched the pattern **eleven** times across `internal/ws/server.go`
and removed all of them, breaking the build. The file's uncommitted content at
that moment was entirely this session's own edit from seconds earlier — checked
with `git diff --stat` and `git log -1` on that path before acting — so the
tracked baseline was restored with `git show HEAD:internal/ws/server.go >
internal/ws/server.go`, verified to build, and the change re-applied
handler-by-handler with the body bounded by the next `func` declaration. Same
recovery route, and the same class of mistake, as MADR 0137 Phase 7's basename
collision.

**2026-09-03 — the phone's timeout table needed an entry, which the plan did not
list.** `op_timeout_ladder_test.dart` requires the client's timeout to equal the
daemon's plus the margin *exactly*, so the daemon's new 15 s for the two respond
methods needed a matching 25 s in `opTimeoutFor`. Added, with the reasoning at
the site.

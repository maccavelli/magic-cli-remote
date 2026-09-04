---
status: proposed
date: 2026-09-04
decision-makers: maccavelli
consulted: —
informed: —
---

# Backward history paging is built, tested and unreachable, and the path that serves a transcript logs nothing when it succeeds

## Context and Problem Statement

MADR 0138 Phase 11 could not complete its acceptance criterion 4 — backward
history paging driven on a device against a session large enough to page. The
attempt failed for reasons that turned out to be more interesting than the
criterion: a seeded fixture rendered as an empty transcript, and **neither the
daemon nor the client said anything at any log level about why**. Diagnosing it
took an hour of reading source that a single log line would have answered.

This record is the assessment that followed, in two parts the investigation
could not separate:

* **Turn structure** — what a turn actually is in the transcript, what holds it
  together, and what a page boundary does to it.
* **Logging gaps** — what the daemon and client can tell an operator about a
  transcript they served.

Everything below was read from the source or measured on this host. Where a
conclusion is derived rather than observed, it says so.

### F1 — `sessionHistoryNewest` has no production caller

MADR 0138 F17 recorded: *"history paging is forward-only, and a chat screen
opens at the bottom."* Phase 3 implemented the fix on both sides — the daemon
grew `HistoryPageBefore` / `HistoryPageBeforeFor` and a `before_seq` / `newest`
branch in `internal/ws/server.go:1855`, and the client grew
`McremoteClient.sessionHistoryNewest` (`mcremote_client.dart:3550`) returning a
`HistoryPage` with a `prev_before_seq` cursor.

**Nothing in the app calls it.** Every reference to `sessionHistoryNewest` in
`apps/mobile` is in `test/session_history_paging_test.dart` — six call sites,
all tests — plus its own declaration:

```text
test/session_history_paging_test.dart:160,181,197,216,231,251
lib/data/ws/mcremote_client.dart:3550          (the declaration)
```

The only production history calls are `sessionHistory(id)` at
`state/session_synchronizer.dart:156` and `:200`, which is the **forward**
walk.

So F17's defect is unfixed in the shipped application. The transport and the
daemon endpoint are correct and unit-tested; the wire between them and the chat
screen was never run.

This also explains an observation from Phase 11's device testing that was
recorded there as inconclusive: scrolling back through a 773-event session
produced **no history request at all**. It could not have. Nothing asks.

### F2 — the forward walk fetches the oldest 6,400 events, not the newest

`sessionHistory` (`mcremote_client.dart:3474-3532`) starts at `since_seq = 0`
and pages *forward*:

```dart
for (var page = 0; page < kHistoryMaxPages; page++) {
  ... 'since_seq': sinceSeq ...
  if (!truncated || nextSince <= sinceSeq) return out;
  sinceSeq = nextSince;
}
```

with `kHistoryMaxPages = 32` and `kHistoryFetchLimit = 200`
(`chat_models.dart:75,80`). The walk therefore stops after at most **6,400
events**, counted from the oldest.

For a session larger than that, the app fetches events 1..6,400 and never
requests the rest. The newest content — the part a chat screen exists to show —
is the part not fetched. `kMaxTranscriptItems = 4000` then evicts down to a
window that is *older still*.

**Derived, not observed.** No session on this host is large enough to reach it:
the three largest are 773, 629 and 606 events. The reasoning is a direct read of
the loop bounds, and it is exactly the shape F17 described, but it has not been
reproduced on a device.

A related inconsistency sits in the same doc comment
(`mcremote_client.dart:3473`):

```dart
/// [limit] defaults to [kHistoryFetchLimit] (800).
```

The constant has been **200** since MADR 0138 Phase 1. The comment overstates
the walk's reach by 4×, which is the kind of drift that makes a bound look
adequate on a reading.

### F3 — `Store.LoadHistory` discards a whole transcript silently

`internal/session/store.go:299-313`:

```go
func (s *Store) LoadHistory(id string) []event.Event {
	b, err := os.ReadFile(s.historyPath(id))
	if err != nil {
		return []event.Event{}
	}
	var hf historyFile
	if json.Unmarshal(b, &hf) != nil {
		return []event.Event{}
	}
	if hf.Events == nil {
		return []event.Event{}
	}
	...
}
```

Three ways to lose an entire session transcript, none of which logs, returns an
error, or is distinguishable by the caller from "this session has no history".

Measured directly against a 3.5 MB file of otherwise well-formed events:

```text
fixture written: 3490304 bytes
LoadHistory returned 0 of 20000 events
```

**The Store has a logger.** `Store` carries `log *slog.Logger`, set to
`slog.Default()` at `OpenStore` (`store.go:56-68`). It is used in exactly one
function:

| function | logs a failure? |
| --- | --- |
| `List` | **yes** — `"session list skipped unreadable meta"`, `"session list skipped corrupt meta"` |
| `LoadHistory` | no |
| `SaveHistory` | no |
| `Save`, `Get`, `Delete`, `LoadEpoch`, `SaveEpoch`, `ResolveAgentSession`, `atomicWrite` | no |

The asymmetry is the point. A corrupt `meta.json` produces a warn line naming
the session. A corrupt or unreadable `history.json` — the same directory, the
same failure class, and far more data — produces nothing.

*What made this concrete.* The Phase 11 fixture was written as a bare JSON array
while the real format is `{"events":[…]}` — verified across all 34 stored
transcripts. That was an authoring mistake, not a product defect, and the
product's strictness is correct. But the cost of the mistake was an hour,
because a 3.5 MB file was read, rejected and discarded without a word.

### F4 — 43 of 51 WebSocket handlers log nothing on success

`internal/ws/server.go` handlers, classified by whether their body contains any
`Info` or `Debug` call:

```text
logs      8
SILENT   43
```

`handleSessionHistory` is one of the silent ones: it can serve an empty page, a
full page, or 6,400 events across 32 round trips, and the daemon log is
identical in all three cases.

**The failure path is already covered, and that is what makes the gap
specific.** `writeError` (`server.go`) logs every error frame at `Info` with
code, request id, device and message — MADR 0069 D7 added it precisely because
*"most error frames reached the phone with no daemon log line at any level, so
diagnosing meant a screenshot of the chat instead of a grep."*

The remaining blind spot is narrower and harder to notice: **a request that
succeeds and returns nothing**. `session.history` returning zero events is not
an error at any layer — `HistoryPageBeforeFor` documents that "History returns
an empty (non-nil) slice for an unknown/never-active session — replay is not an
error" (`server.go:1853-1854`) — so no error frame is written, nothing is logged, and
the phone shows an empty chat. That is exactly the state Phase 11 spent an hour
inside.

By contrast, the **turn** path is well instrumented. MADR 0137 Phase 2's
`"turn latency"` record (`internal/session/turnlatency.go:134`) carries
`turn_ms`, `ttft_ms`, `cold`, `context_used`, and the full token split; 23 such
records sit in the operator's current log across all five providers. Turns are
observable. Transcripts are not.

### F5 — a turn has no identity, and its boundary is a client-side latch

There is no turn id anywhere in `event.Event`. A turn is implied by position: a
`user_message` opens one, a `turn_complete` closes it (`StopReason` set "when
known", `event.go:641`), and everything between belongs to it by adjacency.

What actually holds an assistant message together is optional provider identity:

```go
NativeMessageID string `json:"native_message_id,omitempty"`
NativePartID    string `json:"native_part_id,omitempty"`
```

**Exactly one provider sets them** — opencode (`internal/provider/opencode/parts.go`,
`http.go`). grok emits none, which MADR 0138 Phase 9 established independently
when it chose `UndoSession` over `RevertSession` for exactly that reason. codex,
kilo and goose set none either.

So for four of five providers, every streamed chunk takes the id-less path in
`_reduceStreamed` (`transcript_reducer.dart:539-543`) and is folded into the
previous bubble by adjacency:

```dart
if (!t.sealedTail &&
    t.items.isNotEmpty &&
    t.items.last.kind == ChatItemKind.assistant &&
    t.items.last.nativeMessageId == null) {
  // fold into the previous bubble
```

`sealedTail` is the only thing standing between two different turns' text
merging into one bubble, and it is set in exactly **one** place —
`transcript_cache.dart:104`, when a transcript is restored from the on-disk
snapshot, with the comment: *"The snapshot may end mid-conversation; the next
live chunk must not merge into a restored bubble (it may be a different turn)."*

That is the correct instinct, and it has a hole with the same shape at the other
end. **There is no `sealedHead`.** A backward page prepends older events, and the
last item of a prepended page is adjacent to the first item of what was already
there — two chunks from different turns, both id-less, both assistant. The
folding rule above does not consider prepends at all.

This is **latent, not live**, and only because of F1: nothing prepends a page
today. It becomes reachable the moment F1 is fixed, which makes it a
prerequisite of that work rather than a separate defect.

### What this costs today

* F17 is recorded as fixed by 0138 Phase 3 and is not fixed in the app (F1).
* A session past 6,400 events shows its oldest content as though it were its
  newest (F2, derived).
* A transcript that fails to load is indistinguishable from a session that never
  had one, at every layer, in every log (F3, F4).
* Wiring the backward pager without addressing the head boundary would merge
  turns across page joins for four of five providers (F5).

## Decision Drivers

* F1 means an accepted MADR records a fix that users do not have. That is worse
  than an open defect, because it stops anyone looking.
* The debugging cost in F3/F4 is already paid once and measured: an hour, on a
  failure whose entire explanation was one unlogged branch.
* F5 has to be settled *before* F1 is wired, not after, or the first large
  session to be paged will show merged turns and the cause will be two changes
  back.
* Logging that only covers the failure path leaves the worst state — a
  successful request that returns nothing — the single least observable outcome
  in the system.
* None of this needs new protocol. The daemon endpoint, the client method and
  the cursor all exist and are tested.

## Considered Options

* **Wire the backward pager, seal the head, and log both ends of the history
  path** — treat F1/F5 as one change and F3/F4 as its instrumentation.
* **Wire the backward pager only** — fix F1, leave the boundary and the logging.
* **Log first, wire later** — instrument F3/F4 now, and treat the paging work as
  a separate follow-up.
* **Raise the forward walk's page budget** — leave the architecture alone and
  make `kHistoryMaxPages × kHistoryFetchLimit` large enough to cover any real
  session.

## Decision Outcome

Chosen option: **"Wire the backward pager, seal the head, and log both ends of
the history path"**, because F1 and F5 are the same change — the head boundary
is only reachable through the code path F1 turns on, and shipping either alone
leaves a known defect that the other one exposes. The logging is included rather
than deferred because it is what makes the paging work verifiable at all: this
record exists because an empty transcript could not be explained from either
side, and criterion 4 cannot be honestly acceptance-tested while a served page
leaves no trace.

Concretely:

1. `session_synchronizer` opens a chat on `sessionHistoryNewest` and pages
   backward on scroll, bounded by `kHistoryMaxPages`.
2. A `sealedHead` latch mirrors `sealedTail`, so a prepended page cannot fold
   its last chunk into the first item of the existing transcript.
3. `LoadHistory` logs each of its three discard paths at warn, naming the
   session and the reason, matching what `List` already does for `meta.json`.
4. `session.history` logs one line per served page — session, device, direction,
   cursor, events returned, truncated — at debug, and at warn when it serves
   zero events for a session the store has a file for.
5. The stale `(800)` in `mcremote_client.dart:3473` is corrected to 200.

### Consequences

* Good, because F17 becomes true of the application and not only of the
  transport, which is what the original finding claimed.
* Good, because a chat screen opens on the newest page rather than walking from
  the oldest, which is both correct and cheaper on every session.
* Good, because the next empty transcript is a grep instead of an hour: the
  daemon says what it served and the store says what it refused.
* Good, because 0138's criterion 4 becomes verifiable — a served page leaves a
  record, so a device test can assert paging happened rather than inferring it
  from pixels.
* Neutral, because no protocol or storage format changes; every wire message
  involved already exists and is tested on both sides.
* Bad, because it adds log volume to a per-scroll path. Debug level for the
  per-page line keeps it off by default, and the warn line fires only on a state
  that is already wrong.
* Bad, because `sealedHead` adds a second latch to a reducer that is already the
  subtlest code in the client, and two latches can disagree. They are testable
  together, and F5 shows the alternative is worse.
* Bad, because it does not fix F2 for a session past 6,400 events that the user
  scrolls *all* the way back through — `kHistoryMaxPages` still bounds the
  walk. It changes which end the bound cuts off, from "cannot see the newest" to
  "cannot see the very oldest", which is the right end to lose.

### Confirmation

* `sessionHistoryNewest` has a production caller, asserted by a test that scans
  `lib/` — the same shape as the guard that catches an experimental endpoint in
  `internal/provider/opencode/surface_contract_test.go`, and for the same reason:
  the defect is an absence, and only a scan sees an absence.
* A transcript assembled from two backward pages does not merge the last item of
  the older page into the first of the newer, for an id-less provider.
* `LoadHistory` on an unreadable, malformed, and events-less file each produce a
  distinct warn line naming the session; verified by driving all three.
* A `session.history` call that serves zero events for a session with a history
  file on disk produces a warn line. Verified by reproducing the Phase 11
  fixture failure and checking the log now explains it.
* Criterion 4 re-run on the emulator against a **correctly formatted** fixture
  (`{"events":[…]}`), with the daemon log asserting that more than one backward
  page was served.

## Pros and Cons of the Options

### Wire the backward pager, seal the head, and log both ends of the history path

* Good, because it fixes the defect F17 named, in the layer that still has it.
* Good, because it closes the head boundary in the same change that makes it
  reachable, so it is never briefly shippable-and-wrong.
* Good, because the instrumentation is what makes the rest verifiable rather
  than asserted.
* Neutral, because it is larger than any one of its parts and needs its own plan
  phase.
* Bad, because it touches the transcript reducer, the synchronizer, the store
  and the ws layer in one change — four files whose failure modes are all
  action-at-a-distance.

### Wire the backward pager only

* Good, because it is the smallest change that makes F17 true.
* Bad, because F5 becomes live the moment it lands: prepended pages merge turns
  for grok, codex, kilo and goose.
* Bad, because its own acceptance cannot be checked. A served page leaves no
  trace, so "paging happened" stays an inference from the screen.

### Log first, wire later

* Good, because it is small, safe, and pays back immediately on the next
  unexplained transcript.
* Good, because it is the half that would have prevented this record's own
  investigation cost.
* Bad, because it leaves F17 recorded as fixed and not fixed, which is the most
  expensive item here.
* Neutral, because it is a strict subset of the chosen option and could be
  landed first within it if the phase needs splitting.

### Raise the forward walk's page budget

* Good, because it is a one-constant change with no new code paths.
* Bad, because it does not fix the ordering. The walk still starts at the oldest
  event, so a chat screen still assembles the whole transcript to show its tail,
  and the cost grows with session length rather than staying flat.
* Bad, because it makes the byte budgets and eviction work harder to no end: the
  events fetched first are the ones `kMaxTranscriptItems` evicts.
* Bad, because it discards the backward endpoint and client method that already
  exist and are tested, in favour of a bound that has already been raised once.

## More Information

* **F1**: `apps/mobile/lib/data/ws/mcremote_client.dart:3550` (declaration),
  `apps/mobile/test/session_history_paging_test.dart:160,181,197,216,231,251`
  (the only callers), `apps/mobile/lib/state/session_synchronizer.dart:156,200`
  (what production uses instead).
* **F2**: `mcremote_client.dart:3474-3532`;
  `apps/mobile/lib/data/chat/chat_models.dart:67,75,80`. Largest transcripts on
  this host: 773 / 629 / 606 events across 34 sessions.
* **F3**: `internal/session/store.go:299-313`, logger at `:56-68`, the one
  logging function at `:179,188`. Measured with a 3,490,304-byte fixture through
  `OpenStore` + `LoadHistory`.
* **F4**: `internal/ws/server.go` — 8 handlers log on success, 43 do not;
  `handleSessionHistory` at `:798`/`:1840`, `writeError` logging per MADR 0069
  D7. Turn instrumentation for contrast:
  `internal/session/turnlatency.go:134`, 23 records in
  `~/Library/Logs/mcremote/mcremote.err.log`.
* **F5**: `internal/event/event.go:641,687-696`;
  `internal/provider/opencode/parts.go` and `http.go` (the only providers
  setting native ids); `apps/mobile/lib/data/chat/transcript_reducer.dart:531-561`
  and `:708,730`; `apps/mobile/lib/data/chat/transcript_cache.dart:104`.
* Predecessor: MADR 0138's F17 and its Phase 3, and Phase 11's criterion 4,
  whose failed device run produced this assessment. This record does not
  supersede 0138 — its Phase 3 work is correct and is what makes the chosen
  option small — but it corrects the impression that F17 is closed.
* No implementation plan is paired with this record yet. Per `AGENTS.md`, none
  of the above is implemented until a plan exists and is approved.

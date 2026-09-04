---
status: in-progress
date: 2026-09-04
associated-madr: "0141-MADR-backward-paging-is-unreachable-and-the-history-path-is-unobservable.md"
---

# Implement: reach the backward pager, seal the page join, and make the history path observable

Associated MADR:
[0141-MADR-backward-paging-is-unreachable-and-the-history-path-is-unobservable.md](0141-MADR-backward-paging-is-unreachable-and-the-history-path-is-unobservable.md)

## Goal

A chat opens on the newest page of its transcript and pages backward as the
user scrolls, and every step of that is visible in the daemon log.

When this is complete:

* The 900-turn fixture opens on **turn 900**, not turn 291.
* Scrolling back fetches older pages, and each one leaves a log line.
* A transcript the store refuses to load says so, naming the session and why.
* Two separately fetched pages join without merging one turn's text into
  another's.

## Scope

### In scope

| item | MADR reference |
| --- | --- |
| `LoadHistory` logs its three discard paths | Decision Outcome 3 |
| `session.history` logs served pages, and warns on a silent empty | Decision Outcome 4 |
| `sealedHead` mirrors `sealedTail` | Decision Outcome 2 |
| Chat open and background resync both use the newest-page path | Decision Outcome 1 |
| Scroll-triggered backward paging | Decision Outcome 1 |
| The stale `(800)` doc comment | Decision Outcome 5 |

### Out of scope

* **Raising `kHistoryMaxPages`.** The MADR rejected it as an option and it
  remains rejected: this changes which end the bound cuts off, and 32 pages of
  200 from the newest end is the right shape. The constant is not touched.
* **A turn id in `event.Event`.** F5 describes the absence; the chosen fix is a
  head latch, not a protocol change. A turn id would be a larger decision and
  needs its own record.
* **`available_commands` volume.** MADR 0138 F16 measured it at 44% of
  transcript bytes. Real, and not this record's problem.
* **The daemon-overwrites-a-late-directory behaviour** noted in 0138 Phase 11.
  Still unconfirmed against real data, and unrelated to paging.

## Implementation Steps

Four phases. Phase 1 first because it is what makes phases 3 and 4 verifiable —
without it, "a page was served" is unobservable, which is the state that
produced this record.

### Phase 1 — Make the history path observable (Go)

**1.1 `LoadHistory` names what it discarded.** `internal/session/store.go:299`.
Each of the three empty returns logs at warn with the session id and a distinct
reason: the file could not be read, the JSON did not parse, the parsed object
carried no `events` key. `Store` already holds `log` (`store.go:58`) and `List`
already logs the analogous `meta.json` failures (`:179,188`) — this is the same
line for the same class in the same directory.

A file that is genuinely absent must **not** warn: a session with no transcript
yet is the normal cold case, and warning on it would train the reader to ignore
the line. Distinguish `os.IsNotExist` from every other read error.

**1.2 `session.history` says what it served.** `internal/ws/server.go:1840`.
One debug line per served page: session, device, direction (`forward` /
`backward`), the cursor in (`since_seq` / `before_seq`), events returned,
`truncated`, and the cursor out.

One warn line for the state that has no other signal: **zero events returned
for a session whose history file exists on disk**. That is the exact condition
that cost this record's investigation an hour, and it is invisible at every
other layer — it is not an error, so `writeError` never fires.

### Phase 2 — Make a page join safe (Dart), inert until Phase 3

**2.1 `sealedHead`.** `lib/data/chat/chat_models.dart`, mirroring `sealedTail`
(`:490`): a bool on `SessionTranscript`, defaulted false, carried through
`copyWith`.

**2.2 The reducer respects it.** `lib/data/chat/transcript_reducer.dart`. The
two id-less folding sites (`:708`, `:730` — `_appendAssistant` and its thought
twin) already refuse to fold across `sealedTail`. Prepending needs the mirror:
when items are concatenated ahead of an existing transcript, the **last item of
the older page** must not absorb the **first item of the newer**.

**2.3 `prependHistory`.** `lib/state/transcripts_notifier.dart`. Builds the
older page into its own `SessionTranscript`, marks that transcript's tail
sealed, and concatenates its items ahead of the current ones. Updates
`_firstSeq` to the page's oldest seq.

It reuses `_applyChunked`'s batching discipline rather than applying 200 events
on one frame, and it must uphold the same invariants that method documents —
continue from the transcript `_commit` returned, and record seqs only once the
covering commit happened.

### Phase 3 — Wire the pager (Dart)

**3.1 Both history call sites move together.** This is the step with a
non-obvious dependency, and getting it half-right is worse than not doing it.

`resyncHistory` (`transcripts_notifier.dart:853`) **rebuilds the transcript from
scratch** out of the events handed to it, and its guard explicitly permits
rebuilding downward when `missedOlder` is true:

```dart
if (maxSeq < last && !missedOlder) return;
```

So if chat-open switched to the newest page while
`session_synchronizer.dart:156` kept calling `sessionHistory(id)` — the forward
walk — the background resync would hand it the **oldest** 6,400 events, see
`minSeq < _firstSeq`, and rebuild the user's view down to turn 291 while they
were reading turn 900.

Both call sites (`:156` in `resync`, `:200` in `ensureSession`) therefore move
to the newest-page path in the same change as chat-open.

**3.2 Scroll triggers the older page.** `lib/features/chat/chat_screen.dart`.
The list is `reverse: true` (`:2851`), so offset 0 is newest and `pixels` grows
toward older. `_onScroll` (`:647`) already runs on every scroll; it gains a
check for approaching `maxScrollExtent` that requests the next older page via
the `prev_before_seq` cursor from the last `HistoryPage`.

Bounded by `kHistoryMaxPages` per session, single-flight (a fling must not
launch eight overlapping fetches), and it stops when `prev_before_seq` is 0 —
the daemon's signal that nothing older remains.

**3.3 The stale comment.** `mcremote_client.dart:3473` says the limit defaults
to 800; it is 200.

**3.4 A caller guard.** A test that scans `lib/` and fails if
`sessionHistoryNewest` has no production caller. The defect this record exists
for was an **absence**, and only a scan sees an absence — the same reasoning as
`internal/provider/opencode/surface_contract_test.go`'s A11 guard, which caught
a real defect in Phase 11.

### Phase 4 — Re-run criterion 4 on the device

Re-seed the 900-turn fixture with `scripts/gen-transcript-fixture.py`, validate
it through `Store.LoadHistory` **before** seeding, and open it on the emulator.

## Verification

```bash
make pre-add-check FILES="<changed go files>"
for os in windows linux darwin; do GOOS=$os go vet ./internal/... ./cmd/...; done
go test -race ./internal/... ./cmd/... -count=1
cd apps/mobile && flutter analyze && dart format --output=none --set-exit-if-changed lib test && flutter test
```

### Fail-first evidence required

Each against a scratch copy, never the working tree, and each edit script must
assert **from disk** that its edit landed.

* `R1` — remove the `os.IsNotExist` guard from 1.1; a test asserting a cold
  session does not warn must FAIL.
* `R2` — make `LoadHistory`'s parse failure return empty without logging; a test
  asserting the reason is named must FAIL.
* `R3` — serve zero events for a session with a history file and assert no warn;
  must FAIL.
* `R4` — drop `sealedHead` from the prepend; a test asserting two pages join
  without merging turn text must FAIL.
* `R5` — leave `session_synchronizer.dart:156` on the forward walk while
  chat-open uses the newest page; a test asserting a background resync does not
  rewind a paged transcript must FAIL. This is 3.1's dependency, and it is the
  one a reviewer would most likely wave through.
* `R6` — remove the production call to `sessionHistoryNewest`; the scan guard
  must FAIL.

### Acceptance criteria

1. A cold session logs nothing; an unreadable, unparseable, or events-less
   history file each log a distinct warn naming the session.
2. Every served `session.history` page logs one debug line carrying its
   direction and cursors; a zero-event page for a session with a file on disk
   logs a warn.
3. A transcript assembled from two backward pages does not merge the last item
   of the older page into the first item of the newer, for an id-less provider.
4. A background `resync` while the user is on a backward-paged transcript does
   not rewind it.
5. `sessionHistoryNewest` has a production caller, pinned by a scan.
6. On the emulator, the 900-turn fixture opens on **turn 900 of 900**, and
   scrolling back fetches older pages — each visible in the daemon log from
   Phase 1, which is what turns this from a screenshot into evidence.

## Rollout and Rollback

**Rollout.** Phase 1 is daemon-only and independently shippable. Phases 2–3 are
client-only and ship together in an APK; phase 2 alone is inert.

**Compatibility.** No protocol change. The `before_seq` / `newest` request and
the `prev_before_seq` response field are already implemented, tested and
shipped on the daemon since MADR 0138 Phase 3 — this plan only starts calling
them. An older client keeps working against a newer daemon, and a newer client
against an older daemon requires the daemon to be at least the version that
shipped Phase 3.

**Rollback.** Phase 1 reverts alone. Phases 2–3 revert together; reverting 3
without 2 is safe but pointless, and reverting 2 without 3 would leave the
prepend path unsealed, so they revert as a pair.

## Execution Record

### Phase 1 — 2026-09-04, complete

**1.1** `LoadHistory` warns on each of its three discard paths, naming the
session. A genuinely absent file stays quiet: a cold session is the normal case,
and warning on it would teach the reader to skip the line.

The parse warning carries the hint that a bare JSON array decodes as no events.
That sentence is the whole value of the line — "did not parse" alone sends a
reader looking for corruption in a file that is well-formed JSON.

**1.2** `logHistoryPage` records session, device, direction, both cursors, event
count, `truncated`, and the ring bounds, at debug. The warn fires on one state
only: **zero events for a session that demonstrably has them**, asked for
without a cursor. A cursor-bounded page returning zero is the normal end of a
backward walk and is excluded, or the signal would fire on every completed walk.

No new Manager API was needed — `SeqBounds` already reports `latest`, which is
what makes "the session has events and we served none" expressible.

### Fail-first evidence — three breakages, each on a scratch copy

```text
R1. the os.IsNotExist guard removed from LoadHistory
    FAIL TestColdSessionDoesNotWarn
         a session with no history file warned: … msg="session history
         unreadable; transcript dropped" session_id=cold-1 …

R2. the parse failure returns empty without logging
    FAIL TestUnparseableHistoryWarnsAndNamesTheContainer
         a bare array did not warn / does not name the session / does not say
         what the container must be

R3. the silent-empty warn removed from logHistoryPage
    FAIL TestSilentEmptyPageWarns
         serving nothing for a session with 19,800 events did not warn:
         level=DEBUG … events=0 … latest_seq=19800
```

R3's output is worth keeping: the debug line correctly reports `events=0
latest_seq=19800`, and that is exactly the state a reader would scroll past.
The warn exists because the data being present in a debug line is not the same
as anyone noticing it.

**R1's first attempt asserted the wrong thing.** The edit landed and the test
failed correctly, but the script's own check —
`assert 'os.IsNotExist' not in source` — failed because the string appears twice
in `store.go`. Re-scoped to `LoadHistory`'s function body and re-read from disk.
The same over-broad-assertion mistake as MADR 0138 Phase 9's M1, in the same
shape: a whole-file substring check standing in for a scoped one.

### Verification

```text
for os in windows linux darwin; go vet ./internal/... ./cmd/...  -> all clean
go test -race ./internal/... ./cmd/... -count=1                  -> no failures
make pre-add-check FILES=…                                       -> 4 file(s) clean
```

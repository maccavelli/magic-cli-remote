---
status: complete
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

### Deviation — 2026-09-04: `toolIndex` holds positions, and `sealedHead` is redundant

Two findings from implementing Phase 2, one the plan missed and one where it
asked for machinery that is not needed.

**`toolIndex` invalidates on prepend, and the plan never mentions it.**
`chat_models.dart:507` is `Map<String, int>` — tool id to an **index into
`items`** — and `_upsertTool` dereferences `t.items[i]` directly. Prepending N
older items shifts every existing entry by N, so the next `tool_call_update`
either writes to the wrong card (the kind guard only checks that the target is
*a* tool) or falls through and appends a duplicate.

This is not optional: any prepend must rebase the map. `toolIndex` is the only
position-bearing state on the transcript — `_indexOfNative` scans backwards, so
identified items are position-independent.

**`sealedHead` is redundant under concatenation.** The latch exists to stop the
older page's last chunk folding into the newer page's first item. Folding
happens only *during reduction* (`_appendAssistant`, `chat_models`' id-less
path). If `prependHistory` reduces the older page into its **own** transcript
and then concatenates the two item lists, no reduction ever crosses the join, so
nothing can fold across it.

The two approaches produce identical output. Where a page boundary falls
mid-message — roughly one boundary in seven, at ~3 chunks per assistant message
and 22 events per turn — both render two adjacent bubbles, because forcing that
split is exactly what `sealedHead` was for.

*Resolution chosen.* Concatenate, rebase `toolIndex`, and do not add
`sealedHead`. The guarantee becomes structural rather than a flag, which removes
the cost the MADR itself named for this option: *"`sealedHead` adds a second
latch to a reducer that is already the subtlest code in the client, and two
latches can disagree."* They cannot disagree if there is only one.

*One edge case the concatenation must guard.* Prepending onto an **empty**
transcript would leave `items.last` belonging to the older page, and the next
live chunk would fold into it across an arbitrary gap. There is nothing to page
back from in that state, so `prependHistory` refuses it rather than handling it.

*Scope change.* Steps 2.1 and 2.2 are dropped. Step 2.3 gains the `toolIndex`
rebase and the empty-transcript guard. Acceptance criterion 3 is unchanged — it
asserts the outcome, not the mechanism.

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

### Phase 2 — 2026-09-04, complete

**2.1 and 2.2 dropped**, per the deviation above: `sealedHead` is redundant
under concatenation.

**2.3** `prependHistory` reduces the older page into its own transcript,
yielding every `kHistoryApplyBatchSize` events so a 200-event page does not
block a frame, and commits once — a half-prepended transcript would visibly
jump. It then concatenates the two item lists and rebases `toolIndex`.

Three details that are load-bearing rather than defensive:

* **It re-reads the transcript after the yields.** Live events can append while
  the page is being reduced, and the snapshot taken before the first yield would
  drop them.
* **It refuses an empty transcript.** There is nothing to page back from, and
  prepending would leave `items.last` owned by the older page for the next live
  chunk to fold into.
* **A generation guard** cancels the reduction if another history operation
  starts, matching `_applyChunked`'s discipline.

### Fail-first evidence — two breakages, on a scratch copy of `apps/mobile`

```text
R4a. toolIndex merged without rebasing
     FAIL a tool update after a prepend still finds its own card
          Expected: contains 'NEW RESULT'
            Actual: ''
     — the newer card never receives its own update, because its index now
       points into the prepended page.

R4b. the join folds, as an unguarded reduce would
     FAIL a prepended page does not merge its last bubble into the newer page
          Expected: <2>   Actual: <1>
          the two pages' assistant messages merged into one bubble:
          [OLDER ANSWERNEWER ANSWER]
```

**R4b failed to fail on its first attempt, and the test was wrong, not the
code.** The original version put a `user_message` at the head of the newer page,
so the join was never assistant-to-assistant — and the reducer only folds an
id-less assistant chunk into a preceding id-less assistant bubble. It passed
against a deliberately broken join because it asserted something true about a
case that cannot fail.

Rewritten so the newer page **opens mid-message**, which is also the realistic
shape: a 200-event boundary lands mid-message roughly one time in seven, at ~3
chunks per assistant message and 22 events per turn. The comment on the test
records why, so the next reader does not re-simplify it back.

This is the second time in this record that a check had to be corrected rather
than the code — the same discipline that caught MADR 0138 Phase 7's guard.

### Verification

```text
dart format --output=none --set-exit-if-changed lib test  -> 209 files, 0 changed
flutter analyze                                           -> No issues found
flutter test                                              -> 1410 passed
```

Phase 2 is inert: nothing calls `prependHistory` until Phase 3.

### Phase 3 — 2026-09-04, complete

**3.1** Both history call sites in `session_synchronizer.dart` now fetch the
newest page. They moved together, as the plan required: leaving the periodic
resync on the forward walk would have handed `resyncHistory` the oldest events,
and its `missedOlder` branch would have rebuilt a backward-paged transcript down
to the beginning while the user was reading the end. `R5` is that hazard as a
test.

With the newest page, resync is a **no-op** for a paged-back transcript:
`minSeq` is 19,601 against a local `_firstSeq` of 300, so `missedOlder` is false
and `missedNewer` is false, and it returns early. The clobber cannot happen.

**3.2** `_onScroll` triggers `_loadOlderHistory` one viewport before
`maxScrollExtent` — the oldest end of a `reverse: true` list — so the page lands
before the user arrives. Bounded three ways: a single-flight latch (a fling
crosses the trigger band many times in a few frames), `kHistoryMaxPages` per
screen, and the daemon's `prevBeforeSeq` going null at the oldest retained
event. A failed page does not latch the screen shut; scrolling away and back
retries.

**3.3** The `(800)` in `mcremote_client.dart:3473` is now `(200)`.

**3.4** `test/backward_paging_wired_test.dart` scans `lib/` and fails if
`sessionHistoryNewest` has no production caller, or if a forward walk reappears
in the synchronizer. The defect this record exists for was an **absence**, and
no test that exercises code can see an absence.

A public `TranscriptsNotifier.oldestSeq` replaced a reach into the
`@visibleForTesting` `debugFirstSeq`, which the analyzer correctly rejected.

### An existing test's fake was updated, deliberately

`test/session_synchronizer_test.dart`'s fake client overrode `sessionHistory`
only, so four tests failed against the real (unconnected) client once the
synchronizer moved. The fake gained a `sessionHistoryNewest` override returning
the same canned events as a `HistoryPage`, and incrementing the same
`historyCalls` counter so every existing assertion keeps measuring what it
measured before. The contract changed on purpose; the fake follows it.

### Fail-first evidence — two breakages, on a scratch copy

```text
R5. the synchronizer left on the forward walk
    FAIL the forward walk is no longer what a chat opens on
         Expected: false  Actual: <true>
         a forward walk left in the synchronizer would hand resyncHistory the
         oldest events, and its missedOlder branch would rebuild a
         backward-paged transcript down to the beginning …

R6. the production callers removed
    FAIL sessionHistoryNewest has a production caller
         Expected: non-empty  Actual: []
         nothing in lib/ calls sessionHistoryNewest …
```

`R6` is the regression that produced this whole record, reproduced on demand.

### Verification

```text
dart format --output=none --set-exit-if-changed lib test  -> 210 files, 0 changed
flutter analyze                                           -> No issues found
flutter test                                              -> 1412 passed
```

### Phase 4 — 2026-09-04, complete

Run against the emulator with the Phase 3 client and a build of `master`
installed as the daemon, using the same 900-turn fixture that produced MADR
0141 F2. Validated through `Store.LoadHistory` before seeding —
`authored=19800 LoadHistory=19800` — rather than after.

**The fix, on device:**

```text
before (v0.16.5 client):  TURN 291 of 900   ending mid-turn, 68% never fetched
after  (this change):     TURN 900 of 900   the actual end of the conversation
```

**Backward paging, on device.** The newest page is 200 events ≈ 9 turns, so it
covers turns 891–900. Scrolling back reached **turns 855–857**, which is roughly
36 turns and 800 events older — at least four backward pages fetched and
prepended, none of which the old client could reach at all.

**The designed trade, caught in the wild.** At the top of that screen sits a
bubble containing only `"The"`, with `"working tree was clean at TURN 855 of
900…"` as the bubble beneath it. Turn 856's message renders as one bubble; turn
855's is split in two. That is a page boundary falling between the first and
second chunk of one assistant message, and the concatenation keeping them apart
instead of merging across the join — exactly the behaviour the Phase 2 deviation
chose, at the frequency it predicted (~1 boundary in 7).

Seeing it is worth more than the unit test: it confirms the join is real traffic
rather than a hypothesis, and that the failure mode when it triggers is a split
bubble and not a misattributed turn.

### What Phase 4 could not show

The per-page debug line was **not** observed. The daemon's configured log level
is `info` (`~/.config/mcremote/config.yaml`), so the `session history page`
records never reached the file, and the operator's config was not edited to
change that — an earlier attempt to modify it in this work was refused by the
tool sandbox and was not routed around.

Acceptance 6 is therefore met on its primary claim — the fixture opens on turn
900 and older pages are demonstrably fetched — but its evidence is the
transcript's own content across page boundaries, not the daemon log. The logging
from Phase 1 is unit-tested (`internal/ws/historylog_test.go`) and will appear
for anyone running at debug.

### Environment note: replacing a running daemon binary in place gets it killed

The first swap copied over `~/.local/bin/mcremote` while launchd had it mapped.
The daemon shut down, never logged a startup line, and `launchctl list` showed
exit `-9`. This host has no CLI signing identity (MADR 0069), so a mapped binary
replaced under an unsigned build is killed on exec.

Replacing it as a **new inode** — write beside it, `rm`, `mv` into place, with an
ad-hoc `codesign -s -` — starts cleanly. The operator's daemon was restored to
`v0.16.5` immediately on the first failure and again at the end of the phase;
downtime was about five minutes and no session data was touched.

### Acceptance

| # | criterion | result |
| --- | --- | --- |
| 1 | cold session quiet; three discard paths each warn distinctly | **pass** (R1, R2) |
| 2 | every page logs; a silent empty warns | **pass** in unit tests; not observed live (level=info) |
| 3 | two pages join without merging turns | **pass** (R4b) — and observed on device as a split bubble |
| 4 | a background resync does not rewind a paged transcript | **pass** (R5) |
| 5 | `sessionHistoryNewest` has a production caller | **pass** (R6) |
| 6 | the fixture opens on turn 900 and pages backward | **pass** — turn 900, then back to turn 855 |

### Cleanup

Fixture directory removed, released `v0.16.5` daemon restored and running, 34
real sessions intact, `config.yaml` never modified.

## Plan complete

All four phases executed. Two deviations are recorded above rather than folded
away: `sealedHead` was dropped as redundant under concatenation, and the
`toolIndex` rebase that the plan never anticipated was added.

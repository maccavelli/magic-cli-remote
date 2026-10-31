---
status: in-progress
date: 2026-10-31
associated-madr: "0224-MADR-pause-the-acp-reader-instead-of-dropping-notifications.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement pausing the ACP reader instead of dropping notifications

Associated MADR: [0224-MADR-pause-the-acp-reader-instead-of-dropping-notifications.md](0224-MADR-pause-the-acp-reader-instead-of-dropping-notifications.md)

## Goal

Magic pins `acp-go-sdk2` ~~`v1.29.0`~~ `v1.30.0` (MADR amendment A), and its ACP client connection pauses its reader at a full
notification queue instead of dropping notifications (MADR D1, D2). The drop notice and its recorder
are gone (D3). A test pins the new property, and was seen failing first (D4). The rule the pause
depends on is written where a handler would be added (D5), and every record and comment that says the
connection is per engine says what is true (D6).

The owner's answers to the MADR's questions: Q1 "drop it" (D3), Q2 "move first" (D8), Q3 "skip it"
(D9). Amendment A's Q4, the byte budget (D10), is open; the steps below follow its recommendation.

## Scope

**Code:**

* `go.mod`, `go.sum`: the module's line, and its two hash lines (MADR P1; P4 at v1.30.0).
* `internal/provider/acpagent/acpagent.go`: `clientConnOptions` and its comment, `:1110-1141` (D2,
  D6).
* `internal/provider/acpagent/session.go` (D3, D5, D6):
  * the `testDropped` and `lastDropped` fields, `:240-245`;
  * the snapshot `droppedAtStart`, `:442`, and the call `s.noticeDroppedUpdates(droppedAtStart)`,
    `:500`;
  * `droppedNotifications`, `recordDropped` and `noticeDroppedUpdates`, `:623-643` and `:658-688`;
  * the comment of `noticeRefusedUpdates`, `:690-695`, which names `noticeDroppedUpdates`;
  * `deliver`'s comment, `:1664-1678`;
  * `watchConnClose`'s comment, `:1306-1310`;
  * `SessionUpdate`'s comment, `:1809-1813`.
* `internal/provider/acpagent/extensions.go`: `HandleExtensionMethod`'s comment, `:78-81` (D5).

**Tests:**

* `internal/provider/acpagent/stalledpump_test.go`:
  * `sdkNotificationQueueDepth`'s comment, `:18-33`;
  * `TestACPConnectionSurvivesAStalledPump`'s comment, `:248-261`;
  * `blockingClient` and `TestDropHandlerRecordsTheDroppedMethod`, `:299-354`, replaced by D4's test.
* `internal/provider/acpagent/droppednotice_test.go`: deleted. Its helper `turnEvents` (`:17-41`) moves
  first, because `lifecycle_test.go:175` uses it.
* `internal/provider/acpagent/queue_test.go`: gains `turnEvents`, beside `newQueueTestSession` (`:16`),
  which it builds on.

**Records:**

* [0166-MADR-a-stalled-session-can-still-take-the-acp-transport-down.md](0166-MADR-a-stalled-session-can-still-take-the-acp-transport-down.md):
  an amendment correcting F3, and the two places that build on it (`:172`, `:266`) (D6).
* [0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md](0167-MADR-the-acp-sdk-is-dormant-and-its-bounded-queue-is-an-availability-defect.md):
  notes under D17 (`:1074`) and D18 (`:1085`), and at the two paragraphs that repeat the per-engine
  claim (`:1046`, `:1058`) (D2, D3, D6).
* [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md):
  a note under D12 (`:740`) (D2, D3).
* This pair, and `docs/README.md`'s two index rows for it.

**Not in scope:**

* gobble-cli's pin and its own `OverflowDropNewest` (`acpclient/conn.go:127`), which are gobble's,
  in its repository (D8).
* The inbound-refusal notice (`noteInbound`, `noticeRefusedUpdates`, `refusednotice_test.go`), which
  stays (0207 D13).
* Magic's own best-effort event drops in `deliver` (`session.go:1629-1638`), a different queue (MADR
  F9).
* `check_libs.json`: it reads the version from `go.mod` and needs no change.

## Constraints

* **C1 — One commit.** The agent stages; the owner commits with `git commit --no-edit`. Before
  anything is staged, `git diff --cached --name-only` must list nothing but this plan's files. Any
  other staged file is the owner's to commit first.
* **C2 — No live run** (Q3, D9). No `make live-*` target runs.
* **C3 — Every new check is seen failing first,** on a scratch copy made with `git archive`, never
  in the tree.
* **C4 — The test hosts run only on the owner's word** (Q1 below).
* **C5 — No push without an explicit ask in the same turn.** `leftovers/guard_preview.py` before
  staging; `leftovers/guard_range.py` before asking the owner to push.
* **C6 — Gofmt, golint and govulncheck pass on every staged Go file** (`make pre-add-check`).

## Implementation Steps

### P0 — Before the first write (read only)

1. Record `HEAD` and `git status --short`. Work that is not this plan's is left alone.
2. The MADR's line references still hold: `acpagent.go:1133-1141`; `session.go:240-245`, `:442`,
   `:500`, `:623-688`, `:1306-1310`, `:1664-1678`, `:1809-1813`; `extensions.go:78-81`;
   `stalledpump_test.go:18-33`, `:248-261`, `:299-354`; `droppednotice_test.go:17-41`;
   `lifecycle_test.go:175`. A moved line is re-read, not assumed.
3. `go list -m github.com/maccavelli/acp-go-sdk2` gives `v1.28.0`. `git ls-remote` shows ~~`v1.29.0`
   peeling to `f2c53b9c08f9…`, the commit MADR F1 read~~ `v1.30.0` peeling to `d65819a94986…`, the
   commit MADR F12 read (amendment A).

### S1 — The new test, seen failing first (D4; C3)

1. **Write the test into the tree, before any code changes.** In `stalledpump_test.go`, replace
   `blockingClient` and `TestDropHandlerRecordsTheDroppedMethod` (`:299-354`) with:
   * `recordingBlockedClient`: an `acp.Client` whose `SessionUpdate` waits on a `release` channel or
     the context, then records the `tool_call` id, under a mutex;
   * `TestABlockedHandlerPausesTheAgentAndLosesNothing`. It builds the connection with
     `clientConnOptions(nil, nil)`, as `TestACPConnectionSurvivesAStalledPump` does, and runs
     `initializeForTest`. It writes `sdkNotificationQueueDepth + 200` `toolCallFrame`s from its own
     goroutine. It asserts:
     * the writer stops before the last frame: the reader paused. The writer counts as stopped when
       its count is unchanged for five checks 100 ms apart;
     * while paused, the connection is open and `DroppedNotifications()` is 0;
     * after `release` closes, the writer finishes within 30 s;
     * every frame is handled within 30 s, in order (`tool-0` … `tool-1223`);
     * `DroppedNotifications()` is still 0, and the connection is still open.

   It is MADR P3's probe (`leftovers/0224_probe_test.go.txt`), with the bounds named and its unused
   import removed. Its comment says what it pins and cites MADR 0224 D4.
2. **Red, on a scratch copy of `HEAD` plus this file only** (`leftovers/tree_copy.py`, with only the
   test file changed under `internal/`): `go test -race -count=1 -v -run
   '^TestABlockedHandlerPausesTheAgentAndLosesNothing$' ./internal/provider/acpagent/`. Want: FAIL,
   "the writer was never paused", as P3 measured on v1.29.0 and P4 on v1.30.0 with the drop policy. `HEAD` is v1.28.0,
   where the drop policy is the same, so the same failure is expected. A different failure, or a
   compile error, is a stop. Log: `leftovers/0224-s1-red.log`.

### S2 — The change (D1 to D6)

1. **D1.** `go get github.com/maccavelli/acp-go-sdk2@v1.30.0` (~~`@v1.29.0`~~), then `go mod tidy`.
   `git diff --stat go.mod go.sum` shows one line and two lines, matching MADR P4: `go.sum` gains
   `h1:tKVtczDjyi1e…` and the unchanged `go.mod` hash. Anything else is a stop.
2. **D2.** `clientConnOptions` (`acpagent.go:1133-1141`):
   * `acp.WithNotificationOverflowPolicy(acp.OverflowBlock)`;
   * the `WithNotificationDropHandler(s.recordDropped)` line removed.

   Its comment (`:1110-1132`) is rewritten to say:
   * the client names `OverflowBlock`, the SDK's default since v1.29.0, so the options state what
     production builds;
   * a full queue pauses the reader, so the agent's writes wait and nothing is dropped or closed;
     past twice the queue's size, while the loop waits on the reader, the SDK closes, as the godoc
     says;
   * the pause comes at the count or at the SDK's byte budget, 64 MiB by default, which Magic takes
     (MADR D10); a request from the agent past the budget is refused by the SDK with `-32603`, and
     its handler here does not run (MADR F13, F14);
   * a pause holds only this session: each Magic session has its own agent and connection
     (`spawnAgent`);
   * the inbound validation hook records onto `s` (0207 D13); a nil `s` records nothing;
   * the logger and the message bound, as now.
3. **D3.** In `session.go`, remove:
   * the `testDropped` and `lastDropped` fields and their comments (`:240-245`);
   * `droppedAtStart := s.droppedNotifications()` (`:442`) and `s.noticeDroppedUpdates(droppedAtStart)`
     (`:500`), and the comment above the call, which then names only the refused notice;
   * `droppedNotifications` and `recordDropped` (`:623-643`), and `noticeDroppedUpdates`
     (`:658-688`).

   `noticeRefusedUpdates`' comment (`:690-695`) stops naming `noticeDroppedUpdates` and says the same
   thing on its own. `lastMethod` stays: `noticeRefusedUpdates` uses it (`:712`).
4. **Tests for D3.**
   * Move `turnEvents` from `droppednotice_test.go:17-41` into `queue_test.go`, unchanged, with its
     comment.
   * Delete `droppednotice_test.go`.
   * `go vet ./internal/provider/acpagent/` must then find no undefined name.
5. **D5.** The rule, in two comments:
   * `SessionUpdate` (`session.go:1809-1813`): keep the existing reason it sends no request, and add
     that under `OverflowBlock` a notification handler must not wait for a request or a
     `$/cancel_request` the agent sends later, nor for code that waits for one, nor take a lock that
     is held across such a wait. A full queue would otherwise deadlock this session (MADR 0224 D5;
     the SDK's 0044 D7).
   * `HandleExtensionMethod` (`extensions.go:78-81`): the same rule for the extension notification
     handlers it dispatches.
6. **D6, the code comments.** Each is rewritten to say what the code does at ~~v1.29.0~~ v1.30.0 under
   `OverflowBlock`:
   * `deliver` (`session.go:1664-1678`). Today it says the SDK's queue "does not drop: it closes the
     whole connection (acp-go-sdk@v0.13.5 …)". It now says that a full queue pauses the SDK's reader,
     so a deliver that blocked would pause this session's agent until the consumer drained. The
     non-blocking design and the overflow cap remain why that never lasts.
   * `watchConnClose` (`session.go:1306-1310`). Today: "A queue overflow cannot end it: Magic runs
     OverflowDropNewest". It now says that an overflow ends it only past `OverflowBlock`'s read-on
     limit, which needs a handler waiting on a request of its own, and no handler here sends one.
   * `sdkNotificationQueueDepth` (`stalledpump_test.go:18-33`). It now says what the SDK does at
     ~~v1.29.0~~ v1.30.0 at that depth, with `OverflowBlock`, and that the constant is still the
     SDK's default (`defaultMaxQueuedNotifications`, 1024). The tests' frames are far below the
     byte budget, so the count is what they reach (MADR P4).
   * `TestACPConnectionSurvivesAStalledPump` (`stalledpump_test.go:248-261`). The policy is now
     `OverflowBlock`, it no longer says "taking every session on the engine with it", and it keeps
     its seen-red history. Its body does not change.
7. **Gofmt** on every changed Go file. `git diff --stat` lists only Scope's files.

### S3 — The checks, green, and the plants (D4; C3)

1. **Green, in the tree:**
   * `go test -race -count=1 -v ./internal/provider/acpagent/`: every test passes, D4's test among
     them, and `TestDropHandlerRecordsTheDroppedMethod` and the three `droppednotice_test.go` tests
     are gone;
   * `go test -race -count=20 -run '^(TestAStalledPumpIsContainedNotHung|TestACPConnectionSurvivesAStalledPump|TestABlockedHandlerPausesTheAgentAndLosesNothing)$' ./internal/provider/acpagent/`,
     with `GOMAXPROCS=1`: 60 PASS (0166 D7's condition).
2. **Plants, each on a scratch copy of the changed tree** (`leftovers/tree_copy.py` and one asserted
   edit), D4's test run in each:
   * `clientConnOptions` names `OverflowDropNewest` again: D4's test must fail, "the writer was never
     paused";
   * `clientConnOptions` names `OverflowCloseConnection`: D4's test must fail, on the connection
     having closed;
   * `recordingBlockedClient` never records: D4's test must fail on the count of handled frames.

   Each plant's failure is quoted in the record. A plant that passes is a stop. Copies are removed.
   Log: `leftovers/0224-s3-plants.log`.
3. **The absence checks (MADR Confirmation).** `git grep -n -E
   'OverflowDropNewest|WithNotificationDropHandler|DroppedNotifications|recordDropped|noticeDroppedUpdates|testDropped|lastDropped' -- 'internal/*.go' ':!*_test.go'`
   finds nothing. In tests, only D4's test names `DroppedNotifications`.
4. **The gates:**
   * `make pre-add-check FILES="…"` on the changed Go files;
   * `leftovers/vet_goos.py`: darwin, windows and linux;
   * `make staticcheck`: exit 0. The ignored findings stay the 56 acp-go-sdk2 unstable-schema names,
     since the schema is unchanged (MADR F1);
   * `make vulncheck`;
   * `make race`: every package ok, and no FAIL, panic or DATA RACE line;
   * `leftovers/check_libs_staged.py go.mod go.sum`: `acp-go-sdk2` checked at ~~v1.29.0~~ v1.30.0, 0 failing
     checks;
   * `leftovers/nonascii_scan.py --diff` on the changed Go files.

### S4 — The test hosts (C4)

`PINS_PHASE=0224 leftovers/pins_hosts.py <linux> <windows>`, naming every changed and new file, since
its default misses untracked ones. Want: `go test -race -count=1 ./...` on the Linux test host, and
`make ci-windows` on the Windows test host, each exit 0. Both scratch trees are removed, and their
absence checked.

### S5 — Records and staging

1. **0166, an amendment (D6):** "Amendment — 2026-10-31: the connection is per session." It states:
   * F3's "The connection is per engine, not per session" is not true of the code since `91757927`
     (2026-07-22), and was not when F3 was written;
   * the facts, as 0224 F6 records them;
   * that a stalled consumer, or a closed connection, therefore ended one session, not every session
     on the engine;
   * that `:172` and `:266`, which build on F3, are read with this correction. They are annotated
     there, not rewritten.
2. **0167, notes (D2, D3, D6):**
   * under D17 (`:1074`): amended by 0224 D2; the client names `OverflowBlock`, and the drop handler
     is gone;
   * under D18 (`:1085`): retired by 0224 D3, on the owner's answer to 0224's Q1, because the counter
     cannot rise under `OverflowBlock`;
   * at `:1046` and `:1058`: the per-engine claim is corrected by 0166's amendment of 2026-10-31.
3. **0207, a note under D12 (`:740`):** superseded by 0224 D2 and D3; the drop handler and its
   recorder are gone.
4. **This plan's execution record:** each step's output, the plants' failures quoted, and any
   deviation, dated.
5. `python3 scripts/check_records.py --write-index`; `make check-records`; `make markdownlint-docs`;
   the 0224 pair through markdownlint; `leftovers/guard_preview.py`.
6. **Stage**, under C1:
   * Scope's files;
   * this pair;
   * the three amended records;
   * `docs/README.md` as this pair's two rows only (`leftovers/stage_index_rows.py 0224`).

   Then run `check_records.py --check-all` on the staged tree, exported with `git checkout-index`. The
   owner commits.

### S6 — After the commit

1. `make check-libs` on `HEAD`: `acp-go-sdk2` pinned ~~v1.29.0~~ v1.30.0, 0 failing checks.
2. `leftovers/guard_range.py origin/master HEAD` before asking the owner to push.
3. After the push: every Go job and the Docs workflow pass. Record the run ids. The plan is then
   `complete`.

## Verification

| # | Criterion | MADR |
| :--- | :--- | :--- |
| V1 | `go list -m` gives `acp-go-sdk2` ~~`v1.29.0`~~ `v1.30.0`; `go.mod` and `go.sum` change by one and two lines | D1 |
| V2 | `clientConnOptions` names `OverflowBlock`, and S3 step 3's absence check finds nothing in non-test code | D2, D3 |
| V3 | D4's test failed on `HEAD` (S1) and on the three plants (S3), and passes in the tree, 20 of 20 under `GOMAXPROCS=1` | D4 |
| V4 | `TestAStalledPumpIsContainedNotHung` and `TestACPConnectionSurvivesAStalledPump` pass 20 of 20 under `GOMAXPROCS=1` and `-race` | D4 |
| V5 | The comments of S2 steps 5 and 6 say what the code does, and none says the connection is per engine | D5, D6 |
| V6 | 0166, 0167 and 0207 carry S5's amendment and notes | D2, D3, D6 |
| V7 | S3's gates, S4's hosts, `make check-libs` on `HEAD`, and CI on the push pass | all |

## Rollout and Rollback

* **Rollout:** the next release of `mcremote`. Nothing on the wire changes, and the phone needs no
  change. The phone stops receiving the "may be incomplete" notice, which under `OverflowBlock` has
  nothing to report.
* **Rollback:** revert the commit. `go.mod`, `go.sum`, the options, the notice and its tests return
  together. The records' amendments stay, since they correct a fact independent of the policy.

## Owner decisions needed at approval

* **Q1 — The test hosts (C4):** S4 runs on the Linux and Windows test hosts, in scratch trees.
* **Q4 — The byte budget (MADR D10):** take the SDK's default (recommended; the steps above), or
  name `WithMaxQueuedBytes(64 << 20)` in `clientConnOptions`. Naming it adds one option line to
  S2 step 2 and nothing else.

*Answered by the owner, 2026-10-31:* "use the sdk default" (Q4: the steps stand as written), "you may
run on the test hosts" (Q1: S4 runs), "accept the madr", "plan approved to proceed".

## Not done by this plan

* gobble's move to ~~v1.29.0~~ v1.30.0, and its own overflow policy (MADR D8). 0184 D12 does not
  hold until gobble moves.
* A live run (Q3, D9).
* An automated check of D5's rule. MADR F7's scan is a heuristic, not a gate.
* ~~Bounding notification memory by bytes, which the SDK records as its own open issue (SDK 0044
  F8).~~ v1.30.0 does this itself (MADR F13, F14); Magic takes its default (D10).
* A metric of the SDK's `QueuedBytes` (MADR D10).

## Amendment — 2026-10-31 (A): `acp-go-sdk2 v1.30.0`

Made before approval and before any step ran, on the MADR's amendment A. What changed in this plan:

* the pinned version, v1.29.0 to v1.30.0, in Goal, P0, S1, S2, S3, S6, V1 and "Not done", each
  struck through and annotated rather than rewritten;
* S2 step 2: the comment on `clientConnOptions` also states the byte budget and the refusal of a
  request past it;
* S2 step 6: the queue-depth comment says the tests reach the count, not the budget;
* the owner's question Q4, the byte budget.

Scope's files are unchanged. MADR P4 measured, on v1.30.0, every result S1 and S3 expect.

## Execution record

### P0 — 2026-10-31

* `HEAD` `dd62a24d`; `git status --short`: this pair only, untracked.
* `go list -m github.com/maccavelli/acp-go-sdk2`: `v1.28.0`. `git ls-remote`: `v1.30.0` peels to
  `d65819a94986…`.
* Every line range in P0 step 2 printed and read (`leftovers/0224_p0.py`, log `leftovers/0224-p0.log`);
  each holds as cited. `queue_test.go` already imports everything `turnEvents` needs.
* A `git grep` for the drop machinery and for stale overflow wording in Go code found four places
  outside S2 step 6's list: deviation 1.

### Deviation 1 — 2026-10-31: four more stale places for D6

* **Found (P0, before any write):**
  * `session.go:107-127`, the overflow fields' comment: the SDK's overflow "closes the whole
    connection", and "the reader never blocks, so no client-side change can prevent it at acp-go-sdk
    v0.13.5".
  * `stalledpump_test.go:117-142`, `TestAStalledPumpIsContainedNotHung`'s comment: v0.13.5's close,
    and "If upstream ever makes the reader block … that is the moment to revisit it", a condition
    v1.29.0 met.
  * `stalledpump_test.go:278-279`, in the survival test: "With the policy the reader never stops";
    under `OverflowBlock` it pauses and resumes.
  * `stalledpump_test.go:293-294`, the survival test's failure message, which prints
    `conn.DroppedNotifications()`, always 0 under `OverflowBlock`. With it, S3 step 3's "in tests,
    only D4's test names `DroppedNotifications`" would be false.
* **Decision (owner, "Extend D6 to all four"):** S2 step 6 also rewrites these three comments to
  describe v1.30.0 under `OverflowBlock`, and drops the `DroppedNotifications()` argument from the
  message. For the containment test, the comment says the revisit condition is met and answered by
  `TestACPConnectionSurvivesAStalledPump`, which asserts survival with production's options. No
  assertion changes. MADR D6 carries a note.
* **Scope:** no new files; `session.go` and `stalledpump_test.go` were already in scope.

### S1 — 2026-10-31

* `recordingBlockedClient` and `TestABlockedHandlerPausesTheAgentAndLosesNothing` replaced
  `blockingClient` and `TestDropHandlerRecordsTheDroppedMethod` (`leftovers/0224_s1_apply.py`,
  asserting each anchor once; 57 lines out, 122 in). The bounds are named constants; the import of
  `sync` was added, and the frame ids use `fmt`, which the file already imports, so the probe's
  `strconv` and `strings` were not needed. `gofmt -l`: nothing.
* **Red** on a scratch copy of `dd62a24d` plus this file only (`leftovers/0224_runs.py s1-red`):
  exit 1, `stalledpump_test.go:386: the writer was never paused: 1224 frames written`. That is the
  failure P3 and P4 measured with the drop policy. Copy removed.

### S2 — 2026-10-31

1. `go get github.com/maccavelli/acp-go-sdk2@v1.30.0`, `go mod tidy`: "upgraded … v1.28.0 =>
   v1.30.0". `go.mod` one line, `go.sum` two: `v1.30.0 h1:tKVtczDjyi1e…` and
   `v1.30.0/go.mod h1:Iz5f3g3L3m…`, as P4. `go list -m`: `v1.30.0`.
2. `clientConnOptions` names `OverflowBlock`; the drop handler's line is gone. The comment says what
   step 2 lists, the byte budget and the refusal included, and that naming another policy turns D4's
   test red.
3. `session.go`: the two fields, the snapshot, the call, `droppedNotifications`, `recordDropped` and
   `noticeDroppedUpdates` removed. `noticeRefusedUpdates`' comment states on its own what it had
   borrowed from `noticeDroppedUpdates`. The comment above the call ("so the notice belongs to this
   turn") already read correctly for the one notice left, and was kept.
4. `turnEvents` moved unchanged, with its comment, after `newQueueTestSession` in `queue_test.go`
   (`leftovers/0224_s2_move.py`; 25 lines), whose imports already covered it.
   `git rm internal/provider/acpagent/droppednotice_test.go`, which stages the deletion now rather
   than at S5; it is this plan's file, so C1 holds. `go vet ./internal/provider/acpagent/`: exit 0.
5. D5's rule added to `SessionUpdate`'s comment and `HandleExtensionMethod`'s.
6. D6's comments rewritten: `deliver`, `watchConnClose`, `sdkNotificationQueueDepth`, and
   `TestACPConnectionSurvivesAStalledPump`'s, keeping its seen-red history; and deviation 1's four:
   the overflow fields' comment, `TestAStalledPumpIsContainedNotHung`'s, the survival test's
   in-body comment, and its failure message, which now prints `conn.Err()`.
7. `gofmt -l internal/provider/acpagent/`: nothing. `git diff --stat HEAD`: `go.mod`, `go.sum`,
   `acpagent.go`, `droppednotice_test.go` (deleted), `extensions.go`, `queue_test.go`, `session.go`,
   `stalledpump_test.go`; 229 insertions, 301 deletions.

### S3 — 2026-10-31

1. **Green, in the tree** (`leftovers/0224_s3_green.py`):
   * `go test -race -count=1 -v ./internal/provider/acpagent/`: exit 0, 273 top-level and 398 PASS
     lines in all, 0 FAIL, 0 DATA RACE. D4's test passed; the four removed tests are absent. 398 is
     P1's 410, less the drop-handler test and the three drop-notice tests with their nine subtests,
     plus D4's test.
   * The three tests, `-race -count=20`, `GOMAXPROCS=1`: exit 0, 60 PASS, 20 each.
2. **Plants**, each in a scratch copy of the changed tree with one asserted edit
   (`leftovers/0224_runs.py plants`); D4's test failed in each, and each copy was removed:
   * `OverflowDropNewest` named: `stalledpump_test.go:396: the writer was never paused: 1224 frames
     written`;
   * `OverflowCloseConnection` named: `stalledpump_test.go:399: the connection closed while paused
     after 1026 frames: acp: connection closed: notification queue overflow`;
   * `recordingBlockedClient` never records: `stalledpump_test.go:415: after release 0 of 1224
     notifications were handled`.
3. **Absence:** the `git grep` over non-test `internal/*.go` finds nothing (exit 1). In tests,
   `DroppedNotifications` appears only in D4's test (`stalledpump_test.go:401`, `:424`);
   `OverflowDropNewest` appears only in two comments that tell the history (`:259`, `:344`).
4. **Gates** (`leftovers/0224_s3_gates.py`):
   * `make pre-add-check` on the five changed Go files: exit 0, "5 file(s) clean (gofmt, golint,
     govulncheck)";
   * `vet_goos.py`: darwin/arm64, windows/amd64 and linux/amd64, each exit 0;
   * `make staticcheck`: exit 0; 56 ignored findings on each GOOS, each an acp-go-sdk2
     unstable-schema name;
   * `make vulncheck`: exit 0, "clear; allowlisted required-module findings: GO-2026-5932";
   * `make race`: exit 0, 45 packages ok, no FAIL, panic or DATA RACE line;
   * `check_libs_staged.py`: the first run failed, 7 checks, every one "turnEvents redeclared in this
     block" (`queue_test.go:30`). The harness, not the change: it laid the named files over a clone of
     `HEAD` and was not given the deleted `droppednotice_test.go`, so the clone still held it. The
     script now `git rm`s a named file the tree no longer has. Re-run with the deletion named: exit 0,
     `acp-go-sdk2` "pinned v1.30.0, checking v1.30.0", every check ok, "0 failing checks";
   * `nonascii_scan.py --diff`: 0 characters outside printable ASCII.

### S4 — 2026-10-31

`PINS_PHASE=0224 leftovers/pins_hosts.py <linux> <windows>`, with the default file list, `git diff
--name-only HEAD`. Its only untracked files were this pair, which no Go test reads. The script left a
deleted file in its copy. It now removes one, and printed "deleted in the copy:
internal/provider/acpagent/droppednotice_test.go".

* Linux test host, go1.27.3 linux/amd64: `go test -race -count=1 -timeout=40m ./...` exit 0, 45 ok
  lines.
* Windows test host, go1.27.3 windows/amd64: the dry-run check passed; `make ci-windows` exit 0, "ALL
  SELECTED CHECKS PASSED", 45 ok lines.
* Both scratch trees removed, and their absence checked (exit 0).

### S5 — 2026-10-31

1. **0166:** the amendment "the connection is per session", at the end. In-place annotations at F3
   itself, at option D's bullet and at F11's last sentence. The amendment names the three later
   mentions that are read with it but not annotated: a consequence, a deferred item and the last
   amendment.
2. **0167:** a note under D17, amended by 0224 D2; a note under D18, retired by 0224 D3; and an
   annotation at each of the two paragraphs that repeat the per-engine claim.
3. **0207:** a note under D12, superseded by 0224 D2 and D3; D13 stands.
4. This execution record.
5. `check_records.py --write-index`: exit 0; `docs/README.md` gains this pair's two rows and nothing
   else. `make check-records`: exit 0. `make markdownlint-docs`: exit 0, "0 issues in 0 files". That
   target skips MADR and PLAN files, so the five records were also linted by name
   (`markdownlint-cli2 --no-globs`):
   * the 0224 pair and 0207: 0 errors;
   * 0167: 92 errors at `HEAD` and 92 now, none new (`leftovers/0224_mdl_compare.py`);
   * 0166: 37 errors at `HEAD`, 44 now. The 7 new ones are all MD004 on the amendment's `*` list
     markers. The record already uses `*` throughout, which its 37 are, and its disable comment does
     not list MD004.

   `guard_preview.py`: exit 0, "would edit 0 file(s)".
6. **Staged** under C1: Scope's files, this pair, the three amended records, and `docs/README.md` as
   this pair's two rows (`leftovers/stage_index_rows.py 0224`). `check_records.py --check-all` ran
   on the staged tree, exported with `git checkout-index`. The owner commits.

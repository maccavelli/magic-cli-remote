---
status: proposed
date: 2026-09-28
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# Two CI flakes, two real defects: the relay closes a replaced host under the hub lock, and mobile tests leak transcript saves into the next test

## Context and Problem Statement

Two CI runs on 2026-09-24 failed on changes that could not have caused them:

- **Run 36051385950** (`241d1f7e`, docs only) failed `Go (test; build on tag)` in
  `TestRegisterReplacement` (`internal/relay`).
- **Run 36055229733** (`4ee1d519`, one `Makefile` line) failed `Flutter (analyze & test)` in
  `history_replay_test.dart`, "dispose persists a pending debounced save immediately".

The flake ledger (MADR 0143, `ci-flakes.tsv`) already held one earlier failure of the relay test,
run 36024135246 (`linux/arm64`, `eb001ac7`). Neither failure was treated as noise. Both were traced
to a defect, and in each case the defect was then forced to occur on demand in a scratch clone.

- **The relay one is a production defect.** Replacing a host's control connection can hold the
  hub-wide lock for 5 seconds, stalling every host.
- **The mobile one is a test-isolation defect.** A save started by one test's teardown can land in
  the next test's directory.

This record decides how to fix both. They share a number because they share a discovery and a
shape. In both, an asynchronous close or save outlives the code that started it, and it only
misbehaves when a loaded runner reorders goroutines or isolates.

### What was measured, not assumed

Every experiment ran in a scratch clone of `HEAD` (`4ee1d519`), never in the working tree. The Go
experiments ran on Linux (WSL, Go 1.26.6); the Flutter ones ran on Windows and on Linux.

**The relay failure.**

1. **CI's own log gives the timing.** Tests in a file run sequentially, and CI does not pass
   `-v`, so server log lines interleave with test output in order:
   - `19:56:18 host registered … host_id=h1`: the test's first registration;
   - nothing for the second registration;
   - `--- FAIL: TestRegisterReplacement (5.01s)` with `server_lifecycle_test.go:342: second: {V:0 Type: ID: Payload:[]}`;
   - `19:56:23 host registered … host_id=h1`: the second registration, logged five seconds late.

   `hub.register` logs that line **after** it closes the replaced connection.
2. **The library's `Close` is a handshake, not a teardown.** In `coder/websocket` v1.8.15,
   `Close` (`close.go:99-127`) runs three bounded waits in sequence:
   - `writeClose`, up to 5 s (`close.go:167-195`);
   - `waitCloseHandshake`, which takes `readMu` and reads until the peer's close frame, up to 5 s
     (`close.go:198-226`);
   - `waitGoroutines`, up to 15 s (`close.go:228+`).

   `CloseNow` (`close.go:129-155`) skips the handshake.
3. **A cancelled read hard-closes, but only while a read is in flight.** `Read` arms
   `context.AfterFunc(ctx, c.close)` (`conn.go:188-198`) and disarms it in `finishRead` before
   returning (`read.go:238-257`). If the replaced connection's read loop is inside `conn.Read`
   when `hub.register` calls `old.cancel()`, the hook closes the socket and `Close` returns at
   once. The read loop starts in a goroutine launched **after** `register_ok` is written
   (`server.go:588-616`). If it has not reached `conn.Read`, nothing hard-closes the socket, and
   `Close` waits out the handshake.
4. **The failure does not come from load alone.**
   - 400 isolated race runs of the test passed, at `GOMAXPROCS=1` and `2`.
   - Two load runs passed 630 each: 12 parallel race-built processes, then the same pinned to
     2 CPUs with `taskset`.
   - A probe replacing a host whose old TCP path was frozen mid-stream (a proxy that stops
     forwarding) replaced in about 0.5 ms. An unrelated host registered in 0.5–0.8 ms meanwhile.

   The stall needs the specific ordering in observation 3.
5. **Forcing that ordering reproduces it exactly.** Each variant ran 5 times under `-race`:

   | variant | result | hub lock held during the replacement `Close` |
   | --- | --- | --- |
   | `HEAD`, a timing log added | 5/5 pass | 49–166 µs |
   | read loop starts 300 ms late | **5/5 fail**, `server_lifecycle_test.go:342: second: {V:0 Type: ID: Payload:[]}` | **5.00 s** each run |
   | late start plus the candidate fix (D1) | 5/5 pass, including the check that the old control closes | 2–10 µs |

**The mobile failure.**

6. **The failing test and its neighbours.** `history_replay_test.dart:225`
   (`saved!.items.single.text`) threw `Bad state: Too many elements`. The job log (job
   107820393659) also shows another write colliding in that test's own directory:
   `Transcript cache save failed for s1: PathNotFoundException: Cannot rename file to '/tmp/mcremote_testHZHTBF/transcripts/s1.json', path = '…/s1.json.tmp' (OS Error: No such file or directory, errno = 2)`.
   Neighbouring tests log `Cannot open file … s1.json.tmp`.
7. **How a save outlives its test.**
   - `makeContainer()` disposes the container at teardown (`history_replay_test.dart:21-25`).
   - On dispose, the notifier flushes pending sessions with an **unawaited**
     `_saveCacheBestEffort` (`transcripts_notifier.dart:270-287`, `:326-335`), into the
     `TranscriptCache()` it created itself (`:128`).
   - `save` encodes on a background isolate first (`compute`), and only then resolves its
     directory in `_writeEntry` (`transcript_cache.dart:305-347`).
   - The directory is resolved **lazily, once per cache instance**, through the global
     `PathProviderPlatform.instance` (`transcript_cache.dart:177-192`). Each test's `setUp`
     replaces that instance with a fresh temporary directory (`fake_path_provider.dart:33-35`).
   - Opening a directory also deletes every `*.json.tmp` in it (`_sweepTempFiles`,
     `transcript_cache.dart:190`).

   So a save from a test whose cache never touched disk resolves to whichever test is running
   when the encode finishes. On arrival it can sweep that test's in-flight temp file, and it can
   overwrite that test's `s1`.
8. **Saves do outlive their test** (10/10 runs). Test A replays a large history in a container
   disposed at teardown. Its teardown deletes A's directory, and the save afterwards recreates
   that directory with the transcript in it. On this machine the save always resolved its
   directory before the next test's `setUp`, so it never reached test B's directory.
9. **Forcing the slow-runner ordering reproduces CI's collision.** A 300 ms delay was added at
   the start of `_writeEntry`, before the directory is resolved, standing in for a slow encode on
   a loaded runner. `history_replay_test.dart` then ran 3 times per variant:

   | variant | Linux (WSL) | Windows |
   | --- | --- | --- |
   | `HEAD` | 3/3 clean | 3/3 clean |
   | delay | **3/3 show CI's signature**, `Cannot rename file to '/tmp/mcremote_test…/transcripts/s1.json', path = '…/s1.json.tmp' (No such file or directory, errno = 2)` | 3/3 show `Cannot rename … being used by another process` |
   | delay plus the candidate fix (D3) | 3/3 clean | 3/3 clean |

   **[unverified]** The assertion failure itself (`Too many elements`) did not recur in these
   runs. It needs the stray write to *win* the race, landing after the test's own save and before
   its `load`. The collision that makes that possible is reproduced, carries CI's exact
   signature, and disappears with the fix.

### Findings

**F1 — `hub.register` closes the replaced control while holding the hub lock.**
`hub.go:108-135` calls `old.control.Close(websocket.StatusGoingAway, "replaced")` inside
`h.mu`. With the library's handshake, that holds the lock for up to 5 s, and by the library's
bounds as much as 25 s (obs. 2).

**F2 — The stall is conditional, which is why it is a flake.** It happens only when the replaced
connection's read loop has not yet entered `conn.Read` (obs. 3, 5). Load alone does not produce
it (obs. 4). In production the window is a host re-registering right after registering: a fast
reconnect loop, or a restart racing a slow read-loop start.

**F3 — While the lock is held, the whole relay stalls, not one host.** `h.mu` guards
registration, joins (`beginJoin`), control writes (`writeControl` takes it to find the slot),
tunnel claims and unregisters (`hub.go:139-472`). Every host and phone waits for one host's
handshake.

**F4 — The shutdown path already does this correctly.** `closeAllHosts` (`hub.go:174-196`)
collects the slots, unlocks, and only then cancels and closes. The replacement path is the one
exception.

**F5 — `TestRegisterReplacement` is a correct test of a real defect.** Its 5 s deadline is what
exposed the stall. It needs no change once F1 is fixed.

**F6 — Mobile test containers leave transcript saves running after their test ends.** The
disposal flush is unawaited by design, because the app must not block disposal on disk I/O
(obs. 7). Tests inherit that, so a save routinely outlives the test that started it (obs. 8).

**F7 — A late save can land in the next test's directory.** The cache resolves its directory
lazily from a global that every `setUp` replaces (obs. 7). On a slow runner a stray save joins
the next test's directory, sweeps its temp files and overwrites its `s1`. That reproduces CI's
error signature (obs. 9). It fits the `Too many elements` failure: the preceding test,
"live event arriving mid-resync rebuild survives", holds a multi-item `s1`.

**F8 — The test helper's comment calls this harmless, and it is not.**
`fake_path_provider.dart:26-31` says a late save "is harmless" and that the directory cleanup
"tolerates a late writer". The late writer is tolerated only in the sense that nothing throws.
It still corrupts another test's data.

**F9 — Production `TranscriptCache` behaviour is not at fault.** In the app the path provider
never changes, so lazy resolution always yields the same directory. The lazy, post-migration
ordering is deliberate (MADR 0126 F7). The defect is that tests share one global and do not wait
for their own asynchronous work.

**F10 — Ten test files use `transcriptsProvider`:**
`chat_end_session_navigation_test.dart`, `chat_render_test.dart`,
`chat_send_failure_test.dart`, `history_replay_test.dart`, `permission_loop_test.dart`,
`sessions_screen_test.dart`, `session_synchronizer_test.dart`, `staged_images_test.dart`,
`transcript_ingest_test.dart` and `transcript_prepend_test.dart`. Whether each one disposes a
container holding transcript state with the default cache has not been audited file by file.
That is a PLAN step.

## Decision Drivers

- A relay operation for one host must never block every other host on a peer's behaviour.
- A test that failed because of a real defect keeps its assertion. The defect gets fixed.
- A test must not leave work running that can reach another test.
- Production code changes only where production is wrong. That applies to the relay, but not to
  `TranscriptCache` (F9).
- Each fix must be shown to fail without the change, using a deterministic mechanism, not by
  hoping for load.

## Considered Options

**Relay:**

- **R-A:** Swap the slot under the lock, and close the replaced control after unlocking, in its
  own goroutine.
- **R-B:** Use `CloseNow()` under the lock.
- **R-C:** Keep `Close` under the lock and bound it with a shorter timeout.
- **R-D:** Change only the test: read `first` concurrently so the handshake completes.

**Mobile:**

- **M-A:** Give each test container its own cache, and drain it at teardown, through a shared
  test helper.
- **M-B:** Resolve `TranscriptCache`'s directory eagerly at construction.
- **M-C:** Give every test cache an explicit `directory:` override, and do not drain.
- **M-D:** Retry or skip the flaky test.

## Decision Outcome

Chosen: **R-A** and **M-A**, because each removes the defect at its cause:

- the hub lock is no longer held across a peer handshake;
- no test's asynchronous saves outlive it;
- production `TranscriptCache` stays untouched.

### The decisions

**D1 — Never close a replaced control under the hub lock.** In `hub.register`, cancel the old
registration and swap the slot under `h.mu`, exactly as today. Close the replaced control only
after unlocking, in its own goroutine, keeping `StatusGoingAway` and the reason `"replaced"` so a
live stale host still learns why. This matches `closeAllHosts` (F4). The caller of
`hub.register`, the new host's handler, must not wait on the old peer either.

**D2 — Pin D1 with a deterministic hub-level test.** Add a test in `hub_test.go` that:

- registers `h1` over a real WebSocket whose client never reads, with no server read loop
  running (the stalled ordering of obs. 3, created directly rather than by timing);
- re-registers `h1` on a second connection, and requires `hub.register` to return in under one
  second, where today it takes about 5 s;
- requires the first client to then read a close frame with `StatusGoingAway` and reason
  `"replaced"` within two seconds, so the close is still sent.

It must be seen failing on the unfixed `hub.go`.

**D3 — Test containers drain their transcript saves before the test ends.**

- Add a test-support helper that creates a `ProviderContainer`, gives its `TranscriptsNotifier` a
  cache of its own (`debugCache`), and registers a teardown that disposes the container and then
  awaits that cache's queue (`debugWhenIdle`).
- `history_replay_test.dart`'s `makeContainer` uses it.
- Each of the other nine files (F10) is audited. It is switched to the helper only where it
  disposes a container holding transcript state with the default cache, and the audit records
  each file's verdict.

**D4 — Say what the harness guarantees, and prove it.**

- Correct the comment in `fake_path_provider.dart` (F8): a late save is not harmless. The helper
  from D3 prevents it.
- Add a guard test that proves the helper drains: after its close completes, the session's
  transcript file already exists in the test's own directory.

The guard must be seen failing against a helper that disposes without awaiting.

**D5 — `TranscriptCache` is not changed** (F9). Its lazy, post-migration directory resolution
stays.

### Consequences

- Good, because a reconnecting host can no longer freeze the relay for every other host.
- Good, because `TestRegisterReplacement` becomes deterministic without being weakened.
- Good, because mobile tests stop sharing stray writes. Any failure in them is then a real signal
  again, not the previous test's leftovers.
- Neutral, because a replacement now spawns one goroutine per replaced control, alive for up to
  the library's handshake bounds (at most about 25 s) and holding no lock.
- Bad, because the mobile fix disciplines tests rather than making a leak impossible. A new test
  that builds a bare `ProviderContainer` for transcripts can reintroduce it. D4's comment and
  helper are the mitigation. Whether a lint could catch it is deferred.

### Confirmation

```bash
# Relay, on Linux or macOS (race needs cgo), and on Windows via make ci-windows
go test -race -count=20 -run 'TestHubReplacement|TestRegisterReplacement' ./internal/relay/
make race
# D2 fail-first: the new test fails against the unfixed hub.go in a scratch copy (register takes ~5 s)

# Mobile
cd apps/mobile && flutter analyze && flutter test
dart format --output=none --set-exit-if-changed apps/mobile
# D4 fail-first: the guard fails against a helper whose teardown does not await the cache queue
# Forced ordering (scratch clones, as in obs. 5 and 9):
#   relay: the read loop delayed 300 ms  -> TestRegisterReplacement passes 5/5
#   mobile: _writeEntry delayed 300 ms   -> no "Cannot rename"/"Cannot open" collision in 3/3 runs
```

## Pros and Cons of the Options

### R-A — Close after unlocking, in a goroutine (chosen)

- Good, because the lock is never held across a peer, whatever the peer does.
- Good, because a live stale host still receives `GoingAway`/`"replaced"`.
- Good, because it is the pattern `closeAllHosts` already uses.
- Neutral, because it adds a short-lived goroutine per replacement.

### R-B — `CloseNow()` under the lock

- Good, because it is a one-word change and never waits.
- Bad, because a live stale host loses the close frame and sees an abnormal closure, not a
  reason.
- Bad, because it still does I/O under the hub lock. `CloseNow` waits for the library's
  goroutines too (`close.go:129-155`).

### R-C — A shorter handshake timeout

- Bad, because `coder/websocket` fixes its handshake timeouts at 5 s and exposes no option. A
  wrapper with its own deadline would still hold the lock for that deadline.

### R-D — Change only the test

- Bad, because it is a workaround: it makes the test stop exercising the ordering that stalls
  production, and ships the stall.

### M-A — Per-container cache, drained at teardown (chosen)

- Good, because it fixes the cause: no save outlives its test, so no save can reach another
  test's directory.
- Good, because it touches only tests. Production keeps its unawaited, best-effort disposal flush.
- Bad, because it depends on tests using the helper (see Consequences).

### M-B — Eager directory resolution

- Good, because a stray save would at least land in its own test's directory.
- Bad, because it changes production ordering that MADR 0126 F7 made deliberate.
- Bad, because saves still outlive their tests and write into deleted directories, as observed.

### M-C — Explicit directory overrides without draining

- Good, because each cache is pinned to its test's directory.
- Bad, because saves still outlive their tests, and `_sweepTempFiles`/rename races remain
  wherever two containers share a test.

### M-D — Retry or skip

- Bad, because it is a workaround that hides a real cross-test write. It is excluded by rule.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Relay CI failure, 5.01 s, `second: {V:0 …}` | run 36051385950, job log, `server_lifecycle_test.go:342` |
| Earlier occurrence | `ci-flakes.tsv`, run 36024135246 (`linux/arm64`, `eb001ac7`) |
| Second registration logged at 19:56:23, 5 s late | run 36051385950 job log |
| `Close` under `h.mu` | `internal/relay/hub.go:108-135` |
| Shutdown closes after unlocking | `internal/relay/hub.go:174-196` |
| Read loop launched after `register_ok` | `internal/relay/server.go:588-616` |
| `Close` handshake bounds 5 s / 5 s / 15 s; `CloseNow` skips | `coder/websocket@v1.8.15/close.go:99-240` |
| Cancelled read hard-closes only while armed | `coder/websocket@v1.8.15/conn.go:188-198`, `read.go:238-257` |
| 400 isolated + 1260 loaded runs pass; frozen-peer probe ~0.5 ms | obs. 4 (scratch clones) |
| Delayed read loop: 5/5 fail at 5.00 s; fixed: 5/5 pass | obs. 5 (scratch clones) |
| Flutter CI failure `Too many elements` at line 225 | run 36055229733, job 107820393659 |
| Collision in the failing test's directory | same job log, `/tmp/mcremote_testHZHTBF` |
| Unawaited disposal flush | `apps/mobile/lib/state/transcripts_notifier.dart:270-287`, `:326-335`, `:128` |
| Encode first, then lazy directory; sweep on open | `apps/mobile/lib/data/chat/transcript_cache.dart:177-195`, `:305-347` |
| Global fake swapped per test; "harmless" comment | `apps/mobile/test/support/fake_path_provider.dart:26-35` |
| Saves outlive tests 10/10 | obs. 8 (scratch clone) |
| Forced ordering reproduces CI's signature 3/3; fix clean 3/3 | obs. 9 (scratch clones, Linux and Windows) |

### Related records

- MADR 0143: the CI flake ledger that recorded the first relay occurrence.
- MADR/PLAN 0167, deviation of 2026-09-22 (P2's race gate): the same library's close handshake
  waiting out its timeout on a peer that does not read, found in `internal/relayhost` tests.
- MADR 0126 F7: why `TranscriptCache` resolves its directory lazily, after migration (D5).
- MADR 0084 D3: transcript entries moved to files, which is why the fake path provider exists.

### Open questions for the plan

1. Which of the nine other files in F10 dispose a transcript-holding container with the default
   cache? The PLAN audits each one and records the verdict.
2. Can a guard catch a bare `ProviderContainer` built for transcripts in a new test, such as a
   lint or a test-support assertion? Deferred unless the audit shows the pattern is common.

## Amendment — 2026-09-28: this pair is 0173, not 0172

Filed on 2026-09-24 as `0172-MADR-` / `0172-PLAN-`. `9abbed78` had already
taken 0172 for
[0172-MADR-settings-reconnect-now-centers-under-mesh-relay.md](0172-MADR-settings-reconnect-now-centers-under-mesh-relay.md).
The number is repository-wide and is not reused. This pair is 0173. The
decisions (D1–D5) are unchanged.

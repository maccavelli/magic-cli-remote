---
status: in-progress
date: 2026-09-28
associated-madr: "0173-MADR-fix-two-ci-flakes-hub-lock-close-and-leaked-transcript-saves.md"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0173 — Fix two CI flakes: the hub-lock close and leaked transcript saves

Implements [0173-MADR-fix-two-ci-flakes-hub-lock-close-and-leaked-transcript-saves.md](0173-MADR-fix-two-ci-flakes-hub-lock-close-and-leaked-transcript-saves.md)
decisions **D1–D5**, closing findings **F1–F10**.

Renumbered from 0172 on 2026-09-28: 0172 was already
[0172-MADR-settings-reconnect-now-centers-under-mesh-relay.md](0172-MADR-settings-reconnect-now-centers-under-mesh-relay.md).

## Goal

1. **The relay.**
   - `hub.register` never holds `h.mu` across a peer's close handshake.
   - A new hub test, which registers over a real WebSocket whose peer never reads, re-registers in
     under one second and sees the old peer receive `GoingAway`/`"replaced"`.
   - That test was seen failing, at about 5 s, on the unfixed code.
2. **The forced relay ordering passes.** With the replaced host's read loop delayed 300 ms,
   `TestRegisterReplacement` passes 5/5 in a scratch clone; it fails 5/5 at `HEAD` today.
3. **Mobile tests.**
   - No mobile test's transcript save outlives the test.
   - With `_writeEntry` delayed 300 ms, `history_replay_test.dart` runs 3/3 on Linux without a
     single `Cannot rename` or `Cannot open` cache collision; at `HEAD` it shows one in 3/3.
4. **The mobile guard.** A new guard test proves the helper drains, and it was seen failing
   against a helper that does not.
5. **The full suites pass:** `make race`, `make ci-windows`, `flutter analyze` and
   `flutter test`.

## Scope

### In scope (the only files any phase may touch)

**Relay (P1):**

- `internal/relay/hub.go`, in `hub.register` only
- `internal/relay/hub_test.go`, for the new test

**Mobile (P2):**

- `apps/mobile/test/support/transcripts_container.dart` (new): the helper
- `apps/mobile/test/transcripts_container_test.dart` (new): the guard test
- `apps/mobile/test/support/fake_path_provider.dart`: its comment only
- `apps/mobile/test/history_replay_test.dart`: `makeContainer`, ~~only~~ and the six tests
  that set `n.debugCache` on a `makeContainer()` container (Deviation 1)
- **Only if** the audit (P2 step 3) finds a disposed, transcript-holding container using the
  default cache, one or more of: `chat_end_session_navigation_test.dart`,
  `chat_render_test.dart`, `chat_send_failure_test.dart`, `permission_loop_test.dart`,
  `sessions_screen_test.dart`, `session_synchronizer_test.dart`, `staged_images_test.dart`,
  `transcript_ingest_test.dart`, `transcript_prepend_test.dart` (all under `apps/mobile/test/`)

**Records:** this pair.

### Out of scope

- **`apps/mobile/lib/**`**: production `TranscriptCache` and `TranscriptsNotifier` stay as they
  are (MADR D5).
- **`internal/relay/server.go`**: the read-loop start order is not the defect. The lock is (F1).
- **`coder/websocket`**: no fork, no wrapper around its timeouts (option R-C, rejected).
- **`internal/relay/server_lifecycle_test.go`**: `TestRegisterReplacement` is correct as written
  (F5).
- **`ci-flakes.tsv`**: the ledger is appended by CI.

## Stability rule

Every phase ends with these commands, and a failing one blocks the commit.

**Go (P1).**

```bash
make pre-add-check FILES="internal/relay/hub.go internal/relay/hub_test.go"
gofmt -l internal/relay && go vet ./internal/relay/
go test -race -count=20 -run 'TestHub|TestRegisterReplacement' ./internal/relay/   # on Linux (WSL)
make race
make -n ci-windows && make ci-windows   # the dry run must show the Windows branch
```

**Dart (P2).**

```bash
cd apps/mobile
dart format --output=none --set-exit-if-changed .
flutter analyze
flutter test
```

**Commits.**

- One commit per phase, with the message from the `prepare-commit-msg` hook, via
  `git commit --no-edit`.
- This pair is committed alone, before P1 (bootstrap exception).
- **`git push` is not permitted by this plan**, and needs an explicit ask in the same turn.

## Cross-cutting contracts

- **C1 — No test is weakened.** `TestRegisterReplacement` and "dispose persists a pending
  debounced save immediately" keep their assertions and deadlines unchanged.
- **C2 — Every new check is seen failing first, in a scratch copy, never by dirtying the tree.**
  - D2's hub test must fail against the unfixed `hub.go`.
  - D4's guard must fail against a helper that disposes without awaiting.
  - The forced-ordering runs must fail at `HEAD`.
- **C3 — No production mobile code changes** (D5). A change under `apps/mobile/lib/` means the
  plan is wrong: stop and prompt.
- **C4 — The relay change is confined to `hub.register`.** `closeAllHosts` already follows the
  pattern (F4), and is not touched.

**C1 is the contract most at risk.** Once the relay fix is in, a longer deadline in
`TestRegisterReplacement` would also make CI green, and it would be tempting as belt and braces.
It would hide any regression of D1, so it is forbidden.

## Dependency and delivery order

P1 and P2 are independent and may land in either order. P3 depends on both.

## Implementation Steps

### P1 — Close the replaced control after unlocking (D1, D2; closes F1–F5)

1. **The fix.** In `hub.go`, `hub.register`:
   - keep `old.cancel()` and the slot swap under `h.mu`;
   - remember the replaced `*websocket.Conn`;
   - after the lock is released, run
     `go func() { _ = old.Close(websocket.StatusGoingAway, "replaced") }()`;
   - keep the existing comment's intent (reconnect replaces a stale registration; splices keep
     running; D10), adding one line that points at F1 and `closeAllHosts`.
2. **The test.** In `hub_test.go`, add `TestHubReplacementDoesNotHoldLockOnStaleClose`:
   - an `httptest` server whose handler accepts a WebSocket, hands the server-side `*websocket.Conn`
     to the test, and blocks until cleanup;
   - two client connections, neither ever read;
   - `h.register("h1", serverA, …)`, then time `h.register("h1", serverB, …)`: it must be under
     1 s;
   - a concurrent `h.register("h2", …)` issued during the replacement must also return under 1 s
     (F3);
   - then `clientA.Read` with a 2 s context must return a `websocket.CloseError` with
     `StatusGoingAway` and reason `"replaced"`.
3. **Fail-first (C2).**
   - Apply the new test to a scratch clone of `HEAD` with the **unfixed** `hub.go`, and run it:
     it must fail on the timing assertion, with about 5 s measured.
   - Record the failure text.
4. **Forced ordering.**
   - In a scratch clone with the fix, delay the read-loop goroutine's start by 300 ms
     (`server.go`, scratch only).
   - Run `TestRegisterReplacement` 5 times under `-race`: 5/5 must pass.
   - The same delay without the fix fails 5/5 (MADR obs. 5). Re-run it to confirm the harness.

**Verification.** The stability rule for Go, plus steps 3 and 4 recorded with their output.

### P2 — Mobile tests drain their transcript saves (D3, D4; closes F6–F8, F10)

1. **The helper.** Add `test/support/transcripts_container.dart` with
   `ProviderContainer transcriptsTestContainer(void Function(dynamic Function()) addTearDown)`
   (Deviation 1 adds optional named `overrides` and `cache` parameters).
   It:
   - creates the container (with `overrides`, when given);
   - reads `transcriptsProvider.notifier` and sets `debugCache` to ~~a new `TranscriptCache()`~~
     `cache`, or a new `TranscriptCache()` when none is given;
   - registers a teardown that calls `dispose()` and then awaits the cache's `debugWhenIdle`;
   - returns the container.

   Its doc comment names MADR 0173 F6/F7.
2. **The failing file.** Change `history_replay_test.dart`'s `makeContainer` to return
   `transcriptsTestContainer(addTearDown)`. **Not sufficient (Deviation 1):** six of its tests
   then replace the helper's cache. They pass their cache through `makeContainer(cache: …)`
   and drop their `n.debugCache = cache` line; nothing else in them changes.
3. **The audit.** For each of the other nine files in F10, record one verdict:
   - **switched**: it disposes a container that holds transcript state with the default cache;
   - **not needed**: no such container, and the reason;
   - **already drains**: it awaits its own cache.

   A switched file changes its container construction and nothing else.
4. **The comment.** Correct the comment in `fake_path_provider.dart` (F8): a save that outlives
   its test can land in the next test's directory, and `transcriptsTestContainer` is the way to
   avoid it.
5. **The guard.** Add `test/transcripts_container_test.dart`. Using the helper through a
   hand-invoked teardown:
   - apply one event to session `guard`;
   - run the teardown and await it;
   - assert that `transcripts/guard.json` already exists in this test's fake directory.
6. **Fail-first (C2).**
   - In a scratch copy, change the helper's teardown to dispose without awaiting.
   - The guard must fail (the file is absent when the teardown returns). Record it.
7. **Forced ordering.**
   - In scratch clones on Linux (WSL), with `_writeEntry` delayed 300 ms, run
     `history_replay_test.dart` 3 times.
   - With the change: no `Cannot rename` or `Cannot open` collision. At `HEAD`: one in each run
     (MADR obs. 9). Re-run `HEAD` to confirm the harness.

**Verification.** The stability rule for Dart, plus steps 6 and 7 recorded with their output.

### P3 — Record (closes the plan)

- Append `## Execution record (YYYY-MM-DD)` here: the P1 and P2 fail-first outputs, the
  forced-ordering results, the audit verdicts for the nine files, and what this plan predicted
  incorrectly.
- Add `## Observed — execution results` to the MADR, stating whether the `Too many elements` link
  (MADR obs. 9, marked unverified) was ever reproduced.

## Verification (whole plan)

```bash
make race
make -n ci-windows && make ci-windows
cd apps/mobile && dart format --output=none --set-exit-if-changed . && flutter analyze && flutter test
```

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | `hub.register` closes the replaced control only after `h.mu` is released | D1 |
| A2 | The new hub test passes: re-register and a concurrent `h2` register each take under 1 s, and the old peer reads `GoingAway`/`"replaced"` | D2 |
| A3 | The new hub test was seen failing (about 5 s) against the unfixed `hub.go` | D2, C2 |
| A4 | With the read loop delayed 300 ms, `TestRegisterReplacement` passes 5/5 with the fix, and fails 5/5 without it | D1 |
| A5 | `history_replay_test.dart` uses the draining helper; each of the nine other files has a recorded audit verdict | D3 |
| A6 | The guard test passes, and was seen failing against a non-draining helper | D4, C2 |
| A7 | With `_writeEntry` delayed 300 ms on Linux, 3/3 runs show no cache collision, where `HEAD` shows one in 3/3 | D3 |
| A8 | `make race`, `make ci-windows`, `flutter analyze` and `flutter test` pass; nothing under `apps/mobile/lib/` changed | C3 |
| A9 | `TestRegisterReplacement` and the history-replay test are unmodified | C1 |

**A3 and A6 are the criteria most likely to be dropped.** Once a fix is green it is tempting not
to go back and break it on purpose, and without them neither new test is known to test anything.

## Rollout and Rollback

- **The relay change is internal to the hub.** It changes when a replaced host's close frame is
  sent (after the lock, not under it), not whether it is sent. To roll back, revert P1's commit.
- **The mobile change is test-only.**

A release carrying P1 follows the normal release flow, which is not part of this plan.

## Deferred (named, so they are not mistaken for oversights)

- **A lint or harness check that rejects a bare `ProviderContainer` built for transcripts** (MADR
  open question 2). It waits for P2's audit to show whether the pattern is common enough to be
  worth one.
- **Whether other relay paths do blocking I/O under `h.mu`.** P1 fixes the one this failure
  exposed. A sweep of every `h.mu` holder for network I/O would be its own record.
- **Reproducing `Too many elements` itself on demand** (MADR obs. 9, unverified). The collision is
  reproduced and removed. Engineering the exact winning interleaving adds no protection beyond
  A7.

## Deviation 1 — 2026-09-28: the committed helper is defeated by tests that replace its cache

**Found** during P2 step 3's audit. Steps 1 and 2 had already landed (`c7c65d2`, `1c6a04c`),
without steps 3–7. A scratch clone logged every `_writeEntry` with its resolved directory, and
every fake path provider setUp and tearDown. A write is a leak when it arrives after its own
test's directory is torn down. The detector was seen catching a leak first: `history_replay_test.dart`
as it stood at `30041ed9` (before the helper) showed 13 leaked writes out of 20.

| File (macOS, one run each) | Writes | Leaked |
| --- | --- | --- |
| `history_replay_test.dart` at HEAD, using the helper | 23 | **5** |
| `transcript_ingest_test.dart` | 16 | 16 |
| `session_synchronizer_test.dart` | 8 | 8 |
| `staged_images_test.dart` | 1 | 1 |
| `chat_end_session_navigation_test.dart`, `sessions_screen_test.dart` | 0 | 0 |

The five `history_replay` leaks come from six tests (`:226`–`:396`) that call `makeContainer()`
and then set `n.debugCache = cache` with a cache of their own. That replaces the helper's cache.
The teardown waits on the helper's cache, which no longer receives anything, while the disposal
flush goes into the test's cache, which nobody awaits. On the Linux server, with `_writeEntry`
delayed 300 ms (step 7's harness), HEAD shows the collision as often as the code before the
helper:

| Variant, 3 runs each | Collisions per run |
| --- | --- |
| `history_replay_test.dart` at `30041ed9` | 5, 5, 6 |
| HEAD (`1c6a04c`), with the helper | 5, 5, 5 |

Every HEAD collision reads `Transcript cache save failed for s1: PathNotFoundException: Cannot
open file, path = '…/mcremote_test…/transcripts/s1.json.tmp' (OS Error: No such file or
directory, errno = 2)`. The defect is pre-existing in HEAD, and A7 cannot pass without a fix.

`session_synchronizer_test.dart` builds all ten of its containers with `overrides:`, which the
helper's signature as written cannot take.

**Decision (owner, 2026-09-28): option 1, test-only.**

- `transcriptsTestContainer` gains two optional named parameters: `overrides`, passed to the
  container, and `cache`, used as the notifier's cache instead of a new one. The teardown still
  disposes the container and then awaits that cache.
- `history_replay_test.dart`: `makeContainer` forwards `cache`. The six tests pass their cache
  through it and drop their `n.debugCache = cache` line. No assertion changes. "dispose persists
  a pending debounced save immediately" is not touched (C1).
- `session_synchronizer_test.dart` passes its overrides through the helper.

Declined: a `@visibleForTesting` getter on `TranscriptsNotifier`, so the helper could await
whatever cache the notifier holds at teardown. It changes `apps/mobile/lib/`, against C3 and
MADR D5.

**Files added to scope:** the six test bodies in `history_replay_test.dart`, and the helper's
signature. `session_synchronizer_test.dart` was already conditionally in scope.

## Deviation 2 — 2026-09-28: Linux runs go to the Linux server, not WSL

The stability rule and P2 step 7 name Linux (WSL). **Decision (owner, 2026-09-28):** native Linux
runs use the Linux server, which carries the full toolchain (Go 1.26.6, Flutter 3.47.2). macOS
runs stay on the macOS laptop. Every run is in a scratch clone under `/tmp`, made from a bundle
of this repository's HEAD. The server's own checkout is not used. No files added.

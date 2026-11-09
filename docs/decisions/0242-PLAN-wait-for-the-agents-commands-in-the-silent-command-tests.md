---
status: in-progress
date: 2026-11-09
associated-madr: "0242-MADR-wait-for-the-agents-commands-in-the-silent-command-tests.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement waiting for the agent's commands before the silent-command tests read `/loop`

Associated MADR: [0242-MADR-wait-for-the-agents-commands-in-the-silent-command-tests.md](0242-MADR-wait-for-the-agents-commands-in-the-silent-command-tests.md)

## Goal

When this plan is done:

* `newSilentManager` waits until `/loop` shows the agent's commands (MADR D1), so none of the four tests in
  `silent_test.go` depends on goroutine order (MADR F4, as amended by A1 on 2026-11-09; this line first named two);
* both tests' assertions are unchanged, and each still fails on its own message when its behaviour breaks (D2);
* CI is green on the push, which also closes
  [0241-PLAN-register-every-sent-error-code.md](0241-PLAN-register-every-sent-error-code.md)'s last open item.

## Scope

* **Go:** `internal/session/silent_test.go`, the wait in `newSilentManager` only.
* **Records:** this pair; `docs/README.md` (the index); the 0241 PLAN (its 2026-11-09 deviation entry, and its
  close-out once CI is green).
* **Not in scope:** any daemon file, including `manager.go`'s up-front list (MADR option C); the other tests in
  `internal/session`; CI configuration.

## Conventions

* **C1. Start state.** Each phase starts on a clean `master` equal to `origin/master`, or names what differs.
* **C2. Seen failing first.** The failure is reproduced on today's code before the fix. Planted breaks run in a
  scratch clone of HEAD, never in the tree.
* **C3. Go gates** before staging: `make pre-add-check FILES="internal/session/silent_test.go"`, then
  `go test -race -count=1 ./internal/session/`, then `make race`, with output redirected to the scratchpad and the
  exit status read before any filter.
* **C4. Staging.** The agent stages; the owner commits with `git commit --no-edit`. The disclosure guard's redact
  preview runs before each staging.
* **C5. Deviations.** Anything that does not match this plan or the MADR stops for the owner, and is recorded here
  with a date before work continues.

## Implementation Steps

### P0 — The records

1. This pair, `proposed`; the 0241 PLAN's deviation entry naming it.
2. `check_records.py --write-index`; `make check-records markdownlint-docs`; the redact preview. **Stage.** P1
   starts on the owner's word.

### P1 — The wait (MADR D1, D2)

1. **Seen failing on today's code** (C2): `go test -race -cpu=1 -count=500 -run
   'TestSilentMarkMakesTheCanonicalCommandUnavailable' ./internal/session/` fails with
   `/loop unavailable before any mark: … Reason:this agent doesn't offer it` (MADR F3: 127 of 500).
2. **`silent_test.go:204`** becomes:

   ```go
   	// Wait for the agent's own commands, not just a list: Create sends one
   	// before they arrive, with /loop unavailable (0242-MADR F2). Once they
   	// are resolved, /loop is available or carries a silent mark.
   	waitUntil(t, "the agent's commands resolved", func() bool {
   		c, ok := sink.advertised("loop")
   		return ok && (c.Available || c.Reason == command.ReasonSilent)
   	})
   ```

   `command` is already imported by the file. Nothing else changes.
3. **Checks:**
   * the step 1 command, run on the four tests in the file (`-run
     'TestSilentCommandIsMarked|TestCommandsWithOutputAreNotMarked|TestSilentMarkMakesTheCanonicalCommandUnavailable|TestMarkFromAnotherHostIsIgnored'`):
     no failure;
   * **planted breaks** (C2), a script in the scratchpad, on a scratch clone of HEAD with the changed test file
     copied in, each break restored after its run, `-race -count=1`:

     | Break | Expected failure |
     | :--- | :--- |
     | `silentMarks.commands` ignores the host (`silent.go:101`, `&& m.Host == s.host` removed) | `TestMarkFromAnotherHostIsIgnored`: "/loop was made unavailable by another host's mark" |
     | `silentMarks.commands` returns nothing | `TestSilentMarkMakesTheCanonicalCommandUnavailable`: "timed out waiting for /loop unavailable" |

     Neither may fail with "timed out waiting for the agent's commands resolved": that would mean D2 is not holding;
   * C3's Go gates; `make check-records`; the redact preview.
4. **Stage** the test file and this plan's execution record.

### P2 — Close-out

1. CI on the push: the run's conclusion and id, here and in the 0241 PLAN.
2. This plan `complete`, the MADR `accepted`. The 0241 PLAN `complete`, its Confirmation 4 row updated with the run.
3. `check_records.py --write-index`; `make check-records markdownlint-docs`; the redact preview. **Stage.**

## Verification

| MADR Confirmation | Phase | Check |
| :--- | :--- | :--- |
| 1 fails on HEAD one-CPU, passes with D1 | P1 | steps 1 and 3 |
| 2 planted breaks fail on their own messages | P1 | step 3's table |
| 3 tests, race, pre-add | P1 | C3 |
| 4 records, CI | P1, P2 | `make check-records`; CI on the push |

## Rollout and Rollback

* **Rollout:** a test-only change; nothing ships.
* **Rollback:** revert the P1 commit. The test goes back to failing on timing, as on HEAD.
* **Risk:** low. If the setup ever waits on a state that cannot come, it fails after 3 s with the wait's label.

## Execution record

### P0 — 2026-11-09: the records

* This pair, `proposed`; the 0241 PLAN's deviation entry naming it.
* `check_records.py --write-index`; `make check-records markdownlint-docs`: exit 0; the redact preview: 0 files.
* **Staged** for the owner's commit: this pair, the 0241 PLAN and `docs/README.md`.

The owner committed P0 and approved the plan: "committed, proceed".

### P1 — 2026-11-09: the wait

Scripts and logs are in the session scratchpad (`0242-p1-failfirst.log`, `0242-other-silent.log`,
`0242-p1-fixed.log`, `p1_0242_planted.py`, `0242-planted.log`, `0242-preadd.log`, `0242-session.log`,
`0242-race.log`).

* **Start state (C1).** Clean tree. `master` one commit ahead of `origin/master`: the owner's P0 commit, not yet
  pushed.
* **Seen failing on today's code** (step 1): `-race -cpu=1 -count=500` on
  `TestSilentMarkMakesTheCanonicalCommandUnavailable`, exit 1, 140 of 500 failed, every one with:

  ```text
  silent_test.go:265: /loop unavailable before any mark: {Name:loop … Available:false Reason:this agent doesn't offer it}
  ```

* **The change** (step 2), as written: the wait in `newSilentManager`, its label and comment. Nothing else in the
  file changed.
* **Checks** (step 3):
  * the four tests, `-race -cpu=1 -count=500`, with the change: exit 0, no `--- FAIL`, panic or race line
    (2,000 runs);
  * **planted breaks** (`p1_0242_planted.py`, a scratch clone of HEAD with the changed test file): unplanted, exit 0.
    Then each break, exit 1, failing only the expected test, never with the setup's "timed out waiting for the
    agent's commands resolved":

    | Break | Failed | Message |
    | :--- | :--- | :--- |
    | `silentMarks.commands` ignores the host | `TestMarkFromAnotherHostIsIgnored` | `/loop was made unavailable by another host's mark: … Reason:it produced nothing over the remote connection` |
    | `silentMarks.commands` returns nothing | `TestSilentMarkMakesTheCanonicalCommandUnavailable` | `timed out waiting for /loop unavailable` |

    The clone was removed and checked gone.
  * `make pre-add-check FILES="internal/session/silent_test.go"`: "1 file(s) clean (gofmt, golint, govulncheck)".
  * `go test -race -count=1 ./internal/session/`: ok.
  * `make race`: exit 0, 49 `ok` lines, no FAIL, panic or race line.

### Deviation — 2026-11-09: two more tests share the race (MADR A1)

* **Found.** A one-CPU run on HEAD of the other three tests (`-race -cpu=1 -count=300`, started during P0 and
  stopped by `go test`'s 10-minute limit) failed all three: `TestSilentCommandIsMarked` 92 times ("timed out waiting
  for /quiet turn"), `TestCommandsWithOutputAreNotMarked` 81 times ("timed out waiting for /loud turn"),
  `TestMarkFromAnotherHostIsIgnored` 70 times. The MADR's F4 had implied the first two were safe. They fail because
  an agent command sent before the agent has advertised it is refused with a notice and starts no turn
  (`manager.go:2753-2762`).
* **Decision.** The owner chose "Amend MADR + PLAN": F3 gains the three tests' table, F4 is corrected and marked
  A1, and the MADR's More Information records A1 with the original wording. This plan's Goal now names all four
  tests. No decision, step or file changes: D1 covers both paths, and the four-test run above is the evidence.
* After the amendment: `check_records.py --write-index`; `make check-records markdownlint-docs`; the redact preview.
  **Staged:** the test file, this pair.

---
status: complete
date: 2026-10-22
associated-madr: "0209-MADR-wait-for-shell-traps-in-procutil-tests.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement waiting for the shell's trap in `procutil`'s termination tests

Associated MADR: [0209-MADR-wait-for-shell-traps-in-procutil-tests.md](0209-MADR-wait-for-shell-traps-in-procutil-tests.md)

## Goal

`TestTerminateProcessGroupGracefulPath`, `TestTerminateProcessGroupEscalatesToKill` and
`TestTerminateProcessGroupKillsGrandchildren` signal their shell only after it reports its trap
installed. They then pass `-race -count=200` with no fixed sleep, on macOS and on the Linux test
host.

## Scope

* **In scope:** `internal/procutil/procutil_test.go`, this plan, the MADR (status and a note), and
  the records index. One phase, T1.
* **Not in scope:**
  * `procutil_unix.go`, the code under test, which is correct (the MADR's context);
  * the other fixed sleeps in the package (MADR D4);
  * any CI change.

## Implementation Steps

### Rules

* The phase's files are named here; a file outside them is a deviation: stop, prompt, record.
* Plants run on a scratch copy, never in the tree.
* The phase ends with its execution record, staged for the owner.

### T1 — The handshake

**Files:** `internal/procutil/procutil_test.go`, this plan, the MADR, and `docs/README.md` (the
index).

1. **Failing first, on a scratch copy.** Set the three fixed sleeps (`:62`, `:85`, `:106`) to 0,
   with no handshake, and run `go test -race -count=20 -run
   '^TestTerminateProcessGroup(GracefulPath|EscalatesToKill|KillsGrandchildren)$' ./internal/procutil/`.
   * Expected: exit 1, with `:90`, `:109` and the graceful test's `SIGTERM handler did not run`.
   * If the graceful test does not fail, its window is recorded as measured. The MADR's claim
     about it is then corrected, and the phase still goes ahead for the two that do fail.
2. **The helper**, after `startGroup` in `procutil_test.go`:
   * `startTrapped(t *testing.T, trap, body string) (*exec.Cmd, <-chan struct{})`;
   * it runs `/bin/sh -c trap + "; echo ready; " + body` under `procutil.SetProcessGroup`, with
     `cmd.StdoutPipe()`;
   * it reads `len("ready\n")` bytes with `io.ReadFull` in a goroutine, and selects on that and a
     5 s timer. A timeout, a read error or other bytes fail the test with what was read;
   * it starts the `cmd.Wait` goroutine and the `KillProcessGroup` cleanup exactly as `startGroup`
     does.

   The stdout pipe stays open until the process exits. Nothing more is read from it, and the
   scripts write nothing more.
3. **The three tests** call `startTrapped` with their existing script, split into the `trap …`
   command and the rest. Each deletes its `time.Sleep(200 * time.Millisecond)`, and the graceful test also deletes
   its comment "Give the shell a moment to install the trap". Assertions are unchanged.
4. **Gates.**
   * The three tests under `-race -count=200` on macOS: exit 0, 0 failures.
   * On the Linux test host, the same command on a shipped clone, then the clone is removed. The tests are `//go:build unix`, and Linux is the other system they run on.
   * `make test`, `make race`, and `make pre-add-check FILES="internal/procutil/procutil_test.go"`.
5. **Plant, on a scratch copy.** `startTrapped` returns before its read, and the three sleeps are
   at 0. `-count=20` must fail as in step 1. This shows the handshake is what makes them pass.
6. **Records.**
   * T1's execution record goes here.
   * The MADR gets a dated note with the measured results. Its status becomes `accepted` when the
     owner approves this plan.
   * `check_records.py --write-index` runs, and `make check-records` and markdownlint pass.
7. **Stage.** The owner commits.

## Verification

* The three tests have no fixed sleep, and they pass `-race -count=200` on macOS and Linux.
* Step 1's failures and step 5's plant failures are recorded, with their messages.
* `make test`, `make race` and the pre-add check pass.

## Rollout and Rollback

Test-only. Nothing a binary does changes. Reverting T1's commit restores the sleeps.

## Execution record

### T1, 2026-10-22: the handshake

Approved by the owner, "Do t1". Executed as written, with no deviation.

**T1.1, failing first** (`t1_failfirst.py before`): `HEAD`'s test file on a scratch copy, its three
`time.Sleep(200 * time.Millisecond)` set to `time.Sleep(0)`, `-race -count=20`. It exited 1, with
60 failed runs out of 60:

```text
procutil_test.go:90: reported graceful for a child that ignores SIGTERM  x20
procutil_test.go:109: expected escalation  x20
procutil_test.go:78: SIGTERM handler did not run: open <tmp>/TestTerminateProcessGroupGracefulPath…/001/term-ran: no such file or directory  x20
```

The graceful test fails as the MADR predicted from the code, every time, so the step's fallback
was not needed.

**T1.2 and T1.3, what changed** in `internal/procutil/procutil_test.go`:

* `startTrapped(t, trap, body)` is new, after `startGroup`. It runs
  `/bin/sh -c trap + "; echo ready; " + body` in its own group, with a stdout pipe, and registers
  the `KillProcessGroup` cleanup at once.
* It reads `len("ready\n")` bytes in a goroutine and selects on that and a 5 s timer, failing on a
  timeout, an error or other bytes. Then it starts the `cmd.Wait` goroutine.
* `TestTerminateProcessGroupGracefulPath`, `…EscalatesToKill` and `…KillsGrandchildren` call it with
  their trap and body split. Their three 200 ms sleeps are gone, and so is the graceful test's
  comment "Give the shell a moment to install the trap". No assertion changed.
* `io` is imported. The diff stat is one file.

**T1.4, gates.**

| Gate | Result |
| :--- | :--- |
| The three tests, `-race -count=200`, macOS (darwin/arm64) | `ok …/internal/procutil 168.705s`, exit 0 |
| The same on the Linux test host, on a clone of `a0220490` with T1's diff applied | `procutil200: exit 0 in 136s; 1 ok, 0 FAIL lines`. The clone was removed. |
| `go test -race -count=1 ./internal/procutil/` | ok |
| `make test` | exit 0; 43 ok, 0 FAIL lines |
| `make race` | exit 0; 43 ok, 0 FAIL lines |
| `make pre-add-check FILES="internal/procutil/procutil_test.go"` | "1 file(s) clean (gofmt, golint, govulncheck)" |

**T1.5, plant** (`t1_failfirst.py plant`): the working tree's file on a scratch copy, with
`startTrapped` returning before its `select`, through a stand-in that only starts `cmd.Wait`.
`-race -count=20` exited 1, with 60 failed runs out of 60:

```text
procutil_test.go:124: SIGTERM handler did not run: …  x20
procutil_test.go:135: reported graceful for a child that ignores SIGTERM  x20
procutil_test.go:153: expected escalation  x20
```

These are T1.1's three failures at the moved lines, so the handshake is what passes the tests.

**T1.6, records.** The MADR is `accepted`, with a dated note. The index is regenerated.

**This plan is complete.**

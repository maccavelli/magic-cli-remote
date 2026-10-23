---
status: accepted
date: 2026-10-22
decision-makers: Project Owner
consulted: "a scratch copy of internal/procutil's tests at a0220490, run on darwin/arm64 with -race (fb_probe.py); 0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md's probe, where the flake was seen"
informed: anyone who writes a test that signals a child process
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Wait for the shell's trap, not a fixed sleep, in `procutil`'s termination tests

## Context and Problem Statement

`internal/procutil/procutil_test.go` (`//go:build unix`) tests `TerminateProcessGroup`, which sends
SIGTERM to a process group and escalates to SIGKILL after a timeout. It reports `true` when the
group exits within the timeout. Each test starts `/bin/sh -c <script>` in its own group through
`startGroup`. It then sleeps a fixed 200 ms "to install the trap" before signalling.

Three tests depend on the trap being installed before SIGTERM arrives:

| Test | Script | Fails, if SIGTERM lands first, with |
| :--- | :--- | :--- |
| `TestTerminateProcessGroupGracefulPath` (`:55-80`) | `trap 'echo ok > <marker>; exit 0' TERM; while …` | `SIGTERM handler did not run` (`:78`). First read from the code; measured by the PLAN's T1.1, 20 of 20. |
| `TestTerminateProcessGroupEscalatesToKill` (`:83-100`) | `trap '' TERM; while :; do sleep 0.05; done` | `reported graceful for a child that ignores SIGTERM` (`:90`) |
| `TestTerminateProcessGroupKillsGrandchildren` (`:104-116`) | `trap '' TERM; sleep 60 & while …` | `expected escalation` (`:109`) |

**It was seen failing.** During 0207 D1's probe, a full `go test -race ./...` on a scratch clone at
`864d9917` failed `TestTerminateProcessGroupKillsGrandchildren` once:

```text
--- FAIL: TestTerminateProcessGroupKillsGrandchildren (0.21s)
    procutil_test.go:109: expected escalation
```

The same test passed 30 of 30 runs alone at `HEAD` (`-race -count=30`). The package's dependency
closure holds none of the code that probe changed
([0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md](0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md), F-b).

**The cause is reproduced** on a scratch copy at `a0220490`, with the working tree untouched
(`fb_probe.py`, darwin/arm64, `-race`):

| Variant | Result |
| :--- | :--- |
| The two `trap ''` tests as they are, `-count=20` | exit 0, 0 failures |
| The 200 ms sleeps set to 0, `-count=20` | exit 1. `procutil_test.go:109: expected escalation` ×20 and `procutil_test.go:90: reported graceful for a child that ignores SIGTERM` ×20 |
| The script prints `ready` after its trap, the test reads that line before signalling, sleeps at 0, `-count=200` | exit 0, 0 failures |

So SIGTERM can reach the shell before it has run `trap`. The shell then dies of the signal, the
group exits within the timeout, and `TerminateProcessGroup` correctly reports a graceful stop,
which the test reads as a failure. The 200 ms sleep only makes that window unlikely. Under a loaded
`-race` run, `/bin/sh` can take longer than 200 ms to reach its first command, and then the test
fails. The code under test is right; the tests have the bug.

How should these tests know the trap is in place?

## Decision Drivers

* A test fails only when the code is wrong. One that fails on a loaded machine trains people to
  re-run CI instead of reading it.
* Readiness is something the child reports, not something the parent guesses with a timer.
* No assertion is weakened, retried or skipped.
* The fix works wherever these tests run: they are Unix-only, on macOS and Linux runners and hosts.
* The tests get no slower.

## Considered Options

* **A** — A readiness handshake: the script prints a line after its trap, and the test waits for it
* **B** — A longer sleep
* **C** — Poll the shell's ignored-signal mask before signalling
* **D** — Retry or skip the tests under `-race` or in CI

## Decision Outcome

Chosen option: "A", because it removes the race instead of narrowing it, with the same assertions,
on every Unix system. The probe showed it passing 200 runs under `-race` with no sleep at all.

* **D1 — A helper starts a trapped script and waits for its readiness line.** It is
  `startTrapped(t, trap, body)` in `procutil_test.go`. It runs `/bin/sh -c "<trap>; echo ready;
  <body>"` in its own group, as `startGroup` does. It reads exactly `ready\n` from the shell's
  stdout before returning, and fails the test on anything else.
* **D2 — The three tests use it, and their fixed sleeps go.** They are
  `TestTerminateProcessGroupGracefulPath`, `…EscalatesToKill` and `…KillsGrandchildren`, and they keep
  their scripts and assertions unchanged. `echo` is a `sh` builtin, so it runs after `trap` with no
  new process.
* **D3 — The readiness read is bounded.** A shell that never prints `ready` fails the test after
  5 s with a message, rather than hanging until `go test`'s own timeout.
* **D4 — The other fixed sleeps in the package stay,** each for a stated reason:
  * `procutil_test.go:42` waits for a child to exist before `KillProcessGroup`. That test passes
    whether or not the child has started.
  * `reap_test.go` and `owner_test.go` (`//go:build linux`) wait for a process to appear in `/proc`
    with its environment. That is a different readiness, and none of these tests has been seen
    failing.

  They get their own record if one fails.

### Consequences

* Good, because the three tests fail only when `TerminateProcessGroup` is wrong.
* Good, because each test gets about 200 ms faster: it waits for the shell, not a timer.
* Good, because the pattern is in the package for the next test that signals a child.
* Neutral, because the scripts gain an `echo` and the tests a stdout pipe, and no assertion changes.
* Bad, because a script that writes to stdout before its trap would break the handshake. The
  helper's `ready\n` check fails such a test at once and says why.

### Confirmation

* **Failing first, on a scratch copy:** with the fixed sleeps set to 0 and no handshake, the three
  tests fail as in the table above. The graceful test also fails, with `SIGTERM handler did not
  run`.
* With the handshake and no sleep, `go test -race -count=200` on the three tests passes, on macOS
  here and on the Linux test host.
* A plant on a scratch copy, the helper returning before it reads the line, makes them fail again.
  So the handshake, not luck, is what passes them.
* `make test`, `make race` and `make pre-add-check` on the changed file pass.

## Pros and Cons of the Options

### A — Readiness handshake

* Good, because it closes the window: SIGTERM is sent only after the shell has run `trap`.
* Good, because it is portable: `echo` and a pipe work in every `sh`.
* Good, because it is faster than a sleep.
* Neutral, because it adds one helper of about 25 lines.

### B — A longer sleep

* Good, because it is a one-line change.
* Bad, because it only makes the window less likely, so a slower machine still fails.
* Bad, because every run waits longer, by up to a second a test.
* Bad, because it is the workaround the drivers rule out.

### C — Poll the shell's ignored-signal mask

* Good, because the child needs no cooperation.
* Bad, because the mask is readable only from `/proc/<pid>/status` (`SigIgn`) on Linux. macOS has
  no `/proc`, and these tests run there too.
* Bad, because the graceful test installs a handler, not an ignore, which shows up in `SigCgt`. So
  each test would need its own probe.

### D — Retry or skip under `-race` or in CI

* Good, because it costs nothing to write.
* Bad, because the race stays, and a real regression would be retried until it passes, or hidden.
* Bad, because the drivers forbid it.

## More Information

* The code under test is `internal/procutil/procutil_unix.go:71-111`. It is unchanged by this
  record.
* Plan: [0209-PLAN-wait-for-shell-traps-in-procutil-tests.md](0209-PLAN-wait-for-shell-traps-in-procutil-tests.md).
* Where the flake was found:
  [0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md](0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md), F-b,
  and its Q3, whose answer asked for this record.
* *Note, 2026-10-22.* Accepted when the owner approved the PLAN ("Do t1"). Every Confirmation item
  was measured and holds:
  * failing first, all three tests 20 of 20;
  * the handshake, 200 of 200 on macOS and on the Linux test host;
  * the plant, 20 of 20 failing again;
  * `make test` and `make race`, each 43 packages ok.

  The PLAN's execution record has the output.

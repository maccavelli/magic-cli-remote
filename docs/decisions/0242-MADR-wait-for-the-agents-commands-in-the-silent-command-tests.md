---
status: proposed
date: 2026-11-09
decision-makers: Project Owner
consulted: "the CI log of run 42631805993, job Go (test); internal/session/silent_test.go, manager.go, commands.go and silent.go; internal/command/command.go; local repeat runs on HEAD"
informed: anyone writing a test against remote_commands in internal/session
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Wait for the agent's commands before the silent-command tests read `/loop`

## Context and Problem Statement

`TestSilentMarkMakesTheCanonicalCommandUnavailable` (`internal/session/silent_test.go:262`) failed in CI on the push
that carried [0241-MADR-register-every-sent-error-code.md](0241-MADR-register-every-sent-error-code.md). 0241 changed
nothing in this package. The question this record answers: why does the test fail on timing, and what should change?

### F1. The failure [observed 2026-11-09]

CI run `42631805993`, on `85865449`, job Go (test), step "Test (race, all packages)":

```text
--- FAIL: TestSilentMarkMakesTheCanonicalCommandUnavailable (0.01s)
    silent_test.go:265: /loop unavailable before any mark: {Name:loop Hint:[interval] <prompt> Description:Run a prompt on a recurring interval Available:false Reason:this agent doesn't offer it}
```

That step runs `go test -race ./...` once. It is not retried, because a race is a defect rather than a flake
(`.github/workflows/ci.yml`, the comments on the race steps; MADR 0143). So the failure failed the run, and no row was
written to `ci-flakes.tsv`. The ledger has no row for this test or for `internal/session`.

### F2. Why: the setup waits for `/loop` to be listed, and it is listed before the agent's commands arrive [observed]

* `remote_commands` lists the canonical commands only, each with `Available` and a `Reason`
  (`internal/session/commands.go:136-146`). `/loop` is in every list. Before the agent's own commands are known, it
  is unavailable with the reason "this agent doesn't offer it" (`internal/command/command.go:206`).
* `Manager.Create` sends that list up front, so a client has it before the first keystroke
  (`internal/session/manager.go:1419-1422`).
* The test fake sends its `available_commands`, which include `loop`, from `Start` into the session's event channel
  (`silent_test.go:48`, `73-77`). The session's pump reads it on its own goroutine, records the names
  (`manager.go:1523-1531`), passes the event on, and then sends the list again with `/loop` available
  (`manager.go:1745-1765`).
* `newSilentManager` waits only until `/loop` is in a list (`silent_test.go:204`). The up-front list satisfies that.
  When the pump has not yet re-sent the list, the test's first read (`silent_test.go:264`) sees `Available:false`.

### F3. Reproduced on HEAD [observed 2026-11-09]

| Run, `./internal/session/`, `-run TestSilentMarkMakesTheCanonicalCommandUnavailable` | Failures |
| :--- | :--- |
| `-race -count=300`, all CPUs | 0 of 300 |
| `-race -cpu=1 -count=500` | 127 of 500, each with F1's message |

One CPU makes the pump's goroutine wait for the test's, which is the order a loaded CI runner can produce.

### F4. A second test has the same read [observed]

`TestMarkFromAnotherHostIsIgnored` (`silent_test.go:285`) reads `/loop` right after the same setup
(`silent_test.go:293`) and expects it available. It can fail the same way. The other two tests in the file,
`TestSilentCommandIsMarked` and `TestCommandsWithOutputAreNotMarked`, do not read `/loop`'s availability.

### F5. Earlier failures [observed]

The test arrived on 2026-10-22 (`214cc35c`). Of the six failed CI runs since then, only the one in F1 still has a
log naming its failed test. Run `40113049697` (2026-10-24) also failed at "Test (race, all packages)", but its log is
no longer available, so whether it was this test is unknown.

## Decision Drivers

* The tests must stop depending on goroutine order, and still fail when the behaviour they check breaks.
* No daemon change for a test defect. The up-front list is deliberate (`manager.go:1419-1421`).
* Smallest change that is correct.

## Considered Options

* A. The setup waits until `/loop` shows the agent's commands
* B. The setup waits for a `remote_commands` event that follows the agent's `available_commands` event
* C. The daemon holds the first list until the agent's commands arrive
* D. Leave it

## Decision Outcome

Chosen option: "A. The setup waits until `/loop` shows the agent's commands", because it waits on the state the
tests' assertions read, needs one line of test code, and changes no daemon code.

* **D1. The setup's wait.** `newSilentManager` waits until `/loop` is listed **and** is either available or
  unavailable with `command.ReasonSilent`. Either means the agent's commands have been resolved: before that, `/loop`
  can only carry "this agent doesn't offer it". The wait's label says so, and a comment cites this record.
* **D2. The tests' assertions do not change.** `TestSilentMarkMakesTheCanonicalCommandUnavailable` still requires
  `/loop` available before any mark. `TestMarkFromAnotherHostIsIgnored` still requires it available despite another
  host's mark. A mark wrongly applied shows `ReasonSilent`, which ends D1's wait, so that test fails on its own
  assertion and message rather than on a timeout.

### Consequences

* Good, because the setup now waits for what the tests check, so goroutine order no longer decides the result.
* Good, because a regression in either behaviour still fails with the test's own message (D2).
* Neutral, because D1 encodes that `ReasonSilent` is the only other reason `/loop` can have once the agent offers
  it. If the resolver gains another, the setup times out with D1's label: loudly, not silently.
* Bad, because nothing in the tree pins the interleaving. The one-CPU repeat run is the evidence, and it is repeated
  on the change before it is staged.

### Confirmation

1. On HEAD, `-race -cpu=1 -count=500` on `TestSilentMarkMakesTheCanonicalCommandUnavailable` fails with F1's message
   (F3). With D1, the same run on the four tests in `silent_test.go` has no failure.
2. In a scratch clone with D1, two planted breaks each fail with their test's own message, not a timeout:
   * `silentMarks.commands` ignores the host (`internal/session/silent.go:101`):
     `TestMarkFromAnotherHostIsIgnored` fails with "/loop was made unavailable by another host's mark";
   * `silentMarks.commands` returns nothing: `TestSilentMarkMakesTheCanonicalCommandUnavailable` fails waiting for
     "/loop unavailable".
3. `go test -race -count=1 ./internal/session/`, `make race`, and `make pre-add-check` on the changed file pass.
4. `make check-records` passes, and CI passes on the push.

## Pros and Cons of the Options

### A. The setup waits until `/loop` shows the agent's commands (chosen)

* Good, because it waits on the field the assertions read.
* Good, because it is one changed condition in a test helper.
* Neutral, because it relies on the reason values (D1's Neutral consequence).

### B. The setup waits for a `remote_commands` event after the agent's `available_commands` event

* Good, because it names the event order directly.
* Bad, because it is correct only through a lock the test does not see. A list identical to the last one is not
  re-sent (`commands.go:153-156`). B's wait ends only because the pump holds the session's `deliverMu` from taking
  the event (`manager.go:1483`) until it re-sends the list (`manager.go:1764`), and `Create`'s send takes the same
  lock. If that ever changes, B can wait forever.
* Bad, because the sink would need to track the order of two event types, where A reads the latest list, which it
  already keeps.

### C. The daemon holds the first list until the agent's commands arrive

* Good, because a client would never see `/loop` flip.
* Bad, because a client would have no list until the agent replies, which `manager.go:1419-1421` exists to avoid.
* Bad, because it changes daemon behaviour to fix a test.

### D. Leave it

* Good, because it costs nothing now.
* Bad, because the race step is not retried, so about one run in four on a loaded runner fails (F3), on any push.

## More Information

### Related

* [0208-MADR-make-the-provider-layer-agent-neutral.md](0208-MADR-make-the-provider-layer-agent-neutral.md): D10
  introduced silent marks and these tests.
* [0143-MADR-ci-retries-once-and-records-every-retry.md](0143-MADR-ci-retries-once-and-records-every-retry.md): why
  the race step is not retried.
* [0241-PLAN-register-every-sent-error-code.md](0241-PLAN-register-every-sent-error-code.md): its CI confirmation waits
  for this fix (its deviation entry of 2026-11-09).

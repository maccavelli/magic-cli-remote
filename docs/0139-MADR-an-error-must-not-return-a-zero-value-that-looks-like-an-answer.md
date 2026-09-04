---
status: accepted
date: 2026-09-04
decision-makers: maccavelli
consulted: —
informed: —
---

# An error must not return a zero value that looks like an answer — make `goose.Reconcile`'s failure state nameable

## Context and Problem Statement

CI run **33871538458** (`v0.16.4`) failed one test:

```text
--- FAIL: TestReconcileGooseKeyringSwitches (0.00s)
    goose_keyring_test.go:41: outcome = , want switch
FAIL	github.com/maccavelli/magic-cli-remote/internal/daemon	0.480s
```

That message is the entire diagnostic the failure produced. It does not say what
went wrong, and — as this record establishes — **two materially different
environment failures produce exactly that output**, with nothing to tell them
apart.

This record is about that, not about the flake. The flake is a symptom; the
defect is an error path that returns a value indistinguishable from a decision.

### It is not a race, and it is not the change that was being tagged

Three facts, each checked rather than assumed:

* **The failure is a plain assertion**, not a race-detector report. The job runs
  `go test -race ./...`, so a race would have printed a `DATA RACE` trace. None
  appears.
* **The same commit passed.** `v0.16.4` and `master` both point at
  `b8c0d31cda7680b4c683bff7dedb2c7a5556ea91`, and both ran the same job
  (`Go (test; build on tag)`) and the same step (`Test (race, all packages)`).
  The workflow has no tag-conditional logic on that step.

  | run | ref | commit | `internal/daemon` |
  | --- | --- | --- | --- |
  | 33869958799 | `master` | `b8c0d31` | **ok, 3.519s** |
  | 33871538458 | `v0.16.4` | `b8c0d31` | **FAIL, 0.480s** |

* **The tagged work does not touch the failing code.**
  `git diff --name-only 32f8642..b8c0d31` lists 30 files and **none** is under
  `internal/daemon`, `internal/provider/goose` or `internal/provider/credstore`.

The test appears once in the last 15 CI runs, on that one ref.

### Exactly two states produce an empty outcome, and they were enumerated

`Reconcile` (`internal/provider/goose/keyring.go:69`) has five successful
returns, each with a named `GuardOutcome` — `switch`, `hold`, `host_controls`,
`operator_owned`, `no_change` (`:16-27`). None of them is the empty string, so
`outcome = ` can only come from one of its two error returns:

* `:72` — `goosePaths()` failed;
* `:91` — `credstore.SetGooseKeyringDisabled` failed.

Both return **`Result{}`**, whose `Outcome` is the zero value `""`.

Driving those paths directly, with the test's own fixture:

| state induced | result |
| --- | --- |
| `HOME` and `XDG_CONFIG_HOME` both unset | `outcome="" err=$HOME is not defined` |
| goose config dir not writable | `outcome="" err=reconcile goose keyring setting: create temp in …/.config/goose: open …/.mcremote-*.tmp: permission denied` |
| config path replaced by a directory | `outcome="hold"` — **not** empty |
| config file mode `0000` | `outcome="hold"` — **not** empty |
| the test's own happy path | `outcome="switch"` |

The two "hold" rows matter: `fileStoreCanServe` treats an unreadable config as
"assume something is configured and hold"
(`internal/provider/goose/keyring.go:112-116`), so the obvious corruptions do
*not* produce the empty outcome. The empty outcome is a narrow signal meaning
**either the environment has no home directory, or the daemon could not write
where it was told to** — and the daemon's own log line says neither, because
the test hands it a discarded logger.

### The failure is undiagnosable by construction

`reconcileGooseKeyring` (`internal/daemon/goose_keyring.go:15-21`) does log the
error before returning:

```go
res, err := goose.Reconcile(keyringDisabled)
if err != nil {
    log.Warn("could not reconcile goose keyring setting; leaving it as it is",
        slog.String("err", err.Error()))
    return res            // res is Result{} — Outcome is ""
}
```

In production that warning reaches the operator. In the test it does not: the
call is `reconcileGooseKeyring(true, quietLog())`
(`internal/daemon/goose_keyring_test.go:38`), and the assertion three lines
later (`:41`) reports only `res.Outcome`. **The one place the cause is written
is the one place nobody reads.**

The contrast inside the same package is stark. During this investigation a
transient filesystem fault in a container hit two tests in `internal/daemon` at
once:

```text
--- FAIL: TestEnsureTLSFallsBackWhenACMEFails
    certs_test.go:64: … self-signed fallback failed: tls: certs:
    write /tmp/…/tls.key.new: close /tmp/…/tls.key.new.tmp: bad file descriptor

--- FAIL: TestReconcileGooseKeyringSwitches
    outcome = , want switch
```

Same class of fault. One test names the syscall, the path and the errno; the
other says nothing at all.

*(Those container runs are **not** evidence about the CI failure. Docker on this
host produced `bad file descriptor` on `/tmp` under load, with and without a
tmpfs, which makes it an unreliable instrument for filesystem stress. It is
quoted here only for the contrast between two tests observing the same fault.)*

### An error path with no assertions already exists

`TestReconcileGooseKeyringErrorIsNotFatal`
(`internal/daemon/goose_keyring_test.go:99-110`) deliberately induces a write
failure and then asserts **nothing**:

```go
res := reconcileGooseKeyring(true, quietLog())
if res.Outcome == "" && res.Reason == "" {
    t.Log("reconciliation reported nothing actionable, which is acceptable here")
}
```

So the empty outcome was known to be reachable, was written down as acceptable,
and was left unnamed. That is why the CI failure had no vocabulary to describe
itself.

### What is *not* established

Stated plainly, because this repository has twice recorded conclusions drawn
from unverified instruments:

* **Which of the two states CI was in is unknown.** The output cannot say, which
  is the whole point of this record. It is one of the two and cannot be narrowed
  further from the evidence that exists.
* **The failure was not reproduced.** 38 local attempts on macOS — 20 filtered,
  6 full-package shuffled, 12 in the specific test ordering — all `-race`, all
  clean. Linux container attempts are discounted for the reason above.
* **No mechanism is claimed for how the environment reached either state.** The
  `$HOME`-undefined path would require the two `t.Setenv` calls in
  `gooseTestHome` to not be in effect, and both `daemon.Run()` call sites in the
  package are synchronous with bounded contexts, so no leaked boot goroutine is
  known to reach `reconcileGooseKeyring` (`internal/daemon/daemon.go:233`)
  concurrently.

  The **write-failure class is demonstrated**, though not the CI instance — see
  the amendment below.
* **The `0.480s` versus `3.519s` package timing is unexplained.** Note that the
  master run's *cgo-free* step recorded `0.493s` for the same package, so
  `0.480s` is the no-race timing appearing in a race step. That is an
  observation, not a diagnosis.

## Amendment, 2026-09-04: the write-failure state is reachable in practice, and it is pre-existing

While verifying the fix, `internal/daemon` was stressed on the development host
(macOS, `-race`, `-shuffle=on`). Transient `bad file descriptor` failures appear
against the temp directory, landing on a **different test each run**:

```text
write   …/.config/goose/config.yaml: bad file descriptor
sync    …/provider-auth/codex/.manifest.json-26459106: bad file descriptor
unlinkat …/TestReconcileGooseKeyringHostControls525422747: bad file descriptor
readdirnames …/.config/goose: fdopendir goose: bad file descriptor
```

**They are not caused by this change.** The identical stress against an
unmodified `HEAD` tree — extracted with `git archive HEAD`, verified to contain
no `OutcomeError` — fails the same way, on
`TestCredentialGuardPassesTheCodexBinary`, a test in a different file doing a
different operation.

Two things follow.

**The second empty-outcome state is not hypothetical.** A transient filesystem
fault on the goose config path is exactly the input that makes
`SetGooseKeyringDisabled` fail, and one of the observed faults landed on
`…/.config/goose/config.yaml` itself. Under the code as it stood, that produces
`Result{}` and prints `outcome = `. Under this record's decision it produces
`OutcomeError` naming the path. The record's justification is therefore stronger
than "two states are theoretically reachable": one of them is reproducible on
demand, by accident, on a developer machine.

**The fault itself is out of scope.** It is a host-level condition — it fires on
`write`, `sync`, `unlinkat` and `readdirnames` alike, on code this work does not
touch, and it does not reproduce in single runs at ordinary concurrency (8
separate `-count=1 -shuffle=on` runs: 7 clean, 1 failed). Diagnosing macOS
temp-directory behaviour under `-race` is a different investigation and is not
started here. It is recorded so a later reader does not mistake it for a
regression from this change, and so the next person to stress this package
knows the baseline is not clean.

## Decision Drivers

* A test that cannot say why it failed costs more than the bug it catches: this
  one produced a red build, a tagged release, and no information.
* The zero value of a result type must never be a value the type's consumers can
  mistake for an answer.
* Two distinct operational failures — no home directory, and cannot write —
  need different operator responses, so they must not share one output.
* The fix must be deterministic and testable. Both states were induced on demand
  during this investigation, so both can be pinned.
* Nothing here should change what the daemon *does*. The reconciliation's
  behaviour is correct; only its ability to describe a failure is not.

## Considered Options

* **Make the failure state a named outcome, and assert it** — add
  `OutcomeError`, carry the cause on `Result`, and give the tests real
  assertions and explicit preconditions.
* **Assert the error in the test only** — keep `Result{}` on the error paths and
  change `TestReconcileGooseKeyringSwitches` to check the returned error.
* **Retry the reconciliation on failure** — treat it as transient and try again
  before reporting.
* **Quarantine the test** — mark it flaky, skip it in CI, and move on.

## Decision Outcome

Chosen option: "Make the failure state a named outcome, and assert it", because
the defect is in the type, not in the test. `Result{}` is returned on failure
and is also what a caller gets from a zero-valued struct; every consumer — the
daemon's log, the test's assertion, and any future reader of `GuardOutcome` —
inherits the ambiguity. Naming the state removes it once, for all of them.

Concretely:

1. **`OutcomeError` becomes a real outcome**, and both error returns set it
   along with a `Reason` carrying the cause. After this, `Outcome == ""` is
   unreachable from `Reconcile`, so an empty outcome anywhere means an
   uninitialised `Result` and nothing else.
2. **`Result` carries the error text.** `reconcileGooseKeyring` keeps logging
   it, and the value now also carries it, so a caller that is not reading logs
   is not blind.
3. **`TestReconcileGooseKeyringSwitches` asserts its preconditions** — that the
   config path resolves inside the temp home, and that the directory is
   writable — so an environment that cannot satisfy the test fails saying *that*
   rather than failing on the outcome.
4. **Its failure message names the cause.** On a non-`switch` outcome the test
   reports the outcome, the reason and the resolved path.
5. **`TestReconcileGooseKeyringErrorIsNotFatal` gets assertions.** An induced
   failure must produce `OutcomeError` with a non-empty reason — the case that
   is currently written down as "acceptable" and checked for nothing.
6. **Both empty-outcome states get guard tests**, induced exactly as they were
   during this investigation.

Explicitly **not** adopted: retrying, and quarantining. Neither is a fix, and
one of them hides the next occurrence.

### Consequences

* Good, because the next occurrence of this failure — on any host, in any run —
  names which of the two states it hit, so it can be acted on instead of
  re-investigated from scratch.
* Good, because it is a behaviour-preserving change to a value type and its
  tests: the daemon's reconciliation logic is untouched.
* Good, because the two guard tests make the failure modes reachable on demand,
  which is what makes the diagnostic itself testable.
* Neutral, because it does not make the flake go away. If the CI environment
  intermittently has no writable temp directory, that will still fail — it will
  just say so.
* Bad, because `GuardOutcome` gains a member that every `switch` over it should
  handle, and the compiler does not enforce exhaustiveness. Mitigated by the
  daemon's `switch` already having no `default` branch that swallows unknowns
  silently, and by a test asserting the constant set.
* Bad, because it treats the symptom's *legibility* rather than the underlying
  environmental cause, which remains unknown. That is a deliberate ordering: the
  cause cannot be identified without the legibility.

### Confirmation

* Inducing each of the two states produces `OutcomeError` with a reason that
  names it — `$HOME is not defined` for one, the failing path and syscall for
  the other — pinned by two tests that fail against the current code.
* `TestReconcileGooseKeyringSwitches` fails with a message naming the outcome,
  the reason and the resolved config path, verified by running it against a
  deliberately unwritable directory.
* `TestReconcileGooseKeyringErrorIsNotFatal` fails when `Reconcile` returns an
  unnamed failure, verified by reverting `OutcomeError`.
* `go test -race ./internal/... ./cmd/...` stays clean, and the daemon's own
  behaviour on the happy path is unchanged — the same `switch` outcome, the same
  written key.

## Pros and Cons of the Options

### Make the failure state a named outcome, and assert it

* Good, because it fixes the ambiguity at its source: one type change serves the
  log, the test and every future caller.
* Good, because the invariant it creates — `Outcome` is never empty — is simple
  enough to state and to test.
* Good, because the two guard tests convert an un-reproducible CI event into two
  on-demand ones.
* Neutral, because it touches three packages (`goose`, `daemon`, and the tests)
  for what presents as a one-line test failure.
* Bad, because it adds an enum member, and enum members in Go are not checked
  for exhaustive handling.

### Assert the error in the test only

* Good, because it is the smallest possible change and would have named the
  cause on that CI run.
* Bad, because it fixes one caller. The daemon still receives a `Result` whose
  zero value is ambiguous, and the next consumer inherits the same trap.
* Bad, because it leaves `TestReconcileGooseKeyringErrorIsNotFatal` asserting
  nothing, which is how the empty outcome became normalised.

### Retry the reconciliation on failure

* Good, because if the cause is a transient filesystem fault, a retry would
  likely have turned the red build green.
* Bad, because the cause is unknown, and a retry against `$HOME is not defined`
  loops on a permanent condition.
* Bad, because it converts a visible failure into a hidden one. The operator
  would never learn their host could not write goose's config, which is exactly
  the state MADR 0110 exists to make visible.

### Quarantine the test

* Good, because it stops the immediate noise.
* Bad, because the test guards MADR 0110's central behaviour — that the daemon
  writes the secret backend before the first goose engine starts — and skipping
  it removes that guarantee to silence a message that was already too quiet.
* Bad, because it is the workaround this repository's own process rules exclude
  from consideration.

## More Information

* Failing run: GitHub Actions **33871538458**, ref `v0.16.4`, job
  `Go (test; build on tag)`, step `Test (race, all packages)`.
  Passing run of the same commit: **33869958799**, ref `master`.
* Code: `internal/provider/goose/keyring.go:16-27` (the outcome constants),
  `:69-101` (`Reconcile`, including the two `Result{}` error returns at `:72`
  and `:91`), `:103-118` (`fileStoreCanServe`);
  `internal/daemon/goose_keyring.go:15-38`;
  `internal/daemon/goose_keyring_test.go:17-45` and `:99-110`;
  `internal/daemon/daemon.go:233` (the boot-time call);
  `internal/provider/credstore/credstore.go:42-61` (`Home`, `xdg`);
  `internal/provider/credstore/write.go:45-70` (`writeFileAtomic`);
  `internal/provider/credstore/write.go:491-538`
  (`SetGooseKeyringDisabled`).
* The state enumeration in this record was produced by a temporary probe test in
  `internal/daemon` that called `goose.Reconcile` under each induced condition
  and logged `outcome`, `reason` and `err`. It was removed after the run; the
  two states it found become permanent guard tests under the decision above.
* Behaviour being protected:
  [0110-MADR-goose-keyring-prompts-block-headless-launch.md](0110-MADR-goose-keyring-prompts-block-headless-launch.md),
  referenced from `keyring.go:11` and `:50` as MADR 0110 D1/D2/D4/D10.
* The work that was being tagged when this surfaced:
  [0138-MADR-overhaul-provider-surfaces-and-turn-path.md](0138-MADR-overhaul-provider-surfaces-and-turn-path.md).
  It is unrelated to this failure and is named here only so a later reader does
  not have to re-establish that.
* Implementation:
  [0139-PLAN-an-error-must-not-return-a-zero-value-that-looks-like-an-answer.md](0139-PLAN-an-error-must-not-return-a-zero-value-that-looks-like-an-answer.md),
  executed 2026-09-04.

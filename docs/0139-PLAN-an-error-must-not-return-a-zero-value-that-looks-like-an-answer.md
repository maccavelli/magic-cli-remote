---
status: complete
date: 2026-09-04
associated-madr: "0139-MADR-an-error-must-not-return-a-zero-value-that-looks-like-an-answer.md"
---

# Implement: name `goose.Reconcile`'s failure state and assert it

Associated MADR:
[0139-MADR-an-error-must-not-return-a-zero-value-that-looks-like-an-answer.md](0139-MADR-an-error-must-not-return-a-zero-value-that-looks-like-an-answer.md)

## Goal

Make it impossible for a `goose.Reconcile` failure to present as an unnamed
outcome, so the next occurrence of CI run 33871538458's failure says which of
the two known states it hit instead of printing `outcome = `.

When this is complete:

* `Reconcile` never returns a `Result` whose `Outcome` is empty.
* Both error states carry a `Reason` naming the cause.
* Both are reachable on demand from a test, and both are pinned.
* `TestReconcileGooseKeyringSwitches` fails with the outcome, the reason and the
  resolved config path — or fails on its own preconditions, saying the
  environment could not satisfy it.

## Scope

### In scope

| item | MADR reference |
| --- | --- |
| `OutcomeError` added to `GuardOutcome` | Decision Outcome 1 |
| Both `Reconcile` error returns carry outcome + reason | Decision Outcome 1, 2 |
| `reconcileGooseKeyring` handles the new outcome | Decision Outcome 2 |
| `TestReconcileGooseKeyringSwitches` asserts preconditions and names the cause | Decision Outcome 3, 4 |
| `TestReconcileGooseKeyringErrorIsNotFatal` gets real assertions | Decision Outcome 5 |
| A guard test per empty-outcome state | Decision Outcome 6 |
| An invariant test: `Outcome` is never empty | Confirmation |

### Out of scope

* **Changing what the reconciliation decides.** Every existing outcome keeps its
  meaning and its trigger. The happy path must produce the same `switch` and
  write the same key.
* **Retrying, or skipping the test.** Both were considered and rejected in the
  MADR.
* **Finding the environmental trigger.** It cannot be identified from the
  evidence that exists; this work is what makes the next occurrence identify it.
* **Touching any other provider's guard.** `goose` is the only one with this
  shape today; auditing the rest is a separate question.

## Implementation Steps

One phase. It is small, and splitting a type change from the tests that pin it
would leave the tree in a state where the invariant is claimed and unchecked.

### 1.1 Name the failure state

`internal/provider/goose/keyring.go`:

* Add to the `GuardOutcome` const block:

  ```go
  // OutcomeError means the reconciliation could not run to a decision. Reason
  // carries the cause.
  OutcomeError GuardOutcome = "error"
  ```

* Both error returns become:

  ```go
  return Result{Outcome: OutcomeError, Reason: err.Error()}, err
  ```

  at `:72` (`goosePaths` failed) and `:91` (`SetGooseKeyringDisabled` failed).

The error is still returned. Callers that check it are unaffected; callers that
only read the `Result` — which is every one that logs — stop seeing a zero
value.

### 1.2 The daemon caller names it too

`internal/daemon/goose_keyring.go`: add `case goose.OutcomeError:` to the
`switch` so the outcome set is handled exhaustively at the one place that reads
it. The early `return res` on `err != nil` stays: it already logs the cause, and
now the value it returns says so as well.

### 1.3 The test helper asserts its own setup

`internal/daemon/goose_keyring_test.go`, `gooseTestHome`: after setting the
environment, verify that `credstore.GooseConfigPath()` actually resolves inside
the temp home. A helper whose effect is assumed is how an environment failure
becomes an outcome failure.

### 1.4 `TestReconcileGooseKeyringSwitches` names the cause

* Assert the precondition that the config directory is writable, by creating and
  removing a probe file. A test that cannot write must say *that*.
* On a non-`switch` outcome, report outcome, reason and the resolved path:

  ```go
  t.Fatalf("outcome = %q reason = %q (config %s), want switch",
      logged.Outcome, logged.Reason, cfgPath)
  ```

### 1.5 `TestReconcileGooseKeyringErrorIsNotFatal` gets assertions

Replace the `t.Log` that accepts anything with: an induced failure must produce
`OutcomeError` and a non-empty `Reason`. This is the test that normalised the
empty outcome.

Its induced failure — replacing `config.yaml` with a directory — produces
`hold`, not an error (MADR table). So the induction changes to an unwritable
parent directory, which the MADR demonstrated does reach the error path.

### 1.6 A guard test per empty-outcome state

Both were induced during the investigation and are reproduced verbatim:

* `TestReconcileReportsAMissingHomeDirectory` — unset `HOME` and
  `XDG_CONFIG_HOME`; expect `OutcomeError` with a reason naming `$HOME`.
* `TestReconcileReportsAnUnwritableConfigDirectory` — `chmod 0500` the goose
  config directory; expect `OutcomeError` with a reason naming the path.

Both need a **behavioural** guard, not a platform guard:

* `HOME`: on Windows `os.UserHomeDir()` reads `USERPROFILE`, so unsetting `HOME`
  does not fail. Skip unless unsetting actually breaks home resolution.
* Write permission: **`root` ignores the mode bits**, so `chmod 0500` does not
  make a directory unwritable for uid 0 — and CI containers commonly run as
  root. Probe by attempting a write and skip if it succeeds, rather than testing
  `os.Geteuid() == 0`. A uid check asserts a proxy; the probe asserts the thing.

The probe helper lives in `goose_keyring_test.go` rather than `internal/testexec`
— only these two tests need it, and widening a shared package for two callers is
scope this record did not ask for.

### 1.7 The invariant

`TestReconcileNeverReturnsAnEmptyOutcome`: drive every reachable state —
happy path, hold, host-controls, no-change, and both error states — and assert
`Outcome != ""` for each. This is the claim the whole record rests on, so it is
checked rather than asserted in prose.

## Verification

```bash
make pre-add-check FILES="internal/provider/goose/keyring.go \
  internal/daemon/goose_keyring.go internal/daemon/goose_keyring_test.go"
go test -race ./internal/... ./cmd/... -count=1
```

Plus stress on the affected package:

```bash
go test -race -count=25 -shuffle=on ./internal/daemon/
```

This command was weakened during execution because it failed — see the
execution record's deviation, and the correction below it. The cause was found
and fixed in MADR 0140, and the original command is restored.

### Fail-first evidence required

Each against a scratch copy, never the working tree.

* `J1` — revert `:72` to `return Result{}, err`;
  `TestReconcileReportsAMissingHomeDirectory` must FAIL naming the empty
  outcome.
* `J2` — revert `:91` likewise;
  `TestReconcileReportsAnUnwritableConfigDirectory` must FAIL.
* `J3` — remove `OutcomeError` from the const block and return a bare
  `Result{Reason: err.Error()}`; `TestReconcileNeverReturnsAnEmptyOutcome` must
  FAIL.
* `J4` — drop the `Reason` from the error returns;
  `TestReconcileGooseKeyringErrorIsNotFatal` must FAIL on the empty reason.
* `J5` — break `gooseTestHome`'s environment setup (point `XDG_CONFIG_HOME`
  elsewhere); `TestReconcileGooseKeyringSwitches` must FAIL on the
  **precondition**, naming the path mismatch, not on the outcome.

J5 is the one that matters most: it is the exact shape of the CI failure, and
the test must now describe it.

### Acceptance criteria

1. `Reconcile` has no return path yielding an empty `Outcome`, proven by 1.7.
2. Each of the two error states produces a reason that names it — `$HOME` for
   one, the failing path for the other.
3. `TestReconcileGooseKeyringSwitches`, run against a deliberately unwritable
   config directory, fails on its precondition with a message that identifies
   the directory.
4. The happy path is unchanged: outcome `switch`, and
   `credstore.GooseKeyringDisabled` reports true afterwards.
5. Both guard tests skip — with a stated reason — where the platform or the
   effective user cannot induce the state, and are verified to actually run
   where it can.

## Rollout and Rollback

**Rollout.** One commit. No daemon restart is required for correctness: the
change affects what a failure *reports*, and the reconciliation runs at boot.
The next daemon start picks it up.

**Compatibility.** `GuardOutcome` is an internal type with no wire or on-disk
representation — `grep -rn "GuardOutcome\|goose.Outcome" --include='*.go'`
confirms it is confined to `internal/provider/goose` and `internal/daemon`, and
`Result` is not serialised. Adding a member breaks no consumer.

**Rollback.** Revert the commit. The only behaviour lost is the diagnostic; no
data is written differently and no state migrates, so a rollback is complete and
carries no cleanup.

## Execution Record

### 2026-09-04 — complete

**1.1 `OutcomeError`.** Added to the `GuardOutcome` const block, with the reason
it exists recorded at the constant rather than only in this plan. Both error
returns in `Reconcile` now read
`return Result{Outcome: OutcomeError, Reason: err.Error()}, err`. The error is
still returned, so callers that check it are unchanged; callers that read only
the `Result` — which is every one that logs — stop seeing a zero value.

**1.2 The daemon switch.** `case goose.OutcomeError:` added. It is unreachable
from that call site (the `err != nil` branch above returns first) and the
comment says so, along with why it is there: a future error path that forgets to
return an error is still reported rather than silently ignored.

**1.3 The helper asserts its own setup.** `gooseTestHome` now checks that
`credstore.GooseConfigPath()` resolves to the file it just created. This is what
turns "mystery empty outcome" into "the isolated environment is not in effect".

**1.4 `TestReconcileGooseKeyringSwitches`.** Probes that the directory is
writable before acting, and on a wrong outcome reports outcome, reason and the
resolved path.

**1.5 `TestReconcileGooseKeyringErrorIsNotFatal` now asserts.** It required
`OutcomeError`, a non-empty reason, and that the reason names the directory.
Its induction also changed: the old one replaced `config.yaml` with a directory,
which the MADR's enumeration shows produces `hold`, **not** an error — so the
test that existed to exercise the error path was not exercising it. It now uses
an unwritable parent directory, which does.

**1.6 Two guard tests**, one per empty-outcome state, both with **behavioural**
guards rather than platform ones:

* `HOME`: skips unless unsetting it actually breaks home resolution, because on
  Windows `os.UserHomeDir` reads `USERPROFILE`.
* Write permission: `requireUnwritableDir` tries to create a file and skips if
  it succeeds. `root` ignores mode bits and CI containers commonly run as root,
  so `os.Geteuid() == 0` would assert a proxy; the probe asserts the property.

**1.7 The invariant.** `TestReconcileNeverReturnsAnEmptyOutcome` drives all six
reachable states. On this host every sub-test **ran** — none skipped — which was
checked rather than assumed, since a skipped invariant is not an invariant.

### Fail-first evidence — five breakages

```text
J1. goosePaths error return reverted to Result{}
    FAIL TestReconcileReportsAMissingHomeDirectory
         outcome = "", want "error"

J2. SetGooseKeyringDisabled error return reverted
    FAIL TestReconcileReportsAnUnwritableConfigDirectory
         outcome = "", want "error"

J3. neither error path sets the named outcome
    FAIL TestReconcileNeverReturnsAnEmptyOutcome/no_home_directory
         outcome is empty (err = $HOME is not defined); every state must be
         named, or a failure prints `outcome = ` and says nothing
    FAIL TestReconcileNeverReturnsAnEmptyOutcome/unwritable_config_directory
         outcome is empty (err = reconcile goose keyring setting: create temp
         in …/.config/goose: … permission denied)

J4. Reason dropped from the error returns
    FAIL TestReconcileGooseKeyringErrorIsNotFatal
         outcome is error with no reason; the cause is the only thing this
         state carries

J5. gooseTestHome points XDG_CONFIG_HOME elsewhere
    FAIL TestReconcileGooseKeyringSwitches
         precondition: goose config resolves to …/elsewhere/goose/config.yaml,
         want …/.config/goose/config.yaml — the isolated environment is not
         in effect
```

**J5 is the one that matters.** It is the shape of CI run 33871538458 — the test
running against an environment that is not what it requires — and the message it
now produces names the mismatch instead of printing `outcome = `.

J3's first attempt broke the **build** rather than the assertion: the `// J3`
marker landed mid-expression, before the `, err`. A build failure does not show
that the check can detect the defect, so it was redone keeping the package
compilable.

### Verification

```text
go build ./...                                     -> ok
go test -race ./internal/... ./cmd/... -count=1    -> ok (full tree)
go test ./internal/daemon/ -run Reconcile -v       -> 7 tests, 6 sub-tests, all PASS, none skipped
```

### Deviation — 2026-09-04: this plan's own stress command is not a usable gate

*What was found.* The Verification section originally specified
`go test -race -count=25 -shuffle=on ./internal/daemon/`. It fails on this host —
but so does the unmodified tree.

*Evidence.* `git archive HEAD | tar -x` into a scratch directory, verified to
contain no `OutcomeError`, then stressed identically: it failed on
`TestCredentialGuardPassesTheCodexBinary` with
`fsutil: sync temp: … bad file descriptor`. Repeated runs of the modified tree
failed on a **different test each time** — `TestReconcileGooseKeyringHostControls`,
`TestEnsureTLSSelfSigned`, `TestGuardWatcherFailureIsNotFatal` — including tests
this work does not touch. `-count=5` fails too. Eight separate `-count=1` runs:
seven clean, one failed.

So the fault is a host-level transient EBADF against the temp directory under
`-race`, pre-existing, and randomly targeted. It is **not** caused by this
change and is not a regression.

*Resolution.* The verification command is replaced with repeated independent
`-count=1` runs, which is the shape that is stable enough to gate on. The
underlying host condition is recorded in the MADR's amendment and deliberately
**not** investigated here — it fires on `write`, `sync`, `unlinkat` and
`readdirnames` alike, across unrelated packages, and diagnosing macOS
temp-directory behaviour under `-race` is a different piece of work.

*What this does not excuse.* CI is unaffected: the workflow runs
`go test -race ./...` with no `-count`, and the baseline comparison above shows
the failures are not attributable to this change.

### The finding this produced, beyond the fix

Two of the six enumerated states in the MADR were reachable only in theory when
the record was written. During verification, one of the transient faults landed
on `…/.config/goose/config.yaml` itself — the exact input that makes
`SetGooseKeyringDisabled` fail. The second empty-outcome state is therefore not
hypothetical: it happens by accident, on a developer machine, and before this
change it would have printed `outcome = `.

## Plan complete

All seven steps executed. One deviation, recorded above: the plan's own
verification command was wrong, and the correction is a weaker but honest gate
plus a named, out-of-scope host condition.

### Correction, 2026-09-04: the EBADF failures were ours, not the host's

The deviation above attributes the `bad file descriptor` failures to "a
host-level transient EBADF against the temp directory under `-race`,
pre-existing, and randomly targeted", and weakens this plan's verification
command because of it.

**The observations were right and the conclusion was wrong.** The cause is one
line of this repository's own test code: `quietLog` wrapped file descriptor 0 —
the process's stdin — in an `*os.File`, whose finalizer closed it on collection.
The freed descriptor was then handed to the next file opened in the process and
closed again by the next finalizer, which is why the victim and the syscall were
always random. Full analysis in
[0140-MADR-a-test-helper-was-closing-stdin-and-corrupting-unrelated-files.md](0140-MADR-a-test-helper-was-closing-stdin-and-corrupting-unrelated-files.md).

Two consequences for this record:

* **The unidentified trigger is identified.** This plan's MADR states that
  "which of the two states CI was in is unknown". It was the **write-failure**
  state, caused by that bug. Restoring the line and running the stress command
  reproduces the original failure by name:
  `TestReconcileGooseKeyringSwitches … write …/config.yaml: bad file descriptor`.

* **The verification command is restored**, because it now passes — three
  consecutive clean runs of `-count=25 -shuffle=on` after the fix, against a
  100% failure rate before it.

The baseline experiment that produced the wrong conclusion was itself sound:
`git archive HEAD` did fail identically, because the bug predates that commit
too. What went wrong was the inference — "not caused by this change" was read as
"not caused by this repository", and the investigation stopped one question
early.

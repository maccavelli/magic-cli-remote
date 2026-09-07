---
status: accepted
date: 2026-09-04
decision-makers: maccavelli
consulted: —
informed: —
---

# A test helper wrapped file descriptor 0 and its finalizer closed stdin, corrupting unrelated files across `internal/daemon`

## Context and Problem Statement

`internal/daemon` has produced `bad file descriptor` failures on random tests,
on random syscalls, on macOS and inside Linux containers, for as long as anyone
has stressed it. MADR 0139's plan recorded them as "a host-level transient EBADF
against the temp directory under `-race`, pre-existing, and randomly targeted",
declined to investigate, and weakened its own verification command to work
around them.

**That attribution was wrong.** The cause is one line of this repository's own
test code, and it is fully deterministic.

### The line

`internal/daemon/credentials_test.go:15`:

```go
func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.NewFile(0, os.DevNull), &slog.HandlerOptions{Level: slog.LevelError}))
}
```

`os.NewFile(0, os.DevNull)` does not open `/dev/null`. The first argument is a
**file descriptor** and the second is only a display name. Descriptor 0 is
**stdin**. The call wraps the process's own stdin in an `*os.File` and labels it
`/dev/null`.

`os.NewFile` then attaches a closing finalizer — unconditionally, at the end of
`newFile` (`$GOROOT/src/os/file_unix.go:225`):

```go
runtime.SetFinalizer(f.file, (*file).close)
```

So every call to `quietLog()` leaves behind an object whose garbage collection
**closes file descriptor 0**.

### The failure chain, reproduced in one run

A probe test — 50 `quietLog()` calls, a few `runtime.GC()`, then an ordinary
temp file:

```text
fd 0 at start:                            valid
fd 0 after 50 quietLog() + GC:            INVALID: bad file descriptor
a new temp file was assigned fd           0
victim write=… bad file descriptor  sync=… bad file descriptor
```

Four steps, each observed:

1. A finalizer closes fd 0. Stdin is now free.
2. The next `open` in the process is handed **fd 0** — here, a test's own temp
   file.
3. Another `quietLog()` object is finalized and closes fd 0 **again**.
4. The temp file's descriptor is now closed underneath its owner. Every
   subsequent operation on it fails with `EBADF`.

That is why the victim is random: it is whichever file happens to hold the
recycled descriptor. It is why the syscall is random — `write`, `sync`,
`close`, `unlinkat`, `readdirnames`, `fdopendir` have all been seen. And it is
why `-race` and `-count=N` make it worse: more allocation means more collection,
which means more finalizers.

`quietLog` has **12 call sites** across `credentials_test.go` and
`goose_keyring_test.go`, so a single package run poisons the descriptor many
times over.

### It is the cause of the CI failure MADR 0139 investigated

MADR 0139 established that `outcome = ` meant one of exactly two states, and
recorded that "which of the two states CI was in is unknown". It is now known:
**the write-failure state**, caused by this bug.

Driving it directly — poison fd 0, then run the reconciliation the failing test
runs — reproduces the failure on demand:

```text
fd 0 closed by finalizers: true
REPRODUCED: outcome="error" err=… sync temp: sync …/.mcremote-*.tmp: bad file descriptor
REPRODUCED: outcome="error" err=… chmod temp: chmod …/.mcremote-*.tmp: bad file descriptor
REPRODUCED: outcome="error" err=… close temp: close …/.mcremote-*.tmp: bad file descriptor
REPRODUCED: outcome="error" err=… read goose config: read …/config.yaml: bad file descriptor
```

Before 0139's fix, every one of those returned `Result{}` and printed exactly
**`outcome = `** — CI run 33871538458.

`TestReconcileGooseKeyringSwitches` is an unusually likely victim because it
calls `quietLog()` *itself*, immediately before the write that fails.

### What this corrects

Two earlier statements in this repository's records are wrong and are corrected
here rather than left standing:

* **MADR 0139's plan** called the EBADF failures "a host-level transient" and
  "not caused by this change", and replaced its stress verification with a
  weaker command to avoid them. The second half is true — 0139's change did not
  cause them — but the attribution to the host is not, and the weakened
  verification was working around a real bug in this repository.
* **MADR 0139 itself** states that the trigger "cannot be identified from the
  evidence that exists". It can; the evidence needed was a different experiment,
  not a different environment.

The baseline comparison that made "pre-existing" look like "environmental" was
sound as far as it went: `git archive HEAD` did fail identically, because the
bug predates that commit too. Correct evidence, wrong conclusion — "not mine"
was read as "not ours".

### What is not affected

* **Production code.** `quietLog` is test-only (`credentials_test.go`), and no
  non-test file in the repository calls `os.NewFile`. Verified by
  `grep -rn 'os.NewFile(' --include='*.go'`, which returns this one line.
* **Other packages.** The rest of the repository already writes discard loggers
  correctly — `slog.NewTextHandler(io.Discard, nil)` in
  `internal/provider/codex/model_test.go`, `slog.DiscardHandler` in
  `internal/provider/acpagent/stalledpump_test.go`.
* **CI's own correctness.** The workflow runs `go test -race ./...` with no
  `-count`, which is the lowest-pressure shape; that is why this surfaced as a
  rare flake there and a frequent one under local stress.

## Decision Drivers

* The bug is in this repository, is one line, and has a correct one-line fix.
* It has already cost two investigations: it produced the CI failure 0139 spent
  a record diagnosing, and it caused 0139's own verification command to be
  weakened.
* A test helper that mutates process-wide state is a hazard to every test in its
  package, not only the ones that call it — the victims here never called
  `quietLog` at all.
* Any fix must be checkable. The failure is invisible in normal runs and
  presents as an unrelated test failing somewhere else, so a guard is worth more
  than the fix.

## Considered Options

* **Write to `io.Discard`, and add a guard that fd 0 survives the package** —
  fix the helper and pin the invariant it broke.
* **Open `/dev/null` properly** — `os.OpenFile(os.DevNull, os.O_WRONLY, 0)` and
  keep a real file behind the logger.
* **Fix the helper only** — change the line, add no test.
* **Leave it and keep the weakened verification** — treat the flake as a cost of
  doing business.

## Decision Outcome

Chosen option: "Write to `io.Discard`, and add a guard that fd 0 survives the
package", because the helper needs no file at all — it exists to throw output
away — and because the failure mode is one that a reader cannot see. The fix is
obvious in hindsight and was equally obvious before; what stops the next one is
the guard, not the diff.

Concretely:

1. `quietLog` becomes `slog.New(slog.DiscardHandler)`. No descriptor, no
   finalizer, nothing process-wide.
2. A package-level guard asserts that **file descriptor 0 is still open** after
   the `internal/daemon` suite has run and been collected. It fails naming the
   descriptor, so the next helper that wraps one is caught by the package that
   hosts it rather than by an unrelated test in a later CI run.
3. MADR 0139's plan is corrected where it attributes these failures to the host,
   and its verification command is restored to the stress shape it originally
   specified.

### Consequences

* Good, because it removes a fault that has been corrupting arbitrary file
  operations across an entire package for an unknown length of time.
* Good, because it closes MADR 0139's open question — the state CI was in is now
  identified rather than merely bounded.
* Good, because the guard catches the whole class, not this instance: any future
  helper that leaks a standard descriptor fails the same check.
* Good, because 0139's verification can go back to the stress command it wanted,
  which is a stronger gate than the one it settled for.
* Neutral, because the fix does not change what any test asserts. `quietLog`'s
  purpose — a logger that produces no output — is unchanged.
* Bad, because the guard is a whole-package invariant checked in one place, and a
  test that closes fd 0 *deliberately* would have to opt out. No such test
  exists today, and the guard names what to do if one ever does.
* Bad, because it does not explain why this went unnoticed for so long. The
  honest answer is that the symptom always appeared on a test that had nothing
  to do with the cause, and every previous encounter — including one in this
  repository's own records — attributed it to the environment.

### Confirmation

* The probe that closed fd 0 in one run — 50 `quietLog()` calls plus `GC` —
  leaves it valid after the fix. Verified by running the same probe against
  both versions.
* The reproduction of `outcome = ` through fd-0 poisoning no longer reproduces:
  `goose.Reconcile` returns `switch` on every attempt.
* `go test -race -count=25 -shuffle=on ./internal/daemon/` — the command MADR
  0139's plan abandoned — completes clean, repeatedly.
* The guard fails when fd 0 is closed deliberately, verified before it is
  trusted.

## Pros and Cons of the Options

### Write to `io.Discard`, and add a guard that fd 0 survives the package

* Good, because `io.Discard` is what the helper actually wants: no file, no
  descriptor, no finalizer.
* Good, because the guard converts an invisible, action-at-a-distance failure
  into a named one in the package that causes it.
* Neutral, because it adds a test that asserts something no other test in the
  repository asserts, which needs its comment to explain why it exists.
* Bad, because a whole-package invariant cannot say *which* helper broke it,
  only that something did.

### Open `/dev/null` properly

* Good, because it keeps the helper's apparent intent — the name `os.DevNull`
  suggests someone meant to open it.
* Bad, because it opens a real file per call and leaks a descriptor per logger,
  trading a correctness bug for a resource leak.
* Bad, because there is no reason to involve the filesystem at all to discard
  bytes.

### Fix the helper only

* Good, because it is a one-line diff with no new test.
* Bad, because the next helper written the same way reintroduces it, and the
  symptom will again appear somewhere unrelated. This class has already been
  misdiagnosed once in these records.

### Leave it and keep the weakened verification

* Good, because nothing breaks in CI most of the time — the workflow's shape
  makes it rare.
* Bad, because "most of the time" already cost a red release build and a
  full investigation.
* Bad, because it leaves a known descriptor-corrupting bug in a package whose
  tests write credential files, TLS keys and provider manifests. The failures
  seen so far were in tests; the mechanism does not know that.

## More Information

* The line: `internal/daemon/credentials_test.go:15`. Call sites: 12, across
  `credentials_test.go` and `goose_keyring_test.go`.
* The finalizer: `$GOROOT/src/os/file_unix.go:225`,
  `runtime.SetFinalizer(f.file, (*file).close)`, set at the end of `newFile`
  for every path including `os.NewFile`. Go 1.26.6.
* Observed victims and syscalls, across macOS and Linux containers:
  `TestCredentialGuardPassesTheCodexBinary` (`sync`),
  `TestEnsureTLSFallsBackWhenACMEFails` (`close`),
  `TestGuardRecoverReportsEveryProvider` (`unlinkat`),
  `TestGuardCloseIsSafeBeforeStart` (`unlinkat`),
  `TestReconcileGooseKeyringHostControls` (`readdirnames`/`fdopendir`),
  `TestReconcileGooseKeyringErrorIsNotFatal` (`readdirnames`).
  None of `TestEnsureTLS*` or `TestGuard*` calls `quietLog`.
* The CI failure this explains:
  [0139-MADR-an-error-must-not-return-a-zero-value-that-looks-like-an-answer.md](0139-MADR-an-error-must-not-return-a-zero-value-that-looks-like-an-answer.md),
  GitHub Actions run 33871538458. 0139's fix stands on its own merits — it is
  what made the reproduction above legible — and this record supplies the
  trigger it could not name.
* Implementation:
  [0140-PLAN-a-test-helper-was-closing-stdin-and-corrupting-unrelated-files.md](0140-PLAN-a-test-helper-was-closing-stdin-and-corrupting-unrelated-files.md),
  executed 2026-09-04.

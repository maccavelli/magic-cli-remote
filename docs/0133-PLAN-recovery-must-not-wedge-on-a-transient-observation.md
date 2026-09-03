---
status: in-progress
date: 2026-09-02
associated-madr: "0133-MADR-recovery-must-not-wedge-on-a-transient-observation.md"
---

# Implement: startup credential recovery treats an unprovable observation as a no-op

Associated MADR: [0133-MADR-recovery-must-not-wedge-on-a-transient-observation.md](0133-MADR-recovery-must-not-wedge-on-a-transient-observation.md)

## Goal

End the Codex re-authentication loop by making startup recovery answer the same
question the same way reconciliation already does: an unstable, unreadable or
unparseable LIVE changes nothing and is looked at again, and only a stable,
valid, demonstrably older or different-mode LIVE escalates to
`recovery_required`. Unwedge hosts already stuck. Separately, make
`NativeLockPath` name the file that actually gets flocked, so publications
serialize against the provider's own writer as MADR 0074 D25/F12 says they do.

## Scope

**In scope:**

* `internal/providerauth/recovery.go` — stable observation, no-op on
  unprovable, re-evaluation of an existing `recovery_required`.
* `internal/providerauth/reconcile.go` — extract the stable-read helper so both
  paths share one implementation rather than two copies.
* `internal/providerauth/adapter.go` — `Fresher` gains a "not older"
  comparison for the adoption decision; the strict comparison stays available
  where strictness is the question.
* `internal/provider/credstore/credstore.go` — `CodexAuthLockPath`,
  `GrokAuthLockPath`.
* `internal/provider/codex/adapter.go`, `internal/provider/grok/adapter.go` —
  only if the lock change moves the call, not the returned value.
* Tests: `internal/providerauth/recovery_test.go`,
  `internal/providerauth/transaction_test.go`,
  `internal/provider/credstore/home_test.go`,
  `internal/provider/grok/adapter_test.go`,
  `internal/provider/grok/live_device_auth_test.go`.

**Explicitly out of scope:**

* The Codex and Grok provider auth flows themselves — `StartOwnedDeviceAuth`,
  `CoordinatedLogout`, `SetCredential` are untouched.
* The phone app. No protocol field, no UI change; the fix removes the
  `credential_failed` frames rather than presenting them better.
* `mcremote auth-recovery` CLI semantics. It remains the operator's route for
  genuine ambiguity.
* The `Begin`→`Commit` conflict window (`transaction.go:441-445`). Real, but
  not shown to have fired here; Phase 5 adds a regression test only, and any
  change to its behaviour needs its own record.
* Deleting the stray `auth.json.lock.lock` files. They become inert; a daemon
  that removes files in a provider's home is a separate decision.

## Implementation Steps

### Phase 1 — the native lock path

1. Change `CodexAuthLockPath` (`credstore.go:247-253`) and `GrokAuthLockPath`
   (`credstore.go:197-203`) to return the **base** credential path — the same
   value as `CodexAuthPath` / `GrokAuthPath` — since `fsutil.WithLock` appends
   `.lock` itself (`lock_unix.go:21`, `lock_windows.go:27`). Update both
   doc comments to say the returned value is the path `WithLock` derives the
   lock from, so the next reader cannot repeat the mistake.
2. Update the string assertions that encode the old value:
   `credstore/home_test.go:91` and `:107`, `grok/adapter_test.go:34`,
   `grok/live_device_auth_test.go:60`.
3. Add the assertion that would have caught this, in
   `internal/providerauth/transaction_test.go`: hold an `flock` on
   `<liveDir>/auth.json.lock` from the test, then require a `Commit` to block
   and time out with `fsutil: flock`. It must name the file, not the string.

Commit at the end of the phase.

### Phase 2 — one stable-read implementation, shared

4. Extract the body of `stableObservation` (`reconcile.go:78-104`) into a
   helper both callers use unchanged. Behaviour and the
   `StableReadInterval`/`StableReadDeadline` bounds (`bounds.go:33-34`) stay
   exactly as they are; this step must be a pure move, provable by the existing
   reconcile tests passing untouched.
5. Change `observeLive` (`recovery.go:37`) to use it, so `Recover` sees the
   same settled-file discipline `Reconcile` has.

Commit at the end of the phase.

### Phase 3 — unprovable is a no-op; equal is not older

6. ~~In `recoverIdle` (`recovery.go:82-138`), separate the two cases the single
   `finish(m, StateRecoveryRequired)` at `:138` currently merges: LIVE
   unstable, unreadable, or invalid → leave the manifest untouched and return
   the current state; LIVE stable and valid but not adoptable → escalate
   exactly as today.~~ **Superseded 2026-09-02 by the deviation below.** Add a
   `stable` field to `observation`, set false only on `stableObservation`'s
   deadline path, and in `recoverIdle` separate:
   * LIVE **unstable** (`!obs.stable`) → leave the manifest untouched and
     return the current state. Log at debug, not warn: it is not a fault.
   * LIVE settled and invalid, or absent → escalate, unchanged from today, so
     `recovery_test.go:95` and `:109` keep passing as written.
   * LIVE settled and valid but not adoptable → escalate exactly as today.
7. Add `NotOlder` alongside `Fresher` in `adapter.go`, with the same mode gate
   and the same refusal to guess without a comparable signal, differing only in
   accepting equality. Use it for the adoption decision in `recoverIdle` and
   `reconcileLocked`. Leave `Fresher` in place and in use wherever strictness
   is genuinely the question, rather than loosening it for every caller at once.
8. Confirm the escalation path still fires for a stable, valid, strictly older
   LIVE — that is D24 and it must not move.

Commit at the end of the phase.

### Phase 4 — unwedge hosts already stuck

9. In `recoverLocked` (`recovery.go:59-61`), stop returning immediately for
   `StateRecoveryRequired`. Instead re-run the Phase 3 evaluation against a
   fresh stable observation:
   * adoptable → adopt and return `StateIdle`;
   * still stable, valid and older, or a different mode → stay
     `recovery_required`;
   * unprovable → stay `recovery_required` (unchanged, because there is no
     prior good state to preserve here).
10. Gate this on there being no recorded operator resolution, so the
    re-evaluation can never override a decision someone made. If the manifest
    gains no such marker today, add one in `ResolveRecovery` rather than
    inferring it — inference is what this whole record is about.
11. Update the daemon's warning text (`internal/daemon/credentials.go:81`) only
    if step 9 changes when it fires. Do not reword it otherwise.

Commit at the end of the phase.

### Phase 5 — tests, each seen to fail first

Per `AGENTS.md`, every new assertion is run against a deliberately broken input
before it is trusted. Run each against **unmodified** code, or a scratch copy
under the scratchpad — never by dirtying the tree.

12. `recovery_test.go`: LIVE torn on the first read and settled on the second.
    Assert the state is not `recovery_required` and the manifest is byte-equal
    to before. **Against today's code this must wedge** — that is the
    regression being fixed.
13. `recovery_test.go`: LIVE is Codex's `{}` stub. Same assertion. This is the
    2026-08-21 shape from MADR 0074 §15.13.
14. `recovery_test.go`: equal `last_refresh`, different bytes → adopted.
15. `recovery_test.go`: stable, valid, strictly **older** LIVE → still
    `recovery_required`. This one must pass before and after; if it ever fails,
    the change went too far.
16. `recovery_test.go`: a manifest already in `recovery_required` with an
    adoptable LIVE → recovers to `StateIdle` on the next `Recover`. With an
    operator resolution recorded → stays.
17. `transaction_test.go`: the flock assertion from step 3, plus a regression
    test for the `Begin`→`Commit` conflict window — LIVE rewritten mid-flow
    must still return `ErrConflict` and leave LIVE untouched. Documents the
    window; changes nothing about it.

Commit at the end of the phase.

### Phase 6 — verify on this host

18. Build and restart the daemon. Confirm the start logs
    `credential state recovered provider=codex state=idle` where every start
    since 2026-08-21 logged the operator-decision warning.
19. Leave it running until Codex rotates its token, then confirm a new
    generation with `source: refresh` appears in
    `~/.local/share/mcremote/provider-auth/codex/manifest.json` — the thing
    that has not happened since 2026-08-23.
20. From the phone, open Codex in Settings and confirm no `credential_failed`
    frame, with no sign-in performed.

## Verification

```bash
make pre-add-check
go test ./internal/providerauth/... ./internal/provider/credstore/... \
        ./internal/provider/codex/... ./internal/provider/grok/... -count=1
go test ./... -count=1
make vet
make lint
```

`-count=1` is required: these tests touch real files and a cached PASS would
hide a regression in exactly the layer under change.

Host-level checks, read-only:

```bash
grep -a "credential state" ~/Library/Logs/mcremote/mcremote.err.log | tail -20
python3 -c "import json;print(json.dumps(json.load(open('$HOME/.local/share/mcremote/provider-auth/codex/manifest.json')),indent=2))"
ls -la ~/.codex/auth.json.lock* ~/.grok/auth.json.lock*
```

### Acceptance criteria

* Every command above passes with no findings.
* Tests 12, 13 and 14 each fail against unmodified code, with the failure
  output recorded in this plan — not summarised.
* Test 15 passes before and after, demonstrating D24 survived.
* On this host, a daemon start logs
  `credential state recovered provider=codex state=idle`.
* A `source: refresh` generation appears in the Codex manifest after the next
  token rotation.
* No sign-in is required to reach either of the two previous criteria.
* `git diff --stat` touches only the files named in Scope.

## Rollout and Rollback

**Rollout.** Daemon-side only. It takes effect on the next daemon restart, and
requires no phone update, no release, and no user action — a wedged host clears
itself on that restart. Nothing in the on-disk layout changes, so a host can run
the new daemon against a manifest written by the old one and the reverse.

**Rollback.** Revert the phase commits. The manifest format, the generation
files, and the credential are untouched by this work, so a revert restores the
previous behaviour exactly — including the wedge. A host that recovered under
the new code and then rolled back keeps the credential it adopted; it is a
normal `current` generation with no marker distinguishing it.

**Interim workaround, available now and unaffected by this plan:**
`mcremote auth-recovery status`, then `mcremote auth-recovery choose live`
adopts the credential Codex is already using without another sign-in.

**Deferred, deliberately.** Narrowing the `Begin`→`Commit` conflict window —
for example by re-reading LIVE under the native lock and re-validating rather
than comparing against a fingerprint taken minutes earlier — is a change to
transaction semantics and needs its own MADR. Phase 5 only pins the current
behaviour down with a test.

## Execution Record

### Phase 1 — 2026-09-02, complete

Commit `1c84b3f`. `CodexAuthLockPath` and `GrokAuthLockPath` now return the base
credential path and let `fsutil.WithLock` derive the lock file; the four string
assertions that encoded the old value were updated, and the `fakeAdapter`
fixtures in `crash_test.go`, `transaction_test.go` and
`transaction_hygiene_test.go` were moved to the corrected convention.

**Instruments, seen to fail.** The new
`TestAuthLockPathsFlockTheFileTheProviderHonors` was written and run **first,
against unmodified paths**, and failed for both providers by name:

```text
--- FAIL: TestAuthLockPathsFlockTheFileTheProviderHonors/codex
    no lock file at …/001/auth.json.lock: no such file or directory
    flocked …/001/auth.json.lock.lock instead of …/001/auth.json.lock: the .lock suffix is applied twice
--- FAIL: TestAuthLockPathsFlockTheFileTheProviderHonors/grok
    no lock file at …/002/auth.json.lock: no such file or directory
    flocked …/002/auth.json.lock.lock instead of …/002/auth.json.lock: the .lock suffix is applied twice
```

`TestCommitBlocksOnTheProviderNativeLockFile` was built as a two-case table
rather than a single assertion, so the demonstration is permanent instead of a
one-off manual run: the `pre-0133 doubled suffix` case asserts that Commit
**sails past** an external writer holding `auth.json.lock` and leaves an
`auth.json.lock.lock` behind. Both cases pass, which is what makes the first one
evidence — the assertion is shown to discriminate between the two conventions,
not merely to be satisfiable.

```text
=== RUN   TestCommitBlocksOnTheProviderNativeLockFile/base_path_derives_the_provider's_lock_file
=== RUN   TestCommitBlocksOnTheProviderNativeLockFile/pre-0133_doubled_suffix_locks_a_file_nobody_else_takes
--- PASS: TestCommitBlocksOnTheProviderNativeLockFile (0.40s)
```

**Verification.**

```text
go test ./internal/provider/credstore/... ./internal/provider/grok/... -count=1  -> ok, ok
go test ./internal/providerauth/... -count=1                                     -> ok (21.593s)
gofmt -l <the three packages>                                                    -> clean
go vet <the three packages>                                                      -> clean
```

### Deviations

**2026-09-02 — Phase 3 step 6 was wrong as written; owner chose to distinguish
unstable from stably-invalid.** Found while building Phase 2.

*Evidence.* Step 6 put "unreadable or invalid" on the no-op side. Two
transition-table rows document the opposite and are tested:
`idle/live absent requires recovery` (`recovery_test.go:95`) and
`idle/live invalid requires recovery` (`:109`). Reading
`stableObservation` (`reconcile.go:78-104`) also showed the step to be
unnecessary for its stated purpose: a torn read disagrees with the settled read,
the loop continues, and two matching reads return a **valid** observation — so
Phase 2 alone resolves a torn write, and after it "invalid" means invalid twice
100 ms apart. The genuine gap is narrower: the deadline path returns
`observation{fp: first.fp, valid: false}`, which is indistinguishable from a
settled-but-invalid result, so "never settled" cannot be acted on separately.

*Resolutions offered.* (a) add a `stable` field and no-op only on `!stable`,
keeping absent and invalid escalating; (b) build step 6 as written; (c) rely on
Phase 2 alone and change no rule. **Owner chose (a).**

*Consequence of doing nothing, stated at the time:* under (b) a genuinely
corrupt or deleted credential would never escalate, so the phone would report
nothing wrong while every session failed, and the operator would lose the
restore prompt.

*Docs amended before execution:* the MADR's Decision Outcome point 2 carries a
struck-through original and a dated amendment; step 6 above is struck through
and replaced. No file left Scope.

### Phases 2-3 — 2026-09-02, complete

**Phase 2.** `observeLive` is now three lines that call `stableObservation`; the
duplicated single-read body is gone. `observation` gained `stable`, set true on
every settled read in `observeWithBytes` and false only on `stableObservation`'s
deadline path — the two were previously indistinguishable, both surfacing as
`valid: false`.

The validated **bytes** are now threaded from the observation into
`recoverIdle` rather than re-read. The old code called `readLiveBytes()` after
validating, so the generation it wrote was a *third* read whose contents need
not have matched the fingerprint the manifest recorded for it. Fixing that is
what step 5 has to mean: using the stable read and then re-reading anyway
reintroduces the race it removes. `readLiveBytes` became dead and was deleted.

**Phase 3.** `recoverIdle` defers on `!obs.stable`, **before** the fingerprint
comparisons rather than after — an unstable observation's fingerprint is a value
read from a file mid-rewrite and is not evidence of anything, including of a
match. `NotOlder` was added beside `Fresher`, both delegating to one `order`
helper so the mode gate and the "no signal, no guess" rule cannot drift apart;
`recoverIdle` and `reconcileLocked` use `NotOlder`.

**What the new tests establish, stated exactly.** Run against a
`git worktree` at the Phase 1 baseline (`1c84b3f`), which was removed
afterwards:

```text
--- FAIL: TestEqualOrderingIsAdoptedNotEscalated (0.04s)
    unstable_live_test.go:113: state = recovery_required, want idle: an
    equal-ordering rewrite has not gone backward
```

`TestUnstableLiveDefersInsteadOfWedging` **passed on that baseline**. It is
therefore not a regression test for the original wedge, and its doc comment now
says so. The old single-read path usually caught one of the churn's small
complete writes and adopted it; what the test does prove is that the deferral
branch is reachable and leaves every generation untouched when reached.

**Verification.**

```text
go test ./internal/providerauth/... -count=1   -> ok (28.517s)
go test ./internal/... -count=1                -> 41 packages ok, 0 FAIL
go build ./... , go vet, gofmt -l              -> clean
```

### Deviations (continued)

**2026-09-02 — plan step 12's prediction was wrong; recorded rather than
worked around.** Step 12 asserts the torn-read test "must wedge" against
unmodified code. It does not: see above. Nothing was changed to make it appear
to — the test's claim was corrected to what it actually establishes.

This exposes a real limit of the approved design that the plan did not state:
**a torn write that stays torn across a full `StableReadInterval` is still
escalated.** Two invalid reads 100 ms apart are evidence of corruption, not of a
bad instant, and by the 2026-09-02 deviation above that must escalate. So the
mechanism that demonstrably wedges on the old code is the equal-ordering one,
and the general protection against *any* escalation being permanent is Phase 4's
re-evaluation — which makes Phase 4 load-bearing for the reported symptom rather
than merely a convenience for already-stuck hosts. No decision changed; the
MADR's Confirmation section already lists both tests.

**2026-09-02 — one file added to Phase 1's scope.**
`internal/provider/codex/adapter_test.go:34` carried the same
`lock != live+".lock"` assertion as its grok twin and was missed by step 2,
which named only the grok and credstore tests. Caught by the full-suite run, not
by the per-package one. Updated identically; no production file was added.

### Phase 4 — 2026-09-02, complete

`recoverLocked` no longer returns unconditionally for `StateRecoveryRequired`.
It falls through to `recoverIdle`, which is the correct evaluation for it
unchanged: an unstable read still defers, a LIVE that matches CURRENT or is
adoptable clears the state, and anything else escalates again — a no-op for an
already-escalated provider. The same evidence test either way, so the state
cannot mean two different things depending on how it was reached.

**The operator gate (step 10) earned its keep, but not for the reason the step
gave.** Being in `recovery_required` is itself proof no operator decision is in
effect, because every successful `ResolveRecovery` leaves the state — so a
marker would be dead weight for the case the step described. One narrow case is
real: `ResolveRecovery` is documented to leave the manifest in
`recovery_required` when it **fails**, so a human may have ruled on a state that
re-evaluation would otherwise revisit. `Manifest.OperatorChoice` /
`OperatorChoiceAt` are therefore written *before* the resolution is applied, and
cleared on each of the three paths that successfully leave the state
(`resolveLoggedOut`, `resolveAdoptLive`, `resolveRepublish`). A marker that
survives is a resolution that failed, and that is still terminal.

**Step 11: no change made.** The daemon warning fires exactly when
`RecoverAll` reports `recovery_required`, which now means "still genuinely
ambiguous" — what the text already says. Rewording it would have been churn.

**The documented transition row was amended, not deleted.**
`recovery_required makes no automatic mutation` failed as soon as step 9 landed:

```text
--- FAIL: TestRecoveryTransitionTable/recovery_required_makes_no_automatic_mutation
    recovery_test.go:311: state = idle, want recovery_required
```

That is the MADR's decision arriving, not a regression — and `state = idle` came
from the sub-case where LIVE **equals CURRENT**, which is the least ambiguous
input there is and precisely what was wedging. The row is now
`recovery_required is re-evaluated, never mutates live` with three sub-cases:
LIVE matching CURRENT clears the state; an older LIVE keeps it; a recorded
operator attempt keeps it even when LIVE is adoptable. The invariant the row
actually protected — recovery never mutates LIVE — is asserted in all three, and
the amendment is annotated in place rather than silently rewritten.

**Verification.**

```text
go test ./internal/providerauth/ -run TestRecoveryTransitionTable -count=1 -v
  -> all 16 rows PASS, including idle/live_absent_requires_recovery and
     idle/live_invalid_requires_recovery, which the 2026-09-02 deviation preserved
go test ./internal/... -count=1   -> 41 packages ok, 0 FAIL
go vet ./internal/... , gofmt -l internal   -> clean
```

### Phase 5 — 2026-09-02, complete

Steps 12-14 landed in Phase 3 and step 16 in Phase 4. **Step 17 needed no new
test:** `TestCommitConflictOnStaleLive` (`transaction_test.go:266-288`) already
asserts exactly what the step describes — LIVE rewritten between Begin and
Commit yields `ErrConflict` and the other writer's bytes survive. Verified by
reading it rather than by adding a duplicate.

`make pre-add-check` → `713 file(s) clean (gofmt, golint, govulncheck)`.
`make lint` → clean. `go test ./internal/... -count=1` → 41 packages ok.

### Phase 6 — 2026-09-02, FAILED its acceptance criterion

`make install` built and installed `0.16.1.gb8cf5b2` and restarted the
LaunchAgent. Step 18 requires the start to log
`credential state recovered provider=codex state=idle`. It did not:

```text
2026-09-02T22:32:40  WARN provider=codex  credential state needs an operator decision …
2026-09-02T22:32:40  INFO provider=grok   credential state recovered  state=idle
```

**Why, established on the host.** LIVE is not torn, not equal-ordered, and not
older. It is the three-byte stub:

```text
$ ls -la ~/.codex/auth.json
-rw-------  3 bytes  Sep  2 22:29
$ cat ~/.codex/auth.json
{}
$ codex login status
Logged in using ChatGPT
$ grep -E "credential|store|keychain" ~/.codex/config.toml
(no matches)
```

codex-cli 0.152.1 holds a live ChatGPT subscription session while leaving `{}`
in the file mcremote protects. That is a **stable, valid-JSON, no-auth-material**
observation, which the 2026-09-02 deviation deliberately kept escalating — two
settled reads of a file with no credential in it is not a bad instant.

**So 0133 does not fix the reported symptom.** It fixes the mechanism that made
the state *permanent* — which is real and is why this host stayed wedged from
2026-08-23 to 2026-09-02 — but the *trigger* is not one of the three transient
inputs the MADR reasoned about. The MADR named the `{}` stub as a transient
artefact of Codex's own login (§15.13); on this host at 22:29 it is the resting
state, an hour after a successful sign-in.

Ruled out as a cause of the 22:29 write: this session's test runs. Every codex
test isolates the home with `t.Setenv("CODEX_HOME", t.TempDir())`
(`live_0080_test.go:33` and the rest); codex's own `logs_2.sqlite` was being
written at 22:27-22:32, so a real codex process was live; `codex login status`
still succeeds, which it would not if a test had destroyed the credential; and
the warning has fired since 2026-08-21, before any of this work.

**Steps 19 and 20 are not attempted.** A `refresh` generation cannot appear
while LIVE holds no credential to refresh, and the phone will keep receiving
`credential_failed` until the state below is addressed.

## Outstanding: the trigger needs its own decision

`ObserveCredentialStore` already models this exact state as `RealityExternal`
("the CLI is authenticated but not from the file"), and `backupProjection`
already projects it to the phone as `BackupUnsupported`. Recovery does not
consult either: it sees an unusable file and escalates.

Whether recovery should treat `RealityExternal` as "nothing to protect, not an
ambiguity" is a new architectural decision — it changes what
`recovery_required` means and adds a CLI probe to a path that deliberately has
none. It is **not** in this plan's Scope and is not being implemented here. It
needs its own MADR and plan.

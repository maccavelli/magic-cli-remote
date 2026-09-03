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

6. In `recoverIdle` (`recovery.go:82-138`), separate the two cases the single
   `finish(m, StateRecoveryRequired)` at `:138` currently merges:
   * LIVE unstable, unreadable, or invalid → leave the manifest untouched and
     return the current state. Log at debug, not warn: it is not a fault.
   * LIVE stable and valid but not adoptable → escalate exactly as today.
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

Not started. `status: proposed` — awaiting approval to execute.

---
status: in-progress
date: 2026-09-02
associated-madr: "0134-MADR-an-external-credential-store-is-not-an-ambiguity.md"
---

# Implement: an external Codex credential store is a supported state, not an ambiguity

Associated MADR: [0134-MADR-an-external-credential-store-is-not-an-ambiguity.md](0134-MADR-an-external-credential-store-is-not-an-ambiguity.md)

## Goal

Stop the daemon demanding an operator decision, and the phone reporting a
credential failure, on a host where Codex is signed in but keeps the credential
outside `auth.json`. Recovery asks the adapter what it already knows, records a
distinct `external` state, leaves it automatically when a usable credential
appears in the file, and keeps escalating a genuinely broken one.

## Scope

**In scope:**

* `internal/providerauth/manifest.go` — the `external` state.
* `internal/providerauth/adapter.go` — the optional `RealityReporter`
  interface. `Adapter` itself is unchanged.
* `internal/providerauth/recovery.go` — the probe and the classification.
* `internal/providerauth/reconcile.go` — treat `external` like `idle` for
  adoption; do **not** probe here.
* `internal/providerauth/transaction.go` — managed mutations in `external`
  return `ErrUnsupportedBackend`.
* `internal/provider/codex/adapter.go` — satisfy `RealityReporter` (the
  `Reality` method already exists; this is a wiring check, not a rewrite).
* `internal/daemon/credentials.go` — `external` is reported, not warned about.
* Tests: `internal/providerauth/recovery_test.go`,
  `internal/providerauth/transaction_test.go`, plus a new
  `internal/providerauth/external_state_test.go`.

**Explicitly out of scope:**

* The phone app and the protocol. `backupProjection` already returns
  `BackupUnsupported` for this reality, so nothing new crosses the wire.
* `grok`. It does not implement `RealityReporter` and must be provably
  unaffected.
* `mcremote auth-recovery`. Its three choices stay as they are; they simply
  stop being offered for a state that is no longer `recovery_required`.
* Finding or changing where codex-cli 0.152.1 stores the credential, and any
  attempt to make mcremote protect it. The MADR records that location as
  unknown and the decision does not depend on it.
* MADR 0133's rules. A settled, unusable LIVE with **no** authenticated CLI
  still escalates, unchanged.

## Implementation Steps

### Phase 1 — the optional capability, with no behaviour change

1. Add to `adapter.go`:

   ```go
   // RealityReporter is an optional Adapter capability. An adapter that can
   // observe where its provider's credential actually lives implements it;
   // the coordinator type-asserts and falls back to today's behaviour.
   type RealityReporter interface {
       CredentialIsExternal(ctx context.Context) (bool, error)
   }
   ```

   A boolean, not the `StoreReality` enum: that enum lives in the codex
   package, and `providerauth` must not import a provider.
2. Implement it on `internal/provider/codex/adapter.go` in terms of the
   existing `Reality`, using `ObserveCredentialStoreCached` with
   `realityWindow` so startup does not re-probe what the phone's last
   `providers.list` already asked.
3. Assert the adapter satisfies the interface at compile time
   (`var _ providerauth.RealityReporter = (*CredentialAdapter)(nil)`).
   Nothing calls it yet; the suite must pass unchanged.

Commit at the end of the phase.

### Phase 2 — the state

4. Add `StateExternal State = "external"` to `manifest.go:62-72`, documented as
   "the provider is authenticated from a store this coordinator cannot see;
   nothing to protect, and nothing for an operator to decide".
5. Audit every `switch m.State` and every `case State…` in the package and
   decide each explicitly rather than letting `external` fall into a
   `default:` by accident. `reconcileLocked` (`reconcile.go:31`) must treat it
   like `idle` — adoption is exactly how the state is left.
6. `Begin` permits a transaction in `external`, for the same reason it permits
   one in `recovery_required`: a login is what produces a protectable
   credential.
7. `Commit`, `RecordLogout` and `MarkRevoked` in `external` return
   `ErrUnsupportedBackend`.

Commit at the end of the phase.

### Phase 3 — classification in recovery

8. In `recoverIdle`, after the `!obs.stable` deferral and before the escalation
   at `recovery.go:138`: when the observation is settled and **not** valid, and
   the adapter implements `RealityReporter`, and it reports external, record
   `StateExternal` and return. Preserve every generation; write nothing to LIVE.
9. Order matters and must be asserted: an **adoptable** LIVE is adopted without
   probing at all, so a working host never spawns a process. Only the path that
   was about to escalate probes.
10. A probe error is not a verdict: on error, fall through to today's
    escalation. Never let an unreachable CLI invent a healthy state.
11. `external` is re-evaluated on every `Recover` exactly like `recovery_required`
    after MADR 0133 — the state is a cache of an environment fact and must not
    outlive it.

Commit at the end of the phase.

### Phase 4 — the daemon's voice

12. `internal/daemon/credentials.go:75-88`: `external` logs at info with a
    sentence naming what is true — the provider is signed in, the credential is
    not in a file mcremote can back up, and signing in from the phone will
    create one it can. Reuse `describeReality`'s existing wording rather than
    inventing a second phrasing.
13. Leave the `recovery_required` warning exactly as it is.

Commit at the end of the phase.

### Phase 5 — tests, each seen to fail first

Run every new assertion against a deliberately broken input before trusting it —
a `git worktree` at the previous commit, as 0133 Phase 3 did, never by dirtying
the tree.

14. `external_state_test.go`: settled `{}` LIVE + stub reporting external →
    `StateExternal`, no manifest generation lost, LIVE byte-identical. **Must
    reach `recovery_required` on the pre-Phase-3 code.**
15. Same LIVE, stub reporting **not** external → still `recovery_required`.
    This is the 0133 guarantee and must pass before and after.
16. Same LIVE, stub returning an **error** → still `recovery_required`.
17. An adapter with no `RealityReporter` at all (the existing `fakeAdapter`) →
    behaviour identical to today, which the untouched transition table already
    asserts.
18. Adoptable LIVE + a stub that fails the test if probed → adopted, probe
    never called. Pins step 9.
19. `StateExternal` + a usable LIVE appears → adopted, state returns to `idle`.
20. `Commit` in `external` → `ErrUnsupportedBackend`, and `errors.Is(err,
    ErrRecoveryRequired)` is false.
21. ~~Compatibility: a manifest file containing `"state": "external"` loads,
    and the pre-0134 switch arms treat it as idle.~~ **Corrected 2026-09-03.**
    The forward half is what is testable here and is kept: a manifest carrying
    `"state": "external"` loads in THIS binary and is re-evaluated rather than
    rejected. The backward half was verified out-of-tree against a pre-0134
    worktree and is false; it is recorded in Rollback above as an operator
    procedure instead of asserted as a fallback.

Commit at the end of the phase.

### Phase 6 — verify on the reporting host

22. `make install`, which restarts the LaunchAgent. Confirm the start logs the
    new informational line for codex and **no** operator-decision warning.
23. Confirm `~/.codex/auth.json` is still the three-byte stub afterwards —
    recovery must not have written to it.
24. Confirm the manifest reads `"state": "external"` and still holds both
    generations.
25. From the phone, open Codex in Settings: no `credential_failed` frame, and
    the entry reports the unsupported-backup state. **No sign-in performed** —
    that is the whole point.

## Verification

```bash
make pre-add-check
go test ./internal/providerauth/... ./internal/provider/codex/... \
        ./internal/provider/grok/... ./internal/daemon/... -count=1
go test ./... -count=1
make vet
make lint
```

Host checks, read-only:

```bash
grep -a "credential state" ~/Library/Logs/mcremote/mcremote.err.log | tail -10
python3 -c "import json;print(json.load(open('$HOME/.local/share/mcremote/provider-auth/codex/manifest.json'))['state'])"
ls -la ~/.codex/auth.json && cat ~/.codex/auth.json
codex login status
```

### Acceptance criteria

* Every command above passes with no findings.
* Tests 14 and 20 each fail against the previous commit, with the failure
  output recorded in this plan verbatim.
* Tests 15, 16 and 17 pass before and after, demonstrating that the 0133
  escalation and the no-capability path both survived.
* On the host: no operator-decision warning for codex on daemon start;
  manifest state `external`; `~/.codex/auth.json` unchanged at three bytes;
  no `credential_failed` on the phone; no sign-in performed.
* `git diff --stat` touches only the files named in Scope.

## Rollout and Rollback

**Rollout.** Daemon-side only, effective on the next restart. No phone update,
no release, no protocol change, no user action. A host whose credential file is
usable never reaches the new code path and never spawns the probe.

**Rollback.** Revert the phase commits, **then reset the provider's manifest
directory if it recorded `external`.**

~~step 21 exists to prove a pre-0134 binary reads that as idle, which is the
correct fallback~~ — **corrected 2026-09-03, verified against a pre-0134
worktree.** A pre-0134 binary rejects the manifest outright
(`unknown manifest state "external"`), because `loadManifest` validates the
state, and every coordinator call for that provider then returns that error.
The procedure is:

```bash
# after reverting, ONLY if the manifest recorded the new state
python3 -c "import json;print(json.load(open('$HOME/.local/share/mcremote/provider-auth/codex/manifest.json'))['state'])"
rm -rf ~/.local/share/mcremote/provider-auth/codex   # generations are re-seeded from LIVE
```

This is safe — the generations are copies, and the next start re-seeds `CURRENT`
from LIVE — but it is a manual step, and it is not unique to 0134:
`DisallowUnknownFields` means MADR 0133's `operator_choice` field already made
the manifest unreadable to a pre-0133 binary. The format does not support
downgrade by design.

No credential, generation or LIVE file is altered by this work in either
direction.

**What this does not fix, stated plainly.** mcremote stops mismanaging a state
it cannot control. It does **not** gain the ability to protect a credential
Codex keeps elsewhere, so the phone still cannot back up or restore Codex auth
on such a host, and `RecoveryAvailable` stays false there. Why codex-cli
0.152.1 moved the credential out of `auth.json` is unanswered and worth
pursuing separately; if it turns out to be configurable, restoring the file
backend would make this state rare rather than routine.

## Execution Record

### Phases 1-5 — 2026-09-03, complete

**Phase 1.** `RealityReporter` added to `adapter.go` as an optional capability
returning a bool, not the codex `StoreReality` enum — `providerauth` must not
import a provider to read it. `codex.CredentialAdapter.CredentialIsExternal`
implements it over `ObserveCredentialStoreCached`, answering true only for
`RealityExternal`; `RealityUnsupported`, `RealityUnknown` and
`RealityLoggedOut` all answer false, because none of them establishes that a
usable credential exists anywhere. A compile-time assertion pins the
implementation. Nothing called it yet and the suite passed unchanged.

**Phase 2.** `StateExternal` added, plus the explicit branch at every site the
audit found rather than letting it fall into a `default:`: `reconcileLocked`
adopts (that is how the state is left, and it deliberately does not probe —
that would put a CLI spawn on the watcher's per-event path); `statusLocked`
projects `BackupUnsupported` with `RecoveryAvailable` false; `Begin` permits a
login but skips seeding from an unusable LIVE; `Abort` preserves the state the
way it preserves `recovery_required`; `Commit`, `MarkRevoked` and `RecordLogout`
refuse with `ErrUnsupportedBackend`.

**Phase 3.** The probe sits immediately before the escalation, so an adoptable
LIVE — or one matching CURRENT — returns without spawning anything. A missing
capability, or a probe error, both fall through to the pre-0134 escalation.

**Phase 4.** `external` logs at info and reuses the wording `describeReality`
already had. The `recovery_required` warning is unchanged.

**Phase 5 — instruments, seen to discriminate.** Running the new tests against
the plain pre-0134 commit only proved the symbol was new (`undefined:
StateExternal`), which establishes nothing about behaviour. A scratch worktree
was built instead with Phase 2 and 4 present and **two mechanisms deliberately
removed** — Phase 3's classification and Phase 2's Commit guard — each removal
asserted to have landed before the run:

```text
--- FAIL: …/signed_in_elsewhere_is_external,_not_recovery_required
    external_state_test.go:79: state = recovery_required, want external
--- FAIL: …/external_is_left_automatically_when_the_file_becomes_usable
    external_state_test.go:176: setup: state = recovery_required err = <nil>
--- FAIL: TestCommitInExternalIsUnsupportedNotAmbiguous
    external_state_test.go:213: commit err = <nil>, want ErrUnsupportedBackend
--- PASS: …/not_signed_in_elsewhere_still_escalates
--- PASS: …/a_probe_error_still_escalates
--- PASS: …/an_adoptable_live_is_never_probed
--- PASS: TestExternalStateManifestIsTolerated
```

The three PASSes matter as much as the FAILs: they are the guarantees that had
to survive — MADR 0133's escalation for a genuinely broken credential, the
refusal to trust an unreachable CLI, and the promise that a healthy host never
pays for a probe. Note the Commit line: without the guard, publishing into a
file the provider is not reading **succeeds silently**.

**Verification.**

```text
go test ./internal/... -count=1   -> 42 packages ok, 0 FAIL
make pre-add-check                -> 713 file(s) clean (gofmt, golint, govulncheck)
```

### Deviations

**2026-09-03 — the MADR's rollback consequence was false; owner chose to keep
the state and correct the docs.**

*Evidence.* The MADR said an older binary "reading `external` takes the idle
path … but this must be verified rather than assumed". Verified against a
pre-0134 worktree, and it does not:

```text
state=external (0134)  -> REJECTED: provider auth: unknown manifest state "external"
baseline idle          -> ACCEPTED: state="idle"
```

`loadManifest` validates the state, so the manifest fails to load and every
coordinator call for that provider returns the error. `DisallowUnknownFields`
(`manifest.go:274`) makes the same true of any added field, so MADR 0133's
`operator_choice` had already made the manifest unreadable to a pre-0133 binary
— a flaw in 0133's own Rollback section, corrected at the same time.

*Resolutions offered.* (a) keep the state and correct the docs, treating
downgrade as the operator procedure it already was; (b) drop the durable state
and report `external` as a non-persisted result; (c) make `loadManifest`
forward-tolerant first, under its own MADR. **Owner chose (a).**

*Consequence of doing nothing:* a rollback past either change would leave the
daemon logging `credential recovery failed` for that provider with no
documented way out.

*Docs amended before continuing:* the MADR's consequence and both plans'
Rollback sections carry struck-through originals with dated corrections, and
step 21 now claims only the forward half, which is what a test can pin.

### Phase 6 — 2026-09-03, host criteria met; one step needs the owner

**The first install did not work, and found a wiring defect the unit tests
could not see.** After `0.16.1.g12bea30`, the daemon still warned:

```text
2026-09-03T07:15:53  WARN provider=codex  credential state needs an operator decision …
```

Cause: `newCredentialGuard` built the adapter as
`codex.NewCredentialAdapter("codex")` — **with no binary**. The reality probe
runs the provider's CLI, and `ObserveCredentialStore` returns `RealityUnknown`
when `bin == ""` deliberately ("without the CLI the two states are
indistinguishable, and guessing is what caused the lockout"). So
`CredentialIsExternal` answered false for every real host and every one fell
back to the pre-0134 escalation. Nothing in `providerauth` could catch it: its
tests supply their own adapter.

Fixed by threading `cfg.Providers.Codex.Bin` through `newCredentialGuard`, with
the parameter documented as required rather than optional.

`TestCredentialGuardPassesTheCodexBinary` now pins it end-to-end — a stub CLI
exiting zero, `{}` in the credential file, recovery must reach `external`. It
failed twice before passing, and the second failure was informative: with the
binary wired but no `CURRENT` seeded it returned `idle`, because an unmanaged
provider takes the seeding branch and never reaches the classification. That is
harmless — no warning, and `backupProjection` still reports unsupported — but it
is not the reporting host's state, so the test now seeds a real credential first
and only then swaps in the stub, mirroring the host exactly.

**Host results after the fix.**

```text
step 22  2026-09-03T07:19:48  INFO provider=codex  "provider is signed in but keeps its
                              credential outside the file mcremote can back up; signing in
                              from here will create one it can"
                              — and NO operator-decision warning
step 23  ~/.codex/auth.json   3 bytes, `{}`, sha256 ca3d163b… — byte-identical to the
                              value recorded before this work. mtime 07:19:02 is 46s
                              BEFORE the daemon started at 07:19:48, so codex touched it,
                              not recovery.
step 24  manifest             state: external; both generations retained
                              (previous/refresh 2026-08-23, current/device_auth 2026-09-03)
step 25  phone                NOT YET VERIFIED — needs the owner to open Codex in Settings
```

**Verification.**

```text
go test ./internal/... -count=1   -> 42 packages ok, 0 FAIL
make pre-add-check                -> 714 file(s) clean (gofmt, golint, govulncheck)
```

### Deviations (continued)

**2026-09-03 — `internal/daemon/daemon.go` added to scope.** Threading the
configured binary needed the call site as well as `credentials.go`, which was
the only daemon file the plan named. One line; no other production file was
added.

### Outstanding

* Step 25: the owner opens Codex in Settings on the phone and confirms no
  `credential_failed`, with no sign-in performed. **Note the amendment below
  before reading a pass here as success.**

### Deviations (continued)

**2026-09-03 — the upstream question was answered, and it invalidates this
plan's Phase 6 result.** Investigated at the owner's request via `codex doctor`,
the official documentation, and controlled probes; recorded in full as an
amendment to the MADR.

Codex did **not** move the credential anywhere. `auth storage mode` is `File`,
so `auth.json` is the store Codex intends to use, and the stored credential is
simply incomplete — `stored ChatGPT tokens: false`, `stored auth issue: ChatGPT
auth is missing refresh metadata`, and doctor's websocket check returning
`http 401 Unauthorized`. `OPENAI_API_KEY` is set in the owner's shell and masks
this interactively; the daemon's LaunchAgent does not have it.

The probe this plan wired up cannot tell that apart from a genuinely
unprotectable credential. `codex login status` exits **0** whether a home holds
a real credential, `{}`, or nothing at all — verified in isolated homes — so
`cliIsAuthenticated` is true unconditionally and `RealityExternal` is returned
for any unusable `auth.json`.

**So Phase 6's host result was a false positive.** The daemon stopped warning,
which this plan recorded as success, but it stopped warning about a credential
that really is broken. Phases 1-5 are sound as written and the code is correct
for the state it models; what is wrong is the evidence used to enter that state.

*Consequence of leaving it:* a genuinely corrupt or signed-out Codex credential
is now silent — no warning, no `credential_failed`, and `BackupUnsupported` on
the phone — which is the under-refusing failure the MADR's own drivers warned
about.

*Resolution, not applied here:* replace the exit-code probe with the
machine-readable verdict `codex doctor --json` exposes at
`checks["auth.credentials"]`, which distinguishes storage backend, stored-token
presence, and environment-provided auth. That changes the `RealityReporter`
contract and takes a dependency on another tool's JSON schema, so it is a new
decision needing its own MADR and plan. **No code was changed for it.**

### Recommended next steps

1. Operator: complete a fresh `codex login` with `OPENAI_API_KEY` unset, so
   `auth.json` holds full ChatGPT tokens and refresh metadata. This repairs the
   real fault and is independent of anything in this plan.
2. Then re-check: `codex doctor` should report `auth.credentials` ok, and the
   daemon should return the provider to `idle` at the next checkpoint — the
   0133 machinery adopts a usable credential automatically.
3. Write the probe MADR and plan before touching `store_reality.go`.

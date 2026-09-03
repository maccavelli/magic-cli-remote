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
21. Compatibility: a manifest file containing `"state": "external"` loads, and
    the pre-0134 switch arms treat it as idle. Assert by loading the manifest
    and calling the paths, not by reading the string back.

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

**Rollback.** Revert the phase commits. The one durable artefact is a manifest
that may contain `"state": "external"`; step 21 exists to prove a pre-0134
binary reads that as idle, which is the correct fallback — it resumes managing
the file and, on this host, escalates to `recovery_required` again. No
credential, generation or LIVE file is altered by this work in either
direction.

**What this does not fix, stated plainly.** mcremote stops mismanaging a state
it cannot control. It does **not** gain the ability to protect a credential
Codex keeps elsewhere, so the phone still cannot back up or restore Codex auth
on such a host, and `RecoveryAvailable` stays false there. Why codex-cli
0.152.1 moved the credential out of `auth.json` is unanswered and worth
pursuing separately; if it turns out to be configurable, restoring the file
backend would make this state rare rather than routine.

## Execution Record

Not started. `status: proposed` — awaiting approval to execute.

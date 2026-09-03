---
status: accepted
date: 2026-09-02
decision-makers: maccavelli
consulted: —
informed: —
---

# An external Codex credential store is a supported state, not an ambiguity for an operator to resolve

## Context and Problem Statement

Codex ChatGPT sign-in from the phone still has to be repeated, after
[0133](0133-MADR-recovery-must-not-wedge-on-a-transient-observation.md) removed
the mechanism that made `recovery_required` permanent. 0133's Phase 6 ran on the
reporting host and failed its acceptance criterion:

```text
2026-09-02T22:32:40  WARN provider=codex  credential state needs an operator decision …
2026-09-02T22:32:40  INFO provider=grok   credential state recovered  state=idle
```

The input is neither torn, nor equal-ordered, nor older — the three transient
shapes 0133 reasoned about. It is this:

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

codex-cli **0.152.1** holds a live ChatGPT subscription session while leaving a
three-byte stub in the file mcremote protects, an hour after a sign-in that
wrote a real 3857-byte `auth.json` (manifest generation
`f348d33e`, `source: device_auth`, `2026-09-03T02:34:03Z`).

That observation is stable, well-formed JSON, and contains no auth material.
`recoverIdle` classifies it as not adoptable and escalates to
`recovery_required` — correctly, under 0133's rules, which deliberately kept
escalating a settled-but-invalid LIVE. The daemon then warns on every start,
and the phone receives `credential_failed: provider auth: credential recovery
required` for credential operations. The user's only route out through the app
is another sign-in, which writes a real `auth.json`, which Codex again replaces
with the stub. That is the loop.

### The state is already modelled — recovery is the one place that does not look

`ObserveCredentialStore` (`internal/provider/codex/store_reality.go:54`)
answers this exact question, and its `RealityExternal` documentation describes
this host precisely: "the CLI is authenticated but not from the file. The
credential exists somewhere this coordinator cannot see, so it cannot be backed
up — but a fresh login will produce one it can."

`describeReality` (`:106`) already has the operator sentence for it.
`backupProjection` (`internal/provider/codex/logout.go:233-248`) already checks
reality **first** and projects `BackupUnsupported` to the phone, explicitly
because "saying 'current' there would be a lie (MADR 0074 §15.13)".
`CheckBackend` (`internal/provider/codex/adapter.go:83-89`) already treats
external as **not** a refusal, so a sign-in is still allowed.

So three of the four consumers already agree that this is a known, named,
non-ambiguous condition. Recovery is the exception: it sees an unusable file
and escalates, because `providerauth.Adapter` has no way to ask.

### Why "ask an operator" is not merely unhelpful here, but harmful

`recovery_required` exists so a human can choose between preserved
alternatives. On this host every available choice is wrong:

* `choose live` adopts `{}` as a generation — recording a stub as the
  credential of record, and destroying the last real one by rotation.
* `choose current` or `choose previous` writes a **stale real credential** over
  the stub. For a ChatGPT session that is worse than useless: MADR 0074 F14
  records that any Codex logout revokes the refresh token server-side, so a
  retained generation may already be dead, and it is written to a file Codex is
  not reading anyway.
* `choose logged-out` tombstones and deletes, while the CLI stays signed in.

There is no choice that produces a protected credential, so asking for one
invites a destructive answer to a question with no good answer.

### What is not established

**Where codex-cli 0.152.1 actually keeps the credential is unknown.** It is not
in the login keychain under a `codex` service
(`security find-generic-password -s codex` → "The specified item could not be
found"), and `config.toml` carries no store key. `~/.codex/state_5.sqlite` is a
candidate by timing but is held open by the running process and was not
inspected. Nothing in this record depends on the answer: `RealityExternal` is
defined as "somewhere this coordinator cannot see", and the probe that
establishes it asks the CLI, not the filesystem.

**Whether this is configuration, a 0.152 default, or a migration is also
unknown.** The host ran 0.146-0.148 behaviour previously (MADR 0074 §15.13
recorded the same `{}` shape on 2026-08-21), so it is at least not new.

## Decision Drivers

* `recovery_required` must mean "mcremote cannot decide", not "mcremote cannot
  help". A state no operator choice can resolve is a misuse of it.
* No prompt whose answers are all destructive. See above.
* The projection to the phone is already right; the daemon and the ws error
  path contradict it. One condition must not have two answers — the same
  principle 0133 applied to `Recover` versus `Reconcile`.
* Sign-in must stay available. MADR 0074 §15.13's lesson is explicit: a
  credential we cannot see is a reason to tell the truth, not to block the
  sign-in that would replace it with one we can protect.
* Do not spawn a CLI on a hot path. Startup runs once; the watcher's reconcile
  runs per filesystem event.
* Protection must not weaken when the file *is* the credential. This changes
  behaviour only where a probe says otherwise.

## Considered Options

* Recovery consults the adapter's observed reality and records a distinct
  `external` state
* Treat any settled-but-unparseable LIVE as unmanaged, with no probe
* Leave recovery as it is and suppress the symptoms — the daemon warning and
  the ws error mapping
* Refuse Codex outright while the store is external

## Decision Outcome

Chosen option: "Recovery consults the adapter's observed reality and records a
distinct `external` state", because it is the only option that makes the four
consumers agree, and it does so by asking the question that already has an
implemented answer rather than by inferring one from the bytes.

Concretely:

1. `providerauth` gains an **optional** adapter capability — a
   `RealityReporter` interface the coordinator type-asserts — so
   `providerauth.Adapter` is unchanged and grok, which does not implement it,
   is unaffected.
2. A new manifest state `external` is entered when, and only when, LIVE is
   settled and unusable **and** the adapter reports the CLI authenticated from
   elsewhere. It is not a failure: no warning, no operator prompt.
3. `external` is left automatically. It is re-evaluated at every checkpoint like
   any other state, so the moment a usable credential appears in the file it is
   adopted and the provider returns to `idle`.
4. Managed credential operations attempted in `external` fail with
   `ErrUnsupportedBackend`, not `ErrRecoveryRequired` — the honest error, and
   one the phone already has a projection for.
5. Sign-in stays permitted, exactly as `CheckBackend` already permits it.

The probe runs at startup recovery only, through
`ObserveCredentialStoreCached`, whose 30-second window and existing
invalidation hook already exist for precisely this cost. Reconciliation does
**not** probe; a watcher event on an unusable file leaves the state alone as it
does today.

### Consequences

* Good, because the daemon stops asking for a decision that has no correct
  answer, and stops logging a warning on every start for a host that is working
  as well as it can.
* Good, because the phone's Codex chip and the daemon's state finally say the
  same thing — `BackupUnsupported` is already what `backupProjection` returns.
* Good, because no protocol field, no app change, and no new operator command
  are needed.
* Good, because `recovery_required` regains a single meaning, which is what
  makes it worth surfacing at all.
* Bad, because startup recovery now spawns `codex login status` on a host whose
  credential file is unusable. Bounded by `ProbeTimeout` (30 s) and reached only
  on an already-degraded path, but it is a process spawn in recovery, which had
  none.
* Bad, because a new manifest state cannot be read by an older daemon at all.
  ~~The state is a JSON string and every switch has a `default:` arm, so an
  older binary reading `external` takes the idle path — the pre-0134 behaviour,
  which is the correct fallback, but this must be verified rather than
  assumed.~~ **Amended 2026-09-03: verified, and false.** `loadManifest`
  (`manifest.go:268-283`) calls `validate()`, which rejects any state
  `State.valid()` does not list, so a pre-0134 binary fails to load the manifest
  entirely rather than falling back:

  ```text
  state=external (0134)  -> REJECTED: provider auth: unknown manifest state "external"
  baseline idle          -> ACCEPTED: state="idle"
  ```

  Every coordinator call for that provider then returns that error, and the
  daemon logs `credential recovery failed`. Recovery is to reset the provider's
  manifest directory, from which the next start re-seeds `CURRENT` from LIVE.

  This is not a hazard 0134 introduces. `loadManifest` also sets
  `DisallowUnknownFields` (`:274`), so **any** additive manifest change is
  one-way — MADR 0133's `operator_choice` field already made the manifest
  unreadable to a pre-0133 binary. The format is deliberately strict: the code
  states that a future or malformed manifest is never reconstructed and the
  operator decides. Downgrade is therefore an operator procedure, not an
  automatic fallback, and saying otherwise in this record was wrong.
* Neutral, because it does nothing about the underlying question of why Codex
  moved the credential out of `auth.json`. mcremote stops mismanaging a state it
  cannot control; it does not gain the ability to protect that credential.

### Confirmation

* A recovery test drives a settled `{}` LIVE with a stub adapter reporting the
  CLI authenticated, and asserts `external` — no `recovery_required`, no
  warning, and no mutation of LIVE.
* The same input with the adapter reporting **not** authenticated must still
  reach `recovery_required`, so the 0133 escalation is shown to survive.
* An adapter that does not implement `RealityReporter` at all must behave
  exactly as today, asserted with the existing fake adapter.
* A test asserts a managed publication in `external` returns
  `ErrUnsupportedBackend` and not `ErrRecoveryRequired`.
* A test asserts `external` is left automatically once LIVE holds a usable
  credential again.
* An older-manifest compatibility test asserts a manifest containing
  `"state": "external"` is loaded and treated as idle by the pre-0134 switch
  arms.
* On the reporting host: the daemon start logs no operator-decision warning for
  codex, and the phone's Codex entry reports the unsupported-backup state rather
  than a credential failure — with no sign-in performed.

## Pros and Cons of the Options

### Recovery consults the adapter's observed reality and records a distinct `external` state

* Good, because it asks a question that is already implemented, already cached,
  and already trusted by the phone projection.
* Good, because it distinguishes "no credential anywhere" from "a credential we
  cannot reach", which are genuinely different and today are not.
* Good, because the exit is automatic and needs no operator action.
* Neutral, because it adds a manifest state, which is a durable format change,
  albeit an additive one.
* Bad, because it puts a process spawn into startup recovery.

### Treat any settled-but-unparseable LIVE as unmanaged, with no probe

* Good, because it needs no probe, no new adapter capability, and no spawn.
* Bad, because it cannot tell this host from one whose credential file is
  genuinely corrupt, which is the distinction that matters — it would silence
  the corruption signal 0133 deliberately preserved.
* Bad, because it makes the daemon report a healthy-looking state for a host
  with no working credential at all.

### Leave recovery as it is and suppress the symptoms — the daemon warning and the ws error mapping

* Good, because it is the smallest change and stops the user-visible noise.
* Bad, because the manifest still says `recovery_required`, so reconciliation
  still skips the provider and no refresh is ever recorded.
* Bad, because it hides a state rather than modelling it, and the next reader of
  the manifest — human or code — is misled exactly as today.
* Bad, because `mcremote auth-recovery choose` would still offer its three
  destructive answers to anyone who ran it.

### Refuse Codex outright while the store is external

* Good, because it is unambiguous and cannot mismanage what it refuses.
* Bad, because it blocks the sign-in that is the documented way out, which is
  the specific mistake MADR 0074 §15.13 records and `CheckBackend` was written
  to avoid.
* Bad, because the CLI is authenticated and sessions work; refusing would break
  a working provider to satisfy a bookkeeping preference.

## More Information

* Trigger evidence and the failed Phase 6:
  [0133-PLAN-recovery-must-not-wedge-on-a-transient-observation.md](0133-PLAN-recovery-must-not-wedge-on-a-transient-observation.md),
  "Phase 6 — FAILED its acceptance criterion".
* Reality model and probe: `internal/provider/codex/store_reality.go:26-44`,
  `:54`, `:106`, `:140-155`.
* Consumers that already agree: `internal/provider/codex/logout.go:233-248`
  (`backupProjection`), `internal/provider/codex/adapter.go:83-89`
  (`CheckBackend`).
* Where recovery escalates: `internal/providerauth/recovery.go:82-138`.
* States: `internal/providerauth/manifest.go:62-72`.
* Prior records: MADR 0074 (D21-D28, F12, F14, §15.13) and MADR 0133.
* Implementation:
  [0134-PLAN-an-external-credential-store-is-not-an-ambiguity.md](0134-PLAN-an-external-credential-store-is-not-an-ambiguity.md).

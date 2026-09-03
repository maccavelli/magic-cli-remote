---
status: proposed
date: 2026-09-03
decision-makers: maccavelli
consulted: —
informed: —
---

# A schema change bumps the manifest version, and a refused manifest self-heals instead of silently disarming the provider

## Context and Problem Statement

MADR 0134's Phase 6 established, against a pre-0134 worktree, that an older
daemon cannot read a manifest a newer one wrote:

```text
state=external (0134)  -> REJECTED: provider auth: unknown manifest state "external"
baseline idle          -> ACCEPTED: state="idle"
```

`loadManifest` (`manifest.go:266-283`) refuses on two independent gates —
`dec.DisallowUnknownFields()`, and `validate()`'s `State.valid()` check — and
`loadOrInit` (`transaction.go:147-158`) propagates anything that is not
`os.ErrNotExist`. Both refusals are deliberate. The question this record
settles is not whether to refuse, but **how the refusal should be reached, and
what should happen afterwards.**

### The mechanism for this already exists, and was not used

`manifestVersion` carries an explicit contract
(`manifest.go:17-20`):

> `manifestVersion` is the only schema this binary may mutate. A newer version
> is refused rather than reinterpreted, so an older daemon cannot damage state
> it only partly understands (MADR 0074 P17 step 4).

That is exactly the situation, and it is the designed answer to it. But both
recent schema changes were made **at version 1**:

* MADR 0133 added `operator_choice` / `operator_choice_at`, so an older binary
  fails in the JSON decoder: `read manifest: json: unknown field
  "operator_choice"`.
* MADR 0134 added the state `external`, so an older binary fails in
  `validate()`: `unknown manifest state "external"`.

Neither produces the designed refusal — `unsupported manifest version 2` — and
neither is legible as "this store was written by a newer daemon". The version
gate is intact and simply was not engaged. That is the first defect, and it is
ours, not the format's.

### What a refusal actually costs today

`loadOrInit` is called under `withProviderLock` by every coordinator entry
point, so a refused manifest fails all of them. The consequences are uneven,
and the worst one is easy to miss:

* `Recover` fails; the daemon logs `credential recovery failed` once at
  startup, with no instruction — unlike `recovery_required`, which names the
  exact command to run.
* `Status` fails, so `backupProjection`
  (`internal/provider/codex/logout.go:233-248`) returns `("", false)` and the
  phone shows **no** backup state rather than a problem.
* `Begin` fails, so **sign-in from the phone also fails**. The one documented
  escape hatch — the flow MADR 0074 §15.13 and MADR 0134 both rely on, and that
  `Begin` deliberately keeps open in `recovery_required` — is closed by the very
  condition that most needs it.

Sessions themselves keep working: the coordinator is consulted for credential
management and status, not for running an agent. So the failure is quieter than
"the provider is broken" and worse than it looks — credential management is
disarmed, the phone cannot repair it, and nothing says so. An earlier
description of this in conversation as the provider becoming "unusable"
overstated the blast radius and understated the trap.

There is no automatic recovery. The manifest stays unreadable until a human
deletes or edits it, and nothing tells them that is the remedy.

### Why "just be more tolerant" is not the obvious fix

Relaxing `DisallowUnknownFields` alone would let an older daemon load the
manifest — and then **silently destroy** what it did not understand. Decoding
drops the unknown fields, and `saveManifest` re-marshals from the struct, so
the first write by the older binary erases `operator_choice` and any future
field. A newer daemon would then read a manifest that has quietly lost state it
depends on. That is strictly worse than a refusal, because it is invisible.

Tolerating an unknown **state** is a different and larger concession again:
`external`, `recovery_required` and `idle` select genuinely different
behaviour, and a binary that maps an unrecognised state onto one of them is
guessing about a credential store. That is the case the version gate exists to
prevent.

### What is not established

Whether a downgrade ever happens in practice on a real installation is
unknown. The procedure that motivated this record is a rollback documented in
MADR 0133 and 0134, not an observed incident: no host has been seen running an
older daemon against a newer manifest. The cost of the current behaviour is
therefore latent, and the urgency of this record is correspondingly lower than
0133's or 0134's.

## Decision Drivers

* A credential store must never act on durable state it does not understand.
  This is not up for revision; it is what makes the refusal correct.
* A refusal must be *legible*: it should name the reason a maintainer can act
  on, not surface as a decoder complaint about a field name.
* A refusal must be *recoverable* without hand-editing a file under
  `~/.local/share`.
* A refusal must not close the escape hatch. Sign-in produces a credential and
  reads no durable state, so it is exactly what should still work.
* Silent data loss is worse than a loud refusal — any tolerance must round-trip
  what it does not understand, or refuse.
* The cost of a mistake here is asymmetric: over-refusing disarms backup;
  under-refusing corrupts a credential store.

## Considered Options

* Bump the version on every schema change, and make a version refusal quarantine
  the manifest and re-seed
* Tolerate unknown fields and map an unknown state to `idle`
* Preserve unknown fields with a catch-all so they survive a round trip, while
  still refusing an unknown state
* Keep the refusal exactly as it is, and improve only the message and the
  documented manual procedure

## Decision Outcome

Chosen option: "Bump the version on every schema change, and make a version
refusal quarantine the manifest and re-seed", because it uses the mechanism the
format already documents, keeps the guarantee that no binary acts on state it
does not understand, and converts a permanent silent disarm into a bounded,
logged, self-healing event.

Concretely:

1. **`manifestVersion` becomes 2**, retroactively covering MADR 0133's
   `operator_choice` fields and MADR 0134's `external` state. From here, any
   change to the manifest's shape or vocabulary bumps it, and that rule is
   stated where the constant is defined so the next change cannot miss it.
2. **A version mismatch is classified, not merely returned.** A typed
   `ErrManifestFromNewerDaemon` distinguishes "written by a newer daemon" from
   "corrupt", so the two can be handled differently and logged differently.
3. **On that error only, the manifest is quarantined and the provider re-seeds.**
   The unreadable file is renamed to `manifest.json.v<N>.bak` — never deleted —
   and a fresh manifest is seeded from LIVE, exactly as a first-run host does.
   The daemon logs what happened, where the old file is, and that backup history
   was reset.
4. **Corruption is still terminal.** A manifest that is malformed for any other
   reason keeps today's behaviour: refuse, preserve, and let a human look. The
   self-heal is narrow on purpose — it applies only where the file is known to
   be a *valid manifest of a schema this binary cannot mutate*.
5. **`Begin` survives a refused manifest.** A sign-in must remain possible while
   the store is unreadable, because it is the only action that needs no durable
   state and produces the credential a re-seed would then protect.

### Consequences

* Good, because a downgrade becomes a logged, self-healing event rather than a
  silent disarm that no message explains.
* Good, because the refusal finally reads as `unsupported manifest version 2`,
  which names the actual condition, instead of a decoder error about a field.
* Good, because the escape hatch stays open: a host that lands in this state can
  still be signed in from the phone, which is what produces a protectable
  credential.
* Good, because nothing is deleted. The quarantined manifest and every
  generation payload stay on disk, so an operator who does want the old labels
  back can restore them.
* Bad, because a re-seed **resets backup history**. The retained `previous`
  generation stops being labelled, so a restore of the prior credential is no
  longer offered until a new one accumulates. On a downgrade that is a real
  loss, and it is the price of not hand-editing.
* Bad, because it adds a durable-format rule that must be honoured by every
  future change, and the two most recent changes are evidence that such rules
  get missed. Mitigated only partly by putting the rule at the constant.
* Neutral, because forward compatibility is explicitly *not* gained. A newer
  daemon still cannot be read by an older one; it is only handled better.

### Confirmation

* A test writes a manifest at version 2 and loads it with `manifestVersion`
  forced to 1, asserting `ErrManifestFromNewerDaemon` — the designed refusal,
  not a decoder error.
* A test asserts a manifest with an unknown **field** and a manifest with an
  unknown **state**, both at a newer version, produce that same typed error, so
  neither gate is reached first by accident.
* A test asserts a genuinely corrupt manifest at the *current* version is still
  refused terminally and is **not** quarantined or re-seeded.
* A test asserts quarantine renames rather than deletes, that a fresh manifest
  is seeded from LIVE, and that every generation payload still exists on disk
  afterwards.
* A test asserts `Begin` succeeds against a refused manifest, so the sign-in
  path is proven open rather than assumed.
* An end-to-end check on a real data directory: run the current daemon, then a
  binary with `manifestVersion` pinned to 1, and confirm the provider re-seeds,
  logs the quarantine, and reports a sane backup state — rather than the current
  silent disarm.

## Pros and Cons of the Options

### Bump the version on every schema change, and make a version refusal quarantine the manifest and re-seed

* Good, because it engages a mechanism the format already documents and that
  MADR 0074 P17 step 4 already decided.
* Good, because it keeps the no-guessing guarantee completely intact — nothing
  is reinterpreted, the file is set aside.
* Good, because recovery needs no operator command, while still leaving the
  evidence for one who wants it.
* Neutral, because it makes downgrade a supported-but-lossy operation rather
  than an unsupported one; that is an honest description of what it is.
* Bad, because it resets backup history, and because it relies on future
  authors remembering to bump a constant.

### Tolerate unknown fields and map an unknown state to `idle`

* Good, because it is the smallest change and makes downgrade seamless in the
  common case.
* Bad, because decoding drops unknown fields and the next `saveManifest`
  re-marshals from the struct, so the older binary silently erases state the
  newer one depends on.
* Bad, because mapping an unknown state to `idle` is a binary guessing about a
  credential store — `external` and `recovery_required` mean different things,
  and treating either as `idle` resumes managed mutation on a host that should
  not have any.

### Preserve unknown fields with a catch-all so they survive a round trip, while still refusing an unknown state

* Good, because it removes the data-loss objection to tolerance: unknown fields
  are carried through a save untouched.
* Good, because it keeps the strong refusal exactly where guessing would be
  dangerous — the state vocabulary.
* Neutral, because it makes additive *field* changes downgrade-safe while
  vocabulary changes stay breaking, which is a defensible and common split.
* Bad, because a round-tripped field is preserved but not *honoured*: an older
  daemon would carry `operator_choice` forward while ignoring what it means,
  which is a subtler version of acting on state it does not understand.
* Bad, because it leaves the actual reported problem — a refused manifest
  disarming the provider with no recovery — entirely unaddressed.

### Keep the refusal exactly as it is, and improve only the message and the documented manual procedure

* Good, because it is nearly free and removes the worst property of today's
  behaviour, which is that the message says nothing actionable.
* Good, because it takes no risk with a credential store.
* Bad, because recovery still requires deleting a file under
  `~/.local/share` by hand, which is exactly the kind of instruction that gets
  followed wrongly on the one occasion it matters.
* Bad, because it leaves the phone sign-in path closed, so a host in this state
  cannot be repaired remotely at all.

## More Information

* Refusal gates: `internal/providerauth/manifest.go:266-283` (`loadManifest`),
  `:227-264` (`validate`), `:17-20` (`manifestVersion` and its contract).
* Propagation: `internal/providerauth/transaction.go:147-158` (`loadOrInit`).
* What a refusal disarms: `internal/provider/codex/logout.go:233-248`
  (`backupProjection`), `internal/providerauth/transaction.go:282-342`
  (`Begin`).
* The schema changes made at version 1:
  [0133-MADR-recovery-must-not-wedge-on-a-transient-observation.md](0133-MADR-recovery-must-not-wedge-on-a-transient-observation.md)
  (`operator_choice`) and
  [0134-MADR-an-external-credential-store-is-not-an-ambiguity.md](0134-MADR-an-external-credential-store-is-not-an-ambiguity.md)
  (`external`), with the verification that exposed this in the latter's plan,
  "Phase 6" and its 2026-09-03 deviation.
* Prior record: MADR 0074 P17 steps 4 and 5.
* No implementation plan exists yet. Per the repository workflow, one must be
  written and approved before any code change.

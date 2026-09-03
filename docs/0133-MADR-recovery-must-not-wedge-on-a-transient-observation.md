---
status: accepted
date: 2026-09-02
decision-makers: maccavelli
consulted: —
informed: —
---

# Startup credential recovery treats an unprovable observation as a no-op, not a terminal state

## Context and Problem Statement

Codex ChatGPT sign-in has to be repeated over and over on this host. The
sign-in itself works; it does not stay working. The daemon log names the cause
on every start since 2026-08-21:

```text
2026-08-21T15:17:04  WARN provider=codex  credential state needs an operator decision; run `mcremote auth-recovery status` …
2026-08-21T15:17:04  INFO provider=grok   credential state recovered  state=idle
2026-08-21T16:42:03  WARN provider=codex  … needs an operator decision …
2026-08-21T23:49:22  WARN provider=codex  … needs an operator decision …
2026-08-25T13:43:13  WARN provider=codex  … needs an operator decision …
2026-08-25T15:04:01  WARN provider=codex  … needs an operator decision …
2026-08-29T09:05:24  WARN provider=codex  … needs an operator decision …
2026-08-31T07:42:45  WARN provider=codex  … needs an operator decision …
2026-09-02T20:10:45  WARN provider=codex  … needs an operator decision …
```

Codex on every one of those starts. Grok `idle` on every one. And what the
phone gets while that is true:

```text
2026-08-21T15:20:21  INFO ws error frame  code=credential_failed
  msg="provider auth: credential recovery required"
```

The user's only escape route through the app is to sign in again — which does
work, because `Begin` deliberately permits a login in `recovery_required`
(`transaction.go:286-298`). So the cycle is: wedge, re-authenticate, work for a
while, wedge again.

### Why it wedges, and why it stays wedged

`recoverIdle` (`recovery.go:82-138`) promotes a changed LIVE only when it is
`obs.valid && obs.meta.Fresher(c.metaOf(ctx, cur))`. Anything else falls
through to `return c.finish(m, StateRecoveryRequired)` at `recovery.go:138`.

That state is terminal by construction:

* `reconcileLocked` returns immediately for it (`reconcile.go:31`), so the
  watcher stops adopting Codex's token refreshes;
* `recoverLocked` short-circuits on it (`recovery.go:59-61`), so every later
  daemon start only re-logs the same warning without re-examining anything.

Only `ResolveRecovery` — the `mcremote auth-recovery choose` CLI — or a
successful login leaves it.

The manifest on this host is the receipt. `previous` is a `refresh` generation
created **2026-08-23T13:25:33Z**; `current` is a `device_auth` generation from
**2026-09-03T02:34:03Z**, minutes old at the time of writing. Between those two
dates Codex refreshed its OAuth token many times — `~/.codex/auth.json` is
rewritten on every refresh — and **not one** of those refreshes was recorded.
That is exactly what a terminal state predicts, and it dates the wedge to
between 2026-08-23 and 2026-08-25.

Grok is not exempt by design, only by luck: its credential file rarely changes,
so it takes the `obs.fp == cur.Fingerprint` fast path (`recovery.go:96`) and
never has to prove freshness.

### The transient inputs that trip it

Two of the three ways `recoverIdle` fails to prove freshness are not ambiguity
at all — they are a bad instant to have looked.

**A torn or transitional read.** `observeLive` (`recovery.go:37`) calls
`liveFingerprint` (`store.go:216`), which is a bare `os.Lstat` plus
`os.ReadFile`. There is no stability requirement. `Reconcile`, given the very
same job, uses `stableObservation` (`reconcile.go:78`), whose own comment reads:
"requires two identical validated reads before trusting what is on disk, so a
torn write in progress is classified unstable rather than adopted" — and on an
unstable read `reconcileLocked` changes nothing and returns nil
(`reconcile.go:45-48`).

Identical input, opposite outcomes: reconciliation shrugs and retries; recovery
escalates to a state nothing automatic can leave. A half-written `auth.json`, or
Codex's transient `{}` stub during its own login, is enough. This host has
produced that stub before — it is the 2026-08-21 lockout recorded in MADR 0074
§15.13.

**An equal timestamp.** `Fresher` (`adapter.go:51-62`) requires
`m.ExpiresAt.After(existing.ExpiresAt)` — strictly greater. Codex's
`last_refresh` carries microseconds, so ties are unlikely rather than
impossible; a rewrite that changes bytes without advancing `last_refresh` is
reported as "not fresher" and wedges. This is the same trapdoor as the first,
with a different trigger, and no evidence in this investigation shows it firing
in practice.

The third way is genuine: a LIVE that is stable, valid, same mode, and
demonstrably **older** than the recorded current. Refusing to promote that is
correct and stays correct — it is D24's "never roll a rotated token backward".

### A second, independent defect in the same subsystem

`CodexAuthLockPath` (`credstore.go:247-253`) and `GrokAuthLockPath`
(`credstore.go:197-203`) each return `<home>/auth.json.lock`.
`fsutil.WithLock` then appends its own suffix — `lockPath := path + ".lock"`
(`lock_unix.go:21`, and identically `lock_windows.go:27`) — so every
publication actually flocks **`auth.json.lock.lock`**.

The evidence is sitting in both provider homes:

```text
~/.grok/auth.json.lock         16 bytes  0644  mtime identical to auth.json
~/.codex/auth.json.lock.lock    0 bytes  0600
```

The first is grok's own lock, non-empty and updated in lockstep with its
credential. The second is ours, alone, with nothing on the other side of it.

Every correct caller in the tree passes the **base** path and lets `WithLock`
add the suffix: `store.lockPath()` returns `…/transaction` and produces
`transaction.lock` (`store.go:53`); `devices.json` produces `devices.json.lock`.
The two provider adapters are the only callers that pre-apply it.

So the property `Adapter.NativeLockPath` documents — "the sibling lock the
provider's own writer honors, so mcremote serializes against it instead of
racing it (D25)" (`adapter.go:74-76`) — does not hold for either provider. For
grok, whose writer demonstrably does honor `auth.json.lock`, MADR 0074 F12's
protection has never been in effect.

The tests cannot see it. `grok/adapter_test.go:34` asserts the string the method
returns — "lock = %q, want auth.json.lock — grok's own writer honors it" — and
nothing asserts which file is locked.

### What is not established

No `ErrConflict` appears anywhere in the daemon log, so the theoretical
race between a long browser sign-in and a concurrent refresh
(`transaction.go:441-445`) is a real window but is not shown to have fired
here. It is listed in the plan as a test to write, not as a cause.

## Decision Drivers

* A transient on-disk state must not produce a permanent one. Recovery runs at
  the least controlled moment there is — daemon start — and cannot assume it
  looked at a quiet filesystem.
* `Recover` and `Reconcile` answer the same question about the same file. Two
  answers to one question is the defect, whatever either answer is.
* `recovery_required` must keep meaning "mcremote still cannot decide". Today
  it can also mean "mcremote could not decide once, days ago".
* Never promote an older credential. D24's rule is right and this must not
  weaken it.
* Hosts already wedged must recover without a CLI command they were never told
  to run at the moment it mattered.
* A documented safety property that is not in force is worse than an absent
  one: it is relied on in review.

## Considered Options

* Give recovery the same stable read and no-op semantics as reconciliation, and
  re-evaluate an existing `recovery_required` against fresh evidence
* Adopt LIVE whenever it validates, dropping the freshness comparison at startup
* Keep the terminal state and surface a one-tap "use the credential on disk"
  action on the phone
* Widen `Fresher` alone, so equal or incomparable timestamps count as fresh

## Decision Outcome

Chosen option: "Give recovery the same stable read and no-op semantics as
reconciliation, and re-evaluate an existing `recovery_required` against fresh
evidence", because the two paths ask one question and must not answer it
differently, and because it removes the wedge without weakening the one rule
that matters — an older credential is still never promoted.

Concretely, `recoverIdle` gains three properties:

1. **Stable observation.** It reads LIVE through the same two-matching-reads
   discipline `Reconcile` uses, with the same `StableReadInterval` /
   `StableReadDeadline` bounds (100 ms / 2 s, `bounds.go:33-34`).
2. **Unprovable is a no-op, not a verdict.** An unstable, unreadable, or
   unparseable LIVE leaves the manifest exactly as it was and returns the
   current state. The next checkpoint — the watcher, a pre-mutation
   reconcile, or the next start — looks again at a settled file.
   `recovery_required` is reserved for a *stable, valid* LIVE that is
   demonstrably older or of a different mode.
3. **Equal is not older.** Adoption requires "not older" rather than "strictly
   newer", so a byte change that leaves `last_refresh` untouched is adopted
   instead of escalated. The backward direction stays forbidden.

Separately, and for the same user-visible failure: `NativeLockPath` returns the
**base** credential path for both providers, so `WithLock` produces the file the
provider's own writer honors. This is a correction, not a new decision — MADR
0074 D25/F12 already decided that mcremote serializes against the native
writer, and this makes the code do what that decision says.

### Consequences

* Good, because the re-authentication loop ends: a refresh observed at a bad
  instant costs a retry, not a sign-in.
* Good, because hosts already wedged — this one, for ten days — clear
  themselves on the next daemon start instead of needing
  `mcremote auth-recovery choose`.
* Good, because `recovery_required` regains a single meaning, which makes both
  the warning and the phone's `credential_failed` frame trustworthy.
* Good, because grok's publications begin serializing against grok's refresh
  writer, which MADR 0074 F12 claims and has never delivered.
* Bad, because a genuinely broken LIVE now takes longer to be called broken: it
  must survive the stable-read window and still be valid before recovery will
  escalate. That is the intended trade, and the escalation still happens.
* Bad, because re-evaluating an existing `recovery_required` softens "terminal
  until an operator acts". This is deliberate and narrow: re-evaluation applies
  only where **no** operator decision was ever recorded, uses the same evidence
  test as a fresh start, and still escalates when the evidence is genuinely
  ambiguous. It does not override a resolution, because none was made.
* Neutral, because the lock path change alters no manifest, no generation, and
  no on-disk layout — only which file is flocked. The stray
  `auth.json.lock.lock` files become inert and are left in place rather than
  deleted by the daemon.

### Confirmation

* A recovery test drives `recoverIdle` with a LIVE that is mid-write on first
  read and settled on the second, and asserts the manifest is untouched and the
  state is not `recovery_required`. Run against today's code first, to watch it
  wedge.
* A test asserts an equal-`last_refresh`, different-bytes LIVE is adopted, not
  escalated.
* A test asserts a stable, valid, strictly **older** LIVE still escalates, so
  the D24 guarantee is shown to survive the change.
* A lock test asserts the **file that gets flocked** — by holding
  `<home>/auth.json.lock` from the test and requiring a publication to block on
  it — rather than asserting the string `NativeLockPath` returns. The existing
  string assertions are kept and inverted to the new expected value.
* On this host, after the change: the daemon start logs
  `credential state recovered provider=codex state=idle`, and a new `refresh`
  generation appears in the manifest after Codex next rotates its token.

## Pros and Cons of the Options

### Give recovery the same stable read and no-op semantics as reconciliation, and re-evaluate an existing `recovery_required` against fresh evidence

* Good, because it makes one question have one answer, which is why the two
  paths existed diverging in the first place.
* Good, because the conservative rule that matters is untouched: a stable,
  valid, older LIVE still escalates.
* Good, because the code it adopts is already written, already bounded, and
  already carries the comment explaining why it exists.
* Neutral, because it slows the escalation of a genuinely broken credential by
  at most `StableReadDeadline`.
* Bad, because it needs the re-evaluation carve-out to unwedge existing hosts,
  which is the one place it revisits a documented terminal state.

### Adopt LIVE whenever it validates, dropping the freshness comparison at startup

* Good, because it is the smallest change and would also end the loop.
* Bad, because it discards D24. A daemon starting after an old credential was
  restored from a backup would promote it and record the rotated one as
  superseded — the exact backward roll the freshness test exists to stop.
* Bad, because it would silently make the manifest's ordering meaningless
  while leaving the code that reads it in place.

### Keep the terminal state and surface a one-tap "use the credential on disk" action on the phone

* Good, because the operator sees and decides, which suits genuine ambiguity.
* Good, because it needs no change to the recovery rules.
* Bad, because it asks the user to resolve a question mcremote created by
  looking at the file at the wrong moment. Most prompts would be spurious, and
  a prompt that is usually spurious gets tapped through unread.
* Bad, because it leaves `Recover` and `Reconcile` disagreeing, so the same
  observation still means two different things depending on which one saw it.

### Widen `Fresher` alone, so equal or incomparable timestamps count as fresh

* Good, because it is a two-line change addressing one real trapdoor.
* Bad, because it does not address the torn read, which is the stronger
  candidate for what actually wedged this host — a torn file usually fails
  `Validate` outright and never reaches `Fresher`.
* Bad, because "incomparable counts as fresh" would promote a credential with
  no ordering signal at all, which is a larger concession than the problem
  needs and contradicts `Fresher`'s stated conservatism.

## More Information

* Wedge and escalation: `internal/providerauth/recovery.go:37`, `:82-138`,
  `:59-61`; terminal reconciliation `internal/providerauth/reconcile.go:31`.
* Unstable-read handling that recovery lacks:
  `internal/providerauth/reconcile.go:45-48`, `:78-104`.
* Freshness rule: `internal/providerauth/adapter.go:51-62`; Codex metadata
  extraction `internal/provider/codex/adapter.go:111-133`.
* Lock paths: `internal/provider/credstore/credstore.go:197-203`, `:247-253`;
  suffix application `internal/fsutil/lock_unix.go:21`,
  `internal/fsutil/lock_windows.go:27`; correct convention
  `internal/providerauth/store.go:53`.
* Contract the lock fix restores: `internal/providerauth/adapter.go:74-76`
  (MADR 0074 D25), and MADR 0074 F12 for grok.
* Login permitted during `recovery_required`:
  `internal/providerauth/transaction.go:286-298`.
* Prior record for this subsystem: [0074-MADR-remote-provider-auth-from-phone.md](0074-MADR-remote-provider-auth-from-phone.md) (D21-D28, F12, F14,
  §15.13).
* Implementation:
  [0133-PLAN-recovery-must-not-wedge-on-a-transient-observation.md](0133-PLAN-recovery-must-not-wedge-on-a-transient-observation.md).

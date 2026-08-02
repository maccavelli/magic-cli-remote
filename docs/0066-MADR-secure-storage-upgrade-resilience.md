# MADR 0066: Secure-storage upgrade resilience and credential recovery

<!-- markdownlint-disable MD013 MD024 -->

- **Status**: Proposed — for review
- **Date**: 2026-08-02
- **Deciders**: Project Owner
- **Scope**: The phone app's secret persistence (`apps/mobile/lib/data/local/settings_store.dart`,
  `apps/mobile/lib/data/ws/client_identity.dart`) and the recovery UX around
  lost credentials (`connect_screen.dart`, `settings_screen.dart`). One small
  docs addition host-side; **no daemon or protocol changes**.
- **Related**: [0065-MADR-update-automation.md](0065-MADR-update-automation.md)
  (in-place updates are the trigger; its Stage 0 premise is amended here),
  [MADR-client-identity-decision.md](MADR-client-identity-decision.md) /
  ADR 0005 (client-key enrolment being the thing that breaks),
  [ops-android-signing.md](ops-android-signing.md) (the runbook that made
  in-place updates possible at all).

---

## Incident

First real in-place upgrade of the signed app (v0.6.6 → v0.6.7, upload-key
signed, no uninstall — exactly what the 0065 signing work was for). The
*install* worked. The *app data* did not survive:

- The app came up unpaired on the connect screen.
- Every action that dials or persists produced red error notices — including
  host-side `client_key_required` / `client_key_mismatch` rejections
  ("stale keys") and the local keystore-unavailable message.
- Recovery attempts in-app did not converge. The owner had to clear **all**
  app data (Android Settings → Clear storage), losing every non-secret
  preference — pinned working directories, recents, connect mode, theme —
  then pair from scratch.

The failure is not the update mechanism (0065): it is that the app's secret
store did not survive the update, and that when it dies the app has no
contained, guided recovery — the blast radius grows from "three secrets"
to "everything".

## Grounded findings

Every claim below was verified in the tree at the commit range the phone ran
(v0.6.6 → v0.6.7).

- **F1 — Storage stack.** Secrets live in `flutter_secure_storage` **10.3.1**
  with `AndroidOptions(resetOnError: true)` and otherwise default options
  (`settings_store.dart:44-46`), which on Android means the
  **`EncryptedSharedPreferences`** backend (androidx security-crypto): a
  Tink keyset file in app data, wrapped by an Android Keystore master key.
  Secrets stored there: device token, cert-pin map, client identity
  cert+key (`_kToken`, `_kPins`, `_kClientCert`, `_kClientKey`).
- **F2 — Fail-closed semantics (by design, MADR 0046).** On Android a failed
  secure **read returns null** (treated as "absent", purging any legacy
  cleartext fallback — `settings_store.dart:716-720`); a failed **write
  throws `SecureStorageUnavailable`** (`:745-748`). There is deliberately no
  cleartext fallback on mobile. Consequence: a broken store makes the app
  look *unpaired* (null token) while every persistence attempt errors.
- **F3 — Identity regenerates silently.** `ClientIdentity.loadOrCreate`
  treats an unreadable identity as absent and mints a fresh P-256 keypair
  (`client_identity.dart:68-76`), generated before any socket so pairing
  enrols it (`mcremote_client.dart:656-671`). After a storage reset the
  phone therefore presents a **new** key with the **old** device identity
  nowhere in sight.
- **F4 — Host enforcement is permanent, recovery is re-pair.** With client
  keys required, a token that validates but a key that differs from the
  enrolled fingerprint is a permanent `client_key_mismatch`
  (`internal/ws/server.go:968-979`); `pair.claim` creates a **new** device
  with whatever key the phone presents (`server.go:879`). So host-side,
  plain re-pairing always recovers — there is no state that requires wiping
  the phone. The old device row remains as an orphan (`pair list` /
  `pair revoke` / `pair prune`).
- **F5 — The update shipped no storage changes.** `git diff v0.6.6..v0.6.7`
  touches only the sessions-screen fix, CI, and docs — no pubspec, plugin,
  Gradle, or manifest change. The corruption trigger was **platform-level**
  (update-time keystore/keyset desync), not app code.
- **F6 — Backup restore is already excluded.** `allowBackup=false` plus rule
  files (AndroidManifest) — the classic restored-ciphertext-without-keys
  poisoning is not the cause here and stays excluded.
- **F7 — `resetOnError: true` makes any decrypt error a silent full wipe.**
  One undecryptable read nukes all four secrets with no user-visible record
  that it happened; the app simply behaves unpaired and the *next* errors
  (F2/F4) appear disconnected from the cause.
- **F8 — Recovery UX is scattered toasts.** The relevant error copy exists
  (`connect_screen.dart:522-545`: keystore-unavailable, `client_key_required`,
  `client_key_mismatch`) but each surfaces as an ad-hoc red notice on
  whatever action tripped it; nothing names the actual event ("this phone's
  stored credentials were reset"), and nothing offers the one action that
  fixes it (re-pair after a clean credential reset).
- **F9 — The in-app clear is over-broad anyway.** `SettingsStore.clearAll`
  — the "Clear credentials & sign out" path — also removes pinned cwds,
  recent cwds and last-cwd (`settings_store.dart:811-813`). Even perfect
  in-app recovery today costs the user their pinned paths, which are
  host-path preferences, not credentials.

## External evidence

The failure class is well documented for this exact stack:

- androidx `security-crypto` (`EncryptedSharedPreferences`) is **deprecated
  by Google** (1.1.0-alpha07+), citing reliability problems — including the
  OEM-specific keyset-corruption exceptions (`AEADBadTagException` and
  friends) that surface around app updates, restores and device restarts.
- flutter_secure_storage's issue tracker has the incident by name:
  ["secure storage is deleted on app upgrade" (#677)](https://github.com/juliansteenbakker/flutter_secure_storage/issues/677),
  [silent read/write failure after restore (#853)](https://github.com/juliansteenbakker/flutter_secure_storage/issues/853),
  [intermittent data loss (#622)](https://github.com/mogol/flutter_secure_storage/issues/622).
- v10 added an opt-in **DataStore-backed Android implementation**
  (`AndroidOptions(dataStore: true)`) with automatic migration from
  `EncryptedSharedPreferences`, aligning with Google's post-deprecation
  guidance (DataStore for persistence + Keystore-protected keys). Migration
  has its own reported edge cases
  ([v9→v10 migrator decrypt failure #1079](https://github.com/juliansteenbakker/flutter_secure_storage/issues/1079)),
  so the switch must assume migration can fail and land on the same
  recovery path as corruption.

Sources:
[flutter_secure_storage changelog](https://pub.dev/packages/flutter_secure_storage/changelog),
[EncryptedSharedPreferences deprecation issue #999](https://github.com/juliansteenbakker/flutter_secure_storage/issues/999),
[security-crypto deprecation notice #729](https://github.com/juliansteenbakker/flutter_secure_storage/issues/729),
[EncryptedSharedPreferences is deprecated — what now (Medium)](https://medium.com/@n20/encryptedsharedpreferences-is-deprecated-what-should-android-developers-use-now-7476140e8347),
[Navigating the deprecation of EncryptedSharedPreferences (ProAndroidDev)](https://proandroiddev.com/securing-the-future-navigating-the-deprecation-of-encrypted-shared-preferences-91ce3c20ae8d),
[Deep dive into flutter_secure_storage (zenn)](https://zenn.dev/koji_1009/articles/fb612faf335fe3?locale=en).

## Root cause (stated honestly)

**Class, established:** the Android `EncryptedSharedPreferences` backend
became undecryptable across the app update (its documented failure mode),
after which some combination of `resetOnError`'s silent wipe and continuing
read/write failures left the phone with no token, a regenerated client key
the host had never enrolled, and error surfaces that never named the event
or offered the fix.

**Instance, not established:** the exact platform exception on this phone is
unknown — nothing captures or surfaces it today (that gap is itself D5
below). If the incident recurs before hardening lands, `adb logcat` during
the failure window would pin it precisely.

## Decisions

### D1 — Move the Android backend to DataStore

Set `AndroidOptions(dataStore: true)` (keeping `resetOnError: true`),
migrating off the deprecated `EncryptedSharedPreferences` implementation
that owns this failure mode. The plugin migrates existing entries on first
run; if migration fails it degrades to exactly the lost-credentials state
this MADR makes recoverable (D2/D3), which is the state we were in anyway.

### D2 — Startup canary: detect a dead or reset store, once, loudly

On app start (before auto-connect), probe the secret store: read a canary
key, write it back. Outcomes:

- **Store works, canary present** → normal start.
- **Store works, canary absent but non-secret prefs show prior pairing**
  (host set, `device_id` present) → the store was wiped behind our back
  (resetOnError, reinstall, migration failure). Set a one-shot
  `credentialsLost` flag.
- **Store errors** → retry once after `deleteAll()` on the secure store
  (recover a *working* empty store from a poisoned one — the thing only a
  manual data-wipe achieves today). If it still errors, set a
  `storageBroken` flag.

`credentialsLost` renders **one** clear connect-screen banner — "This
phone's stored credentials were reset (this can happen across app
updates). Pair again with a new code; your preferences are intact." —
instead of ambient red toasts. `storageBroken` renders the existing
keystore-unavailable copy, persistently, in the same slot.

### D3 — Scoped credential reset, and stop clearing preferences

- New `SettingsStore.clearSecrets()`: token + pins + client identity +
  canary, **nothing else**. Wire the recovery affordances to it.
- `clearAll` (sign-out) stops removing pinned/recent/last cwds (F9). Path
  preferences are not credentials; sign-out keeps them. (`host`,
  `device_id`, relay route and transport stickiness remain cleared — they
  are connection identity.)

### D4 — Error surfaces point at the fix

`client_key_mismatch` / `client_key_required` / `SecureStorageUnavailable`
on the connect screen gain an action chip: **"Reset & re-pair"** →
`clearSecrets()` + clear in-memory credentials + open the pair-code flow.
The copy for `client_key_mismatch` already explains the cause; now it also
carries the one-tap recovery instead of leaving the user to invent
"clear all app data".

### D5 — Record and surface the last storage failure

`_recordSecureFailure` already captures the exception in memory; persist
the last failure (string, timestamp, operation) to *non-secret* prefs and
show it in Settings → diagnostics. Next incident report carries the exact
platform exception instead of a paraphrase.

### D6 — 0065 gate and runbook amendments

- 0065 (update automation) **gates on this MADR**: an auto-updater that can
  cost the user their pairing on any update is worse than no updater. The
  0065 plan's phone stages stay parked until D1–D4 are verified on a real
  over-the-top upgrade.
- `ops-android-signing.md` §5 gains the post-incident notes: in-place
  update is expected to preserve pairing only once 0066 lands; orphaned
  device rows after any re-enrolment are cleaned with
  `mcremote pair list` / `pair revoke` / `pair prune`.

### Rejected

- **Token-authenticated key re-enrolment on the host** (auth with valid
  token + new key rotates the enrolled fingerprint). Rejected: it reduces
  ADR 0005's key binding to token-only security — exactly what client keys
  exist to prevent. Re-pair is the sanctioned rotation and D4 makes it
  one tap.
- **Cleartext fallback on mobile.** Unchanged rejection (MADR 0046): a
  broken keystore must fail closed, never downgrade.
- **Dropping `resetOnError`.** A store that throws forever is strictly
  worse than one that resets; D2 makes the reset *visible* instead of
  silent, which was the actual harm.

## Consequences

- An app update that kills the secret store degrades to: one banner, one
  re-pair, preferences intact, orphan row pruned host-side. No data wipe,
  no lost pinned paths.
- The backend switch itself (D1) is a one-time migration risk on the next
  release; its failure mode is the now-recoverable D2 path. The release
  after that is the real proof (see verification).
- Two new pieces of persistent non-secret state (canary flag bookkeeping,
  last-failure diagnostic) — both cheap, neither sensitive.

## Verification

| # | Check | How |
|---|---|---|
| U1 | Canary logic state machine (works/absent/errors × prior-pairing) | unit tests on `SettingsStore` with a throwing/fake secure backend |
| U2 | `clearSecrets` clears exactly the four secret keys, `clearAll` no longer touches cwd prefs | unit test |
| U3 | `credentialsLost` banner renders once, taps route to pair flow | widget test |
| U4 | "Reset & re-pair" chip on `client_key_mismatch` runs `clearSecrets` and opens code entry | widget test |
| U5 | DataStore migration preserves an existing token/identity/pins | hardware: install current release, upgrade to the D1 build over the top, confirm still paired |
| U6 | Post-D1 upgrade resilience | hardware: the release *after* D1, upgraded over the top, still paired — the incident's actual repro |
| U7 | Storage-broken fallback | unit/widget test with a backend that errors even after deleteAll |

## Open questions

- Q1: Should the D2 canary also verify the *client identity* parses (not
  just that reads work), catching partial corruption? Cheap to add while
  there.
- Q2: `storageBroken` on iOS/desktop — same banner slot, or keep current
  behaviour? (The incident is Android-specific; the flags are not.)

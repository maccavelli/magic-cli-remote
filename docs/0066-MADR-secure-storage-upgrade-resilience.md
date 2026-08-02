# MADR 0066: Secure-storage upgrade resilience and credential recovery

<!-- markdownlint-disable MD013 MD024 -->

- **Status**: **Implemented 2026-08-02** (software: U1–U4 and U7–U9
  automated and green — D2 probe + banner, D3 scoped reset, D4 recovery
  chip + converged Settings tile, D5 diagnostics, D8 host log + KEY
  column, D9 fingerprint tile; D1/D7 required no code). Hardware rows
  E1–E3 tracked in
  [ops-hardware-validation.md](ops-hardware-validation.md); **E2 is the
  unblock condition for 0065's phone stages** (D6). History —
  round 1 self-correction 2026-08-02:
  F1/D1 reworked after reading the plugin's installed source — the first
  draft's "switch to DataStore" named a nonexistent option (see External
  evidence); findings F1a/F1b added. **Round 2 (scope broadening,
  2026-08-02)**: prior-art survey across sibling open-source projects
  added; findings F10–F14; decisions D7–D9 added; D2/D4 refined.
  **D7 decided 2026-08-02: Option A** (Keystore-with-recovery; Q3
  closed). The companion PLAN incorporates D8/D9 and the D4 round-2
  amendments as of the same date.
- **Date**: 2026-08-02
- **Deciders**: Project Owner
- **Scope**: The phone app's secret persistence (`apps/mobile/lib/data/local/settings_store.dart`,
  `apps/mobile/lib/data/ws/client_identity.dart`) and the recovery UX around
  lost credentials (`connect_screen.dart`, `settings_screen.dart`). Docs
  host-side, plus — added in round 2 as D8 — one auth-failure log line and
  one `pair list` column in the daemon/CLI. **No protocol changes.**
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

- **F1 — Storage stack (corrected on review round 1).** Secrets live in
  `flutter_secure_storage` **10.3.1** with
  `AndroidOptions(resetOnError: true)` and otherwise default options
  (`settings_store.dart:44-46`). Reading the plugin source (pub cache,
  `android/src/main/java/.../FlutterSecureStorage.java` and
  `lib/options/android_options.dart`) corrects the first draft: v10's
  Android backend is the **plugin's own cipher implementation** — values
  AES-GCM-encrypted into plain SharedPreferences, the AES key wrapped by an
  **RSA-OAEP key in Android Keystore**. The deprecated androidx
  `EncryptedSharedPreferences` is only a vendored *migration source* for
  data written by plugin ≤9.x — not this app's path (the phone's store was
  created fresh by v0.6.6 on 10.3.1). Secrets stored: device token,
  cert-pin map, client identity cert+key (`_kToken`, `_kPins`,
  `_kClientCert`, `_kClientKey`).
- **F1a — `resetOnError` makes corruption *silent*, per key.** In 10.3.1 a
  failed read/write with `resetOnError: true` **deletes that key and
  retries**, so Dart sees `null` / success, never an exception
  (`FlutterSecureStorage.java:58-67`, `:110-120`, `handleStorageError`
  `:1308`). Cipher-mismatch failures at init self-heal by wiping data
  (`handleKeyMismatch`, `:911`); only *other* init failures (e.g. a
  Keystore that will not open the wrapping key at all) leave the store
  throwing on every operation. Consequence: the most likely corruption
  outcomes are "secrets silently vanished" (what F2's null-token unpaired
  state looks like) and, rarer, "every operation errors" (what F2's
  `SecureStorageUnavailable` writes look like). Both were observed in the
  incident.
- **F1b — How the old token met a new key.** The 0064 Settings token editor
  (`settings_screen.dart:310`, `store.setToken`) lets a user re-enter a
  long-lived token by hand. After a silent wipe regenerated the client key
  (F3), re-entering the previously working token is the natural move — and
  produces exactly `client_key_mismatch` (valid token, unenrolled key), the
  "stale keys" error observed.
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

The failure class is well documented for this stack:

- Android-Keystore-backed storage losing or refusing its keys around app
  updates, restores and restarts is a long-standing, OEM-flavoured problem.
  androidx `security-crypto` was **deprecated by Google** over exactly this
  reliability record; flutter_secure_storage v10 replaced it with its own
  implementation (F1) but the Keystore-wrapped-key architecture — and so
  the failure class — is the same.
- flutter_secure_storage's issue tracker has the incident by name:
  ["secure storage is deleted on app upgrade" (#677)](https://github.com/juliansteenbakker/flutter_secure_storage/issues/677),
  [silent read/write failure after restore (#853)](https://github.com/juliansteenbakker/flutter_secure_storage/issues/853),
  [intermittent data loss (#622)](https://github.com/mogol/flutter_secure_storage/issues/622),
  [v9→v10 migrator decrypt failure (#1079)](https://github.com/juliansteenbakker/flutter_secure_storage/issues/1079).
- **Correction from the first draft:** no published flutter_secure_storage
  version offers a DataStore-backed Android implementation — verified
  against the 10.3.1 source in the pub cache (no such `AndroidOptions`
  parameter exists) and [pub.dev](https://pub.dev/packages/flutter_secure_storage)
  (latest stable 10.3.1; prerelease 11.0.0-beta.1, also without one). The
  first draft's D1 ("switch to DataStore") named a nonexistent option,
  from a search summary that conflated a fork; there is **no backend to
  switch to**, and the hardening must be app-level.

Sources:
[flutter_secure_storage changelog](https://pub.dev/packages/flutter_secure_storage/changelog),
[EncryptedSharedPreferences deprecation issue #999](https://github.com/juliansteenbakker/flutter_secure_storage/issues/999),
[security-crypto deprecation notice #729](https://github.com/juliansteenbakker/flutter_secure_storage/issues/729),
[EncryptedSharedPreferences is deprecated — what now (Medium)](https://medium.com/@n20/encryptedsharedpreferences-is-deprecated-what-should-android-developers-use-now-7476140e8347),
[Navigating the deprecation of EncryptedSharedPreferences (ProAndroidDev)](https://proandroiddev.com/securing-the-future-navigating-the-deprecation-of-encrypted-shared-preferences-91ce3c20ae8d),
[Deep dive into flutter_secure_storage (zenn)](https://zenn.dev/koji_1009/articles/fb612faf335fe3?locale=en).

## Prior art survey (round 2)

How comparable open-source projects hold device credentials and what
happens to them when storage or keys die. Two questions per project:
*where do the secrets live*, and *what does the user see when they are
gone*.

| Project | Secrets & placement | Failure / recovery experience | Lesson for 0066 |
|---|---|---|---|
| [Home Assistant companion (Android)](https://community.home-assistant.io/t/app-logout-on-every-phone-reboot-or-app-upgrade/434087) | Server URL + OAuth tokens, Keystore-backed | Recurring "logged out on reboot/upgrade" reports; [official remedy is "clear app data and re-onboard"](https://companion.home-assistant.io/docs/troubleshooting/faqs/) — the exact dead-end this MADR removes | The closest architectural sibling ships the failure mode we refuse to; validates the whole MADR |
| [Tailscale](https://tailscale.com/docs/features/access-control/key-expiry) | Node identity key in a **plain state file** (`tailscaled.state`), app-private / root-owned — not hardware keystore | Identity survives updates and reinstalls; the pain users report is [key-*expiry* re-auth UX](https://nelsonslog.wordpress.com/2023/03/12/tailscale-key-expiry/), incl. [a reauthenticate button that silently fails](https://github.com/tailscale/tailscale/issues/6786) | Mesh-identity-in-sandbox-files is accepted industry practice (→ D7 option B); recovery buttons must visibly work (→ D4 wiring is testable, not decorative) |
| [Syncthing](https://forum.syncthing.net/t/my-syncthing-device-id-changed-folders-lost/6451) | Device ID **is** a cert keypair in plain config files | Losing the files = new device ID = re-pair with every peer; community practice is backing up the cert files to restore identity | Same equation as ADR 0005 (key = device identity); a lost key is a *new device*, so recovery must be re-enrolment, fast (→ D4); identity export considered and rejected below |
| [Signal (Android)](https://signal.org/blog/safety-number-updates/) | Identity key in app storage; reinstall = new key, **expected** | Key change is a *first-class, named event* to every peer ("safety number changed") with inline re-verify; transfer/linked-device flows preserve keys | A changed key deserves a named event, not an opaque permanent error — on the host too (→ D8) |
| [WireGuard (Android, official)](https://blog.oxplot.com/wireguard-vpn-on-android/) | Tunnel configs incl. private keys in app-private storage (plaintext; [hardened forks](https://xdaforums.com/t/app-wireguard-android-hardened-privacy-focused-hardened-fork-of-official-wireguard-for-android.4784795/) add Keystore) | No corruption class at all; criticism is at-rest strength, not reliability | The reliability/at-rest trade is real and shipped both ways by serious projects (→ D7) |
| [K-9 Mail / Thunderbird Android](https://github.com/thunderbird/thunderbird-android/issues/10013) | Account passwords in app-private storage, sandbox as the boundary (unmaskable in settings) | No keystore loss class; long-standing accepted threat model in a mainstream client | Same (→ D7) |
| [Bitwarden (Android)](https://github.com/bitwarden/android/issues/4659) | Vault key biometric-bound in Keystore | Recurring invalidation (`BadPaddingException`, [crashes](https://github.com/bitwarden/android/issues/4726)); the *designed* path is ["Biometrics failed — log in with master password, then re-enable"](https://community.bitwarden.com/t/biometrics-failed-on-android-app/88794) | Always keep a recovery credential the fragile key can't take down — ours is a fresh pair code, one tap away (→ D2/D4); and never crash on storage errors (F2's fail-closed already conforms) |
| [expo-secure-store](https://github.com/expo/expo/issues/22312) | Keystore-wrapped prefs (same shape as F1) | Unhandled `KeyPermanentlyInvalidatedException` class; ecosystem remedy: catch → delete key → regenerate | Validates the D2 self-heal shape; [Samsung dev forums confirm the class hits non-auth-bound keys after OS/security updates](https://forum.developer.samsung.com/t/unrecoverablekeyexception-after-software-or-security-updates/5917) — our keys are non-auth-bound, matching the incident |
| [Nextcloud (Android)](https://help.nextcloud.com/t/android-app-weird-login-token-is-invalid-or-has-expired-error-since-3-30/209568) | App tokens, Keystore-backed | Years of recurring token-invalidation reports with poor self-diagnosis | Another sibling with the same pain and no named event — reinforces D2/D5 |
| [MSAL / Microsoft identity guidance](https://docs.azure.cn/en-us/entra/architecture/resilience-client-app) | Token caches, platform secure storage | Canonical enterprise pattern: silent path → typed "interaction required" → **one** guided interactive recovery; broker failure falls back to local cache | Our probe → banner → one-tap re-pair is this pattern; the client already conforms on the permanent-error side (`_failHandshake` stops the blind loop, `mcremote_client.dart:1806-1824`) |
| [Material Design banners](https://m2.material.io/components/banners) | — | Persistent, non-modal, top-of-screen, action-carrying surface for system/data problems; [snackbars explicitly not for persistent errors](https://m3.material.io/components/snackbar/guidelines) | D2's banner is the per-guidance surface; the incident's ambient toasts were the anti-pattern |

### Round-2 findings (codebase-grounded gaps the survey exposed)

- **F10 — The host is blind to the incident.** `client_key_mismatch` /
  `client_key_required` are returned to the peer but **never logged** —
  `writeAuthError` logs only the unmapped default branch
  (`internal/ws/server.go:1661-1673`). A wedged phone can hammer auth for
  days and leave no host-side trace; `pair list` shows only
  ID/NAME/CREATED/LAST_USED (`internal/cli/pair.go` list command), no key
  fingerprint and no failure info. During the incident the owner had no
  way to see *which* side disagreed about *what*.
- **F11 — Orphan cleanup already exists and covers this.**
  `pair prune --stale <dur>` and `--keyless`
  (`internal/cli/pair.go:234`) remove exactly the rows a re-enrolment
  strands; D6's runbook text can lean on it as-is. No daemon change
  needed for hygiene.
- **F12 — A partial recovery affordance already ships, buried.** Settings
  → Host → **"Re-pair this host"** clears pin + client identity and keeps
  host + token (`settings_screen.dart:978-988`, `_repairHost :464`).
  It is three taps deep, its copy targets certificate rotation (not key
  mismatch), nothing routes to it from any error, and it does not clear
  the token (correct for its purpose; for a mismatch the stale token is
  half the problem). D4's chip and this tile must converge on the same
  `clearSecrets()` primitive rather than shipping two half-resets.
- **F13 — The two fingerprints exist but neither side shows one.** The
  phone can compute the exact SPKI fingerprint the daemon enrols
  (`spkiFingerprintOfKeyPem`, `client_identity.dart:86-94`) but the
  Settings tile prints only "present/absent" (`settings_screen.dart:976`);
  the host stores `ClientKeyFP` (`internal/auth/store.go:87`) but
  `pair list` doesn't print it. A mismatch is therefore undiagnosable by
  inspection, though both halves of the comparison are already on disk.
  The cert-pin tile directly above already shows the pattern to copy —
  fingerprint subtitle with tap-to-copy (`settings_screen.dart:960-971`).
- **F14 — iOS has the mirror-image failure mode, dormant.** Keychain
  entries survive app uninstall→reinstall by default (flutter_secure_
  storage iOS defaults; no `iOptions` set in `settings_store.dart:42-46`),
  so where Android loses live credentials, a future iOS build would
  *resurrect stale ones* onto a fresh install. Not acted on now (Android
  is the shipped target); recorded so an iOS bring-up reads it.

## Root cause (stated honestly)

**Class, established:** across the app update, the Android-Keystore-wrapped
store became unable to decrypt (the stack's documented failure mode), after
which `resetOnError`'s **silent per-key delete-and-retry** (F1a) discarded
secrets without any signal, the app regenerated its client key (F3), the
re-entered token met that unenrolled key as `client_key_mismatch` (F1b/F4),
and continuing storage failures surfaced as `SecureStorageUnavailable` — a
set of error surfaces that never named the event or offered the fix.

**Instance, not established:** the exact platform exception on this phone is
unknown — nothing captures or surfaces it today (that gap is itself D5
below). If the incident recurs before hardening lands, `adb logcat` during
the failure window would pin it precisely.

## Decisions

### D1 — Keep the 10.3.1 backend; the hardening is app-level (revised)

The first draft proposed switching the Android backend to DataStore; no
such backend exists in any published plugin version (see External
evidence). Revised decision: **stay on flutter_secure_storage 10.3.1 with
current options** — it is already the plugin's post-deprecation
implementation — and do not adopt the 11.0.0-beta.1 prerelease (no fix
relevant here, prerelease risk for a credential store). Since silent
per-key loss (F1a) cannot be prevented below the app, D2–D5 carry the
hardening. Revisit the plugin major on its stable release.

### D2 — Startup canary: detect a dead or reset store, once, loudly

On app start (before auto-connect), probe the secret store: read a canary
key, write it back. Outcomes:

- **Store works, canary present** → normal start.
- **Store works, canary absent but a non-secret marker says credentials
  were saved** (`expect_credentials` in SharedPreferences, maintained by
  `setToken`/deliberate clears) → the store was wiped behind our back
  (F1a's silent per-key reset, or a reinstall). Set a `credentialsLost`
  flag. The marker, not host presence, is the guard: a typed-but-never-
  paired host must not trigger it.
- **Store errors** (the rarer every-op-fails mode, F1a) → attempt
  `deleteAll()` once and retry — best-effort: with a Keystore that will
  not open at all, the delete can fail too. If still erroring, set a
  `storageBroken` flag.

`credentialsLost` renders **one** clear connect-screen banner — "This
phone's stored credentials were reset (this can happen across app
updates). Pair again with a new code; your preferences are intact." —
instead of ambient red toasts. `storageBroken` renders the existing
keystore-unavailable copy, persistently, in the same slot. (Round 2:
this is the [Material banner](https://m2.material.io/components/banners)
surface exactly — persistent, non-modal, top-of-screen, action-carrying;
snackbars/toasts are [explicitly wrong](https://m3.material.io/components/snackbar/guidelines)
for a persistent data problem, which is what the incident's ambient
toasts were.)

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

Round-2 amendments (F12, Tailscale lesson):

- The existing Settings "Re-pair this host" tile converges on the same
  `clearSecrets()` primitive (today it clears pin + identity but leaves a
  token that is dead weight in the mismatch case), and its copy widens
  from "host certificate changed" to include "or this phone's key no
  longer matches".
- The recovery action is covered by a widget test that asserts the tap
  *actually lands in the pairing flow* — Tailscale's silently-dead
  reauthenticate button is the cautionary example of an untested
  recovery affordance.

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

### D7 — Where secrets live: Keystore-with-recovery vs sandbox files (**decided: Option A**, 2026-08-02)

> **Recurrence log** (the evidence trail this decision asked for):
>
> - **#1, 2026-08-02** — the founding incident: v0.6.6→v0.6.7 in-place
>   update; store wiped (token + identity), full app-data wipe was the
>   only recovery.
> - **#2, 2026-08-02, same device, same day** — identity PEMs silently
>   deleted **with no update and no user action**; token survived; the
>   live session kept working on the in-memory identity; Settings showed
>   the identity as absent, persistent across reloads. Observed on
>   v0.6.7, i.e. before the 0066 build ever ran — this is baseline
>   platform behaviour on this device, not a 0066 side effect. It also
>   answered Q1 (the probe now checks the identity, not just the token)
>   and demonstrated the D5 limitation: per-key deletions are swallowed
>   natively, so no failure record exists for it.
>
> - **#3, 2026-08-02, same device, first run of the 0066 build** — *not*
>   a platform failure: a 0066 implementation bug. The D4 reset flow
>   cleared stored secrets but `clearMemoryCredentials` left the client's
>   cached `_identityFuture` alive, so the re-pair that followed (in the
>   same process) enrolled a keypair whose persisted copy the reset had
>   just deleted — a RAM-only identity. Symptom: re-paired and connected,
>   Settings shows the identity absent; the next launch would have been
>   `credentialsLost` again. Fixed same day: `clearMemoryCredentials` now
>   drops the identity cache, with a regression test walking the exact
>   sequence (`client_identity_test.dart`).
>
> Two *platform* occurrences in one day on one device is not yet grounds
> to flip to Option B — but a third, especially on the post-0066 build
> where the banner quantifies the cost, should trigger that conversation.
> (#3 does not count toward that threshold; it was ours.)

The survey splits cleanly into two shipped philosophies:

- **Option A — Keystore-backed store + visible recovery** (status quo +
  D2–D5; Home Assistant/Nextcloud/Bitwarden's placement, with the
  recovery UX they lack). Strongest at-rest story: key material wrapped
  by hardware-backed Keystore. Accepts that OEM keystore flakiness makes
  silent loss *possible forever* and invests in making it a
  one-banner/one-tap event.
- **Option B — App-sandbox file storage for the mesh identity and token**
  (Tailscale/WireGuard/Syncthing/K-9's placement). **Eliminates the
  incident class** — nothing depends on Keystore surviving an update. The
  at-rest boundary becomes the app sandbox + device encryption; per the
  [Android Keystore docs](https://developer.android.com/privacy-and-security/keystore)
  the *practical* delta is key extractability under device compromise —
  a compromised device can *use* Keystore keys either way. For a
  self-hosted personal tool whose token is revocable in one command
  (`pair revoke`), that delta is small; `allowBackup=false` (F6) already
  keeps sandbox files out of cloud backups.

**Recommendation: A now, B as the documented fallback** if D5 diagnostics
show storage loss recurring on this hardware. A is zero-risk to adopt
(it is the current placement), keeps the stronger at-rest claim, and
D2–D4 cap the damage of the residual failure. B is recorded here with
its precedent so a second incident is a config change with an MADR
paragraph, not a research project.

### D8 — Name the event on the host (Signal lesson, F10)

Two small daemon/CLI changes, amending this MADR's original
no-daemon-changes scope:

- `verifyClientKey` failures log one structured Warn line — device id,
  device name, enrolled FP prefix, presented FP prefix — so a wedged
  phone is visible in `mcremote` logs (today: silence,
  `internal/ws/server.go:1661-1673`).
- `pair list` gains a KEY column (short SPKI fingerprint prefix, `-` for
  keyless legacy rows), making phone-vs-host comparison possible from
  the operator's side (`internal/cli/pair.go` list command; the store
  already holds `ClientKeyFP`, `internal/auth/store.go:87`).

No protocol change; no new error codes; `pair prune` already covers the
hygiene half (F11).

### D9 — Show the fingerprint on the phone (F13)

The Settings "Client identity" tile upgrades from "present/absent" to the
SPKI fingerprint with tap-to-copy, cloning the cert-pin tile pattern
directly above it (`settings_screen.dart:960-971`). With D8's KEY column,
a mismatch becomes a 10-second visual diff instead of an article of
faith.

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
- **Identity export / credential backup (round 2; Syncthing-style cert
  backup).** Restoring identity from a user-held file would survive any
  storage death — and moves the long-lived private key outside the
  device boundary ADR 0005 exists to enforce. Re-pairing takes under a
  minute with D4; the export's risk buys almost nothing here.
- **Device-transfer flow (round 2; Signal-style key migration between
  phones).** Real UX value, unjustifiable complexity for a one-owner
  tool: `pair code` + D4 is the transfer flow.

## Consequences

- An app update that kills the secret store degrades to: one banner, one
  re-pair, preferences intact, orphan row pruned host-side. No data wipe,
  no lost pinned paths.
- Silent secret loss (F1a) remains possible below the app — the store
  cannot promise durability across updates on every OEM. What changes is
  that it becomes *visible and recoverable in one tap* instead of a
  dead-end that costs all preferences.
- Two new pieces of persistent non-secret state (`expect_credentials`
  marker, last-failure diagnostic) — both cheap, neither sensitive.

## Verification

| # | Check | How |
|---|---|---|
| U1 | Canary logic state machine (works/absent/errors × prior-pairing) | unit tests on `SettingsStore` with a throwing/fake secure backend |
| U2 | `clearSecrets` clears exactly the four secret keys, `clearAll` no longer touches cwd prefs | unit test |
| U3 | `credentialsLost` banner renders once, taps route to pair flow | widget test |
| U4 | "Reset & re-pair" chip on `client_key_mismatch` runs `clearSecrets` and opens code entry | widget test |
| U5 | Upgrade resilience, happy path | hardware: install current release, upgrade to the 0066 build over the top, confirm still paired |
| U6 | Upgrade resilience, repeated | hardware: the release *after* the 0066 build, upgraded over the top, still paired — the incident's actual repro; if the store dies again, the banner + one-tap re-pair is the accepted outcome |
| U7 | Storage-broken fallback | unit/widget test with a backend that errors even after deleteAll |
| U8 | Key-mismatch auth failures produce the structured host log line; `pair list` shows the KEY column incl. `-` for keyless rows (D8) | go test on `writeAuthError`/`verifyClientKey` logging + CLI list output |
| U9 | Client-identity tile shows the SPKI fingerprint with tap-to-copy; Settings re-pair tile routes through `clearSecrets` (D9, D4 amendment) | widget test |

## Open questions

- ~~Q1: Should the D2 canary also verify the *client identity* parses
  (not just that reads work), catching partial corruption?~~ **Answered
  2026-08-02 by recurrence #2** (see D7's log): yes — the probe now
  treats marker-with-token but **no identity** as `credentialsLost`
  (same banner, same recovery), and the connect screen skips auto-connect
  in that state, since dialling a surviving token with a freshly minted
  key can only earn `client_key_mismatch`.
- Q2: `storageBroken` on iOS/desktop — same banner slot, or keep current
  behaviour? (The incident is Android-specific; the flags are not. F14
  records the iOS-specific mirror-image mode for a future bring-up.)
- ~~Q3 (round 2, the owner decision in D7)~~ **Answered 2026-08-02:
  Option A** — Keystore stays, D2–D5 carry the hardening, Option B
  remains documented in D7 as the fallback if D5 diagnostics show
  recurrence.

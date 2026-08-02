# MADR 0065: Update automation — `mcremote update`, `mcrelay update`, phone "Restart to update"

<!-- markdownlint-disable MD013 MD024 -->

- **Status**: Proposed — feasibility assessment complete, grounded in the
  codebase and outside research; **expanded 2026-08-02 with the phone
  deep-dive (§2)**: install-warning taxonomy, Android developer
  verification (B3) and its 2026-09-30/2027 timeline, the
  confirmation-free update mechanism, prior art, and the full staged flow.
  **Open questions at the end await review.** Not implemented.
- **Date**: 2026-08-02
- **Deciders**: Project Owner
- **Scope**: A user-initiated update path for all three shipped artifacts:
  the `mcremote` daemon, the `mcrelay` relay, and the Android app. CLI
  `update` subcommands for the Go binaries (check GitHub Releases, download,
  verify, install, restart the service); a Settings entry on the phone that
  checks, downloads, verifies, and hands off to the Android installer behind
  a "Restart to update" prompt.
- **Related**: [0064](0064-MADR-connect-screen-simplification.md) (Settings
  as the home for durable device actions), `scripts/install-binary.sh`
  (the binary-swap semantics this must preserve), CI release job
  (`.github/workflows/ci.yml`).

---

## Problem

Updating today is a developer workflow, not a product one. The host runs
`git pull && make install`; the relay VPS gets a binary copied to it; the
phone gets an APK built locally and sideloaded by hand. A release pipeline
exists and is disciplined — but nothing consumes it. The request: `update`
commands that scan the repo for new releases, download, install, and restart
the daemons; and a phone flow that prompts "Restart to update" and hands the
device to the installer.

## Feasibility summary

| Surface | Verdict | Why |
|---|---|---|
| `mcremote update` | **Feasible now** | Public repo, deterministic asset names, published checksums, swap semantics already solved in `install-binary.sh`, service model already in Go (`internal/cli/service`) |
| `mcrelay update` | **Feasible now** | Same pipeline, same service machinery, own cobra tree ready for the subcommand |
| Phone "Restart to update" | **Feasible, but BLOCKED on release signing (B1), and time-boxed by Android developer verification (B3)** | Release APKs are currently signed by each CI runner's throwaway debug key; Android refuses an update whose signature differs, and the escape hatch (uninstall) wipes the keystore-backed pairing. Separately, Google begins **blocking installs from unverified developers on certified devices from 2026-09-30** (select regions; global 2027) — registration is free and solves the "unverified" warnings, but requires the stable signing key first. See §2. |

---

## 0. Grounding — what the codebase already provides

### 0.1 The release pipeline (verified against release v0.6.4)

- A `v*` tag run builds both binaries for **darwin/amd64, darwin/arm64,
  linux/amd64**, stamps them `BASE.N` (e.g. `0.6.4.1`, allocated race-safe by
  `scripts/next-build-version.sh` via `build/*` tags), smoke-runs the
  linux/amd64 pair, and publishes assets with fully deterministic names:

  ```text
  mcremote-<os>-<arch>-<BASE.N>     mcrelay-<os>-<arch>-<BASE.N>
  magic-cli-remote-<tag>-arm64.apk  SHA256SUMS-<BASE.N>
  ```

- `SHA256SUMS-<VER>` is regenerated **over the published names** and covers
  the APK too — an updater can verify every download against it.
- The repo is **public**: `GET /repos/maccavelli/magic-cli-remote/releases/latest`
  needs no token (unauthenticated rate limit 60/hr/IP — ample for an
  on-demand command).
- Windows appears in the release script's rename logic but no Windows assets
  are currently published, and `install-binary.sh` has no Windows service
  branch. **Windows is out of scope here.**

### 0.2 Version identity

- Binaries carry `main.version` via ldflags; `mcremote version` prints
  `mcremote <ver> (<commit>) <date>`. Release builds are clean `BASE.N`;
  local builds carry a uniqueness suffix (this dev host currently reports
  `0.6.4.4.gf7fe252`). **The update rule must compare the three-part BASE
  only** — `N` is a build serial, not a release ordering, and a dev suffix
  means "not a release artifact at all".
- The phone stamps `versionName` from the same version and
  `versionCode = github.run_number` — monotonic per repo, which is exactly
  what Android requires of an update. (Constraint to record: the workflow
  must never be recreated in a way that resets `run_number` without setting
  a floor.)

### 0.3 The binary swap is a solved problem — in the wrong language

`scripts/install-binary.sh` already embodies every hard-won rule the update
command needs, with a regression test (`install-binary_test.sh`) that replays
a real incident:

1. **Stage before stopping anything** — a missing download or full disk must
   never take the daemon down.
2. Rename into place, never write over the running inode (ETXTBSY).
3. Keep a `.prev` for rollback.
4. **Restore the service from a trap on every exit path**, keyed on
   "enabled", not "stopped by this run" — so a stranded install heals.
5. `wait_for_teardown` / `wait_for_up` around the launchd/systemd cycle,
   treating `SIGTERMed`/`exited` states as down.

The script is repo tooling, not shipped. **The update command must port
these semantics to Go** (an `internal/update` package shared by both
binaries), not shell out to a script it does not have.

### 0.4 Service management is already modelled in Go

`internal/cli/service` renders and installs the launchd plist
(`com.magiccliremote.mcremote` / `.mcrelay`, gui domain) and the systemd
**--user** unit for both products; `setup-service` ships in both CLIs today.
The update command reuses the same label/unit knowledge for its
stop/swap/start cycle. One open question below: relay deployments on a
server may run as a **system** unit rather than `--user`, which the current
machinery does not target.

### 0.5 The phone-side blocker, precisely (B1)

`apps/mobile/android/app/build.gradle.kts` supports release signing via an
uncommitted `key.properties`, and **falls back to the debug key when it is
absent**. The CI workflow provisions no keystore secret — the only secret it
touches is `GITHUB_TOKEN`. So every release APK is signed by that runner's
freshly generated debug keystore, which means:

- **Each release has a different signing key.** Android refuses to install
  an update whose signature does not match the installed app
  (`INSTALL_FAILED_UPDATE_INCOMPATIBLE`) — in-place update between two
  published releases is impossible today.
- The only path around a mismatch is uninstall/reinstall, which **destroys
  `flutter_secure_storage`** — device token, certificate pins, client
  identity — forcing a full re-pair. An updater that routinely costs the
  user their pairing is worse than no updater.
- Devices currently running locally-built APKs (signed by the dev machine's
  stable debug key) cannot update to a properly-signed release either: the
  first properly-signed release forces a **one-time uninstall + re-pair
  migration** on every existing install. Unavoidable; must be announced.

**Prerequisite, non-negotiable**: generate a long-lived upload keystore,
store it (and its passwords) as CI secrets, write `key.properties` in the
android-apk job, and back the keystore up offline — losing it recreates this
exact problem permanently.

---

## 1. Research — practices worth importing (sources at end)

- **Go self-update is well-trodden.** `minio/selfupdate` (checksum +
  signature verify, atomic apply, rollback) and
  `creativeprojects/go-selfupdate` (GitHub Releases discovery, OS/arch
  asset matching, version comparison) between them cover this design. The
  dependency-light path — and the one that fits, since asset naming and
  checksums are already deterministic here — is a small `internal/update`
  package implementing the same shape: **discover → match → download →
  verify → atomic apply → rollback on failure**.
- **Checksums are integrity, not authenticity.** `SHA256SUMS` fetched from
  the same origin as the binary proves the download wasn't corrupted, not
  that it came from the maintainer; TLS to GitHub is the trust anchor. The
  accepted hardening step is signing the checksums file (minisign/ed25519,
  public key embedded in the binary). **TUF** is the reference framework
  (rollback, key-compromise and mix-and-match resistance; used by PyPI and
  Sigstore) but is operationally heavy for a single-repo project —
  consciously not adopted; recorded as the known stronger alternative.
- **Android sideloaded self-update has a sanctioned shape**: apps targeting
  API 26+ must hold `REQUEST_INSTALL_PACKAGES` and use `PackageInstaller`
  (or an install intent via `FileProvider`); the user must approve a
  per-app "Install unknown apps" grant once, and the system installer
  drives the actual replacement — the app process is killed during it,
  which is precisely the "Restart to update" the request describes.
  Google Play policy **prohibits** self-update for Play-distributed apps;
  this app is sideload-only, so the policy does not bind — but if the app
  ever ships to Play, this entire flow must be removed first.
- **Updates must be user-initiated** on Android by policy and expectation;
  the design keeps *all three* surfaces user-initiated (no background
  auto-update), which also sidesteps unattended-failure rollback scenarios
  on the daemons.

---

## 2. The phone path in depth (added on review — "focus on the phone")

### 2.1 The install-time warnings, taxonomized

"The user has to override the install dialog" is actually **four separate
mechanisms**, with different causes and different fixes:

| # | Dialog | Trigger | Fix |
|---|--------|---------|-----|
| W1 | **"Install unknown apps"** settings grant | Any app that fires an install intent needs a one-time, per-source-app grant (API 26+) | By design; cannot be removed for sideloading. One-time per source. The updater flow must *explain* it before firing the intent or it reads as an error |
| W2 | **Install confirmation** ("Do you want to install/update this app?") | Every session-based or intent-based install, by default | Unavoidable on **first** install. For **updates**, eliminable on Android 12+ via `USER_ACTION_NOT_REQUIRED` (§2.3) — this is what makes the phone flow *better* than "override a scary dialog every release" |
| W3 | **Play Protect scan prompts** ("Unknown app — send for scanning?", "Unsafe app blocked", "built for an older version of Android") | Cloud-unknown APK digests from internet sideload sources; **sensitive-permission use** (RECEIVE_SMS, READ_SMS, NOTIFICATION_LISTENER, ACCESSIBILITY — the financial-fraud set); targetSdk ≥2 majors behind the device | Three mitigations, all already true or nearly free here: (a) this app requests **none** of the fraud-flag permissions (verified against the manifest: INTERNET, CAMERA, POST_NOTIFICATIONS, FOREGROUND_SERVICE*, WAKE_LOCK, RECORD_AUDIO); (b) targetSdk tracks Flutter stable (`flutter.targetSdkVersion`), so the "older Android" warning never fires; (c) a **stable signing key** lets the APK digest lineage build Play Protect reputation instead of every release being a brand-new unknown. Google also runs a developer appeals process for wrong verdicts |
| W4 | **Developer verification block** — the coming one | From **2026-09-30** (Brazil, Indonesia, Singapore, Thailand; **global 2027**), certified Android devices refuse installs from *unverified developers* outright — not a warning, a block | Register. See §2.2 — free, and the durable answer to "unverified" |

The honest summary: **W1 is permanent, W2 is removable for updates (the
whole point), W3 is avoidable through hygiene this app mostly already has,
and W4 is removable by registration — which is also the only future-proof
path, because after the rollout it stops being a warning and becomes a
wall.**

### 2.2 Developer verification (B3): the facts

From Google's published program (sources at end):

- **What it is**: identity verification (legal name, address, email, phone;
  government ID possible; D-U-N-S for organizations) plus **package-name
  registration proving ownership with the app's signing key** — the
  registration explicitly binds `com.<package>` to the key that signs it.
- **Timeline**: the Android Developer Console opened verification to all
  developers in **March 2026** (i.e. already open); enforcement starts
  **2026-09-30** in Brazil/Indonesia/Singapore/Thailand; **global in 2027**.
- **Cost/tiers**: no fee reported. A **limited-distribution tier is free**
  for hobbyists and non-commercial developers, capped at **20 devices that
  end-users explicitly authorize** — a near-exact fit for this project's
  blast radius. A full account (unlimited installs) also exists.
- **Escape hatches**: ADB installs are unaffected, and an "advanced flow"
  lets power users install unverified apps deliberately — so the dev
  workflow (`flutter install`) survives regardless.
- **The dependency that matters**: registration binds a package name to a
  **signing key** — so **B1 (stable keystore) is a prerequisite for B3**,
  not a parallel task. Registering with a throwaway key would enshrine the
  wrong key. Order is forced: keystore → re-sign migration → registration.

### 2.3 Removing the update dialog: `UPDATE_PACKAGES_WITHOUT_USER_ACTION`

Android 12 (API 31) added a sanctioned confirmation-free path for exactly
this case. Per the platform `PackageInstaller` documentation, a session may
set `setRequireUserAction(USER_ACTION_NOT_REQUIRED)` and skip the W2 dialog
when **all** hold:

- the installer declares `android.permission.UPDATE_PACKAGES_WITHOUT_USER_ACTION`
  (a normal, install-time permission) and targets API 31+;
- the session is an **update**, not a fresh install;
- the installer is the **installer of record** of the app being updated,
  **or is updating itself** — the self-update clause covers this app's case
  from the very first update, even though today's installer of record is
  adb/the system installer;
- the app being updated targets API 29+ (true here);
- the device runs Android 12+ (older devices fall back to the W2 dialog —
  degraded, not broken).

Two useful knock-on effects: after the first self-update the app **becomes
its own installer of record**, and on Android 14+ it can additionally claim
**update ownership**, which makes *other* sources trying to replace the app
show a warning instead — mild hijack protection for free.

This is the mechanism Obtainium ships today for background updates on
Android 12+, and the one tracked (unimplemented) by the main F-Droid
client; F-Droid Basic ships it. It is proven, sanctioned, and in
production in exactly this "APKs from GitHub Releases" shape.

### 2.4 Prior art — how open-source projects actually do this

| Project | Model | Lesson |
|---|---|---|
| **Signal** | Website-distributed APK ships an in-app self-updater: polls their endpoint, downloads, verifies, fires the install flow | The requested "Restart to update" UX exists in production at scale; same-key signing is what makes it a two-tap experience |
| **Obtainium** | Generic "update apps straight from GitHub Releases" installer; uses the §2.3 permission for background updates on 12+ | The exact discovery-and-install flow this MADR proposes, generalized; also a zero-code fallback for us today (point it at this repo) — except B1's key churn breaks *it* too |
| **F-Droid / F-Droid Basic** | Repo with its own signing or reproducible builds; Basic ships confirmation-free updates, main client tracks it | Distribution via F-Droid would outsource signing and updates entirely, at the cost of reproducible-build engineering; noted as alternative, not taken |
| **NewPipe** | In-app update *notification* pointing at GitHub Releases; explicitly refuses cross-source updates because of signing-key mismatch | The failure mode of inconsistent keys is well-documented community pain — B1 is not hypothetical |
| **APKUpdater** | Aggregates release feeds (GitHub among them) and drives installs | Confirms GitHub Releases as a first-class Android distribution channel |

### 2.5 The full phone flow, concretely

Stage 0 — prerequisites (ordered, each gating the next):

1. **Keystore (B1)**: generate the upload keystore; store keystore +
   passwords as CI secrets; write `key.properties` in the android-apk job
   (`build.gradle.kts` already consumes it); offline backup. Every release
   from then on shares one key.
2. **Migration**: first stable-key release requires uninstall + re-pair on
   every existing device (dev-key installs cannot update across keys).
   One-time; announce in the release notes; `mcremote pair list` +
   `pair revoke` cleans up the orphaned device rows afterwards.
3. **Verification (B3)**: register the developer account (limited tier, 20
   devices, free — or full; open question 6), verify identity, register the
   package name with the new key. Do this **well before 2026-09-30** even
   though enforcement starts region-limited — it also retires W4 and helps
   W3 reputation.

Stage 1 — the updater (ships first):

1. Settings → **App update** row (beside Version): on tap, GET
   `releases/latest`, compare tag to `PackageInfo.version` (three-part
   compare; the APK asset is versioned by tag: `magic-cli-remote-<tag>-arm64.apk`).
2. Newer → download the APK to app-private cache over TLS (plain
   `HttpClient`, ~60 MB; show progress; Wi-Fi is not required but size is
   stated), fetch `SHA256SUMS-<VER>`, verify the APK's hash.
3. Prompt **"Restart to update"** (explicit copy: the app will close and
   Android will install the update; first time also explains the W1 grant).
4. Confirm → hand the verified file to the platform installer. Two
   implementation levels:
   - **v1 (intent)**: `FileProvider` content URI + install intent +
     `REQUEST_INSTALL_PACKAGES` manifest permission. User sees the W2
     update dialog once per release. Small, no platform-channel code beyond
     the intent.
   - **v2 (session)**: a ~100-line Kotlin `MethodChannel` implementing a
     `PackageInstaller` session with `USER_ACTION_NOT_REQUIRED` (§2.3) —
     after the first v2-installed release, updates are genuinely two taps:
     "Update available" → "Restart to update", no system dialog. This is
     the end state the request describes.
5. The installer kills and replaces the process. A `MY_PACKAGE_REPLACED`
   receiver relaunches the app; reconnect then rides the existing
   cold-start auto-connect path (0062) — pairing intact because the key
   matched.

Failure modes and their handling: checksum mismatch → delete download,
error status, nothing installed; download interrupted → re-download (no
resume in v1); user declines the W2 dialog → APK stays cached, row shows
"update pending — restart to install"; version skew (release pulled) →
re-check reports accordingly.

### 2.6 What this does **not** fix

- The **first install** on a new device keeps W1 + W2 (+ W3 until
  reputation/verification land). That is inherent to sideloading; the
  20-device authorization step of the limited tier formalizes it rather
  than removing it.
- Devices below Android 12 keep the W2 dialog on every update (v1
  behaviour). Acceptable: the fleet here is modern.
- If the app is ever Play-distributed, the whole flow must be removed
  (Play policy prohibits self-update), and Play would own updates.

## 3. Decision outcome (proposed)

### D1 — One shared `internal/update` package, two thin cobra commands

`mcremote update` and `mcrelay update` share an implementation
parameterised by product name. Flow:

1. `GET /releases/latest` (unauthenticated; honour an optional
   `GITHUB_TOKEN` env for rate-limited networks).
2. Compare release BASE against the running binary's BASE (0.2 rule).
   `--check` stops here and reports.
3. Download `<product>-<GOOS>-<GOARCH>-<BASE.N>` and `SHA256SUMS-<BASE.N>`
   to a temp file **next to the destination** (same filesystem, so the
   rename is atomic).
4. Verify sha256. Failure deletes the download and aborts — the daemon has
   not been touched yet.
5. Swap + restart, porting `install-binary.sh` semantics (0.3): stage →
   stop unit → rename with `.prev` retained → start unit → `wait_for_up`.
6. Post-restart health check (`healthz` for mcremote; the relay's
   equivalent). On failure: restore `.prev`, restart again, report loudly.

Prompted by default ("update 0.6.4 → 0.6.5? [y/N]"); `--yes` for scripts.
Not a background job, not a cron — user-initiated only.

### D2 — Version rule

Update **only when remote BASE > local BASE** (three-part semver compare).
Equal BASE: report "up to date" (never chase `N`). Local dev-suffixed
version (`.g<hash>` present): refuse with an explanatory message unless
`--force` — a dev machine's build is not the updater's to replace.

### D3 — Restart via the existing service model

The unit machinery from `internal/cli/service` names what to restart
(launchd gui-domain agent on macOS, systemd `--user` unit on Linux). A
binary not installed as a service updates in place and prints "restart it
yourself" — same behaviour `install-binary.sh` has today (`svc_kind none`).

### D4 — Phone: Settings → "App update", per §2.5, gated on §2.5 Stage 0

The flow is specified in full in §2.5. Decisions it embeds:

- **Prerequisite order is forced**: keystore (B1) → one-time re-sign
  migration → developer verification (B3) → updater. Registration binds the
  package name to the signing key, so it cannot precede the keystore.
- **The acceptance bar is the v2 session installer** (§2.3:
  `PackageInstaller` + `UPDATE_PACKAGES_WITHOUT_USER_ACTION` via a small
  Kotlin `MethodChannel`): after the first session-installed release, an
  update is exactly the requested UX — "Update available" → **"Restart to
  update"** → app restarts updated, no system dialog. The v1 intent flow
  (one W2 dialog per release) is an acceptable intermediate ship, not the
  end state (open question 7).
- New manifest permissions: `REQUEST_INSTALL_PACKAGES` (v1) and
  `UPDATE_PACKAGES_WITHOUT_USER_ACTION` (v2). Both stripped wholesale if
  the app is ever Play-distributed.
- A `MY_PACKAGE_REPLACED` receiver relaunches the app after install; the
  cold-start auto-connect (0062) restores the session, pairing intact.

### D5 — Security phases

- **Phase 1 (ships with the feature)**: TLS to GitHub + sha256 from
  `SHA256SUMS-<VER>`, both hops, all three surfaces. Threat model: protects
  against corrupted/truncated downloads and mirror tampering; trusts GitHub
  and the repo's release process.
- **Phase 2 (hardening, separate decision)**: minisign keypair; CI signs
  `SHA256SUMS-<VER>` with a repo-secret private key; public key embedded in
  both binaries and the app; updaters refuse unsigned checksum files once a
  signed one has ever been seen. TUF explicitly not adopted (0-repo
  overhead budget); revisit only if distribution widens.

### D6 — Out of scope

Windows (no assets, no service branch), delta/patch updates, background or
scheduled update checks, Play Store distribution, updating the *other*
product from either CLI (`mcremote update` updates mcremote only).

---

## 4. Consequences

**Good**

- `git pull && make install` stops being the only way to track releases;
  the relay VPS becomes one SSH command (`mcrelay update --yes`).
- The pipeline's existing rigor (deterministic names, published checksums,
  smoke-tested binaries) finally has a consumer.
- The phone flow gives sideloaded installs a sanctioned upgrade path that
  today requires a laptop.

**Costs / risks**

- **B1 must land first for the phone**, and its fix carries a one-time
  uninstall + re-pair migration for every existing install — including the
  owner's. The keystore becomes a crown-jewel secret with a backup burden.
- A Go port of `install-binary.sh` is a second implementation of subtle
  semantics; its test must replay the same incident the shell test does, or
  the two will drift.
- Update-then-restart on the machine you are SSH'd into via the very daemon
  being restarted (relay case) is briefly self-severing; the command must
  finish the swap before the restart, which D1's ordering guarantees.
- An `update` command in the daemon binary is a new attack surface: it
  writes to its own install path on request. Mitigated by user-initiation,
  checksum verify, and (phase 2) signatures.

## 5. Alternatives considered

| Option | Why not |
|---|---|
| Shell out to a downloaded `install-binary.sh` | Executes freshly-downloaded shell as the install step — the exact pattern the checksum verify exists to prevent; and the script is repo tooling, not a release asset |
| Package managers (Homebrew tap / apt repo / F-Droid) | Cleaner long-term, but three new distribution channels to maintain for a single-user project; F-Droid would also solve B1, at the cost of build reproducibility work |
| Background auto-update daemonside | Unattended failure with no operator present; Android equivalent violates the user-driven rule; the request itself says "when run" |
| TUF | Right framework, wrong scale — see D5 |
| Obtainium-style external updater app for the phone | Works today with zero code, but keeps the update outside the product and doesn't fix B1's signature churn, which breaks *any* updater |

## 6. Verification plan (sketch — full plan belongs to the PLAN doc)

| # | Check | Level |
|---|---|---|
| U1 | `--check` against a stubbed releases API: newer/equal/older/dev-suffix all report correctly, nothing downloaded | unit |
| U2 | Checksum mismatch aborts before the service is touched | unit |
| U3 | Swap + restart preserves up/down state on every exit path (port of `install-binary_test.sh`, same stubbed `launchctl`/`systemctl`) | unit |
| U4 | Failed post-update health check rolls back to `.prev` and restarts | unit |
| U5 | Real update mcremote (macOS launchd) and mcrelay (Linux systemd) from release N−1 → N | hardware |
| U6 | Phone: update available → download → verify → installer prompt → app replaced, pairing intact | hardware |
| U7 | Phone: signature-matched update preserves `flutter_secure_storage` (token still valid after update) | hardware |
| U8 | Tampered APK/binary (bit-flipped) is rejected by the checksum step | unit |
| U9 | v2 session update completes with **no system dialog** on Android 12+ (§2.3), and the app is its own installer of record afterwards (`adb shell pm list packages -i`) | hardware |
| U10 | A stable-key release installs over the previous stable-key release with no Play Protect warning beyond the first-install baseline (W3 regression watch) | hardware |
| U11 | After B3 registration, install on a certified device shows no "unverified developer" surface (re-run after the 2026-09-30 and 2027 enforcement waves) | hardware |

## 7. Open questions (for review)

1. **Keystore custody (B1)**: who generates the upload keystore, where is
   the offline backup, and do we do the signing migration in the very next
   release (forcing the one-time re-pair sooner, while installs are few)?
2. **Relay unit scope**: are real relay deployments systemd `--user` (as the
   tooling assumes) or system units? If system, `mcrelay update` needs a
   root/system branch the current service model lacks.
3. **Rate-limit token**: is honouring `GITHUB_TOKEN` when present enough,
   or should the phone app also get an optional token field? (60/hr/IP is
   fine for a person, marginal behind CGNAT.)
4. **Prerelease channel**: should `--pre` exist for testing tagged
   prereleases, or is "latest only" the rule?
5. **Phase 2 timing**: is minisign signing part of the first
   implementation or a follow-up MADR?
6. **Verification tier (B3)**: the free limited-distribution tier caps
   installs at **20 explicitly-authorized devices** — ample today, but it
   is a distribution ceiling. Limited tier now and upgrade later, or full
   account from the start?
7. **v1 → v2, or straight to v2?** The intent-based installer (one system
   dialog per update) could ship a release earlier; the session installer
   is the real UX. Is the intermediate release worth having, given every
   release is also a migration data point for the burn-in of B1?
8. **Verification timing vs region**: enforcement is region-limited until
   2027. Register immediately anyway (it also feeds Play Protect
   reputation, W3/W4), or defer until the global date is announced?

## References (research sources)

- [minio/selfupdate](https://pkg.go.dev/github.com/minio/selfupdate) —
  checksum/signature-verified atomic self-update for Go
- [creativeprojects/go-selfupdate](https://pkg.go.dev/github.com/creativeprojects/go-selfupdate) —
  GitHub Releases discovery and asset matching
- [rhysd/go-github-selfupdate](https://github.com/rhysd/go-github-selfupdate) —
  prior art for the same flow
- [fynelabs/selfupdate](https://pkg.go.dev/github.com/fynelabs/selfupdate) —
  signature-first single-binary updater
- [The Update Framework (TUF)](https://github.com/theupdateframework/python-tuf)
  and [tuf-on-ci](https://github.com/theupdateframework/tuf-on-ci) —
  the reference design for update-system security
- [Properly signing GitHub releases (minisign)](https://gist.github.com/HacKanCuBa/6fabded3565853adebf3dd140e72d33e)
- [Play Console: use of REQUEST_INSTALL_PACKAGES](https://support.google.com/googleplay/android-developer/answer/12085295?hl=en)
- [Building an Android package installer app](https://medium.com/@solrudev/painless-building-of-an-android-package-installer-app-d5a09b5df432) —
  PackageInstaller vs intent-based install

Phone deep-dive (§2):

- [Android developer verification](https://developer.android.com/developer-verification)
  and [Understanding Android developer verification](https://support.google.com/android-developer-console/answer/16561738?hl=en) —
  requirements, tiers (incl. the free 20-device limited tier), timeline
- [Android Developers Blog: developer verification](https://android-developers.googleblog.com/2026/03/android-developer-verification.html) —
  2026-09-30 enforcement start (BR/ID/SG/TH), global 2027, advanced flow
- [Developer guidance for Google Play Protect warnings](https://developers.google.com/android/play-protect/warning-dev-guidance) —
  the W3 triggers: fraud-set permissions, stale targetSdk; appeals process
- [Obtainium — silent/background updates](https://github.com/ImranR98/Obtainium/issues/25)
  and [F-Droid client: use UPDATE_PACKAGES_WITHOUT_USER_ACTION](https://gitlab.com/fdroid/fdroidclient/-/work_items/2316) —
  production use and adoption status of the §2.3 mechanism
- [Sideloaded Android apps can now auto-update (how-to)](https://www.howtogeek.com/this-is-how-i-keep-my-sideloaded-android-apps-updated-automatically/) —
  the installer-of-record / self-update conditions in practice
- [Signal-Android: updating an existing install with the official APK](https://github.com/signalapp/Signal-Android/issues/13107) —
  Signal's website-APK self-updater as prior art
- [NewPipe install/update FAQ](https://newpipe.net/FAQ/install/) —
  cross-source updates refused on signing-key mismatch (the B1 failure
  mode in the wild)
- [APKUpdater](https://github.com/rumboalla/apkupdater) — GitHub Releases
  as an Android update channel

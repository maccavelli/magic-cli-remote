# MADR 0065: Update automation — `mcremote update`, `mcrelay update`, phone "Restart to update"

<!-- markdownlint-disable MD013 MD024 -->

- **Status**: Proposed — feasibility assessment complete, grounded in the
  codebase and outside research; **open questions at the end await review**.
  Not implemented.
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
| Phone "Restart to update" | **Feasible, but BLOCKED on release signing (B1)** | Release APKs are currently signed by each CI runner's throwaway debug key; Android refuses an update whose signature differs, and the escape hatch (uninstall) wipes the keystore-backed pairing |

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

## 2. Decision outcome (proposed)

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

### D4 — Phone: Settings → "App update", gated on B1

After B1 is resolved (CI-signed releases):

1. Settings gains an **App update** entry (next to Version): compares
   `PackageInfo` version against `releases/latest` tag on tap.
2. Update available → download `magic-cli-remote-<tag>-arm64.apk` to the
   app cache dir, verify sha256 against `SHA256SUMS-<VER>`.
3. Prompt **"Restart to update"** — the wording is honest: confirming hands
   the APK to the system installer (`REQUEST_INSTALL_PACKAGES` +
   `FileProvider` content URI), which kills and replaces the app.
4. First use requires the one-time "Install unknown apps" grant; the flow
   must explain that screen before firing the intent, or it reads as a
   failure.
5. Optional polish: a `MY_PACKAGE_REPLACED` receiver relaunches the app
   after the install completes.

New manifest permission: `REQUEST_INSTALL_PACKAGES`. To be removed wholesale
if the app is ever Play-distributed.

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

## 3. Consequences

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

## 4. Alternatives considered

| Option | Why not |
|---|---|
| Shell out to a downloaded `install-binary.sh` | Executes freshly-downloaded shell as the install step — the exact pattern the checksum verify exists to prevent; and the script is repo tooling, not a release asset |
| Package managers (Homebrew tap / apt repo / F-Droid) | Cleaner long-term, but three new distribution channels to maintain for a single-user project; F-Droid would also solve B1, at the cost of build reproducibility work |
| Background auto-update daemonside | Unattended failure with no operator present; Android equivalent violates the user-driven rule; the request itself says "when run" |
| TUF | Right framework, wrong scale — see D5 |
| Obtainium-style external updater app for the phone | Works today with zero code, but keeps the update outside the product and doesn't fix B1's signature churn, which breaks *any* updater |

## 5. Verification plan (sketch — full plan belongs to the PLAN doc)

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

## 6. Open questions (for review)

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

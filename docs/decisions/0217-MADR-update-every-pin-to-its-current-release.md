---
status: accepted
date: 2026-10-30
decision-makers: Project Owner
consulted: "0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md (D1 to D5, D11, D12, D14, D15 and its amendments to 2026-10-21); 0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md, amendment I (I-4); 0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md; 0137-MADR-prompt-to-first-token-latency-regression.md (the known-good engine pins); 0168-MADR-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md (D2); 0174-MADR-keep-dependencies-free-of-known-advisories.md; magic-git's scripts/tools/devenv/version_audit.py and standard.json, and the history of standard.json; 0169-PLAN-standardize-toolchains-on-current-supported-advisory-free-releases.md (P0, P6, P7, P12 to P14, Deviations 20 and 21); this repository at 07ee60b2 with 0216's P2 staged"
informed: anyone who changes go.mod, pubspec.yaml, the Android build files, a workflow pin or a tool pin, or magic-git's devenv standard
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Update every pin to its current release, security fixes first

## Context and Problem Statement

On 2026-10-29 the owner asked: "i want to update all pins to current versions".

This repository pins versions in eleven places: the Go toolchain, Go modules, the owner's two
libraries, CI actions, CI tool versions, Node, Java, the Flutter SDK, Dart packages, the Android
build tools, and each agent engine's known-good version. Some pins are governed by records already:

* [0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md](0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md):
  the toolchains, by a rule, D1;
* [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md):
  the owner's libraries;
* [0137-MADR-prompt-to-first-token-latency-regression.md](0137-MADR-prompt-to-first-token-latency-regression.md):
  the engine pins.

Others have no record: actions, tool versions, Dart packages and Gradle.

This record takes one inventory of all of them against their publishers, decides what "current"
means for each, and puts the moves in order. One of them closes a high-severity advisory in the
shipped Android app.

On 2026-10-30 the owner added the host standard in magic-git: "we run 1.27.3, update the node
version/pin and ensure the python is latest 3.14 without advisories". The owner also drew the
scope's edge: "when i said to update all pins i did not mean for tooling (python) or pins outside
of the magic-cli-remote codebase itself, eg: node, etc, unless they have active advisories." So
the host standard and the host installs are in scope only where an advisory is active (D1, D11).

### The inventory [observed 2026-10-29 from each pin's source; `leftovers/pins_latest.py`, `leftovers/android_bounds.py`, `leftovers/engine_versions.py`, `flutter pub outdated --json`, `go list -m -u all`, and magic-git's `version_audit.py`]

**F1. Go toolchain: current.** `go.mod` declares `go 1.27.3`; go.dev's stable releases are
`go1.27.3` and `go1.26.10`. 0169 D2 reads 1.27.3. Nothing moves.

**F2. Go modules: three direct and many indirect are behind.**

| Module | Pinned | Newest |
| :--- | :--- | :--- |
| `github.com/caddyserver/certmagic` | v0.25.4 | v0.25.6 |
| `golang.org/x/mod` | v0.41.0 | v0.42.0 |
| `github.com/maccavelli/go-selfupdate-lib` | v1.22.3 | ~~v1.23.0~~ **v1.23.1** (F3, F3a) |
| `github.com/maccavelli/acp-go-sdk2` | v1.26.0 | ~~v1.26.0, current~~ **v1.27.0** (F3b) |

* `go list -m -u all` lists 33 indirect modules with a newer version. They include the AWS SDK used
  by `libdns/route53` (`aws-sdk-go-v2` v1.39.1 → v1.47.5, `service/route53` v1.58.3 → v1.70.6),
  `x/tools` v0.51.0 → v0.52.0, `miekg/dns`, `mholt/acmez/v3`, `go-viper/mapstructure/v2` and
  `pelletier/go-toml/v2`. Some, such as `stretchr/testify` and `yuin/goldmark`, are in the module
  graph but not in the build.
* `govulncheck` is clear, apart from the allowlisted `GO-2026-5932` (`x/crypto/openpgp`, required
  module only). OSV lists no fixed version for it (`introduced: 0`, no `fixed`), so no update can
  clear it.

**F3. `go-selfupdate-lib` v1.23.0 changes behaviour Magic can see.** 0207's I-4 predicted it. Its
`CHANGELOG.md` at the tag:

* `service.Supervised.Start` is `ErrNotInstalled` when the supervisor reports the service not
  installed, where it ran `Up`. Magic's runit, s6 and OpenRC backends run through `Supervised`
  (0212).
* launchd: `Start` on a missing property list is `ErrNotInstalled` at once. `Stop`'s `bootout` and
  its wait share one bound.
* systemd: `Start` of a `Type=notify` unit that exits before ready also wraps `ErrUnhealthy`.
* scm: `Start` on an ended context starts nothing.
* On Unix, the install resolves every name through the `os.Root` of the directory it locked.
* Added: pinned update plans (`Updater.Plan`, `Apply`, `ErrPlanStale`), the handoff carrying the
  plan, and `selfupdate/service/servicetest`.

Magic already treats `ErrNotInstalled` as "not installed" on the supervisor paths
(`internal/cli/service/supervisortool.go:209`, `:227`; `supervisorstate.go:94`, `:206`;
`setup.go:243`). Both `ci.yml` pins (`:743`, `:764`) name v1.22.3's commit, and
`TestWorkflowPinsMatchGoMod` ties them to `go.mod`.

**F3a. `go-selfupdate-lib` v1.23.1 is current, and adds nothing Magic can see.** *Observed
2026-10-30, after the inventory (`leftovers/gsl_peel.py`).*

* It is tagged 2026-10-30. Its annotated tag `b0c08a0e…` peels to commit
  `c24f5919cd9e9e3ba3736e64bcf18a6e2a51489e`. The Go proxy lists it, and `go list -m -u` now offers
  it as the newest version.
* It is four commits past v1.23.0, with nothing behind. Only `go.mod`, `go.sum` and documents
  change. No file under `.github/` changes, so the two reusable workflows are v1.23.0's.
* Its `CHANGELOG.md`: it requires `golang.org/x/sys v0.49.0`, `x/term v0.47.0` and `x/mod v0.42.0`,
  and "No exported identifier and no behaviour a program can see changes: `make apicheck` reports
  `v1.23.1` compatible with `v1.23.0`".
* Its `go.mod` says `go 1.26.10`, below Magic's `go 1.27.3`.
* Magic already requires `x/sys` v0.49.0 and `x/term` v0.47.0, and D4 already moves `x/mod` to
  v0.42.0. So v1.23.1 moves no module that D4 would not.

F3's behaviour changes all come from v1.23.0, and v1.23.1 carries them unchanged.

**F3b. `acp-go-sdk2` v1.27.0 is current, and adds a package Magic does not import.** *Observed
2026-10-30, at PLAN P3 step 1 (`leftovers/release_diff.py`, and the release notes at the tag,
`docs/reports/0043-REPORT-release-notes-v1-27-0.md` in that repository).*

* It is tagged 2026-10-30T17:01Z, after the inventory. Its annotated tag `7c89dd4b…` peels to
  commit `913575f6c7fbf4c67ada357a3597293f43e09d2b`. P3's `go get -u ./...` took it.
* It is 13 commits past v1.26.0, with nothing behind. It adds one package, `remote` (ACP over
  Streamable HTTP and WebSocket), and `example/remote`. The release notes say `agentproc/`,
  `acptest/`, `clienthost/`, `internal/`, the schema and every generated file are unchanged, and
  that "nothing the root package, `agentproc`, `acptest` or `clienthost` sends or accepts changes".
* Its `make api` against v1.26.0: "0 incompatible, 11 compatible", each one in `remote`. `remote`
  is outside its compatibility promise.
* Its `go.mod` gains `github.com/coder/websocket v1.8.15`, "compiled only into a program that
  imports `remote`". Magic already requires `github.com/coder/websocket v1.8.15` directly
  (`go.mod:7` before P3), with the same two `go.sum` lines, so the requirement adds nothing here.
  The `go` lines are unchanged.
* 0216's design reads `acp-go-sdk2 v1.26.0`'s `agentproc` and `acptest`. Those are unchanged in
  v1.27.0, so 0216 is unaffected.

**F4. CI actions: four of eight are behind.** Each is pinned by commit, with a version comment.

| Action | Pinned | Newest release, peeled commit |
| :--- | :--- | :--- |
| `actions/checkout` | v7.0.1 `3d3c42e5` | v7.0.1, current |
| `actions/setup-go` | v7.0.0 `b7ad1dad` | v7.0.0, current |
| `actions/setup-node` | v7.0.0 `82076278` | **v7.1.0** `949feb24` (2026-10-08) |
| `actions/setup-java` | v6.0.0 `dd06d9cb` | **v6.1.0** `b843ebf9` (2026-10-14) |
| `actions/upload-artifact` | v7.0.1 `043fb46d` | **v7.0.2** `cf430e03` (2026-10-07) |
| `actions/download-artifact` | `3e5f45b2`, commented `# v8`; that commit is v8.0.1 | **v8.0.2** `9000827c` (2026-10-07) |
| `subosito/flutter-action` | `1a449444`, commented `# v2` | v2.23.0, the same commit |
| `nick-fields/retry` | v4.0.0 `ad984534` | v4.0.0, current |

**F5. CI tool pins.**

* **`govulncheck` is behind:** `golang.org/x/vuln` v1.7.0, at `ci.yml:127` and
  `scripts/vulncheck.sh:31`. The newest is v1.9.0.
* **`staticcheck` is current:** `STATICCHECK_VERSION ?= v0.8.2` (`Makefile:601`).
* **`golint` floats, with nothing to float to:** `go-precheck.sh:46` installs `@latest`, and the
  module's only version is the pseudo-version of 2024-11-12.
* **`markdownlint-cli2` floats.** `make markdownlint-docs` runs `npx markdownlint-cli2`
  (`Makefile:315`) with no version, so each CI run takes npm's newest: 0.24.0 today. 0169 D15
  installed 0.23.2 on hosts. CI has never had a pin.

**F6. Node is behind 0169's own rule.**

* CI's `NODE_VERSION` is `"24"` (`ci.yml:29`, `docs.yml:22`), and `.node-version` and `.nvmrc` say
  `24`.
* 0169 D4: "From **2026-10-28**, when Node 26 becomes Active LTS, the standard moves to the newest
  26.x". nodejs.org lists `v26.13.0 (Lithium)` as the newest LTS. Node 24's newest is v24.22.2.
* The audit flags `24.21.0` with a later security release in its line, `v24.22.1`. CI's `"24"`
  resolves to the newest 24.x at run time, so CI itself is not on 24.21.0.

**F7. The Flutter SDK has a high-severity advisory, and the shipped Android app is affected.**

* CI's `FLUTTER_VERSION` is `"3.47.5"` (`ci.yml:33`), with Dart 3.13.4. The newest stable is
  **3.47.9**, with Dart 3.13.7, released 2026-10-29.
* `dart-lang/sdk` advisory **GHSA-frr6-h895-5pxp** (high, published 2026-10-23): eight eMudhra
  intermediate CA certificates were disclosed as compromised. `dart:io` on Android and Windows
  validates chains with its compiled-in BoringSSL and does not read the OS blocklist. So apps
  built with Flutter before **3.47.8**, or Dart before 3.13.7, accept certificates forged under
  those CAs. Fixed in Flutter 3.47.8 and Dart 3.13.7. iOS and macOS are not affected.
* **Where the phone is exposed.** Read from the code:
  * **Not exposed:** a self-signed daemon. `SecurityContext(withTrustedRoots: false)`, and only a
    fingerprint match is accepted (`apps/mobile/lib/data/ws/mcremote_client.dart:138-147`, `:290`).
  * **Exposed:** a Let's Encrypt-mode daemon with no fingerprint, which "keeps doing plain chain
    validation" (`mcremote_client.dart:290`, `:968`).
  * **Exposed:** the relay's outer connection, a default `HttpClient()` (`relay_transport.dart:92`).
  * **Exposed:** the in-app update check, a default `http.Client()` to `api.github.com`
    (`apps/mobile/lib/data/update/app_update.dart:13-16`).
* 0169 D3 moves Flutter "in lockstep with magic-git's `build_macos.sh` pin and goldens". That is a
  second repository.

**F8. Dart packages: 15 direct dependencies are behind, two by a major.**

* `flutter pub outdated --json` at the 3.47.5 SDK reports no advisory, retraction or
  discontinuation.
* 13 of the 15 move within their constraints. Examples: `go_router` 18.0.0 → 18.1.2,
  `flutter_local_notifications` 22.3.0 → 22.3.3, `mobile_scanner` 7.4.0 → 7.4.2, `speech_to_text`
  7.4.0 → 7.5.0, `url_launcher` 6.3.2 → 6.3.3.
* Two are new majors:
  * `cupertino_icons` 1.0.9 → **2.0.0**;
  * `flutter_secure_storage` 10.3.1 → **11.2.0**, which holds the client identity and the pinned
    fingerprints (0066's subject).
* 65 transitive packages are behind as well.

**F8a. What `flutter_secure_storage` 11 changes, and why the app's store survives it.** From its
`CHANGELOG.md` on `develop`, read 2026-10-29:

* **11.0.0** (2026-08-06), breaking: "items deprecated in v10 have been removed. Any data saved
  using deprecated algorithms or features will be unusable after this upgrade."
  * Removed: `KeyCipherAlgorithm.RSA_ECB_PKCS1Padding`, `StorageCipherAlgorithm.AES_CBC_PKCS7Padding`,
    and `AndroidOptions`' `encryptedSharedPreferences` and `sharedPreferencesName`.
  * `minSdk` rises to 24; 11.1.0 then uses `flutter.compileSdkVersion` in place of a pinned 37.
* **11.1.0** adds `checkUpgradeStatus()`, "to report data lost on a direct major upgrade".
  **11.2.0** fixes Android biometric and namespace recovery, and scopes `deleteAll` to the key
  prefix.
* **Every value the app has stored was written with v11's ciphers:**
  * the app's first commit (`719401b6`, 2026-07-19) already locked `flutter_secure_storage`
    10.3.1;
  * that is after 10.1.0 "fixed default Android key cipher not aligning with the Flutter default",
    and after 10.2.0 made `RSA_ECB_OAEPwithSHA_256andMGF1Padding` and `AES_GCM_NoPadding` the only
    non-deprecated defaults. 10.0.0 had already made them the defaults.
* **The app uses nothing v11 removed.** Its only options are `AndroidOptions(resetOnError: true)` and
  an `IOSOptions` (`apps/mobile/lib/data/local/settings_store.dart:61-66`).
* **The remaining risk is `resetOnError`.** If v11 failed to decrypt an existing value, the plugin
  would reset the store silently (`settings_store.dart:326`), and the phone would lose its client
  identity and pinned fingerprints. The emulator check in D7 is what shows it does not.

**F8b. `cupertino_icons` 2.0.0** "updates minimum supported SDK version to Flutter 3.44/Dart 3.12"
and "migrates to use `cupertino_ui` instead of `flutter/cupertino.dart`" (its `CHANGELOG.md` in
`flutter/packages`). The app uses no `CupertinoIcons` glyph (`grep -rl "CupertinoIcons\." lib`: no
match). So the move changes no rendering. ~~It adds `cupertino_ui` as a transitive package.~~
*Corrected 2026-10-30, at PLAN P1 step 4:* `cupertino_ui` is already in the graph, at 1.0.1
(`flutter pub get` on 3.47.9: "cupertino_ui 1.0.1 (1.1.5 available)"). The move only moves its
version.

**F9. The Android build tools are bounded by Flutter, not by their publishers.**

| Tool | Pinned | Publisher's newest | Flutter 3.47's known maximum (`flutter_tools/lib/src/android/gradle_utils.dart`) | Newest inside it |
| :--- | :--- | :--- | :--- | :--- |
| Gradle (`gradle-wrapper.properties`) | 9.1.0 | 9.8.2 | `maxKnownAndSupportedGradleVersion = '9.3.1'` (`:85`) | 9.3.1 |
| Android Gradle Plugin (`settings.gradle.kts:22`) | 9.0.1 | 9.5.0 | `maxKnownAndSupportedAgpVersion = '9.2'` (`:98`); `maxKnownAgpVersionWithFullKotlinSupport = '9.1.0'` (`:104`) | 9.2.1, or 9.1.1 for full Kotlin support |
| Kotlin Gradle plugin (`settings.gradle.kts:23`) | 2.3.20 | 2.4.21 | `maxKnownAndSupportedKgpVersion = '2.4.0'` (`:91`) | 2.4.0 (2.4.10 to 2.4.21 are past it) |

Flutter's own templates use Gradle 9.3.1, AGP 9.1.0 and Kotlin 2.4.0 (`:36`, `:44`, `:52`). The
bounds were read from the 3.47.5 SDK on this Mac. A patch release seldom moves them, and the PLAN
re-reads them at 3.47.9.

**F10. Java.**

* CI runs `java-version: "25"` (`ci.yml:474`) through `setup-java`, which takes the newest 25.x.
  0169 D5 chose Temurin 25 LTS, and 0168 D2's bytecode target of 17 stands
  (`app/build.gradle.kts:49-50`, `:106`).
* The audit reports the standard's 25.0.4.1 as stale against 25.0.5, and an OpenJDK advisory of
  2026-10-24 "affects 25.0.5.0.2 and earlier". So the newest Temurin is still affected, and there
  is no fixed build to move to yet.

**F11. Every agent engine's known-good pin is behind what is installed.**

| Engine | `KnownGoodVersion` | Installed here (`--version`) | npm's newest |
| :--- | :--- | :--- | :--- |
| grok (`grok/version.go:16`) | 1.0.13 | 1.0.70 | (not on npm) |
| codex (`codex/version.go:17`) | 0.152.1 | 0.166.0 | 0.166.0 |
| kilo (`kilo/version.go:22`) | 7.5.6 | 7.8.18 | 7.8.18 |
| opencode (`opencode/version.go:29`) | 1.18.26 | **v2.2.2** | `opencode-ai` 1.19.5 |

* A pin is the version whose wire shapes the provider's tests were checked against, and it backs a
  committed fixture (0137 Phase 3). A mismatch only warns.
* Moving one means re-reading those shapes against the new version, which takes a live run of
  that provider: quota. grok 1.0.70 already has a committed capture (0207 SDK-gaps PLAN, C3).
* opencode 2.x is the subject of
  [0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md](0214-MADR-run-opencode-v2-over-acp-with-a-lease-bound-http-control-plane.md),
  proposed.

**F12. The host standard in magic-git is stale against 0169's own amendments.**

* `standard.json` still says Go 1.27.1, Node 24.21.0, Flutter 3.47.5 and Java 25.0.4.1.
* `version_audit.py` reports 8 STALE and 4 ADVISORY against it. Besides F6, F7 and F10, it flags
  Python 3.14.7 (11 advisories fixed by 3.14.10), Git for Windows, gh and glab.
* Those are host installs and a file in another repository (0169 P14). This repository pins none
  of them.
* **Its history stops at 2026-09-30.** magic-git's log for `standard.json` has two commits,
  `74c7380` (2026-09-29) and `4f400c9` (2026-09-30, gh 2.102.0). 0169's P12 step 3 (Go 1.27.2)
  and P14 step 4 (Go 1.27.3) each left the Go move "for the owner to commit there", and neither
  reached the file. The file has no floor key, and the audit reads none: 0169 D2's floor lives in
  prose only.
* **The audit, row by row, with an advisory or without** [run 2026-10-30 against the file and
  against a scratch copy with the candidates below; `leftovers/standard_candidate.py`]:

  | Row | Standard | Advisory active on it | Candidate | The candidate's audit |
  | :--- | :--- | :--- | :--- | :--- |
  | Go | 1.27.1 | **yes:** "16 advisories fixed by 1.27.3", GO-2026-6599 to GO-2026-6809 | 1.27.3 | no row |
  | Node | 24.21.0 | **yes:** "1 later security releases in its line: v24.22.1" | 26.13.0 | no row |
  | Python | 3.14.7 | **yes:** "11 advisories fixed by 3.14.10" | 3.14.10 | `STALE 3.14.10 -> 3.15.2` only (F15) |
  | Flutter | 3.47.5 | **yes:** GHSA-frr6-h895-5pxp (F7) | (magic-git's pair, D11) | — |
  | Java | 25.0.4.1 | yes, with no fixed build (F10) | none exists | — |
  | Git for Windows | 2.56.0.windows.1 | no | — | STALE only |
  | gh | 2.102.0 | no | — | STALE only |
  | glab | 1.120.0 | no | — | STALE only |

**F13. Go on the hosts is already 1.27.3.** 0169's P13 and P14 records, and its Deviation 21: this
Mac, the Linux and Windows test hosts, and the WSL distro all report `go1.27.3`, with
`GOTOOLCHAIN=go1.27.3`. This Mac, read 2026-10-30: `go version go1.27.3 darwin/arm64`. Only the
standard's row is behind.

**F14. Node on the hosts.**

* nodejs.org's index, 2026-10-30: `v26.13.0` (2026-10-28, LTS "Lithium"); `v26.12.1` (2026-10-21) is
  flagged `security: true`. 0169 D4 moved the standard to the newest 26.x from 2026-10-28.
* **This Mac resolves Homebrew's `node` 26.10.0** (`/opt/homebrew/Cellar/node/26.10.0_1`), not the
  `node@24` 24.21.0 that 0169's P6a linked, which is still installed. 26.10.0 predates 26.12.1's
  security release, so the advisory is active here. Homebrew's stable `node` is 26.13.0.
* **The other hosts** run 24.21.0, as 0169's P6a left them:
  * ~~the Linux test host from `~/sdk/node-v24.21.0`, on PATH through the dotfiles' `00-paths.sh` and
    the `mcremote` drop-in;~~ *Corrected 2026-10-30, at PLAN P5c (Deviation 12):* on the Linux
    test host, the login PATH names `~/sdk/node-v26.10.0` (created 2026-09-21), through an owner's
    uncommitted edit to the dotfiles' `00-paths.sh:27`. The `mcremote` drop-in, `path.conf:22`, still
    names `~/sdk/node-v24.21.0`, so the daemon runs 24.21.0;
  * WSL from `~/sdk/node-v24.21.0`, through `~/.config/devenv.sh`;
  * the Windows test host from the `node-v24.21.0-x64.msi` install.

  *Corrected 2026-10-30, at PLAN P5c (Deviation 12):* WSL's `devenv.sh` named
  `~/sdk/node-v26.10.0`, and the Windows test host's MSI install was v26.10.0. Each is the owner's
  earlier move.

  The PLAN re-reads each before it moves anything.
* npm global packages live under each host's own prefix (for example `markdownlint-cli2` under
  `~/.local`, 0169 D15). They are not inside the Node directory, so they survive the swap. A package
  with a native addon would need rebuilding for Node 26's ABI. That is why the PLAN lists them, and
  runs each one's `--version` before and after.

**F15. Python 3.14.10 has no advisory, and the audit fails on any held line.**

* endoflife.date: 3.14's newest is **3.14.10** (2026-10-25), supported until 2027-10-01 and secure
  until 2030-10-31. 3.15 is out, at 3.15.2.
* OSV, queried by each release tag's peeled commit (`leftovers/python_osv.py`, the audit's own
  method), counts records, aliases included:

  | Release | Tag commit | OSV records |
  | :--- | :--- | :--- |
  | 3.14.7 | `823f0323ee6e` | 19 |
  | 3.14.8 | `8e6e75d9102e` | 8 |
  | 3.14.9 | `ead82c07db1f` | 2 (PSF-2026-45, PSF-2026-46) |
  | **3.14.10** | `b703b2c5e704` | **0** |

* **This Mac's `python3` is Homebrew's `python@3.14` 3.14.8** (3.14.7 also installed). Homebrew's
  stable is 3.14.10. By 0169's records (P0, P7, Deviation 20), the Linux test host and WSL run
  python-build-standalone 3.14.7 from `~/sdk/python-3.14.7`, with `~/default-venv` built on it, and
  the Windows test host runs the `py` install manager's 3.14.7. The PLAN re-reads each.
  *Corrected 2026-10-30, at PLAN P5c:* this Mac's `python@3.14` is 3.14.10 by then. On the Linux
  test host the owner installed `~/sdk/python-3.14.10` on 2026-10-30 and rebuilt `~/default-venv` on
  it (Deviation 12); `python-3.14.7` is still there. WSL is the same, and the Windows test host's
  `py` install manager already has 3.14.10.
* **0169 D1 would take 3.15.** Its rule is "the newest stable patch of the newest line". The owner
  holds 3.14.
* **The audit cannot express a held line.** `s_python` (`version_audit.py:569-578`) compares the
  standard with the newest supported cycle of every Python line. `STALE` is in `FAIL_STATES`
  (`:46`), and `exit_code` returns 1 on any fail state (`:61-64`). So with the standard on 3.14.10,
  the audit reports `STALE standard python 3.14.10 -> 3.15.2` and exits 1, on every run, for as
  long as the hold stands.
* A feed that cannot be reached is an `ERROR` row (`:144-146`), and exits 2. So "no advisory row"
  is a real answer, and not an outage read as clean.
* This repository pins no Python. No workflow uses `setup-python`, and there is no
  `.python-version`.

## Decision Drivers

* **A published advisory against a shipped artifact is fixed first.** 0169 D10: "Exposures are
  remediated first, ahead of every other change." F7 is one.
* **"Current" means 0169 D1's rule, applied to every pin:** the newest stable release that is
  supported and has no advisory, *and* is inside what its consumer supports. Flutter bounds Gradle,
  AGP and Kotlin (F9).
* **A version move is adopted only after this repository's gates pass on it** (0169 D1). That
  includes the Linux and Windows test hosts, the APK build, and the live supervisors job for F3.
* **A behaviour change is a decision, not a bump.** F3, the two majors in F8, and the engine pins in
  F11 each change something Magic relies on.
* **Provider quota is spent only with the owner's word** (F11).
* **Outside this codebase, only an active advisory moves a pin** (the owner, 2026-10-30). Host
  installs and magic-git's standard are outside it.
* **What another product gates is changed under that product's record** (F7's lockstep with
  magic-git's goldens). magic-git's devenv tooling has no record of its own: 0169 wrote it, so
  this record changes it the way 0169 did.
* **Each move is reversible on its own,** so one that fails a gate does not hold the rest.

## Considered Options

* **A — Everything to its publisher's newest in one pass,** majors and engine pins included.
* **B — Tiered by risk** (chosen):
  1. the advisory fix;
  2. in-range moves that change no behaviour;
  3. the moves that do, each a decision of its own;
  4. engine pins with their own live runs.
* **C — The advisory fix only;** the rest waits for the next audit.
* **D — Nothing.**

## Decision Outcome

Chosen option: **"B — Tiered by risk"**, because:

* it ships the one fix that matters to users now (F7) without waiting on anything;
* it moves every other pin to current under the same rule (D1);
* and it keeps each behaviour change visible as its own decision, which A buries in a single diff
  and C and D leave undone.

### The decisions (accepted 2026-10-30)

* **D1 — "Current" is 0169 D1 for every pin.** That is the newest stable release that is supported,
  has no advisory affecting it, is inside what its consumer supports, and passes this repository's
  gates. A pin with an affected newest release and no fix (F2's `GO-2026-5932`, F10's JDK) stays
  where it is, and is named in the record.
  * **Scope, the owner's, 2026-10-30.** The rule moves every pin in this codebase. A pin outside it
    moves only while an advisory is active on it. That covers the host installs, magic-git's
    `standard.json`, and developer tooling such as Python. When it does move, it moves to the
    version the rule gives, or to the version the owner holds (D13).
  * By F12's table, that is Go, Node and Python (D11, D14). Flutter moves under magic-git's own
    pair (D2). Java has no fix (D9). Git for Windows, gh and glab have no advisory, and stay.
* **D2 — Flutter 3.47.9 with Dart 3.13.7, first, in a commit of its own (F7).**
  * `FLUTTER_VERSION: "3.47.9"`.
  * `pubspec.yaml`'s `sdk: ^3.12.2` is left alone: it is a floor, and 3.13.7 satisfies it.
  * This Mac's Flutter moves to 3.47.9 for the gates.
  * ~~The next release's APK is built with it. A release is what reaches users, so the PLAN asks
    the owner whether to cut one at once.~~ *Owner's answer, 2026-10-29: "First, then cut a release
    (Recommended)".* A release is cut straight after this phase's commit, so the fixed APK reaches
    users. The agent prepares it, and the owner pushes and tags, each on an explicit ask.
  * magic-git's `build_macos.sh` pin and goldens (0169 D3's lockstep) are listed for that
    repository, and not changed from here. *Owner's answer, 2026-10-30: "magic-git's own pair
    (Recommended)".* That pair is also where `standard.json`'s Flutter row and the other hosts'
    Flutter move, with the pin. Until then, the audit reports this Mac's Flutter as drift
    from the standard. ~~magic-git's `CLAUDE.md` says to use its vendored `.flutter-sdk` whenever
    `flutter` on PATH disagrees with `build_macos.sh`, so this Mac's move leaves its goldens
    alone.~~ *Corrected 2026-10-30, while writing the PLAN:* this Mac has no `.flutter-sdk` in
    magic-git (`.flutter-sdk/bin/flutter`: no such file). `build_macos.sh` uses the Flutter on PATH
    when its `git describe` tag matches the pin (`:158-167`), and that is Homebrew's 3.47.5 today.
    After this phase, PATH gives 3.47.9. magic-git's `CLAUDE.md` then says to use the vendored SDK,
    which `build_macos.sh` clones (`:175-178`, `git clone --depth 1 --branch 3.47.5`, gitignored at
    `.gitignore:53`). So magic-git's goldens are safe only on that SDK, and phase 5 clones it before
    running magic-git's gates.
* **D3 — Node 26, as 0169 D4 already decided (F6).**
  * `NODE_VERSION: "26"` in `ci.yml` and `docs.yml`, and `26` in `.node-version` and `.nvmrc`.
  * The only thing CI runs on Node is `markdownlint-cli2` (D6).
* **D4 — Go modules to their newest, with `go-selfupdate-lib` as D5 says (F2).**
  * `go get -u ./...`, then `go mod tidy`. That moves `certmagic`, `x/mod` and every indirect
    module in the build. ~~`acp-go-sdk2` is already current.~~ *Amended 2026-10-30, at PLAN P3
    step 1 (Deviation 7), by the owner: "Take v1.27.0".* It also moves `acp-go-sdk2` to v1.27.0
    (F3b).
  * `make vulncheck` must stay "clear" apart from `GO-2026-5932`.
* **D5 — `go-selfupdate-lib` ~~v1.23.0~~ v1.23.1, as an amendment to 0207 (F3, F3a).**
  *Amended 2026-10-30 by the owner, before phase 3: "pin go-selfupdate-lib to current v1.23.1".*
  v1.23.1 was tagged after the inventory, and is its current release (F3a).
  * `go.mod`, `go.sum`, and both `ci.yml` pins move to ~~v1.23.0's~~ v1.23.1's commit, with its
    comment.
  * Its behaviour changes are checked by:
    * Magic's service tests;
    * the runit/s6 live supervisors job;
    * the Windows test host's `make ci-windows`;
    * and a read of every `Start` caller for the new `ErrNotInstalled` and `ErrUnhealthy` returns.
  * Adopting its new APIs (`Plan` and `Apply`, `servicetest` for 0212's backends) is not a pin. It
    waits for a record of its own.
* **D6 — CI actions and tool pins to current (F4, F5).**
  * Actions, by peeled commit, each with its exact version comment:
    * `setup-node` v7.1.0 `949feb24…`;
    * `setup-java` v6.1.0 `b843ebf9…`;
    * `upload-artifact` v7.0.2 `cf430e03…`;
    * `download-artifact` v8.0.2 `9000827c…`;
    * `flutter-action`'s comment becomes `# v2.23.0`, on the same commit.
  * `govulncheck` v1.9.0, in `ci.yml:127` and `scripts/vulncheck.sh:31` together.
  * `markdownlint-cli2` gets a pin: `npx --yes markdownlint-cli2@0.24.0` in `Makefile:315`. Its run
    then no longer depends on the day it happens.
  * `staticcheck` v0.8.2 and `golint` are current, and stay.
* **D7 — Dart packages to their newest, the two majors included (F8, F8a, F8b).** *Owner's
  answer, 2026-10-29: "Both now".*
  * `flutter pub upgrade`: the 13 in-range moves, and the transitive packages they allow.
    `pubspec.yaml`'s floors are not raised for those.
  * ~~`cupertino_icons` 2 and `flutter_secure_storage` 11 are not taken here.~~ Both majors are
    taken. `pubspec.yaml` says `cupertino_icons: ^2.0.0` and `flutter_secure_storage: ^11.2.0`, then
    `flutter pub upgrade --major-versions` is limited to those two.
  * `flutter_secure_storage` 11 holds the client identity and the pinned fingerprints (0066). F8a is
    why no migration code is needed. The emulator proves it:
    1. pair the AVD using an APK built from the commit before this phase;
    2. install this phase's APK over it, keeping the app's data;
    3. the app reconnects with no re-pair;
    4. the client identity's public key is unchanged, from the app's diagnostics;
    5. the pinned fingerprint is unchanged.

    A re-pair, or a changed key, is a stop, not a workaround.
* **D8 — Android build tools to the newest inside Flutter 3.47.9's known range (F9):**
  * Gradle **9.3.1**;
  * AGP **9.1.1**, the newest with Flutter's full Kotlin support. 9.2.1 is supported but past
    `maxKnownAgpVersionWithFullKotlinSupport`. *Owner's answer, 2026-10-29: "AGP 9.1.1
    (Recommended)";*
  * Kotlin Gradle plugin **2.4.0**.

  The PLAN re-reads the bounds in the 3.47.9 SDK, and the build must show no Flutter version
  warning. The bytecode target stays 17 (0168 D2).
* **D9 — Java: no change (F10).** `"25"` already takes the newest Temurin 25. The 2026-10-24
  advisory has no fixed build yet. The record names it, and 0169's audit flags the fix when it
  ships.
* **D10 — Engine pins: each its own decision, with its own live run (F11).** *Owner's answer,
  2026-10-29: "Each its own record (Recommended)".*
  * This record moves none of them. It lists them, with the installed versions.
  * Each move is 0137's procedure: a capture, the wire-shape tests re-read, and the pin moved.
  * grok 1.0.70's capture already exists. opencode 2.x waits on 0214.
* ~~**D11 — The host standard is magic-git's to update (F12).** The audit's output is quoted here.
  `standard.json` and the host installs are 0169 P14's, in that repository.~~ *Replaced
  2026-10-30, at the owner's ask: "spec and scope these in".*
* **D11 — The host standard's rows with an active advisory move here (F12, D1's scope).** In
  magic-git's `scripts/tools/devenv/standard.json`:
  * `go` 1.27.1 → **1.27.3** (F13). The hosts are already there, so the row catches up with
    them;
  * `node` 24.21.0 → **26.13.0**, `line` `"24"` → `"26"`, the note rewritten for 26 (F14,
    0169 D4);
  * `python` 3.14.7 → **3.14.10**, held on the 3.14 line (D13, F15);
  * each moved row's `verified` set to the day it is checked, and its note made true. Go's note
    keeps "Every binary in GOBIN and ~/go/bin is built by this version", which F13's hosts already
    satisfy.

  Not moved here:
  * `flutter`, by magic-git's own pair (D2);
  * `java`, which has no fix (D9);
  * `git` (Git for Windows), `gh` and `glab`, which have no advisory (D1's scope).

  The file's `record` field keeps naming 0169 D11, whose rule it still is. The moved rows' notes
  cite this record. 0169's P12 step 3 and P14 step 4, which never reached the file (F12), are
  closed by this, and noted in 0169's PLAN by pointer.
* **D12 — One PLAN, in five phases, each its own commit:**
  1. D2;
  2. D3 and D6;
  3. D4 and D5;
  4. D7 and D8;
  5. D11, D13 and D14: the host standard, the audit's hold, and the hosts. *Added 2026-10-30.*
     * Its commits are the owner's in magic-git (`standard.json`, `version_audit.py`, the devenv
       `README.md`) and in the dotfiles repository (`00-paths.sh` and the drop-in), and this
       PLAN's execution record here.
     * It touches no file of this repository's product, so it can run at any point. It runs
       last, so the D2 release is not held for it.

  Phases 1 to 4 are gated on macOS, the Linux and Windows test hosts, and, where they touch the
  app, the APK build and the emulator. Phase 5 is gated by the audit over every host's probe, and
  by magic-git's own pre-staging gates. The owner's word is asked for those runs.
  * 0216's stderr plan continues afterwards. Its P2 is staged and is committed first.
  * D5 also amends 0207. D2, D3, D11 and D13 are noted in 0169, by pointer: D13 because it
    departs from 0169 D1 for Python.
* **D13 — Python is held on 3.14 at the owner's word, and the audit learns what a hold is (F15).**
  *Owner's answers, 2026-10-30: "ensure the python is latest 3.14 without advisories", and "Held
  line in audit (Recommended)".*
  * This departs from 0169 D1 for Python alone: "the newest line" would be 3.15.
  * `standard.json`'s `python` gains `"held_line": "3.14"` and a `"held_reason"` that cites this
    record. Its existing `line` already says `3.14`.
  * `version_audit.py`'s `s_python`, when `held_line` is set:
    * judges staleness against the newest patch of the held line, not of the newest line. A newer
      3.14 patch is still `STALE`, and still fails;
    * reports a newer line as a new state, `HELD`, which names the line and its newest patch
      (`3.14 held; newest line 3.15 (3.15.2)`). `HELD` is not in `FAIL_STATES`, so it does not
      change the exit status;
    * reports `UNSUPPORTED`, a fail, once the held line is past its end of life, so a hold cannot
      outlive its line's security support;
    * leaves the advisory check as it is: OSV against the standard's version, then the held line's
      newest patch as the fix candidate (`newest_python`, `:428-431`, is already per line);
    * fails with `ERROR` when `held_line` disagrees with `line` or with the standard's version.
      A typo then cannot hold a line nobody chose.
  * Hosts are still judged against the standard's version, as drift. The hold changes only how
    the standard is judged against the publishers.
  * The devenv `README.md` documents `held_line` and `HELD`.
  * The other toolchains do not get a hold. Python is the only line the owner has held, and a
    general mechanism would be a decision nobody has made.
* **D14 — The hosts move with their rows (F13 to F15).** Each host is changed the way 0169 changed
  it, with a dated backup first and the old version left installed until nothing needs it (0169
  C4):
  * **Go:** none moves. Each host's `go version` is read, and must say `go1.27.3`.
    *Amended 2026-10-30, at PLAN P5d (Deviation 13), by the owner: "Fix the Go rows in P5".* The
    audit over the hosts found every Go bin directory's tools built with go1.27.2 (`DRIFT`), and
    the kept go1.27.2 installs, with go1.27.1 on the Windows test host, carrying advisories
    (`ADVISORY` GO-2026-6809; 16 on go1.27.1). Those advisories bring them into scope (D1's rule).
    So on each host every tool not built with go1.27.3 is rebuilt with it, at the module version
    it already records (`go install <path>@<version>`), and the old Go installs are removed once
    nothing on the host names them.
  * **Node 26.13.0,** each archive or installer checked against nodejs.org's `SHASUMS256.txt`:
    * this Mac: `brew upgrade node`;
    * the Linux test host: `~/sdk/node-v26.13.0`, with PATH swapped in the dotfiles'
      `00-paths.sh` and in the `mcremote` drop-in, then `systemctl --user daemon-reload`. The
      owner commits the dotfiles change there;
    * WSL: `~/sdk/node-v26.13.0`, with PATH swapped in `~/.config/devenv.sh`;
    * the Windows test host: `node-v26.13.0-x64.msi`.

    Each host's npm global packages are listed before and after, and each one's `--version`
    must answer. `--version` spends no provider quota.
  * **Python 3.14.10:**
    * this Mac: `brew upgrade python@3.14`;
    * the Linux test host and WSL: python-build-standalone's 3.14.10 `install_only` archive, checked
      against its GitHub asset digest, in `~/sdk/python-3.14.10`. `~/default-venv` is rebuilt on it
      from a saved `pip freeze`, and the old one is kept as a backup until that passes (0169 P0
      step 4);
    * the Windows test host: `py install --update 3.14`.

    Distribution system Pythons stay the distribution's (0169 D6).
  * A host whose install does not verify is left on its old version, and the PLAN stops for the
    owner.
  * Homebrew's `node` formula follows Node's newest release line, odd lines included. When it
    moves past 26, this Mac's `node` drifts from the standard, and the audit reports it.

### Consequences

* Good, because the shipped app stops accepting certificates forged under the compromised CAs.
  That covers its Let's Encrypt-mode, relay and update-check connections (F7).
* Good, because every pin ends current under one stated rule, and the ones that cannot move (D1,
  D9, D10) are named rather than forgotten.
* Good, because `markdownlint-cli2` stops floating in CI (D6).
* Good, because the Android build stays inside what Flutter knows (D8), rather than on versions
  Flutter would warn about.
* Neutral, because `go get -u ./...` moves 33 indirect modules that no one chose individually. The
  gates and `govulncheck` are what check them, as for any tidy.
* Bad, because `go-selfupdate-lib` v1.23.0 changes how `Start` fails for a service that isn't
  installed, on launchd and on every supervised backend. Magic's handling exists, but the live
  job and the Windows host are what prove it.
* Bad, because the four engine pins stay behind after this record, by decision. Each needs a live
  run this record does not do.
* Bad, because `flutter_secure_storage` 11 can reset the phone's store if it cannot read it
  (`resetOnError`). F8a argues that it can read it, and D7's emulator check is the proof.
* ~~Bad, because Flutter's lockstep with magic-git (D2) and the stale host standard (D11) are left to
  another repository.~~ *Superseded 2026-10-30 by D11's replacement.*
* Good, because no host and no standard row is left with an advisory that has a fix: Go's 16,
  Node's security release and Python's 11 are closed (D11, D14).
* Good, because the audit can pass again while Python is held. A newer 3.14 patch, an advisory,
  or the held line's end of life still fails it (D13).
* Bad, because Flutter's lockstep with magic-git (D2) waits on magic-git's own pair. Until then,
  the standard's Flutter row keeps its advisory, and the other hosts' local Flutter builds keep
  accepting the compromised CAs. Released APKs are built in CI, which D2 fixes.
* Bad, because a hold is an exception to 0169 D1 that the audit no longer flags as stale. Only the
  record, and the `HELD` row on every run, keep it visible.
* Bad, because phase 5 changes four hosts and two other repositories. Each change has a backup and
  a rollback, but the dotfiles and magic-git commits are the owner's to make.
* Neutral, because Git for Windows, gh and glab stay `STALE` in the audit by the owner's scope.
  The audit still exits 1 for them until a later move, or until one of them has an advisory.

### Confirmation

* **D2:** `flutter --version` on this Mac gives 3.47.9 and Dart 3.13.7. `make preflight` passes.
  CI's Flutter jobs run 3.47.9. A dispatched `Android APK` run builds.
* **D3:** CI's `setup-node` resolves 26.x, and `make markdownlint-docs` passes on it.
* **D4, D5:** `go list -m -u` shows no newer version for a direct module. `make vulncheck` is
  "clear". `TestWorkflowPinsMatchGoMod` passes. `make test`, `make race`, the runit/s6 live job,
  the Linux test host, and `make ci-windows` on the Windows test host all pass.
* **D6:** every `uses:` names a peeled release commit with its exact version.
  `make markdownlint-docs` prints `markdownlint-cli2 v0.24.0`.
* **D7, D8:** `flutter pub outdated` shows no direct dependency behind. `flutter build apk` shows
  no AGP, Gradle or Kotlin warning. On the emulator, a pairing made with the previous APK survives
  the upgrade, with the same client key and pinned fingerprint.
* ~~After all four phases, magic-git's `version_audit.py` is run again, and every flag it still
  raises is one D9, D10 or D11 names.~~
* **D13:** the held-line logic is seen failing first, on scratch copies of the standard, never in
  magic-git's tree:
  * a held standard behind the line's newest patch gives `STALE`;
  * `--today` past 3.14's end of life gives `UNSUPPORTED`;
  * a `held_line` that disagrees with `line` gives `ERROR`;
  * a standard with no `held_line` still gives `STALE ... -> 3.15.2`, as today;
  * the real standard gives `HELD`, and exit 0 when nothing else fails.

  The audit still runs under macOS's system Python 3.9 (its own floor, and magic-git's
  `dependencies.py`).
* **D11, D14:** after phase 5, the audit over a fresh probe of every host, against the moved
  `standard.json`, reports:
  * no Go, Node or Python row except `HELD`;
  * no `ADVISORY` row except Java's (D9) and Flutter's (magic-git's pair);
  * `STALE` only for Flutter, Java, Git for Windows, gh and glab.

  Each of those is a row this record names. On each host, `go version` gives 1.27.3, `node
  --version` v26.13.0, and the developer `python3` (`python` on Windows) 3.14.10. Every npm global
  package's `--version` answers as before.

## Pros and Cons of the Options

### A — Everything at once

* Good, because it is one change, and nothing stays behind.
* Bad, because the advisory fix waits on the riskiest move in the set.
* Bad, because the secure-storage major, the self-update library's behaviour change and four engine
  pins arrive in one diff, and a failure cannot be traced to one of them.
* Bad, because the engine pins need live runs (quota) and wire re-reads, and Gradle past Flutter's
  known range is what Flutter warns against.

### B — Tiered by risk (chosen)

* Good, because the fix that matters ships first.
* Good, because each behaviour change keeps its own decision.
* Bad, because four commits and three later records are more work than one pass.

### C — The advisory fix only

* Good, because it is the smallest safe change.
* Bad, because the owner asked for every pin, and Node is already behind 0169's own date (F6).

### D — Nothing

* Bad, because the shipped app stays exposed (F7).

## More Information

### Open questions for the owner

1. **The order.** D2, Flutter, first in a commit of its own, before 0216's P3 resumes? And is a
   release with the fixed APK cut straight after?
2. **D7's two Dart majors.** Leave both for records of their own (proposed); take `cupertino_icons` 2
   now and leave secure storage; or take both now?
3. **D8's AGP.** 9.1.1, the newest with Flutter's full Kotlin support (proposed), or 9.2.1, the
   newest Flutter supports at all?
4. **D10's engine pins.** Leave each to its own record (proposed); move grok's to 1.0.70 now, on its
   existing capture; or schedule live runs for all four?

*Answered 2026-10-29:*

1. "First, then cut a release (Recommended)": D2.
2. "Both now": D7, with F8a and F8b researched after the answer.
3. "AGP 9.1.1 (Recommended)": D8.
4. "Each its own record (Recommended)": D10.

*Asked and answered 2026-10-30, after the owner scoped in the host standard:*

1. **How the standard holds Python on 3.14,** when `STALE` fails the audit (F15). "Held line in
   audit (Recommended)": D13.
2. **Git for Windows, gh and glab,** stale with no advisory. The owner did not pick an option, and
   answered: "when i said to update all pins i did not mean for tooling (python) or pins outside of
   the magic-cli-remote codebase itself, eg: node, etc, unless they have active advisories." Read as
   D1's scope rule. So they stay, and Go, Node and Python move because each has an active
   advisory (F12).
3. **The standard's Flutter row and magic-git's pin.** "magic-git's own pair (Recommended)": D2.

~~The record stays `proposed` until the owner approves it.~~ *Accepted 2026-10-30: "committed,
proceed", in answer to "Approve the MADR and I'll write the five-phase PLAN".* The plan is
[0217-PLAN-update-every-pin-to-its-current-release.md](0217-PLAN-update-every-pin-to-its-current-release.md).

### Evidence index

* `leftovers/pins_latest.py` and `pins_latest.json`: go.dev, nodejs.org, the Flutter release feed,
  the GitHub API for each action (latest release and peeled commit), the Go module proxy, npm,
  Gradle, Google's Maven and Maven Central, and Adoptium.
* `leftovers/android_bounds.py`: the AGP, KGP and Gradle patch releases. Flutter's bounds come from
  `/opt/homebrew/share/flutter/packages/flutter_tools/lib/src/android/gradle_utils.dart` at 3.47.5.
* `leftovers/engine_versions.py`: each engine's `--version`, and npm.
* `flutter pub outdated --json`, summarised by `leftovers/pub_outdated_summary.py`.
* `go list -m -u all`; OSV `GET /v1/vulns/GO-2026-5932`.
* `gh api repos/dart-lang/sdk/security-advisories/GHSA-frr6-h895-5pxp`.
* `go-selfupdate-lib`'s `CHANGELOG.md` at `v1.23.0`, from the contents API.
* `leftovers/gsl_peel.py v1.23.0 v1.23.1`, 2026-10-30 (F3a): both tags peeled through `gh api`,
  the compare between them, `CHANGELOG.md` at `v1.23.1`, and `go list -m -versions`. Also
  `go.mod` at `v1.23.1` from the contents API, and `go list -m -u` for `x/sys`, `x/term`, `x/mod`
  and `go-selfupdate-lib` in this repository.
* `leftovers/release_diff.py maccavelli/acp-go-sdk2 v1.26.0 v1.27.0`, 2026-10-30 (F3b): both
  tags peeled, the compare between them, and its `go.mod` and `go.sum` patch. Also that
  repository's `docs/reports/0043-REPORT-release-notes-v1-27-0.md` at `v1.27.0`, from the contents
  API.
* magic-git `scripts/tools/devenv/version_audit.py` against its `standard.json`, run read-only on
  2026-10-29: "4 ADVISORY, 1 ERROR, 1 REVIEW, 8 STALE". The ERROR is that this Mac has no probe
  documents.
* `leftovers/standard_candidate.py`, 2026-10-30. It copies `standard.json` to the scratchpad with
  Go 1.27.3, Node 26.13.0 and Python 3.14.10, and runs the audit on the file and on the copy:
  * the file: "4 ADVISORY, 1 ERROR, 1 REVIEW, 8 STALE; exit 1";
  * the copy: "1 ADVISORY, 1 ERROR, 1 REVIEW, 6 STALE; exit 1". Its one advisory is Java's, and
    its Python row is `STALE 3.14.10 -> 3.15.2`.
* `leftovers/python_osv.py`, 2026-10-30: each CPython tag's commit through `gh api`, then OSV
  `POST /v1/query` by commit (F15's table).
* endoflife.date `GET /api/python/3.14.json`: `latest` 3.14.10, `latestReleaseDate` 2026-10-25,
  `eol` 2030-10-31.
* nodejs.org `dist/index.json` (F14), and `dist/v26.13.0/SHASUMS256.txt`, which lists
  `node-v26.13.0-x64.msi` (`0e5ee99c…`), `-linux-x64.tar.xz` (`d28a96af…`) and `-linux-x64.tar.gz`
  (`58485538…`).
* python-build-standalone's release `20261026` carries
  `cpython-3.14.10+20261026-x86_64-unknown-linux-gnu-install_only.tar.gz`.
* `leftovers/mac_toolchains.py`, 2026-10-30: this Mac's `go`, `node` and `python3` in a login
  shell, and `brew list --versions` / `brew info` for `node`, `node@24` and `python@3.14`.
* magic-git, read only: `git log` of `standard.json`, and `version_audit.py` at `:46`, `:61-69`,
  `:144-146`, `:285-295`, `:428-437` and `:569-578`.

### Related records

* [0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md](0169-MADR-standardize-toolchains-on-current-supported-advisory-free-releases.md): D1's rule, D3, D4, D6, D10, D11, D15. D13 departs from D1 for Python.
* [0169-PLAN-standardize-toolchains-on-current-supported-advisory-free-releases.md](0169-PLAN-standardize-toolchains-on-current-supported-advisory-free-releases.md): how each host was installed (P0, P6, P7), and the standard's Go row that P12 and P14 left for the owner (F12).
* [0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md](0207-MADR-move-to-current-library-releases-and-close-the-adoption-gaps.md): I-4, and the amendment D5 adds.
* [0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md](0212-MADR-run-runit-s6-and-openrc-through-the-librarys-supervised-adapter.md): the backends F3's `Supervised` change reaches.
* [0137-MADR-prompt-to-first-token-latency-regression.md](0137-MADR-prompt-to-first-token-latency-regression.md): the engine pins (D10).
* [0168-MADR-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md](../../apps/mobile/docs/decisions/0168-MADR-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md), in the mobile tree: D2, the bytecode target.
* [0174-MADR-keep-dependencies-free-of-known-advisories.md](0174-MADR-keep-dependencies-free-of-known-advisories.md): the advisory gate D4 keeps.
* [0216-MADR-show-an-agents-last-stderr-and-record-and-replay-both-ways-of-the-acp-wire.md](0216-MADR-show-an-agents-last-stderr-and-record-and-replay-both-ways-of-the-acp-wire.md): its plan, which resumes after this one.
* [0066-MADR-secure-storage-upgrade-resilience.md](0066-MADR-secure-storage-upgrade-resilience.md):
  the storage the `flutter_secure_storage` major touches.

---
status: in-progress
date: 2026-10-30
associated-madr: "0217-MADR-update-every-pin-to-its-current-release.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement updating every pin to its current release, security fixes first

Associated MADR: [0217-MADR-update-every-pin-to-its-current-release.md](0217-MADR-update-every-pin-to-its-current-release.md)

## Goal

Every pin in this codebase ends on its current release under 0169 D1's rule. Every pin outside it
that has an active advisory ends fixed. The order is the MADR's D12:

1. Flutter 3.47.9 and a release, which closes GHSA-frr6-h895-5pxp in the shipped app;
2. Node 26 and the CI action and tool pins;
3. the Go modules and `go-selfupdate-lib` ~~v1.23.0~~ v1.23.1 (Deviation 6);
4. the Dart packages, both majors included, and the Android build tools;
5. magic-git's host standard, the audit's held Python line, and the hosts.

## Scope

**In.** The MADR's D2 to D8 and D11 to D14, phase by phase below. Each phase lists the files it
changes.

**Out, by decision:**

* the four agent engine pins (D10). Each gets a record of its own;
* Java (D9). There is no fixed build;
* Git for Windows, gh and glab (D1's scope). They have no advisory;
* magic-git's `build_macos.sh` Flutter pin, its goldens, `standard.json`'s Flutter row and the
  other hosts' Flutter (D2). These go to magic-git's own pair;
* `go-selfupdate-lib`'s new APIs, `Plan`/`Apply` and `servicetest` (D5);
* 0216's P3 to P7, which resume after this plan.

## Constraints

* **C1 — One phase, one commit in this repository.** The agent stages. The owner commits with
  `git commit --no-edit`. Pre-add checks pass first: `make pre-add-check` on staged Go, and
  `dart format` on staged Dart.
* **C2 — The owner's word comes before:**
  * each host run;
  * the emulator;
  * the local runit, s6 and OpenRC containers;
  * the Homebrew upgrades;
  * the Flutter clone in magic-git;
  * a CI dispatch.

  Asked for at approval, per phase.
* **C3 — No push and no tag without an explicit ask in the same turn.** Before asking the owner to
  push, run `leftovers/guard_range.py` over the outgoing commits. The owner pushes and tags (D2).
* **C4 — Every host change has a dated backup first.** The old toolchain stays installed until
  nothing needs it (0169 C4). A Homebrew upgrade runs with `HOMEBREW_NO_INSTALL_CLEANUP=1`, so the
  old keg stays.
* **C5 — Every download is checked** against its publisher's checksum before it is used.
* **C6 — The new check is seen failing first.** Phase 5's held-line logic is the only new check in
  this plan. Its failure is shown on scratch copies, never in magic-git's tree.
* **C7 — Records carry no identifier.** No hostname, account name, real path or address.
  `leftovers/guard_preview.py` runs on every record before it is staged. Fingerprints are recorded
  by their first 8 hex digits only.
* **C8 — A deviation is a stop.** The plan stops, asks with AskUserQuestion, and records the
  answer here before going on. The MADR is amended too, when a fact or a decision changes.
* **C9 — No provider quota.** An engine is only ever run with `--version`.
* **C10 — Edits land exactly.** Each is made by a `leftovers/` Python script that asserts the old
  text occurs exactly once, or with Edit. Each is read back.

## Implementation Steps

### P0 — Before each phase (read only)

1. `git status --short` is clean, apart from this plan's own records. Record `HEAD`.
2. Re-read the phase's targets from their publishers with `leftovers/pins_latest.py`,
   `leftovers/android_bounds.py` and `gh api`. If a newer release has appeared since 2026-10-30,
   stop and ask, for example a Flutter 3.47.10 or a Node 26.13.1.

### P1 — Flutter 3.47.9 (D2), then the release

**Files:** `.github/workflows/ci.yml`, `apps/mobile/README.md`, `apps/mobile/pubspec.lock`, this
plan, the MADR, `docs/README.md` (the index), and the 0169 PLAN (a pointer).

1. **This Mac** (C2): `brew upgrade --cask flutter`.
   * The cask declares `auto_updates`, so it is upgraded only when named.
   * Its archive is `flutter_macos_arm64_3.47.9-stable.zip`, and the cask checks it against
     `6545b859…`.
   * `flutter --version` must then report `Flutter 3.47.9` and `Dart 3.13.7`.
2. **`ci.yml:33`:** `FLUTTER_VERSION: "3.47.9"`.
3. **`apps/mobile/README.md:22`:** "Flutter **3.47.9** / Dart **3.13.7**". That line says 3.47.2
   today, which is two pins out of date.
4. **`flutter pub get`** in `apps/mobile`, on 3.47.9.
   * The lockfile may change only in the packages the SDK pins exactly (`test_api`, `matcher`,
     `meta`, `vector_math` and the like) and in the `sdks` block.
   * Any other package that moves is a stop: phase 4 owns those.
   * The diff is recorded.
5. **Gates on this Mac:**
   * `make preflight`. That includes `assert-flutter-pin.sh` against the new `FLUTTER_VERSION`,
     the pub advisory check, `dart format`, `flutter analyze` and `flutter test`;
   * `make apk`, then `assert-flutter-release-apk.sh` on its output;
   * `make manifest-surface`.
6. **Gates on the test hosts** (C2): at the phase's tree, `go test -race ./...` on the Linux test
   host and `make ci-windows` on the Windows test host. This phase changes no Go. D12 asks for
   these runs on every phase, so they show the hosts are still green.
7. **0169's PLAN:** a "0217 status note" section, added here. Its first line: CI's Flutter is
   3.47.9 under 0217 D2, and the standard's Flutter row moves under magic-git's pair.
8. **Records:** this plan's P1 record; `python3 scripts/check_records.py --write-index`;
   `make check-records`; the guard preview on every staged record. Then stage.

**The release (D2), after the owner's commit:**

1. Run `leftovers/guard_range.py` over `origin/master..HEAD`, then ask the owner to push. The
   unpushed commits are 0216's `07ee60b2` and `533a6055`, and this phase's.
2. **The push run:** every job the push triggers must pass. `android-apk` runs only on a tag or a
   dispatch (`ci.yml:459`).
3. **The tag** is the owner's.
   * Proposed: **`v0.24.0`**. Since `v0.23.0` there are `feat` commits, 0216's two among them.
   * It is annotated, as `v0.23.0` is.
   * The owner may name another version.
4. **The tag run must show:**
   * `android-apk` setting up Flutter 3.47.9;
   * its release-mode and signer checks passing;
   * `magic-cli-remote-v0.24.0-arm64.apk` published with the release.

   Record the run's id and the result of each job.

**Verification:** step 5's gates exit 0, step 6's hosts are green, and the tag run is green.

**Rollback:**

* Revert the phase commit.
* On this Mac, put back the 3.47.5 release archive at `/opt/homebrew/share/flutter`, checked
  against the release feed's hash.
* A published release is not withdrawn. The next release supersedes it.

### P2 — Node 26, the CI actions and the tool pins (D3, D6)

**Files:** `.github/workflows/ci.yml`, `.github/workflows/docs.yml`,
`.github/workflows/ci-flake-ledger.yml`, `.node-version`, `.nvmrc`, `README.md`,
`scripts/vulncheck.sh`, `Makefile`, this plan, and the 0169 PLAN.

1. **Re-peel every action tag** with `gh api` (a tag object is followed to its commit). Each must
   equal the commit recorded here, or the phase stops:

   | Action | Tag | Commit |
   | :--- | :--- | :--- |
   | `actions/setup-node` | v7.1.0 | `949feb2413d6458794dcd2491c4babbbce0c15c1` |
   | `actions/setup-java` | v6.1.0 | `b843ebf9b913146b38da2eaa7aeddf76074ae1ad` |
   | `actions/upload-artifact` | v7.0.2 | `cf430e030ddbb5b0abf93d22962f4752f3646cd9` |
   | `actions/download-artifact` | v8.0.2 | `9000827ccba6bdab643e8b6fd33ac0654aef8333` |
   | `subosito/flutter-action` | v2.23.0 | `1a449444c387b1966244ae4d4f8c696479add0b2` (unchanged) |

2. **The edits** (C10):
   * `ci.yml:29` and `docs.yml:22`: `NODE_VERSION: "26"`;
   * `.node-version` and `.nvmrc`: `26`;
   * `README.md:1479-1480`: "CI pins **Node.js 26 LTS** via `actions/setup-node`, `.node-version` and
     `.nvmrc`. JavaScript actions themselves run on Node 24 (`FORCE_JAVASCRIPT_ACTIONS_TO_NODE24`).";
   * `ci.yml:69` and `docs.yml:39`: `setup-node@949feb24… # v7.1.0`;
   * `ci.yml:471`: `setup-java@b843ebf9… # v6.1.0`;
   * `ci.yml:306` and `:714`: `upload-artifact@cf430e03… # v7.0.2`;
   * `ci-flake-ledger.yml:98`: `download-artifact@9000827c… # v8.0.2`;
   * `ci.yml:364`, `:431` and `:477`: the comment becomes `# v2.23.0`;
   * `ci.yml:127` and `scripts/vulncheck.sh:31`: `govulncheck@v1.9.0`;
   * `Makefile:315`: `xargs -0 npx --yes markdownlint-cli2@0.24.0 --no-globs`.

   `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24` (`ci.yml:28`) and its comment at `:25` stay. They set
   the runtime the actions run on, not the toolchain `setup-node` installs.
3. **This Mac's `govulncheck`** goes from v1.8.0 to v1.9.0:
   `go install golang.org/x/vuln/cmd/govulncheck@v1.9.0`. It is the local consumer of the pin
   that `make vulncheck` and `make pre-add-check` run. Rollback: `@v1.8.0`.
4. **Gates on this Mac:**
   * `make vulncheck` prints `vulncheck: clear; allowlisted required-module findings:
     GO-2026-5932`;
   * `make markdownlint-docs` prints `markdownlint-cli2 v0.24.0` and finds no issue;
   * `make preflight`.
5. **Gates on the test hosts** (C2): the Linux test host runs `go test -race ./...` and
   `make markdownlint-docs`, which fetches 0.24.0 through `npx` under that host's Node 24. The
   Windows test host runs `make ci-windows`.
6. **0169's PLAN:** the status note gains a line saying CI's Node is 26, which closes the CI half
   of P6b. The hosts' half is phase 5.
7. **Records and staging,** as in P1 step 8.
8. **After the owner's push:** CI's `setup-node` step resolves a 26.x, and the Docs workflow
   passes. Record the run ids.

**Verification:** every `uses:` names a peeled release commit and its exact version. Steps 4, 5
and 8 pass.

**Rollback:** revert the phase commit, and reinstall `govulncheck@v1.8.0` on this Mac.

### P3 — Go modules and `go-selfupdate-lib` v1.23.1 (D4, D5)

*Amended 2026-10-30 by Deviation 6: the target was v1.23.0. Amended again by Deviation 7:
`acp-go-sdk2` moves to v1.27.0. The steps below strike the old values.*

**Files:** `go.mod`, `go.sum`, `.github/workflows/ci.yml`, the 0207 MADR (amendment J), and this
plan.

1. `go get -u ./...`, then `go mod tidy`. Then:
   * `go.mod` still says `go 1.27.3`, and has gained no `toolchain` line. A raised directive is a
     stop, because it means some dependency needs a newer Go;
   * `go-selfupdate-lib` is ~~v1.23.0~~ v1.23.1, `acp-go-sdk2` ~~v1.26.0~~ v1.27.0, `certmagic` v0.25.6 and
     `x/mod` v0.42.0. `x/sys` stays at v0.49.0 or newer and `x/term` at v0.47.0 or newer, the
     floors v1.23.1 requires (F3a);
   * `go list -m -u` shows no newer version for any direct module.

   The diff of `go.mod`'s `require` blocks is recorded, indirect modules included.
2. **`ci.yml:743` and `:764`:**
   ~~`@67520be8af1ad1b6228c29e4b1aa3e61e1ca7ae4 # go-selfupdate-lib v1.23.0`. That is v1.23.0's
   commit, peeled through its annotated tag `e28ca1a1…`; re-peel it at P0.~~
   `@c24f5919cd9e9e3ba3736e64bcf18a6e2a51489e # go-selfupdate-lib v1.23.1`. That is v1.23.1's
   commit, peeled through its annotated tag `b0c08a0e…`; re-peel it at P0
   (`leftovers/gsl_peel.py v1.23.0 v1.23.1`).
   `TestWorkflowPinsMatchGoMod` (`internal/updateclient/spec_test.go:138`) holds both pins to
   `go.mod`.
3. **Read every caller of the changed behaviour** (F3). For each, record what v1.23.0 now returns
   (v1.23.1 carries it unchanged, F3a) and what Magic does with it:
   * `internal/cli/service/setup.go:203` and its `ErrNotInstalled` case at `:243`;
   * `internal/cli/service/installerhook.go:182`;
   * the supervised backends' state and tool paths, `supervisorstate.go:94`, `:206` and
     `supervisortool.go:209`, `:227`;
   * launchd's missing-property-list `Start`, and `Stop`'s shared bound, wherever
     `internal/cli/service` and `internal/updateclient` start or stop through launchd;
   * systemd's `Type=notify` start that now also wraps `ErrUnhealthy`. Both of Magic's user units
     are `Type=notify` (`mcremote.user.service.tmpl:23`, `mcrelay.user.service.tmpl:20`).

   A caller that would now report wrongly is a stop (C8). It is not patched under this phase.
4. **Gates on this Mac:**
   * `make pre-add-check` over every tracked Go file;
   * `go vet ./...` for darwin/arm64, windows/amd64 and linux/amd64 with cgo off;
   * `make staticcheck`;
   * `make vulncheck`;
   * `make race`;
   * `make preflight`;
   * `make check-libs`, when go-shared is checked out beside this repository; otherwise its absence
     is recorded;
   * `make live-supervisors KINDS="runit s6 openrc"` (C2). Podman is on this Mac. It installs and
     drives scratch services inside containers only.
5. **Gates on the test hosts** (C2): the Linux test host runs `go test -race ./...`, and
   `go test -count=1 ./internal/cli/service/... ./internal/updateclient/...`. The Windows test host
   runs `make ci-windows`.
6. **0207, amendment J:** ~~"`go-selfupdate-lib v1.23.0`"~~ "`go-selfupdate-lib v1.23.1`". It
   records:
   * F3's changes, and F3a's: that v1.23.1 adds only the `golang.org/x` floors;
   * F3b: `acp-go-sdk2` v1.27.0, which 0207's amendment I pinned at v1.26.0, and what it adds;
   * step 3's reading of each caller;
   * step 4's and step 5's results;
   * that `Plan`, `Apply` and `servicetest` are not adopted (D5).
7. **Records and staging,** as in P1 step 8. The phase stages no `.go` file. If step 3 finds a
   stop and the owner resolves it with a code change, that change is an amendment to this plan,
   with its own pre-add checks.
8. **After the owner's push:** `Live supervisors (runit, s6)` and every Go job pass. Record the run
   id.

**Verification:** steps 1, 4, 5 and 8 pass, and `TestWorkflowPinsMatchGoMod` passes.

**Rollback:** revert the phase commit. That puts `go.mod`, `go.sum` and both workflow pins back
together.

### P4 — Dart packages and the Android build tools (D7, D8)

**Files:** `apps/mobile/pubspec.yaml`, `apps/mobile/pubspec.lock`,
`apps/mobile/android/gradle/wrapper/gradle-wrapper.properties`,
`apps/mobile/android/settings.gradle.kts`, and this plan.

1. **The baseline APK.** ~~At the P3 commit, run `make apk` and copy
   `build/app/outputs/flutter-apk/app-release.apk` to `leftovers/0217-p4/baseline.apk`. Without
   `key.properties` it is signed with this Mac's debug key, as the phase's APK will be, so one can
   be installed over the other.~~ *Amended 2026-10-30 by Deviation 8:* `key.properties` exists, so
   `make apk` is signed with the upload key, and the app on the AVD with this Mac's debug key. The
   emulator proof uses debug builds: at the P3 commit, `flutter build apk --debug
   --target-platform android-arm64`, copied to `leftovers/0217-p4/baseline-debug.apk`. The
   release `make apk` at the P3 commit is kept as `leftovers/0217-p4/baseline.apk`. The AVD
   `mcremote_test` is `arm64-v8a`, which matches `--target-platform android-arm64`.
2. **The emulator baseline** (C2):
   * Boot the AVD.
   * Back up the app's data to `leftovers/0217-p4/appdata-before.tar`, with
     `adb exec-out run-as com.maccavelli.magic_cli_remote tar -cf - .`.
   * `adb install -r` ~~`baseline.apk`~~ `baseline-debug.apk` (Deviation 8).
   * The app must connect to its paired daemon. If it is not paired, pair it by Paste URI against
     the installed daemon. That is an ask, because the app keeps one host.
   * Capture Settings from the host side (`adb emu screenrecord screenshot`). Record the
     **Certificate pin** fingerprint and the **Client identity** fingerprint
     (`settings_screen.dart:1319-1350`) by their first 8 hex digits (C7).
3. **The two majors:** `flutter pub upgrade --major-versions cupertino_icons flutter_secure_storage`.
   * `pubspec.yaml` must then read exactly `cupertino_icons: ^2.0.0` (line 17) and
     `flutter_secure_storage: ^11.2.0` (line 21), and no other constraint may have changed.
   * Then `flutter pub upgrade` for the in-range moves.
   * `flutter pub outdated --json` must show no direct dependency with a newer resolvable version.
     Its transitive packages are recorded.
4. **Re-read Flutter's Android bounds** in the 3.47.9 SDK's `gradle_utils.dart`. They must be as
   F9 found them at 3.47.5:
   * `maxKnownAndSupportedGradleVersion` 9.3.1;
   * `maxKnownAndSupportedKgpVersion` 2.4.0;
   * `maxKnownAgpVersionWithFullKotlinSupport` 9.1.0;
   * AGP 9.1.x requires Gradle from 9.3.1 to 9.3.1 (`:825-826`).

   A different bound is a stop.
5. **The build tools,** all in this one step, because AGP 9.1.x needs Gradle 9.3.1 exactly:
   * `gradle-wrapper.properties:5`: `gradle-9.3.1-all.zip`;
   * `settings.gradle.kts:22`: `com.android.application` `"9.1.1"`;
   * `settings.gradle.kts:23`: `org.jetbrains.kotlin.android` `"2.4.0"`.

   `app/build.gradle.kts` is unchanged. Its `VERSION_17` and `JVM_17` lines (`:49-50`, `:106`)
   are 0168 D2's bytecode target. `minSdk = flutter.minSdkVersion` is 24 (`FlutterExtension.kt:26`),
   which is `flutter_secure_storage` 11's minimum.
6. **Gates on this Mac:**
   * `make preflight`;
   * `make apk`, with its output saved. The output must carry no Flutter warning about the AGP,
     Gradle or Kotlin version (`grep -i "flutter support for your project"` gives no match), and
     `assert-flutter-release-apk.sh` must pass;
   * `make manifest-surface`, which must pass against `manifest-surface.allow`. A plugin that adds
     a permission is a stop.
7. **The emulator upgrade** (C2), the D7 proof:
   * `adb install -r` this phase's ~~APK~~ debug APK, built as step 1's (Deviation 8). `-r` keeps
     the app's data.
   * Launch it. It reconnects with no re-pair.
   * Settings shows the same Certificate pin and Client identity fingerprints as step 2.
   * A re-pair prompt, a changed fingerprint, or a missing identity is a stop (MADR D7). If that
     happens, restore step 2's backup before anything else.
8. **Gates on the test hosts** (C2): as in P1 step 6. This phase changes no Go either.
9. **Records and staging,** as in P1 step 8. `dart format` passes on every `.dart` file, and this
   phase changes none.

**Verification:** steps 3, 4, 6 and 7 pass.

**Rollback:**

* Revert the phase commit.
* Reinstall `baseline.apk` on the AVD with `adb install -r -d`. `-d` allows the version downgrade,
  if the build numbers differ.
* If the store was reset, restore step 2's backup.

### P5 — The host standard, the held line, and the hosts (D11, D13, D14)

**Files in magic-git** (the owner commits there): `scripts/tools/devenv/version_audit.py`,
`scripts/tools/devenv/standard.json`, `scripts/tools/devenv/README.md`.

**Files in the dotfiles repository** (the owner commits there): `00-paths.sh` and the `mcremote`
drop-in `path.conf`, both links into it on the Linux test host (0169 P14).

**Files here:** this plan, and the 0169 PLAN and MADR (pointers).

#### P5a — the audit learns a held line (D13)

1. **magic-git's Flutter** (C2). Clone 3.47.5 with `build_macos.sh`'s own command:
   `git clone --depth 1 --branch 3.47.5 https://github.com/flutter/flutter.git .flutter-sdk`. It is
   gitignored (`.gitignore:53`). Then `./.flutter-sdk/bin/flutter --version` must report 3.47.5.
   This Mac's PATH Flutter is 3.47.9 by then (the MADR's correction to D2).
2. **`version_audit.py`:**
   * `HELD` is added to the states. It is not in `FAIL_STATES` (`:46`), nor is it `ERROR` or
     `NO-FIX`, so `exit_code` (`:61-69`) is unchanged and ignores it.
   * `s_python` (`:569-578`) reads `held_line` from the standard's `python` entry. When it is
     present:
     * `ERROR` if `held_line` is not equal to `line`, or the version is not in it;
     * `UNSUPPORTED` if the held cycle's `eol` is on or before `--today`;
     * `STALE` against that cycle's `latest`, when it is newer;
     * `HELD`, naming the newest supported line and its `latest`, when that line is newer than the
       held one.
   * With no `held_line`, `s_python` behaves exactly as today.
   * The advisory check (`python_advisories`, `:433-437`) is not changed.
   * The script stays stdlib-only and Python 3.9-compatible (its docstring, `:3`).
3. **The devenv `README.md`:** a `HELD` row in the states table (`:83-92`), with exit "none". A
   paragraph says what `held_line` and `held_reason` mean, and that a hold ends at its line's end of
   life.
4. **Seen failing first** (C6). Each case runs on a scratch copy of the standard, through
   `leftovers/held_line_plants.py`. The standard's other rows (Flutter, Java, Git for Windows, gh,
   glab) fail on every run, so the exit status says nothing about Python. Each case is therefore
   judged by the `standard  python` rows it prints:

   | Plant | Expected Python rows |
   | :--- | :--- |
   | `held_line: "3.14"`, version 3.14.9 | `STALE ... 3.14.9 -> 3.14.10` (and that version's advisories, if any) |
   | `held_line: "3.14"`, version 3.14.10, `--today 2030-11-01` | `UNSUPPORTED` |
   | `held_line: "3.13"`, `line: "3.14"` | `ERROR` |
   | no `held_line`, version 3.14.10 | `STALE ... 3.14.10 -> 3.15.2`, as before the change |
   | `held_line: "3.14"`, version 3.14.10 | `HELD ... 3.15 (3.15.2)` only |
   | the last row, against the script **before** the change (a scratch copy) | `STALE ... -> 3.15.2`. This shows the plants can tell the two apart |

   The exit status is checked on its own. A `Report` holding one `HELD` row returns 0 from
   `exit_code`, and one holding `HELD` and `STALE` returns 1. Every case also runs under
   `/usr/bin/python3` (3.9), and must print the same rows.
5. **magic-git's gates,** before staging there:
   * `python3 dependencies.py`;
   * with `PATH` led by `.flutter-sdk/bin`: `flutter pub get --enforce-lockfile` ("Got
     dependencies!"), `flutter analyze` and `flutter test`;
   * `dart run scripts/tools/records.dart check`.

#### P5b — `standard.json` (D11, D13)

1. Set the rows (C10):
   * `go.version` `1.27.3`;
   * `node.version` `26.13.0`, `node.line` `"26"`. The note says: "The newest Active LTS line, 26
     (Lithium), since 2026-10-28 (magic-cli-remote MADR 0169 D4, 0217 D11)";
   * `python.version` `3.14.10`, `python.held_line` `"3.14"`, and `python.held_reason`: "The
     owner holds 3.14 (magic-cli-remote MADR 0217 D13); 3.15 is not adopted". The old note ("No
     3.14 release is advisory-free; adopt 3.14.8…") is replaced: 3.14.10 has no advisory (F15);
   * each moved row's `verified` set to the day it is checked.
2. Run the audit against the file with no probes. It must show:
   * no Go or Node row;
   * Python as `HELD` only;
   * `STALE` and `ADVISORY` only for Flutter, Java, Git for Windows, gh and glab, and `REVIEW` only
     for Dart's GHSA-frr6-h895-5pxp.

   Each of those rows is named in the MADR.
3. Stage the three magic-git files there, for the owner's commit.

#### P5c — the hosts (D14)

One host at a time, each through a `leftovers/` Python script run on that host (C2, C4, C5). Before
anything moves, read the host: `go version`, `node --version`, `npm ls -g --depth=0` with each
global package's `--version`, the developer Python's `--version`, and, where one exists,
`~/default-venv`'s `pip freeze`. Save them beside the backup. Go moves nowhere, and must read
`go1.27.3`.

| Host | Node 26.13.0 | Python 3.14.10 | Backed up first |
| :--- | :--- | :--- | :--- |
| This Mac | `brew upgrade --dry-run node python@3.14` is read first, and any unrelated formula it would move to a new major is a stop. Then `HOMEBREW_NO_INSTALL_CLEANUP=1 brew upgrade node python@3.14` | the same command | the `brew list --versions` output; the old kegs stay (C4) |
| Linux test host | `node-v26.13.0-linux-x64.tar.xz` (`d28a96af…`) to `~/sdk/node-v26.13.0`. PATH is swapped in the dotfiles' `00-paths.sh` and in the drop-in, then `systemctl --user daemon-reload` | `cpython-3.14.10+20261026-x86_64-unknown-linux-gnu-install_only.tar.gz`, checked against its GitHub asset digest, to `~/sdk/python-3.14.10`. `~/default-venv` is rebuilt on it from the saved freeze; the old venv is kept as `.bak` until the checks pass | `00-paths.sh`, `path.conf`, the freeze |
| WSL distro | the same archive, to `~/sdk/node-v26.13.0`. PATH is swapped in `~/.config/devenv.sh` | the same archive and venv rebuild as the Linux host | `devenv.sh`, the freeze |
| Windows test host | `node-v26.13.0-x64.msi` (`0e5ee99c…`). The MSI replaces 24.21.0 in place, so the 24.21.0 MSI is kept in the staging folder for the rollback | `py install --update 3.14` | the User Path value and its kind, and the `py list` output |

After each host, in a fresh login shell (on Windows, after the owner refreshes the ssh session's
environment, as P14 found):

* `node --version` is `v26.13.0`;
* the developer `python3` (`python` on Windows) is `3.14.10`, and on Linux and WSL `/usr/bin/python3`
  is still the distribution's;
* every npm global package answers `--version` as before. A package that fails for Node 26's ABI is
  a stop;
* on the Linux test host, the `mcremote` unit's PATH names `~/sdk/node-v26.13.0/bin`. The running
  daemon takes it at its next restart, which is the owner's.

The owner commits the dotfiles change there.

#### P5d — the audit over every host

1. Run `run_probe.py` over this Mac, the Linux and Windows test hosts and the WSL distro, as 0169 ran
   it, then `version_audit.py` against the moved standard.
2. Expected: the rows of P5b step 2, plus `DRIFT` for this Mac's Flutter (3.47.9 against the
   standard's 3.47.5) until magic-git's pair. No other row.
3. **0169's records:**
   * the PLAN's status note: P6b done on the hosts, Python's "3.14.8 when it ships" closed by
     3.14.10, and P12 step 3 and P14 step 4 closed by P5b;
   * the MADR: a pointer under D1 and D6 saying 0217 D13 holds Python on 3.14, where D1 alone would
     take 3.15.
4. **Records and staging here,** as in P1 step 8.

**Verification:** P5a step 4's plants behave as the table says. P5a step 5's gates pass. P5b step 2
and P5d step 2 show only the rows named.

**Rollback:**

* magic-git: the owner reverts the commit there. With no `held_line` key, the audit behaves as
  before.
* Each host: restore its backup, and point PATH back at the old `~/sdk` directory. That is a pointer
  change, not a reinstall, except:
  * the Windows test host: the 24.21.0 MSI from the staging folder, and the install manager's
    3.14.7;
  * this Mac: `brew unlink` the new keg and `brew link` the kept one.

## Verification

### Acceptance criteria (mapped to the MADR's Confirmation)

| # | Criterion | MADR |
| :--- | :--- | :--- |
| A1 | This Mac's Flutter is 3.47.9 with Dart 3.13.7. `make preflight` passes, and CI's Flutter jobs run 3.47.9 | D2 |
| A2 | The tag run for the release is green, and its APK is built on Flutter 3.47.9 | D2 |
| A3 | CI's `setup-node` resolves 26.x, and `make markdownlint-docs` passes there and here | D3 |
| A4 | Every `uses:` names a peeled release commit with its exact version. `make markdownlint-docs` prints v0.24.0. `govulncheck` is v1.9.0 in CI and on this Mac | D6 |
| A5 | No direct Go module is behind. `make vulncheck` is clear. `TestWorkflowPinsMatchGoMod`, `make race`, the live supervisors (here and in CI), the Linux test host and `make ci-windows` pass. 0207 amendment J is written | D4, D5 |
| A6 | `flutter pub outdated` shows no direct dependency behind. The APK build shows no AGP, Gradle or Kotlin warning | D7, D8 |
| A7 | On the emulator, a pairing made with the P3 APK survives the P4 APK: no re-pair, and both fingerprints unchanged | D7 |
| A8 | The held-line plants were each seen to do what the table says, the pre-change copy included | D13 |
| A9 | `standard.json` says Go 1.27.3, Node 26.13.0 and Python 3.14.10 held on 3.14, and the audit on it shows only the rows named | D11, D13 |
| A10 | Every host reads go1.27.3, Node v26.13.0 and Python 3.14.10, and its npm globals still answer. The audit over all four shows only the rows named. *Amended by Deviation 13:* its Go tools are built with go1.27.3 and no old Go install is left; the named rows include each host's Flutter, Dart, Java and git rows | D14 |
| A11 | Every host change was made with a backup and the owner's word. *Except, by Deviation 11:* this Mac's `node 26.10.0_1` keg was autoremoved; its cached bottle is the rollback | C2, C4 |

The criterion most likely to be dropped quietly is **A7**. Every gate before it passes whether or
not `flutter_secure_storage` 11 can read the store. Only installing over a paired app shows it.

## Rollout and Rollback

* Phases land in order, one commit each.
* Phase 1 ships first, as a release.
* Phase 5 can run at any point after phase 1. It is last so that nothing holds the release.
* Each phase's rollback is a revert of its commit, plus the host or emulator restore named in it.
* Old toolchains stay installed until nothing needs them (C4). Deleting them is a later, separate
  ask.

## Owner decisions needed at approval

1. **The host runs** (C2): the Linux and Windows test hosts at each phase, and in P5c the four hosts'
   installs.
2. **The emulator**, in P4.
3. **The local runit, s6 and OpenRC containers**, in P3.
4. **The Homebrew upgrades on this Mac:** Flutter in P1, Node and Python in P5c.
5. **The Flutter 3.47.5 clone in magic-git**, in P5a, about 1.7 GB.
6. **The release version**, in P1: `v0.24.0` is proposed.

## Not done by this plan

* The engine pins (D10), Java (D9), Git for Windows, gh and glab (D1), and magic-git's Flutter
  lockstep (D2). Each is named in the MADR.
* Removing the old toolchains: `~/sdk/node-v24.21.0` and `~/sdk/python-3.14.7`, Homebrew's old kegs,
  and `go1.27.2` (0169 C4).
* Adopting `go-selfupdate-lib`'s new APIs (D5).
* magic-git's file-watcher tests, which fail on this Mac at magic-git's own `HEAD` (Deviation 9).
  They are an item for a MADR/PLAN in magic-git.

## Execution record

**Approval, 2026-10-30.** The owner wrote "accept the madr. plan approved to proceed". Asked
whether that covers C2, the owner answered "Yes, all C2 items (Recommended)". Pushes, tags and the
release version still need an explicit ask in the same turn (C3).

### P1 — Flutter 3.47.9

**P0.**

* The tree held only this pair and the index. `HEAD` was `533a6055`.
* Flutter's release feed still names 3.47.9 (Dart 3.13.7, 2026-10-29) as the current stable.
  Its arm64 archive's sha256, `6545b859…`, is the cask's.

**Step 1.** `brew upgrade --cask flutter` reported "3.47.5 -> 3.47.9". `flutter --version` then
gave `Flutter 3.47.9`, framework `c197ca5199`, engine `9e025265`, and `Dart 3.13.7`.

#### Deviation 1 (2026-10-30): the Flutter upgrade ran without `HOMEBREW_NO_INSTALL_CLEANUP=1`

**Found** in step 1's output. C4 says a Homebrew upgrade runs with
`HOMEBREW_NO_INSTALL_CLEANUP=1`. Step 1 as written omitted it, and it was run as written. Homebrew
then ran its 30-day `brew cleanup` as well, which removed:

* the old `pcre2` 10.48 keg. `pcre2` 10.49 stays linked, and its dependents still run: Homebrew's
  `git` (2.56.0), `nmap` (7.991), `gs` (10.08.0) and `magick` (7.1.2-32) each answer their
  version flag;
* download caches, about 3.9 GB, including the Flutter 3.47.5 zip and old bootsnap caches;
* empty Homebrew logs, and a temporary `openssl@3` directory.

The cask also purged its own 3.47.5 copy, as any cask upgrade does. Nothing in use was lost.
P1's rollback still works, because it fetches the 3.47.5 archive from the release feed and checks
it against the feed's hash.

**Decision (owner, 2026-10-30): "Record and continue (Recommended)".** Declined: re-caching the
3.47.5 archive, and stopping to roll back.

**Scope.** P5c's Homebrew upgrades run through a `leftovers/` script. It sets
`HOMEBREW_NO_INSTALL_CLEANUP=1` in the environment it passes to `brew`, and asserts that the
variable is there first. No file is added to the phase.

#### Deviation 2 (2026-10-30): a record edit ran shell commands

**Found** while writing Deviation 1. The record text was passed inline to `python3 -c`, inside
double quotes. That broke the standing rule that scripts are Python files in `leftovers/`. The
shell ran every backticked span in the text as a command substitution before Python started:

* **Harmless:** commands that do not exist (commit ids, hashes, `pcre2`, `openssl@3`);
  `brew upgrade --cask flutter` ("Not upgrading flutter, the latest version is already
  installed"); `flutter --version`; `flutter`, `dart`, `git`, `nmap`, `gs` and `magick` with no
  arguments, or with a version string as their argument; `brew` alone; and a variable assignment
  in a subshell.
* **Not harmless:** `brew cleanup`, a second time. Its stdout was swallowed by the substitution,
  so what it removed is not known. Its stderr listed only "Skipping … most recent version … not
  installed" warnings.

Python then stopped on a syntax error, so nothing was written.

**Re-verified afterwards:**

* `brew list --versions` gives `node 26.10.0_1`, `node@24 24.21.0`,
  `python@3.14 3.14.7 3.14.8_2` and `pcre2 10.49`. That is every keg as it was before P1;
* the Flutter cask is at 3.47.9;
* `go`, `node` and `python3` resolve and report go1.27.3, v26.10.0 and 3.14.8, as before;
* the working tree is unchanged.

**Decision (owner, 2026-10-30): "Record both, continue (Recommended)".** Declined: pausing for the
owner to inspect Homebrew, and stopping P1.

**Scope.** From here on, record text goes only through Edit or Write, and every script through a
`.py` file in `leftovers/`. No inline `-c` code is used. No file is added to the phase.

**Steps 2 to 4.**

* `ci.yml:33` now reads `FLUTTER_VERSION: "3.47.9"`.
* `apps/mobile/README.md:22` now reads "Flutter **3.47.9** / Dart **3.13.7**".
* `flutter pub get` on 3.47.9 left `pubspec.lock` byte-identical. 3.47.9 pins the same SDK
  packages as 3.47.5.
* The same run listed `cupertino_ui 1.0.1` in the graph already, which corrected the MADR's F8b.

**Step 5, this Mac** (logs in `leftovers/0217-p1-*.log`):

| Gate | Result |
| :--- | :--- |
| `make preflight` | exit 0, "✅ preflight passed". The sections in order: check-records; markdownlint-docs (`markdownlint-cli2 v0.24.0`, which the floating `npx` fetched); gofmt; `go mod tidy`; vet; staticcheck v0.8.2; `go test -race`; the install and restart tests; the release build; "assert-flutter-pin: OK local and pinned Flutter agree (3.47.9)"; "pub-advisories: 158 hosted packages, no advisories."; `dart format` "(0 changed)"; `flutter analyze` "No issues found!"; `flutter test` "+1501 ~3: All tests passed!" |
| `make apk` | exit 0. "✓ Built build/app/outputs/flutter-apk/app-release.apk (41.2MB)", and "assert-flutter-release-apk: OK release-mode APK". The build is unstamped, because the tree is dirty |
| `make manifest-surface` | exit 0, "assert-android-manifest-surface: OK surface matches the allowlist" |

**Observed, for P4.** Both builds warn: "Your app uses the following plugins that apply Kotlin
Gradle Plugin (KGP): mobile_scanner, speech_to_text. Future versions of Flutter will fail to
build…". This is not the AGP, Gradle or Kotlin version warning that P4 step 6 checks for. P4
records whether the plugins' upgrades (`mobile_scanner` 7.4.2, `speech_to_text` 7.5.0) clear it.
Gradle 9.1.0 also prints JDK 25's native-access warnings, which are harmless.

**Step 6, the test hosts** (`leftovers/pins_hosts.py`, `PINS_PHASE=p1`; each a scratch git tree of
HEAD plus the three changed files, removed afterwards):

* **The Linux test host:** `go version go1.27.3 linux/amd64`, and `go test -race -count=1 ./...`
  exit 0, with 45 `ok` lines.
* **The Windows test host:** `go version go1.27.3 windows/amd64`. `make ci-windows` exited 2:
  A3, A2, A4 and A5 passed, and A6 (`go test ./...`) failed in `internal/session`. The checks after
  A6 did not run:

  ```text
  --- FAIL: TestPromptFailsAfterDisconnectAutoClose (0.00s)
      manager_lifecycle_test.go:176: want ErrNotLive, got session closed
  ```

#### Deviation 3 (2026-10-30): the Windows gate fails on a race in `Manager.Prompt`

**Found** at step 6. It is pre-existing:

* P1 changes no Go file;
* `ci-flakes.tsv:21` records the same test failing in Windows CI on 2026-10-24 (run
  `40113049697`, commit `7d2a3ecf`), and passing on rerun.

**The cause, read from the code:**

1. `Manager.Prompt` takes the live session (`internal/session/manager.go:2712`), then calls
   `sess.Prompt` (`:2730`).
2. When the provider reports a disconnect, the auto-close runs `closeMatching`. That deletes the
   entry under `m.mu` (`:3494`) and then calls `Close` (`:3515`).
3. A `Prompt` that took the session before step 2's delete, and calls it after `Close`, returns the
   provider's own error (`"session closed"`, from the test's fake at
   `manager_lifecycle_test.go:35`). The manager's contract is `ErrNotLive`.

The log shows that order: "session closed" is logged before "session auto-closed". The test's
tight prompt loop (`:171-176`) finds the window, and Windows' scheduler finds it more often. It is
a product race, not a test artefact.

**Decision (owner, 2026-10-30): "Own pair, fix first (Recommended)".** A new MADR/PLAN pair
fixes the race at its root:

* a deterministic test, seen failing first on a scratch copy;
* the fix, landed as its own commit before P1's;
* then P1's full `make ci-windows` again.

Declined: folding the fix into P1, and pausing P1.

**Scope.** P1 is held after step 5 and step 6's Linux half. No file is added to P1. The
release waits for the new pair,
[0218-MADR-report-not-live-when-a-session-closes-during-a-call.md](0218-MADR-report-not-live-when-a-session-closes-during-a-call.md)
and
[0218-PLAN-report-not-live-when-a-session-closes-during-a-call.md](0218-PLAN-report-not-live-when-a-session-closes-during-a-call.md).
P1's step 8 regenerates the index, which also adds 0218's rows.

**Resolved.** 0218 was executed and committed as `1a6abb37` ("fix(session): report ErrNotLive
when session closes during a call"). Its PLAN records the fix and its gates.

**Step 6, again, at `1a6abb37`** plus P1's four changed files (`leftovers/pins_hosts.py`,
`PINS_PHASE=p1b`):

* **the Linux test host:** `go1.27.3 linux/amd64`, and `go test -race -count=1 ./...` exit 0, with
  45 `ok` lines;
* **the Windows test host:** `go1.27.3 windows/amd64`, and `make ci-windows` exit 0,
  "ci-windows-local: ALL SELECTED CHECKS PASSED", with 45 `ok` lines.

Step 5's gates on this Mac were run at `533a6055` plus P1's files. `1a6abb37` changes only
`internal/session`, and 0218's own gates covered it (`make race`, vet, staticcheck, both hosts),
so step 5 is not repeated.

**Step 7.** 0169's PLAN has a new "0217 status note" section, with the P1 line.

**Step 8.**

* The index was regenerated with `check_records.py --write-index`. It now has rows for the 0217
  and 0218 pairs.
* `make check-records` and the guard preview pass.
* `apps/mobile/pubspec.lock` is unchanged (step 4), so it is not staged.

**Files staged for the P1 commit:**

* `.github/workflows/ci.yml`;
* `apps/mobile/README.md`;
* `docs/README.md`;
* the 0169 PLAN;
* this pair.

**Committed** as `a5ccd412` ("chore(deps): update pins to current releases, fixing Flutter
advisory"). `leftovers/guard_range.py` over `origin/master..HEAD` (`0e84b1f0..a5ccd412`: 0216's
`07ee60b2` and `533a6055`, 0218's `1a6abb37`, and `a5ccd412`) gave exit 0, with no output. The push
and the tag are the owner's (C3).

**The release** (2026-10-30, after P2's step 8 below). The owner pushed the annotated tag
**`v0.24.0`** (tag object `1a1f431c`) on `5ca4a3b1`, the commit P2's step 8 shows green on master.

Its run is CI `41094433851`.

* **Attempt 1: failure, from GitHub runner capacity.**
  * Every gate job passed: Go (test), Go (windows/amd64), Go (darwin/arm64), Go (linux/arm64),
    Flutter (analyze & test), Flutter (test on windows), and Live supervisors (runit, s6).
  * Android APK (release arm64) passed, as did the self-update build and its six Linux and
    Windows identity jobs.
  * The two darwin/arm64 identity jobs (`mcrelay` and `mcremote`, `macos-15`) were cancelled
    after 15m1s with no step run. Each one's annotation reads "The job was not acquired by
    Runner of type hosted even after multiple attempts". The run carried GitHub's notice that
    "jobs targeting macOS arm64 runners may experience longer queue times".
  * Publish was skipped, so no release existed.
* **Attempt 2: success.** Under C2's dispatch, `gh run rerun --failed` re-ran the two identity
  jobs, which passed, and then the publish they gate.
  * `gh run watch` twice exited on an HTTP 502 from the jobs API while the run went on.
    `leftovers/wait_run.py` waits through those errors.
  * Each job's latest result was read from the check-runs API, because the jobs API kept
    returning 502. Every job is `success` except "Publish self-update release / sign", which is
    `skipped`, as it is on `v0.23.0`.

**Step 4's checks, from the Android APK job's log (`124201755019`):**

* Flutter is set up from `stable-3.47.9-x64` at framework `c197ca5199`, and the build prints
  "✓ Built build/app/outputs/flutter-apk/app-release.apk (41.2MB)".
* "Assert Flutter release mode" passes ("android:debuggable is not set (release-compatible)").
* "Assert release signature" passes, with "signer digest matches pinned certificate".

**The release `v0.24.0`** was published at 2026-10-30T17:37:09Z (not a draft, not a
prerelease). It has 12 assets:

* `magic-cli-remote-v0.24.0-arm64.apk` (41,222,202 bytes);
* the eight `mcremote` and `mcrelay` binaries;
* `install.sh`, `install.ps1` and `SHA256SUMS`.

`SHA256SUMS` lists the eight binaries and not the APK. `v0.23.0`'s has the same eight entries, so
this is not a change.

P1's verification is met, and with it **A1** and **A2**.

### P2 — Node 26, the CI actions and the tool pins

**P0.** The tree was clean at `a5ccd412`.

* `leftovers/peel_tags.py` peeled every pinned and target tag to its commit. Each matched step 1's
  table, and each target is its repository's latest release.
* `leftovers/pins_latest.py` showed every other target unchanged since 2026-10-30: Node LTS
  `v26.13.0`, govulncheck v1.9.0, markdownlint-cli2 0.24.0.

**Step 2.** `leftovers/p2_edits.py` asserted each old string's count first (`--check`), then wrote
eight files. Each count is as the step expects: `upload-artifact` 2, the `flutter-action` comment 3,
every other edit 1. The diff was read in full.

#### Deviation 4 (2026-10-30): two step labels still say "LTS 24"

**Found** reading step 2's diff. Both workflows name the step "Set up Node.js (LTS 24)"
(`ci.yml:68`, `docs.yml:38`). Step 2's list omitted them, so with `NODE_VERSION: "26"` every CI log
would label the step wrongly.

**Decision (owner, 2026-10-30): "Add to P2 (Recommended)".** Both become "Set up Node.js (LTS 26)",
asserted like step 2's edits. Declined: a version-free label, and leaving them.

**Scope.** No file is added. Step 2 gains the two labels.

**Deviation 4, executed:** `leftovers/p2_labels.py` found each label once and replaced it.

**Step 3.** `go install golang.org/x/vuln/cmd/govulncheck@v1.9.0`. `govulncheck -version` now gives
`Scanner: govulncheck@v1.9.0` (it was v1.8.0).

**Step 4, this Mac** (logs `leftovers/0217-p2-*.log`):

| Gate | Result |
| :--- | :--- |
| `make vulncheck` | exit 0, "vulncheck: clear; allowlisted required-module findings: GO-2026-5932" |
| `make markdownlint-docs` | exit 0, "markdownlint-cli2 v0.24.0 (markdownlint v0.42.0)", 56 files, 0 issues |
| `make preflight` | exit 0, "✅ preflight passed", the same sections as P1's. "assert-flutter-pin: OK … (3.47.9)", 158 hosted packages with no advisories, `flutter analyze` "No issues found!", `flutter test` "+1501 ~3: All tests passed!" |

**Step 5, the test hosts** (`PINS_PHASE=p2`, with `PINS_LINUX_MAKE=markdownlint-docs`; HEAD
`a5ccd412` plus the nine changed files):

* **the Linux test host:** `go1.27.3`. `go test -race -count=1 ./...` exit 0, 45 `ok` lines.
  `make markdownlint-docs` exit 0, running `markdownlint-cli2 v0.24.0` under that host's Node
  v24.21.0, with 56 files and 0 issues;
* **the Windows test host:** `go1.27.3`, and `make ci-windows` exit 0, "ci-windows-local: ALL
  SELECTED CHECKS PASSED".

**Step 6.** 0169's status note has the P2 line.

**Step 8** waits for the owner's push. CI's `setup-node` must resolve a 26.x, and the Docs workflow
must pass.

**Committed and pushed** as `1a25019a` ("chore(ci): move Node to 26 LTS and bump action and tool
pins"). The owner's push carried `07ee60b2` to `1a25019a` together.

#### Deviation 5 (2026-10-30): both workflows assert Node 24 by literal

**Found** at step 8. The push's Docs run, `41070855767`, failed in "Assert Node.js major version":

```text
v26.13.0
expected Node 24, got v26.13.0
```

`setup-node` resolved v26.13.0 as intended. The step itself hardcodes the major:
`test "$major" = "24"` at `docs.yml:48` and at `ci.yml:78`, so CI's Go job fails the same way.
`ci.yml:82`'s comment also says "Node 24 is already on PATH for npx". Step 2's list omitted all
three lines, and the search for pins before it looked for action and tool names, not for a bare
`"24"`. `ci.yml:25`, `:28` and `:90` concern the runtime the actions run on, which stays Node 24, and
are correct.

**Decision (owner, 2026-10-30): "Assert against NODE_VERSION (Recommended)".**

* Both steps compare the major with `"${NODE_VERSION}"` and say "expected Node ${NODE_VERSION}",
  so the env pin is the one source and the two cannot drift apart again.
* `ci.yml:82`'s comment drops its version.
* The changed comparison is shown failing on a mismatch and passing on a match, by running the
  step's own script, extracted from the workflow file, under this Mac's Node.
* It lands as a new commit, which the owner pushes.

Declined: a literal "26", and reverting P2.

**Scope.** No file is added: `ci.yml`, `docs.yml` and this PLAN.

**Deviation 5, executed** (`leftovers/p2_node_assert.py`, each old line asserted once;
`leftovers/node_assert_plant.py` runs the `run: |` block under "Assert Node.js major version",
taken verbatim from the workflow, under this Mac's Node v26.10.0):

| Workflow and version | Result |
| :--- | :--- |
| `docs.yml` as pushed, `NODE_VERSION=26` (the CI failure, reproduced) | exit 1, "expected Node 24, got v26.10.0" |
| `docs.yml` fixed, `NODE_VERSION=24` (a mismatch) | exit 1, "expected Node 24, got v26.10.0" |
| `docs.yml` fixed, `NODE_VERSION=26` | exit 0 |
| `ci.yml` fixed, `NODE_VERSION=24` | exit 1, "expected Node 24, got v26.10.0" |
| `ci.yml` fixed, `NODE_VERSION=26` | exit 0 |

The CI log shows `NODE_VERSION: 26` in the step's environment, from the workflow's `env`.

**Committed** as `5ca4a3b1` ("fix(ci): assert Node major version against NODE_VERSION").

* The owner's first push of it did not reach origin. The owner saw many errors, which were not
  captured here.
* `leftovers/guard_range.py` over `1a25019a..5ca4a3b1` gave exit 0, and a `git push --dry-run
  --verbose` (`leftovers/push_dryrun.py`) reported `1a25019a..5ca4a3b1  master -> master`, exit 0.
* The owner then pushed through this session's `!`, with the result
  `1a25019a..5ca4a3b1  master -> master` and no error.

**Step 8, CI on `5ca4a3b1`:**

* **Docs, run `41071671813`.** Attempt 1 failed with no job. The run page's one annotation was
  "Docs: Internal server error", a GitHub fault, not a workflow one. `actionlint` was clean on
  `docs.yml`, and CI started every job with the same line in `ci.yml`. Under C2's dispatch, `gh run
  rerun` gave attempt 2, which was a **success**: "Assert Node.js major version" printed
  `v26.13.0`, and markdownlint printed `markdownlint-cli2 v0.24.0` and "Summary: 0 issues in 0
  files".
* **CI, run `41071672275`: success.** These jobs passed: Go (test), Go (windows/amd64), Go
  (darwin/arm64), Go (linux/arm64), Flutter (analyze & test), Flutter (test on windows), and Live
  supervisors (runit, s6). Android APK and the self-update jobs were skipped, as on every push.
  * Go (test)'s Node is `v26.13.0`, and its markdownlint is `markdownlint-cli2 v0.24.0`.
  * Its vulnerability check installs `govulncheck@v1.9.0` and prints "vulncheck: clear;
    allowlisted required-module findings: GO-2026-5932".
  * Flutter (analyze & test) set up `stable-3.47.9-x64` at framework `c197ca5199`, this Mac's
    build. That meets **P1's A1** in CI.

P2's verification is met. P1's release followed on the owner's tag: see "The release" under P1.

**Files staged for the P2 commit (before the push, `1a25019a`):**

* `.github/workflows/ci.yml`, `docs.yml` and `ci-flake-ledger.yml`;
* `.node-version` and `.nvmrc`;
* `README.md`;
* `scripts/vulncheck.sh`;
* `Makefile`;
* the 0169 PLAN;
* this PLAN.

### P3 — Go modules and `go-selfupdate-lib` v1.23.1

#### Deviation 6 (2026-10-30): `go-selfupdate-lib` v1.23.1 replaces v1.23.0

**Found** before P3 started. `go-selfupdate-lib` v1.23.1 was tagged on 2026-10-30, after the
inventory (MADR F3a). `go list -m -u` now offers it, so step 1's `go get -u` would have landed on
v1.23.1, and step 1's "v1.23.0" check would have stopped the phase.

**Decision (owner, 2026-10-30): "pin go-selfupdate-lib to current v1.23.1".**

* Steps 1, 2 and 6 name v1.23.1. Step 2's pin is `c24f5919…`, peeled through tag `b0c08a0e…`.
* Step 3's caller reading is unchanged. F3's behaviour comes from v1.23.0, and v1.23.1 carries it
  unchanged.
* The MADR's F2 and D5 are amended, and F3a records the facts behind them.

**Scope.** No file is added. P3's files are as listed.

**P0.** The tree was clean at `cd2245ea`. `leftovers/gsl_peel.py v1.23.0 v1.23.1` peeled v1.23.1 to
`c24f5919…` again, with nothing new upstream. `go.mod` and `go.sum` were copied to the scratchpad
(`p3-base/`) before step 1.

**Step 1, `go get -u ./...`** (`leftovers/0217-p3-goget.log`): exit 0. `go.mod` still says
`go 1.27.3`, with no `toolchain` line. The direct moves:

* `certmagic` v0.25.4 → v0.25.6;
* `go-selfupdate-lib` v1.22.3 → v1.23.1;
* `x/mod` v0.41.0 → v0.42.0;
* `acp-go-sdk2` v1.26.0 → **v1.27.0**, which the step did not expect.

#### Deviation 7 (2026-10-30): `go get -u` moves `acp-go-sdk2` to v1.27.0

**Found** at step 1. `acp-go-sdk2` v1.27.0 was tagged 2026-10-30T17:01Z, after the inventory, and
step 1 expected v1.26.0 (MADR F2, D4). Read only with `leftovers/release_diff.py` and its release
notes (MADR F3b):

* it adds the package `remote` and `example/remote`, and changes no package that existed in v1.26.0;
* its `make api` reports "0 incompatible";
* its new requirement, `github.com/coder/websocket v1.8.15`, is one Magic already requires
  directly at that version (`go.mod:7` and its two `go.sum` lines, in the `p3-base/` copy).

**Decision (owner, 2026-10-30): "Take v1.27.0 (Recommended)".**

* Step 1 keeps `go get`'s result and names v1.27.0.
* The MADR's F2 and D4 are amended, and F3b records the facts.
* Step 6's amendment J records it, since 0207's amendment I pinned v1.26.0.
* P3's gates cover it. 0216's plan, which resumes after this one, is unaffected: `agentproc` and
  `acptest` are unchanged.

Declined: holding `acp-go-sdk2` at v1.26.0, and stopping P3 for a record of its own.

**Scope.** No file is added.

*Corrected 2026-10-30, at step 1's tidy:* the question put to the owner said `coder/websocket`
would add only `go.sum` lines. Magic already required it directly, at v1.8.15, before P3, so it
adds nothing (F3b, as corrected). The decision does not change.

**Step 1, continued.** `go mod tidy` (`leftovers/0217-p3-tidy.log`): exit 0, no output. `go.mod`
still says `go 1.27.3` with no `toolchain` line. `go list -m -u` shows no newer version for any
direct module. The `require` diff, from the `p3-base/` copy:

* direct: `certmagic` v0.25.4 → v0.25.6, `acp-go-sdk2` v1.26.0 → v1.27.0, `go-selfupdate-lib`
  v1.22.3 → v1.23.1, `x/mod` v0.41.0 → v0.42.0;
* indirect, moved: `aws-sdk-go-v2` v1.39.1 → v1.47.5, with `config`, `credentials`,
  `feature/ec2/imds`, `internal/configsources`, `internal/endpoints/v2`,
  `service/internal/accept-encoding`, `service/internal/presigned-url`, `service/route53`
  (v1.58.3 → v1.70.6), `service/sso`, `service/ssooidc` and `service/sts`; `smithy-go`
  v1.23.0 → v1.28.7; `caddyserver/zerossl` v0.1.5 → v0.1.6; `go-viper/mapstructure/v2` v2.4.0 →
  v2.5.0; `klauspost/cpuid/v2` v2.3.0 → v2.4.1; `mholt/acmez/v3` v3.1.6 → v3.1.7; `miekg/dns`
  v1.1.72 → v1.1.73; `pelletier/go-toml/v2` v2.2.4 → v2.4.3; `sagikazarmark/locafero` v0.11.0 →
  v0.12.0;
* indirect, added: `aws-sdk-go-v2/internal/v4a` v1.5.8, `aws-sdk-go-v2/service/signin` v1.12.0;
* indirect, dropped by tidy as no longer needed: `aws-sdk-go-v2/internal/ini`, `sourcegraph/conc`,
  `golang.org/x/sync`, `golang.org/x/tools`.

`go list -m -u all` still offers newer versions of ten modules in the graph that `go.mod` does not
list, such as `stretchr/testify` and `x/tools`. They are other modules' requirements, outside the
build, which D4 does not move.

**Step 2.** `leftovers/p3_pins.py` asserted the old pin twice and the new one not at all, then
wrote `ci.yml:743` and `:764`:
`@c24f5919cd9e9e3ba3736e64bcf18a6e2a51489e # go-selfupdate-lib v1.23.1`.

* `TestWorkflowPinsMatchGoMod`: PASS.
* The test checks the pin's form and its comment, not that the commit is the tag's. Its doc
  leaves that to `git ls-remote`, which gave `refs/tags/v1.23.1^{}` at `c24f5919…`.

**Step 3, every caller read** (module cache, `v1.22.3` against `v1.23.1`; 0207 amendment J, J-2 and
J-4, holds the file and line of each):

* **Supervised (runit, s6, OpenRC).** `Start`'s one new check: `Installed` false is
  `ErrNotInstalled`, and `Up` does not run. Magic's `State` sets `Installed` from the definition
  file. `startThrough` and the installer hook start only after the definition is written or
  refreshed, so the error means a missing definition, and their wrapped message stays true.
  `stopForRemoval`, the tool paths and the state readers already handle `ErrNotInstalled`.
* **launchd.** A missing property list is `ErrNotInstalled` at once. `Stop`'s one deadline only
  shortens the worst case, and no caller relies on the old one.
* **systemd `Type=notify`.** A start that ran and failed was an error already, and now also wraps
  `ErrUnhealthy`. No Magic code branches on it, and `verifyStart` reads only `WaitHealthy`.
* **The managed update.** The library's own recovery takes `Start`'s errors, and
  `internal/updateclient` checks none.
* **scm.** Not imported by Magic.

No caller reports wrongly. C8's stop did not arise, and no `.go` file changes.

**Step 4, this Mac** (logs `leftovers/0217-p3-*.log`):

| Gate | Result |
| :--- | :--- |
| `make pre-add-check` | exit 0, "992 file(s) clean (gofmt, golint, govulncheck)" |
| `go vet ./...` (`leftovers/vet_goos.py`) | darwin/arm64, windows/amd64 cgo off, linux/amd64 cgo off: exit 0 each |
| `make staticcheck` | exit 0; "56 ignored finding(s), each an acp-go-sdk2 unstable-schema name" for each GOOS, the count of P2's CI run |
| `make vulncheck` | exit 0, "vulncheck: clear; allowlisted required-module findings: GO-2026-5932" |
| `make race` | exit 0; 45 `ok`, no FAIL, panic or data race |
| `make preflight` | exit 0, "✅ preflight passed": every Go package `ok`, the hermetic install tests, "assert-flutter-pin: OK … (3.47.9)", 158 hosted packages with no advisories, `flutter analyze` "No issues found!", `flutter test` "+1501 ~3: All tests passed!" |
| `make check-libs` on `HEAD` | `make` exit 2, from `check_libs.py`'s exit 1 ("2 failing checks"): `acp-go-sdk2` (v1.26.0 → v1.27.0) every check ok; `go-selfupdate-lib` (v1.22.3 → v1.23.1) "modules: FAIL new or changed module: golang.org/x/mod" and "test: FAIL", `TestWorkflowPinsMatchGoMod` on the old `ci.yml` pins |
| check-libs on the phase's content (`leftovers/check_libs_staged.py`) | exit 0, "0 failing checks; scratch removed"; both libraries "pinned at" their tag, "nothing to compare" |
| `make live-supervisors KINDS="runit s6 openrc"` | exit 0: `TestLiveRunit` (61.46s), `TestLiveS6` (59.23s), `TestLiveOpenRC` (60.48s); "failed: none" |

* `make check-libs` checks the committed `HEAD`. It moves one library and nothing else, so a run
  before the phase commit fails as
  [0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md](0207-PLAN-pin-go-selfupdate-lib-v1-17-2.md)'s did
  before its commit.
  * `leftovers/check_libs_staged.py` clones `HEAD` into the scratchpad and copies `go.mod`, `go.sum`
    and `ci.yml` over the clone. It commits them there with `git commit-tree`, which runs no hook,
    in a clone that is never pushed. Then it runs go-shared's `check_libs.py --repo` on the clone.
  * The real tree's status and `HEAD` were unchanged after it.
  * `make check-libs` is run again on the owner's commit (step 8).
* The live run's first attempt stopped before any test, at "unable to connect to Podman socket":
  this Mac's Podman machine was stopped. Under C2 it was started (`podman machine start`), the run
  repeated, and the machine stopped again after it.
* In each live run the update moved the service from this tree's build to `v0.23.0`, with
  `service_was_running` and `service_started` true and `rolled_back` false, through the library's
  `Supervised` adapter.

**Step 5, the test hosts** (`PINS_PHASE=p3`, with
`PINS_LINUX_GO="./internal/cli/service/... ./internal/updateclient/..."`, a new option of
`leftovers/pins_hosts.py`; HEAD `cd2245ea` plus `go.mod`, `go.sum` and `ci.yml`):

* **the Linux test host:** `go1.27.3 linux/amd64`. `go test -race -count=1 ./...` exit 0, 45 `ok`
  lines. `go test -count=1` of the two package trees: exit 0, `internal/cli/service` and
  `internal/updateclient` `ok`.
* **the Windows test host:** `go1.27.3 windows/amd64`. The `make -n` check saw Windows, and
  `make ci-windows` exit 0, "ci-windows-local: ALL SELECTED CHECKS PASSED", 45 `ok` lines.
* Both scratch trees were removed, and their absence checked.

**Step 6.** 0207's MADR has "Amendment — 2026-10-30 (J): `go-selfupdate-lib v1.23.1` and
`acp-go-sdk2 v1.27.0`", with D51 to D53. Its date is now 2026-10-30.

**Step 7.** No page outside the records names either old version, and no code, script or workflow
does. `docs/README.md` names them only in amendment I's plan title, which stays. No `.go` file is
staged.

**Committed** as `c2fcd3db` ("chore(deps): update go-selfupdate-lib to v1.23.1 and acp-go-sdk2 to
v1.27.0"), with the six staged files.

* `make check-libs` on that `HEAD` (`leftovers/0217-p3-checklibs-head.log`): exit 0, "0 failing
  checks; scratch removed". Both libraries show "pinned at" their tag, as 0207's
  `v1.17.2` plan's committed-pin run did.
* `leftovers/guard_range.py` over `5ca4a3b1..c2fcd3db` (`b971517a`, `cd2245ea`, `c2fcd3db`): exit 0,
  no output.
* The owner pushed it. `git ls-remote origin` showed `master` at `c2fcd3db`.

**Step 8, CI on `c2fcd3db`:**

* **CI, run `41114888620`: success.** These jobs passed: Go (test), Go (windows/amd64), Go
  (darwin/arm64), Go (linux/arm64), Live supervisors (runit, s6), Flutter (analyze & test) and
  Flutter (test on windows). Android APK and the self-update jobs were skipped, as on every push.
* **Docs, run `41114869306`: success**, at attempt 1.

P3's verification is met: steps 1, 4, 5 and 8 pass, and `TestWorkflowPinsMatchGoMod` passes.

### P4 — Dart packages and the Android build tools

**P0.** The tree was clean at `c2fcd3db`, apart from the 0219 pair, which is untracked and not this
phase's.

* `flutter pub outdated --json` (`leftovers/0217-p4-outdated-before.json`, summarised by
  `leftovers/pub_outdated_summary.py`): 15 direct packages behind, 0 dev and 65 transitive. Two
  are new majors, `cupertino_icons` 1.0.9 → 2.0.0 and `flutter_secure_storage` 10.3.1 → 11.2.0,
  as step 3 expects. The other 13 are in range. "no advisories, retractions or discontinuations".

**Step 1, first part.** `make apk` at `c2fcd3db` (`leftovers/0217-p4-apk-baseline.log`): exit 0,
"✓ Built build/app/outputs/flutter-apk/app-release.apk (41.2MB)", and
`assert-flutter-release-apk` "OK release-mode APK". Its output repeats the KGP warning for
`mobile_scanner` and `speech_to_text`. It was copied to `leftovers/0217-p4/baseline.apk`.
`apksigner verify --print-certs` (build-tools 36.0.0): `CN=Magic CLI Remote`, SHA-256 `867c9f10…`.

#### Deviation 8 (2026-10-30): the AVD's app and `make apk` have different signers

**Found** at step 2, before anything was installed. The AVD was booted (C2), and the app on it was
read:

* `dumpsys package`: `versionName=0.1.0`, `flags=[ DEBUGGABLE … ]`, last updated 2026-08-25.
* `apksigner` on its pulled `base.apk`: `CN=Android Debug`, SHA-256 `d6b63d40…`.

Step 1 said `make apk` would be signed with the debug key "without `key.properties`". The file
exists in `apps/mobile/android/` (its presence was checked, and its contents were not read), so
`make apk` signs with the upload key. Android refuses `install -r` across signers. Uninstalling
deletes the app's data and its Keystore keys, which are what D7's check must see survive.

**Decision (owner, 2026-10-30): "Debug builds for the proof (Recommended)".**

* Steps 1, 2 and 7 use debug builds, `flutter build apk --debug --target-platform android-arm64`,
  at `c2fcd3db` and after the change. Both are signed with this Mac's debug key, as the installed
  app is, so `install -r` keeps its data and its pairing.
* `flutter_secure_storage`'s code is the same in a debug and a release build, so A7 reads the same.
* Step 6 still gates the release `make apk`, with `assert-flutter-release-apk.sh` and
  `make manifest-surface`.
* The MADR asserts nothing about signing, and is unchanged.

Declined: uninstalling for release builds and a re-pair, and stopping P4 for a record of its own.

**Scope.** No file is added.

**Step 1, as amended.** `flutter build apk --debug --target-platform android-arm64` at
`c2fcd3db` (`leftovers/0217-p4-apk-baseline-debug.log`): exit 0, "✓ Built
build/app/outputs/flutter-apk/app-debug.apk", copied to `leftovers/0217-p4/baseline-debug.apk`.
`apksigner`: `CN=Android Debug`, SHA-256 `d6b63d40…`, the installed app's signer.

**Step 2.**

* `leftovers/adb_backup.py` streamed `adb exec-out run-as … tar -cf - .` to
  `leftovers/0217-p4/appdata-before.tar`: adb exit 0, 97 581 568 bytes, SHA-256 `e439e86c…`, 39
  members, `shared_prefs/FlutterSecureStorage.xml` and its configuration among them. It printed
  member names and sizes only.
* `adb install -r baseline-debug.apk`: "Success".
* Launched with `am start`. The sessions screen showed the green dot and "Connected to … over
  Mesh", with no re-pair. The host name it shows is not recorded (C7).
* Settings, by host-side capture (`adb emu screenrecord screenshot`): **Certificate pin**
  `selfsigned · 23F21EDA…`; **Client identity** `PgZUp-lo…` (base64url, not hex; its first 8
  characters); **Secret storage** "No failures recorded".

**Step 3.**

* `flutter pub upgrade --major-versions cupertino_icons flutter_secure_storage`
  (`leftovers/0217-p4-majors.log`): "Changed 2 constraints in pubspec.yaml". `pubspec.yaml` reads
  `cupertino_icons: ^2.0.0` (line 17) and `flutter_secure_storage: ^11.2.0` (line 21), and no other
  line changed. Four packages moved: those two, `flutter_secure_storage_darwin` 0.3.2 → 0.4.3 and
  `flutter_secure_storage_platform_interface` 2.0.2 → 2.1.1.
* `flutter pub upgrade` (`leftovers/0217-p4-upgrade.log`): "Changed 80 dependencies!". Among them:
  every in-range direct package of P0's list, `cupertino_ui` 1.0.1 → 1.1.5 and `material_ui`
  1.1.0 → 1.9.0.
* 20 transitive packages left the lock, `test`, `test_core`, `analyzer`, `coverage` and `shelf`
  among them.
  * `leftovers/pub_who_needs.py` over the baseline lock found `riverpod` 3.4.2 the only package
    that required `test` at run time. `leftovers/pub_runtime_deps.py`: `riverpod` 3.4.3 requires
    `test_api`, which the SDK pins, in its place. The other 19 were `test`'s own dependencies.
  * No Dart file under `lib/` or `test/` imports `package:test`, `test_api` or `analyzer`.
* `flutter pub outdated --json` (`leftovers/0217-p4-outdated-after.json`): 0 direct and 0 dev
  packages behind, and "no advisories, retractions or discontinuations". Four transitive packages
  stay behind, held by their dependents' constraints: `cross_file` 0.3.5+5 (0.4.0 is a major),
  `material_color_utilities` 0.13.0, `test_api` 0.7.12 (both pinned by the SDK) and `xml` 7.0.1.

**Step 4.** 3.47.9's `gradle_utils.dart` (`flutter --version`: 3.47.9, framework `c197ca5199`,
Dart 3.13.7): `maxKnownAndSupportedGradleVersion` `'9.3.1'` (`:85`),
`maxKnownAndSupportedKgpVersion` `'2.4.0'` (`:91`), `maxKnownAgpVersionWithFullKotlinSupport`
`'9.1.0'` (`:104`), and AGP 9.1.x requiring Gradle `min: '9.3.1'` to the maximum (`:825-826`). Each
is as F9 found it. `maxKnownAndSupportedAgpVersion` is `'9.2'` (`:98`).

**Step 5.** `leftovers/p4_buildtools.py` asserted each old line once and each new one absent, then
wrote `gradle-wrapper.properties:5` `gradle-9.3.1-all.zip`, `settings.gradle.kts:22`
`com.android.application` `"9.1.1"` and `:23` `org.jetbrains.kotlin.android` `"2.4.0"`, and read
each back. `app/build.gradle.kts` is unchanged.

**Step 6, this Mac:**

| Gate | Result |
| :--- | :--- |
| `make apk` (`leftovers/0217-p4-apk.log`) | exit 0, "✓ Built … app-release.apk (41.3MB)", "assert-flutter-release-apk: OK release-mode APK". No line matches "flutter support for your project", "Kotlin Gradle Plugin", "will soon be dropped" or "is not supported" (count 0). The KGP warning for `mobile_scanner` and `speech_to_text`, in both baseline builds, is gone. Copied to `leftovers/0217-p4/phase.apk`. |
| C5, the Gradle download (`leftovers/gradle_sum.py`) | `gradle-9.3.1-all.zip` SHA-256 `17f27786…`, equal to `services.gradle.org`'s published `.sha256` |
| `make manifest-surface` | exit 0, "OK surface matches the allowlist" |
| `make preflight` (`leftovers/0217-p4-preflight.log`) | exit 0, "✅ preflight passed": "assert-flutter-pin: OK … (3.47.9)", "138 hosted packages, no advisories" (158 before; step 3's 20), `flutter analyze` "No issues found!", `flutter test` "+1501 ~3: All tests passed!", `dart format` "Formatted 229 files (0 changed)" |

`make apk` warned "no well-formed build version (0.24.0.gc2fcd3db-dirty); building unstamped", as
it does for a tree with changes.

**Step 7, the emulator upgrade (A7).**

* `flutter build apk --debug --target-platform android-arm64` of this phase's tree
  (`leftovers/0217-p4-apk-debug.log`): exit 0. Copied to `leftovers/0217-p4/phase-debug.apk`.
* `adb install -r phase-debug.apk`: "Success". Launched: the sessions screen showed "Connected to
  … over Mesh", with no re-pair.
* Settings: **Certificate pin** `selfsigned · 23F21EDA…` and **Client identity** `PgZUp-lo…`, each
  the same as step 2's; **Secret storage** "No failures recorded".
* `flutter_secure_storage` 11 read the store that 10 wrote. The backup was not needed. The AVD was
  shut down afterwards (`adb emu kill`), as it was before. It keeps this phase's debug build.

**Step 8, the test hosts** (`PINS_PHASE=p4`; `HEAD` `c2fcd3db` plus the four changed files):

* **the Linux test host:** `go1.27.3 linux/amd64`, and `go test -race -count=1 ./...` exit 0, 45
  `ok` lines;
* **the Windows test host:** `go1.27.3 windows/amd64`, the `make -n` check saw Windows, and
  `make ci-windows` exit 0, "ci-windows-local: ALL SELECTED CHECKS PASSED", 45 `ok` lines;
* both scratch trees removed, and their absence checked.

**Step 9.** No `.dart` file changes in this phase.

**A slip of the agent's, recorded (2026-10-30).** At step 3, one read of
`0217-p4-outdated-after.json` was an inline `python3 -c`, which this session's rule has refused
since Deviation 2. It read the file and printed four tuples. It held no backtick and no shell
substitution, and changed nothing. Every other script in the phase is a `leftovers/` file.

**Files staged for the P4 commit:** `apps/mobile/pubspec.yaml`, `apps/mobile/pubspec.lock`,
`apps/mobile/android/gradle/wrapper/gradle-wrapper.properties`,
`apps/mobile/android/settings.gradle.kts`, and this PLAN. The 0219 pair stays untracked.

P4's verification is met: steps 3, 4, 6 and 7 pass.

**Committed** as `896834a5` ("chore(mobile): update dependencies and gradle to current releases"),
with the five staged files. `leftovers/guard_range.py` over `c2fcd3db..896834a5`: exit 0, no
output. The owner pushes it. CI on the push is recorded when it has run.

### P5 — The host standard, the held line, and the hosts

**P0.** `leftovers/p5_latest.py`: Node line 26's newest is still `v26.13.0` (2026-10-28, LTS
Lithium), and Python 3.14's `latest` is still `3.14.10` (2026-10-25, end of life 2030-10-31).
magic-git was clean on `master` at `5458472`, with no `.flutter-sdk`. Its `.gitignore:53` is
`/.flutter-sdk/`, and `build_macos.sh:46` still pins `FLUTTER_VERSION="3.47.5"`, cloned at `:177`.
magic-git's `AGENTS.md` asks for `git commit --no-edit` and no commit unless asked.

#### P5a — the audit learns a held line

**Step 1.** `git clone --depth 1 --branch 3.47.5 https://github.com/flutter/flutter.git .flutter-sdk`
in magic-git (C2): checkout `6a19cca5…`, 16 074 files. `./.flutter-sdk/bin/flutter --version`
(`leftovers/0217-p5-mg-flutter-version.log`): "Flutter 3.47.5", framework `6a19cca564`, Dart
3.13.4. That first run fetched the tool's 215 MB engine bundle, as `build_macos.sh`'s first run
does. magic-git's `git status` stayed empty.

**Step 2.** `version_audit.py`:

* the docstring lists `HELD`, and says it changes no exit status. `FAIL_STATES` and `exit_code`
  are unchanged;
* `s_python` calls a new `held_python` when the standard's `python` entry has `held_line`, and
  otherwise does what it did, through `newest_python_line`, which is the old inline search;
* `held_python`: `ERROR` when `held_line` differs from `line` or from `version`'s line, or is not
  in the lifecycle feed; `UNSUPPORTED` when the held cycle's `eol` is on or before `--today`;
  `STALE` against the held cycle's `latest` when it is newer; otherwise `HELD`, naming the newest
  supported line, its `latest` and `held_reason`;
* `python_advisories` is unchanged, and still runs for the standard's version.

A scratch copy of the script and the standard before the change is at `leftovers/0217-p5/before/`.

**Step 3.** The devenv `README.md` has the `HELD` row ("none" for its exit), a note that `HELD`
alone leaves the exit at 0, and a paragraph on `held_line` and `held_reason`. It says how the hold
is checked, and that it ends at its line's end of life.

**Step 4, seen failing first** (C6; `leftovers/held_line_plants.py`, planted standards under
`leftovers/0217-p5/plants/`, an empty probe directory; logs `0217-p5-plants-py314.log` and
`-py39.log`). Under Python 3.14.10 and under `/usr/bin/python3` 3.9.6 the rows were the same:

| Plant | `standard python` rows |
| :--- | :--- |
| `held_line` 3.14, version 3.14.9 | `STALE 3.14.9 -> 3.14.10 (the newest patch of held line 3.14)`; `ADVISORY 3.14.9: 2 advisories fixed by 3.14.10: CVE-2026-114583, CVE-2026-115557` |
| `held_line` 3.14, 3.14.10, `--today 2030-11-01` | `UNSUPPORTED 3.14.10: held line 3.14 ended 2030-10-31` |
| `held_line` 3.13, `line` 3.14 | `ERROR held_line 3.13 does not match line 3.14 and version 3.14.10` |
| no `held_line`, 3.14.10 | `STALE 3.14.10 -> 3.15.2`, as before the change |
| `held_line` 3.14, 3.14.10 | `HELD 3.14.10 on held line 3.14; newest line 3.15 (3.15.2): plant`, and nothing else |
| the last row, against the script before the change | `STALE 3.14.10 -> 3.15.2` |

`exit_code`, loaded from the changed script by `leftovers/held_exit_code.py`: `0` for one `HELD`
row, `1` for `HELD` and `STALE`. The script compiles under 3.9 (`py_compile`). The probe's
import left a `__pycache__` beside the script, which git ignores; it was removed.

**Step 5, magic-git's gates** (`leftovers/mg_gates.py`, `PATH` led by `.flutter-sdk/bin`):
`python3 dependencies.py` exit 0, "The Python tooling can run here."; `flutter pub get
--enforce-lockfile` "Got dependencies!"; `flutter analyze` "No issues found!"; `dart run
scripts/tools/records.dart check` "0 finding(s)". `flutter test` failed: see Deviation 9.

#### Deviation 9 (2026-10-30): magic-git's file-watcher tests fail on this Mac at its `HEAD`

**Found** at P5a step 5.

* `flutter test` in magic-git, run 1: "+4762 ~3 -6: Some tests failed". The six were in
  `remote_edit_service_test.dart`, `worktree_invalidation_test.dart`,
  `local_watch_bounded_test.dart` (2), `fswatch_arm_exec_test.dart` and
  `local_watch_worktree_test.dart`. Each reads the real filesystem watcher: "no matching watch
  event within 0:00:15", "FSEvents watches every subtree; the arm must not report one", a diff
  loaded three times where once was expected, an atomic save not seen.
* Run 2: "+4766 ~3 -2", `local_watch_bounded_test.dart` and `local_watch_worktree_test.dart`.
* Alone, in the tree (`leftovers/mg_watch_rerun.py`): the five files failed 3, 3, 3, 3 and 1 times
  of 3. Later, `fswatch_arm_exec` and `worktree_invalidation` passed 2 of 2.
* **Pre-existing.** A clean clone of magic-git's `HEAD` `5458472` in the scratchpad, with none of
  P5's changes and no `.flutter-sdk` (`leftovers/mg_head_rerun.py`): `local_watch_bounded` failed
  3 of 3, `local_watch_worktree` 3 of 3, and `fswatch_arm_exec` 1 of 2.
* No Dart file under magic-git's `lib/` or `test/` reads `devenv`, `standard.json` or
  `version_audit`. `fswatch` is 1.22.0, installed 2026-09-02, so P1's Homebrew upgrade did not move
  it.

**Decision (owner, 2026-10-30): "Record, stage, own item (Recommended)".** The failure is recorded
here as pre-existing. The three magic-git files are staged. The watcher failures are an item for
magic-git's own MADR/PLAN, under "Not done by this plan". Declined: fixing the watchers before
P5's magic-git commit, and leaving magic-git unstaged while the hosts go on.

**Scope.** No file is added.

#### P5b — `standard.json`

**Step 1.** `leftovers/p5b_standard.py` replaced the `go`, `node` and `python` blocks, each
asserted to occur once, and checked that only those keys changed:

* `go`: `version` 1.27.3, `verified` 2026-10-30;
* `node`: `version` 26.13.0, `line` "26", `verified` 2026-10-30, and the note "The newest Active LTS
  line, 26 (Lithium), since 2026-10-28 (magic-cli-remote MADR 0169 D4, 0217 D11).";
* `python`: `version` 3.14.10, `held_line` "3.14", `held_reason` "The owner holds 3.14
  (magic-cli-remote MADR 0217 D13); 3.15 is not adopted.", `verified` 2026-10-30, and the note
  now says 3.14.10 has no published advisory (MADR F15).

**Step 2.** The audit on the moved standard with no probe documents
(`leftovers/0217-p5b-audit.log`), "-- 1 ADVISORY, 1 ERROR, 1 HELD, 1 REVIEW, 5 STALE; exit 1":

* no Go or Node row;
* `HELD … python 3.14.10 on held line 3.14; newest line 3.15 (3.15.2): The owner holds 3.14 …`;
* `STALE`: Flutter 3.47.5 → 3.47.9, Java 25.0.4.1 → 25.0.5, Git for Windows 2.56.0.windows.1 →
  2.56.0.windows.4, gh 2.102.0 → 2.103.3, glab 1.120.0 → 1.126.0;
* `ADVISORY`: Java 25.0.4.1, the OpenJDK advisory of 2026-10-24;
* `REVIEW`: Dart's GHSA-frr6-h895-5pxp;
* `ERROR`: "no probe documents", from the empty directory this step uses.

Each row is one the MADR names (D1, D2, D9).

**Step 3.** magic-git's disclosure preview (`leftovers/guard_preview_repo.py`): "would edit 0
file(s)". No host alias or real path is in its diff. `version_audit.py`, `README.md` and
`standard.json` are staged there, for the owner's commit.

#### P5c — the hosts

**This Mac, read before anything moved** (`leftovers/p5c_read.py`, saved as
`leftovers/0217-p5/mac-before.json`):

* `go1.27.3 darwin/arm64`;
* `node` v26.10.0, from Homebrew's `node 26.10.0_1`;
* npm globals: `@kilocode/cli` 7.8.18 (`kilo`, `kilocode` answer 7.8.18), `@openai/codex` 0.166.0
  ("codex-cli 0.166.0"), `@xai-official/grok` 1.0.70;
* the developer `python3`, Homebrew's `python@3.14` 3.14.10, already the target; `/usr/bin/python3`
  3.9.6; no `~/default-venv`.

`leftovers/p5c_mac_brew.py dry-run` (`HOMEBREW_NO_INSTALL_CLEANUP=1`; log
`leftovers/0217-p5/mac-brew-dry-run.log`):

* "Warning: python@3.14 3.14.10 already installed";
* `node 26.10.0_1 -> 26.13.0_1`, installing `icu4c@79` 79.1, and upgrading its own dependencies
  `simdutf`, `ada-url`, `brotli`, `c-ares`, `hdrhistogram_c` 0.11.10 → 0.12.1, `merve` and
  `simdjson` 4.6.11 → 5.0.7;
* "Would upgrade 1 dependent: gemini-cli 0.46.0 -> 0.66.0", marked deprecated.

#### Deviation 10 (2026-10-30): the Node upgrade would move `gemini-cli`, a dependent

**Found** in the dry run. No unrelated formula moves to a new major: `simdjson`'s major is `node`'s
own dependency. But `gemini-cli`, an agent CLI this plan does not name, would move 0.46.0 → 0.66.0
as a side effect, which the owner's scope rule keeps out.

* `brew uses --installed gemini-cli`: nothing.
* `brew info`: installed on request; "Deprecated because it is not supported upstream! It will be
  disabled on 2026-12-18", replacement the `antigravity-cli` cask.
* Magic never runs a `gemini` binary. Its mentions (`internal/agenterr/agenterr.go:70`, `:102`,
  `:131`, `:769`) read Gemini API error bodies.

The owner asked "can we not just remove gemini-cli?".

**Decision (owner, 2026-10-30): "Yes, uninstall it (Recommended)".** `brew uninstall gemini-cli`,
then `HOMEBREW_NO_INSTALL_CLEANUP=1 brew upgrade node`. `brew uninstall` removes the keg only;
`~/.gemini` stays. A reinstall would get 0.66.0 or the cask, not 0.46.0. Declined: upgrading `node`
with dependents skipped, and letting `gemini-cli` move too.

**Scope.** No file is added. This Mac loses the `gemini-cli` formula.

#### Deviation 11 (2026-10-30): the uninstall autoremoved `node` (the agent's error)

**Found** in the uninstall's own output (`leftovers/0217-p5/mac-brew-uninstall-gemini.log`):
"Autoremoving 8 unneeded formulae: abseil, ada-url, fmt, libffi, merve, nbytes, node, simdutf",
with `node 26.10.0_1` uninstalled. Homebrew had `node` recorded as a dependency of `gemini-cli`,
not as installed on request, so removing its only dependent made it "unneeded". The agent ran the
uninstall without `HOMEBREW_NO_AUTOREMOVE=1`. That breaks C4: the old keg is gone.

* This Mac then had no `node`. `/opt/homebrew/lib/node_modules` still held `@kilocode`, `@openai`,
  `@xai-official` and `npm`, which could not run without it.
* `brew list --versions` was saved first (`leftovers/0217-p5/mac-brew-list-versions-before.txt`).
* The `node--26.10.0_1` bottle (`b0f45417…arm64_tahoe.bottle.tar.gz`) is still in Homebrew's
  download cache.

**Decision (owner, 2026-10-30): "Install 26.13.0 now (Recommended)".**
`HOMEBREW_NO_AUTOREMOVE=1 HOMEBREW_NO_INSTALL_CLEANUP=1 brew install node`, which installs the
target and records `node` as installed on request. Every npm global is checked afterwards. The
cached 26.10.0_1 bottle stands in for C4's old keg as the rollback. `leftovers/p5c_mac_brew.py`
now always sets `HOMEBREW_NO_AUTOREMOVE=1` beside `HOMEBREW_NO_INSTALL_CLEANUP=1`. Declined:
restoring 26.10.0_1 first and then upgrading, and restoring 26.10.0_1 only.

**Scope.** No file is added.

**Deviation 11, executed; this Mac's result.**

* `leftovers/p5c_mac_brew.py install-node` (`leftovers/0217-p5/mac-brew-install-node.log`): exit 0.
  "Pouring node--26.13.0_1", with the eight removed dependencies reinstalled (`icu4c@79` 79.1 new,
  `simdutf` 9.3.3, `ada-url` 4.0.0_1, `merve` 1.2.2_4) and `brotli`, `c-ares`,
  `hdrhistogram_c` 0.12.1 and `simdjson` 5.0.7 upgraded. Homebrew checks each bottle against its
  formula's SHA-256 (C5).
* `brew leaves --installed-on-request` now lists `node`, and still `node@24`.
* `leftovers/p5c_read.py` in a new login shell (`leftovers/0217-p5/mac-after.json`):
  `go1.27.3`; `node` v26.13.0 at `/opt/homebrew/bin/node`; `kilo` and `kilocode` 7.8.18, "codex-cli
  0.166.0", "grok 1.0.70 (f50904eaee6c) [stable]", each as before; `npm` 11.19.1 → 11.20.0, which
  ships with `node`; `python3` 3.14.10; `/usr/bin/python3` 3.9.6.

**The Linux test host, read before anything moved** (`leftovers/p5c_remote.py` running
`p5c_read.py`, `p5c_layout.py` and `p5c_where.py` there; saved as `leftovers/0217-p5/linux-*.txt`
and `linux-before.json`):

* `go1.27.3 linux/amd64`;
* `node` v26.10.0 from `~/sdk/node-v26.10.0`; no npm global package but `npm` 11.19.1;
* the developer `python3` is `~/default-venv/bin/python3`, 3.14.10. Its `pyvenv.cfg` names
  `~/sdk/python-3.14.10`. `/usr/bin/python3` is the distribution's 3.14.4. The freeze lists 84
  packages (`leftovers/freeze_count.py`);
* `~/sdk` holds `node-v24.21.0`, `node-v26.10.0` (created 2026-09-21), `python-3.14.7` and
  `python-3.14.10` (created 2026-10-30 17:31);
* the login PATH's Node comes from `~/dotfiles/bash/.bashrc.d/00-paths.sh:27`,
  `path_prepend "$HOME/sdk/node-v26.10.0/bin"`, an uncommitted change in the dotfiles repository,
  whose last commits are `7bc78b9` (2026-10-21, Go 1.27.3) and `32b064b` (2026-09-30, Node 24.21.0);
* the `mcremote` unit's drop-in `path.conf:22` (a link into the dotfiles, committed) names
  `~/sdk/node-v24.21.0/bin`, and `systemctl --user show mcremote` confirms it.

#### Deviation 12 (2026-10-30): the Linux test host was already partly moved, by the owner

**Found** in the read above. MADR F14 and F15 said the host ran Node 24.21.0 and Python 3.14.7. The
login PATH was on 26.10.0, through an uncommitted dotfiles edit, and the daemon's drop-in on
24.21.0. Python 3.14.10 had been installed and the venv rebuilt on 2026-10-30, not by this
session.

**Decision (owner, 2026-10-30): "Mine; build on it (Recommended)".**

* The MADR's F14 and F15 are corrected to the observed facts.
* Python is already at the target on this host, so its row's Python move is done. The venv is
  not rebuilt again.
* Node: `node-v26.13.0-linux-x64.tar.xz` (SHA-256 `d28a96af…`, from `SHASUMS256.txt`) goes to
  `~/sdk/node-v26.13.0`. `00-paths.sh:27` (on top of the owner's uncommitted edit) and
  `path.conf:22` both name `~/sdk/node-v26.13.0/bin`, each backed up first, then `systemctl --user
  daemon-reload`. The daemon takes the new PATH at its next restart, which is the owner's.
* The owner commits the dotfiles.

Declined: pausing the host, and leaving it to the owner.

**Scope.** No file is added here. On the host: `00-paths.sh` and `path.conf` in the dotfiles.

**The Linux test host, moved** (`leftovers/p5c_linux_node.py`, run there through `p5c_remote.py`;
`--check` first, then the move; `leftovers/0217-p5/linux-node-move.txt`):

* backups of both files in `~/.local/state/mcr-0217-backup-20261030-220316/` (SHA-256
  `d84ee4d2…` and `d6254990…`);
* `node-v26.13.0-linux-x64.tar.xz`: SHA-256 `d28a96af…`, equal to `SHASUMS256.txt` (C5); unpacked
  to `~/sdk/node-v26.13.0`, whose `node --version` is v26.13.0;
* `00-paths.sh:27` and `path.conf:22` now name `node-v26.13.0`, each read back;
* `systemctl --user daemon-reload` exit 0; the unit's `Environment` names `sdk/node-v26.13.0/bin`
  and no longer `node-v24.21.0`. The running daemon takes it at its next restart, which is the
  owner's;
* read again in a new login shell (`linux-after.json`): `go1.27.3`, `node` v26.13.0 with `npm`
  11.20.0, `python3` 3.14.10 (the venv), `/usr/bin/python3` 3.14.4.

The dotfiles' `00-paths.sh` (the owner's earlier edit and this one) and `path.conf` are the owner's
to commit.

**WSL (`Ubuntu-24.04`), read** (`leftovers/p5c_wsl.py`; `--find-devenv` found `~/.config/devenv.sh`
only there; `wsl-before.json`, `wsl-layout.txt`, `wsl-where.txt`): `go1.27.3`; `node` v26.10.0 from
`~/sdk/node-v26.10.0`, named by `devenv.sh:7`, with the owner's backup `devenv.sh.bak-20261030`
naming `node-v24.21.0`; `~/sdk/python-3.14.10` created 2026-10-30, and `~/default-venv` built on
it, with an empty `pip freeze`; `/usr/bin/python3` 3.12.3; no npm global but `npm`; no `mcremote`
unit. The same state as the Linux host's, so it was moved under Deviation 12's decision. The empty
venv is reported to the owner and not changed.

**WSL, moved** (`leftovers/p5c_wsl_node.py`; `wsl-node-move.txt`): `devenv.sh` backed up to
`~/.local/state/mcr-0217-backup-20261030-170616/` (`e163ade0…`); the tarball's SHA-256 `d28a96af…`
matched; `~/sdk/node-v26.13.0` reports v26.13.0; `devenv.sh:7` names `$HOME/sdk/node-v26.13.0/bin`.
Read again (`wsl-after.json`): `node` v26.13.0, `npm` 11.20.0, `python3` 3.14.10,
`/usr/bin/python3` 3.12.3, `go1.27.3`.

**The Windows test host, read** (`windows-before.json`): `go1.27.3`; `node.exe` v26.10.0 from
`C:\Program Files\nodejs` (the owner's earlier MSI move); `python` 3.14.10, `py list` "3.14[-64] *
Python 3.14.10"; no `default-venv`; npm globals in the user prefix, `markdownlint-cli2` 0.23.2,
`prettier` 3.9.9, `tsx` 4.23.15, `typescript` 7.0.2 and `npm` 12.1.0, each answering. Python is at
the target; only Node moves, under Deviation 12's decision.

**Windows, moved** (`leftovers/p5c_windows_node.py`; `--check`, then the move;
`windows-node-move.txt`):

* the User `Path` value (`REG_SZ`) and `py list` saved in `~\mcr-0217-staging\`;
* `node-v26.13.0-x64.msi` SHA-256 `0e5ee99c…`, equal to `SHASUMS256.txt`; for the rollback,
  `node-v26.10.0-x64.msi` `ee07551f…`, equal to v26.10.0's `SHASUMS256.txt`, kept in the staging
  folder;
* `msiexec /i … /qn /norestart /l*v` exit 0; `node.exe --version` v26.13.0; the User `Path`
  unchanged;
* read again in a new ssh session (`windows-after.json`): every npm global answers as before, and
  `tsx` reports `node v26.13.0`; `python` 3.14.10; `go1.27.3`.

#### P5d — the audit over every host

**Step 1.** magic-git's `run_probe.py local ssh:<Linux> ssh:<Windows>` from this Mac, and
`wsl:Ubuntu-24.04` on the Windows host (`leftovers/p5d_wsl_probe.py`, remote copies removed), into
`leftovers/0217-p5/probes/`: four documents, each "snapshot: UNCHANGED". `version_audit.py
--probes` that directory, against the moved standard (`leftovers/0217-p5d-audit.log`): "-- 11
ADVISORY, 14 DRIFT, 1 HELD, 1 REVIEW, 5 STALE; exit 1".

* No Node row on any host. No Python row on any host; the standard's Python is `HELD`.
* The standard's rows are P5b step 2's.
* Every host: `DRIFT go`, the Go bin directory's tools built with go1.27.2 (13 of 14 on this Mac,
  12 of 12 on the Linux and Windows hosts, 11 of 11 on WSL); `ADVISORY go 1.27.2: GO-2026-6809`, a
  kept install; on the Windows host also `ADVISORY go 1.27.1`, 16 advisories.
* Every host: `DRIFT flutter 3.47.9 (standard 3.47.5)` and `DRIFT dart 3.13.7 (standard 3.13.4)`.
* Every host: `ADVISORY java 25.0.4.1`, the OpenJDK advisory of 2026-10-24; on WSL also the
  distribution's Java 21.0.12.1.
* The Linux host and WSL: `DRIFT git 2.55.0 (standard 2.56.0)`, the distribution's git.

Step 2 expected P5b's rows and this Mac's Flutter drift only.

#### Deviation 13 (2026-10-30): the audit shows Go, Flutter, Java and git rows on the hosts

**Found** at P5d step 2. None comes from P5's changes, which moved Node only. The Go rows carry
active advisories, which D1's rule brings into scope.

**Decision (owner, 2026-10-30): "Fix the Go rows in P5".** The MADR's D14 is amended. A new step,
P5e:

* on each host, every tool in the Go bin directory not built with go1.27.3 is rebuilt with it,
  `GOTOOLCHAIN=local go install <path>@<version>`, at the module version the binary records
  (`go version -m`), and read back;
* each old Go install (this Mac's `~/.local/go1.27.2`, the Linux host's and WSL's
  `~/sdk/go1.27.2`, the Windows host's `sdk\go1.27.1` and `sdk\go1.27.2`) is removed once no shell
  file, unit, User `Path` or `PATH` on that host names it;
* P5d is run again.

The Flutter and Dart rows wait for magic-git's pair (D2), the Java rows are D9's, and the
~~distribution's git is left to the distribution (0169 D6)~~ git 2.55.0 rows are 0169 D7's own open
item, with no advisory, so the scope rule keeps them out (*corrected 2026-10-30: the question to the
owner cited 0169 D6, which is Python; git is D7*). P5d's step 2 and A10 name them. Declined: naming
every row and moving the Go work to its own pair, and recording the rows without acting.

**Scope.** No file is added here. On each host: its Go bin directory and its old Go installs.

#### P5e — the Go tools and the old Go installs (Deviation 13)

`leftovers/p5e_go.py` on each host: `--check`, then `rebuild`, then `remove` (logs
`leftovers/0217-p5/go-<host>-*.txt`).

* **What blocks a removal.** The first check matched the bare string `go1.27.2`, which the dotfiles'
  `go/.config/go/apply-go-env.py:40` and `check-go-env.py` use as a `GOTOOLCHAIN` value, not a
  path. The active `GOTOOLCHAIN` is go1.27.3. The check was narrowed to the install's directory
  (`sdk/go1.27.N`, `.local/go1.27.N`, `sdk\go1.27.N`) in shell files, the dotfiles, systemd units,
  Go's env file, the login `PATH`, and on Windows the User and Machine `Path` and `GOROOT`.
  References in backups (`*.bak*`, `*.prev`) and in records (`*.md`, `docs/`) are listed as
  inactive. The two dotfiles scripts are left to the owner: their go1.27.2 tables are stale against
  the go1.27.3 the hosts already ran.
* **`rebuild`** asserts the `go` on `PATH` is go1.27.3, then `GOTOOLCHAIN=local go install
  <path>@<version>` at the version each binary records, and reads each back with `go version`.
* **`treefmt` v2.5.0** failed on every host: "create zip: test/examples/emoji 🕰️/README.md:
  malformed file path". The proxy lists v2.5.0 and answers 404 for it. The installed binary was
  built from a checkout (`vcs.revision=7ab41ba4…`). `leftovers/p5e_treefmt.py` rebuilt it the
  same way: a shallow clone of tag v2.5.0, checked to be `7ab41ba4…`, `GOTOOLCHAIN=local go install
  .`, then `go version -m` showing go1.27.3, module v2.5.0 and that revision; the clone removed.

| Host | Tools rebuilt with go1.27.3 | Old install removed | Active references |
| :--- | :--- | :--- | :--- |
| This Mac | 13 (`govulncheck` already go1.27.3), `treefmt` from its tag | `~/.local/go1.27.2` | none; the dotfiles' `0011-PLAN` record only |
| Linux test host | 12, `treefmt` from its tag | `~/sdk/go1.27.2` | none; `s8b-live.service.prev:50`, a backup |
| WSL | 11, `treefmt` from its tag | `~/sdk/go1.27.2` | none |
| Windows test host | 12, `treefmt.exe` from its tag | `sdk\go1.27.1`, `sdk\go1.27.2` | none |

Each removal printed "gone", and each host's `go version` stayed go1.27.3 with its `GOROOT` at
`go1.27.3`.

**P5d, again.** The four probes again, each "snapshot: UNCHANGED"; the audit
(`leftovers/0217-p5d-audit-2.log`): "-- 6 ADVISORY, 10 DRIFT, 1 HELD, 1 REVIEW, 5 STALE; exit 1".

* No Go, Node or Python row on any host.
* The standard's rows are P5b step 2's, with Python `HELD`.
* Every host: `DRIFT flutter 3.47.9` and `DRIFT dart 3.13.7` (magic-git's pair, D2);
  `ADVISORY java 25.0.4.1` (D9), and on WSL the distribution's Java 21.0.12.1.
* The Linux host and WSL: `DRIFT git 2.55.0` (0169 D7).

Each row is one Deviation 13 names. **A10 is met** as amended there.

**P5d step 3, 0169's records:** its PLAN's "0217 status note" has the P5 line, and its MADR has a
pointer under D1 and under D6. The 0169 PLAN's markdownlint count is 42 issues before and after
this edit, the same under the repository's config in a scratch clone of `HEAD`; none is on the new
lines.

**P5's verification is met:** P5a step 4's plants behave as the table says; P5a step 5's gates pass
but for Deviation 9's pre-existing watcher tests; P5b step 2 and P5d show only the rows named.

**Left with the owner:**

* magic-git: `version_audit.py`, `README.md` and `standard.json`, staged there;
* the Linux test host's dotfiles: `bash/.bashrc.d/00-paths.sh` and the `mcremote` drop-in
  `path.conf`;
* the push of `896834a5` (P4) and of P5's commit here, and CI on each;
* this plan becomes `complete` when those land and their CI is recorded.

**The Linux host's daemon, restarted** (the owner's word, 2026-10-30: "you have access and
permissions to restart mcremote on the linux host"). `leftovers/p5c_linux_restart.py`:
`systemctl --user restart mcremote` exit 0. Before, `MainPID` 1356, active since 2026-10-22, with
`node-v24.21.0` on its `PATH`. After, `MainPID` 2399574, active and running.

* The script's own `PATH` check reported neither Node directory. It read
  `/proc/<pid>/environ` the moment the unit turned active, and the value it compared was not
  looked at first.
* `leftovers/p5c_linux_daemon_env.py` then read the running process: its `PATH` names
  `~/sdk/node-v26.13.0/bin`, `node` resolves there, and the unit's `Environment` `PATH` is the same.

The daemon runs on Node 26.13.0.

**Files staged for the P5 commit here:** this PLAN, the 0217 MADR, and the 0169 PLAN and MADR. The
0219 pair stays untracked.

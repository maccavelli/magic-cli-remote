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
3. the Go modules and `go-selfupdate-lib` v1.23.0;
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

### P3 — Go modules and `go-selfupdate-lib` v1.23.0 (D4, D5)

**Files:** `go.mod`, `go.sum`, `.github/workflows/ci.yml`, the 0207 MADR (amendment J), and this
plan.

1. `go get -u ./...`, then `go mod tidy`. Then:
   * `go.mod` still says `go 1.27.3`, and has gained no `toolchain` line. A raised directive is a
     stop, because it means some dependency needs a newer Go;
   * `go-selfupdate-lib` is v1.23.0, `acp-go-sdk2` v1.26.0, `certmagic` v0.25.6 and `x/mod`
     v0.42.0;
   * `go list -m -u` shows no newer version for any direct module.

   The diff of `go.mod`'s `require` blocks is recorded, indirect modules included.
2. **`ci.yml:743` and `:764`:**
   `@67520be8af1ad1b6228c29e4b1aa3e61e1ca7ae4 # go-selfupdate-lib v1.23.0`. That is v1.23.0's
   commit, peeled through its annotated tag `e28ca1a1…`; re-peel it at P0.
   `TestWorkflowPinsMatchGoMod` (`internal/updateclient/spec_test.go:138`) holds both pins to
   `go.mod`.
3. **Read every caller of the changed behaviour** (F3). For each, record what v1.23.0 now returns
   and what Magic does with it:
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
6. **0207, amendment J:** "`go-selfupdate-lib v1.23.0`". It records:
   * F3's changes;
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

1. **The baseline APK.** At the P3 commit, run `make apk` and copy
   `build/app/outputs/flutter-apk/app-release.apk` to `leftovers/0217-p4/baseline.apk`. Without
   `key.properties` it is signed with this Mac's debug key, as the phase's APK will be, so one can
   be installed over the other. The AVD `mcremote_test` is `arm64-v8a`, which matches
   `--target-platform android-arm64`.
2. **The emulator baseline** (C2):
   * Boot the AVD.
   * Back up the app's data to `leftovers/0217-p4/appdata-before.tar`, with
     `adb exec-out run-as com.maccavelli.magic_cli_remote tar -cf - .`.
   * `adb install -r baseline.apk`.
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
   * `adb install -r` this phase's APK. `-r` keeps the app's data.
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
| A10 | Every host reads go1.27.3, Node v26.13.0 and Python 3.14.10, and its npm globals still answer. The audit over all four shows only the rows named | D14 |
| A11 | Every host change was made with a backup and the owner's word | C2, C4 |

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

**Files staged for the P2 commit (before the push, `1a25019a`):**

* `.github/workflows/ci.yml`, `docs.yml` and `ci-flake-ledger.yml`;
* `.node-version` and `.nvmrc`;
* `README.md`;
* `scripts/vulncheck.sh`;
* `Makefile`;
* the 0169 PLAN;
* this PLAN.

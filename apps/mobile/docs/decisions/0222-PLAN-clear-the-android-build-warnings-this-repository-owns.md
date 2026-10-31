---
status: in-progress
date: 2026-10-31
associated-madr: "0222-MADR-clear-the-android-build-warnings-this-repository-owns.md"
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Implement clearing the Android build warnings this repository owns

Associated MADR: [0222-MADR-clear-the-android-build-warnings-this-repository-owns.md](0222-MADR-clear-the-android-build-warnings-this-repository-owns.md)

## Goal

These warnings go from the Android build: W1, this app's W3 line, and W4 wherever this repository
runs Gradle or `apksigner` (MADR D1, D3, D4). `newDsl` stays `false` (D2). The app installs over
the previous build on the emulator with its pairing intact. A CI dispatch's APK job carries only
MADR D7's lines. With that,
[0217-PLAN-update-every-pin-to-its-current-release.md](../../../../docs/decisions/0217-PLAN-update-every-pin-to-its-current-release.md)'s
A6 closes.

## Scope

**Files:**

* `apps/mobile/android/gradle.properties`: lines 10 to 13 (D1, D2);
* `apps/mobile/android/app/build.gradle.kts`: line 41 and a comment above it (D3);
* `.github/workflows/ci.yml`: the `android-apk` job's `env`, and the `apksigner` call in its
  release-signature step (D4);
* `Makefile`: `GRADLE_OPTS` for `apk`, `manifest-surface` and `profile-apk` (D4);
* `scripts/build-apk.sh`: `GRADLE_OPTS` (D4);
* `apps/mobile/docs/guides/standards/mobile/android.md`: the "Build configuration" paragraph (D6);
* this pair, and `docs/README.md`'s two index rows for it.

**Not in scope:** `settings.gradle.kts` and every other build file (D5); the new DSL (D2); W2,
the plugins' W3 lines, and W5 (D7); Go; the dev hosts' shells.

## Constraints

* **C1 — One commit.** The agent stages. The owner commits with `git commit --no-edit`.
* **C2 — The owner's word comes first** for the emulator (S2, S5) and for the CI dispatch (S6).
  Asked for at approval.
* **C3 — No push without an explicit ask in the same turn.** `leftovers/guard_preview.py` before
  staging, and `leftovers/guard_range.py` before asking the owner to push.
* **C4 — `apps/mobile/android/key.properties` is not read.** `make apk` uses it; nothing else
  opens it.
* **C5 — Evidence goes in the record without identifiers.** Fingerprints are cut to their first
  eight characters, as 0217 recorded them.

## Implementation Steps

### P0 — Before the first write (read only)

1. `git status --short` is empty, or holds only work that is not this pair's, which is left
   alone. Record `HEAD`.
2. `flutter --version` is 3.47.9. `java -version` is 25. `gradle.properties:10-13` and
   `app/build.gradle.kts:41` read as the MADR quotes them.

### S1 — The checks, seen failing first (MADR Confirmation)

1. In the tree, before the change, CI's lint commands, from `apps/mobile`:
   `flutter build apk --config-only --release --target-platform android-arm64`, then in
   `android/`, `./gradlew -PmcAndroidLint=true :app:lintVitalRelease --no-daemon --rerun-tasks`.
   `--rerun-tasks` is there so the run recompiles, as CI does (MADR F12). Output to
   `leftovers/0222-s1-lint-before.log`.
2. Count, each with `grep -c -E`:
   * W1, `is no longer required for Kotlin support|'android\.builtInKotlin=false' is deprecated`:
     want 16;
   * W4, `A restricted method in java\.lang\.System`: want at least 1;
   * this app's W3, `app/build\.gradle\.kts:[0-9]+:[0-9]+: 'fun Project\.android`: 0 is
     expected here (F12: the build script is cached). The probes are its red: 1 in
     `agp9-kotlin-lint.log`, 0 in `agp9-fixes-lint.log`.

   W1 and W4 matching is those checks failing.

### S2 — The emulator baseline (C2)

1. Boot the AVD `mcremote_test`. Note the installed app's version, and its Settings fingerprints
   (Certificate pin, Client identity), as 0217 P4 did.
2. Back up the app's store with `leftovers/adb_backup.py`, as 0217 P4 step 2 did.
3. `flutter build apk --debug --target-platform android-arm64` at `HEAD`, copied to
   `leftovers/0222/before-debug.apk`. It is the rollback.
4. `make apk` at `HEAD`. Copy the APK to `leftovers/0222/before-release.apk`, for S4's comparison.
5. `actionlint .github/workflows/ci.yml` at `HEAD`, recorded as S4 step 8's baseline.

### S3 — The change (D1, D2, D3, D4, D6)

Each edit is an exact replace that asserts the old text first.

1. `gradle.properties`:

   ```properties
   # AGP 9's new DSL stays off: Flutter 3.47.9's Gradle plugin still needs the old DSL types,
   # and fails to apply under android.newDsl=true (MADR 0222 D2). Revisit when Flutter sets
   # android.newDsl=true (flutter/flutter#189236).
   android.newDsl=false
   # Built-in Kotlin, AGP 9's default (MADR 0222 D1). Keep the line: Flutter's migrator
   # re-adds android.builtInKotlin=false to a gradle.properties that lacks it.
   android.builtInKotlin=true
   ```

2. `app/build.gradle.kts:41`: `android {` becomes
   `configure<com.android.build.api.dsl.ApplicationExtension> {`. A comment above it says that
   this configures the same extension through its new-DSL interface. It avoids the deprecated
   `android { }` accessor that AGP generates under `newDsl=false` (MADR 0222 D3).
3. `ci.yml`, the `android-apk` job: `env: GRADLE_OPTS: --enable-native-access=ALL-UNNAMED`, with
   a comment naming MADR 0222 D4. In the release-signature step, `"$BT/apksigner" verify` becomes
   `"$BT/apksigner" -J-enable-native-access=ALL-UNNAMED verify`.
4. `Makefile`: `apk manifest-surface profile-apk: export GRADLE_OPTS += --enable-native-access=ALL-UNNAMED`,
   with a comment, written as the `Makefile`'s other target variables are.
5. `scripts/build-apk.sh`:
   `export GRADLE_OPTS="${GRADLE_OPTS:+$GRADLE_OPTS }--enable-native-access=ALL-UNNAMED"`, before
   its first Gradle call.
6. `android.md`'s "Build configuration" paragraph: AGP 9.1+ and Kotlin 2.4+; the two options and
   D2's revisit trigger; the typed block; the native-access option, and `GRADLE_OPTS` for a bare
   `flutter` build in a shell.
7. `git diff --stat` shows those six files only.

### S4 — Gates on this Mac

1. S1's commands again after the change, with `GRADLE_OPTS` set as the `Makefile` sets it, into
   `leftovers/0222-s4-lint-after.log`. W1 is 0 and W4 is 0. No W3 line names
   `app/build.gradle.kts`. Every other warning line is one of MADR D7's, counted by kind. Anything
   else is a stop.
2. The Kotlin compiler that built-in Kotlin used, from `./gradlew :app:compileReleaseKotlin
   --rerun-tasks --info`. If no line names it, the record says so, and the MADR's
   **[unverified]** note stays.
3. `make preflight`.
4. `make apk`: `assert-flutter-release-apk.sh` passes, no line matches "flutter support for your
   project", and W4 is 0 in its output. Copy the APK to `leftovers/0222/after-release.apk`.
5. `make manifest-surface` passes, and W4 is 0 in its output.
6. Compare `before-release.apk` and `after-release.apk`: `aapt2 dump badging` (package, version,
   SDK levels, permissions) and `apksigner -J-enable-native-access=ALL-UNNAMED verify
   --print-certs` (signer). They are the same, apart from what a rebuild changes. The second
   command is also F10's check, here: no W4 line.
7. `gradle.properties` still reads `android.builtInKotlin=true` after these builds (MADR F2).
8. `actionlint .github/workflows/ci.yml` (installed on this Mac) reports nothing new against the
   same command at `HEAD`, which S2 runs and records first.

### S5 — The emulator upgrade (C2)

1. `flutter build apk --debug --target-platform android-arm64` after the change, copied to
   `leftovers/0222/after-debug.apk`.
2. `adb install -r` it. Launch. It reconnects with no re-pair, and Settings shows the same two
   fingerprints as S2.
3. A re-pair prompt, a changed fingerprint, or a crash is a stop. Restore with
   `before-debug.apk` and S2's backup before anything else.
4. Shut the AVD down, as it was found.

### S6 — Records, staging, and CI

1. This plan's execution record: each step's output.
2. `python3 scripts/check_records.py --write-index`, keeping only this pair's two rows if the tree
   holds others' records; `make check-records`; `make markdownlint-docs`;
   `leftovers/guard_preview.py`.
3. Stage the Scope files. `check_records.py --check-all` on the staged tree. The owner commits.
4. `leftovers/guard_range.py` before asking the owner to push.
5. After the push: the push run's jobs pass, and `ci.yml` was accepted. Then, under C2,
   `gh workflow run ci.yml --ref master`. Every job passes. From the Android APK job's log:
   * W1 0, W4 0 in every step, and no W3 line for `app/build.gradle.kts`;
   * W2 once, the seven plugins' W3 lines, and W5's 13, counted;
   * the release-mode, signature and manifest-surface checks pass.
6. Record the run in this plan and in 0217's PLAN. 0217's A6 closes there, with MADR D7's lines
   named as upstream.

## Verification

| # | Criterion | MADR |
| :--- | :--- | :--- |
| V1 | S1's W1 count was 16 and W4's at least 1, and S4's are 0. The app's W3 line is 0 after the change, against 1 in the probe without D3 | D1, D3, D4 |
| V2 | The six Scope files are the only ones changed beside the records, and `gradle.properties` keeps its value after a `flutter` build | D1 to D6 |
| V3 | `make preflight`, `make apk` with its assert, and `make manifest-surface` pass. The release APK's badging and signer match the build before | D5 |
| V4 | The emulator upgrade keeps the pairing and both fingerprints | D5 |
| V5 | The CI dispatch passes, and its APK log has only MADR D7's lines of W1 to W5 | D1 to D4, D7 |

## Rollout and Rollback

* **Rollout:** the next release carries it. The APK's code and manifest do not change.
* **Rollback:** revert the commit. On the AVD, `adb install -r before-debug.apk`; both are signed
  with this Mac's debug key. If the store was reset, restore S2's backup.

## Owner decisions needed at approval

* **Q1 — The emulator (C2):** S2 and S5 boot the AVD, back up the store, and install over the
  paired app.
* **Q2 — The CI dispatch (C2):** S6 step 5 runs one `workflow_dispatch` of `ci.yml` after the push.

## Not done by this plan

* The new DSL: W2, and the seven plugins' W3 lines (MADR D2, D7).
* W5, in `flutter_foreground_task` and `speech_to_text` (MADR D7). Reporting it upstream would be
  outward-facing, and is the owner's call.
* W4 from a bare `flutter` command on a dev host. The standard says how to set `GRADLE_OPTS`;
  no host's shell is changed.
* The dev hosts' own APK builds. CI's Ubuntu build covers Linux.

## Execution record

**Approval, 2026-10-31.** The owner wrote "accept the madr, approve the plan, approved to use the
emulator, and approved to run a dispatch after push. proceed." The MADR is accepted, this plan is
approved, and Q1 and Q2 are answered yes.

**P0.** `HEAD` `6df89cc6`. The tree held this pair, untracked, and `docs/README.md`'s index rows
for 0221 and 0222. Nothing was staged. Flutter 3.47.9, Java 25 (Temurin 25.0.4.1).
`gradle.properties:10-13` and `app/build.gradle.kts:41` read as the MADR quotes them.

**S1** (`leftovers/0222_lint.py`, CI's two lint commands with `--rerun-tasks`;
`leftovers/0222-s1-lint-before.log`). Both commands exit 0. W1: **16**. W4: **1**. These are the
checks failing first. Also: W2 1, W5 13, the app's W3 line 0, no `BUILD FAILED`. The app's W3 line
is 0 here even with `--rerun-tasks`, because Gradle's script cache is not a task (MADR F12). Its
red is the probes'.

**S2.**

* AVD `mcremote_test` booted. The installed app was 0.1.0 (1), last updated 2026-10-30 14:57,
  0217 P4's debug build. Launched, it showed the green dot and "Connected to … over Mesh", with no
  re-pair. Settings, by host-side capture: **Certificate pin** `selfsigned · 23F21EDA…`; **Client
  identity** `PgZUp-lo…`; **Secret storage** "No failures recorded". These are the values 0217
  recorded.
* `leftovers/adb_backup.py` to `leftovers/0222/appdata-before.tar`: adb exit 0, 134 661 120 bytes,
  SHA-256 `5c35e91b…`, 37 members (`leftovers/0222-s2-backup.log`).
* `flutter build apk --debug --target-platform android-arm64` at `HEAD`: built. Copied to
  `leftovers/0222/before-debug.apk`.
* `make apk` at `HEAD`: "✓ Built … app-release.apk (41.3MB)", and "assert-flutter-release-apk: OK
  release-mode APK". Its output carried the four lines of W4. Copied to
  `leftovers/0222/before-release.apk`.
* `actionlint .github/workflows/ci.yml` at `HEAD`: exit 0, no output.

**S3.** The six edits, as written in S3 steps 1 to 6. In the `Makefile`:
`manifest-surface apk profile-apk: export GRADLE_OPTS += --enable-native-access=ALL-UNNAMED`, with
a comment above it. It is the file's first target-specific variable. `git diff --stat` showed the
six files, and `docs/README.md`'s rows, which are not this step's.

**S4** (logs `leftovers/0222-s4-*.log`):

* **Step 1** (`--native`, as the `Makefile` sets it): both commands exit 0. W1 **0**, W4 **0**, no
  W3 line for `app/build.gradle.kts`. Every other warning line was listed, and was W2 (1), W5 (13),
  or W6 and W7, which the MADR did not name. These were Deviations 1 and 2. Nothing else.
* **Step 2** (`./gradlew :app:compileReleaseKotlin --rerun-tasks --info`): two Kotlin compiler
  daemons. `kotlin-compiler-embeddable` 2.2.21, Gradle's embedded Kotlin, served
  `:gradle:compileKotlin`, which is Flutter's Gradle plugin. 2.4.0, the version
  `settings.gradle.kts:23` pins (`kotlin-build-tools-impl` 2.4.0), served every module's
  `compileReleaseKotlin`, `:app`'s included. The MADR's **[unverified]** note is answered: 2.4.0.
* **Step 3**, `make preflight`: `flutter analyze` "No issues found!", 1501 tests "All tests
  passed!" (3 skipped), "✅ preflight passed".
* **Step 4**, `make apk`: "✓ Built … app-release.apk (41.3MB)", "assert-flutter-release-apk: OK
  release-mode APK". No "flutter support for your project". W4 **0**, where the same target printed
  it before the change, so the `Makefile`'s `GRADLE_OPTS` reaches Gradle. Copied to
  `leftovers/0222/after-release.apk`.
* **Step 5**, `make manifest-surface`: "OK surface matches the allowlist". W4 0.
* **Step 6** (`leftovers/0222_apk_compare.py`): `aapt2 dump badging` of the two release APKs is
  identical, 126 lines each. The signer is the same, `CN=Magic CLI Remote`, SHA-256 `867c9f10…`.
  `apksigner -J-enable-native-access=ALL-UNNAMED` printed no W4 for either (MADR F10).
* **Step 7**: `gradle.properties` still reads `android.newDsl=false` and
  `android.builtInKotlin=true` after these builds.
* **Step 8**: `actionlint .github/workflows/ci.yml`: exit 0, no output, as at `HEAD`.

**S5.**

* `flutter build apk --debug --target-platform android-arm64` after the change: built. As D4 says,
  a bare `flutter` build printed W4's four lines. Copied to `leftovers/0222/after-debug.apk`.
* `adb install -r after-debug.apk`: "Success". `dumpsys package`: last updated 2026-10-31 12:53:30,
  signer `9fd42da8` as before.
* Launched: "Reconnecting with saved credentials…", then the sessions screen with the green dot and
  "Connected to … over Mesh". No re-pair.
* Settings: **Certificate pin** `selfsigned · 23F21EDA…`; **Client identity** `PgZUp-lo…`, each as
  in S2; **Secret storage** "No failures recorded"; **Recent errors** "No errors recorded". The
  backup was not needed.
* `adb emu kill`: the AVD is shut down, as it was found. It keeps this change's debug build.

**S6.**

* `check_records.py --write-index`, then `make check-records`: exit 0. `make markdownlint-docs`:
  57 files, "0 issues in 0 files", `android.md` among them. This pair: 0 issues.
  `leftovers/guard_preview.py`: exit 0, "would edit 0 file(s)". The guide's new relative link to
  this MADR resolves.
* Staged: the six Scope files, this pair, and `docs/README.md` as this pair's two rows only
  (`leftovers/stage_index_rows.py 0222`). The 0221 row, missing from the committed index and not
  this plan's, stays unstaged.
* `check_records.py --check-all` on the staged tree (`git checkout-index` into a scratch
  directory, removed afterwards): exit 0, no output.

### Deviations

**Deviation 1, 2026-10-31: a sixth warning kind, Flutter's Gradle plugin (S4 step 1).**

* **Found.** S4 step 1 lists every warning line after the change. Besides MADR D7's kinds it
  showed nine `w:` lines from the Flutter SDK's own Gradle plugin, under
  `flutter_tools/gradle/src/main/kotlin/`:
  * `Deeplink.kt:13,15,17,19`;
  * `FlutterPlugin.kt:12,664`;
  * `FlutterPluginUtils.kt:766`;
  * `PluginHandler.kt:168,169`.

  The same nine are in `leftovers/0222-s1-lint-before.log` (before the change), in the dispatch
  after 0217 P4 (`leftovers/0217-apk-dispatch-job.log`), and in P1's tag run
  (`leftovers/0217-p1-apk-tagjob.log`). They are pre-existing, Flutter's, and the change neither
  adds nor removes any. The MADR's inventory of five kinds missed them: its log searches matched
  AGP and pub-cache paths only.
* **Options put to the owner.** Add W6 to the record and continue (recommended); the same, with
  an upstream report filed by the owner; hold 0222.
* **Decision.** "Add W6 to the record (Recommended)". No code changes. S4 and S6 count W6 by kind,
  with the others.
* **Records changed.** MADR (amended): the inventory gains W6, and D7 names it as Flutter's. Here:
  this entry.

**Deviation 2, 2026-10-31: a seventh kind, Gradle's Gradle-10 deprecation summary (S4 step 2).**

* **Found.** S4 step 2's `--info` run ended with "Deprecated Gradle features were used in this
  build, making it incompatible with Gradle 10". The line is once in
  `leftovers/0222-s1-lint-before.log`, once in `leftovers/0222-s4-lint-after.log`, and once in
  the dispatch after 0217 P4. The lint was run again with `--warning-mode all`
  (`leftovers/0222-s4-warning-mode-all.log`): one deprecation, twelve times, Groovy's
  `propName value` assignment. The build files are:
  * `flutter_foreground_task-11.0.3/android/build.gradle`, lines 1, 2 and 27;
  * `flutter_local_notifications-22.3.3/android/build.gradle`, lines 1, 2, 25, 28 and 34;
  * `speech_to_text-7.5.0/android/build.gradle`, lines 1, 2 and 14.

  None is in this repository. It is pre-existing, and not in the MADR's inventory.
* **Options put to the owner.** Add W7 to the record and continue (recommended); the same, with
  upstream reports filed by the owner; hold 0222.
* **Decision.** "Add W7 to the record (Recommended)". No code changes. S6 counts the summary line.
* **Records changed.** MADR (second amendment): the inventory gains W7, and D7 names it as the
  three plugins' maintainers'. Here: this entry.

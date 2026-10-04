---
status: completed
date: 2026-09-23
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# PLAN 0168 — Build Android with JDK 21 in CI and on every dev host

Implements [0168-MADR-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md](0168-MADR-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md)
decisions D1–D5, closing findings F1, F2 and F4 (F3 is recorded, not acted on; F5 shapes
phase P3).

## Goal

- A release APK built locally on JDK 21 passes `scripts/assert-flutter-release-apk.sh`
  **before** CI changes (D5).
- `.github/workflows/ci.yml` `android-apk` sets up Temurin 21, and the owner-pushed run is green
  with its JDK step reporting 21.x (D1).
- The three bytecode-level lines in `apps/mobile/android/app/build.gradle.kts` are byte-for-byte
  unchanged (D2).
- Every dev host's `flutter config --list` shows a JDK 21 `jdk-dir`, and each host has built the
  APK on it (D3). JDK 17 is gone from a host only after that (D4).

## Scope

### In scope (the only files any phase may touch)

**Repository:** `.github/workflows/ci.yml` (one line), and this pair.

**Host configuration (outside the repository, each change approved per host, dated backup
first):**

- WSL: a Temurin 21 JDK under `~/sdk/`, the Flutter `jdk-dir`, and `~/.config/devenv.sh`'s
  `JAVA_HOME` line.
- Linux server: `java` in its dotfiles repository's mise config, and the Flutter `jdk-dir` if set.
- ~~Windows and macOS laptop: none. Both already build on 21, and P3 only verifies them.~~
  Windows: the Flutter `jdk-dir` (to the installed Temurin 21), and removal of the stale
  `~/.flutter_settings` after a dated backup (deviation 2026-09-23). macOS laptop: none, already
  on 21, verify only.

### Out of scope

- The bytecode target (D2, MADR option B).
- The macOS laptop's `JAVA_HOME`, which points at OpenJDK 26. Flutter's Android build uses
  `jdk-dir`, so it is recorded in the MADR and left alone.
- The rest of the dev-host standardization (Go tools, Node, mise), which the owner is handling
  separately.

## Stability rule

Each phase ends with:

```sh
make pre-add-check         # Go files: none change here, but the gate is the house rule
git diff --stat            # only the files this phase names
```

Commit at the end of P2 with `git commit --no-edit` (the pair itself is committed first, alone,
under the bootstrap exception). **`git push` and tags are not permitted** without an explicit ask
in the same turn. CI verification in P2 therefore waits for the owner's push.

## Cross-cutting contracts

- **C1 — Prove before depending.** CI changes only after P1's local JDK 21 build passes.
- **C2 — Build files untouched.** `build.gradle.kts`, `settings.gradle.kts`, the Gradle wrapper
  and `gradle.properties` are not edited in any phase.
- **C3 — Host changes are the owner's.** No host file is edited without that host's approval in
  the same turn. Each edit keeps a dated backup and goes through that host's own configuration
  source.
- **C4 — No JDK is removed before its replacement has built the APK on that host.**

The contract most at risk is **C1**. The CI edit is one line and looks obviously safe, which
makes it tempting to push it and let CI be the test. That would make CI the first JDK 21 build
of this project ever observed, which is exactly the claim MADR F4 marks unverified.

## Dependency and delivery order

P1, then P2 (needs P1 green), then P3 (hosts, any order, each after the owner approves it). P3
does not block P2.

## Implementation Steps

### P1 — A release APK on JDK 21, locally (D5; closes F4)

~~On the Windows host, whose Flutter `jdk-dir` is already Temurin 21.0.12:~~ On the Windows host,
after step 0 (deviation 2026-09-23):

0. Back up and remove the stale `~/.flutter_settings`. Run `flutter config --jdk-dir` pointed at
   `C:\Program Files\Eclipse Adoptium\jdk-21.0.12.8-hotspot`. `~/sdk/jdk-17` stays until D4.

1. `flutter config --list`: record the `jdk-dir` (expect the Temurin 21 path).
2. `make apk`: record the Gradle JDK line from the build log.
3. `scripts/assert-flutter-release-apk.sh apps/mobile/build/app/outputs/flutter-apk/app-release.apk`.
4. Confirm the bytecode level did not move. Check that `javap -v` on a class from the app's
   compiled Kotlin/Java intermediates reports major version 61 (Java 17).

**Verification:** the APK builds, the assert script exits 0, and the class major version is 61.
Seen-to-fail for step 4: run `javap` on a class compiled for 21, from any JDK 21 `javac` of a
one-line scratch file (major 65), to show the check can tell the two apart.

### P2 — CI on Temurin 21 (D1, D2; closes F1 for CI, F2)

1. `.github/workflows/ci.yml:604`: `java-version: "17"` becomes `java-version: "21"`. Nothing
   else in the file changes.
2. `git diff` shows exactly that one line. `build.gradle.kts` shows no diff (C2).
3. Commit (`git commit --no-edit`).
4. ~~**After the owner pushes:** the `android-apk` job's setup-java step reports Temurin 21.x, and
   the job is green.~~ A push does not run `android-apk` (tag or `workflow_dispatch` only). After
   the owner pushes, dispatch `ci.yml` on `master`; its setup-java step reports Temurin 21.x, and
   the job is green (deviation 2026-09-23, A4).

**Verification:** steps 2 and 4. Step 4 is the owner's push, then a read of the run log.

### P3 — Dev hosts on JDK 21 (D3, D4; closes F1 for hosts)

Per host, only after the owner approves that host:

- **WSL:** install Temurin 21 to `~/sdk/jdk-21` (official tarball, SHA-256 checked, mirroring
  `~/sdk/jdk-17`). Run `flutter config --jdk-dir ~/sdk/jdk-21`. Change `JAVA_HOME` in
  `~/.config/devenv.sh` to `~/sdk/jdk-21`, with a dated backup of the file first. Build the APK in
  a scratch clone on ext4. Only then remove `~/sdk/jdk-17`, with the owner's approval.
- **Linux server:** in its dotfiles repository, change mise's `java = "temurin-17"` to
  `"temurin-21"`. `mise install`. Verify `jdk-dir`/`JAVA_HOME` resolve to 21. Build the APK in a
  scratch clone. Then `mise uninstall java@temurin-17`, with approval.
- ~~**Windows, macOS laptop:** verify only.~~ **Windows:** switched in P1 step 0. Its JDK 17 is
  removed under D4 like the others. **macOS laptop:** verify only. `flutter config --list` shows a
  JDK 21 `jdk-dir`, and the APK builds.

**Verification:** `flutter config --list` and an APK build per host. The read-only
`devenv_probe.py` (magic-git `scripts/tools/devenv/`) is re-run afterwards, and its section 6
shows JDK 21 on all four hosts.

## Verification (whole plan)

### Acceptance criteria (mapped to MADR Confirmation)

| # | Criterion | MADR |
| --- | --- | --- |
| A1 | Local release APK built on JDK 21 passes the assert script | D5, F4 |
| A2 | App bytecode stays major version 61, and the check was seen to tell 61 from 65 | D2 |
| A3 | `ci.yml` diff is exactly the `java-version` line | D1 |
| A4 | CI `android-apk` green, with its JDK step reporting 21.x | D1 |
| A5 | `build.gradle.kts` unchanged | D2, C2 |
| A6 | Each host's `jdk-dir` is a JDK 21, and the host built the APK | D3 |
| A7 | No JDK 17 removed before A6 held on that host | D4 |

The criterion most likely to be quietly dropped is **A2's seen-to-fail half**. The bytecode check
passes trivially when nothing changed, so it proves nothing until it has rejected a
genuinely-21 class.

## Rollout and Rollback

Rollout is P1 → P2 → owner push → P3 per host. Rollback for CI is the one-line revert of P2. For
a host, restore the dated `devenv.sh` backup or revert the dotfiles commit, then run
`flutter config --jdk-dir` back to the 17 path. That path still exists until D4's removal.

## Deferred (named, so they are not mistaken for oversights)

- **A Java 21 bytecode target** (MADR option B): it needs its own reason (F3) and its own record.
- **The macOS laptop's `JAVA_HOME` → OpenJDK 26**: harmless to Flutter builds, and part of the
  wider host standardization the owner is running separately.

## Deviation — 2026-09-23: P1 found Windows on JDK 17

**Found.** P1's first `make apk` succeeded, and the assert script passed. But steps 1–2 showed
Flutter building with `jdk-dir: ~/sdk/jdk-17` (`flutter doctor -v`: Temurin-17.0.20.1). That
contradicts the MADR's measured table, which listed Windows as already on 21. So that build
proved nothing about JDK 21. The cause was the environment probe: it read a stale
`~/.flutter_settings` (Temurin 21, mid-August) instead of `%APPDATA%\.flutter_settings`, the
file Flutter reads on Windows. It was confirmed by locating both files and comparing each with
`flutter config --list` on all four hosts. Only the Windows row was wrong.

**Decision (owner, 2026-09-23): switch Windows, then P1.** P1 gains step 0: back up and remove
the stale file, then point `jdk-dir` at the installed Temurin 21.0.12. Windows joins the host
scope (Scope and P3 annotated above). `~/sdk/jdk-17` stays until D4. The MADR is amended (F1, D3,
the Windows row). The probe (magic-git `scripts/tools/devenv/devenv_probe.py`) now reads the
file Flutter reads and reports any other settings file as stale.

**Files added to scope:** none in the repository. Host-side: Windows `%APPDATA%\.flutter_settings`
(through `flutter config`) and the removed `~/.flutter_settings` (backed up first).

## Execution record (2026-09-23)

P1 and P2 ran in full, and P3 through its switch and verification steps. The plan stays
`in-progress`: A4 waits for the owner's push, and A7 (the JDK 17 removals) waits for the owner's
per-host approval.

| # | Result | Evidence |
| --- | --- | --- |
| A1 | met | Windows, after step 0: `flutter doctor -v` reports `Temurin-21.0.12+8`, `make apk` exits 0, and the assert script prints `OK release-mode APK` (40M). The first P1 attempt built on 17 (see Deviation) and is not counted. |
| A2 | met | All 3 app classes (`MainActivity`, `UpdateInstaller`, `UpdateInstallReceiver`) are class-file major **61**, compiled 12:45 during the JDK 21 build. `javap -v` agrees. Seen to fail first: a JDK 21 `javac` default-release class is major 65, and the check rejected it. |
| A3 | met | `git diff -U0` on `ci.yml` is exactly `-java-version: "17"` / `+java-version: "21"` (`0eb8ed1`). |
| A4 | pending | Needs the owner's push, then a read of the `android-apk` run log. |
| A5 | met | `git diff --quiet -- apps/mobile/android` exits 0. |
| A6 | met | WSL: Temurin 21.0.12.1 at `~/sdk/jdk-21` (Adoptium API, SHA-256 verified). Linux server: mise `temurin-21.0.12+101.0.LTS`. macOS laptop: Homebrew OpenJDK 21.0.12.1 (unchanged). Each host's `flutter doctor -v` reports 21, and `make apk` plus the assert script pass in a scratch clone, which is removed afterwards. A re-run of the environment probe shows JDK 21 in every host's Flutter settings or `JAVA_HOME`, with every shell-init snapshot unchanged. |
| A7 | holding | No JDK 17 removed: `~/sdk/jdk-17` on Windows and WSL, and mise `temurin-17` on the Linux server, all remain. Removal awaits the owner. |

### Host changes made (each with a dated backup)

- **Windows:** `%APPDATA%\.flutter_settings` `jdk-dir` now points at Temurin 21.0.12. The stale
  `~/.flutter_settings` was removed. Both originals are kept as `*.bak-2026-09-23` beside where they lived.
- **WSL:** `~/.config/flutter/settings` `jdk-dir` and `~/.config/devenv.sh`'s `JAVA_HOME` line now
  point at `~/sdk/jdk-21`. Both have `*.bak-2026-09-23-jdk21` copies.
- **Linux server:** in its dotfiles repository's mise config, `java = "temurin-17"` became
  `"temurin-21"`, a one-line string edit that leaves the rest of the TOML byte-identical. The
  file already carried an uncommitted change from 2026-09-18 (the Flutter 3.47.2 migration),
  which is left as it was. Neither change is committed; that repository is the owner's to commit.
  The backup is kept outside the repository.

### What the plan predicted incorrectly

- **The Windows starting state.** The MADR said Windows already built on 21. It was on 17: the
  environment probe had read a stale settings file that Flutter does not use (Deviation above).
  P1 as first written would have "proved" JDK 21 with a JDK 17 build. It was caught only because
  step 1 recorded `flutter config --list` rather than assuming the table.
- **Temurin patch levels differ.** Adoptium's current 21 GA for Linux is 21.0.12.1+1, while the
  Windows install is 21.0.12+8. Same major version and same LTS line, which is what D3 requires.
  CI's `java-version: "21"` will take whatever the current 21.x is.
- **Apple make 3.81 was expected to be a risk and wasn't.** `make apk` ran cleanly on the macOS
  laptop.

## Deviation — 2026-09-23: a push does not run `android-apk`

**Found.** The owner pushed `2b9f516`, and its CI run (`35910477320`) concluded `success`. But
`Android APK (release arm64)` was **skipped**: `ci.yml:589` gates it on
`github.ref_type == 'tag' || github.event_name == 'workflow_dispatch'`. P2 step 4 assumed a
branch push would exercise it, so A4 was not observed. The gate predates this plan and was not
changed by it: the job was skipped on every push before `0eb8ed1` too. The workflow's own comment
says the dispatch trigger exists so the job can be exercised before a release depends on it.

**Decision (owner, 2026-09-23): dispatch CI on `master`.** `release` and `publish` are gated
`github.ref_type == 'tag'`, so a dispatch on a branch publishes nothing. `android-apk` uploads a
workflow artifact only. P2 step 4 is annotated above. No files are added to scope.

## Closing record (2026-09-23)

The two criteria the execution record left open are now met, and the plan is `completed`.

- **A4 — met.** The owner pushed `2b9f516`; its push run skipped `android-apk` (see the second
  Deviation). A `workflow_dispatch` on `master`, run `35911349275`, concluded `success` on every
  job, `Android APK (release arm64)` included. Its setup-java step printed `java-version: 21` and
  `Resolved Java 21.0.12+1 from tool-cache` (`JAVA_HOME_21_X64`). `release` and `publish` were
  skipped, as their tag gates require.
- **A7 — met.** JDK 17 was removed only after each host had built the APK on 21 (A6), and on the
  owner's instruction:
  - Windows: `~/sdk/jdk-17`. An idle Gradle 9.1.0 daemon from the first P1 attempt still ran on
    it; it was identified by command line and stopped first. The JDK's read-only files
    (`classes.jsa`) needed their attribute cleared.
  - WSL: `~/sdk/jdk-17`.
  - Linux server: `mise uninstall java@temurin-17.0.20+8`, with no dangling alias left.
  - macOS laptop: none installed.

  A sweep of all four hosts afterwards found no JDK 17 install, no process running from one, and
  no live configuration referencing one. Only the dated backups do.

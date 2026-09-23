---
status: in-progress
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
- Windows and macOS laptop: none. Both already build on 21, and P3 only verifies them.

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

On the Windows host, whose Flutter `jdk-dir` is already Temurin 21.0.12:

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
4. **After the owner pushes:** the `android-apk` job's setup-java step reports Temurin 21.x, and
   the job is green.

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
- **Windows, macOS laptop:** verify only. `flutter config --list` shows a JDK 21 `jdk-dir`, and
  the APK builds.

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

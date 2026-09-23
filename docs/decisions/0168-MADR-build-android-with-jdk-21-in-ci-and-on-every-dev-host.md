---
status: accepted
date: 2026-09-23
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD036 MD060 -->

# MADR 0168: Build Android with JDK 21 in CI and on every dev host, keeping Java 17 bytecode

## Context and Problem Statement

The mobile app's Android build runs on a JDK in two places: CI's `android-apk` job and each
developer machine's `flutter build apk` (`make apk`). The owner is standardizing the four dev
hosts: Windows, WSL, a Linux server and a macOS laptop. They decided on 2026-09-23 that the
standard JDK is **21**, and that CI moves to it rather than every host moving back to 17.

Today the hosts and CI disagree. A release APK built on one machine was therefore produced by a
different compiler than the CI artifact, and a JDK-specific failure would show up on one side only.

### What was measured, not assumed

A read-only environment probe (`devenv_probe.py`, run on all four hosts on 2026-09-23, with
every shell-init file fingerprinted before and after and found unchanged) recorded the JDK each
host's Flutter uses for Android builds:

| Host | Flutter `jdk-dir` / JDK used | `JAVA_HOME` |
| --- | --- | --- |
| Windows | Temurin **21.0.12** (`~/.flutter_settings`) | Temurin 21.0.12 |
| WSL | Temurin **17.0.20.1** (`~/sdk/jdk-17`); a system OpenJDK 21.0.12 is also installed | `~/sdk/jdk-17` |
| Linux server | Temurin **17.0.20** via mise (`java = "temurin-17"`) | mise Temurin 17 |
| macOS laptop | Homebrew OpenJDK **21.0.12** (`openjdk@21`) | Homebrew OpenJDK **26.0.2** |
| CI `android-apk` | Temurin **17** | — |

The repository pins the rest of the Android toolchain like this:

- `.github/workflows/ci.yml:601-604`: `actions/setup-java` v6.0.0, `distribution: temurin`,
  `java-version: "17"`. The JDK is used by the `android-apk` job only; no other job sets up Java.
- `apps/mobile/android/app/build.gradle.kts:49-50`: `sourceCompatibility` and
  `targetCompatibility` are `JavaVersion.VERSION_17`.
- `apps/mobile/android/app/build.gradle.kts:106`: Kotlin `jvmTarget = JvmTarget.JVM_17`.
- `apps/mobile/android/gradle/wrapper/gradle-wrapper.properties:5`: Gradle **9.1.0**.
- `apps/mobile/android/settings.gradle.kts:22-23`: AGP **9.0.1**, Kotlin Gradle plugin **2.3.20**.
- `.github/workflows/ci.yml:23`: Flutter **3.47.2**, matching every host.

The only local release APK on the Windows host (`apps/mobile/build/app/outputs/flutter-apk/`,
2026-09-18) predates that host's switch to JDK 21. **No build of this project on JDK 21 has
been observed.** That is the claim this record's plan must establish first.

### Findings

- **F1 — the build JDK differs between CI and the dev hosts.** CI uses 17. Two hosts build with 21
  and two with 17.
- **F2 — the build JDK and the bytecode target are separate settings.** CI's `java-version`
  chooses the JDK that runs Gradle and `javac`/`kotlinc`. `build.gradle.kts` pins the bytecode
  level those compilers emit (17). The owner's decision concerns the first.
- **F3 — raising the bytecode target gains nothing on Android.** The app's Java/Kotlin bytecode
  is re-dexed by D8 for Android's runtime. Language features beyond the target are desugared or
  unavailable according to `minSdk`, not the host JDK. A 21 target would widen the surface for
  D8/R8, lint and desugaring (`coreLibraryDesugaring`, `build.gradle.kts:111`) and buy no
  runtime capability. **[unverified — reasoned from how D8 works, not measured here]**
- **F4 — JDK 21 is within the declared support of the pinned toolchain.** Gradle 9.x runs on
  JDK 17 and later. AGP 9.0 requires JDK 17 or later. Kotlin 2.3 compiles to a JVM 17 target
  from JDK 21. **[unverified against this project — external compatibility statements, to be
  established by the plan's first phase, a real build]**
- **F5 — the host side of the change is not repository content.** Flutter's `jdk-dir` and the
  installed JDKs live in each host's home directory and dotfiles. The plan must treat them as
  owner-approved host changes, not repository edits.

## Decision Drivers

- One compiler for every artifact: CI's APK and a locally built APK should come from the same
  JDK major version.
- The owner's standard: JDK 21 everywhere (decided 2026-09-23).
- Least blast radius. Change what builds, not what is built, unless there is a reason to.
- Prove it before CI depends on it. No JDK 21 build of this project has been observed.

## Considered Options

- **A — JDK 21 as the build JDK in CI and on every host; bytecode target stays 17.**
- **B — JDK 21 build JDK and Java 21 bytecode target.**
- **C — Keep CI on 17 and move every host back to 17.**
- **D — Leave it mixed.**

## Decision Outcome

Chosen option: **A**, because it meets the owner's standard with the smallest change that makes
CI and the hosts agree. It touches one line of CI and each host's JDK selection, and no build
script. It leaves the emitted bytecode, and therefore the APK's runtime behaviour, exactly as
it is.

### The decisions

- **D1 — CI builds Android with Temurin 21.** `.github/workflows/ci.yml` `android-apk` sets
  `java-version: "21"` (distribution stays `temurin`). No other workflow line changes.
- **D2 — The bytecode target stays Java 17.** `sourceCompatibility`, `targetCompatibility` and
  `jvmTarget` in `apps/mobile/android/app/build.gradle.kts` are unchanged. Raising them is a
  separate decision that needs its own reason (F3).
- **D3 — Every dev host's Flutter builds Android with a JDK 21.** Temurin is preferred where the
  host's installer offers it. The WSL host moves `jdk-dir` off `~/sdk/jdk-17`; the Linux server
  moves mise's `java` from `temurin-17` to `temurin-21`. Windows is already on 21. The macOS
  laptop's Flutter is already on OpenJDK 21. Its `JAVA_HOME`, which points at Homebrew's
  OpenJDK 26, is recorded but not changed by this decision: Flutter's Android build uses
  `jdk-dir`, not `JAVA_HOME`.
- **D4 — JDK 17 is removed from a host only after that host has built the APK on 21.** Host
  changes are made one host at a time, each approved by the owner, each through that host's own
  configuration (dotfiles repository, or the untracked WSL `devenv.sh`), with a dated backup.
- **D5 — The switch is proven before CI depends on it.** The plan's first phase builds the
  release APK on JDK 21 locally and passes it through `scripts/assert-flutter-release-apk.sh`
  before the workflow line changes.

### Consequences

- Good, because a locally built APK and CI's APK come from the same JDK major version.
- Good, because the change is one workflow line plus host configuration; the Android build
  files and the APK's bytecode level are untouched.
- Good, because D5 turns F4 from a compatibility claim into an observed build before CI relies
  on it.
- Neutral, because Gradle's daemon and build cache are keyed by JDK. The first CI run on 21
  starts cold.
- Bad, because the hosts end up with two JDKs until D4's removals, and a host that forgets its
  `jdk-dir` silently builds on the old one. The plan checks each host's `jdk-dir` explicitly for
  that reason.

### Confirmation

```sh
# local proof before CI changes (D5), on a host whose Flutter jdk-dir is JDK 21:
flutter config --list | grep jdk-dir          # expect a JDK 21 path
make apk                                      # expect a release APK
scripts/assert-flutter-release-apk.sh apps/mobile/build/app/outputs/flutter-apk/app-release.apk

# CI (D1), after the owner pushes:
#   android-apk's "Set up JDK" step reports a 21.x Temurin, and the job is green.
grep -n 'java-version' .github/workflows/ci.yml   # expect: java-version: "21"

# D2 unchanged:
grep -n 'VERSION_17\|JVM_17' apps/mobile/android/app/build.gradle.kts   # expect three lines, unchanged
```

## Pros and Cons of the Options

### A — JDK 21 build JDK, Java 17 bytecode (chosen)

- Good, because it satisfies the standard with one workflow line.
- Good, because the APK's bytecode, and therefore its runtime behaviour, does not change.
- Bad, because "we use Java 21" becomes true of the toolchain but not of the language level,
  which a reader can misread. D2 states it explicitly for that reason.

### B — JDK 21 and a Java 21 target

- Good, because the language level matches the JDK, and Java 21 language features become usable
  in the (small) Java/Kotlin layer.
- Bad, because it changes what is built, not only what builds. D8/R8, lint and desugaring all see
  new bytecode, and none of that is needed (F3).
- Bad, because a regression would show up in the APK, not the build, which makes it harder to
  catch.

### C — Everything on 17

- Good, because CI needs no change and the target equals the JDK.
- Bad, because it contradicts the owner's decision and reverses two hosts that already moved.

### D — Leave it mixed

- Good, because it is free.
- Bad, because F1 stays true: artifacts built by different compilers, and JDK-specific failures
  seen by only one side.

## More Information

### Evidence index

| Claim | Source |
| --- | --- |
| Per-host JDKs and `jdk-dir` | `devenv_probe.py` outputs, 2026-09-23 (magic-git `scripts/tools/devenv/`), section 6 of its report |
| CI JDK is Temurin 17, `android-apk` only | `.github/workflows/ci.yml:601-604` |
| Bytecode target 17 | `apps/mobile/android/app/build.gradle.kts:49-50`, `:106` |
| Gradle 9.1.0 / AGP 9.0.1 / Kotlin 2.3.20 | `gradle-wrapper.properties:5`, `settings.gradle.kts:22-23` |
| Flutter 3.47.2 on CI and all hosts | `ci.yml:23`; probe section 6 |
| No JDK 21 build observed yet | only local APK dated 2026-09-18, before the Windows `jdk-dir` moved to 21 |
| JDK 21 supported by Gradle 9 / AGP 9 / Kotlin 2.3 | **[unverified here]**: vendor compatibility statements, established by PLAN P1 |

### Related records

None. No earlier record decided the JDK. CI's Temurin 17 predates the decision records.

### Open questions for the plan

- Which JDK 21 does the WSL host use: a Temurin 21 tarball in `~/sdk/jdk-21`, mirroring its
  Temurin 17 layout, or the already-installed system OpenJDK 21? The plan proposes Temurin, for
  parity with CI.

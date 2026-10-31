---
status: accepted
date: 2026-10-31
decision-makers: Project Owner
consulted: "Flutter 3.47.9's flutter_tools (templates/app/android.tmpl/gradle.properties.tmpl; lib/src/android/migrations/disable_built_in_kotlin_migration.dart and disable_new_dsl_migration.dart; lib/src/android/gradle_errors.dart; gradle/src/main/kotlin/FlutterPluginUtils.kt); Flutter master's app template and issues #180137 and #189236; CI run 41210038095 and P1's tag run (APK jobs 124564856862 and 124201755019); build-tools 36.0.0's apksigner script; the scratch probes leftovers/agp9_probe.py and leftovers/agp9_probe2.py"
informed: anyone who builds the Android app, on a dev host or in CI
---
<!-- markdownlint-disable MD004 MD013 MD024 MD060 -->

# Clear the Android build warnings this repository owns: built-in Kotlin, a typed android block, and native access

## Context and Problem Statement

[0217-MADR-update-every-pin-to-its-current-release.md](../../../../docs/decisions/0217-MADR-update-every-pin-to-its-current-release.md)
says the APK build shows no AGP, Gradle or Kotlin warning. The CI dispatch after its P4 (run
41210038095) showed this was not true in CI. P1's tag run, before any 0217 build-tool change,
shows the same warnings. 0217's PLAN, Deviation 15, records them, and the owner's decision to
resolve what this repository can in a new pair. This is that pair.

The APK job's log carries five kinds of warning:

| # | Warning | Printed by | Where |
| :--- | :--- | :--- | :--- |
| W1 | "The option setting 'android.builtInKotlin=false' is deprecated", and "The 'org.jetbrains.kotlin.android' plugin … is no longer required for Kotlin support since AGP 9.0" with a "Solution" for `:app` and 14 plugins | AGP 9.1.1 | every configure |
| W2 | "The option setting 'android.newDsl=false' is deprecated" | AGP 9.1.1 | every configure |
| W3 | "'fun Project.android(…)' is deprecated … will be removed in AGP 10.0", for `app/build.gradle.kts:41` and for seven plugins' build scripts | the Kotlin compiler, compiling build scripts | when a script is compiled |
| W4 | "A restricted method in java.lang.System has been called", from Gradle 9.3.1's `native-platform`, and from `apksigner`'s `conscrypt` in CI's signature step | the JDK, Temurin 25 in CI and on this Mac | every Gradle run, every `apksigner` run |
| W5 | Thirteen compiler warnings in plugin source code: nine in `flutter_foreground_task` 11.0.3 (eight "Deprecated in Java", one "No cast needed"), four in `speech_to_text` 7.5.0 (one "Deprecated in Java", three "Unnecessary safe call") | the Kotlin compiler, compiling plugin code | when the plugins compile |

*Amended 2026-10-31 (PLAN Deviation 1): there is a sixth kind.* **W6:** nine Kotlin compiler
warnings in the Flutter SDK's own Gradle plugin, which Gradle builds from
`flutter_tools/gradle/src/main/kotlin/` in every Android build. They are in `Deeplink.kt` (four,
"This annotation is currently applied to the value parameter only"), `FlutterPlugin.kt` (two,
"'interface ApkVariant…' is deprecated"), `FlutterPluginUtils.kt` (one, "Condition is always
'true'") and `PluginHandler.kt` (two, "Unchecked cast"). They are printed by the Kotlin compiler
when that plugin compiles. The same nine are in P1's tag run, in the dispatch after P4, and in
this tree before and after this decision's change. They were missed because the log searches
behind the table matched AGP and pub-cache paths only.

*Amended 2026-10-31 (PLAN Deviation 2): and a seventh.* **W7:** Gradle's one-line summary
"Deprecated Gradle features were used in this build, making it incompatible with Gradle 10", once
per build. Under `--warning-mode all` it is one deprecation, twelve times: assigning a property
with Groovy's `propName value` form, which Gradle 10 removes. The properties are `group`,
`version`, `namespace`, `coreLibraryDesugaringEnabled` and `multiDexEnabled`. All twelve are in
three plugins' Groovy build scripts under the pub cache: `flutter_foreground_task` 11.0.3,
`flutter_local_notifications` 22.3.3 and `speech_to_text` 7.5.0. This repository's build scripts
are Kotlin DSL and use none of them. The summary is in this tree before and after the change, and
in the dispatch after 0217 P4. The default output prints only the summary line, which no search
matched.

W1 and W2 come from the two AGP options that Flutter's template writes into
`apps/mobile/android/gradle.properties:11,13`:

```properties
android.newDsl=false
android.builtInKotlin=false
```

AGP 10 removes both options. The question: which of these can this repository clear on
Flutter 3.47.9, how, and what is left, waiting on whom?

### What Flutter 3.47.9 does with the two options

* **F1 — Flutter writes both as `false`.** `templates/app/android.tmpl/gradle.properties.tmpl`
  ends with the two lines above. Flutter master's template, read on 2026-10-31, still does.
* **F2 — Flutter puts them back when they are missing.** `disable_built_in_kotlin_migration.dart`
  and `disable_new_dsl_migration.dart` append `…=false` when the property is absent. When it is
  present, with either value, they skip it ("The developer has already configured the … flag,
  skipping migration"). So a deleted line comes back on the next `flutter` build, and a set one
  is kept.
* **F3 — Flutter's Gradle plugin reads built-in Kotlin.** `FlutterPluginUtils.kt:706-728`
  (`isBuiltInKotlinEnabled`) reads `android.builtInKotlin`, and defaults it to `true` on AGP 9.
* **F4 — Flutter's Gradle plugin still needs the old DSL.** `gradle_errors.dart`
  (`useNewAgpDslErrorHandler`) says: "Starting AGP 9+, only the new DSL interfaces are used. This
  results in a failure because we still depend on old DSL types."
* **F5 — Flutter plans to move.** Issue #189236, open, "Use Migrator Tool to Automatically Opt-in
  to newDsl in 3.50": once its own work is done (#180137, open), Flutter's migrator sets
  `android.newDsl=true`, "There should be no action required by developers".

### What the probes showed (2026-10-31)

Every variant ran in a `git archive` copy of `HEAD`, never in the tree, with the commands of CI's
lint step (`ci.yml:585-587`), then `flutter build apk --release --target-platform android-arm64`.
`leftovers/agp9_probe.py`:

| Variant | Lint | Release APK | Left |
| :--- | :--- | :--- | :--- |
| as committed | exit 0 | built | W1 (16 matches), W2, W4 |
| `builtInKotlin=true` | exit 0 | built | W2, W3 (8: the app and seven plugins), W4. W1: 0 |
| both options `true` | exit 1 | not built | "Failed to apply plugin 'dev.flutter.flutter-gradle-plugin'", "`ApplicationExtensionImpl` … cannot be cast to … `AbstractAppExtension`", then Flutter's box "Starting AGP 9+, only the new DSL interface will be read" |

`leftovers/agp9_probe2.py`, variant `fixes`: `builtInKotlin=true`, `app/build.gradle.kts`'s
`android {` replaced by `configure<com.android.build.api.dsl.ApplicationExtension> {` with the
body unchanged, and `GRADLE_OPTS=--enable-native-access=ALL-UNNAMED`. Lint exit 0, release APK
built, and only W2 left.

* **F6 — Built-in Kotlin works with this app as it is.** The app's Kotlin (`MainActivity.kt`,
  `UpdateInstaller.kt`) compiled with no warning. The `kotlin { compilerOptions { jvmTarget =
  JVM_17 } }` block was accepted. The app's `plugins { }` block already applies no
  `kotlin-android`. Flutter left the line as set (F2).
* **F7 — The new DSL fails the build on Flutter 3.47.9,** as F4 says.
* **F8 — A typed `configure<ApplicationExtension>` clears this app's W3 line.** W3 is the
  deprecated Kotlin accessor that AGP generates for `android { }` when `newDsl=false`. Configuring
  the same extension by its new-DSL interface, `com.android.build.api.dsl.ApplicationExtension`,
  avoids the accessor. The changed script compiled with no warning, and its body took effect: the
  lint run's configure printed the script's own "key.properties not found" fallback warning, from
  inside `buildTypes`.
* **F9 — `GRADLE_OPTS=--enable-native-access=ALL-UNNAMED` clears Gradle's W4.** It is the option
  the JDK's warning names ("Use --enable-native-access=ALL-UNNAMED to avoid a warning"). It grants
  the native access Gradle needs rather than hiding the message. The warning comes from Gradle's
  launcher JVM, before the daemon, so `org.gradle.jvmargs` in `gradle.properties` does not reach
  it. The `gradlew` script is generated, not tracked. 1 match per Gradle run without it, 0 with
  it, lint and build both.
* **F10 — `apksigner -J-enable-native-access=ALL-UNNAMED` clears `apksigner`'s W4.** The script
  turns each `-J<opt>` into a JVM option (build-tools 36.0.0, `apksigner:59-73`).
  `apksigner verify --print-certs` on P4's debug APK printed four warning lines without it and
  none with it, with the same signer lines.
* **F11 — The seven plugins' W3 lines follow W2.** Each says the deprecated class "is not used for
  the public extensions in AGP when android.newDsl=true". They are the same accessor as F8's, in
  build scripts this repository does not own. They go when `newDsl` is `true`. That cannot be
  shown before Flutter allows it (F7).
* **F12 — A local build can hide W3 and W5.** Gradle caches compiled build scripts and plugin
  classes. A local run shows them only when it recompiles, as CI always does. In the probe
  `fixes`, only the changed `app` script was recompiled. The plugins' W3 and W5 lines stay in CI
  after this pair.

**[unverified]** Which Kotlin compiler built-in Kotlin uses here: the Kotlin Gradle plugin 2.4.0
on the classpath (`settings.gradle.kts:23`), or AGP's own. The PLAN records it.

## Decision Drivers

* **D-1 — Clear every warning this repository owns.** 0217's A6 stays open until that is done.
* **D-2 — Fix, do not hide.** A change that removes the cause, or grants what the JDK asks for,
  is in. A setting that only silences the message is not.
* **D-3 — No change Flutter undoes or does not support.** F2 undoes a deleted line. F4 and F7
  rule out the new DSL on 3.47.9.
* **D-4 — The app behaves the same.** The same signing, manifest surface, bytecode target (0168
  D2), and the same paired store after an upgrade.
* **D-5 — AGP 10 is coming.** Whatever can move now should, so that only what waits on Flutter is
  left.

## Considered Options

* **A — Clear what this repository owns: built-in Kotlin, a typed `android` block, and native
  access where this repository runs Gradle and `apksigner`. Keep `newDsl=false` until Flutter
  sets it.**
* **B — Built-in Kotlin only.**
* **C — Set both options to `true`.**
* **D — Delete both lines, and take AGP's defaults.**
* **E — Silence the rest.** AGP's option-warning suppression for W2, and dropping the build output
  that carries W3 and W5.
* **F — Change nothing until Flutter sets `newDsl=true`.**

## Decision Outcome

Chosen option: "A", because it clears W1, this app's W3 line, and W4 wherever this repository
runs Gradle or `apksigner`, each by removing the cause (F6, F8, F9, F10). What is left is not this
repository's to change: W2 and the plugins' W3 lines wait on Flutter (F5, F11), and W5 waits on two
plugins' maintainers. B leaves warnings that can go. C fails (F7). D is undone (F2), or fails as C
does. E hides rather than fixes (D-2). F leaves everything that can go today.

### The decisions

* **D1 — `android.builtInKotlin=true`.** The line stays in `gradle.properties`, set rather than
  deleted (F2). Its comment says why it is set, and that Flutter would re-add `false` if it were
  removed.
* **D2 — `android.newDsl=false` stays.** Its comment names F4. **Revisit** when a Flutter release
  sets `android.newDsl=true`, by its migrator (#189236) or its template. That change is a record
  of its own: it removes W2 and, by F11, the plugins' W3 lines.
* **D3 — `app/build.gradle.kts` configures `android` by its interface.** Line 41's `android {`
  becomes `configure<com.android.build.api.dsl.ApplicationExtension> {`. The body is byte for byte
  the same, and a comment says why (F8). It stays valid after D2's revisit: under `newDsl=true`
  the extension *is* an `ApplicationExtension`.
* **D4 — Native access, where this repository runs Java.** `--enable-native-access=ALL-UNNAMED`
  (F9, F10):
  * CI's `android-apk` job: `GRADLE_OPTS` in the job's `env`, and `apksigner
    -J-enable-native-access=ALL-UNNAMED` in the release-signature step;
  * the `Makefile`'s Android targets, `apk`, `manifest-surface` and `profile-apk`: `GRADLE_OPTS`
    exported for those targets, keeping any value the caller set;
  * `scripts/build-apk.sh`: the same.

  A bare `flutter build` or `flutter run` on a dev host, outside these, still prints Gradle's W4.
  No tracked file reaches it. The standard (D6) says to set `GRADLE_OPTS` in the shell for that.
* **D5 — Nothing else in the build changes.** `settings.gradle.kts` keeps AGP 9.1.1 and the
  Kotlin Gradle plugin 2.4.0 with `apply false`, since 14 plugins still apply `kotlin-android`.
  The `kotlin { compilerOptions }` block, the Java 17 lines (0168 D2), signing and lint stay as
  they are.
* **D6 — The Android standard says so.** `guides/standards/mobile/android.md`'s build paragraph
  says AGP 9.1+ and Kotlin 2.4+, which 0217 D8 made true. It names the two options and D2's
  revisit trigger, the typed block (D3), and the native-access option (D4).
* **D7 — What stays, and whose it is:**
  * W2, and the seven plugins' W3 lines: Flutter, by D2's revisit (F5, F11);
  * W5: the maintainers of `flutter_foreground_task` and `speech_to_text`, both on their newest
    release (0217 P4);
  * W4 from a bare `flutter` command on a dev host (D4);
  * *added by the amendment of 2026-10-31:* W6, Flutter's, in its SDK's Gradle plugin sources;
  * *added by the second amendment of 2026-10-31:* W7, the maintainers' of
    `flutter_foreground_task`, `flutter_local_notifications` and `speech_to_text`.

  0217's A6 closes when this pair lands, with these named as upstream, as the owner decided in
  0217 Deviation 15.

### Consequences

* Good, because W1's 16 messages, this app's W3 line, and every W4 in CI and in `make` go:
  everything in the build's output that this repository writes or runs.
* Good, because the build moves to what AGP 9 expects, and D3 already reads the new DSL's type.
  D2's revisit then has only the option left to change.
* Neutral, because nothing changes at run time: the same Kotlin sources, the same JVM 17 target,
  the same signing and manifest.
* Bad, because W2, the plugins' W3 lines and W5 stay in CI's log until Flutter and two plugins'
  maintainers act (D7).
* Bad, because D4 is a CI workflow edit and a `Makefile` edit for a warning. The option is the
  JDK's own, and it needs to be kept when those steps change.
* Bad, because a Flutter upgrade can change what Flutter does with these options. D2's revisit
  trigger is how that gets noticed.

### Confirmation

* `gradle.properties` reads `android.builtInKotlin=true` and `android.newDsl=false`, each with its
  comment. `app/build.gradle.kts` differs from `HEAD` only in line 41 and its comment.
* The CI-lint commands, run in the tree with the `Makefile`'s option set, show:
  * W1 0 times, where the tree before the change shows it 16 times;
  * no W3 line for `app/build.gradle.kts`, where the `builtInKotlin=true` probe shows one;
  * W4 0 times, where the tree before the change shows it once per run.

  Each count was seen failing first in the probes.
* `make preflight`, `make apk` with `assert-flutter-release-apk.sh`, and `make manifest-surface`
  pass. The release APK's manifest and signer match the build before the change.
* On the emulator, the app built after the change installs over the one before it, reconnects
  with no re-pair, and shows the same Certificate pin and Client identity fingerprints.
* After the owner's push, a CI dispatch's APK job passes. Its log carries W2, the seven plugins'
  W3 lines and W5, and nothing else of W1 to W5.

## Pros and Cons of the Options

### A — Clear what this repository owns

* Good, because it clears everything this repository can, each by its cause (F6, F8, F9, F10).
* Good, because D3 is forward-compatible with D2's revisit.
* Bad, because it touches four files beyond the property: the build script, the CI workflow, the
  `Makefile` and a script.

### B — Built-in Kotlin only

* Good, because it is one property line (F6).
* Bad, because this app's W3 line and every W4 stay, although each can go (F8, F9, F10).

### C — Both options `true`

* Good, because it would clear W2, and by F11 the plugins' W3 lines.
* Bad, because Flutter 3.47.9's Gradle plugin fails to apply, and no APK builds (F4, F7).

### D — Delete both lines

* Neutral, because AGP's defaults are both `true`.
* Bad, because the next `flutter` build appends both as `false` again (F2), so nothing changes.
  Without that migration it would fail as C does.

### E — Silence the rest

* Good, because the log would look clean.
* Bad, because the causes stay: W2's option is still set, the scripts still use the deprecated
  accessor, and the plugins' code still calls deprecated APIs. A later AGP or Android release that
  removes them fails with no warning first (D-2).

### F — Change nothing until Flutter moves

* Good, because both options would move in one change.
* Bad, because everything A clears stays, and 0217's A6 stays open for a Flutter release with no
  date.

## More Information

### Evidence index

* CI run 41210038095, APK job 124564856862 (`leftovers/0217-apk-dispatch-job.log`), and P1's tag
  run, APK job 124201755019 (`leftovers/0217-p1-apk-tagjob.log`).
* `leftovers/agp9_probe.py` and its logs, `leftovers/agp9-{baseline,kotlin,both}-{pubget,config,lint,build}.log`.
* `leftovers/agp9_probe2.py` and its logs, `leftovers/agp9-fixes-{pubget,config,lint,build}.log`.
* `leftovers/agp9-apksigner-plain.log` and `leftovers/agp9-apksigner-native.log`.
* Flutter 3.47.9 (`frameworkRevision` `c197ca51`): the five `flutter_tools` files in the metadata.
  Flutter master's `gradle.properties.tmpl`, and issues #180137 and #189236, read with `gh` on
  2026-10-31.

### Related records

* [0217-MADR-update-every-pin-to-its-current-release.md](../../../../docs/decisions/0217-MADR-update-every-pin-to-its-current-release.md)
  D8, and its PLAN's Deviation 15.
* [0168-MADR-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md](0168-MADR-build-android-with-jdk-21-in-ci-and-on-every-dev-host.md)
  D2, the bytecode target.
* [0222-PLAN-clear-the-android-build-warnings-this-repository-owns.md](0222-PLAN-clear-the-android-build-warnings-this-repository-owns.md),
  this decision's plan.

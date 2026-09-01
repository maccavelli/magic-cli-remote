---
status: proposed
date: 2026-09-01
associated-madr: "0127-MADR-adopt-current-flutter-toolchain.md"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD060 -->

# PLAN 0127 — Move to Flutter 3.47.2, pin and host together, and stop dependabot recreating the drift

Implements [0127-MADR-adopt-current-flutter-toolchain.md](0127-MADR-adopt-current-flutter-toolchain.md)
decisions D1–D6.

## Goal

The pinned toolchain, the lockfile and every dependency are current, and drift
cannot come back without a red build.

Finish line:

* `flutter --version` and `ci.yml`'s `FLUTTER_VERSION` both say 3.47.2;
* `flutter pub get` on a clean tree changes nothing;
* `flutter pub outdated` shows no direct dependency with `Resolvable > Current`;
* format / analyze / test green at or above `+1358 ~3`;
* a release APK and an iOS simulator build both succeed;
* `.github/dependabot.yml` is gone;
* both drift gates exist and have each been **seen to fail** on a broken input;
* 0126 is unblocked, with its F1 evidence re-read against the plugin actually
  shipped.

## Scope

### In scope (the only files any phase may touch)

* `apps/mobile/pubspec.lock` — regenerated, never hand-edited (D3)
* `apps/mobile/pubspec.yaml` — **only** the three major constraints in P6 (D4)
* `apps/mobile/.metadata` — the `revision:` the Flutter tool tracks
* `.github/workflows/ci.yml` — `FLUTTER_VERSION`, plus the P7 drift gate
* `.github/dependabot.yml` — **deleted** (D5)
* `README.md` — the two version statements at `:234` and `:1410`
* `Makefile` — the `preflight` pin check (P7)
* `scripts/assert-flutter-pin.sh` (new, P7)
* `apps/mobile/android/gradle/wrapper/gradle-wrapper.properties`,
  `android/settings.gradle.kts`, `android/app/build.gradle.kts` — **only if**
  P4 proves the new SDK requires it
* `apps/mobile/ios/Podfile`, `Podfile.lock` — **only if** P5 proves it
* Dart sources — **only** where a phase shows the new analyzer, framework or a
  dependency major requires it, each justified in its commit
* `docs/0066-MADR-secure-storage-upgrade-resilience.md` — an amendment
  recording the D1 revisit outcome (P6c)
* `docs/0126-*` — the deviation entry and the F1 evidence refresh (P8)

### Out of scope

* **Any 0126 implementation work.** 0126 P1's existing edits are committed by
  P2 of this plan because they are already in the tree and toolchain-independent.
  Its P2–P7 wait.
* **`pubspec.yaml`'s `environment: sdk: ^3.12.2`.** It already admits Dart
  3.13.2. Raising it would drop support this record has no reason to drop.
* **New dependencies.** Currency is not an invitation to add anything.
* **Restoring dependabot in a narrower form.** D5 deletes it; re-adding a
  trimmed config is a different decision and needs its own record.
* **Adding an iOS CI job.** 0121 owns that.

## Stability rule

Every phase that touches the tree ends with:

```bash
cd apps/mobile && dart format . && flutter analyze && flutter test
```

then **one commit** (`git commit --no-edit`; never `-m`). Phases touching shell
or Go run `make pre-add-check` before staging.

**Local `flutter analyze` is not authoritative, and is about to get weaker.**
0124 `:70-76` records 3.47.1 missing lints CI enforces on 3.44.8. Local green
means "probably"; CI green means green. This matters more here than in any other
plan, because the whole point of it is to move the host *to* that analyzer.

`git push` needs an explicit instruction in the same turn.

## Cross-cutting contracts

**C1 — The pin and the host never diverge, not even for one commit.** D2. The
`FLUTTER_VERSION` bump, the README statements and `.metadata` land in the same
commit as the regenerated lockfile. There is no intermediate state where CI
builds on a different SDK than the one that produced the lockfile — that state
is the bug this record exists to end.

**C2 — The toolchain move and the dependency majors are separate commits.** D4.
`pub get` (toolchain-forced) lands in P2; `pub upgrade` (non-major currency) in
P3; each major in its own commit in P6. A regression that appears after four
things changed at once is a regression nobody can attribute, and this plan
changes an SDK, a Dart release and three plugin majors.

**C3 — The lockfile is a result, never an input.** D3. If a diff is surprising,
that is a finding to report, not a file to hand-edit toward any version.

**C4 — A required change is a finding, not a chore.** If 3.47 or a major forces
a Gradle bump, a Dart edit or a Podfile change, it is named in the execution
record with what forced it. "Made it build" is not a record.

**C5 — A gate is not trusted until it has been seen to fail.** D7 item 9. Both
drift gates are proven against a deliberately broken input before the phase that
adds them is committed. A green gate that has never gone red is indistinguishable
from a gate that does nothing — which is exactly how `6c02c8e` survived.

**C2 and C5 are the ones at risk.** C2 because batching everything into one
"upgrade" commit is faster and will feel tidier; C5 because a passing gate looks
finished.

## Dependency and delivery order

```text
P0 (baseline, done)
  -> P1 (owner: flutter upgrade)          <- BLOCKING, cannot be agent-run
    -> P2 (pub get + pin move + 0126 P1)
      -> P3 (pub upgrade: non-major currency)
        -> P4 (Android)  ─┐
        -> P5 (iOS)      ─┴-> P6 (the three majors, one commit each)
                                -> P7 (delete dependabot + both drift gates)
                                  -> P8 (hand back to 0126)
```

P4 and P5 come before P6 so that a platform break can be attributed to the SDK
rather than to a plugin major.

## Implementation Steps

### P0 — Baseline (done, 2026-09-01)

Recorded before any change, tree at `19b8cd8` plus 0126 P1's edits:

```text
Flutter 3.44.8 / Dart 3.12.2 / DevTools 2.57.0
Xcode 26.6 (17F113); iOS deployment target 16.0
Gradle 9.1.0; AGP 9.0.1; Kotlin 2.3.20
compileSdk 36 / targetSdk 36 / minSdk 24  (FlutterExtension.kt defaults)
dart format     -> 204 files, 0 changed
flutter analyze -> No issues found!
flutter test    -> +1358 ~3, All tests passed!
```

Android manifest surface, for P4's comparison: the 2026-09-01 capture in
0126 F3 — 13 permissions and 3 exported components, 16 lines.

### P1 — Owner installs the SDK (D1)

**Agent-blocked.** `flutter upgrade` writes to `/opt/homebrew/Caskroom/` and is
refused by this environment's sandbox. Run on the host:

```bash
flutter upgrade          # 3.44.8 -> 3.47.2, Dart 3.12.2 -> 3.13.2
flutter --version        # confirm before continuing
```

The SDK is a git checkout on `stable` inside the Homebrew cask directory, so
`flutter upgrade` is the correct mechanism; `brew upgrade --cask flutter` would
fight it.

Do **not** run `flutter pub get` yet — P2 wants the first resolution under the
new SDK observed, not incidental.

Any second machine (0124 was executed on Windows at 3.47.1) needs the same
version before it next touches the lockfile. C1 is about every host.

### P2 — Re-resolve, move the pin, land 0126 P1 (D1, D2, D3; C1)

```bash
cd apps/mobile && flutter pub get
git diff --stat pubspec.lock
```

Read the diff first. Expected — **verify, do not assume** — is that the six
packages of the MADR's table settle at or above dependabot's values, because
3.47's `flutter`/`flutter_test` pin newer exacts. Record the resolved versions
of `meta`, `matcher`, `test_api`, `vector_math`, `test` and `test_core`.

**If any still resolves below dependabot's numbers, stop.** The premise of this
record — forward, not down — did not hold, and the owner must see that before
it is committed.

Then, in the same commit (C1):

* `.github/workflows/ci.yml:27` → `FLUTTER_VERSION: "3.47.2"`
* `README.md:234` → the `Flutter 3.44.x` / `Dart ≥ 3.12.2` / `CI pins Flutter
  3.44.8` sentence
* `README.md:1410` → `(Flutter 3.44.8 pinned)` in the CI table
* `apps/mobile/.metadata` → `revision:` to the 3.47.2 framework revision
* 0126 P1's two files, already in the tree — the manifest `stopWithTask` removal
  and the `foreground_service.dart` guard comment. They are toolchain-independent
  and are committed here rather than re-done later.

**Verification** — the property the whole record is about, run twice:

```bash
cd apps/mobile && flutter pub get && git status --short pubspec.lock   # empty
cd apps/mobile && flutter pub get && git status --short pubspec.lock   # empty
```

Once proves it resolved; twice proves it is stable.

### P3 — Non-major currency, and Dart revalidation (D4; C2)

```bash
cd apps/mobile
flutter pub upgrade
git diff --stat pubspec.lock
dart format --output=none --set-exit-if-changed .
flutter analyze
flutter test
```

This is the `pub upgrade` step, deliberately **after** P2's `pub get` so the
toolchain-forced diff and the currency diff are two readable commits, not one
(C2). `pubspec.yaml` is untouched here — only P6 widens constraints.

From P0's `outdated`, this is expected to move roughly 20 transitives
(`archive`, `cross_file`, `dbus`, the four `file_selector_*` platform packages,
the `flutter_secure_storage_*` platform packages, `glob`, `image_picker_*`,
`io`, `jni_flutter`, `mime`, `pool`, `pub_semver`, `shared_preferences_*`,
`url_launcher_*`, `win32`, `yaml`). Several are Android/iOS platform
implementations, which is why P4 and P5 follow rather than precede.

Three outcomes, handled differently:

* **All green at `+1358 ~3`.** Record it and move on.
* **`dart format` wants changes.** The formatter moves between Dart releases.
  Apply them wholesale in their own commit, separate from any semantic change.
  Note the file count.
* **`analyze` or `test` fails.** Each failure is a finding (C4): record the
  message, the file, and what caused it. Fix the code, never the lint
  configuration — `analysis_options.yaml` carries deliberate choices
  (`strict-casts`, `strict-raw-types`, `unawaited_futures`; MADR 0084 D8/E1),
  and loosening one to pass an upgrade silently repeals a decision.

A **falling** test count is a failure even if everything is green: it means
tests stopped being collected. Compare the number, not the colour.

### P4 — Android revalidation (D6)

```bash
export ANDROID_SDK_ROOT=/opt/homebrew/share/android-commandlinetools
export ANDROID_HOME=$ANDROID_SDK_ROOT
export JAVA_HOME=/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home
cd apps/mobile
flutter build apk --release --target-platform android-arm64
../../scripts/assert-flutter-release-apk.sh build/app/outputs/flutter-apk/app-release.apk
```

`JAVA_HOME` is not optional: this host's default JDK is 26 and Gradle rejects it
(`docs/ops-android-emulator.md`, gotcha 2).

Then record each of these as a number, not as "fine":

1. **SDK levels.** `compileSdk` / `targetSdk` / `minSdk` from the new
   `FlutterExtension.kt`, against P0's 36 / 36 / 24. A raised `minSdk` drops
   devices and is an owner decision, not a build detail.
2. **Gradle / AGP / Kotlin.** If the build demands a bump, take it and name what
   demanded it (C4).
3. **The manifest surface.** Re-run 0126 F3's capture against the new merged
   manifest and diff against P0's 16 lines. A plugin or AGP change that adds a
   permission or an exported component must be caught here and carried into
   0126 P3's allowlist, not discovered by it.

### P5 — iOS revalidation (D6)

The host has simulators (`iPhone 17`, `17 Pro`, `17e`, `Air`) and Xcode 26.6.

```bash
cd apps/mobile
xcrun simctl boot "iPhone 17" || true
flutter build ios --simulator --debug
flutter run -d "iPhone 17"     # launches; confirm the connect screen renders
```

A simulator build is the honest bound: it proves the toolchain, the pods and the
Dart-to-native surface still compile and boot. It does not prove signing,
background suspension, or Keychain accessibility — 0067/0121 own those, and 0067
records that the owner has no iPhone.

If CocoaPods needs a `pod repo update` or a `Podfile.lock` refresh, take it and
record what forced it (C4).

### P6 — The three majors, one commit each (D4; C2)

Order is chosen so the riskiest lands last, against an otherwise-green tree.

**P6a — `go_router ^17.5.0` → `^18.0.0`.** Read the changelog for breaking
changes before editing. Full gate; pay attention to the navigation tests
(`chat_back_arrow_test.dart`, `chat_end_session_navigation_test.dart`) and the
notification-tap route in `notification_coordinator.dart:227-233`.

**P6b — `flutter_foreground_task ^10.0.0` → `^11.0.1`.** Then, before
committing, **re-read 11.x's Kotlin** and update 0126 F1 to cite what ships:

```text
ForegroundServiceUtils.isSetStopWithTaskFlag   still manifest-backed?
ForegroundService.onStartCommand               still START_NOT_STICKY under the flag?
ForegroundService.onDestroy                    still guards the restart alarm on it?
ForegroundService.onTaskRemoved                still stopSelf() under it?
RebootReceiver.onReceive                       still returns early under it?
ForegroundTaskOptions.stopWithTask             still the visibility-tracker trap?
```

If 11.x changed any of these, 0126 F1's evidence block and 0126 P1's manifest
comment are amended to describe 11.x. If the *fix* is no longer correct — for
instance if 11.x drops the manifest flag entirely — that is a deviation to
report, not to absorb. This step is the reason D4 exists rather than a bare
version bump.

**P6c — `flutter_secure_storage ^10.3.1` → `^11.0.0`.** The credential store,
and the one 0066 D1 asked to revisit on its stable release. Before committing:

* re-run 0066's confirmation path — the startup canary, the `credentialsLost`
  signal, and the recovery flow (`settings_store_test.dart`,
  `credential_recovery` coverage, `cert_pinning_test.dart`);
* check whether 11.0.0 changes the Android backend or the `AndroidOptions`
  surface that `settings_store.dart:60-61` sets (`resetOnError: true`) and the
  iOS `KeychainAccessibility.first_unlock_this_device` at `:66-68`;
* amend `docs/0066-MADR-secure-storage-upgrade-resilience.md` with the revisit
  outcome, since D1 explicitly asked for it.

**If 11.0.0 changes the Android backend, stop and report.** That is a superseding
record for 0066 — a migration for every already-paired device — and it is not
inside this plan. The dependabot comment being deleted ("11.0.0 is not a release
we will take") was a recorded preference; overriding it is the owner's call, and
this step is where the evidence to make it appears.

### P7 — Delete dependabot, add both drift gates (D5, D7; C5)

**Delete the bot:**

```bash
git rm .github/dependabot.yml
```

Record in the commit what this gives up, so it is not rediscovered as a
surprise: the GitHub Actions SHA pins in `ci.yml` are now updated by hand only,
and that file's own comment called the bot "the only practical way to run SHA
pins without them rotting". The gomod ecosystem stops too; `govulncheck` in the
pre-add gate still covers called vulnerabilities there.

**Gate 1 — the lockfile is reproducible (CI).** In `ci.yml`'s `flutter` job,
immediately after `flutter pub get`:

```yaml
      - name: Lockfile is reproducible under the pinned toolchain
        working-directory: apps/mobile
        run: |
          set -euo pipefail
          if ! git diff --exit-code --stat pubspec.lock; then
            echo "::error::pubspec.lock is not reproducible under Flutter ${{ env.FLUTTER_VERSION }}." >&2
            echo "Commit the resolution this toolchain produces (MADR 0127 D7)." >&2
            exit 1
          fi
```

**Gate 2 — the host matches the pin (`make preflight`).**
`scripts/assert-flutter-pin.sh` reads `FLUTTER_VERSION` from `ci.yml` and
compares it with `flutter --version`, failing with both numbers and the exact
`flutter upgrade`/`downgrade` command to reconcile. Wire it into `preflight`
before the `dart format` step, so a mismatched host is told immediately rather
than after a full Go suite.

**Verification (C5) — both gates observed failing before they are trusted:**

```bash
# Gate 2, on a copy so no tracked file is dirtied:
cp .github/workflows/ci.yml /tmp/ci-probe.yml
sed -i '' 's/FLUTTER_VERSION: "3.47.2"/FLUTTER_VERSION: "3.44.8"/' /tmp/ci-probe.yml
MC_CI_FILE=/tmp/ci-probe.yml ./scripts/assert-flutter-pin.sh    # MUST fail
./scripts/assert-flutter-pin.sh                                 # MUST pass

# Gate 1, on a scratch clone so the working tree is never dirtied:
#   clone to a temp dir, hand-edit one version in its pubspec.lock,
#   run flutter pub get, confirm `git diff --exit-code` fails.
```

Do the negative tests on copies and scratch clones. Never dirty a tracked file
to prove a gate, and never `git checkout --` to clean up after one.

### P8 — Hand back to 0126

Add a dated deviation entry to `0126-PLAN` recording that 0127 interrupted it
between P1 and P2, and what changed underneath it:

* the toolchain version and Dart version;
* whether P4's manifest-surface diff altered the allowlist 0126 P3 will commit;
* the `flutter_foreground_task` major, and any amendment P6b made to 0126 F1.

Then resume 0126 at P2.

## Verification (whole plan)

```bash
cd apps/mobile
flutter pub get && git status --short          # clean
flutter pub outdated                           # no direct dep Resolvable > Current
dart format --output=none --set-exit-if-changed . && flutter analyze && flutter test
flutter build apk --release --target-platform android-arm64
flutter build ios --simulator --debug
cd ../.. && ./scripts/assert-flutter-pin.sh
test ! -f .github/dependabot.yml && echo "dependabot removed"
grep -rn '3\.44\.' README.md .github/workflows/ci.yml   # no stale pins
```

### Acceptance criteria

1. `flutter --version` is 3.47.2 / Dart 3.13.2, and `ci.yml` agrees.
2. **`flutter pub get` twice in a row leaves `pubspec.lock` unmodified.**
3. `flutter pub outdated` shows no direct dependency with `Resolvable > Current`.
4. `flutter test` at or above `+1358 ~3`. A lower count fails even if green.
5. Release APK builds and `assert-flutter-release-apk.sh` passes.
6. iOS simulator build succeeds and the app launches to the connect screen.
7. The merged manifest surface matches P0's 16 lines, or the difference is
   explained and carried into 0126 P3.
8. `.github/dependabot.yml` does not exist.
9. Both drift gates have been observed failing on a broken input and passing on
   the real tree (C5).
10. 0126 F1's plugin evidence cites the `flutter_foreground_task` version
    actually shipped, and 0066 carries the D1 revisit outcome.

Criteria 9 and 10 are the ones that distinguish this from a version bump.

## Rollout and Rollback

No runtime change ships from this record on its own — it is a build-toolchain
and dependency move. The user-visible risk is entirely "does the next release
still build and behave", which criteria 4–7 cover, and the three plugin majors
in P6 are the part with real runtime surface.

Rollback is per-commit, which is what C2's one-commit-per-change buys: a single
plugin major can be reverted without giving up the toolchain move. Reverting the
toolchain is `flutter downgrade` plus P2's commit, which restores
`FLUTTER_VERSION`, the README statements, `.metadata` and the lockfile together
— C1 is what makes that a single coherent step.

The one-way door is CI: once `FLUTTER_VERSION` is 3.47.2, branches opened before
the bump resolve their lockfile differently and will trip gate 1 until rebased.
That is a transient inconvenience on open branches, not a data risk — but it is
worth landing when few branches are open.

## Deferred (named, so they are not mistaken for oversights)

* **A replacement for dependabot's Actions SHA updates.** D5 removes the only
  automation keeping those pins current, and this record deliberately offers no
  substitute. If the pins matter — and `ci.yml`'s own comment argues they do —
  the options are a scheduled `pinact`/`ratchet` job or a calendar reminder, and
  either is its own decision.
* **Whether `flutter_secure_storage 11.0.0` is safe to adopt.** P6c gathers the
  evidence; if the Android backend changed, the decision leaves this plan.
* **A second host.** 0124 was executed on Windows at 3.47.1. C1 and gate 2 apply
  there too, but this plan cannot verify a machine it cannot reach — gate 2 will
  tell that host's operator on their next `make preflight`.
* **Raising `environment: sdk:` past `^3.12.2`.** Not required by Dart 3.13.2
  and would drop support for no reason this record has.
* **F-KGP — `mobile_scanner` and `speech_to_text` still apply the Kotlin Gradle
  Plugin.** P4 found that a future Flutter release will refuse to build an app
  whose plugins apply KGP, and both are already at their latest published
  stable with no migrated release. `flutter_foreground_task` is fixed by P6b;
  these two cannot be fixed from this repository. They need upstream releases,
  and adopting `speech_to_text 7.5.0-beta.1` to get ahead of it would put a
  prerelease in a shipped app. Track the two upstream and revisit when either
  publishes a Built-in Kotlin release — before the Flutter version that turns
  the warning into an error.

## Execution record — 2026-09-01

### P1 — SDK installed (owner-run)

```text
Upgrading Flutter to 3.47.2 from 3.44.8 in /opt/homebrew/share/flutter
Flutter 3.47.2 • stable • revision d3b14c8769 (2026-08-26)
Engine 1cf1c4773f (revision a804b26164) • Tools • Dart 3.13.2 • DevTools 2.60.0
flutter doctor -> No issues found!
```

Needed `--force`: the SDK checkout carried a modified `pubspec.lock`. Verified
before discarding — it is the SDK's **own** internal lockfile (`name:
_flutter_packages`, the file upstream maintains with its "Roll pub packages"
commits), mtime **Jul 28**, and the diff contained package pin fields and
nothing else. No human-authored content, and 3.47.2 ships its own copy. The
owner ran `flutter upgrade --force`.

One doctor warning, pre-existing and unrelated: two `adb` binaries on PATH
(`android-commandlinetools` and the `android-platform-tools` cask).

### P2 — Resolution and pin move

**The headline result: the lockfile did not change at all.**

```text
$ flutter pub get && git status --short apps/mobile/pubspec.lock
(empty)
$ flutter pub get && git status --short apps/mobile/pubspec.lock
(empty)
```

Acceptance criterion 2 met on the first attempt. `flutter pub get` under 3.47.2
reproduces the **committed** lockfile exactly, which is the property lost on
2026-08-14.

The reason is recorded as an amendment in the MADR: 3.47 loosened most of the
SDK pins from exact to caret ranges, keeping only `test_api: 0.7.12`,
`matcher: 0.12.20` and `material_color_utilities: 0.13.0` exact. All six of
dependabot's values are what 3.47.2 resolves. **Dependabot was ahead of the
pinned toolchain, not wrong about the versions** — which narrows the case
against it (D5) to the real one: it resolves against an SDK the repository does
not pin, and no gate compared the two.

Pin moved at four sites: `ci.yml:27`, `README.md:234`, `README.md:1410`,
`apps/mobile/.metadata` `version.revision` →
`d3b14c876900e553bc736ca19295fc09e3853e8e`. `grep -rn '3\.44\.'` over
`README.md` and `ci.yml` returns nothing.

Note on `.metadata`: only `version.revision` was changed. The
`migration.platforms[].create_revision` / `base_revision` entries are left at
their old values deliberately — those record when each platform directory was
created and last migrated, and are `flutter migrate`'s to move, not this plan's.

**Deviation — P2's commit is split into three, not one.**
P2 as written said to land the pin move and 0126 P1's two files in one commit.
That contradicts this plan's own C2 ("one commit per change so a regression
stays attributable"): 0126 P1 is an unrelated Android behaviour fix, and
bundling it means neither can be reverted without the other. Split as:

1. docs only — the 0126 and 0127 pairs (bootstrap exception; source, CI and
   product config must not share that commit);
2. the toolchain move — `ci.yml`, `README.md`, `.metadata`;
3. the Dart 3.13 formatter drift — 5 test files, wholesale;
4. 0126 P1 — the manifest and `foreground_service.dart`.

Nothing about *what* lands changed; only which commit it lands in. C1 is
preserved: the pin, the README statements and `.metadata` are still atomic, and
the lockfile needed no change to join them.

### P3 — Non-major currency and Dart revalidation

`flutter analyze` → **No issues found** (11.2s). No framework API churn across
the 37k lines of Dart; `flutter_lints 6.0.0` needed no bump.

`flutter test` → **+1358 ~3, All tests passed** — identical to the P0 baseline.
No test was lost, so the count check passes rather than merely the colour.

`dart format --output=none --set-exit-if-changed .` → exit 1, **5 files**:

```text
test/device_flow_sheet_test.dart
test/dial_episode_test.dart
test/link_liveness_test.dart
test/socket_dial_failure_test.dart
test/transcript_rows_test.dart
```

All test files, all formatter-only — the Dart 3.13 formatter moved, exactly the
case P3 anticipated. Applied wholesale in their own commit (commit 3 above) so
they cannot be mistaken for semantic change.

### P4 — Android revalidation

```text
flutter build apk --release --target-platform android-arm64   -> 131.5s, 40.8MB, exit 0
assert-flutter-release-apk.sh                                 -> OK release-mode APK (39M)
```

**SDK levels unchanged.** `FlutterExtension.kt` on 3.47.2 still supplies
`compileSdkVersion 36`, `targetSdkVersion 36`, `minSdkVersion 24` — identical to
P0. No devices dropped. `ndkVersion` is `28.2.13676358`.

**No Gradle, AGP or Kotlin bump was required.** Gradle 9.1.0, AGP 9.0.1,
Kotlin 2.3.20 all built unchanged.

**Manifest surface: identical.** The 16-line capture against the new merged
manifest diffs clean against P0's baseline, so **0126 P3's allowlist is
unaffected** by the toolchain move and can be committed as drafted.

```text
diff -u surface-baseline(3.44.8) surface(3.47.2)  ->  IDENTICAL
```

#### New finding — F-KGP: a future Flutter release will fail to build this app

3.47.2 builds fine but emits:

> WARNING: Your app uses the following plugins that apply Kotlin Gradle Plugin
> (KGP): **flutter_foreground_task, mobile_scanner, speech_to_text**
> Future versions of Flutter will fail to build if your app uses plugins that
> apply KGP.

Checked against pub.dev, 2026-09-01 — only one of the three is fixable today:

| plugin | pinned | latest | KGP-migrated? |
|---|---|---|---|
| `flutter_foreground_task` | ^10.0.0 | 11.0.1 | **yes** — 11.0.0 `[FEAT] Migrate to built-in Kotlin (KGP) #385` |
| `mobile_scanner` | ^7.4.0 | 7.4.0 | no — already latest, no migrated release exists |
| `speech_to_text` | ^7.4.0 | 7.4.0 | no — already latest (7.5.0-beta.1 is a prerelease) |

So P6b's `flutter_foreground_task` bump removes one of the three, and the
remaining two have **no fix available at any published version**. This is a
real future build break with no complete remedy today, and it is recorded rather
than acted on: it needs upstream releases, and a prerelease dependency in a
shipped app is not a trade this record makes. See Deferred.

**Incidental — 0126 F8 reproduced live.** The running Gradle invocation's
dart-defines decode to `FLUTTER_BUILD_NAME=0.1.0`, `FLUTTER_BUILD_NUMBER=1`,
confirming that a local `flutter build apk` stamps the pubspec placeholder.
0126 P6 fixes it.

### P5 — iOS revalidation

```text
flutter build ios --simulator --debug   -> Xcode build done, 64.7s, exit 0
                                           built build/ios/iphonesimulator/Runner.app
xcrun simctl install/launch (iPhone 17) -> launched, PID assigned
```

`Podfile.lock` changed by one line — the `Flutter` pod checksum
(`cabc95a1…` → `71a624a5…`), which tracks the engine. Exactly the
toolchain-forced change P5 allows; taken. No `pod repo update` was needed and
`pod install` completed in 752 ms.

**The app runs.** The screenshot showed only the iOS notification permission
alert over a uniform grey field, which looked like a blank app; the logs say
otherwise:

```text
(Flutter) [IMPORTANT:…FlutterDarwinContextMetalImpeller.mm(45)]
         Using the Impeller rendering backend (Metal).
(Flutter) flutter: The Dart VM service is listening on http://127.0.0.1:60970/…
[UIFocus] FlutterView implements focusItemsInRect: …
[UIFocus] FlutterSemanticsScrollView implements focusItemsInRect: …
```

Impeller is up, the Dart VM is serving, and a `FlutterSemanticsScrollView`
exists — the widget tree built and contains the connect screen's scroll view.
No Dart exception at any point. The permission alert is itself Dart-driven
(`NotificationService.init()` → `ios.requestPermissions`), so reaching it proves
the app got well past `main()`.

**Capture caveat, worth writing down.** `xcrun simctl io booted screenshot`
renders the Metal/Impeller surface as flat grey — the same class of limitation
`docs/ops-android-emulator.md` gotcha 1 documents for `adb exec-out screencap`.
System UI (the alert, status bar) captures fine, which is what makes it look
like a broken app rather than a broken screenshot. Do not read a grey simulator
screenshot as a blank screen; check the logs.

`xcrun simctl privacy booted grant all` does **not** cover notifications, so the
alert cannot be dismissed that way and the underlying UI stays occluded in a
screenshot regardless.

#### Observation — flutter_foreground_task's iOS BGTask registration is rejected

At every launch, twice reproduced:

```text
[com.apple.BackgroundTasks:Framework] Registration rejected;
com.pravera.flutter_foreground_task.refresh is not advertised in the
application's Info.plist
```

Confirmed by inspection: `ios/Runner/Info.plist` declares neither
`BGTaskSchedulerPermittedIdentifiers` nor `UIBackgroundModes`.

**This is consistent with the design, not a defect.** MADR 0067 D2 parks the
socket unconditionally on iOS because the OS suspends the process, so the app
deliberately does no background work there — the plugin's background-refresh
path *should* be inert. Recorded because it is a recurring error-level log line
that will otherwise be rediscovered as a bug, and because if 0121 ever wants iOS
background behaviour, this is the first thing that has to change.

#### Second future-breaking warning, same plugin

```text
The following plugins do not support Swift Package Manager for ios:
  - flutter_foreground_task
This will become an error in a future version of Flutter.
```

`flutter_foreground_task` 11.0.0 adds `[FEAT] Support Swift Package Manager
(SPM) #387`, so **P6b's bump clears this and the Android KGP warning at once**.
That makes P6b the highest-value of the three majors, not merely the riskiest.

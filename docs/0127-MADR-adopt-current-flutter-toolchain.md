---
status: accepted
date: 2026-09-01
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD060 -->

# Adopt Flutter 3.47.2 and move the local and CI pins together, instead of resolving the lockfile downward

## Context and Problem Statement

`apps/mobile/pubspec.lock` has been unsatisfiable under the repository's own
pinned toolchain since **2026-08-14**. Every `flutter pub get` — local and CI —
silently re-resolves six packages downward, and nothing fails, so nothing
reported it.

It surfaced while executing [0126](0126-MADR-android-client-debugging-pass-findings.md)
P1: the first command of the session (`flutter analyze`, intended as read-only)
left the lockfile dirty.

### What was measured

**The drift, and its direction.** `flutter pub get` on this host reverses
exactly six pins:

```text
matcher      0.12.20 -> 0.12.19        meta         1.19.0 -> 1.18.0
test         1.31.1  -> 1.31.0         test_api     0.7.12 -> 0.7.11
test_core    0.6.18  -> 0.6.17         vector_math  2.4.2  -> 2.2.0
```

**The commit that wrote them** is `6c02c8e`, *"deps(mobile): bump the
dart-minor-patch group"*, author `dependabot[bot]`, 2026-08-14. Its diff raises
those six and no others — the same six, in the opposite direction.

**Why no resolution can honour it.** The Flutter SDK pins these with *exact*
constraints, not ranges. From the installed 3.44.8 SDK:

```yaml
# packages/flutter/pubspec.yaml
meta: 1.18.0
vector_math: 2.2.0
collection: 1.19.1

# packages/flutter_test/pubspec.yaml
test_api: 0.7.11
matcher: 0.12.19
vector_math: 2.2.0
meta: 1.18.0
```

`test` and `test_core` follow from `test_api: 0.7.11` — 1.31.1 / 0.6.18 require
`test_api 0.7.12`. So all six trace to two exact SDK pins. Dependabot resolves
in its own container without this Flutter SDK, produced a resolution the pinned
toolchain could not honour, and the PR merged because CI does not fail on a
lockfile that pub quietly rewrites.

### Amendment, 2026-09-01 — dependabot was ahead, not wrong

Measured after P1 installed 3.47.2, and it corrects the paragraph above.
Flutter 3.47 **loosened most of these constraints from exact pins to caret
ranges**, keeping only three exact:

```yaml
# packages/flutter/pubspec.yaml       # packages/flutter_test/pubspec.yaml
meta: ^1.18.3                          test_api: 0.7.12     <- exact
vector_math: ^2.4.0                    matcher: 0.12.20     <- exact
material_color_utilities: 0.13.0       vector_math: ^2.4.0
collection: ^1.19.1                    meta: ^1.18.3
```

`flutter pub get` under 3.47.2 then reproduces the **committed** lockfile
exactly — `git status` reports `pubspec.lock` unmodified, twice in a row. Every
one of dependabot's six values is what 3.47.2 resolves: `matcher 0.12.20` and
`test_api 0.7.12` are now the SDK's own exact pins, and `meta 1.19.0` /
`vector_math 2.4.2` fall inside the new ranges.

So `6c02c8e` proposed the right versions for the Flutter the project had not yet
adopted. The defect was never the numbers; it was that a lockfile was committed
which the *pinned* toolchain could not reproduce, and nothing failed when pub
silently rewrote it. That distinction matters for D5: the case against the bot
is not that it proposes bad versions, but that it resolves against an SDK the
repository does not pin and no gate compares the two.

It also removes the need for the `ignore`-list approach entirely — with only
three exact pins left, and D5 deleting the config, the list this record
originally contemplated has no home and is not written.

**This has happened before, and was resolved the other way.**
[0112](0112-MADR-opencode-1.18.21-surface-parity.md) finding 4 records the same
shape in August: a lockfile "from an automated dependency bump" that realigned
six SDK-pinned transitives when built on the pinned toolchain. The resolution
then was to **move the CI pin up to match the locally installed SDK** (3.44.6 →
3.44.8, 2026-08-25), updating `ci.yml` and the two README statements. That is
the precedent, and it is the same shape as the decision here.

**How far behind the toolchain is.** From the official release index
(`storage.googleapis.com/flutter_infra_release/releases/releases_macos.json`,
fetched 2026-09-01):

```text
3.47.2   dart 3.13.2   2026-08-27   <- current stable
3.47.1   dart 3.13.1   2026-08-19
3.47.0   dart 3.13.0   2026-08-12
3.44.9   dart 3.12.2   2026-08-06
3.44.8   dart 3.12.2   2026-07-23   <- this repo, local and CI
```

Worth stating precisely, because it changes the risk framing: the stable line
goes **3.44 → 3.47 with no 3.45 or 3.46**, so this is *one* feature release, not
three, plus Dart 3.12.2 → 3.13.2. `pubspec.yaml` declares `sdk: ^3.12.2`, which
already admits 3.13.2 — no constraint change is required.

**One recorded adverse data point, and it is the important one.**
[0124](0124-PLAN-transcript-cache-path-separator.md) (`:70-76`) states, from the
owner's Windows host:

> `flutter analyze` locally is not authoritative. This host runs Flutter 3.47.1
> against CI's pinned 3.44.8, and the newer analyzer **misses lints CI
> enforces** — it cost one red run during 0123.

So 3.47's analyzer is, on at least one lint, *less* strict than 3.44.8's. That
is not an argument against adopting 3.47; it is a decisive argument that local
and CI must move **in the same commit**. A host running ahead of the pin is the
exact configuration that produced a red build, and it is today's configuration
on at least one of the owner's machines.

### What is not yet known

Nothing below has been verified, because the SDK is not installed and
`flutter upgrade` is blocked by this environment's sandbox:

* whether 3.47 raises the Android `compileSdk` / `targetSdk` / `minSdk` defaults
  (3.44.6 supplies 36 / 36 / 24 from `FlutterExtension.kt`);
* whether it requires a Gradle or AGP bump — the repo is on Gradle 9.1.0, AGP
  9.0.1, Kotlin 2.3.20;
* whether framework API churn produces `flutter analyze` findings across the
  37k lines of Dart, or whether `flutter_lints 6.0.0` needs a bump;
* whether the iOS deployment target (16.0, Xcode 26.6) still builds.

These are the plan's job, not this record's. They are listed so that "the
upgrade was clean" is a *result*, not an assumption.

### Three direct dependencies are also behind

```text
flutter_foreground_task   10.0.0  -> 11.0.1
flutter_secure_storage    10.3.1  -> 11.0.0
go_router                 17.5.0  -> 18.0.0
```

All three show `Upgradable == Current` in `flutter pub outdated`, meaning
`pub upgrade` alone will not move them: their `pubspec.yaml` constraints
(`^10.0.0`, `^10.3.1`, `^17.5.0`) have to be widened. They are majors, and each
carries a different obligation:

* **`go_router 18.0.0`** — no record blocks it. Routing majors change APIs;
  the suite is the check.
* **`flutter_foreground_task 11.0.1`** — entangled with
  [0126](0126-MADR-android-client-debugging-pass-findings.md). That record's F1
  and P1 are a line-by-line reading of **10.0.0's** Kotlin
  (`ForegroundService.kt:185/211/218`, `RebootReceiver.kt:31`,
  `ForegroundServiceUtils.kt:16-28`). The bump does not invalidate the *fix* —
  removing `android:stopWithTask` is a manifest change — but it does invalidate
  the *evidence*, and 0126 P1 is already applied to the working tree.
* **`flutter_secure_storage 11.0.0`** — the highest-risk of the three, and the
  one where the repository's two records disagree with each other.
  [0066](0066-MADR-secure-storage-upgrade-resilience.md) D1 (`:248-257`) says:

  > stay on flutter_secure_storage 10.3.1 with current options … and do not
  > adopt the 11.0.0-beta.1 **prerelease** … **Revisit the plugin major on its
  > stable release.**

  11.0.0 is now that stable release, so 0066 D1 does not forbid it — it
  *instructs the revisit*. `.github/dependabot.yml` is stricter than the MADR it
  cites, saying "11.0.0 is not a release we will take … until something newer
  than 11.0.0". That preference lives only in a config comment, and this record
  deletes the file it lives in, so it has to be decided rather than inherited.

  This is the app's credential store, and 0066 exists because of silent
  per-key credential loss. A backend change in 11.0.0 is a superseding record
  for 0066, not a version bump.

## Decision Drivers

* The owner's stated direction: current Flutter, current dependencies, **no
  local drift**, and no dependabot.
* A lockfile that no supported toolchain can reproduce is not pinning anything;
  CI has been building an unrecorded set for 2.5 weeks.
* 0124's finding makes "local ahead of CI" a known-bad configuration, so pin and
  host must move together — and *stay* together, which needs enforcement rather
  than discipline.
* Dependabot has now caused this class of failure twice (0112 finding 4,
  and `6c02c8e`), and cannot see the constraint that makes its pub proposals
  invalid.
* 0126 is mid-execution against plugin sources read at their current majors.

## Considered Options

* **Adopt 3.47.2, take every dependency the pinned toolchain supports, delete
  dependabot, and enforce the pin in CI.**
* **Adopt 3.47.2, take only non-major currency (`pub upgrade`), keep the three
  majors pinned, delete dependabot.**
* **Stay on 3.44.8; commit the downward resolution and narrow dependabot with
  an `ignore` block instead of deleting it.**
* **Keep dependabot for GitHub Actions only, and delete just its pub and gomod
  ecosystems.**

## Decision Outcome

Chosen option: **"Adopt 3.47.2, take every dependency the pinned toolchain
supports, delete dependabot, and enforce the pin in CI"**, because it is the
only option that satisfies all three parts of the stated direction. Currency
without drift-enforcement is what the repo already had and what failed twice;
enforcement without currency is the downward resolution already rejected.

The rejected options, fairly:

* *Non-major currency only* is the lower-risk change and would still fix the
  lockfile. It was rejected because it leaves three known-stale majors as
  standing drift, which is the condition being eliminated.
* *Stay on 3.44.8 with an `ignore` block* is the smallest correct change and
  would make the lockfile honest today. Rejected on direction, and it leaves the
  project a release train behind, which only makes the eventual jump larger.
* *Keep dependabot for Actions only* is the option this record is least
  comfortable rejecting — see the consequence below. It is rejected because the
  instruction was to turn dependabot off, and a half-off bot is a worse thing to
  reason about than either state.

### Decisions

* **D1 — The pinned toolchain is Flutter 3.47.2 / Dart 3.13.2.**
* **D2 — Local SDK and `FLUTTER_VERSION` move in the same commit**, with the two
  README statements and `apps/mobile/.metadata`. No state exists in which the
  repo says one version and a developer runs another.
* **D3 — `pubspec.lock` is regenerated by the new toolchain and committed as
  whatever it resolves to.** It is never hand-edited toward or away from any
  particular version; the toolchain is the authority.
* **D4 — Dependencies are taken to the newest version the pinned toolchain
  supports**, including the three majors, subject to per-package revalidation:
  * `go_router 18.0.0` — the routing tests are the check.
  * `flutter_foreground_task 11.0.1` — the bump is taken **and** 0126's F1/P1
    evidence is re-read against 11.x's Kotlin, with 0126 amended to cite the
    version actually shipped. A fix whose stated evidence describes a version
    that is no longer present is not a finished fix.
  * `flutter_secure_storage 11.0.0` — taken only after 0066's confirmation
    path (the startup canary, `credentialsLost`, and the recovery flow) is
    re-run green. If 11.0.0 changes the Android backend, this becomes a
    superseding record for 0066 and stops being part of this one.

  **Amended 2026-09-01, after P6c investigated it.** D4 said "including the
  three majors"; the outcome is **two of three**. `go_router 18.0.0` and
  `flutter_foreground_task 11.0.1` were taken. `flutter_secure_storage 11.0.0`
  was **not**, and the reason is neither of the two this record anticipated:
  the credential-migration risk turned out not to apply (the app has shipped
  v10 since its first commit and uses none of the removed API), but the release
  hard-pins `compileSdk = 37`, which AGP propagates to every consumer. Taking it
  would move the whole app off `flutter.compileSdkVersion` (36) onto an API
  level neither this host nor CI has installed. Upstream calls that a defect and
  has an open fix verified against Flutter 3.47.0
  ([PR #1236](https://github.com/juliansteenbakker/flutter_secure_storage/pull/1236),
  2026-08-30). There is no 11.0.1 to take instead.

  This makes D4's own criterion — "the newest version the pinned toolchain
  supports" — decide it: a release requiring `compileSdk 37` is not supported by
  a toolchain whose default is 36. The full analysis and the revisit 0066 D1
  asked for are recorded in
  [0066](0066-MADR-secure-storage-upgrade-resilience.md)'s 2026-09-01
  amendment, deliberately placed there because D5 deletes the
  `dependabot.yml` comment that used to hold this preference.
* **D5 — `.github/dependabot.yml` is deleted.** Not narrowed. It has produced
  two lockfile-drift incidents and cannot see the SDK's exact pins that make its
  pub proposals unsatisfiable. Dependency currency becomes a deliberate,
  recorded act — which is what this pair is.

  **Amended 2026-09-01 by [0128](0128-MADR-triage-the-0126-and-0127-deferred-items.md)
  D1: partially reversed.** D5 deleted all three ecosystems because one of them
  failed. Measured afterwards, the `github-actions` ecosystem had been doing its
  job — 6 of 7 pins current, and the single exception (`actions/setup-java`, one
  major behind) is this config's *stated policy* working, since it said action
  majors are reviewed by hand before merge. Both failures D5 cites were
  `pub`-only, and structurally so: Dependabot resolves without the Flutter SDK
  and cannot see the exact pins in `packages/flutter{,_test}/pubspec.yaml`.

  `github-actions` is restored; `pub` and `gomod` stay deleted. D5's reasoning
  holds for the ecosystem that broke and did not hold for the other two — the
  error was applying a pub-shaped diagnosis to all three.
* **D6 — The upgrade is verified, not assumed.** Android APK build, iOS
  simulator build, the full Dart suite, and 0126 P3's manifest-surface capture
  are all re-run; a difference in any of them is a finding, not a formality.
* **D7 — Drift is prevented by CI, not by discipline.** Two gates, because the
  two drifts observed had different causes:
  1. `flutter pub get` must leave `pubspec.lock` unchanged — this catches a
     lockfile no toolchain can reproduce (`6c02c8e`).
  2. The local Flutter version must equal `FLUTTER_VERSION` — this catches a
     host running ahead of the pin (0124 `:70-76`, 0112 finding 4).

  Gate 1 runs in CI. Gate 2 is a `make preflight` check, because CI cannot
  observe a developer's machine.

### Consequences

* Good, because the lockfile becomes reproducible under the pinned toolchain,
  which is the property it lost on 2026-08-14 — and D7 gate 1 makes losing it
  again a red build rather than a silent rewrite.
* Good, because D7 gate 2 makes the local-ahead-of-CI configuration — the one
  0124 records as having cost a red run — impossible to hold accidentally.
* Good, because after D4 there is no known stale dependency left to be drift.
* **Bad, and this is the sharpest cost: deleting `dependabot.yml` also stops the
  GitHub Actions SHA updates.** That config's own comment calls the bot "the
  only practical way to run SHA pins without them rotting", and `ci.yml` pins
  every action to a commit SHA. After D5, those pins are frozen until someone
  updates them by hand, so a security fix in an action does not arrive on its
  own. The gomod ecosystem stops too, though `govulncheck` in the pre-add gate
  already covers the case that matters there. This is a real trade and it is not
  mitigated by anything in this record.
* Bad, because a feature-release bump plus three dependency majors land close
  together. The plan sequences them as separate commits with their own
  revalidation so a regression stays attributable, but the window is still
  wider than a toolchain move alone.
* Bad, because 3.47's analyzer is known to be less strict on at least one lint
  (0124), so this trades a stricter local check for currency. CI green remains
  the authority; that was already true.
* Neutral, because `pubspec.yaml`'s `environment: sdk: ^3.12.2` already admits
  Dart 3.13.2 — the app's own declared constraint does not move.

### Confirmation

This record is confirmed when `0127-PLAN` has been executed and:

1. `flutter --version` reports 3.47.2 / Dart 3.13.2 on the owner's host, and
   `ci.yml`'s `FLUTTER_VERSION` says the same;
2. **`flutter pub get` on a clean tree leaves `pubspec.lock` unmodified** — the
   property that has been false since 2026-08-14, and the clearest single test
   that this is fixed;
3. `flutter pub outdated` reports no `Resolvable` above `Current` for any direct
   dependency;
4. `dart format --set-exit-if-changed`, `flutter analyze` and `flutter test` are
   green, with the suite at or above its 2026-09-01 baseline of **1358 passing,
   3 skipped**;
5. `flutter build apk --release --target-platform android-arm64` succeeds and
   `scripts/assert-flutter-release-apk.sh` passes;
6. an iOS simulator build succeeds and launches;
7. the merged Android manifest surface is unchanged from the 2026-09-01 capture
   in 0126 F3, or the difference is explained and carried into 0126 P3;
8. `.github/dependabot.yml` no longer exists;
9. both D7 gates exist and have been observed **failing** on a deliberately
   broken input before being trusted.

Items 2, 3 and 9 are the load-bearing ones. The rest would pass on a broken
upgrade that merely compiled, and item 9 is the difference between a gate and a
decoration.

## More Information

* Baseline captured 2026-09-01, tree at `19b8cd8` plus 0126 P1's uncommitted
  edits: Flutter 3.44.8, Dart 3.12.2, Xcode 26.6, Gradle 9.1.0, AGP 9.0.1,
  Kotlin 2.3.20, `flutter test` `+1358 ~3`.
* Precedent: [0112](0112-MADR-opencode-1.18.21-surface-parity.md) finding 4 —
  the previous instance of this drift and the pin move that resolved it.
* Constraint on the method: [0124](0124-PLAN-transcript-cache-path-separator.md)
  `:70-76` — the newer analyzer misses lints CI enforces.
* Touched by D4: [0066](0066-MADR-secure-storage-upgrade-resilience.md) D1
  (`:248-257`) instructs revisiting the `flutter_secure_storage` major on its
  stable release; this record performs that revisit and must amend 0066 with the
  outcome.
* Blocked by this record: [0126](0126-MADR-android-client-debugging-pass-findings.md)
  P2 onward. 0126 P1 is complete and toolchain-independent; D4 requires its F1
  evidence to be re-read against `flutter_foreground_task` 11.x.
* `flutter upgrade` cannot be run by an agent in this environment — it writes
  outside the workspace and is refused by the sandbox. `0127-PLAN` P1 is
  therefore an owner-run step.

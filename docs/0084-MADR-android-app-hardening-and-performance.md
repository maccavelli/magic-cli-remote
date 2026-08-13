<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 -->

# MADR 0084 — Harden the Android app: crash visibility, transcript-cache storage, and platform build gaps

| field | value |
| --- | --- |
| status | **accepted** 2026-08-13. Investigation record; revised the same day with an official-documentation and comparable-project research pass (§Research), which **corrected two of this record's own proposals**. |
| related | MADR 0066 (secure storage + its on-device diagnostics row), MADR 0067 (iOS background limits), MADR 0063 (no simulated liveness), MADR 0065 (in-app APK updates), MADR 0042 (predictive back), MADR 0083 (edge-to-edge insets), `docs/standards/mobile/flutter.md` |
| evidence | Static audit of `apps/mobile` (62 Dart files, 29 503 lines) and the Android platform config, 2026-08-13, every finding carrying a file:line citation verified against the tree at commit `79c82dd`; plus the external sources listed in §Research. |
| plan | [0084-PLAN-android-app-hardening-and-performance.md](0084-PLAN-android-app-hardening-and-performance.md) (proposed, drafted 2026-08-13 for joint review) |

## Context and Problem Statement

The Android client has grown to ~29.5 kLOC across 62 files, with the four
largest — `mcremote_client.dart` (3 704), `chat_screen.dart` (3 239),
`sessions_screen.dart` (1 794), `connect_screen.dart` (1 638) — carrying most
of the runtime behaviour. Twenty-plus MADRs have added features to it. The
socket lifecycle, transport fallback, notification and pairing paths have each
been hardened by their own record.

What has *not* been examined as a whole is the app's behaviour as an Android
process: what happens when something throws where no `try` sits, what the
storage layer costs at cold start, and what the release build and its CI gates
actually check. This record is that pass. It deliberately looks past feature
correctness — which the existing suites cover well (945 Flutter tests) — at
the classes of defect tests of that shape do not see.

Three themes emerged, in descending severity: **the app cannot report its own
failures**; **the transcript cache is a multi-megabyte blob in a store that
was never meant for blobs — on an API the Flutter team has slated for
deprecation — decoded on the UI thread**; and **the release build and platform
config skip checks the app has already been bitten by**.

The findings below came from reading the code; §Research then tested each
proposed remedy against primary documentation and one close architectural peer
before this record committed to it. That pass changed two of the answers.

## Findings

Severity is about user-visible consequence, not effort.

### A — Crash and error visibility

| # | Finding | Evidence | Severity |
| --- | --- | --- | --- |
| A1 | **The app installs no error handling of any kind.** `main.dart` is nine lines: `ensureInitialized()` then `runApp()`. There is no `FlutterError.onError`, no `PlatformDispatcher.instance.onError`, no `runZonedGuarded`, and no `ErrorWidget.builder` anywhere in `lib/` (all four grep to zero). Consequences, in order of how badly they bite: an uncaught **async** error in release is swallowed by the zone and vanishes — no log, no trace, no user signal; an exception thrown during `build` renders Flutter's default error widget, which in release is a plain grey box with no way for the user to say what happened; and a platform-channel error crosses into Dart with nowhere to land. For a **self-hosted, no-telemetry app** this is the whole diagnostic story: when the user reports "it just went blank", nothing on the device recorded why. | `lib/main.dart:1-9`; greps over `lib/` | **high** |
| A2 | The app already has the right *shape* for the fix and does not use it. MADR 0066 D5 records the last secure-storage failure and surfaces it in Settings as a readable row (`settings_screen.dart` "Secret storage" tile, `storageFailure` state) precisely so the next report is "a reading, not a paraphrase". Crashes — a strictly larger class — have no equivalent. | `settings_screen.dart` Storage & diagnostics section; `settings_store.dart` `getLastStorageFailure` | medium |
| A3 | Debug-only reporting is the current substitute: `debugPrint` appears throughout the error paths (e.g. `app_lifecycle.dart:100,190,254`, `foreground_service.dart:91,106,122,129`). `debugPrint` is a no-op sink in release for diagnosis purposes — nothing persists it. Every one of these sites is a place where the app *knows* something failed and has nowhere to put it. | cited lines | medium |

### B — Storage and performance

| # | Finding | Evidence | Severity |
| --- | --- | --- | --- |
| B1 | **The transcript cache uses SharedPreferences as a blob store, up to ~4.8 MB — and the API it uses is on a deprecation path.** Each session entry is capped at 400 KB (`transcript_cache.dart:76`, with a halve-and-retry then drop) and the index holds `kTranscriptCacheMaxSessions = 12` (`chat_models.dart:41`). The legacy `SharedPreferences.getInstance()` API loads **every key and value into a Dart-side `Map` at first call** and keeps it for the process lifetime — and the Android platform side parses the whole XML file. So worst case the app pays a multi-megabyte parse at the first preferences read and then holds those JSON strings resident forever. The file's own comment concedes the point — *"SharedPreferences is not a blob store"* — while doing it anyway. This store is shared with every other preference user, so the cost lands on the **first settings read at startup**, not on chat. The research pass sharpened this from a design smell into a supported-API problem: the plugin's own README says the legacy `SharedPreferences` API *"will be deprecated in the future"*, that the team *"highly encourage"* new code onto `SharedPreferencesAsync`/`SharedPreferencesWithCache` (DataStore-backed on Android), and — pointedly — that the plugin *"must not be used for storing critical data"* because persistence is asynchronous with no guarantees. | `transcript_cache.dart:42-43,76,117`; `chat_models.dart:41`; shared_preferences README (§Research R2) | **high** |
| B2 | **Encoding is moved off the UI thread; decoding is not.** `save()` correctly hands the encode to `compute()` (`transcript_cache.dart:75`, and again at `:95` for the retry). `load()` calls `jsonDecode(raw)` **directly on the main isolate** (`:131`) and then builds every `ChatItem` from it in a loop (`:138-145`). That is up to 400 KB of JSON parsed plus hundreds of objects allocated on the UI thread at exactly the moment the user opens a chat — the jank is placed on the interaction it is meant to make feel instant. | `transcript_cache.dart:75,95,131,138-145` | **high** |
| B3 | `usage()` — which backs the decorative "Cached transcripts · N sessions · X MB" subtitle in Settings — iterates the index and reads **every entry's full string** to sum lengths (`transcript_cache.dart` `usage()`), i.e. touches the entire multi-megabyte set to render one line of text. | `transcript_cache.dart` `usage()` | low |
| B4 | **Latent cross-isolate cache incoherence.** The legacy `SharedPreferences` API caches every value in the Dart heap *per isolate*, and the app runs a second isolate: the foreground service's `mcRemoteForegroundCallback` (`foreground_service.dart:8-11`, `@pragma('vm:entry-point')`). The plugin README names exactly this — caching *"creates synchronization issues across multiple isolates or engine instances"*, resolved by `SharedPreferencesAsync`. **Not an active bug**: today's `_KeepAliveTaskHandler` is deliberately empty and touches no preferences (`:13-22`). It is a landmine for the next person who adds work to that handler — which is a plausible thing to do, since the service exists to keep alerts flowing. | `foreground_service.dart:8-22`; shared_preferences README (§Research R2) | medium |

### C — Lifecycle and unbounded growth

| # | Finding | Evidence | Severity |
| --- | --- | --- | --- |
| C1 | `NotificationCoordinator.sessionLabels` is a plain `Map<String, String>` (`notification_coordinator.dart:457`) written from three call sites (`:128`, `sessions_screen.dart:144,331`) and **never pruned**. The coordinator's own reset clears `_knownAsks` and `_shownAsks` (`:113-114`) but not this map. It grows by one entry per session title ever seen, for the life of the process. Small per entry; monotonic by construction, and the coordinator is explicitly app-lifetime (`app_lifecycle.dart:72`). | cited lines | medium |
| C2 | `request()`'s idempotent retry has a drop window. On timeout it does `_pending.remove(id)` and then re-enters `request()` with the same id, which re-registers a fresh completer (`mcremote_client.dart` `request()`, timeout branch). A first response arriving **between** the removal and the re-registration is discarded by the read loop, which only completes ids present in `_pending` (`:2748-2749`). The caller then waits another full timeout (default 30 s) for a reply the daemon already sent. Narrow window, but it opens exactly when the link is slow — the case the retry exists for. | `mcremote_client.dart` `request()`, `:2748-2749` | medium |
| C3 | The pending-completer bookkeeping is otherwise sound and worth recording as *not* a finding: `_failAllPending` snapshots-then-clears before completing (`:2665-2669`), and the send path removes on both over-size and sink-throw. No leak found. | `:2665-2669` | — |

### D — Android platform and build configuration

| # | Finding | Evidence | Severity |
| --- | --- | --- | --- |
| D1 | **The release build does no code shrinking or obfuscation.** `buildTypes.release` sets only `signingConfig`; there is no `isMinifyEnabled`, no `isShrinkResources`, no ProGuard/R8 rules file. Every release APK ships the full Dart-independent Java/Kotlin surface of all plugins plus unused resources, and class/method names are as-written. | `android/app/build.gradle.kts` `buildTypes` block | medium |
| D2 | **Android lint is off unless someone opts in, and CI never does.** `lint { checkReleaseBuilds = androidLintEnabled }` is gated behind `-PmcAndroidLint=true` (`build.gradle.kts`), and no workflow passes it. The file's own comment states the stakes exactly: *"Android lint is the only thing that inspects the manifest and resources: a missing manifest attribute … is invisible to `flutter analyze` and to every Dart test."* MADR 0083's L1 (edge-to-edge enforced by targetSdk, nothing compensating) was precisely a platform-level regression that no Dart test could see. Worth stating plainly: the Android job is otherwise **unusually well gated** — it asserts release-mode AOT via `aapt`, and pins the signing certificate's SHA-256 so a swapped keystore fails the build (`.github/workflows/ci.yml:400-420`). Lint is the one hole in an otherwise deliberate wall. | `android/app/build.gradle.kts` lint block; `.github/workflows/ci.yml:400-420` | medium |
| D3 | `WAKE_LOCK` is declared in the manifest but nothing uses it: the foreground service sets `allowWakeLock: false` (`foreground_service.dart:51`) — deliberately, so a parked connection holds no radio/CPU lock — and no other Dart or Kotlin code references a wake lock. It is an unused permission on an app whose security posture is a selling point. | `AndroidManifest.xml`; `foreground_service.dart:51` | low |
| D4 | `UPDATE_PACKAGES_WITHOUT_USER_ACTION` is declared for the 0065 in-app updater. It only takes effect for the installer of record, so on a sideloaded build it is inert — correct to keep, but undocumented in the manifest comment next to it, unlike every other permission there. | `AndroidManifest.xml` | low |
| D6 | **Release builds carry no Dart-level obfuscation.** CI builds with `flutter build apk --release --target-platform android-arm64` and version flags only — no `--obfuscate --split-debug-info` (`.github/workflows/ci.yml:393-397`). D1's R8 finding covers the Java/Kotlin half; this is the Dart half, and the two are independent. Flutter's own deployment guide pairs the flags so symbols are randomised *and* a `SYMBOLS` file is retained for `flutter symbolize`. **This finding interacts with D1 of the outcome below** — see the coupling note there. | `.github/workflows/ci.yml:393-397`; Flutter obfuscation guide (§Research R5) | medium |
| D7 | **Validation, not a defect — recorded so it is not "improved" into a regression.** The manifest declares `foregroundServiceType="remoteMessaging"`. Android 15 introduced 6-hour-per-24-hour timeouts, after which `Service.onTimeout` fires and an ANR follows if the service does not stop — but those apply to **`dataSync` and `mediaProcessing` only**, not `remoteMessaging`, and `remoteMessaging` is among the types permitted to start without a visible activity. The type chosen for this app's "walk away and get pinged" feature is the one that is both semantically right and exempt from the timeout that would break it. Any future switch to `dataSync` would silently adopt a 6-hour ceiling. | `AndroidManifest.xml`; Android FGS docs (§Research R4) | — |
| D5 | The manifest is otherwise notably well-hardened and this record should not imply otherwise: `allowBackup=false` plus backup/extraction rules, cleartext disabled app-wide with the pinning rationale documented, the `mcremote://pair` deep link deliberately removed with a re-adding checklist, `enableOnBackInvokedCallback` for predictive back, `stopWithTask` on the service. | `AndroidManifest.xml`, `network_security_config.xml` | — |

### E — Static-analysis strictness

| # | Finding | Evidence | Severity |
| --- | --- | --- | --- |
| E1 | `analysis_options.yaml` is the **unmodified Flutter template**: `include: package:flutter_lints/flutter.yaml` and an empty `rules:` block. No `strict-casts`, no `strict-raw-types`, no `strict-inference`, and — most pointedly — no `unawaited_futures`, even though the codebase calls `unawaited(...)` **95 times**, i.e. the team already treats fire-and-forget futures as something to mark deliberately. Nothing enforces it, so the 96th is a silent unhandled rejection. | `analysis_options.yaml`; 95 `unawaited(` sites in `lib/` | medium |

## Research: official guidance and comparable projects

Conducted 2026-08-13 against primary documentation first, then peer projects.
Two findings **corrected this record's own draft proposals**; they are marked.

| # | Source | What it establishes | Effect here |
| --- | --- | --- | --- |
| R1 | [Flutter — Handling errors](https://docs.flutter.dev/testing/errors) | The official recipe is **three** hooks: `FlutterError.onError` (framework build/layout/paint), `PlatformDispatcher.instance.onError` (async and platform-channel errors, returning `true`), and `ErrorWidget.builder` (build-phase UI, installed via `MaterialApp.builder`). The page **does not recommend `runZonedGuarded`** — since Flutter 3.3, `PlatformDispatcher.onError` is the supported catch-all, and zone-wrapping brings its own "zone mismatch" failure mode when bindings are initialised in a different zone. | **Corrects draft D1**, which proposed `runZonedGuarded` as one of three. Dropped in favour of the documented pair + error widget. |
| R2 | [shared_preferences README](https://github.com/flutter/packages/blob/main/packages/shared_preferences/shared_preferences/README.md) · [pub.dev](https://pub.dev/packages/shared_preferences) | Since 2.3.0 there are three APIs. The legacy `SharedPreferences` *"will be deprecated in the future"*; the team *"highly encourage"* `SharedPreferencesAsync` / `SharedPreferencesWithCache`, defaulting to **DataStore Preferences** on Android as *"the platform-recommended preferences storage system"*. Two explicit warnings: the plugin *"must not be used for storing critical data"* (async persistence, no guarantees), and its caching *"creates synchronization issues across multiple isolates or engine instances"*. A supported migration helper, `migrateLegacySharedPreferencesToSharedPreferencesAsyncIfNecessary()`, is idempotent across launches via a `migrationCompletedKey`. | Upgrades **B1** from design smell to supported-API problem; supplies **B4**; and gives **D3/D4 of the outcome** a documented migration path rather than a hand-rolled one. |
| R3 | [Flutter cookbook — Parse JSON in the background](https://docs.flutter.dev/cookbook/networking/background-parsing) | `compute()` is the documented remedy for JSON parses that exceed a frame budget; only simple values may cross the isolate boundary. | Confirms **B2**'s fix is the standard one, and that the payload (plain JSON maps) already satisfies the boundary constraint. |
| R4 | [Android — Changes to foreground services](https://developer.android.com/develop/background-work/services/fgs/changes) · [FGS timeouts](https://developer.android.com/develop/background-work/services/fgs/timeout) | Android 14 made `foregroundServiceType` mandatory and added `remoteMessaging`. Android 15's 6-hour/24-hour timeout — after which `Service.onTimeout` fires and an ANR follows if the service does not stop — applies to **`dataSync` and `mediaProcessing` only**. `remoteMessaging` may also start without a visible activity. | Produces **D7**: the app's existing choice is correct *and* exempt. Recorded as a validation so a future "cleanup" does not switch it to `dataSync` and inherit a 6-hour ceiling. |
| R5 | [Flutter — Obfuscate Dart code](https://docs.flutter.dev/deployment/obfuscate) | `--obfuscate` must be paired with `--split-debug-info=<dir>`, which emits a `SYMBOLS` file; `flutter symbolize` is the only way back from an obfuscated trace, and the file must be archived per build. Obfuscation renames symbols; it does not encrypt or prevent reverse engineering. | Produces **D6**, and the **coupling constraint** in the outcome: Dart obfuscation without archived symbols would turn this record's new crash recorder into unreadable noise. |
| R6 | [Dart — Customizing static analysis](https://dart.dev/tools/analysis) · [`unawaited_futures`](https://dart.dev/tools/linter-rules/unawaited_futures) | `strict-casts` / `strict-raw-types` / `strict-inference` live under `analyzer.language`; `unawaited_futures` requires async futures to be awaited **or** explicitly marked with `unawaited()` from `dart:async` — exactly the convention this codebase already follows by hand 95 times. | Confirms **E1** and makes D8 a matter of enabling what the team already practises. |
| R7 | [Android — R8 configuration](https://developer.android.com/agents/skills/performance/r8-analyzer/references/CONFIGURATION) and Flutter-community R8 practice | The standard release shape is `isMinifyEnabled` + `isShrinkResources` with `proguard-android-optimize.txt` plus a project rules file; missing keep rules for plugins surface **only at runtime in release**. | Confirms **D1**'s shape and its "revert-first" risk note. |
| R8 | [Immich](https://github.com/immich-app/immich/tree/main/mobile) (self-hosted media, Flutter + **Riverpod**, closest architectural peer) | Uses **Isar/Drift** for local storage — a real database — with SharedPreferences reserved for small key-value settings, and a provider/service/view split matching this app's own layering. | Independent confirmation that a peer at similar scale, on the same state-management stack, does not put bulk data in preferences. |
| R9 | Flutter offline-first practice surveys ([LeanCode](https://leancode.co/glossary/local-storage-in-flutter), [Dinko Marinac](https://dinkomarinac.dev/blog/best-local-database-for-flutter-apps-a-complete-guide/)) | Consistent guidance: SharedPreferences for small key-value only; SQLite/Drift for structured data; files for blobs. A recurring principle — *separate the storage shape, the wire shape and the UI shape, bridged by mappers* — matches what this app already does with `ChatItem.toJson`/`fromJson`. | Supports **D3**'s files-over-preferences choice, and its explicit rejection of adding a database dependency. |

### What the research changed

* **`runZonedGuarded` is out.** The draft proposed the folk-wisdom trio; the
  official page documents a pair plus the error widget, and the zone approach
  carries a mismatch failure mode this app would hit (bindings are initialised
  in `main` before `runApp`). The simpler, documented shape is also the safer
  one.
* **The storage fix got a supported path.** The draft proposed hand-rolled
  file storage plus a hand-rolled migration. Files remain right for
  transcripts, but the *preferences* half now has a first-party migration
  helper and a platform-recommended backend, so the plan should use it rather
  than invent one.
* **Obfuscation and diagnostics are coupled** — a constraint the draft missed
  entirely, and one that would have produced a crash recorder full of
  unreadable symbol names.

## Decision Drivers

* **A self-hosted app with no telemetry must be able to explain itself on
  device.** There is no crash service to fall back on; the only diagnostic
  channel is what the app persists and shows.
* **Don't regress the honesty posture**: MADR 0063 forbids simulated liveness,
  0067 requires honest iOS copy — a diagnostics feature must not overstate what
  it captured either.
* **Preserve the security posture**: no secrets in any new diagnostic surface
  (0066 D5's row records the exception, never the value), no new permissions,
  no widening of the network config.
* **Perf work goes where the user feels it**: cold start and chat-open, not
  micro-optimisation of already-cheap paths.
* **Prefer gates over vigilance**: each finding that a test or lint could have
  caught should end with that check enabled, not with a promise to remember.
* Incremental and independently shippable; no daemon or protocol change.

## Considered Options

* **O1 — Crash visibility only.** Install the error boundary and its
  diagnostics row; leave storage and build config alone.
* **O2 — Full hardening pass.** Error boundary and diagnostics; move the
  transcript cache off SharedPreferences and its decode off the UI thread;
  bound the growing map; close the retry race; enable the release and lint
  gates; tighten the analyzer.
* **O3 — O2 minus the storage migration**, keeping SharedPreferences and only
  moving the decode to an isolate.

## Decision Outcome

Chosen option: **"O2 — full hardening pass"**, because the three themes are
independent in code but identical in character — each is a place where the app
works until it doesn't, and then has nothing to say — and because every item
is small and separately revertable. O1 leaves a known multi-megabyte
main-isolate cost in place. O3 is tempting (the decode fix is most of the felt
jank) but keeps a blob store whose cap logic already has to *delete user data*
when a transcript will not fit (`transcript_cache.dart:100-105`); that is the
storage layer telling us it is the wrong one.

### Sub-decisions

**D1 — Install the error boundary Flutter actually documents.**
Per R1 (and *against* this record's own first draft): `main()` sets
`FlutterError.onError` — calling `FlutterError.presentError(details)` first so
console behaviour is unchanged — and `PlatformDispatcher.instance.onError`
returning `true`; `MaterialApp.builder` installs an `ErrorWidget.builder` that
in release renders a plain, non-technical panel with a "copy details"
affordance instead of the grey box. **No `runZonedGuarded`**: it is absent
from the official guidance, `PlatformDispatcher.onError` supersedes it as of
Flutter 3.3, and wrapping `runApp` in a zone after the bindings are
initialised in `main` is the documented recipe for a zone-mismatch error. All three sinks funnel to one recorder
that persists the last N (5) failures — timestamp, error type, message,
first frames of the stack — through `SettingsStore`, mirroring 0066 D5's
shape. **Never persists** anything from a caught credential path: the
recorder takes the exception's runtime type and message, and the existing
rule that no write path puts a secret in an error string (0074 D11) is what
makes that safe; the record states the dependency explicitly.

**D2 — Surface those failures in Settings, next to the storage row.**
A "Recent errors" row under *Storage & diagnostics*, reading "No failures
recorded" or "N recorded · most recent <time>", opening a list with
copy-to-clipboard. This is the reporting channel the app currently lacks;
it also makes A3's `debugPrint`-only sites worth upgrading to recorded ones
where the failure is user-affecting (notification setup, foreground service
start, reconnect).

**D3 — Move the transcript cache to files, one per session; move the
preferences that remain onto the supported API.**
`getApplicationSupportDirectory()/transcripts/<sessionId>.json`, index kept
in SharedPreferences (small), entries written atomically (temp + rename) at
0600-equivalent app-private permissions. This removes multi-megabyte JSON
from the preferences heap entirely, makes eviction a file delete, makes
`usage()` a `stat()` sum rather than a full read (fixes B3), and removes the
"drop the user's transcript because it exceeds a preferences-shaped cap"
branch. A one-time migration reads any existing prefs entries, writes them
as files, and deletes the keys; a failed migration drops the cache rather
than blocking start (the cache is by definition re-fetchable from the host).

Separately, and following R2: the small key-value state that legitimately
belongs in preferences (the cache index, `SettingsStore`'s desktop fallback)
moves to `SharedPreferencesWithCache` via the first-party
`migrateLegacySharedPreferencesToSharedPreferencesAsyncIfNecessary()` helper
rather than a hand-rolled migration. That retires the legacy API before its
deprecation lands, adopts the DataStore backend Android recommends, and
removes B4's cross-isolate landmine at the root. Explicitly **not** adopted:
a database (Drift/Isar). Peers at this scale do use one (R8), but this app's
cached transcripts are opaque, append-mostly blobs with no queries over
them — files match the access pattern, and a schema layer would be a
dependency bought for nothing.

**D4 — Decode off the UI thread.** `load()` moves its `jsonDecode` +
`ChatItem` construction into `compute()`, symmetric with `save()`'s existing
encode. The parse result crossing the isolate boundary is plain JSON —
already the shape `compute` requires.

**D5 — Bound `sessionLabels`.** Cap it (LRU, same 12-session order as the
transcript cache) and clear it in the coordinator's reset alongside
`_knownAsks`/`_shownAsks`.

**D6 — Close the idempotent-retry drop window.** Register the retry's
completer *before* removing the timed-out one — or, simpler and preferred,
do not remove it at all on the retry path: keep the id registered, replace
its completer, so a late first response completes the retry immediately
instead of being discarded. Covered by a test that delivers the response in
the window.

**D7 — Turn on the Android gates.** Enable R8 (`isMinifyEnabled`,
`isShrinkResources`) for release with an explicit `proguard-rules.pro`
carrying the keep rules Flutter and the plugins in use require (R7), verified
by the existing tag-build path plus an install-and-launch smoke; and run
`:app:lintVitalRelease` (with `-PmcAndroidLint=true`) in CI's Android job so
manifest and resource regressions fail the build rather than ship. Drop the
unused `WAKE_LOCK` permission (finding D3) and document
`UPDATE_PACKAGES_WITHOUT_USER_ACTION`'s installer-of-record caveat inline.
Keep `remoteMessaging` as the service type, with finding D7's exemption
rationale recorded next to it.

**Dart obfuscation is explicitly deferred, not forgotten** (finding D6).
`--obfuscate --split-debug-info` is only safe once the release pipeline
archives each build's `SYMBOLS` artifact and the runbook documents
`flutter symbolize` — otherwise it renders D1/D2's new crash reports
unreadable, defeating the highest-value item in this record for the sake of
symbol renaming that "does not encrypt … nor protect against reverse
engineering" (R5). Sequenced after the diagnostics land, with symbol
archiving as its precondition.

**D8 — Tighten the analyzer.** Enable `strict-casts` and
`strict-raw-types` in `analyzer.language`, and the `unawaited_futures` lint
that the codebase's own 95 `unawaited()` calls already imply. Fix the
resulting findings in the same phase; if any category proves large, it gets
its own follow-up rather than a blanket ignore.

### Value ranking — what to do first, and why

The research pass makes the ordering less a matter of taste. Ranked by
benefit-per-unit-effort, with the reasoning that decides each place:

| rank | item | benefit | effort | why here |
| --- | --- | --- | --- | --- |
| **1** | **D1+D2 — error boundary + diagnostics row** | **highest** | low (≈1 file + 1 row + tests) | Three documented hooks and a bounded recorder. It is the only item that changes what happens on the days the app *fails*, and for a no-telemetry product it is the entire difference between a reproducible report and "it went blank". Nothing else here is worth more per line. |
| **2** | **D4 — decode via `compute()`** | high | **very low** (one call site) | R3's documented one-liner removes up to 400 KB of main-isolate JSON parsing from chat-open — the single most-felt interaction in the app. Highest raw ratio in the table; ranked second only because D1 changes failure behaviour rather than smoothness. |
| **3** | **D8 — `unawaited_futures` + strict-casts** | high | low–medium (tail of fixes) | Turns a convention the team already follows 95 times into a gate. Every future silent unhandled rejection is caught at author time, which compounds with D1: fewer errors need catching at runtime. |
| **4** | **D7 (lint half) — `lintVitalRelease` in CI** | high | low | The only automated check that reads the manifest at all. MADR 0083 L1 shipped to a real device precisely because nothing did. |
| **5** | **D6 — retry drop window** | medium | very low | A contained fix to a 30-second user-visible stall on exactly the slow links the retry exists for. |
| **6** | **D3 — cache to files + supported prefs API** | high | **medium–high** (migration, persisted data) | The largest correctness+performance win, but the only item touching persisted user data and therefore the only one needing a migration path and rollback thought. Worth doing; not worth doing first. |
| **7** | **D5 — bound `sessionLabels`** | low | very low | Real but slow-burning; fold into whichever phase is already in that file. |
| **8** | **D7 (R8 half)** | medium | medium | Size and obfuscation, no behavioural gain — and the one change that can fail *only* in release. Deliberately last of the actioned items. |
| — | Dart obfuscation (finding D6) | low | medium | **Deferred by decision**: blocked on symbol archiving, and actively harmful to item 1 until then. |

A useful reading of that table: items 1–5 are together a few hundred lines,
carry no data migration and no release-only risk, and cover the two findings
rated *high* for user-visible consequence plus all three cheap gates. That is
the first plan phase; D3 is the second; R8 is the third and most revertable.

### Consequences

* Good, because the app gains the ability to answer "what happened?" on a
  device with no telemetry — the single biggest gap this audit found, and the
  one that makes every future bug report cheaper.
* Good, because the two highest-cost main-isolate operations (first
  preferences read, chat open) stop scaling with transcript size, and the
  cache stops having to delete user data to fit a store it should not be in.
* Good, because D7/D8 convert three of these findings from "someone should
  notice" into build failures; MADR 0083's L1 is the standing proof that the
  platform layer needs a gate the Dart tests cannot provide.
* Neutral, because none of it touches the daemon, the protocol, or the
  security posture: no new permissions (one is removed), no new network
  surface, no secret reaches the new diagnostics.
* Bad, because enabling R8 is the one change here that can break at runtime
  rather than at build time (missing keep rules surface as reflection
  failures in release only) — it needs the install-and-launch smoke, and it
  is the first candidate to revert if a release misbehaves.
* Bad, because the cache migration touches persisted user data; it is
  one-way and must be tolerant of partial state, which is why a failed
  migration is specified to drop the cache rather than retry forever.
* Bad, because `strict-casts` on a 29.5 kLOC codebase will surface a tail of
  small fixes that inflate that phase's diff.
* Neutral-but-worth-stating: this record was **revised by its own research**.
  The draft's `runZonedGuarded` proposal and its hand-rolled preferences
  migration were both replaced by documented alternatives, and a coupling it
  had missed (obfuscation defeating the crash recorder) became a sequencing
  constraint. A future reader should treat the pre-research draft's shape as
  superseded, not as an alternative that was weighed and rejected.

### Confirmation

* Unit/widget tests: the recorder captures and persists a thrown error;
  `ErrorWidget.builder` renders the release panel; the diagnostics row reads
  clean and populated; `sessionLabels` evicts past its cap; the retry-window
  test (a response delivered between timeout and retry completes the caller).
* Cache tests: file round-trip, atomic replacement, eviction by index,
  `usage()` without reading contents, migration from a seeded prefs state,
  and migration failure leaving the app startable.
* Build gates: `:app:lintVitalRelease` green in CI; a release APK built with
  R8 installs and reaches the sessions screen on a device.
* Measurement, recorded in the plan: time-to-first-frame and chat-open
  timings before/after D3+D4 on a real device with a full 12-session cache —
  the claim is a performance one and should be reported as a number, not an
  assertion.

## More Information

Two things this audit deliberately did **not** flag, recorded so a later
reader does not re-open them:

* **iOS background behaviour.** No background modes are declared, so the
  socket does not survive suspension. That is MADR 0067 D2's accepted
  position, and the Settings copy is already honest about it ("Alerts arrive
  while the app is open"). Not a defect; a documented product limit.
* **The WebSocket client's pending/close bookkeeping** (C3) and the
  connectivity/lifecycle policy in `app_lifecycle.dart`, which is the most
  carefully reasoned code in the app — the VPN-signal platform gate
  (`vpnSignalMeaningful`), the deliberate absence of a `mounted` guard around
  the background park, and the evidence-gated network-generation bump each
  encode a specific past bug. They were read closely and no defect was found.

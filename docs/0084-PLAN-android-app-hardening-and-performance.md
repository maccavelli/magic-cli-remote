<!-- markdownlint-disable MD004 MD013 MD024 MD033 MD036 -->

# Implement Android app hardening: crash visibility, cache storage, and build gates

Associated MADR: [0084-MADR-android-app-hardening-and-performance.md](0084-MADR-android-app-hardening-and-performance.md)

| field | value |
| --- | --- |
| status | **accepted** 2026-08-13; implementation in progress, phase by phase. |
| phases | P1 error boundary + diagnostics · P2 cheap correctness/perf batch · P3 static + platform gates · P4 transcript cache to files · P5 legacy preferences API retirement · P6 R8 release shrinking · (deferred: Dart obfuscation, precondition stated) |
| rule | Commit per phase; do not push until asked. Every phase leaves the app releasable; no phase depends on a later one. |

## Goal

Deliver MADR 0084 D1–D8 in the value order that record establishes: make the
app able to report its own failures, take the two highest main-isolate costs
off the UI thread and out of a store slated for deprecation, and convert three
findings into build gates — without new permissions, new network surface, or
any daemon/protocol change.

## Scope

In scope: `apps/mobile` (Dart, `pubspec.yaml`, `analysis_options.yaml`,
`android/app/build.gradle.kts`, `android/app/src/main/AndroidManifest.xml`) and
the Android job in `.github/workflows/ci.yml`. Out of scope: the daemon, the
protocol, iOS background modes (MADR 0067 D2 stands), and Dart-level
obfuscation (deferred with a stated precondition, §P7).

## Grounding facts (verified against the tree at `79c82dd`, 2026-08-13)

Each step cites these. If one no longer holds, stop and re-verify before
proceeding.

| # | Fact | Evidence |
| --- | --- | --- |
| G1 | `main()` is nine lines — `WidgetsFlutterBinding.ensureInitialized()` then `runApp(const ProviderScope(child: MagicCliRemoteApp()))`. No error hooks anywhere in `lib/`. | `lib/main.dart:1-9` |
| G2 | The root widget is `MaterialApp.router` inside `ConnectionLifecycleScope`, with `title`/`theme`/`darkTheme`/`themeMode`/`routerConfig` and **no `builder:`** — the parameter `ErrorWidget.builder` must be installed from. | `lib/app.dart:189-201` |
| G3 | `SettingsStore` already implements the exact persistence shape to mirror: key const `_kLastStorageFailure = 'last_storage_failure'` (`:136`), reader `getLastStorageFailure()` returning `({String op, String error, DateTime at})?` and swallowing malformed JSON (`:429-447`), writer `_persistStorageFailure()` which **bounds the message at 300 chars** and stamps `_clock().toUtc().toIso8601String()` (`:1038-1050`). `_clock` is an injectable test seam. | `lib/data/local/settings_store.dart` |
| G4 | The Settings hub renders a *Storage & diagnostics* `SettingsSection` containing the "Secret storage" row driven by `_storageFailure` state, and the receipts spoke. Rows added there inherit the 0082 D1 container styling and 0083's `listBottomPadding`. | `lib/features/settings/settings_screen.dart` |
| G5 | `TranscriptCache` keys prefs by `_indexKey = 'tx_cache_v1_index'` (a `StringList`) and `_entryPrefix = 'tx_cache_v1_'`; its test seam is the constructor parameter `TranscriptCache({SharedPreferences? prefs})`. **It serialises every mutation** through `_serial`/`_serialized()` because "two debounced saves interleaving across their awaits would drop one session from the index while its entry blob stays stored", and exposes `debugWhenIdle` for tests. Any rewrite must preserve that chain. | `lib/data/chat/transcript_cache.dart:18-44` |
| G6 | `save()` encodes via `compute(encodeTranscriptCachePayload, …)` (`:75`, and `:95` for the halve-and-retry); `encodeTranscriptCachePayload` is a top-level function (`:11-12`) — the shape a `compute` entry point requires. `load()` calls `jsonDecode` on the main isolate (`:131`) then builds `ChatItem`s in a loop (`:138-145`). | `transcript_cache.dart` |
| G7 | **`path_provider` is NOT a dependency.** The only existing on-disk write (the 0065 APK updater) uses `Directory('${Directory.systemTemp.path}/mcremote_app_updates')`. `shared_preferences: ^2.5.5` **is** present — ≥ 2.3.0, so `SharedPreferencesAsync`, `SharedPreferencesWithCache` and `migrateLegacySharedPreferencesToSharedPreferencesAsyncIfNecessary()` are all available without a version bump. | `pubspec.yaml:17`; `app_update_tile.dart:101` |
| G8 | `request()`'s timeout branch does `_pending.remove(id)` then re-enters `request(... requestId: id ...)`, which re-registers a new completer at `_pending[id] = completer`. The read loop completes only ids currently present (`if (id != null && _pending.containsKey(id)) _pending.remove(id)!.complete(env)`), so a reply arriving between removal and re-registration is dropped. | `mcremote_client.dart` `request()`; `:2748-2749` |
| G9 | `NotificationCoordinator.sessionLabels` is `final Map<String, String> sessionLabels = {}` (`:457`), written at `:128` and from `sessions_screen.dart:144,331`, read at `:475`; the reset at `:113-114` clears `_shownAsks`/`_knownAsks` but not it. | cited lines |
| G10 | `analysis_options.yaml` is the stock template: `include: package:flutter_lints/flutter.yaml`, empty `rules:`, no `analyzer:` section. `lib/` contains 95 `unawaited(` call sites. | `analysis_options.yaml` |
| G11 | The Android CI job builds `flutter build apk --release --target-platform android-arm64` (+ version flags) at `ci.yml:393-397`, then runs `scripts/assert-flutter-release-apk.sh` (`:408-410`) and the signing-certificate pin (`:412-420`). Gradle lint is gated behind `-PmcAndroidLint=true` and no workflow passes it. | `.github/workflows/ci.yml`; `android/app/build.gradle.kts` |
| G12 | `buildTypes.release` sets only `signingConfig` — no `isMinifyEnabled`, no `isShrinkResources`, no `proguardFiles`, and no `proguard-rules.pro` exists in `android/app/`. | `android/app/build.gradle.kts` |
| G13 | `AndroidManifest.xml` declares `WAKE_LOCK`; the only wake-lock reference in Dart is `allowWakeLock: false` (`foreground_service.dart:51`). Service type is `remoteMessaging` (exempt from the Android 15 timeout — MADR 0084 finding D7). | manifest; `foreground_service.dart` |
| G14 | Test conventions in force: `flutter test` currently 945 passing; widget tests must mock `PackageInfo.setMockInitialValues` and override `notificationCoordinatorProvider` when pumping `SettingsScreen` (both channels hang otherwise — established in 0082 P1). | `test/settings_screen_test.dart` |

Gate for every phase, run from `apps/mobile` before the phase commit:

```sh
dart format --output=none --set-exit-if-changed . && dart analyze && flutter test
```

Phases touching Go or CI additionally run `go build ./... && go test ./... &&
go vet ./...` and `gofmt -l cmd internal` from the repo root (expected: no Go
changes in this plan, so this is a no-regression check).

## Implementation Steps

### P1 — Error boundary and on-device diagnostics (MADR D1, D2)

1. **New file** `apps/mobile/lib/data/diagnostics/error_recorder.dart`:

   ```dart
   /// One recorded failure. `kind` is the exception's runtimeType, never the
   /// object itself — the value could hold anything (MADR 0084 D1).
   class RecordedError {
     const RecordedError({required this.kind, required this.message,
         required this.stack, required this.at, required this.source});
     final String kind;     // e.reasonableRuntimeType
     final String message;  // bounded, see step 3
     final String stack;    // bounded, see step 3
     final DateTime at;
     final String source;   // 'flutter' | 'platform' | 'app'
     Map<String, dynamic> toJson(); factory RecordedError.fromJson(...);
   }

   class ErrorRecorder {
     ErrorRecorder(this._store, {DateTime Function()? clock});
     Future<void> record(Object error, StackTrace? stack, {required String source});
     Future<List<RecordedError>> recent();
     Future<void> clear();
   }
   ```

   `record()` must **never throw** — wrap its whole body in try/catch and
   `debugPrint` on failure. The diagnostics layer failing must not be able to
   take down the error path it exists to serve.
2. **Extend `SettingsStore`** (mirroring G3 exactly, same file):
   * `static const _kRecentErrors = 'recent_errors_v1';`
   * `Future<List<Map<String, dynamic>>> getRecentErrors()` — decodes a JSON
     list, returns `const []` on any malformed value (same swallow-and-return
     posture as `getLastStorageFailure`);
   * `Future<void> appendRecentError(Map<String, dynamic> entry)` — read,
     append, **keep the newest 5**, write;
   * `Future<void> clearRecentErrors()`.
   Written through the same `_p` accessor, so P5's migration carries it
   automatically.
3. **Bounds, fixed and tested** (an unbounded recorder is a second bug):
   `message` truncated to **300 chars** (matching G3's precedent), `stack` to
   the **first 20 lines and 2000 chars**, ring of **5** entries. Constants
   live next to the recorder as named `const`s so the test asserts the
   constant, not a literal.
4. **Secret safety, stated as a dependency**: the recorder persists
   `error.runtimeType.toString()` and `error.toString()`. That is safe only
   because MADR 0074 D11 established that no credential write path puts a
   secret in its error text, and MADR 0083 D5 kept raw engine text on the
   daemon side. A comment in `error_recorder.dart` records this dependency by
   name so a future change to either is visibly load-bearing.
5. **Edit `lib/main.dart`** (G1) to the officially documented shape (MADR
   0084 R1) — note the **absence** of `runZonedGuarded`:

   ```dart
   void main() {
     WidgetsFlutterBinding.ensureInitialized();
     final recorder = ErrorRecorder(SettingsStore());
     FlutterError.onError = (details) {
       FlutterError.presentError(details);           // keep console behaviour
       unawaited(recorder.record(details.exception, details.stack,
           source: 'flutter'));
     };
     PlatformDispatcher.instance.onError = (error, stack) {
       unawaited(recorder.record(error, stack, source: 'platform'));
       return true;                                   // handled
     };
     runApp(ProviderScope(
       overrides: [errorRecorderProvider.overrideWithValue(recorder)],
       child: const MagicCliRemoteApp(),
     ));
   }
   ```

   with `errorRecorderProvider` added to `lib/state/app_providers.dart` so
   screens and tests resolve the same instance.
6. **Edit `lib/app.dart`** (G2): add a `builder:` to `MaterialApp.router` that
   installs `ErrorWidget.builder`. Per R1's example, the replacement widget is
   wrapped in a `Scaffold` when the failing subtree is one, so the panel is
   not painted on a bare surface. Release copy is plain ("Something in this
   screen failed to draw"), with a **Copy details** button putting
   `kind + message + first stack lines` on the clipboard; debug builds keep
   Flutter's red box (`kReleaseMode ? panel : ErrorWidget(details)`), because
   the red box is more useful during development.
7. **Settings row** (`settings_screen.dart`, in the *Storage & diagnostics*
   section from G4, directly under "Secret storage"): key
   `Key('settings-recent-errors')`, title "Recent errors", subtitle
   `'No failures recorded'` or `'N recorded · most recent <local time>'` using
   the existing `_formatLocalTime` helper, trailing chevron. Tapping pushes a
   `RecentErrorsScreen` (new file, `lib/features/settings/recent_errors_screen.dart`)
   listing entries newest-first: kind + local time as title, message as
   subtitle, tap to expand the stack, a **Copy** action per entry, and a
   **Clear** action in the app bar behind an `AlertDialog` confirmation keyed
   `Key('clear-errors-confirm')` (consistent with 0083's confirm-before-destroy
   posture).
8. **Upgrade the highest-value `debugPrint` sites to recorded ones** (MADR
   0084 A3) — only where the failure is user-affecting and currently silent:
   `foreground_service.dart:91` (service start failed → alerts will not
   arrive), `app_lifecycle.dart:100` (notification prefs read failed) and
   `:254` (reconnect threw). Each becomes `debugPrint` **plus**
   `recorder.record(..., source: 'app')`. Deliberately not converted: the
   best-effort decoration paths in `settings_screen.dart`, which fail
   invisibly by design.
9. **Tests** — new `test/error_recorder_test.dart`:
   * a recorded error round-trips through a fake store;
   * the ring keeps the newest 5 and drops the 6th-oldest;
   * a 5 000-char message and a 200-line stack are truncated to the declared
     constants;
   * `record()` returns normally when the store throws (must not rethrow);
   * `recent()` returns `const []` for malformed stored JSON.

   New `test/recent_errors_screen_test.dart`: empty state; populated list
   newest-first; Clear asks first and only clears on confirm.
   Extend `test/settings_screen_test.dart` (respecting G14's mocks): the row
   reads "No failures recorded" when empty and shows the count when
   populated.
   New `test/error_boundary_test.dart`: pumping a widget that throws in
   `build` under the app's `MaterialApp.router` `builder` renders the panel,
   not a raw exception, and the recorder captured one entry with
   `source: 'flutter'`.
10. Gate + commit.

### P2 — Cheap correctness and performance batch (MADR D4, D5, D6)

Grouped because all three are small, independent, and touch different files.

1. **Decode off the UI thread** (`transcript_cache.dart`, G6). Add a top-level
   entry point beside the existing encoder:

   ```dart
   /// Runs off the UI isolate: jsonDecode plus model construction, which
   /// together exceeded a frame budget on a 400 KB entry (MADR 0084 B2).
   SessionTranscript? decodeTranscriptCachePayload(String raw) { … }
   ```

   Move the whole body of `load()`'s `try` block into it — `jsonDecode`, the
   `ChatItem` loop, modes/collaboration-modes/goal parsing — and make `load()`
   `await compute(decodeTranscriptCachePayload, raw)`. Keep returning `null`
   for missing/corrupt, unchanged.
2. **Sendability is asserted, not assumed.** `compute`'s result crosses an
   isolate boundary. Add `test/transcript_cache_isolate_test.dart` asserting a
   real `compute(decodeTranscriptCachePayload, …)` returns an equal
   transcript. **If `SessionTranscript` proves unsendable**, the deterministic
   fallback is: the isolate returns the decoded `Map<String, dynamic>` (plain
   JSON, always sendable — the dominant cost is the parse) and the model
   construction stays on the main isolate. Take that branch only if the test
   fails; record which branch was taken in the commit message.
3. **~~Close the retry drop window~~ — WITHDRAWN during implementation.** The
   test in step 4 proved the race cannot occur: `_pending[id] = completer`
   runs synchronously before any `await`, so nothing can interleave with the
   catch block that re-enters `request()`. The attempted change also leaked
   the entry when the retry threw before registering, and was reverted. MADR
   0084 finding C2 is struck through with the reasoning. Original steps kept
   below for the record. In the
   `TimeoutException` branch, when `idempotentRetry` is true, **do not**
   remove the pending entry before recursing; instead let the recursive call
   overwrite `_pending[id]` with its fresh completer. Keep the existing
   `_pending.remove(id)` for the non-retry path (nothing will ever complete
   it). Add a comment naming the race. Concretely: hoist the removal into the
   non-retry branch only.
4. **Test the window** (`test/mcremote_client_test.dart`): drive a request
   with `idempotentRetry: true` and a short timeout against a fake socket that
   replies **after** the timeout fires; assert the caller resolves from that
   late reply rather than waiting a second full timeout. Use the existing
   fake-server harness in that file.
5. **Bound `sessionLabels`** (`notification_coordinator.dart`, G9). Replace
   the bare map with an insertion-ordered LRU capped at
   `kTranscriptCacheMaxSessions` (12 — reuse the constant so the label cache
   cannot outlive the transcript cache it describes): on write, `remove` then
   re-insert the key, and evict `keys.first` while over the cap. Add
   `sessionLabels.clear()` to the reset at `:113-114`.
6. **Test** (`test/notification_coordinator_test.dart` or new): 13 distinct
   session titles leave 12 entries with the oldest evicted; re-titling an
   existing session does not grow the map; reset clears it.
7. Gate + commit.

### P3 — Static and platform gates (MADR D8, D7-lint half)

1. **Edit `analysis_options.yaml`** (G10):

   ```yaml
   include: package:flutter_lints/flutter.yaml

   analyzer:
     language:
       strict-casts: true
       strict-raw-types: true

   linter:
     rules:
       # The codebase already marks fire-and-forget futures 95 times by hand
       # (MADR 0084 E1); this makes the 96th a build failure, not a silent
       # unhandled rejection.
       - unawaited_futures
   ```

   `strict-inference` is deliberately **not** enabled in this phase: it is the
   noisiest of the three and its findings are stylistic rather than
   correctness-shaped. Revisit separately if wanted.
2. **Fix the resulting analyzer findings** in the same commit. Expected
   classes: missing `await`/`unawaited()` on futures, and implicit
   `dynamic`→typed downcasts around JSON parsing. **Rule for this step: no
   blanket `// ignore_for_file:`.** If any single category exceeds ~30 sites,
   stop, keep the rule enabled with a scoped ignore *for that file only* plus
   a `TODO(0084)` naming the follow-up, and record the count in the commit
   message — an honest partial beats a suppressed whole.
3. **Add the Android lint gate to CI** (`.github/workflows/ci.yml`, G11). In
   the `Android APK (release arm64)` job, insert a step **before** the
   `flutter build apk` step (so a manifest error fails fast, before the
   expensive build):

   ```yaml
   - name: Android lint (manifest + resources)
     working-directory: apps/mobile/android
     run: ./gradlew -PmcAndroidLint=true :app:lintVitalRelease --no-daemon
   ```

   Rationale comment in the workflow: this is the only check that reads the
   manifest at all; MADR 0083 L1 shipped because nothing did.
4. **Verify the gate actually gates** — *done 2026-08-13, and it narrowed the
   claim*. Three defects were injected and the task re-run:
   * removing `android:exported` from `MainActivity` → **BUILD FAILED**
     ("android:exported needs to be explicitly specified"), via the manifest
     merger the lint task depends on. The gate works.
   * `foregroundServiceType="camera"` (mismatched with the declared
     permissions) → **passed**.
   * `@drawable/ic_stat_does_not_exist` in the notification-icon meta-data →
     **passed**, under both `lintVitalRelease` and the fuller `lintRelease`.

   `lintVital*` runs only FATAL-severity checks, so this is a
   **manifest-structure** gate, not a general Android-correctness gate. The
   CI step's comment states that scope explicitly, including that it would
   *not* have caught MADR 0083's L1 (a Dart-side layout issue). The original
   plan text implied otherwise; this is the correction.

   Local runs need `JAVA_HOME` pointed at JDK 21 — invoking `./gradlew`
   directly bypasses `flutter config --jdk-dir`, and Gradle 9.1 rejects the
   host's default JDK 26 during its own `jlink` step.
5. Gate + commit.

### P4 — Transcript cache to files (MADR D3, first half)

1. **Add `path_provider`** to `pubspec.yaml` dependencies (G7 — it is absent
   today), then `flutter pub get`. It is a first-party Flutter package; note
   in the pubspec comment why (app-private support directory for transcript
   blobs, MADR 0084 D3).
2. **Rewrite `TranscriptCache`'s storage layer**, preserving every existing
   behaviour that is not the storage medium:
   * entries at `<applicationSupportDirectory>/transcripts/<sessionId>.json`,
     with `sessionId` **percent-encoded** via `Uri.encodeComponent` so an id
     containing `/` or `..` cannot escape the directory (a path-traversal
     guard the prefs key shape made impossible and the file shape makes
     necessary — assert it in a test);
   * writes atomic: write `<name>.tmp`, `flush`, then `rename` onto the final
     path;
   * the index (a `List<String>` of session ids, newest-last) **stays in
     preferences** under `_indexKey` — it is small key-value state, exactly
     what preferences are for;
   * **the `_serial`/`_serialized` chain is preserved verbatim** (G5), and
     `debugWhenIdle` keeps working: the index read-modify-write race it
     guards is unchanged by moving blobs to files;
   * `_remove` deletes the file and drops the index entry; `clear()` deletes
     the directory contents and the index;
   * **the 400 KB cap and its halve-and-retry disappear** — a file has no such
     constraint. `kTranscriptCacheMaxItems` still bounds the tail, so entry
     size stays bounded by item count rather than by a store limit. Delete the
     "cannot store a current snapshot, drop the old one" branch
     (`transcript_cache.dart:100-105`) and its comment.
3. **`usage()` becomes a stat sum** (MADR 0084 B3): `File.stat().size` per
   index entry, reading no contents.
4. **Test seam**: replace the `SharedPreferences? prefs` constructor parameter
   (G5) with `TranscriptCache({SharedPreferences? prefs, Directory? directory})`,
   defaulting to `getApplicationSupportDirectory()`. Tests pass a temp
   directory; no test may touch the real one.
5. **One-way migration**, run once on first construction after upgrade:
   read every `tx_cache_v1_*` prefs key, write each as a file, delete the
   prefs keys, mark done with `tx_cache_migrated_v2 = true`. Wrap the whole
   migration in try/catch: **on any failure, delete what it can and start
   empty** — the cache is by definition re-fetchable from the host, so a
   failed migration must never block startup or lose the app's ability to run.
6. **Tests** (`test/transcript_cache_test.dart`, extended):
   * save→load round-trip through files;
   * eviction past `kTranscriptCacheMaxSessions` removes the oldest **file**,
     not just its index entry (the leak the serialization chain exists to
     prevent, now re-asserted for files);
   * `clear()` empties both;
   * `usage()` reports the summed file sizes without reading contents;
   * migration from a seeded prefs state produces the same `load()` results
     and leaves **no** `tx_cache_v1_` keys behind;
   * a corrupt/unreadable prefs entry during migration leaves the cache usable
     and empty rather than throwing;
   * a session id containing `../` writes inside the transcripts directory.
7. Gate + commit.

### P5 — Retire the legacy preferences API (MADR D3, second half)

1. **Introduce `SharedPreferencesWithCache`** as the app's preferences
   accessor. `WithCache` (not bare `Async`) is chosen deliberately:
   `SettingsStore` and the theme/mode providers do synchronous-feeling reads
   at startup, and `WithCache` keeps that shape while moving the backing store
   to DataStore. Central it in one accessor (`lib/data/local/prefs.dart`) so
   there is exactly one construction site.
2. **Run the first-party migration** (MADR 0084 R2) —
   `migrateLegacySharedPreferencesToSharedPreferencesAsyncIfNecessary()` — at
   app start, before the first preferences read, with the package's
   `migrationCompletedKey` left at its default so repeat launches are no-ops.
   Do **not** hand-roll this.
3. **Sequencing note, load-bearing**: P4 must land first. Migrating the
   preferences backend while ~4.8 MB of transcript blobs are still in the
   legacy store would copy those blobs into DataStore — the opposite of the
   point. P4 empties them out; P5 then migrates what is genuinely small.
4. **Update call sites**: `SettingsStore` (`_p`), `TranscriptCache`'s index,
   and any direct `SharedPreferences.getInstance()` user. Test seams that
   accept a `SharedPreferences` change type; update the fakes in
   `test/settings_store_test.dart` and friends accordingly.
5. **Tests**: existing `settings_store_test.dart` passes against the new
   backend; a migration test seeds legacy values, runs start-up, and asserts
   the values are readable and the legacy keys gone; a second run is a no-op.
6. **Removes B4 at the root** — note that in the commit message: the
   foreground-service isolate can no longer diverge from the UI isolate's
   cached view, because the cache is no longer per-isolate.
7. Gate + commit.

### P6 — R8 release shrinking (MADR D7, R8 half)

Last of the actioned phases: no behavioural gain, and the only change here
that can fail **only** in release.

1. **Edit `android/app/build.gradle.kts`** (G12), release build type:

   ```kotlin
   isMinifyEnabled = true
   isShrinkResources = true
   proguardFiles(
       getDefaultProguardFile("proguard-android-optimize.txt"),
       "proguard-rules.pro",
   )
   ```

2. **New `android/app/proguard-rules.pro`** with keep rules for the plugins in
   use, each line commented with *which* plugin needs it: `flutter_local_notifications`
   (reflection over receivers/serialized payloads), `flutter_foreground_task`
   (service + `RestartReceiver`, named in the manifest), `speech_to_text`,
   `mobile_scanner`, `image_picker`, `flutter_secure_storage`, and the
   `UpdateInstallReceiver`/`UpdateInstaller` classes referenced by name from
   the manifest (G13 neighbourhood). Start from each plugin's documented
   rules; do not invent.
3. **Drop the unused `WAKE_LOCK` permission** from the manifest (G13) and add
   a one-line comment next to `UPDATE_PACKAGES_WITHOUT_USER_ACTION` recording
   that it is effective only for the installer of record. Keep
   `remoteMessaging` and add the MADR 0084 D7 exemption note beside it.
4. **Prove it at runtime, not just at build**: build the release APK, install
   on a device/emulator, and exercise the paths R8 most endangers — launch,
   pair-scan (mobile_scanner), a notification arriving (local notifications +
   foreground service), voice input (speech_to_text), image attach
   (image_picker), and the update tile (FileProvider + receiver). Any missing
   keep rule shows up here or nowhere.
5. **Record the size delta** (before/after APK bytes) in the commit message.
6. Gate + commit.

### P7 — Deliberately not done in this plan

* **Dart obfuscation** (`--obfuscate --split-debug-info`, MADR 0084 finding
  D6). **Precondition**: the release pipeline must first archive each build's
  `SYMBOLS` artifact alongside the APK and document `flutter symbolize` in the
  release runbook. Until then it would render P1's recorded stack traces
  unreadable — defeating the highest-value item in this plan to gain symbol
  renaming that, per Flutter's own guide, "does not encrypt … nor protect
  against reverse engineering".
* **`strict-inference`** (P3 step 1) and **a local database** (Drift/Isar) —
  the MADR rejects the latter explicitly: the cached transcripts are opaque
  append-mostly blobs with no queries over them.
* **iOS background modes** — MADR 0067 D2 stands; the Settings copy is
  already honest.

## Verification

Per phase: the format/analyze/test gate above, plus the phase's own tests.

Whole-plan acceptance, after P6:

1. All suites green; `flutter test` count ≥ 945 + the ~25 tests this plan
   adds; `dart analyze` clean **with the stricter options in force**.
2. `lintVitalRelease` green in CI, and demonstrated failing once (P3 step 4).
3. **Measured, not asserted** (the MADR promises numbers). On one physical
   Android device, with a seeded cache of 12 sessions at
   `kTranscriptCacheMaxItems` each, record before/after for:
   * cold start to first frame — `flutter run --profile --trace-startup`,
     reading `timeToFirstFrameMicros` from `start_up_info.json`;
   * chat-open jank — `flutter run --profile` and the DevTools timeline, or
     `--trace-to-file`, capturing the frame count over 16 ms while opening a
     cached session.
   Report both as numbers in the P4 commit message and the MADR's follow-up
   note. If either fails to improve, say so — the change is still correct for
   the deprecation and cross-isolate reasons, but the perf claim must not be
   repeated unmeasured.
4. Release APK from P6 installs and passes the manual matrix in P6 step 4.
5. **Crash-path end-to-end**, on device: trigger a deliberate throw (a debug
   affordance is not shipped — use a temporary local patch), confirm the panel
   renders, the Settings row shows "1 recorded", the detail screen shows kind
   and truncated stack, and **Copy details** yields text with no credential in
   it.

## Rollout and Rollback

* One commit per phase; any prefix is releasable. Suggested tag point: after
  P3 (all the cheap wins and gates, no persisted-data change) and again after
  P6.
* **P1–P3 are revert-safe individually** — additive files, one config file,
  one workflow step. Reverting P1 leaves the recorded-errors prefs key
  orphaned and harmless.
* **P4 is the migration phase and the one to think about.** It is one-way
  (prefs → files). Rollback of the *code* after users have upgraded means the
  old code finds no `tx_cache_v1_*` keys and starts with an empty cache —
  degraded, not broken, and self-healing on next fetch from the host. That is
  acceptable precisely because the cache is not source of truth (MADR 0018
  E1); no other persisted data is touched.
* **P5 depends on P4** (step 3) and inherits the same posture: the package's
  own migration is idempotent and keyed, so a re-run is a no-op.
* **P6 is the most likely to need reverting**, and the cheapest to revert:
  three lines in `build.gradle.kts` plus a rules file. A release-only
  reflection failure is the expected failure mode; the P6 step 4 matrix is
  what catches it before users do.
* No daemon, protocol, or wire change anywhere in this plan; no new
  permissions (one is removed); no change to the network security config or
  the pinning path.

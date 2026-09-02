---
status: in-progress
date: 2026-09-01
associated-madr: "0126-MADR-android-client-debugging-pass-findings.md"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD060 -->

# PLAN 0126 — Make the Android client survive being backgrounded, and stop shipping surface nobody reviewed

Implements [0126-MADR-android-client-debugging-pass-findings.md](0126-MADR-android-client-debugging-pass-findings.md)
decisions D1–D9, closing findings F1–F8.

## Goal

The keep-alive service comes back after the OS kills it, a replaced connection
releases everything it holds, and no plugin can widen the shipped Android
surface again without CI saying so.

Finish line:

* `stopWithTask` is gone and `START_STICKY` is proven by `dumpsys`;
* a 4001 close leaves no armed timer, no open relay and no live loopback socket,
  asserted by a unit test;
* `scripts/assert-android-manifest-surface.sh` fails when a permission or an
  exported component appears that is not in the committed allowlist, and runs in
  the `android-apk` job;
* the installer's 40 MB copy is off the platform main thread;
* nothing in `apps/mobile/lib/` cites a "60 s" daemon deadline, and
  `ServerCaps.readDeadlineMs` either has a reader or is deleted;
* `make apk` produces an APK whose `versionName` is the real build version;
* the owner has run P7 on a physical Android device and pasted the output here.

## Scope

### In scope (the only files any phase may touch)

Android / build:

* `apps/mobile/android/app/src/main/AndroidManifest.xml`
* `apps/mobile/android/app/src/main/kotlin/com/maccavelli/magic_cli_remote/MainActivity.kt`
* `apps/mobile/android/app/src/main/res/xml/file_paths.xml`
* `apps/mobile/android/manifest-surface.allow` (new)
* `scripts/assert-android-manifest-surface.sh` (new)
* `Makefile` (`apk`, plus one new `manifest-surface` target)
* `.github/workflows/ci.yml` (`android-apk` job only)

Dart:

* `apps/mobile/lib/data/ws/mcremote_client.dart` — the 4001 branch, two test
  seams, the stale ping comment
* `apps/mobile/lib/data/ws/link_health.dart` — the stale deadline comment
* `apps/mobile/lib/data/protocol/models.dart` — `ServerCaps` fields (P5 only)
* `apps/mobile/lib/data/notifications/foreground_service.dart` — comment only
* `apps/mobile/lib/app_lifecycle.dart` — one `catchError` body
* `apps/mobile/lib/data/chat/transcript_cache.dart`
* `apps/mobile/lib/features/settings/app_update_tile.dart` — download directory
* `apps/mobile/pubspec.yaml` — one comment
* Tests: `replaced_close_test.dart`, `transcript_cache_test.dart`,
  `link_health_test.dart`, `app_update_tile_test.dart`, and one new
  `manifest_surface_test` equivalent as a shell test

### Out of scope

* **iOS.** MADR 0067 D2 parks the socket unconditionally there and none of
  F1–F5 applies. Do not "while we're here" the iOS lifecycle.
* **Re-enabling R8.** `proguard-rules.pro` measured byte-identical output
  (MADR 0084 D7). Untouched.
* **`kAppPingPeriod`'s value, `kLinkFreshFor`, and the `LinkHealth` bands.**
  D5 restates *why* 10 s is correct; it does not change it. Changing the
  freshness contract is MADR 0063's record, not this one.
* **The transcript cache's item/session caps, the `compute()` isolate strategy,
  and the batch window.** P6 fixes three durability gaps and touches nothing
  about performance.
* **`AppUpdateService.isNewerBase` vs `NewerPublished`.** MADR 0126 F8 records
  it as correct-for-now (all 97 tags are three-part) and explicitly not a
  defect. Do not change it in this plan; it needs its own record when a
  four-part tag is first published.
* **Adding `SCHEDULE_EXACT_ALARM` / `USE_EXACT_ALARM`.** See C3.
* **Battery-optimisation exemption prompting.** Named in P1 as the follow-on it
  is, and deliberately not taken here.

## Stability rule

Every phase ends with:

```bash
cd apps/mobile && dart format . && flutter analyze && flutter test
```

then **one commit** (`git commit --no-edit`; never `-m`). Phases that touch Go
or shell also run `make pre-add-check` before staging.

`dart format` first, not last: CI runs
`dart format --output=none --set-exit-if-changed .` and fails on it before it
ever reaches the tests.

`git push` needs an explicit instruction in the same turn.

## Cross-cutting contracts

**C1 — Every terminal socket path runs `_teardownSocket`.** D3. If a new branch
in `_onSocketDone` / `_onSocketError` returns early, it owns a reason in a
comment for why the bundle it is abandoning is already closed. F2 existed
because "park quietly" was allowed to mean "skip the cleanup".

**C2 — The allowlist is generated once, by hand, from a real build, and then
only ever edited deliberately.** D4. Never regenerate it from the artifact to
make CI green — that turns the gate into a rubber stamp. A red
`assert-android-manifest-surface.sh` is a question to answer, not a file to
overwrite.

**C3 — No new permission is added to make a restart path work.** D2. The
alarm-driven restarts are best-effort on API 31+ and the honest fix is to say
so, not to acquire `USE_EXACT_ALARM` (which is for alarm-clock apps) or to
quietly widen the manifest this plan exists to narrow.

**C4 — No behaviour change is justified by "the tests still pass".** 1358 tests
pass on the tree that has F1 in it. Each phase names the *new* evidence it
produces.

**C5 — Comments that state a number state where it comes from.** D5. Both F4
sites were wrong because they hard-coded a figure and a line number that later
moved. Cite the file and the constant, not the value.

**C1 and C2 are the ones at risk.** C1 because the natural F2 fix is a two-line
`unawaited(_teardownSocket(...))` that nobody adds a test for; C2 because the
first time a plugin bump reddens CI, overwriting the allowlist will look like
the obvious move.

## Dependency and delivery order

```text
P1 (stopWithTask)  ─┐
P2 (4001 teardown) ─┼─> releasable together; independent of each other
                    │
P3 (manifest gate) ─┘   MUST follow P1: the allowlist encodes P1's outcome
P4 (installer)          independent
P5 (ping cadence doc)   independent
P6 (low-severity)       independent
P7 (device verification) MUST follow P1, P2, P4
```

P1 and P2 are the shippable pair. P3 cannot be written before P1 because the
allowlist has to record whether `RECEIVE_BOOT_COMPLETED` and `RebootReceiver`
survive. P7 can only run on the owner's hardware.

## Implementation Steps

### P1 — Let the keep-alive service come back (D1, D2; closes F1)

**Edit 1 — `AndroidManifest.xml:116-120`.** Delete the
`android:stopWithTask="true"` attribute. Do not replace it with `"false"`; the
absent attribute is what `ForegroundServiceUtils.isSetStopWithTaskFlag()` reads
as false via `ServiceInfo.FLAG_STOP_WITH_TASK`, and an explicit `"false"` is
equivalent but reads as if something turned it off rather than as the default.

**Edit 2 — do not touch `ForegroundTaskOptions`.** `foreground_service.dart:52-60`
must keep leaving `stopWithTask` unset (null). This is load-bearing and is the
one way to get this phase wrong: setting it from Dart writes the
`stopWithTask` preference, which `ForegroundServiceUtils.isSetStopWithTaskFlag`
prefers over the manifest **and** which trips
`ForegroundService.kt:130-136` into installing `TrackVisibilityUtils` — stopping
the service every time the app becomes invisible. That is worse than the bug.
Add a comment at the `ForegroundTaskOptions(` call saying exactly this.

**Edit 3 — rewrite the manifest comment at `:112-115`** so it describes the
artifact. It currently claims the `RestartReceiver` "relaunches the service if
the OS kills it" while the attribute two lines below made that impossible. The
replacement must state all four recovery paths and their real reliability:

```text
START_STICKY               reliable — the OS recreates the service itself,
                           so API 31+'s background-FGS-start rule does not apply
onDestroy restart alarm    best-effort on API 31+
onTaskRemoved restart alarm best-effort on API 31+
RebootReceiver             inert: autoRunOnBoot / autoRunOnMyPackageReplaced
                           are both left at their false defaults
```

Best-effort, precisely (C3): `RestartReceiver.setRestartAlarm` uses
`setAlarmClock` only when `canScheduleExactAlarms()` is true, and this app holds
neither `SCHEDULE_EXACT_ALARM` nor `USE_EXACT_ALARM`, so it takes the inexact
`AlarmManager.set(RTC_WAKEUP, …)` branch. An inexact alarm is not on Android
12's foreground-service-launch exemption list, so the receiver's
`startForegroundService` throws `ForegroundServiceStartNotAllowedException`
unless the app is exempt from battery optimisation — which the plugin itself
logs a warning about. Write that down; do not fix it here.

**The one thing this phase changes for users:** swiping the app off recents no
longer guarantees the service is gone. That was the original intent of
`stopWithTask` and it is being traded away deliberately, because the service
existing is the feature. Note in the commit that `NotificationCoordinator`
already stops the service on sign-out and on the notifications toggle
(`notification_coordinator.dart:277-278, 315-321`), so the user retains an
explicit off switch.

**Verification.** Structural only at this phase — no Dart test can observe it,
which is exactly how F1 survived:

```bash
cd apps/mobile && flutter build apk --config-only --release --target-platform android-arm64
grep -c 'stopWithTask' android/app/src/main/AndroidManifest.xml   # -> 0
python3 - build/app/intermediates/merged_manifests/release/processReleaseManifest/AndroidManifest.xml <<'EOF'
import sys, xml.etree.ElementTree as ET
A='{http://schemas.android.com/apk/res/android}'
app=ET.parse(sys.argv[1]).getroot().find('application')
svc=[e for e in app.findall('service') if e.get(A+'name','').endswith('ForegroundService')][0]
assert svc.get(A+'stopWithTask') is None, svc.attrib
print('ok: FLAG_STOP_WITH_TASK not set on the merged service')
EOF
```

Real proof is P7. This phase ends with the structural check and the honest note
that the behaviour is unverified until then.

### P2 — A replaced connection releases what it holds (D3; closes F2)

**Edit 1 — `mcremote_client.dart:2491-2497`.** Add the teardown the branch
skips, keeping the synchronous `_setState` so the existing park semantics and
`replaced_close_test.dart`'s timing are untouched:

```dart
if (_channel?.closeCode == kCloseReplaced) {
  _failAllPending('connection replaced');
  debugPrint('mcremote: connection replaced by a newer login');
  _setState(McConnectionState.disconnected);
  // Parking is a state decision, not a licence to skip cleanup (0126 D3/F2).
  // Without this the 10 s ping timer stayed armed and — on the relay path —
  // the outer WSS, its HttpClient and the loopback ServerSocket stayed open
  // until the next dial, which for a replaced client may never come.
  // suppressReconnect mirrors disconnect(); the next _connectLeg clears it.
  unawaited(
    _teardownSocket(suppressReconnect: true).catchError((Object e) {
      debugPrint('mcremote: replaced-close teardown failed: $e');
    }),
  );
  return;
}
```

Two things to check while writing it, both of which the existing tests will
answer: `_teardownSocketImpl` awaits `sub?.cancel()` on the very subscription
whose `onDone` is running (legal on a completed subscription, and the second
test in `replaced_close_test.dart` covers the contrasting path), and
`_suppressReconnect` is deliberately left latched — `_connectLeg` clears it at
`:2192`, immediately after `_adoptOpenedSocket`, the same way `_connectInternal`
already relies on.

**Edit 2 — two test seams**, in the style of the existing `debugIdentityFuture`
/ `debugEnsureIdentity` on this class:

```dart
/// Whether the periodic app-ping timer is armed (0126 F2). A parked or torn
/// down client must not leave one running.
@visibleForTesting
bool get debugPingArmed => _pingTimer?.isActive ?? false;

/// Whether any per-socket resource is still held (0126 F2): channel,
/// subscription, pinned HttpClient or relay bridge.
@visibleForTesting
bool get debugSocketResourcesHeld =>
    _channel != null ||
    _sub != null ||
    _httpClient != null ||
    _relayTransport != null;
```

**Verification.** Extend `test/replaced_close_test.dart` — the `_ClosingPeer`
harness already does exactly the right thing. Add to the existing 4001 test,
after the current assertions:

```dart
expect(client.debugPingArmed, isFalse,
    reason: '0126 F2: a parked client must not keep a 10 s wakeup armed');
expect(client.debugSocketResourcesHeld, isFalse,
    reason: '0126 F2: the replaced socket bundle must be released');
```

Then add the guard that makes the seam meaningful, so a future early `return`
in the same handler cannot pass:

```dart
test('contrast: a live connection does keep the ping armed', () async {
  // …connect as above…
  expect(client.debugPingArmed, isTrue);
});
```

Run `flutter test test/replaced_close_test.dart` and confirm **both new
assertions fail before Edit 1 and pass after**. Record that before/after in the
commit; a test that was green either way proves nothing.

### P3 — A committed Android surface, checked in CI (D4; closes F3)

Depends on P1.

**Step 1 — decide the removals, one at a time, with the reason.** The merged
release manifest currently carries four permissions the repository does not
declare. They are not equivalent:

| permission | injected by | verdict |
|---|---|---|
| `ACCESS_NETWORK_STATE` | connectivity_plus | **keep** — `Connectivity().onConnectivityChanged` is the app's network-change signal (`app_lifecycle.dart:128`) |
| `VIBRATE` | flutter_local_notifications | **keep** — the ask and error channels are `Importance.high` with `enableVibration` left at its default true; removing it silences the buzz on approval alerts |
| `WAKE_LOCK` | flutter_foreground_task | **remove** via `tools:node="remove"` — the app sets `allowWakeLock: false` and `allowWifiLock: false` (`foreground_service.dart:58-59`) and schedules no notifications, so nothing acquires one. This is the permission `AndroidManifest.xml:29-33` already claims was removed |
| `RECEIVE_BOOT_COMPLETED` | flutter_foreground_task | **decide** — see step 2 |

Do not batch these. `VIBRATE` in particular looks like obvious dead weight and
is not.

**Step 2 — `RECEIVE_BOOT_COMPLETED` and `RebootReceiver` are one decision.**
After P1 the receiver is still inert, because `autoRunOnBoot` and
`autoRunOnMyPackageReplaced` both default false. Two coherent outcomes:

* **Remove both** (`tools:node="remove"` on the permission and the receiver).
  Narrowest surface; loses the option of restarting the service after an in-app
  APK update.
* **Keep both and set `autoRunOnMyPackageReplaced: true`** in
  `ForegroundTaskOptions`, making the receiver do something real: the service
  returns by itself after the updater replaces the package, instead of the user
  having to tap the "Updated — tap to open" notification
  (`UpdateInstaller.kt:111-148`). Boot autostart stays off.

Recommend the second: it is the one that serves the same promise as P1, and it
converts an exported no-op into a reviewed, used component. Whichever is
chosen, record it in this plan before editing.

**Step 3 — `scripts/assert-android-manifest-surface.sh`.** Follows the naming
and fail-closed style of `scripts/assert-flutter-release-apk.sh`.

* Argument: path to a merged `AndroidManifest.xml`. Default:
  `apps/mobile/build/app/intermediates/merged_manifests/release/processReleaseManifest/AndroidManifest.xml`.
* Fails with a clear message if that file does not exist — never passes on a
  missing input.
* Emits one line per fact, `sort`ed and de-duplicated so the file has a single
  canonical form:

```text
permission <android:name>
exported <tag> <android:name> <android:permission or "-">
```

* Diffs against `apps/mobile/android/manifest-surface.allow`.
* **Fails on additions and on removals**, with `diff -u` output. A removal is
  how you find out a plugin dropped `POST_NOTIFICATIONS`.
* Implementation: `python3` heredoc over `xml.etree` (present on macOS and on
  `ubuntu-latest`), wrapped in `set -euo pipefail`.

**Step 4 — the allowlist.** Generate it once from a real post-P1, post-step-2
build, read every line, and commit it with a header comment naming why each
non-obvious entry is there. C2: this file is edited by a human with a reason,
never regenerated to clear a red build. Starting content, before step 2's
decision and P1's removals are applied:

```text
exported activity com.maccavelli.magic_cli_remote.MainActivity -
exported receiver androidx.profileinstaller.ProfileInstallReceiver android.permission.DUMP
exported receiver com.pravera.flutter_foreground_task.service.RebootReceiver -
permission android.permission.ACCESS_NETWORK_STATE
permission android.permission.CAMERA
permission android.permission.FOREGROUND_SERVICE
permission android.permission.FOREGROUND_SERVICE_REMOTE_MESSAGING
permission android.permission.INTERNET
permission android.permission.POST_NOTIFICATIONS
permission android.permission.RECEIVE_BOOT_COMPLETED
permission android.permission.RECORD_AUDIO
permission android.permission.REQUEST_INSTALL_PACKAGES
permission android.permission.UPDATE_PACKAGES_WITHOUT_USER_ACTION
permission android.permission.VIBRATE
permission android.permission.WAKE_LOCK
permission com.maccavelli.magic_cli_remote.DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION
```

That is the verbatim output of the step-3 script against the merged manifest of
the last release build on this tree — plain `sort` order, which is why the
`exported` lines come first. Two entries look like mistakes and are not:

* `DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION` is injected by AGP for
  `androidx.core`'s dynamically-registered non-exported receivers. It is a
  signature-level permission the app defines for itself. Leave it.
* `RebootReceiver` is exported and unguarded because the plugin declares it that
  way; step 2 decides whether it stays at all. Its allowlist line is the record
  that this was looked at, not that it was liked.

**Step 5 — wire it in.** A `manifest-surface` target in the Makefile
(`--config-only` build, then the script) so it is runnable locally, and a step
in `ci.yml`'s `android-apk` job placed immediately after the existing
"Android lint (manifest + resources)" step — that step already runs
`flutter build apk --config-only` and `./gradlew :app:lintVitalRelease`, so the
merged manifest exists by then and the check costs seconds. Put it there rather
than after the APK build so a surface change fails in ~1 minute, matching the
stated reason that lint step sits where it does.

**Verification.**

```bash
make manifest-surface                                   # passes on the committed tree
printf 'permission android.permission.BLUETOOTH\n' >> apps/mobile/android/manifest-surface.allow
make manifest-surface                                   # MUST fail (removal detected)
git checkout -- apps/mobile/android/manifest-surface.allow   # ← ask first; see note
```

Note: that last line discards a tracked edit. Make the negative test on a copy
(`cp` the allowlist aside, mutate the copy, point the script at it) rather than
dirtying the tracked file — the repository forbids `git checkout --` without
explicit approval.

### P4 — Get the 40 MB copy off the main thread (D6; closes F5)

**Edit 1 — `MainActivity.kt`.** A `MethodChannel` handler runs on the platform
main thread and `UpdateInstaller.installApkSession` streams the whole APK plus
an `fsync` inside it. Move the work to a single-thread executor and marshal both
the `startActivity` fallback and every `result.*` call back to the UI thread —
`MethodChannel.Result` may only be completed there.

```kotlin
private val installExecutor: ExecutorService = Executors.newSingleThreadExecutor()

override fun onDestroy() {
    installExecutor.shutdown()
    super.onDestroy()
}
```

and, in the handler, after the existing `path.isNullOrEmpty()` guard:

```kotlin
installExecutor.execute {
    var sessionError: Exception? = null
    if (preferSession) {
        try {
            UpdateInstaller.installApkSession(this, path)
            runOnUiThread { result.success(null) }
            return@execute
        } catch (e: Exception) {
            sessionError = e          // fall through to the v1 intent
        }
    }
    try {
        val intent = UpdateInstaller.installApkIntent(this, path)
        runOnUiThread {
            try {
                startActivity(intent)
                result.success(null)
            } catch (e: Exception) {
                result.error("install_failed", e.message, null)
            }
        }
    } catch (e: Exception) {
        val msg = e.message ?: sessionError?.message
        runOnUiThread { result.error("install_failed", msg, null) }
    }
}
```

Behaviour preserved exactly: session-first, silent fallback to the v1 intent,
`bad_args` / `install_failed` codes unchanged. The only difference is which
thread does the copying. Keep `sessionError` — today a session failure followed
by an intent failure reports only the second, and the first is the interesting
one.

**Edit 2 — put the download somewhere the FileProvider can be narrowed to.**
`app_update_tile.dart:100-102` uses
`Directory('${Directory.systemTemp.path}/mcremote_app_updates')`. What
`Directory.systemTemp` resolves to on Android is an engine detail this plan
should not depend on. Replace it with `path_provider`'s
`getTemporaryDirectory()` — already a dependency, documented to return
`context.getCacheDir()` on Android — and keep the `mcremote_app_updates`
subdirectory:

```dart
final dir = widget.cacheDir ??
    (Directory('${(await getTemporaryDirectory()).path}/mcremote_app_updates')
      ..createSync(recursive: true));
```

`app_update_tile_test.dart` injects `cacheDir`, so tests are unaffected; confirm
that rather than assuming it.

**Edit 3 — `res/xml/file_paths.xml`.** Reduce three roots to the one that is
used:

```xml
<paths>
    <!-- The updater's download directory, and nothing else (0126 D6).
         external-cache-path and files-path were never used; files-path
         path="." covered the whole of getFilesDir(), which is where
         getApplicationSupportDirectory() keeps transcripts/. -->
    <cache-path name="app_updates" path="mcremote_app_updates" />
</paths>
```

Order matters: Edit 2 lands before Edit 3, or the next in-app update cannot
build a content URI for its own APK. A previously downloaded APK under the old
path becomes unreachable; it is re-downloaded, which is correct.

**Verification.**

```bash
cd apps/mobile && flutter test test/app_update_tile_test.dart test/app_update_test.dart
```

plus, on device (folded into P7): install a release APK, run the in-app update,
and confirm no ANR and no `Skipped … frames` burst in `logcat` during the copy.
State plainly in the commit that the ANR risk is reasoned from the measured
40,286,708-byte APK (`proguard-rules.pro:11`) and is not yet observed.

### P5 — Say what actually pins the ping cadence (D5; closes F4)

The MADR's 2026-09-01 amendment settles the direction: `kAppPingPeriod` is
correct and its stated reason is not. Do **not** derive the period from
`readDeadlineMs` — anything above `kLinkFreshFor` (15 s) renders a healthy idle
session amber.

**Edit 1 — `link_health.dart:10-15`.** `kLinkDeadAfter`'s doc says
"Half the daemon's 60 s read deadline (`internal/ws/server.go:165`)". The
default is 120 s with a 15 s floor (`internal/config/config.go:912`, `:939-940`)
and the cited line no longer holds the constant. Rewrite per C5: name the config
field, not the number.

**Edit 2 — `link_health.dart:34` (`kAppPingPeriod`).** Add the reason that is
actually load-bearing: the period must stay at or below `kLinkFreshFor` or an
idle session cannot render green, so this is bounded by MADR 0063 D1's UI
contract and only incidentally by the host deadline.

**Edit 3 — `mcremote_client.dart:2378-2383`.** Same correction in the
`_startPing` comment. Keep the "unconditional, this is a protocol obligation"
point — that part is right.

**Edit 4 — give `ServerCaps.readDeadlineMs` a reader, or delete it.**
Recommended: a guard, not a driver. In `_connectLeg`, right after
`serverCaps = ServerCaps.tryParse(...)` (`:2240`), when the advertised deadline
is too short for the fixed cadence to hold it open:

```dart
final deadlineMs = serverCaps?.readDeadlineMs ?? 0;
if (deadlineMs > 0 &&
    _appPingPeriod.inMilliseconds * 3 >= deadlineMs) {
  // The cadence is pinned by kLinkFreshFor (0063 D1) and cannot be raised
  // to fit; say so once, rather than presenting as unexplained drops.
  debugPrint(
    'mcremote: host read deadline ${deadlineMs}ms is too short for a '
    '${_appPingPeriod.inSeconds}s app ping — expect drops on idle sessions',
  );
}
```

with the same string recorded through `ErrorRecorder` so it reaches
`recent_errors_screen.dart` rather than a release no-op.

`ServerCaps.tryParse` has **two** call sites — the auth path at `:2240` and
the pair path at `:2022`. Put the check in one private method and call it
from both, or a freshly paired client is the one that never gets warned. If the owner prefers,
delete `readDeadlineMs`, `pingIntervalMs` and `wsPingResetsDeadline` from
`ServerCaps` instead — but D5 does not permit leaving a decoded field with no
reader.

**Verification.** A table test in `link_health_test.dart` pinning the invariant
so a future edit to either constant fails loudly:

```dart
test('the app ping verifies inside the freshness window (0126 D5)', () {
  expect(kAppPingPeriod, lessThanOrEqualTo(kLinkFreshFor));
});
```

and:

```bash
grep -rn '60 s\|60s' apps/mobile/lib/data/ws/     # no daemon-deadline claims left
grep -rn 'readDeadlineMs' apps/mobile/lib/        # has a reader, or is gone
```

### P6 — The low-severity cluster (D7, D8, D9; closes F6, F7, F8)

Three unrelated fixes, one phase, because each is a handful of lines.

**F6 — `app_lifecycle.dart:108-112`.** Replace `ref.read(errorRecorderProvider)`
with the already-captured `_recorder`. The class captured it in `initState`
with a comment saying `ref` dies with the element (`:50-58`); this is the one
path that did not use it. One-line change, no test — the failure it guards
needs a preferences error racing a dispose.

**F7 — `transcript_cache.dart`, three gaps:**

1. **`load()` and `usage()` race deletions** (`:304-314`, `:388-401`). Both do
   `existsSync()` then an async read; `_retainOnly` can delete in between. Wrap
   each file access so a `FileSystemException` yields "absent", not a throw:
   `load` returns null, `usage` skips the entry. Do **not** put them on the
   `_serialized` chain — that would queue a chat-open behind a debounced save
   and an isolate spawn, trading a rare race for constant latency.
2. **`_directory` runs the migration twice under concurrency** (`:170-179`).
   `_dir` is assigned before `await _migrateLegacyEntries(dir)`, so two
   first-touchers both migrate. Memoise the future instead of the value, and
   clear the memo on failure so a transient error is not cached forever:

   ```dart
   Future<Directory>? _dirFuture;
   Future<Directory> get _directory =>
       _dirFuture ??= _openDirectory().onError<Object>((e, st) {
         _dirFuture = null;
         throw e;
       });
   ```

3. **Orphans are never swept** (`:240-243`, `:228-229`, `:348-358`). A process
   death between `writeAsString` and `rename` strands `<name>.json.tmp`, which
   `_storedIds`' `.json` filter hides from both eviction and `clear()`. Sweep
   `*.json.tmp` in `_openDirectory`. Separately, `_sessionIdFromFile` calls
   `Uri.decodeComponent`, which throws on a malformed escape — one junk filename
   makes `clear()` fail every time it is invoked. Make it return `null` on
   `FormatException` and have `_storedIds` skip those entries.

   **Do not delete unrecognised files.** Skipping an unreadable name is the fix;
   sweeping the directory of anything that fails to parse is how a cache turns
   into a data-loss bug.

**F8 — `Makefile:432-433`.** `make apk` passes no version, so a local build is
`versionName 0.1.0 / versionCode 1` and the in-app updater compares `v0.15.3`
against `0.1.0` forever — breaking on-device testing of the updater itself. CI
is already correct (`ci.yml:620-621`), and `scripts/build-apk.sh:20-28` already
solves this; the `apk` target simply never used it. Add the same resolution,
with the ledger explicitly not consumed:

```make
apk:
	@set -e; \
	VER="$$(MCREMOTE_VERSION_PUSH=0 MCREMOTE_VERSION_TAG=0 $(NEXT_VERSION_SH) | tail -1)"; \
	BUILD_NAME="$${VER%.*}"; BUILD_NUMBER="$${VER##*.}"; \
	...
```

`MCREMOTE_VERSION_PUSH=0 MCREMOTE_VERSION_TAG=0` is the same pair `preflight`
uses for its release build: a developer's local APK must not claim a serial from
the shared ledger or push a `build/*` tag. Fall back to an unstamped build if
the version does not match `^[0-9]+\.[0-9]+\.[0-9]+$` and `^[0-9]+$`, exactly as
`build-apk.sh` does. Add one comment line to `pubspec.yaml:4` recording that
`0.1.0+1` is a placeholder overridden at build time, so the next reader does not
"fix" it by bumping it by hand.

**Verification.**

```bash
cd apps/mobile && flutter test test/transcript_cache_test.dart test/history_replay_test.dart
```

plus two new `transcript_cache_test.dart` cases: `load()` returns null when the
entry is deleted mid-read, and `clear()` still succeeds with a `%zz.json` file
in the directory. Then:

```bash
make apk
"$ANDROID_HOME"/build-tools/*/aapt dump badging \
  apps/mobile/build/app/outputs/flutter-apk/app-release.apk | head -1
# expect versionName to be the BASE.N build version, not 0.1.0
git tag -l 'build/*' | wc -l   # unchanged — no serial claimed
```

### P7 — Owner verification on a physical Android device (closes the MADR's Confirmation block)

**Cannot be done from the development host.** No emulator substitute: F1 is
about low-memory kills and recents behaviour, and F5 is about real storage
throughput.

Install the P1–P4 release APK, pair, then run these four rows and paste the
actual output into the execution record — not a summary:

```text
1  background, swipe from recents, raise a permission_request host-side
   -> the alert arrives
   adb shell dumpsys activity services | grep -A3 flutter_foreground_task
   -> the service is present again after the swipe

2  adb shell am kill com.maccavelli.magic_cli_remote  (with the app backgrounded)
   -> START_STICKY recreates the service; the alert still arrives

3  in-app update over the previous build
   -> no ANR; logcat shows no "Skipped NNN frames" burst during the copy
   -> after install, pairing survives and (if P3 step 2 chose it) the
      service returns without a manual open

4  adb shell dumpsys package com.maccavelli.magic_cli_remote | grep -A20 'requested permissions'
   -> matches manifest-surface.allow exactly
```

Row 1 is the one this plan exists for. If it fails, this plan returns to P1 and
the battery-optimisation exemption named in C3 becomes its own decision — it
does not get added here to make a row go green.

## Verification (whole plan)

```bash
cd apps/mobile && dart format --output=none --set-exit-if-changed . \
  && flutter analyze && flutter test
make manifest-surface
make apk
```

```text
unit tests              -> P2's two new assertions, P5's invariant, P6's two cache cases
structural checks       -> no stopWithTask; allowlist matches; APK versionName stamped
owner device run        -> P7's four rows
```

### Acceptance criteria

1. `flutter analyze` clean and `flutter test` green, with the suite count
   **above** the current 1358 — this plan adds tests, and a flat count means a
   phase shipped without one.
2. `test/replaced_close_test.dart` fails on the pre-P2 tree and passes after
   (recorded in the commit).
3. `scripts/assert-android-manifest-surface.sh` passes on the committed tree and
   fails on a mutated copy of the allowlist, in both directions.
4. `make apk` emits an APK whose `versionName` is `BASE.N` and which claimed no
   `build/*` tag.
5. No occurrence of a "60 s" daemon read deadline remains under
   `apps/mobile/lib/`, and `readDeadlineMs` either has a reader or is deleted.
6. P7's four rows are pasted into the execution record with real output.

## Rollout and Rollback

Ships in the normal tag flow; no migration and no persisted-format change. The
transcript cache's on-disk layout is unchanged (P6 only makes its readers
tolerant), so a downgrade reads the same files.

The one user-visible behaviour change is P1: the keep-alive service now outlives
a swipe from recents. Rollback is restoring `android:stopWithTask="true"` on the
`ForegroundService` element — a one-attribute revert with no state to undo.
P3's allowlist would then need the corresponding line restored, which is
precisely the gate working.

## Deferred — state and trigger (rewritten by 0128 P5)

Every entry names a **state** and, where open, an **observable trigger**
(0128 D5/C4).

### CLOSED

**`AppUpdateService.isNewerBase` vs `NewerPublished`.** **Fixed** by 0128 P2:
the phone now compares major, minor, patch **and the build serial**, mirroring
`update/run.go` under MADR 0103, so a serial-only release can no longer be
offered by the CLI and withheld by the app. A regression test covers the case
and was proven to fail under the old three-part compare.

**`NotificationService.dispose()` leaving `_ready` true.** **Fixed** by
0128 P3: `dispose()` clears `_ready` and `init()` recreates the closed
controller, so both halves of the coordinator/service pair now agree that
restart is supported.

**The `compute()` isolate spawn per debounced cache save.** **Measured and
closed** by 0128 P4: ~0.35 ms median per save against a 400 ms per-session
debounce — roughly 0.6 ms/second/session of overhead versus inline, on an
already-debounced path off the critical rendering path. Nothing to optimise.
The measurement raised a different question — whether `compute` is still
*needed* here at all, since the inline encode is 0.12 ms — which belongs with
MADR 0084's measurement set and is not worth touching for 0.2 ms.

**`autoRunOnBoot`.** A decision not to act, not pending work: starting a service
at boot is a larger claim on the user's device than restarting one they were
already using. Note that P3 step 2 *did* enable `autoRunOnMyPackageReplaced`,
which is the narrower sibling — the service returns after an in-app update only.
Reopen only if alerts are shown to be lost across a device reboot, which needs
P7 row 1 first.

### ~~GATED — on this plan's own P7~~ RESOLVED 2026-09-02: not needed

~~**Battery-optimisation exemption prompting**
(`FlutterForegroundTask.requestIgnoreBatteryOptimization`).
Trigger: **P7 row 1 shows `START_STICKY` alone does not restore alerts after a
swipe.**~~

**Resolved as "not needed"** by
[0129](0129-PLAN-background-alert-delivery-survives-task-removal.md) P6, which
is where rows 1 and 2 were finally run. Both pass **without** a
battery-optimisation exemption:

* row 1 — task removed, then a host-side `permission_request` arrived and was
  delivered by the foreground service's isolate with no Activity in existence;
* row 2 — a real `SIGKILL`, and the service, the isolate and the socket all
  returned in 3.5 seconds under `START_STICKY` alone.

So the trigger's condition — "`START_STICKY` alone does not restore alerts" —
is false, and the UX intrusion of asking a user to exempt the app from battery
optimisation buys nothing. Reopen only if alerts are shown to be lost on a
device whose OEM is more aggressive than stock AOSP; the emulator is the
permissive case, not the hard one.

### OPEN — this plan's remainder

**P7 rows 1–2 now pass** (0129 P6). What remains:

* **row 3** (no ANR during an in-app update) — untouched by 0129 and still
  unrun.
* **row 4** (the notification never claims "Connected" while no socket exists)
  — the case it was written to catch is fixed (a system-restarted service
  inherited a stale "Connected to host" for 29 seconds; now ~2s), but a
  distinct gap remains: the client can believe it is connected for ~33s after
  the socket dies, and the title mirrors that belief. That is connection
  liveness, not the alert path, and it is deferred pending measurement on a
  transport that is not flapping — see 0129's "OPEN — connection liveness
  detection latency".

Note that rows 1–2 no longer need "a physical device or a scene-camera QR": the
pairing obstacle recorded here was worked around in 0129's execution record.
Everything else in P7 is verified — see the execution record below.

## Execution record

### 2026-09-01 — P1 complete

**Edits applied.**

1. `android/app/src/main/AndroidManifest.xml:119` — `android:stopWithTask="true"`
   deleted (not set to `"false"`; the absent attribute is what
   `ForegroundServiceUtils.isSetStopWithTaskFlag()` reads as false via
   `ServiceInfo.FLAG_STOP_WITH_TASK`).
2. Same file, `:112-115` — the comment claiming `RestartReceiver` "relaunches
   the service if the OS kills it" replaced with the four-path table and the
   API 31+ best-effort caveat, plus the trade being made (swipe-away no longer
   guarantees the service is gone) and the user's remaining off switch.
3. `lib/data/notifications/foreground_service.dart:52-60` — the guard comment on
   the `ForegroundTaskOptions(` call, recording why the Dart-side `stopWithTask`
   override must stay unset: the plugin persists it as a preference that
   `isSetStopWithTaskFlag` prefers over the manifest, and a `true` value
   additionally installs `TrackVisibilityUtils`, stopping the service whenever
   the app becomes invisible.

**Verification.** The merged manifest is the authority and it passes:

```text
$ ./gradlew :app:processReleaseManifest   (JAVA_HOME=openjdk@21)
BUILD SUCCESSFUL
$ python3 <assert FLAG_STOP_WITH_TASK absent on the merged ForegroundService>
ok: FLAG_STOP_WITH_TASK not set on the merged service
    {'name': 'com.pravera...ForegroundService', 'exported': 'false',
     'foregroundServiceType': 'remoteMessaging'}
```

`dart format` clean, `flutter analyze` clean, `flutter test` `+1358 ~3`.

**Deviation — P1's `grep` check is not usable as written.** The phase specified
`grep -c 'stopWithTask' … -> 0`. That cannot hold: the replacement comment
explains the attribute's absence *by name*, so both the loose and the precise
(`android:stopWithTask`) greps still match prose. Naming it is the more valuable
outcome — a future reader needs to know why it is missing — so the comment
stands and the **merged-manifest assertion above is the check**. A grep over
source text was never able to distinguish an attribute from a comment about one.

**Not verified.** Everything behavioural. P1's real proof is P7 row 1, and the
plan says so; nothing here shows the service actually returns after a swipe.

### 2026-09-01 — interrupted by 0127 between P1 and P2

Executing P1 surfaced that `apps/mobile/pubspec.lock` was not reproducible under
the repository's own pinned toolchain — dependabot commit `6c02c8e` (2026-08-14)
had committed a resolution Flutter 3.44.8 could not honour, and every
`flutter pub get` since had silently reversed it.

That is out of this plan's scope, so it became its own pair:
[0127](0127-MADR-adopt-current-flutter-toolchain.md) /
[0127-PLAN](0127-PLAN-adopt-current-flutter-toolchain.md). 0126 resumes at P2
once 0127 completes.

**What changed underneath this plan** (fill in the remainder at 0127 P8):

* Toolchain: Flutter 3.44.8 / Dart 3.12.2 → **3.47.2 / 3.13.2**. P1's edits are
  toolchain-independent and were re-verified green on 3.47.2.
* `flutter test` baseline is unchanged at `+1358 ~3`, so P2's "suite count must
  rise" acceptance criterion still measures from 1358.
* **P3's allowlist is unaffected.** 0127 P4 re-captured the merged manifest
  surface on Flutter 3.47.2 and diffed it against the 2026-09-01 baseline:
  **identical**, all 16 lines. The allowlist drafted in P3 can be committed as
  written. Android SDK levels are also unchanged (compileSdk 36 / targetSdk 36 /
  minSdk 24), so P3's `tools:node="remove"` decisions stand.
* **P1's evidence is unchanged, only its version citation.** 0127 P6b took
  `flutter_foreground_task` 10.0.0 → **11.0.1** and re-read 11.x's Kotlin
  against all six claims MADR 0126 F1 makes. All six hold, at identical line
  numbers — 11.0.0 was a Kotlin-KGP and Swift-Package-Manager packaging release,
  not a service-lifecycle change. F1 carries a version note; P1's manifest
  comment needed no edit. The fix is not invalidated.
* **One extra reason P1 was worth doing:** the same bump cleared the plugin from
  both of Flutter 3.47's future-breaking warnings (Android KGP, iOS SPM). Two
  plugins still trip the KGP one — `mobile_scanner` and `speech_to_text`, both
  already at their newest published version with no migrated release. Tracked as
  0127's F-KGP.
* **Toolchain for the remaining phases:** Flutter 3.47.2 / Dart 3.13.2. The
  suite baseline is still `+1358 ~3`, so P2's "count must rise" criterion
  measures from 1358.

### 2026-09-01 — P2 complete (F2, D3)

**Edit 1 — `mcremote_client.dart`, the 4001 branch.** The `kCloseReplaced` case
now runs `unawaited(_teardownSocket(suppressReconnect: true))` before returning,
keeping the synchronous `_setState(disconnected)` so the park semantics and the
existing test's timing are untouched. The two checks the phase flagged both held
in practice: cancelling the subscription from inside its own `onDone` is legal
on a completed subscription, and leaving `_suppressReconnect` latched is safe
because `_connectLeg` clears it after adopting the next socket.

**Edit 2 — two test seams**, in the style of the class's existing
`debugIdentityFuture` / `debugEnsureIdentity`:

```dart
bool get debugPingArmed            // _pingTimer?.isActive ?? false
bool get debugSocketResourcesHeld  // channel | sub | httpClient | relay
```

**Verification — the assertions were proven to fail first.** With the production
fix temporarily reverted (via a `cp` backup and an inverse edit, restored
immediately after — no `git checkout`):

```text
fix REVERTED:
  00:02 +0 -1: 4001 replaced: parks disconnected, keeps pairing, never re-dials [E]
    Expected: false
      Actual: <true>
    0126 F2: a parked client must not keep a ping timer armed

fix RESTORED:
  00:04 +3: All tests passed!
```

A `true` ping timer on a parked client is exactly F2: a wakeup every
`kAppPingPeriod` for a socket that no longer exists, alongside an unreleased
HttpClient and relay bridge.

**Edit 3 — a contrast test.** `'contrast: a live connection does hold its socket
and ping'` asserts both seams are `true` while connected. Without it the two
new assertions would also pass against a client that never armed anything, which
would make them prove nothing — the same trap C5 caught in 0127 P7.

**Gate:** `dart format` clean, `flutter analyze` clean, `flutter test`
**`+1359 ~3`** — up from the 1358 baseline, satisfying the acceptance criterion
that the count must rise rather than merely stay green.

### 2026-09-01 — P3 complete (F3, D4)

**Step 1 — the removals, decided one at a time.** Only one of the four
undeclared permissions was dead weight:

| permission | verdict | why |
|---|---|---|
| `ACCESS_NETWORK_STATE` | keep | connectivity_plus; feeds `onConnectivityChanged`, which `app_lifecycle.dart:128` depends on |
| `VIBRATE` | **keep** | the ask and error channels are `Importance.high` with vibration at its default — removing it silences the buzz on approval alerts |
| `WAKE_LOCK` | **remove** | nothing acquires one: `allowWakeLock`/`allowWifiLock` false and no scheduled notifications |
| `RECEIVE_BOOT_COMPLETED` | keep | see step 2 |

`VIBRATE` is the one this phase warned about: it reads as obvious plugin cruft
and is load-bearing. Removing all four as a batch would have shipped a silent
approval alert.

**Step 2 — the recommended option taken.** `RECEIVE_BOOT_COMPLETED` and the
plugin's `RebootReceiver` are **kept**, and `ForegroundTaskOptions` now sets
`autoRunOnMyPackageReplaced: true`. The service therefore returns by itself
after the in-app updater replaces the package, instead of the user having to tap
the "Updated — tap to open" notification. `autoRunOnBoot` stays `false`:
starting a service at boot is a larger claim on the user's device than
restarting one they were already using.

Verified the option still exists and is honoured at the version 0127 P6b moved
us to — `flutter_foreground_task` **11.0.1**:
`foreground_task_options.dart:9,25` and `RebootReceiver.kt:49-51`.

This converts the exported receiver 0126 F3 complained about from an inert
no-op into a reviewed, used component.

**`WAKE_LOCK` is actually gone now.** The manifest comment claiming MADR 0084 D3
removed it has been corrected: the plugin declares it unconditionally and the
merger put it straight back, so the shipped APK carried it for the entire life
of that claim. `tools:node="remove"` is what removes it; a comment cannot.
Confirmed against the regenerated merged manifest — the surface dropped from 16
lines to 15.

**Steps 3–5 — the gate.** `scripts/assert-android-manifest-surface.sh` (reads
the merged manifest with `xml.etree`, emits sorted `permission …` /
`exported <tag> <name> <perm>` lines, `diff -u` against
`apps/mobile/android/manifest-surface.allow`, fails on additions **and**
removals), a `make manifest-surface` target, and a CI step in `android-apk`
placed immediately after the existing Android lint step — where
`processReleaseManifest` has already run, so it costs seconds and fails in ~1
minute.

The allowlist carries a header naming why each non-obvious entry is there
(`DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION` is AGP-injected and looks like junk;
`RebootReceiver` is exported and unguarded because the plugin declares it that
way — "reviewed, not liked").

**Proven failing in both directions before being trusted** (C2/C5), against
copies of the allowlist so no tracked file was dirtied:

```text
addition (allowlist missing VIBRATE):      +permission …VIBRATE      exit=1
removal  (allowlist has BLUETOOTH):        -permission …BLUETOOTH    exit=1
positive (real tree):   OK surface matches the allowlist             exit=0
```

**Gate:** `dart format` clean, `flutter analyze` clean, `flutter test`
`+1359 ~3`.

### 2026-09-01 — P4 complete (F5, D6)

**Edit 1 — `MainActivity.kt`.** `installApk` now runs on a single-thread
`ExecutorService` (shut down in `onDestroy`), with `runOnUiThread` around
`startActivity` and every `result.*` call — a `MethodChannel.Result` may only be
completed on the platform thread. Behaviour is preserved exactly: session
first, silent fallback to the v1 intent, same `bad_args` / `install_failed`
codes. The only change is which thread copies the ~41 MB APK.

One improvement kept from the phase's sketch: `sessionError` is retained, so
when the session install *and* the fallback both fail the session's error is
reported rather than only the second one.

**Edits 2–3, in that order.** `app_update_tile.dart` now derives the download
directory from `getTemporaryDirectory()` (documented to be `getCacheDir()` on
Android) instead of `Directory.systemTemp`, whose resolution is an engine
detail. Only then was `file_paths.xml` narrowed from three roots at `path="."`
to the single `cache-path` subdirectory the updater actually writes:

```xml
<cache-path name="app_updates" path="mcremote_app_updates" />
```

The dropped `files-path name="files_updates" path="."` covered the whole of
`getFilesDir()` — where `getApplicationSupportDirectory()` keeps the
`transcripts/` snapshots.

#### The phase's own assumption was wrong, and checking it mattered

P4 said *"`app_update_tile_test.dart` injects `cacheDir`, so tests are
unaffected; confirm that rather than assuming it."* Confirmed — and it does
not:

```text
grep -n "cacheDir"          test/app_update_tile_test.dart  -> no matches
grep -n "downloadAndVerify" test/app_update_tile_test.dart  -> no matches
```

**No test reached the download path at all.** The two existing tests inject
`service` and `installApk` and stop before it, so the tests passed for the wrong
reason: the changed line was never executed, and the narrowed FileProvider grant
would have shipped unverified.

Added `'download lands under getTemporaryDirectory/mcremote_app_updates'`,
using the suite's existing `useFakePathProvider` support. It asserts on the
*directory*, not on a successful install — the stubbed checksum is deliberately
wrong, so the download fails verification after the directory is created, which
keeps it a test of the path rather than of sha256.

**Proven to discriminate — at the second attempt.** The first revert experiment
reported "All tests passed" with the fix supposedly removed. The experiment was
the flaw, not the test: `dart format` had reflowed the line across three lines,
so the `str.replace()` matched nothing and silently changed the file not at all.
Re-run with an assertion on the search string:

```text
fix REVERTED:  PROBE want=…/mcremote_testig8VBJ/mcremote_app_updates exists=false
               Expected: true   Actual: <false>
               00:00 +0 -1: Some tests failed.
fix RESTORED:  00:00 +3: All tests passed!
```

Second time in this plan that a proof was itself wrong (0127 P7 gate 1 was the
first). A revert experiment needs its edit asserted, exactly like the gate needs
its negative test.

**Verification.** `dart format` clean, `flutter analyze` clean, `flutter test`
**`+1360 ~3`**. `./gradlew :app:compileReleaseKotlin` exits 0.
`assert-android-manifest-surface.sh` still OK — narrowing `file_paths.xml` does
not change the permission/exported surface.

**Not verified:** the ANR itself. The threading fix is reasoned from the
measured 40,286,708-byte APK (`proguard-rules.pro:11`) and Android's 5 s
threshold; it has not been observed on hardware. P7 row 3 is where that is
checked.

### 2026-09-01 — P5 complete (F4, D5)

The MADR's amendment settled the direction: the cadence is correct and its
stated reason was not. No timing value changed in this phase.

**Edits 1–3 — the stale claims.** `link_health.dart`'s `kLinkDeadAfter` doc and
`mcremote_client.dart`'s `_startPing` comment both cited "the daemon's 60 s read
deadline" at line numbers that no longer hold the constant. Both now name the
field instead of the number — `limits.ws_read_deadline_seconds`
(`internal/config/config.go`), **120 s default, floor 15 s** — per C5.

`kAppPingPeriod` now records what actually pins it: `kLinkFreshFor`.
`_noteInboundFrame` stamps `lastVerifiedAt` on any inbound frame, so on an idle
session the ping is the only thing verifying the link, and any period above the
freshness window leaves a healthy idle session amber. Deriving from the 120 s
deadline would give ~30 s and break the status indicator.

**Edit 4 — `readDeadlineMs` has a reader.** `_checkPingCadenceAgainstCaps()`
fires when `_appPingPeriod * 3 >= caps.read_deadline_ms`, i.e. when an operator
has configured the deadline near its 15 s floor. It reports through
`ErrorRecorder(_settings)` — no constructor change, no new coupling, and the
recorder's never-throws contract means a diagnostics write cannot break a
working connection — so it reaches `recent_errors_screen.dart` instead of a
release-mode no-op. Called from **both** `ServerCaps.tryParse` sites (auth and
pair), so a freshly paired client is warned too.

A guard, not a driver: the cadence cannot be shortened to fit without spending
battery on the platform that actually ships, and cannot be lengthened without
breaking `kLinkFreshFor`. What it can do is say so, rather than presenting as
unexplained mid-session drops with all the evidence already decoded and unused.

**Edit 5 — the invariant is pinned.** `kAppPingPeriod <= kLinkFreshFor`, plus a
classifier assertion that a link verified exactly one ping ago still reads
`fresh`. Proven to discriminate: raising `kAppPingPeriod` to 30 s fails it with
`Expected: <= 15s, Actual: 30s`.

**Found while verifying: two existing tests encoded the same stale figure.**
`link_health_test.dart:46-51` and `:53-58` both compared against a bare `60`
with comments asserting the daemon "drops a silent client at 60s". **Neither
assertion was weakened** — 60 s is kept as a deliberately conservative,
floor-safe bound, and the comments now say that is what it is rather than
claiming it is the default. Relaxing them to the real 120 s would have made them
catch less; the 15 s floor case is out of reach of any static bound, which is
what edit 4 exists for.

**A verification lesson, third in this pair.** The discrimination check first
appeared to pass because `grep … | head -5` truncated the output *before* the
failure line — the `+11` line was the test starting, and the `-2` failure came
after. Do not read a truncated test log as a result.

**Gate:** `dart format` clean, `flutter analyze` clean, `flutter test`
**`+1361 ~3`**. `grep -rn "server.go:165" lib/` returns only this phase's own
explanatory text.

### 2026-09-01 — P6 complete (F6, F7, F8; D7, D8, D9)

**F6 — one line.** `app_lifecycle.dart`'s prefs `catchError` now uses the
captured `_recorder` instead of `ref.read(errorRecorderProvider)`. The class had
already captured it, with a comment saying `ref` dies with the element; this was
the single path that did not use it — inside the handler whose whole job is to
survive a preferences failure.

**F7 — three gaps, and the fix had a bug the tests caught.**

1. *Losing reads.* `load()` and `usage()` do `existsSync()` then an async read;
   `retainOnly` can delete in between. Both now treat a `FileSystemException`
   as a cache miss. Deliberately **not** put on the `_serialized` chain: that
   would queue every chat-open behind a debounced save and an isolate spawn to
   close a rare race — trading a real latency cost for a hypothetical one.
2. *Double migration.* `_directory` memoises the **future** now, not the value,
   so two concurrent first-touchers share one open+migrate. `_dir` was assigned
   before awaiting `_migrateLegacyEntries`, so `load()` and `save()` racing on a
   cold cache both ran it over the same key snapshot. The memo is cleared on
   failure so a transient error is not cached for the process lifetime.
3. *Stranded temp files.* `_sweepTempFiles` removes `*.json.tmp` at open. Only
   that exact suffix — deleting anything that merely fails to parse is how a
   cache turns into a data-loss bug, and the new test asserts the undecodable
   file **survives**.

`_sessionIdFromFile` returns null instead of throwing. **The first version of
this caught `FormatException` and did nothing at all**: `Uri.decodeComponent`
throws `ArgumentError` ("Invalid URL encoding"), which is an `Error`, so neither
`on FormatException` nor `on Exception` would have caught it. The regression
test failed on exactly that and the catch was widened, with the reason recorded
at the call site so it is not "tidied" back to a specific type.

Three tests added: `clear()` survives a `%zz.json`; `load()` returns null after
a mid-read delete; a stranded `.json.tmp` is swept.

**F8 — `make apk` stamps the version.** Resolves `BASE.N` through
`scripts/next-build-version.sh` with `MCREMOTE_VERSION_PUSH=0
MCREMOTE_VERSION_TAG=0` — the same pair `preflight` uses, so a developer's local
build claims no serial from the shared ledger and pushes no `build/*` tag.
Falls back to an unstamped build on a malformed version, as
`scripts/build-apk.sh` already did. Verified:

```text
resolved: 0.15.3.2 -> name=0.15.3 number=2
build tags before=264 after=264   (nothing claimed)
```

`pubspec.yaml:4` now carries a comment saying `0.1.0+1` is a placeholder
overridden at build time, so the next reader does not "fix" it by bumping a
literal that would be wrong the moment a release is cut.

**Gate:** `dart format` clean, `flutter analyze` clean, `flutter test`
**`+1364 ~3`**, and gate 1 still reports `pubspec.lock` reproducible after the
pubspec comment.

### 2026-09-01 — P7 partial: emulator verification

**Deviation — P7 was written as device-only.** It says *"Cannot be done from
the development host. No emulator substitute."* The owner directed that the
host's Android emulator and iOS simulators be used for testing, so P7 ran
against **AVD `mcremote_test`, Android 16 / API 36, headless**. That changes
what the phase can conclude, not what it attempted, and the split is recorded
below rather than left implied by "P7 ran".

**Artifact under test:** `make apk` output after P1–P6, 41.0 MB,
`assert-flutter-release-apk.sh` OK.

#### Verified

**F8 — version stamping, end to end.** The first real proof, on the artifact
rather than the Makefile:

```text
aapt dump badging: versionCode='3' versionName='0.15.3'
build tags before=264  after=264      (no ledger serial claimed)
```

Previously this would have been `versionName='0.1.0' versionCode='1'`, which is
what made the in-app updater report an update for ever.

**F1 — `stopWithTask` is gone from the packaged binary manifest.** Not the
source, the APK:

```text
E: service (line=184)
  android:name="com.pravera.flutter_foreground_task.service.ForegroundService"
  android:exported=false
  android:foregroundServiceType=0x00000200   (remoteMessaging)
```

Three attributes, and `stopWithTask` is not among them. That is the flag the
plugin reads back through `isSetStopWithTaskFlag` to disable `START_STICKY`, the
`onDestroy` and `onTaskRemoved` restart alarms, and `RebootReceiver`.

**F3 / D4 — row 4 passes against the installed package.** `dumpsys package`'s
requested permissions diff **identical** to `manifest-surface.allow`, and
**`WAKE_LOCK` is absent from the real installed app** — the claim MADR 0084 D3
made and the artifact contradicted for its whole life.

**The build installs, launches and runs.** `adb install` Success;
`am start -n …/.MainActivity` → `topResumedActivity=…/.MainActivity`; process
alive; no `FATAL EXCEPTION` and no Dart error in logcat.

#### Not verified, and why

**Rows 1–3 need a paired connection to a live daemon, which this environment
cannot produce.** `docs/ops-android-emulator.md` records the reason: a typed
pair code cannot complete pairing (it carries no certificate fingerprint and the
app refuses an unpinned host — 0046/0074 working as designed), so the QR is the
only way in, and *"the scene camera cannot be aimed from adb … the pose is
driven by WASD/mouse in the emulator window only. This is the one manual step."*
Headless, there is no window to aim in at all.

Without pairing there is no connection transition, so `NotificationCoordinator`
never starts the foreground service — confirmed: `dumpsys activity services`
shows no `ForegroundService`. That leaves unproven:

* **row 1** — swipe from recents, then an alert still arrives;
* **row 2** — `am kill` and `START_STICKY` recreates the service;
* **row 3** — an in-app update completes with no ANR and no `Skipped … frames`
  burst during the 41 MB copy (P4's threading fix).

**These are exactly the rows the plan called load-bearing**, and P7 says so:
*"Row 1 is the one this plan exists for."* The manifest evidence above shows the
flag that *caused* F1 is gone; it does not show the service coming back. An
emulator could in principle show rows 1–2 given a paired session — the blocker
is pairing, not the emulator — so this is a gap in reachable setup, not a
statement that emulators cannot answer it.

**Status:** P7 stays **open**. Rows 1–3 need either a physical device with a
paired daemon, or a windowed emulator session where the owner can aim the
virtual-scene camera at the pair QR once.

### 2026-09-01 — status reconciliation

Statuses were stale across the three records this session produced. Corrected:

```text
0126-MADR   proposed -> accepted     decision approved and executed (F1-F8 fixed)
0126-PLAN   proposed -> in-progress  P1-P6 landed; P7 rows 1-3 still open
0128-MADR   proposed -> accepted     decision approved and executed
```

This plan is **`in-progress`, not `complete`**, and the distinction is the one
0128 P3 had just written into the skill: a plan is complete when every
acceptance criterion is met, not when the last commit lands. Criterion 6 —
P7's four rows — is not met.

Worth naming rather than quietly fixing: 0128 P3 added the plan status
lifecycle to `SKILL.md` in this very session, and then none of the three
records was updated to match it. Writing a convention and applying it are
separate acts; the second did not happen until it was asked for. Same shape as
F2, one document later.

### 2026-09-01 — P7 on the emulator: rows 1 and 2 FAIL, row 3 PASSES

Run against AVD `mcremote_test` (Android 16 / API 36), paired to the live daemon
at `100.64.0.3:7531` via the virtual-scene QR. Artifact: `make apk` output
**0.15.3.5**, release-mode assertion passed, clean-installed.

**A near-miss worth recording first.** The APK initially staged for this run was
built at 15:42; `apps/mobile/lib` last changed at 15:46 — it predated 0128 P3's
`NotificationService` fix. A timestamp comparison caught it. Had it not, P7
would have run green against a binary missing a fix and the result recorded as
verification of the current tree. Sixth verification-of-a-verification failure
in this pair, and the first that would have produced a **false pass**.

#### Row 1 — swipe from recents: **FAIL**

The swipe genuinely happened: `RecentsView: onTaskRemoved: 25`, and
`dumpsys activity recents` then matched the app **0** times.

What survived, and what did not:

```text
process            pid 3503 unchanged
service            isForeground=true  foregroundId=42  types=0x200 (remoteMessaging)
                   stopIfKilled=false  oom_score_adj=200
FGS notification   still "Connected to host"
socket to daemon   GONE — three samples over 9s, never returned
Dart isolate       GONE
```

**P1's fix demonstrably works.** With `android:stopWithTask="true"` the plugin's
`onTaskRemoved` would have called `stopSelf()`; it did not, and there were zero
`ForegroundServiceStartNotAllowed` refusals.

**But the feature does not.** The Dart isolate died with the Activity. Proven by
control rather than inference — thread names captured post-swipe, the app
relaunched, and the two sets diffed:

```text
present when LIVE, absent after the swipe:
  + 3.io
  + 3.raster
  + DartWorker
```

The WebSocket, the ping loop, the reconnect ladder and `NotificationCoordinator`
all live in the **main Dart isolate**, bound to the Activity's `FlutterEngine`.
Keeping the process alive does not keep that isolate alive.
`_KeepAliveTaskHandler` — the isolate that *does* survive — is a deliberate
no-op: empty `onStart`, empty `onRepeatEvent`.

**Second defect, found here:** the foreground-service notification read
"Connected to host" for minutes with no socket at all. MADR 0056 H-5a exists so
that title never lies; after a swipe nothing can correct it, because no Dart code
runs to notice.

#### Row 2 — OS kill: **FAIL, and the step was wrong as written**

`adb shell am kill` (what P7 specified) **does not kill a
foreground-service process** — pid unchanged, `DartWorker` still present, socket
still up. As written the row tested nothing. Re-run with `adb root` and a real
`SIGKILL`:

```text
SIGKILL pid 3503        -> process gone
after +12s              -> pid 4646   (START_STICKY recreated it)
FGS record              -> 1
DartWorker              -> 0
socket to daemon        -> not restored
```

Same conclusion by a different route: the service comes back, the isolate does
not.

#### Row 3 — in-app update: **PASS**

Driven through the real UI (host-side `adb emu screenrecord screenshot`; the
guest `screencap` is black for Flutter). A deliberately older build
(`0.14.0.1+2`) was installed so the updater had something to offer.

```text
Version tile            0.14.0.1+2          <- four-part stamping (0128 P2) on device
check                   "Update available: v0.15.3"   <- isNewerPublished, end to end
download                40,642,346 bytes
landed at               cache/mcremote_app_updates/magic-cli-remote-v0.15.3-arm64.apk
```

That path is exactly the one root P4 narrowed `file_paths.xml` to, so the grant
and the writer agree on device — not just in the test.

The ANR-critical section, sampling the main thread every 2 s across the copy and
session commit:

```text
t+02s..t+16s   main-thread state = S   (sleeping, never R or D)
ANRs                                  0
"Skipped N frames" warnings           0
PackageInstallerActivity displayed    +170ms
```

**Limit of the instrument, stated plainly:** emulator storage is host-backed and
fast, so this does **not** prove the pre-P4 code would have ANR'd on hardware.
What it establishes is that P4's threading change did not break the install path
and the main thread was never blocked during a real 40 MB copy.

The OS then declined at the `REQUEST_INSTALL_PACKAGES` policy gate — expected for
a sideloaded first install, and anticipated by the app's own dialog copy ("You
may need to allow 'Install unknown apps' … the first time"). That gate is
downstream of everything P4 changed.

#### Consequence

Rows 1 and 2 mean **0126 F1's fix is necessary but not sufficient**, and that the
deferred battery-optimisation item is aimed at the wrong layer — the service is
already alive; the isolate is not. Where the connection lives is an
architectural decision, so it goes to its own record rather than being absorbed
here. See 0129.

### 2026-09-02 — P7 row 3 attempted, **blocked by the emulator's network**

Not a pass, and not a failure of the code under test: the download never got far
enough to exercise the part row 3 is about.

**Setup, which needed no provider quota.** `make apk VERSION=0.15.2.1` stamps a
build older than the published release while keeping a higher `versionCode`
(14), so it installs over the current build with no downgrade and the updater
still sees `v0.15.3` as newer. Worth recording as the cheap way to exercise the
update path — the earlier assumption that this row needs a real release cycle
is wrong.

**What worked.**

* Version comparison: from `0.15.2.1+14`, Settings → App update reported
  **"Update available: v0.15.3"**.
* **Pairing survived the reinstall** — one of row 3's own criteria — and the app
  reconnected on launch without re-pairing.
* No ANR, and no `Skipped NNN frames` from this app. The one Choreographer
  warning in the run (`Skipped 82 frames`) belongs to pid 912,
  `com.android.systemui`, not `com.maccavelli.magic_cli_remote` (pid 11314).
  Attributing it to the app would have been a false failure.

**What blocked it.** The APK download died partway, twice, at the same place:

```text
App update
ClientException: Connection closed while receiving data,
uri=https://release-assets.githubusercontent.com/...
```

~40 MB over this AVD's network does not complete. The install and the
`PackageInstaller` session copy — the ANR-sensitive path that P4 moved onto
`Executors.newSingleThreadExecutor()`, and the only reason this row exists —
were **never reached**. Nothing here says anything about whether that fix
holds.

**Incidental, and genuinely useful:** a connection dropped mid-download is
surfaced in the tile as a readable error and the app stays responsive — it
neither crashes nor hangs on a truncated body.

**Still open.** Trigger: a network that can carry the release asset to the
device — a physical device on real Wi-Fi, or the APK served from the host so
the transfer stays local. The row itself is unchanged.

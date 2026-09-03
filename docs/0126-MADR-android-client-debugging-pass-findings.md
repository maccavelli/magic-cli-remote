---
status: accepted
date: 2026-09-01
decision-makers: Project Owner
consulted: none
informed: none
---
<!-- markdownlint-disable MD013 MD024 MD033 MD060 -->

# Fix the Android keep-alive and teardown defects found by the debugging pass, as one remediation pair

## Context and Problem Statement

A read-only assessment and debugging pass was run over `apps/mobile/`, weighted
towards Android because that is the platform with shipped users (97 release tags,
`android-apk` is the only artifact CI builds, `ios/` is simulator-only per
[0067](0067-MADR-ios-port.md)).

The pass found no failing gate. What it found are eight defects that no gate in
this repository can see, because every one of them lives in a place the gates do
not look: the **merged** Android manifest, a plugin's Kotlin source, an early
`return` on an uncommon socket close path, and a capability field that is parsed
and then never read.

### Baseline: every existing gate is green

Run on this tree, 2026-09-01:

```text
flutter analyze          No issues found! (ran in 5.5s)
flutter test             +1358 ~3   All tests passed!
```

So the findings below are not regressions against the suite. They are gaps the
suite does not cover, and F1 in particular is a defect in the app's headline
promise that 1358 green tests cannot observe, because it lives in
`flutter_foreground_task`'s Kotlin and is selected by one XML attribute.

### Method, and its limits

Everything below was verified by reading the tree, the merged build artifact, and
the resolved plugin sources in `~/.pub-cache`. **No finding was reproduced on a
physical device or emulator** — the assessment was read-only, per the
investigation rule. Severities are therefore stated as *reasoned consequence*,
not as observed user reports, and each finding names the exact confirmation that
would settle it. Where a claim depends on a plugin's runtime behaviour, the
plugin's own source is quoted rather than paraphrased.

---

## Findings

### F1 — `stopWithTask="true"` disables every path that would ever restart the keep-alive service (high)

The app's stated P0 is "walk away and get pinged". That depends entirely on
`com.pravera.flutter_foreground_task.service.ForegroundService` surviving in the
background. The app declares it, and next to it declares `RestartReceiver` with
this comment (`android/app/src/main/AndroidManifest.xml:112-115`):

```xml
<!-- flutter_foreground_task: the service that keeps the mesh socket
     alive in the background, plus its restart receiver (relaunches the
     service if the OS kills it). stopWithTask keeps it from lingering
     after the app is swiped away. -->
```

The two halves of that comment are in direct conflict, and the second one wins.
`android:stopWithTask="true"` (`AndroidManifest.xml:119`) is read back by the
plugin through `ForegroundServiceUtils.isSetStopWithTaskFlag()`, which falls
through to the manifest `ServiceInfo.FLAG_STOP_WITH_TASK` when no Dart-side
preference was written — and the app writes none. It therefore returns **true**,
which switches off all four recovery paths:

| path | plugin source | behaviour with `stopWithTask=true` |
|---|---|---|
| OS restart after a kill | `ForegroundService.kt:185-189` | returns `START_NOT_STICKY` — Android will not recreate the service |
| auto-restart alarm on unclean stop | `ForegroundService.kt:211-214` | guard is `&& !isSetStopWithTaskFlag(this)` — alarm never set |
| restart after swipe-away | `ForegroundService.kt:218-224` | `stopSelf()`, no alarm |
| restart on boot / package replace | `RebootReceiver.kt:31-34` | early `return` before reading either option |

**Version note (0127 P6b, 2026-09-01).** The table above was read against
`flutter_foreground_task` **10.0.0**; the app now ships **11.0.1**. All six
claims were re-verified line by line against 11.0.1 and are **unchanged** —
`isSetStopWithTaskFlag` is still prefs-then-manifest
(`ForegroundServiceUtils.kt:16-25`), `onStartCommand` still returns
`START_NOT_STICKY` under the flag (`:185-189`), `onDestroy`'s restart alarm is
still guarded by `!isSetStopWithTaskFlag` (`:211`), `onTaskRemoved` still calls
`stopSelf()` (`:218-225`), `RebootReceiver` still returns early (`:29-32`), and
the Dart-side `stopWithTask` override still trips the `TrackVisibilityUtils`
path (`ForegroundService.kt:130-136`). The line numbers are identical too:
11.0.0 was a Kotlin-KGP and Swift-Package-Manager packaging change, not a
service-lifecycle change.

`allowAutoRestart` defaults to `true`
(`foreground_task_options.dart:12`) and `ForegroundServiceController._ensureInit`
(`lib/data/notifications/foreground_service.dart:52-60`) does not override it —
so the app has opted *into* auto-restart and then disabled the only guard that
could fire it.

**Consequence.** Swipe the app off recents, or let Android reclaim it under
memory pressure, and the keep-alive service is gone permanently. The socket dies
with the process, no approval or turn-complete alert can arrive again, and
nothing restarts it until the user opens the app by hand. The app cannot even
report this, because the reporting code is in the process that was killed.
`RestartReceiver` — declared, kept, and commented — is dead code in the shipped
configuration.

**Confirmation.** Install a release APK, connect, background it, swipe it from
recents, then trigger a `permission_request` host-side. `adb shell dumpsys
activity services | grep flutter_foreground_task` should show the service gone
and never returning.

### F2 — the "connection replaced" close leaks the whole socket bundle and leaves the ping timer armed (high)

`_onSocketDone` handles close code 4001 by parking quietly
(`lib/data/ws/mcremote_client.dart:2491-2497`):

```dart
if (_channel?.closeCode == kCloseReplaced) {
  _failAllPending('connection replaced');
  debugPrint('mcremote: connection replaced by a newer login');
  _setState(McConnectionState.disconnected);
  return;
}
```

Every other terminal path in this class reaches `_teardownSocket`. This one
returns before it, so nothing runs the cleanup that `_teardownSocketImpl`
(`:2712-2753`) exists to do. Left behind:

* `_pingTimer` — still `Timer.periodic(10s)`, firing `_evaluateHealth()` forever.
  Only `_teardownSocketImpl:2716` cancels it.
* `_httpClient` — never `close(force: true)`, so its pinned connection pool stays
  resident.
* `_relayTransport` — never `close()`. When the session was riding the relay this
  is the expensive one: `RelayTransport` owns an outer WSS to `mcrelay`, a second
  `HttpClient`, **and a listening `ServerSocket`** on a loopback port
  (`lib/data/ws/relay_transport.dart:63-66`). All three stay open, and the relay
  continues to see this phone joined and holding a slot.
* `_sub` — never cancelled.

The next `_connectInternal` does call `_teardownSocket` first (`:2096`), so the
leak is bounded by the next dial. But 4001 means *a newer login replaced you*,
and the comment on this branch says the reconnect is deliberately deferred to the
next user-driven action — so "until the next dial" can be hours, or never.

**Consequence.** A phone that has been replaced by another device holds a relay
slot, a loopback listener and a 10 s wakeup timer indefinitely. On a
capacity-limited relay this is visible to other users, not just to this one.

**Confirmation.** A unit test on `McremoteClient` that closes the channel with
code 4001 and asserts `_pingTimer` is cancelled and the relay was closed. The
existing `mcremote_client_test.dart` already drives close codes, so the harness
is present.

### F3 — the shipped APK carries three permissions and one exported receiver the repository does not declare (medium)

`AndroidManifest.xml:29-33` states a decision:

```text
WAKE_LOCK was removed (MADR 0084 D3 finding): nothing used it.
```

The merged manifest that is actually packaged
(`build/app/intermediates/merged_manifests/release/processReleaseManifest/AndroidManifest.xml`)
disagrees:

```text
android.permission.ACCESS_NETWORK_STATE     <- connectivity_plus
android.permission.RECEIVE_BOOT_COMPLETED   <- flutter_foreground_task
android.permission.VIBRATE                  <- flutter_local_notifications
android.permission.WAKE_LOCK                <- flutter_foreground_task
```

`flutter_foreground_task`'s own manifest
(`~/.pub-cache/.../flutter_foreground_task-10.0.0/android/src/main/AndroidManifest.xml`)
declares `WAKE_LOCK`, `RECEIVE_BOOT_COMPLETED` and `POST_NOTIFICATIONS`
unconditionally, and the merger adds them. The app sets `allowWakeLock: false`,
so the permission is declared but not exercised — the *posture* is right and the
*artifact* is not, and an installed-app permission listing shows the artifact.

The same merge also lands an exported component the repository never reviewed:

```text
receiver  exported=true   perm=None
          com.pravera.flutter_foreground_task.service.RebootReceiver
          actions: BOOT_COMPLETED, MY_PACKAGE_REPLACED, QUICKBOOT_POWERON
```

Stated plainly: **its impact today is nil**, because F1's `stopWithTask` flag
makes `RebootReceiver.onReceive` return on its first branch for all three
actions. It is reported because of the precedent, not the exploit. This project
deliberately deleted its own `mcremote://pair` intent-filter for exactly this
class of reason (`AndroidManifest.xml:88-101`: *"an exported entry point that any
installed app or web page could fire"*), and then shipped a plugin-injected one
without noticing — because `lintVitalRelease` does not flag it and nothing in CI
reads the merged manifest.

**Confirmation.** `aapt dump permissions` on the published APK; and a CI step
that diffs the merged manifest's permission and exported-component sets against a
committed allowlist.

### F4 — the daemon advertises its read deadline and ping interval; the phone parses both and uses neither (medium)

`ServerCaps` decodes the v2 capability block including `read_deadline_ms` and
`ping_interval_ms` (`lib/data/protocol/models.dart:25-26, 40-41, 91-92`). A
repository-wide search finds **no reader**:

```text
$ grep -rn "readDeadlineMs" apps/mobile/lib/
models.dart:25   required this.readDeadlineMs,
models.dart:40   final int readDeadlineMs;
models.dart:91   readDeadlineMs: (m['read_deadline_ms'] as num?)?.toInt() ?? 60000,
```

The ping cadence is instead the compile-time constant `kAppPingPeriod =
Duration(seconds: 10)` (`lib/data/ws/link_health.dart:34`). MADR 0068 D2 calls
the advertised deadline *"contract, not just tuning"*
(`internal/config/config.go:131-136`); a contract that is decoded and discarded
is not being honoured. An operator who lowers `ws_read_deadline_seconds` to the
15 s floor gets a phone that still pings every 10 s by luck, not by agreement,
and one who raises it gets no saving at all.

The in-code justification for the cadence is also stale
(`mcremote_client.dart:2378-2383`):

```dart
// The daemon's read loop waits for a *data* message
// (`internal/ws/server.go:535`, 60 s deadline at `:165`); ...
```

The default is **120 s**, not 60 s (`internal/config/config.go:912`), with a 15 s
floor at `:939-940`. The line references no longer resolve to what they name.

**Amendment, 2026-09-01 — the deadline is not what pins the cadence.** While
drafting `0126-PLAN` the obvious remedy (derive the period from
`readDeadlineMs`) was checked against the rest of the client and does not hold.
`_noteInboundFrame` stamps `_lastVerifiedAt` on *any* inbound frame
(`mcremote_client.dart:736`, called at `:2788`), so on an idle session the app ping is the only
thing that verifies the link — and `kLinkFreshFor = 15 s`
(`link_health.dart:8`) is what turns an unverified link amber
(`classifyLinkHealth:81`). **Any ping period above 15 s makes a healthy idle
session render not-green.** The 10 s constant is therefore pinned by MADR 0063
D1's UI freshness contract, not by the daemon's deadline; deriving it from a
120 s deadline would give ~30 s and break the status indicator.

So the defect here is narrower than it first looked, and in the opposite
direction: the cadence is *right* and its stated reason is *wrong*. Two comments
misquote a deadline that does not constrain them (`mcremote_client.dart:2380`,
`link_health.dart:12`), and two decoded capability fields have no reader — which
means a daemon deployed at the 15 s floor, where `3 × kAppPingPeriod` exceeds
the deadline, gets silently dropped sessions that the phone has all the
information to predict and does not use.

**Consequence.** Low as shipped, because the repository's own default (120 s)
and the constant (10 s) happen to agree. It becomes real for any operator who
lowers `ws_read_deadline_seconds`, and it is a live trap for the next
maintainer, who will read "60 s deadline" and reason from it.

**Confirmation.** Set `ws_read_deadline_seconds: 15` on a daemon and connect a
phone: the session should either survive or say why it cannot, rather than
dropping on a timer nobody compared.

### F5 — the APK install copies the whole file on the platform main thread (medium)

`MainActivity.configureFlutterEngine`'s `installApk` handler
(`android/app/src/main/kotlin/.../MainActivity.kt:33-56`) calls
`UpdateInstaller.installApkSession` inline. A `MethodChannel` handler runs on the
Android main thread, and that method streams the entire APK into the
`PackageInstaller` session (`UpdateInstaller.kt:69-75`):

```kotlin
file.inputStream().use { input ->
    session.openWrite("package", 0, file.length()).use { out ->
        input.copyTo(out)
        session.fsync(out)
    }
}
```

The release APK is ~40 MB (measured in `proguard-rules.pro:11-12`:
`40,286,708 bytes`). A 40 MB read-plus-write plus `fsync` on the UI thread is
well past Android's 5 s ANR threshold on slow storage, and blocks every frame
until it returns.

Adjacent, same file: `res/xml/file_paths.xml` grants the FileProvider three
roots — `cache-path`, `external-cache-path` and `files-path`, each `path="."`.
Only the first is used (the verified APK lands in the cache directory, per
`lib/data/update/app_update.dart:95`). `files-path name="files_updates" path="."`
covers the whole of `getFilesDir()`, which on Android is where
`getApplicationSupportDirectory()` puts the `transcripts/` snapshots. No current
call path hands out such a URI — `installApk` is the only caller and it is fed a
cache path — but the grant is wider than the one job it exists for.

**Confirmation.** `adb shell am trace-ipc` / a StrictMode `detectDiskReads` policy
on the main thread during an in-app update; and a read of `file_paths.xml` against
the one path the installer actually uses.

### F6 — the notification-preferences error path reaches for `ref` after the scope may be gone (low)

`_ConnectionLifecycleScopeState` captures `_client`, `_coord` and `_recorder` in
`initState` with an explicit comment saying why
(`lib/app_lifecycle.dart:50-58`): *"`ref` dies with the element"*. The success
path honours that. The `catchError` path does not
(`lib/app_lifecycle.dart:108-112`):

```dart
unawaited(
  ref
      .read(errorRecorderProvider)
      .record(e, st, source: ErrorSource.app),
);
```

`_recorder` is right there, captured for this exact case. If the preferences read
fails *and* the scope has been disposed, `ref.read` throws on a disposed
`ConsumerState` — inside the handler whose whole purpose is to keep a preferences
failure from killing the notification layer.

**Consequence.** Narrow: it needs a preferences failure racing a dispose. But the
failure it would swallow is "the user's alert preferences silently reverted to
defaults", which the comment above it calls user-visible.

### F7 — three small durability gaps in `TranscriptCache` (low)

All in `lib/data/chat/transcript_cache.dart`:

* **`load()` and `usage()` bypass the serialisation chain** (`:304`, `:388`) that
  `save`/`remove`/`retainOnly`/`clear` all go through (`:160-165`). `load` does
  `file.existsSync()` then `await file.readAsString()` (`:308-309`); a
  `_retainOnly` deleting between those two lines makes the read throw. The throw
  propagates to `hydrateFromCache` (`transcripts_notifier.dart`) uncaught.
* **`_directory` runs the legacy migration inside the getter** (`:170-179`) and
  sets `_dir` *before* awaiting `_migrateLegacyEntries`. Two concurrent
  first-touchers — easily `load()` and `save()` on a cold cache — both run the
  migration over the same key snapshot.
* **`.tmp` files are never swept.** `_writeFile` writes `<name>.json.tmp` then
  renames (`:240-243`); a process death between the two strands the temp file.
  `_storedIds` filters on `.json` (`:353`) so `_clear()` cannot see it, and
  nothing else looks. Related: `_sessionIdFromFile` (`:228-229`) calls
  `Uri.decodeComponent`, which throws on a malformed escape — one junk filename
  makes `clear()` fail permanently.

None of these loses host-owned data; the cache is a re-fetchable snapshot by
design (MADR 0018 E1). They cost a round trip and, in the `clear()` case, a
Settings action that fails every time.

### F8 — a locally built APK is stamped `0.1.0 (1)` and always claims an update is available (low)

`apps/mobile/pubspec.yaml:4` still carries the Flutter template's
`version: 0.1.0+1`. CI overrides it —
`ci.yml:620-621` passes `--build-name="$BUILD_NAME" --build-number=...` — so
**published APKs are correct**. `make apk` (`Makefile:432-433`) passes neither.

**Consequence.** Every locally built APK reports `PackageInfo.version == "0.1.0"`,
so `AppUpdateService.checkLatest` (`lib/data/update/app_update.dart:51-77`)
compares `v0.15.3` against `0.1.0` and reports an update forever, and every local
build shares `versionCode 1` so the installer cannot order two of them. On-device
testing of the updater — the one workflow that needs a local APK — is the
workflow this breaks.

Noted while confirming the above and **not** a defect: `isNewerBase`
(`app_update.dart:28-35`) mirrors the now-unused Go `NewerBase` rather than
`NewerPublished`, which `update/run.go:81` switched to under MADR 0103. Every one
of the 97 release tags is three-part, so base-only comparison is correct today.
It becomes wrong the first time a four-part tag is published.

---

## Decision Drivers

* The live platform is Android, and F1 defeats the feature the app exists for.
* F1–F3 are all invisible to `flutter analyze`, to all 1358 Dart tests, and to
  `lintVitalRelease`. Fixing the instances without closing the gate leaves the
  next one equally invisible.
* F2 and F5 have blast radius beyond this device — a held relay slot, and an ANR.
* Findings differ in kind: F1, F2, F5, F6, F7 are defects with a right answer;
  F3 and F4 are decisions to restate or change. Mixing them into one
  "just fix it" pass would smuggle a decision into an implementation.
* This repository's convention ([0105](0105-MADR-mutating-work-requires-madr-and-plan.md))
  is that no source changes until an approved plan exists.

## Considered Options

* **One MADR plus one phased PLAN (this pair, `0126`)**, ordering the findings by
  severity and treating F3/F4 as decisions to be settled in the MADR before the
  plan touches them.
* **A MADR/PLAN pair per finding** — eight numbered pairs.
* **Record the findings only**, and let each be picked up ad hoc later.
* **Fix F1 and F2 immediately as hotfixes**, and record the rest.

## Decision Outcome

Chosen option: **"One MADR plus one phased PLAN (`0126`)"**, because the findings
share one root — nothing in this project inspects the Android build artifact or
its plugins' runtime behaviour — and splitting them across eight pairs would
repeat that context eight times while making the shared gate (a merged-manifest
check in CI) nobody's phase. The phases are ordered so F1 and F2 land first and
can be released without waiting on the rest.

### Decisions

Numbered so `0126-PLAN` can cite them.

* **D1 — The keep-alive service survives task removal and OS kill.** Remove
  `android:stopWithTask="true"`, restoring `START_STICKY` and the plugin's
  restart-alarm paths. Do **not** reach for the Dart-side
  `ForegroundTaskOptions.stopWithTask` override instead: when that preference is
  set, `ForegroundService.kt:130-136` additionally installs a visibility tracker
  that stops the service whenever the app becomes invisible — strictly worse
  than today.
* **D2 — The manifest says what the artifact does.** Including the honest limit:
  on API 31+ the alarm-driven restarts are best-effort, because the app holds no
  exact-alarm permission and a background `startForegroundService` is refused
  unless the app is exempt from battery optimisation. `START_STICKY` is the part
  that is reliable.
* **D3 — Close code 4001 runs the same teardown as every other terminal path.**
  Parking is a state decision, not a licence to skip cleanup.
* **D4 — The Android permission and exported-component surface is a committed
  allowlist**, diffed in CI against the merged manifest. Additions *and*
  removals fail; a plugin bump that widens the surface stops being invisible.
* **D5 — The app-ping cadence is pinned by `kLinkFreshFor`, not by the daemon's
  read deadline.** `kAppPingPeriod` stays 10 s and says so; the stale 60 s
  references are corrected; `ServerCaps.readDeadlineMs` either becomes a guard
  that reports a deadline the cadence cannot satisfy, or is deleted. A decoded
  field with no reader is not permitted to stay.
* **D6 — The installer does no bulk I/O on the platform main thread, and the
  FileProvider grants only the directory the updater writes to.**
* **D7 — Every lifecycle-scope path that can run after dispose uses the captured
  reference, not `ref`.** The class already made this decision; one path missed
  it.
* **D8 — `TranscriptCache` degrades, never throws.** It is a re-fetchable
  snapshot: a losing read returns null, and one malformed entry name is skipped
  rather than wedging `clear()` forever.
* **D9 — `make apk` stamps the version**, without claiming a ledger serial.

Proposed phase order for the accompanying `0126-PLAN`:

1. **F1** — keep-alive survival. Decide `stopWithTask` deliberately, and make the
   manifest comment describe what the artifact does.
2. **F2** — close the 4001 path through `_teardownSocket`, with a unit test.
3. **F3** — merged-manifest allowlist in CI; `tools:node="remove"` for what is
   genuinely unused.
4. **F5** — move the APK copy off the platform main thread; narrow
   `file_paths.xml` to the update cache.
5. **F4** — restate or change the ping-cadence decision against the real 120 s
   default; either consume `ServerCaps.readDeadlineMs` or delete the field.
6. **F6, F7, F8** — the low-severity cluster.

### Consequences

* Good, because F1 and F2 are separable and shippable ahead of the rest.
* Good, because the CI merged-manifest gate in phase 3 is what stops F3
  recurring on the next plugin bump, which no per-finding fix would.
* Good, because F3 and F4 are named as decisions rather than silently "fixed",
  so the record shows what was chosen and why.
* Bad, because one pair covering eight findings is a larger approval surface than
  eight small ones, and a stall in an early phase blocks the later ones from
  starting under this plan.
* Neutral, because none of the eight is a regression against the current suite:
  deferring any of them leaves the tree exactly as green as it is now, and
  exactly as broken.

### Confirmation

This MADR is confirmed when `0126-PLAN` exists, is approved, and each phase's
acceptance criteria are met. Per-finding confirmations are stated inline above.
Two are load-bearing and cannot be met from the host alone:

* **F1** needs a physical device: connect, background, swipe from recents, then
  raise a `permission_request` host-side and observe whether an alert arrives.
* **F5** needs an on-device in-app update with StrictMode's main-thread disk
  policy enabled.

Everything else is confirmable from a build artifact or a unit test.

## More Information

* Baseline for this pass: `flutter analyze` clean, `flutter test` `+1358 ~3`,
  tree at `19b8cd8`, Flutter 3.44.8 / Dart 3.12.2.
* Prior mobile assessment: [docs/mobile-ux-assessment.md](mobile-ux-assessment.md).
  Its headline gap ("there are no push notifications") has since been closed by
  MADR 0052 / 0101; it is stale and was not used as a source here.
* Related records: [0084](0084-MADR-android-app-hardening-and-performance.md) (D3 WAKE_LOCK removal,
  D7 R8/lint, the manifest posture F3 contradicts),
  [0068](0068-MADR-protocol-v2-reconnect-resilient-transport.md) (D2 advertised deadline, D3 close code 4001),
  [0065](0065-MADR-update-automation.md) (P4/P5, the installer in F5),
  [0067](0067-MADR-ios-port.md) (why iOS is out of scope for this pass),
  [0103](0103-MADR-update-tracks-release-build-and-active-service.md)
  (`NewerPublished`, the F8 note).

## Amendment — 2026-09-02: the plan is complete; row 4's residual moved to 0130

[The plan](0126-PLAN-android-client-debugging-pass-findings.md) is `complete`.
All eight findings F1–F8 are addressed, and P7's owner-verification rows 1, 2
and 3 pass — rows 1 and 2 via
[0129](0129-MADR-background-alert-delivery-survives-task-removal.md) P6, row 3
on 2026-09-02.

**Row 4 is the exception and is recorded as unsatisfied rather than closed.**
The stale-notification case this record targeted is fixed (29 s → ~2 s), but
measurement found a second window with a different cause: the client can
believe it is connected for tens of seconds after its socket dies, and every
surface that mirrors that belief — including the foreground-service
notification — repeats it. That is connection liveness, outside F1–F8, and now
carries its own record:
[0130](0130-MADR-client-can-sit-connected-with-no-socket.md), parked with the
cause unfound.

Two corrections this record should carry, because both were asserted here or in
its plan and both were wrong:

* **The in-app update was never blocked by the emulator's network.** Measured
  2026-09-02: the emulator pulls ~38 MB from the internet without difficulty
  and the app's own download runs at 2,880 KB/s. The earlier failures were
  transient.
* **In-loop SHA-256 is not a download bottleneck.** Instrumentation put hashing
  at **3.1%** of wall time against 91.6% waiting on the network, refuting a
  plausible reading of `app_update.dart`.

Both were confident inferences from reading code, and both fell to a single
measurement. The pattern is worth carrying forward more than either finding.

---
status: in-progress
date: 2026-09-01
associated-madr: "0129-MADR-background-alert-delivery-survives-task-removal.md"
---
<!-- markdownlint-disable MD013 MD024 MD033 MD060 -->

# PLAN 0129 — Stop lying first, then move the connection into the service isolate

Implements [0129-MADR-background-alert-delivery-survives-task-removal.md](0129-MADR-background-alert-delivery-survives-task-removal.md)
decisions D1–D5.

## Goal

Swipe the app off recents, and an approval alert still arrives — and until that
is true, the app says plainly that it will not.

Finish line:

* **Phase C** (P1–P2): after a task removal the notification stops claiming a
  live connection and offers a one-tap route back;
* **Phase A** (P3–P6): the socket, the ping, the reconnect and alert delivery
  live in the service isolate, and P7's rows 1–3 from 0126 pass on hardware or
  emulator.

## Scope

### In scope

Phase C:

* `apps/mobile/lib/data/notifications/foreground_service.dart` — a real
  `TaskHandler` in place of the no-op
* `apps/mobile/android/app/src/main/AndroidManifest.xml` — only if a
  notification action needs declaring
* tests for the handler's notification-state logic

Phase A, additionally:

* `apps/mobile/lib/data/ws/mcremote_client.dart` — construction from a second
  isolate; explicit ownership handover
* `apps/mobile/lib/data/local/settings_store.dart`,
  `lib/data/ws/client_identity.dart` — second-isolate access to secure storage
* `apps/mobile/lib/data/notifications/notification_coordinator.dart` — running
  service-side, and answering asks
* `apps/mobile/lib/app_lifecycle.dart` — handover on foreground/background
* `apps/mobile/lib/state/transcripts_notifier.dart` — reconciliation after a
  period with no UI isolate

### Out of scope

* **iOS.** MADR 0067 D2 parks the socket there by design; D1 is Android-only.
  Do not "harmonise" the platforms in this record.
* **Replacing `flutter_foreground_task`.** The coupling is a named cost in the
  MADR, not a task here.
* **Battery-optimisation prompting** and `autoRunOnBoot`. The MADR rules both
  out as wrong-layer.
* **Anything in 0126 P1–P6.** Complete and unaffected; P1's `stopWithTask`
  removal is a precondition for this work.
* **Row 3 of 0126 P7.** Passed; unrelated.

## Stability rule

Every phase ends with:

```bash
cd apps/mobile && dart format . && flutter analyze && flutter test
```

then **one commit** (`git commit --no-edit`; never `-m`).

**Widget tests cannot see any of this.** The defect this record exists for was
invisible to 1367 passing tests, because it lives in isolate lifecycle. A green
suite is necessary and proves nothing about the goal; every phase names the
on-device observation that does.

`git push` needs an explicit instruction in the same turn.

## Cross-cutting contracts

**C1 — One socket at a time.** D2. Before either isolate dials, the other must
have released. Two live sockets make the daemon close one with 4001 and the app
fights itself — the parked-zombie state 0126 F2 had to repair. Any phase that
can produce two connections is wrong even if it demoes correctly.

**C2 — The notification never outlives its owner's knowledge.** D3. If no
isolate is maintaining the connection, the title says so. This holds from P2
onward, not only at the end.

**C3 — An actionable notification's actions must work.** D4. Do not ship a
service-isolate alert carrying Allow/Deny until those buttons resolve the
permission host-side with no UI isolate running. A dead button is worse than a
passive notice — 0126 F2's reasoning about stale asks, one layer up.

**C4 — Every phase states its on-device observation.** See the stability rule.

**C1 and C3 are the ones at risk.** C1 because handover is invisible in a
single-isolate debug run; C3 because the notification will *look* right long
before the buttons work.

## Dependency and delivery order

```text
P1 (TaskHandler skeleton + data channel)
  -> P2 (honest notification)          <- Phase C ships here, independently useful
    -> P3 (second-isolate storage + identity)
      -> P4 (client in the service isolate, handover)
        -> P5 (alert delivery + Allow/Deny service-side)
          -> P6 (on-device verification)
```

P2 is a release boundary. If P3 onward stalls, the app is still honest.

## Implementation Steps

### P1 — A real TaskHandler and a data channel (D5)

Replace `_KeepAliveTaskHandler`'s empty methods with a handler that starts, logs
its lifecycle, and exchanges messages with the UI isolate over
`sendDataToMain` / `sendDataToTask` / `addTaskDataCallback`.

No connection logic yet. The point is to establish that a second isolate runs at
all, survives task removal, and can talk to the UI isolate when there is one.

**On-device observation:** after a swipe, the service isolate logs a heartbeat
and `DartWorker` is present in the surviving process — the exact thread whose
absence is the finding in 0126 P7.

### P2 — The notification stops lying (D3, C2) — Phase C ships

The handler tracks whether a UI isolate is alive (last message received, plus
`onDestroy`). When it is not, the handler updates the notification itself:

```text
title  Alerts paused
text   Tap to reconnect to your host
```

and `onNotificationPressed` launches the app.

Pick the wording deliberately: it must not imply an error, and must not claim a
connection. "Alerts paused" is a state the user can act on; "Disconnected" reads
as a fault they must diagnose.

**On-device observation:** swipe the app away; within one handler tick the
notification changes from "Connected to host"; tapping it restores the app and
the connection. This is the check that failed in 0126 P7 and is the acceptance
criterion for Phase C on its own.

### P3 — Secure storage and identity from a second isolate (D1)

`SettingsStore` and `ClientIdentity` back onto `flutter_secure_storage` and
`SharedPreferences`, both of which are platform-channel plugins. Establish
whether they work from a service isolate at all — background isolates need their
own `BackgroundIsolateBinaryMessenger` registration, and this is the phase most
likely to discover that something does not work rather than that it does.

**If a plugin cannot serve the service isolate, stop and report.** The fallback
shapes — passing credentials across the data channel, or having the UI isolate
prime a cache — have real security consequences (a device token crossing an
isolate boundary, or living longer in memory) and are not a decision to take
mid-phase.

**On-device observation:** the service isolate reads the pinned fingerprint and
the client identity, and logs their presence, with no Activity running.

### P4 — The client moves, with explicit handover (D1, D2, C1)

`McremoteClient` is constructed in the service isolate and owns the socket. The
UI isolate asks for the connection when it comes to the foreground, and yields
it when it goes away.

Handover, both directions, is a message and an acknowledgement — never a
timeout, never optimistic. The releasing side confirms its socket is closed
before the acquiring side dials.

Two things this phase must not do: let both isolates dial (C1), and let the
handover drop the resume token — `_resumeToken` is memory-only by design
(0068 D4), so a handover that loses it turns every app switch into a full
reconcile.

**On-device observation:** foreground/background the app repeatedly while
watching `lsof -iTCP:7531` daemon-side. Exactly one connection at all times, and
never zero while alerts are enabled.

### P5 — Alerts and answers from the service isolate (D4, C3)

`NotificationCoordinator` runs service-side against the service-side client.
~~`onNotificationButtonPressed` maps Allow/Deny to `respondPermission`.~~

> **Superseded 2026-09-02 — see the deviation entry at the end of this plan.**
> `onNotificationButtonPressed` fires only for buttons on the foreground
> service's *own* notification and cannot carry a per-ask alert. Allow/Deny
> instead route `ActionBroadcastReceiver` → `notificationBackgroundHandler` →
> `sendDataToTask` → `McRemoteTaskHandler.onReceiveData` → `respondPermission`.
> The rest of this step stands as written.

`showsUserInterface: true` on the Android actions exists precisely because the
plugin's default routed action taps to a background isolate that could not reach
the WebSocket (`notification_service.dart:187-192`). That reasoning inverts once
the socket lives in the service isolate — revisit it deliberately and record why
whichever way it lands.

**On-device observation, and this is C3's gate:** with the app swiped away,
raise a `permission_request`, tap **Allow** on the notification, and confirm the
permission resolves host-side with no Activity ever started.

### P6 — Re-run 0126 P7 (D1)

Rows 1 and 2 of 0126 P7, which currently fail, plus the notification check:

```text
1  swipe from recents, raise permission_request  -> alert arrives; DartWorker present
2  SIGKILL the process                           -> service AND isolate return;
                                                    socket reappears daemon-side
3  Allow from the notification, no UI isolate    -> resolves host-side
4  at no point does the title claim "Connected"  -> while no socket exists
```

Use a real `SIGKILL` for row 2. `adb shell am kill` — what 0126 P7 originally
specified — cannot kill a foreground-service process and tests nothing; that is
recorded in 0126's execution record.

Then close 0126: its P7 rows 1–2 pass, and its GATED battery-optimisation entry
resolves as "not needed" if rows 1–2 pass without it.

## Verification (whole plan)

```bash
cd apps/mobile && dart format --output=none --set-exit-if-changed . \
  && flutter analyze && flutter test
make apk && make manifest-surface
```

plus the six on-device observations above. The suite is a regression guard here,
not evidence for the goal.

### Acceptance criteria

1. Phase C alone: after a swipe the notification stops claiming a connection,
   and tapping it restores the app. (P2)
2. Swipe from recents, then a host-side `permission_request` produces a
   notification. (P6 row 1)
3. `SIGKILL` → the service and the isolate both return and the socket
   reappears daemon-side. (P6 row 2)
4. Allow, tapped with no UI isolate running, resolves the permission host-side.
   (P6 row 3, C3)
5. Exactly one socket at any moment across a hundred foreground/background
   cycles. (C1)
6. `flutter test` at or above 1367, and `make manifest-surface` still passes —
   a new notification action or receiver must be an allowlist decision, not a
   surprise.

Criterion 5 is the one most likely to be skipped and most likely to bite:
0126 F2 exists because a second connection was allowed to strand the first.

## Rollout and Rollback

Phase C is additive and revertible on its own — the handler changes what the
notification says and nothing else.

Phase A changes where the connection lives, which is not a small revert. It
should land behind whatever release cadence allows a real soak, and the fallback
is Phase C's honesty, which stays correct even if A is reverted.

Nothing here changes the daemon or the protocol, so a phone on an older build
keeps working against the same host.

## Deferred

* **iOS parity.** Out of scope by 0067 D2; the platforms diverge further and
  that is a known, accepted cost.
* **Reducing the `flutter_foreground_task` coupling.** Named as a cost in the
  MADR. Relevant to 0127's F-KGP watch, not resolvable here.
* **Whether the app-ping cadence should differ service-side.** 0126 D5 pinned it
  to `kLinkFreshFor` for UI freshness. With no UI, that constraint may not
  apply — but changing it belongs with the measurement in 0128 P4, not here.

## Execution record — 2026-09-01

### P1 + P2 — Phase C complete (D3, D5)

`_KeepAliveTaskHandler` — empty `onStart`, empty `onRepeatEvent` — is now
`McRemoteTaskHandler`. `eventAction` moved from `.nothing()` to a 30 s repeat;
the comment it replaced ("the service only keeps the process alive") *was* the
mistaken assumption, and the code now says so.

The two isolates share no memory, so the whole vocabulary between them is a
heartbeat carrying the title and text the UI isolate wants shown. Sent on a
timer **and** on every connection transition — the timer alone would leave up to
one interval of staleness after a state change.

**The design call worth recording:** a handler that has *never* seen a heartbeat
reads as paused. That is the `START_STICKY` case — the system recreated the
service after a process death, so the main isolate is gone and never checked in.
Assuming "alive until proven otherwise" would hold a stale "Connected to host"
for the entire grace window, which is the bug itself. Proven to discriminate:

```text
with `return false` for the never-seen case:
  no heartbeat ever seen reads as paused [E]  Expected: true  Actual: <false>
restored: +5 All tests passed
```

Wording is deliberate: "Alerts paused" is a state the user can act on;
"Disconnected" reads as a fault to diagnose, and nothing is faulty — nothing is
maintaining it.

#### On-device observation (AVD `mcremote_test`, API 36, APK 0.15.3.6)

Paired to the live daemon, then swiped from recents:

```text
BASELINE   title "Connected to host"; DartWorker present; socket up
           engines 1.* and 2.*  (2.* is the new service isolate — P1's check)

SWIPE      recents entries        0
           process                alive (pid 2345)
           FGS record             1
           DartWorker             gone      <- main isolate dead, as in 0126 P7
           socket                 gone

RESULT     title "Alerts paused"
           text  "Tap to reconnect to your host"      <- the check that failed in 0126 P7

TAP        top activity  magic_cli_remote/.MainActivity
           socket        restored
           title         "Connected to host"          <- handler stood down
```

Phase C's acceptance criterion is met: the notification no longer claims a
connection nobody is maintaining, and one tap restores it.

**What this does NOT do**, restated so the record cannot be misread: alerts
still do not arrive after a swipe. That is Phase A (P3–P6). Phase C makes the
app honest about it, nothing more.

#### Two measurement errors, both mine

* The title-monitoring loop used `grep -B4 -A16 "channel=host_connection"` and
  reported "no notification" ten times running while the notification was
  present and correct the whole time — the pattern assumed a line ordering
  `dumpsys` does not guarantee. A plain `grep -oE 'android\.title=...'` showed
  the truth immediately. The flip therefore happened somewhere inside a 150 s
  window rather than at a captured moment; the *outcome* is verified, the
  latency is not.
* A `DartWorker` count read 0 on a connected app, contradicting the evidence
  0126 P7 rests on. It was a nested command substitution racing the app
  restart; a clean re-read showed `DartWorker` present. The 0126 finding stands
  — verified there by a controlled before/after diff, not by a single sample.

Seventh and eighth verification-of-a-verification failures in this body of work.
Both were caught because the result contradicted something already established.

#### Scope addition — `docs/ops-android-emulator.md`

Not in this plan's file list. Pairing failed three times with **"code already
used"** before the cause was found: **the emulator loads the virtual-scene
poster at boot and caches it.** Every poster swap after boot was invisible, so
the camera kept showing the first QR, whose one-shot code had been consumed an
hour earlier. The ops doc documented overwriting `poster.png` but not that the
swap must precede boot. Added, with `mcremote pair list` named as the check — a
code that was never claimed does not appear there at all.

P6 will need pairing again, so leaving this undocumented would have cost the
same hour twice.

### P3 — Secure storage and identity from the service isolate: **both work** (D1)

This phase was written as the gate — *"the phase most likely to discover that
something does not work rather than that it does"* — with instructions to stop
and report if a plugin could not serve the service isolate, because the
fallbacks put a device token across an isolate boundary.

**No fallback is needed. Both plugins answer.**

#### Why, before the measurement

`flutter_foreground_task` does not spawn a bare isolate. It creates a **full
`FlutterEngine`** for the task
(`ForegroundTask.kt:49`, `flutterEngine = FlutterEngine(context)`), takes that
engine's own `binaryMessenger`, and runs the entry point through
`DartExecutor.executeDartCallback`. That engine registers plugins, so the task
isolate is not a "background isolate" needing
`BackgroundIsolateBinaryMessenger.ensureInitialized` — it is a second engine
with a working channel of its own.

#### Measured, on device, twice

A temporary probe in the handler read `SharedPreferences` and
`flutter_secure_storage`, logging presence and length only — never a value,
since this lands in a device log.

With the main isolate **alive**:

```text
mcremote/fgs: task isolate started (system)
mcremote/fgs/probe[onStart]: SharedPreferences OK, 10 keys, host=true
mcremote/fgs/probe[onStart]: secure storage OK, device_token present=true len=68
```

With the main isolate **dead** — the case D1 actually needs — fired when the
handler concluded the UI isolate was gone:

```text
mcremote/fgs/probe[uiIsolateGone]: SharedPreferences OK, 10 keys, host=true
mcremote/fgs/probe[uiIsolateGone]: secure storage OK, device_token present=true len=68

corroboration at the same instant:
  DartWorker  0            <- main isolate gone
  socket      0            <- nothing connected
  title       "Alerts paused"
```

So the service isolate can read the host, the device id, the pins and the device
token with no Activity in existence. **D1 is achievable as designed**, and the
security question the phase flagged does not arise: no credential has to cross
an isolate boundary, because the isolate that needs it can fetch it.

#### Incidental: the grace window, measured

The `uiIsolateGone` probe fired at **~t+90 s** after the swipe, against a 95 s
window and a 30 s check interval. That is the flip latency P2 could not capture
because its monitoring grep was broken — recorded here since the probe happened
to timestamp it.

#### The probe is removed

It answered its question, and a permanent version would read the device token on
every pause transition — unnecessary work against a secret, for no ongoing
benefit. P4 will read storage as part of doing its job, not as diagnostics.
Gate after removal: `dart format` clean, `flutter analyze` clean,
`flutter test` `+1372 ~3`.

### P4 — The connection moved into the service isolate (D1, D2, C1)

**The rule implemented**, narrower than "ownership changes on every lifecycle
transition" and deliberately so: the service isolate takes the connection **only
when the UI isolate is confirmed gone**, and hands it straight back when the UI
isolate reappears. While the UI isolate lives — foreground or backgrounded — it
keeps the socket exactly as before, so nothing about today's working behaviour
changes.

That choice also makes C1 nearly free in one direction. When the handler
concludes the UI isolate is gone, that isolate is not holding a socket either —
0126 P7 measured zero connections daemon-side in exactly that state — so there
is nothing to collide with. Only the hand-back needs an acknowledgement.

* `_takeOwnership()` builds an `McremoteClient`, calls
  `reconnectFromStore(SettingsStore())` — reachable from here, proven in P3 —
  and drives the notification title from the real connection state, so MADR
  0056 H-5a's invariant holds no matter which isolate owns the socket.
* `_releaseOwnership()` disconnects with `manual: false` (a handover, not a
  sign-out: pairing and stored credentials are untouched), then sends
  `mc.released`.
* `ForegroundServiceController.claimOwnership()` sends a heartbeat and waits for
  that acknowledgement. A timeout does **not** mean "dial anyway" — it means the
  service never answered, which is almost always because no service is running,
  and the caller is told which happened.
* `app_lifecycle.dart` gates its `reconnectFromStore` behind that claim.

#### On-device: the service takes over (APK 0.15.3.8)

Swiped from recents, then watched:

```text
t+015s..t+075s   sockets=0   "Connected to host"    <- stale title, pre-takeover
t+090s           sockets=1   "Connected to host"

at t+090s, corroborated:
  recents entries   0                      <- app swiped away
  top activity      nexuslauncher          <- not foreground
  engines           1.raster only          <- the service's engine, alone
  notification      "Connected to host"
                    "Listening for approvals — app closed"
  handler log       took ownership, state=McConnectionState.connected
  daemon            emu-0129b LAST_USED 2026-09-02T01:42:12Z
```

**A live, authenticated connection to the daemon, with the app swiped away and
no Activity in existence.** That is D1, and it is the thing 0126 P7 row 1 proved
impossible before.

#### On-device: C1 across the hand-back

The relaunch sampled every 500 ms for 30 s, spanning the handover:

```text
samples 60   max concurrent sockets 1   distribution: 60x 1
handler log  released ownership
title        "Connected to host"
```

Never two, and never zero. The acknowledgement does its job.

#### Correction: `DartWorker` is not a liveness indicator

Recorded because this session leaned on it repeatedly. After the relaunch the
main isolate is unambiguously alive — `topResumedActivity` is
`magic_cli_remote/.MainActivity`, the socket is up, and a second engine
(`3.raster`) exists — and `DartWorker` is **absent**. It is a Dart VM worker
thread created on demand, not a per-isolate fixture.

0126 P7's conclusion still stands: it rested on a controlled before/after diff in
which `3.io`, `3.raster` **and** `DartWorker` all disappeared together, and was
independently corroborated by zero sockets daemon-side and no Flutter log line
of any kind. But single-sample "DartWorker: 0" was weaker evidence than it was
presented as, in this record and in earlier summaries. **The reliable indicators
are the numbered engine thread groups (`N.raster` / `N.io`) and the socket.**

#### Not verified

* Acceptance criterion 5 asks for a hundred foreground/background cycles; this
  was one hand-back, sampled densely. One clean handover is not a hundred.
* P5's work — alert delivery and Allow/Deny from the service isolate — is
  untouched. The service now holds a connection, but `NotificationCoordinator`
  still runs only in the UI isolate, so a `permission_request` arriving while
  the app is swiped away is received and dropped.

Gate: `dart format` clean, `flutter analyze` clean, `flutter test` `+1372 ~3`.

## Deviation — 2026-09-02: P5's answer path is a forwarding hop, not a callback

**Found:** at the start of P5, before any code was written, reading the plugin
sources rather than trusting the step.

**What the plan said.** P5: "`onNotificationButtonPressed` maps Allow/Deny to
`respondPermission`." That step is wrong as written; the original wording is
struck through above rather than rewritten.

**Evidence.** Three readings, all in the pinned plugin versions:

1. `flutter_foreground_task-11.0.1/android/src/main/kotlin/com/pravera/flutter_foreground_task/service/ForegroundService.kt:563-590`
   — `buildNotificationActions` builds button `PendingIntent`s from
   `NotificationContent.buttons`, i.e. only the foreground service's own
   notification. `:98-118` receives the broadcast and calls
   `task?.invokeMethod(action, data)`. No Activity is started, which is the
   half of D4 that survives; but nothing here can attach buttons to a per-ask
   `flutter_local_notifications` alert.
2. `flutter_local_notifications-22.3.0/android/src/main/java/com/dexterous/flutterlocalnotifications/FlutterLocalNotificationsPlugin.java:330-359`
   — `showsUserInterface: true` builds `PendingIntent.getActivity(getLaunchIntent(…))`,
   which **starts the Activity** and fails C3's gate; `false` builds
   `PendingIntent.getBroadcast(ActionBroadcastReceiver)`.
3. `.../ActionBroadcastReceiver.java:92-121` — `startEngine` **always**
   constructs a new `FlutterEngine` and runs the registered background callback
   in a third isolate. It never routes to the foreground service's isolate.

**Pre-existing defect found in the same reading, and it is genuinely
pre-existing.** `ActionBroadcastReceiver` is not declared in
`android/app/src/main/AndroidManifest.xml` (which declares `RestartReceiver`,
`FileProvider` and `UpdateInstallReceiver` only), and the plugin's own manifest
contributes no components. Confirmed pre-existing by history, not by assumption:
`git log -S ActionBroadcastReceiver --oneline -- apps/mobile/android/` returns
**no commits**, so the receiver has never been declared in any revision. (P1–P4
did touch `AndroidManifest.xml`, for `stopWithTask` and `WAKE_LOCK` under 0126
P1/P3 — hence checking the whole history rather than just this plan's diff.) A `PendingIntent.getBroadcast` for an
undeclared receiver is created without error and delivered to nothing, so
`notificationBackgroundHandler` (`notification_service.dart:551`) has never been
reachable in this app. Nothing has ever routed to it, which is why the gap was
invisible.

**Decision (owner, 2026-09-02): the alert keeps its own channel and the answer
takes one hop.** Chosen over putting Allow/Deny on the foreground-service
notification, which is the only literal use of `onNotificationButtonPressed`
and would have dropped the ask onto the `host_connection` channel — no heads-up,
outside the per-kind Settings toggle (0101 C), one ask at a time, and the status
row and alert row merged. The full comparison is in the MADR amendment.

```text
service isolate  --show(showsUserInterface: false)-->  approval_needed channel
                                                              | tap Allow
                                                              v
                                                  ActionBroadcastReceiver
                                                              | new engine
                                                              v
                                                 notificationBackgroundHandler
                                                              | sendDataToTask
                                                              v
                                          McRemoteTaskHandler.onReceiveData
                                                              |
                                                      respondPermission
```

The hop is in-process and Activity-free: `FlutterForegroundTask.sendDataToTask`
→ `MethodCallHandlerImpl.kt:78` → `ForegroundServiceManager.sendData` →
`ForegroundService.sendData`, a companion-object static guarded only by
`isRunningServiceState`, reached through `ServiceProvider` with no
`ActivityPluginBinding` (`FlutterForegroundTaskPlugin.kt:31-37`).

**`showsUserInterface` resolves as: whoever shows the ask sets it to whether
they are the UI isolate.** The UI isolate keeps `true` — the reasoning at
`notification_service.dart:202-207` is unchanged and still correct for it. The
service isolate sets `false`. This is the deliberate revisit P5 asked for, and
it lands on "both, depending on the shower", not on one value.

**Files added to P5's scope** beyond what the step named:

* `apps/mobile/android/app/src/main/AndroidManifest.xml` — declare
  `ActionBroadcastReceiver`, `exported="false"`. `manifest-surface.allow` is
  unchanged: the gate records exported components and permissions, and this is
  neither. That the file needs no edit is itself checked by `make
  manifest-surface`.
* `apps/mobile/lib/data/notifications/notification_service.dart` —
  `showsUserInterface` becomes a parameter; `notificationBackgroundHandler`
  gains the forwarding body.

**Consequence of doing nothing:** C3 stays unmet. With the app swiped away the
alert arrives and its buttons either do nothing (`false`, no receiver) or start
the Activity (`true`) — the exact state 0129 exists to end. 0126 P7 row 3 and
acceptance criterion 4 would stay open, and P6 could not run.

### P5 — Alerts and answers from the service isolate: **C3 met** (D4)

APK `0.15.3.12`, AVD `mcremote_test` / API 36, daemon `macos-laptop`, device
`emu-0129b`, grok session `a787cbb9` in Plan mode.

**The gate, passed.** With the app swiped from recents, a `permission_request`
raised host-side was delivered by the service isolate and answered from the
shade with no Activity at any point:

```text
21:45:47.047  mcremote/fgs: alerts started (enabled=true, asks=true)
21:45:47.047  mcremote/fgs: took ownership, state=McConnectionState.connected
21:45:47.056  mcremote/fgs: reconciled 1 pending ask(s), showing 1
    ... user taps Allow in the notification shade ...
21:47:23.055  mcremote/notif-bg: forwarded allow to service isolate
21:47:23.056  mcremote/fgs: answering allow from shade
02:47:22.757Z permission_resolved  status=resolved  option=plan_approve   (daemon, UTC)
```

Activity state, sampled either side of the tap — unchanged in every field:

```text
                 before tap        after tap
pid              5895              5895
recents entries  0                 0
topResumedActivity  nexuslauncher  nexuslauncher
MainActivity records  0            0
phone sockets    1                 1
```

The answered notification was gone afterwards (`id=1029204051` absent from
`dumpsys notification`), so the cancel-only-on-success rule holds service-side
too: the shade cleared because the respond succeeded, not because the button
was pressed.

**`showsUserInterface` resolved, and the evidence is a side-by-side.** One
`dumpsys notification` captured both isolates' versions of the same alert:

```text
id=1029204051  when=21:45:47  posted by the SERVICE isolate
  [0] "Allow" -> PendingIntent{... broadcastIntent ...}
  [1] "Deny"  -> PendingIntent{... broadcastIntent ...}

id=137623239   when=21:37:00  posted by the UI isolate (stale, earlier ask)
  [0] "Allow" -> PendingIntent{... startActivity ...}
  [1] "Deny"  -> PendingIntent{... startActivity ...}
```

That is the rule working as intended: the flag tracks who is showing, not a
global preference. The UI isolate's reasoning at `notification_service.dart` is
unchanged and still correct for it.

**Manifest.** `ActionBroadcastReceiver` declared `exported="false"`. Verified in
the shipped binary, not just the source — `aapt dump xmltree` on
`app-release.apk` shows `android:enabled=0xffffffff`, `android:exported=0x0`.
`make manifest-surface` passes unchanged, as predicted: the gate records
exported components and permissions, and this is neither.

#### Two defects the gate caught, both mine, both in this plan's own code

Recorded at length because in each case the code looked right, the logs said
the alert path had started, and the thing still did not work.

**1. `init()` needs an Activity, and the service isolate has none.**
`NotificationService.init()` called `requestNotificationsPermission()`, which
resolves against the plugin's activity. In the service isolate that is null:

```text
NotificationService.init failed (non-fatal): PlatformException(error,
  Attempt to invoke virtual method
  'int android.content.Context.checkPermission(java.lang.String, int, int)'
  on a null object reference ...
  at FlutterLocalNotificationsPlugin.requestNotificationsPermission
```

`init` threw, `_ready` stayed false, and every `show*` returned at
`if (!await _ensureReady()) return;` — **silently**. The run reported
`alerts started (enabled=true, asks=true)` and posted nothing. Fixed by
skipping the permission request when `inServiceIsolate`: a permission can only
be requested where there is UI to request it in, the UI isolate already does
that, and the foreground service's own notification being on screen is proof
the grant exists. Channel creation stays on both paths — it needs only the
application context and is idempotent.

That silent return is fixed too, separately: `_ensureReady` now takes the
caller's name and logs `<what> skipped: notifications unavailable (<error>)`.
It was the quietest failure in the app — one failed init, and every alert
afterwards vanished with no line saying why.

**2. The UI isolate went quiet while alive, and the service dialled over it.**
`ForegroundServiceController._start()` armed the heartbeat only after a *fresh*
`startService`; the `isRunningService` early return skipped it. That branch is
the normal path now that 0126 P1 stopped task removal from killing the service
— the app relaunching onto a service that outlived it. So the UI isolate sent
no heartbeats, the service isolate concluded after `kUiIsolatePresumedGone`
that it was gone, and dialled over a socket that isolate still held:

```text
21:24:01.988  mcremote: connection replaced by a newer login
21:24:02.007  mcremote/fgs: task isolate destroyed (timeout=false)
21:24:02.009  mcremote/fgs: takeOwnership failed: disconnected
21:24:02.009  mcremote/fgs: released ownership
```

Daemon-side, `reason=replaced` at 21:24:01.681. This is a **C1 violation and
exactly the self-fighting the 4001 close produces** — the parked-zombie state
0126 F2 exists to repair. It is P4's code, found by P5's run. Fixed by arming
the heartbeat in both branches (`_startHeartbeat` cancels first, so it is
idempotent). After the fix, three takeovers with `phone sockets = 1` throughout
and no `replaced` in the daemon log.

**Neither defect has a unit test, and that is a gap, stated rather than
hidden.** Both live behind `FlutterForegroundTask` / `FlutterLocalNotifications`
statics that a widget test cannot reach, and the seam needed to fake them is a
larger refactor than this phase. The on-device gate is the only check for
them — which is at least a check that has been *seen to fail*, twice, on real
defects rather than injected ones.

#### What was tested off-device

`test/notif_action_forward_test.dart`, 11 cases over the wire format — the one
piece of the hop that is pure. Both ends are unreachable from a widget test
(one lives in a plugin-spawned engine, the other in the foreground service), so
the encoding is all a machine can check.

Mutation-tested to confirm the tests can fail, against a **copy** of the tree in
the scratchpad, never the working tree:

* unknown action defaults to `allow` instead of being refused →
  `an unknown action name is refused, not guessed` fails
  (`Expected: null  Actual: <Instance of 'NotifActionForward'>`);
* discriminator dropped so any map decodes as a tap → three fail, including
  `the UI isolate heartbeat is not a tap` and
  `the release acknowledgement is not a tap`.

Both mutations asserted to have landed before running (a `replace` that matches
nothing exits zero and proves nothing).

#### Suite

`dart format` clean, `flutter analyze` clean, `flutter test` **+1383 ~3**
(+11 over P4's +1372).

#### Not done in P5

* **Questions carry no inline actions**, unchanged and deliberate — answering
  one needs the in-app option list, so its tap opens the app. Only
  permissions are answerable from the shade. Observed in this run: a pending
  `question_request` produced `Question: Session a787cbb9` with no buttons.
* **The "service not running" fallback was reasoned about, not exercised.**
  `ForegroundService.sendData` drops the message when the service is down and
  the notification stays on screen for a body tap. Nothing forced that state.
* **Acceptance criterion 5** (100 foreground/background cycles) remains
  unmet — this run did four.

### P6 — Re-run of 0126 P7: rows 1 and 2 pass, row 4 partly (D1)

APK `0.15.3.13`, AVD `mcremote_test` / API 36, daemon `macos-laptop`, device
`emu-0129b`, grok session `a787cbb9`.

```text
row 1  alert after task removal      PASS
row 2  SIGKILL recovery              PASS
row 3  Allow with no UI isolate      passed in P5; NOT re-run here (see below)
row 4  the title never lies          PARTLY — the case P6 targeted is fixed,
                                     a different one is open
```

**Row 1.** Task removed, then a host-side `permission_request` was delivered by
the service isolate with no Activity in existence:

```text
22:40:02.841  ws client disconnected  reason=peer_closed     (task removed)
22:41:22.504  device authenticated                           (service isolate)
22:41:22.905  mcremote/fgs: took ownership, state=connected
22:41:22.917  mcremote/fgs: reconciled 1 pending ask(s), showing 1
notification  when=22:41:22  [0] "Allow" -> broadcastIntent
                             [1] "Deny"  -> broadcastIntent
```

recents 0, zero `MainActivity` ActivityRecords, process alive. No
`reason=replaced`.

**Row 2.** A real `SIGKILL`, and recovery is now **3.5 seconds**:

```text
22:42:17.266  ws client disconnected  reason=peer_closed     (SIGKILL)
22:42:19.698  mcremote/fgs: task isolate started (system)    (START_STICKY)
22:42:20.820  device authenticated                           (socket back)
22:42:21.251  mcremote/fgs: reconciled 1 pending ask(s), showing 1
```

Before the `onStart` change below it was ~30 seconds, because recovery waited
for the first `onRepeatEvent`.

**Row 3 was not re-run, and this is not a pass.** The pending permission
expired on the daemon's five-minute timer before the tap (the service isolate
replaced it with "Permission timed out" in the shade, which is 0101 A working
service-side — incidental evidence, not the row). It passed in P5 against
`0.15.3.12`, and nothing in this phase touches the Allow path, but it has not
been observed against `0.15.3.13`.

#### Three defects, all in this plan's own code, all found by P6

**1. The cold-start dial never claimed the connection.** P4 wired
`claimForegroundOwnership()` into the *resume* path only. A cold start — the
Activity recreated into a process the service kept alive, which is exactly the
"swiped away, service took over, user reopens" flow — went straight to
`client.connect()` in `connect_screen.dart`:

```text
21:40:12.075  device authenticated                      (UI isolate dialled)
21:40:12.075  ws client disconnected  reason=replaced   (service's socket)
21:40:12.361  mcremote: connection replaced by a newer login
```

A C1 violation and the 4001 self-fighting D2 exists to prevent. Fixed; after
the fix a cold start reads:

```text
22:21:05.465  peer_closed                        (service released first)
22:21:05.779  mcremote/fgs: released ownership
22:21:07.232  device authenticated               (UI isolate dialled after)
```

**2. The release acknowledgement had never been delivered — not once.**
`sendDataToMain` resolves its target through
`IsolateNameServer.lookupPortByName`, and the plugin does not register that
port: the app must call `FlutterForegroundTask.initCommunicationPort()` itself.
The app never did. Every message the service isolate sent the UI isolate was
looked up, found missing, and dropped in silence.

So `claimOwnership` had only ever ended on its three-second timeout. Handover
*appeared* to work because the service releases promptly on the heartbeat and
the app dialled once the stopwatch ran out — the mechanism D2 is built on was
inert, and what shipped was the fallback. Fixed in `main.dart`; the timeout
branch has not logged once since (`grep -c 'no release ack'` = 0), and the
cold-start handover above completes in 1.4s rather than 3s+.

**3. A system-started service inherited a "Connected" title it could not back
up.** The plugin restores the last notification content across a START_STICKY
restart, and `onStart` deliberately corrected nothing, so the notification came
back reading "Connected to host" and kept saying it until the first
`onRepeatEvent` — **29 seconds** measured (21:55:15 → 21:55:41, daemon with no
connection until 21:55:44). D3's invariant broken in the case P2 had called out
as most likely to be stale. Fixed by taking ownership immediately when
`starter == TaskStarter.system`; that window is now ~2s, and it also removed
the 30s recovery delay in row 2.

#### Row 4: what is fixed and what is not

Sampling the foreground-service title every ~2s against the daemon's own
connection log, across a SIGKILL and the following minutes:

```text
22:42:18  ~2s   conn=DOWN  title=Connected to host   restored notification,
                                                     before onStart corrects
22:43:05  ~31s  conn=DOWN  title=Connected to host
22:43:38         conn=DOWN  title=Reconnecting to host
```

The second window is **not the notification layer**. The title mirrored the
client's state faithfully; the client believed it was connected for ~33s after
the socket died, and flipped to "Reconnecting" at 22:43:38 — the same instant
it logged `Relay died under a live session; reconnecting over Mesh`. That is
liveness detection in `McremoteClient` (0068 / 0089), reached here because this
AVD's transport began dying every ~40s late in the session:

```text
22:43:02  mcremote: Mesh died under a live session; reconnecting over Relay
22:43:38  mcremote: Relay died under a live session; reconnecting over Mesh
```

**Decision (owner, 2026-09-02): measure the flap before touching any timing.**
Shortening a read deadline or ping cadence against a single flaky emulator run
is how more aggressive reconnects and worse battery reach real networks, and it
interacts with the per-generation fallback budget 0062 D11 protects. Whether
~33s is a defect or the design working on a genuinely dying link is not
established, so no transport timing is being changed on this evidence. Row 4
stays open; see the new deferred entry below.

#### Two measurement corrections, because both could have produced false passes

**`lsof` was measuring the wrong thing, and earlier records lean on it.** The
socket counts reported in P4 and P5 (`phone sockets: 1`) came from
`lsof -iTCP:7531 -sTCP:ESTABLISHED | grep 100.64.0.2`. That entry turned out to
be the *same* fd and source port (`0xed1300a04916be99`, `:38128`, fd `90u`)
across an hour of connects and disconnects — a stale or persistent view, not
the app's live connection. It read `1` continuously through a window the daemon
logged as disconnected.

The C1 conclusions in P4 and P5 still stand, but on different evidence: the
daemon logs `reason=replaced` at exactly the moment a second login displaces a
first, which is the definitive signal. Every `reason=replaced` in this
session's daemon log is now accounted for — 20:44, 20:46, 21:24 (the unarmed
heartbeat, P5), 21:40 (the unclaimed cold start, above), 22:43 (a transport
fallback dialling before the old socket was reaped, which is 0062 behaviour and
not an isolate conflict). Prefer the daemon log over `lsof` for this question.

**A sampler that had already exited reported zero violations.** The first row-4
run was configured for 150 samples and finished at 22:37:18; the task removal
it was meant to cover happened at 22:40:02. `grep -c` over its output returned
"0 violations" — a vacuous pass from an instrument that was not running. The
re-run adds a coverage assertion (count the `conn=DOWN` samples; zero of them
means the window was never observed, not that nothing was wrong).

#### One substitution in method, stated because it is not the literal row

The injected swipe gesture stopped being registered by the launcher partway
through the session (`input swipe` and explicit `input motionevent` sequences
both left the card in place, confirmed by screenshot). Task removal was done
with `adb shell am stack remove 41` instead, which routes through the same
`removeTask` path a swipe does — the path that fires `onTaskRemoved` — and the
observable end state matches: recents 0, zero `MainActivity` ActivityRecords,
process alive. It is the same framework operation, not the same input event.

#### Tests

`test/foreground_handover_test.dart` (new, 4 cases) covers the acknowledgement
in **plain `test`, not `testWidgets`** — deliberately: the ack arrives on an
isolate `ReceivePort`, which delivers on the real event loop, while
`testWidgets` fakes the clock, so inside a widget test the reply never lands in
a pump and `claimOwnership` can only ever reach its timeout. Testing it there
would have measured the fallback and called it the mechanism. The suite is
differential — the port registered (ack heard, well inside the timeout) against
the port missing (times out), which is the shipped defect encoded both ways.

`connect_screen_test.dart` gains a cold-start regression test asserting the
service is *asked* before the dial, and a `flutter_foreground_task/methods`
stub in `setUp`: unstubbed, the claim's platform call never answers, the
"Reconnecting with saved credentials…" spinner animates forever, and
`pumpAndSettle` cannot idle — the dial looks hung when it is only unmocked.

Mutation-tested against a scratchpad copy, never the working tree: making
`onData` ignore the acknowledgement fails exactly the ack test
(`Expected: true  Actual: <false>`, "the release ack should have been heard").

`dart format` clean, `flutter analyze` clean, `flutter test` **+1388 ~3**.

### ~~OPEN — connection liveness detection latency (from P6 row 4)~~ SUPERSEDED

**Superseded 2026-09-02 by
[0130](0130-MADR-client-can-sit-connected-with-no-socket.md).** The
measurement this entry asked for was taken, and it changed the diagnosis: the
detection latency is not the defect. A deterministic black-hole showed the
client notices a dead link in **25 seconds**, exactly as the 10 s ping with a
two-miss rule predicts. What follows the failover is the problem — a superseded
socket's close event dismantles the live one, leaving no socket, no ping and no
reconnect while the state still reads `connected`. Eleven minutes were observed,
seven of them on a healthy network.

The original entry is kept below because its trigger — "measure on a transport
that is not flapping" — is what produced that result.

### OPEN — connection liveness detection latency (from P6 row 4)

**What was seen.** With the socket dead daemon-side from 22:43:05, the client
went on reporting `connected` until 22:43:38 — ~33 seconds — and the
foreground-service title, which mirrors that state, said "Connected to host"
throughout. The title is not the bug; the client's belief is.

**Why it is not being fixed here.** The only transport that exhibited it is
this AVD's, which began dropping the socket every ~40s late in the session.
Tuning a read deadline or ping cadence against that would change behaviour on
every platform and every session on the strength of one flaky emulator, and it
interacts with the per-generation fallback budget in 0062 D11.

**Trigger to resume.** A controlled session on a transport that is not flapping,
measuring how long the client takes to notice a socket that dies under it. If
the gap reproduces there, fix the deadline/cadence with the measured number and
record it against 0068 / 0089, which own connection liveness — not here.

**Consequence of leaving it.** For up to ~33s after a link dies, the
notification claims a connection that is gone, so a user glancing at the shade
can believe alerts are arriving when they are not. 0126 P7 row 4 therefore
stays open on its literal wording, even though the START_STICKY case it was
written to catch is fixed (29s → ~2s).
